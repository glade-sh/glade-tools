package compat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/internal/testreport"
)

const fullLocalTestFixturesEnv = "GLADE_TOOLS_RUN_FULL_LOCAL_TEST_FIXTURES"

var localTestRunSlots = make(chan struct{}, 1)

type localTestReadyFixture struct {
	name  string
	total int
}

func TestLocalTestFixtureExecutionSelection(t *testing.T) {
	t.Setenv(fullLocalTestFixturesEnv, "")
	if shouldRunLocalTestFixture("platform-apis") {
		t.Fatal("large local-test fixture should not run without the opt-in environment variable")
	}
	if shouldRunLocalTestFixture("enterprise-composed") {
		t.Fatal("full local-test fixture should not run without the opt-in environment variable")
	}

	t.Setenv(fullLocalTestFixturesEnv, "1")
	if !shouldRunLocalTestFixture("platform-apis") {
		t.Fatal("large local-test fixture should run when the opt-in environment variable is set")
	}
	if !shouldRunLocalTestFixture("enterprise-composed") {
		t.Fatal("full local-test fixture should run when the opt-in environment variable is set")
	}
}

func TestRunLocalTestsNoDiskCacheDoesNotWriteStartupCache(t *testing.T) {
	root := t.TempDir()
	writeLocalTestFile(t, filepath.Join(root, "sfdx-project.json"), `{"packageDirectories":[{"path":"force-app","default":true}]}`)
	writeLocalTestFile(t, filepath.Join(root, "force-app/main/default/classes/NoDiskCacheTest.cls"), `
@isTest
private class NoDiskCacheTest {
  @isTest static void runs() {
    System.assertEquals(1, 1);
  }
}
`)
	report, err := RunLocalTests(LocalTestOptions{Project: root, NoDiskCache: true})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Ready || report.Summary.Pass != 1 {
		t.Fatalf("report = %#v", report)
	}
	if _, err := os.Stat(filepath.Join(root, ".glade", "test", "startup.gob")); !os.IsNotExist(err) {
		t.Fatalf("startup cache stat err = %v, want not exist", err)
	}
}

func shouldRunLocalTestFixture(string) bool {
	return os.Getenv(fullLocalTestFixturesEnv) != ""
}

func requireFullLocalTestFixtures(t *testing.T) {
	t.Helper()
	if os.Getenv(fullLocalTestFixturesEnv) == "" {
		t.Skipf("local-test corpus fixture skipped; set %s=1 to run the full sweep", fullLocalTestFixturesEnv)
	}
}

func runLocalTestReadyFixture(t *testing.T, fixture localTestReadyFixture) {
	t.Helper()
	if !shouldRunLocalTestFixture(fixture.name) {
		t.Skipf("local-test fixture skipped; set %s=1 to run the full sweep", fullLocalTestFixturesEnv)
	}
	report, err := runLocalTestsForTest(t, LocalTestOptions{Project: filepath.Join("..", "..", "testdata", "local-tests", fixture.name)})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Ready {
		t.Fatalf("ready = false, summary = %#v outcomes = %#v", report.Summary, report.Outcomes)
	}
	if report.Summary.Total != fixture.total || report.Summary.Pass != fixture.total || report.Summary.CompileErrors != 0 || report.Summary.Unsupported != 0 {
		t.Fatalf("summary = %#v", report.Summary)
	}
}

func runLocalTestsForTest(t *testing.T, options LocalTestOptions) (LocalTestReport, error) {
	t.Helper()
	localTestRunSlots <- struct{}{}
	t.Cleanup(func() {
		<-localTestRunSlots
	})
	options.NoDiskCache = true
	return RunLocalTests(options)
}

func TestRunLocalTestsClassifiesBasicFixture(t *testing.T) {
	t.Parallel()
	report, err := runLocalTestsForTest(t, LocalTestOptions{Project: filepath.Join("..", "..", "testdata", "local-tests", "basic"), TraceBlocked: true})
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.Total != 3 || report.Summary.Pass != 1 || report.Summary.AssertFailures != 1 || report.Summary.Unsupported != 1 {
		t.Fatalf("summary = %#v", report.Summary)
	}
	if report.Ready {
		t.Fatalf("ready = true, want false")
	}
	var failing LocalTestOutcome
	for _, outcome := range report.Outcomes {
		if outcome.Class == "FailingTest" {
			failing = outcome
			break
		}
	}
	if failing.TraceEvents == 0 || failing.ProfileEvents == 0 || len(failing.ProfileCategories) == 0 {
		t.Fatalf("failing outcome missing trace/profile summary: %#v", failing)
	}
}

func TestRunLocalTestsProgressShowsCountsElapsedAndETA(t *testing.T) {
	t.Parallel()
	var progress bytes.Buffer
	report, err := runLocalTestsForTest(t, LocalTestOptions{
		Project:        filepath.Join("..", "..", "testdata", "local-tests", "basic"),
		ProgressWriter: &progress,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.CasesRun != 3 {
		t.Fatalf("casesRun = %d, want 3", report.CasesRun)
	}
	out := progress.String()
	for _, want := range []string{
		"Phase: load_start elapsed=",
		"Phase: run_start elapsed=",
		"Progress: 3/3",
		"elapsed=",
		"eta=",
		"pass=1",
		"fail=2",
		"error=0",
		"running=UnsupportedTest.unsupported",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("progress missing %q:\n%s", want, out)
		}
	}
}

func TestRunLocalTestsPerfJSONIncludesCloneAndAllocationCounters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "perf.json")
	report, err := runLocalTestsForTest(t, LocalTestOptions{
		Project:      filepath.Join("..", "..", "testdata", "local-tests", "basic"),
		PerfJSONPath: path,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.CasesRun == 0 {
		t.Fatalf("expected local tests to run: %#v", report)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var perf LocalTestPerfSummary
	if err := json.Unmarshal(data, &perf); err != nil {
		t.Fatal(err)
	}
	if perf.CloneStats.CloneRuntimeOrgCalls == 0 {
		t.Fatalf("cloneRuntimeOrg calls were not counted: %#v", perf.CloneStats)
	}
	if perf.CloneStats.CloneRuntimeCalls == 0 {
		t.Fatalf("runtime clone calls were not counted: %#v", perf.CloneStats)
	}
	if len(perf.TopCloneClasses) > 0 && perf.TopCloneClasses[0].TestClones == 0 && perf.TopCloneClasses[0].SetupClones == 0 {
		t.Fatalf("topCloneClasses missing clone counts: %#v", perf.TopCloneClasses)
	}
	if len(perf.Phases) == 0 || perf.Phases[0].TotalAllocBytes == 0 {
		t.Fatalf("phase allocation counters missing: %#v", perf.Phases)
	}
}

func TestRunLocalTestsSkipsTraceByDefault(t *testing.T) {
	t.Parallel()
	report, err := runLocalTestsForTest(t, LocalTestOptions{Project: filepath.Join("..", "..", "testdata", "local-tests", "basic")})
	if err != nil {
		t.Fatal(err)
	}
	for _, outcome := range report.Outcomes {
		if outcome.TraceEvents != 0 || outcome.ProfileEvents != 0 || len(outcome.ProfileCategories) != 0 {
			t.Fatalf("default outcome should not include trace/profile: %#v", outcome)
		}
	}
}

func TestRunLocalTestsReportsTopFailures(t *testing.T) {
	t.Parallel()
	report, err := runLocalTestsForTest(t, LocalTestOptions{
		Project:     filepath.Join("..", "..", "testdata", "local-tests", "basic"),
		TopFailures: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.TopFailures) != 2 {
		t.Fatalf("topFailures = %#v", report.TopFailures)
	}
	if report.TopFailures[0].Count == 0 || report.TopFailures[0].Outcome == "pass" {
		t.Fatalf("topFailures[0] = %#v", report.TopFailures[0])
	}
	if len(report.TopFailures[0].Samples) == 0 {
		t.Fatalf("topFailures[0] missing samples: %#v", report.TopFailures[0])
	}
}

func TestRunLocalTestsFiltersClassList(t *testing.T) {
	t.Parallel()
	report, err := runLocalTestsForTest(t, LocalTestOptions{
		Project:   filepath.Join("..", "..", "testdata", "local-tests", "basic"),
		ClassList: []string{"PassingTest"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.CasesDiscovered == 0 {
		t.Fatalf("expected filtered cases: %#v", report)
	}
	for _, outcome := range report.Outcomes {
		if outcome.Class != "PassingTest" {
			t.Fatalf("unexpected class %q in %#v", outcome.Class, report.Outcomes)
		}
	}
}

func TestRunLocalTestsStartsAtClass(t *testing.T) {
	t.Parallel()
	report, err := runLocalTestsForTest(t, LocalTestOptions{
		Project:    filepath.Join("..", "..", "testdata", "local-tests", "basic"),
		StartClass: "PassingTest",
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.CasesDiscovered != 2 {
		t.Fatalf("casesDiscovered = %d, want 2: %#v", report.CasesDiscovered, report.Outcomes)
	}
	for _, outcome := range report.Outcomes {
		if outcome.Class == "FailingTest" {
			t.Fatalf("start class included earlier class: %#v", report.Outcomes)
		}
	}
}

func TestRunLocalTestsStopsAfterMaxFailureGroups(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeLocalTestFile(t, filepath.Join(root, "sfdx-project.json"), `{"packageDirectories":[{"path":"force-app","default":true}]}`)
	writeLocalTestFile(t, filepath.Join(root, "force-app/main/default/classes/AFailingTest.cls"), `
@isTest
private class AFailingTest {
  @isTest static void fails() {
    System.assertEquals(3, 1 + 1);
  }
}
`)
	for i := 0; i < 9; i++ {
		className := fmt.Sprintf("PassingTriage%02dTest", i)
		writeLocalTestFile(t, filepath.Join(root, "force-app/main/default/classes/"+className+".cls"), fmt.Sprintf(`
@isTest
private class %s {
  @isTest static void passes() {
    System.assertEquals(2, 1 + 1);
  }
}
`, className))
	}

	report, err := runLocalTestsForTest(t, LocalTestOptions{
		Project:          root,
		BlockersOnly:     true,
		TopFailures:      1,
		MaxFailureGroups: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.TriageStopped || report.CasesDiscovered != 10 || report.CasesRun >= report.CasesDiscovered {
		t.Fatalf("triage fields = stopped %v discovered %d run %d", report.TriageStopped, report.CasesDiscovered, report.CasesRun)
	}
	if report.Summary.Total != 1 || report.Summary.AssertFailures != 1 || len(report.TopFailures) != 1 {
		t.Fatalf("report = %#v", report)
	}
}

func TestRunLocalTestsStopsAfterMaxFailureGroupsWithParallelism(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeLocalTestFile(t, filepath.Join(root, "sfdx-project.json"), `{"packageDirectories":[{"path":"force-app","default":true}]}`)
	writeLocalTestFile(t, filepath.Join(root, "force-app/main/default/classes/AFailingTest.cls"), `
@isTest
private class AFailingTest {
  @isTest static void fails() {
    System.assertEquals(3, 1 + 1);
  }
}
`)
	for i := 0; i < 9; i++ {
		className := fmt.Sprintf("PassingParallelTriage%02dTest", i)
		writeLocalTestFile(t, filepath.Join(root, "force-app/main/default/classes/"+className+".cls"), fmt.Sprintf(`
@isTest
private class %s {
  @isTest static void passes() {
    System.assertEquals(2, 1 + 1);
  }
}
`, className))
	}

	report, err := runLocalTestsForTest(t, LocalTestOptions{
		Project:          root,
		BlockersOnly:     true,
		TopFailures:      1,
		MaxFailureGroups: 1,
		Parallelism:      4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.TriageStopped || report.CasesDiscovered != 10 || report.CasesRun != 4 {
		t.Fatalf("triage fields = stopped %v discovered %d run %d", report.TriageStopped, report.CasesDiscovered, report.CasesRun)
	}
	if report.Summary.Total != 1 || report.Summary.AssertFailures != 1 || len(report.TopFailures) != 1 {
		t.Fatalf("report = %#v", report)
	}
}

func TestLocalTestRunOutcomeClassifiesDeadlineRuntimeErrorAsTimeout(t *testing.T) {
	outcome := localTestRunOutcome("fixture", testreport.Case{
		ClassName:  "SlowTest",
		MethodName: "timesOut",
		Status:     testreport.StatusRuntimeError,
		Problem:    &testreport.Problem{Type: "RuntimeError", Message: "context deadline exceeded"},
	})
	if outcome.Outcome != "timeout" || outcome.Phase != "timeout" || outcome.CapabilityID != "apex.test.timeout" {
		t.Fatalf("outcome = %#v", outcome)
	}
}

func TestShouldAnalyzeLocalTestsSkipsFocusedRuns(t *testing.T) {
	if !shouldAnalyzeLocalTests(LocalTestOptions{}, 12) {
		t.Fatalf("unfiltered local test run should analyze the full project")
	}
	if shouldAnalyzeLocalTests(LocalTestOptions{Class: "CartItemTest"}, 12) {
		t.Fatalf("class-filtered local test run should skip full-project semantic analysis")
	}
	if shouldAnalyzeLocalTests(LocalTestOptions{Method: "runsFast"}, 12) {
		t.Fatalf("method-filtered local test run should skip full-project semantic analysis")
	}
	if shouldAnalyzeLocalTests(LocalTestOptions{}, largeLocalTestAnalysisThreshold+1) {
		t.Fatalf("large unfiltered local test run should skip full-project semantic analysis by default")
	}
	if shouldAnalyzeLocalTests(LocalTestOptions{BlockersOnly: true}, 12) {
		t.Fatalf("blocker-only local test run should skip full-project semantic analysis")
	}
	if shouldAnalyzeLocalTests(LocalTestOptions{TopFailures: 10}, 12) {
		t.Fatalf("top-failures local test run should skip full-project semantic analysis")
	}
	if !shouldAnalyzeLocalTests(LocalTestOptions{ForceAnalysis: true}, largeLocalTestAnalysisThreshold+1) {
		t.Fatalf("large unfiltered local test run should allow forced full-project semantic analysis")
	}
	if !shouldAnalyzeLocalTests(LocalTestOptions{BlockersOnly: true, ForceAnalysis: true}, largeLocalTestAnalysisThreshold+1) {
		t.Fatalf("forced blocker-only local test run should allow full-project semantic analysis")
	}
	if shouldAnalyzeLocalTests(LocalTestOptions{Parallelism: 8}, 100) {
		t.Fatalf("explicit parallel local test run should skip full-project semantic analysis")
	}
	if shouldAnalyzeLocalTests(LocalTestOptions{ProgressWriter: io.Discard}, 100) {
		t.Fatalf("progress local test run should skip full-project semantic analysis")
	}
}

func TestLocalTestParallelismCapsFocusedClassRuns(t *testing.T) {
	if got := localTestParallelism(LocalTestOptions{}); got < 1 || got > 8 {
		t.Fatalf("full-project default parallelism = %d, want 1..8", got)
	}
	if got := localTestParallelism(LocalTestOptions{Class: "CartSubmitterTest"}); got < 1 || got > 8 {
		t.Fatalf("focused class parallelism = %d, want 1..8", got)
	}
	if got := localTestParallelism(LocalTestOptions{Class: "CartSubmitterTest", Parallelism: 4}); got != 4 {
		t.Fatalf("explicit focused class parallelism = %d, want 4", got)
	}
	if got := localTestParallelism(LocalTestOptions{Class: "CartSubmitterTest", Method: "runs"}); got != 1 {
		t.Fatalf("focused method parallelism = %d, want 1", got)
	}
}

func TestAutoParallelismForCases(t *testing.T) {
	if got := autoParallelismForCases(10); got < 1 || got > 2 {
		t.Fatalf("auto parallelism for tiny suite = %d, want 1..2", got)
	}
	if got := autoParallelismForCases(100); got < 1 || got > 4 {
		t.Fatalf("auto parallelism for small suite = %d, want 1..4", got)
	}
	if got := autoParallelismForCases(800); got < 1 || got > 8 {
		t.Fatalf("auto parallelism for medium suite = %d, want 1..8", got)
	}
	if got := autoParallelismForCases(5000); got < 1 || got > 4 {
		t.Fatalf("auto parallelism for large suite = %d, want 1..4", got)
	}
}

func TestAutoTuneLocalTestOptionsUsesShardEnv(t *testing.T) {
	t.Setenv("GLADE_SHARD_COUNT", "6")
	t.Setenv("GLADE_SHARD_INDEX", "2")
	options, parallelism := autoTuneLocalTestOptions(LocalTestOptions{
		AutoTune:       true,
		AutoShardCount: true,
		AutoShardIndex: true,
	}, 2000, 1)
	if options.ShardCount != 6 {
		t.Fatalf("ShardCount = %d, want 6", options.ShardCount)
	}
	if options.ShardIndex != 2 {
		t.Fatalf("ShardIndex = %d, want 2", options.ShardIndex)
	}
	if parallelism < 1 {
		t.Fatalf("parallelism = %d, want >= 1", parallelism)
	}
}

func TestShouldParallelizeMethodsForLargeFocusedClasses(t *testing.T) {
	if shouldParallelizeMethods(LocalTestOptions{Class: "CartSubmitterTest"}, 4, 12) {
		t.Fatalf("large focused class run should keep methods serial by default")
	}
	if shouldParallelizeMethods(LocalTestOptions{Class: "SmallTest"}, 4, 3) {
		t.Fatalf("small focused class run should keep methods serial")
	}
	if shouldParallelizeMethods(LocalTestOptions{Class: "CartSubmitterTest", Method: "runs"}, 4, 12) {
		t.Fatalf("focused method run should keep methods serial")
	}
	if shouldParallelizeMethods(LocalTestOptions{Class: "CartSubmitterTest"}, 1, 12) {
		t.Fatalf("explicit serial focused class run should keep methods serial")
	}
	if !shouldParallelizeMethods(LocalTestOptions{Class: "CartSubmitterTest", ParallelMethods: true}, 4, 12) {
		t.Fatalf("focused class run should allow explicit method parallelism")
	}
}

func TestFocusedLocalTestsSkipTraceByDefault(t *testing.T) {
	t.Parallel()
	report, err := runLocalTestsForTest(t, LocalTestOptions{
		Project: filepath.Join("..", "..", "testdata", "local-tests", "basic"),
		Class:   "FailingTest",
		Method:  "fails",
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.Total != 1 || report.Summary.AssertFailures != 1 {
		t.Fatalf("summary = %#v", report.Summary)
	}
	outcome := report.Outcomes[0]
	if outcome.TraceEvents != 0 || outcome.ProfileEvents != 0 || len(outcome.ProfileCategories) != 0 {
		t.Fatalf("focused outcome should not include trace/profile by default: %#v", outcome)
	}
}

func TestRunLocalTestsClassFilterIsExact(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeLocalTestFile(t, filepath.Join(root, "sfdx-project.json"), `{"packageDirectories":[{"path":"force-app","default":true}]}`)
	writeLocalTestFile(t, filepath.Join(root, "force-app/main/default/classes/CartSubmitterTest.cls"), `
@isTest
private class CartSubmitterTest {
  @isTest static void runs() {
    System.assertEquals(1, 1);
  }
}
`)
	writeLocalTestFile(t, filepath.Join(root, "force-app/main/default/classes/ScheduleWithCartSubmitterTest.cls"), `
@isTest
private class ScheduleWithCartSubmitterTest {
  @isTest static void shouldNotRun() {
    System.assert(false, 'wrong class');
  }
}
`)

	report, err := runLocalTestsForTest(t, LocalTestOptions{Project: root, Class: "CartSubmitterTest"})
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.Total != 1 || report.Summary.Pass != 1 {
		t.Fatalf("summary = %#v outcomes = %#v", report.Summary, report.Outcomes)
	}
	if report.Outcomes[0].Class != "CartSubmitterTest" {
		t.Fatalf("outcome = %#v", report.Outcomes[0])
	}
}

func TestRunLocalTestsChangedSinceNoneDoesNotTurnFocusedClassIntoLoadError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeLocalTestFile(t, filepath.Join(root, "sfdx-project.json"), `{"packageDirectories":[{"path":"force-app","default":true}]}`)
	writeLocalTestFile(t, filepath.Join(root, "force-app/main/default/classes/SampleTest.cls"), `
@isTest
private class SampleTest {
  @isTest static void runs() {
    System.assertEquals(1, 1);
  }
}
`)
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	runGit("init")
	runGit("config", "user.email", "glade@example.test")
	runGit("config", "user.name", "Glade Test")
	runGit("add", ".")
	runGit("commit", "-m", "initial")

	report, err := runLocalTestsForTest(t, LocalTestOptions{
		Project:      root,
		Class:        "SampleTest",
		ChangedSince: "HEAD",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Ready || report.Summary.Total != 0 || report.Summary.LoadErrors != 0 {
		t.Fatalf("report = %#v, want ready zero-run report", report)
	}
	if report.CasesDiscovered != 0 || report.CasesRun != 0 {
		t.Fatalf("cases = discovered %d run %d, want 0/0", report.CasesDiscovered, report.CasesRun)
	}
	if report.Selection == nil {
		t.Fatalf("selection missing: %#v", report)
	}
}

func TestRunLocalTestsFocusedSelectionReportsNoMatches(t *testing.T) {
	t.Parallel()
	missingRoot := filepath.Join(t.TempDir(), "missing")
	for _, tt := range []struct {
		name    string
		options LocalTestOptions
		want    string
	}{
		{
			name: "class",
			options: LocalTestOptions{
				Project: filepath.Join("..", "..", "testdata", "local-tests", "basic"),
				Class:   "MissingTest",
			},
			want: `no Apex test methods matched --class "MissingTest"`,
		},
		{
			name: "method without class",
			options: LocalTestOptions{
				Project: filepath.Join("..", "..", "testdata", "local-tests", "basic"),
				Method:  "passes",
			},
			want: "--method requires --class",
		},
		{
			name: "missing project",
			options: LocalTestOptions{
				Project: missingRoot,
			},
			want: "project root does not exist",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			report, err := runLocalTestsForTest(t, tt.options)
			if err != nil {
				t.Fatal(err)
			}
			if report.Ready {
				t.Fatalf("ready = true, want false: %#v", report)
			}
			if report.Summary.Total != 1 || report.Summary.LoadErrors != 1 {
				t.Fatalf("summary = %#v, want one load error", report.Summary)
			}
			if len(report.Outcomes) != 1 || report.Outcomes[0].Outcome != "load_error" || !strings.Contains(report.Outcomes[0].Error, tt.want) {
				t.Fatalf("outcomes = %#v, want %q", report.Outcomes, tt.want)
			}
		})
	}
}

func TestLocalTestRunOutcomeSplitsRuntimeAndTimeout(t *testing.T) {
	runtimeGap := localTestRunOutcome("fixture", testreport.Case{
		ClassName:  "RuntimeGapTest",
		MethodName: "fails",
		Status:     testreport.StatusFail,
		Problem:    &testreport.Problem{Type: "RuntimeError", Message: "method dispatch failed"},
	})
	if runtimeGap.Outcome != "runtime_gap" {
		t.Fatalf("runtime outcome = %#v", runtimeGap)
	}

	assertFail := localTestRunOutcome("fixture", testreport.Case{
		ClassName:  "AssertTest",
		MethodName: "fails",
		Status:     testreport.StatusFail,
		Problem:    &testreport.Problem{Type: "AssertException", Message: "Assertion Failed"},
	})
	if assertFail.Outcome != "assert_fail" || assertFail.Phase != "assert" {
		t.Fatalf("assert outcome = %#v", assertFail)
	}

	timeout := localTestRunOutcome("fixture", testreport.Case{
		ClassName:  "TimeoutTest",
		MethodName: "hangs",
		Status:     testreport.StatusUnsupported,
		Problem:    &testreport.Problem{Type: "Canceled", Message: "context deadline exceeded"},
	})
	if timeout.Outcome != "timeout" || timeout.CapabilityID != "apex.test.timeout" {
		t.Fatalf("timeout outcome = %#v", timeout)
	}
}

func TestRunLocalTestsPlatformAPIsFixtureReady(t *testing.T) {
	runLocalTestReadyFixture(t, localTestReadyFixture{name: "platform-apis", total: 4})
}

func TestRunLocalTestsNamedCredentialCalloutsFixtureReady(t *testing.T) {
	runLocalTestReadyFixture(t, localTestReadyFixture{name: "named-credential-callouts", total: 2})
}

func TestRunLocalTestsFilesEmailFixtureReady(t *testing.T) {
	runLocalTestReadyFixture(t, localTestReadyFixture{name: "files-email", total: 2})
}

func TestRunLocalTestsWorkflowFixtureReady(t *testing.T) {
	runLocalTestReadyFixture(t, localTestReadyFixture{name: "workflow", total: 1})
}

func TestRunLocalTestsFlowFixtureReady(t *testing.T) {
	runLocalTestReadyFixture(t, localTestReadyFixture{name: "flow", total: 1})
}

func TestRunLocalTestsResourcesLabelsFixtureReady(t *testing.T) {
	runLocalTestReadyFixture(t, localTestReadyFixture{name: "resources-labels", total: 2})
}

func TestRunLocalTestsUIControllerContractsFixtureReady(t *testing.T) {
	runLocalTestReadyFixture(t, localTestReadyFixture{name: "ui-controller-contracts", total: 2})
}

func TestRunLocalTestsVisualforcePagesFixtureReady(t *testing.T) {
	runLocalTestReadyFixture(t, localTestReadyFixture{name: "visualforce-pages", total: 3})
}

func TestRunLocalTestsOrgLikeRunnerFixtureReady(t *testing.T) {
	runLocalTestReadyFixture(t, localTestReadyFixture{name: "org-like-runner", total: 2})
}

func TestRunLocalTestsVMExceptionDispatchFixtureReady(t *testing.T) {
	runLocalTestReadyFixture(t, localTestReadyFixture{name: "vm-exception-dispatch", total: 1})
}

func TestRunLocalTestsStandardObjectShapeFixtureReady(t *testing.T) {
	runLocalTestReadyFixture(t, localTestReadyFixture{name: "standard-object-shape", total: 2})
}

func TestRunLocalTestsEnterpriseComposedFixtureReady(t *testing.T) {
	runLocalTestReadyFixture(t, localTestReadyFixture{name: "enterprise-composed", total: 2})
}

func TestRunLocalTestsMetadataDeployFixtureReady(t *testing.T) {
	runLocalTestReadyFixture(t, localTestReadyFixture{name: "metadata-deploy", total: 1})
}

func TestCheckLocalTestCorpusFixture(t *testing.T) {
	requireFullLocalTestFixtures(t)
	report, err := CheckLocalTestCorpus(filepath.Join("..", "..", "docs", "fixtures", "local-tests-corpus.json"))
	if err != nil {
		t.Fatalf("CheckLocalTestCorpus error = %v, report = %#v", err, report)
	}
	if !report.Ready || len(report.Projects) != 16 {
		t.Fatalf("report = %#v", report)
	}
}

func TestCheckPostParityTraceFixture(t *testing.T) {
	report, err := CheckPostParityTraceFixture(filepath.Join("..", "..", "docs", "fixtures", "post-parity-trace-events.json"))
	if err != nil {
		t.Fatalf("CheckPostParityTraceFixture error = %v, report = %#v", err, report)
	}
	if !report.Ready || len(report.Surfaces) != 3 {
		t.Fatalf("report = %#v", report)
	}
}

func TestCheckUIControllerDiscoveryFixture(t *testing.T) {
	report, err := CheckUIControllerDiscovery(filepath.Join("..", "..", "docs", "fixtures", "ui-controller-discovery.json"))
	if err != nil {
		t.Fatalf("CheckUIControllerDiscovery error = %v, report = %#v", err, report)
	}
	if !report.Ready || report.Summary.AuraBundles != 1 || report.Summary.LWCBundles != 1 || report.Summary.UnresolvedApex != 1 {
		t.Fatalf("report = %#v", report)
	}
}

func TestRunLocalTestsReportsLoadError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeLocalTestFile(t, filepath.Join(root, "sfdx-project.json"), `{`)
	report, err := runLocalTestsForTest(t, LocalTestOptions{Project: root})
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.LoadErrors != 1 || report.Outcomes[0].Outcome != "load_error" {
		t.Fatalf("report = %#v", report)
	}
}

func writeLocalTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
