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

func TestAcceptOrchestratorRawCanaryClosesCleanupWithoutProofCredit(t *testing.T) {
	fixture := withOraclePlanCampaignScope(t, newOrchestratorSalesforceReconciliationFixture(t))
	retained := t.TempDir()
	receiptPath, packetPath := filepath.Join(retained, "receipt.json"), filepath.Join(retained, "packet")
	if _, err := CreateOrchestratorSalesforceReconciliation(OrchestratorSalesforceReconciliationRequest{Plan: fixture.plan, Lease: fixture.lease, OraclePlanPath: fixture.oraclePlanPath, BindingPath: fixture.bindingPath, ShardFiles: fixture.files, PacketOutput: packetPath, OutputPath: receiptPath}); err != nil {
		t.Fatal(err)
	}
	if err := VerifyOrchestratorSalesforceReconciliation(fixture.plan, fixture.lease, receiptPath, packetPath); err != nil {
		t.Fatal(err)
	}
	orchestrator := openTestOrchestrator(t)
	if err := orchestrator.InitCampaign(fixture.plan); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	lease, err := orchestrator.Lease(fixture.plan.CampaignID, fixture.lease.Worker, now, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.SetHubCapacity("sealed-dev-hub", 1); err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.Reserve(lease, "sealed-dev-hub", "scratch-canary", now); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	planPath, leasePath := filepath.Join(retained, "plan.json"), filepath.Join(retained, "lease.json")
	writeRawAcceptJSON(t, planPath, fixture.plan)
	prettyLease, err := json.MarshalIndent(lease, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(leasePath, append(prettyLease, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	planBytes, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	leaseBytes, err := os.ReadFile(leasePath)
	if err != nil {
		t.Fatal(err)
	}
	reconciliation, _, err := readExactJSONBytes[SalesforceReconciliation](receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(reconciliation.Rows) != 1 || reconciliation.Rows[0].SurfaceID != "apex:Runtime.run" || reconciliation.Rows[0].Action != oracleRuntime {
		t.Fatalf("Oracle-plan reconciliation rows = %#v", reconciliation.Rows)
	}
	sshReceipt := OrchestratorSSHDispatchReceipt{SchemaVersion: 1, CampaignID: lease.CampaignID, JobID: lease.JobID, ShardIndex: lease.ShardIndex, Generation: lease.Generation, Status: "worker-complete", Passed: true, ExitCode: 0, SpecSHA256: fixture.plan.SpecSHA256, PlanSHA256: replayBytesSHA256(planBytes), LeaseSHA256: replayBytesSHA256(leaseBytes), CommandSHA256: strings.Repeat("1", 64), StdoutSHA256: strings.Repeat("2", 64), StderrSHA256: strings.Repeat("3", 64), OrchestratorBindingSHA256: reconciliation.OrchestratorBindingSHA256, SalesforceShardSHA256: reconciliation.Shards[0].InputSHA256["shard"], OrgCleanupSHA256: reconciliation.Shards[0].InputSHA256["cleanup"]}
	sshPath := filepath.Join(retained, "ssh-receipt.json")
	writeRawAcceptJSON(t, sshPath, sshReceipt)
	sshBytes, err := os.ReadFile(sshPath)
	if err != nil {
		t.Fatal(err)
	}
	acceptRequest := OrchestratorRawCanaryRequest{Coordinator: orchestrator, Plan: fixture.plan, Lease: lease, PlanSHA256: replayBytesSHA256(planBytes), LeaseSHA256: replayBytesSHA256(leaseBytes), SSHReceiptSHA256: replayBytesSHA256(sshBytes), SSHReceipt: sshReceipt, ReceiptPath: receiptPath, PacketPath: packetPath, AllocationAlias: "scratch-canary", OutputPath: filepath.Join(retained, "missing", "canary.json")}
	wrongSSH := acceptRequest
	wrongSSH.SSHReceiptSHA256 = strings.Repeat("f", 64)
	if _, err := AcceptOrchestratorRawCanary(wrongSSH); err == nil || !strings.Contains(err.Error(), "receipt bytes") {
		t.Fatalf("wrong SSH receipt hash error = %v", err)
	}
	if _, err := AcceptOrchestratorRawCanary(acceptRequest); err == nil || !strings.Contains(err.Error(), "preflight") {
		t.Fatalf("invalid output parent error = %v", err)
	}
	var pending string
	if err := orchestrator.db.QueryRow(`SELECT state FROM cleanup_journal WHERE allocation_alias = ?`, "scratch-canary").Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if pending != "pending" {
		t.Fatalf("cleanup after output preflight failure = %q", pending)
	}
	outputPath := filepath.Join(retained, "canary.json")
	acceptRequest.OutputPath = outputPath
	got, err := AcceptOrchestratorRawCanary(acceptRequest)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "validated-zero-credit" || got.ProofCredit != 0 || !got.CleanupClosed || got.CampaignID != lease.CampaignID || got.OrgCleanupSHA256 != sshReceipt.OrgCleanupSHA256 || got.SSHReceiptSHA256 != replayBytesSHA256(sshBytes) || got.ReconciliationSHA256 == "" || got.PacketManifestSHA256 == "" {
		t.Fatalf("canary receipt = %#v", got)
	}
	replayed, err := AcceptOrchestratorRawCanary(acceptRequest)
	if err != nil || replayed != got {
		t.Fatalf("exact replay = %#v, err=%v", replayed, err)
	}
	outputBytes, err := os.ReadFile(outputPath)
	if err != nil || bytes.Contains(outputBytes, []byte(retained)) || bytes.Contains(outputBytes, []byte("scratch-canary")) {
		t.Fatalf("canary receipt leaked private data: %s", outputBytes)
	}
	for _, privateKey := range []string{`"host"`, `"username"`, `"orgId"`, `"path"`, `"command"`, `"output"`, `"allocation"`} {
		if bytes.Contains(outputBytes, []byte(privateKey)) {
			t.Fatalf("canary receipt contains private key %s: %s", privateKey, outputBytes)
		}
	}
	var cleanupState, jobState, attemptState string
	if err := orchestrator.db.QueryRow(`SELECT c.state, j.status, a.status FROM cleanup_journal c JOIN jobs j ON j.campaign_id = c.campaign_id AND j.id = c.job_id AND j.generation = c.generation JOIN attempts a ON a.campaign_id = c.campaign_id AND a.job_id = c.job_id AND a.generation = c.generation WHERE c.allocation_alias = ?`, "scratch-canary").Scan(&cleanupState, &jobState, &attemptState); err != nil {
		t.Fatal(err)
	}
	if cleanupState != "closed" || jobState != "running" || attemptState != "running" {
		t.Fatalf("states = %q/%q/%q", cleanupState, jobState, attemptState)
	}
	var reserved int
	if err := orchestrator.db.QueryRow(`SELECT reserved FROM hub_capacity WHERE hub_alias = ?`, "sealed-dev-hub").Scan(&reserved); err != nil || reserved != 0 {
		t.Fatalf("hub reserved = %d, err=%v", reserved, err)
	}
	var credits int
	if err := orchestrator.db.QueryRow(`SELECT count(*) FROM proof_credits WHERE campaign_id = ?`, lease.CampaignID).Scan(&credits); err != nil || credits != 0 {
		t.Fatalf("proof credits = %d, err=%v", credits, err)
	}
	var receipts int
	if err := orchestrator.db.QueryRow(`SELECT count(*) FROM receipts WHERE campaign_id = ?`, lease.CampaignID).Scan(&receipts); err != nil || receipts != 0 {
		t.Fatalf("orchestrator receipts = %d, err=%v", receipts, err)
	}
	replayed, replayErr := AcceptOrchestratorRawCanary(acceptRequest)
	if replayErr != nil || replayed != got {
		t.Fatalf("exact replay = %#v, %v", replayed, replayErr)
	}
	if err := os.WriteFile(outputPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := AcceptOrchestratorRawCanary(acceptRequest); err == nil || !strings.Contains(err.Error(), "existing raw canary output") {
		t.Fatalf("tampered replay error = %v", err)
	}
}

func writeRawAcceptJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}
