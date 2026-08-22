package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

var messagingEmailDepthOwners = map[string][]string{
	"core-runtime-messaging-email-base-accessors-evidence": {
		"apex:Messaging.Email.equals(Object)",
		"apex:Messaging.Email.hashCode()",
		"apex:Messaging.Email.subject",
		"apex:Messaging.Email.toString()",
	},
	"core-runtime-messaging-single-email-accessors-broad-evidence": {
		"apex:Messaging.SingleEmailMessage.bccaddresses",
		"apex:Messaging.SingleEmailMessage.ccaddresses",
		"apex:Messaging.SingleEmailMessage.charset",
		"apex:Messaging.SingleEmailMessage.equals(Object)",
		"apex:Messaging.SingleEmailMessage.hashCode()",
		"apex:Messaging.SingleEmailMessage.htmlbody",
		"apex:Messaging.SingleEmailMessage.inreplyto",
		"apex:Messaging.SingleEmailMessage.oneclickpost",
		"apex:Messaging.SingleEmailMessage.optoutpolicy",
		"apex:Messaging.SingleEmailMessage.orgwideemailaddressid",
		"apex:Messaging.SingleEmailMessage.plaintextbody",
		"apex:Messaging.SingleEmailMessage.references",
		"apex:Messaging.SingleEmailMessage.targetobjectid",
		"apex:Messaging.SingleEmailMessage.templateid",
		"apex:Messaging.SingleEmailMessage.toString()",
		"apex:Messaging.SingleEmailMessage.toaddresses",
		"apex:Messaging.SingleEmailMessage.treatbodiesastemplate",
		"apex:Messaging.SingleEmailMessage.treattargetobjectasrecipient",
		"apex:Messaging.SingleEmailMessage.unsubscribecomment",
		"apex:Messaging.SingleEmailMessage.unsubscribeurls",
		"apex:Messaging.SingleEmailMessage.whatid",
	},
	"core-runtime-messaging-mass-email-accessors-evidence": {
		"apex:Messaging.MassEmailMessage.MassEmailMessage()",
		"apex:Messaging.MassEmailMessage.description",
		"apex:Messaging.MassEmailMessage.equals(Object)",
		"apex:Messaging.MassEmailMessage.hashCode()",
		"apex:Messaging.MassEmailMessage.targetobjectids",
		"apex:Messaging.MassEmailMessage.templateid",
		"apex:Messaging.MassEmailMessage.toString()",
		"apex:Messaging.MassEmailMessage.whatids",
	},
}

func TestMessagingEmailDepthHasExactExecutableOwnership(t *testing.T) {
	root := filepath.Join("..", "..")
	want := make(map[string]string)
	var selected []SurfaceLedgerRow
	for fixtureName, ids := range messagingEmailDepthOwners {
		fixturePath := filepath.Join(root, "docs", "fixtures", fixtureName+".json")
		fixture, err := compat.LoadFile(fixturePath)
		if err != nil {
			t.Fatal(err)
		}
		if err := compat.Validate(fixture); err != nil {
			t.Fatal(err)
		}
		if result, err := compat.Run(fixture); err != nil || !result.OK {
			t.Fatalf("fixture %s = %#v, error = %v", fixtureName, result, err)
		}
		for _, id := range ids {
			want[id] = fixtureName
		}
		evidence, err := BuildEvidenceSnapshot([]string{fixturePath})
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range evidence {
			if want[row.SurfaceID] == fixtureName {
				selected = append(selected, row)
			}
		}
	}
	wantIDs := make([]string, 0, len(want))
	for id := range want {
		wantIDs = append(wantIDs, id)
	}
	assertExactSurfaceSet(t, selected, wantIDs)
	if len(wantIDs) != 33 {
		t.Fatalf("selected rows = %d, want 33", len(wantIDs))
	}
	for _, row := range selected {
		if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported || len(row.Sources) != 1 || row.Sources[0] != "fixture:"+want[row.SurfaceID] {
			t.Fatalf("%s evidence/behavior/source = %s/%s/%v", row.SurfaceID, row.Evidence, row.GladeBehavior, row.Sources)
		}
	}

	witnesses := map[string][]string{
		"core-runtime-messaging-email-base-accessors-evidence": {
			"email.bccSender = true;", "email.emailPriority = 'High';", "email.replyTo = 'reply@example.test';", "email.saveAsActivity = false;", "email.senderDisplayName = 'Trail Sender';", "email.subject = 'Trail Subject';", "email.useSignature = false;",
			"email.equals(email)", "email.equals(otherEmail)", "email.equals(null)", "email.hashCode()", "email.toString()",
		},
		"core-runtime-messaging-single-email-accessors-broad-evidence": {
			"msg.bccAddresses =", "msg.ccAddresses =", "msg.charset =", "msg.entityAttachments =", "msg.fileAttachments =", "msg.htmlBody =", "msg.inReplyTo =", "msg.oneClickPost =", "msg.optOutPolicy =", "msg.orgWideEmailAddressId =", "msg.plainTextBody =", "msg.references =", "msg.targetObjectId =", "msg.templateId =", "msg.toAddresses =", "msg.treatBodiesAsTemplate =", "msg.treatTargetObjectAsRecipient =", "msg.unsubscribeComment =", "msg.unsubscribeUrls =", "msg.whatId =",
			"msg.equals(msg)", "msg.equals(otherMessage)", "msg.equals(null)", "msg.hashCode()", "msg.toString()",
		},
		"core-runtime-messaging-mass-email-accessors-evidence": {
			"new Messaging.MassEmailMessage()", "mass.description =", "mass.targetObjectIds =", "mass.templateId =", "mass.whatIds =",
			"mass.equals(mass)", "mass.equals(otherMass)", "mass.equals(null)", "mass.hashCode()", "mass.toString()",
		},
	}
	for fixtureName, required := range witnesses {
		fixture, err := compat.LoadFile(filepath.Join(root, "docs", "fixtures", fixtureName+".json"))
		if err != nil {
			t.Fatal(err)
		}
		var source strings.Builder
		for _, file := range fixture.Source {
			source.WriteString(file.Content)
		}
		for _, witness := range required {
			if !strings.Contains(source.String(), witness) {
				t.Fatalf("fixture %s source missing %q", fixtureName, witness)
			}
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
