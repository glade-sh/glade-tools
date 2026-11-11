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

func TestRunRepoIsolatesAmbientWorkspace(t *testing.T) {
	for _, parent := range []string{"direct", "nested"} {
		t.Run(parent, func(t *testing.T) {
			base := t.TempDir()
			scratch := filepath.Join(base, "scratch")
			if parent == "nested" {
				scratch = filepath.Join(base, "outer", "scratch")
			}
			ambient := filepath.Join(filepath.Dir(scratch), "ambient")
			for _, dir := range []string{scratch, ambient} {
				if err := os.MkdirAll(dir, 0755); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(ambient, "sfdx-project.json"), []byte(`{"packageDirectories":[]}`), 0644); err != nil {
				t.Fatal(err)
			}
			source := filepath.Join(base, "source")
			if err := os.Mkdir(source, 0755); err != nil {
				t.Fatal(err)
			}
			for _, args := range [][]string{{"init", "-q"}, {"-c", "user.name=test", "-c", "user.email=test@example.invalid", "commit", "-q", "--allow-empty", "-m", "synthetic"}} {
				if out, err := exec.Command("git", append([]string{"-C", source}, args...)...).CombinedOutput(); err != nil {
					t.Fatalf("git: %v: %s", err, out)
				}
			}
			sha, err := exec.Command("git", "-C", source, "rev-parse", "HEAD").Output()
			if err != nil {
				t.Fatal(err)
			}
			fake := filepath.Join(base, "glade")
			script := `#!/bin/sh
case "$1" in
check)
 project=""; output=""
 while [ "$#" -gt 0 ]; do case "$1" in --project) project="$2";; --output) output="$2";; esac; shift; done
 grandparent=$(dirname "$(dirname "$project")")
 if [ -f "$grandparent/ambient/sfdx-project.json" ]; then
  printf '{"project":{"sourceApiVersion":"67.0"},"summary":{"types":1},"diagnostics":[{"code":"SYNTHETIC_AMBIENT","severity":"error","message":"ambient source visible"}]}' > "$output"
 else
  printf '{"project":{"sourceApiVersion":"67.0"},"summary":{"types":1},"diagnostics":[]}' > "$output"
 fi ;;
test) printf '{"summary":{"total":1,"passed":1},"tests":[{"name":"Synthetic.test","status":"pass"}]}' ;;
esac
`
			if err := os.WriteFile(fake, []byte(script), 0755); err != nil {
				t.Fatal(err)
			}
			cfg := &Config{dir: base}
			repo := Repo{Name: "synthetic", Clone: PathList{source}, Pin: strings.TrimSpace(string(sha))}
			result := runRepo(context.Background(), cfg, repo, Options{Glade: fake, StepTimeout: time.Minute, Parallelism: 1}, scratch)
			if !result.Complete || len(result.Observed.Diagnostics) != 0 {
				t.Fatalf("ambient project must not affect isolated run: complete=%v diagnostics=%+v infra=%s", result.Complete, result.Observed.Diagnostics, result.Diff.Infra)
			}
		})
	}
}
