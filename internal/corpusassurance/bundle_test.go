package corpusassurance

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestApprovedSalesforceFilterHashMatchesCheckedInScript(t *testing.T) {
	path := filepath.Join("..", "..", "scripts", "corpus-assurance", "salesforce-first-filter.py")
	hash, err := sha256File(path)
	if err != nil {
		t.Fatal(err)
	}
	if hash != approvedSalesforceFilterSHA256 {
		t.Fatalf("approved Salesforce filter hash = %q, checked-in script hash = %q", approvedSalesforceFilterSHA256, hash)
	}
}

func TestToolsAMD64BuildEnvironmentIgnoresAmbientToolchain(t *testing.T) {
	t.Setenv("GOROOT", "/attacker/go")
	t.Setenv("GOTOOLCHAIN", "go1.99.0")
	t.Setenv("PATH", "/attacker/bin")
	want := append(fixedReleaseEnvironment(), "CGO_ENABLED=0", "GOOS=darwin", "GOARCH=amd64", "GOFLAGS=")
	if got := toolsAMD64BuildEnvironment(); !equalStrings(got, want) {
		t.Fatalf("toolsAMD64BuildEnvironment = %#v, want %#v", got, want)
	}
}

func TestToolsAMD64BuildArgsForceFreshBuild(t *testing.T) {
	want := []string{"build", "-a", "-buildvcs=false", "-trimpath", "-o", "/sealed/bin/glade-tools-darwin-amd64", "./cmd/glade-tools"}
	if got := toolsAMD64BuildArgs("/sealed/bin/glade-tools-darwin-amd64"); !equalStrings(got, want) {
		t.Fatalf("toolsAMD64BuildArgs = %#v, want %#v", got, want)
	}
}

func TestValidateOracleBundleAllowsNonArm64BuildHost(t *testing.T) {
	bundle := OracleBundle{
		SchemaVersion:         1,
		Candidate:             RuntimeArtifact{Commit: strings.Repeat("a", 40), OS: "linux", Arch: "amd64", SHA256: strings.Repeat("b", 64)},
		Tools:                 RuntimeArtifact{Commit: strings.Repeat("c", 40), OS: "linux", Arch: "amd64", SHA256: strings.Repeat("d", 64)},
		ToolsAMD64:            RuntimeArtifact{Commit: strings.Repeat("c", 40), OS: "darwin", Arch: "amd64", SHA256: strings.Repeat("e", 64)},
		ToolsAMD64SHA256:      strings.Repeat("e", 64),
		DevHubAuthoritySHA256: strings.Repeat("f", 64),
		DevHub:                "sealed-dev-hub",
		DevHubOrgID:           "00D000000000001",
		DevHubUsername:        "sealed-dev-hub@example.invalid",
		SalesforceExecution:   testSalesforceExecutionAuthority(t),
	}
	path := filepath.Join(t.TempDir(), "bundle.json")
	if err := WriteNewJSON(path, bundle); err != nil {
		t.Fatal(err)
	}
	if err := ValidateOracleBundle(path); err == nil || err.Error() == "invalid oracle bundle" {
		t.Fatalf("non-arm64 build host rejected before staged-input validation: %v", err)
	}
}

func TestOracleReleaseValidationAcceptsSealedForeignRuntimeEnvironment(t *testing.T) {
	inputs := oracleBundleTestInputsForLocalProof(t)
	writeSealedReleaseValidation(t, inputs, inputs.attemptPath)
	validation, _, err := readExactJSONBytes[ReleaseValidation](inputs.releasePath)
	if err != nil {
		t.Fatal(err)
	}
	for index := range validation.Commands {
		validation.Commands[index].Environment[1] = "PATH=/foreign-go/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/usr/sbin:/bin"
		command := validation.Commands[index]
		command.CommandSpecSHA256 = releaseCommandSpecSHA256(releaseCommand{Path: command.Command[0], Args: command.Command[1:], WorkingDirectory: command.WorkingDirectory, Environment: command.Environment, Timeout: time.Duration(command.TimeoutMS) * time.Millisecond})
		validation.Commands[index] = command
	}
	if err := validateOracleReleaseValidation(validation, inputs.plan); err != nil {
		t.Fatalf("validateOracleReleaseValidation rejected a sealed foreign runtime: %v", err)
	}
	if err := validateOracleReleaseSources(validation, inputs.plan); err == nil {
		t.Fatal("local release provenance accepted a foreign runtime environment")
	}
}

func TestBuildOracleBundleRejectsEmptyAndFabricatedReleaseValidation(t *testing.T) {
	inputs := oracleBundleTestInputsForLocalProof(t)
	root := filepath.Dir(inputs.releasePath)
	for _, item := range []struct {
		name string
		data []byte
	}{
		{name: "empty", data: nil},
		{name: "fabricated", data: []byte(`{"status":"pass"}`)},
	} {
		t.Run(item.name, func(t *testing.T) {
			if err := os.WriteFile(inputs.releasePath, item.data, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := BuildOracleBundle(inputs.request(filepath.Join(root, "salesforce-worker-"+item.name))); err == nil {
				t.Fatalf("BuildOracleBundle accepted %s release validation", item.name)
			}
		})
	}
}

func TestBuildOracleBundleRejectsReleaseValidationWithoutSourceProvenance(t *testing.T) {
	inputs := oracleBundleTestInputsForLocalProof(t)
	writeSealedReleaseValidation(t, inputs, inputs.attemptPath)
	validation, _, err := readExactJSONBytes[ReleaseValidation](inputs.releasePath)
	if err != nil {
		t.Fatal(err)
	}
	validation.GladeRoot = ""
	data, err := json.Marshal(validation)
	if err != nil || os.WriteFile(inputs.releasePath, append(data, '\n'), 0o600) != nil {
		t.Fatal(err)
	}
	if _, err := BuildOracleBundle(inputs.request(filepath.Join(t.TempDir(), "bundle"))); err == nil {
		t.Fatal("BuildOracleBundle accepted a release validation without sealed source provenance")
	}
}

func TestBuildOracleBundleRequiresTheAttemptBoundByReleaseValidation(t *testing.T) {
	inputs := oracleBundleTestInputsForLocalProof(t)
	writeSealedReleaseValidation(t, inputs, inputs.attemptPath)
	request := inputs.request(filepath.Join(t.TempDir(), "bundle"))
	request.AttemptPath = filepath.Join(t.TempDir(), "missing-ATTEMPT.json")
	if _, err := BuildOracleBundle(request); err == nil {
		t.Fatal("BuildOracleBundle accepted a release validation without its authoritative attempt")
	}
}

func TestBuildOracleBundleBindsRemoteCleanupAuthority(t *testing.T) {
	inputs := oracleBundleTestInputsForLocalProof(t)
	writeSealedReleaseValidation(t, inputs, inputs.attemptPath)
	output := filepath.Join(t.TempDir(), "salesforce-worker")
	bundle, err := BuildOracleBundle(inputs.request(output))
	if err != nil {
		t.Fatal(err)
	}
	if bundle.SalesforceRemoteCleanupAuthoritySHA256 != localProofFileSHA256(t, inputs.remoteAuthorityPath) {
		t.Fatalf("remote cleanup authority hash = %q", bundle.SalesforceRemoteCleanupAuthoritySHA256)
	}
	if _, err := os.Stat(filepath.Join(output, "bundle", "SALESFORCE_REMOTE_CLEANUP_AUTHORITY.json")); err != nil {
		t.Fatalf("staged remote cleanup authority: %v", err)
	}
}

func TestValidateOracleBundleRejectsNewBundleWithoutRemoteCleanupAuthority(t *testing.T) {
	inputs := oracleBundleTestInputsForLocalProof(t)
	writeSealedReleaseValidation(t, inputs, inputs.attemptPath)
	outputRoot := filepath.Join(t.TempDir(), "salesforce-worker")
	if _, err := BuildOracleBundle(inputs.request(outputRoot)); err != nil {
		t.Fatal(err)
	}
	bundlePath := filepath.Join(outputRoot, "bundle", "bundle.json")
	bundle, _, err := readExactJSONBytes[OracleBundle](bundlePath)
	if err != nil {
		t.Fatal(err)
	}
	bundle.SalesforceRemoteCleanupAuthoritySHA256 = ""
	bundleBytes, err := json.Marshal(bundle)
	if err != nil || os.WriteFile(bundlePath, append(bundleBytes, '\n'), 0o600) != nil {
		t.Fatal(err)
	}
	if err := ValidateOracleBundle(bundlePath); err == nil {
		t.Fatal("ValidateOracleBundle accepted a new bundle without remote cleanup authority")
	}
}

func TestBuildOracleBundleRejectsAReplacementAttempt(t *testing.T) {
	inputs := oracleBundleTestInputsForLocalProof(t)
	writeSealedReleaseValidation(t, inputs, inputs.attemptPath)
	bytes, err := os.ReadFile(inputs.attemptPath)
	if err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(t.TempDir(), "ATTEMPT.json")
	if err := os.WriteFile(replacement, append(bytes, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	request := inputs.request(filepath.Join(t.TempDir(), "bundle"))
	request.AttemptPath = replacement
	if _, err := BuildOracleBundle(request); err == nil {
		t.Fatal("BuildOracleBundle accepted a replacement attempt")
	}
}

func TestBuildOracleBundleRejectsReleaseValidationFromAnotherValidAttempt(t *testing.T) {
	inputs := oracleBundleTestInputsForLocalProof(t)
	attempt, _, err := readExactJSONBytes[AssuranceAttempt](inputs.attemptPath)
	if err != nil {
		t.Fatal(err)
	}
	attempt.InventorySHA256 = strings.Repeat("f", 64)
	otherAttemptPath := filepath.Join(t.TempDir(), "ATTEMPT.json")
	if err := WriteNewJSON(otherAttemptPath, attempt); err != nil {
		t.Fatal(err)
	}
	writeSealedReleaseValidation(t, inputs, otherAttemptPath)
	request := inputs.request(filepath.Join(t.TempDir(), "bundle"))
	request.AttemptPath = otherAttemptPath
	if _, err := BuildOracleBundle(request); err == nil {
		t.Fatal("BuildOracleBundle mixed a local proof and release validation from different attempts")
	}
}

func TestBuildOracleBundleRejectsAnUnsealedToolsSourceRoot(t *testing.T) {
	inputs := oracleBundleTestInputsForLocalProof(t)
	writeSealedReleaseValidation(t, inputs, inputs.attemptPath)
	request := inputs.request(filepath.Join(t.TempDir(), "bundle"))
	request.ToolsRoot = t.TempDir()
	if _, err := BuildOracleBundle(request); err == nil {
		t.Fatal("BuildOracleBundle accepted an unsealed tools source root")
	}
}

func TestBuildOracleBundleRejectsAnotherCheckoutAtTheSameToolsCommit(t *testing.T) {
	inputs := oracleBundleTestInputsForLocalProof(t)
	writeSealedReleaseValidation(t, inputs, inputs.attemptPath)
	alternateRoot := filepath.Join(t.TempDir(), "glade-tools")
	gitRun(t, filepath.Dir(alternateRoot), "clone", "--quiet", inputs.toolsRoot, alternateRoot)
	request := inputs.request(filepath.Join(t.TempDir(), "salesforce-worker"))
	request.ToolsRoot = alternateRoot
	if _, err := BuildOracleBundle(request); err == nil || !strings.Contains(err.Error(), "tools source does not match sealed release validation") {
		t.Fatalf("BuildOracleBundle accepted another checkout at the same tools commit: %v", err)
	}
}

func TestBuildOracleBundleRequiresBoundSurfaceWavePlan(t *testing.T) {
	inputs := oracleBundleTestInputsForLocalProof(t)
	writeSealedReleaseValidation(t, inputs, inputs.attemptPath)
	inputs.plan.SurfaceWavePlanSHA256 = strings.Repeat("f", 64)
	writeLocalProofJSON(t, inputs.planPath, inputs.plan)
	authority, _, err := readExactJSONBytes[ExclusionAuthority](inputs.authorityPath)
	if err != nil {
		t.Fatal(err)
	}
	authority.PlanSHA256 = localProofFileSHA256(t, inputs.planPath)
	writeLocalProofJSON(t, inputs.authorityPath, authority)
	if _, err := BuildOracleBundle(inputs.request(filepath.Join(t.TempDir(), "salesforce-worker"))); err == nil || !strings.Contains(err.Error(), "surface wave plan is required") {
		t.Fatalf("missing wave plan error = %v", err)
	}
}

type oracleBundleTestInputs struct {
	proof               LocalProof
	plan                OraclePlan
	gladeRoot           string
	attemptPath         string
	remoteAuthorityPath string
	devHubAuthorityPath string
	profilePath         string
	planPath            string
	authorityPath       string
	releasePath         string
	filterPath          string
	scratchPath         string
	toolsRoot           string
	localProofPath      string
	fixtureManifestPath string
	filterSHA256        string
}

func (inputs oracleBundleTestInputs) request(outputPath string) OracleBundleRequest {
	return OracleBundleRequest{
		AttemptPath:                inputs.attemptPath,
		RemoteCleanupAuthorityPath: inputs.remoteAuthorityPath,
		DevHubAuthorityPath:        inputs.devHubAuthorityPath,
		ProfilePath:                inputs.profilePath,
		PlanPath:                   inputs.planPath,
		AuthorityPath:              inputs.authorityPath,
		ReleaseValidationPath:      inputs.releasePath,
		LocalProofPath:             inputs.localProofPath,
		FixtureManifestPath:        inputs.fixtureManifestPath,
		FilterScriptPath:           inputs.filterPath,
		ScratchDefinitionPath:      inputs.scratchPath,
		ToolsRoot:                  inputs.toolsRoot,
		OutputPath:                 outputPath, expectedFilterSHA256: inputs.filterSHA256,
	}
}

func oracleBundleTestInputsForLocalProof(t *testing.T) oracleBundleTestInputs {
	t.Helper()
	request, _ := localProofRequest(t)
	proof, err := RunLocalProof(request)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Dir(request.OutputPath)
	devHubAuthorityPath := filepath.Join(root, "DEV_HUB_AUTHORITY.json")
	if err := WriteNewJSON(devHubAuthorityPath, testSalesforceDevHubAuthority(t, "sealed-dev-hub", "00D000000000001", "sealed-dev-hub@example.invalid", testSalesforceExecutionAuthority(t))); err != nil {
		t.Fatal(err)
	}
	gladeRoot := newInventoryRepository(t, map[string]string{"go.mod": "module example.com/glade\n\ngo 1.23.0\n", "scripts/smoke.sh": "#!/bin/sh\n"})
	toolsRoot := newInventoryRepository(t, map[string]string{"go.mod": "module example.com/glade-tools\n\ngo 1.23.0\n", "cmd/glade-tools/main.go": "package main\nfunc main() {}\n", "scripts/release-check.sh": "#!/bin/sh\n"})
	candidate, tools := proof.Candidate, proof.Tools
	candidate.Commit, tools.Commit = testGitOutput(t, gladeRoot, "rev-parse", "HEAD"), testGitOutput(t, toolsRoot, "rev-parse", "HEAD")
	replaceAssuranceAttemptForRuntimes(t, request.AttemptPath, candidate, tools)
	proof.Candidate, proof.Tools = candidate, tools
	attempt, _, err := readExactJSONBytes[AssuranceAttempt](request.AttemptPath)
	if err != nil {
		t.Fatal(err)
	}
	remoteAuthorityPath := filepath.Join(root, "SALESFORCE_REMOTE_CLEANUP_AUTHORITY.json")
	parent := filepath.Join(root, "glade-assurance-test")
	if err := os.MkdirAll(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	remoteAuthority := RemoteAttemptAuthority{SchemaVersion: 2, AttemptSHA256: attemptBindingHash(attempt), Role: "salesforce-worker", Host: "operator@salesforce-worker", Parent: parent, AttemptRoot: filepath.Join(parent, "assurance-"+attemptBindingHash(attempt)[:16]+"-test-salesforce-worker")}
	if err := WriteNewJSON(remoteAuthorityPath, remoteAuthority); err != nil {
		t.Fatal(err)
	}
	attempt.RemoteCleanupAuthoritySHA256 = map[string]string{"replay-worker": strings.Repeat("0", 64), "salesforce-worker": localProofFileSHA256(t, remoteAuthorityPath)}
	attemptBytes, err := json.Marshal(attempt)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(request.AttemptPath, append(attemptBytes, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	proof.AttemptSHA256 = attemptHash(attempt)
	if data, err := json.Marshal(proof); err != nil || os.WriteFile(request.OutputPath, append(data, '\n'), 0o600) != nil {
		t.Fatal(err)
	}
	inputs := oracleBundleTestInputs{
		proof:               proof,
		gladeRoot:           gladeRoot,
		attemptPath:         request.AttemptPath,
		remoteAuthorityPath: remoteAuthorityPath,
		devHubAuthorityPath: devHubAuthorityPath,
		profilePath:         filepath.Join(root, "BUNDLE_PROFILE.json"),
		planPath:            filepath.Join(root, "ORACLE_PLAN.json"),
		authorityPath:       filepath.Join(root, "EXCLUSION_AUTHORITY.json"),
		releasePath:         filepath.Join(root, "RELEASE_VALIDATION.json"),
		filterPath:          filepath.Join(root, "filter.py"),
		scratchPath:         filepath.Join(root, "scratch.json"),
		toolsRoot:           toolsRoot,
		localProofPath:      request.OutputPath,
		fixtureManifestPath: request.FixtureManifestPath,
	}
	profile := AssuranceProfile{SchemaVersion: 1, FixtureManifestSHA256: localProofFileSHA256(t, request.FixtureManifestPath), LocalProofSHA256: localProofFileSHA256(t, request.OutputPath)}
	if err := WriteNewJSON(inputs.profilePath, profile); err != nil {
		t.Fatal(err)
	}
	inputs.plan = OraclePlan{Candidate: proof.Candidate, Tools: proof.Tools, ProfileSHA256: localProofFileSHA256(t, inputs.profilePath), Rows: []OraclePlanRow{{SurfaceID: "apex:Runtime.run", Action: oracleRuntime}}}
	if err := WriteNewJSON(inputs.planPath, inputs.plan); err != nil {
		t.Fatal(err)
	}
	authority := ExclusionAuthority{Candidate: proof.Candidate, Tools: proof.Tools, PlanSHA256: localProofFileSHA256(t, inputs.planPath), ProfileSHA256: localProofFileSHA256(t, inputs.profilePath), SealedUsageSHA256: strings.Repeat("a", 64), DecisionSHA256: strings.Repeat("b", 64), LocalProofSHA256: localProofFileSHA256(t, request.OutputPath), PolicySHA256: strings.Repeat("c", 64)}
	if err := WriteNewJSON(inputs.authorityPath, authority); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inputs.filterPath, []byte("#!/usr/bin/env python3\nimport argparse\np=argparse.ArgumentParser()\np.add_argument('--tools-amd64-sha256')\np.parse_args()\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	inputs.filterSHA256 = localProofFileSHA256(t, inputs.filterPath)
	previousFilterAuthority := testApprovedSalesforceFilterSHA256
	testApprovedSalesforceFilterSHA256 = inputs.filterSHA256
	t.Cleanup(func() { testApprovedSalesforceFilterSHA256 = previousFilterAuthority })
	if err := os.WriteFile(inputs.scratchPath, []byte(`{"orgName":"Glade Assurance","edition":"Developer","features":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return inputs
}

func TestBuildOracleBundleRequiresExecutableAMD64FilterContract(t *testing.T) {
	inputs := oracleBundleTestInputsForLocalProof(t)
	writeSealedReleaseValidation(t, inputs, inputs.attemptPath)
	if err := os.WriteFile(inputs.filterPath, []byte("#!/usr/bin/env python3\nprint('no sealed tools binding')\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildOracleBundle(inputs.request(filepath.Join(t.TempDir(), "salesforce-worker"))); err == nil {
		t.Fatal("accepted a filter without the amd64 tools contract")
	}
}

func TestBuildOracleBundleRequiresSalesforceWorkerOutputRoot(t *testing.T) {
	inputs := oracleBundleTestInputsForLocalProof(t)
	writeSealedReleaseValidation(t, inputs, inputs.attemptPath)
	if _, err := BuildOracleBundle(inputs.request(filepath.Join(t.TempDir(), "wrong-root"))); err == nil {
		t.Fatal("BuildOracleBundle accepted an output root that dispatch cannot consume")
	}
}

func TestValidateOracleBundleRejectsCallerForgedFilterAuthority(t *testing.T) {
	inputs := oracleBundleTestInputsForLocalProof(t)
	writeSealedReleaseValidation(t, inputs, inputs.attemptPath)
	outputRoot := filepath.Join(t.TempDir(), "salesforce-worker")
	if _, err := BuildOracleBundle(inputs.request(outputRoot)); err != nil {
		t.Fatal(err)
	}
	testApprovedSalesforceFilterSHA256 = ""
	if err := ValidateOracleBundle(filepath.Join(outputRoot, "bundle", "bundle.json")); err == nil {
		t.Fatal("accepted a bundle whose caller-selected filter hash differs from production authority")
	}
}

func writeSealedReleaseValidation(t *testing.T, inputs oracleBundleTestInputs, attemptPath string) {
	t.Helper()
	freezePath := filepath.Join(filepath.Dir(inputs.releasePath), "FINAL_TOOLS_COMMIT")
	if err := os.WriteFile(freezePath, []byte(inputs.plan.Tools.Commit+"\n"), 0o400); err != nil {
		t.Fatal(err)
	}
	if _, err := RunReleaseValidation(ReleaseValidationRequest{AttemptPath: attemptPath, GladeRoot: inputs.gladeRoot, CandidatePath: inputs.proof.CandidatePath, ToolsRoot: inputs.toolsRoot, ToolsPath: inputs.proof.ToolsPath, ToolsFreezePath: freezePath, OutputPath: inputs.releasePath, runner: func(context.Context, releaseCommand) (salesforceCommandOutput, error) {
		return salesforceCommandOutput{}, nil
	}}); err != nil {
		t.Fatal(err)
	}
}
