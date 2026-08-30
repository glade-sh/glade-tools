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
		if filename == "data-database-deleted-window-local-only-api67.json" {
			for _, token := range []string{"Datetime deletedAt = deleted.getDeletedDate()", "deletedAt >= windowStart.addMinutes(-1)", "deletedAt <= windowEnd"} {
				if !strings.Contains(fixture.Source[0].Content, token) {
					t.Fatalf("%s source missing Datetime witness %q", filename, token)
				}
			}
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

func TestDatabaseDeletionFixturesUseDatetime(t *testing.T) {
	root := filepath.Join("..", "..", "docs", "fixtures")
	for _, test := range []struct {
		filename string
		require  []string
		reject   []string
	}{
		{
			filename: "data-platform-database-date-return-types.json",
			require:  []string{"Datetime deletedDate = deletedRecord.getDeletedDate()", "deletedDate >= deletedWindowStart.addMinutes(-1)", "deletedDate <= deletedWindowEnd"},
			reject:   []string{"Date deletedDate = deletedRecord.getDeletedDate()"},
		},
		{
			filename: "data-platform-database-deleted-sync-window.json",
			require:  []string{"Datetime deletedDate = deleted.getDeletedDate()", "deletedDate >= startWindow.addMinutes(-1)", "deletedDate <= endWindow"},
			reject:   []string{"Date deletedDate = deleted.getDeletedDate()", "Datetime.newInstanceGmt(2026, 5, 2"},
		},
	} {
		t.Run(test.filename, func(t *testing.T) {
			fixture, err := compat.LoadFile(filepath.Join(root, test.filename))
			if err != nil {
				t.Fatal(err)
			}
			if err := compat.Validate(fixture); err != nil {
				t.Fatal(err)
			}
			if result, err := compat.Run(fixture); err != nil || !result.OK {
				t.Fatalf("local execution = %#v, error = %v", result, err)
			}
			source := fixture.Source[0].Content
			for _, token := range test.require {
				if !strings.Contains(source, token) {
					t.Fatalf("source missing Datetime witness %q", token)
				}
			}
			for _, token := range test.reject {
				if strings.Contains(source, token) {
					t.Fatalf("source retains stale Date witness %q", token)
				}
			}
		})
	}
}

func TestDatabaseSystemModeFixturesUseDeployableTests(t *testing.T) {
	root := filepath.Join("..", "..", "docs", "fixtures")
	splits := map[string]struct {
		anonymousRows int
		testFixture   string
		testRows      int
	}{
		"data-database-cursor-runtime-depth.json":           {2, "data-database-cursor-system-mode-runtime-depth.json", 1},
		"data-database-delete-undelete-id-runtime.json":     {8, "data-database-delete-undelete-id-system-mode-runtime.json", 8},
		"data-database-delete-undelete-object-runtime.json": {6, "data-database-delete-undelete-object-system-mode-runtime.json", 4},
		"data-database-dml-accesslevel-runtime.json":        {4, "data-database-dml-system-mode-runtime.json", 3},
		"data-database-query-binds-runtime.json":            {1, "data-database-query-binds-system-mode-runtime.json", 1},
		"data-database-query-locator-access-runtime.json":   {8, "data-database-query-locator-access-system-mode-runtime.json", 3},
		"data-database-query-locator-modes-runtime.json":    {5, "data-database-query-locator-modes-system-mode-runtime.json", 2},
	}
	upserts := []string{
		"core-database-upsert-list-object-accesslevel-runtime.json",
		"core-database-upsert-list-object-boolean-accesslevel-runtime.json",
		"core-database-upsert-object-accesslevel-runtime.json",
		"core-database-upsert-object-boolean-accesslevel-runtime.json",
	}

	for anonymousFixture, split := range splits {
		t.Run(anonymousFixture, func(t *testing.T) {
			anonymous := loadDatabaseContextFixture(t, root, anonymousFixture, "exec", split.anonymousRows)
			if strings.Contains(anonymous.Command.Args[0], "AccessLevel.SYSTEM_MODE") {
				t.Fatal("anonymous Apex retains SYSTEM_MODE")
			}
			systemMode := loadDatabaseContextFixture(t, root, split.testFixture, "test", split.testRows)
			if len(systemMode.Command.Args) != 0 || !deployableSystemModeTest(systemMode) {
				t.Fatalf("SYSTEM_MODE fixture is not one deployable Apex test class: %#v", systemMode)
			}
			owners := map[string]string{}
			for _, fixture := range []compat.Fixture{anonymous, systemMode} {
				for _, evidence := range fixture.Evidence {
					if previous := owners[evidence.SurfaceID]; previous != "" {
						t.Fatalf("surface %s owned by both %s and %s", evidence.SurfaceID, previous, fixture.Name)
					}
					owners[evidence.SurfaceID] = fixture.Name
				}
			}
			if len(owners) != split.anonymousRows+split.testRows {
				t.Fatalf("combined surface owners = %d, want %d", len(owners), split.anonymousRows+split.testRows)
			}
		})
	}

	for _, filename := range upserts {
		t.Run(filename, func(t *testing.T) {
			fixture := loadDatabaseContextFixture(t, root, filename, "test", 1)
			if !deployableSystemModeTest(fixture) {
				t.Fatalf("upsert fixture is not one deployable Apex test class: %#v", fixture)
			}
		})
	}
}

func deployableSystemModeTest(fixture compat.Fixture) bool {
	classes := 0
	for _, source := range fixture.Source {
		if strings.HasSuffix(source.Path, "Test.cls") {
			classes++
			if !strings.Contains(source.Content, "@isTest") || !strings.Contains(source.Content, "AccessLevel.SYSTEM_MODE") {
				return false
			}
		}
	}
	return classes == 1
}

func loadDatabaseContextFixture(t *testing.T, root, filename, kind string, rows int) compat.Fixture {
	t.Helper()
	fixture, err := compat.LoadFile(filepath.Join(root, filename))
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Command.Kind != kind || len(fixture.Evidence) != rows {
		t.Fatalf("fixture contract = %s/%d rows, want %s/%d", fixture.Command.Kind, len(fixture.Evidence), kind, rows)
	}
	for _, evidence := range fixture.Evidence {
		if evidence.Kind != kind {
			t.Fatalf("surface %s evidence kind = %q, want %q", evidence.SurfaceID, evidence.Kind, kind)
		}
		if kind == "test" && !localProofCommandMatchesDisposition(localRuntimeRequired, kind, evidence.SurfaceID) {
			t.Fatalf("surface %s is not admitted as test-context local proof", evidence.SurfaceID)
		}
	}
	data, err := os.ReadFile(filepath.Join(root, filename))
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
	return fixture
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
			rows:     4,
			require:  []string{"accountType.newSObject(accountId)", "Schema.describeSObjects"},
			reject:   []string{"Apex.EmptyStackException", "new Schema.Schema()", "fieldSets.get('GladeAbsentFieldSet')"},
		},
		{
			filename: "core-runtime-schema-sobjecttypefields-get-api67.json",
			rows:     1,
			require:  []string{"Account.SObjectType.getDescribe().fields.get('Name')"},
			reject:   []string{"fields.getMap().get('Name')"},
		},
		{
			filename: "core-runtime-database-cursor-sync-tail-api67.json",
			rows:     8,
			require:  []string{"WHERE Id IN :accountIds", "Datetime start = Datetime.now();", "getEarliestDateAvailable()", "getLatestDateCovered()"},
			reject:   []string{"new Database.Cursor()", "Database.Cursor.DeleteFilter", "SELECT Id, Name FROM Account ORDER BY Name", "Datetime.now().addDays(-1)", "assertEquals(0, deleted.getDeletedRecords().size())", "assertEquals(null, deleted.getEarliestDateAvailable())", "earliest <= latest"},
		},
		{
			filename: "core-dml-exception-accessors-runtime.json",
			rows:     8,
			require:  []string{"System.assertEquals(Account.Name, e.getDmlFields(0).get(0))"},
			reject:   []string{"String.valueOf(dmlField)"},
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
			require:  []string{"Database.GetDeletedResult", "if (candidate.getId() == accountId)", "Datetime deletedAt = deleted.getDeletedDate()", "deletedAt >= windowStart.addMinutes(-1)", "deletedAt <= windowEnd"},
			reject:   []string{"System.Database.GetDeletedResult", "System.Database.DeletedRecord", "getDeletedRecords().size())", "Date.today(), deleted.getDeletedDate()"},
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
				Eligible        *bool  `json:"salesforceEligible"`
				ExclusionClass  string `json:"salesforceExclusionClass"`
				ExclusionReason string `json:"salesforceExclusionReason"`
			}
			if err := json.Unmarshal(data, &metadata); err != nil {
				t.Fatal(err)
			}
			if test.filename == "core-runtime-schema-sobjecttypefields-get-api67.json" {
				if metadata.Eligible == nil || *metadata.Eligible || metadata.ExclusionClass != "policy-local-only" || metadata.ExclusionReason != "Salesforce API 67 rejects direct Schema.SObjectTypeFields.get(String); this local exec witness is policy-local-only and grants zero Salesforce parity credit." {
					t.Fatalf("local-only API-67 rejection policy = %#v", metadata)
				}
			} else if metadata.Eligible == nil || !*metadata.Eligible {
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
