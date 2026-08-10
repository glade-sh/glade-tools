package corpusassurance

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRunReleaseValidationRejectsToolsHeadThatDoesNotMatchFrozenCommit(t *testing.T) {
	root := t.TempDir()
	gladeRoot := newInventoryRepository(t, map[string]string{"main.go": "package main\n"})
	toolsRoot := newInventoryRepository(t, map[string]string{"main.go": "package main\n"})
	candidatePath := filepath.Join(root, "glade")
	if err := os.WriteFile(candidatePath, []byte("binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	toolsPath, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	attemptPath := writeReleaseAttempt(t, root, candidatePath, testGitOutput(t, gladeRoot, "rev-parse", "HEAD"), toolsPath, testGitOutput(t, toolsRoot, "rev-parse", "HEAD"))
	freezePath := filepath.Join(root, "FINAL_TOOLS_COMMIT")
	if err := os.WriteFile(freezePath, []byte(strings.Repeat("f", 40)+"\n"), 0o400); err != nil {
		t.Fatal(err)
	}
	_, err = RunReleaseValidation(ReleaseValidationRequest{
		AttemptPath: attemptPath, GladeRoot: gladeRoot, CandidatePath: candidatePath,
		ToolsRoot: toolsRoot, ToolsPath: toolsPath, ToolsFreezePath: freezePath,
		OutputPath: filepath.Join(root, "RELEASE_VALIDATION.json"),
		runner: func(context.Context, releaseCommand) (salesforceCommandOutput, error) {
			t.Fatal("release validation ran checks before validating the frozen tools commit")
			return salesforceCommandOutput{}, nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "frozen tools commit") {
		t.Fatalf("RunReleaseValidation error = %v", err)
	}
}

func TestRunReleaseValidationRequiresReadOnlyFrozenToolsCommit(t *testing.T) {
	root := t.TempDir()
	gladeRoot := newInventoryRepository(t, map[string]string{"main.go": "package main\n"})
	toolsRoot := newInventoryRepository(t, map[string]string{"main.go": "package main\n"})
	candidatePath, toolsPath := filepath.Join(root, "glade"), filepath.Join(root, "glade-tools")
	for _, path := range []string{candidatePath, toolsPath} {
		if err := os.WriteFile(path, []byte("binary"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	toolsCommit := testGitOutput(t, toolsRoot, "rev-parse", "HEAD")
	attemptPath := writeReleaseAttempt(t, root, candidatePath, testGitOutput(t, gladeRoot, "rev-parse", "HEAD"), toolsPath, toolsCommit)
	freezePath := filepath.Join(root, "FINAL_TOOLS_COMMIT")
	if err := os.WriteFile(freezePath, []byte(toolsCommit+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := RunReleaseValidation(ReleaseValidationRequest{
		AttemptPath: attemptPath, GladeRoot: gladeRoot, CandidatePath: candidatePath,
		ToolsRoot: toolsRoot, ToolsPath: toolsPath, ToolsFreezePath: freezePath, OutputPath: filepath.Join(root, "RELEASE_VALIDATION.json"),
	})
	if err == nil || !strings.Contains(err.Error(), "frozen tools commit must be mode 0400") {
		t.Fatalf("RunReleaseValidation error = %v", err)
	}
}

func TestRunReleaseValidationDerivesArtifactsAndFreezeFromSealedAttempt(t *testing.T) {
	root := t.TempDir()
	gladeRoot := newInventoryRepository(t, map[string]string{"go.mod": "module example.com/glade\n\ngo 1.23.0\n", "main.go": "package main\n", "scripts/smoke.sh": "#!/bin/sh\n"})
	toolsRoot := newInventoryRepository(t, map[string]string{"go.mod": "module example.com/tools\n\ngo 1.23.0\n", "main.go": "package main\n", "scripts/release-check.sh": "#!/bin/sh\n"})
	candidatePath := filepath.Join(root, "glade")
	if err := os.WriteFile(candidatePath, []byte("binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	toolsPath, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	candidateCommit, toolsCommit := testGitOutput(t, gladeRoot, "rev-parse", "HEAD"), testGitOutput(t, toolsRoot, "rev-parse", "HEAD")
	candidate, err := runtimeArtifactFor(candidatePath, candidateCommit)
	if err != nil {
		t.Fatal(err)
	}
	tools, err := runtimeArtifactFor(toolsPath, toolsCommit)
	if err != nil {
		t.Fatal(err)
	}
	attemptPath := filepath.Join(root, "ATTEMPT.json")
	attempt := AssuranceAttempt{SchemaVersion: 1, InventorySHA256: strings.Repeat("a", 64), CandidateAuthoritySHA256: strings.Repeat("b", 64), Candidate: candidate, Tools: tools, RemoteCleanupAuthoritySHA256: testCleanupAuthorityHashes()}
	if err := WriteNewJSON(attemptPath, attempt); err != nil {
		t.Fatal(err)
	}
	freezePath := filepath.Join(root, "FINAL_TOOLS_COMMIT")
	if err := os.WriteFile(freezePath, []byte(toolsCommit+"\n"), 0o400); err != nil {
		t.Fatal(err)
	}
	validation, err := RunReleaseValidation(ReleaseValidationRequest{
		AttemptPath: attemptPath, GladeRoot: gladeRoot, CandidatePath: candidatePath,
		ToolsRoot: toolsRoot, ToolsPath: toolsPath, ToolsFreezePath: freezePath, OutputPath: filepath.Join(root, "RELEASE_VALIDATION.json"),
		runner: func(context.Context, releaseCommand) (salesforceCommandOutput, error) {
			return salesforceCommandOutput{}, nil
		},
	})
	if err != nil || validation.Candidate != candidate || validation.Tools != tools {
		t.Fatalf("RunReleaseValidation = %#v, %v", validation, err)
	}
}

func TestRunReleaseValidationSealsFourFixedChecks(t *testing.T) {
	root := t.TempDir()
	gladeRoot := newInventoryRepository(t, map[string]string{"go.mod": "module example.com/glade\n\ngo 1.23.0\n", "main.go": "package main\n", "scripts/smoke.sh": "#!/bin/sh\n"})
	toolsRoot := newInventoryRepository(t, map[string]string{"go.mod": "module example.com/tools\n\ngo 1.23.0\n", "main.go": "package main\n", "scripts/release-check.sh": "#!/bin/sh\n"})
	candidatePath := filepath.Join(root, "glade")
	if err := os.WriteFile(candidatePath, []byte("binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	toolsPath, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	toolsCommit := testGitOutput(t, toolsRoot, "rev-parse", "HEAD")
	attemptPath := writeReleaseAttempt(t, root, candidatePath, testGitOutput(t, gladeRoot, "rev-parse", "HEAD"), toolsPath, toolsCommit)
	freezePath := filepath.Join(root, "FINAL_TOOLS_COMMIT")
	if err := os.WriteFile(freezePath, []byte(toolsCommit+"\n"), 0o400); err != nil {
		t.Fatal(err)
	}
	var commands []releaseCommand
	outputPath := filepath.Join(root, "RELEASE_VALIDATION.json")
	validation, err := RunReleaseValidation(ReleaseValidationRequest{
		AttemptPath: attemptPath, GladeRoot: gladeRoot, CandidatePath: candidatePath,
		ToolsRoot: toolsRoot, ToolsPath: toolsPath, ToolsFreezePath: freezePath, OutputPath: outputPath,
		runner: func(_ context.Context, command releaseCommand) (salesforceCommandOutput, error) {
			commands = append(commands, command)
			return salesforceCommandOutput{Stdout: []byte("ok")}, nil
		},
	})
	if err != nil {
		t.Fatalf("RunReleaseValidation: %v", err)
	}
	if len(commands) != 4 || len(validation.Commands) != 4 || validation.Candidate.SHA256 != fileSHA256(t, candidatePath) || validation.Tools.SHA256 != fileSHA256(t, toolsPath) || validation.ToolsFreezeSHA256 != fileSHA256(t, freezePath) {
		t.Fatalf("validation = %#v, commands = %#v", validation, commands)
	}
	for _, command := range validation.Commands {
		if !command.Passed || command.WorkingDirectory == "" || !equalStrings(command.Environment, fixedReleaseEnvironment()) || command.TimeoutMS != releaseValidationTimeout.Milliseconds() {
			t.Fatalf("release command = %#v", command)
		}
	}
	for _, command := range commands {
		if command.Path == filepath.Join(runtime.GOROOT(), "bin", "go") && strings.Join(command.Args, " ") != "test -timeout 19m -count=1 ./..." {
			t.Fatalf("go release command = %#v", command)
		}
	}
	if _, err := os.Stat(outputPath); err != nil {
		t.Fatal(err)
	}
}

func TestRunReleaseValidationRejectsToolsPathThatIsNotTheExecutingBinary(t *testing.T) {
	root := t.TempDir()
	gladeRoot := newInventoryRepository(t, map[string]string{"go.mod": "module example.com/glade\n\ngo 1.23.0\n", "main.go": "package main\n", "scripts/smoke.sh": "#!/bin/sh\n"})
	toolsRoot := newInventoryRepository(t, map[string]string{"go.mod": "module example.com/tools\n\ngo 1.23.0\n", "main.go": "package main\n", "scripts/release-check.sh": "#!/bin/sh\n"})
	candidatePath, toolsPath := filepath.Join(root, "glade"), filepath.Join(root, "glade-tools")
	for _, path := range []string{candidatePath, toolsPath} {
		if err := os.WriteFile(path, []byte("binary"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	toolsCommit := testGitOutput(t, toolsRoot, "rev-parse", "HEAD")
	attemptPath := writeReleaseAttempt(t, root, candidatePath, testGitOutput(t, gladeRoot, "rev-parse", "HEAD"), toolsPath, toolsCommit)
	freezePath := filepath.Join(root, "FINAL_TOOLS_COMMIT")
	if err := os.WriteFile(freezePath, []byte(toolsCommit+"\n"), 0o400); err != nil {
		t.Fatal(err)
	}
	_, err := RunReleaseValidation(ReleaseValidationRequest{
		AttemptPath: attemptPath, GladeRoot: gladeRoot, CandidatePath: candidatePath,
		ToolsRoot: toolsRoot, ToolsPath: toolsPath, ToolsFreezePath: freezePath, OutputPath: filepath.Join(root, "RELEASE_VALIDATION.json"),
		runner: func(context.Context, releaseCommand) (salesforceCommandOutput, error) {
			t.Fatal("release validation ran checks before binding the executor")
			return salesforceCommandOutput{}, nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "executing glade-tools binary") {
		t.Fatalf("RunReleaseValidation error = %v", err)
	}
}

func TestRunReleaseCommandCapturesBothOutputStreams(t *testing.T) {
	root := t.TempDir()
	script := filepath.Join(root, "check.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf out\nprintf err >&2\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := runReleaseCommand(context.Background(), releaseCommand{Path: script, WorkingDirectory: root, Environment: []string{"PATH=/usr/bin:/bin"}})
	if err != nil || !bytes.Equal(output.Stdout, []byte("out")) || !bytes.Equal(output.Stderr, []byte("err")) {
		t.Fatalf("runReleaseCommand = %#v, %v", output, err)
	}
}

func TestValidateToolsLocalReplacementsRejectsUnboundCandidate(t *testing.T) {
	root := t.TempDir()
	gladeRoot := newInventoryRepository(t, map[string]string{"go.mod": "module github.com/glade-sh/glade\n\ngo 1.23.0\n"})
	otherRoot := newInventoryRepository(t, map[string]string{"go.mod": "module example.com/other\n\ngo 1.23.0\n"})
	toolsRoot := filepath.Join(root, "tools")
	if err := os.MkdirAll(toolsRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(toolsRoot, "go.mod"), []byte("module example.com/tools\n\ngo 1.23.0\n\nrequire (\n github.com/glade-sh/glade v0.0.0\n example.com/other v0.0.0\n)\n\nreplace github.com/glade-sh/glade => "+gladeRoot+"\nreplace example.com/other => "+otherRoot+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateToolsLocalReplacements(toolsRoot, gladeRoot); err == nil {
		t.Fatal("accepted a local replacement outside the sealed candidate root")
	}
}

func TestFixedReleaseCommandsDoNotInheritAmbientPATH(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{filepath.Join(root, "glade", "scripts", "smoke.sh"), filepath.Join(root, "tools", "scripts", "release-check.sh")} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", "/attacker/bin")
	commands, err := fixedReleaseCommands(filepath.Join(root, "glade"), filepath.Join(root, "tools"))
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range commands {
		if strings.Contains(strings.Join(command.Environment, "\n"), "/attacker/bin") {
			t.Fatalf("release environment inherits PATH: %#v", command.Environment)
		}
		if _, err := fixedReleaseGoBinary(command.Environment); err != nil {
			t.Fatalf("release environment cannot resolve Go: %#v, %v", command.Environment, err)
		}
	}
}

func TestFixedReleaseGoBinaryUsesSealedAbsolutePath(t *testing.T) {
	root := t.TempDir()
	goPath := filepath.Join(root, "go")
	if err := os.WriteFile(goPath, []byte("binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	got, err := fixedReleaseGoBinary([]string{"PATH=bin:" + root})
	if err != nil || got != goPath {
		t.Fatalf("fixedReleaseGoBinary = %q, %v", got, err)
	}
}

func TestFixedReleasePathOmitsRelativeGoRoot(t *testing.T) {
	for _, directory := range strings.Split(fixedReleasePath(""), string(filepath.ListSeparator)) {
		if !filepath.IsAbs(directory) {
			t.Fatalf("fixed release PATH has relative component %q", directory)
		}
	}
}

func TestFixedReleasePathIncludesMacOSSystemAdministrationTools(t *testing.T) {
	for _, directory := range strings.Split(fixedReleasePath(""), string(filepath.ListSeparator)) {
		if directory == "/usr/sbin" {
			return
		}
	}
	t.Fatalf("fixed release PATH omits /usr/sbin: %q", fixedReleasePath(""))
}

func TestFixedReleaseEnvironmentUsesWritableSealedHome(t *testing.T) {
	if !strings.Contains(strings.Join(fixedReleaseEnvironment(), "\n"), "HOME=/private/tmp/glade-assurance-home") {
		t.Fatalf("release environment has no writable sealed home: %#v", fixedReleaseEnvironment())
	}
}

func TestFixedReleaseEnvironmentDisablesMutableGoToolchainConfig(t *testing.T) {
	environment := strings.Join(fixedReleaseEnvironment(), "\n")
	for _, entry := range []string{"GOENV=off", "GOTOOLCHAIN=local"} {
		if !strings.Contains(environment, entry) {
			t.Fatalf("release environment lacks %q: %s", entry, environment)
		}
	}
}

func TestFixedReleaseEnvironmentPrefersSupportedMacTools(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Homebrew path is macOS-specific")
	}
	want := "PATH=" + fixedReleasePath(runtime.GOROOT())
	if !strings.Contains(strings.Join(fixedReleaseEnvironment(), "\n"), want) {
		t.Fatalf("release environment does not prefer supported macOS tools: %#v", fixedReleaseEnvironment())
	}
}

func TestFixedReleaseCommandsRejectAmbientGOROOT(t *testing.T) {
	t.Setenv("GOROOT", t.TempDir())
	if _, err := fixedReleaseCommands(t.TempDir(), t.TempDir()); err == nil {
		t.Fatal("accepted ambient GOROOT")
	}
}

func TestValidateToolsLocalReplacementsRejectsQuotedOutsidePath(t *testing.T) {
	root := t.TempDir()
	gladeRoot := newInventoryRepository(t, map[string]string{"go.mod": "module example.com/glade\n\ngo 1.23.0\n"})
	toolsRoot := filepath.Join(root, "tools")
	if err := os.MkdirAll(toolsRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(toolsRoot, "go.mod"), []byte("module example.com/tools\n\ngo 1.23.0\n\nreplace example.com/outside => \"../outside dir\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateToolsLocalReplacements(toolsRoot, gladeRoot); err == nil {
		t.Fatal("accepted quoted replacement outside sealed roots")
	}
}

func writeReleaseAttempt(t *testing.T, root, candidatePath, candidateCommit, toolsPath, toolsCommit string) string {
	t.Helper()
	candidate, err := runtimeArtifactFor(candidatePath, candidateCommit)
	if err != nil {
		t.Fatal(err)
	}
	tools, err := runtimeArtifactFor(toolsPath, toolsCommit)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "ATTEMPT.json")
	attempt := AssuranceAttempt{SchemaVersion: 1, InventorySHA256: strings.Repeat("a", 64), CandidateAuthoritySHA256: strings.Repeat("b", 64), Candidate: candidate, Tools: tools, RemoteCleanupAuthoritySHA256: testCleanupAuthorityHashes()}
	if err := WriteNewJSON(path, attempt); err != nil {
		t.Fatal(err)
	}
	return path
}
