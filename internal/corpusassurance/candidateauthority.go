package corpusassurance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"golang.org/x/mod/modfile"
)

const candidateAuthorityStatus = "sealed-candidate-authority"

type CandidateAuthorityRequest struct {
	CandidateRoot string
	ToolsRoot     string
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
	ToolsBuild         candidateBuildBinding    `json:"toolsBuild"`
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
	SchemaVersion      int              `json:"schemaVersion"`
	Status             string           `json:"status"`
	SourceCommit       string           `json:"sourceCommit"`
	BinarySHA256       string           `json:"binarySha256"`
	CleanWorktree      bool             `json:"cleanWorktree"`
	CandidateRef       string           `json:"candidateRef,omitempty"`
	CandidateRefCommit string           `json:"candidateRefCommit,omitempty"`
	ToolsRef           string           `json:"toolsRef,omitempty"`
	ToolsRefCommit     string           `json:"toolsRefCommit,omitempty"`
	Candidate          attemptCandidate `json:"candidate"`
	Tools              candidateTool    `json:"tools"`
}

type candidateBuildBinding struct {
	SourceRoot  string                   `json:"sourceRoot"`
	SourceTree  string                   `json:"sourceTree"`
	Go          candidateAuthoritySource `json:"go"`
	Arguments   []string                 `json:"arguments"`
	Environment []string                 `json:"environment"`
}

var validateSealedCandidateBuild = validateCandidateBuildFromSource
var validateSealedToolsBuild = validateToolsBuildFromSource

type candidateTool struct {
	RuntimeArtifact
	Path string `json:"path"`
}

// CreateCandidateAuthority seals one candidate derived from its exact build
// receipt and independently reviewed candidate identity.
func CreateCandidateAuthority(request CandidateAuthorityRequest) (candidateAuthorityInput, error) {
	for _, path := range []string{request.CandidateRoot, request.ToolsRoot, request.ReceiptPath, request.ReviewPath, request.OutputPath} {
		if !filepath.IsAbs(path) {
			return candidateAuthorityInput{}, fmt.Errorf("absolute candidate authority paths are required")
		}
	}
	if _, err := os.Lstat(request.OutputPath); err == nil {
		return candidateAuthorityInput{}, fmt.Errorf("candidate authority output already exists: %s", request.OutputPath)
	} else if !os.IsNotExist(err) {
		return candidateAuthorityInput{}, err
	}
	input, build, toolsBuild, receiptSource, reviewSource, err := validateCandidateAuthoritySources(request.CandidateRoot, request.ToolsRoot, request.ReceiptPath, request.ReviewPath)
	if err != nil {
		return candidateAuthorityInput{}, err
	}
	document := candidateAuthorityDocument{SchemaVersion: 2, Status: candidateAuthorityStatus, Binding: input, BoundInputs: input, Build: build, ToolsBuild: toolsBuild, SourceBuildReceipt: receiptSource, Review: reviewSource}
	if err := WriteNewJSON(request.OutputPath, document); err != nil {
		return candidateAuthorityInput{}, err
	}
	return input, nil
}

func validateCandidateAuthorityDocument(document candidateAuthorityDocument) (candidateAuthorityInput, error) {
	if document.SchemaVersion != 2 || document.Status != candidateAuthorityStatus || document.Binding.Candidate != document.BoundInputs.Candidate {
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
	if err != nil || validateCandidateAuthorityReviewForReceipt(reviewBytes, candidate, receipt.Tools, receipt) != nil || replayBytesSHA256(reviewBytes) != document.Review.SHA256 {
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
	if err := validateSealedToolsBuild(document.ToolsBuild.SourceRoot, input.Tools, document.ToolsBuild); err != nil {
		return candidateAuthorityInput{}, fmt.Errorf("candidate authority tools source build is stale")
	}
	if err := validateToolsCandidatePair(document.Build.SourceRoot, document.ToolsBuild); err != nil {
		return candidateAuthorityInput{}, fmt.Errorf("candidate authority source pairing is stale")
	}
	return input, nil
}

func validateCandidateAuthoritySources(candidateRoot, toolsRoot, receiptPath, reviewPath string) (candidateAuthorityInput, candidateBuildBinding, candidateBuildBinding, candidateAuthoritySource, candidateAuthoritySource, error) {
	receipt, receiptBytes, err := readExactCandidateBuildReceipt(receiptPath)
	input := candidateAuthorityInput{Candidate: receipt.Candidate, Tools: receipt.Tools}
	if err != nil || !validCandidateBuildReceipt(receipt, input) {
		return candidateAuthorityInput{}, candidateBuildBinding{}, candidateBuildBinding{}, candidateAuthoritySource{}, candidateAuthoritySource{}, fmt.Errorf("candidate authority build receipt is invalid")
	}
	if receipt.SchemaVersion != 2 {
		return candidateAuthorityInput{}, candidateBuildBinding{}, candidateBuildBinding{}, candidateAuthoritySource{}, candidateAuthoritySource{}, fmt.Errorf("new candidate authorities require schema version 2 build receipts")
	}
	if err := validateCleanGitRoot(candidateRoot, receipt.Candidate.Commit); err != nil {
		return candidateAuthorityInput{}, candidateBuildBinding{}, candidateBuildBinding{}, candidateAuthoritySource{}, candidateAuthoritySource{}, fmt.Errorf("candidate source: %w", err)
	}
	if err := validateAdvertisedBuildRef(candidateRoot, receipt.CandidateRef, receipt.CandidateRefCommit); err != nil {
		return candidateAuthorityInput{}, candidateBuildBinding{}, candidateBuildBinding{}, candidateAuthoritySource{}, candidateAuthoritySource{}, fmt.Errorf("candidate advertised ref: %w", err)
	}
	actual, err := runtimeArtifactFor(receipt.Candidate.Path, receipt.Candidate.Commit)
	if err != nil || actual.SHA256 != receipt.Candidate.SHA256 {
		return candidateAuthorityInput{}, candidateBuildBinding{}, candidateBuildBinding{}, candidateAuthoritySource{}, candidateAuthoritySource{}, fmt.Errorf("candidate authority binary is stale")
	}
	if err := validateCandidateTool(receipt.Tools); err != nil {
		return candidateAuthorityInput{}, candidateBuildBinding{}, candidateBuildBinding{}, candidateAuthoritySource{}, candidateAuthoritySource{}, fmt.Errorf("candidate authority tools are stale")
	}
	if err := validateAdvertisedBuildRef(toolsRoot, receipt.ToolsRef, receipt.ToolsRefCommit); err != nil {
		return candidateAuthorityInput{}, candidateBuildBinding{}, candidateBuildBinding{}, candidateAuthoritySource{}, candidateAuthoritySource{}, fmt.Errorf("tools advertised ref: %w", err)
	}
	build, err := deriveCandidateBuildBinding(candidateRoot)
	if err != nil || validateSealedCandidateBuild(candidateRoot, receipt.Candidate, build) != nil {
		return candidateAuthorityInput{}, candidateBuildBinding{}, candidateBuildBinding{}, candidateAuthoritySource{}, candidateAuthoritySource{}, fmt.Errorf("candidate authority source build is invalid")
	}
	toolsBuild, err := deriveToolsBuildBinding(toolsRoot)
	if err != nil || validateSealedToolsBuild(toolsRoot, receipt.Tools, toolsBuild) != nil {
		return candidateAuthorityInput{}, candidateBuildBinding{}, candidateBuildBinding{}, candidateAuthoritySource{}, candidateAuthoritySource{}, fmt.Errorf("candidate authority tools source build is invalid")
	}
	if err := validateToolsCandidatePair(candidateRoot, toolsBuild); err != nil {
		return candidateAuthorityInput{}, candidateBuildBinding{}, candidateBuildBinding{}, candidateAuthoritySource{}, candidateAuthoritySource{}, fmt.Errorf("candidate authority source pairing is invalid")
	}
	if err := validateCandidateParser(receipt.Candidate, candidateRoot); err != nil {
		return candidateAuthorityInput{}, candidateBuildBinding{}, candidateBuildBinding{}, candidateAuthoritySource{}, candidateAuthoritySource{}, err
	}
	reviewBytes, err := os.ReadFile(reviewPath)
	if err != nil {
		return candidateAuthorityInput{}, candidateBuildBinding{}, candidateBuildBinding{}, candidateAuthoritySource{}, candidateAuthoritySource{}, err
	}
	if err := validateCandidateAuthorityReviewForReceipt(reviewBytes, receipt.Candidate, receipt.Tools, receipt); err != nil {
		return candidateAuthorityInput{}, candidateBuildBinding{}, candidateBuildBinding{}, candidateAuthoritySource{}, candidateAuthoritySource{}, err
	}
	return input, build, toolsBuild, candidateAuthoritySource{Path: receiptPath, SHA256: replayBytesSHA256(receiptBytes)}, candidateAuthoritySource{Path: reviewPath, SHA256: replayBytesSHA256(reviewBytes)}, nil
}

func deriveCandidateBuildBinding(sourceRoot string) (candidateBuildBinding, error) {
	if err := validateCandidateLocalReplacements(sourceRoot); err != nil {
		return candidateBuildBinding{}, fmt.Errorf("candidate local replacements are invalid: %w", err)
	}
	return deriveBuildBinding(sourceRoot, "./cmd/glade")
}

func deriveToolsBuildBinding(sourceRoot string) (candidateBuildBinding, error) {
	return deriveBuildBinding(sourceRoot, "./cmd/glade-tools")
}

func deriveBuildBinding(sourceRoot, target string) (candidateBuildBinding, error) {
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
	commit, err := cleanGitHead(canonical)
	if err != nil {
		return candidateBuildBinding{}, err
	}
	tree, err := gitOutput(canonical, "rev-parse", "HEAD^{tree}")
	if err != nil || !commitPattern.MatchString(tree) {
		return candidateBuildBinding{}, fmt.Errorf("candidate source tree is unavailable")
	}
	environment := append(fixedCandidateBuildEnvironment(commit), "CGO_ENABLED=1")
	goPath, err := fixedReleaseGoBinary(environment)
	if err != nil {
		return candidateBuildBinding{}, err
	}
	goSHA256, err := sha256File(goPath)
	if err != nil {
		return candidateBuildBinding{}, err
	}
	arguments := []string{"build", "-buildvcs=false", "-trimpath", "-o", "<candidate>", target}
	if target == "./cmd/glade" {
		arguments = []string{"build", "-buildvcs=false", "-trimpath", "-ldflags", "-s -w -X github.com/glade-sh/glade/internal/gladecli.Version=" + commit, "-o", "<candidate>", target}
	}
	return candidateBuildBinding{
		SourceRoot:  canonical,
		SourceTree:  tree,
		Go:          candidateAuthoritySource{Path: goPath, SHA256: goSHA256},
		Arguments:   arguments,
		Environment: environment,
	}, nil
}

func validateToolsCandidatePair(candidateRoot string, toolsBuild candidateBuildBinding) error {
	candidateRoot, err := filepath.EvalSymlinks(candidateRoot)
	if err != nil || !filepath.IsAbs(candidateRoot) {
		return fmt.Errorf("candidate source pairing is unavailable")
	}
	if filepath.Dir(candidateRoot) != filepath.Dir(toolsBuild.SourceRoot) {
		return fmt.Errorf("candidate and tools source roots must be siblings")
	}
	if err := validateToolsLocalReplacements(toolsBuild.SourceRoot, candidateRoot); err != nil {
		return fmt.Errorf("tools candidate source pairing is invalid")
	}
	data, err := os.ReadFile(filepath.Join(toolsBuild.SourceRoot, "go.mod"))
	if err != nil {
		return fmt.Errorf("tools candidate source pairing is unavailable")
	}
	parsed, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		return fmt.Errorf("tools candidate source pairing is invalid")
	}
	expected := map[string]string{
		"github.com/glade-sh/glade":       candidateRoot,
		"github.com/glade-sh/apex-parser": filepath.Join(candidateRoot, "third_party", "glade-apex-parser"),
	}
	seen := make(map[string]bool, len(expected))
	for _, replacement := range parsed.Replace {
		want, required := expected[replacement.Old.Path]
		if !required {
			continue
		}
		got := replacement.New.Path
		if seen[replacement.Old.Path] || replacement.Old.Version != "" || replacement.New.Version != "" {
			return fmt.Errorf("tools candidate source pairing is invalid")
		}
		if !filepath.IsAbs(got) {
			got = filepath.Join(toolsBuild.SourceRoot, got)
		}
		got, err = filepath.EvalSymlinks(got)
		want, wantErr := filepath.EvalSymlinks(want)
		if err != nil || wantErr != nil || filepath.Clean(got) != filepath.Clean(want) {
			return fmt.Errorf("tools candidate source pairing is invalid")
		}
		seen[replacement.Old.Path] = true
	}
	if len(seen) != len(expected) {
		return fmt.Errorf("tools candidate source pairing is invalid")
	}
	return nil
}

func validateToolsBuildBinding(sourceRoot string, _ candidateTool, binding candidateBuildBinding) error {
	derived, err := deriveToolsBuildBinding(sourceRoot)
	if err != nil || !reflect.DeepEqual(binding, derived) {
		return fmt.Errorf("tools build binding does not match sealed source")
	}
	return nil
}

func validateToolsBuildFromSource(sourceRoot string, tools candidateTool, binding candidateBuildBinding) error {
	if err := validateToolsBuildBinding(sourceRoot, tools, binding); err != nil {
		return err
	}
	before, err := runtimeArtifactFor(tools.Path, tools.Commit)
	if err != nil || before != tools.RuntimeArtifact {
		return fmt.Errorf("tools binary is stale")
	}
	root, err := os.MkdirTemp("", "glade-tools-rebuild-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	rebuilt := filepath.Join(root, "glade-tools")
	if err := runBoundCandidateBuild(binding, rebuilt); err != nil {
		return err
	}
	rebuiltSHA256, err := sha256File(rebuilt)
	if err != nil || rebuiltSHA256 != tools.SHA256 {
		return fmt.Errorf("tools binary is not reproducible from sealed source")
	}
	after, err := runtimeArtifactFor(tools.Path, tools.Commit)
	if err != nil || after != before || validateCleanGitRoot(binding.SourceRoot, tools.Commit) != nil {
		return fmt.Errorf("tools source or binary changed during build validation")
	}
	tree, err := gitOutput(binding.SourceRoot, "rev-parse", "HEAD^{tree}")
	if err != nil || tree != binding.SourceTree {
		return fmt.Errorf("tools source tree changed during build validation")
	}
	return nil
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
	if !filepath.IsAbs(outputPath) {
		return fmt.Errorf("candidate build output is invalid")
	}
	placeholder := -1
	for index, argument := range binding.Arguments {
		if argument != "<candidate>" {
			continue
		}
		if placeholder != -1 {
			return fmt.Errorf("candidate build output is invalid")
		}
		placeholder = index
	}
	if placeholder == -1 {
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
	arguments[placeholder] = outputPath
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	var stderr bytes.Buffer
	command := exec.CommandContext(ctx, binding.Go.Path, arguments...)
	command.Dir, command.Env, command.Stderr = binding.SourceRoot, append([]string(nil), binding.Environment...), &stderr
	if err := command.Run(); err != nil || ctx.Err() == context.DeadlineExceeded {
		exitCode := -1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
		if ctx.Err() == context.DeadlineExceeded {
			err = ctx.Err()
		}
		return &candidateBuildCommandError{Err: err, Command: append([]string{binding.Go.Path}, arguments...), ExitCode: exitCode, Stderr: stderr.String()}
	}
	after, err := sha256File(binding.Go.Path)
	if err != nil || after != before {
		return fmt.Errorf("candidate Go executable changed during build")
	}
	return nil
}

type candidateBuildCommandError struct {
	Err      error
	Command  []string
	ExitCode int
	Stderr   string
}

func (err *candidateBuildCommandError) Error() string {
	message := fmt.Sprintf("candidate source build failed: %v", err.Err)
	if stderr := strings.TrimSpace(err.Stderr); stderr != "" {
		message += ": " + stderr
	}
	return message
}

func (err *candidateBuildCommandError) Unwrap() error { return err.Err }

func validateCandidateParser(candidate attemptCandidate, workingDirectory string) error {
	root, err := os.MkdirTemp("", "glade-candidate-authority-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	home, data := filepath.Join(root, "home"), filepath.Join(root, "data")
	for _, path := range []string{home, data} {
		if err := os.Mkdir(path, 0o700); err != nil {
			return err
		}
	}
	environment := []string{"HOME=" + home, "XDG_DATA_HOME=" + data, "GLADE_HOME=" + workingDirectory, "PATH=/usr/bin:/bin", "TMPDIR=" + root}
	result, stdout, _ := runReplayCommandOutput(workingDirectory, ReplayCommand{Path: candidate.Path, Args: []string{"version", "--json"}, Env: environment, Timeout: 30 * time.Second})
	if !validCandidateCommandResult(result, candidate) || !validCandidateVersionJSON(stdout, candidate.Commit) {
		return fmt.Errorf("candidate embedded version is invalid")
	}
	result, stdout, _ = runReplayCommandOutput(workingDirectory, ReplayCommand{Path: candidate.Path, Args: []string{"doctor", "--json"}, Env: environment, Timeout: 30 * time.Second})
	var doctor struct {
		Command     string `json:"command"`
		ExitCode    int    `json:"exitCode"`
		ParserOK    bool   `json:"parserOK"`
		ToolchainOK bool   `json:"toolchainOK"`
	}
	if validateJSONWithoutDuplicateKeys(stdout) != nil || json.Unmarshal(stdout, &doctor) != nil || doctor.Command != "doctor" {
		return fmt.Errorf("candidate doctor output is invalid")
	}
	if !doctor.ParserOK {
		return fmt.Errorf("candidate doctor reported parser unavailable")
	}
	if !doctor.ToolchainOK {
		return fmt.Errorf("candidate doctor reported toolchain unavailable")
	}
	if !validCandidateCommandResult(result, candidate) || doctor.ExitCode != 0 {
		exitCode := result.ExitCode
		if doctor.ExitCode != 0 {
			exitCode = doctor.ExitCode
		}
		return fmt.Errorf("candidate doctor command failed with exit code %d", exitCode)
	}
	fixture, err := os.CreateTemp(root, "probe-*.cls")
	if err != nil {
		return err
	}
	fixturePath := fixture.Name()
	defer os.Remove(fixturePath)
	if _, err := fixture.WriteString("public class CandidateAuthorityProbe {}\n"); err != nil {
		_ = fixture.Close()
		return err
	}
	if err := fixture.Close(); err != nil {
		return err
	}
	result, stdout, _ = runReplayCommandOutput(workingDirectory, ReplayCommand{Path: candidate.Path, Args: []string{"parse", fixturePath, "--json", "--no-progress"}, Env: environment, Timeout: 30 * time.Second})
	if !validCandidateCommandResult(result, candidate) || !validCandidateParseJSON(stdout) {
		return fmt.Errorf("candidate Apex parse check failed")
	}
	return nil
}

func validCandidateCommandResult(result CommandResult, candidate attemptCandidate) bool {
	return result.Passed && result.ExitCode == 0 && !result.TimedOut && result.ExecutableSHA256 == candidate.SHA256 && result.ExecutableAfterSHA256 == candidate.SHA256
}

func validCandidateVersionJSON(data []byte, commit string) bool {
	var version struct {
		SchemaVersion string `json:"schemaVersion"`
		Command       string `json:"command"`
		Status        string `json:"status"`
		ExitCode      int    `json:"exitCode"`
		Data          struct {
			Version string `json:"version"`
		} `json:"data"`
	}
	return validateJSONWithoutDuplicateKeys(data) == nil && json.Unmarshal(data, &version) == nil && version.SchemaVersion == "1.0" && version.Command == "version" && version.Status == "passed" && version.ExitCode == 0 && version.Data.Version == commit
}

func validCandidateParseJSON(data []byte) bool {
	var parse struct {
		SchemaVersion string `json:"schemaVersion"`
		Command       string `json:"command"`
		Status        string `json:"status"`
		ExitCode      int    `json:"exitCode"`
	}
	return validateJSONWithoutDuplicateKeys(data) == nil && json.Unmarshal(data, &parse) == nil && parse.SchemaVersion == "1.0" && parse.Command == "parse" && parse.Status == "passed" && parse.ExitCode == 0
}

func validCandidateBuildReceipt(receipt candidateBuildReceipt, input candidateAuthorityInput) bool {
	candidate := input.Candidate
	if receipt.SchemaVersion != 1 && receipt.SchemaVersion != 2 {
		return false
	}
	if receipt.SchemaVersion == 2 && (receipt.CandidateRef == "" || receipt.ToolsRef == "" || receipt.CandidateRefCommit != candidate.Commit || receipt.ToolsRefCommit != input.Tools.Commit) {
		return false
	}
	return receipt.Status == "clean-exact-candidate" && receipt.CleanWorktree && receipt.SourceCommit == candidate.Commit && receipt.BinarySHA256 == candidate.SHA256 && receipt.Candidate == candidate && receipt.Tools == input.Tools && commitPattern.MatchString(candidate.Commit) && filepath.IsAbs(candidate.Path) && sha256Pattern.MatchString(candidate.SHA256) && validateCandidateTool(receipt.Tools) == nil
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
	return validateCandidateAuthorityReviewFields(data, "PASS", map[string]string{"Candidate commit": candidate.Commit, "Candidate SHA-256": candidate.SHA256, "Tools commit": tools.Commit, "Tools OS": tools.OS, "Tools arch": tools.Arch, "Tools SHA-256": tools.SHA256, "Tools path": tools.Path})
}

func validateCandidateAuthorityReviewForReceipt(data []byte, candidate attemptCandidate, tools candidateTool, receipt candidateBuildReceipt) error {
	expected := map[string]string{"Candidate commit": candidate.Commit, "Candidate SHA-256": candidate.SHA256, "Tools commit": tools.Commit, "Tools OS": tools.OS, "Tools arch": tools.Arch, "Tools SHA-256": tools.SHA256, "Tools path": tools.Path}
	if receipt.SchemaVersion == 2 {
		expected["Candidate ref"] = receipt.CandidateRef
		expected["Candidate ref commit"] = receipt.CandidateRefCommit
		expected["Tools ref"] = receipt.ToolsRef
		expected["Tools ref commit"] = receipt.ToolsRefCommit
	}
	return validateCandidateAuthorityReviewFields(data, "PASS", expected)
}

func validateCandidateAuthorityReviewFields(data []byte, verdict string, expected map[string]string) error {
	if !strings.HasPrefix(string(data), "Verdict: "+verdict+"\n") {
		return fmt.Errorf("candidate authority review is invalid")
	}
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
