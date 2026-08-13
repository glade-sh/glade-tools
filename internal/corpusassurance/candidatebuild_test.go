package corpusassurance

import (
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
	review, err := os.ReadFile(request.ReviewOutput)
	if err != nil || !strings.HasPrefix(string(review), "Verdict: PENDING\n") {
		t.Fatalf("review = %q, err=%v", review, err)
	}
	if _, err := CreateCandidateBuildReceipt(request); err == nil {
		t.Fatal("CreateCandidateBuildReceipt overwrote create-only outputs")
	}
}
