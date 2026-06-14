package perfscan

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/glade-sh/glade/internal/profile"
	"github.com/glade-sh/glade/internal/trace"
)

const nearestTraceLineWindow = 5

type TraceCorrelationResult struct {
	Matched   []TraceCorrelationMatch
	Unmatched []profile.Entry
}

type TraceCorrelationMatch struct {
	NodeID NodeID
	Entry  profile.Entry
	Reason string
}

func TraceProfileFromBytes(data []byte) (profile.Report, error) {
	doc, err := profile.ReadTrace(bytes.NewReader(data))
	if err != nil {
		return profile.Report{}, err
	}
	return profile.Analyze(doc), nil
}

func CorrelateTrace(g *Graph, profileReport profile.Report) TraceCorrelationResult {
	var result TraceCorrelationResult
	for _, entry := range traceCorrelationEntries(profileReport) {
		nodeID, reason := matchTraceEntry(g, entry)
		if nodeID == 0 {
			result.Unmatched = append(result.Unmatched, entry)
			continue
		}
		addTraceEntryEvidence(g, nodeID, entry)
		result.Matched = append(result.Matched, TraceCorrelationMatch{
			NodeID: nodeID,
			Entry:  entry,
			Reason: reason,
		})
	}
	return result
}

func promoteReportFindingsFromTrace(report *Report, graph *Graph) {
	if report == nil || graph == nil {
		return
	}
	for i := range report.Findings {
		if report.Findings[i].Confidence == ConfidenceMeasured {
			continue
		}
		nodeID := bestTraceEvidenceNodeForFinding(graph, report.Findings[i])
		if nodeID == 0 {
			continue
		}
		traceEvidence := filterTraceEvidence(graph.Evidence(nodeID))
		if len(traceEvidence) == 0 {
			continue
		}
		report.Findings[i].Evidence = appendUniqueEvidence(report.Findings[i].Evidence, traceEvidence...)
		report.Findings[i].Confidence = ConfidenceCombined
		report.Findings[i].ResourceRisk = mergeResourceRisk(report.Findings[i].ResourceRisk, graph.ResourceRisk(nodeID))
		if report.Findings[i].Path == nil {
			if entryID, path := firstEntryPath(graph, nodeID, 2); len(path) > 0 {
				report.Findings[i].Path = path
				if report.Findings[i].EntryPoint.Name == "" {
					if entryNode, ok := graph.node(entryID); ok {
						report.Findings[i].EntryPoint = entryPointFromGraphNode(entryNode)
					}
				}
			}
		}
	}
}

func AddMeasuredTraceFindings(report *Report, profileReport profile.Report) {
	AddTraceMeasurements(report, profileReport.Spans)
	AddMeasuredTraceFindingsForEntries(report, profileReport.Hot)
}

func AddTraceMeasurements(report *Report, spans []profile.Entry) {
	for _, span := range spans {
		report.AddMeasurement(Measurement{
			Name:       span.Name,
			Category:   span.Category,
			DurationMS: span.DurationMS,
			Count:      measuredCount(span),
			File:       firstFile(span),
			Line:       firstLine(span),
		})
	}
}

func AddMeasuredTraceFindingsForEntries(report *Report, entries []profile.Entry) {
	for _, entry := range entries {
		if entry.DurationMS >= 100 {
			report.AddFinding(Finding{
				ID:         "perf.measured.hot-span",
				Category:   CategoryMeasured,
				Severity:   measuredSeverity(entry.DurationMS),
				Confidence: ConfidenceMeasured,
				Score:      measuredScore(entry.DurationMS),
				Message:    fmt.Sprintf("Measured runtime span `%s` consumed %d ms across %d span(s).", entry.Name, entry.DurationMS, measuredCount(entry)),
				Location:   Location{File: firstFile(entry), Line: firstLine(entry)},
				Evidence:   []Evidence{{Kind: "trace", Message: "duration ms", Value: fmt.Sprint(entry.DurationMS)}},
				Fix:        "Open the measured transaction path, inspect the child SOQL/DML/describe/automation spans, and reduce the highest-duration work first.",
			})
		}
	}
	for _, entry := range entries {
		if entry.Category != "apex.soql" {
			continue
		}
		rows := soqlRowsFromHotEvent(entry)
		if rows >= 500 {
			report.AddFinding(Finding{
				ID:         "perf.measured.soql-rows",
				Category:   CategorySOQL,
				Severity:   SeverityMedium,
				Confidence: ConfidenceMeasured,
				Score:      72,
				Message:    "Measured SOQL returned a high row count in the traced transaction.",
				Location:   Location{File: firstFile(entry), Line: firstLine(entry)},
				Evidence:   []Evidence{{Kind: "trace", Message: "SOQL rows", Value: fmt.Sprint(rows)}},
				Fix:        "Check query filters and projections, then use a selective predicate or smaller data window.",
			})
		}
	}
}

func bestTraceEvidenceNodeForFinding(graph *Graph, finding Finding) NodeID {
	if finding.Location.File == "" {
		return 0
	}
	kinds := nodeKindsForFinding(finding)
	if len(kinds) == 0 {
		return 0
	}
	bestDistance := nearestTraceLineWindow + 1
	var best NodeID
	for index, node := range graph.nodes {
		if !nodeKindIn(node.Kind, kinds) {
			continue
		}
		if !traceFileMatches(finding.Location.File, node.File) {
			continue
		}
		if len(filterTraceEvidence(graph.Evidence(NodeID(index+1)))) == 0 {
			continue
		}
		distance := absInt(node.Line - finding.Location.Line)
		if finding.Location.Line == 0 || node.Line == 0 {
			distance = nearestTraceLineWindow
		}
		if distance > nearestTraceLineWindow {
			continue
		}
		if distance < bestDistance {
			bestDistance = distance
			best = NodeID(index + 1)
		}
	}
	return best
}

func nodeKindsForFinding(finding Finding) []NodeKind {
	switch finding.Category {
	case CategorySOQL:
		return []NodeKind{NodeSOQL}
	case CategoryDML:
		return []NodeKind{NodeDML}
	case CategoryDescribe:
		return []NodeKind{NodeDescribe, NodeStaticInit}
	case CategoryAutomation:
		return []NodeKind{NodeAutomation}
	default:
		return nil
	}
}

func nodeKindIn(kind NodeKind, kinds []NodeKind) bool {
	for _, candidate := range kinds {
		if kind == candidate {
			return true
		}
	}
	return false
}

func filterTraceEvidence(evidence []Evidence) []Evidence {
	var out []Evidence
	for _, item := range evidence {
		if item.Kind == "trace" {
			out = append(out, item)
		}
	}
	return out
}

func appendUniqueEvidence(existing []Evidence, additions ...Evidence) []Evidence {
	seen := make(map[string]struct{}, len(existing)+len(additions))
	for _, item := range existing {
		seen[evidenceKey(item)] = struct{}{}
	}
	for _, item := range additions {
		key := evidenceKey(item)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		existing = append(existing, item)
	}
	return existing
}

func traceCorrelationEntries(report profile.Report) []profile.Entry {
	if len(report.Hot) > 0 {
		return report.Hot
	}
	seen := map[string]struct{}{}
	var entries []profile.Entry
	for _, group := range [][]profile.Entry{
		report.Spans,
		report.Methods,
		report.SOQL,
		report.DML,
		report.Triggers,
		report.Describe,
		report.Async,
		report.Automation,
		report.Visualforce,
		report.Metadata,
	} {
		for _, entry := range group {
			key := traceEntryKey(entry)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			entries = append(entries, entry)
		}
	}
	return entries
}

func matchTraceEntry(g *Graph, entry profile.Entry) (NodeID, string) {
	if g == nil {
		return 0, ""
	}
	if id := exactFileLineTraceMatch(g, entry); id != 0 {
		return id, "file-line"
	}
	if id := nearestFileLineTraceMatch(g, entry); id != 0 {
		return id, "nearest-file-line"
	}
	if id := queryHashTraceMatch(g, entry); id != 0 {
		return id, "query"
	}
	if id := dmlObjectTraceMatch(g, entry); id != 0 {
		return id, "dml-object"
	}
	if id := entryNameTraceMatch(g, entry); id != 0 {
		return id, "entry-name"
	}
	return 0, ""
}

func exactFileLineTraceMatch(g *Graph, entry profile.Entry) NodeID {
	lines := traceEntryLines(entry)
	if entry.File == "" || len(lines) == 0 {
		return 0
	}
	for index, node := range g.nodes {
		if !traceEntryMatchesNodeKind(entry, node) || node.Line <= 0 {
			continue
		}
		if !traceFileMatches(entry.File, node.File) {
			continue
		}
		for _, line := range lines {
			if line == node.Line {
				return NodeID(index + 1)
			}
		}
	}
	return 0
}

func nearestFileLineTraceMatch(g *Graph, entry profile.Entry) NodeID {
	lines := traceEntryLines(entry)
	if entry.File == "" || len(lines) == 0 {
		return 0
	}
	bestDistance := nearestTraceLineWindow + 1
	var best NodeID
	for index, node := range g.nodes {
		if !traceEntryMatchesNodeKind(entry, node) || node.Line <= 0 {
			continue
		}
		if !traceFileMatches(entry.File, node.File) {
			continue
		}
		for _, line := range lines {
			distance := absInt(node.Line - line)
			if distance == 0 || distance > nearestTraceLineWindow {
				continue
			}
			if distance < bestDistance {
				bestDistance = distance
				best = NodeID(index + 1)
			}
		}
	}
	return best
}

func queryHashTraceMatch(g *Graph, entry profile.Entry) NodeID {
	if entry.Category != "apex.soql" {
		return 0
	}
	entryHash := strings.TrimSpace(entry.QueryHash)
	if entryHash == "" && strings.TrimSpace(entry.Name) != "" {
		entryHash = trace.StableQueryHash(entry.Name)
	}
	entryQuery := trace.NormalizeFactText(entry.Name)
	for index, node := range g.nodes {
		if node.Kind != NodeSOQL {
			continue
		}
		nodeQuery := queryTextFromNode(node)
		nodeHash := ""
		if nodeQuery != "" {
			nodeHash = trace.StableQueryHash(nodeQuery)
		}
		if entryHash != "" && nodeHash != "" && entryHash == nodeHash {
			return NodeID(index + 1)
		}
		if entryQuery != "" && trace.NormalizeFactText(nodeQuery) == entryQuery {
			return NodeID(index + 1)
		}
	}
	return 0
}

func dmlObjectTraceMatch(g *Graph, entry profile.Entry) NodeID {
	if entry.Category != "apex.dml" {
		return 0
	}
	objects := traceEntryObjects(entry)
	if len(objects) == 0 {
		return 0
	}
	for index, node := range g.nodes {
		if node.Kind != NodeDML {
			continue
		}
		for _, object := range objects {
			if dmlNodeMentionsObject(node, object) {
				return NodeID(index + 1)
			}
		}
	}
	return 0
}

func entryNameTraceMatch(g *Graph, entry profile.Entry) NodeID {
	names := traceEntryNames(entry)
	if len(names) == 0 {
		return 0
	}
	for index, node := range g.nodes {
		if !traceEntryMatchesNodeKind(entry, node) {
			continue
		}
		for _, name := range names {
			if traceNameMatchesNode(name, node.Name) || traceNameMatchesNode(name, node.Operation) {
				return NodeID(index + 1)
			}
		}
	}
	return 0
}

func addTraceEntryEvidence(g *Graph, nodeID NodeID, entry profile.Entry) {
	for _, evidence := range traceEntryEvidence(entry) {
		g.AddEvidence(nodeID, evidence)
	}
}

func traceEntryEvidence(entry profile.Entry) []Evidence {
	operation := traceEvidenceOperation(entry)
	var evidence []Evidence
	if entry.DurationMS > 0 {
		evidence = append(evidence, Evidence{Kind: "trace", Message: "duration ms", Value: strconv.FormatInt(entry.DurationMS, 10), Operation: operation})
	}
	if entry.Count > 0 {
		evidence = append(evidence, Evidence{Kind: "trace", Message: "count", Value: strconv.Itoa(entry.Count), Operation: operation})
	}
	if entry.Rows > 0 {
		evidence = append(evidence, Evidence{Kind: "trace", Message: "rows", Value: strconv.Itoa(entry.Rows), Operation: operation})
	}
	return evidence
}

func traceEntryLines(entry profile.Entry) []int {
	seen := map[int]struct{}{}
	var lines []int
	for _, sourceRange := range entry.SourceRanges {
		if sourceRange.Line <= 0 {
			continue
		}
		if _, ok := seen[sourceRange.Line]; ok {
			continue
		}
		seen[sourceRange.Line] = struct{}{}
		lines = append(lines, sourceRange.Line)
	}
	return lines
}

func traceFileMatches(traceFile, nodeFile string) bool {
	traceFile = cleanTracePath(traceFile)
	nodeFile = cleanTracePath(nodeFile)
	if traceFile == "" || nodeFile == "" {
		return false
	}
	if strings.EqualFold(traceFile, nodeFile) {
		return true
	}
	if strings.EqualFold(filepath.Base(traceFile), filepath.Base(nodeFile)) {
		return true
	}
	return strings.HasSuffix(strings.ToLower(nodeFile), "/"+strings.ToLower(traceFile)) ||
		strings.HasSuffix(strings.ToLower(traceFile), "/"+strings.ToLower(nodeFile))
}

func traceEntryMatchesNodeKind(entry profile.Entry, node Node) bool {
	switch entry.Category {
	case "apex.method":
		return node.Kind == NodeMethod
	case "apex.soql":
		return node.Kind == NodeSOQL
	case "apex.dml":
		return node.Kind == NodeDML
	case "apex.describe":
		return node.Kind == NodeDescribe
	case "apex.trigger":
		return node.Kind == NodeEntryPoint || node.Kind == NodeMethod
	case "apex.flow", "apex.workflow":
		return node.Kind == NodeAutomation || node.Kind == NodeEntryPoint
	case "apex.visualforce", "apex.visualforce.standard_controller":
		return node.Kind == NodeEntryPoint || node.Kind == NodeAutomation
	default:
		return true
	}
}

func traceEntryObjects(entry profile.Entry) []string {
	seen := map[string]struct{}{}
	var objects []string
	for _, object := range append([]string{entry.Object}, entry.Objects...) {
		object = strings.TrimSpace(object)
		if object == "" {
			continue
		}
		key := strings.ToLower(object)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		objects = append(objects, object)
	}
	return objects
}

func dmlNodeMentionsObject(node Node, object string) bool {
	object = strings.ToLower(trace.NormalizeFactText(object))
	if object == "" {
		return false
	}
	for _, value := range []string{node.Name, node.Operation} {
		value = strings.ToLower(trace.NormalizeFactText(value))
		if value == object || strings.Contains(value, " "+object) || strings.Contains(value, object+" ") {
			return true
		}
	}
	return false
}

func traceEntryNames(entry profile.Entry) []string {
	var names []string
	names = appendTraceName(names, entry.Name)
	names = appendTraceName(names, entry.EntryPoint)
	names = appendTraceName(names, entry.Operation)
	if entry.Class != "" && entry.Method != "" {
		names = appendTraceName(names, entry.Class+"."+entry.Method)
	}
	return names
}

func traceNameMatchesNode(traceName, nodeName string) bool {
	traceName = trace.NormalizeFactText(traceName)
	nodeName = trace.NormalizeFactText(nodeName)
	if traceName == "" || nodeName == "" {
		return false
	}
	if strings.EqualFold(traceName, nodeName) {
		return true
	}
	return strings.HasSuffix(strings.ToLower(traceName), "."+strings.ToLower(nodeName)) ||
		strings.HasSuffix(strings.ToLower(nodeName), "."+strings.ToLower(traceName))
}

func appendTraceName(names []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return names
	}
	for _, existing := range names {
		if strings.EqualFold(existing, value) {
			return names
		}
	}
	return append(names, value)
}

func traceEvidenceOperation(entry profile.Entry) string {
	if entry.OperationID != "" {
		return entry.OperationID
	}
	return entry.Operation
}

func traceEntryKey(entry profile.Entry) string {
	return strings.Join([]string{
		entry.Category,
		entry.Name,
		entry.File,
		entry.OperationID,
		strconv.Itoa(firstLine(entry)),
	}, "\x00")
}

func cleanTracePath(path string) string {
	return filepath.ToSlash(strings.TrimSpace(path))
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
