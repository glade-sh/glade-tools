package corpusassurance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// AssuranceAttempt is the create-only authority for every executable used by
// an assurance run. Later workflow stages derive their runtime identities from
// this record rather than accepting a caller-declared identity.
type AssuranceAttempt struct {
	SchemaVersion                int               `json:"schemaVersion"`
	InventorySHA256              string            `json:"inventorySha256"`
	CandidateAuthoritySHA256     string            `json:"candidateAuthoritySha256"`
	Candidate                    RuntimeArtifact   `json:"candidate"`
	Tools                        RuntimeArtifact   `json:"tools"`
	RemoteCleanupAuthoritySHA256 map[string]string `json:"remoteCleanupAuthoritySha256,omitempty"`
}

type AssuranceAttemptRequest struct {
	InventoryPath               string
	CandidateAuthorityPath      string
	CandidatePath               string
	CandidateRoot               string
	ToolsPath                   string
	ToolsRoot                   string
	RemoteCleanupAuthorityPaths map[string]string
	OutputPath                  string
}

type AssuranceAttemptInitRequest struct {
	InventoryPath          string
	CandidateAuthorityPath string
	CandidatePath          string
	CandidateRoot          string
	ToolsPath              string
	ToolsRoot              string
	ReplayHost             string
	ReplayParent           string
	SalesforceHost         string
	SalesforceParent       string
	RunID                  string
	OutputDir              string
}

type attemptCandidate struct {
	Commit string `json:"commit"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// CreateAssuranceAttempt verifies the frozen candidate against the sealed
// reconciliation record, verifies clean source roots, and records the one
// candidate/tools pair permitted for the run.
func CreateAssuranceAttempt(request AssuranceAttemptRequest) (AssuranceAttempt, error) {
	for _, path := range []string{request.InventoryPath, request.CandidateAuthorityPath, request.CandidatePath, request.CandidateRoot, request.ToolsPath, request.ToolsRoot, request.OutputPath} {
		if !filepath.IsAbs(path) {
			return AssuranceAttempt{}, fmt.Errorf("absolute attempt paths are required")
		}
	}
	if err := validateAttemptCleanupAuthorityPaths(request.RemoteCleanupAuthorityPaths); err != nil {
		return AssuranceAttempt{}, err
	}
	if _, err := os.Lstat(request.OutputPath); err == nil {
		return AssuranceAttempt{}, fmt.Errorf("attempt output already exists: %s", request.OutputPath)
	} else if !os.IsNotExist(err) {
		return AssuranceAttempt{}, err
	}
	attempt, err := deriveAssuranceAttempt(request)
	if err != nil {
		return AssuranceAttempt{}, err
	}
	authorities, err := bindAttemptCleanupAuthorities(request.RemoteCleanupAuthorityPaths, attempt)
	if err != nil {
		return AssuranceAttempt{}, err
	}
	attempt.RemoteCleanupAuthoritySHA256 = authorities
	if err := ValidateAssuranceAttempt(attempt); err != nil {
		return AssuranceAttempt{}, err
	}
	if err := WriteNewJSON(request.OutputPath, attempt); err != nil {
		return AssuranceAttempt{}, err
	}
	return attempt, nil
}

// CreateAssuranceAttemptWithAuthorities creates both bound remote cleanup
// authorities before sealing the final attempt. A failed directory is left
// intact so the caller must use a successor run ID and output directory.
func CreateAssuranceAttemptWithAuthorities(request AssuranceAttemptInitRequest) (AssuranceAttempt, error) {
	for _, path := range []string{request.InventoryPath, request.CandidateAuthorityPath, request.CandidatePath, request.CandidateRoot, request.ToolsPath, request.ToolsRoot, request.ReplayParent, request.SalesforceParent, request.OutputDir} {
		if !filepath.IsAbs(path) {
			return AssuranceAttempt{}, fmt.Errorf("absolute attempt-init paths are required")
		}
	}
	if !safeAttemptRunID(request.RunID) {
		return AssuranceAttempt{}, fmt.Errorf("invalid attempt run id")
	}
	if _, err := os.Lstat(request.OutputDir); err == nil {
		return AssuranceAttempt{}, fmt.Errorf("attempt-init output directory already exists: %s", request.OutputDir)
	} else if !os.IsNotExist(err) {
		return AssuranceAttempt{}, err
	}
	if err := os.Mkdir(request.OutputDir, 0o700); err != nil {
		return AssuranceAttempt{}, err
	}
	if err := os.Chmod(request.OutputDir, 0o700); err != nil {
		return AssuranceAttempt{}, err
	}
	attempt, err := deriveAssuranceAttempt(AssuranceAttemptRequest{InventoryPath: request.InventoryPath, CandidateAuthorityPath: request.CandidateAuthorityPath, CandidatePath: request.CandidatePath, CandidateRoot: request.CandidateRoot, ToolsPath: request.ToolsPath, ToolsRoot: request.ToolsRoot})
	if err != nil {
		return AssuranceAttempt{}, fmt.Errorf("attempt-init: %w", err)
	}
	attemptSHA := attemptBindingHash(attempt)
	targets := map[string]struct {
		host   string
		parent string
	}{
		"replay-worker":     {request.ReplayHost, request.ReplayParent},
		"salesforce-worker": {request.SalesforceHost, request.SalesforceParent},
	}
	authorityPaths := map[string]string{
		"replay-worker":     filepath.Join(request.OutputDir, "REPLAY_REMOTE_CLEANUP_AUTHORITY.json"),
		"salesforce-worker": filepath.Join(request.OutputDir, "SALESFORCE_REMOTE_CLEANUP_AUTHORITY.json"),
	}
	boundPaths := make(map[string]string, len(targets))
	for _, role := range []string{"replay-worker", "salesforce-worker"} {
		target := targets[role]
		attemptRoot := filepath.Join(target.parent, fmt.Sprintf("assurance-%s-%s-%s", attemptSHA[:16], request.RunID, role))
		if err := validateRemoteAttemptTarget(attemptSHA, role, target.host, target.parent, attemptRoot); err != nil {
			return AssuranceAttempt{}, err
		}
		if err := WriteNewJSON(authorityPaths[role], RemoteAttemptAuthority{SchemaVersion: 1, AttemptSHA256: attemptSHA, Role: role, Host: target.host, Parent: filepath.Clean(target.parent), AttemptRoot: filepath.Clean(attemptRoot)}); err != nil {
			return AssuranceAttempt{}, fmt.Errorf("write %s authority in %s: %w", role, request.OutputDir, err)
		}
		boundPaths[role] = authorityPaths[role]
	}
	return CreateAssuranceAttempt(AssuranceAttemptRequest{InventoryPath: request.InventoryPath, CandidateAuthorityPath: request.CandidateAuthorityPath, CandidatePath: request.CandidatePath, CandidateRoot: request.CandidateRoot, ToolsPath: request.ToolsPath, ToolsRoot: request.ToolsRoot, RemoteCleanupAuthorityPaths: boundPaths, OutputPath: filepath.Join(request.OutputDir, "ATTEMPT.json")})
}

func deriveAssuranceAttempt(request AssuranceAttemptRequest) (AssuranceAttempt, error) {
	_, inventoryBytes, err := readInventorySpec(request.InventoryPath)
	if err != nil {
		return AssuranceAttempt{}, fmt.Errorf("read IN_SCOPE inventory: %w", err)
	}
	authority, authorityBytes, err := readCandidateAuthority(request.CandidateAuthorityPath)
	if err != nil {
		return AssuranceAttempt{}, err
	}
	if err := validateCleanGitRoot(request.CandidateRoot, authority.Candidate.Commit); err != nil {
		return AssuranceAttempt{}, fmt.Errorf("candidate source: %w", err)
	}
	candidate, err := runtimeArtifactFor(request.CandidatePath, authority.Candidate.Commit)
	if err != nil {
		return AssuranceAttempt{}, fmt.Errorf("candidate: %w", err)
	}
	if candidate.SHA256 != authority.Candidate.SHA256 {
		return AssuranceAttempt{}, fmt.Errorf("candidate binary does not match sealed authority")
	}
	toolsCommit, err := cleanGitHead(request.ToolsRoot)
	if err != nil {
		return AssuranceAttempt{}, fmt.Errorf("tools source: %w", err)
	}
	tools, err := releaseExecutingTools(request.ToolsPath, toolsCommit)
	if err != nil {
		return AssuranceAttempt{}, fmt.Errorf("tools: %w", err)
	}
	requestedTools, err := canonicalCandidateToolPath(request.ToolsPath)
	if err != nil || tools != authority.Tools.RuntimeArtifact || requestedTools != authority.Tools.Path {
		return AssuranceAttempt{}, fmt.Errorf("tools do not match sealed authority")
	}
	return AssuranceAttempt{SchemaVersion: 1, InventorySHA256: replayBytesSHA256(inventoryBytes), CandidateAuthoritySHA256: replayBytesSHA256(authorityBytes), Candidate: candidate, Tools: tools}, nil
}

func safeAttemptRunID(runID string) bool {
	if runID == "" || strings.ContainsAny(runID, "/\\") {
		return false
	}
	for _, r := range runID {
		if r != '-' && r != '_' && r != '.' && (r < '0' || r > '9') && (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') {
			return false
		}
	}
	return true
}

func LoadAssuranceAttempt(path string) (AssuranceAttempt, error) {
	if !filepath.IsAbs(path) {
		return AssuranceAttempt{}, fmt.Errorf("attempt path must be absolute")
	}
	attempt, _, err := readExactJSONBytes[AssuranceAttempt](path)
	if err != nil {
		return AssuranceAttempt{}, err
	}
	if err := ValidateAssuranceAttempt(attempt); err != nil {
		return AssuranceAttempt{}, err
	}
	return attempt, nil
}

func ValidateAssuranceAttempt(attempt AssuranceAttempt) error {
	if attempt.SchemaVersion != 1 || !sha256Pattern.MatchString(attempt.InventorySHA256) || !sha256Pattern.MatchString(attempt.CandidateAuthoritySHA256) {
		return fmt.Errorf("invalid assurance attempt bindings")
	}
	if err := ValidateRuntimeArtifact(attempt.Candidate); err != nil {
		return fmt.Errorf("candidate: %w", err)
	}
	if err := ValidateRuntimeArtifact(attempt.Tools); err != nil {
		return fmt.Errorf("tools: %w", err)
	}
	if len(attempt.RemoteCleanupAuthoritySHA256) != 2 {
		return fmt.Errorf("invalid remote cleanup authority bindings")
	}
	for _, role := range []string{"replay-worker", "salesforce-worker"} {
		if !sha256Pattern.MatchString(attempt.RemoteCleanupAuthoritySHA256[role]) {
			return fmt.Errorf("missing remote cleanup authority binding for %q", role)
		}
	}
	return nil
}

func validateAttemptCleanupAuthorityPaths(paths map[string]string) error {
	if len(paths) != 2 {
		return fmt.Errorf("both remote cleanup authority paths are required")
	}
	for _, role := range []string{"replay-worker", "salesforce-worker"} {
		if path := paths[role]; path == "" || !filepath.IsAbs(path) {
			return fmt.Errorf("absolute remote cleanup authority path is required for %q", role)
		}
	}
	return nil
}

func bindAttemptCleanupAuthorities(paths map[string]string, attempt AssuranceAttempt) (map[string]string, error) {
	authorities := make(map[string]string, len(paths))
	for _, role := range []string{"replay-worker", "salesforce-worker"} {
		authority, data, err := readRemoteAttemptAuthority(paths[role])
		if err != nil || authority.Role != role || authority.AttemptSHA256 != attemptBindingHash(attempt) {
			return nil, fmt.Errorf("remote cleanup authority does not bind %q", role)
		}
		authorities[role] = replayBytesSHA256(data)
	}
	return authorities, nil
}

func attemptHash(attempt AssuranceAttempt) string {
	data, _ := json.Marshal(attempt)
	return replayBytesSHA256(data)
}

func attemptBindingHash(attempt AssuranceAttempt) string {
	authorities := attempt.RemoteCleanupAuthoritySHA256
	attempt.RemoteCleanupAuthoritySHA256 = nil
	data, _ := json.Marshal(attempt)
	attempt.RemoteCleanupAuthoritySHA256 = authorities
	return replayBytesSHA256(data)
}

func remoteCleanupAuthorityMatches(attempt AssuranceAttempt, authority RemoteAttemptAuthority, authoritySHA string) bool {
	if authority.AttemptSHA256 != attemptBindingHash(attempt) {
		return false
	}
	if len(attempt.RemoteCleanupAuthoritySHA256) != 2 {
		return false
	}
	return attempt.RemoteCleanupAuthoritySHA256[authority.Role] == authoritySHA
}

func readCandidateAuthority(path string) (candidateAuthorityInput, []byte, error) {
	var document candidateAuthorityDocument
	data, err := readExactCandidateAuthorityDocument(path, &document)
	if err != nil {
		return candidateAuthorityInput{}, nil, err
	}
	input, err := validateCandidateAuthorityDocument(document)
	if err != nil {
		return candidateAuthorityInput{}, nil, err
	}
	return input, data, nil
}

func runtimeArtifactFor(path, commit string) (RuntimeArtifact, error) {
	if !commitPattern.MatchString(commit) {
		return RuntimeArtifact{}, fmt.Errorf("invalid source commit")
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return RuntimeArtifact{}, fmt.Errorf("runtime must be an executable regular file")
	}
	hash, err := sha256File(path)
	if err != nil {
		return RuntimeArtifact{}, err
	}
	return RuntimeArtifact{Commit: commit, OS: runtime.GOOS, Arch: runtime.GOARCH, SHA256: hash}, nil
}

func validateCleanGitRoot(root, expectedCommit string) error {
	head, err := cleanGitHead(root)
	if err != nil {
		return err
	}
	if head != expectedCommit {
		return fmt.Errorf("HEAD does not match sealed commit")
	}
	return nil
}

func cleanGitHead(root string) (string, error) {
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("git root must be absolute")
	}
	status, err := gitOutput(root, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil || status != "" {
		return "", fmt.Errorf("git root is not clean")
	}
	head, err := gitOutput(root, "rev-parse", "HEAD")
	if err != nil || !commitPattern.MatchString(head) {
		return "", fmt.Errorf("read git HEAD")
	}
	return head, nil
}
