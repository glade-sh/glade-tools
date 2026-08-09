package corpusassurance

import (
	"fmt"
	"path/filepath"
)

var salesforceInventoryTypes = []string{"ApexClass", "ApexPage", "ApexTrigger", "CustomObject", "CustomField", "FieldSet", "StaticResource", "PlatformCachePartition"}

type SalesforceInventory struct {
	Counts map[string]int `json:"counts,omitempty"`
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
