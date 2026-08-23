package corpusassurance

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOrchestratorWorkerCrashRetainsJournalAndNoCredit(t *testing.T) {
	for _, stage := range []string{"before-scratch-create", "after-scratch-create", "after-salesforce-run", "before-credit"} {
		t.Run(stage, func(t *testing.T) {
			orchestrator, plan, lease, now, batch, oraclePlan := readyOrchestratorWorker(t)
			request := OrchestratorWorkerRequest{
				Plan: plan, Lease: lease, HubAlias: "hub-a", HubCapacity: 1,
				AllocationAlias: "scratch-" + stage, EvidenceRoot: filepath.Join(t.TempDir(), "evidence"), OraclePlanPath: oraclePlan,
			}
			runner := func(context.Context, OrchestratorWorkerRequest) (OrchestratorWorkerRunResult, error) {
				if stage == "before-credit" {
					return OrchestratorWorkerRunResult{BatchRoot: batch}, nil
				}
				return OrchestratorWorkerRunResult{}, errors.New("simulated " + stage)
			}
			beforeCredit := func() error { return nil }
			if stage == "before-credit" {
				beforeCredit = func() error { return errors.New("simulated before credit") }
			}
			_, err := runOrchestratorWorkerOnce(context.Background(), orchestrator, request, fixedOrchestratorWorkerClock(now), runner, func(string, string, string) error { return nil }, beforeCredit)
			if err == nil {
				t.Fatal("simulated worker crash succeeded")
			}
			var attempts, allocations, cleanup, credit, actions int
			for query, target := range map[string]*int{
				`SELECT count(*) FROM attempts WHERE campaign_id = ? AND job_id = ? AND generation = ? AND status = 'failed'`: &attempts,
				`SELECT count(*) FROM scratch_allocations WHERE campaign_id = ? AND job_id = ? AND generation = ?`:            &allocations,
				`SELECT count(*) FROM cleanup_journal WHERE campaign_id = ? AND job_id = ? AND generation = ?`:                &cleanup,
				`SELECT count(*) FROM proof_credits WHERE campaign_id = ?`:                                                    &credit,
				`SELECT count(*) FROM actions WHERE campaign_id = ? AND state = 'open'`:                                       &actions,
			} {
				args := []any{plan.CampaignID}
				if !strings.Contains(query, "proof_credits") && !strings.Contains(query, "actions") {
					args = append(args, lease.JobID, lease.Generation)
				}
				if err := orchestrator.db.QueryRow(query, args...).Scan(target); err != nil {
					t.Fatal(err)
				}
			}
			if attempts != 1 || allocations != 1 || cleanup != 1 || credit != 0 || actions != 1 {
				t.Fatalf("attempts=%d allocations=%d cleanup=%d credit=%d actions=%d", attempts, allocations, cleanup, credit, actions)
			}
			var allocationState, cleanupState string
			if err := orchestrator.db.QueryRow(`SELECT a.state, c.state FROM scratch_allocations a JOIN cleanup_journal c ON c.allocation_alias = a.allocation_alias WHERE a.campaign_id = ? AND a.job_id = ? AND a.generation = ?`, plan.CampaignID, lease.JobID, lease.Generation).Scan(&allocationState, &cleanupState); err != nil {
				t.Fatal(err)
			}
			wantAllocation, wantCleanup := "reserved", "pending"
			if stage == "before-credit" {
				wantAllocation, wantCleanup = "closed", "closed"
			}
			if allocationState != wantAllocation || cleanupState != wantCleanup {
				t.Fatalf("allocation=%q cleanup=%q, want %q/%q", allocationState, cleanupState, wantAllocation, wantCleanup)
			}
			if stage == "before-credit" {
				final := filepath.Join(request.EvidenceRoot, orchestratorWorkerBatchName(lease))
				if _, err := orchestrator.RecordReceipt(OrchestratorReceiptRequest{Lease: lease, BatchRoot: final}, now); err == nil {
					t.Fatal("failed attempt gained credit after worker returned")
				}
			}
		})
	}
}

func TestOrchestratorWorkerTransferIsAtomicSanitizedAndImmutable(t *testing.T) {
	_, plan, lease, _, batch, oraclePlan := readyOrchestratorWorker(t)
	writeFile(t, filepath.Join(batch, "raw", "worker.log"), "private raw output")
	evidenceRoot := filepath.Join(t.TempDir(), "evidence")
	started, release := make(chan string, 1), make(chan struct{})
	result := make(chan struct {
		transfer OrchestratorWorkerTransfer
		err      error
	}, 1)
	go func() {
		transfer, err := transferOrchestratorWorkerBatch(OrchestratorWorkerTransferRequest{Plan: plan, Lease: lease, SourceBatchRoot: batch, EvidenceRoot: evidenceRoot, OraclePlanPath: oraclePlan}, func(string, string, string) error { return nil }, func(final string) error {
			started <- final
			<-release
			return nil
		})
		result <- struct {
			transfer OrchestratorWorkerTransfer
			err      error
		}{transfer, err}
	}()
	final := <-started
	if _, err := os.Stat(final); !os.IsNotExist(err) {
		t.Fatalf("final batch became visible before rename: %v", err)
	}
	close(release)
	got := <-result
	if got.err != nil {
		t.Fatal(got.err)
	}
	if got.transfer.BatchRoot != final || !sha256Pattern.MatchString(got.transfer.ManifestSHA256) {
		t.Fatalf("transfer = %#v", got.transfer)
	}
	if _, err := os.Stat(filepath.Join(final, "raw", "worker.log")); !os.IsNotExist(err) {
		t.Fatalf("raw worker log transferred: %v", err)
	}
	info, err := os.Stat(filepath.Join(final, "evidence", "ORCHESTRATOR_BINDING.json"))
	if err != nil || info.Mode().Perm() != 0o400 {
		t.Fatalf("binding mode = %v, %v", info, err)
	}
	replayed, err := transferOrchestratorWorkerBatch(OrchestratorWorkerTransferRequest{Plan: plan, Lease: lease, SourceBatchRoot: batch, EvidenceRoot: evidenceRoot, OraclePlanPath: oraclePlan}, func(string, string, string) error { return nil }, nil)
	if err != nil || replayed != got.transfer {
		t.Fatalf("exact transfer replay = %#v, %v", replayed, err)
	}
	finalAudit := filepath.Join(final, "evidence", "FINAL_AUDIT.json")
	if err := os.Chmod(finalAudit, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(finalAudit, []byte("drift"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := transferOrchestratorWorkerBatch(OrchestratorWorkerTransferRequest{Plan: plan, Lease: lease, SourceBatchRoot: batch, EvidenceRoot: evidenceRoot, OraclePlanPath: oraclePlan}, func(string, string, string) error { return nil }, nil); err == nil {
		t.Fatal("drifted duplicate transfer succeeded")
	}
}

func TestOrchestratorWorkerResumesAfterCrashBeforeCredit(t *testing.T) {
	orchestrator, plan, lease, now, batch, oraclePlan := readyOrchestratorWorker(t)
	request := OrchestratorWorkerRequest{
		Plan: plan, Lease: lease, HubAlias: "hub-a", HubCapacity: 1,
		AllocationAlias: "scratch-resume", EvidenceRoot: filepath.Join(t.TempDir(), "evidence"), OraclePlanPath: oraclePlan,
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("simulated crash did not occur")
			}
		}()
		_, _ = runOrchestratorWorkerOnce(context.Background(), orchestrator, request, fixedOrchestratorWorkerClock(now), func(context.Context, OrchestratorWorkerRequest) (OrchestratorWorkerRunResult, error) {
			return OrchestratorWorkerRunResult{BatchRoot: batch}, nil
		}, func(string, string, string) error { return nil }, func() error { panic("simulated crash") })
	}()
	restart := now.Add(2 * time.Minute)
	renewedUntil := restart.Add(time.Minute)
	if _, err := orchestrator.db.Exec(`UPDATE jobs SET lease_until = ? WHERE campaign_id = ? AND id = ? AND generation = ?`, renewedUntil.UnixMilli(), plan.CampaignID, lease.JobID, lease.Generation); err != nil {
		t.Fatal(err)
	}
	if _, err := orchestrator.db.Exec(`UPDATE attempts SET lease_until = ? WHERE campaign_id = ? AND job_id = ? AND generation = ?`, renewedUntil.UnixMilli(), plan.CampaignID, lease.JobID, lease.Generation); err != nil {
		t.Fatal(err)
	}
	result, err := runOrchestratorWorkerOnce(context.Background(), orchestrator, request, fixedOrchestratorWorkerClock(restart), func(context.Context, OrchestratorWorkerRequest) (OrchestratorWorkerRunResult, error) {
		t.Fatal("resume reran Salesforce worker")
		return OrchestratorWorkerRunResult{}, nil
	}, func(string, string, string) error { return nil }, func() error { return nil })
	if err != nil || result.Receipt.AcceptedCredit+result.Receipt.RejectedCredit != len(lease.SurfaceIDs) {
		t.Fatalf("resume result = %#v, %v", result, err)
	}
}

func TestOrchestratorWorkerInvalidBatchLeavesCleanupOpen(t *testing.T) {
	orchestrator, plan, lease, now, batch, oraclePlan := readyOrchestratorWorker(t)
	if err := os.Remove(filepath.Join(batch, "evidence", "FINAL_AUDIT.json")); err != nil {
		t.Fatal(err)
	}
	request := OrchestratorWorkerRequest{
		Plan: plan, Lease: lease, HubAlias: "hub-a", HubCapacity: 1,
		AllocationAlias: "scratch-invalid-batch", EvidenceRoot: filepath.Join(t.TempDir(), "evidence"), OraclePlanPath: oraclePlan,
	}
	_, err := runOrchestratorWorkerOnce(context.Background(), orchestrator, request, fixedOrchestratorWorkerClock(now), func(context.Context, OrchestratorWorkerRequest) (OrchestratorWorkerRunResult, error) {
		return OrchestratorWorkerRunResult{BatchRoot: batch}, nil
	}, func(string, string, string) error { return nil }, func() error { return nil })
	if err == nil {
		t.Fatal("invalid worker batch succeeded")
	}
	var allocationState, cleanupState string
	if err := orchestrator.db.QueryRow(`SELECT a.state, c.state FROM scratch_allocations a JOIN cleanup_journal c ON c.allocation_alias = a.allocation_alias WHERE a.campaign_id = ? AND a.job_id = ? AND a.generation = ?`, plan.CampaignID, lease.JobID, lease.Generation).Scan(&allocationState, &cleanupState); err != nil {
		t.Fatal(err)
	}
	if allocationState != "reserved" || cleanupState != "pending" {
		t.Fatalf("invalid batch closed cleanup: allocation=%q cleanup=%q", allocationState, cleanupState)
	}
}

func TestOrchestratorWorkerTransferRejectsPartialUnboundAndCandidateDriftedBatch(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*testing.T, string)
		want   string
	}{
		{"partial", func(t *testing.T, batch string) {
			t.Helper()
			os.Remove(filepath.Join(batch, "evidence", "FINAL_AUDIT.json"))
		}, "regular file"},
		{"unbound", func(t *testing.T, batch string) {
			t.Helper()
			os.Remove(filepath.Join(batch, "evidence", "ORCHESTRATOR_BINDING.json"))
		}, "binding"},
		{"candidate-drift", func(t *testing.T, batch string) {
			t.Helper()
			if err := os.WriteFile(filepath.Join(batch, "bin", "glade-sealed"), []byte("drift"), 0o600); err != nil {
				t.Fatal(err)
			}
			resealSurfaceOracleBatch(t, batch)
		}, "campaign binding drift"},
		{"packet-traversal", func(t *testing.T, batch string) {
			t.Helper()
			raw := filepath.Join(batch, "raw", "worker.log")
			writeFile(t, raw, "private raw output")
			if err := os.Chmod(raw, 0o600); err != nil {
				t.Fatal(err)
			}
			manifestPath := filepath.Join(batch, "evidence", "salesforce-reconciliation-packet", reconciliationPacketManifestName)
			if err := os.Remove(manifestPath); err != nil {
				t.Fatal(err)
			}
			writeJSONValue(t, manifestPath, reportPacketManifest{SchemaVersion: 1, Files: []reportPacketManifestFile{{Name: "../../raw/worker.log", SHA256: surfaceOracleFileSHA256(t, raw), Mode: 0o600}}})
			receiptPath := filepath.Join(batch, "evidence", "SALESFORCE_RECONCILIATION.json")
			if err := os.Remove(receiptPath); err != nil {
				t.Fatal(err)
			}
			writeJSONValue(t, receiptPath, SalesforceReconciliation{SchemaVersion: 1, PacketManifestSHA256: surfaceOracleFileSHA256(t, manifestPath)})
		}, "packet manifest entry"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, plan, lease, _, batch, oraclePlan := readyOrchestratorWorker(t)
			test.mutate(t, batch)
			_, err := transferOrchestratorWorkerBatch(OrchestratorWorkerTransferRequest{Plan: plan, Lease: lease, SourceBatchRoot: batch, EvidenceRoot: filepath.Join(t.TempDir(), "evidence"), OraclePlanPath: oraclePlan}, func(string, string, string) error { return nil }, nil)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("transfer error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestOrchestratorWorkerRunsCoordinatorReconciliationValidationBeforeCredit(t *testing.T) {
	orchestrator, plan, lease, now, batch, oraclePlan := readyOrchestratorWorker(t)
	request := OrchestratorWorkerRequest{Plan: plan, Lease: lease, HubAlias: "hub-a", HubCapacity: 1, AllocationAlias: "scratch-validation", EvidenceRoot: filepath.Join(t.TempDir(), "evidence"), OraclePlanPath: oraclePlan}
	_, err := runOrchestratorWorkerOnce(context.Background(), orchestrator, request, fixedOrchestratorWorkerClock(now), func(context.Context, OrchestratorWorkerRequest) (OrchestratorWorkerRunResult, error) {
		return OrchestratorWorkerRunResult{BatchRoot: batch}, nil
	}, func(string, string, string) error { return errors.New("reconciliation rejected") }, func() error { return nil })
	if err == nil || !strings.Contains(err.Error(), "reconciliation") {
		t.Fatalf("worker validation error = %v", err)
	}
	var credit int
	if err := orchestrator.db.QueryRow(`SELECT count(*) FROM proof_credits WHERE campaign_id = ?`, plan.CampaignID).Scan(&credit); err != nil || credit != 0 {
		t.Fatalf("credit=%d, err=%v", credit, err)
	}
	var cleanupState string
	if err := orchestrator.db.QueryRow(`SELECT state FROM cleanup_journal WHERE campaign_id = ? AND job_id = ? AND generation = ?`, plan.CampaignID, lease.JobID, lease.Generation).Scan(&cleanupState); err != nil || cleanupState != "pending" {
		t.Fatalf("cleanup state=%q, err=%v", cleanupState, err)
	}
}

func TestOrchestratorWorkerRenewsLeaseDuringLongRunner(t *testing.T) {
	orchestrator, plan, lease, _, batch, oraclePlan := readyOrchestratorWorker(t)
	start := time.Now().UTC()
	originalUntil := start.Add(90 * time.Millisecond)
	lease.LeaseUntil = originalUntil
	lease.DurationMS = 90
	if _, err := orchestrator.db.Exec(`UPDATE jobs SET lease_until = ? WHERE campaign_id = ? AND id = ? AND generation = ?`, originalUntil.UnixMilli(), plan.CampaignID, lease.JobID, lease.Generation); err != nil {
		t.Fatal(err)
	}
	if _, err := orchestrator.db.Exec(`UPDATE attempts SET lease_until = ? WHERE campaign_id = ? AND job_id = ? AND generation = ?`, originalUntil.UnixMilli(), plan.CampaignID, lease.JobID, lease.Generation); err != nil {
		t.Fatal(err)
	}
	if _, err := orchestrator.db.Exec(`UPDATE lease_terms SET duration_ms = ? WHERE campaign_id = ? AND job_id = ? AND generation = ?`, lease.DurationMS, plan.CampaignID, lease.JobID, lease.Generation); err != nil {
		t.Fatal(err)
	}
	request := OrchestratorWorkerRequest{
		Plan: plan, Lease: lease, HubAlias: "hub-a", HubCapacity: 1,
		AllocationAlias: "scratch-heartbeat", EvidenceRoot: filepath.Join(t.TempDir(), "evidence"), OraclePlanPath: oraclePlan,
	}
	if _, err := runOrchestratorWorkerOnce(context.Background(), orchestrator, request, func() time.Time { return time.Now().UTC() }, func(context.Context, OrchestratorWorkerRequest) (OrchestratorWorkerRunResult, error) {
		time.Sleep(180 * time.Millisecond)
		return OrchestratorWorkerRunResult{BatchRoot: batch}, nil
	}, func(string, string, string) error { return nil }, func() error { return nil }); err != nil {
		t.Fatalf("long worker failed: %v", err)
	}
	var renewedMS int64
	if err := orchestrator.db.QueryRow(`SELECT lease_until FROM jobs WHERE campaign_id = ? AND id = ?`, plan.CampaignID, lease.JobID).Scan(&renewedMS); err != nil {
		t.Fatal(err)
	}
	if renewedMS <= originalUntil.UnixMilli() {
		t.Fatalf("lease was not renewed: got %d, original %d", renewedMS, originalUntil.UnixMilli())
	}
}

func TestOrchestratorWorkerRejectsMutatedLeaseDuration(t *testing.T) {
	orchestrator, plan, lease, now, batch, oraclePlan := readyOrchestratorWorker(t)
	lease.DurationMS++
	runnerCalled := false
	_, err := runOrchestratorWorkerOnce(context.Background(), orchestrator, OrchestratorWorkerRequest{
		Plan: plan, Lease: lease, HubAlias: "hub-a", HubCapacity: 1,
		AllocationAlias: "scratch-mutated-duration", EvidenceRoot: filepath.Join(t.TempDir(), "evidence"), OraclePlanPath: oraclePlan,
	}, fixedOrchestratorWorkerClock(now), func(context.Context, OrchestratorWorkerRequest) (OrchestratorWorkerRunResult, error) {
		runnerCalled = true
		return OrchestratorWorkerRunResult{BatchRoot: batch}, nil
	}, func(string, string, string) error { return nil }, func() error { return nil })
	if err == nil || !strings.Contains(err.Error(), "wrapper") {
		t.Fatalf("mutated duration error = %v", err)
	}
	if runnerCalled {
		t.Fatal("runner called with mutated lease duration")
	}
}

func TestOrchestratorWorkerRetryClaimsOnlyItsCleanupJournal(t *testing.T) {
	orchestrator, plan, first, now, batch, oraclePlan := readyOrchestratorWorker(t)
	if err := orchestrator.SetHubCapacity("hub-a", 2); err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.Reserve(first, "hub-a", "scratch-first", now); err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.recordWorkerFailure(first, orchestratorWorkerWrapperFailed, now); err != nil {
		t.Fatal(err)
	}
	second, err := orchestrator.Lease(plan.CampaignID, "worker-a", now.Add(2*time.Minute), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	bindingPath := filepath.Join(batch, "evidence", "ORCHESTRATOR_BINDING.json")
	if err := os.Remove(bindingPath); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteOrchestratorBatchBinding(bindingPath, plan, second); err != nil {
		t.Fatal(err)
	}
	request := OrchestratorWorkerRequest{
		Plan: plan, Lease: second, HubAlias: "hub-a", HubCapacity: 2,
		AllocationAlias: "scratch-second", EvidenceRoot: filepath.Join(t.TempDir(), "evidence"), OraclePlanPath: oraclePlan,
	}
	if _, err := runOrchestratorWorkerOnce(context.Background(), orchestrator, request, fixedOrchestratorWorkerClock(now.Add(2*time.Minute)), func(context.Context, OrchestratorWorkerRequest) (OrchestratorWorkerRunResult, error) {
		return OrchestratorWorkerRunResult{BatchRoot: batch}, nil
	}, func(string, string, string) error { return nil }, func() error { return nil }); err != nil {
		t.Fatalf("retry worker failed: %v", err)
	}
	states := map[string]string{}
	rows, err := orchestrator.db.Query(`SELECT allocation_alias, state FROM cleanup_journal WHERE campaign_id = ? ORDER BY allocation_alias`, plan.CampaignID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var alias, state string
		if err := rows.Scan(&alias, &state); err != nil {
			t.Fatal(err)
		}
		states[alias] = state
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if states["scratch-first"] != "pending" || states["scratch-second"] != "closed" {
		t.Fatalf("cleanup states = %#v", states)
	}
	var openActions int
	if err := orchestrator.db.QueryRow(`SELECT count(*) FROM actions WHERE campaign_id = ? AND state = 'open'`, plan.CampaignID).Scan(&openActions); err != nil || openActions != 1 {
		t.Fatalf("open actions=%d, err=%v", openActions, err)
	}
}

func TestOrchestratorWorkerSuccessfulRetryClosesResolvedPriorGenerationAction(t *testing.T) {
	orchestrator, plan, first, now, batch, oraclePlan := readyOrchestratorWorker(t)
	evidenceRoot := filepath.Join(t.TempDir(), "evidence")
	_, err := runOrchestratorWorkerOnce(context.Background(), orchestrator, OrchestratorWorkerRequest{
		Plan: plan, Lease: first, HubAlias: "hub-a", HubCapacity: 1,
		AllocationAlias: "scratch-first", EvidenceRoot: evidenceRoot, OraclePlanPath: oraclePlan,
	}, fixedOrchestratorWorkerClock(now), func(context.Context, OrchestratorWorkerRequest) (OrchestratorWorkerRunResult, error) {
		return OrchestratorWorkerRunResult{BatchRoot: batch}, nil
	}, func(string, string, string) error { return nil }, func() error { return errors.New("crash before credit") })
	if err == nil || !strings.Contains(err.Error(), "credit") {
		t.Fatalf("first generation error = %v", err)
	}
	secondNow := now.Add(2 * time.Minute)
	second, err := orchestrator.Lease(plan.CampaignID, "worker-b", secondNow, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if second.JobID != first.JobID || second.Generation != first.Generation+1 {
		t.Fatalf("retry lease = %#v, want retry of %#v", second, first)
	}
	bindingPath := filepath.Join(batch, "evidence", "ORCHESTRATOR_BINDING.json")
	if err := os.Remove(bindingPath); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteOrchestratorBatchBinding(bindingPath, plan, second); err != nil {
		t.Fatal(err)
	}
	if _, err := runOrchestratorWorkerOnce(context.Background(), orchestrator, OrchestratorWorkerRequest{
		Plan: plan, Lease: second, HubAlias: "hub-a", HubCapacity: 1,
		AllocationAlias: "scratch-second", EvidenceRoot: evidenceRoot, OraclePlanPath: oraclePlan,
	}, fixedOrchestratorWorkerClock(secondNow), func(context.Context, OrchestratorWorkerRequest) (OrchestratorWorkerRunResult, error) {
		return OrchestratorWorkerRunResult{BatchRoot: batch}, nil
	}, func(string, string, string) error { return nil }, func() error { return nil }); err != nil {
		t.Fatalf("second generation failed: %v", err)
	}
	var openActions int
	if err := orchestrator.db.QueryRow(`SELECT count(*) FROM actions WHERE campaign_id = ? AND state = 'open'`, plan.CampaignID).Scan(&openActions); err != nil || openActions != 0 {
		t.Fatalf("open actions=%d, err=%v", openActions, err)
	}
}

func fixedOrchestratorWorkerClock(now time.Time) func() time.Time {
	return func() time.Time { return now }
}

func readyOrchestratorWorker(t *testing.T) (*Orchestrator, OrchestratorCampaignPlan, OrchestratorLease, time.Time, string, string) {
	t.Helper()
	root := t.TempDir()
	scope, batch := writeSurfaceOracleIndexInputs(t, root)
	oraclePlan := filepath.Join(root, "ORACLE_PLAN.json")
	writeJSONValue(t, oraclePlan, map[string]any{"schemaVersion": 1})
	writeSyntheticOrchestratorReconciliation(t, batch)
	definition := testOrchestratorDefinition(t, scope, [2][]string{{"apex:System.One", "apex:System.Two"}, {"apex:System.Three"}})
	definition.Candidate = OrchestratorArtifact{Commit: strings.Repeat("1", 40), SHA256: surfaceOracleFileSHA256(t, filepath.Join(batch, "bin", "glade-sealed"))}
	definition.Tools = OrchestratorArtifact{Commit: strings.Repeat("2", 40), SHA256: surfaceOracleFileSHA256(t, filepath.Join(batch, "bin", "glade-tools"))}
	definition.ControlledInputSHA256["oracle-plan"] = surfaceOracleFileSHA256(t, oraclePlan)
	plan, err := PlanOrchestratorCampaign(definition)
	if err != nil {
		t.Fatal(err)
	}
	orchestrator := openTestOrchestrator(t)
	if err := orchestrator.InitCampaign(plan); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	lease, err := orchestrator.Lease(plan.CampaignID, "worker-a", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := WriteOrchestratorBatchBinding(filepath.Join(batch, "evidence", "ORCHESTRATOR_BINDING.json"), plan, lease); err != nil {
		t.Fatal(err)
	}
	return orchestrator, plan, lease, now, batch, oraclePlan
}

func writeSyntheticOrchestratorReconciliation(t *testing.T, batch string) {
	t.Helper()
	packet := filepath.Join(batch, "evidence", "salesforce-reconciliation-packet")
	if err := os.MkdirAll(packet, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(packet, reconciliationPacketManifestName)
	writeJSONValue(t, manifest, reportPacketManifest{SchemaVersion: 1})
	writeJSONValue(t, filepath.Join(batch, "evidence", "SALESFORCE_RECONCILIATION.json"), SalesforceReconciliation{SchemaVersion: 1, PacketManifestSHA256: surfaceOracleFileSHA256(t, manifest)})
}
