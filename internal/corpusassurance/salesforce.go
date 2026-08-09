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
	WorkflowScriptSHA256  string `json:"workflowScriptSha256"`
	LocalSummarySHA256    string `json:"localSummarySha256"`
}

type salesforceFilterFixtureResult struct {
	SurfaceIDs    []string       `json:"surfaceIds"`
	Org           string         `json:"org"`
	Kind          string         `json:"kind"`
	ExitCode      *int           `json:"exitCode"`
	Deployable    bool           `json:"deployable"`
	RuntimePassed *bool          `json:"runtimePassed"`
	RemoteCleanup CleanupReceipt `json:"remoteCleanup"`
	OrgCleanup    CleanupReceipt `json:"orgCleanup"`
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
	Bindings      SalesforceBindings        `json:"bindings"`
	Candidate     RuntimeArtifact           `json:"candidate"`
	Tools         RuntimeArtifact           `json:"tools"`
	ShardIndex    int                       `json:"shardIndex"`
	ShardCount    int                       `json:"shardCount"`
	OrgAlias      string                    `json:"orgAlias"`
	OrgID         string                    `json:"orgId"`
	OrgStatus     string                    `json:"orgStatus"`
	PreInventory  SalesforceInventory       `json:"preInventory"`
	Commands      []CommandResult           `json:"commands"`
	PostInventory SalesforceInventory       `json:"postInventory"`
	Results       []SalesforceSurfaceResult `json:"results"`
	Cleanup       CleanupReceipt            `json:"cleanup"`
}

const salesforceCommandTimeout = 30 * time.Second
const salesforceFilterTimeout = 15 * time.Minute

type SalesforceShardRequest struct {
	BundlePath     string
	PreflightPath  string
	TargetOrg      string
	SFBin          string
	ExecutorRoot   string
	RunID          string
	ShardIndex     int
	ShardCount     int
	OutputPath     string
	validateBundle func(string) error
	filterRunner   salesforceCommandRunner
	sfRunner       salesforceCommandRunner
}

// RunSalesforceShard executes one sealed filter partition, obtains a fresh
// eight-type postflight receipt, and writes the normalized shard only after
// the staged bundle still validates.
func RunSalesforceShard(request SalesforceShardRequest) (SalesforceShard, error) {
	if !filepath.IsAbs(request.BundlePath) || !filepath.IsAbs(request.PreflightPath) || !filepath.IsAbs(request.ExecutorRoot) || !filepath.IsAbs(request.OutputPath) || !strings.Contains(filepath.ToSlash(request.ExecutorRoot), "/executor/") || request.TargetOrg == "" || request.SFBin != "/usr/local/bin/sf" || request.RunID == "" || request.ShardCount != 2 || request.ShardIndex < 0 || request.ShardIndex >= request.ShardCount {
		return SalesforceShard{}, fmt.Errorf("invalid Salesforce shard request")
	}
	if _, err := os.Lstat(request.OutputPath); err == nil {
		return SalesforceShard{}, fmt.Errorf("Salesforce shard output already exists: %s", request.OutputPath)
	} else if !os.IsNotExist(err) {
		return SalesforceShard{}, err
	}
	filterOutput := filepath.Join(request.ExecutorRoot, "filter")
	if _, err := os.Lstat(filterOutput); err == nil {
		return SalesforceShard{}, fmt.Errorf("Salesforce filter output already exists: %s", filterOutput)
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
	preflight, preflightBytes, err := readExactJSONBytes[SalesforceOrgPreflight](request.PreflightPath)
	if err != nil || !validSalesforceOrgPreflight(preflight, bundleSHA, request.BundlePath) || preflight.OrgAlias != request.TargetOrg {
		return SalesforceShard{}, fmt.Errorf("invalid sealed Salesforce preflight")
	}
	filterPath := filepath.Join(filepath.Dir(filepath.Dir(request.BundlePath)), "transport", "salesforce-first-filter.py")
	args, err := salesforceFilterArgs(filterPath, filepath.Dir(request.BundlePath), request.ExecutorRoot, request.RunID, request.TargetOrg, bundle, bundleSHA, request.ShardIndex, request.ShardCount)
	if err != nil {
		return SalesforceShard{}, err
	}
	filterRunner := request.filterRunner
	if filterRunner == nil {
		filterRunner = runSalesforceCLI
	}
	_, command, err := runSalesforceFilterCommand(filterRunner, "python3", args...)
	if err != nil {
		return SalesforceShard{}, err
	}
	filterPathResult := filepath.Join(filterOutput, "results.json")
	filter, filterBytes, err := readSalesforceFilterResults(filterPathResult)
	if err != nil {
		return SalesforceShard{}, err
	}
	postflightPath := filepath.Join(request.ExecutorRoot, "postflight.json")
	postflight, err := RunSalesforceOrgPreflight(SalesforceOrgPreflightRequest{BundlePath: request.BundlePath, TargetOrg: request.TargetOrg, SFBin: request.SFBin, OutputPath: postflightPath, validateBundle: validate, runner: request.sfRunner})
	if err != nil {
		return SalesforceShard{}, fmt.Errorf("Salesforce postflight: %w", err)
	}
	if err := validate(request.BundlePath); err != nil {
		return SalesforceShard{}, fmt.Errorf("staged bundle changed during Salesforce execution: %w", err)
	}
	postflightRead, postflightBytes, err := readExactJSONBytes[SalesforceOrgPreflight](postflightPath)
	if err != nil || !reflect.DeepEqual(postflightRead, postflight) {
		return SalesforceShard{}, fmt.Errorf("read sealed Salesforce postflight")
	}
	shard, err := NormalizeSalesforceFilterResults(plan, bundle, request.BundlePath, preflight, postflight, filter, command, request.ShardIndex, request.ShardCount)
	if err != nil {
		return SalesforceShard{}, err
	}
	for _, input := range []struct {
		path string
		hash string
	}{{request.BundlePath, bundleSHA}, {planPath, replayBytesSHA256(planBytes)}, {request.PreflightPath, replayBytesSHA256(preflightBytes)}, {filterPathResult, replayBytesSHA256(filterBytes)}, {postflightPath, replayBytesSHA256(postflightBytes)}} {
		if hash, err := sha256File(input.path); err != nil || hash != input.hash {
			return SalesforceShard{}, fmt.Errorf("Salesforce shard input changed during execution")
		}
	}
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

func runSalesforceFilterCommand(runner salesforceCommandRunner, binary string, args ...string) (salesforceCommandOutput, CommandResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), salesforceFilterTimeout)
	defer cancel()
	started := time.Now()
	output, err := runner(ctx, binary, args...)
	receipt := CommandResult{Command: append([]string{binary}, args...), CommandSpecSHA256: commandSpecSHA256(ReplayCommand{Path: binary, Args: args, Timeout: salesforceFilterTimeout}), ExitCode: output.ExitCode, DurationMS: time.Since(started).Milliseconds(), StdoutSHA256: replayBytesSHA256(output.Stdout), StderrSHA256: replayBytesSHA256(output.Stderr), Passed: err == nil && output.ExitCode == 0, TimedOut: ctx.Err() == context.DeadlineExceeded}
	if err != nil || output.ExitCode != 0 || receipt.TimedOut {
		return output, receipt, fmt.Errorf("Salesforce filter command failed")
	}
	return output, receipt, nil
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
	receipt := CommandResult{Command: append([]string{binary}, args...), WorkingDirectory: workingDirectory, Environment: environment, ExecutableSHA256: binarySHA256, CommandSpecSHA256: salesforceCommandSpecSHA256(binary, args, workingDirectory, environment, binarySHA256), ExitCode: output.ExitCode, DurationMS: time.Since(started).Milliseconds(), StdoutSHA256: replayBytesSHA256(output.Stdout), StderrSHA256: replayBytesSHA256(output.Stderr), Passed: runErr == nil && output.ExitCode == 0, TimedOut: ctx.Err() == context.DeadlineExceeded}
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
func NormalizeSalesforceFilterResults(plan OraclePlan, bundle OracleBundle, bundlePath string, preflight, postflight SalesforceOrgPreflight, filter salesforceFilterResults, command CommandResult, shardIndex, shardCount int) (SalesforceShard, error) {
	expected, err := oracleSalesforceResultKinds(plan)
	if err != nil {
		return SalesforceShard{}, err
	}
	if !validSalesforceOrgPreflight(preflight, preflight.BundleSHA256, bundlePath) || !validSalesforceOrgPreflight(postflight, preflight.BundleSHA256, bundlePath) || preflight.OrgAlias != postflight.OrgAlias || preflight.OrgID != postflight.OrgID || !filter.Sealed || len(filter.Orgs) != 1 || filter.Orgs[0] != preflight.OrgAlias || !filter.RemoteCleanup.ResidueAbsent || !filter.OrgPostflight.MatchesPreflight || !command.Passed || command.ExitCode != 0 || command.TimedOut {
		return SalesforceShard{}, fmt.Errorf("invalid Salesforce filter or org evidence")
	}
	if filter.Binding.ManifestSHA256 != bundle.TransportManifestSHA256 || filter.Binding.ProfileSHA256 != bundle.ProfileSHA256 || filter.Binding.QueueSHA256 != bundle.OraclePlanSHA256 || filter.Binding.SelectorSHA256 != bundle.OraclePlanSHA256 || filter.Binding.SelectorReceiptSHA256 != preflight.BundleSHA256 || filter.Binding.CandidateCommit != bundle.Candidate.Commit || filter.Binding.CandidateSHA256 != bundle.Candidate.SHA256 || filter.Binding.ToolsCommit != bundle.Tools.Commit || filter.Binding.WorkflowScriptSHA256 != bundle.FilterSHA256 || filter.Binding.LocalSummarySHA256 != bundle.LocalProofSummarySHA256 {
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
			if action == oracleRuntime && result.Kind != "exec" && (result.RuntimePassed == nil || !*result.RuntimePassed) {
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
	return SalesforceShard{Bindings: SalesforceBindings{OraclePlanSHA256: bundle.OraclePlanSHA256, BundleSHA256: bundleSHA, FilterSHA256: bundle.FilterSHA256, FilterCommandSpecSHA256: command.CommandSpecSHA256}, Candidate: bundle.Candidate, Tools: bundle.Tools, ShardIndex: shardIndex, ShardCount: shardCount, OrgAlias: preflight.OrgAlias, OrgID: preflight.OrgID, OrgStatus: preflight.OrgStatus, PreInventory: preflight.Inventory, Commands: []CommandResult{command}, PostInventory: postflight.Inventory, Results: results, Cleanup: CleanupReceipt{ResidueAbsent: true}}, nil
}

func validSalesforceOrgPreflight(preflight SalesforceOrgPreflight, bundleSHA, bundlePath string) bool {
	environment, err := fixedSalesforceEnvironment()
	if err != nil || !filepath.IsAbs(bundlePath) || preflight.SchemaVersion != 1 || preflight.BundleSHA256 != bundleSHA || preflight.OrgAlias == "" || preflight.OrgID == "" || preflight.OrgStatus != "Active" || !zeroInventory(preflight.Inventory) || len(preflight.Inventory.Counts) != len(salesforceInventoryTypes) || len(preflight.Commands) != len(salesforceInventoryTypes)+1 {
		return false
	}
	for index, args := range salesforcePreflightArgs(preflight.OrgAlias) {
		command := preflight.Commands[index]
		expectedCommand := append([]string{"/usr/local/bin/sf"}, args...)
		expectedSpec := salesforceCommandSpecSHA256("/usr/local/bin/sf", args, filepath.Dir(bundlePath), environment, command.ExecutableSHA256)
		if !equalStrings(command.Command, expectedCommand) || command.WorkingDirectory != filepath.Dir(bundlePath) || !reflect.DeepEqual(command.Environment, environment) || !sha256Pattern.MatchString(command.ExecutableSHA256) || command.CommandSpecSHA256 != expectedSpec || !command.Passed || command.ExitCode != 0 || command.TimedOut || !sha256Pattern.MatchString(command.StdoutSHA256) || !sha256Pattern.MatchString(command.StderrSHA256) {
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
	expectedSpec := salesforceCommandSpecSHA256("/usr/local/bin/sf", args, filepath.Dir(bundlePath), environment, creation.Command.ExecutableSHA256)
	return err == nil && filepath.IsAbs(bundlePath) && creation.SchemaVersion == 1 && creation.BundleSHA256 == bundleSHA && creation.DevHub == devHub && creation.Alias == alias && creation.OrgID != "" && equalStrings(creation.Command.Command, append([]string{"/usr/local/bin/sf"}, args...)) && creation.Command.WorkingDirectory == filepath.Dir(bundlePath) && reflect.DeepEqual(creation.Command.Environment, environment) && sha256Pattern.MatchString(creation.Command.ExecutableSHA256) && creation.Command.CommandSpecSHA256 == expectedSpec && creation.Command.Passed && creation.Command.ExitCode == 0 && !creation.Command.TimedOut && sha256Pattern.MatchString(creation.Command.StdoutSHA256) && sha256Pattern.MatchString(creation.Command.StderrSHA256)
}

func fixedSalesforceEnvironment() ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil || !filepath.IsAbs(home) {
		return nil, fmt.Errorf("resolve Salesforce CLI home")
	}
	return []string{"HOME=" + home, "PATH=/usr/local/bin:/usr/bin:/bin", "SF_USE_GENERIC_UNIX_KEYCHAIN=true", "TMPDIR=/private/tmp"}, nil
}

func salesforceCommandSpecSHA256(binary string, args []string, workingDirectory string, environment []string, executableSHA256 string) string {
	data, _ := json.Marshal(struct {
		Binary           string   `json:"binary"`
		Arguments        []string `json:"arguments"`
		Environment      []string `json:"environment"`
		WorkingDirectory string   `json:"workingDirectory"`
		ExecutableSHA256 string   `json:"executableSha256"`
		TimeoutNS        int64    `json:"timeoutNs"`
	}{binary, args, environment, workingDirectory, executableSHA256, salesforceCommandTimeout.Nanoseconds()})
	return replayBytesSHA256(data)
}

func salesforceFilterArgs(filterPath, bundleRoot, executorRoot, runID, orgAlias string, bundle OracleBundle, bundleSHA string, shardIndex, shardCount int) ([]string, error) {
	if !filepath.IsAbs(filterPath) || !filepath.IsAbs(bundleRoot) || !filepath.IsAbs(executorRoot) || !strings.Contains(filepath.ToSlash(executorRoot), "/executor/") || runID == "" || orgAlias == "" || !sha256Pattern.MatchString(bundleSHA) || ValidateRuntimeArtifact(bundle.Candidate) != nil || ValidateRuntimeArtifact(bundle.Tools) != nil || !sha256Pattern.MatchString(bundle.OraclePlanSHA256) || !sha256Pattern.MatchString(bundle.TransportManifestSHA256) || !sha256Pattern.MatchString(bundle.LocalProofSummarySHA256) || len(bundle.Fixtures) == 0 || shardCount != 2 || shardIndex < 0 || shardIndex >= shardCount {
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
		"--ssh-host", "razor.local",
		"--ssh-user", "matt",
		"--remote-root", executorRoot,
		"--remote-run-id", runID,
		"--remote-sf-bin", "/usr/local/bin/sf",
		"--candidate-commit", bundle.Candidate.Commit,
		"--candidate-sha256", bundle.Candidate.SHA256,
		"--tools-commit", bundle.Tools.Commit,
		"--queue-sha256", bundle.OraclePlanSHA256,
		"--selector-sha256", bundle.OraclePlanSHA256,
		"--selector-receipt-sha256", bundleSHA,
		"--runtime",
		"--local-summary", filepath.Join(bundleRoot, "LOCAL_PROOF_SUMMARY.json"),
		"--manifest-index-modulus", strconv.Itoa(shardCount),
		"--manifest-index-remainder", strconv.Itoa(shardIndex),
	}, nil
}

// ValidateSalesforceShardFiles derives the runtime and compile denominator
// from the sealed oracle plan, then validates every raw shard against it.
// Callers cannot choose a smaller expected set.
func ValidateSalesforceShardFiles(planPath string, shardPaths []string) error {
	if !filepath.IsAbs(planPath) || len(shardPaths) == 0 {
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
	shards := make([]SalesforceShard, 0, len(shardPaths))
	shardHashes := make([]string, 0, len(shardPaths))
	for _, path := range shardPaths {
		if !filepath.IsAbs(path) {
			return fmt.Errorf("absolute Salesforce shard paths are required")
		}
		shard, data, err := readExactJSONBytes[SalesforceShard](path)
		if err != nil {
			return fmt.Errorf("read Salesforce shard: %w", err)
		}
		if shard.Bindings.OraclePlanSHA256 != planSHA || shard.Candidate != plan.Candidate || shard.Tools != plan.Tools {
			return fmt.Errorf("Salesforce shard does not bind sealed oracle plan")
		}
		shards, shardHashes = append(shards, shard), append(shardHashes, replayBytesSHA256(data))
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
	for index, path := range shardPaths {
		if _, after, err := readExactJSONBytes[SalesforceShard](path); err != nil || replayBytesSHA256(after) != shardHashes[index] {
			return fmt.Errorf("Salesforce shard changed during reconciliation")
		}
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
		if ValidateRuntimeArtifact(shard.Candidate) != nil || ValidateRuntimeArtifact(shard.Tools) != nil || !validSalesforceBindings(shard.Bindings) || shard.Candidate != first.Candidate || shard.Tools != first.Tools || shard.Bindings != first.Bindings || shard.ShardCount != len(shards) || shard.ShardIndex < 0 || shard.ShardIndex >= shard.ShardCount || indexes[shard.ShardIndex] || shard.OrgAlias == "" || aliases[shard.OrgAlias] || shard.OrgID == "" || orgs[shard.OrgID] || shard.OrgStatus != "Active" || !validSalesforceCommands(shard.Commands, shard.Bindings.FilterCommandSpecSHA256) || !zeroInventory(shard.PreInventory) || !sameInventory(shard.PreInventory, shard.PostInventory) || !shard.Cleanup.ResidueAbsent {
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

func validSalesforceBindings(bindings SalesforceBindings) bool {
	return sha256Pattern.MatchString(bindings.OraclePlanSHA256) && sha256Pattern.MatchString(bindings.BundleSHA256) && sha256Pattern.MatchString(bindings.FilterSHA256) && sha256Pattern.MatchString(bindings.FilterCommandSpecSHA256)
}

func validSalesforceCommands(commands []CommandResult, filterSpecSHA256 string) bool {
	if len(commands) != 1 {
		return false
	}
	command := commands[0]
	return len(command.Command) > 0 && command.CommandSpecSHA256 == filterSpecSHA256 && command.ExitCode == 0 && command.Passed && !command.TimedOut && sha256Pattern.MatchString(command.StdoutSHA256) && sha256Pattern.MatchString(command.StderrSHA256)
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
