package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

var messagingResultNotificationDepthIDs = []string{
	"apex:Messaging.ActionError",
	"apex:Messaging.ActionResult",
	"apex:Messaging.ActionResult.Builder",
	"apex:Messaging.ActionResult.Builder.Builder()",
	"apex:Messaging.ActionResult.Builder.clone()",
	"apex:Messaging.ActionResult.clone()",
	"apex:Messaging.ActionableNotification",
	"apex:Messaging.ActionableNotification.Builder",
	"apex:Messaging.ActionableNotification.Builder.Builder()",
	"apex:Messaging.ActionableNotification.Builder.clone()",
	"apex:Messaging.ActionableNotification.clone()",
	"apex:Messaging.CustomNotification.CustomNotification()",
	"apex:Messaging.CustomNotification.CustomNotification(String,String,String,String,String,String)",
	"apex:Messaging.CustomNotification.clone()",
	"apex:Messaging.PushNotification.PushNotification()",
	"apex:Messaging.PushNotification.PushNotification(Map<String,Object>)",
	"apex:Messaging.PushNotification.clone()",
	"apex:Messaging.PushNotificationPayload",
	"apex:Messaging.PushNotificationPayload.PushNotificationPayload()",
	"apex:Messaging.PushNotificationPayload.clone()",
	"apex:Messaging.SendEmailError.message",
	"apex:Messaging.SendEmailError.statuscode",
	"apex:Messaging.SendEmailError.targetobjectid",
	"apex:Messaging.SendEmailResult.errors",
	"apex:Messaging.SendEmailResult.success",
}

func TestMessagingResultNotificationDepthHasExactExecutableOwnership(t *testing.T) {
	root := filepath.Join("..", "..")
	fixtureName := "core-runtime-messaging-result-notification-dtos-evidence"
	fixturePath := filepath.Join(root, "docs", "fixtures", fixtureName+".json")
	fixture, err := compat.LoadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	if result, err := compat.Run(fixture); err != nil || !result.OK {
		t.Fatalf("fixture = %#v, error = %v", result, err)
	}
	want := make(map[string]struct{}, len(messagingResultNotificationDepthIDs))
	for _, id := range messagingResultNotificationDepthIDs {
		want[id] = struct{}{}
	}
	evidence, err := BuildEvidenceSnapshot([]string{fixturePath})
	if err != nil {
		t.Fatal(err)
	}
	selected := make([]SurfaceLedgerRow, 0, len(want))
	for _, row := range evidence {
		if _, ok := want[row.SurfaceID]; ok {
			selected = append(selected, row)
		}
	}
	assertExactSurfaceSet(t, selected, messagingResultNotificationDepthIDs)
	if len(selected) != 25 {
		t.Fatalf("selected rows = %d, want 25", len(selected))
	}
	for _, row := range selected {
		if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported || len(row.Sources) != 1 || row.Sources[0] != "fixture:"+fixtureName {
			t.Fatalf("%s evidence/behavior/source = %s/%s/%v", row.SurfaceID, row.Evidence, row.GladeBehavior, row.Sources)
		}
	}
	fixtureData, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	var policy struct {
		SalesforceEligible        *bool  `json:"salesforceEligible"`
		SalesforceExclusionClass  string `json:"salesforceExclusionClass"`
		SalesforceExclusionReason string `json:"salesforceExclusionReason"`
	}
	if err := json.Unmarshal(fixtureData, &policy); err != nil {
		t.Fatal(err)
	}
	if policy.SalesforceEligible == nil || *policy.SalesforceEligible || policy.SalesforceExclusionClass != "policy-local-only" || !strings.Contains(strings.ToLower(policy.SalesforceExclusionReason), "zero salesforce parity") {
		t.Fatalf("local-only policy = eligible:%v class:%q reason:%q", policy.SalesforceEligible, policy.SalesforceExclusionClass, policy.SalesforceExclusionReason)
	}

	var source strings.Builder
	for _, file := range fixture.Source {
		source.WriteString(file.Content)
	}
	for _, witness := range []string{
		"Messaging.SendEmailResult result = results[0];", "result.success", "result.errors", "err.message", "err.statusCode", "err.targetObjectId",
		"Messaging.ActionError actionError = Messaging.ActionError.INVALID_STATE;",
		"Messaging.CustomNotification custom = new Messaging.CustomNotification();", "new Messaging.CustomNotification('0ML000000000001AAA'", "Messaging.CustomNotification clonedCustom = (Messaging.CustomNotification)custom.clone();", "JSON.serialize(clonedCustom)", "clonedCustom.send(",
		"Messaging.ActionResult.Builder actionBuilder = new Messaging.ActionResult.Builder();", "Messaging.ActionResult.Builder clonedActionBuilder = (Messaging.ActionResult.Builder)actionBuilder.clone();", "Messaging.ActionResult clonedBuilderAction = clonedActionBuilder.build();", "Messaging.ActionResult clonedAction = (Messaging.ActionResult)action.clone();",
		"Messaging.ActionableNotification.Builder noteBuilder = new Messaging.ActionableNotification.Builder();", "Messaging.ActionableNotification.Builder clonedNoteBuilder = (Messaging.ActionableNotification.Builder)noteBuilder.clone();", "Messaging.ActionableNotification clonedBuilderNote = clonedNoteBuilder.build();", "Messaging.ActionableNotification clonedNote = (Messaging.ActionableNotification)note.clone();",
		"Messaging.PushNotification emptyPush = new Messaging.PushNotification();", "Messaging.PushNotification push = new Messaging.PushNotification(shortPayload);", "push.setTtl(60);", "Messaging.PushNotification clonedPush = (Messaging.PushNotification)push.clone();", "JSON.serialize(clonedPush)",
		"Messaging.PushNotificationPayload payloadDTO = new Messaging.PushNotificationPayload();", "Messaging.PushNotificationPayload clonedPayloadDTO = (Messaging.PushNotificationPayload)payloadDTO.clone();",
	} {
		if !strings.Contains(source.String(), witness) {
			t.Fatalf("fixture source missing %q", witness)
		}
	}

	paths, err := filepath.Glob(filepath.Join(root, "docs", "fixtures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	owners := make(map[string]int, len(want))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var header struct {
			EvidenceOnly bool `json:"evidenceOnly"`
			Evidence     []struct {
				SurfaceID string `json:"surfaceId"`
			} `json:"evidence"`
		}
		if err := json.Unmarshal(data, &header); err != nil {
			t.Fatal(err)
		}
		if header.EvidenceOnly {
			continue
		}
		for _, item := range header.Evidence {
			if _, ok := want[item.SurfaceID]; ok {
				owners[item.SurfaceID]++
			}
		}
	}
	for id := range want {
		if owners[id] != 1 {
			t.Fatalf("fixture ownership for %s = %d, want exactly one", id, owners[id])
		}
	}
}
