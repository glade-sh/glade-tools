package corpusrun

import (
	"reflect"
	"strings"
	"testing"
)

func baseGolden() Golden {
	return Golden{
		Repo:        "sample-app",
		Pin:         "1111111111111111111111111111111111111111",
		Overlay:     Overlay{Deps: []string{"sample-dep"}},
		Glade:       GladeInfo{SourceAPI: "67.0"},
		Counts:      Counts{Classes: 10, Tests: 100, Passed: 97},
		WallSeconds: 100,
	}
}

func baseObserved() Observed {
	return Observed{
		Pin:         "1111111111111111111111111111111111111111",
		Overlay:     Overlay{Deps: []string{"sample-dep"}},
		Glade:       GladeInfo{SourceAPI: "67.0"},
		Counts:      Counts{Classes: 10, Tests: 100, Passed: 97},
		WallSeconds: 100,
	}
}

func testEntries(names ...string) []TestEntry {
	entries := make([]TestEntry, 0, len(names))
	for _, name := range names {
		entries = append(entries, TestEntry{Test: name})
	}
	return entries
}

func testNames(entries []TestEntry) []string {
	names := []string{}
	for _, entry := range entries {
		names = append(names, entry.Test)
	}
	return names
}

func TestCompareCleanRunIsOK(t *testing.T) {
	golden := baseGolden()
	golden.Failing = testEntries("SampleTest.expected")
	golden.Diagnostics = []DiagnosticEntry{{Code: "GLADESEMA001", File: "src/A.cls", Line: 3, Status: StatusKnownGap}}
	observed := baseObserved()
	observed.Failing = testEntries("SampleTest.expected")
	observed.Diagnostics = []DiagnosticEntry{{Code: "GLADESEMA001", File: "src/A.cls", Line: 3}}

	diff := Compare(golden, observed)
	if got := diff.Line(); got != "OK" {
		t.Fatalf("Line() = %q, want OK; diff = %+v", got, diff)
	}
}

func TestCompareFailingTestNotInGoldenIsNew(t *testing.T) {
	golden := baseGolden()
	golden.Failing = testEntries("SampleTest.expected")
	observed := baseObserved()
	observed.Failing = []TestEntry{{Test: "SampleTest.expected"}, {Test: "SampleTest.broken", Message: "boom"}}

	diff := Compare(golden, observed)
	if got := diff.New.Count(); got != 1 {
		t.Fatalf("New.Count() = %d, want 1", got)
	}
	if got := diff.New.Failing[0]; got.Test != "SampleTest.broken" || got.Message != "boom" {
		t.Fatalf("New.Failing[0] = %+v", got)
	}
	if got := diff.Line(); got != "NEW 1" {
		t.Fatalf("Line() = %q, want NEW 1", got)
	}
}

func TestCompareDiagnosticNotInGoldenIsNew(t *testing.T) {
	golden := baseGolden()
	golden.Diagnostics = []DiagnosticEntry{{Code: "GLADESEMA001", File: "src/A.cls", Line: 3}}
	observed := baseObserved()
	observed.Diagnostics = []DiagnosticEntry{
		{Code: "GLADESEMA001", File: "src/A.cls", Line: 3},
		{Code: "GLADESEMA001", File: "src/A.cls", Line: 4},
		{Code: "GLADESEMA002", File: "src/A.cls", Line: 3},
		{Code: "GLADESEMA001", File: "src/B.cls", Line: 3},
	}

	diff := Compare(golden, observed)
	if got := diff.New.Count(); got != 3 {
		t.Fatalf("New.Count() = %d, want 3 (line, code and file each identify a diagnostic): %+v", got, diff.New)
	}
}

// Two diagnostics can share one code, file and line. The golden lists each
// copy, so a third copy is NEW and a missing copy is GONE.
func TestCompareDiagnosticsCountCopies(t *testing.T) {
	entry := DiagnosticEntry{Code: "GLADESEMA001", File: "src/A.cls", Line: 3}

	golden := baseGolden()
	golden.Diagnostics = []DiagnosticEntry{entry, entry}
	observed := baseObserved()
	observed.Diagnostics = []DiagnosticEntry{entry, entry, entry}
	if diff := Compare(golden, observed); diff.New.Count() != 1 || diff.Gone.Count() != 0 {
		t.Fatalf("three copies against two: new %d gone %d, want 1 and 0", diff.New.Count(), diff.Gone.Count())
	}

	observed.Diagnostics = []DiagnosticEntry{entry}
	if diff := Compare(golden, observed); diff.New.Count() != 0 || diff.Gone.Count() != 1 {
		t.Fatalf("one copy against two: new %d gone %d, want 0 and 1", diff.New.Count(), diff.Gone.Count())
	}
}

func TestCompareGoldenEntryThatPassesIsGoneNotRed(t *testing.T) {
	golden := baseGolden()
	golden.Failing = testEntries("SampleTest.fixedNow", "SampleTest.stillBroken")
	golden.Diagnostics = []DiagnosticEntry{{Code: "GLADESEMA001", File: "src/A.cls", Line: 3}}
	observed := baseObserved()
	observed.Failing = testEntries("SampleTest.stillBroken")

	diff := Compare(golden, observed)
	if got := diff.New.Count(); got != 0 {
		t.Fatalf("New.Count() = %d, want 0", got)
	}
	if got := diff.Gone.Count(); got != 2 {
		t.Fatalf("Gone.Count() = %d, want 2: %+v", got, diff.Gone)
	}
	if got := diff.Line(); got != "GONE 2" {
		t.Fatalf("Line() = %q, want GONE 2", got)
	}
	first, exit := Summarize([]RepoResult{{Repo: "sample-app", Diff: diff}})
	if first != "CORPUS-CLEAN" || exit != ExitClean {
		t.Fatalf("Summarize = %q, %d; GONE must not be red", first, exit)
	}
}

// A flaky entry that passes is what flaky means, so it is not a prompt to
// shrink the golden. A flaky entry that fails is still expected.
func TestCompareFlakyEntryIsNeverGone(t *testing.T) {
	golden := baseGolden()
	golden.Failing = []TestEntry{{Test: "SampleTest.clockSensitive", Status: StatusFlaky}}
	golden.Diagnostics = []DiagnosticEntry{{Code: "GLADESEMA001", File: "src/A.cls", Line: 3, Status: StatusFlaky}}

	if diff := Compare(golden, baseObserved()); diff.Line() != "OK" {
		t.Fatalf("flaky entries passing: Line() = %q, want OK", diff.Line())
	}

	observed := baseObserved()
	observed.Failing = testEntries("SampleTest.clockSensitive")
	observed.Diagnostics = []DiagnosticEntry{{Code: "GLADESEMA001", File: "src/A.cls", Line: 3}}
	if diff := Compare(golden, observed); diff.Line() != "OK" {
		t.Fatalf("flaky entries failing: Line() = %q, want OK", diff.Line())
	}
}

func TestCompareTimeoutRule(t *testing.T) {
	cases := []struct {
		name          string
		goldenFailing []string
		goldenTimeout []string
		timeouts      []string
		wantNew       []string
	}{
		{name: "zero to zero"},
		{
			name:     "zero to one is new",
			timeouts: []string{"SampleTest.hangs"},
			wantNew:  []string{"SampleTest.hangs"},
		},
		{
			name:          "exactly twice the golden is not new",
			goldenTimeout: []string{"SampleTest.a", "SampleTest.b"},
			timeouts:      []string{"SampleTest.a", "SampleTest.b", "SampleTest.c", "SampleTest.d"},
		},
		{
			name:          "more than twice the golden counts every timeout the golden lacks",
			goldenTimeout: []string{"SampleTest.a", "SampleTest.b"},
			timeouts:      []string{"SampleTest.a", "SampleTest.b", "SampleTest.c", "SampleTest.d", "SampleTest.e"},
			wantNew:       []string{"SampleTest.c", "SampleTest.d", "SampleTest.e"},
		},
		{
			name:          "under the limit a different timeout is not new",
			goldenTimeout: []string{"SampleTest.a", "SampleTest.b"},
			timeouts:      []string{"SampleTest.c"},
		},
		{
			name:          "a test the golden expects to fail is not a new timeout",
			goldenFailing: []string{"SampleTest.expected"},
			timeouts:      []string{"SampleTest.expected", "SampleTest.hangs"},
			wantNew:       []string{"SampleTest.hangs"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			golden := baseGolden()
			golden.Failing = testEntries(tc.goldenFailing...)
			golden.Timeouts = testEntries(tc.goldenTimeout...)
			observed := baseObserved()
			observed.Timeouts = testEntries(tc.timeouts...)

			diff := Compare(golden, observed)
			want := tc.wantNew
			if want == nil {
				want = []string{}
			}
			if got := testNames(diff.New.Timeouts); !reflect.DeepEqual(got, want) {
				t.Fatalf("New.Timeouts = %v, want %v", got, want)
			}
		})
	}
}

// A test that timed out did not run, so it says nothing about the failure the
// golden expected, and a failure of a test the golden saw time out is not NEW.
func TestCompareTimedOutTestIsNotRun(t *testing.T) {
	golden := baseGolden()
	golden.Failing = testEntries("SampleTest.failedBefore")
	golden.Timeouts = testEntries("SampleTest.hungBefore", "SampleTest.otherHang")
	observed := baseObserved()
	observed.Timeouts = testEntries("SampleTest.failedBefore", "SampleTest.otherHang")
	observed.Failing = testEntries("SampleTest.hungBefore")

	diff := Compare(golden, observed)
	if diff.New.Count() != 0 || diff.Gone.Count() != 0 {
		t.Fatalf("new %+v gone %+v, want neither", diff.New, diff.Gone)
	}
}

func TestCompareGoneTimeoutIsReported(t *testing.T) {
	golden := baseGolden()
	golden.Timeouts = testEntries("SampleTest.hungBefore")
	diff := Compare(golden, baseObserved())
	if got := testNames(diff.Gone.Timeouts); !reflect.DeepEqual(got, []string{"SampleTest.hungBefore"}) {
		t.Fatalf("Gone.Timeouts = %v", got)
	}
}

func TestCompareSlow(t *testing.T) {
	cases := []struct {
		name   string
		golden float64
		run    float64
		want   string
	}{
		{name: "faster", golden: 100, run: 50, want: "OK"},
		{name: "exactly 1.5x is not slow", golden: 100, run: 150, want: "OK"},
		{name: "above 1.5x is slow", golden: 100, run: 170, want: "SLOW 1.7"},
		{name: "a golden with no wall time cannot be slow", golden: 0, run: 900, want: "OK"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			golden := baseGolden()
			golden.WallSeconds = tc.golden
			observed := baseObserved()
			observed.WallSeconds = tc.run

			diff := Compare(golden, observed)
			if got := diff.Line(); got != tc.want {
				t.Fatalf("Line() = %q, want %q", got, tc.want)
			}
			first, exit := Summarize([]RepoResult{{Repo: "sample-app", Diff: diff}})
			if first != "CORPUS-CLEAN" || exit != ExitClean {
				t.Fatalf("Summarize = %q, %d; SLOW must not be red", first, exit)
			}
		})
	}
}

// A golden is the result of one repo at one pin under one overlay, so a run of
// anything else cannot be compared with it.
func TestCompareMismatchIsInfra(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Observed)
		want   string
	}{
		{
			name:   "pin",
			mutate: func(o *Observed) { o.Pin = "2222222222222222222222222222222222222222" },
			want:   "golden pin 111111111111 differs from run pin 222222222222",
		},
		{
			name:   "overlay deps",
			mutate: func(o *Observed) { o.Overlay.Deps = []string{"other-dep"} },
			want:   "golden overlay differs from config",
		},
		{
			name:   "overlay config",
			mutate: func(o *Observed) { o.Overlay.Config.Features = []string{"SampleFeature"} },
			want:   "golden overlay differs from config",
		},
		{
			name:   "source api",
			mutate: func(o *Observed) { o.Glade.SourceAPI = "62.0" },
			want:   "golden source API 67.0 differs from run 62.0",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			observed := baseObserved()
			observed.Failing = testEntries("SampleTest.wouldBeNew")
			tc.mutate(&observed)

			diff := Compare(baseGolden(), observed)
			if diff.Infra != tc.want {
				t.Fatalf("Infra = %q, want %q", diff.Infra, tc.want)
			}
			if diff.New.Count() != 0 {
				t.Fatalf("a mismatched golden must not produce NEW: %+v", diff.New)
			}
			if got := diff.Line(); got != "INFRA "+tc.want {
				t.Fatalf("Line() = %q", got)
			}
		})
	}
}

func TestCompareOverlayIgnoresEmptyVersusNil(t *testing.T) {
	golden := baseGolden()
	golden.Overlay = Overlay{Deps: []string{}, Config: OverlayConfig{Features: []string{}}}
	observed := baseObserved()
	observed.Overlay = Overlay{}
	if diff := Compare(golden, observed); diff.Infra != "" {
		t.Fatalf("Infra = %q, want none", diff.Infra)
	}
}

// Nothing in the golden names a passing test, so losing tests would otherwise
// read as clean. A real failure in the same run is still NEW and still red.
func TestCompareDroppedTestsAreInfra(t *testing.T) {
	golden := baseGolden()
	golden.Failing = testEntries("SampleTest.expected")
	observed := baseObserved()
	observed.Counts.Tests = 60
	observed.Failing = testEntries("SampleTest.broken")

	diff := Compare(golden, observed)
	if diff.Infra != "tests dropped from 100 to 60" {
		t.Fatalf("Infra = %q", diff.Infra)
	}
	if got := diff.New.Count(); got != 1 {
		t.Fatalf("New.Count() = %d, want 1", got)
	}
	if got := diff.Gone.Count(); got != 0 {
		t.Fatalf("Gone.Count() = %d, want 0: a test that did not run is not gone", got)
	}
	if got := diff.Line(); got != "NEW 1 INFRA tests dropped from 100 to 60" {
		t.Fatalf("Line() = %q", got)
	}
}

func TestCompareMoreTestsIsFine(t *testing.T) {
	observed := baseObserved()
	observed.Counts.Tests = 140
	if diff := Compare(baseGolden(), observed); diff.Line() != "OK" {
		t.Fatalf("Line() = %q, want OK", diff.Line())
	}
}

func TestLineJoinsEveryResultInOrder(t *testing.T) {
	diff := Diff{
		New:   Delta{Failing: testEntries("a", "b")},
		Gone:  Delta{Failing: testEntries("c")},
		Slow:  1.84,
		Infra: "something broke",
	}
	if got := diff.Line(); got != "NEW 2 GONE 1 SLOW 1.8 INFRA something broke" {
		t.Fatalf("Line() = %q", got)
	}
}

func TestSummarize(t *testing.T) {
	clean := RepoResult{Repo: "repo-a"}
	gone := RepoResult{Repo: "repo-b", Diff: Diff{Gone: Delta{Failing: testEntries("x")}}}
	new1 := RepoResult{Repo: "repo-c", Diff: Diff{New: Delta{Failing: testEntries("x", "y")}}}
	new2 := RepoResult{Repo: "repo-d", Diff: Diff{New: Delta{Timeouts: testEntries("x")}}}
	infra := RepoResult{Repo: "repo-e", Diff: Diff{Infra: "no golden"}}

	cases := []struct {
		name    string
		results []RepoResult
		first   string
		exit    int
	}{
		{name: "clean", results: []RepoResult{clean, gone}, first: "CORPUS-CLEAN", exit: ExitClean},
		{name: "new", results: []RepoResult{clean, new1, new2}, first: "CORPUS-NEW 3 in 2 repos", exit: ExitNew},
		{name: "infra alone is not clean", results: []RepoResult{clean, infra}, first: "CORPUS-INFRA 1 repos", exit: ExitInfra},
		{name: "new outranks infra", results: []RepoResult{new1, infra}, first: "CORPUS-NEW 2 in 1 repos", exit: ExitNew},
		{name: "nothing ran is not clean", results: nil, first: "CORPUS-INFRA 0 repos", exit: ExitInfra},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			first, exit := Summarize(tc.results)
			if first != tc.first || exit != tc.exit {
				t.Fatalf("Summarize = %q, %d; want %q, %d", first, exit, tc.first, tc.exit)
			}
		})
	}
}

func TestWriteReportPutsVerdictFirstAndOneLinePerRepo(t *testing.T) {
	report := NewRunReport("B", []RepoResult{
		{Repo: "repo-a"},
		{Repo: "repo-b", Diff: Diff{New: Delta{Failing: testEntries("x")}}},
		{Repo: "repo-c", Diff: Diff{Infra: "no golden"}},
	})
	var out strings.Builder
	report.WriteText(&out)
	want := "CORPUS-NEW 1 in 1 repos\nrepo-a OK\nrepo-b NEW 1\nrepo-c INFRA no golden\n"
	if out.String() != want {
		t.Fatalf("report:\n%s\nwant:\n%s", out.String(), want)
	}
	if report.ExitCode != ExitNew {
		t.Fatalf("ExitCode = %d", report.ExitCode)
	}
}

func TestCompareRejectsUnclassifiedGoldenEntries(t *testing.T) {
	for _, kind := range []string{"failing", "timeout", "diagnostic"} {
		t.Run(kind, func(t *testing.T) {
			g := baseGolden()
			switch kind {
			case "failing":
				g.Failing = []TestEntry{{Test: "Example.test", Status: StatusUnclassified}}
			case "timeout":
				g.Timeouts = []TestEntry{{Test: "Example.test", Status: StatusUnclassified}}
			case "diagnostic":
				g.Diagnostics = []DiagnosticEntry{{Code: "EXAMPLE", Status: StatusUnclassified}}
			}
			if got := Compare(g, baseObserved()); got.Infra == "" {
				t.Fatalf("unclassified %s accepted: %+v", kind, got)
			}
		})
	}
}

func TestCompareDetectsAdditionalOverlayChanges(t *testing.T) {
	for _, kind := range []string{"namespace", "locale", "external"} {
		t.Run(kind, func(t *testing.T) {
			golden := baseGolden()
			observed := baseObserved()
			switch kind {
			case "namespace":
				s := ""
				observed.Overlay.Config.ProjectNamespace = &s
			case "locale":
				observed.Overlay.Config.UserLocale = "fr_CA"
			case "external":
				observed.Overlay.Config.ExternalNamespaces = []string{"sample"}
			}
			if got := Compare(golden, observed); got.Infra == "" {
				t.Fatalf("overlay change accepted: %+v", got)
			}
		})
	}
}
