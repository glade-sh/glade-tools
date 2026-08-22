package toolcli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/apexdocs"
)

func TestSalesforceReleaseCommandRejectsUnavailableGeneration(t *testing.T) {
	for _, flag := range []string{"--write", "--check"} {
		var stdout, stderr strings.Builder
		code := Run(context.Background(), []string{"salesforce", "release", "--contract", "contract.json", "--glade-root", ".", flag}, &stdout, &stderr)
		if code != 1 || stderr.String() != "glade-tools: release generation is not available until the generator is added\n" {
			t.Fatalf("%s: code=%d stderr=%q", flag, code, stderr.String())
		}
	}
	var stdout, stderr strings.Builder
	if code := Run(context.Background(), []string{"salesforce", "release", "--contract", "contract.json", "--write"}, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "generation mode requires --glade-root") {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
}

func TestSalesforceReleaseCommandRejectsInvalidModes(t *testing.T) {
	var stdout, stderr strings.Builder
	code := Run(context.Background(), []string{"salesforce", "release", "--contract", "contract.json", "--write", "--check", "--glade-root", "."}, &stdout, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "cannot combine --write and --check") {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	for _, args := range [][]string{{"salesforce", "release", "--contract", "contract.json", "--json", "--write", "--glade-root", "."}, {"salesforce", "release", "--contract", "contract.json", "--json", "--json"}} {
		stdout.Reset()
		stderr.Reset()
		code = Run(context.Background(), args, &stdout, &stderr)
		if code != 1 || stderr.Len() == 0 {
			t.Fatalf("args=%v code=%d stderr=%q", args, code, stderr.String())
		}
	}
}

func TestSalesforceReleaseCommandRejectsMissingContractAndUnknownFlag(t *testing.T) {
	for _, args := range [][]string{{"salesforce", "release", "--json"}, {"salesforce", "release", "--contract", "x", "--json", "--wat"}} {
		var stdout, stderr strings.Builder
		if code := Run(context.Background(), args, &stdout, &stderr); code != 1 || stderr.Len() == 0 {
			t.Fatalf("args=%v code=%d stderr=%q", args, code, stderr.String())
		}
	}
}

func TestSalesforceReleaseCommandHelpDispatch(t *testing.T) {
	var stdout, stderr strings.Builder
	if code := Run(context.Background(), []string{"salesforce", "release", "--help"}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "salesforce release") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestSalesforceReleaseCommandJSONWritesFailingReport(t *testing.T) {
	root := t.TempDir()
	inv := apexdocs.Inventory{SchemaVersion: 1, Documents: []apexdocs.Document{{SourcePath: "apex/apex_class_System_Stable.md", Kind: "class", Namespace: "System", Name: "Stable"}}}
	write := func(name string, value any) string {
		path := filepath.Join(root, name)
		data, _ := json.Marshal(value)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	digest := apexdocs.CanonicalDigest(inv)
	write("inventory.json", inv)
	write("manifest.json", map[string]any{"schemaVersion": 1, "release": "Winter '26", "apiVersion": "65.0", "digest": digest, "acquisition": "test", "sourceFamilies": []string{"apex"}})
	contract := map[string]any{"schemaVersion": 1, "defaults": map[string]string{"source": "65.0", "endpoint": "65.0", "orgProfile": "default"}, "windows": map[string]any{"source": []any{map[string]any{"version": "65.0", "productTests": []string{"x_test.go:TestSource"}}}, "endpoint": []any{map[string]any{"version": "65.0", "productTests": []string{"x_test.go:TestEndpoint"}}}, "orgProfiles": []any{map[string]any{"name": "default", "productTests": []string{"x_test.go:TestOrg"}}}}, "releases": []any{map[string]any{"name": "Winter '26", "apiVersion": "65.0", "maturity": "ga", "manifest": "manifest.json", "inventory": "inventory.json"}}, "behaviors": []any{}, "noFallbackProductTests": []string{"x_test.go:TestNoFallback"}}
	contractPath := write("contract.json", contract)
	var stdout, stderr strings.Builder
	code := Run(context.Background(), []string{"salesforce", "release", "--contract", contractPath, "--json"}, &stdout, &stderr)
	var report map[string]any
	if err := json.Unmarshal([]byte(stdout.String()), &report); code != 1 || err != nil || report["status"] != "fail" {
		t.Fatalf("code=%d report=%v stderr=%q", code, report, stderr.String())
	}
}
