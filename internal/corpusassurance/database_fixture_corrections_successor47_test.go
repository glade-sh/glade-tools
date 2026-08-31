package corpusassurance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func TestDatabaseCursorFixtureScopesEveryQueryToInsertedRecords(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "fixtures", "data-database-cursor-runtime-depth.json")
	fixture := loadDatabaseSuccessor47Fixture(t, path)
	assertDatabaseSuccessor47CommandSource(t, fixture, "exec")

	source := fixture.Source[0].Content
	for _, token := range []string{
		"Set<Id> accountIds = new Set<Id>{first.Id, second.Id, third.Id}",
		"SELECT Id, Name FROM Account WHERE Id IN :accountIds ORDER BY Name",
		"SELECT COUNT() FROM Account WHERE Id IN :accountIds",
	} {
		if !strings.Contains(source, token) {
			t.Fatalf("cursor source missing isolated-record witness %q", token)
		}
	}
	for _, broad := range []string{
		"SELECT Id, Name FROM Account ORDER BY Name",
		"SELECT COUNT() FROM Account]",
	} {
		if strings.Contains(source, broad) {
			t.Fatalf("cursor source retains org-wide query %q", broad)
		}
	}
	if got := databaseSuccessor47SurfaceIDs(fixture); !reflect.DeepEqual(got, []string{
		"apex:System.Database.getCursor(String)",
		"apex:System.Database.getCursor(String,AccessLevel)",
	}) {
		t.Fatalf("cursor evidence = %v", got)
	}
}

func TestDatabaseAbsentObjectSignaturesAreLocalOnly(t *testing.T) {
	root := filepath.Join("..", "..", "docs", "fixtures")
	localFile := "data-database-object-signatures-local-only-api67.json"
	absent := []string{
		"apex:System.Database.insert(Object,Boolean)",
		"apex:System.Database.update(Object,Boolean,AccessLevel)",
		"apex:System.Database.delete(Object,Boolean)",
		"apex:System.Database.delete(Object,AccessLevel)",
		"apex:System.Database.undelete(Object,AccessLevel)",
	}
	eligible := map[string][]string{
		"data-database-batch-boolean-runtime.json": {
			"apex:System.Database.update(SObject,Boolean)",
		},
		"data-database-delete-undelete-object-runtime.json": {
			"apex:System.Database.delete(List<Object>)",
			"apex:System.Database.delete(List<Object>,Boolean)",
			"apex:System.Database.undelete(List<Object>)",
			"apex:System.Database.undelete(List<Object>,Boolean)",
			"apex:System.Database.undelete(Object)",
			"apex:System.Database.undelete(Object,Boolean)",
		},
		"data-database-delete-undelete-object-system-mode-runtime.json": {
			"apex:System.Database.delete(List<Object>,AccessLevel)",
			"apex:System.Database.delete(List<Object>,Boolean,AccessLevel)",
			"apex:System.Database.undelete(List<Object>,AccessLevel)",
			"apex:System.Database.undelete(List<Object>,Boolean,AccessLevel)",
		},
	}

	if _, err := os.Stat(filepath.Join(root, "data-database-batch-boolean-system-mode-runtime.json")); !os.IsNotExist(err) {
		t.Fatalf("empty eligible fixture still exists: %v", err)
	}

	supported := map[string]bool{}
	for filename, want := range eligible {
		path := filepath.Join(root, filename)
		fixture := loadDatabaseSuccessor47Fixture(t, path)
		if got := databaseSuccessor47SurfaceIDs(fixture); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s evidence = %v, want %v", filename, got, want)
		}
		kind := "exec"
		if strings.Contains(filename, "system-mode") {
			kind = "test"
		}
		assertDatabaseSuccessor47CommandSource(t, fixture, kind)
		if filename == "data-database-batch-boolean-runtime.json" {
			if !strings.Contains(fixture.Source[0].Content, "SObject updatedObject") || strings.Contains(fixture.Source[0].Content, "; Object updatedObject") {
				t.Fatalf("%s does not use the Salesforce SObject overload", filename)
			}
		}
		policy := loadDatabaseSuccessor47Policy(t, path)
		if policy.SalesforceEligible == nil || !*policy.SalesforceEligible || policy.SalesforceExclusionClass != "" || policy.SalesforceExclusionReason != "" {
			t.Fatalf("%s Salesforce policy = %#v", filename, policy)
		}
		for _, id := range want {
			supported[id] = true
		}
	}

	path := filepath.Join(root, localFile)
	fixture := loadDatabaseSuccessor47Fixture(t, path)
	assertDatabaseSuccessor47CommandSource(t, fixture, "exec")
	if got := databaseSuccessor47SurfaceIDs(fixture); !reflect.DeepEqual(got, absent) {
		t.Fatalf("%s evidence = %v, want %v", localFile, got, absent)
	}
	policy := loadDatabaseSuccessor47Policy(t, path)
	if policy.EvidenceOnly || policy.Mode != "local-runtime" || policy.SalesforceEligible == nil || *policy.SalesforceEligible || policy.SalesforceExclusionClass != "policy-local-only" || !strings.Contains(policy.SalesforceExclusionReason, "zero Salesforce parity") || policy.SelectedRowCount != len(absent) {
		t.Fatalf("%s local-only policy = %#v", localFile, policy)
	}
	for _, token := range []string{
		"Object insertedObject",
		"Database.insert(insertedObject, false)",
		"Database.update(updatedObject, false, AccessLevel.SYSTEM_MODE)",
		"Database.delete(deletedObject, false)",
		"Database.delete(accessDeletedObject, AccessLevel.SYSTEM_MODE)",
		"Database.undelete(accessRestoredObject, AccessLevel.SYSTEM_MODE)",
	} {
		if !strings.Contains(fixture.Source[0].Content, token) {
			t.Fatalf("%s source missing %q", localFile, token)
		}
	}
	if result, err := compat.Run(fixture); err != nil || !result.OK {
		t.Fatalf("%s local execution = %#v, error = %v", localFile, result, err)
	}

	paths, err := filepath.Glob(filepath.Join(root, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	absentSet := map[string]bool{}
	supportedOwners := map[string]int{}
	for _, id := range absent {
		absentSet[id] = true
	}
	for _, candidate := range paths {
		policy := loadDatabaseSuccessor47Policy(t, candidate)
		if policy.EvidenceOnly || policy.SalesforceEligible == nil || !*policy.SalesforceEligible {
			continue
		}
		fixture := loadDatabaseSuccessor47Fixture(t, candidate)
		for _, id := range databaseSuccessor47SurfaceIDs(fixture) {
			if absentSet[id] {
				t.Fatalf("Salesforce-absent signature %s remains eligible in %s", id, filepath.Base(candidate))
			}
			if supported[id] {
				supportedOwners[id]++
			}
		}
	}
	for id := range supported {
		if supportedOwners[id] != 1 {
			t.Fatalf("supported sibling %s eligible owners = %d, want 1", id, supportedOwners[id])
		}
	}
}

type databaseSuccessor47Policy struct {
	EvidenceOnly              bool            `json:"evidenceOnly"`
	Mode                      string          `json:"mode"`
	SalesforceEligible        *bool           `json:"salesforceEligible"`
	SalesforceExclusionClass  string          `json:"salesforceExclusionClass"`
	SalesforceExclusionReason string          `json:"salesforceExclusionReason"`
	Profile                   json.RawMessage `json:"profile"`
	SelectedRowCount          int
}

func loadDatabaseSuccessor47Policy(t *testing.T, path string) databaseSuccessor47Policy {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var policy databaseSuccessor47Policy
	if err := json.Unmarshal(data, &policy); err != nil {
		t.Fatal(err)
	}
	var profile struct {
		SelectedRowCount int `json:"selectedRowCount"`
	}
	if len(policy.Profile) > 0 && policy.Profile[0] == '{' {
		if err := json.Unmarshal(policy.Profile, &profile); err != nil {
			t.Fatal(err)
		}
		policy.SelectedRowCount = profile.SelectedRowCount
	}
	return policy
}

func loadDatabaseSuccessor47Fixture(t *testing.T, path string) compat.Fixture {
	t.Helper()
	fixture, err := compat.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func assertDatabaseSuccessor47CommandSource(t *testing.T, fixture compat.Fixture, kind string) {
	t.Helper()
	if fixture.Command.Kind != kind || len(fixture.Source) != 1 {
		t.Fatalf("%s command/source = %#v/%#v", fixture.Name, fixture.Command, fixture.Source)
	}
	for _, evidence := range fixture.Evidence {
		if evidence.Kind != kind {
			t.Fatalf("%s evidence %s kind = %q, want %q", fixture.Name, evidence.SurfaceID, evidence.Kind, kind)
		}
	}
	if kind == "exec" && (len(fixture.Command.Args) != 1 || fixture.Command.Args[0] != fixture.Source[0].Content || fixture.Source[0].Path != "anonymous.apex") {
		t.Fatalf("%s exec command/source differ", fixture.Name)
	}
	if kind == "test" && (len(fixture.Command.Args) != 0 || !strings.HasSuffix(fixture.Source[0].Path, "Test.cls") || !strings.Contains(fixture.Source[0].Content, "@isTest")) {
		t.Fatalf("%s is not one deployable test source", fixture.Name)
	}
}

func databaseSuccessor47SurfaceIDs(fixture compat.Fixture) []string {
	ids := make([]string, len(fixture.Evidence))
	for i, evidence := range fixture.Evidence {
		ids[i] = evidence.SurfaceID
	}
	return ids
}
