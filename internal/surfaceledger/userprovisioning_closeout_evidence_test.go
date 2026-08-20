package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func userProvisioningCloseoutIDs() []string {
	ids := []string{
		"apex:UserProvisioning.ProvisioningProcessHandlerInput",
		"apex:UserProvisioning.ProvisioningProcessHandlerInput.ProvisioningProcessHandlerInput(String,String,String,String,String)",
		"apex:UserProvisioning.ProvisioningProcessHandlerInput.clone()",
		"apex:UserProvisioning.ProvisioningProcessHandlerOutput",
		"apex:UserProvisioning.ProvisioningProcessHandlerOutput.ProvisioningProcessHandlerOutput()",
		"apex:UserProvisioning.ProvisioningProcessHandlerOutput.ProvisioningProcessHandlerOutput(String,String,String,String,String,String,String,String,String)",
		"apex:UserProvisioning.ProvisioningProcessHandlerOutput.clone()",
		"apex:UserProvisioning.UserProvisioningLog.UserProvisioningLog()",
	}
	inputFields := []string{"NamedCredDevName", "ReconFilter", "ReconOffset", "UserId", "UserProvisioningRequestId"}
	for _, field := range inputFields {
		ids = append(ids,
			"apex:UserProvisioning.ProvisioningProcessHandlerInput.get"+field+"()",
			"apex:UserProvisioning.ProvisioningProcessHandlerInput.set"+field+"(String)",
		)
	}
	for _, field := range []string{"namedCredDevName", "reconFilter", "reconOffset", "userId", "userProvisioningRequestId"} {
		ids = append(ids, "apex:UserProvisioning.ProvisioningProcessHandlerInput."+field)
	}
	outputFields := []string{"Details", "ExternalEmail", "ExternalFirstName", "ExternalLastName", "ExternalUserId", "ExternalUsername", "NextReconOffset", "ReconState", "Status", "UPAStatus"}
	for _, field := range outputFields {
		ids = append(ids,
			"apex:UserProvisioning.ProvisioningProcessHandlerOutput.get"+field+"()",
			"apex:UserProvisioning.ProvisioningProcessHandlerOutput.set"+field+"(String)",
		)
	}
	for _, field := range []string{"details", "externalEmail", "externalFirstName", "externalLastName", "externalUserId", "externalUsername", "nextReconOffset", "reconState", "status", "UPAStatus"} {
		ids = append(ids, "apex:UserProvisioning.ProvisioningProcessHandlerOutput."+field)
	}
	return ids
}

func TestUserProvisioningCloseoutHasExactExecutableOwnership(t *testing.T) {
	root := filepath.Join("..", "..")
	want := userProvisioningCloseoutIDs()
	if len(want) != 53 {
		t.Fatalf("UserProvisioning closeout IDs = %d, want 53", len(want))
	}
	paths, err := filepath.Glob(filepath.Join(root, "docs", "fixtures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := BuildEvidenceSnapshot(paths)
	if err != nil {
		t.Fatal(err)
	}
	wantSet := make(map[string]bool, len(want))
	for _, id := range want {
		wantSet[id] = true
	}
	var selected []SurfaceLedgerRow
	for _, row := range evidence {
		if wantSet[row.SurfaceID] {
			selected = append(selected, row)
		}
	}
	assertExactSurfaceSet(t, selected, want)
	for _, row := range selected {
		if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported || len(row.Sources) != 1 || row.Sources[0] != "fixture:core-runtime-userprovisioning-tail-evidence" {
			t.Fatalf("%s evidence/behavior/source = %s/%s/%v", row.SurfaceID, row.Evidence, row.GladeBehavior, row.Sources)
		}
	}

	fixturePath := filepath.Join(root, "docs", "fixtures", "core-runtime-userprovisioning-tail-evidence.json")
	fixture, err := compat.LoadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Command.Kind != "exec" || len(fixture.Command.Args) != 1 || len(fixture.Source) != 1 || fixture.Source[0].Content != fixture.Command.Args[0] {
		t.Fatalf("fixture source/command envelope = %#v", fixture)
	}
	if result, err := compat.Run(fixture); err != nil || !result.OK {
		t.Fatalf("fixture execution = %#v, error = %v", result, err)
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
	reason := strings.ToLower(policy.SalesforceExclusionReason)
	if policy.SalesforceEligible == nil || *policy.SalesforceEligible || policy.SalesforceExclusionClass != "policy-local-only" || !strings.Contains(reason, "not a salesforce oracle") || !strings.Contains(reason, "no hosted") {
		t.Fatalf("fixture local-only policy = %#v", policy)
	}
	source := fixture.Source[0].Content
	for _, field := range []string{"NamedCredDevName", "ReconFilter", "ReconOffset", "UserId", "UserProvisioningRequestId"} {
		for _, witness := range []string{"input.set" + field + "(", "input.get" + field + "()"} {
			if !strings.Contains(source, witness) {
				t.Fatalf("input source missing %q", witness)
			}
		}
	}
	for _, field := range []string{"Details", "ExternalEmail", "ExternalFirstName", "ExternalLastName", "ExternalUserId", "ExternalUsername", "NextReconOffset", "ReconState", "Status", "UPAStatus"} {
		for _, witness := range []string{"output.set" + field + "(", "output.get" + field + "()"} {
			if !strings.Contains(source, witness) {
				t.Fatalf("output source missing %q", witness)
			}
		}
	}
	for _, witness := range []string{
		"new UserProvisioning.ProvisioningProcessHandlerInput('upr', '005000000000001', 'named', 'filter', 'offset')",
		"input.setUserProvisioningRequestId('upr-updated')",
		"System.assertEquals('upr-updated', input.userProvisioningRequestId)",
		"System.assertEquals('upr-updated', input.getUserProvisioningRequestId())",
		"UserProvisioning.ProvisioningProcessHandlerInput inputClone = (UserProvisioning.ProvisioningProcessHandlerInput)input.clone()",
		"System.assertEquals('upr-updated', inputClone.getUserProvisioningRequestId())",
		"new UserProvisioning.ProvisioningProcessHandlerOutput()",
		"new UserProvisioning.ProvisioningProcessHandlerOutput('Queued', 'details', 'external-id', 'external-user', 'external@example.com', 'First', 'Last', 'Pending', 'next')",
		"output.setStatus('Completed')",
		"System.assertEquals('Completed', output.status)",
		"System.assertEquals('Completed', output.getStatus())",
		"UserProvisioning.ProvisioningProcessHandlerOutput outputClone = (UserProvisioning.ProvisioningProcessHandlerOutput)output.clone()",
		"System.assertEquals('Completed', outputClone.getStatus())",
		"new UserProvisioning.UserProvisioningLog()",
	} {
		if !strings.Contains(source, witness) {
			t.Fatalf("fixture source missing %q", witness)
		}
	}
}
