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
type SalesforceShard struct {
	Candidate     RuntimeArtifact           `json:"candidate"`
	Tools         RuntimeArtifact           `json:"tools"`
	ShardIndex    int                       `json:"shardIndex"`
	ShardCount    int                       `json:"shardCount"`
	OrgID         string                    `json:"orgId"`
	OrgStatus     string                    `json:"orgStatus"`
	PreInventory  SalesforceInventory       `json:"preInventory"`
	PostInventory SalesforceInventory       `json:"postInventory"`
	Results       []SalesforceSurfaceResult `json:"results"`
	Cleanup       CleanupReceipt            `json:"cleanup"`
}

func ValidateSalesforceShards(shards []SalesforceShard, expected []string) error {
	if len(shards) == 0 || len(expected) == 0 {
		return fmt.Errorf("Salesforce shards and expected surfaces are required")
	}
	expectedSet, results, indexes, orgs := map[string]bool{}, map[string]bool{}, map[int]bool{}, map[string]bool{}
	for _, id := range expected {
		if id == "" || expectedSet[id] {
			return fmt.Errorf("invalid expected surface %q", id)
		}
		expectedSet[id] = true
	}
	first := shards[0]
	for _, shard := range shards {
		if ValidateRuntimeArtifact(shard.Candidate) != nil || ValidateRuntimeArtifact(shard.Tools) != nil || shard.Candidate != first.Candidate || shard.Tools != first.Tools || shard.ShardCount != len(shards) || shard.ShardIndex < 0 || shard.ShardIndex >= shard.ShardCount || indexes[shard.ShardIndex] || shard.OrgID == "" || orgs[shard.OrgID] || shard.OrgStatus != "Active" || !zeroInventory(shard.PreInventory) || !sameInventory(shard.PreInventory, shard.PostInventory) || !shard.Cleanup.ResidueAbsent {
			return fmt.Errorf("invalid Salesforce shard %d", shard.ShardIndex)
		}
		indexes[shard.ShardIndex], orgs[shard.OrgID] = true, true
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
