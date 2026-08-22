package corpusassurance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExceptionObjectMethodFixturesSeparateHostedAndLocalRows(t *testing.T) {
	root := filepath.Join("..", "..", "docs", "fixtures")
	tests := []struct {
		filename    string
		typeName    string
		otherType   string
		eligible    bool
		localReason bool
	}{
		{"core-exception-object-methods-runtime.json", "NoDataFoundException", "NoAccessException", false, true},
		{"core-noaccess-exception-object-methods-local-runtime.json", "NoAccessException", "NoDataFoundException", false, true},
	}
	for _, test := range tests {
		t.Run(test.filename, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(root, test.filename))
			if err != nil {
				t.Fatal(err)
			}
			fixture, metadata, err := decodeLocalProofFixtureWithMetadata(data)
			if err != nil {
				t.Fatal(err)
			}
			if len(fixture.Evidence) != 4 {
				t.Fatalf("evidence rows = %d, want 4", len(fixture.Evidence))
			}
			want := map[string]bool{
				"apex:System." + test.typeName:                     true,
				"apex:System." + test.typeName + ".equals(Object)": true,
				"apex:System." + test.typeName + ".hashCode()":     true,
				"apex:System." + test.typeName + ".toString()":     true,
			}
			for _, evidence := range fixture.Evidence {
				if !want[evidence.SurfaceID] {
					t.Fatalf("unexpected evidence row %q", evidence.SurfaceID)
				}
				delete(want, evidence.SurfaceID)
			}
			if len(want) != 0 {
				t.Fatalf("missing evidence rows: %v", want)
			}
			if len(fixture.Source) != 1 || strings.Contains(fixture.Source[0].Content, test.otherType) {
				t.Fatalf("source mixes exception families: %#v", fixture.Source)
			}
			if len(fixture.Command.Args) != 1 || fixture.Source[0].Content != fixture.Command.Args[0] {
				t.Fatal("source content and command argument differ")
			}
			if metadata.Eligible == nil || *metadata.Eligible != test.eligible {
				t.Fatalf("salesforceEligible = %v, want %t", metadata.Eligible, test.eligible)
			}
			if test.localReason && (metadata.ExclusionClass != "policy-local-only" || !strings.Contains(metadata.ExclusionReason, "anonymous Apex") || !strings.Contains(metadata.ExclusionReason, "no hosted parity")) {
				t.Fatalf("local Salesforce policy = %#v", metadata)
			}
		})
	}
}
