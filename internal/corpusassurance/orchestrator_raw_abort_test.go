package corpusassurance

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOrchestratorRawPrecreationAbortObserveAndAccept(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	scope, _ := writeSurfaceOracleIndexInputs(t, root)
	definition := testOrchestratorDefinition(t, scope, [][]string{{"apex:System.One", "apex:System.Two"}, {"apex:System.Three"}})
	plan, err := PlanOrchestratorCampaign(definition)
	if err != nil {
		t.Fatal(err)
	}
	orchestrator := openTestOrchestrator(t)
	if err := orchestrator.InitCampaign(plan); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	lease, err := orchestrator.Lease(plan.CampaignID, "worker-a", now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.SetHubCapacity("hub-a", 1); err != nil {
		t.Fatal(err)
	}
	observeReadyHub(t, orchestrator, "hub-a", now)
	allocation := "abort-campaign-0"
	if err := orchestrator.Reserve(lease, "hub-a", allocation, now); err != nil {
		t.Fatal(err)
	}

	bundlePath := filepath.Join(root, "bundle", "bundle.json")
	if err := os.MkdirAll(filepath.Dir(bundlePath), 0o700); err != nil {
		t.Fatal(err)
	}
	writeSyntheticDevHubBundle(t, bundlePath)
	bundle, _, err := readExactJSONBytes[OracleBundle](bundlePath)
	if err != nil {
		t.Fatal(err)
	}
	canonicalSF := filepath.Join(root, "sf")
	if err := os.WriteFile(canonicalSF, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	authorityPath := filepath.Join(filepath.Dir(bundlePath), "DEV_HUB_AUTHORITY.json")
	authority, _, err := readExactJSONBytes[SalesforceDevHubAuthority](authorityPath)
	if err != nil {
		t.Fatal(err)
	}
	authority.Execution.SFBinary = canonicalSF
	authority.Execution.Environment = []string{"HOME=" + filepath.Join(root, "home"), "PATH=" + root + ":/usr/bin:/bin", "SF_USE_GENERIC_UNIX_KEYCHAIN=true", "TMPDIR=" + filepath.Join(root, "tmp")}
	authority.Execution.SFSHA256, err = sha256File(canonicalSF)
	if err != nil {
		t.Fatal(err)
	}
	authority.Command.Command[0] = canonicalSF
	authority.Command.Environment = authority.Execution.Environment
	authority.Command.ExecutableSHA256 = authority.Execution.SFSHA256
	authority.Command.ExecutableAfterSHA256 = authority.Execution.SFSHA256
	authority.Command.CommandSpecSHA256 = salesforceCommandSpecSHA256(canonicalSF, authority.Command.Command[1:], authority.Command.WorkingDirectory, authority.Command.Environment, authority.Execution.SFSHA256, authority.Execution.SFSHA256)
	if err := os.Remove(authorityPath); err != nil {
		t.Fatal(err)
	}
	if err := WriteNewJSON(authorityPath, authority); err != nil {
		t.Fatal(err)
	}
	authoritySHA, err := sha256File(authorityPath)
	if err != nil {
		t.Fatal(err)
	}
	bundle.Candidate = RuntimeArtifact{Commit: definition.Candidate.Commit, OS: "darwin", Arch: "arm64", SHA256: definition.Candidate.SHA256}
	bundle.Tools = RuntimeArtifact{Commit: definition.Tools.Commit, OS: "darwin", Arch: "arm64", SHA256: definition.Tools.SHA256}
	bundle.DevHubAuthoritySHA256, bundle.SalesforceExecution = authoritySHA, authority.Execution
	if err := os.Remove(bundlePath); err != nil {
		t.Fatal(err)
	}
	if err := WriteNewJSON(bundlePath, bundle); err != nil {
		t.Fatal(err)
	}

	rawRoot := filepath.Join(root, "raw")
	if err := os.Mkdir(rawRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteOrchestratorBatchBinding(filepath.Join(rawRoot, "ORCHESTRATOR_BINDING.json"), plan, lease); err != nil {
		t.Fatal(err)
	}
	planSHA, leaseSHA, err := canonicalPlanLeaseHashes(plan, lease)
	if err != nil {
		t.Fatal(err)
	}
	failed := OrchestratorSSHDispatchReceipt{SchemaVersion: 1, CampaignID: lease.CampaignID, JobID: lease.JobID, ShardIndex: lease.ShardIndex, Generation: lease.Generation, Status: "failed", FailureCode: orchestratorSSHDispatchFailed, CommandSHA256: strings.Repeat("1", 64), StdoutSHA256: strings.Repeat("2", 64), StderrSHA256: strings.Repeat("3", 64), ExitCode: 1, ActionRequired: true, ActionCode: orchestratorSSHActionCode, SpecSHA256: plan.SpecSHA256, PlanSHA256: planSHA, LeaseSHA256: leaseSHA}
	failedSHA, err := canonicalJSONHash(failed)
	if err != nil {
		t.Fatal(err)
	}
	for _, unsafe := range []OrchestratorSSHDispatchReceipt{
		func() OrchestratorSSHDispatchReceipt { value := failed; value.ExitCode = 255; return value }(),
		func() OrchestratorSSHDispatchReceipt { value := failed; value.ExitCode = -1; return value }(),
		func() OrchestratorSSHDispatchReceipt {
			value := failed
			value.Status, value.FailureCode, value.TimedOut, value.ExitCode = "timeout", orchestratorSSHDispatchTimeout, true, -1
			return value
		}(),
	} {
		if validFailedRawAbortSSHReceipt(unsafe, plan, lease) {
			t.Fatalf("unsafe transport receipt accepted: %#v", unsafe)
		}
	}
	outputPath := filepath.Join(root, "observation.json")
	runnerCalls := 0
	runner := func(_ context.Context, _ string, args ...string) (salesforceCommandOutput, error) {
		if len(args) != 5 || args[0] != "org" || args[1] != "display" || args[2] != "--target-org" || args[3] != allocation || args[4] != "--json" {
			return salesforceCommandOutput{}, os.ErrInvalid
		}
		runnerCalls++
		time.Sleep(time.Duration(runnerCalls) * 5 * time.Millisecond)
		return salesforceCommandOutput{ExitCode: 2, Stdout: []byte(`{"status":2,"exitCode":2,"name":"NamedOrgNotFoundError","code":"NamedOrgNotFoundError","context":"OrgDisplayCommand","commandName":"OrgDisplayCommand","message":"No authorization information found for target org"}`)}, nil
	}
	observeRequest := OrchestratorRawPrecreationAbortObservationRequest{
		Plan: plan, Lease: lease, PlanSHA256: planSHA, LeaseSHA256: leaseSHA, FailedSSHReceipt: failed, FailedSSHReceiptSHA256: failedSHA,
		BundlePath: bundlePath, RawRoot: rawRoot, AllocationAlias: allocation, TargetOrg: allocation, SFBin: canonicalSF, OutputPath: outputPath,
		validateBundle: func(string) error { return nil }, recoveryTool: func() (string, error) { return canonicalSF, nil },
		runner: runner,
	}
	extraPath := filepath.Join(rawRoot, "unexpected.json")
	if err := os.WriteFile(extraPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ObserveOrchestratorRawPrecreationAbort(observeRequest); err == nil || !strings.Contains(err.Error(), "only ORCHESTRATOR_BINDING") {
		t.Fatalf("extra raw artifact error = %v", err)
	}
	if err := os.Remove(extraPath); err != nil {
		t.Fatal(err)
	}
	racingRequest := observeRequest
	racingRequest.OutputPath = filepath.Join(root, "racing-observation.json")
	racingRequest.runner = func(ctx context.Context, binary string, args ...string) (salesforceCommandOutput, error) {
		if err := os.WriteFile(extraPath, []byte("{}\n"), 0o600); err != nil {
			return salesforceCommandOutput{}, err
		}
		return runner(ctx, binary, args...)
	}
	if _, err := ObserveOrchestratorRawPrecreationAbort(racingRequest); err == nil || !strings.Contains(err.Error(), "only ORCHESTRATOR_BINDING") {
		t.Fatalf("post-probe raw artifact error = %v", err)
	}
	if err := os.Remove(extraPath); err != nil {
		t.Fatal(err)
	}
	observation, err := ObserveOrchestratorRawPrecreationAbort(observeRequest)
	if err != nil {
		t.Fatal(err)
	}
	if replay, err := ObserveOrchestratorRawPrecreationAbort(observeRequest); err != nil || replay != observation {
		t.Fatalf("observation replay = %#v, err=%v", replay, err)
	}
	data, err := os.ReadFile(outputPath)
	if err != nil || strings.Contains(string(data), allocation) || strings.Contains(string(data), bundlePath) {
		t.Fatalf("sanitized observation leaked private input: %v %s", err, data)
	}
	obsSHA := replayBytesSHA256(data)
	receiptPath := filepath.Join(root, "receipt.json")
	badAccept := OrchestratorRawPrecreationAbortAcceptanceRequest{
		Coordinator: orchestrator, Plan: plan, Lease: lease, PlanSHA256: planSHA, LeaseSHA256: leaseSHA, FailedSSHReceipt: failed, FailedSSHReceiptSHA256: failedSHA,
		AllocationAlias: allocation, ObservationPath: outputPath, ObservationSHA256: obsSHA, OutputPath: receiptPath,
		executingTool: os.Executable,
	}
	if _, err := AcceptOrchestratorRawPrecreationAbort(badAccept); err == nil || !strings.Contains(err.Error(), "executing recovery tool hash") {
		t.Fatalf("mismatched acceptance tool error = %v", err)
	}
	got, err := AcceptOrchestratorRawPrecreationAbort(OrchestratorRawPrecreationAbortAcceptanceRequest{
		Coordinator: orchestrator, Plan: plan, Lease: lease, PlanSHA256: planSHA, LeaseSHA256: leaseSHA, FailedSSHReceipt: failed, FailedSSHReceiptSHA256: failedSHA,
		AllocationAlias: allocation, ObservationPath: outputPath, ObservationSHA256: obsSHA, OutputPath: receiptPath,
		executingTool: func() (string, error) { return canonicalSF, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.CleanupClosed || got.ProofCredit != 0 || got.Status != "validated-zero-credit" {
		t.Fatalf("receipt = %#v", got)
	}
	var cleanup, allocationState, job, attempt string
	if err := orchestrator.db.QueryRow(`SELECT c.state, a.state, j.status, t.status FROM cleanup_journal c JOIN scratch_allocations a ON a.allocation_alias = c.allocation_alias JOIN jobs j ON j.campaign_id = c.campaign_id AND j.id = c.job_id AND j.generation = c.generation JOIN attempts t ON t.campaign_id = c.campaign_id AND t.job_id = c.job_id AND t.generation = c.generation WHERE c.allocation_alias = ?`, allocation).Scan(&cleanup, &allocationState, &job, &attempt); err != nil {
		t.Fatal(err)
	}
	if cleanup != "closed" || allocationState != "closed" || job != "running" || attempt != "running" {
		t.Fatalf("states = %s/%s/%s/%s", cleanup, allocationState, job, attempt)
	}
	for query, want := range map[string]int{
		`SELECT count(*) FROM proof_credits WHERE campaign_id = ?`:                                                                               0,
		`SELECT count(*) FROM receipts WHERE campaign_id = ?`:                                                                                    0,
		`SELECT count(*) FROM cleanup_credit_blocks b JOIN cleanup_journal c ON c.allocation_alias = b.allocation_alias WHERE c.campaign_id = ?`: 1,
		`SELECT h.reserved FROM hub_capacity h JOIN scratch_allocations a ON a.hub_alias = h.hub_alias WHERE a.campaign_id = ?`:                  0,
	} {
		var got int
		if err := orchestrator.db.QueryRow(query, lease.CampaignID).Scan(&got); err != nil || got != want {
			t.Fatalf("query %q = %d, err=%v, want %d", query, got, err, want)
		}
	}
	if err := os.Remove(receiptPath); err != nil {
		t.Fatal(err)
	}
	if replay, err := AcceptOrchestratorRawPrecreationAbort(OrchestratorRawPrecreationAbortAcceptanceRequest{
		Coordinator: orchestrator, Plan: plan, Lease: lease, PlanSHA256: planSHA, LeaseSHA256: leaseSHA, FailedSSHReceipt: failed, FailedSSHReceiptSHA256: failedSHA,
		AllocationAlias: allocation, ObservationPath: outputPath, ObservationSHA256: obsSHA, OutputPath: receiptPath,
		executingTool: func() (string, error) { return canonicalSF, nil },
	}); err != nil || replay != got {
		t.Fatalf("replay = %#v, err=%v", replay, err)
	}
}
