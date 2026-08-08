package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const systemExceptionBatchComparisonPath = "docs/fixtures/salesforce-system-exception-api67-safe-family-comparisons.json"

type systemExceptionBatchEnvelope struct {
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
		ProjectPath   string `json:"projectPath"`
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

func TestSystemExceptionBatchRowsHaveExactFixtureAndOracleEvidence(t *testing.T) {
	toolsRoot := filepath.Join("..", "..")
	comparisonPath := filepath.Join(toolsRoot, systemExceptionBatchComparisonPath)
	var comparison systemExceptionBatchEnvelope
	readJSON(t, comparisonPath, &comparison)
	if comparison.Candidate.Commit != "6419bf1e8ede470d9fd5c6c789aede9ef5d2713d" || comparison.Candidate.SHA256 != comparison.Local.CandidateSHA || comparison.Profile.SelectedRowCount != 111 || comparison.Profile.PredecessorNonDeferredGaps != 5307 || comparison.Profile.ExpectedSuccessorGaps != 5196 || len(comparison.LocalFixtures) != 1 || len(comparison.Comparisons) != 1 {
		t.Fatalf("exception batch provenance = %#v", comparison)
	}
	if comparison.Profile.Path != "evidence/current-base/canonical-bundle-system-exceptions-9b9b95b/apex-support-profile.json" || comparison.Profile.PredecessorPath != "evidence/current-base/canonical-bundle-metadata-enum-8e151ad/apex-support-profile.json" {
		t.Fatalf("exception batch profile paths = %#v", comparison.Profile)
	}
	if comparison.Salesforce.TargetOrgAlias != "glade-sf-correctness" || comparison.Salesforce.TargetOrgID == "" || comparison.Salesforce.APIVersion != "67.0" {
		t.Fatalf("Salesforce provenance = %#v", comparison.Salesforce)
	}

	fixturePath := filepath.Join(toolsRoot, comparison.LocalFixtures[0].Path)
	assertMetadataDTOBatchSHA256(t, fixturePath, comparison.LocalFixtures[0].SHA256)
	fixtureData, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	var fixtureSource struct {
		Command struct {
			Args []string `json:"args"`
		} `json:"command"`
	}
	if err := json.Unmarshal(fixtureData, &fixtureSource); err != nil {
		t.Fatal(err)
	}
	source := strings.Join(fixtureSource.Command.Args, "\n")
	if strings.Count(source, ".getCause(") != 19 {
		t.Fatalf("fixture source getCause coverage = %d, want 19", strings.Count(source, ".getCause("))
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
	assertExactIDs(t, wantIDs, comparison.Comparisons[0].SurfaceIDs)
	if len(wantIDs) != 111 || comparison.Comparisons[0].CaseID != "system-exception-api67-safe-family" || comparison.Comparisons[0].Status != "pass" || comparison.Comparisons[0].SFObservation == "" || comparison.Comparisons[0].Glade == "" {
		t.Fatalf("exception batch IDs/comparison = %d %#v", len(wantIDs), comparison.Comparisons)
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
		t.Fatalf("local exception batch = %#v", local)
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
		t.Fatalf("Salesforce exception batch = %#v", sf)
	}

	ledger := Merge(nil, nil, BuildGladeSnapshot(), append(fixtureEvidence, oracleEvidence...))
	byID := rowsBySurfaceKey(ledger.Rows)
	for _, id := range wantIDs {
		row, ok := byID[surfaceIDKey(id)]
		if !ok || row.Evidence != EvidenceFixtureAndOracle || row.GladeShape == ShapeAbsent || row.GladeBehavior != BehaviorSupported {
			t.Fatalf("%s ledger row = %#v", id, row)
		}
	}
}
