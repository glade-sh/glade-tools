package oracleprobe

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// ProjectCmdRunner abstracts a command execution that sets a working
// directory. Tests inject a fake; the real runner uses exec. Exported
// so the verifier can invoke the runner without duplicating an executor.
type ProjectCmdRunner interface {
	RunDir(ctx context.Context, dir, name string, arg ...string) ([]byte, error)
}

// RealProjectRunner is the OS-backed implementation of ProjectCmdRunner.
type RealProjectRunner struct{}

func (RealProjectRunner) RunDir(ctx context.Context, dir, name string, arg ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, arg...)
	cmd.Dir = dir
	return cmd.Output()
}

// ProjectOracleCases returns the twelve lifecycle probe cases in
// deterministic catalog order.
func ProjectOracleCases() []Case {
	return []Case{
		{ID: "SF-LIFECYCLE-DISCOVERY", Area: "Lifecycle", API: "CorrectnessProbeTest", Mode: ModeDeploy, Statements: []string{"sfLifecycleDiscovery"}, ValueType: "assertion"},
		{ID: "SF-LIFECYCLE-TEST-SETUP-EXECUTION", Area: "Lifecycle", API: "CorrectnessProbeTest", Mode: ModeDeploy, Statements: []string{"sfLifecycleTestSetupExecution"}, ValueType: "assertion"},
		{ID: "SF-LIFECYCLE-TEST-SETUP-COPY", Area: "Lifecycle", API: "CorrectnessProbeTest", Mode: ModeDeploy, Statements: []string{"sfLifecycleTestSetupCopy"}, ValueType: "assertion"},
		{ID: "SF-LIFECYCLE-METHOD-ROLLBACK", Area: "Lifecycle", API: "CorrectnessProbeTest", Mode: ModeDeploy, Statements: []string{"sfLifecycleMethodRollback"}, ValueType: "assertion"},
		{ID: "SF-LIFECYCLE-STATIC-RESET", Area: "Lifecycle", API: "CorrectnessProbeTest", Mode: ModeDeploy, Statements: []string{"sfLifecycleStaticReset"}, ValueType: "assertion"},
		{ID: "SF-LIFECYCLE-START-TEST-LIMITS", Area: "Lifecycle", API: "CorrectnessProbeTest", Mode: ModeDeploy, Statements: []string{"sfLifecycleStartTestLimits"}, ValueType: "assertion"},
		{ID: "SF-LIFECYCLE-STOP-TEST-FUTURE-QUEUEABLE", Area: "Lifecycle", API: "CorrectnessProbeTest", Mode: ModeDeploy, Statements: []string{"sfLifecycleStopTestFutureQueueable"}, ValueType: "assertion"},
		{ID: "SF-LIFECYCLE-STOP-TEST-BATCH-SCHEDULED", Area: "Lifecycle", API: "CorrectnessProbeTest", Mode: ModeDeploy, Statements: []string{"sfLifecycleStopTestBatchScheduled"}, ValueType: "assertion"},
		{ID: "SF-LIFECYCLE-HTTP-CALLOUT-MOCK", Area: "Lifecycle", API: "CorrectnessProbeTest", Mode: ModeDeploy, Statements: []string{"sfLifecycleHttpCalloutMock"}, ValueType: "assertion"},
		{ID: "SF-LIFECYCLE-DML-ALL-OR-NONE", Area: "Lifecycle", API: "CorrectnessProbeTest", Mode: ModeDeploy, Statements: []string{"sfLifecycleDmlAllOrNone"}, ValueType: "assertion"},
		{ID: "SF-LIFECYCLE-TRANSACTION-VISIBILITY", Area: "Lifecycle", API: "CorrectnessProbeTest", Mode: ModeDeploy, Statements: []string{"sfLifecycleTransactionVisibility"}, ValueType: "assertion"},
		{ID: "SF-LIFECYCLE-RUN-AS", Area: "Lifecycle", API: "CorrectnessProbeTest", Mode: ModeDeploy, Statements: []string{"sfLifecycleRunAs"}, ValueType: "assertion"},
	}
}

// buildMethodCaseMap builds a lookup from test method name to Case.
func buildMethodCaseMap(cases []Case) map[string]Case {
	m := make(map[string]Case, len(cases))
	for _, c := range cases {
		if len(c.Statements) > 0 {
			m[c.Statements[0]] = c
		}
	}
	return m
}

// RunProjectOracle deploys the project and runs the CorrectnessProbeTest
// class, then normalizes the twelve lifecycle observations into a Report.
// The report never carries a TargetOrg value.
func RunProjectOracle(ctx context.Context, runner ProjectCmdRunner, projectDir string, targetOrg string, cases []Case) (Report, error) {
	if err := ctx.Err(); err != nil {
		return Report{}, fmt.Errorf("project-oracle: %w", err)
	}

	// 1. Deploy.
	deployArgs := []string{"project", "deploy", "start", "--source-dir", "force-app", "--target-org", targetOrg, "--wait", "30", "--json"}
	deployOut, deployErr := runner.RunDir(ctx, projectDir, "sf", deployArgs...)
	if deployErr != nil {
		if ctx.Err() != nil {
			return Report{}, fmt.Errorf("timeout")
		}
		return Report{}, fmt.Errorf("deploy-failed")
	}
	var deployRes sfDeployJSON
	if err := json.Unmarshal(deployOut, &deployRes); err != nil {
		return Report{}, fmt.Errorf("deploy-malformed")
	}
	if deployRes.Result.Status != "Succeeded" {
		return Report{}, fmt.Errorf("deploy-failed")
	}

	// 2. Run tests.
	testArgs := []string{"apex", "run", "test", "--class-names", "CorrectnessProbeTest", "--target-org", targetOrg, "--synchronous", "--result-format", "json", "--json"}
	testOut, testErr := runner.RunDir(ctx, projectDir, "sf", testArgs...)

	results, parseErr := normalizeSFTests(testOut, cases)
	if parseErr != nil {
		if ctx.Err() != nil {
			return Report{}, fmt.Errorf("timeout")
		}
		if testErr != nil {
			return Report{}, fmt.Errorf("test-failed")
		}
		return Report{}, fmt.Errorf("test-malformed")
	}

	_ = testErr // nonzero exit acceptable when JSON is valid

	return Report{Results: results}, nil
}

// ------ Salesforce JSON decoders ------

type sfDeployJSON struct {
	Status int `json:"status"`
	Result struct {
		Status string `json:"status"`
	} `json:"result"`
}

type sfTestJSON struct {
	Status int `json:"status"`
	Result struct {
		Summary struct {
			Outcome  string `json:"outcome"`
			TestsRan int    `json:"testsRan"`
			Passing  int    `json:"passing"`
			Failing  int    `json:"failing"`
			Skipped  int    `json:"skipped"`
		} `json:"summary"`
		Tests []struct {
			MethodName string `json:"MethodName"`
			Outcome    string `json:"Outcome"`
			Message    string `json:"Message"`
			StackTrace string `json:"StackTrace"`
			ApexClass  struct {
				Name string `json:"Name"`
			} `json:"ApexClass"`
		} `json:"tests"`
	} `json:"result"`
}

// normalizeSFTests parses Salesforce test-run JSON and returns one Result
// per case in deterministic catalog order.
func normalizeSFTests(raw []byte, cases []Case) ([]Result, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("empty-output")
	}
	var parsed sfTestJSON
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("malformed-output")
	}

	methodMap := buildMethodCaseMap(cases)

	seen := map[string]bool{}
	byMethod := map[string]Result{}
	for _, test := range parsed.Result.Tests {
		method := test.MethodName
		c, ok := methodMap[method]
		if !ok {
			return nil, fmt.Errorf("incomplete-output")
		}
		if seen[method] {
			return nil, fmt.Errorf("incomplete-output")
		}
		seen[method] = true

		if test.ApexClass.Name != "CorrectnessProbeTest" {
			return nil, fmt.Errorf("incomplete-output")
		}

		r := Result{
			ID:   c.ID,
			Area: "Lifecycle",
			API:  fmt.Sprintf("%s.%s", test.ApexClass.Name, method),
			Mode: ModeDeploy,
		}

		switch test.Outcome {
		case "Pass":
			r.HasValue = true
			v := "pass"
			r.Value = &v
			r.ValueType = "assertion"
		case "Fail":
			cat := sfExceptionCategory(test.Message, test.StackTrace)
			if cat == "" {
				return nil, fmt.Errorf("incomplete-output")
			}
			r.HasValue = true
			v := "fail"
			r.Value = &v
			r.ValueType = cat
		case "CompileFail":
			r.HasValue = true
			v := "fail"
			r.Value = &v
			r.ValueType = "compile"
		default:
			return nil, fmt.Errorf("incomplete-output")
		}

		byMethod[method] = r
	}

	// Validate summary counts against normalized results.
	results := resultsInCatalogOrder(cases, byMethod)
	if parsed.Result.Summary.TestsRan != len(results) {
		return nil, fmt.Errorf("incomplete-output")
	}
	if parsed.Result.Summary.Skipped > 0 {
		return nil, fmt.Errorf("incomplete-output")
	}
	passCount, failCount := countPassFail(results)
	if passCount != parsed.Result.Summary.Passing || failCount != parsed.Result.Summary.Failing {
		return nil, fmt.Errorf("incomplete-output")
	}

	return results, nil
}

// sfExceptionCategory extracts an assertion category from Salesforce
// test failure text. Returns "" when the category cannot be classified.
func sfExceptionCategory(message, stackTrace string) string {
	text := message + " " + stackTrace
	for _, pair := range [][2]string{
		{"System.AssertException", "assertion"},
		{"System.MathException", "runtime"},
		{"System.NullPointerException", "runtime"},
		{"System.DmlException", "runtime"},
		{"System.CalloutException", "runtime"},
		{"System.LimitException", "runtime"},
		{"System.QueryException", "runtime"},
		{"System.SObjectException", "runtime"},
		{"System.TypeException", "runtime"},
		{"System.StringException", "runtime"},
		{"System.NoSuchElementException", "runtime"},
		{"System.ListException", "runtime"},
		{"System.JSONException", "runtime"},
		{"System.AsyncException", "runtime"},
		{"System.FinalException", "compile"},
		{"System.NoAccessException", "load"},
	} {
		if strings.Contains(text, pair[0]) {
			return pair[1]
		}
	}
	return "" // unclassified — caller must treat as inconclusive
}

// ------ Glade JSON decoders (exact real artifact shape) ------

type gladeTestJSON struct {
	Status   string `json:"status"`
	ExitCode int    `json:"exitCode"`
	Summary  struct {
		Total          int `json:"total"`
		Passed         int `json:"passed"`
		Failed         int `json:"failed"`
		Skipped        int `json:"skipped"`
		CompileErrors  int `json:"compileErrors"`
		RuntimeErrors  int `json:"runtimeErrors"`
		Unsupported    int `json:"unsupported"`
		Errors         int `json:"errors"`
	} `json:"summary"`
	Tests []struct {
		ClassName  string `json:"className"`
		MethodName string `json:"methodName"`
		Status     string `json:"status"`
		Problem    *struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"problem"`
	} `json:"tests"`
}

// GladeProjectOptions holds configuration for running Glade in project mode.
type GladeProjectOptions struct {
	GladeBin   string
	ProjectDir string
}

// RunGladeProject runs the Glade CLI in project test mode and returns a
// normalized Report. A nonzero exit with complete valid JSON is behavioral
// evidence, not operational failure. Raw command output never appears in
// returned errors.
func RunGladeProject(ctx context.Context, runner CmdRunner, opts GladeProjectOptions, cases []Case) (Report, error) {
	if err := ctx.Err(); err != nil {
		return Report{}, fmt.Errorf("glade-timeout")
	}

	args := []string{"test", "--project", opts.ProjectDir, "--json"}
	out, cmdErr := runner.RunContext(ctx, opts.GladeBin, args...)

	results, parseErr := normalizeGladeTests(out, cases)
	if parseErr != nil {
		if ctx.Err() != nil {
			return Report{}, fmt.Errorf("glade-timeout")
		}
		if cmdErr != nil {
			return Report{}, fmt.Errorf("glade-command-failed")
		}
		return Report{}, fmt.Errorf("glade-%s", parseErr.Error())
	}

	_ = cmdErr // nonzero exit acceptable when JSON is valid

	return Report{Results: results}, nil
}

// normalizeGladeTests parses Glade test-run JSON and returns one Result
// per case in deterministic catalog order.
func normalizeGladeTests(raw []byte, cases []Case) ([]Result, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("empty-output")
	}
	var parsed gladeTestJSON
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("malformed-output")
	}

	methodMap := buildMethodCaseMap(cases)

	seen := map[string]bool{}
	byMethod := map[string]Result{}
	for _, test := range parsed.Tests {
		method := test.MethodName
		c, ok := methodMap[method]
		if !ok {
			return nil, fmt.Errorf("incomplete-output")
		}
		if seen[method] {
			return nil, fmt.Errorf("incomplete-output")
		}
		seen[method] = true

		if test.ClassName != "CorrectnessProbeTest" {
			return nil, fmt.Errorf("incomplete-output")
		}

		r := Result{
			ID:   c.ID,
			Area: "Lifecycle",
			API:  fmt.Sprintf("%s.%s", test.ClassName, method),
			Mode: ModeDeploy,
		}

		switch test.Status {
		case "pass":
			r.HasValue = true
			v := "pass"
			r.Value = &v
			r.ValueType = "assertion"
		case "fail":
			cat := gladeProblemCategory(test.Problem)
			if cat == "" {
				return nil, fmt.Errorf("incomplete-output")
			}
			r.HasValue = true
			v := "fail"
			r.Value = &v
			r.ValueType = cat
		default:
			return nil, fmt.Errorf("incomplete-output")
		}

		byMethod[method] = r
	}

	// Validate summary counts against normalized results.
	results := resultsInCatalogOrder(cases, byMethod)
	if parsed.Summary.Total != len(results) {
		return nil, fmt.Errorf("incomplete-output")
	}
	if parsed.Summary.Skipped > 0 {
		return nil, fmt.Errorf("incomplete-output")
	}
	// Any nonzero operational/unsupported count that isn't represented by
	// per-method results is inconclusive.
	if parsed.Summary.CompileErrors > 0 || parsed.Summary.RuntimeErrors > 0 ||
		parsed.Summary.Unsupported > 0 || parsed.Summary.Errors > 0 {
		return nil, fmt.Errorf("incomplete-output")
	}
	passCount, failCount := countPassFail(results)
	if passCount != parsed.Summary.Passed || failCount != parsed.Summary.Failed {
		return nil, fmt.Errorf("incomplete-output")
	}

	return results, nil
}

// gladeProblemCategory maps a Glade problem type to an assertion category.
// Returns "" when the category cannot be classified.
func gladeProblemCategory(p *struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}) string {
	if p == nil {
		return "" // unclassified — a fail without a problem is not categorizable
	}
	switch {
	case strings.Contains(p.Type, "AssertException"):
		return "assertion"
	case strings.Contains(p.Type, "RuntimeError"):
		return "runtime"
	case strings.Contains(p.Type, "CompileError"):
		return "compile"
	case strings.Contains(p.Type, "LoadError"):
		return "load"
	default:
		return "" // unclassified — caller must treat as inconclusive
	}
}

// ------ result ordering ------

func countPassFail(results []Result) (pass, fail int) {
	for _, r := range results {
		if r.Value == nil {
			continue
		}
		switch *r.Value {
		case "pass":
			pass++
		case "fail":
			fail++
		}
	}
	return
}

func resultsInCatalogOrder(cases []Case, byMethod map[string]Result) []Result {
	results := make([]Result, 0, len(cases))
	for _, c := range cases {
		method := c.Statements[0]
		if r, ok := byMethod[method]; ok {
			results = append(results, r)
		}
	}
	return results
}
