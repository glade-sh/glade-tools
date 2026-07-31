package oracleprobe

import (
	"context"
	"encoding/json"
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
	// Return output alongside error — mirrors real exec behaviour where
	// a command can write stdout before exiting nonzero.
	return f.output, f.err
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

// --- SF-05: Glade exec --json envelope transport tests ---

// gladeExecEnvelopeDebug builds a minimal real Glade exec --json
// envelope with the oracle marker in data.debug.  The marker text
// must be a raw (unescaped) line — the function JSON-escapes it
// so the envelope is valid.
func gladeExecEnvelopeDebug(markerLine string) []byte {
	env := map[string]any{
		"schemaVersion": "1.0",
		"command":       "exec",
		"status":        "passed",
		"exitCode":      0,
		"data": map[string]any{
			"debug": []string{markerLine},
		},
	}
	b, _ := json.Marshal(env)
	return b
}

// gladeExecEnvelopeDebugEvents builds the envelope with the marker in
// data.debugEvents[].message.
func gladeExecEnvelopeDebugEvents(markerLine string) []byte {
	env := map[string]any{
		"schemaVersion": "1.0",
		"command":       "exec",
		"status":        "passed",
		"exitCode":      0,
		"data": map[string]any{
			"debugEvents": []map[string]any{
				{"message": markerLine},
			},
		},
	}
	b, _ := json.Marshal(env)
	return b
}

func TestRunGladeParsesDebugEnvelope(t *testing.T) {
	marker := `GLADE_STDLIB_ORACLE:[{"id":"t","area":"Test","api":"Test","mode":"anonymous","value":"2","valueType":"Integer"}]`
	fake := &fakeCmdRunner{output: gladeExecEnvelopeDebug(marker)}
	cases := []Case{{
		ID: "t", Area: "Test", API: "Test", Mode: ModeAnonymous,
		Expression: "1+1",
	}}
	opts := GladeOptions{
		GladeBin:   "/opt/glade/bin/glade",
		ProjectDir: t.TempDir(),
	}

	report, err := RunGlade(context.Background(), fake, opts, cases)
	if err != nil {
		t.Fatalf("RunGlade: %v", err)
	}
	if len(report.Results) != 1 || report.Results[0].ID != "t" {
		t.Fatalf("results = %#v", report.Results)
	}
	r := report.Results[0]
	if r.Value == nil || *r.Value != "2" {
		t.Errorf("value = %v, want 2", r.Value)
	}
}

func TestRunGladeParsesDebugEventsEnvelope(t *testing.T) {
	marker := `GLADE_STDLIB_ORACLE:[{"id":"t","area":"Test","api":"Test","mode":"anonymous","value":"3","valueType":"Integer"}]`
	fake := &fakeCmdRunner{output: gladeExecEnvelopeDebugEvents(marker)}
	cases := []Case{{
		ID: "t", Area: "Test", API: "Test", Mode: ModeAnonymous,
		Expression: "1+1",
	}}
	opts := GladeOptions{
		GladeBin:   "/opt/glade/bin/glade",
		ProjectDir: t.TempDir(),
	}

	report, err := RunGlade(context.Background(), fake, opts, cases)
	if err != nil {
		t.Fatalf("RunGlade: %v", err)
	}
	if len(report.Results) != 1 || report.Results[0].ID != "t" {
		t.Fatalf("results = %#v", report.Results)
	}
	r := report.Results[0]
	if r.Value == nil || *r.Value != "3" {
		t.Errorf("value = %v, want 3", r.Value)
	}
}

func TestRunGladeEnvelopeReturnsAllCaseIDsOnce(t *testing.T) {
	marker := `GLADE_STDLIB_ORACLE:[` +
		`{"id":"a","area":"Decimal","api":"Decimal.round","mode":"anonymous","value":"1","valueType":"Integer"},` +
		`{"id":"b","area":"String","api":"String.split","mode":"anonymous","value":"2","valueType":"Integer"},` +
		`{"id":"c","area":"JSON","api":"JSON.serialize","mode":"anonymous","value":"3","valueType":"Integer"}]`
	fake := &fakeCmdRunner{output: gladeExecEnvelopeDebug(marker)}
	cases := []Case{
		{ID: "a", Area: "Decimal", API: "Decimal.round", Mode: ModeAnonymous, Expression: "a"},
		{ID: "b", Area: "String", API: "String.split", Mode: ModeAnonymous, Expression: "b"},
		{ID: "c", Area: "JSON", API: "JSON.serialize", Mode: ModeAnonymous, Expression: "c"},
	}
	opts := GladeOptions{
		GladeBin:   "/opt/glade/bin/glade",
		ProjectDir: t.TempDir(),
	}

	report, err := RunGlade(context.Background(), fake, opts, cases)
	if err != nil {
		t.Fatalf("RunGlade: %v", err)
	}
	if len(report.Results) != 3 {
		t.Fatalf("results count = %d, want 3", len(report.Results))
	}
	seen := map[string]bool{}
	for _, r := range report.Results {
		if seen[r.ID] {
			t.Errorf("duplicate ID: %s", r.ID)
		}
		seen[r.ID] = true
	}
	for _, id := range []string{"a", "b", "c"} {
		if !seen[id] {
			t.Errorf("missing ID: %s", id)
		}
	}
}

func TestRunGladeEnvelopeMissingMarker(t *testing.T) {
	// Valid JSON envelope with no oracle marker in debug or debugEvents.
	output := []byte(`{"schemaVersion":"1.0","command":"exec","status":"passed","exitCode":0,"data":{"debug":["line1","line2"]}}`)
	fake := &fakeCmdRunner{output: output}
	cases := []Case{{ID: "t", Area: "Test", API: "Test", Mode: ModeAnonymous, Expression: "1+1"}}
	opts := GladeOptions{
		GladeBin:   "/opt/glade/bin/glade",
		ProjectDir: t.TempDir(),
	}
	_, err := RunGlade(context.Background(), fake, opts, cases)
	if err == nil {
		t.Fatal("expected error for missing marker")
	}
	if strings.Contains(err.Error(), `"line1"`) || strings.Contains(err.Error(), "line2") {
		t.Errorf("error leaks raw debug output: %v", err)
	}
}

func TestRunGladeEnvelopeMalformedJSON(t *testing.T) {
	fake := &fakeCmdRunner{output: []byte("{bad json")}
	cases := []Case{{ID: "t", Area: "Test", API: "Test", Mode: ModeAnonymous, Expression: "1+1"}}
	opts := GladeOptions{
		GladeBin:   "/opt/glade/bin/glade",
		ProjectDir: t.TempDir(),
	}
	_, err := RunGlade(context.Background(), fake, opts, cases)
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	if !strings.Contains(err.Error(), "glade parse results") && !strings.Contains(err.Error(), "malform") {
		t.Errorf("error = %v, want parse-or-malformed category", err)
	}
}

func TestRunGladeEnvelopeCommandFailureNoRawLeak(t *testing.T) {
	fake := &fakeCmdRunner{
		output: []byte(`{"schemaVersion":"1.0","command":"exec","status":"failed","exitCode":1,"data":{"debug":["GLADE_STDLIB_ORACLE:[{\"id\":\"t\",\"area\":\"Test\",\"api\":\"Test\",\"mode\":\"anonymous\",\"value\":\"2\",\"valueType\":\"Integer\"}]"]}}`),
		err:    errors.New("exit status 1"),
	}
	cases := []Case{{ID: "t", Area: "Test", API: "Test", Mode: ModeAnonymous, Expression: "1+1"}}
	opts := GladeOptions{
		GladeBin:   "/opt/glade/bin/glade",
		ProjectDir: t.TempDir(),
	}
	_, err := RunGlade(context.Background(), fake, opts, cases)
	if err == nil {
		t.Fatal("expected error for command failure")
	}
	errStr := err.Error()
	if strings.Contains(errStr, `"id":"t"`) || strings.Contains(errStr, "GLADE_STDLIB_ORACLE:") {
		t.Errorf("error leaks raw output: %s", errStr)
	}
}

// Existing raw-marker fixtures must still work.
func TestRunGladeRawMarkerStillParses(t *testing.T) {
	output := `USER_DEBUG|DEBUG|GLADE_STDLIB_ORACLE:[{"id":"t","area":"Test","api":"Test","mode":"anonymous","value":"2","valueType":"Integer"}]`
	fake := &fakeCmdRunner{output: []byte(output)}
	cases := []Case{{ID: "t", Area: "Test", API: "Test", Mode: ModeAnonymous, Expression: "1+1"}}
	opts := GladeOptions{
		GladeBin:   "/opt/glade/bin/glade",
		ProjectDir: t.TempDir(),
	}

	report, err := RunGlade(context.Background(), fake, opts, cases)
	if err != nil {
		t.Fatalf("RunGlade: %v", err)
	}
	if len(report.Results) != 1 || report.Results[0].ID != "t" {
		t.Fatalf("results = %#v", report.Results)
	}
	r := report.Results[0]
	if r.Value == nil || *r.Value != "2" {
		t.Errorf("value = %v, want 2", r.Value)
	}
}

// --- SF-07: stdout / stderr separation ---

func TestOSExecRunnerStdoutSeparateFromStderr(t *testing.T) {
	runner := OSExecRunner{}
	out, err := runner.RunContext(context.Background(),
		"sh", "-c", `printf '{"status":"passed"}\n'; printf 'Warning: update available\n' >&2`)
	if err != nil {
		t.Fatalf("unexpected error (command exit 0): %v", err)
	}
	if !strings.Contains(string(out), `"status":"passed"`) {
		t.Errorf("missing JSON in stdout: %s", out)
	}
	if strings.Contains(string(out), "Warning") {
		t.Errorf("stderr leaked into stdout: %s", out)
	}
}

func TestOSExecRunnerStdoutWithStderrAndNonzeroExit(t *testing.T) {
	runner := OSExecRunner{}
	out, err := runner.RunContext(context.Background(),
		"sh", "-c", `printf '{"status":"passed"}\n'; printf 'Warning: update available\n' >&2; exit 1`)
	if err == nil {
		t.Fatal("expected error for exit 1")
	}
	if !strings.Contains(string(out), `"status":"passed"`) {
		t.Errorf("missing JSON in stdout: %s", out)
	}
	if strings.Contains(string(out), "Warning") {
		t.Errorf("stderr leaked into stdout: %s", out)
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

// --- SF-10: Observation shape tests ---

func TestObservationShapesOrderedList(t *testing.T) {
	cases := StdlibCases()

	shapeIDs := map[string]bool{
		"pattern-grapheme-crlf-span":                  true,
		"pattern-grapheme-zwj-family-span":            true,
		"matcher-find-thumbs-up-skin-tone-span":       true,
		"string-fromchararray-utf16-surrogate-pair":   true,
		"string-fromchararray-scalar-out-of-bmp":      true,
		"string-fromchararray-utf16-truncated-scalar": true,
		"json-deserialize-string-key-map":             true,
	}
	var shapeCases []Case
	for _, c := range cases {
		if shapeIDs[c.ID] {
			shapeCases = append(shapeCases, c)
		}
	}
	if len(shapeCases) != 7 {
		t.Fatalf("shape cases count = %d, want 7", len(shapeCases))
	}

	source := RenderAnonymous(shapeCases)
	if !strings.Contains(source, "new List<") {
		t.Error("observation-shape cases missing ordered list serialization")
	}

	// None of the seven cases may serialize a Map variable directly.
	directMapPatterns := []string{
		"JSON.serialize(gladeCRLFSpan)",
		"JSON.serialize(gladeFamilySpan)",
		"JSON.serialize(gladeThumbsSpan)",
		"JSON.serialize(gladeSurrogatePairShape)",
		"JSON.serialize(gladeScalarShape)",
		"JSON.serialize(gladeTruncatedScalarShape)",
	}
	for _, pat := range directMapPatterns {
		if strings.Contains(source, pat) {
			t.Errorf("shape case must not serialize Map variable directly: %s", pat)
		}
	}
}

func TestJSONStrictDuplicateFieldsShape(t *testing.T) {
	cases := StdlibCases()
	var strictDup Case
	for _, c := range cases {
		if c.ID == "json-strict-duplicate-fields" {
			strictDup = c
			break
		}
	}
	if strictDup.ID == "" {
		t.Fatal("json-strict-duplicate-fields case not found")
	}

	if strictDup.ExpectThrow {
		t.Error("json-strict-duplicate-fields must observe value, not expect throw")
	}
	if strictDup.ValueType != "String" {
		t.Errorf("json-strict-duplicate-fields ValueType = %q, want String", strictDup.ValueType)
	}

	source := RenderAnonymous([]Case{strictDup})
	if !strings.Contains(source, "(Account)") {
		t.Error("json-strict-duplicate-fields must cast to Account before reading .Name")
	}
	if !strings.Contains(source, ".Name") {
		t.Error("json-strict-duplicate-fields must access .Name field")
	}
}

func TestMapSerializeCasesUnchanged(t *testing.T) {
	cases := StdlibCases()
	var primMap, prettyMap Case
	for _, c := range cases {
		switch c.ID {
		case "json-serialize-primitive-map":
			primMap = c
		case "json-serialize-pretty-map":
			prettyMap = c
		}
	}

	source := RenderAnonymous([]Case{primMap, prettyMap})
	if !strings.Contains(source, "JSON.serialize(new Map<String,Object>") {
		t.Error("json-serialize-primitive-map must still serialize Map directly")
	}
	if !strings.Contains(source, "JSON.serializePretty(new Map<String,Object>") {
		t.Error("json-serialize-pretty-map must still serialize Map directly")
	}
}
