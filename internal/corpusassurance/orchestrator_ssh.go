package corpusassurance

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	orchestratorSSHBinary  = "/usr/bin/ssh"
	orchestratorSSHTimeout = orchestratorWorkerOnceTimeout
)

const (
	orchestratorSSHDispatchFailed  = "orchestrator-ssh-dispatch-failed"
	orchestratorSSHDispatchTimeout = "orchestrator-ssh-dispatch-timeout"
	orchestratorSSHActionCode      = "inspect-remote-lifecycle-artifacts-and-close-cleanup"
)

// OrchestratorSSHDispatchRequest contains only the fixed worker-once inputs.
// Plan and lease are coordinator-readable bindings addressed at the worker;
// bundle, Salesforce binary, and output root are worker-side paths.
type OrchestratorSSHDispatchRequest struct {
	Coordinator *Orchestrator
	Host        string
	WorkerBin   string
	PlanPath    string
	LeasePath   string
	BundlePath  string
	TargetOrg   string
	SFBin       string
	OutputRoot  string
	OutputPath  string
}

// OrchestratorSSHDispatchReceipt is safe to publish. It deliberately contains
// no host, user, org, path, command, or retained SSH output.
type OrchestratorSSHDispatchReceipt struct {
	SchemaVersion             int    `json:"schemaVersion"`
	CampaignID                string `json:"campaignId"`
	JobID                     string `json:"jobId"`
	ShardIndex                int    `json:"shardIndex"`
	Generation                int    `json:"generation"`
	Status                    string `json:"status"`
	FailureCode               string `json:"failureCode,omitempty"`
	CommandSHA256             string `json:"commandSha256"`
	StdoutSHA256              string `json:"stdoutSha256"`
	StderrSHA256              string `json:"stderrSha256"`
	ExitCode                  int    `json:"exitCode"`
	DurationMS                int64  `json:"durationMs"`
	TimeoutMS                 int64  `json:"timeoutMs"`
	TimedOut                  bool   `json:"timedOut"`
	Passed                    bool   `json:"passed"`
	ActionRequired            bool   `json:"actionRequired"`
	ActionCode                string `json:"actionCode,omitempty"`
	SpecSHA256                string `json:"specSha256"`
	PlanSHA256                string `json:"planSha256"`
	LeaseSHA256               string `json:"leaseSha256"`
	OrchestratorBindingSHA256 string `json:"orchestratorBindingSha256"`
	SalesforceShardSHA256     string `json:"salesforceShardSha256"`
	OrgCleanupSHA256          string `json:"orgCleanupSha256"`
}

// OrchestratorWorkerOnceCompletion is the only stdout contract accepted from
// a remote worker. It contains no paths, host identity, org identity, or raw
// command output.
type OrchestratorWorkerOnceCompletion struct {
	CampaignID                string `json:"campaignId"`
	JobID                     string `json:"jobId"`
	ShardIndex                int    `json:"shardIndex"`
	Generation                int    `json:"generation"`
	Status                    string `json:"status"`
	SpecSHA256                string `json:"specSha256"`
	PlanSHA256                string `json:"planSha256"`
	LeaseSHA256               string `json:"leaseSha256"`
	OrchestratorBindingSHA256 string `json:"orchestratorBindingSha256"`
	SalesforceShardSHA256     string `json:"salesforceShardSha256"`
	OrgCleanupSHA256          string `json:"orgCleanupSha256"`
}

type orchestratorSSHRunner func(context.Context, string, ...string) (salesforceCommandOutput, error)

// RunOrchestratorSSHDispatch invokes exactly one bounded worker-once command.
// It records a sanitized coordinator receipt but never records proof credit.
func RunOrchestratorSSHDispatch(request OrchestratorSSHDispatchRequest) (OrchestratorSSHDispatchReceipt, error) {
	if request.Coordinator == nil || request.Coordinator.db == nil {
		return OrchestratorSSHDispatchReceipt{}, fmt.Errorf("live orchestrator coordinator is required")
	}
	return runOrchestratorSSHDispatchWithTimeout(request, orchestratorSSHTimeout, runSalesforceCLI)
}

func runOrchestratorSSHDispatch(request OrchestratorSSHDispatchRequest, runner orchestratorSSHRunner) (OrchestratorSSHDispatchReceipt, error) {
	return runOrchestratorSSHDispatchWithTimeout(request, orchestratorSSHTimeout, runner)
}

func runOrchestratorSSHDispatchWithTimeout(request OrchestratorSSHDispatchRequest, timeout time.Duration, runner orchestratorSSHRunner) (OrchestratorSSHDispatchReceipt, error) {
	if err := validateOrchestratorSSHDispatchRequest(request); err != nil {
		return OrchestratorSSHDispatchReceipt{}, err
	}
	if timeout <= 0 || runner == nil {
		return OrchestratorSSHDispatchReceipt{}, fmt.Errorf("bounded SSH runner is required")
	}
	if _, err := os.Lstat(request.OutputPath); err == nil {
		return OrchestratorSSHDispatchReceipt{}, fmt.Errorf("orchestrator SSH dispatch output already exists: %s", request.OutputPath)
	} else if !os.IsNotExist(err) {
		return OrchestratorSSHDispatchReceipt{}, err
	}
	if err := preflightOrchestratorSSHReceiptPath(request.OutputPath); err != nil {
		return OrchestratorSSHDispatchReceipt{}, fmt.Errorf("preflight orchestrator SSH receipt: %w", err)
	}

	plan, planBytes, err := readExactJSONBytes[OrchestratorCampaignPlan](request.PlanPath)
	if err != nil {
		return OrchestratorSSHDispatchReceipt{}, fmt.Errorf("read orchestrator plan: %w", err)
	}
	lease, leaseBytes, err := readExactJSONBytes[OrchestratorLease](request.LeasePath)
	if err != nil {
		return OrchestratorSSHDispatchReceipt{}, fmt.Errorf("read orchestrator lease: %w", err)
	}
	if err := validateOrchestratorWorkerPlanLease(plan, lease); err != nil {
		return OrchestratorSSHDispatchReceipt{}, fmt.Errorf("orchestrator plan and lease drift: %w", err)
	}
	now := time.Now().UTC()
	if err := validateOrchestratorLiveWorkerLease(lease, now); err != nil || lease.LeaseUntil.Before(now.Add(timeout)) {
		if err != nil {
			return OrchestratorSSHDispatchReceipt{}, err
		}
		return OrchestratorSSHDispatchReceipt{}, fmt.Errorf("orchestrator lease does not cover bounded SSH dispatch")
	}
	reservedHub := ""
	if request.Coordinator != nil {
		reservedHub, err = validateOrchestratorSSHCoordinator(request.Coordinator, plan, lease, request.TargetOrg, now, timeout)
		if err != nil {
			return OrchestratorSSHDispatchReceipt{}, err
		}
	} else {
		return OrchestratorSSHDispatchReceipt{}, fmt.Errorf("live orchestrator coordinator is required")
	}
	if !safeOrchestratorToken(reservedHub) {
		return OrchestratorSSHDispatchReceipt{}, fmt.Errorf("SSH dispatch requires exact reserved Dev Hub")
	}
	planSHA, leaseSHA := replayBytesSHA256(planBytes), replayBytesSHA256(leaseBytes)
	command := orchestratorSSHWorkerOnceCommand(request, plan.Definition.Tools.SHA256, plan.Definition.ControlledInputSHA256[OrchestratorToolsAMD64Input], planSHA, leaseSHA, reservedHub)
	args := []string{"-o", "BatchMode=yes", "--", request.Host, command}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	started := time.Now()
	output, runErr := runner(ctx, orchestratorSSHBinary, args...)
	receipt := OrchestratorSSHDispatchReceipt{
		SchemaVersion: 1, CampaignID: lease.CampaignID, JobID: lease.JobID, ShardIndex: lease.ShardIndex, Generation: lease.Generation,
		Status: "failed", FailureCode: orchestratorSSHDispatchFailed,
		CommandSHA256: commandSpecSHA256(ReplayCommand{Path: orchestratorSSHBinary, Args: args, Timeout: timeout}),
		StdoutSHA256:  replayBytesSHA256(output.Stdout), StderrSHA256: replayBytesSHA256(output.Stderr),
		ExitCode: output.ExitCode, DurationMS: time.Since(started).Milliseconds(), TimeoutMS: timeout.Milliseconds(),
		TimedOut: ctx.Err() == context.DeadlineExceeded, ActionRequired: true, ActionCode: orchestratorSSHActionCode,
		SpecSHA256: plan.SpecSHA256, PlanSHA256: planSHA, LeaseSHA256: leaseSHA,
	}
	if runErr != nil && receipt.ExitCode == 0 {
		receipt.ExitCode = -1
	}
	var completion OrchestratorWorkerOnceCompletion
	completionErr := decodeExactJSON(output.Stdout, &completion)
	completionValid := completionErr == nil && completion.CampaignID == lease.CampaignID && completion.JobID == lease.JobID && completion.ShardIndex == lease.ShardIndex && completion.Generation == lease.Generation && completion.Status == "worker-complete" && completion.SpecSHA256 == plan.SpecSHA256 && completion.PlanSHA256 == planSHA && completion.LeaseSHA256 == leaseSHA && sha256Pattern.MatchString(completion.OrchestratorBindingSHA256) && sha256Pattern.MatchString(completion.SalesforceShardSHA256) && sha256Pattern.MatchString(completion.OrgCleanupSHA256)
	receipt.Passed = runErr == nil && receipt.ExitCode == 0 && !receipt.TimedOut && completionValid
	if receipt.Passed {
		receipt.Status, receipt.FailureCode = "worker-complete", ""
		receipt.ActionRequired, receipt.ActionCode = false, ""
		receipt.SpecSHA256 = completion.SpecSHA256
		receipt.PlanSHA256 = completion.PlanSHA256
		receipt.LeaseSHA256 = completion.LeaseSHA256
		receipt.OrchestratorBindingSHA256 = completion.OrchestratorBindingSHA256
		receipt.SalesforceShardSHA256 = completion.SalesforceShardSHA256
		receipt.OrgCleanupSHA256 = completion.OrgCleanupSHA256
	} else if receipt.TimedOut {
		receipt.Status, receipt.FailureCode = "timeout", orchestratorSSHDispatchTimeout
	}
	if err := WriteNewJSON(request.OutputPath, receipt); err != nil {
		receipt.Passed = false
		receipt.Status = "failed"
		receipt.FailureCode = orchestratorSSHDispatchFailed
		receipt.ActionRequired = true
		receipt.ActionCode = orchestratorSSHActionCode
		return receipt, err
	}
	if !receipt.Passed {
		return receipt, fmt.Errorf("%s", receipt.FailureCode)
	}
	return receipt, nil
}

func preflightOrchestratorSSHReceiptPath(path string) error {
	file, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".preflight-")
	if err != nil {
		return err
	}
	name := file.Name()
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		os.Remove(name)
		return err
	}
	if err := file.Close(); err != nil {
		os.Remove(name)
		return err
	}
	return os.Remove(name)
}

func validateOrchestratorSSHDispatchRequest(request OrchestratorSSHDispatchRequest) error {
	if !safeRemoteSSHHost.MatchString(request.Host) || !safeOrchestratorToken(request.TargetOrg) {
		return fmt.Errorf("invalid orchestrator SSH dispatch target")
	}
	for _, path := range []string{request.WorkerBin, request.PlanPath, request.LeasePath, request.BundlePath, request.SFBin, request.OutputRoot, request.OutputPath} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return fmt.Errorf("absolute clean orchestrator SSH paths are required")
		}
	}
	return nil
}

func orchestratorSSHWorkerOnceCommand(request OrchestratorSSHDispatchRequest, toolsSHA256, alternateToolsSHA256, planSHA256, leaseSHA256, devHub string) string {
	command := strings.Join([]string{
		shellQuote(request.WorkerBin), "corpus assurance orchestrator worker-once --plan", shellQuote(request.PlanPath),
		"--plan-sha256", shellQuote(planSHA256), "--lease", shellQuote(request.LeasePath), "--lease-sha256", shellQuote(leaseSHA256), "--bundle", shellQuote(request.BundlePath),
		"--dev-hub", shellQuote(devHub),
		"--target-org", shellQuote(request.TargetOrg), "--sf-bin", shellQuote(request.SFBin),
		"--output-root", shellQuote(request.OutputRoot),
	}, " ")
	workerHashCommand := "/usr/bin/shasum -a 256 -- " + shellQuote(request.WorkerBin) + " | /usr/bin/awk '{print $1}'"
	workerCheck := "test \"$(" + workerHashCommand + ")\" = " + shellQuote(toolsSHA256)
	if alternateToolsSHA256 != "" {
		workerCheck = "worker_sha=\"$(" + workerHashCommand + ")\" && { test \"$worker_sha\" = " + shellQuote(toolsSHA256) + " || test \"$worker_sha\" = " + shellQuote(alternateToolsSHA256) + "; }"
	}
	checks := []string{
		workerCheck,
		"test \"$(/usr/bin/shasum -a 256 -- " + shellQuote(request.PlanPath) + " | /usr/bin/awk '{print $1}')\" = " + shellQuote(planSHA256),
		"test \"$(/usr/bin/shasum -a 256 -- " + shellQuote(request.LeasePath) + " | /usr/bin/awk '{print $1}')\" = " + shellQuote(leaseSHA256),
	}
	return strings.Join(checks, " && ") + " || { echo 'worker input integrity check failed' >&2; exit 126; }; export SF_USE_GENERIC_UNIX_KEYCHAIN=true; exec " + command
}

func validateOrchestratorSSHCoordinator(orchestrator *Orchestrator, plan OrchestratorCampaignPlan, lease OrchestratorLease, targetOrg string, now time.Time, timeout time.Duration) (string, error) {
	if orchestrator == nil || orchestrator.db == nil || !safeOrchestratorToken(targetOrg) || !safeOrchestratorToken(lease.Worker) {
		return "", fmt.Errorf("live orchestrator coordinator is required")
	}
	if timeout <= 0 {
		return "", fmt.Errorf("bounded SSH dispatch timeout is required")
	}
	var hub string
	deadline := now.Add(timeout).UnixMilli()
	err := orchestrator.db.QueryRow(`SELECT s.hub_alias FROM campaigns c JOIN jobs j ON j.campaign_id = c.id JOIN attempts a ON a.campaign_id = j.campaign_id AND a.job_id = j.id AND a.generation = j.generation JOIN lease_terms l ON l.campaign_id = j.campaign_id AND l.job_id = j.id AND l.generation = j.generation JOIN scratch_allocations s ON s.campaign_id = j.campaign_id AND s.job_id = j.id AND s.generation = j.generation JOIN cleanup_journal x ON x.allocation_alias = s.allocation_alias AND x.campaign_id = s.campaign_id AND x.job_id = s.job_id AND x.generation = s.generation WHERE c.id = ? AND c.spec_sha256 = ? AND j.id = ? AND j.generation = ? AND j.leased_by = ? AND j.status = 'running' AND j.lease_until = ? AND j.lease_until >= ? AND a.worker = ? AND a.status = 'running' AND a.lease_until = ? AND a.lease_until >= ? AND l.duration_ms = ? AND l.duration_ms >= ? AND s.allocation_alias = ? AND s.state = 'reserved' AND x.state = 'pending'`, lease.CampaignID, plan.SpecSHA256, lease.JobID, lease.Generation, lease.Worker, lease.LeaseUntil.UnixMilli(), deadline, lease.Worker, lease.LeaseUntil.UnixMilli(), deadline, lease.DurationMS, timeout.Milliseconds(), targetOrg).Scan(&hub)
	if err != nil || !safeOrchestratorToken(hub) {
		return "", fmt.Errorf("SSH dispatch requires exact current attempt, campaign, reservation, and cleanup authority")
	}
	return hub, nil
}
