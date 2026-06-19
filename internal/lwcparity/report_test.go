package lwcparity

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildCrossReferencesDocsAndLocalLWCShims(t *testing.T) {
	docs := t.TempDir()
	writeLWCParityDoc(t, docs, "reference-api-modules.md", `
| API Module Name | Provides | First Available in Salesforce API Version |
| --- | --- | --- |
| [lightning/uiRecordApi](reference-lightning-ui-api-record.md) | Wire adapters and functions for record data. | 45.0 |
| [lightning/uiAppsApi](reference-lightning-ui-api-apps.md) | Wire adapters and functions for app metadata. | 48.0 |
`)
	writeLWCParityDoc(t, docs, "reference-ui-api.md",
		"- [`lightning/uiAppsApi` (Beta)](/docs/platform/lwc/guide/reference-lightning-ui-api-apps.html)\n"+
			"- [`lightning/uiListApi` (Deprecated)](/docs/platform/lwc/guide/reference-lightning-ui-api-list-ui.html)\n")
	writeLWCParityDoc(t, docs, "reference-salesforce-modules.md",
		"Use `@salesforce/community/basePath` for the community base path.\n"+
			"Use `@salesforce/site/activeLanguages` for active site languages.\n"+
			"Import a custom permission from `@salesforce/customPermission/Foo`.\n"+
			"Import a user permission from `@salesforce/userPermission/ViewSetup`.\n")
	writeLWCParityDoc(t, docs, "reference-page-reference-type.md", `
These page reference types are supported.

- Record Page
- Standard Flow
`)

	report, err := Build(Options{DocsDir: docs})
	if err != nil {
		t.Fatal(err)
	}
	rows := rowsByID(report.Rows)
	assertLWCParityRow(t, rows, "api-module:lightning/uiRecordApi", StatusSupportedLocal, "Wire adapters and functions for record data.", "45.0")
	assertLWCParityRow(t, rows, "api-module:lightning/uiAppsApi", StatusDocsOnly, "Wire adapters and functions for app metadata.", "48.0")
	assertLWCParityRow(t, rows, "api-module:lightning/uiListApi", StatusPartialLocal, "", "")
	assertLWCParityRow(t, rows, "salesforce-module:@salesforce/community/basePath", StatusSupportedLocal, "", "")
	assertLWCParityRow(t, rows, "salesforce-module:@salesforce/site/activeLanguages", StatusUnsupportedLocal, "", "")
	assertLWCParityRow(t, rows, "salesforce-module:@salesforce/userPermission/ViewSetup", StatusDocsOnly, "", "")
	assertLWCParityRow(t, rows, "page-reference:standard__recordPage", StatusSupportedLocal, "Record Page", "")
	assertLWCParityRow(t, rows, "page-reference:standard__flow", StatusDocsOnly, "Standard Flow", "")
	assertLWCParityRow(t, rows, "base-component:lightning/button", StatusLocalOnly, "", "")
	for id := range rows {
		if strings.Contains(id, "`") {
			t.Fatalf("row id contains backtick: %s", id)
		}
	}

	if report.Summary.Total != len(report.Rows) || report.Summary.ByStatus[StatusSupportedLocal] == 0 || report.Summary.ByCategory[CategoryBaseComponent] == 0 {
		t.Fatalf("summary = %#v rows=%d", report.Summary, len(report.Rows))
	}

	var markdown bytes.Buffer
	if err := WriteMarkdown(&markdown, report); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"# Native LWC API Parity Ledger",
		"`lightning/uiRecordApi`",
		"`@salesforce/site/activeLanguages`",
		"`standard__flow`",
		"`lightning/button`",
	} {
		if !strings.Contains(markdown.String(), want) {
			t.Fatalf("markdown omitted %q:\n%s", want, markdown.String())
		}
	}
}

func TestBuildRequiresDocsDirectory(t *testing.T) {
	_, err := Build(Options{})
	if err == nil || !strings.Contains(err.Error(), "--docs is required") {
		t.Fatalf("err = %v", err)
	}
}

func writeLWCParityDoc(t *testing.T, root, name, contents string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte(strings.TrimSpace(contents)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func rowsByID(rows []Row) map[string]Row {
	out := map[string]Row{}
	for _, row := range rows {
		out[row.ID] = row
	}
	return out
}

func assertLWCParityRow(t *testing.T, rows map[string]Row, id, status, summary, firstAPI string) {
	t.Helper()
	row, ok := rows[id]
	if !ok {
		t.Fatalf("missing row %s", id)
	}
	if row.Status != status || row.OracleStatus != StatusOracleMissing {
		t.Fatalf("%s status=%q oracle=%q", id, row.Status, row.OracleStatus)
	}
	if summary != "" && row.Summary != summary {
		t.Fatalf("%s summary=%q want %q", id, row.Summary, summary)
	}
	if firstAPI != "" && row.FirstAPI != firstAPI {
		t.Fatalf("%s firstAPI=%q want %q", id, row.FirstAPI, firstAPI)
	}
}
