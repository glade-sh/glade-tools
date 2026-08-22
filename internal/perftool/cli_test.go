package perftool

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/internal/trace"
)

func TestManifestJSONListsPerformanceScan(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"manifest", "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run returned %d, stderr=%s", code, stderr.String())
	}

	var manifest struct {
		APIVersion string `json:"apiVersion"`
		Name       string `json:"name"`
		Commands   []struct {
			Path []string `json:"path"`
		} `json:"commands"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &manifest); err != nil {
		t.Fatalf("manifest is not JSON: %v\n%s", err, stdout.String())
	}
	if manifest.APIVersion != "glade.plugin.v1" || manifest.Name != "performance" {
		t.Fatalf("unexpected manifest identity: %#v", manifest)
	}
	if len(manifest.Commands) != 1 || len(manifest.Commands[0].Path) != 1 || manifest.Commands[0].Path[0] != "performance" {
		t.Fatalf("unexpected commands: %#v", manifest.Commands)
	}
}

func TestHelpListsEntrypointsFlagsAndExamples(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"--help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run returned %d, stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"Entrypoints:",
		"glade performance scan [flags]",
		"glade-plugin-performance performance scan [flags]",
		"Flags:",
		"--trace <path>",
		"--org-facts <path>",
		"--fail-on none|high|measured",
		"Examples:",
		"glade performance scan --project . --trace reports/slow.trace.json --top 10",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("help omitted %q:\n%s", want, out)
		}
	}
}

func TestManifestJSONListsPerformanceEditorAction(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"manifest", "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run returned %d, stderr=%s", code, stderr.String())
	}

	var manifest struct {
		Editor struct {
			Actions []struct {
				ID       string   `json:"id"`
				Title    string   `json:"title"`
				View     string   `json:"view"`
				Contexts []string `json:"contexts"`
				Command  []string `json:"command"`
				Args     []string `json:"args"`
				Output   string   `json:"output"`
				Icon     string   `json:"icon"`
			} `json:"actions"`
		} `json:"editor"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &manifest); err != nil {
		t.Fatalf("manifest is not JSON: %v\n%s", err, stdout.String())
	}
	if len(manifest.Editor.Actions) != 1 {
		t.Fatalf("editor actions = %#v", manifest.Editor.Actions)
	}
	action := manifest.Editor.Actions[0]
	if action.ID != "performance.scanProject" ||
		action.Title != "Scan Performance Risks" ||
		action.View != "startHere" ||
		action.Output != "glade.findings.v1" ||
		action.Icon != "pulse" ||
		!stringSlicesEqual(action.Contexts, []string{"project"}) ||
		!stringSlicesEqual(action.Command, []string{"performance"}) ||
		!stringSlicesEqual(action.Args, []string{"--project", "${projectRoot}", "--json", "--editor-findings"}) {
		t.Fatalf("unexpected editor action: %#v", action)
	}
}

func TestPerformanceScanJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	project := filepath.Join("..", "perfscan", "testdata", "perf-project")
	code := Run(context.Background(), []string{"performance", "scan", "--project", project, "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run returned %d, stderr=%s", code, stderr.String())
	}

	var report struct {
		SchemaVersion int `json:"schemaVersion"`
		Summary       struct {
			Findings int `json:"findings"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("report is not JSON: %v\n%s", err, stdout.String())
	}
	if report.SchemaVersion == 0 || report.Summary.Findings == 0 {
		t.Fatalf("expected performance findings, got %#v", report)
	}
}

func TestPerformanceScanEditorFindingsJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	project := filepath.Join("..", "perfscan", "testdata", "perf-project")
	code := Run(context.Background(), []string{"performance", "--project", project, "--json", "--editor-findings"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run returned %d, stderr=%s", code, stderr.String())
	}

	var payload struct {
		Kind     string `json:"kind"`
		Summary  string `json:"summary"`
		Findings []struct {
			Severity string `json:"severity"`
			Message  string `json:"message"`
			File     string `json:"file"`
			Line     int    `json:"line"`
			RuleID   string `json:"ruleId"`
			Source   string `json:"source"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("payload is not JSON: %v\n%s", err, stdout.String())
	}
	if payload.Kind != "glade.findings.v1" || len(payload.Findings) == 0 {
		t.Fatalf("unexpected payload: %s", stdout.String())
	}
	first := payload.Findings[0]
	if first.Source != "performance" || first.RuleID == "" || first.Message == "" {
		t.Fatalf("unexpected first finding: %#v", first)
	}
	if !strings.Contains(payload.Summary, "finding") {
		t.Fatalf("summary = %q", payload.Summary)
	}
}

func TestPerformanceScanFormatJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	project := filepath.Join("..", "perfscan", "testdata", "perf-project")
	code := Run(context.Background(), []string{"performance", "scan", "--project", project, "--format", "json", "--top", "1"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run returned %d, stderr=%s", code, stderr.String())
	}

	var report struct {
		Findings []struct {
			ID string `json:"id"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("report is not JSON: %v\n%s", err, stdout.String())
	}
	if len(report.Findings) != 1 {
		t.Fatalf("top filter did not trim findings: %#v", report.Findings)
	}
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestPerformanceScanFormatSARIF(t *testing.T) {
	var stdout, stderr bytes.Buffer
	project := filepath.Join("..", "perfscan", "testdata", "perf-project")
	code := Run(context.Background(), []string{"performance", "scan", "--project", project, "--format", "sarif", "--top", "1"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run returned %d, stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"version": "2.1.0"`) || !strings.Contains(stdout.String(), `"tool"`) {
		t.Fatalf("SARIF output missing expected fields:\n%s", stdout.String())
	}
}

func TestPerformanceScanFailOnHigh(t *testing.T) {
	var stdout, stderr bytes.Buffer
	project := filepath.Join("..", "perfscan", "testdata", "perf-project")
	code := Run(context.Background(), []string{"performance", "scan", "--project", project, "--format", "json", "--fail-on", "high"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("Run returned %d, stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "performance findings meet --fail-on high") {
		t.Fatalf("stderr = %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), `"findings"`) {
		t.Fatalf("expected report on stdout, got %s", stdout.String())
	}
}

func TestPerformanceScanMinConfidenceFiltersStaticFindings(t *testing.T) {
	var stdout, stderr bytes.Buffer
	project := filepath.Join("..", "perfscan", "testdata", "perf-project")
	code := Run(context.Background(), []string{"performance", "scan", "--project", project, "--format", "json", "--min-confidence", "measured"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run returned %d, stderr=%s", code, stderr.String())
	}

	var report struct {
		Summary struct {
			Findings int `json:"findings"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("report is not JSON: %v\n%s", err, stdout.String())
	}
	if report.Summary.Findings != 0 {
		t.Fatalf("expected static findings to be filtered, got %#v", report.Summary)
	}
}

func TestPerformanceScanAppliesTopAfterMinConfidence(t *testing.T) {
	tracePath := filepath.Join(t.TempDir(), "trace.json")
	var traceJSON bytes.Buffer
	if err := trace.WriteJSON(&traceJSON, trace.NewDocument([]trace.Event{
		trace.Duration("apex.method.TraceOnly.run", "apex.method", 0, 150000, map[string]any{
			trace.ArgFile: "TraceOnly.cls",
			trace.ArgLine: 1,
		}),
	})); err != nil {
		t.Fatal(err)
	}
	writeCLITestFile(t, tracePath, traceJSON.String())

	var stdout, stderr bytes.Buffer
	project := filepath.Join("..", "perfscan", "testdata", "perf-project")
	code := Run(context.Background(), []string{
		"performance", "scan",
		"--project", project,
		"--trace", tracePath,
		"--format", "json",
		"--min-confidence", "measured",
		"--top", "1",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run returned %d, stderr=%s", code, stderr.String())
	}

	var report struct {
		Findings []struct {
			ID         string `json:"id"`
			Confidence string `json:"confidence"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("report is not JSON: %v\n%s", err, stdout.String())
	}
	if len(report.Findings) != 1 || report.Findings[0].ID != "perf.measured.hot-span" || report.Findings[0].Confidence != "measured" {
		t.Fatalf("unexpected findings after measured top filter: %#v", report.Findings)
	}
}

func TestPerformanceScanOrgFactsFlag(t *testing.T) {
	root := t.TempDir()
	writeCLITestFile(t, filepath.Join(root, "sfdx-project.json"), `{"packageDirectories":[{"path":"force-app","default":true}],"sourceApiVersion":"65.0"}`)
	writeCLITestFile(t, filepath.Join(root, "force-app/main/default/classes/QueryRisk.cls"), `
public class QueryRisk {
  public static List<Account> byFormula(String value) {
    return [SELECT Id FROM Account WHERE Formula_Key__c = :value];
  }
}`)
	orgFactsPath := filepath.Join(t.TempDir(), "org-facts.json")
	writeCLITestFile(t, orgFactsPath, `{
  "schemaVersion": 1,
  "objects": {
    "Account": {
      "estimatedRows": 1200000,
      "fields": {
        "Formula_Key__c": {"formula": true}
      }
    }
  }
}`)

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"performance", "scan", "--project", root, "--org-facts", orgFactsPath, "--format", "json", "--fail-on", "none"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run returned %d, stderr=%s", code, stderr.String())
	}

	var report struct {
		Findings []struct {
			ID string `json:"id"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("report is not JSON: %v\n%s", err, stdout.String())
	}
	for _, finding := range report.Findings {
		if finding.ID == "perf.soql.query-plan-risk" {
			return
		}
	}
	t.Fatalf("missing org facts finding in %#v", report.Findings)
}

func writeCLITestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
