package oracleprobe

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
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

// --- SF-02: Runtime comparator red tests ---

type fakeCmdRunner struct {
	mu     sync.Mutex
	calls  []fakeCmdCall
	output []byte
	err    error
	delay  time.Duration
}

type fakeCmdCall struct {
	Name string
	Args []string
}

func (f *fakeCmdRunner) RunContext(ctx context.Context, name string, arg ...string) ([]byte, error) {
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	f.mu.Lock()
	args := make([]string, len(arg))
	copy(args, arg)
	f.calls = append(f.calls, fakeCmdCall{Name: name, Args: args})
	f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	return f.output, nil
}

func TestGladeCommandArgsSourceFinalNoShell(t *testing.T) {
	output := `USER_DEBUG|DEBUG|GLADE_STDLIB_ORACLE:[{"id":"t","area":"Test","api":"Test","mode":"anonymous","value":"2","valueType":"Integer"}]`
	fake := &fakeCmdRunner{output: []byte(output)}
	cases := []Case{{
		ID: "t", Area: "Test", API: "Test", Mode: ModeAnonymous,
		Expression: "1+1",
	}}
	opts := GladeOptions{
		GladeBin:   "/opt/glade/bin/glade",
		ProjectDir: t.TempDir(),
	}

	_, err := RunGlade(context.Background(), fake, opts, cases)
	if err != nil {
		t.Fatalf("RunGlade: %v", err)
	}

	if len(fake.calls) != 1 {
		t.Fatalf("expected 1 command call, got %d", len(fake.calls))
	}
	call := fake.calls[0]

	if call.Name != "/opt/glade/bin/glade" {
		t.Errorf("program = %q, want /opt/glade/bin/glade", call.Name)
	}

	if len(call.Args) < 5 {
		t.Fatalf("args = %v, want at least 5", call.Args)
	}

	if call.Args[0] != "exec" {
		t.Errorf("args[0] = %q, want exec", call.Args[0])
	}
	if call.Args[1] != "--project" {
		t.Errorf("args[1] = %q, want --project", call.Args[1])
	}
	if call.Args[3] != "--json" {
		t.Errorf("args[3] = %q, want --json", call.Args[3])
	}

	last := call.Args[len(call.Args)-1]
	if last == "--json" || last == "--project" || last == "exec" {
		t.Errorf("last arg is flag %q, want apex source", last)
	}
	if !strings.Contains(last, "1+1") {
		t.Errorf("last arg missing apex source: %q", last)
	}

	for _, a := range call.Args {
		if a == "sh" || a == "-c" || a == "/bin/sh" || a == "/bin/bash" {
			t.Errorf("shell invocation detected: arg %q", a)
		}
	}
}

func TestComparePass(t *testing.T) {
	v := "3"
	sf := &Result{ID: "x", HasValue: true, Value: &v, ValueType: "Integer"}
	glade := &Result{ID: "x", HasValue: true, Value: &v, ValueType: "Integer"}
	c := Case{ID: "x", ValueType: "Integer"}

	cc := Compare(sf, glade, c)
	if cc.Status != StatusPass {
		t.Errorf("status = %s, want pass; observations: sf=%q glade=%q", cc.Status, cc.SFObservation, cc.GladeObservation)
	}
}

func TestCompareFailValue(t *testing.T) {
	sfV := "3"
	gladeV := "4"
	sf := &Result{ID: "x", HasValue: true, Value: &sfV, ValueType: "Integer"}
	glade := &Result{ID: "x", HasValue: true, Value: &gladeV, ValueType: "Integer"}
	c := Case{ID: "x", ValueType: "Integer"}

	cc := Compare(sf, glade, c)
	if cc.Status != StatusFail {
		t.Errorf("status = %s, want fail", cc.Status)
	}
}

func TestCompareFailException(t *testing.T) {
	sf := &Result{ID: "x", ExceptionType: "System.MathException", ExceptionMessage: "divide by zero"}
	glade := &Result{ID: "x", HasValue: false}
	c := Case{ID: "x", ExpectThrow: true}

	cc := Compare(sf, glade, c)
	if cc.Status != StatusFail {
		t.Errorf("status = %s, want fail for exception mismatch", cc.Status)
	}

	sf2 := &Result{ID: "x", ExceptionType: "System.MathException"}
	glade2 := &Result{ID: "x", ExceptionType: "System.NullPointerException"}
	cc2 := Compare(sf2, glade2, c)
	if cc2.Status != StatusFail {
		t.Errorf("status = %s, want fail for different exception types", cc2.Status)
	}
}

func TestCompareExceptionMessageNotContractual(t *testing.T) {
	// Equal exception types with different messages pass when the
	// case does not declare the message contractual.
	sf := &Result{ID: "x", ExceptionType: "System.MathException", ExceptionMessage: "divide by zero"}
	glade := &Result{ID: "x", ExceptionType: "System.MathException", ExceptionMessage: "Division by zero"}
	c := Case{ID: "x", ExpectThrow: true}

	cc := Compare(sf, glade, c)
	if cc.Status != StatusPass {
		t.Errorf("status = %s, want pass; sf=%q glade=%q", cc.Status, cc.SFObservation, cc.GladeObservation)
	}
}

func TestCompareExceptionMessageContractual(t *testing.T) {
	// Same exception type but different messages must fail when the
	// case declares the message contractual.
	sf := &Result{ID: "x", ExceptionType: "System.MathException", ExceptionMessage: "divide by zero"}
	glade := &Result{ID: "x", ExceptionType: "System.MathException", ExceptionMessage: "Division by zero"}
	c := Case{ID: "x", ExpectThrow: true, ExceptionMessageContractual: true}

	cc := Compare(sf, glade, c)
	if cc.Status != StatusFail {
		t.Errorf("status = %s, want fail; sf=%q glade=%q", cc.Status, cc.SFObservation, cc.GladeObservation)
	}
}

func TestCompareNormalized(t *testing.T) {
	sfV := "abc-123-def"
	gladeV := "abc-456-def"
	sf := &Result{ID: "x", HasValue: true, Value: &sfV, ValueType: "String"}
	glade := &Result{ID: "x", HasValue: true, Value: &gladeV, ValueType: "String"}
	c := Case{ID: "x", ValueType: "String", UnstableValue: "any-value-is-unstable"}
	// When a case declares an unstable value the observation text
	// replaces the concrete value with <UNSTABLE> so only the
	// execution shape (value vs exception, type) is compared.

	cc := Compare(sf, glade, c)
	if cc.Status != StatusPass {
		t.Errorf("status = %s, want pass after normalization; sf=%q glade=%q", cc.Status, cc.SFObservation, cc.GladeObservation)
	}
}

func TestCompareNoGlobalNormalization(t *testing.T) {
	sfV := "hello"
	gladeV := "world"
	sf := &Result{ID: "x", HasValue: true, Value: &sfV, ValueType: "String"}
	glade := &Result{ID: "x", HasValue: true, Value: &gladeV, ValueType: "String"}
	c := Case{ID: "x", ValueType: "String"}
	// No UnstableValue declared — stable mismatch must remain visible.

	cc := Compare(sf, glade, c)
	if cc.Status != StatusFail {
		t.Errorf("status = %s, want fail for stable mismatch without normalization", cc.Status)
	}
}

func TestMissingResultsInconclusive(t *testing.T) {
	v := "1"
	sf := &Result{ID: "x", HasValue: true, Value: &v, ValueType: "Integer"}
	c := Case{ID: "x", ValueType: "Integer"}

	cc := Compare(sf, nil, c)
	if cc.Status != StatusInconclusive {
		t.Errorf("missing glade: status = %s, want inconclusive", cc.Status)
	}

	cc2 := Compare(nil, sf, c)
	if cc2.Status != StatusInconclusive {
		t.Errorf("missing sf: status = %s, want inconclusive", cc2.Status)
	}

	cc3 := Compare(nil, nil, c)
	if cc3.Status != StatusInconclusive {
		t.Errorf("both missing: status = %s, want inconclusive", cc3.Status)
	}
}

func TestMalformedInconclusive(t *testing.T) {
	cases := []Case{{
		ID: "t", Area: "Test", API: "Test", Mode: ModeAnonymous,
		Expression: "1+1",
	}}
	opts := GladeOptions{
		GladeBin:   "/opt/glade/bin/glade",
		ProjectDir: t.TempDir(),
	}

	// Process failure
	fakeErr := &fakeCmdRunner{err: errors.New("exec: glade: executable file not found")}
	_, err := RunGlade(context.Background(), fakeErr, opts, cases)
	if err == nil {
		t.Errorf("expected error from process failure, got nil")
	}

	// Malformed JSON
	fakeBad := &fakeCmdRunner{output: []byte("this is not json and has no marker")}
	_, err = RunGlade(context.Background(), fakeBad, opts, cases)
	if err == nil {
		t.Errorf("expected error from malformed output, got nil")
	}

	// Timeout
	fakeSlow := &fakeCmdRunner{output: []byte("GLADE_STDLIB_ORACLE:[]"), delay: 2 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err = RunGlade(ctx, fakeSlow, opts, cases)
	if err == nil {
		t.Errorf("expected timeout error, got nil")
	}
}

func TestRedactCredentials(t *testing.T) {
	r := Report{
		TargetOrg:  "my-dev-hub",
		Username:   "admin@example.com",
		OrgID:      "00DXXXXXXXXXXXXXXX",
		APIVersion: "66.0",
		Results:    []Result{},
	}
	redacted := RedactReport(r)
	if redacted.Username != "" {
		t.Errorf("username not redacted: %q", redacted.Username)
	}
	if redacted.OrgID != "" {
		t.Errorf("orgId not redacted: %q", redacted.OrgID)
	}
	if redacted.TargetOrg != "" {
		t.Errorf("targetOrg not redacted: %q", redacted.TargetOrg)
	}
	// Stable fields must survive.
	if redacted.APIVersion != "66.0" {
		t.Errorf("apiVersion lost: %q", redacted.APIVersion)
	}
	if len(redacted.Results) != 0 {
		t.Errorf("results altered")
	}
}
