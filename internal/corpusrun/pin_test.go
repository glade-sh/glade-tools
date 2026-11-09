package corpusrun

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPinnedCloneRejectsDirtyTree(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "source")
	if err := os.Mkdir(repo, 0755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"-c", "user.name=test", "-c", "user.email=test@example.invalid", "commit", "-q", "--allow-empty", "-m", "initial"}} {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git: %v %s", err, out)
		}
	}
	head, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "untracked.cls"), []byte("example"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{dir: root}
	r := Repo{Name: "sample", Clone: PathList{repo}, Pin: strings.TrimSpace(string(head))}
	result := runRepo(context.Background(), cfg, r, Options{Glade: "missing"}, filepath.Join(root, "scratch"))
	if !strings.Contains(result.Diff.Infra, "dirty") {
		t.Fatalf("dirty clone not rejected: %+v", result.Diff)
	}
}
