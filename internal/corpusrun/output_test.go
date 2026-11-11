package corpusrun

import (
	"strings"
	"testing"
)

const sampleTestOutput = `{
  "schemaVersion": "1.0",
  "command": "test",
  "status": "failed",
  "exitCode": 1,
  "summary": {"total": 6, "passed": 2, "failed": 1, "skipped": 1, "compileErrors": 1, "runtimeErrors": 0, "unsupported": 1, "errors": 2, "durationMs": 10, "excludedClasses": 2},
  "tests": [
    {"name": "SampleTest.passes", "className": "SampleTest", "methodName": "passes", "status": "pass", "durationMs": 1},
    {"name": "SampleTest.passesToo", "className": "SampleTest", "methodName": "passesToo", "status": "pass", "durationMs": 1},
    {"name": "SampleTest.fails", "className": "SampleTest", "methodName": "fails", "status": "fail", "reason": "assertion",
     "problem": {"type": "System.AssertException", "message": "Assertion Failed:\nExpected: 1, Actual: 2 at @ROOT@/src/A.cls"}},
    {"name": "SampleTest.skipped", "className": "SampleTest", "methodName": "skipped", "status": "skipped"},
    {"className": "BrokenTest", "methodName": "wontCompile", "status": "compile_error", "problem": {"type": "CompileError", "message": "Invalid type: Missing"}},
    {"name": "SampleTest.cut", "className": "SampleTest", "methodName": "cut", "status": "unsupported", "reason": "cancelled",
     "problem": {"type": "Canceled", "message": "context canceled"}}
  ],
  "data": {
    "excludedClasses": [
      {"className": "BrokenTest", "root": "BrokenTest", "message": "Invalid type: Missing"},
      {"className": "Helper", "root": "Helper", "message": "Invalid type: Missing"}
    ]
  }
}`

func TestParseTestOutput(t *testing.T) {
	out, err := parseTestOutput([]byte(strings.ReplaceAll(sampleTestOutput, "@ROOT@", "/scratch/sample-app")), "/scratch/sample-app")
	if err != nil {
		t.Fatal(err)
	}
	if out.Tests != 6 || out.Passed != 2 {
		t.Fatalf("counts: tests %d passed %d", out.Tests, out.Passed)
	}
	if got := testNames(out.Timeouts); len(got) != 1 || got[0] != "SampleTest.cut" {
		t.Fatalf("Timeouts = %v", got)
	}
	// A skipped test did not fail. BrokenTest appears through its method and
	// Helper, which has no method, through the class it names.
	want := []string{"BrokenTest.wontCompile", "Helper", "SampleTest.fails"}
	if got := testNames(out.Failing); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("Failing = %v, want %v", got, want)
	}
	message := out.Failing[2].Message
	if strings.Contains(message, "\n") {
		t.Fatalf("message is not one line: %q", message)
	}
	if strings.Contains(message, "/scratch/sample-app") || !strings.Contains(message, "<root>/src/A.cls") {
		t.Fatalf("message keeps the scratch path: %q", message)
	}
	if !strings.HasPrefix(message, "System.AssertException: Assertion Failed:") {
		t.Fatalf("message = %q", message)
	}
}

func TestParseTestOutputTimeoutShapes(t *testing.T) {
	cases := []struct {
		name string
		row  string
	}{
		{"reason timeout", `{"name":"T.a","status":"unsupported","reason":"timeout","problem":{"type":"Canceled","message":"context deadline exceeded"}}`},
		{"reason cancelled", `{"name":"T.a","status":"unsupported","reason":"cancelled"}`},
		{"reason canceled", `{"name":"T.a","status":"unsupported","reason":"canceled"}`},
		{"problem type", `{"name":"T.a","status":"unsupported","problem":{"type":"canceled","message":"x"}}`},
		{"problem message", `{"name":"T.a","status":"runtime_error","problem":{"type":"X","message":"run stopped: context canceled"}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := parseTestOutput([]byte(`{"summary":{"total":1},"tests":[`+tc.row+`]}`), "")
			if err != nil {
				t.Fatal(err)
			}
			if len(out.Timeouts) != 1 || len(out.Failing) != 0 {
				t.Fatalf("timeouts %v failing %v", out.Timeouts, out.Failing)
			}
		})
	}
}

func TestParseTestOutputListsADuplicatedTestOnce(t *testing.T) {
	out, err := parseTestOutput([]byte(`{"summary":{"total":2},"tests":[
		{"name":"T.a","status":"fail"},{"name":"T.a","status":"fail"}]}`), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Failing) != 1 {
		t.Fatalf("Failing = %v", out.Failing)
	}
}

func TestParseTestOutputRefusesAnEnvelopeWithoutResults(t *testing.T) {
	for _, data := range []string{``, `not json`, `{"status":"error"}`, `[1,2]`} {
		if _, err := parseTestOutput([]byte(data), ""); err == nil {
			t.Fatalf("parseTestOutput(%q) succeeded", data)
		}
	}
}

func TestParseCheckOutput(t *testing.T) {
	data := `{
  "schemaVersion": "1.0",
  "command": "check",
  "status": "failed",
  "exitCode": 1,
  "project": {"root": "/scratch/sample-app", "sourceApiVersion": "63.0"},
  "summary": {"types": 2, "triggers": 0, "objects": 0, "diagnostics": 3},
  "diagnostics": [
    {"severity": "warning", "code": "GLADEPERF001", "message": "slow\nloop in /scratch/sample-app/src/B.cls", "file": "/scratch/sample-app/src/B.cls", "range": {"start": {"line": 7, "column": 2}, "end": {"line": 7, "column": 9}}},
    {"severity": "error", "code": "GLADESEMA001", "message": "unknown type", "file": "/scratch/sample-app/src/A.cls", "range": {"start": {"line": 3, "column": 1}}},
    {"severity": "error", "code": "GLADEGEN001", "message": "no location"}
  ]
}`
	out, err := parseCheckOutput([]byte(data), "/ignored/when/the/report/names/a/root")
	if err != nil {
		t.Fatal(err)
	}
	if out.SourceAPI != "63.0" {
		t.Fatalf("SourceAPI = %q", out.SourceAPI)
	}
	if len(out.Diagnostics) != 3 {
		t.Fatalf("Diagnostics = %+v", out.Diagnostics)
	}
	first := out.Diagnostics[1]
	if first.Code != "GLADESEMA001" || first.File != "src/A.cls" || first.Line != 3 || first.Severity != "error" {
		t.Fatalf("diagnostic = %+v", first)
	}
	perf := out.Diagnostics[2]
	if perf.File != "src/B.cls" || perf.Line != 7 || strings.Contains(perf.Message, "\n") || strings.Contains(perf.Message, "/scratch/sample-app") {
		t.Fatalf("diagnostic = %+v", perf)
	}
	if none := out.Diagnostics[0]; none.File != "" || none.Line != 0 || none.Code != "GLADEGEN001" {
		t.Fatalf("diagnostic without a location = %+v", none)
	}
}

func TestParseCheckOutputFallsBackToTheCopyPath(t *testing.T) {
	data := `{"project":{},"diagnostics":[{"code":"C","file":"/scratch/sample-app/src/A.cls"}]}`
	out, err := parseCheckOutput([]byte(data), "/scratch/sample-app")
	if err != nil {
		t.Fatal(err)
	}
	if out.Diagnostics[0].File != "src/A.cls" {
		t.Fatalf("File = %q", out.Diagnostics[0].File)
	}
}

func TestParseCheckOutputRefusesAnEnvelopeWithoutResults(t *testing.T) {
	for _, data := range []string{``, `not json`, `{"status":"error"}`} {
		if _, err := parseCheckOutput([]byte(data), ""); err == nil {
			t.Fatalf("parseCheckOutput(%q) succeeded", data)
		}
	}
}

func TestLongMessageIsTruncated(t *testing.T) {
	long := strings.Repeat("x", 5000)
	out, err := parseTestOutput([]byte(`{"summary":{"total":1},"tests":[{"name":"T.a","status":"fail","problem":{"message":"`+long+`"}}]}`), "")
	if err != nil {
		t.Fatal(err)
	}
	if got := len(out.Failing[0].Message); got > maxMessage+3 {
		t.Fatalf("message length = %d", got)
	}
}
