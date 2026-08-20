package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

var messagingAttachmentRetrievalOptionSurfaceIDs = []string{
	"apex:Messaging.AttachmentRetrievalOption",
	"apex:Messaging.AttachmentRetrievalOption.METADATA_ONLY",
	"apex:Messaging.AttachmentRetrievalOption.METADATA_WITH_BODY",
	"apex:Messaging.AttachmentRetrievalOption.NONE",
	"apex:Messaging.AttachmentRetrievalOption.equals(Object)",
	"apex:Messaging.AttachmentRetrievalOption.hashCode()",
	"apex:Messaging.AttachmentRetrievalOption.ordinal()",
	"apex:Messaging.AttachmentRetrievalOption.valueOf(String)",
	"apex:Messaging.AttachmentRetrievalOption.values()",
}

func TestMessagingAttachmentRetrievalOptionHasExecutableLocalEnumProof(t *testing.T) {
	root := filepath.Join("..", "..")
	fixturePath := filepath.Join(root, "docs", "fixtures", "core-runtime-messaging-attachment-retrieval-option-enum.json")
	fixture, err := compat.LoadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Command.Kind != "test" {
		t.Fatalf("local fixture contract = %#v", fixture.Command)
	}
	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	var raw struct {
		SalesforceEligible       *bool  `json:"salesforceEligible"`
		SalesforceExclusionClass string `json:"salesforceExclusionClass"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if raw.SalesforceEligible == nil || *raw.SalesforceEligible || raw.SalesforceExclusionClass != "policy-local-only" {
		t.Fatalf("local-only metadata = %#v", raw)
	}

	evidence, err := BuildEvidenceSnapshot([]string{fixturePath})
	if err != nil {
		t.Fatal(err)
	}
	assertExactSurfaceSet(t, evidence, messagingAttachmentRetrievalOptionSurfaceIDs)
	for _, row := range evidence {
		if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported {
			t.Fatalf("fixture evidence row = %#v", row)
		}
	}

	policy, err := LoadSupportPolicy(filepath.Join(root, "docs", "fixtures", "apex-local-support-policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	profile := ComputeSupportProfile(evidence, policy, nil)
	if len(profile.Rows) != len(messagingAttachmentRetrievalOptionSurfaceIDs) {
		t.Fatalf("support profile rows = %d, want %d", len(profile.Rows), len(messagingAttachmentRetrievalOptionSurfaceIDs))
	}
	for _, row := range profile.Rows {
		if row.Disposition != DispositionDeterministicMockRequired || row.MatchRule != "namespace=Messaging" {
			t.Fatalf("support profile row = %#v", row)
		}
	}

	if result, err := compat.Run(fixture); err != nil || !result.OK {
		t.Fatalf("fixture execution = %#v, error = %v", result, err)
	}
}
