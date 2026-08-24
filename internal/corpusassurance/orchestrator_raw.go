package corpusassurance

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type rawSalesforceOrgCreateRunner func(SalesforceOrgCreateRequest) (SalesforceOrgCreation, error)
type rawSalesforceOrgPreflightRunner func(SalesforceOrgPreflightRequest) (SalesforceOrgPreflight, error)
type rawSalesforceDispatchRunner func(SalesforceDispatchRequest) (SalesforceDispatch, error)
type rawSalesforceShardRunner func(SalesforceShardRequest) (SalesforceShard, error)
type rawSalesforceOrgCleanupRunner func(SalesforceOrgCleanupRequest) (SalesforceOrgCleanup, error)

const orchestratorWorkerOnceTimeout = 45 * time.Minute

// RawSalesforceShardRequest is the complete caller-controlled input for one
// lease-bound Salesforce shard. The runner fields are package-private test
// seams; production calls use the typed Salesforce lifecycle functions.
type RawSalesforceShardRequest struct {
	Plan       OrchestratorCampaignPlan
	Lease      OrchestratorLease
	BundlePath string
	DevHub     string
	TargetOrg  string
	SFBin      string
	OutputRoot string

	validateBundle func(string) error
	orgCreate      rawSalesforceOrgCreateRunner
	orgPreflight   rawSalesforceOrgPreflightRunner
	dispatch       rawSalesforceDispatchRunner
	shard          rawSalesforceShardRunner
	orgCleanup     rawSalesforceOrgCleanupRunner
}

type RawSalesforceShardResult struct {
	BindingPath          string
	SalesforceShardFiles SalesforceShardFiles
	Binding              OrchestratorBatchBinding
	Creation             SalesforceOrgCreation
	Preflight            SalesforceOrgPreflight
	Dispatch             SalesforceDispatch
	Shard                SalesforceShard
	Cleanup              SalesforceOrgCleanup
}

type OrchestratorRawCanaryRequest struct {
	Coordinator      *Orchestrator
	Plan             OrchestratorCampaignPlan
	Lease            OrchestratorLease
	PlanSHA256       string
	LeaseSHA256      string
	SSHReceiptSHA256 string
	AllocationAlias  string
	SSHReceipt       OrchestratorSSHDispatchReceipt
	ReceiptPath      string
	PacketPath       string
	OutputPath       string
}

type OrchestratorRawCanaryReceipt struct {
	SchemaVersion             int                  `json:"schemaVersion"`
	Status                    string               `json:"status"`
	ProofCredit               int                  `json:"proofCredit"`
	CampaignID                string               `json:"campaignId"`
	JobID                     string               `json:"jobId"`
	ShardIndex                int                  `json:"shardIndex"`
	Generation                int                  `json:"generation"`
	Candidate                 OrchestratorArtifact `json:"candidate"`
	Tools                     OrchestratorArtifact `json:"tools"`
	SpecSHA256                string               `json:"specSha256"`
	PlanSHA256                string               `json:"planSha256"`
	LeaseSHA256               string               `json:"leaseSha256"`
	SSHReceiptSHA256          string               `json:"sshReceiptSha256"`
	ReconciliationSHA256      string               `json:"reconciliationSha256"`
	PacketManifestSHA256      string               `json:"packetManifestSha256"`
	OraclePlanSHA256          string               `json:"oraclePlanSha256"`
	BundleSHA256              string               `json:"bundleSha256"`
	OrchestratorBindingSHA256 string               `json:"orchestratorBindingSha256"`
	SalesforceShardSHA256     string               `json:"salesforceShardSha256"`
	OrgCleanupSHA256          string               `json:"orgCleanupSha256"`
	CleanupClosed             bool                 `json:"cleanupClosed"`
}

func AcceptOrchestratorRawCanary(request OrchestratorRawCanaryRequest) (result OrchestratorRawCanaryReceipt, err error) {
	if request.Coordinator == nil || request.Coordinator.db == nil || !filepath.IsAbs(request.ReceiptPath) || !filepath.IsAbs(request.PacketPath) || !filepath.IsAbs(request.OutputPath) || filepath.Clean(request.ReceiptPath) != request.ReceiptPath || filepath.Clean(request.PacketPath) != request.PacketPath || filepath.Clean(request.OutputPath) != request.OutputPath || !safeOrchestratorToken(request.AllocationAlias) {
		return result, fmt.Errorf("invalid raw canary acceptance request")
	}
	outputExists := false
	if info, err := os.Lstat(request.OutputPath); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return result, fmt.Errorf("raw canary output must not be a symlink")
		}
		outputExists = true
	} else if !os.IsNotExist(err) {
		return result, err
	}
	if err := validateOrchestratorWorkerPlanLease(request.Plan, request.Lease); err != nil || !sha256Pattern.MatchString(request.PlanSHA256) || !sha256Pattern.MatchString(request.LeaseSHA256) || !sha256Pattern.MatchString(request.SSHReceiptSHA256) {
		return result, fmt.Errorf("raw canary plan and lease binding is invalid")
	}
	// The successful SSH receipt below binds the exact input bytes. Re-marshalling
	// here would reject equivalent JSON encodings used by recovered leases.
	ssh := request.SSHReceipt
	if ssh.SchemaVersion != 1 || !ssh.Passed || ssh.Status != "worker-complete" || ssh.ExitCode != 0 || ssh.TimedOut || ssh.FailureCode != "" || ssh.ActionRequired || ssh.ActionCode != "" || ssh.CampaignID != request.Lease.CampaignID || ssh.JobID != request.Lease.JobID || ssh.ShardIndex != request.Lease.ShardIndex || ssh.Generation != request.Lease.Generation || ssh.SpecSHA256 != request.Plan.SpecSHA256 || ssh.PlanSHA256 != request.PlanSHA256 || ssh.LeaseSHA256 != request.LeaseSHA256 || !sha256Pattern.MatchString(ssh.CommandSHA256) || !sha256Pattern.MatchString(ssh.StdoutSHA256) || !sha256Pattern.MatchString(ssh.StderrSHA256) || !sha256Pattern.MatchString(ssh.OrchestratorBindingSHA256) || !sha256Pattern.MatchString(ssh.SalesforceShardSHA256) || !sha256Pattern.MatchString(ssh.OrgCleanupSHA256) {
		return result, fmt.Errorf("SSH worker receipt does not bind the raw canary")
	}
	sshBytes, err := json.Marshal(ssh)
	if err != nil {
		return result, err
	}
	if replayBytesSHA256(append(sshBytes, '\n')) != request.SSHReceiptSHA256 {
		return result, fmt.Errorf("SSH worker receipt bytes are not canonical")
	}
	if err := VerifyOrchestratorSalesforceReconciliation(request.Plan, request.Lease, request.ReceiptPath, request.PacketPath); err != nil {
		return result, fmt.Errorf("verify retained worker reconciliation: %w", err)
	}
	reconciliation, reconciliationBytes, err := readExactJSONBytes[SalesforceReconciliation](request.ReceiptPath)
	if err != nil || len(reconciliation.Shards) != 1 {
		return result, fmt.Errorf("read retained worker reconciliation")
	}
	shard := reconciliation.Shards[0]
	if reconciliation.Status != "pass" || reconciliation.OrchestratorPlanSHA256 != request.PlanSHA256 || reconciliation.OrchestratorBindingSHA256 != ssh.OrchestratorBindingSHA256 || shard.InputSHA256["shard"] != ssh.SalesforceShardSHA256 || shard.InputSHA256["cleanup"] != ssh.OrgCleanupSHA256 {
		return result, fmt.Errorf("SSH worker receipt does not match retained reconciliation")
	}
	packetFiles, err := readReconciliationPacket(request.PacketPath, reconciliation.PacketManifestSHA256)
	if err != nil {
		return result, fmt.Errorf("read retained worker packet: %w", err)
	}
	cleanupPath := fmt.Sprintf("shards/%02d/ORG_CLEANUP.json", shard.ShardIndex)
	cleanup, _, cleanupErr := decodeReconciliationJSON[SalesforceOrgCleanup](packetFiles[cleanupPath].Data)
	if cleanupErr != nil || replayBytesSHA256(packetFiles[cleanupPath].Data) != ssh.OrgCleanupSHA256 {
		return result, fmt.Errorf("retained cleanup does not bind the SSH receipt")
	}
	result = OrchestratorRawCanaryReceipt{SchemaVersion: 1, Status: "validated-zero-credit", ProofCredit: 0, CleanupClosed: true, CampaignID: request.Lease.CampaignID, JobID: request.Lease.JobID, ShardIndex: request.Lease.ShardIndex, Generation: request.Lease.Generation, Candidate: request.Plan.Definition.Candidate, Tools: request.Plan.Definition.Tools, SpecSHA256: request.Plan.SpecSHA256, PlanSHA256: request.PlanSHA256, LeaseSHA256: request.LeaseSHA256, SSHReceiptSHA256: request.SSHReceiptSHA256, ReconciliationSHA256: replayBytesSHA256(reconciliationBytes), PacketManifestSHA256: reconciliation.PacketManifestSHA256, OraclePlanSHA256: reconciliation.OraclePlanSHA256, BundleSHA256: reconciliation.BundleSHA256, OrchestratorBindingSHA256: ssh.OrchestratorBindingSHA256, SalesforceShardSHA256: ssh.SalesforceShardSHA256, OrgCleanupSHA256: ssh.OrgCleanupSHA256}
	if outputExists {
		snapshot, readErr := readRegularFileSnapshot(request.OutputPath)
		var existing OrchestratorRawCanaryReceipt
		if readErr != nil {
			return OrchestratorRawCanaryReceipt{}, fmt.Errorf("read existing raw canary output: %w", readErr)
		}
		if snapshot.Mode.Perm() != 0o600 || decodeExactJSON(snapshot.Data, &existing) != nil {
			return OrchestratorRawCanaryReceipt{}, fmt.Errorf("existing raw canary output is invalid")
		}
		if existing != result {
			return OrchestratorRawCanaryReceipt{}, fmt.Errorf("existing raw canary output does not match request")
		}
	} else {
		if err := preflightOrchestratorSSHReceiptPath(request.OutputPath); err != nil {
			return result, fmt.Errorf("preflight raw canary output: %w", err)
		}
	}
	var cleanupState, allocationState string
	if err := request.Coordinator.db.QueryRow(`SELECT c.state, a.state FROM cleanup_journal c JOIN scratch_allocations a ON a.allocation_alias = c.allocation_alias WHERE c.allocation_alias = ? AND c.campaign_id = ? AND c.job_id = ? AND c.generation = ?`, request.AllocationAlias, request.Lease.CampaignID, request.Lease.JobID, request.Lease.Generation).Scan(&cleanupState, &allocationState); err != nil {
		return OrchestratorRawCanaryReceipt{}, fmt.Errorf("read raw canary cleanup: %w", err)
	}
	if cleanupState == "closed" && allocationState == "closed" {
		if err := request.Coordinator.closeRawAcceptanceJob(request.Lease); err != nil {
			return OrchestratorRawCanaryReceipt{}, fmt.Errorf("close raw canary job: %w", err)
		}
		if !outputExists {
			if err := WriteNewJSON(request.OutputPath, result); err != nil {
				return OrchestratorRawCanaryReceipt{}, err
			}
		}
		return result, nil
	}
	if cleanupState != "pending" || allocationState != "reserved" {
		return OrchestratorRawCanaryReceipt{}, fmt.Errorf("raw canary cleanup is not claimable")
	}
	now := time.Now().UTC()
	if err := request.Coordinator.closeRawAcceptanceCleanup(request.Lease, request.AllocationAlias, now, !cleanup.RecoveredAbsent); err != nil {
		return result, fmt.Errorf("close raw canary cleanup: %w", err)
	}
	if err := request.Coordinator.closeRawAcceptanceJob(request.Lease); err != nil {
		return result, fmt.Errorf("close raw canary job: %w", err)
	}
	if !outputExists {
		if err := WriteNewJSON(request.OutputPath, result); err != nil {
			return OrchestratorRawCanaryReceipt{}, err
		}
	}
	return result, nil
}

func (o *Orchestrator) closeRawAcceptanceJob(lease OrchestratorLease) error {
	tx, err := o.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE jobs SET status = 'closed' WHERE campaign_id = ? AND id = ? AND generation = ? AND leased_by = ? AND status = 'running'`, lease.CampaignID, lease.JobID, lease.Generation, lease.Worker); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE attempts SET status = 'closed' WHERE campaign_id = ? AND job_id = ? AND generation = ? AND worker = ? AND status = 'running'`, lease.CampaignID, lease.JobID, lease.Generation, lease.Worker); err != nil {
		return err
	}
	var jobStatus, attemptStatus string
	if err := tx.QueryRow(`SELECT status FROM jobs WHERE campaign_id = ? AND id = ? AND generation = ? AND leased_by = ?`, lease.CampaignID, lease.JobID, lease.Generation, lease.Worker).Scan(&jobStatus); err != nil {
		return fmt.Errorf("read closed raw canary job: %w", err)
	}
	if err := tx.QueryRow(`SELECT status FROM attempts WHERE campaign_id = ? AND job_id = ? AND generation = ? AND worker = ?`, lease.CampaignID, lease.JobID, lease.Generation, lease.Worker).Scan(&attemptStatus); err != nil {
		return fmt.Errorf("read closed raw canary attempt: %w", err)
	}
	if jobStatus != "closed" || attemptStatus != "closed" {
		return fmt.Errorf("raw canary job closure is not terminal")
	}
	return tx.Commit()
}

func RunRawSalesforceShard(request RawSalesforceShardRequest) (result RawSalesforceShardResult, err error) {
	return runRawSalesforceShardAt(request, func() time.Time { return time.Now().UTC() })
}

func runRawSalesforceShardAt(request RawSalesforceShardRequest, clock func() time.Time) (result RawSalesforceShardResult, err error) {
	if clock == nil {
		return result, fmt.Errorf("raw Salesforce shard clock is required")
	}
	if err := validateRawSalesforceShardRequestAt(request, clock().UTC()); err != nil {
		return result, err
	}
	validateBundle := request.validateBundle
	if validateBundle == nil {
		validateBundle = ValidateOracleBundle
	}
	if err := validateBundle(request.BundlePath); err != nil {
		return result, fmt.Errorf("validate staged bundle: %w", err)
	}
	bundle, _, err := readExactJSONBytes[OracleBundle](request.BundlePath)
	if err != nil {
		return result, fmt.Errorf("read staged bundle: %w", err)
	}
	if bundle.DevHub != request.DevHub {
		return result, fmt.Errorf("reserved Dev Hub does not match sealed bundle")
	}
	if err := validateRawSalesforceScope(filepath.Dir(request.BundlePath), request.Plan, request.Lease); err != nil {
		return result, err
	}
	executorRoot, runID, err := sealedSalesforceDispatchLayout(request.BundlePath, bundle.AttemptSHA256, request.Lease.ShardIndex)
	if err != nil {
		return result, err
	}
	shardCount := len(request.Plan.Jobs)
	if shardCount < 1 {
		return result, fmt.Errorf("raw Salesforce shard count must be positive")
	}
	if err := os.Mkdir(request.OutputRoot, 0o700); err != nil {
		return result, err
	}
	if err := os.Chmod(request.OutputRoot, 0o700); err != nil {
		return result, err
	}
	paths := rawSalesforceShardPaths(request.OutputRoot)
	result.BindingPath = paths.binding
	result.SalesforceShardFiles = SalesforceShardFiles{ShardPath: paths.shard, DispatchPath: paths.dispatch, CreationPath: paths.creation, CleanupPath: paths.cleanup, PreflightPath: paths.preflight}
	result.Binding, err = WriteOrchestratorBatchBinding(paths.binding, request.Plan, request.Lease)
	if err != nil {
		return result, err
	}

	create := request.orgCreate
	if create == nil {
		create = RunSalesforceOrgCreate
	}
	preflight := request.orgPreflight
	if preflight == nil {
		preflight = RunSalesforceOrgPreflight
	}
	dispatch := request.dispatch
	if dispatch == nil {
		dispatch = CreateSalesforceDispatch
	}
	shard := request.shard
	if shard == nil {
		shard = RunSalesforceShard
	}
	cleanup := request.orgCleanup
	if cleanup == nil {
		cleanup = RunSalesforceOrgCleanup
	}
	cleanupRequest := SalesforceOrgCleanupRequest{BundlePath: request.BundlePath, CreationPath: paths.creation, TargetOrg: request.TargetOrg, DevHub: bundle.DevHub, SFBin: request.SFBin, OutputPath: paths.cleanup}
	cleanupCalled := false
	runCleanup := func() error {
		if cleanupCalled {
			return nil
		}
		cleanupCalled = true
		var cleanupErr error
		result.Cleanup, cleanupErr = cleanup(cleanupRequest)
		return cleanupErr
	}
	cleanupEligible := false
	defer func() {
		if err != nil && cleanupEligible {
			if cleanupErr := runCleanup(); cleanupErr != nil {
				err = errors.Join(err, cleanupErr)
			}
		}
	}()

	result.Creation, err = create(SalesforceOrgCreateRequest{BundlePath: request.BundlePath, DevHub: bundle.DevHub, Alias: request.TargetOrg, SFBin: request.SFBin, OutputPath: paths.creation, validateBundle: validateBundle})
	cleanupEligible = rawSalesforceCreationArtifactExists(paths.creation)
	if err != nil {
		return result, err
	}
	if !cleanupEligible {
		return result, fmt.Errorf("Salesforce org creation lacks a cleanup authority artifact")
	}
	result.Preflight, err = preflight(SalesforceOrgPreflightRequest{BundlePath: request.BundlePath, TargetOrg: request.TargetOrg, SFBin: request.SFBin, OutputPath: paths.preflight, validateBundle: validateBundle})
	if err != nil {
		return result, err
	}
	cleanupRequest.PreflightPath = paths.preflight
	result.Dispatch, err = dispatch(SalesforceDispatchRequest{BundlePath: request.BundlePath, OrgAlias: request.TargetOrg, ExecutorRoot: executorRoot, RunID: runID, ShardIndex: request.Lease.ShardIndex, ShardCount: shardCount, OutputPath: paths.dispatch, approvedFilterSHA256: approvedSalesforceFilterSHA256})
	if err != nil {
		return result, err
	}
	result.Shard, err = shard(SalesforceShardRequest{BundlePath: request.BundlePath, DispatchPath: paths.dispatch, PreflightPath: paths.preflight, TargetOrg: request.TargetOrg, SFBin: request.SFBin, ExecutorRoot: executorRoot, RunID: runID, ShardIndex: request.Lease.ShardIndex, ShardCount: shardCount, OutputPath: paths.shard, validateBundle: validateBundle, approvedFilterSHA256: approvedSalesforceFilterSHA256})
	if err != nil {
		return result, err
	}
	if err := runCleanup(); err != nil {
		return result, err
	}
	return result, nil
}

type rawSalesforceShardPathSet struct {
	binding, creation, preflight, dispatch, shard, cleanup string
}

func rawSalesforceShardPaths(root string) rawSalesforceShardPathSet {
	return rawSalesforceShardPathSet{
		binding:   filepath.Join(root, "ORCHESTRATOR_BINDING.json"),
		creation:  filepath.Join(root, "ORG_CREATION.json"),
		preflight: filepath.Join(root, "ORG_PREFLIGHT.json"),
		dispatch:  filepath.Join(root, "SALESFORCE_DISPATCH.json"),
		shard:     filepath.Join(root, "SALESFORCE_SHARD.json"),
		cleanup:   filepath.Join(root, "ORG_CLEANUP.json"),
	}
}

func validateRawSalesforceShardRequest(request RawSalesforceShardRequest) error {
	return validateRawSalesforceShardRequestAt(request, time.Now().UTC())
}

func validateRawSalesforceShardRequestAt(request RawSalesforceShardRequest, now time.Time) error {
	if !filepath.IsAbs(request.BundlePath) || filepath.Clean(request.BundlePath) != request.BundlePath || !filepath.IsAbs(request.SFBin) || filepath.Clean(request.SFBin) != request.SFBin || !filepath.IsAbs(request.OutputRoot) || filepath.Clean(request.OutputRoot) != request.OutputRoot || !safeOrchestratorToken(request.DevHub) || !safeOrchestratorToken(request.TargetOrg) {
		return fmt.Errorf("invalid raw Salesforce shard request")
	}
	if err := validateOrchestratorWorkerPlanLease(request.Plan, request.Lease); err != nil {
		return err
	}
	if err := validateOrchestratorLiveWorkerLease(request.Lease, now); err != nil {
		return err
	}
	if request.Lease.LeaseUntil.Before(now.Add(orchestratorWorkerOnceTimeout)) {
		return fmt.Errorf("raw Salesforce shard lease does not cover bounded lifecycle")
	}
	return nil
}

func validateOrchestratorLiveWorkerLease(lease OrchestratorLease, now time.Time) error {
	if !safeOrchestratorToken(lease.Worker) || lease.LeaseUntil.IsZero() || !lease.LeaseUntil.UTC().After(now.UTC()) {
		return fmt.Errorf("raw Salesforce shard requires a safe, live worker lease")
	}
	return nil
}

// OrchestratorWorkerOnceCompletionFromRaw seals only safe lifecycle hashes
// after the raw worker has returned. It never includes paths or private data.
func OrchestratorWorkerOnceCompletionFromRaw(plan OrchestratorCampaignPlan, lease OrchestratorLease, planSHA256, leaseSHA256 string, result RawSalesforceShardResult) (OrchestratorWorkerOnceCompletion, error) {
	if err := validateOrchestratorWorkerPlanLease(plan, lease); err != nil || !safeOrchestratorToken(lease.Worker) || !sha256Pattern.MatchString(planSHA256) || !sha256Pattern.MatchString(leaseSHA256) {
		return OrchestratorWorkerOnceCompletion{}, fmt.Errorf("invalid worker completion binding")
	}
	bindingSHA, bindingErr := sha256File(result.BindingPath)
	shardSHA, shardErr := sha256File(result.SalesforceShardFiles.ShardPath)
	cleanupSHA, cleanupErr := sha256File(result.SalesforceShardFiles.CleanupPath)
	if bindingErr != nil || shardErr != nil || cleanupErr != nil || !sha256Pattern.MatchString(bindingSHA) || !sha256Pattern.MatchString(shardSHA) || !sha256Pattern.MatchString(cleanupSHA) {
		return OrchestratorWorkerOnceCompletion{}, fmt.Errorf("worker lifecycle artifacts are incomplete")
	}
	return OrchestratorWorkerOnceCompletion{CampaignID: plan.CampaignID, JobID: lease.JobID, ShardIndex: lease.ShardIndex, Generation: lease.Generation, Status: "worker-complete", SpecSHA256: plan.SpecSHA256, PlanSHA256: planSHA256, LeaseSHA256: leaseSHA256, OrchestratorBindingSHA256: bindingSHA, SalesforceShardSHA256: shardSHA, OrgCleanupSHA256: cleanupSHA}, nil
}

func validateRawSalesforceScope(bundleRoot string, plan OrchestratorCampaignPlan, lease OrchestratorLease) error {
	oraclePlan, data, err := readExactJSONBytes[OraclePlan](filepath.Join(bundleRoot, "ORACLE_PLAN.json"))
	if err != nil || replayBytesSHA256(data) != plan.Definition.ControlledInputSHA256["oracle-plan"] {
		return fmt.Errorf("sealed oracle plan binding drift")
	}
	if oraclePlan.Candidate.Commit != plan.Definition.Candidate.Commit || oraclePlan.Candidate.SHA256 != plan.Definition.Candidate.SHA256 || oraclePlan.Tools.Commit != plan.Definition.Tools.Commit || oraclePlan.Tools.SHA256 != plan.Definition.Tools.SHA256 {
		return fmt.Errorf("sealed oracle plan candidate binding drift")
	}
	expected, err := orchestratorSalesforceExpectedSurfaceIDs(oraclePlan, plan, lease)
	if err != nil {
		return err
	}
	manifest, _, err := readExactJSONBytes[oracleTransportManifest](filepath.Join(bundleRoot, "fixture-manifest.json"))
	if err != nil {
		return fmt.Errorf("read sealed Salesforce transport manifest: %w", err)
	}
	selected := []string{}
	for index, fixture := range manifest.Fixtures {
		if index%len(plan.Jobs) == lease.ShardIndex {
			selected = append(selected, fixture.SurfaceIDs...)
		}
	}
	if !equalStrings(sortedStrings(selected), sortedStrings(expected)) {
		return fmt.Errorf("orchestrator lease does not match the sealed Salesforce filter partition")
	}
	return nil
}

func rawSalesforceCreationArtifactExists(path string) bool {
	for _, candidate := range []string{path, path + ".reservation", path + ".invalidated"} {
		if info, err := os.Lstat(candidate); err == nil && info.Mode().IsRegular() {
			return true
		}
	}
	return false
}
