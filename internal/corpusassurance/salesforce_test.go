package corpusassurance

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseSalesforceDevHubDisplayUsesConnectedStatus(t *testing.T) {
	id, status, username, err := parseSalesforceOrgDisplay([]byte(`{"status":0,"result":{"id":"00D0","connectedStatus":"Connected","username":"sealed-dev-hub@example.invalid"}}`))
	if err != nil || id != "00D0" || status != "Connected" || username != "sealed-dev-hub@example.invalid" {
		t.Fatalf("parseSalesforceOrgDisplay = %q, %q, %q, %v", id, status, username, err)
	}
}

func TestSalesforceBaselineInventoryRequiresOneFieldSet(t *testing.T) {
	inventory := SalesforceInventory{Counts: map[string]int{}}
	for _, kind := range salesforceInventoryTypes {
		inventory.Counts[kind] = 0
	}
	inventory.Counts["FieldSet"] = 1
	if !baselineSalesforceInventory(inventory) {
		t.Fatal("fresh scratch inventory rejected")
	}
	inventory.Counts["FieldSet"] = 2
	if baselineSalesforceInventory(inventory) {
		t.Fatal("non-baseline FieldSet inventory accepted")
	}
}

func TestSalesforceBaselineInventoryRequiresExactKeys(t *testing.T) {
	inventory := salesforceBaselineInventoryForTest()
	delete(inventory.Counts, "ApexClass")
	inventory.Counts["Injected"] = 0
	if baselineSalesforceInventory(inventory) {
		t.Fatal("baseline inventory accepted substituted key")
	}
}

func TestSameInventoryRequiresExactKeys(t *testing.T) {
	one, two := salesforceBaselineInventoryForTest(), salesforceBaselineInventoryForTest()
	delete(two.Counts, "ApexClass")
	two.Counts["Injected"] = 0
	if sameInventory(one, two) {
		t.Fatal("inventory comparison accepted substituted key")
	}
}

func TestParseSalesforceCountRequiresResultAndTotalSize(t *testing.T) {
	for _, raw := range [][]byte{[]byte(`{}`), []byte(`{"status":0}`), []byte(`{"status":0,"result":{}}`)} {
		if _, err := parseSalesforceCount(raw); err == nil {
			t.Fatalf("parseSalesforceCount accepted %s", raw)
		}
	}
}

func TestValidSalesforceOrgPreflightRejectsMissingCount(t *testing.T) {
	root := t.TempDir()
	bundlePath := filepath.Join(root, "bundle.json")
	if err := os.WriteFile(bundlePath, []byte(`{"bundle":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	preflight := salesforcePreflightForTest(t, "assurance-sf0", localProofFileSHA256(t, bundlePath), bundlePath)
	preflight.Commands[1].Output.Stdout = []byte(`{}`)
	preflight.Commands[1].StdoutSHA256 = replayBytesSHA256(preflight.Commands[1].Output.Stdout)
	if validSalesforceOrgPreflight(preflight, preflight.BundleSHA256, bundlePath) {
		t.Fatal("accepted preflight with a missing retained count")
	}
}

func TestValidateSalesforceShardsRequiresCleanDisjointCompleteEvidence(t *testing.T) {
	candidate := RuntimeArtifact{Commit: strings.Repeat("a", 40), OS: "darwin", Arch: "arm64", SHA256: strings.Repeat("b", 64)}
	tools := RuntimeArtifact{Commit: strings.Repeat("c", 40), OS: "darwin", Arch: "amd64", SHA256: strings.Repeat("d", 64)}
	bindings := SalesforceBindings{OraclePlanSHA256: strings.Repeat("e", 64), BundleSHA256: strings.Repeat("f", 64), FilterSHA256: strings.Repeat("1", 64), FilterCommandSpecSHA256: strings.Repeat("2", 64)}
	command := CommandResult{Command: []string{"python3", "transport/salesforce-first-filter.py"}, CommandSpecSHA256: bindings.FilterCommandSpecSHA256, ExitCode: 0, Passed: true, StdoutSHA256: strings.Repeat("3", 64), StderrSHA256: strings.Repeat("4", 64)}
	preflight0 := shardLifecycleForTest("assurance-sf0", "00D0", bindings.BundleSHA256)
	preflight1 := shardLifecycleForTest("assurance-sf1", "00D1", bindings.BundleSHA256)
	shards := []SalesforceShard{
		{Bindings: bindings, Candidate: candidate, Tools: tools, ShardIndex: 0, ShardCount: 2, OrgAlias: "assurance-sf0", OrgID: "00D0", OrgStatus: "Active", Preflight: preflight0, PreInventory: salesforceBaselineInventoryForTest(), Commands: []CommandResult{command}, Postflight: preflight0, PostInventory: salesforceBaselineInventoryForTest(), Results: []SalesforceSurfaceResult{{SurfaceID: "apex:System.run()", Kind: "runtime", Passed: true}}, Cleanup: CleanupReceipt{ResidueAbsent: true}},
		{Bindings: bindings, Candidate: candidate, Tools: tools, ShardIndex: 1, ShardCount: 2, OrgAlias: "assurance-sf1", OrgID: "00D1", OrgStatus: "Active", Preflight: preflight1, PreInventory: salesforceBaselineInventoryForTest(), Commands: []CommandResult{command}, Postflight: preflight1, PostInventory: salesforceBaselineInventoryForTest(), Results: []SalesforceSurfaceResult{{SurfaceID: "apex:System.compile()", Kind: "compile", Passed: true}}, Cleanup: CleanupReceipt{ResidueAbsent: true}},
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

func TestValidSalesforceOrgPreflightRequiresRetainedRawOutput(t *testing.T) {
	root := t.TempDir()
	bundlePath := filepath.Join(root, "bundle.json")
	if err := os.WriteFile(bundlePath, []byte(`{"bundle":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	preflight := salesforcePreflightForTest(t, "assurance-sf0", localProofFileSHA256(t, bundlePath), bundlePath)
	output := reflect.ValueOf(&preflight.Commands[0]).Elem().FieldByName("Output")
	if !output.IsValid() {
		t.Fatal("CommandResult does not retain raw command output")
	}
	output.Set(reflect.Zero(output.Type()))
	if validSalesforceOrgPreflight(preflight, preflight.BundleSHA256, bundlePath) {
		t.Fatal("accepted Salesforce preflight without retained raw output")
	}
}

func TestSalesforceDeployObservationRequiresMaterializedComponent(t *testing.T) {
	project := "filter/projects/fixture"
	files := map[string][]byte{project + "/force-app/main/default/classes/Fixture.cls": []byte("public class Fixture {}")}
	matched := []byte(`{"status":0,"result":{"status":"Succeeded","details":{"componentSuccesses":[{"fileName":"classes/Fixture.cls"}],"componentFailures":[]}}}`)
	if !validSalesforceDeployObservationForProject(matched, files, project) {
		t.Fatal("rejected a successful deployment of a materialized fixture component")
	}
	other := []byte(`{"status":0,"result":{"status":"Succeeded","details":{"componentSuccesses":[{"fileName":"classes/Other.cls"}],"componentFailures":[]}}}`)
	if validSalesforceDeployObservationForProject(other, files, project) {
		t.Fatal("accepted a successful deployment of a component absent from the sealed fixture project")
	}
}

func TestValidSalesforceInvocationRejectsTestWithoutRuntimeCommand(t *testing.T) {
	project, org := "/private/tmp/worker/projects/fixture", "assurance-sf0@example.invalid"
	invocation := salesforceInvocationForTest(project, org, "test")
	if validSalesforceInvocation(invocation, project, org, "test", []string{"FixtureTest"}, "", "") {
		t.Fatal("accepted a test receipt without its runtime-test command")
	}
}

func TestValidateSalesforceShardsRejectsReusedOrgAlias(t *testing.T) {
	candidate := RuntimeArtifact{Commit: strings.Repeat("a", 40), OS: "darwin", Arch: "arm64", SHA256: strings.Repeat("b", 64)}
	tools := RuntimeArtifact{Commit: strings.Repeat("c", 40), OS: "darwin", Arch: "amd64", SHA256: strings.Repeat("d", 64)}
	bindings := SalesforceBindings{OraclePlanSHA256: strings.Repeat("e", 64), BundleSHA256: strings.Repeat("f", 64), FilterSHA256: strings.Repeat("1", 64), FilterCommandSpecSHA256: strings.Repeat("2", 64)}
	command := CommandResult{Command: []string{"python3", "transport/salesforce-first-filter.py"}, CommandSpecSHA256: bindings.FilterCommandSpecSHA256, ExitCode: 0, Passed: true, StdoutSHA256: strings.Repeat("3", 64), StderrSHA256: strings.Repeat("4", 64)}
	shards := []SalesforceShard{
		{Bindings: bindings, Candidate: candidate, Tools: tools, ShardIndex: 0, ShardCount: 2, OrgAlias: "assurance-sf", OrgID: "00D0", OrgStatus: "Active", PreInventory: salesforceBaselineInventoryForTest(), Commands: []CommandResult{command}, PostInventory: salesforceBaselineInventoryForTest(), Results: []SalesforceSurfaceResult{{SurfaceID: "apex:System.run()", Kind: oracleRuntime, Passed: true}}, Cleanup: CleanupReceipt{ResidueAbsent: true}},
		{Bindings: bindings, Candidate: candidate, Tools: tools, ShardIndex: 1, ShardCount: 2, OrgAlias: "assurance-sf", OrgID: "00D1", OrgStatus: "Active", PreInventory: salesforceBaselineInventoryForTest(), Commands: []CommandResult{command}, PostInventory: salesforceBaselineInventoryForTest(), Results: []SalesforceSurfaceResult{{SurfaceID: "apex:System.compile()", Kind: oracleCompile, Passed: true}}, Cleanup: CleanupReceipt{ResidueAbsent: true}},
	}
	if err := ValidateSalesforceShards(shards, []string{"apex:System.run()", "apex:System.compile()"}); err == nil {
		t.Fatal("accepted shards that reuse a scratch-org alias")
	}
}

func TestValidateSalesforceShardFilesDerivesRequiredSurfacesFromTheSealedPlan(t *testing.T) {
	inputs := oracleBundleTestInputsForLocalProof(t)
	writeSealedReleaseValidation(t, inputs, inputs.attemptPath)
	outputRoot := filepath.Join(t.TempDir(), "salesforce-worker")
	bundle, err := BuildOracleBundle(inputs.request(outputRoot))
	if err != nil {
		t.Fatal(err)
	}
	bundlePath, planPath, shardPath := filepath.Join(outputRoot, "bundle", "bundle.json"), filepath.Join(outputRoot, "bundle", "ORACLE_PLAN.json"), filepath.Join(t.TempDir(), "shard.json")
	bundleSHA := localProofFileSHA256(t, bundlePath)
	attemptRoot, err := filepath.EvalSymlinks(filepath.Dir(outputRoot))
	if err != nil {
		t.Fatal(err)
	}
	executorRoot, runID, alias := filepath.Join(attemptRoot, "executor", "shard-0"), "assurance-"+bundle.AttemptSHA256[:16]+"-shard-0", "assurance-sf0"
	args, err := salesforceFilterArgs(sealedSalesforceFilterScriptPath(executorRoot), filepath.Dir(bundlePath), executorRoot, runID, alias, bundle, bundleSHA, 0, 1)
	if err == nil {
		t.Fatal("salesforceFilterArgs accepted an invalid shard count")
	}
	args, err = salesforceFilterArgs(sealedSalesforceFilterScriptPath(executorRoot), filepath.Dir(bundlePath), executorRoot, runID, alias, bundle, bundleSHA, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	filterSource, err := os.ReadFile(filepath.Join(outputRoot, "transport", "salesforce-first-filter.py"))
	if err != nil {
		t.Fatal(err)
	}
	args, err = sealedSalesforceFilterInvocationArgs(sealedSalesforceFilterScriptPath(executorRoot), filterSource, args)
	if err != nil {
		t.Fatal(err)
	}
	environment := mustFixedSalesforceEnvironment(t)
	pythonSHA := mustSealedPythonSHA(t)
	command := salesforceFilterCommandForTest(args, bundlePath, environment, pythonSHA)
	bindings := SalesforceBindings{OraclePlanSHA256: bundle.OraclePlanSHA256, BundleSHA256: bundleSHA, FilterSHA256: bundle.FilterSHA256, FilterCommandSpecSHA256: command.CommandSpecSHA256}
	lifecycle := salesforcePreflightForTest(t, alias, bundleSHA, bundlePath)
	shard := SalesforceShard{Bindings: bindings, Candidate: bundle.Candidate, Tools: bundle.Tools, ExecutorRoot: executorRoot, RunID: runID, ShardIndex: 0, ShardCount: 2, OrgAlias: alias, OrgID: lifecycle.OrgID, OrgStatus: "Active", Preflight: lifecycle, PreInventory: salesforceBaselineInventoryForTest(), Commands: []CommandResult{command}, Postflight: lifecycle, PostInventory: salesforceBaselineInventoryForTest(), Results: []SalesforceSurfaceResult{{SurfaceID: "apex:Runtime.run", Kind: oracleRuntime, Passed: true}}, Cleanup: CleanupReceipt{ResidueAbsent: true}}
	shard1Path := filepath.Join(t.TempDir(), "shard-1.json")
	shard1Alias, shard1Executor, shard1RunID := "assurance-sf1", filepath.Join(attemptRoot, "executor", "shard-1"), "assurance-"+bundle.AttemptSHA256[:16]+"-shard-1"
	shard1Args, err := salesforceFilterArgs(sealedSalesforceFilterScriptPath(shard1Executor), filepath.Dir(bundlePath), shard1Executor, shard1RunID, shard1Alias, bundle, bundleSHA, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	shard1Args, err = sealedSalesforceFilterInvocationArgs(sealedSalesforceFilterScriptPath(shard1Executor), filterSource, shard1Args)
	if err != nil {
		t.Fatal(err)
	}
	shard1Command := salesforceFilterCommandForTest(shard1Args, bundlePath, environment, pythonSHA)
	shard1Lifecycle := salesforcePreflightForTest(t, shard1Alias, bundleSHA, bundlePath)
	shard1Lifecycle.OrgID = "00D1"
	shard1Lifecycle.Commands[0].Output.Stdout = []byte(`{"status":0,"result":{"id":"00D1","status":"Active","username":"assurance-sf1@example.invalid"}}`)
	shard1Lifecycle.Commands[0].StdoutSHA256 = replayBytesSHA256(shard1Lifecycle.Commands[0].Output.Stdout)
	shard1 := SalesforceShard{Bindings: SalesforceBindings{OraclePlanSHA256: bundle.OraclePlanSHA256, BundleSHA256: bundleSHA, FilterSHA256: bundle.FilterSHA256, FilterCommandSpecSHA256: shard1Command.CommandSpecSHA256}, Candidate: bundle.Candidate, Tools: bundle.Tools, ExecutorRoot: shard1Executor, RunID: shard1RunID, ShardIndex: 1, ShardCount: 2, OrgAlias: shard1Alias, OrgID: "00D1", OrgStatus: "Active", Preflight: shard1Lifecycle, PreInventory: salesforceBaselineInventoryForTest(), Commands: []CommandResult{shard1Command}, Postflight: shard1Lifecycle, PostInventory: salesforceBaselineInventoryForTest(), Cleanup: CleanupReceipt{ResidueAbsent: true}}
	if err := WriteNewJSON(shardPath, shard); err != nil {
		t.Fatal(err)
	}
	if err := WriteNewJSON(shard1Path, shard1); err != nil {
		t.Fatal(err)
	}
	files0 := salesforceShardFilesForTest(t, shardPath, bundlePath, bundleSHA, alias, shard.OrgID)
	files1 := salesforceShardFilesForTest(t, shard1Path, bundlePath, bundleSHA, shard1Alias, shard1.OrgID)
	snapshot, err := readSealedSalesforceExecutor(executorRoot)
	if err != nil {
		t.Fatal(err)
	}
	missingProjectManifest := snapshot
	missingProjectManifest.Files = make(map[string][]byte, len(snapshot.Files))
	for path, data := range snapshot.Files {
		missingProjectManifest.Files[path] = append([]byte(nil), data...)
	}
	var projectPayload map[string]any
	if err := json.Unmarshal(missingProjectManifest.Files["filter/results.json"], &projectPayload); err != nil {
		t.Fatal(err)
	}
	delete(projectPayload["results"].([]any)[0].(map[string]any), "projectManifest")
	missingProjectManifest.Files["filter/results.json"], err = json.Marshal(projectPayload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deriveSalesforceFilterEvidence(bundle, bundlePath, alias, lifecycle.OrgID, lifecycle.OrgUsername, executorRoot, runID, 0, missingProjectManifest); err == nil {
		t.Fatal("accepted a fixture receipt without its pre-transport project manifest")
	}
	tamperedRemoteProject := snapshot
	tamperedRemoteProject.Files = make(map[string][]byte, len(snapshot.Files))
	for path, data := range snapshot.Files {
		tamperedRemoteProject.Files[path] = append([]byte(nil), data...)
	}
	var remoteProjectPayload map[string]any
	if err := json.Unmarshal(tamperedRemoteProject.Files["filter/results.json"], &remoteProjectPayload); err != nil {
		t.Fatal(err)
	}
	remoteProjectPayload["results"].([]any)[0].(map[string]any)["projectManifest"].([]any)[0].(map[string]any)["sha256"] = strings.Repeat("0", 64)
	tamperedRemoteProject.Files["filter/results.json"], err = json.Marshal(remoteProjectPayload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deriveSalesforceFilterEvidence(bundle, bundlePath, alias, lifecycle.OrgID, lifecycle.OrgUsername, executorRoot, runID, 0, tamperedRemoteProject); err == nil {
		t.Fatal("accepted a Salesforce-worker project tree that differs from the sealed manifest")
	}
	missingReceipt := snapshot
	missingReceipt.Files = make(map[string][]byte, len(snapshot.Files))
	for path, data := range snapshot.Files {
		missingReceipt.Files[path] = append([]byte(nil), data...)
	}
	missingReceipt.Files["filter/results.json"] = []byte(`{"results":[]}`)
	if _, err := deriveSalesforceFilterEvidence(bundle, bundlePath, alias, lifecycle.OrgID, lifecycle.OrgUsername, executorRoot, runID, 0, missingReceipt); err == nil {
		t.Fatal("accepted raw fixture output without the adapter's per-fixture receipt")
	}
	missingExecution := snapshot
	missingExecution.Files = make(map[string][]byte, len(snapshot.Files))
	for path, data := range snapshot.Files {
		missingExecution.Files[path] = append([]byte(nil), data...)
	}
	var adapterPayload map[string]any
	if err := json.Unmarshal(missingExecution.Files["filter/results.json"], &adapterPayload); err != nil {
		t.Fatal(err)
	}
	adapterPayload["results"].([]any)[0].(map[string]any)["project"] = ""
	adapterPayload["results"].([]any)[0].(map[string]any)["invocation"] = nil
	missingExecution.Files["filter/results.json"], err = json.Marshal(adapterPayload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deriveSalesforceFilterEvidence(bundle, bundlePath, alias, lifecycle.OrgID, lifecycle.OrgUsername, executorRoot, runID, 0, missingExecution); err == nil {
		t.Fatal("accepted raw fixture output without its project and remote command receipt")
	}
	tamperedCommand := snapshot
	tamperedCommand.Files = make(map[string][]byte, len(snapshot.Files))
	for path, data := range snapshot.Files {
		tamperedCommand.Files[path] = append([]byte(nil), data...)
	}
	var tamperedPayload map[string]any
	if err := json.Unmarshal(tamperedCommand.Files["filter/results.json"], &tamperedPayload); err != nil {
		t.Fatal(err)
	}
	remoteCommand := tamperedPayload["results"].([]any)[0].(map[string]any)["invocation"].(map[string]any)["commands"].([]any)[0].(map[string]any)
	remoteCommand["args"] = []string{"invalid"}
	tamperedCommand.Files["filter/results.json"], err = json.Marshal(tamperedPayload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deriveSalesforceFilterEvidence(bundle, bundlePath, alias, lifecycle.OrgID, lifecycle.OrgUsername, executorRoot, runID, 0, tamperedCommand); err == nil {
		t.Fatal("accepted a remote command whose target and subcommand differ from the sealed fixture execution")
	}
	tamperedCleanup := snapshot
	tamperedCleanup.Files = make(map[string][]byte, len(snapshot.Files))
	for path, data := range snapshot.Files {
		tamperedCleanup.Files[path] = append([]byte(nil), data...)
	}
	var cleanupPayload map[string]any
	if err := json.Unmarshal(tamperedCleanup.Files["filter/results.json"], &cleanupPayload); err != nil {
		t.Fatal(err)
	}
	cleanupPayload["results"].([]any)[0].(map[string]any)["orgCleanup"].(map[string]any)["residueAbsent"] = false
	tamperedCleanup.Files["filter/results.json"], err = json.Marshal(cleanupPayload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deriveSalesforceFilterEvidence(bundle, bundlePath, alias, lifecycle.OrgID, lifecycle.OrgUsername, executorRoot, runID, 0, tamperedCleanup); err == nil {
		t.Fatal("accepted a fixture receipt with remote cleanup residue")
	}
	if err := ValidateSalesforceShardFiles(planPath, []SalesforceShardFiles{files0, files1}); err != nil {
		t.Fatalf("ValidateSalesforceShardFiles: %v", err)
	}
	selectionPath := filepath.Join(sealedSalesforceFilterOutputPath(executorRoot), "selection.json")
	originalSelection, err := os.ReadFile(selectionPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(selectionPath, []byte(`[]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSalesforceShardFiles(planPath, []SalesforceShardFiles{files0, files1}); err == nil {
		t.Fatal("accepted a changed executor artifact outside the shard sidecar")
	}
	if err := os.WriteFile(selectionPath, originalSelection, 0o600); err != nil {
		t.Fatal(err)
	}
	transport, _, err := readExactJSONBytes[oracleTransportManifest](filepath.Join(filepath.Dir(bundlePath), "fixture-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	stem, err := salesforceFixtureStem(transport.Fixtures[0].Fixture)
	if err != nil {
		t.Fatal(err)
	}
	rawDeployPath := filepath.Join(sealedSalesforceFilterOutputPath(executorRoot), "projects", stem, "salesforce-"+alias+".json")
	originalRawDeploy, err := os.ReadFile(rawDeployPath)
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(executorRoot, salesforceExecutorManifestName)
	originalManifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	originalShard, err := os.ReadFile(shardPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rawDeployPath, []byte(`{"status":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	forged, _, err := readExactJSONBytes[SalesforceShard](shardPath)
	if err != nil {
		t.Fatal(err)
	}
	forged.ExecutorManifestSHA256 = rewriteSalesforceExecutorManifestForTest(t, executorRoot)
	forgedBytes, err := json.Marshal(forged)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shardPath, append(forgedBytes, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSalesforceShardFiles(planPath, []SalesforceShardFiles{files0, files1}); err == nil {
		t.Fatal("accepted a failed raw deploy behind a fabricated filter summary")
	}
	if err := os.WriteFile(rawDeployPath, originalRawDeploy, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, originalManifest, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shardPath, originalShard, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(sealedSalesforceFilterScriptPath(shard.ExecutorRoot)); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSalesforceShardFiles(planPath, []SalesforceShardFiles{files0, files1}); err == nil {
		t.Fatal("accepted fabricated lifecycle receipts without the retained executed filter")
	}
	if err := copyOracleBundleFile(filepath.Join(outputRoot, "transport", "salesforce-first-filter.py"), sealedSalesforceFilterScriptPath(shard.ExecutorRoot), 0o500); err != nil {
		t.Fatal(err)
	}
	rewritten, _, err := readExactJSONBytes[SalesforceShard](shardPath)
	if err != nil {
		t.Fatal(err)
	}
	rewritten.ExecutorRoot, rewritten.RunID = filepath.Join(t.TempDir(), "executor", "rewritten"), "rewritten-run"
	rewrittenArgs, err := salesforceFilterArgs(sealedSalesforceFilterScriptPath(rewritten.ExecutorRoot), filepath.Dir(bundlePath), rewritten.ExecutorRoot, rewritten.RunID, alias, bundle, bundleSHA, rewritten.ShardIndex, rewritten.ShardCount)
	if err != nil {
		t.Fatal(err)
	}
	rewrittenArgs, err = sealedSalesforceFilterInvocationArgs(sealedSalesforceFilterScriptPath(rewritten.ExecutorRoot), filterSource, rewrittenArgs)
	if err != nil {
		t.Fatal(err)
	}
	rewritten.Commands[0].Command = append([]string{"/usr/bin/python3"}, rewrittenArgs...)
	rewritten.Commands[0].ExecutableSHA256, rewritten.Commands[0].ExecutableAfterSHA256 = pythonSHA, pythonSHA
	rewritten.Commands[0].CommandSpecSHA256 = salesforceFilterCommandSpecSHA256("/usr/bin/python3", rewrittenArgs, filepath.Dir(bundlePath), environment, pythonSHA, pythonSHA)
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
			return salesforceCommandOutput{Stdout: []byte(`{"status":0,"result":{"id":"00D000000000001","status":"Active","username":"assurance-sf0@example.invalid"}}`)}, nil
		}
		return salesforceCommandOutput{Stdout: salesforceCountOutputForTest(args)}, nil
	}
	preflight, err := RunSalesforceOrgPreflight(SalesforceOrgPreflightRequest{BundlePath: bundlePath, TargetOrg: "assurance-sf0", SFBin: "/usr/local/bin/sf", OutputPath: outputPath, validateBundle: func(string) error { return nil }, runner: runner})
	if err != nil {
		t.Fatalf("RunSalesforceOrgPreflight: %v", err)
	}
	if preflight.OrgID != "00D000000000001" || preflight.OrgStatus != "Active" || !baselineSalesforceInventory(preflight.Inventory) || len(preflight.Commands) != len(salesforceInventoryTypes)+1 || commands != len(salesforceInventoryTypes)+1 {
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

func TestRunSalesforceOrgPreflightRejectsMissingCount(t *testing.T) {
	root := t.TempDir()
	bundlePath, outputPath := filepath.Join(root, "bundle.json"), filepath.Join(root, "preflight.json")
	if err := os.WriteFile(bundlePath, []byte(`{"bundle":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := RunSalesforceOrgPreflight(SalesforceOrgPreflightRequest{BundlePath: bundlePath, TargetOrg: "assurance-sf0", SFBin: "/usr/local/bin/sf", OutputPath: outputPath, validateBundle: func(string) error { return nil }, runner: func(_ context.Context, _ string, args ...string) (salesforceCommandOutput, error) {
		if len(args) >= 2 && args[0] == "org" && args[1] == "display" {
			return salesforceCommandOutput{Stdout: []byte(`{"status":0,"result":{"id":"00D000000000001","status":"Active","username":"assurance-sf0@example.invalid"}}`)}, nil
		}
		return salesforceCommandOutput{Stdout: []byte(`{}`)}, nil
	}})
	if err == nil {
		t.Fatal("accepted missing Salesforce count")
	}
	if _, statErr := os.Stat(outputPath); !os.IsNotExist(statErr) {
		t.Fatalf("preflight receipt exists after missing count: %v", statErr)
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
				return salesforceCommandOutput{Stdout: []byte(`{"status":0,"result":{"id":"00D000000000001","status":"Active","username":"assurance-sf0@example.invalid"}}`)}, nil
			}
			return salesforceCommandOutput{Stdout: salesforceCountOutputForTest(args)}, nil
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
				return salesforceCommandOutput{Stdout: []byte(`{"status":0,"result":{"id":"00D000000000001","status":"Active","username":"assurance-sf0@example.invalid"}}`)}, nil
			}
			if err := os.WriteFile(bundlePath, []byte(`{"bundle":false}`), 0o600); err != nil {
				t.Fatal(err)
			}
			return salesforceCommandOutput{Stdout: salesforceCountOutputForTest(args)}, nil
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
	writeSyntheticDevHubBundle(t, bundlePath)
	if err := os.WriteFile(filepath.Join(root, "corpus-assurance-scratch-def.json"), []byte(`{"orgName":"Glade Assurance","edition":"Developer","features":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	creation, err := RunSalesforceOrgCreate(SalesforceOrgCreateRequest{BundlePath: bundlePath, DevHub: "sealed-dev-hub", Alias: "assurance-sf0", SFBin: "/usr/local/bin/sf", OutputPath: outputPath, validateBundle: func(string) error { return nil }, runner: func(_ context.Context, path string, args ...string) (salesforceCommandOutput, error) {
		if len(args) >= 2 && args[0] == "org" && args[1] == "display" {
			return salesforceCommandOutput{Stdout: []byte(`{"status":0,"result":{"id":"00D0","status":"Active","username":"sealed-dev-hub@example.invalid"}}`)}, nil
		}
		if path != "/usr/local/bin/sf" || !containsString(args, "--definition-file") || !containsString(args, "sealed-dev-hub") {
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
	writeSyntheticDevHubBundle(t, bundlePath)
	if err := os.WriteFile(filepath.Join(root, "corpus-assurance-scratch-def.json"), []byte(`{"orgName":"Glade Assurance","edition":"Developer","features":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	validations := 0
	_, err := RunSalesforceOrgCreate(SalesforceOrgCreateRequest{
		BundlePath: bundlePath, DevHub: "sealed-dev-hub", Alias: "assurance-sf0", SFBin: "/usr/local/bin/sf", OutputPath: outputPath,
		validateBundle: func(string) error {
			validations++
			if validations == 2 {
				return errors.New("bundle changed")
			}
			return nil
		},
		runner: func(_ context.Context, _ string, args ...string) (salesforceCommandOutput, error) {
			if len(args) >= 2 && args[0] == "org" && args[1] == "display" {
				return salesforceCommandOutput{Stdout: []byte(`{"status":0,"result":{"id":"00D0","status":"Active","username":"sealed-dev-hub@example.invalid"}}`)}, nil
			}
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
	writeSyntheticDevHubBundle(t, bundlePath)
	if err := os.WriteFile(filepath.Join(root, "corpus-assurance-scratch-def.json"), []byte(`{"orgName":"Glade Assurance","edition":"Developer","features":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := RunSalesforceOrgCreate(SalesforceOrgCreateRequest{
		BundlePath: bundlePath, DevHub: "sealed-dev-hub", Alias: "assurance-sf0", SFBin: "/usr/local/bin/sf", OutputPath: outputPath,
		validateBundle: func(string) error { return nil },
		runner: func(_ context.Context, _ string, args ...string) (salesforceCommandOutput, error) {
			if len(args) >= 2 && args[0] == "org" && args[1] == "display" {
				return salesforceCommandOutput{Stdout: []byte(`{"status":0,"result":{"id":"00D0","status":"Active","username":"sealed-dev-hub@example.invalid"}}`)}, nil
			}
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
	writeSyntheticDevHubBundle(t, bundlePath)
	if err := os.WriteFile(filepath.Join(root, "corpus-assurance-scratch-def.json"), []byte(`{"orgName":"Glade Assurance","edition":"Developer","features":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	validations := 0
	_, err := RunSalesforceOrgCreate(SalesforceOrgCreateRequest{
		BundlePath: bundlePath, DevHub: "sealed-dev-hub", Alias: "assurance-sf0", SFBin: "/usr/local/bin/sf", OutputPath: outputPath,
		validateBundle: func(string) error {
			validations++
			if validations == 2 {
				return errors.New("bundle changed")
			}
			return nil
		},
		runner: func(_ context.Context, _ string, args ...string) (salesforceCommandOutput, error) {
			if len(args) >= 2 && args[0] == "org" && args[1] == "display" {
				return salesforceCommandOutput{Stdout: []byte(`{"status":0,"result":{"id":"00D0","status":"Active","username":"sealed-dev-hub@example.invalid"}}`)}, nil
			}
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

func TestRunSalesforceOrgCreateSealsInvalidatedAuthorityWhenDevHubCheckFails(t *testing.T) {
	root := t.TempDir()
	bundlePath, outputPath := filepath.Join(root, "bundle.json"), filepath.Join(root, "org-create.json")
	writeSyntheticDevHubBundle(t, bundlePath)
	if err := os.WriteFile(filepath.Join(root, "corpus-assurance-scratch-def.json"), []byte(`{"orgName":"Glade Assurance","edition":"Developer","features":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := RunSalesforceOrgCreate(SalesforceOrgCreateRequest{
		BundlePath: bundlePath, DevHub: "sealed-dev-hub", Alias: "assurance-sf0", SFBin: "/usr/local/bin/sf", OutputPath: outputPath,
		validateBundle: func(string) error { return nil },
		runner: func(_ context.Context, _ string, args ...string) (salesforceCommandOutput, error) {
			if len(args) >= 2 && args[0] == "org" && args[1] == "display" {
				return salesforceCommandOutput{}, errors.New("Dev Hub check unavailable")
			}
			return salesforceCommandOutput{Stdout: []byte(`{"status":0,"result":{"orgId":"00D000000000001"}}`)}, nil
		},
	})
	if err == nil {
		t.Fatal("RunSalesforceOrgCreate accepted a failed Dev Hub check")
	}
	invalidated, readErr := readExactJSON[SalesforceOrgCreation](outputPath + ".invalidated")
	if readErr != nil || !validInvalidatedSalesforceOrgCreation(invalidated, "sealed-dev-hub", "assurance-sf0") || invalidated.OrgID != "00D000000000001" {
		t.Fatalf("invalidated creation = %#v, %v", invalidated, readErr)
	}
}

func TestRunSalesforceOrgCleanupOnlyDeletesTheReceiptCreatedOrg(t *testing.T) {
	root := t.TempDir()
	bundlePath, creationPath, preflightPath, outputPath := filepath.Join(root, "bundle.json"), filepath.Join(root, "creation.json"), filepath.Join(root, "preflight.json"), filepath.Join(root, "cleanup.json")
	writeSyntheticDevHubBundle(t, bundlePath)
	bundleSHA := localProofFileSHA256(t, bundlePath)
	createArgs := salesforceOrgCreateArgs(filepath.Join(root, "corpus-assurance-scratch-def.json"), "sealed-dev-hub", "assurance-sf0")
	creation := SalesforceOrgCreation{SchemaVersion: 1, BundleSHA256: bundleSHA, DevHub: "sealed-dev-hub", DevHubOrgID: "00D0", DevHubUsername: "sealed-dev-hub@example.invalid", Alias: "assurance-sf0", OrgID: "00D0", Command: salesforceCommandForTest(t, bundlePath, createArgs), DevHubCommand: salesforceCommandForTest(t, bundlePath, []string{"org", "display", "--target-org", "sealed-dev-hub", "--json"})}
	if err := WriteNewJSON(creationPath, creation); err != nil {
		t.Fatal(err)
	}
	preflight := salesforcePreflightForTest(t, "assurance-sf0", bundleSHA, bundlePath)
	if err := WriteNewJSON(preflightPath, preflight); err != nil {
		t.Fatal(err)
	}
	postDeleteChecked := false
	cleanup, err := RunSalesforceOrgCleanup(SalesforceOrgCleanupRequest{BundlePath: bundlePath, CreationPath: creationPath, PreflightPath: preflightPath, TargetOrg: "assurance-sf0", DevHub: "sealed-dev-hub", SFBin: "/usr/local/bin/sf", OutputPath: outputPath, validateBundle: func(string) error { return nil }, runner: func(_ context.Context, path string, args ...string) (salesforceCommandOutput, error) {
		if path != "/usr/local/bin/sf" {
			return salesforceCommandOutput{}, fmt.Errorf("unexpected sf path %q", path)
		}
		if len(args) >= 3 && args[0] == "org" && args[1] == "delete" {
			return salesforceCommandOutput{Stdout: []byte(`{"status":0}`)}, nil
		}
		if len(args) >= 2 && args[0] == "org" && args[1] == "display" {
			if containsString(args, "assurance-sf0") {
				postDeleteChecked = true
				return salesforceCommandOutput{Stdout: []byte(`{"status":1,"message":"not found"}`), ExitCode: 1}, nil
			}
			return salesforceCommandOutput{Stdout: []byte(`{"status":0,"result":{"id":"00D0","status":"Active","username":"sealed-dev-hub@example.invalid"}}`)}, nil
		}
		t.Fatalf("unexpected cleanup command %q", args)
		return salesforceCommandOutput{}, nil
	}})
	if err != nil {
		t.Fatalf("RunSalesforceOrgCleanup: %v", err)
	}
	if !cleanup.ResidueAbsent || cleanup.OrgID != creation.OrgID || len(cleanup.Commands) != 2 || !postDeleteChecked || cleanup.Commands[1].Passed || cleanup.Commands[1].ExitCode == 0 {
		t.Fatalf("cleanup = %#v", cleanup)
	}
	if _, err := os.Stat(outputPath); err != nil {
		t.Fatal(err)
	}
}

func TestRunSalesforceOrgCleanupAcceptsAnInvalidatedCreationWithoutPreflight(t *testing.T) {
	root := t.TempDir()
	bundlePath, creationPath, outputPath := filepath.Join(root, "bundle.json"), filepath.Join(root, "creation.invalidated.json"), filepath.Join(root, "cleanup.json")
	writeSyntheticDevHubBundle(t, bundlePath)
	bundleSHA := localProofFileSHA256(t, bundlePath)
	args := salesforceOrgCreateArgs(filepath.Join(root, "corpus-assurance-scratch-def.json"), "sealed-dev-hub", "assurance-sf0")
	creation := SalesforceOrgCreation{SchemaVersion: 1, BundleSHA256: bundleSHA, DevHub: "sealed-dev-hub", DevHubOrgID: "00D0", DevHubUsername: "sealed-dev-hub@example.invalid", Alias: "assurance-sf0", OrgID: "00D0", Invalidated: true, Command: salesforceCommandForTest(t, bundlePath, args), DevHubCommand: salesforceCommandForTest(t, bundlePath, []string{"org", "display", "--target-org", "sealed-dev-hub", "--json"})}
	if err := WriteNewJSON(creationPath, creation); err != nil {
		t.Fatal(err)
	}
	cleanup, err := RunSalesforceOrgCleanup(SalesforceOrgCleanupRequest{BundlePath: bundlePath, CreationPath: creationPath, TargetOrg: "assurance-sf0", DevHub: "sealed-dev-hub", SFBin: "/usr/local/bin/sf", OutputPath: outputPath, validateBundle: func(string) error { return nil }, runner: func(_ context.Context, _ string, args ...string) (salesforceCommandOutput, error) {
		if len(args) >= 2 && args[0] == "org" && args[1] == "display" && containsString(args, "sealed-dev-hub") {
			return salesforceCommandOutput{Stdout: []byte(`{"status":0,"result":{"id":"00D0","status":"Active","username":"sealed-dev-hub@example.invalid"}}`)}, nil
		}
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
	bundle := OracleBundle{Candidate: candidate, Tools: tools, ToolsAMD64: RuntimeArtifact{Commit: tools.Commit, OS: "darwin", Arch: "amd64", SHA256: strings.Repeat("9", 64)}, ToolsAMD64SHA256: strings.Repeat("9", 64), ProfileSHA256: strings.Repeat("e", 64), OraclePlanSHA256: strings.Repeat("f", 64), TransportManifestSHA256: strings.Repeat("1", 64), LocalProofSummarySHA256: strings.Repeat("2", 64), FilterSHA256: strings.Repeat("3", 64)}
	bundlePath := "/private/tmp/bundle.json"
	preflight := SalesforceOrgPreflight{SchemaVersion: 1, BundleSHA256: strings.Repeat("4", 64), OrgAlias: "assurance-sf0", OrgID: "00D0", OrgUsername: "assurance-sf0@example.invalid", OrgStatus: "Active", Inventory: SalesforceInventory{Counts: map[string]int{}}}
	preflightArgs := [][]string{{"org", "display", "--target-org", preflight.OrgAlias, "--json"}}
	for _, kind := range salesforceInventoryTypes {
		preflightArgs = append(preflightArgs, []string{"data", "query", "--query", "SELECT count() FROM " + kind, "--target-org", preflight.OrgAlias, "--use-tooling-api", "--json"})
		preflight.Inventory.Counts[kind] = salesforceInventoryBaselineCount(kind)
	}
	for _, args := range preflightArgs {
		preflight.Commands = append(preflight.Commands, salesforceCommandForTest(t, bundlePath, args))
	}
	postflight := preflight
	runtimePassed := true
	filter := salesforceFilterResults{Sealed: true, Orgs: []string{"assurance-sf0"}, Binding: salesforceFilterBinding{ManifestSHA256: bundle.TransportManifestSHA256, ProfileSHA256: bundle.ProfileSHA256, QueueSHA256: bundle.OraclePlanSHA256, SelectorSHA256: bundle.OraclePlanSHA256, SelectorReceiptSHA256: preflight.BundleSHA256, CandidateCommit: candidate.Commit, CandidateSHA256: candidate.SHA256, ToolsCommit: tools.Commit, ToolsAMD64SHA256: bundle.ToolsAMD64SHA256, WorkflowScriptSHA256: bundle.FilterSHA256, LocalSummarySHA256: bundle.LocalProofSummarySHA256}, OrgPostflight: salesforceFilterPostflight{MatchesPreflight: true}, Results: []salesforceFilterFixtureResult{{SurfaceIDs: []string{"apex:System.run()"}, Org: "assurance-sf0", Kind: "exec", ExitCode: &zero, Deployable: true, RuntimePassed: &runtimePassed, RuntimeResult: json.RawMessage(`{"status":0,"result":{"success":true,"compiled":true}}`), OrgCleanup: CleanupReceipt{ResidueAbsent: true}}, {SurfaceIDs: []string{"apex:System.compile()"}, Org: "assurance-sf0", Kind: "check", ExitCode: &zero, Deployable: true, OrgCleanup: CleanupReceipt{ResidueAbsent: true}}}}
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
	filter.Results[0].RuntimePassed = &runtimePassed
	filter.Results[0].RuntimeResult = nil
	if _, err := NormalizeSalesforceFilterResults(plan, bundle, bundlePath, "/private/tmp/executor/shard-0", "attempt-shard-0", preflight, postflight, filter, command, 0, 2); err == nil {
		t.Fatal("accepted a runtime surface without a Salesforce runtime observation")
	}
	filter.Results[0].RuntimeResult = json.RawMessage(`{"status":0,"result":{"success":true,"compiled":true}}`)
	filter.Results[0].Kind = "test"
	filter.Results[0].RuntimeResult = json.RawMessage(`{"status":0,"result":{"summary":{"outcome":"Passed","testsRan":1,"failing":0,"passing":1}}}`)
	if _, err := NormalizeSalesforceFilterResults(plan, bundle, bundlePath, "/private/tmp/executor/shard-0", "attempt-shard-0", preflight, postflight, filter, command, 0, 2); err != nil {
		t.Fatalf("NormalizeSalesforceFilterResults rejected a passing Salesforce test-context runtime result: %v", err)
	}
	filter.Results[0].Kind = "exec"
	filter.Results[0].RuntimeResult = json.RawMessage(`{"status":0,"result":{"success":true,"compiled":true}}`)
	filter.Binding.ToolsAMD64SHA256 = ""
	if _, err := NormalizeSalesforceFilterResults(plan, bundle, bundlePath, "/private/tmp/executor/shard-0", "attempt-shard-0", preflight, postflight, filter, command, 0, 2); err == nil {
		t.Fatal("accepted a filter result without the sealed amd64 tools hash")
	}
	preflight.Commands[0].Command = []string{"/usr/local/bin/sf", "org", "list", "--json"}
	if _, err := NormalizeSalesforceFilterResults(plan, bundle, bundlePath, "/private/tmp/executor/shard-0", "attempt-shard-0", preflight, postflight, filter, command, 0, 2); err == nil {
		t.Fatal("accepted a preflight receipt without the exact org-display command")
	}
}

func TestSalesforceFilterArgsDeriveEveryIdentityFromTheSealedBundle(t *testing.T) {
	bundle := OracleBundle{Candidate: RuntimeArtifact{Commit: strings.Repeat("a", 40), OS: "darwin", Arch: "arm64", SHA256: strings.Repeat("b", 64)}, Tools: RuntimeArtifact{Commit: strings.Repeat("c", 40), OS: "darwin", Arch: "amd64", SHA256: strings.Repeat("d", 64)}, ToolsAMD64: RuntimeArtifact{Commit: strings.Repeat("c", 40), OS: "darwin", Arch: "amd64", SHA256: strings.Repeat("3", 64)}, ToolsAMD64SHA256: strings.Repeat("3", 64), AttemptSHA256: strings.Repeat("4", 64), OraclePlanSHA256: strings.Repeat("e", 64), TransportManifestSHA256: strings.Repeat("f", 64), LocalProofSummarySHA256: strings.Repeat("1", 64), Fixtures: []OracleBundleFixture{{ID: "system"}}}
	args, err := salesforceFilterArgs("/private/tmp/assurance/transport/salesforce-first-filter.py", "/private/tmp/assurance/bundle", "/private/tmp/assurance/executor/shard-0", "attempt-shard-0", "assurance-sf0", bundle, strings.Repeat("2", 64), 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--candidate-commit", bundle.Candidate.Commit, "--candidate-sha256", bundle.Candidate.SHA256, "--tools-commit", bundle.Tools.Commit, "--tools-amd64-sha256", bundle.ToolsAMD64SHA256, "--queue-sha256", bundle.OraclePlanSHA256, "--selector-receipt-sha256", strings.Repeat("2", 64), "--manifest-index-modulus", "2", "--manifest-index-remainder", "0", "--sf-bin", "/usr/local/bin/sf", "--runtime"} {
		if !containsString(args, want) {
			t.Fatalf("filter args omit %q: %v", want, args)
		}
	}
	for _, forbidden := range []string{"--ssh-host", "--ssh-user", "--remote-root", "--remote-run-id", "--remote-sf-bin"} {
		if containsString(args, forbidden) {
			t.Fatalf("filter args retain self-SSH option %q: %v", forbidden, args)
		}
	}
}

func TestCreateSalesforceDispatchRejectsExecutorOutsideSealedAttempt(t *testing.T) {
	inputs := oracleBundleTestInputsForLocalProof(t)
	writeSealedReleaseValidation(t, inputs, inputs.attemptPath)
	outputRoot := filepath.Join(t.TempDir(), "salesforce-worker")
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

func TestCreateSalesforceDispatchRejectsSymlinkedExecutor(t *testing.T) {
	inputs := oracleBundleTestInputsForLocalProof(t)
	writeSealedReleaseValidation(t, inputs, inputs.attemptPath)
	outputRoot := filepath.Join(t.TempDir(), "salesforce-worker")
	bundle, err := BuildOracleBundle(inputs.request(outputRoot))
	if err != nil {
		t.Fatal(err)
	}
	bundlePath := filepath.Join(outputRoot, "bundle", "bundle.json")
	executorBase := filepath.Join(filepath.Dir(outputRoot), "executor")
	if err := os.Symlink(t.TempDir(), executorBase); err != nil {
		t.Fatal(err)
	}
	_, err = CreateSalesforceDispatch(SalesforceDispatchRequest{
		BundlePath: bundlePath, OrgAlias: "assurance-sf0", ExecutorRoot: filepath.Join(executorBase, "shard-0"),
		RunID: "assurance-" + bundle.AttemptSHA256[:16] + "-shard-0", ShardIndex: 0, ShardCount: 2, OutputPath: filepath.Join(t.TempDir(), "DISPATCH.json"),
	})
	if err == nil {
		t.Fatal("accepted a symlinked sealed executor root")
	}
}

func TestRunSalesforceShardSealsFilterAndFreshPostflight(t *testing.T) {
	root, attemptRoot := t.TempDir(), ""
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	attemptRoot = filepath.Join(canonicalRoot, "attempt")
	workerRoot := filepath.Join(attemptRoot, "salesforce-worker")
	bundleRoot, executorRoot := filepath.Join(workerRoot, "bundle"), filepath.Join(attemptRoot, "executor", "shard-0")
	for _, path := range []string{bundleRoot, filepath.Join(workerRoot, "transport"), executorRoot} {
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
	stagedFilterPath := filepath.Join(workerRoot, "transport", "salesforce-first-filter.py")
	if err := os.WriteFile(stagedFilterPath, []byte("test filter"), 0o700); err != nil {
		t.Fatal(err)
	}
	fixturePath := filepath.Join(bundleRoot, "fixtures", "fixture-runtime.json")
	if err := os.MkdirAll(filepath.Dir(fixturePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixturePath, []byte(`{"command":{"kind":"exec"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	fixtureSHA := localProofFileSHA256(t, fixturePath)
	transportPath := filepath.Join(bundleRoot, "fixture-manifest.json")
	transport := oracleTransportManifest{Fixtures: []oracleTransportFixture{{ID: "system", Fixture: "fixture-runtime.json", Path: "fixtures/fixture-runtime.json", SHA256: fixtureSHA, SurfaceIDs: []string{"apex:System.run()"}, SalesforceEligible: true}}}
	if err := WriteNewJSON(transportPath, transport); err != nil {
		t.Fatal(err)
	}
	transportSHA := localProofFileSHA256(t, transportPath)
	filterSHA := localProofFileSHA256(t, stagedFilterPath)
	previousFilterAuthority := testApprovedSalesforceFilterSHA256
	testApprovedSalesforceFilterSHA256 = filterSHA
	t.Cleanup(func() { testApprovedSalesforceFilterSHA256 = previousFilterAuthority })
	bundle := OracleBundle{
		SchemaVersion: 1, Candidate: candidate, Tools: tools,
		ToolsAMD64:       RuntimeArtifact{Commit: tools.Commit, OS: "darwin", Arch: "amd64", SHA256: strings.Repeat("3", 64)},
		ToolsAMD64SHA256: strings.Repeat("3", 64), ProfileSHA256: strings.Repeat("e", 64), OraclePlanSHA256: localProofFileSHA256(t, planPath),
		AttemptSHA256: strings.Repeat("a", 64), TransportManifestSHA256: transportSHA, LocalProofSummarySHA256: strings.Repeat("1", 64), FilterSHA256: filterSHA,
		Fixtures: []OracleBundleFixture{{ID: "system", Name: "system", Path: fixturePath, SHA256: fixtureSHA, SurfaceIDs: []string{"apex:System.run()"}}},
	}
	if err := WriteNewJSON(bundlePath, bundle); err != nil {
		t.Fatal(err)
	}
	bundleSHA := localProofFileSHA256(t, bundlePath)
	preflightPath := filepath.Join(root, "preflight.json")
	preflight := salesforcePreflightForTest(t, "assurance-sf0", bundleSHA, bundlePath)
	if err := WriteNewJSON(preflightPath, preflight); err != nil {
		t.Fatal(err)
	}
	runID := "assurance-" + bundle.AttemptSHA256[:16] + "-shard-0"
	filterRunner := func(ctx context.Context, path string, args ...string) (salesforceCommandOutput, error) {
		execution, ok := ctx.Value(salesforceExecutionKey{}).(salesforceExecution)
		if !ok || execution.workingDirectory != filepath.Dir(bundlePath) || !reflect.DeepEqual(execution.environment, mustFixedSalesforceEnvironment(t)) {
			return salesforceCommandOutput{}, fmt.Errorf("unsealed Salesforce filter execution")
		}
		if path != "/usr/bin/python3" {
			return salesforceCommandOutput{}, fmt.Errorf("unexpected filter runner %q", path)
		}
		if len(args) < 4 || args[0] != "-c" || args[2] != sealedSalesforceFilterScriptPath(executorRoot) {
			return salesforceCommandOutput{}, fmt.Errorf("filter did not run from sealed executor copy: %v", args)
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
		project := filepath.Join(out, "projects", "fixture-runtime")
		zero, runtimePassed := 0, true
		anonymous := []byte("System.debug('fixture');\n")
		projectManifest := []salesforceExecutorFile{{Path: "anonymous.apex", SHA256: replayBytesSHA256(anonymous)}}
		filter := salesforceFilterResults{Sealed: true, Orgs: []string{"assurance-sf0"}, Binding: salesforceFilterBinding{ManifestSHA256: bundle.TransportManifestSHA256, ProfileSHA256: bundle.ProfileSHA256, QueueSHA256: bundle.OraclePlanSHA256, SelectorSHA256: bundle.OraclePlanSHA256, SelectorReceiptSHA256: bundleSHA, CandidateCommit: candidate.Commit, CandidateSHA256: candidate.SHA256, ToolsCommit: tools.Commit, ToolsAMD64SHA256: bundle.ToolsAMD64SHA256, WorkflowScriptSHA256: bundle.FilterSHA256, LocalSummarySHA256: bundle.LocalProofSummarySHA256}, OrgPostflight: salesforceFilterPostflight{MatchesPreflight: true}, Results: []salesforceFilterFixtureResult{{Fixture: "fixture-runtime.json", FixtureSHA256: fixtureSHA, OrgIdentity: salesforceFilterOrgIdentity{Alias: preflight.OrgAlias, OrgID: preflight.OrgID, Username: preflight.OrgUsername}, Project: project, Invocation: salesforceInvocationForTest(project, preflight.OrgUsername, "exec"), ProjectManifest: projectManifest, SurfaceIDs: []string{"apex:System.run()"}, Org: "assurance-sf0", Kind: "exec", ExitCode: &zero, Deployable: true, RuntimePassed: &runtimePassed, RuntimeResult: json.RawMessage(`{"status":0,"result":{"success":true,"compiled":true}}`), OrgCleanup: salesforceOrgCleanupForTest()}}}
		data, err := json.Marshal(filter)
		if err != nil {
			return salesforceCommandOutput{}, err
		}
		if err := os.WriteFile(filepath.Join(out, "results.json"), data, 0o600); err != nil {
			return salesforceCommandOutput{}, err
		}
		selection, marshalErr := json.Marshal([]salesforceFilterSelection{{Fixture: "fixture-runtime.json", Coverage: 1, Kind: "exec", SurfaceIDs: []string{"apex:System.run()"}}})
		if marshalErr != nil {
			return salesforceCommandOutput{}, marshalErr
		}
		if err := os.WriteFile(filepath.Join(out, "selection.json"), selection, 0o600); err != nil {
			return salesforceCommandOutput{}, err
		}
		if err := os.MkdirAll(project, 0o700); err != nil {
			return salesforceCommandOutput{}, err
		}
		if err := os.WriteFile(filepath.Join(project, "anonymous.apex"), anonymous, 0o600); err != nil {
			return salesforceCommandOutput{}, err
		}
		for path, data := range map[string][]byte{filepath.Join(project, "salesforce-assurance-sf0.json"): []byte(`{"status":0,"result":{"success":true,"compiled":true}}`), filepath.Join(project, "salesforce-assurance-sf0.stderr"): nil, filepath.Join(project, "salesforce-assurance-sf0.setup"): nil} {
			if err := os.WriteFile(path, data, 0o600); err != nil {
				return salesforceCommandOutput{}, err
			}
		}
		return salesforceCommandOutput{Stdout: []byte(`{"selectedRows":1}`)}, nil
	}
	sfRunner := func(_ context.Context, path string, args ...string) (salesforceCommandOutput, error) {
		if path != "/usr/local/bin/sf" {
			return salesforceCommandOutput{}, fmt.Errorf("unexpected sf path %q", path)
		}
		if len(args) > 1 && args[0] == "org" {
			return salesforceCommandOutput{Stdout: []byte(`{"status":0,"result":{"id":"00D0","status":"Active","username":"assurance-sf0@example.invalid"}}`)}, nil
		}
		return salesforceCommandOutput{Stdout: salesforceCountOutputForTest(args)}, nil
	}
	dispatchPath := filepath.Join(root, "SALESFORCE_DISPATCH.json")
	filterPath := sealedSalesforceFilterScriptPath(executorRoot)
	args, err := salesforceFilterArgs(filterPath, filepath.Dir(bundlePath), executorRoot, runID, "assurance-sf0", bundle, bundleSHA, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	args, err = sealedSalesforceFilterInvocationArgs(filterPath, []byte("test filter"), args)
	if err != nil {
		t.Fatal(err)
	}
	pythonSHA := mustSealedPythonSHA(t)
	dispatch := SalesforceDispatch{SchemaVersion: 1, BundleSHA256: bundleSHA, OrgAlias: "assurance-sf0", ExecutorRoot: executorRoot, RunID: runID, ShardIndex: 0, ShardCount: 2, FilterCommandSpecSHA256: salesforceFilterCommandSpecSHA256("/usr/bin/python3", args, filepath.Dir(bundlePath), mustFixedSalesforceEnvironment(t), pythonSHA, pythonSHA)}
	if err := os.MkdirAll(executorRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := WriteNewJSON(dispatchPath, dispatch); err != nil {
		t.Fatal(err)
	}
	shardPath := filepath.Join(root, "SALESFORCE_SHARD.json")
	shard, err := RunSalesforceShard(SalesforceShardRequest{BundlePath: bundlePath, DispatchPath: dispatchPath, PreflightPath: preflightPath, TargetOrg: "assurance-sf0", SFBin: "/usr/local/bin/sf", OutputPath: shardPath, validateBundle: func(string) error { return nil }, filterRunner: filterRunner, sfRunner: sfRunner, approvedFilterSHA256: filterSHA})
	if err != nil {
		t.Fatalf("RunSalesforceShard: %v", err)
	}
	if len(shard.Results) != 1 || shard.Results[0].Kind != oracleRuntime || !baselineSalesforceInventory(shard.PostInventory) {
		t.Fatalf("shard = %#v", shard)
	}
	if _, err := os.Stat(shardPath); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(sealedSalesforceFilterOutputPath(executorRoot)); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Dir(sealedSalesforceFilterScriptPath(executorRoot))); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(executorRoot, "postflight.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(executorRoot, salesforceExecutorManifestName)); err != nil {
		t.Fatal(err)
	}
	sawFrozenSource := false
	_, err = RunSalesforceShard(SalesforceShardRequest{BundlePath: bundlePath, DispatchPath: dispatchPath, PreflightPath: preflightPath, TargetOrg: "assurance-sf0", SFBin: "/usr/local/bin/sf", OutputPath: filepath.Join(root, "SWAP_RESTORED_SHARD.json"), validateBundle: func(string) error { return nil }, filterRunner: func(ctx context.Context, path string, args ...string) (salesforceCommandOutput, error) {
		if len(args) < 4 || args[0] != "-c" || args[1] != sealedSalesforceFilterWrapper || args[2] != filterPath {
			return salesforceCommandOutput{}, fmt.Errorf("filter was invoked by pathname: %v", args)
		}
		source, decodeErr := base64.StdEncoding.DecodeString(args[3])
		if decodeErr != nil || string(source) != "test filter" {
			return salesforceCommandOutput{}, fmt.Errorf("filter did not receive frozen bytes")
		}
		sawFrozenSource = true
		output, runnerErr := filterRunner(ctx, path, args...)
		if chmodErr := os.Chmod(filterPath, 0o700); chmodErr != nil {
			return salesforceCommandOutput{}, chmodErr
		}
		if writeErr := os.WriteFile(filterPath, []byte("swapped executor copy"), 0o500); writeErr != nil {
			return salesforceCommandOutput{}, writeErr
		}
		if writeErr := os.WriteFile(filterPath, []byte("test filter"), 0o500); writeErr != nil {
			return salesforceCommandOutput{}, writeErr
		}
		return output, runnerErr
	}, sfRunner: sfRunner, approvedFilterSHA256: filterSHA})
	if err != nil || !sawFrozenSource {
		t.Fatalf("swap-and-restore did not use the frozen filter bytes: err=%v saw=%t", err, sawFrozenSource)
	}
	if err := os.Remove(filepath.Join(executorRoot, "postflight.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(executorRoot, salesforceExecutorManifestName)); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(sealedSalesforceFilterOutputPath(executorRoot)); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Dir(sealedSalesforceFilterScriptPath(executorRoot))); err != nil {
		t.Fatal(err)
	}
	_, err = RunSalesforceShard(SalesforceShardRequest{BundlePath: bundlePath, DispatchPath: dispatchPath, PreflightPath: preflightPath, TargetOrg: "assurance-sf0", SFBin: "/usr/local/bin/sf", OutputPath: filepath.Join(root, "SWAPPED_COPY_SHARD.json"), validateBundle: func(string) error { return nil }, filterRunner: func(ctx context.Context, path string, args ...string) (salesforceCommandOutput, error) {
		output, err := filterRunner(ctx, path, args...)
		if writeErr := os.WriteFile(sealedSalesforceFilterScriptPath(executorRoot), []byte("swapped executor copy"), 0o500); writeErr != nil {
			return salesforceCommandOutput{}, writeErr
		}
		return output, err
	}, sfRunner: sfRunner, approvedFilterSHA256: filterSHA})
	if err == nil {
		t.Fatal("accepted a filter copy swapped by the runner")
	}
	if err := os.RemoveAll(filepath.Join(executorRoot, "filter")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stagedFilterPath, []byte("swapped filter"), 0o700); err != nil {
		t.Fatal(err)
	}
	runnerCalled := false
	_, err = RunSalesforceShard(SalesforceShardRequest{BundlePath: bundlePath, DispatchPath: dispatchPath, PreflightPath: preflightPath, TargetOrg: "assurance-sf0", SFBin: "/usr/local/bin/sf", OutputPath: filepath.Join(root, "REJECTED_SHARD.json"), validateBundle: func(string) error { return nil }, filterRunner: func(context.Context, string, ...string) (salesforceCommandOutput, error) {
		runnerCalled = true
		return salesforceCommandOutput{}, nil
	}, sfRunner: sfRunner, approvedFilterSHA256: filterSHA})
	if err == nil || runnerCalled {
		t.Fatalf("accepted a swapped filter before execution: err=%v runnerCalled=%t", err, runnerCalled)
	}
}

func salesforcePreflightForTest(t *testing.T, alias, bundleSHA, bundlePath string) SalesforceOrgPreflight {
	t.Helper()
	preflight := SalesforceOrgPreflight{SchemaVersion: 1, BundleSHA256: bundleSHA, OrgAlias: alias, OrgID: "00D0", OrgUsername: alias + "@example.invalid", OrgStatus: "Active", Inventory: SalesforceInventory{Counts: map[string]int{}}}
	for index, args := range salesforcePreflightArgs(alias) {
		if index > 0 {
			preflight.Inventory.Counts[salesforceInventoryTypes[index-1]] = salesforceInventoryBaselineCount(salesforceInventoryTypes[index-1])
		}
		preflight.Commands = append(preflight.Commands, salesforceCommandForTest(t, bundlePath, args))
	}
	return preflight
}

func writeSyntheticDevHubBundle(t *testing.T, bundlePath string) {
	t.Helper()
	authorityPath := filepath.Join(filepath.Dir(bundlePath), "DEV_HUB_AUTHORITY.json")
	authority := SalesforceDevHubAuthority{SchemaVersion: 1, Alias: "sealed-dev-hub", OrgID: "00D0", Username: "sealed-dev-hub@example.invalid"}
	if err := WriteNewJSON(authorityPath, authority); err != nil {
		t.Fatal(err)
	}
	authoritySHA, err := sha256File(authorityPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteNewJSON(bundlePath, OracleBundle{DevHubAuthoritySHA256: authoritySHA, DevHub: authority.Alias, DevHubOrgID: authority.OrgID, DevHubUsername: authority.Username}); err != nil {
		t.Fatal(err)
	}
}

func salesforceCountOutputForTest(args []string) []byte {
	count := 0
	if strings.Contains(strings.Join(args, " "), "FROM FieldSet") {
		count = 1
	}
	return []byte(fmt.Sprintf(`{"status":0,"result":{"totalSize":%d}}`, count))
}

func salesforceCommandForTest(t *testing.T, bundlePath string, args []string) CommandResult {
	t.Helper()
	environment, err := fixedSalesforceEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	executableSHA256, err := sha256File("/usr/local/bin/sf")
	if err != nil {
		t.Fatal(err)
	}
	stdout := []byte(`{"status":0}`)
	if len(args) >= 2 && args[0] == "org" && args[1] == "display" {
		stdout = []byte(`{"status":0,"result":{"id":"00D0","status":"Active","username":"` + args[len(args)-2] + `@example.invalid"}}`)
	} else if len(args) >= 2 && args[0] == "org" && args[1] == "create" {
		stdout = []byte(`{"status":0,"result":{"orgId":"00D0"}}`)
	} else if len(args) >= 2 && args[0] == "data" && args[1] == "query" {
		stdout = salesforceCountOutputForTest(args)
	}
	output := &RetainedCommandOutput{Stdout: stdout, Stderr: []byte{}}
	return CommandResult{Command: append([]string{"/usr/local/bin/sf"}, args...), WorkingDirectory: filepath.Dir(bundlePath), Environment: environment, ExecutableSHA256: executableSHA256, ExecutableAfterSHA256: executableSHA256, CommandSpecSHA256: salesforceCommandSpecSHA256("/usr/local/bin/sf", args, filepath.Dir(bundlePath), environment, executableSHA256, executableSHA256), ExitCode: 0, Passed: true, StdoutSHA256: replayBytesSHA256(output.Stdout), StderrSHA256: replayBytesSHA256(output.Stderr), Output: output}
}

func salesforceFilterCommandForTest(args []string, bundlePath string, environment []string, pythonSHA string) CommandResult {
	output := &RetainedCommandOutput{Stdout: []byte{}, Stderr: []byte{}}
	return CommandResult{Command: append([]string{"/usr/bin/python3"}, args...), WorkingDirectory: filepath.Dir(bundlePath), Environment: environment, ExecutableSHA256: pythonSHA, ExecutableAfterSHA256: pythonSHA, CommandSpecSHA256: salesforceFilterCommandSpecSHA256("/usr/bin/python3", args, filepath.Dir(bundlePath), environment, pythonSHA, pythonSHA), ExitCode: 0, Passed: true, StdoutSHA256: replayBytesSHA256(output.Stdout), StderrSHA256: replayBytesSHA256(output.Stderr), Output: output}
}

func salesforceShardFilesForTest(t *testing.T, shardPath, bundlePath, bundleSHA, alias, orgID string) SalesforceShardFiles {
	t.Helper()
	root := t.TempDir()
	dispatchPath, creationPath, cleanupPath, preflightPath := filepath.Join(root, "DISPATCH.json"), filepath.Join(root, "ORG_CREATION.json"), filepath.Join(root, "ORG_CLEANUP.json"), filepath.Join(root, "ORG_PREFLIGHT.json")
	bundle, _, err := readExactJSONBytes[OracleBundle](bundlePath)
	if err != nil {
		t.Fatal(err)
	}
	shard, _, err := readExactJSONBytes[SalesforceShard](shardPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteNewJSON(preflightPath, shard.Preflight); err != nil {
		t.Fatal(err)
	}
	stagedFilterPath := filepath.Join(filepath.Dir(filepath.Dir(bundlePath)), "transport", "salesforce-first-filter.py")
	filterPath := sealedSalesforceFilterScriptPath(shard.ExecutorRoot)
	if err := os.MkdirAll(filepath.Dir(filterPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := copyOracleBundleFile(stagedFilterPath, filterPath, 0o500); err != nil {
		t.Fatal(err)
	}
	filterResultsPath := filepath.Join(sealedSalesforceFilterOutputPath(shard.ExecutorRoot), "results.json")
	if err := os.MkdirAll(filepath.Dir(filterResultsPath), 0o700); err != nil {
		t.Fatal(err)
	}
	filterResults := salesforceFilterResultsForShard(bundlePath, shard, bundle, bundleSHA)
	filterBytes, err := json.Marshal(filterResults)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filterResultsPath, filterBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	writeSalesforceExecutorEvidenceForTest(t, bundlePath, shard)
	populateSalesforceProjectManifestsForTest(t, filterResultsPath, shard.ExecutorRoot)
	postflightPath := filepath.Join(shard.ExecutorRoot, "postflight.json")
	if err := WriteNewJSON(postflightPath, shard.Postflight); err != nil {
		t.Fatal(err)
	}
	executor, err := sealSalesforceExecutor(shard.ExecutorRoot)
	if err != nil {
		t.Fatal(err)
	}
	dispatch, err := CreateSalesforceDispatch(SalesforceDispatchRequest{BundlePath: bundlePath, OrgAlias: alias, ExecutorRoot: shard.ExecutorRoot, RunID: shard.RunID, ShardIndex: shard.ShardIndex, ShardCount: shard.ShardCount, OutputPath: dispatchPath, approvedFilterSHA256: bundle.FilterSHA256})
	if err != nil {
		t.Fatal(err)
	}
	plan, _, err := readExactJSONBytes[OraclePlan](filepath.Join(filepath.Dir(bundlePath), "ORACLE_PLAN.json"))
	if err != nil {
		t.Fatal(err)
	}
	derived, err := deriveSalesforceFilterEvidence(bundle, bundlePath, shard.Preflight.OrgAlias, shard.Preflight.OrgID, shard.Preflight.OrgUsername, shard.ExecutorRoot, shard.RunID, shard.ShardIndex, executor)
	if err != nil {
		t.Fatal(err)
	}
	derived.Binding.SelectorReceiptSHA256 = shard.Preflight.BundleSHA256
	rebuilt, err := NormalizeSalesforceFilterResults(plan, bundle, bundlePath, shard.ExecutorRoot, shard.RunID, shard.Preflight, shard.Postflight, derived, shard.Commands[0], shard.ShardIndex, shard.ShardCount)
	if err != nil {
		t.Fatal(err)
	}
	shard = rebuilt
	shard.DispatchSHA256 = localProofFileSHA256(t, dispatchPath)
	shard.PreflightSHA256 = localProofFileSHA256(t, preflightPath)
	shard.PostflightSHA256 = localProofFileSHA256(t, postflightPath)
	shard.FilterResultsSHA256 = localProofFileSHA256(t, filterResultsPath)
	shard.ExecutedFilterSHA256 = localProofFileSHA256(t, filterPath)
	shard.ExecutorManifestSHA256 = executor.ManifestSHA256
	if data, err := json.Marshal(shard); err != nil || os.WriteFile(shardPath, append(data, '\n'), 0o600) != nil {
		t.Fatal(err)
	}
	creation := SalesforceOrgCreation{SchemaVersion: 1, BundleSHA256: bundleSHA, DevHub: bundle.DevHub, DevHubOrgID: bundle.DevHubOrgID, DevHubUsername: bundle.DevHubUsername, Alias: alias, OrgID: orgID, Command: salesforceCommandForTest(t, bundlePath, salesforceOrgCreateArgs(filepath.Join(filepath.Dir(bundlePath), "corpus-assurance-scratch-def.json"), bundle.DevHub, alias)), DevHubCommand: salesforceCommandForTest(t, bundlePath, []string{"org", "display", "--target-org", bundle.DevHub, "--json"})}
	creation.DevHubCommand.Output.Stdout = []byte(`{"status":0,"result":{"id":"` + bundle.DevHubOrgID + `","status":"Active","username":"` + bundle.DevHubUsername + `"}}`)
	creation.DevHubCommand.StdoutSHA256 = replayBytesSHA256(creation.DevHubCommand.Output.Stdout)
	creation.Command.Output.Stdout = []byte(`{"status":0,"result":{"orgId":"` + orgID + `"}}`)
	creation.Command.StdoutSHA256 = replayBytesSHA256(creation.Command.Output.Stdout)
	if err := WriteNewJSON(creationPath, creation); err != nil {
		t.Fatal(err)
	}
	deleted := salesforceCommandForTest(t, bundlePath, []string{"org", "delete", "scratch", "--target-org", alias, "--no-prompt", "--json"})
	absent := salesforceCommandForTest(t, bundlePath, []string{"org", "display", "--target-org", alias, "--json"})
	absent.ExitCode, absent.Passed = 1, false
	absent.Output.Stdout = []byte(`{"status":1,"message":"not found"}`)
	absent.StdoutSHA256 = replayBytesSHA256(absent.Output.Stdout)
	cleanup := SalesforceOrgCleanup{SchemaVersion: 1, BundleSHA256: bundleSHA, DevHub: bundle.DevHub, DevHubOrgID: bundle.DevHubOrgID, DevHubUsername: bundle.DevHubUsername, OrgAlias: alias, OrgID: orgID, Commands: []CommandResult{deleted, absent}, DevHubCommand: salesforceCommandForTest(t, bundlePath, []string{"org", "display", "--target-org", bundle.DevHub, "--json"}), ResidueAbsent: true}
	cleanup.DevHubCommand.Output.Stdout = append([]byte(`{"status":0,"result":{"id":"`), []byte(bundle.DevHubOrgID+`","status":"Active","username":"`+bundle.DevHubUsername+`"}}`)...)
	cleanup.DevHubCommand.StdoutSHA256 = replayBytesSHA256(cleanup.DevHubCommand.Output.Stdout)
	if err := WriteNewJSON(cleanupPath, cleanup); err != nil {
		t.Fatal(err)
	}
	_ = dispatch
	return SalesforceShardFiles{ShardPath: shardPath, DispatchPath: dispatchPath, CreationPath: creationPath, CleanupPath: cleanupPath, PreflightPath: preflightPath}
}

func TestSalesforceFixtureProjectManifestExcludesSourceTracking(t *testing.T) {
	files := map[string][]byte{
		"filter/projects/fixture/force-app/main/default/classes/Probe.cls": []byte("public class Probe {}\n"),
		"filter/projects/fixture/.sf/orgs/00D/source-tracking":             []byte("runtime state"),
	}
	manifest, err := salesforceFixtureProjectManifest(files, "filter/projects/fixture")
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest) != 1 || manifest[0].Path != "force-app/main/default/classes/Probe.cls" {
		t.Fatalf("manifest = %#v", manifest)
	}
}

func populateSalesforceProjectManifestsForTest(t *testing.T, resultsPath, executorRoot string) {
	t.Helper()
	result, _, err := readSalesforceFilterResults(resultsPath)
	if err != nil {
		t.Fatal(err)
	}
	files, _, err := readSalesforceExecutorFiles(executorRoot)
	if err != nil {
		t.Fatal(err)
	}
	for index := range result.Results {
		relative, err := filepath.Rel(executorRoot, result.Results[index].Project)
		if err != nil || filepath.IsAbs(relative) || strings.HasPrefix(relative, "..") {
			t.Fatal("invalid synthetic project path")
		}
		manifest, err := salesforceFixtureProjectManifest(files, filepath.ToSlash(relative))
		if err != nil {
			t.Fatal(err)
		}
		result.Results[index].ProjectManifest = manifest
	}
	updated, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(resultsPath, updated, 0o600); err != nil {
		t.Fatal(err)
	}
}

func salesforceFilterResultsForShard(bundlePath string, shard SalesforceShard, bundle OracleBundle, bundleSHA string) salesforceFilterResults {
	manifest, _, err := readExactJSONBytes[oracleTransportManifest](filepath.Join(filepath.Dir(bundlePath), "fixture-manifest.json"))
	if err != nil {
		panic(err)
	}
	results := make([]salesforceFilterFixtureResult, 0, len(shard.Results))
	for _, result := range shard.Results {
		var fixture oracleTransportFixture
		for _, candidate := range manifest.Fixtures {
			for _, surfaceID := range candidate.SurfaceIDs {
				if surfaceID == result.SurfaceID {
					fixture = candidate
					break
				}
			}
			if fixture.Fixture != "" {
				break
			}
		}
		if fixture.Fixture == "" {
			panic("missing fixture for synthetic Salesforce result")
		}
		stem, err := salesforceFixtureStem(fixture.Fixture)
		if err != nil {
			panic(err)
		}
		project := filepath.Join(shard.ExecutorRoot, "filter", "projects", stem)
		zero := 0
		kind := "check"
		if result.Kind == oracleRuntime {
			kind = "exec"
		}
		row := salesforceFilterFixtureResult{Fixture: fixture.Fixture, FixtureSHA256: fixture.SHA256, SourceFiles: fixture.SourceFiles, OrgIdentity: salesforceFilterOrgIdentity{Alias: shard.Preflight.OrgAlias, OrgID: shard.Preflight.OrgID, Username: shard.Preflight.OrgUsername}, Project: project, Invocation: salesforceInvocationForTest(project, shard.Preflight.OrgUsername, kind), SurfaceIDs: fixture.SurfaceIDs, Org: shard.OrgAlias, Kind: kind, ExitCode: &zero, Deployable: true, OrgCleanup: salesforceOrgCleanupForTest()}
		if result.Kind == oracleRuntime {
			passed := true
			row.RuntimePassed = &passed
			row.RuntimeResult = json.RawMessage(`{"status":0,"result":{"success":true,"compiled":true}}`)
		}
		results = append(results, row)
	}
	return salesforceFilterResults{Sealed: true, Orgs: []string{shard.OrgAlias}, Binding: salesforceFilterBinding{ManifestSHA256: bundle.TransportManifestSHA256, ProfileSHA256: bundle.ProfileSHA256, QueueSHA256: bundle.OraclePlanSHA256, SelectorSHA256: bundle.OraclePlanSHA256, SelectorReceiptSHA256: bundleSHA, CandidateCommit: bundle.Candidate.Commit, CandidateSHA256: bundle.Candidate.SHA256, ToolsCommit: bundle.Tools.Commit, ToolsAMD64SHA256: bundle.ToolsAMD64SHA256, WorkflowScriptSHA256: bundle.FilterSHA256, LocalSummarySHA256: bundle.LocalProofSummarySHA256}, OrgPostflight: salesforceFilterPostflight{MatchesPreflight: true}, Results: results}
}

func salesforceOrgCleanupForTest() CleanupReceipt {
	zero := 0
	hash, _ := sha256File("/usr/local/bin/sf")
	return CleanupReceipt{CleanupExitCode: &zero, Verification: &salesforceCleanupCheck{}, SFExecutableSHA256: hash, SFExecutableAfterSHA256: hash, ResidueAbsent: true}
}

func salesforceInvocationForTest(project, org, kind string) *salesforceFilterInvocation {
	hash, _ := sha256File("/usr/local/bin/sf")
	return &salesforceFilterInvocation{SFBinary: "/usr/local/bin/sf", Environment: map[string]string{"SF_USE_GENERIC_UNIX_KEYCHAIN": "true"}, TargetOrg: org, Commands: []salesforceFilterCommand{{Purpose: "deploy-or-exec", Args: expectedSalesforceCommand(project, org, kind), ExecutableSHA256: hash, ExecutableAfterSHA256: hash}}}
}

func writeSalesforceExecutorEvidenceForTest(t *testing.T, bundlePath string, shard SalesforceShard) {
	t.Helper()
	manifest, _, err := readExactJSONBytes[oracleTransportManifest](filepath.Join(filepath.Dir(bundlePath), "fixture-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	wanted := map[string]bool{}
	for _, result := range shard.Results {
		wanted[result.SurfaceID] = true
	}
	selection := []salesforceFilterSelection{}
	for _, fixture := range manifest.Fixtures {
		selected := false
		for _, surfaceID := range fixture.SurfaceIDs {
			selected = selected || wanted[surfaceID]
		}
		if !selected {
			continue
		}
		for _, surfaceID := range fixture.SurfaceIDs {
			if !wanted[surfaceID] {
				t.Fatalf("fixture %q split across synthetic shards", fixture.ID)
			}
		}
		kind, err := oracleTransportFixtureKind(filepath.Dir(bundlePath), fixture)
		if err != nil {
			t.Fatal(err)
		}
		selection = append(selection, salesforceFilterSelection{Fixture: fixture.Fixture, Coverage: len(fixture.SurfaceIDs), Kind: kind, SurfaceIDs: fixture.SurfaceIDs})
		stem, err := salesforceFixtureStem(fixture.Fixture)
		if err != nil {
			t.Fatal(err)
		}
		project := filepath.Join(sealedSalesforceFilterOutputPath(shard.ExecutorRoot), "projects", stem)
		if err := os.MkdirAll(project, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(project, "sfdx-project.json"), []byte(`{"packageDirectories":[{"path":"force-app"}]}`), 0o600); err != nil {
			t.Fatal(err)
		}
		classPath := filepath.Join(project, "force-app", "main", "default", "classes", "Fixture.cls")
		if err := os.MkdirAll(filepath.Dir(classPath), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(classPath, []byte("public class Fixture {}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		base := filepath.Join(project, "salesforce-"+shard.OrgAlias)
		deploy := []byte(`{"status":0,"result":{"status":"Succeeded","details":{"componentSuccesses":[{"fileName":"classes/Fixture.cls"}],"componentFailures":[]}}}`)
		if kind == "exec" {
			deploy = []byte(`{"status":0,"result":{"success":true,"compiled":true}}`)
		}
		if err := os.WriteFile(base+".json", deploy, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(base+".stderr", nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(base+".setup", nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if kind == "test" {
			if err := os.WriteFile(base+"-tests.json", []byte(`{"status":0,"result":{"summary":{"outcome":"Passed","failing":0,"testsRan":1}}}`), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(base+"-tests.stderr", nil, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	data, err := json.Marshal(selection)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sealedSalesforceFilterOutputPath(shard.ExecutorRoot), "selection.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func rewriteSalesforceExecutorManifestForTest(t *testing.T, executorRoot string) string {
	t.Helper()
	_, entries, err := readSalesforceExecutorFiles(executorRoot)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(salesforceExecutorManifest{SchemaVersion: 1, Files: entries})
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(executorRoot, salesforceExecutorManifestName), data, 0o600); err != nil {
		t.Fatal(err)
	}
	return replayBytesSHA256(data)
}

func mustFixedSalesforceEnvironment(t *testing.T) []string {
	t.Helper()
	environment, err := fixedSalesforceEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	return environment
}

func mustSealedPythonSHA(t *testing.T) string {
	t.Helper()
	hash, err := sealedPythonSHA256()
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

func shardLifecycleForTest(alias, id, bundleSHA string) SalesforceOrgPreflight {
	return SalesforceOrgPreflight{SchemaVersion: 1, BundleSHA256: bundleSHA, OrgAlias: alias, OrgID: id, OrgUsername: alias + "@example.invalid", OrgStatus: "Active", Inventory: salesforceBaselineInventoryForTest(), Commands: make([]CommandResult, len(salesforceInventoryTypes)+1)}
}

func salesforceBaselineInventoryForTest() SalesforceInventory {
	inventory := SalesforceInventory{Counts: map[string]int{}}
	for _, kind := range salesforceInventoryTypes {
		inventory.Counts[kind] = salesforceInventoryBaselineCount(kind)
	}
	return inventory
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
