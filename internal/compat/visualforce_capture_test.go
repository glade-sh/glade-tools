package compat

import (
	"bytes"
	"compress/zlib"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

type fakeVisualforceRunner struct {
	calls         []string
	fail          map[string]error
	apexStatus    int
	apexLogs      string
	scriptBatches [][]string
}

type fakeVisualforceHTTPClient struct {
	requests  []string
	responses map[string]*http.Response
}

func (c *fakeVisualforceHTTPClient) Do(req *http.Request) (*http.Response, error) {
	c.requests = append(c.requests, req.Method+" "+req.URL.RequestURI())
	resp := c.responses[req.URL.RequestURI()]
	if resp == nil {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Header:     http.Header{"Content-Type": []string{"text/plain"}},
			Body:       io.NopCloser(strings.NewReader("not found")),
		}, nil
	}
	return resp, nil
}

func (r *fakeVisualforceRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	call := name + " " + strings.Join(args, " ")
	r.calls = append(r.calls, call)
	for prefix, err := range r.fail {
		if strings.HasPrefix(call, prefix) {
			return []byte(`{"status":1}`), err
		}
	}
	switch {
	case strings.Contains(call, "org display"):
		return []byte(`{"status":0,"result":{"username":"test@example.com","orgId":"00Dxx0000000001","instanceUrl":"https://example.my.salesforce.com","accessToken":"secret-token"}}`), nil
	case strings.Contains(call, "project deploy start"):
		return []byte(`{"status":0,"result":{"status":"Succeeded"}}`), nil
	case strings.Contains(call, "apex run"):
		pages := readFakeVisualforceProbeScriptPages(args)
		if len(pages) == 0 {
			pages = []string{"Core"}
		}
		r.scriptBatches = append(r.scriptBatches, pages)
		logs := fakeVisualforceProbeLogs(pages)
		if r.apexLogs != "" {
			logs = r.apexLogs
		}
		return []byte(`{"status":` + strconv.Itoa(r.apexStatus) + `,"result":{"logs":` + strconvQuote(logs) + `}}`), nil
	default:
		return []byte(`{"status":0}`), nil
	}
}

func readFakeVisualforceProbeScriptPages(args []string) []string {
	for i := 0; i+1 < len(args); i++ {
		if args[i] != "--file" {
			continue
		}
		data, err := os.ReadFile(args[i+1])
		if err != nil {
			return nil
		}
		return uniqueFakeVisualforceProbePages(string(data))
	}
	return nil
}

func uniqueFakeVisualforceProbePages(script string) []string {
	matches := regexp.MustCompile(`Page\.([A-Za-z_][A-Za-z0-9_]*)\.getContent`).FindAllStringSubmatch(script, -1)
	seen := map[string]bool{}
	var pages []string
	for _, match := range matches {
		page := match[1]
		if seen[page] {
			continue
		}
		seen[page] = true
		pages = append(pages, page)
	}
	return pages
}

func fakeVisualforceProbeLogs(pages []string) string {
	var lines []string
	for _, page := range pages {
		html := base64.StdEncoding.EncodeToString([]byte(`<html><body>` + page + `</body></html>`))
		pdf := base64.StdEncoding.EncodeToString([]byte("%PDF-1.4\n" + page + "\n"))
		lines = append(lines,
			"USER_DEBUG|DEBUG|GLADE_VF_CAPTURE|"+page+"|HTML|OK|"+html,
			"USER_DEBUG|DEBUG|GLADE_VF_CAPTURE|"+page+"|PDF|OK|"+pdf,
		)
	}
	return strings.Join(lines, "\n")
}

func fakeCoreVisualforceHTTPClient() *fakeVisualforceHTTPClient {
	return &fakeVisualforceHTTPClient{responses: map[string]*http.Response{
		"/apex/Core": {
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
			Body:       io.NopCloser(strings.NewReader("<html><body>Core</body></html>")),
		},
		"/apex/Core?renderAs=pdf": {
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/pdf"}},
			Body:       io.NopCloser(strings.NewReader("%PDF-1.4\nCore\n")),
		},
	}}
}

func TestRunVisualforceCaptureBatchesSalesforceProbeRuns(t *testing.T) {
	pages := []string{
		"ProbeBatch01",
		"ProbeBatch02",
		"ProbeBatch03",
		"ProbeBatch04",
		"ProbeBatch05",
		"ProbeBatch06",
		"ProbeBatch07",
		"ProbeBatch08",
		"ProbeBatch09",
		"ProbeBatch10",
		"ProbeBatch11",
		"ProbeBatch12",
	}
	client := &fakeVisualforceHTTPClient{responses: map[string]*http.Response{}}
	for _, page := range pages {
		client.responses["/apex/"+page] = &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
			Body:       io.NopCloser(strings.NewReader("<html><body>" + page + "</body></html>")),
		}
	}
	runner := &fakeVisualforceRunner{}

	report, err := RunVisualforceCapture(context.Background(), VisualforceCaptureOptions{
		TargetOrg:  "oaer-probe-max",
		Project:    t.TempDir(),
		Pages:      pages,
		SkipDeploy: true,
		Runner:     runner,
		HTTPClient: client,
	})
	if err != nil {
		t.Fatal(err)
	}

	wantBatches := [][]string{
		pages[0:5],
		pages[5:10],
		pages[10:12],
	}
	if len(runner.scriptBatches) != len(wantBatches) {
		t.Fatalf("script batches = %#v, want %#v", runner.scriptBatches, wantBatches)
	}
	for i := range wantBatches {
		if strings.Join(runner.scriptBatches[i], ",") != strings.Join(wantBatches[i], ",") {
			t.Fatalf("script batch %d = %#v, want %#v", i, runner.scriptBatches[i], wantBatches[i])
		}
	}
	if report.Counts.Pages != len(pages) || report.Counts.HTMLPass != len(pages) || report.Counts.PDFPass != len(pages) {
		t.Fatalf("counts = %#v", report.Counts)
	}
}

func TestRunVisualforceCaptureIncludesRawHTTPMetadata(t *testing.T) {
	root := t.TempDir()
	body := []byte("<html><head><title>ignored</title></head><body><h1>Core</h1>\n<p>  Hello   Salesforce </p></body></html>")
	sum := sha256.Sum256(body)
	pdfBody := []byte("%PDF-1.4\nCore PDF\n")
	pdfSum := sha256.Sum256(pdfBody)
	client := &fakeVisualforceHTTPClient{responses: map[string]*http.Response{
		"/apex/Core": {
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type":  []string{"text/html; charset=utf-8"},
				"Cache-Control": []string{"private, no-cache"},
				"Set-Cookie":    []string{"sid=secret"},
			},
			Body: io.NopCloser(strings.NewReader(string(body))),
		},
		"/apex/Core?renderAs=pdf": {
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type":  []string{"application/pdf"},
				"Cache-Control": []string{"private, no-cache"},
			},
			Body: io.NopCloser(strings.NewReader(string(pdfBody))),
		},
	}}

	report, err := RunVisualforceCapture(context.Background(), VisualforceCaptureOptions{
		TargetOrg:  "oaer-probe-max",
		Project:    root,
		Pages:      []string{"Core"},
		SkipDeploy: true,
		Runner:     &fakeVisualforceRunner{},
		HTTPClient: client,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 2 || client.requests[0] != "GET /apex/Core" || client.requests[1] != "GET /apex/Core?renderAs=pdf" {
		t.Fatalf("requests = %v", client.requests)
	}
	if len(report.Pages) != 1 || len(report.Pages[0].Raw) != 2 {
		t.Fatalf("raw captures = %#v", report.Pages)
	}
	raw := report.Pages[0].Raw[0]
	if raw.Page != "Core" || raw.Request.Path != "/apex/Core" || raw.Request.Method != "GET" {
		t.Fatalf("request identity = %#v", raw)
	}
	if raw.StatusCode != http.StatusOK || raw.ContentType != "text/html; charset=utf-8" || raw.BodyBytes != len(body) {
		t.Fatalf("response metadata = %#v", raw)
	}
	if raw.Headers["Content-Type"] != "text/html; charset=utf-8" || raw.Headers["Cache-Control"] != "private, no-cache" {
		t.Fatalf("headers = %#v", raw.Headers)
	}
	if _, ok := raw.Headers["Set-Cookie"]; ok {
		t.Fatalf("selected headers leaked Set-Cookie: %#v", raw.Headers)
	}
	if raw.NormalizedText != "ignored Core Hello Salesforce" {
		t.Fatalf("normalized text = %q", raw.NormalizedText)
	}
	if raw.HTMLSHA256 != hex.EncodeToString(sum[:]) || raw.PDFSHA256 != "" {
		t.Fatalf("hashes = %#v", raw)
	}
	pdfRaw := report.Pages[0].Raw[1]
	if pdfRaw.Page != "Core" || pdfRaw.Request.Path != "/apex/Core" || pdfRaw.Request.Query != "renderAs=pdf" || pdfRaw.Request.Method != "GET" {
		t.Fatalf("pdf request identity = %#v", pdfRaw)
	}
	if pdfRaw.StatusCode != http.StatusOK || pdfRaw.ContentType != "application/pdf" || pdfRaw.BodyBytes != len(pdfBody) {
		t.Fatalf("pdf response metadata = %#v", pdfRaw)
	}
	if pdfRaw.PDFSHA256 != hex.EncodeToString(pdfSum[:]) || pdfRaw.HTMLSHA256 != "" {
		t.Fatalf("pdf hashes = %#v", pdfRaw)
	}
}

func TestRunVisualforceCaptureAnnotatesProbeMetadataAndContractText(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "visualforce-probe-index.json"), []byte(`{
  "groups": [
    {"name": "lifecycle", "owner": "oracle/corpus", "category": "phase1", "pages": ["Core"]}
  ],
  "pages": [
    {"name": "Core", "group": "lifecycle", "owner": "oracle/corpus", "category": "phase1"}
  ]
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	body := []byte("<html><body><h1>Core</h1><p>Hello 005000000000001AAA</p></body></html>")
	client := &fakeVisualforceHTTPClient{responses: map[string]*http.Response{
		"/apex/Core": {
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
			Body:       io.NopCloser(strings.NewReader(string(body))),
		},
	}}

	report, err := RunVisualforceCapture(context.Background(), VisualforceCaptureOptions{
		TargetOrg:  "oaer-probe-max",
		Project:    root,
		Pages:      []string{"Core"},
		SkipDeploy: true,
		Runner:     &fakeVisualforceRunner{},
		HTTPClient: client,
	})
	if err != nil {
		t.Fatal(err)
	}
	page := report.Pages[0]
	if page.Raw[0].NormalizedText != "Core Hello 005000000000001AAA" {
		t.Fatalf("raw normalized text = %q", page.Raw[0].NormalizedText)
	}
	data, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"group":    "lifecycle",
		"owner":    "oracle/corpus",
		"category": "phase1",
	} {
		if got, _ := payload[key].(string); got != want {
			t.Fatalf("%s = %q, want %q; json=%s", key, got, want, data)
		}
	}
	htmlPayload, ok := payload["html"].(map[string]any)
	if !ok {
		t.Fatalf("html payload missing: %s", data)
	}
	if got, _ := htmlPayload["normalizedText"].(string); got != "Core" {
		t.Fatalf("html normalizedText = %q; json=%s", got, data)
	}
	if got, _ := htmlPayload["contractText"].(string); got != "Core" {
		t.Fatalf("html contractText = %q; json=%s", got, data)
	}
}

func TestRunVisualforceCaptureKeepsProbeFailureWhenRawHTTPSucceeds(t *testing.T) {
	root := t.TempDir()
	errorMessage := base64.StdEncoding.EncodeToString([]byte("System.VisualforceException: getContent failed"))
	runner := &fakeVisualforceRunner{apexLogs: strings.Join([]string{
		"USER_DEBUG|DEBUG|GLADE_VF_CAPTURE|Core|HTML|ERROR|" + errorMessage,
		"USER_DEBUG|DEBUG|GLADE_VF_CAPTURE|Core|PDF|OK|" + base64.StdEncoding.EncodeToString([]byte("%PDF-1.4\nCore\n")),
	}, "\n")}
	client := fakeCoreVisualforceHTTPClient()

	report, err := RunVisualforceCapture(context.Background(), VisualforceCaptureOptions{
		TargetOrg:  "oaer-probe-max",
		Project:    root,
		Pages:      []string{"Core"},
		SkipDeploy: true,
		Runner:     runner,
		HTTPClient: client,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.OK || report.Counts.HTMLFail != 1 || report.Counts.HTMLPass != 0 {
		t.Fatalf("report = %#v", report)
	}
	page := report.Pages[0]
	if page.HTML.Status != "fail" || !strings.Contains(page.HTML.Error, "getContent failed") {
		t.Fatalf("html capture = %#v", page.HTML)
	}
	if len(page.Raw) != 2 || page.Raw[0].Status != "pass" || page.Raw[1].Status != "pass" {
		t.Fatalf("raw captures = %#v", page.Raw)
	}
}

func TestVisualforceRawCaptureDetectsPDFResponses(t *testing.T) {
	body := []byte("%PDF-1.4\ninvoice\n")
	sum := sha256.Sum256(body)
	capture, err := buildVisualforceRawCapture("Invoice", "https://example.my.salesforce.com/apex/Invoice", &http.Response{
		StatusCode: http.StatusFound,
		Header: http.Header{
			"Content-Type": []string{"application/octet-stream"},
			"Location":     []string{"/apex/Login"},
		},
		Body: io.NopCloser(strings.NewReader(string(body))),
	})
	if err != nil {
		t.Fatal(err)
	}
	if capture.RedirectLocation != "/apex/Login" {
		t.Fatalf("redirect location = %q", capture.RedirectLocation)
	}
	if capture.PDFSHA256 != hex.EncodeToString(sum[:]) || capture.HTMLSHA256 != "" {
		t.Fatalf("hashes = %#v", capture)
	}
	if capture.BodyBytes != len(body) || capture.NormalizedText != "" {
		t.Fatalf("body metadata = %#v", capture)
	}
}

func TestSetVisualforceRenderedPDFTextExtractsCompressedStreams(t *testing.T) {
	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	if _, err := writer.Write([]byte("BT /F1 24 Tf (PDF Basic)Tj ET BT /F2 12 Tf (Probe PDF body.)Tj ET")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	body := []byte("%PDF-1.4\n4 0 obj <</Filter/FlateDecode/Length " + strconv.Itoa(compressed.Len()) + ">>stream\n" + compressed.String() + "\nendstream\nendobj\n%%EOF")
	capture := VisualforceRenderedCapture{Status: "pass"}
	setVisualforceRenderedPDFText(&capture, body)

	if capture.NormalizedText != "PDF Basic Probe PDF body." || capture.ContractText != "PDF Basic Probe PDF body." || capture.TextHash == "" {
		t.Fatalf("capture text = %#v", capture)
	}
}

func TestRunVisualforceCaptureWritesRedactedRenderingEvidence(t *testing.T) {
	root := t.TempDir()
	pageDir := filepath.Join(root, "force-app", "main", "default", "pages")
	if err := os.MkdirAll(pageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pageDir, "Core.page"), []byte(`<apex:page>Core</apex:page>`), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, "capture.json")
	runner := &fakeVisualforceRunner{}
	report, err := RunVisualforceCapture(context.Background(), VisualforceCaptureOptions{
		TargetOrg:  "oaer-probe-max",
		Project:    root,
		Out:        out,
		Now:        func() time.Time { return time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC) },
		Runner:     runner,
		HTTPClient: fakeCoreVisualforceHTTPClient(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK || report.Counts.HTMLPass != 1 || report.Counts.PDFPass != 1 {
		t.Fatalf("unexpected report: %#v", report)
	}
	if got := report.Pages[0].HTML.Body; !strings.Contains(got, "Core") {
		t.Fatalf("html body = %q", got)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "secret-token") || strings.Contains(string(data), "accessToken") {
		t.Fatalf("capture leaked org credentials:\n%s", string(data))
	}
	if !strings.Contains(string(data), `"targetOrg": "oaer-probe-max"`) {
		t.Fatalf("capture omitted target org:\n%s", string(data))
	}
	if len(runner.calls) != 3 {
		t.Fatalf("calls = %v, want org display, deploy, apex run", runner.calls)
	}
	if !strings.Contains(runner.calls[0], "--verbose") {
		t.Fatalf("org display call omitted --verbose: %v", runner.calls)
	}
}

func TestRunVisualforceCaptureCanSkipDeploy(t *testing.T) {
	runner := &fakeVisualforceRunner{}
	report, err := RunVisualforceCapture(context.Background(), VisualforceCaptureOptions{
		TargetOrg:  "oaer-probe-max",
		Project:    t.TempDir(),
		Pages:      []string{"Core"},
		SkipDeploy: true,
		Runner:     runner,
		HTTPClient: fakeCoreVisualforceHTTPClient(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Deploy.Ran {
		t.Fatalf("deploy ran despite --skip-deploy")
	}
	for _, call := range runner.calls {
		if strings.Contains(call, "project deploy") {
			t.Fatalf("deploy call found: %v", runner.calls)
		}
	}
}

func TestRunVisualforceCaptureMarksNonOKApexRunStatusAsFailedReport(t *testing.T) {
	runner := &fakeVisualforceRunner{apexStatus: 1}
	report, err := RunVisualforceCapture(context.Background(), VisualforceCaptureOptions{
		TargetOrg:  "oaer-probe-max",
		Project:    t.TempDir(),
		Pages:      []string{"Core"},
		SkipDeploy: true,
		Runner:     runner,
		HTTPClient: fakeCoreVisualforceHTTPClient(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.OK || report.Probe.OK || report.Probe.Error == "" {
		t.Fatalf("expected failed probe status to fail report: %#v", report)
	}
	if report.Counts.HTMLPass != 1 || report.Counts.PDFPass != 1 {
		t.Fatalf("expected captures to remain available: %#v", report.Counts)
	}
}

func TestRunVisualforceCaptureRejectsInvalidPageIdentifier(t *testing.T) {
	_, err := RunVisualforceCapture(context.Background(), VisualforceCaptureOptions{
		TargetOrg: "oaer-probe-max",
		Project:   t.TempDir(),
		Pages:     []string{"Bad-Name"},
		Runner:    &fakeVisualforceRunner{},
	})
	if err == nil || !strings.Contains(err.Error(), "valid Apex Page identifier") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseVisualforceProbeCapturesReportsApexErrors(t *testing.T) {
	msg := base64.StdEncoding.EncodeToString([]byte("System.VisualforceException: PDF failed"))
	captures := parseVisualforceProbeCaptures([]byte("GLADE_VF_CAPTURE&#124;Invoice&#124;PDF&#124;ERROR&#124;" + msg))
	got := captures[visualforceCaptureKey("Invoice", "PDF")]
	if got.Status != "fail" || !strings.Contains(got.Error, "PDF failed") {
		t.Fatalf("capture = %#v", got)
	}
}

func TestParseVisualforceProbeCapturesReassemblesChunks(t *testing.T) {
	raw := []byte("<html><body>chunked visualforce capture</body></html>")
	encoded := base64.StdEncoding.EncodeToString(raw)
	sum := sha256.Sum256(raw)
	line := "GLADE_VF_CAPTURE_CHUNK&#124;Core&#124;HTML&#124;OK&#124;" +
		strconv.Itoa(len(raw)) + "&#124;" + hex.EncodeToString(sum[:]) +
		"&#124;0&#124;2&#124;" + encoded[:12] + "\n" +
		"GLADE_VF_CAPTURE_CHUNK&#124;Core&#124;HTML&#124;OK&#124;" +
		strconv.Itoa(len(raw)) + "&#124;" + hex.EncodeToString(sum[:]) +
		"&#124;1&#124;2&#124;" + encoded[12:]
	captures := parseVisualforceProbeCaptures([]byte(line))
	got := captures[visualforceCaptureKey("Core", "HTML")]
	if got.Status != "pass" || got.Body != string(raw) || got.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("capture = %#v", got)
	}
}

func TestRunVisualforceCaptureReturnsDeployFailure(t *testing.T) {
	runner := &fakeVisualforceRunner{fail: map[string]error{
		"sf project deploy": errors.New("deploy failed"),
	}}
	_, err := RunVisualforceCapture(context.Background(), VisualforceCaptureOptions{
		TargetOrg: "oaer-probe-max",
		Project:   t.TempDir(),
		Pages:     []string{"Core"},
		Runner:    runner,
	})
	if err == nil || !strings.Contains(err.Error(), "deploy") {
		t.Fatalf("err = %v", err)
	}
}

func strconvQuote(s string) string {
	data, _ := json.Marshal(s)
	return string(data)
}
