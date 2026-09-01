package corpusassurance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCreateCandidateAuthorityDerivesOnlySealedReceiptCandidate(t *testing.T) {
	root := t.TempDir()
	candidateRoot, toolsRoot := newPairedBuildRepositories(t, "package main\n", "package main\n")
	candidatePath := filepath.Join(root, "glade")
	toolsPath, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	writeCandidateAuthorityExecutable(t, candidatePath, candidateRoot, true)
	candidate := sealedAttemptCandidate{Commit: testGitOutput(t, candidateRoot, "rev-parse", "HEAD"), Path: candidatePath, SHA256: fileSHA256(t, candidatePath)}
	tools := candidateToolForTest(t, toolsRoot, toolsPath)
	receiptPath := filepath.Join(root, "candidate-receipt.json")
	writeCandidateBuildReceiptForTest(t, receiptPath, candidate, tools)
	reviewPath := filepath.Join(root, "REVIEW.md")
	if err := os.WriteFile(reviewPath, candidateAuthorityReviewForTest(attemptCandidate(candidate), tools), 0o600); err != nil {
		t.Fatal(err)
	}
	authorityPath := filepath.Join(root, "CANDIDATE_AUTHORITY.json")
	authority, err := CreateCandidateAuthority(CandidateAuthorityRequest{CandidateRoot: candidateRoot, ToolsRoot: toolsRoot, ReceiptPath: receiptPath, ReviewPath: reviewPath, OutputPath: authorityPath})
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
	if _, err := CreateCandidateAuthority(CandidateAuthorityRequest{CandidateRoot: candidateRoot, ToolsRoot: toolsRoot, ReceiptPath: unknownReceiptPath, ReviewPath: reviewPath, OutputPath: filepath.Join(root, "UNKNOWN_RECEIPT_AUTHORITY.json")}); err == nil {
		t.Fatal("CreateCandidateAuthority accepted an unknown receipt member")
	}
	data, err := os.ReadFile(authorityPath)
	if err != nil {
		t.Fatal(err)
	}
	var goTampered candidateAuthorityDocument
	if err := json.Unmarshal(data, &goTampered); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "fake-go-ran")
	fakeGo := filepath.Join(root, "fake-go")
	if err := os.WriteFile(fakeGo, []byte("#!/bin/sh\n/usr/bin/touch '"+marker+"'\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	goTampered.ToolsBuild.Go = candidateAuthoritySource{Path: fakeGo, SHA256: fileSHA256(t, fakeGo)}
	goTamperedPath := filepath.Join(root, "GO_TAMPERED_AUTHORITY.json")
	writeCandidateAuthorityJSON(t, goTamperedPath, goTampered)
	if _, _, err := readCandidateAuthority(goTamperedPath); err == nil {
		t.Fatal("readCandidateAuthority accepted a changed Go executable")
	}
	if _, err := os.Lstat(marker); !os.IsNotExist(err) {
		t.Fatalf("readCandidateAuthority executed an untrusted Go executable: %v", err)
	}
	var platformTampered candidateAuthorityDocument
	if err := json.Unmarshal(data, &platformTampered); err != nil {
		t.Fatal(err)
	}
	tamperedArch := "amd64"
	if runtime.GOARCH == tamperedArch {
		tamperedArch = "arm64"
	}
	platformTampered.Binding.Tools.Arch = tamperedArch
	platformTampered.BoundInputs.Tools.Arch = tamperedArch
	platformAuthorityPath := filepath.Join(root, "PLATFORM_TAMPERED_AUTHORITY.json")
	writeCandidateAuthorityJSON(t, platformAuthorityPath, platformTampered)
	if _, _, err := readCandidateAuthority(platformAuthorityPath); err == nil {
		t.Fatal("readCandidateAuthority accepted a tools platform change")
	}
	var toolsBuildTampered candidateAuthorityDocument
	if err := json.Unmarshal(data, &toolsBuildTampered); err != nil {
		t.Fatal(err)
	}
	toolsBuildTampered.ToolsBuild.SourceTree = strings.Repeat("f", 40)
	toolsBuildAuthorityPath := filepath.Join(root, "TOOLS_BUILD_TAMPERED_AUTHORITY.json")
	writeCandidateAuthorityJSON(t, toolsBuildAuthorityPath, toolsBuildTampered)
	if _, _, err := readCandidateAuthority(toolsBuildAuthorityPath); err == nil {
		t.Fatal("readCandidateAuthority accepted a changed tools source build")
	}
	legacy := strings.TrimSuffix(string(data), "}\n") + `,"candidateRebind":{"path":"/legacy","sha256":"` + strings.Repeat("0", 64) + `"}}` + "\n"
	if err := os.WriteFile(authorityPath, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readCandidateAuthority(authorityPath); err == nil {
		t.Fatal("readCandidateAuthority accepted a legacy authority member")
	}
}

func TestCandidateBuildValidatorBindsExactSource(t *testing.T) {
	root := newInventoryRepository(t, map[string]string{
		"go.mod":                               "module github.com/glade-sh/glade\n\ngo 1.23.0\n\nrequire github.com/glade-sh/apex-parser v0.1.0\n\nreplace github.com/glade-sh/apex-parser => ./third_party/glade-apex-parser\n",
		"cmd/glade/main.go":                    "package main\nimport \"fmt\"\nfunc main() { fmt.Println(`{\"command\":\"doctor\",\"parserOK\":true}`) }\n",
		"third_party/glade-apex-parser/go.mod": "module github.com/glade-sh/apex-parser\n\ngo 1.23.0\n",
	})
	binding, err := deriveCandidateBuildBinding(root)
	if err != nil {
		t.Fatal(err)
	}
	candidatePath := filepath.Join(t.TempDir(), "glade")
	if err := runBoundCandidateBuild(binding, candidatePath); err != nil {
		t.Fatal(err)
	}
	candidate := attemptCandidate{Commit: testGitOutput(t, root, "rev-parse", "HEAD"), Path: candidatePath, SHA256: fileSHA256(t, candidatePath)}
	if err := validateCandidateBuildFromSource(root, candidate, binding); err != nil {
		t.Fatalf("exact source build rejected: %v", err)
	}
	if err := os.WriteFile(candidatePath, append([]byte("not the build\n"), []byte(candidate.SHA256)...), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := validateCandidateBuildFromSource(root, candidate, binding); err == nil {
		t.Fatal("source build validation accepted an unrelated binary")
	}
}

func TestCandidateBuildBindingUsesCommitScopedCaches(t *testing.T) {
	root := newInventoryRepository(t, map[string]string{
		"go.mod":                               "module github.com/glade-sh/glade\n\ngo 1.23.0\n\nrequire github.com/glade-sh/apex-parser v0.1.0\n\nreplace github.com/glade-sh/apex-parser => ./third_party/glade-apex-parser\n",
		"cmd/glade/main.go":                    "package main\nfunc main() {}\n",
		"third_party/glade-apex-parser/go.mod": "module github.com/glade-sh/apex-parser\n\ngo 1.23.0\n",
	})
	commit := testGitOutput(t, root, "rev-parse", "HEAD")
	binding, err := deriveCandidateBuildBinding(root)
	if err != nil {
		t.Fatal(err)
	}
	environment := strings.Join(binding.Environment, "\n")
	for _, name := range []string{"GOCACHE=/tmp/glade-assurance-go-cache-" + commit[:12], "GOMODCACHE=/tmp/glade-assurance-go-mod-" + commit[:12]} {
		if !strings.Contains(environment, name) {
			t.Fatalf("candidate build environment = %q, missing %q", environment, name)
		}
	}
	if !equalStrings(binding.Arguments, []string{"build", "-buildvcs=false", "-trimpath", "-ldflags", "-s -w -X github.com/glade-sh/glade/internal/gladecli.Version=" + commit, "-o", "<candidate>", "./cmd/glade"}) {
		t.Fatalf("candidate build arguments = %#v", binding.Arguments)
	}
}

func TestCandidateAuthorityRejectsToolsBoundToAnotherCandidateRoot(t *testing.T) {
	candidateRoot, _ := newPairedBuildRepositories(t, "package main\n", "package main\n")
	otherCandidateRoot := newInventoryRepository(t, map[string]string{"go.mod": "module github.com/glade-sh/glade\n\ngo 1.23.0\n"})
	toolsRoot := newInventoryRepository(t, map[string]string{
		"go.mod":                  "module github.com/glade-sh/glade/tools\n\ngo 1.23.0\n\nrequire github.com/glade-sh/glade v0.0.0\n\nreplace github.com/glade-sh/glade => " + otherCandidateRoot + "\n",
		"cmd/glade-tools/main.go": "package main\n",
	})
	toolsBuild, err := deriveToolsBuildBinding(toolsRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateToolsCandidatePair(candidateRoot, toolsBuild); err == nil {
		t.Fatal("candidate authority accepted tools bound to another candidate root")
	}
}

func TestCandidateAuthorityRejectsNonSiblingSourceRoots(t *testing.T) {
	candidateRoot := newInventoryRepository(t, map[string]string{
		"go.mod":                               "module github.com/glade-sh/glade\n\ngo 1.23.0\n",
		"cmd/glade/main.go":                    "package main\n",
		"third_party/glade-apex-parser/go.mod": "module github.com/glade-sh/apex-parser\n\ngo 1.23.0\n",
	})
	toolsParent := t.TempDir()
	toolsRoot := filepath.Join(toolsParent, "glade-tools")
	candidateRelative, err := filepath.Rel(toolsRoot, candidateRoot)
	if err != nil {
		t.Fatal(err)
	}
	toolsRoot = newInventoryRepositoryAt(t, toolsRoot, map[string]string{
		"go.mod":                  "module github.com/glade-sh/glade/tools\n\ngo 1.23.0\n\nrequire (\n\tgithub.com/glade-sh/glade v0.0.0\n\tgithub.com/glade-sh/apex-parser v0.1.0\n)\n\nreplace github.com/glade-sh/glade => " + candidateRelative + "\n\nreplace github.com/glade-sh/apex-parser => " + filepath.Join(candidateRelative, "third_party", "glade-apex-parser") + "\n",
		"cmd/glade-tools/main.go": "package main\n",
	})
	toolsBuild, err := deriveToolsBuildBinding(toolsRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateToolsCandidatePair(candidateRoot, toolsBuild); err == nil {
		t.Fatal("candidate authority accepted non-sibling source roots")
	}
}

func TestCandidateAuthorityRejectsToolsBoundToAnotherParserRoot(t *testing.T) {
	candidateRoot, toolsRoot := newPairedBuildRepositories(t, "package main\n", "package main\n")
	otherParserRoot := newInventoryRepository(t, map[string]string{"go.mod": "module github.com/glade-sh/apex-parser\n\ngo 1.23.0\n"})
	goModPath := filepath.Join(toolsRoot, "go.mod")
	goMod, err := os.ReadFile(goModPath)
	if err != nil {
		t.Fatal(err)
	}
	goMod = []byte(strings.Replace(string(goMod), "../glade/third_party/glade-apex-parser", otherParserRoot, 1))
	if err := os.WriteFile(goModPath, goMod, 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, toolsRoot, "add", "go.mod")
	gitRun(t, toolsRoot, "commit", "--quiet", "-m", "replace parser")
	toolsBuild, err := deriveToolsBuildBinding(toolsRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateToolsCandidatePair(candidateRoot, toolsBuild); err == nil {
		t.Fatal("candidate authority accepted tools bound to another parser root")
	}
}

func TestCandidateAuthorityRejectsVersionSpecificReplacementOverride(t *testing.T) {
	candidateRoot, toolsRoot := newPairedBuildRepositories(t, "package main\n", "package main\n")
	alternateRoot := filepath.Join(candidateRoot, "alternate")
	if err := os.MkdirAll(alternateRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(alternateRoot, "go.mod"), []byte("module github.com/glade-sh/glade\n\ngo 1.23.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	goModPath := filepath.Join(toolsRoot, "go.mod")
	file, err := os.OpenFile(goModPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("\nreplace github.com/glade-sh/glade v0.0.0 => ../glade/alternate\n"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	gitRun(t, toolsRoot, "add", "go.mod")
	gitRun(t, toolsRoot, "commit", "--quiet", "-m", "override glade")
	toolsBuild, err := deriveToolsBuildBinding(toolsRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateToolsCandidatePair(candidateRoot, toolsBuild); err == nil {
		t.Fatal("candidate authority accepted a version-specific replacement override")
	}
}

func TestCandidateAuthorityRejectsAbsoluteCandidateReplacementPaths(t *testing.T) {
	for _, module := range []string{"glade", "parser"} {
		t.Run(module, func(t *testing.T) {
			candidateRoot, toolsRoot := newPairedBuildRepositories(t, "package main\n", "package main\n")
			goModPath := filepath.Join(toolsRoot, "go.mod")
			goMod, err := os.ReadFile(goModPath)
			if err != nil {
				t.Fatal(err)
			}
			if module == "glade" {
				goMod = []byte(strings.Replace(string(goMod), "github.com/glade-sh/glade => ../glade", "github.com/glade-sh/glade => "+candidateRoot, 1))
			} else {
				goMod = []byte(strings.Replace(string(goMod), "github.com/glade-sh/apex-parser => ../glade/third_party/glade-apex-parser", "github.com/glade-sh/apex-parser => "+filepath.Join(candidateRoot, "third_party", "glade-apex-parser"), 1))
			}
			if err := os.WriteFile(goModPath, goMod, 0o644); err != nil {
				t.Fatal(err)
			}
			gitRun(t, toolsRoot, "add", "go.mod")
			gitRun(t, toolsRoot, "commit", "--quiet", "-m", "use absolute replacement")
			toolsBuild, err := deriveToolsBuildBinding(toolsRoot)
			if err != nil {
				t.Fatal(err)
			}
			if err := validateToolsCandidatePair(candidateRoot, toolsBuild); err == nil {
				t.Fatalf("candidate authority accepted absolute %s replacement", module)
			}
		})
	}
}

func TestCreateCandidateAuthorityRejectsInvalidCandidateReplacementsBeforeRebuild(t *testing.T) {
	for _, replacement := range []string{"missing", "absolute", "external-relative", "symlink-escape"} {
		t.Run(replacement, func(t *testing.T) {
			root := t.TempDir()
			candidateRoot, toolsRoot := newPairedBuildRepositories(t, "package main\n", "package main\n")
			rewriteCandidateReplacementForTest(t, candidateRoot, replacement)
			candidatePath := filepath.Join(root, "glade")
			writeCandidateAuthorityExecutable(t, candidatePath, candidateRoot, true)
			toolsPath, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			candidate := sealedAttemptCandidate{Commit: testGitOutput(t, candidateRoot, "rev-parse", "HEAD"), Path: candidatePath, SHA256: fileSHA256(t, candidatePath)}
			tools := candidateToolForTest(t, toolsRoot, toolsPath)
			receiptPath := filepath.Join(root, "candidate-receipt.json")
			writeCandidateBuildReceiptForTest(t, receiptPath, candidate, tools)
			reviewPath := filepath.Join(root, "REVIEW.md")
			if err := os.WriteFile(reviewPath, candidateAuthorityReviewForTest(attemptCandidate(candidate), tools), 0o600); err != nil {
				t.Fatal(err)
			}
			outputPath := filepath.Join(root, "CANDIDATE_AUTHORITY.json")
			if _, err := CreateCandidateAuthority(CandidateAuthorityRequest{CandidateRoot: candidateRoot, ToolsRoot: toolsRoot, ReceiptPath: receiptPath, ReviewPath: reviewPath, OutputPath: outputPath}); err == nil {
				t.Fatalf("CreateCandidateAuthority accepted %s candidate replacement", replacement)
			}
			if _, err := os.Lstat(outputPath); !os.IsNotExist(err) {
				t.Fatalf("invalid candidate replacement created authority: %v", err)
			}
		})
	}
}

func TestToolsBuildValidatorBindsExactSource(t *testing.T) {
	root := newInventoryRepository(t, map[string]string{
		"go.mod":                  "module example.invalid/tools\n\ngo 1.22\n",
		"cmd/glade-tools/main.go": "package main\nfunc main() {}\n",
	})
	binding, err := deriveToolsBuildBinding(root)
	if err != nil {
		t.Fatal(err)
	}
	toolsPath := filepath.Join(t.TempDir(), "glade-tools")
	if err := runBoundCandidateBuild(binding, toolsPath); err != nil {
		t.Fatal(err)
	}
	tools := candidateTool{RuntimeArtifact: RuntimeArtifact{Commit: testGitOutput(t, root, "rev-parse", "HEAD"), OS: runtime.GOOS, Arch: runtime.GOARCH, SHA256: fileSHA256(t, toolsPath)}, Path: toolsPath}
	if err := validateToolsBuildFromSource(root, tools, binding); err != nil {
		t.Fatalf("exact tools source build rejected: %v", err)
	}
	if err := os.WriteFile(toolsPath, []byte("not the build\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := validateToolsBuildFromSource(root, tools, binding); err == nil {
		t.Fatal("source build validation accepted unrelated tools")
	}
}

func TestCreateCandidateAuthorityRejectsToolsThatAreNotExecuting(t *testing.T) {
	root := t.TempDir()
	candidateRoot := newInventoryRepository(t, map[string]string{"main.go": "package main\n"})
	toolsRoot := newInventoryRepository(t, map[string]string{"main.go": "package main\n"})
	candidatePath, toolsPath := filepath.Join(root, "glade"), filepath.Join(root, "other-glade-tools")
	writeCandidateAuthorityExecutable(t, candidatePath, candidateRoot, true)
	if err := os.WriteFile(toolsPath, []byte(filepath.Base(toolsPath)), 0o700); err != nil {
		t.Fatal(err)
	}
	candidate := sealedAttemptCandidate{Commit: testGitOutput(t, candidateRoot, "rev-parse", "HEAD"), Path: candidatePath, SHA256: fileSHA256(t, candidatePath)}
	tools := candidateToolForTest(t, toolsRoot, toolsPath)
	receiptPath := filepath.Join(root, "candidate-receipt.json")
	writeCandidateBuildReceiptForTest(t, receiptPath, candidate, tools)
	reviewPath := filepath.Join(root, "REVIEW.md")
	if err := os.WriteFile(reviewPath, candidateAuthorityReviewForTest(attemptCandidate(candidate), tools), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateCandidateAuthority(CandidateAuthorityRequest{CandidateRoot: candidateRoot, ToolsRoot: toolsRoot, ReceiptPath: receiptPath, ReviewPath: reviewPath, OutputPath: filepath.Join(root, "CANDIDATE_AUTHORITY.json")}); err == nil {
		t.Fatal("CreateCandidateAuthority accepted tools that are not executing")
	}
}

func TestCreateAssuranceAttemptRejectsToolsOutsideCandidateAuthority(t *testing.T) {
	root := t.TempDir()
	candidateRoot, sealedToolsRoot := newPairedBuildRepositories(t, "package main\n", "package main\n")
	candidatePath := filepath.Join(root, "glade")
	writeCandidateAuthorityExecutable(t, candidatePath, candidateRoot, true)
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
	writeCandidateBuildReceiptForTest(t, receiptPath, candidate, sealedTools)
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
	if _, err := CreateCandidateAuthority(CandidateAuthorityRequest{CandidateRoot: candidateRoot, ToolsRoot: sealedToolsRoot, ReceiptPath: receiptPath, ReviewPath: reviewPath, OutputPath: authorityPath}); err != nil {
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
	writeCandidateAuthorityExecutable(t, candidatePath, candidateRoot, true)
	candidate := sealedAttemptCandidate{Commit: testGitOutput(t, candidateRoot, "rev-parse", "HEAD"), Path: candidatePath, SHA256: fileSHA256(t, candidatePath)}
	tools := candidateToolForTest(t, toolsRoot, toolsPath)
	receiptPath := filepath.Join(root, "candidate-receipt.json")
	writeCandidateBuildReceiptForTest(t, receiptPath, candidate, tools)
	reviewPath := filepath.Join(root, "REVIEW.md")
	if err := os.WriteFile(reviewPath, []byte("invalid\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(root, "CANDIDATE_AUTHORITY.json")
	if _, err := CreateCandidateAuthority(CandidateAuthorityRequest{CandidateRoot: candidateRoot, ToolsRoot: toolsRoot, ReceiptPath: receiptPath, ReviewPath: reviewPath, OutputPath: outputPath}); err == nil {
		t.Fatal("CreateCandidateAuthority accepted an invalid review")
	}
	if _, err := os.Lstat(outputPath); !os.IsNotExist(err) {
		t.Fatalf("invalid review created authority: %v", err)
	}
}

func TestCreateCandidateAuthorityRejectsCandidateWithoutParser(t *testing.T) {
	root := t.TempDir()
	candidateRoot := newInventoryRepository(t, map[string]string{"main.go": "package main\n"})
	toolsRoot := newInventoryRepository(t, map[string]string{"main.go": "package main\n"})
	candidatePath := filepath.Join(root, "glade")
	writeCandidateAuthorityExecutable(t, candidatePath, candidateRoot, false)
	toolsPath, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	candidate := sealedAttemptCandidate{Commit: testGitOutput(t, candidateRoot, "rev-parse", "HEAD"), Path: candidatePath, SHA256: fileSHA256(t, candidatePath)}
	tools := candidateToolForTest(t, toolsRoot, toolsPath)
	receiptPath := filepath.Join(root, "candidate-receipt.json")
	writeCandidateBuildReceiptForTest(t, receiptPath, candidate, tools)
	reviewPath := filepath.Join(root, "REVIEW.md")
	if err := os.WriteFile(reviewPath, candidateAuthorityReviewForTest(attemptCandidate(candidate), tools), 0o600); err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(root, "CANDIDATE_AUTHORITY.json")
	if _, err := CreateCandidateAuthority(CandidateAuthorityRequest{CandidateRoot: candidateRoot, ToolsRoot: toolsRoot, ReceiptPath: receiptPath, ReviewPath: reviewPath, OutputPath: outputPath}); err == nil {
		t.Fatal("CreateCandidateAuthority accepted a candidate without its Apex parser")
	}
	if _, err := os.Lstat(outputPath); !os.IsNotExist(err) {
		t.Fatalf("parser-less candidate created authority: %v", err)
	}
}

func TestCreateCandidateAuthorityRejectsNonzeroCandidateChecks(t *testing.T) {
	for _, operation := range []string{"version", "doctor", "parse"} {
		t.Run(operation, func(t *testing.T) {
			root := t.TempDir()
			candidateRoot, toolsRoot := newPairedBuildRepositories(t, "package main\n", "package main\n")
			candidatePath := filepath.Join(root, "glade")
			writeCandidateAuthorityExecutableWithFailure(t, candidatePath, candidateRoot, true, operation)
			toolsPath, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			candidate := sealedAttemptCandidate{Commit: testGitOutput(t, candidateRoot, "rev-parse", "HEAD"), Path: candidatePath, SHA256: fileSHA256(t, candidatePath)}
			tools := candidateToolForTest(t, toolsRoot, toolsPath)
			receiptPath := filepath.Join(root, "candidate-receipt.json")
			writeCandidateBuildReceiptForTest(t, receiptPath, candidate, tools)
			reviewPath := filepath.Join(root, "REVIEW.md")
			if err := os.WriteFile(reviewPath, candidateAuthorityReviewForTest(attemptCandidate(candidate), tools), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := CreateCandidateAuthority(CandidateAuthorityRequest{CandidateRoot: candidateRoot, ToolsRoot: toolsRoot, ReceiptPath: receiptPath, ReviewPath: reviewPath, OutputPath: filepath.Join(root, "CANDIDATE_AUTHORITY.json")}); err == nil {
				t.Fatalf("CreateCandidateAuthority accepted nonzero %s", operation)
			}
		})
	}
}

func TestCreateCandidateAuthorityRejectsWrongEmbeddedVersion(t *testing.T) {
	root := t.TempDir()
	candidateRoot, toolsRoot := newPairedBuildRepositories(t, "package main\n", "package main\n")
	candidatePath := filepath.Join(root, "glade")
	writeCandidateAuthorityExecutableWithVersion(t, candidatePath, true, strings.Repeat("f", 40), "")
	toolsPath, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	candidate := sealedAttemptCandidate{Commit: testGitOutput(t, candidateRoot, "rev-parse", "HEAD"), Path: candidatePath, SHA256: fileSHA256(t, candidatePath)}
	tools := candidateToolForTest(t, toolsRoot, toolsPath)
	receiptPath := filepath.Join(root, "candidate-receipt.json")
	writeCandidateBuildReceiptForTest(t, receiptPath, candidate, tools)
	reviewPath := filepath.Join(root, "REVIEW.md")
	if err := os.WriteFile(reviewPath, candidateAuthorityReviewForTest(attemptCandidate(candidate), tools), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateCandidateAuthority(CandidateAuthorityRequest{CandidateRoot: candidateRoot, ToolsRoot: toolsRoot, ReceiptPath: receiptPath, ReviewPath: reviewPath, OutputPath: filepath.Join(root, "CANDIDATE_AUTHORITY.json")}); err == nil {
		t.Fatal("CreateCandidateAuthority accepted the wrong embedded version")
	}
}

func TestValidateCandidateParserProvisionsOwnedHermeticToolchain(t *testing.T) {
	root := t.TempDir()
	commit := strings.Repeat("a", 40)
	candidatePath := filepath.Join(root, "glade")
	eventsPath := filepath.Join(root, "events")
	writeCandidateAuthorityProvisioningExecutable(t, candidatePath, commit, eventsPath)
	candidate := attemptCandidate{Commit: commit, Path: candidatePath, SHA256: fileSHA256(t, candidatePath)}
	if err := validateCandidateParser(candidate, root); err != nil {
		t.Fatalf("validateCandidateParser: %v", err)
	}
	events, err := os.ReadFile(eventsPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(events) != "npm\ninstall\nversion\ndoctor\nparse\n" {
		t.Fatalf("candidate authority events = %q", events)
	}
}

func TestValidateCandidateParserReturnsToolchainSetupErrors(t *testing.T) {
	for _, operation := range []string{"npm", "toolchain"} {
		t.Run(operation, func(t *testing.T) {
			root := t.TempDir()
			commit := strings.Repeat("a", 40)
			candidatePath := filepath.Join(root, "glade")
			writeCandidateAuthorityExecutableWithVersion(t, candidatePath, true, commit, operation)
			if operation == "npm" {
				writeCandidateAuthorityNPM(t, candidatePath, "exit 1\n")
			}
			candidate := attemptCandidate{Commit: commit, Path: candidatePath, SHA256: fileSHA256(t, candidatePath)}
			want := "candidate " + operation + " setup failed with exit code 1"
			if operation == "toolchain" {
				want = "candidate toolchain install failed with exit code 1"
			}
			if err := validateCandidateParser(candidate, root); err == nil || err.Error() != want {
				t.Fatalf("validateCandidateParser error = %v, want %q", err, want)
			}
		})
	}
}

func TestValidateCandidateParserUsesFixedReleaseNPM(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PATH", filepath.Join(root, "ambient-path-must-be-ignored"))
	commit := strings.Repeat("a", 40)
	candidatePath := filepath.Join(root, "glade")
	if err := os.WriteFile(candidatePath, []byte("fixed candidate\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	candidate := attemptCandidate{Commit: commit, Path: candidatePath, SHA256: fileSHA256(t, candidatePath)}
	oldResolver, oldRunner := resolveCandidateAuthorityBinary, runCandidateAuthorityCommand
	t.Cleanup(func() {
		resolveCandidateAuthorityBinary, runCandidateAuthorityCommand = oldResolver, oldRunner
	})
	var resolvedEnvironment []string
	resolveCandidateAuthorityBinary = func(environment []string, name string) (string, error) {
		resolvedEnvironment = append([]string(nil), environment...)
		if name != "npm" {
			t.Fatalf("resolved binary = %q", name)
		}
		return "/fixed/release/npm", nil
	}
	var operations []string
	outputs := map[string]string{
		"version": `{"schemaVersion":"1.0","command":"version","status":"passed","exitCode":0,"data":{"version":"` + commit + `"}}`,
		"doctor":  `{"command":"doctor","exitCode":0,"parserOK":true,"toolchainOK":true}`,
		"parse":   `{"schemaVersion":"1.0","command":"parse","status":"passed","exitCode":0}`,
	}
	runCandidateAuthorityCommand = func(workingDirectory string, command ReplayCommand) (CommandResult, []byte, []byte) {
		if workingDirectory != root {
			t.Fatalf("working directory = %q", workingDirectory)
		}
		operation := command.Args[0]
		operations = append(operations, operation)
		result := CommandResult{Passed: true, ExitCode: 0}
		if operation == "ci" {
			if command.Path != "/fixed/release/npm" {
				t.Fatalf("npm path = %q", command.Path)
			}
			for _, name := range []string{"HOME", "XDG_DATA_HOME", "TMPDIR"} {
				info, err := os.Stat(environmentEntry(command.Env, name))
				if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
					t.Fatalf("%s is not an owned mode-0700 directory", name)
				}
			}
		} else {
			result.ExecutableSHA256, result.ExecutableAfterSHA256 = candidate.SHA256, candidate.SHA256
		}
		return result, []byte(outputs[operation]), nil
	}
	if err := validateCandidateParser(candidate, root); err != nil {
		t.Fatalf("validateCandidateParser: %v", err)
	}
	if !equalStrings(resolvedEnvironment, fixedReleaseEnvironment()) {
		t.Fatalf("resolver environment = %#v", resolvedEnvironment)
	}
	if !equalStrings(operations, []string{"ci", "toolchain", "version", "doctor", "parse"}) {
		t.Fatalf("operations = %#v", operations)
	}
}

func environmentEntry(environment []string, name string) string {
	for _, entry := range environment {
		if strings.HasPrefix(entry, name+"=") {
			return strings.TrimPrefix(entry, name+"=")
		}
	}
	return ""
}

func TestValidateCandidateParserReturnsPreciseDoctorErrors(t *testing.T) {
	for _, test := range []struct {
		name        string
		parserOK    bool
		toolchainOK bool
		processExit int
		want        string
	}{
		{name: "command exit", parserOK: true, toolchainOK: true, processExit: 1, want: "candidate doctor command failed with exit code 1"},
		{name: "parser false", toolchainOK: true, want: "candidate doctor reported parser unavailable"},
		{name: "toolchain false", parserOK: true, want: "candidate doctor reported toolchain unavailable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			commit := strings.Repeat("a", 40)
			candidatePath := filepath.Join(root, "glade")
			writeCandidateAuthorityDoctorExecutable(t, candidatePath, commit, test.parserOK, test.toolchainOK, 0, test.processExit)
			candidate := attemptCandidate{Commit: commit, Path: candidatePath, SHA256: fileSHA256(t, candidatePath)}
			if err := validateCandidateParser(candidate, root); err == nil || err.Error() != test.want {
				t.Fatalf("validateCandidateParser error = %v, want %q", err, test.want)
			}
		})
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

func writeCandidateAuthorityExecutable(t *testing.T, path, candidateRoot string, parserOK bool) {
	t.Helper()
	writeCandidateAuthorityExecutableWithFailure(t, path, candidateRoot, parserOK, "")
}

func writeCandidateAuthorityExecutableWithFailure(t *testing.T, path, candidateRoot string, parserOK bool, failingOperation string) {
	t.Helper()
	writeCandidateAuthorityExecutableWithVersion(t, path, parserOK, testGitOutput(t, candidateRoot, "rev-parse", "HEAD"), failingOperation)
}

func writeCandidateAuthorityExecutableWithVersion(t *testing.T, path string, parserOK bool, embeddedVersion, failingOperation string) {
	t.Helper()
	value := "false"
	if parserOK {
		value = "true"
	}
	script := "#!/bin/sh\ncase \"$1\" in\n" +
		"version) printf '%s\\n' '{\"schemaVersion\":\"1.0\",\"command\":\"version\",\"status\":\"passed\",\"exitCode\":0,\"data\":{\"version\":\"" + embeddedVersion + "\",\"go\":\"go-test\"}}' ;;\n" +
		"doctor) printf '%s\\n' '{\"command\":\"doctor\",\"exitCode\":0,\"parserOK\":" + value + ",\"toolchainOK\":true}' ;;\n" +
		"parse) printf '%s\\n' '{\"schemaVersion\":\"1.0\",\"command\":\"parse\",\"status\":\"passed\",\"exitCode\":0,\"data\":{}}' ;;\n" +
		"toolchain) : ;;\n" +
		"*) exit 2 ;;\nesac\n" +
		"if [ \"$1\" = \"" + failingOperation + "\" ]; then exit 1; fi\nexit 0\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	writeCandidateAuthorityNPM(t, path, "exit 0\n")
}

func writeCandidateAuthorityDoctorExecutable(t *testing.T, path, commit string, parserOK, toolchainOK bool, jsonExit, processExit int) {
	t.Helper()
	script := "#!/bin/sh\ncase \"$1\" in\n" +
		"version) printf '%s\\n' '{\"schemaVersion\":\"1.0\",\"command\":\"version\",\"status\":\"passed\",\"exitCode\":0,\"data\":{\"version\":\"" + commit + "\"}}' ;;\n" +
		"doctor) parser_ok=" + fmt.Sprint(parserOK) + "; toolchain_ok=" + fmt.Sprint(toolchainOK) + "; json_exit=" + fmt.Sprint(jsonExit) + "; process_exit=" + fmt.Sprint(processExit) + "\n" +
		"printf '{\"command\":\"doctor\",\"exitCode\":%s,\"parserOK\":%s,\"toolchainOK\":%s}\\n' \"$json_exit\" \"$parser_ok\" \"$toolchain_ok\"\nexit \"$process_exit\" ;;\n" +
		"parse) printf '%s\\n' '{\"schemaVersion\":\"1.0\",\"command\":\"parse\",\"status\":\"passed\",\"exitCode\":0}' ;;\n" +
		"toolchain) : ;;\n" +
		"*) exit 2 ;;\nesac\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	writeCandidateAuthorityNPM(t, path, "exit 0\n")
}

func writeCandidateAuthorityProvisioningExecutable(t *testing.T, path, commit, eventsPath string) {
	t.Helper()
	checks := "home_mode=$(stat -f '%Lp' \"$HOME\" 2>/dev/null || stat -c '%a' \"$HOME\")\n" +
		"data_mode=$(stat -f '%Lp' \"$XDG_DATA_HOME\" 2>/dev/null || stat -c '%a' \"$XDG_DATA_HOME\")\n" +
		"[ -d \"$HOME\" ] && [ -d \"$XDG_DATA_HOME\" ] && [ \"$home_mode\" = 700 ] && [ \"$data_mode\" = 700 ] && [ -z \"$GLADE_HOME\" ] || exit 9\n"
	script := "#!/bin/sh\ncase \"$1\" in\n" +
		"toolchain) " + checks + "[ \"$2\" = install ] && [ \"$3\" = --from ] && [ \"$4\" = \"" + filepath.Dir(path) + "\" ] || exit 8\nmkdir -p \"$XDG_DATA_HOME/glade\"\ntouch \"$XDG_DATA_HOME/glade/installed\"\nprintf 'install\\n' >> \"" + eventsPath + "\" ;;\n" +
		"version) [ -f \"$XDG_DATA_HOME/glade/installed\" ] && [ -z \"$GLADE_HOME\" ] || exit 7\nprintf 'version\\n' >> \"" + eventsPath + "\"\nprintf '%s\\n' '{\"schemaVersion\":\"1.0\",\"command\":\"version\",\"status\":\"passed\",\"exitCode\":0,\"data\":{\"version\":\"" + commit + "\"}}' ;;\n" +
		"doctor) printf 'doctor\\n' >> \"" + eventsPath + "\"\nprintf '%s\\n' '{\"command\":\"doctor\",\"exitCode\":0,\"parserOK\":true,\"toolchainOK\":true}' ;;\n" +
		"parse) printf 'parse\\n' >> \"" + eventsPath + "\"\nprintf '%s\\n' '{\"schemaVersion\":\"1.0\",\"command\":\"parse\",\"status\":\"passed\",\"exitCode\":0}' ;;\n" +
		"*) exit 2 ;;\nesac\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	npmScript := checks + "[ \"$1\" = ci ] && [ \"$2\" = --ignore-scripts ] && [ \"$3\" = --no-audit ] && [ \"$4\" = --no-fund ] && [ \"$5\" = --prefix ] && [ \"$6\" = third_party/lwc ] || exit 8\nprintf 'npm\\n' >> \"" + eventsPath + "\"\n"
	writeCandidateAuthorityNPM(t, path, npmScript)
}

func writeCandidateAuthorityNPM(t *testing.T, candidatePath, body string) {
	t.Helper()
	npmPath := filepath.Join(filepath.Dir(candidatePath), "npm")
	if err := os.WriteFile(npmPath, []byte("#!/bin/sh\n"+body), 0o700); err != nil {
		t.Fatal(err)
	}
	oldResolver := resolveCandidateAuthorityBinary
	resolveCandidateAuthorityBinary = func([]string, string) (string, error) { return npmPath, nil }
	t.Cleanup(func() { resolveCandidateAuthorityBinary = oldResolver })
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
	return []byte("Verdict: PASS\nCandidate commit: " + candidate.Commit + "\nCandidate SHA-256: " + candidate.SHA256 + "\nCandidate ref: HEAD\nCandidate ref commit: " + candidate.Commit + "\nTools commit: " + tools.Commit + "\nTools OS: " + tools.OS + "\nTools arch: " + tools.Arch + "\nTools SHA-256: " + tools.SHA256 + "\nTools path: " + tools.Path + "\nTools ref: HEAD\nTools ref commit: " + tools.Commit + "\n")
}

func writeCandidateBuildReceiptForTest(t *testing.T, path string, candidate sealedAttemptCandidate, tools candidateTool) {
	t.Helper()
	receipt := candidateBuildReceipt{SchemaVersion: 2, Status: "clean-exact-candidate", SourceCommit: candidate.Commit, BinarySHA256: candidate.SHA256, CleanWorktree: true, CandidateRef: "HEAD", CandidateRefCommit: candidate.Commit, ToolsRef: "HEAD", ToolsRefCommit: tools.Commit, Candidate: attemptCandidate(candidate), Tools: tools}
	writeCandidateAuthorityJSON(t, path, receipt)
}

func newPairedBuildRepositories(t *testing.T, candidateMain, toolsMain string) (string, string) {
	t.Helper()
	root := t.TempDir()
	candidateRoot := filepath.Join(root, "glade")
	toolsRoot := filepath.Join(root, "glade-tools")
	for path, files := range map[string]map[string]string{
		candidateRoot: {
			"go.mod":                               "module github.com/glade-sh/glade\n\ngo 1.23.0\n\nrequire github.com/glade-sh/apex-parser v0.1.0\n\nreplace github.com/glade-sh/apex-parser => ./third_party/glade-apex-parser\n",
			"cmd/glade/main.go":                    candidateMain,
			"third_party/glade-apex-parser/go.mod": "module github.com/glade-sh/apex-parser\n\ngo 1.23.0\n",
		},
		toolsRoot: {
			"go.mod":                  "module github.com/glade-sh/glade/tools\n\ngo 1.23.0\n\nrequire (\n\tgithub.com/glade-sh/glade v0.0.0\n\tgithub.com/glade-sh/apex-parser v0.1.0\n)\n\nreplace github.com/glade-sh/glade => ../glade\n\nreplace github.com/glade-sh/apex-parser => ../glade/third_party/glade-apex-parser\n",
			"cmd/glade-tools/main.go": toolsMain,
		},
	} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		gitRun(t, path, "init", "--quiet")
		gitRun(t, path, "config", "user.email", "inventory@example.test")
		gitRun(t, path, "config", "user.name", "Inventory Test")
		for relativePath, content := range files {
			writeFixtureFile(t, path, relativePath, content)
		}
		gitRun(t, path, "add", ".")
		gitRun(t, path, "commit", "--quiet", "-m", "fixture")
	}
	return candidateRoot, toolsRoot
}

func newInventoryRepositoryAt(t *testing.T, repository string, files map[string]string) string {
	t.Helper()
	if err := os.MkdirAll(repository, 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repository, "init", "--quiet")
	gitRun(t, repository, "config", "user.email", "inventory@example.test")
	gitRun(t, repository, "config", "user.name", "Inventory Test")
	for path, content := range files {
		writeFixtureFile(t, repository, path, content)
	}
	gitRun(t, repository, "add", ".")
	gitRun(t, repository, "commit", "--quiet", "-m", "fixture")
	return repository
}

func rewriteCandidateReplacementForTest(t *testing.T, candidateRoot, replacement string) {
	t.Helper()
	target := "./third_party/glade-apex-parser"
	old := "github.com/glade-sh/apex-parser"
	includeReplacement := true
	switch replacement {
	case "missing":
		includeReplacement = false
	case "absolute":
		target = filepath.Join(candidateRoot, "third_party", "glade-apex-parser")
	case "external-relative", "symlink-escape":
		external := filepath.Join(filepath.Dir(candidateRoot), "external-parser")
		if err := os.MkdirAll(external, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(external, "go.mod"), []byte("module github.com/glade-sh/apex-parser\n\ngo 1.23.0\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if replacement == "external-relative" {
			target = "../external-parser"
		} else {
			target = "./parser-link"
			if err := os.Symlink(external, filepath.Join(candidateRoot, "parser-link")); err != nil {
				t.Fatal(err)
			}
		}
	case "unexpected":
		old = "example.invalid/parser"
	case "version-specific":
		old += " v0.1.0"
	default:
		t.Fatalf("unknown candidate replacement %q", replacement)
	}
	goMod := "module github.com/glade-sh/glade\n\ngo 1.23.0\n\nrequire github.com/glade-sh/apex-parser v0.1.0\n"
	if includeReplacement {
		goMod += "\nreplace " + old + " => " + target + "\n"
	}
	if err := os.WriteFile(filepath.Join(candidateRoot, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, candidateRoot, "add", ".")
	gitRun(t, candidateRoot, "commit", "--quiet", "-m", "replace parser")
}
