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

const (
	publishGladeSHA = "1111111111111111111111111111111111111111"
	publishToolsSHA = "2222222222222222222222222222222222222222"
)

func TestPublishSalesforceAuthorityCreatesExactCheckRun(t *testing.T) {
	fixture := makePublisherFixture(t, validAuthorityReceipt())
	out, err := runPublisherFixture(fixture, nil)
	if err != nil {
		t.Fatalf("publish authority: %v\n%s", err, out)
	}
	request, err := os.ReadFile(fixture.request)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(request, &payload); err != nil {
		t.Fatalf("decode check request: %v\n%s", err, request)
	}
	digest := sha256.Sum256(fixture.receiptBytes)
	wantExternal := fmt.Sprintf("salesforce-release-authority/v1;tools_sha=%s;run_id=123;run_attempt=2;receipt_sha256=%x", publishToolsSHA, digest)
	for key, want := range map[string]string{
		"name":        "Salesforce Correctness",
		"head_sha":    publishGladeSHA,
		"status":      "completed",
		"conclusion":  "success",
		"external_id": wantExternal,
	} {
		if payload[key] != want {
			t.Fatalf("check request %s = %v, want %s\n%s", key, payload[key], want, request)
		}
	}
	args, err := os.ReadFile(fixture.args)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(args), "--method POST repos/glade-sh/glade/check-runs --input -") {
		t.Fatalf("unexpected gh invocation: %s", args)
	}
}

func TestPublishSalesforceAuthorityRejectsInvalidReceipts(t *testing.T) {
	valid := validAuthorityReceipt()
	cases := map[string]struct {
		receipt map[string]any
		raw     string
		env     map[string]string
		digest  bool
	}{
		"wrong glade SHA": {receipt: changedAuthorityReceipt(valid, "gladeSHA", strings.Repeat("3", 40))},
		"wrong tools SHA": {receipt: changedAuthorityReceipt(valid, "toolsSHA", strings.Repeat("4", 40))},
		"non PASS":        {receipt: changedAuthorityReceipt(valid, "gateStatus", "FAIL")},
		"malformed":       {raw: `{"schemaVersion":`},
		"duplicate":       {raw: strings.Replace(string(mustMarshalAuthorityReceipt(t, valid)), `"gladeSHA":`, `"gladeSHA":"`+publishGladeSHA+`","gladeSHA":`, 1)},
		"digest mismatch": {receipt: valid, digest: true},
		"wrong run env":   {receipt: valid, env: map[string]string{"GITHUB_RUN_ID": "124"}},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			var receiptBytes []byte
			if testCase.raw != "" {
				receiptBytes = []byte(testCase.raw)
			} else {
				receiptBytes = mustMarshalAuthorityReceipt(t, testCase.receipt)
			}
			fixture := makePublisherFixture(t, receiptBytes)
			if testCase.digest {
				if err := os.WriteFile(fixture.sidecar, []byte(strings.Repeat("0", 64)+"  "+filepath.Base(fixture.receipt)+"\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if out, err := runPublisherFixture(fixture, testCase.env); err == nil {
				t.Fatalf("invalid authority published\n%s", out)
			}
			if _, err := os.Stat(fixture.request); !os.IsNotExist(err) {
				t.Fatalf("gh received a request for invalid authority: %v", err)
			}
		})
	}
}

type publisherFixture struct {
	root, receipt, sidecar, args, request string
	receiptBytes                          []byte
}

func validAuthorityReceipt() map[string]any {
	return map[string]any{
		"schemaVersion":        1,
		"gladeSHA":             publishGladeSHA,
		"toolsSHA":             publishToolsSHA,
		"workflowRunID":        123,
		"workflowRunAttempt":   2,
		"workflowRunURL":       "https://github.com/glade-sh/glade-tools/actions/runs/123/attempts/2",
		"gateStatus":           "PASS",
		"cleanupStatus":        "PASS",
		"evidenceArtifactName": "salesforce-correctness-evidence",
	}
}

func changedAuthorityReceipt(receipt map[string]any, key string, value any) map[string]any {
	changed := make(map[string]any, len(receipt))
	for k, v := range receipt {
		changed[k] = v
	}
	changed[key] = value
	return changed
}

func mustMarshalAuthorityReceipt(t *testing.T, receipt map[string]any) []byte {
	t.Helper()
	contents, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	return append(contents, '\n')
}

func makePublisherFixture(t *testing.T, receipt any) publisherFixture {
	t.Helper()
	root := t.TempDir()
	fixture := publisherFixture{
		root:    root,
		receipt: filepath.Join(root, "salesforce-release-authority.json"),
		sidecar: filepath.Join(root, "salesforce-release-authority.json.sha256"),
		args:    filepath.Join(root, "gh.args"),
		request: filepath.Join(root, "gh.request"),
	}
	if bytes, ok := receipt.([]byte); ok {
		fixture.receiptBytes = bytes
	} else {
		fixture.receiptBytes = mustMarshalAuthorityReceipt(t, receipt.(map[string]any))
	}
	if err := os.WriteFile(fixture.receipt, fixture.receiptBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(fixture.receiptBytes)
	if err := os.WriteFile(fixture.sidecar, []byte(fmt.Sprintf("%x  %s\n", digest, filepath.Base(fixture.receipt))), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	fakeGH := "#!/usr/bin/env bash\nset -euo pipefail\nprintf '%s\\n' \"$*\" > \"$FAKE_GH_ARGS\"\ncat > \"$FAKE_GH_REQUEST\"\n"
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(fakeGH), 0o755); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func runPublisherFixture(fixture publisherFixture, overrides map[string]string) (string, error) {
	script := filepath.Join("scripts", "publish-salesforce-authority.sh")
	cmd := exec.Command(script, fixture.receipt, fixture.sidecar, "glade-sh/glade", "Salesforce Correctness")
	cmd.Dir = filepath.Join("..")
	env := map[string]string{
		"PATH":               filepath.Join(fixture.root, "bin") + ":" + os.Getenv("PATH"),
		"FAKE_GH_ARGS":       fixture.args,
		"FAKE_GH_REQUEST":    fixture.request,
		"GH_TOKEN":           "test-token",
		"GITHUB_SHA":         publishToolsSHA,
		"GLADE_SHA":          publishGladeSHA,
		"GITHUB_RUN_ID":      "123",
		"GITHUB_RUN_ATTEMPT": "2",
		"GITHUB_REPOSITORY":  "glade-sh/glade-tools",
		"GITHUB_SERVER_URL":  "https://github.com",
	}
	for key, value := range overrides {
		env[key] = value
	}
	cmd.Env = os.Environ()
	for key, value := range env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}
