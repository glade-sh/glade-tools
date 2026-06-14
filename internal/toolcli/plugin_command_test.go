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
}

type pluginManifestFileCommand struct {
	Path    []string `json:"path"`
	Summary string   `json:"summary"`
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

	runtimeCommands := runtimeCommandSummaryByRoot(runtime.Commands)
	packagedCommands := packagedCommandSummaryByRoot(packaged.Commands)
	if len(packagedCommands) != len(runtimeCommands) {
		t.Fatalf("packaged commands = %v, runtime commands = %v", packagedCommands, runtimeCommands)
	}
	for command, runtimeSummary := range runtimeCommands {
		if packagedSummary, ok := packagedCommands[command]; !ok || packagedSummary != runtimeSummary {
			t.Fatalf("packaged manifest command %q = %q, want %q", command, packagedSummary, runtimeSummary)
		}
	}
}

func runtimeCommandSummaryByRoot(commands []pluginCommandManifest) map[string]string {
	result := make(map[string]string, len(commands))
	for _, command := range commands {
		if len(command.Path) != 1 {
			continue
		}
		result[command.Path[0]] = command.Summary
	}
	return result
}

func packagedCommandSummaryByRoot(commands []pluginManifestFileCommand) map[string]string {
	result := make(map[string]string, len(commands))
	for _, command := range commands {
		if len(command.Path) != 1 {
			continue
		}
		result[command.Path[0]] = command.Summary
	}
	return result
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
