package corpusassurance

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestCreateAssuranceAttemptDerivesCandidateFromSealedAuthority(t *testing.T) {
	root := t.TempDir()
	candidateRoot := newInventoryRepository(t, map[string]string{"main.go": "package main\n"})
	toolsRoot := newInventoryRepository(t, map[string]string{"main.go": "package main\n"})
	candidatePath := filepath.Join(root, "glade")
	toolsPath := filepath.Join(root, "glade-tools")
	if err := os.WriteFile(candidatePath, []byte("candidate"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(toolsPath, []byte("tools"), 0o700); err != nil {
		t.Fatal(err)
	}
	inventoryPath := filepath.Join(root, "IN_SCOPE.json")
	if err := WriteNewJSON(inventoryPath, InventorySpec{SchemaVersion: 1, Scope: "private-corpus-assurance", Repositories: []InventoryEntry{{ID: "private-corpus-001", CheckoutPath: candidateRoot, ExpectedCommit: testGitOutput(t, candidateRoot, "rev-parse", "HEAD")}}}); err != nil {
		t.Fatal(err)
	}
	candidate := sealedAttemptCandidate{Commit: testGitOutput(t, candidateRoot, "rev-parse", "HEAD"), Path: candidatePath, SHA256: fileSHA256(t, candidatePath)}
	authorityPath := filepath.Join(root, "RECONCILIATION.json")
	writeAttemptAuthority(t, authorityPath, candidate, candidateRoot)
	cleanupAuthorities := writeAttemptCleanupAuthorities(t, root, inventoryPath, fileSHA256(t, authorityPath), candidate, toolsRoot, toolsPath)
	outputPath := filepath.Join(root, "ATTEMPT.json")
	attempt, err := CreateAssuranceAttempt(AssuranceAttemptRequest{InventoryPath: inventoryPath, CandidateAuthorityPath: authorityPath, CandidatePath: candidatePath, CandidateRoot: candidateRoot, ToolsPath: toolsPath, ToolsRoot: toolsRoot, RemoteCleanupAuthorityPaths: cleanupAuthorities, OutputPath: outputPath})
	if err != nil {
		t.Fatalf("CreateAssuranceAttempt: %v", err)
	}
	if attempt.InventorySHA256 != fileSHA256(t, inventoryPath) || attempt.Candidate.Commit != candidate.Commit || attempt.Candidate.SHA256 != candidate.SHA256 || attempt.Tools.Commit != testGitOutput(t, toolsRoot, "rev-parse", "HEAD") || attempt.Tools.SHA256 != fileSHA256(t, toolsPath) || attempt.Candidate.OS != runtime.GOOS || attempt.Candidate.Arch != runtime.GOARCH {
		t.Fatalf("attempt = %#v", attempt)
	}
	if loaded, err := LoadAssuranceAttempt(outputPath); err != nil || !reflect.DeepEqual(loaded, attempt) {
		t.Fatalf("LoadAssuranceAttempt = %#v, %v", loaded, err)
	}
}

func TestCreateAssuranceAttemptRejectsMismatchedAuthorityOrDirtySource(t *testing.T) {
	root := t.TempDir()
	candidateRoot := newInventoryRepository(t, map[string]string{"main.go": "package main\n"})
	toolsRoot := newInventoryRepository(t, map[string]string{"main.go": "package main\n"})
	candidatePath, toolsPath := filepath.Join(root, "glade"), filepath.Join(root, "glade-tools")
	for _, path := range []string{candidatePath, toolsPath} {
		if err := os.WriteFile(path, []byte(filepath.Base(path)), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	inventoryPath := filepath.Join(root, "IN_SCOPE.json")
	if err := WriteNewJSON(inventoryPath, InventorySpec{SchemaVersion: 1, Scope: "private-corpus-assurance", Repositories: []InventoryEntry{{ID: "private-corpus-001", CheckoutPath: candidateRoot, ExpectedCommit: testGitOutput(t, candidateRoot, "rev-parse", "HEAD")}}}); err != nil {
		t.Fatal(err)
	}
	candidate := sealedAttemptCandidate{Commit: testGitOutput(t, candidateRoot, "rev-parse", "HEAD"), Path: candidatePath, SHA256: fileSHA256(t, candidatePath)}
	authorityPath := filepath.Join(root, "RECONCILIATION.json")
	writeAttemptAuthority(t, authorityPath, candidate, candidateRoot)
	cleanupAuthorities := writeAttemptCleanupAuthorities(t, root, inventoryPath, fileSHA256(t, authorityPath), candidate, toolsRoot, toolsPath)
	for name, mutate := range map[string]func(){
		"candidate hash": func() {
			data, err := os.ReadFile(authorityPath)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(authorityPath, []byte(strings.ReplaceAll(string(data), candidate.SHA256, strings.Repeat("f", 64))), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"dirty tools": func() {
			if err := os.WriteFile(filepath.Join(toolsRoot, "dirty"), []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			if name == "candidate hash" {
				writeAttemptAuthority(t, authorityPath, candidate, candidateRoot)
			}
			mutate()
			if _, err := CreateAssuranceAttempt(AssuranceAttemptRequest{InventoryPath: inventoryPath, CandidateAuthorityPath: authorityPath, CandidatePath: candidatePath, CandidateRoot: candidateRoot, ToolsPath: toolsPath, ToolsRoot: toolsRoot, RemoteCleanupAuthorityPaths: cleanupAuthorities, OutputPath: filepath.Join(root, name+".json")}); err == nil {
				t.Fatal("CreateAssuranceAttempt accepted an unsealed runtime")
			}
		})
	}
}

type sealedAttemptCandidate struct{ Commit, Path, SHA256 string }

func writeAttemptAuthority(t *testing.T, path string, candidate sealedAttemptCandidate, candidateRoot string) {
	t.Helper()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	runRoot := t.TempDir()
	productRoot := filepath.Join(runRoot, "integration", "glade")
	if err := os.MkdirAll(filepath.Dir(productRoot), 0o700); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("git", "clone", "--quiet", candidateRoot, productRoot).CombinedOutput(); err != nil {
		t.Fatalf("git clone: %v\n%s", err, output)
	}
	candidatePath := filepath.Join(runRoot, "evidence", "current-base", "candidate", "glade")
	if err := os.MkdirAll(filepath.Dir(candidatePath), 0o700); err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(candidate.Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(candidatePath, binary, 0o700); err != nil {
		t.Fatal(err)
	}
	pathForRun := func(value string) string {
		return filepath.ToSlash(strings.TrimPrefix(value, runRoot+string(filepath.Separator)))
	}
	receiptPath := filepath.Join(filepath.Dir(candidatePath), "candidate-receipt.json")
	writeCandidateAuthorityJSON(t, receiptPath, map[string]any{"schemaVersion": 1, "status": "clean-exact-candidate", "sourceCommit": candidate.Commit, "binarySha256": candidate.SHA256, "cleanWorktree": true})
	manifestPath := filepath.Join(filepath.Dir(candidatePath), "candidate-manifest.json")
	writeCandidateAuthorityJSON(t, manifestPath, map[string]any{"candidate": map[string]any{"commit": candidate.Commit, "path": pathForRun(candidatePath), "sha256": candidate.SHA256}})
	reviewPath := filepath.Join(filepath.Dir(candidatePath), "REVIEW.md")
	if err := os.WriteFile(reviewPath, []byte("Verdict: PASS\nCandidate commit: "+candidate.Commit+"\nCandidate SHA-256: "+candidate.SHA256+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeCandidateAuthorityJSON(t, filepath.Join(runRoot, "run.json"), map[string]any{"currentBase": map[string]any{"candidate": map[string]any{"productCommit": candidate.Commit, "sha256": candidate.SHA256}}})
	writeCandidateAuthorityJSON(t, filepath.Join(runRoot, "evidence", "current-base", "review-freeze.json"), map[string]any{"candidateCommit": candidate.Commit, "candidateSha256": candidate.SHA256})
	rebindPath := filepath.Join(runRoot, "evidence", "current-base", "current-base-candidate-rebind.json")
	writeCandidateAuthorityJSON(t, rebindPath, map[string]any{"status": "PASS", "manifest": pathForRun(manifestPath), "terraReview": pathForRun(reviewPath), "newCandidateCommit": candidate.Commit, "newCandidateSha256": candidate.SHA256, "candidatePath": pathForRun(candidatePath), "buildReceipt": pathForRun(receiptPath)})
	if _, err := CreateCandidateAuthority(CandidateAuthorityRequest{RunRoot: runRoot, RebindPath: rebindPath, OutputPath: path}); err != nil {
		t.Fatal(err)
	}
}

func writeAttemptCleanupAuthorities(t *testing.T, root, inventoryPath, candidateAuthoritySHA string, candidate sealedAttemptCandidate, toolsRoot, toolsPath string) map[string]string {
	t.Helper()
	toolsCommit := testGitOutput(t, toolsRoot, "rev-parse", "HEAD")
	base := AssuranceAttempt{SchemaVersion: 1, InventorySHA256: fileSHA256(t, inventoryPath), CandidateAuthoritySHA256: candidateAuthoritySHA, Candidate: RuntimeArtifact{Commit: candidate.Commit, OS: runtime.GOOS, Arch: runtime.GOARCH, SHA256: candidate.SHA256}, Tools: RuntimeArtifact{Commit: toolsCommit, OS: runtime.GOOS, Arch: runtime.GOARCH, SHA256: fileSHA256(t, toolsPath)}}
	attemptSHA := attemptBindingHash(base)
	parent := filepath.Join(root, "glade-assurance-worker")
	if err := os.MkdirAll(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	paths := map[string]string{}
	for _, role := range []string{"replay-worker", "salesforce-worker"} {
		path := filepath.Join(root, role+"-authority.json")
		authority := RemoteAttemptAuthority{SchemaVersion: 1, AttemptSHA256: attemptSHA, Role: role, Host: "operator@" + role, Parent: parent, AttemptRoot: filepath.Join(parent, "assurance-"+attemptSHA[:16]+"-test-"+role)}
		if err := WriteNewJSON(path, authority); err != nil {
			t.Fatal(err)
		}
		paths[role] = path
	}
	return paths
}

func assuranceAttemptForTest(t *testing.T, inventoryPath string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ATTEMPT.json")
	attempt := AssuranceAttempt{SchemaVersion: 1, InventorySHA256: fileSHA256(t, inventoryPath), CandidateAuthoritySHA256: strings.Repeat("a", 64), Candidate: replayRuntime("b"), Tools: replayRuntime("c"), RemoteCleanupAuthoritySHA256: testCleanupAuthorityHashes()}
	if err := WriteNewJSON(path, attempt); err != nil {
		t.Fatal(err)
	}
	return path
}

func assuranceAttemptForRuntimes(t *testing.T, root string, candidate, tools RuntimeArtifact) string {
	t.Helper()
	path := filepath.Join(root, "ATTEMPT.json")
	attempt := AssuranceAttempt{SchemaVersion: 1, InventorySHA256: strings.Repeat("a", 64), CandidateAuthoritySHA256: strings.Repeat("b", 64), Candidate: candidate, Tools: tools, RemoteCleanupAuthoritySHA256: testCleanupAuthorityHashes()}
	if err := WriteNewJSON(path, attempt); err != nil {
		t.Fatal(err)
	}
	return path
}

func replaceAssuranceAttemptForRuntimes(t *testing.T, path string, candidate, tools RuntimeArtifact) {
	t.Helper()
	attempt := AssuranceAttempt{SchemaVersion: 1, InventorySHA256: strings.Repeat("a", 64), CandidateAuthoritySHA256: strings.Repeat("b", 64), Candidate: candidate, Tools: tools, RemoteCleanupAuthoritySHA256: testCleanupAuthorityHashes()}
	data, err := json.Marshal(attempt)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func testCleanupAuthorityHashes() map[string]string {
	return map[string]string{"replay-worker": strings.Repeat("0", 64), "salesforce-worker": strings.Repeat("0", 64)}
}
