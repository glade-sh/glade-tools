package surfaceledger

import (
	"path/filepath"
	"testing"
)

const metadataEnumBatchComparisonPath = "docs/fixtures/salesforce-metadata-enum-batch-20260803-comparisons.json"

type metadataEnumBatchEnvelope struct {
	Candidate struct {
		Commit string `json:"commit"`
		SHA256 string `json:"sha256"`
	} `json:"candidate"`
	Profile struct {
		Path                             string `json:"path"`
		SelectedRowCount                 int    `json:"selectedRowCount"`
		PredecessorNonDeferredGaps       int    `json:"predecessorNonDeferredGaps"`
		ExpectedSuccessorNonDeferredGaps int    `json:"expectedSuccessorNonDeferredGaps"`
	} `json:"profile"`
	Local struct {
		CandidatePath string `json:"candidatePath"`
		CandidateSHA  string `json:"candidateSha256"`
		ProjectPath   string `json:"projectPath"`
		SourcePath    string `json:"sourcePath"`
		SourceSHA     string `json:"sourceSha256"`
		ReportPath    string `json:"reportPath"`
		ReportSHA     string `json:"reportSha256"`
	} `json:"local"`
	Salesforce struct {
		TargetOrgAlias string `json:"targetOrgAlias"`
		OrgID          string `json:"orgId"`
		APIVersion     string `json:"apiVersion"`
		DeployPath     string `json:"deployPath"`
		TestPath       string `json:"testPath"`
		DeletePath     string `json:"deletePath"`
		PostDeletePath string `json:"postDeletePath"`
		DeploySHA      string `json:"deploySha256"`
		TestSHA        string `json:"testSha256"`
		DeleteSHA      string `json:"deleteSha256"`
		PostDeleteSHA  string `json:"postDeleteSha256"`
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

func TestMetadataEnumBatchRowsHaveExactFixtureAndOracleEvidence(t *testing.T) {
	toolsRoot := filepath.Join("..", "..")
	comparisonPath := filepath.Join(toolsRoot, metadataEnumBatchComparisonPath)
	var comparison metadataEnumBatchEnvelope
	readJSON(t, comparisonPath, &comparison)
	if comparison.Candidate.Commit != "6419bf1e8ede470d9fd5c6c789aede9ef5d2713d" || comparison.Candidate.SHA256 != comparison.Local.CandidateSHA || comparison.Profile.SelectedRowCount != 101 || comparison.Profile.PredecessorNonDeferredGaps != 5408 || comparison.Profile.ExpectedSuccessorNonDeferredGaps != 5307 || len(comparison.LocalFixtures) != 1 || len(comparison.Comparisons) != 1 {
		t.Fatalf("batch provenance = %#v", comparison)
	}
	if comparison.Salesforce.TargetOrgAlias != "glade-sf-correctness" || comparison.Salesforce.OrgID == "" || comparison.Salesforce.APIVersion != "67.0" {
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
	if len(wantIDs) != 101 || comparison.Comparisons[0].CaseID != "metadata-enum-constants-api67" || comparison.Comparisons[0].Status != "pass" {
		t.Fatalf("enum batch IDs/comparison = %d %#v", len(wantIDs), comparison.Comparisons)
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
	profilePath := filepath.Join(evidenceRoot, comparison.Profile.Path)
	var profile struct {
		NonDeferredGaps []struct {
			SurfaceID string `json:"surfaceId"`
		} `json:"nonDeferredGaps"`
	}
	readJSON(t, profilePath, &profile)
	if len(profile.NonDeferredGaps) != comparison.Profile.ExpectedSuccessorNonDeferredGaps {
		t.Fatalf("successor profile non-deferred gaps = %d, want %d", len(profile.NonDeferredGaps), comparison.Profile.ExpectedSuccessorNonDeferredGaps)
	}
	metadataEnumBatchAssertLocal(t, filepath.Join(evidenceRoot, comparison.Local.ReportPath))
	metadataEnumBatchAssertSalesforce(t, evidenceRoot, comparison)

	ledger := Merge(nil, nil, BuildGladeSnapshot(), append(fixtureEvidence, oracleEvidence...))
	byID := rowsBySurfaceKey(ledger.Rows)
	for _, id := range wantIDs {
		row, ok := byID[surfaceIDKey(id)]
		if !ok || row.Evidence != EvidenceFixtureAndOracle || row.GladeShape == ShapeAbsent || row.GladeBehavior != BehaviorSupported {
			t.Fatalf("%s ledger row = %#v", id, row)
		}
	}
}

func metadataEnumBatchAssertLocal(t *testing.T, path string) {
	t.Helper()
	var report struct {
		Status  string `json:"status"`
		Summary struct {
			Total         int `json:"total"`
			Passed        int `json:"passed"`
			CompileErrors int `json:"compileErrors"`
			RuntimeErrors int `json:"runtimeErrors"`
			Unsupported   int `json:"unsupported"`
		} `json:"summary"`
	}
	readJSON(t, path, &report)
	if report.Status != "passed" || report.Summary.Total != 1 || report.Summary.Passed != 1 || report.Summary.CompileErrors != 0 || report.Summary.RuntimeErrors != 0 || report.Summary.Unsupported != 0 {
		t.Fatalf("local enum test = %#v", report)
	}
}

func metadataEnumBatchAssertSalesforce(t *testing.T, root string, comparison metadataEnumBatchEnvelope) {
	t.Helper()
	assertMetadataDTOBatchSHA256(t, filepath.Join(root, comparison.Salesforce.DeployPath), comparison.Salesforce.DeploySHA)
	assertMetadataDTOBatchSHA256(t, filepath.Join(root, comparison.Salesforce.TestPath), comparison.Salesforce.TestSHA)
	assertMetadataDTOBatchSHA256(t, filepath.Join(root, comparison.Salesforce.DeletePath), comparison.Salesforce.DeleteSHA)
	assertMetadataDTOBatchSHA256(t, filepath.Join(root, comparison.Salesforce.PostDeletePath), comparison.Salesforce.PostDeleteSHA)
	var deploy struct {
		Status int `json:"status"`
		Result struct {
			Success                  bool `json:"success"`
			NumberComponentErrors    int  `json:"numberComponentErrors"`
			NumberComponentsDeployed int  `json:"numberComponentsDeployed"`
		} `json:"result"`
	}
	readJSON(t, filepath.Join(root, comparison.Salesforce.DeployPath), &deploy)
	if deploy.Status != 0 || !deploy.Result.Success || deploy.Result.NumberComponentErrors != 0 || deploy.Result.NumberComponentsDeployed != 1 {
		t.Fatalf("Salesforce deploy = %#v", deploy)
	}
	var test struct {
		Result struct {
			Summary struct {
				OrgID    string `json:"orgId"`
				Outcome  string `json:"outcome"`
				Failing  int    `json:"failing"`
				TestsRan int    `json:"testsRan"`
			} `json:"summary"`
		} `json:"result"`
	}
	readJSON(t, filepath.Join(root, comparison.Salesforce.TestPath), &test)
	if test.Result.Summary.OrgID != comparison.Salesforce.OrgID || test.Result.Summary.Outcome != "Passed" || test.Result.Summary.Failing != 0 || test.Result.Summary.TestsRan != 1 {
		t.Fatalf("Salesforce test = %#v", test)
	}
	var deletion struct {
		Status int `json:"status"`
		Result struct {
			Success bool `json:"success"`
		} `json:"result"`
	}
	readJSON(t, filepath.Join(root, comparison.Salesforce.DeletePath), &deletion)
	if deletion.Status != 0 || !deletion.Result.Success {
		t.Fatalf("Salesforce cleanup = %#v", deletion)
	}
	var postDelete struct {
		Result struct {
			TotalSize int `json:"totalSize"`
		} `json:"result"`
	}
	readJSON(t, filepath.Join(root, comparison.Salesforce.PostDeletePath), &postDelete)
	if postDelete.Result.TotalSize != 0 {
		t.Fatalf("Salesforce post-delete query = %#v", postDelete)
	}
}
