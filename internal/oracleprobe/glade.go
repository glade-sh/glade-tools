package oracleprobe

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

// CmdRunner abstracts command execution so fake runners can be
// injected during tests.
type CmdRunner interface {
	RunContext(ctx context.Context, name string, arg ...string) ([]byte, error)
}

// OSExecRunner is the real runner backed by os/exec.
type OSExecRunner struct{}

func (OSExecRunner) RunContext(ctx context.Context, name string, arg ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, arg...)
	return cmd.CombinedOutput()
}

// GladeOptions holds the configuration needed to run Glade probes.
type GladeOptions struct {
	GladeBin   string // path to the Glade binary
	ProjectDir string // temp project directory (created if empty)
}

// RunGlade executes anonymous Apex cases through the Glade CLI and
// returns a parsed report.  The Apex source is passed as the final
// positional argument — the caller never invokes a shell.
func RunGlade(ctx context.Context, runner CmdRunner, opts GladeOptions, cases []Case) (Report, error) {
	dir := opts.ProjectDir
	if dir == "" {
		tmp, err := os.MkdirTemp("", "glade-stdlib-oracle-*")
		if err != nil {
			return Report{}, fmt.Errorf("create glade temp project dir: %w", err)
		}
		dir = tmp
	}

	source := RenderAnonymous(cases)

	// Contract: glade exec --project <dir> --json <apex-source>
	args := []string{"exec", "--project", dir, "--json", source}
	out, err := runner.RunContext(ctx, opts.GladeBin, args...)
	if err != nil {
		return Report{}, fmt.Errorf("glade exec failed: %w\noutput:\n%s", err, string(out))
	}

	results, err := parseResults(string(out))
	if err != nil {
		return Report{}, fmt.Errorf("glade parse results: %w", err)
	}

	return Report{Results: results}, nil
}
