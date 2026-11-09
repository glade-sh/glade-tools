package toolcli

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const fakeGlade = `#!/bin/sh
case "$1" in
check)
  out=""
  while [ $# -gt 0 ]; do [ "$1" = "--output" ] && out="$2"; shift; done
  printf '{"project":{"sourceApiVersion":"67.0"},"summary":{"types":1},"diagnostics":[]}' > "$out" ;;
test)
  printf '{"summary":{"total":2,"passed":%s},"tests":[{"name":"T.a","status":"pass"},{"name":"T.b","status":"%s"}]}' "$PASSED" "$B_STATUS"
  exit 1 ;;
esac
`

func TestCorpusRunVerdicts(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	root := t.TempDir()
	repo := filepath.Join(root, "config", "clones", "sample")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "x"}} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	head, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(root, "config")
	tiers := `{"schemaVersion":1,"repos":[{"name":"sample","tier":"A","clone":"clones/sample","pin":"` + strings.TrimSpace(string(head)) + `"}]}`
	if err := os.WriteFile(filepath.Join(config, "tiers.json"), []byte(tiers), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "glade")
	if err := os.WriteFile(bin, []byte(fakeGlade), 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(extra ...string) (string, int) {
		var out, errOut bytes.Buffer
		code := Run(context.Background(), append([]string{"corpus", "run", "--tier", "A", "--glade", bin, "--config", config}, extra...), &out, &errOut)
		return out.String(), code
	}

	t.Setenv("PASSED", "1")
	t.Setenv("B_STATUS", "fail")
	out, code := run("--json", filepath.Join(root, "run.json"))
	if code != 2 || !strings.HasPrefix(out, "CORPUS-INFRA 1 repos\nsample INFRA no golden") {
		t.Fatalf("no golden: %d %q", code, out)
	}
	var gout, gerr bytes.Buffer
	if code := Run(context.Background(), []string{"corpus", "golden", "--config", config, "--repo", "sample", "--from-run", filepath.Join(root, "run.json")}, &gout, &gerr); code != 0 {
		t.Fatalf("golden: %d %s", code, gerr.String())
	}
	// The golden is unclassified but present: the same run is clean.
	if out, code = run(); code != 0 || !strings.HasPrefix(out, "CORPUS-CLEAN\nsample OK") {
		t.Fatalf("same run: %d %q", code, out)
	}
	t.Setenv("B_STATUS", "pass")
	t.Setenv("PASSED", "2")
	if out, code = run(); code != 0 || !strings.HasPrefix(out, "CORPUS-CLEAN\nsample GONE 1") {
		t.Fatalf("fixed: %d %q", code, out)
	}
}

func TestCorpusRunUsageIsExit2(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"corpus", "run"}, &out, &errOut); code != 2 {
		t.Fatalf("code = %d", code)
	}
}
