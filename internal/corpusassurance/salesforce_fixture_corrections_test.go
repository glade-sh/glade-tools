package corpusassurance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

var salesforceFixtureLocalOnlyRows = map[string][]string{
	"core-runtime-apex-schema-tail-local-only-api67.json": {
		"apex:Apex.EmptyStackException",
		"apex:Apex.EmptyStackException.EmptyStackException()",
		"apex:Apex.EmptyStackException.EmptyStackException(Exception)",
		"apex:Apex.EmptyStackException.EmptyStackException(String)",
		"apex:Apex.EmptyStackException.EmptyStackException(String,Exception)",
		"apex:Apex.EmptyStackException.clone()",
		"apex:Schema.Schema.Schema()",
	},
	"core-runtime-database-cursor-sync-tail-local-only-api67.json": {
		"apex:Database.Cursor.Cursor()",
		"apex:Database.Cursor.DeleteFilter",
	},
	"core-runtime-database-error-extended-local-only-api67.json": {
		"apex:Database.Error.getExtendedErrorDetails()",
	},
	"data-database-deleted-window-local-only-api67.json": {
		"apex:System.Database.DeletedRecord.getDeletedDate()",
		"apex:System.Database.DeletedRecord.getId()",
		"apex:System.Database.GetDeletedResult.getDeletedRecords()",
	},
}

func TestSalesforceFixtureCorrectionsSeparateThirteenLocalOnlyRows(t *testing.T) {
	root := filepath.Join("..", "..", "docs", "fixtures")
	wantOwner := map[string]string{}
	for filename, ids := range salesforceFixtureLocalOnlyRows {
		path := filepath.Join(root, filename)
		fixture, err := compat.LoadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := compat.Validate(fixture); err != nil {
			t.Fatal(err)
		}
		if len(fixture.Source) != 1 || len(fixture.Command.Args) != 1 || fixture.Source[0].Content != fixture.Command.Args[0] {
			t.Fatalf("%s source and command differ", filename)
		}
		if result, err := compat.Run(fixture); err != nil || !result.OK {
			t.Fatalf("%s local execution = %#v, error = %v", filename, result, err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var metadata struct {
			Eligible        *bool  `json:"salesforceEligible"`
			ExclusionClass  string `json:"salesforceExclusionClass"`
			ExclusionReason string `json:"salesforceExclusionReason"`
			Profile         struct {
				SelectedRows int `json:"selectedRowCount"`
			} `json:"profile"`
		}
		if err := json.Unmarshal(data, &metadata); err != nil {
			t.Fatal(err)
		}
		if metadata.Eligible == nil || *metadata.Eligible || metadata.ExclusionClass != "policy-local-only" || !strings.Contains(metadata.ExclusionReason, "zero Salesforce parity") || metadata.Profile.SelectedRows != len(ids) {
			t.Fatalf("%s local-only policy = %#v", filename, metadata)
		}
		seen := map[string]bool{}
		for _, row := range fixture.Evidence {
			seen[row.SurfaceID] = true
		}
		for _, id := range ids {
			if !seen[id] {
				t.Fatalf("%s does not own %s", filename, id)
			}
			wantOwner[id] = filename
		}
		if len(seen) != len(ids) {
			t.Fatalf("%s evidence rows = %d, want %d", filename, len(seen), len(ids))
		}
	}
	if len(wantOwner) != 13 {
		t.Fatalf("local-only correction rows = %d, want 13", len(wantOwner))
	}

	owners := map[string][]string{}
	paths, err := filepath.Glob(filepath.Join(root, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		var header struct {
			EvidenceOnly bool `json:"evidenceOnly"`
			Evidence     []struct {
				SurfaceID string `json:"surfaceId"`
				Kind      string `json:"kind"`
			} `json:"evidence"`
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &header); err != nil {
			t.Fatal(err)
		}
		if header.EvidenceOnly {
			continue
		}
		for _, row := range header.Evidence {
			if row.Kind == "exec" && wantOwner[row.SurfaceID] != "" {
				owners[row.SurfaceID] = append(owners[row.SurfaceID], filepath.Base(path))
			}
		}
	}
	for id, filename := range wantOwner {
		if len(owners[id]) != 1 || owners[id][0] != filename {
			t.Fatalf("%s owners = %v, want [%s]", id, owners[id], filename)
		}
	}
}

func TestSalesforceFixtureCorrectionsUsePortableIsolatedProbes(t *testing.T) {
	root := filepath.Join("..", "..", "docs", "fixtures")
	tests := []struct {
		filename string
		rows     int
		require  []string
		reject   []string
	}{
		{
			filename: "core-runtime-apex-schema-tail-api67.json",
			rows:     6,
			require:  []string{"accountType.newSObject(accountId)", "Schema.describeSObjects"},
			reject:   []string{"Apex.EmptyStackException", "new Schema.Schema()"},
		},
		{
			filename: "core-runtime-database-cursor-sync-tail-api67.json",
			rows:     8,
			require:  []string{"WHERE Id IN :accountIds", "getEarliestDateAvailable()", "getLatestDateCovered()"},
			reject:   []string{"new Database.Cursor()", "Database.Cursor.DeleteFilter", "SELECT Id, Name FROM Account ORDER BY Name", "assertEquals(0, deleted.getDeletedRecords().size())", "assertEquals(null, deleted.getEarliestDateAvailable())"},
		},
		{
			filename: "core-dml-exception-accessors-runtime.json",
			rows:     8,
			require:  []string{"Object dmlField = e.getDmlFields(0).get(0)", "dmlFieldText.endsWith('.Name')"},
			reject:   []string{"System.assertEquals('Name', e.getDmlFields(0).get(0))"},
		},
		{
			filename: "core-runtime-database-query-locator-api67.json",
			rows:     4,
			require:  []string{"Set<Id> accountIds", "WHERE Id IN :accountIds"},
			reject:   []string{"SELECT Id, Name FROM Account ORDER BY Name"},
		},
		{
			filename: "core-runtime-database-result-error-accessors-api67.json",
			rows:     9,
			require:  []string{"Database.UndeleteResult restored = Database.undelete(record, false)", "Database.UndeleteResult restoreAgain = Database.undelete(record, false)", "System.assert(!restoreAgain.isSuccess())"},
			reject:   []string{"getExtendedErrorDetails()", "JSON.deserialize"},
		},
		{
			filename: "data-database-deleted-window-runtime.json",
			rows:     3,
			require:  []string{"Database.GetDeletedResult", "if (candidate.getId() == accountId)"},
			reject:   []string{"System.Database.GetDeletedResult", "System.Database.DeletedRecord", "getDeletedRecords().size())"},
		},
		{
			filename: "core-blob-crypto-stdlib.json",
			rows:     11,
			require:  []string{"Crypto.generateMac('HmacSHA256', message, key)"},
			reject:   []string{"Crypto.generateMac(' HMAC-SHA256 '", "normalizedHmacSHA256"},
		},
	}
	for _, test := range tests {
		t.Run(test.filename, func(t *testing.T) {
			fixture, err := compat.LoadFile(filepath.Join(root, test.filename))
			if err != nil {
				t.Fatal(err)
			}
			if err := compat.Validate(fixture); err != nil {
				t.Fatal(err)
			}
			if len(fixture.Evidence) != test.rows {
				t.Fatalf("evidence rows = %d, want %d", len(fixture.Evidence), test.rows)
			}
			data, err := os.ReadFile(filepath.Join(root, test.filename))
			if err != nil {
				t.Fatal(err)
			}
			var metadata struct {
				Eligible *bool `json:"salesforceEligible"`
			}
			if err := json.Unmarshal(data, &metadata); err != nil {
				t.Fatal(err)
			}
			if metadata.Eligible == nil || !*metadata.Eligible {
				t.Fatalf("salesforceEligible = %v, want true", metadata.Eligible)
			}
			source := fixture.Command.Args[0]
			for _, token := range test.require {
				if !strings.Contains(source, token) {
					t.Fatalf("source missing portable witness %q", token)
				}
			}
			for _, token := range test.reject {
				if strings.Contains(source, token) {
					t.Fatalf("source retains nonportable or nonisolated witness %q", token)
				}
			}
			if result, err := compat.Run(fixture); err != nil || !result.OK {
				t.Fatalf("local execution = %#v, error = %v", result, err)
			}
		})
	}
}
