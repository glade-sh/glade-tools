package perfscan

import (
	"bytes"
	"encoding/xml"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/glade-sh/glade/internal/apexast"
	"github.com/glade-sh/glade/internal/project"
)

var (
	dmlNewObjectRe     = regexp.MustCompile(`(?i)\bnew\s+([A-Za-z_][A-Za-z0-9_]*(?:__(?:c|mdt))?)\s*\(`)
	dmlListObjectRe    = regexp.MustCompile(`(?i)\b(?:List|Set)\s*<\s*([A-Za-z_][A-Za-z0-9_]*(?:__(?:c|mdt))?)\s*>`)
	dmlKeywordObjectRe = regexp.MustCompile(`(?i)\b(?:insert|update|upsert|delete|undelete|merge)\s+([A-Za-z_][A-Za-z0-9_]*(?:__(?:c|mdt))?)\b`)
	dmlObjectMarkerRe  = regexp.MustCompile(`(?i)\bobject:([A-Za-z_][A-Za-z0-9_]*(?:__(?:c|mdt))?)\b`)
)

type MetadataFacts struct {
	Objects map[string]*MetadataObjectFacts
}

type MetadataObjectFacts struct {
	Object         string
	Triggers       []MetadataFact
	RecordFlows    []MetadataFact
	WorkflowRules  []MetadataFact
	Rollups        []MetadataFact
	SharingModels  []MetadataFact
	CustomMetadata []MetadataFact
}

type MetadataFact struct {
	Kind    string
	Message string
	Value   string
	File    string
	Line    int
}

type triggerContext struct {
	Object string
	File   string
	Start  int
	End    int
}

func BuildMetadataFacts(p project.Project, parsed apexast.Result) MetadataFacts {
	facts := MetadataFacts{Objects: map[string]*MetadataObjectFacts{}}
	for _, file := range parsed.Files {
		for _, decl := range file.Declarations {
			if decl.Kind != apexast.DeclarationTrigger || strings.TrimSpace(decl.ObjectName) == "" {
				continue
			}
			object := strings.TrimSpace(decl.ObjectName)
			facts.object(object).Triggers = append(facts.object(object).Triggers, MetadataFact{
				Kind:    "trigger",
				Message: "Apex triggers",
				Value:   decl.Name,
				File:    file.Path,
				Line:    decl.Range.Start.Line,
			})
		}
	}
	for _, path := range p.FlowFiles {
		info := readFlowMetadata(path)
		if info.object == "" || !info.recordTriggered {
			continue
		}
		facts.object(info.object).RecordFlows = append(facts.object(info.object).RecordFlows, MetadataFact{
			Kind:    "flow",
			Message: "record-triggered flows",
			Value:   info.name,
			File:    path,
			Line:    1,
		})
	}
	for _, path := range p.WorkflowFiles {
		count := activeWorkflowRuleCount(path)
		if count == 0 {
			continue
		}
		object := workflowObjectName(path)
		if object == "" {
			continue
		}
		facts.object(object).WorkflowRules = append(facts.object(object).WorkflowRules, MetadataFact{
			Kind:    "workflow",
			Message: "active workflow rules",
			Value:   strconv.Itoa(count),
			File:    path,
			Line:    1,
		})
	}
	for _, path := range p.FieldFiles {
		info := readFieldMetadata(path)
		if !info.rollup || info.summarizedObject == "" {
			continue
		}
		facts.object(info.summarizedObject).Rollups = append(facts.object(info.summarizedObject).Rollups, MetadataFact{
			Kind:    "rollup",
			Message: "roll-up summary fields",
			Value:   fieldParentObject(path),
			File:    path,
			Line:    1,
		})
	}
	for _, path := range p.ObjectFiles {
		info := readObjectMetadata(path)
		if info.sharingModel == "" {
			continue
		}
		object := objectNameFromMetadataPath(path)
		if object == "" {
			continue
		}
		facts.object(object).SharingModels = append(facts.object(object).SharingModels, MetadataFact{
			Kind:    "sharing",
			Message: "object sharing model",
			Value:   info.sharingModel,
			File:    path,
			Line:    1,
		})
	}
	for object, count := range customMetadataCounts(p.CustomMetadataFiles) {
		facts.object(object).CustomMetadata = append(facts.object(object).CustomMetadata, MetadataFact{
			Kind:    "metadata",
			Message: "custom metadata records",
			Value:   strconv.Itoa(count),
		})
	}
	facts.sort()
	return facts
}

func ApplyMetadataFacts(g *Graph, facts MetadataFacts, parsed apexast.Result) {
	if g == nil || len(facts.Objects) == 0 {
		return
	}
	triggers := collectTriggerContexts(parsed)
	for index, node := range g.nodes {
		if node.Kind != NodeDML {
			continue
		}
		dmlID := NodeID(index + 1)
		for _, object := range dmlTouchedObjects(node, triggers) {
			objectFacts := facts.lookup(object)
			if objectFacts == nil {
				continue
			}
			for _, fact := range objectFacts.allBlastRadiusFacts() {
				automation := g.AddNode(Node{
					Kind:      NodeAutomation,
					Name:      objectFacts.Object + " " + fact.Message,
					File:      fact.File,
					Line:      fact.Line,
					Operation: fact.Kind,
				})
				g.AddEdge(dmlID, automation, EdgeWakes)
				g.AddEvidence(dmlID, Evidence{Kind: fact.Kind, Message: fact.Message, Value: fact.Value})
				g.AddEvidence(automation, Evidence{Kind: fact.Kind, Message: fact.Message, Value: fact.Value})
				g.AddResourceRisk(automation, metadataFactRisk(fact.Kind))
			}
			g.AddResourceRisk(dmlID, ResourceRisk{CPU: true, DBTime: true, Locks: true, SharedLimit: true})
		}
	}
}

func emitMetadataGraphFindings(report *Report, graph *Graph) {
	if graph == nil {
		return
	}
	for index, node := range graph.nodes {
		if node.Kind != NodeDML {
			continue
		}
		nodeID := NodeID(index + 1)
		evidence := graph.Evidence(nodeID)
		if !metadataBlastRadiusEvidence(evidence) {
			continue
		}
		entryID, path := firstEntryPath(graph, nodeID, 2)
		entry := EntryPoint{Kind: EntryUnknown}
		if entryID != 0 {
			if entryNode, ok := graph.node(entryID); ok {
				entry = entryPointFromGraphNode(entryNode)
			}
		}
		risk := ResourceRisk{CPU: true, DBTime: true, Locks: true, SharedLimit: true}
		if entryID != 0 {
			risk = mergeResourceRisk(risk, graph.PathResourceRisk(entryID, nodeID))
		}
		finding := Finding{
			ID:           "perf.dml.blast-radius",
			Category:     CategoryDML,
			Severity:     SeverityMedium,
			Confidence:   confidenceFromEvidence(evidence, ConfidenceStatic),
			Score:        84,
			EntryPoint:   entry,
			Message:      "DML touches an object with downstream automation or sharing work in the same save order.",
			Location:     Location{File: node.File, Line: node.Line},
			Path:         path,
			Multiplicity: "per-DML",
			Evidence:     evidence,
			ResourceRisk: risk,
			Fix:          "Guard no-op updates, bulkify save-order side effects, and remove duplicate Flow, Workflow, and trigger work where safe.",
			Acceptance:   "A no-op field update on the same object is removed or guarded, and DML tests prove only necessary rows are updated.",
		}
		if !reportHasFinding(report, finding) {
			report.AddFinding(finding)
		}
	}
}

func (f *MetadataFacts) object(name string) *MetadataObjectFacts {
	key := metadataObjectKey(name)
	if key == "" {
		return &MetadataObjectFacts{}
	}
	if f.Objects == nil {
		f.Objects = map[string]*MetadataObjectFacts{}
	}
	if existing := f.Objects[key]; existing != nil {
		return existing
	}
	object := &MetadataObjectFacts{Object: strings.TrimSpace(name)}
	f.Objects[key] = object
	return object
}

func (f MetadataFacts) lookup(name string) *MetadataObjectFacts {
	return f.Objects[metadataObjectKey(name)]
}

func (f MetadataFacts) sort() {
	for _, object := range f.Objects {
		sortMetadataFacts(object.Triggers)
		sortMetadataFacts(object.RecordFlows)
		sortMetadataFacts(object.WorkflowRules)
		sortMetadataFacts(object.Rollups)
		sortMetadataFacts(object.SharingModels)
		sortMetadataFacts(object.CustomMetadata)
	}
}

func (f MetadataObjectFacts) allBlastRadiusFacts() []MetadataFact {
	var out []MetadataFact
	out = append(out, f.RecordFlows...)
	out = append(out, f.WorkflowRules...)
	out = append(out, f.Triggers...)
	out = append(out, f.Rollups...)
	out = append(out, f.SharingModels...)
	return out
}

func metadataBlastRadiusEvidence(evidence []Evidence) bool {
	automation := 0
	hasRollupOrSharing := false
	for _, item := range evidence {
		switch item.Kind {
		case "flow", "workflow", "trigger":
			automation += evidenceCount(item)
		case "rollup", "sharing":
			hasRollupOrSharing = true
		}
	}
	return automation >= 2 || hasRollupOrSharing
}

func evidenceCount(item Evidence) int {
	if value, err := strconv.Atoi(strings.TrimSpace(item.Value)); err == nil && value > 0 {
		return value
	}
	return 1
}

func metadataFactRisk(kind string) ResourceRisk {
	switch kind {
	case "flow", "workflow", "trigger":
		return ResourceRisk{CPU: true, DBTime: true, SharedLimit: true}
	case "rollup", "sharing":
		return ResourceRisk{DBTime: true, Locks: true, SharedLimit: true}
	default:
		return ResourceRisk{CPU: true, SharedLimit: true}
	}
}

func collectTriggerContexts(parsed apexast.Result) []triggerContext {
	var out []triggerContext
	for _, file := range parsed.Files {
		for _, decl := range file.Declarations {
			if decl.Kind != apexast.DeclarationTrigger || strings.TrimSpace(decl.ObjectName) == "" {
				continue
			}
			out = append(out, triggerContext{
				Object: strings.TrimSpace(decl.ObjectName),
				File:   filepath.ToSlash(file.Path),
				Start:  decl.Range.Start.Line,
				End:    decl.Range.End.Line,
			})
		}
	}
	return out
}

func dmlTouchedObjects(node Node, triggers []triggerContext) []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(object string) {
		object = strings.TrimSpace(object)
		key := metadataObjectKey(object)
		if key == "" {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, object)
	}
	for _, object := range dmlObjectsFromOperation(node.Operation) {
		add(object)
	}
	for _, object := range dmlObjectsFromOperation(node.Name) {
		add(object)
	}
	file := filepath.ToSlash(node.File)
	for _, trigger := range triggers {
		if filepath.ToSlash(trigger.File) != file {
			continue
		}
		if node.Line >= trigger.Start && (trigger.End == 0 || node.Line <= trigger.End) {
			add(trigger.Object)
		}
	}
	sort.Strings(out)
	return out
}

func dmlObjectsFromOperation(operation string) []string {
	var out []string
	for _, re := range []*regexp.Regexp{dmlObjectMarkerRe, dmlNewObjectRe, dmlListObjectRe, dmlKeywordObjectRe} {
		for _, match := range re.FindAllStringSubmatch(operation, -1) {
			if len(match) < 2 {
				continue
			}
			object := strings.TrimSpace(match[1])
			if sourceLooksLikeObjectName(object) {
				out = append(out, object)
			}
		}
	}
	return dedupeStrings(out)
}

func sourceLooksLikeObjectName(value string) bool {
	if value == "" {
		return false
	}
	if strings.Contains(value, "__") {
		return true
	}
	first := value[0]
	return first >= 'A' && first <= 'Z'
}

type flowMetadataInfo struct {
	name            string
	object          string
	recordTriggered bool
}

type fieldMetadataInfo struct {
	rollup           bool
	summarizedObject string
}

type objectMetadataInfo struct {
	sharingModel string
}

func readFlowMetadata(path string) flowMetadataInfo {
	texts := readXMLTexts(path)
	info := flowMetadataInfo{name: strings.TrimSuffix(filepath.Base(path), ".flow-meta.xml")}
	startObject := firstXMLText(texts, "Flow/start/object")
	triggerType := firstXMLText(texts, "Flow/start/triggerType")
	if startObject != "" && strings.Contains(strings.ToLower(triggerType), "record") {
		info.object = startObject
		info.recordTriggered = true
		return info
	}
	return info
}

func activeWorkflowRuleCount(path string) int {
	texts := readXMLTexts(path)
	count := 0
	for _, active := range xmlTexts(texts, "Workflow/rules/active") {
		if strings.EqualFold(active, "true") {
			count++
		}
	}
	return count
}

func readFieldMetadata(path string) fieldMetadataInfo {
	texts := readXMLTexts(path)
	info := fieldMetadataInfo{}
	for _, fieldType := range xmlTexts(texts, "CustomField/type") {
		if strings.EqualFold(fieldType, "Summary") {
			info.rollup = true
			break
		}
	}
	info.summarizedObject = firstXMLText(texts, "CustomField/summarizedObject")
	return info
}

func readObjectMetadata(path string) objectMetadataInfo {
	texts := readXMLTexts(path)
	sharingModel := firstXMLText(texts, "CustomObject/sharingModel")
	if sharingModel == "" {
		sharingModel = firstXMLText(texts, "CustomObject/externalSharingModel")
	}
	return objectMetadataInfo{sharingModel: sharingModel}
}

func readXMLTexts(path string) map[string][]string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var stack []string
	out := map[string][]string{}
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		switch typed := token.(type) {
		case xml.StartElement:
			stack = append(stack, typed.Name.Local)
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			value := strings.TrimSpace(string(typed))
			if value == "" || len(stack) == 0 {
				continue
			}
			key := strings.Join(stack, "/")
			out[key] = append(out[key], value)
		}
	}
	return out
}

func firstXMLText(texts map[string][]string, suffix string) string {
	values := xmlTexts(texts, suffix)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func xmlTexts(texts map[string][]string, suffix string) []string {
	var out []string
	for key, values := range texts {
		if key == suffix || strings.HasSuffix(key, "/"+suffix) {
			out = append(out, values...)
		}
	}
	sort.Strings(out)
	return out
}

func workflowObjectName(path string) string {
	name := filepath.Base(path)
	name = strings.TrimSuffix(name, ".workflow-meta.xml")
	name = strings.TrimSuffix(name, ".workflow")
	return strings.TrimSpace(name)
}

func fieldParentObject(path string) string {
	return objectNameFromMetadataPath(path)
}

func objectNameFromMetadataPath(path string) string {
	parts := strings.Split(filepath.ToSlash(path), "/")
	for i := 0; i < len(parts)-1; i++ {
		if parts[i] == "objects" {
			return parts[i+1]
		}
	}
	name := filepath.Base(path)
	name = strings.TrimSuffix(name, ".object-meta.xml")
	name = strings.TrimSuffix(name, ".object")
	return strings.TrimSpace(name)
}

func customMetadataCounts(paths []string) map[string]int {
	counts := map[string]int{}
	for _, path := range paths {
		object := customMetadataObjectName(path)
		if object != "" {
			counts[object]++
		}
	}
	return counts
}

func customMetadataObjectName(path string) string {
	name := filepath.Base(path)
	name = strings.TrimSuffix(name, ".md-meta.xml")
	name = strings.TrimSuffix(name, ".md")
	if idx := strings.Index(name, "."); idx > 0 {
		return name[:idx] + "__mdt"
	}
	return ""
}

func metadataObjectKey(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func sortMetadataFacts(facts []MetadataFact) {
	sort.Slice(facts, func(i, j int) bool {
		if facts[i].Kind != facts[j].Kind {
			return facts[i].Kind < facts[j].Kind
		}
		if facts[i].Message != facts[j].Message {
			return facts[i].Message < facts[j].Message
		}
		if facts[i].File != facts[j].File {
			return facts[i].File < facts[j].File
		}
		return facts[i].Value < facts[j].Value
	})
}

func dedupeStrings(values []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, value := range values {
		key := strings.ToLower(strings.TrimSpace(value))
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, strings.TrimSpace(value))
	}
	sort.Strings(out)
	return out
}
