package surfaceledger

import (
	"path/filepath"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

var cb190SchemaBroadSurfaceIDs = []string{
	"apex:Schema.SObjectType",
	"apex:Schema.SObjectType.getDescribe()",
	"apex:Schema.SObjectField",
	"apex:Schema.SObjectField.getDescribe()",
	"apex:Schema.DescribeSObjectResult",
	"apex:Schema.DescribeFieldResult",
	"apex:Schema.DisplayType",
	"apex:Schema.DisplayType.STRING",
	"apex:Schema.DescribeSObjectResult.getRecordTypeInfos()",
	"apex:Schema.RecordTypeInfo",
	"apex:Schema.RecordTypeInfo.getName()",
	"apex:Schema.DescribeFieldResult.getPicklistValues()",
	"apex:Schema.PicklistEntry",
	"apex:Schema.PicklistEntry.getValue()",
	"apex:Schema.DisplayType.PICKLIST",
	"apex:Schema.DescribeSObjectResult.getChildRelationships()",
	"apex:Schema.ChildRelationship",
	"apex:Schema.ChildRelationship.getChildSObject()",
}

func TestCB190SchemaBroadRowsHaveExactDualEvidence(t *testing.T) {
	root := filepath.Join("..", "..")
	fixturePath := filepath.Join(root, "docs", "fixtures", "current-base-cb190-schema-broad-positive-api67.json")
	fixture, err := compat.LoadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if fixture.Name != "current-base-cb190-schema-broad-positive-api67" {
		t.Fatalf("fixture name = %q", fixture.Name)
	}
	fixtureEvidence, err := BuildEvidenceSnapshot([]string{fixturePath})
	if err != nil {
		t.Fatal(err)
	}
	oracle, err := BuildOracleEvidenceSnapshot([]string{filepath.Join(root, "docs", "fixtures", "salesforce-cb190-schema-broad-comparisons.json")})
	if err != nil {
		t.Fatal(err)
	}
	assertExactSurfaceSet(t, fixtureEvidence, cb190SchemaBroadSurfaceIDs)
	assertExactSurfaceSet(t, oracle, cb190SchemaBroadSurfaceIDs)

	ledger := Merge(nil, nil, BuildGladeSnapshot(), append(fixtureEvidence, oracle...))
	byID := rowsByID(ledger.Rows)
	for _, id := range cb190SchemaBroadSurfaceIDs {
		row, ok := byID[id]
		if !ok {
			t.Fatalf("missing CB190 evidence row %s", id)
		}
		if row.Evidence != EvidenceFixtureAndOracle {
			t.Errorf("%s evidence = %s, want fixture-and-oracle", id, row.Evidence)
		}
		if row.GladeBehavior != BehaviorSupported {
			t.Errorf("%s behavior = %s, want supported", id, row.GladeBehavior)
		}
	}
}
