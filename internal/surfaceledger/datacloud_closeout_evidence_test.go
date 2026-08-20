package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func datacloudCloseoutIDs() []string {
	return []string{
		"apex:Datacloud.FindDuplicates.clone()",
		"apex:Datacloud.FindDuplicatesByIds",
		"apex:Datacloud.FindDuplicatesByIds.clone()",
		"apex:Datacloud.FindDuplicatesResult.getDuplicateResults()",
		"apex:Datacloud.FindDuplicatesResult.getErrors()",
		"apex:Datacloud.FindDuplicatesResult.isSuccess()",
	}
}

func TestDatacloudCloseoutHasExactExecutableOwnership(t *testing.T) {
	root := filepath.Join("..", "..")
	wantIDs := datacloudCloseoutIDs()
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
		if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported || len(row.Sources) != 1 || row.Sources[0] != "fixture:current-base-datacloud-dto-deterministic-001-api67" {
			t.Fatalf("%s evidence/behavior/source = %s/%s/%v", row.SurfaceID, row.Evidence, row.GladeBehavior, row.Sources)
		}
	}

	fixturePath := filepath.Join(root, "docs", "fixtures", "current-base-datacloud-dto-deterministic-001-api67.json")
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
	for _, witness := range []string{
		"Datacloud.FindDuplicates finderClone = (Datacloud.FindDuplicates)finder.clone();",
		"Datacloud.FindDuplicatesByIds finderByIds = new Datacloud.FindDuplicatesByIds();",
		"Datacloud.FindDuplicatesByIds finderByIdsClone = (Datacloud.FindDuplicatesByIds)finderByIds.clone();",
		"System.assertEquals(0, fdr.getDuplicateResults().size());",
		"System.assertEquals(0, fdr.getErrors().size());",
		"System.assertEquals(true, fdr.isSuccess());",
	} {
		if !strings.Contains(fixture.Source[0].Content, witness) {
			t.Fatalf("Datacloud source missing %q", witness)
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
	if policy.SalesforceEligible == nil || *policy.SalesforceEligible || policy.SalesforceExclusionClass != "policy-local-only" || !strings.Contains(policy.SalesforceExclusionReason, "without claiming Salesforce availability") {
		t.Fatalf("fixture policy = %#v", policy)
	}
}
