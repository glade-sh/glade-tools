package corpusassurance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
			if _, err := BuildOracleBundle(inputs.request(filepath.Join(root, "razor-"+item.name))); err == nil {
				t.Fatalf("BuildOracleBundle accepted %s release validation", item.name)
			}
		})
	}
}

func TestBuildOracleBundleRequiresTheAttemptBoundByReleaseValidation(t *testing.T) {
	inputs := oracleBundleTestInputsForLocalProof(t)
	writeSealedReleaseValidation(t, inputs.releasePath, inputs.attemptPath, inputs.plan.Candidate, inputs.plan.Tools)
	request := inputs.request(filepath.Join(t.TempDir(), "bundle"))
	request.AttemptPath = filepath.Join(t.TempDir(), "missing-ATTEMPT.json")
	if _, err := BuildOracleBundle(request); err == nil {
		t.Fatal("BuildOracleBundle accepted a release validation without its authoritative attempt")
	}
}

func TestBuildOracleBundleRejectsAReplacementAttempt(t *testing.T) {
	inputs := oracleBundleTestInputsForLocalProof(t)
	writeSealedReleaseValidation(t, inputs.releasePath, inputs.attemptPath, inputs.plan.Candidate, inputs.plan.Tools)
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
	writeSealedReleaseValidation(t, inputs.releasePath, otherAttemptPath, inputs.plan.Candidate, inputs.plan.Tools)
	request := inputs.request(filepath.Join(t.TempDir(), "bundle"))
	request.AttemptPath = otherAttemptPath
	if _, err := BuildOracleBundle(request); err == nil {
		t.Fatal("BuildOracleBundle mixed a local proof and release validation from different attempts")
	}
}

type oracleBundleTestInputs struct {
	proof               LocalProof
	plan                OraclePlan
	attemptPath         string
	profilePath         string
	planPath            string
	authorityPath       string
	releasePath         string
	filterPath          string
	scratchPath         string
	toolsPath           string
	localProofPath      string
	fixtureManifestPath string
}

func (inputs oracleBundleTestInputs) request(outputPath string) OracleBundleRequest {
	return OracleBundleRequest{
		AttemptPath:           inputs.attemptPath,
		ProfilePath:           inputs.profilePath,
		PlanPath:              inputs.planPath,
		AuthorityPath:         inputs.authorityPath,
		ReleaseValidationPath: inputs.releasePath,
		LocalProofPath:        inputs.localProofPath,
		FixtureManifestPath:   inputs.fixtureManifestPath,
		FilterScriptPath:      inputs.filterPath,
		ScratchDefinitionPath: inputs.scratchPath,
		ToolsAMD64Path:        inputs.toolsPath,
		OutputPath:            outputPath,
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
	inputs := oracleBundleTestInputs{
		attemptPath:         request.AttemptPath,
		profilePath:         filepath.Join(root, "BUNDLE_PROFILE.json"),
		planPath:            filepath.Join(root, "ORACLE_PLAN.json"),
		authorityPath:       filepath.Join(root, "EXCLUSION_AUTHORITY.json"),
		releasePath:         filepath.Join(root, "RELEASE_VALIDATION.json"),
		filterPath:          filepath.Join(root, "filter.py"),
		scratchPath:         filepath.Join(root, "scratch.json"),
		toolsPath:           request.ToolsPath,
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
	if err := os.WriteFile(inputs.filterPath, []byte("#!/usr/bin/env python3\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inputs.scratchPath, []byte(`{"orgName":"Glade Assurance","edition":"Developer","features":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return inputs
}

func writeSealedReleaseValidation(t *testing.T, path, attemptPath string, candidate, tools RuntimeArtifact) {
	t.Helper()
	environment := []string{"HOME=/var/empty", "PATH=/usr/local/bin:/usr/bin:/bin", "TMPDIR=/private/tmp", "GOCACHE=/private/tmp/glade-assurance-go-cache", "GOMODCACHE=/Users/matt/go/pkg/mod"}
	commands := []releaseCommand{
		{Path: "/usr/local/bin/go", Args: []string{"test", "./..."}, WorkingDirectory: "/glade", Environment: environment, Timeout: releaseValidationTimeout},
		{Path: "/glade/scripts/smoke.sh", WorkingDirectory: "/glade", Environment: environment, Timeout: releaseValidationTimeout},
		{Path: "/usr/local/bin/go", Args: []string{"test", "./..."}, WorkingDirectory: "/glade-tools", Environment: environment, Timeout: releaseValidationTimeout},
		{Path: "/glade-tools/scripts/release-check.sh", WorkingDirectory: "/glade-tools", Environment: environment, Timeout: releaseValidationTimeout},
	}
	results := make([]ReleaseCommandResult, 0, len(commands))
	for _, command := range commands {
		results = append(results, ReleaseCommandResult{
			CommandResult: CommandResult{
				Command:           append([]string{command.Path}, command.Args...),
				CommandSpecSHA256: releaseCommandSpecSHA256(command),
				ExitCode:          0,
				DurationMS:        1,
				StdoutSHA256:      strings.Repeat("1", 64),
				StderrSHA256:      strings.Repeat("2", 64),
				Passed:            true,
			},
			WorkingDirectory: command.WorkingDirectory,
			Environment:      append([]string(nil), command.Environment...),
			TimeoutMS:        command.Timeout.Milliseconds(),
		})
	}
	attemptHash, err := sha256File(attemptPath)
	if err != nil {
		t.Fatal(err)
	}
	validation := ReleaseValidation{SchemaVersion: 1, AttemptSHA256: attemptHash, Candidate: candidate, Tools: tools, ToolsFreezeSHA256: strings.Repeat("a", 64), Commands: results}
	if err := WriteNewJSON(path, validation); err != nil {
		t.Fatal(err)
	}
}
