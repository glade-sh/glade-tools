package surfaceledger

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

const (
	cb198CandidateCommit        = "fd687589"
	cb198CandidateSHA256        = "b4b4f6797a9d661699a0d2bf4662fe3c64d8b7ec9e9716c855704ecb89876d72"
	cb198ExpectedDigest         = "fc0c09ac0a6f06f2363828d716c37d64ccf2b891a0d9fdba2c22bcf0a5e2fb82"
	cb198ProfilePath            = "evidence/current-base/profile-381e27e4-cb191-20260802-1505/apex-support-profile.json"
	cb198ProfileSHA256          = "d50cde1597365eb721c796e6e071d30e924e23e0e5b42181a3296dfc5a4c7200"
	cb198SnapshotName           = "GLADE_SNAPSHOT.json"
	cb198SnapshotPath           = "evidence/current-base/surface-381e27e4-cb191-20260802-1505/GLADE_SNAPSHOT.json"
	cb198SnapshotSHA256         = "07389a70201b1e14020278c7bea88e24afaa98398864861ddfc36b02ae8dd7e4"
	cb198SourcePath             = "evidence/current-base/cb194-schema-database-wide-api67/soaptype.apex"
	cb198SourceSHA256           = "9766948a8a7982667ee2f51dd2d07388cd3293f9e9c8f5714d869da178278fec"
	cb198SalesforceResultPath   = "evidence/current-base/cb194-schema-database-wide-api67/raw/salesforce/soaptype-result.json"
	cb198CandidateResultPath    = "evidence/current-base/cb194-schema-database-wide-api67/raw/candidate/soaptype-result.json"
	cb198RetainedComparisonPath = "evidence/current-base/cb194-schema-database-wide-api67/soaptype-comparison.json"
	cb198ReplayResultPath       = "evidence/current-base/cb194-schema-database-wide-api67/raw/candidate/soaptype-fd687589-result.json"
	cb198RowMapPath             = "evidence/current-base/cb194-schema-database-wide-api67/soaptype-row-map.json"
)

var cb198SoapTypeMethods = []string{
	"apex:Schema.SoapType.equals(Object)",
	"apex:Schema.SoapType.hashCode()",
	"apex:Schema.SoapType.ordinal()",
	"apex:Schema.SoapType.valueOf(String)",
	"apex:Schema.SoapType.values()",
}

type cb198Identity struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type cb198Candidate struct {
	Commit string `json:"commit"`
	SHA256 string `json:"sha256"`
}

type cb198Profile struct {
	Path             string   `json:"path"`
	SHA256           string   `json:"sha256"`
	Selection        string   `json:"selection"`
	Areas            []string `json:"areas"`
	SelectedRowCount int      `json:"selectedRowCount"`
}

type cb198Replay struct {
	Path          string `json:"path"`
	CandidateSHA  string `json:"candidateSha256"`
	Digest        string `json:"digest"`
	SequenceCount int    `json:"sequenceCount"`
}

type cb198Scenario struct {
	SourcePath   string `json:"sourcePath"`
	SourceSHA256 string `json:"sourceSha256"`
}

type cb198Retained struct {
	SourcePath           string `json:"sourcePath"`
	SalesforceResultPath string `json:"salesforceResultPath"`
	CandidateResultPath  string `json:"candidateResultPath"`
	ComparisonPath       string `json:"comparisonPath"`
	RowMapPath           string `json:"rowMapPath"`
}

type cb198FixtureEnvelope struct {
	Name              string         `json:"name"`
	PacketID          string         `json:"packetId"`
	APIVersion        string         `json:"apiVersion"`
	Mode              string         `json:"mode"`
	Candidate         cb198Candidate `json:"candidate"`
	Profile           cb198Profile   `json:"profile"`
	CanonicalSnapshot cb198Identity  `json:"canonicalSnapshot"`
	Scenario          cb198Scenario  `json:"scenario"`
	Replay            cb198Replay    `json:"replay"`
	Retained          cb198Retained  `json:"retained"`
	CreditScope       string         `json:"creditScope"`
	NotCredited       []string       `json:"notCredited"`
}

type cb198ComparisonRow struct {
	CaseID           string   `json:"caseId"`
	Status           string   `json:"status"`
	SurfaceIDs       []string `json:"surfaceIds"`
	SFObservation    string   `json:"sfObservation"`
	GladeObservation string   `json:"gladeObservation"`
}

type cb198ExcludedCase struct {
	CaseID     string   `json:"caseId"`
	Status     string   `json:"status"`
	Credited   bool     `json:"credited"`
	SurfaceIDs []string `json:"surfaceIds"`
	Reason     string   `json:"reason"`
}

type cb198ComparisonEnvelope struct {
	Name              string               `json:"name"`
	PacketID          string               `json:"packetId"`
	APIVersion        string               `json:"apiVersion"`
	Mode              string               `json:"mode"`
	Candidate         cb198Candidate       `json:"candidate"`
	Profile           cb198Profile         `json:"profile"`
	CanonicalSnapshot cb198Identity        `json:"canonicalSnapshot"`
	Scenario          cb198Scenario        `json:"scenario"`
	Replay            cb198Replay          `json:"replay"`
	Retained          cb198Retained        `json:"retained"`
	CreditScope       string               `json:"creditScope"`
	NotCredited       []string             `json:"notCredited"`
	Salesforce        cb198SalesforceRun   `json:"salesforce"`
	Comparisons       []cb198ComparisonRow `json:"comparisons"`
	Excluded          []cb198ExcludedCase  `json:"excluded"`
	Mismatches        []any                `json:"mismatches"`
}

type cb198SalesforceRun struct {
	Execution  string `json:"execution"`
	APIversion string `json:"apiVersion"`
	ResultPath string `json:"resultPath"`
	Result     string `json:"result"`
}

type cb198RowMap struct {
	Contract         string `json:"contract"`
	CandidateCount   int    `json:"candidateCount"`
	SalesforceCount  int    `json:"salesforceCount"`
	CandidateDigest  string `json:"candidateDigest"`
	SalesforceDigest string `json:"salesforceDigest"`
	ExpectedDigest   string `json:"expectedDigest"`
	ExactRows        bool   `json:"exactRows"`
	FirstMismatch    int    `json:"firstMismatch"`
	Rows             []struct {
		SurfaceID string `json:"surfaceId"`
		Name      string `json:"name"`
		Ordinal   int    `json:"ordinal"`
		HashCode  int    `json:"hashCode"`
		Row       string `json:"row"`
		Index     int    `json:"index"`
	} `json:"rows"`
}

type cb198CandidateResult struct {
	Status string `json:"status"`
	Data   struct {
		Debug []string `json:"debug"`
	} `json:"data"`
}

type cb198SalesforceResult struct {
	Result struct {
		Logs string `json:"logs"`
	} `json:"result"`
}

func TestCB198SchemaSoapTypeRowsHaveExactDualEvidence(t *testing.T) {
	root := filepath.Join("..", "..")
	fixturePath := filepath.Join(root, "docs", "fixtures", "current-base-cb198-schema-soaptype-positive-api67.json")
	comparisonPath := filepath.Join(root, "docs", "fixtures", "salesforce-cb198-schema-soaptype-comparisons.json")
	fixture := cb198ReadFixture(t, fixturePath)
	comparison := cb198ReadComparison(t, comparisonPath)
	if fixture.Name != "current-base-cb198-schema-soaptype-positive-api67" || fixture.PacketID != "CB198" || fixture.APIVersion != "67.0" || fixture.Mode != "exec" {
		t.Fatalf("fixture metadata = %#v", fixture)
	}
	if comparison.Name != "salesforce-cb198-schema-soaptype-comparisons" || comparison.PacketID != "CB198" || comparison.APIVersion != "67.0" || comparison.Mode != "exec" {
		t.Fatalf("comparison metadata = %#v", comparison)
	}
	if fixture.CreditScope != "Schema.SoapType only" || comparison.CreditScope != "Schema.SoapType only" || !slices.Equal(fixture.NotCredited, comparison.NotCredited) || len(comparison.NotCredited) != 2 || !strings.Contains(comparison.NotCredited[0], "DescribeSObjectResult") || !strings.Contains(comparison.NotCredited[1], "Database") {
		t.Fatalf("credit boundary = fixture %#v comparison %#v", fixture, comparison)
	}
	cb198AssertCandidate(t, fixture.Candidate)
	cb198AssertCandidate(t, comparison.Candidate)
	cb198AssertProfile(t, fixture.Profile)
	cb198AssertProfile(t, comparison.Profile)
	cb198AssertSnapshot(t, fixture.CanonicalSnapshot)
	cb198AssertSnapshot(t, comparison.CanonicalSnapshot)
	if fixture.Scenario.SourcePath != cb198SourcePath || fixture.Scenario.SourceSHA256 != cb198SourceSHA256 || comparison.Scenario.SourcePath != cb198SourcePath || comparison.Scenario.SourceSHA256 != cb198SourceSHA256 {
		t.Fatalf("source identity = fixture %#v comparison %#v", fixture.Scenario, comparison.Scenario)
	}
	if fixture.Replay != comparison.Replay || fixture.Retained != comparison.Retained {
		t.Fatalf("replay/retained identity differs between fixture and comparison")
	}
	if fixture.Replay.Path != cb198ReplayResultPath || fixture.Replay.CandidateSHA != cb198CandidateSHA256 || fixture.Replay.Digest != cb198ExpectedDigest || fixture.Replay.SequenceCount != 1302 {
		t.Fatalf("replay identity = %#v", fixture.Replay)
	}
	if fixture.Retained.SourcePath != cb198SourcePath || fixture.Retained.SalesforceResultPath != cb198SalesforceResultPath || fixture.Retained.CandidateResultPath != cb198CandidateResultPath || fixture.Retained.ComparisonPath != cb198RetainedComparisonPath || fixture.Retained.RowMapPath != cb198RowMapPath {
		t.Fatalf("retained identity = %#v", fixture.Retained)
	}
	if comparison.Salesforce.Execution != "retained-reuse-no-reexecution" || comparison.Salesforce.APIversion != "67.0" || comparison.Salesforce.ResultPath != cb198SalesforceResultPath || comparison.Salesforce.Result != "Pass" {
		t.Fatalf("Salesforce evidence = %#v", comparison.Salesforce)
	}

	evidenceRoot := filepath.Join(root, "..", "..", "..")
	rowMapPath := filepath.Join(evidenceRoot, cb198RowMapPath)
	var rowMap cb198RowMap
	cb198ReadJSON(t, rowMapPath, &rowMap)
	if rowMap.Contract != "Schema.SoapType.values/valueOf/equals/hashCode/ordinal" || rowMap.CandidateCount != 1302 || rowMap.SalesforceCount != 1302 || rowMap.CandidateDigest != cb198ExpectedDigest || rowMap.SalesforceDigest != cb198ExpectedDigest || rowMap.ExpectedDigest != cb198ExpectedDigest || !rowMap.ExactRows || rowMap.FirstMismatch != -1 || len(rowMap.Rows) != 1302 {
		t.Fatalf("retained row map metadata = %#v", rowMap)
	}
	wantIDs := []string{"apex:Schema.SoapType"}
	for index, row := range rowMap.Rows {
		if row.Index != index || row.SurfaceID != "apex:Schema.SoapType."+row.Name || row.Ordinal != index || row.Row != row.Name+"|"+itoa(row.Ordinal)+"|"+itoa(row.HashCode) {
			t.Fatalf("retained row map row %d = %#v", index, row)
		}
		wantIDs = append(wantIDs, row.SurfaceID)
	}
	wantIDs = append(wantIDs, cb198SoapTypeMethods...)
	slices.Sort(wantIDs)
	if len(wantIDs) != 1308 {
		t.Fatalf("canonical SoapType IDs = %d, want 1308", len(wantIDs))
	}

	fixturePathIDs, err := compat.LoadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if fixturePathIDs.Name != fixture.Name {
		t.Fatalf("loaded fixture name = %q, metadata name = %q", fixturePathIDs.Name, fixture.Name)
	}
	fixtureEvidence, err := BuildEvidenceSnapshot([]string{fixturePath})
	if err != nil {
		t.Fatal(err)
	}
	oracleEvidence, err := BuildOracleEvidenceSnapshot([]string{comparisonPath})
	if err != nil {
		t.Fatal(err)
	}
	assertExactSurfaceSet(t, fixtureEvidence, wantIDs)
	assertExactSurfaceSet(t, oracleEvidence, wantIDs)

	if len(comparison.Comparisons) != 1 || comparison.Comparisons[0].CaseID != "cb198-schema-soaptype-contract" || comparison.Comparisons[0].Status != "pass" || comparison.Comparisons[0].SFObservation == "" || comparison.Comparisons[0].GladeObservation == "" {
		t.Fatalf("comparisons = %#v", comparison.Comparisons)
	}
	if comparison.Comparisons[0].SurfaceIDs == nil || !slices.Equal(sortedCB198IDs(comparison.Comparisons[0].SurfaceIDs), wantIDs) {
		t.Fatalf("comparison surface IDs do not exactly match canonical IDs")
	}
	if len(comparison.Excluded) != 2 || len(comparison.Mismatches) != 0 {
		t.Fatalf("excluded/mismatches counts = %d/%d", len(comparison.Excluded), len(comparison.Mismatches))
	}
	for _, excluded := range comparison.Excluded {
		if excluded.Credited || excluded.Status != "inconclusive" || excluded.Reason == "" || len(excluded.SurfaceIDs) == 0 {
			t.Fatalf("excluded case = %#v", excluded)
		}
		for _, id := range excluded.SurfaceIDs {
			if strings.Contains(id, "Schema.SoapType") {
				t.Fatalf("excluded case credits or overlaps SoapType: %#v", excluded)
			}
		}
	}

	cb198AssertArtifact(t, filepath.Join(evidenceRoot, cb198CandidateResultPath), "retained original candidate result", "")
	cb198AssertArtifact(t, filepath.Join(evidenceRoot, cb198RetainedComparisonPath), "retained comparison", "")
	cb198AssertArtifact(t, filepath.Join(evidenceRoot, cb198ReplayResultPath), "fd687589 replay result", "")
	cb198AssertArtifact(t, filepath.Join(evidenceRoot, cb198SourcePath), "retained source", cb198SourceSHA256)
	cb198AssertArtifact(t, filepath.Join(evidenceRoot, cb198ProfilePath), "profile", cb198ProfileSHA256)
	cb198AssertArtifact(t, filepath.Join(evidenceRoot, cb198SnapshotPath), "snapshot", cb198SnapshotSHA256)
	cb198AssertCandidateBinary(t, filepath.Join(evidenceRoot, "evidence", "glade-candidate-fd687589"))

	var candidateResult cb198CandidateResult
	cb198ReadJSON(t, filepath.Join(evidenceRoot, cb198ReplayResultPath), &candidateResult)
	var salesforceResult cb198SalesforceResult
	cb198ReadJSON(t, filepath.Join(evidenceRoot, cb198SalesforceResultPath), &salesforceResult)
	candidateRows := cb198MarkerRows(t, strings.Join(candidateResult.Data.Debug, "\n"), "GLADE_ORACLE_SCHEMA_SOAP_TYPE_ROWS=", false)
	salesforceRows := cb198MarkerRows(t, salesforceResult.Result.Logs, "GLADE_ORACLE_SCHEMA_SOAP_TYPE_ROWS=", true)
	if !slices.Equal(candidateRows, salesforceRows) || len(candidateRows) != 1302 {
		t.Fatalf("fd687589 replay sequence differs from retained Salesforce sequence")
	}
	if got := cb198Digest(candidateRows); got != cb198ExpectedDigest {
		t.Fatalf("fd687589 replay digest = %s, want %s", got, cb198ExpectedDigest)
	}

	ledger := Merge(nil, nil, BuildGladeSnapshot(), append(fixtureEvidence, oracleEvidence...))
	byID := rowsByID(ledger.Rows)
	for _, id := range wantIDs {
		row, ok := byID[id]
		if !ok {
			t.Fatalf("missing CB198 evidence row %s", id)
		}
		if row.Evidence != EvidenceFixtureAndOracle {
			t.Errorf("%s evidence = %s, want fixture-and-oracle", id, row.Evidence)
		}
		if row.GladeBehavior != BehaviorSupported {
			t.Errorf("%s behavior = %s, want supported", id, row.GladeBehavior)
		}
	}
}

func cb198AssertCandidate(t *testing.T, candidate cb198Candidate) {
	t.Helper()
	if candidate.Commit != cb198CandidateCommit || candidate.SHA256 != cb198CandidateSHA256 {
		t.Fatalf("candidate = %#v", candidate)
	}
}

func cb198AssertProfile(t *testing.T, profile cb198Profile) {
	t.Helper()
	if profile.Path != cb198ProfilePath || profile.SHA256 != cb198ProfileSHA256 || profile.Selection != "local-runtime-required" || !slices.Equal(profile.Areas, []string{"Schema.SoapType"}) || profile.SelectedRowCount != 1308 {
		t.Fatalf("profile = %#v", profile)
	}
}

func cb198AssertSnapshot(t *testing.T, snapshot cb198Identity) {
	t.Helper()
	if snapshot.Path != cb198SnapshotPath || snapshot.SHA256 != cb198SnapshotSHA256 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func cb198ReadFixture(t *testing.T, path string) cb198FixtureEnvelope {
	t.Helper()
	var fixture cb198FixtureEnvelope
	cb198ReadJSON(t, path, &fixture)
	return fixture
}

func cb198ReadComparison(t *testing.T, path string) cb198ComparisonEnvelope {
	t.Helper()
	var comparison cb198ComparisonEnvelope
	cb198ReadJSON(t, path, &comparison)
	return comparison
}

func cb198ReadJSON(t *testing.T, path string, target any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatal(err)
	}
}

func cb198AssertArtifact(t *testing.T, path, label, wantSHA256 string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s %s: %v", label, path, err)
	}
	if wantSHA256 != "" {
		if got := fmt.Sprintf("%x", sha256.Sum256(data)); got != wantSHA256 {
			t.Fatalf("%s SHA-256 = %s, want %s", label, got, wantSHA256)
		}
	}
}

func cb198AssertCandidateBinary(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(data)); got != cb198CandidateSHA256 {
		t.Fatalf("candidate binary SHA-256 = %s, want %s", got, cb198CandidateSHA256)
	}
}

func cb198MarkerRows(t *testing.T, output, marker string, htmlEncoded bool) []string {
	t.Helper()
	for _, line := range strings.Split(output, "\n") {
		if !strings.Contains(line, marker) {
			continue
		}
		if htmlEncoded && !strings.Contains(line, "|USER_DEBUG|") {
			continue
		}
		payload := line[strings.Index(line, marker)+len(marker):]
		if htmlEncoded {
			payload = strings.ReplaceAll(payload, "&#124;", "|")
		}
		var rows []string
		if err := json.Unmarshal([]byte(payload), &rows); err != nil {
			t.Fatal(err)
		}
		return rows
	}
	t.Fatalf("marker %q not found", marker)
	return nil
}

func cb198Digest(rows []string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(rows, "\n"))))
}

func sortedCB198IDs(ids []string) []string {
	out := append([]string(nil), ids...)
	slices.Sort(out)
	return out
}

func itoa(value int) string {
	return fmt.Sprintf("%d", value)
}
