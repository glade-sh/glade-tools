package apexdocs

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

const InventorySchemaVersion = 1

type Inventory struct {
	SchemaVersion int                `json:"schemaVersion"`
	TotalFiles    int                `json:"totalFiles"`
	TotalMembers  int                `json:"totalMembers"`
	Namespaces    []NamespaceSummary `json:"namespaces"`
	Documents     []Document         `json:"documents"`
}

type NamespaceSummary struct {
	Namespace  string `json:"namespace"`
	Documents  int    `json:"documents"`
	Namespaces int    `json:"namespaces,omitempty"`
	Classes    int    `json:"classes,omitempty"`
	Interfaces int    `json:"interfaces,omitempty"`
	Enums      int    `json:"enums,omitempty"`
	Inputs     int    `json:"inputs,omitempty"`
	Outputs    int    `json:"outputs,omitempty"`
	Members    int    `json:"members"`
}

type Document struct {
	SourcePath   string        `json:"sourcePath"`
	Kind         string        `json:"kind"`
	Namespace    string        `json:"namespace,omitempty"`
	Name         string        `json:"name"`
	Title        string        `json:"title,omitempty"`
	Signature    string        `json:"signature,omitempty"`
	APIVersion   string        `json:"apiVersion,omitempty"`
	InternalOnly bool          `json:"internalOnly,omitempty"`
	Headings     []string      `json:"headings,omitempty"`
	Members      []Member      `json:"members,omitempty"`
	Examples     []Example     `json:"examples,omitempty"`
	Behaviors    []DocBehavior `json:"behaviors,omitempty"`
}

type Member struct {
	Kind         string   `json:"kind"`
	Name         string   `json:"name"`
	Signature    string   `json:"signature"`
	APIVersion   string   `json:"apiVersion,omitempty"`
	ReturnType   string   `json:"returnType,omitempty"`
	PropertyType string   `json:"propertyType,omitempty"`
	Parameters   []string `json:"parameters,omitempty"`
	Section      string   `json:"section,omitempty"`
	Description  string   `json:"description,omitempty"`
}

// DocBehavior is a behavioral constraint mined from a doc's Usage prose, such
// as "this method can't be used in a test method" or "treated as a callout".
// These are the contract sentences that hand-patched runtime stubs routinely
// drop; capturing them lets the runtime honor real Salesforce semantics.
type DocBehavior struct {
	Kind     string `json:"kind"`
	Evidence string `json:"evidence,omitempty"`
}

type Example struct {
	Heading string `json:"heading"`
	Snippet string `json:"snippet,omitempty"`
}

type Diff struct {
	AddedDocuments   []string       `json:"addedDocuments,omitempty"`
	RemovedDocuments []string       `json:"removedDocuments,omitempty"`
	ChangedDocuments []DocumentDiff `json:"changedDocuments,omitempty"`
}

type DocumentDiff struct {
	SourcePath     string   `json:"sourcePath"`
	AddedMembers   []string `json:"addedMembers,omitempty"`
	RemovedMembers []string `json:"removedMembers,omitempty"`
	OldIdentity    string   `json:"oldIdentity,omitempty"`
	NewIdentity    string   `json:"newIdentity,omitempty"`
}

func BuildInventory(root string) (Inventory, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return Inventory{}, err
	}
	info, err := os.Stat(absRoot)
	if err != nil {
		return Inventory{}, err
	}
	if !info.IsDir() {
		return Inventory{}, fmt.Errorf("docs source is not a directory: %s", root)
	}

	var docs []Document
	err = filepath.WalkDir(absRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.EqualFold(filepath.Ext(path), ".md") {
			return nil
		}
		rel, err := filepath.Rel(absRoot, path)
		if err != nil {
			return err
		}
		doc, err := parseDocument(path, filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		docs = append(docs, doc)
		return nil
	})
	if err != nil {
		return Inventory{}, err
	}

	sort.Slice(docs, func(i, j int) bool {
		return docs[i].SourcePath < docs[j].SourcePath
	})

	inv := Inventory{
		SchemaVersion: InventorySchemaVersion,
		TotalFiles:    len(docs),
		Documents:     docs,
	}
	inv.Namespaces, inv.TotalMembers = summarize(docs)
	return inv, nil
}

func WriteJSON(w io.Writer, inv Inventory) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(inv)
}

func ReadInventory(path string) (Inventory, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Inventory{}, err
	}
	var inv Inventory
	if err := json.Unmarshal(data, &inv); err != nil {
		return Inventory{}, err
	}
	return inv, nil
}

func DiffInventories(oldInv, newInv Inventory) Diff {
	oldDocs := map[string]Document{}
	newDocs := map[string]Document{}
	for _, doc := range oldInv.Documents {
		oldDocs[doc.SourcePath] = doc
	}
	for _, doc := range newInv.Documents {
		newDocs[doc.SourcePath] = doc
	}

	var diff Diff
	for path := range newDocs {
		if _, ok := oldDocs[path]; !ok {
			diff.AddedDocuments = append(diff.AddedDocuments, path)
		}
	}
	for path := range oldDocs {
		if _, ok := newDocs[path]; !ok {
			diff.RemovedDocuments = append(diff.RemovedDocuments, path)
		}
	}
	for path, newDoc := range newDocs {
		oldDoc, ok := oldDocs[path]
		if !ok {
			continue
		}
		docDiff := compareDocument(oldDoc, newDoc)
		if docDiff.OldIdentity != "" || len(docDiff.AddedMembers) > 0 || len(docDiff.RemovedMembers) > 0 {
			diff.ChangedDocuments = append(diff.ChangedDocuments, docDiff)
		}
	}
	sort.Strings(diff.AddedDocuments)
	sort.Strings(diff.RemovedDocuments)
	sort.Slice(diff.ChangedDocuments, func(i, j int) bool {
		return diff.ChangedDocuments[i].SourcePath < diff.ChangedDocuments[j].SourcePath
	})
	return diff
}

func WriteDiffJSON(w io.Writer, diff Diff) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(diff)
}

func HasChanges(diff Diff) bool {
	return len(diff.AddedDocuments) > 0 || len(diff.RemovedDocuments) > 0 || len(diff.ChangedDocuments) > 0
}

func parseDocument(path, rel string) (Document, error) {
	file, err := os.Open(path)
	if err != nil {
		return Document{}, err
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return Document{}, err
	}

	title := firstHeading(lines, "# ")
	meta := inferMetadata(rel, title)
	doc := Document{
		SourcePath:   rel,
		Kind:         meta.kind,
		Namespace:    meta.namespace,
		Name:         meta.name,
		Title:        title,
		Signature:    documentSignature(lines),
		InternalOnly: introIsInternalOnly(lines),
	}
	doc.Headings = collectHeadings(lines)
	doc.Members = collectMembers(lines, doc.Name)
	if len(doc.Members) == 0 {
		doc.APIVersion = documentAPIVersion(lines)
	} else {
		doc.APIVersion = completeMembersAPIVersion(doc.Members)
	}
	doc.Examples = collectExamples(lines)
	doc.Behaviors = collectBehaviors(lines)
	if doc.Namespace == "" {
		doc.Namespace = namespaceFromSection(lines)
	}
	return doc, nil
}

func introIsInternalOnly(lines []string) bool {
	var intro strings.Builder
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "## ") {
			break
		}
		intro.WriteString(strings.ToLower(line))
		intro.WriteByte(' ')
	}
	text := strings.Join(strings.Fields(intro.String()), " ")
	return strings.Contains(text, "for internal use only") ||
		strings.Contains(text, "reserved for internal use") ||
		strings.Contains(text, "reserved for future use")
}

var (
	availableAPIVersionPattern = regexp.MustCompile(`(?i)Available\s+(?:in|from)\s+(?:Tooling\s+)?API\s+versions?\s+([0-9]+(?:\.[0-9]+)?)`)
	canonicalAPIVersionPattern = regexp.MustCompile(`^[1-9][0-9]*(?:\.0)?$`)
)

func documentAPIVersion(lines []string) string {
	return sectionAPIVersion(lines, "## ")
}

func memberAPIVersion(lines []string) string {
	return sectionAPIVersion(lines, "#### ")
}

func sectionAPIVersion(lines []string, headingPrefix string) string {
	earliest := ""
	add := func(value string) {
		value = canonicalAPIVersion(value)
		if value == "" {
			return
		}
		if earliest == "" || apiVersionMajor(value) < apiVersionMajor(earliest) {
			earliest = value
		}
	}
	introEnd := len(lines)
	for index, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "##") {
			introEnd = index
			break
		}
	}
	for _, match := range availableAPIVersionPattern.FindAllStringSubmatch(strings.Join(lines[:introEnd], "\n"), -1) {
		add(match[1])
	}
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, headingPrefix) || !strings.EqualFold(strings.TrimSpace(strings.TrimPrefix(trimmed, headingPrefix)), "API Version") {
			continue
		}
		for _, next := range lines[i+1:] {
			next = strings.TrimSpace(next)
			if next == "" {
				continue
			}
			add(next)
			break
		}
	}
	for i, line := range lines {
		header := markdownTableCells(line)
		versionIndex := -1
		for index, cell := range header {
			name := strings.ToLower(strings.Join(strings.Fields(stripMarkdownInline(cell)), " "))
			if name == "available version" || name == "api version" {
				versionIndex = index
				break
			}
		}
		if versionIndex < 0 {
			continue
		}
		for _, next := range lines[i+1:] {
			row := markdownTableCells(next)
			if len(row) == 0 {
				break
			}
			if !isMarkdownSeparatorRow(row) && versionIndex < len(row) {
				add(stripMarkdownInline(row[versionIndex]))
			}
		}
	}
	return earliest
}

func canonicalAPIVersion(value string) string {
	value = strings.TrimSpace(value)
	if !canonicalAPIVersionPattern.MatchString(value) {
		return ""
	}
	if !strings.Contains(value, ".") {
		value += ".0"
	}
	return value
}

func apiVersionMajor(value string) int {
	major, _ := strconv.Atoi(strings.TrimSuffix(value, ".0"))
	return major
}

func completeMembersAPIVersion(members []Member) string {
	earliest := 0
	for _, member := range members {
		major, err := strconv.Atoi(strings.TrimSuffix(member.APIVersion, ".0"))
		if err != nil || major == 0 {
			return ""
		}
		if earliest == 0 || major < earliest {
			earliest = major
		}
	}
	if earliest == 0 {
		return ""
	}
	return strconv.Itoa(earliest) + ".0"
}

func documentSignature(lines []string) string {
	for i, line := range lines {
		if strings.TrimSpace(line) == "## Signature" {
			return readSignatureCode(lines[i+1:])
		}
	}
	return ""
}

type documentMeta struct {
	kind      string
	namespace string
	name      string
}

func inferMetadata(rel, title string) documentMeta {
	base := strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel))
	titleName := nameFromTitle(title)
	meta := documentMeta{kind: "document", name: titleName}
	switch {
	case strings.HasPrefix(base, "apex_methods_system_"):
		meta.kind = "class"
		meta.namespace = "System"
		meta.name = titleName
	case strings.HasPrefix(base, "apex_System_") && strings.HasSuffix(base, "_methods"):
		meta.kind = "class"
		meta.namespace = "System"
		meta.name = strings.TrimSuffix(strings.TrimPrefix(base, "apex_System_"), "_methods")
	case strings.HasPrefix(base, "apex_class_"):
		meta.kind = "class"
		meta.namespace, meta.name = splitNamespaceName(strings.TrimPrefix(base, "apex_class_"), titleName)
	case strings.HasPrefix(base, "apex_interface_"):
		meta.kind = "interface"
		meta.namespace, meta.name = splitNamespaceName(strings.TrimPrefix(base, "apex_interface_"), titleName)
	case strings.HasPrefix(base, "apex_enum_"):
		meta.kind = "enum"
		meta.namespace, meta.name = splitNamespaceName(strings.TrimPrefix(base, "apex_enum_"), titleName)
	case strings.HasPrefix(base, "apex_namespace_"):
		meta.kind = "namespace"
		meta.name = strings.TrimPrefix(base, "apex_namespace_")
		meta.namespace = meta.name
	case strings.HasPrefix(base, "apex_connectapi_input_"):
		meta.kind = "input"
		meta.namespace = "ConnectApi"
		meta.name = titleName
	case strings.HasPrefix(base, "apex_connectapi_output_"):
		meta.kind = "output"
		meta.namespace = "ConnectApi"
		meta.name = titleName
	}
	if meta.name == "" {
		meta.name = fallbackName(base)
	}
	return meta
}

func splitNamespaceName(value, fallback string) (string, string) {
	parts := strings.SplitN(value, "_", 2)
	if len(parts) == 1 {
		return "", fallbackOr(parts[0], fallback)
	}
	return parts[0], fallbackOr(parts[1], fallback)
}

func fallbackOr(value, fallback string) string {
	if fallback != "" {
		return fallback
	}
	return value
}

func fallbackName(base string) string {
	base = strings.TrimPrefix(base, "apex_")
	if base == "" {
		return "unknown"
	}
	return base
}

func nameFromTitle(title string) string {
	title = strings.TrimSpace(title)
	for _, suffix := range []string{
		" Class",
		" Methods",
		" Method",
		" Constructors",
		" Constructor",
		" Enum",
		" Interface",
	} {
		title = strings.TrimSuffix(title, suffix)
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return ""
	}
	return strings.Fields(title)[0]
}

func firstHeading(lines []string, prefix string) string {
	for _, line := range lines {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return ""
}

func namespaceFromSection(lines []string) string {
	for i, line := range lines {
		if strings.TrimSpace(line) != "## Namespace" {
			continue
		}
		for _, next := range lines[i+1:] {
			next = strings.TrimSpace(next)
			if next == "" {
				continue
			}
			if strings.HasPrefix(next, "#") {
				return ""
			}
			if strings.HasPrefix(next, "[") {
				end := strings.Index(next, "]")
				if end > 1 {
					return next[1:end]
				}
			}
			return strings.Fields(next)[0]
		}
	}
	return ""
}

func collectHeadings(lines []string) []string {
	var headings []string
	for _, line := range lines {
		if strings.HasPrefix(line, "## ") || strings.HasPrefix(line, "### ") {
			headings = append(headings, strings.TrimSpace(strings.TrimLeft(line, "# ")))
		}
	}
	return headings
}

// Behavior kinds mined from doc Usage prose.
const (
	BehaviorUnavailableInTest  = "unavailable-in-test"
	BehaviorCalloutInTest      = "callout-in-test"
	BehaviorNotInTriggers      = "not-in-triggers"
	BehaviorNotInBatch         = "not-in-batch"
	BehaviorNotInScheduled     = "not-in-scheduled"
	BehaviorNotInEmailServices = "not-in-email-services"
	BehaviorNotInContext       = "not-in-context"
	BehaviorThrows             = "throws"
	BehaviorDeprecated         = "deprecated"
	BehaviorAvailableOnly      = "available-only"
)

var exceptionTypePattern = regexp.MustCompile(`[A-Z][A-Za-z0-9_.]*Exception`)

// sectionLines returns the lines under the first "## <title>" heading, up to
// the next "## " heading.
func sectionLines(lines []string, title string) []string {
	var out []string
	inSection := false
	for _, line := range lines {
		if strings.HasPrefix(line, "## ") {
			if inSection {
				break
			}
			heading := strings.TrimSpace(strings.TrimPrefix(line, "## "))
			if strings.EqualFold(heading, title) {
				inSection = true
			}
			continue
		}
		if inSection {
			out = append(out, line)
		}
	}
	return out
}

// normalizeProse lowercases, normalizes curly apostrophes, and collapses
// whitespace so multi-line phrases (e.g. "treated as\n a callout") match.
func normalizeProse(text string) string {
	text = strings.ReplaceAll(text, "\u2019", "'")
	text = strings.ToLower(text)
	return strings.Join(strings.Fields(text), " ")
}

// collectBehaviors mines behavioral constraints from a doc's Usage section. It
// detects phrase markers (e.g. "treated as a callout") across the whole section
// and classifies any "can't be used in:" bullet list into per-context markers.
func collectBehaviors(lines []string) []DocBehavior {
	usage := sectionLines(lines, "Usage")

	seen := map[string]bool{}
	var behaviors []DocBehavior
	add := func(kind, evidence string) {
		if kind == "" || seen[kind] {
			return
		}
		seen[kind] = true
		behaviors = append(behaviors, DocBehavior{Kind: kind, Evidence: strings.TrimSpace(evidence)})
	}

	// "deprecated" is a strong, unambiguous signal; scan the whole doc.
	if strings.Contains(normalizeProse(strings.Join(lines, "\n")), "deprecated") {
		add(BehaviorDeprecated, "deprecated")
	}

	if len(usage) == 0 {
		sort.SliceStable(behaviors, func(i, j int) bool { return behaviors[i].Kind < behaviors[j].Kind })
		return behaviors
	}
	rawUsage := strings.Join(usage, "\n")
	blob := normalizeProse(rawUsage)

	if strings.Contains(blob, "treated as a callout") {
		add(BehaviorCalloutInTest, "treated as a callout")
	}
	if strings.Contains(blob, "test method") &&
		(strings.Contains(blob, "fails") ||
			strings.Contains(blob, "can't be used") ||
			strings.Contains(blob, "cannot be used") ||
			strings.Contains(blob, "not supported")) {
		add(BehaviorUnavailableInTest, "not available in test methods")
	}
	if strings.Contains(blob, "available only") || strings.Contains(blob, "only available") {
		add(BehaviorAvailableOnly, "available only in certain contexts")
	}
	if strings.Contains(blob, "throws") {
		evidence := "throws an exception"
		if match := exceptionTypePattern.FindString(rawUsage); match != "" {
			evidence = "throws " + match
		}
		add(BehaviorThrows, evidence)
	}

	for _, item := range cantBeUsedInItems(usage) {
		lower := normalizeProse(item)
		switch {
		case strings.Contains(lower, "trigger"):
			add(BehaviorNotInTriggers, item)
		case strings.Contains(lower, "test"):
			add(BehaviorUnavailableInTest, item)
		case strings.Contains(lower, "email"):
			add(BehaviorNotInEmailServices, item)
		case strings.Contains(lower, "batch"):
			add(BehaviorNotInBatch, item)
		case strings.Contains(lower, "schedul"):
			add(BehaviorNotInScheduled, item)
		default:
			add(BehaviorNotInContext, item)
		}
	}

	sort.SliceStable(behaviors, func(i, j int) bool {
		return behaviors[i].Kind < behaviors[j].Kind
	})
	return behaviors
}

// cantBeUsedInItems returns the trimmed bullet items that follow a
// "can't be used in:" sentence in the Usage section.
func cantBeUsedInItems(usage []string) []string {
	start := -1
	for i, line := range usage {
		l := normalizeProse(line)
		if strings.Contains(l, "can't be used in") || strings.Contains(l, "cannot be used in") {
			start = i
			break
		}
	}
	if start < 0 {
		return nil
	}
	var items []string
	for _, line := range usage[start+1:] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if len(items) > 0 {
				break
			}
			continue
		}
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			items = append(items, strings.TrimSpace(trimmed[2:]))
			continue
		}
		if len(items) > 0 {
			// Continuation of the previous bullet (wrapped line).
			items[len(items)-1] = strings.TrimSpace(items[len(items)-1] + " " + trimmed)
			continue
		}
		break
	}
	return items
}

func collectMembers(lines []string, typeName string) []Member {
	var members []Member
	section := ""
	propertyMembers := collectPropertyTableMembers(lines)
	propertyTypes := map[string]string{}
	propertyVersions := map[string]string{}
	for _, member := range propertyMembers {
		propertyTypes[member.Name] = member.PropertyType
		propertyVersions[member.Name] = member.APIVersion
	}
	seen := map[string]bool{}
	for i, line := range lines {
		if strings.HasPrefix(line, "## ") {
			section = strings.TrimSpace(strings.TrimPrefix(line, "## "))
			continue
		}
		if !strings.HasPrefix(line, "### ") {
			continue
		}
		heading := strings.TrimSpace(strings.TrimPrefix(line, "### "))
		if heading == "" || strings.HasPrefix(heading, "Example") {
			continue
		}
		if isNarrativeSubsection(heading, section) {
			continue
		}
		member := Member{
			Kind:        memberKind(heading, section, typeName),
			Name:        memberName(heading),
			Signature:   heading,
			APIVersion:  memberAPIVersion(memberSection(lines[i+1:])),
			Section:     section,
			Description: firstParagraph(lines[i+1:]),
		}
		if propertyType := propertyTypes[member.Name]; propertyType != "" {
			member.Kind = "property"
			member.PropertyType = propertyType
			member.APIVersion = propertyVersions[member.Name]
		}
		if signature := signatureBlock(lines[i+1:]); signature != "" {
			member.Signature = signature
			if name, propertyType := parseApexPropertySignature(signature); member.Kind == "property" && name != "" {
				member.Name = name
				member.PropertyType = propertyType
			} else if parsed := parseApexSignature(signature, typeName); parsed.name != "" {
				headingName := member.Name
				headingParameters := parameterNamesFromHeading(heading)
				if parsed.name == headingName && len(parsed.parameters) < len(headingParameters) {
					if parameterTypes := documentedParameterTypes(lines[i+1:]); len(parameterTypes) == len(headingParameters) {
						signature = signatureWithParameters(signature, headingParameters, parameterTypes)
						member.Signature = signature
						parsed = parseApexSignature(signature, typeName)
					}
				}
				if parsed.name != headingName {
					member.Signature = heading
					members = append(members, member)
					seen[member.Name] = true
					continue
				}
				member.Name = parsed.name
				member.ReturnType = parsed.returnType
				member.Parameters = parsed.parameters
				if parsed.constructor {
					member.Kind = "constructor"
				} else {
					member.Kind = "method"
				}
			}
		}
		members = append(members, member)
		seen[member.Name] = true
	}
	for _, member := range propertyMembers {
		if seen[member.Name] {
			continue
		}
		members = append(members, member)
	}
	sort.SliceStable(members, func(i, j int) bool {
		if members[i].Name == members[j].Name {
			return members[i].Signature < members[j].Signature
		}
		return members[i].Name < members[j].Name
	})
	return members
}

func memberSection(lines []string) []string {
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "### ") {
			return lines[:i]
		}
	}
	return lines
}

func collectPropertyTableMembers(lines []string) []Member {
	var out []Member
	section := ""
	for i := 0; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "## ") {
			section = strings.TrimSpace(strings.TrimPrefix(lines[i], "## "))
			continue
		}
		cells := markdownTableCells(lines[i])
		if len(cells) == 0 {
			continue
		}
		if !strings.Contains(strings.ToLower(section), "propert") {
			continue
		}
		nameIdx, typeIdx, versionIdx := propertyTableIndexes(cells)
		if nameIdx < 0 || typeIdx < 0 {
			continue
		}
		for j := i + 1; j < len(lines); j++ {
			row := markdownTableCells(lines[j])
			if len(row) == 0 {
				break
			}
			if isMarkdownSeparatorRow(row) {
				continue
			}
			if nameIdx >= len(row) || typeIdx >= len(row) {
				continue
			}
			name := strings.TrimSpace(stripMarkdownInline(row[nameIdx]))
			if name == "" {
				continue
			}
			if typ := NormalizeApexDocType(row[typeIdx]); typ != "" {
				member := Member{
					Kind:         "property",
					Name:         name,
					Signature:    name,
					PropertyType: typ,
					Section:      section,
				}
				if versionIdx >= 0 && versionIdx < len(row) {
					member.APIVersion = canonicalAPIVersion(stripMarkdownInline(row[versionIdx]))
				}
				out = append(out, member)
			}
		}
	}
	return out
}

func markdownTableCells(line string) []string {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "|") || !strings.HasSuffix(line, "|") {
		return nil
	}
	line = strings.Trim(line, "|")
	raw := strings.Split(line, "|")
	cells := make([]string, 0, len(raw))
	for _, cell := range raw {
		cells = append(cells, strings.TrimSpace(cell))
	}
	return cells
}

func propertyTableIndexes(cells []string) (int, int, int) {
	nameIdx, typeIdx, versionIdx := -1, -1, -1
	for i, cell := range cells {
		header := strings.ToLower(strings.Join(strings.Fields(stripMarkdownInline(cell)), " "))
		switch header {
		case "property name", "name":
			nameIdx = i
		case "type":
			typeIdx = i
		case "available version":
			versionIdx = i
		}
	}
	return nameIdx, typeIdx, versionIdx
}

func isMarkdownSeparatorRow(cells []string) bool {
	if len(cells) == 0 {
		return false
	}
	for _, cell := range cells {
		cell = strings.Trim(cell, " :-")
		if cell != "" {
			return false
		}
	}
	return true
}

func signatureBlock(lines []string) string {
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "### ") {
			break
		}
		if trimmed != "#### Signature" {
			continue
		}
		return readSignatureCode(lines[i+1:])
	}
	return ""
}

func readSignatureCode(lines []string) string {
	var fenced, inline strings.Builder
	inFence := false
	inInline := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "#### Note" {
			continue
		}
		if strings.HasPrefix(trimmed, "#### ") || strings.HasPrefix(trimmed, "### ") {
			break
		}
		if strings.HasPrefix(trimmed, "```") {
			if inFence {
				break
			}
			inFence = true
			continue
		}
		if inFence {
			if trimmed != "" {
				fenced.WriteString(trimmed)
				fenced.WriteByte(' ')
			}
			continue
		}
		for _, r := range line {
			if r == '`' {
				if inInline {
					candidate := strings.Join(strings.Fields(inline.String()), " ")
					if looksLikeApexDeclaration(candidate) {
						return candidate
					}
					inline.Reset()
					inInline = false
					continue
				}
				inInline = true
				continue
			}
			if inInline {
				inline.WriteRune(r)
			}
		}
		if inInline {
			inline.WriteByte(' ')
		}
	}
	return strings.Join(strings.Fields(fenced.String()), " ")
}

func looksLikeApexDeclaration(value string) bool {
	if strings.Contains(value, "(") || strings.Contains(value, "{") {
		return true
	}
	fields := strings.Fields(value)
	if len(fields) < 2 {
		return false
	}
	switch strings.ToLower(fields[0]) {
	case "public", "private", "protected", "global", "webservice", "static":
		return true
	default:
		return false
	}
}

func parameterNamesFromHeading(heading string) []string {
	open := strings.IndexByte(heading, '(')
	close := strings.LastIndexByte(heading, ')')
	if open < 0 || close < open {
		return nil
	}
	parts := splitTopLevel(heading[open+1:close], ',')
	if len(parts) == 1 && strings.TrimSpace(parts[0]) == "" {
		return []string{}
	}
	for i, part := range parts {
		fields := strings.Fields(part)
		if len(fields) > 0 {
			parts[i] = fields[len(fields)-1]
		}
	}
	return parts
}

func documentedParameterTypes(lines []string) []string {
	inParameters := false
	var out []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#### ") {
			if inParameters {
				break
			}
			inParameters = trimmed == "#### Parameters"
			continue
		}
		if !inParameters {
			continue
		}
		lower := strings.ToLower(trimmed)
		index := strings.Index(lower, "type:")
		if index < 0 {
			continue
		}
		value := NormalizeApexDocType(trimmed[index+len("type:"):])
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func signatureWithParameters(signature string, names, types []string) string {
	open := strings.IndexByte(signature, '(')
	close := strings.LastIndexByte(signature, ')')
	if open < 0 || close < open || len(names) != len(types) {
		return signature
	}
	parameters := make([]string, len(names))
	for i := range names {
		parameters[i] = types[i] + " " + names[i]
	}
	return signature[:open+1] + strings.Join(parameters, ", ") + signature[close:]
}

type parsedApexSignature struct {
	name        string
	returnType  string
	parameters  []string
	constructor bool
}

func parseApexPropertySignature(signature string) (string, string) {
	signature = strings.Join(strings.Fields(stripZeroWidth(signature)), " ")
	open := strings.IndexByte(signature, '{')
	if open <= 0 {
		return "", ""
	}
	fields := dropApexModifiers(topLevelFields(strings.TrimSpace(signature[:open])))
	if len(fields) < 2 {
		return "", ""
	}
	return fields[len(fields)-1], NormalizeApexDocType(fields[len(fields)-2])
}

func parseApexSignature(signature, typeName string) parsedApexSignature {
	signature = strings.Join(strings.Fields(stripZeroWidth(signature)), " ")
	open := strings.IndexByte(signature, '(')
	close := strings.LastIndexByte(signature, ')')
	if open <= 0 || close < open {
		return parsedApexSignature{}
	}
	prefix := strings.TrimSpace(signature[:open])
	params := parseApexParameterTypes(signature[open+1 : close])
	fields := topLevelFields(prefix)
	fields = dropApexModifiers(fields)
	if len(fields) == 0 {
		return parsedApexSignature{}
	}
	last := fields[len(fields)-1]
	lastBase := genericBaseName(last)
	typeBase := genericBaseName(typeName)
	if lastBase == typeBase {
		return parsedApexSignature{name: typeBase, parameters: params, constructor: true}
	}
	if len(fields) < 2 {
		return parsedApexSignature{name: lastBase, parameters: params}
	}
	return parsedApexSignature{
		name:       lastBase,
		returnType: NormalizeApexDocType(fields[len(fields)-2]),
		parameters: params,
	}
}

func topLevelFields(value string) []string {
	var fields []string
	start := -1
	depth := 0
	for i, r := range value {
		switch r {
		case '<':
			depth++
		case '>':
			if depth > 0 {
				depth--
			}
		default:
			if unicode.IsSpace(r) && depth == 0 {
				if start >= 0 {
					fields = append(fields, strings.TrimSpace(value[start:i]))
					start = -1
				}
				continue
			}
		}
		if start < 0 && !unicode.IsSpace(r) {
			start = i
		}
	}
	if start >= 0 {
		fields = append(fields, strings.TrimSpace(value[start:]))
	}
	return fields
}

func dropApexModifiers(fields []string) []string {
	for len(fields) > 0 {
		switch strings.ToLower(fields[0]) {
		case "public", "private", "protected", "global", "webservice", "static", "virtual", "override", "abstract", "testmethod":
			fields = fields[1:]
		default:
			return fields
		}
	}
	return fields
}

func parseApexParameterTypes(params string) []string {
	params = strings.TrimSpace(params)
	if params == "" {
		return []string{}
	}
	parts := splitTopLevel(params, ',')
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		fields := strings.Fields(part)
		if len(fields) > 1 {
			part = strings.Join(fields[:len(fields)-1], " ")
		}
		out = append(out, NormalizeApexDocType(part))
	}
	return out
}

func NormalizeApexDocType(value string) string {
	value = stripZeroWidth(value)
	value = stripMarkdownInline(value)
	value = strings.Join(strings.Fields(value), " ")
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	for strings.HasSuffix(value, "[]") {
		value = strings.TrimSpace(strings.TrimSuffix(value, "[]"))
		value = "List<" + NormalizeApexDocType(value) + ">"
	}
	if strings.EqualFold(value, "sObject") {
		return "SObject"
	}
	if open := strings.IndexByte(value, '<'); open > 0 && strings.HasSuffix(value, ">") {
		name := NormalizeApexDocType(value[:open])
		inner := strings.TrimSuffix(value[open+1:], ">")
		args := splitTopLevel(inner, ',')
		for i := range args {
			args[i] = NormalizeApexDocType(args[i])
		}
		return name + "<" + strings.Join(args, ",") + ">"
	}
	return strings.TrimSpace(value)
}

func stripZeroWidth(value string) string {
	replacer := strings.NewReplacer("\u200b", "", "\u200c", "", "\u200d", "", "\ufeff", "")
	return replacer.Replace(value)
}

func stripMarkdownInline(value string) string {
	for {
		start := strings.IndexByte(value, '[')
		if start < 0 {
			break
		}
		end := strings.IndexByte(value[start:], ']')
		if end < 0 {
			break
		}
		end += start
		if end+1 >= len(value) || value[end+1] != '(' {
			break
		}
		close := strings.IndexByte(value[end+2:], ')')
		if close < 0 {
			break
		}
		close += end + 2
		label := value[start+1 : end]
		value = value[:start] + label + value[close+1:]
	}
	value = strings.ReplaceAll(value, "`", "")
	return strings.TrimSpace(value)
}

func splitTopLevel(value string, sep rune) []string {
	var parts []string
	start := 0
	depth := 0
	for i, r := range value {
		switch r {
		case '<':
			depth++
		case '>':
			if depth > 0 {
				depth--
			}
		default:
			if r == sep && depth == 0 {
				parts = append(parts, strings.TrimSpace(value[start:i]))
				start = i + len(string(r))
			}
		}
	}
	parts = append(parts, strings.TrimSpace(value[start:]))
	return parts
}

func genericBaseName(value string) string {
	value = strings.TrimSpace(value)
	if idx := strings.IndexByte(value, '<'); idx >= 0 {
		value = value[:idx]
	}
	if idx := strings.LastIndexByte(value, '.'); idx >= 0 {
		return value[idx+1:]
	}
	return value
}

func isNarrativeSubsection(heading, section string) bool {
	sectionLower := strings.ToLower(section)
	if !strings.Contains(sectionLower, "example") {
		return false
	}
	if strings.Contains(heading, "(") || strings.Contains(strings.ToLower(heading), " property") {
		return false
	}
	return strings.ContainsAny(heading, " \t")
}

func memberKind(heading, section, typeName string) string {
	sectionLower := strings.ToLower(section)
	name := memberName(heading)
	switch {
	case strings.Contains(sectionLower, "constructor") || name == typeName:
		return "constructor"
	case strings.Contains(sectionLower, "propert") || strings.Contains(strings.ToLower(heading), " property"):
		return "property"
	case strings.Contains(heading, "("):
		return "method"
	default:
		return "member"
	}
}

func memberName(heading string) string {
	heading = strings.TrimSpace(heading)
	if idx := strings.Index(heading, "("); idx >= 0 {
		return strings.TrimSpace(heading[:idx])
	}
	heading = strings.TrimSuffix(heading, " Property")
	heading = strings.TrimSpace(heading)
	fields := strings.FieldsFunc(heading, func(r rune) bool {
		return unicode.IsSpace(r) || r == ':' || r == '-'
	})
	if len(fields) == 0 {
		return heading
	}
	return fields[0]
}

func firstParagraph(lines []string) string {
	var parts []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			if len(parts) > 0 {
				break
			}
			continue
		}
		if strings.HasPrefix(line, "#") {
			break
		}
		if strings.HasPrefix(line, "|") || strings.HasPrefix(line, "```") {
			break
		}
		parts = append(parts, line)
	}
	out := strings.Join(parts, " ")
	if len(out) > 400 {
		out = strings.ToValidUTF8(out[:400], "")
	}
	return out
}

func collectExamples(lines []string) []Example {
	var examples []Example
	for i, line := range lines {
		if !strings.HasPrefix(line, "## ") && !strings.HasPrefix(line, "### ") && !strings.HasPrefix(line, "#### ") {
			continue
		}
		heading := strings.TrimSpace(strings.TrimLeft(line, "# "))
		if !strings.Contains(strings.ToLower(heading), "example") {
			continue
		}
		examples = append(examples, Example{
			Heading: heading,
			Snippet: firstParagraph(lines[i+1:]),
		})
	}
	return examples
}

func summarize(docs []Document) ([]NamespaceSummary, int) {
	summaries := map[string]*NamespaceSummary{}
	totalMembers := 0
	for _, doc := range docs {
		ns := doc.Namespace
		if ns == "" {
			ns = "(none)"
		}
		summary := summaries[ns]
		if summary == nil {
			summary = &NamespaceSummary{Namespace: ns}
			summaries[ns] = summary
		}
		summary.Documents++
		summary.Members += len(doc.Members)
		totalMembers += len(doc.Members)
		switch doc.Kind {
		case "namespace":
			summary.Namespaces++
		case "class":
			summary.Classes++
		case "interface":
			summary.Interfaces++
		case "enum":
			summary.Enums++
		case "input":
			summary.Inputs++
		case "output":
			summary.Outputs++
		}
	}
	out := make([]NamespaceSummary, 0, len(summaries))
	for _, summary := range summaries {
		out = append(out, *summary)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Namespace < out[j].Namespace
	})
	return out, totalMembers
}

func compareDocument(oldDoc, newDoc Document) DocumentDiff {
	diff := DocumentDiff{SourcePath: newDoc.SourcePath}
	oldIdentity := documentIdentity(oldDoc)
	newIdentity := documentIdentity(newDoc)
	if oldIdentity != newIdentity {
		diff.OldIdentity = oldIdentity
		diff.NewIdentity = newIdentity
	}
	oldMembers := memberSet(oldDoc.Members)
	newMembers := memberSet(newDoc.Members)
	for signature := range newMembers {
		if _, ok := oldMembers[signature]; !ok {
			diff.AddedMembers = append(diff.AddedMembers, signature)
		}
	}
	for signature := range oldMembers {
		if _, ok := newMembers[signature]; !ok {
			diff.RemovedMembers = append(diff.RemovedMembers, signature)
		}
	}
	sort.Strings(diff.AddedMembers)
	sort.Strings(diff.RemovedMembers)
	return diff
}

func documentIdentity(doc Document) string {
	return doc.Kind + "|" + doc.Namespace + "|" + doc.Name + "|" + doc.Title
}

func memberSet(members []Member) map[string]struct{} {
	out := map[string]struct{}{}
	for _, member := range members {
		out[member.Kind+"|"+member.Name+"|"+member.Signature] = struct{}{}
	}
	return out
}

// ReleaseManifest records the canonical digest together with enough acquisition
// provenance to reproduce the normalized inventory from the same Salesforce
// release.  It contains no credentials, org identity, or session data.
type ReleaseManifest struct {
	SchemaVersion  int      `json:"schemaVersion"`
	Release        string   `json:"release"`
	APIVersion     string   `json:"apiVersion"`
	Digest         string   `json:"digest"`
	Acquisition    string   `json:"acquisition"`
	SourceFamilies []string `json:"sourceFamilies"`
}

// NewReleaseManifest creates a ReleaseManifest with SchemaVersion pinned to
// InventorySchemaVersion.
func NewReleaseManifest(release, apiVersion, digest, acquisition string, sourceFamilies []string) ReleaseManifest {
	return ReleaseManifest{
		SchemaVersion:  InventorySchemaVersion,
		Release:        release,
		APIVersion:     apiVersion,
		Digest:         digest,
		Acquisition:    acquisition,
		SourceFamilies: sourceFamilies,
	}
}

// CanonicalDigest returns the lowercase hexadecimal SHA-256 digest of a
// deep-copied, order-normalized encoding of inv.  Two logical inventories that
// differ only in slice order produce the same digest.  Any contract-bearing
// content change (member signature, description, behavior, heading, example)
// produces a different digest.  The caller's inventory is not mutated.
func CanonicalDigest(inv Inventory) string {
	cp := deepCopyInventory(inv)
	normalizeInventory(&cp)
	data, err := json.Marshal(cp)
	if err != nil {
		// json.Marshal on a pure-struct tree with no channels/funcs should never error.
		panic("cannot marshal canonical inventory: " + err.Error())
	}
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func deepCopyInventory(inv Inventory) Inventory {
	cp := inv
	// Documents
	if inv.Documents != nil {
		cp.Documents = make([]Document, len(inv.Documents))
		for i, d := range inv.Documents {
			cp.Documents[i] = deepCopyDocument(d)
		}
	} else {
		cp.Documents = nil
	}
	// Namespaces
	if inv.Namespaces != nil {
		cp.Namespaces = make([]NamespaceSummary, len(inv.Namespaces))
		copy(cp.Namespaces, inv.Namespaces)
	} else {
		cp.Namespaces = nil
	}
	return cp
}

func deepCopyDocument(doc Document) Document {
	cp := doc
	if doc.Headings != nil {
		cp.Headings = make([]string, len(doc.Headings))
		copy(cp.Headings, doc.Headings)
	}
	if doc.Members != nil {
		cp.Members = make([]Member, len(doc.Members))
		for i, m := range doc.Members {
			cp.Members[i] = deepCopyMember(m)
		}
	}
	if doc.Examples != nil {
		cp.Examples = make([]Example, len(doc.Examples))
		copy(cp.Examples, doc.Examples)
	}
	if doc.Behaviors != nil {
		cp.Behaviors = make([]DocBehavior, len(doc.Behaviors))
		copy(cp.Behaviors, doc.Behaviors)
	}
	return cp
}

func deepCopyMember(m Member) Member {
	cp := m
	if m.Parameters != nil {
		cp.Parameters = make([]string, len(m.Parameters))
		copy(cp.Parameters, m.Parameters)
	}
	return cp
}

func normalizeInventory(inv *Inventory) {
	// Sort documents by SourcePath.
	sort.Slice(inv.Documents, func(i, j int) bool {
		return inv.Documents[i].SourcePath < inv.Documents[j].SourcePath
	})
	for i := range inv.Documents {
		normalizeDocument(&inv.Documents[i])
	}
	// Sort namespaces by Namespace name.
	sort.Slice(inv.Namespaces, func(i, j int) bool {
		return inv.Namespaces[i].Namespace < inv.Namespaces[j].Namespace
	})
}

func normalizeDocument(doc *Document) {
	// Stable sort headings.
	sort.Strings(doc.Headings)
	// Stable sort members by (Kind, Name, Signature).
	sort.SliceStable(doc.Members, func(i, j int) bool {
		if doc.Members[i].Kind != doc.Members[j].Kind {
			return doc.Members[i].Kind < doc.Members[j].Kind
		}
		if doc.Members[i].Name != doc.Members[j].Name {
			return doc.Members[i].Name < doc.Members[j].Name
		}
		return doc.Members[i].Signature < doc.Members[j].Signature
	})
	// Stable sort examples by Heading.
	sort.SliceStable(doc.Examples, func(i, j int) bool {
		return doc.Examples[i].Heading < doc.Examples[j].Heading
	})
	// Stable sort behaviors by Kind.
	sort.SliceStable(doc.Behaviors, func(i, j int) bool {
		return doc.Behaviors[i].Kind < doc.Behaviors[j].Kind
	})
	// Sort member Parameters so that parameter order differences are detected.
	for i := range doc.Members {
		sort.Strings(doc.Members[i].Parameters)
	}
}
