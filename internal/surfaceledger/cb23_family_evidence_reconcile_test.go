package surfaceledger

import (
	"path/filepath"
	"testing"
)

func TestCB23MergedFamilyEvidenceClosesTargetRows(t *testing.T) {
	fixtureNames := []string{
		"apex-tail-supported-shape-evidence.json",
		"core-runtime-math-location-exact-evidence.json",
		"core-runtime-address-value-object.json",
		"core-runtime-messaging-single-email-attachments-evidence.json",
		"visualforce-controller-runtime.json",
		"core-feature-management-constructor-unsupported.json",
		"core-feature-management.json",
	}
	paths := make([]string, 0, len(fixtureNames))
	for _, name := range fixtureNames {
		paths = append(paths, filepath.Join("..", "..", "docs", "fixtures", name))
	}
	evidence, err := BuildEvidenceSnapshot(paths)
	if err != nil {
		t.Fatal(err)
	}
	ledger := Merge(nil, nil, BuildGladeSnapshot(), evidence)
	byID := rowsByID(ledger.Rows)

	for _, id := range []string{
		"apex:System.Location.newInstance()",
		"apex:System.Location.getDistance()",
		"apex:System.Address.getDistance()",
		"apex:Messaging.SingleEmailMessage.setDocumentAttachments()",
		"apex:ApexPages.KnowledgeArticleVersionStandardController.setDataCategory()",
		"apex:System.FeatureManagement",
	} {
		row, ok := byID[id]
		if !ok {
			t.Fatalf("missing target row %s", id)
		}
		if row.GladeShape == ShapeAbsent || row.GladeBehavior != BehaviorSupported || row.Evidence != EvidenceFixture || row.GapClass != "" {
			t.Fatalf("%s merged state = shape:%s behavior:%s evidence:%s gap:%s", id, row.GladeShape, row.GladeBehavior, row.Evidence, row.GapClass)
		}
	}
}

func TestCB23FeatureManagementConstructorRemainsUnsupported(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "fixtures", "core-feature-management-constructor-unsupported.json")
	evidence, err := BuildEvidenceSnapshot([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	ledger := Merge(nil, nil, BuildGladeSnapshot(), evidence)
	row, ok := rowsByID(ledger.Rows)["apex:System.FeatureManagement.FeatureManagement()"]
	if !ok {
		t.Fatal("missing FeatureManagement constructor row")
	}
	if row.GladeBehavior != BehaviorUnsupported || row.Evidence != EvidenceFixture {
		t.Fatalf("constructor state = behavior:%s evidence:%s, want unsupported/fixture", row.GladeBehavior, row.Evidence)
	}
}
