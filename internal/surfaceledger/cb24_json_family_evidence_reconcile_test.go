package surfaceledger

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func TestCB24JSONDeserializeFamilyEvidenceDoesNotInheritScenarioFailure(t *testing.T) {
	fixturePath := func(name string) string {
		return filepath.Join("..", "..", "docs", "fixtures", name)
	}
	positivePaths := []string{
		fixturePath("core-json-stdlib.json"),
		fixturePath("core-json-sobject-field-types.json"),
		fixturePath("core-json-source-dto-roundtrip.json"),
		fixturePath("core-json-typed-collections.json"),
	}
	negativePath := fixturePath("core-json-unsupported-mapping.json")

	positiveEvidence, err := BuildEvidenceSnapshot(positivePaths)
	if err != nil {
		t.Fatal(err)
	}
	withNegative, err := BuildEvidenceSnapshot(append(append([]string{}, positivePaths...), negativePath))
	if err != nil {
		t.Fatal(err)
	}

	positiveLedger := Merge(nil, nil, BuildGladeSnapshot(), positiveEvidence)
	ledger := Merge(nil, nil, BuildGladeSnapshot(), withNegative)
	positiveRows := rowsByID(positiveLedger.Rows)
	rows := rowsByID(ledger.Rows)

	familyID := "apex:System.JSON.deserialize"
	family, ok := rows[familyID]
	if !ok {
		t.Fatalf("missing JSON.deserialize family row %s", familyID)
	}
	if family.GladeBehavior != BehaviorSupported || family.Evidence != EvidenceFixture || family.GapClass != "" {
		t.Fatalf("JSON.deserialize family state = shape:%s behavior:%s evidence:%s gap:%s bucket:%s", family.GladeShape, family.GladeBehavior, family.Evidence, family.GapClass, family.Bucket)
	}

	exactID := "apex:System.JSON.deserialize(String,Type)"
	exact, ok := rows[exactID]
	if !ok {
		t.Fatalf("missing JSON.deserialize overload row %s", exactID)
	}
	if exact.GladeBehavior != BehaviorSupported || exact.Evidence != EvidenceFixture || exact.GapClass != "" {
		t.Fatalf("JSON.deserialize overload state = shape:%s behavior:%s evidence:%s gap:%s bucket:%s", exact.GladeShape, exact.GladeBehavior, exact.Evidence, exact.GapClass, exact.Bucket)
	}

	for _, id := range []string{
		"apex:System.JSON.deserializeStrict(String,Type)",
		"apex:System.JSON.deserializeUntyped(String)",
		"apex:System.JSON.serialize(Object,Boolean)",
		"apex:System.JSON.serializePretty(Object,Boolean)",
	} {
		before, beforeOK := positiveRows[id]
		after, afterOK := rows[id]
		if !beforeOK || !afterOK {
			t.Fatalf("unrelated JSON family %s present before/after correction: %t/%t", id, beforeOK, afterOK)
		}
		if !reflect.DeepEqual(before, after) {
			t.Fatalf("unrelated JSON family %s changed: before=%#v after=%#v", id, before, after)
		}
	}

	negative, err := compat.LoadFile(negativePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(negative.Evidence) != 0 {
		t.Fatalf("negative fixture evidence = %#v, want no scenario-specific family evidence", negative.Evidence)
	}
	if negative.Command.Kind != "exec" || len(negative.Command.Args) != 1 || negative.Command.Args[0] != "JSON.deserialize('{\"Name\":\"Acme\"}', UnknownJsonShape.class);" {
		t.Fatalf("negative fixture command changed: %#v", negative.Command)
	}
	if negative.Expected.Error == nil || negative.Expected.Error.Type != "UnsupportedFeature" || negative.Expected.Error.Message != "unsupported call \"JSON.deserialize local class/SObject mapping for UnknownJsonShape\"" {
		t.Fatalf("negative fixture expected diagnostic changed: %#v", negative.Expected.Error)
	}
}
