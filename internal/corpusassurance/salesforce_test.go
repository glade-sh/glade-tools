package corpusassurance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestValidateSalesforceShardsRequiresCleanDisjointCompleteEvidence(t *testing.T) {
	candidate := RuntimeArtifact{Commit: strings.Repeat("a", 40), OS: "darwin", Arch: "arm64", SHA256: strings.Repeat("b", 64)}
	tools := RuntimeArtifact{Commit: strings.Repeat("c", 40), OS: "darwin", Arch: "amd64", SHA256: strings.Repeat("d", 64)}
	bindings := SalesforceBindings{OraclePlanSHA256: strings.Repeat("e", 64), BundleSHA256: strings.Repeat("f", 64), FilterSHA256: strings.Repeat("1", 64), FilterCommandSpecSHA256: strings.Repeat("2", 64)}
	command := CommandResult{Command: []string{"python3", "transport/salesforce-first-filter.py"}, CommandSpecSHA256: bindings.FilterCommandSpecSHA256, ExitCode: 0, Passed: true, StdoutSHA256: strings.Repeat("3", 64), StderrSHA256: strings.Repeat("4", 64)}
	preflight0 := shardLifecycleForTest("assurance-sf0", "00D0", bindings.BundleSHA256)
	preflight1 := shardLifecycleForTest("assurance-sf1", "00D1", bindings.BundleSHA256)
	shards := []SalesforceShard{
		{Bindings: bindings, Candidate: candidate, Tools: tools, ShardIndex: 0, ShardCount: 2, OrgAlias: "assurance-sf0", OrgID: "00D0", OrgStatus: "Active", Preflight: preflight0, PreInventory: SalesforceInventory{}, Commands: []CommandResult{command}, Postflight: preflight0, PostInventory: SalesforceInventory{}, Results: []SalesforceSurfaceResult{{SurfaceID: "apex:System.run()", Kind: "runtime", Passed: true}}, Cleanup: CleanupReceipt{ResidueAbsent: true}},
		{Bindings: bindings, Candidate: candidate, Tools: tools, ShardIndex: 1, ShardCount: 2, OrgAlias: "assurance-sf1", OrgID: "00D1", OrgStatus: "Active", Preflight: preflight1, PreInventory: SalesforceInventory{}, Commands: []CommandResult{command}, Postflight: preflight1, PostInventory: SalesforceInventory{}, Results: []SalesforceSurfaceResult{{SurfaceID: "apex:System.compile()", Kind: "compile", Passed: true}}, Cleanup: CleanupReceipt{ResidueAbsent: true}},
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

func TestValidateSalesforceShardsRejectsMissingLifecycleReceipts(t *testing.T) {
	artifact := RuntimeArtifact{Commit: strings.Repeat("a", 40), OS: "darwin", Arch: "arm64", SHA256: strings.Repeat("b", 64)}
	bindings := SalesforceBindings{OraclePlanSHA256: strings.Repeat("c", 64), BundleSHA256: strings.Repeat("d", 64), FilterSHA256: strings.Repeat("e", 64), FilterCommandSpecSHA256: strings.Repeat("f", 64)}
	filter := CommandResult{Command: []string{"python3", "transport/salesforce-first-filter.py"}, CommandSpecSHA256: bindings.FilterCommandSpecSHA256, ExitCode: 0, Passed: true, StdoutSHA256: strings.Repeat("1", 64), StderrSHA256: strings.Repeat("2", 64)}
	shard := SalesforceShard{Bindings: bindings, Candidate: artifact, Tools: artifact, ShardCount: 1, OrgAlias: "assurance-sf0", OrgID: "00D0", OrgStatus: "Active", Commands: []CommandResult{filter}, Results: []SalesforceSurfaceResult{{SurfaceID: "apex:System.run()", Kind: oracleRuntime, Passed: true}}, Cleanup: CleanupReceipt{ResidueAbsent: true}}
	if err := ValidateSalesforceShards([]SalesforceShard{shard}, []string{"apex:System.run()"}); err == nil {
		t.Fatal("accepted Salesforce shard without sealed preflight and postflight receipts")
	}
}

func TestValidSalesforceOrgPreflightRejectsUnsealedCLIExecution(t *testing.T) {
	root := t.TempDir()
	bundlePath := filepath.Join(root, "bundle.json")
	if err := os.WriteFile(bundlePath, []byte(`{"bundle":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	preflight := salesforcePreflightForTest(t, "assurance-sf0", localProofFileSHA256(t, bundlePath), bundlePath)
	preflight.Commands[0].ExecutableAfterSHA256 = ""
	if validSalesforceOrgPreflight(preflight, preflight.BundleSHA256, bundlePath) {
		t.Fatal("accepted Salesforce preflight commands without a sealed executable, environment, and working directory")
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
	inputs := oracleBundleTestInputsForLocalProof(t)
	writeSealedReleaseValidation(t, inputs.releasePath, inputs.attemptPath, inputs.plan.Candidate, inputs.plan.Tools)
	outputRoot := filepath.Join(t.TempDir(), "razor")
	bundle, err := BuildOracleBundle(inputs.request(outputRoot))
	if err != nil {
		t.Fatal(err)
	}
	bundlePath, planPath, shardPath := filepath.Join(outputRoot, "bundle", "bundle.json"), filepath.Join(outputRoot, "bundle", "ORACLE_PLAN.json"), filepath.Join(t.TempDir(), "shard.json")
	bundleSHA := localProofFileSHA256(t, bundlePath)
	executorRoot, runID, alias := filepath.Join(filepath.Dir(outputRoot), "executor", "shard-0"), "assurance-"+bundle.AttemptSHA256[:16]+"-shard-0", "assurance-sf0"
	args, err := salesforceFilterArgs(filepath.Join(outputRoot, "transport", "salesforce-first-filter.py"), filepath.Dir(bundlePath), executorRoot, runID, alias, bundle, bundleSHA, 0, 1)
	if err == nil {
		t.Fatal("salesforceFilterArgs accepted an invalid shard count")
	}
	args, err = salesforceFilterArgs(filepath.Join(outputRoot, "transport", "salesforce-first-filter.py"), filepath.Dir(bundlePath), executorRoot, runID, alias, bundle, bundleSHA, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	environment := mustFixedSalesforceEnvironment(t)
	command := CommandResult{Command: append([]string{"python3"}, args...), WorkingDirectory: filepath.Dir(bundlePath), Environment: environment, CommandSpecSHA256: salesforceFilterCommandSpecSHA256("python3", args, filepath.Dir(bundlePath), environment), ExitCode: 0, Passed: true, StdoutSHA256: strings.Repeat("2", 64), StderrSHA256: strings.Repeat("3", 64)}
	bindings := SalesforceBindings{OraclePlanSHA256: bundle.OraclePlanSHA256, BundleSHA256: bundleSHA, FilterSHA256: bundle.FilterSHA256, FilterCommandSpecSHA256: command.CommandSpecSHA256}
	lifecycle := salesforcePreflightForTest(t, alias, bundleSHA, bundlePath)
	shard := SalesforceShard{Bindings: bindings, Candidate: bundle.Candidate, Tools: bundle.Tools, ExecutorRoot: executorRoot, RunID: runID, ShardIndex: 0, ShardCount: 2, OrgAlias: alias, OrgID: lifecycle.OrgID, OrgStatus: "Active", Preflight: lifecycle, PreInventory: SalesforceInventory{}, Commands: []CommandResult{command}, Postflight: lifecycle, PostInventory: SalesforceInventory{}, Results: []SalesforceSurfaceResult{{SurfaceID: "apex:Runtime.run", Kind: oracleRuntime, Passed: true}}, Cleanup: CleanupReceipt{ResidueAbsent: true}}
	shard1Path := filepath.Join(t.TempDir(), "shard-1.json")
	shard1Alias, shard1Executor, shard1RunID := "assurance-sf1", filepath.Join(filepath.Dir(outputRoot), "executor", "shard-1"), "assurance-"+bundle.AttemptSHA256[:16]+"-shard-1"
	shard1Args, err := salesforceFilterArgs(filepath.Join(outputRoot, "transport", "salesforce-first-filter.py"), filepath.Dir(bundlePath), shard1Executor, shard1RunID, shard1Alias, bundle, bundleSHA, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	shard1Command := CommandResult{Command: append([]string{"python3"}, shard1Args...), WorkingDirectory: filepath.Dir(bundlePath), Environment: environment, CommandSpecSHA256: salesforceFilterCommandSpecSHA256("python3", shard1Args, filepath.Dir(bundlePath), environment), ExitCode: 0, Passed: true, StdoutSHA256: strings.Repeat("4", 64), StderrSHA256: strings.Repeat("5", 64)}
	shard1Lifecycle := salesforcePreflightForTest(t, shard1Alias, bundleSHA, bundlePath)
	shard1 := SalesforceShard{Bindings: SalesforceBindings{OraclePlanSHA256: bundle.OraclePlanSHA256, BundleSHA256: bundleSHA, FilterSHA256: bundle.FilterSHA256, FilterCommandSpecSHA256: shard1Command.CommandSpecSHA256}, Candidate: bundle.Candidate, Tools: bundle.Tools, ExecutorRoot: shard1Executor, RunID: shard1RunID, ShardIndex: 1, ShardCount: 2, OrgAlias: shard1Alias, OrgID: "00D1", OrgStatus: "Active", Preflight: shard1Lifecycle, PreInventory: SalesforceInventory{}, Commands: []CommandResult{shard1Command}, Postflight: shard1Lifecycle, PostInventory: SalesforceInventory{}, Cleanup: CleanupReceipt{ResidueAbsent: true}}
	shard1.Preflight.OrgID, shard1.Postflight.OrgID = shard1.OrgID, shard1.OrgID
	if err := WriteNewJSON(shardPath, shard); err != nil {
		t.Fatal(err)
	}
	if err := WriteNewJSON(shard1Path, shard1); err != nil {
		t.Fatal(err)
	}
	files0 := salesforceShardFilesForTest(t, shardPath, bundlePath, bundleSHA, alias, shard.OrgID)
	files1 := salesforceShardFilesForTest(t, shard1Path, bundlePath, bundleSHA, shard1Alias, shard1.OrgID)
	if err := ValidateSalesforceShardFiles(planPath, []SalesforceShardFiles{files0, files1}); err != nil {
		t.Fatalf("ValidateSalesforceShardFiles: %v", err)
	}
	rewritten, _, err := readExactJSONBytes[SalesforceShard](shardPath)
	if err != nil {
		t.Fatal(err)
	}
	rewritten.ExecutorRoot, rewritten.RunID = filepath.Join(t.TempDir(), "executor", "rewritten"), "rewritten-run"
	rewrittenArgs, err := salesforceFilterArgs(filepath.Join(outputRoot, "transport", "salesforce-first-filter.py"), filepath.Dir(bundlePath), rewritten.ExecutorRoot, rewritten.RunID, alias, bundle, bundleSHA, rewritten.ShardIndex, rewritten.ShardCount)
	if err != nil {
		t.Fatal(err)
	}
	rewritten.Commands[0].Command = append([]string{"python3"}, rewrittenArgs...)
	rewritten.Commands[0].CommandSpecSHA256 = salesforceFilterCommandSpecSHA256("python3", rewrittenArgs, filepath.Dir(bundlePath), environment)
	rewritten.Bindings.FilterCommandSpecSHA256 = rewritten.Commands[0].CommandSpecSHA256
	rewrittenPath := filepath.Join(t.TempDir(), "rewritten-dispatch.json")
	if err := WriteNewJSON(rewrittenPath, rewritten); err != nil {
		t.Fatal(err)
	}
	filesRewritten := files0
	filesRewritten.ShardPath = rewrittenPath
	if err := ValidateSalesforceShardFiles(planPath, []SalesforceShardFiles{filesRewritten, files1}); err == nil {
		t.Fatal("accepted a shard that rewrote its sealed dispatch")
	}
	wrongKindPath := filepath.Join(t.TempDir(), "wrong-kind.json")
	shard.Results[0].Kind = oracleCompile
	if err := WriteNewJSON(wrongKindPath, shard); err != nil {
		t.Fatal(err)
	}
	filesWrongKind := files0
	filesWrongKind.ShardPath = wrongKindPath
	if err := ValidateSalesforceShardFiles(planPath, []SalesforceShardFiles{filesWrongKind, files1}); err == nil {
		t.Fatal("accepted a compile receipt for a runtime oracle row")
	}
	wrongCommandPath := filepath.Join(t.TempDir(), "wrong-command.json")
	shard.Commands[0].Environment = nil
	if err := WriteNewJSON(wrongCommandPath, shard); err != nil {
		t.Fatal(err)
	}
	filesWrongCommand := files0
	filesWrongCommand.ShardPath = wrongCommandPath
	if err := ValidateSalesforceShardFiles(planPath, []SalesforceShardFiles{filesWrongCommand, files1}); err == nil {
		t.Fatal("accepted a filter receipt without the sealed environment")
	}
	filesReusedCreation := files1
	filesReusedCreation.CreationPath = files0.CreationPath
	if err := ValidateSalesforceShardFiles(planPath, []SalesforceShardFiles{files0, filesReusedCreation}); err == nil {
		t.Fatal("accepted a creation receipt reused by two shards")
	}
	if err := os.WriteFile(bundlePath, []byte(`{"schemaVersion":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSalesforceShardFiles(planPath, []SalesforceShardFiles{files0, files1}); err == nil {
		t.Fatal("accepted a replaced staged bundle")
	}
}

func TestValidateSalesforceShardFilesRejectsMissingStagedBundle(t *testing.T) {
	root := t.TempDir()
	planPath, shardPath := filepath.Join(root, "ORACLE_PLAN.json"), filepath.Join(root, "shard.json")
	artifact := RuntimeArtifact{Commit: strings.Repeat("a", 40), OS: "darwin", Arch: "arm64", SHA256: strings.Repeat("b", 64)}
	plan := OraclePlan{Candidate: artifact, Tools: artifact, Rows: []OraclePlanRow{{SurfaceID: "apex:System.run()", Action: oracleRuntime}}}
	if err := WriteNewJSON(planPath, plan); err != nil {
		t.Fatal(err)
	}
	bindings := SalesforceBindings{OraclePlanSHA256: localProofFileSHA256(t, planPath), BundleSHA256: strings.Repeat("c", 64), FilterSHA256: strings.Repeat("d", 64), FilterCommandSpecSHA256: strings.Repeat("e", 64)}
	lifecycle := shardLifecycleForTest("assurance-sf0", "00D0", bindings.BundleSHA256)
	filter := CommandResult{Command: []string{"python3", "transport/filter.py"}, CommandSpecSHA256: bindings.FilterCommandSpecSHA256, ExitCode: 0, Passed: true, StdoutSHA256: strings.Repeat("f", 64), StderrSHA256: strings.Repeat("1", 64)}
	shard := SalesforceShard{Bindings: bindings, Candidate: artifact, Tools: artifact, ShardCount: 1, OrgAlias: "assurance-sf0", OrgID: "00D0", OrgStatus: "Active", Preflight: lifecycle, Postflight: lifecycle, Commands: []CommandResult{filter}, Results: []SalesforceSurfaceResult{{SurfaceID: "apex:System.run()", Kind: oracleRuntime, Passed: true}}, Cleanup: CleanupReceipt{ResidueAbsent: true}}
	if err := WriteNewJSON(shardPath, shard); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSalesforceShardFiles(planPath, []SalesforceShardFiles{{ShardPath: shardPath, CreationPath: filepath.Join(root, "creation.json"), CleanupPath: filepath.Join(root, "cleanup.json")}}); err == nil {
		t.Fatal("accepted Salesforce shard files without the staged bundle that binds lifecycle receipts")
	}
}

func TestRunSalesforceOrgPreflightSealsZeroEightTypeInventory(t *testing.T) {
	root := t.TempDir()
	bundlePath, outputPath := filepath.Join(root, "bundle.json"), filepath.Join(root, "preflight.json")
	if err := os.WriteFile(bundlePath, []byte(`{"bundle":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	commands := 0
	runner := func(ctx context.Context, path string, args ...string) (salesforceCommandOutput, error) {
		commands++
		execution, ok := ctx.Value(salesforceExecutionKey{}).(salesforceExecution)
		if !ok || execution.workingDirectory != root {
			return salesforceCommandOutput{}, fmt.Errorf("unsealed Salesforce execution context")
		}
		environment, err := fixedSalesforceEnvironment()
		if err != nil || !reflect.DeepEqual(execution.environment, environment) {
			return salesforceCommandOutput{}, fmt.Errorf("unexpected Salesforce execution environment")
		}
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
	cliSHA256, err := sha256File("/usr/local/bin/sf")
	if err != nil || preflight.Commands[0].WorkingDirectory != root || !reflect.DeepEqual(preflight.Commands[0].Environment, mustFixedSalesforceEnvironment(t)) || preflight.Commands[0].ExecutableSHA256 != cliSHA256 {
		t.Fatalf("unsealed Salesforce command receipt = %#v, %v", preflight.Commands[0], err)
	}
	if _, err := os.Stat(outputPath); err != nil {
		t.Fatal(err)
	}
}

func TestRunSalesforceOrgPreflightRejectsBundleChangedDuringCommands(t *testing.T) {
	root := t.TempDir()
	bundlePath, outputPath := filepath.Join(root, "bundle.json"), filepath.Join(root, "preflight.json")
	if err := os.WriteFile(bundlePath, []byte(`{"bundle":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	validations := 0
	_, err := RunSalesforceOrgPreflight(SalesforceOrgPreflightRequest{
		BundlePath: bundlePath, TargetOrg: "assurance-sf0", SFBin: "/usr/local/bin/sf", OutputPath: outputPath,
		validateBundle: func(string) error {
			validations++
			if validations == 2 {
				return errors.New("bundle changed")
			}
			return nil
		},
		runner: func(_ context.Context, _ string, args ...string) (salesforceCommandOutput, error) {
			if len(args) >= 2 && args[0] == "org" && args[1] == "display" {
				return salesforceCommandOutput{Stdout: []byte(`{"status":0,"result":{"id":"00D000000000001","status":"Active"}}`)}, nil
			}
			return salesforceCommandOutput{Stdout: []byte(`{"status":0,"result":{"totalSize":0}}`)}, nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "bundle changed during org preflight") {
		t.Fatalf("RunSalesforceOrgPreflight error = %v", err)
	}
	if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
		t.Fatalf("preflight receipt exists after changed bundle: %v", err)
	}
}

func TestRunSalesforceOrgPreflightRejectsBundleHashChangedDuringCommands(t *testing.T) {
	root := t.TempDir()
	bundlePath, outputPath := filepath.Join(root, "bundle.json"), filepath.Join(root, "preflight.json")
	if err := os.WriteFile(bundlePath, []byte(`{"bundle":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := RunSalesforceOrgPreflight(SalesforceOrgPreflightRequest{
		BundlePath: bundlePath, TargetOrg: "assurance-sf0", SFBin: "/usr/local/bin/sf", OutputPath: outputPath,
		validateBundle: func(string) error { return nil },
		runner: func(_ context.Context, _ string, args ...string) (salesforceCommandOutput, error) {
			if len(args) >= 2 && args[0] == "org" && args[1] == "display" {
				return salesforceCommandOutput{Stdout: []byte(`{"status":0,"result":{"id":"00D000000000001","status":"Active"}}`)}, nil
			}
			if err := os.WriteFile(bundlePath, []byte(`{"bundle":false}`), 0o600); err != nil {
				t.Fatal(err)
			}
			return salesforceCommandOutput{Stdout: []byte(`{"status":0,"result":{"totalSize":0}}`)}, nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "staged bundle changed during org preflight") {
		t.Fatalf("RunSalesforceOrgPreflight error = %v", err)
	}
	if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
		t.Fatalf("preflight receipt exists after changed bundle: %v", err)
	}
}

func TestRunSalesforceOrgCreateSealsFreshBundleBoundReceipt(t *testing.T) {
	root := t.TempDir()
	bundlePath, outputPath := filepath.Join(root, "bundle.json"), filepath.Join(root, "org-create.json")
	if err := os.WriteFile(bundlePath, []byte(`{"bundle":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "corpus-assurance-scratch-def.json"), []byte(`{"orgName":"Glade Assurance","edition":"Developer","features":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	creation, err := RunSalesforceOrgCreate(SalesforceOrgCreateRequest{BundlePath: bundlePath, DevHub: "glade-dev-hub4", Alias: "assurance-sf0", SFBin: "/usr/local/bin/sf", OutputPath: outputPath, validateBundle: func(string) error { return nil }, runner: func(_ context.Context, path string, args ...string) (salesforceCommandOutput, error) {
		if path != "/usr/local/bin/sf" || !containsString(args, "--definition-file") || !containsString(args, "glade-dev-hub4") {
			return salesforceCommandOutput{}, fmt.Errorf("unexpected create invocation %s %v", path, args)
		}
		return salesforceCommandOutput{Stdout: []byte(`{"status":0,"result":{"orgId":"00D000000000001"}}`)}, nil
	}})
	if err != nil {
		t.Fatalf("RunSalesforceOrgCreate: %v", err)
	}
	if creation.OrgID != "00D000000000001" || creation.Alias != "assurance-sf0" || creation.BundleSHA256 != localProofFileSHA256(t, bundlePath) {
		t.Fatalf("creation = %#v", creation)
	}
	if _, err := os.Stat(outputPath); err != nil {
		t.Fatal(err)
	}
}

func TestRunSalesforceOrgCreateRejectsBundleChangedDuringCreation(t *testing.T) {
	root := t.TempDir()
	bundlePath, outputPath := filepath.Join(root, "bundle.json"), filepath.Join(root, "org-create.json")
	if err := os.WriteFile(bundlePath, []byte(`{"bundle":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "corpus-assurance-scratch-def.json"), []byte(`{"orgName":"Glade Assurance","edition":"Developer","features":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	validations := 0
	_, err := RunSalesforceOrgCreate(SalesforceOrgCreateRequest{
		BundlePath: bundlePath, DevHub: "glade-dev-hub4", Alias: "assurance-sf0", SFBin: "/usr/local/bin/sf", OutputPath: outputPath,
		validateBundle: func(string) error {
			validations++
			if validations == 2 {
				return errors.New("bundle changed")
			}
			return nil
		},
		runner: func(_ context.Context, _ string, _ ...string) (salesforceCommandOutput, error) {
			return salesforceCommandOutput{Stdout: []byte(`{"status":0,"result":{"orgId":"00D000000000001"}}`)}, nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "bundle changed during org creation") {
		t.Fatalf("RunSalesforceOrgCreate error = %v", err)
	}
	if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
		t.Fatalf("creation receipt exists after changed bundle: %v", err)
	}
}

func TestRunSalesforceOrgCreateRejectsBundleHashChangedDuringCreation(t *testing.T) {
	root := t.TempDir()
	bundlePath, outputPath := filepath.Join(root, "bundle.json"), filepath.Join(root, "org-create.json")
	if err := os.WriteFile(bundlePath, []byte(`{"bundle":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "corpus-assurance-scratch-def.json"), []byte(`{"orgName":"Glade Assurance","edition":"Developer","features":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := RunSalesforceOrgCreate(SalesforceOrgCreateRequest{
		BundlePath: bundlePath, DevHub: "glade-dev-hub4", Alias: "assurance-sf0", SFBin: "/usr/local/bin/sf", OutputPath: outputPath,
		validateBundle: func(string) error { return nil },
		runner: func(_ context.Context, _ string, _ ...string) (salesforceCommandOutput, error) {
			if err := os.WriteFile(bundlePath, []byte(`{"bundle":false}`), 0o600); err != nil {
				t.Fatal(err)
			}
			return salesforceCommandOutput{Stdout: []byte(`{"status":0,"result":{"orgId":"00D000000000001"}}`)}, nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "staged bundle changed during org creation") {
		t.Fatalf("RunSalesforceOrgCreate error = %v", err)
	}
	if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
		t.Fatalf("creation receipt exists after changed bundle: %v", err)
	}
}

func TestRunSalesforceOrgCreateSealsInvalidatedCleanupAuthorityAfterCreate(t *testing.T) {
	root := t.TempDir()
	bundlePath, outputPath := filepath.Join(root, "bundle.json"), filepath.Join(root, "org-create.json")
	if err := os.WriteFile(bundlePath, []byte(`{"bundle":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "corpus-assurance-scratch-def.json"), []byte(`{"orgName":"Glade Assurance","edition":"Developer","features":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	validations := 0
	_, err := RunSalesforceOrgCreate(SalesforceOrgCreateRequest{
		BundlePath: bundlePath, DevHub: "glade-dev-hub4", Alias: "assurance-sf0", SFBin: "/usr/local/bin/sf", OutputPath: outputPath,
		validateBundle: func(string) error {
			validations++
			if validations == 2 {
				return errors.New("bundle changed")
			}
			return nil
		},
		runner: func(_ context.Context, _ string, _ ...string) (salesforceCommandOutput, error) {
			return salesforceCommandOutput{Stdout: []byte(`{"status":0,"result":{"orgId":"00D000000000001"}}`)}, nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "bundle changed during org creation") {
		t.Fatalf("RunSalesforceOrgCreate error = %v", err)
	}
	invalidated, readErr := readExactJSON[SalesforceOrgCreation](outputPath + ".invalidated")
	if readErr != nil || !invalidated.Invalidated || invalidated.OrgID != "00D000000000001" || invalidated.Alias != "assurance-sf0" {
		t.Fatalf("invalidated creation = %#v, %v", invalidated, readErr)
	}
}

func TestRunSalesforceOrgCleanupOnlyDeletesTheReceiptCreatedOrg(t *testing.T) {
	root := t.TempDir()
	bundlePath, creationPath, preflightPath, outputPath := filepath.Join(root, "bundle.json"), filepath.Join(root, "creation.json"), filepath.Join(root, "preflight.json"), filepath.Join(root, "cleanup.json")
	if err := os.WriteFile(bundlePath, []byte(`{"bundle":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	bundleSHA := localProofFileSHA256(t, bundlePath)
	createArgs := salesforceOrgCreateArgs(filepath.Join(root, "corpus-assurance-scratch-def.json"), "glade-dev-hub4", "assurance-sf0")
	creation := SalesforceOrgCreation{SchemaVersion: 1, BundleSHA256: bundleSHA, DevHub: "glade-dev-hub4", Alias: "assurance-sf0", OrgID: "00D0", Command: salesforceCommandForTest(t, bundlePath, createArgs)}
	if err := WriteNewJSON(creationPath, creation); err != nil {
		t.Fatal(err)
	}
	preflight := salesforcePreflightForTest(t, "assurance-sf0", bundleSHA, bundlePath)
	if err := WriteNewJSON(preflightPath, preflight); err != nil {
		t.Fatal(err)
	}
	cleanup, err := RunSalesforceOrgCleanup(SalesforceOrgCleanupRequest{BundlePath: bundlePath, CreationPath: creationPath, PreflightPath: preflightPath, TargetOrg: "assurance-sf0", DevHub: "glade-dev-hub4", SFBin: "/usr/local/bin/sf", OutputPath: outputPath, validateBundle: func(string) error { return nil }, runner: func(_ context.Context, path string, args ...string) (salesforceCommandOutput, error) {
		if path != "/usr/local/bin/sf" {
			return salesforceCommandOutput{}, fmt.Errorf("unexpected sf path %q", path)
		}
		if len(args) >= 3 && args[0] == "org" && args[1] == "delete" {
			return salesforceCommandOutput{Stdout: []byte(`{"status":0}`)}, nil
		}
		t.Fatal("cleanup used an org-display failure as absence evidence")
		return salesforceCommandOutput{}, nil
	}})
	if err != nil {
		t.Fatalf("RunSalesforceOrgCleanup: %v", err)
	}
	if !cleanup.ResidueAbsent || cleanup.OrgID != creation.OrgID || len(cleanup.Commands) != 1 {
		t.Fatalf("cleanup = %#v", cleanup)
	}
	if _, err := os.Stat(outputPath); err != nil {
		t.Fatal(err)
	}
}

func TestRunSalesforceOrgCleanupAcceptsAnInvalidatedCreationWithoutPreflight(t *testing.T) {
	root := t.TempDir()
	bundlePath, creationPath, outputPath := filepath.Join(root, "bundle.json"), filepath.Join(root, "creation.invalidated.json"), filepath.Join(root, "cleanup.json")
	if err := os.WriteFile(bundlePath, []byte(`{"bundle":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	bundleSHA := localProofFileSHA256(t, bundlePath)
	args := salesforceOrgCreateArgs(filepath.Join(root, "corpus-assurance-scratch-def.json"), "glade-dev-hub4", "assurance-sf0")
	creation := SalesforceOrgCreation{SchemaVersion: 1, BundleSHA256: bundleSHA, DevHub: "glade-dev-hub4", Alias: "assurance-sf0", OrgID: "00D0", Invalidated: true, Command: salesforceCommandForTest(t, bundlePath, args)}
	if err := WriteNewJSON(creationPath, creation); err != nil {
		t.Fatal(err)
	}
	cleanup, err := RunSalesforceOrgCleanup(SalesforceOrgCleanupRequest{BundlePath: bundlePath, CreationPath: creationPath, TargetOrg: "assurance-sf0", DevHub: "glade-dev-hub4", SFBin: "/usr/local/bin/sf", OutputPath: outputPath, validateBundle: func(string) error { return nil }, runner: func(_ context.Context, _ string, args ...string) (salesforceCommandOutput, error) {
		if len(args) >= 2 && args[0] == "org" && args[1] == "delete" {
			return salesforceCommandOutput{}, nil
		}
		return salesforceCommandOutput{ExitCode: 1}, nil
	}})
	if err != nil || !cleanup.ResidueAbsent || cleanup.OrgID != creation.OrgID {
		t.Fatalf("RunSalesforceOrgCleanup = %#v, %v", cleanup, err)
	}
}

func TestNormalizeSalesforceFilterResultsRequiresSealedPlanBundleAndOrgEvidence(t *testing.T) {
	zero := 0
	candidate := RuntimeArtifact{Commit: strings.Repeat("a", 40), OS: "darwin", Arch: "arm64", SHA256: strings.Repeat("b", 64)}
	tools := RuntimeArtifact{Commit: strings.Repeat("c", 40), OS: "darwin", Arch: "amd64", SHA256: strings.Repeat("d", 64)}
	plan := OraclePlan{Candidate: candidate, Tools: tools, Rows: []OraclePlanRow{{SurfaceID: "apex:System.run()", Action: oracleRuntime}, {SurfaceID: "apex:System.compile()", Action: oracleCompile}}}
	bundle := OracleBundle{Candidate: candidate, Tools: tools, ProfileSHA256: strings.Repeat("e", 64), OraclePlanSHA256: strings.Repeat("f", 64), TransportManifestSHA256: strings.Repeat("1", 64), LocalProofSummarySHA256: strings.Repeat("2", 64), FilterSHA256: strings.Repeat("3", 64)}
	bundlePath := "/private/tmp/bundle.json"
	preflight := SalesforceOrgPreflight{SchemaVersion: 1, BundleSHA256: strings.Repeat("4", 64), OrgAlias: "assurance-sf0", OrgID: "00D0", OrgStatus: "Active", Inventory: SalesforceInventory{Counts: map[string]int{}}}
	preflightArgs := [][]string{{"org", "display", "--target-org", preflight.OrgAlias, "--json"}}
	for _, kind := range salesforceInventoryTypes {
		preflightArgs = append(preflightArgs, []string{"data", "query", "--query", "SELECT count() FROM " + kind, "--target-org", preflight.OrgAlias, "--json"})
		preflight.Inventory.Counts[kind] = 0
	}
	for _, args := range preflightArgs {
		preflight.Commands = append(preflight.Commands, salesforceCommandForTest(t, bundlePath, args))
	}
	postflight := preflight
	runtimePassed := true
	filter := salesforceFilterResults{Sealed: true, Orgs: []string{"assurance-sf0"}, Binding: salesforceFilterBinding{ManifestSHA256: bundle.TransportManifestSHA256, ProfileSHA256: bundle.ProfileSHA256, QueueSHA256: bundle.OraclePlanSHA256, SelectorSHA256: bundle.OraclePlanSHA256, SelectorReceiptSHA256: preflight.BundleSHA256, CandidateCommit: candidate.Commit, CandidateSHA256: candidate.SHA256, ToolsCommit: tools.Commit, WorkflowScriptSHA256: bundle.FilterSHA256, LocalSummarySHA256: bundle.LocalProofSummarySHA256}, RemoteCleanup: CleanupReceipt{ResidueAbsent: true}, OrgPostflight: salesforceFilterPostflight{MatchesPreflight: true}, Results: []salesforceFilterFixtureResult{{SurfaceIDs: []string{"apex:System.run()"}, Org: "assurance-sf0", Kind: "exec", ExitCode: &zero, Deployable: true, RuntimePassed: &runtimePassed, RemoteCleanup: CleanupReceipt{ResidueAbsent: true}, OrgCleanup: CleanupReceipt{ResidueAbsent: true}}, {SurfaceIDs: []string{"apex:System.compile()"}, Org: "assurance-sf0", Kind: "check", ExitCode: &zero, Deployable: true, RemoteCleanup: CleanupReceipt{ResidueAbsent: true}, OrgCleanup: CleanupReceipt{ResidueAbsent: true}}}}
	command := CommandResult{Command: []string{"python3", "transport/salesforce-first-filter.py"}, CommandSpecSHA256: strings.Repeat("5", 64), ExitCode: 0, Passed: true, StdoutSHA256: strings.Repeat("6", 64), StderrSHA256: strings.Repeat("7", 64)}
	shard, err := NormalizeSalesforceFilterResults(plan, bundle, bundlePath, "/private/tmp/executor/shard-0", "attempt-shard-0", preflight, postflight, filter, command, 0, 2)
	if err != nil {
		t.Fatalf("NormalizeSalesforceFilterResults: %v", err)
	}
	if shard.Results[0].Kind != oracleRuntime || shard.Results[1].Kind != oracleCompile || !sameInventory(shard.PreInventory, preflight.Inventory) || !sameInventory(shard.PostInventory, postflight.Inventory) {
		t.Fatalf("shard = %#v", shard)
	}
	filter.Results[0].RuntimePassed = nil
	if _, err := NormalizeSalesforceFilterResults(plan, bundle, bundlePath, "/private/tmp/executor/shard-0", "attempt-shard-0", preflight, postflight, filter, command, 0, 2); err == nil {
		t.Fatal("accepted a runtime result without a successful runtime observation")
	}
	preflight.Commands[0].Command = []string{"/usr/local/bin/sf", "org", "list", "--json"}
	if _, err := NormalizeSalesforceFilterResults(plan, bundle, bundlePath, "/private/tmp/executor/shard-0", "attempt-shard-0", preflight, postflight, filter, command, 0, 2); err == nil {
		t.Fatal("accepted a preflight receipt without the exact org-display command")
	}
}

func TestSalesforceFilterArgsDeriveEveryIdentityFromTheSealedBundle(t *testing.T) {
	bundle := OracleBundle{Candidate: RuntimeArtifact{Commit: strings.Repeat("a", 40), OS: "darwin", Arch: "arm64", SHA256: strings.Repeat("b", 64)}, Tools: RuntimeArtifact{Commit: strings.Repeat("c", 40), OS: "darwin", Arch: "amd64", SHA256: strings.Repeat("d", 64)}, OraclePlanSHA256: strings.Repeat("e", 64), TransportManifestSHA256: strings.Repeat("f", 64), LocalProofSummarySHA256: strings.Repeat("1", 64), Fixtures: []OracleBundleFixture{{ID: "system"}}}
	args, err := salesforceFilterArgs("/private/tmp/assurance/transport/salesforce-first-filter.py", "/private/tmp/assurance/bundle", "/private/tmp/assurance/executor/shard-0", "attempt-shard-0", "assurance-sf0", bundle, strings.Repeat("2", 64), 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--candidate-commit", bundle.Candidate.Commit, "--candidate-sha256", bundle.Candidate.SHA256, "--tools-commit", bundle.Tools.Commit, "--queue-sha256", bundle.OraclePlanSHA256, "--selector-receipt-sha256", strings.Repeat("2", 64), "--manifest-index-modulus", "2", "--manifest-index-remainder", "0", "--remote-sf-bin", "/usr/local/bin/sf", "--runtime"} {
		if !containsString(args, want) {
			t.Fatalf("filter args omit %q: %v", want, args)
		}
	}
}

func TestCreateSalesforceDispatchRejectsExecutorOutsideSealedAttempt(t *testing.T) {
	inputs := oracleBundleTestInputsForLocalProof(t)
	writeSealedReleaseValidation(t, inputs.releasePath, inputs.attemptPath, inputs.plan.Candidate, inputs.plan.Tools)
	outputRoot := filepath.Join(t.TempDir(), "razor")
	bundle, err := BuildOracleBundle(inputs.request(outputRoot))
	if err != nil {
		t.Fatal(err)
	}
	bundlePath := filepath.Join(outputRoot, "bundle", "bundle.json")
	_, err = CreateSalesforceDispatch(SalesforceDispatchRequest{
		BundlePath: bundlePath, OrgAlias: "assurance-sf0",
		ExecutorRoot: filepath.Join(filepath.Dir(outputRoot), "executor", "..", "outside"),
		RunID:        "assurance-" + bundle.AttemptSHA256[:16] + "-shard-0",
		ShardIndex:   0, ShardCount: 2, OutputPath: filepath.Join(t.TempDir(), "DISPATCH.json"),
	})
	if err == nil {
		t.Fatal("accepted an executor root outside the sealed attempt")
	}
}

func TestRunSalesforceShardSealsFilterAndFreshPostflight(t *testing.T) {
	root, attemptRoot := t.TempDir(), ""
	attemptRoot = filepath.Join(root, "attempt")
	razorRoot := filepath.Join(attemptRoot, "razor")
	bundleRoot, executorRoot := filepath.Join(razorRoot, "bundle"), filepath.Join(attemptRoot, "executor", "shard-0")
	for _, path := range []string{bundleRoot, filepath.Join(attemptRoot, "transport"), executorRoot} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	candidate := RuntimeArtifact{Commit: strings.Repeat("a", 40), OS: "darwin", Arch: "arm64", SHA256: strings.Repeat("b", 64)}
	tools := RuntimeArtifact{Commit: strings.Repeat("c", 40), OS: "darwin", Arch: "amd64", SHA256: strings.Repeat("d", 64)}
	planPath, bundlePath := filepath.Join(bundleRoot, "ORACLE_PLAN.json"), filepath.Join(bundleRoot, "bundle.json")
	plan := OraclePlan{Candidate: candidate, Tools: tools, Rows: []OraclePlanRow{{SurfaceID: "apex:System.run()", Action: oracleRuntime}}}
	if err := WriteNewJSON(planPath, plan); err != nil {
		t.Fatal(err)
	}
	bundle := OracleBundle{SchemaVersion: 1, Candidate: candidate, Tools: tools, ProfileSHA256: strings.Repeat("e", 64), OraclePlanSHA256: localProofFileSHA256(t, planPath), AttemptSHA256: strings.Repeat("a", 64), TransportManifestSHA256: strings.Repeat("f", 64), LocalProofSummarySHA256: strings.Repeat("1", 64), FilterSHA256: strings.Repeat("2", 64), Fixtures: []OracleBundleFixture{{ID: "system"}}}
	if err := WriteNewJSON(bundlePath, bundle); err != nil {
		t.Fatal(err)
	}
	bundleSHA := localProofFileSHA256(t, bundlePath)
	preflightPath := filepath.Join(root, "preflight.json")
	preflight := salesforcePreflightForTest(t, "assurance-sf0", bundleSHA, bundlePath)
	if err := WriteNewJSON(preflightPath, preflight); err != nil {
		t.Fatal(err)
	}
	filterRunner := func(ctx context.Context, path string, args ...string) (salesforceCommandOutput, error) {
		execution, ok := ctx.Value(salesforceExecutionKey{}).(salesforceExecution)
		if !ok || execution.workingDirectory != filepath.Dir(bundlePath) || !reflect.DeepEqual(execution.environment, mustFixedSalesforceEnvironment(t)) {
			return salesforceCommandOutput{}, fmt.Errorf("unsealed Salesforce filter execution")
		}
		if path != "python3" {
			return salesforceCommandOutput{}, fmt.Errorf("unexpected filter runner %q", path)
		}
		out := ""
		for index, arg := range args {
			if arg == "--out" && index+1 < len(args) {
				out = args[index+1]
			}
		}
		if out == "" {
			return salesforceCommandOutput{}, fmt.Errorf("filter output is missing")
		}
		if err := os.MkdirAll(out, 0o700); err != nil {
			return salesforceCommandOutput{}, err
		}
		zero, runtimePassed := 0, true
		filter := salesforceFilterResults{Sealed: true, Orgs: []string{"assurance-sf0"}, Binding: salesforceFilterBinding{ManifestSHA256: bundle.TransportManifestSHA256, ProfileSHA256: bundle.ProfileSHA256, QueueSHA256: bundle.OraclePlanSHA256, SelectorSHA256: bundle.OraclePlanSHA256, SelectorReceiptSHA256: bundleSHA, CandidateCommit: candidate.Commit, CandidateSHA256: candidate.SHA256, ToolsCommit: tools.Commit, WorkflowScriptSHA256: bundle.FilterSHA256, LocalSummarySHA256: bundle.LocalProofSummarySHA256}, RemoteCleanup: CleanupReceipt{ResidueAbsent: true}, OrgPostflight: salesforceFilterPostflight{MatchesPreflight: true}, Results: []salesforceFilterFixtureResult{{SurfaceIDs: []string{"apex:System.run()"}, Org: "assurance-sf0", Kind: "exec", ExitCode: &zero, Deployable: true, RuntimePassed: &runtimePassed, RemoteCleanup: CleanupReceipt{ResidueAbsent: true}, OrgCleanup: CleanupReceipt{ResidueAbsent: true}}}}
		data, err := json.Marshal(filter)
		if err != nil {
			return salesforceCommandOutput{}, err
		}
		if err := os.WriteFile(filepath.Join(out, "results.json"), data, 0o600); err != nil {
			return salesforceCommandOutput{}, err
		}
		return salesforceCommandOutput{Stdout: []byte(`{"selectedRows":1}`)}, nil
	}
	sfRunner := func(_ context.Context, path string, args ...string) (salesforceCommandOutput, error) {
		if path != "/usr/local/bin/sf" {
			return salesforceCommandOutput{}, fmt.Errorf("unexpected sf path %q", path)
		}
		if len(args) > 1 && args[0] == "org" {
			return salesforceCommandOutput{Stdout: []byte(`{"status":0,"result":{"id":"00D0","status":"Active"}}`)}, nil
		}
		return salesforceCommandOutput{Stdout: []byte(`{"status":0,"result":{"totalSize":0}}`)}, nil
	}
	dispatchPath := filepath.Join(root, "SALESFORCE_DISPATCH.json")
	runID := "assurance-" + bundle.AttemptSHA256[:16] + "-shard-0"
	filterPath := filepath.Join(razorRoot, "transport", "salesforce-first-filter.py")
	args, err := salesforceFilterArgs(filterPath, filepath.Dir(bundlePath), executorRoot, runID, "assurance-sf0", bundle, bundleSHA, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	dispatch := SalesforceDispatch{SchemaVersion: 1, BundleSHA256: bundleSHA, OrgAlias: "assurance-sf0", ExecutorRoot: executorRoot, RunID: runID, ShardIndex: 0, ShardCount: 2, FilterCommandSpecSHA256: salesforceFilterCommandSpecSHA256("python3", args, filepath.Dir(bundlePath), mustFixedSalesforceEnvironment(t))}
	if err := WriteNewJSON(dispatchPath, dispatch); err != nil {
		t.Fatal(err)
	}
	shardPath := filepath.Join(root, "SALESFORCE_SHARD.json")
	shard, err := RunSalesforceShard(SalesforceShardRequest{BundlePath: bundlePath, DispatchPath: dispatchPath, PreflightPath: preflightPath, TargetOrg: "assurance-sf0", SFBin: "/usr/local/bin/sf", OutputPath: shardPath, validateBundle: func(string) error { return nil }, filterRunner: filterRunner, sfRunner: sfRunner})
	if err != nil {
		t.Fatalf("RunSalesforceShard: %v", err)
	}
	if len(shard.Results) != 1 || shard.Results[0].Kind != oracleRuntime || !zeroInventory(shard.PostInventory) {
		t.Fatalf("shard = %#v", shard)
	}
	if _, err := os.Stat(shardPath); err != nil {
		t.Fatal(err)
	}
}

func salesforcePreflightForTest(t *testing.T, alias, bundleSHA, bundlePath string) SalesforceOrgPreflight {
	t.Helper()
	preflight := SalesforceOrgPreflight{SchemaVersion: 1, BundleSHA256: bundleSHA, OrgAlias: alias, OrgID: "00D0", OrgStatus: "Active", Inventory: SalesforceInventory{Counts: map[string]int{}}}
	for index, args := range salesforcePreflightArgs(alias) {
		if index > 0 {
			preflight.Inventory.Counts[salesforceInventoryTypes[index-1]] = 0
		}
		preflight.Commands = append(preflight.Commands, salesforceCommandForTest(t, bundlePath, args))
	}
	return preflight
}

func salesforceCommandForTest(t *testing.T, bundlePath string, args []string) CommandResult {
	t.Helper()
	environment, err := fixedSalesforceEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	executableSHA256 := strings.Repeat("a", 64)
	return CommandResult{Command: append([]string{"/usr/local/bin/sf"}, args...), WorkingDirectory: filepath.Dir(bundlePath), Environment: environment, ExecutableSHA256: executableSHA256, ExecutableAfterSHA256: executableSHA256, CommandSpecSHA256: salesforceCommandSpecSHA256("/usr/local/bin/sf", args, filepath.Dir(bundlePath), environment, executableSHA256, executableSHA256), ExitCode: 0, Passed: true, StdoutSHA256: strings.Repeat("3", 64), StderrSHA256: strings.Repeat("4", 64)}
}

func salesforceShardFilesForTest(t *testing.T, shardPath, bundlePath, bundleSHA, alias, orgID string) SalesforceShardFiles {
	t.Helper()
	root := t.TempDir()
	dispatchPath, creationPath, cleanupPath := filepath.Join(root, "DISPATCH.json"), filepath.Join(root, "ORG_CREATION.json"), filepath.Join(root, "ORG_CLEANUP.json")
	shard, _, err := readExactJSONBytes[SalesforceShard](shardPath)
	if err != nil {
		t.Fatal(err)
	}
	dispatch, err := CreateSalesforceDispatch(SalesforceDispatchRequest{BundlePath: bundlePath, OrgAlias: alias, ExecutorRoot: shard.ExecutorRoot, RunID: shard.RunID, ShardIndex: shard.ShardIndex, ShardCount: shard.ShardCount, OutputPath: dispatchPath})
	if err != nil {
		t.Fatal(err)
	}
	shard.DispatchSHA256 = localProofFileSHA256(t, dispatchPath)
	if data, err := json.Marshal(shard); err != nil || os.WriteFile(shardPath, append(data, '\n'), 0o600) != nil {
		t.Fatal(err)
	}
	creation := SalesforceOrgCreation{SchemaVersion: 1, BundleSHA256: bundleSHA, DevHub: "glade-dev-hub4", Alias: alias, OrgID: orgID, Command: salesforceCommandForTest(t, bundlePath, salesforceOrgCreateArgs(filepath.Join(filepath.Dir(bundlePath), "corpus-assurance-scratch-def.json"), "glade-dev-hub4", alias))}
	if err := WriteNewJSON(creationPath, creation); err != nil {
		t.Fatal(err)
	}
	deleted := salesforceCommandForTest(t, bundlePath, []string{"org", "delete", "scratch", "--target-org", alias, "--no-prompt", "--json"})
	cleanup := SalesforceOrgCleanup{SchemaVersion: 1, BundleSHA256: bundleSHA, DevHub: "glade-dev-hub4", OrgAlias: alias, OrgID: orgID, Commands: []CommandResult{deleted}, ResidueAbsent: true}
	if err := WriteNewJSON(cleanupPath, cleanup); err != nil {
		t.Fatal(err)
	}
	_ = dispatch
	return SalesforceShardFiles{ShardPath: shardPath, DispatchPath: dispatchPath, CreationPath: creationPath, CleanupPath: cleanupPath}
}

func mustFixedSalesforceEnvironment(t *testing.T) []string {
	t.Helper()
	environment, err := fixedSalesforceEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	return environment
}

func shardLifecycleForTest(alias, id, bundleSHA string) SalesforceOrgPreflight {
	return SalesforceOrgPreflight{SchemaVersion: 1, BundleSHA256: bundleSHA, OrgAlias: alias, OrgID: id, OrgStatus: "Active", Inventory: SalesforceInventory{Counts: map[string]int{}}, Commands: make([]CommandResult, len(salesforceInventoryTypes)+1)}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
