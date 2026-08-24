package corpusassurance

import (
	"database/sql"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestOrchestratorOpensLocalWALDatabase(t *testing.T) {
	if _, err := OpenOrchestrator("relative.db"); err == nil {
		t.Fatal("relative database path accepted")
	}
	orchestrator := openTestOrchestrator(t)
	var mode string
	if err := orchestrator.db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if strings.ToLower(mode) != "wal" {
		t.Fatalf("journal mode = %q, want wal", mode)
	}
	for _, table := range []string{"campaigns", "jobs", "attempts", "hub_observations", "receipts", "proof_credits", "hub_capacity", "scratch_allocations", "cleanup_journal", "actions"} {
		var found string
		if err := orchestrator.db.QueryRow("SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&found); err != nil {
			t.Fatalf("missing table %s: %v", table, err)
		}
	}
}

func TestOrchestratorMigratesLegacyTwoShardSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orchestrator.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Exec(`CREATE TABLE campaigns (id TEXT PRIMARY KEY, spec_sha256 TEXT NOT NULL, candidate_commit TEXT NOT NULL, candidate_sha256 TEXT NOT NULL, tools_commit TEXT NOT NULL, tools_sha256 TEXT NOT NULL, scope_path TEXT NOT NULL, scope_sha256 TEXT NOT NULL, controlled_inputs_json TEXT NOT NULL, created_at INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Exec(`CREATE TABLE jobs (campaign_id TEXT NOT NULL, id TEXT NOT NULL, kind TEXT NOT NULL CHECK (kind = 'surface-runtime-shard'), shard_index INTEGER NOT NULL CHECK (shard_index IN (0, 1)), surface_ids_json TEXT NOT NULL, status TEXT NOT NULL, generation INTEGER NOT NULL, leased_by TEXT, lease_until INTEGER, heartbeat_at INTEGER, PRIMARY KEY (campaign_id, id), UNIQUE (campaign_id, shard_index))`); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Exec(`CREATE TABLE attempts (campaign_id TEXT NOT NULL, job_id TEXT NOT NULL, generation INTEGER NOT NULL, worker TEXT NOT NULL, status TEXT NOT NULL, leased_at INTEGER NOT NULL, lease_until INTEGER NOT NULL, heartbeat_at INTEGER NOT NULL, PRIMARY KEY (campaign_id, job_id, generation), FOREIGN KEY (campaign_id, job_id) REFERENCES jobs(campaign_id, id))`); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Exec(`INSERT INTO campaigns VALUES ('campaign', 'spec', 'candidate', 'candidate-sha', 'tools', 'tools-sha', '/scope', 'scope-sha', '{}', 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Exec(`INSERT INTO jobs VALUES ('campaign', 'job-0', 'surface-runtime-shard', 0, '[]', 'queued', 0, NULL, NULL, NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Exec(`INSERT INTO attempts VALUES ('campaign', 'job-0', 0, 'worker', 'closed', 1, 2, 1)`); err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}
	orchestrator, err := OpenOrchestrator(path)
	if err != nil {
		t.Fatal(err)
	}
	defer orchestrator.Close()
	var schema string
	if err := orchestrator.db.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'jobs'`).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(schema, "shard_index IN (0, 1)") {
		t.Fatalf("legacy shard constraint remained: %s", schema)
	}
	var foreignKeyErrors int
	if err := orchestrator.db.QueryRow(`SELECT count(*) FROM pragma_foreign_key_check`).Scan(&foreignKeyErrors); err != nil || foreignKeyErrors != 0 {
		t.Fatalf("foreign keys after migration = %d, err = %v", foreignKeyErrors, err)
	}
	if _, err := orchestrator.db.Exec(`INSERT INTO jobs (campaign_id, id, kind, shard_index, surface_ids_json, status, generation) VALUES ('campaign', 'job-2', 'surface-runtime-shard', 2, '[]', 'queued', 0)`); err != nil {
		t.Fatalf("shard index 2 rejected after migration: %v", err)
	}
}

func TestOrchestratorPlansExactlyTwoBoundShardJobs(t *testing.T) {
	root := t.TempDir()
	scope, _ := writeSurfaceOracleIndexInputs(t, root)
	definition := testOrchestratorDefinition(t, scope, [][]string{{"apex:System.One", "apex:System.Two"}, {"apex:System.Three"}})
	plan, err := PlanOrchestratorCampaign(definition)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(plan.CampaignID, "campaign-") || len(plan.Jobs) != 2 {
		t.Fatalf("plan = %#v", plan)
	}
	for index, job := range plan.Jobs {
		if job.Kind != OrchestratorJobSurfaceRuntimeShard || job.ShardIndex != index || !strings.HasPrefix(job.ID, plan.CampaignID+":") {
			t.Fatalf("job %d = %#v", index, job)
		}
	}

	orchestrator := openTestOrchestrator(t)
	if err := orchestrator.InitCampaign(plan); err != nil {
		t.Fatal(err)
	}
	var jobs int
	if err := orchestrator.db.QueryRow(`SELECT count(*) FROM jobs WHERE campaign_id = ?`, plan.CampaignID).Scan(&jobs); err != nil || jobs != 2 {
		t.Fatalf("atomic campaign jobs = %d, err = %v", jobs, err)
	}
	if err := orchestrator.InitCampaign(plan); err != nil {
		t.Fatalf("identical campaign init is not idempotent: %v", err)
	}
	if err := orchestrator.Enqueue(plan); err != nil {
		t.Fatalf("identical enqueue is not idempotent: %v", err)
	}
	immutableColumns := map[string]string{
		"candidate_commit":       strings.Repeat("9", 40),
		"candidate_sha256":       strings.Repeat("9", 64),
		"tools_commit":           strings.Repeat("8", 40),
		"tools_sha256":           strings.Repeat("8", 64),
		"scope_path":             filepath.Join(t.TempDir(), "foreign-scope.json"),
		"scope_sha256":           strings.Repeat("7", 64),
		"controlled_inputs_json": `{"foreign":"` + strings.Repeat("6", 64) + `"}`,
	}
	for column, value := range immutableColumns {
		t.Run("rejects drifted "+column, func(t *testing.T) {
			drifted := openTestOrchestrator(t)
			if err := drifted.InitCampaign(plan); err != nil {
				t.Fatal(err)
			}
			if _, err := drifted.db.Exec(`UPDATE campaigns SET `+column+` = ? WHERE id = ?`, value, plan.CampaignID); err != nil {
				t.Fatal(err)
			}
			if err := drifted.InitCampaign(plan); err == nil || !strings.Contains(err.Error(), "binding drift") {
				t.Fatalf("drifted %s error = %v", column, err)
			}
		})
	}
	var candidateCommit, candidateHash, toolsCommit, toolsHash, scopeHash, inputs string
	if err := orchestrator.db.QueryRow(`SELECT candidate_commit, candidate_sha256, tools_commit, tools_sha256, scope_sha256, controlled_inputs_json FROM campaigns WHERE id = ?`, plan.CampaignID).Scan(&candidateCommit, &candidateHash, &toolsCommit, &toolsHash, &scopeHash, &inputs); err != nil {
		t.Fatal(err)
	}
	if candidateCommit != definition.Candidate.Commit || candidateHash != definition.Candidate.SHA256 || toolsCommit != definition.Tools.Commit || toolsHash != definition.Tools.SHA256 || scopeHash != definition.ScopeSHA256 || !strings.Contains(inputs, "oracle-plan") {
		t.Fatalf("stored bindings = %q %q %q %q %q %q", candidateCommit, candidateHash, toolsCommit, toolsHash, scopeHash, inputs)
	}

	unknown := plan
	unknown.Jobs = append([]OrchestratorJob(nil), plan.Jobs...)
	unknown.Jobs[0].Kind = OrchestratorJobKind("shell")
	if err := orchestrator.Enqueue(unknown); err == nil || !strings.Contains(err.Error(), "kind") {
		t.Fatalf("unknown kind error = %v", err)
	}
	drifted := plan
	drifted.Jobs = append([]OrchestratorJob(nil), plan.Jobs...)
	drifted.Jobs[0].SurfaceIDs = []string{"apex:System.Forged"}
	if err := orchestrator.Enqueue(drifted); err == nil || !strings.Contains(err.Error(), "drift") {
		t.Fatalf("drift error = %v", err)
	}
	if _, err := PlanOrchestratorCampaign(testOrchestratorDefinition(t, scope, [][]string{{"apex:System.One"}, {"apex:System.One"}})); err == nil || !strings.Contains(err.Error(), "disjoint") {
		t.Fatalf("overlapping shards error = %v", err)
	}
	if _, err := PlanOrchestratorCampaign(testOrchestratorDefinition(t, scope, [][]string{{"apex:System.One"}, {"apex:System.Three"}})); err == nil || !strings.Contains(err.Error(), "partition") {
		t.Fatalf("incomplete scope partition error = %v", err)
	}
}

func TestOrchestratorPlansFiveDeterministicShardJobs(t *testing.T) {
	root := t.TempDir()
	ids := []string{"apex:System.Five", "apex:System.Four", "apex:System.One", "apex:System.Three", "apex:System.Two"}
	rows := make([]SurfaceOracleScopeRow, len(ids))
	for index, id := range ids {
		rows[index] = SurfaceOracleScopeRow{SurfaceID: id, Disposition: localRuntimeRequired}
	}
	scope := filepath.Join(root, "scope.json")
	writeJSONValue(t, scope, SurfaceOracleScope{SchemaVersion: 1, Kind: "all-runtime", SourceProfileSHA256: strings.Repeat("a", 64), LedgerSHA256: strings.Repeat("b", 64), PolicySHA256: strings.Repeat("c", 64), Total: len(rows), ByDisposition: map[string]int{deterministicMockRequired: 0, localRuntimeRequired: len(rows)}, Rows: rows})
	definition := testOrchestratorDefinition(t, scope, [][]string{
		{"apex:System.Five"},
		{"apex:System.Four"},
		{"apex:System.One"},
		{"apex:System.Three"},
		{"apex:System.Two"},
	})
	plan, err := PlanOrchestratorCampaign(definition)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Jobs) != 5 {
		t.Fatalf("job count = %d, want 5", len(plan.Jobs))
	}
	for index, job := range plan.Jobs {
		if job.ShardIndex != index || job.SurfaceIDs[0] != definition.Shards[index][0] {
			t.Fatalf("job %d = %#v", index, job)
		}
	}
	if plan.Jobs[2].ShardIndex != 2 {
		t.Fatalf("third job shard index = %d, want 2", plan.Jobs[2].ShardIndex)
	}
	orchestrator := openTestOrchestrator(t)
	if err := orchestrator.InitCampaign(plan); err != nil {
		t.Fatal(err)
	}
	var stored int
	if err := orchestrator.db.QueryRow(`SELECT shard_index FROM jobs WHERE campaign_id = ? AND id = ?`, plan.CampaignID, plan.Jobs[2].ID).Scan(&stored); err != nil || stored != 2 {
		t.Fatalf("stored shard index = %d, err = %v", stored, err)
	}
}

func TestOrchestratorRejectsZeroShardsForEmptyScope(t *testing.T) {
	root := t.TempDir()
	scope := filepath.Join(root, "scope.json")
	writeJSONValue(t, scope, SurfaceOracleScope{
		SchemaVersion: 1, Kind: "all-runtime",
		SourceProfileSHA256: strings.Repeat("a", 64), LedgerSHA256: strings.Repeat("b", 64), PolicySHA256: strings.Repeat("c", 64),
		ByDisposition: map[string]int{deterministicMockRequired: 0, localRuntimeRequired: 0},
	})
	if _, err := PlanOrchestratorCampaign(testOrchestratorDefinition(t, scope, nil)); err == nil || !strings.Contains(err.Error(), "shard") {
		t.Fatalf("zero-shard campaign error = %v", err)
	}
}

func TestOrchestratorPlanningDoesNotMutateCallerShards(t *testing.T) {
	root := t.TempDir()
	scope, _ := writeSurfaceOracleIndexInputs(t, root)
	definition := testOrchestratorDefinition(t, scope, [][]string{{"apex:System.Two", "apex:System.One"}, {"apex:System.Three"}})
	want := [][]string{{"apex:System.Two", "apex:System.One"}, {"apex:System.Three"}}
	if _, err := PlanOrchestratorCampaign(definition); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(definition.Shards, want) {
		t.Fatalf("caller shards mutated = %#v, want %#v", definition.Shards, want)
	}
}

func TestOrchestratorLeasesAtLeastOnceAndHeartbeatsTransactionally(t *testing.T) {
	orchestrator, plan := initializedTestOrchestrator(t)
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	first, err := orchestrator.Lease(plan.CampaignID, "worker-a", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if first.Generation != 1 || first.Worker != "worker-a" {
		t.Fatalf("first lease = %#v", first)
	}
	if err := orchestrator.Heartbeat(first, now.Add(30*time.Second), time.Minute); err != nil {
		t.Fatal(err)
	}
	second, err := orchestrator.Lease(plan.CampaignID, "worker-b", now.Add(2*time.Minute), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if second.JobID != first.JobID || second.Generation != 2 {
		t.Fatalf("expired lease = %#v, want retry of %#v", second, first)
	}
	if err := orchestrator.Heartbeat(first, now.Add(2*time.Minute), time.Minute); err == nil {
		t.Fatal("stale generation heartbeat accepted")
	}
	var attempts int
	if err := orchestrator.db.QueryRow(`SELECT count(*) FROM attempts WHERE campaign_id = ? AND job_id = ?`, plan.CampaignID, first.JobID).Scan(&attempts); err != nil || attempts != 2 {
		t.Fatalf("attempts = %d, err = %v", attempts, err)
	}
	status, err := orchestrator.Status(plan.CampaignID)
	if err != nil || status.Retryable != 1 || status.Failed != 0 || status.Unseen != 3 {
		t.Fatalf("retry status = %#v, %v", status, err)
	}
}

func TestOrchestratorReservesGlobalHubSlotAndClosesClaimedCleanup(t *testing.T) {
	orchestrator, plan := initializedTestOrchestrator(t)
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	first, err := orchestrator.Lease(plan.CampaignID, "worker-a", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.SetHubCapacity("hub-a", 1); err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.SetHubCapacity("hub-a", 1); err != nil {
		t.Fatalf("identical capacity is not idempotent: %v", err)
	}
	if err := orchestrator.SetHubCapacity("hub-a", 2); err == nil {
		t.Fatal("caller reset existing hub capacity")
	}
	if err := orchestrator.Reserve(first, "hub-a", "scratch-global-1", now); err != nil {
		t.Fatal(err)
	}
	second, err := orchestrator.Lease(plan.CampaignID, "worker-b", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.Reserve(second, "hub-a", "scratch-global-1", now); err == nil {
		t.Fatal("duplicate global allocation alias accepted")
	}
	if err := orchestrator.Reserve(second, "hub-a", "scratch-global-2", now); err == nil || !strings.Contains(err.Error(), "capacity") {
		t.Fatalf("capacity error = %v", err)
	}
	if _, err := orchestrator.ClaimCleanup(plan.CampaignID, "cleaner", now, time.Minute); err == nil {
		t.Fatal("cross-worker cleanup claimed a live attempt")
	}
	claim, err := orchestrator.ClaimCleanup(plan.CampaignID, "worker-a", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if claim.AllocationAlias != "scratch-global-1" {
		t.Fatalf("claim = %#v", claim)
	}
	claim.HubAlias = "caller-controlled-hub"
	if err := orchestrator.CloseCleanup(claim, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.Reserve(second, "hub-a", "scratch-global-2", now.Add(2*time.Second)); err != nil {
		t.Fatalf("released capacity was not reusable: %v", err)
	}
}

func TestOrchestratorCleanupCloseRejectsReclaimedStaleToken(t *testing.T) {
	orchestrator, plan := initializedTestOrchestrator(t)
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	lease, err := orchestrator.Lease(plan.CampaignID, "worker-a", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.SetHubCapacity("hub-a", 1); err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.Reserve(lease, "hub-a", "scratch-reclaimed", now); err != nil {
		t.Fatal(err)
	}
	stale, err := orchestrator.ClaimCleanup(plan.CampaignID, "worker-a", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	current, err := orchestrator.ClaimCleanup(plan.CampaignID, "cleaner", now.Add(2*time.Minute), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.CloseCleanup(stale, now.Add(2*time.Minute+time.Second)); err == nil {
		t.Fatal("stale cleanup claim token closed a reclaimed journal")
	}
	if err := orchestrator.CloseCleanup(current, now.Add(2*time.Minute+time.Second)); err != nil {
		t.Fatal(err)
	}
}

func TestOrchestratorRecordsOnlyValidatedCleanupClosedCredit(t *testing.T) {
	root := t.TempDir()
	scope, batch := writeSurfaceOracleIndexInputs(t, root)
	definition := testOrchestratorDefinition(t, scope, [][]string{{"apex:System.One", "apex:System.Two"}, {"apex:System.Three"}})
	definition.Candidate = OrchestratorArtifact{Commit: strings.Repeat("1", 40), SHA256: surfaceOracleFileSHA256(t, filepath.Join(batch, "bin", "glade-sealed"))}
	definition.Tools = OrchestratorArtifact{Commit: strings.Repeat("2", 40), SHA256: surfaceOracleFileSHA256(t, filepath.Join(batch, "bin", "glade-tools"))}
	plan, err := PlanOrchestratorCampaign(definition)
	if err != nil {
		t.Fatal(err)
	}
	databasePath := filepath.Join(root, "orchestrator.db")
	orchestrator, err := OpenOrchestrator(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = orchestrator.Close() })
	if err := orchestrator.InitCampaign(plan); err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.Enqueue(plan); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	lease, err := orchestrator.Lease(plan.CampaignID, "worker-a", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.SetHubCapacity("hub-a", 1); err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.Reserve(lease, "hub-a", "scratch-credit", now); err != nil {
		t.Fatal(err)
	}
	request := OrchestratorReceiptRequest{Lease: lease, BatchRoot: batch}
	if _, err := orchestrator.RecordReceipt(request, now); err == nil || !strings.Contains(err.Error(), "cleanup") {
		t.Fatalf("open cleanup credit error = %v", err)
	}
	claim, err := orchestrator.ClaimCleanup(plan.CampaignID, "worker-a", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.CloseCleanup(claim, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := orchestrator.RecordReceipt(request, now.Add(2*time.Second)); err == nil || !strings.Contains(err.Error(), "binding") {
		t.Fatalf("unbound batch credit error = %v", err)
	}
	bindingPath := filepath.Join(batch, "evidence", "ORCHESTRATOR_BINDING.json")
	if _, err := WriteOrchestratorBatchBinding(bindingPath, plan, lease); err != nil {
		t.Fatal(err)
	}
	peer, err := OpenOrchestrator(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = peer.Close() })
	type result struct {
		receipt OrchestratorReceipt
		err     error
	}
	start, results := make(chan struct{}), make(chan result, 2)
	for _, instance := range []*Orchestrator{orchestrator, peer} {
		go func(instance *Orchestrator) {
			<-start
			receipt, err := instance.RecordReceipt(request, now.Add(2*time.Second))
			results <- result{receipt, err}
		}(instance)
	}
	close(start)
	first, second := <-results, <-results
	if first.err != nil || second.err != nil || first.receipt != second.receipt {
		t.Fatalf("concurrent receipt replay = %#v / %#v", first, second)
	}
	receipt := first.receipt
	if receipt.AcceptedCredit != 2 || !sha256Pattern.MatchString(receipt.BindingSHA256) {
		t.Fatalf("receipt = %#v", receipt)
	}
	replayed, err := orchestrator.RecordReceipt(request, now.Add(3*time.Second))
	if err != nil || replayed != receipt {
		t.Fatalf("identical receipt replay = %#v, %v", replayed, err)
	}
	status, err := orchestrator.Status(plan.CampaignID)
	if err != nil || status.Accepted != 2 || status.Rejected != 0 || status.Unseen != 1 {
		t.Fatalf("credit status = %#v, %v", status, err)
	}
}

func TestOrchestratorRejectsPartialShardCredit(t *testing.T) {
	root := t.TempDir()
	scope, _ := writeSurfaceOracleIndexInputs(t, root)
	batch := writeSurfaceOracleBatch(t, root, "partial", []string{"apex:System.One"})
	definition := testOrchestratorDefinition(t, scope, [][]string{{"apex:System.One", "apex:System.Two"}, {"apex:System.Three"}})
	definition.Candidate = OrchestratorArtifact{Commit: strings.Repeat("1", 40), SHA256: surfaceOracleFileSHA256(t, filepath.Join(batch, "bin", "glade-sealed"))}
	definition.Tools = OrchestratorArtifact{Commit: strings.Repeat("2", 40), SHA256: surfaceOracleFileSHA256(t, filepath.Join(batch, "bin", "glade-tools"))}
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
	if err := orchestrator.SetHubCapacity("hub-a", 1); err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.Reserve(lease, "hub-a", "scratch-partial", now); err != nil {
		t.Fatal(err)
	}
	claim, err := orchestrator.ClaimCleanup(plan.CampaignID, "worker-a", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.CloseCleanup(claim, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteOrchestratorBatchBinding(filepath.Join(batch, "evidence", "ORCHESTRATOR_BINDING.json"), plan, lease); err != nil {
		t.Fatal(err)
	}
	if _, err := orchestrator.RecordReceipt(OrchestratorReceiptRequest{Lease: lease, BatchRoot: batch}, now.Add(2*time.Second)); err == nil || !strings.Contains(err.Error(), "exact shard") {
		t.Fatalf("partial shard credit error = %v", err)
	}
}

func TestOrchestratorRecordsConfirmedMismatchAsRejected(t *testing.T) {
	root := t.TempDir()
	scope, _ := writeSurfaceOracleIndexInputs(t, root)
	batch := writeSurfaceOracleBatch(t, root, "mismatch", []string{"apex:System.One"})
	setSurfaceOracleBatchAdjudication(t, batch, "apex:System.One", "mismatch")
	setSurfaceOracleBatchFixtureKind(t, batch, "test")
	definition := testOrchestratorDefinition(t, scope, [][]string{{"apex:System.One"}, {"apex:System.Two", "apex:System.Three"}})
	definition.Candidate = OrchestratorArtifact{Commit: strings.Repeat("1", 40), SHA256: surfaceOracleFileSHA256(t, filepath.Join(batch, "bin", "glade-sealed"))}
	definition.Tools = OrchestratorArtifact{Commit: strings.Repeat("2", 40), SHA256: surfaceOracleFileSHA256(t, filepath.Join(batch, "bin", "glade-tools"))}
	orchestrator, plan, lease, now := readyOrchestratorReceipt(t, definition, batch)
	receipt, err := orchestrator.RecordReceipt(OrchestratorReceiptRequest{Lease: lease, BatchRoot: batch}, now)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.AcceptedCredit != 0 || receipt.RejectedCredit != 1 {
		t.Fatalf("mismatch receipt = %#v", receipt)
	}
	status, err := orchestrator.Status(plan.CampaignID)
	if err != nil || status.Accepted != 0 || status.Rejected != 1 || status.Unseen != 2 {
		t.Fatalf("mismatch status = %#v, %v", status, err)
	}
}

func TestOrchestratorRecoveredCleanupCannotRecordReceipt(t *testing.T) {
	root := t.TempDir()
	scope, _ := writeSurfaceOracleIndexInputs(t, root)
	batch := writeSurfaceOracleBatch(t, root, "recovered", []string{"apex:System.One"})
	definition := testOrchestratorDefinition(t, scope, [][]string{{"apex:System.One"}, {"apex:System.Two", "apex:System.Three"}})
	definition.Candidate = OrchestratorArtifact{Commit: strings.Repeat("1", 40), SHA256: surfaceOracleFileSHA256(t, filepath.Join(batch, "bin", "glade-sealed"))}
	definition.Tools = OrchestratorArtifact{Commit: strings.Repeat("2", 40), SHA256: surfaceOracleFileSHA256(t, filepath.Join(batch, "bin", "glade-tools"))}
	orchestrator, plan, lease, now := readyOrchestratorReceiptWithoutCleanup(t, definition, batch)
	claim, err := orchestrator.ClaimCleanup(plan.CampaignID, "worker-a", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.closeCleanup(claim, now.Add(time.Second), false); err != nil {
		t.Fatal(err)
	}
	if _, err := orchestrator.RecordReceipt(OrchestratorReceiptRequest{Lease: lease, BatchRoot: batch}, now.Add(2*time.Second)); err == nil || !strings.Contains(err.Error(), "proof-ineligible") {
		t.Fatalf("recovered cleanup recorded receipt: %v", err)
	}
}

func readyOrchestratorReceiptWithoutCleanup(t *testing.T, definition OrchestratorCampaignDefinition, batch string) (*Orchestrator, OrchestratorCampaignPlan, OrchestratorLease, time.Time) {
	t.Helper()
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
	if err := orchestrator.SetHubCapacity("hub-a", 1); err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.Reserve(lease, "hub-a", "scratch-"+filepath.Base(batch), now); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteOrchestratorBatchBinding(filepath.Join(batch, "evidence", "ORCHESTRATOR_BINDING.json"), plan, lease); err != nil {
		t.Fatal(err)
	}
	return orchestrator, plan, lease, now.Add(2 * time.Second)
}

func TestOrchestratorLeavesInconclusiveBatchUnseen(t *testing.T) {
	root := t.TempDir()
	scope, _ := writeSurfaceOracleIndexInputs(t, root)
	batch := writeSurfaceOracleBatch(t, root, "inconclusive", []string{"apex:System.One", "apex:System.Two"})
	setSurfaceOracleBatchAdjudication(t, batch, "apex:System.Two", "environment")
	setSurfaceOracleBatchAdjudication(t, batch, "apex:System.One", "environment")
	definition := testOrchestratorDefinition(t, scope, [][]string{{"apex:System.One", "apex:System.Two"}, {"apex:System.Three"}})
	definition.Candidate = OrchestratorArtifact{Commit: strings.Repeat("1", 40), SHA256: surfaceOracleFileSHA256(t, filepath.Join(batch, "bin", "glade-sealed"))}
	definition.Tools = OrchestratorArtifact{Commit: strings.Repeat("2", 40), SHA256: surfaceOracleFileSHA256(t, filepath.Join(batch, "bin", "glade-tools"))}
	orchestrator, plan, lease, now := readyOrchestratorReceipt(t, definition, batch)
	if _, err := orchestrator.RecordReceipt(OrchestratorReceiptRequest{Lease: lease, BatchRoot: batch}, now); err == nil || !strings.Contains(err.Error(), "inconclusive") {
		t.Fatalf("inconclusive receipt error = %v", err)
	}
	status, err := orchestrator.Status(plan.CampaignID)
	if err != nil || status.Accepted != 0 || status.Rejected != 0 || status.Unseen != 3 {
		t.Fatalf("inconclusive status = %#v, %v", status, err)
	}
}

func TestOrchestratorRefusesOperationalFailureAsRejected(t *testing.T) {
	root := t.TempDir()
	scope, _ := writeSurfaceOracleIndexInputs(t, root)
	batch := writeSurfaceOracleBatch(t, root, "forged-mismatch", []string{"apex:System.One"})
	setSurfaceOracleBatchAdjudication(t, batch, "apex:System.One", "mismatch")
	updateJSONMap(t, filepath.Join(batch, "oracle", "results.json"), func(value map[string]any) {
		result := value["results"].([]any)[0].(map[string]any)
		result["kind"], result["status"], result["deployable"], result["exitCode"] = "exec", "Failed", false, 1
		result["runtimePassed"], result["runtimeStatus"], result["runtimeExitCode"] = false, "Failed", nil
		result["componentFailures"] = []any{map[string]any{"problem": "transport failed"}}
	})
	resealSurfaceOracleBatch(t, batch)
	definition := testOrchestratorDefinition(t, scope, [][]string{{"apex:System.One"}, {"apex:System.Two", "apex:System.Three"}})
	definition.Candidate = OrchestratorArtifact{Commit: strings.Repeat("1", 40), SHA256: surfaceOracleFileSHA256(t, filepath.Join(batch, "bin", "glade-sealed"))}
	definition.Tools = OrchestratorArtifact{Commit: strings.Repeat("2", 40), SHA256: surfaceOracleFileSHA256(t, filepath.Join(batch, "bin", "glade-tools"))}
	orchestrator, plan, lease, now := readyOrchestratorReceipt(t, definition, batch)
	if _, err := orchestrator.RecordReceipt(OrchestratorReceiptRequest{Lease: lease, BatchRoot: batch}, now); err == nil {
		t.Fatal("operational exec failure was relabeled as rejected credit")
	}
	status, err := orchestrator.Status(plan.CampaignID)
	if err != nil || status.Rejected != 0 || status.Unseen != 3 {
		t.Fatalf("forged mismatch status = %#v, %v", status, err)
	}
}

func TestOrchestratorRefusesOracleOnlyKindFlip(t *testing.T) {
	root := t.TempDir()
	scope, _ := writeSurfaceOracleIndexInputs(t, root)
	batch := writeSurfaceOracleBatch(t, root, "kind-flip", []string{"apex:System.One"})
	setSurfaceOracleBatchAdjudication(t, batch, "apex:System.One", "mismatch")
	definition := testOrchestratorDefinition(t, scope, [][]string{{"apex:System.One"}, {"apex:System.Two", "apex:System.Three"}})
	definition.Candidate = OrchestratorArtifact{Commit: strings.Repeat("1", 40), SHA256: surfaceOracleFileSHA256(t, filepath.Join(batch, "bin", "glade-sealed"))}
	definition.Tools = OrchestratorArtifact{Commit: strings.Repeat("2", 40), SHA256: surfaceOracleFileSHA256(t, filepath.Join(batch, "bin", "glade-tools"))}
	orchestrator, plan, lease, now := readyOrchestratorReceipt(t, definition, batch)
	if _, err := orchestrator.RecordReceipt(OrchestratorReceiptRequest{Lease: lease, BatchRoot: batch}, now); err == nil {
		t.Fatal("oracle-only fixture kind flip earned rejected credit")
	}
	status, err := orchestrator.Status(plan.CampaignID)
	if err != nil || status.Rejected != 0 || status.Unseen != 3 {
		t.Fatalf("kind-flip status = %#v, %v", status, err)
	}
}

func TestOrchestratorStatusRejectsHistoricalCreditBlendedIntoCurrentCandidate(t *testing.T) {
	currentCandidate := strings.Repeat("a", 40)
	historical := OrchestratorStatusCredit{Candidate: "9452", Credit: 203, Total: 8216}
	valid := OrchestratorStatusSnapshot{
		Current:           OrchestratorStatusCredit{Candidate: currentCandidate, Credit: 0, Total: 8216},
		Historical:        historical,
		Accounted:         8216,
		DirectLocal:       8177,
		TerminalLocalOnly: 39,
	}
	if err := ValidateOrchestratorStatus(valid, currentCandidate, 0, historical); err != nil {
		t.Fatal(err)
	}
	blended := valid
	blended.Current.Credit = 203
	blended.Historical = OrchestratorStatusCredit{}
	if err := ValidateOrchestratorStatus(blended, currentCandidate, 0, historical); err == nil || !strings.Contains(err.Error(), "current") {
		t.Fatalf("blended status error = %v", err)
	}
	if err := ValidateOrchestratorStatus(valid, strings.Repeat("b", 40), 0, historical); err == nil || !strings.Contains(err.Error(), "candidate") {
		t.Fatalf("changed candidate status error = %v", err)
	}
	for name, mutate := range map[string]func(*OrchestratorStatusSnapshot){
		"omitted":         func(status *OrchestratorStatusSnapshot) { status.Historical = OrchestratorStatusCredit{} },
		"wrong candidate": func(status *OrchestratorStatusSnapshot) { status.Historical.Candidate = "other" },
		"same candidate":  func(status *OrchestratorStatusSnapshot) { status.Historical.Candidate = currentCandidate },
		"wrong credit":    func(status *OrchestratorStatusSnapshot) { status.Historical.Credit-- },
		"wrong total":     func(status *OrchestratorStatusSnapshot) { status.Historical.Total-- },
	} {
		t.Run(name+" historical binding", func(t *testing.T) {
			status := valid
			mutate(&status)
			if err := ValidateOrchestratorStatus(status, currentCandidate, 0, historical); err == nil || !strings.Contains(err.Error(), "historical") {
				t.Fatalf("historical contradiction error = %v", err)
			}
		})
	}
	later := valid
	later.Current.Credit = 203
	if err := ValidateOrchestratorStatus(later, currentCandidate, 203, historical); err != nil {
		t.Fatalf("separately bound equal current and historical credit: %v", err)
	}
}

func TestOrchestratorRejectsReservationAfterLeaseExpiry(t *testing.T) {
	orchestrator, plan := initializedTestOrchestrator(t)
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	lease, err := orchestrator.Lease(plan.CampaignID, "worker-a", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.SetHubCapacity("hub-a", 1); err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.Reserve(lease, "hub-a", "scratch-expired", now.Add(time.Minute)); err == nil || !strings.Contains(err.Error(), "current") {
		t.Fatalf("expired lease reservation error = %v", err)
	}
}

func openTestOrchestrator(t *testing.T) *Orchestrator {
	t.Helper()
	orchestrator, err := OpenOrchestrator(filepath.Join(t.TempDir(), "orchestrator.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = orchestrator.Close() })
	return orchestrator
}

func initializedTestOrchestrator(t *testing.T) (*Orchestrator, OrchestratorCampaignPlan) {
	t.Helper()
	root := t.TempDir()
	scope, _ := writeSurfaceOracleIndexInputs(t, root)
	plan, err := PlanOrchestratorCampaign(testOrchestratorDefinition(t, scope, [][]string{{"apex:System.One", "apex:System.Two"}, {"apex:System.Three"}}))
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
	return orchestrator, plan
}

func testOrchestratorDefinition(t *testing.T, scope string, shards [][]string) OrchestratorCampaignDefinition {
	t.Helper()
	return OrchestratorCampaignDefinition{
		Candidate: OrchestratorArtifact{Commit: strings.Repeat("a", 40), SHA256: strings.Repeat("b", 64)},
		Tools:     OrchestratorArtifact{Commit: strings.Repeat("c", 40), SHA256: strings.Repeat("d", 64)},
		ScopePath: scope, ScopeSHA256: surfaceOracleFileSHA256(t, scope),
		ControlledInputSHA256: map[string]string{"oracle-plan": strings.Repeat("e", 64)},
		Shards:                shards,
	}
}

func readyOrchestratorReceipt(t *testing.T, definition OrchestratorCampaignDefinition, batch string) (*Orchestrator, OrchestratorCampaignPlan, OrchestratorLease, time.Time) {
	t.Helper()
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
	if err := orchestrator.SetHubCapacity("hub-a", 1); err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.Reserve(lease, "hub-a", "scratch-"+filepath.Base(batch), now); err != nil {
		t.Fatal(err)
	}
	claim, err := orchestrator.ClaimCleanup(plan.CampaignID, "worker-a", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.CloseCleanup(claim, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteOrchestratorBatchBinding(filepath.Join(batch, "evidence", "ORCHESTRATOR_BINDING.json"), plan, lease); err != nil {
		t.Fatal(err)
	}
	return orchestrator, plan, lease, now.Add(2 * time.Second)
}

func setSurfaceOracleBatchAdjudication(t *testing.T, batch, surfaceID, classification string) {
	t.Helper()
	reconciliation := filepath.Join(batch, "evidence", "RECONCILIATION.json")
	review := filepath.Join(batch, "evidence", "MISMATCH_REVIEW.json")
	oracle := filepath.Join(batch, "oracle", "results.json")
	updateJSONMap(t, oracle, func(value map[string]any) {
		result := value["results"].([]any)[0].(map[string]any)
		result["runtimePassed"] = false
		if classification == "mismatch" {
			result["kind"], result["runtimeStatus"], result["runtimeExitCode"] = "test", "Failed", 1
		} else {
			result["status"], result["deployable"], result["exitCode"], result["runtimeStatus"] = "Failed", false, 1, "Failed"
			result["componentFailures"] = []any{map[string]any{"problem": "transport failed"}}
		}
	})
	updateJSONMap(t, reconciliation, func(value map[string]any) {
		counts := value["counts"].(map[string]any)
		counts["match"] = counts["match"].(float64) - 1
		counts[classification] = counts[classification].(float64) + 1
		for _, item := range value["rows"].([]any) {
			row := item.(map[string]any)
			if row["surfaceId"] != surfaceID {
				continue
			}
			row["classification"] = classification
			if classification == "mismatch" {
				row["reason"] = "salesforce-runtime-assertion-differed"
				salesforce := row["salesforce"].(map[string]any)
				salesforce["runtimePassed"], salesforce["runtimeStatus"], salesforce["runtimeExitCode"] = false, "Failed", 1
			} else {
				row["reason"] = "salesforce-runtime-infrastructure-failed"
				salesforce := row["salesforce"].(map[string]any)
				salesforce["status"], salesforce["deployable"], salesforce["exitCode"], salesforce["runtimePassed"], salesforce["runtimeStatus"] = "Failed", false, 1, false, "Failed"
				salesforce["componentFailures"] = []any{map[string]any{"problem": "transport failed"}}
			}
		}
	})
	updateJSONMap(t, review, func(value map[string]any) {
		rawCounts := value["rawClassifications"].(map[string]any)
		rawCounts["match"] = rawCounts["match"].(float64) - 1
		rawCounts[classification] = rawCounts[classification].(float64) + 1
		counts := value["reviewCounts"].(map[string]any)
		counts["confirmedMatch"] = counts["confirmedMatch"].(float64) - 1
		disposition := "inconclusive"
		if classification == "mismatch" {
			disposition = "confirmed-mismatch"
			counts["confirmedMismatch"] = jsonNumber(counts["confirmedMismatch"]) + 1
		} else {
			counts["inconclusive"] = jsonNumber(counts["inconclusive"]) + 1
		}
		group := value["groups"].([]any)[0].(map[string]any)
		group["confirmedMatchRows"] = group["confirmedMatchRows"].(float64) - 1
		if classification == "mismatch" {
			group["confirmedMismatchRows"] = jsonNumber(group["confirmedMismatchRows"]) + 1
		} else {
			group["inconclusiveRows"] = jsonNumber(group["inconclusiveRows"]) + 1
		}
		for _, item := range value["rows"].([]any) {
			row := item.(map[string]any)
			if row["surfaceId"] == surfaceID {
				row["reviewDisposition"], row["sealedClassification"] = disposition, classification
			}
		}
	})
	resealSurfaceOracleBatch(t, batch)
}

func jsonNumber(value any) float64 {
	number, _ := value.(float64)
	return number
}

func setSurfaceOracleBatchFixtureKind(t *testing.T, batch, kind string) {
	t.Helper()
	fixture := filepath.Join(batch, "source", "glade-tools", "docs", "fixtures", "fixture-one.json")
	updateJSONMap(t, fixture, func(value map[string]any) { value["command"] = map[string]any{"kind": kind} })
	updateJSONMap(t, filepath.Join(batch, "inputs", "RUNTIME_BATCH_MANIFEST.json"), func(value map[string]any) {
		value["fixtures"].([]any)[0].(map[string]any)["sha256"] = surfaceOracleFileSHA256(t, fixture)
	})
	updateJSONMap(t, filepath.Join(batch, "local-proof", "LOCAL_RUNTIME_SUMMARY.json"), func(value map[string]any) {
		value["results"].([]any)[0].(map[string]any)["kind"] = kind
	})
	resealSurfaceOracleBatch(t, batch)
}
