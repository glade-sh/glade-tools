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
	evidencePaths := make([]string, 0, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var header struct {
			EvidenceOnly bool `json:"evidenceOnly"`
		}
		if err := json.Unmarshal(data, &header); err != nil {
			t.Fatal(err)
		}
		if !header.EvidenceOnly {
			evidencePaths = append(evidencePaths, path)
		}
	}
	rows, err := BuildEvidenceSnapshot(evidencePaths)
	if err != nil {
		t.Fatal(err)
	}
	for id := range want {
		count := 0
		for _, row := range rows {
			if row.SurfaceID != id {
				continue
			}
			count++
			if len(row.Sources) != 1 || row.Sources[0] != "fixture:"+owner || row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported {
				t.Fatalf("%s owner row = %#v, want one fixture:%s supported row", id, row, owner)
			}
		}
		if count != 1 {
			t.Fatalf("%s fixture rows = %d, want exactly one", id, count)
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
	data, err := os.ReadFile(ownerPath)
	if err != nil {
		t.Fatal(err)
	}
	var policy struct {
		Eligible *bool  `json:"salesforceEligible"`
		Class    string `json:"salesforceExclusionClass"`
		Reason   string `json:"salesforceExclusionReason"`
	}
	if err := json.Unmarshal(data, &policy); err != nil {
		t.Fatal(err)
	}
	if policy.Eligible == nil || *policy.Eligible || policy.Class != "policy-local-only" || !strings.Contains(policy.Reason, "zero Salesforce parity") {
		t.Fatalf("fixture policy = %#v, want false/local-only/zero-parity", policy)
	}
	seen, source := map[string]bool{}, fixture.Source[0].Content
	for _, evidence := range fixture.Evidence {
		expected, ok := want[evidence.SurfaceID]
		if !ok || seen[evidence.SurfaceID] || evidence.Kind != "exec" || !strings.Contains(source, expected[0]) || !strings.Contains(source, expected[1]) {
			t.Fatalf("invalid owner evidence row = %#v", evidence)
		}
		seen[evidence.SurfaceID] = true
	}
	if len(seen) != len(want) {
		t.Fatalf("owner IDs = %d, want exactly %d", len(seen), len(want))
	}
	result, err := compat.Run(fixture)
	if err != nil || !result.OK {
		t.Fatalf("run %s = %#v, %v", fixture.Name, result, err)
	}
}
