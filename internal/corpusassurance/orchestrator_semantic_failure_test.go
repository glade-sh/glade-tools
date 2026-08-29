package corpusassurance

import (
	"strings"
	"testing"
	"time"
)

func semanticMismatchFixture(t *testing.T, closeCleanup bool) (*Orchestrator, OrchestratorCampaignPlan, OrchestratorLease, string, time.Time) {
	t.Helper()
	root := t.TempDir()
	scope, _ := writeSurfaceOracleIndexInputs(t, root)
	definition := testOrchestratorDefinition(t, scope, [][]string{{"apex:System.One"}, {"apex:System.Three", "apex:System.Two"}})
	plan, err := PlanOrchestratorCampaign(definition)
	if err != nil {
		t.Fatal(err)
	}
	orchestrator := openTestOrchestrator(t)
	if err := orchestrator.InitCampaign(plan); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	lease, err := orchestrator.Lease(plan.CampaignID, "worker-a", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.SetHubCapacity("hub-a", 1); err != nil {
		t.Fatal(err)
	}
	observeReadyHub(t, orchestrator, "hub-a", now)
	allocation := "scratch-semantic"
	if err := orchestrator.Reserve(lease, "hub-a", allocation, now); err != nil {
		t.Fatal(err)
	}
	claim, err := orchestrator.ClaimCleanup(plan.CampaignID, lease.Worker, now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if closeCleanup {
		if err := orchestrator.closeCleanup(claim, now.Add(time.Second), false); err != nil {
			t.Fatal(err)
		}
	}
	return orchestrator, plan, lease, allocation, now.Add(2 * time.Second)
}

func semanticMismatchAuthorityFor(plan OrchestratorCampaignPlan, lease OrchestratorLease, allocation string) OrchestratorSemanticMismatchAuthority {
	dispatch := OrchestratorSSHDispatchReceipt{
		SchemaVersion: 1, CampaignID: lease.CampaignID, JobID: lease.JobID,
		ShardIndex: lease.ShardIndex, Generation: lease.Generation, Status: "failed",
		FailureCode: orchestratorSSHDispatchFailed, ExitCode: 1,
		SpecSHA256: plan.SpecSHA256, PlanSHA256: planSHA256For(plan), LeaseSHA256: leaseSHA256For(lease),
	}
	dispatchSHA, _ := canonicalJSONHash(dispatch)
	evidence := OrchestratorSemanticMismatchEvidence{
		SchemaVersion: 1, Status: "validated-semantic-mismatch", FailureCode: OrchestratorSemanticMismatchFailureCode,
		CampaignID: lease.CampaignID, JobID: lease.JobID, ShardIndex: lease.ShardIndex, Generation: lease.Generation,
		SpecSHA256: plan.SpecSHA256, PlanSHA256: planSHA256For(plan), LeaseSHA256: leaseSHA256For(lease),
		Candidate: plan.Definition.Candidate, Tools: plan.Definition.Tools, SurfaceIDs: append([]string(nil), lease.SurfaceIDs...),
		Expected: "622", Actual: "623", ResultSHA256: strings.Repeat("9", 64), Assertion: "status-code-count", CompilePassed: true, ResidueAbsent: true, CleanupClosed: true,
	}
	evidenceSHA, _ := canonicalJSONHash(evidence)
	return OrchestratorSemanticMismatchAuthority{
		SchemaVersion: 1, Kind: "semantic-mismatch", Status: "validated-semantic-mismatch",
		FailureCode: OrchestratorSemanticMismatchFailureCode, CampaignID: lease.CampaignID, JobID: lease.JobID,
		ShardIndex: lease.ShardIndex, Generation: lease.Generation, SpecSHA256: plan.SpecSHA256,
		PlanSHA256: planSHA256For(plan), LeaseSHA256: leaseSHA256For(lease), DispatchSHA256: dispatchSHA,
		Candidate: plan.Definition.Candidate, Tools: plan.Definition.Tools, SurfaceIDs: append([]string(nil), lease.SurfaceIDs...),
		AllocationAlias: allocation, Worker: lease.Worker, LeaseUntil: lease.LeaseUntil, DurationMS: lease.DurationMS, Evidence: evidence, EvidenceSHA256: evidenceSHA, Dispatch: dispatch,
	}
}

func TestOrchestratorTerminalizesSemanticMismatchWithPermanentZeroCredit(t *testing.T) {
	orchestrator, plan, lease, allocation, _ := semanticMismatchFixture(t, true)
	authority := semanticMismatchAuthorityFor(plan, lease, allocation)
	// The lease may expire before the recovery worker gets to the terminal
	// decision; its exact identity remains valid until a new generation exists.
	receipt, err := orchestrator.TerminalizeSemanticMismatch(authority)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Status != "terminalized-semantic-mismatch" || receipt.ProofCredit != 0 || receipt.CleanupCreditBlock != 1 {
		t.Fatalf("terminal receipt = %#v", receipt)
	}
	var jobStatus, attemptStatus string
	var generation int
	if err := orchestrator.db.QueryRow(`SELECT status, generation FROM jobs WHERE campaign_id = ? AND id = ?`, plan.CampaignID, lease.JobID).Scan(&jobStatus, &generation); err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.db.QueryRow(`SELECT status FROM attempts WHERE campaign_id = ? AND job_id = ? AND generation = ?`, plan.CampaignID, lease.JobID, lease.Generation).Scan(&attemptStatus); err != nil {
		t.Fatal(err)
	}
	if jobStatus != "failed" || attemptStatus != "failed" || generation != lease.Generation {
		t.Fatalf("terminal states job=%s attempt=%s generation=%d", jobStatus, attemptStatus, generation)
	}
	var allocationState, cleanupState string
	if err := orchestrator.db.QueryRow(`SELECT state FROM scratch_allocations WHERE allocation_alias = ?`, allocation).Scan(&allocationState); err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.db.QueryRow(`SELECT state FROM cleanup_journal WHERE allocation_alias = ?`, allocation).Scan(&cleanupState); err != nil {
		t.Fatal(err)
	}
	var blocks, receipts, credits int
	if err := orchestrator.db.QueryRow(`SELECT count(*) FROM cleanup_credit_blocks WHERE allocation_alias = ?`, allocation).Scan(&blocks); err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.db.QueryRow(`SELECT count(*) FROM receipts WHERE campaign_id = ? AND job_id = ? AND generation = ?`, plan.CampaignID, lease.JobID, lease.Generation).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.db.QueryRow(`SELECT count(*) FROM proof_credits WHERE campaign_id = ?`, plan.CampaignID).Scan(&credits); err != nil {
		t.Fatal(err)
	}
	if allocationState != "closed" || cleanupState != "closed" || blocks != 1 || receipts != 0 || credits != 0 {
		t.Fatalf("terminal accounting allocation=%s cleanup=%s blocks=%d receipts=%d credits=%d", allocationState, cleanupState, blocks, receipts, credits)
	}
	if _, err := orchestrator.TerminalizeSemanticMismatch(authority); err != nil {
		t.Fatal("idempotent terminalization failed: ", err)
	}
	changed := authority
	changed.Evidence.Expected = "621"
	changed.EvidenceSHA256, _ = canonicalJSONHash(changed.Evidence)
	if _, err := orchestrator.TerminalizeSemanticMismatch(changed); err == nil {
		t.Fatal("changed self-consistent semantic evidence replay succeeded")
	}
	if err := orchestrator.db.QueryRow(`SELECT count(*) FROM cleanup_credit_blocks WHERE allocation_alias = ?`, allocation).Scan(&blocks); err != nil || blocks != 1 {
		t.Fatalf("zero-credit block count after replay=%d err=%v", blocks, err)
	}
}

func TestOrchestratorSemanticMismatchReplayRejectsAnotherBlockedAllocation(t *testing.T) {
	orchestrator, plan, lease, allocation, _ := semanticMismatchFixture(t, true)
	authority := semanticMismatchAuthorityFor(plan, lease, allocation)
	if _, err := orchestrator.TerminalizeSemanticMismatch(authority); err != nil {
		t.Fatal(err)
	}
	now := lease.LeaseUntil.Add(2 * time.Hour)
	otherLease, err := orchestrator.Lease(plan.CampaignID, "worker-b", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.SetHubCapacity("hub-b", 1); err != nil {
		t.Fatal(err)
	}
	observeReadyHub(t, orchestrator, "hub-b", now)
	if err := orchestrator.Reserve(otherLease, "hub-b", "scratch-other", now); err != nil {
		t.Fatal(err)
	}
	claim, err := orchestrator.ClaimCleanup(plan.CampaignID, otherLease.Worker, now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.closeCleanup(claim, now.Add(time.Second), false); err != nil {
		t.Fatal(err)
	}
	changed := authority
	changed.AllocationAlias = "scratch-other"
	if _, err := orchestrator.TerminalizeSemanticMismatch(changed); err == nil {
		t.Fatal("semantic mismatch replay accepted another generation's blocked allocation")
	}
}

func TestOrchestratorTerminalSemanticMismatchRejectsInvalidStates(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(*testing.T, *Orchestrator, OrchestratorCampaignPlan, OrchestratorLease, string)
		mutate func(*OrchestratorSemanticMismatchAuthority)
		want   string
	}{
		{name: "infrastructure dispatch", setup: func(*testing.T, *Orchestrator, OrchestratorCampaignPlan, OrchestratorLease, string) {}, mutate: func(a *OrchestratorSemanticMismatchAuthority) {
			a.Evidence.Status = "infra-failure"
		}, want: "semantic"},
		{name: "open cleanup", setup: func(*testing.T, *Orchestrator, OrchestratorCampaignPlan, OrchestratorLease, string) {}, mutate: func(*OrchestratorSemanticMismatchAuthority) {}, want: "closed allocation"},
		{name: "recorded receipt", setup: func(t *testing.T, o *Orchestrator, p OrchestratorCampaignPlan, l OrchestratorLease, _ string) {
			if _, err := o.db.Exec(`INSERT INTO receipts (id, campaign_id, job_id, generation, batch_root, manifest_sha256, binding_sha256, validated, recorded_at) VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?)`, "receipt-existing", p.CampaignID, l.JobID, l.Generation, "/tmp/batch", strings.Repeat("a", 64), strings.Repeat("b", 64), time.Now().UnixMilli()); err != nil {
				t.Fatal(err)
			}
		}, mutate: func(*OrchestratorSemanticMismatchAuthority) {}, want: "recorded receipt"},
		{name: "missing zero-credit block", setup: func(t *testing.T, o *Orchestrator, _ OrchestratorCampaignPlan, _ OrchestratorLease, allocation string) {
			if _, err := o.db.Exec(`DELETE FROM cleanup_credit_blocks WHERE allocation_alias = ?`, allocation); err != nil {
				t.Fatal(err)
			}
		}, mutate: func(*OrchestratorSemanticMismatchAuthority) {}, want: "zero-credit block"},
		{name: "stale lease", setup: func(_ *testing.T, o *Orchestrator, p OrchestratorCampaignPlan, _ OrchestratorLease, _ string) {
			if _, err := o.Lease(p.CampaignID, "worker-b", time.Date(2026, 8, 28, 12, 0, 3, 0, time.UTC), time.Minute); err != nil {
				panic(err)
			}
		}, mutate: func(a *OrchestratorSemanticMismatchAuthority) {}, want: "lease"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			closed := test.name != "open cleanup"
			o, p, l, allocation, _ := semanticMismatchFixture(t, closed)
			a := semanticMismatchAuthorityFor(p, l, allocation)
			test.setup(t, o, p, l, allocation)
			test.mutate(&a)
			if _, err := o.TerminalizeSemanticMismatch(a); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("terminalization error=%v, want %q", err, test.want)
			}
			var status string
			if err := o.db.QueryRow(`SELECT status FROM jobs WHERE campaign_id = ? AND id = ?`, p.CampaignID, l.JobID).Scan(&status); err != nil {
				t.Fatal(err)
			}
			if status != "running" {
				t.Fatalf("rejected terminalization changed job to %s", status)
			}
		})
	}
}
