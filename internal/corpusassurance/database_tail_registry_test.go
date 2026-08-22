package corpusassurance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func TestDatabaseTailHasExactExecutableLocalOwners(t *testing.T) {
	want := []string{
		"apex:System.Database.deleteAsync(List<Object>,DataSource.AsyncDeleteCallback)",
		"apex:System.Database.deleteAsync(List<Object>,DataSource.AsyncDeleteCallback,AccessLevel)",
		"apex:System.Database.deleteAsync(Object,DataSource.AsyncDeleteCallback,AccessLevel)",
		"apex:System.Database.deleteImmediate(List<Object>,Object)",
		"apex:System.Database.deleteImmediate(Object,Object)",
		"apex:System.Database.getCursorWithBinds(String,Map,Object)",
		"apex:System.Database.getPaginationCursorWithBinds(String,Map,Object)",
		"apex:System.Database.insertAsync(List<Object>,DataSource.AsyncSaveCallback)",
		"apex:System.Database.insertAsync(List<Object>,DataSource.AsyncSaveCallback,AccessLevel)",
		"apex:System.Database.insertAsync(Object,DataSource.AsyncSaveCallback,AccessLevel)",
		"apex:System.Database.insertImmediate(List<Object>,Object)",
		"apex:System.Database.insertImmediate(Object,Object)",
		"apex:System.Database.updateAsync(List<Object>,DataSource.AsyncSaveCallback)",
		"apex:System.Database.updateAsync(List<Object>,DataSource.AsyncSaveCallback,AccessLevel)",
		"apex:System.Database.updateAsync(Object,DataSource.AsyncSaveCallback,AccessLevel)",
		"apex:System.Database.updateImmediate(List<Object>,Object)",
		"apex:System.Database.updateImmediate(Object,Object)",
	}
	wantOwners := map[string][]string{
		"async-database-list-dml-contracts":          {want[0], want[1], want[7], want[8], want[12], want[13]},
		"async-datasource-callback-contracts":        {want[2], want[9], want[14]},
		"data-platform-database-async-immediate-dml": {want[3], want[4], want[10], want[11], want[15], want[16]},
		"data-platform-database-cursor-sync":         {want[5], want[6]},
	}
	type aliasWitness struct {
		owner string
		typed string
		call  string
	}
	aliases := map[string]aliasWitness{
		want[3]:  {"data-platform-database-async-immediate-dml", "apex:System.Database.deleteImmediate(List<Object>,AccessLevel)", "Database.deleteImmediate(new List<Account>{new Account(Id = listRows[0].Id), new Account(Id = listRows[1].Id)}, AccessLevel.SYSTEM_MODE)"},
		want[4]:  {"data-platform-database-async-immediate-dml", "apex:System.Database.deleteImmediate(Object,AccessLevel)", "Database.deleteImmediate(asyncUpdate, AccessLevel.SYSTEM_MODE)"},
		want[5]:  {"data-platform-database-cursor-sync", "apex:System.Database.getCursorWithBinds(String,Map,AccessLevel)", "Database.getCursorWithBinds('SELECT Id, Name FROM Account WHERE Rating = :rating ORDER BY Name', binds, AccessLevel.SYSTEM_MODE)"},
		want[6]:  {"data-platform-database-cursor-sync", "apex:System.Database.getPaginationCursorWithBinds(String,Map,AccessLevel)", "Database.getPaginationCursorWithBinds('SELECT Id, Name FROM Account WHERE Rating = :rating ORDER BY Name', binds, AccessLevel.SYSTEM_MODE)"},
		want[10]: {"data-platform-database-async-immediate-dml", "apex:System.Database.insertImmediate(List<Object>,AccessLevel)", "Database.insertImmediate(new List<Account>{new Account(Name = 'Immediate List A'), new Account(Name = 'Immediate List B')}, AccessLevel.SYSTEM_MODE)"},
		want[11]: {"data-platform-database-async-immediate-dml", "apex:System.Database.insertImmediate(Object,AccessLevel)", "Database.insertImmediate(immediate, AccessLevel.SYSTEM_MODE)"},
		want[15]: {"data-platform-database-async-immediate-dml", "apex:System.Database.updateImmediate(List<Object>,AccessLevel)", "Database.updateImmediate(new List<Account>{new Account(Id = listRows[0].Id, Name = 'Immediate List A Updated'), new Account(Id = listRows[1].Id, Name = 'Immediate List B Updated')}, AccessLevel.SYSTEM_MODE)"},
		want[16]: {"data-platform-database-async-immediate-dml", "apex:System.Database.updateImmediate(Object,AccessLevel)", "Database.updateImmediate(immediateUpdate, AccessLevel.SYSTEM_MODE)"},
	}
	if len(aliases) != 8 {
		t.Fatalf("alias rows = %d, want 8", len(aliases))
	}
	targets := make(map[string]bool, len(want))
	for _, surfaceID := range want {
		targets[surfaceID] = true
		if !localProofCommandMatchesDisposition(localRuntimeRequired, "test", surfaceID) {
			t.Errorf("test-kind planner admission missing for %s", surfaceID)
		}
	}
	root, err := filepath.Abs(filepath.Join("..", "..", "docs", "fixtures"))
	if err != nil {
		t.Fatal(err)
	}
	items, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]string{}
	for _, item := range items {
		if item.IsDir() || !strings.HasSuffix(item.Name(), ".json") {
			continue
		}
		path := filepath.Join(root, item.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		fixture, metadata, err := decodeLocalProofFixtureWithMetadata(data)
		if err != nil || fixture.Command.Kind != "test" || compat.Validate(fixture) != nil {
			continue
		}
		got := []string{}
		for _, evidence := range fixture.Evidence {
			if targets[evidence.SurfaceID] {
				got = append(got, evidence.SurfaceID)
			}
		}
		if len(got) == 0 {
			continue
		}
		expected, ok := wantOwners[fixture.Name]
		if !ok || metadata.Eligible == nil || *metadata.Eligible || metadata.ExclusionClass != "policy-local-only" {
			t.Fatalf("unexpected executable owner %s", fixture.Name)
		}
		sort.Strings(got)
		sort.Strings(expected)
		if !reflect.DeepEqual(got, expected) {
			t.Fatalf("%s owns %v, want %v", fixture.Name, got, expected)
		}
		code := fixture.Source[len(fixture.Source)-1].Content
		if strings.Contains(code, "(Object)AccessLevel") {
			t.Fatalf("%s uses a forbidden Object cast", fixture.Name)
		}
		evidence := make(map[string]compat.FixtureEvidence, len(fixture.Evidence))
		for _, item := range fixture.Evidence {
			evidence[item.SurfaceID] = item
		}
		for alias, witness := range aliases {
			if witness.owner != fixture.Name {
				continue
			}
			if evidence[alias].SurfaceID == "" || evidence[witness.typed].SurfaceID == "" || !strings.Contains(evidence[alias].Notes, "system-fixture-alias") || !strings.Contains(code, witness.call) {
				t.Fatalf("%s alias mapping %s -> %s lacks co-owned evidence or typed call", fixture.Name, alias, witness.typed)
			}
		}
		var envelope struct {
			Candidate struct {
				Commit string `json:"commit"`
				SHA256 string `json:"sha256"`
			} `json:"candidate"`
			SalesforceExclusionReason string `json:"salesforceExclusionReason"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.Candidate.Commit != "86ec4226e33f205bf7a42f6f00cc40aa57fc11b5" || envelope.Candidate.SHA256 != "0aa758618a8908550aa468c4c9eabd1fcdd06f9f6a7d317ccce45a077380d29a" || !strings.Contains(strings.ToLower(envelope.SalesforceExclusionReason), "zero salesforce parity") {
			t.Fatalf("%s provenance = %#v", fixture.Name, envelope)
		}
		if result, err := compat.Run(fixture); err != nil || !result.OK {
			t.Fatalf("%s execution = %#v, error = %v", fixture.Name, result, err)
		}
		for _, surfaceID := range got {
			if prior := seen[surfaceID]; prior != "" {
				t.Fatalf("duplicate executable owners for %s: %s and %s", surfaceID, prior, fixture.Name)
			}
			seen[surfaceID] = fixture.Name
		}
	}
	if len(seen) != len(want) {
		t.Fatalf("owned rows = %d, want %d", len(seen), len(want))
	}
	required := make(map[string]string, len(want))
	for _, surfaceID := range want {
		required[surfaceID] = localRuntimeRequired
	}
	manifest, missing, err := analyzeLocalProofFixtures(root, required)
	if err != nil {
		t.Fatal(err)
	}
	planned := map[string]bool{}
	for _, fixture := range manifest.Fixtures {
		for _, surfaceID := range fixture.OwnedSurfaceIDs {
			planned[surfaceID] = true
		}
	}
	if len(missing) != 0 || len(planned) != len(want) {
		t.Fatalf("fixed-scope planner owned %d rows with missing %v, want exact %d", len(planned), missing, len(want))
	}
}
