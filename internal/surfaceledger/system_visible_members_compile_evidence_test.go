package surfaceledger

import (
	"path/filepath"
	"testing"
)

func TestSystemVisibleMembersCompileRowsHaveExactDualOracleEvidence(t *testing.T) {
	toolsRoot := filepath.Join("..", "..")
	fixturePath := filepath.Join(toolsRoot, "docs", "fixtures", "current-base-system-visible-members-compile-api67.json")
	comparisonPath := filepath.Join(toolsRoot, "docs", "fixtures", "salesforce-system-visible-members-compile-comparisons.json")
	var fixture struct {
		Name string `json:"name"`
		API string `json:"apiVersion"`
		Mode string `json:"mode"`
		Command struct { Args []string `json:"args"` } `json:"command"`
		Evidence []struct { SurfaceID string `json:"surfaceId"` } `json:"evidence"`
	}
	readJSON(t, fixturePath, &fixture)
	if fixture.Name != "current-base-system-visible-members-compile-api67" || fixture.API != "67.0" || fixture.Mode != "compile-shape" || len(fixture.Command.Args) != 1 || len(fixture.Evidence) != 170 {
		t.Fatalf("fixture metadata = %#v", fixture)
	}
	var comparison struct {
		Candidate struct { Commit, SHA256 string } `json:"candidate"`
		Local struct { CandidatePath, CandidateSha256, SourcePath, SourceSha256, ReportPath, ReportSha256 string } `json:"local"`
		Salesforce struct { TargetOrgAlias, APIVersion, ExecutePath, ExecuteSha256 string } `json:"salesforce"`
		LocalFixtures []struct { Path, SHA256 string } `json:"localFixtures"`
		Comparisons []struct { CaseID, Status string; SurfaceIDs []string } `json:"comparisons"`
	}
	readJSON(t, comparisonPath, &comparison)
	if comparison.Candidate.Commit != "6419bf1e8ede470d9fd5c6c789aede9ef5d2713d" || comparison.Candidate.SHA256 != "35c3cd0c023384574381d390ab899d363e6bef1b0d3b88cd9e9653c8fb2887bb" || comparison.Salesforce.TargetOrgAlias != "glade-sf-correctness" || comparison.Salesforce.APIVersion != "67.0" || len(comparison.LocalFixtures) != 1 || len(comparison.Comparisons) != 1 || comparison.Comparisons[0].Status != "pass" {
		t.Fatalf("comparison metadata = %#v", comparison)
	}
	if comparison.LocalFixtures[0].SHA256 != "0d7e839416cd654e65f94a93e7070b9b17b766b3406ae35468875046237576d9" {
		t.Fatalf("fixture hash = %s", comparison.LocalFixtures[0].SHA256)
	}
	fixtureEvidence, err := BuildEvidenceSnapshot([]string{fixturePath})
	if err != nil { t.Fatal(err) }
	oracleEvidence, err := BuildOracleEvidenceSnapshot([]string{comparisonPath})
	if err != nil { t.Fatal(err) }
	wantIDs := make([]string, 0, len(fixtureEvidence))
	for _, row := range fixtureEvidence { wantIDs = append(wantIDs, row.SurfaceID) }
	assertExactIDs(t, wantIDs, comparison.Comparisons[0].SurfaceIDs)
	oracleIDs := make([]string, 0, len(oracleEvidence))
	for _, row := range oracleEvidence { oracleIDs = append(oracleIDs, row.SurfaceID) }
	assertExactIDs(t, wantIDs, oracleIDs)
	evidenceRoot, err := filepath.Abs(filepath.Join(toolsRoot, "..", ".."))
	if err != nil { t.Fatal(err) }
	for path, hash := range map[string]string{
		comparison.Local.CandidatePath: comparison.Local.CandidateSha256,
		comparison.Local.SourcePath: comparison.Local.SourceSha256,
		comparison.Local.ReportPath: comparison.Local.ReportSha256,
		comparison.Salesforce.ExecutePath: comparison.Salesforce.ExecuteSha256,
	} { assertMetadataDTOBatchSHA256(t, filepath.Join(evidenceRoot, path), hash) }
	var local struct { Status string `json:"status"`; ExitCode int `json:"exitCode"` }
	readJSON(t, filepath.Join(evidenceRoot, comparison.Local.ReportPath), &local)
	if local.Status != "passed" || local.ExitCode != 0 { t.Fatalf("local report = %#v", local) }
	var sf struct { Status int `json:"status"`; Result struct { Success, Compiled bool; CompileProblem, ExceptionMessage string } `json:"result"` }
	readJSON(t, filepath.Join(evidenceRoot, comparison.Salesforce.ExecutePath), &sf)
	if sf.Status != 0 || !sf.Result.Success || !sf.Result.Compiled || sf.Result.CompileProblem != "" || sf.Result.ExceptionMessage != "" { t.Fatalf("Salesforce report = %#v", sf) }
}
