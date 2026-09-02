package scripts

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSalesforceStaleScratchCleanup(t *testing.T) {
	scriptPath := filepath.Join(toolsRoot(t), "scripts", "salesforce-stale-scratch-cleanup.sh")
	info, err := os.Stat(scriptPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatal("stale scratch cleanup script is not executable")
	}
	const fakeSF = `#!/usr/bin/env bash
set -euo pipefail
printf 'sf %s\n' "$*" >> "$FAKE_CALLS"
if [[ "${1:-}" == "data" && "${2:-}" == "delete" ]]; then
  record_id=""
  while [[ $# -gt 0 ]]; do
    if [[ "$1" == "--record-id" ]]; then record_id="$2"; break; fi
    shift
  done
  printf 'delete %s\n' "$record_id" >> "$FAKE_CALLS"
  : > "$FAKE_STATE"
  printf '{"status":0,"result":{"id":"%s","success":true,"errors":[]}}\n' "$record_id"
  exit 0
fi
if [[ "$*" == *"FROM ScratchOrgInfo"* ]]; then
  case "$FAKE_SCENARIO" in
    mixed)
      printf '%s\n' '{"status":0,"result":{"totalSize":2,"records":[{"Id":"2SR000000000001","ScratchOrg":"00D000000000001","OrgName":"glade-correctness-100-1","Status":"Active"},{"Id":"2SR000000000002","ScratchOrg":"00D000000000002","OrgName":"glade-correctness-200-2","Status":"Active"}]}}'
      ;;
    current-only)
      printf '{"status":0,"result":{"totalSize":2,"records":[{"Id":"2SR000000000003","ScratchOrg":"00D000000000003","OrgName":"%s","Status":"Active"},{"Id":"2SR000000000001","ScratchOrg":"00D000000000001","OrgName":"glade-correctness-100-1","Status":"Active"}]}}\n' "$FAKE_CURRENT_MARKER"
      ;;
    malformed-identity)
      printf '%s\n' '{"status":0,"result":{"totalSize":1,"records":[{"Id":"BAD","ScratchOrg":"00D000000000001","OrgName":"glade-correctness-100-1","Status":"Active"}]}}'
      ;;
    malformed-response)
      printf '%s\n' '{"status":false,"result":{"totalSize":0,"records":[]}}'
      ;;
    wrong-repository)
      printf '%s\n' '{"status":0,"result":{"totalSize":1,"records":[{"Id":"2SR000000000001","ScratchOrg":"00D000000000001","OrgName":"glade-correctness-100-1","Status":"Active"}]}}'
      ;;
    residue)
      printf '%s\n' '{"status":0,"result":{"totalSize":1,"records":[{"Id":"2SR000000000002","ScratchOrg":"00D000000000002","OrgName":"glade-correctness-200-2","Status":"Active"}]}}'
      ;;
    *) exit 91 ;;
  esac
  exit 0
fi
if [[ "$*" == *"FROM ActiveScratchOrg"* ]]; then
  if [[ -e "$FAKE_STATE" && "$FAKE_SCENARIO" != "residue" ]]; then
    printf '%s\n' '{"status":0,"result":{"totalSize":0,"records":[]}}'
  else
    printf '%s\n' '{"status":0,"result":{"totalSize":1,"records":[{"Id":"2AS000000000002","ScratchOrg":"00D000000000002"}]}}'
  fi
  exit 0
fi
exit 92
`
	const fakeGH = `#!/usr/bin/env bash
set -euo pipefail
printf 'gh %s\n' "$*" >> "$FAKE_CALLS"
url="${2:-}"
case "$FAKE_SCENARIO:$url" in
  mixed:*'/runs/100/attempts/1'|current-only:*'/runs/100/attempts/1')
    printf '%s\n' '{"id":100,"run_attempt":1,"repository":{"full_name":"glade-sh/glade-tools"},"path":".github/workflows/salesforce-correctness.yml","status":"in_progress","conclusion":null}'
    ;;
  mixed:*'/runs/200/attempts/2'|residue:*'/runs/200/attempts/2')
    printf '%s\n' '{"id":200,"run_attempt":2,"repository":{"full_name":"glade-sh/glade-tools"},"path":".github/workflows/salesforce-correctness.yml","status":"completed","conclusion":"failure"}'
    ;;
  wrong-repository:*'/runs/100/attempts/1')
    printf '%s\n' '{"id":100,"run_attempt":1,"repository":{"full_name":"wrong/repository"},"path":".github/workflows/salesforce-correctness.yml","status":"completed","conclusion":"failure"}'
    ;;
  *) exit 93 ;;
esac
`

	tests := []struct {
		name        string
		scenario    string
		wantSuccess bool
		wantDeletes int
		wantStatus  string
	}{
		{name: "mixed nonterminal then terminal selects only terminal", scenario: "mixed", wantSuccess: true, wantDeletes: 1, wantStatus: "pass"},
		{name: "current and nonterminal delete nothing", scenario: "current-only", wantSuccess: true, wantDeletes: 0, wantStatus: "pass"},
		{name: "malformed Salesforce identity fails before delete", scenario: "malformed-identity", wantSuccess: false, wantDeletes: 0, wantStatus: "fail"},
		{name: "malformed Salesforce response fails before delete", scenario: "malformed-response", wantSuccess: false, wantDeletes: 0, wantStatus: "fail"},
		{name: "wrong GitHub repository fails before delete", scenario: "wrong-repository", wantSuccess: false, wantDeletes: 0, wantStatus: "fail"},
		{name: "post-delete residue fails", scenario: "residue", wantSuccess: false, wantDeletes: 1, wantStatus: "fail"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			binDir := filepath.Join(dir, "bin")
			if err := os.Mkdir(binDir, 0o755); err != nil {
				t.Fatal(err)
			}
			for name, body := range map[string]string{"sf": fakeSF, "gh": fakeGH} {
				if err := os.WriteFile(filepath.Join(binDir, name), []byte(body), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			calls := filepath.Join(dir, "calls.log")
			state := filepath.Join(dir, "state")
			evidence := filepath.Join(dir, "evidence.json")
			currentMarker := "glade-correctness-300-1"
			cmd := exec.Command(scriptPath, "glade-dev-hub", currentMarker, "glade-sh/glade-tools", evidence)
			cmd.Env = append(os.Environ(),
				"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
				"FAKE_CALLS="+calls,
				"FAKE_STATE="+state,
				"FAKE_SCENARIO="+test.scenario,
				"FAKE_CURRENT_MARKER="+currentMarker,
			)
			output, err := cmd.CombinedOutput()
			if test.wantSuccess && err != nil {
				t.Fatalf("cleanup failed: %v\n%s", err, output)
			}
			if !test.wantSuccess && err == nil {
				t.Fatalf("cleanup unexpectedly passed:\n%s", output)
			}
			callsRaw, readErr := os.ReadFile(calls)
			if readErr != nil {
				t.Fatal(readErr)
			}
			callsText := string(callsRaw)
			if got := strings.Count("\n"+callsText, "\ndelete "); got != test.wantDeletes {
				t.Fatalf("delete count = %d, want %d\n%s", got, test.wantDeletes, callsText)
			}
			if test.scenario == "mixed" && !strings.Contains(callsText, "delete 2AS000000000002") {
				t.Fatalf("terminal candidate was not selected:\n%s", callsText)
			}
			evidenceRaw, readErr := os.ReadFile(evidence)
			if readErr != nil {
				t.Fatal(readErr)
			}
			var receipt map[string]any
			if err := json.Unmarshal(evidenceRaw, &receipt); err != nil {
				t.Fatalf("invalid evidence: %v", err)
			}
			if receipt["status"] != test.wantStatus {
				t.Fatalf("evidence status = %v, want %s: %s", receipt["status"], test.wantStatus, evidenceRaw)
			}
			for _, sensitive := range []string{"2SR", "00D", "2AS", "glade-correctness-"} {
				if strings.Contains(string(evidenceRaw), sensitive) {
					t.Fatalf("evidence contains sensitive identity %q: %s", sensitive, evidenceRaw)
				}
			}
		})
	}
}
