package corpusassurance

import "fmt"

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
