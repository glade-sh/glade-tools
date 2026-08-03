package surfaceledger

import (
	"path/filepath"
	"testing"
)

func TestMergeExcludesAPI67RemovedSiteHelpers(t *testing.T) {
	rows := Merge(
		[]SurfaceLedgerRow{{SurfaceID: "apex:System.Site.getPrefix", Product: ProductApex}},
		[]SurfaceLedgerRow{{SurfaceID: "apex:System.Site.getPrefix()", Product: ProductApex}},
		nil,
		nil,
	).Rows
	for _, row := range rows {
		if isAPI67RemovedSurfaceID(row.SurfaceID) {
			t.Fatalf("removed Site helper entered current-base ledger: %s", row.SurfaceID)
		}
	}
}

func TestMergeExcludesAPI67InvalidDatabaseAllowCalloutsRows(t *testing.T) {
	ids := []string{
		"apex:System.Database.insertAsync(Object,Database.AllowCallouts,AccessLevel)",
		"apex:System.Database.insertAsync(List<Object>,Database.AllowCallouts,AccessLevel)",
		"apex:System.Database.updateAsync(Object,Database.AllowCallouts,AccessLevel)",
		"apex:System.Database.updateAsync(List<Object>,Database.AllowCallouts,AccessLevel)",
		"apex:System.Database.deleteAsync(Object,Database.AllowCallouts,AccessLevel)",
		"apex:System.Database.deleteAsync(List<Object>,Database.AllowCallouts,AccessLevel)",
	}
	rows := make([]SurfaceLedgerRow, 0, len(ids))
	for _, id := range ids {
		rows = append(rows, SurfaceLedgerRow{SurfaceID: id, Product: ProductApex})
	}
	merged := Merge(rows, rows, rows, rows)
	if len(merged.Rows) != 0 {
		t.Fatalf("invalid API67 Database.AllowCallouts rows entered current-base ledger: %#v", merged.Rows)
	}
}

func TestMergeExcludesAPI67InvalidSystemDebugObjectObjectRow(t *testing.T) {
	rows := []SurfaceLedgerRow{{
		SurfaceID: "apex:System.System.debug(Object,Object)",
		Product:   ProductApex,
	}}
	merged := Merge(rows, rows, rows, rows)
	if len(merged.Rows) != 0 {
		t.Fatalf("invalid API67 System.debug(Object,Object) row entered current-base ledger: %#v", merged.Rows)
	}
}

func TestBuildEvidenceSnapshotExcludesAPI67InvalidDatabaseAllowCalloutsRows(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "fixtures", "async-database-allowcallouts-unsupported.json")
	rows, err := BuildEvidenceSnapshot([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if isAPI67RemovedSurfaceID(row.SurfaceID) {
			t.Fatalf("invalid API67 Database.AllowCallouts row entered evidence snapshot: %s", row.SurfaceID)
		}
	}
}
