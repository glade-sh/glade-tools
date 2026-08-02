package surfaceledger

import (
	"path/filepath"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

var cb189SystemJSONTypeSurfaceIDs = []string{
	"apex:System.JSON",
	"apex:System.JSON.serialize",
	"apex:System.JSON.serialize(Object)",
	"apex:System.JSON.serializePretty",
	"apex:System.JSON.serializePretty(Object)",
	"apex:System.JSON.deserializeUntyped",
	"apex:System.JSON.deserializeUntyped(String)",
	"apex:System.JSON.deserialize",
	"apex:System.JSON.deserialize(String,Type)",
	"apex:System.Type",
	"apex:System.Type.forName",
	"apex:System.Type.forName(String)",
	"apex:System.Type.forName(String,String)",
	"apex:System.Type.forName(namespace,name)",
}

func TestCB189SystemJSONTypeRowsHaveExactDualEvidence(t *testing.T) {
	root := filepath.Join("..", "..")
	fixturePath := filepath.Join(root, "docs", "fixtures", "current-base-cb189-system-json-type-positive-api67.json")
	fixture, err := compat.LoadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if fixture.Name != "current-base-cb189-system-json-type-positive-api67" {
		t.Fatalf("fixture name = %q", fixture.Name)
	}
	fixtureEvidence, err := BuildEvidenceSnapshot([]string{fixturePath})
	if err != nil {
		t.Fatal(err)
	}
	oracle, err := BuildOracleEvidenceSnapshot([]string{filepath.Join(root, "docs", "fixtures", "salesforce-cb189-system-json-type-comparisons.json")})
	if err != nil {
		t.Fatal(err)
	}
	assertExactSurfaceSet(t, fixtureEvidence, cb189SystemJSONTypeSurfaceIDs)
	assertExactSurfaceSet(t, oracle, cb189SystemJSONTypeSurfaceIDs)

	ledger := Merge(nil, nil, BuildGladeSnapshot(), append(fixtureEvidence, oracle...))
	byID := rowsByID(ledger.Rows)
	for _, id := range cb189SystemJSONTypeSurfaceIDs {
		row, ok := byID[id]
		if !ok {
			t.Fatalf("missing CB189 evidence row %s", id)
		}
		if row.Evidence != EvidenceFixtureAndOracle {
			t.Errorf("%s evidence = %s, want fixture-and-oracle", id, row.Evidence)
		}
		if row.GladeBehavior != BehaviorSupported {
			t.Errorf("%s behavior = %s, want supported", id, row.GladeBehavior)
		}
	}
}
