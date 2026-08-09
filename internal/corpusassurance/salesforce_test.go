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

func TestValidSalesforceOrgPreflightRejectsUnsealedCLIExecution(t *testing.T) {
	root := t.TempDir()
	bundlePath := filepath.Join(root, "bundle.json")
	if err := os.WriteFile(bundlePath, []byte(`{"bundle":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	preflight := salesforcePreflightForTest(t, "assurance-sf0", localProofFileSHA256(t, bundlePath), bundlePath)
	preflight.Commands[0].ExecutableSHA256 = ""
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
		return salesforceCommandOutput{ExitCode: 1, Stderr: []byte("not found")}, nil
	}})
	if err != nil {
		t.Fatalf("RunSalesforceOrgCleanup: %v", err)
	}
	if !cleanup.ResidueAbsent || cleanup.OrgID != creation.OrgID || len(cleanup.Commands) != 2 {
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
	filter := salesforceFilterResults{Sealed: true, Orgs: []string{"assurance-sf0"}, Binding: salesforceFilterBinding{ManifestSHA256: bundle.TransportManifestSHA256, ProfileSHA256: bundle.ProfileSHA256, QueueSHA256: bundle.OraclePlanSHA256, SelectorSHA256: bundle.OraclePlanSHA256, SelectorReceiptSHA256: preflight.BundleSHA256, CandidateCommit: candidate.Commit, CandidateSHA256: candidate.SHA256, ToolsCommit: tools.Commit, WorkflowScriptSHA256: bundle.FilterSHA256, LocalSummarySHA256: bundle.LocalProofSummarySHA256}, RemoteCleanup: CleanupReceipt{ResidueAbsent: true}, OrgPostflight: salesforceFilterPostflight{MatchesPreflight: true}, Results: []salesforceFilterFixtureResult{{SurfaceIDs: []string{"apex:System.run()"}, Org: "assurance-sf0", Kind: "exec", ExitCode: &zero, Deployable: true, RemoteCleanup: CleanupReceipt{ResidueAbsent: true}, OrgCleanup: CleanupReceipt{ResidueAbsent: true}}, {SurfaceIDs: []string{"apex:System.compile()"}, Org: "assurance-sf0", Kind: "check", ExitCode: &zero, Deployable: true, RemoteCleanup: CleanupReceipt{ResidueAbsent: true}, OrgCleanup: CleanupReceipt{ResidueAbsent: true}}}}
	command := CommandResult{Command: []string{"python3", "transport/salesforce-first-filter.py"}, CommandSpecSHA256: strings.Repeat("5", 64), ExitCode: 0, Passed: true, StdoutSHA256: strings.Repeat("6", 64), StderrSHA256: strings.Repeat("7", 64)}
	shard, err := NormalizeSalesforceFilterResults(plan, bundle, bundlePath, preflight, postflight, filter, command, 0, 2)
	if err != nil {
		t.Fatalf("NormalizeSalesforceFilterResults: %v", err)
	}
	if shard.Results[0].Kind != oracleRuntime || shard.Results[1].Kind != oracleCompile || !sameInventory(shard.PreInventory, preflight.Inventory) || !sameInventory(shard.PostInventory, postflight.Inventory) {
		t.Fatalf("shard = %#v", shard)
	}
	preflight.Commands[0].Command = []string{"/usr/local/bin/sf", "org", "list", "--json"}
	if _, err := NormalizeSalesforceFilterResults(plan, bundle, bundlePath, preflight, postflight, filter, command, 0, 2); err == nil {
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

func TestRunSalesforceShardSealsFilterAndFreshPostflight(t *testing.T) {
	root, attemptRoot := t.TempDir(), ""
	attemptRoot = filepath.Join(root, "attempt")
	bundleRoot, executorRoot := filepath.Join(attemptRoot, "bundle"), filepath.Join(attemptRoot, "executor", "shard-0")
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
	bundle := OracleBundle{SchemaVersion: 1, Candidate: candidate, Tools: tools, ProfileSHA256: strings.Repeat("e", 64), OraclePlanSHA256: localProofFileSHA256(t, planPath), TransportManifestSHA256: strings.Repeat("f", 64), LocalProofSummarySHA256: strings.Repeat("1", 64), FilterSHA256: strings.Repeat("2", 64), Fixtures: []OracleBundleFixture{{ID: "system"}}}
	if err := WriteNewJSON(bundlePath, bundle); err != nil {
		t.Fatal(err)
	}
	bundleSHA := localProofFileSHA256(t, bundlePath)
	preflightPath := filepath.Join(root, "preflight.json")
	preflight := salesforcePreflightForTest(t, "assurance-sf0", bundleSHA, bundlePath)
	if err := WriteNewJSON(preflightPath, preflight); err != nil {
		t.Fatal(err)
	}
	filterRunner := func(_ context.Context, path string, args ...string) (salesforceCommandOutput, error) {
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
		zero := 0
		filter := salesforceFilterResults{Sealed: true, Orgs: []string{"assurance-sf0"}, Binding: salesforceFilterBinding{ManifestSHA256: bundle.TransportManifestSHA256, ProfileSHA256: bundle.ProfileSHA256, QueueSHA256: bundle.OraclePlanSHA256, SelectorSHA256: bundle.OraclePlanSHA256, SelectorReceiptSHA256: bundleSHA, CandidateCommit: candidate.Commit, CandidateSHA256: candidate.SHA256, ToolsCommit: tools.Commit, WorkflowScriptSHA256: bundle.FilterSHA256, LocalSummarySHA256: bundle.LocalProofSummarySHA256}, RemoteCleanup: CleanupReceipt{ResidueAbsent: true}, OrgPostflight: salesforceFilterPostflight{MatchesPreflight: true}, Results: []salesforceFilterFixtureResult{{SurfaceIDs: []string{"apex:System.run()"}, Org: "assurance-sf0", Kind: "exec", ExitCode: &zero, Deployable: true, RemoteCleanup: CleanupReceipt{ResidueAbsent: true}, OrgCleanup: CleanupReceipt{ResidueAbsent: true}}}}
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
	shardPath := filepath.Join(root, "SALESFORCE_SHARD.json")
	shard, err := RunSalesforceShard(SalesforceShardRequest{BundlePath: bundlePath, PreflightPath: preflightPath, TargetOrg: "assurance-sf0", SFBin: "/usr/local/bin/sf", ExecutorRoot: executorRoot, RunID: "attempt-shard-0", ShardIndex: 0, ShardCount: 2, OutputPath: shardPath, validateBundle: func(string) error { return nil }, filterRunner: filterRunner, sfRunner: sfRunner})
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
	return CommandResult{Command: append([]string{"/usr/local/bin/sf"}, args...), WorkingDirectory: filepath.Dir(bundlePath), Environment: environment, ExecutableSHA256: executableSHA256, CommandSpecSHA256: salesforceCommandSpecSHA256("/usr/local/bin/sf", args, filepath.Dir(bundlePath), environment, executableSHA256), ExitCode: 0, Passed: true, StdoutSHA256: strings.Repeat("3", 64), StderrSHA256: strings.Repeat("4", 64)}
}

func mustFixedSalesforceEnvironment(t *testing.T) []string {
	t.Helper()
	environment, err := fixedSalesforceEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	return environment
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
