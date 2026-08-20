package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func TestSystemMetadataAliasHasHonestLocalOnlyTypeEvidence(t *testing.T) {
	const (
		id    = "apex:System.Metadata"
		owner = "fixture:current-base-metadata-system-alias-deterministic"
	)
	root := filepath.Join("..", "..")
	paths, err := filepath.Glob(filepath.Join(root, "docs", "fixtures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := BuildEvidenceSnapshot(paths)
	if err != nil {
		t.Fatal(err)
	}
	var selected []SurfaceLedgerRow
	for _, row := range rows {
		if row.SurfaceID == id {
			selected = append(selected, row)
		}
	}
	assertExactSurfaceSet(t, selected, []string{id})
	row := selected[0]
	if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported || len(row.Sources) != 1 || row.Sources[0] != owner {
		t.Fatalf("%s evidence/behavior/source = %s/%s/%v", row.SurfaceID, row.Evidence, row.GladeBehavior, row.Sources)
	}
	if strings.Contains(strings.ToLower(row.Notes), "documented") || !strings.Contains(row.Notes, "local compatibility alias") {
		t.Fatalf("%s note = %q, want honest local compatibility boundary", row.SurfaceID, row.Notes)
	}

	fixturePath := filepath.Join(root, "docs", "fixtures", "current-base-metadata-system-alias-deterministic.json")
	fixture, err := compat.LoadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	if result, err := compat.Run(fixture); err != nil || !result.OK {
		t.Fatalf("fixture execution = %#v, error = %v", result, err)
	}
	for _, evidence := range fixture.Evidence {
		if strings.HasPrefix(evidence.SurfaceID, "apex:System.Metadata") && strings.Contains(strings.ToLower(evidence.Notes), "documented") {
			t.Fatalf("%s still claims a documented alias: %q", evidence.SurfaceID, evidence.Notes)
		}
	}
	if len(fixture.Source) != 1 || !strings.Contains(fixture.Source[0].Content, "System.Metadata metadata = new System.Metadata();") {
		t.Fatalf("fixture lacks direct System.Metadata type witness")
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
	if policy.SalesforceEligible == nil || *policy.SalesforceEligible || policy.SalesforceExclusionClass != "policy-local-only" || !strings.Contains(policy.SalesforceExclusionReason, "rejects the System.Metadata alias") || !strings.Contains(policy.SalesforceExclusionReason, "excluded from hosted parity") {
		t.Fatalf("fixture policy = %#v", policy)
	}
}
