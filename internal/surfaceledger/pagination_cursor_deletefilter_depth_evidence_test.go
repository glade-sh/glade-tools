package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func TestPaginationCursorDeleteFilterHasExactLocalOnlyEnumEvidence(t *testing.T) {
	wantIDs := []string{
		"apex:Database.PaginationCursor.DeleteFilter",
		"apex:Database.PaginationCursor.DeleteFilter.DELETED_ROWS_ONLY",
		"apex:Database.PaginationCursor.DeleteFilter.NO_DELETED_ROWS",
		"apex:Database.PaginationCursor.DeleteFilter.NO_DELETED_SHARING_ROWS",
		"apex:Database.PaginationCursor.DeleteFilter.NO_FILTER",
		"apex:Database.PaginationCursor.DeleteFilter.equals(Object)",
		"apex:Database.PaginationCursor.DeleteFilter.hashCode()",
		"apex:Database.PaginationCursor.DeleteFilter.ordinal()",
		"apex:Database.PaginationCursor.DeleteFilter.valueOf(String)",
		"apex:Database.PaginationCursor.DeleteFilter.values()",
	}
	const owner = "fixture:data-platform-database-pagination-cursor-wave19-runtime"
	root := filepath.Join("..", "..")
	paths, err := filepath.Glob(filepath.Join(root, "docs", "fixtures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := BuildEvidenceSnapshot(paths)
	if err != nil {
		t.Fatal(err)
	}
	want := make(map[string]struct{}, len(wantIDs))
	for _, id := range wantIDs {
		want[id] = struct{}{}
	}
	var selected []SurfaceLedgerRow
	for _, row := range rows {
		if _, ok := want[row.SurfaceID]; ok {
			selected = append(selected, row)
		}
	}
	assertExactSurfaceSet(t, selected, wantIDs)
	for _, row := range selected {
		if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported || len(row.Sources) != 1 || row.Sources[0] != owner {
			t.Fatalf("%s evidence/behavior/source = %s/%s/%v", row.SurfaceID, row.Evidence, row.GladeBehavior, row.Sources)
		}
	}

	fixturePath := filepath.Join(root, "docs", "fixtures", "data-platform-database-pagination-cursor-wave19-runtime.json")
	fixture, err := compat.LoadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	if result, err := compat.Run(fixture); err != nil || !result.OK {
		t.Fatalf("fixture execution = %#v, error = %v", result, err)
	}
	if fixture.Command.Kind != "exec" || len(fixture.Source) != 3 {
		t.Fatalf("fixture source/command envelope = %d sources/%q command", len(fixture.Source), fixture.Command.Kind)
	}
	source := fixture.Source[2].Content
	for _, witness := range []string{
		"Database.PaginationCursor.DeleteFilter.DELETED_ROWS_ONLY",
		"Database.PaginationCursor.DeleteFilter.NO_DELETED_ROWS",
		"Database.PaginationCursor.DeleteFilter.NO_DELETED_SHARING_ROWS",
		"Database.PaginationCursor.DeleteFilter.NO_FILTER",
		"noFilter.equals(noFilter)", "!noFilter.equals(deletedRowsOnly)", "!noFilter.equals(null)",
		"System.assertNotEquals(noFilter.hashCode(), deletedRowsOnly.hashCode())", "noFilter.ordinal()",
		"Database.PaginationCursor.DeleteFilter.valueOf('NO_FILTER')",
		"Database.PaginationCursor.DeleteFilter.values()",
	} {
		if !strings.Contains(source, witness) {
			t.Fatalf("PaginationCursor DeleteFilter source missing %q", witness)
		}
	}

	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	var policy struct {
		SalesforceEligible        *bool  `json:"salesforceEligible"`
		SalesforceExclusionClass  string `json:"salesforceExclusionClass"`
		SalesforceExclusionReason string `json:"salesforceExclusionReason"`
	}
	if err := json.Unmarshal(data, &policy); err != nil {
		t.Fatal(err)
	}
	if policy.SalesforceEligible == nil || *policy.SalesforceEligible || policy.SalesforceExclusionClass != "policy-local-only" || !strings.Contains(policy.SalesforceExclusionReason, "excluded from hosted parity") {
		t.Fatalf("fixture policy = %#v", policy)
	}
}
