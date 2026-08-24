package corpusassurance

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"time"
)

const (
	orchestratorWorkerReserveFailed        = "orchestrator-worker-reserve-failed"
	orchestratorWorkerWrapperFailed        = "orchestrator-worker-wrapper-failed"
	orchestratorWorkerCleanupFailed        = "orchestrator-worker-cleanup-failed"
	orchestratorWorkerTransferFailed       = "orchestrator-worker-transfer-failed"
	orchestratorWorkerReconciliationFailed = "orchestrator-worker-reconciliation-failed"
	orchestratorWorkerCreditFailed         = "orchestrator-worker-credit-failed"
)

type OrchestratorWorkerRequest struct {
	Plan            OrchestratorCampaignPlan
	Lease           OrchestratorLease
	HubAlias        string
	HubCapacity     int
	AllocationAlias string
	EvidenceRoot    string
	OraclePlanPath  string
}

type OrchestratorWorkerRunResult struct {
	BatchRoot  string                                   `json:"batchRoot,omitempty"`
	Production *BuildOrchestratorProductionBatchRequest `json:"-"`
}

type OrchestratorWorkerResult struct {
	BatchRoot      string              `json:"batchRoot"`
	ManifestSHA256 string              `json:"manifestSha256"`
	Receipt        OrchestratorReceipt `json:"receipt"`
}

type OrchestratorWorkerTransferRequest struct {
	Plan            OrchestratorCampaignPlan
	Lease           OrchestratorLease
	SourceBatchRoot string
	EvidenceRoot    string
	OraclePlanPath  string
}

type OrchestratorWorkerTransfer struct {
	BatchRoot      string `json:"batchRoot"`
	ManifestSHA256 string `json:"manifestSha256"`
}

// OrchestratorCleanupTakeoverRequest binds cleanup to the exact journal claim
// and the existing typed Salesforce cleanup inputs. It intentionally has no
// proof-credit fields: takeover can close cleanup only.
type OrchestratorCleanupTakeoverRequest struct {
	Claim         OrchestratorCleanupClaim       `json:"claim"`
	BundlePath    string                         `json:"bundlePath"`
	CreationPath  string                         `json:"creationPath"`
	PreflightPath string                         `json:"preflightPath"`
	TargetOrg     string                         `json:"targetOrg"`
	SFBin         string                         `json:"sfBin"`
	OutputPath    string                         `json:"outputPath"`
	SSH           *OrchestratorSSHCleanupBinding `json:"ssh,omitempty"`
}

type salesforceOrgCleanupRunner func(SalesforceOrgCleanupRequest) (SalesforceOrgCleanup, error)

// RunOrchestratorCleanupTakeover runs the real typed Salesforce cleanup
// before closing the coordinator journal. The callback is kept internal-test
// injectable; the public path always uses RunSalesforceOrgCleanup.
func RunOrchestratorCleanupTakeover(orchestrator *Orchestrator, request OrchestratorCleanupTakeoverRequest) error {
	return runOrchestratorCleanupTakeoverAt(orchestrator, request, func() time.Time { return time.Now().UTC() }, RunSalesforceOrgCleanup)
}

func runOrchestratorCleanupTakeover(orchestrator *Orchestrator, request OrchestratorCleanupTakeoverRequest, now time.Time, cleanup salesforceOrgCleanupRunner) error {
	return runOrchestratorCleanupTakeoverAt(orchestrator, request, func() time.Time { return now }, cleanup)
}

func runOrchestratorCleanupTakeoverAt(orchestrator *Orchestrator, request OrchestratorCleanupTakeoverRequest, clock func() time.Time, cleanup salesforceOrgCleanupRunner) error {
	if request.SSH != nil {
		if request.BundlePath != "" || request.CreationPath != "" || request.PreflightPath != "" || request.TargetOrg != "" || request.SFBin != "" {
			return fmt.Errorf("local and SSH cleanup modes are mutually exclusive")
		}
		_, err := runOrchestratorSSHCleanupTakeover(request.SSH.coordinatorRequest(orchestrator, request.Claim, request.OutputPath), orchestratorSSHCleanupTimeout)
		return err
	}
	if orchestrator == nil || cleanup == nil || request.Claim.CampaignID == "" || request.Claim.JobID == "" || request.Claim.Generation < 1 || request.Claim.AllocationAlias == "" || request.Claim.Worker == "" {
		return orchestratorWorkerError{orchestratorWorkerCleanupFailed}
	}
	for _, path := range []string{request.BundlePath, request.CreationPath, request.PreflightPath, request.OutputPath} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return orchestratorWorkerError{orchestratorWorkerCleanupFailed}
		}
	}
	if request.TargetOrg == "" || request.TargetOrg != request.Claim.AllocationAlias || request.SFBin == "" {
		return orchestratorWorkerError{orchestratorWorkerCleanupFailed}
	}
	if clock == nil || validateOrchestratorCleanupClaim(orchestrator, request.Claim, clock().UTC()) != nil {
		return orchestratorWorkerError{orchestratorWorkerCleanupFailed}
	}
	if info, err := os.Lstat(request.OutputPath); err == nil {
		if !info.Mode().IsRegular() {
			return orchestratorWorkerError{orchestratorWorkerCleanupFailed}
		}
		cleanupReceipt, err := readValidatedOrchestratorCleanup(request)
		if err != nil {
			return orchestratorWorkerError{orchestratorWorkerCleanupFailed}
		}
		if err := orchestrator.closeCleanup(request.Claim, clock().UTC(), !cleanupReceipt.RecoveredAbsent); err != nil {
			return orchestratorWorkerError{orchestratorWorkerCleanupFailed}
		}
		return nil
	} else if !os.IsNotExist(err) {
		return orchestratorWorkerError{orchestratorWorkerCleanupFailed}
	}
	cleanupRequest := SalesforceOrgCleanupRequest{BundlePath: request.BundlePath, CreationPath: request.CreationPath, PreflightPath: request.PreflightPath, TargetOrg: request.TargetOrg, DevHub: request.Claim.HubAlias, SFBin: request.SFBin, OutputPath: request.OutputPath}
	if _, err := cleanup(cleanupRequest); err != nil {
		return orchestratorWorkerError{orchestratorWorkerCleanupFailed}
	}
	cleanupReceipt, err := readValidatedOrchestratorCleanup(request)
	if err != nil {
		return orchestratorWorkerError{orchestratorWorkerCleanupFailed}
	}
	if err := orchestrator.closeCleanup(request.Claim, clock().UTC(), !cleanupReceipt.RecoveredAbsent); err != nil {
		return orchestratorWorkerError{orchestratorWorkerCleanupFailed}
	}
	return nil
}

func validateOrchestratorCleanupClaim(orchestrator *Orchestrator, claim OrchestratorCleanupClaim, now time.Time) error {
	var claimUntil int64
	var claimedBy, hubAlias string
	err := orchestrator.db.QueryRow(`SELECT c.claim_until, c.claimed_by, a.hub_alias FROM cleanup_journal c JOIN scratch_allocations a ON a.allocation_alias = c.allocation_alias WHERE c.campaign_id = ? AND c.job_id = ? AND c.generation = ? AND c.allocation_alias = ? AND c.state = 'running' AND a.campaign_id = c.campaign_id AND a.job_id = c.job_id AND a.generation = c.generation AND a.state = 'reserved'`, claim.CampaignID, claim.JobID, claim.Generation, claim.AllocationAlias).Scan(&claimUntil, &claimedBy, &hubAlias)
	if err != nil || claimUntil != claim.ClaimUntil.UTC().UnixMilli() || claimedBy != claim.Worker || hubAlias != claim.HubAlias || claimUntil <= now.UTC().UnixMilli() {
		return orchestratorWorkerError{orchestratorWorkerCleanupFailed}
	}
	return nil
}

func validateExistingOrchestratorCleanup(request OrchestratorCleanupTakeoverRequest) error {
	_, err := readValidatedOrchestratorCleanup(request)
	return err
}

func readValidatedOrchestratorCleanup(request OrchestratorCleanupTakeoverRequest) (SalesforceOrgCleanup, error) {
	bundleSHA, err := sha256File(request.BundlePath)
	if err != nil {
		return SalesforceOrgCleanup{}, orchestratorWorkerError{orchestratorWorkerCleanupFailed}
	}
	creation, _, err := readExactJSONBytes[SalesforceOrgCreation](request.CreationPath)
	if err != nil || !validSalesforceOrgCreation(creation, bundleSHA, request.BundlePath, request.Claim.HubAlias, request.TargetOrg) {
		return SalesforceOrgCleanup{}, orchestratorWorkerError{orchestratorWorkerCleanupFailed}
	}
	preflight, _, err := readExactJSONBytes[SalesforceOrgPreflight](request.PreflightPath)
	if err != nil || !validSalesforceOrgPreflight(preflight, bundleSHA, request.BundlePath) || preflight.OrgAlias != request.TargetOrg || preflight.OrgID != creation.OrgID {
		return SalesforceOrgCleanup{}, orchestratorWorkerError{orchestratorWorkerCleanupFailed}
	}
	snapshot, err := readRegularFileSnapshot(request.OutputPath)
	if err != nil || snapshot.Mode.Perm() != 0o600 {
		return SalesforceOrgCleanup{}, orchestratorWorkerError{orchestratorWorkerCleanupFailed}
	}
	var cleanup SalesforceOrgCleanup
	if err := decodeExactJSON(snapshot.Data, &cleanup); err != nil || cleanup.OrgAlias != request.TargetOrg || (!validSalesforceOrgCleanup(cleanup, bundleSHA, request.BundlePath, creation) && !validSalesforceRecoveredOrgCleanup(cleanup, bundleSHA, request.BundlePath, creation)) {
		return SalesforceOrgCleanup{}, orchestratorWorkerError{orchestratorWorkerCleanupFailed}
	}
	return cleanup, nil
}

type OrchestratorShardRunner func(context.Context, OrchestratorWorkerRequest) (OrchestratorWorkerRunResult, error)

type orchestratorWorkerError struct{ code string }

func (e orchestratorWorkerError) Error() string { return e.code }

func RunOrchestratorWorkerOnce(ctx context.Context, orchestrator *Orchestrator, request OrchestratorWorkerRequest, runner OrchestratorShardRunner) (OrchestratorWorkerResult, error) {
	return runOrchestratorWorkerOnce(ctx, orchestrator, request, func() time.Time { return time.Now().UTC() }, runner, VerifySalesforceReconciliation, func() error { return nil })
}

func runOrchestratorWorkerOnce(ctx context.Context, orchestrator *Orchestrator, request OrchestratorWorkerRequest, clock func() time.Time, runner OrchestratorShardRunner, reconcile func(string, string, string) error, beforeCredit func() error) (result OrchestratorWorkerResult, err error) {
	if orchestrator == nil || clock == nil || runner == nil || reconcile == nil || beforeCredit == nil || validateOrchestratorWorkerRequest(request) != nil {
		return result, orchestratorWorkerError{orchestratorWorkerWrapperFailed}
	}
	failureCode := orchestratorWorkerWrapperFailed
	defer func() {
		if err != nil {
			_ = orchestrator.recordWorkerFailure(request.Lease, failureCode, clock().UTC())
		}
	}()
	if receipt, _, found, loadErr := loadOrchestratorReceipt(orchestrator.db, request.Lease.CampaignID, request.Lease.JobID, request.Lease.Generation); loadErr != nil {
		return result, orchestratorWorkerError{failureCode}
	} else if found {
		transfer, carrier, validateErr := validateExistingOrchestratorWorkerTransferBatch(request.Plan, request.Lease, request.EvidenceRoot, request.OraclePlanPath, reconcile)
		if validateErr != nil || receipt.BatchRoot != transfer.BatchRoot || receipt.ManifestSHA256 != carrier.ManifestSHA256 || receipt.BindingSHA256 != carrier.AuthoritySHA256 {
			return result, orchestratorWorkerError{failureCode}
		}
		_ = orchestrator.closeWorkerActions(request.Lease)
		return OrchestratorWorkerResult{BatchRoot: transfer.BatchRoot, ManifestSHA256: transfer.ManifestSHA256, Receipt: receipt}, nil
	}
	now := clock().UTC()
	heartbeat, durationErr := orchestrator.issuedLeaseDuration(request.Lease)
	if durationErr != nil || request.Lease.DurationMS != heartbeat.Milliseconds() || orchestrator.Heartbeat(request.Lease, now, heartbeat) != nil {
		return result, orchestratorWorkerError{failureCode}
	}
	workContext, cancelHeartbeat := context.WithCancel(ctx)
	heartbeatDone := make(chan error, 1)
	go maintainOrchestratorWorkerHeartbeat(workContext, cancelHeartbeat, orchestrator, request.Lease, heartbeat, clock, heartbeatDone)
	heartbeatStopped := false
	stopHeartbeat := func() error {
		if heartbeatStopped {
			return nil
		}
		cancelHeartbeat()
		heartbeatStopped = true
		return <-heartbeatDone
	}
	defer func() {
		if heartbeatErr := stopHeartbeat(); err == nil && heartbeatErr != nil {
			failureCode = orchestratorWorkerWrapperFailed
			err = orchestratorWorkerError{failureCode}
		}
	}()
	transfer, carrier, transferErr := validateExistingOrchestratorWorkerTransferBatch(request.Plan, request.Lease, request.EvidenceRoot, request.OraclePlanPath, reconcile)
	if transferErr != nil {
		if !os.IsNotExist(transferErr) {
			failureCode = orchestratorWorkerTransferFailed
			if transferErr == errOrchestratorReconciliation {
				failureCode = orchestratorWorkerReconciliationFailed
			}
			return result, orchestratorWorkerError{failureCode}
		}
		if err = orchestrator.SetHubCapacity(request.HubAlias, request.HubCapacity); err != nil {
			failureCode = orchestratorWorkerReserveFailed
			return result, orchestratorWorkerError{failureCode}
		}
		if err = orchestrator.Reserve(request.Lease, request.HubAlias, request.AllocationAlias, now); err != nil {
			failureCode = orchestratorWorkerReserveFailed
			return result, orchestratorWorkerError{failureCode}
		}
		var run OrchestratorWorkerRunResult
		if run, err = runner(workContext, request); err != nil || workContext.Err() != nil || (run.Production == nil) == (run.BatchRoot == "") {
			return result, orchestratorWorkerError{failureCode}
		}
		sourceBatchRoot := run.BatchRoot
		if run.Production != nil {
			production := *run.Production
			if _, err = BuildOrchestratorProductionBatch(production); err != nil {
				return result, orchestratorWorkerError{failureCode}
			}
			sourceBatchRoot = production.OutputPath
		}
		if !filepath.IsAbs(sourceBatchRoot) {
			return result, orchestratorWorkerError{failureCode}
		}
		if _, err = validateOrchestratorBatch(sourceBatchRoot, request.Plan, request.Lease, true); err != nil {
			return result, orchestratorWorkerError{failureCode}
		}
		failureCode = orchestratorWorkerTransferFailed
		transfer, carrier, transferErr = transferValidatedOrchestratorWorkerBatch(OrchestratorWorkerTransferRequest{Plan: request.Plan, Lease: request.Lease, SourceBatchRoot: sourceBatchRoot, EvidenceRoot: request.EvidenceRoot, OraclePlanPath: request.OraclePlanPath}, reconcile, nil)
		if transferErr != nil {
			if transferErr == errOrchestratorReconciliation {
				failureCode = orchestratorWorkerReconciliationFailed
			}
			return result, orchestratorWorkerError{failureCode}
		}
	}
	failureCode = orchestratorWorkerCleanupFailed
	cleanupState, stateErr := orchestrator.workerCleanupState(request.Lease, request.AllocationAlias)
	if stateErr != nil {
		return result, orchestratorWorkerError{failureCode}
	}
	if cleanupState == "pending" {
		now = clock().UTC()
		if err = orchestrator.Heartbeat(request.Lease, now, heartbeat); err != nil {
			return result, orchestratorWorkerError{failureCode}
		}
		claim, claimErr := orchestrator.ClaimCleanupForLease(request.Lease, request.AllocationAlias, now, heartbeat)
		if claimErr != nil || claim.JobID != request.Lease.JobID || claim.Generation != request.Lease.Generation || claim.AllocationAlias != request.AllocationAlias || claim.HubAlias != request.HubAlias {
			return result, orchestratorWorkerError{failureCode}
		}
		if err = orchestrator.CloseCleanup(claim, now); err != nil {
			return result, orchestratorWorkerError{failureCode}
		}
	} else if cleanupState != "closed" {
		return result, orchestratorWorkerError{failureCode}
	}
	if err = beforeCredit(); err != nil {
		failureCode = orchestratorWorkerCreditFailed
		return result, orchestratorWorkerError{failureCode}
	}
	if err = stopHeartbeat(); err != nil {
		failureCode = orchestratorWorkerCreditFailed
		return result, orchestratorWorkerError{failureCode}
	}
	now = clock().UTC()
	if err = orchestrator.Heartbeat(request.Lease, now, heartbeat); err != nil {
		failureCode = orchestratorWorkerCreditFailed
		return result, orchestratorWorkerError{failureCode}
	}
	receipt, receiptErr := orchestrator.recordValidatedReceipt(OrchestratorReceiptRequest{Lease: request.Lease, BatchRoot: transfer.BatchRoot}, now, carrier)
	if receiptErr != nil {
		failureCode = orchestratorWorkerCreditFailed
		return result, orchestratorWorkerError{failureCode}
	}
	_ = orchestrator.closeWorkerActions(request.Lease)
	return OrchestratorWorkerResult{BatchRoot: transfer.BatchRoot, ManifestSHA256: transfer.ManifestSHA256, Receipt: receipt}, nil
}

func maintainOrchestratorWorkerHeartbeat(ctx context.Context, cancel context.CancelFunc, orchestrator *Orchestrator, lease OrchestratorLease, duration time.Duration, clock func() time.Time, done chan<- error) {
	interval := duration / 3
	if interval < 10*time.Millisecond {
		interval = 10 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			done <- nil
			return
		case <-ticker.C:
			if err := orchestrator.Heartbeat(lease, clock().UTC(), duration); err != nil {
				cancel()
				done <- err
				return
			}
		}
	}
}

var errOrchestratorReconciliation = orchestratorWorkerError{orchestratorWorkerReconciliationFailed}

func TransferOrchestratorWorkerBatch(request OrchestratorWorkerTransferRequest) (OrchestratorWorkerTransfer, error) {
	return transferOrchestratorWorkerBatch(request, VerifySalesforceReconciliation, nil)
}

func transferOrchestratorWorkerBatch(request OrchestratorWorkerTransferRequest, reconcile func(string, string, string) error, beforeRename func(string) error) (OrchestratorWorkerTransfer, error) {
	transfer, _, err := transferValidatedOrchestratorWorkerBatch(request, reconcile, beforeRename)
	return transfer, err
}

func transferValidatedOrchestratorWorkerBatch(request OrchestratorWorkerTransferRequest, reconcile func(string, string, string) error, beforeRename func(string) error) (OrchestratorWorkerTransfer, validatedOrchestratorBatch, error) {
	if reconcile == nil || !filepath.IsAbs(request.SourceBatchRoot) || !filepath.IsAbs(request.EvidenceRoot) || !filepath.IsAbs(request.OraclePlanPath) {
		return OrchestratorWorkerTransfer{}, validatedOrchestratorBatch{}, fmt.Errorf("absolute worker transfer paths are required")
	}
	if err := validateOrchestratorWorkerPlanLease(request.Plan, request.Lease); err != nil {
		return OrchestratorWorkerTransfer{}, validatedOrchestratorBatch{}, err
	}
	batch, err := validateOrchestratorBatch(request.SourceBatchRoot, request.Plan, request.Lease, true)
	if err != nil {
		return OrchestratorWorkerTransfer{}, validatedOrchestratorBatch{}, err
	}
	if batch.SchemaVersion != orchestratorProductionBatchSchema {
		if hash, err := sha256File(request.OraclePlanPath); err != nil || request.Plan.Definition.ControlledInputSHA256["oracle-plan"] != hash {
			return OrchestratorWorkerTransfer{}, validatedOrchestratorBatch{}, fmt.Errorf("oracle plan binding drift")
		}
	}
	if err := ensureOrchestratorEvidenceRoot(request.EvidenceRoot); err != nil {
		return OrchestratorWorkerTransfer{}, validatedOrchestratorBatch{}, err
	}
	final := filepath.Join(request.EvidenceRoot, orchestratorWorkerBatchName(request.Lease))
	if _, err := os.Lstat(final); err == nil {
		existingTransfer, existingBatch, validateErr := validateExistingOrchestratorWorkerTransferBatch(request.Plan, request.Lease, request.EvidenceRoot, request.OraclePlanPath, reconcile)
		comparison := existingBatch
		comparison.BatchRoot = ""
		if validateErr != nil || !reflect.DeepEqual(comparison, batch) {
			return OrchestratorWorkerTransfer{}, validatedOrchestratorBatch{}, fmt.Errorf("existing worker batch differs from sealed transfer")
		}
		return existingTransfer, existingBatch, nil
	} else if !os.IsNotExist(err) {
		return OrchestratorWorkerTransfer{}, validatedOrchestratorBatch{}, err
	}
	temp, err := os.MkdirTemp(request.EvidenceRoot, ".worker-transfer-")
	if err != nil {
		return OrchestratorWorkerTransfer{}, validatedOrchestratorBatch{}, err
	}
	defer os.RemoveAll(temp)
	if err := os.Chmod(temp, 0o700); err != nil {
		return OrchestratorWorkerTransfer{}, validatedOrchestratorBatch{}, err
	}
	for _, relative := range batch.Paths {
		if err := copyOrchestratorWorkerFile(request.SourceBatchRoot, temp, relative); err != nil {
			return OrchestratorWorkerTransfer{}, validatedOrchestratorBatch{}, err
		}
	}
	if batch.SchemaVersion != orchestratorProductionBatchSchema {
		receipt := filepath.Join(temp, "evidence", "SALESFORCE_RECONCILIATION.json")
		packet := filepath.Join(temp, "evidence", "salesforce-reconciliation-packet")
		if err := reconcile(request.OraclePlanPath, receipt, packet); err != nil {
			return OrchestratorWorkerTransfer{}, validatedOrchestratorBatch{}, errOrchestratorReconciliation
		}
	}
	validated, err := validateOrchestratorBatch(temp, request.Plan, request.Lease, true)
	if err != nil || !reflect.DeepEqual(validated, batch) {
		return OrchestratorWorkerTransfer{}, validatedOrchestratorBatch{}, fmt.Errorf("coordinator-local runtime batch validation failed")
	}
	if err := syncOrchestratorWorkerTree(temp); err != nil {
		return OrchestratorWorkerTransfer{}, validatedOrchestratorBatch{}, err
	}
	if beforeRename != nil {
		if err := beforeRename(final); err != nil {
			return OrchestratorWorkerTransfer{}, validatedOrchestratorBatch{}, err
		}
	}
	if err := os.Rename(temp, final); err != nil {
		return OrchestratorWorkerTransfer{}, validatedOrchestratorBatch{}, err
	}
	if err := syncOrchestratorWorkerDirectory(request.EvidenceRoot); err != nil && batch.SchemaVersion != orchestratorProductionBatchSchema {
		return OrchestratorWorkerTransfer{}, validatedOrchestratorBatch{}, err
	}
	validated.BatchRoot = final
	return OrchestratorWorkerTransfer{BatchRoot: final, ManifestSHA256: batch.ManifestSHA256}, validated, nil
}

func validateExistingOrchestratorWorkerTransfer(plan OrchestratorCampaignPlan, lease OrchestratorLease, evidenceRoot, oraclePlanPath string, reconcile func(string, string, string) error) (OrchestratorWorkerTransfer, error) {
	transfer, _, err := validateExistingOrchestratorWorkerTransferBatch(plan, lease, evidenceRoot, oraclePlanPath, reconcile)
	return transfer, err
}

func validateExistingOrchestratorWorkerTransferBatch(plan OrchestratorCampaignPlan, lease OrchestratorLease, evidenceRoot, oraclePlanPath string, reconcile func(string, string, string) error) (OrchestratorWorkerTransfer, validatedOrchestratorBatch, error) {
	if reconcile == nil || !filepath.IsAbs(evidenceRoot) || !filepath.IsAbs(oraclePlanPath) {
		return OrchestratorWorkerTransfer{}, validatedOrchestratorBatch{}, fmt.Errorf("absolute worker transfer paths are required")
	}
	if err := validateOrchestratorWorkerPlanLease(plan, lease); err != nil {
		return OrchestratorWorkerTransfer{}, validatedOrchestratorBatch{}, err
	}
	final := filepath.Join(evidenceRoot, orchestratorWorkerBatchName(lease))
	info, err := os.Lstat(final)
	if err != nil {
		return OrchestratorWorkerTransfer{}, validatedOrchestratorBatch{}, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return OrchestratorWorkerTransfer{}, validatedOrchestratorBatch{}, fmt.Errorf("worker batch destination is not a directory")
	}
	batch, err := validateOrchestratorBatch(final, plan, lease, true)
	if err != nil {
		return OrchestratorWorkerTransfer{}, validatedOrchestratorBatch{}, err
	}
	if batch.SchemaVersion != orchestratorProductionBatchSchema {
		if hash, err := sha256File(oraclePlanPath); err != nil || plan.Definition.ControlledInputSHA256["oracle-plan"] != hash {
			return OrchestratorWorkerTransfer{}, validatedOrchestratorBatch{}, fmt.Errorf("oracle plan binding drift")
		}
		if err := reconcile(oraclePlanPath, filepath.Join(final, "evidence", "SALESFORCE_RECONCILIATION.json"), filepath.Join(final, "evidence", "salesforce-reconciliation-packet")); err != nil {
			return OrchestratorWorkerTransfer{}, validatedOrchestratorBatch{}, errOrchestratorReconciliation
		}
	}
	batch.BatchRoot = final
	return OrchestratorWorkerTransfer{BatchRoot: final, ManifestSHA256: batch.ManifestSHA256}, batch, nil
}

type validatedOrchestratorBatch struct {
	BatchRoot       string
	SchemaVersion   int
	ManifestSHA256  string
	AuthoritySHA256 string
	Candidate       OrchestratorArtifact
	Tools           OrchestratorArtifact
	ProofStates     map[string]string
	Paths           []string
	ProductionFiles []ProductionRuntimeBatchFile
	Legacy          SurfaceOracleIndexRuntimeBatch
}

func validateOrchestratorBatch(root string, plan OrchestratorCampaignPlan, lease OrchestratorLease, transfer bool) (validatedOrchestratorBatch, error) {
	productionMarker := filepath.Join(root, "production", "PRODUCTION_RUNTIME_BATCH.json")
	_, productionErr := os.Lstat(productionMarker)
	production := productionErr == nil
	legacy := false
	for _, marker := range []string{filepath.Join(root, "inputs", "RUNTIME_BATCH_MANIFEST.json"), filepath.Join(root, "evidence", "ORCHESTRATOR_BINDING.json")} {
		if _, err := os.Lstat(marker); err == nil {
			legacy = true
		} else if !os.IsNotExist(err) {
			return validatedOrchestratorBatch{}, err
		}
	}
	if production && legacy {
		return validatedOrchestratorBatch{}, fmt.Errorf("mixed production v3 and legacy batch markers")
	}
	if production {
		validated, err := validateOrchestratorProductionBatch(root, plan, lease)
		if err != nil {
			return validatedOrchestratorBatch{}, err
		}
		return validatedOrchestratorBatch{
			SchemaVersion: orchestratorProductionBatchSchema, ManifestSHA256: validated.manifestSHA256, AuthoritySHA256: validated.manifestSHA256,
			Candidate: OrchestratorArtifact{Commit: validated.manifest.Candidate.Commit, SHA256: validated.manifest.Candidate.SHA256}, Tools: OrchestratorArtifact{Commit: validated.manifest.NativeTools.Commit, SHA256: validated.manifest.NativeTools.SHA256},
			ProofStates: validated.proofStates, Paths: validated.paths, ProductionFiles: append([]ProductionRuntimeBatchFile(nil), validated.manifest.Files...),
		}, nil
	}
	if !os.IsNotExist(productionErr) {
		return validatedOrchestratorBatch{}, productionErr
	}
	scope, scopeBytes, err := readExactJSONBytes[SurfaceOracleScope](plan.Definition.ScopePath)
	if err != nil || replayBytesSHA256(scopeBytes) != plan.Definition.ScopeSHA256 {
		return validatedOrchestratorBatch{}, fmt.Errorf("campaign scope binding drift")
	}
	legacyBatch, states, err := validateSurfaceRuntimeAdjudications(root, scope)
	if err != nil {
		return validatedOrchestratorBatch{}, err
	}
	if legacyBatch.candidateCommit != plan.Definition.Candidate.Commit || legacyBatch.candidateSHA256 != plan.Definition.Candidate.SHA256 || legacyBatch.toolsCommit != plan.Definition.Tools.Commit || legacyBatch.toolsSHA256 != plan.Definition.Tools.SHA256 {
		return validatedOrchestratorBatch{}, fmt.Errorf("worker batch campaign binding drift")
	}
	if len(states) != len(lease.SurfaceIDs) {
		return validatedOrchestratorBatch{}, fmt.Errorf("worker batch must adjudicate exact shard")
	}
	for _, surfaceID := range lease.SurfaceIDs {
		if states[surfaceID] == "" {
			return validatedOrchestratorBatch{}, fmt.Errorf("worker batch must adjudicate exact shard")
		}
	}
	binding, err := readSurfaceBatchFile(root, filepath.Join("evidence", "ORCHESTRATOR_BINDING.json"))
	if err != nil || binding.Mode.Perm() != 0o400 {
		return validatedOrchestratorBatch{}, fmt.Errorf("sealed orchestrator batch binding is required")
	}
	var bindingValue OrchestratorBatchBinding
	wantBinding, wantErr := expectedOrchestratorBatchBinding(plan, lease)
	if decodeExactJSON(binding.Data, &bindingValue) != nil || wantErr != nil || !reflect.DeepEqual(bindingValue, wantBinding) {
		return validatedOrchestratorBatch{}, fmt.Errorf("orchestrator batch binding drift")
	}
	proofStates := make(map[string]string, len(states))
	for surfaceID, state := range states {
		switch state {
		case "matched":
			proofStates[surfaceID] = "accepted"
		case "product-mismatch":
			proofStates[surfaceID] = "rejected"
		case "inconclusive":
			proofStates[surfaceID] = "inconclusive"
		default:
			return validatedOrchestratorBatch{}, fmt.Errorf("validated batch must adjudicate the exact shard in v1")
		}
	}
	paths := []string(nil)
	if transfer {
		validated, transferPaths, err := validateOrchestratorWorkerBatch(root, plan, lease)
		if err != nil {
			return validatedOrchestratorBatch{}, err
		}
		if !reflect.DeepEqual(validated, legacyBatch) {
			return validatedOrchestratorBatch{}, fmt.Errorf("legacy worker batch validation drift")
		}
		paths = transferPaths
	}
	return validatedOrchestratorBatch{SchemaVersion: 1, ManifestSHA256: legacyBatch.ManifestSHA256, AuthoritySHA256: replayBytesSHA256(binding.Data), Candidate: OrchestratorArtifact{Commit: legacyBatch.candidateCommit, SHA256: legacyBatch.candidateSHA256}, Tools: OrchestratorArtifact{Commit: legacyBatch.toolsCommit, SHA256: legacyBatch.toolsSHA256}, ProofStates: proofStates, Paths: paths, Legacy: legacyBatch}, nil
}

func validateOrchestratorWorkerRequest(request OrchestratorWorkerRequest) error {
	if request.HubCapacity < 1 || !safeOrchestratorToken(request.HubAlias) || !safeOrchestratorToken(request.AllocationAlias) || !filepath.IsAbs(request.EvidenceRoot) || !filepath.IsAbs(request.OraclePlanPath) {
		return fmt.Errorf("invalid worker request")
	}
	return validateOrchestratorWorkerPlanLease(request.Plan, request.Lease)
}

func validateOrchestratorWorkerPlanLease(plan OrchestratorCampaignPlan, lease OrchestratorLease) error {
	if err := validateOrchestratorPlan(plan); err != nil {
		return err
	}
	if lease.CampaignID != plan.CampaignID || lease.Generation < 1 || lease.DurationMS < 1 || lease.Kind != OrchestratorJobSurfaceRuntimeShard {
		return fmt.Errorf("worker lease does not bind immutable campaign")
	}
	for _, job := range plan.Jobs {
		if job.ID == lease.JobID && job.Kind == lease.Kind && job.ShardIndex == lease.ShardIndex && reflect.DeepEqual(job.SurfaceIDs, lease.SurfaceIDs) {
			return nil
		}
	}
	return fmt.Errorf("worker lease does not bind immutable campaign")
}

func validateOrchestratorWorkerBatch(root string, plan OrchestratorCampaignPlan, lease OrchestratorLease) (SurfaceOracleIndexRuntimeBatch, []string, error) {
	scope, scopeBytes, err := readExactJSONBytes[SurfaceOracleScope](plan.Definition.ScopePath)
	if err != nil || replayBytesSHA256(scopeBytes) != plan.Definition.ScopeSHA256 {
		return SurfaceOracleIndexRuntimeBatch{}, nil, fmt.Errorf("campaign scope binding drift")
	}
	batch, states, err := validateSurfaceRuntimeAdjudications(root, scope)
	if err != nil {
		return SurfaceOracleIndexRuntimeBatch{}, nil, err
	}
	if batch.candidateCommit != plan.Definition.Candidate.Commit || batch.candidateSHA256 != plan.Definition.Candidate.SHA256 || batch.toolsCommit != plan.Definition.Tools.Commit || batch.toolsSHA256 != plan.Definition.Tools.SHA256 {
		return SurfaceOracleIndexRuntimeBatch{}, nil, fmt.Errorf("worker batch campaign binding drift")
	}
	if len(states) != len(lease.SurfaceIDs) {
		return SurfaceOracleIndexRuntimeBatch{}, nil, fmt.Errorf("worker batch must adjudicate exact shard")
	}
	for _, id := range lease.SurfaceIDs {
		if states[id] == "" {
			return SurfaceOracleIndexRuntimeBatch{}, nil, fmt.Errorf("worker batch must adjudicate exact shard")
		}
	}
	expected := OrchestratorBatchBinding{
		SchemaVersion: 1, CampaignID: plan.CampaignID, SpecSHA256: plan.SpecSHA256,
		Candidate: plan.Definition.Candidate, Tools: plan.Definition.Tools, ScopeSHA256: plan.Definition.ScopeSHA256,
		ControlledInputSHA256: plan.Definition.ControlledInputSHA256, JobID: lease.JobID, JobKind: lease.Kind,
		Generation: lease.Generation, ShardIndex: lease.ShardIndex, SurfaceIDs: lease.SurfaceIDs,
	}
	binding, _, err := readExactJSONBytes[OrchestratorBatchBinding](filepath.Join(root, "evidence", "ORCHESTRATOR_BINDING.json"))
	if err != nil || !reflect.DeepEqual(binding, expected) {
		return SurfaceOracleIndexRuntimeBatch{}, nil, fmt.Errorf("worker batch orchestrator binding drift")
	}
	paths, err := orchestratorWorkerTransferPaths(root)
	return batch, paths, err
}

func orchestratorWorkerTransferPaths(root string) ([]string, error) {
	paths := []string{
		"inputs/RUNTIME_BATCH_MANIFEST.json", "inputs/RUNTIME_BATCH_PROFILE.json", "evidence/BINDINGS.json",
		"local-proof/LOCAL_RUNTIME_SUMMARY.json", "evidence/RECONCILIATION.json", "evidence/MISMATCH_REVIEW.json",
		"evidence/FINAL_AUDIT.json", "oracle/results.json", "evidence/ORG_CLEANUP.json", "evidence/RUN_SUMMARY.json",
		"bin/glade-sealed", "bin/glade-tools", "source/glade-tools/scripts/corpus-assurance/salesforce-first-filter.py",
		"evidence/ORCHESTRATOR_BINDING.json", "evidence/SALESFORCE_RECONCILIATION.json",
	}
	manifest, _, err := readExactJSONBytes[surfaceRuntimeManifest](filepath.Join(root, "inputs", "RUNTIME_BATCH_MANIFEST.json"))
	if err != nil {
		return nil, err
	}
	for _, fixture := range manifest.Fixtures {
		paths = append(paths, filepath.ToSlash(filepath.Join("source", "glade-tools", fixture.Path)))
	}
	receipt, _, err := readExactJSONBytes[SalesforceReconciliation](filepath.Join(root, "evidence", "SALESFORCE_RECONCILIATION.json"))
	if err != nil || !sha256Pattern.MatchString(receipt.PacketManifestSHA256) {
		return nil, fmt.Errorf("invalid Salesforce reconciliation receipt")
	}
	packetRoot := filepath.Join(root, "evidence", "salesforce-reconciliation-packet")
	packet, err := readReconciliationPacket(packetRoot, receipt.PacketManifestSHA256)
	if err != nil {
		return nil, err
	}
	paths = append(paths, "evidence/salesforce-reconciliation-packet/"+reconciliationPacketManifestName)
	for relative := range packet {
		paths = append(paths, filepath.ToSlash(filepath.Join("evidence", "salesforce-reconciliation-packet", relative)))
	}
	sort.Strings(paths)
	return paths, nil
}

func ensureOrchestratorEvidenceRoot(root string) error {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 {
		return fmt.Errorf("coordinator evidence root must be a mode-0700 directory")
	}
	return nil
}

func orchestratorWorkerBatchName(lease OrchestratorLease) string {
	return lease.CampaignID + "-shard-" + strconv.Itoa(lease.ShardIndex) + "-g" + strconv.Itoa(lease.Generation)
}

func copyOrchestratorWorkerFile(sourceRoot, destinationRoot, relative string) error {
	snapshot, err := readSurfaceBatchFile(sourceRoot, filepath.FromSlash(relative))
	if err != nil {
		return err
	}
	destination, err := rootedPath(destinationRoot, filepath.FromSlash(relative))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, snapshot.Mode.Perm())
	if err != nil {
		return err
	}
	if _, err = file.Write(snapshot.Data); err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	return err
}

func syncOrchestratorWorkerTree(root string) error {
	directories := []string{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err == nil && info.IsDir() {
			directories = append(directories, path)
		}
		return err
	})
	if err != nil {
		return err
	}
	sort.Sort(sort.Reverse(sort.StringSlice(directories)))
	for _, directory := range directories {
		if err := syncOrchestratorWorkerDirectory(directory); err != nil {
			return err
		}
	}
	return nil
}

func syncOrchestratorWorkerDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	err = directory.Sync()
	if closeErr := directory.Close(); err == nil {
		err = closeErr
	}
	return err
}

func (o *Orchestrator) recordWorkerFailure(lease OrchestratorLease, code string, now time.Time) error {
	tx, err := o.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE attempts SET status = 'failed' WHERE campaign_id = ? AND job_id = ? AND generation = ? AND worker = ? AND status = 'running'`, lease.CampaignID, lease.JobID, lease.Generation, lease.Worker); err != nil {
		return err
	}
	detail := fmt.Sprintf("job %s generation %d: retry required; no scratch reservation recorded", lease.JobID, lease.Generation)
	var cleanupState string
	if queryErr := tx.QueryRow(`SELECT state FROM cleanup_journal WHERE campaign_id = ? AND job_id = ? AND generation = ?`, lease.CampaignID, lease.JobID, lease.Generation).Scan(&cleanupState); queryErr == nil {
		if cleanupState == "closed" {
			detail = fmt.Sprintf("job %s generation %d: proof finalization retry required; scratch cleanup closed", lease.JobID, lease.Generation)
		} else {
			detail = fmt.Sprintf("job %s generation %d: scratch cleanup action required; journal %s", lease.JobID, lease.Generation, cleanupState)
		}
	}
	if _, err := tx.Exec(`INSERT INTO actions (campaign_id, kind, detail, state, created_at) SELECT ?, ?, ?, 'open', ? WHERE NOT EXISTS (SELECT 1 FROM actions WHERE campaign_id = ? AND kind = ? AND detail = ? AND state = 'open')`, lease.CampaignID, code, detail, now.UTC().UnixMilli(), lease.CampaignID, code, detail); err != nil {
		return err
	}
	return tx.Commit()
}

func (o *Orchestrator) closeWorkerActions(lease OrchestratorLease) error {
	exact := fmt.Sprintf("job %s generation %d:", lease.JobID, lease.Generation)
	job := fmt.Sprintf("job %s generation ", lease.JobID)
	noReservation := ": retry required; no scratch reservation recorded"
	cleanupClosed := ": proof finalization retry required; scratch cleanup closed"
	_, err := o.db.Exec(`UPDATE actions SET state = 'closed' WHERE campaign_id = ? AND state = 'open' AND (substr(detail, 1, ?) = ? OR (substr(detail, 1, ?) = ? AND (substr(detail, -?) = ? OR substr(detail, -?) = ?)))`, lease.CampaignID, len(exact), exact, len(job), job, len(noReservation), noReservation, len(cleanupClosed), cleanupClosed)
	return err
}

func (o *Orchestrator) workerCleanupState(lease OrchestratorLease, allocationAlias string) (string, error) {
	var state string
	err := o.db.QueryRow(`SELECT state FROM cleanup_journal WHERE allocation_alias = ? AND campaign_id = ? AND job_id = ? AND generation = ?`, allocationAlias, lease.CampaignID, lease.JobID, lease.Generation).Scan(&state)
	return state, err
}
