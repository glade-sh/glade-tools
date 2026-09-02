package scripts

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSalesforceProductTestsBuildOnceAndShardSerially(t *testing.T) {
	script := filepath.Join(toolsRoot(t), "scripts", "salesforce-product-tests.sh")
	info, err := os.Stat(script)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatal("Salesforce product-test wrapper is not executable")
	}

	for _, test := range []struct {
		name     string
		failMode string
		wantPass bool
	}{
		{name: "validated exact union", wantPass: true},
		{name: "failed shard leaves no combined proof", failMode: "shard", wantPass: false},
		{name: "failed test2json leaves no combined proof", failMode: "test2json", wantPass: false},
		{name: "duplicate top-level pass leaves no combined proof", failMode: "duplicate", wantPass: false},
		{name: "malformed plan leaves no combined proof", failMode: "plan", wantPass: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			binDir := filepath.Join(dir, "bin")
			gladeRoot := filepath.Join(dir, "glade")
			outDir := filepath.Join(dir, "out")
			if err := os.MkdirAll(filepath.Join(gladeRoot, "scripts", "internal", "cishard"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(gladeRoot, "scripts", "internal", "cishard", "main.go"), []byte("package main\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(binDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(outDir, 0o755); err != nil {
				t.Fatal(err)
			}

			calls := filepath.Join(dir, "calls.log")
			fakeTest := filepath.Join(dir, "fake-apextest")
			if err := os.WriteFile(fakeTest, []byte(fakeApexTestBinary), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(binDir, "go"), []byte(fakeSalesforceProductGo), 0o755); err != nil {
				t.Fatal(err)
			}

			cmd := exec.Command("bash", script, gladeRoot, outDir)
			cmd.Env = append(os.Environ(),
				"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
				"LC_ALL=C",
				"GLADE_LWC_COMPILE=1",
				"GLADE_ROOT="+gladeRoot,
				"FAKE_GO_CALLS="+calls,
				"FAKE_APEXTEST_BINARY="+fakeTest,
				"FAKE_FAIL_MODE="+test.failMode,
			)
			output, err := cmd.CombinedOutput()
			if test.wantPass && err != nil {
				t.Fatalf("wrapper failed: %v\n%s", err, output)
			}
			if !test.wantPass && err == nil {
				t.Fatalf("wrapper unexpectedly passed:\n%s", output)
			}

			combined := filepath.Join(outDir, "product-tests.jsonl")
			binary := filepath.Join(outDir, "product-test-evidence", "apextest.test")
			if _, err := os.Stat(binary); !os.IsNotExist(err) {
				t.Fatalf("compiled test binary remains: %v", err)
			}
			if !test.wantPass {
				if _, err := os.Stat(combined); !os.IsNotExist(err) {
					t.Fatalf("failed wrapper published combined events: %v", err)
				}
				return
			}

			callsRaw, err := os.ReadFile(calls)
			if err != nil {
				t.Fatal(err)
			}
			callsText := string(callsRaw)
			if got := strings.Count(callsText, "go-test-compile\n"); got != 1 {
				t.Fatalf("Apex test binary builds = %d, want 1:\n%s", got, callsText)
			}
			if got := strings.Count(callsText, "apex-shard "); got != 16 {
				t.Fatalf("Apex shard executions = %d, want 16:\n%s", got, callsText)
			}
			if !strings.Contains(callsText, "non-apextest -json -count=1 -p 1 -timeout=30m") || strings.Contains(callsText, "non-apextest github.com/glade-sh/glade/internal/apextest") {
				t.Fatalf("non-Apex packages were not run serially and separately:\n%s", callsText)
			}
			if !strings.Contains(callsText, "cishard --package github.com/glade-sh/glade/internal/apextest --shards 16") {
				t.Fatalf("exact Glade shard planner was not used:\n%s", callsText)
			}

			validationRaw, err := os.ReadFile(filepath.Join(outDir, "product-test-evidence", "validation.json"))
			if err != nil {
				t.Fatal(err)
			}
			var validation struct {
				SchemaVersion int    `json:"schemaVersion"`
				Status        string `json:"status"`
				ShardCount    int    `json:"shardCount"`
				Binary        struct {
					SHA256  string `json:"sha256"`
					Removed bool   `json:"removed"`
				} `json:"apexTestBinary"`
				Discovery struct {
					Count  int    `json:"count"`
					SHA256 string `json:"sha256"`
				} `json:"apexDiscovery"`
				Shards []struct {
					Index        int    `json:"index"`
					TestCount    int    `json:"testCount"`
					EventsSHA256 string `json:"eventsSHA256"`
				} `json:"shards"`
				Union struct {
					Valid bool `json:"valid"`
					Count int  `json:"count"`
				} `json:"union"`
				TestEvents struct {
					Path   string `json:"path"`
					SHA256 string `json:"sha256"`
				} `json:"testEvents"`
				Artifacts []struct {
					Path   string `json:"path"`
					SHA256 string `json:"sha256"`
				} `json:"artifacts"`
			}
			if err := json.Unmarshal(validationRaw, &validation); err != nil {
				t.Fatal(err)
			}
			if validation.SchemaVersion != 1 || validation.Status != "pass" || validation.ShardCount != 16 || !validation.Binary.Removed || len(validation.Binary.SHA256) != 64 || validation.Discovery.Count != 16 || len(validation.Discovery.SHA256) != 64 || len(validation.Shards) != 16 || !validation.Union.Valid || validation.Union.Count != 16 || validation.TestEvents.Path != "product-tests.jsonl" || len(validation.TestEvents.SHA256) != 64 {
				t.Fatalf("invalid validation summary: %s", validationRaw)
			}
			for index, shard := range validation.Shards {
				if shard.Index != index || shard.TestCount != 1 || len(shard.EventsSHA256) != 64 {
					t.Fatalf("invalid shard %d: %#v", index, shard)
				}
			}
			for _, artifact := range validation.Artifacts {
				raw, err := os.ReadFile(filepath.Join(outDir, artifact.Path))
				if err != nil {
					t.Fatal(err)
				}
				if got := fmt.Sprintf("%x", sha256.Sum256(raw)); got != artifact.SHA256 {
					t.Fatalf("artifact %s SHA-256 = %s, want %s", artifact.Path, got, artifact.SHA256)
				}
			}
		})
	}
}

const fakeApexTestBinary = `#!/usr/bin/env bash
set -euo pipefail
if [[ "$*" == *"-test.list"* ]]; then
  for index in {0..15}; do printf 'Test%02d\n' "$index"; done
  exit 0
fi
regex=""
for arg in "$@"; do case "$arg" in -test.run=*) regex="${arg#-test.run=}" ;; esac; done
name="$(printf '%s' "$regex" | sed -E 's/^\^\(\?://; s/\)\$$//')"
printf 'apex-shard %s\n' "$name" >> "$FAKE_GO_CALLS"
printf '{"Action":"run","Package":"github.com/glade-sh/glade/internal/apextest","Test":"%s"}\n' "$name"
if [[ "${FAKE_FAIL_MODE:-}" == shard && "$name" == Test07 ]]; then
  printf '{"Action":"fail","Package":"github.com/glade-sh/glade/internal/apextest","Test":"%s"}\n' "$name"
  exit 1
fi
printf '{"Action":"pass","Package":"github.com/glade-sh/glade/internal/apextest","Test":"%s"}\n' "$name"
if [[ "${FAKE_FAIL_MODE:-}" == duplicate && "$name" == Test07 ]]; then
  printf '{"Action":"pass","Package":"github.com/glade-sh/glade/internal/apextest","Test":"%s"}\n' "$name"
fi
printf '{"Action":"pass","Package":"github.com/glade-sh/glade/internal/apextest"}\n'
`

const fakeSalesforceProductGo = `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == -C ]]; then shift 2; fi
case "${1:-}" in
  list)
    printf '%s\n' github.com/glade-sh/glade/cmd/glade github.com/glade-sh/glade/internal/apextest github.com/glade-sh/glade/internal/sema
    ;;
  test)
    shift
    if [[ " $* " == *" -c "* ]]; then
      output=""
      while [[ $# -gt 0 ]]; do if [[ "$1" == -o ]]; then output="$2"; break; fi; shift; done
      cp "$FAKE_APEXTEST_BINARY" "$output"
      chmod 755 "$output"
      printf 'go-test-compile\n' >> "$FAKE_GO_CALLS"
      exit 0
    fi
    printf 'non-apextest %s\n' "$*" >> "$FAKE_GO_CALLS"
    for package in "$@"; do
      [[ "$package" == github.com/* ]] || continue
      printf '{"Action":"pass","Package":"%s","Test":"TestProduct"}\n' "$package"
      printf '{"Action":"pass","Package":"%s"}\n' "$package"
    done
    ;;
  run)
    shift
    [[ "${1:-}" == ./scripts/internal/cishard ]]
    shift
    printf 'cishard %s\n' "$*" >> "$FAKE_GO_CALLS"
    tests=""
    while [[ $# -gt 0 ]]; do if [[ "$1" == --tests ]]; then tests="$2"; break; fi; shift; done
    if [[ "${FAKE_FAIL_MODE:-}" == plan ]]; then printf '{"version":1,"package":"wrong","historyUsed":false,"shards":[]}\n'; exit 0; fi
    python3 - "$tests" <<'PY'
import json, sys
names = open(sys.argv[1], encoding="utf-8").read().splitlines()
print(json.dumps({"version": 1, "package": "github.com/glade-sh/glade/internal/apextest", "historyUsed": False, "shards": [{"index": i, "tests": [name], "estimatedDurationMillis": 0, "regex": "^(?:" + name + ")$"} for i, name in enumerate(names)]}))
PY
    ;;
  tool)
    [[ "${2:-}" == test2json ]]
    cat
    if [[ "${FAKE_FAIL_MODE:-}" == test2json ]]; then exit 91; fi
    ;;
  *) exit 90 ;;
esac
`
