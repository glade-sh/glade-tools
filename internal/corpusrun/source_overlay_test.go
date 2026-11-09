package corpusrun

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSourceOverlayVerifiesAndConfinesArchive(t *testing.T) {
	for _, name := range []string{"settings.json", "../outside"} {
		t.Run(name, func(t *testing.T) {
			var b bytes.Buffer
			w := tar.NewWriter(&b)
			content := []byte("synthetic")
			if err := w.WriteHeader(&tar.Header{Name: name, Mode: 0644, Size: int64(len(content))}); err != nil {
				t.Fatal(err)
			}
			if _, err := w.Write(content); err != nil {
				t.Fatal(err)
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			archive := filepath.Join(dir, "overlay.tar")
			if err := os.WriteFile(archive, b.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			hash := fmt.Sprintf("%x", sha256.Sum256(b.Bytes()))
			root := filepath.Join(dir, "root")
			if err := os.Mkdir(root, 0755); err != nil {
				t.Fatal(err)
			}
			err := applySourceArchive(root, archive, hash, nil)
			if strings.HasPrefix(name, "..") {
				if err == nil {
					t.Fatal("archive path escaped root")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got, err := os.ReadFile(filepath.Join(root, name)); err != nil || string(got) != "synthetic" {
				t.Fatalf("read = %q, %v", got, err)
			}
			if err := applySourceArchive(root, archive, strings.Repeat("0", 64), nil); err == nil {
				t.Fatal("wrong archive hash accepted")
			}
		})
	}
}

func TestVerifyComponentPinsDetectsChanges(t *testing.T) {
	root := t.TempDir()
	component := filepath.Join(root, "component")
	if err := os.Mkdir(component, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(component, "Example.cls")
	if err := os.WriteFile(path, []byte("synthetic"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "Example.cls"}, {"-c", "user.name=test", "-c", "user.email=test@example.invalid", "commit", "-q", "-m", "initial"}} {
		cmd := exec.Command("git", append([]string{"-C", component}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git: %v %s", err, out)
		}
	}
	out, err := exec.Command("git", "-C", component, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	pins := map[string]string{"component": strings.TrimSpace(string(out))}
	if err := verifyComponentPins(root, pins); err != nil {
		t.Fatalf("clean: %v", err)
	}
	if err := os.WriteFile(path, []byte("changed"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := verifyComponentPins(root, pins); err == nil {
		t.Fatal("dirty component accepted")
	}
	pins["../outside"] = "bad"
	if err := verifyComponentPins(root, pins); err == nil {
		t.Fatal("escaping component accepted")
	}
}

func TestSourceOverlayRejectsSymlinkAndComponentOverlap(t *testing.T) {
	for _, kind := range []string{"symlink", "component"} {
		t.Run(kind, func(t *testing.T) {
			var b bytes.Buffer
			w := tar.NewWriter(&b)
			content := []byte("synthetic")
			if err := w.WriteHeader(&tar.Header{Name: "nested/file.txt", Mode: 0644, Size: int64(len(content))}); err != nil {
				t.Fatal(err)
			}
			if _, err := w.Write(content); err != nil {
				t.Fatal(err)
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			root := filepath.Join(dir, "root")
			if err := os.Mkdir(root, 0755); err != nil {
				t.Fatal(err)
			}
			archive := filepath.Join(dir, "overlay.tar")
			if err := os.WriteFile(archive, b.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			components := map[string]string{}
			if kind == "symlink" {
				if err := os.Symlink(t.TempDir(), filepath.Join(root, "nested")); err != nil {
					t.Fatal(err)
				}
			} else {
				components["nested"] = "sample"
			}
			if err := applySourceArchive(root, archive, fmt.Sprintf("%x", sha256.Sum256(b.Bytes())), components); err == nil {
				t.Fatal("unsafe archive accepted")
			}
		})
	}
}

func TestRunRepoAppliesPinnedSourceOverlay(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	if err := os.Mkdir(source, 0755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"-c", "user.name=test", "-c", "user.email=test@example.invalid", "commit", "-q", "--allow-empty", "-m", "initial"}} {
		cmd := exec.Command("git", append([]string{"-C", source}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git: %v %s", err, out)
		}
	}
	out, err := exec.Command("git", "-C", source, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	tw := tar.NewWriter(&b)
	content := []byte("overlay-present")
	if err := tw.WriteHeader(&tar.Header{Name: "marker.txt", Mode: 0644, Size: int64(len(content))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(dir, "overlay.tar")
	if err := os.WriteFile(archive, b.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "glade")
	script := `#!/bin/sh
[ "$(cat marker.txt)" = "overlay-present" ] || exit 2
case "$1" in
check) while [ "$#" -gt 0 ]; do if [ "$1" = "--output" ]; then printf '{"summary":{"types":1},"diagnostics":[]}' > "$2"; exit 0; fi; shift; done;;
test) printf '{"summary":{"total":1,"passed":1},"tests":[{"name":"Synthetic.test","status":"pass"}]}' ;;
esac
`
	if err := os.WriteFile(bin, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	repo := Repo{Name: "sample", Clone: PathList{source}, Pin: strings.TrimSpace(string(out)), SourceArchive: archive, Overlay: Overlay{SourceArchiveSHA256: fmt.Sprintf("%x", sha256.Sum256(b.Bytes()))}}
	got := runRepo(context.Background(), &Config{dir: dir}, repo, Options{Glade: bin, StepTimeout: time.Minute}, filepath.Join(dir, "scratch"))
	if !got.Complete || got.Observed.Counts.Tests != 1 || got.Diff.Infra != "no golden" {
		t.Fatalf("result: %+v", got)
	}
}

func TestSourceOverlayRejectsWarmCache(t *testing.T) {
	var b bytes.Buffer
	w := tar.NewWriter(&b)
	content := []byte("cached")
	if err := w.WriteHeader(&tar.Header{Name: ".glade/cache/state.gob", Mode: 0644, Size: int64(len(content))}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "overlay.tar")
	if err := os.WriteFile(path, b.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	if err := applySourceArchive(root, path, fmt.Sprintf("%x", sha256.Sum256(b.Bytes())), nil); err == nil {
		t.Fatal("warm cache archive accepted")
	}
}
