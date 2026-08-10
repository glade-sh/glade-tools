package corpusassurance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
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
	RemoteCleanupAuthorityPaths []string
	OutputPath                  string
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
	if _, err := os.Lstat(request.OutputPath); err == nil {
		return AssuranceAttempt{}, fmt.Errorf("attempt output already exists: %s", request.OutputPath)
	} else if !os.IsNotExist(err) {
		return AssuranceAttempt{}, err
	}
	_, inventoryBytes, err := readInventorySpec(request.InventoryPath)
	if err != nil {
		return AssuranceAttempt{}, fmt.Errorf("read IN_SCOPE inventory: %w", err)
	}
	authorityCandidate, authorityBytes, err := readCandidateAuthority(request.CandidateAuthorityPath)
	if err != nil {
		return AssuranceAttempt{}, err
	}
	if err := validateCleanGitRoot(request.CandidateRoot, authorityCandidate.Commit); err != nil {
		return AssuranceAttempt{}, fmt.Errorf("candidate source: %w", err)
	}
	candidate, err := runtimeArtifactFor(request.CandidatePath, authorityCandidate.Commit)
	if err != nil {
		return AssuranceAttempt{}, fmt.Errorf("candidate: %w", err)
	}
	if candidate.SHA256 != authorityCandidate.SHA256 {
		return AssuranceAttempt{}, fmt.Errorf("candidate binary does not match sealed authority")
	}
	toolsCommit, err := cleanGitHead(request.ToolsRoot)
	if err != nil {
		return AssuranceAttempt{}, fmt.Errorf("tools source: %w", err)
	}
	tools, err := runtimeArtifactFor(request.ToolsPath, toolsCommit)
	if err != nil {
		return AssuranceAttempt{}, fmt.Errorf("tools: %w", err)
	}
	attempt := AssuranceAttempt{SchemaVersion: 1, InventorySHA256: replayBytesSHA256(inventoryBytes), CandidateAuthoritySHA256: replayBytesSHA256(authorityBytes), Candidate: candidate, Tools: tools}
	if len(request.RemoteCleanupAuthorityPaths) != 0 {
		if len(request.RemoteCleanupAuthorityPaths) != 2 {
			return AssuranceAttempt{}, fmt.Errorf("exactly two remote cleanup authorities are required")
		}
		bindingSHA := attemptHash(attempt)
		attempt.RemoteCleanupAuthoritySHA256 = make(map[string]string, len(request.RemoteCleanupAuthorityPaths))
		for _, authorityPath := range request.RemoteCleanupAuthorityPaths {
			authority, authorityBytes, err := readRemoteAttemptAuthority(authorityPath)
			if err != nil || authority.AttemptSHA256 != bindingSHA || attempt.RemoteCleanupAuthoritySHA256[authority.Role] != "" {
				return AssuranceAttempt{}, fmt.Errorf("remote cleanup authority is not bound to the candidate attempt")
			}
			attempt.RemoteCleanupAuthoritySHA256[authority.Role] = replayBytesSHA256(authorityBytes)
		}
		if attempt.RemoteCleanupAuthoritySHA256["replay-worker"] == "" || attempt.RemoteCleanupAuthoritySHA256["salesforce-worker"] == "" {
			return AssuranceAttempt{}, fmt.Errorf("remote cleanup authorities must cover both workers")
		}
	}
	if err := ValidateAssuranceAttempt(attempt); err != nil {
		return AssuranceAttempt{}, err
	}
	if err := WriteNewJSON(request.OutputPath, attempt); err != nil {
		return AssuranceAttempt{}, err
	}
	return attempt, nil
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
	if len(attempt.RemoteCleanupAuthoritySHA256) != 0 {
		if len(attempt.RemoteCleanupAuthoritySHA256) != 2 {
			return fmt.Errorf("invalid remote cleanup authority bindings")
		}
		for _, role := range []string{"replay-worker", "salesforce-worker"} {
			if !sha256Pattern.MatchString(attempt.RemoteCleanupAuthoritySHA256[role]) {
				return fmt.Errorf("missing remote cleanup authority binding for %q", role)
			}
		}
	}
	return nil
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

func readCandidateAuthority(path string) (attemptCandidate, []byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return attemptCandidate{}, nil, err
	}
	var document map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&document); err != nil {
		return attemptCandidate{}, nil, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return attemptCandidate{}, nil, fmt.Errorf("candidate authority contains multiple JSON values")
	}
	var schemaVersion int
	if err := json.Unmarshal(document["schemaVersion"], &schemaVersion); err != nil || schemaVersion != 1 {
		return attemptCandidate{}, nil, fmt.Errorf("candidate authority schema is invalid")
	}
	binding, err := authorityCandidateAt(document["binding"])
	if err != nil {
		return attemptCandidate{}, nil, err
	}
	bound, err := authorityCandidateAt(document["boundInputs"])
	if err != nil {
		return attemptCandidate{}, nil, err
	}
	if binding != bound || !commitPattern.MatchString(binding.Commit) || !filepath.IsAbs(binding.Path) || !sha256Pattern.MatchString(binding.SHA256) {
		return attemptCandidate{}, nil, fmt.Errorf("candidate authority candidates do not agree")
	}
	return binding, data, nil
}

func authorityCandidateAt(raw json.RawMessage) (attemptCandidate, error) {
	var holder struct {
		Candidate json.RawMessage `json:"candidate"`
	}
	if err := json.Unmarshal(raw, &holder); err != nil || len(holder.Candidate) == 0 {
		return attemptCandidate{}, fmt.Errorf("candidate authority lacks candidate binding")
	}
	var candidate attemptCandidate
	decoder := json.NewDecoder(bytes.NewReader(holder.Candidate))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&candidate); err != nil {
		return attemptCandidate{}, err
	}
	return candidate, nil
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
