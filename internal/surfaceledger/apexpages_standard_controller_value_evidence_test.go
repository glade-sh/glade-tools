package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func TestApexPagesStandardControllerValueEvidenceHasUniqueExecutableOwner(t *testing.T) {
	const owner = "current-base-apexpages-local-runtime-001-api67"
	want := map[string]bool{
		"apex:ApexPages.StandardController.equals(Object)": true,
		"apex:ApexPages.StandardController.hashCode()":     true,
		"apex:ApexPages.StandardController.toString()":     true,
	}
	root := filepath.Join("..", "..")
	paths, err := filepath.Glob(filepath.Join(root, "docs", "fixtures", "*.json"))
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
				t.Fatalf("%s owner row = %#v", id, row)
			}
		}
		if count != 1 {
			t.Fatalf("%s fixture rows = %d, want exactly one", id, count)
		}
	}

	path := filepath.Join(root, "docs", "fixtures", owner+".json")
	fixture, err := compat.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if fixture.Command.Kind != "exec" || len(fixture.Command.Args) != 1 || len(fixture.Source) != 1 || fixture.Source[0].Content != fixture.Command.Args[0] {
		t.Fatalf("execution envelope = %#v", fixture)
	}
	data, err := os.ReadFile(path)
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
		t.Fatalf("fixture policy = %#v", policy)
	}
	source := fixture.Source[0].Content
	for _, witness := range []string{
		"ApexPages.StandardController standard = new ApexPages.StandardController(account);",
		"System.assertEquals(true, standard.equals(standard));",
		"System.assertNotEquals(0, standard.hashCode());",
		"System.assert(standard.toString().length() > 0);",
	} {
		if !strings.Contains(source, witness) {
			t.Fatalf("source lacks direct %s assertion: %q", witness, source)
		}
	}
	result, err := compat.Run(fixture)
	if err != nil || !result.OK {
		t.Fatalf("run %s = %#v, %v", fixture.Name, result, err)
	}
}
