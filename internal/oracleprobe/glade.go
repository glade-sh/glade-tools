package oracleprobe

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
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

// gladeExecEnvelope is the minimum shape needed to extract the oracle
// marker from a Glade exec --json response.  data.debug is an array of
// debug log strings; data.debugEvents[].message carries debug event
// messages.
type gladeExecEnvelope struct {
	Data struct {
		Debug       []string                 `json:"debug"`
		DebugEvents []gladeExecDebugEvent    `json:"debugEvents"`
	} `json:"data"`
}

type gladeExecDebugEvent struct {
	Message string `json:"message"`
}

// extractEnvelopedOutput tries to decode the raw output as a Glade
// exec --json envelope.  When successful, it returns the joined
// contents of data.debug and data.debugEvents[].message for
// parseResults.  When the output is not a valid envelope, it returns
// the original text so existing raw-marker callers keep working.
func extractEnvelopedOutput(raw []byte) string {
	var env gladeExecEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		// Not a valid JSON envelope — pass through as raw text.
		return string(raw)
	}
	var lines []string
	lines = append(lines, env.Data.Debug...)
	for _, de := range env.Data.DebugEvents {
		if de.Message != "" {
			lines = append(lines, de.Message)
		}
	}
	return strings.Join(lines, "\n")
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
		return Report{}, fmt.Errorf("glade exec failed: %w", err)
	}

	// Decode the JSON envelope first, then pass the extracted
	// debug lines to the existing marker parser.
	text := extractEnvelopedOutput(out)
	results, err := parseResults(text)
	if err != nil {
		return Report{}, fmt.Errorf("glade parse results: %w", err)
	}

	return Report{Results: results}, nil
}
