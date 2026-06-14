package perfscan

import (
	"strings"
	"testing"
)

func TestMetadataGraphExplainsDMLBlastRadius(t *testing.T) {
	root := testPerfProjectWithMetadata(t)
	report := analyzeTestProject(t, root, Options{})

	finding := requireFinding(t, report, "perf.dml.blast-radius")
	if finding.ResourceRisk.CPU != true || finding.ResourceRisk.DBTime != true || finding.ResourceRisk.Locks != true || finding.ResourceRisk.SharedLimit != true {
		t.Fatalf("resource risk = %#v", finding.ResourceRisk)
	}
	requireEvidence(t, finding, "flow", "record-triggered flows")
	requireEvidence(t, finding, "workflow", "active workflow rules")
}

func TestAnalyzeProjectDetectsMetadataRisks(t *testing.T) {
	root := testPerfProjectWithMetadata(t)
	report := analyzeTestProject(t, root, Options{})

	requireFinding(t, report, "perf.automation.flow.data-fanout")
	requireFinding(t, report, "perf.automation.workflow.active-rule")
	requireFinding(t, report, "perf.dml.blast-radius")
}

func TestMetadataGraphExplainsVariableDMLBlastRadius(t *testing.T) {
	root := testPerfProject(t, map[string]string{
		"force-app/main/default/classes/AccountUpdater.cls": `
public class AccountUpdater {
  @InvocableMethod
  public static void run(List<Id> ids) {
    Account account = new Account(Id = ids[0], Description = 'changed');
    update account;
  }
}`,
		"force-app/main/default/flows/Account_After_Save.flow-meta.xml": "<Flow><start><object>Account</object><triggerType>RecordAfterSave</triggerType></start></Flow>",
		"force-app/main/default/workflows/Account.workflow-meta.xml":    "<Workflow><rules><fullName>Active_Rule</fullName><active>true</active></rules></Workflow>",
	})

	report := analyzeTestProject(t, root, Options{})

	finding := requireFinding(t, report, "perf.dml.blast-radius")
	if !strings.HasSuffix(finding.Location.File, "AccountUpdater.cls") {
		t.Fatalf("location = %#v, want AccountUpdater.cls", finding.Location)
	}
	requireEvidence(t, finding, "flow", "record-triggered flows")
	requireEvidence(t, finding, "workflow", "active workflow rules")
}
