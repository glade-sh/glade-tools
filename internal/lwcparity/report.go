package lwcparity

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/glade-sh/glade/internal/lwcbrowser"
)

const SchemaVersion = 1

type Options struct {
	DocsDir string
}

type Report struct {
	SchemaVersion int        `json:"schemaVersion"`
	DocsDir       string     `json:"docsDir,omitempty"`
	Rows          []Row      `json:"rows"`
	Summary       Summary    `json:"summary"`
	NextGates     []NextGate `json:"nextGates"`
}

type Row struct {
	ID            string `json:"id"`
	Category      string `json:"category"`
	Name          string `json:"name"`
	Status        string `json:"status"`
	DocsSource    string `json:"docsSource,omitempty"`
	LocalSource   string `json:"localSource,omitempty"`
	FirstAPI      string `json:"firstApiVersion,omitempty"`
	Summary       string `json:"summary,omitempty"`
	LocalEvidence string `json:"localEvidence,omitempty"`
	OracleStatus  string `json:"oracleStatus"`
	Notes         string `json:"notes,omitempty"`
}

type Summary struct {
	Total      int            `json:"total"`
	ByCategory map[string]int `json:"byCategory"`
	ByStatus   map[string]int `json:"byStatus"`
}

type NextGate struct {
	Name    string `json:"name"`
	Command string `json:"command"`
}

const (
	StatusSupportedLocal   = "supported-local"
	StatusPartialLocal     = "partial-local"
	StatusUnsupportedLocal = "unsupported-local"
	StatusDocsOnly         = "docs-only"
	StatusLocalOnly        = "local-only"
	StatusOracleMissing    = "not-probed"
)

const (
	CategoryAPIModule        = "api-module"
	CategorySalesforceModule = "salesforce-module"
	CategoryPageReference    = "page-reference"
	CategoryBaseComponent    = "base-component"
)

var salesforceModuleRE = regexp.MustCompile(`@salesforce/[A-Za-z0-9_./]+`)

func Build(options Options) (Report, error) {
	options.DocsDir = strings.TrimSpace(options.DocsDir)
	if options.DocsDir == "" {
		return Report{}, fmt.Errorf("--docs is required")
	}
	if info, err := os.Stat(options.DocsDir); err != nil {
		return Report{}, err
	} else if !info.IsDir() {
		return Report{}, fmt.Errorf("docs source is not a directory: %s", options.DocsDir)
	}

	rows := map[string]Row{}
	if err := addAPIModuleDocs(rows, options.DocsDir); err != nil {
		return Report{}, err
	}
	if err := addSalesforceModuleDocs(rows, options.DocsDir); err != nil {
		return Report{}, err
	}
	if err := addPageReferenceDocs(rows, options.DocsDir); err != nil {
		return Report{}, err
	}
	addLocalBaseComponents(rows)
	applyLocalStatuses(rows)

	report := Report{
		SchemaVersion: SchemaVersion,
		DocsDir:       options.DocsDir,
		Rows:          sortedRows(rows),
		NextGates: []NextGate{{
			Name:    "live org oracle",
			Command: "glade-tools lwc capture --target-org oaer-probe-max --project <fixture> --local-browser-capture --browser-capture --out <capture.json>",
		}, {
			Name:    "expand docs source",
			Command: "refresh the LWC docs scrape with Component Reference and per-module API pages, then rerun glade-tools lwc parity --docs <lwc-docs>",
		}, {
			Name:    "local parity check",
			Command: "glade-tools lwc parity --docs <lwc-docs> --check docs/generated/LWC_NATIVE_API_PARITY.md",
		}},
	}
	report.Summary = summarize(report.Rows)
	return report, nil
}

func addAPIModuleDocs(rows map[string]Row, docsDir string) error {
	for _, name := range []string{"reference-api-modules.md", "reference-ui-api.md"} {
		source := filepath.Join(docsDir, name)
		content, err := os.ReadFile(source)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		for _, line := range strings.Split(string(content), "\n") {
			if !strings.Contains(line, "lightning/") && !strings.Contains(line, "experience/") {
				continue
			}
			module, ok := firstMarkdownLinkLabel(line)
			if !ok {
				continue
			}
			module = cleanDocName(module)
			if !strings.Contains(module, "/") || strings.HasPrefix(module, "@") {
				continue
			}
			cells := markdownCells(line)
			summary := ""
			firstAPI := ""
			if len(cells) >= 2 {
				summary = strings.TrimSpace(cells[1])
			}
			if len(cells) >= 3 {
				firstAPI = strings.TrimSpace(cells[2])
			}
			upsertDocRow(rows, CategoryAPIModule, module, relDocSource(source), summary, firstAPI)
		}
	}
	return nil
}

func addSalesforceModuleDocs(rows map[string]Row, docsDir string) error {
	source := filepath.Join(docsDir, "reference-salesforce-modules.md")
	content, err := os.ReadFile(source)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	text := string(content)
	for _, raw := range salesforceModuleRE.FindAllString(text, -1) {
		module := normalizeSalesforceModule(raw)
		upsertDocRow(rows, CategorySalesforceModule, module, relDocSource(source), "", "")
	}
	lower := strings.ToLower(text)
	if strings.Contains(lower, "custom permission") {
		upsertDocRow(rows, CategorySalesforceModule, "@salesforce/customPermission/", relDocSource(source), "", "")
	}
	if strings.Contains(lower, "user permission") {
		upsertDocRow(rows, CategorySalesforceModule, "@salesforce/userPermission/", relDocSource(source), "", "")
	}
	return nil
}

func addPageReferenceDocs(rows map[string]Row, docsDir string) error {
	source := filepath.Join(docsDir, "reference-page-reference-type.md")
	content, err := os.ReadFile(source)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	supported := map[string]bool{}
	inList := false
	sawBullet := false
	for _, line := range strings.Split(string(content), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "These page reference types are supported.") {
			inList = true
			continue
		}
		if inList && trimmed == "" && sawBullet {
			break
		}
		if !inList || !strings.HasPrefix(trimmed, "- ") {
			continue
		}
		sawBullet = true
		supported[strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))] = true
	}
	for label, pageType := range documentedPageReferenceTypes() {
		if !supported[label] {
			continue
		}
		upsertDocRow(rows, CategoryPageReference, pageType, relDocSource(source), label, "")
	}
	return nil
}

func documentedPageReferenceTypes() map[string]string {
	return map[string]string{
		"App":                                   "standard__app",
		"External Record Page":                  "standard__externalRecordPage",
		"External Record Relationship Page":     "standard__externalRecordRelationshipPage",
		"Knowledge Article":                     "standard__knowledgeArticlePage",
		"Lightning Component":                   "standard__component",
		"Login Page":                            "comm__loginPage",
		"Managed Content Page (Salesforce CMS)": "comm__managedContentPage",
		"Named Page (Experience Builder sites)": "comm__namedPage",
		"Named Page (Standard)":                 "standard__namedPage",
		"Navigation Item Page":                  "standard__navItemPage",
		"Object Page":                           "standard__objectPage",
		"Quick Action Page Type":                "standard__quickAction",
		"Record Page":                           "standard__recordPage",
		"Record Relationship Page":              "standard__recordRelationshipPage",
		"Standard Flow":                         "standard__flow",
		"Web Page":                              "standard__webPage",
	}
}

func addLocalBaseComponents(rows map[string]Row) {
	for specifier := range lwcbrowser.SupportedLightningBaseComponentSpecifiers() {
		name := strings.TrimPrefix(specifier, "lightning/")
		id := rowID(CategoryBaseComponent, specifier)
		rows[id] = Row{
			ID:            id,
			Category:      CategoryBaseComponent,
			Name:          specifier,
			Status:        StatusLocalOnly,
			LocalSource:   "github.com/glade-sh/glade/internal/lwcbrowser.SupportedLightningBaseComponentSpecifiers",
			LocalEvidence: "/lightning/shims/lightning/" + name + ".js",
			OracleStatus:  StatusOracleMissing,
			Notes:         "local base-component implementation; supplied LWC docs scrape does not include Component Reference rows",
		}
	}
}

func applyLocalStatuses(rows map[string]Row) {
	imports := lwcbrowser.SalesforceImportMap()
	pageRefs := localPageReferenceTypes()
	for id, row := range rows {
		switch row.Category {
		case CategoryAPIModule, CategorySalesforceModule:
			status, evidence, notes := moduleLocalStatus(row.Name, imports)
			row.Status = status
			row.LocalEvidence = evidence
			row.Notes = joinNotes(row.Notes, notes)
		case CategoryPageReference:
			if _, ok := pageRefs[row.Name]; ok {
				row.Status = StatusSupportedLocal
				row.LocalEvidence = "lwcruntime/src/shell/navigation-service.mjs"
			} else {
				row.Status = StatusDocsOnly
				row.Notes = joinNotes(row.Notes, "not implemented by the local navigation service")
			}
		}
		if row.OracleStatus == "" {
			row.OracleStatus = StatusOracleMissing
		}
		rows[id] = row
	}
}

func moduleLocalStatus(name string, imports map[string]string) (string, string, string) {
	if status, ok := localModuleOverrides()[name]; ok {
		evidence := imports[name]
		if evidence == "" && status != StatusDocsOnly {
			evidence = localModulePrefixEvidence(name, imports)
		}
		return status, evidence, localModuleOverrideNotes()[name]
	}
	if evidence, ok := imports[name]; ok {
		return StatusSupportedLocal, evidence, ""
	}
	if evidence := localModulePrefixEvidence(name, imports); evidence != "" {
		return StatusPartialLocal, evidence, "dynamic import family exists; individual member behavior must be probed"
	}
	return StatusDocsOnly, "", "documented by Salesforce but no local shim is registered"
}

func localModuleOverrides() map[string]string {
	return map[string]string{
		"@salesforce/community/":           StatusPartialLocal,
		"@salesforce/i18n/":                StatusPartialLocal,
		"@salesforce/site/":                StatusPartialLocal,
		"@salesforce/site/activeLanguages": StatusUnsupportedLocal,
		"@salesforce/user/":                StatusPartialLocal,
		"@salesforce/userPermission/":      StatusDocsOnly,
		"lightning/graphql":                StatusDocsOnly,
		"lightning/uiAppsApi":              StatusDocsOnly,
		"lightning/uiGraphQLApi":           StatusDocsOnly,
		"lightning/uiLearningPlatformApi":  StatusDocsOnly,
		"lightning/uiListApi":              StatusPartialLocal,
		"lightning/uiListsApi":             StatusDocsOnly,
		"lightning/platformUtilityBarApi":  StatusDocsOnly,
	}
}

func localModuleOverrideNotes() map[string]string {
	return map[string]string{
		"@salesforce/community/":           "only basePath and Id are modeled",
		"@salesforce/i18n/":                "common locale values are modeled; full org locale matrix is not probed",
		"@salesforce/site/":                "only Id is modeled",
		"@salesforce/site/activeLanguages": "local site shim returns unsupported for activeLanguages",
		"@salesforce/user/":                "Id and isGuest are modeled",
		"@salesforce/userPermission/":      "no local user permission shim is registered",
		"lightning/uiListApi":              "deprecated module has a local diagnostic path, not full List UI parity",
	}
}

func localModulePrefixEvidence(name string, imports map[string]string) string {
	for prefix, target := range imports {
		if !strings.HasSuffix(prefix, "/") {
			continue
		}
		if strings.HasPrefix(name, prefix) || strings.TrimSuffix(name, "/") == strings.TrimSuffix(prefix, "/") {
			return target
		}
	}
	return ""
}

func localPageReferenceTypes() map[string]bool {
	out := map[string]bool{}
	for _, name := range []string{
		"standard__recordPage",
		"standard__objectPage",
		"standard__recordRelationshipPage",
		"standard__navItemPage",
		"standard__app",
		"standard__namedPage",
		"standard__component",
		"standard__quickAction",
		"standard__webPage",
		"comm__namedPage",
		"comm__loginPage",
		"comm__managedContentPage",
		"comm__recordPage",
		"comm__recordRelationshipPage",
	} {
		out[name] = true
	}
	return out
}

func upsertDocRow(rows map[string]Row, category, name, source, summary, firstAPI string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	id := rowID(category, name)
	row := rows[id]
	if row.ID == "" {
		row = Row{ID: id, Category: category, Name: name, Status: StatusDocsOnly, OracleStatus: StatusOracleMissing}
	}
	row.DocsSource = firstNonEmpty(row.DocsSource, source)
	row.Summary = firstNonEmpty(row.Summary, summary)
	row.FirstAPI = firstNonEmpty(row.FirstAPI, firstAPI)
	rows[id] = row
}

func sortedRows(rows map[string]Row) []Row {
	out := make([]Row, 0, len(rows))
	for _, row := range rows {
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Category != out[j].Category {
			return out[i].Category < out[j].Category
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func summarize(rows []Row) Summary {
	summary := Summary{Total: len(rows), ByCategory: map[string]int{}, ByStatus: map[string]int{}}
	for _, row := range rows {
		summary.ByCategory[row.Category]++
		summary.ByStatus[row.Status]++
	}
	return summary
}

func WriteJSON(w io.Writer, report Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

func WriteMarkdown(w io.Writer, report Report) error {
	var b strings.Builder
	b.WriteString("# Native LWC API Parity Ledger\n\n")
	fmt.Fprintf(&b, "Docs source: `%s`\n\n", report.DocsDir)
	fmt.Fprintf(&b, "Rows: %d\n\n", report.Summary.Total)

	b.WriteString("## Status\n\n")
	b.WriteString("| Status | Count |\n| --- | ---: |\n")
	for _, key := range sortedKeys(report.Summary.ByStatus) {
		fmt.Fprintf(&b, "| `%s` | %d |\n", key, report.Summary.ByStatus[key])
	}

	b.WriteString("\n## Categories\n\n")
	b.WriteString("| Category | Count |\n| --- | ---: |\n")
	for _, key := range sortedKeys(report.Summary.ByCategory) {
		fmt.Fprintf(&b, "| `%s` | %d |\n", key, report.Summary.ByCategory[key])
	}

	b.WriteString("\n## Rows\n\n")
	b.WriteString("| Category | Name | Status | Oracle | Evidence | Source | Notes |\n")
	b.WriteString("| --- | --- | --- | --- | --- | --- | --- |\n")
	for _, row := range report.Rows {
		fmt.Fprintf(&b, "| `%s` | `%s` | `%s` | `%s` | %s | %s | %s |\n",
			row.Category,
			row.Name,
			row.Status,
			row.OracleStatus,
			mdCell(row.LocalEvidence),
			mdCell(row.DocsSource),
			mdCell(row.Notes),
		)
	}

	b.WriteString("\n## Next Gates\n\n")
	for _, gate := range report.NextGates {
		fmt.Fprintf(&b, "- **%s**: `%s`\n", gate.Name, gate.Command)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func firstMarkdownLinkLabel(line string) (string, bool) {
	start := strings.Index(line, "[")
	if start < 0 {
		return "", false
	}
	end := strings.Index(line[start+1:], "]")
	if end < 0 {
		return "", false
	}
	return line[start+1 : start+1+end], true
}

func markdownCells(line string) []string {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "|") {
		return nil
	}
	parts := strings.Split(line, "|")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

func cleanDocName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.Trim(name, "`")
	name = strings.ReplaceAll(name, "(Beta)", "")
	name = strings.ReplaceAll(name, "(Deprecated)", "")
	name = strings.ReplaceAll(name, "`", "")
	return strings.TrimSpace(name)
}

func normalizeSalesforceModule(name string) string {
	name = strings.TrimSpace(name)
	name = strings.Trim(name, "`.,;:)")
	for _, family := range []string{
		"@salesforce/apex/",
		"@salesforce/client/",
		"@salesforce/community/",
		"@salesforce/contentAssetUrl/",
		"@salesforce/customPermission/",
		"@salesforce/i18n/",
		"@salesforce/label/",
		"@salesforce/messageChannel/",
		"@salesforce/resourceUrl/",
		"@salesforce/schema/",
		"@salesforce/site/",
		"@salesforce/user/",
		"@salesforce/userPermission/",
	} {
		if strings.TrimSuffix(name, "/") == strings.TrimSuffix(family, "/") {
			return family
		}
	}
	return name
}

func relDocSource(path string) string {
	return filepath.Base(path)
}

func rowID(category, name string) string {
	return category + ":" + name
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func joinNotes(values ...string) string {
	parts := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			parts = append(parts, value)
		}
	}
	return strings.Join(parts, "; ")
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func mdCell(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "|", "\\|")
	return strings.TrimSpace(s)
}
