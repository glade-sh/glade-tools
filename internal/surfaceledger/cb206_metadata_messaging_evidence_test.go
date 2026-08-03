package surfaceledger

import (
	"path/filepath"
	"testing"
)

type cb206MetadataMessagingComparison struct {
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
		ExecutePath string `json:"executePath"`
		ExecuteSHA  string `json:"executeSha256"`
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

func TestCB206MetadataMessagingRowsHaveExactFixtureAndOracleEvidence(t *testing.T) {
	toolsRoot := filepath.Join("..", "..")
	comparisonPath := filepath.Join(toolsRoot, "docs", "fixtures", "salesforce-current-base-cb206-metadata-messaging-deterministic-api67-comparisons.json")
	var comparison cb206MetadataMessagingComparison
	readJSON(t, comparisonPath, &comparison)
	if comparison.Candidate.Commit != "0a0f624e9c6fc82f8efc824852aef2808cd823fa" || comparison.Candidate.SHA256 != comparison.Local.CandidateSHA || comparison.Profile.SelectedRowCount != 4 || comparison.Profile.PredecessorNonDeferredGaps != 4465 || comparison.Profile.ExpectedSuccessorGaps != 4461 || len(comparison.LocalFixtures) != 1 || len(comparison.Comparisons) != 1 {
		t.Fatalf("CB206 provenance = %#v", comparison)
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
	if len(wantIDs) != 4 || comparison.Comparisons[0].CaseID != "metadata-messaging-deterministic-api67" || comparison.Comparisons[0].Status != "pass" {
		t.Fatalf("CB206 IDs/comparison = %d %#v", len(wantIDs), comparison.Comparisons)
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
		t.Fatalf("CB206 local report = %#v", local)
	}
	var sf struct {
		Status int `json:"status"`
		Result struct {
			Success  bool `json:"success"`
			Compiled bool `json:"compiled"`
		} `json:"result"`
	}
	readJSON(t, filepath.Join(evidenceRoot, comparison.Salesforce.ExecutePath), &sf)
	if sf.Status != 0 || !sf.Result.Success || !sf.Result.Compiled {
		t.Fatalf("CB206 Salesforce report = %#v", sf)
	}

	var profile struct {
		NonDeferredGaps []struct {
			SurfaceID string `json:"surfaceId"`
		} `json:"nonDeferredGaps"`
	}
	readJSON(t, filepath.Join(evidenceRoot, comparison.Profile.PredecessorPath), &profile)
	if len(profile.NonDeferredGaps) != comparison.Profile.PredecessorNonDeferredGaps {
		t.Fatalf("CB206 predecessor profile gaps = %d", len(profile.NonDeferredGaps))
	}
}
