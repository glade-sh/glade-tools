package toolcli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if os.Getenv("GLADE_TOOLS_FAKE_GLADE_VF") == "1" {
		runFakeGladeDevVF()
		return
	}
	os.Exit(m.Run())
}

func TestVisualforceHelpListsScratchOrgCapture(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"visualforce", "--help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run returned %d, stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"visualforce capture",
		"visualforce diff",
		"--target-org",
		"--salesforce",
		"--batch-size <n>",
		"oaer-probe-max",
		"glade compat visualforce capture",
		"glade compat visualforce diff",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("help omitted %q:\n%s", want, out)
		}
	}
}

func TestVisualforceHelpListsLocalCapture(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"visualforce", "--help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run returned %d, stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"visualforce capture --local --glade-bin <path>",
		"--glade-bin <path>",
		"--local",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("help omitted %q:\n%s", want, out)
		}
	}
}

func TestCompatHelpListsVisualforceBatchSizeAndDiffOut(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"compat", "--help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run returned %d, stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"visualforce capture --target-org <alias> [--project <root>] [--pages <a,b>] [--out <path>] [--skip-deploy] [--batch-size <n>] [--json]",
		"visualforce diff --salesforce <json> --local <json> [--project <root>] [--out <json>] [--json]",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("help omitted %q:\n%s", want, out)
		}
	}
}

func TestVisualforceLocalCaptureRequiresGladeBin(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"visualforce", "capture", "--local", "--project", t.TempDir(), "--pages", "Core"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected missing glade bin to fail, stdout=%s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "--glade-bin is required") {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestVisualforceLocalCaptureReturnsNonzeroAfterWritingFailedReport(t *testing.T) {
	root := t.TempDir()
	pageDir := filepath.Join(root, "force-app", "main", "default", "pages")
	if err := os.MkdirAll(pageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pageDir, "Core.page"), []byte(`<apex:page>Core</apex:page>`), 0o644); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(root, "capture.json")
	termFile := filepath.Join(t.TempDir(), "terminated.txt")
	t.Setenv("GLADE_TOOLS_FAKE_GLADE_VF", "1")
	t.Setenv("GLADE_TOOLS_FAKE_GLADE_VF_TERM", termFile)

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"visualforce", "capture", "--local", "--glade-bin", os.Args[0], "--project", root, "--pages", "Core", "--out", outPath}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected failed capture report to return nonzero, stdout=%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "pdf pass=0 fail=1") || !strings.Contains(stdout.String(), "wrote "+outPath) {
		t.Fatalf("stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "Visualforce capture failed") {
		t.Fatalf("stderr = %s", stderr.String())
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		OK     bool `json:"ok"`
		Counts struct {
			HTMLPass int `json:"htmlPass"`
			HTMLFail int `json:"htmlFail"`
			PDFPass  int `json:"pdfPass"`
			PDFFail  int `json:"pdfFail"`
		} `json:"counts"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.OK || payload.Counts.HTMLPass != 1 || payload.Counts.HTMLFail != 0 || payload.Counts.PDFPass != 0 || payload.Counts.PDFFail != 1 {
		t.Fatalf("capture json = %s", data)
	}
	waitForFile(t, termFile)
}

func TestVisualforceCaptureRequiresTargetOrg(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"visualforce", "capture", "--pages", "Core", "--skip-deploy"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected missing target org to fail, stdout=%s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "--target-org is required") {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestVisualforceDiffReportsMismatchesAndReturnsNonzero(t *testing.T) {
	dir := t.TempDir()
	salesforce := filepath.Join(dir, "salesforce.json")
	local := filepath.Join(dir, "local.json")
	writeVisualforceCommandTestFile(t, salesforce, `{
  "pages": [
    {"name":"Core","html":{"status":"pass","contentType":"text/html","redirectLocation":"/apex/Core","bytes":10,"sha256":"sf","body":"salesforce"}},
    {"name":"SalesforceOnly","html":{"status":"pass"}}
  ]
}`)
	writeVisualforceCommandTestFile(t, local, `{
  "pages": [
    {"name":"Core","html":{"status":"fail","contentType":"text/plain","redirectLocation":"/apex/Login","bytes":12,"sha256":"local","body":"local"}},
    {"name":"LocalOnly","html":{"status":"pass"}}
  ]
}`)

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"compat", "visualforce", "diff", "--salesforce", salesforce, "--local", local}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected visualforce diff to return nonzero, stdout=%s", stdout.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"visualforce diff: 6 differences",
		"Core html contractText: salesforce=salesforce local=local",
		"Core html status: salesforce=pass local=fail",
		"Core html contentType: salesforce=text/html local=text/plain",
		"Core html redirectLocation: salesforce=/apex/Core local=/apex/Login",
		"SalesforceOnly page: salesforce=present local=missing",
		"LocalOnly page: salesforce=missing local=present",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout omitted %q:\n%s", want, out)
		}
	}
	if !strings.Contains(stderr.String(), "Visualforce capture diff found") {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestVisualforceDiffWritesReportToOutAndKeepsHumanStdout(t *testing.T) {
	dir := t.TempDir()
	salesforce := filepath.Join(dir, "salesforce.json")
	local := filepath.Join(dir, "local.json")
	outPath := filepath.Join(dir, "reports", "visualforce", "diff.json")
	writeVisualforceCommandTestFile(t, salesforce, `{
  "pages": [
    {"name":"Core","html":{"status":"pass","bytes":10}},
    {"name":"SalesforceOnly","html":{"status":"pass"}}
  ]
}`)
	writeVisualforceCommandTestFile(t, local, `{
  "pages": [
    {"name":"Core","html":{"status":"fail","bytes":12}},
    {"name":"LocalOnly","html":{"status":"pass"}}
  ]
}`)

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"visualforce", "diff", "--salesforce", salesforce, "--local", local, "--out", outPath}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected visualforce diff to return nonzero, stdout=%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "visualforce diff: 4 differences") {
		t.Fatalf("stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "Visualforce capture diff found") {
		t.Fatalf("stderr = %s", stderr.String())
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		DiffCount int `json:"diffCount"`
		Summary   struct {
			PageCountCompared int            `json:"pageCountCompared"`
			MissingPageCount  int            `json:"missingPageCount"`
			DiffCountByField  map[string]int `json:"diffCountByField"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.DiffCount != 4 {
		t.Fatalf("diffCount = %d, want 4; json=%s", payload.DiffCount, data)
	}
	if payload.Summary.PageCountCompared != 3 {
		t.Fatalf("pageCountCompared = %d, want 3; json=%s", payload.Summary.PageCountCompared, data)
	}
	if payload.Summary.MissingPageCount != 2 {
		t.Fatalf("missingPageCount = %d, want 2; json=%s", payload.Summary.MissingPageCount, data)
	}
	if got := payload.Summary.DiffCountByField["page"]; got != 2 {
		t.Fatalf("diffCountByField[page] = %d, want 2; json=%s", got, data)
	}
}

func TestVisualforceDiffRedactsRawPayloadsInStdoutAndJSON(t *testing.T) {
	dir := t.TempDir()
	salesforce := filepath.Join(dir, "salesforce.json")
	local := filepath.Join(dir, "local.json")
	outPath := filepath.Join(dir, "diff.json")
	longSalesforceBody := "<html><body>" + strings.Repeat("salesforce secret payload ", 20) + "</body></html>"
	longLocalBody := "<html><body>" + strings.Repeat("local secret payload ", 20) + "</body></html>"
	writeVisualforceCommandTestFile(t, salesforce, `{"pages":[{"name":"Core","html":{"status":"pass","body":`+strconvQuote(longSalesforceBody)+`}}]}`)
	writeVisualforceCommandTestFile(t, local, `{"pages":[{"name":"Core","html":{"status":"pass","body":`+strconvQuote(longLocalBody)+`}}]}`)

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"visualforce", "diff", "--salesforce", salesforce, "--local", local, "--out", outPath}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected visualforce diff to return nonzero, stdout=%s", stdout.String())
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	combined := stdout.String() + string(data)
	for _, leaked := range []string{
		"salesforce secret payload salesforce secret payload",
		"local secret payload local secret payload",
	} {
		if strings.Contains(combined, leaked) {
			t.Fatalf("diff leaked raw payload %q\nstdout=%s\njson=%s", leaked, stdout.String(), data)
		}
	}
	if !strings.Contains(combined, "redacted") || !strings.Contains(combined, "sha256=") {
		t.Fatalf("diff omitted redaction diagnostics\nstdout=%s\njson=%s", stdout.String(), data)
	}
}

func TestVisualforceDiffScoresFixtureIndexLanes(t *testing.T) {
	dir := t.TempDir()
	salesforce := filepath.Join(dir, "salesforce.json")
	local := filepath.Join(dir, "local.json")
	outPath := filepath.Join(dir, "diff.json")
	writeVisualforceCommandTestFile(t, filepath.Join(dir, "visualforce-probe-index.json"), `{
  "summary": {"pageCount": 3, "groupCount": 2, "owners": ["oracle/corpus"], "categories": ["phase1"]},
  "groups": [
    {"name": "lifecycle", "owner": "oracle/corpus", "category": "phase1", "pages": ["LifecyclePass", "LifecycleFail"]},
    {"name": "fields", "owner": "oracle/corpus", "category": "phase1", "pages": ["FieldMissing"]}
  ],
  "pages": [
    {"name": "LifecyclePass", "group": "lifecycle"},
    {"name": "LifecycleFail", "group": "lifecycle"},
    {"name": "FieldMissing", "group": "fields"}
  ]
}`)
	writeVisualforceCommandTestFile(t, salesforce, `{
  "pages": [
    {"name": "LifecyclePass", "html": {"status": "pass"}},
    {"name": "LifecycleFail", "html": {"status": "pass"}},
    {"name": "FieldMissing", "html": {"status": "pass"}}
  ]
}`)
	writeVisualforceCommandTestFile(t, local, `{
  "pages": [
    {"name": "LifecyclePass", "html": {"status": "pass"}},
    {"name": "LifecycleFail", "html": {"status": "fail"}}
  ]
}`)

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"visualforce", "diff", "--salesforce", salesforce, "--local", local, "--project", dir, "--out", outPath}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected visualforce diff to return nonzero, stdout=%s", stdout.String())
	}
	for _, want := range []string{
		"scoreboard by group:",
		"- lifecycle (phase1, oracle/corpus): pass=1 fail=1 missing=0 diffs=1 pages=2",
		"- fields (phase1, oracle/corpus): pass=0 fail=1 missing=1 diffs=1 pages=1",
		"scoreboard by owner:",
		"- oracle/corpus: pass=1 fail=2 missing=1 diffs=2 pages=3",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout omitted %q:\n%s", want, stdout.String())
		}
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Summary struct {
			Scoreboard struct {
				Groups []struct {
					Name      string `json:"name"`
					Owner     string `json:"owner"`
					Category  string `json:"category"`
					PageCount int    `json:"pageCount"`
					PassCount int    `json:"passCount"`
					FailCount int    `json:"failCount"`
					Missing   int    `json:"missingCount"`
					DiffCount int    `json:"diffCount"`
				} `json:"groups"`
			} `json:"scoreboard"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Summary.Scoreboard.Groups) != 2 {
		t.Fatalf("scoreboard groups = %#v", payload.Summary.Scoreboard.Groups)
	}
	if row := payload.Summary.Scoreboard.Groups[0]; row.Name != "fields" || row.Owner != "oracle/corpus" || row.Category != "phase1" || row.PageCount != 1 || row.PassCount != 0 || row.FailCount != 1 || row.Missing != 1 || row.DiffCount != 1 {
		t.Fatalf("fields scoreboard row = %#v", row)
	}
}

func TestVisualforceDiffReturnsZeroForMatchingReports(t *testing.T) {
	dir := t.TempDir()
	salesforce := filepath.Join(dir, "salesforce.json")
	local := filepath.Join(dir, "local.json")
	report := `{"pages":[{"name":"Core","html":{"status":"pass","bytes":4,"sha256":"abcd","body":"Core"}}]}`
	writeVisualforceCommandTestFile(t, salesforce, report)
	writeVisualforceCommandTestFile(t, local, report)

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"visualforce", "diff", "--salesforce", salesforce, "--local", local}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run returned %d, stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "visualforce diff: no differences") {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestVisualforceSummaryPrintsCorpusFriendlyFixtureCounts(t *testing.T) {
	root := t.TempDir()
	writeVisualforceCommandTestFile(t, filepath.Join(root, "visualforce-probe-index.json"), `{
  "summary": {
    "pageCount": 2,
    "groupCount": 2,
    "owners": ["oracle/corpus"],
    "categories": ["phase1", "broad-corpus"]
  },
  "groups": [
    {"name": "lifecycle", "owner": "oracle/corpus", "category": "phase1", "pages": ["ProbeLifecycleBasic"]},
    {"name": "security/errors", "owner": "oracle/corpus", "category": "broad-corpus", "pages": ["ProbeSecurityMessages"]}
  ],
  "pages": [
    {"name": "ProbeLifecycleBasic", "group": "lifecycle", "owner": "oracle/corpus", "category": "phase1"},
    {"name": "ProbeSecurityMessages", "group": "security/errors", "owner": "oracle/corpus", "category": "broad-corpus"}
  ]
}`)

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"visualforce", "summary", "--project", root, "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run returned %d, stderr=%s", code, stderr.String())
	}
	var payload struct {
		Project        string         `json:"project"`
		PageCount      int            `json:"pageCount"`
		GroupCount     int            `json:"groupCount"`
		OwnerCounts    map[string]int `json:"ownerCounts"`
		CategoryCounts map[string]int `json:"categoryCounts"`
		Groups         []struct {
			Name      string `json:"name"`
			Owner     string `json:"owner"`
			Category  string `json:"category"`
			PageCount int    `json:"pageCount"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("json = %s; err = %v", stdout.String(), err)
	}
	if payload.Project != root || payload.PageCount != 2 || payload.GroupCount != 2 {
		t.Fatalf("summary = %#v", payload)
	}
	if payload.OwnerCounts["oracle/corpus"] != 2 {
		t.Fatalf("ownerCounts = %#v", payload.OwnerCounts)
	}
	if payload.CategoryCounts["phase1"] != 1 || payload.CategoryCounts["broad-corpus"] != 1 {
		t.Fatalf("categoryCounts = %#v", payload.CategoryCounts)
	}
	if len(payload.Groups) != 2 || payload.Groups[0].Name != "lifecycle" || payload.Groups[0].PageCount != 1 {
		t.Fatalf("groups = %#v", payload.Groups)
	}
}

func TestRootPluginManifestListsVisualforce(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Commands []struct {
			Path []string `json:"path"`
		} `json:"commands"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	for _, command := range manifest.Commands {
		if len(command.Path) == 1 && command.Path[0] == "visualforce" {
			return
		}
	}
	t.Fatalf("root plugin manifest omitted visualforce: %s", data)
}

func writeVisualforceCommandTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func strconvQuote(s string) string {
	data, _ := json.Marshal(s)
	return string(data)
}

func runFakeGladeDevVF() {
	args := os.Args[1:]
	if len(args) >= 3 && args[0] == "dev" && args[1] == "vf" && args[2] == "--help" {
		if os.Getenv("GLADE_TOOLS_FAKE_GLADE_VF_NO_READY") == "1" {
			fmt.Println("usage: glade dev vf --project <root> --addr <addr>")
			os.Exit(0)
		}
		fmt.Println("usage: glade dev vf --project <root> --addr <addr> --ready-file <path>")
		os.Exit(0)
	}
	if len(args) < 2 || args[0] != "dev" || args[1] != "vf" {
		fmt.Fprintf(os.Stderr, "unexpected fake glade args: %v\n", args)
		os.Exit(2)
	}
	addr := ""
	readyFile := ""
	for i := 2; i < len(args); i++ {
		switch args[i] {
		case "--addr":
			i++
			addr = args[i]
		case "--ready-file":
			i++
			readyFile = args[i]
		case "--project":
			i++
		}
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/apex/", func(w http.ResponseWriter, r *http.Request) {
		page := strings.TrimPrefix(r.URL.Path, "/apex/")
		if os.Getenv("GLADE_TOOLS_FAKE_GLADE_VF_PDF") == "1" && r.URL.Query().Get("renderAs") == "pdf" {
			w.Header().Set("Content-Type", "application/pdf")
			fmt.Fprintf(w, "%%PDF-1.4\n%s local pdf\n", page)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, "<html><body><h1>%s</h1><p>local render</p></body></html>", page)
	})
	server := &http.Server{Handler: mux}
	go func() {
		_ = server.Serve(ln)
	}()
	if readyFile != "" {
		_ = os.WriteFile(readyFile, []byte("ready"), 0o644)
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	<-signals
	if path := os.Getenv("GLADE_TOOLS_FAKE_GLADE_VF_TERM"); path != "" {
		_ = os.WriteFile(path, []byte("terminated"), 0o644)
	}
	_ = server.Close()
	os.Exit(0)
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}
