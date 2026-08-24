package corpusassurance

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFetchOrchestratorSSHRawPublishesBoundIdempotentTree(t *testing.T) {
	root := t.TempDir()
	plan, lease, dispatch := readyOrchestratorSSHTestRequest(t, root)
	remoteRoot := filepath.Join(root, "remote-raw")
	writeSSHRawFetchFiles(t, remoteRoot)
	dispatch.OutputRoot = remoteRoot
	planSHA, leaseSHA := orchestratorSSHTestInputHashes(t, dispatch)
	sshReceipt := orchestratorSSHFetchDispatchReceipt(t, dispatch, plan, lease, planSHA, leaseSHA, remoteRoot)
	sshReceiptPath := filepath.Join(root, "SSH_DISPATCH.json")
	if err := WriteNewJSON(sshReceiptPath, sshReceipt); err != nil {
		t.Fatal(err)
	}
	localRoot := filepath.Join(root, "private", "raw")
	calls := 0
	request := OrchestratorSSHRawFetchRequest{
		Plan: plan, Lease: lease, Dispatch: sshReceipt, Host: dispatch.Host, WorkerBin: dispatch.WorkerBin,
		PlanPath: dispatch.PlanPath, LeasePath: dispatch.LeasePath, DispatchPath: sshReceiptPath, BundlePath: dispatch.BundlePath,
		DevHub: "sealed-hub", TargetOrg: dispatch.TargetOrg, SFBin: dispatch.SFBin,
		RemoteRoot: remoteRoot, LocalRoot: localRoot,
		runner: func(_ context.Context, source, destination, _ string, checksum bool) (salesforceCommandOutput, error) {
			calls++
			if source != dispatch.Host+":"+remoteRoot+"/" {
				t.Fatalf("source = %q", source)
			}
			if checksum {
				return salesforceCommandOutput{}, nil
			}
			copySSHRawFetchFiles(t, remoteRoot, destination)
			return salesforceCommandOutput{}, nil
		},
	}
	receipt, err := FetchOrchestratorSSHRaw(request)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || receipt.Status != "fetched" || !receipt.Passed || receipt.CampaignID != plan.CampaignID || receipt.JobID != lease.JobID || receipt.PlanSHA256 != planSHA || receipt.LeaseSHA256 != leaseSHA || receipt.TreeManifestSHA256 == "" || receipt.ExecutedTools != request.Dispatch.ExecutedTools {
		t.Fatalf("receipt=%#v calls=%d", receipt, calls)
	}
	for _, name := range append(orchestratorSSHRawFileNames(), "TREE_MANIFEST.json", "SSH_FETCH.json") {
		if _, err := os.Lstat(filepath.Join(localRoot, name)); err != nil {
			t.Fatalf("missing fetched file %q: %v", name, err)
		}
	}
	sealed, err := os.ReadFile(filepath.Join(localRoot, "SSH_FETCH.json"))
	if err != nil || strings.Contains(string(sealed), dispatch.Host) || strings.Contains(string(sealed), remoteRoot) || strings.Contains(string(sealed), localRoot) {
		t.Fatalf("fetch receipt leaked private transport identity: %q err=%v", sealed, err)
	}
	if second, err := FetchOrchestratorSSHRaw(request); err != nil || second != receipt || calls != 2 {
		t.Fatalf("idempotent fetch=%#v err=%v calls=%d", second, err, calls)
	}
	if err := os.WriteFile(filepath.Join(localRoot, "ORG_PREFLIGHT.json"), []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := FetchOrchestratorSSHRaw(request); err == nil {
		t.Fatal("existing fetched tree mutation was accepted")
	}
}

func TestFetchOrchestratorSSHRawRejectsDispatchAndFetchedHashDrift(t *testing.T) {
	for _, test := range []struct {
		name       string
		mutate     func(*OrchestratorSSHRawFetchRequest)
		mutateCopy func(string)
	}{
		{name: "dispatch", mutate: func(request *OrchestratorSSHRawFetchRequest) { request.Host = "operator@other.example.internal" }},
		{name: "fetched hash", mutateCopy: func(destination string) {
			_ = os.WriteFile(filepath.Join(destination, "SALESFORCE_SHARD.json"), []byte("changed\n"), 0o600)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			plan, lease, dispatch := readyOrchestratorSSHTestRequest(t, root)
			remoteRoot := filepath.Join(root, "remote-raw")
			writeSSHRawFetchFiles(t, remoteRoot)
			dispatch.OutputRoot = remoteRoot
			planSHA, leaseSHA := orchestratorSSHTestInputHashes(t, dispatch)
			request := OrchestratorSSHRawFetchRequest{
				Plan: plan, Lease: lease, Dispatch: orchestratorSSHFetchDispatchReceipt(t, dispatch, plan, lease, planSHA, leaseSHA, remoteRoot),
				Host: dispatch.Host, WorkerBin: dispatch.WorkerBin, PlanPath: dispatch.PlanPath, LeasePath: dispatch.LeasePath,
				BundlePath: dispatch.BundlePath, DevHub: "sealed-hub", TargetOrg: dispatch.TargetOrg, SFBin: dispatch.SFBin,
				RemoteRoot: remoteRoot, LocalRoot: filepath.Join(root, "private", "raw"),
			}
			request.DispatchPath = filepath.Join(root, "SSH_DISPATCH.json")
			if err := WriteNewJSON(request.DispatchPath, request.Dispatch); err != nil {
				t.Fatal(err)
			}
			called := false
			request.runner = func(_ context.Context, _, destination, _ string, checksum bool) (salesforceCommandOutput, error) {
				called = true
				if !checksum {
					copySSHRawFetchFiles(t, remoteRoot, destination)
					if test.mutateCopy != nil {
						test.mutateCopy(destination)
					}
				}
				return salesforceCommandOutput{}, nil
			}
			if test.mutate != nil {
				test.mutate(&request)
			}
			if _, err := FetchOrchestratorSSHRaw(request); err == nil {
				t.Fatal("drifted fetch succeeded")
			}
			if test.name == "dispatch" && called {
				t.Fatal("copy ran before dispatch binding validation")
			}
			if _, err := os.Lstat(request.LocalRoot); !os.IsNotExist(err) {
				t.Fatalf("local root published after failure: %v", err)
			}
		})
	}
}

func orchestratorSSHFetchDispatchReceipt(t *testing.T, request OrchestratorSSHDispatchRequest, plan OrchestratorCampaignPlan, lease OrchestratorLease, planSHA, leaseSHA, remoteRoot string) OrchestratorSSHDispatchReceipt {
	t.Helper()
	command := orchestratorSSHWorkerOnceCommand(request, plan.Definition.Tools.SHA256, plan.Definition.ControlledInputSHA256[OrchestratorToolsAMD64Input], planSHA, leaseSHA, "sealed-hub")
	args := []string{"-o", "BatchMode=yes", "--", request.Host, command}
	return OrchestratorSSHDispatchReceipt{
		SchemaVersion: 1, CampaignID: plan.CampaignID, JobID: lease.JobID, ShardIndex: lease.ShardIndex, Generation: lease.Generation,
		Status: "worker-complete", CommandSHA256: commandSpecSHA256(ReplayCommand{Path: orchestratorSSHBinary, Args: args, Timeout: orchestratorSSHTimeout}),
		StdoutSHA256: strings.Repeat("1", 64), StderrSHA256: strings.Repeat("2", 64), ExitCode: 0, TimeoutMS: orchestratorSSHTimeout.Milliseconds(), Passed: true,
		SpecSHA256: plan.SpecSHA256, PlanSHA256: planSHA, LeaseSHA256: leaseSHA,
		OrchestratorBindingSHA256: sha256FileForTest(t, filepath.Join(remoteRoot, "ORCHESTRATOR_BINDING.json")),
		SalesforceShardSHA256:     sha256FileForTest(t, filepath.Join(remoteRoot, "SALESFORCE_SHARD.json")),
		OrgCleanupSHA256:          sha256FileForTest(t, filepath.Join(remoteRoot, "ORG_CLEANUP.json")),
		ExecutedTools:             RuntimeArtifact{Commit: plan.Definition.Tools.Commit, OS: "darwin", Arch: "arm64", SHA256: plan.Definition.Tools.SHA256},
	}
}

func writeSSHRawFetchFiles(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range orchestratorSSHRawFileNames() {
		mode := os.FileMode(0o600)
		if name == "ORCHESTRATOR_BINDING.json" {
			mode = 0o400
		}
		if err := os.WriteFile(filepath.Join(root, name), []byte(name+"\n"), mode); err != nil {
			t.Fatal(err)
		}
	}
}

func copySSHRawFetchFiles(t *testing.T, source, destination string) {
	t.Helper()
	for _, name := range orchestratorSSHRawFileNames() {
		data, err := os.ReadFile(filepath.Join(source, name))
		if err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(filepath.Join(source, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(destination, name), data, info.Mode().Perm()); err != nil {
			t.Fatalf("copy %q: %v", name, err)
		}
	}
}
