package corpusassurance

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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

func TestValidateSalesforceShardFilesDerivesRequiredSurfacesFromTheSealedPlan(t *testing.T) {
	root := t.TempDir()
	planPath, shardPath := filepath.Join(root, "ORACLE_PLAN.json"), filepath.Join(root, "shard.json")
	candidate := RuntimeArtifact{Commit: strings.Repeat("a", 40), OS: "darwin", Arch: "arm64", SHA256: strings.Repeat("b", 64)}
	tools := RuntimeArtifact{Commit: strings.Repeat("c", 40), OS: "darwin", Arch: "amd64", SHA256: strings.Repeat("d", 64)}
	plan := OraclePlan{Candidate: candidate, Tools: tools, Rows: []OraclePlanRow{{SurfaceID: "apex:System.compile()", Action: oracleCompile}, {SurfaceID: "apex:System.run()", Action: oracleRuntime}, {SurfaceID: "apex:Hosted.only", Action: oracleWaiver, ExclusionClass: "hosted", ExclusionReason: "identity"}}}
	if err := WriteNewJSON(planPath, plan); err != nil {
		t.Fatal(err)
	}
	bindings := SalesforceBindings{OraclePlanSHA256: localProofFileSHA256(t, planPath), BundleSHA256: strings.Repeat("e", 64), FilterSHA256: strings.Repeat("f", 64), FilterCommandSpecSHA256: strings.Repeat("1", 64)}
	command := CommandResult{Command: []string{"python3", "transport/salesforce-first-filter.py"}, CommandSpecSHA256: bindings.FilterCommandSpecSHA256, ExitCode: 0, Passed: true, StdoutSHA256: strings.Repeat("2", 64), StderrSHA256: strings.Repeat("3", 64)}
	shard := SalesforceShard{Bindings: bindings, Candidate: candidate, Tools: tools, ShardIndex: 0, ShardCount: 1, OrgAlias: "assurance-sf0", OrgID: "00D0", OrgStatus: "Active", PreInventory: SalesforceInventory{}, Commands: []CommandResult{command}, PostInventory: SalesforceInventory{}, Results: []SalesforceSurfaceResult{{SurfaceID: "apex:System.run()", Kind: oracleRuntime, Passed: true}, {SurfaceID: "apex:System.compile()", Kind: oracleCompile, Passed: true}}, Cleanup: CleanupReceipt{ResidueAbsent: true}}
	if err := WriteNewJSON(shardPath, shard); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSalesforceShardFiles(planPath, []string{shardPath}); err != nil {
		t.Fatalf("ValidateSalesforceShardFiles: %v", err)
	}
	wrongKindPath := filepath.Join(root, "wrong-kind.json")
	shard.Results[0].Kind = oracleCompile
	if err := WriteNewJSON(wrongKindPath, shard); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSalesforceShardFiles(planPath, []string{wrongKindPath}); err == nil {
		t.Fatal("accepted a compile receipt for a runtime oracle row")
	}
}

func TestRunSalesforceOrgPreflightSealsZeroEightTypeInventory(t *testing.T) {
	root := t.TempDir()
	bundlePath, outputPath := filepath.Join(root, "bundle.json"), filepath.Join(root, "preflight.json")
	if err := os.WriteFile(bundlePath, []byte(`{"bundle":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	commands := 0
	runner := func(_ context.Context, path string, args ...string) (salesforceCommandOutput, error) {
		commands++
		if path != "/usr/local/bin/sf" {
			return salesforceCommandOutput{}, fmt.Errorf("unexpected sf path %q", path)
		}
		if len(args) >= 2 && args[0] == "org" && args[1] == "display" {
			return salesforceCommandOutput{Stdout: []byte(`{"status":0,"result":{"id":"00D000000000001","status":"Active"}}`)}, nil
		}
		return salesforceCommandOutput{Stdout: []byte(`{"status":0,"result":{"totalSize":0}}`)}, nil
	}
	preflight, err := RunSalesforceOrgPreflight(SalesforceOrgPreflightRequest{BundlePath: bundlePath, TargetOrg: "assurance-sf0", SFBin: "/usr/local/bin/sf", OutputPath: outputPath, validateBundle: func(string) error { return nil }, runner: runner})
	if err != nil {
		t.Fatalf("RunSalesforceOrgPreflight: %v", err)
	}
	if preflight.OrgID != "00D000000000001" || preflight.OrgStatus != "Active" || !zeroInventory(preflight.Inventory) || len(preflight.Commands) != len(salesforceInventoryTypes)+1 || commands != len(salesforceInventoryTypes)+1 {
		t.Fatalf("preflight = %#v, commands=%d", preflight, commands)
	}
	if _, err := os.Stat(outputPath); err != nil {
		t.Fatal(err)
	}
}

func TestNormalizeSalesforceFilterResultsRequiresSealedPlanBundleAndOrgEvidence(t *testing.T) {
	zero := 0
	candidate := RuntimeArtifact{Commit: strings.Repeat("a", 40), OS: "darwin", Arch: "arm64", SHA256: strings.Repeat("b", 64)}
	tools := RuntimeArtifact{Commit: strings.Repeat("c", 40), OS: "darwin", Arch: "amd64", SHA256: strings.Repeat("d", 64)}
	plan := OraclePlan{Candidate: candidate, Tools: tools, Rows: []OraclePlanRow{{SurfaceID: "apex:System.run()", Action: oracleRuntime}, {SurfaceID: "apex:System.compile()", Action: oracleCompile}}}
	bundle := OracleBundle{Candidate: candidate, Tools: tools, ProfileSHA256: strings.Repeat("e", 64), OraclePlanSHA256: strings.Repeat("f", 64), TransportManifestSHA256: strings.Repeat("1", 64), LocalProofSummarySHA256: strings.Repeat("2", 64), FilterSHA256: strings.Repeat("3", 64)}
	preflight := SalesforceOrgPreflight{SchemaVersion: 1, BundleSHA256: strings.Repeat("4", 64), OrgAlias: "assurance-sf0", OrgID: "00D0", OrgStatus: "Active", Inventory: SalesforceInventory{Counts: map[string]int{}}}
	preflightArgs := [][]string{{"org", "display", "--target-org", preflight.OrgAlias, "--json"}}
	for _, kind := range salesforceInventoryTypes {
		preflightArgs = append(preflightArgs, []string{"data", "query", "--query", "SELECT count() FROM " + kind, "--target-org", preflight.OrgAlias, "--json"})
		preflight.Inventory.Counts[kind] = 0
	}
	for _, args := range preflightArgs {
		preflight.Commands = append(preflight.Commands, CommandResult{Command: append([]string{"/usr/local/bin/sf"}, args...), CommandSpecSHA256: commandSpecSHA256(ReplayCommand{Path: "/usr/local/bin/sf", Args: args, Env: []string{"SF_USE_GENERIC_UNIX_KEYCHAIN=true"}, Timeout: salesforceCommandTimeout}), ExitCode: 0, Passed: true, StdoutSHA256: strings.Repeat("6", 64), StderrSHA256: strings.Repeat("7", 64)})
	}
	postflight := preflight
	filter := salesforceFilterResults{Sealed: true, Orgs: []string{"assurance-sf0"}, Binding: salesforceFilterBinding{ManifestSHA256: bundle.TransportManifestSHA256, ProfileSHA256: bundle.ProfileSHA256, QueueSHA256: bundle.OraclePlanSHA256, SelectorSHA256: bundle.OraclePlanSHA256, SelectorReceiptSHA256: preflight.BundleSHA256, CandidateCommit: candidate.Commit, CandidateSHA256: candidate.SHA256, ToolsCommit: tools.Commit, WorkflowScriptSHA256: bundle.FilterSHA256, LocalSummarySHA256: bundle.LocalProofSummarySHA256}, RemoteCleanup: CleanupReceipt{ResidueAbsent: true}, OrgPostflight: salesforceFilterPostflight{MatchesPreflight: true}, Results: []salesforceFilterFixtureResult{{SurfaceIDs: []string{"apex:System.run()"}, Org: "assurance-sf0", Kind: "exec", ExitCode: &zero, Deployable: true, RemoteCleanup: CleanupReceipt{ResidueAbsent: true}, OrgCleanup: CleanupReceipt{ResidueAbsent: true}}, {SurfaceIDs: []string{"apex:System.compile()"}, Org: "assurance-sf0", Kind: "check", ExitCode: &zero, Deployable: true, RemoteCleanup: CleanupReceipt{ResidueAbsent: true}, OrgCleanup: CleanupReceipt{ResidueAbsent: true}}}}
	command := CommandResult{Command: []string{"python3", "transport/salesforce-first-filter.py"}, CommandSpecSHA256: strings.Repeat("5", 64), ExitCode: 0, Passed: true, StdoutSHA256: strings.Repeat("6", 64), StderrSHA256: strings.Repeat("7", 64)}
	shard, err := NormalizeSalesforceFilterResults(plan, bundle, preflight, postflight, filter, command, 0, 2)
	if err != nil {
		t.Fatalf("NormalizeSalesforceFilterResults: %v", err)
	}
	if shard.Results[0].Kind != oracleRuntime || shard.Results[1].Kind != oracleCompile || !sameInventory(shard.PreInventory, preflight.Inventory) || !sameInventory(shard.PostInventory, postflight.Inventory) {
		t.Fatalf("shard = %#v", shard)
	}
	preflight.Commands[0].Command = []string{"/usr/local/bin/sf", "org", "list", "--json"}
	if _, err := NormalizeSalesforceFilterResults(plan, bundle, preflight, postflight, filter, command, 0, 2); err == nil {
		t.Fatal("accepted a preflight receipt without the exact org-display command")
	}
}
