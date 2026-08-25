package toolcli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
		Shards:                [][]string{{"apex:System.One"}, {"apex:System.Two"}},
	})
	planPath := filepath.Join(root, "plan.json")
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"corpus", "assurance", "orchestrator", "plan", "--campaign", definitionPath, "--output", planPath}, &stdout, &stderr); code != 0 {
		t.Fatalf("plan code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var plan corpusassurance.OrchestratorCampaignPlan
	readOrchestratorCLIJSON(t, planPath, &plan)
	if plan.MaxAttemptsPerJob != corpusassurance.DefaultOrchestratorMaxAttemptsPerJob {
		t.Fatalf("plan max attempts per job = %d, want %d", plan.MaxAttemptsPerJob, corpusassurance.DefaultOrchestratorMaxAttemptsPerJob)
	}
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

func TestCorpusAssuranceOrchestratorProductionBuildIsReachable(t *testing.T) {
	request := filepath.Join(t.TempDir(), "request.json")
	if err := os.WriteFile(request, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if err := runCorpusAssuranceOrchestrator(context.Background(), []string{"production-build", "--request", request}, &stdout); err == nil || !strings.Contains(err.Error(), "mode-0600") {
		t.Fatalf("production-build accepted writable private request: %v", err)
	}
	if err := os.Chmod(request, 0o600); err != nil {
		t.Fatal(err)
	}
	for name, args := range map[string][]string{
		"duplicate": {"production-build", "--request", request, "--request", request},
		"trailing":  {"production-build", "--request", request, "trailing"},
	} {
		if err := runCorpusAssuranceOrchestrator(context.Background(), args, &stdout); err == nil {
			t.Fatalf("production-build accepted %s arguments", name)
		}
	}
}

func TestCorpusAssuranceOrchestratorHubObserveRequiresModeProtectedSanitizedJSON(t *testing.T) {
	root := t.TempDir()
	database := filepath.Join(root, "orchestrator.db")
	observation := filepath.Join(root, "hub-observation.json")
	writeOrchestratorCLIJSON(t, observation, map[string]any{
		"hubAlias": "hub-a", "observedAt": "2026-08-22T12:00:00Z", "healthy": true, "quarantined": false,
		"dailyScratchOrgsRemaining": 1, "activeScratchOrgsRemaining": 1,
	})
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"corpus", "assurance", "orchestrator", "hub-observe", "--db", database, "--observation", observation}, &stdout, &stderr); code != 0 {
		t.Fatalf("hub-observe code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String(), "hub-a") || strings.Contains(stdout.String(), "detail") {
		t.Fatalf("hub-observe leaked observation data: %q", stdout.String())
	}
	orchestrator, err := corpusassurance.OpenOrchestrator(database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = orchestrator.Close() })
	var healthy, daily, active int
	db, err := sql.Open("sqlite", database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.QueryRow(`SELECT healthy, daily_scratch_orgs_remaining, active_scratch_orgs_remaining FROM hub_observations WHERE hub_alias = 'hub-a'`).Scan(&healthy, &daily, &active); err != nil {
		t.Fatal(err)
	}
	if healthy != 1 || daily != 1 || active != 1 {
		t.Fatalf("recorded observation = healthy=%d daily=%d active=%d", healthy, daily, active)
	}
	if err := os.Chmod(observation, 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run(context.Background(), []string{"corpus", "assurance", "orchestrator", "hub-observe", "--db", database, "--observation", observation}, &stdout, &stderr); code == 0 {
		t.Fatal("world-readable observation accepted")
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

func TestCorpusAssuranceOrchestratorWorkerOnceHasOnlyTypedFixedFlags(t *testing.T) {
	root := t.TempDir()
	args := []string{"corpus", "assurance", "orchestrator", "worker-once", "--db", filepath.Join(root, "orchestrator.db")}
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), args, &stdout, &stderr); code == 0 || !strings.Contains(stderr.String(), "flag provided but not defined") {
		t.Fatalf("worker-once accepted coordinator DB/arbitrary flags: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestCorpusAssuranceOrchestratorWorkerCleanupAndExactClaimHaveFixedBindings(t *testing.T) {
	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"corpus", "assurance", "orchestrator", "worker-cleanup", "--db", filepath.Join(root, "orchestrator.db")}, &stdout, &stderr); code == 0 || !strings.Contains(stderr.String(), "flag provided but not defined") {
		t.Fatalf("worker-cleanup accepted coordinator DB: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	leasePath := filepath.Join(root, "lease.json")
	writeOrchestratorCLIJSON(t, leasePath, corpusassurance.OrchestratorLease{CampaignID: "campaign-a", JobID: "job-a", Generation: 1, Worker: "worker-a"})
	base := []string{"corpus", "assurance", "orchestrator", "cleanup-claim", "--db", filepath.Join(root, "orchestrator.db"), "--output", filepath.Join(root, "claim.json")}
	for name, extra := range map[string][]string{
		"missing allocation": {"--lease", leasePath, "--seconds", "360", "--worker", "worker-a"},
		"short duration":     {"--lease", leasePath, "--allocation", "scratch-a", "--seconds", "250", "--worker", "worker-a"},
		"worker drift":       {"--lease", leasePath, "--allocation", "scratch-a", "--seconds", "360", "--worker", "worker-b"},
	} {
		t.Run(name, func(t *testing.T) {
			stdout.Reset()
			stderr.Reset()
			args := append(append([]string(nil), base...), extra...)
			if code := Run(context.Background(), args, &stdout, &stderr); code == 0 {
				t.Fatalf("invalid exact cleanup claim accepted: stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
		})
	}
	scopePath := filepath.Join(root, "scope.json")
	writeOrchestratorCLIJSON(t, scopePath, corpusassurance.SurfaceOracleScope{SchemaVersion: 1, Kind: "all-runtime", SourceProfileSHA256: strings.Repeat("1", 64), LedgerSHA256: strings.Repeat("2", 64), PolicySHA256: strings.Repeat("3", 64), Total: 2, ByDisposition: map[string]int{"deterministic-mock-required": 1, "local-runtime-required": 1}, Rows: []corpusassurance.SurfaceOracleScopeRow{{SurfaceID: "apex:System.One", Disposition: "deterministic-mock-required"}, {SurfaceID: "apex:System.Two", Disposition: "local-runtime-required"}}})
	plan, err := corpusassurance.PlanOrchestratorCampaign(corpusassurance.OrchestratorCampaignDefinition{
		Candidate: corpusassurance.OrchestratorArtifact{Commit: strings.Repeat("a", 40), SHA256: strings.Repeat("b", 64)}, Tools: corpusassurance.OrchestratorArtifact{Commit: strings.Repeat("c", 40), SHA256: strings.Repeat("d", 64)},
		ScopePath: scopePath, ScopeSHA256: orchestratorCLIFileSHA256(t, scopePath), ControlledInputSHA256: map[string]string{"oracle-plan": strings.Repeat("e", 64)}, Shards: [][]string{{"apex:System.One", "apex:System.Two"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	database := filepath.Join(root, "orchestrator.db")
	orchestrator, err := corpusassurance.OpenOrchestrator(database)
	if err != nil {
		t.Fatal(err)
	}
	defer orchestrator.Close()
	if err := orchestrator.InitCampaign(plan); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	lease, err := orchestrator.Lease(plan.CampaignID, "worker-a", now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	remaining := 1
	if err := orchestrator.SetHubCapacity("hub-a", 1); err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.ObserveHub(corpusassurance.OrchestratorHubObservation{HubAlias: "hub-a", ObservedAt: now, Healthy: true, DailyScratchOrgsRemaining: &remaining, ActiveScratchOrgsRemaining: &remaining}); err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.Reserve(lease, "hub-a", "scratch-a", now); err != nil {
		t.Fatal(err)
	}
	writeOrchestratorCLIJSON(t, leasePath, lease)
	stdout.Reset()
	stderr.Reset()
	mixedPath := filepath.Join(root, "mixed-claim.json")
	mixedArgs := []string{"corpus", "assurance", "orchestrator", "cleanup-claim", "--db", database, "--campaign", plan.CampaignID, "--lease", leasePath, "--allocation", "scratch-a", "--worker", "worker-a", "--seconds", "360", "--output", mixedPath}
	if code := Run(context.Background(), mixedArgs, &stdout, &stderr); code == 0 || !strings.Contains(stderr.String(), "mutually exclusive") {
		t.Fatalf("mixed exact/campaign cleanup claim accepted: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	claimPath := filepath.Join(root, "exact-claim.json")
	args := []string{"corpus", "assurance", "orchestrator", "cleanup-claim", "--db", database, "--lease", leasePath, "--allocation", "scratch-a", "--worker", "worker-a", "--seconds", "360", "--output", claimPath}
	if code := Run(context.Background(), args, &stdout, &stderr); code != 0 {
		t.Fatalf("exact cleanup claim code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var claim corpusassurance.OrchestratorCleanupClaim
	readOrchestratorCLIJSON(t, claimPath, &claim)
	if claim.JobID != lease.JobID || claim.Generation != lease.Generation || claim.AllocationAlias != "scratch-a" || claim.Worker != lease.Worker || !claim.ClaimUntil.After(now.Add(corpusassurance.MinimumOrchestratorSSHCleanupClaimDuration)) {
		t.Fatalf("exact cleanup claim = %#v", claim)
	}
}

func TestCorpusAssuranceOrchestratorRawIngestUsesTypedFixedFlags(t *testing.T) {
	root := t.TempDir()
	args := []string{"corpus", "assurance", "orchestrator", "raw-ingest", "--plan", filepath.Join(root, "plan.json"), "--lease", filepath.Join(root, "lease.json"), "--oracle-plan", filepath.Join(root, "ORACLE_PLAN.json"), "--raw-root", filepath.Join(root, "raw"), "--packet-output", filepath.Join(root, "packet"), "--output", filepath.Join(root, "receipt.json")}
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), args, &stdout, &stderr)
	if code == 0 || strings.Contains(stderr.String(), "unknown corpus assurance orchestrator operation") || strings.Contains(stderr.String(), "flag provided but not defined") {
		t.Fatalf("raw-ingest contract rejected before typed input validation: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "read orchestrator plan") {
		t.Fatalf("raw-ingest did not validate the plan: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestCorpusAssuranceOrchestratorRawIngestRejectsNonPrivateRawRoot(t *testing.T) {
	root := t.TempDir()
	planPath, leasePath, oraclePath := filepath.Join(root, "plan.json"), filepath.Join(root, "lease.json"), filepath.Join(root, "ORACLE_PLAN.json")
	for _, path := range []string{planPath, leasePath, oraclePath} {
		if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	rawRoot := filepath.Join(root, "raw")
	if err := os.WriteFile(rawRoot, []byte("not a directory\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"corpus", "assurance", "orchestrator", "raw-ingest", "--plan", planPath, "--lease", leasePath, "--oracle-plan", oraclePath, "--raw-root", rawRoot, "--packet-output", filepath.Join(root, "packet"), "--output", filepath.Join(root, "receipt.json")}
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), args, &stdout, &stderr); code == 0 || !strings.Contains(stderr.String(), "raw-ingest root") {
		t.Fatalf("non-private raw root accepted: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestCorpusAssuranceOrchestratorRawAcceptUsesTypedFixedFlags(t *testing.T) {
	root := t.TempDir()
	args := []string{"corpus", "assurance", "orchestrator", "raw-accept", "--db", filepath.Join(root, "orchestrator.db"), "--plan", filepath.Join(root, "plan.json"), "--lease", filepath.Join(root, "lease.json"), "--allocation", "scratch-canary", "--ssh-receipt", filepath.Join(root, "ssh.json"), "--receipt", filepath.Join(root, "receipt.json"), "--packet", filepath.Join(root, "packet"), "--output", filepath.Join(root, "acceptance.json")}
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), args, &stdout, &stderr)
	if code == 0 || strings.Contains(stderr.String(), "unknown corpus assurance orchestrator operation") || strings.Contains(stderr.String(), "flag provided but not defined") {
		t.Fatalf("raw-accept contract rejected before typed input validation: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "read orchestrator plan") {
		t.Fatalf("raw-accept did not validate the plan: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestCorpusAssuranceOrchestratorRawAbortUsesTypedFixedFlags(t *testing.T) {
	root := t.TempDir()
	for _, test := range []struct {
		name string
		args []string
	}{
		{"observe", []string{"corpus", "assurance", "orchestrator", "raw-abort-observe", "--plan", filepath.Join(root, "plan.json"), "--lease", filepath.Join(root, "lease.json"), "--ssh-receipt", filepath.Join(root, "ssh.json"), "--bundle", filepath.Join(root, "bundle.json"), "--allocation", "scratch-canary", "--sf-bin", filepath.Join(root, "sf"), "--raw-root", filepath.Join(root, "raw"), "--output", filepath.Join(root, "observation.json")}},
		{"accept", []string{"corpus", "assurance", "orchestrator", "raw-abort-accept", "--db", filepath.Join(root, "orchestrator.db"), "--plan", filepath.Join(root, "plan.json"), "--lease", filepath.Join(root, "lease.json"), "--ssh-receipt", filepath.Join(root, "ssh.json"), "--allocation", "scratch-canary", "--observation", filepath.Join(root, "observation.json"), "--output", filepath.Join(root, "acceptance.json")}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run(context.Background(), test.args, &stdout, &stderr)
			if code == 0 || strings.Contains(stderr.String(), "unknown corpus assurance orchestrator operation") || strings.Contains(stderr.String(), "flag provided but not defined") {
				t.Fatalf("raw abort contract rejected before typed input validation: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			if !strings.Contains(stderr.String(), "read orchestrator plan") {
				t.Fatalf("raw abort did not validate the plan first: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
		})
	}
}

func TestCorpusAssuranceOrchestratorWorkerOnceRejectsUnsealedExecutableBeforeOrgWork(t *testing.T) {
	root := t.TempDir()
	plan := filepath.Join(root, "plan.json")
	writeOrchestratorCLIJSON(t, plan, corpusassurance.OrchestratorCampaignPlan{Definition: corpusassurance.OrchestratorCampaignDefinition{Tools: corpusassurance.OrchestratorArtifact{SHA256: strings.Repeat("f", 64)}}})
	outputRoot := filepath.Join(root, "output")
	args := []string{"corpus", "assurance", "orchestrator", "worker-once", "--plan", plan, "--plan-sha256", orchestratorCLIFileSHA256(t, plan), "--lease", filepath.Join(root, "lease.json"), "--lease-sha256", strings.Repeat("e", 64), "--bundle", filepath.Join(root, "bundle.json"), "--dev-hub", "sealed-hub", "--target-org", "scratch-a", "--sf-bin", filepath.Join(root, "sf"), "--output-root", outputRoot}
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), args, &stdout, &stderr); code == 0 || !strings.Contains(stderr.String(), "executing worker does not match sealed tools") {
		t.Fatalf("unsealed executable accepted: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if _, err := os.Lstat(outputRoot); !os.IsNotExist(err) {
		t.Fatalf("output root exists after executable rejection: %v", err)
	}
}

func TestOrchestratorWorkerExecutableAllowsSealedMatchingArchitecture(t *testing.T) {
	commit := strings.Repeat("a", 40)
	primarySHA := strings.Repeat("b", 64)
	amd64SHA := strings.Repeat("c", 64)
	tools := corpusassurance.OrchestratorArtifact{Commit: commit, SHA256: primarySHA}
	definition := corpusassurance.OrchestratorCampaignDefinition{Tools: tools, ControlledInputSHA256: map[string]string{corpusassurance.OrchestratorToolsAMD64Input: amd64SHA}}
	bundle := corpusassurance.OracleBundle{
		Tools:            corpusassurance.RuntimeArtifact{Commit: commit, OS: "darwin", Arch: "arm64", SHA256: primarySHA},
		ToolsAMD64:       corpusassurance.RuntimeArtifact{Commit: commit, OS: "darwin", Arch: "amd64", SHA256: amd64SHA},
		ToolsAMD64SHA256: amd64SHA,
	}
	if !orchestratorWorkerExecutableMatches(definition, bundle, primarySHA, "darwin", "arm64") {
		t.Fatal("exact primary worker was rejected")
	}
	if !orchestratorWorkerExecutableMatches(definition, bundle, amd64SHA, "darwin", "amd64") {
		t.Fatal("sealed same-commit amd64 worker was rejected")
	}
	unbound := definition
	unbound.ControlledInputSHA256 = nil
	if orchestratorWorkerExecutableMatches(unbound, bundle, amd64SHA, "darwin", "amd64") {
		t.Fatal("alternate worker absent from the campaign was accepted")
	}
	for name, mutate := range map[string]func(*corpusassurance.OracleBundle){
		"primary hash": func(value *corpusassurance.OracleBundle) { value.Tools.SHA256 = strings.Repeat("d", 64) },
		"commit":       func(value *corpusassurance.OracleBundle) { value.ToolsAMD64.Commit = strings.Repeat("e", 40) },
		"hash":         func(value *corpusassurance.OracleBundle) { value.ToolsAMD64.SHA256 = strings.Repeat("f", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			changed := bundle
			mutate(&changed)
			if orchestratorWorkerExecutableMatches(definition, changed, amd64SHA, "darwin", "amd64") {
				t.Fatal("unbound alternate worker was accepted")
			}
		})
	}
	if orchestratorWorkerExecutableMatches(definition, bundle, amd64SHA, "darwin", "arm64") {
		t.Fatal("alternate worker was accepted on the wrong architecture")
	}
}

func TestCorpusAssuranceOrchestratorWorkerOnceRejectsDispatchedInputHashDrift(t *testing.T) {
	root := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	executableSHA, err := sha256File(executable)
	if err != nil {
		t.Fatal(err)
	}
	planPath, leasePath := filepath.Join(root, "plan.json"), filepath.Join(root, "lease.json")
	writeOrchestratorCLIJSON(t, planPath, corpusassurance.OrchestratorCampaignPlan{Definition: corpusassurance.OrchestratorCampaignDefinition{Tools: corpusassurance.OrchestratorArtifact{SHA256: executableSHA}}})
	writeOrchestratorCLIJSON(t, leasePath, corpusassurance.OrchestratorLease{})
	planSHA, leaseSHA := orchestratorCLIFileSHA256(t, planPath), orchestratorCLIFileSHA256(t, leasePath)
	for name, hashes := range map[string][2]string{
		"plan":  {strings.Repeat("a", 64), leaseSHA},
		"lease": {planSHA, strings.Repeat("b", 64)},
	} {
		t.Run(name, func(t *testing.T) {
			outputRoot := filepath.Join(root, "output-"+name)
			args := []string{"corpus", "assurance", "orchestrator", "worker-once", "--plan", planPath, "--plan-sha256", hashes[0], "--lease", leasePath, "--lease-sha256", hashes[1], "--bundle", filepath.Join(root, "bundle.json"), "--dev-hub", "sealed-hub", "--target-org", "scratch-a", "--sf-bin", filepath.Join(root, "sf"), "--output-root", outputRoot}
			var stdout, stderr bytes.Buffer
			if code := Run(context.Background(), args, &stdout, &stderr); code == 0 || !strings.Contains(stderr.String(), "does not match dispatched hash") {
				t.Fatalf("%s hash drift accepted: code=%d stdout=%q stderr=%q", name, code, stdout.String(), stderr.String())
			}
			if _, err := os.Lstat(outputRoot); !os.IsNotExist(err) {
				t.Fatalf("output root exists after %s hash drift: %v", name, err)
			}
		})
	}
}

func TestCorpusAssuranceOrchestratorSSHDispatchRejectsUnsafeHostBeforeFiles(t *testing.T) {
	root := t.TempDir()
	args := []string{"corpus", "assurance", "orchestrator", "ssh-dispatch", "--db", filepath.Join(root, "orchestrator.db"), "--host", "worker;rm -rf /", "--worker-bin", filepath.Join(root, "glade-tools"), "--plan", filepath.Join(root, "plan"), "--remote-plan", filepath.Join(root, "remote-plan"), "--lease", filepath.Join(root, "lease"), "--bundle", filepath.Join(root, "bundle"), "--target-org", "scratch-a", "--sf-bin", filepath.Join(root, "sf"), "--output-root", filepath.Join(root, "output-root"), "--output", filepath.Join(root, "output")}
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), args, &stdout, &stderr); code == 0 || !strings.Contains(stderr.String(), "invalid orchestrator SSH dispatch target") {
		t.Fatalf("unsafe SSH host accepted: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestCorpusAssuranceOrchestratorSSHFetchValidatesTypedInputs(t *testing.T) {
	root := t.TempDir()
	args := []string{"corpus", "assurance", "orchestrator", "ssh-fetch", "--plan", filepath.Join(root, "plan.json"), "--remote-plan", filepath.Join(root, "remote-plan.json"), "--lease", filepath.Join(root, "lease.json"), "--ssh-receipt", filepath.Join(root, "ssh.json"), "--host", "operator@worker.example.internal", "--worker-bin", filepath.Join(root, "glade-tools"), "--bundle", filepath.Join(root, "bundle.json"), "--dev-hub", "sealed-hub", "--target-org", "scratch-a", "--sf-bin", filepath.Join(root, "sf"), "--remote-root", filepath.Join(root, "remote"), "--raw-root", filepath.Join(root, "raw")}
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), args, &stdout, &stderr); code == 0 || strings.Contains(stderr.String(), "unknown corpus assurance orchestrator operation") || strings.Contains(stderr.String(), "flag provided but not defined") || !strings.Contains(stderr.String(), "read orchestrator plan") {
		t.Fatalf("ssh-fetch contract was not recognized: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestWriteOrchestratorSSHResultExposesActionReceiptOnError(t *testing.T) {
	var output bytes.Buffer
	wantErr := errors.New("receipt path changed")
	receipt := corpusassurance.OrchestratorSSHDispatchReceipt{SchemaVersion: 1, Status: "failed", ActionRequired: true, ActionCode: "inspect-remote-lifecycle-artifacts-and-close-cleanup"}
	if err := writeOrchestratorSSHResult(&output, receipt, wantErr); !errors.Is(err, wantErr) {
		t.Fatalf("result error = %v", err)
	}
	var got corpusassurance.OrchestratorSSHDispatchReceipt
	if err := json.Unmarshal(output.Bytes(), &got); err != nil || got != receipt {
		t.Fatalf("action receipt = %#v, want %#v (err=%v)", got, receipt, err)
	}
}

func TestCorpusAssuranceOrchestratorNewCommandsRejectDuplicateFlagsBeforeParse(t *testing.T) {
	root := t.TempDir()
	for _, command := range []string{"worker-once", "ssh-dispatch", "ssh-fetch"} {
		for _, first := range []string{"--plan", "-plan"} {
			t.Run(command+first, func(t *testing.T) {
				args := []string{"corpus", "assurance", "orchestrator", command, first, filepath.Join(root, "one"), "--plan", filepath.Join(root, "two")}
				var stdout, stderr bytes.Buffer
				if code := Run(context.Background(), args, &stdout, &stderr); code == 0 || !strings.Contains(stderr.String(), "duplicate flag --plan") {
					t.Fatalf("duplicate flag accepted: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
				}
			})
		}
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
