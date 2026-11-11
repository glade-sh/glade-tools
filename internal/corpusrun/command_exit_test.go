package corpusrun

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunGladeRejectsInfrastructureExit(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "glade")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf '{}\\n'\nexit 2\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := runGlade(context.Background(), Options{Glade: bin, StepTimeout: time.Second}, t.TempDir(), "test"); err == nil {
		t.Fatal("infrastructure exit accepted as test result")
	}
}

func TestRunGladePreservesFailureWithoutOutput(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "glade")
	script := "#!/bin/sh\nprintf 'synthetic failure\\n' >&2\nexit 1\n"
	if err := os.WriteFile(bin, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	_, err := runGlade(context.Background(), Options{Glade: bin, StepTimeout: time.Second}, t.TempDir(), "test")
	if err == nil || !strings.Contains(err.Error(), "synthetic failure") {
		t.Fatalf("missing failure detail: %v", err)
	}
}

func TestRunGladeAllowsFailureWithOutputFile(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "glade")
	output := filepath.Join(root, "out.json")
	script := "#!/bin/sh\nprintf '{}\\n' > \"$2\"\nexit 1\n"
	if err := os.WriteFile(bin, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := runGlade(context.Background(), Options{Glade: bin, StepTimeout: time.Second}, root, "--output", output); err != nil {
		t.Fatalf("expected output file: %v", err)
	}
}
