package surfaceledger

import "testing"

func TestComputeDeltaPreflightMergesRowsAndRemovesTombstones(t *testing.T) {
	base := []SurfaceLedgerRow{
		{SurfaceID: "apex:System.Base", Product: ProductApex, Kind: KindType, GladeShape: ShapeTypeKnown, GladeBehavior: BehaviorSupported, Evidence: EvidenceFixture},
		{SurfaceID: "apex:System.ToRemove", Product: ProductApex, Kind: KindType, GladeShape: ShapeTypeKnown, GladeBehavior: BehaviorSupported, Evidence: EvidenceFixture},
	}
	additions := []SurfaceLedgerRow{
		{SurfaceID: "apex:System.Base", Product: ProductApex, Kind: KindType, GladeShape: ShapeSignatureKnown, GladeBehavior: BehaviorSupported, Evidence: EvidenceFixture},
		{SurfaceID: "apex:System.Added", Product: ProductApex, Kind: KindType, GladeShape: ShapeTypeKnown, GladeBehavior: BehaviorSupported, Evidence: EvidenceFixture},
	}
	ledger, result, err := ComputeDeltaPreflight(base, additions, nil, []string{"apex:System.ToRemove"}, nil)
	if err != nil {
		t.Fatalf("ComputeDeltaPreflight: %v", err)
	}
	if len(ledger.Rows) != 2 {
		t.Fatalf("result rows = %d, want 2", len(ledger.Rows))
	}
	if got, want := result.AddedIDs, []string{"apex:System.Added"}; !equalStrings(got, want) {
		t.Fatalf("added IDs = %v, want %v", got, want)
	}
	if got, want := result.RemovedIDs, []string{"apex:System.ToRemove"}; !equalStrings(got, want) {
		t.Fatalf("removed IDs = %v, want %v", got, want)
	}
	if got, want := result.ChangedIDs, []string{"apex:System.Base"}; !equalStrings(got, want) {
		t.Fatalf("changed IDs = %v, want %v", got, want)
	}
	if result.Summary.Total != 2 || ledger.Summary.Total != 2 {
		t.Fatalf("summary total = result %d ledger %d, want 2", result.Summary.Total, ledger.Summary.Total)
	}
}

func TestComputeDeltaPreflightTombstonesWinOverAdditions(t *testing.T) {
	additions := []SurfaceLedgerRow{{
		SurfaceID:     "apex:System.Invalid",
		Product:       ProductApex,
		Kind:          KindType,
		GladeShape:    ShapeTypeKnown,
		GladeBehavior: BehaviorSupported,
		Evidence:      EvidenceFixture,
	}}
	ledger, result, err := ComputeDeltaPreflight(nil, additions, nil, []string{"apex:System.Invalid"}, nil)
	if err != nil {
		t.Fatalf("ComputeDeltaPreflight: %v", err)
	}
	if len(ledger.Rows) != 0 {
		t.Fatalf("tombstoned addition remained: %#v", ledger.Rows)
	}
	if len(result.AddedIDs) != 0 || len(result.RemovedIDs) != 0 {
		t.Fatalf("tombstoned addition reported as delta: added=%v removed=%v", result.AddedIDs, result.RemovedIDs)
	}
	if got, want := result.TombstoneIDs, []string{"apex:System.Invalid"}; !equalStrings(got, want) {
		t.Fatalf("tombstone IDs = %v, want %v", got, want)
	}
}

func TestComputeDeltaPreflightReportsNonDeferredGapIDs(t *testing.T) {
	base := []SurfaceLedgerRow{{
		SurfaceID:     "apex:System.Open",
		Product:       ProductApex,
		Namespace:     "System",
		TypeName:      "Open",
		Kind:          KindType,
		GladeShape:    ShapeAbsent,
		GladeBehavior: BehaviorNone,
		Evidence:      EvidenceNone,
	}}
	policy := SupportPolicy{Rules: []SupportPolicyRule{{
		Namespace:   "System",
		Disposition: DispositionLocalRuntimeRequired,
		Reason:      "local test",
	}}}
	_, result, err := ComputeDeltaPreflight(base, nil, nil, nil, &policy)
	if err != nil {
		t.Fatalf("ComputeDeltaPreflight: %v", err)
	}
	if got, want := result.NonDeferredIDs, []string{"apex:System.Open"}; !equalStrings(got, want) {
		t.Fatalf("non-deferred IDs = %v, want %v", got, want)
	}
}

func TestComputeDeltaPreflightRejectsEmptyRemovalID(t *testing.T) {
	_, _, err := ComputeDeltaPreflight(nil, nil, []string{""}, nil, nil)
	if err == nil {
		t.Fatal("empty removal ID unexpectedly accepted")
	}
}
