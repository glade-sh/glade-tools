package corpusassurance

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
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
	OrgUsername   string              `json:"orgUsername"`
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
	Fixture               string                            `json:"fixture"`
	FixtureSHA256         string                            `json:"fixtureSha256"`
	SourceFiles           []oracleSourceFile                `json:"sourceFiles"`
	OrgIdentity           salesforceFilterOrgIdentity       `json:"orgIdentity"`
	Project               string                            `json:"project"`
	RemoteProject         string                            `json:"remoteProject"`
	RemoteInvocation      *salesforceFilterRemoteInvocation `json:"remoteInvocation"`
	ProjectManifest       []salesforceExecutorFile          `json:"projectManifest"`
	RemoteProjectManifest []salesforceExecutorFile          `json:"remoteProjectManifest"`
	ProjectTreeSHA256     string                            `json:"projectTreeSha256"`
	StdoutSHA256          string                            `json:"stdoutSha256"`
	StderrSHA256          string                            `json:"stderrSha256"`
	SetupSHA256           string                            `json:"setupSha256"`
	RuntimeSHA256         string                            `json:"runtimeSha256,omitempty"`
	RuntimeStderrSHA256   string                            `json:"runtimeStderrSha256,omitempty"`
	TestClasses           []string                          `json:"testClasses"`
	RuntimeExitCode       *int                              `json:"runtimeExitCode"`
	SurfaceIDs            []string                          `json:"surfaceIds"`
	Org                   string                            `json:"org"`
	Kind                  string                            `json:"kind"`
	ExitCode              *int                              `json:"exitCode"`
	Deployable            bool                              `json:"deployable"`
	RuntimePassed         *bool                             `json:"runtimePassed"`
	RuntimeResult         json.RawMessage                   `json:"runtimeResult"`
	RemoteCleanup         CleanupReceipt                    `json:"remoteCleanup"`
	OrgCleanup            CleanupReceipt                    `json:"orgCleanup"`
}

type salesforceFilterRemoteInvocation struct {
	SSHHost      string                          `json:"sshHost"`
	SSHUser      string                          `json:"sshUser"`
	SSHBatchMode bool                            `json:"sshBatchMode"`
	RemoteRoot   string                          `json:"remoteRoot"`
	SFBinary     string                          `json:"sfBinary"`
	Environment  map[string]string               `json:"environment"`
	TargetOrg    string                          `json:"targetOrg"`
	Commands     []salesforceFilterRemoteCommand `json:"commands"`
}

type salesforceFilterRemoteCommand struct {
	Purpose string   `json:"purpose"`
	Command string   `json:"command"`
	SSHArgs []string `json:"sshArgs"`
}

type salesforceFilterOrgIdentity struct {
	Alias    string `json:"alias"`
	OrgID    string `json:"orgId"`
	Username string `json:"username"`
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

type salesforceFilterSelection struct {
	Fixture    string   `json:"fixture"`
	Coverage   int      `json:"coverage"`
	Kind       string   `json:"kind"`
	SurfaceIDs []string `json:"surfaceIds"`
}
type SalesforceSurfaceResult struct {
	SurfaceID string `json:"surfaceId"`
	Kind      string `json:"kind"`
	Passed    bool   `json:"passed"`
}
type CleanupReceipt struct {
	Path                 string                  `json:"path,omitempty"`
	CleanupExitCode      *int                    `json:"cleanupExitCode,omitempty"`
	AbsenceCheckExitCode *int                    `json:"absenceCheckExitCode,omitempty"`
	ExitCode             *int                    `json:"exitCode,omitempty"`
	Requested            []string                `json:"requested,omitempty"`
	Verification         *salesforceCleanupCheck `json:"verification,omitempty"`
	ResidueAbsent        bool                    `json:"residueAbsent"`
}

type salesforceCleanupCheck struct {
	MetadataTypes []string `json:"metadataTypes"`
	Remaining     []string `json:"remaining"`
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
	Bindings               SalesforceBindings        `json:"bindings"`
	Candidate              RuntimeArtifact           `json:"candidate"`
	Tools                  RuntimeArtifact           `json:"tools"`
	DispatchSHA256         string                    `json:"dispatchSha256"`
	ExecutorRoot           string                    `json:"executorRoot"`
	RunID                  string                    `json:"runId"`
	ShardIndex             int                       `json:"shardIndex"`
	ShardCount             int                       `json:"shardCount"`
	OrgAlias               string                    `json:"orgAlias"`
	OrgID                  string                    `json:"orgId"`
	OrgStatus              string                    `json:"orgStatus"`
	Preflight              SalesforceOrgPreflight    `json:"preflight"`
	PreInventory           SalesforceInventory       `json:"preInventory"`
	Commands               []CommandResult           `json:"commands"`
	Postflight             SalesforceOrgPreflight    `json:"postflight"`
	PostInventory          SalesforceInventory       `json:"postInventory"`
	Results                []SalesforceSurfaceResult `json:"results"`
	Cleanup                CleanupReceipt            `json:"cleanup"`
	PreflightSHA256        string                    `json:"preflightSha256"`
	PostflightSHA256       string                    `json:"postflightSha256"`
	FilterResultsSHA256    string                    `json:"filterResultsSha256"`
	ExecutedFilterSHA256   string                    `json:"executedFilterSha256"`
	ExecutorManifestSHA256 string                    `json:"executorManifestSha256"`
}

type salesforceExecutorFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type salesforceExecutorManifest struct {
	SchemaVersion int                      `json:"schemaVersion"`
	Files         []salesforceExecutorFile `json:"files"`
}

type salesforceExecutorSnapshot struct {
	ManifestSHA256 string
	Files          map[string][]byte
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

type salesforceShardEvidenceSnapshot struct {
	Shard  SalesforceShard
	Inputs map[string]string
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

const salesforceExecutorManifestName = "EXECUTOR_MANIFEST.json"

const sealedSalesforceFilterWrapper = "import base64,sys\nscript_path=sys.argv[1]\nsource=base64.b64decode(sys.argv[2])\nsys.argv=[script_path]+sys.argv[3:]\nexec(compile(source, script_path, 'exec'), {'__name__':'__main__','__file__':script_path})\n"

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
	stagedFilterPath := filepath.Join(filepath.Dir(filepath.Dir(request.BundlePath)), "transport", "salesforce-first-filter.py")
	if err := validateOracleFilterContract(stagedFilterPath, approvedFilterSHA256(request.approvedFilterSHA256)); err != nil {
		return SalesforceDispatch{}, fmt.Errorf("validate independently authorized Salesforce filter: %w", err)
	}
	filterSource, err := os.ReadFile(stagedFilterPath)
	if err != nil || replayBytesSHA256(filterSource) != bundle.FilterSHA256 {
		return SalesforceDispatch{}, fmt.Errorf("read independently authorized Salesforce filter")
	}
	filterArgs, err := salesforceFilterArgs(filterPath, filepath.Dir(request.BundlePath), executorRoot, runID, request.OrgAlias, bundle, bundleSHA, request.ShardIndex, request.ShardCount)
	if err != nil {
		return SalesforceDispatch{}, err
	}
	args, err := sealedSalesforceFilterInvocationArgs(filterPath, filterSource, filterArgs)
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
	filterSource, err := os.ReadFile(filterPath)
	if err != nil || replayBytesSHA256(filterSource) != bundle.FilterSHA256 {
		return SalesforceShard{}, fmt.Errorf("read sealed Salesforce filter copy")
	}
	filterArgs, err := salesforceFilterArgs(filterPath, filepath.Dir(request.BundlePath), dispatch.ExecutorRoot, dispatch.RunID, request.TargetOrg, bundle, bundleSHA, dispatch.ShardIndex, dispatch.ShardCount)
	if err != nil {
		return SalesforceShard{}, err
	}
	args, err := sealedSalesforceFilterInvocationArgs(filterPath, filterSource, filterArgs)
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
	snapshot, err := sealSalesforceExecutor(dispatch.ExecutorRoot)
	if err != nil {
		return SalesforceShard{}, err
	}
	filterBytes, exists := snapshot.Files["filter/results.json"]
	if !exists {
		return SalesforceShard{}, fmt.Errorf("Salesforce executor has no filter results")
	}
	filter, err := deriveSalesforceFilterEvidence(bundle, request.BundlePath, preflight.OrgAlias, preflight.OrgID, preflight.OrgUsername, dispatch.ExecutorRoot, dispatch.RunID, dispatch.ShardIndex, snapshot)
	if err != nil {
		return SalesforceShard{}, err
	}
	filter.Binding.SelectorReceiptSHA256 = preflight.BundleSHA256
	postflightBytes, exists := snapshot.Files["postflight.json"]
	var postflightRead SalesforceOrgPreflight
	if !exists || json.Unmarshal(postflightBytes, &postflightRead) != nil || !reflect.DeepEqual(postflightRead, postflight) {
		return SalesforceShard{}, fmt.Errorf("read sealed Salesforce postflight")
	}
	shard, err := NormalizeSalesforceFilterResults(plan, bundle, request.BundlePath, dispatch.ExecutorRoot, dispatch.RunID, preflight, postflight, filter, command, dispatch.ShardIndex, dispatch.ShardCount)
	if err != nil {
		return SalesforceShard{}, err
	}
	for _, input := range []struct {
		path string
		hash string
	}{{request.BundlePath, bundleSHA}, {request.DispatchPath, dispatchSHA}, {planPath, replayBytesSHA256(planBytes)}, {request.PreflightPath, replayBytesSHA256(preflightBytes)}} {
		if hash, err := sha256File(input.path); err != nil || hash != input.hash {
			return SalesforceShard{}, fmt.Errorf("Salesforce shard input changed during execution")
		}
	}
	if after, err := readSealedSalesforceExecutor(dispatch.ExecutorRoot); err != nil || after.ManifestSHA256 != snapshot.ManifestSHA256 || !reflect.DeepEqual(after.Files, snapshot.Files) {
		return SalesforceShard{}, fmt.Errorf("Salesforce executor changed during execution")
	}
	shard.DispatchSHA256 = dispatchSHA
	shard.PreflightSHA256 = replayBytesSHA256(preflightBytes)
	shard.PostflightSHA256 = replayBytesSHA256(postflightBytes)
	shard.FilterResultsSHA256 = replayBytesSHA256(filterBytes)
	shard.ExecutedFilterSHA256 = bundle.FilterSHA256
	shard.ExecutorManifestSHA256 = snapshot.ManifestSHA256
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
	_, absent, err := runSalesforceExpectedCommand(runner, request.SFBin, filepath.Dir(request.BundlePath), false, "org", "display", "--target-org", creation.Alias, "--json")
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
	cleanup := SalesforceOrgCleanup{SchemaVersion: 1, BundleSHA256: bundleSHA, DevHub: request.DevHub, OrgAlias: creation.Alias, OrgID: creation.OrgID, Commands: []CommandResult{deleted, absent}, ResidueAbsent: true}
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
	_, absent, err := runSalesforceExpectedCommand(runner, request.SFBin, filepath.Dir(request.BundlePath), false, "org", "display", "--target-org", creation.Alias, "--json")
	if err != nil {
		return SalesforceOrgCleanup{}, err
	}
	if after, err := sha256File(request.CreationPath); err != nil || after != replayBytesSHA256(creationBytes) {
		return SalesforceOrgCleanup{}, fmt.Errorf("invalidated creation receipt changed during cleanup")
	}
	cleanup := SalesforceOrgCleanup{SchemaVersion: 1, BundleSHA256: creation.BundleSHA256, DevHub: request.DevHub, OrgAlias: creation.Alias, OrgID: creation.OrgID, Commands: []CommandResult{deleted, absent}, ResidueAbsent: true}
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
	receipt := CommandResult{Command: append([]string{binary}, args...), WorkingDirectory: workingDirectory, Environment: environment, ExecutableSHA256: before, ExecutableAfterSHA256: after, CommandSpecSHA256: salesforceFilterCommandSpecSHA256(binary, args, workingDirectory, environment, before, after), ExitCode: output.ExitCode, DurationMS: time.Since(started).Milliseconds(), StdoutSHA256: replayBytesSHA256(output.Stdout), StderrSHA256: replayBytesSHA256(output.Stderr), Output: retainedCommandOutput(output), Passed: err == nil && afterErr == nil && before == after && output.ExitCode == 0, TimedOut: ctx.Err() == context.DeadlineExceeded}
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
	if err != nil {
		return salesforceFilterResults{}, nil, fmt.Errorf("read Salesforce filter results: %w", err)
	}
	result, err := parseSalesforceFilterResults(data)
	if err != nil {
		return salesforceFilterResults{}, nil, err
	}
	return result, data, nil
}

func parseSalesforceFilterResults(data []byte) (salesforceFilterResults, error) {
	if !json.Valid(data) {
		return salesforceFilterResults{}, fmt.Errorf("read Salesforce filter results")
	}
	var result salesforceFilterResults
	if err := json.Unmarshal(data, &result); err != nil {
		return salesforceFilterResults{}, err
	}
	return result, nil
}

func deriveSalesforceFilterEvidence(bundle OracleBundle, bundlePath, orgAlias, orgID, orgUsername, executorRoot, runID string, shardIndex int, snapshot salesforceExecutorSnapshot) (salesforceFilterResults, error) {
	bundleRoot := filepath.Dir(bundlePath)
	manifest, _, err := readExactJSONBytes[oracleTransportManifest](filepath.Join(bundleRoot, "fixture-manifest.json"))
	if err != nil || !validOracleTransportManifest(bundleRoot, manifest, bundle.Fixtures) {
		return salesforceFilterResults{}, fmt.Errorf("invalid sealed Salesforce transport manifest")
	}
	selectionBytes, exists := snapshot.Files["filter/selection.json"]
	if !exists {
		return salesforceFilterResults{}, fmt.Errorf("Salesforce executor has no selection")
	}
	var selection []salesforceFilterSelection
	if err := json.Unmarshal(selectionBytes, &selection); err != nil {
		return salesforceFilterResults{}, fmt.Errorf("read Salesforce selection: %w", err)
	}
	resultBytes, exists := snapshot.Files["filter/results.json"]
	if !exists {
		return salesforceFilterResults{}, fmt.Errorf("Salesforce executor has no adapter receipt")
	}
	adapter, err := parseSalesforceFilterResults(resultBytes)
	if err != nil {
		return salesforceFilterResults{}, fmt.Errorf("read Salesforce adapter receipt: %w", err)
	}
	remoteRoot, err := sealedSalesforceRemoteExecutorRoot(bundle.AttemptSHA256, shardIndex)
	if err != nil {
		return salesforceFilterResults{}, err
	}
	if !adapter.Sealed || !equalStrings(adapter.Orgs, []string{orgAlias}) || !validSalesforceRunCleanup(adapter.RemoteCleanup, filepath.Join(remoteRoot, "projects", runID)) || !adapter.OrgPostflight.MatchesPreflight {
		return salesforceFilterResults{}, fmt.Errorf("Salesforce adapter lacks a passing lifecycle receipt")
	}
	adapterByFixture := make(map[string]salesforceFilterFixtureResult, len(adapter.Results))
	for _, result := range adapter.Results {
		if result.Fixture == "" || adapterByFixture[result.Fixture].Fixture != "" {
			return salesforceFilterResults{}, fmt.Errorf("invalid Salesforce adapter fixture receipt")
		}
		adapterByFixture[result.Fixture] = result
	}
	byFixture := make(map[string]oracleTransportFixture, len(manifest.Fixtures))
	for _, fixture := range manifest.Fixtures {
		byFixture[fixture.Fixture] = fixture
	}
	seenFixtures, seenSurfaces := map[string]bool{}, map[string]bool{}
	derived := make([]salesforceFilterFixtureResult, 0, len(selection))
	for _, item := range selection {
		fixture, known := byFixture[item.Fixture]
		if !known || seenFixtures[item.Fixture] || item.Coverage != len(item.SurfaceIDs) || !equalStrings(sortedStrings(item.SurfaceIDs), fixture.SurfaceIDs) {
			return salesforceFilterResults{}, fmt.Errorf("invalid sealed Salesforce selection")
		}
		seenFixtures[item.Fixture] = true
		adapterResult, hasAdapterResult := adapterByFixture[item.Fixture]
		if !hasAdapterResult || adapterResult.FixtureSHA256 != fixture.SHA256 || !reflect.DeepEqual(adapterResult.SourceFiles, fixture.SourceFiles) || !equalStrings(sortedStrings(adapterResult.SurfaceIDs), item.SurfaceIDs) || adapterResult.Org != orgAlias || adapterResult.Kind != item.Kind || adapterResult.ExitCode == nil || *adapterResult.ExitCode != 0 {
			return salesforceFilterResults{}, fmt.Errorf("Salesforce fixture %q lacks a sealed adapter receipt", item.Fixture)
		}
		kind, err := oracleTransportFixtureKind(bundleRoot, fixture)
		if err != nil || kind != item.Kind {
			return salesforceFilterResults{}, fmt.Errorf("Salesforce selection does not match sealed fixture")
		}
		for _, surfaceID := range item.SurfaceIDs {
			if seenSurfaces[surfaceID] {
				return salesforceFilterResults{}, fmt.Errorf("duplicate Salesforce selection surface %q", surfaceID)
			}
			seenSurfaces[surfaceID] = true
		}
		stem, err := salesforceFixtureStem(item.Fixture)
		if err != nil {
			return salesforceFilterResults{}, err
		}
		expectedProject := filepath.Join(executorRoot, "filter", "projects", stem)
		expectedRemoteProject := filepath.Join(remoteRoot, "projects", runID, stem)
		if adapterResult.Project != expectedProject || adapterResult.RemoteProject != expectedRemoteProject || adapterResult.OrgIdentity != (salesforceFilterOrgIdentity{Alias: orgAlias, OrgID: orgID, Username: orgUsername}) || !validSalesforceTestClasses(adapterResult.TestClasses, kind) || !validSalesforceRemoteInvocation(adapterResult.RemoteInvocation, remoteRoot, expectedRemoteProject, orgUsername, kind, adapterResult.TestClasses) || !validSalesforceFixtureRemoteCleanup(adapterResult.RemoteCleanup, expectedRemoteProject) || !validSalesforceOrgCleanupReceipt(adapterResult.OrgCleanup) {
			return salesforceFilterResults{}, fmt.Errorf("Salesforce fixture %q has an unbound execution receipt", item.Fixture)
		}
		base := filepath.ToSlash(filepath.Join("filter", "projects", stem, "salesforce-"+orgAlias))
		deploy, deployOK := snapshot.Files[base+".json"]
		stderr, stderrOK := snapshot.Files[base+".stderr"]
		setup, setupOK := snapshot.Files[base+".setup"]
		if !deployOK || !stderrOK || !setupOK {
			return salesforceFilterResults{}, fmt.Errorf("Salesforce fixture %q lacks retained command evidence", item.Fixture)
		}
		if kind == "exec" {
			if !validSalesforceRuntimeObservation(deploy) {
				return salesforceFilterResults{}, fmt.Errorf("Salesforce runtime fixture %q lacks raw success", item.Fixture)
			}
		} else if !validSalesforceDeployObservationForProject(deploy, snapshot.Files, filepath.ToSlash(filepath.Join("filter", "projects", stem))) {
			return salesforceFilterResults{}, fmt.Errorf("Salesforce deploy fixture %q lacks raw success", item.Fixture)
		}
		projectTreeSHA, err := salesforceFixtureProjectTreeSHA256(snapshot.Files, filepath.ToSlash(filepath.Join("filter", "projects", stem)))
		if err != nil {
			return salesforceFilterResults{}, err
		}
		if !validSalesforceProjectManifest(adapterResult.ProjectManifest, projectTreeSHA) || !reflect.DeepEqual(adapterResult.RemoteProjectManifest, adapterResult.ProjectManifest) {
			return salesforceFilterResults{}, fmt.Errorf("Salesforce fixture %q project tree does not match the sealed transport receipt", item.Fixture)
		}
		exitCode := *adapterResult.ExitCode
		row := salesforceFilterFixtureResult{Fixture: item.Fixture, FixtureSHA256: fixture.SHA256, SourceFiles: append([]oracleSourceFile(nil), fixture.SourceFiles...), OrgIdentity: adapterResult.OrgIdentity, Project: expectedProject, RemoteProject: expectedRemoteProject, RemoteInvocation: adapterResult.RemoteInvocation, ProjectManifest: append([]salesforceExecutorFile(nil), adapterResult.ProjectManifest...), RemoteProjectManifest: append([]salesforceExecutorFile(nil), adapterResult.RemoteProjectManifest...), ProjectTreeSHA256: projectTreeSHA, StdoutSHA256: replayBytesSHA256(deploy), StderrSHA256: replayBytesSHA256(stderr), SetupSHA256: replayBytesSHA256(setup), TestClasses: append([]string(nil), adapterResult.TestClasses...), RuntimeExitCode: adapterResult.RuntimeExitCode, SurfaceIDs: append([]string(nil), item.SurfaceIDs...), Org: orgAlias, Kind: kind, ExitCode: &exitCode, Deployable: true, RemoteCleanup: adapterResult.RemoteCleanup, OrgCleanup: adapterResult.OrgCleanup}
		if kind == "exec" {
			passed := true
			row.RuntimePassed, row.RuntimeResult = &passed, append(json.RawMessage(nil), deploy...)
		}
		if kind == "test" {
			runtime, ok := snapshot.Files[base+"-tests.json"]
			if !ok || !validSalesforceTestObservation(runtime) || adapterResult.RuntimeExitCode == nil || *adapterResult.RuntimeExitCode != 0 || adapterResult.RuntimePassed == nil || !*adapterResult.RuntimePassed {
				return salesforceFilterResults{}, fmt.Errorf("Salesforce test fixture %q lacks raw runtime success", item.Fixture)
			}
			runtimeStderr, ok := snapshot.Files[base+"-tests.stderr"]
			if !ok {
				return salesforceFilterResults{}, fmt.Errorf("Salesforce test fixture %q lacks retained runtime stderr", item.Fixture)
			}
			row.RuntimeSHA256, row.RuntimeStderrSHA256 = replayBytesSHA256(runtime), replayBytesSHA256(runtimeStderr)
		}
		derived = append(derived, row)
	}
	return salesforceFilterResults{Sealed: true, Orgs: []string{orgAlias}, Binding: salesforceFilterBinding{ManifestSHA256: bundle.TransportManifestSHA256, ProfileSHA256: bundle.ProfileSHA256, QueueSHA256: bundle.OraclePlanSHA256, SelectorSHA256: bundle.OraclePlanSHA256, SelectorReceiptSHA256: "", CandidateCommit: bundle.Candidate.Commit, CandidateSHA256: bundle.Candidate.SHA256, ToolsCommit: bundle.Tools.Commit, ToolsAMD64SHA256: bundle.ToolsAMD64SHA256, WorkflowScriptSHA256: bundle.FilterSHA256, LocalSummarySHA256: bundle.LocalProofSummarySHA256}, RemoteCleanup: adapter.RemoteCleanup, OrgPostflight: adapter.OrgPostflight, Results: derived}, nil
}

func salesforceFixtureProjectTreeSHA256(files map[string][]byte, project string) (string, error) {
	entries, err := salesforceFixtureProjectManifest(files, project)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(entries)
	if err != nil {
		return "", err
	}
	return replayBytesSHA256(data), nil
}

func salesforceFixtureProjectManifest(files map[string][]byte, project string) ([]salesforceExecutorFile, error) {
	prefix := project + "/"
	entries := make([]salesforceExecutorFile, 0)
	for path, data := range files {
		if !strings.HasPrefix(path, prefix) || strings.HasPrefix(filepath.Base(path), "salesforce-") {
			continue
		}
		entries = append(entries, salesforceExecutorFile{Path: strings.TrimPrefix(path, prefix), SHA256: replayBytesSHA256(data)})
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("Salesforce fixture project has no generated files")
	}
	sort.Slice(entries, func(left, right int) bool { return entries[left].Path < entries[right].Path })
	return entries, nil
}

func validSalesforceProjectManifest(entries []salesforceExecutorFile, expectedSHA256 string) bool {
	if len(entries) == 0 || !sha256Pattern.MatchString(expectedSHA256) {
		return false
	}
	for index, entry := range entries {
		if entry.Path == "" || filepath.IsAbs(entry.Path) || strings.HasPrefix(filepath.Clean(entry.Path), "..") || !sha256Pattern.MatchString(entry.SHA256) || index > 0 && entries[index-1].Path >= entry.Path {
			return false
		}
	}
	data, err := json.Marshal(entries)
	return err == nil && replayBytesSHA256(data) == expectedSHA256
}

func validSalesforceRemoteInvocation(invocation *salesforceFilterRemoteInvocation, remoteRoot, project, org, kind string, testClasses []string) bool {
	if invocation == nil || invocation.SSHHost != "razor.local" || invocation.SSHUser != "matt" || !invocation.SSHBatchMode || invocation.RemoteRoot != remoteRoot || invocation.SFBinary != "/usr/local/bin/sf" || !reflect.DeepEqual(invocation.Environment, map[string]string{"SF_USE_GENERIC_UNIX_KEYCHAIN": "true"}) || invocation.TargetOrg != org || len(invocation.Commands) == 0 {
		return false
	}
	command := expectedSalesforceRemoteCommand(project, org, kind)
	if invocation.Commands[0].Purpose != "deploy-or-exec" || invocation.Commands[0].Command != command || !equalStrings(invocation.Commands[0].SSHArgs, []string{"ssh", "-o", "BatchMode=yes", "matt@razor.local", command}) {
		return false
	}
	if kind != "test" {
		return len(invocation.Commands) == 1
	}
	if len(invocation.Commands) != 2 || !validSalesforceTestClasses(testClasses, kind) {
		return false
	}
	runtime := expectedSalesforceRuntimeTestCommand(project, org, testClasses)
	return invocation.Commands[1].Purpose == "runtime-test" && invocation.Commands[1].Command == runtime && equalStrings(invocation.Commands[1].SSHArgs, []string{"ssh", "-o", "BatchMode=yes", "matt@razor.local", runtime})
}

func expectedSalesforceRuntimeTestCommand(project, org string, testClasses []string) string {
	parts := []string{"cd", pythonShellQuote(project), "&&", "env", "SF_USE_GENERIC_UNIX_KEYCHAIN=true", pythonShellQuote("/usr/local/bin/sf"), "apex", "run", "test", "--tests", pythonShellQuote(strings.Join(testClasses, ",")), "--target-org", pythonShellQuote(org)}
	if len(testClasses) == 1 {
		parts = append(parts, "--synchronous")
	}
	return strings.Join(append(parts, "--wait", "10", "--result-format", "json", "--json"), " ")
}

func validSalesforceTestClasses(classes []string, kind string) bool {
	if kind != "test" {
		return len(classes) == 0
	}
	if len(classes) == 0 {
		return false
	}
	for index, class := range classes {
		if class == "" || index > 0 && classes[index-1] >= class {
			return false
		}
	}
	return true
}

func validSalesforceFixtureRemoteCleanup(cleanup CleanupReceipt, project string) bool {
	return cleanup.Path == project && cleanup.CleanupExitCode != nil && *cleanup.CleanupExitCode == 0 && cleanup.AbsenceCheckExitCode != nil && *cleanup.AbsenceCheckExitCode == 0 && cleanup.ResidueAbsent
}

func validSalesforceOrgCleanupReceipt(cleanup CleanupReceipt) bool {
	return cleanup.CleanupExitCode != nil && *cleanup.CleanupExitCode == 0 && cleanup.Verification != nil && len(cleanup.Verification.Remaining) == 0 && cleanup.ResidueAbsent
}

func validSalesforceRunCleanup(cleanup CleanupReceipt, path string) bool {
	return cleanup.Path == path && cleanup.ExitCode != nil && *cleanup.ExitCode == 0 && cleanup.ResidueAbsent
}

func expectedSalesforceRemoteCommand(project, org, kind string) string {
	parts := []string{"cd", pythonShellQuote(project), "&&", "env", "SF_USE_GENERIC_UNIX_KEYCHAIN=true", pythonShellQuote("/usr/local/bin/sf")}
	if kind == "exec" {
		parts = append(parts, "apex", "run", "--file", "anonymous.apex", "--target-org", pythonShellQuote(org), "--api-version", "67.0", "--json")
	} else {
		parts = append(parts, "project", "deploy", "start", "--source-dir", "force-app", "--target-org", pythonShellQuote(org), "--ignore-conflicts", "--wait", "30", "--json")
	}
	return strings.Join(parts, " ")
}

func pythonShellQuote(value string) string {
	if value != "" && strings.IndexFunc(value, func(r rune) bool {
		return !strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_@%+=:,./-", r)
	}) == -1 {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func oracleTransportFixtureKind(bundleRoot string, fixture oracleTransportFixture) (string, error) {
	data, err := os.ReadFile(filepath.Join(bundleRoot, filepath.FromSlash(fixture.Path)))
	if err != nil {
		return "", err
	}
	var document struct {
		Command struct {
			Kind string `json:"kind"`
		} `json:"command"`
	}
	if err := json.Unmarshal(data, &document); err != nil || (document.Command.Kind != "exec" && document.Command.Kind != "test" && document.Command.Kind != "check") {
		return "", fmt.Errorf("invalid sealed Salesforce fixture kind")
	}
	return document.Command.Kind, nil
}

func salesforceFixtureStem(name string) (string, error) {
	if name == "" || filepath.Base(name) != name || strings.Contains(name, "..") {
		return "", fmt.Errorf("invalid Salesforce fixture name")
	}
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	if stem == "" || stem == "." {
		return "", fmt.Errorf("invalid Salesforce fixture name")
	}
	return stem, nil
}

func sortedStrings(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result
}

func validSalesforceDeployObservation(data []byte) bool {
	var payload struct {
		Status *int            `json:"status"`
		Result json.RawMessage `json:"result"`
	}
	if json.Unmarshal(data, &payload) != nil || payload.Status == nil || *payload.Status != 0 {
		return false
	}
	var result struct {
		Status  string `json:"status"`
		Success *bool  `json:"success"`
		Details struct {
			ComponentSuccesses []json.RawMessage `json:"componentSuccesses"`
			ComponentFailures  []json.RawMessage `json:"componentFailures"`
		} `json:"details"`
	}
	return len(payload.Result) > 0 && json.Unmarshal(payload.Result, &result) == nil && (result.Status == "Succeeded" || result.Success != nil && *result.Success) && len(result.Details.ComponentSuccesses) > 0 && len(result.Details.ComponentFailures) == 0
}

func validSalesforceDeployObservationForProject(data []byte, files map[string][]byte, project string) bool {
	if !validSalesforceDeployObservation(data) {
		return false
	}
	var payload struct {
		Result struct {
			Details struct {
				ComponentSuccesses []struct {
					FileName string `json:"fileName"`
				} `json:"componentSuccesses"`
			} `json:"details"`
		} `json:"result"`
	}
	if json.Unmarshal(data, &payload) != nil {
		return false
	}
	materialized := map[string]bool{}
	prefix := project + "/force-app/"
	for path := range files {
		if strings.HasPrefix(path, prefix) {
			relative := strings.TrimPrefix(path, prefix)
			materialized[relative] = true
			materialized[strings.TrimPrefix(relative, "main/default/")] = true
		}
	}
	for _, success := range payload.Result.Details.ComponentSuccesses {
		if materialized[success.FileName] {
			return true
		}
	}
	return false
}

func validSalesforceTestObservation(data []byte) bool {
	var payload struct {
		Status *int `json:"status"`
		Result struct {
			Summary struct {
				Outcome  string `json:"outcome"`
				Failing  int    `json:"failing"`
				TestsRan int    `json:"testsRan"`
			} `json:"summary"`
		} `json:"result"`
	}
	return json.Unmarshal(data, &payload) == nil && payload.Status != nil && *payload.Status == 0 && payload.Result.Summary.Outcome == "Passed" && payload.Result.Summary.Failing == 0 && payload.Result.Summary.TestsRan > 0
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
	orgID, status, username, err := parseSalesforceOrgDisplay(display.Stdout)
	if err != nil || status != "Active" {
		return SalesforceOrgPreflight{}, fmt.Errorf("scratch org is not Active")
	}
	preflight := SalesforceOrgPreflight{SchemaVersion: 1, BundleSHA256: bundleSHA, OrgAlias: request.TargetOrg, OrgID: orgID, OrgUsername: username, OrgStatus: status, Inventory: SalesforceInventory{Counts: make(map[string]int)}, Commands: []CommandResult{displayReceipt}}
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
	receipt := CommandResult{Command: append([]string{binary}, args...), WorkingDirectory: workingDirectory, Environment: environment, ExecutableSHA256: binarySHA256, ExecutableAfterSHA256: afterSHA256, CommandSpecSHA256: salesforceCommandSpecSHA256(binary, args, workingDirectory, environment, binarySHA256, afterSHA256), ExitCode: output.ExitCode, DurationMS: time.Since(started).Milliseconds(), StdoutSHA256: replayBytesSHA256(output.Stdout), StderrSHA256: replayBytesSHA256(output.Stderr), Output: retainedCommandOutput(output), Passed: runErr == nil && output.ExitCode == 0, TimedOut: ctx.Err() == context.DeadlineExceeded}
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

func parseSalesforceOrgDisplay(data []byte) (string, string, string, error) {
	var payload struct {
		Status int `json:"status"`
		Result struct {
			ID       string `json:"id"`
			Status   string `json:"status"`
			Username string `json:"username"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &payload); err != nil || payload.Status != 0 || payload.Result.ID == "" || payload.Result.Status == "" || payload.Result.Username == "" {
		return "", "", "", fmt.Errorf("invalid Salesforce org display JSON")
	}
	return payload.Result.ID, payload.Result.Status, payload.Result.Username, nil
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
	if !validSalesforceOrgPreflight(preflight, preflight.BundleSHA256, bundlePath) || !validSalesforceOrgPreflight(postflight, preflight.BundleSHA256, bundlePath) || preflight.OrgAlias != postflight.OrgAlias || preflight.OrgID != postflight.OrgID || preflight.OrgUsername != postflight.OrgUsername || !filter.Sealed || len(filter.Orgs) != 1 || filter.Orgs[0] != preflight.OrgAlias || !filter.RemoteCleanup.ResidueAbsent || !filter.OrgPostflight.MatchesPreflight || !command.Passed || command.ExitCode != 0 || command.TimedOut {
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
	results := make([]SalesforceSurfaceResult, 0, len(bySurface))
	for _, row := range plan.Rows {
		if action, exists := expected[row.SurfaceID]; exists && bySurface[row.SurfaceID].SurfaceIDs != nil {
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
	if err != nil || !filepath.IsAbs(bundlePath) || preflight.SchemaVersion != 1 || preflight.BundleSHA256 != bundleSHA || preflight.OrgAlias == "" || preflight.OrgID == "" || preflight.OrgUsername == "" || preflight.OrgStatus != "Active" || !zeroInventory(preflight.Inventory) || len(preflight.Inventory.Counts) != len(salesforceInventoryTypes) || len(preflight.Commands) != len(salesforceInventoryTypes)+1 {
		return false
	}
	for index, args := range salesforcePreflightArgs(preflight.OrgAlias) {
		command := preflight.Commands[index]
		expectedCommand := append([]string{"/usr/local/bin/sf"}, args...)
		expectedSpec := salesforceCommandSpecSHA256("/usr/local/bin/sf", args, filepath.Dir(bundlePath), environment, command.ExecutableSHA256, command.ExecutableAfterSHA256)
		if !validRetainedCommandOutput(command) || !equalStrings(command.Command, expectedCommand) || command.WorkingDirectory != filepath.Dir(bundlePath) || !reflect.DeepEqual(command.Environment, environment) || !sha256Pattern.MatchString(command.ExecutableSHA256) || command.ExecutableSHA256 != command.ExecutableAfterSHA256 || command.CommandSpecSHA256 != expectedSpec || !command.Passed || command.ExitCode != 0 || command.TimedOut || !sha256Pattern.MatchString(command.StdoutSHA256) || !sha256Pattern.MatchString(command.StderrSHA256) {
			return false
		}
		if index == 0 {
			orgID, status, username, err := parseSalesforceOrgDisplay(command.Output.Stdout)
			if err != nil || orgID != preflight.OrgID || status != preflight.OrgStatus || username != preflight.OrgUsername {
				return false
			}
		} else if count, err := parseSalesforceCount(command.Output.Stdout); err != nil || count != preflight.Inventory.Counts[salesforceInventoryTypes[index-1]] {
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
	orgID, outputErr := retainedSalesforceOrgCreate(creation.Command)
	return err == nil && outputErr == nil && orgID == creation.OrgID && filepath.IsAbs(bundlePath) && creation.SchemaVersion == 1 && creation.BundleSHA256 == bundleSHA && creation.DevHub == devHub && creation.Alias == alias && creation.OrgID != "" && equalStrings(creation.Command.Command, append([]string{"/usr/local/bin/sf"}, args...)) && creation.Command.WorkingDirectory == filepath.Dir(bundlePath) && reflect.DeepEqual(creation.Command.Environment, environment) && sha256Pattern.MatchString(creation.Command.ExecutableSHA256) && creation.Command.ExecutableSHA256 == creation.Command.ExecutableAfterSHA256 && creation.Command.CommandSpecSHA256 == expectedSpec && creation.Command.Passed && creation.Command.ExitCode == 0 && !creation.Command.TimedOut && sha256Pattern.MatchString(creation.Command.StdoutSHA256) && sha256Pattern.MatchString(creation.Command.StderrSHA256)
}

func validSalesforceOrgCleanup(cleanup SalesforceOrgCleanup, bundleSHA, bundlePath string, creation SalesforceOrgCreation) bool {
	if cleanup.SchemaVersion != 1 || cleanup.BundleSHA256 != bundleSHA || cleanup.DevHub != "glade-dev-hub4" || cleanup.OrgAlias != creation.Alias || cleanup.OrgID != creation.OrgID || !cleanup.ResidueAbsent || len(cleanup.Commands) != 2 {
		return false
	}
	expected := []struct {
		args     []string
		passed   bool
		exitCode int
	}{
		{[]string{"org", "delete", "scratch", "--target-org", creation.Alias, "--no-prompt", "--json"}, true, 0},
		{[]string{"org", "display", "--target-org", creation.Alias, "--json"}, false, 1},
	}
	environment, err := fixedSalesforceEnvironment()
	if err != nil {
		return false
	}
	for index, want := range expected {
		command := cleanup.Commands[index]
		spec := salesforceCommandSpecSHA256("/usr/local/bin/sf", want.args, filepath.Dir(bundlePath), environment, command.ExecutableSHA256, command.ExecutableAfterSHA256)
		if !validRetainedCommandOutput(command) || !equalStrings(command.Command, append([]string{"/usr/local/bin/sf"}, want.args...)) || command.WorkingDirectory != filepath.Dir(bundlePath) || !reflect.DeepEqual(command.Environment, environment) || !sha256Pattern.MatchString(command.ExecutableSHA256) || command.ExecutableSHA256 != command.ExecutableAfterSHA256 || command.CommandSpecSHA256 != spec || command.Passed != want.passed || command.ExitCode != want.exitCode || command.TimedOut || !sha256Pattern.MatchString(command.StdoutSHA256) || !sha256Pattern.MatchString(command.StderrSHA256) {
			return false
		}
		if index == 1 && validSalesforceOrgDisplayFailure(command.Output.Stdout) == false {
			return false
		}
	}
	return true
}

func retainedCommandOutput(output salesforceCommandOutput) *RetainedCommandOutput {
	return &RetainedCommandOutput{Stdout: append([]byte{}, output.Stdout...), Stderr: append([]byte{}, output.Stderr...)}
}

func validRetainedCommandOutput(command CommandResult) bool {
	return command.Output != nil && command.Output.Stdout != nil && command.Output.Stderr != nil && command.StdoutSHA256 == replayBytesSHA256(command.Output.Stdout) && command.StderrSHA256 == replayBytesSHA256(command.Output.Stderr)
}

func retainedSalesforceOrgCreate(command CommandResult) (string, error) {
	if !validRetainedCommandOutput(command) {
		return "", fmt.Errorf("Salesforce org create output is not retained")
	}
	return parseSalesforceOrgCreate(command.Output.Stdout)
}

func validSalesforceOrgDisplayFailure(data []byte) bool {
	var payload struct {
		Status int `json:"status"`
	}
	return json.Unmarshal(data, &payload) == nil && payload.Status != 0
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

func sealedSalesforceFilterInvocationArgs(filterPath string, filterSource []byte, filterArgs []string) ([]string, error) {
	if !filepath.IsAbs(filterPath) || len(filterSource) == 0 || len(filterArgs) == 0 || filterArgs[0] != filterPath {
		return nil, fmt.Errorf("invalid sealed Salesforce filter invocation")
	}
	args := []string{"-c", sealedSalesforceFilterWrapper, filterPath, base64.StdEncoding.EncodeToString(filterSource)}
	return append(args, filterArgs[1:]...), nil
}

func sealSalesforceExecutor(root string) (salesforceExecutorSnapshot, error) {
	manifestPath := filepath.Join(root, salesforceExecutorManifestName)
	if _, err := os.Lstat(manifestPath); err == nil {
		return salesforceExecutorSnapshot{}, fmt.Errorf("Salesforce executor manifest already exists")
	} else if !os.IsNotExist(err) {
		return salesforceExecutorSnapshot{}, err
	}
	_, entries, err := readSalesforceExecutorFiles(root)
	if err != nil {
		return salesforceExecutorSnapshot{}, err
	}
	if err := WriteNewJSON(manifestPath, salesforceExecutorManifest{SchemaVersion: 1, Files: entries}); err != nil {
		return salesforceExecutorSnapshot{}, err
	}
	return readSealedSalesforceExecutor(root)
}

func readSealedSalesforceExecutor(root string) (salesforceExecutorSnapshot, error) {
	manifestPath := filepath.Join(root, salesforceExecutorManifestName)
	info, err := os.Lstat(manifestPath)
	if err != nil || !info.Mode().IsRegular() {
		return salesforceExecutorSnapshot{}, fmt.Errorf("invalid Salesforce executor manifest")
	}
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		return salesforceExecutorSnapshot{}, err
	}
	var manifest salesforceExecutorManifest
	decoder := json.NewDecoder(bytes.NewReader(manifestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil || manifest.SchemaVersion != 1 {
		return salesforceExecutorSnapshot{}, fmt.Errorf("invalid Salesforce executor manifest")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return salesforceExecutorSnapshot{}, fmt.Errorf("invalid Salesforce executor manifest")
	}
	files, entries, err := readSalesforceExecutorFiles(root)
	if err != nil || !reflect.DeepEqual(entries, manifest.Files) {
		return salesforceExecutorSnapshot{}, fmt.Errorf("Salesforce executor artifacts do not match manifest")
	}
	return salesforceExecutorSnapshot{ManifestSHA256: replayBytesSHA256(manifestBytes), Files: files}, nil
}

func readSalesforceExecutorFiles(root string) (map[string][]byte, []salesforceExecutorFile, error) {
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, nil, fmt.Errorf("Salesforce executor root is not a physical directory")
	}
	files := map[string][]byte{}
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("Salesforce executor contains symlink")
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("Salesforce executor contains non-regular artifact")
		}
		relative, err := filepath.Rel(root, path)
		if err != nil || relative == "." || filepath.IsAbs(relative) || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return fmt.Errorf("invalid Salesforce executor artifact path")
		}
		relative = filepath.ToSlash(relative)
		if relative == salesforceExecutorManifestName {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[relative] = data
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	entries := make([]salesforceExecutorFile, 0, len(files))
	for path, data := range files {
		entries = append(entries, salesforceExecutorFile{Path: path, SHA256: replayBytesSHA256(data)})
	}
	sort.Slice(entries, func(left, right int) bool { return entries[left].Path < entries[right].Path })
	return files, entries, nil
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
	stagedFilterPath := filepath.Join(filepath.Dir(filepath.Dir(bundlePath)), "transport", "salesforce-first-filter.py")
	filterSource, sourceErr := os.ReadFile(stagedFilterPath)
	filterArgs, err := salesforceFilterArgs(filterPath, filepath.Dir(bundlePath), executorRoot, runID, dispatch.OrgAlias, bundle, dispatch.BundleSHA256, dispatch.ShardIndex, dispatch.ShardCount)
	args, invocationErr := sealedSalesforceFilterInvocationArgs(filterPath, filterSource, filterArgs)
	environment, environmentErr := fixedSalesforceEnvironment()
	pythonSHA, pythonErr := sealedPythonSHA256()
	dispatchExecutorRoot, dispatchErr := canonicalSalesforceExecutorRoot(dispatch.ExecutorRoot)
	return identityErr == nil && dispatchErr == nil && pythonErr == nil && sourceErr == nil && replayBytesSHA256(filterSource) == bundle.FilterSHA256 && dispatchExecutorRoot == executorRoot && dispatch.RunID == runID && err == nil && invocationErr == nil && environmentErr == nil && dispatch.FilterCommandSpecSHA256 == salesforceFilterCommandSpecSHA256("/usr/bin/python3", args, filepath.Dir(bundlePath), environment, pythonSHA, pythonSHA)
}

// ValidateSalesforceShardFiles derives the runtime and compile denominator
// from the sealed oracle plan, then validates every raw shard against it.
// Callers cannot choose a smaller expected set.
func ValidateSalesforceShardFiles(planPath string, shardFiles []SalesforceShardFiles) error {
	return validateSalesforceShardFiles(planPath, shardFiles, nil)
}

func validateSalesforceShardFiles(planPath string, shardFiles []SalesforceShardFiles, snapshots *[]salesforceShardEvidenceSnapshot) error {
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
	files := make([][]string, 0, len(shardFiles))
	fileHashes := make([][]string, 0, len(shardFiles))
	validatedSnapshots := make([]salesforceShardEvidenceSnapshot, 0, len(shardFiles))
	executorRoots := make([]string, 0, len(shardFiles))
	executorSnapshots := make([]salesforceExecutorSnapshot, 0, len(shardFiles))
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
		snapshot, snapshotErr := readSealedSalesforceExecutor(shard.ExecutorRoot)
		filterPath := sealedSalesforceFilterScriptPath(shard.ExecutorRoot)
		filterResultsPath := filepath.Join(sealedSalesforceFilterOutputPath(shard.ExecutorRoot), "results.json")
		postflightPath := filepath.Join(shard.ExecutorRoot, "postflight.json")
		filterBytes, filterExists := snapshot.Files["filter/results.json"]
		filterSource, filterScriptExists := snapshot.Files[filepath.ToSlash(filepath.Join("filter-script", "salesforce-first-filter.py"))]
		postflightBytes, postflightExists := snapshot.Files["postflight.json"]
		filter, filterReadErr := deriveSalesforceFilterEvidence(bundle, bundlePath, preflight.OrgAlias, preflight.OrgID, preflight.OrgUsername, shard.ExecutorRoot, shard.RunID, shard.ShardIndex, snapshot)
		filter.Binding.SelectorReceiptSHA256 = preflight.BundleSHA256
		var postflight SalesforceOrgPreflight
		postflightErr := json.Unmarshal(postflightBytes, &postflight)
		filterResultsSHA, executedFilterSHA := replayBytesSHA256(filterBytes), replayBytesSHA256(filterSource)
		rebuilt, rebuildErr := NormalizeSalesforceFilterResults(plan, bundle, bundlePath, shard.ExecutorRoot, shard.RunID, preflight, postflight, filter, shard.Commands[0], shard.ShardIndex, shard.ShardCount)
		if rebuildErr == nil {
			rebuilt.DispatchSHA256 = replayBytesSHA256(dispatchBytes)
			rebuilt.PreflightSHA256 = replayBytesSHA256(preflightBytes)
			rebuilt.PostflightSHA256 = replayBytesSHA256(postflightBytes)
			rebuilt.FilterResultsSHA256 = replayBytesSHA256(filterBytes)
			rebuilt.ExecutedFilterSHA256 = executedFilterSHA
			rebuilt.ExecutorManifestSHA256 = snapshot.ManifestSHA256
		}
		if shard.Bindings.OraclePlanSHA256 != planSHA || shard.Bindings.BundleSHA256 != bundleSHA || shard.Candidate != plan.Candidate || shard.Tools != plan.Tools || shard.DispatchSHA256 != replayBytesSHA256(dispatchBytes) || shard.PreflightSHA256 != replayBytesSHA256(preflightBytes) || shard.PostflightSHA256 != replayBytesSHA256(postflightBytes) || shard.FilterResultsSHA256 != filterResultsSHA || shard.ExecutedFilterSHA256 != executedFilterSHA || shard.ExecutorManifestSHA256 != snapshot.ManifestSHA256 || shard.ExecutedFilterSHA256 != bundle.FilterSHA256 || !reflect.DeepEqual(preflight, shard.Preflight) || snapshotErr != nil || !filterExists || !filterScriptExists || !postflightExists || postflightErr != nil || !reflect.DeepEqual(postflight, shard.Postflight) || filterReadErr != nil || rebuildErr != nil || !reflect.DeepEqual(rebuilt, shard) || !validSalesforceDispatch(dispatch, bundle, bundlePath) || dispatch.BundleSHA256 != bundleSHA || dispatch.OrgAlias != shard.OrgAlias || dispatch.ShardIndex != shard.ShardIndex || dispatch.ShardCount != shard.ShardCount || dispatch.ExecutorRoot != shard.ExecutorRoot || dispatch.RunID != shard.RunID || !validSalesforceOrgPreflight(shard.Preflight, bundleSHA, bundlePath) || !validSalesforceOrgPreflight(shard.Postflight, bundleSHA, bundlePath) || !validSealedFilterCommand(shard, bundle, bundlePath) || creation.Invalidated || !validSalesforceOrgCreation(creation, bundleSHA, bundlePath, "glade-dev-hub4", shard.OrgAlias) || creation.OrgID != shard.OrgID || !validSalesforceOrgCleanup(cleanup, bundleSHA, bundlePath, creation) || cleanup.OrgAlias != shard.OrgAlias || cleanup.OrgID != shard.OrgID || !cleanup.ResidueAbsent {
			return fmt.Errorf("Salesforce shard does not bind sealed oracle plan")
		}
		shards = append(shards, shard)
		validatedSnapshots = append(validatedSnapshots, salesforceShardEvidenceSnapshot{Shard: shard, Inputs: map[string]string{"shard": replayBytesSHA256(shardBytes), "dispatch": replayBytesSHA256(dispatchBytes), "preflight": replayBytesSHA256(preflightBytes), "creation": replayBytesSHA256(creationBytes), "cleanup": replayBytesSHA256(cleanupBytes)}})
		files = append(files, []string{evidence.ShardPath, evidence.DispatchPath, evidence.PreflightPath, filterPath, filterResultsPath, postflightPath, evidence.CreationPath, evidence.CleanupPath})
		fileHashes = append(fileHashes, []string{replayBytesSHA256(shardBytes), replayBytesSHA256(dispatchBytes), replayBytesSHA256(preflightBytes), executedFilterSHA, replayBytesSHA256(filterBytes), replayBytesSHA256(postflightBytes), replayBytesSHA256(creationBytes), replayBytesSHA256(cleanupBytes)})
		executorRoots = append(executorRoots, shard.ExecutorRoot)
		executorSnapshots = append(executorSnapshots, snapshot)
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
	for index, root := range executorRoots {
		after, err := readSealedSalesforceExecutor(root)
		if err != nil || after.ManifestSHA256 != executorSnapshots[index].ManifestSHA256 || !reflect.DeepEqual(after.Files, executorSnapshots[index].Files) {
			return fmt.Errorf("Salesforce executor evidence changed during reconciliation")
		}
	}
	if err := ValidateOracleBundle(bundlePath); err != nil {
		return fmt.Errorf("staged Oracle bundle changed during Salesforce reconciliation: %w", err)
	}
	if after, err := sha256File(bundlePath); err != nil || after != bundleSHA {
		return fmt.Errorf("staged Oracle bundle changed during Salesforce reconciliation")
	}
	if snapshots != nil {
		*snapshots = validatedSnapshots
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
	filterSource, sourceErr := os.ReadFile(filterPath)
	filterArgs, err := salesforceFilterArgs(filterPath, filepath.Dir(bundlePath), shard.ExecutorRoot, shard.RunID, shard.OrgAlias, bundle, shard.Bindings.BundleSHA256, shard.ShardIndex, shard.ShardCount)
	args, invocationErr := sealedSalesforceFilterInvocationArgs(filterPath, filterSource, filterArgs)
	environment, environmentErr := fixedSalesforceEnvironment()
	command := shard.Commands[0]
	pythonSHA, pythonErr := sealedPythonSHA256()
	return err == nil && sourceErr == nil && replayBytesSHA256(filterSource) == bundle.FilterSHA256 && invocationErr == nil && environmentErr == nil && pythonErr == nil && validRetainedCommandOutput(command) && equalStrings(command.Command, append([]string{"/usr/bin/python3"}, args...)) && command.WorkingDirectory == filepath.Dir(bundlePath) && reflect.DeepEqual(command.Environment, environment) && command.ExecutableSHA256 == pythonSHA && command.ExecutableAfterSHA256 == pythonSHA && command.CommandSpecSHA256 == salesforceFilterCommandSpecSHA256("/usr/bin/python3", args, filepath.Dir(bundlePath), environment, pythonSHA, pythonSHA) && command.CommandSpecSHA256 == shard.Bindings.FilterCommandSpecSHA256 && command.ExitCode == 0 && command.Passed && !command.TimedOut && sha256Pattern.MatchString(command.StdoutSHA256) && sha256Pattern.MatchString(command.StderrSHA256)
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
