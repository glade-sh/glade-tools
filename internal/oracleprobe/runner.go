package oracleprobe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Options struct {
	TargetOrg string
	WorkDir   string
}

func RunAnonymous(ctx context.Context, cases []Case, opts Options) (Report, error) {
	if opts.TargetOrg == "" {
		return Report{}, fmt.Errorf("target org is required")
	}
	dir := opts.WorkDir
	if dir == "" {
		tmp, err := os.MkdirTemp("", "glade-stdlib-oracle-*")
		if err != nil {
			return Report{}, err
		}
		dir = tmp
	}
	apexPath := filepath.Join(dir, "probe.apex")
	if err := os.WriteFile(apexPath, []byte(RenderAnonymous(cases)), 0o644); err != nil {
		return Report{}, err
	}
	cmd := exec.CommandContext(ctx, "sf", "apex", "run", "--target-org", opts.TargetOrg, "--file", apexPath)
	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return Report{}, fmt.Errorf("sf apex run failed: %w\nstdout:\n%s\nstderr:\n%s", err, out.String(), stderr.String())
	}
	results, err := parseResults(out.String() + "\n" + stderr.String())
	if err != nil {
		return Report{}, err
	}
	return Report{TargetOrg: opts.TargetOrg, Results: results}, nil
}

func parseResults(text string) ([]Result, error) {
	for _, line := range strings.Split(text, "\n") {
		idx := strings.Index(line, marker)
		if idx < 0 {
			continue
		}
		raw := strings.TrimSpace(line[idx+len(marker):])
		if !strings.HasPrefix(raw, "[") && !strings.HasPrefix(raw, "{") {
			continue
		}
		var results []Result
		if err := json.Unmarshal([]byte(raw), &results); err != nil {
			return nil, fmt.Errorf("decode oracle marker JSON: %w", err)
		}
		for i := range results {
			results[i].RawLogLine = line
		}
		return results, nil
	}
	return nil, fmt.Errorf("oracle marker %q not found in sf output", marker)
}
