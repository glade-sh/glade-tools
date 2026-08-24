package corpusassurance

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCreateOrchestratorSalesforceReconciliationRequiresExactInputs(t *testing.T) {
	if _, err := CreateOrchestratorSalesforceReconciliation(OrchestratorSalesforceReconciliationRequest{}); err == nil {
		t.Fatal("accepted an unbound orchestrator Salesforce shard")
	}
}

func TestCreateAndVerifyOrchestratorSalesforceReconciliationAfterWorkerDeletion(t *testing.T) {
	fixture := newOrchestratorSalesforceReconciliationFixture(t)
	retained := t.TempDir()
	receiptPath := filepath.Join(retained, "SALESFORCE_RECONCILIATION.json")
	packetPath := filepath.Join(retained, "packet")
	receipt, err := CreateOrchestratorSalesforceReconciliation(OrchestratorSalesforceReconciliationRequest{
		Plan: fixture.plan, Lease: fixture.lease, OraclePlanPath: fixture.oraclePlanPath,
		BindingPath: fixture.bindingPath, ShardFiles: fixture.files,
		PacketOutput: packetPath, OutputPath: receiptPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.SchemaVersion != 2 || len(receipt.Shards) != 1 || len(receipt.Rows) != 1 || receipt.Rows[0].SurfaceID != "apex:Runtime.run" || receipt.Rows[0].Action != oracleRuntime || !receipt.Rows[0].Passed || !sha256Pattern.MatchString(receipt.OrchestratorPlanSHA256) || !sha256Pattern.MatchString(receipt.OrchestratorBindingSHA256) {
		t.Fatalf("orchestrator reconciliation = %#v", receipt)
	}
	if _, err := os.Stat(filepath.Join(packetPath, "ORCHESTRATOR_PLAN.json")); !os.IsNotExist(err) {
		t.Fatalf("packet retained a standalone orchestrator plan: %v", err)
	}
	for _, path := range []string{fixture.files.DispatchPath, fixture.files.PreflightPath, fixture.files.CreationPath, fixture.files.CleanupPath} {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.RemoveAll(fixture.workerRoot); err != nil {
		t.Fatal(err)
	}
	if err := VerifyOrchestratorSalesforceReconciliation(fixture.plan, fixture.lease, receiptPath, packetPath); err != nil {
		t.Fatal(err)
	}
}

func TestCreateOrchestratorSalesforceReconciliationRejectsDrift(t *testing.T) {
	fixture := newOrchestratorSalesforceReconciliationFixture(t)
	shard, _, err := readExactJSONBytes[SalesforceShard](fixture.files.ShardPath)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*SalesforceShard){
		"wrong subset": func(value *SalesforceShard) { value.Results[0].SurfaceID = "apex:Local.only" },
		"wrong index":  func(value *SalesforceShard) { value.ShardIndex = 1 },
		"wrong count":  func(value *SalesforceShard) { value.ShardCount = 1 },
		"wrong action": func(value *SalesforceShard) { value.Results[0].Kind = oracleCompile },
		"failed row":   func(value *SalesforceShard) { value.Results[0].Passed = false },
	} {
		t.Run(name, func(t *testing.T) {
			changed := shard
			changed.Results = append([]SalesforceSurfaceResult(nil), shard.Results...)
			mutate(&changed)
			shardPath := filepath.Join(t.TempDir(), "SALESFORCE_SHARD.json")
			if err := WriteNewJSON(shardPath, changed); err != nil {
				t.Fatal(err)
			}
			request := fixture.request(t.TempDir())
			request.ShardFiles.ShardPath = shardPath
			if _, err := CreateOrchestratorSalesforceReconciliation(request); err == nil {
				t.Fatalf("accepted %s", name)
			}
		})
	}
	t.Run("binding mode", func(t *testing.T) {
		if err := os.Chmod(fixture.bindingPath, 0o600); err != nil {
			t.Fatal(err)
		}
		defer os.Chmod(fixture.bindingPath, 0o400)
		if _, err := CreateOrchestratorSalesforceReconciliation(fixture.request(t.TempDir())); err == nil {
			t.Fatal("accepted a writable orchestrator binding")
		}
	})
	t.Run("binding content", func(t *testing.T) {
		binding, _, err := readExactJSONBytes[OrchestratorBatchBinding](fixture.bindingPath)
		if err != nil {
			t.Fatal(err)
		}
		binding.Generation++
		path := filepath.Join(t.TempDir(), "ORCHESTRATOR_BINDING.json")
		if err := WriteNewJSON(path, binding); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o400); err != nil {
			t.Fatal(err)
		}
		request := fixture.request(t.TempDir())
		request.BindingPath = path
		if _, err := CreateOrchestratorSalesforceReconciliation(request); err == nil {
			t.Fatal("accepted drifted orchestrator binding")
		}
	})
	t.Run("controlled oracle", func(t *testing.T) {
		definition := fixture.plan.Definition
		definition.ControlledInputSHA256 = map[string]string{"oracle-plan": strings.Repeat("0", 64)}
		plan, err := PlanOrchestratorCampaign(definition)
		if err != nil {
			t.Fatal(err)
		}
		request := fixture.request(t.TempDir())
		request.Plan, request.Lease = plan, leaseForOrchestratorPlan(plan, 0)
		if _, err := CreateOrchestratorSalesforceReconciliation(request); err == nil {
			t.Fatal("accepted wrong controlled oracle-plan hash")
		}
	})
	t.Run("empty oracle intersection", func(t *testing.T) {
		definition := fixture.plan.Definition
		definition.Shards = [][]string{{"apex:Local.only"}, {"apex:Mock.run", "apex:Other.only", "apex:Runtime.run", "apex:Shape.run"}}
		plan, err := PlanOrchestratorCampaign(definition)
		if err != nil {
			t.Fatal(err)
		}
		request := fixture.request(t.TempDir())
		request.Plan, request.Lease = plan, leaseForOrchestratorPlan(plan, 0)
		if _, err := CreateOrchestratorSalesforceReconciliation(request); err == nil {
			t.Fatal("accepted an empty Salesforce-required lease intersection")
		}
	})
	t.Run("lease subset", func(t *testing.T) {
		request := fixture.request(t.TempDir())
		request.Lease.SurfaceIDs = []string{"apex:Runtime.run"}
		if _, err := CreateOrchestratorSalesforceReconciliation(request); err == nil {
			t.Fatal("accepted caller-selected lease subset")
		}
	})
	t.Run("non partition plan", func(t *testing.T) {
		request := fixture.request(t.TempDir())
		request.Plan.Definition.Shards[1] = append(request.Plan.Definition.Shards[1], "apex:Runtime.run")
		if _, err := CreateOrchestratorSalesforceReconciliation(request); err == nil {
			t.Fatal("accepted a plan that is not the exact two-shard partition")
		}
	})
}

func TestOrchestratorSalesforceExpectedSurfaceIDsRequiresExactCampaignPartition(t *testing.T) {
	fixture := newOrchestratorSalesforceReconciliationFixture(t)
	oraclePlan, _, err := readExactJSONBytes[OraclePlan](fixture.oraclePlanPath)
	if err != nil {
		t.Fatal(err)
	}
	missing := oraclePlan
	missing.Rows = append([]OraclePlanRow(nil), oraclePlan.Rows[1:]...)
	if _, err := orchestratorSalesforceExpectedSurfaceIDs(missing, fixture.plan, fixture.lease); err == nil {
		t.Fatal("accepted campaign surfaces outside the Oracle plan")
	}
	oraclePlan.Rows = append(oraclePlan.Rows, OraclePlanRow{SurfaceID: "apex:Outside.run", Action: oracleRuntime})
	if _, err := orchestratorSalesforceExpectedSurfaceIDs(oraclePlan, fixture.plan, fixture.lease); err == nil {
		t.Fatal("accepted an Oracle-required surface outside the campaign partition")
	}
}

func TestVerifyOrchestratorSalesforceReconciliationRejectsTamper(t *testing.T) {
	fixture := newOrchestratorSalesforceReconciliationFixture(t)
	retained := t.TempDir()
	receiptPath, packetPath := filepath.Join(retained, "receipt.json"), filepath.Join(retained, "packet")
	if _, err := CreateOrchestratorSalesforceReconciliation(OrchestratorSalesforceReconciliationRequest{
		Plan: fixture.plan, Lease: fixture.lease, OraclePlanPath: fixture.oraclePlanPath, BindingPath: fixture.bindingPath,
		ShardFiles: fixture.files, PacketOutput: packetPath, OutputPath: receiptPath,
	}); err != nil {
		t.Fatal(err)
	}
	prefix := filepath.Join(packetPath, "shards", "00")
	mutations := map[string]struct {
		path   string
		mutate func([]byte) []byte
	}{
		"receipt": {receiptPath, func(data []byte) []byte {
			return bytes.Replace(data, []byte(`"status":"pass"`), []byte(`"status":"fail"`), 1)
		}},
		"packet": {filepath.Join(packetPath, reconciliationPacketManifestName), func(data []byte) []byte { return append(data, '\n') }},
		"binding": {filepath.Join(packetPath, "ORCHESTRATOR_BINDING.json"), func(data []byte) []byte {
			return bytes.Replace(data, []byte(`"generation":1`), []byte(`"generation":2`), 1)
		}},
		"lifecycle": {filepath.Join(prefix, "ORG_CLEANUP.json"), func(data []byte) []byte { return append(data, '\n') }},
		"executor":  {filepath.Join(prefix, "executor", "filter", "results.json"), func(data []byte) []byte { return append(data, '\n') }},
		"oracle":    {filepath.Join(packetPath, "ORACLE_PLAN.json"), func(data []byte) []byte { return append(data, '\n') }},
		"duplicate keys": {receiptPath, func(data []byte) []byte {
			return bytes.Replace(data, []byte(`"status":"pass"`), []byte(`"status":"pass","status":"pass"`), 1)
		}},
	}
	for name, mutation := range mutations {
		t.Run(name, func(t *testing.T) {
			withMutatedReconciliationFile(t, mutation.path, mutation.mutate, func() {
				if err := VerifyOrchestratorSalesforceReconciliation(fixture.plan, fixture.lease, receiptPath, packetPath); err == nil {
					t.Fatalf("accepted %s tamper", name)
				}
			})
		})
	}
	t.Run("receipt plan hash", func(t *testing.T) {
		plan := fixture.plan
		plan.SpecSHA256 = strings.Repeat("0", 64)
		if err := VerifyOrchestratorSalesforceReconciliation(plan, fixture.lease, receiptPath, packetPath); err == nil {
			t.Fatal("accepted a different orchestrator plan")
		}
	})
	t.Run("lease generation", func(t *testing.T) {
		lease := fixture.lease
		lease.Generation++
		if err := VerifyOrchestratorSalesforceReconciliation(fixture.plan, lease, receiptPath, packetPath); err == nil {
			t.Fatal("accepted a different orchestrator lease")
		}
	})
	t.Run("binding mode", func(t *testing.T) {
		path := filepath.Join(packetPath, "ORCHESTRATOR_BINDING.json")
		if err := os.Chmod(path, 0o600); err != nil {
			t.Fatal(err)
		}
		defer os.Chmod(path, 0o400)
		if err := VerifyOrchestratorSalesforceReconciliation(fixture.plan, fixture.lease, receiptPath, packetPath); err == nil {
			t.Fatal("accepted changed retained binding mode")
		}
	})
	for name, path := range map[string]string{
		"receipt mode":          receiptPath,
		"manifest mode":         filepath.Join(packetPath, reconciliationPacketManifestName),
		"packet root mode":      packetPath,
		"packet directory mode": filepath.Join(packetPath, "shards"),
	} {
		t.Run(name, func(t *testing.T) {
			info, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, 0o750); err != nil {
				t.Fatal(err)
			}
			defer os.Chmod(path, info.Mode().Perm())
			if err := VerifyOrchestratorSalesforceReconciliation(fixture.plan, fixture.lease, receiptPath, packetPath); err == nil {
				t.Fatalf("accepted changed %s", name)
			}
		})
	}
	t.Run("symlink manifest", func(t *testing.T) {
		path := filepath.Join(packetPath, reconciliationPacketManifestName)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(t.TempDir(), reconciliationPacketManifestName)
		if err := os.WriteFile(target, data, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, path); err != nil {
			t.Fatal(err)
		}
		defer func() {
			_ = os.Remove(path)
			_ = os.WriteFile(path, data, 0o600)
		}()
		if err := VerifyOrchestratorSalesforceReconciliation(fixture.plan, fixture.lease, receiptPath, packetPath); err == nil {
			t.Fatal("accepted a symlinked packet manifest")
		}
	})
	t.Run("symlink receipt", func(t *testing.T) {
		data, err := os.ReadFile(receiptPath)
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(t.TempDir(), "receipt.json")
		if err := os.WriteFile(target, data, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(receiptPath); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, receiptPath); err != nil {
			t.Fatal(err)
		}
		defer func() {
			_ = os.Remove(receiptPath)
			_ = os.WriteFile(receiptPath, data, 0o600)
		}()
		if err := VerifyOrchestratorSalesforceReconciliation(fixture.plan, fixture.lease, receiptPath, packetPath); err == nil {
			t.Fatal("accepted a symlinked reconciliation receipt")
		}
	})
}

type orchestratorSalesforceReconciliationFixture struct {
	plan             OrchestratorCampaignPlan
	lease            OrchestratorLease
	oraclePlanPath   string
	bindingPath      string
	files            SalesforceShardFiles
	workerRoot       string
	oracleBundleRoot string
	localProofPath   string
}

func (fixture orchestratorSalesforceReconciliationFixture) request(outputRoot string) OrchestratorSalesforceReconciliationRequest {
	return OrchestratorSalesforceReconciliationRequest{
		Plan: fixture.plan, Lease: fixture.lease, OraclePlanPath: fixture.oraclePlanPath,
		BindingPath: fixture.bindingPath, ShardFiles: fixture.files,
		PacketOutput: filepath.Join(outputRoot, "packet"), OutputPath: filepath.Join(outputRoot, "receipt.json"),
	}
}

func TestVerifyOrchestratorSalesforceReconciliationRejectsResealedWrongAction(t *testing.T) {
	fixture := newOrchestratorSalesforceReconciliationFixture(t)
	retained := t.TempDir()
	receiptPath, packetPath := filepath.Join(retained, "receipt.json"), filepath.Join(retained, "packet")
	if _, err := CreateOrchestratorSalesforceReconciliation(OrchestratorSalesforceReconciliationRequest{
		Plan: fixture.plan, Lease: fixture.lease, OraclePlanPath: fixture.oraclePlanPath, BindingPath: fixture.bindingPath,
		ShardFiles: fixture.files, PacketOutput: packetPath, OutputPath: receiptPath,
	}); err != nil {
		t.Fatal(err)
	}
	shardPath := filepath.Join(packetPath, "shards", "00", "SALESFORCE_SHARD.json")
	shard, _, err := readExactJSONBytes[SalesforceShard](shardPath)
	if err != nil {
		t.Fatal(err)
	}
	shard.Results[0].Kind = oracleCompile
	overwriteReconciliationJSON(t, shardPath, shard)
	manifestPath := filepath.Join(packetPath, reconciliationPacketManifestName)
	manifest, _, err := readExactJSONBytes[reportPacketManifest](manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	for index := range manifest.Files {
		if manifest.Files[index].Name == "shards/00/SALESFORCE_SHARD.json" {
			manifest.Files[index].SHA256 = localProofFileSHA256(t, shardPath)
		}
	}
	overwriteReconciliationJSON(t, manifestPath, manifest)
	receipt, _, err := readExactJSONBytes[SalesforceReconciliation](receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	receipt.Rows[0].Action = oracleCompile
	receipt.Shards[0].InputSHA256["shard"] = localProofFileSHA256(t, shardPath)
	receipt.PacketManifestSHA256 = localProofFileSHA256(t, manifestPath)
	overwriteReconciliationJSON(t, receiptPath, receipt)
	if err := VerifyOrchestratorSalesforceReconciliation(fixture.plan, fixture.lease, receiptPath, packetPath); err == nil {
		t.Fatal("accepted a fully resealed compile result for a runtime Oracle row")
	}
}

func leaseForOrchestratorPlan(plan OrchestratorCampaignPlan, shardIndex int) OrchestratorLease {
	job := plan.Jobs[shardIndex]
	return OrchestratorLease{CampaignID: plan.CampaignID, JobID: job.ID, Kind: job.Kind, ShardIndex: job.ShardIndex, SurfaceIDs: append([]string(nil), job.SurfaceIDs...), Generation: 1, Worker: "worker-a", LeaseUntil: time.Now().UTC().Add(time.Minute), DurationMS: 60_000}
}

func withMutatedReconciliationFile(t *testing.T, path string, mutate func([]byte) []byte, verify func()) {
	t.Helper()
	snapshot, err := readRegularFileSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	changed := mutate(append([]byte(nil), snapshot.Data...))
	if bytes.Equal(changed, snapshot.Data) {
		t.Fatal("test mutation did not change the file")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, changed, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, snapshot.Mode.Perm()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = os.Chmod(path, 0o600)
		_ = os.WriteFile(path, snapshot.Data, 0o600)
		_ = os.Chmod(path, snapshot.Mode.Perm())
	}()
	verify()
}

func overwriteReconciliationJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	mode, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode.Mode().Perm()); err != nil {
		t.Fatal(err)
	}
}

func newOrchestratorSalesforceReconciliationFixture(t *testing.T) orchestratorSalesforceReconciliationFixture {
	t.Helper()
	inputs := oracleBundleTestInputsForLocalProof(t)
	manifest, _, err := readExactJSONBytes[LocalProofFixtureManifest](inputs.fixtureManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range manifest.Fixtures {
		if fixture.ID == "shape" {
			manifest.SalesforceFixtures = append(manifest.SalesforceFixtures, fixture)
		}
	}
	overwriteReconciliationJSON(t, inputs.fixtureManifestPath, manifest)
	profile, _, err := readExactJSONBytes[AssuranceProfile](inputs.profilePath)
	if err != nil {
		t.Fatal(err)
	}
	profile.FixtureManifestSHA256 = localProofFileSHA256(t, inputs.fixtureManifestPath)
	overwriteReconciliationJSON(t, inputs.profilePath, profile)
	inputs.plan.ProfileSHA256 = localProofFileSHA256(t, inputs.profilePath)
	inputs.plan.Rows = []OraclePlanRow{
		{SurfaceID: "apex:Local.only", Action: oracleLocalContractOnly, ExclusionClass: "local-contract", ExclusionReason: "local-only proof"},
		{SurfaceID: "apex:Mock.run", Action: oracleLocalContractOnly, ExclusionClass: "local-contract", ExclusionReason: "local-only mock"},
		{SurfaceID: "apex:Other.only", Action: oracleWaiver, ExclusionClass: "waiver", ExclusionReason: "explicit non-parity waiver"},
		{SurfaceID: "apex:Runtime.run", Action: oracleRuntime},
		{SurfaceID: "apex:Shape.run", Action: oracleCompile},
	}
	overwriteReconciliationJSON(t, inputs.planPath, inputs.plan)
	authority, _, err := readExactJSONBytes[ExclusionAuthority](inputs.authorityPath)
	if err != nil {
		t.Fatal(err)
	}
	authority.PlanSHA256 = localProofFileSHA256(t, inputs.planPath)
	authority.ProfileSHA256 = localProofFileSHA256(t, inputs.profilePath)
	authority.Rows = []ExclusionPolicyRow{
		{SurfaceID: "apex:Local.only", Class: "local-contract", Reason: "local-only proof"},
		{SurfaceID: "apex:Mock.run", Class: "local-contract", Reason: "local-only mock"},
		{SurfaceID: "apex:Other.only", Class: "waiver", Reason: "explicit non-parity waiver"},
	}
	overwriteReconciliationJSON(t, inputs.authorityPath, authority)
	writeSealedReleaseValidation(t, inputs, inputs.attemptPath)
	workerRoot, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	outputRoot := filepath.Join(workerRoot, "salesforce-worker")
	bundle, err := BuildOracleBundle(inputs.request(outputRoot))
	if err != nil {
		t.Fatal(err)
	}
	bundlePath := filepath.Join(outputRoot, "bundle", "bundle.json")
	oraclePlanPath := filepath.Join(outputRoot, "bundle", "ORACLE_PLAN.json")
	bundleSHA := localProofFileSHA256(t, bundlePath)
	executorRoot := filepath.Join(workerRoot, "executor", "shard-0")
	runID, alias := "assurance-"+bundle.AttemptSHA256[:16]+"-shard-0", "assurance-sf0"
	args, err := salesforceFilterArgs(sealedSalesforceFilterScriptPath(executorRoot), filepath.Dir(bundlePath), executorRoot, runID, alias, bundle, bundleSHA, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	filterSource, err := os.ReadFile(filepath.Join(outputRoot, "transport", "salesforce-first-filter.py"))
	if err != nil {
		t.Fatal(err)
	}
	args, err = sealedSalesforceFilterInvocationArgs(sealedSalesforceFilterScriptPath(executorRoot), filterSource, args)
	if err != nil {
		t.Fatal(err)
	}
	command := salesforceFilterCommandForTest(args, bundlePath, mustFixedSalesforceEnvironment(t), mustSealedPythonSHA(t))
	bindings := SalesforceBindings{OraclePlanSHA256: bundle.OraclePlanSHA256, BundleSHA256: bundleSHA, FilterSHA256: bundle.FilterSHA256, FilterCommandSpecSHA256: command.CommandSpecSHA256}
	lifecycle := salesforcePreflightForTest(t, alias, bundleSHA, bundlePath)
	shardPath := filepath.Join(workerRoot, "SALESFORCE_SHARD.json")
	shard := SalesforceShard{
		Bindings: bindings, Candidate: bundle.Candidate, Tools: bundle.Tools, ExecutorRoot: executorRoot, RunID: runID,
		ShardIndex: 0, ShardCount: 2, OrgAlias: alias, OrgID: lifecycle.OrgID, OrgStatus: "Active",
		Preflight: lifecycle, PreInventory: salesforceBaselineInventoryForTest(), Commands: []CommandResult{command},
		Postflight: lifecycle, PostInventory: salesforceBaselineInventoryForTest(),
		Results: []SalesforceSurfaceResult{{SurfaceID: "apex:Runtime.run", Kind: oracleRuntime, Passed: true}}, Cleanup: CleanupReceipt{ResidueAbsent: true},
	}
	if err := WriteNewJSON(shardPath, shard); err != nil {
		t.Fatal(err)
	}
	files := salesforceShardFilesForTest(t, shardPath, bundlePath, bundleSHA, alias, lifecycle.OrgID)
	controllerRoot := t.TempDir()
	scopePath := filepath.Join(controllerRoot, "scope.json")
	scope := SurfaceOracleScope{
		SchemaVersion: 1, Kind: "all-runtime", SourceProfileSHA256: strings.Repeat("a", 64), LedgerSHA256: strings.Repeat("b", 64), PolicySHA256: strings.Repeat("c", 64),
		Total: 5, ByDisposition: map[string]int{deterministicMockRequired: 0, localRuntimeRequired: 5}, Rows: []SurfaceOracleScopeRow{
			{SurfaceID: "apex:Local.only", Disposition: localRuntimeRequired},
			{SurfaceID: "apex:Mock.run", Disposition: localRuntimeRequired},
			{SurfaceID: "apex:Other.only", Disposition: localRuntimeRequired},
			{SurfaceID: "apex:Runtime.run", Disposition: localRuntimeRequired},
			{SurfaceID: "apex:Shape.run", Disposition: localRuntimeRequired},
		},
	}
	if err := WriteNewJSON(scopePath, scope); err != nil {
		t.Fatal(err)
	}
	definition := OrchestratorCampaignDefinition{
		Candidate: OrchestratorArtifact{Commit: bundle.Candidate.Commit, SHA256: bundle.Candidate.SHA256},
		Tools:     OrchestratorArtifact{Commit: bundle.Tools.Commit, SHA256: bundle.Tools.SHA256},
		ScopePath: scopePath, ScopeSHA256: localProofFileSHA256(t, scopePath),
		ControlledInputSHA256: map[string]string{"oracle-plan": localProofFileSHA256(t, oraclePlanPath)},
		Shards:                [][]string{{"apex:Local.only", "apex:Runtime.run"}, {"apex:Mock.run", "apex:Other.only", "apex:Shape.run"}},
	}
	plan, err := PlanOrchestratorCampaign(definition)
	if err != nil {
		t.Fatal(err)
	}
	lease := leaseForOrchestratorPlan(plan, 0)
	bindingPath := filepath.Join(outputRoot, "evidence", "ORCHESTRATOR_BINDING.json")
	if err := os.MkdirAll(filepath.Dir(bindingPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteOrchestratorBatchBinding(bindingPath, plan, lease); err != nil {
		t.Fatal(err)
	}
	return orchestratorSalesforceReconciliationFixture{plan: plan, lease: lease, oraclePlanPath: oraclePlanPath, bindingPath: bindingPath, files: files, workerRoot: workerRoot, oracleBundleRoot: outputRoot, localProofPath: inputs.localProofPath}
}

func withOraclePlanCampaignScope(t *testing.T, fixture orchestratorSalesforceReconciliationFixture) orchestratorSalesforceReconciliationFixture {
	t.Helper()
	root := t.TempDir()
	scopePath := filepath.Join(root, "scope.json")
	scope := SurfaceOracleScope{
		SchemaVersion: 1, Kind: "oracle-plan", OraclePlanSHA256: localProofFileSHA256(t, fixture.oraclePlanPath),
		SourceProfileSHA256: strings.Repeat("a", 64), LedgerSHA256: strings.Repeat("b", 64), PolicySHA256: strings.Repeat("c", 64),
		Total: 2, ByDisposition: map[string]int{deterministicMockRequired: 0, localRuntimeRequired: 1, compileShapeRequired: 1}, Rows: []SurfaceOracleScopeRow{
			{SurfaceID: "apex:Runtime.run", Disposition: localRuntimeRequired, Action: oracleRuntime},
			{SurfaceID: "apex:Shape.run", Disposition: compileShapeRequired, Action: oracleCompile},
		},
	}
	if err := WriteNewJSON(scopePath, scope); err != nil {
		t.Fatal(err)
	}
	definition := fixture.plan.Definition
	definition.ScopePath, definition.ScopeSHA256 = scopePath, localProofFileSHA256(t, scopePath)
	definition.Shards = [][]string{{"apex:Runtime.run"}, {"apex:Shape.run"}}
	plan, err := PlanOrchestratorCampaign(definition)
	if err != nil {
		t.Fatal(err)
	}
	fixture.plan, fixture.lease = plan, leaseForOrchestratorPlan(plan, 0)
	fixture.bindingPath = filepath.Join(root, "ORCHESTRATOR_BINDING.json")
	if _, err := WriteOrchestratorBatchBinding(fixture.bindingPath, fixture.plan, fixture.lease); err != nil {
		t.Fatal(err)
	}
	return fixture
}
