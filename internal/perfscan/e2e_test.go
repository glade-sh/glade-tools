package perfscan

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var updateGolden = flag.Bool("update", false, "update golden files")

func TestPerformanceScanGoldenReport(t *testing.T) {
	root := filepath.Join("testdata", "perf-project")
	report, err := AnalyzeProject(Options{ProjectRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	report = normalizeReportForGolden(t, report, root)
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	golden := filepath.Join("testdata", "golden", "perf-project-report.json")
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != string(data) {
		t.Fatalf("golden mismatch\nwant:\n%s\n\ngot:\n%s", string(want), string(data))
	}
}

func normalizeReportForGolden(t *testing.T, report Report, root string) Report {
	t.Helper()
	absRoot, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	absRoot = filepath.ToSlash(absRoot)
	normalizePath := func(path string) string {
		path = filepath.ToSlash(path)
		if path == absRoot {
			return "$PROJECT"
		}
		if rel, ok := strings.CutPrefix(path, absRoot+"/"); ok {
			return "$PROJECT/" + rel
		}
		return path
	}
	report.Project = normalizePath(report.Project)
	for i := range report.Findings {
		normalizeFindingPaths(&report.Findings[i], normalizePath)
	}
	for i := range report.EntryPoints {
		report.EntryPoints[i].File = normalizePath(report.EntryPoints[i].File)
	}
	for i := range report.Measurements {
		report.Measurements[i].File = normalizePath(report.Measurements[i].File)
		normalizePathSteps(report.Measurements[i].Path, normalizePath)
		for j := range report.Measurements[i].Evidence {
			normalizeEvidencePaths(&report.Measurements[i].Evidence[j], normalizePath)
		}
	}
	return report
}

func normalizeFindingPaths(finding *Finding, normalizePath func(string) string) {
	finding.EntryPoint.File = normalizePath(finding.EntryPoint.File)
	finding.Location.File = normalizePath(finding.Location.File)
	normalizePathSteps(finding.Path, normalizePath)
	for i := range finding.Evidence {
		normalizeEvidencePaths(&finding.Evidence[i], normalizePath)
	}
}

func normalizeEvidencePaths(evidence *Evidence, normalizePath func(string) string) {
	normalizePathSteps(evidence.Path, normalizePath)
}

func normalizePathSteps(path []PathStep, normalizePath func(string) string) {
	for i := range path {
		path[i].File = normalizePath(path[i].File)
	}
}
