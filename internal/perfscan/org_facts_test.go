package perfscan

import "testing"

func TestLoadOrgFactsReadsSnapshot(t *testing.T) {
	path := writeOrgFactsFixture(t)

	facts, err := LoadOrgFacts(path)
	if err != nil {
		t.Fatal(err)
	}

	account := facts.Objects["Account"]
	if facts.SchemaVersion != 1 || account.EstimatedRows != 1200000 || account.SharingModel != "Private" {
		t.Fatalf("facts = %#v", facts)
	}
	formula := account.Fields["Formula_Key__c"]
	if !formula.Formula {
		t.Fatalf("formula field facts = %#v", formula)
	}
	contact := facts.Objects["Contact"]
	if len(contact.ParentSkew) != 1 || contact.ParentSkew[0].Count != 24000 {
		t.Fatalf("parent skew = %#v", contact.ParentSkew)
	}
}

func TestOrgFactsRaiseSelectivityAndSkewRisk(t *testing.T) {
	root := testPerfProject(t, map[string]string{
		"force-app/main/default/classes/QueryRisk.cls": `
public class QueryRisk {
  public static List<Account> byFormula(String value) {
    return [SELECT Id FROM Account WHERE Formula_Key__c = :value];
  }
  public static List<Contact> contactsByAccount(Id accountId) {
    return [SELECT Id FROM Contact WHERE AccountId = :accountId];
  }
}`,
	})

	report := analyzeTestProject(t, root, Options{OrgFactsPath: writeOrgFactsFixture(t)})

	queryPlan := requireFinding(t, report, "perf.soql.query-plan-risk")
	requireEvidence(t, queryPlan, "org-facts", "estimated rows")
	requireEvidence(t, queryPlan, "org-facts", "formula field")

	skew := requireFinding(t, report, "perf.data-skew.parent")
	requireEvidence(t, skew, "org-facts", "parent skew")
}

func TestOrgFactsSkipUnreferencedParentSkew(t *testing.T) {
	root := testPerfProject(t, map[string]string{
		"force-app/main/default/classes/QueryRisk.cls": `
public class QueryRisk {
  public static List<Account> byFormula(String value) {
    return [SELECT Id FROM Account WHERE Formula_Key__c = :value];
  }
}`,
	})

	report := analyzeTestProject(t, root, Options{OrgFactsPath: writeOrgFactsFixture(t)})

	for _, finding := range report.Findings {
		if finding.ID == "perf.data-skew.parent" {
			t.Fatalf("unexpected unreferenced skew finding: %#v", finding)
		}
	}
}
