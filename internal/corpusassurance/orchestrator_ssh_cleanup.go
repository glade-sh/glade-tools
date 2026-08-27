package corpusassurance

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"
)

const (
	orchestratorSSHCleanupFailedCode           = "orchestrator-ssh-cleanup-failed"
	orchestratorSSHCleanupTimeoutCode          = "orchestrator-ssh-cleanup-timeout"
	orchestratorSSHCleanupTimeout              = 4 * time.Minute
	orchestratorSSHCleanupCloseMargin          = 10 * time.Second
	MinimumOrchestratorSSHCleanupClaimDuration = orchestratorSSHCleanupTimeout + orchestratorSSHCleanupCloseMargin
)

// OrchestratorWorkerCleanupReceipt is the sanitized worker-side result. It
// contains no org identity, path, host, command output, or proof credit.
type OrchestratorWorkerCleanupReceipt struct {
	SchemaVersion             int             `json:"schemaVersion"`
	Status                    string          `json:"status"`
	LifecycleStage            string          `json:"lifecycleStage"`
	CampaignID                string          `json:"campaignId"`
	JobID                     string          `json:"jobId"`
	ShardIndex                int             `json:"shardIndex"`
	Generation                int             `json:"generation"`
	SpecSHA256                string          `json:"specSha256"`
	PlanSHA256                string          `json:"planSha256"`
	LeaseSHA256               string          `json:"leaseSha256"`
	FailedSSHReceiptSHA256    string          `json:"failedSshReceiptSha256"`
	OrchestratorBindingSHA256 string          `json:"orchestratorBindingSha256"`
	LifecycleAuthoritySHA256  string          `json:"lifecycleAuthoritySha256"`
	OrgCleanupSHA256          string          `json:"orgCleanupSha256"`
	ResidueAbsent             bool            `json:"residueAbsent"`
	ProofCredit               int             `json:"proofCredit"`
	ExecutedTools             RuntimeArtifact `json:"executedTools"`
}

type OrchestratorWorkerCleanupRequest struct {
	Plan                   OrchestratorCampaignPlan
	Lease                  OrchestratorLease
	ScopePath              string
	PlanSHA256             string
	LeaseSHA256            string
	FailedSSHReceiptSHA256 string
	BundlePath             string
	DevHub                 string
	TargetOrg              string
	SFBin                  string
	OutputRoot             string
	ExecutedTools          RuntimeArtifact

	cleanup salesforceOrgCleanupRunner
}

// RunOrchestratorWorkerCleanup validates the worker-local lifecycle authority,
// runs the existing no-preflight cleanup, and seals a zero-credit receipt.
func RunOrchestratorWorkerCleanup(request OrchestratorWorkerCleanupRequest) (OrchestratorWorkerCleanupReceipt, error) {
	scopePath := request.ScopePath
	if scopePath == "" {
		scopePath = request.Plan.Definition.ScopePath
	}
	if err := validateOrchestratorWorkerPlanLeaseAtScope(request.Plan, request.Lease, scopePath); err != nil || !sha256Pattern.MatchString(request.PlanSHA256) || !sha256Pattern.MatchString(request.LeaseSHA256) || !sha256Pattern.MatchString(request.FailedSSHReceiptSHA256) || !safeOrchestratorToken(request.DevHub) || !safeOrchestratorToken(request.TargetOrg) || request.ExecutedTools == (RuntimeArtifact{}) || !validOrchestratorExecutedTools(request.ExecutedTools, request.Plan) {
		return OrchestratorWorkerCleanupReceipt{}, fmt.Errorf("invalid worker cleanup binding")
	}
	for _, path := range []string{request.BundlePath, request.SFBin, request.OutputRoot} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return OrchestratorWorkerCleanupReceipt{}, fmt.Errorf("absolute clean worker cleanup paths are required")
		}
	}
	release, err := acquireWorkerLifecycleLock(request.OutputRoot)
	if err != nil {
		return OrchestratorWorkerCleanupReceipt{}, err
	}
	defer release()
	stage, authoritySHA, bindingSHA, err := validateWorkerCleanupLifecycle(request)
	if err != nil {
		return OrchestratorWorkerCleanupReceipt{}, err
	}
	receiptPath := filepath.Join(request.OutputRoot, "WORKER_CLEANUP.json")
	cleanupPath := filepath.Join(request.OutputRoot, "ORG_CLEANUP.json")
	want := OrchestratorWorkerCleanupReceipt{
		SchemaVersion: 1, Status: "cleanup-closed", LifecycleStage: stage,
		CampaignID: request.Lease.CampaignID, JobID: request.Lease.JobID, ShardIndex: request.Lease.ShardIndex, Generation: request.Lease.Generation,
		SpecSHA256: request.Plan.SpecSHA256, PlanSHA256: request.PlanSHA256, LeaseSHA256: request.LeaseSHA256, FailedSSHReceiptSHA256: request.FailedSSHReceiptSHA256,
		OrchestratorBindingSHA256: bindingSHA, LifecycleAuthoritySHA256: authoritySHA,
		ResidueAbsent: true, ProofCredit: 0, ExecutedTools: request.ExecutedTools,
	}
	if _, err := os.Lstat(receiptPath); err == nil {
		receipt, _, readErr := readMode0600JSON[OrchestratorWorkerCleanupReceipt](receiptPath)
		cleanupInfo, statErr := os.Lstat(cleanupPath)
		cleanupSHA, hashErr := sha256File(cleanupPath)
		want.OrgCleanupSHA256 = cleanupSHA
		if readErr != nil || statErr != nil || !cleanupInfo.Mode().IsRegular() || cleanupInfo.Mode().Perm() != 0o600 || hashErr != nil || !reflect.DeepEqual(receipt, want) {
			return OrchestratorWorkerCleanupReceipt{}, fmt.Errorf("existing worker cleanup receipt is invalid")
		}
		return receipt, nil
	} else if !os.IsNotExist(err) {
		return OrchestratorWorkerCleanupReceipt{}, err
	}
	runner := request.cleanup
	if runner == nil {
		runner = RunSalesforceOrgCleanup
	}
	cleanup, err := existingWorkerSalesforceCleanup(request, cleanupPath)
	if os.IsNotExist(err) {
		cleanup, err = runner(SalesforceOrgCleanupRequest{BundlePath: request.BundlePath, CreationPath: filepath.Join(request.OutputRoot, "ORG_CREATION.json"), TargetOrg: request.TargetOrg, DevHub: request.DevHub, SFBin: request.SFBin, OutputPath: cleanupPath})
	}
	if err != nil || !cleanup.ResidueAbsent {
		return OrchestratorWorkerCleanupReceipt{}, fmt.Errorf("worker Salesforce cleanup failed")
	}
	if afterStage, afterAuthority, afterBinding, afterErr := validateWorkerCleanupLifecycle(request); afterErr != nil || afterStage != stage || afterAuthority != authoritySHA || afterBinding != bindingSHA {
		return OrchestratorWorkerCleanupReceipt{}, fmt.Errorf("worker cleanup lifecycle changed during cleanup")
	}
	want.OrgCleanupSHA256, err = sha256File(cleanupPath)
	if err != nil {
		return OrchestratorWorkerCleanupReceipt{}, err
	}
	if err := WriteNewJSON(receiptPath, want); err != nil {
		return OrchestratorWorkerCleanupReceipt{}, err
	}
	return want, nil
}

func existingWorkerSalesforceCleanup(request OrchestratorWorkerCleanupRequest, path string) (SalesforceOrgCleanup, error) {
	if _, err := os.Lstat(path); err != nil {
		return SalesforceOrgCleanup{}, err
	}
	cleanup, _, err := readMode0600JSON[SalesforceOrgCleanup](path)
	if err != nil {
		return SalesforceOrgCleanup{}, err
	}
	records := append([]CommandResult{cleanup.DevHubCommand}, cleanup.Commands...)
	index := 0
	runner := func(_ context.Context, binary string, args ...string) (salesforceCommandOutput, error) {
		if index >= len(records) || !reflect.DeepEqual(records[index].Command, append([]string{binary}, args...)) || records[index].Output == nil {
			return salesforceCommandOutput{}, fmt.Errorf("existing worker Salesforce cleanup command drift")
		}
		record := records[index]
		index++
		return salesforceCommandOutput{Stdout: record.Output.Stdout, Stderr: record.Output.Stderr, ExitCode: record.ExitCode}, nil
	}
	temporary, err := os.MkdirTemp(filepath.Dir(path), ".validate-cleanup-")
	if err != nil {
		return SalesforceOrgCleanup{}, err
	}
	defer os.RemoveAll(temporary)
	temporaryPath := filepath.Join(temporary, "ORG_CLEANUP.json")
	replayed, err := RunSalesforceOrgCleanup(SalesforceOrgCleanupRequest{BundlePath: request.BundlePath, CreationPath: filepath.Join(request.OutputRoot, "ORG_CREATION.json"), TargetOrg: request.TargetOrg, DevHub: request.DevHub, SFBin: request.SFBin, OutputPath: temporaryPath, runner: runner})
	if err != nil || index != len(records) {
		return SalesforceOrgCleanup{}, fmt.Errorf("existing worker Salesforce cleanup is invalid")
	}
	replayed.DevHubCommand.DurationMS = 0
	cleanup.DevHubCommand.DurationMS = 0
	for i := range replayed.Commands {
		replayed.Commands[i].DurationMS = 0
	}
	for i := range cleanup.Commands {
		cleanup.Commands[i].DurationMS = 0
	}
	if !reflect.DeepEqual(replayed, cleanup) {
		return SalesforceOrgCleanup{}, fmt.Errorf("existing worker Salesforce cleanup is invalid")
	}
	return cleanup, nil
}

func validateWorkerCleanupLifecycle(request OrchestratorWorkerCleanupRequest) (string, string, string, error) {
	info, err := os.Lstat(request.OutputRoot)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 {
		return "", "", "", fmt.Errorf("worker cleanup root must be mode 0700")
	}
	bindingPath := filepath.Join(request.OutputRoot, "ORCHESTRATOR_BINDING.json")
	if info, err := os.Lstat(bindingPath); err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o400 {
		return "", "", "", fmt.Errorf("worker cleanup binding mode is invalid")
	}
	binding, bindingBytes, err := readExactJSONBytes[OrchestratorBatchBinding](bindingPath)
	scopePath := request.ScopePath
	if scopePath == "" {
		scopePath = request.Plan.Definition.ScopePath
	}
	wantBinding, wantErr := expectedOrchestratorBatchBindingAtScope(request.Plan, request.Lease, scopePath)
	if err != nil || wantErr != nil || !reflect.DeepEqual(binding, wantBinding) {
		return "", "", "", fmt.Errorf("worker cleanup binding drift")
	}
	bindingSHA := replayBytesSHA256(bindingBytes)
	bundleSHA, err := sha256File(request.BundlePath)
	if err != nil {
		return "", "", "", err
	}
	reservationPath := filepath.Join(request.OutputRoot, "ORG_CREATION.json.reservation")
	if info, err := os.Lstat(reservationPath); err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return "", "", "", fmt.Errorf("worker cleanup reservation mode is invalid")
	}
	reservation, reservationBytes, err := readExactJSONBytes[salesforceOrgReservation](reservationPath)
	if err != nil || reservation.SchemaVersion != 1 || reservation.BundleSHA256 != bundleSHA || reservation.DevHub != request.DevHub || reservation.Alias != request.TargetOrg || !validSalesforceScratchMarker(reservation.Marker) || !validSalesforceReservedAliasAbsence(reservation.AliasAbsent, request.BundlePath, request.TargetOrg) {
		return "", "", "", fmt.Errorf("worker cleanup reservation is invalid")
	}
	reservationSHA := replayBytesSHA256(reservationBytes)
	stage, authoritySHA := "reservation-only", reservationSHA
	creationPath := filepath.Join(request.OutputRoot, "ORG_CREATION.json")
	invalidatedPath := creationPath + ".invalidated"
	creation, creationBytes, creationErr := readExactJSONBytes[SalesforceOrgCreation](creationPath)
	invalidated, invalidatedBytes, invalidatedErr := readExactJSONBytes[SalesforceOrgCreation](invalidatedPath)
	if creationErr == nil && invalidatedErr == nil {
		return "", "", "", fmt.Errorf("worker cleanup creation authority is ambiguous")
	}
	if creationErr == nil {
		if info, statErr := os.Lstat(creationPath); statErr != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
			return "", "", "", fmt.Errorf("worker cleanup creation mode is invalid")
		}
		if creation.Invalidated || creation.Marker != reservation.Marker || !validSalesforceOrgCreation(creation, bundleSHA, request.BundlePath, request.DevHub, request.TargetOrg) {
			return "", "", "", fmt.Errorf("worker cleanup creation is invalid")
		}
		stage, authoritySHA = "created-before-preflight", replayBytesSHA256([]byte(reservationSHA+":"+replayBytesSHA256(creationBytes)))
	} else if invalidatedErr == nil {
		if info, statErr := os.Lstat(invalidatedPath); statErr != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
			return "", "", "", fmt.Errorf("worker cleanup invalidated creation mode is invalid")
		}
		if invalidated.BundleSHA256 != bundleSHA || invalidated.Marker != reservation.Marker || !validInvalidatedSalesforceOrgCreation(invalidated, request.DevHub, request.TargetOrg) {
			return "", "", "", fmt.Errorf("worker cleanup invalidated creation is invalid")
		}
		stage, authoritySHA = "invalidated-creation", replayBytesSHA256([]byte(reservationSHA+":"+replayBytesSHA256(invalidatedBytes)))
	} else if !os.IsNotExist(creationErr) || !os.IsNotExist(invalidatedErr) {
		return "", "", "", fmt.Errorf("worker cleanup creation authority is unreadable")
	}
	preflightPath := filepath.Join(request.OutputRoot, "ORG_PREFLIGHT.json")
	dispatchPath := filepath.Join(request.OutputRoot, "SALESFORCE_DISPATCH.json")
	preflight, preflightBytes, preflightErr := readExactJSONBytes[SalesforceOrgPreflight](preflightPath)
	dispatch, dispatchBytes, dispatchErr := readExactJSONBytes[SalesforceDispatch](dispatchPath)
	if preflightErr == nil || dispatchErr == nil {
		if stage != "created-before-preflight" || preflightErr != nil || dispatchErr != nil {
			return "", "", "", fmt.Errorf("worker cleanup pre-shard lifecycle is incomplete")
		}
		for _, path := range []string{preflightPath, dispatchPath} {
			if info, err := os.Lstat(path); err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
				return "", "", "", fmt.Errorf("worker cleanup pre-shard lifecycle mode is invalid")
			}
		}
		if !validSalesforceOrgPreflight(preflight, bundleSHA, request.BundlePath) || preflight.OrgAlias != request.TargetOrg || preflight.OrgID != creation.OrgID {
			return "", "", "", fmt.Errorf("worker cleanup preflight is invalid")
		}
		if dispatch.SchemaVersion != 1 || dispatch.BundleSHA256 != bundleSHA || dispatch.OrgAlias != request.TargetOrg || dispatch.ShardIndex != request.Lease.ShardIndex || dispatch.ShardCount != len(request.Plan.Jobs) || !filepath.IsAbs(dispatch.ExecutorRoot) || filepath.Clean(dispatch.ExecutorRoot) != dispatch.ExecutorRoot || !safeOrchestratorToken(dispatch.RunID) || !sha256Pattern.MatchString(dispatch.PythonSHA256) || !sha256Pattern.MatchString(dispatch.FilterCommandSpecSHA256) {
			return "", "", "", fmt.Errorf("worker cleanup dispatch is invalid")
		}
		stage = "dispatched-before-shard"
		authoritySHA = replayBytesSHA256([]byte(authoritySHA + ":" + replayBytesSHA256(preflightBytes) + ":" + replayBytesSHA256(dispatchBytes)))
	} else if !os.IsNotExist(preflightErr) || !os.IsNotExist(dispatchErr) {
		return "", "", "", fmt.Errorf("worker cleanup pre-shard lifecycle is unreadable")
	}
	allowed := map[string]bool{"ORCHESTRATOR_BINDING.json": true, "ORG_CREATION.json.reservation": true, "ORG_CLEANUP.json": true, "WORKER_CLEANUP.json": true}
	if stage == "created-before-preflight" || stage == "dispatched-before-shard" {
		allowed["ORG_CREATION.json"] = true
	} else if stage == "invalidated-creation" {
		allowed["ORG_CREATION.json.invalidated"] = true
	}
	if stage == "dispatched-before-shard" {
		allowed["ORG_PREFLIGHT.json"] = true
		allowed["SALESFORCE_DISPATCH.json"] = true
	}
	entries, err := os.ReadDir(request.OutputRoot)
	if err != nil {
		return "", "", "", err
	}
	for _, entry := range entries {
		if !allowed[entry.Name()] {
			return "", "", "", fmt.Errorf("worker cleanup root contains ineligible lifecycle artifacts")
		}
	}
	return stage, authoritySHA, bindingSHA, nil
}

type OrchestratorSSHCleanupTakeoverRequest struct {
	Coordinator *Orchestrator
	Claim       OrchestratorCleanupClaim
	OrchestratorSSHCleanupBinding
	OutputPath string
}

// OrchestratorSSHCleanupBinding is the strict remote branch of the existing
// cleanup-takeover request. Local plan/lease/dispatch paths remain distinct
// from worker-side paths so hosts never need shared storage.
type OrchestratorSSHCleanupBinding struct {
	PlanPath           string `json:"planPath"`
	LeasePath          string `json:"leasePath"`
	FailedDispatchPath string `json:"failedDispatchPath"`
	Host               string `json:"host"`
	WorkerBin          string `json:"workerBin"`
	RemotePlanPath     string `json:"remotePlanPath"`
	RemoteScopePath    string `json:"remoteScopePath"`
	RemoteLeasePath    string `json:"remoteLeasePath"`
	RemoteBundlePath   string `json:"remoteBundlePath"`
	RemoteSFBin        string `json:"remoteSfBin"`
	RemoteRoot         string `json:"remoteRoot"`
	FetchedReceiptPath string `json:"fetchedReceiptPath"`

	sshRunner  orchestratorSSHRunner
	copyRunner remoteFailureCopyRunner
}

type OrchestratorSSHCleanupTakeoverReceipt struct {
	SchemaVersion             int    `json:"schemaVersion"`
	Status                    string `json:"status"`
	FailureCode               string `json:"failureCode,omitempty"`
	LifecycleStage            string `json:"lifecycleStage,omitempty"`
	CampaignID                string `json:"campaignId"`
	JobID                     string `json:"jobId"`
	Generation                int    `json:"generation"`
	CommandSHA256             string `json:"commandSha256"`
	WorkerReceiptSHA256       string `json:"workerReceiptSha256,omitempty"`
	FailedSSHReceiptSHA256    string `json:"failedSshReceiptSha256,omitempty"`
	OrgCleanupSHA256          string `json:"orgCleanupSha256,omitempty"`
	ExitCode                  int    `json:"exitCode"`
	TimeoutMS                 int64  `json:"timeoutMs"`
	TimedOut                  bool   `json:"timedOut"`
	Passed                    bool   `json:"passed"`
	ResidueAbsent             bool   `json:"residueAbsent"`
	ProofCredit               int    `json:"proofCredit"`
	CleanupPermanentlyBlocked bool   `json:"cleanupPermanentlyBlocked"`
}

func runOrchestratorSSHCleanupTakeover(request OrchestratorSSHCleanupTakeoverRequest, timeout time.Duration) (OrchestratorSSHCleanupTakeoverReceipt, error) {
	plan, planBytes, lease, leaseBytes, _, failedBytes, err := validateOrchestratorSSHCleanupTakeoverRequest(request, timeout)
	if err != nil {
		return OrchestratorSSHCleanupTakeoverReceipt{}, err
	}
	planSHA, leaseSHA, failedSHA := replayBytesSHA256(planBytes), replayBytesSHA256(leaseBytes), replayBytesSHA256(failedBytes)
	command := orchestratorSSHWorkerCleanupCommand(request, plan, planSHA, leaseSHA, failedSHA)
	args := []string{"-o", "BatchMode=yes", "--", request.Host, command}
	commandSHA := commandSpecSHA256(ReplayCommand{Path: orchestratorSSHBinary, Args: args, Timeout: timeout})
	if existing, ok, err := existingSSHCleanupTakeoverReceipt(request, plan, lease, planSHA, leaseSHA, failedSHA, commandSHA, timeout); err != nil || ok {
		return existing, err
	}
	if fetched, fetchedBytes, ok, err := existingFetchedWorkerCleanup(request, plan, lease, planSHA, leaseSHA, failedSHA); err != nil {
		return OrchestratorSSHCleanupTakeoverReceipt{}, err
	} else if ok {
		return finishOrchestratorSSHCleanupTakeover(request, fetched, fetchedBytes, commandSHA, timeout)
	}
	if validateOrchestratorCleanupClaim(request.Coordinator, request.Claim, time.Now().UTC()) != nil || !request.Claim.ClaimUntil.After(time.Now().UTC().Add(timeout+orchestratorSSHCleanupCloseMargin)) {
		return OrchestratorSSHCleanupTakeoverReceipt{}, fmt.Errorf("cleanup claim does not cover bounded SSH cleanup and close")
	}
	receipt := OrchestratorSSHCleanupTakeoverReceipt{SchemaVersion: 1, Status: "failed", FailureCode: orchestratorSSHCleanupFailedCode, CampaignID: lease.CampaignID, JobID: lease.JobID, Generation: lease.Generation, CommandSHA256: commandSHA, TimeoutMS: timeout.Milliseconds(), ProofCredit: 0}
	runner := request.sshRunner
	if runner == nil {
		runner = runSalesforceCLI
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	output, runErr := runner(ctx, orchestratorSSHBinary, args...)
	receipt.ExitCode, receipt.TimedOut = output.ExitCode, ctx.Err() == context.DeadlineExceeded
	if runErr != nil && receipt.ExitCode == 0 {
		receipt.ExitCode = -1
	}
	var completion OrchestratorWorkerCleanupReceipt
	completionErr := decodeExactJSON(output.Stdout, &completion)
	if receipt.TimedOut {
		receipt.Status, receipt.FailureCode = "timeout", orchestratorSSHCleanupTimeoutCode
	}
	if runErr != nil || receipt.ExitCode != 0 || receipt.TimedOut || completionErr != nil || !validWorkerCleanupCompletion(completion, plan, lease, planSHA, leaseSHA, failedSHA) {
		return receipt, fmt.Errorf("%s", receipt.FailureCode)
	}
	copyRunner := request.copyRunner
	if copyRunner == nil {
		copyRunner = runRemoteFailureCopy
	}
	temporary, err := os.MkdirTemp(filepath.Dir(request.FetchedReceiptPath), ".worker-cleanup-")
	if err != nil {
		return receipt, err
	}
	defer os.RemoveAll(temporary)
	copyOutput, copyErr := copyRunner(ctx, request.Host+":"+filepath.Join(request.RemoteRoot, "WORKER_CLEANUP.json"), temporary, "", false)
	fetchedPath := filepath.Join(temporary, "WORKER_CLEANUP.json")
	fetched, fetchedBytes, readErr := readMode0600JSON[OrchestratorWorkerCleanupReceipt](fetchedPath)
	if copyErr != nil || copyOutput.ExitCode != 0 || readErr != nil || !reflect.DeepEqual(fetched, completion) {
		return receipt, fmt.Errorf("fetch worker cleanup receipt failed")
	}
	if len(strings.TrimSpace(string(copyOutput.Stdout))) != 0 || len(strings.TrimSpace(string(copyOutput.Stderr))) != 0 {
		return receipt, fmt.Errorf("fetch worker cleanup receipt produced output")
	}
	if err := WriteNewJSON(request.FetchedReceiptPath, fetched); err != nil {
		existing, existingBytes, readErr := readMode0600JSON[OrchestratorWorkerCleanupReceipt](request.FetchedReceiptPath)
		if readErr != nil || !reflect.DeepEqual(existing, fetched) || !reflect.DeepEqual(existingBytes, fetchedBytes) {
			return receipt, fmt.Errorf("publish fetched worker cleanup receipt: %w", err)
		}
		fetchedBytes = existingBytes
	} else {
		published, publishedBytes, readErr := readMode0600JSON[OrchestratorWorkerCleanupReceipt](request.FetchedReceiptPath)
		if readErr != nil || !reflect.DeepEqual(published, fetched) || !reflect.DeepEqual(publishedBytes, fetchedBytes) {
			return receipt, fmt.Errorf("published worker cleanup receipt bytes drift")
		}
		fetchedBytes = publishedBytes
	}
	return finishOrchestratorSSHCleanupTakeover(request, fetched, fetchedBytes, commandSHA, timeout)
}

func existingFetchedWorkerCleanup(request OrchestratorSSHCleanupTakeoverRequest, plan OrchestratorCampaignPlan, lease OrchestratorLease, planSHA, leaseSHA, failedSHA string) (OrchestratorWorkerCleanupReceipt, []byte, bool, error) {
	if _, err := os.Lstat(request.FetchedReceiptPath); os.IsNotExist(err) {
		return OrchestratorWorkerCleanupReceipt{}, nil, false, nil
	} else if err != nil {
		return OrchestratorWorkerCleanupReceipt{}, nil, false, err
	}
	receipt, data, err := readMode0600JSON[OrchestratorWorkerCleanupReceipt](request.FetchedReceiptPath)
	if err != nil || !validWorkerCleanupCompletion(receipt, plan, lease, planSHA, leaseSHA, failedSHA) {
		return OrchestratorWorkerCleanupReceipt{}, nil, false, fmt.Errorf("existing fetched worker cleanup receipt is invalid")
	}
	return receipt, data, true, nil
}

func finishOrchestratorSSHCleanupTakeover(request OrchestratorSSHCleanupTakeoverRequest, worker OrchestratorWorkerCleanupReceipt, workerBytes []byte, commandSHA string, timeout time.Duration) (OrchestratorSSHCleanupTakeoverReceipt, error) {
	closed, blocked, err := orchestratorSSHCleanupClosureState(request.Coordinator, request.Claim)
	if err != nil {
		return OrchestratorSSHCleanupTakeoverReceipt{}, err
	}
	if !closed {
		if validateOrchestratorCleanupClaim(request.Coordinator, request.Claim, time.Now().UTC()) != nil {
			return OrchestratorSSHCleanupTakeoverReceipt{}, fmt.Errorf("cleanup claim changed before close")
		}
		if err := request.Coordinator.closeCleanup(request.Claim, time.Now().UTC(), false); err != nil {
			return OrchestratorSSHCleanupTakeoverReceipt{}, err
		}
		closed, blocked, err = orchestratorSSHCleanupClosureState(request.Coordinator, request.Claim)
		if err != nil {
			return OrchestratorSSHCleanupTakeoverReceipt{}, err
		}
	}
	if !closed || !blocked {
		return OrchestratorSSHCleanupTakeoverReceipt{}, fmt.Errorf("cleanup closure lacks permanent credit block")
	}
	receipt := successfulSSHCleanupTakeoverReceipt(request, worker, workerBytes, commandSHA, timeout)
	if err := WriteNewJSON(request.OutputPath, receipt); err != nil {
		existing, _, readErr := readMode0600JSON[OrchestratorSSHCleanupTakeoverReceipt](request.OutputPath)
		if readErr != nil || existing != receipt {
			return receipt, err
		}
	}
	return receipt, nil
}

func existingSSHCleanupTakeoverReceipt(request OrchestratorSSHCleanupTakeoverRequest, plan OrchestratorCampaignPlan, lease OrchestratorLease, planSHA, leaseSHA, failedSHA, commandSHA string, timeout time.Duration) (OrchestratorSSHCleanupTakeoverReceipt, bool, error) {
	if _, err := os.Lstat(request.OutputPath); os.IsNotExist(err) {
		return OrchestratorSSHCleanupTakeoverReceipt{}, false, nil
	} else if err != nil {
		return OrchestratorSSHCleanupTakeoverReceipt{}, false, err
	}
	worker, workerBytes, ok, err := existingFetchedWorkerCleanup(request, plan, lease, planSHA, leaseSHA, failedSHA)
	if err != nil || !ok {
		return OrchestratorSSHCleanupTakeoverReceipt{}, false, fmt.Errorf("coordinator cleanup receipt lacks fetched worker authority")
	}
	want := successfulSSHCleanupTakeoverReceipt(request, worker, workerBytes, commandSHA, timeout)
	existing, _, err := readMode0600JSON[OrchestratorSSHCleanupTakeoverReceipt](request.OutputPath)
	closed, blocked, stateErr := orchestratorSSHCleanupClosureState(request.Coordinator, request.Claim)
	if err != nil || stateErr != nil || existing != want || !closed || !blocked || !worker.ResidueAbsent {
		return OrchestratorSSHCleanupTakeoverReceipt{}, false, fmt.Errorf("existing coordinator cleanup receipt is invalid")
	}
	return existing, true, nil
}

func successfulSSHCleanupTakeoverReceipt(request OrchestratorSSHCleanupTakeoverRequest, worker OrchestratorWorkerCleanupReceipt, workerBytes []byte, commandSHA string, timeout time.Duration) OrchestratorSSHCleanupTakeoverReceipt {
	return OrchestratorSSHCleanupTakeoverReceipt{
		SchemaVersion: 1, Status: "cleanup-closed", LifecycleStage: worker.LifecycleStage, CampaignID: request.Claim.CampaignID, JobID: request.Claim.JobID, Generation: request.Claim.Generation,
		CommandSHA256: commandSHA, WorkerReceiptSHA256: replayBytesSHA256(workerBytes), FailedSSHReceiptSHA256: worker.FailedSSHReceiptSHA256, OrgCleanupSHA256: worker.OrgCleanupSHA256,
		ExitCode: 0, TimeoutMS: timeout.Milliseconds(), Passed: true, ResidueAbsent: true, ProofCredit: 0, CleanupPermanentlyBlocked: true,
	}
}

func orchestratorSSHCleanupClosureState(orchestrator *Orchestrator, claim OrchestratorCleanupClaim) (bool, bool, error) {
	var cleanupState, allocationState, hubAlias, claimedBy string
	var claimUntil int64
	var blocks int
	err := orchestrator.db.QueryRow(`SELECT c.state, a.state, a.hub_alias, c.claimed_by, c.claim_until, (SELECT count(*) FROM cleanup_credit_blocks b WHERE b.allocation_alias = c.allocation_alias) FROM cleanup_journal c JOIN scratch_allocations a ON a.allocation_alias = c.allocation_alias WHERE c.campaign_id = ? AND c.job_id = ? AND c.generation = ? AND c.allocation_alias = ?`, claim.CampaignID, claim.JobID, claim.Generation, claim.AllocationAlias).Scan(&cleanupState, &allocationState, &hubAlias, &claimedBy, &claimUntil, &blocks)
	if err != nil {
		return false, false, err
	}
	if hubAlias != claim.HubAlias || claimedBy != claim.Worker || claimUntil != claim.ClaimUntil.UTC().UnixMilli() {
		return false, false, fmt.Errorf("cleanup closure claim drift")
	}
	return cleanupState == "closed" && allocationState == "closed", blocks == 1, nil
}

func validateOrchestratorSSHCleanupTakeoverRequest(request OrchestratorSSHCleanupTakeoverRequest, timeout time.Duration) (OrchestratorCampaignPlan, []byte, OrchestratorLease, []byte, OrchestratorSSHDispatchReceipt, []byte, error) {
	zeroPlan, zeroLease, zeroDispatch := OrchestratorCampaignPlan{}, OrchestratorLease{}, OrchestratorSSHDispatchReceipt{}
	if request.Coordinator == nil || request.Coordinator.db == nil || timeout <= 0 || !safeRemoteSSHHost.MatchString(request.Host) {
		return zeroPlan, nil, zeroLease, nil, zeroDispatch, nil, fmt.Errorf("live coordinator and safe SSH host are required")
	}
	for _, path := range []string{request.PlanPath, request.LeasePath, request.FailedDispatchPath, request.WorkerBin, request.RemotePlanPath, request.RemoteScopePath, request.RemoteLeasePath, request.RemoteBundlePath, request.RemoteSFBin, request.RemoteRoot, request.FetchedReceiptPath, request.OutputPath} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return zeroPlan, nil, zeroLease, nil, zeroDispatch, nil, fmt.Errorf("absolute clean SSH cleanup paths are required")
		}
	}
	if !safeOrchestratorSSHRemotePath.MatchString(request.RemoteRoot) || request.RemoteRoot == "/" || request.FetchedReceiptPath == request.OutputPath {
		return zeroPlan, nil, zeroLease, nil, zeroDispatch, nil, fmt.Errorf("unsafe SSH cleanup paths")
	}
	for _, path := range []string{request.FetchedReceiptPath, request.OutputPath} {
		if _, err := os.Lstat(path); os.IsNotExist(err) {
			if err := preflightOrchestratorSSHReceiptPath(path); err != nil {
				return zeroPlan, nil, zeroLease, nil, zeroDispatch, nil, err
			}
		} else if err != nil {
			return zeroPlan, nil, zeroLease, nil, zeroDispatch, nil, err
		}
	}
	plan, planBytes, err := readExactJSONBytes[OrchestratorCampaignPlan](request.PlanPath)
	if err != nil {
		return zeroPlan, nil, zeroLease, nil, zeroDispatch, nil, err
	}
	lease, leaseBytes, err := readExactJSONBytes[OrchestratorLease](request.LeasePath)
	if err != nil || validateOrchestratorWorkerPlanLease(plan, lease) != nil {
		return zeroPlan, nil, zeroLease, nil, zeroDispatch, nil, fmt.Errorf("SSH cleanup plan or lease is invalid")
	}
	failed, failedBytes, err := readExactJSONBytes[OrchestratorSSHDispatchReceipt](request.FailedDispatchPath)
	if err != nil {
		return zeroPlan, nil, zeroLease, nil, zeroDispatch, nil, err
	}
	planSHA, leaseSHA := replayBytesSHA256(planBytes), replayBytesSHA256(leaseBytes)
	if request.Claim.CampaignID != lease.CampaignID || request.Claim.JobID != lease.JobID || request.Claim.Generation != lease.Generation || request.Claim.AllocationAlias == "" || request.Claim.HubAlias == "" || request.Claim.Worker == "" || request.Claim.ClaimUntil.IsZero() || validateStoredCleanupPlanLease(request.Coordinator, plan, lease) != nil {
		return zeroPlan, nil, zeroLease, nil, zeroDispatch, nil, fmt.Errorf("SSH cleanup claim is not exact")
	}
	original := OrchestratorSSHDispatchRequest{Host: request.Host, WorkerBin: request.WorkerBin, PlanPath: request.PlanPath, RemotePlanPath: request.RemotePlanPath, RemoteScopePath: request.RemoteScopePath, LeasePath: request.RemoteLeasePath, BundlePath: request.RemoteBundlePath, TargetOrg: request.Claim.AllocationAlias, SFBin: request.RemoteSFBin, OutputRoot: request.RemoteRoot}
	command := orchestratorSSHWorkerOnceCommand(original, plan.Definition.Tools.SHA256, plan.Definition.ControlledInputSHA256[OrchestratorToolsAMD64Input], planSHA, leaseSHA, plan.Definition.ScopeSHA256, request.Claim.HubAlias)
	args := []string{"-o", "BatchMode=yes", "--", request.Host, command}
	common := failed.SchemaVersion == 1 && !failed.Passed && failed.DurationMS >= 0 && failed.CampaignID == lease.CampaignID && failed.JobID == lease.JobID && failed.ShardIndex == lease.ShardIndex && failed.Generation == lease.Generation && failed.SpecSHA256 == plan.SpecSHA256 && failed.PlanSHA256 == planSHA && failed.LeaseSHA256 == leaseSHA && failed.TimeoutMS == orchestratorSSHTimeout.Milliseconds() && failed.ActionRequired && failed.ActionCode == orchestratorSSHActionCode && sha256Pattern.MatchString(failed.StdoutSHA256) && sha256Pattern.MatchString(failed.StderrSHA256) && failed.OrchestratorBindingSHA256 == "" && failed.SalesforceShardSHA256 == "" && failed.OrgCleanupSHA256 == "" && failed.ExecutedTools == (RuntimeArtifact{}) && failed.CommandSHA256 == commandSpecSHA256(ReplayCommand{Path: orchestratorSSHBinary, Args: args, Timeout: orchestratorSSHTimeout})
	failedStatus := failed.Status == "failed" && failed.FailureCode == orchestratorSSHDispatchFailed && !failed.TimedOut && failed.ExitCode != 0
	timedOut := failed.Status == "timeout" && failed.FailureCode == orchestratorSSHDispatchTimeout && failed.TimedOut
	if !common || !failedStatus && !timedOut {
		return zeroPlan, nil, zeroLease, nil, zeroDispatch, nil, fmt.Errorf("failed SSH dispatch authority is invalid")
	}
	return plan, planBytes, lease, leaseBytes, failed, failedBytes, nil
}

func validateStoredCleanupPlanLease(orchestrator *Orchestrator, plan OrchestratorCampaignPlan, lease OrchestratorLease) error {
	inputs, _ := json.Marshal(plan.Definition.ControlledInputSHA256)
	surfaces, _ := json.Marshal(lease.SurfaceIDs)
	maxAttempts, err := normalizedOrchestratorMaxAttemptsPerJob(plan.MaxAttemptsPerJob)
	if err != nil {
		return err
	}
	var spec, candidateCommit, candidateSHA, toolsCommit, toolsSHA, scopePath, scopeSHA, storedInputs, kind, storedSurfaces, worker string
	var storedMax, shard, generation int
	var leaseUntil, durationMS int64
	err = orchestrator.db.QueryRow(`SELECT c.spec_sha256, c.candidate_commit, c.candidate_sha256, c.tools_commit, c.tools_sha256, c.scope_path, c.scope_sha256, c.controlled_inputs_json, c.max_attempts_per_job, j.kind, j.shard_index, j.surface_ids_json, j.generation, a.worker, a.lease_until, l.duration_ms FROM campaigns c JOIN jobs j ON j.campaign_id = c.id JOIN attempts a ON a.campaign_id = j.campaign_id AND a.job_id = j.id AND a.generation = ? JOIN lease_terms l ON l.campaign_id = j.campaign_id AND l.job_id = j.id AND l.generation = a.generation WHERE c.id = ? AND j.id = ?`, lease.Generation, lease.CampaignID, lease.JobID).Scan(&spec, &candidateCommit, &candidateSHA, &toolsCommit, &toolsSHA, &scopePath, &scopeSHA, &storedInputs, &storedMax, &kind, &shard, &storedSurfaces, &generation, &worker, &leaseUntil, &durationMS)
	if err != nil || spec != plan.SpecSHA256 || candidateCommit != plan.Definition.Candidate.Commit || candidateSHA != plan.Definition.Candidate.SHA256 || toolsCommit != plan.Definition.Tools.Commit || toolsSHA != plan.Definition.Tools.SHA256 || scopePath != plan.Definition.ScopePath || scopeSHA != plan.Definition.ScopeSHA256 || storedInputs != string(inputs) || storedMax != maxAttempts || kind != string(lease.Kind) || shard != lease.ShardIndex || storedSurfaces != string(surfaces) || generation != lease.Generation || worker != lease.Worker || leaseUntil != lease.LeaseUntil.UTC().UnixMilli() || durationMS != lease.DurationMS {
		return fmt.Errorf("stored cleanup plan or lease drift")
	}
	return nil
}

func orchestratorSSHWorkerCleanupCommand(request OrchestratorSSHCleanupTakeoverRequest, plan OrchestratorCampaignPlan, planSHA, leaseSHA, failedSHA string) string {
	command := strings.Join([]string{
		shellQuote(request.WorkerBin), "corpus assurance orchestrator worker-cleanup --plan", shellQuote(request.RemotePlanPath), "--plan-sha256", shellQuote(planSHA),
		"--scope", shellQuote(request.RemoteScopePath), "--lease", shellQuote(request.RemoteLeasePath), "--lease-sha256", shellQuote(leaseSHA), "--failed-ssh-sha256", shellQuote(failedSHA),
		"--bundle", shellQuote(request.RemoteBundlePath), "--dev-hub", shellQuote(request.Claim.HubAlias), "--target-org", shellQuote(request.Claim.AllocationAlias),
		"--sf-bin", shellQuote(request.RemoteSFBin), "--output-root", shellQuote(request.RemoteRoot),
	}, " ")
	workerHash := "/usr/bin/shasum -a 256 -- " + shellQuote(request.WorkerBin) + " | /usr/bin/awk '{print $1}'"
	check := "worker_sha=\"$(" + workerHash + ")\" && { test \"$worker_sha\" = " + shellQuote(plan.Definition.Tools.SHA256)
	if alternate := plan.Definition.ControlledInputSHA256[OrchestratorToolsAMD64Input]; alternate != "" {
		check += " || test \"$worker_sha\" = " + shellQuote(alternate)
	}
	check += "; }"
	checks := []string{check,
		"test \"$(/usr/bin/shasum -a 256 -- " + shellQuote(request.RemotePlanPath) + " | /usr/bin/awk '{print $1}')\" = " + shellQuote(planSHA),
		"test \"$(/usr/bin/shasum -a 256 -- " + shellQuote(request.RemoteScopePath) + " | /usr/bin/awk '{print $1}')\" = " + shellQuote(plan.Definition.ScopeSHA256),
		"test \"$(/usr/bin/shasum -a 256 -- " + shellQuote(request.RemoteLeasePath) + " | /usr/bin/awk '{print $1}')\" = " + shellQuote(leaseSHA),
	}
	return strings.Join(checks, " && ") + " || { echo 'worker cleanup input integrity check failed' >&2; exit 126; }; export SF_USE_GENERIC_UNIX_KEYCHAIN=true; exec " + command
}

func validWorkerCleanupCompletion(receipt OrchestratorWorkerCleanupReceipt, plan OrchestratorCampaignPlan, lease OrchestratorLease, planSHA, leaseSHA, failedSHA string) bool {
	stages := map[string]bool{"reservation-only": true, "invalidated-creation": true, "created-before-preflight": true}
	return receipt.SchemaVersion == 1 && receipt.Status == "cleanup-closed" && stages[receipt.LifecycleStage] && receipt.CampaignID == lease.CampaignID && receipt.JobID == lease.JobID && receipt.ShardIndex == lease.ShardIndex && receipt.Generation == lease.Generation && receipt.SpecSHA256 == plan.SpecSHA256 && receipt.PlanSHA256 == planSHA && receipt.LeaseSHA256 == leaseSHA && receipt.FailedSSHReceiptSHA256 == failedSHA && sha256Pattern.MatchString(receipt.OrchestratorBindingSHA256) && sha256Pattern.MatchString(receipt.LifecycleAuthoritySHA256) && sha256Pattern.MatchString(receipt.OrgCleanupSHA256) && receipt.ResidueAbsent && receipt.ProofCredit == 0 && validOrchestratorExecutedTools(receipt.ExecutedTools, plan) && receipt.ExecutedTools != (RuntimeArtifact{})
}
