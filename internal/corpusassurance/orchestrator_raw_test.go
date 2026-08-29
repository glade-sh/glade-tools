package corpusassurance

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRunRawSalesforceShardRunsSealedLifecycleInOrder(t *testing.T) {
	root := t.TempDir()
	bundlePath := filepath.Join(root, "attempt", "salesforce-worker", "bundle", "bundle.json")
	if err := os.MkdirAll(filepath.Dir(bundlePath), 0o700); err != nil {
		t.Fatal(err)
	}
	bundle := OracleBundle{AttemptSHA256: strings.Repeat("a", 64), DevHub: "sealed-hub"}
	if err := WriteNewJSON(bundlePath, bundle); err != nil {
		t.Fatal(err)
	}
	_, plan, lease, _, _, oraclePlanPath := readyRawOrchestratorWorker(t)
	copyRawOraclePlan(t, oraclePlanPath, filepath.Join(filepath.Dir(bundlePath), "ORACLE_PLAN.json"))
	writeRawTransportManifest(t, filepath.Dir(bundlePath))
	outputRoot := filepath.Join(root, "raw-output")
	sfBin := filepath.Join(root, "sf")
	var phases []string
	request := RawSalesforceShardRequest{
		Plan: plan, Lease: lease, BundlePath: bundlePath, DevHub: bundle.DevHub, TargetOrg: "scratch-a", SFBin: sfBin, OutputRoot: outputRoot,
		validateBundle: func(string) error { return nil },
		orgCreate: func(value SalesforceOrgCreateRequest) (SalesforceOrgCreation, error) {
			phases = append(phases, "create")
			if value.DevHub != bundle.DevHub || value.Alias != "scratch-a" || value.OutputPath != filepath.Join(outputRoot, "ORG_CREATION.json") {
				t.Fatalf("create request = %#v", value)
			}
			if err := WriteNewJSON(value.OutputPath, SalesforceOrgCreation{}); err != nil {
				t.Fatal(err)
			}
			return SalesforceOrgCreation{}, nil
		},
		orgPreflight: func(value SalesforceOrgPreflightRequest) (SalesforceOrgPreflight, error) {
			phases = append(phases, "preflight")
			if value.OutputPath != filepath.Join(outputRoot, "ORG_PREFLIGHT.json") {
				t.Fatalf("preflight request = %#v", value)
			}
			return SalesforceOrgPreflight{}, nil
		},
		dispatch: func(value SalesforceDispatchRequest) (SalesforceDispatch, error) {
			phases = append(phases, "dispatch")
			attemptRoot, err := filepath.EvalSymlinks(filepath.Dir(filepath.Dir(filepath.Dir(bundlePath))))
			if err != nil {
				t.Fatal(err)
			}
			wantExecutor := filepath.Join(attemptRoot, "executor", fmt.Sprintf("shard-%d", lease.ShardIndex))
			if value.ExecutorRoot != wantExecutor || value.RunID != "assurance-"+bundle.AttemptSHA256[:16]+fmt.Sprintf("-shard-%d", lease.ShardIndex) || value.ShardCount != 2 || value.ShardIndex != lease.ShardIndex || value.Generation != lease.Generation {
				t.Fatalf("dispatch request = %#v", value)
			}
			return SalesforceDispatch{}, nil
		},
		shard: func(value SalesforceShardRequest) (SalesforceShard, error) {
			phases = append(phases, "shard")
			attemptRoot, err := filepath.EvalSymlinks(filepath.Dir(filepath.Dir(filepath.Dir(bundlePath))))
			if err != nil {
				t.Fatal(err)
			}
			if value.TargetOrg != "scratch-a" || value.SFBin != sfBin || value.ShardCount != 2 || !reflect.DeepEqual(value.ExecutorRoot, filepath.Join(attemptRoot, "executor", fmt.Sprintf("shard-%d", lease.ShardIndex))) {
				t.Fatalf("shard request = %#v", value)
			}
			return SalesforceShard{}, nil
		},
		orgCleanup: func(value SalesforceOrgCleanupRequest) (SalesforceOrgCleanup, error) {
			phases = append(phases, "cleanup")
			if value.DevHub != bundle.DevHub || value.PreflightPath != filepath.Join(outputRoot, "ORG_PREFLIGHT.json") || value.OutputPath != filepath.Join(outputRoot, "ORG_CLEANUP.json") {
				t.Fatalf("cleanup request = %#v", value)
			}
			return SalesforceOrgCleanup{}, nil
		},
	}
	result, err := RunRawSalesforceShard(request)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(phases, []string{"create", "preflight", "dispatch", "shard", "cleanup"}) {
		t.Fatalf("phases = %v", phases)
	}
	info, err := os.Stat(outputRoot)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("output root mode = %v, %v", info, err)
	}
	if _, err := os.Stat(filepath.Join(outputRoot, "ORCHESTRATOR_BINDING.json")); err != nil {
		t.Fatalf("binding receipt missing: %v", err)
	}
	wantFiles := SalesforceShardFiles{ShardPath: filepath.Join(outputRoot, "SALESFORCE_SHARD.json"), DispatchPath: filepath.Join(outputRoot, "SALESFORCE_DISPATCH.json"), CreationPath: filepath.Join(outputRoot, "ORG_CREATION.json"), CleanupPath: filepath.Join(outputRoot, "ORG_CLEANUP.json"), PreflightPath: filepath.Join(outputRoot, "ORG_PREFLIGHT.json")}
	if result.BindingPath != filepath.Join(outputRoot, "ORCHESTRATOR_BINDING.json") || result.SalesforceShardFiles != wantFiles {
		t.Fatalf("result paths = %q, %#v", result.BindingPath, result.SalesforceShardFiles)
	}
}

func TestRunRawSalesforceShardPassesThreeShardPlanToLifecycle(t *testing.T) {
	request, _, _ := rawSalesforceShardTestRequest(t)
	definition := request.Plan.Definition
	definition.Shards = [][]string{{"apex:System.One"}, {"apex:System.Three"}, {"apex:System.Two"}}
	plan, err := PlanOrchestratorCampaign(definition)
	if err != nil {
		t.Fatal(err)
	}
	job := plan.Jobs[0]
	request.Plan = plan
	request.Lease.CampaignID, request.Lease.JobID, request.Lease.Kind = plan.CampaignID, job.ID, job.Kind
	request.Lease.ShardIndex, request.Lease.SurfaceIDs = job.ShardIndex, job.SurfaceIDs
	request.orgCreate = func(value SalesforceOrgCreateRequest) (SalesforceOrgCreation, error) {
		if err := WriteNewJSON(value.OutputPath, SalesforceOrgCreation{}); err != nil {
			t.Fatal(err)
		}
		return SalesforceOrgCreation{}, nil
	}
	request.orgPreflight = func(SalesforceOrgPreflightRequest) (SalesforceOrgPreflight, error) {
		return SalesforceOrgPreflight{}, nil
	}
	request.dispatch = func(value SalesforceDispatchRequest) (SalesforceDispatch, error) {
		if value.ShardCount != 3 || value.ShardIndex != 0 {
			t.Fatalf("dispatch shard partition = %d/%d", value.ShardIndex, value.ShardCount)
		}
		return SalesforceDispatch{}, nil
	}
	request.shard = func(value SalesforceShardRequest) (SalesforceShard, error) {
		if value.ShardCount != 3 || value.ShardIndex != 0 {
			t.Fatalf("worker shard partition = %d/%d", value.ShardIndex, value.ShardCount)
		}
		return SalesforceShard{}, nil
	}
	request.orgCleanup = func(SalesforceOrgCleanupRequest) (SalesforceOrgCleanup, error) { return SalesforceOrgCleanup{}, nil }
	if _, err := RunRawSalesforceShard(request); err != nil {
		t.Fatal(err)
	}
}

func TestRunRawSalesforceShardUsesWorkerScopeWithoutChangingPlan(t *testing.T) {
	request, outputRoot, _ := rawSalesforceShardTestRequest(t)
	remoteScope := filepath.Join(t.TempDir(), "SALESFORCE_SURFACE_SCOPE.json")
	data, err := os.ReadFile(request.Plan.Definition.ScopePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(remoteScope, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(request.Plan.Definition.ScopePath); err != nil {
		t.Fatal(err)
	}
	request.ScopePath = remoteScope
	if err := validateOrchestratorWorkerPlanLeaseAtScope(request.Plan, request.Lease, remoteScope); err != nil {
		t.Fatalf("remote scope validation failed: %v", err)
	}
	request.orgCreate = func(SalesforceOrgCreateRequest) (SalesforceOrgCreation, error) {
		return SalesforceOrgCreation{}, errors.New("scope override passed validation")
	}
	_, err = RunRawSalesforceShard(request)
	if err == nil || !strings.Contains(err.Error(), "scope override passed validation") {
		t.Fatalf("worker scope override error = %v", err)
	}
	if request.Plan.Definition.ScopePath == remoteScope {
		t.Fatal("worker scope override changed the sealed plan")
	}
	if _, err := os.Stat(outputRoot); err != nil {
		t.Fatalf("worker lifecycle did not reach output creation: %v", err)
	}
}

func TestRunRawSalesforceShardRejectsWorkerScopeDriftBeforeSideEffects(t *testing.T) {
	request, outputRoot, _ := rawSalesforceShardTestRequest(t)
	request.ScopePath = filepath.Join(t.TempDir(), "SALESFORCE_SURFACE_SCOPE.json")
	if err := os.WriteFile(request.ScopePath, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	called := false
	request.orgCreate = func(SalesforceOrgCreateRequest) (SalesforceOrgCreation, error) {
		called = true
		return SalesforceOrgCreation{}, nil
	}
	if _, err := RunRawSalesforceShard(request); err == nil || called {
		t.Fatalf("drifted worker scope accepted: err=%v called=%t", err, called)
	}
	if _, err := os.Lstat(outputRoot); !os.IsNotExist(err) {
		t.Fatalf("output root exists after scope drift: %v", err)
	}
}

func TestRunRawSalesforceShardRejectsSealedOraclePlanDriftBeforeOutput(t *testing.T) {
	request, outputRoot, _ := rawSalesforceShardTestRequest(t)
	data, err := json.Marshal(OraclePlan{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(request.BundlePath), "ORACLE_PLAN.json"), append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	called := false
	request.orgCreate = func(SalesforceOrgCreateRequest) (SalesforceOrgCreation, error) {
		called = true
		return SalesforceOrgCreation{}, nil
	}
	if _, err := RunRawSalesforceShard(request); err == nil {
		t.Fatal("sealed oracle plan drift succeeded")
	}
	if called {
		t.Fatal("org creation ran after sealed oracle plan drift")
	}
	if _, err := os.Lstat(outputRoot); !os.IsNotExist(err) {
		t.Fatalf("output root exists after sealed oracle plan drift: %v", err)
	}
}

func TestRunRawSalesforceShardRejectsFilterPartitionDriftBeforeOutput(t *testing.T) {
	request, outputRoot, _ := rawSalesforceShardTestRequest(t)
	writeJSONValue(t, filepath.Join(filepath.Dir(request.BundlePath), "fixture-manifest.json"), oracleTransportManifest{Fixtures: []oracleTransportFixture{
		{SurfaceIDs: []string{"apex:System.Three"}},
		{SurfaceIDs: []string{"apex:System.One"}},
		{SurfaceIDs: []string{"apex:System.Two"}},
	}})
	called := false
	request.orgCreate = func(SalesforceOrgCreateRequest) (SalesforceOrgCreation, error) {
		called = true
		return SalesforceOrgCreation{}, nil
	}
	if _, err := RunRawSalesforceShard(request); err == nil {
		t.Fatal("filter partition drift succeeded")
	}
	if called {
		t.Fatal("org creation ran after filter partition drift")
	}
	if _, err := os.Lstat(outputRoot); !os.IsNotExist(err) {
		t.Fatalf("output root exists after filter partition drift: %v", err)
	}
}

func TestRunRawSalesforceShardCleansUpAfterPreflightFailure(t *testing.T) {
	request, outputRoot, bundle := rawSalesforceShardTestRequest(t)
	primary := errors.New("preflight failed")
	cleanupCalls := 0
	request.orgCreate = func(SalesforceOrgCreateRequest) (SalesforceOrgCreation, error) {
		if err := WriteNewJSON(filepath.Join(outputRoot, "ORG_CREATION.json"), SalesforceOrgCreation{}); err != nil {
			t.Fatal(err)
		}
		return SalesforceOrgCreation{}, nil
	}
	request.orgPreflight = func(SalesforceOrgPreflightRequest) (SalesforceOrgPreflight, error) {
		return SalesforceOrgPreflight{}, primary
	}
	request.orgCleanup = func(value SalesforceOrgCleanupRequest) (SalesforceOrgCleanup, error) {
		cleanupCalls++
		if value.PreflightPath != "" || value.DevHub != bundle.DevHub || value.OutputPath != filepath.Join(outputRoot, "ORG_CLEANUP.json") {
			t.Fatalf("cleanup request = %#v", value)
		}
		return SalesforceOrgCleanup{}, nil
	}
	_, err := RunRawSalesforceShard(request)
	if !errors.Is(err, primary) {
		t.Fatalf("error = %v, want primary error", err)
	}
	if cleanupCalls != 1 {
		t.Fatalf("cleanup calls = %d, want 1", cleanupCalls)
	}
}

func TestRunRawSalesforceShardJoinsCleanupFailureAfterCreateFailure(t *testing.T) {
	request, _, bundle := rawSalesforceShardTestRequest(t)
	primary := errors.New("create failed after reservation")
	cleanupFailure := errors.New("cleanup failed")
	cleanupCalls := 0
	request.orgCreate = func(value SalesforceOrgCreateRequest) (SalesforceOrgCreation, error) {
		bundleSHA, err := sha256File(request.BundlePath)
		if err != nil {
			t.Fatal(err)
		}
		if err := WriteNewJSON(value.OutputPath+".reservation", salesforceOrgReservation{SchemaVersion: 1, BundleSHA256: bundleSHA, DevHub: bundle.DevHub, Alias: value.Alias, Marker: "marker"}); err != nil {
			t.Fatal(err)
		}
		return SalesforceOrgCreation{}, primary
	}
	request.orgCleanup = func(value SalesforceOrgCleanupRequest) (SalesforceOrgCleanup, error) {
		cleanupCalls++
		if value.PreflightPath != "" {
			t.Fatalf("reserved cleanup unexpectedly used preflight: %#v", value)
		}
		return SalesforceOrgCleanup{}, cleanupFailure
	}
	_, err := RunRawSalesforceShard(request)
	if !errors.Is(err, primary) || !errors.Is(err, cleanupFailure) {
		t.Fatalf("error = %v, want joined primary and cleanup errors", err)
	}
	if cleanupCalls != 1 {
		t.Fatalf("cleanup calls = %d, want 1", cleanupCalls)
	}
}

func TestRunRawSalesforceShardDoesNotCleanupCreateFailureBeforeReservation(t *testing.T) {
	request, _, _ := rawSalesforceShardTestRequest(t)
	primary := errors.New("create failed before reservation")
	cleanupCalls := 0
	request.orgCreate = func(SalesforceOrgCreateRequest) (SalesforceOrgCreation, error) {
		return SalesforceOrgCreation{}, primary
	}
	request.orgCleanup = func(SalesforceOrgCleanupRequest) (SalesforceOrgCleanup, error) {
		cleanupCalls++
		return SalesforceOrgCleanup{}, nil
	}
	_, err := RunRawSalesforceShard(request)
	if !errors.Is(err, primary) {
		t.Fatalf("error = %v, want primary error", err)
	}
	if cleanupCalls != 0 {
		t.Fatalf("cleanup calls = %d, want 0", cleanupCalls)
	}
}

func rawSalesforceShardTestRequest(t *testing.T) (RawSalesforceShardRequest, string, OracleBundle) {
	t.Helper()
	root := t.TempDir()
	bundlePath := filepath.Join(root, "attempt", "salesforce-worker", "bundle", "bundle.json")
	if err := os.MkdirAll(filepath.Dir(bundlePath), 0o700); err != nil {
		t.Fatal(err)
	}
	bundle := OracleBundle{AttemptSHA256: strings.Repeat("a", 64), DevHub: "sealed-hub"}
	if err := WriteNewJSON(bundlePath, bundle); err != nil {
		t.Fatal(err)
	}
	_, plan, lease, _, _, oraclePlanPath := readyRawOrchestratorWorker(t)
	copyRawOraclePlan(t, oraclePlanPath, filepath.Join(filepath.Dir(bundlePath), "ORACLE_PLAN.json"))
	writeRawTransportManifest(t, filepath.Dir(bundlePath))
	outputRoot := filepath.Join(root, "raw-output")
	return RawSalesforceShardRequest{Plan: plan, Lease: lease, BundlePath: bundlePath, DevHub: bundle.DevHub, TargetOrg: "scratch-a", SFBin: filepath.Join(root, "sf"), OutputRoot: outputRoot, validateBundle: func(string) error { return nil }}, outputRoot, bundle
}

func copyRawOraclePlan(t *testing.T, source, destination string) {
	t.Helper()
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeRawTransportManifest(t *testing.T, bundleRoot string) {
	t.Helper()
	writeJSONValue(t, filepath.Join(bundleRoot, "fixture-manifest.json"), oracleTransportManifest{Fixtures: []oracleTransportFixture{
		{SurfaceIDs: []string{"apex:System.One"}, SalesforceEligible: true},
		{SurfaceIDs: []string{"apex:System.Three"}, SalesforceEligible: true},
		{SurfaceIDs: []string{"apex:System.Two"}, SalesforceEligible: true},
	}})
}

func readyRawOrchestratorWorker(t *testing.T) (*Orchestrator, OrchestratorCampaignPlan, OrchestratorLease, time.Time, string, string) {
	t.Helper()
	root := t.TempDir()
	scope, batch := writeSurfaceOracleIndexInputs(t, root)
	oraclePlanPath := filepath.Join(root, "ORACLE_PLAN.json")
	writeSyntheticOrchestratorReconciliation(t, batch)
	definition := testOrchestratorDefinition(t, scope, [][]string{{"apex:System.One", "apex:System.Two"}, {"apex:System.Three"}})
	definition.Candidate = OrchestratorArtifact{Commit: strings.Repeat("1", 40), SHA256: surfaceOracleFileSHA256(t, filepath.Join(batch, "bin", "glade-sealed"))}
	definition.Tools = OrchestratorArtifact{Commit: strings.Repeat("2", 40), SHA256: surfaceOracleFileSHA256(t, filepath.Join(batch, "bin", "glade-tools"))}
	writeJSONValue(t, oraclePlanPath, OraclePlan{
		Candidate: RuntimeArtifact{Commit: definition.Candidate.Commit, OS: runtime.GOOS, Arch: runtime.GOARCH, SHA256: definition.Candidate.SHA256},
		Tools:     RuntimeArtifact{Commit: definition.Tools.Commit, OS: runtime.GOOS, Arch: runtime.GOARCH, SHA256: definition.Tools.SHA256},
		Rows: []OraclePlanRow{
			{SurfaceID: "apex:System.One", Action: oracleRuntime},
			{SurfaceID: "apex:System.Three", Action: oracleRuntime},
			{SurfaceID: "apex:System.Two", Action: oracleCompile},
		},
	})
	definition.ControlledInputSHA256["oracle-plan"] = surfaceOracleFileSHA256(t, oraclePlanPath)
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
	if _, err := WriteOrchestratorBatchBinding(filepath.Join(batch, "evidence", "ORCHESTRATOR_BINDING.json"), plan, lease); err != nil {
		t.Fatal(err)
	}
	return orchestrator, plan, lease, now, batch, oraclePlanPath
}

func TestRunRawSalesforceShardRejectsPlanLeaseDriftBeforeSideEffects(t *testing.T) {
	root := filepath.Join(t.TempDir(), "raw")
	called := false
	_, err := RunRawSalesforceShard(RawSalesforceShardRequest{
		Plan:       OrchestratorCampaignPlan{},
		Lease:      OrchestratorLease{},
		BundlePath: filepath.Join(t.TempDir(), "bundle.json"),
		DevHub:     "sealed-hub",
		TargetOrg:  "scratch-a",
		SFBin:      filepath.Join(t.TempDir(), "sf"),
		OutputRoot: root,
		orgCreate: func(SalesforceOrgCreateRequest) (SalesforceOrgCreation, error) {
			called = true
			return SalesforceOrgCreation{}, nil
		},
	})
	if err == nil {
		t.Fatal("plan/lease drift succeeded")
	}
	if called {
		t.Fatal("org creation ran for plan/lease drift")
	}
	if _, statErr := os.Lstat(root); !os.IsNotExist(statErr) {
		t.Fatalf("output root exists after plan/lease drift: %v", statErr)
	}
}

func TestRunRawSalesforceShardRejectsReservedDevHubDriftBeforeOutput(t *testing.T) {
	request, outputRoot, _ := rawSalesforceShardTestRequest(t)
	request.DevHub = "other-hub"
	if _, err := RunRawSalesforceShard(request); err == nil || !strings.Contains(err.Error(), "reserved Dev Hub") {
		t.Fatalf("Dev Hub drift error = %v", err)
	}
	if _, err := os.Lstat(outputRoot); !os.IsNotExist(err) {
		t.Fatalf("output root exists after Dev Hub drift: %v", err)
	}
}

func TestRunRawSalesforceShardRejectsUnsafeOrExpiredWorkerLeaseBeforeOutput(t *testing.T) {
	for _, mutate := range []func(*RawSalesforceShardRequest){
		func(request *RawSalesforceShardRequest) { request.Lease.Worker = "" },
		func(request *RawSalesforceShardRequest) { request.Lease.Worker = "worker;unsafe" },
		func(request *RawSalesforceShardRequest) {
			request.Lease.LeaseUntil = time.Now().UTC().Add(-time.Second)
		},
		func(request *RawSalesforceShardRequest) {
			request.Lease.LeaseUntil = time.Now().UTC().Add(time.Minute)
		},
	} {
		request, outputRoot, _ := rawSalesforceShardTestRequest(t)
		mutate(&request)
		if _, err := RunRawSalesforceShard(request); err == nil {
			t.Fatal("unsafe or expired worker lease accepted")
		}
		if _, err := os.Lstat(outputRoot); !os.IsNotExist(err) {
			t.Fatalf("output root exists after invalid lease: %v", err)
		}
	}
}

func TestOrchestratorWorkerOnceCompletionFromRawBindsSpecAndLifecycleHashes(t *testing.T) {
	_, plan, lease, _, _, _ := readyRawOrchestratorWorker(t)
	root := t.TempDir()
	binding, shard, cleanup := filepath.Join(root, "ORCHESTRATOR_BINDING.json"), filepath.Join(root, "SALESFORCE_SHARD.json"), filepath.Join(root, "ORG_CLEANUP.json")
	for _, path := range []string{binding, shard, cleanup} {
		if err := os.WriteFile(path, []byte(path), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	completion, err := OrchestratorWorkerOnceCompletionFromRaw(plan, lease, strings.Repeat("4", 64), strings.Repeat("5", 64), RawSalesforceShardResult{BindingPath: binding, SalesforceShardFiles: SalesforceShardFiles{ShardPath: shard, CleanupPath: cleanup}})
	if err != nil {
		t.Fatal(err)
	}
	if completion.SpecSHA256 != plan.SpecSHA256 || completion.PlanSHA256 != strings.Repeat("4", 64) || completion.LeaseSHA256 != strings.Repeat("5", 64) || !sha256Pattern.MatchString(completion.OrchestratorBindingSHA256) || !sha256Pattern.MatchString(completion.SalesforceShardSHA256) || !sha256Pattern.MatchString(completion.OrgCleanupSHA256) {
		t.Fatalf("completion = %#v", completion)
	}
}
