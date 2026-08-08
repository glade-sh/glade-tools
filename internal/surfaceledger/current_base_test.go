package surfaceledger

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestComputeStrictCurrentBase(t *testing.T) {
	cases := []struct {
		name       string
		row        SurfaceLedgerRow
		wantClosed bool
		wantReason string
	}{
		{
			name: "supported and evidenced closes",
			row: func() SurfaceLedgerRow {
				r := SurfaceLedgerRow{
					SurfaceID:       "apex:system:Foo.bar",
					Product:         ProductApex,
					Area:            AreaRuntime,
					Kind:            KindMethod,
					TypeName:        "Foo",
					MemberName:      "bar",
					Signature:       "String bar()",
					ReturnType:      "String",
					GladeReturnType: "String",
					GladeBehavior:   BehaviorSupported,
				}
				r = RowFromGladeShape(r)
				r = RowFromEvidence(r)
				return r
			}(),
			wantClosed: true,
			wantReason: "",
		},
		{
			name: "supported but unevidenced stays open",
			row: func() SurfaceLedgerRow {
				r := SurfaceLedgerRow{
					SurfaceID:       "apex:system:Foo.baz",
					Product:         ProductApex,
					Area:            AreaRuntime,
					Kind:            KindMethod,
					TypeName:        "Foo",
					MemberName:      "baz",
					Signature:       "String baz()",
					ReturnType:      "String",
					GladeReturnType: "String",
					GladeBehavior:   BehaviorSupported,
				}
				r = RowFromGladeShape(r)
				return r
			}(),
			wantClosed: false,
			wantReason: "evidence-none",
		},
		{
			name: "bucket implemented without executable evidence stays open",
			row: func() SurfaceLedgerRow {
				r := SurfaceLedgerRow{
					SurfaceID:       "apex:system:Object.clone",
					Product:         ProductApex,
					Area:            AreaRuntime,
					Kind:            KindMethod,
					Namespace:       "System",
					TypeName:        "Object",
					MemberName:      "clone",
					Signature:       "Object clone()",
					ReturnType:      "Object",
					GladeReturnType: "Object",
					GladeBehavior:   BehaviorSupported,
				}
				r = RowFromGladeShape(r)
				return r
			}(),
			wantClosed: false,
			wantReason: "evidence-none",
		},
		{
			name: "passive stays open even with evidence",
			row: func() SurfaceLedgerRow {
				r := SurfaceLedgerRow{
					SurfaceID:     "apex:system:PassiveThing.kind",
					Product:       ProductApex,
					Area:          AreaRuntime,
					Kind:          KindType,
					TypeName:      "PassiveThing",
					GladeBehavior: BehaviorPassive,
				}
				r = RowFromGladeShape(r)
				r = RowFromEvidence(r)
				return r
			}(),
			wantClosed: false,
			wantReason: "behavior-passive",
		},
		{
			name: "unsupported stays open even with evidence",
			row: func() SurfaceLedgerRow {
				r := SurfaceLedgerRow{
					SurfaceID:     "apex:system:UnsupportedThing.kind",
					Product:       ProductApex,
					Area:          AreaRuntime,
					Kind:          KindType,
					TypeName:      "UnsupportedThing",
					GladeBehavior: BehaviorUnsupported,
				}
				r = RowFromGladeShape(r)
				r = RowFromEvidence(r)
				return r
			}(),
			wantClosed: false,
			wantReason: "behavior-unsupported",
		},
		{
			name: "stub no-op stays open",
			row: func() SurfaceLedgerRow {
				r := SurfaceLedgerRow{
					SurfaceID:     "apex:system:StubThing.kind",
					Product:       ProductApex,
					Area:          AreaRuntime,
					Kind:          KindType,
					TypeName:      "StubThing",
					GladeBehavior: BehaviorStubNoOp,
				}
				r = RowFromGladeShape(r)
				r = RowFromEvidence(r)
				return r
			}(),
			wantClosed: false,
			wantReason: "behavior-stub-noop",
		},
		{
			name: "missing shape stays open",
			row: func() SurfaceLedgerRow {
				r := SurfaceLedgerRow{
					SurfaceID: "apex:system:OnlyDocs.kind",
					Product:   ProductApex,
					Area:      AreaRuntime,
					Kind:      KindType,
					TypeName:  "OnlyDocs",
				}
				r = RowFromDocs(r)
				return r
			}(),
			wantClosed: false,
			wantReason: "missing-shape",
		},
		{
			name: "return type mismatch stays open",
			row: func() SurfaceLedgerRow {
				r := SurfaceLedgerRow{
					SurfaceID:       "apex:system:Mismatch.kind",
					Product:         ProductApex,
					Area:            AreaRuntime,
					Kind:            KindMethod,
					TypeName:        "Mismatch",
					MemberName:      "kind",
					Signature:       "String kind()",
					ReturnType:      "String",
					GladeReturnType: "String",
					DocsReturnType:  "Integer",
					GladeBehavior:   BehaviorSupported,
				}
				r = RowFromDocs(r)
				r = RowFromGladeShape(r)
				r = RowFromEvidence(r)
				return r
			}(),
			wantClosed: false,
			wantReason: "bucket-failure",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ComputeStrictCurrentBase([]SurfaceLedgerRow{tc.row})
			if tc.wantClosed {
				if got.StrictClosed != 1 || got.StrictOpen != 0 {
					t.Fatalf("expected strict-closed, got closed=%d open=%d reasons=%v",
						got.StrictClosed, got.StrictOpen, got.OpenRows)
				}
				if len(got.OpenRows) != 0 {
					t.Fatalf("expected no open rows, got %#v", got.OpenRows)
				}
				return
			}
			if got.StrictOpen != 1 || len(got.OpenRows) != 1 {
				t.Fatalf("expected one open row, got closed=%d open=%d rows=%#v",
					got.StrictClosed, got.StrictOpen, got.OpenRows)
			}
			open := got.OpenRows[0]
			if len(open.Reasons) == 0 {
				t.Fatalf("expected reasons, got none for %s", open.SurfaceID)
			}
			if open.Reasons[0] != tc.wantReason {
				t.Fatalf("primary reason: want %q got %q (all=%v)", tc.wantReason, open.Reasons[0], open.Reasons)
			}
		})
	}
}

func TestComputeStrictCurrentBaseCounts(t *testing.T) {
	supported := func(id string) SurfaceLedgerRow {
		r := SurfaceLedgerRow{
			SurfaceID:       id,
			Product:         ProductApex,
			Area:            AreaRuntime,
			Kind:            KindMethod,
			TypeName:        "Thing",
			MemberName:      "doIt",
			Signature:       "String doIt()",
			ReturnType:      "String",
			GladeReturnType: "String",
			GladeBehavior:   BehaviorSupported,
		}
		r = RowFromGladeShape(r)
		r = RowFromEvidence(r)
		return r
	}
	unevidenced := supported("apex:system:Thing.unevidenced")
	unevidenced.SurfaceID = "apex:system:Thing.unevidenced"
	unevidenced.Evidence = EvidenceNone
	unevidenced.GapClass = ""
	unevidenced.Bucket = ""

	passive := func(id string) SurfaceLedgerRow {
		r := SurfaceLedgerRow{
			SurfaceID:     id,
			Product:       ProductApex,
			Area:          AreaRuntime,
			Kind:          KindType,
			TypeName:      "Passive",
			GladeBehavior: BehaviorPassive,
		}
		r = RowFromGladeShape(r)
		r = RowFromEvidence(r)
		return r
	}

	rows := []SurfaceLedgerRow{
		supported("apex:system:Thing.doIt"),
		unevidenced,
		passive("apex:system:Passive.kind"),
	}

	got := ComputeStrictCurrentBase(rows)

	if got.Total != 3 {
		t.Fatalf("total: want 3 got %d", got.Total)
	}
	if got.ShapePresent != 3 {
		t.Fatalf("shapePresent: want 3 got %d", got.ShapePresent)
	}
	if got.BehaviorClaimed != 3 {
		t.Fatalf("behaviorClaimed: want 3 got %d", got.BehaviorClaimed)
	}
	if got.EvidenceBacked != 2 {
		t.Fatalf("evidenceBacked: want 2 got %d", got.EvidenceBacked)
	}
	if got.StrictClosed != 1 {
		t.Fatalf("strictClosed: want 1 got %d", got.StrictClosed)
	}
	if got.StrictOpen != 2 {
		t.Fatalf("strictOpen: want 2 got %d", got.StrictOpen)
	}
	if len(got.OpenRows) != 2 {
		t.Fatalf("openRows: want 2 got %d", len(got.OpenRows))
	}
	if got.OpenRows[0].SurfaceID != "apex:system:Passive.kind" {
		t.Fatalf("first open row: want apex:system:Passive.kind got %q", got.OpenRows[0].SurfaceID)
	}
	if got.OpenRows[0].Reasons[0] != "behavior-passive" {
		t.Fatalf("first open reason: want behavior-passive got %q", got.OpenRows[0].Reasons[0])
	}
	if got.OpenRows[1].SurfaceID != "apex:system:Thing.unevidenced" {
		t.Fatalf("second open row: want apex:system:Thing.unevidenced got %q", got.OpenRows[1].SurfaceID)
	}
	if got.OpenRows[1].Reasons[0] != "evidence-none" {
		t.Fatalf("second open reason: want evidence-none got %q", got.OpenRows[1].Reasons[0])
	}
}

func TestComputeStrictCurrentBaseDocsOnlyEvidenceIsInsufficient(t *testing.T) {
	r := SurfaceLedgerRow{
		SurfaceID:       "apex:system:DocsBacked.kind",
		Product:         ProductApex,
		Area:            AreaRuntime,
		Kind:            KindMethod,
		TypeName:        "DocsBacked",
		MemberName:      "kind",
		Signature:       "String kind()",
		ReturnType:      "String",
		GladeReturnType: "String",
		GladeBehavior:   BehaviorSupported,
		Evidence:        EvidenceDocs,
	}
	r = RowFromGladeShape(r)

	got := ComputeStrictCurrentBase([]SurfaceLedgerRow{r})
	if got.StrictClosed != 0 || got.StrictOpen != 1 {
		t.Fatalf("docs-only evidence must not close: closed=%d open=%d", got.StrictClosed, got.StrictOpen)
	}
	if got.OpenRows[0].Reasons[0] != "evidence-docs" {
		t.Fatalf("primary reason: want evidence-docs got %q", got.OpenRows[0].Reasons[0])
	}
	if got.EvidenceBacked != 0 {
		t.Fatalf("docs evidence must not count as evidence-backed: got %d", got.EvidenceBacked)
	}
}

func TestComputeStrictCurrentBaseBucketFailureStaysOpen(t *testing.T) {
	r := SurfaceLedgerRow{
		SurfaceID:       "apex:system:FailureThing.kind",
		Product:         ProductREST,
		Area:            AreaRuntime,
		Kind:            KindMethod,
		TypeName:        "FailureThing",
		MemberName:      "kind",
		Signature:       "String kind()",
		ReturnType:      "String",
		GladeReturnType: "String",
		DocsReturnType:  "String",
		GladeBehavior:   BehaviorSupported,
		GladeShape:      ShapeSignatureKnown,
		Evidence:        EvidenceFixture,
	}
	r = RowFromGladeShape(r)

	got := ComputeStrictCurrentBase([]SurfaceLedgerRow{r})
	if got.StrictClosed != 0 {
		t.Fatalf("bucket-failure row must not close: strictClosed=%d", got.StrictClosed)
	}
	if got.StrictOpen != 1 || len(got.OpenRows) != 1 {
		t.Fatalf("expected one open row, got open=%d rows=%d", got.StrictOpen, len(got.OpenRows))
	}
	open := got.OpenRows[0]
	if len(open.Reasons) == 0 {
		t.Fatalf("expected reasons, got none for %s", open.SurfaceID)
	}
	if open.Reasons[0] != "bucket-failure" {
		t.Fatalf("primary reason: want bucket-failure got %q (all=%v)", open.Reasons[0], open.Reasons)
	}
	if got.EvidenceBacked != 1 {
		t.Fatalf("fixture-evidenced row should count as evidence-backed: got %d", got.EvidenceBacked)
	}
}

func TestWriteStrictCurrentBaseJSONRoundTrips(t *testing.T) {
	supported := func(id string) SurfaceLedgerRow {
		r := SurfaceLedgerRow{
			SurfaceID:       id,
			Product:         ProductApex,
			Area:            AreaRuntime,
			Kind:            KindMethod,
			TypeName:        "Thing",
			MemberName:      "doIt",
			Signature:       "String doIt()",
			ReturnType:      "String",
			GladeReturnType: "String",
			GladeBehavior:   BehaviorSupported,
		}
		r = RowFromGladeShape(r)
		r = RowFromEvidence(r)
		return r
	}
	base := ComputeStrictCurrentBase([]SurfaceLedgerRow{
		supported("apex:system:Thing.doIt"),
	})
	if base.StrictClosed != 1 {
		t.Fatalf("setup closed=%d, want 1", base.StrictClosed)
	}

	var buf bytes.Buffer
	if err := WriteStrictCurrentBaseJSON(&buf, base); err != nil {
		t.Fatalf("write: %v", err)
	}
	out := buf.String()
	if !strings.HasSuffix(out, "\n") {
		t.Fatalf("writer must end with newline, got %q", out)
	}
	if !strings.Contains(out, "  ") {
		t.Fatalf("writer must be indented, got %q", out)
	}

	var decoded StrictCurrentBase
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("decode: %v\n%s", err, out)
	}
	if decoded.Total != base.Total || decoded.StrictClosed != base.StrictClosed || decoded.StrictOpen != base.StrictOpen {
		t.Fatalf("round-trip mismatch: decoded=%#v base=%#v", decoded, base)
	}
	if len(decoded.OpenRows) != len(base.OpenRows) {
		t.Fatalf("openRows mismatch: decoded=%d base=%d", len(decoded.OpenRows), len(base.OpenRows))
	}
}
