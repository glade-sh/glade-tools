package corpusassurance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func TestDatabaseUpsertOverloadsHaveSingletonSalesforceProofFixtures(t *testing.T) {
	root := filepath.Join("..", "..", "docs", "fixtures")
	type expectedFixture struct {
		surfaceID    string
		sourcePrefix string
	}
	want := map[string]expectedFixture{
		"core-database-upsert-object-runtime.json":                          {"apex:System.Database.upsert(Object)", "SObject row = new Account("},
		"core-database-upsert-object-accesslevel-runtime.json":              {"apex:System.Database.upsert(Object,AccessLevel)", "SObject row = new Account("},
		"core-database-upsert-object-boolean-runtime.json":                  {"apex:System.Database.upsert(Object,Boolean)", "SObject row = new Account("},
		"core-database-upsert-object-boolean-accesslevel-runtime.json":      {"apex:System.Database.upsert(Object,Boolean,AccessLevel)", "SObject row = new Account("},
		"core-database-upsert-list-object-runtime.json":                     {"apex:System.Database.upsert(List<Object>)", "List<SObject> rows = new List<SObject>{"},
		"core-database-upsert-list-object-accesslevel-runtime.json":         {"apex:System.Database.upsert(List<Object>,AccessLevel)", "List<SObject> rows = new List<SObject>{"},
		"core-database-upsert-list-object-boolean-runtime.json":             {"apex:System.Database.upsert(List<Object>,Boolean)", "List<SObject> rows = new List<SObject>{"},
		"core-database-upsert-list-object-boolean-accesslevel-runtime.json": {"apex:System.Database.upsert(List<Object>,Boolean,AccessLevel)", "List<SObject> rows = new List<SObject>{"},
	}
	tracked := make(map[string]bool, len(want))
	wantKind := make(map[string]string, len(want))
	for _, expected := range want {
		tracked[expected.surfaceID] = true
		wantKind[expected.surfaceID] = "exec"
		if strings.Contains(expected.surfaceID, "AccessLevel)") {
			wantKind[expected.surfaceID] = "test"
		}
	}

	for file, expected := range want {
		path := filepath.Join(root, file)
		fixture, err := compat.LoadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := compat.Validate(fixture); err != nil {
			t.Fatal(err)
		}
		kind := wantKind[expected.surfaceID]
		if fixture.Command.Kind != kind || len(fixture.Source) != 1 || kind == "exec" && (len(fixture.Command.Args) != 1 || fixture.Source[0].Content != fixture.Command.Args[0]) || kind == "test" && (len(fixture.Command.Args) != 0 || !strings.HasSuffix(fixture.Source[0].Path, "Test.cls")) {
			t.Fatalf("%s command/source = %#v/%#v", file, fixture.Command, fixture.Source)
		}
		if !strings.Contains(fixture.Source[0].Content, expected.sourcePrefix) {
			t.Fatalf("%s source = %q, want %q", file, fixture.Source[0].Content, expected.sourcePrefix)
		}
		if len(fixture.Evidence) != 1 || fixture.Evidence[0].SurfaceID != expected.surfaceID || fixture.Evidence[0].Kind != kind {
			t.Fatalf("%s evidence = %#v, want only %s", file, fixture.Evidence, expected.surfaceID)
		}
		if strings.Count(fixture.Source[0].Content, "Database.upsert(") != 1 {
			t.Fatalf("%s masks multiple overloads: %q", file, fixture.Source[0].Content)
		}
		var policy struct {
			SalesforceEligible        *bool  `json:"salesforceEligible"`
			SalesforceExclusionClass  string `json:"salesforceExclusionClass"`
			SalesforceExclusionReason string `json:"salesforceExclusionReason"`
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &policy); err != nil {
			t.Fatal(err)
		}
		if policy.SalesforceEligible == nil || !*policy.SalesforceEligible || policy.SalesforceExclusionClass != "" || policy.SalesforceExclusionReason != "" {
			t.Fatalf("%s Salesforce policy = %#v", file, policy)
		}
	}

	paths, err := filepath.Glob(filepath.Join(root, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	owners := make(map[string]int, len(tracked))
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
				Kind      string `json:"kind"`
			} `json:"evidence"`
		}
		if err := json.Unmarshal(data, &header); err != nil {
			t.Fatal(err)
		}
		if header.EvidenceOnly || header.SalesforceEligible == nil || !*header.SalesforceEligible {
			continue
		}
		for _, row := range header.Evidence {
			if row.Kind == wantKind[row.SurfaceID] && tracked[row.SurfaceID] {
				owners[row.SurfaceID]++
			}
		}
	}
	for id := range tracked {
		if owners[id] != 1 {
			t.Fatalf("Salesforce fixture ownership for %s = %d, want exactly one", id, owners[id])
		}
	}
}
