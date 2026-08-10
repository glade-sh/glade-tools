package corpusassurance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const candidateAuthorityStatus = "current-base-candidate-rebind-authorized"

type CandidateAuthorityRequest struct {
	RunRoot    string
	RebindPath string
	OutputPath string
}

type candidateAuthorityDocument struct {
	SchemaVersion      int                      `json:"schemaVersion"`
	Status             string                   `json:"status"`
	Binding            candidateAuthorityInput  `json:"binding"`
	BoundInputs        candidateAuthorityInput  `json:"boundInputs"`
	CandidateRebind    candidateAuthoritySource `json:"candidateRebind"`
	SourceBuildReceipt candidateAuthoritySource `json:"sourceBuildReceipt"`
	SuccessorManifest  candidateAuthoritySource `json:"successorManifest"`
	Review             candidateAuthoritySource `json:"review"`
}

type candidateAuthorityInput struct {
	Candidate attemptCandidate `json:"candidate"`
}

type candidateAuthoritySource struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type candidateRebindRecord struct {
	Status             string `json:"status"`
	Manifest           string `json:"manifest"`
	TerraReview        string `json:"terraReview"`
	NewCandidateCommit string `json:"newCandidateCommit"`
	NewCandidateSHA256 string `json:"newCandidateSha256"`
	CandidatePath      string `json:"candidatePath"`
	BuildReceipt       string `json:"buildReceipt"`
}

type candidateBuildReceipt struct {
	SchemaVersion int    `json:"schemaVersion"`
	Status        string `json:"status"`
	SourceCommit  string `json:"sourceCommit"`
	BinarySHA256  string `json:"binarySha256"`
	CleanWorktree bool   `json:"cleanWorktree"`
}

// CreateCandidateAuthority derives the one executable candidate permitted for
// an assurance attempt from the guarded current-base rebind record.
func CreateCandidateAuthority(request CandidateAuthorityRequest) (attemptCandidate, error) {
	for _, path := range []string{request.RunRoot, request.RebindPath, request.OutputPath} {
		if !filepath.IsAbs(path) {
			return attemptCandidate{}, fmt.Errorf("absolute candidate authority paths are required")
		}
	}
	if _, err := os.Lstat(request.OutputPath); err == nil {
		return attemptCandidate{}, fmt.Errorf("candidate authority output already exists: %s", request.OutputPath)
	} else if !os.IsNotExist(err) {
		return attemptCandidate{}, err
	}
	runRoot, err := filepath.EvalSymlinks(request.RunRoot)
	if err != nil {
		return attemptCandidate{}, err
	}
	rebindPath, err := filepath.EvalSymlinks(request.RebindPath)
	if err != nil || rebindPath != filepath.Join(runRoot, "evidence", "current-base", "current-base-candidate-rebind.json") {
		return attemptCandidate{}, fmt.Errorf("candidate authority rebind path is not canonical")
	}
	candidate, rebindSource, receiptSource, manifestSource, reviewSource, err := validateCandidateRebind(runRoot, rebindPath)
	if err != nil {
		return attemptCandidate{}, err
	}
	if err := validateCleanGitRoot(filepath.Join(runRoot, "integration", "glade"), candidate.Commit); err != nil {
		return attemptCandidate{}, fmt.Errorf("candidate source: %w", err)
	}
	document := candidateAuthorityDocument{
		SchemaVersion:      1,
		Status:             candidateAuthorityStatus,
		Binding:            candidateAuthorityInput{Candidate: candidate},
		BoundInputs:        candidateAuthorityInput{Candidate: candidate},
		CandidateRebind:    rebindSource,
		SourceBuildReceipt: receiptSource,
		SuccessorManifest:  manifestSource,
		Review:             reviewSource,
	}
	if err := WriteNewJSON(request.OutputPath, document); err != nil {
		return attemptCandidate{}, err
	}
	return candidate, nil
}

func validateCandidateAuthorityDocument(document candidateAuthorityDocument) (attemptCandidate, error) {
	if document.SchemaVersion != 1 || document.Status != candidateAuthorityStatus || document.Binding.Candidate != document.BoundInputs.Candidate {
		return attemptCandidate{}, fmt.Errorf("candidate authority schema is invalid")
	}
	candidate := document.Binding.Candidate
	if !commitPattern.MatchString(candidate.Commit) || !filepath.IsAbs(candidate.Path) || !sha256Pattern.MatchString(candidate.SHA256) {
		return attemptCandidate{}, fmt.Errorf("candidate authority candidates do not agree")
	}
	for _, source := range []candidateAuthoritySource{document.CandidateRebind, document.SourceBuildReceipt, document.SuccessorManifest, document.Review} {
		if !filepath.IsAbs(source.Path) || !sha256Pattern.MatchString(source.SHA256) {
			return attemptCandidate{}, fmt.Errorf("candidate authority source is invalid")
		}
	}
	rebindPath, err := filepath.EvalSymlinks(document.CandidateRebind.Path)
	if err != nil {
		return attemptCandidate{}, fmt.Errorf("candidate authority rebind path is not canonical")
	}
	runRoot := filepath.Dir(filepath.Dir(filepath.Dir(rebindPath)))
	if rebindPath != filepath.Join(runRoot, "evidence", "current-base", "current-base-candidate-rebind.json") {
		return attemptCandidate{}, fmt.Errorf("candidate authority rebind path is not canonical")
	}
	derived, rebindSource, receiptSource, manifestSource, reviewSource, err := validateCandidateRebind(runRoot, rebindPath)
	if err != nil || derived != candidate || rebindSource != document.CandidateRebind || receiptSource != document.SourceBuildReceipt || manifestSource != document.SuccessorManifest || reviewSource != document.Review {
		return attemptCandidate{}, fmt.Errorf("candidate authority does not match guarded rebind")
	}
	return candidate, nil
}

func validateCandidateRebind(runRoot, rebindPath string) (attemptCandidate, candidateAuthoritySource, candidateAuthoritySource, candidateAuthoritySource, candidateAuthoritySource, error) {
	rebind, rebindBytes, err := readExactCandidateRebind(rebindPath)
	if err != nil {
		return attemptCandidate{}, candidateAuthoritySource{}, candidateAuthoritySource{}, candidateAuthoritySource{}, candidateAuthoritySource{}, err
	}
	if rebind.Status != "PASS" || !commitPattern.MatchString(rebind.NewCandidateCommit) || !sha256Pattern.MatchString(rebind.NewCandidateSHA256) {
		return attemptCandidate{}, candidateAuthoritySource{}, candidateAuthoritySource{}, candidateAuthoritySource{}, candidateAuthoritySource{}, fmt.Errorf("candidate rebind is invalid")
	}
	candidatePath, err := candidateAuthorityRunPath(runRoot, rebind.CandidatePath)
	if err != nil {
		return attemptCandidate{}, candidateAuthoritySource{}, candidateAuthoritySource{}, candidateAuthoritySource{}, candidateAuthoritySource{}, err
	}
	receiptPath, err := candidateAuthorityRunPath(runRoot, rebind.BuildReceipt)
	if err != nil {
		return attemptCandidate{}, candidateAuthoritySource{}, candidateAuthoritySource{}, candidateAuthoritySource{}, candidateAuthoritySource{}, err
	}
	manifestPath, err := candidateAuthorityRunPath(runRoot, rebind.Manifest)
	if err != nil {
		return attemptCandidate{}, candidateAuthoritySource{}, candidateAuthoritySource{}, candidateAuthoritySource{}, candidateAuthoritySource{}, err
	}
	reviewPath, err := candidateAuthorityRunPath(runRoot, rebind.TerraReview)
	if err != nil {
		return attemptCandidate{}, candidateAuthoritySource{}, candidateAuthoritySource{}, candidateAuthoritySource{}, candidateAuthoritySource{}, err
	}
	candidateHash, err := sha256FileDirect(candidatePath)
	if err != nil || candidateHash != rebind.NewCandidateSHA256 {
		return attemptCandidate{}, candidateAuthoritySource{}, candidateAuthoritySource{}, candidateAuthoritySource{}, candidateAuthoritySource{}, fmt.Errorf("candidate rebind binary hash is stale")
	}
	receipt, receiptBytes, err := readExactCandidateBuildReceipt(receiptPath)
	if err != nil || receipt.SchemaVersion != 1 || receipt.Status != "clean-exact-candidate" || receipt.SourceCommit != rebind.NewCandidateCommit || receipt.BinarySHA256 != candidateHash || !receipt.CleanWorktree {
		return attemptCandidate{}, candidateAuthoritySource{}, candidateAuthoritySource{}, candidateAuthoritySource{}, candidateAuthoritySource{}, fmt.Errorf("candidate rebind build receipt is invalid")
	}
	if err := validateCandidateAuthorityManifest(manifestPath, rebind.NewCandidateCommit, rebind.CandidatePath, candidateHash); err != nil {
		return attemptCandidate{}, candidateAuthoritySource{}, candidateAuthoritySource{}, candidateAuthoritySource{}, candidateAuthoritySource{}, err
	}
	if err := validateCandidateAuthorityReview(reviewPath, rebind.NewCandidateCommit, candidateHash); err != nil {
		return attemptCandidate{}, candidateAuthoritySource{}, candidateAuthoritySource{}, candidateAuthoritySource{}, candidateAuthoritySource{}, err
	}
	if err := validateCandidateAuthorityFrozenState(runRoot, rebind.NewCandidateCommit, candidateHash); err != nil {
		return attemptCandidate{}, candidateAuthoritySource{}, candidateAuthoritySource{}, candidateAuthoritySource{}, candidateAuthoritySource{}, err
	}
	manifestHash, err := sha256FileDirect(manifestPath)
	if err != nil {
		return attemptCandidate{}, candidateAuthoritySource{}, candidateAuthoritySource{}, candidateAuthoritySource{}, candidateAuthoritySource{}, err
	}
	reviewHash, err := sha256FileDirect(reviewPath)
	if err != nil {
		return attemptCandidate{}, candidateAuthoritySource{}, candidateAuthoritySource{}, candidateAuthoritySource{}, candidateAuthoritySource{}, err
	}
	return attemptCandidate{Commit: rebind.NewCandidateCommit, Path: candidatePath, SHA256: candidateHash}, candidateAuthoritySource{Path: rebindPath, SHA256: replayBytesSHA256(rebindBytes)}, candidateAuthoritySource{Path: receiptPath, SHA256: replayBytesSHA256(receiptBytes)}, candidateAuthoritySource{Path: manifestPath, SHA256: manifestHash}, candidateAuthoritySource{Path: reviewPath, SHA256: reviewHash}, nil
}

func candidateAuthorityRunPath(runRoot, relative string) (string, error) {
	path := filepath.Clean(filepath.FromSlash(relative))
	if relative == "" || filepath.IsAbs(path) || path == "." || path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("candidate rebind artifact path is unsafe")
	}
	resolved := filepath.Join(runRoot, path)
	if actual, err := filepath.EvalSymlinks(resolved); err != nil || !strings.HasPrefix(actual, filepath.Clean(runRoot)+string(filepath.Separator)) {
		return "", fmt.Errorf("candidate rebind artifact escapes run root")
	}
	return resolved, nil
}

func validateCandidateAuthorityManifest(path, commit, candidatePath, candidateSHA string) error {
	var document struct {
		Candidate struct{ Commit, Path, SHA256 string } `json:"candidate"`
	}
	if _, err := readExactCandidateAuthorityJSON(path, &document); err != nil || document.Candidate.Commit != commit || document.Candidate.Path != candidatePath || document.Candidate.SHA256 != candidateSHA {
		return fmt.Errorf("candidate rebind manifest is invalid")
	}
	return nil
}

func validateCandidateAuthorityReview(path, commit, candidateSHA string) error {
	data, err := os.ReadFile(path)
	if err != nil || !strings.HasPrefix(string(data), "Verdict: PASS\n") || !strings.Contains(string(data), "\nCandidate commit: "+commit+"\n") || !strings.Contains(string(data), "\nCandidate SHA-256: "+candidateSHA+"\n") {
		return fmt.Errorf("candidate rebind review is invalid")
	}
	return nil
}

func validateCandidateAuthorityFrozenState(runRoot, commit, candidateSHA string) error {
	var run struct {
		CurrentBase struct {
			Candidate struct{ ProductCommit, SHA256 string } `json:"candidate"`
		} `json:"currentBase"`
	}
	var freeze struct{ CandidateCommit, CandidateSHA256 string }
	if _, err := readExactCandidateAuthorityJSON(filepath.Join(runRoot, "run.json"), &run); err != nil || run.CurrentBase.Candidate.ProductCommit != commit || run.CurrentBase.Candidate.SHA256 != candidateSHA {
		return fmt.Errorf("candidate rebind run state is stale")
	}
	if _, err := readExactCandidateAuthorityJSON(filepath.Join(runRoot, "evidence", "current-base", "review-freeze.json"), &freeze); err != nil || freeze.CandidateCommit != commit || freeze.CandidateSHA256 != candidateSHA {
		return fmt.Errorf("candidate rebind review freeze is stale")
	}
	return nil
}

func readExactCandidateRebind(path string) (candidateRebindRecord, []byte, error) {
	var record candidateRebindRecord
	data, err := readExactCandidateAuthorityJSON(path, &record)
	return record, data, err
}

func readExactCandidateBuildReceipt(path string) (candidateBuildReceipt, []byte, error) {
	var receipt candidateBuildReceipt
	data, err := readExactCandidateAuthorityJSON(path, &receipt)
	return receipt, data, err
}

func readExactCandidateAuthorityJSON(path string, value any) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(value); err != nil {
		return nil, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("candidate authority contains multiple JSON values")
	}
	return data, nil
}
