package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func TestDatabaseValueObjectsHaveExactExecutableLocalEvidence(t *testing.T) {
	owners := map[string]string{
		"apex:Database.Cursor":                          "fixture:current-base-local-runtime-required-Database-001",
		"apex:Database.DeletedRecord.deleteddate":       "fixture:current-base-local-runtime-required-Database-001",
		"apex:Database.DeletedRecord.id":                "fixture:current-base-local-runtime-required-Database-001",
		"apex:Database.Error.equals(Object)":            "fixture:current-base-local-runtime-required-Database-002",
		"apex:Database.Error.hashCode()":                "fixture:current-base-local-runtime-required-Database-002",
		"apex:Database.Error.toString()":                "fixture:current-base-local-runtime-required-Database-002",
		"apex:Database.GetDeletedResult.equals(Object)": "fixture:current-base-local-runtime-required-Database-002",
		"apex:Database.GetDeletedResult.hashCode()":     "fixture:current-base-local-runtime-required-Database-002",
		"apex:Database.GetDeletedResult.toString()":     "fixture:current-base-local-runtime-required-Database-002",
		"apex:Database.GetUpdatedResult.equals(Object)": "fixture:current-base-local-runtime-required-Database-002",
		"apex:Database.GetUpdatedResult.hashCode()":     "fixture:current-base-local-runtime-required-Database-002",
		"apex:Database.GetUpdatedResult.toString()":     "fixture:current-base-local-runtime-required-Database-002",
		"apex:Database.MergeRequest.equals(Object)":     "fixture:current-base-local-runtime-required-Database-002",
		"apex:Database.MergeRequest.hashCode()":         "fixture:current-base-local-runtime-required-Database-002",
		"apex:Database.MergeRequest.toString()":         "fixture:current-base-local-runtime-required-Database-002",
	}
	root := filepath.Join("..", "..")
	paths, err := filepath.Glob(filepath.Join(root, "docs", "fixtures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := BuildEvidenceSnapshot(paths)
	if err != nil {
		t.Fatal(err)
	}
	selected := make(map[string]SurfaceLedgerRow, len(owners))
	for _, row := range rows {
		if _, ok := owners[row.SurfaceID]; ok {
			selected[row.SurfaceID] = row
		}
	}
	if len(selected) != len(owners) {
		t.Fatalf("selected evidence rows = %d, want %d", len(selected), len(owners))
	}
	for id, owner := range owners {
		row, ok := selected[id]
		if !ok || row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported || len(row.Sources) != 1 || row.Sources[0] != owner {
			t.Fatalf("%s evidence row = %#v, want sole owner %q", id, row, owner)
		}
	}

	fixtures := map[string][]string{
		"current-base-local-runtime-required-database-001.json": {
			"Database.Cursor cursor = new Database.Cursor();",
			"deletedRecord.deleteddate = Date.today();",
			"deletedRecord.id = '001000000000001AAA';",
			"System.assertEquals(Date.today(), deletedRecord.deleteddate);",
			"System.assertEquals('001000000000001AAA', deletedRecord.id);",
		},
		"current-base-local-runtime-required-database-002.json": {
			"error.equals(error)", "error.equals(null)", "error.hashCode()", "error.toString()",
			"deleted.equals(deleted)", "deleted.equals(null)", "deleted.hashCode()", "deleted.toString()",
			"updated.equals(updated)", "updated.equals(null)", "updated.hashCode()", "updated.toString()",
			"mergeRequest.equals(mergeRequest)", "mergeRequest.equals(null)", "mergeRequest.hashCode()", "mergeRequest.toString()",
		},
	}
	for name, witnesses := range fixtures {
		path := filepath.Join(root, "docs", "fixtures", name)
		fixture, err := compat.LoadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := compat.Validate(fixture); err != nil {
			t.Fatal(err)
		}
		if result, err := compat.Run(fixture); err != nil || !result.OK {
			t.Fatalf("%s execution = %#v, error = %v", name, result, err)
		}
		var source strings.Builder
		for _, file := range fixture.Source {
			source.WriteString(file.Content)
		}
		sourceText := source.String()
		for _, witness := range witnesses {
			if !strings.Contains(sourceText, witness) {
				t.Fatalf("%s source missing %q", name, witness)
			}
		}

		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var policy struct {
			SalesforceEligible        *bool  `json:"salesforceEligible"`
			SalesforceExclusionClass  string `json:"salesforceExclusionClass"`
			SalesforceExclusionReason string `json:"salesforceExclusionReason"`
			Profile                   struct {
				SelectedRowCount int `json:"selectedRowCount"`
			} `json:"profile"`
		}
		if err := json.Unmarshal(data, &policy); err != nil {
			t.Fatal(err)
		}
		if policy.SalesforceEligible == nil || *policy.SalesforceEligible || policy.SalesforceExclusionClass != "policy-local-only" || policy.SalesforceExclusionReason == "" || policy.Profile.SelectedRowCount != len(fixture.Evidence) {
			t.Fatalf("%s policy = %#v", name, policy)
		}
	}
}
