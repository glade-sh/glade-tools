package surfaceledger

import (
	"os"
	"path/filepath"
	"testing"
)

const schemaG02CurrentComparisonPath = "docs/fixtures/salesforce-current-base-schema-g02-cb146-api67-20260803-comparisons.json"
const schemaG02HistoricalFixtureSHA = "c4857003e5f8061a4f6f20a14be2966cdb8cc333cd08ae9563cd509bfa7ed253"

type schemaG02CurrentEnvelope struct {
	Candidate struct {
		Path   string `json:"path"`
		Commit string `json:"commit"`
		SHA256 string `json:"sha256"`
	} `json:"candidate"`
	Salesforce struct {
		TargetOrgAlias string `json:"targetOrgAlias"`
		OrgID          string `json:"orgId"`
		APIVersion     string `json:"apiVersion"`
		ExecutePath    string `json:"executePath"`
		ExecuteSHA     string `json:"executeSha256"`
	} `json:"salesforce"`
	Local struct {
		CandidatePath string `json:"candidatePath"`
		CandidateSHA  string `json:"candidateSha256"`
		SourcePath    string `json:"sourcePath"`
		SourceSHA     string `json:"sourceSha256"`
		ReportPath    string `json:"reportPath"`
		ReportSHA     string `json:"reportSha256"`
		ReportPath2   string `json:"reportPathSecond"`
		ReportSHA2    string `json:"reportSha256Second"`
	} `json:"local"`
	FixturePath string `json:"fixturePath"`
	FixtureSHA  string `json:"fixtureSha256"`
	Comparisons []struct {
		CaseID        string   `json:"caseId"`
		Status        string   `json:"status"`
		SurfaceIDs    []string `json:"surfaceIds"`
		SFObservation string   `json:"sfObservation"`
		Glade         string   `json:"gladeObservation"`
	} `json:"comparisons"`
	Excluded []struct {
		SurfaceID string `json:"surfaceId"`
	} `json:"excluded"`
}

func TestSchemaG02CurrentRowsHaveExactFixtureAndOracleEvidence(t *testing.T) {
	toolsRoot := filepath.Join("..", "..")
	comparisonPath := filepath.Join(toolsRoot, schemaG02CurrentComparisonPath)
	var comparison schemaG02CurrentEnvelope
	readJSON(t, comparisonPath, &comparison)
	if comparison.Candidate.Commit != "0a0f624e9c6fc82f8efc824852aef2808cd823fa" || comparison.Candidate.SHA256 != "773bd1ddc0d1a41c2972032837321714bba3255dbc21187a43fc52d306dee4e4" || comparison.Candidate.SHA256 != comparison.Local.CandidateSHA || comparison.Salesforce.TargetOrgAlias != "glade-sf-correctness" || comparison.Salesforce.OrgID == "" || comparison.Salesforce.APIVersion != "67.0" {
		t.Fatalf("Schema G02 provenance = %#v", comparison)
	}
	if len(comparison.Comparisons) != 1 || comparison.Comparisons[0].CaseID != "schema-g02-cb146-api67-exact" || comparison.Comparisons[0].Status != "pass" || len(comparison.Comparisons[0].SurfaceIDs) != 54 || len(comparison.Excluded) != 4 {
		t.Fatalf("Schema G02 comparison accounting = %#v", comparison)
	}

	fixturePath := filepath.Join(toolsRoot, comparison.FixturePath)
	if comparison.FixtureSHA != schemaG02HistoricalFixtureSHA {
		t.Fatalf("Schema G02 historical fixture SHA = %q, want %q", comparison.FixtureSHA, schemaG02HistoricalFixtureSHA)
	}
	fixtureEvidence, err := BuildEvidenceSnapshot([]string{fixturePath})
	if err != nil {
		t.Fatal(err)
	}
	if len(fixtureEvidence) != 35 {
		t.Fatalf("Schema G02 retained fixture rows = %d, want 35", len(fixtureEvidence))
	}
	transferredPath := filepath.Join(toolsRoot, "docs/fixtures/data-platform-schema-describe-results-wave16-runtime.json")
	transferredEvidence, err := BuildEvidenceSnapshot([]string{transferredPath})
	if err != nil {
		t.Fatal(err)
	}
	wantedSet := make(map[string]struct{}, len(comparison.Comparisons[0].SurfaceIDs))
	for _, id := range comparison.Comparisons[0].SurfaceIDs {
		wantedSet[id] = struct{}{}
	}
	transferredRows := make([]SurfaceLedgerRow, 0, len(transferredEvidence))
	for _, row := range transferredEvidence {
		if row.Product == ProductApex && row.SurfaceID != "" {
			if _, ok := wantedSet[row.SurfaceID]; ok {
				transferredRows = append(transferredRows, row)
			}
		}
	}
	if len(transferredRows) != 19 {
		t.Fatalf("Schema G02 transferred fixture rows = %d, want 19", len(transferredRows))
	}
	fixtureEvidence = append(fixtureEvidence, transferredRows...)
	wantIDs := make([]string, 0, len(fixtureEvidence))
	for _, row := range fixtureEvidence {
		if row.Product == ProductApex && row.SurfaceID != "" {
			wantIDs = append(wantIDs, row.SurfaceID)
		}
	}
	assertExactIDs(t, wantIDs, comparison.Comparisons[0].SurfaceIDs)
	if len(wantIDs) != 54 {
		t.Fatalf("Schema G02 fixture IDs = %d, want 54", len(wantIDs))
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
	assertMetadataDTOBatchSHA256(t, filepath.Join(evidenceRoot, comparison.Local.ReportPath2), comparison.Local.ReportSHA2)
	assertMetadataDTOBatchSHA256(t, filepath.Join(evidenceRoot, comparison.Salesforce.ExecutePath), comparison.Salesforce.ExecuteSHA)

	var local struct {
		Status   string `json:"status"`
		ExitCode int    `json:"exitCode"`
		Summary  struct {
			DebugEvents int `json:"debugEvents"`
		} `json:"summary"`
	}
	readJSON(t, filepath.Join(evidenceRoot, comparison.Local.ReportPath), &local)
	if local.Status != "passed" || local.ExitCode != 0 || local.Summary.DebugEvents != 1 {
		t.Fatalf("local Schema G02 report = %#v", local)
	}
	var local2 struct {
		Status   string `json:"status"`
		ExitCode int    `json:"exitCode"`
	}
	readJSON(t, filepath.Join(evidenceRoot, comparison.Local.ReportPath2), &local2)
	if local2.Status != "passed" || local2.ExitCode != 0 {
		t.Fatalf("second local Schema G02 report = %#v", local2)
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
		t.Fatalf("Salesforce Schema G02 report = %#v", sf)
	}

	ledger := Merge(nil, nil, nil, append(fixtureEvidence, oracleEvidence...))
	byID := rowsBySurfaceKey(ledger.Rows)
	for _, id := range wantIDs {
		row, ok := byID[surfaceIDKey(id)]
		if !ok || row.Evidence != EvidenceFixtureAndOracle || row.GladeBehavior != BehaviorSupported {
			t.Fatalf("%s Schema G02 ledger row = %#v", id, row)
		}
	}
	if _, err := os.Stat(filepath.Join(evidenceRoot, comparison.Local.SourcePath)); err != nil {
		t.Fatal(err)
	}
}
