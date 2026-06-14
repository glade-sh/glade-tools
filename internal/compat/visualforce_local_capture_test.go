package compat

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

func TestRunLocalVisualforceCaptureStartsGladeDevVFFetchesPagesAndStops(t *testing.T) {
	root := t.TempDir()
	pageDir := filepath.Join(root, "force-app", "main", "default", "pages")
	if err := os.MkdirAll(pageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pageDir, "Core.page"), []byte(`<apex:page>Core</apex:page>`), 0o644); err != nil {
		t.Fatal(err)
	}

	argsFile := filepath.Join(t.TempDir(), "args.txt")
	termFile := filepath.Join(t.TempDir(), "terminated.txt")
	t.Setenv("GLADE_TOOLS_FAKE_GLADE_VF", "1")
	t.Setenv("GLADE_TOOLS_FAKE_GLADE_VF_PDF", "1")
	t.Setenv("GLADE_TOOLS_FAKE_GLADE_VF_ARGS", argsFile)
	t.Setenv("GLADE_TOOLS_FAKE_GLADE_VF_TERM", termFile)

	report, err := RunLocalVisualforceCapture(context.Background(), LocalVisualforceCaptureOptions{
		GladeBin: os.Args[0],
		Project:  root,
		Pages:    []string{"Core"},
		Now:      func() time.Time { return time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK {
		t.Fatalf("report not ok: %#v", report)
	}
	if report.Project != root || report.SourceDir != filepath.Join(root, "force-app") {
		t.Fatalf("project metadata = %#v", report)
	}
	if report.Probe.Command == "" || !strings.Contains(report.Probe.Command, "dev vf") {
		t.Fatalf("probe command = %#v", report.Probe)
	}
	if len(report.Pages) != 1 {
		t.Fatalf("pages = %#v", report.Pages)
	}
	page := report.Pages[0]
	if page.Name != "Core" || len(page.Raw) != 2 {
		t.Fatalf("page = %#v", page)
	}
	raw := page.Raw[0]
	if raw.Request.Path != "/apex/Core" || raw.StatusCode != http.StatusOK {
		t.Fatalf("raw capture = %#v", raw)
	}
	body := []byte("<html><body><h1>Core</h1><p>local render</p></body></html>")
	sum := sha256.Sum256(body)
	if page.HTML.Status != "pass" || page.HTML.ContentType != "text/html; charset=utf-8" {
		t.Fatalf("html metadata = %#v", page.HTML)
	}
	if page.HTML.Body != string(body) || page.HTML.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("html body/hash = %#v", page.HTML)
	}
	if page.HTML.Text != "Core local render" || page.HTML.TextHash == "" {
		t.Fatalf("html text = %#v", page.HTML)
	}
	argsData, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	argsText := string(argsData)
	for _, want := range []string{"dev vf", "--project " + root, "--addr 127.0.0.1:", "--ready-file "} {
		if !strings.Contains(argsText, want) {
			t.Fatalf("subprocess args omitted %q: %s", want, argsText)
		}
	}
	waitForFile(t, termFile)
}

func TestRunLocalVisualforceCaptureCollectsPDFWhenLocalRouteReturnsPDF(t *testing.T) {
	root := t.TempDir()
	pageDir := filepath.Join(root, "force-app", "main", "default", "pages")
	if err := os.MkdirAll(pageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pageDir, "Invoice.page"), []byte(`<apex:page>Invoice</apex:page>`), 0o644); err != nil {
		t.Fatal(err)
	}

	termFile := filepath.Join(t.TempDir(), "terminated.txt")
	t.Setenv("GLADE_TOOLS_FAKE_GLADE_VF", "1")
	t.Setenv("GLADE_TOOLS_FAKE_GLADE_VF_PDF", "1")
	t.Setenv("GLADE_TOOLS_FAKE_GLADE_VF_TERM", termFile)

	report, err := RunLocalVisualforceCapture(context.Background(), LocalVisualforceCaptureOptions{
		GladeBin: os.Args[0],
		Project:  root,
		Pages:    []string{"Invoice"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK {
		t.Fatalf("report not ok: %#v", report)
	}
	if report.Counts.PDFPass != 1 || report.Counts.PDFFail != 0 {
		t.Fatalf("pdf counts = %#v", report.Counts)
	}
	page := report.Pages[0]
	pdf := []byte("%PDF-1.4\nInvoice local pdf\n")
	sum := sha256.Sum256(pdf)
	if page.PDF.Status != "pass" || page.PDF.ContentType != "application/pdf" {
		t.Fatalf("pdf metadata = %#v", page.PDF)
	}
	if page.PDF.Bytes != len(pdf) || page.PDF.SHA256 != hex.EncodeToString(sum[:]) || page.PDF.BodyHash != hex.EncodeToString(sum[:]) {
		t.Fatalf("pdf body/hash = %#v", page.PDF)
	}
	if page.PDF.Base64 == "" {
		t.Fatalf("pdf omitted base64 evidence: %#v", page.PDF)
	}
	if len(page.Raw) != 2 {
		t.Fatalf("raw captures = %#v", page.Raw)
	}
	if got := page.Raw[1]; got.Request.Path != "/apex/Invoice" || got.Request.Query != "renderAs=pdf" || got.PDFSHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("pdf raw capture = %#v", got)
	}
	waitForFile(t, termFile)
}

func TestRunLocalVisualforceCaptureCountsPDFNonPassAsFailure(t *testing.T) {
	root := t.TempDir()
	pageDir := filepath.Join(root, "force-app", "main", "default", "pages")
	if err := os.MkdirAll(pageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pageDir, "Core.page"), []byte(`<apex:page>Core</apex:page>`), 0o644); err != nil {
		t.Fatal(err)
	}

	termFile := filepath.Join(t.TempDir(), "terminated.txt")
	t.Setenv("GLADE_TOOLS_FAKE_GLADE_VF", "1")
	t.Setenv("GLADE_TOOLS_FAKE_GLADE_VF_TERM", termFile)

	report, err := RunLocalVisualforceCapture(context.Background(), LocalVisualforceCaptureOptions{
		GladeBin: os.Args[0],
		Project:  root,
		Pages:    []string{"Core"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.OK {
		t.Fatalf("expected report to fail when local PDF route is not PDF: %#v", report)
	}
	if report.Counts.HTMLPass != 1 || report.Counts.HTMLFail != 0 || report.Counts.PDFPass != 0 || report.Counts.PDFFail != 1 {
		t.Fatalf("counts = %#v", report.Counts)
	}
	if report.Counts.PDFNotCaptured != 1 {
		t.Fatalf("not captured counts = %#v", report.Counts)
	}
	if got := report.Pages[0].PDF; got.Status != "notCaptured" || !strings.Contains(got.Error, "local PDF route did not return PDF") {
		t.Fatalf("pdf capture = %#v", got)
	}
	waitForFile(t, termFile)
}

func TestRunLocalVisualforceCaptureTreatsRenderAsPDFPageAsPDFOnly(t *testing.T) {
	root := t.TempDir()
	pageDir := filepath.Join(root, "force-app", "main", "default", "pages")
	if err := os.MkdirAll(pageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pageDir, "Invoice.page"), []byte(`<apex:page renderAs="pdf">Invoice</apex:page>`), 0o644); err != nil {
		t.Fatal(err)
	}

	termFile := filepath.Join(t.TempDir(), "terminated.txt")
	t.Setenv("GLADE_TOOLS_FAKE_GLADE_VF", "1")
	t.Setenv("GLADE_TOOLS_FAKE_GLADE_VF_PDF", "1")
	t.Setenv("GLADE_TOOLS_FAKE_GLADE_VF_PDF_PAGE", "1")
	t.Setenv("GLADE_TOOLS_FAKE_GLADE_VF_TERM", termFile)

	report, err := RunLocalVisualforceCapture(context.Background(), LocalVisualforceCaptureOptions{
		GladeBin: os.Args[0],
		Project:  root,
		Pages:    []string{"Invoice"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK {
		t.Fatalf("report not ok: %#v", report)
	}
	if report.Counts.HTMLFail != 0 || report.Counts.HTMLPass != 0 || report.Counts.PDFPass != 1 {
		t.Fatalf("counts = %#v", report.Counts)
	}
	if report.Counts.HTMLNotCaptured != 1 || report.Counts.PDFNotCaptured != 0 {
		t.Fatalf("not captured counts = %#v", report.Counts)
	}
	page := report.Pages[0]
	if page.HTML.Status != "notCaptured" || !strings.Contains(page.HTML.Error, "page route returned PDF") {
		t.Fatalf("html capture = %#v", page.HTML)
	}
	if page.PDF.Status != "pass" {
		t.Fatalf("pdf capture = %#v", page.PDF)
	}
	waitForFile(t, termFile)
}

func TestRunLocalVisualforceCaptureDiscoversPagesAndWritesJSON(t *testing.T) {
	root := t.TempDir()
	pageDir := filepath.Join(root, "force-app", "main", "default", "pages")
	if err := os.MkdirAll(pageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Beta", "Alpha"} {
		if err := os.WriteFile(filepath.Join(pageDir, name+".page"), []byte(`<apex:page/>`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out := filepath.Join(root, "capture.json")
	t.Setenv("GLADE_TOOLS_FAKE_GLADE_VF", "1")
	t.Setenv("GLADE_TOOLS_FAKE_GLADE_VF_PDF", "1")
	t.Setenv("GLADE_TOOLS_FAKE_GLADE_VF_TERM", filepath.Join(t.TempDir(), "terminated.txt"))

	report, err := RunLocalVisualforceCapture(context.Background(), LocalVisualforceCaptureOptions{
		GladeBin: os.Args[0],
		Project:  root,
		Out:      out,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{report.Pages[0].Name, report.Pages[1].Name}; strings.Join(got, ",") != "Alpha,Beta" {
		t.Fatalf("pages = %v", got)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"project": "`+root+`"`) || !strings.Contains(string(data), `"body": "<html><body><h1>Alpha</h1><p>local render</p></body></html>"`) {
		t.Fatalf("capture json = %s", string(data))
	}
}

func TestRunLocalVisualforceCapturePollsHTTPWithoutReadyFileSupport(t *testing.T) {
	root := t.TempDir()
	argsFile := filepath.Join(t.TempDir(), "args.txt")
	termFile := filepath.Join(t.TempDir(), "terminated.txt")
	t.Setenv("GLADE_TOOLS_FAKE_GLADE_VF", "1")
	t.Setenv("GLADE_TOOLS_FAKE_GLADE_VF_PDF", "1")
	t.Setenv("GLADE_TOOLS_FAKE_GLADE_VF_NO_READY", "1")
	t.Setenv("GLADE_TOOLS_FAKE_GLADE_VF_ARGS", argsFile)
	t.Setenv("GLADE_TOOLS_FAKE_GLADE_VF_TERM", termFile)

	report, err := RunLocalVisualforceCapture(context.Background(), LocalVisualforceCaptureOptions{
		GladeBin: os.Args[0],
		Project:  root,
		Pages:    []string{"Core"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK || report.Counts.HTMLPass != 1 {
		t.Fatalf("report = %#v", report)
	}
	argsData, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(argsData), "--ready-file") {
		t.Fatalf("subprocess args included --ready-file despite unsupported help: %s", string(argsData))
	}
	waitForFile(t, termFile)
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
	if path := os.Getenv("GLADE_TOOLS_FAKE_GLADE_VF_ARGS"); path != "" {
		_ = os.WriteFile(path, []byte(strings.Join(args, " ")), 0o644)
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/apex/", func(w http.ResponseWriter, r *http.Request) {
		page := strings.TrimPrefix(r.URL.Path, "/apex/")
		if os.Getenv("GLADE_TOOLS_FAKE_GLADE_VF_PDF_PAGE") == "1" || os.Getenv("GLADE_TOOLS_FAKE_GLADE_VF_PDF") == "1" && r.URL.Query().Get("renderAs") == "pdf" {
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
