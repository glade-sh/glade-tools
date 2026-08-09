package corpusassurance

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"time"

	"golang.org/x/mod/modfile"
)

const releaseValidationTimeout = 20 * time.Minute

type ReleaseValidation struct {
	SchemaVersion     int                    `json:"schemaVersion"`
	AttemptSHA256     string                 `json:"attemptSha256"`
	Candidate         RuntimeArtifact        `json:"candidate"`
	Tools             RuntimeArtifact        `json:"tools"`
	ToolsFreezeSHA256 string                 `json:"toolsFreezeSha256"`
	GladeRoot         string                 `json:"gladeRoot"`
	CandidatePath     string                 `json:"candidatePath"`
	ToolsRoot         string                 `json:"toolsRoot"`
	ToolsPath         string                 `json:"toolsPath"`
	Commands          []ReleaseCommandResult `json:"commands"`
}

type ReleaseCommandResult struct {
	CommandResult
	WorkingDirectory string   `json:"workingDirectory"`
	Environment      []string `json:"environment"`
	TimeoutMS        int64    `json:"timeoutMs"`
}

type ReleaseValidationRequest struct {
	AttemptPath     string
	GladeRoot       string
	CandidatePath   string
	ToolsRoot       string
	ToolsPath       string
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
	for _, path := range []string{request.AttemptPath, request.GladeRoot, request.CandidatePath, request.ToolsRoot, request.ToolsPath, request.ToolsFreezePath, request.OutputPath} {
		if !filepath.IsAbs(path) {
			return ReleaseValidation{}, fmt.Errorf("absolute release-validation paths are required")
		}
	}
	if _, err := os.Lstat(request.OutputPath); err == nil {
		return ReleaseValidation{}, fmt.Errorf("release-validation output already exists: %s", request.OutputPath)
	} else if !os.IsNotExist(err) {
		return ReleaseValidation{}, err
	}
	attempt, attemptBytes, err := readExactJSONBytes[AssuranceAttempt](request.AttemptPath)
	if err != nil || ValidateAssuranceAttempt(attempt) != nil {
		return ReleaseValidation{}, fmt.Errorf("invalid sealed assurance attempt")
	}
	freezeInfo, err := os.Stat(request.ToolsFreezePath)
	if err != nil || !freezeInfo.Mode().IsRegular() || freezeInfo.Mode().Perm() != 0o400 {
		return ReleaseValidation{}, fmt.Errorf("frozen tools commit must be mode 0400")
	}
	freezeBytes, err := os.ReadFile(request.ToolsFreezePath)
	if err != nil || strings.TrimSpace(string(freezeBytes)) != attempt.Tools.Commit {
		return ReleaseValidation{}, fmt.Errorf("frozen tools commit does not match sealed attempt")
	}
	freezeSHA := replayBytesSHA256(freezeBytes)
	if err := validateCleanGitRoot(request.GladeRoot, attempt.Candidate.Commit); err != nil {
		return ReleaseValidation{}, fmt.Errorf("candidate source: %w", err)
	}
	if err := validateCleanGitRoot(request.ToolsRoot, attempt.Tools.Commit); err != nil {
		return ReleaseValidation{}, fmt.Errorf("tools source: %w", err)
	}
	if err := validateToolsLocalReplacements(request.ToolsRoot, request.GladeRoot); err != nil {
		return ReleaseValidation{}, fmt.Errorf("tools replacements: %w", err)
	}
	if err := validateToolsLocalReplacements(request.GladeRoot, request.GladeRoot); err != nil {
		return ReleaseValidation{}, fmt.Errorf("candidate replacements: %w", err)
	}
	candidate, err := runtimeArtifactFor(request.CandidatePath, attempt.Candidate.Commit)
	if err != nil {
		return ReleaseValidation{}, fmt.Errorf("candidate: %w", err)
	}
	if candidate != attempt.Candidate {
		return ReleaseValidation{}, fmt.Errorf("candidate does not match sealed attempt")
	}
	tools, err := releaseExecutingTools(request.ToolsPath, attempt.Tools.Commit)
	if err != nil {
		return ReleaseValidation{}, fmt.Errorf("tools: %w", err)
	}
	if tools != attempt.Tools {
		return ReleaseValidation{}, fmt.Errorf("tools do not match sealed attempt")
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
	if err := validateCleanGitRoot(request.GladeRoot, attempt.Candidate.Commit); err != nil {
		return ReleaseValidation{}, fmt.Errorf("candidate source changed during release validation: %w", err)
	}
	if err := validateCleanGitRoot(request.ToolsRoot, attempt.Tools.Commit); err != nil {
		return ReleaseValidation{}, fmt.Errorf("tools source changed during release validation: %w", err)
	}
	if err := validateToolsLocalReplacements(request.ToolsRoot, request.GladeRoot); err != nil {
		return ReleaseValidation{}, fmt.Errorf("tools replacements changed during release validation: %w", err)
	}
	if err := validateToolsLocalReplacements(request.GladeRoot, request.GladeRoot); err != nil {
		return ReleaseValidation{}, fmt.Errorf("candidate replacements changed during release validation: %w", err)
	}
	if current, err := runtimeArtifactFor(request.CandidatePath, attempt.Candidate.Commit); err != nil || current != candidate || current != attempt.Candidate {
		return ReleaseValidation{}, fmt.Errorf("candidate changed during release validation")
	}
	if current, err := releaseExecutingTools(request.ToolsPath, attempt.Tools.Commit); err != nil || current != tools || current != attempt.Tools {
		return ReleaseValidation{}, fmt.Errorf("tools changed during release validation")
	}
	if current, err := sha256File(request.ToolsFreezePath); err != nil || current != freezeSHA {
		return ReleaseValidation{}, fmt.Errorf("frozen tools commit changed during release validation")
	}
	if current, err := sha256File(request.AttemptPath); err != nil || current != replayBytesSHA256(attemptBytes) {
		return ReleaseValidation{}, fmt.Errorf("sealed assurance attempt changed during release validation")
	}
	validation := ReleaseValidation{SchemaVersion: 1, AttemptSHA256: replayBytesSHA256(attemptBytes), Candidate: candidate, Tools: tools, ToolsFreezeSHA256: freezeSHA, GladeRoot: request.GladeRoot, CandidatePath: request.CandidatePath, ToolsRoot: request.ToolsRoot, ToolsPath: request.ToolsPath, Commands: results}
	if err := WriteNewJSON(request.OutputPath, validation); err != nil {
		return ReleaseValidation{}, err
	}
	return validation, nil
}

func fixedReleaseCommands(gladeRoot, toolsRoot string) ([]releaseCommand, error) {
	if os.Getenv("GOROOT") != "" {
		return nil, fmt.Errorf("ambient GOROOT is not permitted")
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	env := fixedReleaseEnvironment()
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

func fixedReleaseEnvironment() []string {
	return []string{"HOME=/var/empty", "PATH=" + filepath.Join(runtime.GOROOT(), "bin") + ":/usr/local/bin:/usr/bin:/bin", "TMPDIR=/private/tmp", "GOCACHE=/private/tmp/glade-assurance-go-cache", "GOMODCACHE=/private/tmp/glade-assurance-go-mod", "GOWORK=off"}
}

func releaseExecutingTools(path, commit string) (RuntimeArtifact, error) {
	requested, err := filepath.EvalSymlinks(path)
	if err != nil {
		return RuntimeArtifact{}, fmt.Errorf("resolve tools path: %w", err)
	}
	executable, err := os.Executable()
	if err != nil {
		return RuntimeArtifact{}, fmt.Errorf("locate executing glade-tools binary: %w", err)
	}
	executing, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return RuntimeArtifact{}, fmt.Errorf("resolve executing glade-tools binary: %w", err)
	}
	if filepath.Clean(requested) != filepath.Clean(executing) {
		return RuntimeArtifact{}, fmt.Errorf("tools path does not identify the executing glade-tools binary")
	}
	return runtimeArtifactFor(executable, commit)
}

func runReleaseValidationCommand(runner releaseCommandRunner, command releaseCommand) (ReleaseCommandResult, error) {
	before, err := sha256File(command.Path)
	if err != nil {
		return ReleaseCommandResult{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), command.Timeout)
	defer cancel()
	started := time.Now()
	output, err := runner(ctx, command)
	after, hashErr := sha256File(command.Path)
	receipt := ReleaseCommandResult{CommandResult: CommandResult{Command: append([]string{command.Path}, command.Args...), ExecutableSHA256: before, ExecutableAfterSHA256: after, CommandSpecSHA256: releaseCommandSpecSHA256(command), ExitCode: output.ExitCode, DurationMS: time.Since(started).Milliseconds(), StdoutSHA256: replayBytesSHA256(output.Stdout), StderrSHA256: replayBytesSHA256(output.Stderr), Passed: err == nil && hashErr == nil && before == after && output.ExitCode == 0, TimedOut: ctx.Err() == context.DeadlineExceeded}, WorkingDirectory: command.WorkingDirectory, Environment: append([]string(nil), command.Environment...), TimeoutMS: command.Timeout.Milliseconds()}
	if !receipt.Passed || receipt.TimedOut {
		return ReleaseCommandResult{}, fmt.Errorf("release validation command failed")
	}
	return receipt, nil
}

func validateOracleReleaseSources(validation ReleaseValidation, plan OraclePlan) error {
	if err := validateCleanGitRoot(validation.GladeRoot, plan.Candidate.Commit); err != nil {
		return fmt.Errorf("candidate source: %w", err)
	}
	if err := validateCleanGitRoot(validation.ToolsRoot, plan.Tools.Commit); err != nil {
		return fmt.Errorf("tools source: %w", err)
	}
	if err := validateToolsLocalReplacements(validation.GladeRoot, validation.GladeRoot); err != nil {
		return fmt.Errorf("candidate replacements: %w", err)
	}
	if err := validateToolsLocalReplacements(validation.ToolsRoot, validation.GladeRoot); err != nil {
		return fmt.Errorf("tools replacements: %w", err)
	}
	if current, err := runtimeArtifactFor(validation.CandidatePath, plan.Candidate.Commit); err != nil || current != plan.Candidate {
		return fmt.Errorf("candidate executable does not match sealed release validation")
	}
	if current, err := releaseExecutingTools(validation.ToolsPath, plan.Tools.Commit); err != nil || current != plan.Tools {
		return fmt.Errorf("tools executable does not match sealed release validation")
	}
	commands, err := fixedReleaseCommands(validation.GladeRoot, validation.ToolsRoot)
	if err != nil || len(commands) != len(validation.Commands) {
		return fmt.Errorf("fixed release command contract is unavailable")
	}
	for index, command := range commands {
		result := validation.Commands[index]
		if !reflect.DeepEqual(result.Command, append([]string{command.Path}, command.Args...)) || result.WorkingDirectory != command.WorkingDirectory || !reflect.DeepEqual(result.Environment, command.Environment) || result.TimeoutMS != command.Timeout.Milliseconds() || result.CommandSpecSHA256 != releaseCommandSpecSHA256(command) {
			return fmt.Errorf("release command %d does not match fixed contract", index+1)
		}
		if current, err := sha256File(command.Path); err != nil || current != result.ExecutableSHA256 || current != result.ExecutableAfterSHA256 {
			return fmt.Errorf("release command %d executable changed", index+1)
		}
	}
	return nil
}

func validateToolsLocalReplacements(toolsRoot, gladeRoot string) error {
	toolsRoot, err := filepath.EvalSymlinks(toolsRoot)
	if err != nil {
		return err
	}
	gladeRoot, err = filepath.EvalSymlinks(gladeRoot)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(toolsRoot, "go.mod"))
	if err != nil {
		return err
	}
	parsed, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		return err
	}
	for _, replacement := range parsed.Replace {
		target := replacement.New.Path
		if replacement.New.Version != "" || (!filepath.IsAbs(target) && !strings.HasPrefix(target, "./") && !strings.HasPrefix(target, "../")) {
			continue
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(toolsRoot, target)
		}
		target, err = filepath.EvalSymlinks(target)
		if err != nil || (!pathWithin(gladeRoot, target) && !pathWithin(toolsRoot, target)) {
			return fmt.Errorf("local replacement is outside sealed roots")
		}
	}
	return nil
}

func pathWithin(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
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
