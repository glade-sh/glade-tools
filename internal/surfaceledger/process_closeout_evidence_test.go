package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func processCloseoutIDs() []string {
	return []string{
		"apex:Process.PluginDescribeResult",
		"apex:Process.PluginDescribeResult.clone()",
		"apex:Process.PluginDescribeResult.description",
		"apex:Process.PluginDescribeResult.inputParameters",
		"apex:Process.PluginDescribeResult.name",
		"apex:Process.PluginDescribeResult.outputParameters",
		"apex:Process.PluginDescribeResult.tag",
		"apex:Process.PluginRequest",
		"apex:Process.PluginRequest.PluginRequest(Map<String,Object>)",
		"apex:Process.PluginRequest.clone()",
		"apex:Process.PluginRequest.inputParameters",
		"apex:Process.PluginResult",
		"apex:Process.PluginResult.PluginResult(Map<String,Object>)",
		"apex:Process.PluginResult.PluginResult(String,Object)",
		"apex:Process.PluginResult.clone()",
		"apex:Process.PluginResult.outputParameters",
		"apex:Process.SparkPlugApi",
		"apex:Process.SparkPlugApi.SparkPlugApi()",
		"apex:Process.SparkPlugApi.clone()",
	}
}

func TestProcessCloseoutHasExactExecutableOwnership(t *testing.T) {
	root := filepath.Join("..", "..")
	wantIDs := processCloseoutIDs()
	paths, err := filepath.Glob(filepath.Join(root, "docs", "fixtures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := BuildEvidenceSnapshot(paths)
	if err != nil {
		t.Fatal(err)
	}
	want := make(map[string]struct{}, len(wantIDs))
	for _, id := range wantIDs {
		want[id] = struct{}{}
	}
	var selected []SurfaceLedgerRow
	for _, row := range evidence {
		if _, ok := want[row.SurfaceID]; ok {
			selected = append(selected, row)
		}
	}
	assertExactSurfaceSet(t, selected, wantIDs)
	for _, row := range selected {
		if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported || len(row.Sources) != 1 || row.Sources[0] != "fixture:current-base-process-001-api67" {
			t.Fatalf("%s evidence/behavior/source = %s/%s/%v", row.SurfaceID, row.Evidence, row.GladeBehavior, row.Sources)
		}
	}

	fixturePath := filepath.Join(root, "docs", "fixtures", "current-base-process-001-api67.json")
	fixture, err := compat.LoadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	if result, err := compat.Run(fixture); err != nil || !result.OK {
		t.Fatalf("fixture execution = %#v, error = %v", result, err)
	}
	if len(fixture.Source) != 1 {
		t.Fatalf("fixture sources = %d, want 1", len(fixture.Source))
	}
	source := fixture.Source[0].Content
	for _, witness := range []string{
		"Process.PluginDescribeResult describe = new Process.PluginDescribeResult();",
		"Process.PluginDescribeResult describeClone = (Process.PluginDescribeResult)describe.clone();",
		"Process.PluginRequest request = new Process.PluginRequest(requestValues);",
		"Process.PluginRequest requestClone = (Process.PluginRequest)request.clone();",
		"Process.PluginResult mapResult = new Process.PluginResult(resultValues);",
		"Process.PluginResult pairResult = new Process.PluginResult('pair', 'value');",
		"Process.PluginResult resultClone = (Process.PluginResult)mapResult.clone();",
		"System.assertEquals(0, pairResult.outputParameters.size());",
		"Process.SparkPlugApi sparkPlugApi = new Process.SparkPlugApi();",
		"Process.SparkPlugApi sparkPlugApiClone = (Process.SparkPlugApi)sparkPlugApi.clone();",
	} {
		if !strings.Contains(source, witness) {
			t.Fatalf("Process source missing %q", witness)
		}
	}

	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	var policy struct {
		SalesforceEligible        *bool  `json:"salesforceEligible"`
		SalesforceExclusionClass  string `json:"salesforceExclusionClass"`
		SalesforceExclusionReason string `json:"salesforceExclusionReason"`
	}
	if err := json.Unmarshal(data, &policy); err != nil {
		t.Fatal(err)
	}
	if policy.SalesforceEligible == nil || *policy.SalesforceEligible || policy.SalesforceExclusionClass != "policy-local-only" || !strings.Contains(policy.SalesforceExclusionReason, "no hosted Salesforce runtime claim") {
		t.Fatalf("fixture policy = %#v", policy)
	}
}
