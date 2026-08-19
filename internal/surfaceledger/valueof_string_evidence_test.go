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
	root := filepath.Join("..", "..", "docs", "fixtures")
	paths := []string{filepath.Join(root, "core-valueof-string-local.json")}
	want := map[string]struct {
		call, assert string
	}{
		"apex:System.Integer.valueOf(String)": {"Integer.valueOf('42')", "System.assertEquals(42, integerValue);"},
		"apex:System.Long.valueOf(String)":    {"Long.valueOf('9001')", "System.assertEquals(9001, longValue);"},
		"apex:System.Double.valueOf(String)":  {"Double.valueOf('2.25')", "System.assertEquals(2.25, doubleValue);"},
		"apex:System.Id.valueOf(String)":      {"Id.valueOf('001B000001DVM9t')", "System.assertEquals('001B000001DVM9t', recordId.toString());"},
	}
	owners := map[string]string{}
	for _, path := range paths {
		fixture, err := compat.LoadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if fixture.Name != "core-valueof-string-local" || fixture.Command.Kind != "exec" || len(fixture.Source) != 1 {
			t.Fatalf("%s execution envelope = kind:%q source:%d", fixture.Name, fixture.Command.Kind, len(fixture.Source))
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var policy struct {
			SalesforceEligible        bool   `json:"salesforceEligible"`
			SalesforceExclusionClass  string `json:"salesforceExclusionClass"`
			SalesforceExclusionReason string `json:"salesforceExclusionReason"`
		}
		if err := json.Unmarshal(data, &policy); err != nil {
			t.Fatal(err)
		}
		if policy.SalesforceEligible || policy.SalesforceExclusionClass != "policy-local-only" || !strings.Contains(policy.SalesforceExclusionReason, "zero Salesforce parity") {
			t.Fatalf("fixture policy = %#v, want local-only zero-parity", policy)
		}
		result, err := compat.Run(fixture)
		if err != nil || !result.OK {
			t.Fatalf("run %s = %#v, %v", fixture.Name, result, err)
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
			if evidence.Kind != "exec" {
				t.Fatalf("%s owner kind:%s", evidence.SurfaceID, evidence.Kind)
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
