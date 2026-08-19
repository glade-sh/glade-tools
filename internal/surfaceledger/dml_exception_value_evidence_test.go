package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func TestDmlExceptionValueEvidenceHasUniqueExecutableOwners(t *testing.T) {
	const owner = "dml-exception-value-local-evidence"
	want := map[string]string{
		"apex:System.DmlException.equals(Object)": "same.equals(other)",
		"apex:System.DmlException.hashCode()":     "same.hashCode()",
		"apex:System.DmlException.toString()":     "same.toString()",
	}
	paths, err := filepath.Glob(filepath.Join("..", "..", "docs", "fixtures", "*.json"))
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

	ownerPath := filepath.Join("..", "..", "docs", "fixtures", owner+".json")
	fixture, err := compat.LoadFile(ownerPath)
	if err != nil {
		t.Fatal(err)
	}
	if fixture.Name != owner || fixture.Command.Kind != "exec" || len(fixture.Source) != 1 || len(fixture.Command.Args) != 1 || fixture.Source[0].Content != fixture.Command.Args[0] {
		t.Fatalf("owner execution envelope = %#v", fixture)
	}
	data, err := os.ReadFile(ownerPath)
	if err != nil {
		t.Fatal(err)
	}
	var policy struct {
		Mode     string `json:"mode"`
		Eligible *bool  `json:"salesforceEligible"`
		Class    string `json:"salesforceExclusionClass"`
		Reason   string `json:"salesforceExclusionReason"`
	}
	if err := json.Unmarshal(data, &policy); err != nil {
		t.Fatal(err)
	}
	if policy.Mode != "local-runtime" || policy.Eligible == nil || *policy.Eligible || policy.Class != "policy-local-only" || !strings.Contains(policy.Reason, "zero Salesforce parity") {
		t.Fatalf("fixture policy = %#v, want local-runtime/false/local-only/zero-parity", policy)
	}
	if len(fixture.Evidence) != len(want) {
		t.Fatalf("owner evidence rows = %d, want exactly %d", len(fixture.Evidence), len(want))
	}
	for _, evidence := range fixture.Evidence {
		if expected, ok := want[evidence.SurfaceID]; !ok || evidence.Kind != "exec" || !strings.Contains(fixture.Source[0].Content, expected) {
			t.Fatalf("invalid owner evidence row = %#v", evidence)
		}
	}
	result, err := compat.Run(fixture)
	if err != nil || !result.OK {
		t.Fatalf("run %s = %#v, %v", fixture.Name, result, err)
	}
}
