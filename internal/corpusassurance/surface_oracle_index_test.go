package corpusassurance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCreateSurfaceOracleIndex(t *testing.T) {
	root := t.TempDir()
	scopePath, batchRoot := writeSurfaceOracleIndexInputs(t, root)
	output := filepath.Join(root, "SURFACE_ORACLE_INDEX.json")
	index, err := CreateSurfaceOracleIndex(SurfaceOracleIndexRequest{ScopePath: scopePath, RuntimeBatchRoots: []string{batchRoot}, OutputPath: output})
	if err != nil {
		t.Fatalf("CreateSurfaceOracleIndex: %v", err)
	}
	if index.Total != 3 || index.Counts.Open != 1 || index.Counts.Matched != 2 {
		t.Fatalf("counts = %#v, total = %d", index.Counts, index.Total)
	}
	want := []SurfaceOracleIndexRow{
		{SurfaceID: "apex:System.One", State: "matched"},
		{SurfaceID: "apex:System.Three", State: "open"},
		{SurfaceID: "apex:System.Two", State: "matched"},
	}
	for i := range want {
		if index.Rows[i] != want[i] {
			t.Fatalf("row %d = %#v, want %#v", i, index.Rows[i], want[i])
		}
	}
	if index.ScopeSHA256 != surfaceOracleFileSHA256(t, scopePath) || len(index.RuntimeBatches) != 1 || index.RuntimeBatches[0].ManifestSHA256 != surfaceOracleFileSHA256(t, filepath.Join(batchRoot, "inputs", "RUNTIME_BATCH_MANIFEST.json")) {
		t.Fatalf("index bindings = %#v", index)
	}
	if strings.Join(index.RuntimeBatches[0].SurfaceIDs, ",") != "apex:System.One,apex:System.Two" {
		t.Fatalf("runtime batch surface IDs are not sorted: %#v", index.RuntimeBatches[0].SurfaceIDs)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var exact map[string]any
	if err := json.Unmarshal(data, &exact); err != nil {
		t.Fatal(err)
	}
	row := exact["rows"].([]any)[0].(map[string]any)
	if exact["kind"] != "all-runtime" || len(row) != 2 || row["state"] != "matched" || exact["candidate"].(map[string]any)["binarySha256"] == nil || exact["tools"].(map[string]any)["binarySha256"] == nil {
		t.Fatalf("public index shape = %#v", exact)
	}
	if _, err := CreateSurfaceOracleIndex(SurfaceOracleIndexRequest{ScopePath: scopePath, RuntimeBatchRoots: []string{batchRoot}, OutputPath: output}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("create-only error = %v", err)
	}
}

func TestCreateSurfaceOracleIndexRejectsUnrecordedProductionRuntimeBatch(t *testing.T) {
	batch, _, _ := buildStandaloneProductionBatchForTest(t)
	root := t.TempDir()
	scopePath := writeProductionSurfaceIndexScope(t, root)
	if _, err := CreateSurfaceOracleIndex(SurfaceOracleIndexRequest{ScopePath: scopePath, RuntimeBatchRoots: []string{batch}, OutputPath: filepath.Join(root, "index.json")}); err == nil || !strings.Contains(err.Error(), "orchestrator database") {
		t.Fatalf("unrecorded production batch error = %v", err)
	}
}

func TestCreateSurfaceOracleIndexPreservesProductionMismatch(t *testing.T) {
	batches, database, scopePath := writeRecordedProductionCampaignForSurfaceIndex(t, 0)
	root := t.TempDir()
	index, err := CreateSurfaceOracleIndex(SurfaceOracleIndexRequest{ScopePath: scopePath, RuntimeBatchRoots: batches, OrchestratorDBPath: database, OutputPath: filepath.Join(root, "index.json")})
	if err != nil {
		t.Fatal(err)
	}
	if index.Counts.Matched != 2 || index.Counts.ProductMismatch != 1 || index.Counts.Adjudicated != 3 || index.Counts.Open != 0 || index.Rows[0].State != "product-mismatch" {
		t.Fatalf("production mismatch index = %#v", index)
	}
	persisted, _, err := readExactJSONBytes[SurfaceOracleIndex](filepath.Join(root, "index.json"))
	if err != nil || ValidateSurfaceOracleIndex(persisted) != nil || persisted.RuntimeBatches[0].Layout != "orchestrator-production-v3" || persisted.Rows[0].State != "product-mismatch" {
		t.Fatalf("persisted production mismatch index = %#v, err = %v", persisted, err)
	}
}

func TestCreateSurfaceOracleIndexAcceptsRecordedProductionMatch(t *testing.T) {
	batches, database, scopePath := writeRecordedProductionCampaignForSurfaceIndex(t, -1)
	root := t.TempDir()
	index, err := CreateSurfaceOracleIndex(SurfaceOracleIndexRequest{ScopePath: scopePath, RuntimeBatchRoots: batches, OrchestratorDBPath: database, OutputPath: filepath.Join(root, "index.json")})
	if err != nil {
		t.Fatal(err)
	}
	if index.Counts.Matched != 3 || index.Counts.Adjudicated != 3 || index.Rows[0].State != "matched" || len(index.RuntimeBatches) != 3 {
		t.Fatalf("production index = %#v", index)
	}
	surfaceID := index.RuntimeBatches[0].SurfaceIDs[0]
	index.RuntimeBatches[0].States[0] = "inconclusive"
	for i := range index.Rows {
		if index.Rows[i].SurfaceID == surfaceID {
			index.Rows[i].State = "inconclusive"
		}
	}
	index.Counts = surfaceOracleIndexCounts(index.Rows)
	if err := ValidateSurfaceOracleIndex(index); err == nil || !strings.Contains(err.Error(), "runtime batch surface state") {
		t.Fatalf("production inconclusive state error = %v", err)
	}
}

func TestCreateSurfaceOracleIndexRejectsProductionReceiptDrift(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*testing.T, *Orchestrator)
	}{
		{name: "receipt root", mutate: func(t *testing.T, orchestrator *Orchestrator) {
			if _, err := orchestrator.db.Exec(`UPDATE receipts SET batch_root = batch_root || '-drift'`); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "proof state", mutate: func(t *testing.T, orchestrator *Orchestrator) {
			if _, err := orchestrator.db.Exec(`UPDATE proof_credits SET state = 'rejected'`); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "attempt lifecycle", mutate: func(t *testing.T, orchestrator *Orchestrator) {
			if _, err := orchestrator.db.Exec(`UPDATE attempts SET status = 'retryable' WHERE rowid = (SELECT min(rowid) FROM attempts)`); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "allocation lifecycle", mutate: func(t *testing.T, orchestrator *Orchestrator) {
			if _, err := orchestrator.db.Exec(`UPDATE scratch_allocations SET state = 'reserved'`); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "cleanup credit block", mutate: func(t *testing.T, orchestrator *Orchestrator) {
			if _, err := orchestrator.db.Exec(`INSERT INTO cleanup_credit_blocks (allocation_alias) SELECT allocation_alias FROM cleanup_journal`); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			batches, database, scopePath := writeRecordedProductionCampaignForSurfaceIndex(t, -1)
			orchestrator, err := OpenOrchestrator(database)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = orchestrator.Close() })
			test.mutate(t, orchestrator)
			root := t.TempDir()
			_, err = CreateSurfaceOracleIndex(SurfaceOracleIndexRequest{ScopePath: scopePath, RuntimeBatchRoots: batches, OrchestratorDBPath: database, OutputPath: filepath.Join(root, "index.json")})
			if err == nil {
				t.Fatal("production receipt drift was accepted")
			}
		})
	}
}

func TestCreateSurfaceOracleIndexRejectsOmittedCampaignReceipt(t *testing.T) {
	batches, database, scopePath := writeRecordedProductionCampaignForSurfaceIndex(t, -1)
	root := t.TempDir()
	_, err := CreateSurfaceOracleIndex(SurfaceOracleIndexRequest{ScopePath: scopePath, RuntimeBatchRoots: batches[:len(batches)-1], OrchestratorDBPath: database, OutputPath: filepath.Join(root, "index.json")})
	if err == nil || !strings.Contains(err.Error(), "exactly cover") {
		t.Fatalf("omitted campaign receipt error = %v", err)
	}
}

func TestProductionSurfaceReceiptDatabaseRejectsMixedCampaigns(t *testing.T) {
	batches, database, _ := writeRecordedProductionCampaignForSurfaceIndex(t, -1)
	authority, err := readProductionSurfaceReceiptAuthority(batches[0])
	if err != nil {
		t.Fatal(err)
	}
	mixed := authority
	mixed.lease.CampaignID += "-other"
	if _, err := validateProductionSurfaceReceiptDatabase(database, []productionSurfaceReceiptAuthority{authority, mixed}); err == nil || !strings.Contains(err.Error(), "one campaign") {
		t.Fatalf("mixed campaign error = %v", err)
	}
}

func TestCreateSurfaceOracleIndexAcceptsLeaseExpiryRetry(t *testing.T) {
	batches, database, scopePath := writeRecordedProductionCampaignForSurfaceIndex(t, -1, true)
	orchestrator, err := OpenOrchestrator(database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = orchestrator.Close() })
	var retryable int
	if err := orchestrator.db.QueryRow(`SELECT count(*) FROM attempts WHERE status = 'retryable'`).Scan(&retryable); err != nil || retryable != 1 {
		t.Fatalf("retryable attempts = %d, err = %v", retryable, err)
	}
	root := t.TempDir()
	if _, err := CreateSurfaceOracleIndex(SurfaceOracleIndexRequest{ScopePath: scopePath, RuntimeBatchRoots: batches, OrchestratorDBPath: database, OutputPath: filepath.Join(root, "index.json")}); err != nil {
		t.Fatalf("lease expiry retry: %v", err)
	}
}

func TestCreateSurfaceOracleIndexPreservesLegacyReviewedStates(t *testing.T) {
	for _, test := range []struct {
		name, classification, state string
	}{
		{name: "mismatch", classification: "mismatch", state: "product-mismatch"},
		{name: "inconclusive", classification: "environment", state: "inconclusive"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			scope, batch := writeSurfaceOracleIndexInputs(t, root)
			if test.classification == "mismatch" {
				setSurfaceOracleBatchFixtureKind(t, batch, "test")
			}
			setSurfaceOracleBatchAdjudication(t, batch, "apex:System.One", test.classification)
			index, err := CreateSurfaceOracleIndex(SurfaceOracleIndexRequest{ScopePath: scope, RuntimeBatchRoots: []string{batch}, OutputPath: filepath.Join(root, "index.json")})
			if err != nil {
				t.Fatal(err)
			}
			if index.Rows[0].State != test.state || strings.Join(index.RuntimeBatches[0].States, ",") != test.state+",matched" || index.Counts.Adjudicated != 2 || index.Counts.Open != 1 {
				t.Fatalf("legacy reviewed index = %#v", index)
			}
		})
	}
}

func TestCreateSurfaceOracleIndexRejectsUnsealedEvidence(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, string, string)
		want   string
	}{
		{name: "forged audit", want: "final audit", mutate: func(t *testing.T, _, batch string) {
			updateJSONMap(t, filepath.Join(batch, "evidence", "FINAL_AUDIT.json"), func(value map[string]any) { value["passed"] = false })
		}},
		{name: "hash drift", want: "local summary", mutate: func(t *testing.T, _, batch string) {
			updateJSONMap(t, filepath.Join(batch, "local-proof", "LOCAL_RUNTIME_SUMMARY.json"), func(value map[string]any) { value["selectedRows"] = float64(1) })
		}},
		{name: "symlink", want: "regular file", mutate: func(t *testing.T, root, batch string) {
			path := filepath.Join(batch, "evidence", "RECONCILIATION.json")
			copyPath := filepath.Join(root, "reconciliation-copy.json")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(copyPath, data, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(copyPath, path); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "count", want: "manifest", mutate: func(t *testing.T, _, batch string) {
			updateJSONMap(t, filepath.Join(batch, "inputs", "RUNTIME_BATCH_MANIFEST.json"), func(value map[string]any) { value["surfaceRowCount"] = float64(1) })
			resealSurfaceOracleBatch(t, batch)
		}},
		{name: "row set", want: "row set", mutate: func(t *testing.T, _, batch string) {
			updateJSONMap(t, filepath.Join(batch, "evidence", "MISMATCH_REVIEW.json"), func(value map[string]any) {
				value["rows"].([]any)[0].(map[string]any)["surfaceId"] = "apex:System.Forged"
			})
			resealSurfaceOracleBatch(t, batch)
		}},
		{name: "fixture traversal", want: "escapes root", mutate: func(t *testing.T, _, batch string) {
			outside := filepath.Join(batch, "source", "outside.json")
			writeJSONValue(t, outside, map[string]any{"fixture": "synthetic"})
			updateJSONMap(t, filepath.Join(batch, "inputs", "RUNTIME_BATCH_MANIFEST.json"), func(value map[string]any) {
				fixture := value["fixtures"].([]any)[0].(map[string]any)
				fixture["fixture"], fixture["path"], fixture["sha256"] = "outside", "../outside.json", surfaceOracleFileSHA256(t, outside)
			})
			resealSurfaceOracleBatch(t, batch)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			scope, batch := writeSurfaceOracleIndexInputs(t, root)
			test.mutate(t, root, batch)
			_, err := CreateSurfaceOracleIndex(SurfaceOracleIndexRequest{ScopePath: scope, RuntimeBatchRoots: []string{batch}, OutputPath: filepath.Join(root, "index.json")})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestCreateSurfaceOracleIndexAccumulatesOnlyDisjointIdenticalCandidateBatches(t *testing.T) {
	root := t.TempDir()
	scope, _ := writeSurfaceOracleIndexInputs(t, root)
	first := writeSurfaceOracleBatch(t, root, "first", []string{"apex:System.One"})
	second := writeSurfaceOracleBatch(t, root, "second", []string{"apex:System.Two"})
	index, err := CreateSurfaceOracleIndex(SurfaceOracleIndexRequest{ScopePath: scope, RuntimeBatchRoots: []string{second, first}, OutputPath: filepath.Join(root, "index.json")})
	if err != nil {
		t.Fatal(err)
	}
	if index.Counts.Matched != 2 || len(index.RuntimeBatches) != 2 || index.RuntimeBatches[0].ManifestSHA256 >= index.RuntimeBatches[1].ManifestSHA256 {
		t.Fatalf("cumulative index = %#v", index)
	}
	if _, err := CreateSurfaceOracleIndex(SurfaceOracleIndexRequest{ScopePath: scope, RuntimeBatchRoots: []string{first, first}, OutputPath: filepath.Join(root, "duplicate.json")}); err == nil || !strings.Contains(err.Error(), "more than one runtime batch") {
		t.Fatalf("duplicate credit error = %v", err)
	}
	writeFile(t, filepath.Join(second, "bin", "glade-sealed"), "different candidate")
	resealSurfaceOracleBatch(t, second)
	if _, err := CreateSurfaceOracleIndex(SurfaceOracleIndexRequest{ScopePath: scope, RuntimeBatchRoots: []string{first, second}, OutputPath: filepath.Join(root, "candidate.json")}); err == nil || !strings.Contains(err.Error(), "candidate or tools") {
		t.Fatalf("changed candidate error = %v", err)
	}
}

func TestCreateSurfaceOracleIndexExtendsPredecessor(t *testing.T) {
	root := t.TempDir()
	scope, _ := writeSurfaceOracleIndexInputs(t, root)
	first := writeSurfaceOracleBatch(t, root, "first", []string{"apex:System.One"})
	second := writeSurfaceOracleBatch(t, root, "second", []string{"apex:System.Two"})
	predecessorPath := filepath.Join(root, "predecessor.json")
	if _, err := CreateSurfaceOracleIndex(SurfaceOracleIndexRequest{ScopePath: scope, RuntimeBatchRoots: []string{first}, OutputPath: predecessorPath}); err != nil {
		t.Fatal(err)
	}
	index, err := CreateSurfaceOracleIndex(SurfaceOracleIndexRequest{ScopePath: scope, PredecessorIndexPath: predecessorPath, RuntimeBatchRoots: []string{second}, OutputPath: filepath.Join(root, "successor.json")})
	if err != nil {
		t.Fatal(err)
	}
	if index.Counts.Matched != 2 || index.Counts.Open != 1 || len(index.RuntimeBatches) != 2 {
		t.Fatalf("successor index = %#v", index)
	}
}

func TestCreateSurfaceOracleIndexIncludesTerminalAuthority(t *testing.T) {
	root := t.TempDir()
	scopePath, batch := writeSurfaceOracleIndexInputs(t, root)
	scope, scopeBytes, err := readExactJSONBytes[SurfaceOracleScope](scopePath)
	if err != nil {
		t.Fatal(err)
	}
	authority := SurfaceTerminalAuthority{
		SchemaVersion: 1, ScopeSHA256: replayBytesSHA256(scopeBytes), SourceProfileSHA256: scope.SourceProfileSHA256, LedgerSHA256: scope.LedgerSHA256, SupportPolicySHA256: scope.PolicySHA256,
		SourceCoverageSHA256: strings.Repeat("d", 64), DirectCoverageSHA256: strings.Repeat("e", 64), ClassificationSHA256: strings.Repeat("f", 64), FixtureSetSHA256: strings.Repeat("1", 64),
		Count: 1, ByClass: map[string]int{terminalHostedContext: 1}, Rows: []SurfaceTerminalAuthorityRow{{
			SurfaceID: "apex:System.Three", Class: terminalHostedContext, Reason: "synthetic terminal authority",
			Policy: SurfaceTerminalPolicyProvenance{Disposition: localRuntimeRequired, MatchRule: "synthetic", Reason: "synthetic"},
			Ledger: SurfaceTerminalLedgerProvenance{SHA256: strings.Repeat("2", 64), Sources: []string{"synthetic"}},
		}},
	}
	authority.RowsSHA256 = surfaceTerminalRowsSHA256(authority.Rows)
	authorityPath := filepath.Join(root, "terminal-authority.json")
	writeJSONValue(t, authorityPath, authority)
	index, err := CreateSurfaceOracleIndex(SurfaceOracleIndexRequest{ScopePath: scopePath, TerminalAuthorityPath: authorityPath, RuntimeBatchRoots: []string{batch}, OutputPath: filepath.Join(root, "index.json")})
	if err != nil {
		t.Fatal(err)
	}
	if index.Counts.Matched != 2 || index.Counts.ExplicitNonParity != 1 || index.Counts.Open != 0 || index.Rows[1].State != "explicit-non-parity" || index.TerminalAuthoritySHA256 != surfaceOracleFileSHA256(t, authorityPath) {
		t.Fatalf("terminal index = %#v", index)
	}
}

func TestValidateSurfaceOracleIndexRejectsForgedMatchedRow(t *testing.T) {
	root := t.TempDir()
	scope, batch := writeSurfaceOracleIndexInputs(t, root)
	if _, err := CreateSurfaceOracleIndex(SurfaceOracleIndexRequest{ScopePath: scope, OutputPath: filepath.Join(root, "empty.json")}); err == nil || !strings.Contains(err.Error(), "at least one") {
		t.Fatalf("empty batch set error = %v", err)
	}
	index, err := CreateSurfaceOracleIndex(SurfaceOracleIndexRequest{ScopePath: scope, RuntimeBatchRoots: []string{batch}, OutputPath: filepath.Join(root, "index.json")})
	if err != nil {
		t.Fatal(err)
	}
	index.Rows[1].State = "matched"
	index.Counts = surfaceOracleIndexCounts(index.Rows)
	if err := ValidateSurfaceOracleIndex(index); err == nil || !strings.Contains(err.Error(), "runtime batch adjudications") {
		t.Fatalf("forged matched row error = %v", err)
	}
	index.Rows[1].State = "inconclusive"
	index.Counts = surfaceOracleIndexCounts(index.Rows)
	if err := ValidateSurfaceOracleIndex(index); err == nil || !strings.Contains(err.Error(), "runtime batch adjudications") {
		t.Fatalf("speculative state error = %v", err)
	}
	index.Rows[0].State = "explicit-non-parity"
	index.Rows[1].State = "open"
	index.RuntimeBatches[0].States = []string{"explicit-non-parity", "matched"}
	index.Counts = surfaceOracleIndexCounts(index.Rows)
	if err := ValidateSurfaceOracleIndex(index); err == nil || !strings.Contains(err.Error(), "runtime batch surface state") {
		t.Fatalf("runtime-invented explicit non-parity error = %v", err)
	}
}

func writeProductionSurfaceIndexScope(t *testing.T, root string) string {
	t.Helper()
	path := filepath.Join(root, "scope.json")
	writeJSONValue(t, path, SurfaceOracleScope{
		SchemaVersion: 1, Kind: "all-runtime", SourceProfileSHA256: strings.Repeat("a", 64), LedgerSHA256: strings.Repeat("b", 64), PolicySHA256: strings.Repeat("c", 64),
		Total: 1, ByDisposition: map[string]int{deterministicMockRequired: 0, localRuntimeRequired: 1}, Rows: []SurfaceOracleScopeRow{{SurfaceID: "apex:Runtime.run", Disposition: localRuntimeRequired}},
	})
	return path
}

func writeRecordedProductionCampaignForSurfaceIndex(t *testing.T, mismatchIndex int, expireFirst ...bool) ([]string, string, string) {
	t.Helper()
	inputs := newProductionV3N3Fixture(t)
	if mismatchIndex >= 0 {
		fixture := inputs.fixture
		fixture.files = inputs.shards[mismatchIndex]
		markSalesforceFixtureMismatchForTest(t, fixture)
	}
	orchestrator := openTestOrchestrator(t)
	if err := orchestrator.InitCampaign(inputs.plan); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	batchRoots := make([]string, 0, len(inputs.plan.Jobs))
	for index := range inputs.plan.Jobs {
		lease, err := orchestrator.Lease(inputs.plan.CampaignID, fmt.Sprintf("surface-index-worker-%d", index), now, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		operationNow := now
		if index == 0 && len(expireFirst) != 0 && expireFirst[0] {
			operationNow = now.Add(2 * time.Minute)
			lease, err = orchestrator.Lease(inputs.plan.CampaignID, "surface-index-worker-0-retry", operationNow, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
		}
		root := t.TempDir()
		planPath, leasePath := filepath.Join(root, "ORCHESTRATOR_PLAN.json"), filepath.Join(root, "ORCHESTRATOR_LEASE.json")
		writeJSONValue(t, planPath, inputs.plan)
		writeJSONValue(t, leasePath, lease)
		bindingPath := filepath.Join(root, "ORCHESTRATOR_BINDING.json")
		if _, err := WriteOrchestratorBatchBinding(bindingPath, inputs.plan, lease); err != nil {
			t.Fatal(err)
		}
		reconciliationPath, packetPath := filepath.Join(root, "SALESFORCE_RECONCILIATION.json"), filepath.Join(root, "salesforce-packet")
		if _, err := CreateOrchestratorSalesforceReconciliation(OrchestratorSalesforceReconciliationRequest{Plan: inputs.plan, Lease: lease, OraclePlanPath: inputs.oraclePlanPath, BindingPath: bindingPath, ShardFiles: inputs.shards[index], PacketOutput: packetPath, OutputPath: reconciliationPath}); err != nil {
			t.Fatal(err)
		}
		jobFixture := inputs.fixture
		jobFixture.lease, jobFixture.bindingPath, jobFixture.files = lease, bindingPath, inputs.shards[index]
		rawRoot := productionRawRootForTest(t, root, jobFixture, reconciliationPath)
		classification, disposition := "match", "confirmed-match"
		if index == mismatchIndex {
			classification, disposition = "mismatch", "confirmed-mismatch"
		}
		reviewPath := filepath.Join(root, "PRODUCTION_REVIEW.json")
		writeJSONValue(t, reviewPath, ProductionRuntimeReview{SchemaVersion: 1, PlanSHA256: surfaceOracleFileSHA256(t, planPath), LeaseSHA256: surfaceOracleFileSHA256(t, leasePath), LocalProofSHA256: surfaceOracleFileSHA256(t, inputs.localProofPath), ReconciliationSHA256: surfaceOracleFileSHA256(t, reconciliationPath), Rows: []ProductionRuntimeReviewRow{{SurfaceID: lease.SurfaceIDs[0], Action: oracleRuntime, Classification: classification, ReviewDisposition: disposition}}})
		output := filepath.Join(root, "production-batch")
		if _, err := BuildOrchestratorProductionBatch(BuildOrchestratorProductionBatchRequest{PlanPath: planPath, LeasePath: leasePath, LocalProofPath: inputs.localProofPath, ReviewPath: reviewPath, OracleBundleRoot: inputs.oracleBundleRoot, RawRoot: rawRoot, SalesforceReconciliationPath: reconciliationPath, SalesforcePacketPath: packetPath, OutputPath: output}); err != nil {
			t.Fatal(err)
		}
		transfer, err := TransferOrchestratorWorkerBatch(OrchestratorWorkerTransferRequest{Plan: inputs.plan, Lease: lease, SourceBatchRoot: output, EvidenceRoot: filepath.Join(root, "evidence"), OraclePlanPath: inputs.oraclePlanPath})
		if err != nil {
			t.Fatal(err)
		}
		hub, allocation := fmt.Sprintf("surface-index-hub-%d", index), fmt.Sprintf("surface-index-scratch-%d", index)
		if err := orchestrator.SetHubCapacity(hub, 1); err != nil {
			t.Fatal(err)
		}
		observeReadyHub(t, orchestrator, hub, now)
		if err := orchestrator.Reserve(lease, hub, allocation, operationNow); err != nil {
			t.Fatal(err)
		}
		claim, err := orchestrator.ClaimCleanup(inputs.plan.CampaignID, lease.Worker, operationNow, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if err := orchestrator.CloseCleanup(claim, operationNow.Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		if _, err := orchestrator.RecordReceipt(OrchestratorReceiptRequest{Lease: lease, BatchRoot: transfer.BatchRoot}, operationNow.Add(2*time.Second)); err != nil {
			t.Fatal(err)
		}
		batchRoots = append(batchRoots, transfer.BatchRoot)
	}
	var sequence int
	var name, database string
	if err := orchestrator.db.QueryRow(`PRAGMA database_list`).Scan(&sequence, &name, &database); err != nil {
		t.Fatal(err)
	}
	scopePath := filepath.Join(t.TempDir(), "surface-index-scope.json")
	rows := make([]SurfaceOracleScopeRow, len(inputs.surfaceIDs))
	for i, surfaceID := range inputs.surfaceIDs {
		rows[i] = SurfaceOracleScopeRow{SurfaceID: surfaceID, Disposition: localRuntimeRequired}
	}
	writeJSONValue(t, scopePath, SurfaceOracleScope{SchemaVersion: 1, Kind: "all-runtime", SourceProfileSHA256: strings.Repeat("a", 64), LedgerSHA256: strings.Repeat("b", 64), PolicySHA256: strings.Repeat("c", 64), Total: len(rows), ByDisposition: map[string]int{deterministicMockRequired: 0, localRuntimeRequired: len(rows)}, Rows: rows})
	return batchRoots, database, scopePath
}

func writeSurfaceOracleIndexInputs(t *testing.T, root string) (string, string) {
	t.Helper()
	hashA, hashB, hashC := strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)
	scope := SurfaceOracleScope{SchemaVersion: 1, Kind: "all-runtime", SourceProfileSHA256: hashA, LedgerSHA256: hashB, PolicySHA256: hashC, Total: 3, ByDisposition: map[string]int{deterministicMockRequired: 1, localRuntimeRequired: 2}, Rows: []SurfaceOracleScopeRow{{SurfaceID: "apex:System.One", Disposition: deterministicMockRequired}, {SurfaceID: "apex:System.Three", Disposition: localRuntimeRequired}, {SurfaceID: "apex:System.Two", Disposition: localRuntimeRequired}}}
	scopePath := filepath.Join(root, "scope.json")
	writeJSONValue(t, scopePath, scope)
	return scopePath, writeSurfaceOracleBatch(t, root, "batch", []string{"apex:System.Two", "apex:System.One"})
}

func writeSurfaceOracleBatch(t *testing.T, root, name string, ids []string) string {
	t.Helper()
	batch := filepath.Join(root, name)
	if err := os.MkdirAll(batch, 0o700); err != nil {
		t.Fatal(err)
	}
	idsJSON, rawRows, reviewRows := make([]any, len(ids)), make([]any, len(ids)), make([]any, len(ids))
	for i, id := range ids {
		idsJSON[i], rawRows[i], reviewRows[i] = id, surfaceOracleRawRow(id), surfaceOracleReviewRow(id)
	}
	profilePath := filepath.Join(batch, "inputs", "RUNTIME_BATCH_PROFILE.json")
	writeJSONValue(t, profilePath, map[string]any{"profile": "synthetic"})
	fixturePath := filepath.Join(batch, "source", "glade-tools", "docs", "fixtures", "fixture-one.json")
	writeJSONValue(t, fixturePath, map[string]any{"fixture": "synthetic", "command": map[string]any{"kind": "exec"}})
	writeJSONValue(t, filepath.Join(batch, "inputs", "RUNTIME_BATCH_MANIFEST.json"), map[string]any{
		"schemaVersion": 1, "selectionPolicy": "whole, disjoint, inline anonymous-runtime fixtures with assertion-bearing programs", "surfaceRowCount": len(ids),
		"fixtures": []any{map[string]any{"fixture": "fixture-one", "path": "docs/fixtures/fixture-one.json", "rowCount": len(ids), "salesforceEligible": true, "sha256": surfaceOracleFileSHA256(t, fixturePath), "sourceFiles": []any{}, "surfaceIds": idsJSON}},
	})
	if err := os.Chmod(filepath.Join(batch, "inputs", "RUNTIME_BATCH_MANIFEST.json"), 0o400); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(batch, "bin", "glade-sealed"), "candidate")
	writeFile(t, filepath.Join(batch, "bin", "glade-tools"), "tools")
	writeFile(t, filepath.Join(batch, "source", "glade-tools", "scripts", "corpus-assurance", "salesforce-first-filter.py"), "# synthetic\n")
	writeJSONValue(t, filepath.Join(batch, "evidence", "RUN_SUMMARY.json"), map[string]any{})
	writeJSONValue(t, filepath.Join(batch, "evidence", "ORG_CLEANUP.json"), map[string]any{})
	writeJSONValue(t, filepath.Join(batch, "local-proof", "LOCAL_RUNTIME_SUMMARY.json"), map[string]any{
		"schemaVersion": 1, "sealed": true, "candidateSha256": "", "manifestSha256": "", "selectedFixtures": 1, "selectedRows": len(ids),
		"startedAtUnixNs": 1, "endedAtUnixNs": 2, "durationMs": 1.0,
		"results": []any{map[string]any{"candidateExitCode": 0, "candidateStatus": "passed", "durationMs": 1.0, "endedAtUnixNs": 2, "exitCode": 0, "fixture": "fixture-one.json", "kind": "exec", "path": "docs/fixtures/fixture-one.json", "result": map[string]any{}, "startedAtUnixNs": 1, "status": "exit-0", "stderrSha256": strings.Repeat("e", 64), "stdoutSha256": strings.Repeat("f", 64)}},
	})
	writeJSONValue(t, filepath.Join(batch, "evidence", "RECONCILIATION.json"), map[string]any{
		"schemaVersion": 1, "sealed": true, "manifestSha256": "", "runtimeRequested": true, "orgPostflightMatched": true, "runnerError": nil,
		"counts": map[string]any{"environment": 0, "match": len(ids), "mismatch": 0},
		"rows":   rawRows,
	})
	writeJSONValue(t, filepath.Join(batch, "evidence", "MISMATCH_REVIEW.json"), map[string]any{
		"schemaVersion": 1, "sealed": true, "manifestSha256": "", "oracleResultsSha256": "", "rawReconciliationSha256": "",
		"rawClassifications": map[string]any{"environment": 0, "match": len(ids), "mismatch": 0}, "reviewCounts": map[string]any{"confirmedMatch": len(ids)},
		"groups": []any{map[string]any{"confirmedMatchRows": len(ids), "fixture": "fixture-one.json"}},
		"rows":   reviewRows,
	})
	writeJSONValue(t, filepath.Join(batch, "oracle", "results.json"), surfaceOracleResultsValue(strings.Repeat("1", 40), strings.Repeat("2", 40), idsJSON))
	writeJSONValue(t, filepath.Join(batch, "evidence", "BINDINGS.json"), map[string]any{})
	writeJSONValue(t, filepath.Join(batch, "evidence", "FINAL_AUDIT.json"), surfaceOracleFinalAudit())
	resealSurfaceOracleBatch(t, batch)
	return batch
}

func surfaceOracleRawRow(id string) map[string]any {
	return map[string]any{"classification": "match", "fixture": "fixture-one.json", "local": map[string]any{"candidateExitCode": 0, "candidateStatus": "passed", "status": "exit-0"}, "reason": "local-and-salesforce-runtime-passed", "salesforce": map[string]any{"componentFailures": []any{}, "deployable": true, "exitCode": 0, "runtimeExitCode": nil, "runtimePassed": true, "runtimeRequested": true, "runtimeStatus": "Passed", "status": "Succeeded"}, "surfaceId": id}
}

func surfaceOracleReviewRow(id string) map[string]any {
	return map[string]any{"fixture": "fixture-one.json", "reviewDisposition": "confirmed-match", "sealedClassification": "match", "surfaceId": id}
}

func surfaceOracleResultsValue(candidateCommit, toolsCommit string, ids []any) map[string]any {
	result := map[string]any{"manifestIndex": 0, "fixture": "fixture-one.json", "fixtureSha256": strings.Repeat("d", 64), "manifestSha256": "", "candidateCommit": candidateCommit, "candidateSha256": "", "toolsCommit": toolsCommit, "workflowScriptSha256": "", "status": "Succeeded", "deployable": true, "exitCode": 0, "runtimeRequested": true, "runtimePassed": true, "runtimeExitCode": nil, "runtimeStatus": "Passed", "surfaceIds": ids, "classNameMap": map[string]any{}, "componentFailures": []any{}, "componentSuccesses": []any{}, "coverage": map[string]any{}, "execution": map[string]any{}, "invocation": map[string]any{}, "kind": "exec", "org": map[string]any{}, "orgCleanup": map[string]any{}, "orgIdentity": map[string]any{}, "project": map[string]any{}, "projectManifest": map[string]any{}, "runtimeResult": map[string]any{}, "sourceFiles": []any{}, "testClasses": []any{}}
	return map[string]any{"schemaVersion": 1, "sealed": true, "binding": map[string]any{"manifestSha256": "", "profileSha256": "", "queueSha256": nil, "selectorSha256": nil, "selectorReceiptSha256": nil, "selectionSha256": strings.Repeat("3", 64), "candidateCommit": candidateCommit, "candidateSha256": "", "toolsCommit": toolsCommit, "toolsAmd64Sha256": "", "workflowScriptSha256": "", "orgPreflightSha256": strings.Repeat("4", 64), "localSummarySha256": ""}, "selectedFixtures": 1, "excludedFixtures": 0, "selectedRows": len(ids), "excludedRows": 0, "runtimeRequested": true, "orgIdentities": map[string]any{}, "workerExecution": map[string]any{}, "orgPreflightSha256": strings.Repeat("4", 64), "localSummarySha256": "", "selectionSha256": strings.Repeat("3", 64), "selectedManifestIndexes": []any{0}, "orgPostflight": map[string]any{}, "skippedDeferredFixtures": []any{}, "orgs": []any{}, "results": []any{result}}
}

func surfaceOracleFinalAudit() map[string]any {
	checks := map[string]any{}
	for _, name := range []string{"candidateHashMatched", "cleanupReceiptPassed", "credentialScanClean", "finalActiveRecordResidueZero", "finalOrgDisplayRejected", "finalQuotaMatchedReceipt", "localRowsAllPassed", "manifestHashMatched", "manifestMode0400", "manifestRowsBoundedUnique", "oraclePostflightMatched", "oracleSealedRuntime", "privateRootMode0700", "reconciliationSealed", "runtimeReviewSealed", "sourceHeadsCleanExact", "toolsHashMatched", "workflowHashMatched"} {
		checks[name] = true
	}
	return map[string]any{"schemaVersion": 1, "passed": true, "artifactHashes": map[string]any{}, "checks": checks, "finalQuota": map[string]any{"activeScratchOrgs": map[string]any{"max": 3, "remaining": 3}, "dailyScratchOrgs": map[string]any{"max": 6, "remaining": 2}}, "finalResidueCount": 0, "privacyScan": map[string]any{"credentialJsonKeyHits": 0, "credentialPatternHits": 0, "scannedFiles": 1}, "sourceChecks": map[string]any{"glade": map[string]any{"clean": true, "headMatched": true}, "glade-tools": map[string]any{"clean": true, "headMatched": true}}}
}

func resealSurfaceOracleBatch(t *testing.T, batch string) {
	t.Helper()
	manifest, profile := filepath.Join(batch, "inputs", "RUNTIME_BATCH_MANIFEST.json"), filepath.Join(batch, "inputs", "RUNTIME_BATCH_PROFILE.json")
	local, oracle := filepath.Join(batch, "local-proof", "LOCAL_RUNTIME_SUMMARY.json"), filepath.Join(batch, "oracle", "results.json")
	reconciliation, review := filepath.Join(batch, "evidence", "RECONCILIATION.json"), filepath.Join(batch, "evidence", "MISMATCH_REVIEW.json")
	candidate, tools, workflow := filepath.Join(batch, "bin", "glade-sealed"), filepath.Join(batch, "bin", "glade-tools"), filepath.Join(batch, "source", "glade-tools", "scripts", "corpus-assurance", "salesforce-first-filter.py")
	manifestHash, candidateHash, toolsHash, workflowHash := surfaceOracleFileSHA256(t, manifest), surfaceOracleFileSHA256(t, candidate), surfaceOracleFileSHA256(t, tools), surfaceOracleFileSHA256(t, workflow)
	updateJSONMap(t, local, func(v map[string]any) { v["manifestSha256"], v["candidateSha256"] = manifestHash, candidateHash })
	localHash := surfaceOracleFileSHA256(t, local)
	updateJSONMap(t, oracle, func(v map[string]any) {
		v["localSummarySha256"] = localHash
		b := v["binding"].(map[string]any)
		b["manifestSha256"], b["profileSha256"], b["candidateSha256"], b["toolsAmd64Sha256"], b["workflowScriptSha256"], b["localSummarySha256"] = manifestHash, surfaceOracleFileSHA256(t, profile), candidateHash, toolsHash, workflowHash, localHash
		fixtureSHA := readJSONMap(t, manifest)["fixtures"].([]any)[0].(map[string]any)["sha256"]
		for _, item := range v["results"].([]any) {
			r := item.(map[string]any)
			r["manifestSha256"], r["candidateSha256"], r["workflowScriptSha256"] = manifestHash, candidateHash, workflowHash
			r["fixtureSha256"] = fixtureSHA
		}
	})
	updateJSONMap(t, reconciliation, func(v map[string]any) { v["manifestSha256"] = manifestHash })
	updateJSONMap(t, review, func(v map[string]any) {
		v["manifestSha256"], v["oracleResultsSha256"], v["rawReconciliationSha256"] = manifestHash, surfaceOracleFileSHA256(t, oracle), surfaceOracleFileSHA256(t, reconciliation)
	})
	bindings := map[string]any{"candidateCommit": strings.Repeat("1", 40), "candidateSha256": candidateHash, "localSummarySha256": localHash, "manifestSha256": manifestHash, "profileSha256": surfaceOracleFileSHA256(t, profile), "scratchDefinitionSha256": strings.Repeat("5", 64), "sfCliSha256": strings.Repeat("6", 64), "toolsCommit": strings.Repeat("2", 40), "toolsSha256": toolsHash, "workflowScriptSha256": workflowHash}
	writeJSONValue(t, filepath.Join(batch, "evidence", "BINDINGS.json"), bindings)
	auditPath := filepath.Join(batch, "evidence", "FINAL_AUDIT.json")
	updateJSONMap(t, auditPath, func(v map[string]any) {
		v["artifactHashes"] = map[string]any{"bindings": surfaceOracleFileSHA256(t, filepath.Join(batch, "evidence", "BINDINGS.json")), "cleanup": surfaceOracleFileSHA256(t, filepath.Join(batch, "evidence", "ORG_CLEANUP.json")), "localSummary": localHash, "manifest": manifestHash, "mismatchReview": surfaceOracleFileSHA256(t, review), "oracleResults": surfaceOracleFileSHA256(t, oracle), "reconciliation": surfaceOracleFileSHA256(t, reconciliation), "runSummary": surfaceOracleFileSHA256(t, filepath.Join(batch, "evidence", "RUN_SUMMARY.json"))}
	})
}

func updateJSONMap(t *testing.T, path string, update func(map[string]any)) {
	t.Helper()
	value := readJSONMap(t, path)
	update(value)
	mode := os.FileMode(0o600)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	writeJSONValue(t, path, value)
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func readJSONMap(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func writeJSONValue(t *testing.T, path string, value any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, value string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0o700); err != nil {
		t.Fatal(err)
	}
}

func surfaceOracleFileSHA256(t *testing.T, path string) string {
	t.Helper()
	hash, err := sha256FileDirect(path)
	if err != nil {
		t.Fatal(err)
	}
	return hash
}
