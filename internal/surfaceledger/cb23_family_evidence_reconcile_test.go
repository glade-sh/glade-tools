package surfaceledger

import (
	"path/filepath"
	"testing"
)

func TestCB23MergedFamilyEvidenceClosesTargetRows(t *testing.T) {
	fixtureNames := []string{
		"apex-tail-supported-shape-evidence.json",
		"core-runtime-math-location-exact-evidence.json",
		"current-base-cb191-system-rebind-positive-api67.json",
		"core-runtime-messaging-single-email-attachments-evidence.json",
		"visualforce-controller-runtime.json",
		"core-feature-management.json",
		"core-runtime-deterministic-tail-local-evidence-api67.json",
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
		"apex:System.Address.getDistance(Location,String)",
		"apex:Messaging.SingleEmailMessage.setDocumentAttachments()",
		"apex:ApexPages.KnowledgeArticleVersionStandardController.setDataCategory()",
		"apex:System.FeatureManagement",
	} {
		row, ok := byID[id]
		if !ok {
			t.Fatalf("missing target row %s", id)
		}
		wantBehavior := BehaviorSupported
		if id == "apex:ApexPages.KnowledgeArticleVersionStandardController.setDataCategory()" {
			wantBehavior = BehaviorNone
		}
		if row.GladeShape == ShapeAbsent || row.GladeBehavior != wantBehavior || row.Evidence != EvidenceFixture || row.GapClass != "" {
			t.Errorf("%s merged state = shape:%s behavior:%s evidence:%s gap:%s", id, row.GladeShape, row.GladeBehavior, row.Evidence, row.GapClass)
		}
	}
}
