package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

var messagingCloseoutOwners = map[string]string{
	"apex:Messaging.AttachmentRetrievalOption":                           "core-runtime-messaging-attachment-retrieval-option-enum",
	"apex:Messaging.EmailToSalesforceHandler":                            "core-runtime-messaging-email-to-salesforce-handler-evidence",
	"apex:Messaging.EmailToSalesforceHandler.EmailToSalesforceHandler()": "core-runtime-messaging-email-to-salesforce-handler-evidence",
	"apex:Messaging.EmailToSalesforceHandler.clone()":                    "core-runtime-messaging-email-to-salesforce-handler-evidence",
	"apex:Messaging.InboundEmail.Header":                                 "core-runtime-messaging-inbound-email-dto-evidence",
	"apex:Messaging.InboundEmail.TextAttachment":                         "core-runtime-messaging-inbound-email-dto-evidence",
}

func TestMessagingCloseoutHasExactExecutableOwnership(t *testing.T) {
	root := filepath.Join("..", "..")
	paths, err := filepath.Glob(filepath.Join(root, "docs", "fixtures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := BuildEvidenceSnapshot(paths)
	if err != nil {
		t.Fatal(err)
	}
	var selected []SurfaceLedgerRow
	for _, row := range evidence {
		if _, ok := messagingCloseoutOwners[row.SurfaceID]; ok {
			selected = append(selected, row)
		}
	}
	wantIDs := make([]string, 0, len(messagingCloseoutOwners))
	for id := range messagingCloseoutOwners {
		wantIDs = append(wantIDs, id)
	}
	assertExactSurfaceSet(t, selected, wantIDs)
	for _, row := range selected {
		owner := "fixture:" + messagingCloseoutOwners[row.SurfaceID]
		if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported || len(row.Sources) != 1 || row.Sources[0] != owner {
			t.Fatalf("%s evidence/behavior/source = %s/%s/%v", row.SurfaceID, row.Evidence, row.GladeBehavior, row.Sources)
		}
	}

	fixtures := map[string][]string{
		"core-runtime-messaging-attachment-retrieval-option-enum": {
			"Messaging.AttachmentRetrievalOption metadataOnly =",
		},
		"core-runtime-messaging-email-to-salesforce-handler-evidence": {
			"Messaging.EmailToSalesforceHandler handler = new Messaging.EmailToSalesforceHandler();",
			"Messaging.EmailToSalesforceHandler cloned = (Messaging.EmailToSalesforceHandler)handler.clone();",
			"System.assertNotEquals(handler, cloned);",
		},
		"core-runtime-messaging-inbound-email-dto-evidence": {
			"Messaging.InboundEmail.Header header = new Messaging.InboundEmail.Header();",
			"Messaging.InboundEmail.TextAttachment text = new Messaging.InboundEmail.TextAttachment();",
		},
	}
	for name, witnesses := range fixtures {
		path := filepath.Join(root, "docs", "fixtures", name+".json")
		fixture, err := compat.LoadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := compat.Validate(fixture); err != nil {
			t.Fatal(err)
		}
		if fixture.Command.Kind != "test" || len(fixture.Source) == 0 {
			t.Fatalf("fixture %s envelope = %#v", name, fixture)
		}
		var source strings.Builder
		for _, file := range fixture.Source {
			source.WriteString(file.Content)
		}
		for _, witness := range witnesses {
			if !strings.Contains(source.String(), witness) {
				t.Fatalf("fixture %s source missing %q", name, witness)
			}
		}
		data, err := os.ReadFile(path)
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
		if policy.SalesforceEligible == nil || *policy.SalesforceEligible || policy.SalesforceExclusionClass != "policy-local-only" || !strings.Contains(strings.ToLower(policy.SalesforceExclusionReason), "no salesforce parity") {
			t.Fatalf("fixture %s local-only policy = %#v", name, policy)
		}
		if result, err := compat.Run(fixture); err != nil || !result.OK {
			t.Fatalf("fixture %s execution = %#v, error = %v", name, result, err)
		}
	}
}
