package surfaceledger

import (
	"slices"
	"testing"
)

func TestVerifyExactLedgerDeltaJSONPreservesStoredAndUnknownRowFields(t *testing.T) {
	base := []byte(`{"schemaVersion":1,"rows":[{"surfaceId":"apex:System.Row","gapClass":"stored-base","futureField":{"value":1}}]}`)
	current := []byte(`{"schemaVersion":1,"rows":[{"futureField":{"value":2},"gapClass":"stored-current","surfaceId":"apex:System.Row"}]}`)
	report, err := VerifyExactLedgerDeltaJSON(base, current, []string{"apex:System.Row"})
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "pass" || report.Counts.Changed != 1 || !slices.Equal(report.ChangedSurfaceIDs, []string{"apex:System.Row"}) {
		t.Fatalf("raw row report = %+v", report)
	}

	semanticallyEqual := []byte(`{"rows":[{"surfaceId":"apex:System.Row","gapClass":"stored-base","futureField":{"value":1}}]}`)
	report, err = VerifyExactLedgerDeltaJSON(base, semanticallyEqual, []string{"apex:System.Row"})
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "fail" || report.Counts.FullChanged != 0 || !slices.Equal(report.MissingExpectedIDs, []string{"apex:System.Row"}) {
		t.Fatalf("semantic row equality report = %+v", report)
	}

	largeBase := []byte(`{"rows":[{"surfaceId":"apex:System.Row","futureNumber":9007199254740992}]}`)
	largeCurrent := []byte(`{"rows":[{"surfaceId":"apex:System.Row","futureNumber":9007199254740993}]}`)
	report, err = VerifyExactLedgerDeltaJSON(largeBase, largeCurrent, []string{"apex:System.Row"})
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "pass" || report.Counts.Changed != 1 {
		t.Fatalf("large-number row report = %+v", report)
	}
}

func TestVerifyExactLedgerDeltaUsesCompleteExactSurfaceIDSet(t *testing.T) {
	base := []SurfaceLedgerRow{
		{SurfaceID: "apex:System.Changed", Evidence: EvidenceNone},
		{SurfaceID: "apex:System.Removed", Evidence: EvidenceFixture},
		{SurfaceID: "apex:System.Unchanged", Evidence: EvidenceFixture},
	}
	current := []SurfaceLedgerRow{
		{SurfaceID: "apex:System.Added", Evidence: EvidenceFixture},
		{SurfaceID: "apex:System.Changed", Evidence: EvidenceFixture},
		{SurfaceID: "apex:System.Unchanged", Evidence: EvidenceFixture},
	}

	report, err := VerifyExactLedgerDelta(base, current, []string{
		"apex:System.Added",
		"apex:System.Changed",
		"apex:System.Removed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "pass" {
		t.Fatalf("status = %q, want pass", report.Status)
	}
	if !slices.Equal(report.ChangedSurfaceIDs, []string{
		"apex:System.Added",
		"apex:System.Changed",
		"apex:System.Removed",
	}) {
		t.Fatalf("changed IDs = %v", report.ChangedSurfaceIDs)
	}
	if report.Counts.Added != 1 || report.Counts.Changed != 1 || report.Counts.Removed != 1 || report.Counts.Unexpected != 0 || report.Counts.MissingExpected != 0 {
		t.Fatalf("counts = %+v", report.Counts)
	}
	if report.UnexpectedSurfaceIDs == nil || report.MissingExpectedIDs == nil {
		t.Fatalf("empty mismatch lists must be stable arrays: unexpected=%v missing=%v", report.UnexpectedSurfaceIDs, report.MissingExpectedIDs)
	}

	projected, err := VerifyExactLedgerDelta(base, current, []string{
		"apex:System.Changed",
		"apex:System.Removed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if projected.Status != "fail" || !slices.Equal(projected.UnexpectedSurfaceIDs, []string{"apex:System.Added"}) {
		t.Fatalf("projected report = %+v", projected)
	}
}

func TestVerifyExactLedgerDeltaRejectsDuplicateExactIDs(t *testing.T) {
	row := SurfaceLedgerRow{SurfaceID: "apex:System.Duplicate"}
	if _, err := VerifyExactLedgerDelta(nil, nil, nil); err == nil {
		t.Fatal("expected empty expected SurfaceID set error")
	}
	if _, err := VerifyExactLedgerDelta([]SurfaceLedgerRow{row, row}, nil, []string{"apex:System.Duplicate"}); err == nil {
		t.Fatal("expected duplicate base SurfaceID error")
	}
	if _, err := VerifyExactLedgerDelta(nil, []SurfaceLedgerRow{row, row}, []string{"apex:System.Duplicate"}); err == nil {
		t.Fatal("expected duplicate current SurfaceID error")
	}
	if _, err := VerifyExactLedgerDelta([]SurfaceLedgerRow{{}}, nil, []string{"apex:System.Expected"}); err == nil {
		t.Fatal("expected empty base SurfaceID error")
	}
	if _, err := VerifyExactLedgerDelta(nil, []SurfaceLedgerRow{{}}, []string{"apex:System.Expected"}); err == nil {
		t.Fatal("expected empty current SurfaceID error")
	}
	if _, err := VerifyExactLedgerDelta(nil, nil, []string{"apex:System.Duplicate", "apex:System.Duplicate"}); err == nil {
		t.Fatal("expected duplicate expected SurfaceID error")
	}
	if _, err := VerifyExactLedgerDelta(nil, nil, []string{""}); err == nil {
		t.Fatal("expected empty expected SurfaceID error")
	}
}

func TestVerifyExactLedgerDeltaIsCaseSensitive(t *testing.T) {
	report, err := VerifyExactLedgerDelta(
		[]SurfaceLedgerRow{{SurfaceID: "apex:System.Case"}},
		[]SurfaceLedgerRow{{SurfaceID: "apex:System.case"}},
		[]string{"apex:System.Case", "apex:System.case"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "pass" || report.Counts.Added != 1 || report.Counts.Removed != 1 || report.Counts.FullChanged != 2 {
		t.Fatalf("case-sensitive report = %+v", report)
	}
}
