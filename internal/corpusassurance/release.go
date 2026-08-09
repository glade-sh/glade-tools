package corpusassurance

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const releaseValidationTimeout = 20 * time.Minute

type ReleaseValidation struct {
	SchemaVersion     int                    `json:"schemaVersion"`
	Candidate         RuntimeArtifact        `json:"candidate"`
	Tools             RuntimeArtifact        `json:"tools"`
	ToolsFreezeSHA256 string                 `json:"toolsFreezeSha256"`
	Commands          []ReleaseCommandResult `json:"commands"`
}

type ReleaseCommandResult struct {
	CommandResult
	WorkingDirectory string   `json:"workingDirectory"`
	Environment      []string `json:"environment"`
	TimeoutMS        int64    `json:"timeoutMs"`
}

type ReleaseValidationRequest struct {
	GladeRoot       string
	CandidatePath   string
	CandidateCommit string
	ToolsRoot       string
	ToolsPath       string
	ToolsCommit     string
	ToolsFreezePath string
	OutputPath      string
	runner          releaseCommandRunner
}

type releaseCommand struct {
	Path             string
	Args             []string
	WorkingDirectory string
	Environment      []string
	Timeout          time.Duration
}

type releaseCommandRunner func(context.Context, releaseCommand) (salesforceCommandOutput, error)

// RunReleaseValidation seals the complete fixed release checks only after the
// frozen tools commit, clean source roots, and executable hashes agree.
func RunReleaseValidation(request ReleaseValidationRequest) (ReleaseValidation, error) {
	for _, path := range []string{request.GladeRoot, request.CandidatePath, request.ToolsRoot, request.ToolsPath, request.ToolsFreezePath, request.OutputPath} {
		if !filepath.IsAbs(path) {
			return ReleaseValidation{}, fmt.Errorf("absolute release-validation paths are required")
		}
	}
	if !commitPattern.MatchString(request.CandidateCommit) || !commitPattern.MatchString(request.ToolsCommit) {
		return ReleaseValidation{}, fmt.Errorf("release-validation commits are invalid")
	}
	if _, err := os.Lstat(request.OutputPath); err == nil {
		return ReleaseValidation{}, fmt.Errorf("release-validation output already exists: %s", request.OutputPath)
	} else if !os.IsNotExist(err) {
		return ReleaseValidation{}, err
	}
	freezeInfo, err := os.Stat(request.ToolsFreezePath)
	if err != nil || !freezeInfo.Mode().IsRegular() || freezeInfo.Mode().Perm() != 0o400 {
		return ReleaseValidation{}, fmt.Errorf("frozen tools commit must be mode 0400")
	}
	freezeBytes, err := os.ReadFile(request.ToolsFreezePath)
	if err != nil || strings.TrimSpace(string(freezeBytes)) != request.ToolsCommit {
		return ReleaseValidation{}, fmt.Errorf("frozen tools commit does not match requested tools commit")
	}
	freezeSHA := replayBytesSHA256(freezeBytes)
	if err := validateCleanGitRoot(request.GladeRoot, request.CandidateCommit); err != nil {
		return ReleaseValidation{}, fmt.Errorf("candidate source: %w", err)
	}
	if err := validateCleanGitRoot(request.ToolsRoot, request.ToolsCommit); err != nil {
		return ReleaseValidation{}, fmt.Errorf("tools source: %w", err)
	}
	candidate, err := runtimeArtifactFor(request.CandidatePath, request.CandidateCommit)
	if err != nil {
		return ReleaseValidation{}, fmt.Errorf("candidate: %w", err)
	}
	tools, err := runtimeArtifactFor(request.ToolsPath, request.ToolsCommit)
	if err != nil {
		return ReleaseValidation{}, fmt.Errorf("tools: %w", err)
	}
	commands, err := fixedReleaseCommands(request.GladeRoot, request.ToolsRoot)
	if err != nil {
		return ReleaseValidation{}, err
	}
	runner := request.runner
	if runner == nil {
		runner = runReleaseCommand
	}
	results := make([]ReleaseCommandResult, 0, len(commands))
	for _, command := range commands {
		result, err := runReleaseValidationCommand(runner, command)
		if err != nil {
			return ReleaseValidation{}, err
		}
		results = append(results, result)
	}
	if err := validateCleanGitRoot(request.GladeRoot, request.CandidateCommit); err != nil {
		return ReleaseValidation{}, fmt.Errorf("candidate source changed during release validation: %w", err)
	}
	if err := validateCleanGitRoot(request.ToolsRoot, request.ToolsCommit); err != nil {
		return ReleaseValidation{}, fmt.Errorf("tools source changed during release validation: %w", err)
	}
	if current, err := runtimeArtifactFor(request.CandidatePath, request.CandidateCommit); err != nil || current != candidate {
		return ReleaseValidation{}, fmt.Errorf("candidate changed during release validation")
	}
	if current, err := runtimeArtifactFor(request.ToolsPath, request.ToolsCommit); err != nil || current != tools {
		return ReleaseValidation{}, fmt.Errorf("tools changed during release validation")
	}
	if current, err := sha256File(request.ToolsFreezePath); err != nil || current != freezeSHA {
		return ReleaseValidation{}, fmt.Errorf("frozen tools commit changed during release validation")
	}
	validation := ReleaseValidation{SchemaVersion: 1, Candidate: candidate, Tools: tools, ToolsFreezeSHA256: freezeSHA, Commands: results}
	if err := WriteNewJSON(request.OutputPath, validation); err != nil {
		return ReleaseValidation{}, err
	}
	return validation, nil
}

func fixedReleaseCommands(gladeRoot, toolsRoot string) ([]releaseCommand, error) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		return nil, fmt.Errorf("find go: %w", err)
	}
	env := []string{"HOME=" + os.Getenv("HOME"), "PATH=" + os.Getenv("PATH"), "TMPDIR=" + os.TempDir()}
	commands := []releaseCommand{
		{Path: goBin, Args: []string{"test", "./..."}, WorkingDirectory: gladeRoot, Environment: env, Timeout: releaseValidationTimeout},
		{Path: filepath.Join(gladeRoot, "scripts", "smoke.sh"), WorkingDirectory: gladeRoot, Environment: env, Timeout: releaseValidationTimeout},
		{Path: goBin, Args: []string{"test", "./..."}, WorkingDirectory: toolsRoot, Environment: env, Timeout: releaseValidationTimeout},
		{Path: filepath.Join(toolsRoot, "scripts", "release-check.sh"), WorkingDirectory: toolsRoot, Environment: env, Timeout: releaseValidationTimeout},
	}
	for _, command := range commands {
		info, err := os.Stat(command.Path)
		if err != nil || !info.Mode().IsRegular() || command.WorkingDirectory == "" || !filepath.IsAbs(command.WorkingDirectory) {
			return nil, fmt.Errorf("release check command is unavailable")
		}
	}
	return commands, nil
}

func runReleaseValidationCommand(runner releaseCommandRunner, command releaseCommand) (ReleaseCommandResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), command.Timeout)
	defer cancel()
	started := time.Now()
	output, err := runner(ctx, command)
	receipt := ReleaseCommandResult{CommandResult: CommandResult{Command: append([]string{command.Path}, command.Args...), CommandSpecSHA256: releaseCommandSpecSHA256(command), ExitCode: output.ExitCode, DurationMS: time.Since(started).Milliseconds(), StdoutSHA256: replayBytesSHA256(output.Stdout), StderrSHA256: replayBytesSHA256(output.Stderr), Passed: err == nil && output.ExitCode == 0, TimedOut: ctx.Err() == context.DeadlineExceeded}, WorkingDirectory: command.WorkingDirectory, Environment: append([]string(nil), command.Environment...), TimeoutMS: command.Timeout.Milliseconds()}
	if !receipt.Passed || receipt.TimedOut {
		return ReleaseCommandResult{}, fmt.Errorf("release validation command failed")
	}
	return receipt, nil
}

func releaseCommandSpecSHA256(command releaseCommand) string {
	parts := []string{command.WorkingDirectory, command.Path, strings.Join(command.Args, "\x00"), strings.Join(command.Environment, "\x00"), command.Timeout.String()}
	return replayBytesSHA256([]byte(strings.Join(parts, "\x00")))
}

func runReleaseCommand(ctx context.Context, command releaseCommand) (salesforceCommandOutput, error) {
	execCommand := exec.CommandContext(ctx, command.Path, command.Args...)
	execCommand.Dir, execCommand.Env = command.WorkingDirectory, command.Environment
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	execCommand.Stdout, execCommand.Stderr = stdout, stderr
	err := execCommand.Run()
	result := salesforceCommandOutput{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	if exit, ok := err.(*exec.ExitError); ok {
		result.ExitCode = exit.ExitCode()
		return result, nil
	}
	return result, err
}
