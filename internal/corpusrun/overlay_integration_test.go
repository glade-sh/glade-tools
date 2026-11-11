package corpusrun

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunRepoDependencyOverlayIntegration(t *testing.T) {
	for _, mode := range []string{"source", "managed", "stub"} {
		t.Run(mode, func(t *testing.T) {
			temp := t.TempDir()
			source := filepath.Join(temp, "source")
			dep := filepath.Join(temp, "dependency")
			scratch := filepath.Join(temp, "scratch")
			for _, dir := range []string{source, filepath.Join(dep, "src"), scratch} {
				if err := os.MkdirAll(dir, 0755); err != nil {
					t.Fatal(err)
				}
			}
			write := func(path, content string) {
				t.Helper()
				if err := os.WriteFile(path, []byte(content), 0644); err != nil {
					t.Fatal(err)
				}
			}
			write(filepath.Join(source, "sfdx-project.json"), `{"packageDirectories":[{"path":"src","dependencies":[{"package":"sample-package"}]}]}`)
			write(filepath.Join(dep, "sfdx-project.json"), `{"namespace":"sample","packageDirectories":[{"path":"src","default":true}]}`)
			write(filepath.Join(dep, "src", "DependencyTest.cls"), "synthetic")
			for _, args := range [][]string{{"init", "-q"}, {"add", "sfdx-project.json"}, {"-c", "user.name=test", "-c", "user.email=test@example.invalid", "commit", "-q", "-m", "initial"}} {
				cmd := exec.Command("git", append([]string{"-C", source}, args...)...)
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("git: %v %s", err, out)
				}
			}
			sha, err := exec.Command("git", "-C", source, "rev-parse", "HEAD").Output()
			if err != nil {
				t.Fatal(err)
			}
			script := `#!/bin/sh
case "$1" in
check)
 while [ "$#" -gt 0 ]; do if [ "$1" = "--output" ]; then printf '{"project":{"sourceApiVersion":"67.0"},"summary":{"types":1},"diagnostics":[]}' > "$2"; exit 0; fi; shift; done; exit 1;;
test)
 for arg in "$@"; do [ "$arg" != "--write-class-shards" ] || discover=1; done
 if [ "$discover" = 1 ]; then while [ "$#" -gt 0 ]; do if [ "$1" = "--write-class-shards" ]; then printf 'ProjectTest\nDependencyTest\n' > "$2/shard.txt"; exit 0; fi; shift; done; fi
 if [ "$OVERLAY_MODE" = source ]; then
  found=0; while [ "$#" -gt 0 ]; do if [ "$1" = "--class-file" ]; then found=1; grep -qx ProjectTest "$2" || exit 3; grep -q DependencyTest "$2" && exit 4; fi; shift; done; [ "$found" = 1 ] || exit 5
 else
  if [ "$OVERLAY_MODE" = managed ]; then grep -q managedPackageDependencies glade.yml || exit 6; else grep -q dependencyPaths glade.yml || exit 7; fi
 fi
 printf '{"summary":{"total":1,"passed":1},"tests":[{"name":"ProjectTest.test","status":"pass"}]}' ;;
esac
`
			bin := filepath.Join(temp, "glade")
			write(bin, script)
			if err := os.Chmod(bin, 0755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("OVERLAY_MODE", mode)
			cfg := &Config{dir: temp, DepSources: map[string]DepSource{"dependency": {Mode: mode, Paths: PathList{dep}, Dirs: []string{"src"}, Packages: []string{"sample-package"}}}}
			repo := Repo{Name: "example", Clone: PathList{source}, Pin: strings.TrimSpace(string(sha)), Overlay: Overlay{Deps: []string{"dependency"}}}
			result := runRepo(context.Background(), cfg, repo, Options{Glade: bin, StepTimeout: time.Minute, Parallelism: 1}, scratch)
			if !result.Complete || result.Observed.Counts.Tests != 1 || result.Diff.Infra != "no golden" {
				t.Fatalf("incomplete run: %+v", result)
			}
		})
	}
}

func TestRunRepoRejectsUnavailableProjectDependency(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "glade.yml"), []byte("project:\n  managedPackageDependencies: [\"sample:../missing\"]\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "glade.yml"}, {"-c", "user.name=test", "-c", "user.email=test@example.invalid", "commit", "-q", "-m", "initial"}} {
		cmd := exec.Command("git", append([]string{"-C", source}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git: %v %s", err, out)
		}
	}
	sha, err := exec.Command("git", "-C", source, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	repo := Repo{Name: "sample", Clone: PathList{source}, Pin: strings.TrimSpace(string(sha))}
	got := runRepo(context.Background(), &Config{dir: root}, repo, Options{Glade: "missing", StepTimeout: time.Minute}, filepath.Join(root, "scratch"))
	if got.Complete || !strings.HasPrefix(got.Diff.Infra, "project dependencies:") {
		t.Fatalf("unavailable dependency: %+v", got)
	}
}
