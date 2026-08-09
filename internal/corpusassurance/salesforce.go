package corpusassurance

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"
)

var salesforceInventoryTypes = []string{"ApexClass", "ApexPage", "ApexTrigger", "CustomObject", "CustomField", "FieldSet", "StaticResource", "PlatformCachePartition"}

type SalesforceInventory struct {
	Counts map[string]int `json:"counts,omitempty"`
}

type SalesforceOrgPreflight struct {
	SchemaVersion int                 `json:"schemaVersion"`
	BundleSHA256  string              `json:"bundleSha256"`
	OrgAlias      string              `json:"orgAlias"`
	OrgID         string              `json:"orgId"`
	OrgStatus     string              `json:"orgStatus"`
	Inventory     SalesforceInventory `json:"inventory"`
	Commands      []CommandResult     `json:"commands"`
}

type SalesforceOrgCreation struct {
	SchemaVersion int           `json:"schemaVersion"`
	BundleSHA256  string        `json:"bundleSha256"`
	DevHub        string        `json:"devHub"`
	Alias         string        `json:"alias"`
	OrgID         string        `json:"orgId"`
	Command       CommandResult `json:"command"`
	Invalidated   bool          `json:"invalidated,omitempty"`
}

type SalesforceOrgCreateRequest struct {
	BundlePath     string
	DevHub         string
	Alias          string
	SFBin          string
	OutputPath     string
	validateBundle func(string) error
	runner         salesforceCommandRunner
}

type salesforceOrgReservation struct {
	SchemaVersion int    `json:"schemaVersion"`
	BundleSHA256  string `json:"bundleSha256"`
	DevHub        string `json:"devHub"`
	Alias         string `json:"alias"`
}

type SalesforceOrgCleanup struct {
	SchemaVersion int             `json:"schemaVersion"`
	BundleSHA256  string          `json:"bundleSha256"`
	DevHub        string          `json:"devHub"`
	OrgAlias      string          `json:"orgAlias"`
	OrgID         string          `json:"orgId"`
	Commands      []CommandResult `json:"commands"`
	ResidueAbsent bool            `json:"residueAbsent"`
}

type SalesforceOrgCleanupRequest struct {
	BundlePath     string
	CreationPath   string
	PreflightPath  string
	TargetOrg      string
	DevHub         string
	SFBin          string
	OutputPath     string
	validateBundle func(string) error
	runner         salesforceCommandRunner
}

type SalesforceOrgPreflightRequest struct {
	BundlePath     string
	TargetOrg      string
	SFBin          string
	OutputPath     string
	validateBundle func(string) error
	runner         salesforceCommandRunner
}

type salesforceCommandOutput struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

type salesforceCommandRunner func(context.Context, string, ...string) (salesforceCommandOutput, error)

type salesforceExecution struct {
	workingDirectory string
	environment      []string
}

type salesforceExecutionKey struct{}

type salesforceFilterBinding struct {
	ManifestSHA256        string `json:"manifestSha256"`
	ProfileSHA256         string `json:"profileSha256"`
	QueueSHA256           string `json:"queueSha256"`
	SelectorSHA256        string `json:"selectorSha256"`
	SelectorReceiptSHA256 string `json:"selectorReceiptSha256"`
	CandidateCommit       string `json:"candidateCommit"`
	CandidateSHA256       string `json:"candidateSha256"`
	ToolsCommit           string `json:"toolsCommit"`
	ToolsAMD64SHA256      string `json:"toolsAmd64Sha256"`
	WorkflowScriptSHA256  string `json:"workflowScriptSha256"`
	LocalSummarySHA256    string `json:"localSummarySha256"`
}

type salesforceFilterFixtureResult struct {
	SurfaceIDs    []string        `json:"surfaceIds"`
	Org           string          `json:"org"`
	Kind          string          `json:"kind"`
	ExitCode      *int            `json:"exitCode"`
	Deployable    bool            `json:"deployable"`
	RuntimePassed *bool           `json:"runtimePassed"`
	RuntimeResult json.RawMessage `json:"runtimeResult"`
	RemoteCleanup CleanupReceipt  `json:"remoteCleanup"`
	OrgCleanup    CleanupReceipt  `json:"orgCleanup"`
}

type salesforceFilterPostflight struct {
	MatchesPreflight bool `json:"matchesPreflight"`
}

type salesforceFilterResults struct {
	Sealed        bool                            `json:"sealed"`
	Orgs          []string                        `json:"orgs"`
	Binding       salesforceFilterBinding         `json:"binding"`
	RemoteCleanup CleanupReceipt                  `json:"remoteCleanup"`
	OrgPostflight salesforceFilterPostflight      `json:"orgPostflight"`
	Results       []salesforceFilterFixtureResult `json:"results"`
}
type SalesforceSurfaceResult struct {
	SurfaceID string `json:"surfaceId"`
	Kind      string `json:"kind"`
	Passed    bool   `json:"passed"`
}
type CleanupReceipt struct {
	ResidueAbsent bool `json:"residueAbsent"`
}

// SalesforceBindings tie each raw shard to the exact sealed oracle bundle and
// recorded filter invocation. The values are rechecked by final reconciliation.
type SalesforceBindings struct {
	OraclePlanSHA256        string `json:"oraclePlanSha256"`
	BundleSHA256            string `json:"bundleSha256"`
	FilterSHA256            string `json:"filterSha256"`
	FilterCommandSpecSHA256 string `json:"filterCommandSpecSha256"`
}

type SalesforceShard struct {
	Bindings             SalesforceBindings        `json:"bindings"`
	Candidate            RuntimeArtifact           `json:"candidate"`
	Tools                RuntimeArtifact           `json:"tools"`
	DispatchSHA256       string                    `json:"dispatchSha256"`
	ExecutorRoot         string                    `json:"executorRoot"`
	RunID                string                    `json:"runId"`
	ShardIndex           int                       `json:"shardIndex"`
	ShardCount           int                       `json:"shardCount"`
	OrgAlias             string                    `json:"orgAlias"`
	OrgID                string                    `json:"orgId"`
	OrgStatus            string                    `json:"orgStatus"`
	Preflight            SalesforceOrgPreflight    `json:"preflight"`
	PreInventory         SalesforceInventory       `json:"preInventory"`
	Commands             []CommandResult           `json:"commands"`
	Postflight           SalesforceOrgPreflight    `json:"postflight"`
	PostInventory        SalesforceInventory       `json:"postInventory"`
	Results              []SalesforceSurfaceResult `json:"results"`
	Cleanup              CleanupReceipt            `json:"cleanup"`
	PreflightSHA256      string                    `json:"preflightSha256"`
	PostflightSHA256     string                    `json:"postflightSha256"`
	FilterResultsSHA256  string                    `json:"filterResultsSha256"`
	ExecutedFilterSHA256 string                    `json:"executedFilterSha256"`
}

// SalesforceShardFiles names the complete file-backed lifecycle evidence for
// one Salesforce oracle shard. Reconciliation never trusts embedded copies.
type SalesforceShardFiles struct {
	ShardPath     string
	DispatchPath  string
	CreationPath  string
	CleanupPath   string
	PreflightPath string
}

type SalesforceDispatch struct {
	SchemaVersion           int    `json:"schemaVersion"`
	BundleSHA256            string `json:"bundleSha256"`
	OrgAlias                string `json:"orgAlias"`
	ExecutorRoot            string `json:"executorRoot"`
	RunID                   string `json:"runId"`
	ShardIndex              int    `json:"shardIndex"`
	ShardCount              int    `json:"shardCount"`
	FilterCommandSpecSHA256 string `json:"filterCommandSpecSha256"`
}

type SalesforceDispatchRequest struct {
	BundlePath           string
	OrgAlias             string
	ExecutorRoot         string
	RunID                string
	ShardIndex           int
	ShardCount           int
	OutputPath           string
	approvedFilterSHA256 string
}

const salesforceCommandTimeout = 30 * time.Second
const salesforceFilterTimeout = 15 * time.Minute

type SalesforceShardRequest struct {
	BundlePath           string
	DispatchPath         string
	PreflightPath        string
	TargetOrg            string
	SFBin                string
	ExecutorRoot         string
	RunID                string
	ShardIndex           int
	ShardCount           int
	OutputPath           string
	validateBundle       func(string) error
	filterRunner         salesforceCommandRunner
	sfRunner             salesforceCommandRunner
	approvedFilterSHA256 string
}

// CreateSalesforceDispatch seals the only permitted filter invocation before
// a shard can execute it.
func CreateSalesforceDispatch(request SalesforceDispatchRequest) (SalesforceDispatch, error) {
	if !filepath.IsAbs(request.BundlePath) || !filepath.IsAbs(request.ExecutorRoot) || !filepath.IsAbs(request.OutputPath) || request.OrgAlias == "" || request.RunID == "" || request.ShardCount != 2 || request.ShardIndex < 0 || request.ShardIndex >= request.ShardCount {
		return SalesforceDispatch{}, fmt.Errorf("invalid Salesforce dispatch request")
	}
	if _, err := os.Lstat(request.OutputPath); err == nil {
		return SalesforceDispatch{}, fmt.Errorf("Salesforce dispatch output already exists: %s", request.OutputPath)
	} else if !os.IsNotExist(err) {
		return SalesforceDispatch{}, err
	}
	if err := ValidateOracleBundle(request.BundlePath); err != nil {
		return SalesforceDispatch{}, fmt.Errorf("validate staged bundle: %w", err)
	}
	bundle, bundleBytes, err := readExactJSONBytes[OracleBundle](request.BundlePath)
	if err != nil {
		return SalesforceDispatch{}, err
	}
	bundleSHA := replayBytesSHA256(bundleBytes)
	if bundle.FilterSHA256 != approvedFilterSHA256(request.approvedFilterSHA256) {
		return SalesforceDispatch{}, fmt.Errorf("Salesforce bundle filter is not independently authorized")
	}
	executorRoot, runID, err := sealedSalesforceDispatchLayout(request.BundlePath, bundle.AttemptSHA256, request.ShardIndex)
	requestExecutorRoot, requestErr := canonicalSalesforceExecutorRoot(request.ExecutorRoot)
	if err != nil || requestErr != nil || requestExecutorRoot != executorRoot || request.RunID != runID {
		return SalesforceDispatch{}, fmt.Errorf("Salesforce dispatch does not use the sealed attempt layout")
	}
	if err := createSealedSalesforceExecutorRoot(filepath.Dir(filepath.Dir(executorRoot)), executorRoot); err != nil {
		return SalesforceDispatch{}, err
	}
	filterPath := sealedSalesforceFilterScriptPath(executorRoot)
	args, err := salesforceFilterArgs(filterPath, filepath.Dir(request.BundlePath), executorRoot, runID, request.OrgAlias, bundle, bundleSHA, request.ShardIndex, request.ShardCount)
	if err != nil {
		return SalesforceDispatch{}, err
	}
	environment, err := fixedSalesforceEnvironment()
	if err != nil {
		return SalesforceDispatch{}, err
	}
	pythonSHA, pythonErr := sealedPythonSHA256()
	if pythonErr != nil {
		return SalesforceDispatch{}, pythonErr
	}
	dispatch := SalesforceDispatch{SchemaVersion: 1, BundleSHA256: bundleSHA, OrgAlias: request.OrgAlias, ExecutorRoot: executorRoot, RunID: runID, ShardIndex: request.ShardIndex, ShardCount: request.ShardCount, FilterCommandSpecSHA256: salesforceFilterCommandSpecSHA256("/usr/bin/python3", args, filepath.Dir(request.BundlePath), environment, pythonSHA, pythonSHA)}
	if err := WriteNewJSON(request.OutputPath, dispatch); err != nil {
		return SalesforceDispatch{}, err
	}
	return dispatch, nil
}

func sealedSalesforceDispatchIdentity(bundlePath, attemptSHA256 string, shardIndex int) (string, string, error) {
	executorRoot, runID, err := sealedSalesforceDispatchLayout(bundlePath, attemptSHA256, shardIndex)
	if err != nil {
		return "", "", err
	}
	if err := sealedSalesforceExecutorRoot(filepath.Dir(filepath.Dir(executorRoot)), executorRoot); err != nil {
		return "", "", err
	}
	return executorRoot, runID, nil
}

func sealedSalesforceDispatchLayout(bundlePath, attemptSHA256 string, shardIndex int) (string, string, error) {
	canonicalBundle, err := filepath.EvalSymlinks(bundlePath)
	if err != nil || !filepath.IsAbs(canonicalBundle) || filepath.Base(filepath.Dir(canonicalBundle)) != "bundle" || filepath.Base(filepath.Dir(filepath.Dir(canonicalBundle))) != "razor" || !sha256Pattern.MatchString(attemptSHA256) || shardIndex < 0 || shardIndex >= 2 {
		return "", "", fmt.Errorf("invalid staged bundle layout")
	}
	attemptRoot := filepath.Dir(filepath.Dir(filepath.Dir(canonicalBundle)))
	return filepath.Join(attemptRoot, "executor", fmt.Sprintf("shard-%d", shardIndex)), "assurance-" + attemptSHA256[:16] + fmt.Sprintf("-shard-%d", shardIndex), nil
}

func canonicalSalesforceExecutorRoot(path string) (string, error) {
	if !filepath.IsAbs(path) || filepath.Base(filepath.Dir(path)) != "executor" || !strings.HasPrefix(filepath.Base(path), "shard-") {
		return "", fmt.Errorf("invalid Salesforce executor root")
	}
	attemptRoot, err := filepath.EvalSymlinks(filepath.Dir(filepath.Dir(path)))
	if err != nil {
		return "", err
	}
	return filepath.Join(attemptRoot, "executor", filepath.Base(path)), nil
}

func createSealedSalesforceExecutorRoot(attemptRoot, executorRoot string) error {
	for _, path := range []string{filepath.Join(attemptRoot, "executor"), executorRoot} {
		if info, err := os.Lstat(path); err == nil {
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				return fmt.Errorf("sealed Salesforce executor path is not a physical directory")
			}
			continue
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := os.Mkdir(path, 0o700); err != nil && !os.IsExist(err) {
			return err
		}
	}
	return sealedSalesforceExecutorRoot(attemptRoot, executorRoot)
}

func sealedSalesforceExecutorRoot(attemptRoot, executorRoot string) error {
	if !filepath.IsAbs(attemptRoot) || !filepath.IsAbs(executorRoot) {
		return fmt.Errorf("sealed Salesforce executor path is not absolute")
	}
	for _, path := range []string{filepath.Join(attemptRoot, "executor"), executorRoot} {
		info, err := os.Lstat(path)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("sealed Salesforce executor path is not a physical directory")
		}
	}
	canonical, err := filepath.EvalSymlinks(executorRoot)
	if err != nil {
		return err
	}
	canonicalAttempt, err := filepath.EvalSymlinks(attemptRoot)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(canonicalAttempt, canonical)
	if err != nil || relative != filepath.Join("executor", filepath.Base(executorRoot)) {
		return fmt.Errorf("sealed Salesforce executor path escapes the attempt")
	}
	return nil
}

// RunSalesforceShard executes one sealed filter partition, obtains a fresh
// eight-type postflight receipt, and writes the normalized shard only after
// the staged bundle still validates.
func RunSalesforceShard(request SalesforceShardRequest) (SalesforceShard, error) {
	if !filepath.IsAbs(request.BundlePath) || !filepath.IsAbs(request.DispatchPath) || !filepath.IsAbs(request.PreflightPath) || !filepath.IsAbs(request.OutputPath) || request.TargetOrg == "" || request.SFBin != "/usr/local/bin/sf" {
		return SalesforceShard{}, fmt.Errorf("invalid Salesforce shard request")
	}
	if _, err := os.Lstat(request.OutputPath); err == nil {
		return SalesforceShard{}, fmt.Errorf("Salesforce shard output already exists: %s", request.OutputPath)
	} else if !os.IsNotExist(err) {
		return SalesforceShard{}, err
	}
	validate := request.validateBundle
	if validate == nil {
		validate = ValidateOracleBundle
	}
	if err := validate(request.BundlePath); err != nil {
		return SalesforceShard{}, fmt.Errorf("validate staged bundle: %w", err)
	}
	bundle, bundleBytes, err := readExactJSONBytes[OracleBundle](request.BundlePath)
	if err != nil {
		return SalesforceShard{}, fmt.Errorf("read staged bundle: %w", err)
	}
	planPath := filepath.Join(filepath.Dir(request.BundlePath), "ORACLE_PLAN.json")
	plan, planBytes, err := readExactJSONBytes[OraclePlan](planPath)
	if err != nil || plan.Candidate != bundle.Candidate || plan.Tools != bundle.Tools || replayBytesSHA256(planBytes) != bundle.OraclePlanSHA256 {
		return SalesforceShard{}, fmt.Errorf("staged oracle plan does not bind bundle")
	}
	bundleSHA := replayBytesSHA256(bundleBytes)
	if bundle.FilterSHA256 != approvedFilterSHA256(request.approvedFilterSHA256) {
		return SalesforceShard{}, fmt.Errorf("Salesforce bundle filter is not independently authorized")
	}
	dispatch, dispatchBytes, err := readExactJSONBytes[SalesforceDispatch](request.DispatchPath)
	if err != nil || !validSalesforceDispatch(dispatch, bundle, request.BundlePath) || dispatch.BundleSHA256 != bundleSHA || dispatch.OrgAlias != request.TargetOrg {
		return SalesforceShard{}, fmt.Errorf("invalid sealed Salesforce dispatch")
	}
	dispatchSHA := replayBytesSHA256(dispatchBytes)
	if executorRoot, runID, err := sealedSalesforceDispatchIdentity(request.BundlePath, bundle.AttemptSHA256, dispatch.ShardIndex); err != nil || executorRoot != dispatch.ExecutorRoot || runID != dispatch.RunID {
		return SalesforceShard{}, fmt.Errorf("invalid physical sealed Salesforce executor")
	}
	filterOutput := sealedSalesforceFilterOutputPath(dispatch.ExecutorRoot)
	if _, err := os.Lstat(filterOutput); err == nil {
		return SalesforceShard{}, fmt.Errorf("Salesforce filter output already exists: %s", filterOutput)
	} else if !os.IsNotExist(err) {
		return SalesforceShard{}, err
	}
	if err := os.Mkdir(filterOutput, 0o700); err != nil {
		return SalesforceShard{}, err
	}
	filterScriptRoot := filepath.Dir(sealedSalesforceFilterScriptPath(dispatch.ExecutorRoot))
	if err := os.Mkdir(filterScriptRoot, 0o700); err != nil {
		return SalesforceShard{}, err
	}
	preflight, preflightBytes, err := readExactJSONBytes[SalesforceOrgPreflight](request.PreflightPath)
	if err != nil || !validSalesforceOrgPreflight(preflight, bundleSHA, request.BundlePath) || preflight.OrgAlias != request.TargetOrg {
		return SalesforceShard{}, fmt.Errorf("invalid sealed Salesforce preflight")
	}
	stagedFilterPath := filepath.Join(filepath.Dir(filepath.Dir(request.BundlePath)), "transport", "salesforce-first-filter.py")
	filterPath := sealedSalesforceFilterScriptPath(dispatch.ExecutorRoot)
	if err := validateOracleFilterContract(stagedFilterPath, approvedFilterSHA256(request.approvedFilterSHA256)); err != nil {
		return SalesforceShard{}, fmt.Errorf("validate independently authorized Salesforce filter: %w", err)
	}
	if err := copyOracleBundleFile(stagedFilterPath, filterPath, 0o500); err != nil {
		return SalesforceShard{}, fmt.Errorf("copy independently authorized Salesforce filter: %w", err)
	}
	if err := validateOracleFilterContract(filterPath, approvedFilterSHA256(request.approvedFilterSHA256)); err != nil {
		return SalesforceShard{}, fmt.Errorf("verify sealed Salesforce filter copy: %w", err)
	}
	args, err := salesforceFilterArgs(filterPath, filepath.Dir(request.BundlePath), dispatch.ExecutorRoot, dispatch.RunID, request.TargetOrg, bundle, bundleSHA, dispatch.ShardIndex, dispatch.ShardCount)
	if err != nil {
		return SalesforceShard{}, err
	}
	filterRunner := request.filterRunner
	if filterRunner == nil {
		filterRunner = runSalesforceCLI
	}
	_, command, err := runSalesforceFilterCommand(filterRunner, "/usr/bin/python3", filepath.Dir(request.BundlePath), args...)
	if err != nil {
		return SalesforceShard{}, err
	}
	if command.CommandSpecSHA256 != dispatch.FilterCommandSpecSHA256 {
		return SalesforceShard{}, fmt.Errorf("Salesforce filter command does not match sealed dispatch")
	}
	if executorRoot, runID, err := sealedSalesforceDispatchIdentity(request.BundlePath, bundle.AttemptSHA256, dispatch.ShardIndex); err != nil || executorRoot != dispatch.ExecutorRoot || runID != dispatch.RunID {
		return SalesforceShard{}, fmt.Errorf("sealed Salesforce executor changed during filter execution")
	}
	if info, err := os.Lstat(filterOutput); err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return SalesforceShard{}, fmt.Errorf("Salesforce filter output is not a physical directory")
	}
	filterPathResult := filepath.Join(filterOutput, "results.json")
	filter, filterBytes, err := readSalesforceFilterResults(filterPathResult)
	if err != nil {
		return SalesforceShard{}, err
	}
	postflightPath := filepath.Join(dispatch.ExecutorRoot, "postflight.json")
	postflight, err := RunSalesforceOrgPreflight(SalesforceOrgPreflightRequest{BundlePath: request.BundlePath, TargetOrg: request.TargetOrg, SFBin: request.SFBin, OutputPath: postflightPath, validateBundle: validate, runner: request.sfRunner})
	if err != nil {
		return SalesforceShard{}, fmt.Errorf("Salesforce postflight: %w", err)
	}
	if executorRoot, runID, err := sealedSalesforceDispatchIdentity(request.BundlePath, bundle.AttemptSHA256, dispatch.ShardIndex); err != nil || executorRoot != dispatch.ExecutorRoot || runID != dispatch.RunID {
		return SalesforceShard{}, fmt.Errorf("sealed Salesforce executor changed during postflight execution")
	}
	if err := validate(request.BundlePath); err != nil {
		return SalesforceShard{}, fmt.Errorf("staged bundle changed during Salesforce execution: %w", err)
	}
	postflightRead, postflightBytes, err := readExactJSONBytes[SalesforceOrgPreflight](postflightPath)
	if err != nil || !reflect.DeepEqual(postflightRead, postflight) {
		return SalesforceShard{}, fmt.Errorf("read sealed Salesforce postflight")
	}
	shard, err := NormalizeSalesforceFilterResults(plan, bundle, request.BundlePath, dispatch.ExecutorRoot, dispatch.RunID, preflight, postflight, filter, command, dispatch.ShardIndex, dispatch.ShardCount)
	if err != nil {
		return SalesforceShard{}, err
	}
	for _, input := range []struct {
		path string
		hash string
	}{{request.BundlePath, bundleSHA}, {request.DispatchPath, dispatchSHA}, {planPath, replayBytesSHA256(planBytes)}, {request.PreflightPath, replayBytesSHA256(preflightBytes)}, {filterPath, bundle.FilterSHA256}, {filterPathResult, replayBytesSHA256(filterBytes)}, {postflightPath, replayBytesSHA256(postflightBytes)}} {
		if hash, err := sha256File(input.path); err != nil || hash != input.hash {
			return SalesforceShard{}, fmt.Errorf("Salesforce shard input changed during execution")
		}
	}
	shard.DispatchSHA256 = dispatchSHA
	shard.PreflightSHA256 = replayBytesSHA256(preflightBytes)
	shard.PostflightSHA256 = replayBytesSHA256(postflightBytes)
	shard.FilterResultsSHA256 = replayBytesSHA256(filterBytes)
	shard.ExecutedFilterSHA256 = bundle.FilterSHA256
	if err := WriteNewJSON(request.OutputPath, shard); err != nil {
		return SalesforceShard{}, err
	}
	return shard, nil
}

// RunSalesforceOrgCreate creates one short-lived org from the scratch
// definition sealed in the staged bundle. Its receipt is cleanup authority.
func RunSalesforceOrgCreate(request SalesforceOrgCreateRequest) (SalesforceOrgCreation, error) {
	if !filepath.IsAbs(request.BundlePath) || !filepath.IsAbs(request.OutputPath) || request.DevHub != "glade-dev-hub4" || request.Alias == "" || request.SFBin != "/usr/local/bin/sf" {
		return SalesforceOrgCreation{}, fmt.Errorf("invalid Salesforce org creation request")
	}
	if _, err := os.Lstat(request.OutputPath); err == nil {
		return SalesforceOrgCreation{}, fmt.Errorf("Salesforce org creation output already exists: %s", request.OutputPath)
	} else if !os.IsNotExist(err) {
		return SalesforceOrgCreation{}, err
	}
	validate := request.validateBundle
	if validate == nil {
		validate = ValidateOracleBundle
	}
	if err := validate(request.BundlePath); err != nil {
		return SalesforceOrgCreation{}, fmt.Errorf("validate staged bundle: %w", err)
	}
	bundleSHA, err := sha256File(request.BundlePath)
	if err != nil {
		return SalesforceOrgCreation{}, err
	}
	definition := filepath.Join(filepath.Dir(request.BundlePath), "corpus-assurance-scratch-def.json")
	if data, err := os.ReadFile(definition); err != nil || !json.Valid(data) {
		return SalesforceOrgCreation{}, fmt.Errorf("sealed scratch definition is unavailable")
	}
	if err := WriteNewJSON(request.OutputPath+".reservation", salesforceOrgReservation{SchemaVersion: 1, BundleSHA256: bundleSHA, DevHub: request.DevHub, Alias: request.Alias}); err != nil {
		return SalesforceOrgCreation{}, fmt.Errorf("reserve Salesforce org creation: %w", err)
	}
	runner := request.runner
	if runner == nil {
		runner = runSalesforceCLI
	}
	output, command, err := runSalesforcePreflightCommand(runner, request.SFBin, filepath.Dir(request.BundlePath), salesforceOrgCreateArgs(definition, request.DevHub, request.Alias)...)
	if err != nil {
		return SalesforceOrgCreation{}, err
	}
	orgID, err := parseSalesforceOrgCreate(output.Stdout)
	if err != nil {
		return SalesforceOrgCreation{}, err
	}
	creation := SalesforceOrgCreation{SchemaVersion: 1, BundleSHA256: bundleSHA, DevHub: request.DevHub, Alias: request.Alias, OrgID: orgID, Command: command}
	if err := validate(request.BundlePath); err != nil {
		if sealErr := WriteNewJSON(request.OutputPath+".invalidated", SalesforceOrgCreation{SchemaVersion: 1, BundleSHA256: bundleSHA, DevHub: request.DevHub, Alias: request.Alias, OrgID: orgID, Command: command, Invalidated: true}); sealErr != nil {
			return SalesforceOrgCreation{}, fmt.Errorf("seal invalidated Salesforce org creation: %w", sealErr)
		}
		return SalesforceOrgCreation{}, fmt.Errorf("staged bundle changed during org creation: %w", err)
	}
	if hash, err := sha256File(request.BundlePath); err != nil || hash != bundleSHA {
		if sealErr := WriteNewJSON(request.OutputPath+".invalidated", SalesforceOrgCreation{SchemaVersion: 1, BundleSHA256: bundleSHA, DevHub: request.DevHub, Alias: request.Alias, OrgID: orgID, Command: command, Invalidated: true}); sealErr != nil {
			return SalesforceOrgCreation{}, fmt.Errorf("seal invalidated Salesforce org creation: %w", sealErr)
		}
		return SalesforceOrgCreation{}, fmt.Errorf("staged bundle changed during org creation")
	}
	if err := WriteNewJSON(request.OutputPath, creation); err != nil {
		return SalesforceOrgCreation{}, err
	}
	return creation, nil
}

// RunSalesforceOrgCleanup deletes only an org whose exact creation and
// preflight receipts bind it to this bundle, then verifies the alias is gone.
func RunSalesforceOrgCleanup(request SalesforceOrgCleanupRequest) (SalesforceOrgCleanup, error) {
	if !filepath.IsAbs(request.BundlePath) || !filepath.IsAbs(request.CreationPath) || (request.PreflightPath != "" && !filepath.IsAbs(request.PreflightPath)) || !filepath.IsAbs(request.OutputPath) || request.DevHub != "glade-dev-hub4" || request.TargetOrg == "" || request.SFBin != "/usr/local/bin/sf" {
		return SalesforceOrgCleanup{}, fmt.Errorf("invalid Salesforce cleanup request")
	}
	if _, err := os.Lstat(request.OutputPath); err == nil {
		return SalesforceOrgCleanup{}, fmt.Errorf("Salesforce cleanup output already exists: %s", request.OutputPath)
	} else if !os.IsNotExist(err) {
		return SalesforceOrgCleanup{}, err
	}
	if request.PreflightPath == "" {
		return runInvalidatedSalesforceOrgCleanup(request)
	}
	validate := request.validateBundle
	if validate == nil {
		validate = ValidateOracleBundle
	}
	if err := validate(request.BundlePath); err != nil {
		return SalesforceOrgCleanup{}, fmt.Errorf("validate staged bundle: %w", err)
	}
	bundleSHA, err := sha256File(request.BundlePath)
	if err != nil {
		return SalesforceOrgCleanup{}, err
	}
	creation, creationBytes, err := readExactJSONBytes[SalesforceOrgCreation](request.CreationPath)
	if err != nil || !validSalesforceOrgCreation(creation, bundleSHA, request.BundlePath, request.DevHub, request.TargetOrg) {
		return SalesforceOrgCleanup{}, fmt.Errorf("invalid Salesforce org creation receipt")
	}
	preflight, preflightBytes, err := readExactJSONBytes[SalesforceOrgPreflight](request.PreflightPath)
	if err != nil || !validSalesforceOrgPreflight(preflight, bundleSHA, request.BundlePath) || preflight.OrgAlias != creation.Alias || preflight.OrgID != creation.OrgID {
		return SalesforceOrgCleanup{}, fmt.Errorf("cleanup preflight does not match created org")
	}
	runner := request.runner
	if runner == nil {
		runner = runSalesforceCLI
	}
	_, deleted, err := runSalesforceExpectedCommand(runner, request.SFBin, filepath.Dir(request.BundlePath), true, "org", "delete", "scratch", "--target-org", creation.Alias, "--no-prompt", "--json")
	if err != nil {
		return SalesforceOrgCleanup{}, err
	}
	if err := validate(request.BundlePath); err != nil {
		return SalesforceOrgCleanup{}, fmt.Errorf("staged bundle changed during cleanup: %w", err)
	}
	for _, input := range []struct{ path, hash string }{{request.BundlePath, bundleSHA}, {request.CreationPath, replayBytesSHA256(creationBytes)}, {request.PreflightPath, replayBytesSHA256(preflightBytes)}} {
		if hash, err := sha256File(input.path); err != nil || hash != input.hash {
			return SalesforceOrgCleanup{}, fmt.Errorf("Salesforce cleanup input changed during execution")
		}
	}
	cleanup := SalesforceOrgCleanup{SchemaVersion: 1, BundleSHA256: bundleSHA, DevHub: request.DevHub, OrgAlias: creation.Alias, OrgID: creation.OrgID, Commands: []CommandResult{deleted}, ResidueAbsent: true}
	if err := WriteNewJSON(request.OutputPath, cleanup); err != nil {
		return SalesforceOrgCleanup{}, err
	}
	return cleanup, nil
}

func runInvalidatedSalesforceOrgCleanup(request SalesforceOrgCleanupRequest) (SalesforceOrgCleanup, error) {
	creation, creationBytes, err := readExactJSONBytes[SalesforceOrgCreation](request.CreationPath)
	if err != nil || !creation.Invalidated || !validSalesforceOrgCreation(creation, creation.BundleSHA256, request.BundlePath, request.DevHub, request.TargetOrg) {
		return SalesforceOrgCleanup{}, fmt.Errorf("invalidated Salesforce org creation receipt is invalid")
	}
	runner := request.runner
	if runner == nil {
		runner = runSalesforceCLI
	}
	_, deleted, err := runSalesforceExpectedCommand(runner, request.SFBin, filepath.Dir(request.BundlePath), true, "org", "delete", "scratch", "--target-org", creation.Alias, "--no-prompt", "--json")
	if err != nil {
		return SalesforceOrgCleanup{}, err
	}
	if after, err := sha256File(request.CreationPath); err != nil || after != replayBytesSHA256(creationBytes) {
		return SalesforceOrgCleanup{}, fmt.Errorf("invalidated creation receipt changed during cleanup")
	}
	cleanup := SalesforceOrgCleanup{SchemaVersion: 1, BundleSHA256: creation.BundleSHA256, DevHub: request.DevHub, OrgAlias: creation.Alias, OrgID: creation.OrgID, Commands: []CommandResult{deleted}, ResidueAbsent: true}
	if err := WriteNewJSON(request.OutputPath, cleanup); err != nil {
		return SalesforceOrgCleanup{}, err
	}
	return cleanup, nil
}

func runSalesforceExpectedCommand(runner salesforceCommandRunner, binary, workingDirectory string, expectedSuccess bool, args ...string) (salesforceCommandOutput, CommandResult, error) {
	output, receipt, err := runSealedSalesforceCommand(runner, binary, workingDirectory, args...)
	if err != nil || receipt.TimedOut || (expectedSuccess != receipt.Passed) {
		return output, receipt, fmt.Errorf("Salesforce cleanup command did not have expected result")
	}
	return output, receipt, nil
}

func runSalesforceFilterCommand(runner salesforceCommandRunner, binary, workingDirectory string, args ...string) (salesforceCommandOutput, CommandResult, error) {
	environment, err := fixedSalesforceEnvironment()
	if err != nil || !filepath.IsAbs(workingDirectory) {
		return salesforceCommandOutput{}, CommandResult{}, fmt.Errorf("invalid Salesforce filter execution")
	}
	before, hashErr := sha256File(binary)
	if hashErr != nil || !filepath.IsAbs(binary) {
		return salesforceCommandOutput{}, CommandResult{}, fmt.Errorf("sealed Python interpreter is unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), salesforceFilterTimeout)
	defer cancel()
	started := time.Now()
	ctx = context.WithValue(ctx, salesforceExecutionKey{}, salesforceExecution{workingDirectory: workingDirectory, environment: environment})
	output, err := runner(ctx, binary, args...)
	after, afterErr := sha256File(binary)
	receipt := CommandResult{Command: append([]string{binary}, args...), WorkingDirectory: workingDirectory, Environment: environment, ExecutableSHA256: before, ExecutableAfterSHA256: after, CommandSpecSHA256: salesforceFilterCommandSpecSHA256(binary, args, workingDirectory, environment, before, after), ExitCode: output.ExitCode, DurationMS: time.Since(started).Milliseconds(), StdoutSHA256: replayBytesSHA256(output.Stdout), StderrSHA256: replayBytesSHA256(output.Stderr), Passed: err == nil && afterErr == nil && before == after && output.ExitCode == 0, TimedOut: ctx.Err() == context.DeadlineExceeded}
	if err != nil || output.ExitCode != 0 || receipt.TimedOut || !receipt.Passed {
		return output, receipt, fmt.Errorf("Salesforce filter command failed")
	}
	return output, receipt, nil
}

func salesforceFilterCommandSpecSHA256(binary string, args []string, workingDirectory string, environment []string, executableSHA256, executableAfterSHA256 string) string {
	data, _ := json.Marshal(struct {
		Binary                string   `json:"binary"`
		Arguments             []string `json:"arguments"`
		WorkingDirectory      string   `json:"workingDirectory"`
		Environment           []string `json:"environment"`
		ExecutableSHA256      string   `json:"executableSha256"`
		ExecutableAfterSHA256 string   `json:"executableAfterSha256"`
		TimeoutNS             int64    `json:"timeoutNs"`
	}{binary, args, workingDirectory, environment, executableSHA256, executableAfterSHA256, salesforceFilterTimeout.Nanoseconds()})
	return replayBytesSHA256(data)
}

func readSalesforceFilterResults(path string) (salesforceFilterResults, []byte, error) {
	data, err := os.ReadFile(path)
	if err != nil || !json.Valid(data) {
		return salesforceFilterResults{}, nil, fmt.Errorf("read Salesforce filter results: %w", err)
	}
	var result salesforceFilterResults
	if err := json.Unmarshal(data, &result); err != nil {
		return salesforceFilterResults{}, nil, err
	}
	return result, data, nil
}

// RunSalesforceOrgPreflight records the eight-type zero-inventory gate for a
// newly created scratch org. It only writes a receipt after every command and
// the staged bundle pass validation.
func RunSalesforceOrgPreflight(request SalesforceOrgPreflightRequest) (SalesforceOrgPreflight, error) {
	if !filepath.IsAbs(request.BundlePath) || !filepath.IsAbs(request.OutputPath) || request.TargetOrg == "" || request.SFBin != "/usr/local/bin/sf" {
		return SalesforceOrgPreflight{}, fmt.Errorf("invalid Salesforce preflight request")
	}
	if _, err := os.Lstat(request.OutputPath); err == nil {
		return SalesforceOrgPreflight{}, fmt.Errorf("Salesforce preflight output already exists: %s", request.OutputPath)
	} else if !os.IsNotExist(err) {
		return SalesforceOrgPreflight{}, err
	}
	validate := request.validateBundle
	if validate == nil {
		validate = ValidateOracleBundle
	}
	if err := validate(request.BundlePath); err != nil {
		return SalesforceOrgPreflight{}, fmt.Errorf("validate staged bundle: %w", err)
	}
	bundleSHA, err := sha256File(request.BundlePath)
	if err != nil {
		return SalesforceOrgPreflight{}, err
	}
	runner := request.runner
	if runner == nil {
		runner = runSalesforceCLI
	}
	commands := salesforcePreflightArgs(request.TargetOrg)
	display, displayReceipt, err := runSalesforcePreflightCommand(runner, request.SFBin, filepath.Dir(request.BundlePath), commands[0]...)
	if err != nil {
		return SalesforceOrgPreflight{}, err
	}
	orgID, status, err := parseSalesforceOrgDisplay(display.Stdout)
	if err != nil || status != "Active" {
		return SalesforceOrgPreflight{}, fmt.Errorf("scratch org is not Active")
	}
	preflight := SalesforceOrgPreflight{SchemaVersion: 1, BundleSHA256: bundleSHA, OrgAlias: request.TargetOrg, OrgID: orgID, OrgStatus: status, Inventory: SalesforceInventory{Counts: make(map[string]int)}, Commands: []CommandResult{displayReceipt}}
	for index, kind := range salesforceInventoryTypes {
		output, receipt, err := runSalesforcePreflightCommand(runner, request.SFBin, filepath.Dir(request.BundlePath), commands[index+1]...)
		if err != nil {
			return SalesforceOrgPreflight{}, err
		}
		count, err := parseSalesforceCount(output.Stdout)
		if err != nil || count != 0 {
			return SalesforceOrgPreflight{}, fmt.Errorf("scratch org %s inventory is not empty", kind)
		}
		preflight.Inventory.Counts[kind] = count
		preflight.Commands = append(preflight.Commands, receipt)
	}
	if err := validate(request.BundlePath); err != nil {
		return SalesforceOrgPreflight{}, fmt.Errorf("staged bundle changed during org preflight: %w", err)
	}
	if hash, err := sha256File(request.BundlePath); err != nil || hash != bundleSHA {
		return SalesforceOrgPreflight{}, fmt.Errorf("staged bundle changed during org preflight")
	}
	if err := WriteNewJSON(request.OutputPath, preflight); err != nil {
		return SalesforceOrgPreflight{}, err
	}
	return preflight, nil
}

func runSalesforcePreflightCommand(runner salesforceCommandRunner, binary, workingDirectory string, args ...string) (salesforceCommandOutput, CommandResult, error) {
	output, receipt, err := runSealedSalesforceCommand(runner, binary, workingDirectory, args...)
	if err != nil || output.ExitCode != 0 || receipt.TimedOut {
		return output, receipt, fmt.Errorf("Salesforce preflight command failed")
	}
	return output, receipt, nil
}

func runSealedSalesforceCommand(runner salesforceCommandRunner, binary, workingDirectory string, args ...string) (salesforceCommandOutput, CommandResult, error) {
	if !filepath.IsAbs(workingDirectory) {
		return salesforceCommandOutput{}, CommandResult{}, fmt.Errorf("invalid Salesforce working directory")
	}
	environment, err := fixedSalesforceEnvironment()
	if err != nil {
		return salesforceCommandOutput{}, CommandResult{}, err
	}
	binarySHA256, err := sha256File(binary)
	if err != nil {
		return salesforceCommandOutput{}, CommandResult{}, fmt.Errorf("hash Salesforce CLI before execution: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), salesforceCommandTimeout)
	defer cancel()
	started := time.Now()
	ctx = context.WithValue(ctx, salesforceExecutionKey{}, salesforceExecution{workingDirectory: workingDirectory, environment: environment})
	output, runErr := runner(ctx, binary, args...)
	afterSHA256, hashErr := sha256File(binary)
	receipt := CommandResult{Command: append([]string{binary}, args...), WorkingDirectory: workingDirectory, Environment: environment, ExecutableSHA256: binarySHA256, ExecutableAfterSHA256: afterSHA256, CommandSpecSHA256: salesforceCommandSpecSHA256(binary, args, workingDirectory, environment, binarySHA256, afterSHA256), ExitCode: output.ExitCode, DurationMS: time.Since(started).Milliseconds(), StdoutSHA256: replayBytesSHA256(output.Stdout), StderrSHA256: replayBytesSHA256(output.Stderr), Passed: runErr == nil && output.ExitCode == 0, TimedOut: ctx.Err() == context.DeadlineExceeded}
	if hashErr != nil || binarySHA256 != afterSHA256 {
		return output, receipt, fmt.Errorf("Salesforce CLI changed during execution")
	}
	return output, receipt, runErr
}

func runSalesforceCLI(ctx context.Context, binary string, args ...string) (salesforceCommandOutput, error) {
	command := exec.CommandContext(ctx, binary, args...)
	if execution, ok := ctx.Value(salesforceExecutionKey{}).(salesforceExecution); ok {
		command.Dir = execution.workingDirectory
		command.Env = append([]string(nil), execution.environment...)
	} else {
		command.Env = append(os.Environ(), "SF_USE_GENERIC_UNIX_KEYCHAIN=true")
	}
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	command.Stdout, command.Stderr = stdout, stderr
	err := command.Run()
	output := salesforceCommandOutput{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	if exit, ok := err.(*exec.ExitError); ok {
		output.ExitCode = exit.ExitCode()
		return output, nil
	}
	return output, err
}

func parseSalesforceOrgDisplay(data []byte) (string, string, error) {
	var payload struct {
		Status int `json:"status"`
		Result struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &payload); err != nil || payload.Status != 0 || payload.Result.ID == "" || payload.Result.Status == "" {
		return "", "", fmt.Errorf("invalid Salesforce org display JSON")
	}
	return payload.Result.ID, payload.Result.Status, nil
}

func parseSalesforceOrgCreate(data []byte) (string, error) {
	var payload struct {
		Status int `json:"status"`
		Result struct {
			OrgID string `json:"orgId"`
			ID    string `json:"id"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &payload); err != nil || payload.Status != 0 {
		return "", fmt.Errorf("invalid Salesforce org create JSON")
	}
	if payload.Result.OrgID != "" {
		return payload.Result.OrgID, nil
	}
	if payload.Result.ID != "" {
		return payload.Result.ID, nil
	}
	return "", fmt.Errorf("Salesforce org create response lacks org id")
}

func parseSalesforceCount(data []byte) (int, error) {
	var payload struct {
		Status int `json:"status"`
		Result struct {
			TotalSize int `json:"totalSize"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &payload); err != nil || payload.Status != 0 || payload.Result.TotalSize < 0 {
		return 0, fmt.Errorf("invalid Salesforce count JSON")
	}
	return payload.Result.TotalSize, nil
}

// NormalizeSalesforceFilterResults turns the filter's raw fixture results into
// the only per-surface evidence eligible for final assurance reconciliation.
func NormalizeSalesforceFilterResults(plan OraclePlan, bundle OracleBundle, bundlePath, executorRoot, runID string, preflight, postflight SalesforceOrgPreflight, filter salesforceFilterResults, command CommandResult, shardIndex, shardCount int) (SalesforceShard, error) {
	expected, err := oracleSalesforceResultKinds(plan)
	if err != nil {
		return SalesforceShard{}, err
	}
	if !validSalesforceOrgPreflight(preflight, preflight.BundleSHA256, bundlePath) || !validSalesforceOrgPreflight(postflight, preflight.BundleSHA256, bundlePath) || preflight.OrgAlias != postflight.OrgAlias || preflight.OrgID != postflight.OrgID || !filter.Sealed || len(filter.Orgs) != 1 || filter.Orgs[0] != preflight.OrgAlias || !filter.RemoteCleanup.ResidueAbsent || !filter.OrgPostflight.MatchesPreflight || !command.Passed || command.ExitCode != 0 || command.TimedOut {
		return SalesforceShard{}, fmt.Errorf("invalid Salesforce filter or org evidence")
	}
	if filter.Binding.ManifestSHA256 != bundle.TransportManifestSHA256 || filter.Binding.ProfileSHA256 != bundle.ProfileSHA256 || filter.Binding.QueueSHA256 != bundle.OraclePlanSHA256 || filter.Binding.SelectorSHA256 != bundle.OraclePlanSHA256 || filter.Binding.SelectorReceiptSHA256 != preflight.BundleSHA256 || filter.Binding.CandidateCommit != bundle.Candidate.Commit || filter.Binding.CandidateSHA256 != bundle.Candidate.SHA256 || filter.Binding.ToolsCommit != bundle.Tools.Commit || filter.Binding.ToolsAMD64SHA256 != bundle.ToolsAMD64SHA256 || filter.Binding.WorkflowScriptSHA256 != bundle.FilterSHA256 || filter.Binding.LocalSummarySHA256 != bundle.LocalProofSummarySHA256 {
		return SalesforceShard{}, fmt.Errorf("Salesforce filter bindings do not match the staged bundle")
	}
	bySurface := make(map[string]salesforceFilterFixtureResult, len(expected))
	for _, result := range filter.Results {
		if result.Org != preflight.OrgAlias || result.ExitCode == nil || *result.ExitCode != 0 || !result.Deployable || !result.RemoteCleanup.ResidueAbsent || !result.OrgCleanup.ResidueAbsent || len(result.SurfaceIDs) == 0 {
			return SalesforceShard{}, fmt.Errorf("invalid Salesforce filter fixture result")
		}
		for _, surfaceID := range result.SurfaceIDs {
			action, exists := expected[surfaceID]
			if !exists || bySurface[surfaceID].SurfaceIDs != nil {
				return SalesforceShard{}, fmt.Errorf("unexpected or duplicate Salesforce surface %q", surfaceID)
			}
			if action == oracleRuntime && (result.Kind != "exec" || result.RuntimePassed == nil || !*result.RuntimePassed || !validSalesforceRuntimeObservation(result.RuntimeResult)) {
				return SalesforceShard{}, fmt.Errorf("runtime surface %q lacks Salesforce runtime proof", surfaceID)
			}
			bySurface[surfaceID] = result
		}
	}
	if len(bySurface) != len(expected) {
		return SalesforceShard{}, fmt.Errorf("Salesforce filter surface coverage is incomplete")
	}
	results := make([]SalesforceSurfaceResult, 0, len(expected))
	for _, row := range plan.Rows {
		if action, exists := expected[row.SurfaceID]; exists {
			results = append(results, SalesforceSurfaceResult{SurfaceID: row.SurfaceID, Kind: action, Passed: true})
		}
	}
	bundleSHA := preflight.BundleSHA256
	return SalesforceShard{Bindings: SalesforceBindings{OraclePlanSHA256: bundle.OraclePlanSHA256, BundleSHA256: bundleSHA, FilterSHA256: bundle.FilterSHA256, FilterCommandSpecSHA256: command.CommandSpecSHA256}, Candidate: bundle.Candidate, Tools: bundle.Tools, ExecutorRoot: executorRoot, RunID: runID, ShardIndex: shardIndex, ShardCount: shardCount, OrgAlias: preflight.OrgAlias, OrgID: preflight.OrgID, OrgStatus: preflight.OrgStatus, Preflight: preflight, PreInventory: preflight.Inventory, Commands: []CommandResult{command}, Postflight: postflight, PostInventory: postflight.Inventory, Results: results, Cleanup: CleanupReceipt{ResidueAbsent: true}}, nil
}

func validSalesforceRuntimeObservation(raw json.RawMessage) bool {
	var payload struct {
		Status *int `json:"status"`
		Result struct {
			Success          *bool  `json:"success"`
			Compiled         *bool  `json:"compiled"`
			CompileProblem   string `json:"compileProblem"`
			ExceptionMessage string `json:"exceptionMessage"`
		} `json:"result"`
	}
	return len(raw) > 0 && json.Unmarshal(raw, &payload) == nil && payload.Status != nil && *payload.Status == 0 && payload.Result.Success != nil && *payload.Result.Success && payload.Result.Compiled != nil && *payload.Result.Compiled && payload.Result.CompileProblem == "" && payload.Result.ExceptionMessage == ""
}

func validSalesforceOrgPreflight(preflight SalesforceOrgPreflight, bundleSHA, bundlePath string) bool {
	environment, err := fixedSalesforceEnvironment()
	if err != nil || !filepath.IsAbs(bundlePath) || preflight.SchemaVersion != 1 || preflight.BundleSHA256 != bundleSHA || preflight.OrgAlias == "" || preflight.OrgID == "" || preflight.OrgStatus != "Active" || !zeroInventory(preflight.Inventory) || len(preflight.Inventory.Counts) != len(salesforceInventoryTypes) || len(preflight.Commands) != len(salesforceInventoryTypes)+1 {
		return false
	}
	for index, args := range salesforcePreflightArgs(preflight.OrgAlias) {
		command := preflight.Commands[index]
		expectedCommand := append([]string{"/usr/local/bin/sf"}, args...)
		expectedSpec := salesforceCommandSpecSHA256("/usr/local/bin/sf", args, filepath.Dir(bundlePath), environment, command.ExecutableSHA256, command.ExecutableAfterSHA256)
		if !equalStrings(command.Command, expectedCommand) || command.WorkingDirectory != filepath.Dir(bundlePath) || !reflect.DeepEqual(command.Environment, environment) || !sha256Pattern.MatchString(command.ExecutableSHA256) || command.ExecutableSHA256 != command.ExecutableAfterSHA256 || command.CommandSpecSHA256 != expectedSpec || !command.Passed || command.ExitCode != 0 || command.TimedOut || !sha256Pattern.MatchString(command.StdoutSHA256) || !sha256Pattern.MatchString(command.StderrSHA256) {
			return false
		}
	}
	return true
}

func salesforcePreflightArgs(alias string) [][]string {
	args := [][]string{{"org", "display", "--target-org", alias, "--json"}}
	for _, kind := range salesforceInventoryTypes {
		args = append(args, []string{"data", "query", "--query", "SELECT count() FROM " + kind, "--target-org", alias, "--json"})
	}
	return args
}

func salesforceOrgCreateArgs(definition, devHub, alias string) []string {
	return []string{"org", "create", "scratch", "--target-dev-hub", devHub, "--definition-file", definition, "--alias", alias, "--duration-days", "1", "--json"}
}

func validSalesforceOrgCreation(creation SalesforceOrgCreation, bundleSHA, bundlePath, devHub, alias string) bool {
	args := salesforceOrgCreateArgs(filepath.Join(filepath.Dir(bundlePath), "corpus-assurance-scratch-def.json"), devHub, alias)
	environment, err := fixedSalesforceEnvironment()
	expectedSpec := salesforceCommandSpecSHA256("/usr/local/bin/sf", args, filepath.Dir(bundlePath), environment, creation.Command.ExecutableSHA256, creation.Command.ExecutableAfterSHA256)
	return err == nil && filepath.IsAbs(bundlePath) && creation.SchemaVersion == 1 && creation.BundleSHA256 == bundleSHA && creation.DevHub == devHub && creation.Alias == alias && creation.OrgID != "" && equalStrings(creation.Command.Command, append([]string{"/usr/local/bin/sf"}, args...)) && creation.Command.WorkingDirectory == filepath.Dir(bundlePath) && reflect.DeepEqual(creation.Command.Environment, environment) && sha256Pattern.MatchString(creation.Command.ExecutableSHA256) && creation.Command.ExecutableSHA256 == creation.Command.ExecutableAfterSHA256 && creation.Command.CommandSpecSHA256 == expectedSpec && creation.Command.Passed && creation.Command.ExitCode == 0 && !creation.Command.TimedOut && sha256Pattern.MatchString(creation.Command.StdoutSHA256) && sha256Pattern.MatchString(creation.Command.StderrSHA256)
}

func validSalesforceOrgCleanup(cleanup SalesforceOrgCleanup, bundleSHA, bundlePath string, creation SalesforceOrgCreation) bool {
	if cleanup.SchemaVersion != 1 || cleanup.BundleSHA256 != bundleSHA || cleanup.DevHub != "glade-dev-hub4" || cleanup.OrgAlias != creation.Alias || cleanup.OrgID != creation.OrgID || !cleanup.ResidueAbsent || len(cleanup.Commands) != 1 {
		return false
	}
	expected := []struct {
		args     []string
		passed   bool
		exitCode int
	}{
		{[]string{"org", "delete", "scratch", "--target-org", creation.Alias, "--no-prompt", "--json"}, true, 0},
	}
	environment, err := fixedSalesforceEnvironment()
	if err != nil {
		return false
	}
	for index, want := range expected {
		command := cleanup.Commands[index]
		spec := salesforceCommandSpecSHA256("/usr/local/bin/sf", want.args, filepath.Dir(bundlePath), environment, command.ExecutableSHA256, command.ExecutableAfterSHA256)
		if !equalStrings(command.Command, append([]string{"/usr/local/bin/sf"}, want.args...)) || command.WorkingDirectory != filepath.Dir(bundlePath) || !reflect.DeepEqual(command.Environment, environment) || !sha256Pattern.MatchString(command.ExecutableSHA256) || command.ExecutableSHA256 != command.ExecutableAfterSHA256 || command.CommandSpecSHA256 != spec || command.Passed != want.passed || command.ExitCode != want.exitCode || command.TimedOut || !sha256Pattern.MatchString(command.StdoutSHA256) || !sha256Pattern.MatchString(command.StderrSHA256) {
			return false
		}
	}
	return true
}

func fixedSalesforceEnvironment() ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil || !filepath.IsAbs(home) {
		return nil, fmt.Errorf("resolve Salesforce CLI home")
	}
	return []string{"HOME=" + home, "PATH=/usr/local/bin:/usr/bin:/bin", "SF_USE_GENERIC_UNIX_KEYCHAIN=true", "TMPDIR=/private/tmp"}, nil
}

func salesforceCommandSpecSHA256(binary string, args []string, workingDirectory string, environment []string, executableSHA256, executableAfterSHA256 string) string {
	data, _ := json.Marshal(struct {
		Binary                string   `json:"binary"`
		Arguments             []string `json:"arguments"`
		Environment           []string `json:"environment"`
		WorkingDirectory      string   `json:"workingDirectory"`
		ExecutableSHA256      string   `json:"executableSha256"`
		ExecutableAfterSHA256 string   `json:"executableAfterSha256"`
		TimeoutNS             int64    `json:"timeoutNs"`
	}{binary, args, environment, workingDirectory, executableSHA256, executableAfterSHA256, salesforceCommandTimeout.Nanoseconds()})
	return replayBytesSHA256(data)
}

func salesforceFilterArgs(filterPath, bundleRoot, executorRoot, runID, orgAlias string, bundle OracleBundle, bundleSHA string, shardIndex, shardCount int) ([]string, error) {
	if !filepath.IsAbs(filterPath) || !filepath.IsAbs(bundleRoot) || !filepath.IsAbs(executorRoot) || !strings.Contains(filepath.ToSlash(executorRoot), "/executor/") || runID == "" || orgAlias == "" || !sha256Pattern.MatchString(bundleSHA) || ValidateRuntimeArtifact(bundle.Candidate) != nil || ValidateRuntimeArtifact(bundle.Tools) != nil || ValidateRuntimeArtifact(bundle.ToolsAMD64) != nil || bundle.ToolsAMD64.SHA256 != bundle.ToolsAMD64SHA256 || bundle.ToolsAMD64.Commit != bundle.Tools.Commit || !sha256Pattern.MatchString(bundle.OraclePlanSHA256) || !sha256Pattern.MatchString(bundle.TransportManifestSHA256) || !sha256Pattern.MatchString(bundle.LocalProofSummarySHA256) || len(bundle.Fixtures) == 0 || shardCount != 2 || shardIndex < 0 || shardIndex >= shardCount {
		return nil, fmt.Errorf("invalid sealed Salesforce filter inputs")
	}
	remoteExecutorRoot, err := sealedSalesforceRemoteExecutorRoot(bundle.AttemptSHA256, shardIndex)
	if err != nil {
		return nil, err
	}
	return []string{filterPath,
		"--profile", filepath.Join(bundleRoot, "profile.json"),
		"--fixtures", filepath.Join(bundleRoot, "fixtures"),
		"--manifest", filepath.Join(bundleRoot, "fixture-manifest.json"),
		"--root", bundleRoot,
		"--out", filepath.Join(executorRoot, "filter"),
		"--limit", strconv.Itoa(len(bundle.Fixtures)),
		"--orgs", orgAlias,
		"--ssh-host", "razor.local",
		"--ssh-user", "matt",
		"--remote-root", remoteExecutorRoot,
		"--remote-run-id", runID,
		"--remote-sf-bin", "/usr/local/bin/sf",
		"--candidate-commit", bundle.Candidate.Commit,
		"--candidate-sha256", bundle.Candidate.SHA256,
		"--tools-commit", bundle.Tools.Commit,
		"--tools-amd64-sha256", bundle.ToolsAMD64SHA256,
		"--queue-sha256", bundle.OraclePlanSHA256,
		"--selector-sha256", bundle.OraclePlanSHA256,
		"--selector-receipt-sha256", bundleSHA,
		"--runtime",
		"--local-summary", filepath.Join(bundleRoot, "LOCAL_PROOF_SUMMARY.json"),
		"--manifest-index-modulus", strconv.Itoa(shardCount),
		"--manifest-index-remainder", strconv.Itoa(shardIndex),
	}, nil
}

func sealedSalesforceRemoteExecutorRoot(attemptSHA256 string, shardIndex int) (string, error) {
	if !sha256Pattern.MatchString(attemptSHA256) || shardIndex < 0 || shardIndex >= 2 {
		return "", fmt.Errorf("invalid sealed Salesforce remote executor")
	}
	return filepath.Join(remoteCleanupParent, remoteCleanupPrefix+attemptSHA256[:16], "executor", fmt.Sprintf("shard-%d", shardIndex)), nil
}

func sealedSalesforceFilterOutputPath(executorRoot string) string {
	return filepath.Join(executorRoot, "filter")
}

func sealedSalesforceFilterScriptPath(executorRoot string) string {
	return filepath.Join(executorRoot, "filter-script", "salesforce-first-filter.py")
}

func validSalesforceDispatch(dispatch SalesforceDispatch, bundle OracleBundle, bundlePath string) bool {
	if dispatch.SchemaVersion != 1 || dispatch.BundleSHA256 == "" || dispatch.OrgAlias == "" || dispatch.ExecutorRoot == "" || dispatch.RunID == "" {
		return false
	}
	if validateApprovedOracleBundleFilter(bundle) != nil {
		return false
	}
	executorRoot, runID, identityErr := sealedSalesforceDispatchLayout(bundlePath, bundle.AttemptSHA256, dispatch.ShardIndex)
	filterPath := sealedSalesforceFilterScriptPath(executorRoot)
	args, err := salesforceFilterArgs(filterPath, filepath.Dir(bundlePath), executorRoot, runID, dispatch.OrgAlias, bundle, dispatch.BundleSHA256, dispatch.ShardIndex, dispatch.ShardCount)
	environment, environmentErr := fixedSalesforceEnvironment()
	pythonSHA, pythonErr := sealedPythonSHA256()
	dispatchExecutorRoot, dispatchErr := canonicalSalesforceExecutorRoot(dispatch.ExecutorRoot)
	return identityErr == nil && dispatchErr == nil && pythonErr == nil && dispatchExecutorRoot == executorRoot && dispatch.RunID == runID && err == nil && environmentErr == nil && dispatch.FilterCommandSpecSHA256 == salesforceFilterCommandSpecSHA256("/usr/bin/python3", args, filepath.Dir(bundlePath), environment, pythonSHA, pythonSHA)
}

// ValidateSalesforceShardFiles derives the runtime and compile denominator
// from the sealed oracle plan, then validates every raw shard against it.
// Callers cannot choose a smaller expected set.
func ValidateSalesforceShardFiles(planPath string, shardFiles []SalesforceShardFiles) error {
	if !filepath.IsAbs(planPath) || len(shardFiles) == 0 {
		return fmt.Errorf("absolute oracle plan and Salesforce shard paths are required")
	}
	plan, planBytes, err := readExactJSONBytes[OraclePlan](planPath)
	if err != nil {
		return fmt.Errorf("read oracle plan: %w", err)
	}
	expectedKinds, err := oracleSalesforceResultKinds(plan)
	if err != nil {
		return err
	}
	expected := make([]string, 0, len(expectedKinds))
	for surfaceID := range expectedKinds {
		expected = append(expected, surfaceID)
	}
	planSHA := replayBytesSHA256(planBytes)
	bundlePath := filepath.Join(filepath.Dir(planPath), "bundle.json")
	if err := ValidateOracleBundle(bundlePath); err != nil {
		return fmt.Errorf("validate staged Oracle bundle: %w", err)
	}
	bundle, bundleBytes, err := readExactJSONBytes[OracleBundle](bundlePath)
	if err != nil || bundle.SchemaVersion != 1 || bundle.OraclePlanSHA256 != planSHA || bundle.Candidate != plan.Candidate || bundle.Tools != plan.Tools {
		return fmt.Errorf("staged Oracle bundle does not bind the sealed plan")
	}
	bundleSHA := replayBytesSHA256(bundleBytes)
	shards := make([]SalesforceShard, 0, len(shardFiles))
	files := make([][4]string, 0, len(shardFiles))
	fileHashes := make([][4]string, 0, len(shardFiles))
	seenPaths := map[string]bool{}
	for _, evidence := range shardFiles {
		if !filepath.IsAbs(evidence.ShardPath) || !filepath.IsAbs(evidence.DispatchPath) || !filepath.IsAbs(evidence.CreationPath) || !filepath.IsAbs(evidence.CleanupPath) || !filepath.IsAbs(evidence.PreflightPath) || seenPaths[evidence.ShardPath] || seenPaths[evidence.DispatchPath] || seenPaths[evidence.CreationPath] || seenPaths[evidence.CleanupPath] || seenPaths[evidence.PreflightPath] {
			return fmt.Errorf("absolute Salesforce shard paths are required")
		}
		seenPaths[evidence.ShardPath], seenPaths[evidence.DispatchPath], seenPaths[evidence.CreationPath], seenPaths[evidence.CleanupPath], seenPaths[evidence.PreflightPath] = true, true, true, true, true
		shard, shardBytes, err := readExactJSONBytes[SalesforceShard](evidence.ShardPath)
		if err != nil {
			return fmt.Errorf("read Salesforce shard: %w", err)
		}
		dispatch, dispatchBytes, err := readExactJSONBytes[SalesforceDispatch](evidence.DispatchPath)
		if err != nil {
			return fmt.Errorf("read Salesforce dispatch: %w", err)
		}
		creation, creationBytes, err := readExactJSONBytes[SalesforceOrgCreation](evidence.CreationPath)
		if err != nil {
			return fmt.Errorf("read Salesforce org creation: %w", err)
		}
		cleanup, cleanupBytes, err := readExactJSONBytes[SalesforceOrgCleanup](evidence.CleanupPath)
		if err != nil {
			return fmt.Errorf("read Salesforce org cleanup: %w", err)
		}
		preflight, preflightBytes, err := readExactJSONBytes[SalesforceOrgPreflight](evidence.PreflightPath)
		if err != nil {
			return fmt.Errorf("read Salesforce org preflight: %w", err)
		}
		filterPath := sealedSalesforceFilterScriptPath(shard.ExecutorRoot)
		filterResultsPath := filepath.Join(sealedSalesforceFilterOutputPath(shard.ExecutorRoot), "results.json")
		postflightPath := filepath.Join(shard.ExecutorRoot, "postflight.json")
		filterResultsSHA, filterErr := sha256File(filterResultsPath)
		executedFilterSHA, filterScriptErr := sha256File(filterPath)
		postflight, postflightBytes, postflightErr := readExactJSONBytes[SalesforceOrgPreflight](postflightPath)
		if shard.Bindings.OraclePlanSHA256 != planSHA || shard.Bindings.BundleSHA256 != bundleSHA || shard.Candidate != plan.Candidate || shard.Tools != plan.Tools || shard.DispatchSHA256 != replayBytesSHA256(dispatchBytes) || shard.PreflightSHA256 != replayBytesSHA256(preflightBytes) || shard.PostflightSHA256 != replayBytesSHA256(postflightBytes) || shard.FilterResultsSHA256 != filterResultsSHA || shard.ExecutedFilterSHA256 != executedFilterSHA || shard.ExecutedFilterSHA256 != bundle.FilterSHA256 || !reflect.DeepEqual(preflight, shard.Preflight) || postflightErr != nil || !reflect.DeepEqual(postflight, shard.Postflight) || filterErr != nil || filterScriptErr != nil || !validSalesforceDispatch(dispatch, bundle, bundlePath) || dispatch.BundleSHA256 != bundleSHA || dispatch.OrgAlias != shard.OrgAlias || dispatch.ShardIndex != shard.ShardIndex || dispatch.ShardCount != shard.ShardCount || dispatch.ExecutorRoot != shard.ExecutorRoot || dispatch.RunID != shard.RunID || !validSalesforceOrgPreflight(shard.Preflight, bundleSHA, bundlePath) || !validSalesforceOrgPreflight(shard.Postflight, bundleSHA, bundlePath) || !validSealedFilterCommand(shard, bundle, bundlePath) || creation.Invalidated || !validSalesforceOrgCreation(creation, bundleSHA, bundlePath, "glade-dev-hub4", shard.OrgAlias) || creation.OrgID != shard.OrgID || !validSalesforceOrgCleanup(cleanup, bundleSHA, bundlePath, creation) || cleanup.OrgAlias != shard.OrgAlias || cleanup.OrgID != shard.OrgID || !cleanup.ResidueAbsent {
			return fmt.Errorf("Salesforce shard does not bind sealed oracle plan")
		}
		shards = append(shards, shard)
		files = append(files, [4]string{evidence.ShardPath, evidence.DispatchPath, evidence.CreationPath, evidence.CleanupPath})
		fileHashes = append(fileHashes, [4]string{replayBytesSHA256(shardBytes), replayBytesSHA256(dispatchBytes), replayBytesSHA256(creationBytes), replayBytesSHA256(cleanupBytes)})
	}
	if err := ValidateSalesforceShards(shards, expected); err != nil {
		return err
	}
	for _, shard := range shards {
		for _, result := range shard.Results {
			if result.Kind != expectedKinds[result.SurfaceID] {
				return fmt.Errorf("Salesforce result %q has wrong oracle action", result.SurfaceID)
			}
		}
	}
	if _, after, err := readExactJSONBytes[OraclePlan](planPath); err != nil || replayBytesSHA256(after) != planSHA {
		return fmt.Errorf("oracle plan changed during Salesforce reconciliation")
	}
	for index, paths := range files {
		for item, path := range paths {
			if after, err := sha256File(path); err != nil || after != fileHashes[index][item] {
				return fmt.Errorf("Salesforce lifecycle evidence changed during reconciliation")
			}
		}
	}
	if err := ValidateOracleBundle(bundlePath); err != nil {
		return fmt.Errorf("staged Oracle bundle changed during Salesforce reconciliation: %w", err)
	}
	if after, err := sha256File(bundlePath); err != nil || after != bundleSHA {
		return fmt.Errorf("staged Oracle bundle changed during Salesforce reconciliation")
	}
	return nil
}

func oracleSalesforceSurfaceIDs(plan OraclePlan) ([]string, error) {
	resultKinds, err := oracleSalesforceResultKinds(plan)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(resultKinds))
	for id := range resultKinds {
		ids = append(ids, id)
	}
	return ids, nil
}

func oracleSalesforceResultKinds(plan OraclePlan) (map[string]string, error) {
	if ValidateRuntimeArtifact(plan.Candidate) != nil || ValidateRuntimeArtifact(plan.Tools) != nil || len(plan.Rows) == 0 {
		return nil, fmt.Errorf("invalid oracle plan runtime bindings")
	}
	seen := make(map[string]bool, len(plan.Rows))
	resultKinds := make(map[string]string, len(plan.Rows))
	for _, row := range plan.Rows {
		if row.SurfaceID == "" || seen[row.SurfaceID] {
			return nil, fmt.Errorf("invalid or duplicate oracle plan surface %q", row.SurfaceID)
		}
		seen[row.SurfaceID] = true
		switch row.Action {
		case oracleRuntime, oracleCompile:
			if row.ExclusionClass != "" || row.ExclusionReason != "" {
				return nil, fmt.Errorf("Salesforce oracle row %q carries an exclusion", row.SurfaceID)
			}
			resultKinds[row.SurfaceID] = row.Action
		case oracleLocalContractOnly, oracleWaiver:
			if row.ExclusionClass == "" || row.ExclusionReason == "" {
				return nil, fmt.Errorf("non-parity oracle row %q lacks an exclusion", row.SurfaceID)
			}
		default:
			return nil, fmt.Errorf("oracle plan contains unresolved action %q", row.Action)
		}
	}
	if len(resultKinds) == 0 {
		return nil, fmt.Errorf("oracle plan has no Salesforce-required surfaces")
	}
	return resultKinds, nil
}

func ValidateSalesforceShards(shards []SalesforceShard, expected []string) error {
	if len(shards) == 0 || len(expected) == 0 {
		return fmt.Errorf("Salesforce shards and expected surfaces are required")
	}
	expectedSet, results, indexes, aliases, orgs := map[string]bool{}, map[string]bool{}, map[int]bool{}, map[string]bool{}, map[string]bool{}
	for _, id := range expected {
		if id == "" || expectedSet[id] {
			return fmt.Errorf("invalid expected surface %q", id)
		}
		expectedSet[id] = true
	}
	first := shards[0]
	for _, shard := range shards {
		if ValidateRuntimeArtifact(shard.Candidate) != nil || ValidateRuntimeArtifact(shard.Tools) != nil || !validSalesforceBindings(shard.Bindings) || shard.Candidate != first.Candidate || shard.Tools != first.Tools || !sameSalesforceBundleBindings(shard.Bindings, first.Bindings) || shard.ShardCount != len(shards) || shard.ShardIndex < 0 || shard.ShardIndex >= shard.ShardCount || indexes[shard.ShardIndex] || shard.OrgAlias == "" || aliases[shard.OrgAlias] || shard.OrgID == "" || orgs[shard.OrgID] || shard.OrgStatus != "Active" || !validShardLifecycle(shard) || !validSalesforceCommands(shard.Commands, shard.Bindings.FilterCommandSpecSHA256) || !zeroInventory(shard.PreInventory) || !sameInventory(shard.PreInventory, shard.PostInventory) || !shard.Cleanup.ResidueAbsent {
			return fmt.Errorf("invalid Salesforce shard %d", shard.ShardIndex)
		}
		indexes[shard.ShardIndex], aliases[shard.OrgAlias], orgs[shard.OrgID] = true, true, true
		for _, result := range shard.Results {
			if result.SurfaceID == "" || results[result.SurfaceID] || !expectedSet[result.SurfaceID] || !result.Passed || (result.Kind != oracleRuntime && result.Kind != oracleCompile) {
				return fmt.Errorf("invalid Salesforce result %q", result.SurfaceID)
			}
			results[result.SurfaceID] = true
		}
	}
	if len(indexes) != len(shards) || len(results) != len(expectedSet) {
		return fmt.Errorf("Salesforce shard coverage is incomplete")
	}
	return nil
}

func validShardLifecycle(shard SalesforceShard) bool {
	for _, receipt := range []SalesforceOrgPreflight{shard.Preflight, shard.Postflight} {
		if receipt.SchemaVersion != 1 || receipt.BundleSHA256 != shard.Bindings.BundleSHA256 || receipt.OrgAlias != shard.OrgAlias || receipt.OrgID != shard.OrgID || receipt.OrgStatus != shard.OrgStatus || len(receipt.Commands) != len(salesforceInventoryTypes)+1 || !zeroInventory(receipt.Inventory) {
			return false
		}
	}
	return sameInventory(shard.Preflight.Inventory, shard.PreInventory) && sameInventory(shard.Postflight.Inventory, shard.PostInventory)
}

func validSalesforceBindings(bindings SalesforceBindings) bool {
	return sha256Pattern.MatchString(bindings.OraclePlanSHA256) && sha256Pattern.MatchString(bindings.BundleSHA256) && sha256Pattern.MatchString(bindings.FilterSHA256) && sha256Pattern.MatchString(bindings.FilterCommandSpecSHA256)
}

func sameSalesforceBundleBindings(one, two SalesforceBindings) bool {
	return one.OraclePlanSHA256 == two.OraclePlanSHA256 && one.BundleSHA256 == two.BundleSHA256 && one.FilterSHA256 == two.FilterSHA256
}

func validSalesforceCommands(commands []CommandResult, filterSpecSHA256 string) bool {
	if len(commands) != 1 {
		return false
	}
	command := commands[0]
	return len(command.Command) > 0 && command.CommandSpecSHA256 == filterSpecSHA256 && command.ExitCode == 0 && command.Passed && !command.TimedOut && sha256Pattern.MatchString(command.StdoutSHA256) && sha256Pattern.MatchString(command.StderrSHA256)
}

func validSealedFilterCommand(shard SalesforceShard, bundle OracleBundle, bundlePath string) bool {
	if len(shard.Commands) != 1 || shard.ExecutorRoot == "" || shard.RunID == "" {
		return false
	}
	if validateApprovedOracleBundleFilter(bundle) != nil {
		return false
	}
	filterPath := sealedSalesforceFilterScriptPath(shard.ExecutorRoot)
	args, err := salesforceFilterArgs(filterPath, filepath.Dir(bundlePath), shard.ExecutorRoot, shard.RunID, shard.OrgAlias, bundle, shard.Bindings.BundleSHA256, shard.ShardIndex, shard.ShardCount)
	environment, environmentErr := fixedSalesforceEnvironment()
	command := shard.Commands[0]
	pythonSHA, pythonErr := sealedPythonSHA256()
	return err == nil && environmentErr == nil && pythonErr == nil && equalStrings(command.Command, append([]string{"/usr/bin/python3"}, args...)) && command.WorkingDirectory == filepath.Dir(bundlePath) && reflect.DeepEqual(command.Environment, environment) && command.ExecutableSHA256 == pythonSHA && command.ExecutableAfterSHA256 == pythonSHA && command.CommandSpecSHA256 == salesforceFilterCommandSpecSHA256("/usr/bin/python3", args, filepath.Dir(bundlePath), environment, pythonSHA, pythonSHA) && command.CommandSpecSHA256 == shard.Bindings.FilterCommandSpecSHA256 && command.ExitCode == 0 && command.Passed && !command.TimedOut && sha256Pattern.MatchString(command.StdoutSHA256) && sha256Pattern.MatchString(command.StderrSHA256)
}

func sealedPythonSHA256() (string, error) {
	info, err := os.Stat("/usr/bin/python3")
	if err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("sealed Python interpreter is unavailable")
	}
	return sha256File("/usr/bin/python3")
}

func zeroInventory(inventory SalesforceInventory) bool {
	for _, kind := range salesforceInventoryTypes {
		if inventory.Counts[kind] != 0 {
			return false
		}
	}
	return true
}
func sameInventory(one, two SalesforceInventory) bool {
	for _, kind := range salesforceInventoryTypes {
		if one.Counts[kind] != two.Counts[kind] {
			return false
		}
	}
	return true
}
