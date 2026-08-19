package surfaceledger

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func TestValueOfStringEvidenceHasUniqueExecutableOwners(t *testing.T) {
	root := filepath.Join("..", "..", "docs", "fixtures")
	paths := []string{
		filepath.Join(root, "core-numeric-stdlib.json"),
		filepath.Join(root, "core-type-id-url-stdlib.json"),
	}
	want := map[string]struct {
		fixture string
		call    string
		assert  string
	}{
		"apex:System.Integer.valueOf(String)": {"core-numeric-stdlib", "Integer.valueOf('42')", "System.assertEquals(42, i);"},
		"apex:System.Long.valueOf(String)":    {"core-numeric-stdlib", "Long.valueOf('9001')", "System.assertEquals(9001, l);"},
		"apex:System.Double.valueOf(String)":  {"core-numeric-stdlib", "Double.valueOf('2.25')", "System.assertEquals(2.25, x);"},
		"apex:System.Id.valueOf(String)":      {"core-type-id-url-stdlib", "Id.valueOf('001B000001DVM9t')", "System.assert(valid.equals(same));"},
	}
	owners := map[string]string{}
	for _, path := range paths {
		fixture, err := compat.LoadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if fixture.Command.Kind != "exec" || len(fixture.Source) != 1 {
			t.Fatalf("%s execution envelope = kind:%q source:%d", fixture.Name, fixture.Command.Kind, len(fixture.Source))
		}
		source := fixture.Source[0].Content
		for _, evidence := range fixture.Evidence {
			expected, ok := want[evidence.SurfaceID]
			if !ok {
				continue
			}
			if previous, exists := owners[evidence.SurfaceID]; exists {
				t.Fatalf("duplicate executable fixture owner for %s: %s and %s", evidence.SurfaceID, previous, fixture.Name)
			}
			if fixture.Name != expected.fixture || evidence.Kind != "exec" {
				t.Fatalf("%s owner = fixture:%s kind:%s", evidence.SurfaceID, fixture.Name, evidence.Kind)
			}
			if !strings.Contains(source, expected.call) || !strings.Contains(source, expected.assert) {
				t.Fatalf("%s lacks direct source witness call/assertion", evidence.SurfaceID)
			}
			owners[evidence.SurfaceID] = fixture.Name
		}
	}
	if len(owners) != len(want) {
		t.Fatalf("owned valueOf(String) IDs = %d, want %d", len(owners), len(want))
	}

	rows, err := BuildEvidenceSnapshot(paths)
	if err != nil {
		t.Fatal(err)
	}
	for id := range want {
		row, ok := rowsByID(rows)[id]
		if !ok {
			t.Fatalf("missing evidence snapshot row %s", id)
		}
		if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported {
			t.Fatalf("%s evidence/behavior = %s/%s, want fixture/supported", id, row.Evidence, row.GladeBehavior)
		}
	}
}
