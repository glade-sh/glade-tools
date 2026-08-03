package surfaceledger

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

const (
	metadataTypeCandidateCommit = "98ef01033dde700661fa150e3bae05116fa62ab8"
	metadataTypeCandidateSHA256 = "ab3b86d0fb96c92068de7ba82d56378e6ebac468ebe5129813c600a7e832607a"
	metadataTypeOracleSHA256    = "6e277ec4d136830fbb232d966c37a4e5b372df42421aad71b6d412ccd16d44b4"
)

var metadataTypeSurfaceIDs = []string{
	"apex:Metadata.MetadataType",
	"apex:Metadata.MetadataType.CustomMetadata",
}

type metadataTypeEvidenceFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type metadataTypeCandidateIdentity struct {
	Commit string `json:"commit"`
	SHA256 string `json:"sha256"`
}

type metadataTypeComparisonRow struct {
	CaseID              string   `json:"caseId"`
	Status              string   `json:"status"`
	SurfaceIDs          []string `json:"surfaceIds"`
	ExpectedObservation string   `json:"expectedObservation"`
	SFObservation       string   `json:"sfObservation"`
	GladeObservation    string   `json:"gladeObservation"`
}

type metadataTypeComparisonEnvelope struct {
	Name                string                        `json:"name"`
	APIVersion          string                        `json:"apiVersion"`
	Mode                string                        `json:"mode"`
	Candidate           metadataTypeCandidateIdentity `json:"candidate"`
	SalesforceExecution string                        `json:"salesforceExecution"`
	RetainedOracle      struct {
		metadataTypeEvidenceFile
		CaseID string `json:"caseId"`
	} `json:"retainedOracle"`
	LocalFixtures []metadataTypeEvidenceFile  `json:"localFixtures"`
	Comparisons   []metadataTypeComparisonRow `json:"comparisons"`
}

func TestMetadataTypeCustomMetadataRowsHaveExactCurrentDualEvidence(t *testing.T) {
	toolsRoot := filepath.Join("..", "..")
	fixturePaths := []string{
		filepath.Join(toolsRoot, "docs", "fixtures", "core-runtime-cb62-metadata-evidence.json"),
		filepath.Join(toolsRoot, "docs", "fixtures", "core-runtime-metadata-operations-custommetadata.json"),
	}
	for _, path := range fixturePaths {
		fixture, err := compat.LoadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		result, err := compat.Run(fixture)
		if err != nil {
			t.Fatalf("run %s: %v", path, err)
		}
		if !result.OK {
			t.Fatalf("run %s: %#v", path, result)
		}
	}

	fixtureEvidence, err := BuildEvidenceSnapshot(fixturePaths)
	if err != nil {
		t.Fatal(err)
	}
	fixtureIDs := evidenceIDsInSet(fixtureEvidence, metadataTypeSurfaceIDs)
	if !slices.Equal(fixtureIDs, metadataTypeSurfaceIDs) {
		t.Fatalf("fixture IDs = %v, want %v", fixtureIDs, metadataTypeSurfaceIDs)
	}

	comparisonPath := filepath.Join(toolsRoot, "docs", "fixtures", "salesforce-metadata-metadatatype-custommetadata-comparisons.json")
	var comparison metadataTypeComparisonEnvelope
	readJSON(t, comparisonPath, &comparison)
	if comparison.Name != "salesforce-metadata-metadatatype-custommetadata-comparisons" || comparison.APIVersion != "67.0" || comparison.Mode != "anonymous" {
		t.Fatalf("comparison metadata = %#v", comparison)
	}
	if comparison.Candidate.Commit != metadataTypeCandidateCommit || comparison.Candidate.SHA256 != metadataTypeCandidateSHA256 {
		t.Fatalf("candidate = %#v", comparison.Candidate)
	}
	if comparison.SalesforceExecution != "retained-reuse-no-reexecution" || comparison.RetainedOracle.CaseID != "metadata-metadatatype-custommetadata" {
		t.Fatalf("retained oracle metadata = %#v", comparison.RetainedOracle)
	}
	if len(comparison.Comparisons) != 1 || comparison.Comparisons[0].CaseID != comparison.RetainedOracle.CaseID {
		t.Fatalf("comparisons = %#v", comparison.Comparisons)
	}
	if got := append([]string(nil), comparison.Comparisons[0].SurfaceIDs...); !slices.Equal(got, metadataTypeSurfaceIDs) {
		t.Fatalf("comparison IDs = %v, want %v", got, metadataTypeSurfaceIDs)
	}

	evidenceRoot := metadataTypeEvidenceRoot(t, toolsRoot)
	assertMetadataTypeSHA256(t, filepath.Join(evidenceRoot, "evidence", "glade-candidate-sitefix"), metadataTypeCandidateSHA256)
	retainedPath := filepath.Join(evidenceRoot, comparison.RetainedOracle.Path)
	assertMetadataTypeSHA256(t, retainedPath, metadataTypeOracleSHA256)
	if comparison.RetainedOracle.SHA256 != metadataTypeOracleSHA256 {
		t.Fatalf("retained oracle SHA = %q", comparison.RetainedOracle.SHA256)
	}
	var retained struct {
		Comparisons []metadataTypeComparisonRow `json:"comparisons"`
	}
	readJSON(t, retainedPath, &retained)
	var retainedCase metadataTypeComparisonRow
	for _, row := range retained.Comparisons {
		if row.CaseID == comparison.RetainedOracle.CaseID {
			retainedCase = row
			break
		}
	}
	if !reflect.DeepEqual(comparison.Comparisons[0], retainedCase) {
		t.Fatalf("checked comparison differs from retained API 67 case")
	}

	if len(comparison.LocalFixtures) != len(fixturePaths) {
		t.Fatalf("local fixtures = %#v", comparison.LocalFixtures)
	}
	for index, source := range comparison.LocalFixtures {
		if filepath.Clean(source.Path) != filepath.Clean("docs/fixtures/"+filepath.Base(fixturePaths[index])) {
			t.Fatalf("local fixture %d path = %q", index, source.Path)
		}
		assertMetadataTypeSHA256(t, filepath.Join(toolsRoot, source.Path), source.SHA256)
	}

	oracleEvidence, err := BuildOracleEvidenceSnapshot([]string{comparisonPath})
	if err != nil {
		t.Fatal(err)
	}
	if got := evidenceIDsInSet(oracleEvidence, metadataTypeSurfaceIDs); !slices.Equal(got, metadataTypeSurfaceIDs) {
		t.Fatalf("oracle IDs = %v, want %v", got, metadataTypeSurfaceIDs)
	}
	ledger := Merge(nil, nil, BuildGladeSnapshot(), append(fixtureEvidence, oracleEvidence...))
	byID := rowsBySurfaceKey(ledger.Rows)
	for _, id := range metadataTypeSurfaceIDs {
		row, ok := byID[surfaceIDKey(id)]
		if !ok || row.Evidence != EvidenceFixtureAndOracle || row.GladeBehavior != BehaviorSupported {
			t.Fatalf("%s ledger row = %#v", id, row)
		}
	}
}

func evidenceIDsInSet(rows []SurfaceLedgerRow, wanted []string) []string {
	wantedSet := make(map[string]bool, len(wanted))
	for _, id := range wanted {
		wantedSet[id] = true
	}
	var ids []string
	for _, row := range rows {
		if wantedSet[row.SurfaceID] {
			ids = append(ids, row.SurfaceID)
		}
	}
	slices.Sort(ids)
	return slices.Compact(ids)
}

func metadataTypeEvidenceRoot(t *testing.T, toolsRoot string) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join(toolsRoot, "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "evidence", "glade-candidate-sitefix")); err != nil {
		t.Fatalf("locate current-base evidence root: %v", err)
	}
	return root
}

func assertMetadataTypeSHA256(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := sha256.Sum256(data)
	if hex.EncodeToString(got[:]) != want {
		t.Fatalf("SHA-256 %s = %s, want %s", path, hex.EncodeToString(got[:]), want)
	}
}
