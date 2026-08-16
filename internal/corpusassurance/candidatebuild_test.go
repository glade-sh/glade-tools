package corpusassurance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateCandidateBuildReceiptBuildsAndBindsExactBinaries(t *testing.T) {
	root := t.TempDir()
	candidateRoot := newInventoryRepository(t, map[string]string{
		"go.mod":            "module example.invalid/candidate\n\ngo 1.22\n",
		"cmd/glade/main.go": "package main\nfunc main() {}\n",
	})
	toolsRoot := newInventoryRepository(t, map[string]string{
		"go.mod":                  "module example.invalid/tools\n\ngo 1.22\n",
		"cmd/glade-tools/main.go": "package main\nfunc main() {}\n",
	})
	request := CandidateBuildRequest{
		CandidateRoot:     candidateRoot,
		ToolsRoot:         toolsRoot,
		CandidateRef:      "HEAD",
		ToolsRef:          "HEAD",
		CandidateOutput:   filepath.Join(root, "bin", "glade"),
		ToolsOutput:       filepath.Join(root, "bin", "glade-tools"),
		ReceiptOutput:     filepath.Join(root, "bindings", "CANDIDATE_BUILD_RECEIPT.json"),
		ReviewOutput:      filepath.Join(root, "bindings", "REVIEW.md"),
		ToolsFreezeOutput: filepath.Join(root, "bindings", "TOOLS_COMMIT"),
	}
	receipt, err := CreateCandidateBuildReceipt(request)
	if err != nil {
		t.Fatalf("CreateCandidateBuildReceipt: %v", err)
	}
	if receipt.SchemaVersion != 2 || receipt.Status != "clean-exact-candidate" || !receipt.CleanWorktree {
		t.Fatalf("receipt = %#v", receipt)
	}
	if receipt.CandidateRef != "HEAD" || receipt.ToolsRef != "HEAD" || receipt.CandidateRefCommit != receipt.Candidate.Commit || receipt.ToolsRefCommit != receipt.Tools.Commit {
		t.Fatalf("receipt refs = %#v", receipt)
	}
	for _, path := range []string{request.CandidateOutput, request.ToolsOutput, request.ReceiptOutput, request.ReviewOutput, request.ToolsFreezeOutput} {
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			t.Fatalf("artifact %s: %v", path, err)
		}
	}
	if info, err := os.Stat(request.ToolsFreezeOutput); err != nil || info.Mode().Perm() != 0o400 {
		t.Fatalf("tools freeze mode = %v, %v", info.Mode().Perm(), err)
	}
	progressPath, failurePath := candidateBuildEvidencePaths(request.ReceiptOutput)
	progress, err := os.ReadFile(progressPath)
	if err != nil {
		t.Fatalf("candidate build progress: %v", err)
	}
	if !strings.Contains(string(progress), "event=candidate-build-start") || !strings.Contains(string(progress), "event=candidate-build-complete") {
		t.Fatalf("candidate build progress = %q", progress)
	}
	if _, err := os.Stat(failurePath); !os.IsNotExist(err) {
		t.Fatalf("successful candidate build has failure receipt: %v", err)
	}
	review, err := os.ReadFile(request.ReviewOutput)
	if err != nil || !strings.HasPrefix(string(review), "Verdict: PENDING\n") {
		t.Fatalf("review = %q, err=%v", review, err)
	}
	if _, err := CreateCandidateBuildReceipt(request); err == nil {
		t.Fatal("CreateCandidateBuildReceipt overwrote create-only outputs")
	}
}

func TestCreateCandidateBuildReceiptPreservesFailedBuildEvidence(t *testing.T) {
	root := t.TempDir()
	candidateRoot := newInventoryRepository(t, map[string]string{
		"go.mod":            "module example.invalid/candidate\n\ngo 1.22\n",
		"cmd/glade/main.go": "package main\nfunc main() {}\n",
	})
	toolsRoot := newInventoryRepository(t, map[string]string{
		"go.mod":                  "module example.invalid/tools\n\ngo 1.22\n",
		"cmd/glade-tools/main.go": "package main\nfunc main() {\n",
	})
	request := CandidateBuildRequest{
		CandidateRoot:     candidateRoot,
		ToolsRoot:         toolsRoot,
		CandidateRef:      "HEAD",
		ToolsRef:          "HEAD",
		CandidateOutput:   filepath.Join(root, "bin", "glade"),
		ToolsOutput:       filepath.Join(root, "bin", "glade-tools"),
		ReceiptOutput:     filepath.Join(root, "bindings", "CANDIDATE_BUILD_RECEIPT.json"),
		ReviewOutput:      filepath.Join(root, "bindings", "REVIEW.md"),
		ToolsFreezeOutput: filepath.Join(root, "bindings", "TOOLS_COMMIT"),
	}
	if _, err := CreateCandidateBuildReceipt(request); err == nil {
		t.Fatal("CreateCandidateBuildReceipt accepted a malformed tools build")
	} else if !strings.Contains(err.Error(), "syntax error") {
		t.Fatalf("CreateCandidateBuildReceipt error = %v, want compiler stderr", err)
	}
	progressPath, failurePath := candidateBuildEvidencePaths(request.ReceiptOutput)
	progress, err := os.ReadFile(progressPath)
	if err != nil {
		t.Fatalf("failed candidate build progress: %v", err)
	}
	if !strings.Contains(string(progress), "event=candidate-build-failed") || !strings.Contains(string(progress), "event=tools-build-failed") {
		t.Fatalf("failed candidate build progress = %q", progress)
	}
	data, err := os.ReadFile(failurePath)
	if err != nil {
		t.Fatalf("failed candidate build receipt: %v", err)
	}
	var failure candidateBuildFailure
	if err := json.Unmarshal(data, &failure); err != nil {
		t.Fatal(err)
	}
	if failure.SchemaVersion != 1 || failure.Status != "candidate-build-failed" || failure.Stage != "tools-build" || !strings.Contains(failure.ToolsStderr, "syntax error") {
		t.Fatalf("failed candidate build receipt = %#v", failure)
	}
}

func TestCreateCandidateBuildReceiptPreservesBothBuildFailures(t *testing.T) {
	root := t.TempDir()
	candidateRoot := newInventoryRepository(t, map[string]string{
		"go.mod":            "module example.invalid/candidate\n\ngo 1.22\n",
		"cmd/glade/main.go": "package main\nfunc main() {\n",
	})
	toolsRoot := newInventoryRepository(t, map[string]string{
		"go.mod":                  "module example.invalid/tools\n\ngo 1.22\n",
		"cmd/glade-tools/main.go": "package main\nfunc main() {\n",
	})
	request := CandidateBuildRequest{
		CandidateRoot:     candidateRoot,
		ToolsRoot:         toolsRoot,
		CandidateRef:      "HEAD",
		ToolsRef:          "HEAD",
		CandidateOutput:   filepath.Join(root, "bin", "glade"),
		ToolsOutput:       filepath.Join(root, "bin", "glade-tools"),
		ReceiptOutput:     filepath.Join(root, "bindings", "CANDIDATE_BUILD_RECEIPT.json"),
		ReviewOutput:      filepath.Join(root, "bindings", "REVIEW.md"),
		ToolsFreezeOutput: filepath.Join(root, "bindings", "TOOLS_COMMIT"),
	}
	if _, err := CreateCandidateBuildReceipt(request); err == nil {
		t.Fatal("CreateCandidateBuildReceipt accepted two malformed builds")
	}
	_, failurePath := candidateBuildEvidencePaths(request.ReceiptOutput)
	data, err := os.ReadFile(failurePath)
	if err != nil {
		t.Fatalf("failed candidate build receipt: %v", err)
	}
	var failure candidateBuildFailure
	if err := json.Unmarshal(data, &failure); err != nil {
		t.Fatal(err)
	}
	if failure.Stage != "builds" || !strings.Contains(failure.CandidateStderr, "syntax error") || !strings.Contains(failure.ToolsStderr, "syntax error") || len(failure.CandidateCommand) == 0 || len(failure.ToolsCommand) == 0 {
		t.Fatalf("failed candidate build receipt = %#v", failure)
	}
}

func TestCreateCandidateBuildReceiptRejectsUnboundInputs(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*CandidateBuildRequest)
	}{
		{name: "non-absolute root", mutate: func(request *CandidateBuildRequest) { request.CandidateRoot = "candidate" }},
		{name: "dirty root", mutate: func(request *CandidateBuildRequest) {
			if err := os.WriteFile(filepath.Join(request.CandidateRoot, "untracked"), []byte("dirty"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "unknown ref", mutate: func(request *CandidateBuildRequest) { request.CandidateRef = "missing-ref" }},
		{name: "ref and head mismatch", mutate: func(request *CandidateBuildRequest) { request.CandidateRef = "HEAD^" }},
		{name: "pre-existing output", mutate: func(request *CandidateBuildRequest) {
			if err := os.MkdirAll(filepath.Dir(request.CandidateOutput), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(request.CandidateOutput, []byte("existing"), 0o700); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			root := t.TempDir()
			candidateRoot := newInventoryRepository(t, map[string]string{"go.mod": "module example.invalid/candidate\n\ngo 1.22\n", "cmd/glade/main.go": "package main\nfunc main() {}\n"})
			toolsRoot := newInventoryRepository(t, map[string]string{"go.mod": "module example.invalid/tools\n\ngo 1.22\n", "cmd/glade-tools/main.go": "package main\nfunc main() {}\n"})
			request := CandidateBuildRequest{CandidateRoot: candidateRoot, ToolsRoot: toolsRoot, CandidateRef: "HEAD", ToolsRef: "HEAD", CandidateOutput: filepath.Join(root, "bin", "glade"), ToolsOutput: filepath.Join(root, "bin", "glade-tools"), ReceiptOutput: filepath.Join(root, "bindings", "CANDIDATE_BUILD_RECEIPT.json"), ReviewOutput: filepath.Join(root, "bindings", "REVIEW.md"), ToolsFreezeOutput: filepath.Join(root, "bindings", "TOOLS_COMMIT")}
			testCase.mutate(&request)
			if _, err := CreateCandidateBuildReceipt(request); err == nil {
				t.Fatal("CreateCandidateBuildReceipt accepted unbound input")
			}
		})
	}
}
