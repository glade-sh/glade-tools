package corpusassurance

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunOrchestratorSSHDispatchUsesFixedWorkerCommandAndSanitizedReceipt(t *testing.T) {
	root := t.TempDir()
	plan, lease, request := readyOrchestratorSSHTestRequest(t, root)
	request.Host = "-F@host"
	if request.RemotePlanPath == request.PlanPath {
		t.Fatal("test requires distinct coordinator and worker plan paths")
	}
	planSHA, leaseSHA := orchestratorSSHTestInputHashes(t, request)
	var gotBinary string
	var gotArgs []string
	executedTools := RuntimeArtifact{Commit: plan.Definition.Tools.Commit, OS: "darwin", Arch: "arm64", SHA256: plan.Definition.Tools.SHA256}
	completion, err := json.Marshal(OrchestratorWorkerOnceCompletion{CampaignID: lease.CampaignID, JobID: lease.JobID, ShardIndex: lease.ShardIndex, Generation: lease.Generation, Status: "worker-complete", SpecSHA256: plan.SpecSHA256, PlanSHA256: planSHA, LeaseSHA256: leaseSHA, OrchestratorBindingSHA256: strings.Repeat("a", 64), SalesforceShardSHA256: strings.Repeat("b", 64), OrgCleanupSHA256: strings.Repeat("c", 64), ExecutedTools: executedTools})
	if err != nil {
		t.Fatal(err)
	}
	sensitive := []string{request.Host, request.TargetOrg, request.BundlePath, "00D000000000001", "force://client:" + "secret@example.invalid", "access" + "_token=private", "refresh" + "_token=private"}
	receipt, err := runOrchestratorSSHDispatch(request, func(_ context.Context, binary string, args ...string) (salesforceCommandOutput, error) {
		gotBinary, gotArgs = binary, append([]string(nil), args...)
		return salesforceCommandOutput{Stdout: append(completion, '\n'), Stderr: []byte(strings.Join(sensitive, "\n"))}, nil
	})
	if err != nil {
		t.Fatalf("runOrchestratorSSHDispatch: %v", err)
	}
	if gotBinary != orchestratorSSHBinary || len(gotArgs) != 5 || gotArgs[0] != "-o" || gotArgs[1] != "BatchMode=yes" || gotArgs[2] != "--" || gotArgs[3] != request.Host {
		t.Fatalf("ssh invocation = %q %#v", gotBinary, gotArgs)
	}
	want := orchestratorSSHWorkerOnceCommand(request, plan.Definition.Tools.SHA256, plan.Definition.ControlledInputSHA256[OrchestratorToolsAMD64Input], planSHA, leaseSHA, "sealed-hub")
	if gotArgs[4] != want || !strings.Contains(want, "/usr/bin/shasum -a 256 --") || !strings.Contains(want, "export SF_USE_GENERIC_UNIX_KEYCHAIN=true; exec") || !strings.Contains(want, " corpus assurance orchestrator worker-once") || !strings.Contains(want, "--plan '"+request.RemotePlanPath+"'") || strings.Contains(want, request.PlanPath) || !strings.Contains(want, "--dev-hub 'sealed-hub'") {
		t.Fatalf("worker command = %q, want %q", gotArgs[4], want)
	}
	if receipt.Status != "worker-complete" || !receipt.Passed || receipt.TimeoutMS != orchestratorSSHTimeout.Milliseconds() || receipt.StdoutSHA256 == "" || receipt.StderrSHA256 == "" || receipt.SpecSHA256 != plan.SpecSHA256 || receipt.PlanSHA256 != planSHA || receipt.LeaseSHA256 != leaseSHA || receipt.OrchestratorBindingSHA256 != strings.Repeat("a", 64) || receipt.SalesforceShardSHA256 != strings.Repeat("b", 64) || receipt.OrgCleanupSHA256 != strings.Repeat("c", 64) || receipt.ExecutedTools != executedTools || receipt.ActionRequired || receipt.ActionCode != "" {
		t.Fatalf("receipt = %#v", receipt)
	}
	data, err := os.ReadFile(request.OutputPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range sensitive {
		if strings.Contains(string(data), value) {
			t.Fatalf("receipt leaked private dispatch data: %s", data)
		}
	}
	var sealed OrchestratorSSHDispatchReceipt
	if err := json.Unmarshal(data, &sealed); err != nil || sealed != receipt {
		t.Fatalf("sealed receipt = %#v, want %#v (err=%v)", sealed, receipt, err)
	}
}

func TestOrchestratorExecutedToolsPreservesHistoricalZeroValueJSON(t *testing.T) {
	for name, value := range map[string]any{
		"completion": OrchestratorWorkerOnceCompletion{Status: "worker-complete"},
		"dispatch":   OrchestratorSSHDispatchReceipt{Status: "worker-complete"},
		"fetch":      OrchestratorSSHRawFetchReceipt{Status: "fetched"},
	} {
		data, err := json.Marshal(value)
		if err != nil || strings.Contains(string(data), "executedTools") {
			t.Fatalf("%s zero-value JSON = %s, %v", name, data, err)
		}
	}
}

func TestOrchestratorSSHWorkerOnceCommandAllowsPlanBoundPlatformWorker(t *testing.T) {
	root := t.TempDir()
	request := OrchestratorSSHDispatchRequest{
		WorkerBin: filepath.Join(root, "glade-tools"), PlanPath: filepath.Join(root, "local-plan"), RemotePlanPath: filepath.Join(root, "remote-plan"),
		LeasePath: filepath.Join(root, "lease"), BundlePath: filepath.Join(root, "bundle"),
		TargetOrg: "scratch-a", SFBin: filepath.Join(root, "sf"), OutputRoot: filepath.Join(root, "output"),
	}
	primarySHA := strings.Repeat("a", 64)
	alternateSHA := strings.Repeat("b", 64)
	command := orchestratorSSHWorkerOnceCommand(request, primarySHA, alternateSHA, strings.Repeat("c", 64), strings.Repeat("d", 64), "sealed-hub")
	if !strings.Contains(command, "worker_sha=") || !strings.Contains(command, primarySHA) || !strings.Contains(command, alternateSHA) || !strings.Contains(command, "|| test \"$worker_sha\"") {
		t.Fatalf("worker command does not allow both plan-bound hashes: %q", command)
	}
	if output, err := exec.Command("/bin/sh", "-n", "-c", command).CombinedOutput(); err != nil {
		t.Fatalf("worker command is not valid shell: %v: %s", err, output)
	}
}

func TestRunOrchestratorSSHDispatchRejectsUnsafeInputsAndExistingOutput(t *testing.T) {
	root := t.TempDir()
	_, _, valid := readyOrchestratorSSHTestRequest(t, root)
	for name, mutate := range map[string]func(*OrchestratorSSHDispatchRequest){
		"unsafe host":          func(r *OrchestratorSSHDispatchRequest) { r.Host = "operator@worker;rm -rf /" },
		"relative path":        func(r *OrchestratorSSHDispatchRequest) { r.PlanPath = "plan.json" },
		"unclean path":         func(r *OrchestratorSSHDispatchRequest) { r.PlanPath = root + "/dir/../plan" },
		"missing remote plan":  func(r *OrchestratorSSHDispatchRequest) { r.RemotePlanPath = "" },
		"relative remote plan": func(r *OrchestratorSSHDispatchRequest) { r.RemotePlanPath = "plan.json" },
		"unclean remote plan":  func(r *OrchestratorSSHDispatchRequest) { r.RemotePlanPath = root + "/dir/../remote-plan" },
		"unsafe target org":    func(r *OrchestratorSSHDispatchRequest) { r.TargetOrg = "scratch;rm" },
	} {
		t.Run(name, func(t *testing.T) {
			request := valid
			mutate(&request)
			if _, err := runOrchestratorSSHDispatch(request, func(context.Context, string, ...string) (salesforceCommandOutput, error) {
				t.Fatal("runner called")
				return salesforceCommandOutput{}, nil
			}); err == nil {
				t.Fatal("unsafe request accepted")
			}
		})
	}
	if err := os.WriteFile(valid.OutputPath, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := runOrchestratorSSHDispatch(valid, func(context.Context, string, ...string) (salesforceCommandOutput, error) {
		t.Fatal("runner called")
		return salesforceCommandOutput{}, nil
	}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("existing output error = %v", err)
	}
	missingParent := valid
	missingParent.OutputPath = filepath.Join(root, "missing", "output")
	if _, err := runOrchestratorSSHDispatch(missingParent, func(context.Context, string, ...string) (salesforceCommandOutput, error) {
		t.Fatal("runner called")
		return salesforceCommandOutput{}, nil
	}); err == nil || !strings.Contains(err.Error(), "preflight") {
		t.Fatalf("missing output parent error = %v", err)
	}
}

func TestRunOrchestratorSSHDispatchRequiresCurrentReservedAllocation(t *testing.T) {
	root := t.TempDir()
	orchestrator, plan := initializedTestOrchestrator(t)
	now := time.Now().UTC()
	lease, err := orchestrator.Lease(plan.CampaignID, "worker-a", now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.SetHubCapacity("hub-a", 1); err != nil {
		t.Fatal(err)
	}
	observeReadyHub(t, orchestrator, "hub-a", now)
	if err := orchestrator.Reserve(lease, "hub-a", "scratch-a", now); err != nil {
		t.Fatal(err)
	}
	request := OrchestratorSSHDispatchRequest{
		Coordinator: orchestrator, Host: "operator@worker-a.example.internal", WorkerBin: filepath.Join(root, "glade-tools"),
		PlanPath: filepath.Join(root, "local-plan"), RemotePlanPath: filepath.Join(root, "remote-plan"), LeasePath: filepath.Join(root, "lease"), BundlePath: filepath.Join(root, "bundle"),
		TargetOrg: "scratch-a", SFBin: filepath.Join(root, "sf"), OutputRoot: filepath.Join(root, "output-root"), OutputPath: filepath.Join(root, "output"),
	}
	writeJSONForOrchestratorSSHTest(t, request.PlanPath, plan)
	writeJSONForOrchestratorSSHTest(t, request.LeasePath, lease)
	planSHA, leaseSHA := orchestratorSSHTestInputHashes(t, request)
	completion, err := json.Marshal(OrchestratorWorkerOnceCompletion{CampaignID: lease.CampaignID, JobID: lease.JobID, ShardIndex: lease.ShardIndex, Generation: lease.Generation, Status: "worker-complete", SpecSHA256: plan.SpecSHA256, PlanSHA256: planSHA, LeaseSHA256: leaseSHA, OrchestratorBindingSHA256: strings.Repeat("a", 64), SalesforceShardSHA256: strings.Repeat("b", 64), OrgCleanupSHA256: strings.Repeat("c", 64)})
	if err != nil {
		t.Fatal(err)
	}
	runner := func(context.Context, string, ...string) (salesforceCommandOutput, error) {
		return salesforceCommandOutput{Stdout: append(completion, '\n')}, nil
	}
	if _, err := runOrchestratorSSHDispatch(request, runner); err != nil {
		t.Fatalf("reserved dispatch failed: %v", err)
	}
	for name, mutate := range map[string]func(*OrchestratorSSHDispatchRequest, *OrchestratorLease){
		"unreserved target": func(request *OrchestratorSSHDispatchRequest, _ *OrchestratorLease) { request.TargetOrg = "scratch-b" },
		"expired lease": func(_ *OrchestratorSSHDispatchRequest, lease *OrchestratorLease) {
			lease.LeaseUntil = now.Add(-time.Second)
		},
		"short lease": func(_ *OrchestratorSSHDispatchRequest, lease *OrchestratorLease) {
			lease.LeaseUntil = time.Now().UTC().Add(time.Minute)
		},
		"alternate live lease": func(_ *OrchestratorSSHDispatchRequest, lease *OrchestratorLease) {
			lease.LeaseUntil = time.Now().UTC().Add(50 * time.Minute)
		},
		"alternate lease term": func(_ *OrchestratorSSHDispatchRequest, lease *OrchestratorLease) {
			lease.DurationMS = (50 * time.Minute).Milliseconds()
		},
	} {
		t.Run(name, func(t *testing.T) {
			invalidRequest, invalidLease := request, lease
			invalidRequest.OutputPath = filepath.Join(root, strings.ReplaceAll(name, " ", "-"))
			mutate(&invalidRequest, &invalidLease)
			writeJSONForOrchestratorSSHTest(t, invalidRequest.LeasePath+"-"+strings.ReplaceAll(name, " ", "-"), invalidLease)
			invalidRequest.LeasePath += "-" + strings.ReplaceAll(name, " ", "-")
			called := false
			if _, err := runOrchestratorSSHDispatch(invalidRequest, func(context.Context, string, ...string) (salesforceCommandOutput, error) {
				called = true
				return salesforceCommandOutput{}, nil
			}); err == nil {
				t.Fatal("invalid dispatch succeeded")
			}
			if called {
				t.Fatal("SSH ran before coordinator authority validation")
			}
		})
	}
	if _, err := orchestrator.db.Exec(`UPDATE campaigns SET spec_sha256 = ? WHERE id = ?`, strings.Repeat("f", 64), plan.CampaignID); err != nil {
		t.Fatal(err)
	}
	request.OutputPath = filepath.Join(root, "drifted-spec")
	called := false
	if _, err := runOrchestratorSSHDispatch(request, func(context.Context, string, ...string) (salesforceCommandOutput, error) {
		called = true
		return salesforceCommandOutput{}, nil
	}); err == nil || called {
		t.Fatalf("drifted coordinator spec reached SSH: called=%v err=%v", called, err)
	}
}

func TestRunOrchestratorSSHDispatchRejectsExitZeroWithoutMatchingCompletion(t *testing.T) {
	root := t.TempDir()
	plan, _, request := readyOrchestratorSSHTestRequest(t, root)
	planSHA, leaseSHA := orchestratorSSHTestInputHashes(t, request)
	receipt, err := runOrchestratorSSHDispatch(request, func(context.Context, string, ...string) (salesforceCommandOutput, error) {
		return salesforceCommandOutput{Stdout: []byte(`{"status":"worker-complete"}`)}, nil
	})
	if err == nil || receipt.Passed || receipt.Status != "failed" || !receipt.ActionRequired || receipt.ActionCode != orchestratorSSHActionCode || receipt.SpecSHA256 != plan.SpecSHA256 || receipt.PlanSHA256 != planSHA || receipt.LeaseSHA256 != leaseSHA {
		t.Fatalf("malformed completion passed: receipt=%#v err=%v", receipt, err)
	}
	if _, statErr := os.Stat(request.OutputPath); statErr != nil {
		t.Fatalf("failed sanitized receipt was not written: %v", statErr)
	}
}

func TestRunOrchestratorSSHDispatchTimesOutWithoutRawOutput(t *testing.T) {
	root := t.TempDir()
	plan, _, request := readyOrchestratorSSHTestRequest(t, root)
	planSHA, leaseSHA := orchestratorSSHTestInputHashes(t, request)
	receipt, err := runOrchestratorSSHDispatchWithTimeout(request, time.Millisecond, func(ctx context.Context, _ string, _ ...string) (salesforceCommandOutput, error) {
		<-ctx.Done()
		return salesforceCommandOutput{Stderr: []byte("private timeout")}, ctx.Err()
	})
	if err == nil || receipt.Passed || !receipt.TimedOut || receipt.Status != "timeout" || !receipt.ActionRequired || receipt.ActionCode != orchestratorSSHActionCode || receipt.SpecSHA256 != plan.SpecSHA256 || receipt.PlanSHA256 != planSHA || receipt.LeaseSHA256 != leaseSHA {
		t.Fatalf("timeout receipt=%#v err=%v", receipt, err)
	}
	if receipt.TimeoutMS != time.Millisecond.Milliseconds() || receipt.DurationMS < 0 {
		t.Fatalf("timeout bounds = %#v", receipt)
	}
	data, readErr := os.ReadFile(request.OutputPath)
	if readErr != nil || strings.Contains(string(data), "private timeout") {
		t.Fatalf("timeout receipt leaked output: %q err=%v", data, readErr)
	}
}

func TestRunOrchestratorSSHDispatchReturnsActionReceiptIfOutputIsClaimedDuringSSH(t *testing.T) {
	root := t.TempDir()
	_, _, request := readyOrchestratorSSHTestRequest(t, root)
	receipt, err := runOrchestratorSSHDispatch(request, func(context.Context, string, ...string) (salesforceCommandOutput, error) {
		if writeErr := os.WriteFile(request.OutputPath, []byte("claimed"), 0o600); writeErr != nil {
			t.Fatal(writeErr)
		}
		return salesforceCommandOutput{}, context.DeadlineExceeded
	})
	if err == nil || !receipt.ActionRequired || receipt.ActionCode != orchestratorSSHActionCode || receipt.Passed {
		t.Fatalf("uncertain receipt was discarded: receipt=%#v err=%v", receipt, err)
	}
}

func TestRunOrchestratorSSHDispatchTurnsSuccessfulCollisionIntoActionReceipt(t *testing.T) {
	root := t.TempDir()
	plan, lease, request := readyOrchestratorSSHTestRequest(t, root)
	planSHA, leaseSHA := orchestratorSSHTestInputHashes(t, request)
	completion, err := json.Marshal(OrchestratorWorkerOnceCompletion{CampaignID: lease.CampaignID, JobID: lease.JobID, ShardIndex: lease.ShardIndex, Generation: lease.Generation, Status: "worker-complete", SpecSHA256: plan.SpecSHA256, PlanSHA256: planSHA, LeaseSHA256: leaseSHA, OrchestratorBindingSHA256: strings.Repeat("a", 64), SalesforceShardSHA256: strings.Repeat("b", 64), OrgCleanupSHA256: strings.Repeat("c", 64)})
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := runOrchestratorSSHDispatch(request, func(context.Context, string, ...string) (salesforceCommandOutput, error) {
		if writeErr := os.WriteFile(request.OutputPath, []byte("claimed"), 0o600); writeErr != nil {
			t.Fatal(writeErr)
		}
		return salesforceCommandOutput{Stdout: append(completion, '\n')}, nil
	})
	if err == nil || !receipt.ActionRequired || receipt.ActionCode != orchestratorSSHActionCode || receipt.Passed || receipt.Status != "failed" {
		t.Fatalf("successful collision lost action signal: receipt=%#v err=%v", receipt, err)
	}
}

func readyOrchestratorSSHTestRequest(t *testing.T, root string) (OrchestratorCampaignPlan, OrchestratorLease, OrchestratorSSHDispatchRequest) {
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
	request := OrchestratorSSHDispatchRequest{Coordinator: orchestrator, Host: "operator@worker-a.example.internal", WorkerBin: filepath.Join(root, "glade-tools"), PlanPath: filepath.Join(root, "local-plan"), RemotePlanPath: filepath.Join(root, "remote-plan"), LeasePath: filepath.Join(root, "lease"), BundlePath: filepath.Join(root, "bundle"), TargetOrg: "scratch-a", SFBin: filepath.Join(root, "sf"), OutputRoot: filepath.Join(root, "output-root"), OutputPath: filepath.Join(root, "output")}
	writeJSONForOrchestratorSSHTest(t, request.PlanPath, plan)
	writeJSONForOrchestratorSSHTest(t, request.LeasePath, lease)
	return plan, lease, request
}

func writeJSONForOrchestratorSSHTest(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func orchestratorSSHTestInputHashes(t *testing.T, request OrchestratorSSHDispatchRequest) (string, string) {
	t.Helper()
	planSHA, err := sha256File(request.PlanPath)
	if err != nil {
		t.Fatal(err)
	}
	leaseSHA, err := sha256File(request.LeasePath)
	if err != nil {
		t.Fatal(err)
	}
	return planSHA, leaseSHA
}
