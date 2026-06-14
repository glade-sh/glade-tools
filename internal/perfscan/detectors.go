package perfscan

import (
	"fmt"
	"sort"
	"strings"
)

func emitSourceGraphFindings(report *Report, graph *Graph) {
	for _, finding := range detectSourceGraphFindings(graph) {
		if reportHasFinding(report, finding) {
			continue
		}
		report.AddFinding(finding)
	}
}

func detectSourceGraphFindings(graph *Graph) []Finding {
	if graph == nil {
		return nil
	}
	var findings []Finding
	findings = append(findings, detectInterproceduralLoopFindings(graph, NodeSOQL)...)
	findings = append(findings, detectInterproceduralLoopFindings(graph, NodeDML)...)
	findings = append(findings, detectStaticFirstTouchFindings(graph)...)
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Score != findings[j].Score {
			return findings[i].Score > findings[j].Score
		}
		if findings[i].ID != findings[j].ID {
			return findings[i].ID < findings[j].ID
		}
		return findings[i].Location.Line < findings[j].Location.Line
	})
	return findings
}

func detectInterproceduralLoopFindings(graph *Graph, kind NodeKind) []Finding {
	var findings []Finding
	for opIndex, node := range graph.nodes {
		if node.Kind != kind {
			continue
		}
		opID := NodeID(opIndex + 1)
		if !graphHasEvidence(opID, graph, "static", "per-record path") {
			continue
		}
		entryID, path := firstEntryPath(graph, opID, 4)
		if len(path) == 0 {
			continue
		}
		entryNode, _ := graph.node(entryID)
		category := CategorySOQL
		id := "perf.soql.loop.interprocedural"
		score := 93
		message := "SOQL is reachable from a per-record loop through a call path."
		fix := "Move selector work outside the loop and pass the full record set into one bulk query."
		if kind == NodeDML {
			category = CategoryDML
			id = "perf.dml.loop.interprocedural"
			score = 91
			message = "DML is reachable from a per-record loop through a call path."
			fix = "Accumulate records through the call path and perform one DML operation after the loop."
		}
		risk := graph.PathResourceRisk(entryID, opID)
		evidence := append([]Evidence{}, graph.Evidence(opID)...)
		evidence = append(evidence, Evidence{
			Kind:         "static",
			Message:      "graph path",
			NodeID:       fmt.Sprint(opID),
			Path:         path,
			ResourceRisk: risk,
		})
		findings = append(findings, Finding{
			ID:            id,
			Category:      category,
			Severity:      SeverityHigh,
			Confidence:    confidenceFromEvidence(evidence, ConfidenceStatic),
			Score:         score,
			EntryPoint:    entryPointFromGraphNode(entryNode),
			Message:       message,
			Location:      Location{File: node.File, Line: node.Line},
			Path:          path,
			NamespacePath: namespacePath(graph, graph.PathNodeIDs(entryID, opID)),
			Multiplicity:  "per-record",
			Evidence:      evidence,
			ResourceRisk:  risk,
			Fix:           fix,
		})
	}
	return findings
}

func detectStaticFirstTouchFindings(graph *Graph) []Finding {
	var findings []Finding
	for index, node := range graph.nodes {
		if node.Kind != NodeStaticInit {
			continue
		}
		initID := NodeID(index + 1)
		if !graphHasEvidence(initID, graph, "static", "first touch") {
			continue
		}
		entryID, path := firstEntryPath(graph, initID, 3)
		if len(path) == 0 {
			continue
		}
		entryNode, _ := graph.node(entryID)
		risk := graph.PathResourceRisk(entryID, initID)
		evidence := append([]Evidence{}, graph.Evidence(initID)...)
		evidence = append(evidence, Evidence{
			Kind:         "static",
			Message:      "graph path",
			NodeID:       fmt.Sprint(initID),
			Path:         path,
			ResourceRisk: risk,
		})
		findings = append(findings, Finding{
			ID:            "perf.static.first-touch",
			Category:      CategoryDescribe,
			Severity:      SeverityHigh,
			Confidence:    confidenceFromEvidence(evidence, ConfidenceStatic),
			Score:         86,
			EntryPoint:    entryPointFromGraphNode(entryNode),
			Message:       "A cheap static field read first-touches describe, SOQL, or metadata work.",
			Location:      Location{File: node.File, Line: node.Line},
			Path:          path,
			NamespacePath: namespacePath(graph, graph.PathNodeIDs(entryID, initID)),
			Multiplicity:  "once-per-transaction",
			Evidence:      evidence,
			ResourceRisk:  risk,
			Fix:           "Split cheap constants from metadata/config loaders and lazy-load describe/config by key.",
			Acceptance:    "Referencing a string constant must not execute describe, SOQL, or getAll work.",
		})
	}
	return findings
}

func confidenceFromEvidence(evidence []Evidence, fallback Confidence) Confidence {
	hasTrace := false
	hasStatic := false
	for _, item := range evidence {
		switch item.Kind {
		case "trace":
			hasTrace = true
		case "static", "soql", "dml", "flow", "workflow", "trigger", "org-facts", "rollup", "sharing":
			hasStatic = true
		}
	}
	if hasTrace && hasStatic {
		return ConfidenceCombined
	}
	if hasTrace {
		return ConfidenceMeasured
	}
	if fallback == "" {
		return ConfidenceStatic
	}
	return fallback
}

func firstEntryPath(graph *Graph, target NodeID, minLen int) (NodeID, []PathStep) {
	type candidate struct {
		id   NodeID
		path []PathStep
	}
	var candidates []candidate
	for index, node := range graph.nodes {
		if node.Kind != NodeEntryPoint {
			continue
		}
		entryID := NodeID(index + 1)
		path := graph.Path(entryID, target)
		if len(path) < minLen {
			continue
		}
		candidates = append(candidates, candidate{id: entryID, path: path})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if len(candidates[i].path) != len(candidates[j].path) {
			return len(candidates[i].path) < len(candidates[j].path)
		}
		return formatPath(candidates[i].path) < formatPath(candidates[j].path)
	})
	if len(candidates) == 0 {
		return 0, nil
	}
	return candidates[0].id, candidates[0].path
}

func entryPointFromGraphNode(node Node) EntryPoint {
	kind := EntryKind(node.Operation)
	if kind == "" {
		kind = EntryUnknown
	}
	return EntryPoint{
		Kind: kind,
		Name: node.Name,
		File: node.File,
		Line: node.Line,
	}
}

func namespacePath(graph *Graph, ids []NodeID) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, id := range ids {
		node, ok := graph.node(id)
		if !ok || node.Namespace == "" {
			continue
		}
		if _, exists := seen[node.Namespace]; exists {
			continue
		}
		seen[node.Namespace] = struct{}{}
		out = append(out, node.Namespace)
	}
	return out
}

func graphHasEvidence(id NodeID, graph *Graph, kind, messagePart string) bool {
	for _, evidence := range graph.Evidence(id) {
		if kind != "" && evidence.Kind != kind {
			continue
		}
		if strings.Contains(evidence.Message, messagePart) {
			return true
		}
	}
	return false
}

func reportHasFinding(report *Report, finding Finding) bool {
	for _, existing := range report.Findings {
		if existing.ID != finding.ID {
			continue
		}
		if existing.Location.File == finding.Location.File && existing.Location.Line == finding.Location.Line {
			return true
		}
	}
	return false
}
