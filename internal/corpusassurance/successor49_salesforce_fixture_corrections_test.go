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

func TestSuccessor49SalesforceFixtureCorrections(t *testing.T) {
	root := filepath.Join("..", "..", "docs", "fixtures")
	tests := []struct {
		name    string
		kind    string
		rows    int
		require []string
		reject  []string
	}{
		{
			name: "core-runtime-matcher-exact-evidence", kind: "exec", rows: 25,
			require: []string{
				"region = region.region(4, 7);",
				"region = region.useAnchoringBounds(false);",
				"region = region.useTransparentBounds(true);",
				"region = region.usePattern(Pattern.compile('\\\\d+'));",
			},
			reject: []string{"assertNotEquals(region, region.region", "assertNotEquals(region, region.useAnchoringBounds", "assertNotEquals(region, region.useTransparentBounds", "assertNotEquals(region, region.usePattern"},
		},
		{
			name: "core-runtime-system-stdlib-cut3-evidence", kind: "exec", rows: 16,
			require: []string{"detailed.getUserInfo()", "detailed.sameFile(sameFile)"},
			reject:  []string{"URL.getFileFieldURL("},
		},
		{
			name: "core-runtime-url-file-field-local-only-api67", kind: "exec", rows: 1,
			require: []string{"URL.getFileFieldURL('001B000001DVM9tIAH', 'Body')", "fileURL.contains('id=001B000001DVM9t')", "fileURL.contains('field=Body')"},
		},
		{
			name: "core-stdlib-supported-closeout", kind: "test", rows: 18,
			require: []string{"System.assertEquals(12.34, Decimal.valueOf('12.345').setScale(2));", "System.assertEquals(1.3, Decimal.valueOf('1.25').setScale(1, RoundingMode.HALF_UP));"},
			reject:  []string{"System.assertEquals(12.35, Decimal.valueOf('12.345').setScale(2));"},
		},
		{
			name: "core-string-completion-stdlib", kind: "exec", rows: 31,
			require: []string{`String htmlValue = '<tag attr=\'x\'>&';`, `System.assertEquals('a\\/b', slash.escapeEcmaScript());`, `String.escapeSingleQuotes('Bob\'s')`, "System.assertEquals(escapedOmega, escapedOmega.unescapeUnicode());"},
			reject:  []string{"attr=''x''", "String.escapeSingleQuotes('Bob''s')", "System.assertEquals(omega, escapedOmega.unescapeUnicode());"},
		},
		{
			name: "core-string-entity-edge-stdlib", kind: "exec", rows: 11,
			require: []string{`String htmlCoreValue = '"\'<>&';`, "System.assertEquals('xaxbxcx', replaceEmpty.replace('', 'x'));"},
			reject:  []string{`String htmlCoreValue = '"''<>&';`, "System.assertEquals('abc', replaceEmpty.replace('', 'x'));"},
		},
		{
			name: "data-database-delete-undelete-object-runtime", kind: "exec", rows: 6,
			require: []string{"SObject object2 = a6;", "SObject object3 = a7;"},
			reject:  []string{"; Object object2 = a6;", "; Object object3 = a7;"},
		},
		{
			name: "data-database-query-locator-access-runtime", kind: "exec", rows: 8,
			require: []string{"Iterator<SObject> iterator = locator.iterator();", "WHERE Name IN :ownedNames"},
			reject:  []string{"Object iterator = locator.iterator();", "SELECT Id, Name FROM Account', AccessLevel.USER_MODE", "SELECT COUNT() FROM Account');", "SELECT COUNT() FROM Account', AccessLevel.USER_MODE", "SELECT Id, Name FROM Account ORDER BY Name');"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture, err := compat.LoadFile(filepath.Join(root, test.name+".json"))
			if err != nil {
				t.Fatal(err)
			}
			if err := compat.Validate(fixture); err != nil {
				t.Fatal(err)
			}
			if fixture.Command.Kind != test.kind || len(fixture.Evidence) != test.rows {
				t.Fatalf("contract = %s/%d rows, want %s/%d", fixture.Command.Kind, len(fixture.Evidence), test.kind, test.rows)
			}
			var source string
			for _, file := range fixture.Source {
				if file.Path == "anonymous.apex" || strings.HasSuffix(file.Path, ".cls") {
					source = file.Content
				}
			}
			if source == "" {
				t.Fatal("fixture has no executable source")
			}
			if test.kind == "exec" && (len(fixture.Command.Args) != 1 || source != fixture.Command.Args[0]) {
				t.Fatal("source and command differ")
			}
			for _, token := range test.require {
				if !strings.Contains(source, token) {
					t.Fatalf("source missing %q", token)
				}
			}
			for _, token := range test.reject {
				if strings.Contains(source, token) {
					t.Fatalf("source retains %q", token)
				}
			}
		})
	}

	const surfaceID = "apex:System.URL.getFileFieldURL(String,String)"
	const owner = "core-runtime-url-file-field-local-only-api67"
	absRoot, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	required := map[string]string{surfaceID: localRuntimeRequired}
	candidates, err := discoverLocalProofFixtures(absRoot, required)
	if err != nil {
		t.Fatal(err)
	}
	owners := []string{}
	for _, candidate := range candidates {
		if candidate.owned[surfaceID] {
			owners = append(owners, candidate.entry.ID)
		}
	}
	if !reflect.DeepEqual(owners, []string{owner}) {
		t.Fatalf("local-proof owners = %v, want [%s]", owners, owner)
	}
	manifest, missing, err := analyzeLocalProofFixtures(absRoot, required)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 0 || len(manifest.Fixtures) != 1 || manifest.Fixtures[0].ID != owner || !reflect.DeepEqual(manifest.Fixtures[0].OwnedSurfaceIDs, []string{surfaceID}) {
		t.Fatalf("URL local-proof plan = %#v, missing = %v", manifest, missing)
	}
	salesforceFixtures, err := selectSalesforceFixtures(manifest.Fixtures)
	if err != nil {
		t.Fatal(err)
	}
	if len(salesforceFixtures) != 0 {
		t.Fatalf("Salesforce runtime fixtures = %#v, want none", salesforceFixtures)
	}
	data, err := os.ReadFile(filepath.Join(absRoot, owner+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var policy struct {
		EvidenceOnly              *bool  `json:"evidenceOnly"`
		SalesforceEligible        *bool  `json:"salesforceEligible"`
		SalesforceExclusionClass  string `json:"salesforceExclusionClass"`
		SalesforceExclusionReason string `json:"salesforceExclusionReason"`
	}
	if err := json.Unmarshal(data, &policy); err != nil {
		t.Fatal(err)
	}
	if policy.EvidenceOnly == nil || *policy.EvidenceOnly || policy.SalesforceEligible == nil || *policy.SalesforceEligible || policy.SalesforceExclusionClass != "policy-local-only" || !strings.Contains(policy.SalesforceExclusionReason, "zero Salesforce parity") {
		t.Fatalf("URL fixture policy = %#v", policy)
	}
}
