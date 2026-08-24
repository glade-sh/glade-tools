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
	observeReadyHub(t, orchestrator, "hub-a", time.Now().UTC())
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
	observeReadyHub(t, orchestrator, "hub-a", now)
	if err := orchestrator.Reserve(first, "hub-a", "scratch-first", now); err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.recordWorkerFailure(first, orchestratorWorkerWrapperFailed, now); err != nil {
		t.Fatal(err)
	}
	two := 2
	if err := orchestrator.ObserveHub(OrchestratorHubObservation{HubAlias: "hub-a", ObservedAt: now.Add(2 * time.Minute), Healthy: true, DailyScratchOrgsRemaining: &two, ActiveScratchOrgsRemaining: &two}); err != nil {
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
	observeReadyHub(t, orchestrator, "hub-a", secondNow)
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

func TestOrchestratorCleanupTakeoverClosesExactAction(t *testing.T) {
	orchestrator, plan, lease, now, _, _ := readyOrchestratorWorker(t)
	if err := orchestrator.SetHubCapacity("hub-a", 1); err != nil {
		t.Fatal(err)
	}
	observeReadyHub(t, orchestrator, "hub-a", now)
	if err := orchestrator.Reserve(lease, "hub-a", "scratch-takeover", now); err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.recordWorkerFailure(lease, orchestratorWorkerCleanupFailed, now); err != nil {
		t.Fatal(err)
	}
	claimNow := now.Add(2 * time.Minute)
	claim, err := orchestrator.ClaimCleanup(plan.CampaignID, "worker-b", claimNow, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.CloseCleanup(claim, claimNow); err != nil {
		t.Fatal(err)
	}
	var openActions int
	if err := orchestrator.db.QueryRow(`SELECT count(*) FROM actions WHERE campaign_id = ? AND state = 'open'`, plan.CampaignID).Scan(&openActions); err != nil || openActions != 0 {
		t.Fatalf("open actions=%d, err=%v", openActions, err)
	}
}

func TestOrchestratorCleanupTakeoverRunsTypedSalesforceCleanupBeforeClosingJournal(t *testing.T) {
	orchestrator, plan, lease, now, _, _ := readyOrchestratorWorker(t)
	if err := orchestrator.SetHubCapacity("sealed-dev-hub", 1); err != nil {
		t.Fatal(err)
	}
	observeReadyHub(t, orchestrator, "sealed-dev-hub", now)
	if err := orchestrator.Reserve(lease, "sealed-dev-hub", "scratch-takeover-typed", now); err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.recordWorkerFailure(lease, orchestratorWorkerCleanupFailed, now); err != nil {
		t.Fatal(err)
	}
	claim, err := orchestrator.ClaimCleanup(plan.CampaignID, "worker-b", now.Add(2*time.Minute), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	root := t.TempDir()
	cleanupRequest := OrchestratorCleanupTakeoverRequest{Claim: claim, BundlePath: filepath.Join(root, "bundle.json"), CreationPath: filepath.Join(root, "creation.json"), PreflightPath: filepath.Join(root, "preflight.json"), TargetOrg: "scratch-takeover-typed", SFBin: filepath.Join(root, "sf"), OutputPath: filepath.Join(root, "cleanup.json")}
	var got SalesforceOrgCleanupRequest
	if err := runOrchestratorCleanupTakeover(orchestrator, cleanupRequest, now.Add(2*time.Minute), func(request SalesforceOrgCleanupRequest) (SalesforceOrgCleanup, error) {
		called = true
		got = request
		writeValidCleanupTakeoverFiles(t, root, cleanupRequest.TargetOrg)
		cleanup, _, err := readExactJSONBytes[SalesforceOrgCleanup](cleanupRequest.OutputPath)
		return cleanup, err
	}); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("typed Salesforce cleanup was not called")
	}
	if got.BundlePath != cleanupRequest.BundlePath || got.CreationPath != cleanupRequest.CreationPath || got.PreflightPath != cleanupRequest.PreflightPath || got.TargetOrg != cleanupRequest.TargetOrg || got.SFBin != cleanupRequest.SFBin || got.OutputPath != cleanupRequest.OutputPath {
		t.Fatalf("cleanup request propagation = %#v", got)
	}
	var state string
	if err := orchestrator.db.QueryRow(`SELECT state FROM cleanup_journal WHERE campaign_id = ? AND job_id = ? AND generation = ?`, plan.CampaignID, lease.JobID, lease.Generation).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "closed" {
		t.Fatalf("cleanup state=%q, want closed", state)
	}
}

func TestOrchestratorCleanupTakeoverKeepsJournalOpenWhenResidueRemains(t *testing.T) {
	orchestrator, plan, lease, now, _, _ := readyOrchestratorWorker(t)
	if err := orchestrator.SetHubCapacity("hub-a", 1); err != nil {
		t.Fatal(err)
	}
	observeReadyHub(t, orchestrator, "hub-a", now)
	if err := orchestrator.Reserve(lease, "hub-a", "scratch-takeover-failed", now); err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.recordWorkerFailure(lease, orchestratorWorkerCleanupFailed, now); err != nil {
		t.Fatal(err)
	}
	claim, err := orchestrator.ClaimCleanup(plan.CampaignID, "worker-b", now.Add(2*time.Minute), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	request := OrchestratorCleanupTakeoverRequest{Claim: claim, BundlePath: filepath.Join(root, "bundle.json"), CreationPath: filepath.Join(root, "creation.json"), PreflightPath: filepath.Join(root, "preflight.json"), TargetOrg: "scratch-takeover-failed", SFBin: filepath.Join(root, "sf"), OutputPath: filepath.Join(root, "cleanup.json")}
	if err := runOrchestratorCleanupTakeover(orchestrator, request, now.Add(2*time.Minute), func(SalesforceOrgCleanupRequest) (SalesforceOrgCleanup, error) {
		writeValidCleanupTakeoverFiles(t, root, request.TargetOrg)
		cleanup, _, err := readExactJSONBytes[SalesforceOrgCleanup](request.OutputPath)
		if err := os.Remove(request.OutputPath); err != nil {
			return SalesforceOrgCleanup{}, err
		}
		cleanup.ResidueAbsent = false
		if err == nil {
			err = WriteNewJSON(request.OutputPath, cleanup)
		}
		return cleanup, err
	}); err == nil {
		t.Fatal("cleanup takeover succeeded while residue remained")
	}
	var state string
	if err := orchestrator.db.QueryRow(`SELECT state FROM cleanup_journal WHERE allocation_alias = ?`, claim.AllocationAlias).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state == "closed" {
		t.Fatal("cleanup journal closed after typed cleanup failure")
	}
}

func TestOrchestratorCleanupTakeoverRequiresWrittenReceiptAfterCallback(t *testing.T) {
	orchestrator, plan, lease, now, _, _ := readyOrchestratorWorker(t)
	if err := orchestrator.SetHubCapacity("hub-a", 1); err != nil {
		t.Fatal(err)
	}
	observeReadyHub(t, orchestrator, "hub-a", now)
	if err := orchestrator.Reserve(lease, "hub-a", "scratch-receipt-required", now); err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.recordWorkerFailure(lease, orchestratorWorkerCleanupFailed, now); err != nil {
		t.Fatal(err)
	}
	claim, err := orchestrator.ClaimCleanup(plan.CampaignID, "worker-b", now.Add(2*time.Minute), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	request := OrchestratorCleanupTakeoverRequest{Claim: claim, BundlePath: filepath.Join(root, "bundle.json"), CreationPath: filepath.Join(root, "creation.json"), PreflightPath: filepath.Join(root, "preflight.json"), TargetOrg: claim.AllocationAlias, SFBin: filepath.Join(root, "sf"), OutputPath: filepath.Join(root, "cleanup.json")}
	if err := runOrchestratorCleanupTakeover(orchestrator, request, now.Add(2*time.Minute), func(SalesforceOrgCleanupRequest) (SalesforceOrgCleanup, error) {
		return SalesforceOrgCleanup{ResidueAbsent: true}, nil
	}); err == nil {
		t.Fatal("cleanup takeover closed without a written receipt")
	}
	var state string
	if err := orchestrator.db.QueryRow(`SELECT state FROM cleanup_journal WHERE allocation_alias = ?`, claim.AllocationAlias).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state == "closed" {
		t.Fatal("cleanup journal closed without a written receipt")
	}
}

func TestOrchestratorCleanupTakeoverRejectsTargetAliasMismatch(t *testing.T) {
	orchestrator, plan, lease, now, _, _ := readyOrchestratorWorker(t)
	if err := orchestrator.SetHubCapacity("hub-a", 1); err != nil {
		t.Fatal(err)
	}
	observeReadyHub(t, orchestrator, "hub-a", now)
	if err := orchestrator.Reserve(lease, "hub-a", "scratch-claim-a", now); err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.recordWorkerFailure(lease, orchestratorWorkerCleanupFailed, now); err != nil {
		t.Fatal(err)
	}
	claim, err := orchestrator.ClaimCleanup(plan.CampaignID, "worker-b", now.Add(2*time.Minute), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	called := false
	request := OrchestratorCleanupTakeoverRequest{Claim: claim, BundlePath: filepath.Join(root, "bundle.json"), CreationPath: filepath.Join(root, "creation.json"), TargetOrg: "scratch-claim-b", SFBin: filepath.Join(root, "sf"), OutputPath: filepath.Join(root, "cleanup.json")}
	if err := runOrchestratorCleanupTakeover(orchestrator, request, now.Add(2*time.Minute), func(SalesforceOrgCleanupRequest) (SalesforceOrgCleanup, error) {
		called = true
		return SalesforceOrgCleanup{ResidueAbsent: true}, nil
	}); err == nil {
		t.Fatal("target alias mismatch succeeded")
	}
	if called {
		t.Fatal("typed cleanup called for target alias mismatch")
	}
	var state string
	if err := orchestrator.db.QueryRow(`SELECT state FROM cleanup_journal WHERE allocation_alias = ?`, claim.AllocationAlias).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state == "closed" {
		t.Fatal("claim A journal closed for target B")
	}
}

func TestOrchestratorCleanupTakeoverRejectsExpiredOrForgedClaimsBeforeCleanup(t *testing.T) {
	for _, name := range []string{"expired", "forged"} {
		t.Run(name, func(t *testing.T) {
			orchestrator, plan, lease, now, _, _ := readyOrchestratorWorker(t)
			if err := orchestrator.SetHubCapacity("hub-a", 1); err != nil {
				t.Fatal(err)
			}
			observeReadyHub(t, orchestrator, "hub-a", now)
			if err := orchestrator.Reserve(lease, "hub-a", "scratch-claim-check", now); err != nil {
				t.Fatal(err)
			}
			if err := orchestrator.recordWorkerFailure(lease, orchestratorWorkerCleanupFailed, now); err != nil {
				t.Fatal(err)
			}
			claim, err := orchestrator.ClaimCleanup(plan.CampaignID, "worker-b", now.Add(2*time.Minute), time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			checkNow := now.Add(2 * time.Minute)
			if name == "expired" {
				checkNow = claim.ClaimUntil.Add(time.Nanosecond)
			} else {
				claim.HubAlias = "hub-forged"
			}
			root := t.TempDir()
			called := false
			request := OrchestratorCleanupTakeoverRequest{Claim: claim, BundlePath: filepath.Join(root, "bundle.json"), CreationPath: filepath.Join(root, "creation.json"), PreflightPath: filepath.Join(root, "preflight.json"), TargetOrg: claim.AllocationAlias, SFBin: filepath.Join(root, "sf"), OutputPath: filepath.Join(root, "cleanup.json")}
			if err := runOrchestratorCleanupTakeover(orchestrator, request, checkNow, func(SalesforceOrgCleanupRequest) (SalesforceOrgCleanup, error) {
				called = true
				return SalesforceOrgCleanup{ResidueAbsent: true}, nil
			}); err == nil {
				t.Fatal("invalid claim succeeded")
			}
			if called {
				t.Fatal("typed cleanup called for invalid claim")
			}
			var state string
			if err := orchestrator.db.QueryRow(`SELECT state FROM cleanup_journal WHERE allocation_alias = ?`, claim.AllocationAlias).Scan(&state); err != nil {
				t.Fatal(err)
			}
			if state == "closed" {
				t.Fatal("invalid claim closed cleanup journal")
			}
		})
	}
}

func TestOrchestratorCleanupTakeoverRequiresPreflightReceipt(t *testing.T) {
	orchestrator, plan, lease, now, _, _ := readyOrchestratorWorker(t)
	if err := orchestrator.SetHubCapacity("hub-a", 1); err != nil {
		t.Fatal(err)
	}
	observeReadyHub(t, orchestrator, "hub-a", now)
	if err := orchestrator.Reserve(lease, "hub-a", "scratch-preflight-required", now); err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.recordWorkerFailure(lease, orchestratorWorkerCleanupFailed, now); err != nil {
		t.Fatal(err)
	}
	claim, err := orchestrator.ClaimCleanup(plan.CampaignID, "worker-b", now.Add(2*time.Minute), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	called := false
	request := OrchestratorCleanupTakeoverRequest{Claim: claim, BundlePath: filepath.Join(root, "bundle.json"), CreationPath: filepath.Join(root, "creation.json"), TargetOrg: claim.AllocationAlias, SFBin: filepath.Join(root, "sf"), OutputPath: filepath.Join(root, "cleanup.json")}
	if err := runOrchestratorCleanupTakeover(orchestrator, request, now.Add(2*time.Minute), func(SalesforceOrgCleanupRequest) (SalesforceOrgCleanup, error) {
		called = true
		return SalesforceOrgCleanup{ResidueAbsent: true}, nil
	}); err == nil {
		t.Fatal("cleanup without preflight succeeded")
	}
	if called {
		t.Fatal("typed cleanup called without preflight")
	}
}

func TestOrchestratorCleanupTakeoverValidReceiptSkipsCleanupAndCloses(t *testing.T) {
	orchestrator, plan, lease, now, _, _ := readyOrchestratorWorker(t)
	if err := orchestrator.SetHubCapacity("sealed-dev-hub", 1); err != nil {
		t.Fatal(err)
	}
	observeReadyHub(t, orchestrator, "sealed-dev-hub", now)
	if err := orchestrator.Reserve(lease, "sealed-dev-hub", "scratch-existing-receipt", now); err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.recordWorkerFailure(lease, orchestratorWorkerCleanupFailed, now); err != nil {
		t.Fatal(err)
	}
	claim, err := orchestrator.ClaimCleanup(plan.CampaignID, "worker-b", now.Add(2*time.Minute), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	bundlePath, creationPath, preflightPath, outputPath := writeValidCleanupTakeoverFiles(t, root, claim.AllocationAlias)
	called := false
	request := OrchestratorCleanupTakeoverRequest{Claim: claim, BundlePath: bundlePath, CreationPath: creationPath, PreflightPath: preflightPath, TargetOrg: claim.AllocationAlias, SFBin: salesforceCLIPath, OutputPath: outputPath}
	if err := validateExistingOrchestratorCleanup(request); err != nil {
		t.Fatal(err)
	}
	if err := runOrchestratorCleanupTakeover(orchestrator, request, now.Add(2*time.Minute), func(SalesforceOrgCleanupRequest) (SalesforceOrgCleanup, error) {
		called = true
		return SalesforceOrgCleanup{}, errors.New("must not delete twice")
	}); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("typed cleanup reran for valid existing receipt")
	}
	var state string
	if err := orchestrator.db.QueryRow(`SELECT state FROM cleanup_journal WHERE allocation_alias = ?`, claim.AllocationAlias).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "closed" {
		t.Fatalf("cleanup state=%q, want closed", state)
	}
}

func TestOrchestratorCleanupTakeoverReplaysRecoveredExitZeroTimeoutReceipt(t *testing.T) {
	orchestrator, plan, lease, now, _, _ := readyOrchestratorWorker(t)
	if err := orchestrator.SetHubCapacity("sealed-dev-hub", 1); err != nil {
		t.Fatal(err)
	}
	observeReadyHub(t, orchestrator, "sealed-dev-hub", now)
	if err := orchestrator.Reserve(lease, "sealed-dev-hub", "scratch-recovered-replay", now); err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.recordWorkerFailure(lease, orchestratorWorkerCleanupFailed, now); err != nil {
		t.Fatal(err)
	}
	claim, err := orchestrator.ClaimCleanup(plan.CampaignID, "worker-b", now.Add(2*time.Minute), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	bundlePath, creationPath, preflightPath, outputPath := writeValidCleanupTakeoverFiles(t, root, claim.AllocationAlias)
	cleanup, _, err := readExactJSONBytes[SalesforceOrgCleanup](outputPath)
	if err != nil {
		t.Fatal(err)
	}
	cleanup.RecoveredAbsent = true
	cleanup.Commands[0].Passed = false
	cleanup.Commands[0].ExitCode = 0
	cleanup.Commands[0].TimedOut = true
	query := salesforceCommandForTest(t, bundlePath, []string{"data", "query", "--target-org", cleanup.DevHub, "--query", salesforceActiveScratchOrgQuery(cleanup.OrgID), "--json"})
	query.Output.Stdout = []byte(`{"status":0,"result":{"totalSize":0,"records":[]}}`)
	query.StdoutSHA256 = replayBytesSHA256(query.Output.Stdout)
	cleanup.Commands = []CommandResult{cleanup.Commands[0], query}
	if err := os.Remove(outputPath); err != nil {
		t.Fatal(err)
	}
	if err := WriteNewJSON(outputPath, cleanup); err != nil {
		t.Fatal(err)
	}
	request := OrchestratorCleanupTakeoverRequest{Claim: claim, BundlePath: bundlePath, CreationPath: creationPath, PreflightPath: preflightPath, TargetOrg: claim.AllocationAlias, SFBin: salesforceCLIPath, OutputPath: outputPath}
	called := false
	if err := runOrchestratorCleanupTakeover(orchestrator, request, now.Add(2*time.Minute), func(SalesforceOrgCleanupRequest) (SalesforceOrgCleanup, error) {
		called = true
		return SalesforceOrgCleanup{}, errors.New("must not delete twice")
	}); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("typed cleanup reran for recovered receipt")
	}
}

func TestOrchestratorCleanupTakeoverReclaimsExpiredClaimWithExistingReceipt(t *testing.T) {
	orchestrator, plan, lease, now, _, _ := readyOrchestratorWorker(t)
	if err := orchestrator.SetHubCapacity("sealed-dev-hub", 1); err != nil {
		t.Fatal(err)
	}
	observeReadyHub(t, orchestrator, "sealed-dev-hub", now)
	if err := orchestrator.Reserve(lease, "sealed-dev-hub", "scratch-replay-after-expiry", now); err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.recordWorkerFailure(lease, orchestratorWorkerCleanupFailed, now); err != nil {
		t.Fatal(err)
	}
	claim, err := orchestrator.ClaimCleanup(plan.CampaignID, "worker-b", now.Add(2*time.Minute), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	request := OrchestratorCleanupTakeoverRequest{Claim: claim, BundlePath: filepath.Join(root, "bundle.json"), CreationPath: filepath.Join(root, "creation.json"), PreflightPath: filepath.Join(root, "preflight.json"), TargetOrg: claim.AllocationAlias, SFBin: salesforceCLIPath, OutputPath: filepath.Join(root, "cleanup.json")}
	closeAfterExpiry := claim.ClaimUntil.Add(time.Second)
	clockCalls := 0
	err = runOrchestratorCleanupTakeoverAt(orchestrator, request, func() time.Time {
		clockCalls++
		if clockCalls == 1 {
			return now.Add(2 * time.Minute)
		}
		return closeAfterExpiry
	}, func(SalesforceOrgCleanupRequest) (SalesforceOrgCleanup, error) {
		writeValidCleanupTakeoverFiles(t, root, claim.AllocationAlias)
		cleanup, _, err := readExactJSONBytes[SalesforceOrgCleanup](request.OutputPath)
		return cleanup, err
	})
	if err == nil {
		t.Fatal("cleanup takeover succeeded after claim expiry")
	}
	if clockCalls != 2 {
		t.Fatalf("clock calls=%d, want preflight and close", clockCalls)
	}
	var state string
	if err := orchestrator.db.QueryRow(`SELECT state FROM cleanup_journal WHERE allocation_alias = ?`, claim.AllocationAlias).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state == "closed" {
		t.Fatal("expired claim closed cleanup journal")
	}
	reclaimNow := closeAfterExpiry.Add(time.Second)
	reclaimed, err := orchestrator.ClaimCleanup(plan.CampaignID, "worker-c", reclaimNow, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	request.Claim = reclaimed
	if err := runOrchestratorCleanupTakeover(orchestrator, request, reclaimNow, func(SalesforceOrgCleanupRequest) (SalesforceOrgCleanup, error) {
		called = true
		return SalesforceOrgCleanup{}, errors.New("must replay existing cleanup")
	}); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("typed cleanup reran after reclaim")
	}
	if err := orchestrator.db.QueryRow(`SELECT state FROM cleanup_journal WHERE allocation_alias = ?`, claim.AllocationAlias).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "closed" {
		t.Fatalf("cleanup state=%q, want closed after reclaim", state)
	}
}

func TestOrchestratorCleanupTakeoverRejectsSymlinkedExistingReceipt(t *testing.T) {
	orchestrator, plan, lease, now, _, _ := readyOrchestratorWorker(t)
	if err := orchestrator.SetHubCapacity("sealed-dev-hub", 1); err != nil {
		t.Fatal(err)
	}
	observeReadyHub(t, orchestrator, "sealed-dev-hub", now)
	if err := orchestrator.Reserve(lease, "sealed-dev-hub", "scratch-symlinked-receipt", now); err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.recordWorkerFailure(lease, orchestratorWorkerCleanupFailed, now); err != nil {
		t.Fatal(err)
	}
	claim, err := orchestrator.ClaimCleanup(plan.CampaignID, "worker-b", now.Add(2*time.Minute), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	bundlePath, creationPath, preflightPath, outputPath := writeValidCleanupTakeoverFiles(t, root, claim.AllocationAlias)
	receiptPath := filepath.Join(root, "cleanup-real.json")
	if err := os.Rename(outputPath, receiptPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(receiptPath, outputPath); err != nil {
		t.Fatal(err)
	}
	called := false
	request := OrchestratorCleanupTakeoverRequest{Claim: claim, BundlePath: bundlePath, CreationPath: creationPath, PreflightPath: preflightPath, TargetOrg: claim.AllocationAlias, SFBin: salesforceCLIPath, OutputPath: outputPath}
	if err := runOrchestratorCleanupTakeover(orchestrator, request, now.Add(2*time.Minute), func(SalesforceOrgCleanupRequest) (SalesforceOrgCleanup, error) {
		called = true
		return SalesforceOrgCleanup{ResidueAbsent: true}, nil
	}); err == nil {
		t.Fatal("symlinked existing receipt succeeded")
	}
	if called {
		t.Fatal("typed cleanup called for symlinked existing receipt")
	}
	var state string
	if err := orchestrator.db.QueryRow(`SELECT state FROM cleanup_journal WHERE allocation_alias = ?`, claim.AllocationAlias).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state == "closed" {
		t.Fatal("symlinked existing receipt closed cleanup journal")
	}
}

func TestOrchestratorCleanupTakeoverRejectsWorldReadableExistingReceipt(t *testing.T) {
	orchestrator, plan, lease, now, _, _ := readyOrchestratorWorker(t)
	if err := orchestrator.SetHubCapacity("sealed-dev-hub", 1); err != nil {
		t.Fatal(err)
	}
	observeReadyHub(t, orchestrator, "sealed-dev-hub", now)
	if err := orchestrator.Reserve(lease, "sealed-dev-hub", "scratch-world-readable-receipt", now); err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.recordWorkerFailure(lease, orchestratorWorkerCleanupFailed, now); err != nil {
		t.Fatal(err)
	}
	claim, err := orchestrator.ClaimCleanup(plan.CampaignID, "worker-b", now.Add(2*time.Minute), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	bundlePath, creationPath, preflightPath, outputPath := writeValidCleanupTakeoverFiles(t, root, claim.AllocationAlias)
	if err := os.Chmod(outputPath, 0o644); err != nil {
		t.Fatal(err)
	}
	called := false
	request := OrchestratorCleanupTakeoverRequest{Claim: claim, BundlePath: bundlePath, CreationPath: creationPath, PreflightPath: preflightPath, TargetOrg: claim.AllocationAlias, SFBin: salesforceCLIPath, OutputPath: outputPath}
	if err := runOrchestratorCleanupTakeover(orchestrator, request, now.Add(2*time.Minute), func(SalesforceOrgCleanupRequest) (SalesforceOrgCleanup, error) {
		called = true
		return SalesforceOrgCleanup{ResidueAbsent: true}, nil
	}); err == nil {
		t.Fatal("world-readable existing receipt succeeded")
	}
	if called {
		t.Fatal("typed cleanup called for world-readable existing receipt")
	}
}

func TestOrchestratorCleanupTakeoverRejectsInvalidExistingReceipt(t *testing.T) {
	orchestrator, plan, lease, now, _, _ := readyOrchestratorWorker(t)
	if err := orchestrator.SetHubCapacity("hub-a", 1); err != nil {
		t.Fatal(err)
	}
	observeReadyHub(t, orchestrator, "hub-a", now)
	if err := orchestrator.Reserve(lease, "hub-a", "scratch-invalid-receipt", now); err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.recordWorkerFailure(lease, orchestratorWorkerCleanupFailed, now); err != nil {
		t.Fatal(err)
	}
	claim, err := orchestrator.ClaimCleanup(plan.CampaignID, "worker-b", now.Add(2*time.Minute), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	outputPath := filepath.Join(root, "cleanup.json")
	if err := os.WriteFile(outputPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	called := false
	request := OrchestratorCleanupTakeoverRequest{Claim: claim, BundlePath: filepath.Join(root, "bundle.json"), CreationPath: filepath.Join(root, "creation.json"), PreflightPath: filepath.Join(root, "preflight.json"), TargetOrg: claim.AllocationAlias, SFBin: filepath.Join(root, "sf"), OutputPath: outputPath}
	if err := runOrchestratorCleanupTakeover(orchestrator, request, now.Add(2*time.Minute), func(SalesforceOrgCleanupRequest) (SalesforceOrgCleanup, error) {
		called = true
		return SalesforceOrgCleanup{ResidueAbsent: true}, nil
	}); err == nil {
		t.Fatal("invalid existing receipt succeeded")
	}
	if called {
		t.Fatal("typed cleanup called with invalid existing receipt")
	}
	var state string
	if err := orchestrator.db.QueryRow(`SELECT state FROM cleanup_journal WHERE allocation_alias = ?`, claim.AllocationAlias).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state == "closed" {
		t.Fatal("invalid existing receipt closed cleanup journal")
	}
}

func writeValidCleanupTakeoverFiles(t *testing.T, root, alias string) (string, string, string, string) {
	t.Helper()
	bundlePath := filepath.Join(root, "bundle.json")
	creationPath := filepath.Join(root, "creation.json")
	preflightPath := filepath.Join(root, "preflight.json")
	outputPath := filepath.Join(root, "cleanup.json")
	writeSyntheticDevHubBundle(t, bundlePath)
	bundleSHA := localProofFileSHA256(t, bundlePath)
	bundle, _, err := readExactJSONBytes[OracleBundle](bundlePath)
	if err != nil {
		t.Fatal(err)
	}
	creation := SalesforceOrgCreation{SchemaVersion: 1, BundleSHA256: bundleSHA, DevHub: bundle.DevHub, DevHubOrgID: bundle.DevHubOrgID, DevHubUsername: bundle.DevHubUsername, Alias: alias, Marker: testSalesforceScratchMarker, OrgID: testSalesforceCleanupOrgID, Command: salesforceCommandForTest(t, bundlePath, salesforceOrgCreateArgs(filepath.Join(root, "corpus-assurance-scratch-def.json"), bundle.DevHub, alias, testSalesforceScratchMarker)), DevHubCommand: salesforceCommandForTest(t, bundlePath, []string{"org", "display", "--target-org", bundle.DevHub, "--json"})}
	creation.Command.Output.Stdout = []byte(`{"status":0,"result":{"orgId":"` + testSalesforceCleanupOrgID + `"}}`)
	creation.Command.StdoutSHA256 = replayBytesSHA256(creation.Command.Output.Stdout)
	creation.DevHubCommand.Output.Stdout = []byte(`{"status":0,"result":{"id":"` + bundle.DevHubOrgID + `","status":"Active","username":"` + bundle.DevHubUsername + `"}}`)
	creation.DevHubCommand.StdoutSHA256 = replayBytesSHA256(creation.DevHubCommand.Output.Stdout)
	if err := WriteNewJSON(creationPath, creation); err != nil {
		t.Fatal(err)
	}
	preflight := salesforcePreflightForTest(t, alias, bundleSHA, bundlePath)
	preflight.OrgID = testSalesforceCleanupOrgID
	preflight.Commands[0].Output.Stdout = []byte(`{"status":0,"result":{"id":"` + testSalesforceCleanupOrgID + `","status":"Active","username":"` + alias + `@example.invalid"}}`)
	preflight.Commands[0].StdoutSHA256 = replayBytesSHA256(preflight.Commands[0].Output.Stdout)
	if err := WriteNewJSON(preflightPath, preflight); err != nil {
		t.Fatal(err)
	}
	deleted := salesforceCommandForTest(t, bundlePath, []string{"org", "delete", "scratch", "--target-org", alias, "--no-prompt", "--json"})
	absent := salesforceCommandForTest(t, bundlePath, []string{"org", "display", "--target-org", alias, "--json"})
	absent.ExitCode, absent.Passed = 2, false
	absent.Output.Stdout = []byte(`{"status":1,"message":"not found"}`)
	absent.StdoutSHA256 = replayBytesSHA256(absent.Output.Stdout)
	cleanup := SalesforceOrgCleanup{SchemaVersion: 1, BundleSHA256: bundleSHA, DevHub: bundle.DevHub, DevHubOrgID: bundle.DevHubOrgID, DevHubUsername: bundle.DevHubUsername, OrgAlias: alias, OrgID: testSalesforceCleanupOrgID, Commands: []CommandResult{deleted, absent}, DevHubCommand: salesforceCommandForTest(t, bundlePath, []string{"org", "display", "--target-org", bundle.DevHub, "--json"}), ResidueAbsent: true}
	cleanup.DevHubCommand.Output.Stdout = []byte(`{"status":0,"result":{"id":"` + bundle.DevHubOrgID + `","status":"Active","username":"` + bundle.DevHubUsername + `"}}`)
	cleanup.DevHubCommand.StdoutSHA256 = replayBytesSHA256(cleanup.DevHubCommand.Output.Stdout)
	if err := WriteNewJSON(outputPath, cleanup); err != nil {
		t.Fatal(err)
	}
	return bundlePath, creationPath, preflightPath, outputPath
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
	definition := testOrchestratorDefinition(t, scope, [][]string{{"apex:System.One", "apex:System.Two"}, {"apex:System.Three"}})
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
	observeReadyHub(t, orchestrator, "hub-a", now)
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
