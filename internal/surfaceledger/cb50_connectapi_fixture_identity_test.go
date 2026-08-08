package surfaceledger

import (
	"path/filepath"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func TestCB50ConnectAPIFixtureDoesNotReintroduceMalformedStandaloneMethodIDs(t *testing.T) {
	fixturePath := filepath.Join("..", "..", "docs", "fixtures", "apex-connectapi-offplatform-unsupported-surfaces.json")
	fixture, err := compat.LoadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := BuildEvidenceSnapshot([]string{fixturePath})
	if err != nil {
		t.Fatal(err)
	}
	byID := rowsByID(rows)

	for _, id := range []string{
		"apex:ConnectApi.getTextClassificationsBulkResults(ids)",
		"apex:ConnectApi.productsExpand(scope,",
		"apex:ConnectApi.productsReturnRate(pageParam,",
		"apex:ConnectApi.submitTextClassificationsRequest(textClassificationsRequestInput,",
	} {
		for _, evidence := range fixture.Evidence {
			if evidence.SurfaceID == id {
				t.Errorf("fixture reintroduced malformed evidence ID %s", id)
			}
		}
		if _, ok := byID[id]; ok {
			t.Errorf("evidence snapshot contains malformed ID %s", id)
		}
	}
}
