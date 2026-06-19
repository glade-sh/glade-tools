package orgpackage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecSFRunnerUsesBodyFlagForStdin(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	bin := filepath.Join(dir, "sf")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + shellQuote(argsPath) + "\ncat >/dev/null\nprintf '{}'\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	runner := ExecSFRunner{Bin: bin}
	if _, err := runner.Request(context.Background(), sfCall{
		TargetOrg: "packaging",
		Method:    "POST",
		URL:       "/services/data/v65.0/tooling/executeAnonymous",
		Body:      "payload",
	}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	args := "\n" + string(data)
	for _, want := range []string{"\napi\n", "\nrequest\n", "\nrest\n", "\n--body\n", "\n-\n"} {
		if !strings.Contains(args, want) {
			t.Fatalf("args = %q, missing %q", string(data), want)
		}
	}
}

func shellQuote(path string) string {
	return "'" + strings.ReplaceAll(path, "'", "'\\''") + "'"
}
