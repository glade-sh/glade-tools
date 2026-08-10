package corpusassurance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateCandidateAuthorityDerivesOnlyGuardedRebindCandidate(t *testing.T) {
	root := t.TempDir()
	runRoot := filepath.Join(root, "run")
	candidateRoot := newInventoryRepository(t, map[string]string{"main.go": "package main\n"})
	if err := os.MkdirAll(filepath.Join(runRoot, "integration"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(candidateRoot, filepath.Join(runRoot, "integration", "glade")); err != nil {
		t.Fatal(err)
	}
	candidateRoot = filepath.Join(runRoot, "integration", "glade")
	candidatePath := filepath.Join(runRoot, "evidence", "current-base", "candidate", "glade")
	if err := os.MkdirAll(filepath.Dir(candidatePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(candidatePath, []byte("candidate"), 0o700); err != nil {
		t.Fatal(err)
	}
	commit := testGitOutput(t, candidateRoot, "rev-parse", "HEAD")
	candidate := sealedAttemptCandidate{Commit: commit, Path: candidatePath, SHA256: fileSHA256(t, candidatePath)}
	receiptPath := filepath.Join(filepath.Dir(candidatePath), "candidate-receipt.json")
	writeCandidateAuthorityJSON(t, receiptPath, map[string]any{"schemaVersion": 1, "status": "clean-exact-candidate", "sourceCommit": candidate.Commit, "binarySha256": candidate.SHA256, "cleanWorktree": true})
	manifestPath := filepath.Join(filepath.Dir(candidatePath), "candidate-manifest.json")
	writeCandidateAuthorityJSON(t, manifestPath, map[string]any{"candidate": map[string]any{"commit": candidate.Commit, "path": filepath.ToSlash(strings.TrimPrefix(candidate.Path, runRoot+string(filepath.Separator))), "sha256": candidate.SHA256}})
	reviewPath := filepath.Join(filepath.Dir(candidatePath), "REVIEW.md")
	review := "Verdict: PASS\nCandidate commit: " + candidate.Commit + "\nCandidate SHA-256: " + candidate.SHA256 + "\n"
	if err := os.WriteFile(reviewPath, []byte(review), 0o600); err != nil {
		t.Fatal(err)
	}
	writeCandidateAuthorityJSON(t, filepath.Join(runRoot, "run.json"), map[string]any{"currentBase": map[string]any{"candidate": map[string]any{"productCommit": candidate.Commit, "sha256": candidate.SHA256}}})
	writeCandidateAuthorityJSON(t, filepath.Join(runRoot, "evidence", "current-base", "review-freeze.json"), map[string]any{"candidateCommit": candidate.Commit, "candidateSha256": candidate.SHA256})
	rebindPath := filepath.Join(runRoot, "evidence", "current-base", "current-base-candidate-rebind.json")
	writeCandidateAuthorityJSON(t, rebindPath, map[string]any{"status": "PASS", "manifest": relCandidateAuthorityPath(t, runRoot, manifestPath), "terraReview": relCandidateAuthorityPath(t, runRoot, reviewPath), "newCandidateCommit": candidate.Commit, "newCandidateSha256": candidate.SHA256, "candidatePath": relCandidateAuthorityPath(t, runRoot, candidate.Path), "buildReceipt": relCandidateAuthorityPath(t, runRoot, receiptPath)})

	authorityPath := filepath.Join(root, "CANDIDATE_AUTHORITY.json")
	authority, err := CreateCandidateAuthority(CandidateAuthorityRequest{RunRoot: runRoot, RebindPath: rebindPath, OutputPath: authorityPath})
	if err != nil {
		t.Fatalf("CreateCandidateAuthority: %v", err)
	}
	if authority.Commit != candidate.Commit || authority.SHA256 != candidate.SHA256 {
		t.Fatalf("candidate = %#v, want %#v", authority, candidate)
	}
	if got, _, err := readCandidateAuthority(authorityPath); err != nil || got.Commit != candidate.Commit || got.SHA256 != candidate.SHA256 {
		t.Fatalf("readCandidateAuthority = %#v, %v", got, err)
	}

	for _, path := range []string{receiptPath, manifestPath, reviewPath} {
		original, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(original, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := readCandidateAuthority(authorityPath); err == nil {
			t.Fatalf("readCandidateAuthority accepted a changed bound input: %s", path)
		}
		if err := os.WriteFile(path, original, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func writeCandidateAuthorityJSON(t *testing.T, path string, value any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func relCandidateAuthorityPath(t *testing.T, root, path string) string {
	t.Helper()
	rel, err := filepath.Rel(root, path)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.ToSlash(rel)
}
