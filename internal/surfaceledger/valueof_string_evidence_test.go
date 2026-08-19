package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func TestValueOfStringEvidenceHasUniqueExecutableOwners(t *testing.T) {
	root, owner := filepath.Join("..", "..", "docs", "fixtures"), "core-valueof-string-local"
	want := map[string][2]string{
		"apex:System.Integer.valueOf(String)": {"Integer.valueOf('42')", "System.assertEquals(42, integerValue);"},
		"apex:System.Long.valueOf(String)":    {"Long.valueOf('9001')", "System.assertEquals(9001, longValue);"},
		"apex:System.Double.valueOf(String)":  {"Double.valueOf('2.25')", "System.assertEquals(2.25, doubleValue);"},
		"apex:System.Id.valueOf(String)":      {"Id.valueOf('001B000001DVM9t')", "System.assertEquals('001B000001DVM9t', recordId.toString());"},
	}
	paths, err := filepath.Glob(filepath.Join(root, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	owners := map[string][]string{}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			EvidenceOnly bool   `json:"evidenceOnly"`
			Name         string `json:"name"`
			Evidence     []struct {
				SurfaceID string `json:"surfaceId"`
				Kind      string `json:"kind"`
			} `json:"evidence"`
			Source []struct {
				Content string `json:"content"`
			} `json:"source"`
			Command struct {
				Kind string   `json:"kind"`
				Args []string `json:"args"`
			} `json:"command"`
		}
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatal(err)
		}
		if doc.EvidenceOnly || doc.Command.Kind != "exec" || len(doc.Source) != 1 || len(doc.Command.Args) != 1 || doc.Source[0].Content != doc.Command.Args[0] {
			continue
		}
		for _, evidence := range doc.Evidence {
			if _, ok := want[evidence.SurfaceID]; ok && evidence.Kind == "exec" {
				owners[evidence.SurfaceID] = append(owners[evidence.SurfaceID], doc.Name)
			}
		}
	}
	for id := range want {
		if len(owners[id]) != 1 || owners[id][0] != owner {
			t.Fatalf("%s runnable owners = %#v, want exactly %s", id, owners[id], owner)
		}
	}

	ownerPath := filepath.Join(root, owner+".json")
	fixture, err := compat.LoadFile(ownerPath)
	if err != nil {
		t.Fatal(err)
	}
	if fixture.Name != owner || fixture.Command.Kind != "exec" || len(fixture.Source) != 1 || len(fixture.Command.Args) != 1 || fixture.Source[0].Content != fixture.Command.Args[0] {
		t.Fatalf("owner execution envelope = %#v", fixture)
	}
	if len(fixture.Evidence) != len(want) {
		t.Fatalf("owner evidence rows = %d, want exactly %d", len(fixture.Evidence), len(want))
	}
	result, err := compat.Run(fixture)
	if err != nil || !result.OK {
		t.Fatalf("run %s = %#v, %v", fixture.Name, result, err)
	}
	seen, source := map[string]bool{}, fixture.Source[0].Content
	for _, evidence := range fixture.Evidence {
		expected, ok := want[evidence.SurfaceID]
		if !ok || seen[evidence.SurfaceID] || evidence.Kind != "exec" || !strings.Contains(source, expected[0]) || !strings.Contains(source, expected[1]) {
			t.Fatalf("invalid owner evidence row = %#v", evidence)
		}
		seen[evidence.SurfaceID] = true
	}
	rows, err := BuildEvidenceSnapshot([]string{ownerPath})
	if err != nil {
		t.Fatal(err)
	}
	for id := range want {
		row, ok := rowsByID(rows)[id]
		if !seen[id] || !ok || row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported {
			t.Fatalf("%s missing fixture/supported evidence", id)
		}
	}
}
