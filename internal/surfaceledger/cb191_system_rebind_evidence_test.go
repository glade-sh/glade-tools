package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

const (
	cb191CandidateCommit = "381e27e47e720ac7dbecb906bf275d2d5bcdd37f"
	cb191CandidateSHA256 = "976c2ceceb2a0936889129d2dc7314d2065fbc538caf54bbb534f20e4f588166"
)

type cb191CandidateIdentity struct {
	Commit string `json:"commit"`
	SHA256 string `json:"sha256"`
}

type cb191FixtureEnvelope struct {
	Name       string                 `json:"name"`
	PacketID   string                 `json:"packetId"`
	APIVersion string                 `json:"apiVersion"`
	Candidate  cb191CandidateIdentity `json:"candidate"`
}

type cb191ComparisonRow struct {
	CaseID           string   `json:"caseId"`
	Status           string   `json:"status"`
	SurfaceIDs       []string `json:"surfaceIds"`
	SFObservation    string   `json:"sfObservation"`
	GladeObservation string   `json:"gladeObservation"`
}

type cb191ExcludedCase struct {
	CaseID         string   `json:"caseId"`
	Status         string   `json:"status"`
	Credited       bool     `json:"credited"`
	SurfaceIDs     []string `json:"surfaceIds"`
	Reason         string   `json:"reason"`
	MismatchStatus string   `json:"mismatchStatus"`
	MismatchFamily string   `json:"mismatchFamily"`
}

type cb191ComparisonEnvelope struct {
	PacketID            string                 `json:"packetId"`
	APIVersion          string                 `json:"apiVersion"`
	Candidate           cb191CandidateIdentity `json:"candidate"`
	SalesforceExecution string                 `json:"salesforceExecution"`
	Comparisons         []cb191ComparisonRow   `json:"comparisons"`
	Excluded            []cb191ExcludedCase    `json:"excluded"`
}

func TestCB191SystemRebindRowsHaveExactDualEvidence(t *testing.T) {
	root := filepath.Join("..", "..")
	fixturePath := filepath.Join(root, "docs", "fixtures", "current-base-cb191-system-rebind-positive-api67.json")
	comparisonPath := filepath.Join(root, "docs", "fixtures", "salesforce-cb191-system-rebind-comparisons.json")

	var fixtureMeta cb191FixtureEnvelope
	readJSON(t, fixturePath, &fixtureMeta)
	if fixtureMeta.Name != "current-base-cb191-system-rebind-positive-api67" || fixtureMeta.PacketID != "CB191" || fixtureMeta.APIVersion != "67.0" {
		t.Fatalf("fixture metadata = %#v", fixtureMeta)
	}
	assertCB191Candidate(t, fixtureMeta.Candidate)

	fixture, err := compat.LoadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if fixture.Name != fixtureMeta.Name {
		t.Fatalf("loaded fixture name = %q, metadata name = %q", fixture.Name, fixtureMeta.Name)
	}
	fixtureEvidence, err := BuildEvidenceSnapshot([]string{fixturePath})
	if err != nil {
		t.Fatal(err)
	}
	transferred := map[string]bool{
		"apex:System.RoundingMode.UNNECESSARY":                             true,
		"apex:System.RoundingMode.equals(Object)":                          true,
		"apex:System.RoundingMode.hashCode()":                              true,
		"apex:System.RoundingMode.ordinal()":                               true,
		"apex:System.RoundingMode.values()":                                true,
		"apex:System.SObject.addError(Schema.SObjectField,String)":         true,
		"apex:System.SObject.addError(Schema.SObjectField,String,Boolean)": true,
		"apex:System.SObject.get(Schema.SObjectField)":                     true,
		"apex:System.SObject.isSet(Schema.SObjectField)":                   true,
		"apex:System.SObject.put(Schema.SObjectField,Object)":              true,
	}
	for _, name := range []string{"core-runtime-enum-families-wave15-runtime.json", "data-runtime-sobject-helper-wave15-runtime.json"} {
		rows, err := BuildEvidenceSnapshot([]string{filepath.Join(root, "docs", "fixtures", name)})
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			if transferred[row.SurfaceID] {
				fixtureEvidence = append(fixtureEvidence, row)
			}
		}
	}
	gladeSnapshot := BuildGladeSnapshot()
	gladeByID := rowsBySurfaceKey(gladeSnapshot)
	for _, row := range fixtureEvidence {
		canonical, ok := gladeByID[surfaceIDKey(row.SurfaceID)]
		if !ok {
			t.Errorf("CB191 fixture surface %s is absent from current Glade snapshot", row.SurfaceID)
		} else if row.SurfaceID != canonical.SurfaceID {
			t.Errorf("CB191 fixture surface %s is not canonical snapshot ID %s", row.SurfaceID, canonical.SurfaceID)
		}
	}

	var comparisonMeta cb191ComparisonEnvelope
	readJSON(t, comparisonPath, &comparisonMeta)
	if comparisonMeta.PacketID != "CB191" || comparisonMeta.APIVersion != "67.0" {
		t.Fatalf("comparison metadata = %#v", comparisonMeta)
	}
	assertCB191Candidate(t, comparisonMeta.Candidate)
	if comparisonMeta.SalesforceExecution != "retained-reuse-no-reexecution" {
		t.Fatalf("salesforce execution = %q", comparisonMeta.SalesforceExecution)
	}
	if len(comparisonMeta.Comparisons) != 164 {
		t.Fatalf("source-pass comparison cases = %d, want 164", len(comparisonMeta.Comparisons))
	}
	if len(comparisonMeta.Excluded) != 70 {
		t.Fatalf("excluded cases = %d, want 70", len(comparisonMeta.Excluded))
	}

	excludedIDs := map[string]bool{}
	inconclusiveCount := 0
	mismatchCount := 0
	mismatchCaseID := ""
	for _, excluded := range comparisonMeta.Excluded {
		if excluded.Credited {
			t.Errorf("excluded case %s is credited", excluded.CaseID)
		}
		if excluded.Status == "inconclusive" {
			inconclusiveCount++
		} else if excluded.Status == "mismatch" {
			mismatchCount++
			mismatchCaseID = excluded.CaseID
		}
		if excluded.Reason == "" {
			t.Errorf("excluded case %s has no reason", excluded.CaseID)
		}
		excludedIDs[excluded.CaseID] = true
	}
	if inconclusiveCount != 69 || mismatchCount != 1 {
		t.Fatalf("excluded statuses = inconclusive %d mismatch %d, want 69 and 1", inconclusiveCount, mismatchCount)
	}
	if mismatchCaseID != "cb109-handler-exception-family" {
		t.Fatalf("mismatch case = %q", mismatchCaseID)
	}

	creditedCaseCount := 0
	for _, comparison := range comparisonMeta.Comparisons {
		if comparison.Status != "pass" || comparison.SFObservation == "" || comparison.GladeObservation == "" {
			t.Errorf("source-pass case %s is not a complete pass observation", comparison.CaseID)
		}
		if excludedIDs[comparison.CaseID] {
			t.Errorf("excluded case %s appears in source-pass comparisons", comparison.CaseID)
		}
		if len(comparison.SurfaceIDs) > 0 {
			creditedCaseCount++
		}
	}
	if creditedCaseCount != 111 {
		t.Fatalf("snapshot-backed credited cases = %d, want 111", creditedCaseCount)
	}

	oracleEvidence, err := BuildOracleEvidenceSnapshot([]string{comparisonPath})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range oracleEvidence {
		canonical, ok := gladeByID[surfaceIDKey(row.SurfaceID)]
		if !ok {
			t.Errorf("CB191 oracle surface %s is absent from current Glade snapshot", row.SurfaceID)
		} else if row.SurfaceID != canonical.SurfaceID {
			t.Errorf("CB191 oracle surface %s is not canonical snapshot ID %s", row.SurfaceID, canonical.SurfaceID)
		}
	}
	fixtureIDs := exactCB191SurfaceIDs(fixtureEvidence)
	oracleIDs := exactCB191SurfaceIDs(oracleEvidence)
	if !slices.Equal(fixtureIDs, oracleIDs) {
		t.Fatalf("fixture/oracle surface IDs differ: fixture %d oracle %d", len(fixtureIDs), len(oracleIDs))
	}

	ledger := Merge(nil, nil, gladeSnapshot, append(fixtureEvidence, oracleEvidence...))
	byID := rowsBySurfaceKey(ledger.Rows)
	for _, id := range fixtureIDs {
		row, ok := byID[surfaceIDKey(id)]
		if !ok {
			t.Fatalf("missing CB191 evidence row %s", id)
		}
		if row.Evidence != EvidenceFixtureAndOracle {
			t.Errorf("%s evidence = %s, want fixture-and-oracle", id, row.Evidence)
		}
		if row.GladeBehavior != BehaviorSupported {
			t.Errorf("%s behavior = %s, want supported", id, row.GladeBehavior)
		}
	}
}

func rowsBySurfaceKey(rows []SurfaceLedgerRow) map[string]SurfaceLedgerRow {
	out := map[string]SurfaceLedgerRow{}
	for _, row := range rows {
		out[surfaceIDKey(row.SurfaceID)] = row
	}
	return out
}

func assertCB191Candidate(t *testing.T, candidate cb191CandidateIdentity) {
	t.Helper()
	if candidate.Commit != cb191CandidateCommit || candidate.SHA256 != cb191CandidateSHA256 {
		t.Fatalf("candidate = %#v", candidate)
	}
}

func exactCB191SurfaceIDs(rows []SurfaceLedgerRow) []string {
	ids := make([]string, 0, len(rows))
	seen := map[string]bool{}
	for _, row := range rows {
		if !seen[row.SurfaceID] {
			seen[row.SurfaceID] = true
			ids = append(ids, row.SurfaceID)
		}
	}
	slices.Sort(ids)
	return ids
}

func readJSON(t *testing.T, path string, target any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatal(err)
	}
}
