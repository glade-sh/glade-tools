package corpusassurance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildSurfaceOraclePlanProjectsExactWave(t *testing.T) {
	waveRequest, _, _ := surfaceWavePlanRequest(t)
	if _, err := BuildSurfaceWavePlan(waveRequest); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(filepath.Dir(waveRequest.OutputPath), "oracle-wave")
	artifacts, err := BuildSurfaceOraclePlan(SurfaceOraclePlanRequest{
		WavePlanPath:        waveRequest.OutputPath,
		ScopePath:           waveRequest.ScopePath,
		ProfilePath:         waveRequest.ProfilePath,
		LocalProofPath:      waveRequest.LocalProofPath,
		FixtureManifestPath: waveRequest.FixtureManifestPath,
		CoveragePath:        waveRequest.CoveragePath,
		OutputPath:          output,
	})
	if err != nil {
		t.Fatal(err)
	}
	if artifacts.Profile.Total != 2 || artifacts.Plan.SurfaceWavePlanSHA256 != localProofFileSHA256(t, waveRequest.OutputPath) || artifacts.Profile.SurfaceWavePlanSHA256 != artifacts.Plan.SurfaceWavePlanSHA256 {
		t.Fatalf("wave bindings = %#v / %#v", artifacts.Profile, artifacts.Plan)
	}
	if len(artifacts.Plan.Rows) != 2 || artifacts.Plan.Rows[0].SurfaceID != "apex:Mock.run" || artifacts.Plan.Rows[0].Action != oracleCompile || artifacts.Plan.Rows[1].SurfaceID != "apex:Runtime.run" || artifacts.Plan.Rows[1].Action != oracleRuntime {
		t.Fatalf("oracle rows = %#v", artifacts.Plan.Rows)
	}
	if artifacts.Authority.SalesforceParityCredit != 0 || len(artifacts.Authority.Rows) != 0 || artifacts.Authority.PlanSHA256 != localProofFileSHA256(t, filepath.Join(output, "ORACLE_PLAN.json")) {
		t.Fatalf("exclusion authority = %#v", artifacts.Authority)
	}
	wave, waveBytes, err := readExactJSONBytes[SurfaceWavePlan](waveRequest.OutputPath)
	if err != nil {
		t.Fatal(err)
	}
	proof, proofBytes, err := readExactJSONBytes[LocalProof](waveRequest.LocalProofPath)
	if err != nil {
		t.Fatal(err)
	}
	manifest, manifestBytes, err := readExactJSONBytes[LocalProofFixtureManifest](waveRequest.FixtureManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	selectedManifest, err := surfaceWaveBundleManifest(artifacts.Plan, artifacts.Profile, proof, manifest, wave, replayBytesSHA256(waveBytes), replayBytesSHA256(proofBytes), replayBytesSHA256(manifestBytes))
	if err != nil || len(selectedManifest.Fixtures) != 2 || len(selectedManifest.SalesforceFixtures) != 2 {
		t.Fatalf("bundle manifest = %#v, %v", selectedManifest, err)
	}
	shortPlan := artifacts.Plan
	shortPlan.Rows = shortPlan.Rows[:1]
	if _, err := surfaceWaveBundleManifest(shortPlan, artifacts.Profile, proof, manifest, wave, replayBytesSHA256(waveBytes), replayBytesSHA256(proofBytes), replayBytesSHA256(manifestBytes)); err == nil {
		t.Fatal("bundle manifest accepted an incomplete Oracle plan")
	}
	if mode := surfaceOraclePlanFileMode(t, output); mode != 0o700 {
		t.Fatalf("output mode = %o", mode)
	}
	for _, name := range []string{"ASSURANCE_PROFILE.json", "ORACLE_PLAN.json", "EXCLUSION_AUTHORITY.json"} {
		if mode := surfaceOraclePlanFileMode(t, filepath.Join(output, name)); mode != 0o600 {
			t.Fatalf("%s mode = %o", name, mode)
		}
	}
	campaignScope := filepath.Join(filepath.Dir(output), "campaign-scope.json")
	if _, err := BuildSurfaceOracleCampaignScope(filepath.Join(output, "ORACLE_PLAN.json"), filepath.Join(output, "ASSURANCE_PROFILE.json"), campaignScope); err != nil {
		t.Fatal(err)
	}
}

func TestBuildSurfaceOraclePlanRejectsChangedWaveAndExistingOutput(t *testing.T) {
	waveRequest, _, _ := surfaceWavePlanRequest(t)
	if _, err := BuildSurfaceWavePlan(waveRequest); err != nil {
		t.Fatal(err)
	}
	wave, _, err := readExactJSONBytes[SurfaceWavePlan](waveRequest.OutputPath)
	if err != nil {
		t.Fatal(err)
	}
	wave.Candidate.SHA256 = strings.Repeat("f", 64)
	writeLocalProofJSON(t, waveRequest.OutputPath, wave)
	request := SurfaceOraclePlanRequest{
		WavePlanPath: waveRequest.OutputPath, ScopePath: waveRequest.ScopePath, ProfilePath: waveRequest.ProfilePath,
		LocalProofPath: waveRequest.LocalProofPath, FixtureManifestPath: waveRequest.FixtureManifestPath,
		CoveragePath: waveRequest.CoveragePath, OutputPath: filepath.Join(filepath.Dir(waveRequest.OutputPath), "oracle-wave"),
	}
	if _, err := BuildSurfaceOraclePlan(request); err == nil || !strings.Contains(err.Error(), "authoritative recomputation") {
		t.Fatalf("changed wave error = %v", err)
	}
	if err := os.Mkdir(request.OutputPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildSurfaceOraclePlan(request); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("existing output error = %v", err)
	}
}

func surfaceOraclePlanFileMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}
