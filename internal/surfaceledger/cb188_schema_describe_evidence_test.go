package surfaceledger

import (
	"path/filepath"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

var cb188SchemaDescribeSurfaceIDs = []string{
	"apex:Schema.SObjectType.equals(Object)",
	"apex:Schema.SObjectType.hashCode()",
	"apex:Schema.SObjectType.toString()",
	"apex:Schema.SObjectType.newSObject()",
	"apex:Schema.SObjectType.newSObject(Id,Boolean)",
	"apex:Schema.SObjectField.equals(Object)",
	"apex:Schema.SObjectField.hashCode()",
	"apex:Schema.SObjectField.toString()",
	"apex:Schema.DescribeSObjectResult.getKeyPrefix()",
	"apex:Schema.DescribeSObjectResult.getLocalName()",
	"apex:Schema.DescribeSObjectResult.getSObjectType()",
	"apex:Schema.DescribeSObjectResult.getSObjectDescribeOption()",
	"apex:Schema.DescribeSObjectResult.fields",
	"apex:Schema.DescribeSObjectResult.isCustom()",
	"apex:Schema.DescribeSObjectResult.isCustomSetting()",
	"apex:Schema.DescribeSObjectResult.isDeprecatedAndHidden()",
	"apex:Schema.DescribeFieldResult.getByteLength()",
	"apex:Schema.DescribeFieldResult.getCalculatedFormula()",
	"apex:Schema.DescribeFieldResult.getCompoundFieldName()",
	"apex:Schema.DescribeFieldResult.getSObjectType()",
	"apex:Schema.DescribeFieldResult.getSobjectField()",
	"apex:Schema.DescribeFieldResult.isNillable()",
	"apex:Schema.DescribeFieldResult.isExternalId()",
	"apex:Schema.DescribeFieldResult.isUnique()",
	"apex:Schema.DescribeFieldResult.isEncrypted()",
	"apex:Schema.DescribeFieldResult.isCalculated()",
	"apex:Schema.DescribeFieldResult.isAutoNumber()",
	"apex:Schema.DescribeFieldResult.isCaseSensitive()",
	"apex:Schema.DescribeFieldResult.isCustom()",
	"apex:Schema.DescribeFieldResult.isHtmlFormatted()",
	"apex:Schema.DescribeFieldResult.getDefaultValue()",
	"apex:Schema.DescribeFieldResult.getDefaultValueFormula()",
	"apex:Schema.DescribeFieldResult.isDefaultedOnCreate()",
	"apex:Schema.DescribeFieldResult.isSortable()",
	"apex:Schema.DescribeFieldResult.getReferenceTo()",
	"apex:Schema.DescribeFieldResult.getRelationshipName()",
	"apex:Schema.DescribeFieldResult.getReferenceTargetField()",
	"apex:Schema.DescribeFieldResult.getRelationshipOrder()",
}

func TestCB188SchemaDescribeRowsHaveExactDualEvidence(t *testing.T) {
	root := filepath.Join("..", "..")
	fixturePath := filepath.Join(root, "docs", "fixtures", "current-base-cb188-schema-describe-positive-api67.json")
	fixture, err := compat.LoadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if fixture.Name != "current-base-cb188-schema-describe-positive-api67" {
		t.Fatalf("fixture name = %q", fixture.Name)
	}
	fixtureEvidence, err := BuildEvidenceSnapshot([]string{fixturePath})
	if err != nil {
		t.Fatal(err)
	}
	oracle, err := BuildOracleEvidenceSnapshot([]string{filepath.Join(root, "docs", "fixtures", "salesforce-cb188-schema-describe-comparisons.json")})
	if err != nil {
		t.Fatal(err)
	}
	assertExactSurfaceSet(t, fixtureEvidence, cb188SchemaDescribeSurfaceIDs)
	assertExactSurfaceSet(t, oracle, cb188SchemaDescribeSurfaceIDs)

	ledger := Merge(nil, nil, BuildGladeSnapshot(), append(fixtureEvidence, oracle...))
	byID := rowsByID(ledger.Rows)
	for _, id := range cb188SchemaDescribeSurfaceIDs {
		row, ok := byID[id]
		if !ok {
			t.Fatalf("missing CB188 evidence row %s", id)
		}
		if row.Evidence != EvidenceFixtureAndOracle {
			t.Errorf("%s evidence = %s, want fixture-and-oracle", id, row.Evidence)
		}
		if row.GladeBehavior != BehaviorSupported {
			t.Errorf("%s behavior = %s, want supported", id, row.GladeBehavior)
		}
	}
}
