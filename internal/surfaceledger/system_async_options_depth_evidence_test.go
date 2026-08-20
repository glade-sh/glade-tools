package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func TestSystemAsyncOptionsHasExactExecutableLocalEvidence(t *testing.T) {
	wantIDs := []string{
		"apex:System.AsyncOptions",
		"apex:System.AsyncOptions.AsyncOptions()",
		"apex:System.AsyncOptions.DuplicateSignature",
		"apex:System.AsyncOptions.MaximumQueueableStackDepth",
		"apex:System.AsyncOptions.MinimumQueueableDelayInMinutes",
		"apex:System.AsyncOptions.clone()",
	}
	const fixtureName = "core-runtime-system-async-options-depth"
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
	if fixture.Command.Kind != "exec" || len(fixture.Source) != 1 || len(fixture.Command.Args) != 1 || fixture.Source[0].Content != fixture.Command.Args[0] {
		t.Fatalf("fixture execution envelope = %#v", fixture)
	}
	if result, err := compat.Run(fixture); err != nil || !result.OK {
		t.Fatalf("fixture execution = %#v, error = %v", result, err)
	}

	paths, err := filepath.Glob(filepath.Join(root, "docs", "fixtures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := BuildEvidenceSnapshot(paths)
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
		"AsyncOptions options = new AsyncOptions();",
		"options.MaximumQueueableStackDepth = 4;",
		"options.MinimumQueueableDelayInMinutes = 7;",
		"options.DuplicateSignature = signature;",
		"System.assertEquals(4, options.MaximumQueueableStackDepth);",
		"System.assertEquals(7, options.MinimumQueueableDelayInMinutes);",
		"AsyncOptions cloned = (AsyncOptions)options.clone();",
		"System.assertEquals(signature.toString(), ((QueueableDuplicateSignature)cloned.DuplicateSignature).toString());",
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
