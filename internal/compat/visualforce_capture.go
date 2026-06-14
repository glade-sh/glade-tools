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
	"fmt"
	"html"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type VisualforceCaptureOptions struct {
	TargetOrg  string
	Project    string
	Pages      []string
	Out        string
	SkipDeploy bool
	BatchSize  int
	Now        func() time.Time
	Runner     VisualforceCommandRunner
	HTTPClient VisualforceHTTPClient
}

const defaultVisualforceCaptureBatchSize = 5

type VisualforceCommandRunner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type visualforceCommandRunnerWithDir interface {
	RunInDir(ctx context.Context, dir, name string, args ...string) ([]byte, error)
}

type VisualforceHTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

type VisualforceCaptureReport struct {
	OK          bool                     `json:"ok"`
	TargetOrg   string                   `json:"targetOrg"`
	Username    string                   `json:"username,omitempty"`
	OrgID       string                   `json:"orgId,omitempty"`
	InstanceURL string                   `json:"instanceUrl,omitempty"`
	Project     string                   `json:"project"`
	SourceDir   string                   `json:"sourceDir,omitempty"`
	CapturedAt  string                   `json:"capturedAt"`
	Deploy      VisualforceCommandResult `json:"deploy"`
	Probe       VisualforceCommandResult `json:"probe"`
	Pages       []VisualforcePageCapture `json:"pages"`
	Counts      VisualforceCaptureCounts `json:"counts"`
}

type VisualforceCommandResult struct {
	Ran     bool   `json:"ran"`
	OK      bool   `json:"ok"`
	Command string `json:"command,omitempty"`
	Error   string `json:"error,omitempty"`
}

type VisualforcePageCapture struct {
	Name     string                     `json:"name"`
	Group    string                     `json:"group,omitempty"`
	Owner    string                     `json:"owner,omitempty"`
	Category string                     `json:"category,omitempty"`
	HTML     VisualforceRenderedCapture `json:"html"`
	PDF      VisualforceRenderedCapture `json:"pdf"`
	Raw      []VisualforceRawCapture    `json:"raw,omitempty"`
}

type VisualforceRenderedCapture struct {
	Status           string            `json:"status"`
	ContentType      string            `json:"contentType,omitempty"`
	RedirectLocation string            `json:"redirectLocation,omitempty"`
	Headers          map[string]string `json:"headers,omitempty"`
	Bytes            int               `json:"bytes,omitempty"`
	SHA256           string            `json:"sha256,omitempty"`
	BodyHash         string            `json:"bodyHash,omitempty"`
	TextHash         string            `json:"textHash,omitempty"`
	Text             string            `json:"text,omitempty"`
	NormalizedText   string            `json:"normalizedText,omitempty"`
	ContractText     string            `json:"contractText,omitempty"`
	Body             string            `json:"body,omitempty"`
	Base64           string            `json:"base64,omitempty"`
	Error            string            `json:"error,omitempty"`
}

type VisualforceProbeIndex struct {
	Summary VisualforceProbeIndexSummary `json:"summary,omitempty"`
	Groups  []VisualforceProbeGroup      `json:"groups"`
	Pages   []VisualforceProbePage       `json:"pages"`
}

type VisualforceProbeIndexSummary struct {
	PageCount  int      `json:"pageCount"`
	GroupCount int      `json:"groupCount"`
	Owners     []string `json:"owners,omitempty"`
	Categories []string `json:"categories,omitempty"`
}

type VisualforceProbeGroup struct {
	Name     string   `json:"name"`
	Owner    string   `json:"owner,omitempty"`
	Category string   `json:"category,omitempty"`
	Pages    []string `json:"pages"`
}

type VisualforceProbePage struct {
	Name     string `json:"name"`
	Group    string `json:"group"`
	Owner    string `json:"owner,omitempty"`
	Category string `json:"category,omitempty"`
}

type VisualforceProbeCorpusSummary struct {
	Project        string                         `json:"project"`
	PageCount      int                            `json:"pageCount"`
	GroupCount     int                            `json:"groupCount"`
	OwnerCounts    map[string]int                 `json:"ownerCounts"`
	CategoryCounts map[string]int                 `json:"categoryCounts"`
	Groups         []VisualforceProbeGroupSummary `json:"groups"`
}

type VisualforceProbeGroupSummary struct {
	Name      string `json:"name"`
	Owner     string `json:"owner,omitempty"`
	Category  string `json:"category,omitempty"`
	PageCount int    `json:"pageCount"`
}

type VisualforceCaptureCounts struct {
	Pages           int `json:"pages"`
	HTMLPass        int `json:"htmlPass"`
	HTMLFail        int `json:"htmlFail"`
	HTMLNotCaptured int `json:"htmlNotCaptured"`
	PDFPass         int `json:"pdfPass"`
	PDFFail         int `json:"pdfFail"`
	PDFNotCaptured  int `json:"pdfNotCaptured"`
}

type VisualforceRawCapture struct {
	Page             string                       `json:"page"`
	Request          VisualforceRawCaptureRequest `json:"request"`
	Status           string                       `json:"status"`
	StatusCode       int                          `json:"statusCode,omitempty"`
	ContentType      string                       `json:"contentType,omitempty"`
	RedirectLocation string                       `json:"redirectLocation,omitempty"`
	Headers          map[string]string            `json:"headers,omitempty"`
	BodyBytes        int                          `json:"bodyBytes,omitempty"`
	BodySHA256       string                       `json:"bodySha256,omitempty"`
	HTMLSHA256       string                       `json:"htmlSha256,omitempty"`
	PDFSHA256        string                       `json:"pdfSha256,omitempty"`
	NormalizedText   string                       `json:"normalizedText,omitempty"`
	Error            string                       `json:"error,omitempty"`
}

type VisualforceRawCaptureRequest struct {
	Page   string `json:"page"`
	Method string `json:"method"`
	Path   string `json:"path"`
	Query  string `json:"query,omitempty"`
}

type shellVisualforceCommandRunner struct{}

func (shellVisualforceCommandRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.CombinedOutput()
}

func (shellVisualforceCommandRunner) RunInDir(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PWD="+dir)
	return cmd.CombinedOutput()
}

func defaultVisualforceHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func RunVisualforceCapture(ctx context.Context, options VisualforceCaptureOptions) (VisualforceCaptureReport, error) {
	if err := ctx.Err(); err != nil {
		return VisualforceCaptureReport{}, err
	}
	options.TargetOrg = strings.TrimSpace(options.TargetOrg)
	if options.TargetOrg == "" {
		return VisualforceCaptureReport{}, errors.New("--target-org is required")
	}
	if options.Project == "" {
		options.Project = "."
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Runner == nil {
		options.Runner = shellVisualforceCommandRunner{}
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
	batchSize := options.BatchSize
	if batchSize <= 0 {
		batchSize = defaultVisualforceCaptureBatchSize
	}
	pageMetadata, err := loadVisualforceProbePageMetadata(absProject)
	if err != nil {
		return VisualforceCaptureReport{}, err
	}

	report := VisualforceCaptureReport{
		OK:         true,
		TargetOrg:  options.TargetOrg,
		Project:    absProject,
		SourceDir:  sourceDir,
		CapturedAt: options.Now().UTC().Format(time.RFC3339),
	}
	orgOutput, err := options.Runner.Run(ctx, "sf", "org", "display", "--target-org", options.TargetOrg, "--verbose", "--json")
	if err != nil {
		return report, fmt.Errorf("sf org display failed: %w: %s", err, trimCommandOutput(orgOutput))
	}
	orgIdentity := parseVisualforceOrgIdentity(orgOutput)
	report.Username = orgIdentity.Username
	report.OrgID = orgIdentity.OrgID
	report.InstanceURL = orgIdentity.InstanceURL

	if !options.SkipDeploy {
		report.Deploy.Ran = true
		report.Deploy.Command = "sf project deploy start"
		deploySourceDir := sourceDir
		if rel, relErr := filepath.Rel(absProject, sourceDir); relErr == nil && rel != "." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			deploySourceDir = rel
		}
		deployOutput, err := runVisualforceCommandInDir(ctx, options.Runner, absProject, "sf", "project", "deploy", "start", "--target-org", options.TargetOrg, "--source-dir", deploySourceDir, "--ignore-conflicts", "--json")
		report.Deploy.OK = err == nil && sfJSONStatusOK(deployOutput)
		if err != nil {
			report.Deploy.Error = trimCommandOutput(deployOutput)
			report.OK = false
			return report, fmt.Errorf("sf project deploy start failed: %w: %s", err, report.Deploy.Error)
		}
		if !report.Deploy.OK {
			report.Deploy.Error = trimCommandOutput(deployOutput)
			report.OK = false
			return report, fmt.Errorf("sf project deploy start returned failure: %s", report.Deploy.Error)
		}
	}

	report.Probe.Ran = true
	report.Probe.Command = "sf apex run"
	report.Probe.OK = true
	captures := map[string]VisualforceRenderedCapture{}
	for _, batch := range chunkVisualforceCapturePages(pages, batchSize) {
		script, err := writeVisualforceProbeScript(batch)
		if err != nil {
			return report, err
		}
		probeOutput, err := runVisualforceCommandInDir(ctx, options.Runner, absProject, "sf", "apex", "run", "--target-org", options.TargetOrg, "--file", script, "--json")
		_ = os.Remove(script)
		batchOK := err == nil && sfJSONStatusOK(probeOutput)
		report.Probe.OK = report.Probe.OK && batchOK
		if !batchOK && err == nil && report.Probe.Error == "" {
			report.Probe.Error = trimCommandOutput(probeOutput)
		}
		if err != nil {
			report.Probe.Error = trimCommandOutput(probeOutput)
			report.OK = false
			return report, fmt.Errorf("sf apex run failed: %w: %s", err, report.Probe.Error)
		}
		for key, capture := range parseVisualforceProbeCaptures(probeOutput) {
			captures[key] = capture
		}
	}
	for _, page := range pages {
		pageReport := VisualforcePageCapture{Name: page}
		applyVisualforceProbePageMetadata(&pageReport, pageMetadata)
		pageReport.HTML = captures[visualforceCaptureKey(page, "HTML")]
		pageReport.PDF = captures[visualforceCaptureKey(page, "PDF")]
		if orgIdentity.InstanceURL != "" && orgIdentity.AccessToken != "" {
			pageReport.Raw = append(pageReport.Raw, fetchVisualforceRawCapture(ctx, options.HTTPClient, page, orgIdentity.InstanceURL, orgIdentity.AccessToken))
			pageReport.Raw = append(pageReport.Raw, fetchVisualforceRawPDFCapture(ctx, options.HTTPClient, page, orgIdentity.InstanceURL, orgIdentity.AccessToken))
		}
		if pageReport.HTML.Status == "" {
			pageReport.HTML = VisualforceRenderedCapture{Status: "missing", Error: "probe log did not contain an HTML capture"}
		}
		if pageReport.PDF.Status == "" {
			pageReport.PDF = VisualforceRenderedCapture{Status: "missing", Error: "probe log did not contain a PDF capture"}
		}
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

func chunkVisualforceCapturePages(pages []string, batchSize int) [][]string {
	if batchSize <= 0 {
		batchSize = defaultVisualforceCaptureBatchSize
	}
	var batches [][]string
	for start := 0; start < len(pages); start += batchSize {
		end := start + batchSize
		if end > len(pages) {
			end = len(pages)
		}
		batches = append(batches, pages[start:end])
	}
	return batches
}

type visualforceOrgIdentity struct {
	Username    string
	OrgID       string
	InstanceURL string
	AccessToken string
}

func runVisualforceCommandInDir(ctx context.Context, runner VisualforceCommandRunner, dir, name string, args ...string) ([]byte, error) {
	if runnerWithDir, ok := runner.(visualforceCommandRunnerWithDir); ok {
		return runnerWithDir.RunInDir(ctx, dir, name, args...)
	}
	return runner.Run(ctx, name, args...)
}

func fetchVisualforceRawCapture(ctx context.Context, client VisualforceHTTPClient, page, instanceURL, accessToken string) VisualforceRawCapture {
	endpoint, err := visualforcePageURL(instanceURL, page)
	if err != nil {
		return VisualforceRawCapture{Page: page, Status: "fail", Error: err.Error()}
	}
	return fetchVisualforceRawCaptureEndpoint(ctx, client, page, endpoint, accessToken, "text/html,application/pdf;q=0.9,*/*;q=0.1")
}

func fetchVisualforceRawPDFCapture(ctx context.Context, client VisualforceHTTPClient, page, instanceURL, accessToken string) VisualforceRawCapture {
	endpoint, err := visualforcePagePDFURL(instanceURL, page)
	if err != nil {
		return VisualforceRawCapture{Page: page, Status: "fail", Error: err.Error()}
	}
	return fetchVisualforceRawCaptureEndpoint(ctx, client, page, endpoint, accessToken, "application/pdf,text/html;q=0.9,*/*;q=0.1")
}

func fetchVisualforceRawCaptureEndpoint(ctx context.Context, client VisualforceHTTPClient, page, endpoint, accessToken, accept string) VisualforceRawCapture {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return VisualforceRawCapture{Page: page, Status: "fail", Error: err.Error()}
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", accept)
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
		}
	}
	capture, err := buildVisualforceRawCapture(page, endpoint, resp)
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
		}
	}
	return capture
}

func visualforcePagePDFURL(instanceURL, page string) (string, error) {
	endpoint, err := visualforcePageURL(instanceURL, page)
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

func visualforcePageURL(instanceURL, page string) (string, error) {
	base, err := url.Parse(strings.TrimRight(instanceURL, "/"))
	if err != nil {
		return "", err
	}
	if base.Scheme == "" || base.Host == "" {
		return "", fmt.Errorf("invalid Salesforce instance URL %q", instanceURL)
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/apex/" + url.PathEscape(page)
	base.RawQuery = ""
	base.Fragment = ""
	return base.String(), nil
}

func buildVisualforceRawCapture(page, requestURL string, resp *http.Response) (VisualforceRawCapture, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return VisualforceRawCapture{}, err
	}
	return buildVisualforceRawCaptureFromBody(page, requestURL, resp, body)
}

func ReadVisualforceProbeIndex(project string) (VisualforceProbeIndex, error) {
	path := filepath.Join(project, "visualforce-probe-index.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return VisualforceProbeIndex{}, err
	}
	var index VisualforceProbeIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return VisualforceProbeIndex{}, fmt.Errorf("%s: %w", path, err)
	}
	return index, nil
}

func SummarizeVisualforceProbeIndex(project string) (VisualforceProbeCorpusSummary, error) {
	absProject, err := filepath.Abs(project)
	if err != nil {
		return VisualforceProbeCorpusSummary{}, err
	}
	index, err := ReadVisualforceProbeIndex(absProject)
	if err != nil {
		return VisualforceProbeCorpusSummary{}, err
	}
	groupMetadata := visualforceProbeGroupsByName(index.Groups)
	summary := VisualforceProbeCorpusSummary{
		Project:        absProject,
		PageCount:      len(index.Pages),
		GroupCount:     len(index.Groups),
		OwnerCounts:    map[string]int{},
		CategoryCounts: map[string]int{},
	}
	for _, page := range index.Pages {
		page = fillVisualforceProbePageDefaults(page, groupMetadata[page.Group])
		if page.Owner != "" {
			summary.OwnerCounts[page.Owner]++
		}
		if page.Category != "" {
			summary.CategoryCounts[page.Category]++
		}
	}
	for _, group := range index.Groups {
		summary.Groups = append(summary.Groups, VisualforceProbeGroupSummary{
			Name:      group.Name,
			Owner:     group.Owner,
			Category:  group.Category,
			PageCount: len(group.Pages),
		})
	}
	return summary, nil
}

func loadVisualforceProbePageMetadata(project string) (map[string]VisualforceProbePage, error) {
	index, err := ReadVisualforceProbeIndex(project)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	groupMetadata := visualforceProbeGroupsByName(index.Groups)
	metadata := make(map[string]VisualforceProbePage, len(index.Pages))
	for _, page := range index.Pages {
		page = fillVisualforceProbePageDefaults(page, groupMetadata[page.Group])
		if page.Name != "" {
			metadata[page.Name] = page
		}
	}
	return metadata, nil
}

func visualforceProbeGroupsByName(groups []VisualforceProbeGroup) map[string]VisualforceProbeGroup {
	result := make(map[string]VisualforceProbeGroup, len(groups))
	for _, group := range groups {
		if group.Name != "" {
			result[group.Name] = group
		}
	}
	return result
}

func fillVisualforceProbePageDefaults(page VisualforceProbePage, group VisualforceProbeGroup) VisualforceProbePage {
	if page.Owner == "" {
		page.Owner = group.Owner
	}
	if page.Category == "" {
		page.Category = group.Category
	}
	return page
}

func applyVisualforceProbePageMetadata(capture *VisualforcePageCapture, metadata map[string]VisualforceProbePage) {
	if capture == nil || len(metadata) == 0 {
		return
	}
	page := metadata[capture.Name]
	capture.Group = page.Group
	capture.Owner = page.Owner
	capture.Category = page.Category
}

func setVisualforceRenderedBodyText(capture *VisualforceRenderedCapture, body []byte) {
	if !isVisualforceHTMLResponse("", body) {
		return
	}
	setVisualforceRenderedNormalizedText(capture, normalizeVisualforceRawText(body))
}

func setVisualforceRenderedPDFText(capture *VisualforceRenderedCapture, body []byte) {
	if !isVisualforcePDFResponse("", body) {
		return
	}
	setVisualforceRenderedNormalizedText(capture, normalizeVisualforcePDFText(body))
}

func setVisualforceRenderedNormalizedText(capture *VisualforceRenderedCapture, normalized string) {
	normalized = strings.TrimSpace(normalized)
	if normalized == "" {
		return
	}
	capture.Text = normalized
	capture.NormalizedText = normalized
	sum := sha256.Sum256([]byte(normalized))
	capture.TextHash = hex.EncodeToString(sum[:])
	contractText := normalizeVisualforceDiffContractText(normalized)
	if contractText != "" {
		capture.ContractText = contractText
	}
}

func visualforceCaptureSourceDir(project string) string {
	for _, candidate := range []string{
		filepath.Join(project, "force-app"),
		filepath.Join(project, "src"),
	} {
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
	}
	return project
}

func normalizeVisualforceCapturePages(pages []string) []string {
	seen := map[string]bool{}
	var result []string
	for _, raw := range pages {
		for _, part := range strings.Split(raw, ",") {
			name := strings.TrimSpace(part)
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			result = append(result, name)
		}
	}
	sort.Strings(result)
	return result
}

func discoverVisualforcePages(sourceDir string) ([]string, error) {
	var pages []string
	err := filepath.WalkDir(sourceDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", ".sf", ".sfdx", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".page" {
			return nil
		}
		pages = append(pages, strings.TrimSuffix(filepath.Base(path), ".page"))
		return nil
	})
	sort.Strings(pages)
	return pages, err
}

func writeVisualforceProbeScript(pages []string) (string, error) {
	var b strings.Builder
	b.WriteString("// Generated by glade-tools compat visualforce capture.\n")
	for _, page := range pages {
		if !isApexIdentifier(page) {
			return "", fmt.Errorf("Visualforce page %q is not a valid Apex Page identifier", page)
		}
		label := strings.ReplaceAll(page, "'", "\\'")
		fmt.Fprintf(&b, "try { Blob body = Page.%s.getContent(); String body64 = EncodingUtil.base64Encode(body); String digest = EncodingUtil.convertToHex(Crypto.generateDigest('SHA-256', body)); Integer chunks = (body64.length() + 2999) / 3000; for (Integer i = 0; i < chunks; i++) { Integer startAt = i * 3000; Integer endAt = Math.min(body64.length(), startAt + 3000); System.debug('GLADE_VF_CAPTURE_CHUNK|%s|HTML|OK|' + body.size() + '|' + digest + '|' + i + '|' + chunks + '|' + body64.substring(startAt, endAt)); } } catch (Exception e) { System.debug('GLADE_VF_CAPTURE|%s|HTML|ERROR|' + EncodingUtil.base64Encode(Blob.valueOf(e.getTypeName() + ': ' + e.getMessage()))); }\n", page, label, label)
		fmt.Fprintf(&b, "try { Blob body = Page.%s.getContentAsPDF(); String body64 = EncodingUtil.base64Encode(body); String digest = EncodingUtil.convertToHex(Crypto.generateDigest('SHA-256', body)); Integer chunks = (body64.length() + 2999) / 3000; for (Integer i = 0; i < chunks; i++) { Integer startAt = i * 3000; Integer endAt = Math.min(body64.length(), startAt + 3000); System.debug('GLADE_VF_CAPTURE_CHUNK|%s|PDF|OK|' + body.size() + '|' + digest + '|' + i + '|' + chunks + '|' + body64.substring(startAt, endAt)); } } catch (Exception e) { System.debug('GLADE_VF_CAPTURE|%s|PDF|ERROR|' + EncodingUtil.base64Encode(Blob.valueOf(e.getTypeName() + ': ' + e.getMessage()))); }\n", page, label, label)
	}
	file, err := os.CreateTemp("", "glade-vf-capture-*.apex")
	if err != nil {
		return "", err
	}
	if _, err := file.WriteString(b.String()); err != nil {
		file.Close()
		os.Remove(file.Name())
		return "", err
	}
	if err := file.Close(); err != nil {
		os.Remove(file.Name())
		return "", err
	}
	return file.Name(), nil
}

var visualforceCaptureLine = regexp.MustCompile(`GLADE_VF_CAPTURE\|([^|]+)\|(HTML|PDF)\|(OK|ERROR)\|([A-Za-z0-9+/=]+)`)
var visualforceCaptureChunkLine = regexp.MustCompile(`GLADE_VF_CAPTURE_CHUNK\|([^|]+)\|(HTML|PDF)\|OK\|([0-9]+)\|([a-fA-F0-9]+)\|([0-9]+)\|([0-9]+)\|([A-Za-z0-9+/=]+)`)

func parseVisualforceProbeCaptures(output []byte) map[string]VisualforceRenderedCapture {
	captures := map[string]VisualforceRenderedCapture{}
	unescaped := []byte(html.UnescapeString(string(output)))
	parseVisualforceProbeCaptureChunks(captures, unescaped)
	for _, match := range visualforceCaptureLine.FindAllSubmatch(unescaped, -1) {
		page := string(match[1])
		kind := string(match[2])
		state := string(match[3])
		raw, err := base64.StdEncoding.DecodeString(string(match[4]))
		if err != nil {
			captures[visualforceCaptureKey(page, kind)] = VisualforceRenderedCapture{Status: "fail", Error: err.Error()}
			continue
		}
		if state == "ERROR" {
			captures[visualforceCaptureKey(page, kind)] = VisualforceRenderedCapture{Status: "fail", Error: string(raw)}
			continue
		}
		sum := sha256.Sum256(raw)
		capture := VisualforceRenderedCapture{
			Status: "pass",
			Bytes:  len(raw),
			SHA256: hex.EncodeToString(sum[:]),
		}
		if kind == "HTML" {
			capture.Body = string(raw)
			setVisualforceRenderedBodyText(&capture, raw)
		} else {
			capture.Base64 = base64.StdEncoding.EncodeToString(raw)
			setVisualforceRenderedPDFText(&capture, raw)
		}
		captures[visualforceCaptureKey(page, kind)] = capture
	}
	return captures
}

func parseVisualforceProbeCaptureChunks(captures map[string]VisualforceRenderedCapture, output []byte) {
	type chunkSet struct {
		page   string
		kind   string
		bytes  int
		sha    string
		total  int
		chunks map[int]string
	}
	sets := map[string]*chunkSet{}
	for _, match := range visualforceCaptureChunkLine.FindAllSubmatch(output, -1) {
		page := string(match[1])
		kind := string(match[2])
		byteCount, _ := strconv.Atoi(string(match[3]))
		sha := strings.ToLower(string(match[4]))
		index, _ := strconv.Atoi(string(match[5]))
		total, _ := strconv.Atoi(string(match[6]))
		key := visualforceCaptureKey(page, kind)
		set := sets[key]
		if set == nil {
			set = &chunkSet{page: page, kind: kind, bytes: byteCount, sha: sha, total: total, chunks: map[int]string{}}
			sets[key] = set
		}
		set.chunks[index] = string(match[7])
	}
	for key, set := range sets {
		if set.total <= 0 || len(set.chunks) != set.total {
			captures[key] = VisualforceRenderedCapture{Status: "fail", Error: "probe log omitted one or more capture chunks"}
			continue
		}
		var encoded strings.Builder
		missing := false
		for i := 0; i < set.total; i++ {
			chunk, ok := set.chunks[i]
			if !ok {
				captures[key] = VisualforceRenderedCapture{Status: "fail", Error: "probe log omitted capture chunk"}
				missing = true
				break
			}
			encoded.WriteString(chunk)
		}
		if missing {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(encoded.String())
		if err != nil {
			captures[key] = VisualforceRenderedCapture{Status: "fail", Error: err.Error()}
			continue
		}
		sum := sha256.Sum256(raw)
		actualSHA := hex.EncodeToString(sum[:])
		if set.sha != "" && set.sha != actualSHA {
			captures[key] = VisualforceRenderedCapture{Status: "fail", Error: "probe capture SHA-256 mismatch"}
			continue
		}
		capture := VisualforceRenderedCapture{Status: "pass", Bytes: len(raw), SHA256: actualSHA}
		if set.bytes > 0 {
			capture.Bytes = set.bytes
		}
		if set.kind == "HTML" {
			capture.Body = string(raw)
			setVisualforceRenderedBodyText(&capture, raw)
		} else {
			capture.Base64 = base64.StdEncoding.EncodeToString(raw)
			setVisualforceRenderedPDFText(&capture, raw)
		}
		captures[key] = capture
	}
}

func visualforceCaptureKey(page, kind string) string {
	return page + "\x00" + kind
}

func accumulateVisualforceCaptureCounts(counts *VisualforceCaptureCounts, page VisualforcePageCapture) {
	switch page.HTML.Status {
	case "pass":
		counts.HTMLPass++
	case "notCaptured":
		counts.HTMLNotCaptured++
	default:
		if page.HTML.Status == "missing" {
			counts.HTMLNotCaptured++
		}
		counts.HTMLFail++
	}
	switch page.PDF.Status {
	case "pass":
		counts.PDFPass++
	default:
		if page.PDF.Status == "notCaptured" || page.PDF.Status == "missing" {
			counts.PDFNotCaptured++
		}
		counts.PDFFail++
	}
}

func writeVisualforceCaptureReport(path string, report VisualforceCaptureReport) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		return err
	}
	return os.WriteFile(path, b.Bytes(), 0o644)
}

func parseVisualforceOrgIdentity(output []byte) visualforceOrgIdentity {
	var payload struct {
		Result struct {
			Username    string `json:"username"`
			OrgID       string `json:"orgId"`
			ID          string `json:"id"`
			InstanceURL string `json:"instanceUrl"`
			AccessToken string `json:"accessToken"`
		} `json:"result"`
	}
	if err := json.Unmarshal(output, &payload); err != nil {
		return visualforceOrgIdentity{}
	}
	orgID := payload.Result.OrgID
	if orgID == "" {
		orgID = payload.Result.ID
	}
	return visualforceOrgIdentity{
		Username:    payload.Result.Username,
		OrgID:       orgID,
		InstanceURL: strings.TrimSpace(payload.Result.InstanceURL),
		AccessToken: strings.TrimSpace(payload.Result.AccessToken),
	}
}

func selectedVisualforceResponseHeaders(headers http.Header) map[string]string {
	selected := map[string]string{}
	for _, name := range []string{
		"Cache-Control",
		"Content-Disposition",
		"Content-Language",
		"Content-Type",
		"ETag",
		"Expires",
		"Last-Modified",
		"Location",
		"Pragma",
		"Retry-After",
		"X-Content-Type-Options",
		"X-Frame-Options",
		"X-Robots-Tag",
	} {
		if value := firstHeaderValue(headers, name); value != "" {
			selected[name] = value
		}
	}
	if len(selected) == 0 {
		return nil
	}
	return selected
}

func firstHeaderValue(headers http.Header, name string) string {
	values := headers.Values(name)
	if len(values) == 0 {
		return ""
	}
	return strings.Join(values, ", ")
}

func isVisualforcePDFResponse(contentType string, body []byte) bool {
	mediaType := visualforceMediaType(contentType)
	if mediaType == "application/pdf" {
		return true
	}
	return bytes.HasPrefix(bytes.TrimSpace(body), []byte("%PDF-"))
}

func isVisualforceHTMLResponse(contentType string, body []byte) bool {
	mediaType := visualforceMediaType(contentType)
	if mediaType == "text/html" || mediaType == "application/xhtml+xml" {
		return true
	}
	trimmed := bytes.TrimSpace(bytes.ToLower(body))
	return bytes.HasPrefix(trimmed, []byte("<!doctype html")) || bytes.HasPrefix(trimmed, []byte("<html"))
}

func visualforceMediaType(contentType string) string {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	}
	return strings.ToLower(mediaType)
}

var visualforceScriptStyleBlock = regexp.MustCompile(`(?is)<(script|style)\b[^>]*>.*?</(script|style)>`)
var visualforceHTMLTag = regexp.MustCompile(`(?s)<[^>]+>`)
var visualforcePDFStream = regexp.MustCompile(`(?s)(.{0,256})stream\r?\n(.*?)\r?\nendstream`)
var visualforcePDFTjString = regexp.MustCompile(`(?s)\((?:\\.|[^\\()])*\)\s*Tj`)

func normalizeVisualforceRawText(body []byte) string {
	text := string(body)
	text = visualforceScriptStyleBlock.ReplaceAllString(text, " ")
	text = visualforceHTMLTag.ReplaceAllString(text, " ")
	text = html.UnescapeString(text)
	return strings.Join(strings.Fields(text), " ")
}

func normalizeVisualforcePDFText(body []byte) string {
	var parts []string
	for _, match := range visualforcePDFStream.FindAllSubmatch(body, -1) {
		stream := match[2]
		if bytes.Contains(match[1], []byte("/FlateDecode")) {
			if inflated, err := inflateVisualforcePDFStream(stream); err == nil {
				stream = inflated
			}
		}
		for _, textMatch := range visualforcePDFTjString.FindAll(stream, -1) {
			raw := string(textMatch)
			closeAt := strings.LastIndex(raw, ")")
			if closeAt <= 0 {
				continue
			}
			if decoded := decodeVisualforcePDFString(raw[1:closeAt]); decoded != "" {
				parts = append(parts, decoded)
			}
		}
	}
	return strings.Join(strings.Fields(strings.Join(parts, " ")), " ")
}

func inflateVisualforcePDFStream(stream []byte) ([]byte, error) {
	reader, err := zlib.NewReader(bytes.NewReader(stream))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(reader)
}

func decodeVisualforcePDFString(raw string) string {
	var out strings.Builder
	for i := 0; i < len(raw); i++ {
		ch := raw[i]
		if ch != '\\' || i+1 >= len(raw) {
			out.WriteByte(ch)
			continue
		}
		i++
		switch raw[i] {
		case 'n':
			out.WriteByte('\n')
		case 'r':
			out.WriteByte('\r')
		case 't':
			out.WriteByte('\t')
		case 'b':
			out.WriteByte('\b')
		case 'f':
			out.WriteByte('\f')
		case '\\', '(', ')':
			out.WriteByte(raw[i])
		default:
			if raw[i] >= '0' && raw[i] <= '7' {
				start := i
				for i+1 < len(raw) && i-start < 2 && raw[i+1] >= '0' && raw[i+1] <= '7' {
					i++
				}
				value, err := strconv.ParseInt(raw[start:i+1], 8, 32)
				if err == nil {
					out.WriteByte(byte(value))
					continue
				}
			}
			out.WriteByte(raw[i])
		}
	}
	return out.String()
}

func sfJSONStatusOK(output []byte) bool {
	var payload struct {
		Status int `json:"status"`
	}
	if err := json.Unmarshal(output, &payload); err != nil {
		return len(bytes.TrimSpace(output)) > 0
	}
	return payload.Status == 0
}

func trimCommandOutput(output []byte) string {
	text := strings.TrimSpace(string(output))
	if len(text) > 2000 {
		return text[:2000] + "...(truncated)"
	}
	return text
}

func isApexIdentifier(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if i == 0 {
			if r != '_' && (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') {
				return false
			}
			continue
		}
		if r != '_' && (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}
