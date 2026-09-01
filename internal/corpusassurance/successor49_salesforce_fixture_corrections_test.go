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
			require: []string{`String htmlValue = '<tag attr=\'x\'>&';`, `System.assertEquals('a\\/b', slash.escapeEcmaScript());`, `String.escapeSingleQuotes('Bob\'s')`, "String expectedEscapedOmega = 'A' + String.fromCharArray(new List<Integer>{92}) + 'u03A9';", "System.assertEquals(expectedEscapedOmega, omega.escapeUnicode());", "System.assertEquals(omega, escapedOmega.unescapeUnicode());"},
			reject:  []string{"attr=''x''", "String.escapeSingleQuotes('Bob''s')", `System.assertEquals('A\u03A9', omega.escapeUnicode());`, "System.assertEquals(escapedOmega, escapedOmega.unescapeUnicode());"},
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

func TestUnicodeUnescapeFixturesMatchSalesforceObservedBehavior(t *testing.T) {
	root := filepath.Join("..", "..", "docs", "fixtures")
	want := map[string]string{
		"core-runtime-string-encoding-rewrite-depth": "System.assertEquals(omega,escapedOmega.unescapeUnicode());",
		"core-string-completion-stdlib":              "System.assertEquals(omega, escapedOmega.unescapeUnicode());",
		"core-string-final-families-stdlib":          "System.assertEquals(unicodeRaw, unicodeEscaped.unescapeUnicode());",
	}
	wantEscapedConstruction := map[string]string{
		"core-runtime-string-encoding-rewrite-depth": "String expectedEscapedOmega='A'+String.fromCharArray(new List<Integer>{92})+'u03A9';",
		"core-string-completion-stdlib":              "String expectedEscapedOmega = 'A' + String.fromCharArray(new List<Integer>{92}) + 'u03A9';",
		"core-string-final-families-stdlib":          "String expectedUnicodeEscaped = String.fromCharArray(new List<Integer>{92,117,48,48,48,56,92,117,48,48,48,57,92,117,48,48,48,65,92,117,48,48,48,67,92,117,48,48,48,68,34,39,47,92,117,48,51,65,57,92,117,68,56,51,68,92,117,68,69,48,48});",
	}
	found := map[string]bool{}
	paths, err := filepath.Glob(filepath.Join(root, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var fixture struct {
			Name     string `json:"name"`
			Evidence []struct {
				SurfaceID string `json:"surfaceId"`
				Symbol    string `json:"symbol"`
				Notes     string `json:"notes"`
			} `json:"evidence"`
			Source []struct {
				Content string `json:"content"`
			} `json:"source"`
		}
		if err := json.Unmarshal(data, &fixture); err != nil {
			t.Fatalf("load %s: %v", path, err)
		}
		for _, source := range fixture.Source {
			if !strings.Contains(source.Content, ".unescapeUnicode()") {
				continue
			}
			expectation, ok := want[fixture.Name]
			if !ok {
				t.Fatalf("unaccounted executable unescapeUnicode fixture %s", fixture.Name)
			}
			found[fixture.Name] = true
			if !strings.Contains(source.Content, expectation) {
				t.Errorf("%s does not match its Salesforce-observed Unicode expectation", fixture.Name)
			}
			if !strings.Contains(source.Content, wantEscapedConstruction[fixture.Name]) {
				t.Errorf("%s does not construct escaped Unicode text from character codes", fixture.Name)
			}
			if strings.Contains(source.Content, ".escapeUnicode().unescapeUnicode()") {
				t.Errorf("%s still assumes Unicode escape round-trip decoding", fixture.Name)
			}
		}
		for _, evidence := range fixture.Evidence {
			if evidence.Symbol != "String.unescapeUnicode" && !strings.Contains(evidence.SurfaceID, "String.unescapeUnicode") {
				continue
			}
			notes := strings.ToLower(evidence.Notes)
			if _, executableOwner := want[fixture.Name]; executableOwner && !strings.Contains(notes, "decode") {
				t.Errorf("%s lacks decoded unescapeUnicode notes %q", fixture.Name, evidence.Notes)
			}
		}
	}
	if !reflect.DeepEqual(found, map[string]bool{
		"core-runtime-string-encoding-rewrite-depth": true,
		"core-string-completion-stdlib":              true,
		"core-string-final-families-stdlib":          true,
	}) {
		t.Fatalf("executable unescapeUnicode fixtures = %v", found)
	}
}

func TestStringFinalFamiliesExcludesAPIVersionRemovedXmlMethods(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "fixtures", "core-string-final-families-stdlib.json")
	fixture, err := compat.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	for _, evidence := range fixture.Evidence {
		if evidence.Symbol == "String.escapeXml10" || evidence.Symbol == "String.escapeXml11" {
			t.Errorf("fixture retains removed API 67 member %s", evidence.Symbol)
		}
	}
	if len(fixture.Source) != 1 || len(fixture.Command.Args) != 1 || fixture.Source[0].Content != fixture.Command.Args[0] {
		t.Fatal("source and command differ")
	}
	for _, call := range []string{".escapeXml10()", ".escapeXml11()"} {
		if strings.Contains(fixture.Source[0].Content, call) {
			t.Errorf("fixture retains removed API 67 call %s", call)
		}
	}
	required := map[string]string{
		"apex:System.String.escapeXml10": localRuntimeRequired,
		"apex:System.String.escapeXml11": localRuntimeRequired,
	}
	root, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	manifest, missing, err := analyzeLocalProofFixtures(root, required)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Fixtures) != 0 || !reflect.DeepEqual(missing, []string{"apex:System.String.escapeXml10", "apex:System.String.escapeXml11"}) {
		t.Fatalf("legacy XML runtime plan = %#v, missing = %v", manifest.Fixtures, missing)
	}
}

func TestStringEntityNegativeAuthorityRuns(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "fixtures", "current-base-string-entity-negative-api67.json")
	fixture, err := compat.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		`unsupported call "value.escapeXml10"`,
		`unsupported call "value.escapeXml11"`,
		`unsupported call "value.unescapeXml10"`,
		`unsupported call "value.unescapeXml11"`,
	}
	if fixture.Expected.Error == nil || fixture.Expected.Error.Type != "UnsupportedFeature" || fixture.Expected.Error.Message != want[0] || len(fixture.Command.Args) != len(want) {
		t.Fatalf("negative authority contract = %#v", fixture.Expected.Error)
	}
	for i, source := range fixture.Command.Args {
		probe := fixture
		probe.Command.Args = []string{source}
		probe.Expected.Error = &compat.ExpectedError{Type: "UnsupportedFeature", Message: want[i]}
		result, err := compat.Run(probe)
		if err != nil {
			t.Fatalf("probe %d: %v", i, err)
		}
		if !result.OK || result.Error == nil || result.Error.Type != "UnsupportedFeature" || result.Error.Message != want[i] {
			t.Fatalf("probe %d result = %#v", i, result)
		}
	}
}
