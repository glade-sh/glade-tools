package surfaceledger

import (
	"path/filepath"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

var cb185DatabaseSchemaSurfaceIDs = []string{
	"apex:System.Database.insert(Object)",
	"apex:System.Database.update(Object)",
	"apex:System.Database.delete(Object)",
	"apex:Schema.DescribeSObjectResult.getLabelPlural()",
	"apex:Schema.DescribeSObjectResult.isAccessible()",
	"apex:Schema.DescribeSObjectResult.isCreateable()",
	"apex:Schema.DescribeSObjectResult.isDeletable()",
	"apex:Schema.DescribeSObjectResult.isQueryable()",
	"apex:Schema.DescribeSObjectResult.isSearchable()",
	"apex:Schema.DescribeSObjectResult.isUndeletable()",
	"apex:Schema.DescribeSObjectResult.isUpdateable()",
	"apex:Schema.DescribeFieldResult.getLength()",
	"apex:Schema.DescribeFieldResult.getType()",
	"apex:Schema.DescribeFieldResult.isAccessible()",
	"apex:Schema.DescribeFieldResult.isCreateable()",
	"apex:Schema.DescribeFieldResult.isNameField()",
	"apex:Schema.DescribeFieldResult.isUpdateable()",
}

func TestCB185DatabaseSchemaRowsHaveExactDualEvidence(t *testing.T) {
	root := filepath.Join("..", "..")
	fixturePath := filepath.Join(root, "docs", "fixtures", "current-base-cb185-database-schema-positive-api67.json")
	fixture, err := compat.LoadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if fixture.Name != "current-base-cb185-database-schema-positive-api67" {
		t.Fatalf("fixture name = %q", fixture.Name)
	}
	fixtureEvidence, err := BuildEvidenceSnapshot([]string{fixturePath})
	if err != nil {
		t.Fatal(err)
	}
	oracle, err := BuildOracleEvidenceSnapshot([]string{filepath.Join(root, "docs", "fixtures", "salesforce-cb185-database-schema-comparisons.json")})
	if err != nil {
		t.Fatal(err)
	}
	assertExactSurfaceSet(t, fixtureEvidence, cb185DatabaseSchemaSurfaceIDs)
	assertExactSurfaceSet(t, oracle, cb185DatabaseSchemaSurfaceIDs)

	ledger := Merge(nil, nil, BuildGladeSnapshot(), append(fixtureEvidence, oracle...))
	byID := rowsByID(ledger.Rows)
	for _, id := range cb185DatabaseSchemaSurfaceIDs {
		row, ok := byID[id]
		if !ok {
			t.Fatalf("missing CB185 evidence row %s", id)
		}
		if row.Evidence != EvidenceFixtureAndOracle {
			t.Errorf("%s evidence = %s, want fixture-and-oracle", id, row.Evidence)
		}
		if row.GladeBehavior != BehaviorSupported {
			t.Errorf("%s behavior = %s, want supported", id, row.GladeBehavior)
		}
	}
}
