package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func TestDatabaseLeadConvertDTOHasExactLocalOnlyEvidence(t *testing.T) {
	wantIDs := []string{
		"apex:Database.LeadConvert.accountrecord",
		"apex:Database.LeadConvert.bypassaccountdedupecheck",
		"apex:Database.LeadConvert.bypasscontactdedupecheck",
		"apex:Database.LeadConvert.contactrecord",
		"apex:Database.LeadConvert.convertedstatus",
		"apex:Database.LeadConvert.donotcreateopportunity",
		"apex:Database.LeadConvert.equals(Object)",
		"apex:Database.LeadConvert.hashCode()",
		"apex:Database.LeadConvert.opportunityrecord",
		"apex:Database.LeadConvert.relatedpersonaccountrecord",
		"apex:Database.LeadConvert.toString()",
	}
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
		if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported || len(row.Sources) != 1 || row.Sources[0] != "fixture:core-runtime-database-leadconvert-dto-local-evidence" {
			t.Fatalf("%s evidence/behavior/source = %s/%s/%v", row.SurfaceID, row.Evidence, row.GladeBehavior, row.Sources)
		}
	}

	fixturePath := filepath.Join(root, "docs", "fixtures", "core-runtime-database-leadconvert-dto-local-evidence.json")
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
	if fixture.Command.Kind != "exec" || len(fixture.Command.Args) != 1 || len(fixture.Source) != 1 || fixture.Source[0].Content != fixture.Command.Args[0] {
		t.Fatalf("fixture source/command envelope does not identify one executable program")
	}
	for _, witness := range []string{
		"convert.accountrecord", "convert.bypassaccountdedupecheck", "convert.bypasscontactdedupecheck",
		"convert.contactrecord", "convert.convertedstatus", "convert.donotcreateopportunity",
		"convert.opportunityrecord", "convert.relatedpersonaccountrecord",
		"convert.equals(convert)", "!convert.equals(null)", "convert.hashCode()", "convert.toString()",
	} {
		if !strings.Contains(fixture.Source[0].Content, witness) {
			t.Fatalf("LeadConvert source missing %q", witness)
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
	if policy.SalesforceEligible == nil || *policy.SalesforceEligible || policy.SalesforceExclusionClass != "policy-local-only" || !strings.Contains(policy.SalesforceExclusionReason, "zero Salesforce parity") {
		t.Fatalf("fixture policy = %#v", policy)
	}
}
