package corpuscheck

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCheckSimulatesSourceAPIUpgradeInTemporaryMirror(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture uses sh")
	}
	root := t.TempDir()
	sfdx := filepath.Join(root, "sfdx-project")
	metadata := filepath.Join(root, "metadata-package")
	files := map[string]string{
		filepath.Join(sfdx, "sfdx-project.json"):                                       `{"packageDirectories":[{"path":"force-app","default":true}],"sourceApiVersion":"64.0"}`,
		filepath.Join(sfdx, "force-app/main/default/classes/Legacy.cls"):               "public class Legacy {}",
		filepath.Join(sfdx, "force-app/main/default/classes/Legacy.cls-meta.xml"):      `<ApexClass><apiVersion>61.0</apiVersion></ApexClass>`,
		filepath.Join(sfdx, "force-app/main/default/classes/Current.cls"):              "public class Current {}",
		filepath.Join(sfdx, "force-app/main/default/classes/Current.cls-meta.xml"):     `<ApexClass><apiVersion>66.0</apiVersion></ApexClass>`,
		filepath.Join(sfdx, "force-app/main/default/triggers/Legacy.trigger"):          "trigger Legacy on Account (before insert) {}",
		filepath.Join(sfdx, "force-app/main/default/triggers/Legacy.trigger-meta.xml"): `<ApexTrigger><apiVersion>52.0</apiVersion></ApexTrigger>`,
		filepath.Join(metadata, "package.xml"):                                         `<Package/>`,
		filepath.Join(metadata, "classes/MetadataLegacy.cls"):                          "public class MetadataLegacy {}",
		filepath.Join(metadata, "classes/MetadataLegacy.cls-meta.xml"):                 `<ApexClass><apiVersion>45.0</apiVersion></ApexClass>`,
		filepath.Join(metadata, "lwc/example/example.js-meta.xml"):                     `<LightningComponentBundle><apiVersion>48.0</apiVersion></LightningComponentBundle>`,
	}
	for path, content := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	capture := filepath.Join(root, "capture.tsv")
	glade := filepath.Join(root, "fake-glade.sh")
	script := `#!/bin/sh
printf 'project\t%s\n' "$3" >> '` + capture + `'
find "$3" -type f \( -name 'sfdx-project.json' -o -name '*-meta.xml' \) | sort | while IFS= read -r file; do
  relative=${file#"$3"/}
  printf '%s\t' "$relative" >> '` + capture + `'
  tr -d '\n' < "$file" >> '` + capture + `'
  printf '\n' >> '` + capture + `'
done
printf '{"diagnostics":[]}'
`
	if err := os.WriteFile(glade, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, "out")

	_, err := Check(context.Background(), Options{Root: root, Glade: glade, OutDir: out, SimulateSourceAPIVersion: "65.0"})
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range files {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Fatalf("source changed at %s: %q, %v", path, got, err)
		}
	}
	captured, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	text := string(captured)
	for _, want := range []string{
		`"sourceApiVersion": "65.0"`,
		"classes/Legacy.cls-meta.xml\t<ApexClass><apiVersion>65.0</apiVersion></ApexClass>",
		"classes/Current.cls-meta.xml\t<ApexClass><apiVersion>66.0</apiVersion></ApexClass>",
		"triggers/Legacy.trigger-meta.xml\t<ApexTrigger><apiVersion>65.0</apiVersion></ApexTrigger>",
		"classes/MetadataLegacy.cls-meta.xml\t<ApexClass><apiVersion>65.0</apiVersion></ApexClass>",
		"lwc/example/example.js-meta.xml\t<LightningComponentBundle><apiVersion>48.0</apiVersion></LightningComponentBundle>",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("capture missing %q:\n%s", want, text)
		}
	}
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		if !strings.HasPrefix(line, "project\t") {
			continue
		}
		temporaryProject := strings.TrimPrefix(line, "project\t")
		if strings.HasPrefix(temporaryProject, root+string(os.PathSeparator)) {
			t.Fatalf("candidate ran against source tree: %s", temporaryProject)
		}
		if _, err := os.Stat(temporaryProject); !os.IsNotExist(err) {
			t.Fatalf("temporary project was not removed: %s, %v", temporaryProject, err)
		}
	}
	receiptData, err := os.ReadFile(filepath.Join(out, "upgrade-simulation.json"))
	if err != nil {
		t.Fatal(err)
	}
	var receipt struct {
		TargetSourceAPIVersion             string `json:"targetSourceApiVersion"`
		OriginalVersionCorrectnessMeasured bool   `json:"originalVersionCorrectnessMeasured"`
		RuntimeProof                       bool   `json:"runtimeProof"`
		Changes                            []struct {
			Project          string `json:"project"`
			Path             string `json:"path"`
			Family           string `json:"family"`
			OriginalVersion  string `json:"originalVersion"`
			SimulatedVersion string `json:"simulatedVersion"`
			OriginalSHA256   string `json:"originalSha256"`
		} `json:"changes"`
	}
	if err := json.Unmarshal(receiptData, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.TargetSourceAPIVersion != "65.0" || receipt.OriginalVersionCorrectnessMeasured || receipt.RuntimeProof || len(receipt.Changes) != 4 {
		t.Fatalf("receipt = %#v", receipt)
	}
	for _, change := range receipt.Changes {
		if change.Project == "" || change.Path == "" || change.Family == "" || change.OriginalVersion == "" || change.SimulatedVersion != "65.0" || len(change.OriginalSHA256) != 64 {
			t.Fatalf("incomplete change provenance: %#v", change)
		}
	}
}

func TestCheckRejectsSignedSourceAPISimulationVersion(t *testing.T) {
	root := t.TempDir()
	writeProject(t, root, "alpha")
	glade := filepath.Join(root, "fake-glade.sh")
	if err := os.WriteFile(glade, []byte("#!/bin/sh\nexit 99\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := Check(context.Background(), Options{Root: root, Glade: glade, OutDir: filepath.Join(root, "out"), SimulateSourceAPIVersion: "+65.0"})
	if err == nil || !strings.Contains(err.Error(), `API version "+65.0" must use MAJOR.0`) {
		t.Fatalf("expected strict target rejection, got %v", err)
	}
}

func TestCheckLabelsRejectedUpgradeTargetAndCleansTemporaryMirror(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture uses sh")
	}
	root := t.TempDir()
	writeProject(t, root, "alpha")
	logPath := filepath.Join(root, "project.log")
	glade := filepath.Join(root, "fake-glade.sh")
	script := "#!/bin/sh\nprintf '%s\\n' \"$3\" > '" + logPath + "'\nprintf 'unsupported source API version 64.0' >&2\nexit 7\n"
	if err := os.WriteFile(glade, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := Check(context.Background(), Options{Root: root, Glade: glade, OutDir: filepath.Join(root, "out"), SimulateSourceAPIVersion: "64.0"})
	if err == nil || !strings.Contains(err.Error(), "source API upgrade simulation to 64.0 failed") || !strings.Contains(err.Error(), "unsupported source API version 64.0") {
		t.Fatalf("expected explicit rejected-target error, got %v", err)
	}
	data, readErr := os.ReadFile(logPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	temporaryProject := strings.TrimSpace(string(data))
	if _, statErr := os.Stat(temporaryProject); !os.IsNotExist(statErr) {
		t.Fatalf("temporary project was not removed after failure: %s, %v", temporaryProject, statErr)
	}
}

func TestCheckWritesClassifiedTSVs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture uses sh")
	}
	root := t.TempDir()
	writeProject(t, root, "alpha")
	writeProject(t, root, "beta")
	glade := filepath.Join(root, "fake-glade.sh")
	if err := os.WriteFile(glade, []byte(`#!/bin/sh
case "$3" in
  */alpha) printf '{"diagnostics":[{"code":"APEXPARSE001","message":"Unexpected token","file":"force-app/main/default/classes/A.cls","line":3,"column":4},{"code":"GLADETYPE001","message":"duplicate declaration","file":"A.cls"}]}' ;;
  */beta) printf '{"diagnostics":[{"code":"GLADEPERF001","message":"slow check","file":"B.cls"},{"code":"GLADESEMA009","message":"No overload matches return-type mismatch contract","file":"B.cls"}]}' ;;
esac
`), 0o755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, "out")

	report, err := Check(context.Background(), Options{Root: root, Glade: glade, OutDir: out})
	if err != nil {
		t.Fatal(err)
	}
	if report.Counts["source-parse-error"] != 1 || report.Counts["project-discovery-duplicate"] != 1 || report.Counts["performance-advisory"] != 1 || report.Counts["docs-contract-mismatch"] != 1 {
		t.Fatalf("counts = %#v", report.Counts)
	}
	for _, name := range []string{"summary.tsv", "diagnostics.tsv", "by_code.tsv", "by_project_code.tsv", "by_stem.tsv", "classified.tsv"} {
		if _, err := os.Stat(filepath.Join(out, name)); err != nil {
			t.Fatalf("%s missing: %v", name, err)
		}
	}
	diagnostics, err := os.ReadFile(filepath.Join(out, "diagnostics.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(diagnostics), "APEXPARSE001") || !strings.Contains(string(diagnostics), "duplicate declaration") {
		t.Fatalf("diagnostics.tsv did not preserve raw diagnostics:\n%s", diagnostics)
	}
}

func TestCheckBypassesCandidateCaches(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture uses sh")
	}
	root := t.TempDir()
	writeProject(t, root, "alpha")
	glade := filepath.Join(root, "fake-glade.sh")
	if err := os.WriteFile(glade, []byte(`#!/bin/sh
case " $* " in
  *" --no-cache "*) printf '{"diagnostics":[]}' ;;
  *) exit 9 ;;
esac
`), 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := Check(context.Background(), Options{Root: root, Glade: glade, OutDir: filepath.Join(root, "out")}); err != nil {
		t.Fatal(err)
	}
}

func TestCheckClassifiesInvalidJSONAsUnclassified(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture uses sh")
	}
	root := t.TempDir()
	writeProject(t, root, "broken")
	glade := filepath.Join(root, "fake-glade.sh")
	if err := os.WriteFile(glade, []byte("#!/bin/sh\nprintf 'not-json'\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := Check(context.Background(), Options{Root: root, Glade: glade, OutDir: filepath.Join(root, "out"), FailOnUnclassified: true, MaxUnclassified: 0})
	if err == nil || !strings.Contains(err.Error(), "unclassified=1 exceeds max 0") {
		t.Fatalf("expected unclassified failure, got %v", err)
	}
}

func TestReportDisallowedFindingsForPublicCheckClosure(t *testing.T) {
	report := Report{Counts: map[string]int{
		"performance-advisory":        3,
		"project-metadata-missing":    2,
		"project-source-invalid":      2,
		"explicit-unsupported":        1,
		"generated-shape-gap":         1,
		"platform-shaped":             1,
		"runtime-open":                1,
		"semantic-contract-gap":       1,
		"source-parse-error":          1,
		"unclassified":                1,
		"project-discovery-duplicate": 1,
	}}
	got := DisallowedForCheckClosure(report)
	want := map[string]int{
		"runtime-open":          1,
		"semantic-contract-gap": 1,
		"source-parse-error":    1,
		"unclassified":          1,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DisallowedForCheckClosure() = %#v, want %#v", got, want)
	}
}

func TestCheckAllowsRerunWithOwnedCorpusReportsInOutDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture uses sh")
	}
	root := t.TempDir()
	writeProject(t, root, "alpha")
	glade := filepath.Join(root, "fake-glade.sh")
	if err := os.WriteFile(glade, []byte("#!/bin/sh\nprintf '{\"diagnostics\":[]}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, "test-results")

	if _, err := Check(context.Background(), Options{Root: root, Glade: glade, OutDir: out}); err != nil {
		t.Fatal(err)
	}
	if _, err := Check(context.Background(), Options{Root: root, Glade: glade, OutDir: out}); err != nil {
		t.Fatalf("second corpus check should overwrite owned report files: %v", err)
	}
}

func TestCheckRemovesOwnedUpgradeReceiptWhenSimulationIsDisabled(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture uses sh")
	}
	root := t.TempDir()
	writeProject(t, root, "alpha")
	glade := filepath.Join(root, "fake-glade.sh")
	if err := os.WriteFile(glade, []byte("#!/bin/sh\nprintf '{\"diagnostics\":[]}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, "out")

	if _, err := Check(context.Background(), Options{Root: root, Glade: glade, OutDir: out, SimulateSourceAPIVersion: "65.0"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "upgrade-simulation.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := Check(context.Background(), Options{Root: root, Glade: glade, OutDir: out}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "upgrade-simulation.json")); !os.IsNotExist(err) {
		t.Fatalf("stale upgrade receipt survived ordinary corpus check: %v", err)
	}
}

func TestCheckRejectsStaleSurfaceReportsInOutDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture uses sh")
	}
	root := t.TempDir()
	writeProject(t, root, "alpha")
	glade := filepath.Join(root, "fake-glade.sh")
	if err := os.WriteFile(glade, []byte("#!/bin/sh\nprintf '{\"diagnostics\":[]}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, "test-results")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"summary.tsv", "SURFACE_DASHBOARD.md"} {
		if err := os.WriteFile(filepath.Join(out, name), []byte("old report\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	_, err := Check(context.Background(), Options{Root: root, Glade: glade, OutDir: out})
	if err == nil {
		t.Fatal("expected stale output rejection")
	}
	for _, want := range []string{"stale generated report", "SURFACE_DASHBOARD.md"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error missing %q: %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "summary.tsv") {
		t.Fatalf("owned corpus report should not be stale: %v", err)
	}
}

func TestCheckReportSummaryCountsClosureBlockingDiagnostics(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture uses sh")
	}
	root := t.TempDir()
	writeProject(t, root, "alpha")
	glade := filepath.Join(root, "fake-glade.sh")
	if err := os.WriteFile(glade, []byte(`#!/bin/sh
printf '{"diagnostics":[{"code":"GLADEPERF001","message":"slow check","file":"A.cls"},{"code":"GLADESEMA009","message":"No overload matches return-type mismatch contract","file":"B.cls"},{"code":"UNKNOWN001","message":"unknown diagnostic","file":"C.cls"}]}'
`), 0o755); err != nil {
		t.Fatal(err)
	}

	report, err := Check(context.Background(), Options{Root: root, Glade: glade, OutDir: filepath.Join(root, "out")})
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.ProjectCount != 1 || report.Summary.DiagnosticCount != 3 || report.Summary.UnclassifiedCount != 1 || report.Summary.ClosureBlockingCount != 2 {
		t.Fatalf("summary = %#v", report.Summary)
	}
}

func TestReportSummaryIsComputedByCheck(t *testing.T) {
	report := Report{
		Projects:    []ProjectResult{{Name: "alpha"}, {Name: "beta"}},
		Diagnostics: []ClassifiedDiagnostic{{Class: "performance-advisory"}, {Class: "semantic-contract-gap"}, {Class: "unclassified"}},
		Counts: map[string]int{
			"performance-advisory":  1,
			"semantic-contract-gap": 1,
			"unclassified":          1,
		},
	}
	if report.Summary != (ReportSummary{}) {
		t.Fatalf("summary should not be precomputed on plain Report literals: %#v", report.Summary)
	}
	got := summarizeReport(report)
	want := ReportSummary{ProjectCount: 2, DiagnosticCount: 3, UnclassifiedCount: 1, ClosureBlockingCount: 2}
	if got != want {
		t.Fatalf("summarizeReport() = %#v, want %#v", got, want)
	}
}

func TestDiscoverProjectsSkipsAggregateRootWhenNestedProjectsExist(t *testing.T) {
	root := t.TempDir()
	writeProject(t, root, "LightningFlowComponents")
	writeProject(t, filepath.Join(root, "LightningFlowComponents", "flow_action_components"), "PostRichChatter")
	writeProject(t, filepath.Join(root, "LightningFlowComponents", "flow_screen_components"), "QuickQuery")
	writeProject(t, filepath.Join(root, "LightningFlowComponents", "zz_after_sfdx_project"), "NestedAfterRootManifest")

	projects, err := discoverProjects(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, project := range projects {
		if filepath.Base(project) == "LightningFlowComponents" {
			t.Fatalf("aggregate root should not be checked when nested projects exist: %#v", projects)
		}
	}
	if len(projects) != 3 {
		t.Fatalf("projects = %#v, want 3 nested projects", projects)
	}
}

func TestCheckDiscoversAndRunsMetadataPackageRoots(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture uses sh")
	}
	root := t.TempDir()
	writeProject(t, root, "sfdx-project")
	metadataRoot := filepath.Join(root, "metadata-package")
	if err := os.MkdirAll(filepath.Join(root, "sfdx-project", "manifest"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sfdx-project", "package.xml"), []byte("<Package/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sfdx-project", "manifest", "package.xml"), []byte("<Package/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(metadataRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(metadataRoot, "package.xml"), []byte("<Package/>"), 0o644); err != nil {
		t.Fatal(err)
	}

	logPath := filepath.Join(root, "projects.log")
	glade := filepath.Join(root, "fake-glade.sh")
	if err := os.WriteFile(glade, []byte("#!/bin/sh\nprintf '%s\\n' \"$3\" >> \""+logPath+"\"\nprintf '{\"diagnostics\":[]}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	report, err := Check(context.Background(), Options{Root: root, Glade: glade, OutDir: filepath.Join(root, "out")})
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.ProjectCount != 2 {
		t.Fatalf("project count = %d, want 2: %#v", report.Summary.ProjectCount, report.Projects)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSpace(string(data)), "\n")
	want := []string{filepath.Join(root, "metadata-package"), filepath.Join(root, "sfdx-project")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("run projects = %#v, want %#v", got, want)
	}
}

func TestClassifyClosureAllowedDiagnosticsAreNarrow(t *testing.T) {
	tests := []struct {
		name string
		diag ClassifiedDiagnostic
		want string
	}{
		{
			name: "stable explicit unsupported LWC code is allowed",
			diag: ClassifiedDiagnostic{Code: "GLADELWC060", Message: "GLADELWC060 base component unsupported: lightning-map"},
			want: "explicit-unsupported",
		},
		{
			name: "stable generated shape code is allowed",
			diag: ClassifiedDiagnostic{Code: "GLADEGEN_SHAPE", Message: "generated standard symbol missing: System.Address"},
			want: "generated-shape-gap",
		},
		{
			name: "stable platform LWC module code is allowed",
			diag: ClassifiedDiagnostic{Code: "GLADELWC092", Message: "platformUtilityBarApi requires Salesforce platform shell"},
			want: "platform-shaped",
		},
		{
			name: "unsupported prose alone is not allowed",
			diag: ClassifiedDiagnostic{Code: "UNKNOWN001", Message: "unsupported overload should be fixed"},
			want: "unclassified",
		},
		{
			name: "standard symbol prose alone is not allowed",
			diag: ClassifiedDiagnostic{Code: "UNKNOWN001", Message: "standard symbol shape gap should be fixed"},
			want: "unclassified",
		},
		{
			name: "sema diagnostic with unsupported prose stays semantic",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA009", Message: "unsupported overload should be fixed"},
			want: "semantic-contract-gap",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Classify(tt.diag); got != tt.want {
				t.Fatalf("Classify() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClassifyPrivateCorpusProductShapedDiagnostics(t *testing.T) {
	tests := []struct {
		name string
		diag ClassifiedDiagnostic
		want string
	}{
		{
			name: "string literal method return mismatch stays semantic",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA019", Message: `method "hashCode" has invalid return: returns String from Integer method`},
			want: "semantic-contract-gap",
		},
		{
			name: "string split assignment stays semantic",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA018", Message: `method "run" initializes List<String> local "parts" with String`},
			want: "semantic-contract-gap",
		},
		{
			name: "static field fluent string call stays semantic",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA008", Message: `method "run" calls unknown method "FieldNames.State.toLowerCase"`},
			want: "semantic-contract-gap",
		},
		{
			name: "builder overload miss stays semantic",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA009", Message: `method "run" has no matching overload for call "Q.condition" with 1 argument(s)`},
			want: "semantic-contract-gap",
		},
		{
			name: "parameter local name stays semantic",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA009", Message: `method "parse" has no matching overload for call "IProviderParameter.parseList" with 1 argument(s)`},
			want: "semantic-contract-gap",
		},
		{
			name: "current parameters variable stays semantic",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA018", Message: `method "process" initializes List<Object> local "currentParameters" with Object`},
			want: "semantic-contract-gap",
		},
		{
			name: "generated variable name stays semantic",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA018", Message: `method "handleEvent" initializes List<znu.OrderLine> local "generatedLines" with znu.OrderLine`},
			want: "semantic-contract-gap",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Classify(tt.diag); got != tt.want {
				t.Fatalf("Classify() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClassifyPrivateCorpusMetadataDiagnostics(t *testing.T) {
	tests := []struct {
		name string
		diag ClassifiedDiagnostic
		want string
	}{
		{
			name: "unknown custom object type is metadata",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA002", Message: `field "record" references unknown type "Package__Order__c"`},
			want: "project-metadata-missing",
		},
		{
			name: "unknown namespaced package type is metadata until source is present",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA004", Message: `method "run" parameter "line" references unknown type "pkg.OrderLine"`},
			want: "project-metadata-missing",
		},
		{
			name: "unknown arbitrary namespaced package type is metadata until source is present",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA004", Message: `method "run" parameter "line" references unknown type "namz.OrderLine"`},
			want: "project-metadata-missing",
		},
		{
			name: "unknown fflib package source type is metadata until source is present",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA002", Message: `method "run" references unknown type "fflib_QueryFactory"`},
			want: "project-metadata-missing",
		},
		{
			name: "unknown dependency inner enum is metadata until source is present",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA004", Message: `constructor "ApplicationSObjectSelector" parameter "dataAccess" references unknown type "DataAccess"`},
			want: "project-metadata-missing",
		},
		{
			name: "unknown dependency namespace type is metadata until source is present",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA006", Message: `method "configure" constructs unknown type "di_Module"`},
			want: "project-metadata-missing",
		},
		{
			name: "unknown dependency nested exception is metadata until source is present",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA006", Message: `method "configure" constructs unknown type "ModuleException"`},
			want: "project-metadata-missing",
		},
		{
			name: "missing fflib enum expression is metadata until source is present",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA013", Message: `method "testConstructors" reads unknown variable "fflib_SObjectSelector.DataAccess.LEGACY"`},
			want: "project-metadata-missing",
		},
		{
			name: "interface cascade from missing fflib type is metadata until source is present",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA017", Message: `concrete class "TestAccountsSelector" must implement interface method "configureQueryFactoryFields" from "IApplicationSObjectSelector"`},
			want: "project-metadata-missing",
		},
		{
			name: "unknown mock helper is metadata until source is present",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA006", Message: `method "test" constructs unknown type "MockHttpResponseGenerator"`},
			want: "project-metadata-missing",
		},
		{
			name: "unknown method on custom field path is metadata",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA008", Message: `method "generateDescriptionText" calls unknown method "agreement.namz__StartDate__c.format"`},
			want: "project-metadata-missing",
		},
		{
			name: "unknown custom object expression type is metadata",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA006", Message: `method "create" references unknown expression type "NU__Product__c"`},
			want: "project-metadata-missing",
		},
		{
			name: "unknown namespaced expression type is metadata",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA006", Message: `method "setAgreementFieldsFromCartLine" references unknown expression type "namz.OrderLineAgreement"`},
			want: "project-metadata-missing",
		},
		{
			name: "unknown private expression type is metadata until source is present",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA006", Message: `method "fetchAccountDto" references unknown expression type "AccountBase"`},
			want: "project-metadata-missing",
		},
		{
			name: "unknown namespaced super constructor target is metadata",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA011", Message: `constructor "ProgramAgreementSetter" has invalid super(...) call: unknown constructor target "namz.AgreementSetter"`},
			want: "project-metadata-missing",
		},
		{
			name: "unknown relationship path is metadata",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA_QUERY_RELATIONSHIP", Message: `SOQL query references unknown relationship path "Parent__r.Name" on Child__c`},
			want: "project-metadata-missing",
		},
		{
			name: "private package DTO type is metadata until source is present",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA006", Message: `method "buildDto" declares enhanced-for local "product" with unknown type "Product"`},
			want: "project-metadata-missing",
		},
		{
			name: "private znu assignment is metadata until package source is present",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA018", Message: `method "storePayment" assigns detailsToStore with znu.CreditCardDetail`},
			want: "project-metadata-missing",
		},
		{
			name: "private relationship metadata mismatch is metadata",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA024", Message: `method "buildOfferings" enhanced-for assigns znu__ParentProduct__c elements to znu__ProductLink__c variable "parentProductLink"`},
			want: "project-metadata-missing",
		},
		{
			name: "unknown custom query object is metadata",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA_QUERY_OBJECT", Message: `SOQL query references unknown SObject "ASR_Survey_Log__c"`},
			want: "project-metadata-missing",
		},
		{
			name: "duplicate symbol is project discovery duplicate",
			diag: ClassifiedDiagnostic{Code: "GLADETYPE001", Message: `duplicate top-level symbol "DuplicateType"; first seen in /repo/first.cls`},
			want: "project-discovery-duplicate",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Classify(tt.diag); got != tt.want {
				t.Fatalf("Classify() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClassifyMissingPackageSourceUsesDiagnosticMessageNotSourcePath(t *testing.T) {
	pathOnly := ClassifiedDiagnostic{
		Code:    "GLADESEMA008",
		File:    "force-app/main/default/classes/fflib_SObjectSelector.cls",
		Message: `method "select" calls unknown method "QueryFactory.execute"`,
	}
	if got := Classify(pathOnly); got != "semantic-contract-gap" {
		t.Fatalf("Classify(path-only fflib marker) = %q, want semantic-contract-gap", got)
	}

	projectOnly := ClassifiedDiagnostic{
		Code:    "GLADESEMA008",
		Project: "fflib-apex-mocks",
		Message: `method "select" calls unknown method "QueryFactory.execute"`,
	}
	if got := Classify(projectOnly); got != "semantic-contract-gap" {
		t.Fatalf("Classify(project-only fflib marker) = %q, want semantic-contract-gap", got)
	}

	messageMarker := ClassifiedDiagnostic{
		Code:    "GLADESEMA002",
		File:    "force-app/main/default/classes/Selector.cls",
		Message: `method "select" references unknown type "fflib_QueryFactory"`,
	}
	if got := Classify(messageMarker); got != "project-metadata-missing" {
		t.Fatalf("Classify(message fflib marker) = %q, want project-metadata-missing", got)
	}
}

func TestClassifyMetadataIdentifierDoesNotImplyMissingProjectMetadata(t *testing.T) {
	for name, diag := range map[string]ClassifiedDiagnostic{
		"method name": {
			Code:    "GLADESEMA030",
			Message: `method "getActionMetadata" has invalid statement: switch branch must be a literal or enum constant`,
		},
		"test name": {
			Code:    "GLADESEMA019",
			Message: `method "test_buildMetadataDeployContainer" has invalid expression: cast is incompatible with its operand`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			if got := Classify(diag); got != "semantic-contract-gap" {
				t.Fatalf("Classify() = %q, want semantic-contract-gap", got)
			}
		})
	}
}

func TestClassifyMissingMetadataServiceExamplesSource(t *testing.T) {
	diag := ClassifiedDiagnostic{
		Code:    "GLADESEMA008",
		Message: `method "execute" calls unknown method "MetadataServiceExamples.createService"`,
	}
	if got := Classify(diag); got != "project-metadata-missing" {
		t.Fatalf("Classify() = %q, want project-metadata-missing", got)
	}
}

func TestClassifyPublicCorpusProjectSourceInvalidDiagnostics(t *testing.T) {
	tests := []struct {
		name string
		diag ClassifiedDiagnostic
		want string
	}{
		{
			name: "map initialized from list return is project source invalid",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA018", Message: `method "getPicklistValuesByObjectFieldTest" initializes Map<String, String> local "sourceMap" with List<String>`},
			want: "project-source-invalid",
		},
		{
			name: "removed old helper method is project source invalid",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA008", Message: `method "getPicklistValuesByObjectFieldTestOld" calls unknown method "GovComponentHelper.getPicklistValuesByObjectFieldOld"`},
			want: "project-source-invalid",
		},
		{
			name: "fabricated test helper scalar initialization is project source invalid",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA018", Message: `method "createProductWithPriceComponents" initializes fabricatedPriceComponent with Decimal`},
			want: "project-source-invalid",
		},
		{
			name: "product test helper scalar initialization is project source invalid",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA018", Message: `method "availableTicketsWithConflictsConflictWithEachOtherWhenConflictsOverlap" initializes product1 with String`},
			want: "project-source-invalid",
		},
		{
			name: "missing IModel source return cascade is project source invalid",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA019", Message: `method "getModel" has invalid return: returns BatchSObjectWrapper from IModel method`},
			want: "project-source-invalid",
		},
		{
			name: "missing IModel list source return cascade is project source invalid",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA019", Message: `method "newListForType" has invalid return: returns List<BatchSObjectWrapper> from List<IModel> method`},
			want: "project-source-invalid",
		},
		{
			name: "missing query plugin collection cascade is project source invalid",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA023", Message: `method "getFilterPlugins" has invalid collection call "add" with 1 argument(s)`},
			want: "project-source-invalid",
		},
		{
			name: "static method through instance is project source invalid",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA027", Message: `method "run" has invalid static access for "selector.getRows": static method called through an instance`},
			want: "project-source-invalid",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Classify(tt.diag); got != tt.want {
				t.Fatalf("Classify() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCheckRejectsZeroDiscoveredProjects(t *testing.T) {
	root := t.TempDir()
	glade := filepath.Join(root, "fake-glade.sh")
	if err := os.WriteFile(glade, []byte("#!/bin/sh\nprintf '{}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := Check(context.Background(), Options{Root: root, Glade: glade, OutDir: filepath.Join(root, "out")})
	if err == nil {
		t.Fatal("expected error for zero discovered projects, got nil")
	}
}

func TestCheckRejectsMissingCandidateLaunchFailure(t *testing.T) {
	root := t.TempDir()
	writeProject(t, root, "alpha")
	missing := filepath.Join(root, "nonexistent", "glade")
	_, err := Check(context.Background(), Options{Root: root, Glade: missing, OutDir: filepath.Join(root, "out")})
	if err == nil {
		t.Fatal("expected error for missing candidate, got nil")
	}
}

func TestCheckRejectsNonzeroExitWithEmptyOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture uses sh")
	}
	root := t.TempDir()
	writeProject(t, root, "alpha")
	glade := filepath.Join(root, "fake-glade.sh")
	if err := os.WriteFile(glade, []byte("#!/bin/sh\nexit 7\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := Check(context.Background(), Options{Root: root, Glade: glade, OutDir: filepath.Join(root, "out")})
	if err == nil {
		t.Fatal("expected error for nonzero exit with empty output, got nil")
	}
}

func TestCheckRejectsContextTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture uses sh")
	}
	root := t.TempDir()
	writeProject(t, root, "alpha")
	glade := filepath.Join(root, "fake-glade.sh")
	if err := os.WriteFile(glade, []byte("#!/bin/sh\nsleep 10\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := Check(ctx, Options{Root: root, Glade: glade, OutDir: filepath.Join(root, "out")})
	if err == nil {
		t.Fatal("expected error for context timeout, got nil")
	}
}

func TestCheckValidProjectPreservesClassifiedDiagnostics(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture uses sh")
	}
	root := t.TempDir()
	writeProject(t, root, "alpha")
	glade := filepath.Join(root, "fake-glade.sh")
	if err := os.WriteFile(glade, []byte(`#!/bin/sh
printf '{"diagnostics":[{"code":"APEXPARSE001","message":"Unexpected token","file":"force-app/main/default/classes/A.cls","line":3,"column":4}]}'
`), 0o755); err != nil {
		t.Fatal(err)
	}
	report, err := Check(context.Background(), Options{Root: root, Glade: glade, OutDir: filepath.Join(root, "out")})
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.ProjectCount != 1 || report.Summary.DiagnosticCount != 1 || report.Counts["source-parse-error"] != 1 {
		t.Fatalf("report = %#v", report)
	}
}

func writeProject(t *testing.T, root, name string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sfdx-project.json"), []byte(`{"packageDirectories":[{"path":"force-app","default":true}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
}
