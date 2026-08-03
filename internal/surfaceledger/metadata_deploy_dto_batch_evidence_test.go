package surfaceledger

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

const metadataDeployDTOBatchComparisonPath = "docs/fixtures/salesforce-metadata-deploy-dto-api67-comparisons.json"

type metadataDeployDTOBatchEnvelope struct {
	Candidate struct {
		Commit string `json:"commit"`
		SHA256 string `json:"sha256"`
	} `json:"candidate"`
	Profile struct {
		Path                       string `json:"path"`
		PredecessorPath            string `json:"predecessorPath"`
		SelectedRowCount           int    `json:"selectedRowCount"`
		PredecessorNonDeferredGaps int    `json:"predecessorNonDeferredGaps"`
		ExpectedSuccessorGaps      int    `json:"expectedSuccessorNonDeferredGaps"`
	} `json:"profile"`
	Local struct {
		CandidatePath string `json:"candidatePath"`
		CandidateSHA  string `json:"candidateSha256"`
		SourcePath    string `json:"sourcePath"`
		SourceSHA     string `json:"sourceSha256"`
		ReportPath    string `json:"reportPath"`
		ReportSHA     string `json:"reportSha256"`
	} `json:"local"`
	Salesforce struct {
		TargetOrgAlias string `json:"targetOrgAlias"`
		TargetOrgID    string `json:"targetOrgId"`
		APIVersion     string `json:"apiVersion"`
		ExecutePath    string `json:"executePath"`
		ExecuteSHA     string `json:"executeSha256"`
	} `json:"salesforce"`
	LocalFixtures []struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	} `json:"localFixtures"`
	Comparisons []struct {
		CaseID        string   `json:"caseId"`
		Status        string   `json:"status"`
		SFObservation string   `json:"sfObservation"`
		Glade         string   `json:"gladeObservation"`
		SurfaceIDs    []string `json:"surfaceIds"`
	} `json:"comparisons"`
}

func TestMetadataDeployDTOBatchRowsHaveExactFixtureAndOracleEvidence(t *testing.T) {
	toolsRoot := filepath.Join("..", "..")
	comparisonPath := filepath.Join(toolsRoot, metadataDeployDTOBatchComparisonPath)
	var comparison metadataDeployDTOBatchEnvelope
	readJSON(t, comparisonPath, &comparison)
	if comparison.Candidate.Commit != "0a0f624e9c6fc82f8efc824852aef2808cd823fa" || comparison.Candidate.SHA256 != comparison.Local.CandidateSHA || comparison.Profile.SelectedRowCount != 34 || comparison.Profile.PredecessorNonDeferredGaps != 4683 || comparison.Profile.ExpectedSuccessorGaps != 4649 || len(comparison.LocalFixtures) != 1 || len(comparison.Comparisons) != 1 {
		t.Fatalf("metadata deploy DTO provenance = %#v", comparison)
	}
	if comparison.Salesforce.TargetOrgAlias != "glade-sf-correctness" || comparison.Salesforce.TargetOrgID == "" || comparison.Salesforce.APIVersion != "67.0" {
		t.Fatalf("Salesforce provenance = %#v", comparison.Salesforce)
	}

	fixturePath := filepath.Join(toolsRoot, comparison.LocalFixtures[0].Path)
	assertMetadataDTOBatchSHA256(t, fixturePath, comparison.LocalFixtures[0].SHA256)
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
	assertExactIDs(t, wantIDs, comparison.Comparisons[0].SurfaceIDs)
	if len(wantIDs) != 34 || comparison.Comparisons[0].CaseID != "metadata-deploy-dto-api67" || comparison.Comparisons[0].Status != "pass" || comparison.Comparisons[0].SFObservation == "" || comparison.Comparisons[0].Glade == "" {
		t.Fatalf("metadata deploy DTO IDs/comparison = %d %#v", len(wantIDs), comparison.Comparisons)
	}
	oracleEvidence, err := BuildOracleEvidenceSnapshot([]string{comparisonPath})
	if err != nil {
		t.Fatal(err)
	}
	assertExactIDs(t, evidenceIDsInSet(oracleEvidence, wantIDs), wantIDs)

	evidenceRoot, err := filepath.Abs(filepath.Join(toolsRoot, "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	assertMetadataDTOBatchSHA256(t, filepath.Join(evidenceRoot, comparison.Local.CandidatePath), comparison.Local.CandidateSHA)
	assertMetadataDTOBatchSHA256(t, filepath.Join(evidenceRoot, comparison.Local.SourcePath), comparison.Local.SourceSHA)
	assertMetadataDTOBatchSHA256(t, filepath.Join(evidenceRoot, comparison.Local.ReportPath), comparison.Local.ReportSHA)
	assertMetadataDTOBatchSHA256(t, filepath.Join(evidenceRoot, comparison.Salesforce.ExecutePath), comparison.Salesforce.ExecuteSHA)
	var local struct {
		Status   string `json:"status"`
		ExitCode int    `json:"exitCode"`
	}
	readJSON(t, filepath.Join(evidenceRoot, comparison.Local.ReportPath), &local)
	if local.Status != "passed" || local.ExitCode != 0 {
		t.Fatalf("local metadata deploy DTO batch = %#v", local)
	}
	var sf struct {
		Status int `json:"status"`
		Result struct {
			Success          bool   `json:"success"`
			Compiled         bool   `json:"compiled"`
			CompileProblem   string `json:"compileProblem"`
			ExceptionMessage string `json:"exceptionMessage"`
		} `json:"result"`
	}
	readJSON(t, filepath.Join(evidenceRoot, comparison.Salesforce.ExecutePath), &sf)
	if sf.Status != 0 || !sf.Result.Success || !sf.Result.Compiled || sf.Result.CompileProblem != "" || sf.Result.ExceptionMessage != "" {
		t.Fatalf("Salesforce metadata deploy DTO batch = %#v", sf)
	}

	ledger := Merge(nil, nil, BuildGladeSnapshot(), append(fixtureEvidence, oracleEvidence...))
	byID := rowsBySurfaceKey(ledger.Rows)
	for _, id := range wantIDs {
		row, ok := byID[surfaceIDKey(id)]
		if !ok || row.Evidence != EvidenceFixtureAndOracle || row.GladeShape == ShapeAbsent || row.GladeBehavior != BehaviorSupported {
			t.Fatalf("%s ledger row = %#v", id, row)
		}
	}

	// Keep the report files strict JSON so future profile regeneration cannot
	// silently consume a warning banner or a failed CLI envelope.
	for _, path := range []string{comparison.Local.ReportPath, comparison.Salesforce.ExecutePath} {
		var raw json.RawMessage
		readJSON(t, filepath.Join(evidenceRoot, path), &raw)
		if len(raw) == 0 {
			t.Fatalf("empty report %s", path)
		}
	}
}
