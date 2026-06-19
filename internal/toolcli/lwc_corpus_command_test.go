package toolcli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/lwcparity"
)

func TestRunCompatLWCCorpusWritesJSONToStdoutAndOut(t *testing.T) {
	root := t.TempDir()
	writeLWCCorpusCommandTestFile(t, filepath.Join(root, "repo-a", "sfdx-project.json"), `{"packageDirectories":[{"path":"force-app","package":"Main"}]}`)
	writeLWCCorpusCommandTestFile(t, filepath.Join(root, "repo-a", "force-app", "main", "default", "lwc", "statusPanel", "statusPanel.js"), `import { LightningElement } from 'lwc';`)
	writeLWCCorpusCommandTestFile(t, filepath.Join(root, "repo-a", "force-app", "main", "default", "lwc", "statusPanel", "statusPanel.html"), `<template><lightning-badge label="Ready"></lightning-badge></template>`)
	writeLWCCorpusCommandTestFile(t, filepath.Join(root, "repo-a", "force-app", "main", "default", "lwc", "statusPanel", "statusPanel.js-meta.xml"), `<LightningComponentBundle><targets><target>lightning__HomePage</target></targets></LightningComponentBundle>`)
	outPath := filepath.Join(root, "reports", "lwc-corpus.json")

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{
		"compat", "lwc", "corpus",
		"--root", root,
		"--out", outPath,
		"--include-repos", "repo-a,repo-empty",
		"--json",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run returned %d, stderr=%s", code, stderr.String())
	}
	var payload struct {
		Command string `json:"command"`
		Counts  struct {
			Meta    int `json:"meta"`
			JS      int `json:"js"`
			HTML    int `json:"html"`
			Bundles int `json:"bundles"`
		} `json:"counts"`
		Repositories []struct {
			Name   string `json:"name"`
			Path   string `json:"path"`
			Counts struct {
				Bundles int `json:"bundles"`
			} `json:"counts"`
		} `json:"repositories"`
		Packages []struct {
			Repository string `json:"repository"`
			Name       string `json:"name"`
			Counts     struct {
				Bundles int `json:"bundles"`
			} `json:"counts"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout.String())
	}
	if payload.Command != "glade compat lwc corpus" || payload.Counts.Meta != 1 || payload.Counts.JS != 1 || payload.Counts.HTML != 1 || payload.Counts.Bundles != 1 {
		t.Fatalf("payload = %#v", payload)
	}
	if len(payload.Repositories) != 2 || payload.Repositories[0].Path != "repo-a" || payload.Repositories[1].Name != "repo-empty" || payload.Repositories[1].Counts.Bundles != 0 {
		t.Fatalf("repositories = %#v", payload.Repositories)
	}
	if len(payload.Packages) != 1 || payload.Packages[0].Repository != "repo-a" || payload.Packages[0].Name != "Main" || payload.Packages[0].Counts.Bundles != 1 {
		t.Fatalf("packages = %#v", payload.Packages)
	}
	outData, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.TrimSpace(stdout.Bytes()), bytes.TrimSpace(outData)) {
		t.Fatalf("out file differs from stdout\nstdout=%s\nout=%s", stdout.String(), string(outData))
	}
}

func TestRunCompatLWCCorpusWritesJSONToStdoutWithoutOut(t *testing.T) {
	root := t.TempDir()
	writeLWCCorpusCommandTestFile(t, filepath.Join(root, "repo-a", "force-app", "main", "default", "lwc", "statusPanel", "statusPanel.js"), `import { LightningElement } from 'lwc';`)

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{
		"compat", "lwc", "corpus",
		"--root", root,
		"--include-repos", "repo-a",
		"--json",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run returned %d, stderr=%s", code, stderr.String())
	}
	var payload struct {
		Counts struct {
			JS int `json:"js"`
		} `json:"counts"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout.String())
	}
	if payload.Counts.JS != 1 {
		t.Fatalf("payload = %#v", payload)
	}
	if strings.Contains(stdout.String(), "wrote ") {
		t.Fatalf("stdout included file write message: %s", stdout.String())
	}
}

func TestRunCompatLWCCorpusCheckFailsForUnsupportedTags(t *testing.T) {
	root := t.TempDir()
	writeLWCCorpusCommandTestFile(t, filepath.Join(root, "repo-a", "force-app", "main", "default", "lwc", "statusPanel", "statusPanel.html"), `<template><lightning-unknown-panel></lightning-unknown-panel></template>`)

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{
		"compat", "lwc", "corpus",
		"--root", root,
		"--include-repos", "repo-a",
		"--check",
	}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("Run returned success, stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "unsupported tags=1") {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestLWCHelpListsCorpusCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"lwc", "--help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run returned %d, stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"lwc corpus --root <path> [--out <path>] [--json] [--check]",
		"--include-repos <a,b>",
		"--check",
		"LWC corpus report",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("help omitted %q:\n%s", want, out)
		}
	}
}

func TestRunCompatLWCParityWritesReportAndChecksIt(t *testing.T) {
	root := t.TempDir()
	writeLWCCorpusCommandTestFile(t, filepath.Join(root, "reference-api-modules.md"), `
| API Module Name | Provides | First Available in Salesforce API Version |
| --- | --- | --- |
| [lightning/uiRecordApi](reference-lightning-ui-api-record.md) | Wire adapters and functions for record data. | 45.0 |
`)
	writeLWCCorpusCommandTestFile(t, filepath.Join(root, "reference-salesforce-modules.md"), "`@salesforce/site/activeLanguages`\n")
	writeLWCCorpusCommandTestFile(t, filepath.Join(root, "reference-page-reference-type.md"), "These page reference types are supported.\n\n- Record Page\n")
	outPath := filepath.Join(root, "LWC_NATIVE_API_PARITY.md")

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"compat", "lwc", "parity", "--docs", root, "--output", outPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run returned %d, stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "wrote "+outPath) {
		t.Fatalf("stdout = %s", stdout.String())
	}
	report, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(report), "`lightning/uiRecordApi`") || !strings.Contains(string(report), "`@salesforce/site/activeLanguages`") {
		t.Fatalf("report = %s", string(report))
	}

	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"lwc", "parity", "--docs", root, "--check", outPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run returned %d, stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "up to date") {
		t.Fatalf("stdout = %s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"lwc", "parity", "--docs", root, "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run returned %d, stderr=%s", code, stderr.String())
	}
	var payload struct {
		SchemaVersion int `json:"schemaVersion"`
		Summary       struct {
			ByStatus map[string]int `json:"byStatus"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout.String())
	}
	if payload.SchemaVersion != 2 || payload.Summary.ByStatus["supported-local"] == 0 {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestRunCompatLWCParityRefreshDocsWritesJSONWhenInventoryGapsAllowed(t *testing.T) {
	root := t.TempDir()
	writeLWCCorpusCommandTestFile(t, filepath.Join(root, "reference-api-modules.md"), `
| API Module Name | Provides | First Available in Salesforce API Version |
| --- | --- | --- |
| [lightning/uiRecordApi](reference-lightning-ui-api-record.md) | Wire adapters and functions for record data. | 45.0 |
`)
	writeLWCCorpusCommandTestFile(t, filepath.Join(root, "reference-salesforce-modules.md"), "`@salesforce/site/activeLanguages`\n")
	writeLWCCorpusCommandTestFile(t, filepath.Join(root, "reference-page-reference-type.md"), "These page reference types are supported.\n\n- Record Page\n")
	outPath := filepath.Join(root, "LWC_NATIVE_API_PARITY.json")

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{
		"lwc", "parity", "refresh-docs",
		"--source", root,
		"--output", outPath,
		"--allow-inventory-gaps",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run returned %d, stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "wrote "+outPath) {
		t.Fatalf("stdout = %s", stdout.String())
	}
	var payload struct {
		SchemaVersion int `json:"schemaVersion"`
		Summary       struct {
			ByStatus map[string]int `json:"byStatus"`
		} `json:"summary"`
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, string(data))
	}
	if payload.SchemaVersion != 2 || payload.Summary.ByStatus["local-only"] == 0 {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestRunCompatLWCParityRefreshDocsFailsOnInventoryGapsByDefault(t *testing.T) {
	root := t.TempDir()
	writeLWCCorpusCommandTestFile(t, filepath.Join(root, "reference-api-modules.md"), `
| API Module Name | Provides | First Available in Salesforce API Version |
| --- | --- | --- |
| [lightning/uiRecordApi](reference-lightning-ui-api-record.md) | Wire adapters and functions for record data. | 45.0 |
`)
	writeLWCCorpusCommandTestFile(t, filepath.Join(root, "reference-salesforce-modules.md"), "`@salesforce/site/activeLanguages`\n")
	writeLWCCorpusCommandTestFile(t, filepath.Join(root, "reference-page-reference-type.md"), "These page reference types are supported.\n\n- Record Page\n")

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{
		"lwc", "parity", "refresh-docs",
		"--source", root,
		"--output", filepath.Join(root, "ledger.json"),
	}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("Run returned success, stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "inventory gaps") {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestRunCompatLWCParityFailsOnStatusGate(t *testing.T) {
	root := t.TempDir()
	writeLWCCorpusCommandTestFile(t, filepath.Join(root, "reference-api-modules.md"), `
| API Module Name | Provides | First Available in Salesforce API Version |
| --- | --- | --- |
| [lightning/uiRecordApi](reference-lightning-ui-api-record.md) | Wire adapters and functions for record data. | 45.0 |
`)
	writeLWCCorpusCommandTestFile(t, filepath.Join(root, "reference-salesforce-modules.md"), "`@salesforce/site/activeLanguages`\n")
	writeLWCCorpusCommandTestFile(t, filepath.Join(root, "reference-page-reference-type.md"), "These page reference types are supported.\n\n- Record Page\n")

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{
		"lwc", "parity",
		"--docs", root,
		"--fail-on", "local-only",
	}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("Run returned success, stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "LWC parity gate failed") || !strings.Contains(stderr.String(), "local-only=") {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestRunCompatLWCParityFailsWhenOracleRequired(t *testing.T) {
	root := t.TempDir()
	writeLWCCorpusCommandTestFile(t, filepath.Join(root, "reference-api-modules.md"), `
| API Module Name | Provides | First Available in Salesforce API Version |
| --- | --- | --- |
| [lightning/uiRecordApi](reference-lightning-ui-api-record.md) | Wire adapters and functions for record data. | 45.0 |
`)
	writeLWCCorpusCommandTestFile(t, filepath.Join(root, "reference-salesforce-modules.md"), "`@salesforce/site/activeLanguages`\n")
	writeLWCCorpusCommandTestFile(t, filepath.Join(root, "reference-page-reference-type.md"), "These page reference types are supported.\n\n- Record Page\n")

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{
		"lwc", "parity",
		"--docs", root,
		"--require-oracle",
	}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("Run returned success, stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "oracle-missing=") {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestRunCompatLWCParityLedgerSatisfiesOracleGate(t *testing.T) {
	root := t.TempDir()
	ledgerPath := filepath.Join(root, "ledger.json")
	checkPath := filepath.Join(root, "ledger.md")
	writeLWCCorpusCommandTestFile(t, ledgerPath, `{
  "schemaVersion": 2,
  "rows": [
    {
      "id": "api-module:lightning/uiRecordApi",
      "category": "api-module",
      "name": "lightning/uiRecordApi",
      "status": "supported-local",
      "oracleStatus": "pass",
      "oracleTest": "uiRecordApiOracle"
    }
  ],
  "summary": {
    "total": 1,
    "byCategory": {"api-module": 1},
    "byStatus": {"supported-local": 1}
  }
}`)
	var rendered bytes.Buffer
	var report lwcparity.Report
	data, err := os.ReadFile(ledgerPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	if err := lwcparity.WriteMarkdown(&rendered, report); err != nil {
		t.Fatal(err)
	}
	writeLWCCorpusCommandTestFile(t, checkPath, rendered.String())

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{
		"lwc", "parity",
		"--ledger", ledgerPath,
		"--require-oracle",
		"--check", checkPath,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run returned %d, stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "up to date") {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestRunCompatLWCParityReconcileOracleWritesJSON(t *testing.T) {
	root := t.TempDir()
	ledgerPath := filepath.Join(root, "ledger.json")
	capturePath := filepath.Join(root, "capture.json")
	outPath := filepath.Join(root, "reconciled.json")
	writeLWCCorpusCommandTestFile(t, ledgerPath, `{
  "schemaVersion": 2,
  "rows": [
    {
      "id": "api-module:lightning/uiRecordApi",
      "category": "api-module",
      "name": "lightning/uiRecordApi",
      "status": "supported-local",
      "oracleStatus": "not-probed"
    }
  ],
  "summary": {
    "total": 1,
    "byCategory": {"api-module": 1},
    "byStatus": {"supported-local": 1}
  }
}`)
	writeLWCCorpusCommandTestFile(t, capturePath, `{
  "capturedAt": "2026-06-19T12:34:56Z",
  "rows": [
    {
      "category": "api-module",
      "name": "lightning/uiRecordApi",
      "oracleTest": "uiRecordApiOracle",
      "oracleStatus": "pass"
    }
  ]
}`)

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{
		"lwc", "parity", "reconcile-oracle",
		"--ledger", ledgerPath,
		"--capture", capturePath,
		"--output", outPath,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run returned %d, stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "reconciled 1 oracle rows") {
		t.Fatalf("stdout = %s", stdout.String())
	}
	var payload struct {
		Rows []struct {
			OracleTest   string `json:"oracleTest"`
			OracleStatus string `json:"oracleStatus"`
			LastVerified string `json:"lastVerified"`
		} `json:"rows"`
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, string(data))
	}
	if len(payload.Rows) != 1 || payload.Rows[0].OracleTest != "uiRecordApiOracle" || payload.Rows[0].OracleStatus != "pass" || payload.Rows[0].LastVerified != "2026-06-19T12:34:56Z" {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestRunCompatLWCParityFixturesCheckDetectsDrift(t *testing.T) {
	root := t.TempDir()
	ledgerPath := filepath.Join(root, "ledger.json")
	outPath := filepath.Join(root, "fixtures")
	componentPath := filepath.Join(outPath, "force-app", "main", "default", "lwc", "uiAppsApiOracle", "uiAppsApiOracle.js")
	writeLWCCorpusCommandTestFile(t, ledgerPath, `{
  "schemaVersion": 2,
  "rows": [
    {
      "id": "api-module:lightning/uiAppsApi",
      "category": "api-module",
      "name": "lightning/uiAppsApi",
      "status": "supported-local",
      "oracleStatus": "not-probed"
    }
  ],
  "summary": {
    "total": 1,
    "byCategory": {"api-module": 1},
    "byStatus": {"supported-local": 1}
  }
}`)

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"lwc", "parity", "fixtures", "--ledger", ledgerPath, "--out", outPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run returned %d, stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"lwc", "parity", "fixtures", "--ledger", ledgerPath, "--out", outPath, "--check"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run returned %d, stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), outPath+": up to date") {
		t.Fatalf("stdout = %s", stdout.String())
	}
	if err := os.WriteFile(componentPath, []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"lwc", "parity", "fixtures", "--ledger", ledgerPath, "--out", outPath, "--check"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("Run returned success, stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "fixture drift") {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestRunCompatLWCParityFixturesWritesGeneratedProject(t *testing.T) {
	root := t.TempDir()
	ledgerPath := filepath.Join(root, "ledger.json")
	outPath := filepath.Join(root, "fixtures")
	writeLWCCorpusCommandTestFile(t, ledgerPath, `{
  "schemaVersion": 2,
  "rows": [
    {
      "id": "api-module:lightning/uiAppsApi",
      "category": "api-module",
      "name": "lightning/uiAppsApi",
      "status": "supported-local",
      "oracleStatus": "not-probed"
    },
    {
      "id": "page-reference:standard__flow",
      "category": "page-reference",
      "name": "standard__flow",
      "status": "supported-local",
      "oracleStatus": "not-probed"
    }
  ],
  "summary": {
    "total": 2,
    "byCategory": {"api-module": 1, "page-reference": 1},
    "byStatus": {"supported-local": 2}
  }
}`)

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"lwc", "parity", "fixtures", "--ledger", ledgerPath, "--out", outPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run returned %d, stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "wrote 2 LWC oracle fixtures") {
		t.Fatalf("stdout = %s", stdout.String())
	}
	for _, path := range []string{
		filepath.Join(outPath, "sfdx-project.json"),
		filepath.Join(outPath, "glade-lwc-oracle.json"),
		filepath.Join(outPath, "force-app", "main", "default", "lwc", "uiAppsApiOracle", "uiAppsApiOracle.js"),
		filepath.Join(outPath, "force-app", "main", "default", "lwc", "standardFlowOracle", "standardFlowOracle.js"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("missing %s: %v", path, err)
		}
	}
}

func TestLWCHelpListsParityCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"lwc", "--help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run returned %d, stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"lwc parity [--docs <dir>|--ledger <json>] [--json|--output <path>|--check <path>] [--fail-on <statuses>] [--require-oracle]",
		"lwc parity refresh-docs --source <dir> --output <json> [--allow-inventory-gaps]",
		"lwc parity reconcile-oracle --ledger <json> --capture <json> --output <path>",
		"lwc parity fixtures --ledger <path> --out <dir> [--check]",
		"Native LWC API parity ledger",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("help omitted %q:\n%s", want, out)
		}
	}
}

func TestManifestListsLWCCorpusCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"manifest", "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run returned %d, stderr=%s", code, stderr.String())
	}
	var manifest pluginManifestFile
	if err := json.Unmarshal(stdout.Bytes(), &manifest); err != nil {
		t.Fatalf("manifest is not JSON: %v\n%s", err, stdout.String())
	}
	summaries := packagedCommandSummaryByPath(manifest.Commands)
	if got := summaries["compat lwc corpus"]; got != "Scan package-first LWC corpus support gaps." {
		t.Fatalf("compat/lwc/corpus summary = %q", got)
	}
	if got := summaries["compat lwc parity"]; got != "Generate the Native LWC API parity ledger." {
		t.Fatalf("compat/lwc/parity summary = %q", got)
	}
}

func writeLWCCorpusCommandTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
