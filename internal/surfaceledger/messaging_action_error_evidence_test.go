package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

var messagingActionErrorEvidenceIDs = []string{
	"apex:Messaging.ActionError.ACCESS_DENIED",
	"apex:Messaging.ActionError.ACTION_NOT_IMPLEMENTED",
	"apex:Messaging.ActionError.INTERNAL_ERROR",
	"apex:Messaging.ActionError.INVALID_ACTION_PARAMETERS",
	"apex:Messaging.ActionError.INVALID_STATE",
	"apex:Messaging.ActionError.equals(Object)",
	"apex:Messaging.ActionError.hashCode()",
	"apex:Messaging.ActionError.ordinal()",
	"apex:Messaging.ActionError.valueOf(String)",
	"apex:Messaging.ActionError.values()",
}

func TestMessagingActionErrorEvidenceFixture(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "fixtures", "core-runtime-messaging-action-error-local.json")
	fixture, err := compat.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if fixture.Name != "core-runtime-messaging-action-error-local" {
		t.Fatalf("fixture name = %q", fixture.Name)
	}
	if fixture.Command.Kind != "test" || len(fixture.Source) != 1 {
		t.Fatalf("fixture command/source = %q/%d", fixture.Command.Kind, len(fixture.Source))
	}
	var metadata struct {
		SalesforceEligible *bool  `json:"salesforceEligible"`
		ExclusionClass     string `json:"salesforceExclusionClass"`
		ExclusionReason    string `json:"salesforceExclusionReason"`
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.SalesforceEligible == nil || *metadata.SalesforceEligible || metadata.ExclusionClass != "policy-local-only" {
		t.Fatalf("Salesforce policy = %#v", metadata)
	}
	if !strings.Contains(metadata.ExclusionReason, "deterministic local enum behavior") || !strings.Contains(metadata.ExclusionReason, "zero Salesforce parity") {
		t.Fatalf("Salesforce exclusion reason = %q", metadata.ExclusionReason)
	}

	if result, err := compat.Run(fixture); err != nil || !result.OK {
		t.Fatalf("fixture execution = %#v, error = %v", result, err)
	}
	if len(fixture.Evidence) != len(messagingActionErrorEvidenceIDs) {
		t.Fatalf("fixture evidence rows = %d, want %d", len(fixture.Evidence), len(messagingActionErrorEvidenceIDs))
	}
	rows, err := BuildEvidenceSnapshot([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	assertExactSurfaceSet(t, rows, messagingActionErrorEvidenceIDs)
	for _, row := range rows {
		if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported {
			t.Fatalf("%s evidence/behavior = %s/%s, want fixture/supported", row.SurfaceID, row.Evidence, row.GladeBehavior)
		}
	}

	source := fixture.Source[0].Content
	for _, constant := range []string{
		"ACCESS_DENIED", "ACTION_NOT_IMPLEMENTED", "INTERNAL_ERROR", "INVALID_ACTION_PARAMETERS", "INVALID_STATE",
	} {
		assertSourceContains(t, source, "Messaging.ActionError."+constant)
	}
	for _, method := range []string{"equals", "hashCode", "ordinal", "valueOf", "values"} {
		assertSourceContains(t, source, "."+method+"(")
	}
}
