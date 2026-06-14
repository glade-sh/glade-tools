package perfscan

import (
	"testing"

	"github.com/glade-sh/glade/internal/profile"
	"github.com/glade-sh/glade/internal/trace"
)

func TestTraceCorrelationAttachesEvidenceByFileLine(t *testing.T) {
	g := NewGraph()
	method := g.AddNode(Node{Kind: NodeMethod, Name: "PerfRisk.run", File: "force-app/main/default/classes/PerfRisk.cls", Line: 3})
	query := g.AddNode(Node{Kind: NodeSOQL, Name: "SELECT Id FROM Contact WHERE AccountId = :account.Id", File: "force-app/main/default/classes/PerfRisk.cls", Line: 5})

	traceReport := profile.Analyze(trace.NewDocument([]trace.Event{
		trace.Duration("apex.method.PerfRisk.run", "apex.method", 0, 250000, map[string]any{
			trace.ArgFile: "PerfRisk.cls",
			trace.ArgLine: 3,
		}),
		trace.Instant("apex.soql", "apex.soql", 260000, map[string]any{
			trace.ArgQuery: "SELECT Id FROM Contact WHERE AccountId = :account.Id",
			trace.ArgRows:  1000,
			trace.ArgFile:  "PerfRisk.cls",
			trace.ArgLine:  5,
		}),
		trace.Duration("apex.soql", "apex.soql", 260000, 120000, map[string]any{
			trace.ArgQuery: "SELECT Id FROM Contact WHERE AccountId = :account.Id",
			trace.ArgFile:  "PerfRisk.cls",
			trace.ArgLine:  5,
		}),
	}))

	result := CorrelateTrace(g, traceReport)

	if len(result.Matched) != 2 {
		t.Fatalf("matched = %#v", result.Matched)
	}
	requireGraphEvidence(t, g, method, "duration ms", "250")
	requireGraphEvidence(t, g, method, "count", "1")
	requireGraphEvidence(t, g, query, "duration ms", "120")
	requireGraphEvidence(t, g, query, "count", "2")
	requireGraphEvidence(t, g, query, "rows", "1000")
}

func TestTraceCorrelationMatchesSOQLByQueryHash(t *testing.T) {
	queryText := "SELECT Id FROM Contact WHERE AccountId = :account.Id"
	g := NewGraph()
	query := g.AddNode(Node{Kind: NodeSOQL, Name: "SOQL", Operation: queryText, File: "Selector.cls", Line: 42})

	traceReport := profile.Analyze(trace.NewDocument([]trace.Event{
		trace.Instant("apex.soql", "apex.soql", 1000, map[string]any{
			trace.ArgQueryHash: trace.StableQueryHash(queryText),
			trace.ArgRows:      750,
		}),
		trace.Duration("apex.soql", "apex.soql", 1000, 85000, map[string]any{
			trace.ArgQueryHash: trace.StableQueryHash(queryText),
		}),
	}))

	result := CorrelateTrace(g, traceReport)

	if len(result.Matched) != 1 {
		t.Fatalf("matched = %#v", result.Matched)
	}
	requireGraphEvidence(t, g, query, "duration ms", "85")
	requireGraphEvidence(t, g, query, "rows", "750")
}

func TestTraceCorrelationMatchesDMLByObject(t *testing.T) {
	g := NewGraph()
	dml := g.AddNode(Node{Kind: NodeDML, Name: "update Account", File: "DmlRisk.cls", Line: 12})

	traceReport := profile.Analyze(trace.NewDocument([]trace.Event{
		trace.Instant("apex.dml", "apex.dml", 1000, map[string]any{
			trace.ArgObject: "Account",
			trace.ArgRows:   150,
		}),
		trace.Duration("apex.dml", "apex.dml", 1000, 65000, map[string]any{
			trace.ArgObject: "Account",
		}),
	}))

	result := CorrelateTrace(g, traceReport)

	if len(result.Matched) != 1 {
		t.Fatalf("matched = %#v", result.Matched)
	}
	requireGraphEvidence(t, g, dml, "duration ms", "65")
	requireGraphEvidence(t, g, dml, "rows", "150")
}

func TestAnalyzeTracePromotesStaticFindingToCombined(t *testing.T) {
	root := testPerfProject(t, map[string]string{
		"force-app/main/default/classes/PerfRisk.cls": `
public class PerfRisk {
  public static void run(List<Account> accounts) {
    for (Account account : accounts) {
      List<Contact> contacts = [SELECT Id FROM Contact WHERE AccountId = :account.Id];
    }
  }
}`,
	})
	tracePath := writeTrace(t, []trace.Event{
		trace.Duration("apex.method.PerfRisk.run", "apex.method", 0, 250000, map[string]any{
			trace.ArgFile: "PerfRisk.cls",
			trace.ArgLine: 3,
		}),
		trace.Instant("apex.soql", "apex.soql", 260000, map[string]any{
			trace.ArgQuery: "SELECT Id FROM Contact WHERE AccountId = :account.Id",
			trace.ArgRows:  1000,
			trace.ArgFile:  "PerfRisk.cls",
			trace.ArgLine:  5,
		}),
		trace.Duration("apex.soql", "apex.soql", 260000, 120000, map[string]any{
			trace.ArgQuery: "SELECT Id FROM Contact WHERE AccountId = :account.Id",
			trace.ArgFile:  "PerfRisk.cls",
			trace.ArgLine:  5,
		}),
	})

	report := analyzeTestProject(t, root, Options{TracePath: tracePath})
	finding := requireFinding(t, report, "perf.soql.loop")
	if finding.Confidence != ConfidenceCombined {
		t.Fatalf("confidence = %s, finding = %#v", finding.Confidence, finding)
	}
	requireEvidence(t, finding, "trace", "duration ms")
	requireEvidence(t, finding, "trace", "rows")
	assertNoFinding(t, report, "perf.measured.hot-span")
	assertNoFinding(t, report, "perf.measured.soql-rows")
	if len(report.Measurements) == 0 {
		t.Fatalf("missing trace measurements")
	}
}

func requireGraphEvidence(t *testing.T, g *Graph, node NodeID, message, value string) {
	t.Helper()
	for _, evidence := range g.Evidence(node) {
		if evidence.Kind == "trace" && evidence.Message == message && evidence.Value == value {
			return
		}
	}
	t.Fatalf("missing trace evidence %s=%s in %#v", message, value, g.Evidence(node))
}
