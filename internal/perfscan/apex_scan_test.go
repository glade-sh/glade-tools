package perfscan

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestAnalyzeProjectFindsApexPerformanceRisks(t *testing.T) {
	report, err := AnalyzeProject(Options{ProjectRoot: filepath.Join("testdata", "perf-project")})
	if err != nil {
		t.Fatal(err)
	}
	report.Finalize()

	assertFinding(t, report, "perf.soql.loop")
	assertFinding(t, report, "perf.dml.loop")
	assertFinding(t, report, "perf.describe.repeated")
	assertFinding(t, report, "perf.async.loop")
	assertFinding(t, report, "perf.async.future")
	assertFinding(t, report, "perf.async.future.callout-dml")
	assertFinding(t, report, "perf.async.queueable.cycle")
	assertFinding(t, report, "perf.async.queueable.self-continuation")
	assertFinding(t, report, "perf.async.queueable.chain-depth")
	assertFinding(t, report, "perf.async.batch.execute")
	assertFinding(t, report, "perf.async.batch.finish")
	assertFinding(t, report, "perf.async.batch.execute-queueable")
	assertFinding(t, report, "perf.async.schedule.recursive")
	assertFindingSeverity(t, report, "perf.soql.selectivity", SeverityMedium)
	assertFinding(t, report, "perf.soql.overfetch")
	assertFinding(t, report, "perf.soql.subquery-no-limit")
	assertFinding(t, report, "perf.soql.where-formula")
	assertFinding(t, report, "perf.soql.orderby-no-index")
	assertFinding(t, report, "perf.soql.sobject-list-bind")
	assertFinding(t, report, "perf.soql.null-bind")
	assertFinding(t, report, "perf.debug.serialize")
	assertEntryPoint(t, report, EntryTrigger)
	assertEntryPoint(t, report, EntryBatch)

	assertNoFindingAtLine(t, report, "perf.soql.loop", 28)
	assertNoFindingAtLine(t, report, "perf.dml.loop", 38)
	assertNoFindingAtLine(t, report, "perf.dml.loop", 40)
	assertNoFindingAtLine(t, report, "perf.async.loop", 37)
	assertNoFindingAtLine(t, report, "perf.async.queueable.cycle", 194)
	assertNoFindingAtLine(t, report, "perf.async.queueable.cycle", 204)
	assertNoFindingAtLine(t, report, "perf.async.queueable.self-continuation", 204)
	assertNoFindingAtLine(t, report, "perf.describe.repeated", 210)
	assertNoFindingAtLine(t, report, "perf.describe.repeated", 214)
	assertNoFindingInPath(t, report, "perf.describe.repeated", "/fflib/")
	assertNoFinding(t, report, "perf.entry.trigger")
	assertNoFinding(t, report, "perf.ui.auraenabled.uncached")
	assertNoFinding(t, report, "perf.async.batch.unfiltered-start")
	assertNoFinding(t, report, "perf.soql.unfiltered")
}

func TestAnalyzeProjectFindsQueryBindAndDebugSerializationRisks(t *testing.T) {
	root := testPerfProject(t, map[string]string{
		"force-app/main/default/classes/QueryShapeRisk.cls": `
public class QueryShapeRisk {
  public static List<Account> byRecords(List<SObject> records) {
    return [SELECT Id FROM Account WHERE Id IN :records];
  }

  public static List<Contact> byMaybeAccount(Id maybeAccountId) {
    return [SELECT Id FROM Contact WHERE AccountId = :maybeAccountId];
  }

  public static List<Contact> byProofedAccount(Id accountId) {
    if (accountId == null) {
      return new List<Contact>();
    }
    return [SELECT Id FROM Contact WHERE AccountId = :accountId];
  }

  public static List<Account> byIds(List<Id> ids) {
    return [SELECT Id FROM Account WHERE Id IN :ids];
  }

  public static void debugRows(List<Account> records) {
    System.debug(JSON.serialize(records));
  }
}`,
	})

	report := analyzeTestProject(t, root, Options{})

	sobjectBind := requireFinding(t, report, "perf.soql.sobject-list-bind")
	if sobjectBind.Location.Line != 4 {
		t.Fatalf("sobject bind location = %#v", sobjectBind.Location)
	}
	nullBind := requireFinding(t, report, "perf.soql.null-bind")
	if nullBind.Location.Line != 8 {
		t.Fatalf("null bind location = %#v", nullBind.Location)
	}
	debugSerialize := requireFinding(t, report, "perf.debug.serialize")
	if debugSerialize.Location.Line != 23 {
		t.Fatalf("debug serialize location = %#v", debugSerialize.Location)
	}
	assertNoFindingAtLine(t, report, "perf.soql.null-bind", 15)
	assertNoFindingAtLine(t, report, "perf.soql.sobject-list-bind", 19)
}

func assertFinding(t *testing.T, report Report, id string) {
	t.Helper()
	for _, finding := range report.Findings {
		if finding.ID == id {
			return
		}
	}
	t.Fatalf("missing finding %s in %#v", id, report.Findings)
}

func assertFindingSeverity(t *testing.T, report Report, id string, severity Severity) {
	t.Helper()
	for _, finding := range report.Findings {
		if finding.ID == id {
			if finding.Severity != severity {
				t.Fatalf("finding %s severity = %s, want %s", id, finding.Severity, severity)
			}
			return
		}
	}
	t.Fatalf("missing finding %s in %#v", id, report.Findings)
}

func assertNoFindingAtLine(t *testing.T, report Report, id string, line int) {
	t.Helper()
	for _, finding := range report.Findings {
		if finding.ID != id {
			continue
		}
		if finding.Location.Line == line {
			t.Fatalf("unexpected finding %s at line %d", id, line)
		}
	}
}

func assertNoFindingInPath(t *testing.T, report Report, id, pathFragment string) {
	t.Helper()
	for _, finding := range report.Findings {
		if finding.ID != id {
			continue
		}
		if strings.Contains(filepath.ToSlash(finding.Location.File), pathFragment) {
			t.Fatalf("unexpected finding %s in %s", id, finding.Location.File)
		}
	}
}

func assertNoFinding(t *testing.T, report Report, id string) {
	t.Helper()
	for _, finding := range report.Findings {
		if finding.ID == id {
			t.Fatalf("unexpected finding %s in %#v", id, report.Findings)
		}
	}
}
