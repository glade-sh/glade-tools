package corpusrun

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
