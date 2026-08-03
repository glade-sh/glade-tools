package surfaceledger

import "testing"

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
