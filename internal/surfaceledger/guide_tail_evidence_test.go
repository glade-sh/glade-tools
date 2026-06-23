package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func TestGuideTailUnsupportedFixturesCoverEveryEvidenceRow(t *testing.T) {
	tests := []struct {
		path  string
		name  string
		count int
	}{
		{
			path:  filepath.Join("..", "..", "docs", "fixtures", "sourcefamily-apex-guide-tail-unsupported.json"),
			name:  "sourcefamily-apex-guide-tail-unsupported",
			count: 535,
		},
		{
			path:  filepath.Join("..", "..", "docs", "fixtures", "platform-events-guide-tail-unsupported.json"),
			name:  "platform-events-guide-tail-unsupported",
			count: 142,
		},
		{
			path:  filepath.Join("..", "..", "docs", "fixtures", "sourcefamily-site-references-tail-unsupported.json"),
			name:  "sourcefamily-site-references-tail-unsupported",
			count: 52,
		},
		{
			path:  filepath.Join("..", "..", "docs", "fixtures", "sourcefamily-limits-reference-tail-unsupported.json"),
			name:  "sourcefamily-limits-reference-tail-unsupported",
			count: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := loadGuideTailFixture(t, tt.path)
			if fixture.Name != tt.name {
				t.Fatalf("fixture name = %q, want %q", fixture.Name, tt.name)
			}
			if len(fixture.Evidence) != tt.count {
				t.Fatalf("evidence rows = %d, want %d", len(fixture.Evidence), tt.count)
			}
			seen := map[string]bool{}
			for _, item := range fixture.Evidence {
				if item.SurfaceID == "" {
					t.Fatalf("fixture %s has evidence without surfaceId: %#v", fixture.Name, item)
				}
				if seen[item.SurfaceID] {
					t.Fatalf("duplicate surfaceId %s", item.SurfaceID)
				}
				seen[item.SurfaceID] = true
				if item.Kind != "unsupported" {
					t.Fatalf("%s kind = %q, want unsupported", item.SurfaceID, item.Kind)
				}
				assertNoUnicodeFormatMarksInGuideTail(t, item.SurfaceID)
			}

			rows, err := BuildEvidenceSnapshot([]string{tt.path})
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != tt.count {
				t.Fatalf("snapshot rows = %d, want %d", len(rows), tt.count)
			}
			for _, row := range rows {
				if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorUnsupported {
					t.Fatalf("%s evidence/behavior = %s/%s, want fixture/unsupported", row.SurfaceID, row.Evidence, row.GladeBehavior)
				}
			}
		})
	}
}

func TestPlatformEventsGuideTailFixtureDoesNotMarkLocalApexEventRowsUnsupported(t *testing.T) {
	fixture := loadGuideTailFixture(t, filepath.Join("..", "..", "docs", "fixtures", "platform-events-guide-tail-unsupported.json"))
	for _, item := range fixture.Evidence {
		id := strings.ToLower(item.SurfaceID)
		switch {
		case strings.HasPrefix(id, "apex:system.eventbus."):
			t.Fatalf("local EventBus row must not be explicit unsupported evidence: %s", item.SurfaceID)
		case strings.HasPrefix(id, "apex:system.test.geteventbus"):
			t.Fatalf("local Test.getEventBus row must not be explicit unsupported evidence: %s", item.SurfaceID)
		case strings.Contains(id, ".trigger"):
			t.Fatalf("local trigger row must not be explicit unsupported evidence: %s", item.SurfaceID)
		}
	}
}

func loadGuideTailFixture(t *testing.T, path string) compat.Fixture {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fixture compat.Fixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func assertNoUnicodeFormatMarksInGuideTail(t *testing.T, id string) {
	t.Helper()
	for len(id) > 0 {
		r, size := utf8.DecodeRuneInString(id)
		if r == utf8.RuneError && size == 1 {
			t.Fatalf("surfaceId contains invalid UTF-8")
		}
		if unicode.Is(unicode.Cf, r) {
			t.Fatalf("surfaceId contains Unicode format mark U+%04X", r)
		}
		id = id[size:]
	}
}
