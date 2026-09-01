package corpusassurance

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CandidateBuildRequest describes the one exact candidate/tools build whose
// outputs may be used to create a candidate authority.
type CandidateBuildRequest struct {
	CandidateRoot     string
	ToolsRoot         string
	CandidateRef      string
	ToolsRef          string
	CandidateOutput   string
	ToolsOutput       string
	ReceiptOutput     string
	ReviewOutput      string
	ToolsFreezeOutput string
}

// CreateCandidateBuildReceipt builds both binaries from clean roots and seals
// their exact source/ref/artifact identities. Every output is create-only.
func CreateCandidateBuildReceipt(request CandidateBuildRequest) (receipt candidateBuildReceipt, returnErr error) {
	var progress *candidateBuildProgress
	stage := "validate-inputs"
	defer func() {
		if progress == nil {
			return
		}
		if returnErr != nil {
			progress.event("candidate-build-failed", "stage="+stage)
		}
		if err := progress.close(); returnErr == nil && err != nil {
			returnErr = err
		}
	}()
	paths := []string{request.CandidateRoot, request.ToolsRoot, request.CandidateOutput, request.ToolsOutput, request.ReceiptOutput, request.ReviewOutput, request.ToolsFreezeOutput}
	for _, path := range paths {
		if !filepath.IsAbs(path) {
			return candidateBuildReceipt{}, fmt.Errorf("absolute candidate build paths are required")
		}
	}
	for _, path := range paths[2:] {
		if _, err := os.Lstat(path); err == nil {
			return candidateBuildReceipt{}, fmt.Errorf("candidate build output already exists: %s", path)
		} else if !os.IsNotExist(err) {
			return candidateBuildReceipt{}, err
		}
	}
	seen := make(map[string]bool, len(paths)-2)
	for _, path := range paths[2:] {
		path = filepath.Clean(path)
		if seen[path] {
			return candidateBuildReceipt{}, fmt.Errorf("candidate build outputs must be distinct")
		}
		seen[path] = true
	}
	if request.CandidateRef == "" || request.ToolsRef == "" {
		return candidateBuildReceipt{}, fmt.Errorf("candidate and tools refs are required")
	}
	progress, err := newCandidateBuildProgress(request.ReceiptOutput)
	if err != nil {
		return candidateBuildReceipt{}, err
	}
	progress.event("candidate-build-start", "candidate-ref="+request.CandidateRef+" tools-ref="+request.ToolsRef)
	stage = "validate-sources"
	candidateCommit, err := cleanGitHead(request.CandidateRoot)
	if err != nil {
		return candidateBuildReceipt{}, fmt.Errorf("candidate source: %w", err)
	}
	candidateRefCommit, err := resolveGitCommit(request.CandidateRoot, request.CandidateRef)
	if err != nil || candidateRefCommit != candidateCommit {
		return candidateBuildReceipt{}, fmt.Errorf("candidate ref does not match clean HEAD")
	}
	toolsCommit, err := cleanGitHead(request.ToolsRoot)
	if err != nil {
		return candidateBuildReceipt{}, fmt.Errorf("tools source: %w", err)
	}
	toolsRefCommit, err := resolveGitCommit(request.ToolsRoot, request.ToolsRef)
	if err != nil || toolsRefCommit != toolsCommit {
		return candidateBuildReceipt{}, fmt.Errorf("tools ref does not match clean HEAD")
	}
	candidateBuild, err := deriveCandidateBuildBinding(request.CandidateRoot)
	if err != nil {
		return candidateBuildReceipt{}, err
	}
	toolsBuild, err := deriveToolsBuildBinding(request.ToolsRoot)
	if err != nil {
		return candidateBuildReceipt{}, err
	}
	if err := validateToolsCandidatePair(request.CandidateRoot, toolsBuild); err != nil {
		return candidateBuildReceipt{}, err
	}
	for _, path := range []string{request.CandidateOutput, request.ToolsOutput, request.ReceiptOutput, request.ReviewOutput, request.ToolsFreezeOutput} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return candidateBuildReceipt{}, err
		}
	}
	stage = "candidate-build"
	progress.event("candidate-build-start", "commit="+candidateCommit)
	if err := runBoundCandidateBuild(candidateBuild, request.CandidateOutput); err != nil {
		progress.event("candidate-build-failed", "error="+progressSafe(err.Error()))
		return candidateBuildReceipt{}, fmt.Errorf("candidate build: %w", err)
	}
	progress.event("candidate-build-complete", "output="+request.CandidateOutput)
	stage = "tools-build"
	progress.event("tools-build-start", "commit="+toolsCommit)
	if err := runBoundCandidateBuild(toolsBuild, request.ToolsOutput); err != nil {
		progress.event("tools-build-failed", "error="+progressSafe(err.Error()))
		return candidateBuildReceipt{}, fmt.Errorf("tools build: %w", err)
	}
	progress.event("tools-build-complete", "output="+request.ToolsOutput)
	stage = "validate-builds"
	progress.event("builds-complete", "")
	if err := validateAdvertisedBuildRef(request.CandidateRoot, request.CandidateRef, candidateCommit); err != nil {
		return candidateBuildReceipt{}, fmt.Errorf("candidate source changed during build: %w", err)
	}
	if err := validateAdvertisedBuildRef(request.ToolsRoot, request.ToolsRef, toolsCommit); err != nil {
		return candidateBuildReceipt{}, fmt.Errorf("tools source changed during build: %w", err)
	}
	candidatePath, err := canonicalBuiltOutput(request.CandidateOutput)
	if err != nil {
		return candidateBuildReceipt{}, err
	}
	toolsPath, err := canonicalBuiltOutput(request.ToolsOutput)
	if err != nil {
		return candidateBuildReceipt{}, err
	}
	candidate, err := runtimeArtifactFor(candidatePath, candidateCommit)
	if err != nil {
		return candidateBuildReceipt{}, fmt.Errorf("candidate artifact: %w", err)
	}
	toolsArtifact, err := runtimeArtifactFor(toolsPath, toolsCommit)
	if err != nil {
		return candidateBuildReceipt{}, fmt.Errorf("tools artifact: %w", err)
	}
	receipt = candidateBuildReceipt{
		SchemaVersion:      2,
		Status:             "clean-exact-candidate",
		SourceCommit:       candidateCommit,
		BinarySHA256:       candidate.SHA256,
		CleanWorktree:      true,
		CandidateRef:       request.CandidateRef,
		CandidateRefCommit: candidateRefCommit,
		ToolsRef:           request.ToolsRef,
		ToolsRefCommit:     toolsRefCommit,
		Candidate:          attemptCandidate{Commit: candidate.Commit, Path: candidatePath, SHA256: candidate.SHA256},
		Tools:              candidateTool{RuntimeArtifact: toolsArtifact, Path: toolsPath},
	}
	if err := WriteNewJSON(request.ReceiptOutput, receipt); err != nil {
		return candidateBuildReceipt{}, err
	}
	review := candidateAuthorityReviewTemplate(receipt)
	if err := writeNewText(request.ReviewOutput, review, 0o600); err != nil {
		return candidateBuildReceipt{}, err
	}
	if err := writeNewText(request.ToolsFreezeOutput, toolsCommit+"\n", 0o400); err != nil {
		return candidateBuildReceipt{}, err
	}
	stage = "complete"
	progress.event("candidate-build-complete", "receipt="+request.ReceiptOutput)
	return receipt, nil
}

type candidateBuildProgress struct {
	file *os.File
}

func newCandidateBuildProgress(receiptPath string) (*candidateBuildProgress, error) {
	progressPath := candidateBuildProgressPath(receiptPath)
	if err := os.MkdirAll(filepath.Dir(progressPath), 0o700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(progressPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	return &candidateBuildProgress{file: file}, nil
}

func (progress *candidateBuildProgress) event(event, detail string) {
	_, _ = fmt.Fprintf(progress.file, "%s event=%s %s\n", time.Now().UTC().Format(time.RFC3339Nano), event, detail)
}

func (progress *candidateBuildProgress) close() error {
	return progress.file.Close()
}

func candidateBuildProgressPath(receiptPath string) string {
	evidenceRoot := filepath.Dir(receiptPath)
	if filepath.Base(evidenceRoot) == "bindings" {
		evidenceRoot = filepath.Dir(evidenceRoot)
	}
	logsRoot := filepath.Join(evidenceRoot, "logs")
	return filepath.Join(logsRoot, "candidate-build.log")
}

func progressSafe(value string) string {
	return strings.NewReplacer("\n", "\\n", "\r", "\\r").Replace(value)
}

func resolveGitCommit(root, ref string) (string, error) {
	commit, err := gitOutput(root, "rev-parse", "--verify", ref+"^{commit}")
	if err != nil || !commitPattern.MatchString(commit) {
		return "", fmt.Errorf("resolve Git ref %q", ref)
	}
	return commit, nil
}

func validateAdvertisedBuildRef(root, ref, commit string) error {
	head, err := cleanGitHead(root)
	if err != nil {
		return err
	}
	resolved, err := resolveGitCommit(root, ref)
	if err != nil || resolved != commit || head != commit {
		return fmt.Errorf("advertised ref or HEAD moved")
	}
	return nil
}

func canonicalBuiltOutput(path string) (string, error) {
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil || !filepath.IsAbs(canonical) {
		return "", fmt.Errorf("built output is unavailable")
	}
	return filepath.Clean(canonical), nil
}

func writeNewText(path, value string, mode os.FileMode) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := file.WriteString(value); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func candidateAuthorityReviewTemplate(receipt candidateBuildReceipt) string {
	return strings.Join([]string{
		"Verdict: PENDING",
		"Candidate commit: " + receipt.Candidate.Commit,
		"Candidate SHA-256: " + receipt.Candidate.SHA256,
		"Candidate ref: " + receipt.CandidateRef,
		"Candidate ref commit: " + receipt.CandidateRefCommit,
		"Tools commit: " + receipt.Tools.Commit,
		"Tools OS: " + receipt.Tools.OS,
		"Tools arch: " + receipt.Tools.Arch,
		"Tools SHA-256: " + receipt.Tools.SHA256,
		"Tools path: " + receipt.Tools.Path,
		"Tools ref: " + receipt.ToolsRef,
		"Tools ref commit: " + receipt.ToolsRefCommit,
		"",
	}, "\n")
}
