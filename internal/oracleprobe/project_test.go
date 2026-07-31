package oracleprobe

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- fake project command runner ---

type fakeProjectRunnerSeq struct {
	mu      sync.Mutex
	calls   []projectCmdCall
	deploy  fakeProjectOutput
	test    fakeProjectOutput
	callIdx int
}

type projectCmdCall struct {
	Dir  string
	Name string
	Args []string
}

type fakeProjectOutput struct {
	out []byte
	err error
}

func (f *fakeProjectRunnerSeq) RunDir(ctx context.Context, dir, name string, arg ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	args := make([]string, len(arg))
	copy(args, arg)
	f.calls = append(f.calls, projectCmdCall{Dir: dir, Name: name, Args: args})

	f.callIdx++
	if f.callIdx == 1 {
		return f.deploy.out, f.deploy.err
	}
	return f.test.out, f.test.err
}

// --- test helpers ---

func sfDeploySuccessJSON() []byte {
	return []byte(`{"status":0,"result":{"status":"Succeeded","success":true}}`)
}

func sfDeployFailJSON() []byte {
	return []byte(`{"status":0,"result":{"status":"Failed"}}`)
}

func sfTestPassJSON() []byte {
	return []byte(`{
  "status": 0,
  "result": {
    "summary": {"outcome":"Passed","testsRan":12,"passing":12,"failing":0,"skipped":0},
    "tests": [
      {"MethodName":"sfLifecycleDiscovery","Outcome":"Pass","ApexClass":{"Name":"CorrectnessProbeTest"}},
      {"MethodName":"sfLifecycleTestSetupExecution","Outcome":"Pass","ApexClass":{"Name":"CorrectnessProbeTest"}},
      {"MethodName":"sfLifecycleTestSetupCopy","Outcome":"Pass","ApexClass":{"Name":"CorrectnessProbeTest"}},
      {"MethodName":"sfLifecycleMethodRollback","Outcome":"Pass","ApexClass":{"Name":"CorrectnessProbeTest"}},
      {"MethodName":"sfLifecycleStaticReset","Outcome":"Pass","ApexClass":{"Name":"CorrectnessProbeTest"}},
      {"MethodName":"sfLifecycleStartTestLimits","Outcome":"Pass","ApexClass":{"Name":"CorrectnessProbeTest"}},
      {"MethodName":"sfLifecycleStopTestFutureQueueable","Outcome":"Pass","ApexClass":{"Name":"CorrectnessProbeTest"}},
      {"MethodName":"sfLifecycleStopTestBatchScheduled","Outcome":"Pass","ApexClass":{"Name":"CorrectnessProbeTest"}},
      {"MethodName":"sfLifecycleHttpCalloutMock","Outcome":"Pass","ApexClass":{"Name":"CorrectnessProbeTest"}},
      {"MethodName":"sfLifecycleDmlAllOrNone","Outcome":"Pass","ApexClass":{"Name":"CorrectnessProbeTest"}},
      {"MethodName":"sfLifecycleTransactionVisibility","Outcome":"Pass","ApexClass":{"Name":"CorrectnessProbeTest"}},
      {"MethodName":"sfLifecycleRunAs","Outcome":"Pass","ApexClass":{"Name":"CorrectnessProbeTest"}}
    ]
  }
}`)
}

func sfTestAssertFailJSON() []byte {
	return []byte(`{
  "status": 0,
  "result": {
    "summary": {"outcome":"Failed","testsRan":12,"passing":11,"failing":1,"skipped":0},
    "tests": [
      {"MethodName":"sfLifecycleDiscovery","Outcome":"Pass","ApexClass":{"Name":"CorrectnessProbeTest"}},
      {"MethodName":"sfLifecycleTestSetupExecution","Outcome":"Pass","ApexClass":{"Name":"CorrectnessProbeTest"}},
      {"MethodName":"sfLifecycleTestSetupCopy","Outcome":"Pass","ApexClass":{"Name":"CorrectnessProbeTest"}},
      {"MethodName":"sfLifecycleMethodRollback","Outcome":"Pass","ApexClass":{"Name":"CorrectnessProbeTest"}},
      {"MethodName":"sfLifecycleStaticReset","Outcome":"Pass","ApexClass":{"Name":"CorrectnessProbeTest"}},
      {"MethodName":"sfLifecycleStartTestLimits","Outcome":"Pass","ApexClass":{"Name":"CorrectnessProbeTest"}},
      {"MethodName":"sfLifecycleStopTestFutureQueueable","Outcome":"Pass","ApexClass":{"Name":"CorrectnessProbeTest"}},
      {"MethodName":"sfLifecycleStopTestBatchScheduled","Outcome":"Pass","ApexClass":{"Name":"CorrectnessProbeTest"}},
      {"MethodName":"sfLifecycleHttpCalloutMock","Outcome":"Pass","ApexClass":{"Name":"CorrectnessProbeTest"}},
      {"MethodName":"sfLifecycleDmlAllOrNone","Outcome":"Fail","Message":"System.AssertException: Assertion Failed: expected REQUIRED_FIELD_MISSING status","StackTrace":"Class.CorrectnessProbeTest.sfLifecycleDmlAllOrNone: line 293","ApexClass":{"Name":"CorrectnessProbeTest"}},
      {"MethodName":"sfLifecycleTransactionVisibility","Outcome":"Pass","ApexClass":{"Name":"CorrectnessProbeTest"}},
      {"MethodName":"sfLifecycleRunAs","Outcome":"Pass","ApexClass":{"Name":"CorrectnessProbeTest"}}
    ]
  }
}`)
}

// Glade fixture using the exact real artifact shape (summary.total/passed/failed/skipped,
// tests[].className/methodName/status, optional problem.type/problem.message).
func gladeTestAllPassJSON() []byte {
	return []byte(`{
  "schemaVersion":"1.0",
  "command":"test",
  "status":"passed",
  "exitCode":0,
  "summary":{"total":12,"passed":12,"failed":0,"skipped":0,"compileErrors":0,"runtimeErrors":0,"unsupported":0,"errors":0},
  "tests": [
    {"className":"CorrectnessProbeTest","methodName":"sfLifecycleDiscovery","status":"pass"},
    {"className":"CorrectnessProbeTest","methodName":"sfLifecycleTestSetupExecution","status":"pass"},
    {"className":"CorrectnessProbeTest","methodName":"sfLifecycleTestSetupCopy","status":"pass"},
    {"className":"CorrectnessProbeTest","methodName":"sfLifecycleMethodRollback","status":"pass"},
    {"className":"CorrectnessProbeTest","methodName":"sfLifecycleStaticReset","status":"pass"},
    {"className":"CorrectnessProbeTest","methodName":"sfLifecycleStartTestLimits","status":"pass"},
    {"className":"CorrectnessProbeTest","methodName":"sfLifecycleStopTestFutureQueueable","status":"pass"},
    {"className":"CorrectnessProbeTest","methodName":"sfLifecycleStopTestBatchScheduled","status":"pass"},
    {"className":"CorrectnessProbeTest","methodName":"sfLifecycleHttpCalloutMock","status":"pass"},
    {"className":"CorrectnessProbeTest","methodName":"sfLifecycleDmlAllOrNone","status":"pass"},
    {"className":"CorrectnessProbeTest","methodName":"sfLifecycleTransactionVisibility","status":"pass"},
    {"className":"CorrectnessProbeTest","methodName":"sfLifecycleRunAs","status":"pass"}
  ]
}`)
}

// Exact 12-case artifact: 10 pass, 2 fail (one assertion, one runtime).
func gladeTestArtifactShapeJSON() []byte {
	return []byte(`{
  "schemaVersion":"1.0",
  "command":"test",
  "status":"failed",
  "exitCode":1,
  "summary":{"total":12,"passed":10,"failed":2,"skipped":0,"compileErrors":0,"runtimeErrors":0,"unsupported":0,"errors":0},
  "tests": [
    {"className":"CorrectnessProbeTest","methodName":"sfLifecycleDiscovery","status":"fail","problem":{"type":"System.AssertException","message":"values should not be equal: <null>: SF-LIFECYCLE-DISCOVERY: test class discoverable"}},
    {"className":"CorrectnessProbeTest","methodName":"sfLifecycleTestSetupExecution","status":"pass"},
    {"className":"CorrectnessProbeTest","methodName":"sfLifecycleTestSetupCopy","status":"pass"},
    {"className":"CorrectnessProbeTest","methodName":"sfLifecycleMethodRollback","status":"pass"},
    {"className":"CorrectnessProbeTest","methodName":"sfLifecycleStaticReset","status":"pass"},
    {"className":"CorrectnessProbeTest","methodName":"sfLifecycleStartTestLimits","status":"pass"},
    {"className":"CorrectnessProbeTest","methodName":"sfLifecycleStopTestFutureQueueable","status":"pass"},
    {"className":"CorrectnessProbeTest","methodName":"sfLifecycleStopTestBatchScheduled","status":"fail","problem":{"type":"RuntimeError","message":"Database.executeBatch expects Batchable object"}},
    {"className":"CorrectnessProbeTest","methodName":"sfLifecycleHttpCalloutMock","status":"pass"},
    {"className":"CorrectnessProbeTest","methodName":"sfLifecycleDmlAllOrNone","status":"pass"},
    {"className":"CorrectnessProbeTest","methodName":"sfLifecycleTransactionVisibility","status":"pass"},
    {"className":"CorrectnessProbeTest","methodName":"sfLifecycleRunAs","status":"pass"}
  ]
}`)
}

func gladeTestOneFailJSON() []byte {
	return []byte(`{
  "schemaVersion":"1.0",
  "summary":{"total":12,"passed":11,"failed":1,"skipped":0,"compileErrors":0,"runtimeErrors":0,"unsupported":0,"errors":0},
  "tests": [
    {"className":"CorrectnessProbeTest","methodName":"sfLifecycleDiscovery","status":"pass"},
    {"className":"CorrectnessProbeTest","methodName":"sfLifecycleTestSetupExecution","status":"pass"},
    {"className":"CorrectnessProbeTest","methodName":"sfLifecycleTestSetupCopy","status":"pass"},
    {"className":"CorrectnessProbeTest","methodName":"sfLifecycleMethodRollback","status":"pass"},
    {"className":"CorrectnessProbeTest","methodName":"sfLifecycleStaticReset","status":"pass"},
    {"className":"CorrectnessProbeTest","methodName":"sfLifecycleStartTestLimits","status":"pass"},
    {"className":"CorrectnessProbeTest","methodName":"sfLifecycleStopTestFutureQueueable","status":"pass"},
    {"className":"CorrectnessProbeTest","methodName":"sfLifecycleStopTestBatchScheduled","status":"pass"},
    {"className":"CorrectnessProbeTest","methodName":"sfLifecycleHttpCalloutMock","status":"pass"},
    {"className":"CorrectnessProbeTest","methodName":"sfLifecycleDmlAllOrNone","status":"fail","problem":{"type":"System.AssertException","message":"Assertion Failed: expected REQUIRED_FIELD_MISSING status"}},
    {"className":"CorrectnessProbeTest","methodName":"sfLifecycleTransactionVisibility","status":"pass"},
    {"className":"CorrectnessProbeTest","methodName":"sfLifecycleRunAs","status":"pass"}
  ]
}`)
}

// --- Test 1: twelve unique case IDs and method names ---

func TestProjectOracleLifecycleTwelveCases(t *testing.T) {
	cases := ProjectOracleCases()
	if len(cases) != 12 {
		t.Fatalf("count = %d, want 12", len(cases))
	}
	seenID := map[string]bool{}
	seenMethod := map[string]bool{}
	for _, c := range cases {
		if seenID[c.ID] {
			t.Errorf("duplicate ID: %s", c.ID)
		}
		seenID[c.ID] = true
		if c.Mode != ModeDeploy {
			t.Errorf("case %s mode = %s", c.ID, c.Mode)
		}
		if c.Area != "Lifecycle" {
			t.Errorf("case %s area = %s", c.ID, c.Area)
		}
		if len(c.Statements) != 1 {
			t.Errorf("case %s Statements len = %d, want 1", c.ID, len(c.Statements))
		}
		if seenMethod[c.Statements[0]] {
			t.Errorf("duplicate method: %s", c.Statements[0])
		}
		seenMethod[c.Statements[0]] = true
	}
	wantIDs := []string{
		"SF-LIFECYCLE-DISCOVERY", "SF-LIFECYCLE-TEST-SETUP-EXECUTION",
		"SF-LIFECYCLE-TEST-SETUP-COPY", "SF-LIFECYCLE-METHOD-ROLLBACK",
		"SF-LIFECYCLE-STATIC-RESET", "SF-LIFECYCLE-START-TEST-LIMITS",
		"SF-LIFECYCLE-STOP-TEST-FUTURE-QUEUEABLE", "SF-LIFECYCLE-STOP-TEST-BATCH-SCHEDULED",
		"SF-LIFECYCLE-HTTP-CALLOUT-MOCK", "SF-LIFECYCLE-DML-ALL-OR-NONE",
		"SF-LIFECYCLE-TRANSACTION-VISIBILITY", "SF-LIFECYCLE-RUN-AS",
	}
	wantMethods := []string{
		"sfLifecycleDiscovery", "sfLifecycleTestSetupExecution",
		"sfLifecycleTestSetupCopy", "sfLifecycleMethodRollback",
		"sfLifecycleStaticReset", "sfLifecycleStartTestLimits",
		"sfLifecycleStopTestFutureQueueable", "sfLifecycleStopTestBatchScheduled",
		"sfLifecycleHttpCalloutMock", "sfLifecycleDmlAllOrNone",
		"sfLifecycleTransactionVisibility", "sfLifecycleRunAs",
	}
	for _, id := range wantIDs {
		if !seenID[id] {
			t.Errorf("missing ID: %s", id)
		}
	}
	for _, m := range wantMethods {
		if !seenMethod[m] {
			t.Errorf("missing method: %s", m)
		}
	}
}

// --- Test 2: deploy before test ---

func TestProjectOracleLifecycleDeployBeforeTest(t *testing.T) {
	fake := &fakeProjectRunnerSeq{
		deploy: fakeProjectOutput{out: sfDeploySuccessJSON()},
		test:   fakeProjectOutput{out: sfTestPassJSON()},
	}
	_, err := RunProjectOracle(context.Background(), fake, "/proj", "org", ProjectOracleCases())
	if err != nil {
		t.Fatalf("RunProjectOracle: %v", err)
	}
	if len(fake.calls) < 2 {
		t.Fatalf("calls = %d, want at least 2", len(fake.calls))
	}
	if !strings.Contains(strings.Join(fake.calls[0].Args, " "), "deploy") {
		t.Errorf("first call not deploy: %v", fake.calls[0].Args)
	}
	if !strings.Contains(strings.Join(fake.calls[1].Args, " "), "run test") {
		t.Errorf("second call not test: %v", fake.calls[1].Args)
	}
}

// --- Test 3: working directory ---

func TestProjectOracleLifecycleWorkingDirectory(t *testing.T) {
	fake := &fakeProjectRunnerSeq{
		deploy: fakeProjectOutput{out: sfDeploySuccessJSON()},
		test:   fakeProjectOutput{out: sfTestPassJSON()},
	}
	_, err := RunProjectOracle(context.Background(), fake, "/my-project", "org", ProjectOracleCases())
	if err != nil {
		t.Fatalf("RunProjectOracle: %v", err)
	}
	if len(fake.calls) != 2 {
		t.Fatalf("calls = %d", len(fake.calls))
	}
	for i, call := range fake.calls {
		if call.Dir != "/my-project" {
			t.Errorf("call %d Dir = %q", i, call.Dir)
		}
	}
}

// --- Test 4: no shell ---

func TestProjectOracleLifecycleNoShell(t *testing.T) {
	fake := &fakeProjectRunnerSeq{
		deploy: fakeProjectOutput{out: sfDeploySuccessJSON()},
		test:   fakeProjectOutput{out: sfTestPassJSON()},
	}
	_, err := RunProjectOracle(context.Background(), fake, "/proj", "org", ProjectOracleCases())
	if err != nil {
		t.Fatalf("RunProjectOracle: %v", err)
	}
	for _, call := range fake.calls {
		for _, a := range call.Args {
			if a == "sh" || a == "-c" || a == "/bin/sh" || a == "/bin/bash" {
				t.Errorf("shell arg: %q", a)
			}
		}
		j := strings.Join(call.Args, " ")
		if strings.Contains(j, "&&") || strings.Contains(j, ";") || strings.Contains(j, "|") {
			t.Errorf("joined command: %s", j)
		}
	}
}

// --- Test 5: Glade args ---

func TestProjectOracleLifecycleGladeProjectArgs(t *testing.T) {
	fake := &fakeCmdRunner{output: gladeTestAllPassJSON()}
	_, err := RunGladeProject(context.Background(), fake, GladeProjectOptions{
		GladeBin: "/glade", ProjectDir: "/proj",
	}, ProjectOracleCases())
	if err != nil {
		t.Fatalf("RunGladeProject: %v", err)
	}
	if len(fake.calls) != 1 {
		t.Fatalf("calls = %d", len(fake.calls))
	}
	c := fake.calls[0]
	if c.Name != "/glade" {
		t.Errorf("Name = %q", c.Name)
	}
	if len(c.Args) != 4 || c.Args[0] != "test" || c.Args[1] != "--project" || c.Args[2] != "/proj" || c.Args[3] != "--json" {
		t.Errorf("Args = %v", c.Args)
	}
	for _, a := range c.Args {
		if a == "sh" || a == "-c" {
			t.Errorf("shell: %q", a)
		}
	}
}

// --- Test 6: SF normalization ---

func TestProjectOracleLifecycleNormalizeSalesforceResult(t *testing.T) {
	results, err := normalizeSFTests(sfTestPassJSON(), ProjectOracleCases())
	if err != nil {
		t.Fatalf("normalizeSFTests: %v", err)
	}
	if len(results) != 12 {
		t.Fatalf("count = %d", len(results))
	}
	// Check catalog order: first is DISCOVERY, last is RUN-AS.
	if results[0].ID != "SF-LIFECYCLE-DISCOVERY" {
		t.Errorf("first = %s", results[0].ID)
	}
	if results[11].ID != "SF-LIFECYCLE-RUN-AS" {
		t.Errorf("last = %s", results[11].ID)
	}
	for _, r := range results {
		if r.Mode != ModeDeploy || r.Area != "Lifecycle" {
			t.Errorf("mode/area wrong: %v", r)
		}
		if !r.HasValue || r.Value == nil || *r.Value != "pass" || r.ValueType != "assertion" {
			t.Errorf("pass encoding: %+v", r)
		}
		if strings.HasPrefix(r.ID, "707") || strings.HasPrefix(r.ID, "01p") {
			t.Errorf("generated ID leak: %q", r.ID)
		}
	}
}

// --- Test 7: Glade normalization matches real artifact ---

func TestProjectOracleLifecycleNormalizeGladeArtifact(t *testing.T) {
	// Exact 12-case artifact: 10 pass, 2 fail, exitCode 1.
	results, err := normalizeGladeTests(gladeTestArtifactShapeJSON(), ProjectOracleCases())
	if err != nil {
		t.Fatalf("normalizeGladeTests: %v", err)
	}
	if len(results) != 12 {
		t.Fatalf("count = %d", len(results))
	}
	// Catalog order.
	if results[0].ID != "SF-LIFECYCLE-DISCOVERY" {
		t.Errorf("first = %s", results[0].ID)
	}
	if results[11].ID != "SF-LIFECYCLE-RUN-AS" {
		t.Errorf("last = %s", results[11].ID)
	}

	// Count passes and fails.
	pass, fail := 0, 0
	for _, r := range results {
		if r.Value == nil {
			t.Errorf("nil value for %s", r.ID)
			continue
		}
		if *r.Value == "pass" {
			pass++
		}
		if *r.Value == "fail" {
			fail++
		}
	}
	if pass != 10 || fail != 2 {
		t.Errorf("pass=%d fail=%d, want 10/2", pass, fail)
	}

	// DISCOVERY should be assertion fail.
	d := results[0]
	if d.ValueType != "assertion" || *d.Value != "fail" {
		t.Errorf("DISCOVERY = fail/%s, want fail/assertion", d.ValueType)
	}
	// BATCH-SCHEDULED should be runtime fail.
	b := results[7]
	if b.ValueType != "runtime" || *b.Value != "fail" {
		t.Errorf("BATCH-SCHEDULED = fail/%s, want fail/runtime", b.ValueType)
	}
	// No generated IDs or raw output leak.
	for _, r := range results {
		if r.RawLogLine != "" {
			t.Errorf("RawLogLine leak: %q", r.RawLogLine)
		}
		if r.Value != nil && strings.Contains(*r.Value, "<null>") {
			t.Errorf("raw message leak: %q", *r.Value)
		}
	}
}

// --- Test 8: nonzero test with valid JSON = behavioral ---

func TestProjectOracleLifecycleNonzeroTestResultBehavioral(t *testing.T) {
	cases := ProjectOracleCases()
	fake := &fakeProjectRunnerSeq{
		deploy: fakeProjectOutput{out: sfDeploySuccessJSON()},
		test:   fakeProjectOutput{out: sfTestAssertFailJSON(), err: errors.New("exit status 1")},
	}
	report, err := RunProjectOracle(context.Background(), fake, "/proj", "org", cases)
	if err != nil {
		t.Fatalf("should return behavioral evidence: %v", err)
	}
	if len(report.Results) != 12 {
		t.Fatalf("count = %d", len(report.Results))
	}
	// DML case is assertion fail.
	byID := map[string]*Result{}
	for i := range report.Results {
		byID[report.Results[i].ID] = &report.Results[i]
	}
	r := byID["SF-LIFECYCLE-DML-ALL-OR-NONE"]
	if r == nil || *r.Value != "fail" || r.ValueType != "assertion" {
		t.Errorf("DML = %v", r)
	}
}

// --- Test 9: Glade nonzero exit with valid JSON = behavioral ---

func TestProjectOracleLifecycleGladeNonzeroExitBehavioral(t *testing.T) {
	// Simulate Glade exiting 1 with complete valid JSON.
	fake := &fakeCmdRunner{output: gladeTestArtifactShapeJSON(), err: errors.New("exit status 1")}
	report, err := RunGladeProject(context.Background(), fake, GladeProjectOptions{
		GladeBin: "/glade", ProjectDir: "/proj",
	}, ProjectOracleCases())
	if err != nil {
		t.Fatalf("RunGladeProject should return behavioral evidence: %v", err)
	}
	if len(report.Results) != 12 {
		t.Fatalf("count = %d", len(report.Results))
	}
}

// --- Test 10: category differentiation (assertion vs runtime) ---

func TestProjectOracleLifecycleCategoryDifferentiation(t *testing.T) {
	cases := ProjectOracleCases()
	sfResults, _ := normalizeSFTests(sfTestAssertFailJSON(), cases)
	gladeResults, _ := normalizeGladeTests(gladeTestOneFailJSON(), cases)

	// Both sides fail DML case with assertion category → pass.
	comp := CompareReports(Report{Results: sfResults}, Report{Results: gladeResults}, cases)
	dml := findComp(comp, "SF-LIFECYCLE-DML-ALL-OR-NONE")
	if dml == nil || dml.Status != StatusPass {
		t.Errorf("assertion vs assertion should pass: %v", dml)
	}

	// Glade all-pass vs SF with assertion fail → comparison fail.
	gladeAllPass, _ := normalizeGladeTests(gladeTestAllPassJSON(), cases)
	comp2 := CompareReports(Report{Results: sfResults}, Report{Results: gladeAllPass}, cases)
	dml2 := findComp(comp2, "SF-LIFECYCLE-DML-ALL-OR-NONE")
	if dml2 == nil || dml2.Status != StatusFail {
		t.Errorf("assertion fail vs pass should fail: %v", dml2)
	}
}

// --- Test 11: unknown status inconclusive ---

func TestProjectOracleLifecycleUnknownStatusInconclusive(t *testing.T) {
	cases := ProjectOracleCases()
	unknown := []byte(`{
  "summary":{"total":1,"passed":0,"failed":0,"skipped":0,"compileErrors":0,"runtimeErrors":0,"unsupported":0,"errors":0},
  "tests":[
    {"className":"CorrectnessProbeTest","methodName":"sfLifecycleDiscovery","status":"unknown-xyz"}
  ]
}`)
	_, err := normalizeGladeTests(unknown, cases)
	if err == nil {
		t.Fatal("expected error for unknown status")
	}
	if !strings.Contains(err.Error(), "incomplete-output") {
		t.Errorf("error = %v", err)
	}
}

// --- Test 12: table-driven inconclusive conditions ---

func TestProjectOracleLifecycleInconclusiveTable(t *testing.T) {
	cases := ProjectOracleCases()

	tests := []struct {
		name     string
		fake     *fakeProjectRunnerSeq
		wantErr  string
	}{
		{"deploy process fail",   &fakeProjectRunnerSeq{deploy: fakeProjectOutput{err: errors.New("exec: sf: not found")}}, "deploy-failed"},
		{"deploy JSON malformed", &fakeProjectRunnerSeq{deploy: fakeProjectOutput{out: []byte("not json")}}, "deploy-malformed"},
		{"deploy non-Succeeded",  &fakeProjectRunnerSeq{deploy: fakeProjectOutput{out: sfDeployFailJSON()}}, "deploy-failed"},
		{"test process fail + bad output", &fakeProjectRunnerSeq{
			deploy: fakeProjectOutput{out: sfDeploySuccessJSON()},
			test:   fakeProjectOutput{err: errors.New("exec: sf: not found"), out: []byte("not json")},
		}, "test-failed"},
		{"test malformed JSON", &fakeProjectRunnerSeq{
			deploy: fakeProjectOutput{out: sfDeploySuccessJSON()},
			test:   fakeProjectOutput{out: []byte("not json")},
		}, "test-malformed"},
		{"test empty output", &fakeProjectRunnerSeq{
			deploy: fakeProjectOutput{out: sfDeploySuccessJSON()},
			test:   fakeProjectOutput{out: []byte{}},
		}, "test-malformed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := RunProjectOracle(context.Background(), tt.fake, "/proj", "org", cases)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want %q", err.Error(), tt.wantErr)
			}
		})
	}

	// Glade table-driven inconclusive.
	gladeTests := []struct {
		name    string
		out     []byte
		err     error
		wantErr string
	}{
		{"glade process error", []byte("not json"), errors.New("exec: not found"), "glade-command-failed"},
		{"glade malformed", []byte("not json"), nil, "glade-malformed-output"},
		{"glade empty", []byte{}, nil, "glade-empty-output"},
		{"glade missing method", []byte(`{"summary":{"total":1,"passed":0,"failed":0,"skipped":0,"compileErrors":0,"runtimeErrors":0,"unsupported":0,"errors":0},"tests":[{"className":"CorrectnessProbeTest","methodName":"noSuch","status":"pass"}]}`), nil, "glade-incomplete-output"},
		{"glade duplicate method", []byte(`{"summary":{"total":2,"passed":1,"failed":0,"skipped":0,"compileErrors":0,"runtimeErrors":0,"unsupported":0,"errors":0},"tests":[{"className":"CorrectnessProbeTest","methodName":"sfLifecycleDiscovery","status":"pass"},{"className":"CorrectnessProbeTest","methodName":"sfLifecycleDiscovery","status":"pass"}]}`), nil, "glade-incomplete-output"},
		{"glade wrong class", []byte(`{"summary":{"total":1,"passed":0,"failed":0,"skipped":0,"compileErrors":0,"runtimeErrors":0,"unsupported":0,"errors":0},"tests":[{"className":"WrongClass","methodName":"sfLifecycleDiscovery","status":"pass"}]}`), nil, "glade-incomplete-output"},
		{"glade summary mismatch", []byte(`{"summary":{"total":99,"passed":1,"failed":0,"skipped":0,"compileErrors":0,"runtimeErrors":0,"unsupported":0,"errors":0},"tests":[{"className":"CorrectnessProbeTest","methodName":"sfLifecycleDiscovery","status":"pass"}]}`), nil, "glade-incomplete-output"},
	}
	for _, tt := range gladeTests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeCmdRunner{output: tt.out, err: tt.err}
			_, err := RunGladeProject(context.Background(), fake, GladeProjectOptions{
				GladeBin: "/glade", ProjectDir: "/proj",
			}, cases)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want %q", err.Error(), tt.wantErr)
			}
		})
	}
}

// --- Test 12b: fail with no problem object is inconclusive ---

func TestProjectOracleLifecycleFailWithoutProblemInconclusive(t *testing.T) {
	cases := ProjectOracleCases()
	// A failed test method with no problem object cannot be classified.
	failNoProblem := []byte(`{
  "summary":{"total":1,"passed":0,"failed":1,"skipped":0,"compileErrors":0,"runtimeErrors":0,"unsupported":0,"errors":0},
  "tests":[
    {"className":"CorrectnessProbeTest","methodName":"sfLifecycleDiscovery","status":"fail"}
  ]
}`)
	_, err := normalizeGladeTests(failNoProblem, cases)
	if err == nil {
		t.Fatal("expected error for fail without problem")
	}
	if !strings.Contains(err.Error(), "incomplete-output") {
		t.Errorf("error = %v, want incomplete-output", err)
	}
}

// --- Test 13: missing/operational → inconclusive comparison ---

func TestProjectOracleLifecycleComparisonInconclusive(t *testing.T) {
	cases := ProjectOracleCases()
	sfResults, _ := normalizeSFTests(sfTestPassJSON(), cases)
	// Missing Glade.
	comp := CompareReports(Report{Results: sfResults}, Report{}, cases)
	for _, c := range comp {
		if c.Status != StatusInconclusive {
			t.Errorf("%s = %s, want inconclusive", c.CaseID, c.Status)
		}
	}
	// Both missing.
	comp2 := CompareReports(Report{}, Report{}, cases)
	for _, c := range comp2 {
		if c.Status != StatusInconclusive {
			t.Errorf("%s = %s, want inconclusive", c.CaseID, c.Status)
		}
	}
}

// --- Test 14: full comparison pass ---

func TestProjectOracleLifecycleComparisonPass(t *testing.T) {
	cases := ProjectOracleCases()
	sf, _ := normalizeSFTests(sfTestPassJSON(), cases)
	gl, _ := normalizeGladeTests(gladeTestAllPassJSON(), cases)
	for _, cc := range CompareReports(Report{Results: sf}, Report{Results: gl}, cases) {
		if cc.Status != StatusPass {
			t.Errorf("%s = %s", cc.CaseID, cc.Status)
		}
	}
}

// --- Test 15: comparison fail ---

func TestProjectOracleLifecycleComparisonFail(t *testing.T) {
	cases := ProjectOracleCases()
	sf, _ := normalizeSFTests(sfTestAssertFailJSON(), cases)
	gl, _ := normalizeGladeTests(gladeTestAllPassJSON(), cases)
	failCount := 0
	for _, cc := range CompareReports(Report{Results: sf}, Report{Results: gl}, cases) {
		if cc.Status == StatusFail {
			failCount++
		}
	}
	if failCount == 0 {
		t.Error("expected at least one fail")
	}
}

// --- Test 16: redaction — no credential/raw output leaks ---

func TestProjectOracleLifecycleRedaction(t *testing.T) {
	cases := ProjectOracleCases()
	fake := &fakeProjectRunnerSeq{
		deploy: fakeProjectOutput{out: sfDeploySuccessJSON()},
		test:   fakeProjectOutput{out: sfTestPassJSON()},
	}
	report, err := RunProjectOracle(context.Background(), fake, "/proj", "my-org-alias", cases)
	if err != nil {
		t.Fatalf("RunProjectOracle: %v", err)
	}

	// Report must never carry TargetOrg.
	if report.TargetOrg != "" {
		t.Errorf("TargetOrg leak: %q", report.TargetOrg)
	}
	if report.Username != "" {
		t.Errorf("Username leak: %q", report.Username)
	}
	if report.OrgID != "" {
		t.Errorf("OrgID leak: %q", report.OrgID)
	}

	// Results must never contain raw IDs or aliases.
	for _, r := range report.Results {
		if strings.HasPrefix(r.ID, "707") || strings.HasPrefix(r.ID, "01p") {
			t.Errorf("generated ID in case: %q", r.ID)
		}
		if r.Value != nil && (strings.Contains(*r.Value, "707") || strings.Contains(*r.Value, "my-org-alias")) {
			t.Errorf("raw value leak: %q", *r.Value)
		}
	}

	// RedactReport must clear all identity fields.
	rr := RedactReport(report)
	if rr.TargetOrg != "" || rr.Username != "" || rr.OrgID != "" {
		t.Errorf("RedactReport failed: %+v", rr)
	}

	// Hostile fake output with credentials must not leak through errors.
	hostileFake := &fakeProjectRunnerSeq{
		deploy: fakeProjectOutput{out: sfDeploySuccessJSON()},
		test:   fakeProjectOutput{out: []byte("bad alias my-org-alias user@test.com 00DXXXXXXXXXX")},
	}
	_, err2 := RunProjectOracle(context.Background(), hostileFake, "/proj", "my-org-alias", cases)
	if err2 == nil {
		t.Fatal("expected error")
	}
	errStr := err2.Error()
	for _, sentinel := range []string{"my-org-alias", "user@test.com", "00DXXXX"} {
		if strings.Contains(errStr, sentinel) {
			t.Errorf("error leaks %q: %s", sentinel, errStr)
		}
	}
}

// --- Test 17: Glade error redaction (no raw output) ---

func TestProjectOracleLifecycleGladeErrorRedaction(t *testing.T) {
	cases := ProjectOracleCases()
	// Glade fails with raw output containing paths, IDs, etc.
	fake := &fakeCmdRunner{
		err:    errors.New("exit status 1"),
		output: []byte("raw path /home/user/project output with 00DXXXXX org ID and user@test.com"),
	}
	_, err := RunGladeProject(context.Background(), fake, GladeProjectOptions{
		GladeBin: "/glade", ProjectDir: "/proj",
	}, cases)
	if err == nil {
		t.Fatal("expected error")
	}
	errStr := err.Error()
	for _, sentinel := range []string{"00DXXXXX", "user@test.com", "raw path", "/home/user"} {
		if strings.Contains(errStr, sentinel) {
			t.Errorf("error leaks %q: %s", sentinel, errStr)
		}
	}
}

// --- Test 18: Glade error stable category ---

func TestProjectOracleLifecycleGladeErrorCategory(t *testing.T) {
	cases := ProjectOracleCases()
	tests := []struct {
		name    string
		out     []byte
		err     error
		wantCat string
	}{
		{"process error", []byte("bad"), errors.New("exec: not found"), "glade-command-failed"},
		{"malformed", []byte("bad"), nil, "glade-malformed-output"},
		{"empty", []byte{}, nil, "glade-empty-output"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeCmdRunner{output: tt.out, err: tt.err}
			_, err := RunGladeProject(context.Background(), fake, GladeProjectOptions{
				GladeBin: "/glade", ProjectDir: "/proj",
			}, cases)
			if err == nil || !strings.Contains(err.Error(), tt.wantCat) {
				t.Errorf("error = %v, want %s", err, tt.wantCat)
			}
		})
	}
}

// --- Test 19: context timeout ---

func TestProjectOracleLifecycleContextTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	time.Sleep(1 * time.Millisecond)
	defer cancel()
	_, err := RunProjectOracle(ctx, RealProjectRunner{}, "/proj", "org", ProjectOracleCases())
	if err == nil {
		t.Fatal("expected timeout")
	}
}

// --- Test 20: full pipeline with artifact shape ---

func TestProjectOracleLifecycleFullPipeline(t *testing.T) {
	cases := ProjectOracleCases()
	fake := &fakeProjectRunnerSeq{
		deploy: fakeProjectOutput{out: sfDeploySuccessJSON()},
		test:   fakeProjectOutput{out: sfTestPassJSON()},
	}
	sfReport, err := RunProjectOracle(context.Background(), fake, "/proj", "org", cases)
	if err != nil {
		t.Fatalf("SF: %v", err)
	}
	gladeRunner := &fakeCmdRunner{output: gladeTestAllPassJSON()}
	gladeReport, err := RunGladeProject(context.Background(), gladeRunner, GladeProjectOptions{
		GladeBin: "/glade", ProjectDir: "/proj",
	}, cases)
	if err != nil {
		t.Fatalf("Glade: %v", err)
	}
	for _, cc := range CompareReports(sfReport, gladeReport, cases) {
		if cc.Status != StatusPass {
			t.Errorf("%s = %s", cc.CaseID, cc.Status)
		}
	}
}

// --- Test 21: fixture structure — top-level batch class and no nested Batchable ---

func TestProjectOracleLifecycleFixtureStructure(t *testing.T) {
	batchPath := "../../testdata/salesforce-correctness/force-app/main/default/classes/CorrectnessProbeBatch.cls"
	metaPath := "../../testdata/salesforce-correctness/force-app/main/default/classes/CorrectnessProbeBatch.cls-meta.xml"
	probePath := "../../testdata/salesforce-correctness/force-app/main/default/classes/CorrectnessProbe.cls"

	batch, err := os.ReadFile(batchPath)
	if err != nil {
		t.Fatalf("CorrectnessProbeBatch.cls missing: %v", err)
	}
	if !strings.Contains(string(batch), "implements Database.Batchable<SObject>") {
		t.Error("CorrectnessProbeBatch.cls missing Batchable implementation declaration")
	}
	if !strings.Contains(string(batch), "Database.QueryLocator start") {
		t.Error("CorrectnessProbeBatch.cls missing start method")
	}
	if !strings.Contains(string(batch), "batch-side-effect") {
		t.Error("CorrectnessProbeBatch.cls missing batch-side-effect insert")
	}
	if !strings.Contains(string(batch), "void finish(Database.BatchableContext") {
		t.Error("CorrectnessProbeBatch.cls missing finish method")
	}

	meta, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("CorrectnessProbeBatch.cls-meta.xml missing: %v", err)
	}
	if !strings.Contains(string(meta), "<ApexClass") {
		t.Error("CorrectnessProbeBatch.cls-meta.xml missing ApexClass element")
	}

	probe, err := os.ReadFile(probePath)
	if err != nil {
		t.Fatalf("CorrectnessProbe.cls missing: %v", err)
	}
	if strings.Contains(string(probe), "class BatchJob implements Database.Batchable") {
		t.Error("CorrectnessProbe.cls still contains nested Batchable — must be removed")
	}
	if !strings.Contains(string(probe), "new CorrectnessProbeBatch()") {
		t.Error("CorrectnessProbe.executeBatch() must instantiate CorrectnessProbeBatch")
	}
	if !strings.Contains(string(probe), "Database.executeBatch(new CorrectnessProbeBatch(), 1)") {
		t.Error("CorrectnessProbe.executeBatch() must pass scope size 1 to Database.executeBatch")
	}
}

// --- helper ---

func findComp(ccs []CaseComparison, id string) *CaseComparison {
	for i := range ccs {
		if ccs[i].CaseID == id {
			return &ccs[i]
		}
	}
	return nil
}
