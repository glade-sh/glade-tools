package surfaceledger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildOracleEvidenceSnapshotCreatesRowsForLinkedPassingComparisons(t *testing.T) {
	path := writeOracleComparisonFile(t, `{
  "comparisons": [{
    "caseId": "case-linked",
    "status": "pass",
    "sfObservation": "value 1 Integer",
    "gladeObservation": "value 1 Integer",
    "surfaceIds": ["apex:System.Linked.one()", "apex:System.Linked.two()"]
  }]
}`)

	rows, err := BuildOracleEvidenceSnapshot([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	for _, row := range rows {
		if row.Evidence != EvidenceOracle || row.GladeShape != ShapeAbsent || row.GladeBehavior != BehaviorNone {
			t.Fatalf("oracle row must not assert Glade shape or behavior: %#v", row)
		}
		if !containsString(row.Sources, "oracle:case-linked:"+filepath.Clean(path)) {
			t.Fatalf("sources = %#v, want case and artifact path", row.Sources)
		}
		if !strings.Contains(row.Notes, "case-linked") || !strings.Contains(row.Notes, filepath.Clean(path)) {
			t.Fatalf("notes = %q, want case and artifact path", row.Notes)
		}
	}
}

func TestOracleEvidenceMergesWithFixtureEvidence(t *testing.T) {
	path := writeOracleComparisonFile(t, `[{"caseId":"case-linked","status":"pass","sfObservation":"sf","gladeObservation":"gl","surfaceIds":["apex:System.Linked.one()"]}]`)
	oracle, err := BuildOracleEvidenceSnapshot([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	fixture := RowFromEvidence(SurfaceLedgerRow{SurfaceID: "apex:System.Linked.one()"})
	ledger := Merge([]SurfaceLedgerRow{fixture}, nil, nil, oracle)
	if len(ledger.Rows) != 1 || ledger.Rows[0].Evidence != EvidenceFixtureAndOracle {
		t.Fatalf("ledger = %#v, want fixture-and-oracle row", ledger.Rows)
	}
}

func TestOracleEvidenceIgnoresUnqualifiedComparisons(t *testing.T) {
	path := writeOracleComparisonFile(t, `{"comparisons":[
    {"caseId":"no-observations","status":"pass","surfaceIds":["apex:System.NoObservations"]},
    {"caseId":"no-links","status":"pass","sfObservation":"sf","gladeObservation":"gl"},
    {"caseId":"blank-salesforce","status":"pass","sfObservation":"  ","gladeObservation":"gl","surfaceIds":["apex:System.BlankSalesforce"]},
    {"caseId":"blank-glade","status":"pass","sfObservation":"sf","gladeObservation":"\n\t","surfaceIds":["apex:System.BlankGlade"]},
    {"caseId":"fail","status":"fail","sfObservation":"sf","gladeObservation":"gl","surfaceIds":["apex:System.Fail"]},
    {"caseId":"inconclusive","status":"inconclusive","surfaceIds":["apex:System.Inconclusive"]}
  ]}`)
	rows, err := BuildOracleEvidenceSnapshot([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("rows = %#v, want no oracle evidence", rows)
	}
}

func TestOracleEvidenceRejectsMalformedLinkedSurfaceID(t *testing.T) {
	path := writeOracleComparisonFile(t, `[{"caseId":"bad-link","status":"pass","sfObservation":"sf","gladeObservation":"gl","surfaceIds":["not-a-canonical-surface-id"]}]`)
	_, err := BuildOracleEvidenceSnapshot([]string{path})
	if err == nil || !strings.Contains(err.Error(), "bad-link") || !strings.Contains(err.Error(), "surfaceId") {
		t.Fatalf("error = %v, want useful malformed linked data error", err)
	}
}

func TestOracleEvidenceReadsSalesforceVerificationReport(t *testing.T) {
	path := writeOracleComparisonFile(t, `{
  "runtime": {"cases": [{"id":"runtime-linked","status":"pass","salesforceObservation":"sf","gladeObservation":"gl","surfaceIds":["apex:System.RuntimeLinked()"]}]},
  "lifecycle": {"cases": []}
}`)
	rows, err := BuildOracleEvidenceSnapshot([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].SurfaceID != "apex:System.RuntimeLinked()" {
		t.Fatalf("rows = %#v", rows)
	}
}

func writeOracleComparisonFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "oracle.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
