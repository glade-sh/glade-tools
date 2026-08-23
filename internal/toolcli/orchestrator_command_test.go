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
