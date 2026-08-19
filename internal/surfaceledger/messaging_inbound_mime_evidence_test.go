package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

const messagingExtractInboundEmailSurfaceID = "apex:System.Messaging.extractInboundEmail(Object,Boolean)"

func TestMessagingExtractInboundEmailHasExecutableLocalMIMEProof(t *testing.T) {
	root := filepath.Join("..", "..")
	fixturePath := filepath.Join(root, "docs", "fixtures", "core-runtime-messaging-inbound-mime-local.json")
	fixture, err := compat.LoadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Command.Kind != "test" {
		t.Fatalf("local fixture contract = %#v", fixture)
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
	assertExactSurfaceSet(t, evidence, []string{messagingExtractInboundEmailSurfaceID})
	if evidence[0].Evidence != EvidenceFixture || evidence[0].GladeBehavior != BehaviorSupported {
		t.Fatalf("fixture evidence = %#v, want fixture/supported", evidence[0])
	}

	policy, err := LoadSupportPolicy(filepath.Join(root, "docs", "fixtures", "apex-local-support-policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	profile := ComputeSupportProfile(evidence, policy, nil)
	if len(profile.Rows) != 1 || profile.Rows[0].Disposition != DispositionDeterministicMockRequired || profile.Rows[0].MatchRule != "namespace=Messaging" {
		t.Fatalf("support profile = %#v, want Messaging deterministic-mock-required", profile.Rows)
	}

	if result, err := compat.Run(fixture); err != nil || !result.OK {
		t.Fatalf("fixture execution = %#v, error = %v", result, err)
	}
}
