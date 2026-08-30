package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

var databaseRuntimeTailFixtures = map[string][]string{
	"core-runtime-database-async-result-tail-local-api67.json": {
		"apex:System.Database.getAsyncLocator",
		"apex:System.Database.getAsyncLocator(Object)",
		"apex:System.Database.getAsyncSaveResult(String)",
		"apex:System.Database.getAsyncDeleteResult(String)",
	},
	"core-runtime-database-cursor-runtime-tail-local-api67.json": {
		"apex:System.Database.getCursor",
		"apex:System.Database.getCursor(String,Object)",
		"apex:System.Database.getCursorWithBinds",
		"apex:System.Database.getDeleted",
		"apex:System.Database.getPaginationCursor(String,Object)",
		"apex:System.Database.getUpdated",
	},
	"core-runtime-database-querylocator-runtime-tail-local-api67.json": {
		"apex:System.Database.getQueryLocatorWithBinds",
	},
	"core-runtime-database-undelete-runtime-tail-local-api67.json": {
		"apex:System.Database.undelete",
	},
}

var databaseRuntimeTailRejectedIDs = []string{
	"apex:System.Database.deleteAsync(List<Object>,DataSource.AsyncDeleteCallback)",
	"apex:System.Database.deleteAsync(List<Object>,DataSource.AsyncDeleteCallback,AccessLevel)",
	"apex:System.Database.deleteAsync(Object,DataSource.AsyncDeleteCallback,AccessLevel)",
	"apex:System.Database.insertAsync(List<Object>,DataSource.AsyncSaveCallback)",
	"apex:System.Database.insertAsync(List<Object>,DataSource.AsyncSaveCallback,AccessLevel)",
	"apex:System.Database.insertAsync(Object,DataSource.AsyncSaveCallback,AccessLevel)",
	"apex:System.Database.updateAsync(List<Object>,DataSource.AsyncSaveCallback)",
	"apex:System.Database.updateAsync(List<Object>,DataSource.AsyncSaveCallback,AccessLevel)",
	"apex:System.Database.updateAsync(Object,DataSource.AsyncSaveCallback,AccessLevel)",
	"apex:System.Database.deleteImmediate(List<Object>,Object)",
	"apex:System.Database.deleteImmediate(Object,Object)",
	"apex:System.Database.insertImmediate(List<Object>,Object)",
	"apex:System.Database.insertImmediate(Object,Object)",
	"apex:System.Database.updateImmediate(List<Object>,Object)",
	"apex:System.Database.updateImmediate(Object,Object)",
	"apex:System.Database.getCursorWithBinds(String,Map,Object)",
	"apex:System.Database.getPaginationCursorWithBinds(String,Map,Object)",
	"apex:System.Database.getQueryLocator(List,Object)",
	"apex:System.Database.getQueryLocator(Object)",
	"apex:System.Database.getQueryLocator(Object,AccessLevel)",
}

var databaseRuntimeTailObjectOverloadWitnesses = map[string]struct {
	exact  []string
	forbid []string
}{
	"core-runtime-database-cursor-runtime-tail-local-api67.json": {
		exact: []string{
			"Database.Cursor cursor = Database.getCursor('SELECT Id FROM Account', (Object)AccessLevel.SYSTEM_MODE);",
			"Database.PaginationCursor page = Database.getPaginationCursor('SELECT Id FROM Account', (Object)AccessLevel.SYSTEM_MODE);",
		},
		forbid: []string{
			"Database.getCursor('SELECT Id FROM Account', AccessLevel.SYSTEM_MODE)",
			"Database.getPaginationCursor('SELECT Id FROM Account', AccessLevel.SYSTEM_MODE)",
		},
	},
}

func TestQueryLocatorRuntimeUsesInlineSOQL(t *testing.T) {
	fixture, err := compat.LoadFile(filepath.Join("..", "..", "docs", "fixtures", "data-database-query-locator-modes-runtime.json"))
	if err != nil {
		t.Fatal(err)
	}
	source := fixture.Command.Args[0]
	if exact := "Database.QueryLocator listLocator = Database.getQueryLocator([SELECT Id, Name, Rating FROM Account ORDER BY Name]);"; !strings.Contains(source, exact) {
		t.Fatalf("inline SOQL witness missing: %s", exact)
	}
	for _, invalid := range []string{"Database.getQueryLocator(sourceRows)", "Database.getQueryLocator(accessRows"} {
		if strings.Contains(source, invalid) {
			t.Fatalf("query locator uses rejected list variable: %s", invalid)
		}
	}
	systemMode, err := compat.LoadFile(filepath.Join("..", "..", "docs", "fixtures", "data-database-query-locator-modes-system-mode-runtime.json"))
	if err != nil {
		t.Fatal(err)
	}
	exact := "Database.QueryLocator locator = Database.getQueryLocator([SELECT Id, Name, Rating FROM Account WHERE Name = 'Hot Account'], AccessLevel.SYSTEM_MODE);"
	if len(systemMode.Source) != 1 || !strings.Contains(systemMode.Source[0].Content, exact) {
		t.Fatalf("SYSTEM_MODE inline SOQL test witness missing: %s", exact)
	}
}

func TestDatabaseRuntimeTailHasExactCandidateRowsAndRejectGuards(t *testing.T) {
	root := filepath.Join("..", "..")
	want := map[string]bool{}
	for _, ids := range databaseRuntimeTailFixtures {
		for _, id := range ids {
			if !strings.HasPrefix(id, "apex:System.Database") || strings.HasPrefix(id, "apex:Database.") {
				t.Fatalf("non-System.Database candidate row: %s", id)
			}
			want[id] = true
		}
	}
	rejected := map[string]bool{}
	for _, id := range databaseRuntimeTailRejectedIDs {
		if want[id] {
			t.Fatalf("rejected row admitted: %s", id)
		}
		rejected[id] = true
	}

	for filename, ids := range databaseRuntimeTailFixtures {
		path := filepath.Join(root, "docs", "fixtures", filename)
		fixture, err := compat.LoadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := compat.Validate(fixture); err != nil {
			t.Fatal(err)
		}
		if fixture.Name != strings.TrimSuffix(filename, ".json") || fixture.Command.Kind != "exec" || len(fixture.Command.Args) != 1 || len(fixture.Source) != 1 || fixture.Source[0].Content != fixture.Command.Args[0] {
			t.Fatalf("fixture envelope %s = %#v", filename, fixture)
		}
		if witness, ok := databaseRuntimeTailObjectOverloadWitnesses[filename]; ok {
			for _, exact := range witness.exact {
				if !strings.Contains(fixture.Command.Args[0], exact) {
					t.Fatalf("object overload witness missing from %s: %s", filename, exact)
				}
			}
			for _, forbidden := range witness.forbid {
				if strings.Contains(fixture.Command.Args[0], forbidden) {
					t.Fatalf("typed AccessLevel misbinding remains in %s: %s", filename, forbidden)
				}
			}
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var metadata struct {
			APIVersion         string `json:"apiVersion"`
			Mode               string `json:"mode"`
			EvidenceOnly       bool   `json:"evidenceOnly"`
			SalesforceEligible *bool  `json:"salesforceEligible"`
			ExclusionClass     string `json:"salesforceExclusionClass"`
			ExclusionReason    string `json:"salesforceExclusionReason"`
			Notes              string `json:"notes"`
			Candidate          struct {
				Commit string `json:"commit"`
				SHA256 string `json:"sha256"`
			} `json:"candidate"`
			Profile struct {
				CandidateCommit string `json:"candidateCommit"`
				CandidateSHA256 string `json:"candidateSha256"`
				SelectedRows    int    `json:"selectedRowCount"`
			} `json:"profile"`
		}
		if err := json.Unmarshal(data, &metadata); err != nil {
			t.Fatal(err)
		}
		if metadata.APIVersion != "67.0" || metadata.Mode != "local-runtime" || metadata.EvidenceOnly || metadata.SalesforceEligible == nil || *metadata.SalesforceEligible || metadata.ExclusionClass != "policy-local-only" || !strings.Contains(metadata.ExclusionReason, "zero hosted Salesforce parity") || metadata.Profile.CandidateCommit != "3409c4c85827b19712e9df83fc8905aa02bd1dc8" || metadata.Profile.CandidateSHA256 != "960ac9f26fa92aae6054cbe0e59f9c4ab1f84397df67bd8a89528068d02a1fce" || metadata.Profile.SelectedRows != len(ids) {
			t.Fatalf("fixture provenance %s = %#v", filename, metadata)
		}
		seen := map[string]bool{}
		for _, evidence := range fixture.Evidence {
			if evidence.Kind != "exec" || !want[evidence.SurfaceID] || seen[evidence.SurfaceID] {
				t.Fatalf("unexpected evidence in %s: %#v", filename, evidence)
			}
			seen[evidence.SurfaceID] = true
		}
		if len(seen) != len(ids) {
			t.Fatalf("evidence rows in %s = %d, want %d", filename, len(seen), len(ids))
		}
		if result, err := compat.Run(fixture); err != nil || !result.OK {
			t.Fatalf("fixture execution %s = %#v, error = %v", filename, result, err)
		}
	}

	paths, err := filepath.Glob(filepath.Join(root, "docs", "fixtures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	owners := map[string]int{}
	for _, path := range paths {
		var header struct {
			EvidenceOnly bool `json:"evidenceOnly"`
			Evidence     []struct {
				SurfaceID string `json:"surfaceId"`
			} `json:"evidence"`
		}
		readJSON(t, path, &header)
		if header.EvidenceOnly {
			continue
		}
		for _, row := range header.Evidence {
			if rejected[row.SurfaceID] && strings.Contains(row.SurfaceID, "getQueryLocator") {
				t.Fatalf("rejected row has active fixture ownership: %s", row.SurfaceID)
			}
			if want[row.SurfaceID] {
				owners[row.SurfaceID]++
			}
		}
	}
	for id := range want {
		if owners[id] == 0 {
			t.Fatalf("active fixture ownership missing for %s", id)
		}
	}
}
