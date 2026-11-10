package corpusrun

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
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
	if repo.Blocked != "" {
		return infra(repo, "blocked: %s", repo.Blocked)
	}
	src, ok := config.ResolvePath(repo.Clone)
	if !ok {
		return infra(repo, "clone not found")
	}
	head, err := gitHead(ctx, src)
	if err != nil || !strings.HasPrefix(head, repo.Pin) && !strings.HasPrefix(repo.Pin, head) {
		return infra(repo, "clone is not at the config pin")
	}
	status, err := exec.CommandContext(ctx, "git", "-C", src, "status", "--porcelain", "--untracked-files=all").Output()
	if err != nil {
		return infra(repo, "cannot check clone cleanliness: %v", err)
	}
	if len(status) != 0 {
		return infra(repo, "clone is dirty")
	}
	if err := verifyComponentPins(src, repo.Overlay.ComponentPins); err != nil {
		return infra(repo, "components: %v", err)
	}
	root := filepath.Join(scratch, repo.Name)
	defer os.RemoveAll(root)
	if err := cloneTree(src, root); err != nil {
		return infra(repo, "copy failed: %v", err)
	}
	if err := verifyComponentPins(root, repo.Overlay.ComponentPins); err != nil {
		return infra(repo, "copied components: %v", err)
	}
	if repo.SourceArchive != "" || repo.Overlay.SourceArchiveSHA256 != "" {
		archive := repo.SourceArchive
		if strings.HasPrefix(archive, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				archive = filepath.Join(home, archive[2:])
			}
		} else if !filepath.IsAbs(archive) {
			archive = filepath.Join(config.dir, archive)
		}
		if err := applySourceArchive(root, archive, repo.Overlay.SourceArchiveSHA256, repo.Overlay.ComponentPins); err != nil {
			return infra(repo, "source overlay: %v", err)
		}
	}
	if err := applyOverlay(config, repo, root); err != nil {
		return infra(repo, "overlay: %v", err)
	}
	if err := validateProjectDependencyPaths(root); err != nil {
		return infra(repo, "project dependencies: %v", err)
	}

	start := time.Now()
	checkJSON, err := runGlade(ctx, opts, root, "check", "--project", root, "--format", "json", "--output", filepath.Join(root, ".glade-check.json"), "--no-progress", "--no-cache")
	if err == nil {
		checkJSON, err = os.ReadFile(filepath.Join(root, ".glade-check.json"))
	}
	if err != nil {
		return infra(repo, "check: %v", err)
	}
	testArgs := []string{"test", "--project", root, "--json", "--no-serve", "--no-progress", "--parallelism", strconv.Itoa(opts.Parallelism), "--gc-aggressive", "--test-timeout", "0"}
	var sourceDeps []string
	for _, name := range repo.Overlay.Deps {
		mode := config.DepSources[name].Mode
		if mode == "" || mode == "source" {
			sourceDeps = append(sourceDeps, filepath.Join(root, depsDir, name))
		}
	}
	if len(sourceDeps) > 0 {
		classFile, err := selectProjectClasses(ctx, opts, root, sourceDeps)
		if err != nil {
			return infra(repo, "class selection: %v", err)
		}
		testArgs = append(testArgs, "--class-file", classFile)
	}
	testJSON, err := runGlade(ctx, opts, root, testArgs...)
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

// validateProjectDependencyPaths prevents a fresh clone from silently losing
// dependencies that were only available as siblings of the source checkout.
func validateProjectDependencyPaths(root string) error {
	data, err := os.ReadFile(filepath.Join(root, "glade.yml"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var config struct {
		Project struct {
			DependencyPaths            []string `yaml:"dependencyPaths"`
			ManagedPackageDependencies []string `yaml:"managedPackageDependencies"`
		} `yaml:"project"`
	}
	if err := yaml.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("parse glade.yml: %w", err)
	}
	paths := append([]string(nil), config.Project.DependencyPaths...)
	for _, entry := range config.Project.ManagedPackageDependencies {
		_, path, ok := strings.Cut(entry, ":")
		if !ok || path == "" {
			return fmt.Errorf("invalid managed dependency entry")
		}
		paths = append(paths, path)
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	for _, path := range paths {
		if filepath.IsAbs(path) {
			return fmt.Errorf("dependency path is outside the project")
		}
		resolved, err := filepath.EvalSymlinks(filepath.Join(root, path))
		if err != nil {
			return fmt.Errorf("dependency path is unavailable: %w", err)
		}
		rel, err := filepath.Rel(resolvedRoot, resolved)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("dependency path is outside the project")
		}
		info, err := os.Stat(resolved)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("dependency path is not a directory")
		}
	}
	return nil
}

// selectProjectClasses follows the runner's own discovery and removes classes
// supplied by source overlays, as those are dependencies rather than tests of
// the project under evaluation.
func selectProjectClasses(ctx context.Context, opts Options, root string, depRoots []string) (string, error) {
	excluded := map[string]bool{}
	for _, dep := range depRoots {
		if err := filepath.WalkDir(dep, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !entry.IsDir() && strings.EqualFold(filepath.Ext(entry.Name()), ".cls") {
				excluded[strings.ToLower(strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name())))] = true
			}
			return nil
		}); err != nil {
			return "", err
		}
	}
	dir, err := os.MkdirTemp(root, ".corpus-classes-")
	if err != nil {
		return "", err
	}
	timeout := opts.StepTimeout
	if timeout <= 0 {
		timeout = 2 * time.Hour
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, opts.Glade, "test", "--project", root, "--shard-count", "1", "--write-class-shards", dir)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("discovery failed: %w: %s", err, out)
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.txt"))
	if err != nil || len(files) == 0 {
		return "", fmt.Errorf("discovery produced no class shard")
	}
	var names []string
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return "", err
		}
		for _, line := range strings.Split(string(data), "\n") {
			name := strings.TrimSpace(line)
			if name != "" && !excluded[strings.ToLower(name)] {
				names = append(names, name)
			}
		}
	}
	if len(names) == 0 {
		return "", fmt.Errorf("class selection is empty")
	}
	file := filepath.Join(dir, "selected.classes")
	if err := os.WriteFile(file, []byte(strings.Join(names, "\n")+"\n"), 0644); err != nil {
		return "", err
	}
	return file, nil
}

func gitHead(ctx context.Context, dir string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "HEAD").Output()
	return strings.TrimSpace(string(out)), err
}

// runGlade runs the binary and returns stdout. A non-zero exit is normal when
// tests fail with exit 1, while a higher exit code is an infrastructure error.
func runGlade(ctx context.Context, opts Options, dir string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, opts.StepTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, opts.Glade, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		return nil, fmt.Errorf("timeout after %s", opts.StepTimeout)
	}
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		return nil, err
	}
	if exit != nil && exit.ExitCode() != 1 {
		return nil, fmt.Errorf("glade exited with status %d: %s", exit.ExitCode(), oneLine(stderr.String(), dir))
	}
	if exit != nil && stdout.Len() == 0 {
		hasOutput := false
		for i := 0; i+1 < len(args); i++ {
			if args[i] != "--output" {
				continue
			}
			path := args[i+1]
			if !filepath.IsAbs(path) {
				path = filepath.Join(dir, path)
			}
			if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && info.Size() > 0 {
				hasOutput = true
			}
		}
		if !hasOutput {
			return nil, fmt.Errorf("glade exited with status 1 without output: %s", oneLine(stderr.String(), dir))
		}
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

// applyOverlay wires dependencies and merges the requested project settings
// into the copied project. Missing or invalid dependencies fail closed.
func applyOverlay(config *Config, repo Repo, root string) error {
	var extra, managed, dependencyPaths []string
	for _, name := range repo.Overlay.Deps {
		spec := config.DepSources[name]
		expected, pinned := repo.Overlay.DependencyPins[name]
		if repo.Overlay.DependencyPins != nil && !pinned {
			return fmt.Errorf("dependency %q has no pin", name)
		}
		if spec.Mode == "absent" {
			if pinned && expected != "absent" {
				return fmt.Errorf("dependency %q pin differs from absent mode", name)
			}
			continue
		}
		dest := filepath.Join(root, depsDir, name)
		rel := filepath.ToSlash(filepath.Join(depsDir, name))
		if spec.Mode == "stub" {
			if pinned {
				data, err := json.Marshal(spec.Packages)
				if err != nil {
					return err
				}
				if expected != fmt.Sprintf("stub:%x", sha256.Sum256(data)) {
					return fmt.Errorf("dependency %q stub pin differs", name)
				}
			}
			data, err := os.ReadFile(filepath.Join(root, "sfdx-project.json"))
			if err != nil {
				return err
			}
			var project struct {
				PackageDirectories []struct {
					Dependencies []struct {
						Package string `json:"package"`
					} `json:"dependencies"`
				} `json:"packageDirectories"`
			}
			if err := json.Unmarshal(data, &project); err != nil {
				return err
			}
			declared := map[string]bool{}
			for _, d := range project.PackageDirectories {
				for _, p := range d.Dependencies {
					declared[p.Package] = true
				}
			}
			if len(spec.Packages) == 0 {
				return fmt.Errorf("stub packages must be explicitly declared by the consumer")
			}
			var dirs []map[string]any
			for i, p := range spec.Packages {
				if !declared[p] {
					return fmt.Errorf("stub packages must be explicitly declared by the consumer")
				}
				sub := fmt.Sprintf("src%d", i)
				if err := os.MkdirAll(filepath.Join(dest, sub), 0755); err != nil {
					return err
				}
				dirs = append(dirs, map[string]any{"path": sub, "default": true, "package": p})
			}
			data, err = json.Marshal(map[string]any{"namespace": "", "packageDirectories": dirs})
			if err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(dest, "sfdx-project.json"), data, 0644); err != nil {
				return err
			}
			dependencyPaths = append(dependencyPaths, rel)
			continue
		}
		src, ok := config.ResolvePath(spec.Paths)
		if spec.RepoPath != "" {
			candidate := filepath.Join(root, spec.RepoPath)
			resolvedRoot, err := filepath.EvalSymlinks(root)
			if err != nil {
				return err
			}
			resolved, err := filepath.EvalSymlinks(candidate)
			if err != nil {
				return fmt.Errorf("dependency %q repoPath unavailable: %w", name, err)
			}
			relative, err := filepath.Rel(resolvedRoot, resolved)
			if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(spec.RepoPath) {
				return fmt.Errorf("dependency %q repoPath escapes the project", name)
			}
			src, ok = resolved, true
		}
		if !ok {
			return fmt.Errorf("dependency %q source unavailable", name)
		}
		if pinned {
			head, err := gitHead(context.Background(), src)
			if err != nil || expected != "git:"+head {
				return fmt.Errorf("dependency %q pin differs from source", name)
			}
			status, err := exec.Command("git", "-C", src, "status", "--porcelain", "--untracked-files=all", "--", ".").Output()
			if err != nil || len(status) != 0 {
				return fmt.Errorf("dependency %q pinned source is not clean", name)
			}
		}
		if spec.Mode == "project" {
			relative, err := filepath.Rel(src, dest)
			if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				return fmt.Errorf("dependency %q destination lies inside its source", name)
			}
			if err := cloneTree(src, dest); err != nil {
				return err
			}
			dependencyPaths = append(dependencyPaths, rel)
			continue
		}
		if spec.Mode == "managed" {
			if err := cloneTree(src, dest); err != nil {
				return err
			}
			path := filepath.Join(dest, "sfdx-project.json")
			data, err := os.ReadFile(path)
			var project map[string]any
			if errors.Is(err, os.ErrNotExist) {
				if spec.Namespace == "" {
					return fmt.Errorf("managed dependency %q without a manifest needs a namespace", name)
				}
				dirs := spec.Dirs
				if len(dirs) == 0 {
					dirs = []string{"."}
				}
				packages := make([]map[string]any, 0, len(dirs))
				for _, dir := range dirs {
					clean := filepath.Clean(dir)
					if filepath.IsAbs(dir) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
						return fmt.Errorf("managed dependency %q directory escapes its source", name)
					}
					resolved, err := filepath.EvalSymlinks(filepath.Join(dest, clean))
					if err != nil {
						return fmt.Errorf("managed dependency %q directory unavailable: %w", name, err)
					}
					base, err := filepath.EvalSymlinks(dest)
					if err != nil {
						return err
					}
					rel, err := filepath.Rel(base, resolved)
					if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
						return fmt.Errorf("managed dependency %q directory escapes its source", name)
					}
					if info, err := os.Stat(resolved); err != nil || !info.IsDir() {
						return fmt.Errorf("managed dependency %q directory unavailable", name)
					}
					packages = append(packages, map[string]any{"path": filepath.ToSlash(clean), "default": true})
				}
				project = map[string]any{"namespace": spec.Namespace, "packageDirectories": packages}
				data, err = json.MarshalIndent(project, "", "  ")
				if err != nil {
					return err
				}
				if err := os.WriteFile(path, append(data, '\n'), 0644); err != nil {
					return err
				}
			} else if err != nil {
				return err
			} else if err := json.Unmarshal(data, &project); err != nil {
				return err
			}
			ns := spec.Namespace
			if ns == "" {
				ns, _ = project["namespace"].(string)
			}
			if ns == "" {
				return fmt.Errorf("managed dependency %q needs a namespace", name)
			}
			if len(spec.MarkDefault) > 0 {
				dirs, _ := project["packageDirectories"].([]any)
				for _, item := range dirs {
					d, ok := item.(map[string]any)
					if !ok {
						continue
					}
					p, _ := d["path"].(string)
					for _, mark := range spec.MarkDefault {
						if filepath.Clean(p) == filepath.Clean(mark) {
							d["default"] = true
						}
					}
				}
				data, err = json.MarshalIndent(project, "", "  ")
				if err != nil {
					return err
				}
				if err := os.WriteFile(path, data, 0644); err != nil {
					return err
				}
			}
			managed = append(managed, ns+":"+rel)
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
	if len(extra) > 0 || repo.Overlay.Config.ProjectNamespace != nil {
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
		if len(extra) > 0 {
			project["packageDirectories"] = dirs
		}
		if repo.Overlay.Config.ProjectNamespace != nil {
			project["namespace"] = *repo.Overlay.Config.ProjectNamespace
		}
		out, _ := json.MarshalIndent(project, "", "  ")
		if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil {
			return err
		}
	}
	cfg := repo.Overlay.Config
	yml := filepath.Join(root, "glade.yml")
	if len(cfg.Features) == 0 && len(cfg.PermissionSets) == 0 && len(cfg.ExternalNamespaces) == 0 && cfg.UserLocale == "" && len(managed) == 0 && len(dependencyPaths) == 0 {
		return nil
	}
	return mergeOverlayYAML(yml, map[string]map[string][]string{
		"project": {"managedPackageDependencies": managed, "dependencyPaths": dependencyPaths, "externalNamespaces": cfg.ExternalNamespaces},
		"org":     {"features": cfg.Features}, "test": {"permissionSets": cfg.PermissionSets},
	}, cfg.UserLocale)
}

func mergeOverlayYAML(path string, additions map[string]map[string][]string, locale string) error {
	config := map[string]any{}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil {
		if err := yaml.Unmarshal(data, &config); err != nil {
			return fmt.Errorf("parse glade.yml: %w", err)
		}
		if config == nil {
			config = map[string]any{}
		}
	}
	for section, fields := range additions {
		for key, items := range fields {
			if len(items) == 0 {
				continue
			}
			if config[section] == nil {
				config[section] = map[string]any{}
			}
			m, ok := config[section].(map[string]any)
			if !ok {
				return fmt.Errorf("glade.yml %s is not a mapping", section)
			}
			var values []any
			if m[key] != nil {
				var ok bool
				values, ok = m[key].([]any)
				if !ok {
					return fmt.Errorf("glade.yml %s.%s is not a list", section, key)
				}
			}
			for _, item := range items {
				found := false
				for _, existing := range values {
					if s, ok := existing.(string); ok && s == item {
						found = true
						break
					}
				}
				if !found {
					values = append(values, item)
				}
			}
			m[key] = values
		}
	}
	if locale != "" {
		if config["test"] == nil {
			config["test"] = map[string]any{}
		}
		test, ok := config["test"].(map[string]any)
		if !ok {
			return fmt.Errorf("glade.yml test is not a mapping")
		}
		if test["user"] == nil {
			test["user"] = map[string]any{}
		}
		user, ok := test["user"].(map[string]any)
		if !ok {
			return fmt.Errorf("glade.yml test.user is not a mapping")
		}
		if old, ok := user["locale"]; ok {
			s, valid := old.(string)
			if !valid || s != locale {
				return fmt.Errorf("glade.yml test.user.locale conflicts with overlay")
			}
		}
		user["locale"] = locale
	}
	var node yaml.Node
	if err := node.Encode(config); err != nil {
		return err
	}
	inlineScalarLists(&node)
	data, err = yaml.Marshal(&node)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// Glade's configuration parser accepts scalar lists in inline form, rather
// than the block sequences emitted by yaml.Marshal by default. Keep sequences
// of mappings in block form for the separate structured-dependency parser.
func inlineScalarLists(node *yaml.Node) {
	if node.Kind == yaml.SequenceNode {
		allScalar := true
		for _, item := range node.Content {
			if item.Kind != yaml.ScalarNode {
				allScalar = false
				break
			}
		}
		if allScalar {
			node.Style = yaml.FlowStyle
		}
	}
	for _, child := range node.Content {
		inlineScalarLists(child)
	}
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
