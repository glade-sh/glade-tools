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
		"compat":              true,
		"surface":             true,
		"matrix":              true,
		"mvp":                 true,
		"local-tests":         true,
		"post-parity":         true,
		"examples":            true,
		"replay":              true,
		"ui-controllers":      true,
		"server-examples":     true,
		"visualforce":         true,
		"dashboard":           true,
		"gaps":                true,
		"stdlib":              true,
		"docs-inventory":      true,
		"catalog":             true,
		"reconcile":           true,
		"doc-contracts":       true,
		"salesforce-coverage": true,
		"standard-objects":    true,
		"stub-contracts":      true,
		"stub-behavior":       true,
		"stub-inventory":      true,
		"product-namespaces":  true,
		"tooling-fixtures":    true,
		"evidence":            true,
		"oracle-stdlib":       true,
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
			Title:    "Capture LWC Org Evidence",
			View:     "preview",
			Contexts: []string{"project", "lwcServerRunning"},
			Command:  []string{"compat", "lwc", "capture"},
			Inputs:   []pluginManifestFileEditorInput{{Name: "targetOrg", Label: "Target org alias", Type: "text", Required: true}},
			Args:     []string{"--target-org", "${input.targetOrg}", "--project", "${projectRoot}", "--out", "${outputDir}/lwc-org-capture.json", "--json", "--editor-findings"},
			Output:   "glade.findings.v1",
			Icon:     "cloud-download",
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

func TestRunCompatLwcCaptureDoesNotPrintPreparedTextWhenReportWriteFails(t *testing.T) {
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

func TestPluginArchiveIndexCompatCommandsIncludeLwcRoots(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "scripts", "build-plugin-archives.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	for _, command := range []string{`"lwc"`, `"visualforce"`, `"oracle-stdlib"`} {
		if !strings.Contains(script, command) {
			t.Fatalf("archive index command list omits %s", command)
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
	for _, command := range []string{
		"stub-inventory",
		"product-namespaces",
		"salesforce-coverage",
		"tooling-fixtures",
		"evidence",
		"oracle-stdlib",
		"visualforce",
		"lwc",
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
