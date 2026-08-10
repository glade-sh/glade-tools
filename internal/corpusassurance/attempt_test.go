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
	writeAttemptAuthority(t, authorityPath, candidate)
	outputPath := filepath.Join(root, "ATTEMPT.json")
	attempt, err := CreateAssuranceAttempt(AssuranceAttemptRequest{InventoryPath: inventoryPath, CandidateAuthorityPath: authorityPath, CandidatePath: candidatePath, CandidateRoot: candidateRoot, ToolsPath: toolsPath, ToolsRoot: toolsRoot, OutputPath: outputPath})
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
	writeAttemptAuthority(t, authorityPath, candidate)
	for name, mutate := range map[string]func(){
		"candidate hash": func() {
			writeAttemptAuthority(t, authorityPath, sealedAttemptCandidate{Commit: candidate.Commit, Path: candidate.Path, SHA256: strings.Repeat("f", 64)})
		},
		"dirty tools": func() {
			if err := os.WriteFile(filepath.Join(toolsRoot, "dirty"), []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			if name == "candidate hash" {
				writeAttemptAuthority(t, authorityPath, candidate)
			}
			mutate()
			if _, err := CreateAssuranceAttempt(AssuranceAttemptRequest{InventoryPath: inventoryPath, CandidateAuthorityPath: authorityPath, CandidatePath: candidatePath, CandidateRoot: candidateRoot, ToolsPath: toolsPath, ToolsRoot: toolsRoot, OutputPath: filepath.Join(root, name+".json")}); err == nil {
				t.Fatal("CreateAssuranceAttempt accepted an unsealed runtime")
			}
		})
	}
}

type sealedAttemptCandidate struct{ Commit, Path, SHA256 string }

func writeAttemptAuthority(t *testing.T, path string, candidate sealedAttemptCandidate) {
	t.Helper()
	data := `{"schemaVersion":1,"status":"closed-with-waivers","binding":{"candidate":{"commit":"` + candidate.Commit + `","path":"` + candidate.Path + `","sha256":"` + candidate.SHA256 + `"}},"boundInputs":{"candidate":{"commit":"` + candidate.Commit + `","path":"` + candidate.Path + `","sha256":"` + candidate.SHA256 + `"}},"candidateRebind":{"path":"/private/rebind.json","sha256":"` + strings.Repeat("a", 64) + `"}}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assuranceAttemptForTest(t *testing.T, inventoryPath string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ATTEMPT.json")
	attempt := AssuranceAttempt{SchemaVersion: 1, InventorySHA256: fileSHA256(t, inventoryPath), CandidateAuthoritySHA256: strings.Repeat("a", 64), Candidate: replayRuntime("b"), Tools: replayRuntime("c")}
	if err := WriteNewJSON(path, attempt); err != nil {
		t.Fatal(err)
	}
	return path
}

func assuranceAttemptForRuntimes(t *testing.T, root string, candidate, tools RuntimeArtifact) string {
	t.Helper()
	path := filepath.Join(root, "ATTEMPT.json")
	attempt := AssuranceAttempt{SchemaVersion: 1, InventorySHA256: strings.Repeat("a", 64), CandidateAuthoritySHA256: strings.Repeat("b", 64), Candidate: candidate, Tools: tools}
	if err := WriteNewJSON(path, attempt); err != nil {
		t.Fatal(err)
	}
	return path
}

func replaceAssuranceAttemptForRuntimes(t *testing.T, path string, candidate, tools RuntimeArtifact) {
	t.Helper()
	attempt := AssuranceAttempt{SchemaVersion: 1, InventorySHA256: strings.Repeat("a", 64), CandidateAuthoritySHA256: strings.Repeat("b", 64), Candidate: candidate, Tools: tools}
	data, err := json.Marshal(attempt)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}
