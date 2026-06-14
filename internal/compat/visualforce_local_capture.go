package compat

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type LocalVisualforceCaptureOptions struct {
	GladeBin   string
	Project    string
	Pages      []string
	Out        string
	Now        func() time.Time
	HTTPClient VisualforceHTTPClient
}

func RunLocalVisualforceCapture(ctx context.Context, options LocalVisualforceCaptureOptions) (VisualforceCaptureReport, error) {
	if err := ctx.Err(); err != nil {
		return VisualforceCaptureReport{}, err
	}
	options.GladeBin = strings.TrimSpace(options.GladeBin)
	if options.GladeBin == "" {
		return VisualforceCaptureReport{}, errors.New("--glade-bin is required")
	}
	if options.Project == "" {
		options.Project = "."
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.HTTPClient == nil {
		options.HTTPClient = defaultVisualforceHTTPClient()
	}
	absProject, err := filepath.Abs(options.Project)
	if err != nil {
		return VisualforceCaptureReport{}, err
	}
	sourceDir := visualforceCaptureSourceDir(absProject)
	pages := normalizeVisualforceCapturePages(options.Pages)
	if len(pages) == 0 {
		pages, err = discoverVisualforcePages(sourceDir)
		if err != nil {
			return VisualforceCaptureReport{}, err
		}
	}
	if len(pages) == 0 {
		return VisualforceCaptureReport{}, errors.New("no Visualforce pages found; pass --pages or add pages under the project source")
	}
	for _, page := range pages {
		if !isApexIdentifier(page) {
			return VisualforceCaptureReport{}, fmt.Errorf("Visualforce page %q is not a valid Apex Page identifier", page)
		}
	}
	pageMetadata, err := loadVisualforceProbePageMetadata(absProject)
	if err != nil {
		return VisualforceCaptureReport{}, err
	}

	report := VisualforceCaptureReport{
		OK:         true,
		Project:    absProject,
		SourceDir:  sourceDir,
		CapturedAt: options.Now().UTC().Format(time.RFC3339),
	}
	addr, err := reserveLocalVisualforceAddr()
	if err != nil {
		return report, err
	}
	readyFile := ""
	args := []string{"dev", "vf", "--project", absProject, "--addr", addr}
	if gladeDevVFSupportsReadyFile(ctx, options.GladeBin) {
		file, err := os.CreateTemp("", "glade-vf-ready-*")
		if err != nil {
			return report, err
		}
		readyFile = file.Name()
		_ = file.Close()
		_ = os.Remove(readyFile)
		defer os.Remove(readyFile)
		args = append(args, "--ready-file", readyFile)
	}

	cmdCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, options.GladeBin, args...)
	var processOutput bytes.Buffer
	cmd.Stdout = &processOutput
	cmd.Stderr = &processOutput
	if err := cmd.Start(); err != nil {
		return report, err
	}
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	defer stopLocalVisualforceProcess(cmd, done)

	report.Probe = VisualforceCommandResult{
		Ran:     true,
		Command: options.GladeBin + " " + strings.Join(args, " "),
	}
	baseURL := "http://" + addr
	if err := waitForLocalVisualforce(ctx, options.HTTPClient, baseURL, pages[0], readyFile, done, &processOutput); err != nil {
		report.Probe.Error = err.Error()
		report.OK = false
		return report, err
	}
	report.Probe.OK = true

	for _, page := range pages {
		pageReport := VisualforcePageCapture{Name: page}
		applyVisualforceProbePageMetadata(&pageReport, pageMetadata)
		raw, body, err := fetchLocalVisualforceRawCapture(ctx, options.HTTPClient, baseURL, page)
		if err != nil {
			raw = VisualforceRawCapture{Page: page, Status: "fail", Error: err.Error()}
		}
		pageReport.Raw = append(pageReport.Raw, raw)
		pageReport.HTML = localVisualforceHTMLCapture(raw, body)
		pdfRaw, pdfBody, err := fetchLocalVisualforcePDFCapture(ctx, options.HTTPClient, baseURL, page)
		if err != nil {
			pdfRaw = VisualforceRawCapture{Page: page, Status: "fail", Error: err.Error()}
		}
		pageReport.Raw = append(pageReport.Raw, pdfRaw)
		pageReport.PDF = localVisualforcePDFCapture(pdfRaw, pdfBody)
		report.Pages = append(report.Pages, pageReport)
		accumulateVisualforceCaptureCounts(&report.Counts, pageReport)
	}
	report.Counts.Pages = len(report.Pages)
	report.OK = report.Probe.OK && report.Counts.HTMLFail == 0 && report.Counts.PDFFail == 0
	if options.Out != "" {
		if err := writeVisualforceCaptureReport(options.Out, report); err != nil {
			return report, err
		}
	}
	return report, nil
}

func reserveLocalVisualforceAddr() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		return "", err
	}
	return addr, nil
}

func gladeDevVFSupportsReadyFile(ctx context.Context, gladeBin string) bool {
	helpCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(helpCtx, gladeBin, "dev", "vf", "--help")
	output, err := cmd.CombinedOutput()
	return err == nil && bytes.Contains(output, []byte("--ready-file"))
}

func waitForLocalVisualforce(ctx context.Context, client VisualforceHTTPClient, baseURL, page, readyFile string, done <-chan error, output *bytes.Buffer) error {
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case err := <-done:
			if err == nil {
				return fmt.Errorf("glade dev vf exited before capture: %s", trimCommandOutput(output.Bytes()))
			}
			return fmt.Errorf("glade dev vf exited before capture: %w: %s", err, trimCommandOutput(output.Bytes()))
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("timed out waiting for glade dev vf at %s", baseURL)
		case <-tick.C:
			if readyFile != "" {
				if _, err := os.Stat(readyFile); err == nil {
					return nil
				}
				continue
			}
			if localVisualforceHTTPReady(ctx, client, baseURL, page) {
				return nil
			}
		}
	}
}

func localVisualforceHTTPReady(ctx context.Context, client VisualforceHTTPClient, baseURL, page string) bool {
	endpoint, err := localVisualforcePageURL(baseURL, page)
	if err != nil {
		return false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return resp.StatusCode < 500
}

func fetchLocalVisualforceRawCapture(ctx context.Context, client VisualforceHTTPClient, baseURL, page string) (VisualforceRawCapture, []byte, error) {
	endpoint, err := localVisualforcePageURL(baseURL, page)
	if err != nil {
		return VisualforceRawCapture{}, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return VisualforceRawCapture{}, nil, err
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml;q=0.9,*/*;q=0.1")
	resp, err := client.Do(req)
	if err != nil {
		return VisualforceRawCapture{
			Page: page,
			Request: VisualforceRawCaptureRequest{
				Page:   page,
				Method: req.Method,
				Path:   req.URL.EscapedPath(),
				Query:  req.URL.RawQuery,
			},
			Status: "fail",
			Error:  err.Error(),
		}, nil, nil
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return VisualforceRawCapture{}, nil, err
	}
	raw, err := buildVisualforceRawCaptureFromBody(page, endpoint, resp, body)
	return raw, body, err
}

func fetchLocalVisualforcePDFCapture(ctx context.Context, client VisualforceHTTPClient, baseURL, page string) (VisualforceRawCapture, []byte, error) {
	endpoint, err := localVisualforcePDFURL(baseURL, page)
	if err != nil {
		return VisualforceRawCapture{}, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return VisualforceRawCapture{}, nil, err
	}
	req.Header.Set("Accept", "application/pdf,*/*;q=0.1")
	resp, err := client.Do(req)
	if err != nil {
		return VisualforceRawCapture{
			Page: page,
			Request: VisualforceRawCaptureRequest{
				Page:   page,
				Method: req.Method,
				Path:   req.URL.EscapedPath(),
				Query:  req.URL.RawQuery,
			},
			Status: "fail",
			Error:  err.Error(),
		}, nil, nil
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return VisualforceRawCapture{}, nil, err
	}
	raw, err := buildVisualforceRawCaptureFromBody(page, endpoint, resp, body)
	return raw, body, err
}

func localVisualforcePageURL(baseURL, page string) (string, error) {
	base, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil {
		return "", err
	}
	if base.Scheme == "" || base.Host == "" {
		return "", fmt.Errorf("invalid local Visualforce URL %q", baseURL)
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/apex/" + url.PathEscape(page)
	base.RawQuery = ""
	base.Fragment = ""
	return base.String(), nil
}

func localVisualforcePDFURL(baseURL, page string) (string, error) {
	endpoint, err := localVisualforcePageURL(baseURL, page)
	if err != nil {
		return "", err
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	query := parsed.Query()
	query.Set("renderAs", "pdf")
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func localVisualforceHTMLCapture(raw VisualforceRawCapture, body []byte) VisualforceRenderedCapture {
	if raw.Status != "pass" {
		return VisualforceRenderedCapture{Status: "fail", Error: raw.Error}
	}
	if raw.StatusCode >= 400 {
		return VisualforceRenderedCapture{Status: "fail", ContentType: raw.ContentType, RedirectLocation: raw.RedirectLocation, Headers: raw.Headers, Bytes: raw.BodyBytes, BodyHash: raw.BodySHA256, Error: raw.Status}
	}
	if raw.PDFSHA256 != "" {
		return VisualforceRenderedCapture{Status: "notCaptured", ContentType: raw.ContentType, RedirectLocation: raw.RedirectLocation, Headers: raw.Headers, Bytes: raw.BodyBytes, BodyHash: raw.BodySHA256, Error: "local page route returned PDF"}
	}
	if raw.HTMLSHA256 == "" {
		return VisualforceRenderedCapture{Status: "fail", ContentType: raw.ContentType, RedirectLocation: raw.RedirectLocation, Headers: raw.Headers, Bytes: raw.BodyBytes, BodyHash: raw.BodySHA256, Error: "local response was not HTML"}
	}
	capture := VisualforceRenderedCapture{
		Status:           "pass",
		ContentType:      raw.ContentType,
		RedirectLocation: raw.RedirectLocation,
		Headers:          raw.Headers,
		Bytes:            raw.BodyBytes,
		SHA256:           raw.HTMLSHA256,
		BodyHash:         raw.BodySHA256,
		Text:             raw.NormalizedText,
		Body:             string(body),
	}
	if raw.NormalizedText != "" {
		setVisualforceRenderedNormalizedText(&capture, raw.NormalizedText)
	}
	return capture
}

func localVisualforcePDFCapture(raw VisualforceRawCapture, body []byte) VisualforceRenderedCapture {
	if raw.Status != "pass" {
		return VisualforceRenderedCapture{Status: "notCaptured", Error: raw.Error}
	}
	if raw.StatusCode >= 400 {
		return VisualforceRenderedCapture{Status: "notCaptured", ContentType: raw.ContentType, RedirectLocation: raw.RedirectLocation, Headers: raw.Headers, Bytes: raw.BodyBytes, BodyHash: raw.BodySHA256, Error: raw.Status}
	}
	if raw.PDFSHA256 == "" {
		return VisualforceRenderedCapture{Status: "notCaptured", ContentType: raw.ContentType, RedirectLocation: raw.RedirectLocation, Headers: raw.Headers, Bytes: raw.BodyBytes, BodyHash: raw.BodySHA256, Error: "local PDF route did not return PDF"}
	}
	capture := VisualforceRenderedCapture{
		Status:           "pass",
		ContentType:      raw.ContentType,
		RedirectLocation: raw.RedirectLocation,
		Headers:          raw.Headers,
		Bytes:            raw.BodyBytes,
		SHA256:           raw.PDFSHA256,
		BodyHash:         raw.BodySHA256,
		Base64:           base64.StdEncoding.EncodeToString(body),
	}
	setVisualforceRenderedPDFText(&capture, body)
	return capture
}

func buildVisualforceRawCaptureFromBody(page, requestURL string, resp *http.Response, body []byte) (VisualforceRawCapture, error) {
	parsed, err := url.Parse(requestURL)
	if err != nil {
		return VisualforceRawCapture{}, err
	}
	bodySum := sha256.Sum256(body)
	contentType := firstHeaderValue(resp.Header, "Content-Type")
	capture := VisualforceRawCapture{
		Page: page,
		Request: VisualforceRawCaptureRequest{
			Page:   page,
			Method: http.MethodGet,
			Path:   parsed.EscapedPath(),
			Query:  parsed.RawQuery,
		},
		Status:           "pass",
		StatusCode:       resp.StatusCode,
		ContentType:      contentType,
		RedirectLocation: firstHeaderValue(resp.Header, "Location"),
		Headers:          selectedVisualforceResponseHeaders(resp.Header),
		BodyBytes:        len(body),
		BodySHA256:       hex.EncodeToString(bodySum[:]),
	}
	if isVisualforcePDFResponse(contentType, body) {
		capture.PDFSHA256 = capture.BodySHA256
		return capture, nil
	}
	if isVisualforceHTMLResponse(contentType, body) {
		capture.HTMLSHA256 = capture.BodySHA256
		capture.NormalizedText = normalizeVisualforceRawText(body)
	}
	return capture, nil
}

func stopLocalVisualforceProcess(cmd *exec.Cmd, done <-chan error) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	if cmd.ProcessState != nil {
		return
	}
	select {
	case <-done:
		return
	default:
	}
	_ = cmd.Process.Signal(os.Interrupt)
	select {
	case <-done:
		return
	case <-time.After(2 * time.Second):
	}
	_ = cmd.Process.Kill()
	<-done
}
