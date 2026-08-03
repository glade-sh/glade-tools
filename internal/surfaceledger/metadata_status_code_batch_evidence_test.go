package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const metadataStatusCodeBatchComparisonPath = "docs/fixtures/salesforce-metadata-status-code-batch-20260803-comparisons.json"

type metadataStatusCodeBatchEnvelope struct {
	Candidate struct {
		Commit string `json:"commit"`
		SHA256 string `json:"sha256"`
	} `json:"candidate"`
	Profile struct {
		Path                string `json:"path"`
		PredecessorPath     string `json:"predecessorPath"`
		SelectedRowCount    int    `json:"selectedRowCount"`
		PredecessorGapCount int    `json:"predecessorNonDeferredGaps"`
		SuccessorGapCount   int    `json:"expectedSuccessorNonDeferredGaps"`
	} `json:"profile"`
	LocalFixtures []struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	} `json:"localFixtures"`
	Comparisons []struct {
		CaseID                 string   `json:"caseId"`
		Status                 string   `json:"status"`
		SFObservation          string   `json:"sfObservation"`
		GladeObservation       string   `json:"gladeObservation"`
		SurfaceIDs             []string `json:"surfaceIds"`
		SourcePath             string   `json:"sourcePath"`
		SourceSHA256           string   `json:"sourceSha256"`
		GladeReportPath        string   `json:"gladeReportPath"`
		GladeReportSHA256      string   `json:"gladeReportSha256"`
		SalesforceReportPath   string   `json:"salesforceReportPath"`
		SalesforceReportSHA256 string   `json:"salesforceReportSha256"`
	} `json:"comparisons"`
	Salesforce struct {
		TargetOrgAlias string `json:"targetOrgAlias"`
		OrgID          string `json:"orgId"`
		APIVersion     string `json:"apiVersion"`
	} `json:"salesforce"`
}

func TestMetadataStatusCodeBatchRowsHaveExactFixtureAndOracleEvidence(t *testing.T) {
	toolsRoot := filepath.Join("..", "..")
	comparisonPath := filepath.Join(toolsRoot, metadataStatusCodeBatchComparisonPath)
	var comparison metadataStatusCodeBatchEnvelope
	readJSON(t, comparisonPath, &comparison)
	if comparison.Candidate.Commit != "6419bf1e8ede470d9fd5c6c789aede9ef5d2713d" || comparison.Candidate.SHA256 != "35c3cd0c023384574381d390ab899d363e6bef1b0d3b88cd9e9653c8fb2887bb" || comparison.Profile.SelectedRowCount != 513 || comparison.Profile.PredecessorGapCount != 5196 || comparison.Profile.SuccessorGapCount != 4683 || len(comparison.LocalFixtures) != 6 || len(comparison.Comparisons) != 6 {
		t.Fatalf("StatusCode batch provenance = %#v", comparison)
	}
	if comparison.Profile.Path != "evidence/current-base/canonical-bundle-status-code-e5e3abf/apex-support-profile.json" || comparison.Profile.PredecessorPath != "evidence/current-base/canonical-bundle-system-exceptions-9b9b95b/apex-support-profile.json" {
		t.Fatalf("StatusCode profile paths = %#v", comparison.Profile)
	}
	if comparison.Salesforce.TargetOrgAlias != "glade-sf-correctness" || comparison.Salesforce.OrgID == "" || comparison.Salesforce.APIVersion != "67.0" {
		t.Fatalf("Salesforce provenance = %#v", comparison.Salesforce)
	}

	var allFixtureEvidence []SurfaceLedgerRow
	var wantIDs []string
	evidenceRoot, err := filepath.Abs(filepath.Join(toolsRoot, "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for i, fixture := range comparison.LocalFixtures {
		fixturePath := filepath.Join(toolsRoot, fixture.Path)
		assertMetadataDTOBatchSHA256(t, fixturePath, fixture.SHA256)
		var raw struct {
			Command struct {
				Args []string `json:"args"`
			} `json:"command"`
		}
		data, err := os.ReadFile(fixturePath)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatal(err)
		}
		source := strings.Join(raw.Command.Args, "\n")
		if strings.Count(source, "Metadata.StatusCode.") != len(comparison.Comparisons[i].SurfaceIDs) {
			t.Fatalf("fixture %d source constant coverage = %d, want %d", i, strings.Count(source, "Metadata.StatusCode."), len(comparison.Comparisons[i].SurfaceIDs))
		}
		fixtureEvidence, err := BuildEvidenceSnapshot([]string{fixturePath})
		if err != nil {
			t.Fatal(err)
		}
		ids := make([]string, 0, len(fixtureEvidence))
		for _, row := range fixtureEvidence {
			if row.Product == ProductApex && row.SurfaceID != "" {
				ids = append(ids, row.SurfaceID)
			}
		}
		caseRow := comparison.Comparisons[i]
		assertExactIDs(t, ids, caseRow.SurfaceIDs)
		if caseRow.Status != "pass" || caseRow.CaseID == "" || caseRow.SFObservation == "" || caseRow.GladeObservation == "" {
			t.Fatalf("comparison %d = %#v", i, caseRow)
		}
		allFixtureEvidence = append(allFixtureEvidence, fixtureEvidence...)
		wantIDs = append(wantIDs, ids...)
		assertMetadataDTOBatchSHA256(t, filepath.Join(evidenceRoot, caseRow.SourcePath), caseRow.SourceSHA256)
		assertMetadataDTOBatchSHA256(t, filepath.Join(evidenceRoot, caseRow.GladeReportPath), caseRow.GladeReportSHA256)
		assertMetadataDTOBatchSHA256(t, filepath.Join(evidenceRoot, caseRow.SalesforceReportPath), caseRow.SalesforceReportSHA256)
		metadataStatusCodeBatchAssertReports(t, evidenceRoot, caseRow)
	}
	if len(wantIDs) != 513 {
		t.Fatalf("StatusCode IDs = %d, want 513", len(wantIDs))
	}
	oracleEvidence, err := BuildOracleEvidenceSnapshot([]string{comparisonPath})
	if err != nil {
		t.Fatal(err)
	}
	assertExactIDs(t, evidenceIDsInSet(oracleEvidence, wantIDs), wantIDs)

	var successor struct {
		NonDeferredGaps []struct {
			SurfaceID string `json:"surfaceId"`
		} `json:"nonDeferredGaps"`
	}
	readJSON(t, filepath.Join(evidenceRoot, comparison.Profile.Path), &successor)
	var predecessor struct {
		NonDeferredGaps []struct {
			SurfaceID string `json:"surfaceId"`
		} `json:"nonDeferredGaps"`
	}
	readJSON(t, filepath.Join(evidenceRoot, comparison.Profile.PredecessorPath), &predecessor)
	if len(successor.NonDeferredGaps) != 4683 || len(predecessor.NonDeferredGaps) != 5196 {
		t.Fatalf("profile gaps = successor %d predecessor %d", len(successor.NonDeferredGaps), len(predecessor.NonDeferredGaps))
	}
	oldIDs := make(map[string]bool, len(predecessor.NonDeferredGaps))
	for _, row := range predecessor.NonDeferredGaps {
		oldIDs[row.SurfaceID] = true
	}
	newIDs := make(map[string]bool, len(successor.NonDeferredGaps))
	for _, row := range successor.NonDeferredGaps {
		newIDs[row.SurfaceID] = true
	}
	var removed, added []string
	for id := range oldIDs {
		if !newIDs[id] {
			removed = append(removed, id)
		}
	}
	for id := range newIDs {
		if !oldIDs[id] {
			added = append(added, id)
		}
	}
	assertExactIDs(t, removed, wantIDs)
	if len(added) != 0 {
		t.Fatalf("successor profile added IDs: %v", added)
	}

	ledger := Merge(nil, nil, BuildGladeSnapshot(), append(allFixtureEvidence, oracleEvidence...))
	byID := rowsBySurfaceKey(ledger.Rows)
	for _, id := range wantIDs {
		row, ok := byID[surfaceIDKey(id)]
		if !ok || row.Evidence != EvidenceFixtureAndOracle || row.GladeShape == ShapeAbsent || row.GladeBehavior != BehaviorSupported {
			t.Fatalf("%s ledger row = %#v", id, row)
		}
	}
}

func metadataStatusCodeBatchAssertReports(t *testing.T, root string, comparison struct {
	CaseID                 string   `json:"caseId"`
	Status                 string   `json:"status"`
	SFObservation          string   `json:"sfObservation"`
	GladeObservation       string   `json:"gladeObservation"`
	SurfaceIDs             []string `json:"surfaceIds"`
	SourcePath             string   `json:"sourcePath"`
	SourceSHA256           string   `json:"sourceSha256"`
	GladeReportPath        string   `json:"gladeReportPath"`
	GladeReportSHA256      string   `json:"gladeReportSha256"`
	SalesforceReportPath   string   `json:"salesforceReportPath"`
	SalesforceReportSHA256 string   `json:"salesforceReportSha256"`
}) {
	t.Helper()
	var local struct {
		Status   string `json:"status"`
		ExitCode int    `json:"exitCode"`
	}
	readJSON(t, filepath.Join(root, comparison.GladeReportPath), &local)
	if local.Status != "passed" || local.ExitCode != 0 {
		t.Fatalf("Glade report %s = %#v", comparison.CaseID, local)
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
	readJSON(t, filepath.Join(root, comparison.SalesforceReportPath), &sf)
	if sf.Status != 0 || !sf.Result.Success || !sf.Result.Compiled || sf.Result.CompileProblem != "" || sf.Result.ExceptionMessage != "" {
		t.Fatalf("Salesforce report %s = %#v", comparison.CaseID, sf)
	}
}
