package corpusassurance

import (
	"bytes"
	"debug/buildinfo"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateCandidateBuildReceiptBuildsAndBindsExactBinaries(t *testing.T) {
	root := t.TempDir()
	candidateRoot, toolsRoot := newPairedBuildRepositories(t, "package main\nfunc main() {}\n", "package main\nfunc main() {}\n")
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
	progressPath := candidateBuildProgressPath(request.ReceiptOutput)
	progress, err := os.ReadFile(progressPath)
	if err != nil {
		t.Fatalf("candidate build progress: %v", err)
	}
	if !strings.Contains(string(progress), "event=candidate-build-start") || !strings.Contains(string(progress), "event=candidate-build-complete") {
		t.Fatalf("candidate build progress = %q", progress)
	}
	review, err := os.ReadFile(request.ReviewOutput)
	if err != nil || !strings.HasPrefix(string(review), "Verdict: PENDING\n") {
		t.Fatalf("review = %q, err=%v", review, err)
	}
	if _, err := CreateCandidateBuildReceipt(request); err == nil {
		t.Fatal("CreateCandidateBuildReceipt overwrote create-only outputs")
	}
}

func TestCreateCandidateBuildReceiptReportsFailedBuild(t *testing.T) {
	root := t.TempDir()
	candidateRoot, toolsRoot := newPairedBuildRepositories(t, "package main\nfunc main() {}\n", "package main\nfunc main() {\n")
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
	progressPath := candidateBuildProgressPath(request.ReceiptOutput)
	progress, err := os.ReadFile(progressPath)
	if err != nil {
		t.Fatalf("failed candidate build progress: %v", err)
	}
	if !strings.Contains(string(progress), "event=candidate-build-failed") || !strings.Contains(string(progress), "event=tools-build-failed") {
		t.Fatalf("failed candidate build progress = %q", progress)
	}
}

func TestCreateCandidateBuildReceiptStopsAfterCandidateFailure(t *testing.T) {
	root := t.TempDir()
	candidateRoot, toolsRoot := newPairedBuildRepositories(t, "package main\nfunc main() {\n", "package main\nfunc main() {\n")
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
	progressPath := candidateBuildProgressPath(request.ReceiptOutput)
	progress, err := os.ReadFile(progressPath)
	if err != nil {
		t.Fatalf("failed candidate build progress: %v", err)
	}
	if !strings.Contains(string(progress), "event=candidate-build-failed") || strings.Contains(string(progress), "event=tools-build-start") {
		t.Fatalf("candidate build did not fail fast before tools: %q", progress)
	}
}

func TestCandidateBuildBindingUsesExactReleaseVersion(t *testing.T) {
	candidateRoot, toolsRoot := newPairedBuildRepositories(t, "package main\nfunc main() {}\n", "package main\nfunc main() {}\n")
	commit := testGitOutput(t, candidateRoot, "rev-parse", "HEAD")
	binding, err := deriveCandidateBuildBinding(candidateRoot)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"build", "-buildvcs=false", "-trimpath", "-ldflags", "-s -w -X github.com/glade-sh/glade/internal/gladecli.Version=" + commit, "-o", "<candidate>", "./cmd/glade"}
	if !equalStrings(binding.Arguments, want) {
		t.Fatalf("candidate build arguments = %#v, want %#v", binding.Arguments, want)
	}
	toolsBinding, err := deriveToolsBuildBinding(toolsRoot)
	if err != nil {
		t.Fatal(err)
	}
	if !equalStrings(toolsBinding.Arguments, []string{"build", "-buildvcs=false", "-trimpath", "-o", "<candidate>", "./cmd/glade-tools"}) {
		t.Fatalf("tools build arguments = %#v", toolsBinding.Arguments)
	}
}

func TestRunBoundCandidateBuildRequiresExactlyOneOutputPlaceholder(t *testing.T) {
	for name, arguments := range map[string][]string{
		"missing":   {"build", "./cmd/glade"},
		"duplicate": {"build", "-o", "<candidate>", "<candidate>", "./cmd/glade"},
	} {
		t.Run(name, func(t *testing.T) {
			binding := candidateBuildBinding{Arguments: arguments}
			if err := runBoundCandidateBuild(binding, filepath.Join(t.TempDir(), "glade")); err == nil {
				t.Fatal("runBoundCandidateBuild accepted an invalid placeholder count")
			}
		})
	}
}

func TestRunBoundCandidateBuildFindsTheOutputPlaceholderByValue(t *testing.T) {
	root := t.TempDir()
	goPath := filepath.Join(root, "go")
	if err := os.WriteFile(goPath, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	binding := candidateBuildBinding{
		SourceRoot: root,
		Go:         candidateAuthoritySource{Path: goPath, SHA256: fileSHA256(t, goPath)},
		Arguments:  []string{"build", "./cmd/glade", "<candidate>"},
	}
	if err := runBoundCandidateBuild(binding, filepath.Join(root, "glade")); err != nil {
		t.Fatalf("runBoundCandidateBuild rejected a single non-positional placeholder: %v", err)
	}
}

func TestToolsBuildInfoKeepsRelativeCandidateReplacements(t *testing.T) {
	candidateRoot, toolsRoot := newPairedBuildRepositories(t, "package main\nfunc main() {}\n", "package main\nfunc main() {}\n")
	writeFixtureFile(t, candidateRoot, "probe/probe.go", "package probe\n")
	writeFixtureFile(t, candidateRoot, "third_party/glade-apex-parser/probe/probe.go", "package probe\n")
	writeFixtureFile(t, toolsRoot, "cmd/glade-tools/main.go", "package main\nimport (\n _ \"github.com/glade-sh/glade/probe\"\n _ \"github.com/glade-sh/apex-parser/probe\"\n)\nfunc main() {}\n")
	gitRun(t, candidateRoot, "add", ".")
	gitRun(t, candidateRoot, "commit", "--quiet", "-m", "add probes")
	gitRun(t, toolsRoot, "add", ".")
	gitRun(t, toolsRoot, "commit", "--quiet", "-m", "use probes")
	binding, err := deriveToolsBuildBinding(toolsRoot)
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "glade-tools")
	if err := runBoundCandidateBuild(binding, output); err != nil {
		t.Fatal(err)
	}
	info, err := buildinfo.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"github.com/glade-sh/glade":       "../glade",
		"github.com/glade-sh/apex-parser": "../glade/third_party/glade-apex-parser",
	}
	for _, dependency := range info.Deps {
		if dependency.Replace == nil || want[dependency.Path] == "" {
			continue
		}
		if dependency.Replace.Path != want[dependency.Path] || filepath.IsAbs(dependency.Replace.Path) {
			t.Fatalf("replacement for %s = %q, want relative %q", dependency.Path, dependency.Replace.Path, want[dependency.Path])
		}
		delete(want, dependency.Path)
	}
	if len(want) != 0 {
		t.Fatalf("build info missing relative replacements: %#v", want)
	}
	binary, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(binary, []byte(candidateRoot)) || bytes.Contains(binary, []byte(toolsRoot)) {
		t.Fatal("tools binary contains an original absolute source root")
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
		{name: "tools candidate mismatch", mutate: func(request *CandidateBuildRequest) {
			_, request.ToolsRoot = newPairedBuildRepositories(t, "package main\nfunc main() {}\n", "package main\nfunc main() {}\n")
		}},
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
			candidateRoot, toolsRoot := newPairedBuildRepositories(t, "package main\nfunc main() {}\n", "package main\nfunc main() {}\n")
			request := CandidateBuildRequest{CandidateRoot: candidateRoot, ToolsRoot: toolsRoot, CandidateRef: "HEAD", ToolsRef: "HEAD", CandidateOutput: filepath.Join(root, "bin", "glade"), ToolsOutput: filepath.Join(root, "bin", "glade-tools"), ReceiptOutput: filepath.Join(root, "bindings", "CANDIDATE_BUILD_RECEIPT.json"), ReviewOutput: filepath.Join(root, "bindings", "REVIEW.md"), ToolsFreezeOutput: filepath.Join(root, "bindings", "TOOLS_COMMIT")}
			testCase.mutate(&request)
			if _, err := CreateCandidateBuildReceipt(request); err == nil {
				t.Fatal("CreateCandidateBuildReceipt accepted unbound input")
			}
		})
	}
}

func TestCreateCandidateBuildReceiptRejectsInvalidCandidateReplacementsBeforeBuild(t *testing.T) {
	for _, replacement := range []string{"missing", "absolute", "external-relative", "symlink-escape", "unexpected", "version-specific"} {
		t.Run(replacement, func(t *testing.T) {
			root := t.TempDir()
			candidateRoot, toolsRoot := newPairedBuildRepositories(t, "package main\nfunc main() {}\n", "package main\nfunc main() {}\n")
			rewriteCandidateReplacementForTest(t, candidateRoot, replacement)
			request := CandidateBuildRequest{
				CandidateRoot: candidateRoot, ToolsRoot: toolsRoot, CandidateRef: "HEAD", ToolsRef: "HEAD",
				CandidateOutput: filepath.Join(root, "bin", "glade"), ToolsOutput: filepath.Join(root, "bin", "glade-tools"),
				ReceiptOutput: filepath.Join(root, "bindings", "CANDIDATE_BUILD_RECEIPT.json"), ReviewOutput: filepath.Join(root, "bindings", "REVIEW.md"), ToolsFreezeOutput: filepath.Join(root, "bindings", "TOOLS_COMMIT"),
			}
			if _, err := CreateCandidateBuildReceipt(request); err == nil {
				t.Fatalf("CreateCandidateBuildReceipt accepted %s candidate replacement", replacement)
			}
			for _, path := range []string{request.CandidateOutput, request.ToolsOutput} {
				if _, err := os.Lstat(path); !os.IsNotExist(err) {
					t.Fatalf("invalid candidate replacement created build output %s: %v", path, err)
				}
			}
			progress, err := os.ReadFile(candidateBuildProgressPath(request.ReceiptOutput))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(progress), "event=candidate-build-start commit=") {
				t.Fatalf("invalid candidate replacement reached build execution: %q", progress)
			}
		})
	}
}

func TestCreateCandidateBuildReceiptRejectsCanonicalOutputCollisions(t *testing.T) {
	for _, collision := range []string{"symlink-alias", "progress-log"} {
		t.Run(collision, func(t *testing.T) {
			root := t.TempDir()
			candidateRoot, toolsRoot := newPairedBuildRepositories(t, "package main\nfunc main() {}\n", "package main\nfunc main() {}\n")
			request := CandidateBuildRequest{
				CandidateRoot: candidateRoot, ToolsRoot: toolsRoot, CandidateRef: "HEAD", ToolsRef: "HEAD",
				CandidateOutput: filepath.Join(root, "bin", "glade"), ToolsOutput: filepath.Join(root, "bin", "glade-tools"),
				ReceiptOutput: filepath.Join(root, "bindings", "CANDIDATE_BUILD_RECEIPT.json"), ReviewOutput: filepath.Join(root, "bindings", "REVIEW.md"), ToolsFreezeOutput: filepath.Join(root, "bindings", "TOOLS_COMMIT"),
			}
			if collision == "symlink-alias" {
				alias := filepath.Join(t.TempDir(), "output-alias")
				if err := os.Symlink(root, alias); err != nil {
					t.Fatal(err)
				}
				request.ToolsOutput = filepath.Join(alias, "bin", "glade")
			} else {
				request.CandidateOutput = candidateBuildProgressPath(request.ReceiptOutput)
			}
			if _, err := CreateCandidateBuildReceipt(request); err == nil {
				t.Fatalf("CreateCandidateBuildReceipt accepted %s output collision", collision)
			}
			if _, err := os.Lstat(candidateBuildProgressPath(request.ReceiptOutput)); !os.IsNotExist(err) {
				t.Fatalf("output collision created progress log: %v", err)
			}
		})
	}
}
