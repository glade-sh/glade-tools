package corpusrun

import (
	"context"
	"os"
	"path/filepath"
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
