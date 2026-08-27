package corpusassurance

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
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

// SalesforceDevHubAuthority is an independently sealed private operator input.
// Salesforce receipts may use only this identity, never a caller-selected
// Dev Hub alias that is absent from the sealed bundle.
type SalesforceDevHubAuthority struct {
	SchemaVersion int                          `json:"schemaVersion"`
	Alias         string                       `json:"alias"`
	OrgID         string                       `json:"orgId"`
	Username      string                       `json:"username"`
	Execution     SalesforceExecutionAuthority `json:"execution"`
	Command       CommandResult                `json:"command,omitempty"`
}

func validSalesforceDevHubAuthority(authority SalesforceDevHubAuthority) bool {
	if (authority.SchemaVersion != 1 && authority.SchemaVersion != 2) || authority.Alias == "" || authority.OrgID == "" || authority.Username == "" || !validSalesforceExecutionAuthority(authority.Execution) {
		return false
	}
	if authority.SchemaVersion == 2 {
		command := authority.Command
		return command.Passed && command.ExitCode == 0 && !command.TimedOut && command.DurationMS >= 0 && command.DurationMS <= devHubAuthorityTimeout.Milliseconds() && len(command.Command) == 6 && command.Command[0] == authority.Execution.SFBinary && command.Command[1] == "org" && command.Command[2] == "display" && command.Command[3] == "--target-org" && command.Command[4] == authority.Alias && command.Command[5] == "--json" && command.WorkingDirectory != "" && reflect.DeepEqual(command.Environment, authority.Execution.Environment) && command.ExecutableSHA256 == authority.Execution.SFSHA256 && command.ExecutableAfterSHA256 == authority.Execution.SFSHA256 && command.CommandSpecSHA256 == salesforceCommandSpecSHA256(command.Command[0], command.Command[1:], command.WorkingDirectory, command.Environment, command.ExecutableSHA256, command.ExecutableAfterSHA256) && command.Output == nil && sha256Pattern.MatchString(command.StdoutSHA256) && sha256Pattern.MatchString(command.StderrSHA256)
	}
	return true
}

func validateNewSalesforceDevHubAuthority(authority SalesforceDevHubAuthority) error {
	if authority.SchemaVersion != 2 || !validSalesforceDevHubAuthority(authority) {
		return fmt.Errorf("new oracle bundles require Dev Hub authority schema version 2")
	}
	return nil
}

type SalesforceDevHubAuthorityRequest struct {
	TargetOrg string
	SFBin     string
	PythonBin string
	Home      string
	Path      string
	TmpDir    string
	Output    string
	runner    salesforceCommandRunner
}

const devHubAuthorityTimeout = 30 * time.Second

// CreateSalesforceDevHubAuthority records one bounded org display using an
// exact execution environment. The response is parsed in memory and omitted.
func CreateSalesforceDevHubAuthority(request SalesforceDevHubAuthorityRequest) (SalesforceDevHubAuthority, error) {
	for _, path := range []string{request.SFBin, request.PythonBin, request.Home, request.TmpDir, request.Output} {
		if !filepath.IsAbs(path) {
			return SalesforceDevHubAuthority{}, fmt.Errorf("absolute Dev Hub authority paths are required")
		}
	}
	if request.TargetOrg == "" || request.Path == "" || strings.ContainsAny(request.TargetOrg, " \t\r\n") {
		return SalesforceDevHubAuthority{}, fmt.Errorf("invalid Dev Hub target or PATH")
	}
	if _, err := os.Lstat(request.Output); err == nil {
		return SalesforceDevHubAuthority{}, fmt.Errorf("Dev Hub authority output already exists: %s", request.Output)
	} else if !os.IsNotExist(err) {
		return SalesforceDevHubAuthority{}, err
	}
	if err := validateDevHubExecutable(request.SFBin); err != nil {
		return SalesforceDevHubAuthority{}, err
	}
	if err := validateDevHubExecutable(request.PythonBin); err != nil {
		return SalesforceDevHubAuthority{}, err
	}
	pathEntries := filepath.SplitList(request.Path)
	if len(pathEntries) == 0 {
		return SalesforceDevHubAuthority{}, fmt.Errorf("exact PATH is required")
	}
	hasSF, hasPython := false, false
	for _, entry := range pathEntries {
		if !cleanAbsolutePath(entry) {
			return SalesforceDevHubAuthority{}, fmt.Errorf("PATH entries must be clean absolute paths")
		}
		hasSF = hasSF || entry == filepath.Dir(request.SFBin)
		hasPython = hasPython || entry == filepath.Dir(request.PythonBin)
	}
	if !hasSF || !hasPython {
		return SalesforceDevHubAuthority{}, fmt.Errorf("PATH must contain Salesforce CLI and Python directories")
	}
	sfSHA, err := sha256File(request.SFBin)
	if err != nil {
		return SalesforceDevHubAuthority{}, err
	}
	pythonSHA, err := sha256File(request.PythonBin)
	if err != nil {
		return SalesforceDevHubAuthority{}, err
	}
	environment := []string{"HOME=" + request.Home, "PATH=" + request.Path, "TMPDIR=" + request.TmpDir, "SF_USE_GENERIC_UNIX_KEYCHAIN=true"}
	runner := request.runner
	if runner == nil {
		runner = runSalesforceCLI
	}
	args := []string{"org", "display", "--target-org", request.TargetOrg, "--json"}
	ctx, cancel := context.WithTimeout(context.Background(), devHubAuthorityTimeout)
	defer cancel()
	ctx = context.WithValue(ctx, salesforceExecutionKey{}, salesforceExecution{workingDirectory: filepath.Dir(request.Output), environment: environment})
	started := time.Now()
	output, runErr := runner(ctx, request.SFBin, args...)
	postSFSHA, sfErr := sha256File(request.SFBin)
	postPythonSHA, pythonErr := sha256File(request.PythonBin)
	if runErr != nil || output.ExitCode != 0 || ctx.Err() == context.DeadlineExceeded || sfErr != nil || pythonErr != nil || postSFSHA != sfSHA || postPythonSHA != pythonSHA {
		return SalesforceDevHubAuthority{}, fmt.Errorf("Dev Hub org display failed or execution tools changed")
	}
	orgID, status, username, err := parseSalesforceOrgDisplay(output.Stdout)
	if err != nil || status != "Connected" && status != "Active" {
		return SalesforceDevHubAuthority{}, fmt.Errorf("Dev Hub org display did not identify an active org")
	}
	command := CommandResult{Command: append([]string{request.SFBin}, args...), WorkingDirectory: filepath.Dir(request.Output), Environment: environment, ExecutableSHA256: sfSHA, ExecutableAfterSHA256: postSFSHA, CommandSpecSHA256: salesforceCommandSpecSHA256(request.SFBin, args, filepath.Dir(request.Output), environment, sfSHA, postSFSHA), ExitCode: output.ExitCode, DurationMS: time.Since(started).Milliseconds(), StdoutSHA256: replayBytesSHA256(output.Stdout), StderrSHA256: replayBytesSHA256(output.Stderr), Passed: true, TimedOut: false}
	authority := SalesforceDevHubAuthority{SchemaVersion: 2, Alias: request.TargetOrg, OrgID: orgID, Username: username, Execution: SalesforceExecutionAuthority{SFBinary: request.SFBin, SFSHA256: sfSHA, PythonBinary: request.PythonBin, PythonSHA256: pythonSHA, Environment: environment}, Command: command}
	if err := WriteNewJSON(request.Output, authority); err != nil {
		return SalesforceDevHubAuthority{}, err
	}
	return authority, nil
}

func validateDevHubExecutable(path string) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("Dev Hub executable must be a regular executable")
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil || canonical != path {
		return fmt.Errorf("Dev Hub executable must not be a symlink")
	}
	return nil
}

type SalesforceExecutionAuthority struct {
	SFBinary     string   `json:"sfBinary"`
	SFSHA256     string   `json:"sfSha256"`
	PythonBinary string   `json:"pythonBinary"`
	PythonSHA256 string   `json:"pythonSha256"`
	Environment  []string `json:"environment"`
}

func validSalesforceExecutionAuthority(authority SalesforceExecutionAuthority) bool {
	if !cleanAbsolutePath(authority.SFBinary) || !cleanAbsolutePath(authority.PythonBinary) || !sha256Pattern.MatchString(authority.SFSHA256) || !sha256Pattern.MatchString(authority.PythonSHA256) || len(authority.Environment) != 4 {
		return false
	}
	values := make(map[string]string, len(authority.Environment))
	for _, entry := range authority.Environment {
		name, value, ok := strings.Cut(entry, "=")
		if !ok || name == "" || value == "" || values[name] != "" {
			return false
		}
		values[name] = value
	}
	if values["SF_USE_GENERIC_UNIX_KEYCHAIN"] != "true" || !cleanAbsolutePath(values["HOME"]) || !cleanAbsolutePath(values["TMPDIR"]) {
		return false
	}
	pathDirectories := filepath.SplitList(values["PATH"])
	if len(pathDirectories) == 0 {
		return false
	}
	containsSF, containsPython := false, false
	for _, directory := range pathDirectories {
		if !cleanAbsolutePath(directory) {
			return false
		}
		containsSF = containsSF || directory == filepath.Dir(authority.SFBinary)
		containsPython = containsPython || directory == filepath.Dir(authority.PythonBinary)
	}
	return containsSF && containsPython
}

func cleanAbsolutePath(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path
}

type SalesforceOrgCreation struct {
	SchemaVersion  int           `json:"schemaVersion"`
	BundleSHA256   string        `json:"bundleSha256"`
	DevHub         string        `json:"devHub"`
	DevHubOrgID    string        `json:"devHubOrgId"`
	DevHubUsername string        `json:"devHubUsername"`
	Alias          string        `json:"alias"`
	Marker         string        `json:"marker"`
	OrgID          string        `json:"orgId"`
	Command        CommandResult `json:"command"`
	DevHubCommand  CommandResult `json:"devHubCommand"`
	Invalidated    bool          `json:"invalidated,omitempty"`
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
	SchemaVersion int           `json:"schemaVersion"`
	BundleSHA256  string        `json:"bundleSha256"`
	DevHub        string        `json:"devHub"`
	Alias         string        `json:"alias"`
	Marker        string        `json:"marker"`
	AliasAbsent   CommandResult `json:"aliasAbsent"`
}

type SalesforceOrgCleanup struct {
	SchemaVersion   int             `json:"schemaVersion"`
	BundleSHA256    string          `json:"bundleSha256"`
	DevHub          string          `json:"devHub"`
	DevHubOrgID     string          `json:"devHubOrgId"`
	DevHubUsername  string          `json:"devHubUsername"`
	OrgAlias        string          `json:"orgAlias"`
	OrgID           string          `json:"orgId"`
	Commands        []CommandResult `json:"commands"`
	DevHubCommand   CommandResult   `json:"devHubCommand"`
	ReservedOnly    bool            `json:"reservedOnly,omitempty"`
	RecoveredAbsent bool            `json:"recoveredAbsent,omitempty"`
	ResidueAbsent   bool            `json:"residueAbsent"`
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
	Fixture             string                      `json:"fixture"`
	FixtureSHA256       string                      `json:"fixtureSha256"`
	SourceFiles         []oracleSourceFile          `json:"sourceFiles"`
	OrgIdentity         salesforceFilterOrgIdentity `json:"orgIdentity"`
	Project             string                      `json:"project"`
	Invocation          *salesforceFilterInvocation `json:"invocation"`
	ProjectManifest     []salesforceExecutorFile    `json:"projectManifest"`
	ProjectTreeSHA256   string                      `json:"projectTreeSha256"`
	StdoutSHA256        string                      `json:"stdoutSha256"`
	StderrSHA256        string                      `json:"stderrSha256"`
	SetupSHA256         string                      `json:"setupSha256"`
	RuntimeSHA256       string                      `json:"runtimeSha256,omitempty"`
	RuntimeStderrSHA256 string                      `json:"runtimeStderrSha256,omitempty"`
	TestClasses         []string                    `json:"testClasses"`
	RuntimeExitCode     *int                        `json:"runtimeExitCode"`
	SurfaceIDs          []string                    `json:"surfaceIds"`
	Org                 string                      `json:"org"`
	Kind                string                      `json:"kind"`
	ExitCode            *int                        `json:"exitCode"`
	Deployable          bool                        `json:"deployable"`
	RuntimePassed       *bool                       `json:"runtimePassed"`
	RuntimeResult       json.RawMessage             `json:"runtimeResult"`
	OrgCleanup          CleanupReceipt              `json:"orgCleanup"`
}

type salesforceFilterInvocation struct {
	SFBinary    string                    `json:"sfBinary"`
	Environment map[string]string         `json:"environment"`
	TargetOrg   string                    `json:"targetOrg"`
	Commands    []salesforceFilterCommand `json:"commands"`
}

type salesforceFilterCommand struct {
	Purpose               string   `json:"purpose"`
	Args                  []string `json:"args"`
	ExecutableSHA256      string   `json:"executableSha256"`
	ExecutableAfterSHA256 string   `json:"executableAfterSha256"`
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
	Path                    string                  `json:"path,omitempty"`
	CleanupExitCode         *int                    `json:"cleanupExitCode,omitempty"`
	AbsenceCheckExitCode    *int                    `json:"absenceCheckExitCode,omitempty"`
	ExitCode                *int                    `json:"exitCode,omitempty"`
	Requested               []string                `json:"requested,omitempty"`
	Verification            *salesforceCleanupCheck `json:"verification,omitempty"`
	SFExecutableSHA256      string                  `json:"sfExecutableSha256,omitempty"`
	SFExecutableAfterSHA256 string                  `json:"sfExecutableAfterSha256,omitempty"`
	ResidueAbsent           bool                    `json:"residueAbsent"`
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
	Manifest       reportInputSnapshot
	Files          map[string][]byte
	Snapshots      map[string]reportInputSnapshot
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
	Shard    SalesforceShard
	Dispatch SalesforceDispatch
	Creation SalesforceOrgCreation
	Cleanup  SalesforceOrgCleanup
	Inputs   map[string]string
	Executor salesforceExecutorSnapshot
}

type SalesforceDispatch struct {
	SchemaVersion           int    `json:"schemaVersion"`
	BundleSHA256            string `json:"bundleSha256"`
	OrgAlias                string `json:"orgAlias"`
	ExecutorRoot            string `json:"executorRoot"`
	RunID                   string `json:"runId"`
	ShardIndex              int    `json:"shardIndex"`
	ShardCount              int    `json:"shardCount"`
	PythonSHA256            string `json:"pythonSha256"`
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
const salesforceOrgCreateTimeout = 10 * time.Minute
const salesforceFilterTimeout = 15 * time.Minute

func salesforceCommandTimeoutForArgs(args []string) time.Duration {
	if len(args) >= 3 && args[0] == "org" && args[1] == "create" && args[2] == "scratch" {
		return salesforceOrgCreateTimeout
	}
	return salesforceCommandTimeout
}

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
	if !filepath.IsAbs(request.BundlePath) || !filepath.IsAbs(request.ExecutorRoot) || !filepath.IsAbs(request.OutputPath) || request.OrgAlias == "" || request.RunID == "" || request.ShardCount < 1 || request.ShardIndex < 0 || request.ShardIndex >= request.ShardCount {
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
	executorRoot, runID, err := sealedSalesforceDispatchLayout(request.BundlePath, bundle.AttemptSHA256, request.ShardIndex, request.ShardCount)
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
	execution := bundle.SalesforceExecution
	if !validSalesforceExecutionAuthority(execution) {
		return SalesforceDispatch{}, fmt.Errorf("sealed Salesforce execution authority is invalid")
	}
	dispatch := SalesforceDispatch{SchemaVersion: 1, BundleSHA256: bundleSHA, OrgAlias: request.OrgAlias, ExecutorRoot: executorRoot, RunID: runID, ShardIndex: request.ShardIndex, ShardCount: request.ShardCount, PythonSHA256: execution.PythonSHA256, FilterCommandSpecSHA256: salesforceFilterCommandSpecSHA256(execution.PythonBinary, args, filepath.Dir(request.BundlePath), execution.Environment, execution.PythonSHA256, execution.PythonSHA256)}
	if err := WriteNewJSON(request.OutputPath, dispatch); err != nil {
		return SalesforceDispatch{}, err
	}
	return dispatch, nil
}

func sealedSalesforceDispatchIdentity(bundlePath, attemptSHA256 string, shardIndex, shardCount int) (string, string, error) {
	executorRoot, runID, err := sealedSalesforceDispatchLayout(bundlePath, attemptSHA256, shardIndex, shardCount)
	if err != nil {
		return "", "", err
	}
	if err := sealedSalesforceExecutorRoot(filepath.Dir(filepath.Dir(executorRoot)), executorRoot); err != nil {
		return "", "", err
	}
	return executorRoot, runID, nil
}

func sealedSalesforceDispatchLayout(bundlePath, attemptSHA256 string, shardIndex, shardCount int) (string, string, error) {
	canonicalBundle, err := filepath.EvalSymlinks(bundlePath)
	if err != nil {
		return "", "", fmt.Errorf("invalid staged bundle layout")
	}
	return salesforceDispatchLayoutAt(canonicalBundle, attemptSHA256, shardIndex, shardCount)
}

func salesforceDispatchLayoutAt(bundlePath, attemptSHA256 string, shardIndex, shardCount int) (string, string, error) {
	if !cleanAbsolutePath(bundlePath) || filepath.Base(filepath.Dir(bundlePath)) != "bundle" || filepath.Base(filepath.Dir(filepath.Dir(bundlePath))) != "salesforce-worker" || !sha256Pattern.MatchString(attemptSHA256) || shardCount < 1 || shardIndex < 0 || shardIndex >= shardCount {
		return "", "", fmt.Errorf("invalid staged bundle layout")
	}
	attemptRoot := filepath.Dir(filepath.Dir(filepath.Dir(bundlePath)))
	suffix := fmt.Sprintf("shard-%d", shardIndex)
	if shardCount != 2 {
		suffix = fmt.Sprintf("shard-%d-of-%d", shardIndex, shardCount)
	}
	return filepath.Join(attemptRoot, "executor", suffix), "assurance-" + attemptSHA256[:16] + "-" + suffix, nil
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
	if !filepath.IsAbs(request.BundlePath) || !filepath.IsAbs(request.DispatchPath) || !filepath.IsAbs(request.PreflightPath) || !filepath.IsAbs(request.OutputPath) || request.TargetOrg == "" {
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
	execution := bundle.SalesforceExecution
	if !validSalesforceExecutionAuthority(execution) || request.SFBin != execution.SFBinary {
		return SalesforceShard{}, fmt.Errorf("Salesforce shard does not use sealed execution authority")
	}
	if bundle.FilterSHA256 != approvedFilterSHA256(request.approvedFilterSHA256) {
		return SalesforceShard{}, fmt.Errorf("Salesforce bundle filter is not independently authorized")
	}
	dispatch, dispatchBytes, err := readExactJSONBytes[SalesforceDispatch](request.DispatchPath)
	if err != nil || !validSalesforceDispatch(dispatch, bundle, request.BundlePath) || dispatch.PythonSHA256 != execution.PythonSHA256 || dispatch.BundleSHA256 != bundleSHA || dispatch.OrgAlias != request.TargetOrg {
		return SalesforceShard{}, fmt.Errorf("invalid sealed Salesforce dispatch")
	}
	dispatchSHA := replayBytesSHA256(dispatchBytes)
	if executorRoot, runID, err := sealedSalesforceDispatchIdentity(request.BundlePath, bundle.AttemptSHA256, dispatch.ShardIndex, dispatch.ShardCount); err != nil || executorRoot != dispatch.ExecutorRoot || runID != dispatch.RunID {
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
	_, command, err := runSalesforceFilterCommand(filterRunner, execution, filepath.Dir(request.BundlePath), args...)
	if err != nil {
		return SalesforceShard{}, err
	}
	if command.CommandSpecSHA256 != dispatch.FilterCommandSpecSHA256 {
		return SalesforceShard{}, fmt.Errorf("Salesforce filter command does not match sealed dispatch")
	}
	if executorRoot, runID, err := sealedSalesforceDispatchIdentity(request.BundlePath, bundle.AttemptSHA256, dispatch.ShardIndex, dispatch.ShardCount); err != nil || executorRoot != dispatch.ExecutorRoot || runID != dispatch.RunID {
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
	if executorRoot, runID, err := sealedSalesforceDispatchIdentity(request.BundlePath, bundle.AttemptSHA256, dispatch.ShardIndex, dispatch.ShardCount); err != nil || executorRoot != dispatch.ExecutorRoot || runID != dispatch.RunID {
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
	filter, err := deriveSalesforceFilterEvidenceWithCLI(bundle, request.BundlePath, preflight.OrgAlias, preflight.OrgID, preflight.OrgUsername, preflight.Commands[0].ExecutableSHA256, postflight.Commands[0].ExecutableAfterSHA256, dispatch.ExecutorRoot, dispatch.RunID, dispatch.ShardIndex, snapshot)
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
	if after, err := readSealedSalesforceExecutor(dispatch.ExecutorRoot); err != nil || !reflect.DeepEqual(after, snapshot) {
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
	if !filepath.IsAbs(request.BundlePath) || !filepath.IsAbs(request.OutputPath) || request.Alias == "" {
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
	devHubAuthority, authorityErr := readSealedSalesforceDevHubAuthority(request.BundlePath)
	if authorityErr != nil {
		return SalesforceOrgCreation{}, authorityErr
	}
	if request.DevHub != "" && request.DevHub != devHubAuthority.Alias {
		return SalesforceOrgCreation{}, fmt.Errorf("caller-selected Dev Hub does not match sealed authority")
	}
	request.DevHub = devHubAuthority.Alias
	if request.SFBin != devHubAuthority.Execution.SFBinary {
		return SalesforceOrgCreation{}, fmt.Errorf("Salesforce org creation does not use sealed execution authority")
	}
	bundleSHA, err := sha256File(request.BundlePath)
	if err != nil {
		return SalesforceOrgCreation{}, err
	}
	definition := filepath.Join(filepath.Dir(request.BundlePath), "corpus-assurance-scratch-def.json")
	if data, err := os.ReadFile(definition); err != nil || !json.Valid(data) {
		return SalesforceOrgCreation{}, fmt.Errorf("sealed scratch definition is unavailable")
	}
	runner := request.runner
	if runner == nil {
		runner = runSalesforceCLI
	}
	_, aliasAbsent, err := runSalesforceExpectedCommand(runner, devHubAuthority.Execution, filepath.Dir(request.BundlePath), false, "org", "display", "--target-org", request.Alias, "--json")
	if err != nil || !validSalesforceReservedAliasAbsence(aliasAbsent, request.BundlePath, request.Alias) {
		return SalesforceOrgCreation{}, fmt.Errorf("Salesforce scratch alias is already in use or cannot be verified absent")
	}
	marker, err := newSalesforceScratchMarker()
	if err != nil {
		return SalesforceOrgCreation{}, fmt.Errorf("create Salesforce scratch marker: %w", err)
	}
	if err := WriteNewJSON(request.OutputPath+".reservation", salesforceOrgReservation{SchemaVersion: 1, BundleSHA256: bundleSHA, DevHub: request.DevHub, Alias: request.Alias, Marker: marker, AliasAbsent: aliasAbsent}); err != nil {
		return SalesforceOrgCreation{}, fmt.Errorf("reserve Salesforce org creation: %w", err)
	}
	output, command, err := runSalesforcePreflightCommand(runner, devHubAuthority.Execution, filepath.Dir(request.BundlePath), salesforceOrgCreateArgs(definition, request.DevHub, request.Alias, marker)...)
	if err != nil {
		return SalesforceOrgCreation{}, err
	}
	orgID, err := parseSalesforceOrgCreate(output.Stdout)
	if err != nil {
		return SalesforceOrgCreation{}, err
	}
	creation := SalesforceOrgCreation{SchemaVersion: 1, BundleSHA256: bundleSHA, DevHub: request.DevHub, DevHubOrgID: devHubAuthority.OrgID, DevHubUsername: devHubAuthority.Username, Alias: request.Alias, Marker: marker, OrgID: orgID, Command: command}
	sealInvalidated := func(cause error) (SalesforceOrgCreation, error) {
		invalidated := creation
		invalidated.Invalidated = true
		if sealErr := WriteNewJSON(request.OutputPath+".invalidated", invalidated); sealErr != nil {
			return SalesforceOrgCreation{}, fmt.Errorf("seal invalidated Salesforce org creation: %w", sealErr)
		}
		return SalesforceOrgCreation{}, cause
	}
	devHubOutput, devHubCommand, err := runSalesforceExpectedCommand(runner, devHubAuthority.Execution, filepath.Dir(request.BundlePath), true, "org", "display", "--target-org", request.DevHub, "--json")
	if err != nil {
		return sealInvalidated(err)
	}
	observedOrgID, _, observedUsername, err := parseSalesforceOrgDisplay(devHubOutput.Stdout)
	if err != nil || observedOrgID != devHubAuthority.OrgID || observedUsername != devHubAuthority.Username {
		return sealInvalidated(fmt.Errorf("Salesforce Dev Hub identity does not match sealed authority"))
	}
	creation.DevHubCommand = devHubCommand
	if err := validate(request.BundlePath); err != nil {
		return sealInvalidated(fmt.Errorf("staged bundle changed during org creation: %w", err))
	}
	if hash, err := sha256File(request.BundlePath); err != nil || hash != bundleSHA {
		return sealInvalidated(fmt.Errorf("staged bundle changed during org creation"))
	}
	if err := WriteNewJSON(request.OutputPath, creation); err != nil {
		return sealInvalidated(err)
	}
	return creation, nil
}

// RunSalesforceOrgCleanup deletes only an org whose exact creation and
// preflight receipts bind it to this bundle, then verifies the alias is gone.
func RunSalesforceOrgCleanup(request SalesforceOrgCleanupRequest) (SalesforceOrgCleanup, error) {
	if !filepath.IsAbs(request.BundlePath) || !filepath.IsAbs(request.CreationPath) || (request.PreflightPath != "" && !filepath.IsAbs(request.PreflightPath)) || !filepath.IsAbs(request.OutputPath) || request.TargetOrg == "" {
		return SalesforceOrgCleanup{}, fmt.Errorf("invalid Salesforce cleanup request")
	}
	if _, err := os.Lstat(request.OutputPath); err == nil {
		return SalesforceOrgCleanup{}, fmt.Errorf("Salesforce cleanup output already exists: %s", request.OutputPath)
	} else if !os.IsNotExist(err) {
		return SalesforceOrgCleanup{}, err
	}
	validate := request.validateBundle
	if validate == nil {
		validate = ValidateOracleBundle
	}
	if request.PreflightPath == "" {
		devHubAuthority, authorityErr := readSealedSalesforceDevHubAuthority(request.BundlePath)
		if authorityErr != nil {
			return SalesforceOrgCleanup{}, authorityErr
		}
		if request.DevHub != "" && request.DevHub != devHubAuthority.Alias {
			return SalesforceOrgCleanup{}, fmt.Errorf("caller-selected Dev Hub does not match sealed authority")
		}
		request.DevHub = devHubAuthority.Alias
		if request.SFBin != devHubAuthority.Execution.SFBinary {
			return SalesforceOrgCleanup{}, fmt.Errorf("Salesforce cleanup does not use sealed execution authority")
		}
		if _, err := os.Stat(request.CreationPath); os.IsNotExist(err) {
			if _, invalidatedErr := os.Stat(request.CreationPath + ".invalidated"); invalidatedErr == nil {
				request.CreationPath += ".invalidated"
			} else {
				request.CreationPath += ".reservation"
				return runReservedSalesforceOrgCleanup(request, devHubAuthority)
			}
		}
		return runInvalidatedSalesforceOrgCleanup(request)
	}
	if err := validate(request.BundlePath); err != nil {
		return SalesforceOrgCleanup{}, fmt.Errorf("validate staged bundle: %w", err)
	}
	devHubAuthority, authorityErr := readSealedSalesforceDevHubAuthority(request.BundlePath)
	if authorityErr != nil {
		return SalesforceOrgCleanup{}, authorityErr
	}
	if request.DevHub != "" && request.DevHub != devHubAuthority.Alias {
		return SalesforceOrgCleanup{}, fmt.Errorf("caller-selected Dev Hub does not match sealed authority")
	}
	request.DevHub = devHubAuthority.Alias
	if request.SFBin != devHubAuthority.Execution.SFBinary {
		return SalesforceOrgCleanup{}, fmt.Errorf("Salesforce cleanup does not use sealed execution authority")
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
	devHubOutput, devHubCommand, err := runSalesforceExpectedCommand(runner, devHubAuthority.Execution, filepath.Dir(request.BundlePath), true, "org", "display", "--target-org", request.DevHub, "--json")
	if err != nil {
		return SalesforceOrgCleanup{}, err
	}
	observedOrgID, _, observedUsername, err := parseSalesforceOrgDisplay(devHubOutput.Stdout)
	if err != nil || observedOrgID != devHubAuthority.OrgID || observedUsername != devHubAuthority.Username {
		return SalesforceOrgCleanup{}, fmt.Errorf("Salesforce Dev Hub identity does not match sealed authority")
	}
	_, deleted, deleteErr := runSalesforceExpectedCommand(runner, devHubAuthority.Execution, filepath.Dir(request.BundlePath), true, "org", "delete", "scratch", "--target-org", creation.Alias, "--no-prompt", "--json")
	recoveredAbsent := false
	cleanupCommands := []CommandResult{deleted}
	if deleteErr != nil {
		serverCommands, recoveryErr := cleanupSalesforceServerOrgByID(runner, devHubAuthority, filepath.Dir(request.BundlePath), creation.OrgID)
		if recoveryErr != nil {
			return SalesforceOrgCleanup{}, recoveryErr
		}
		cleanupCommands = append(cleanupCommands, serverCommands...)
		recoveredAbsent = true
	} else {
		absentOutput, absent, err := runSalesforceExpectedCommand(runner, devHubAuthority.Execution, filepath.Dir(request.BundlePath), false, "org", "display", "--target-org", creation.Alias, "--json")
		if err != nil || !validSalesforceOrgDisplayFailure(absentOutput.Stdout) {
			if err != nil {
				return SalesforceOrgCleanup{}, err
			}
			return SalesforceOrgCleanup{}, fmt.Errorf("Salesforce cleanup did not verify org absence")
		}
		cleanupCommands = append(cleanupCommands, absent)
	}
	if err := validate(request.BundlePath); err != nil {
		return SalesforceOrgCleanup{}, fmt.Errorf("staged bundle changed during cleanup: %w", err)
	}
	for _, input := range []struct{ path, hash string }{{request.BundlePath, bundleSHA}, {request.CreationPath, replayBytesSHA256(creationBytes)}, {request.PreflightPath, replayBytesSHA256(preflightBytes)}} {
		if hash, err := sha256File(input.path); err != nil || hash != input.hash {
			return SalesforceOrgCleanup{}, fmt.Errorf("Salesforce cleanup input changed during execution")
		}
	}
	cleanup := SalesforceOrgCleanup{SchemaVersion: 1, BundleSHA256: bundleSHA, DevHub: request.DevHub, DevHubOrgID: creation.DevHubOrgID, DevHubUsername: creation.DevHubUsername, OrgAlias: creation.Alias, OrgID: creation.OrgID, Commands: cleanupCommands, DevHubCommand: devHubCommand, RecoveredAbsent: recoveredAbsent, ResidueAbsent: true}
	if err := WriteNewJSON(request.OutputPath, cleanup); err != nil {
		return SalesforceOrgCleanup{}, err
	}
	return cleanup, nil
}

func runInvalidatedSalesforceOrgCleanup(request SalesforceOrgCleanupRequest) (SalesforceOrgCleanup, error) {
	authority, authorityErr := readSealedSalesforceDevHubAuthority(request.BundlePath)
	if authorityErr != nil || request.SFBin != authority.Execution.SFBinary {
		return SalesforceOrgCleanup{}, fmt.Errorf("invalid Salesforce cleanup execution authority")
	}
	creation, creationBytes, err := readExactJSONBytes[SalesforceOrgCreation](request.CreationPath)
	if err != nil || (!validInvalidatedSalesforceOrgCreation(creation, request.DevHub, request.TargetOrg) && !validSalesforceOrgCreationWithoutPreflight(creation, request.BundlePath, request.DevHub, request.TargetOrg)) {
		return SalesforceOrgCleanup{}, fmt.Errorf("invalidated Salesforce org creation receipt is invalid")
	}
	runner := request.runner
	if runner == nil {
		runner = runSalesforceCLI
	}
	devHubOutput, devHubCommand, err := runSalesforceExpectedCommand(runner, authority.Execution, filepath.Dir(request.BundlePath), true, "org", "display", "--target-org", request.DevHub, "--json")
	if err != nil {
		return SalesforceOrgCleanup{}, err
	}
	observedOrgID, _, observedUsername, err := parseSalesforceOrgDisplay(devHubOutput.Stdout)
	if err != nil || observedOrgID != creation.DevHubOrgID || observedUsername != creation.DevHubUsername {
		return SalesforceOrgCleanup{}, fmt.Errorf("Salesforce Dev Hub identity does not match sealed authority")
	}
	_, deleted, err := runSalesforceExpectedCommand(runner, authority.Execution, filepath.Dir(request.BundlePath), true, "org", "delete", "scratch", "--target-org", creation.Alias, "--no-prompt", "--json")
	if err != nil {
		return SalesforceOrgCleanup{}, err
	}
	_, absent, err := runSalesforceExpectedCommand(runner, authority.Execution, filepath.Dir(request.BundlePath), false, "org", "display", "--target-org", creation.Alias, "--json")
	if err != nil {
		return SalesforceOrgCleanup{}, err
	}
	if after, err := sha256File(request.CreationPath); err != nil || after != replayBytesSHA256(creationBytes) {
		return SalesforceOrgCleanup{}, fmt.Errorf("invalidated creation receipt changed during cleanup")
	}
	cleanup := SalesforceOrgCleanup{SchemaVersion: 1, BundleSHA256: creation.BundleSHA256, DevHub: request.DevHub, DevHubOrgID: creation.DevHubOrgID, DevHubUsername: creation.DevHubUsername, OrgAlias: creation.Alias, OrgID: creation.OrgID, Commands: []CommandResult{deleted, absent}, DevHubCommand: devHubCommand, ResidueAbsent: true}
	if err := WriteNewJSON(request.OutputPath, cleanup); err != nil {
		return SalesforceOrgCleanup{}, err
	}
	return cleanup, nil
}

func runReservedSalesforceOrgCleanup(request SalesforceOrgCleanupRequest, authority SalesforceDevHubAuthority) (SalesforceOrgCleanup, error) {
	reservation, reservationBytes, err := readExactJSONBytes[salesforceOrgReservation](request.CreationPath)
	bundleSHA, hashErr := sha256File(request.BundlePath)
	if err != nil || hashErr != nil || reservation.SchemaVersion != 1 || reservation.BundleSHA256 != bundleSHA || reservation.DevHub != authority.Alias || reservation.Alias != request.TargetOrg || !validSalesforceScratchMarker(reservation.Marker) || !validSalesforceReservedAliasAbsence(reservation.AliasAbsent, request.BundlePath, reservation.Alias) {
		return SalesforceOrgCleanup{}, fmt.Errorf("Salesforce org reservation is invalid")
	}
	runner := request.runner
	if runner == nil {
		runner = runSalesforceCLI
	}
	devHubOutput, devHubCommand, err := runSalesforceExpectedCommand(runner, authority.Execution, filepath.Dir(request.BundlePath), true, "org", "display", "--target-org", authority.Alias, "--json")
	if err != nil {
		return SalesforceOrgCleanup{}, err
	}
	observedID, _, observedUsername, err := parseSalesforceOrgDisplay(devHubOutput.Stdout)
	if err != nil || observedID != authority.OrgID || observedUsername != authority.Username {
		return SalesforceOrgCleanup{}, fmt.Errorf("Salesforce Dev Hub identity does not match sealed authority")
	}
	markerOrgID, markerCommands, err := inspectReservedSalesforceServerOrg(runner, authority, filepath.Dir(request.BundlePath), reservation.Marker)
	if err != nil {
		return SalesforceOrgCleanup{}, fmt.Errorf("inspect reserved Salesforce server org: %w", err)
	}
	aliasOutput, observed, err := runSealedSalesforceCommand(runner, authority.Execution, filepath.Dir(request.BundlePath), "org", "display", "--target-org", reservation.Alias, "--json")
	if err != nil {
		return SalesforceOrgCleanup{}, fmt.Errorf("inspect reserved Salesforce alias: %w", err)
	}
	if observed.TimedOut {
		return SalesforceOrgCleanup{}, fmt.Errorf("inspect reserved Salesforce alias timed out")
	}
	commands := append(markerCommands, observed)
	orgID := ""
	if observed.Passed {
		orgID, _, _, err = parseSalesforceOrgDisplay(aliasOutput.Stdout)
		if err != nil {
			return SalesforceOrgCleanup{}, fmt.Errorf("reserved Salesforce alias has invalid org identity")
		}
		if markerOrgID != "" && orgID == markerOrgID {
			_, deleted, err := runSalesforceExpectedCommand(runner, authority.Execution, filepath.Dir(request.BundlePath), true, "org", "delete", "scratch", "--target-org", reservation.Alias, "--no-prompt", "--json")
			if err != nil {
				return SalesforceOrgCleanup{}, err
			}
			_, absent, err := runSalesforceExpectedCommand(runner, authority.Execution, filepath.Dir(request.BundlePath), false, "org", "display", "--target-org", reservation.Alias, "--json")
			if err != nil || !validSalesforceOrgDisplayFailure(absent.Output.Stdout) {
				return SalesforceOrgCleanup{}, fmt.Errorf("reserved Salesforce alias remains after cleanup")
			}
			commands = append(commands, deleted, absent)
		}
	} else if !validSalesforceOrgDisplayFailure(aliasOutput.Stdout) {
		return SalesforceOrgCleanup{}, fmt.Errorf("reserved Salesforce alias absence is invalid")
	}
	serverOrgID, serverCommands, err := cleanupReservedSalesforceServerOrg(runner, authority, filepath.Dir(request.BundlePath), reservation.Marker)
	if err != nil {
		return SalesforceOrgCleanup{}, fmt.Errorf("verify reserved Salesforce server cleanup: %w", err)
	}
	if markerOrgID != "" && serverOrgID != "" && markerOrgID != serverOrgID {
		return SalesforceOrgCleanup{}, fmt.Errorf("reserved marker changed scratch org identity")
	}
	if serverOrgID != "" {
		orgID = serverOrgID
	} else if markerOrgID != "" {
		orgID = markerOrgID
	} else {
		orgID = ""
	}
	commands = append(commands, serverCommands...)
	if after, err := sha256File(request.CreationPath); err != nil || after != replayBytesSHA256(reservationBytes) {
		return SalesforceOrgCleanup{}, fmt.Errorf("Salesforce org reservation changed during cleanup")
	}
	cleanup := SalesforceOrgCleanup{SchemaVersion: 1, BundleSHA256: bundleSHA, DevHub: authority.Alias, DevHubOrgID: authority.OrgID, DevHubUsername: authority.Username, OrgAlias: reservation.Alias, OrgID: orgID, Commands: commands, DevHubCommand: devHubCommand, ReservedOnly: true, ResidueAbsent: true}
	if err := WriteNewJSON(request.OutputPath, cleanup); err != nil {
		return SalesforceOrgCleanup{}, err
	}
	return cleanup, nil
}

func cleanupSalesforceServerOrgByID(runner salesforceCommandRunner, authority SalesforceDevHubAuthority, workingDirectory, orgID string) ([]CommandResult, error) {
	if !validSalesforceRecordID(orgID, "00D") {
		return nil, fmt.Errorf("invalid Salesforce org ID")
	}
	activeQuery := salesforceActiveScratchOrgQuery(orgID)
	activeOutput, activeReceipt, err := runSalesforceExpectedCommand(runner, authority.Execution, workingDirectory, true, "data", "query", "--target-org", authority.Alias, "--query", activeQuery, "--json")
	commands := []CommandResult{activeReceipt}
	activeRecords, parseErr := parseSalesforceActiveScratchOrg(activeOutput.Stdout)
	if err != nil || parseErr != nil || len(activeRecords) > 1 {
		return commands, fmt.Errorf("invalid ActiveScratchOrg result")
	}
	if len(activeRecords) == 0 {
		return commands, nil
	}
	active := activeRecords[0]
	if active.ScratchOrg != orgID || !validSalesforceRecordID(active.ID, "2AS") {
		return commands, fmt.Errorf("invalid active scratch org record")
	}
	_, deleted, err := runSalesforceExpectedCommand(runner, authority.Execution, workingDirectory, true, "data", "delete", "record", "--target-org", authority.Alias, "--sobject", "ActiveScratchOrg", "--record-id", active.ID, "--json")
	commands = append(commands, deleted)
	if err != nil {
		return commands, err
	}
	verifyQuery := salesforceActiveScratchOrgQuery(orgID)
	verifyOutput, verified, err := runSalesforceExpectedCommand(runner, authority.Execution, workingDirectory, true, "data", "query", "--target-org", authority.Alias, "--query", verifyQuery, "--json")
	commands = append(commands, verified)
	remaining, parseErr := parseSalesforceActiveScratchOrg(verifyOutput.Stdout)
	if err != nil || parseErr != nil || len(remaining) != 0 {
		return commands, fmt.Errorf("active scratch org remains after server cleanup")
	}
	return commands, nil
}

func salesforceActiveScratchOrgQuery(orgID string) string {
	return "SELECT Id, ScratchOrg FROM ActiveScratchOrg WHERE ScratchOrg = '" + orgID + "'"
}

func cleanupReservedSalesforceServerOrg(runner salesforceCommandRunner, authority SalesforceDevHubAuthority, workingDirectory, marker string) (string, []CommandResult, error) {
	scratchOrgID, commands, err := inspectReservedSalesforceServerOrg(runner, authority, workingDirectory, marker)
	if err != nil || scratchOrgID == "" {
		return scratchOrgID, commands, err
	}
	serverCommands, cleanupErr := cleanupSalesforceServerOrgByID(runner, authority, workingDirectory, scratchOrgID)
	commands = append(commands, serverCommands...)
	return scratchOrgID, commands, cleanupErr
}

func inspectReservedSalesforceServerOrg(runner salesforceCommandRunner, authority SalesforceDevHubAuthority, workingDirectory, marker string) (string, []CommandResult, error) {
	scratchQuery := "SELECT Id, ScratchOrg, SignupUsername, Status FROM ScratchOrgInfo WHERE OrgName = '" + marker + "'"
	scratchOutput, scratchReceipt, err := runSalesforceExpectedCommand(runner, authority.Execution, workingDirectory, true, "data", "query", "--target-org", authority.Alias, "--query", scratchQuery, "--json")
	if err != nil {
		return "", nil, err
	}
	commands := []CommandResult{scratchReceipt}
	scratchRecords, err := parseSalesforceScratchOrgInfo(scratchOutput.Stdout)
	if err != nil || len(scratchRecords) > 1 {
		return "", commands, fmt.Errorf("invalid ScratchOrgInfo marker result")
	}
	if len(scratchRecords) == 0 {
		return "", commands, nil
	}
	scratch := scratchRecords[0]
	if !validSalesforceRecordID(scratch.ID, "2SR") || scratch.Status == "" {
		return "", commands, fmt.Errorf("invalid scratch org request record")
	}
	if scratch.ScratchOrg == "" {
		if scratch.Status == "Error" {
			return "", commands, nil
		}
		return "", commands, fmt.Errorf("scratch org request remains pending")
	}
	if !validSalesforceRecordID(scratch.ScratchOrg, "00D") {
		return "", commands, fmt.Errorf("invalid scratch org id")
	}
	return scratch.ScratchOrg, commands, nil
}

type salesforceScratchOrgInfo struct {
	ID             string `json:"Id"`
	ScratchOrg     string `json:"ScratchOrg"`
	SignupUsername string `json:"SignupUsername"`
	Status         string `json:"Status"`
}

type salesforceActiveScratchOrg struct {
	ID         string `json:"Id"`
	ScratchOrg string `json:"ScratchOrg"`
}

func parseSalesforceScratchOrgInfo(data []byte) ([]salesforceScratchOrgInfo, error) {
	return parseSalesforceQueryRecords[salesforceScratchOrgInfo](data)
}

func parseSalesforceActiveScratchOrg(data []byte) ([]salesforceActiveScratchOrg, error) {
	return parseSalesforceQueryRecords[salesforceActiveScratchOrg](data)
}

func parseSalesforceQueryRecords[T any](data []byte) ([]T, error) {
	var payload struct {
		Status *int `json:"status"`
		Result struct {
			TotalSize int `json:"totalSize"`
			Records   []T `json:"records"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &payload); err != nil || payload.Status == nil || *payload.Status != 0 || payload.Result.TotalSize != len(payload.Result.Records) {
		return nil, fmt.Errorf("invalid Salesforce query result")
	}
	return payload.Result.Records, nil
}

func newSalesforceScratchMarker() (string, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return "glade-assurance-" + hex.EncodeToString(random), nil
}

func validSalesforceScratchMarker(marker string) bool {
	const prefix = "glade-assurance-"
	if len(marker) != len(prefix)+32 || !strings.HasPrefix(marker, prefix) {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(marker, prefix))
	return err == nil
}

func validSalesforceRecordID(id, prefix string) bool {
	if (len(id) != 15 && len(id) != 18) || !strings.HasPrefix(id, prefix) {
		return false
	}
	for _, character := range id {
		if character < '0' || character > '9' && character < 'A' || character > 'Z' && character < 'a' || character > 'z' {
			return false
		}
	}
	return true
}

func validSalesforceReservedAliasAbsence(command CommandResult, bundlePath, alias string) bool {
	if !validRetainedCommandOutput(command) || command.Passed || command.ExitCode == 0 || command.TimedOut || !validSalesforceOrgDisplayFailure(command.Output.Stdout) {
		return false
	}
	args := []string{"org", "display", "--target-org", alias, "--json"}
	execution, err := sealedSalesforceExecution(bundlePath)
	return err == nil && validSalesforceCommandReceipt(command, execution, bundlePath, args)
}

func validInvalidatedSalesforceOrgCreation(creation SalesforceOrgCreation, devHub, alias string) bool {
	command := creation.Command
	if !creation.Invalidated || creation.SchemaVersion != 1 || !sha256Pattern.MatchString(creation.BundleSHA256) || creation.DevHub != devHub || creation.Alias != alias || !validSalesforceScratchMarker(creation.Marker) || creation.OrgID == "" || !validRetainedCommandOutput(command) || !command.Passed || command.ExitCode != 0 || command.TimedOut {
		return false
	}
	bundlePath := filepath.Join(command.WorkingDirectory, "bundle.json")
	execution, err := sealedSalesforceExecution(bundlePath)
	if err != nil {
		return false
	}
	args := salesforceOrgCreateArgs(filepath.Join(command.WorkingDirectory, "corpus-assurance-scratch-def.json"), devHub, alias, creation.Marker)
	orgID, outputErr := retainedSalesforceOrgCreate(command)
	return outputErr == nil && orgID == creation.OrgID && validSalesforceCommandReceipt(command, execution, bundlePath, args)
}

func validSalesforceOrgCreationWithoutPreflight(creation SalesforceOrgCreation, bundlePath, devHub, alias string) bool {
	bundleSHA, err := sha256File(bundlePath)
	return err == nil && validSalesforceOrgCreation(creation, bundleSHA, bundlePath, devHub, alias)
}

func runSalesforceExpectedCommand(runner salesforceCommandRunner, execution SalesforceExecutionAuthority, workingDirectory string, expectedSuccess bool, args ...string) (salesforceCommandOutput, CommandResult, error) {
	output, receipt, err := runSealedSalesforceCommand(runner, execution, workingDirectory, args...)
	if err != nil || receipt.TimedOut || (expectedSuccess != receipt.Passed) {
		return output, receipt, fmt.Errorf("Salesforce cleanup command did not have expected result")
	}
	return output, receipt, nil
}

func runSalesforceFilterCommand(runner salesforceCommandRunner, execution SalesforceExecutionAuthority, workingDirectory string, args ...string) (salesforceCommandOutput, CommandResult, error) {
	if !validSalesforceExecutionAuthority(execution) || !filepath.IsAbs(workingDirectory) {
		return salesforceCommandOutput{}, CommandResult{}, fmt.Errorf("invalid Salesforce filter execution")
	}
	binary, environment := execution.PythonBinary, execution.Environment
	before, hashErr := sha256File(binary)
	if hashErr != nil || before != execution.PythonSHA256 {
		return salesforceCommandOutput{}, CommandResult{}, fmt.Errorf("sealed Python interpreter is unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), salesforceFilterTimeout)
	defer cancel()
	started := time.Now()
	ctx = context.WithValue(ctx, salesforceExecutionKey{}, salesforceExecution{workingDirectory: workingDirectory, environment: environment})
	output, err := runner(ctx, binary, args...)
	after, afterErr := sha256File(binary)
	receipt := CommandResult{Command: append([]string{binary}, args...), WorkingDirectory: workingDirectory, Environment: environment, ExecutableSHA256: before, ExecutableAfterSHA256: after, CommandSpecSHA256: salesforceFilterCommandSpecSHA256(binary, args, workingDirectory, environment, before, after), ExitCode: output.ExitCode, DurationMS: time.Since(started).Milliseconds(), StdoutSHA256: replayBytesSHA256(output.Stdout), StderrSHA256: replayBytesSHA256(output.Stderr), Output: retainedCommandOutput(output), Passed: err == nil && afterErr == nil && before == after && after == execution.PythonSHA256 && output.ExitCode == 0, TimedOut: ctx.Err() == context.DeadlineExceeded}
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
	return deriveSalesforceFilterEvidenceWithCLI(bundle, bundlePath, orgAlias, orgID, orgUsername, "", "", executorRoot, runID, shardIndex, snapshot)
}

func deriveSalesforceFilterEvidenceWithCLI(bundle OracleBundle, bundlePath, orgAlias, orgID, orgUsername, sfExecutableSHA256, sfExecutableAfterSHA256, executorRoot, runID string, shardIndex int, snapshot salesforceExecutorSnapshot) (salesforceFilterResults, error) {
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
	if !adapter.Sealed || !equalStrings(adapter.Orgs, []string{orgAlias}) || !adapter.OrgPostflight.MatchesPreflight {
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
		if !hasAdapterResult || adapterResult.FixtureSHA256 != fixture.SHA256 || !reflect.DeepEqual(adapterResult.SourceFiles, fixture.SourceFiles) || !equalStrings(sortedStrings(adapterResult.SurfaceIDs), item.SurfaceIDs) || adapterResult.Org != orgAlias || adapterResult.Kind != item.Kind || adapterResult.ExitCode == nil || (*adapterResult.ExitCode == 0) != adapterResult.Deployable {
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
		if adapterResult.Project != expectedProject || adapterResult.OrgIdentity != (salesforceFilterOrgIdentity{Alias: orgAlias, OrgID: orgID, Username: orgUsername}) || !validSalesforceTestClasses(adapterResult.TestClasses, kind) || !validSalesforceInvocation(adapterResult.Invocation, bundle.SalesforceExecution.SFBinary, expectedProject, orgUsername, kind, adapterResult.TestClasses, sfExecutableSHA256, sfExecutableAfterSHA256) || !validSalesforceOrgCleanupReceipt(adapterResult.OrgCleanup, sfExecutableSHA256, sfExecutableAfterSHA256) {
			return salesforceFilterResults{}, fmt.Errorf("Salesforce fixture %q has an unbound execution receipt", item.Fixture)
		}
		base := filepath.ToSlash(filepath.Join("filter", "projects", stem, "salesforce-"+orgAlias))
		deploy, deployOK := snapshot.Files[base+".json"]
		stderr, stderrOK := snapshot.Files[base+".stderr"]
		setup, setupOK := snapshot.Files[base+".setup"]
		if !deployOK || !stderrOK || !setupOK {
			return salesforceFilterResults{}, fmt.Errorf("Salesforce fixture %q lacks retained command evidence", item.Fixture)
		}
		deployPassed := *adapterResult.ExitCode == 0 && adapterResult.Deployable
		if deployPassed && kind == "exec" && !validSalesforceRuntimeObservation(kind, deploy) {
			return salesforceFilterResults{}, fmt.Errorf("Salesforce runtime fixture %q lacks raw success", item.Fixture)
		} else if deployPassed && kind != "exec" && !validSalesforceDeployObservationForProject(deploy, snapshot.Files, filepath.ToSlash(filepath.Join("filter", "projects", stem))) {
			return salesforceFilterResults{}, fmt.Errorf("Salesforce deploy fixture %q lacks raw success", item.Fixture)
		} else if !deployPassed && (kind != "exec" || len(item.SurfaceIDs) != 1 || !validSalesforceFailureObservation(deploy)) {
			return salesforceFilterResults{}, fmt.Errorf("Salesforce fixture %q lacks raw failure", item.Fixture)
		}
		projectTreeSHA, err := salesforceFixtureProjectTreeSHA256(snapshot.Files, filepath.ToSlash(filepath.Join("filter", "projects", stem)))
		if err != nil {
			return salesforceFilterResults{}, err
		}
		if !validSalesforceProjectManifest(adapterResult.ProjectManifest, projectTreeSHA) {
			return salesforceFilterResults{}, fmt.Errorf("Salesforce fixture %q project tree does not match the sealed transport receipt", item.Fixture)
		}
		exitCode := *adapterResult.ExitCode
		row := salesforceFilterFixtureResult{Fixture: item.Fixture, FixtureSHA256: fixture.SHA256, SourceFiles: append([]oracleSourceFile(nil), fixture.SourceFiles...), OrgIdentity: adapterResult.OrgIdentity, Project: expectedProject, Invocation: adapterResult.Invocation, ProjectManifest: append([]salesforceExecutorFile(nil), adapterResult.ProjectManifest...), ProjectTreeSHA256: projectTreeSHA, StdoutSHA256: replayBytesSHA256(deploy), StderrSHA256: replayBytesSHA256(stderr), SetupSHA256: replayBytesSHA256(setup), TestClasses: append([]string(nil), adapterResult.TestClasses...), RuntimeExitCode: adapterResult.RuntimeExitCode, SurfaceIDs: append([]string(nil), item.SurfaceIDs...), Org: orgAlias, Kind: kind, ExitCode: &exitCode, Deployable: adapterResult.Deployable, OrgCleanup: adapterResult.OrgCleanup}
		if kind == "exec" {
			passed := deployPassed
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
			passed := true
			row.RuntimePassed, row.RuntimeResult = &passed, append(json.RawMessage(nil), runtime...)
		}
		derived = append(derived, row)
	}
	return salesforceFilterResults{Sealed: true, Orgs: []string{orgAlias}, Binding: salesforceFilterBinding{ManifestSHA256: bundle.TransportManifestSHA256, ProfileSHA256: bundle.ProfileSHA256, QueueSHA256: bundle.OraclePlanSHA256, SelectorSHA256: bundle.OraclePlanSHA256, SelectorReceiptSHA256: "", CandidateCommit: bundle.Candidate.Commit, CandidateSHA256: bundle.Candidate.SHA256, ToolsCommit: bundle.Tools.Commit, ToolsAMD64SHA256: bundle.ToolsAMD64SHA256, WorkflowScriptSHA256: bundle.FilterSHA256, LocalSummarySHA256: bundle.LocalProofSummarySHA256}, OrgPostflight: adapter.OrgPostflight, Results: derived}, nil
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
		relative := strings.TrimPrefix(path, prefix)
		if !strings.HasPrefix(path, prefix) || strings.HasPrefix(filepath.Base(path), "salesforce-") || strings.HasPrefix(relative, ".sf/") {
			continue
		}
		entries = append(entries, salesforceExecutorFile{Path: relative, SHA256: replayBytesSHA256(data)})
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

func validSalesforceInvocation(invocation *salesforceFilterInvocation, sfBinary, project, org, kind string, testClasses []string, expectedBefore, expectedAfter string) bool {
	if invocation == nil || invocation.SFBinary != sfBinary || !reflect.DeepEqual(invocation.Environment, map[string]string{"SF_USE_GENERIC_UNIX_KEYCHAIN": "true"}) || invocation.TargetOrg != org || len(invocation.Commands) == 0 {
		return false
	}
	command := expectedSalesforceCommand(sfBinary, project, org, kind)
	if invocation.Commands[0].Purpose != "deploy-or-exec" || !equalStrings(invocation.Commands[0].Args, command) || !validSalesforceCLIHashes(invocation.Commands[0], expectedBefore, expectedAfter) {
		return false
	}
	if kind != "test" {
		return len(invocation.Commands) == 1
	}
	if len(invocation.Commands) != 2 || !validSalesforceTestClasses(testClasses, kind) {
		return false
	}
	runtime := expectedSalesforceRuntimeTestCommand(sfBinary, project, org, testClasses)
	return invocation.Commands[1].Purpose == "runtime-test" && equalStrings(invocation.Commands[1].Args, runtime) && validSalesforceCLIHashes(invocation.Commands[1], expectedBefore, expectedAfter)
}

func validSalesforceCLIHashes(command salesforceFilterCommand, expectedBefore, expectedAfter string) bool {
	return sha256Pattern.MatchString(command.ExecutableSHA256) && command.ExecutableSHA256 == command.ExecutableAfterSHA256 && (expectedBefore == "" || command.ExecutableSHA256 == expectedBefore && command.ExecutableAfterSHA256 == expectedAfter)
}

func expectedSalesforceRuntimeTestCommand(sfBinary, project, org string, testClasses []string) []string {
	parts := []string{sfBinary, "apex", "run", "test", "--tests", strings.Join(testClasses, ","), "--target-org", org}
	if len(testClasses) == 1 {
		parts = append(parts, "--synchronous")
	}
	return append(parts, "--wait", "10", "--result-format", "json", "--json")
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

func validSalesforceOrgCleanupReceipt(cleanup CleanupReceipt, expectedBefore, expectedAfter string) bool {
	return cleanup.CleanupExitCode != nil && *cleanup.CleanupExitCode == 0 && cleanup.Verification != nil && len(cleanup.Verification.Remaining) == 0 && cleanup.ResidueAbsent && (expectedBefore == "" || cleanup.SFExecutableSHA256 == expectedBefore && cleanup.SFExecutableAfterSHA256 == expectedAfter)
}

func expectedSalesforceCommand(sfBinary, project, org, kind string) []string {
	parts := []string{sfBinary}
	if kind == "exec" {
		return append(parts, "apex", "run", "--file", filepath.Join(project, "anonymous.apex"), "--target-org", org, "--api-version", "67.0", "--json")
	} else {
		return append(parts, "project", "deploy", "start", "--source-dir", filepath.Join(project, "force-app"), "--target-org", org, "--ignore-conflicts", "--wait", "30", "--json")
	}
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
	if !filepath.IsAbs(request.BundlePath) || !filepath.IsAbs(request.OutputPath) || request.TargetOrg == "" {
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
	authority, err := readSealedSalesforceDevHubAuthority(request.BundlePath)
	if err != nil || request.SFBin != authority.Execution.SFBinary {
		return SalesforceOrgPreflight{}, fmt.Errorf("Salesforce preflight does not use sealed execution authority")
	}
	runner := request.runner
	if runner == nil {
		runner = runSalesforceCLI
	}
	commands := salesforcePreflightArgs(request.TargetOrg)
	display, displayReceipt, err := runSalesforcePreflightCommand(runner, authority.Execution, filepath.Dir(request.BundlePath), commands[0]...)
	if err != nil {
		return SalesforceOrgPreflight{}, err
	}
	orgID, status, username, err := parseSalesforceOrgDisplay(display.Stdout)
	if err != nil || status != "Active" {
		return SalesforceOrgPreflight{}, fmt.Errorf("scratch org is not Active")
	}
	preflight := SalesforceOrgPreflight{SchemaVersion: 1, BundleSHA256: bundleSHA, OrgAlias: request.TargetOrg, OrgID: orgID, OrgUsername: username, OrgStatus: status, Inventory: SalesforceInventory{Counts: make(map[string]int)}, Commands: []CommandResult{displayReceipt}}
	for index, kind := range salesforceInventoryTypes {
		output, receipt, err := runSalesforcePreflightCommand(runner, authority.Execution, filepath.Dir(request.BundlePath), commands[index+1]...)
		if err != nil {
			return SalesforceOrgPreflight{}, err
		}
		count, err := parseSalesforceCount(output.Stdout)
		if err != nil || count != salesforceInventoryBaselineCount(kind) {
			return SalesforceOrgPreflight{}, fmt.Errorf("scratch org %s inventory does not match the fresh baseline", kind)
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

func runSalesforcePreflightCommand(runner salesforceCommandRunner, execution SalesforceExecutionAuthority, workingDirectory string, args ...string) (salesforceCommandOutput, CommandResult, error) {
	output, receipt, err := runSealedSalesforceCommand(runner, execution, workingDirectory, args...)
	if err != nil || output.ExitCode != 0 || receipt.TimedOut {
		return output, receipt, fmt.Errorf("Salesforce preflight command failed")
	}
	return output, receipt, nil
}

func runSealedSalesforceCommand(runner salesforceCommandRunner, execution SalesforceExecutionAuthority, workingDirectory string, args ...string) (salesforceCommandOutput, CommandResult, error) {
	if !filepath.IsAbs(workingDirectory) || !validSalesforceExecutionAuthority(execution) {
		return salesforceCommandOutput{}, CommandResult{}, fmt.Errorf("invalid Salesforce working directory")
	}
	binary, environment := execution.SFBinary, execution.Environment
	binarySHA256, err := sha256File(binary)
	if err != nil {
		return salesforceCommandOutput{}, CommandResult{}, fmt.Errorf("hash Salesforce CLI before execution: %w", err)
	}
	if binarySHA256 != execution.SFSHA256 {
		return salesforceCommandOutput{}, CommandResult{}, fmt.Errorf("Salesforce CLI does not match sealed execution authority")
	}
	ctx, cancel := context.WithTimeout(context.Background(), salesforceCommandTimeoutForArgs(args))
	defer cancel()
	started := time.Now()
	ctx = context.WithValue(ctx, salesforceExecutionKey{}, salesforceExecution{workingDirectory: workingDirectory, environment: environment})
	output, runErr := runner(ctx, binary, args...)
	afterSHA256, hashErr := sha256File(binary)
	receipt := CommandResult{Command: append([]string{binary}, args...), WorkingDirectory: workingDirectory, Environment: environment, ExecutableSHA256: binarySHA256, ExecutableAfterSHA256: afterSHA256, CommandSpecSHA256: salesforceCommandSpecSHA256(binary, args, workingDirectory, environment, binarySHA256, afterSHA256), ExitCode: output.ExitCode, DurationMS: time.Since(started).Milliseconds(), StdoutSHA256: replayBytesSHA256(output.Stdout), StderrSHA256: replayBytesSHA256(output.Stderr), Output: retainedCommandOutput(output), Passed: runErr == nil && output.ExitCode == 0, TimedOut: ctx.Err() == context.DeadlineExceeded}
	if hashErr != nil || binarySHA256 != afterSHA256 || afterSHA256 != execution.SFSHA256 {
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
			ID              string `json:"id"`
			Status          string `json:"status"`
			ConnectedStatus string `json:"connectedStatus"`
			Username        string `json:"username"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &payload); err != nil || payload.Status != 0 {
		return "", "", "", fmt.Errorf("invalid Salesforce org display JSON")
	}
	status := payload.Result.Status
	if status == "" {
		status = payload.Result.ConnectedStatus
	}
	if payload.Result.ID == "" || status == "" || payload.Result.Username == "" {
		return "", "", "", fmt.Errorf("invalid Salesforce org display JSON")
	}
	return payload.Result.ID, status, payload.Result.Username, nil
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
		Status *int `json:"status"`
		Result *struct {
			TotalSize *int `json:"totalSize"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &payload); err != nil || payload.Status == nil || *payload.Status != 0 || payload.Result == nil || payload.Result.TotalSize == nil || *payload.Result.TotalSize < 0 {
		return 0, fmt.Errorf("invalid Salesforce count JSON")
	}
	return *payload.Result.TotalSize, nil
}

// NormalizeSalesforceFilterResults turns the filter's raw fixture results into
// the only per-surface evidence eligible for final assurance reconciliation.
func NormalizeSalesforceFilterResults(plan OraclePlan, bundle OracleBundle, bundlePath, executorRoot, runID string, preflight, postflight SalesforceOrgPreflight, filter salesforceFilterResults, command CommandResult, shardIndex, shardCount int) (SalesforceShard, error) {
	execution, err := sealedSalesforceExecution(bundlePath)
	if err != nil {
		return SalesforceShard{}, err
	}
	return NormalizeSalesforceFilterResultsAt(plan, bundle, filepath.Dir(bundlePath), execution, filepath.Dir(bundlePath), executorRoot, runID, preflight, postflight, filter, command, shardIndex, shardCount)
}

func NormalizeSalesforceFilterResultsAt(plan OraclePlan, bundle OracleBundle, bundleRoot string, execution SalesforceExecutionAuthority, workingDirectory, executorRoot, runID string, preflight, postflight SalesforceOrgPreflight, filter salesforceFilterResults, command CommandResult, shardIndex, shardCount int) (SalesforceShard, error) {
	expected, err := oracleSalesforceResultKinds(plan)
	if err != nil {
		return SalesforceShard{}, err
	}
	if !validSalesforceOrgPreflightAt(preflight, preflight.BundleSHA256, execution, workingDirectory) || !validSalesforceOrgPreflightAt(postflight, preflight.BundleSHA256, execution, workingDirectory) || preflight.OrgAlias != postflight.OrgAlias || preflight.OrgID != postflight.OrgID || preflight.OrgUsername != postflight.OrgUsername || !filter.Sealed || len(filter.Orgs) != 1 || filter.Orgs[0] != preflight.OrgAlias || !filter.OrgPostflight.MatchesPreflight || !command.Passed || command.ExitCode != 0 || command.TimedOut {
		return SalesforceShard{}, fmt.Errorf("invalid Salesforce filter or org evidence")
	}
	if filter.Binding.ManifestSHA256 != bundle.TransportManifestSHA256 || filter.Binding.ProfileSHA256 != bundle.ProfileSHA256 || filter.Binding.QueueSHA256 != bundle.OraclePlanSHA256 || filter.Binding.SelectorSHA256 != bundle.OraclePlanSHA256 || filter.Binding.SelectorReceiptSHA256 != preflight.BundleSHA256 || filter.Binding.CandidateCommit != bundle.Candidate.Commit || filter.Binding.CandidateSHA256 != bundle.Candidate.SHA256 || filter.Binding.ToolsCommit != bundle.Tools.Commit || filter.Binding.ToolsAMD64SHA256 != bundle.ToolsAMD64SHA256 || filter.Binding.WorkflowScriptSHA256 != bundle.FilterSHA256 || filter.Binding.LocalSummarySHA256 != bundle.LocalProofSummarySHA256 {
		return SalesforceShard{}, fmt.Errorf("Salesforce filter bindings do not match the staged bundle")
	}
	bySurface := make(map[string]salesforceFilterFixtureResult, len(expected))
	for _, result := range filter.Results {
		passed := salesforceFilterFixturePassed(result)
		negativeRuntime := !passed && len(result.SurfaceIDs) == 1 && result.Kind == "exec" && expected[result.SurfaceIDs[0]] == oracleRuntime
		if result.Org != preflight.OrgAlias || result.ExitCode == nil || (*result.ExitCode == 0) != result.Deployable || !result.OrgCleanup.ResidueAbsent || len(result.SurfaceIDs) == 0 || !passed && !negativeRuntime {
			return SalesforceShard{}, fmt.Errorf("invalid Salesforce filter fixture result")
		}
		for _, surfaceID := range result.SurfaceIDs {
			action, exists := expected[surfaceID]
			if !exists || bySurface[surfaceID].SurfaceIDs != nil {
				return SalesforceShard{}, fmt.Errorf("unexpected or duplicate Salesforce surface %q", surfaceID)
			}
			if action == oracleRuntime && (result.RuntimePassed == nil || *result.RuntimePassed != passed || passed && !validSalesforceRuntimeObservation(result.Kind, result.RuntimeResult)) {
				return SalesforceShard{}, fmt.Errorf("runtime surface %q lacks Salesforce runtime proof", surfaceID)
			}
			bySurface[surfaceID] = result
		}
	}
	results := make([]SalesforceSurfaceResult, 0, len(bySurface))
	for _, row := range plan.Rows {
		if action, exists := expected[row.SurfaceID]; exists && bySurface[row.SurfaceID].SurfaceIDs != nil {
			result := bySurface[row.SurfaceID]
			results = append(results, SalesforceSurfaceResult{SurfaceID: row.SurfaceID, Kind: action, Passed: salesforceFilterFixturePassed(result)})
		}
	}
	bundleSHA := preflight.BundleSHA256
	return SalesforceShard{Bindings: SalesforceBindings{OraclePlanSHA256: bundle.OraclePlanSHA256, BundleSHA256: bundleSHA, FilterSHA256: bundle.FilterSHA256, FilterCommandSpecSHA256: command.CommandSpecSHA256}, Candidate: bundle.Candidate, Tools: bundle.Tools, ExecutorRoot: executorRoot, RunID: runID, ShardIndex: shardIndex, ShardCount: shardCount, OrgAlias: preflight.OrgAlias, OrgID: preflight.OrgID, OrgStatus: preflight.OrgStatus, Preflight: preflight, PreInventory: preflight.Inventory, Commands: []CommandResult{command}, Postflight: postflight, PostInventory: postflight.Inventory, Results: results, Cleanup: CleanupReceipt{ResidueAbsent: true}}, nil
}

func salesforceFilterFixturePassed(result salesforceFilterFixtureResult) bool {
	return result.ExitCode != nil && *result.ExitCode == 0 && result.Deployable && (result.RuntimePassed == nil || *result.RuntimePassed)
}

func validSalesforceRuntimeObservation(kind string, raw json.RawMessage) bool {
	if kind == "test" {
		var payload struct {
			Status *int `json:"status"`
			Result struct {
				Summary struct {
					Outcome  string `json:"outcome"`
					TestsRan int    `json:"testsRan"`
					Failing  int    `json:"failing"`
					Passing  int    `json:"passing"`
				} `json:"summary"`
			} `json:"result"`
		}
		return len(raw) > 0 && json.Unmarshal(raw, &payload) == nil && payload.Status != nil && *payload.Status == 0 && payload.Result.Summary.Outcome == "Passed" && payload.Result.Summary.TestsRan > 0 && payload.Result.Summary.Failing == 0 && payload.Result.Summary.Passing > 0
	}
	if kind != "exec" {
		return false
	}
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

func validSalesforceFailureObservation(raw json.RawMessage) bool {
	var payload struct {
		Status *int `json:"status"`
		Result struct {
			Success          *bool  `json:"success"`
			Compiled         *bool  `json:"compiled"`
			CompileProblem   string `json:"compileProblem"`
			ExceptionMessage string `json:"exceptionMessage"`
		} `json:"result"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &payload) != nil || payload.Status == nil || *payload.Status == 0 {
		return false
	}
	return payload.Result.Success != nil && !*payload.Result.Success && payload.Result.Compiled != nil && (payload.Result.CompileProblem != "" || *payload.Result.Compiled && payload.Result.ExceptionMessage != "")
}

func validSalesforceOrgPreflight(preflight SalesforceOrgPreflight, bundleSHA, bundlePath string) bool {
	execution, err := sealedSalesforceExecution(bundlePath)
	if err != nil || !filepath.IsAbs(bundlePath) || preflight.SchemaVersion != 1 || preflight.BundleSHA256 != bundleSHA || preflight.OrgAlias == "" || preflight.OrgID == "" || preflight.OrgUsername == "" || preflight.OrgStatus != "Active" || !baselineSalesforceInventory(preflight.Inventory) || len(preflight.Inventory.Counts) != len(salesforceInventoryTypes) || len(preflight.Commands) != len(salesforceInventoryTypes)+1 {
		return false
	}
	return validSalesforceOrgPreflightAt(preflight, bundleSHA, execution, filepath.Dir(bundlePath))
}

func validSalesforceOrgPreflightAt(preflight SalesforceOrgPreflight, bundleSHA string, execution SalesforceExecutionAuthority, workingDirectory string) bool {
	if !validSalesforceExecutionAuthority(execution) || !sha256Pattern.MatchString(bundleSHA) || preflight.SchemaVersion != 1 || preflight.BundleSHA256 != bundleSHA || preflight.OrgAlias == "" || preflight.OrgID == "" || preflight.OrgUsername == "" || preflight.OrgStatus != "Active" || !baselineSalesforceInventory(preflight.Inventory) || len(preflight.Inventory.Counts) != len(salesforceInventoryTypes) || len(preflight.Commands) != len(salesforceInventoryTypes)+1 {
		return false
	}
	for index, args := range salesforcePreflightArgs(preflight.OrgAlias) {
		command := preflight.Commands[index]
		if !validSalesforceCommandReceiptAt(command, execution, workingDirectory, args) || !command.Passed || command.ExitCode != 0 || command.TimedOut {
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
		args = append(args, []string{"data", "query", "--query", "SELECT count() FROM " + kind, "--target-org", alias, "--use-tooling-api", "--json"})
	}
	return args
}

func salesforceOrgCreateArgs(definition, devHub, alias, marker string) []string {
	return []string{"org", "create", "scratch", "--target-dev-hub", devHub, "--definition-file", definition, "--alias", alias, "--name", marker, "--duration-days", "30", "--json"}
}

func validSalesforceOrgCreation(creation SalesforceOrgCreation, bundleSHA, bundlePath, devHub, alias string) bool {
	execution, err := sealedSalesforceExecution(bundlePath)
	orgID, outputErr := retainedSalesforceOrgCreate(creation.Command)
	return err == nil && outputErr == nil && orgID == creation.OrgID && filepath.IsAbs(bundlePath) && validSalesforceOrgCreationAt(creation, bundleSHA, execution, filepath.Dir(bundlePath), devHub, alias)
}

func validSalesforceOrgCreationAt(creation SalesforceOrgCreation, bundleSHA string, execution SalesforceExecutionAuthority, workingDirectory, devHub, alias string) bool {
	args := salesforceOrgCreateArgs(filepath.Join(workingDirectory, "corpus-assurance-scratch-def.json"), devHub, alias, creation.Marker)
	orgID, outputErr := retainedSalesforceOrgCreate(creation.Command)
	return outputErr == nil && orgID == creation.OrgID && validSalesforceExecutionAuthority(execution) && sha256Pattern.MatchString(bundleSHA) && creation.SchemaVersion == 1 && creation.BundleSHA256 == bundleSHA && creation.DevHub == devHub && creation.Alias == alias && validSalesforceScratchMarker(creation.Marker) && creation.OrgID != "" && validSalesforceCommandReceiptAt(creation.Command, execution, workingDirectory, args) && creation.Command.Passed && creation.Command.ExitCode == 0 && !creation.Command.TimedOut && validSalesforceDevHubCommandAt(creation.DevHubCommand, execution, workingDirectory, devHub, creation.DevHubOrgID, creation.DevHubUsername)
}

func validSalesforceOrgCleanup(cleanup SalesforceOrgCleanup, bundleSHA, bundlePath string, creation SalesforceOrgCreation) bool {
	if cleanup.RecoveredAbsent || cleanup.SchemaVersion != 1 || cleanup.BundleSHA256 != bundleSHA || cleanup.DevHub != creation.DevHub || cleanup.DevHubOrgID != creation.DevHubOrgID || cleanup.DevHubUsername != creation.DevHubUsername || cleanup.OrgAlias != creation.Alias || cleanup.OrgID != creation.OrgID || !cleanup.ResidueAbsent || len(cleanup.Commands) != 2 || !validSalesforceDevHubCommand(cleanup.DevHubCommand, bundlePath, creation.DevHub, creation.DevHubOrgID, creation.DevHubUsername) {
		return false
	}
	execution, err := sealedSalesforceExecution(bundlePath)
	if err != nil {
		return false
	}
	return validSalesforceOrgCleanupAt(cleanup, bundleSHA, execution, filepath.Dir(bundlePath), creation)
}

func validSalesforceOrgCleanupAt(cleanup SalesforceOrgCleanup, bundleSHA string, execution SalesforceExecutionAuthority, workingDirectory string, creation SalesforceOrgCreation) bool {
	if !validSalesforceExecutionAuthority(execution) || !sha256Pattern.MatchString(bundleSHA) || cleanup.RecoveredAbsent || cleanup.SchemaVersion != 1 || cleanup.BundleSHA256 != bundleSHA || cleanup.DevHub != creation.DevHub || cleanup.DevHubOrgID != creation.DevHubOrgID || cleanup.DevHubUsername != creation.DevHubUsername || cleanup.OrgAlias != creation.Alias || cleanup.OrgID != creation.OrgID || !cleanup.ResidueAbsent || len(cleanup.Commands) != 2 || !validSalesforceDevHubCommandAt(cleanup.DevHubCommand, execution, workingDirectory, creation.DevHub, creation.DevHubOrgID, creation.DevHubUsername) {
		return false
	}
	expected := []struct {
		args   []string
		passed bool
	}{
		{[]string{"org", "delete", "scratch", "--target-org", creation.Alias, "--no-prompt", "--json"}, true},
		{[]string{"org", "display", "--target-org", creation.Alias, "--json"}, false},
	}
	for index, want := range expected {
		command := cleanup.Commands[index]
		if !validSalesforceCommandReceiptAt(command, execution, workingDirectory, want.args) || command.Passed != want.passed || (command.ExitCode == 0) != want.passed || command.TimedOut {
			return false
		}
		if index == 1 && validSalesforceOrgDisplayFailure(command.Output.Stdout) == false {
			return false
		}
	}
	return true
}

func validSalesforceRecoveredOrgCleanup(cleanup SalesforceOrgCleanup, bundleSHA, bundlePath string, creation SalesforceOrgCreation) bool {
	if !cleanup.RecoveredAbsent || cleanup.SchemaVersion != 1 || cleanup.BundleSHA256 != bundleSHA || cleanup.DevHub != creation.DevHub || cleanup.DevHubOrgID != creation.DevHubOrgID || cleanup.DevHubUsername != creation.DevHubUsername || cleanup.OrgAlias != creation.Alias || cleanup.OrgID != creation.OrgID || !cleanup.ResidueAbsent || (len(cleanup.Commands) != 2 && len(cleanup.Commands) != 4) || !validSalesforceDevHubCommand(cleanup.DevHubCommand, bundlePath, creation.DevHub, creation.DevHubOrgID, creation.DevHubUsername) {
		return false
	}
	execution, err := sealedSalesforceExecution(bundlePath)
	if err != nil {
		return false
	}
	delete := cleanup.Commands[0]
	if !validSalesforceCommandReceipt(delete, execution, bundlePath, []string{"org", "delete", "scratch", "--target-org", creation.Alias, "--no-prompt", "--json"}) || delete.Passed {
		return false
	}
	query := cleanup.Commands[1]
	queryArgs := []string{"data", "query", "--target-org", creation.DevHub, "--query", salesforceActiveScratchOrgQuery(creation.OrgID), "--json"}
	if !validSalesforceCommandReceipt(query, execution, bundlePath, queryArgs) || !query.Passed || query.ExitCode != 0 || query.TimedOut {
		return false
	}
	activeRecords, err := parseSalesforceActiveScratchOrg(query.Output.Stdout)
	if err != nil || len(activeRecords) > 1 {
		return false
	}
	if len(activeRecords) == 0 {
		return len(cleanup.Commands) == 2
	}
	active := activeRecords[0]
	if len(cleanup.Commands) != 4 || active.ScratchOrg != creation.OrgID || !validSalesforceRecordID(active.ID, "2AS") {
		return false
	}
	deleted := cleanup.Commands[2]
	if !validSalesforceCommandReceipt(deleted, execution, bundlePath, []string{"data", "delete", "record", "--target-org", creation.DevHub, "--sobject", "ActiveScratchOrg", "--record-id", active.ID, "--json"}) || !deleted.Passed || deleted.ExitCode != 0 || deleted.TimedOut {
		return false
	}
	verified := cleanup.Commands[3]
	if !validSalesforceCommandReceipt(verified, execution, bundlePath, queryArgs) || !verified.Passed || verified.ExitCode != 0 || verified.TimedOut {
		return false
	}
	remaining, err := parseSalesforceActiveScratchOrg(verified.Output.Stdout)
	return err == nil && len(remaining) == 0
}

func validSalesforceDevHubCommand(command CommandResult, bundlePath, alias, orgID, username string) bool {
	execution, err := sealedSalesforceExecution(bundlePath)
	if err != nil {
		return false
	}
	return validSalesforceDevHubCommandAt(command, execution, filepath.Dir(bundlePath), alias, orgID, username)
}

func validSalesforceDevHubCommandAt(command CommandResult, execution SalesforceExecutionAuthority, workingDirectory, alias, orgID, username string) bool {
	args := []string{"org", "display", "--target-org", alias, "--json"}
	if !validSalesforceCommandReceiptAt(command, execution, workingDirectory, args) || !command.Passed || command.ExitCode != 0 || command.TimedOut {
		return false
	}
	observedID, _, observedUsername, err := parseSalesforceOrgDisplay(command.Output.Stdout)
	return err == nil && observedID == orgID && observedUsername == username
}

func readSealedSalesforceDevHubAuthority(bundlePath string) (SalesforceDevHubAuthority, error) {
	bundle, _, err := readExactJSONBytes[OracleBundle](bundlePath)
	if err != nil || !sha256Pattern.MatchString(bundle.DevHubAuthoritySHA256) {
		return SalesforceDevHubAuthority{}, fmt.Errorf("sealed Salesforce Dev Hub authority is unavailable")
	}
	path := filepath.Join(filepath.Dir(bundlePath), "DEV_HUB_AUTHORITY.json")
	authority, bytes, err := readExactJSONBytes[SalesforceDevHubAuthority](path)
	if err != nil || !validSalesforceDevHubAuthority(authority) || replayBytesSHA256(bytes) != bundle.DevHubAuthoritySHA256 || authority.Alias != bundle.DevHub || authority.OrgID != bundle.DevHubOrgID || authority.Username != bundle.DevHubUsername || !reflect.DeepEqual(authority.Execution, bundle.SalesforceExecution) {
		return SalesforceDevHubAuthority{}, fmt.Errorf("sealed Salesforce Dev Hub authority is invalid")
	}
	return authority, nil
}

func sealedSalesforceExecution(bundlePath string) (SalesforceExecutionAuthority, error) {
	authority, err := readSealedSalesforceDevHubAuthority(bundlePath)
	return authority.Execution, err
}

func validSalesforceCommandReceipt(command CommandResult, execution SalesforceExecutionAuthority, bundlePath string, args []string) bool {
	return validSalesforceCommandReceiptAt(command, execution, filepath.Dir(bundlePath), args)
}

func validSalesforceCommandReceiptAt(command CommandResult, execution SalesforceExecutionAuthority, workingDirectory string, args []string) bool {
	return validRetainedCommandOutput(command) && cleanAbsolutePath(workingDirectory) && equalStrings(command.Command, append([]string{execution.SFBinary}, args...)) && command.WorkingDirectory == workingDirectory && reflect.DeepEqual(command.Environment, execution.Environment) && command.ExecutableSHA256 == execution.SFSHA256 && command.ExecutableAfterSHA256 == execution.SFSHA256 && command.CommandSpecSHA256 == salesforceCommandSpecSHA256(execution.SFBinary, args, workingDirectory, execution.Environment, execution.SFSHA256, execution.SFSHA256) && sha256Pattern.MatchString(command.StdoutSHA256) && sha256Pattern.MatchString(command.StderrSHA256)
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
		Status      int    `json:"status"`
		ExitCode    *int   `json:"exitCode"`
		Name        string `json:"name"`
		Code        string `json:"code"`
		Context     string `json:"context"`
		CommandName string `json:"commandName"`
		Message     string `json:"message"`
	}
	if json.Unmarshal(data, &payload) != nil {
		return false
	}
	message := strings.ToLower(strings.TrimSpace(payload.Message))
	missing := strings.Contains(message, "not found") || strings.Contains(message, "no authorization information found") || strings.Contains(message, "does not exist")
	legacy := payload.Status == 1 && missing
	current := payload.Status == 2 && payload.ExitCode != nil && *payload.ExitCode == 2 && payload.Name == "NamedOrgNotFoundError" && payload.Code == payload.Name && payload.Context == "OrgDisplayCommand" && payload.CommandName == payload.Context && strings.Contains(message, "no authorization information found")
	return legacy || current
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
	}{binary, args, environment, workingDirectory, executableSHA256, executableAfterSHA256, salesforceCommandTimeoutForArgs(args).Nanoseconds()})
	return replayBytesSHA256(data)
}

func salesforceFilterArgs(filterPath, bundleRoot, executorRoot, runID, orgAlias string, bundle OracleBundle, bundleSHA string, shardIndex, shardCount int) ([]string, error) {
	if !filepath.IsAbs(filterPath) || !filepath.IsAbs(bundleRoot) || !filepath.IsAbs(executorRoot) || !strings.Contains(filepath.ToSlash(executorRoot), "/executor/") || runID == "" || orgAlias == "" || !sha256Pattern.MatchString(bundleSHA) || ValidateRuntimeArtifact(bundle.Candidate) != nil || ValidateRuntimeArtifact(bundle.Tools) != nil || ValidateRuntimeArtifact(bundle.ToolsAMD64) != nil || bundle.ToolsAMD64.SHA256 != bundle.ToolsAMD64SHA256 || bundle.ToolsAMD64.Commit != bundle.Tools.Commit || !sha256Pattern.MatchString(bundle.OraclePlanSHA256) || !sha256Pattern.MatchString(bundle.TransportManifestSHA256) || !sha256Pattern.MatchString(bundle.LocalProofSummarySHA256) || !validSalesforceExecutionAuthority(bundle.SalesforceExecution) || len(bundle.Fixtures) == 0 || shardCount < 1 || shardIndex < 0 || shardIndex >= shardCount {
		return nil, fmt.Errorf("invalid sealed Salesforce filter inputs")
	}
	return []string{filterPath,
		"--profile", filepath.Join(bundleRoot, "profile.json"),
		"--fixtures", filepath.Join(bundleRoot, "fixtures"),
		"--manifest", filepath.Join(bundleRoot, "fixture-manifest.json"),
		"--root", bundleRoot,
		"--out", filepath.Join(executorRoot, "filter"),
		"--limit", strconv.Itoa(len(bundle.Fixtures)),
		"--orgs", orgAlias,
		"--sf-bin", bundle.SalesforceExecution.SFBinary,
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
	manifestSnapshot, err := readRegularFileSnapshot(manifestPath)
	if err != nil {
		return salesforceExecutorSnapshot{}, fmt.Errorf("invalid Salesforce executor manifest")
	}
	manifestBytes := manifestSnapshot.Data
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
	snapshots, entries, err := readSalesforceExecutorFileSnapshots(root)
	if err != nil || !reflect.DeepEqual(entries, manifest.Files) {
		return salesforceExecutorSnapshot{}, fmt.Errorf("Salesforce executor artifacts do not match manifest")
	}
	files := make(map[string][]byte, len(snapshots))
	for path, snapshot := range snapshots {
		files[path] = append([]byte(nil), snapshot.Data...)
	}
	return salesforceExecutorSnapshot{ManifestSHA256: replayBytesSHA256(manifestBytes), Manifest: manifestSnapshot, Files: files, Snapshots: snapshots}, nil
}

func readSalesforceExecutorFiles(root string) (map[string][]byte, []salesforceExecutorFile, error) {
	snapshots, entries, err := readSalesforceExecutorFileSnapshots(root)
	files := make(map[string][]byte, len(snapshots))
	for path, snapshot := range snapshots {
		files[path] = append([]byte(nil), snapshot.Data...)
	}
	return files, entries, err
}

func readSalesforceExecutorFileSnapshots(root string) (map[string]reportInputSnapshot, []salesforceExecutorFile, error) {
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, nil, fmt.Errorf("Salesforce executor root is not a physical directory")
	}
	files := map[string]reportInputSnapshot{}
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
		snapshot, err := readRegularFileSnapshot(path)
		if err != nil {
			return err
		}
		files[relative] = snapshot
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	entries := make([]salesforceExecutorFile, 0, len(files))
	for path, snapshot := range files {
		entries = append(entries, salesforceExecutorFile{Path: path, SHA256: replayBytesSHA256(snapshot.Data)})
	}
	sort.Slice(entries, func(left, right int) bool { return entries[left].Path < entries[right].Path })
	return files, entries, nil
}

func validSalesforceDispatch(dispatch SalesforceDispatch, bundle OracleBundle, bundlePath string) bool {
	filterSourcePath := filepath.Join(filepath.Dir(filepath.Dir(bundlePath)), "transport", "salesforce-first-filter.py")
	filterSource, sourceErr := os.ReadFile(filterSourcePath)
	execution := bundle.SalesforceExecution
	executorRoot, runID, identityErr := sealedSalesforceDispatchLayout(bundlePath, bundle.AttemptSHA256, dispatch.ShardIndex, dispatch.ShardCount)
	return sourceErr == nil && identityErr == nil && validSalesforceDispatchAt(dispatch, bundle, filepath.Dir(bundlePath), execution, filepath.Dir(bundlePath), executorRoot, runID, filterSource)
}

func validSalesforceDispatchAt(dispatch SalesforceDispatch, bundle OracleBundle, bundleRoot string, execution SalesforceExecutionAuthority, workingDirectory, executorRoot, runID string, filterSource []byte) bool {
	if dispatch.SchemaVersion != 1 || dispatch.BundleSHA256 == "" || dispatch.OrgAlias == "" || dispatch.ExecutorRoot == "" || dispatch.RunID == "" {
		return false
	}
	if validateApprovedOracleBundleFilter(bundle) != nil {
		return false
	}
	filterPath := sealedSalesforceFilterScriptPath(executorRoot)
	filterArgs, err := salesforceFilterArgs(filterPath, workingDirectory, executorRoot, runID, dispatch.OrgAlias, bundle, dispatch.BundleSHA256, dispatch.ShardIndex, dispatch.ShardCount)
	args, invocationErr := sealedSalesforceFilterInvocationArgs(filterPath, filterSource, filterArgs)
	return cleanAbsolutePath(bundleRoot) && cleanAbsolutePath(executorRoot) && cleanAbsolutePath(workingDirectory) && dispatch.ExecutorRoot == executorRoot && replayBytesSHA256(filterSource) == bundle.FilterSHA256 && dispatch.RunID == runID && err == nil && invocationErr == nil && validSalesforceExecutionAuthority(execution) && dispatch.PythonSHA256 == execution.PythonSHA256 && dispatch.FilterCommandSpecSHA256 == salesforceFilterCommandSpecSHA256(execution.PythonBinary, args, workingDirectory, execution.Environment, execution.PythonSHA256, execution.PythonSHA256)
}

// ValidateSalesforceShardFiles derives the runtime and compile denominator
// from the sealed oracle plan, then validates every raw shard against it.
// Callers cannot choose a smaller expected set.
func ValidateSalesforceShardFiles(planPath string, shardFiles []SalesforceShardFiles) error {
	return validateSalesforceShardFiles(planPath, shardFiles, nil)
}

type salesforceShardValidationScope struct {
	ExpectedSurfaceIDs []string
	LogicalShardCount  int
}

func validateSalesforceShardFiles(planPath string, shardFiles []SalesforceShardFiles, snapshots *[]salesforceShardEvidenceSnapshot, trustedScope ...salesforceShardValidationScope) error {
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
	logicalShardCount := len(shardFiles)
	if len(trustedScope) > 1 {
		return fmt.Errorf("invalid trusted Salesforce shard scope")
	}
	if len(trustedScope) == 1 {
		expected = append([]string(nil), trustedScope[0].ExpectedSurfaceIDs...)
		logicalShardCount = trustedScope[0].LogicalShardCount
		seen := map[string]bool{}
		for _, surfaceID := range expected {
			if surfaceID == "" || seen[surfaceID] || expectedKinds[surfaceID] == "" {
				return fmt.Errorf("invalid trusted Salesforce surface %q", surfaceID)
			}
			seen[surfaceID] = true
		}
		if len(expected) == 0 || logicalShardCount < 1 {
			return fmt.Errorf("trusted Salesforce shard scope is empty")
		}
	}
	planSHA := replayBytesSHA256(planBytes)
	bundlePath := filepath.Join(filepath.Dir(planPath), "bundle.json")
	if err := ValidateOracleBundle(bundlePath); err != nil {
		return fmt.Errorf("validate staged Oracle bundle: %w", err)
	}
	bundle, bundleBytes, err := readExactJSONBytes[OracleBundle](bundlePath)
	if err != nil || (bundle.SchemaVersion != 1 && bundle.SchemaVersion != 2) || bundle.OraclePlanSHA256 != planSHA || bundle.Candidate != plan.Candidate || bundle.Tools != plan.Tools {
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
		workingDirectory := filepath.Dir(bundlePath)
		if creation.Command.WorkingDirectory != workingDirectory {
			return fmt.Errorf("Salesforce lifecycle working directory does not match staged bundle")
		}
		layoutBundlePath, layoutErr := filepath.EvalSymlinks(bundlePath)
		if layoutErr != nil {
			return fmt.Errorf("resolve Salesforce dispatch layout: %w", layoutErr)
		}
		snapshot, snapshotErr := readSealedSalesforceExecutor(shard.ExecutorRoot)
		if snapshotErr != nil {
			return fmt.Errorf("read Salesforce executor: %w", snapshotErr)
		}
		filterPath := sealedSalesforceFilterScriptPath(shard.ExecutorRoot)
		filterResultsPath := filepath.Join(sealedSalesforceFilterOutputPath(shard.ExecutorRoot), "results.json")
		postflightPath := filepath.Join(shard.ExecutorRoot, "postflight.json")
		var postflight SalesforceOrgPreflight
		if err := json.Unmarshal(snapshot.Files["postflight.json"], &postflight); err != nil {
			return fmt.Errorf("read Salesforce postflight: %w", err)
		}
		inputs := map[string]string{"dispatch": replayBytesSHA256(dispatchBytes), "preflight": replayBytesSHA256(preflightBytes), "creation": replayBytesSHA256(creationBytes), "cleanup": replayBytesSHA256(cleanupBytes)}
		if err := validateSalesforceShardSemanticsAt(plan, bundle, filepath.Dir(bundlePath), bundleSHA, bundle.SalesforceExecution, workingDirectory, layoutBundlePath, shard, dispatch, creation, cleanup, preflight, postflight, snapshot, inputs); err != nil {
			return err
		}
		filterBytes, filterExists := snapshot.Files["filter/results.json"]
		filterSource, filterScriptExists := snapshot.Files[filepath.ToSlash(filepath.Join("filter-script", "salesforce-first-filter.py"))]
		postflightBytes, postflightExists := snapshot.Files["postflight.json"]
		filterResultsSHA, executedFilterSHA := replayBytesSHA256(filterBytes), replayBytesSHA256(filterSource)
		if shard.Bindings.OraclePlanSHA256 != planSHA || shard.Bindings.BundleSHA256 != bundleSHA || shard.Candidate != plan.Candidate || shard.Tools != plan.Tools || shard.DispatchSHA256 != replayBytesSHA256(dispatchBytes) || shard.PreflightSHA256 != replayBytesSHA256(preflightBytes) || shard.PostflightSHA256 != replayBytesSHA256(postflightBytes) || shard.FilterResultsSHA256 != filterResultsSHA || shard.ExecutedFilterSHA256 != executedFilterSHA || shard.ExecutorManifestSHA256 != snapshot.ManifestSHA256 || shard.ExecutedFilterSHA256 != bundle.FilterSHA256 || !reflect.DeepEqual(preflight, shard.Preflight) || !filterExists || !filterScriptExists || !postflightExists || !reflect.DeepEqual(postflight, shard.Postflight) || dispatch.BundleSHA256 != bundleSHA || dispatch.OrgAlias != shard.OrgAlias || dispatch.ShardIndex != shard.ShardIndex || dispatch.ShardCount != shard.ShardCount || dispatch.ExecutorRoot != shard.ExecutorRoot || dispatch.RunID != shard.RunID || dispatch.FilterCommandSpecSHA256 != shard.Bindings.FilterCommandSpecSHA256 {
			return fmt.Errorf("Salesforce shard does not bind sealed oracle plan")
		}
		shards = append(shards, shard)
		validatedSnapshots = append(validatedSnapshots, salesforceShardEvidenceSnapshot{Shard: shard, Dispatch: dispatch, Creation: creation, Cleanup: cleanup, Inputs: map[string]string{"shard": replayBytesSHA256(shardBytes), "dispatch": replayBytesSHA256(dispatchBytes), "preflight": replayBytesSHA256(preflightBytes), "creation": replayBytesSHA256(creationBytes), "cleanup": replayBytesSHA256(cleanupBytes)}, Executor: snapshot})
		files = append(files, []string{evidence.ShardPath, evidence.DispatchPath, evidence.PreflightPath, filterPath, filterResultsPath, postflightPath, evidence.CreationPath, evidence.CleanupPath})
		fileHashes = append(fileHashes, []string{replayBytesSHA256(shardBytes), replayBytesSHA256(dispatchBytes), replayBytesSHA256(preflightBytes), executedFilterSHA, replayBytesSHA256(filterBytes), replayBytesSHA256(postflightBytes), replayBytesSHA256(creationBytes), replayBytesSHA256(cleanupBytes)})
		executorRoots = append(executorRoots, shard.ExecutorRoot)
		executorSnapshots = append(executorSnapshots, snapshot)
	}
	if err := validateSalesforceShards(shards, expected, logicalShardCount); err != nil {
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
		if err != nil || !reflect.DeepEqual(after, executorSnapshots[index]) {
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

func validateSalesforceShardSemanticsAt(plan OraclePlan, bundle OracleBundle, bundleRoot, bundleSHA string, execution SalesforceExecutionAuthority, workingDirectory, layoutBundlePath string, shard SalesforceShard, dispatch SalesforceDispatch, creation SalesforceOrgCreation, cleanup SalesforceOrgCleanup, preflight, postflight SalesforceOrgPreflight, snapshot salesforceExecutorSnapshot, inputs map[string]string) error {
	if !cleanAbsolutePath(bundleRoot) || !validSalesforceExecutionAuthority(execution) || !cleanAbsolutePath(workingDirectory) || len(preflight.Commands) == 0 || len(postflight.Commands) == 0 || len(shard.Commands) == 0 {
		return fmt.Errorf("incomplete Salesforce lifecycle evidence")
	}
	expectedExecutorRoot, expectedRunID, layoutErr := salesforceDispatchLayoutAt(layoutBundlePath, bundle.AttemptSHA256, shard.ShardIndex, shard.ShardCount)
	if layoutErr != nil || shard.ExecutorRoot != expectedExecutorRoot || shard.RunID != expectedRunID || dispatch.ExecutorRoot != expectedExecutorRoot || dispatch.RunID != expectedRunID {
		return fmt.Errorf("Salesforce dispatch does not use the sealed attempt layout")
	}
	filterBytes, filterExists := snapshot.Files["filter/results.json"]
	filterSource, filterScriptExists := snapshot.Files["filter-script/salesforce-first-filter.py"]
	postflightBytes, postflightExists := snapshot.Files["postflight.json"]
	if !filterExists || !filterScriptExists || !postflightExists {
		return fmt.Errorf("incomplete Salesforce lifecycle evidence")
	}
	bundlePath := filepath.Join(bundleRoot, "bundle.json")
	filter, err := deriveSalesforceFilterEvidenceWithCLI(bundle, bundlePath, preflight.OrgAlias, preflight.OrgID, preflight.OrgUsername, preflight.Commands[0].ExecutableSHA256, postflight.Commands[0].ExecutableAfterSHA256, expectedExecutorRoot, expectedRunID, shard.ShardIndex, snapshot)
	if err != nil {
		return err
	}
	filter.Binding.SelectorReceiptSHA256 = preflight.BundleSHA256
	if shard.Bindings.OraclePlanSHA256 != bundle.OraclePlanSHA256 || shard.Bindings.BundleSHA256 != bundleSHA || shard.Candidate != bundle.Candidate || shard.Tools != bundle.Tools || dispatch.BundleSHA256 != bundleSHA || dispatch.OrgAlias != shard.OrgAlias || dispatch.ShardIndex != shard.ShardIndex || dispatch.ShardCount != shard.ShardCount || dispatch.ExecutorRoot != expectedExecutorRoot || dispatch.RunID != expectedRunID || dispatch.FilterCommandSpecSHA256 != shard.Bindings.FilterCommandSpecSHA256 || shard.ExecutedFilterSHA256 != bundle.FilterSHA256 || !validSalesforceDispatchAt(dispatch, bundle, bundleRoot, execution, workingDirectory, expectedExecutorRoot, expectedRunID, filterSource) || !validSalesforceOrgPreflightAt(preflight, bundleSHA, execution, workingDirectory) || !validSalesforceOrgPreflightAt(postflight, bundleSHA, execution, workingDirectory) || preflight.OrgAlias != postflight.OrgAlias || preflight.OrgID != postflight.OrgID || preflight.OrgUsername != postflight.OrgUsername || !validSealedFilterCommandAt(shard, bundle, bundleRoot, execution, workingDirectory, expectedExecutorRoot, filterSource) || creation.Invalidated || creation.DevHub != bundle.DevHub || creation.DevHubOrgID != bundle.DevHubOrgID || creation.DevHubUsername != bundle.DevHubUsername || creation.OrgID != shard.OrgID || !validSalesforceOrgCreationAt(creation, bundleSHA, execution, workingDirectory, bundle.DevHub, shard.OrgAlias) || cleanup.DevHub != bundle.DevHub || cleanup.DevHubOrgID != bundle.DevHubOrgID || cleanup.DevHubUsername != bundle.DevHubUsername || cleanup.OrgAlias != shard.OrgAlias || cleanup.OrgID != shard.OrgID || !validSalesforceOrgCleanupAt(cleanup, bundleSHA, execution, workingDirectory, creation) {
		return fmt.Errorf("Salesforce shard lifecycle semantics do not validate")
	}
	rebuilt, err := NormalizeSalesforceFilterResultsAt(plan, bundle, bundleRoot, execution, workingDirectory, expectedExecutorRoot, expectedRunID, preflight, postflight, filter, shard.Commands[0], shard.ShardIndex, shard.ShardCount)
	if err != nil {
		return err
	}
	rebuilt.DispatchSHA256 = inputs["dispatch"]
	rebuilt.PreflightSHA256 = inputs["preflight"]
	rebuilt.PostflightSHA256 = replayBytesSHA256(postflightBytes)
	rebuilt.FilterResultsSHA256 = replayBytesSHA256(filterBytes)
	rebuilt.ExecutedFilterSHA256 = replayBytesSHA256(filterSource)
	rebuilt.ExecutorManifestSHA256 = snapshot.ManifestSHA256
	if !reflect.DeepEqual(rebuilt, shard) {
		return fmt.Errorf("Salesforce shard normalization drift")
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
	return validateSalesforceShards(shards, expected, len(shards))
}

func validateSalesforceShards(shards []SalesforceShard, expected []string, logicalShardCount int) error {
	if len(shards) == 0 || len(expected) == 0 {
		return fmt.Errorf("Salesforce shards and expected surfaces are required")
	}
	if logicalShardCount < 1 {
		return fmt.Errorf("invalid logical Salesforce shard count")
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
		if ValidateRuntimeArtifact(shard.Candidate) != nil || ValidateRuntimeArtifact(shard.Tools) != nil || !validSalesforceBindings(shard.Bindings) || shard.Candidate != first.Candidate || shard.Tools != first.Tools || !sameSalesforceBundleBindings(shard.Bindings, first.Bindings) || shard.ShardCount != logicalShardCount || shard.ShardIndex < 0 || shard.ShardIndex >= shard.ShardCount || indexes[shard.ShardIndex] || shard.OrgAlias == "" || aliases[shard.OrgAlias] || shard.OrgID == "" || orgs[shard.OrgID] || shard.OrgStatus != "Active" || !validShardLifecycle(shard) || !validSalesforceCommands(shard.Commands, shard.Bindings.FilterCommandSpecSHA256) || !baselineSalesforceInventory(shard.PreInventory) || !sameInventory(shard.PreInventory, shard.PostInventory) || !shard.Cleanup.ResidueAbsent {
			return fmt.Errorf("invalid Salesforce shard %d", shard.ShardIndex)
		}
		indexes[shard.ShardIndex], aliases[shard.OrgAlias], orgs[shard.OrgID] = true, true, true
		for _, result := range shard.Results {
			if result.SurfaceID == "" || results[result.SurfaceID] || !expectedSet[result.SurfaceID] || (result.Kind != oracleRuntime && result.Kind != oracleCompile) {
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
		if receipt.SchemaVersion != 1 || receipt.BundleSHA256 != shard.Bindings.BundleSHA256 || receipt.OrgAlias != shard.OrgAlias || receipt.OrgID != shard.OrgID || receipt.OrgStatus != shard.OrgStatus || len(receipt.Commands) != len(salesforceInventoryTypes)+1 || !baselineSalesforceInventory(receipt.Inventory) {
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
	return sourceErr == nil && validSealedFilterCommandAt(shard, bundle, filepath.Dir(bundlePath), bundle.SalesforceExecution, filepath.Dir(bundlePath), shard.ExecutorRoot, filterSource)
}

func validSealedFilterCommandAt(shard SalesforceShard, bundle OracleBundle, bundleRoot string, execution SalesforceExecutionAuthority, workingDirectory, executorRoot string, filterSource []byte) bool {
	if len(shard.Commands) != 1 || shard.ExecutorRoot == "" || shard.RunID == "" {
		return false
	}
	if validateApprovedOracleBundleFilter(bundle) != nil {
		return false
	}
	filterPath := sealedSalesforceFilterScriptPath(executorRoot)
	filterArgs, err := salesforceFilterArgs(filterPath, workingDirectory, executorRoot, shard.RunID, shard.OrgAlias, bundle, shard.Bindings.BundleSHA256, shard.ShardIndex, shard.ShardCount)
	args, invocationErr := sealedSalesforceFilterInvocationArgs(filterPath, filterSource, filterArgs)
	command := shard.Commands[0]
	return cleanAbsolutePath(bundleRoot) && err == nil && replayBytesSHA256(filterSource) == bundle.FilterSHA256 && invocationErr == nil && validSalesforceExecutionAuthority(execution) && validRetainedCommandOutput(command) && equalStrings(command.Command, append([]string{execution.PythonBinary}, args...)) && command.WorkingDirectory == workingDirectory && reflect.DeepEqual(command.Environment, execution.Environment) && command.ExecutableSHA256 == execution.PythonSHA256 && command.ExecutableAfterSHA256 == execution.PythonSHA256 && command.CommandSpecSHA256 == salesforceFilterCommandSpecSHA256(execution.PythonBinary, args, workingDirectory, execution.Environment, execution.PythonSHA256, execution.PythonSHA256) && command.CommandSpecSHA256 == shard.Bindings.FilterCommandSpecSHA256 && command.ExitCode == 0 && command.Passed && !command.TimedOut && sha256Pattern.MatchString(command.StdoutSHA256) && sha256Pattern.MatchString(command.StderrSHA256)
}

func salesforceInventoryBaselineCount(kind string) int {
	if kind == "FieldSet" {
		return 1
	}
	return 0
}

func baselineSalesforceInventory(inventory SalesforceInventory) bool {
	if len(inventory.Counts) != len(salesforceInventoryTypes) {
		return false
	}
	for _, kind := range salesforceInventoryTypes {
		count, ok := inventory.Counts[kind]
		if !ok || count != salesforceInventoryBaselineCount(kind) {
			return false
		}
	}
	return true
}

func sameInventory(one, two SalesforceInventory) bool {
	if len(one.Counts) != len(two.Counts) {
		return false
	}
	for kind, count := range one.Counts {
		other, ok := two.Counts[kind]
		if !ok || count != other {
			return false
		}
	}
	return true
}
