package perfscan

import "testing"

func TestGraphBuildsStableTransactionPath(t *testing.T) {
	g := NewGraph()
	trigger := g.AddNode(Node{Kind: NodeEntryPoint, Name: "AccountTrigger", File: "Account.trigger", Line: 1})
	method := g.AddNode(Node{Kind: NodeMethod, Name: "PricingService.reprice", File: "PricingService.cls", Line: 8})
	query := g.AddNode(Node{Kind: NodeSOQL, Name: "SELECT Id FROM Product2", File: "PricingService.cls", Line: 10})
	g.AddEdge(trigger, method, EdgeCalls)
	g.AddEdge(method, query, EdgeExecutes)
	g.AddEvidence(query, Evidence{Kind: "static", Message: "query in per-record path"})

	path := g.Path(trigger, query)
	if len(path) != 3 {
		t.Fatalf("path = %#v", path)
	}
	if path[2].Kind != "soql" || path[2].Name != "SELECT Id FROM Product2" {
		t.Fatalf("path tail = %#v", path[2])
	}
	if len(g.Evidence(query)) != 1 {
		t.Fatalf("evidence = %#v", g.Evidence(query))
	}
}

func TestGraphMergesDuplicateNodesEvidenceAndRisk(t *testing.T) {
	g := NewGraph()
	first := g.AddNode(Node{Kind: NodeSOQL, Name: "SELECT Id FROM Account", File: "force-app/main/default/classes/Selector.cls", Line: 12})
	second := g.AddNode(Node{Kind: NodeSOQL, Name: "SELECT Id FROM Account", File: "force-app/main/default/classes/Selector.cls", Line: 12})
	if first != second {
		t.Fatalf("duplicate node IDs = %d and %d", first, second)
	}

	g.AddEvidence(first, Evidence{Kind: "trace", Message: "duration ms", Value: "421"})
	g.AddEvidence(first, Evidence{Kind: "static", Message: "query in per-record path"})
	g.AddEvidence(second, Evidence{Kind: "static", Message: "query in per-record path"})
	evidence := g.Evidence(first)
	if len(evidence) != 2 {
		t.Fatalf("evidence = %#v", evidence)
	}
	if evidence[0].Kind != "static" || evidence[1].Kind != "trace" {
		t.Fatalf("evidence order = %#v", evidence)
	}

	g.AddResourceRisk(first, ResourceRisk{CPU: true})
	g.AddResourceRisk(second, ResourceRisk{DBRows: true, SharedLimit: true})
	risk := g.ResourceRisk(first)
	if !risk.CPU || !risk.DBRows || !risk.SharedLimit {
		t.Fatalf("risk = %#v", risk)
	}
}

func TestGraphOrdersPathsDeterministically(t *testing.T) {
	g := NewGraph()
	entry := g.AddNode(Node{Kind: NodeEntryPoint, Name: "AccountTrigger", File: "Account.trigger", Line: 1})
	later := g.AddNode(Node{Kind: NodeMethod, Name: "ZService.work", File: "ZService.cls", Line: 4})
	target := g.AddNode(Node{Kind: NodeDML, Name: "update Account", File: "Dml.cls", Line: 9})
	earlier := g.AddNode(Node{Kind: NodeMethod, Name: "AService.work", File: "AService.cls", Line: 4})
	g.AddEdge(entry, later, EdgeCalls)
	g.AddEdge(later, target, EdgeExecutes)
	g.AddEdge(entry, earlier, EdgeCalls)
	g.AddEdge(earlier, target, EdgeExecutes)

	path := g.Path(entry, target)
	if len(path) != 3 {
		t.Fatalf("path = %#v", path)
	}
	if path[1].Name != "AService.work" {
		t.Fatalf("path order = %#v", path)
	}

	g.AddResourceRisk(entry, ResourceRisk{CPU: true})
	g.AddResourceRisk(target, ResourceRisk{Locks: true})
	risk := g.PathResourceRisk(entry, target)
	if !risk.CPU || !risk.Locks {
		t.Fatalf("path risk = %#v", risk)
	}
}
