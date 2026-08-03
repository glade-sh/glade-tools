package surfaceledger

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

const metadataDTOBatchComparisonPath = "docs/fixtures/salesforce-metadata-dto-batch-20260803-comparisons.json"

var metadataDTOBatchSurfaceIDs = []string{
	"apex:Metadata.CustomMetadata",
	"apex:Metadata.CustomMetadataValue",
	"apex:Metadata.Metadata",
	"apex:Metadata.DeployContainer",
	"apex:Metadata.DeployResult",
	"apex:Metadata.DeployStatus",
	"apex:Metadata.DeployStatus.Succeeded",
	"apex:Metadata.DeployStatus.Failed",
	"apex:Metadata.DeployStatus.InProgress",
	"apex:Metadata.MetadataType",
	"apex:Metadata.MetadataType.CustomMetadata",
}

type metadataDTOBatchComparison struct {
	CaseID           string   `json:"caseId"`
	Status           string   `json:"status"`
	SurfaceIDs       []string `json:"surfaceIds"`
	SFObservation    string   `json:"sfObservation"`
	GladeObservation string   `json:"gladeObservation"`
}

type metadataDTOBatchEnvelope struct {
	Candidate struct {
		Commit string `json:"commit"`
		SHA256 string `json:"sha256"`
	} `json:"candidate"`
	SalesforceExecution string `json:"salesforceExecution"`
	SalesforceReport    struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	} `json:"salesforceReport"`
	GladeReport struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	} `json:"gladeReport"`
	LocalFixtures []struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	} `json:"localFixtures"`
	Comparisons []metadataDTOBatchComparison `json:"comparisons"`
}

func TestMetadataDTOBatchRowsHaveExactFixtureAndFreshOracleEvidence(t *testing.T) {
	toolsRoot := filepath.Join("..", "..")
	comparisonPath := filepath.Join(toolsRoot, metadataDTOBatchComparisonPath)
	var comparison metadataDTOBatchEnvelope
	readJSON(t, comparisonPath, &comparison)
	if comparison.Candidate.Commit != "6419bf1e8ede470d9fd5c6c789aede9ef5d2713d" || comparison.Candidate.SHA256 == "" || comparison.SalesforceExecution != "fresh-api67" {
		t.Fatalf("comparison provenance = %#v", comparison)
	}
	if len(comparison.LocalFixtures) != 2 || len(comparison.Comparisons) != 3 {
		t.Fatalf("comparison batch shape = %#v", comparison)
	}

	fixturePaths := make([]string, 0, len(comparison.LocalFixtures))
	for _, source := range comparison.LocalFixtures {
		path := filepath.Join(toolsRoot, source.Path)
		assertMetadataDTOBatchSHA256(t, path, source.SHA256)
		fixturePaths = append(fixturePaths, path)
	}

	fixtureEvidence, err := BuildEvidenceSnapshot(fixturePaths)
	if err != nil {
		t.Fatal(err)
	}
	assertExactIDs(t, evidenceIDsInSet(fixtureEvidence, metadataDTOBatchSurfaceIDs), metadataDTOBatchSurfaceIDs)

	var allComparisonIDs []string
	for _, row := range comparison.Comparisons {
		if row.Status != "pass" || row.CaseID == "" || row.SFObservation == "" || row.GladeObservation == "" {
			t.Fatalf("comparison row = %#v", row)
		}
		allComparisonIDs = append(allComparisonIDs, row.SurfaceIDs...)
	}
	assertExactIDs(t, allComparisonIDs, metadataDTOBatchSurfaceIDs)
	oracleEvidence, err := BuildOracleEvidenceSnapshot([]string{comparisonPath})
	if err != nil {
		t.Fatal(err)
	}
	assertExactIDs(t, evidenceIDsInSet(oracleEvidence, metadataDTOBatchSurfaceIDs), metadataDTOBatchSurfaceIDs)

	evidenceRoot, err := filepath.Abs(filepath.Join(toolsRoot, "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	assertMetadataDTOBatchSHA256(t, filepath.Join(evidenceRoot, "evidence", "glade-candidate-metadata-fix"), comparison.Candidate.SHA256)
	for _, report := range []struct{ path, sum string }{
		{comparison.SalesforceReport.Path, comparison.SalesforceReport.SHA256},
		{comparison.GladeReport.Path, comparison.GladeReport.SHA256},
	} {
		assertMetadataDTOBatchSHA256(t, filepath.Join(evidenceRoot, report.path), report.sum)
	}

	ledger := Merge(nil, nil, BuildGladeSnapshot(), append(fixtureEvidence, oracleEvidence...))
	byID := rowsBySurfaceKey(ledger.Rows)
	for _, id := range metadataDTOBatchSurfaceIDs {
		row, ok := byID[surfaceIDKey(id)]
		if !ok || row.Evidence != EvidenceFixtureAndOracle || row.GladeShape == ShapeAbsent || row.GladeBehavior != BehaviorSupported {
			t.Fatalf("%s ledger row = %#v", id, row)
		}
	}
}

func assertMetadataDTOBatchSHA256(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != want {
		t.Fatalf("SHA-256 %s = %s, want %s", path, got, want)
	}
}

func assertExactIDs(t *testing.T, got, want []string) {
	t.Helper()
	got = append([]string(nil), got...)
	want = append([]string(nil), want...)
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(slices.Compact(got), slices.Compact(want)) {
		t.Fatalf("surface IDs = %v, want %v", got, want)
	}
}
