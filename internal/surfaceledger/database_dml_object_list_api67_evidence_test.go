package surfaceledger

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

const databaseDMLObjectListAPI67ComparisonPath = "docs/fixtures/salesforce-current-base-database-dml-object-list-api67-20260803-comparisons.json"

type databaseDMLObjectListAPI67Envelope struct {
	Candidate struct {
		Commit string `json:"commit"`
		SHA256 string `json:"sha256"`
	} `json:"candidate"`
	LocalFixtures []struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	} `json:"localFixtures"`
	Comparisons []struct {
		CaseID                 string   `json:"caseId"`
		Status                 string   `json:"status"`
		SurfaceIDs             []string `json:"surfaceIds"`
		SourcePath             string   `json:"sourcePath"`
		SourceSHA256           string   `json:"sourceSha256"`
		GladeReportPath        string   `json:"gladeReportPath"`
		GladeReportSHA256      string   `json:"gladeReportSha256"`
		SalesforceReportPath   string   `json:"salesforceReportPath"`
		SalesforceReportSHA256 string   `json:"salesforceReportSha256"`
		SFObservation          string   `json:"sfObservation"`
		GladeObservation       string   `json:"gladeObservation"`
	} `json:"comparisons"`
	Salesforce struct {
		TargetOrgAlias string `json:"targetOrgAlias"`
		OrgID          string `json:"orgId"`
		APIVersion     string `json:"apiVersion"`
	} `json:"salesforce"`
}

func TestDatabaseDMLObjectListAPI67RowsHaveExactFixtureAndOracleEvidence(t *testing.T) {
	toolsRoot := filepath.Join("..", "..")
	comparisonPath := filepath.Join(toolsRoot, databaseDMLObjectListAPI67ComparisonPath)
	var comparison databaseDMLObjectListAPI67Envelope
	readJSON(t, comparisonPath, &comparison)
	if comparison.Candidate.Commit != "0a0f624e9c6fc82f8efc824852aef2808cd823fa" || comparison.Candidate.SHA256 != "773bd1ddc0d1a41c2972032837321714bba3255dbc21187a43fc52d306dee4e4" || comparison.Salesforce.TargetOrgAlias != "glade-sf-correctness" || comparison.Salesforce.OrgID == "" || comparison.Salesforce.APIVersion != "67.0" || len(comparison.LocalFixtures) != 1 || len(comparison.Comparisons) != 1 {
		t.Fatalf("database DML provenance = %#v", comparison)
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
	if len(wantIDs) != 20 {
		t.Fatalf("database DML fixture rows = %d, want 20", len(wantIDs))
	}
	caseRow := comparison.Comparisons[0]
	if caseRow.CaseID != "database-dml-object-list-api67" || caseRow.Status != "pass" || caseRow.SFObservation == "" || caseRow.GladeObservation == "" {
		t.Fatalf("database DML comparison = %#v", caseRow)
	}
	assertExactIDs(t, wantIDs, caseRow.SurfaceIDs)
	oracleEvidence, err := BuildOracleEvidenceSnapshot([]string{comparisonPath})
	if err != nil {
		t.Fatal(err)
	}
	assertExactIDs(t, evidenceIDsInSet(oracleEvidence, wantIDs), wantIDs)

	evidenceRoot, err := filepath.Abs(filepath.Join(toolsRoot, "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	assertMetadataDTOBatchSHA256(t, filepath.Join(evidenceRoot, caseRow.SourcePath), caseRow.SourceSHA256)
	source, err := os.ReadFile(filepath.Join(evidenceRoot, caseRow.SourcePath))
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range [][]byte{[]byte("Database.insert("), []byte("Database.update("), []byte("Database.delete("), []byte("Database.undelete(")} {
		if !bytes.Contains(source, token) {
			t.Fatalf("source %s does not reach %q", caseRow.CaseID, token)
		}
	}
	assertMetadataDTOBatchSHA256(t, filepath.Join(evidenceRoot, caseRow.GladeReportPath), caseRow.GladeReportSHA256)
	assertMetadataDTOBatchSHA256(t, filepath.Join(evidenceRoot, caseRow.SalesforceReportPath), caseRow.SalesforceReportSHA256)
	var local struct {
		Status   string `json:"status"`
		ExitCode int    `json:"exitCode"`
	}
	readJSON(t, filepath.Join(evidenceRoot, caseRow.GladeReportPath), &local)
	if local.Status != "passed" || local.ExitCode != 0 {
		t.Fatalf("local DML report = %#v", local)
	}
	var sf struct {
		Status int `json:"status"`
		Result struct {
			Summary struct {
				Outcome  string `json:"outcome"`
				TestsRan int    `json:"testsRan"`
			} `json:"summary"`
			Tests []struct {
				Outcome string `json:"Outcome"`
			} `json:"tests"`
		} `json:"result"`
	}
	readJSON(t, filepath.Join(evidenceRoot, caseRow.SalesforceReportPath), &sf)
	if sf.Status != 0 || sf.Result.Summary.Outcome != "Passed" || sf.Result.Summary.TestsRan != 1 || len(sf.Result.Tests) != 1 || sf.Result.Tests[0].Outcome != "Pass" {
		t.Fatalf("Salesforce DML report = %#v", sf)
	}

}
