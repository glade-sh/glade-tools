package surfaceledger

import (
	"path/filepath"
	"testing"
)

func TestSystemCompileShapeNextPacketHasExactDualOracleEvidence(t *testing.T) {
	toolsRoot := filepath.Join("..", "..")
	fixturePath := filepath.Join(toolsRoot, "docs", "fixtures", "current-base-system-compile-shape-next-api67.json")
	comparisonPath := filepath.Join(toolsRoot, "docs", "fixtures", "salesforce-current-base-system-compile-shape-next-api67-comparisons.json")

	var fixture struct {
		Name    string `json:"name"`
		API     string `json:"apiVersion"`
		Mode    string `json:"mode"`
		Command struct {
			Args []string `json:"args"`
		} `json:"command"`
		Evidence []struct {
			SurfaceID string `json:"surfaceId"`
		} `json:"evidence"`
	}
	readJSON(t, fixturePath, &fixture)
	if fixture.Name != "current-base-system-compile-shape-next-api67" || fixture.API != "67.0" || fixture.Mode != "compile-shape" || len(fixture.Command.Args) != 1 || len(fixture.Evidence) != 33 {
		t.Fatalf("fixture metadata = name:%q api:%q mode:%q args:%d evidence:%d", fixture.Name, fixture.API, fixture.Mode, len(fixture.Command.Args), len(fixture.Evidence))
	}

	var comparison struct {
		Candidate struct {
			Commit string `json:"commit"`
			SHA256 string `json:"sha256"`
		} `json:"candidate"`
		Profile   struct {
			PacketID                         string `json:"packetId"`
			TotalRows                        int    `json:"totalRows"`
			CompiledRows                     int    `json:"compiledRows"`
			OpenRows                         int    `json:"openRows"`
			PredecessorNonDeferredGaps       int    `json:"predecessorNonDeferredGaps"`
			ExpectedSuccessorNonDeferredGaps int    `json:"expectedSuccessorNonDeferredGaps"`
		} `json:"profile"`
		Local struct {
			CandidatePath   string `json:"candidatePath"`
			CandidateSHA256 string `json:"candidateSha256"`
			SourcePath      string `json:"sourcePath"`
			SourceSHA256    string `json:"sourceSha256"`
			ReportPath      string `json:"reportPath"`
			ReportSHA256    string `json:"reportSha256"`
		} `json:"local"`
		Salesforce struct {
			TargetOrgAlias string `json:"targetOrgAlias"`
			APIVersion     string `json:"apiVersion"`
			ExecutePath    string `json:"executePath"`
			ExecuteSHA256  string `json:"executeSha256"`
		} `json:"salesforce"`
		LocalFixtures []struct {
			Path   string `json:"path"`
			SHA256 string `json:"sha256"`
		} `json:"localFixtures"`
		Comparisons []struct {
			CaseID     string   `json:"caseId"`
			Status     string   `json:"status"`
			SurfaceIDs []string `json:"surfaceIds"`
		} `json:"comparisons"`
	}
	readJSON(t, comparisonPath, &comparison)

	if comparison.Candidate.Commit != "9c3edbe43fabeb3b9069c2d47911e8362be688fe" {
		t.Fatalf("candidate commit = %s", comparison.Candidate.Commit)
	}
	if comparison.Candidate.SHA256 != "e3788be1542125cf9947dd482c7d5694562ac7627faad8d0ac12f64c33a4e24a" {
		t.Fatalf("candidate sha256 = %s", comparison.Candidate.SHA256)
	}
	if comparison.Salesforce.TargetOrgAlias != "glade-sf-correctness" {
		t.Fatalf("org alias = %s", comparison.Salesforce.TargetOrgAlias)
	}
	if comparison.Salesforce.APIVersion != "67.0" {
		t.Fatalf("api version = %s", comparison.Salesforce.APIVersion)
	}
	if len(comparison.LocalFixtures) != 1 {
		t.Fatalf("localFixtures = %d", len(comparison.LocalFixtures))
	}
	if len(comparison.Comparisons) != 1 || comparison.Comparisons[0].Status != "pass" {
		t.Fatalf("comparisons = %#v", comparison.Comparisons)
	}
	if comparison.Profile.TotalRows != 229 || comparison.Profile.CompiledRows != 33 || comparison.Profile.OpenRows != 196 {
		t.Fatalf("arithmetic = total:%d compiled:%d open:%d", comparison.Profile.TotalRows, comparison.Profile.CompiledRows, comparison.Profile.OpenRows)
	}
	if comparison.Profile.CompiledRows+comparison.Profile.OpenRows != comparison.Profile.TotalRows {
		t.Fatalf("arithmetic mismatch: %d + %d != %d", comparison.Profile.CompiledRows, comparison.Profile.OpenRows, comparison.Profile.TotalRows)
	}
	if comparison.Profile.PredecessorNonDeferredGaps != 4309 || comparison.Profile.ExpectedSuccessorNonDeferredGaps != 4276 {
		t.Fatalf("gap arithmetic = pred:%d succ:%d", comparison.Profile.PredecessorNonDeferredGaps, comparison.Profile.ExpectedSuccessorNonDeferredGaps)
	}
	if comparison.Profile.ExpectedSuccessorNonDeferredGaps != comparison.Profile.PredecessorNonDeferredGaps-comparison.Profile.CompiledRows {
		t.Fatalf("gap closure: %d - %d != %d", comparison.Profile.PredecessorNonDeferredGaps, comparison.Profile.CompiledRows, comparison.Profile.ExpectedSuccessorNonDeferredGaps)
	}

	fixtureEvidence, err := BuildEvidenceSnapshot([]string{fixturePath})
	if err != nil {
		t.Fatal(err)
	}

	wantIDs := make([]string, 0, len(fixtureEvidence))
	for _, row := range fixtureEvidence {
		if row.Product == ProductApex && row.SurfaceID != "" {
			wantIDs = append(wantIDs, row.SurfaceID)
		}
	}
	if len(wantIDs) != 33 {
		t.Fatalf("fixture evidence IDs = %d, want 33", len(wantIDs))
	}
	assertExactIDs(t, wantIDs, comparison.Comparisons[0].SurfaceIDs)

	oracleEvidence, err := BuildOracleEvidenceSnapshot([]string{comparisonPath})
	if err != nil {
		t.Fatal(err)
	}
	oracleIDs := make([]string, 0, len(oracleEvidence))
	for _, row := range oracleEvidence {
		oracleIDs = append(oracleIDs, row.SurfaceID)
	}
	assertExactIDs(t, wantIDs, oracleIDs)

	evidenceRoot, err := filepath.Abs(filepath.Join(toolsRoot, "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for path, hash := range map[string]string{
		comparison.Local.CandidatePath:    comparison.Local.CandidateSHA256,
		comparison.Local.SourcePath:       comparison.Local.SourceSHA256,
		comparison.Local.ReportPath:       comparison.Local.ReportSHA256,
		comparison.Salesforce.ExecutePath: comparison.Salesforce.ExecuteSHA256,
	} {
		assertMetadataDTOBatchSHA256(t, filepath.Join(evidenceRoot, path), hash)
	}

	var local struct {
		Status   string `json:"status"`
		ExitCode int    `json:"exitCode"`
	}
	readJSON(t, filepath.Join(evidenceRoot, comparison.Local.ReportPath), &local)
	if local.Status != "passed" || local.ExitCode != 0 {
		t.Fatalf("local report = %#v", local)
	}

	assertMetadataDTOBatchSHA256(t, filepath.Join(toolsRoot, comparison.LocalFixtures[0].Path), comparison.LocalFixtures[0].SHA256)
}
