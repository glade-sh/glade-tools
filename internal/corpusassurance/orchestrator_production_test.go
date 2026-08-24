package corpusassurance

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestBuildTransferAndRecordOrchestratorProductionBatch(t *testing.T) {
	fixture := withOraclePlanCampaignScope(t, newOrchestratorSalesforceReconciliationFixture(t))
	root := t.TempDir()
	orchestrator := openTestOrchestrator(t)
	if err := orchestrator.InitCampaign(fixture.plan); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	lease, err := orchestrator.Lease(fixture.plan.CampaignID, "worker-production", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	fixture.lease = lease
	receiptPath := filepath.Join(root, "SALESFORCE_RECONCILIATION.json")
	packetPath := filepath.Join(root, "salesforce-packet")
	if _, err := CreateOrchestratorSalesforceReconciliation(OrchestratorSalesforceReconciliationRequest{
		Plan: fixture.plan, Lease: fixture.lease, OraclePlanPath: fixture.oraclePlanPath, BindingPath: fixture.bindingPath,
		ShardFiles: fixture.files, PacketOutput: packetPath, OutputPath: receiptPath,
	}); err != nil {
		t.Fatal(err)
	}
	rawRoot := productionRawRootForTest(t, root, fixture, receiptPath)
	planPath, leasePath := filepath.Join(root, "ORCHESTRATOR_PLAN.json"), filepath.Join(root, "ORCHESTRATOR_LEASE.json")
	writeJSONValue(t, planPath, fixture.plan)
	writeJSONValue(t, leasePath, fixture.lease)
	reviewPath := filepath.Join(root, "PRODUCTION_REVIEW.json")
	writeJSONValue(t, reviewPath, ProductionRuntimeReview{
		SchemaVersion: 1,
		PlanSHA256:    surfaceOracleFileSHA256(t, planPath), LeaseSHA256: surfaceOracleFileSHA256(t, leasePath),
		LocalProofSHA256: surfaceOracleFileSHA256(t, fixture.localProofPath), ReconciliationSHA256: surfaceOracleFileSHA256(t, receiptPath),
		Rows: []ProductionRuntimeReviewRow{{SurfaceID: "apex:Runtime.run", Action: oracleRuntime, Classification: "match", ReviewDisposition: "confirmed-match"}},
	})
	output := filepath.Join(root, "production-batch")
	request := BuildOrchestratorProductionBatchRequest{
		PlanPath: planPath, LeasePath: leasePath, LocalProofPath: fixture.localProofPath, ReviewPath: reviewPath,
		OracleBundleRoot: fixture.oracleBundleRoot, RawRoot: rawRoot,
		SalesforceReconciliationPath: receiptPath, SalesforcePacketPath: packetPath, OutputPath: output,
	}
	manifest, err := BuildOrchestratorProductionBatch(request)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.SchemaVersion != 3 || manifest.Candidate != mustProductionLocalProof(t, fixture.localProofPath).Candidate || manifest.NativeTools != mustProductionLocalProof(t, fixture.localProofPath).Tools || manifest.ExecutedTools != manifest.NativeTools || len(manifest.Files) == 0 {
		t.Fatalf("production manifest = %#v", manifest)
	}
	for _, forbidden := range []string{"counts", "rows", "filesSha256", "privacy"} {
		data, err := os.ReadFile(filepath.Join(output, "production", "PRODUCTION_RUNTIME_BATCH.json"))
		if err != nil || strings.Contains(strings.ToLower(string(data)), strings.ToLower(forbidden)) {
			t.Fatalf("production manifest contains %q: %s (err=%v)", forbidden, data, err)
		}
	}
	transfer, err := transferOrchestratorWorkerBatch(OrchestratorWorkerTransferRequest{Plan: fixture.plan, Lease: fixture.lease, SourceBatchRoot: output, EvidenceRoot: filepath.Join(root, "evidence"), OraclePlanPath: fixture.oraclePlanPath}, VerifySalesforceReconciliation, nil)
	if err != nil || transfer.ManifestSHA256 != surfaceOracleFileSHA256(t, filepath.Join(transfer.BatchRoot, "production", "PRODUCTION_RUNTIME_BATCH.json")) {
		t.Fatalf("production transfer = %#v, %v", transfer, err)
	}
	if err := orchestrator.SetHubCapacity("hub-production", 1); err != nil {
		t.Fatal(err)
	}
	observeReadyHub(t, orchestrator, "hub-production", now)
	if err := orchestrator.Reserve(lease, "hub-production", "scratch-production", now); err != nil {
		t.Fatal(err)
	}
	if _, err := orchestrator.RecordReceipt(OrchestratorReceiptRequest{Lease: lease, BatchRoot: transfer.BatchRoot}, now); err == nil || !strings.Contains(err.Error(), "cleanup") {
		t.Fatalf("production receipt bypassed cleanup gate: %v", err)
	}
	claim, err := orchestrator.ClaimCleanup(fixture.plan.CampaignID, lease.Worker, now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.CloseCleanup(claim, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := orchestrator.db.Exec(`INSERT INTO cleanup_credit_blocks (allocation_alias) VALUES (?)`, "scratch-production"); err != nil {
		t.Fatal(err)
	}
	if _, err := orchestrator.RecordReceipt(OrchestratorReceiptRequest{Lease: lease, BatchRoot: transfer.BatchRoot}, now.Add(2*time.Second)); err == nil || !strings.Contains(err.Error(), "proof-ineligible") {
		t.Fatalf("production receipt bypassed proof block: %v", err)
	}
	if _, err := orchestrator.db.Exec(`DELETE FROM cleanup_credit_blocks WHERE allocation_alias = ?`, "scratch-production"); err != nil {
		t.Fatal(err)
	}
	if _, err := orchestrator.db.Exec(`UPDATE jobs SET status = 'queued' WHERE campaign_id = ? AND id = ?`, lease.CampaignID, lease.JobID); err != nil {
		t.Fatal(err)
	}
	if _, err := orchestrator.RecordReceipt(OrchestratorReceiptRequest{Lease: lease, BatchRoot: transfer.BatchRoot}, now.Add(2*time.Second)); err == nil || !strings.Contains(err.Error(), "running") {
		t.Fatalf("production receipt bypassed running gate: %v", err)
	}
	if _, err := orchestrator.db.Exec(`UPDATE jobs SET status = 'running', lease_until = ? WHERE campaign_id = ? AND id = ?`, now.UnixMilli(), lease.CampaignID, lease.JobID); err != nil {
		t.Fatal(err)
	}
	if _, err := orchestrator.RecordReceipt(OrchestratorReceiptRequest{Lease: lease, BatchRoot: transfer.BatchRoot}, now.Add(2*time.Second)); err == nil || !strings.Contains(err.Error(), "running") {
		t.Fatalf("production receipt bypassed unexpired gate: %v", err)
	}
	if _, err := orchestrator.db.Exec(`UPDATE jobs SET lease_until = ? WHERE campaign_id = ? AND id = ?`, lease.LeaseUntil.UnixMilli(), lease.CampaignID, lease.JobID); err != nil {
		t.Fatal(err)
	}
	receipt, err := orchestrator.RecordReceipt(OrchestratorReceiptRequest{Lease: lease, BatchRoot: transfer.BatchRoot}, now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if receipt.AcceptedCredit != 1 || receipt.RejectedCredit != 0 || receipt.ManifestSHA256 != transfer.ManifestSHA256 || receipt.BindingSHA256 != transfer.ManifestSHA256 {
		t.Fatalf("production receipt = %#v", receipt)
	}
	legacyID := "receipt-" + replayBytesSHA256([]byte(lease.CampaignID + "\x00" + lease.JobID + "\x00" + "1" + "\x00" + receipt.ManifestSHA256 + "\x00" + receipt.BindingSHA256))[:16]
	if receipt.ID == legacyID {
		t.Fatalf("production receipt used legacy identity: %s", receipt.ID)
	}
	if replay, err := orchestrator.RecordReceipt(OrchestratorReceiptRequest{Lease: lease, BatchRoot: transfer.BatchRoot}, now.Add(3*time.Second)); err != nil || replay != receipt {
		t.Fatalf("production receipt replay = %#v, %v", replay, err)
	}
	manifestPath := filepath.Join(output, "production", "PRODUCTION_RUNTIME_BATCH.json")
	badManifest := manifest
	if badManifest.ExecutedTools.OS == "darwin" {
		badManifest.ExecutedTools.OS = "linux"
	} else {
		badManifest.ExecutedTools.OS = "darwin"
	}
	overwriteReconciliationJSON(t, manifestPath, badManifest)
	if _, err := validateOrchestratorProductionBatch(output, fixture.plan, fixture.lease); err == nil || !strings.Contains(err.Error(), "executed Tools") {
		t.Fatalf("accepted mutated local executed Tools: %v", err)
	}
	overwriteReconciliationJSON(t, manifestPath, manifest)
	review, _, err := readMode0600JSON[ProductionRuntimeReview](reviewPath)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*ProductionRuntimeReview){
		"denominator": func(value *ProductionRuntimeReview) { value.Rows = append(value.Rows, value.Rows[0]) },
		"action":      func(value *ProductionRuntimeReview) { value.Rows[0].Action = oracleCompile },
		"review":      func(value *ProductionRuntimeReview) { value.Rows[0].ReviewDisposition = "unconfirmed" },
	} {
		t.Run("rejects "+name+" before publish", func(t *testing.T) {
			changed := review
			changed.Rows = append([]ProductionRuntimeReviewRow(nil), review.Rows...)
			mutate(&changed)
			changedPath := filepath.Join(root, "PRODUCTION_REVIEW-"+name+".json")
			writeJSONValue(t, changedPath, changed)
			changedRequest := request
			changedRequest.ReviewPath, changedRequest.OutputPath = changedPath, filepath.Join(root, "failed-"+name)
			if _, err := BuildOrchestratorProductionBatch(changedRequest); err == nil {
				t.Fatalf("accepted changed %s", name)
			}
			if _, err := os.Lstat(changedRequest.OutputPath); !os.IsNotExist(err) {
				t.Fatalf("published changed %s: %v", name, err)
			}
		})
	}
	mutatedReview := review
	mutatedReview.Rows = append([]ProductionRuntimeReviewRow(nil), review.Rows...)
	mutatedReview.Rows[0].Classification = "mismatch"
	overwriteReconciliationJSON(t, filepath.Join(transfer.BatchRoot, "production", "PRODUCTION_REVIEW.json"), mutatedReview)
	if _, err := orchestrator.RecordReceipt(OrchestratorReceiptRequest{Lease: lease, BatchRoot: transfer.BatchRoot}, now.Add(4*time.Second)); err == nil {
		t.Fatal("mutated production authority replayed recorded credit")
	}
	if err := os.MkdirAll(filepath.Join(output, "inputs"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(output, "inputs", "RUNTIME_BATCH_MANIFEST.json"), "legacy\n")
	if _, err := validateOrchestratorBatch(output, fixture.plan, fixture.lease, false); err == nil || !strings.Contains(err.Error(), "mixed") {
		t.Fatalf("mixed v3 and legacy markers were accepted: %v", err)
	}
}

func TestStackedSyntheticReceiptStoreEntriesRemainUniqueAcrossReplay(t *testing.T) {
	root := t.TempDir()
	scopePath := filepath.Join(root, "scope.json")
	scope := SurfaceOracleScope{
		SchemaVersion: 1, Kind: "oracle-plan", OraclePlanSHA256: strings.Repeat("e", 64),
		SourceProfileSHA256: strings.Repeat("b", 64), LedgerSHA256: strings.Repeat("c", 64), PolicySHA256: strings.Repeat("d", 64), Total: 3,
		ByDisposition: map[string]int{deterministicMockRequired: 0, localRuntimeRequired: 2, compileShapeRequired: 1},
		Rows: []SurfaceOracleScopeRow{
			{SurfaceID: "apex:ControlPlane.compile", Disposition: compileShapeRequired, Action: oracleCompile},
			{SurfaceID: "apex:Runtime.match", Disposition: localRuntimeRequired, Action: oracleRuntime},
			{SurfaceID: "apex:Runtime.pass", Disposition: localRuntimeRequired, Action: oracleRuntime},
		},
	}
	writeJSONValue(t, scopePath, scope)
	definition := testOrchestratorDefinition(t, scopePath, [][]string{{"apex:Runtime.pass"}, {"apex:Runtime.match"}, {"apex:ControlPlane.compile"}})
	plan, err := PlanOrchestratorCampaign(definition)
	if err != nil {
		t.Fatal(err)
	}
	orchestrator := openTestOrchestrator(t)
	if err := orchestrator.InitCampaign(plan); err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.Enqueue(plan); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	type jobCase struct {
		name, state                string
		wantAccepted, wantRejected int
	}
	cases := []jobCase{{"accepted runtime receipt-store entry", "accepted", 1, 0}, {"confirmed mismatch receipt-store entry", "rejected", 0, 1}, {"inconclusive compile control-plane receipt-store entry", "inconclusive", 0, 0}}
	wantSurfaceIDs := []string{"apex:Runtime.pass", "apex:Runtime.match", "apex:ControlPlane.compile"}
	for index, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			lease, err := orchestrator.Lease(plan.CampaignID, fmt.Sprintf("worker-%d", index), now, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			if len(lease.SurfaceIDs) != 1 || lease.SurfaceIDs[0] != wantSurfaceIDs[index] {
				t.Fatalf("leased SurfaceID = %v, want [%q]", lease.SurfaceIDs, wantSurfaceIDs[index])
			}
			if err := orchestrator.SetHubCapacity(fmt.Sprintf("hub-%d", index), 1); err != nil {
				t.Fatal(err)
			}
			observeReadyHub(t, orchestrator, fmt.Sprintf("hub-%d", index), now)
			allocation := fmt.Sprintf("scratch-%d", index)
			if err := orchestrator.Reserve(lease, fmt.Sprintf("hub-%d", index), allocation, now); err != nil {
				t.Fatal(err)
			}
			claim, err := orchestrator.ClaimCleanup(plan.CampaignID, lease.Worker, now, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			if err := orchestrator.CloseCleanup(claim, now.Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			batchRoot := filepath.Join(root, fmt.Sprintf("receipt-store-%d", index))
			manifestSHA := writeSyntheticReceiptStoreBatch(t, batchRoot, plan)
			carrier := validatedOrchestratorBatch{
				BatchRoot: batchRoot, SchemaVersion: orchestratorProductionBatchSchema, ManifestSHA256: manifestSHA, AuthoritySHA256: manifestSHA,
				Candidate: plan.Definition.Candidate, Tools: plan.Definition.Tools, ProofStates: map[string]string{lease.SurfaceIDs[0]: testCase.state},
				ProductionFiles: []ProductionRuntimeBatchFile{},
			}
			request := OrchestratorReceiptRequest{Lease: lease, BatchRoot: batchRoot}
			receipt, err := orchestrator.recordValidatedReceipt(request, now.Add(2*time.Second), carrier)
			if testCase.state == "inconclusive" {
				if err == nil || !strings.Contains(err.Error(), "inconclusive") {
					t.Fatalf("inconclusive receipt-store evidence error = %v", err)
				}
				assertSyntheticReceiptCounts(t, orchestrator, plan.CampaignID, lease.JobID, 0, 0)
				return
			}
			if err != nil || receipt.AcceptedCredit != testCase.wantAccepted || receipt.RejectedCredit != testCase.wantRejected || receipt.ManifestSHA256 != manifestSHA || receipt.BindingSHA256 != manifestSHA {
				t.Fatalf("receipt = %#v, err=%v", receipt, err)
			}
			wantID := "receipt-" + replayBytesSHA256([]byte("orchestrator-production-receipt/v3\x00" + lease.CampaignID + "\x00" + lease.JobID + "\x00" + fmt.Sprint(lease.Generation) + "\x00" + manifestSHA + "\x00" + manifestSHA))[:16]
			if receipt.ID != wantID {
				t.Fatalf("receipt ID = %q, want %q", receipt.ID, wantID)
			}
			replay, replayErr := orchestrator.recordValidatedReceipt(request, now.Add(3*time.Second), carrier)
			if replayErr != nil || replay != receipt {
				t.Fatalf("exact receipt replay = %#v, %v", replay, replayErr)
			}
			assertSyntheticReceiptCounts(t, orchestrator, plan.CampaignID, lease.JobID, 1, 1)
		})
	}
	status, err := orchestrator.Status(plan.CampaignID)
	if err != nil || status.Accepted != 1 || status.Rejected != 1 || status.Unseen != 1 {
		t.Fatalf("stacked receipt-store status = %#v, %v", status, err)
	}
}

func writeSyntheticReceiptStoreBatch(t *testing.T, root string, plan OrchestratorCampaignPlan) string {
	t.Helper()
	production := filepath.Join(root, "production")
	if err := os.MkdirAll(production, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := ProductionRuntimeBatch{
		SchemaVersion: orchestratorProductionBatchSchema,
		Candidate:     RuntimeArtifact{Commit: plan.Definition.Candidate.Commit, SHA256: plan.Definition.Candidate.SHA256, OS: runtime.GOOS, Arch: runtime.GOARCH},
		NativeTools:   RuntimeArtifact{Commit: plan.Definition.Tools.Commit, SHA256: plan.Definition.Tools.SHA256, OS: runtime.GOOS, Arch: runtime.GOARCH},
		ExecutedTools: RuntimeArtifact{Commit: plan.Definition.Tools.Commit, SHA256: plan.Definition.Tools.SHA256, OS: runtime.GOOS, Arch: runtime.GOARCH},
		Files:         []ProductionRuntimeBatchFile{},
	}
	path := filepath.Join(production, "PRODUCTION_RUNTIME_BATCH.json")
	writeJSONValue(t, path, manifest)
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return replayBytesSHA256(data)
}

func assertSyntheticReceiptCounts(t *testing.T, orchestrator *Orchestrator, campaignID, jobID string, wantReceipts, wantCredits int) {
	t.Helper()
	var receipts, credits int
	if err := orchestrator.db.QueryRow(`SELECT count(*) FROM receipts WHERE campaign_id = ? AND job_id = ?`, campaignID, jobID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.db.QueryRow(`SELECT count(*) FROM proof_credits WHERE campaign_id = ? AND receipt_id IN (SELECT id FROM receipts WHERE campaign_id = ? AND job_id = ?)`, campaignID, campaignID, jobID).Scan(&credits); err != nil {
		t.Fatal(err)
	}
	if receipts != wantReceipts || credits != wantCredits {
		t.Fatalf("receipt counts = %d/%d, want %d/%d", receipts, credits, wantReceipts, wantCredits)
	}
}

func TestBuildTransferAndRecordOrchestratorProductionBatchFromSSH(t *testing.T) {
	fixture := withOraclePlanCampaignScope(t, newOrchestratorSalesforceReconciliationFixture(t))
	root := t.TempDir()
	orchestrator := openTestOrchestrator(t)
	if err := orchestrator.InitCampaign(fixture.plan); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	lease, err := orchestrator.Lease(fixture.plan.CampaignID, "worker-production-ssh", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	fixture.lease = lease
	reconciliationPath, packetPath := filepath.Join(root, "SALESFORCE_RECONCILIATION.json"), filepath.Join(root, "salesforce-packet")
	if _, err := CreateOrchestratorSalesforceReconciliation(OrchestratorSalesforceReconciliationRequest{Plan: fixture.plan, Lease: lease, OraclePlanPath: fixture.oraclePlanPath, BindingPath: fixture.bindingPath, ShardFiles: fixture.files, PacketOutput: packetPath, OutputPath: reconciliationPath}); err != nil {
		t.Fatal(err)
	}
	rawRoot := productionRawRootForTest(t, root, fixture, reconciliationPath)
	planPath, leasePath := filepath.Join(root, "ORCHESTRATOR_PLAN.json"), filepath.Join(root, "ORCHESTRATOR_LEASE.json")
	writeJSONValue(t, planPath, fixture.plan)
	writeJSONValue(t, leasePath, lease)
	planSHA, leaseSHA := surfaceOracleFileSHA256(t, planPath), surfaceOracleFileSHA256(t, leasePath)
	proof := mustProductionLocalProof(t, fixture.localProofPath)
	dispatchPath, fetchPath, treePath := productionSSHReceiptsForTest(t, root, rawRoot, fixture.plan, lease, planSHA, leaseSHA, proof.Tools)
	reviewPath := filepath.Join(root, "PRODUCTION_REVIEW.json")
	writeJSONValue(t, reviewPath, ProductionRuntimeReview{SchemaVersion: 1, PlanSHA256: planSHA, LeaseSHA256: leaseSHA, LocalProofSHA256: surfaceOracleFileSHA256(t, fixture.localProofPath), ReconciliationSHA256: surfaceOracleFileSHA256(t, reconciliationPath), Rows: []ProductionRuntimeReviewRow{{SurfaceID: "apex:Runtime.run", Action: oracleRuntime, Classification: "match", ReviewDisposition: "confirmed-match"}}})
	output := filepath.Join(root, "production-ssh-batch")
	request := BuildOrchestratorProductionBatchRequest{
		PlanPath: planPath, LeasePath: leasePath, LocalProofPath: fixture.localProofPath, ReviewPath: reviewPath, OracleBundleRoot: fixture.oracleBundleRoot, RawRoot: rawRoot,
		SalesforceReconciliationPath: reconciliationPath, SalesforcePacketPath: packetPath, SSHDispatchPath: dispatchPath, SSHFetchPath: fetchPath, SSHTreeManifestPath: treePath, OutputPath: output,
	}
	manifest, err := BuildOrchestratorProductionBatch(request)
	if err != nil || manifest.ExecutedTools != proof.Tools {
		t.Fatalf("SSH production manifest = %#v, %v", manifest, err)
	}
	transfer, err := TransferOrchestratorWorkerBatch(OrchestratorWorkerTransferRequest{Plan: fixture.plan, Lease: lease, SourceBatchRoot: output, EvidenceRoot: filepath.Join(root, "evidence"), OraclePlanPath: fixture.oraclePlanPath})
	if err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.SetHubCapacity("hub-production-ssh", 1); err != nil {
		t.Fatal(err)
	}
	observeReadyHub(t, orchestrator, "hub-production-ssh", now)
	if err := orchestrator.Reserve(lease, "hub-production-ssh", "scratch-production-ssh", now); err != nil {
		t.Fatal(err)
	}
	claim, err := orchestrator.ClaimCleanup(fixture.plan.CampaignID, lease.Worker, now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.CloseCleanup(claim, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	receipt, err := orchestrator.RecordReceipt(OrchestratorReceiptRequest{Lease: lease, BatchRoot: transfer.BatchRoot}, now.Add(2*time.Second))
	if err != nil || receipt.AcceptedCredit != 1 || receipt.RejectedCredit != 0 || receipt.ManifestSHA256 != manifestSHAForTest(t, transfer.BatchRoot) {
		t.Fatalf("SSH production receipt = %#v, %v", receipt, err)
	}
	badDispatch, _, err := readMode0600JSON[OrchestratorSSHDispatchReceipt](dispatchPath)
	if err != nil {
		t.Fatal(err)
	}
	badDispatch.ExecutedTools.SHA256 = strings.Repeat("f", 64)
	badDispatchPath := filepath.Join(root, "SSH_DISPATCH_BAD_TOOLS.json")
	writeJSONValue(t, badDispatchPath, badDispatch)
	badRequest := request
	badRequest.SSHDispatchPath, badRequest.OutputPath = badDispatchPath, filepath.Join(root, "bad-tools")
	if _, err := BuildOrchestratorProductionBatch(badRequest); err == nil {
		t.Fatal("accepted unbound executed Tools")
	}
	if _, err := os.Lstat(badRequest.OutputPath); !os.IsNotExist(err) {
		t.Fatalf("bad Tools published production batch: %v", err)
	}
	partialRequest := request
	partialRequest.SSHFetchPath, partialRequest.OutputPath = "", filepath.Join(root, "partial-ssh")
	if _, err := BuildOrchestratorProductionBatch(partialRequest); err == nil {
		t.Fatal("accepted partial SSH receipt set")
	}
}

func TestOrchestratorProductionBatchLocalProofIsSelfContained(t *testing.T) {
	output, fixture, _ := buildStandaloneProductionBatchForTest(t)
	proof := mustProductionLocalProof(t, fixture.localProofPath)
	fixtureManifest, _, err := readExactJSONBytes[LocalProofFixtureManifest](proof.FixtureManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{proof.AttemptPath, proof.FixtureManifestPath, fixtureManifest.Fixtures[0].Path} {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := validateOrchestratorProductionBatch(output, fixture.plan, fixture.lease); err != nil {
		t.Fatalf("retained production proof consulted external inputs: %v", err)
	}

	t.Run("retained input deletion", func(t *testing.T) {
		attempt := filepath.Join(output, "production", "local-proof", "ATTEMPT.json")
		if err := os.Remove(attempt); err != nil {
			t.Fatalf("retained attempt is unavailable: %v", err)
		}
		if _, err := validateOrchestratorProductionBatch(output, fixture.plan, fixture.lease); err == nil {
			t.Fatal("accepted production proof without retained attempt")
		}
	})

	t.Run("proof byte substitution", func(t *testing.T) {
		otherOutput, otherFixture, otherManifest := buildStandaloneProductionBatchForTest(t)
		proofPath := filepath.Join(otherOutput, "production", "LOCAL_PROOF.json")
		data, err := os.ReadFile(proofPath)
		if err != nil {
			t.Fatal(err)
		}
		data = append(data, '\n')
		if err := os.WriteFile(proofPath, data, 0o600); err != nil {
			t.Fatal(err)
		}
		for index := range otherManifest.Files {
			if otherManifest.Files[index].Path == "LOCAL_PROOF.json" {
				otherManifest.Files[index].SHA256 = replayBytesSHA256(data)
			}
		}
		overwriteReconciliationJSON(t, filepath.Join(otherOutput, "production", "PRODUCTION_RUNTIME_BATCH.json"), otherManifest)
		if _, err := validateOrchestratorProductionBatch(otherOutput, otherFixture.plan, otherFixture.lease); err == nil {
			t.Fatal("accepted local proof bytes not bound by the retained oracle bundle")
		}
	})
}

func TestOrchestratorWorkerBuildsProductionBatchEndToEnd(t *testing.T) {
	inputs := newProductionWorkerInputsForTest(t)
	result, err := runOrchestratorWorkerOnce(context.Background(), inputs.orchestrator, inputs.worker, fixedOrchestratorWorkerClock(inputs.now), func(context.Context, OrchestratorWorkerRequest) (OrchestratorWorkerRunResult, error) {
		return OrchestratorWorkerRunResult{Production: &inputs.build}, nil
	}, VerifySalesforceReconciliation, func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if result.Receipt.AcceptedCredit != 1 || result.Receipt.RejectedCredit != 0 || result.BatchRoot == inputs.build.OutputPath || result.ManifestSHA256 != result.Receipt.ManifestSHA256 {
		t.Fatalf("production worker result = %#v", result)
	}
	var cleanupState string
	if err := inputs.orchestrator.db.QueryRow(`SELECT state FROM cleanup_journal WHERE campaign_id = ? AND job_id = ? AND generation = ?`, inputs.fixture.plan.CampaignID, inputs.fixture.lease.JobID, inputs.fixture.lease.Generation).Scan(&cleanupState); err != nil || cleanupState != "closed" {
		t.Fatalf("production worker cleanup = %q, %v", cleanupState, err)
	}
}

func TestCorpusAssuranceOrchestratorProductionBuildCLI(t *testing.T) {
	inputs := newProductionWorkerInputsForTest(t)
	requestPath := filepath.Join(filepath.Dir(inputs.build.OutputPath), "PRODUCTION_BUILD_REQUEST.json")
	writeJSONValue(t, requestPath, inputs.build)
	repositoryRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	filterSHA := surfaceOracleFileSHA256(t, filepath.Join(inputs.fixture.oracleBundleRoot, "transport", "salesforce-first-filter.py"))
	ldflags := "-X github.com/glade-sh/glade/tools/internal/corpusassurance.testApprovedSalesforceFilterSHA256=" + filterSHA
	command := exec.Command("go", "run", "-ldflags", ldflags, "./cmd/glade-tools", "corpus", "assurance", "orchestrator", "production-build", "--request", requestPath)
	command.Dir = repositoryRoot
	stdout, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("production-build CLI: %v: %s", err, stdout)
	}
	var manifest ProductionRuntimeBatch
	if err := decodeExactJSON(stdout, &manifest); err != nil {
		t.Fatalf("production-build stdout: %s (%v)", stdout, err)
	}
	validated, err := validateOrchestratorProductionBatch(inputs.build.OutputPath, inputs.fixture.plan, inputs.fixture.lease)
	if err != nil || !reflect.DeepEqual(validated.manifest, manifest) {
		t.Fatalf("production-build retained manifest = %#v, stdout = %#v, err = %v", validated.manifest, manifest, err)
	}
}

func TestOrchestratorWorkerRejectsProductionMutationAfterValidation(t *testing.T) {
	inputs := newProductionWorkerInputsForTest(t)
	final := filepath.Join(inputs.worker.EvidenceRoot, orchestratorWorkerBatchName(inputs.fixture.lease))
	result, err := runOrchestratorWorkerOnce(context.Background(), inputs.orchestrator, inputs.worker, fixedOrchestratorWorkerClock(inputs.now), func(context.Context, OrchestratorWorkerRequest) (OrchestratorWorkerRunResult, error) {
		return OrchestratorWorkerRunResult{Production: &inputs.build}, nil
	}, VerifySalesforceReconciliation, func() error {
		reviewPath := filepath.Join(final, "production", "PRODUCTION_REVIEW.json")
		review, _, err := readMode0600JSON[ProductionRuntimeReview](reviewPath)
		if err != nil {
			return err
		}
		review.Rows[0].Classification = "mutated-after-validation"
		data, err := json.Marshal(review)
		if err != nil {
			return err
		}
		return os.WriteFile(reviewPath, append(data, '\n'), 0o600)
	})
	if err == nil || err.Error() != orchestratorWorkerCreditFailed {
		t.Fatalf("production mutation error = %v", err)
	}
	if result.Receipt.ID != "" || result.BatchRoot != "" || result.ManifestSHA256 != "" {
		t.Fatalf("production mutation result = %#v", result)
	}
	status, statusErr := inputs.orchestrator.Status(inputs.fixture.plan.CampaignID)
	if statusErr != nil || status.Accepted != 0 || status.Rejected != 0 {
		t.Fatalf("production mutation status = %#v, %v", status, statusErr)
	}
	var receipts int
	if queryErr := inputs.orchestrator.db.QueryRow(`SELECT count(*) FROM receipts WHERE campaign_id = ?`, inputs.fixture.plan.CampaignID).Scan(&receipts); queryErr != nil || receipts != 0 {
		t.Fatalf("production mutation receipts = %d, %v", receipts, queryErr)
	}
}

func TestOrchestratorValidatedProductionCarrierRejectsMutation(t *testing.T) {
	inputs := newProductionWorkerInputsForTest(t)
	if _, err := BuildOrchestratorProductionBatch(inputs.build); err != nil {
		t.Fatal(err)
	}
	transfer, carrier, err := transferValidatedOrchestratorWorkerBatch(OrchestratorWorkerTransferRequest{Plan: inputs.fixture.plan, Lease: inputs.fixture.lease, SourceBatchRoot: inputs.build.OutputPath, EvidenceRoot: inputs.worker.EvidenceRoot, OraclePlanPath: inputs.fixture.oraclePlanPath}, VerifySalesforceReconciliation, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := inputs.orchestrator.Reserve(inputs.fixture.lease, inputs.worker.HubAlias, inputs.worker.AllocationAlias, inputs.now); err != nil {
		t.Fatal(err)
	}
	claim, err := inputs.orchestrator.ClaimCleanupForLease(inputs.fixture.lease, inputs.worker.AllocationAlias, inputs.now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := inputs.orchestrator.CloseCleanup(claim, inputs.now); err != nil {
		t.Fatal(err)
	}
	request := OrchestratorReceiptRequest{Lease: inputs.fixture.lease, BatchRoot: transfer.BatchRoot}
	reviewPath := filepath.Join(transfer.BatchRoot, "production", "PRODUCTION_REVIEW.json")
	if err := os.WriteFile(reviewPath, []byte("mutated after validated receipt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	receipt, err := inputs.orchestrator.recordValidatedReceipt(request, inputs.now, carrier)
	if err == nil || receipt.ID != "" {
		t.Fatalf("mutated validated carrier receipt = %#v, %v", receipt, err)
	}
	var receipts, credits int
	if queryErr := inputs.orchestrator.db.QueryRow(`SELECT count(*) FROM receipts WHERE campaign_id = ?`, inputs.fixture.plan.CampaignID).Scan(&receipts); queryErr != nil || receipts != 0 {
		t.Fatalf("mutated validated carrier receipts = %d, %v", receipts, queryErr)
	}
	if queryErr := inputs.orchestrator.db.QueryRow(`SELECT count(*) FROM proof_credits WHERE campaign_id = ?`, inputs.fixture.plan.CampaignID).Scan(&credits); queryErr != nil || credits != 0 {
		t.Fatalf("mutated validated carrier credits = %d, %v", credits, queryErr)
	}
}

func TestOrchestratorWorkerRestartsFromPublishedProductionBatch(t *testing.T) {
	inputs := newProductionWorkerInputsForTest(t)
	_, err := BuildOrchestratorProductionBatch(inputs.build)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{inputs.build.LocalProofPath, inputs.build.ReviewPath, inputs.build.OracleBundleRoot, inputs.build.RawRoot, inputs.build.SalesforceReconciliationPath, inputs.build.SalesforcePacketPath} {
		if err := os.RemoveAll(path); err != nil {
			t.Fatal(err)
		}
	}
	var sequence int
	var databaseName, databasePath string
	if err := inputs.orchestrator.db.QueryRow(`PRAGMA database_list`).Scan(&sequence, &databaseName, &databasePath); err != nil {
		t.Fatal(err)
	}
	if err := inputs.orchestrator.Close(); err != nil {
		t.Fatal(err)
	}
	inputs.orchestrator, err = OpenOrchestrator(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = inputs.orchestrator.Close() })
	runner := func(context.Context, OrchestratorWorkerRequest) (OrchestratorWorkerRunResult, error) {
		return OrchestratorWorkerRunResult{Production: &inputs.build}, nil
	}
	first, err := runOrchestratorWorkerOnce(context.Background(), inputs.orchestrator, inputs.worker, fixedOrchestratorWorkerClock(inputs.now), runner, VerifySalesforceReconciliation, func() error { return nil })
	if err != nil || first.Receipt.AcceptedCredit != 1 || first.ManifestSHA256 != manifestSHAForTest(t, inputs.build.OutputPath) {
		t.Fatalf("published production restart = %#v, %v", first, err)
	}
	if err := inputs.orchestrator.Close(); err != nil {
		t.Fatal(err)
	}
	inputs.orchestrator, err = OpenOrchestrator(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := runOrchestratorWorkerOnce(context.Background(), inputs.orchestrator, inputs.worker, fixedOrchestratorWorkerClock(inputs.now.Add(time.Second)), func(context.Context, OrchestratorWorkerRequest) (OrchestratorWorkerRunResult, error) {
		return OrchestratorWorkerRunResult{}, fmt.Errorf("runner called during production replay")
	}, VerifySalesforceReconciliation, func() error { return nil })
	if err != nil || replayed != first {
		t.Fatalf("reopened production replay = %#v, want %#v, %v", replayed, first, err)
	}
}

func TestBuildOrchestratorProductionBatchRejectsPublishedLeaseDrift(t *testing.T) {
	inputs := newProductionWorkerInputsForTest(t)
	if _, err := BuildOrchestratorProductionBatch(inputs.build); err != nil {
		t.Fatal(err)
	}
	drifted := inputs.fixture.lease
	drifted.Worker = "worker-production-drifted"
	driftedPath := filepath.Join(filepath.Dir(inputs.build.OutputPath), "DRIFTED_LEASE.json")
	writeJSONValue(t, driftedPath, drifted)
	request := inputs.build
	request.LeasePath = driftedPath
	if _, err := BuildOrchestratorProductionBatch(request); err == nil || !strings.Contains(err.Error(), "lease drift") {
		t.Fatalf("published production lease drift error = %v", err)
	}
}

func TestOrchestratorProductionBatchRejectsRecursivePacketOverlap(t *testing.T) {
	inputs := newProductionWorkerInputsForTest(t)
	packet := inputs.build.SalesforcePacketPath
	tests := map[string]BuildOrchestratorProductionBatchRequest{}
	inside := inputs.build
	inside.OutputPath = filepath.Join(packet, "missing-parent", "production-batch")
	tests["output inside packet"] = inside
	equal := inputs.build
	equal.OutputPath = packet
	tests["output equals packet"] = equal
	outside := t.TempDir()
	containedPacket := filepath.Join(outside, "packet")
	if err := copyProductionTree(packet, containedPacket); err != nil {
		t.Fatal(err)
	}
	contains := inputs.build
	contains.OutputPath, contains.SalesforcePacketPath = outside, containedPacket
	tests["packet inside output"] = contains
	for name, request := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := BuildOrchestratorProductionBatch(request); err == nil || !strings.Contains(err.Error(), "overlap") {
				t.Fatalf("recursive packet overlap error = %v", err)
			}
		})
	}
	if _, err := os.Lstat(filepath.Dir(inside.OutputPath)); !os.IsNotExist(err) {
		t.Fatalf("recursive packet overlap left staging residue: %v", err)
	}
	temporary, err := filepath.Glob(filepath.Join(packet, ".production-batch-*"))
	if err != nil || len(temporary) != 0 {
		t.Fatalf("recursive packet overlap temporary paths = %v, %v", temporary, err)
	}
}

type productionWorkerInputs struct {
	orchestrator *Orchestrator
	fixture      orchestratorSalesforceReconciliationFixture
	now          time.Time
	build        BuildOrchestratorProductionBatchRequest
	worker       OrchestratorWorkerRequest
}

func newProductionWorkerInputsForTest(t *testing.T) productionWorkerInputs {
	t.Helper()
	fixture := withOraclePlanCampaignScope(t, newOrchestratorSalesforceReconciliationFixture(t))
	root := t.TempDir()
	orchestrator := openTestOrchestrator(t)
	if err := orchestrator.InitCampaign(fixture.plan); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	lease, err := orchestrator.Lease(fixture.plan.CampaignID, "worker-production-end-to-end", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	fixture.lease = lease
	reconciliationPath, packetPath := filepath.Join(root, "SALESFORCE_RECONCILIATION.json"), filepath.Join(root, "salesforce-packet")
	if _, err := CreateOrchestratorSalesforceReconciliation(OrchestratorSalesforceReconciliationRequest{Plan: fixture.plan, Lease: lease, OraclePlanPath: fixture.oraclePlanPath, BindingPath: fixture.bindingPath, ShardFiles: fixture.files, PacketOutput: packetPath, OutputPath: reconciliationPath}); err != nil {
		t.Fatal(err)
	}
	rawRoot := productionRawRootForTest(t, root, fixture, reconciliationPath)
	planPath, leasePath := filepath.Join(root, "ORCHESTRATOR_PLAN.json"), filepath.Join(root, "ORCHESTRATOR_LEASE.json")
	writeJSONValue(t, planPath, fixture.plan)
	writeJSONValue(t, leasePath, lease)
	reviewPath := filepath.Join(root, "PRODUCTION_REVIEW.json")
	writeJSONValue(t, reviewPath, ProductionRuntimeReview{SchemaVersion: 1, PlanSHA256: surfaceOracleFileSHA256(t, planPath), LeaseSHA256: surfaceOracleFileSHA256(t, leasePath), LocalProofSHA256: surfaceOracleFileSHA256(t, fixture.localProofPath), ReconciliationSHA256: surfaceOracleFileSHA256(t, reconciliationPath), Rows: []ProductionRuntimeReviewRow{{SurfaceID: "apex:Runtime.run", Action: oracleRuntime, Classification: "match", ReviewDisposition: "confirmed-match"}}})
	build := BuildOrchestratorProductionBatchRequest{PlanPath: planPath, LeasePath: leasePath, LocalProofPath: fixture.localProofPath, ReviewPath: reviewPath, OracleBundleRoot: fixture.oracleBundleRoot, RawRoot: rawRoot, SalesforceReconciliationPath: reconciliationPath, SalesforcePacketPath: packetPath, OutputPath: filepath.Join(root, "production-batch")}
	worker := OrchestratorWorkerRequest{Plan: fixture.plan, Lease: lease, HubAlias: "hub-production-worker", HubCapacity: 1, AllocationAlias: "scratch-production-worker", EvidenceRoot: filepath.Join(root, "evidence"), OraclePlanPath: fixture.oraclePlanPath}
	if err := orchestrator.SetHubCapacity(worker.HubAlias, worker.HubCapacity); err != nil {
		t.Fatal(err)
	}
	observeReadyHub(t, orchestrator, worker.HubAlias, now)
	return productionWorkerInputs{orchestrator: orchestrator, fixture: fixture, now: now, build: build, worker: worker}
}

func buildStandaloneProductionBatchForTest(t *testing.T) (string, orchestratorSalesforceReconciliationFixture, ProductionRuntimeBatch) {
	t.Helper()
	fixture := withOraclePlanCampaignScope(t, newOrchestratorSalesforceReconciliationFixture(t))
	root := t.TempDir()
	orchestrator := openTestOrchestrator(t)
	if err := orchestrator.InitCampaign(fixture.plan); err != nil {
		t.Fatal(err)
	}
	lease, err := orchestrator.Lease(fixture.plan.CampaignID, "worker-production-proof", time.Now().UTC(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	fixture.lease = lease
	reconciliationPath, packetPath := filepath.Join(root, "SALESFORCE_RECONCILIATION.json"), filepath.Join(root, "salesforce-packet")
	if _, err := CreateOrchestratorSalesforceReconciliation(OrchestratorSalesforceReconciliationRequest{Plan: fixture.plan, Lease: lease, OraclePlanPath: fixture.oraclePlanPath, BindingPath: fixture.bindingPath, ShardFiles: fixture.files, PacketOutput: packetPath, OutputPath: reconciliationPath}); err != nil {
		t.Fatal(err)
	}
	rawRoot := productionRawRootForTest(t, root, fixture, reconciliationPath)
	planPath, leasePath := filepath.Join(root, "ORCHESTRATOR_PLAN.json"), filepath.Join(root, "ORCHESTRATOR_LEASE.json")
	writeJSONValue(t, planPath, fixture.plan)
	writeJSONValue(t, leasePath, lease)
	reviewPath := filepath.Join(root, "PRODUCTION_REVIEW.json")
	writeJSONValue(t, reviewPath, ProductionRuntimeReview{SchemaVersion: 1, PlanSHA256: surfaceOracleFileSHA256(t, planPath), LeaseSHA256: surfaceOracleFileSHA256(t, leasePath), LocalProofSHA256: surfaceOracleFileSHA256(t, fixture.localProofPath), ReconciliationSHA256: surfaceOracleFileSHA256(t, reconciliationPath), Rows: []ProductionRuntimeReviewRow{{SurfaceID: "apex:Runtime.run", Action: oracleRuntime, Classification: "match", ReviewDisposition: "confirmed-match"}}})
	output := filepath.Join(root, "production-batch")
	manifest, err := BuildOrchestratorProductionBatch(BuildOrchestratorProductionBatchRequest{PlanPath: planPath, LeasePath: leasePath, LocalProofPath: fixture.localProofPath, ReviewPath: reviewPath, OracleBundleRoot: fixture.oracleBundleRoot, RawRoot: rawRoot, SalesforceReconciliationPath: reconciliationPath, SalesforcePacketPath: packetPath, OutputPath: output})
	if err != nil {
		t.Fatal(err)
	}
	return output, fixture, manifest
}

func productionSSHReceiptsForTest(t *testing.T, root, rawRoot string, plan OrchestratorCampaignPlan, lease OrchestratorLease, planSHA, leaseSHA string, executed RuntimeArtifact) (string, string, string) {
	t.Helper()
	tree := make([]orchestratorSSHRawManifestEntry, 0, len(orchestratorSSHRawFileNames()))
	for _, name := range orchestratorSSHRawFileNames() {
		info, err := os.Lstat(filepath.Join(rawRoot, name))
		if err != nil {
			t.Fatal(err)
		}
		tree = append(tree, orchestratorSSHRawManifestEntry{Path: name, Mode: formatModeForTest(info.Mode().Perm()), SHA256: surfaceOracleFileSHA256(t, filepath.Join(rawRoot, name))})
	}
	sort.Slice(tree, func(i, j int) bool { return tree[i].Path < tree[j].Path })
	treePath := filepath.Join(root, "TREE_MANIFEST.json")
	writeJSONValue(t, treePath, tree)
	dispatch := OrchestratorSSHDispatchReceipt{
		SchemaVersion: 1, CampaignID: lease.CampaignID, JobID: lease.JobID, ShardIndex: lease.ShardIndex, Generation: lease.Generation,
		Status: "worker-complete", CommandSHA256: strings.Repeat("1", 64), StdoutSHA256: strings.Repeat("2", 64), StderrSHA256: strings.Repeat("3", 64), ExitCode: 0, TimeoutMS: orchestratorSSHTimeout.Milliseconds(), Passed: true,
		SpecSHA256: plan.SpecSHA256, PlanSHA256: planSHA, LeaseSHA256: leaseSHA, OrchestratorBindingSHA256: surfaceOracleFileSHA256(t, filepath.Join(rawRoot, "ORCHESTRATOR_BINDING.json")), SalesforceShardSHA256: surfaceOracleFileSHA256(t, filepath.Join(rawRoot, "SALESFORCE_SHARD.json")), OrgCleanupSHA256: surfaceOracleFileSHA256(t, filepath.Join(rawRoot, "ORG_CLEANUP.json")), ExecutedTools: executed,
	}
	dispatchPath := filepath.Join(root, "SSH_DISPATCH.json")
	writeJSONValue(t, dispatchPath, dispatch)
	fetch := OrchestratorSSHRawFetchReceipt{
		SchemaVersion: 1, Status: "fetched", Passed: true, CampaignID: lease.CampaignID, JobID: lease.JobID, ShardIndex: lease.ShardIndex, Generation: lease.Generation,
		SpecSHA256: plan.SpecSHA256, PlanSHA256: planSHA, LeaseSHA256: leaseSHA, SSHReceiptSHA256: surfaceOracleFileSHA256(t, dispatchPath), TreeManifestSHA256: surfaceOracleFileSHA256(t, treePath),
		OrchestratorBindingSHA256: dispatch.OrchestratorBindingSHA256, SalesforceShardSHA256: dispatch.SalesforceShardSHA256, OrgCleanupSHA256: dispatch.OrgCleanupSHA256,
		CopyStdoutSHA256: strings.Repeat("4", 64), CopyStderrSHA256: strings.Repeat("5", 64), ChecksumStdoutSHA256: strings.Repeat("6", 64), ChecksumStderrSHA256: strings.Repeat("7", 64), ExecutedTools: executed,
	}
	fetchPath := filepath.Join(root, "SSH_FETCH.json")
	writeJSONValue(t, fetchPath, fetch)
	return dispatchPath, fetchPath, treePath
}

func formatModeForTest(mode os.FileMode) string {
	return fmt.Sprintf("%04o", mode)
}

func manifestSHAForTest(t *testing.T, root string) string {
	t.Helper()
	return surfaceOracleFileSHA256(t, filepath.Join(root, "production", "PRODUCTION_RUNTIME_BATCH.json"))
}

func mustProductionLocalProof(t *testing.T, path string) LocalProof {
	t.Helper()
	proof, _, err := readExactJSONBytes[LocalProof](path)
	if err != nil {
		t.Fatal(err)
	}
	return proof
}

func productionRawRootForTest(t *testing.T, root string, fixture orchestratorSalesforceReconciliationFixture, receiptPath string) string {
	t.Helper()
	rawRoot := filepath.Join(root, "raw")
	if err := os.Mkdir(rawRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	sources := map[string]string{
		"ORCHESTRATOR_BINDING.json": fixture.bindingPath,
		"ORG_CREATION.json":         fixture.files.CreationPath,
		"ORG_PREFLIGHT.json":        fixture.files.PreflightPath,
		"SALESFORCE_DISPATCH.json":  fixture.files.DispatchPath,
		"SALESFORCE_SHARD.json":     fixture.files.ShardPath,
		"ORG_CLEANUP.json":          fixture.files.CleanupPath,
	}
	for name, source := range sources {
		snapshot, err := readRegularFileSnapshot(source)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(rawRoot, name), snapshot.Data, snapshot.Mode.Perm()); err != nil {
			t.Fatal(err)
		}
	}
	reconciliation, _, err := readExactJSONBytes[SalesforceReconciliation](receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	bundle, _, err := readExactJSONBytes[OracleBundle](filepath.Join(fixture.oracleBundleRoot, "bundle", "bundle.json"))
	if err != nil {
		t.Fatal(err)
	}
	creation, _, err := readExactJSONBytes[SalesforceOrgCreation](fixture.files.CreationPath)
	if err != nil {
		t.Fatal(err)
	}
	bundlePath := filepath.Join(fixture.oracleBundleRoot, "bundle", "bundle.json")
	absence := salesforceCommandForTest(t, bundlePath, []string{"org", "display", "--target-org", creation.Alias, "--json"})
	absence.Passed, absence.ExitCode = false, 1
	absence.Output.Stdout = []byte(`{"status":1,"message":"not found"}`)
	absence.StdoutSHA256 = replayBytesSHA256(absence.Output.Stdout)
	writeJSONValue(t, filepath.Join(rawRoot, "ORG_CREATION.json.reservation"), salesforceOrgReservation{SchemaVersion: 1, BundleSHA256: reconciliation.BundleSHA256, DevHub: bundle.DevHub, Alias: creation.Alias, Marker: creation.Marker, AliasAbsent: absence})
	return rawRoot
}
