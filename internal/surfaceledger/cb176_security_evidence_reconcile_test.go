package surfaceledger

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCB176SecurityFourArgumentLocalEvidenceWinsOverStaleNegative(t *testing.T) {
	root := filepath.Join("..", "..")
	evidence, err := BuildEvidenceSnapshot([]string{
		filepath.Join(root, "docs", "fixtures", "core-runtime-cb72-frozen-behavior-local-evidence.json"),
		filepath.Join(root, "docs", "fixtures", "data-runtime-sobject-access-decision.json"),
	})
	if err != nil {
		t.Fatal(err)
	}

	const target = "apex:System.Security.stripInaccessible(AccessType,List<Object>,Boolean,Id)"
	ledger := Merge(nil, nil, BuildGladeSnapshot(), evidence)
	row, ok := rowsByID(ledger.Rows)[target]
	if !ok {
		t.Fatalf("missing target row %s", target)
	}
	if row.GladeBehavior != BehaviorSupported {
		t.Fatalf("target behavior = %s, want supported", row.GladeBehavior)
	}
	if row.Evidence != EvidenceFixture {
		t.Fatalf("target evidence = %s, want fixture", row.Evidence)
	}
	if row.Bucket != BucketImplemented || row.GapClass != "" {
		t.Fatalf("target classification = bucket:%s gap:%s, want implemented/no gap", row.Bucket, row.GapClass)
	}
	notes := strings.ToLower(row.Notes)
	if !strings.Contains(notes, "local permission state only") || !strings.Contains(notes, "no hosted org permission state claimed") {
		t.Fatalf("target notes must state the bounded local-only non-claim: %q", row.Notes)
	}
	for _, sibling := range []string{
		"apex:System.Security.stripInaccessible(AccessType,List<Object>)",
		"apex:System.Security.stripInaccessible(AccessType,List<Object>,Boolean)",
	} {
		if got := rowsByID(ledger.Rows)[sibling].GladeBehavior; got != BehaviorSupported {
			t.Fatalf("sibling %s behavior = %s, want supported", sibling, got)
		}
	}
}
