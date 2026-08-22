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
		"apex:Database.Cursor":                          "fixture:data-database-cursor-object-runtime-depth",
		"apex:Database.DeletedRecord.deleteddate":       "fixture:data-platform-database-dto-wave18-runtime",
		"apex:Database.DeletedRecord.id":                "fixture:data-platform-database-dto-wave18-runtime",
		"apex:Database.Error.equals(Object)":            "fixture:data-platform-database-dto-wave18-runtime",
		"apex:Database.Error.hashCode()":                "fixture:data-platform-database-dto-wave18-runtime",
		"apex:Database.Error.toString()":                "fixture:data-platform-database-dto-wave18-runtime",
		"apex:Database.GetDeletedResult.equals(Object)": "fixture:data-platform-database-dto-wave18-runtime",
		"apex:Database.GetDeletedResult.hashCode()":     "fixture:data-platform-database-dto-wave18-runtime",
		"apex:Database.GetDeletedResult.toString()":     "fixture:data-platform-database-dto-wave18-runtime",
		"apex:Database.GetUpdatedResult.equals(Object)": "fixture:data-platform-database-dto-wave18-runtime",
		"apex:Database.GetUpdatedResult.hashCode()":     "fixture:data-platform-database-dto-wave18-runtime",
		"apex:Database.GetUpdatedResult.toString()":     "fixture:data-platform-database-dto-wave18-runtime",
		"apex:Database.MergeRequest.equals(Object)":     "fixture:data-platform-database-dto-wave18-runtime",
		"apex:Database.MergeRequest.hashCode()":         "fixture:data-platform-database-dto-wave18-runtime",
		"apex:Database.MergeRequest.toString()":         "fixture:data-platform-database-dto-wave18-runtime",
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
		"data-database-cursor-object-runtime-depth.json": {
			"Database.Cursor cursor = Database.getCursor('SELECT Id, Name FROM Account ORDER BY Name');",
		},
		"current-base-local-runtime-required-database-001.json": {
			"Database.Cursor cursor = new Database.Cursor();",
		},
		"current-base-local-runtime-required-database-002.json": {
			"Object errorsType = (Database.Errors)null;", "Database.LocaleOptions locale = new Database.LocaleOptions();",
		},
		"current-base-local-runtime-required-database-003.json": {
			"Database.SaveResult saveResult = new Database.SaveResult();", "Object savepoint = (Database.Savepoint)null;", "Database.UndeleteResult undeleteResult = new Database.UndeleteResult();",
		},
		"data-platform-database-dto-wave18-runtime.json": {
			"deletedA.deleteddate = Date.newInstance(2026,1,2);", "deletedA.equals(deletedB)", "deletedA.hashCode()", "deletedA.toString()",
			"duplicateA.equals(duplicateB)", "duplicateA.hashCode()", "duplicateA.toString()",
			"errorA.equals(errorB)", "errorA.hashCode()", "errorA.toString()",
			"deletedResultA.earliestdateavailable", "deletedResultA.equals(deletedResultB)", "deletedResultA.hashCode()", "deletedResultA.toString()",
			"updatedA.equals(updatedB)", "updatedA.hashCode()", "updatedA.toString()",
			"mergeA.equals(mergeB)", "mergeA.hashCode()", "mergeA.toString()",
			"mergeResult.errors", "fetch.clone()", "pagination.clone()", "work.clone()",
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
			Profile                   *struct {
				SelectedRowCount int `json:"selectedRowCount"`
			} `json:"profile"`
		}
		if err := json.Unmarshal(data, &policy); err != nil {
			t.Fatal(err)
		}
		if policy.SalesforceEligible == nil || *policy.SalesforceEligible || policy.SalesforceExclusionClass != "policy-local-only" || policy.SalesforceExclusionReason == "" || (policy.Profile != nil && policy.Profile.SelectedRowCount != len(fixture.Evidence)) {
			t.Fatalf("%s policy = %#v", name, policy)
		}
	}
}
