package corpusassurance

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
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
	freezePath := filepath.Join(root, "FINAL_TOOLS_COMMIT")
	if err := os.WriteFile(freezePath, []byte(strings.Repeat("f", 40)+"\n"), 0o400); err != nil {
		t.Fatal(err)
	}
	_, err = RunReleaseValidation(ReleaseValidationRequest{
		GladeRoot: gladeRoot, CandidatePath: candidatePath, CandidateCommit: testGitOutput(t, gladeRoot, "rev-parse", "HEAD"),
		ToolsRoot: toolsRoot, ToolsPath: toolsPath, ToolsCommit: testGitOutput(t, toolsRoot, "rev-parse", "HEAD"), ToolsFreezePath: freezePath,
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
	freezePath := filepath.Join(root, "FINAL_TOOLS_COMMIT")
	if err := os.WriteFile(freezePath, []byte(toolsCommit+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := RunReleaseValidation(ReleaseValidationRequest{
		GladeRoot: gladeRoot, CandidatePath: candidatePath, CandidateCommit: testGitOutput(t, gladeRoot, "rev-parse", "HEAD"),
		ToolsRoot: toolsRoot, ToolsPath: toolsPath, ToolsCommit: toolsCommit, ToolsFreezePath: freezePath, OutputPath: filepath.Join(root, "RELEASE_VALIDATION.json"),
	})
	if err == nil || !strings.Contains(err.Error(), "frozen tools commit must be mode 0400") {
		t.Fatalf("RunReleaseValidation error = %v", err)
	}
}

func TestRunReleaseValidationSealsFourFixedChecks(t *testing.T) {
	root := t.TempDir()
	gladeRoot := newInventoryRepository(t, map[string]string{"main.go": "package main\n", "scripts/smoke.sh": "#!/bin/sh\n"})
	toolsRoot := newInventoryRepository(t, map[string]string{"main.go": "package main\n", "scripts/release-check.sh": "#!/bin/sh\n"})
	candidatePath := filepath.Join(root, "glade")
	if err := os.WriteFile(candidatePath, []byte("binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	toolsPath, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	toolsCommit := testGitOutput(t, toolsRoot, "rev-parse", "HEAD")
	freezePath := filepath.Join(root, "FINAL_TOOLS_COMMIT")
	if err := os.WriteFile(freezePath, []byte(toolsCommit+"\n"), 0o400); err != nil {
		t.Fatal(err)
	}
	var commands []releaseCommand
	outputPath := filepath.Join(root, "RELEASE_VALIDATION.json")
	validation, err := RunReleaseValidation(ReleaseValidationRequest{
		GladeRoot: gladeRoot, CandidatePath: candidatePath, CandidateCommit: testGitOutput(t, gladeRoot, "rev-parse", "HEAD"),
		ToolsRoot: toolsRoot, ToolsPath: toolsPath, ToolsCommit: toolsCommit, ToolsFreezePath: freezePath, OutputPath: outputPath,
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
		if !command.Passed || command.WorkingDirectory == "" || len(command.Environment) != 3 || command.TimeoutMS != releaseValidationTimeout.Milliseconds() {
			t.Fatalf("release command = %#v", command)
		}
	}
	if _, err := os.Stat(outputPath); err != nil {
		t.Fatal(err)
	}
}

func TestRunReleaseValidationRejectsToolsPathThatIsNotTheExecutingBinary(t *testing.T) {
	root := t.TempDir()
	gladeRoot := newInventoryRepository(t, map[string]string{"main.go": "package main\n", "scripts/smoke.sh": "#!/bin/sh\n"})
	toolsRoot := newInventoryRepository(t, map[string]string{"main.go": "package main\n", "scripts/release-check.sh": "#!/bin/sh\n"})
	candidatePath, toolsPath := filepath.Join(root, "glade"), filepath.Join(root, "glade-tools")
	for _, path := range []string{candidatePath, toolsPath} {
		if err := os.WriteFile(path, []byte("binary"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	toolsCommit := testGitOutput(t, toolsRoot, "rev-parse", "HEAD")
	freezePath := filepath.Join(root, "FINAL_TOOLS_COMMIT")
	if err := os.WriteFile(freezePath, []byte(toolsCommit+"\n"), 0o400); err != nil {
		t.Fatal(err)
	}
	_, err := RunReleaseValidation(ReleaseValidationRequest{
		GladeRoot: gladeRoot, CandidatePath: candidatePath, CandidateCommit: testGitOutput(t, gladeRoot, "rev-parse", "HEAD"),
		ToolsRoot: toolsRoot, ToolsPath: toolsPath, ToolsCommit: toolsCommit, ToolsFreezePath: freezePath, OutputPath: filepath.Join(root, "RELEASE_VALIDATION.json"),
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
