package corpusassurance

import (
	"context"
	"encoding/json"
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

type oracleBundleTestInputs struct {
	proof               LocalProof
	plan                OraclePlan
	gladeRoot           string
	attemptPath         string
	profilePath         string
	planPath            string
	authorityPath       string
	releasePath         string
	filterPath          string
	scratchPath         string
	toolsRoot           string
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
		ToolsRoot:             inputs.toolsRoot,
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
	gladeRoot := newInventoryRepository(t, map[string]string{"go.mod": "module example.com/glade\n\ngo 1.23.0\n", "scripts/smoke.sh": "#!/bin/sh\n"})
	toolsRoot := newInventoryRepository(t, map[string]string{"go.mod": "module example.com/glade-tools\n\ngo 1.23.0\n", "cmd/glade-tools/main.go": "package main\nfunc main() {}\n", "scripts/release-check.sh": "#!/bin/sh\n"})
	candidate, tools := proof.Candidate, proof.Tools
	candidate.Commit, tools.Commit = testGitOutput(t, gladeRoot, "rev-parse", "HEAD"), testGitOutput(t, toolsRoot, "rev-parse", "HEAD")
	replaceAssuranceAttemptForRuntimes(t, request.AttemptPath, candidate, tools)
	proof.Candidate, proof.Tools = candidate, tools
	proof.AttemptSHA256 = attemptHash(AssuranceAttempt{SchemaVersion: 1, InventorySHA256: strings.Repeat("a", 64), CandidateAuthoritySHA256: strings.Repeat("b", 64), Candidate: candidate, Tools: tools})
	if data, err := json.Marshal(proof); err != nil || os.WriteFile(request.OutputPath, append(data, '\n'), 0o600) != nil {
		t.Fatal(err)
	}
	inputs := oracleBundleTestInputs{
		proof:               proof,
		gladeRoot:           gladeRoot,
		attemptPath:         request.AttemptPath,
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
	if err := os.WriteFile(inputs.filterPath, []byte("#!/usr/bin/env python3\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inputs.scratchPath, []byte(`{"orgName":"Glade Assurance","edition":"Developer","features":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return inputs
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
