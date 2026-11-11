package corpusrun

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildGoldenKeepsStatusesOfEntriesThatPersist(t *testing.T) {
	existing := baseGolden()
	existing.Failing = []TestEntry{
		{Test: "SampleTest.persists", Status: StatusKnownGap, Reason: "hosted boundary", Ticket: "ISSUE-1"},
		{Test: "SampleTest.fixedNow", Status: StatusIntended, Reason: "old", Ticket: "ISSUE-2"},
	}
	existing.Diagnostics = []DiagnosticEntry{
		{Code: "GLADESEMA001", File: "src/A.cls", Line: 3, Status: StatusEnv, Reason: "missing source", Ticket: "ISSUE-3"},
		{Code: "GLADESEMA001", File: "src/A.cls", Line: 9, Status: StatusEnv, Reason: "old", Ticket: "ISSUE-4"},
	}
	existing.Timeouts = []TestEntry{{Test: "SampleTest.hangs", Status: StatusKnownGap, Reason: "slow by design", Ticket: "ISSUE-5"}}

	observed := baseObserved()
	observed.Pin = "3333333333333333333333333333333333333333"
	observed.Overlay = Overlay{Deps: []string{"other-dep"}}
	observed.Glade = GladeInfo{SourceAPI: "66.0"}
	observed.Counts = Counts{Classes: 11, Tests: 120, Passed: 116}
	observed.WallSeconds = 88.5
	observed.Failing = []TestEntry{
		{Test: "SampleTest.persists", Message: "new wording"},
		{Test: "SampleTest.brandNew", Message: "boom"},
	}
	observed.Diagnostics = []DiagnosticEntry{
		{Code: "GLADESEMA001", File: "src/A.cls", Line: 3, Severity: "error", Message: "unknown type"},
		{Code: "GLADESEMA002", File: "src/B.cls", Line: 1, Severity: "error", Message: "other"},
	}
	observed.Timeouts = []TestEntry{{Test: "SampleTest.hangs"}, {Test: "SampleTest.hangsToo"}}

	golden, stats := BuildGolden(&existing, "sample-app", observed)

	if golden.Repo != "sample-app" || golden.Pin != observed.Pin || golden.Glade.SourceAPI != "66.0" || golden.WallSeconds != 88.5 {
		t.Fatalf("identity not refreshed from the run: %+v", golden)
	}
	if golden.Counts != observed.Counts {
		t.Fatalf("Counts = %+v", golden.Counts)
	}
	if len(golden.Overlay.Deps) != 1 || golden.Overlay.Deps[0] != "other-dep" {
		t.Fatalf("Overlay = %+v", golden.Overlay)
	}

	if len(golden.Failing) != 2 {
		t.Fatalf("Failing = %+v", golden.Failing)
	}
	brandNew, persists := golden.Failing[0], golden.Failing[1]
	if brandNew.Test != "SampleTest.brandNew" || brandNew.Status != StatusUnclassified || brandNew.Reason != "" || brandNew.Ticket != "" || brandNew.Message != "boom" {
		t.Fatalf("new entry = %+v", brandNew)
	}
	if persists.Test != "SampleTest.persists" || persists.Status != StatusKnownGap || persists.Reason != "hosted boundary" || persists.Ticket != "ISSUE-1" || persists.Message != "new wording" {
		t.Fatalf("persisting entry = %+v", persists)
	}

	if len(golden.Diagnostics) != 2 {
		t.Fatalf("Diagnostics = %+v", golden.Diagnostics)
	}
	keptDiag, newDiag := golden.Diagnostics[0], golden.Diagnostics[1]
	if keptDiag.Status != StatusEnv || keptDiag.Reason != "missing source" || keptDiag.Ticket != "ISSUE-3" || keptDiag.Message != "unknown type" {
		t.Fatalf("persisting diagnostic = %+v", keptDiag)
	}
	if newDiag.Status != StatusUnclassified || newDiag.Code != "GLADESEMA002" {
		t.Fatalf("new diagnostic = %+v", newDiag)
	}

	if len(golden.Timeouts) != 2 || golden.Timeouts[0].Status != StatusKnownGap || golden.Timeouts[1].Status != StatusUnclassified {
		t.Fatalf("Timeouts = %+v", golden.Timeouts)
	}

	if stats.Unclassified != 3 {
		t.Fatalf("stats.Unclassified = %d, want 3", stats.Unclassified)
	}
	if stats.Dropped != 2 {
		t.Fatalf("stats.Dropped = %d, want 2", stats.Dropped)
	}
}

func TestBuildGoldenWithoutExistingMarksEverythingUnclassified(t *testing.T) {
	observed := baseObserved()
	observed.Failing = testEntries("SampleTest.b", "SampleTest.a")
	observed.Diagnostics = []DiagnosticEntry{{Code: "GLADESEMA001", File: "src/A.cls", Line: 3}}

	golden, stats := BuildGolden(nil, "sample-app", observed)
	if stats.Unclassified != 3 {
		t.Fatalf("stats.Unclassified = %d", stats.Unclassified)
	}
	if golden.Failing[0].Test != "SampleTest.a" || golden.Failing[1].Test != "SampleTest.b" {
		t.Fatalf("Failing not sorted: %+v", golden.Failing)
	}
	for _, entry := range golden.Failing {
		if entry.Status != StatusUnclassified {
			t.Fatalf("entry %+v", entry)
		}
	}
	if golden.Diagnostics[0].Status != StatusUnclassified {
		t.Fatalf("diagnostic %+v", golden.Diagnostics[0])
	}
}

// A flaky entry that happened to pass in the run a refresh is built from has to
// stay, or its next failure would be NEW.
func TestBuildGoldenKeepsFlakyEntriesThatPassed(t *testing.T) {
	existing := baseGolden()
	existing.Failing = []TestEntry{{Test: "SampleTest.clockSensitive", Status: StatusFlaky, Reason: "time of day", Ticket: "ISSUE-9"}}
	existing.Diagnostics = []DiagnosticEntry{{Code: "GLADESEMA001", File: "src/A.cls", Line: 3, Status: StatusFlaky, Reason: "order", Ticket: "ISSUE-9"}}

	golden, stats := BuildGolden(&existing, "sample-app", baseObserved())
	if len(golden.Failing) != 1 || golden.Failing[0].Status != StatusFlaky || golden.Failing[0].Ticket != "ISSUE-9" {
		t.Fatalf("Failing = %+v", golden.Failing)
	}
	if len(golden.Diagnostics) != 1 || golden.Diagnostics[0].Status != StatusFlaky {
		t.Fatalf("Diagnostics = %+v", golden.Diagnostics)
	}
	if stats.Dropped != 0 || stats.Unclassified != 0 {
		t.Fatalf("stats = %+v", stats)
	}
	if diff := Compare(golden, baseObserved()); diff.Line() != "OK" {
		t.Fatalf("refreshed golden against the same run: %q", diff.Line())
	}
}

// A test that moved between failing and timed out keeps its classification and
// is listed once.
func TestBuildGoldenCarriesClassificationBetweenFailingAndTimeouts(t *testing.T) {
	existing := baseGolden()
	existing.Failing = []TestEntry{{Test: "SampleTest.nowHangs", Status: StatusFlaky, Reason: "r", Ticket: "ISSUE-1"}}
	existing.Timeouts = []TestEntry{{Test: "SampleTest.nowFails", Status: StatusKnownGap, Reason: "s", Ticket: "ISSUE-2"}}
	observed := baseObserved()
	observed.Timeouts = testEntries("SampleTest.nowHangs")
	observed.Failing = testEntries("SampleTest.nowFails")

	golden, _ := BuildGolden(&existing, "sample-app", observed)
	if len(golden.Failing) != 1 || golden.Failing[0].Test != "SampleTest.nowFails" || golden.Failing[0].Ticket != "ISSUE-2" {
		t.Fatalf("Failing = %+v", golden.Failing)
	}
	if len(golden.Timeouts) != 1 || golden.Timeouts[0].Test != "SampleTest.nowHangs" || golden.Timeouts[0].Ticket != "ISSUE-1" {
		t.Fatalf("Timeouts = %+v", golden.Timeouts)
	}
}

func TestBuildGoldenCopiesOfADiagnosticKeepTheirOwnClassification(t *testing.T) {
	existing := baseGolden()
	existing.Diagnostics = []DiagnosticEntry{{Code: "GLADESEMA001", File: "src/A.cls", Line: 3, Status: StatusEnv, Ticket: "ISSUE-3"}}
	observed := baseObserved()
	entry := DiagnosticEntry{Code: "GLADESEMA001", File: "src/A.cls", Line: 3}
	observed.Diagnostics = []DiagnosticEntry{entry, entry}

	golden, stats := BuildGolden(&existing, "sample-app", observed)
	if len(golden.Diagnostics) != 2 || golden.Diagnostics[0].Status != StatusEnv || golden.Diagnostics[1].Status != StatusUnclassified {
		t.Fatalf("Diagnostics = %+v", golden.Diagnostics)
	}
	if stats.Unclassified != 1 {
		t.Fatalf("stats.Unclassified = %d", stats.Unclassified)
	}
}

// The refreshed golden has to read as OK against the run it was built from,
// whatever that run contained.
func TestBuildGoldenThenCompareIsOK(t *testing.T) {
	observed := baseObserved()
	observed.Failing = testEntries("SampleTest.a")
	observed.Timeouts = testEntries("SampleTest.b")
	entry := DiagnosticEntry{Code: "GLADESEMA001", File: "src/A.cls", Line: 3}
	observed.Diagnostics = []DiagnosticEntry{entry, entry}

	golden, _ := BuildGolden(nil, "sample-app", observed)
	if diff := Compare(golden, observed); diff.Infra == "" {
		t.Fatalf("Line() = %q, diff %+v", diff.Line(), diff)
	}
}

func TestGoldenJSONMatchesTheDocumentedFormat(t *testing.T) {
	golden := Golden{
		Repo:        "sample-app",
		Pin:         "abc",
		Overlay:     Overlay{Deps: []string{"sample-dep"}},
		Glade:       GladeInfo{SourceAPI: "67.0"},
		Counts:      Counts{Classes: 139, Tests: 612, Passed: 609},
		Failing:     []TestEntry{{Test: "SampleTest.a", Status: StatusKnownGap, Reason: "r", Ticket: "ISSUE-123"}},
		Diagnostics: []DiagnosticEntry{{Code: "GLADESEMA013", File: "src/X.cls", Line: 42, Status: StatusIntended, Reason: "r", Ticket: "ISSUE-358"}},
		WallSeconds: 145,
	}
	data, err := json.Marshal(golden)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if got := decoded["overlay"].(map[string]any)["config"]; got == nil {
		t.Fatalf("overlay.config missing: %s", data)
	}
	if got := decoded["glade"].(map[string]any)["sourceApi"]; got != "67.0" {
		t.Fatalf("glade.sourceApi = %v", got)
	}
	counts := decoded["counts"].(map[string]any)
	if counts["classes"] != float64(139) || counts["tests"] != float64(612) || counts["passed"] != float64(609) {
		t.Fatalf("counts = %v", counts)
	}
	failing := decoded["failing"].([]any)[0].(map[string]any)
	if failing["test"] != "SampleTest.a" || failing["status"] != "known-gap" || failing["reason"] != "r" || failing["ticket"] != "ISSUE-123" {
		t.Fatalf("failing = %v", failing)
	}
	diagnostic := decoded["diagnostics"].([]any)[0].(map[string]any)
	if diagnostic["code"] != "GLADESEMA013" || diagnostic["file"] != "src/X.cls" || diagnostic["line"] != float64(42) || diagnostic["status"] != "intended" {
		t.Fatalf("diagnostic = %v", diagnostic)
	}
	if decoded["wallSeconds"] != float64(145) {
		t.Fatalf("wallSeconds = %v", decoded["wallSeconds"])
	}
}

func TestLoadGoldenRejectsUnknownStatus(t *testing.T) {
	config := t.TempDir()
	path := GoldenPath(config, "sample-app")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"repo":"sample-app","failing":[{"test":"a","status":"probably-fine"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadGolden(config, "sample-app")
	if err == nil || !strings.Contains(err.Error(), "probably-fine") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadGoldenRejectsAnotherReposFile(t *testing.T) {
	config := t.TempDir()
	path := GoldenPath(config, "sample-app")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"repo":"sample-lib"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadGolden(config, "sample-app"); err == nil || !strings.Contains(err.Error(), "sample-lib") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadGoldenMissingIsNotExist(t *testing.T) {
	_, err := LoadGolden(t.TempDir(), "sample-app")
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v", err)
	}
}

func TestWriteGoldenRoundTrips(t *testing.T) {
	config := t.TempDir()
	golden := baseGolden()
	golden.Failing = []TestEntry{{Test: "SampleTest.a", Status: StatusKnownGap, Reason: "r", Ticket: "ISSUE-1"}}
	if err := WriteGolden(config, golden); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadGolden(config, "sample-app")
	if err != nil {
		t.Fatal(err)
	}
	if diff := Compare(loaded, Observed{
		Pin: golden.Pin, Overlay: golden.Overlay, Glade: golden.Glade, Counts: golden.Counts,
		Failing: testEntries("SampleTest.a"), WallSeconds: golden.WallSeconds,
	}); diff.Line() != "OK" {
		t.Fatalf("Line() = %q", diff.Line())
	}
	data, err := os.ReadFile(GoldenPath(config, "sample-app"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(data), "\n") || !strings.Contains(string(data), "\n  \"repo\": \"sample-app\"") {
		t.Fatalf("golden is not indented JSON with a final newline:\n%s", data)
	}
}

func writeTestConfig(t *testing.T, config, tiers string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(config, TiersFile), []byte(tiers), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeRunReport(t *testing.T, path string, results ...RepoResult) {
	t.Helper()
	data, err := json.Marshal(NewRunReport("A", results))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBuildFromRunWritesGoldenAndRefreshes(t *testing.T) {
	config := t.TempDir()
	writeTestConfig(t, config, `{"schemaVersion":1,"repos":[{"name":"sample-app","tier":"A","clone":"x","pin":"p1"}]}`)
	runPath := filepath.Join(t.TempDir(), "run.json")
	writeRunReport(t, runPath, RepoResult{
		Repo: "sample-app", Complete: true,
		Observed: Observed{Pin: "p1", Failing: testEntries("SampleTest.a"), Counts: Counts{Tests: 2, Passed: 1}, WallSeconds: 3},
		Diff:     Diff{Infra: "no golden"},
	})

	stats, err := BuildFromRun(config, "sample-app", runPath)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Unclassified != 1 {
		t.Fatalf("stats = %+v", stats)
	}
	golden, err := LoadGolden(config, "sample-app")
	if err != nil {
		t.Fatal(err)
	}
	if golden.Failing[0].Status != StatusUnclassified {
		t.Fatalf("golden = %+v", golden)
	}

	// A human fills the entry in; the next refresh must keep it.
	golden.Failing[0].Status = StatusKnownGap
	golden.Failing[0].Reason = "hosted boundary"
	golden.Failing[0].Ticket = "ISSUE-1"
	if err := WriteGolden(config, golden); err != nil {
		t.Fatal(err)
	}
	stats, err = BuildFromRun(config, "sample-app", runPath)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Unclassified != 0 {
		t.Fatalf("stats = %+v", stats)
	}
	golden, err = LoadGolden(config, "sample-app")
	if err != nil {
		t.Fatal(err)
	}
	if got := golden.Failing[0]; got.Status != StatusKnownGap || got.Reason != "hosted boundary" || got.Ticket != "ISSUE-1" {
		t.Fatalf("entry = %+v", got)
	}
}

func TestBuildFromRunRefusals(t *testing.T) {
	config := t.TempDir()
	writeTestConfig(t, config, `{"schemaVersion":1,"repos":[{"name":"sample-app","tier":"A","clone":"x","pin":"p1"}]}`)
	runPath := filepath.Join(t.TempDir(), "run.json")

	writeRunReport(t, runPath, RepoResult{Repo: "sample-app", Complete: false, Diff: Diff{Infra: "check timeout"}})
	if _, err := BuildFromRun(config, "sample-app", runPath); err == nil || !strings.Contains(err.Error(), "check timeout") {
		t.Fatalf("incomplete run: err = %v", err)
	}

	writeRunReport(t, runPath, RepoResult{Repo: "sample-lib", Complete: true})
	if _, err := BuildFromRun(config, "sample-app", runPath); err == nil || !strings.Contains(err.Error(), "not in") {
		t.Fatalf("repo not in run: err = %v", err)
	}

	writeRunReport(t, runPath, RepoResult{Repo: "sample-other", Complete: true})
	if _, err := BuildFromRun(config, "sample-other", runPath); err == nil || !strings.Contains(err.Error(), TiersFile) {
		t.Fatalf("repo not in config: err = %v", err)
	}

	writeRunReport(t, runPath, RepoResult{Repo: "sample-app", Complete: true, Observed: Observed{Pin: "p2"}})
	if _, err := BuildFromRun(config, "sample-app", runPath); err == nil || !strings.Contains(err.Error(), "pin") {
		t.Fatalf("stale run: err = %v", err)
	}

	if _, err := os.Stat(GoldenPath(config, "sample-app")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a refused build must not write a golden: %v", err)
	}
}

func TestClassifyUnclassifiedLeavesClassifiedEntriesAlone(t *testing.T) {
	dir := t.TempDir()
	g := baseGolden()
	g.Repo = "sample-app"
	g.Failing = []TestEntry{
		{Test: "SampleTest.kept", Status: StatusIntended, Reason: "old reason", Ticket: "ISSUE-1"},
		{Test: "SampleTest.open", Status: StatusUnclassified},
	}
	g.Timeouts = []TestEntry{{Test: "SampleTest.hangs", Status: StatusUnclassified}}
	g.Diagnostics = []DiagnosticEntry{{Code: "GLADESEMA001", File: "src/A.cls", Line: 3, Status: StatusUnclassified}}
	if err := WriteGolden(dir, g); err != nil {
		t.Fatal(err)
	}
	n, err := ClassifyUnclassified(dir, "sample-app", Classification{Status: StatusKnownGap, Reason: "baseline", Ticket: "ISSUE-9"})
	if err != nil || n != 3 {
		t.Fatalf("changed %d, err %v", n, err)
	}
	got, err := LoadGolden(dir, "sample-app")
	if err != nil {
		t.Fatal(err)
	}
	if e := got.Failing[0]; e.Status != StatusIntended || e.Reason != "old reason" || e.Ticket != "ISSUE-1" {
		t.Fatalf("classified entry changed: %+v", e)
	}
	if e := got.Failing[1]; e.Status != StatusKnownGap || e.Reason != "baseline" || e.Ticket != "ISSUE-9" {
		t.Fatalf("open entry = %+v", e)
	}
	if e := got.Timeouts[0]; e.Status != StatusKnownGap {
		t.Fatalf("timeout = %+v", e)
	}
	if e := got.Diagnostics[0]; e.Status != StatusKnownGap || e.Ticket != "ISSUE-9" {
		t.Fatalf("diagnostic = %+v", e)
	}
	if n, err := ClassifyUnclassified(dir, "sample-app", Classification{Status: StatusEnv, Reason: "r", Ticket: "T"}); err != nil || n != 0 {
		t.Fatalf("second pass changed %d, err %v", n, err)
	}
}

func TestClassifyUnclassifiedRejectsIncompleteClassification(t *testing.T) {
	dir := t.TempDir()
	for _, c := range []Classification{
		{Status: StatusUnclassified, Reason: "r", Ticket: "T"},
		{Status: "bogus", Reason: "r", Ticket: "T"},
		{Status: StatusKnownGap, Ticket: "T"},
		{Status: StatusKnownGap, Reason: "r"},
	} {
		if _, err := ClassifyUnclassified(dir, "sample-app", c); err == nil {
			t.Fatalf("accepted %+v", c)
		}
	}
}
