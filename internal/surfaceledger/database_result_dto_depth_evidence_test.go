package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func databaseResultDTODepthIDs() []string {
	groups := []struct {
		typeName string
		members  []string
	}{
		{"DeleteResult", []string{"equals(Object)", "errors", "hashCode()", "id", "success", "toString()"}},
		{"EmptyRecycleBinResult", []string{"", "equals(Object)", "errors", "hashCode()", "id", "success", "toString()"}},
		{"MergeResult", []string{"equals(Object)", "hashCode()", "id", "mergedrecordids", "success", "toString()", "updatedrelatedids"}},
		{"SaveResult", []string{"equals(Object)", "errors", "hashCode()", "id", "success", "toString()"}},
		{"UndeleteResult", []string{"", "equals(Object)", "errors", "hashCode()", "id", "success", "toString()"}},
		{"UpsertResult", []string{"created", "equals(Object)", "errors", "hashCode()", "id", "success", "toString()"}},
	}
	var ids []string
	for _, group := range groups {
		prefix := "apex:Database." + group.typeName
		for _, member := range group.members {
			if member == "" {
				ids = append(ids, prefix)
			} else {
				ids = append(ids, prefix+"."+member)
			}
		}
	}
	return ids
}

func TestDatabaseResultDTOsHaveExactLocalOnlyEvidence(t *testing.T) {
	const (
		legacyOwner     = "fixture:core-runtime-database-result-dto-local-evidence"
		fieldStateOwner = "fixture:data-database-result-field-state-runtime"
		runtimeOwner    = "fixture:data-platform-database-result-dto-wave18-runtime"
	)
	legacyIDs := map[string]struct{}{
		"apex:Database.DeleteResult.errors":          {},
		"apex:Database.DeleteResult.id":              {},
		"apex:Database.DeleteResult.success":         {},
		"apex:Database.EmptyRecycleBinResult.errors": {},
		"apex:Database.SaveResult.errors":            {},
		"apex:Database.SaveResult.id":                {},
		"apex:Database.SaveResult.success":           {},
		"apex:Database.UndeleteResult.errors":        {},
		"apex:Database.UndeleteResult.id":            {},
		"apex:Database.UndeleteResult.success":       {},
	}
	fieldStateIDs := map[string]struct{}{
		"apex:Database.DeleteResult.errors":    {},
		"apex:Database.DeleteResult.id":        {},
		"apex:Database.DeleteResult.success":   {},
		"apex:Database.SaveResult.errors":      {},
		"apex:Database.SaveResult.id":          {},
		"apex:Database.SaveResult.success":     {},
		"apex:Database.UndeleteResult.errors":  {},
		"apex:Database.UndeleteResult.id":      {},
		"apex:Database.UndeleteResult.success": {},
	}
	wantIDs := databaseResultDTODepthIDs()
	if len(wantIDs) != 40 {
		t.Fatalf("frozen Database result set = %d, want 40", len(wantIDs))
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
	expectedOwners := make(map[string]map[string]struct{}, len(wantIDs))
	for _, id := range wantIDs {
		expectedOwners[id] = map[string]struct{}{runtimeOwner: {}}
	}
	for id := range legacyIDs {
		expectedOwners[id] = map[string]struct{}{legacyOwner: {}}
	}
	for id := range fieldStateIDs {
		expectedOwners[id][fieldStateOwner] = struct{}{}
	}
	selectedByID := make(map[string]SurfaceLedgerRow, len(wantIDs))
	for _, row := range rows {
		owners, ok := expectedOwners[row.SurfaceID]
		if !ok {
			continue
		}
		if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported || len(row.Sources) != 1 {
			t.Fatalf("%s evidence/behavior/source = %s/%s/%v", row.SurfaceID, row.Evidence, row.GladeBehavior, row.Sources)
		}
		if _, ok := owners[row.Sources[0]]; !ok {
			t.Fatalf("%s source = %v, want one of %v", row.SurfaceID, row.Sources, owners)
		}
		if _, seen := selectedByID[row.SurfaceID]; !seen {
			selectedByID[row.SurfaceID] = row
		}
	}
	selected := make([]SurfaceLedgerRow, 0, len(wantIDs))
	for _, id := range wantIDs {
		if row, ok := selectedByID[id]; ok {
			selected = append(selected, row)
		}
	}
	assertExactSurfaceSet(t, selected, wantIDs)

	fixturePath := filepath.Join(root, "docs", "fixtures", "core-runtime-database-result-dto-local-evidence.json")
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
	if fixture.Command.Kind != "test" || len(fixture.Source) != 3 {
		t.Fatalf("fixture source/command envelope = %d sources/%q command", len(fixture.Source), fixture.Command.Kind)
	}
	source := fixture.Source[len(fixture.Source)-1].Content
	for _, witness := range []string{
		"Database.SaveResult saved = Database.insert(base, false);",
		"saved.success", "saved.id", "saved.errors", "saved.equals(saved)", "saved.hashCode()", "saved.toString()",
		"Database.UpsertResult upserted = Database.upsert(upsertRow, false);",
		"upserted.created", "upserted.success", "upserted.id", "upserted.errors", "upserted.equals(upserted)", "upserted.hashCode()", "upserted.toString()",
		"Database.MergeResult merged = Database.merge(master, duplicate, false);",
		"merged.success", "merged.id", "merged.mergedrecordids", "merged.updatedrelatedids", "merged.equals(merged)", "merged.hashCode()", "merged.toString()",
		"Database.DeleteResult deleted = Database.delete(recycle, false);",
		"deleted.success", "deleted.id", "deleted.errors", "deleted.equals(deleted)", "deleted.hashCode()", "deleted.toString()",
		"Database.UndeleteResult restored = Database.undelete(recycle, false);",
		"restored.success", "restored.id", "restored.errors", "restored.equals(restored)", "restored.hashCode()", "restored.toString()",
		"Database.EmptyRecycleBinResult emptied = Database.emptyRecycleBin(recycle);",
		"emptied.success", "emptied.id", "emptied.errors", "emptied.equals(emptied)", "emptied.hashCode()", "emptied.toString()",
	} {
		if !strings.Contains(source, witness) {
			t.Fatalf("Database result source missing %q", witness)
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
	if policy.SalesforceEligible == nil || *policy.SalesforceEligible || policy.SalesforceExclusionClass != "policy-local-only" || !strings.Contains(policy.SalesforceExclusionReason, "zero Salesforce parity") {
		t.Fatalf("fixture policy = %#v", policy)
	}
}
