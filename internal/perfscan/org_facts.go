package perfscan

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/glade-sh/glade/internal/soql"
)

const (
	queryPlanLargeObjectRows = 100000
	parentSkewHighCount      = 10000
)

type OrgFacts struct {
	SchemaVersion int                       `json:"schemaVersion"`
	Objects       map[string]OrgObjectFacts `json:"objects"`
}

type OrgObjectFacts struct {
	EstimatedRows int64                `json:"estimatedRows,omitempty"`
	SharingModel  string               `json:"sharingModel,omitempty"`
	Fields        map[string]FieldFact `json:"fields,omitempty"`
	ParentSkew    []ParentSkewFact     `json:"parentSkew,omitempty"`
}

type FieldFact struct {
	Indexed bool `json:"indexed,omitempty"`
	Unique  bool `json:"unique,omitempty"`
	Formula bool `json:"formula,omitempty"`
}

type ParentSkewFact struct {
	Field    string `json:"field"`
	ParentID string `json:"parentId"`
	Count    int64  `json:"count"`
}

func LoadOrgFacts(path string) (OrgFacts, error) {
	if strings.TrimSpace(path) == "" {
		return OrgFacts{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return OrgFacts{}, err
	}
	var facts OrgFacts
	if err := json.Unmarshal(data, &facts); err != nil {
		return OrgFacts{}, err
	}
	if facts.Objects == nil {
		facts.Objects = map[string]OrgObjectFacts{}
	}
	return facts, nil
}

func ApplyOrgFacts(g *Graph, facts OrgFacts) {
	if g == nil || len(facts.Objects) == 0 {
		return
	}
	for index, node := range g.nodes {
		if node.Kind != NodeSOQL {
			continue
		}
		query, ok := parseSOQLNode(node)
		if !ok {
			continue
		}
		risk := queryPlanRisk(query, facts)
		if !risk.risky {
			continue
		}
		nodeID := NodeID(index + 1)
		g.AddResourceRisk(nodeID, ResourceRisk{DBTime: true, DBRows: true, SharedLimit: true})
		for _, evidence := range risk.evidence {
			g.AddEvidence(nodeID, evidence)
		}
	}
}

func emitOrgFactFindings(report *Report, graph *Graph, facts OrgFacts) {
	if graph == nil || len(facts.Objects) == 0 {
		return
	}
	emitOrgQueryPlanFindings(report, graph, facts)
	emitOrgParentSkewFindings(report, graph, facts)
}

func emitOrgQueryPlanFindings(report *Report, graph *Graph, facts OrgFacts) {
	for index, node := range graph.nodes {
		if node.Kind != NodeSOQL {
			continue
		}
		query, ok := parseSOQLNode(node)
		if !ok {
			continue
		}
		risk := queryPlanRisk(query, facts)
		if !risk.risky {
			continue
		}
		nodeID := NodeID(index + 1)
		entryID, path := firstEntryPath(graph, nodeID, 2)
		entry := EntryPoint{Kind: EntryUnknown}
		resourceRisk := ResourceRisk{DBTime: true, DBRows: true, SharedLimit: true}
		if entryID != 0 {
			if entryNode, ok := graph.node(entryID); ok {
				entry = entryPointFromGraphNode(entryNode)
			}
			resourceRisk = mergeResourceRisk(resourceRisk, graph.PathResourceRisk(entryID, nodeID))
		}
		evidence := append([]Evidence{{Kind: "soql", Message: "query", Value: queryTextFromNode(node)}}, risk.evidence...)
		finding := Finding{
			ID:           "perf.soql.query-plan-risk",
			Category:     CategorySOQL,
			Severity:     SeverityHigh,
			Confidence:   ConfidenceCombined,
			Score:        88,
			EntryPoint:   entry,
			Message:      "SOQL uses predicates that are risky against the supplied org row-count and field facts.",
			Location:     Location{File: node.File, Line: node.Line},
			Path:         path,
			Multiplicity: "per-query",
			Evidence:     evidence,
			ResourceRisk: resourceRisk,
			Fix:          "Check the query plan in the target org and move filters to indexed, selective fields or narrower predicates.",
		}
		report.AddFinding(finding)
	}
}

func emitOrgParentSkewFindings(report *Report, graph *Graph, facts OrgFacts) {
	type skewFinding struct {
		object string
		skew   ParentSkewFact
		node   Node
		found  bool
	}
	var findings []skewFinding
	for object, objectFacts := range facts.Objects {
		for _, skew := range objectFacts.ParentSkew {
			if skew.Count < parentSkewHighCount {
				continue
			}
			node, found := firstSkewSourceNode(graph, object, skew.Field)
			if !found {
				continue
			}
			findings = append(findings, skewFinding{object: object, skew: skew, node: node, found: found})
		}
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].skew.Count != findings[j].skew.Count {
			return findings[i].skew.Count > findings[j].skew.Count
		}
		if findings[i].object != findings[j].object {
			return findings[i].object < findings[j].object
		}
		return findings[i].skew.Field < findings[j].skew.Field
	})
	for _, item := range findings {
		location := Location{}
		if item.found {
			location = Location{File: item.node.File, Line: item.node.Line}
		}
		finding := Finding{
			ID:         "perf.data-skew.parent",
			Category:   CategoryDML,
			Severity:   SeverityHigh,
			Confidence: ConfidenceStatic,
			Score:      86,
			Message:    "Org facts show a parent skew that can amplify locking and query work.",
			Location:   location,
			Evidence: []Evidence{
				{Kind: "org-facts", Message: "parent skew object", Value: item.object},
				{Kind: "org-facts", Message: "parent skew field", Value: item.skew.Field},
				{Kind: "org-facts", Message: "parent skew count", Value: strconv.FormatInt(item.skew.Count, 10)},
				{Kind: "org-facts", Message: "parent skew parent id", Value: item.skew.ParentID},
			},
			ResourceRisk: ResourceRisk{DBTime: true, DBRows: true, Locks: true, SharedLimit: true},
			Fix:          "Split hot-parent work, avoid broad child updates under one parent, and add selective child queries before DML.",
		}
		if !reportHasFinding(report, finding) {
			report.AddFinding(finding)
		}
	}
}

type orgQueryPlanRisk struct {
	risky    bool
	evidence []Evidence
}

func queryPlanRisk(query soql.Query, facts OrgFacts) orgQueryPlanRisk {
	objectName, objectFacts, ok := lookupOrgObjectFacts(facts, query.Object)
	if !ok {
		return orgQueryPlanRisk{}
	}
	if query.Where == nil {
		return orgQueryPlanRisk{}
	}
	fields := conditionFields(*query.Where)
	var evidence []Evidence
	if objectFacts.EstimatedRows >= queryPlanLargeObjectRows {
		evidence = append(evidence, Evidence{Kind: "org-facts", Message: "estimated rows", Value: strconv.FormatInt(objectFacts.EstimatedRows, 10)})
	}
	if strings.EqualFold(objectFacts.SharingModel, "Private") {
		evidence = append(evidence, Evidence{Kind: "org-facts", Message: "private sharing model", Value: objectName})
	}
	for _, field := range fields {
		fieldName, fieldFacts, ok := lookupOrgFieldFacts(objectFacts, field)
		if !ok {
			continue
		}
		switch {
		case fieldFacts.Formula:
			evidence = append(evidence, Evidence{Kind: "org-facts", Message: "formula field in WHERE", Value: fieldName})
		case !fieldFacts.Indexed && objectFacts.EstimatedRows >= queryPlanLargeObjectRows:
			evidence = append(evidence, Evidence{Kind: "org-facts", Message: "unindexed field in WHERE", Value: fieldName})
		}
	}
	if !hasQueryPlanShapeEvidence(evidence) {
		return orgQueryPlanRisk{}
	}
	return orgQueryPlanRisk{risky: true, evidence: dedupeEvidence(evidence)}
}

func hasQueryPlanShapeEvidence(evidence []Evidence) bool {
	hasRows := false
	hasFieldRisk := false
	hasPrivate := false
	for _, item := range evidence {
		switch item.Message {
		case "estimated rows":
			hasRows = true
		case "formula field in WHERE", "unindexed field in WHERE":
			hasFieldRisk = true
		case "private sharing model":
			hasPrivate = true
		}
	}
	return hasRows && (hasFieldRisk || hasPrivate)
}

func parseSOQLNode(node Node) (soql.Query, bool) {
	query, err := soql.Parse(queryTextFromNode(node))
	if err != nil {
		return soql.Query{}, false
	}
	return query, true
}

func queryTextFromNode(node Node) string {
	text := strings.TrimSpace(node.Operation)
	if text == "" {
		text = strings.TrimSpace(node.Name)
	}
	if strings.HasPrefix(text, "[") && strings.HasSuffix(text, "]") {
		text = strings.TrimSpace(text[1 : len(text)-1])
	}
	return text
}

func conditionFields(condition soql.Condition) []string {
	seen := map[string]struct{}{}
	var out []string
	var walk func(soql.Condition)
	walk = func(current soql.Condition) {
		if current.Field != "" {
			field := leafFieldName(current.Field)
			key := strings.ToLower(field)
			if key != "" {
				if _, ok := seen[key]; !ok {
					seen[key] = struct{}{}
					out = append(out, field)
				}
			}
		}
		for _, child := range current.And {
			walk(child)
		}
		for _, child := range current.Or {
			walk(child)
		}
	}
	walk(condition)
	sort.Strings(out)
	return out
}

func lookupOrgObjectFacts(facts OrgFacts, object string) (string, OrgObjectFacts, bool) {
	for name, objectFacts := range facts.Objects {
		if strings.EqualFold(strings.TrimSpace(name), strings.TrimSpace(object)) {
			return name, objectFacts, true
		}
	}
	return "", OrgObjectFacts{}, false
}

func lookupOrgFieldFacts(object OrgObjectFacts, field string) (string, FieldFact, bool) {
	for name, fieldFacts := range object.Fields {
		if strings.EqualFold(strings.TrimSpace(name), strings.TrimSpace(field)) {
			return name, fieldFacts, true
		}
	}
	return "", FieldFact{}, false
}

func firstSkewSourceNode(graph *Graph, object, field string) (Node, bool) {
	if graph == nil {
		return Node{}, false
	}
	for _, node := range graph.nodes {
		switch node.Kind {
		case NodeDML:
			for _, touched := range dmlObjectsFromOperation(node.Operation + " " + node.Name) {
				if strings.EqualFold(touched, object) {
					return node, true
				}
			}
		case NodeSOQL:
			query, ok := parseSOQLNode(node)
			if !ok || !strings.EqualFold(query.Object, object) || query.Where == nil {
				continue
			}
			for _, queryField := range conditionFields(*query.Where) {
				if strings.EqualFold(queryField, field) {
					return node, true
				}
			}
		}
	}
	return Node{}, false
}

func leafFieldName(field string) string {
	parts := strings.Split(strings.TrimSpace(field), ".")
	if len(parts) == 0 {
		return strings.TrimSpace(field)
	}
	return strings.TrimSpace(parts[len(parts)-1])
}

func dedupeEvidence(evidence []Evidence) []Evidence {
	seen := map[string]struct{}{}
	var out []Evidence
	for _, item := range evidence {
		key := strings.Join([]string{item.Kind, item.Message, item.Value}, "|")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		return fmt.Sprintf("%s|%s|%s", out[i].Kind, out[i].Message, out[i].Value) <
			fmt.Sprintf("%s|%s|%s", out[j].Kind, out[j].Message, out[j].Value)
	})
	return out
}
