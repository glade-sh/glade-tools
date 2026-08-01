package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCB52GladeSnapshotUsesCanonicalIdentities(t *testing.T) {
	byID := rowsByID(BuildGladeSnapshot())

	for _, id := range []string{
		"apex:System.ApexPages.Message",
		"apex:System.Database.UnitOfWork",
		"apex:System.Messaging.SendEmailOptions",
		"apex:System.Messaging.SingleEmailMessage",
		"apex:System.Search.query / SOSL FIND",
		"apex:System.Limits.get*",
		"apex:System.InvalidParameterValueException constructors",
		"apex:System.PageReference(partialURL)",
		"apex:System.BusinessHours malformed local holiday metadata",
		"apex:System.unimplemented platform/stdlib calls",
	} {
		if _, ok := byID[id]; ok {
			t.Fatalf("stale or synthetic identity remains in generated snapshot: %s", id)
		}
	}

	for _, id := range []string{
		ApexTypeID("ApexPages", "Message"),
		ApexTypeID("Database", "UnitOfWork"),
		ApexTypeID("Messaging", "SendEmailOptions"),
		ApexTypeID("Messaging", "SingleEmailMessage"),
		ApexMemberID("System", "Schema", "describeDataCategoryGroups", []string{"List<String>"}),
		ApexMemberID("System", "Schema", "describeDataCategoryGroupStructures", []string{"List<Schema.DataCategoryGroupSobjectTypePair>", "Boolean"}),
		ApexMemberID("System", "Search", "query", []string{"String"}),
	} {
		row, ok := byID[id]
		if !ok {
			t.Fatalf("canonical product identity is missing from generated snapshot: %s", id)
		}
		if row.GladeShape == ShapeAbsent {
			t.Fatalf("canonical product identity has no Glade shape: %s", id)
		}
	}
}

func TestCB52BusinessHoursStringAliasesCanonicalizeToId(t *testing.T) {
	for _, method := range []string{"add", "addGmt", "nextStartDate"} {
		var stringID, idID string
		if method == "nextStartDate" {
			stringID = ApexMemberID("", "BusinessHours", method, []string{"String", "Datetime"})
			idID = ApexMemberID("", "BusinessHours", method, []string{"Id", "Datetime"})
		} else {
			stringID = ApexMemberID("", "BusinessHours", method, []string{"String", "Datetime", "Long"})
			idID = ApexMemberID("", "BusinessHours", method, []string{"Id", "Datetime", "Long"})
		}
		if stringID != idID {
			t.Fatalf("BusinessHours.%s String and Id identities did not canonicalize: %s != %s", method, stringID, idID)
		}
	}
}

func TestCB52CheckedFixturesUseCanonicalOwnedIdentities(t *testing.T) {
	root := filepath.Join("..", "..")
	tests := []struct {
		fixture string
		present []string
		absent  []string
	}{
		{
			fixture: "docs/fixtures/core-runtime-messaging-inbound-email-dto-evidence.json",
			present: []string{
				"apex:Messaging.InboundEmail.AuthenticationResult.AuthenticationResult()",
				"apex:Messaging.InboundEmail.AuthenticationResultField.AuthenticationResultField()",
				"apex:Messaging.InboundEmail.BinaryAttachment.BinaryAttachment()",
				"apex:Messaging.InboundEmail.TextAttachment.TextAttachment()",
			},
			absent: []string{
				"apex:Messaging.InboundEmail.AuthenticationResult.InboundEmail.AuthenticationResult()",
				"apex:Messaging.InboundEmail.AuthenticationResultField.InboundEmail.AuthenticationResultField()",
				"apex:Messaging.InboundEmail.BinaryAttachment.InboundEmail.BinaryAttachment()",
				"apex:Messaging.InboundEmail.TextAttachment.InboundEmail.TextAttachment()",
			},
		},
		{
			fixture: "docs/fixtures/ui-apexpages-message-construction.json",
			present: []string{"apex:ApexPages.Message"},
			absent:  []string{"apex:System.ApexPages.Message"},
		},
		{
			fixture: "docs/fixtures/data-platform-database-unitofwork-evidence.json",
			present: []string{"apex:Database.UnitOfWork"},
			absent:  []string{"apex:System.Database.UnitOfWork"},
		},
		{
			fixture: "docs/fixtures/data-platform-search-result-suggestion-dtos.json",
			absent:  []string{"apex:Search.SuggestionOption.setFilter(Search.KnowledegeSuggestionFilter)"},
		},
		{
			fixture: "docs/fixtures/platform-unsupported-surfaces.json",
			absent:  []string{"apex:Approval.*"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			path := filepath.Join(root, tt.fixture)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var fixture struct {
				Evidence []struct {
					SurfaceID string `json:"surfaceId"`
				} `json:"evidence"`
			}
			if err := json.Unmarshal(data, &fixture); err != nil {
				t.Fatal(err)
			}
			seen := map[string]bool{}
			for _, evidence := range fixture.Evidence {
				seen[evidence.SurfaceID] = true
			}
			for _, id := range tt.present {
				if !seen[id] {
					t.Errorf("fixture is missing canonical identity %s", id)
				}
			}
			for _, id := range tt.absent {
				if seen[id] {
					t.Errorf("fixture retains stale or malformed identity %s", id)
				}
			}
		})
	}
}

func TestCB52NonCanonicalNegativeFixturesDoNotCreateSurfaceRows(t *testing.T) {
	root := filepath.Join("..", "..", "docs", "fixtures")
	paths := []string{
		filepath.Join(root, "core-feature-management-constructor-unsupported.json"),
		filepath.Join(root, "core-runtime-email-exception-dml-accessors-unsupported.json"),
		filepath.Join(root, "data-platform-schema-describe-constructor-fences.json"),
	}
	evidence, err := BuildEvidenceSnapshot(paths)
	if err != nil {
		t.Fatal(err)
	}
	byID := rowsByID(evidence)
	for id := range nonCanonicalGeneratedSurfaceIDs {
		if _, ok := byID[id]; ok {
			t.Fatalf("negative fixture created noncanonical surface row %s", id)
		}
	}
}
