package compat

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

type VisualforceCaptureDiffReport struct {
	OK        bool                          `json:"ok"`
	DiffCount int                           `json:"diffCount"`
	Summary   VisualforceCaptureDiffSummary `json:"summary"`
	Diffs     []VisualforceCaptureDiff      `json:"diffs,omitempty"`
}

type VisualforceCaptureDiffSummary struct {
	PageCountCompared  int                              `json:"pageCountCompared"`
	MissingPageCount   int                              `json:"missingPageCount"`
	DifferingPageCount int                              `json:"differingPageCount"`
	DiffCountByField   map[string]int                   `json:"diffCountByField"`
	DiffCountByVariant map[string]int                   `json:"diffCountByVariant"`
	Scoreboard         VisualforceCaptureDiffScoreboard `json:"scoreboard,omitempty"`
}

type VisualforceCaptureDiffScoreboard struct {
	Groups     []VisualforceCaptureDiffScoreboardRow `json:"groups,omitempty"`
	Owners     []VisualforceCaptureDiffScoreboardRow `json:"owners,omitempty"`
	Categories []VisualforceCaptureDiffScoreboardRow `json:"categories,omitempty"`
}

type VisualforceCaptureDiffScoreboardRow struct {
	Name         string `json:"name"`
	Owner        string `json:"owner,omitempty"`
	Category     string `json:"category,omitempty"`
	PageCount    int    `json:"pageCount"`
	PassCount    int    `json:"passCount"`
	FailCount    int    `json:"failCount"`
	MissingCount int    `json:"missingCount"`
	DiffCount    int    `json:"diffCount"`
}

type VisualforceCaptureDiff struct {
	Page       string `json:"page"`
	Variant    string `json:"variant,omitempty"`
	Field      string `json:"field"`
	Salesforce string `json:"salesforce"`
	Local      string `json:"local"`
}

type visualforceDiffCaptureReport struct {
	Pages []visualforceDiffPage `json:"pages"`
}

type visualforceDiffPage struct {
	Name     string
	Group    string
	Owner    string
	Category string
	Variants map[string]map[string]any
}

var visualforceRenderDiffFields = []string{
	"status",
	"contentType",
	"redirectLocation",
	"bytes",
	"sha256",
	"bodyHash",
	"body",
	"base64",
	"textHash",
	"text",
	"normalizedText",
	"contractText",
}

func DiffVisualforceCaptureFiles(salesforcePath, localPath string) (VisualforceCaptureDiffReport, error) {
	return DiffVisualforceCaptureFilesWithProject(salesforcePath, localPath, "")
}

func DiffVisualforceCaptureFilesWithProject(salesforcePath, localPath, project string) (VisualforceCaptureDiffReport, error) {
	salesforce, err := os.ReadFile(salesforcePath)
	if err != nil {
		return VisualforceCaptureDiffReport{}, err
	}
	local, err := os.ReadFile(localPath)
	if err != nil {
		return VisualforceCaptureDiffReport{}, err
	}
	if strings.TrimSpace(project) == "" {
		return DiffVisualforceCaptureReports(salesforce, local)
	}
	index, err := ReadVisualforceProbeIndex(project)
	if err != nil {
		return VisualforceCaptureDiffReport{}, err
	}
	return diffVisualforceCaptureReports(salesforce, local, &index)
}

func DiffVisualforceCaptureReports(salesforceJSON, localJSON []byte) (VisualforceCaptureDiffReport, error) {
	return diffVisualforceCaptureReports(salesforceJSON, localJSON, nil)
}

func diffVisualforceCaptureReports(salesforceJSON, localJSON []byte, index *VisualforceProbeIndex) (VisualforceCaptureDiffReport, error) {
	salesforce, err := readVisualforceDiffCaptureReport(salesforceJSON)
	if err != nil {
		return VisualforceCaptureDiffReport{}, fmt.Errorf("read salesforce capture: %w", err)
	}
	local, err := readVisualforceDiffCaptureReport(localJSON)
	if err != nil {
		return VisualforceCaptureDiffReport{}, fmt.Errorf("read local capture: %w", err)
	}
	report := VisualforceCaptureDiffReport{OK: true}
	salesforcePages := visualforceDiffPagesByName(salesforce.Pages)
	localPages := visualforceDiffPagesByName(local.Pages)
	for _, page := range sortedVisualforceDiffPageNames(salesforcePages, localPages, index) {
		salesforcePage, hasSalesforce := salesforcePages[page]
		localPage, hasLocal := localPages[page]
		switch {
		case !hasSalesforce && !hasLocal:
			appendVisualforceCaptureDiff(&report, VisualforceCaptureDiff{Page: page, Field: "page", Salesforce: "missing", Local: "missing"})
			continue
		case !hasLocal:
			appendVisualforceCaptureDiff(&report, VisualforceCaptureDiff{Page: page, Field: "page", Salesforce: "present", Local: "missing"})
			continue
		case !hasSalesforce:
			appendVisualforceCaptureDiff(&report, VisualforceCaptureDiff{Page: page, Field: "page", Salesforce: "missing", Local: "present"})
			continue
		}
		appendVisualforceRenderDiffs(&report, page, salesforcePage.Variants, localPage.Variants)
	}
	report.DiffCount = len(report.Diffs)
	report.OK = report.DiffCount == 0
	report.Summary = summarizeVisualforceCaptureDiffs(salesforcePages, localPages, report.Diffs, index)
	return report, nil
}

func (p *visualforceDiffPage) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	var name string
	if err := json.Unmarshal(raw["name"], &name); err != nil {
		return fmt.Errorf("page name: %w", err)
	}
	p.Name = strings.TrimSpace(name)
	_ = json.Unmarshal(raw["group"], &p.Group)
	_ = json.Unmarshal(raw["owner"], &p.Owner)
	_ = json.Unmarshal(raw["category"], &p.Category)
	p.Variants = map[string]map[string]any{}
	for key, value := range raw {
		if key == "name" || key == "group" || key == "owner" || key == "category" {
			continue
		}
		var fields map[string]any
		if err := decodeVisualforceDiffJSON(value, &fields); err != nil {
			continue
		}
		if fields != nil {
			p.Variants[key] = fields
		}
	}
	return nil
}

func readVisualforceDiffCaptureReport(data []byte) (visualforceDiffCaptureReport, error) {
	var report visualforceDiffCaptureReport
	if err := decodeVisualforceDiffJSON(data, &report); err != nil {
		return visualforceDiffCaptureReport{}, err
	}
	if len(report.Pages) == 0 {
		return visualforceDiffCaptureReport{}, errorsVisualforceDiffNoPages()
	}
	for _, page := range report.Pages {
		if page.Name == "" {
			return visualforceDiffCaptureReport{}, fmt.Errorf("page name is required")
		}
	}
	enrichVisualforceDiffPDFContractText(&report)
	return report, nil
}

func errorsVisualforceDiffNoPages() error {
	return fmt.Errorf("capture report has no pages")
}

func decodeVisualforceDiffJSON(data []byte, out any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	return dec.Decode(out)
}

func visualforceDiffPagesByName(pages []visualforceDiffPage) map[string]visualforceDiffPage {
	result := make(map[string]visualforceDiffPage, len(pages))
	for _, page := range pages {
		result[page.Name] = page
	}
	return result
}

func enrichVisualforceDiffPDFContractText(report *visualforceDiffCaptureReport) {
	if report == nil {
		return
	}
	for _, page := range report.Pages {
		pdf := page.Variants["pdf"]
		if len(pdf) == 0 {
			continue
		}
		if _, ok := visualforceDiffContractText("pdf", pdf); ok {
			continue
		}
		encoded, ok := visualforceDiffValue(pdf, "base64")
		if !ok {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			continue
		}
		normalized := normalizeVisualforcePDFText(raw)
		if normalized == "" {
			continue
		}
		pdf["text"] = normalized
		pdf["normalizedText"] = normalized
		if contractText := normalizeVisualforceDiffContractText(normalized); contractText != "" {
			pdf["contractText"] = contractText
		}
	}
}

func appendVisualforceRenderDiffs(report *VisualforceCaptureDiffReport, page string, salesforce, local map[string]map[string]any) {
	for _, variant := range sortedVisualforceDiffKeys(salesforce, local) {
		salesforceRender, hasSalesforce := salesforce[variant]
		localRender, hasLocal := local[variant]
		switch {
		case !hasLocal:
			appendVisualforceCaptureDiff(report, VisualforceCaptureDiff{Page: page, Variant: variant, Field: "variant", Salesforce: "present", Local: "missing"})
			continue
		case !hasSalesforce:
			appendVisualforceCaptureDiff(report, VisualforceCaptureDiff{Page: page, Variant: variant, Field: "variant", Salesforce: "missing", Local: "present"})
			continue
		}
		if visualforceDiffVariantMatches(variant, salesforceRender, localRender) {
			continue
		}
		salesforceContract, hasSalesforceContract := visualforceDiffContractText(variant, salesforceRender)
		localContract, hasLocalContract := visualforceDiffContractText(variant, localRender)
		contractTextComparable := hasSalesforceContract && hasLocalContract
		contractTextMatches := contractTextComparable && salesforceContract == localContract
		if !contractTextComparable {
			if fallbackSalesforceContract, fallbackLocalContract, ok := visualforceDiffMatchingHTMLContractText(variant, salesforce, local); ok {
				contractTextComparable = true
				salesforceContract = fallbackSalesforceContract
				localContract = fallbackLocalContract
				contractTextMatches = fallbackSalesforceContract == fallbackLocalContract
			}
		}
		if contractTextComparable && !contractTextMatches {
			appendVisualforceCaptureDiff(report, VisualforceCaptureDiff{
				Page:       page,
				Variant:    variant,
				Field:      "contractText",
				Salesforce: salesforceContract,
				Local:      localContract,
			})
		}
		for _, field := range visualforceRenderDiffFields {
			salesforceValue, hasSalesforceValue := visualforceDiffValue(salesforceRender, field)
			localValue, hasLocalValue := visualforceDiffValue(localRender, field)
			if !hasSalesforceValue && !hasLocalValue {
				continue
			}
			if field == "contentType" && (!hasSalesforceValue || !hasLocalValue) {
				continue
			}
			if !hasSalesforceValue {
				salesforceValue = "missing"
			}
			if !hasLocalValue {
				localValue = "missing"
			}
			if contractTextComparable && visualforceDiffFieldUsesContractText(variant, field) {
				continue
			}
			if visualforceDiffFieldMatches(variant, field, salesforceValue, localValue, contractTextMatches) {
				continue
			}
			if salesforceValue != localValue {
				appendVisualforceCaptureDiff(report, VisualforceCaptureDiff{
					Page:       page,
					Variant:    variant,
					Field:      field,
					Salesforce: salesforceValue,
					Local:      localValue,
				})
			}
		}
	}
}

func appendVisualforceCaptureDiff(report *VisualforceCaptureDiffReport, diff VisualforceCaptureDiff) {
	diff.Salesforce = visualforceDiffDiagnosticValue(diff.Field, diff.Salesforce)
	diff.Local = visualforceDiffDiagnosticValue(diff.Field, diff.Local)
	report.Diffs = append(report.Diffs, diff)
}

func visualforceDiffDiagnosticValue(field, value string) string {
	if value == "" || value == "missing" || value == "present" || value == "null" {
		return value
	}
	switch field {
	case "body", "base64":
		return visualforceDiffRedactedValue(field, value, 0)
	}
	if len(value) > 200 {
		return visualforceDiffRedactedValue(field, value, 24)
	}
	return value
}

func visualforceDiffRedactedValue(field, value string, previewLimit int) string {
	sum := sha256.Sum256([]byte(value))
	parts := []string{
		"redacted",
		field,
		fmt.Sprintf("bytes=%d", len(value)),
		"sha256=" + hex.EncodeToString(sum[:]),
	}
	if previewLimit > 0 {
		preview := value
		if len(preview) > previewLimit {
			preview = preview[:previewLimit]
		}
		parts = append(parts, fmt.Sprintf("preview=%q", preview))
	}
	return strings.Join(parts, " ")
}

func visualforceDiffMatchingHTMLContractText(variant string, salesforce, local map[string]map[string]any) (string, string, bool) {
	if variant != "pdf" {
		return "", "", false
	}
	salesforceHTML, hasSalesforceHTML := salesforce["html"]
	localHTML, hasLocalHTML := local["html"]
	if !hasSalesforceHTML || !hasLocalHTML {
		return "", "", false
	}
	salesforceContract, hasSalesforceContract := visualforceDiffContractText("html", salesforceHTML)
	localContract, hasLocalContract := visualforceDiffContractText("html", localHTML)
	if !hasSalesforceContract || !hasLocalContract {
		return "", "", false
	}
	return salesforceContract, localContract, true
}

func summarizeVisualforceCaptureDiffs(salesforcePages, localPages map[string]visualforceDiffPage, diffs []VisualforceCaptureDiff, index *VisualforceProbeIndex) VisualforceCaptureDiffSummary {
	summary := VisualforceCaptureDiffSummary{
		PageCountCompared:  len(sortedVisualforceDiffPageNames(salesforcePages, localPages, index)),
		DiffCountByField:   map[string]int{},
		DiffCountByVariant: map[string]int{},
	}
	differingPages := map[string]bool{}
	for _, diff := range diffs {
		differingPages[diff.Page] = true
		if diff.Field == "page" {
			summary.MissingPageCount++
		}
		summary.DiffCountByField[diff.Field]++
		variant := diff.Variant
		if variant == "" {
			variant = "page"
		}
		summary.DiffCountByVariant[variant]++
	}
	summary.DifferingPageCount = len(differingPages)
	summary.Scoreboard = buildVisualforceCaptureDiffScoreboard(salesforcePages, localPages, diffs, index)
	return summary
}

func buildVisualforceCaptureDiffScoreboard(salesforcePages, localPages map[string]visualforceDiffPage, diffs []VisualforceCaptureDiff, index *VisualforceProbeIndex) VisualforceCaptureDiffScoreboard {
	pageMetadata := visualforceDiffScoreboardPageMetadata(salesforcePages, localPages, index)
	if len(pageMetadata) == 0 {
		return VisualforceCaptureDiffScoreboard{}
	}
	groupMetadata := map[string]VisualforceProbeGroup{}
	if index != nil {
		groupMetadata = visualforceProbeGroupsByName(index.Groups)
	}
	diffCountByPage := map[string]int{}
	missingByPage := map[string]bool{}
	for _, diff := range diffs {
		diffCountByPage[diff.Page]++
		if diff.Field == "page" {
			missingByPage[diff.Page] = true
		}
	}
	groupRows := map[string]*VisualforceCaptureDiffScoreboardRow{}
	ownerRows := map[string]*VisualforceCaptureDiffScoreboardRow{}
	categoryRows := map[string]*VisualforceCaptureDiffScoreboardRow{}
	for _, pageName := range sortedVisualforceProbePageNames(pageMetadata) {
		page := pageMetadata[pageName]
		hasSalesforce := visualforceDiffPageExists(salesforcePages, pageName)
		hasLocal := visualforceDiffPageExists(localPages, pageName)
		missing := missingByPage[pageName] || !hasSalesforce || !hasLocal
		failed := missing || diffCountByPage[pageName] > 0
		if page.Group != "" {
			row := visualforceDiffScoreboardRow(groupRows, page.Group)
			if group, ok := groupMetadata[page.Group]; ok {
				row.Owner = group.Owner
				row.Category = group.Category
			}
			if row.Owner == "" {
				row.Owner = page.Owner
			}
			if row.Category == "" {
				row.Category = page.Category
			}
			countVisualforceDiffScoreboardPage(row, failed, missing, diffCountByPage[pageName])
		}
		if page.Owner != "" {
			countVisualforceDiffScoreboardPage(visualforceDiffScoreboardRow(ownerRows, page.Owner), failed, missing, diffCountByPage[pageName])
		}
		if page.Category != "" {
			countVisualforceDiffScoreboardPage(visualforceDiffScoreboardRow(categoryRows, page.Category), failed, missing, diffCountByPage[pageName])
		}
	}
	return VisualforceCaptureDiffScoreboard{
		Groups:     sortedVisualforceDiffScoreboardRows(groupRows),
		Owners:     sortedVisualforceDiffScoreboardRows(ownerRows),
		Categories: sortedVisualforceDiffScoreboardRows(categoryRows),
	}
}

func visualforceDiffScoreboardPageMetadata(salesforcePages, localPages map[string]visualforceDiffPage, index *VisualforceProbeIndex) map[string]VisualforceProbePage {
	metadata := map[string]VisualforceProbePage{}
	if index != nil {
		groupMetadata := visualforceProbeGroupsByName(index.Groups)
		for _, page := range index.Pages {
			page = fillVisualforceProbePageDefaults(page, groupMetadata[page.Group])
			if page.Name != "" {
				metadata[page.Name] = page
			}
		}
		for _, group := range index.Groups {
			for _, pageName := range group.Pages {
				if pageName == "" {
					continue
				}
				if _, ok := metadata[pageName]; ok {
					continue
				}
				metadata[pageName] = VisualforceProbePage{
					Name:     pageName,
					Group:    group.Name,
					Owner:    group.Owner,
					Category: group.Category,
				}
			}
		}
	}
	for _, page := range salesforcePages {
		visualforceDiffAddCaptureMetadata(metadata, page)
	}
	for _, page := range localPages {
		visualforceDiffAddCaptureMetadata(metadata, page)
	}
	return metadata
}

func visualforceDiffAddCaptureMetadata(metadata map[string]VisualforceProbePage, page visualforceDiffPage) {
	if page.Name == "" {
		return
	}
	existing := metadata[page.Name]
	if existing.Name == "" {
		existing.Name = page.Name
	}
	if existing.Group == "" {
		existing.Group = page.Group
	}
	if existing.Owner == "" {
		existing.Owner = page.Owner
	}
	if existing.Category == "" {
		existing.Category = page.Category
	}
	if existing.Group != "" || existing.Owner != "" || existing.Category != "" {
		metadata[page.Name] = existing
	}
}

func sortedVisualforceProbePageNames(pages map[string]VisualforceProbePage) []string {
	names := make([]string, 0, len(pages))
	for name := range pages {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func sortedVisualforceDiffPageNames(salesforcePages, localPages map[string]visualforceDiffPage, index *VisualforceProbeIndex) []string {
	seen := map[string]bool{}
	var names []string
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		names = append(names, name)
	}
	for name := range salesforcePages {
		add(name)
	}
	for name := range localPages {
		add(name)
	}
	if index != nil {
		for _, page := range index.Pages {
			add(page.Name)
		}
		for _, group := range index.Groups {
			for _, pageName := range group.Pages {
				add(pageName)
			}
		}
	}
	sort.Strings(names)
	return names
}

func visualforceDiffPageExists(pages map[string]visualforceDiffPage, name string) bool {
	_, ok := pages[name]
	return ok
}

func visualforceDiffScoreboardRow(rows map[string]*VisualforceCaptureDiffScoreboardRow, name string) *VisualforceCaptureDiffScoreboardRow {
	row := rows[name]
	if row == nil {
		row = &VisualforceCaptureDiffScoreboardRow{Name: name}
		rows[name] = row
	}
	return row
}

func countVisualforceDiffScoreboardPage(row *VisualforceCaptureDiffScoreboardRow, failed, missing bool, diffCount int) {
	row.PageCount++
	if failed {
		row.FailCount++
	} else {
		row.PassCount++
	}
	if missing {
		row.MissingCount++
	}
	row.DiffCount += diffCount
}

func sortedVisualforceDiffScoreboardRows(rows map[string]*VisualforceCaptureDiffScoreboardRow) []VisualforceCaptureDiffScoreboardRow {
	keys := make([]string, 0, len(rows))
	for key := range rows {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]VisualforceCaptureDiffScoreboardRow, 0, len(keys))
	for _, key := range keys {
		result = append(result, *rows[key])
	}
	return result
}

func visualforceDiffValue(fields map[string]any, name string) (string, bool) {
	value, ok := fields[name]
	if !ok {
		return "", false
	}
	return visualforceDiffString(value), true
}

func visualforceDiffString(value any) string {
	switch v := value.(type) {
	case nil:
		return "null"
	case string:
		return v
	case json.Number:
		return v.String()
	case bool:
		if v {
			return "true"
		}
		return "false"
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(data)
	}
}

func sortedVisualforceDiffKeys[V any](left, right map[string]V) []string {
	seen := map[string]bool{}
	keys := make([]string, 0, len(left)+len(right))
	for key := range left {
		if !seen[key] {
			seen[key] = true
			keys = append(keys, key)
		}
	}
	for key := range right {
		if !seen[key] {
			seen[key] = true
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}
