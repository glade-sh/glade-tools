package corpusassurance

import (
	"encoding/json"
	"os"
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
	toolsPath, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	writeCandidateAuthorityExecutable(t, candidatePath, true)
	inventoryPath := filepath.Join(root, "IN_SCOPE.json")
	if err := WriteNewJSON(inventoryPath, InventorySpec{SchemaVersion: 1, Scope: "private-corpus-assurance", Repositories: []InventoryEntry{{ID: "private-corpus-001", CheckoutPath: candidateRoot, ExpectedCommit: testGitOutput(t, candidateRoot, "rev-parse", "HEAD")}}}); err != nil {
		t.Fatal(err)
	}
	candidate := sealedAttemptCandidate{Commit: testGitOutput(t, candidateRoot, "rev-parse", "HEAD"), Path: candidatePath, SHA256: fileSHA256(t, candidatePath)}
	authorityPath := filepath.Join(root, "RECONCILIATION.json")
	writeAttemptAuthority(t, authorityPath, candidate, candidateRoot, candidateToolForTest(t, toolsRoot, toolsPath))
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
	candidatePath := filepath.Join(root, "glade")
	writeCandidateAuthorityExecutable(t, candidatePath, true)
	toolsPath, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	inventoryPath := filepath.Join(root, "IN_SCOPE.json")
	if err := WriteNewJSON(inventoryPath, InventorySpec{SchemaVersion: 1, Scope: "private-corpus-assurance", Repositories: []InventoryEntry{{ID: "private-corpus-001", CheckoutPath: candidateRoot, ExpectedCommit: testGitOutput(t, candidateRoot, "rev-parse", "HEAD")}}}); err != nil {
		t.Fatal(err)
	}
	candidate := sealedAttemptCandidate{Commit: testGitOutput(t, candidateRoot, "rev-parse", "HEAD"), Path: candidatePath, SHA256: fileSHA256(t, candidatePath)}
	authorityPath := filepath.Join(root, "RECONCILIATION.json")
	writeAttemptAuthority(t, authorityPath, candidate, candidateRoot, candidateToolForTest(t, toolsRoot, toolsPath))
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
				writeAttemptAuthority(t, authorityPath, candidate, candidateRoot, candidateToolForTest(t, toolsRoot, toolsPath))
			}
			mutate()
			if _, err := CreateAssuranceAttempt(AssuranceAttemptRequest{InventoryPath: inventoryPath, CandidateAuthorityPath: authorityPath, CandidatePath: candidatePath, CandidateRoot: candidateRoot, ToolsPath: toolsPath, ToolsRoot: toolsRoot, RemoteCleanupAuthorityPaths: cleanupAuthorities, OutputPath: filepath.Join(root, name+".json")}); err == nil {
				t.Fatal("CreateAssuranceAttempt accepted an unsealed runtime")
			}
		})
	}
}

type sealedAttemptCandidate struct{ Commit, Path, SHA256 string }

func writeAttemptAuthority(t *testing.T, path string, candidate sealedAttemptCandidate, candidateRoot string, tools candidateTool) {
	t.Helper()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	receiptPath := filepath.Join(filepath.Dir(path), "candidate-receipt.json")
	writeCandidateAuthorityJSON(t, receiptPath, map[string]any{"schemaVersion": 1, "status": "clean-exact-candidate", "sourceCommit": candidate.Commit, "binarySha256": candidate.SHA256, "cleanWorktree": true, "candidate": attemptCandidate(candidate), "tools": tools})
	reviewPath := filepath.Join(filepath.Dir(path), "REVIEW.md")
	if err := os.WriteFile(reviewPath, candidateAuthorityReviewForTest(attemptCandidate(candidate), tools), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateCandidateAuthority(CandidateAuthorityRequest{CandidateRoot: candidateRoot, ReceiptPath: receiptPath, ReviewPath: reviewPath, OutputPath: path}); err != nil {
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
