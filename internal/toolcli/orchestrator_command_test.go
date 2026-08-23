package toolcli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/corpusassurance"
)

func TestCorpusAssuranceOrchestratorPlansInitializesAndRejectsArbitraryCommands(t *testing.T) {
	root := t.TempDir()
	scopePath := filepath.Join(root, "scope.json")
	scope := corpusassurance.SurfaceOracleScope{
		SchemaVersion: 1, Kind: "all-runtime", SourceProfileSHA256: strings.Repeat("1", 64), LedgerSHA256: strings.Repeat("2", 64), PolicySHA256: strings.Repeat("3", 64), Total: 2,
		ByDisposition: map[string]int{"deterministic-mock-required": 1, "local-runtime-required": 1},
		Rows:          []corpusassurance.SurfaceOracleScopeRow{{SurfaceID: "apex:System.One", Disposition: "deterministic-mock-required"}, {SurfaceID: "apex:System.Two", Disposition: "local-runtime-required"}},
	}
	writeOrchestratorCLIJSON(t, scopePath, scope)
	definitionPath := filepath.Join(root, "campaign.json")
	writeOrchestratorCLIJSON(t, definitionPath, corpusassurance.OrchestratorCampaignDefinition{
		Candidate: corpusassurance.OrchestratorArtifact{Commit: strings.Repeat("a", 40), SHA256: strings.Repeat("b", 64)},
		Tools:     corpusassurance.OrchestratorArtifact{Commit: strings.Repeat("c", 40), SHA256: strings.Repeat("d", 64)},
		ScopePath: scopePath, ScopeSHA256: orchestratorCLIFileSHA256(t, scopePath),
		ControlledInputSHA256: map[string]string{"oracle-plan": strings.Repeat("e", 64)},
		Shards:                [2][]string{{"apex:System.One"}, {"apex:System.Two"}},
	})
	planPath := filepath.Join(root, "plan.json")
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"corpus", "assurance", "orchestrator", "plan", "--campaign", definitionPath, "--output", planPath}, &stdout, &stderr); code != 0 {
		t.Fatalf("plan code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var plan corpusassurance.OrchestratorCampaignPlan
	readOrchestratorCLIJSON(t, planPath, &plan)
	if len(plan.Jobs) != 2 || plan.Jobs[0].Kind != corpusassurance.OrchestratorJobSurfaceRuntimeShard || plan.Jobs[1].Kind != corpusassurance.OrchestratorJobSurfaceRuntimeShard {
		t.Fatalf("plan jobs = %#v", plan.Jobs)
	}
	database := filepath.Join(root, "orchestrator.db")
	for _, args := range [][]string{
		{"corpus", "assurance", "orchestrator", "init", "--db", database, "--plan", planPath},
		{"corpus", "assurance", "orchestrator", "enqueue", "--db", database, "--plan", planPath},
		{"corpus", "assurance", "orchestrator", "status", "--db", database, "--campaign", plan.CampaignID},
	} {
		stdout.Reset()
		stderr.Reset()
		if code := Run(context.Background(), args, &stdout, &stderr); code != 0 {
			t.Fatalf("%q code=%d stdout=%q stderr=%q", args, code, stdout.String(), stderr.String())
		}
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run(context.Background(), []string{"corpus", "assurance", "orchestrator", "lease", "--db", database, "--campaign", plan.CampaignID, "--worker", "worker-a", "--seconds", "60", "--argv", "rm -rf"}, &stdout, &stderr); code == 0 || !strings.Contains(stderr.String(), "flag provided but not defined") {
		t.Fatalf("arbitrary argv accepted: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run(context.Background(), []string{"corpus", "assurance", "orchestrator", "status", "--db", database, "--campaign", plan.CampaignID, "trailing"}, &stdout, &stderr); code == 0 || !strings.Contains(stderr.String(), "unexpected argument") {
		t.Fatalf("trailing argument accepted: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestCorpusAssuranceOrchestratorJSONRejectsDuplicateFields(t *testing.T) {
	root := t.TempDir()
	definitionPath := filepath.Join(root, "campaign.json")
	if err := os.WriteFile(definitionPath, []byte(`{"candidate":{},"candidate":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"corpus", "assurance", "orchestrator", "plan", "--campaign", definitionPath, "--output", filepath.Join(root, "plan.json")}, &stdout, &stderr); code == 0 || !strings.Contains(stderr.String(), "duplicate") {
		t.Fatalf("duplicate JSON field accepted: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestCorpusAssuranceOrchestratorCleanupTakeoverRejectsUnknownTrailingAndExtraArgs(t *testing.T) {
	root := t.TempDir()
	request := filepath.Join(root, "cleanup.json")
	for _, content := range []string{
		`{"claim":{},"unknown":true}`,
		`{"claim":{}} {"trailing":true}`,
	} {
		if err := os.WriteFile(request, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		if code := Run(context.Background(), []string{"corpus", "assurance", "orchestrator", "cleanup-takeover", "--db", filepath.Join(root, "orchestrator.db"), "--request", request}, &stdout, &stderr); code == 0 || (!strings.Contains(stderr.String(), "unknown JSON key") && !strings.Contains(stderr.String(), "multiple JSON values") && !strings.Contains(stderr.String(), "one value")) {
			t.Fatalf("invalid cleanup request accepted: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
		}
	}
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"corpus", "assurance", "orchestrator", "cleanup-takeover", "--db", filepath.Join(root, "orchestrator.db"), "--request", request, "trailing"}, &stdout, &stderr); code == 0 || !strings.Contains(stderr.String(), "unexpected argument") {
		t.Fatalf("extra cleanup argument accepted: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestCorpusAssuranceOrchestratorCleanupTakeoverDecodesRFC3339ClaimUntil(t *testing.T) {
	root := t.TempDir()
	request := filepath.Join(root, "cleanup.json")
	if err := os.WriteFile(request, []byte(`{"claim":{"campaignId":"campaign","jobId":"job","generation":1,"allocationAlias":"scratch-a","hubAlias":"hub-a","worker":"worker-b","claimUntil":"2030-01-01T00:00:00Z"},"bundlePath":"/tmp/bundle.json","creationPath":"/tmp/creation.json","preflightPath":"/tmp/preflight.json","targetOrg":"scratch-a","sfBin":"/tmp/sf","outputPath":"/tmp/cleanup.json"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"corpus", "assurance", "orchestrator", "cleanup-takeover", "--db", filepath.Join(root, "orchestrator.db"), "--request", request}, &stdout, &stderr); code == 0 || !strings.Contains(stderr.String(), "orchestrator-worker-cleanup-failed") || strings.Contains(stderr.String(), "expected JSON object") || strings.Contains(stderr.String(), "cannot parse time") || strings.Contains(stderr.String(), "RFC3339") {
		t.Fatalf("RFC3339 claimUntil was not decoded before cleanup validation: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestCorpusAssuranceOrchestratorCleanupCloseIsNotPublic(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"corpus", "assurance", "orchestrator", "cleanup-close"}, &stdout, &stderr); code == 0 || !strings.Contains(stderr.String(), "unknown corpus assurance orchestrator operation") {
		t.Fatalf("public cleanup-close accepted: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestCorpusAssuranceOrchestratorWorkerTransferValidatesTypedInputs(t *testing.T) {
	root := t.TempDir()
	planPath := filepath.Join(root, "plan.json")
	leasePath := filepath.Join(root, "lease.json")
	oraclePath := filepath.Join(root, "oracle-plan.json")
	outputPath := filepath.Join(root, "transfer.json")
	if err := os.WriteFile(leasePath, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oraclePath, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name          string
		plan          string
		args          []string
		relativePaths bool
		wantErr       string
	}{
		{name: "unknown JSON key", plan: `{"unknown":true}`, wantErr: "unknown JSON key"},
		{name: "trailing JSON value", plan: `{} {}`, wantErr: "multiple JSON values"},
		{name: "relative transfer path", plan: `{}`, relativePaths: true, wantErr: "absolute worker transfer paths are required"},
		{name: "trailing CLI argument", plan: `{}`, args: []string{"trailing"}, wantErr: "unexpected argument"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := os.WriteFile(planPath, []byte(test.plan), 0o600); err != nil {
				t.Fatal(err)
			}
			args := []string{"corpus", "assurance", "orchestrator", "worker-transfer", "--plan", planPath, "--lease", leasePath, "--source-batch", filepath.Join(root, "batch"), "--evidence-root", filepath.Join(root, "evidence"), "--oracle-plan", oraclePath, "--output", outputPath}
			if test.relativePaths {
				args = []string{"corpus", "assurance", "orchestrator", "worker-transfer", "--plan", planPath, "--lease", leasePath, "--source-batch", "relative-batch", "--evidence-root", "relative-evidence", "--oracle-plan", "relative-oracle", "--output", outputPath}
			}
			args = append(args, test.args...)
			var stdout, stderr bytes.Buffer
			if code := Run(context.Background(), args, &stdout, &stderr); code == 0 || !strings.Contains(stderr.String(), test.wantErr) {
				t.Fatalf("worker transfer validation: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
		})
	}
}

func TestCorpusAssuranceOrchestratorWorkerTransferPreflightsOutput(t *testing.T) {
	root := t.TempDir()
	planPath := filepath.Join(root, "plan.json")
	leasePath := filepath.Join(root, "lease.json")
	oraclePath := filepath.Join(root, "oracle-plan.json")
	if err := os.WriteFile(planPath, []byte(`{"unknown":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"corpus", "assurance", "orchestrator", "worker-transfer", "--plan", planPath, "--lease", leasePath, "--source-batch", filepath.Join(root, "batch"), "--evidence-root", filepath.Join(root, "evidence"), "--oracle-plan", oraclePath}

	t.Run("existing output before plan validation", func(t *testing.T) {
		outputPath := filepath.Join(root, "existing.json")
		if err := os.WriteFile(outputPath, []byte(`{}\n`), 0o600); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		if code := Run(context.Background(), append(append([]string{}, args...), "--output", outputPath), &stdout, &stderr); code == 0 || !strings.Contains(stderr.String(), "worker transfer output already exists") {
			t.Fatalf("existing output was not preflighted: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
		}
	})

	t.Run("relative output before plan validation", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		if code := Run(context.Background(), append(append([]string{}, args...), "--output", "relative-transfer.json"), &stdout, &stderr); code == 0 || !strings.Contains(stderr.String(), "absolute clean worker transfer output path is required") {
			t.Fatalf("relative output was not preflighted: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
		}
	})
}

func writeOrchestratorCLIJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readOrchestratorCLIJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, value); err != nil {
		t.Fatal(err)
	}
}

func orchestratorCLIFileSHA256(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
