package corpusassurance

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"
)

const orchestratorSSHFetchTimeout = 2 * time.Minute

var safeOrchestratorSSHRemotePath = regexp.MustCompile(`^/[A-Za-z0-9._/-]+$`)

type OrchestratorSSHRawFetchRequest struct {
	Plan           OrchestratorCampaignPlan
	Lease          OrchestratorLease
	Dispatch       OrchestratorSSHDispatchReceipt
	Host           string
	WorkerBin      string
	PlanPath       string
	RemotePlanPath string
	LeasePath      string
	DispatchPath   string
	BundlePath     string
	DevHub         string
	TargetOrg      string
	SFBin          string
	RemoteRoot     string
	LocalRoot      string

	runner remoteFailureCopyRunner
}

type OrchestratorSSHRawFetchReceipt struct {
	SchemaVersion             int             `json:"schemaVersion"`
	Status                    string          `json:"status"`
	Passed                    bool            `json:"passed"`
	CampaignID                string          `json:"campaignId"`
	JobID                     string          `json:"jobId"`
	ShardIndex                int             `json:"shardIndex"`
	Generation                int             `json:"generation"`
	SpecSHA256                string          `json:"specSha256"`
	PlanSHA256                string          `json:"planSha256"`
	LeaseSHA256               string          `json:"leaseSha256"`
	SSHReceiptSHA256          string          `json:"sshReceiptSha256"`
	TreeManifestSHA256        string          `json:"treeManifestSha256"`
	OrchestratorBindingSHA256 string          `json:"orchestratorBindingSha256"`
	SalesforceShardSHA256     string          `json:"salesforceShardSha256"`
	OrgCleanupSHA256          string          `json:"orgCleanupSha256"`
	CopyStdoutSHA256          string          `json:"copyStdoutSha256"`
	CopyStderrSHA256          string          `json:"copyStderrSha256"`
	ChecksumStdoutSHA256      string          `json:"checksumStdoutSha256"`
	ChecksumStderrSHA256      string          `json:"checksumStderrSha256"`
	ExecutedTools             RuntimeArtifact `json:"executedTools,omitzero"`
}

func FetchOrchestratorSSHRaw(request OrchestratorSSHRawFetchRequest) (OrchestratorSSHRawFetchReceipt, error) {
	planSHA, leaseSHA, sshSHA, err := validateOrchestratorSSHRawFetchRequest(request)
	if err != nil {
		return OrchestratorSSHRawFetchReceipt{}, err
	}
	if _, err := os.Lstat(request.LocalRoot); err == nil {
		return validateExistingOrchestratorSSHRawFetch(request, planSHA, leaseSHA, sshSHA)
	} else if !os.IsNotExist(err) {
		return OrchestratorSSHRawFetchReceipt{}, err
	}
	parent := filepath.Dir(request.LocalRoot)
	if err := ensureOrchestratorEvidenceRoot(parent); err != nil {
		return OrchestratorSSHRawFetchReceipt{}, err
	}
	temporary, err := os.MkdirTemp(parent, "."+filepath.Base(request.LocalRoot)+"-")
	if err != nil {
		return OrchestratorSSHRawFetchReceipt{}, err
	}
	defer os.RemoveAll(temporary)
	if err := os.Chmod(temporary, 0o700); err != nil {
		return OrchestratorSSHRawFetchReceipt{}, err
	}
	runner := request.runner
	if runner == nil {
		runner = runRemoteFailureCopy
	}
	source := request.Host + ":" + request.RemoteRoot + "/"
	ctx, cancel := context.WithTimeout(context.Background(), orchestratorSSHFetchTimeout)
	defer cancel()
	copyOutput, copyErr := runner(ctx, source, temporary, "", false)
	if copyErr != nil || copyOutput.ExitCode != 0 || ctx.Err() != nil {
		return OrchestratorSSHRawFetchReceipt{}, fmt.Errorf("orchestrator SSH raw copy failed")
	}
	checksumOutput, checksumErr := runner(ctx, source, temporary, "", true)
	if checksumErr != nil || checksumOutput.ExitCode != 0 || ctx.Err() != nil || len(strings.TrimSpace(string(append(append([]byte{}, checksumOutput.Stdout...), checksumOutput.Stderr...)))) != 0 {
		return OrchestratorSSHRawFetchReceipt{}, fmt.Errorf("orchestrator SSH raw checksum failed")
	}
	if err := validateOrchestratorSSHRawFiles(temporary, request.Dispatch, false); err != nil {
		return OrchestratorSSHRawFetchReceipt{}, err
	}
	if err := writeModeBearingTreeManifest(temporary); err != nil {
		return OrchestratorSSHRawFetchReceipt{}, err
	}
	manifestSHA, err := validateOrchestratorSSHRawManifest(temporary)
	if err != nil {
		return OrchestratorSSHRawFetchReceipt{}, err
	}
	receipt := orchestratorSSHRawFetchReceipt(request, planSHA, leaseSHA, sshSHA, manifestSHA, copyOutput, checksumOutput)
	if err := WriteNewJSON(filepath.Join(temporary, "SSH_FETCH.json"), receipt); err != nil {
		return OrchestratorSSHRawFetchReceipt{}, err
	}
	if err := syncOrchestratorWorkerTree(temporary); err != nil {
		return OrchestratorSSHRawFetchReceipt{}, err
	}
	if err := os.Rename(temporary, request.LocalRoot); err != nil {
		return OrchestratorSSHRawFetchReceipt{}, err
	}
	if err := syncOrchestratorWorkerDirectory(parent); err != nil {
		return OrchestratorSSHRawFetchReceipt{}, err
	}
	return receipt, nil
}

func validateOrchestratorSSHRawFetchRequest(request OrchestratorSSHRawFetchRequest) (string, string, string, error) {
	if err := validateOrchestratorWorkerPlanLease(request.Plan, request.Lease); err != nil || !safeRemoteSSHHost.MatchString(request.Host) || !safeOrchestratorToken(request.DevHub) || !safeOrchestratorToken(request.TargetOrg) {
		return "", "", "", fmt.Errorf("invalid orchestrator SSH raw fetch binding")
	}
	for _, path := range []string{request.WorkerBin, request.PlanPath, request.RemotePlanPath, request.LeasePath, request.DispatchPath, request.BundlePath, request.SFBin, request.RemoteRoot, request.LocalRoot} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return "", "", "", fmt.Errorf("absolute clean orchestrator SSH raw fetch paths are required")
		}
	}
	if !safeOrchestratorSSHRemotePath.MatchString(request.RemoteRoot) || request.RemoteRoot == "/" || request.LocalRoot == "/" {
		return "", "", "", fmt.Errorf("unsafe orchestrator SSH raw root")
	}
	plan, planBytes, err := readExactJSONBytes[OrchestratorCampaignPlan](request.PlanPath)
	if err != nil || !reflect.DeepEqual(plan, request.Plan) {
		return "", "", "", fmt.Errorf("orchestrator fetch plan drift")
	}
	lease, leaseBytes, err := readExactJSONBytes[OrchestratorLease](request.LeasePath)
	if err != nil || !reflect.DeepEqual(lease, request.Lease) {
		return "", "", "", fmt.Errorf("orchestrator fetch lease drift")
	}
	planSHA, leaseSHA := replayBytesSHA256(planBytes), replayBytesSHA256(leaseBytes)
	dispatch, sshBytes, err := readExactJSONBytes[OrchestratorSSHDispatchReceipt](request.DispatchPath)
	if err != nil || !reflect.DeepEqual(dispatch, request.Dispatch) {
		return "", "", "", fmt.Errorf("orchestrator fetch SSH receipt drift")
	}
	sshSHA := replayBytesSHA256(sshBytes)
	if dispatch.SchemaVersion != 1 || !dispatch.Passed || dispatch.Status != "worker-complete" || dispatch.ExitCode != 0 || dispatch.TimedOut || dispatch.FailureCode != "" || dispatch.ActionRequired || dispatch.ActionCode != "" || dispatch.CampaignID != request.Lease.CampaignID || dispatch.JobID != request.Lease.JobID || dispatch.ShardIndex != request.Lease.ShardIndex || dispatch.Generation != request.Lease.Generation || dispatch.SpecSHA256 != request.Plan.SpecSHA256 || dispatch.PlanSHA256 != planSHA || dispatch.LeaseSHA256 != leaseSHA || dispatch.TimeoutMS != orchestratorSSHTimeout.Milliseconds() || !sha256Pattern.MatchString(dispatch.StdoutSHA256) || !sha256Pattern.MatchString(dispatch.StderrSHA256) || !sha256Pattern.MatchString(dispatch.OrchestratorBindingSHA256) || !sha256Pattern.MatchString(dispatch.SalesforceShardSHA256) || !sha256Pattern.MatchString(dispatch.OrgCleanupSHA256) || !validOrchestratorExecutedTools(dispatch.ExecutedTools, request.Plan) {
		return "", "", "", fmt.Errorf("invalid completed SSH dispatch receipt")
	}
	commandRequest := OrchestratorSSHDispatchRequest{Host: request.Host, WorkerBin: request.WorkerBin, PlanPath: request.PlanPath, RemotePlanPath: request.RemotePlanPath, LeasePath: request.LeasePath, BundlePath: request.BundlePath, TargetOrg: request.TargetOrg, SFBin: request.SFBin, OutputRoot: request.RemoteRoot}
	command := orchestratorSSHWorkerOnceCommand(commandRequest, request.Plan.Definition.Tools.SHA256, request.Plan.Definition.ControlledInputSHA256[OrchestratorToolsAMD64Input], planSHA, leaseSHA, request.DevHub)
	args := []string{"-o", "BatchMode=yes", "--", request.Host, command}
	if dispatch.CommandSHA256 != commandSpecSHA256(ReplayCommand{Path: orchestratorSSHBinary, Args: args, Timeout: orchestratorSSHTimeout}) {
		return "", "", "", fmt.Errorf("SSH dispatch command binding drift")
	}
	return planSHA, leaseSHA, sshSHA, nil
}

func validateExistingOrchestratorSSHRawFetch(request OrchestratorSSHRawFetchRequest, planSHA, leaseSHA, sshSHA string) (OrchestratorSSHRawFetchReceipt, error) {
	if err := validateOrchestratorSSHRawFiles(request.LocalRoot, request.Dispatch, true); err != nil {
		return OrchestratorSSHRawFetchReceipt{}, err
	}
	receipt, _, err := readExactJSONBytes[OrchestratorSSHRawFetchReceipt](filepath.Join(request.LocalRoot, "SSH_FETCH.json"))
	manifestSHA, hashErr := validateOrchestratorSSHRawManifest(request.LocalRoot)
	if err != nil || hashErr != nil || !validOrchestratorSSHRawFetchReceipt(receipt, request, planSHA, leaseSHA, sshSHA, manifestSHA) {
		return OrchestratorSSHRawFetchReceipt{}, fmt.Errorf("existing orchestrator SSH raw fetch is invalid")
	}
	return receipt, nil
}

func validOrchestratorSSHRawFetchReceipt(receipt OrchestratorSSHRawFetchReceipt, request OrchestratorSSHRawFetchRequest, planSHA, leaseSHA, sshSHA, manifestSHA string) bool {
	return receipt.SchemaVersion == 1 && receipt.Status == "fetched" && receipt.Passed && receipt.CampaignID == request.Plan.CampaignID && receipt.JobID == request.Lease.JobID && receipt.ShardIndex == request.Lease.ShardIndex && receipt.Generation == request.Lease.Generation && receipt.SpecSHA256 == request.Plan.SpecSHA256 && receipt.PlanSHA256 == planSHA && receipt.LeaseSHA256 == leaseSHA && receipt.SSHReceiptSHA256 == sshSHA && receipt.TreeManifestSHA256 == manifestSHA && receipt.OrchestratorBindingSHA256 == request.Dispatch.OrchestratorBindingSHA256 && receipt.SalesforceShardSHA256 == request.Dispatch.SalesforceShardSHA256 && receipt.OrgCleanupSHA256 == request.Dispatch.OrgCleanupSHA256 && receipt.ExecutedTools == request.Dispatch.ExecutedTools && sha256Pattern.MatchString(receipt.CopyStdoutSHA256) && sha256Pattern.MatchString(receipt.CopyStderrSHA256) && sha256Pattern.MatchString(receipt.ChecksumStdoutSHA256) && sha256Pattern.MatchString(receipt.ChecksumStderrSHA256)
}

func orchestratorSSHRawFetchReceipt(request OrchestratorSSHRawFetchRequest, planSHA, leaseSHA, sshSHA, manifestSHA string, copyOutput, checksumOutput salesforceCommandOutput) OrchestratorSSHRawFetchReceipt {
	return OrchestratorSSHRawFetchReceipt{
		SchemaVersion: 1, Status: "fetched", Passed: true, CampaignID: request.Plan.CampaignID, JobID: request.Lease.JobID, ShardIndex: request.Lease.ShardIndex, Generation: request.Lease.Generation,
		SpecSHA256: request.Plan.SpecSHA256, PlanSHA256: planSHA, LeaseSHA256: leaseSHA, SSHReceiptSHA256: sshSHA, TreeManifestSHA256: manifestSHA,
		OrchestratorBindingSHA256: request.Dispatch.OrchestratorBindingSHA256, SalesforceShardSHA256: request.Dispatch.SalesforceShardSHA256, OrgCleanupSHA256: request.Dispatch.OrgCleanupSHA256,
		ExecutedTools:    request.Dispatch.ExecutedTools,
		CopyStdoutSHA256: replayBytesSHA256(copyOutput.Stdout), CopyStderrSHA256: replayBytesSHA256(copyOutput.Stderr), ChecksumStdoutSHA256: replayBytesSHA256(checksumOutput.Stdout), ChecksumStderrSHA256: replayBytesSHA256(checksumOutput.Stderr),
	}
}

func validateOrchestratorSSHRawFiles(root string, dispatch OrchestratorSSHDispatchReceipt, generated bool) error {
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 {
		return fmt.Errorf("orchestrator SSH raw root must be a mode-0700 directory")
	}
	want := map[string]os.FileMode{}
	for _, name := range orchestratorSSHRawFileNames() {
		want[name] = 0o600
	}
	want["ORCHESTRATOR_BINDING.json"] = 0o400
	if generated {
		want["TREE_MANIFEST.json"], want["SSH_FETCH.json"] = 0o600, 0o600
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != len(want) {
		return fmt.Errorf("orchestrator SSH raw file set is incomplete")
	}
	for _, entry := range entries {
		mode, ok := want[entry.Name()]
		path := filepath.Join(root, entry.Name())
		info, statErr := os.Lstat(path)
		if !ok || statErr != nil || !info.Mode().IsRegular() || info.Mode().Perm() != mode {
			return fmt.Errorf("orchestrator SSH raw file set is invalid")
		}
	}
	for name, hash := range map[string]string{"ORCHESTRATOR_BINDING.json": dispatch.OrchestratorBindingSHA256, "SALESFORCE_SHARD.json": dispatch.SalesforceShardSHA256, "ORG_CLEANUP.json": dispatch.OrgCleanupSHA256} {
		if actual, err := sha256File(filepath.Join(root, name)); err != nil || actual != hash {
			return fmt.Errorf("orchestrator SSH raw hash drift")
		}
	}
	return nil
}

func orchestratorSSHRawFileNames() []string {
	return []string{"ORCHESTRATOR_BINDING.json", "ORG_CREATION.json", "ORG_CREATION.json.reservation", "ORG_PREFLIGHT.json", "SALESFORCE_DISPATCH.json", "SALESFORCE_SHARD.json", "ORG_CLEANUP.json"}
}

type orchestratorSSHRawManifestEntry struct {
	Path   string `json:"path"`
	Mode   string `json:"mode"`
	SHA256 string `json:"sha256"`
}

func validateOrchestratorSSHRawManifest(root string) (string, error) {
	manifestPath := filepath.Join(root, "TREE_MANIFEST.json")
	manifest, _, err := readExactJSONBytes[[]orchestratorSSHRawManifestEntry](manifestPath)
	if err != nil {
		return "", err
	}
	expected := make([]orchestratorSSHRawManifestEntry, 0, len(orchestratorSSHRawFileNames()))
	for _, name := range orchestratorSSHRawFileNames() {
		info, err := os.Lstat(filepath.Join(root, name))
		hash, hashErr := sha256File(filepath.Join(root, name))
		if err != nil || hashErr != nil || !info.Mode().IsRegular() {
			return "", fmt.Errorf("orchestrator SSH raw manifest input is invalid")
		}
		expected = append(expected, orchestratorSSHRawManifestEntry{Path: name, Mode: fmt.Sprintf("%04o", info.Mode().Perm()), SHA256: hash})
	}
	sort.Slice(expected, func(i, j int) bool { return expected[i].Path < expected[j].Path })
	if !reflect.DeepEqual(manifest, expected) {
		return "", fmt.Errorf("orchestrator SSH raw manifest drift")
	}
	return sha256File(manifestPath)
}
