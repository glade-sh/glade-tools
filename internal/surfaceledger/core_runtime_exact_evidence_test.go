package surfaceledger

import (
	"path/filepath"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

var coreRuntimeExactEvidenceFixtures = []string{
	"core-runtime-cb62-apexpages-evidence.json",
	"core-runtime-cb62-messaging-evidence.json",
	"core-runtime-cb62-metadata-evidence.json",
	"core-runtime-cb62-system-core-evidence.json",
	"core-runtime-cb62-system-enums-evidence.json",
	"core-runtime-cb62-system-exceptions-evidence.json",
}

func TestCoreRuntimeExactEvidenceFixtures(t *testing.T) {
	paths := make([]string, 0, len(coreRuntimeExactEvidenceFixtures))
	for _, name := range coreRuntimeExactEvidenceFixtures {
		path := filepath.Join("..", "..", "docs", "fixtures", name)
		paths = append(paths, path)
		fixture, err := compat.LoadFile(path)
		if err != nil {
			t.Fatalf("load %s: %v", name, err)
		}
		result, err := compat.Run(fixture)
		if err != nil {
			t.Fatalf("run %s: %v", name, err)
		}
		if !result.OK {
			t.Fatalf("run %s: %#v", name, result)
		}
	}

	rows, err := BuildEvidenceSnapshot(paths)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 34 {
		t.Fatalf("exact runtime evidence rows = %d, want 34", len(rows))
	}
	seen := make(map[string]bool, len(rows))
	for _, row := range rows {
		if seen[row.SurfaceID] {
			t.Fatalf("duplicate exact runtime evidence row %q", row.SurfaceID)
		}
		seen[row.SurfaceID] = true
		if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported {
			t.Fatalf("row %q = evidence:%s behavior:%s, want fixture/supported", row.SurfaceID, row.Evidence, row.GladeBehavior)
		}
	}
}
