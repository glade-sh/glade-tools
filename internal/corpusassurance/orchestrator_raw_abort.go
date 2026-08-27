package corpusassurance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"time"
)

// OrchestratorRawPrecreationAbortObservationRequest is the worker-side input
// for an abort before Salesforce creates a scratch org. Private aliases and
// paths are accepted as inputs but never appear in the observation.
type OrchestratorRawPrecreationAbortObservationRequest struct {
	Plan                   OrchestratorCampaignPlan
	Lease                  OrchestratorLease
	PlanSHA256             string
	LeaseSHA256            string
	FailedSSHReceipt       OrchestratorSSHDispatchReceipt
	FailedSSHReceiptSHA256 string
	BundlePath             string
	ScopePath              string
	RawRoot                string
	AllocationAlias        string
	TargetOrg              string
	SFBin                  string
	OutputPath             string
	validateBundle         func(string) error
	runner                 salesforceCommandRunner
	recoveryTool           func() (string, error)
}

// OrchestratorRawPrecreationAbortObservation is the canonical, zero-credit
// worker result. It intentionally contains hashes and bindings only.
type OrchestratorRawPrecreationAbortObservation struct {
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
	FailedSSHReceiptSHA256    string               `json:"failedSshReceiptSha256"`
	OrchestratorBindingSHA256 string               `json:"orchestratorBindingSha256"`
	BundleSHA256              string               `json:"bundleSha256"`
	AllocationSHA256          string               `json:"allocationSha256"`
	MissingAliasSHA256        string               `json:"missingAliasSha256"`
	RecoveryToolSHA256        string               `json:"recoveryToolSha256"`
}

// OrchestratorRawPrecreationAbortAcceptanceRequest is the coordinator-side
// input for accepting a validated observation and closing only its allocation.
type OrchestratorRawPrecreationAbortAcceptanceRequest struct {
	Coordinator            *Orchestrator
	Plan                   OrchestratorCampaignPlan
	Lease                  OrchestratorLease
	PlanSHA256             string
	LeaseSHA256            string
	FailedSSHReceipt       OrchestratorSSHDispatchReceipt
	FailedSSHReceiptSHA256 string
	AllocationAlias        string
	executingTool          func() (string, error)
	ObservationPath        string
	ObservationSHA256      string
	OutputPath             string
}

// OrchestratorRawPrecreationAbortReceipt is the accepted public receipt.
// The same shape is used for replay, keeping the acceptance idempotent.
type OrchestratorRawPrecreationAbortReceipt struct {
	OrchestratorRawPrecreationAbortObservation
	CleanupClosed bool `json:"cleanupClosed"`
}

func ObserveOrchestratorRawPrecreationAbort(request OrchestratorRawPrecreationAbortObservationRequest) (OrchestratorRawPrecreationAbortObservation, error) {
	if err := validateRawPrecreationAbortRequest(request); err != nil {
		return OrchestratorRawPrecreationAbortObservation{}, err
	}
	scopePath := rawAbortScopePath(request)
	if err := validateOrchestratorWorkerPlanLeaseAtScope(request.Plan, request.Lease, scopePath); err != nil {
		return OrchestratorRawPrecreationAbortObservation{}, err
	}
	planSHA, leaseSHA, err := canonicalPlanLeaseHashes(request.Plan, request.Lease)
	if err != nil || planSHA != request.PlanSHA256 || leaseSHA != request.LeaseSHA256 {
		return OrchestratorRawPrecreationAbortObservation{}, fmt.Errorf("raw abort plan and lease bytes do not match typed bindings")
	}
	sshSHA, err := canonicalJSONHash(request.FailedSSHReceipt)
	if err != nil || sshSHA != request.FailedSSHReceiptSHA256 || !validFailedRawAbortSSHReceipt(request.FailedSSHReceipt, request.Plan, request.Lease) {
		return OrchestratorRawPrecreationAbortObservation{}, fmt.Errorf("failed SSH receipt is not a valid sanitized abort receipt")
	}
	if err := ensureRawAbortRoot(request.RawRoot, request.Plan, request.Lease, scopePath); err != nil {
		return OrchestratorRawPrecreationAbortObservation{}, err
	}
	bundleValidator := request.validateBundle
	if bundleValidator == nil {
		bundleValidator = ValidateOracleBundle
	}
	if err := bundleValidator(request.BundlePath); err != nil {
		return OrchestratorRawPrecreationAbortObservation{}, fmt.Errorf("validate staged bundle: %w", err)
	}
	bundle, bundleBytes, err := readExactJSONBytes[OracleBundle](request.BundlePath)
	if err != nil || bundle.Candidate.Commit != request.Plan.Definition.Candidate.Commit || bundle.Candidate.SHA256 != request.Plan.Definition.Candidate.SHA256 || bundle.Tools.Commit != request.Plan.Definition.Tools.Commit || bundle.Tools.SHA256 != request.Plan.Definition.Tools.SHA256 || bundle.SalesforceExecution.SFBinary != request.SFBin {
		return OrchestratorRawPrecreationAbortObservation{}, fmt.Errorf("staged bundle does not match campaign or sealed Salesforce path")
	}
	if err := validateDevHubExecutable(request.SFBin); err != nil {
		return OrchestratorRawPrecreationAbortObservation{}, fmt.Errorf("validate recovery tool: %w", err)
	}
	sfSHA, err := sha256File(request.SFBin)
	if err != nil || sfSHA != bundle.SalesforceExecution.SFSHA256 {
		return OrchestratorRawPrecreationAbortObservation{}, fmt.Errorf("recovery tool changed from sealed Salesforce executable")
	}
	recoveryTool := request.recoveryTool
	if recoveryTool == nil {
		recoveryTool = os.Executable
	}
	recoveryPath, err := recoveryTool()
	if err != nil || !cleanAbsolutePath(recoveryPath) {
		return OrchestratorRawPrecreationAbortObservation{}, fmt.Errorf("executing recovery tool is unavailable")
	}
	if err := validateDevHubExecutable(recoveryPath); err != nil {
		return OrchestratorRawPrecreationAbortObservation{}, fmt.Errorf("validate executing recovery tool: %w", err)
	}
	recoverySHA, err := sha256File(recoveryPath)
	if err != nil {
		return OrchestratorRawPrecreationAbortObservation{}, fmt.Errorf("hash executing recovery tool: %w", err)
	}
	runner := request.runner
	if runner == nil {
		runner = runSalesforceCLI
	}
	_, missingAlias, err := runSalesforceExpectedCommand(runner, bundle.SalesforceExecution, filepath.Dir(request.BundlePath), false, "org", "display", "--target-org", request.TargetOrg, "--json")
	if err != nil || !validSalesforceReservedAliasAbsence(missingAlias, request.BundlePath, request.TargetOrg) {
		return OrchestratorRawPrecreationAbortObservation{}, fmt.Errorf("current Salesforce alias absence is not validated")
	}
	if err := validateRawAbortRootAtScope(request.RawRoot, request.Plan, request.Lease, scopePath); err != nil {
		return OrchestratorRawPrecreationAbortObservation{}, err
	}
	missingAlias.DurationMS = 0
	missingSHA, err := canonicalJSONHash(missingAlias)
	if err != nil {
		return OrchestratorRawPrecreationAbortObservation{}, err
	}
	bindingPath := filepath.Join(request.RawRoot, "ORCHESTRATOR_BINDING.json")
	bindingBytes, err := os.ReadFile(bindingPath)
	if err != nil {
		return OrchestratorRawPrecreationAbortObservation{}, err
	}
	bindingSHA := replayBytesSHA256(bindingBytes)
	bundleSHA := replayBytesSHA256(bundleBytes)
	result := OrchestratorRawPrecreationAbortObservation{
		SchemaVersion: 1, Status: "validated-zero-credit", ProofCredit: 0,
		CampaignID: request.Lease.CampaignID, JobID: request.Lease.JobID, ShardIndex: request.Lease.ShardIndex, Generation: request.Lease.Generation,
		Candidate: request.Plan.Definition.Candidate, Tools: request.Plan.Definition.Tools,
		SpecSHA256: request.Plan.SpecSHA256, PlanSHA256: request.PlanSHA256, LeaseSHA256: request.LeaseSHA256,
		FailedSSHReceiptSHA256: request.FailedSSHReceiptSHA256, OrchestratorBindingSHA256: bindingSHA, BundleSHA256: bundleSHA,
		AllocationSHA256: replayBytesSHA256([]byte(request.AllocationAlias)), MissingAliasSHA256: missingSHA, RecoveryToolSHA256: recoverySHA,
	}
	return writeOrReplayRawAbortObservation(request.OutputPath, result)
}

func AcceptOrchestratorRawPrecreationAbort(request OrchestratorRawPrecreationAbortAcceptanceRequest) (OrchestratorRawPrecreationAbortReceipt, error) {
	if request.Coordinator == nil || request.Coordinator.db == nil || !cleanAbsolutePath(request.ObservationPath) || !cleanAbsolutePath(request.OutputPath) || !safeOrchestratorToken(request.AllocationAlias) {
		return OrchestratorRawPrecreationAbortReceipt{}, fmt.Errorf("invalid raw abort acceptance request")
	}
	if err := validateOrchestratorWorkerPlanLease(request.Plan, request.Lease); err != nil {
		return OrchestratorRawPrecreationAbortReceipt{}, err
	}
	planSHA, leaseSHA, err := canonicalPlanLeaseHashes(request.Plan, request.Lease)
	if err != nil || planSHA != request.PlanSHA256 || leaseSHA != request.LeaseSHA256 {
		return OrchestratorRawPrecreationAbortReceipt{}, fmt.Errorf("raw abort plan and lease hashes do not match")
	}
	sshSHA, err := canonicalJSONHash(request.FailedSSHReceipt)
	if err != nil || sshSHA != request.FailedSSHReceiptSHA256 || !validFailedRawAbortSSHReceipt(request.FailedSSHReceipt, request.Plan, request.Lease) {
		return OrchestratorRawPrecreationAbortReceipt{}, fmt.Errorf("raw abort failed SSH receipt does not match")
	}
	observation, observationBytes, err := readExactJSONBytes[OrchestratorRawPrecreationAbortObservation](request.ObservationPath)
	if err != nil {
		return OrchestratorRawPrecreationAbortReceipt{}, fmt.Errorf("read raw abort observation: %w", err)
	}
	if info, statErr := os.Lstat(request.ObservationPath); statErr != nil || info.Mode().Perm() != 0o600 || info.Mode()&os.ModeSymlink != 0 || replayBytesSHA256(observationBytes) != request.ObservationSHA256 {
		return OrchestratorRawPrecreationAbortReceipt{}, fmt.Errorf("raw abort observation is not sealed")
	}
	if err := validateRawAbortObservation(observation, request.Plan, request.Lease, request.PlanSHA256, request.LeaseSHA256, request.FailedSSHReceiptSHA256, request.AllocationAlias); err != nil {
		return OrchestratorRawPrecreationAbortReceipt{}, err
	}
	executingTool := request.executingTool
	if executingTool == nil {
		executingTool = os.Executable
	}
	executingPath, err := executingTool()
	if err != nil || !cleanAbsolutePath(executingPath) {
		return OrchestratorRawPrecreationAbortReceipt{}, fmt.Errorf("executing recovery tool is unavailable")
	}
	executingSHA, err := sha256File(executingPath)
	if err != nil || executingSHA != observation.RecoveryToolSHA256 {
		return OrchestratorRawPrecreationAbortReceipt{}, fmt.Errorf("executing recovery tool hash does not match observation")
	}
	receipt := OrchestratorRawPrecreationAbortReceipt{OrchestratorRawPrecreationAbortObservation: observation, CleanupClosed: true}
	outputExists := false
	if info, err := os.Lstat(request.OutputPath); err == nil {
		if info.Mode().Perm() != 0o600 || info.Mode()&os.ModeSymlink != 0 {
			return OrchestratorRawPrecreationAbortReceipt{}, fmt.Errorf("existing raw abort output is invalid")
		}
		data, readErr := os.ReadFile(request.OutputPath)
		var existing OrchestratorRawPrecreationAbortReceipt
		if readErr != nil || decodeExactJSON(data, &existing) != nil || !reflect.DeepEqual(existing, receipt) {
			return OrchestratorRawPrecreationAbortReceipt{}, fmt.Errorf("existing raw abort output does not match request")
		}
		outputExists = true
	} else if !os.IsNotExist(err) {
		return OrchestratorRawPrecreationAbortReceipt{}, err
	}
	if !outputExists {
		if err := preflightOrchestratorSSHReceiptPath(request.OutputPath); err != nil {
			return OrchestratorRawPrecreationAbortReceipt{}, err
		}
	}
	var cleanupState, allocationState string
	var claimUntil *int64
	if err := request.Coordinator.db.QueryRow(`SELECT c.state, a.state, c.claim_until FROM cleanup_journal c JOIN scratch_allocations a ON a.allocation_alias = c.allocation_alias WHERE c.allocation_alias = ? AND c.campaign_id = ? AND c.job_id = ? AND c.generation = ?`, request.AllocationAlias, request.Lease.CampaignID, request.Lease.JobID, request.Lease.Generation).Scan(&cleanupState, &allocationState, &claimUntil); err != nil {
		return OrchestratorRawPrecreationAbortReceipt{}, err
	}
	if cleanupState == "closed" && allocationState == "closed" {
		if !outputExists {
			if err := WriteNewJSON(request.OutputPath, receipt); err != nil {
				return OrchestratorRawPrecreationAbortReceipt{}, err
			}
		}
		return receipt, nil
	}
	now := time.Now().UTC()
	if (cleanupState != "pending" && (cleanupState != "running" || claimUntil == nil || *claimUntil > now.UnixMilli())) || allocationState != "reserved" {
		return OrchestratorRawPrecreationAbortReceipt{}, fmt.Errorf("raw abort cleanup is not closable")
	}
	if err := request.Coordinator.closeRawAcceptanceCleanup(request.Lease, request.AllocationAlias, now, false); err != nil {
		return OrchestratorRawPrecreationAbortReceipt{}, fmt.Errorf("close raw abort cleanup: %w", err)
	}
	if !outputExists {
		if err := WriteNewJSON(request.OutputPath, receipt); err != nil {
			return OrchestratorRawPrecreationAbortReceipt{}, err
		}
	}
	return receipt, nil
}

func validateRawPrecreationAbortRequest(request OrchestratorRawPrecreationAbortObservationRequest) error {
	if !cleanAbsolutePath(request.BundlePath) || !cleanAbsolutePath(rawAbortScopePath(request)) || !cleanAbsolutePath(request.RawRoot) || !cleanAbsolutePath(request.SFBin) || !cleanAbsolutePath(request.OutputPath) || !safeOrchestratorToken(request.AllocationAlias) || request.TargetOrg != request.AllocationAlias || !sha256Pattern.MatchString(request.PlanSHA256) || !sha256Pattern.MatchString(request.LeaseSHA256) || !sha256Pattern.MatchString(request.FailedSSHReceiptSHA256) {
		return fmt.Errorf("invalid raw precreation abort request")
	}
	return nil
}

func rawAbortScopePath(request OrchestratorRawPrecreationAbortObservationRequest) string {
	if request.ScopePath != "" {
		return request.ScopePath
	}
	return request.Plan.Definition.ScopePath
}

func ensureRawAbortRoot(root string, plan OrchestratorCampaignPlan, lease OrchestratorLease, scopePath string) error {
	if _, err := os.Lstat(root); err == nil {
		return validateRawAbortRootAtScope(root, plan, lease, scopePath)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect raw abort root: %w", err)
	}
	if err := os.Mkdir(root, 0o700); err != nil {
		return fmt.Errorf("create raw abort root: %w", err)
	}
	if _, err := writeOrchestratorBatchBindingAtScope(filepath.Join(root, "ORCHESTRATOR_BINDING.json"), plan, lease, scopePath); err != nil {
		_ = os.Remove(root)
		return fmt.Errorf("seal raw abort binding: %w", err)
	}
	return validateRawAbortRootAtScope(root, plan, lease, scopePath)
}

func validateRawAbortRoot(root string, plan OrchestratorCampaignPlan, lease OrchestratorLease) error {
	return validateRawAbortRootAtScope(root, plan, lease, plan.Definition.ScopePath)
}

func validateRawAbortRootAtScope(root string, plan OrchestratorCampaignPlan, lease OrchestratorLease, scopePath string) error {
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 {
		return fmt.Errorf("raw abort root must be an exact mode-0700 directory")
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 || entries[0].Name() != "ORCHESTRATOR_BINDING.json" {
		return fmt.Errorf("raw abort root must contain only ORCHESTRATOR_BINDING.json")
	}
	bindingInfo, err := os.Lstat(filepath.Join(root, entries[0].Name()))
	if err != nil || !bindingInfo.Mode().IsRegular() || bindingInfo.Mode()&os.ModeSymlink != 0 || bindingInfo.Mode().Perm() != 0o400 {
		return fmt.Errorf("raw abort binding is not a sealed regular file")
	}
	binding, _, err := readExactJSONBytes[OrchestratorBatchBinding](filepath.Join(root, entries[0].Name()))
	if err != nil {
		return fmt.Errorf("read raw abort binding: %w", err)
	}
	want, err := orchestratorBatchBindingValueAtScope(plan, lease, scopePath)
	if err != nil || !reflect.DeepEqual(binding, want) {
		return fmt.Errorf("raw abort binding does not match plan and lease")
	}
	return nil
}

func orchestratorBatchBindingValue(plan OrchestratorCampaignPlan, lease OrchestratorLease) (OrchestratorBatchBinding, error) {
	return orchestratorBatchBindingValueAtScope(plan, lease, plan.Definition.ScopePath)
}

func orchestratorBatchBindingValueAtScope(plan OrchestratorCampaignPlan, lease OrchestratorLease, scopePath string) (OrchestratorBatchBinding, error) {
	if err := validateOrchestratorPlanAtScope(plan, scopePath); err != nil {
		return OrchestratorBatchBinding{}, err
	}
	for _, job := range plan.Jobs {
		if job.ID == lease.JobID && lease.CampaignID == plan.CampaignID && lease.Kind == job.Kind && lease.ShardIndex == job.ShardIndex && reflect.DeepEqual(lease.SurfaceIDs, job.SurfaceIDs) && lease.Generation >= 1 {
			return OrchestratorBatchBinding{SchemaVersion: 1, CampaignID: plan.CampaignID, SpecSHA256: plan.SpecSHA256, Candidate: plan.Definition.Candidate, Tools: plan.Definition.Tools, ScopeSHA256: plan.Definition.ScopeSHA256, ControlledInputSHA256: plan.Definition.ControlledInputSHA256, JobID: job.ID, JobKind: job.Kind, Generation: lease.Generation, ShardIndex: job.ShardIndex, SurfaceIDs: lease.SurfaceIDs}, nil
		}
	}
	return OrchestratorBatchBinding{}, fmt.Errorf("lease does not match immutable campaign job")
}

func validFailedRawAbortSSHReceipt(receipt OrchestratorSSHDispatchReceipt, plan OrchestratorCampaignPlan, lease OrchestratorLease) bool {
	return receipt.SchemaVersion == 1 && !receipt.Passed && receipt.Status == "failed" && receipt.FailureCode == orchestratorSSHDispatchFailed && !receipt.TimedOut && receipt.ExitCode == 1 && receipt.ActionRequired && receipt.ActionCode == orchestratorSSHActionCode && receipt.OrchestratorBindingSHA256 == "" && receipt.SalesforceShardSHA256 == "" && receipt.OrgCleanupSHA256 == "" && receipt.CampaignID == lease.CampaignID && receipt.JobID == lease.JobID && receipt.ShardIndex == lease.ShardIndex && receipt.Generation == lease.Generation && receipt.SpecSHA256 == plan.SpecSHA256 && receipt.PlanSHA256 == planSHA256For(plan) && receipt.LeaseSHA256 == leaseSHA256For(lease) && sha256Pattern.MatchString(receipt.CommandSHA256) && sha256Pattern.MatchString(receipt.StdoutSHA256) && sha256Pattern.MatchString(receipt.StderrSHA256)
}

func validateRawAbortObservation(observation OrchestratorRawPrecreationAbortObservation, plan OrchestratorCampaignPlan, lease OrchestratorLease, planSHA, leaseSHA, sshSHA, allocation string) error {
	if observation.SchemaVersion != 1 || observation.Status != "validated-zero-credit" || observation.ProofCredit != 0 || observation.CampaignID != lease.CampaignID || observation.JobID != lease.JobID || observation.ShardIndex != lease.ShardIndex || observation.Generation != lease.Generation || observation.Candidate != plan.Definition.Candidate || observation.Tools != plan.Definition.Tools || observation.SpecSHA256 != plan.SpecSHA256 || observation.PlanSHA256 != planSHA || observation.LeaseSHA256 != leaseSHA || observation.FailedSSHReceiptSHA256 != sshSHA || observation.AllocationSHA256 != replayBytesSHA256([]byte(allocation)) {
		return fmt.Errorf("raw abort observation does not match accepted bindings")
	}
	for _, hash := range []string{observation.PlanSHA256, observation.LeaseSHA256, observation.FailedSSHReceiptSHA256, observation.OrchestratorBindingSHA256, observation.BundleSHA256, observation.AllocationSHA256, observation.MissingAliasSHA256, observation.RecoveryToolSHA256} {
		if !sha256Pattern.MatchString(hash) {
			return fmt.Errorf("raw abort observation contains an invalid hash")
		}
	}
	return nil
}

func canonicalPlanLeaseHashes(plan OrchestratorCampaignPlan, lease OrchestratorLease) (string, string, error) {
	planSHA, err := canonicalJSONHash(plan)
	if err != nil {
		return "", "", err
	}
	leaseSHA, err := canonicalJSONHash(lease)
	return planSHA, leaseSHA, err
}

func planSHA256For(plan OrchestratorCampaignPlan) string {
	hash, _ := canonicalJSONHash(plan)
	return hash
}
func leaseSHA256For(lease OrchestratorLease) string { hash, _ := canonicalJSONHash(lease); return hash }

func canonicalJSONHash(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return replayBytesSHA256(append(data, '\n')), nil
}

func writeOrReplayRawAbortObservation(path string, value OrchestratorRawPrecreationAbortObservation) (OrchestratorRawPrecreationAbortObservation, error) {
	if info, err := os.Lstat(path); err == nil {
		if info.Mode().Perm() != 0o600 || info.Mode()&os.ModeSymlink != 0 {
			return OrchestratorRawPrecreationAbortObservation{}, fmt.Errorf("existing raw abort observation is invalid")
		}
		data, readErr := os.ReadFile(path)
		var existing OrchestratorRawPrecreationAbortObservation
		if readErr != nil || decodeExactJSON(data, &existing) != nil || !reflect.DeepEqual(existing, value) {
			return OrchestratorRawPrecreationAbortObservation{}, fmt.Errorf("existing raw abort observation does not match request")
		}
		return existing, nil
	} else if !os.IsNotExist(err) {
		return OrchestratorRawPrecreationAbortObservation{}, err
	}
	if err := preflightOrchestratorSSHReceiptPath(path); err != nil {
		return OrchestratorRawPrecreationAbortObservation{}, err
	}
	if err := WriteNewJSON(path, value); err != nil {
		return OrchestratorRawPrecreationAbortObservation{}, err
	}
	return value, nil
}
