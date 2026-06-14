package perfscan

import (
	"strings"
	"testing"

	"github.com/glade-sh/glade/internal/apexast"
	"github.com/glade-sh/glade/internal/diagnostic"
)

func TestSourceGraphPropagatesPerRecordQueryThroughCall(t *testing.T) {
	root := testPerfProject(t, map[string]string{
		"force-app/main/default/triggers/AccountTrigger.trigger": `
trigger AccountTrigger on Account (after update) {
  for (Account account : Trigger.new) {
    PricingService.reprice(account);
  }
}`,
		"force-app/main/default/classes/PricingService.cls": `
public class PricingService {
  public static void reprice(Account account) {
    ProductSelector.byFamily(account.Industry);
  }
}`,
		"force-app/main/default/classes/ProductSelector.cls": `
public class ProductSelector {
  public static List<Product2> byFamily(String family) {
    return [SELECT Id, Name FROM Product2 WHERE Family = :family];
  }
}`,
	})
	report := analyzeTestProject(t, root, Options{})

	finding := requireFinding(t, report, "perf.soql.loop.interprocedural")
	if finding.Multiplicity != "per-record" {
		t.Fatalf("multiplicity = %q", finding.Multiplicity)
	}
	if len(finding.Path) < 4 {
		t.Fatalf("path = %#v", finding.Path)
	}
	if !finding.ResourceRisk.DBRows || !finding.ResourceRisk.DBTime || !finding.ResourceRisk.SharedLimit {
		t.Fatalf("resource risk = %#v", finding.ResourceRisk)
	}
	requireEvidence(t, finding, "static", "per-record path")
}

func TestSourceGraphDoesNotPropagateAcrossWrongOverload(t *testing.T) {
	root := testPerfProject(t, map[string]string{
		"force-app/main/default/classes/EntryService.cls": `
public class EntryService {
  @InvocableMethod
  public static void run(List<String> values) {
    for (String value : values) {
      Selector.work(value);
    }
  }
}`,
		"force-app/main/default/classes/Selector.cls": `
public class Selector {
  public static void work(String value) {
  }
  public static void work(Integer value) {
    List<Account> accounts = [SELECT Id FROM Account];
  }
}`,
	})

	report := analyzeTestProject(t, root, Options{})

	for _, finding := range report.Findings {
		if finding.ID == "perf.soql.loop.interprocedural" {
			t.Fatalf("unexpected overload propagation finding: %#v", finding)
		}
	}
}

func TestSourceGraphAnnotatesVariableDMLObject(t *testing.T) {
	root := testPerfProject(t, map[string]string{
		"force-app/main/default/classes/AccountUpdater.cls": `
public class AccountUpdater {
  public static void run() {
    Account account = new Account(Name = 'Acme');
    update account;
  }
}`,
	})

	graph := buildTestSourceGraph(t, root)

	for _, node := range graph.nodes {
		if node.Kind != NodeDML {
			continue
		}
		if strings.Contains(node.Operation, "object:Account") {
			return
		}
		t.Fatalf("DML node missing object marker: %#v", node)
	}
	t.Fatalf("missing DML node in graph: %#v", graph.nodes)
}

func TestSourceLocalTypesCaptureMethodBodyDeclarations(t *testing.T) {
	source := `
public class AccountUpdater {
  public static void run() {
    Account account = new Account(Name = 'Acme');
    update account;
  }
}`
	decl := apexast.Declaration{
		Range: diagnostic.Range{
			Start: diagnostic.Position{Line: 3, Offset: strings.Index(source, "public static void run")},
			End:   diagnostic.Position{Line: 6, Offset: strings.Index(source, "  }\n}") + len("  }")},
		},
	}

	types := sourceLocalTypes(source, decl)

	if types["account"] != "Account" {
		t.Fatalf("local types = %#v", types)
	}
}

func TestSourceDMLObjectNamesUseLocalTypes(t *testing.T) {
	method := &sourceMethodFact{localTypes: map[string]string{"account": "Account"}}

	objects := sourceDMLObjectNames(method, "update account")

	if len(objects) != 1 || objects[0] != "Account" {
		t.Fatalf("objects = %#v", objects)
	}
}
