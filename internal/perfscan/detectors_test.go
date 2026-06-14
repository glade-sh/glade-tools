package perfscan

import "testing"

func TestDetectorFindsHeavyStaticFirstTouch(t *testing.T) {
	root := testPerfProject(t, map[string]string{
		"force-app/main/default/classes/Constants.cls": `
public class Constants {
  public static final String LABEL = 'A';
  public static Map<String, Schema.SObjectType> TOKENS = Schema.getGlobalDescribe();
}`,
		"force-app/main/default/classes/Controller.cls": `
public class Controller {
  @AuraEnabled(cacheable=true)
  public static String label() {
    return Constants.LABEL;
  }
}`,
	})
	report := analyzeTestProject(t, root, Options{})

	finding := requireFinding(t, report, "perf.static.first-touch")
	if finding.EntryPoint.Kind != EntryLWC && finding.EntryPoint.Kind != EntryAura {
		t.Fatalf("entry point = %#v", finding.EntryPoint)
	}
	if finding.Multiplicity != "once-per-transaction" {
		t.Fatalf("multiplicity = %q", finding.Multiplicity)
	}
	if !finding.ResourceRisk.CPU || !finding.ResourceRisk.Heap || !finding.ResourceRisk.SharedLimit {
		t.Fatalf("resource risk = %#v", finding.ResourceRisk)
	}
	requireEvidence(t, finding, "static", "first touch")
}
