package corpusassurance

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOrchestratorWorkerCleanupClosesEveryPreflightCrashStageAndReplays(t *testing.T) {
	for _, stage := range []string{"reservation-only", "invalidated-creation", "created-before-preflight"} {
		t.Run(stage, func(t *testing.T) {
			plan, lease, request, _ := workerCleanupTestRequest(t, stage)
			calls := 0
			request.cleanup = func(cleanupRequest SalesforceOrgCleanupRequest) (SalesforceOrgCleanup, error) {
				calls++
				cleanup := SalesforceOrgCleanup{SchemaVersion: 1, ResidueAbsent: true}
				if err := WriteNewJSON(cleanupRequest.OutputPath, cleanup); err != nil {
					return SalesforceOrgCleanup{}, err
				}
				return cleanup, nil
			}
			receipt, err := RunOrchestratorWorkerCleanup(request)
			if err != nil {
				t.Fatal(err)
			}
			if receipt.LifecycleStage != stage || receipt.Status != "cleanup-closed" || !receipt.ResidueAbsent || receipt.ProofCredit != 0 || receipt.CampaignID != plan.CampaignID || receipt.JobID != lease.JobID || receipt.Generation != lease.Generation || !sha256Pattern.MatchString(receipt.OrgCleanupSHA256) {
				t.Fatalf("receipt = %#v", receipt)
			}
			sealed, _, err := readMode0600JSON[OrchestratorWorkerCleanupReceipt](filepath.Join(request.OutputRoot, "WORKER_CLEANUP.json"))
			if err != nil || sealed != receipt {
				t.Fatalf("sealed receipt = %#v, err=%v", sealed, err)
			}
			replayed, err := RunOrchestratorWorkerCleanup(request)
			if err != nil || replayed != receipt || calls != 1 {
				t.Fatalf("replay=%#v calls=%d err=%v", replayed, calls, err)
			}
		})
	}
}

func TestOrchestratorWorkerCleanupRejectsTamperedOrProofEligibleLifecycle(t *testing.T) {
	for name, mutate := range map[string]func(t *testing.T, request OrchestratorWorkerCleanupRequest){
		"binding": func(t *testing.T, request OrchestratorWorkerCleanupRequest) {
			path := filepath.Join(request.OutputRoot, "ORCHESTRATOR_BINDING.json")
			if err := os.Chmod(path, 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"preflight": func(t *testing.T, request OrchestratorWorkerCleanupRequest) {
			if err := os.WriteFile(filepath.Join(request.OutputRoot, "ORG_PREFLIGHT.json"), []byte("{}\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"reservation": func(t *testing.T, request OrchestratorWorkerCleanupRequest) {
			path := filepath.Join(request.OutputRoot, "ORG_CREATION.json.reservation")
			reservation, _, err := readExactJSONBytes[salesforceOrgReservation](path)
			if err != nil {
				t.Fatal(err)
			}
			reservation.Marker = "glade-assurance-00000000000000000000000000000000"
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := WriteNewJSON(path, reservation); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, request, _ := workerCleanupTestRequest(t, "created-before-preflight")
			mutate(t, request)
			called := false
			request.cleanup = func(SalesforceOrgCleanupRequest) (SalesforceOrgCleanup, error) {
				called = true
				return SalesforceOrgCleanup{ResidueAbsent: true}, nil
			}
			if _, err := RunOrchestratorWorkerCleanup(request); err == nil || called {
				t.Fatalf("tamper accepted: called=%t err=%v", called, err)
			}
		})
	}
}

func TestOrchestratorWorkerCleanupResumesAfterCleanupReceiptWrite(t *testing.T) {
	for _, stage := range []string{"reservation-only", "invalidated-creation", "created-before-preflight"} {
		t.Run(stage, func(t *testing.T) {
			_, _, request, cleanup := workerCleanupTestRequest(t, stage)
			cleanupPath := filepath.Join(request.OutputRoot, "ORG_CLEANUP.json")
			if stage == "reservation-only" {
				var err error
				cleanup, err = RunSalesforceOrgCleanup(SalesforceOrgCleanupRequest{BundlePath: request.BundlePath, CreationPath: filepath.Join(request.OutputRoot, "ORG_CREATION.json"), TargetOrg: request.TargetOrg, DevHub: request.DevHub, SFBin: request.SFBin, OutputPath: cleanupPath, runner: cleanupRunnerForTest()})
				if err != nil {
					t.Fatal(err)
				}
			} else if err := WriteNewJSON(cleanupPath, cleanup); err != nil {
				t.Fatal(err)
			}
			request.cleanup = func(SalesforceOrgCleanupRequest) (SalesforceOrgCleanup, error) {
				t.Fatal("recovery reran Salesforce cleanup")
				return SalesforceOrgCleanup{}, nil
			}
			receipt, err := RunOrchestratorWorkerCleanup(request)
			if err != nil || !receipt.ResidueAbsent || !sha256Pattern.MatchString(receipt.OrgCleanupSHA256) {
				t.Fatalf("receipt=%#v cleanup=%#v err=%v", receipt, cleanup, err)
			}
		})
	}
}

func TestOrchestratorSSHCleanupTakeoverUsesSeparateRemotePathsAndPermanentlyBlocksCredit(t *testing.T) {
	request, workerReceipt := coordinatorCleanupTestRequest(t)
	var gotArgs []string
	request.sshRunner = func(_ context.Context, binary string, args ...string) (salesforceCommandOutput, error) {
		if binary != orchestratorSSHBinary {
			t.Fatalf("binary = %q", binary)
		}
		gotArgs = append([]string(nil), args...)
		data, err := json.Marshal(workerReceipt)
		if err != nil {
			t.Fatal(err)
		}
		return salesforceCommandOutput{Stdout: append(data, '\n')}, nil
	}
	request.copyRunner = func(_ context.Context, source, destination, _ string, checksum bool) (salesforceCommandOutput, error) {
		if checksum || source != request.Host+":"+filepath.Join(request.RemoteRoot, "WORKER_CLEANUP.json") {
			t.Fatalf("copy source=%q checksum=%t", source, checksum)
		}
		if err := WriteNewJSON(filepath.Join(destination, "WORKER_CLEANUP.json"), workerReceipt); err != nil {
			t.Fatal(err)
		}
		return salesforceCommandOutput{}, nil
	}
	receipt, err := runOrchestratorSSHCleanupTakeover(request, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Passed || receipt.Status != "cleanup-closed" || !receipt.ResidueAbsent || receipt.ProofCredit != 0 || !receipt.CleanupPermanentlyBlocked || !sha256Pattern.MatchString(receipt.WorkerReceiptSHA256) {
		t.Fatalf("coordinator receipt = %#v", receipt)
	}
	encoded, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	for _, synthetic := range []string{"durationMs", "copyStdoutSha256", "copyStderrSha256"} {
		if strings.Contains(string(encoded), synthetic) {
			t.Fatalf("success receipt retained synthetic field %q: %s", synthetic, encoded)
		}
	}
	if len(gotArgs) != 5 || !strings.Contains(gotArgs[4], request.RemoteBundlePath) || strings.Contains(gotArgs[4], request.PlanPath) || request.RemoteBundlePath == request.PlanPath {
		t.Fatalf("remote cleanup command did not preserve host path separation: %#v", gotArgs)
	}
	var cleanupState, allocationState string
	if err := request.Coordinator.db.QueryRow(`SELECT c.state, a.state FROM cleanup_journal c JOIN scratch_allocations a ON a.allocation_alias = c.allocation_alias WHERE c.allocation_alias = ?`, request.Claim.AllocationAlias).Scan(&cleanupState, &allocationState); err != nil {
		t.Fatal(err)
	}
	var blocks, credits int
	if err := request.Coordinator.db.QueryRow(`SELECT count(*) FROM cleanup_credit_blocks WHERE allocation_alias = ?`, request.Claim.AllocationAlias).Scan(&blocks); err != nil {
		t.Fatal(err)
	}
	if err := request.Coordinator.db.QueryRow(`SELECT count(*) FROM proof_credits WHERE campaign_id = ?`, request.Claim.CampaignID).Scan(&credits); err != nil {
		t.Fatal(err)
	}
	if cleanupState != "closed" || allocationState != "closed" || blocks != 1 || credits != 0 {
		t.Fatalf("cleanup=%q allocation=%q blocks=%d credits=%d", cleanupState, allocationState, blocks, credits)
	}
}

func TestOrchestratorCleanupTakeoverRejectsMixedLocalAndSSHMode(t *testing.T) {
	request, _ := coordinatorCleanupTestRequest(t)
	called := false
	binding := OrchestratorSSHCleanupBinding{
		PlanPath: request.PlanPath, LeasePath: request.LeasePath, FailedDispatchPath: request.FailedDispatchPath,
		Host: request.Host, WorkerBin: request.WorkerBin, RemotePlanPath: request.RemotePlanPath, RemoteLeasePath: request.RemoteLeasePath,
		RemoteBundlePath: request.RemoteBundlePath, RemoteSFBin: request.RemoteSFBin, RemoteRoot: request.RemoteRoot, FetchedReceiptPath: request.FetchedReceiptPath,
		sshRunner: func(context.Context, string, ...string) (salesforceCommandOutput, error) {
			called = true
			return salesforceCommandOutput{}, nil
		},
	}
	err := runOrchestratorCleanupTakeoverAt(request.Coordinator, OrchestratorCleanupTakeoverRequest{
		Claim: request.Claim, BundlePath: "/ignored/bundle", CreationPath: "/ignored/creation", PreflightPath: "/ignored/preflight",
		TargetOrg: request.Claim.AllocationAlias, SFBin: "/ignored/sf", OutputPath: request.OutputPath, SSH: &binding,
	}, time.Now, func(SalesforceOrgCleanupRequest) (SalesforceOrgCleanup, error) {
		t.Fatal("mixed request ran local cleanup")
		return SalesforceOrgCleanup{}, nil
	})
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") || called {
		t.Fatalf("mixed cleanup mode: called=%t err=%v", called, err)
	}
}

func TestOrchestratorSSHCleanupTakeoverTimeoutAndTamperLeaveCleanupOpen(t *testing.T) {
	for _, variant := range []string{"timeout", "dispatch-tamper", "receipt-tamper"} {
		t.Run(variant, func(t *testing.T) {
			request, workerReceipt := coordinatorCleanupTestRequest(t)
			called := false
			if variant == "dispatch-tamper" {
				failed, _, err := readExactJSONBytes[OrchestratorSSHDispatchReceipt](request.FailedDispatchPath)
				if err != nil {
					t.Fatal(err)
				}
				failed.CommandSHA256 = strings.Repeat("f", 64)
				if err := os.Remove(request.FailedDispatchPath); err != nil {
					t.Fatal(err)
				}
				writeJSONForOrchestratorSSHTest(t, request.FailedDispatchPath, failed)
			}
			request.sshRunner = func(ctx context.Context, _ string, _ ...string) (salesforceCommandOutput, error) {
				called = true
				if variant == "timeout" {
					<-ctx.Done()
					return salesforceCommandOutput{}, ctx.Err()
				}
				if variant == "receipt-tamper" {
					workerReceipt.ProofCredit = 1
				}
				data, _ := json.Marshal(workerReceipt)
				return salesforceCommandOutput{Stdout: append(data, '\n')}, nil
			}
			_, err := runOrchestratorSSHCleanupTakeover(request, time.Millisecond)
			if err == nil {
				t.Fatal("invalid cleanup takeover succeeded")
			}
			if variant == "dispatch-tamper" && called {
				t.Fatal("SSH ran before failed dispatch validation")
			}
			var state string
			if queryErr := request.Coordinator.db.QueryRow(`SELECT state FROM cleanup_journal WHERE allocation_alias = ?`, request.Claim.AllocationAlias).Scan(&state); queryErr != nil || state != "running" {
				t.Fatalf("cleanup state=%q err=%v", state, queryErr)
			}
		})
	}
}

func TestOrchestratorSSHCleanupTakeoverResumesCrashWindowsWithoutRemoteRerun(t *testing.T) {
	for _, window := range []string{"after-fetch", "after-close", "after-output"} {
		t.Run(window, func(t *testing.T) {
			request, workerReceipt := coordinatorCleanupTestRequest(t)
			if window == "after-output" {
				configureSuccessfulCleanupTakeover(t, &request, workerReceipt)
				first, err := runOrchestratorSSHCleanupTakeover(request, orchestratorSSHCleanupTimeout)
				if err != nil {
					t.Fatal(err)
				}
				request.sshRunner = func(context.Context, string, ...string) (salesforceCommandOutput, error) {
					t.Fatal("replay reran SSH")
					return salesforceCommandOutput{}, nil
				}
				replayed, err := runOrchestratorSSHCleanupTakeover(request, orchestratorSSHCleanupTimeout)
				if err != nil || replayed != first {
					t.Fatalf("output replay=%#v err=%v", replayed, err)
				}
				return
			}
			if err := WriteNewJSON(request.FetchedReceiptPath, workerReceipt); err != nil {
				t.Fatal(err)
			}
			if window == "after-close" {
				if err := request.Coordinator.closeCleanup(request.Claim, time.Now().UTC(), false); err != nil {
					t.Fatal(err)
				}
			}
			request.sshRunner = func(context.Context, string, ...string) (salesforceCommandOutput, error) {
				t.Fatal("resume reran SSH")
				return salesforceCommandOutput{}, nil
			}
			request.copyRunner = func(context.Context, string, string, string, bool) (salesforceCommandOutput, error) {
				t.Fatal("resume recopied worker receipt")
				return salesforceCommandOutput{}, nil
			}
			receipt, err := runOrchestratorSSHCleanupTakeover(request, orchestratorSSHCleanupTimeout)
			if err != nil || !receipt.Passed || !receipt.CleanupPermanentlyBlocked {
				t.Fatalf("resume receipt=%#v err=%v", receipt, err)
			}
		})
	}
}

func TestOrchestratorSSHCleanupTakeoverFetchedPublishIsCreateOnly(t *testing.T) {
	request, workerReceipt := coordinatorCleanupTestRequest(t)
	request.sshRunner = func(context.Context, string, ...string) (salesforceCommandOutput, error) {
		data, _ := json.Marshal(workerReceipt)
		return salesforceCommandOutput{Stdout: append(data, '\n')}, nil
	}
	sentinel := []byte("do-not-overwrite\n")
	request.copyRunner = func(_ context.Context, _ string, destination, _ string, _ bool) (salesforceCommandOutput, error) {
		if err := WriteNewJSON(filepath.Join(destination, "WORKER_CLEANUP.json"), workerReceipt); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(request.FetchedReceiptPath, sentinel, 0o600); err != nil {
			t.Fatal(err)
		}
		return salesforceCommandOutput{}, nil
	}
	if _, err := runOrchestratorSSHCleanupTakeover(request, orchestratorSSHCleanupTimeout); err == nil {
		t.Fatal("publish race overwrote final receipt")
	}
	data, err := os.ReadFile(request.FetchedReceiptPath)
	if err != nil || string(data) != string(sentinel) {
		t.Fatalf("race destination=%q err=%v", data, err)
	}
}

func TestOrchestratorSSHCleanupTakeoverRequiresClaimCloseMargin(t *testing.T) {
	request, _ := coordinatorCleanupTestRequest(t)
	shortUntil := time.Now().UTC().Add(orchestratorSSHCleanupTimeout + orchestratorSSHCleanupCloseMargin - time.Second)
	if _, err := request.Coordinator.db.Exec(`UPDATE cleanup_journal SET claim_until = ? WHERE allocation_alias = ?`, shortUntil.UnixMilli(), request.Claim.AllocationAlias); err != nil {
		t.Fatal(err)
	}
	request.Claim.ClaimUntil = shortUntil
	called := false
	request.sshRunner = func(context.Context, string, ...string) (salesforceCommandOutput, error) {
		called = true
		return salesforceCommandOutput{}, nil
	}
	if _, err := runOrchestratorSSHCleanupTakeover(request, orchestratorSSHCleanupTimeout); err == nil || called {
		t.Fatalf("short cleanup claim reached SSH: called=%t err=%v", called, err)
	}
}

func TestClaimCleanupForLeaseSelectsOnlyExactJournalAndReclaimsExpiry(t *testing.T) {
	orchestrator, plan := initializedTestOrchestrator(t)
	now := time.Now().UTC()
	first, err := orchestrator.Lease(plan.CampaignID, "worker-a", now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	second, err := orchestrator.Lease(plan.CampaignID, "worker-b", now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.SetHubCapacity("sealed-hub", 2); err != nil {
		t.Fatal(err)
	}
	remaining := 2
	if err := orchestrator.ObserveHub(OrchestratorHubObservation{HubAlias: "sealed-hub", ObservedAt: now, Healthy: true, DailyScratchOrgsRemaining: &remaining, ActiveScratchOrgsRemaining: &remaining}); err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.Reserve(first, "sealed-hub", "scratch-first", now); err != nil {
		t.Fatal(err)
	}
	remaining = 2
	if err := orchestrator.ObserveHub(OrchestratorHubObservation{HubAlias: "sealed-hub", ObservedAt: now.Add(time.Second), Healthy: true, DailyScratchOrgsRemaining: &remaining, ActiveScratchOrgsRemaining: &remaining}); err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.Reserve(second, "sealed-hub", "scratch-second", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	forged := second
	forged.LeaseUntil = forged.LeaseUntil.Add(time.Minute)
	if _, err := orchestrator.ClaimCleanupForLease(forged, "scratch-second", now.Add(time.Minute), 6*time.Minute); err == nil {
		t.Fatal("forged exact lease claimed cleanup")
	}
	var untouched string
	if err := orchestrator.db.QueryRow(`SELECT state FROM cleanup_journal WHERE allocation_alias = 'scratch-second'`).Scan(&untouched); err != nil || untouched != "pending" {
		t.Fatalf("forged lease mutated cleanup journal: state=%q err=%v", untouched, err)
	}
	claim, err := orchestrator.ClaimCleanupForLease(second, "scratch-second", now.Add(time.Minute), 6*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	var firstState, secondState string
	if err := orchestrator.db.QueryRow(`SELECT state FROM cleanup_journal WHERE allocation_alias = 'scratch-first'`).Scan(&firstState); err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.db.QueryRow(`SELECT state FROM cleanup_journal WHERE allocation_alias = 'scratch-second'`).Scan(&secondState); err != nil {
		t.Fatal(err)
	}
	if firstState != "pending" || secondState != "running" || claim.JobID != second.JobID || claim.Generation != second.Generation {
		t.Fatalf("first=%q second=%q claim=%#v", firstState, secondState, claim)
	}
	if _, err := orchestrator.ClaimCleanupForLease(second, "scratch-second", now.Add(2*time.Minute), 6*time.Minute); err == nil {
		t.Fatal("unexpired exact cleanup claim replay succeeded")
	}
	if _, err := orchestrator.db.Exec(`UPDATE cleanup_journal SET claim_until = ? WHERE allocation_alias = 'scratch-second'`, now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	reclaimed, err := orchestrator.ClaimCleanupForLease(second, "scratch-second", now.Add(2*time.Minute), 6*time.Minute)
	if err != nil || reclaimed.AllocationAlias != "scratch-second" || reclaimed.Worker != second.Worker {
		t.Fatalf("expired exact reclaim=%#v err=%v", reclaimed, err)
	}
}

func workerCleanupTestRequest(t *testing.T, stage string) (OrchestratorCampaignPlan, OrchestratorLease, OrchestratorWorkerCleanupRequest, SalesforceOrgCleanup) {
	t.Helper()
	_, plan, lease, _, _, _ := readyOrchestratorWorker(t)
	root := t.TempDir()
	bundlePath, sourceCreation, _, sourceCleanup := writeValidCleanupTakeoverFiles(t, root, "scratch-a")
	cleanup, _, err := readExactJSONBytes[SalesforceOrgCleanup](sourceCleanup)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(sourceCleanup); err != nil {
		t.Fatal(err)
	}
	outputRoot := filepath.Join(root, "remote-lifecycle")
	if err := os.Mkdir(outputRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteOrchestratorBatchBinding(filepath.Join(outputRoot, "ORCHESTRATOR_BINDING.json"), plan, lease); err != nil {
		t.Fatal(err)
	}
	creation, _, err := readExactJSONBytes[SalesforceOrgCreation](sourceCreation)
	if err != nil {
		t.Fatal(err)
	}
	absence := salesforceCommandForTest(t, bundlePath, []string{"org", "display", "--target-org", creation.Alias, "--json"})
	absence.Passed, absence.ExitCode = false, 1
	absence.Output.Stdout = []byte(`{"status":1,"message":"not found"}`)
	absence.StdoutSHA256 = replayBytesSHA256(absence.Output.Stdout)
	reservation := salesforceOrgReservation{SchemaVersion: 1, BundleSHA256: creation.BundleSHA256, DevHub: creation.DevHub, Alias: creation.Alias, Marker: creation.Marker, AliasAbsent: absence}
	if err := WriteNewJSON(filepath.Join(outputRoot, "ORG_CREATION.json.reservation"), reservation); err != nil {
		t.Fatal(err)
	}
	switch stage {
	case "created-before-preflight":
		if err := WriteNewJSON(filepath.Join(outputRoot, "ORG_CREATION.json"), creation); err != nil {
			t.Fatal(err)
		}
	case "invalidated-creation":
		creation.Invalidated, creation.DevHubCommand = true, CommandResult{}
		if err := WriteNewJSON(filepath.Join(outputRoot, "ORG_CREATION.json.invalidated"), creation); err != nil {
			t.Fatal(err)
		}
	case "reservation-only":
	default:
		t.Fatalf("unknown stage %q", stage)
	}
	planBytes, _ := json.Marshal(plan)
	leaseBytes, _ := json.Marshal(lease)
	return plan, lease, OrchestratorWorkerCleanupRequest{
		Plan: plan, Lease: lease, PlanSHA256: replayBytesSHA256(append(planBytes, '\n')), LeaseSHA256: replayBytesSHA256(append(leaseBytes, '\n')), FailedSSHReceiptSHA256: strings.Repeat("f", 64),
		BundlePath: bundlePath, DevHub: creation.DevHub, TargetOrg: creation.Alias, SFBin: salesforceCLIPath, OutputRoot: outputRoot,
		ExecutedTools: RuntimeArtifact{Commit: plan.Definition.Tools.Commit, OS: "darwin", Arch: "arm64", SHA256: plan.Definition.Tools.SHA256},
	}, cleanup
}

func coordinatorCleanupTestRequest(t *testing.T) (OrchestratorSSHCleanupTakeoverRequest, OrchestratorWorkerCleanupReceipt) {
	t.Helper()
	orchestrator, plan := initializedTestOrchestrator(t)
	now := time.Now().UTC()
	lease, err := orchestrator.Lease(plan.CampaignID, "worker-a", now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.SetHubCapacity("sealed-hub", 1); err != nil {
		t.Fatal(err)
	}
	observeReadyHub(t, orchestrator, "sealed-hub", now)
	if err := orchestrator.Reserve(lease, "sealed-hub", "scratch-a", now); err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.recordWorkerFailure(lease, orchestratorSSHDispatchFailed, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	claim, err := orchestrator.ClaimCleanup(plan.CampaignID, "worker-a", now.Add(2*time.Minute), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	planPath, leasePath, failedPath := filepath.Join(root, "plan.json"), filepath.Join(root, "lease.json"), filepath.Join(root, "failed.json")
	writeJSONForOrchestratorSSHTest(t, planPath, plan)
	writeJSONForOrchestratorSSHTest(t, leasePath, lease)
	planSHA, _ := sha256File(planPath)
	leaseSHA, _ := sha256File(leasePath)
	request := OrchestratorSSHCleanupTakeoverRequest{
		Coordinator: orchestrator, Claim: claim, OutputPath: filepath.Join(root, "coordinator.json"),
		OrchestratorSSHCleanupBinding: OrchestratorSSHCleanupBinding{
			PlanPath: planPath, LeasePath: leasePath, FailedDispatchPath: failedPath,
			Host: "operator@worker.example.internal", WorkerBin: "/remote/bin/glade-tools", RemotePlanPath: "/remote/authority/plan.json", RemoteLeasePath: "/remote/authority/lease.json",
			RemoteBundlePath: "/remote/authority/bundle.json", RemoteSFBin: "/remote/bin/sf", RemoteRoot: "/remote/lifecycle", FetchedReceiptPath: filepath.Join(root, "fetched.json"),
		},
	}
	original := OrchestratorSSHDispatchRequest{Host: request.Host, WorkerBin: request.WorkerBin, PlanPath: request.RemotePlanPath, LeasePath: request.RemoteLeasePath, BundlePath: request.RemoteBundlePath, TargetOrg: claim.AllocationAlias, SFBin: request.RemoteSFBin, OutputRoot: request.RemoteRoot}
	originalCommand := orchestratorSSHWorkerOnceCommand(original, plan.Definition.Tools.SHA256, plan.Definition.ControlledInputSHA256[OrchestratorToolsAMD64Input], planSHA, leaseSHA, claim.HubAlias)
	failed := OrchestratorSSHDispatchReceipt{
		SchemaVersion: 1, CampaignID: lease.CampaignID, JobID: lease.JobID, ShardIndex: lease.ShardIndex, Generation: lease.Generation, Status: "failed", FailureCode: orchestratorSSHDispatchFailed,
		CommandSHA256: commandSpecSHA256(ReplayCommand{Path: orchestratorSSHBinary, Args: []string{"-o", "BatchMode=yes", "--", request.Host, originalCommand}, Timeout: orchestratorSSHTimeout}), StdoutSHA256: strings.Repeat("a", 64), StderrSHA256: strings.Repeat("b", 64), ExitCode: 255, DurationMS: 1,
		TimeoutMS: orchestratorSSHTimeout.Milliseconds(), Passed: false, ActionRequired: true, ActionCode: orchestratorSSHActionCode, SpecSHA256: plan.SpecSHA256, PlanSHA256: planSHA, LeaseSHA256: leaseSHA,
	}
	writeJSONForOrchestratorSSHTest(t, failedPath, failed)
	failedSHA, _ := sha256File(failedPath)
	workerReceipt := OrchestratorWorkerCleanupReceipt{
		SchemaVersion: 1, Status: "cleanup-closed", LifecycleStage: "created-before-preflight", CampaignID: lease.CampaignID, JobID: lease.JobID, ShardIndex: lease.ShardIndex, Generation: lease.Generation,
		SpecSHA256: plan.SpecSHA256, PlanSHA256: planSHA, LeaseSHA256: leaseSHA, FailedSSHReceiptSHA256: failedSHA, OrchestratorBindingSHA256: strings.Repeat("c", 64), LifecycleAuthoritySHA256: strings.Repeat("d", 64), OrgCleanupSHA256: strings.Repeat("e", 64), ResidueAbsent: true, ProofCredit: 0,
		ExecutedTools: RuntimeArtifact{Commit: plan.Definition.Tools.Commit, OS: "darwin", Arch: "arm64", SHA256: plan.Definition.Tools.SHA256},
	}
	return request, workerReceipt
}

func configureSuccessfulCleanupTakeover(t *testing.T, request *OrchestratorSSHCleanupTakeoverRequest, workerReceipt OrchestratorWorkerCleanupReceipt) {
	t.Helper()
	request.sshRunner = func(context.Context, string, ...string) (salesforceCommandOutput, error) {
		data, _ := json.Marshal(workerReceipt)
		return salesforceCommandOutput{Stdout: append(data, '\n')}, nil
	}
	request.copyRunner = func(_ context.Context, _ string, destination, _ string, _ bool) (salesforceCommandOutput, error) {
		if err := WriteNewJSON(filepath.Join(destination, "WORKER_CLEANUP.json"), workerReceipt); err != nil {
			t.Fatal(err)
		}
		return salesforceCommandOutput{}, nil
	}
}
