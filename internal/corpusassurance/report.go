package corpusassurance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/glade-sh/glade/tools/internal/surfaceledger"
)

// AssuranceReport is the public, neutral release outcome projection.
type AssuranceReport struct {
	SchemaVersion         int                             `json:"schemaVersion"`
	Rows                  []AssuranceSurfaceRow           `json:"rows"`
	RepositorySurfaceRows []AssuranceRepositorySurfaceRow `json:"repositorySurfaceRows"`
	RepositorySummaries   []AssuranceRepositorySummary    `json:"repositorySummaries"`
}

// AssuranceReceipt deliberately does not hash itself, keeping the sealed
// artifact graph acyclic.
type AssuranceReceipt struct {
	SchemaVersion   int               `json:"schemaVersion"`
	AssuranceSHA256 string            `json:"assuranceSha256"`
	HTMLSHA256      string            `json:"htmlSha256"`
	InputsSHA256    map[string]string `json:"inputsSha256"`
	ReceiptSHA256   string            `json:"receiptSha256,omitempty"`
}

// AssuranceReportRequest names every direct, sealed input required to publish
// the final public readiness projection.
type AssuranceReportRequest struct {
	InventoryPath               string
	RootManifestPath            string
	LedgerPath                  string
	SourceProfilePath           string
	PolicyPath                  string
	DecisionPath                string
	UsagePath                   string
	ProfilePath                 string
	FixtureManifestPath         string
	ReplayPath                  string
	ReplayHostManifestPaths     []string
	ReplayShardPaths            []string
	AttemptPath                 string
	LocalProofPath              string
	OraclePlanPath              string
	ExclusionRequestPath        string
	ExclusionPolicyPath         string
	AuthorityPath               string
	ReleaseValidationPath       string
	BundlePath                  string
	FilterScriptPath            string
	ScratchDefinitionPath       string
	ToolsAMD64Path              string
	SalesforceFiles             []SalesforceShardFiles
	RemoteCleanupPaths          []string
	RemoteCleanupAuthorityPaths []string
	JSONPath                    string
	HTMLPath                    string
	ReceiptPath                 string
	remoteRunner                salesforceCommandRunner
}

// BuildAssuranceReport derives public outcomes only from sealed files, then
// writes the report, explorer, and acyclic receipt once.
func BuildAssuranceReport(request AssuranceReportRequest) (AssuranceReceipt, error) {
	if err := requiredReportEvidencePaths(request); err != nil {
		return AssuranceReceipt{}, err
	}
	initialInputHashes, err := snapshotReportInputHashes(request)
	if err != nil {
		return AssuranceReceipt{}, err
	}
	defer clearReportSnapshot()
	inventory, inventoryBytes, err := readInventorySpec(request.InventoryPath)
	if err != nil {
		return AssuranceReceipt{}, err
	}
	root, rootBytes, err := readExactJSONBytes[InventoryManifest](request.RootManifestPath)
	if err != nil || root.InventorySHA256 != replayBytesSHA256(inventoryBytes) || ValidateInventoryCoverage(inventory, root.Repositories) != nil {
		return AssuranceReceipt{}, fmt.Errorf("root manifest does not bind frozen inventory")
	}
	usage, usageBytes, err := readExactJSONBytes[SealedCorpusUsage](request.UsagePath)
	if err != nil || usage.InventorySHA256 != replayBytesSHA256(inventoryBytes) || usage.RootManifestSHA256 != replayBytesSHA256(rootBytes) {
		return AssuranceReceipt{}, fmt.Errorf("sealed usage does not bind frozen inventory")
	}
	profile, profileBytes, err := readExactJSONBytes[AssuranceProfile](request.ProfilePath)
	if err != nil || profile.SchemaVersion != 1 || profile.SealedUsageSHA256 != replayBytesSHA256(usageBytes) {
		return AssuranceReceipt{}, fmt.Errorf("assurance profile does not bind sealed usage")
	}
	manifest, manifestBytes, err := readExactJSONBytes[LocalProofFixtureManifest](request.FixtureManifestPath)
	if err != nil || profile.FixtureManifestSHA256 != replayBytesSHA256(manifestBytes) {
		return AssuranceReceipt{}, fmt.Errorf("fixture manifest does not bind assurance profile")
	}
	proof, proofBytes, err := readExactJSONBytes[LocalProof](request.LocalProofPath)
	if err != nil || profile.LocalProofSHA256 != replayBytesSHA256(proofBytes) || ValidateLocalProof(proof, manifest) != nil {
		return AssuranceReceipt{}, fmt.Errorf("local proof does not bind assurance profile")
	}
	plan, planBytes, err := readExactJSONBytes[OraclePlan](request.OraclePlanPath)
	if err != nil || plan.ProfileSHA256 != replayBytesSHA256(profileBytes) || plan.SealedUsageSHA256 != replayBytesSHA256(usageBytes) || plan.LocalProofSHA256 != replayBytesSHA256(proofBytes) {
		return AssuranceReceipt{}, fmt.Errorf("oracle plan does not bind current evidence")
	}
	authority, authorityBytes, err := readExactJSONBytes[ExclusionAuthority](request.AuthorityPath)
	if err != nil || authority.PlanSHA256 != replayBytesSHA256(planBytes) || authority.ProfileSHA256 != replayBytesSHA256(profileBytes) || authority.SealedUsageSHA256 != replayBytesSHA256(usageBytes) || authority.LocalProofSHA256 != replayBytesSHA256(proofBytes) || authority.SalesforceParityCredit != 0 {
		return AssuranceReceipt{}, fmt.Errorf("exclusion authority does not bind current evidence")
	}
	sidecarInputs, err := validateReportSidecarEvidence(request, usage, profile, proof, manifest, plan, authority, replayBytesSHA256(usageBytes), replayBytesSHA256(profileBytes), replayBytesSHA256(planBytes))
	if err != nil {
		return AssuranceReceipt{}, err
	}
	if err := ValidateOracleBundle(request.BundlePath); err != nil {
		return AssuranceReceipt{}, err
	}
	bundle, bundleBytes, err := readExactJSONBytes[OracleBundle](request.BundlePath)
	if err != nil || bundle.OraclePlanSHA256 != replayBytesSHA256(planBytes) || bundle.Candidate != plan.Candidate || bundle.Tools != plan.Tools {
		return AssuranceReceipt{}, fmt.Errorf("oracle bundle does not bind current plan")
	}
	stagedPlanPath := filepath.Join(filepath.Dir(request.BundlePath), "ORACLE_PLAN.json")
	var salesforceSnapshots []salesforceShardEvidenceSnapshot
	if err := validateSalesforceShardFiles(stagedPlanPath, request.SalesforceFiles, &salesforceSnapshots); err != nil {
		return AssuranceReceipt{}, err
	}
	shards := make([]SalesforceShard, 0, len(salesforceSnapshots))
	for _, snapshot := range salesforceSnapshots {
		shards = append(shards, snapshot.Shard)
	}
	merge, mergeBytes, err := readExactJSONBytes[ReplayMerge](request.ReplayPath)
	if err != nil || merge.RootManifestSHA256 != replayBytesSHA256(rootBytes) || merge.Candidate != plan.Candidate || merge.Tools != plan.Tools {
		return AssuranceReceipt{}, fmt.Errorf("replay merge does not bind current evidence")
	}
	if err := validateReplayRootBinding(merge, root); err != nil {
		return AssuranceReceipt{}, fmt.Errorf("replay merge does not bind current root manifest: %w", err)
	}
	replayInputs, err := validateReportReplayEvidence(request, merge)
	if err != nil {
		return AssuranceReceipt{}, err
	}
	rows, err := deriveAssuranceRows(usage.Reconciliation, profile, proof, plan, shards, merge.TestReadyByRepository)
	if err != nil {
		return AssuranceReceipt{}, err
	}
	repositoryRows, repositorySummaries, err := deriveRepositoryAssuranceRows(inventory, rows, merge.TestReadyByRepository)
	if err != nil {
		return AssuranceReceipt{}, err
	}
	inputs := map[string]string{"IN_SCOPE.json": replayBytesSHA256(inventoryBytes), "MANIFEST.json": replayBytesSHA256(rootBytes), "CORPUS_USAGE.json": replayBytesSHA256(usageBytes), "ASSURANCE_PROFILE.json": replayBytesSHA256(profileBytes), "FIXTURE_MANIFEST.json": replayBytesSHA256(manifestBytes), "REPLAY.json": replayBytesSHA256(mergeBytes), "LOCAL_PROOF.json": replayBytesSHA256(proofBytes), "ORACLE_PLAN.json": replayBytesSHA256(planBytes), "EXCLUSION_AUTHORITY.json": replayBytesSHA256(authorityBytes), "ORACLE_BUNDLE.json": replayBytesSHA256(bundleBytes)}
	for name, hash := range sidecarInputs {
		inputs[name] = hash
	}
	for name, hash := range replayInputs {
		inputs[name] = hash
	}
	for index, snapshot := range salesforceSnapshots {
		for name, hash := range snapshot.Inputs {
			inputs[fmt.Sprintf("salesforce-%s-%d.json", name, index)] = hash
		}
	}
	for index, path := range reportInputPaths(request) {
		inputs[fmt.Sprintf("DIRECT_INPUT_%03d", index)] = initialInputHashes[path]
	}
	if err := revalidateReportInputHashes(request, initialInputHashes); err != nil {
		return AssuranceReceipt{}, err
	}
	return writeAssuranceArtifacts(AssuranceReport{SchemaVersion: 1, Rows: rows, RepositorySurfaceRows: repositoryRows, RepositorySummaries: repositorySummaries}, request.JSONPath, request.HTMLPath, request.ReceiptPath, inputs)
}

func requiredReportEvidencePaths(request AssuranceReportRequest) error {
	paths := []string{request.InventoryPath, request.RootManifestPath, request.LedgerPath, request.SourceProfilePath, request.PolicyPath, request.DecisionPath, request.UsagePath, request.ProfilePath, request.FixtureManifestPath, request.ReplayPath, request.AttemptPath, request.LocalProofPath, request.OraclePlanPath, request.ExclusionRequestPath, request.ExclusionPolicyPath, request.AuthorityPath, request.ReleaseValidationPath, request.BundlePath, request.FilterScriptPath, request.ScratchDefinitionPath, request.ToolsAMD64Path, request.JSONPath, request.HTMLPath, request.ReceiptPath}
	for _, path := range paths {
		if !filepath.IsAbs(path) {
			return fmt.Errorf("absolute direct assurance paths are required")
		}
	}
	if len(request.SalesforceFiles) == 0 || len(request.RemoteCleanupPaths) != 2 || len(request.RemoteCleanupAuthorityPaths) != 2 {
		return fmt.Errorf("complete Salesforce and remote cleanup evidence is required")
	}
	if len(request.ReplayHostManifestPaths) != 2 || len(request.ReplayShardPaths) != 2 {
		return fmt.Errorf("complete replay manifest and shard evidence is required")
	}
	for _, path := range append(append([]string{}, request.ReplayHostManifestPaths...), request.ReplayShardPaths...) {
		if !filepath.IsAbs(path) {
			return fmt.Errorf("absolute replay evidence paths are required")
		}
	}
	for _, path := range request.RemoteCleanupPaths {
		if !filepath.IsAbs(path) {
			return fmt.Errorf("absolute remote cleanup paths are required")
		}
	}
	for _, path := range request.RemoteCleanupAuthorityPaths {
		if !filepath.IsAbs(path) {
			return fmt.Errorf("absolute remote cleanup authority paths are required")
		}
	}
	return nil
}

func validateReportReplayEvidence(request AssuranceReportRequest, merge ReplayMerge) (map[string]string, error) {
	recomputed, err := loadReplayMergeFromFiles(request.InventoryPath, request.RootManifestPath, request.ReplayHostManifestPaths, request.ReplayShardPaths)
	if err != nil || !reflect.DeepEqual(recomputed, merge) {
		return nil, fmt.Errorf("replay shards do not rederive the sealed replay merge")
	}
	shards := make([]ReplayShard, 0, len(request.ReplayShardPaths))
	inputs := make(map[string]string, len(request.ReplayShardPaths))
	for _, path := range request.ReplayShardPaths {
		shard, data, err := readExactJSONBytes[ReplayShard](path)
		if err != nil || (shard.Host != "local" && shard.Host != "replay-worker") || inputs["REPLAY_SHARD_"+strings.ToUpper(shard.Host)+".json"] != "" {
			return nil, fmt.Errorf("invalid report replay shard evidence")
		}
		inputs["REPLAY_SHARD_"+strings.ToUpper(shard.Host)+".json"] = replayBytesSHA256(data)
		shards = append(shards, shard)
	}
	if err := ValidateReplayMerge(merge, shards); err != nil || len(inputs) != 2 {
		return nil, fmt.Errorf("replay shards do not validate retained evidence")
	}
	for _, path := range request.ReplayShardPaths {
		shard, data, err := readExactJSONBytes[ReplayShard](path)
		if err != nil {
			return nil, fmt.Errorf("replay shard changed during report generation")
		}
		if inputs["REPLAY_SHARD_"+strings.ToUpper(shard.Host)+".json"] != replayBytesSHA256(data) {
			return nil, fmt.Errorf("replay shard changed during report generation")
		}
	}
	return inputs, nil
}

func validateReportSidecarEvidence(request AssuranceReportRequest, usage SealedCorpusUsage, profile AssuranceProfile, proof LocalProof, manifest LocalProofFixtureManifest, plan OraclePlan, authority ExclusionAuthority, usageSHA, profileSHA, planSHA string) (map[string]string, error) {
	cleanupRoots, err := expectedReportCleanupRoots(request)
	if err != nil {
		return nil, err
	}
	ledger, ledgerBytes, err := readExactJSONBytes[surfaceledger.SurfaceLedger](request.LedgerPath)
	if err != nil || len(ledger.Rows) == 0 {
		return nil, fmt.Errorf("read report ledger")
	}
	_, _, sourceBytes, err := readUsageProfileRows(request.SourceProfilePath)
	if err != nil {
		return nil, fmt.Errorf("read report source profile: %w", err)
	}
	policy, policyBytes, err := readExactJSONBytes[surfaceledger.SupportPolicy](request.PolicyPath)
	if err != nil || len(policy.Rules) == 0 {
		return nil, fmt.Errorf("read report support policy")
	}
	decisions, decisionBytes, err := readExactJSONBytes[UsageDecisionFile](request.DecisionPath)
	if err != nil || decisions.SchemaVersion != 1 {
		return nil, fmt.Errorf("read report usage decisions")
	}
	ledgerSHA, sourceSHA, policySHA, decisionSHA := replayBytesSHA256(ledgerBytes), replayBytesSHA256(sourceBytes), replayBytesSHA256(policyBytes), replayBytesSHA256(decisionBytes)
	if usage.LedgerSHA256 != ledgerSHA || usage.ProfileSHA256 != sourceSHA || usage.PolicySHA256 != policySHA || usage.DecisionSHA256 != decisionSHA || profile.SourceProfileSHA256 != sourceSHA || profile.LedgerSHA256 != ledgerSHA || profile.PolicySHA256 != policySHA || decisions.ProfileSHA256 != sourceSHA || decisions.PolicySHA256 != policySHA {
		return nil, fmt.Errorf("report usage sidecars do not bind sealed evidence")
	}
	exclusion, exclusionBytes, err := readExactJSONBytes[ExclusionRequest](request.ExclusionRequestPath)
	if err != nil {
		return nil, fmt.Errorf("read report exclusion request: %w", err)
	}
	exclusionPolicy, exclusionPolicyBytes, err := readExactJSONBytes[ExclusionPolicy](request.ExclusionPolicyPath)
	if err != nil || exclusionPolicy.SchemaVersion != 1 {
		return nil, fmt.Errorf("report exclusion policy does not authorize every exclusion")
	}
	if !validReportExclusionPartition(exclusion, authority, plan, planSHA, profileSHA, usageSHA, decisionSHA, replayBytesSHA256(exclusionPolicyBytes)) {
		return nil, fmt.Errorf("report exclusions do not form the authorized plan partition")
	}
	if !policyAuthorizesRows(exclusionPolicy.Rows, authority.Rows) {
		return nil, fmt.Errorf("report exclusion policy does not authorize every exclusion")
	}
	release, releaseBytes, err := readExactJSONBytes[ReleaseValidation](request.ReleaseValidationPath)
	if err != nil || validateOracleReleaseValidation(release, plan) != nil {
		return nil, fmt.Errorf("report release validation does not bind oracle plan")
	}
	if err := validateOracleReleaseSources(release, plan); err != nil {
		return nil, fmt.Errorf("report release provenance is unavailable: %w", err)
	}
	if err := verifyLocalProofReplay(proof, manifest, release.CandidatePath, release.ToolsPath, nil); err != nil {
		return nil, fmt.Errorf("final local proof replay failed: %w", err)
	}
	bundle, _, err := readExactJSONBytes[OracleBundle](request.BundlePath)
	if err != nil {
		return nil, fmt.Errorf("read report oracle bundle: %w", err)
	}
	filterSHA, filterErr := sha256File(request.FilterScriptPath)
	scratchSHA, scratchErr := sha256File(request.ScratchDefinitionPath)
	toolsSHA, toolsErr := sha256File(request.ToolsAMD64Path)
	if filterErr != nil || scratchErr != nil || toolsErr != nil || filterSHA != bundle.FilterSHA256 || scratchSHA != bundle.ScratchDefinitionSHA256 || toolsSHA != bundle.ToolsAMD64SHA256 || replayBytesSHA256(releaseBytes) != bundle.ReleaseValidationSHA256 {
		return nil, fmt.Errorf("report bundle execution sidecars are unavailable")
	}
	attempt, _, err := readExactJSONBytes[AssuranceAttempt](request.AttemptPath)
	if err != nil || ValidateAssuranceAttempt(attempt) != nil {
		return nil, fmt.Errorf("read report sealed attempt")
	}
	attemptSHA := attemptBindingHash(attempt)
	remoteCleanupSHA, err := sha256File(remoteCleanupBinary)
	if err != nil {
		return nil, fmt.Errorf("hash remote cleanup executable: %w", err)
	}
	cleanupInputs := map[string]string{}
	for index, path := range request.RemoteCleanupPaths {
		cleanup, bytes, err := readExactJSONBytes[RemoteAttemptCleanupReceipt](path)
		authority, authorityBytes, authorityErr := readRemoteAttemptAuthority(request.RemoteCleanupAuthorityPaths[index])
		if err != nil || authorityErr != nil || cleanupRoots[authority.Role] != authority.AttemptRoot || !remoteCleanupAuthorityMatches(attempt, authority, replayBytesSHA256(authorityBytes)) || cleanup.SchemaVersion != 1 || cleanup.AttemptSHA256 != attemptSHA || cleanup.Role != authority.Role || cleanup.Host != authority.Host || cleanup.Parent != authority.Parent || cleanup.AttemptRoot != authority.AttemptRoot || cleanup.BindingSHA256 != replayBytesSHA256(authorityBytes) || cleanup.BindingSHA256 != cleanup.BindingPostSHA256 || !cleanup.ResidueAbsent || !sha256Pattern.MatchString(cleanup.BindingSHA256) || cleanup.TimeoutMS != remoteCleanupTimeout.Milliseconds() || !validRetainedCommandOutput(cleanup.Command) || !cleanup.Command.Passed || cleanup.Command.ExitCode != 0 || cleanup.Command.TimedOut || !equalStrings(cleanup.Command.Command, []string{remoteCleanupBinary, "-o", "BatchMode=yes", cleanup.Host, remoteAttemptCleanupShellCommand(cleanup.Parent, filepath.Base(cleanup.AttemptRoot))}) || cleanup.Command.CommandSpecSHA256 != commandSpecSHA256(ReplayCommand{Path: remoteCleanupBinary, Args: cleanup.Command.Command, Timeout: remoteCleanupTimeout}) || cleanup.Command.ExecutableSHA256 != remoteCleanupSHA || cleanup.Command.ExecutableSHA256 != cleanup.Command.ExecutableAfterSHA256 || !sha256Pattern.MatchString(cleanup.Command.ExecutableSHA256) || cleanupInputs[cleanup.Role] != "" {
			return nil, fmt.Errorf("invalid report remote cleanup receipt")
		}
		if err := verifyRemoteAttemptAbsent(authority, request.remoteRunner); err != nil {
			return nil, fmt.Errorf("remote cleanup receipt is not independently verified: %w", err)
		}
		cleanupInputs[cleanup.Role] = replayBytesSHA256(bytes)
	}
	if len(cleanupInputs) != 2 || cleanupInputs["replay-worker"] == "" || cleanupInputs["salesforce-worker"] == "" {
		return nil, fmt.Errorf("report requires one cleanup receipt per worker role")
	}
	return map[string]string{"SURFACE_LEDGER.json": ledgerSHA, "SOURCE_PROFILE.json": sourceSHA, "SUPPORT_POLICY.json": policySHA, "USAGE_DECISIONS.json": decisionSHA, "EXCLUSION_REQUEST.json": replayBytesSHA256(exclusionBytes), "EXCLUSION_POLICY.json": replayBytesSHA256(exclusionPolicyBytes), "RELEASE_VALIDATION.json": replayBytesSHA256(releaseBytes), "FILTER_SCRIPT.py": filterSHA, "SCRATCH_DEFINITION.json": scratchSHA, "TOOLS_AMD64": toolsSHA, "REMOTE_CLEANUP_REPLAY_WORKER.json": cleanupInputs["replay-worker"], "REMOTE_CLEANUP_SALESFORCE_WORKER.json": cleanupInputs["salesforce-worker"]}, nil
}

func validReportExclusionPartition(exclusion ExclusionRequest, authority ExclusionAuthority, plan OraclePlan, planSHA, profileSHA, usageSHA, decisionSHA, exclusionPolicySHA string) bool {
	expected, err := exclusionRowsFromPlan(plan)
	return err == nil && exclusion.Candidate == plan.Candidate && exclusion.Tools == plan.Tools && exclusion.PlanSHA256 == planSHA && exclusion.ProfileSHA256 == profileSHA && exclusion.SealedUsageSHA256 == usageSHA && exclusion.DecisionSHA256 == decisionSHA && exclusion.LocalProofSHA256 == plan.LocalProofSHA256 && reflect.DeepEqual(exclusion.Rows, expected) && authority.Candidate == plan.Candidate && authority.Tools == plan.Tools && authority.DecisionSHA256 == decisionSHA && authority.PolicySHA256 == exclusionPolicySHA && reflect.DeepEqual(authority.Rows, exclusion.Rows)
}

func expectedReportCleanupRoots(request AssuranceReportRequest) (map[string]string, error) {
	roots := map[string]string{}
	for _, path := range request.ReplayShardPaths {
		shard, _, err := readExactJSONBytes[ReplayShard](path)
		if err != nil || (shard.Host != "local" && shard.Host != "replay-worker") {
			return nil, fmt.Errorf("replay cleanup root is not sealed by the replay shard")
		}
		if shard.Host == "local" {
			continue
		}
		if !filepath.IsAbs(shard.AttemptRoot) || roots[shard.Host] != "" {
			return nil, fmt.Errorf("replay cleanup root is not sealed by the replay shard")
		}
		roots[shard.Host] = filepath.Clean(shard.AttemptRoot)
	}
	for _, files := range request.SalesforceFiles {
		shard, _, err := readExactJSONBytes[SalesforceShard](files.ShardPath)
		if err != nil || !filepath.IsAbs(shard.ExecutorRoot) {
			return nil, fmt.Errorf("Salesforce cleanup root is not sealed by the shard")
		}
		root := filepath.Clean(filepath.Dir(filepath.Dir(shard.ExecutorRoot)))
		if roots["salesforce-worker"] != "" && roots["salesforce-worker"] != root {
			return nil, fmt.Errorf("Salesforce shards do not share one cleanup root")
		}
		roots["salesforce-worker"] = root
	}
	if roots["replay-worker"] == "" || roots["salesforce-worker"] == "" {
		return nil, fmt.Errorf("report requires sealed cleanup roots for both workers")
	}
	return roots, nil
}

func policyAuthorizesRows(policy, rows []ExclusionPolicyRow) bool {
	allowed := map[string]ExclusionPolicyRow{}
	for _, row := range policy {
		allowed[row.SurfaceID] = row
	}
	for _, row := range rows {
		if allowed[row.SurfaceID] != row {
			return false
		}
	}
	return true
}

func snapshotReportInputHashes(request AssuranceReportRequest) (map[string]string, error) {
	hashes := map[string]string{}
	files := map[string][]byte{}
	for _, path := range reportInputPaths(request) {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("hash report input %s: %w", path, err)
		}
		files[path] = append([]byte(nil), data...)
		hashes[path] = replayBytesSHA256(data)
	}
	setReportSnapshot(files)
	return hashes, nil
}

func revalidateReportInputHashes(request AssuranceReportRequest, expected map[string]string) error {
	for _, path := range reportInputPaths(request) {
		hash, err := sha256FileDirect(path)
		if err != nil || hash != expected[path] {
			return fmt.Errorf("report input changed during generation: %s", path)
		}
	}
	return nil
}

func reportInputPaths(request AssuranceReportRequest) []string {
	paths := []string{request.InventoryPath, request.RootManifestPath, request.LedgerPath, request.SourceProfilePath, request.PolicyPath, request.DecisionPath, request.UsagePath, request.ProfilePath, request.FixtureManifestPath, request.ReplayPath, request.AttemptPath, request.LocalProofPath, request.OraclePlanPath, request.ExclusionRequestPath, request.ExclusionPolicyPath, request.AuthorityPath, request.ReleaseValidationPath, request.BundlePath, request.FilterScriptPath, request.ScratchDefinitionPath, request.ToolsAMD64Path}
	paths = append(paths, request.ReplayHostManifestPaths...)
	paths = append(paths, request.ReplayShardPaths...)
	paths = append(paths, request.RemoteCleanupPaths...)
	paths = append(paths, request.RemoteCleanupAuthorityPaths...)
	for _, files := range request.SalesforceFiles {
		paths = append(paths, files.ShardPath, files.DispatchPath, files.CreationPath, files.CleanupPath, files.PreflightPath)
	}
	seen := make(map[string]bool, len(paths))
	unique := make([]string, 0, len(paths))
	for _, path := range paths {
		if !seen[path] {
			seen[path] = true
			unique = append(unique, path)
		}
	}
	return unique
}

// AssuranceSurfaceRow is the public, neutral per-surface release outcome.
// Readiness is cumulative: runtime parity implies test and compile readiness.
// Explicit non-parity can retain independently proven local readiness.
type AssuranceSurfaceRow struct {
	Namespace          string   `json:"namespace"`
	SurfaceID          string   `json:"surfaceId"`
	UsageKeys          []string `json:"usageKeys"`
	RepositoryIDs      []string `json:"repositoryIds"`
	PrivateProdRefs    int      `json:"privateProdRefs"`
	PrivateTestRefs    int      `json:"privateTestRefs"`
	Disposition        string   `json:"disposition"`
	LocalEvidence      string   `json:"localEvidence"`
	SalesforceAction   string   `json:"salesforceAction"`
	SalesforceEvidence string   `json:"salesforceEvidence"`
	ExclusionClass     string   `json:"exclusionClass,omitempty"`
	ExclusionReason    string   `json:"exclusionReason,omitempty"`
	FixtureIDs         []string `json:"fixtureIds"`
	CompileReady       bool     `json:"compileReady"`
	TestReady          bool     `json:"testReady"`
	RuntimeParityReady bool     `json:"runtimeParityReady"`
	NonParity          bool     `json:"nonParity"`
}

type AssuranceRepositorySurfaceRow struct {
	RepositoryID string `json:"repositoryId"`
	AssuranceSurfaceRow
}

type AssuranceRepositorySummary struct {
	RepositoryID       string `json:"repositoryId"`
	SurfaceCount       int    `json:"surfaceCount"`
	CompileReady       bool   `json:"compileReady"`
	TestReady          bool   `json:"testReady"`
	RuntimeParityReady bool   `json:"runtimeParityReady"`
	NonParity          bool   `json:"nonParity"`
	NonParityReason    string `json:"nonParityReason,omitempty"`
}

func ValidateAssuranceOutcomes(rows []AssuranceSurfaceRow) error {
	if len(rows) == 0 {
		return fmt.Errorf("assurance outcome rows are required")
	}
	seen := make(map[string]bool, len(rows))
	for _, row := range rows {
		if row.SurfaceID == "" || seen[row.SurfaceID] {
			return fmt.Errorf("invalid or duplicate assurance surface %q", row.SurfaceID)
		}
		seen[row.SurfaceID] = true
		if row.NonParity {
			if row.RuntimeParityReady || (row.TestReady && !row.CompileReady) || row.ExclusionClass == "" || row.ExclusionReason == "" {
				return fmt.Errorf("invalid non-parity outcome for %q", row.SurfaceID)
			}
			continue
		}
		if !row.CompileReady || (row.TestReady && !row.CompileReady) || (row.RuntimeParityReady && !row.TestReady) {
			return fmt.Errorf("invalid readiness outcome for %q", row.SurfaceID)
		}
		if row.ExclusionClass != "" || row.ExclusionReason != "" {
			return fmt.Errorf("parity outcome carries exclusion for %q", row.SurfaceID)
		}
	}
	return nil
}

func repositoryTestReadiness(merge ReplayMerge, shards []ReplayShard) (map[string]bool, error) {
	repositories := make(map[string]RepositorySpec, len(merge.Repositories))
	for _, repository := range merge.Repositories {
		if repository.ID == "" || repositories[repository.ID].ID != "" || (repository.LocalTests != "required" && repository.LocalTests != "tests-not-present") {
			return nil, fmt.Errorf("invalid replay repository %q", repository.ID)
		}
		repositories[repository.ID] = repository
	}
	ready := make(map[string]bool, len(repositories))
	seen := make(map[string]map[int]bool, len(repositories))
	for _, shard := range shards {
		for _, result := range shard.Repositories {
			repository, exists := repositories[result.RepositoryID]
			if !exists || !result.Check.Passed || result.Check.ExitCode != 0 {
				return nil, fmt.Errorf("invalid replay result for %q", result.RepositoryID)
			}
			if err := validateReplayResultShard(repository, shard.Host, result); err != nil {
				return nil, err
			}
			if seen[result.RepositoryID] == nil {
				seen[result.RepositoryID] = map[int]bool{}
			}
			if seen[result.RepositoryID][result.TestShardIndex] {
				return nil, fmt.Errorf("duplicate replay test shard %d for %q", result.TestShardIndex, result.RepositoryID)
			}
			seen[result.RepositoryID][result.TestShardIndex] = true
			if repository.LocalTests == "required" {
				if result.LocalTest == nil || !result.LocalTest.Passed || result.LocalTest.ExitCode != 0 {
					return nil, fmt.Errorf("required local test failed for %q", result.RepositoryID)
				}
			} else if result.LocalTest != nil {
				return nil, fmt.Errorf("repository %q unexpectedly ran local tests", result.RepositoryID)
			}
		}
	}
	for id, repository := range repositories {
		if len(seen[id]) != replayResultCount(repository) {
			return nil, fmt.Errorf("replay result coverage is incomplete for %q", id)
		}
		ready[id] = repository.LocalTests == "required"
	}
	return ready, nil
}

// deriveAssuranceRows joins only the sealed reconciliation inputs. It never
// accepts a caller-selected surface list; every mapped usage surface gets one
// explicit readiness or non-parity outcome.
func deriveAssuranceRows(usage UsageReconciliation, profile AssuranceProfile, proof LocalProof, plan OraclePlan, shards []SalesforceShard, repositoryTests map[string]bool) ([]AssuranceSurfaceRow, error) {
	type usageAggregate struct {
		namespace    string
		usageKeys    []string
		repositories map[string]bool
		prod, test   int
	}
	aggregates := map[string]*usageAggregate{}
	for _, entry := range usage.Usage {
		switch entry.Class {
		case usageClassExact, usageClassCaseAlias, usageClassAggregateParent, usageClassCanonicalAlias:
			if entry.SurfaceID == "" || entry.Namespace == "" || len(entry.RepositoryIDs) == 0 {
				return nil, fmt.Errorf("mapped usage %q is incomplete", entry.UsageKey)
			}
			row := aggregates[entry.SurfaceID]
			if row == nil {
				row = &usageAggregate{namespace: entry.Namespace, repositories: map[string]bool{}}
				aggregates[entry.SurfaceID] = row
			}
			if row.namespace != entry.Namespace {
				return nil, fmt.Errorf("surface %q has conflicting namespaces", entry.SurfaceID)
			}
			row.usageKeys = append(row.usageKeys, entry.UsageKey)
			row.prod += entry.PrivateProdRefs
			row.test += entry.PrivateTestRefs
			for _, repositoryID := range entry.RepositoryIDs {
				row.repositories[repositoryID] = true
			}
		case usageClassLocalSymbol, usageClassNonSalesforceGenerated:
			if entry.SurfaceID != "" {
				return nil, fmt.Errorf("non-Salesforce usage %q selects a surface", entry.UsageKey)
			}
		default:
			return nil, fmt.Errorf("unknown usage class %q", entry.Class)
		}
	}
	if len(aggregates) == 0 {
		return nil, fmt.Errorf("no mapped assurance surfaces")
	}
	profiles := map[string]AssuranceProfileRow{}
	for _, row := range profile.Rows {
		if row.SurfaceID == "" || row.Disposition == "" || profiles[row.SurfaceID].SurfaceID != "" {
			return nil, fmt.Errorf("invalid or duplicate assurance profile surface %q", row.SurfaceID)
		}
		profiles[row.SurfaceID] = row
	}
	plans := map[string]OraclePlanRow{}
	for _, row := range plan.Rows {
		if row.SurfaceID == "" || plans[row.SurfaceID].SurfaceID != "" {
			return nil, fmt.Errorf("invalid or duplicate oracle plan surface %q", row.SurfaceID)
		}
		plans[row.SurfaceID] = row
	}
	proofs := map[string]LocalSurfaceProof{}
	for _, row := range proof.Surfaces {
		if row.SurfaceID == "" || proofs[row.SurfaceID].SurfaceID != "" {
			return nil, fmt.Errorf("invalid or duplicate local proof surface %q", row.SurfaceID)
		}
		proofs[row.SurfaceID] = row
	}
	remote := map[string]SalesforceSurfaceResult{}
	for _, shard := range shards {
		for _, row := range shard.Results {
			if row.SurfaceID == "" || remote[row.SurfaceID].SurfaceID != "" {
				return nil, fmt.Errorf("invalid or duplicate Salesforce surface %q", row.SurfaceID)
			}
			remote[row.SurfaceID] = row
		}
	}
	ids := make([]string, 0, len(aggregates))
	for surfaceID := range aggregates {
		ids = append(ids, surfaceID)
	}
	sort.Strings(ids)
	rows := make([]AssuranceSurfaceRow, 0, len(ids))
	for _, surfaceID := range ids {
		aggregate, profileRow, planRow := aggregates[surfaceID], profiles[surfaceID], plans[surfaceID]
		if profileRow.SurfaceID == "" || planRow.SurfaceID == "" {
			return nil, fmt.Errorf("surface %q lacks profile or oracle plan", surfaceID)
		}
		repositories := make([]string, 0, len(aggregate.repositories))
		testReady := true
		for repositoryID := range aggregate.repositories {
			ready, exists := repositoryTests[repositoryID]
			if !exists {
				return nil, fmt.Errorf("surface %q lacks replay test outcome for %q", surfaceID, repositoryID)
			}
			repositories, testReady = append(repositories, repositoryID), testReady && ready
		}
		sort.Strings(repositories)
		sort.Strings(aggregate.usageKeys)
		row := AssuranceSurfaceRow{Namespace: profileRow.Namespace, SurfaceID: surfaceID, UsageKeys: append([]string(nil), aggregate.usageKeys...), RepositoryIDs: repositories, PrivateProdRefs: aggregate.prod, PrivateTestRefs: aggregate.test, Disposition: profileRow.Disposition, SalesforceAction: planRow.Action}
		if row.Namespace == "" {
			row.Namespace = aggregate.namespace
		}
		local := proofs[surfaceID]
		if local.SurfaceID != "" {
			row.FixtureIDs = []string{local.FixtureID}
		}
		switch planRow.Action {
		case oracleRuntime:
			if local.SurfaceID == "" || !localProofSupportsOracleAction(local, profileRow.Disposition, planRow.Action) || remote[surfaceID].Kind != oracleRuntime || !remote[surfaceID].Passed {
				return nil, fmt.Errorf("runtime surface %q lacks complete local or Salesforce evidence", surfaceID)
			}
			row.LocalEvidence, row.SalesforceEvidence = "runtime", "runtime"
			row.CompileReady, row.TestReady = true, testReady
			row.RuntimeParityReady = row.TestReady
		case oracleCompile:
			if local.SurfaceID == "" || !localProofSupportsOracleAction(local, profileRow.Disposition, planRow.Action) || remote[surfaceID].Kind != oracleCompile || !remote[surfaceID].Passed {
				return nil, fmt.Errorf("compile surface %q lacks complete local or Salesforce evidence", surfaceID)
			}
			row.LocalEvidence, row.SalesforceEvidence = "compile", "compile"
			row.CompileReady, row.TestReady = true, testReady
		case oracleLocalContractOnly, oracleWaiver:
			if planRow.ExclusionClass == "" || planRow.ExclusionReason == "" || remote[surfaceID].SurfaceID != "" {
				return nil, fmt.Errorf("non-parity surface %q lacks an explicit exclusion", surfaceID)
			}
			row.LocalEvidence, row.SalesforceEvidence = "local-contract", "non-parity"
			if local.SurfaceID != "" {
				localAction := oracleCompile
				if profileRow.Disposition == localRuntimeRequired {
					localAction = oracleRuntime
				}
				if !localProofSupportsOracleAction(local, profileRow.Disposition, localAction) {
					return nil, fmt.Errorf("non-parity surface %q lacks complete local evidence", surfaceID)
				}
				row.LocalEvidence, row.CompileReady, row.TestReady = localAction, true, testReady
			}
			row.ExclusionClass, row.ExclusionReason, row.NonParity = planRow.ExclusionClass, planRow.ExclusionReason, true
		default:
			return nil, fmt.Errorf("surface %q has unsupported oracle action %q", surfaceID, planRow.Action)
		}
		rows = append(rows, row)
	}
	return rows, ValidateAssuranceOutcomes(rows)
}

func deriveRepositoryAssuranceRows(inventory InventorySpec, rows []AssuranceSurfaceRow, repositoryTests map[string]bool) ([]AssuranceRepositorySurfaceRow, []AssuranceRepositorySummary, error) {
	summaries := make(map[string]*AssuranceRepositorySummary, len(inventory.Repositories))
	for _, repository := range inventory.Repositories {
		if repository.ID == "" || summaries[repository.ID] != nil {
			return nil, nil, fmt.Errorf("invalid or duplicate repository %q", repository.ID)
		}
		ready, exists := repositoryTests[repository.ID]
		if !exists {
			return nil, nil, fmt.Errorf("repository %q lacks replay readiness", repository.ID)
		}
		summaries[repository.ID] = &AssuranceRepositorySummary{RepositoryID: repository.ID, CompileReady: true, TestReady: ready, RuntimeParityReady: true}
	}
	pairs := make([]AssuranceRepositorySurfaceRow, 0)
	for _, row := range rows {
		for _, repositoryID := range row.RepositoryIDs {
			summary := summaries[repositoryID]
			if summary == nil {
				return nil, nil, fmt.Errorf("surface %q names unknown repository %q", row.SurfaceID, repositoryID)
			}
			pair := AssuranceRepositorySurfaceRow{RepositoryID: repositoryID, AssuranceSurfaceRow: row}
			pair.RepositoryIDs = []string{repositoryID}
			pair.TestReady = row.TestReady && repositoryTests[repositoryID]
			pair.RuntimeParityReady = row.RuntimeParityReady && repositoryTests[repositoryID]
			pairs = append(pairs, pair)
			summary.SurfaceCount++
			summary.CompileReady = summary.CompileReady && pair.CompileReady
			summary.TestReady = summary.TestReady && pair.TestReady
			summary.RuntimeParityReady = summary.RuntimeParityReady && pair.RuntimeParityReady
			if pair.NonParity {
				summary.NonParity = true
				summary.NonParityReason = "contains-non-parity-surface"
			}
		}
	}
	resultSummaries := make([]AssuranceRepositorySummary, 0, len(summaries))
	for _, summary := range summaries {
		if summary.SurfaceCount == 0 {
			summary.CompileReady, summary.TestReady, summary.RuntimeParityReady, summary.NonParity = false, false, false, true
			summary.NonParityReason = "no-mapped-salesforce-surfaces"
		}
		resultSummaries = append(resultSummaries, *summary)
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].RepositoryID != pairs[j].RepositoryID {
			return pairs[i].RepositoryID < pairs[j].RepositoryID
		}
		return pairs[i].SurfaceID < pairs[j].SurfaceID
	})
	sort.Slice(resultSummaries, func(i, j int) bool { return resultSummaries[i].RepositoryID < resultSummaries[j].RepositoryID })
	if err := validateRepositoryAssuranceSummaries(resultSummaries); err != nil {
		return nil, nil, err
	}
	return pairs, resultSummaries, nil
}

func validateRepositoryAssuranceSummaries(summaries []AssuranceRepositorySummary) error {
	seen := make(map[string]bool, len(summaries))
	for _, summary := range summaries {
		if summary.RepositoryID == "" || seen[summary.RepositoryID] || summary.SurfaceCount < 0 {
			return fmt.Errorf("invalid repository summary %q", summary.RepositoryID)
		}
		seen[summary.RepositoryID] = true
		if summary.NonParity {
			if (summary.TestReady && !summary.CompileReady) || summary.RuntimeParityReady || summary.NonParityReason == "" {
				return fmt.Errorf("repository summary %q mixes non-parity and readiness", summary.RepositoryID)
			}
			continue
		}
		if summary.SurfaceCount == 0 || (summary.TestReady && !summary.CompileReady) || (summary.RuntimeParityReady && !summary.TestReady) || summary.NonParityReason != "" {
			return fmt.Errorf("invalid readiness summary %q", summary.RepositoryID)
		}
	}
	return nil
}

func localProofSupportsOracleAction(proof LocalSurfaceProof, disposition, action string) bool {
	if proof.Disposition != disposition || !validSurfaceObservation(proof) {
		return false
	}
	switch action {
	case oracleRuntime:
		return proof.RuntimeObserved
	case oracleCompile:
		return proof.CompilePassed || disposition == deterministicMockRequired && proof.BehaviorObserved
	default:
		return false
	}
}

func writeAssuranceArtifacts(report AssuranceReport, jsonPath, htmlPath, receiptPath string, inputs map[string]string) (AssuranceReceipt, error) {
	if report.SchemaVersion != 1 {
		return AssuranceReceipt{}, fmt.Errorf("unsupported assurance report schema version %d", report.SchemaVersion)
	}
	if err := ValidateAssuranceOutcomes(report.Rows); err != nil {
		return AssuranceReceipt{}, err
	}
	if len(inputs) == 0 {
		return AssuranceReceipt{}, fmt.Errorf("sealed assurance receipt inputs are required")
	}
	for name, hash := range inputs {
		if name == "" || filepath.IsAbs(name) || containsPrivateReportPath([]byte(name)) || !sha256Pattern.MatchString(hash) {
			return AssuranceReceipt{}, fmt.Errorf("assurance receipt input is not public-safe")
		}
	}
	reportJSON, err := json.Marshal(report)
	if err != nil {
		return AssuranceReceipt{}, err
	}
	if containsPrivateReportPath(reportJSON) {
		return AssuranceReceipt{}, fmt.Errorf("assurance report is not safe for public output")
	}
	paths := []string{jsonPath, htmlPath, receiptPath}
	for index, path := range paths {
		if !filepath.IsAbs(path) {
			return AssuranceReceipt{}, fmt.Errorf("absolute assurance artifact paths are required")
		}
		for _, earlier := range paths[:index] {
			if path == earlier {
				return AssuranceReceipt{}, fmt.Errorf("assurance artifact paths must be distinct")
			}
		}
		if _, err := os.Lstat(path); err == nil {
			return AssuranceReceipt{}, fmt.Errorf("assurance artifact output already exists: %s", path)
		} else if !os.IsNotExist(err) {
			return AssuranceReceipt{}, err
		}
	}
	reportJSON = append(reportJSON, '\n')
	page, err := renderAssuranceHTML(reportJSON)
	if err != nil {
		return AssuranceReceipt{}, err
	}
	if err := WriteNewJSONText(jsonPath, string(reportJSON)); err != nil {
		return AssuranceReceipt{}, err
	}
	if err := WriteNewJSONText(htmlPath, string(page)); err != nil {
		return AssuranceReceipt{}, err
	}
	assuranceHash := replayBytesSHA256(reportJSON)
	htmlHash := replayBytesSHA256(page)
	receipt := AssuranceReceipt{SchemaVersion: 1, AssuranceSHA256: assuranceHash, HTMLSHA256: htmlHash, InputsSHA256: inputs}
	if err := WriteNewJSON(receiptPath, receipt); err != nil {
		return AssuranceReceipt{}, err
	}
	return receipt, nil
}
