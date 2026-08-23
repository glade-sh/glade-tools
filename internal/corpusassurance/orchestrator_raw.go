package corpusassurance

import (
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
	if shardCount != 2 {
		return result, fmt.Errorf("raw Salesforce shard count must be two")
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
