package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

const messagingDTOMockFixture = "core-runtime-messaging-dto-mock-api67.json"

var messagingDTOMockIDs = []string{
	"apex:Messaging.Email.bccsender",
	"apex:Messaging.Email.emailpriority",
	"apex:Messaging.Email.replyto",
	"apex:Messaging.Email.saveasactivity",
	"apex:Messaging.Email.senderdisplayname",
	"apex:Messaging.Email.usesignature",
	"apex:Messaging.EmailAttachment.contentid",
	"apex:Messaging.EmailAttachment.contenttype",
	"apex:Messaging.EmailAttachment.filename",
	"apex:Messaging.EmailFileAttachment.contenttype",
	"apex:Messaging.EmailFileAttachment.filename",
	"apex:Messaging.PushNotification",
	"apex:Messaging.SingleEmailMessage.entityattachments",
	"apex:Messaging.SingleEmailMessage.fileattachments",
}

func TestMessagingDTOMockHasExactExecutableEvidence(t *testing.T) {
	root := filepath.Join("..", "..")
	path := filepath.Join(root, "docs", "fixtures", messagingDTOMockFixture)
	fixture, err := compat.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Name != strings.TrimSuffix(messagingDTOMockFixture, ".json") || fixture.Command.Kind != "test" || len(fixture.Command.Args) != 0 || len(fixture.Source) != 1 {
		t.Fatalf("fixture envelope = %#v", fixture)
	}
	var metadata struct {
		APIVersion         string `json:"apiVersion"`
		Mode               string `json:"mode"`
		Notes              string `json:"notes"`
		EvidenceOnly       bool   `json:"evidenceOnly"`
		SalesforceEligible *bool  `json:"salesforceEligible"`
		Salesforce         any    `json:"salesforce"`
		Comparisons        any    `json:"comparisons"`
		ExclusionClass     string `json:"salesforceExclusionClass"`
		ExclusionReason    string `json:"salesforceExclusionReason"`
		Profile            struct {
			CandidateCommit string `json:"candidateCommit"`
			CandidateSHA256 string `json:"candidateSha256"`
			SelectedRows    int    `json:"selectedRowCount"`
		} `json:"profile"`
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.APIVersion != "67.0" || metadata.Mode != "local-runtime" || metadata.EvidenceOnly || metadata.SalesforceEligible == nil || *metadata.SalesforceEligible || metadata.Profile.CandidateCommit != "3409c4c85827b19712e9df83fc8905aa02bd1dc8" || metadata.Profile.CandidateSHA256 != "960ac9f26fa92aae6054cbe0e59f9c4ab1f84397df67bd8a89528068d02a1fce" || metadata.Profile.SelectedRows != 14 {
		t.Fatalf("fixture provenance = %#v", metadata)
	}
	if metadata.Salesforce != nil || metadata.Comparisons != nil || metadata.ExclusionClass != "policy-local-only" || !strings.Contains(metadata.ExclusionReason, "no hosted Salesforce execution or parity claim") || !strings.Contains(metadata.Notes, "no hosted Salesforce execution or parity claim") {
		t.Fatalf("fixture parity metadata = %#v", metadata)
	}
	want, seen := map[string]bool{}, map[string]bool{}
	for _, id := range messagingDTOMockIDs {
		want[id] = true
	}
	for _, evidence := range fixture.Evidence {
		if evidence.Kind != "test" || !want[evidence.SurfaceID] || seen[evidence.SurfaceID] {
			t.Fatalf("unexpected or duplicate evidence: %#v", evidence)
		}
		seen[evidence.SurfaceID] = true
	}
	if len(seen) != 14 {
		t.Fatalf("evidence rows = %d, want 14", len(seen))
	}
	for _, witness := range []string{"@IsTest", "bccsender", "emailpriority", "replyto", "saveasactivity", "senderdisplayname", "usesignature", "contentid", "contenttype", "filename", "PushNotification", "entityattachments", "fileattachments"} {
		if !strings.Contains(fixture.Source[0].Content, witness) {
			t.Fatalf("source missing witness %q", witness)
		}
	}
	evidence, err := BuildEvidenceSnapshot([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(evidence) != 14 {
		t.Fatalf("snapshot rows = %d, want 14", len(evidence))
	}
	for _, row := range evidence {
		if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported {
			t.Fatalf("%s evidence/behavior = %s/%s", row.SurfaceID, row.Evidence, row.GladeBehavior)
		}
	}
	if result, err := compat.Run(fixture); err != nil || !result.OK {
		t.Fatalf("fixture execution = %#v, error = %v", result, err)
	}
	owners := map[string]int{}
	paths, err := filepath.Glob(filepath.Join(root, "docs", "fixtures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range paths {
		var header struct {
			EvidenceOnly bool `json:"evidenceOnly"`
			Evidence     []struct {
				SurfaceID string `json:"surfaceId"`
			} `json:"evidence"`
		}
		readJSON(t, file, &header)
		if header.EvidenceOnly {
			continue
		}
		for _, row := range header.Evidence {
			if want[row.SurfaceID] {
				owners[row.SurfaceID]++
			}
		}
	}
	for _, id := range messagingDTOMockIDs {
		if owners[id] != 1 {
			t.Fatalf("fixture ownership for %s = %d, want 1", id, owners[id])
		}
	}
}
