package corpusassurance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCreateCandidateAuthorityDerivesOnlySealedReceiptCandidate(t *testing.T) {
	root := t.TempDir()
	candidateRoot := newInventoryRepository(t, map[string]string{"main.go": "package main\n"})
	toolsRoot := newInventoryRepository(t, map[string]string{"main.go": "package main\n"})
	candidatePath := filepath.Join(root, "glade")
	toolsPath, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(candidatePath, []byte("candidate"), 0o700); err != nil {
		t.Fatal(err)
	}
	candidate := sealedAttemptCandidate{Commit: testGitOutput(t, candidateRoot, "rev-parse", "HEAD"), Path: candidatePath, SHA256: fileSHA256(t, candidatePath)}
	tools := candidateToolForTest(t, toolsRoot, toolsPath)
	receiptPath := filepath.Join(root, "candidate-receipt.json")
	writeCandidateAuthorityJSON(t, receiptPath, map[string]any{"schemaVersion": 1, "status": "clean-exact-candidate", "sourceCommit": candidate.Commit, "binarySha256": candidate.SHA256, "cleanWorktree": true, "candidate": attemptCandidate(candidate), "tools": tools})
	reviewPath := filepath.Join(root, "REVIEW.md")
	if err := os.WriteFile(reviewPath, candidateAuthorityReviewForTest(attemptCandidate(candidate), tools), 0o600); err != nil {
		t.Fatal(err)
	}
	authorityPath := filepath.Join(root, "CANDIDATE_AUTHORITY.json")
	authority, err := CreateCandidateAuthority(CandidateAuthorityRequest{CandidateRoot: candidateRoot, ReceiptPath: receiptPath, ReviewPath: reviewPath, OutputPath: authorityPath})
	if err != nil {
		t.Fatalf("CreateCandidateAuthority: %v", err)
	}
	want := candidateAuthorityInput{Candidate: attemptCandidate(candidate), Tools: tools}
	if authority != want {
		t.Fatalf("authority = %#v, want %#v", authority, want)
	}
	if got, _, err := readCandidateAuthority(authorityPath); err != nil || got != want {
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
	receiptBytes, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	unknownReceiptPath := filepath.Join(root, "unknown-candidate-receipt.json")
	unknownReceipt := strings.TrimSuffix(string(receiptBytes), "}") + `,"untrusted":true}`
	if err := os.WriteFile(unknownReceiptPath, []byte(unknownReceipt), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateCandidateAuthority(CandidateAuthorityRequest{CandidateRoot: candidateRoot, ReceiptPath: unknownReceiptPath, ReviewPath: reviewPath, OutputPath: filepath.Join(root, "UNKNOWN_RECEIPT_AUTHORITY.json")}); err == nil {
		t.Fatal("CreateCandidateAuthority accepted an unknown receipt member")
	}
	data, err := os.ReadFile(authorityPath)
	if err != nil {
		t.Fatal(err)
	}
	var platformTampered candidateAuthorityDocument
	if err := json.Unmarshal(data, &platformTampered); err != nil {
		t.Fatal(err)
	}
	platformTampered.Binding.Tools.Arch = "amd64"
	platformTampered.BoundInputs.Tools.Arch = "amd64"
	platformAuthorityPath := filepath.Join(root, "PLATFORM_TAMPERED_AUTHORITY.json")
	writeCandidateAuthorityJSON(t, platformAuthorityPath, platformTampered)
	if _, _, err := readCandidateAuthority(platformAuthorityPath); err == nil {
		t.Fatal("readCandidateAuthority accepted a tools platform change")
	}
	legacy := strings.TrimSuffix(string(data), "}\n") + `,"candidateRebind":{"path":"/legacy","sha256":"` + strings.Repeat("0", 64) + `"}}` + "\n"
	if err := os.WriteFile(authorityPath, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readCandidateAuthority(authorityPath); err == nil {
		t.Fatal("readCandidateAuthority accepted a legacy authority member")
	}
}

func TestCreateCandidateAuthorityRejectsToolsThatAreNotExecuting(t *testing.T) {
	root := t.TempDir()
	candidateRoot := newInventoryRepository(t, map[string]string{"main.go": "package main\n"})
	toolsRoot := newInventoryRepository(t, map[string]string{"main.go": "package main\n"})
	candidatePath, toolsPath := filepath.Join(root, "glade"), filepath.Join(root, "other-glade-tools")
	for _, path := range []string{candidatePath, toolsPath} {
		if err := os.WriteFile(path, []byte(filepath.Base(path)), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	candidate := sealedAttemptCandidate{Commit: testGitOutput(t, candidateRoot, "rev-parse", "HEAD"), Path: candidatePath, SHA256: fileSHA256(t, candidatePath)}
	tools := candidateToolForTest(t, toolsRoot, toolsPath)
	receiptPath := filepath.Join(root, "candidate-receipt.json")
	writeCandidateAuthorityJSON(t, receiptPath, map[string]any{"schemaVersion": 1, "status": "clean-exact-candidate", "sourceCommit": candidate.Commit, "binarySha256": candidate.SHA256, "cleanWorktree": true, "candidate": attemptCandidate(candidate), "tools": tools})
	reviewPath := filepath.Join(root, "REVIEW.md")
	if err := os.WriteFile(reviewPath, candidateAuthorityReviewForTest(attemptCandidate(candidate), tools), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateCandidateAuthority(CandidateAuthorityRequest{CandidateRoot: candidateRoot, ReceiptPath: receiptPath, ReviewPath: reviewPath, OutputPath: filepath.Join(root, "CANDIDATE_AUTHORITY.json")}); err == nil {
		t.Fatal("CreateCandidateAuthority accepted tools that are not executing")
	}
}

func TestCreateAssuranceAttemptRejectsToolsOutsideCandidateAuthority(t *testing.T) {
	root := t.TempDir()
	candidateRoot := newInventoryRepository(t, map[string]string{"main.go": "package main\n"})
	sealedToolsRoot := newInventoryRepository(t, map[string]string{"main.go": "package main\n"})
	candidatePath := filepath.Join(root, "glade")
	if err := os.WriteFile(candidatePath, []byte(filepath.Base(candidatePath)), 0o700); err != nil {
		t.Fatal(err)
	}
	sealedToolsPath, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	alternateToolsPath := filepath.Join(root, "alternate-tools")
	if err := os.Link(sealedToolsPath, alternateToolsPath); err != nil {
		t.Fatal(err)
	}
	candidate := sealedAttemptCandidate{Commit: testGitOutput(t, candidateRoot, "rev-parse", "HEAD"), Path: candidatePath, SHA256: fileSHA256(t, candidatePath)}
	sealedTools := candidateToolForTest(t, sealedToolsRoot, sealedToolsPath)
	receiptPath := filepath.Join(root, "candidate-receipt.json")
	writeCandidateAuthorityJSON(t, receiptPath, map[string]any{"schemaVersion": 1, "status": "clean-exact-candidate", "sourceCommit": candidate.Commit, "binarySha256": candidate.SHA256, "cleanWorktree": true, "candidate": attemptCandidate(candidate), "tools": sealedTools})
	receipt, _, err := readExactCandidateBuildReceipt(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateCandidateTool(receipt.Tools); err != nil {
		t.Fatal(err)
	}
	if !validCandidateBuildReceipt(receipt, candidateAuthorityInput{Candidate: attemptCandidate(candidate), Tools: sealedTools}) {
		t.Fatal("candidate receipt did not preserve its sealed tools")
	}
	reviewPath := filepath.Join(root, "REVIEW.md")
	if err := os.WriteFile(reviewPath, candidateAuthorityReviewForTest(attemptCandidate(candidate), sealedTools), 0o600); err != nil {
		t.Fatal(err)
	}
	authorityPath := filepath.Join(root, "CANDIDATE_AUTHORITY.json")
	if _, err := CreateCandidateAuthority(CandidateAuthorityRequest{CandidateRoot: candidateRoot, ReceiptPath: receiptPath, ReviewPath: reviewPath, OutputPath: authorityPath}); err != nil {
		t.Fatal(err)
	}
	inventoryPath := filepath.Join(root, "IN_SCOPE.json")
	if err := WriteNewJSON(inventoryPath, InventorySpec{SchemaVersion: 1, Scope: "private-corpus-assurance", Repositories: []InventoryEntry{{ID: "private-corpus-001", CheckoutPath: candidateRoot, ExpectedCommit: candidate.Commit}}}); err != nil {
		t.Fatal(err)
	}
	cleanupAuthorities := writeAttemptCleanupAuthorities(t, root, inventoryPath, fileSHA256(t, authorityPath), candidate, sealedToolsRoot, alternateToolsPath)
	if _, err := CreateAssuranceAttempt(AssuranceAttemptRequest{InventoryPath: inventoryPath, CandidateAuthorityPath: authorityPath, CandidatePath: candidatePath, CandidateRoot: candidateRoot, ToolsPath: alternateToolsPath, ToolsRoot: sealedToolsRoot, RemoteCleanupAuthorityPaths: cleanupAuthorities, OutputPath: filepath.Join(root, "ATTEMPT.json")}); err == nil {
		t.Fatal("CreateAssuranceAttempt accepted tools outside candidate authority")
	}
}

func TestValidateCandidateAuthorityReviewBytes(t *testing.T) {
	candidate := attemptCandidate{Commit: strings.Repeat("a", 40), SHA256: strings.Repeat("b", 64)}
	tools := candidateTool{RuntimeArtifact: RuntimeArtifact{Commit: strings.Repeat("c", 40), OS: runtime.GOOS, Arch: runtime.GOARCH, SHA256: strings.Repeat("d", 64)}, Path: "/tools"}
	if err := validateCandidateAuthorityReviewBytes(candidateAuthorityReviewForTest(candidate, tools), candidate, tools); err != nil {
		t.Fatal(err)
	}
}

func TestCandidateAuthorityRejectsAmbiguousInputs(t *testing.T) {
	root := t.TempDir()
	candidate := attemptCandidate{Commit: strings.Repeat("a", 40), Path: "/candidate", SHA256: strings.Repeat("b", 64)}
	tools := candidateTool{RuntimeArtifact: RuntimeArtifact{Commit: strings.Repeat("c", 40), OS: "darwin", Arch: "arm64", SHA256: strings.Repeat("d", 64)}, Path: "/tools"}
	valid, err := json.Marshal(candidateBuildReceipt{SchemaVersion: 1, Status: "clean-exact-candidate", SourceCommit: candidate.Commit, BinarySHA256: candidate.SHA256, CleanWorktree: true, Candidate: candidate, Tools: tools})
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{
		"duplicate top level":   []byte(strings.Replace(string(valid), `"schemaVersion":1`, `"schemaVersion":1,"schemaVersion":1`, 1)),
		"case changed key":      []byte(strings.Replace(string(valid), `"schemaVersion"`, `"SchemaVersion"`, 1)),
		"duplicate nested tool": []byte(strings.Replace(string(valid), `"path":"/tools"`, `"path":"/tools","path":"/other-tools"`, 1)),
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(root, strings.ReplaceAll(name, " ", "-")+".json")
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, _, err := readExactCandidateBuildReceipt(path); err == nil {
				t.Fatal("readExactCandidateBuildReceipt accepted ambiguous JSON")
			}
		})
	}
	duplicateReview := append(candidateAuthorityReviewForTest(candidate, tools), []byte("Tools SHA-256: "+tools.SHA256+"\n")...)
	if err := validateCandidateAuthorityReviewBytes(duplicateReview, candidate, tools); err == nil {
		t.Fatal("validateCandidateAuthorityReviewBytes accepted a duplicate field")
	}
}

func TestCreateCandidateAuthorityRejectsInvalidReviewWithoutOutput(t *testing.T) {
	root := t.TempDir()
	candidateRoot := newInventoryRepository(t, map[string]string{"main.go": "package main\n"})
	toolsRoot := newInventoryRepository(t, map[string]string{"main.go": "package main\n"})
	candidatePath := filepath.Join(root, "glade")
	toolsPath, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(candidatePath, []byte("candidate"), 0o700); err != nil {
		t.Fatal(err)
	}
	candidate := sealedAttemptCandidate{Commit: testGitOutput(t, candidateRoot, "rev-parse", "HEAD"), Path: candidatePath, SHA256: fileSHA256(t, candidatePath)}
	tools := candidateToolForTest(t, toolsRoot, toolsPath)
	receiptPath := filepath.Join(root, "candidate-receipt.json")
	writeCandidateAuthorityJSON(t, receiptPath, map[string]any{"schemaVersion": 1, "status": "clean-exact-candidate", "sourceCommit": candidate.Commit, "binarySha256": candidate.SHA256, "cleanWorktree": true, "candidate": attemptCandidate(candidate), "tools": tools})
	reviewPath := filepath.Join(root, "REVIEW.md")
	if err := os.WriteFile(reviewPath, []byte("invalid\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(root, "CANDIDATE_AUTHORITY.json")
	if _, err := CreateCandidateAuthority(CandidateAuthorityRequest{CandidateRoot: candidateRoot, ReceiptPath: receiptPath, ReviewPath: reviewPath, OutputPath: outputPath}); err == nil {
		t.Fatal("CreateCandidateAuthority accepted an invalid review")
	}
	if _, err := os.Lstat(outputPath); !os.IsNotExist(err) {
		t.Fatalf("invalid review created authority: %v", err)
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

func candidateToolForTest(t *testing.T, root, path string) candidateTool {
	t.Helper()
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return candidateTool{RuntimeArtifact: RuntimeArtifact{Commit: testGitOutput(t, root, "rev-parse", "HEAD"), OS: runtime.GOOS, Arch: runtime.GOARCH, SHA256: fileSHA256(t, path)}, Path: filepath.Clean(canonical)}
}

func candidateAuthorityReviewForTest(candidate attemptCandidate, tools candidateTool) []byte {
	return []byte("Verdict: PASS\nCandidate commit: " + candidate.Commit + "\nCandidate SHA-256: " + candidate.SHA256 + "\nTools commit: " + tools.Commit + "\nTools OS: " + tools.OS + "\nTools arch: " + tools.Arch + "\nTools SHA-256: " + tools.SHA256 + "\nTools path: " + tools.Path + "\n")
}
