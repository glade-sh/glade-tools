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

func TestSalesforceReleaseCommandGeneratesAndChecks(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module github.com/glade-sh/glade\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	contract := fixturesPath("docs/fixtures/salesforce-release-contract.json")
	for _, test := range []struct{ flag, want string }{{"--write", "wrote"}, {"--check", "current"}} {
		var stdout, stderr strings.Builder
		code := Run(context.Background(), []string{"salesforce", "release", "--contract", contract, "--glade-root", root, test.flag}, &stdout, &stderr)
		if code != 0 || stderr.Len() != 0 || !strings.Contains(stdout.String(), test.want+": internal/apexversion/support_generated.go") {
			t.Fatalf("%s: code=%d stdout=%q stderr=%q", test.flag, code, stdout.String(), stderr.String())
		}
	}

	var stdout, stderr strings.Builder
	if code := Run(context.Background(), []string{"salesforce", "release", "--contract", contract, "--write"}, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "generation mode requires --glade-root") {
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
	var stdout, stderr strings.Builder
	code := Run(context.Background(), []string{"salesforce", "release", "--contract", fixturesPath("docs/fixtures/salesforce-release-contract.json"), "--json"}, &stdout, &stderr)
	var report map[string]any
	if err := json.Unmarshal([]byte(stdout.String()), &report); code != 1 || err != nil || report["status"] != "fail" {
		t.Fatalf("code=%d report=%v stderr=%q", code, report, stderr.String())
	}
}

func TestDocsInventorySummaryPrintsCanonicalDigest(t *testing.T) {
	root := t.TempDir()
	docPath := filepath.Join(root, "apex_class_System_String.md")
	if err := os.WriteFile(docPath, []byte("# String Class\n\n## Namespace\n[System](./apex_namespace_System.md)\n\n## String Methods\n### trim()\nRemoves whitespace.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	inv, err := apexdocs.BuildInventory(root)
	if err != nil {
		t.Fatal(err)
	}

	var stdout, stderr strings.Builder
	code := Run(context.Background(), []string{"docs-inventory", "--source", root}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run returned %d, stderr=%q", code, stderr.String())
	}
	want := []string{
		"schemaVersion: 1",
		"documents: 1",
		"members: 1",
		"namespaces: 1",
		"digest: " + apexdocs.CanonicalDigest(inv),
	}
	for _, line := range want {
		if !strings.Contains(stdout.String(), line) {
			t.Fatalf("summary missing %q:\n%s", line, stdout.String())
		}
	}
}
