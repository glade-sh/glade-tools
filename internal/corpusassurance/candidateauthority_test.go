package corpusassurance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateCandidateAuthorityDerivesOnlySealedReceiptCandidate(t *testing.T) {
	root := t.TempDir()
	candidateRoot := newInventoryRepository(t, map[string]string{"main.go": "package main\n"})
	candidatePath := filepath.Join(root, "glade")
	if err := os.WriteFile(candidatePath, []byte("candidate"), 0o700); err != nil {
		t.Fatal(err)
	}
	candidate := sealedAttemptCandidate{Commit: testGitOutput(t, candidateRoot, "rev-parse", "HEAD"), Path: candidatePath, SHA256: fileSHA256(t, candidatePath)}
	receiptPath := filepath.Join(root, "candidate-receipt.json")
	writeCandidateAuthorityJSON(t, receiptPath, map[string]any{"schemaVersion": 1, "status": "clean-exact-candidate", "sourceCommit": candidate.Commit, "binarySha256": candidate.SHA256, "cleanWorktree": true, "candidate": candidate})
	reviewPath := filepath.Join(root, "REVIEW.md")
	if err := os.WriteFile(reviewPath, []byte("Verdict: PASS\nCandidate commit: "+candidate.Commit+"\nCandidate SHA-256: "+candidate.SHA256+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	authorityPath := filepath.Join(root, "CANDIDATE_AUTHORITY.json")
	authority, err := CreateCandidateAuthority(CandidateAuthorityRequest{CandidateRoot: candidateRoot, ReceiptPath: receiptPath, ReviewPath: reviewPath, OutputPath: authorityPath})
	if err != nil {
		t.Fatalf("CreateCandidateAuthority: %v", err)
	}
	if authority != attemptCandidate(candidate) {
		t.Fatalf("candidate = %#v, want %#v", authority, candidate)
	}
	if got, _, err := readCandidateAuthority(authorityPath); err != nil || got != attemptCandidate(candidate) {
		t.Fatalf("readCandidateAuthority = %#v, %v", got, err)
	}
	for _, path := range []string{receiptPath, reviewPath} {
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
	data, err := os.ReadFile(authorityPath)
	if err != nil {
		t.Fatal(err)
	}
	legacy := strings.TrimSuffix(string(data), "}\n") + `,"candidateRebind":{"path":"/legacy","sha256":"` + strings.Repeat("0", 64) + `"}}` + "\n"
	if err := os.WriteFile(authorityPath, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readCandidateAuthority(authorityPath); err == nil {
		t.Fatal("readCandidateAuthority accepted a legacy authority member")
	}
}

func TestValidateCandidateAuthorityReviewBytes(t *testing.T) {
	candidate := attemptCandidate{Commit: strings.Repeat("a", 40), SHA256: strings.Repeat("b", 64)}
	data := []byte("Verdict: PASS\nCandidate commit: " + candidate.Commit + "\nCandidate SHA-256: " + candidate.SHA256 + "\n")
	if err := validateCandidateAuthorityReviewBytes(data, candidate); err != nil {
		t.Fatal(err)
	}
}

func writeCandidateAuthorityJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
