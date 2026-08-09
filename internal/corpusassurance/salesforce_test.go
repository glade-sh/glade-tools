package corpusassurance

import (
	"strings"
	"testing"
)

func TestValidateSalesforceShardsRequiresCleanDisjointCompleteEvidence(t *testing.T) {
	candidate := RuntimeArtifact{Commit: strings.Repeat("a", 40), OS: "darwin", Arch: "arm64", SHA256: strings.Repeat("b", 64)}
	tools := RuntimeArtifact{Commit: strings.Repeat("c", 40), OS: "darwin", Arch: "amd64", SHA256: strings.Repeat("d", 64)}
	shards := []SalesforceShard{
		{Candidate: candidate, Tools: tools, ShardIndex: 0, ShardCount: 2, OrgID: "00D0", OrgStatus: "Active", PreInventory: SalesforceInventory{}, PostInventory: SalesforceInventory{}, Results: []SalesforceSurfaceResult{{SurfaceID: "apex:System.run()", Kind: "runtime", Passed: true}}, Cleanup: CleanupReceipt{ResidueAbsent: true}},
		{Candidate: candidate, Tools: tools, ShardIndex: 1, ShardCount: 2, OrgID: "00D1", OrgStatus: "Active", PreInventory: SalesforceInventory{}, PostInventory: SalesforceInventory{}, Results: []SalesforceSurfaceResult{{SurfaceID: "apex:System.compile()", Kind: "compile", Passed: true}}, Cleanup: CleanupReceipt{ResidueAbsent: true}},
	}
	if err := ValidateSalesforceShards(shards, []string{"apex:System.compile()", "apex:System.run()"}); err != nil {
		t.Fatal(err)
	}
}

func TestValidateSalesforceShardsRejectsResidueAndGaps(t *testing.T) {
	artifact := RuntimeArtifact{Commit: strings.Repeat("a", 40), OS: "darwin", Arch: "arm64", SHA256: strings.Repeat("b", 64)}
	shard := SalesforceShard{Candidate: artifact, Tools: artifact, ShardIndex: 0, ShardCount: 1, OrgID: "00D0", OrgStatus: "Active", PreInventory: SalesforceInventory{}, PostInventory: SalesforceInventory{}, Results: []SalesforceSurfaceResult{{SurfaceID: "apex:System.run()", Kind: "runtime", Passed: true}}, Cleanup: CleanupReceipt{ResidueAbsent: false}}
	if err := ValidateSalesforceShards([]SalesforceShard{shard}, []string{"apex:System.run()"}); err == nil {
		t.Fatal("accepted cleanup residue")
	}
}
