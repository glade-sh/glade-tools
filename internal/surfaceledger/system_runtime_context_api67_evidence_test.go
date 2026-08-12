package surfaceledger

import (
    "os"
    "path/filepath"
    "strings"
    "testing"
)

type systemRuntimeContextFixture struct {
    Name string `json:"name"`
    Command struct { Kind string `json:"kind"` } `json:"command"`
    Evidence []struct { SurfaceID string `json:"surfaceId"` } `json:"evidence"`
    Source []struct { Content string `json:"content"` } `json:"source"`
}

func TestSystemRuntimeContextAPI67FixtureIsSourceBacked(t *testing.T) {
    toolsRoot := filepath.Join("..", "..")
    fixturePath := filepath.Join(toolsRoot, "docs", "fixtures", "current-base-system-runtime-context-api67-20260803.json")
    var fixture systemRuntimeContextFixture
    readJSON(t, fixturePath, &fixture)
    if fixture.Name != "current-base-system-runtime-context-api67-20260803" || fixture.Command.Kind != "exec" || len(fixture.Evidence) != 24 || len(fixture.Source) != 1 {
        t.Fatalf("fixture metadata = %#v", fixture)
    }
    source := fixture.Source[0].Content
    for _, token := range []string{
        "System.assert(true,", "System.assertEquals(1, 1,", "System.assertNotEquals(1, 2,",
        "System.debug(LoggingLevel.ERROR", "System.debug(LoggingLevel.NONE", "System.debug('single object')",
        "System.today()", "System.now()",
        "System.currentTimeMillis()", "System.isBatch()", "System.isFuture()", "System.isQueueable()", "System.isScheduled()",
    } {
        if !strings.Contains(source, token) { t.Fatalf("source missing %q", token) }
    }
    rows, err := BuildEvidenceSnapshot([]string{fixturePath})
    if err != nil { t.Fatal(err) }
    if len(rows) != 24 { t.Fatalf("evidence rows = %d, want 24", len(rows)) }
}

func TestSystemRuntimeContextAPI67DualOracleEvidence(t *testing.T) {
    toolsRoot := filepath.Join("..", "..")
    fixturePath := filepath.Join(toolsRoot, "docs", "fixtures", "current-base-system-runtime-context-api67-20260803.json")
    comparisonPath := filepath.Join(toolsRoot, "docs", "fixtures", "salesforce-current-base-system-runtime-context-api67-20260803-comparisons.json")
    var comparison struct {
        Candidate struct{ Commit, SHA256 string } `json:"candidate"`
        Local struct{ CandidatePath, CandidateSha256, SourcePath, SourceSha256, ReportPath, ReportSha256 string } `json:"local"`
        Salesforce struct{ TargetOrgAlias, TargetOrgId, APIVersion, ExecutePath, ExecuteSha256 string } `json:"salesforce"`
        LocalFixtures []struct{ Path, SHA256 string } `json:"localFixtures"`
        Comparisons []struct{ Status string; SurfaceIDs []string } `json:"comparisons"`
    }
    readJSON(t, comparisonPath, &comparison)
    if comparison.Candidate.Commit != "0a0f624e9c6fc82f8efc824852aef2808cd823fa" || comparison.Candidate.SHA256 != "773bd1ddc0d1a41c2972032837321714bba3255dbc21187a43fc52d306dee4e4" || comparison.Salesforce.TargetOrgAlias != "glade-sf-correctness" || comparison.Salesforce.APIVersion != "67.0" || len(comparison.LocalFixtures) != 1 || len(comparison.Comparisons) != 1 || comparison.Comparisons[0].Status != "pass" {
        t.Fatalf("comparison metadata = %#v", comparison)
    }
    fixtureEvidence, err := BuildEvidenceSnapshot([]string{fixturePath})
    if err != nil { t.Fatal(err) }
    oracleEvidence, err := BuildOracleEvidenceSnapshot([]string{comparisonPath})
    if err != nil { t.Fatal(err) }
    ids := make([]string, 0, len(fixtureEvidence))
    for _, row := range fixtureEvidence { ids = append(ids, row.SurfaceID) }
    assertExactIDs(t, ids, comparison.Comparisons[0].SurfaceIDs)
    oracleIDs := make([]string, 0, len(oracleEvidence))
    for _, row := range oracleEvidence { oracleIDs = append(oracleIDs, row.SurfaceID) }
    assertExactIDs(t, ids, oracleIDs)
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
    if _, err := os.Stat(filepath.Join(evidenceRoot, comparison.Local.SourcePath)); err != nil { t.Fatal(err) }
}
