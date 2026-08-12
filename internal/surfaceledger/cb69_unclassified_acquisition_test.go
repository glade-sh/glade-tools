package surfaceledger

import (
	"path/filepath"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

var cb69InventedSurfaceIDs = []string{
	"apex:Approval.Approval()",
	"apex:QueueableDuplicateSignature.QueueableDuplicateSignature()",
	"apex:ConnectApi.getError()",
	"apex:ConnectApi.getErrorMessage()",
	"apex:ConnectApi.getErrorTypeName()",
	"apex:ConnectApi.getResult()",
	"apex:ConnectApi.isSuccess()",
}

func TestCB69GeneratedSnapshotOmitsInventedConstructorsAndRetainsNearbyRows(t *testing.T) {
	byID := rowsByID(BuildGladeSnapshot())

	assertCB69RowsAbsent(t, byID, cb69InventedSurfaceIDs, "generated snapshot")
	assertCB69RowsPresent(t, byID, []string{
		"apex:Approval.process(Approval.ProcessRequest)",
		"apex:QueueableDuplicateSignature.builder()",
		"apex:QueueableDuplicateSignature.Builder.addString(String)",
	}, "generated snapshot")
}

func TestCB69ConnectAPIEvidenceSnapshotOmitsOnlyStaleExactRows(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "fixtures", "apex-connectapi-offplatform-unsupported-surfaces.json")
	fixture, err := compat.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fixtureIDs := make(map[string]bool, len(fixture.Evidence))
	for _, evidence := range fixture.Evidence {
		fixtureIDs[evidence.SurfaceID] = true
	}
	staleConnectAPIIDs := cb69InventedSurfaceIDs[2:]
	assertCB69IDsAbsent(t, fixtureIDs, staleConnectAPIIDs, "ConnectApi fixture")
	assertCB69IDsPresent(t, fixtureIDs, []string{
		"apex:ConnectApi.TaxPlatform",
	}, "ConnectApi fixture")

	rows, err := BuildEvidenceSnapshot([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	assertCB69RowsAbsent(t, rowsByID(rows), cb69InventedSurfaceIDs, "ConnectApi evidence snapshot")
	assertCB69RowsPresent(t, rowsByID(rows), []string{
		"apex:ConnectApi.TaxPlatform",
	}, "ConnectApi evidence snapshot")
}

func TestCB69FreshMergedLedgerAndProfileHaveNoInventedRows(t *testing.T) {
	fixturesDir := filepath.Join("..", "..", "docs", "fixtures")
	paths := []string{
		filepath.Join(fixturesDir, "apex-connectapi-offplatform-unsupported-surfaces.json"),
		filepath.Join(fixturesDir, "core-runtime-approval-local-engine-full.json"),
		filepath.Join(fixturesDir, "core-runtime-queueable-duplicate-signature-evidence.json"),
	}
	evidence, err := BuildEvidenceSnapshot(paths)
	if err != nil {
		t.Fatal(err)
	}
	ledger := Merge(nil, nil, BuildGladeSnapshot(), evidence)
	byID := rowsByID(ledger.Rows)
	assertCB69RowsAbsent(t, byID, cb69InventedSurfaceIDs, "fresh merged ledger")
	assertCB69RowsPresent(t, byID, []string{
		"apex:Approval.process(Approval.ProcessRequest)",
		"apex:QueueableDuplicateSignature.builder()",
		"apex:QueueableDuplicateSignature.Builder.addString(String)",
		"apex:ConnectApi.TaxPlatform",
	}, "fresh merged ledger")

	policy, err := LoadSupportPolicy(filepath.Join(fixturesDir, "apex-local-support-policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	profile := ComputeSupportProfile(ledger.Rows, policy, nil)
	for _, id := range cb69InventedSurfaceIDs {
		for _, row := range profile.UnclassifiedRows {
			if row.SurfaceID == id {
				t.Errorf("invented row remains unclassified in fresh profile: %s", id)
			}
		}
	}
}

func assertCB69RowsAbsent(t *testing.T, rows map[string]SurfaceLedgerRow, ids []string, source string) {
	t.Helper()
	for _, id := range ids {
		if _, ok := rows[id]; ok {
			t.Errorf("%s retains invented row %s", source, id)
		}
	}
}

func assertCB69RowsPresent(t *testing.T, rows map[string]SurfaceLedgerRow, ids []string, source string) {
	t.Helper()
	for _, id := range ids {
		if _, ok := rows[id]; !ok {
			t.Errorf("%s lost nearby real row %s", source, id)
		}
	}
}

func assertCB69IDsAbsent(t *testing.T, rows map[string]bool, ids []string, source string) {
	t.Helper()
	for _, id := range ids {
		if rows[id] {
			t.Errorf("%s retains invented row %s", source, id)
		}
	}
}

func assertCB69IDsPresent(t *testing.T, rows map[string]bool, ids []string, source string) {
	t.Helper()
	for _, id := range ids {
		if !rows[id] {
			t.Errorf("%s lost nearby real row %s", source, id)
		}
	}
}
