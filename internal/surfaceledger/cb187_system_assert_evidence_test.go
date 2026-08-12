package surfaceledger

import (
	"path/filepath"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

var cb187SystemAssertSurfaceIDs = []string{
	"apex:System.Assert",
	"apex:System.Assert.areEqual",
	"apex:System.Assert.areNotEqual",
	"apex:System.Assert.fail",
	"apex:System.Assert.fail()",
	"apex:System.Assert.fail(String)",
	"apex:System.Assert.isFalse",
	"apex:System.Assert.isNotNull",
	"apex:System.Assert.isNull",
	"apex:System.Assert.isTrue",
	"apex:System.AssertException",
	"apex:System.AssertException.AssertException()",
	"apex:System.AssertException.AssertException(Exception)",
	"apex:System.AssertException.AssertException(String)",
	"apex:System.AssertException.AssertException(String,Exception)",
	"apex:System.AssertException.getCause()",
	"apex:System.AssertException.getInaccessibleFields()",
	"apex:System.AssertException.getMessage()",
	"apex:System.AssertException.getTypeName()",
	"apex:System.AssertException.initCause(Exception)",
	"apex:System.AssertException.setMessage(String)",
}

func TestCB187SystemAssertRowsHaveExactDualEvidence(t *testing.T) {
	root := filepath.Join("..", "..")
	fixturePath := filepath.Join(root, "docs", "fixtures", "current-base-cb187-system-assert-positive-api67.json")
	comparisonPath := filepath.Join(root, "docs", "fixtures", "salesforce-cb187-system-assert-comparisons.json")
	fixture, err := compat.LoadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if fixture.Name != "current-base-cb187-system-assert-positive-api67" {
		t.Fatalf("fixture name = %q", fixture.Name)
	}
	if result, err := compat.Run(fixture); err != nil || !result.OK {
		t.Fatalf("fixture execution = %#v, error = %v", result, err)
	}

	fixtureEvidence, err := BuildEvidenceSnapshot([]string{fixturePath})
	if err != nil {
		t.Fatal(err)
	}
	oracleEvidence, err := BuildOracleEvidenceSnapshot([]string{comparisonPath})
	if err != nil {
		t.Fatal(err)
	}
	assertExactSurfaceSet(t, fixtureEvidence, cb187SystemAssertSurfaceIDs)
	assertExactSurfaceSet(t, oracleEvidence, cb187SystemAssertSurfaceIDs)

	ledger := Merge(nil, nil, BuildGladeSnapshot(), append(fixtureEvidence, oracleEvidence...))
	byID := rowsByID(ledger.Rows)
	for _, id := range cb187SystemAssertSurfaceIDs {
		row, ok := byID[id]
		if !ok {
			t.Fatalf("missing CB187 evidence row %s", id)
		}
		if row.Evidence != EvidenceFixtureAndOracle {
			t.Errorf("%s evidence = %s, want fixture-and-oracle", id, row.Evidence)
		}
		if row.GladeBehavior != BehaviorSupported {
			t.Errorf("%s behavior = %s, want supported", id, row.GladeBehavior)
		}
	}
}
