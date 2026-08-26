package surfaceledger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var g3ResidualGladeSnapshotIDs = []string{
	"apex:System.Set.deepClone()",
}

func TestG3ResidualRowsJoinExactGladeAndFixtureSnapshots(t *testing.T) {
	root := filepath.Join("..", "..")
	mapFixturePath := filepath.Join(root, "docs", "fixtures", "core-collection-stdlib.json")
	mapFixture, err := os.ReadFile(mapFixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(mapFixture), "containsValue") {
		t.Fatalf("%s must not claim unsupported Apex Map.containsValue behavior", mapFixturePath)
	}
	fixturePaths := []string{
		mapFixturePath,
		filepath.Join(root, "docs", "fixtures", "core-collection-small-rows-closeout.json"),
	}
	evidence, err := BuildEvidenceSnapshot(fixturePaths)
	if err != nil {
		t.Fatal(err)
	}
	evidenceCounts := make(map[string]int, len(evidence))
	for _, row := range evidence {
		evidenceCounts[row.SurfaceID]++
	}
	evidenceByID := rowsByID(evidence)
	gladeByID := rowsByID(BuildGladeSnapshot())
	for _, id := range g3ResidualGladeSnapshotIDs {
		if evidenceCounts[id] != 1 {
			t.Errorf("exact fixture evidence count for %s = %d, want 1", id, evidenceCounts[id])
		}
		evidenceRow, ok := evidenceByID[id]
		if !ok {
			t.Errorf("missing exact fixture evidence row %s", id)
		} else if evidenceRow.Evidence != EvidenceFixture || evidenceRow.GladeBehavior != BehaviorSupported {
			t.Errorf("fixture %s evidence/behavior = %s/%s, want fixture/supported", id, evidenceRow.Evidence, evidenceRow.GladeBehavior)
		}

		gladeRow, ok := gladeByID[id]
		if !ok {
			t.Errorf("missing exact Glade snapshot row %s", id)
			continue
		}
		if gladeRow.GladeShape != ShapeSignatureKnown || gladeRow.GladeBehavior != BehaviorSupported {
			t.Errorf("Glade %s shape/behavior = %s/%s, want signature-known/supported", id, gladeRow.GladeShape, gladeRow.GladeBehavior)
		}
	}
	for _, id := range []string{
		"apex:System.Map.containsValue",
		"apex:System.Set.deepClone",
	} {
		if _, ok := evidenceByID[id]; ok {
			t.Errorf("stale bare fixture ID substitutes for exact row: %s", id)
		}
	}
	mapID := "apex:System.Map.containsValue(Object)"
	if _, ok := evidenceByID[mapID]; ok {
		t.Fatalf("unsupported %s has direct fixture evidence", mapID)
	}
	if row := gladeByID[mapID]; row.GladeShape != ShapeSignatureKnown || row.GladeBehavior != BehaviorUnsupported {
		t.Fatalf("Glade %s shape/behavior = %s/%s, want signature-known/unsupported", mapID, row.GladeShape, row.GladeBehavior)
	}
}
