package corpusassurance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
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
	Tools     candidateTool    `json:"tools"`
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
	Tools         candidateTool    `json:"tools"`
}

type candidateTool struct {
	RuntimeArtifact
	Path string `json:"path"`
}

// CreateCandidateAuthority seals one candidate derived from its exact build
// receipt and independently reviewed candidate identity.
func CreateCandidateAuthority(request CandidateAuthorityRequest) (candidateAuthorityInput, error) {
	for _, path := range []string{request.CandidateRoot, request.ReceiptPath, request.ReviewPath, request.OutputPath} {
		if !filepath.IsAbs(path) {
			return candidateAuthorityInput{}, fmt.Errorf("absolute candidate authority paths are required")
		}
	}
	if _, err := os.Lstat(request.OutputPath); err == nil {
		return candidateAuthorityInput{}, fmt.Errorf("candidate authority output already exists: %s", request.OutputPath)
	} else if !os.IsNotExist(err) {
		return candidateAuthorityInput{}, err
	}
	input, receiptSource, reviewSource, err := validateCandidateAuthoritySources(request.CandidateRoot, request.ReceiptPath, request.ReviewPath)
	if err != nil {
		return candidateAuthorityInput{}, err
	}
	document := candidateAuthorityDocument{SchemaVersion: 1, Status: candidateAuthorityStatus, Binding: input, BoundInputs: input, SourceBuildReceipt: receiptSource, Review: reviewSource}
	if err := WriteNewJSON(request.OutputPath, document); err != nil {
		return candidateAuthorityInput{}, err
	}
	return input, nil
}

func validateCandidateAuthorityDocument(document candidateAuthorityDocument) (candidateAuthorityInput, error) {
	if document.SchemaVersion != 1 || document.Status != candidateAuthorityStatus || document.Binding.Candidate != document.BoundInputs.Candidate {
		return candidateAuthorityInput{}, fmt.Errorf("candidate authority schema is invalid")
	}
	input := document.Binding
	if input != document.BoundInputs {
		return candidateAuthorityInput{}, fmt.Errorf("candidate authority schema is invalid")
	}
	candidate := input.Candidate
	if !commitPattern.MatchString(candidate.Commit) || !filepath.IsAbs(candidate.Path) || !sha256Pattern.MatchString(candidate.SHA256) {
		return candidateAuthorityInput{}, fmt.Errorf("candidate authority candidates do not agree")
	}
	if err := validateCandidateTool(input.Tools); err != nil {
		return candidateAuthorityInput{}, fmt.Errorf("candidate authority tools are invalid")
	}
	for _, source := range []candidateAuthoritySource{document.SourceBuildReceipt, document.Review} {
		if !filepath.IsAbs(source.Path) || !sha256Pattern.MatchString(source.SHA256) {
			return candidateAuthorityInput{}, fmt.Errorf("candidate authority source is invalid")
		}
	}
	receipt, receiptBytes, err := readExactCandidateBuildReceipt(document.SourceBuildReceipt.Path)
	if err != nil || replayBytesSHA256(receiptBytes) != document.SourceBuildReceipt.SHA256 || !validCandidateBuildReceipt(receipt, input) {
		return candidateAuthorityInput{}, fmt.Errorf("candidate authority build receipt is invalid")
	}
	reviewBytes, err := os.ReadFile(document.Review.Path)
	if err != nil || validateCandidateAuthorityReviewBytes(reviewBytes, candidate, receipt.Tools) != nil || replayBytesSHA256(reviewBytes) != document.Review.SHA256 {
		return candidateAuthorityInput{}, fmt.Errorf("candidate authority review is stale")
	}
	actual, err := runtimeArtifactFor(candidate.Path, candidate.Commit)
	if err != nil || actual.SHA256 != candidate.SHA256 {
		return candidateAuthorityInput{}, fmt.Errorf("candidate authority binary is stale")
	}
	if err := validateCandidateTool(receipt.Tools); err != nil {
		return candidateAuthorityInput{}, fmt.Errorf("candidate authority tools are stale")
	}
	return input, nil
}

func validateCandidateAuthoritySources(candidateRoot, receiptPath, reviewPath string) (candidateAuthorityInput, candidateAuthoritySource, candidateAuthoritySource, error) {
	receipt, receiptBytes, err := readExactCandidateBuildReceipt(receiptPath)
	input := candidateAuthorityInput{Candidate: receipt.Candidate, Tools: receipt.Tools}
	if err != nil || !validCandidateBuildReceipt(receipt, input) {
		return candidateAuthorityInput{}, candidateAuthoritySource{}, candidateAuthoritySource{}, fmt.Errorf("candidate authority build receipt is invalid")
	}
	if err := validateCleanGitRoot(candidateRoot, receipt.Candidate.Commit); err != nil {
		return candidateAuthorityInput{}, candidateAuthoritySource{}, candidateAuthoritySource{}, fmt.Errorf("candidate source: %w", err)
	}
	actual, err := runtimeArtifactFor(receipt.Candidate.Path, receipt.Candidate.Commit)
	if err != nil || actual.SHA256 != receipt.Candidate.SHA256 {
		return candidateAuthorityInput{}, candidateAuthoritySource{}, candidateAuthoritySource{}, fmt.Errorf("candidate authority binary is stale")
	}
	if err := validateCandidateTool(receipt.Tools); err != nil {
		return candidateAuthorityInput{}, candidateAuthoritySource{}, candidateAuthoritySource{}, fmt.Errorf("candidate authority tools are stale")
	}
	if err := validateCandidateParser(receipt.Candidate, candidateRoot); err != nil {
		return candidateAuthorityInput{}, candidateAuthoritySource{}, candidateAuthoritySource{}, err
	}
	reviewBytes, err := os.ReadFile(reviewPath)
	if err != nil {
		return candidateAuthorityInput{}, candidateAuthoritySource{}, candidateAuthoritySource{}, err
	}
	if err := validateCandidateAuthorityReviewBytes(reviewBytes, receipt.Candidate, receipt.Tools); err != nil {
		return candidateAuthorityInput{}, candidateAuthoritySource{}, candidateAuthoritySource{}, err
	}
	return input, candidateAuthoritySource{Path: receiptPath, SHA256: replayBytesSHA256(receiptBytes)}, candidateAuthoritySource{Path: reviewPath, SHA256: replayBytesSHA256(reviewBytes)}, nil
}

func validateCandidateParser(candidate attemptCandidate, workingDirectory string) error {
	result, stdout, _ := runReplayCommandOutput(workingDirectory, ReplayCommand{Path: candidate.Path, Args: []string{"doctor", "--json"}, Env: append([]string(nil), fixedReplayEnvironment...), Timeout: 30 * time.Second})
	if result.TimedOut || result.ExecutableSHA256 != candidate.SHA256 || result.ExecutableAfterSHA256 != candidate.SHA256 || !validCandidateDoctorJSON(stdout) {
		return fmt.Errorf("candidate Apex parser is unavailable")
	}
	return nil
}

func validCandidateDoctorJSON(data []byte) bool {
	var doctor struct {
		Command  string `json:"command"`
		ParserOK bool   `json:"parserOK"`
	}
	return validateJSONWithoutDuplicateKeys(data) == nil && json.Unmarshal(data, &doctor) == nil && doctor.Command == "doctor" && doctor.ParserOK
}

func validCandidateBuildReceipt(receipt candidateBuildReceipt, input candidateAuthorityInput) bool {
	candidate := input.Candidate
	return receipt.SchemaVersion == 1 && receipt.Status == "clean-exact-candidate" && receipt.CleanWorktree && receipt.SourceCommit == candidate.Commit && receipt.BinarySHA256 == candidate.SHA256 && receipt.Candidate == candidate && receipt.Tools == input.Tools && commitPattern.MatchString(candidate.Commit) && filepath.IsAbs(candidate.Path) && sha256Pattern.MatchString(candidate.SHA256) && validateCandidateTool(receipt.Tools) == nil
}

func validateCandidateTool(tool candidateTool) error {
	if !filepath.IsAbs(tool.Path) || ValidateRuntimeArtifact(tool.RuntimeArtifact) != nil {
		return fmt.Errorf("candidate tools are invalid")
	}
	canonical, err := canonicalCandidateToolPath(tool.Path)
	if err != nil || canonical != filepath.Clean(tool.Path) {
		return fmt.Errorf("candidate tools path is not canonical")
	}
	actual, err := releaseExecutingTools(tool.Path, tool.Commit)
	if err != nil || actual != tool.RuntimeArtifact {
		return fmt.Errorf("candidate tools are stale")
	}
	return nil
}

func canonicalCandidateToolPath(path string) (string, error) {
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil || !filepath.IsAbs(canonical) {
		return "", fmt.Errorf("resolve candidate tools path")
	}
	return filepath.Clean(canonical), nil
}

func validateCandidateAuthorityReviewBytes(data []byte, candidate attemptCandidate, tools candidateTool) error {
	if !strings.HasPrefix(string(data), "Verdict: PASS\n") {
		return fmt.Errorf("candidate authority review is invalid")
	}
	expected := map[string]string{"Verdict": "PASS", "Candidate commit": candidate.Commit, "Candidate SHA-256": candidate.SHA256, "Tools commit": tools.Commit, "Tools OS": tools.OS, "Tools arch": tools.Arch, "Tools SHA-256": tools.SHA256, "Tools path": tools.Path}
	seen := make(map[string]bool, len(expected))
	for _, line := range strings.Split(string(data), "\n") {
		label, value, ok := strings.Cut(line, ": ")
		if !ok {
			continue
		}
		want, required := expected[label]
		if !required {
			continue
		}
		if seen[label] || value != want {
			return fmt.Errorf("candidate authority review is invalid")
		}
		seen[label] = true
	}
	for label := range expected {
		if !seen[label] {
			return fmt.Errorf("candidate authority review is invalid")
		}
	}
	return nil
}

func readExactCandidateBuildReceipt(path string) (candidateBuildReceipt, []byte, error) {
	var receipt candidateBuildReceipt
	data, err := readExactCandidateAuthorityJSON(path, &receipt)
	return receipt, data, err
}

func readExactCandidateAuthorityDocument(path string, value any) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := decodeExactJSON(data, value); err != nil {
		return nil, err
	}
	return data, nil
}

func readExactCandidateAuthorityJSON(path string, value any) ([]byte, error) {
	return readExactCandidateAuthorityDocument(path, value)
}
