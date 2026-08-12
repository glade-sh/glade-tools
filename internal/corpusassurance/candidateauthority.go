package corpusassurance

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
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
	Build              candidateBuildBinding    `json:"build"`
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

type candidateBuildBinding struct {
	SourceRoot  string                   `json:"sourceRoot"`
	SourceTree  string                   `json:"sourceTree"`
	Go          candidateAuthoritySource `json:"go"`
	Arguments   []string                 `json:"arguments"`
	Environment []string                 `json:"environment"`
}

var validateSealedCandidateBuild = validateCandidateBuildFromSource

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
	input, build, receiptSource, reviewSource, err := validateCandidateAuthoritySources(request.CandidateRoot, request.ReceiptPath, request.ReviewPath)
	if err != nil {
		return candidateAuthorityInput{}, err
	}
	document := candidateAuthorityDocument{SchemaVersion: 1, Status: candidateAuthorityStatus, Binding: input, BoundInputs: input, Build: build, SourceBuildReceipt: receiptSource, Review: reviewSource}
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
	if err := validateSealedCandidateBuild(document.Build.SourceRoot, candidate, document.Build); err != nil {
		return candidateAuthorityInput{}, fmt.Errorf("candidate authority source build is stale")
	}
	return input, nil
}

func validateCandidateAuthoritySources(candidateRoot, receiptPath, reviewPath string) (candidateAuthorityInput, candidateBuildBinding, candidateAuthoritySource, candidateAuthoritySource, error) {
	receipt, receiptBytes, err := readExactCandidateBuildReceipt(receiptPath)
	input := candidateAuthorityInput{Candidate: receipt.Candidate, Tools: receipt.Tools}
	if err != nil || !validCandidateBuildReceipt(receipt, input) {
		return candidateAuthorityInput{}, candidateBuildBinding{}, candidateAuthoritySource{}, candidateAuthoritySource{}, fmt.Errorf("candidate authority build receipt is invalid")
	}
	if err := validateCleanGitRoot(candidateRoot, receipt.Candidate.Commit); err != nil {
		return candidateAuthorityInput{}, candidateBuildBinding{}, candidateAuthoritySource{}, candidateAuthoritySource{}, fmt.Errorf("candidate source: %w", err)
	}
	actual, err := runtimeArtifactFor(receipt.Candidate.Path, receipt.Candidate.Commit)
	if err != nil || actual.SHA256 != receipt.Candidate.SHA256 {
		return candidateAuthorityInput{}, candidateBuildBinding{}, candidateAuthoritySource{}, candidateAuthoritySource{}, fmt.Errorf("candidate authority binary is stale")
	}
	if err := validateCandidateTool(receipt.Tools); err != nil {
		return candidateAuthorityInput{}, candidateBuildBinding{}, candidateAuthoritySource{}, candidateAuthoritySource{}, fmt.Errorf("candidate authority tools are stale")
	}
	build, err := deriveCandidateBuildBinding(candidateRoot)
	if err != nil || validateSealedCandidateBuild(candidateRoot, receipt.Candidate, build) != nil {
		return candidateAuthorityInput{}, candidateBuildBinding{}, candidateAuthoritySource{}, candidateAuthoritySource{}, fmt.Errorf("candidate authority source build is invalid")
	}
	if err := validateCandidateParser(receipt.Candidate, candidateRoot); err != nil {
		return candidateAuthorityInput{}, candidateBuildBinding{}, candidateAuthoritySource{}, candidateAuthoritySource{}, err
	}
	reviewBytes, err := os.ReadFile(reviewPath)
	if err != nil {
		return candidateAuthorityInput{}, candidateBuildBinding{}, candidateAuthoritySource{}, candidateAuthoritySource{}, err
	}
	if err := validateCandidateAuthorityReviewBytes(reviewBytes, receipt.Candidate, receipt.Tools); err != nil {
		return candidateAuthorityInput{}, candidateBuildBinding{}, candidateAuthoritySource{}, candidateAuthoritySource{}, err
	}
	return input, build, candidateAuthoritySource{Path: receiptPath, SHA256: replayBytesSHA256(receiptBytes)}, candidateAuthoritySource{Path: reviewPath, SHA256: replayBytesSHA256(reviewBytes)}, nil
}

func deriveCandidateBuildBinding(sourceRoot string) (candidateBuildBinding, error) {
	canonical, err := filepath.EvalSymlinks(sourceRoot)
	if err != nil || !filepath.IsAbs(canonical) {
		return candidateBuildBinding{}, fmt.Errorf("candidate source root is unavailable")
	}
	top, err := gitOutput(canonical, "rev-parse", "--show-toplevel")
	if err != nil {
		return candidateBuildBinding{}, err
	}
	top, err = filepath.EvalSymlinks(top)
	if err != nil || filepath.Clean(top) != filepath.Clean(canonical) {
		return candidateBuildBinding{}, fmt.Errorf("candidate source root is not the Git root")
	}
	tree, err := gitOutput(canonical, "rev-parse", "HEAD^{tree}")
	if err != nil || !commitPattern.MatchString(tree) {
		return candidateBuildBinding{}, fmt.Errorf("candidate source tree is unavailable")
	}
	environment := append(fixedReleaseEnvironment(), "CGO_ENABLED=1")
	goPath, err := fixedReleaseGoBinary(environment)
	if err != nil {
		return candidateBuildBinding{}, err
	}
	goSHA256, err := sha256File(goPath)
	if err != nil {
		return candidateBuildBinding{}, err
	}
	return candidateBuildBinding{
		SourceRoot:  canonical,
		SourceTree:  tree,
		Go:          candidateAuthoritySource{Path: goPath, SHA256: goSHA256},
		Arguments:   []string{"build", "-trimpath", "-o", "<candidate>", "./cmd/glade"},
		Environment: environment,
	}, nil
}

func validateCandidateBuildBinding(sourceRoot string, _ attemptCandidate, binding candidateBuildBinding) error {
	derived, err := deriveCandidateBuildBinding(sourceRoot)
	if err != nil || !reflect.DeepEqual(binding, derived) {
		return fmt.Errorf("candidate build binding does not match sealed source")
	}
	return nil
}

func validateCandidateBuildFromSource(sourceRoot string, candidate attemptCandidate, binding candidateBuildBinding) error {
	if err := validateCandidateBuildBinding(sourceRoot, candidate, binding); err != nil {
		return err
	}
	before, err := runtimeArtifactFor(candidate.Path, candidate.Commit)
	if err != nil || before.SHA256 != candidate.SHA256 {
		return fmt.Errorf("candidate binary is stale")
	}
	root, err := os.MkdirTemp("", "glade-candidate-rebuild-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	rebuilt := filepath.Join(root, "glade")
	if err := runBoundCandidateBuild(binding, rebuilt); err != nil {
		return err
	}
	rebuiltSHA256, err := sha256File(rebuilt)
	if err != nil || rebuiltSHA256 != candidate.SHA256 {
		return fmt.Errorf("candidate binary is not reproducible from sealed source")
	}
	after, err := runtimeArtifactFor(candidate.Path, candidate.Commit)
	if err != nil || after != before || validateCleanGitRoot(binding.SourceRoot, candidate.Commit) != nil {
		return fmt.Errorf("candidate source or binary changed during build validation")
	}
	tree, err := gitOutput(binding.SourceRoot, "rev-parse", "HEAD^{tree}")
	if err != nil || tree != binding.SourceTree {
		return fmt.Errorf("candidate source tree changed during build validation")
	}
	return nil
}

func runBoundCandidateBuild(binding candidateBuildBinding, outputPath string) error {
	if !filepath.IsAbs(outputPath) || len(binding.Arguments) != 5 || binding.Arguments[3] != "<candidate>" {
		return fmt.Errorf("candidate build output is invalid")
	}
	before, err := sha256File(binding.Go.Path)
	if err != nil || before != binding.Go.SHA256 {
		return fmt.Errorf("candidate Go executable is stale")
	}
	for _, value := range binding.Environment {
		name, path, ok := strings.Cut(value, "=")
		if ok && (name == "HOME" || name == "GOCACHE" || name == "GOMODCACHE") {
			if err := os.MkdirAll(path, 0o700); err != nil {
				return err
			}
		}
	}
	arguments := append([]string(nil), binding.Arguments...)
	arguments[3] = outputPath
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	var stderr bytes.Buffer
	command := exec.CommandContext(ctx, binding.Go.Path, arguments...)
	command.Dir, command.Env, command.Stderr = binding.SourceRoot, append([]string(nil), binding.Environment...), &stderr
	if err := command.Run(); err != nil || ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("candidate source build failed: %w", err)
	}
	after, err := sha256File(binding.Go.Path)
	if err != nil || after != before {
		return fmt.Errorf("candidate Go executable changed during build")
	}
	return nil
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
