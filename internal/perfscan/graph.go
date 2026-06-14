package perfscan

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

type NodeKind string

const (
	NodeEntryPoint NodeKind = "entryPoint"
	NodeMethod     NodeKind = "method"
	NodeSOQL       NodeKind = "soql"
	NodeDML        NodeKind = "dml"
	NodeDescribe   NodeKind = "describe"
	NodeStaticInit NodeKind = "staticInit"
	NodeAutomation NodeKind = "automation"
)

type EdgeKind string

const (
	EdgeCalls    EdgeKind = "calls"
	EdgeExecutes EdgeKind = "executes"
	EdgeWakes    EdgeKind = "wakes"
	EdgeMeasures EdgeKind = "measures"
)

type NodeID int

type Node struct {
	Kind      NodeKind
	Name      string
	File      string
	Line      int
	Namespace string
	Operation string
}

type Edge struct {
	From NodeID
	To   NodeID
	Kind EdgeKind
}

type Graph struct {
	nodes        []Node
	nodeIDs      map[string]NodeID
	nodeKeys     map[NodeID]string
	edges        map[NodeID][]Edge
	edgeKeys     map[string]struct{}
	evidence     map[NodeID][]Evidence
	evidenceKeys map[NodeID]map[string]struct{}
	risks        map[NodeID]ResourceRisk
}

func NewGraph() *Graph {
	return &Graph{
		nodeIDs:      map[string]NodeID{},
		nodeKeys:     map[NodeID]string{},
		edges:        map[NodeID][]Edge{},
		edgeKeys:     map[string]struct{}{},
		evidence:     map[NodeID][]Evidence{},
		evidenceKeys: map[NodeID]map[string]struct{}{},
		risks:        map[NodeID]ResourceRisk{},
	}
}

func (g *Graph) AddNode(n Node) NodeID {
	n.File = filepath.ToSlash(n.File)
	key := nodeKey(n)
	if id, ok := g.nodeIDs[key]; ok {
		return id
	}
	id := NodeID(len(g.nodes) + 1)
	g.nodes = append(g.nodes, n)
	g.nodeIDs[key] = id
	g.nodeKeys[id] = key
	return id
}

func (g *Graph) AddEdge(from, to NodeID, kind EdgeKind) {
	if !g.validNode(from) || !g.validNode(to) || kind == "" {
		return
	}
	key := fmt.Sprintf("%d|%d|%s", from, to, kind)
	if _, ok := g.edgeKeys[key]; ok {
		return
	}
	g.edgeKeys[key] = struct{}{}
	g.edges[from] = append(g.edges[from], Edge{From: from, To: to, Kind: kind})
	sort.Slice(g.edges[from], func(i, j int) bool {
		left := g.edges[from][i]
		right := g.edges[from][j]
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		if g.nodeKeys[left.To] != g.nodeKeys[right.To] {
			return g.nodeKeys[left.To] < g.nodeKeys[right.To]
		}
		return left.To < right.To
	})
}

func (g *Graph) AddEvidence(node NodeID, e Evidence) {
	if !g.validNode(node) || e.Kind == "" || e.Message == "" {
		return
	}
	if g.evidenceKeys[node] == nil {
		g.evidenceKeys[node] = map[string]struct{}{}
	}
	key := evidenceKey(e)
	if _, ok := g.evidenceKeys[node][key]; ok {
		return
	}
	g.evidenceKeys[node][key] = struct{}{}
	g.evidence[node] = append(g.evidence[node], e)
	sort.Slice(g.evidence[node], func(i, j int) bool {
		return evidenceKey(g.evidence[node][i]) < evidenceKey(g.evidence[node][j])
	})
}

func (g *Graph) Evidence(node NodeID) []Evidence {
	values := g.evidence[node]
	if len(values) == 0 {
		return nil
	}
	out := make([]Evidence, len(values))
	copy(out, values)
	return out
}

func (g *Graph) AddResourceRisk(node NodeID, risk ResourceRisk) {
	if !g.validNode(node) {
		return
	}
	g.risks[node] = mergeResourceRisk(g.risks[node], risk)
}

func (g *Graph) ResourceRisk(node NodeID) ResourceRisk {
	return g.risks[node]
}

func (g *Graph) PathResourceRisk(from, to NodeID) ResourceRisk {
	var risk ResourceRisk
	for _, node := range g.PathNodeIDs(from, to) {
		risk = mergeResourceRisk(risk, g.ResourceRisk(node))
	}
	return risk
}

func (g *Graph) Path(from, to NodeID) []PathStep {
	ids := g.PathNodeIDs(from, to)
	if len(ids) == 0 {
		return nil
	}
	path := make([]PathStep, 0, len(ids))
	for _, id := range ids {
		node, ok := g.node(id)
		if !ok {
			continue
		}
		path = append(path, PathStep{
			Kind: string(node.Kind),
			Name: node.Name,
			File: node.File,
			Line: node.Line,
		})
	}
	return path
}

func (g *Graph) PathNodeIDs(from, to NodeID) []NodeID {
	if !g.validNode(from) || !g.validNode(to) {
		return nil
	}
	if from == to {
		return []NodeID{from}
	}

	visited := map[NodeID]bool{from: true}
	parent := map[NodeID]NodeID{}
	queue := []NodeID{from}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, edge := range g.edges[current] {
			if visited[edge.To] {
				continue
			}
			visited[edge.To] = true
			parent[edge.To] = current
			if edge.To == to {
				return reconstructPath(parent, from, to)
			}
			queue = append(queue, edge.To)
		}
	}
	return nil
}

func (g *Graph) node(id NodeID) (Node, bool) {
	if !g.validNode(id) {
		return Node{}, false
	}
	return g.nodes[int(id)-1], true
}

func (g *Graph) validNode(id NodeID) bool {
	return id > 0 && int(id) <= len(g.nodes)
}

func reconstructPath(parent map[NodeID]NodeID, from, to NodeID) []NodeID {
	var reversed []NodeID
	for current := to; current != 0; current = parent[current] {
		reversed = append(reversed, current)
		if current == from {
			break
		}
	}
	if reversed[len(reversed)-1] != from {
		return nil
	}
	path := make([]NodeID, len(reversed))
	for i := range reversed {
		path[i] = reversed[len(reversed)-1-i]
	}
	return path
}

func nodeKey(n Node) string {
	return strings.Join([]string{
		string(n.Kind),
		n.Namespace,
		filepath.ToSlash(n.File),
		fmt.Sprint(n.Line),
		n.Name,
		n.Operation,
	}, "|")
}

func evidenceKey(e Evidence) string {
	return strings.Join([]string{
		e.Kind,
		e.Message,
		e.Value,
		e.NodeID,
		e.Operation,
		formatPath(e.Path),
		formatResourceRisk(e.ResourceRisk),
	}, "|")
}
