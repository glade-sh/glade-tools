package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func TestEventBusGetOperationIDFixtureIsExecutableAndLocalOnly(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "fixtures", "core-runtime-eventbus-operation-id-unsupported.json")
	fixture, err := compat.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var metadata struct {
		SalesforceEligible       *bool  `json:"salesforceEligible"`
		SalesforceExclusionClass string `json:"salesforceExclusionClass"`
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.SalesforceEligible == nil || *metadata.SalesforceEligible {
		t.Fatalf("salesforceEligible = %v, want false", metadata.SalesforceEligible)
	}
	if metadata.SalesforceExclusionClass != "policy-local-only" {
		t.Fatalf("salesforceExclusionClass = %q, want policy-local-only", metadata.SalesforceExclusionClass)
	}
	result, err := compat.Run(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK {
		t.Fatalf("run result = %#v, want ok", result)
	}
	rows, err := BuildEvidenceSnapshot([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("evidence rows = %d, want 1", len(rows))
	}
	row := rows[0]
	if row.SurfaceID != "apex:System.EventBus.getOperationId(Object)" {
		t.Fatalf("surface id = %q", row.SurfaceID)
	}
	if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported {
		t.Fatalf("row evidence/behavior = %s/%s, want fixture/supported", row.Evidence, row.GladeBehavior)
	}
}
