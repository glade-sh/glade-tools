package corpusassurance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestSealedSalesforceDispatchLayoutBindsLogicalShardCount(t *testing.T) {
	root := filepath.Join(t.TempDir(), "salesforce-worker")
	bundlePath := filepath.Join(root, "bundle", "bundle.json")
	if err := os.MkdirAll(filepath.Dir(bundlePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundlePath, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	canonicalBundle, err := filepath.EvalSymlinks(bundlePath)
	if err != nil {
		t.Fatal(err)
	}
	attemptRoot := filepath.Dir(filepath.Dir(filepath.Dir(canonicalBundle)))
	attempt := strings.Repeat("a", 64)
	legacyRoot, legacyRun, err := sealedSalesforceDispatchLayout(bundlePath, attempt, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(attemptRoot, "executor", "shard-0"); legacyRoot != want || legacyRun != "assurance-"+attempt[:16]+"-shard-0" {
		t.Fatalf("N=2 identity = %q, %q; want %q and legacy run ID", legacyRoot, legacyRun, want)
	}
	n3Root, n3Run, err := sealedSalesforceDispatchLayout(bundlePath, attempt, 0, 3)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(attemptRoot, "executor", "shard-0-of-3"); n3Root != want || n3Run != "assurance-"+attempt[:16]+"-shard-0-of-3" {
		t.Fatalf("N=3 identity = %q, %q; want %q and count-qualified run ID", n3Root, n3Run, want)
	}
	if n3Root == legacyRoot || n3Run == legacyRun {
		t.Fatal("N=2 and N=3 identities collide")
	}
	for _, invalid := range [][2]int{{0, 0}, {-1, 3}, {3, 3}} {
		if _, _, err := sealedSalesforceDispatchLayout(bundlePath, attempt, invalid[0], invalid[1]); err == nil {
			t.Fatalf("accepted invalid shard index/count %d/%d", invalid[0], invalid[1])
		}
	}
}

func TestProductionV3N3AcceptedReceiptReplayPublicPath(t *testing.T) {
	inputs := newProductionV3N3Fixture(t)
	orchestrator := openTestOrchestrator(t)
	if err := orchestrator.InitCampaign(inputs.plan); err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.Enqueue(inputs.plan); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	receipts := make([]OrchestratorReceipt, 0, len(inputs.plan.Jobs))
	for index, job := range inputs.plan.Jobs {
		lease, err := orchestrator.Lease(inputs.plan.CampaignID, fmt.Sprintf("production-v3-n3-worker-%d", index), now, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if lease.JobID != job.ID || len(lease.SurfaceIDs) != 1 || lease.SurfaceIDs[0] != inputs.surfaceIDs[index] {
			t.Fatalf("lease %d = %#v, want job %#v and surface %q", index, lease, job, inputs.surfaceIDs[index])
		}
		root := t.TempDir()
		planPath, leasePath := filepath.Join(root, "ORCHESTRATOR_PLAN.json"), filepath.Join(root, "ORCHESTRATOR_LEASE.json")
		writeJSONValue(t, planPath, inputs.plan)
		writeJSONValue(t, leasePath, lease)
		bindingPath := filepath.Join(root, "ORCHESTRATOR_BINDING.json")
		if _, err := WriteOrchestratorBatchBinding(bindingPath, inputs.plan, lease); err != nil {
			t.Fatal(err)
		}
		receiptPath, packetPath := filepath.Join(root, "SALESFORCE_RECONCILIATION.json"), filepath.Join(root, "salesforce-packet")
		jobFixture := inputs.fixture
		jobFixture.lease, jobFixture.bindingPath, jobFixture.files = lease, bindingPath, inputs.shards[index]
		if _, err := CreateOrchestratorSalesforceReconciliation(OrchestratorSalesforceReconciliationRequest{Plan: inputs.plan, Lease: lease, OraclePlanPath: inputs.oraclePlanPath, BindingPath: bindingPath, ShardFiles: inputs.shards[index], PacketOutput: packetPath, OutputPath: receiptPath}); err != nil {
			t.Fatal(err)
		}
		rawRoot := productionRawRootForTest(t, root, jobFixture, receiptPath)
		reviewPath := filepath.Join(root, "PRODUCTION_REVIEW.json")
		writeJSONValue(t, reviewPath, ProductionRuntimeReview{SchemaVersion: 1, PlanSHA256: surfaceOracleFileSHA256(t, planPath), LeaseSHA256: surfaceOracleFileSHA256(t, leasePath), LocalProofSHA256: surfaceOracleFileSHA256(t, inputs.localProofPath), ReconciliationSHA256: surfaceOracleFileSHA256(t, receiptPath), Rows: []ProductionRuntimeReviewRow{{SurfaceID: lease.SurfaceIDs[0], Action: oracleRuntime, Classification: "match", ReviewDisposition: "confirmed-match"}}})
		output := filepath.Join(root, "production-batch")
		manifest, err := BuildOrchestratorProductionBatch(BuildOrchestratorProductionBatchRequest{PlanPath: planPath, LeasePath: leasePath, LocalProofPath: inputs.localProofPath, ReviewPath: reviewPath, OracleBundleRoot: inputs.oracleBundleRoot, RawRoot: rawRoot, SalesforceReconciliationPath: receiptPath, SalesforcePacketPath: packetPath, OutputPath: output})
		if err != nil || manifest.SchemaVersion != orchestratorProductionBatchSchema {
			t.Fatalf("production build %d = %#v, %v", index, manifest, err)
		}
		transfer, err := TransferOrchestratorWorkerBatch(OrchestratorWorkerTransferRequest{Plan: inputs.plan, Lease: lease, SourceBatchRoot: output, EvidenceRoot: filepath.Join(root, "evidence"), OraclePlanPath: inputs.oraclePlanPath})
		if err != nil || transfer.BatchRoot == output || transfer.ManifestSHA256 == "" {
			t.Fatalf("validated production transfer %d = %#v, %v", index, transfer, err)
		}
		if err := orchestrator.SetHubCapacity(fmt.Sprintf("production-v3-n3-hub-%d", index), 1); err != nil {
			t.Fatal(err)
		}
		observeReadyHub(t, orchestrator, fmt.Sprintf("production-v3-n3-hub-%d", index), now)
		if err := orchestrator.Reserve(lease, fmt.Sprintf("production-v3-n3-hub-%d", index), fmt.Sprintf("production-v3-n3-scratch-%d", index), now); err != nil {
			t.Fatal(err)
		}
		claim, err := orchestrator.ClaimCleanup(inputs.plan.CampaignID, lease.Worker, now, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if err := orchestrator.CloseCleanup(claim, now.Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		receipt, err := orchestrator.RecordReceipt(OrchestratorReceiptRequest{Lease: lease, BatchRoot: transfer.BatchRoot}, now.Add(2*time.Second))
		if err != nil || receipt.AcceptedCredit != 1 || receipt.RejectedCredit != 0 || receipt.ManifestSHA256 != transfer.ManifestSHA256 || receipt.BindingSHA256 != transfer.ManifestSHA256 {
			t.Fatalf("production receipt %d = %#v, %v", index, receipt, err)
		}
		replay, err := orchestrator.RecordReceipt(OrchestratorReceiptRequest{Lease: lease, BatchRoot: transfer.BatchRoot}, now.Add(3*time.Second))
		if err != nil || !reflect.DeepEqual(replay, receipt) {
			t.Fatalf("production receipt replay %d = %#v, want %#v, %v", index, replay, receipt, err)
		}
		receipts = append(receipts, receipt)
	}
	ids := map[string]bool{}
	for _, receipt := range receipts {
		if ids[receipt.ID] {
			t.Fatalf("duplicate receipt identity %q", receipt.ID)
		}
		ids[receipt.ID] = true
	}
	status, err := orchestrator.Status(inputs.plan.CampaignID)
	if err != nil || status.Closed != 3 || status.Accepted != 3 || status.Rejected != 0 || status.Unseen != 0 || status.CleanupOpen != 0 {
		t.Fatalf("production v3 N=3 status = %#v, %v", status, err)
	}
	var attempts, closedAttempts, receiptCount, creditCount, openAllocations int
	if err := orchestrator.db.QueryRow(`SELECT count(*), count(*) FILTER (WHERE status = 'closed') FROM attempts WHERE campaign_id = ?`, inputs.plan.CampaignID).Scan(&attempts, &closedAttempts); err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.db.QueryRow(`SELECT count(*) FROM receipts WHERE campaign_id = ?`, inputs.plan.CampaignID).Scan(&receiptCount); err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.db.QueryRow(`SELECT count(*) FROM proof_credits WHERE campaign_id = ? AND state = 'accepted'`, inputs.plan.CampaignID).Scan(&creditCount); err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.db.QueryRow(`SELECT count(*) FROM scratch_allocations WHERE campaign_id = ? AND state != 'closed'`, inputs.plan.CampaignID).Scan(&openAllocations); err != nil {
		t.Fatal(err)
	}
	if attempts != 3 || closedAttempts != 3 || receiptCount != 3 || creditCount != 3 || openAllocations != 0 {
		t.Fatalf("production v3 N=3 database counts attempts=%d/%d receipts=%d credits=%d openAllocations=%d", attempts, closedAttempts, receiptCount, creditCount, openAllocations)
	}
}

type productionV3N3Fixture struct {
	fixture          orchestratorSalesforceReconciliationFixture
	plan             OrchestratorCampaignPlan
	oraclePlanPath   string
	oracleBundleRoot string
	localProofPath   string
	shards           []SalesforceShardFiles
	surfaceIDs       []string
}

func newProductionV3N3Fixture(t *testing.T) productionV3N3Fixture {
	t.Helper()
	inputs := oracleBundleTestInputsForLocalProof(t)
	ids := []string{"apex:ProductionV3N3.0", "apex:ProductionV3N3.1", "apex:ProductionV3N3.2"}
	proof, _, err := readExactJSONBytes[LocalProof](inputs.localProofPath)
	if err != nil {
		t.Fatal(err)
	}
	manifest, _, err := readExactJSONBytes[LocalProofFixtureManifest](inputs.fixtureManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	updatedFixtures := make([]LocalProofFixture, 0, len(manifest.Fixtures)+2)
	var runtimeFixture LocalProofFixture
	for _, fixture := range manifest.Fixtures {
		if fixture.ID == "runtime" {
			runtimeFixture = fixture
			continue
		}
		updatedFixtures = append(updatedFixtures, fixture)
	}
	if runtimeFixture.ID == "" {
		t.Fatal("canonical runtime fixture is missing")
	}
	for index, surfaceID := range ids {
		fixture := runtimeFixture
		fixture.ID = fmt.Sprintf("production-v3-n3-%d", index)
		fixture.OwnedSurfaceIDs = []string{surfaceID}
		fixture.Path = filepath.Join(filepath.Dir(runtimeFixture.Path), fixture.ID+".json")
		data, err := os.ReadFile(runtimeFixture.Path)
		if err != nil {
			t.Fatal("copy canonical runtime fixture")
		}
		var document map[string]any
		if err := json.Unmarshal(data, &document); err != nil {
			t.Fatal(err)
		}
		evidence, ok := document["evidence"].([]any)
		if !ok || len(evidence) == 0 {
			t.Fatal("canonical runtime fixture has no evidence")
		}
		entry, ok := evidence[0].(map[string]any)
		if !ok {
			t.Fatal("canonical runtime fixture evidence is invalid")
		}
		entry["surfaceId"] = surfaceID
		evidence[0] = entry
		document["evidence"] = evidence
		data, err = json.Marshal(document)
		if err != nil || os.WriteFile(fixture.Path, data, 0o600) != nil {
			t.Fatal("write canonical runtime fixture copy")
		}
		fixture.SHA256 = replayBytesSHA256(data)
		updatedFixtures = append(updatedFixtures, fixture)
	}
	manifest.Fixtures = updatedFixtures
	manifest.SalesforceFixtures = make([]LocalProofFixture, 0, len(ids))
	for _, fixture := range updatedFixtures {
		if strings.HasPrefix(fixture.ID, "production-v3-n3-") {
			manifest.SalesforceFixtures = append(manifest.SalesforceFixtures, fixture)
		}
	}
	writeJSONValue(t, inputs.fixtureManifestPath, manifest)
	manifestSHA := surfaceOracleFileSHA256(t, inputs.fixtureManifestPath)
	profile, _, err := readExactJSONBytes[AssuranceProfile](inputs.profilePath)
	if err != nil {
		t.Fatal(err)
	}
	profile.FixtureManifestSHA256 = manifestSHA
	writeJSONValue(t, inputs.profilePath, profile)
	profileSHA := surfaceOracleFileSHA256(t, inputs.profilePath)
	localProfile, _, err := readExactJSONBytes[LocalProofProfile](proof.ProfilePath)
	if err != nil {
		t.Fatal(err)
	}
	localProfile.Rows = replaceLocalProofSurface(t, localProfile.Rows, ids)
	writeJSONValue(t, proof.ProfilePath, localProfile)
	localUsage, _, err := readExactJSONBytes[LocalProofUsage](proof.UsagePath)
	if err != nil {
		t.Fatal(err)
	}
	localUsage.Usage = replaceLocalProofUsage(t, localUsage.Usage, ids)
	writeJSONValue(t, proof.UsagePath, localUsage)
	localProfileSHA := surfaceOracleFileSHA256(t, proof.ProfilePath)
	localUsageSHA := surfaceOracleFileSHA256(t, proof.UsagePath)
	decision, _, err := readExactJSONBytes[LocalProofDecision](proof.DecisionPath)
	if err != nil {
		t.Fatal(err)
	}
	decision.ProfileSHA256 = localProfileSHA
	decision.UsageSHA256 = localUsageSHA
	decision.FixtureManifestSHA256 = manifestSHA
	decision.Decisions = append([]LocalProofDecisionRow(nil), decision.Decisions...)
	for index := range decision.Decisions {
		if decision.Decisions[index].SurfaceID == "apex:Runtime.run" {
			decision.Decisions = append(decision.Decisions[:index], append([]LocalProofDecisionRow{{SurfaceID: ids[0], RequireLocalProof: true}, {SurfaceID: ids[1], RequireLocalProof: true}, {SurfaceID: ids[2], RequireLocalProof: true}}, decision.Decisions[index+1:]...)...)
			break
		}
	}
	writeJSONValue(t, proof.DecisionPath, decision)
	proofRequest := LocalProofRequest{AttemptPath: proof.AttemptPath, ProfilePath: proof.ProfilePath, UsagePath: proof.UsagePath, DecisionPath: proof.DecisionPath, FixtureManifestPath: inputs.fixtureManifestPath, Candidate: proof.Candidate, CandidatePath: proof.CandidatePath, Tools: proof.Tools, ToolsPath: proof.ToolsPath, OutputPath: filepath.Join(filepath.Dir(inputs.localProofPath), "LOCAL_PROOF_N3.json")}
	proofRequest.architecture = func(string) (string, error) { return runtime.GOARCH, nil }
	proofRequest.executor = func(command localProofCommand) localProofExecution {
		return localProofExecution{Receipt: localProofReceipt(command), Validated: true, Stdout: localProofSuccessOutputFor(command.Args[0])}
	}
	newProof, err := RunLocalProof(proofRequest)
	if err != nil {
		t.Fatal(err)
	}
	inputs.proof, inputs.localProofPath = newProof, proofRequest.OutputPath
	profile.LocalProofSHA256 = surfaceOracleFileSHA256(t, inputs.localProofPath)
	writeJSONValue(t, inputs.profilePath, profile)
	profileSHA = surfaceOracleFileSHA256(t, inputs.profilePath)
	inputs.plan = OraclePlan{Candidate: newProof.Candidate, Tools: newProof.Tools, ProfileSHA256: profileSHA, Rows: []OraclePlanRow{{SurfaceID: "apex:Local.only", Action: oracleLocalContractOnly, ExclusionClass: "local-contract", ExclusionReason: "local-only proof"}, {SurfaceID: "apex:Mock.run", Action: oracleLocalContractOnly, ExclusionClass: "local-contract", ExclusionReason: "local-only mock"}, {SurfaceID: "apex:Other.only", Action: oracleWaiver, ExclusionClass: "waiver", ExclusionReason: "explicit non-parity waiver"}, {SurfaceID: "apex:Shape.run", Action: oracleLocalContractOnly, ExclusionClass: "local-contract", ExclusionReason: "local-only shape"}}}
	for _, surfaceID := range ids {
		inputs.plan.Rows = append(inputs.plan.Rows, OraclePlanRow{SurfaceID: surfaceID, Action: oracleRuntime})
	}
	writeJSONValue(t, inputs.planPath, inputs.plan)
	authority, _, err := readExactJSONBytes[ExclusionAuthority](inputs.authorityPath)
	if err != nil {
		t.Fatal(err)
	}
	authority.PlanSHA256, authority.ProfileSHA256, authority.LocalProofSHA256 = surfaceOracleFileSHA256(t, inputs.planPath), profileSHA, surfaceOracleFileSHA256(t, inputs.localProofPath)
	writeJSONValue(t, inputs.authorityPath, authority)
	writeSealedReleaseValidation(t, inputs, inputs.attemptPath)
	workerRoot, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bundleRoot := filepath.Join(workerRoot, "salesforce-worker")
	bundle, err := BuildOracleBundle(inputs.request(bundleRoot))
	if err != nil {
		t.Fatal(err)
	}
	oraclePlanPath := filepath.Join(bundleRoot, "bundle", "ORACLE_PLAN.json")
	scopePath := filepath.Join(t.TempDir(), "scope.json")
	scope := SurfaceOracleScope{SchemaVersion: 1, Kind: "oracle-plan", OraclePlanSHA256: surfaceOracleFileSHA256(t, oraclePlanPath), SourceProfileSHA256: strings.Repeat("a", 64), LedgerSHA256: strings.Repeat("b", 64), PolicySHA256: strings.Repeat("c", 64), Total: 3, ByDisposition: map[string]int{deterministicMockRequired: 0, localRuntimeRequired: 3, compileShapeRequired: 0}, Rows: []SurfaceOracleScopeRow{{SurfaceID: ids[0], Disposition: localRuntimeRequired, Action: oracleRuntime}, {SurfaceID: ids[1], Disposition: localRuntimeRequired, Action: oracleRuntime}, {SurfaceID: ids[2], Disposition: localRuntimeRequired, Action: oracleRuntime}}}
	writeJSONValue(t, scopePath, scope)
	definition := OrchestratorCampaignDefinition{Candidate: OrchestratorArtifact{Commit: bundle.Candidate.Commit, SHA256: bundle.Candidate.SHA256}, Tools: OrchestratorArtifact{Commit: bundle.Tools.Commit, SHA256: bundle.Tools.SHA256}, ScopePath: scopePath, ScopeSHA256: surfaceOracleFileSHA256(t, scopePath), ControlledInputSHA256: map[string]string{"oracle-plan": surfaceOracleFileSHA256(t, oraclePlanPath)}, Shards: [][]string{{ids[0]}, {ids[1]}, {ids[2]}}}
	plan, err := PlanOrchestratorCampaign(definition)
	if err != nil {
		t.Fatal(err)
	}
	shards := make([]SalesforceShardFiles, len(ids))
	for index, surfaceID := range ids {
		root := filepath.Join(t.TempDir(), fmt.Sprintf("shard-%d", index))
		if err := os.MkdirAll(root, 0o700); err != nil {
			t.Fatal(err)
		}
		bundlePath := filepath.Join(bundleRoot, "bundle", "bundle.json")
		bundleSHA := surfaceOracleFileSHA256(t, bundlePath)
		executorRoot, runID, err := sealedSalesforceDispatchLayout(bundlePath, bundle.AttemptSHA256, index, 3)
		if err != nil {
			t.Fatal(err)
		}
		alias := "production-v3-n3-sf"
		args, err := salesforceFilterArgs(sealedSalesforceFilterScriptPath(executorRoot), filepath.Dir(bundlePath), executorRoot, runID, alias, bundle, bundleSHA, index, 3)
		if err != nil {
			t.Fatal(err)
		}
		filterSource, err := os.ReadFile(filepath.Join(bundleRoot, "transport", "salesforce-first-filter.py"))
		if err != nil {
			t.Fatal(err)
		}
		args, err = sealedSalesforceFilterInvocationArgs(sealedSalesforceFilterScriptPath(executorRoot), filterSource, args)
		if err != nil {
			t.Fatal(err)
		}
		command := salesforceFilterCommandForTest(args, bundlePath, mustFixedSalesforceEnvironment(t), mustSealedPythonSHA(t))
		lifecycle := salesforcePreflightForTest(t, alias, bundleSHA, bundlePath)
		shard := SalesforceShard{
			Bindings:      SalesforceBindings{OraclePlanSHA256: bundle.OraclePlanSHA256, BundleSHA256: bundleSHA, FilterSHA256: bundle.FilterSHA256, FilterCommandSpecSHA256: command.CommandSpecSHA256},
			Candidate:     bundle.Candidate,
			Tools:         bundle.Tools,
			ExecutorRoot:  executorRoot,
			RunID:         runID,
			ShardIndex:    index,
			ShardCount:    3,
			OrgAlias:      alias,
			OrgID:         lifecycle.OrgID,
			OrgStatus:     "Active",
			Preflight:     lifecycle,
			PreInventory:  salesforceBaselineInventoryForTest(),
			Commands:      []CommandResult{command},
			Postflight:    lifecycle,
			PostInventory: salesforceBaselineInventoryForTest(),
			Results:       []SalesforceSurfaceResult{{SurfaceID: surfaceID, Kind: oracleRuntime, Passed: true}},
			Cleanup:       CleanupReceipt{ResidueAbsent: true},
		}
		shardPath := filepath.Join(root, "SALESFORCE_SHARD.json")
		if err := WriteNewJSON(shardPath, shard); err != nil {
			t.Fatal(err)
		}
		shards[index] = salesforceShardFilesForTest(t, shardPath, bundlePath, bundleSHA, alias, lifecycle.OrgID)
		// salesforceShardFilesForTest seals the complete executor and lifecycle evidence.
		shardBytes, _, err := readExactJSONBytes[SalesforceShard](shards[index].ShardPath)
		if err != nil || shardBytes.Results[0].SurfaceID != surfaceID {
			t.Fatalf("canonical shard %d = %#v, %v", index, shardBytes, err)
		}
	}
	return productionV3N3Fixture{fixture: orchestratorSalesforceReconciliationFixture{plan: plan, oraclePlanPath: oraclePlanPath, oracleBundleRoot: bundleRoot, localProofPath: inputs.localProofPath}, plan: plan, oraclePlanPath: oraclePlanPath, oracleBundleRoot: bundleRoot, localProofPath: inputs.localProofPath, shards: shards, surfaceIDs: ids}
}

func replaceLocalProofSurface(t *testing.T, rows []LocalProofProfileRow, ids []string) []LocalProofProfileRow {
	t.Helper()
	result := make([]LocalProofProfileRow, 0, len(rows)+len(ids)-1)
	for _, row := range rows {
		if row.SurfaceID == "apex:Runtime.run" {
			for _, id := range ids {
				result = append(result, LocalProofProfileRow{SurfaceID: id})
			}
			continue
		}
		result = append(result, row)
	}
	return result
}

func replaceLocalProofUsage(t *testing.T, rows []LocalProofUsageEntry, ids []string) []LocalProofUsageEntry {
	t.Helper()
	result := make([]LocalProofUsageEntry, 0, len(rows)+len(ids)-1)
	for _, row := range rows {
		if row.SurfaceID == "apex:Runtime.run" {
			for _, id := range ids {
				result = append(result, LocalProofUsageEntry{SurfaceID: id})
			}
			continue
		}
		result = append(result, row)
	}
	return result
}
