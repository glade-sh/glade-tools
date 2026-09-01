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

func TestSalesforceFixtureCorrectionsSecondBatch(t *testing.T) {
	root := filepath.Join("..", "..", "docs", "fixtures")

	resource := loadSecondBatchFixture(t, root, "core-runtime-page-reference-resource-evidence.json")
	for _, evidence := range resource.Evidence {
		if evidence.Kind != "test" || !localProofCommandMatchesDisposition(localRuntimeRequired, resource.Command.Kind, evidence.SurfaceID) {
			t.Fatalf("resource evidence %s is not locally runnable through test", evidence.SurfaceID)
		}
	}
	if localProofCommandMatchesDisposition(localRuntimeRequired, "test", "apex:System.PageReference.forResource(String,Integer)") {
		t.Fatal("unowned PageReference.forResource signature is locally runnable through test")
	}
	if resource.Command.Kind != "test" {
		t.Fatalf("resource command = %q, want test", resource.Command.Kind)
	}
	assertDeployableClassNames(t, resource)
	resourceSource := sourceContent(resource, "force-app/main/default/classes/PageReferenceResourceEvidenceTest.cls")
	for _, token := range []string{"startsWith('/resource/')", "endsWith('/Images')", "endsWith('/Images/icons/logo.png')"} {
		if !strings.Contains(resourceSource, token) {
			t.Fatalf("resource source missing stable URL assertion %q", token)
		}
	}
	if strings.Contains(resourceSource, "assertEquals('/resource/Images'") {
		t.Fatal("resource source retains cache-version-sensitive URL assertion")
	}

	dml := loadSecondBatchFixture(t, root, "data-database-dml-system-mode-runtime.json")
	assertDeployableClassNames(t, dml)
	dmlSource := sourceContent(dml, "force-app/main/default/classes/DataDatabaseDmlSystemModeRuntimeTest.cls")
	if !strings.Contains(dmlSource, "Database.upsert(upserted, Account.Id, false, AccessLevel.SYSTEM_MODE)") {
		t.Fatal("SYSTEM_MODE DML fixture does not exercise the SObjectField overload with Account.Id")
	}
	for _, source := range dml.Source {
		if strings.Contains(source.Path, "/objects/") {
			t.Fatalf("SYSTEM_MODE DML fixture retains deployable object metadata %q", source.Path)
		}
	}

	eligible := loadSecondBatchFixture(t, root, "data-database-empty-recycle-bin-runtime.json")
	if got := secondBatchSurfaceIDs(eligible); !reflect.DeepEqual(got, []string{
		"apex:System.Database.emptyRecycleBin(List<Id>)",
		"apex:System.Database.emptyRecycleBin(Object)",
		"apex:System.Database.emptyRecycleBin(List<Object>)",
	}) {
		t.Fatalf("eligible emptyRecycleBin surfaces = %v", got)
	}
	if source := anonymousSource(t, eligible); strings.Contains(source, "emptyRecycleBin(idValue)") || !strings.Contains(source, "emptyRecycleBin(ids)") {
		t.Fatalf("eligible emptyRecycleBin source does not isolate supported overloads: %s", source)
	} else if strings.Contains(source, "Rows =") {
		t.Fatalf("eligible emptyRecycleBin source asserts engine-specific same-transaction query visibility: %s", source)
	}
	negative := loadSecondBatchFixture(t, root, "current-api67-negative-database-empty-recycle-bin-id.json")
	if negative.APIVersion != "67.0" || negative.Command.Kind != "check" || len(negative.Schema) != 1 || len(negative.Expected.Result) == 0 || !reflect.DeepEqual(secondBatchSurfaceIDs(negative), []string{"apex:System.Database.emptyRecycleBin(Id)"}) {
		t.Fatalf("single-Id negative fixture contract = API %q command %#v expected %#v evidence %v", negative.APIVersion, negative.Command, negative.Expected, secondBatchSurfaceIDs(negative))
	}
	negativeData, err := os.ReadFile(filepath.Join(root, "current-api67-negative-database-empty-recycle-bin-id.json"))
	if err != nil {
		t.Fatal(err)
	}
	var policy struct {
		SalesforceEligible        *bool  `json:"salesforceEligible"`
		SalesforceExclusionClass  string `json:"salesforceExclusionClass"`
		SalesforceExclusionReason string `json:"salesforceExclusionReason"`
	}
	if err := json.Unmarshal(negativeData, &policy); err != nil {
		t.Fatal(err)
	}
	if policy.SalesforceEligible == nil || *policy.SalesforceEligible || policy.SalesforceExclusionClass != "policy-local-only" || !strings.Contains(policy.SalesforceExclusionReason, "no Salesforce runtime parity") {
		t.Fatalf("single-Id negative policy = %#v", policy)
	}
	paths, err := filepath.Glob(filepath.Join(root, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var header struct {
			EvidenceOnly       bool  `json:"evidenceOnly"`
			SalesforceEligible *bool `json:"salesforceEligible"`
			Evidence           []struct {
				SurfaceID string `json:"surfaceId"`
			} `json:"evidence"`
		}
		if err := json.Unmarshal(data, &header); err != nil {
			t.Fatal(err)
		}
		if header.EvidenceOnly || header.SalesforceEligible == nil || !*header.SalesforceEligible {
			continue
		}
		for _, evidence := range header.Evidence {
			if evidence.SurfaceID == "apex:System.Database.emptyRecycleBin(Id)" {
				t.Fatalf("Salesforce-absent single-Id overload remains eligible in %s", filepath.Base(path))
			}
		}
	}

	for _, filename := range []string{
		"data-database-query-locator-access-system-mode-runtime.json",
		"test-helper-load-data-fixed-search-evidence.json",
		"data-database-query-locator-modes-system-mode-runtime.json",
	} {
		assertDeployableClassNames(t, loadSecondBatchFixture(t, root, filename))
	}

	for _, filename := range []string{
		"data-database-query-locator-access-system-mode-runtime.json",
		"data-database-query-locator-modes-runtime.json",
		"data-database-query-locator-modes-system-mode-runtime.json",
	} {
		fixture := loadSecondBatchFixture(t, root, filename)
		source := strings.Join(secondBatchSourceContents(fixture), "\n")
		if strings.Contains(source, "Object iterator") || strings.Contains(source, "Object listIterator") || strings.Contains(source, "Object stringIterator") || !strings.Contains(source, "Iterator<SObject>") {
			t.Fatalf("%s does not use typed query-locator iterators", filename)
		}
	}

	modes := loadSecondBatchFixture(t, root, "data-database-query-locator-modes-runtime.json")
	modesSource := anonymousSource(t, modes)
	if !strings.Contains(modesSource, "Set<Id> fixtureIds") || strings.Count(modesSource, "Id IN :fixtureIds") < 4 {
		t.Fatalf("query-locator modes fixture does not scope every query to fixture rows: %s", modesSource)
	}
	for _, broad := range []string{"FROM Account ORDER BY Name", "SELECT Id FROM Account', AccessLevel.USER_MODE", "WHERE Rating = :rating'"} {
		if strings.Contains(modesSource, broad) {
			t.Fatalf("query-locator modes fixture retains org-wide query %q", broad)
		}
	}
}

func loadSecondBatchFixture(t *testing.T, root, filename string) compat.Fixture {
	t.Helper()
	fixture, err := compat.LoadFile(filepath.Join(root, filename))
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func anonymousSource(t *testing.T, fixture compat.Fixture) string {
	t.Helper()
	for _, source := range fixture.Source {
		if source.Path == "anonymous.apex" {
			if len(fixture.Command.Args) != 1 || fixture.Command.Args[0] != source.Content {
				t.Fatalf("%s source and command differ", fixture.Name)
			}
			return source.Content
		}
	}
	t.Fatalf("%s has no anonymous.apex", fixture.Name)
	return ""
}

func assertDeployableClassNames(t *testing.T, fixture compat.Fixture) {
	t.Helper()
	classes := 0
	for _, source := range fixture.Source {
		if !strings.HasSuffix(source.Path, ".cls") {
			continue
		}
		classes++
		name := strings.TrimSuffix(filepath.Base(source.Path), ".cls")
		if len(name) > 40 || !strings.Contains(source.Content, "class "+name+" ") {
			t.Fatalf("%s class/file contract = %q", fixture.Name, name)
		}
	}
	if classes != 1 {
		t.Fatalf("%s deployable class count = %d", fixture.Name, classes)
	}
}

func sourceContent(fixture compat.Fixture, want string) string {
	for _, source := range fixture.Source {
		if source.Path == want {
			return source.Content
		}
	}
	return ""
}

func secondBatchSurfaceIDs(fixture compat.Fixture) []string {
	ids := make([]string, len(fixture.Evidence))
	for i, evidence := range fixture.Evidence {
		ids[i] = evidence.SurfaceID
	}
	return ids
}

func secondBatchSourceContents(fixture compat.Fixture) []string {
	contents := make([]string, len(fixture.Source))
	for i, source := range fixture.Source {
		contents[i] = source.Content
	}
	return contents
}
