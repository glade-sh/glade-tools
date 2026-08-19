package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func TestApexPagesSeverityMethodEvidenceHasUniqueExecutableOwner(t *testing.T) {
	const owner = "apex-apexpages-severity-local-evidence"
	prior := map[string]bool{
		"apex:ApexPages.Severity.ERROR":   true,
		"apex:ApexPages.Severity.INFO":    true,
		"apex:ApexPages.Severity.WARNING": true,
		"apex:ApexPages.Severity.CONFIRM": true,
		"apex:ApexPages.Severity.FATAL":   true,
	}
	want := map[string][2]string{
		"apex:ApexPages.Severity.equals(Object)":  {"severity.equals(ApexPages.Severity.ERROR)", "System.assertEquals(false, severity.equals(ApexPages.Severity.WARNING));"},
		"apex:ApexPages.Severity.hashCode()":      {"severity.hashCode()", "System.assertNotEquals(severity.hashCode(), ApexPages.Severity.WARNING.hashCode());"},
		"apex:ApexPages.Severity.ordinal()":       {"severity.ordinal()", "System.assertEquals(1, severity.ordinal());"},
		"apex:ApexPages.Severity.valueOf(String)": {"ApexPages.Severity.valueOf('ERROR')", "System.assertEquals(ApexPages.Severity.ERROR, valueOfError);"},
		"apex:ApexPages.Severity.values()":        {"ApexPages.Severity.values()", "System.assertEquals(ApexPages.Severity.FATAL, severities[0]);"},
	}
	root := filepath.Join("..", "..", "docs", "fixtures")
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
	path := filepath.Join(root, owner+".json")
	fixture, err := compat.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if fixture.Command.Kind != "exec" || len(fixture.Source) != 1 || len(fixture.Command.Args) != 1 || fixture.Source[0].Content != fixture.Command.Args[0] {
		t.Fatalf("execution envelope = %#v", fixture)
	}
	if len(fixture.Evidence) != len(want)+5 {
		t.Fatalf("owner evidence rows = %d, want five method rows plus five existing witnesses", len(fixture.Evidence))
	}
	for _, evidence := range fixture.Evidence {
		delete(prior, evidence.SurfaceID)
	}
	if len(prior) != 0 {
		t.Fatalf("prior witness IDs missing: %v", prior)
	}
	data, err := os.ReadFile(path)
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
	source := fixture.Source[0].Content
	seen := make(map[string]bool, len(want))
	for _, evidence := range fixture.Evidence {
		expected, ok := want[evidence.SurfaceID]
		if !ok {
			continue
		}
		if seen[evidence.SurfaceID] || evidence.Kind != "exec" || !strings.Contains(source, expected[0]) || !strings.Contains(source, expected[1]) {
			t.Fatalf("invalid method evidence row = %#v", evidence)
		}
		seen[evidence.SurfaceID] = true
	}
	if len(seen) != len(want) {
		t.Fatalf("method owner IDs = %d, want exactly %d", len(seen), len(want))
	}
	result, err := compat.Run(fixture)
	if err != nil || !result.OK {
		t.Fatalf("run %s = %#v, %v", fixture.Name, result, err)
	}
}
