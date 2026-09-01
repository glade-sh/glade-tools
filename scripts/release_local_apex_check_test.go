package scripts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

var releaseLocalApexFixtures = []struct {
	name  string
	total int
}{
	{"enterprise-composed", 2},
	{"org-like-runner", 2},
	{"files-email", 2},
	{"flow", 1},
	{"resources-labels", 2},
}

func TestReleaseLocalApexCheck(t *testing.T) {
	toolsRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	fake := filepath.Join(t.TempDir(), "glade")
	logPath := filepath.Join(t.TempDir(), "calls.log")
	fakeScript := `#!/bin/sh
printf '%s\n' "$*" >> "$FAKE_GLADE_LOG"
command=$1
shift
while [ "$#" -gt 0 ]; do
	if [ "$1" = "--project" ]; then project=$2; shift 2; else shift; fi
done
fixture=$(basename "$project")
[ "${FAKE_FAIL_COMMAND:-}" != "$command:$fixture" ] || exit 17
if [ "$command" = check ]; then
	printf '{"status":"passed","exitCode":0,"summary":{"types":1,"diagnostics":0}}\n'
	exit 0
fi
case "$fixture" in
	enterprise-composed|org-like-runner|files-email|resources-labels) total=2 ;;
	flow) total=1 ;;
	*) exit 18 ;;
esac
[ "${FAKE_WRONG_FIXTURE:-}" != "$fixture" ] || total=$((total + 1))
printf '{"status":"passed","exitCode":0,"summary":{"total":%s,"passed":%s,"failed":0,"errors":0,"compileErrors":0,"runtimeErrors":0,"unsupported":0}}\n' "$total" "$total"
`
	if err := os.WriteFile(fake, []byte(fakeScript), 0o700); err != nil {
		t.Fatal(err)
	}

	t.Run("writes a bound nine-test receipt", func(t *testing.T) {
		temp := t.TempDir()
		runReleaseLocalApexCheck(t, fake, toolsRoot, temp, logPath)

		calls, err := os.ReadFile(logPath)
		if err != nil {
			t.Fatal(err)
		}
		var wantCalls []string
		for _, fixture := range releaseLocalApexFixtures {
			project := filepath.Join(toolsRoot, "testdata", "local-tests", fixture.name)
			wantCalls = append(wantCalls,
				"check --project "+project+" --json --no-progress",
				"test --project "+project+" --json --no-progress",
			)
		}
		if got := strings.Split(strings.TrimSpace(string(calls)), "\n"); !reflect.DeepEqual(got, wantCalls) {
			t.Fatalf("calls = %q, want %q", got, wantCalls)
		}

		data, err := os.ReadFile(filepath.Join(temp, "release-local-apex-summary.json"))
		if err != nil {
			t.Fatal(err)
		}
		var receipt struct {
			Glade struct {
				BinarySHA256 string `json:"binarySha256"`
				Commit       string `json:"commit"`
			} `json:"glade"`
			Tools struct {
				Commit string `json:"commit"`
			} `json:"tools"`
			Fixtures []struct {
				Name   string `json:"name"`
				Passed int    `json:"passed"`
				Total  int    `json:"total"`
			} `json:"fixtures"`
			Summary struct {
				Passed int `json:"passed"`
				Total  int `json:"total"`
			} `json:"summary"`
		}
		if err := json.Unmarshal(data, &receipt); err != nil {
			t.Fatal(err)
		}
		binary, err := os.ReadFile(fake)
		if err != nil {
			t.Fatal(err)
		}
		wantBinarySHA := sha256.Sum256(binary)
		commit := gitOutput(t, toolsRoot, "rev-parse", "HEAD")
		if receipt.Glade.BinarySHA256 != hex.EncodeToString(wantBinarySHA[:]) || receipt.Glade.Commit != commit || receipt.Tools.Commit != commit {
			t.Fatalf("receipt bindings = %#v", receipt)
		}
		if receipt.Summary.Passed != 9 || receipt.Summary.Total != 9 || len(receipt.Fixtures) != len(releaseLocalApexFixtures) {
			t.Fatalf("receipt counts = %#v", receipt)
		}
		for i, fixture := range releaseLocalApexFixtures {
			if got := receipt.Fixtures[i]; got.Name != fixture.name || got.Passed != fixture.total || got.Total != fixture.total {
				t.Fatalf("fixture %d = %#v, want %#v", i, got, fixture)
			}
		}
	})

	for _, test := range []struct {
		name string
		env  string
	}{
		{"rejects a wrong report count", "FAKE_WRONG_FIXTURE=flow"},
		{"rejects a nonzero command", "FAKE_FAIL_COMMAND=test:flow"},
	} {
		t.Run(test.name, func(t *testing.T) {
			temp := t.TempDir()
			cmd := exec.Command("bash", "release-local-apex-check.sh", fake, toolsRoot)
			cmd.Env = append(os.Environ(), "TMPDIR="+temp, "FAKE_GLADE_LOG="+logPath, test.env)
			if out, err := cmd.CombinedOutput(); err == nil {
				t.Fatalf("gate unexpectedly passed:\n%s", out)
			}
			if _, err := os.Stat(filepath.Join(temp, "release-local-apex-summary.json")); !os.IsNotExist(err) {
				t.Fatalf("failed gate left a receipt: %v", err)
			}
		})
	}
}

func runReleaseLocalApexCheck(t *testing.T, binary, sourceRoot, temp, logPath string) {
	t.Helper()
	cmd := exec.Command("bash", "release-local-apex-check.sh", binary, sourceRoot)
	cmd.Env = append(os.Environ(), "TMPDIR="+temp, "FAKE_GLADE_LOG="+logPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("release local Apex check: %v\n%s", err, out)
	}
}

func gitOutput(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}
