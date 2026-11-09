package corpusrun

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Options configures Run.
type Options struct {
	Glade       string        // glade binary
	ConfigDir   string        // private config directory
	Tier        string        // A, B or C
	Only        []string      // narrow the tier
	Scratch     string        // scratch parent; default is a temp dir
	StepTimeout time.Duration // per glade command; default 2h
	Parallelism int           // glade test --parallelism; default 1
	Progress    func(string)  // optional progress lines (stderr)
}

const depsDir = ".glade-corpus-deps"

// Run runs every selected repo and compares it with its golden. Per-repo
// problems become INFRA results; only a bad config or selection is an error.
func Run(ctx context.Context, opts Options) (RunReport, error) {
	config, err := LoadConfig(opts.ConfigDir)
	if err != nil {
		return RunReport{}, err
	}
	repos, err := config.Select(opts.Tier, opts.Only)
	if err != nil {
		return RunReport{}, err
	}
	if opts.StepTimeout <= 0 {
		opts.StepTimeout = 2 * time.Hour
	}
	if opts.Parallelism <= 0 {
		opts.Parallelism = 1
	}
	scratch := opts.Scratch
	if scratch == "" {
		if scratch, err = os.MkdirTemp("", "corpus-run-"); err != nil {
			return RunReport{}, err
		}
		defer os.RemoveAll(scratch)
	}
	var results []RepoResult
	for _, repo := range repos {
		if opts.Progress != nil {
			opts.Progress("running " + repo.Name)
		}
		res := runRepo(ctx, config, repo, opts, scratch)
		if opts.Progress != nil {
			opts.Progress(repo.Name + " " + res.Diff.Line())
		}
		results = append(results, res)
	}
	return NewRunReport(opts.Tier, results), nil
}

func infra(repo Repo, format string, args ...any) RepoResult {
	return RepoResult{Repo: repo.Name, Diff: Diff{Infra: fmt.Sprintf(format, args...)}}
}

func runRepo(ctx context.Context, config *Config, repo Repo, opts Options, scratch string) RepoResult {
	src, ok := config.ResolvePath(repo.Clone)
	if !ok {
		return infra(repo, "clone not found")
	}
	head, err := gitHead(ctx, src)
	if err != nil || !strings.HasPrefix(head, repo.Pin) && !strings.HasPrefix(repo.Pin, head) {
		return infra(repo, "clone is not at the config pin")
	}
	root := filepath.Join(scratch, repo.Name)
	defer os.RemoveAll(root)
	if err := cloneTree(src, root); err != nil {
		return infra(repo, "copy failed: %v", err)
	}
	if err := applyOverlay(config, repo, root); err != nil {
		return infra(repo, "overlay: %v", err)
	}

	start := time.Now()
	checkJSON, err := runGlade(ctx, opts, root, "check", "--project", root, "--format", "json", "--output", filepath.Join(root, ".glade-check.json"), "--no-progress", "--no-cache")
	if err == nil {
		checkJSON, err = os.ReadFile(filepath.Join(root, ".glade-check.json"))
	}
	if err != nil {
		return infra(repo, "check: %v", err)
	}
	testJSON, err := runGlade(ctx, opts, root, "test", "--project", root, "--json", "--no-serve", "--no-progress", "--parallelism", strconv.Itoa(opts.Parallelism), "--gc-aggressive", "--test-timeout", "0")
	if err != nil {
		return infra(repo, "test: %v", err)
	}
	wall := time.Since(start).Seconds()
	check, err := parseCheckOutput(checkJSON, root)
	if err != nil {
		return infra(repo, "check: %v", err)
	}
	test, err := parseTestOutput(testJSON, root)
	if err != nil {
		return infra(repo, "test: %v", err)
	}
	observed := Observed{
		Pin:         repo.Pin,
		Overlay:     repo.Overlay,
		Glade:       GladeInfo{SourceAPI: check.SourceAPI},
		Counts:      Counts{Classes: check.Classes, Tests: test.Tests, Passed: test.Passed},
		Failing:     test.Failing,
		Timeouts:    test.Timeouts,
		Diagnostics: check.Diagnostics,
		WallSeconds: float64(int(wall*10)) / 10,
	}
	res := RepoResult{Repo: repo.Name, Complete: true, Observed: observed}
	golden, err := LoadGolden(config.dir, repo.Name)
	switch {
	case errors.Is(err, os.ErrNotExist):
		res.Diff.Infra = "no golden"
	case err != nil:
		res.Diff.Infra = err.Error()
	default:
		res.Diff = Compare(golden, observed)
	}
	return res
}

func gitHead(ctx context.Context, dir string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "HEAD").Output()
	return strings.TrimSpace(string(out)), err
}

// runGlade runs the binary and returns stdout. A non-zero exit is normal when
// tests fail, so only a missing start, a timeout or empty output is an error.
func runGlade(ctx context.Context, opts Options, dir string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, opts.StepTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, opts.Glade, args...)
	cmd.Dir = dir
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	err := cmd.Run()
	if ctx.Err() != nil {
		return nil, fmt.Errorf("timeout after %s", opts.StepTimeout)
	}
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		return nil, err
	}
	return stdout.Bytes(), nil
}

func cloneTree(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	os.RemoveAll(dst)
	if exec.Command("cp", "-cR", src, dst).Run() == nil {
		return nil
	}
	os.RemoveAll(dst)
	return exec.Command("cp", "-R", src, dst).Run()
}

// applyOverlay wires source dependencies in as extra package directories and
// writes glade.yml for features and permission sets, as the v1 sweep did. A
// dependency whose paths all miss is skipped; managed and stub modes are not
// supported yet and fail the repo.
func applyOverlay(config *Config, repo Repo, root string) error {
	var extra []string
	for _, name := range repo.Overlay.Deps {
		spec := config.DepSources[name]
		if spec.Mode == "managed" || spec.Mode == "stub" {
			return fmt.Errorf("dependency mode %q is not supported by the runner", spec.Mode)
		}
		src, ok := config.ResolvePath(spec.Paths)
		if !ok {
			continue
		}
		dirs := spec.Dirs
		if len(dirs) == 0 {
			dirs = projectDirs(src)
		}
		for _, d := range dirs {
			rel := filepath.Join(depsDir, name, filepath.Clean(d))
			if strings.HasPrefix(filepath.Clean(d), "..") || filepath.IsAbs(d) {
				return fmt.Errorf("dependency dir %q escapes its source", d)
			}
			if err := cloneTree(filepath.Join(src, d), filepath.Join(root, rel)); err != nil {
				return err
			}
			extra = append(extra, rel)
		}
	}
	if len(extra) > 0 {
		path := filepath.Join(root, "sfdx-project.json")
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read sfdx-project.json: %w", err)
		}
		var project map[string]any
		if err := json.Unmarshal(data, &project); err != nil {
			return fmt.Errorf("parse sfdx-project.json: %w", err)
		}
		dirs, _ := project["packageDirectories"].([]any)
		for _, rel := range extra {
			dirs = append(dirs, map[string]any{"path": rel})
		}
		project["packageDirectories"] = dirs
		out, _ := json.MarshalIndent(project, "", "  ")
		if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil {
			return err
		}
	}
	cfg := repo.Overlay.Config
	yml := filepath.Join(root, "glade.yml")
	if _, err := os.Stat(yml); err == nil || (len(cfg.Features) == 0 && len(cfg.PermissionSets) == 0) {
		return nil
	}
	var b strings.Builder
	if len(cfg.Features) > 0 {
		b.WriteString("org:\n  features: " + jsonList(cfg.Features) + "\n")
	}
	if len(cfg.PermissionSets) > 0 {
		b.WriteString("test:\n  permissionSets: " + jsonList(cfg.PermissionSets) + "\n")
	}
	return os.WriteFile(yml, []byte(b.String()), 0o644)
}

func jsonList(items []string) string {
	data, _ := json.Marshal(items)
	return string(data)
}

func projectDirs(src string) []string {
	data, err := os.ReadFile(filepath.Join(src, "sfdx-project.json"))
	if err == nil {
		var p struct {
			PackageDirectories []struct {
				Path string `json:"path"`
			} `json:"packageDirectories"`
		}
		if json.Unmarshal(data, &p) == nil {
			var dirs []string
			for _, d := range p.PackageDirectories {
				if d.Path != "" {
					dirs = append(dirs, d.Path)
				}
			}
			if len(dirs) > 0 {
				return dirs
			}
		}
	}
	return []string{"."}
}
