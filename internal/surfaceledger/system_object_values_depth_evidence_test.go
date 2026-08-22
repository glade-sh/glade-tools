package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func TestSystemObjectValuesHaveExactExecutableLocalEvidence(t *testing.T) {
	wantIDs := []string{
		"apex:System.Object",
		"apex:System.Object.equals(Object)",
		"apex:System.Object.hashCode()",
		"apex:System.Object.toString()",
	}
	const fixtureName = "core-runtime-system-object-values-depth"
	const owner = "fixture:" + fixtureName
	root := filepath.Join("..", "..")
	fixturePath := filepath.Join(root, "docs", "fixtures", fixtureName+".json")
	fixture, err := compat.LoadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Command.Kind != "test" || len(fixture.Source) != 1 || len(fixture.Command.Args) != 0 {
		t.Fatalf("fixture execution envelope = %#v", fixture)
	}
	if result, err := compat.Run(fixture); err != nil || !result.OK {
		t.Fatalf("fixture execution = %#v, error = %v", result, err)
	}

	rows, err := BuildEvidenceSnapshot([]string{fixturePath})
	if err != nil {
		t.Fatal(err)
	}
	want := make(map[string]struct{}, len(wantIDs))
	for _, id := range wantIDs {
		want[id] = struct{}{}
	}
	var selected []SurfaceLedgerRow
	for _, row := range rows {
		if _, ok := want[row.SurfaceID]; ok {
			selected = append(selected, row)
		}
	}
	assertExactSurfaceSet(t, selected, wantIDs)
	for _, row := range selected {
		if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported || len(row.Sources) != 1 || row.Sources[0] != owner {
			t.Fatalf("%s evidence row = %#v", row.SurfaceID, row)
		}
	}

	source := fixture.Source[0].Content
	for _, witness := range []string{
		"Request request = Request.getCurrent();",
		"Request clonedRequest = (Request)request.clone();",
		"System.assertEquals(request.getRequestId(), clonedRequest.getRequestId());",
		"Object objectValue = (Object)request;",
		"System.assert(objectValue.equals(objectValue));",
		"System.assert(!objectValue.equals(clonedRequest));",
		"System.assertEquals(objectValue.hashCode(), objectValue.hashCode());",
		"System.assertNotEquals(null, objectValue.toString());",
		"Savepoint first = Database.setSavepoint();",
		"Savepoint second = Database.setSavepoint();",
		"System.assert(!first.equals(second));",
		"System.assertEquals(first.hashCode(), first.hashCode());",
		"System.assertNotEquals(null, first.toString());",
	} {
		if !strings.Contains(source, witness) {
			t.Fatalf("source missing %q", witness)
		}
	}

	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	var policy struct {
		SalesforceEligible        *bool  `json:"salesforceEligible"`
		SalesforceExclusionClass  string `json:"salesforceExclusionClass"`
		SalesforceExclusionReason string `json:"salesforceExclusionReason"`
	}
	if err := json.Unmarshal(data, &policy); err != nil {
		t.Fatal(err)
	}
	if policy.SalesforceEligible == nil || *policy.SalesforceEligible || policy.SalesforceExclusionClass != "policy-local-only" || !strings.Contains(policy.SalesforceExclusionReason, "zero Salesforce parity") {
		t.Fatalf("fixture policy = %#v", policy)
	}
}
