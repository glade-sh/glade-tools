package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

var messagingInboundDTODepthOwners = map[string][]string{
	"core-runtime-messaging-inbound-email-dto-evidence": {
		"apex:Messaging.InboundEmail.Header.name",
		"apex:Messaging.InboundEmail.Header.value",
		"apex:Messaging.InboundEmail.InboundEmail()",
		"apex:Messaging.InboundEmail.binaryAttachments",
		"apex:Messaging.InboundEmail.ccAddresses",
		"apex:Messaging.InboundEmail.clone()",
		"apex:Messaging.InboundEmail.fromAddress",
		"apex:Messaging.InboundEmail.fromName",
		"apex:Messaging.InboundEmail.headers",
		"apex:Messaging.InboundEmail.htmlBody",
		"apex:Messaging.InboundEmail.htmlBodyIsTruncated",
		"apex:Messaging.InboundEmail.inReplyTo",
		"apex:Messaging.InboundEmail.messageId",
		"apex:Messaging.InboundEmail.plainTextBody",
		"apex:Messaging.InboundEmail.plainTextBodyIsTruncated",
		"apex:Messaging.InboundEmail.references",
		"apex:Messaging.InboundEmail.replyTo",
		"apex:Messaging.InboundEmail.subject",
		"apex:Messaging.InboundEmail.textAttachments",
		"apex:Messaging.InboundEmail.toAddresses",
	},
	"core-runtime-messaging-attachment-accessors-evidence": {
		"apex:Messaging.EmailAttachment",
		"apex:Messaging.EmailAttachment.EmailAttachment()",
		"apex:Messaging.EmailAttachment.body",
		"apex:Messaging.EmailAttachment.equals(Object)",
		"apex:Messaging.EmailAttachment.hashCode()",
		"apex:Messaging.EmailAttachment.toString()",
		"apex:Messaging.EmailFileAttachment.EmailFileAttachment()",
		"apex:Messaging.EmailFileAttachment.body",
		"apex:Messaging.EmailFileAttachment.equals(Object)",
		"apex:Messaging.EmailFileAttachment.hashCode()",
		"apex:Messaging.EmailFileAttachment.inline",
		"apex:Messaging.EmailFileAttachment.toString()",
	},
	"core-runtime-messaging-inbound-handler-evidence": {
		"apex:Messaging.InboundEmailResult",
		"apex:Messaging.InboundEmailResult.InboundEmailResult()",
		"apex:Messaging.InboundEmailResult.clone()",
		"apex:Messaging.InboundEmailResult.message",
		"apex:Messaging.InboundEmailResult.success",
		"apex:Messaging.InboundEnvelope",
		"apex:Messaging.InboundEnvelope.InboundEnvelope()",
		"apex:Messaging.InboundEnvelope.clone()",
		"apex:Messaging.InboundEnvelope.fromAddress",
		"apex:Messaging.InboundEnvelope.toAddress",
	},
}

func TestMessagingInboundDTODepthHasExactExecutableOwnership(t *testing.T) {
	root := filepath.Join("..", "..")
	want := make(map[string]string)
	var selected []SurfaceLedgerRow
	for fixtureName, ids := range messagingInboundDTODepthOwners {
		fixturePath := filepath.Join(root, "docs", "fixtures", fixtureName+".json")
		fixture, err := compat.LoadFile(fixturePath)
		if err != nil {
			t.Fatal(err)
		}
		if err := compat.Validate(fixture); err != nil {
			t.Fatal(err)
		}
		if fixture.Command.Kind != "test" || len(fixture.Source) == 0 {
			t.Fatalf("fixture %s envelope = %#v", fixtureName, fixture)
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
	if len(wantIDs) != 42 {
		t.Fatalf("selected rows = %d, want 42", len(wantIDs))
	}
	for _, row := range selected {
		if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported || len(row.Sources) != 1 || row.Sources[0] != "fixture:"+want[row.SurfaceID] {
			t.Fatalf("%s evidence/behavior/source = %s/%s/%v", row.SurfaceID, row.Evidence, row.GladeBehavior, row.Sources)
		}
	}

	witnesses := map[string][]string{
		"core-runtime-messaging-inbound-email-dto-evidence": {
			"header.name = 'Content-Type';", "header.value = 'text/plain';",
			"email.binaryAttachments =", "email.ccAddresses =", "email.headers =", "email.textAttachments =", "email.toAddresses =",
			"email.fromAddress =", "email.fromName =", "email.htmlBody =", "email.htmlBodyIsTruncated =", "email.inReplyTo =", "email.messageId =", "email.plainTextBody =", "email.plainTextBodyIsTruncated =", "email.references =", "email.replyTo =", "email.subject =",
			"Messaging.InboundEmail clonedEmail = (Messaging.InboundEmail)email.clone();", "System.assertNotEquals(email, clonedEmail);",
		},
		"core-runtime-messaging-attachment-accessors-evidence": {
			"attachment.body =", "attachment.contentId =", "attachment.contentType =", "attachment.fileName =",
			"attachment.equals(attachment)", "attachment.equals(new Messaging.EmailAttachment())", "attachment.equals(null)", "attachment.hashCode()", "attachment.toString()",
			"fileAttachment.body =", "fileAttachment.contentType =", "fileAttachment.fileName =", "fileAttachment.inline =",
			"fileAttachment.equals(fileAttachment)", "fileAttachment.equals(new Messaging.EmailFileAttachment())", "fileAttachment.equals(null)", "fileAttachment.hashCode()", "fileAttachment.toString()",
		},
		"core-runtime-messaging-inbound-handler-evidence": {
			"envelope.fromAddress =", "envelope.toAddress =", "Messaging.InboundEnvelope clonedEnvelope = (Messaging.InboundEnvelope)envelope.clone();", "System.assertNotEquals(envelope, clonedEnvelope);",
			"result.success = true;", "result.message =", "Messaging.InboundEmailResult clonedResult = (Messaging.InboundEmailResult)result.clone();", "System.assertNotEquals(result, clonedResult);",
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
