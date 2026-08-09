package corpusassurance

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	display, displayReceipt, err := runSalesforcePreflightCommand(runner, request.SFBin, "org", "display", "--target-org", request.TargetOrg, "--json")
	if err != nil {
		return SalesforceOrgPreflight{}, err
	}
	orgID, status, err := parseSalesforceOrgDisplay(display.Stdout)
	if err != nil || status != "Active" {
		return SalesforceOrgPreflight{}, fmt.Errorf("scratch org is not Active")
	}
	preflight := SalesforceOrgPreflight{SchemaVersion: 1, BundleSHA256: bundleSHA, OrgAlias: request.TargetOrg, OrgID: orgID, OrgStatus: status, Inventory: SalesforceInventory{Counts: make(map[string]int)}, Commands: []CommandResult{displayReceipt}}
	for _, kind := range salesforceInventoryTypes {
		output, receipt, err := runSalesforcePreflightCommand(runner, request.SFBin, "data", "query", "--query", "SELECT count() FROM "+kind, "--target-org", request.TargetOrg, "--json")
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
	if err := WriteNewJSON(request.OutputPath, preflight); err != nil {
		return SalesforceOrgPreflight{}, err
	}
	return preflight, nil
}

func runSalesforcePreflightCommand(runner salesforceCommandRunner, binary string, args ...string) (salesforceCommandOutput, CommandResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), salesforceCommandTimeout)
	defer cancel()
	started := time.Now()
	output, err := runner(ctx, binary, args...)
	receipt := CommandResult{Command: append([]string{binary}, args...), CommandSpecSHA256: commandSpecSHA256(ReplayCommand{Path: binary, Args: args, Env: []string{"SF_USE_GENERIC_UNIX_KEYCHAIN=true"}, Timeout: salesforceCommandTimeout}), ExitCode: output.ExitCode, DurationMS: time.Since(started).Milliseconds(), StdoutSHA256: replayBytesSHA256(output.Stdout), StderrSHA256: replayBytesSHA256(output.Stderr), Passed: err == nil && output.ExitCode == 0, TimedOut: ctx.Err() == context.DeadlineExceeded}
	if err != nil || output.ExitCode != 0 || receipt.TimedOut {
		return output, receipt, fmt.Errorf("Salesforce preflight command failed")
	}
	return output, receipt, nil
}

func runSalesforceCLI(ctx context.Context, binary string, args ...string) (salesforceCommandOutput, error) {
	command := exec.CommandContext(ctx, binary, args...)
	command.Env = append(os.Environ(), "SF_USE_GENERIC_UNIX_KEYCHAIN=true")
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
