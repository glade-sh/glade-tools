package toolcli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type pluginManifestFile struct {
	Commands []pluginManifestFileCommand `json:"commands"`
	Editor   pluginManifestFileEditor    `json:"editor"`
}

type pluginManifestFileCommand struct {
	Path    []string `json:"path"`
	Summary string   `json:"summary"`
}

type pluginManifestFileEditor struct {
	Actions []pluginManifestFileEditorAction `json:"actions"`
}

type pluginManifestFileEditorAction struct {
	ID       string                          `json:"id"`
	Title    string                          `json:"title"`
	View     string                          `json:"view"`
	Contexts []string                        `json:"contexts"`
	Command  []string                        `json:"command"`
	Args     []string                        `json:"args"`
	Inputs   []pluginManifestFileEditorInput `json:"inputs,omitempty"`
	Output   string                          `json:"output"`
	Icon     string                          `json:"icon"`
}

type pluginManifestFileEditorInput struct {
	Name     string `json:"name"`
	Label    string `json:"label"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
}

func TestManifestJSONListsCompatCommands(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"manifest", "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run returned %d, stderr=%s", code, stderr.String())
	}

	var manifest struct {
		APIVersion string `json:"apiVersion"`
		Name       string `json:"name"`
		Version    string `json:"version"`
		Commands   []struct {
			Path    []string `json:"path"`
			Summary string   `json:"summary"`
		} `json:"commands"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &manifest); err != nil {
		t.Fatalf("manifest is not JSON: %v\n%s", err, stdout.String())
	}
	if manifest.APIVersion != "glade.plugin.v1" || manifest.Name != "compat" || manifest.Version == "" {
		t.Fatalf("unexpected manifest identity: %#v", manifest)
	}

	want := map[string]bool{
		"compat":                true,
		"surface":               true,
		"corpus":                true,
		"matrix":                true,
		"mvp":                   true,
		"local-tests":           true,
		"post-parity":           true,
		"examples":              true,
		"replay":                true,
		"ui-controllers":        true,
		"server-examples":       true,
		"visualforce":           true,
		"dashboard":             true,
		"gaps":                  true,
		"stdlib":                true,
		"docs-inventory":        true,
		"catalog":               true,
		"reconcile":             true,
		"doc-contracts":         true,
		"declaration-contracts": true,
		"salesforce-coverage":   true,
		"standard-objects":      true,
		"stub-contracts":        true,
		"stub-behavior":         true,
		"stub-inventory":        true,
		"product-namespaces":    true,
		"tooling-fixtures":      true,
		"evidence":              true,
		"oracle-stdlib":         true,
	}
	for _, command := range manifest.Commands {
		if len(command.Path) == 1 {
			delete(want, command.Path[0])
		}
	}
	if len(want) != 0 {
		t.Fatalf("manifest missing command roots: %#v", want)
	}
}

func TestManifestJSONListsCompatEditorActions(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"manifest", "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run returned %d, stderr=%s", code, stderr.String())
	}
	var manifest pluginManifestFile
	if err := json.Unmarshal(stdout.Bytes(), &manifest); err != nil {
		t.Fatalf("manifest is not JSON: %v\n%s", err, stdout.String())
	}

	actions := editorActionByID(manifest.Editor.Actions)
	want := map[string]pluginManifestFileEditorAction{
		"compat.postParity": {
			ID:       "compat.postParity",
			Title:    "Scan Unsupported Local Surfaces",
			View:     "runs",
			Contexts: []string{"project"},
			Command:  []string{"post-parity"},
			Args:     []string{"--project", "${projectRoot}", "--json", "--editor-findings"},
			Output:   "glade.findings.v1",
			Icon:     "search",
		},
		"compat.visualforceLocalCapture": {
			ID:       "compat.visualforceLocalCapture",
			Title:    "Capture Local Visualforce Evidence",
			View:     "preview",
			Contexts: []string{"project", "vfServerRunning"},
			Command:  []string{"visualforce", "capture"},
			Args:     []string{"--local", "--glade-bin", "glade", "--project", "${projectRoot}", "--out", "${outputDir}/visualforce-local.json", "--json", "--editor-findings"},
			Output:   "glade.findings.v1",
			Icon:     "record",
		},
		"compat.lwcCapture": {
			ID:       "compat.lwcCapture",
			Title:    "Capture LWC Browser Evidence",
			View:     "preview",
			Contexts: []string{"project", "lwcServerRunning"},
			Command:  []string{"compat", "lwc", "capture"},
			Inputs: []pluginManifestFileEditorInput{
				{Name: "targetOrg", Label: "Target org alias", Type: "text", Required: true},
				{Name: "localBaseUrl", Label: "Local LWC shell URL", Type: "text", Required: true},
			},
			Args:   []string{"--target-org", "${input.targetOrg}", "--project", "${projectRoot}", "--local-browser-capture", "--local-base-url", "${input.localBaseUrl}", "--browser-capture", "--out", "${outputDir}/lwc-browser-capture.json", "--json", "--editor-findings"},
			Output: "glade.findings.v1",
			Icon:   "cloud-download",
		},
	}
	if len(actions) != len(want) {
		t.Fatalf("editor actions = %#v, want %#v", actions, want)
	}
	for id, wantAction := range want {
		if got, ok := actions[id]; !ok || !editorActionsEqual(got, wantAction) {
			t.Fatalf("editor action %s = %#v, want %#v", id, got, wantAction)
		}
	}
}

func TestEvidenceRequireGatedFailsOnUngatedUnsupportedRows(t *testing.T) {
	dir := t.TempDir()
	catalogPath := filepath.Join(dir, "catalog.json")
	fixturePath := filepath.Join(dir, "fixture.json")
	if err := os.WriteFile(catalogPath, []byte(`{
  "schemaVersion": 1,
  "entries": [
    {"symbol":"String.trim","area":"Core stdlib","target":"executable-parity","status":"supported"},
    {"symbol":"Answers.findSimilar","area":"Core stdlib","target":"unsupported","status":"unsupported"}
  ]
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixturePath, []byte(`{
  "name": "string-trim",
  "source": [{"path": "force-app/main/classes/StringTrim.cls", "content": "public class StringTrim {}"}],
  "command": {"kind": "exec", "apex": "String s = ' x '.trim();"},
  "evidence": [{"symbol": "String.trim", "kind": "exec"}]
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"evidence", "--catalog", catalogPath, "--require-gated", fixturePath}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("Run returned 0, stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "ungated unsupported rows: 1") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestPackagedCompatManifestMatchesRuntimeCommands(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"manifest", "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run returned %d, stderr=%s", code, stderr.String())
	}
	var runtime pluginManifest
	if err := json.Unmarshal(stdout.Bytes(), &runtime); err != nil {
		t.Fatal(err)
	}

	manifestBytes, err := os.ReadFile(filepath.Join("..", "..", "plugins", "compat", "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	var packaged pluginManifestFile
	if err := json.Unmarshal(manifestBytes, &packaged); err != nil {
		t.Fatal(err)
	}

	runtimeCommands := runtimeCommandSummaryByPath(runtime.Commands)
	packagedCommands := packagedCommandSummaryByPath(packaged.Commands)
	if len(packagedCommands) != len(runtimeCommands) {
		t.Fatalf("packaged commands = %v, runtime commands = %v", packagedCommands, runtimeCommands)
	}
	for command, runtimeSummary := range runtimeCommands {
		if packagedSummary, ok := packagedCommands[command]; !ok || packagedSummary != runtimeSummary {
			t.Fatalf("packaged manifest command %q = %q, want %q", command, packagedSummary, runtimeSummary)
		}
	}

	if runtime.Editor == nil {
		t.Fatal("runtime manifest missing editor actions")
	}
	runtimeActions := runtimeEditorActionByID(runtime.Editor.Actions)
	packagedActions := editorActionByID(packaged.Editor.Actions)
	if len(packagedActions) != len(runtimeActions) {
		t.Fatalf("packaged actions = %#v, runtime actions = %#v", packagedActions, runtimeActions)
	}
	for id, runtimeAction := range runtimeActions {
		if packagedAction, ok := packagedActions[id]; !ok || !editorActionsEqual(packagedAction, runtimeAction) {
			t.Fatalf("packaged editor action %q = %#v, want %#v", id, packagedAction, runtimeAction)
		}
	}
}

func TestRunCompatLWCCaptureDoesNotPrintPreparedTextWhenReportWriteFails(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{
		"lwc", "capture",
		"--target-org", "dummy",
		"--project", ".",
		"--targets", "direct-component",
		"--skip-deploy",
		"--out", t.TempDir(),
	}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected output write failure, stdout=%s", stdout.String())
	}
	if strings.Contains(stdout.String(), "prepared") || strings.Contains(stdout.String(), "artifacts=") {
		t.Fatalf("stdout claimed prepared output after write failure: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "is a directory") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestCompatPluginManifestIncludesLWCAndMaintainerCommands(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "plugins", "compat", "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest pluginManifestFile
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("decode compat plugin manifest: %v", err)
	}
	paths := make(map[string]bool, len(manifest.Commands))
	for _, command := range manifest.Commands {
		paths[strings.Join(command.Path, " ")] = true
	}
	for _, command := range []string{"compat lwc", "corpus", "visualforce", "oracle-stdlib", "declaration-contracts"} {
		if !paths[command] {
			t.Fatalf("compat plugin manifest omits %q", command)
		}
	}
}

func runtimeCommandSummaryByPath(commands []pluginCommandManifest) map[string]string {
	result := make(map[string]string, len(commands))
	for _, command := range commands {
		path := strings.Join(command.Path, " ")
		if path == "" {
			continue
		}
		result[path] = command.Summary
	}
	return result
}

func packagedCommandSummaryByPath(commands []pluginManifestFileCommand) map[string]string {
	result := make(map[string]string, len(commands))
	for _, command := range commands {
		path := strings.Join(command.Path, " ")
		if path == "" {
			continue
		}
		result[path] = command.Summary
	}
	return result
}

func editorActionByID(actions []pluginManifestFileEditorAction) map[string]pluginManifestFileEditorAction {
	out := make(map[string]pluginManifestFileEditorAction, len(actions))
	for _, action := range actions {
		out[action.ID] = action
	}
	return out
}

func runtimeEditorActionByID(actions []pluginEditorActionManifest) map[string]pluginManifestFileEditorAction {
	out := make(map[string]pluginManifestFileEditorAction, len(actions))
	for _, action := range actions {
		out[action.ID] = pluginManifestFileEditorAction{
			ID:       action.ID,
			Title:    action.Title,
			View:     action.View,
			Contexts: action.Contexts,
			Command:  action.Command,
			Args:     action.Args,
			Inputs:   runtimeEditorInputs(action.Inputs),
			Output:   action.Output,
			Icon:     action.Icon,
		}
	}
	return out
}

func runtimeEditorInputs(inputs []pluginEditorInputManifest) []pluginManifestFileEditorInput {
	out := make([]pluginManifestFileEditorInput, 0, len(inputs))
	for _, input := range inputs {
		out = append(out, pluginManifestFileEditorInput{
			Name:     input.Name,
			Label:    input.Label,
			Type:     input.Type,
			Required: input.Required,
		})
	}
	return out
}

func editorActionsEqual(a, b pluginManifestFileEditorAction) bool {
	return a.ID == b.ID &&
		a.Title == b.Title &&
		a.View == b.View &&
		a.Output == b.Output &&
		a.Icon == b.Icon &&
		stringSliceEqual(a.Contexts, b.Contexts) &&
		stringSliceEqual(a.Command, b.Command) &&
		stringSliceEqual(a.Args, b.Args) &&
		editorInputsEqual(a.Inputs, b.Inputs)
}

func editorInputsEqual(a, b []pluginManifestFileEditorInput) bool {
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

func stringSliceEqual(a, b []string) bool {
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

func TestTopLevelHelpListsMaintenanceCommandRoots(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"--help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run returned %d, stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"Entrypoints:",
		"glade-tools <command> [flags]",
		"glade-plugin-compat <command> [flags]",
		"glade compat <command> [flags]",
		"Help:",
		"glade-tools <command> --help",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("help omitted %q:\n%s", want, out)
		}
	}
	for _, command := range []string{
		"stub-inventory",
		"product-namespaces",
		"salesforce-coverage",
		"tooling-fixtures",
		"evidence",
		"oracle-stdlib",
		"visualforce",
		"lwc",
		"declaration-contracts",
	} {
		if !strings.Contains(out, command) {
			t.Fatalf("help omitted %s:\n%s", command, out)
		}
	}
}

func TestOracleStdlibRequiresTargetOrg(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"oracle-stdlib"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected missing target org to fail, stdout=%s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "oracle-stdlib --target-org") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestManifestRequiresJSONFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"manifest"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected manifest without --json to fail, stdout=%s", stdout.String())
	}
	if stderr.Len() == 0 {
		t.Fatal("expected an error on stderr")
	}
}

func TestLocalTestsHelpListsMaintenanceAndPluginEntrypoints(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"local-tests", "--help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run returned %d, stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "glade-tools local-tests") {
		t.Fatalf("help omitted glade-tools entrypoint:\n%s", out)
	}
	if !strings.Contains(out, "glade compat local-tests") {
		t.Fatalf("help omitted plugin entrypoint:\n%s", out)
	}
}

func TestLocalTestsCompareHelpListsBothEntrypointsRequiredFlagsAndMeasurementRules(t *testing.T) {
	for _, args := range [][]string{
		{"local-tests", "compare", "--help"},
		{"compat", "local-tests", "compare", "--help"},
	} {
		var stdout, stderr bytes.Buffer
		code := Run(context.Background(), args, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("Run(%v) returned %d, stderr=%s", args, code, stderr.String())
		}
		out := stdout.String()
		for _, required := range []string{
			"glade-tools local-tests compare",
			"glade compat local-tests compare",
			"--base-bin", "--candidate-bin", "--project", "--out", "--workers", "--runs 5", "--manifest",
			"five cold alternating pairs", "profiles are excluded", "compat executable",
		} {
			if !strings.Contains(out, required) {
				t.Fatalf("compare help omitted %q:\n%s", required, out)
			}
		}
		if strings.Contains(out, "Glade or compat binary") {
			t.Fatalf("compare help promises unsupported product Glade binary mode:\n%s", out)
		}
	}
}

func TestLocalTestsCompareRejectsMissingUnknownDuplicateBlankAndDirectSelectorFlags(t *testing.T) {
	valid := []string{
		"local-tests", "compare",
		"--base-bin", "/generic/base",
		"--candidate-bin", "/generic/candidate",
		"--project", "/generic/project",
		"--out", "/generic/out",
		"--workers", "1",
		"--runs", "5",
		"--manifest", "/generic/targets.json",
	}
	for _, flag := range []string{"--base-bin", "--candidate-bin", "--project", "--out", "--workers", "--runs", "--manifest"} {
		t.Run("missing "+flag, func(t *testing.T) {
			args := removeLocalTestsCompareFlag(valid, flag)
			assertLocalTestsCompareCLIError(t, args, "required")
		})
		t.Run("blank "+flag, func(t *testing.T) {
			args := append([]string(nil), valid...)
			for index := range args {
				if args[index] == flag {
					args[index+1] = " "
					break
				}
			}
			assertLocalTestsCompareCLIError(t, args, "required")
		})
		t.Run("duplicate "+flag, func(t *testing.T) {
			args := append(append([]string(nil), valid...), flag, "duplicate")
			assertLocalTestsCompareCLIError(t, args, "duplicate")
		})
	}
	for _, tt := range []struct {
		name string
		args []string
		want string
	}{
		{name: "unknown", args: append(append([]string(nil), valid...), "--unknown"), want: "unknown"},
		{name: "duplicate json", args: append(append([]string(nil), valid...), "--json", "--json"), want: "duplicate"},
		{name: "class selector", args: append(append([]string(nil), valid...), "--class", "GenericTest"), want: "external manifest"},
		{name: "method selector", args: append(append([]string(nil), valid...), "--method", "runs"), want: "external manifest"},
		{name: "CPU profile selector", args: append(append([]string(nil), valid...), "--cpu-profile", "/generic/cpu.pprof"), want: "external manifest"},
		{name: "zero workers", args: replaceLocalTestsCompareFlag(valid, "--workers", "0"), want: "workers must be at least 1"},
		{name: "wrong run count", args: replaceLocalTestsCompareFlag(valid, "--runs", "4"), want: "runs must be exactly 5"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assertLocalTestsCompareCLIError(t, tt.args, tt.want)
		})
	}
}

func TestLocalTestsCompareWritesSummaryAndJSONMirrorsItExactly(t *testing.T) {
	args, out := newLocalTestsCompareCLIArgs(t)
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), append(args, "--json"), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run returned %d, stderr=%s", code, stderr.String())
	}
	summaryPath := filepath.Join(out, "summary.json")
	data, err := os.ReadFile(summaryPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stdout.Bytes(), data) {
		t.Fatalf("stdout does not mirror summary.json exactly:\nstdout=%s\nfile=%s", stdout.Bytes(), data)
	}
	var summary struct {
		SchemaVersion int    `json:"schemaVersion"`
		Status        string `json:"status"`
		Runs          int    `json:"runs"`
	}
	if err := json.Unmarshal(data, &summary); err != nil || summary.SchemaVersion != 1 || summary.Status != "matched" || summary.Runs != 5 {
		t.Fatalf("summary = %#v, %v\n%s", summary, err, data)
	}
	if info, err := os.Stat(out); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("output mode = %v, %v", info, err)
	}
	if info, err := os.Stat(summaryPath); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("summary mode = %v, %v", info, err)
	}

	textArgs, textOut := newLocalTestsCompareCLIArgs(t)
	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), textArgs, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("text Run returned %d, stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "matched") || !strings.Contains(stdout.String(), filepath.Join(textOut, "summary.json")) {
		t.Fatalf("text output omitted status/path: %q", stdout.String())
	}
}

func TestLocalTestsCompareDoesNotPrintOrAdvertiseStaleSummaryFromExistingOutput(t *testing.T) {
	for _, jsonOut := range []bool{false, true} {
		t.Run(map[bool]string{false: "text", true: "json"}[jsonOut], func(t *testing.T) {
			args, out := newLocalTestsCompareCLIArgs(t)
			if err := os.Mkdir(out, 0o700); err != nil {
				t.Fatal(err)
			}
			stale := []byte(`{"schemaVersion":1,"status":"matched","stale":true}` + "\n")
			if err := os.WriteFile(filepath.Join(out, "summary.json"), stale, 0o600); err != nil {
				t.Fatal(err)
			}
			if jsonOut {
				args = append(args, "--json")
			}
			var stdout, stderr bytes.Buffer
			code := Run(context.Background(), args, &stdout, &stderr)
			if code == 0 || !strings.Contains(stderr.String(), "must not exist") {
				t.Fatalf("Run code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			if stdout.Len() != 0 {
				t.Fatalf("rejected comparison emitted stale/advisory output: %q", stdout.String())
			}
			if data, err := os.ReadFile(filepath.Join(out, "summary.json")); err != nil || !bytes.Equal(data, stale) {
				t.Fatalf("stale summary changed: %q, %v", data, err)
			}
		})
	}
}

func TestLocalTestsCompareLeavesOrdinaryLocalTestsHelpAndValidationUnchanged(t *testing.T) {
	for _, prefix := range [][]string{{"local-tests"}, {"compat", "local-tests"}} {
		var stdout, stderr bytes.Buffer
		code := Run(context.Background(), append(append([]string(nil), prefix...), "--help"), &stdout, &stderr)
		if code != 0 || !strings.Contains(stdout.String(), "--class <name>") || !strings.Contains(stdout.String(), "--check <path>") {
			t.Fatalf("ordinary help changed for %v: code=%d stdout=%s stderr=%s", prefix, code, stdout.String(), stderr.String())
		}
		stdout.Reset()
		stderr.Reset()
		code = Run(context.Background(), append(append([]string(nil), prefix...), "--method", "runs"), &stdout, &stderr)
		if code == 0 || !strings.Contains(stderr.String(), "--method requires --class") {
			t.Fatalf("ordinary validation changed for %v: code=%d stderr=%s", prefix, code, stderr.String())
		}
	}
}

func assertLocalTestsCompareCLIError(t *testing.T, args []string, want string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), args, &stdout, &stderr)
	if code == 0 || !strings.Contains(stderr.String(), want) {
		t.Fatalf("Run(%v) code=%d stdout=%q stderr=%q, want error containing %q", args, code, stdout.String(), stderr.String(), want)
	}
}

func removeLocalTestsCompareFlag(args []string, flag string) []string {
	result := make([]string, 0, len(args)-2)
	for index := 0; index < len(args); index++ {
		if args[index] == flag {
			index++
			continue
		}
		result = append(result, args[index])
	}
	return result
}

func replaceLocalTestsCompareFlag(args []string, flag, value string) []string {
	result := append([]string(nil), args...)
	for index := range result {
		if result[index] == flag {
			result[index+1] = value
			return result
		}
	}
	return result
}

func newLocalTestsCompareCLIArgs(t *testing.T) ([]string, string) {
	t.Helper()
	directory := t.TempDir()
	project := filepath.Join(directory, "project")
	if err := os.Mkdir(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "project.txt"), []byte("generic project\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(directory, "targets.json")
	if err := os.WriteFile(manifest, []byte(`{"schemaVersion":1,"targets":[{"id":"generic","cpuProfile":false}]}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(directory, "comparison")
	return []string{
		"local-tests", "compare",
		"--base-bin", newLocalTestsCompareCLIExecutable(t),
		"--candidate-bin", newLocalTestsCompareCLIExecutable(t),
		"--project", project,
		"--out", out,
		"--workers", "1",
		"--runs", "5",
		"--manifest", manifest,
	}, out
}

func newLocalTestsCompareCLIExecutable(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "generic-compat")
	script := `#!/bin/sh
set -eu
if [ "${1:-}" = "manifest" ]; then
  printf '{"schemaVersion":1,"name":"generic-cli"}\n'
  exit 0
fi
[ "${1:-}" = "local-tests" ]
shift
project=""
perf=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --project) project=$2; shift 2 ;;
    --parallel) shift 2 ;;
    --parallel-methods|--json) shift ;;
    --perf-json) perf=$2; shift 2 ;;
    --class|--method) shift 2 ;;
    *) exit 42 ;;
  esac
done
printf '{"phases":[{"event":"start","totalAllocBytes":100},{"event":"end","totalAllocBytes":110}]}\n' > "$perf"
printf '{"target":"local Apex test execution readiness","ready":true,"project":"%s","casesDiscovered":1,"casesRun":1,"summary":{"total":1,"pass":1,"fail":0,"unsupported":0,"loadError":0,"compileError":0,"internalError":0},"outcomes":[{"class":"GenericTest","method":"runs","outcome":"pass","capabilityId":"%s/capability/pass"}]}\n' "$project" "$project"
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSalesforceCoverageRequiresSourceInput(t *testing.T) {
	t.Setenv("GLADE_SALESFORCE_DOCS_SOURCE", "")
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"salesforce-coverage", "--json"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected missing source to fail, stdout=%s", stdout.String())
	}
	errText := stderr.String()
	if !strings.Contains(errText, "GLADE_SALESFORCE_DOCS_SOURCE") {
		t.Fatalf("error omitted env fallback:\n%s", errText)
	}
	if strings.Contains(errText, "/Users/") {
		t.Fatalf("error leaked a private fallback path:\n%s", errText)
	}
}

func TestProductNamespacesRequiresSourceInput(t *testing.T) {
	t.Setenv("GLADE_SALESFORCE_DOCS_SOURCE", "")
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"product-namespaces", "--json"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected missing source to fail, stdout=%s", stdout.String())
	}
	errText := stderr.String()
	if !strings.Contains(errText, "GLADE_SALESFORCE_DOCS_SOURCE") {
		t.Fatalf("error omitted env fallback:\n%s", errText)
	}
	if strings.Contains(errText, "/Users/") {
		t.Fatalf("error leaked a private fallback path:\n%s", errText)
	}
}

func TestDeclarationContractsCommandWritesGeneratorJSON(t *testing.T) {
	dir := t.TempDir()
	inventoryPath := filepath.Join(dir, "inventory.json")
	outputPath := filepath.Join(dir, "contracts.json")
	if err := os.WriteFile(inventoryPath, []byte(`{
  "schemaVersion": 1,
  "documents": [
    {
      "sourcePath": "apex/apex_methods_system_string.md",
      "kind": "class",
      "namespace": "System",
      "name": "String",
      "members": [
        {
          "kind": "method",
          "name": "format",
          "signature": "public static String format(String stringToFormat, List<Object> formattingArguments)",
          "returnType": "String",
          "parameters": ["String", "List<Object>"]
        }
      ]
    }
  ]
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"declaration-contracts", "--inventory", inventoryPath, "--output", outputPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d stderr=%q stdout=%q", code, stderr.String(), stdout.String())
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"sourcePath": "apex/apex_methods_system_string.md"`) ||
		!strings.Contains(string(data), `"static": true`) {
		t.Fatalf("contracts json = %s", string(data))
	}
}

func TestCorpusCheckCommandWritesReports(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "alpha")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "sfdx-project.json"), []byte(`{"packageDirectories":[{"path":"force-app","default":true}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	glade := filepath.Join(root, "fake-glade.sh")
	if err := os.WriteFile(glade, []byte(`#!/bin/sh
printf '{"diagnostics":[{"code":"GLADEPERF001","message":"slow check","file":"A.cls"}]}'
`), 0o755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, "out")
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"corpus", "check", "--root", root, "--glade", glade, "--out", out}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d stderr=%q stdout=%q", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "corpus check: projects=1 diagnostics=1 unclassified=0 closure_blocking=0") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(out, "classified.tsv")); err != nil {
		t.Fatalf("classified.tsv missing: %v", err)
	}
}

func TestCorpusCheckCommandSimulatesSourceAPIUpgrade(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "alpha")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "sfdx-project.json"), []byte(`{"packageDirectories":[{"path":"force-app","default":true}],"sourceApiVersion":"64.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	glade := filepath.Join(root, "fake-glade.sh")
	if err := os.WriteFile(glade, []byte(`#!/bin/sh
grep -q '"sourceApiVersion": "65.0"' "$3/sfdx-project.json" || exit 9
printf '{"diagnostics":[]}'
`), 0o755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, "out")
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"corpus", "check", "--root", root, "--glade", glade, "--out", out, "--simulate-source-api-version", "65.0"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d stderr=%q stdout=%q", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "source_api_upgrade_target=65.0") {
		t.Fatalf("stdout omitted upgrade target: %q", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(out, "upgrade-simulation.json")); err != nil {
		t.Fatalf("upgrade-simulation.json missing: %v", err)
	}
}

func TestCorpusCheckCommandPrintsCountsBeforeClosureFailure(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "alpha")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "sfdx-project.json"), []byte(`{"packageDirectories":[{"path":"force-app","default":true}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	glade := filepath.Join(root, "fake-glade.sh")
	if err := os.WriteFile(glade, []byte(`#!/bin/sh
printf '{"diagnostics":[{"code":"GLADESEMA009","message":"No overload matches call","file":"A.cls"}]}'
`), 0o755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, "out")
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"corpus", "check", "--root", root, "--glade", glade, "--out", out, "--fail-on-check-closure"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected closure failure, stdout=%q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "corpus check: projects=1 diagnostics=1 unclassified=0 closure_blocking=1") {
		t.Fatalf("stdout missing counts: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "public check closure failed") {
		t.Fatalf("stderr missing closure error: %q", stderr.String())
	}
}

func TestUnknownCommandUsageLeadsWithMaintenanceEntrypoint(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"nope"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected unknown command to fail, stdout=%s", stdout.String())
	}
	errText := stderr.String()
	if !strings.Contains(errText, "usage: glade-tools") {
		t.Fatalf("usage omitted glade-tools entrypoint:\n%s", errText)
	}
	if !strings.Contains(errText, "glade compat") {
		t.Fatalf("usage omitted plugin entrypoint:\n%s", errText)
	}
}

func TestStubInventoryDriftHintIncludesSource(t *testing.T) {
	source := t.TempDir()
	if err := os.MkdirAll(filepath.Join(source, "apex-system-stubs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(source, "apex-sobject-stubs"), 0o755); err != nil {
		t.Fatal(err)
	}
	checkPath := filepath.Join(t.TempDir(), "STUB_INVENTORY.md")
	if err := os.WriteFile(checkPath, []byte("stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"stub-inventory", "--source", source, "--check", checkPath}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected drift check to fail, stdout=%s", stdout.String())
	}
	want := "glade-tools stub-inventory --source " + source + " --output " + checkPath
	if !strings.Contains(stderr.String(), want) {
		t.Fatalf("stderr missing rerun hint %q:\n%s", want, stderr.String())
	}
}
