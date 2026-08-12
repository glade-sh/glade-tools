package compat

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestDiffVisualforceCaptureReportsFindsRenderAndPageMismatches(t *testing.T) {
	salesforce := []byte(`{
  "pages": [
    {
      "name": "Core",
      "html": {
        "status": "pass",
        "contentType": "text/html",
        "redirectLocation": "/apex/Core",
        "bytes": 22,
        "sha256": "sf-html-sha",
        "bodyHash": "sf-body-hash",
        "body": "<html>salesforce</html>",
        "textHash": "sf-text-hash",
        "text": "Salesforce"
      },
      "pdf": {"status": "pass", "bytes": 12, "sha256": "same-pdf"}
    },
    {"name": "SalesforceOnly", "html": {"status": "pass"}}
  ]
}`)
	local := []byte(`{
  "pages": [
    {
      "name": "Core",
      "html": {
        "status": "fail",
        "contentType": "text/plain",
        "redirectLocation": "/apex/Login",
        "bytes": 18,
        "sha256": "local-html-sha",
        "bodyHash": "local-body-hash",
        "body": "<html>local</html>",
        "textHash": "local-text-hash",
        "text": "Local"
      },
      "pdf": {"status": "pass", "bytes": 12, "sha256": "same-pdf"}
    },
    {"name": "LocalOnly", "html": {"status": "pass"}}
  ]
}`)

	report, err := DiffVisualforceCaptureReports(salesforce, local)
	if err != nil {
		t.Fatal(err)
	}
	if report.OK {
		t.Fatalf("expected mismatches, got %#v", report)
	}
	for _, want := range []string{
		"Core html contractText salesforce=Salesforce local=Local",
		"Core html status salesforce=pass local=fail",
		"Core html contentType salesforce=text/html local=text/plain",
		"Core html redirectLocation salesforce=/apex/Core local=/apex/Login",
		"SalesforceOnly page salesforce=present local=missing",
		"LocalOnly page salesforce=missing local=present",
	} {
		if !hasVisualforceCaptureDiff(report.Diffs, want) {
			t.Fatalf("missing diff %q in %#v", want, report.Diffs)
		}
	}
}

func TestDiffVisualforceCaptureReportsAcceptsMatchingReports(t *testing.T) {
	reportJSON := []byte(`{"pages":[{"name":"Core","html":{"status":"pass","bytes":4,"sha256":"abcd","body":"Core"},"pdf":{"status":"pass","bytes":4,"sha256":"pdf"}}]}`)

	report, err := DiffVisualforceCaptureReports(reportJSON, reportJSON)
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK || len(report.Diffs) != 0 {
		t.Fatalf("expected no diffs, got %#v", report)
	}
}

func TestDiffVisualforceCaptureReportsNormalizesContentType(t *testing.T) {
	salesforce := []byte(`{"pages":[{"name":"Core","html":{"status":"pass","contentType":"TEXT/HTML; boundary=abc; charset=UTF-8"}}]}`)
	local := []byte(`{"pages":[{"name":"Core","html":{"status":"pass","contentType":"text/html; charset=utf-8; boundary=abc"}}]}`)

	report, err := DiffVisualforceCaptureReports(salesforce, local)
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK || len(report.Diffs) != 0 {
		t.Fatalf("expected normalized contentType to match, got %#v", report.Diffs)
	}
}

func TestDiffVisualforceCaptureReportsIgnoresMissingContentType(t *testing.T) {
	salesforce := []byte(`{"pages":[{"name":"Core","html":{"status":"pass","contractText":"Core"}}]}`)
	local := []byte(`{"pages":[{"name":"Core","html":{"status":"pass","contentType":"text/html; charset=utf-8","contractText":"Core"}}]}`)

	report, err := DiffVisualforceCaptureReports(salesforce, local)
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK || len(report.Diffs) != 0 {
		t.Fatalf("expected missing contentType to be ignored, got %#v", report.Diffs)
	}
}

func TestDiffVisualforceCaptureReportsSkipsVolatileHTMLShellDiffs(t *testing.T) {
	salesforce := []byte(`{
  "pages": [{
    "name": "Core",
    "html": {
      "status": "pass",
      "contentType": "text/html",
      "bytes": 12345,
      "sha256": "sf-shell-sha",
      "bodyHash": "sf-shell-body",
      "body": "<html><head><style>.bPageBlock{}</style><script src=\"/jslibrary/1718290000000/sfdc/VFRemote.js\"></script><link rel=\"stylesheet\" href=\"/sCSS/1718290000000/Theme3/default/gc/zen-componentsCompatible.css\" /></head><body><form id=\"j_id0:j_id1\" name=\"j_id0:j_id1\"><input type=\"hidden\" name=\"com.salesforce.visualforce.ViewState\" id=\"j_id0:javax.faces.ViewState:0\" value=\"VGhpcyBpcyB2aWV3IHN0YXRl\" /><input type=\"hidden\" name=\"com.salesforce.visualforce.ViewStateMAC\" value=\"MAC-1718290000000\" /><input type=\"hidden\" name=\"_CONFIRMATIONTOKEN\" value=\"00Dxx0000001gPFEAY:005xx000001Sv6fAAC:1718290100000\" /><h1 id=\"j_id0:j_id2\">Account Console</h1><label for=\"j_id0:j_id3\">Account Name</label><span id=\"j_id0:j_id4\">Acme Inc</span><input type=\"submit\" name=\"j_id0:j_id5\" value=\"Save\" /><span>2026-06-13T12:01:02.000Z</span></form></body></html>"
    }
  }]
}`)
	local := []byte(`{
  "pages": [{
    "name": "Core",
    "html": {
      "status": "pass",
      "contentType": "text/html; charset=utf-8",
      "bytes": 42,
      "sha256": "local-shell-sha",
      "bodyHash": "local-shell-body",
      "body": "<html><body><form id=\"j_id9:j_id8\" name=\"j_id9:j_id8\"><h1 id=\"j_id9:j_id7\">Account Console</h1><label for=\"j_id9:j_id6\">Account Name</label><span id=\"j_id9:j_id5\">Acme Inc</span><input type=\"submit\" name=\"j_id9:j_id4\" value=\"Save\" /><span>2026-06-13 12:04:55</span></form></body></html>"
    }
  }]
}`)

	report, err := DiffVisualforceCaptureReports(salesforce, local)
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK || len(report.Diffs) != 0 {
		t.Fatalf("expected volatile html shell diffs to be skipped, got %#v", report.Diffs)
	}
}

func TestDiffVisualforceCaptureReportsTreatsPDFOnlyHTMLVariantAsEquivalent(t *testing.T) {
	salesforce := []byte(`{
  "pages": [{
    "name": "Invoice",
    "html": {
      "status": "pass",
      "contentType": "text/html; charset=UTF-8",
      "bytes": 1400,
      "sha256": "sf-html-pdf-sha",
      "bodyHash": "sf-html-pdf-body",
      "body": "%PDF-1.4\n1 0 obj\n<< /Type /Catalog >>\nendobj\n%%EOF"
    },
    "pdf": {
      "status": "pass",
      "contentType": "application/pdf",
      "bytes": 1100,
      "sha256": "sf-pdf-sha"
    }
  }]
}`)
	local := []byte(`{
  "pages": [{
    "name": "Invoice",
    "html": {
      "status": "notCaptured",
      "contentType": "application/pdf",
      "bytes": 610,
      "bodyHash": "local-html-pdf-body"
    },
    "pdf": {
      "status": "pass",
      "contentType": "application/pdf",
      "bytes": 610,
      "sha256": "local-pdf-sha"
    }
  }]
}`)

	report, err := DiffVisualforceCaptureReports(salesforce, local)
	if err != nil {
		t.Fatal(err)
	}
	for _, diff := range report.Diffs {
		if diff.Page == "Invoice" && diff.Variant == "html" {
			t.Fatalf("expected PDF-only html variant diffs to be skipped, got %#v", report.Diffs)
		}
	}
}

func TestDiffVisualforceCaptureReportsPrefersExplicitTextAndPreservesVisibleText(t *testing.T) {
	salesforce := []byte(`{
  "pages": [{
    "name": "Core",
    "html": {
      "status": "pass",
      "contentType": "text/html",
      "sha256": "sf-sha",
      "bodyHash": "sf-body",
      "body": "<html><body><h1>Wrong Shell Text</h1></body></html>",
      "textHash": "sf-text-hash",
      "text": "Account Console Account Name Acme Inc Save"
    }
  }]
}`)
	local := []byte(`{
  "pages": [{
    "name": "Core",
    "html": {
      "status": "pass",
      "contentType": "text/html",
      "sha256": "local-sha",
      "bodyHash": "local-body",
      "body": "<html><body><h1>Account Console</h1><label>Account Name</label><span>Acme Incorporated</span><button>Save</button></body></html>",
      "textHash": "local-text-hash",
      "text": "Account Console Account Name Acme Incorporated Save"
    }
  }]
}`)

	report, err := DiffVisualforceCaptureReports(salesforce, local)
	if err != nil {
		t.Fatal(err)
	}
	if report.OK {
		t.Fatalf("expected visible text mismatch, got %#v", report)
	}
	if !hasVisualforceCaptureDiffField(report.Diffs, "contractText") {
		t.Fatalf("expected contractText diff to preserve controller value mismatch, got %#v", report.Diffs)
	}
}

func TestDiffVisualforceCaptureReportsPrefersExplicitContractText(t *testing.T) {
	salesforce := []byte(`{
  "pages": [{
    "name": "Core",
    "html": {
      "status": "pass",
      "contentType": "text/html",
      "body": "<html><body><form id=\"j_id0\"><h1>Account Console</h1><input type=\"hidden\" value=\"volatile\" /></form></body></html>",
      "contractText": "Account Console Save"
    }
  }]
}`)
	local := []byte(`{
  "pages": [{
    "name": "Core",
    "html": {
      "status": "pass",
      "contentType": "text/html; charset=utf-8",
      "body": "<html><body><main><h1>Account Console</h1><button>Save</button></main></body></html>",
      "contractText": "Account Console Save"
    }
  }]
}`)

	report, err := DiffVisualforceCaptureReports(salesforce, local)
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK || len(report.Diffs) != 0 {
		t.Fatalf("expected explicit contractText to suppress shell diffs, got %#v", report.Diffs)
	}
}

func TestDiffVisualforceCaptureReportsUsesNormalizedTextAsContractFallback(t *testing.T) {
	salesforce := []byte(`{
  "pages": [{
    "name": "Core",
    "html": {
      "status": "pass",
      "body": "<html><body><h1>Wrong shell</h1></body></html>",
      "normalizedText": "Account Console 005000000000001AAA"
    }
  }]
}`)
	local := []byte(`{
  "pages": [{
    "name": "Core",
    "html": {
      "status": "pass",
      "body": "<html><body><h1>Different shell</h1></body></html>",
      "normalizedText": "Account Console 005000000000001AAA"
    }
  }]
}`)

	report, err := DiffVisualforceCaptureReports(salesforce, local)
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK || len(report.Diffs) != 0 {
		t.Fatalf("expected normalizedText to suppress shell diffs, got %#v", report.Diffs)
	}
}

func TestDiffVisualforceCaptureReportsPreservesMissingSalesforceIDGaps(t *testing.T) {
	salesforce := []byte(`{
  "pages": [{
    "name": "Globals",
    "html": {
      "status": "pass",
      "body": "<html><body>User: 005000000000001AAA Org: 00D000000000001EAA</body></html>"
    }
  }]
}`)
	local := []byte(`{
  "pages": [{
    "name": "Globals",
    "html": {
      "status": "pass",
      "body": "<html><body><span>User: </span><span> Org: </span></body></html>"
    }
  }]
}`)

	report, err := DiffVisualforceCaptureReports(salesforce, local)
	if err != nil {
		t.Fatal(err)
	}
	if report.OK {
		t.Fatalf("expected missing local id values to remain a diff")
	}
	if !hasVisualforceCaptureDiffField(report.Diffs, "contractText") {
		t.Fatalf("expected contractText diff for missing ids, got %#v", report.Diffs)
	}
	if hasVisualforceCaptureDiffField(report.Diffs, "body") || hasVisualforceCaptureDiffField(report.Diffs, "sha256") || hasVisualforceCaptureDiffField(report.Diffs, "bodyHash") {
		t.Fatalf("expected raw body/hash noise to be suppressed, got %#v", report.Diffs)
	}
}

func TestNormalizeVisualforceDiffContractTextPreservesLowercaseComponentTextAndDropsVolatileTokens(t *testing.T) {
	text := strings.Join([]string{
		"alphaalphabravobravocharliecharlie",
		"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"VGhpcyBpcyB2aWV3IHN0YXRlVGhpcyBpcyB2aWV3IHN0YXRl",
		"j_id0:j_id1",
	}, " ")

	got := normalizeVisualforceDiffContractText(text)
	want := "alphaalphabravobravocharliecharlie"
	if got != want {
		t.Fatalf("normalized contract text = %q, want %q", got, want)
	}
}

func TestDiffVisualforceCaptureReportsSkipsPDFHashesWhenContractTextMatches(t *testing.T) {
	salesforce := []byte(`{"pages":[{"name":"Core","pdf":{"status":"pass","contentType":"application/pdf","sha256":"sf-pdf-sha","bodyHash":"sf-pdf-body","text":"Invoice Total 42"}}]}`)
	local := []byte(`{"pages":[{"name":"Core","pdf":{"status":"pass","contentType":"application/pdf","sha256":"local-pdf-sha","bodyHash":"local-pdf-body","text":"Invoice   Total 42"}}]}`)

	report, err := DiffVisualforceCaptureReports(salesforce, local)
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK || len(report.Diffs) != 0 {
		t.Fatalf("expected pdf hash diffs to be skipped when contract text matches, got %#v", report.Diffs)
	}
}

func TestDiffVisualforceCaptureReportsFlagsPDFHashesWhenNoTextContractExists(t *testing.T) {
	salesforce := []byte(`{"pages":[{"name":"Core","pdf":{"status":"pass","bytes":1100,"sha256":"sf-pdf-sha","bodyHash":"sf-pdf-body"}}]}`)
	local := []byte(`{"pages":[{"name":"Core","pdf":{"status":"pass","contentType":"application/pdf","bytes":610,"sha256":"local-pdf-sha","bodyHash":"local-pdf-body"}}]}`)

	report, err := DiffVisualforceCaptureReports(salesforce, local)
	if err != nil {
		t.Fatal(err)
	}
	if report.OK || !hasVisualforceCaptureDiffField(report.Diffs, "sha256") {
		t.Fatalf("expected pdf hash diff without text contract, got %#v", report.Diffs)
	}
}

func TestDiffVisualforceCaptureReportsSkipsPDFHashesWhenHTMLContractMatches(t *testing.T) {
	salesforce := []byte(`{"pages":[{"name":"Core","html":{"status":"pass","contractText":"Invoice Total 42"},"pdf":{"status":"pass","bytes":1100,"sha256":"sf-pdf-sha","bodyHash":"sf-pdf-body"}}]}`)
	local := []byte(`{"pages":[{"name":"Core","html":{"status":"pass","contractText":"Invoice   Total 42"},"pdf":{"status":"pass","contentType":"application/pdf","bytes":610,"sha256":"local-pdf-sha","bodyHash":"local-pdf-body"}}]}`)

	report, err := DiffVisualforceCaptureReports(salesforce, local)
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK || len(report.Diffs) != 0 {
		t.Fatalf("expected pdf hash diffs to be skipped when html contract text matches, got %#v", report.Diffs)
	}
}

func TestDiffVisualforceCaptureReportsDerivesPDFContractTextFromBase64(t *testing.T) {
	pdf := base64.StdEncoding.EncodeToString([]byte("%PDF-1.4\n1 0 obj\n<< /Length 29 >>\nstream\nBT (Invoice Total 42) Tj ET\nendstream\nendobj\n%%EOF"))
	salesforce := []byte(`{"pages":[{"name":"Core","pdf":{"status":"pass","contentType":"application/pdf","bytes":1100,"sha256":"sf-pdf-sha","bodyHash":"sf-pdf-body","base64":"` + pdf + `"}}]}`)
	local := []byte(`{"pages":[{"name":"Core","pdf":{"status":"pass","contentType":"application/pdf","bytes":610,"sha256":"local-pdf-sha","bodyHash":"local-pdf-body","base64":"` + pdf + `"}}]}`)

	report, err := DiffVisualforceCaptureReports(salesforce, local)
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK || len(report.Diffs) != 0 {
		t.Fatalf("expected pdf hashes to be skipped after deriving base64 text, got %#v", report.Diffs)
	}
}

func TestDiffVisualforceCaptureReportsIncludesScoreboardSummary(t *testing.T) {
	salesforce := []byte(`{
  "pages": [
    {"name": "Core", "html": {"status": "pass", "bytes": 22}, "pdf": {"status": "pass"}},
    {"name": "SalesforceOnly", "html": {"status": "pass"}}
  ]
}`)
	local := []byte(`{
  "pages": [
    {"name": "Core", "html": {"status": "fail", "bytes": 18}, "pdf": {"status": "pass"}},
    {"name": "LocalOnly", "html": {"status": "pass"}}
  ]
}`)

	report, err := DiffVisualforceCaptureReports(salesforce, local)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Summary struct {
			PageCountCompared  int            `json:"pageCountCompared"`
			MissingPageCount   int            `json:"missingPageCount"`
			DifferingPageCount int            `json:"differingPageCount"`
			DiffCountByField   map[string]int `json:"diffCountByField"`
			DiffCountByVariant map[string]int `json:"diffCountByVariant"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Summary.PageCountCompared != 3 {
		t.Fatalf("pageCountCompared = %d, want 3; json=%s", payload.Summary.PageCountCompared, data)
	}
	if payload.Summary.MissingPageCount != 2 {
		t.Fatalf("missingPageCount = %d, want 2; json=%s", payload.Summary.MissingPageCount, data)
	}
	if payload.Summary.DifferingPageCount != 3 {
		t.Fatalf("differingPageCount = %d, want 3; json=%s", payload.Summary.DifferingPageCount, data)
	}
	if got := payload.Summary.DiffCountByField["status"]; got != 1 {
		t.Fatalf("diffCountByField[status] = %d, want 1; json=%s", got, data)
	}
	if got := payload.Summary.DiffCountByField["bytes"]; got != 1 {
		t.Fatalf("diffCountByField[bytes] = %d, want 1; json=%s", got, data)
	}
	if got := payload.Summary.DiffCountByField["page"]; got != 2 {
		t.Fatalf("diffCountByField[page] = %d, want 2; json=%s", got, data)
	}
	if got := payload.Summary.DiffCountByVariant["html"]; got != 2 {
		t.Fatalf("diffCountByVariant[html] = %d, want 2; json=%s", got, data)
	}
	if got := payload.Summary.DiffCountByVariant["page"]; got != 2 {
		t.Fatalf("diffCountByVariant[page] = %d, want 2; json=%s", got, data)
	}
}

func TestDiffVisualforceCaptureReportsIncludesLaneScoreboardFromPageMetadata(t *testing.T) {
	salesforce := []byte(`{
  "pages": [
    {"name": "LifecyclePass", "group": "lifecycle", "owner": "oracle/corpus", "category": "phase1", "html": {"status": "pass"}},
    {"name": "LifecycleFail", "group": "lifecycle", "owner": "oracle/corpus", "category": "phase1", "html": {"status": "pass"}},
    {"name": "FieldMissing", "group": "fields", "owner": "oracle/corpus", "category": "phase1", "html": {"status": "pass"}}
  ]
}`)
	local := []byte(`{
  "pages": [
    {"name": "LifecyclePass", "group": "lifecycle", "owner": "oracle/corpus", "category": "phase1", "html": {"status": "pass"}},
    {"name": "LifecycleFail", "group": "lifecycle", "owner": "oracle/corpus", "category": "phase1", "html": {"status": "fail"}}
  ]
}`)

	report, err := DiffVisualforceCaptureReports(salesforce, local)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Summary struct {
			Scoreboard struct {
				Groups     []visualforceScoreboardTestRow `json:"groups"`
				Owners     []visualforceScoreboardTestRow `json:"owners"`
				Categories []visualforceScoreboardTestRow `json:"categories"`
			} `json:"scoreboard"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	assertVisualforceScoreboardRow(t, payload.Summary.Scoreboard.Groups, "lifecycle", "oracle/corpus", "phase1", 2, 1, 1, 0, 1)
	assertVisualforceScoreboardRow(t, payload.Summary.Scoreboard.Groups, "fields", "oracle/corpus", "phase1", 1, 0, 1, 1, 1)
	assertVisualforceScoreboardRow(t, payload.Summary.Scoreboard.Owners, "oracle/corpus", "", "", 3, 1, 2, 1, 2)
	assertVisualforceScoreboardRow(t, payload.Summary.Scoreboard.Categories, "phase1", "", "", 3, 1, 2, 1, 2)
}

func TestDiffVisualforceCaptureReportsFlagsIndexOnlyPages(t *testing.T) {
	salesforce := []byte(`{"pages":[{"name":"Captured","html":{"status":"pass"}}]}`)
	local := []byte(`{"pages":[{"name":"Captured","html":{"status":"pass"}}]}`)
	index := &VisualforceProbeIndex{
		Groups: []VisualforceProbeGroup{{
			Name:     "lifecycle",
			Owner:    "oracle/corpus",
			Category: "phase1",
			Pages:    []string{"Captured", "IndexOnly"},
		}},
		Pages: []VisualforceProbePage{
			{Name: "Captured", Group: "lifecycle"},
			{Name: "IndexOnly", Group: "lifecycle"},
		},
	}

	report, err := diffVisualforceCaptureReports(salesforce, local, index)
	if err != nil {
		t.Fatal(err)
	}
	if report.OK || report.DiffCount == 0 {
		t.Fatalf("expected index-only page to fail diff, got %#v", report)
	}
	if report.Summary.MissingPageCount != 1 || report.Summary.DifferingPageCount != 1 {
		t.Fatalf("summary = %#v", report.Summary)
	}
	if !hasVisualforceCaptureDiff(report.Diffs, "IndexOnly page salesforce=missing local=missing") {
		t.Fatalf("diffs = %#v", report.Diffs)
	}
}

func TestDiffVisualforceCaptureReportsRedactsRawPayloadValues(t *testing.T) {
	longSalesforceBody := "<html><body><main>" + strings.Repeat("salesforce raw body ", 20) + "</main></body></html>"
	longLocalBody := "<html><body><main>" + strings.Repeat("local raw body ", 20) + "</main></body></html>"
	longText := strings.Repeat("visible text ", 40)
	sfPDF := base64.StdEncoding.EncodeToString([]byte("%PDF-1.4\n" + strings.Repeat("Salesforce PDF ", 30)))
	localPDF := base64.StdEncoding.EncodeToString([]byte("%PDF-1.4\n" + strings.Repeat("Local PDF ", 30)))
	salesforce := []byte(`{"pages":[{"name":"Core","html":{"status":"pass","body":` + strconvQuote(longSalesforceBody) + `,"text":` + strconvQuote(longText+"Salesforce") + `},"pdf":{"status":"pass","base64":` + strconvQuote(sfPDF) + `}}]}`)
	local := []byte(`{"pages":[{"name":"Core","html":{"status":"pass","body":` + strconvQuote(longLocalBody) + `,"text":` + strconvQuote(longText+"Local") + `},"pdf":{"status":"pass","base64":` + strconvQuote(localPDF) + `}}]}`)

	report, err := DiffVisualforceCaptureReports(salesforce, local)
	if err != nil {
		t.Fatal(err)
	}
	if report.OK {
		t.Fatalf("expected diff, got %#v", report)
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	jsonText := string(data)
	for _, leaked := range []string{
		"salesforce raw body salesforce raw body",
		"local raw body local raw body",
		sfPDF,
		localPDF,
		longText + "Salesforce",
		longText + "Local",
	} {
		if strings.Contains(jsonText, leaked) {
			t.Fatalf("diff leaked raw payload %q in %s", leaked, jsonText)
		}
	}
	if !strings.Contains(jsonText, "redacted") || !strings.Contains(jsonText, "sha256=") {
		t.Fatalf("diff omitted redaction diagnostics: %s", jsonText)
	}
}

func TestDiffVisualforceCaptureReportsComparesAndRedactsBase64WithoutContractText(t *testing.T) {
	sfPDF := base64.StdEncoding.EncodeToString([]byte("%PDF-1.4\n" + strings.Repeat("A", 80)))
	localPDF := base64.StdEncoding.EncodeToString([]byte("%PDF-1.4\n" + strings.Repeat("B", 80)))
	salesforce := []byte(`{"pages":[{"name":"Core","pdf":{"status":"pass","base64":` + strconvQuote(sfPDF) + `}}]}`)
	local := []byte(`{"pages":[{"name":"Core","pdf":{"status":"pass","base64":` + strconvQuote(localPDF) + `}}]}`)

	report, err := DiffVisualforceCaptureReports(salesforce, local)
	if err != nil {
		t.Fatal(err)
	}
	if report.OK || !hasVisualforceCaptureDiffField(report.Diffs, "base64") {
		t.Fatalf("expected base64 diff, got %#v", report)
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	jsonText := string(data)
	if strings.Contains(jsonText, sfPDF) || strings.Contains(jsonText, localPDF) {
		t.Fatalf("base64 diff leaked raw payload: %s", jsonText)
	}
	if !strings.Contains(jsonText, "redacted base64") || !strings.Contains(jsonText, "sha256=") {
		t.Fatalf("base64 diff omitted redaction diagnostics: %s", jsonText)
	}
}

func TestDiffVisualforceCaptureReportsPopulatesPDFFallbackContractMismatch(t *testing.T) {
	salesforce := []byte(`{"pages":[{"name":"Invoice","html":{"status":"pass","contractText":"Invoice Total 42"},"pdf":{"status":"pass","sha256":"sf-pdf"}}]}`)
	local := []byte(`{"pages":[{"name":"Invoice","html":{"status":"pass","contractText":"Invoice Total 43"},"pdf":{"status":"pass","sha256":"local-pdf"}}]}`)

	report, err := DiffVisualforceCaptureReports(salesforce, local)
	if err != nil {
		t.Fatal(err)
	}
	if report.OK {
		t.Fatalf("expected fallback contract mismatch, got %#v", report)
	}
	if !hasVisualforceCaptureDiff(report.Diffs, "Invoice pdf contractText salesforce=Invoice Total 42 local=Invoice Total 43") {
		t.Fatalf("diffs = %#v", report.Diffs)
	}
}

type visualforceScoreboardTestRow struct {
	Name      string `json:"name"`
	Owner     string `json:"owner,omitempty"`
	Category  string `json:"category,omitempty"`
	PageCount int    `json:"pageCount"`
	PassCount int    `json:"passCount"`
	FailCount int    `json:"failCount"`
	Missing   int    `json:"missingCount"`
	DiffCount int    `json:"diffCount"`
}

func assertVisualforceScoreboardRow(t *testing.T, rows []visualforceScoreboardTestRow, name, owner, category string, pageCount, passCount, failCount, missing, diffCount int) {
	t.Helper()
	for _, row := range rows {
		if row.Name != name {
			continue
		}
		if row.Owner != owner || row.Category != category || row.PageCount != pageCount || row.PassCount != passCount || row.FailCount != failCount || row.Missing != missing || row.DiffCount != diffCount {
			t.Fatalf("scoreboard row %q = %#v", name, row)
		}
		return
	}
	t.Fatalf("missing scoreboard row %q in %#v", name, rows)
}

func hasVisualforceCaptureDiff(diffs []VisualforceCaptureDiff, want string) bool {
	for _, diff := range diffs {
		parts := []string{diff.Page}
		if diff.Variant != "" {
			parts = append(parts, diff.Variant)
		}
		if diff.Field != "" {
			parts = append(parts, diff.Field)
		}
		got := strings.Join(parts, " ") + " salesforce=" + diff.Salesforce + " local=" + diff.Local
		if got == want {
			return true
		}
	}
	return false
}

func hasVisualforceCaptureDiffField(diffs []VisualforceCaptureDiff, field string) bool {
	for _, diff := range diffs {
		if diff.Field == field {
			return true
		}
	}
	return false
}
