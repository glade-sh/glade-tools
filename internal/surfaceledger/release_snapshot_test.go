package surfaceledger

import (
	"reflect"
	"testing"
)

func TestMergeReleaseSnapshotAppliesOnlyAPI67Threshold(t *testing.T) {
	rows := []SurfaceLedgerRow{
		{SurfaceID: "apex:Database.DeleteFilter", Product: ProductApex, Area: AreaRuntime, Kind: KindType},
		{SurfaceID: "apex:System.Site.getPrefix", Product: ProductApex, Area: AreaRuntime, Kind: KindMethod},
	}
	if got := Merge(rows, nil, nil, nil).Rows; len(got) != 0 {
		t.Fatalf("current merge rows = %#v", got)
	}
	got66, err := MergeReleaseSnapshot(rows, "66.0")
	if err != nil {
		t.Fatal(err)
	}
	if got := got66.Rows; len(got) != 1 || CanonicalSurfaceIDKey(got[0].SurfaceID) != "apex:database.deletefilter" {
		t.Fatalf("66 rows = %#v", got)
	}
	got67, err := MergeReleaseSnapshot(rows, "67.0")
	if err != nil {
		t.Fatal(err)
	}
	if got := got67.Rows; len(got) != 0 {
		t.Fatalf("67 rows = %#v", got)
	}
	gotSite, err := MergeReleaseSnapshot([]SurfaceLedgerRow{{SurfaceID: "apex:System.Site.getPrefix", Product: ProductApex, Area: AreaRuntime, Kind: KindMethod}}, "66.0")
	if err != nil {
		t.Fatal(err)
	}
	if got := gotSite.Rows; len(got) != 0 {
		t.Fatalf("66 site rows = %#v", got)
	}
}

func TestCanonicalSurfaceIDKeyAliasesRemainEquivalent(t *testing.T) {
	for _, ids := range [][]string{{"apex:System.QueryLocator.iterator()", "apex:Database.QueryLocator.iterator()"}, {"apex:System.Schema.SObjectType", "apex:Schema.Schema.SObjectType"}} {
		if !reflect.DeepEqual(CanonicalSurfaceIDKey(ids[0]), CanonicalSurfaceIDKey(ids[1])) {
			t.Fatalf("keys differ: %q %q", ids[0], ids[1])
		}
	}
}

func TestMergeReleaseSnapshotRejectsMalformedAPIVersion(t *testing.T) {
	if _, err := MergeReleaseSnapshot(nil, "66"); err == nil {
		t.Fatal("malformed API unexpectedly accepted")
	}
}
