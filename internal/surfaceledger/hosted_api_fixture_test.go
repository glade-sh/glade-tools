package surfaceledger

import (
	"path/filepath"
	"testing"
	"unicode"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func TestHostedAPIFixturesMarkHostedRowsUnsupported(t *testing.T) {
	tests := []struct {
		name      string
		file      string
		wantRows  int
		product   string
		area      string
		localRows []string
	}{
		{
			name:     "SOAP API",
			file:     "hosted-soap-api-unsupported-surfaces.json",
			wantRows: 997,
			product:  ProductSOAPAPI,
			area:     AreaServer,
		},
		{
			name:     "Metadata API",
			file:     "hosted-metadata-api-unsupported-surfaces.json",
			wantRows: 911,
			product:  ProductMetadataAPI,
			area:     AreaServer,
			localRows: []string{
				"apex:Metadata.Operations.retrieve(Metadata.MetadataType,List<String>)",
			},
		},
		{
			name:     "Bulk API",
			file:     "hosted-bulk-api-unsupported-surfaces.json",
			wantRows: 162,
			product:  ProductBulkAPI,
			area:     AreaServer,
		},
		{
			name:     "Streaming API",
			file:     "hosted-streaming-api-unsupported-surfaces.json",
			wantRows: 93,
			product:  ProductStreamingAPI,
			area:     AreaServer,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join("..", "..", "docs", "fixtures", tt.file)
			fixture, err := compat.LoadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if len(fixture.Evidence) != tt.wantRows {
				t.Fatalf("fixture rows = %d, want %d", len(fixture.Evidence), tt.wantRows)
			}
			for _, evidence := range fixture.Evidence {
				assertNoUnicodeFormatMarks(t, evidence.SurfaceID)
			}

			rows, err := BuildEvidenceSnapshot([]string{path})
			if err != nil {
				t.Fatal(err)
			}
			got := rowsByID(rows)
			if len(got) != tt.wantRows {
				t.Fatalf("snapshot rows = %d, want %d", len(got), tt.wantRows)
			}
			for _, evidence := range fixture.Evidence {
				row, ok := got[evidence.SurfaceID]
				if !ok {
					t.Fatalf("missing evidence row %s", evidence.SurfaceID)
				}
				if row.Product != tt.product || row.Area != tt.area || row.Evidence != EvidenceFixture || row.GladeShape != ShapeAbsent || row.GladeBehavior != BehaviorUnsupported {
					t.Fatalf("%s = product:%s area:%s evidence:%s shape:%s behavior:%s, want %s/%s/fixture/absent/unsupported", row.SurfaceID, row.Product, row.Area, row.Evidence, row.GladeShape, row.GladeBehavior, tt.product, tt.area)
				}
			}
			for _, localID := range tt.localRows {
				if _, ok := got[localID]; ok {
					t.Fatalf("local Apex row %s must not be marked hosted unsupported", localID)
				}
			}
		})
	}
}

func assertNoUnicodeFormatMarks(t *testing.T, surfaceID string) {
	t.Helper()
	for _, r := range surfaceID {
		if unicode.Is(unicode.Cf, r) {
			t.Fatalf("surfaceId %q contains hidden Unicode format mark U+%04X", surfaceID, r)
		}
	}
}
