package corpuscheck

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type Options struct {
	Root               string
	Glade              string
	OutDir             string
	FailOnUnclassified bool
	FailOnCheckClosure bool
	MaxUnclassified    int
}

type Report struct {
	Summary     ReportSummary
	Projects    []ProjectResult
	Diagnostics []ClassifiedDiagnostic
	Counts      map[string]int
}

type ReportSummary struct {
	ProjectCount         int
	DiagnosticCount      int
	UnclassifiedCount    int
	ClosureBlockingCount int
}

type ProjectResult struct {
	Name        string
	Path        string
	ExitCode    int
	Diagnostics int
	Error       string
}

type ClassifiedDiagnostic struct {
	Project  string
	Code     string
	Stem     string
	Class    string
	File     string
	Line     int
	Column   int
	Severity string
	Message  string
	Raw      string
}

func Check(ctx context.Context, options Options) (Report, error) {
	if strings.TrimSpace(options.Root) == "" {
		return Report{}, errors.New("--root requires a value")
	}
	if strings.TrimSpace(options.Glade) == "" {
		return Report{}, errors.New("--glade requires a value")
	}
	if strings.TrimSpace(options.OutDir) == "" {
		return Report{}, errors.New("--out requires a value")
	}
	if err := rejectStaleGeneratedReports(options.OutDir); err != nil {
		return Report{}, err
	}
	projects, err := discoverProjects(options.Root)
	if err != nil {
		return Report{}, err
	}
	report := Report{Counts: map[string]int{}}
	for _, project := range projects {
		result, diagnostics := runProject(ctx, options.Glade, project)
		report.Projects = append(report.Projects, result)
		for _, diagnostic := range diagnostics {
			report.Counts[diagnostic.Class]++
			report.Diagnostics = append(report.Diagnostics, diagnostic)
		}
	}
	sort.Slice(report.Diagnostics, func(i, j int) bool {
		a, b := report.Diagnostics[i], report.Diagnostics[j]
		return strings.Join([]string{a.Project, a.Code, a.File, a.Message}, "\x00") < strings.Join([]string{b.Project, b.Code, b.File, b.Message}, "\x00")
	})
	report.Summary = summarizeReport(report)
	if err := writeReport(options.OutDir, report); err != nil {
		return Report{}, err
	}
	if options.FailOnUnclassified && report.Summary.UnclassifiedCount > options.MaxUnclassified {
		return report, fmt.Errorf("unclassified=%d exceeds max %d", report.Summary.UnclassifiedCount, options.MaxUnclassified)
	}
	if options.FailOnCheckClosure {
		if disallowed := DisallowedForCheckClosure(report); len(disallowed) > 0 {
			return report, fmt.Errorf("public check closure failed: %v", disallowed)
		}
	}
	return report, nil
}

func discoverProjects(root string) ([]string, error) {
	var projects []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			name := entry.Name()
			if strings.HasPrefix(name, ".") && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Name() != "sfdx-project.json" {
			return nil
		}
		projects = append(projects, filepath.Dir(path))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(projects)
	return removeAggregateProjectRoots(projects), nil
}

func removeAggregateProjectRoots(projects []string) []string {
	sort.Strings(projects)
	nestedUnder := map[string]bool{}
	for _, parent := range projects {
		parentWithSep := parent + string(os.PathSeparator)
		for _, child := range projects {
			if child != parent && strings.HasPrefix(child, parentWithSep) {
				nestedUnder[parent] = true
				break
			}
		}
	}
	out := projects[:0]
	for _, project := range projects {
		if !nestedUnder[project] {
			out = append(out, project)
		}
	}
	return out
}

func DisallowedForCheckClosure(report Report) map[string]int {
	disallowed := map[string]int{}
	for class, count := range report.Counts {
		switch class {
		case "performance-advisory", "project-discovery-duplicate", "project-metadata-missing", "project-source-invalid", "explicit-unsupported", "generated-shape-gap", "platform-shaped":
			continue
		default:
			if count > 0 {
				disallowed[class] = count
			}
		}
	}
	return disallowed
}

func summarizeReport(report Report) ReportSummary {
	closureBlocking := 0
	for _, count := range DisallowedForCheckClosure(report) {
		closureBlocking += count
	}
	return ReportSummary{
		ProjectCount:         len(report.Projects),
		DiagnosticCount:      len(report.Diagnostics),
		UnclassifiedCount:    report.Counts["unclassified"],
		ClosureBlockingCount: closureBlocking,
	}
}

func rejectStaleGeneratedReports(outDir string) error {
	info, err := os.Stat(outDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return nil
	}
	var stale []string
	err = filepath.WalkDir(outDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		name := entry.Name()
		if strings.HasPrefix(name, "SURFACE_") {
			rel, relErr := filepath.Rel(outDir, path)
			if relErr != nil {
				rel = path
			}
			stale = append(stale, rel)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(stale) == 0 {
		return nil
	}
	sort.Strings(stale)
	return fmt.Errorf("stale generated report files in --out %s: %s; remove them or choose a fresh --out", outDir, strings.Join(stale, ", "))
}

func runProject(ctx context.Context, glade string, project string) (ProjectResult, []ClassifiedDiagnostic) {
	cmd := exec.CommandContext(ctx, glade, "check", "--project", project, "--format", "json", "--no-progress")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	exitCode := 0
	if err != nil {
		exitCode = 1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
			out = append(out, exitErr.Stderr...)
		}
	}
	projectName := filepath.Base(project)
	result := ProjectResult{Name: projectName, Path: project, ExitCode: exitCode}
	diagnostics, parseErr := parseDiagnostics(projectName, out)
	if parseErr != nil {
		raw := strings.TrimSpace(string(out))
		if raw == "" {
			raw = strings.TrimSpace(stderr.String())
		}
		result.Error = parseErr.Error()
		diagnostics = []ClassifiedDiagnostic{{
			Project: projectName,
			Code:    "INVALID_JSON",
			Stem:    "INVALID_JSON",
			Class:   "unclassified",
			Message: parseErr.Error(),
			Raw:     raw,
		}}
	}
	result.Diagnostics = len(diagnostics)
	return result, diagnostics
}

func parseDiagnostics(project string, data []byte) ([]ClassifiedDiagnostic, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, nil
	}
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	items := collectDiagnosticObjects(raw)
	out := make([]ClassifiedDiagnostic, 0, len(items))
	for _, item := range items {
		encoded, _ := json.Marshal(item)
		diag := ClassifiedDiagnostic{
			Project:  project,
			Code:     stringField(item, "code"),
			File:     firstStringField(item, "file", "path", "uri"),
			Line:     intField(item, "line"),
			Column:   intField(item, "column"),
			Severity: stringField(item, "severity"),
			Message:  stringField(item, "message"),
			Raw:      string(encoded),
		}
		diag.Stem = diagnosticStem(diag.Code)
		diag.Class = Classify(diag)
		out = append(out, diag)
	}
	return out, nil
}

func collectDiagnosticObjects(value any) []map[string]any {
	switch typed := value.(type) {
	case []any:
		var out []map[string]any
		for _, item := range typed {
			out = append(out, collectDiagnosticObjects(item)...)
		}
		return out
	case map[string]any:
		if _, ok := typed["code"]; ok {
			return []map[string]any{typed}
		}
		for _, key := range []string{"diagnostics", "Diagnostics", "results", "files"} {
			if child, ok := typed[key]; ok {
				if out := collectDiagnosticObjects(child); len(out) > 0 {
					return out
				}
			}
		}
	}
	return nil
}

func Classify(diag ClassifiedDiagnostic) string {
	code := strings.ToUpper(diag.Code)
	text := strings.ToLower(diag.Project + " " + diag.Message + " " + diag.File)
	switch {
	case strings.Contains(code, "APEXPARSE"):
		return "source-parse-error"
	case strings.Contains(code, "GLADEPERF"):
		return "performance-advisory"
	case code == "GLADETYPE001" || strings.Contains(text, "duplicate declaration"):
		return "project-discovery-duplicate"
	case diagnosticLooksLikeMissingMetadata(code, text):
		return "project-metadata-missing"
	case diagnosticLooksLikeProjectSourceInvalid(code, text):
		return "project-source-invalid"
	case diagnosticLooksLikeDocsContractMismatch(code, text):
		return "docs-contract-mismatch"
	case diagnosticLooksLikeGeneratedShapeGap(code, text):
		return "generated-shape-gap"
	case diagnosticLooksLikeExplicitUnsupported(code, text):
		return "explicit-unsupported"
	case diagnosticLooksLikePlatformShaped(code, text):
		return "platform-shaped"
	case strings.HasPrefix(code, "GLADESEMA"):
		return "semantic-contract-gap"
	default:
		return "unclassified"
	}
}

func diagnosticLooksLikeMissingMetadata(code, text string) bool {
	if code == "GLADESEMA_QUERY_OBJECT" && (strings.Contains(text, "__c") || strings.Contains(text, "__mdt") || strings.Contains(text, "__e")) {
		return true
	}
	if code == "GLADESEMA_QUERY_RELATIONSHIP" {
		return true
	}
	if strings.Contains(text, "metadata") || strings.Contains(text, "custom field") {
		return true
	}
	if diagnosticTextMentionsMissingPackageSource(text) {
		return true
	}
	if strings.Contains(text, "unknown variable") && (strings.Contains(text, "znu.") || strings.Contains(text, "znu__")) {
		return true
	}
	if strings.Contains(text, "unknown relationship") || strings.Contains(text, "relationship path") {
		return true
	}
	if strings.Contains(text, "calls unknown method") && mentionsCustomFieldPath(text) {
		return true
	}
	if strings.Contains(text, "unknown expression type") && quotedDiagnosticNameLooksMetadata(text, "unknown expression type \"") {
		return true
	}
	if strings.Contains(text, "unknown expression type") && quotedDiagnosticNameLooksMissingPackageSource(text, "unknown expression type \"") {
		return true
	}
	if strings.Contains(text, "unknown constructor target") && quotedDiagnosticNameLooksMetadata(text, "unknown constructor target \"") {
		return true
	}
	for _, prefix := range []string{
		"unknown type \"",
		"constructs unknown type \"",
		"declares local \"",
		"declares enhanced-for local \"",
	} {
		if quotedDiagnosticNameLooksMissingPackageSource(text, prefix) {
			return true
		}
	}
	if !strings.Contains(text, "unknown type") && !strings.Contains(text, "unknown field") {
		return false
	}
	return strings.Contains(text, "__c") ||
		strings.Contains(text, "__mdt") ||
		strings.Contains(text, "__e") ||
		unknownQuotedTypeLooksNamespaced(text)
}

func diagnosticLooksLikeProjectSourceInvalid(code, text string) bool {
	if code == "GLADESEMA018" && strings.Contains(text, "initializes map<") && strings.Contains(text, " with list<") {
		return true
	}
	if code == "GLADESEMA018" && diagnosticTextLooksFabricatedHelperAssignment(text) {
		return true
	}
	if code == "GLADESEMA019" && (strings.Contains(text, " from imodel method") || strings.Contains(text, " from list<imodel> method")) {
		return true
	}
	if code == "GLADESEMA023" && diagnosticTextLooksMissingQueryPluginCollectionCascade(text) {
		return true
	}
	if code == "GLADESEMA027" && strings.Contains(text, "static method called through an instance") {
		return true
	}
	if code == "GLADESEMA011" && (strings.Contains(text, "registrationhistory constructor") || strings.Contains(text, "miscellaneoushistory constructor")) {
		return true
	}
	if code == "GLADESEMA008" && strings.Contains(text, "calls unknown method") && strings.Contains(text, "getpicklistvaluesbyobjectfieldold") {
		return true
	}
	if strings.Contains(text, "runleadassignmentrules") && (strings.Contains(text, "invalid collection call") || strings.Contains(text, "unknown field \"leadids\"")) {
		return true
	}
	if strings.Contains(text, "createoreditlistview") && strings.Contains(text, "unknown field \"success\"") {
		return true
	}
	if strings.Contains(text, "sendhtmlemailtest") && strings.Contains(text, "unknown variable \"sendto") {
		return true
	}
	if strings.Contains(text, "webhook2flow") && strings.Contains(text, "unknown variable \"retjson\"") {
		return true
	}
	return false
}

func diagnosticLooksLikeDocsContractMismatch(code, text string) bool {
	return strings.Contains(text, "return-type mismatch") ||
		strings.Contains(text, "parameter mismatch") ||
		strings.Contains(text, "docs return") ||
		strings.Contains(text, "docs parameter") ||
		strings.Contains(text, "docs contract")
}

func diagnosticLooksLikeGeneratedShapeGap(code, text string) bool {
	if !strings.HasPrefix(code, "GLADEGEN") {
		return false
	}
	return strings.Contains(text, "generated shape") ||
		strings.Contains(text, "generated standard symbol") ||
		strings.Contains(text, "shape gap")
}

func diagnosticLooksLikeExplicitUnsupported(code, text string) bool {
	switch code {
	case "GLADELWC050", "GLADELWC060", "GLADELWC061":
		return strings.Contains(text, "unsupported")
	default:
		return false
	}
}

func diagnosticLooksLikePlatformShaped(code, text string) bool {
	switch code {
	case "GLADELWC091", "GLADELWC092", "GLADELWC093", "GLADELWC094", "GLADELWC095":
		return strings.Contains(text, "platform") || strings.Contains(text, "salesforce")
	default:
		return false
	}
}

func mentionsCustomFieldPath(text string) bool {
	return strings.Contains(text, "__c.") ||
		strings.Contains(text, "__mdt.") ||
		strings.Contains(text, "__e.") ||
		strings.Contains(text, "__r.")
}

func unknownQuotedTypeLooksNamespaced(text string) bool {
	typeName, ok := quotedDiagnosticName(text, "unknown type \"")
	return ok && strings.Contains(typeName, ".")
}

func quotedDiagnosticNameLooksMetadata(text, prefix string) bool {
	name, ok := quotedDiagnosticName(text, prefix)
	if !ok {
		return false
	}
	return strings.Contains(name, ".") ||
		strings.HasSuffix(name, "__c") ||
		strings.HasSuffix(name, "__mdt") ||
		strings.HasSuffix(name, "__e")
}

func quotedDiagnosticNameLooksMissingPackageSource(text, prefix string) bool {
	name, ok := quotedDiagnosticName(text, prefix)
	if !ok {
		return false
	}
	return diagnosticNameLooksMissingPackageSource(name)
}

func diagnosticNameLooksMissingPackageSource(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	if lower == "" {
		return false
	}
	switch lower {
	case "dataaccess", "mockhttpresponsegenerator", "moduleexception":
		return true
	}
	for _, prefix := range []string{"fflib_", "di_", "usf3.", "metadataservice.", "sfab_", "znu.", "znu__"} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	for _, name := range []string{
		"accountbase",
		"accountdto",
		"accountsapiaccountretriever",
		"addressdto",
		"calculatemembershipterm2",
		"currenciescommand",
		"currenciesservice",
		"currentuser",
		"fieldsetfield",
		"fieldsetwrapper",
		"getmembershipsterm2command",
		"getmembershiptype2",
		"getmembershiptype2command",
		"getmembershiptypewithitems2",
		"getrecordtypes",
		"getrecordtypescommand",
		"ilogger",
		"imodel",
		"invalidargumentexception",
		"logcontext",
		"loggerlevel",
		"membershiptermdto2",
		"membershiptypedto2",
		"orderline",
		"orderlinetestdata",
		"ordersapilinefactory",
		"ordersapilineretriever",
		"persistenceservice",
		"pluggable",
		"product",
		"productbaseadapter",
		"productretrieveradapter",
		"q",
		"qcondition",
		"qorder",
		"qplugin",
		"qrunner",
		"recordtypedto",
		"resourcenotfoundexception",
		"routenotfoundexception",
		"restroute",
		"sobjectdatatable",
		"trace",
		"triggeringcontext",
		"unhandledapexexception",
	} {
		if lower == name {
			return true
		}
	}
	return false
}

func diagnosticTextMentionsMissingPackageSource(text string) bool {
	if strings.Contains(text, "fflib_") || strings.Contains(text, "di_") || strings.Contains(text, "usf3.") || strings.Contains(text, "metadataservice.") ||
		strings.Contains(text, "sfab_") {
		return true
	}
	for _, marker := range []string{
		"znu.bankaccountdetail",
		"znu.creditcarddetail",
		"znu.membershiporderitemtypecartsubmitter",
		"znu.pluggable",
		"znu.productbase",
		"znu.qplugin",
		"znu.standardbulkpricingmanager",
		"znu__",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	for _, marker := range []string{
		"from \"iapplicationsobjectselector\"",
		"response.result.",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func diagnosticTextLooksFabricatedHelperAssignment(text string) bool {
	if strings.Contains(text, "fabricated") || strings.Contains(text, "productfab") {
		return true
	}
	for _, marker := range []string{
		"initializes orderitem",
		"initializes passcode",
		"initializes product",
		"initializes previousorder",
		"initializes waitlistrecord",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func diagnosticTextLooksMissingQueryPluginCollectionCascade(text string) bool {
	for _, marker := range []string{
		"getfilterplugins",
		"getpluginsforconditions",
		"abletosetwithmultiplenuqplugins",
	} {
		if strings.Contains(text, marker) && strings.Contains(text, "invalid collection call \"add\"") {
			return true
		}
	}
	return false
}

func quotedDiagnosticName(text, prefix string) (string, bool) {
	start := strings.Index(text, prefix)
	if start < 0 {
		return "", false
	}
	start += len(prefix)
	end := strings.Index(text[start:], "\"")
	if end < 0 {
		return "", false
	}
	return text[start : start+end], true
}

func writeReport(outDir string, report Report) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	files := map[string]string{
		"summary.tsv":         summaryTSV(report),
		"diagnostics.tsv":     diagnosticsTSV(report.Diagnostics),
		"by_code.tsv":         countTSV(countBy(report.Diagnostics, func(d ClassifiedDiagnostic) string { return d.Code })),
		"by_project_code.tsv": countTSV(countBy(report.Diagnostics, func(d ClassifiedDiagnostic) string { return d.Project + "\t" + d.Code })),
		"by_stem.tsv":         countTSV(countBy(report.Diagnostics, func(d ClassifiedDiagnostic) string { return d.Stem })),
		"classified.tsv":      countTSV(report.Counts),
	}
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(outDir, name), []byte(text), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func summaryTSV(report Report) string {
	var b strings.Builder
	b.WriteString("project\tpath\texitCode\tdiagnostics\terror\n")
	for _, project := range report.Projects {
		fmt.Fprintf(&b, "%s\t%s\t%d\t%d\t%s\n", tsv(project.Name), tsv(project.Path), project.ExitCode, project.Diagnostics, tsv(project.Error))
	}
	return b.String()
}

func diagnosticsTSV(diagnostics []ClassifiedDiagnostic) string {
	var b strings.Builder
	b.WriteString("project\tclass\tcode\tstem\tfile\tline\tcolumn\tseverity\tmessage\traw\n")
	for _, diag := range diagnostics {
		fmt.Fprintf(&b, "%s\t%s\t%s\t%s\t%s\t%d\t%d\t%s\t%s\t%s\n", tsv(diag.Project), tsv(diag.Class), tsv(diag.Code), tsv(diag.Stem), tsv(diag.File), diag.Line, diag.Column, tsv(diag.Severity), tsv(diag.Message), tsv(diag.Raw))
	}
	return b.String()
}

func countBy(diagnostics []ClassifiedDiagnostic, key func(ClassifiedDiagnostic) string) map[string]int {
	out := map[string]int{}
	for _, diag := range diagnostics {
		out[key(diag)]++
	}
	return out
}

func countTSV(counts map[string]int) string {
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("key\tcount\n")
	for _, key := range keys {
		fmt.Fprintf(&b, "%s\t%d\n", tsv(key), counts[key])
	}
	return b.String()
}

func diagnosticStem(code string) string {
	if idx := strings.IndexByte(code, '-'); idx > 0 {
		return code[:idx]
	}
	return code
}

func tsv(value string) string {
	value = strings.ReplaceAll(value, "\t", " ")
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	return value
}

func stringField(item map[string]any, key string) string {
	if value, ok := item[key].(string); ok {
		return value
	}
	return ""
}

func firstStringField(item map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringField(item, key); value != "" {
			return value
		}
	}
	return ""
}

func intField(item map[string]any, key string) int {
	if value, ok := item[key].(float64); ok {
		return int(value)
	}
	return 0
}
