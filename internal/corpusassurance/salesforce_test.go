package corpusassurance

import (
	"strings"
	"testing"
)

func TestValidateSalesforceShardsRequiresCleanDisjointCompleteEvidence(t *testing.T) {
	candidate := RuntimeArtifact{Commit: strings.Repeat("a", 40), OS: "darwin", Arch: "arm64", SHA256: strings.Repeat("b", 64)}
	tools := RuntimeArtifact{Commit: strings.Repeat("c", 40), OS: "darwin", Arch: "amd64", SHA256: strings.Repeat("d", 64)}
	bindings := SalesforceBindings{OraclePlanSHA256: strings.Repeat("e", 64), BundleSHA256: strings.Repeat("f", 64), FilterSHA256: strings.Repeat("1", 64), FilterCommandSpecSHA256: strings.Repeat("2", 64)}
	command := CommandResult{Command: []string{"python3", "transport/salesforce-first-filter.py"}, CommandSpecSHA256: bindings.FilterCommandSpecSHA256, ExitCode: 0, Passed: true, StdoutSHA256: strings.Repeat("3", 64), StderrSHA256: strings.Repeat("4", 64)}
	shards := []SalesforceShard{
		{Bindings: bindings, Candidate: candidate, Tools: tools, ShardIndex: 0, ShardCount: 2, OrgAlias: "assurance-sf0", OrgID: "00D0", OrgStatus: "Active", PreInventory: SalesforceInventory{}, Commands: []CommandResult{command}, PostInventory: SalesforceInventory{}, Results: []SalesforceSurfaceResult{{SurfaceID: "apex:System.run()", Kind: "runtime", Passed: true}}, Cleanup: CleanupReceipt{ResidueAbsent: true}},
		{Bindings: bindings, Candidate: candidate, Tools: tools, ShardIndex: 1, ShardCount: 2, OrgAlias: "assurance-sf1", OrgID: "00D1", OrgStatus: "Active", PreInventory: SalesforceInventory{}, Commands: []CommandResult{command}, PostInventory: SalesforceInventory{}, Results: []SalesforceSurfaceResult{{SurfaceID: "apex:System.compile()", Kind: "compile", Passed: true}}, Cleanup: CleanupReceipt{ResidueAbsent: true}},
	}
	if err := ValidateSalesforceShards(shards, []string{"apex:System.compile()", "apex:System.run()"}); err != nil {
		t.Fatal(err)
	}
}

func TestValidateSalesforceShardsRejectsResidueAndGaps(t *testing.T) {
	artifact := RuntimeArtifact{Commit: strings.Repeat("a", 40), OS: "darwin", Arch: "arm64", SHA256: strings.Repeat("b", 64)}
	bindings := SalesforceBindings{OraclePlanSHA256: strings.Repeat("e", 64), BundleSHA256: strings.Repeat("f", 64), FilterSHA256: strings.Repeat("1", 64), FilterCommandSpecSHA256: strings.Repeat("2", 64)}
	command := CommandResult{Command: []string{"python3", "transport/salesforce-first-filter.py"}, CommandSpecSHA256: bindings.FilterCommandSpecSHA256, ExitCode: 0, Passed: true, StdoutSHA256: strings.Repeat("3", 64), StderrSHA256: strings.Repeat("4", 64)}
	shard := SalesforceShard{Bindings: bindings, Candidate: artifact, Tools: artifact, ShardIndex: 0, ShardCount: 1, OrgAlias: "assurance-sf0", OrgID: "00D0", OrgStatus: "Active", PreInventory: SalesforceInventory{}, Commands: []CommandResult{command}, PostInventory: SalesforceInventory{}, Results: []SalesforceSurfaceResult{{SurfaceID: "apex:System.run()", Kind: "runtime", Passed: true}}, Cleanup: CleanupReceipt{ResidueAbsent: false}}
	if err := ValidateSalesforceShards([]SalesforceShard{shard}, []string{"apex:System.run()"}); err == nil {
		t.Fatal("accepted cleanup residue")
	}
}

func TestValidateSalesforceShardsRejectsAnUnboundRemoteCommand(t *testing.T) {
	candidate := RuntimeArtifact{Commit: strings.Repeat("a", 40), OS: "darwin", Arch: "arm64", SHA256: strings.Repeat("b", 64)}
	tools := RuntimeArtifact{Commit: strings.Repeat("c", 40), OS: "darwin", Arch: "amd64", SHA256: strings.Repeat("d", 64)}
	shard := SalesforceShard{
		Bindings:  SalesforceBindings{OraclePlanSHA256: strings.Repeat("e", 64), BundleSHA256: strings.Repeat("f", 64), FilterSHA256: strings.Repeat("1", 64), FilterCommandSpecSHA256: strings.Repeat("5", 64)},
		Candidate: candidate, Tools: tools, ShardIndex: 0, ShardCount: 1, OrgAlias: "assurance-sf0", OrgID: "00D0", OrgStatus: "Active",
		PreInventory: SalesforceInventory{}, PostInventory: SalesforceInventory{},
		Commands: []CommandResult{{Command: []string{"python3", "transport/filter.py"}, CommandSpecSHA256: strings.Repeat("2", 64), ExitCode: 0, Passed: true, StdoutSHA256: strings.Repeat("3", 64), StderrSHA256: strings.Repeat("4", 64)}},
		Results:  []SalesforceSurfaceResult{{SurfaceID: "apex:System.run()", Kind: oracleRuntime, Passed: true}}, Cleanup: CleanupReceipt{ResidueAbsent: true},
	}
	if err := ValidateSalesforceShards([]SalesforceShard{shard}, []string{"apex:System.run()"}); err == nil {
		t.Fatal("accepted a Salesforce command without the sealed filter binding")
	}
}

func TestValidateSalesforceShardsRejectsReusedOrgAlias(t *testing.T) {
	candidate := RuntimeArtifact{Commit: strings.Repeat("a", 40), OS: "darwin", Arch: "arm64", SHA256: strings.Repeat("b", 64)}
	tools := RuntimeArtifact{Commit: strings.Repeat("c", 40), OS: "darwin", Arch: "amd64", SHA256: strings.Repeat("d", 64)}
	bindings := SalesforceBindings{OraclePlanSHA256: strings.Repeat("e", 64), BundleSHA256: strings.Repeat("f", 64), FilterSHA256: strings.Repeat("1", 64), FilterCommandSpecSHA256: strings.Repeat("2", 64)}
	command := CommandResult{Command: []string{"python3", "transport/salesforce-first-filter.py"}, CommandSpecSHA256: bindings.FilterCommandSpecSHA256, ExitCode: 0, Passed: true, StdoutSHA256: strings.Repeat("3", 64), StderrSHA256: strings.Repeat("4", 64)}
	shards := []SalesforceShard{
		{Bindings: bindings, Candidate: candidate, Tools: tools, ShardIndex: 0, ShardCount: 2, OrgAlias: "assurance-sf", OrgID: "00D0", OrgStatus: "Active", PreInventory: SalesforceInventory{}, Commands: []CommandResult{command}, PostInventory: SalesforceInventory{}, Results: []SalesforceSurfaceResult{{SurfaceID: "apex:System.run()", Kind: oracleRuntime, Passed: true}}, Cleanup: CleanupReceipt{ResidueAbsent: true}},
		{Bindings: bindings, Candidate: candidate, Tools: tools, ShardIndex: 1, ShardCount: 2, OrgAlias: "assurance-sf", OrgID: "00D1", OrgStatus: "Active", PreInventory: SalesforceInventory{}, Commands: []CommandResult{command}, PostInventory: SalesforceInventory{}, Results: []SalesforceSurfaceResult{{SurfaceID: "apex:System.compile()", Kind: oracleCompile, Passed: true}}, Cleanup: CleanupReceipt{ResidueAbsent: true}},
	}
	if err := ValidateSalesforceShards(shards, []string{"apex:System.run()", "apex:System.compile()"}); err == nil {
		t.Fatal("accepted shards that reuse a scratch-org alias")
	}
}
