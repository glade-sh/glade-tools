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

const candidateAuthorityStatus = "sealed-candidate-authority"

type CandidateAuthorityRequest struct {
	CandidateRoot string
	ReceiptPath   string
	ReviewPath    string
	OutputPath    string
}

type candidateAuthorityDocument struct {
	SchemaVersion      int                      `json:"schemaVersion"`
	Status             string                   `json:"status"`
	Binding            candidateAuthorityInput  `json:"binding"`
	BoundInputs        candidateAuthorityInput  `json:"boundInputs"`
	SourceBuildReceipt candidateAuthoritySource `json:"sourceBuildReceipt"`
	Review             candidateAuthoritySource `json:"review"`
}

type candidateAuthorityInput struct {
	Candidate attemptCandidate `json:"candidate"`
}

type candidateAuthoritySource struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type candidateBuildReceipt struct {
	SchemaVersion int              `json:"schemaVersion"`
	Status        string           `json:"status"`
	SourceCommit  string           `json:"sourceCommit"`
	BinarySHA256  string           `json:"binarySha256"`
	CleanWorktree bool             `json:"cleanWorktree"`
	Candidate     attemptCandidate `json:"candidate"`
}

// CreateCandidateAuthority seals one candidate derived from its exact build
// receipt and independently reviewed candidate identity.
func CreateCandidateAuthority(request CandidateAuthorityRequest) (attemptCandidate, error) {
	for _, path := range []string{request.CandidateRoot, request.ReceiptPath, request.ReviewPath, request.OutputPath} {
		if !filepath.IsAbs(path) {
			return attemptCandidate{}, fmt.Errorf("absolute candidate authority paths are required")
		}
	}
	if _, err := os.Lstat(request.OutputPath); err == nil {
		return attemptCandidate{}, fmt.Errorf("candidate authority output already exists: %s", request.OutputPath)
	} else if !os.IsNotExist(err) {
		return attemptCandidate{}, err
	}
	candidate, receiptSource, reviewSource, err := validateCandidateAuthoritySources(request.CandidateRoot, request.ReceiptPath, request.ReviewPath)
	if err != nil {
		return attemptCandidate{}, err
	}
	document := candidateAuthorityDocument{SchemaVersion: 1, Status: candidateAuthorityStatus, Binding: candidateAuthorityInput{Candidate: candidate}, BoundInputs: candidateAuthorityInput{Candidate: candidate}, SourceBuildReceipt: receiptSource, Review: reviewSource}
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
	for _, source := range []candidateAuthoritySource{document.SourceBuildReceipt, document.Review} {
		if !filepath.IsAbs(source.Path) || !sha256Pattern.MatchString(source.SHA256) {
			return attemptCandidate{}, fmt.Errorf("candidate authority source is invalid")
		}
	}
	receipt, receiptBytes, err := readExactCandidateBuildReceipt(document.SourceBuildReceipt.Path)
	if err != nil || replayBytesSHA256(receiptBytes) != document.SourceBuildReceipt.SHA256 || !validCandidateBuildReceipt(receipt, candidate) {
		return attemptCandidate{}, fmt.Errorf("candidate authority build receipt is invalid")
	}
	if err := validateCandidateAuthorityReview(document.Review.Path, candidate); err != nil {
		return attemptCandidate{}, err
	}
	reviewBytes, err := os.ReadFile(document.Review.Path)
	if err != nil || replayBytesSHA256(reviewBytes) != document.Review.SHA256 {
		return attemptCandidate{}, fmt.Errorf("candidate authority review is stale")
	}
	actual, err := runtimeArtifactFor(candidate.Path, candidate.Commit)
	if err != nil || actual.SHA256 != candidate.SHA256 {
		return attemptCandidate{}, fmt.Errorf("candidate authority binary is stale")
	}
	return candidate, nil
}

func validateCandidateAuthoritySources(candidateRoot, receiptPath, reviewPath string) (attemptCandidate, candidateAuthoritySource, candidateAuthoritySource, error) {
	receipt, receiptBytes, err := readExactCandidateBuildReceipt(receiptPath)
	if err != nil || !validCandidateBuildReceipt(receipt, receipt.Candidate) {
		return attemptCandidate{}, candidateAuthoritySource{}, candidateAuthoritySource{}, fmt.Errorf("candidate authority build receipt is invalid")
	}
	if err := validateCleanGitRoot(candidateRoot, receipt.Candidate.Commit); err != nil {
		return attemptCandidate{}, candidateAuthoritySource{}, candidateAuthoritySource{}, fmt.Errorf("candidate source: %w", err)
	}
	actual, err := runtimeArtifactFor(receipt.Candidate.Path, receipt.Candidate.Commit)
	if err != nil || actual.SHA256 != receipt.Candidate.SHA256 {
		return attemptCandidate{}, candidateAuthoritySource{}, candidateAuthoritySource{}, fmt.Errorf("candidate authority binary is stale")
	}
	if err := validateCandidateAuthorityReview(reviewPath, receipt.Candidate); err != nil {
		return attemptCandidate{}, candidateAuthoritySource{}, candidateAuthoritySource{}, err
	}
	reviewBytes, err := os.ReadFile(reviewPath)
	if err != nil {
		return attemptCandidate{}, candidateAuthoritySource{}, candidateAuthoritySource{}, err
	}
	return receipt.Candidate, candidateAuthoritySource{Path: receiptPath, SHA256: replayBytesSHA256(receiptBytes)}, candidateAuthoritySource{Path: reviewPath, SHA256: replayBytesSHA256(reviewBytes)}, nil
}

func validCandidateBuildReceipt(receipt candidateBuildReceipt, candidate attemptCandidate) bool {
	return receipt.SchemaVersion == 1 && receipt.Status == "clean-exact-candidate" && receipt.CleanWorktree && receipt.SourceCommit == candidate.Commit && receipt.BinarySHA256 == candidate.SHA256 && receipt.Candidate == candidate && commitPattern.MatchString(candidate.Commit) && filepath.IsAbs(candidate.Path) && sha256Pattern.MatchString(candidate.SHA256)
}

func validateCandidateAuthorityReview(path string, candidate attemptCandidate) error {
	data, err := os.ReadFile(path)
	if err != nil || !strings.HasPrefix(string(data), "Verdict: PASS\n") || !strings.Contains(string(data), "\nCandidate commit: "+candidate.Commit+"\n") || !strings.Contains(string(data), "\nCandidate SHA-256: "+candidate.SHA256+"\n") {
		return fmt.Errorf("candidate authority review is invalid")
	}
	return nil
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
