package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func TestSystemTriggerHasExactExecutableLocalEvidence(t *testing.T) {
	wantIDs := []string{
		"apex:System.Trigger",
		"apex:System.Trigger.isAfter",
		"apex:System.Trigger.isBefore",
		"apex:System.Trigger.isDelete",
		"apex:System.Trigger.isExecuting",
		"apex:System.Trigger.isInsert",
		"apex:System.Trigger.isUndelete",
		"apex:System.Trigger.isUpdate",
		"apex:System.Trigger.newMap",
		"apex:System.Trigger.old",
		"apex:System.Trigger.oldMap",
		"apex:System.Trigger.operationType",
		"apex:System.Trigger.size",
	}
	const owner = "fixture:data-fidelity-soql-dml"
	root := filepath.Join("..", "..")
	paths, err := filepath.Glob(filepath.Join(root, "docs", "fixtures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := BuildEvidenceSnapshot(paths)
	if err != nil {
		t.Fatal(err)
	}
	want := make(map[string]struct{}, len(wantIDs))
	for _, id := range wantIDs {
		want[id] = struct{}{}
	}
	var selected []SurfaceLedgerRow
	for _, row := range rows {
		if _, ok := want[row.SurfaceID]; ok {
			selected = append(selected, row)
		}
	}
	assertExactSurfaceSet(t, selected, wantIDs)
	for _, row := range selected {
		if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported || len(row.Sources) != 1 || row.Sources[0] != owner {
			t.Fatalf("%s evidence/behavior/source = %s/%s/%v", row.SurfaceID, row.Evidence, row.GladeBehavior, row.Sources)
		}
	}

	fixturePath := filepath.Join(root, "docs", "fixtures", "data-fidelity-soql-dml.json")
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
	if fixture.Command.Kind != "test" {
		t.Fatalf("fixture command = %q, want test", fixture.Command.Kind)
	}
	var source strings.Builder
	for _, file := range fixture.Source {
		source.WriteString(file.Content)
		source.WriteByte('\n')
	}
	sourceText := source.String()
	for _, witness := range []string{
		"Trigger.isAfter", "Trigger.isBefore", "Trigger.isDelete", "Trigger.isExecuting",
		"Trigger.isInsert", "Trigger.isUndelete", "Trigger.isUpdate", "Trigger.newMap",
		"Trigger.old", "Trigger.oldMap", "Trigger.size",
		"TriggerOperation.BEFORE_UPDATE, Trigger.operationType",
		"TriggerOperation.AFTER_UNDELETE, Trigger.operationType",
	} {
		if !strings.Contains(sourceText, witness) {
			t.Fatalf("trigger source missing %q", witness)
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
	if policy.SalesforceEligible == nil || *policy.SalesforceEligible || policy.SalesforceExclusionClass != "policy-local-only" || !strings.Contains(policy.SalesforceExclusionReason, "hosted parity") {
		t.Fatalf("fixture policy = %#v", policy)
	}
}
