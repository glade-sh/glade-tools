package main

import (
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/releasecontract"
	"github.com/glade-sh/glade/tools/internal/surfaceledger"
)

func TestReviewRequiresExactCoverageAndEvidence(t *testing.T) {
	changes := []releasecontract.SurfaceChange{{Entry: surfaceledger.DeltaEntry{SurfaceID: "apex:System.Example.call()"}}}
	good := decision{SurfaceID: "apex:System.Example.call()", Scope: surfaceledger.ScopeT0, Disposition: surfaceledger.DispoExistingCase, Declaration: "public static void call()", SourceURL: "https://developer.salesforce.com/docs/atlas.en-us.264.0.apexref.meta/apexref/example.htm", ProductEvidence: "internal/vm/example.go implements this declaration", Reason: "Implemented; runtime result is covered by the bound test.", ProductTests: []string{"internal/vm/example_test.go:TestExample"}}
	for _, tc := range []struct {
		name      string
		decisions []decision
		want      string
	}{
		{"missing", nil, "missing review"},
		{"duplicate", []decision{good, good}, "duplicate"},
		{"wildcard", []decision{{SurfaceID: "apex:System.*"}}, "stale review"},
		{"valid", []decision{good}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := classify(changes, tc.decisions)
			if tc.want != "" {
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("got %v, want %s", err, tc.want)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(got[0].ReasonRef, good.SourceURL) || !strings.Contains(got[0].ReasonRef, good.ProductEvidence) {
				t.Fatal("lost per-item evidence")
			}
		})
	}
	for _, field := range []string{"declaration", "source", "product", "reason", "tests"} {
		t.Run(field, func(t *testing.T) {
			d := good
			switch field {
			case "declaration":
				d.Declaration = ""
			case "source":
				d.SourceURL = "https://example.invalid/doc"
			case "product":
				d.ProductEvidence = ""
			case "reason":
				d.Reason = ""
			case "tests":
				d.ProductTests = nil
			}
			if _, err := classify(changes, []decision{d}); err == nil {
				t.Fatal("accepted incomplete evidence")
			}
		})
	}
}
