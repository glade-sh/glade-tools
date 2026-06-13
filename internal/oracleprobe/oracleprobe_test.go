package oracleprobe

import (
	"strings"
	"testing"
)

func TestRenderAnonymousIncludesMarkerStatementsAndCases(t *testing.T) {
	source := RenderAnonymous([]Case{{
		ID:         "decimal-round-half-up",
		Area:       "Decimal",
		API:        "Decimal.round",
		Mode:       ModeAnonymous,
		Statements: []string{"Decimal gladeDecimal = Decimal.valueOf('2.5')"},
		Expression: "gladeDecimal.round()",
		ValueType:  "Integer",
	}})

	if !containsAll(source,
		"GLADE_STDLIB_ORACLE:",
		"Decimal gladeDecimal = Decimal.valueOf('2.5');",
		"Object gladeValue = gladeDecimal.round();",
		"decimal-round-half-up",
	) {
		t.Fatalf("rendered source missing required probe pieces:\n%s", source)
	}
}

func TestParseResultsReadsDebugMarker(t *testing.T) {
	text := "Execute Anonymous: System.debug('GLADE_STDLIB_ORACLE:' + JSON.serialize(gladeRows));\n" +
		`11:07:02.41|USER_DEBUG|[1]|DEBUG|GLADE_STDLIB_ORACLE:[{"id":"x","area":"Decimal","api":"Decimal.round","mode":"anonymous","value":"3","valueType":"Integer"}]`
	results, err := parseResults(text)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].ID != "x" || results[0].Value == nil || *results[0].Value != "3" || results[0].RawLogLine == "" {
		t.Fatalf("results = %#v", results)
	}
}

func TestParseResultsPreservesNullValues(t *testing.T) {
	line := `USER_DEBUG|DEBUG|GLADE_STDLIB_ORACLE:[{"id":"x","area":"Pattern","api":"Matcher.group","mode":"anonymous","value":null,"valueType":"String"}]`
	results, err := parseResults(line)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || !results[0].HasValue || results[0].Value != nil {
		t.Fatalf("results = %#v, want present null value", results)
	}
}

func TestStdlibCasesCoverTargetRows(t *testing.T) {
	cases := StdlibCases()
	if len(cases) < 30 {
		t.Fatalf("StdlibCases count = %d, want broad target row matrix", len(cases))
	}
	want := map[string]bool{
		"String:String.split":                 true,
		"Decimal:Decimal.round":               true,
		"Decimal:Decimal.setScale":            true,
		"JSON:JSON.deserialize":               true,
		"JSON:JSON.deserializeStrict":         true,
		"JSON:JSON.deserializeUntyped":        true,
		"JSON:JSON.serialize":                 true,
		"JSON:JSON.serializePretty":           true,
		"Pattern:Pattern.compile":             true,
		"Pattern:Pattern.matches":             true,
		"Pattern:Matcher.find":                true,
		"Pattern:Matcher.group":               true,
		"Pattern:Matcher.matches":             true,
		"EncodingUtil:EncodingUtil.urlDecode": true,
		"EncodingUtil:EncodingUtil.urlEncode": true,
		"Crypto:Crypto.generateDigest":        true,
	}
	for _, tc := range cases {
		delete(want, tc.Area+":"+tc.API)
	}
	if len(want) != 0 {
		t.Fatalf("missing target case coverage: %#v", want)
	}
}

func TestStdlibCasesCoverRegexParityOracleRows(t *testing.T) {
	cases := StdlibCases()
	wantIDs := map[string]bool{
		"pattern-grapheme-crlf-span":                  true,
		"pattern-grapheme-zwj-family-span":            true,
		"matcher-find-thumbs-up-skin-tone-span":       true,
		"pattern-grapheme-boundary-spans":             true,
		"pattern-class-algebra-nested-intersection":   true,
		"pattern-class-algebra-nested-subtraction":    true,
		"string-fromchararray-utf16-surrogate-pair":   true,
		"string-fromchararray-scalar-out-of-bmp":      true,
		"string-fromchararray-utf16-truncated-scalar": true,
	}
	for _, tc := range cases {
		delete(wantIDs, tc.ID)
	}
	if len(wantIDs) != 0 {
		t.Fatalf("missing regex parity oracle cases: %#v", wantIDs)
	}

	source := RenderAnonymous(cases)
	if !containsAll(source,
		"Pattern.compile('\\\\X')",
		"Pattern.compile('\\\\b{g}')",
		"String.fromCharArray(new List<Integer>{55357, 56397, 55356, 57341})",
		"String.fromCharArray(new List<Integer>{55357, 56397})",
		"[a-z&&[b-d&&[^c]]]+",
	) {
		t.Fatalf("rendered source missing regex parity probes:\n%s", source)
	}
}

func containsAll(text string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(text, part) {
			return false
		}
	}
	return true
}
