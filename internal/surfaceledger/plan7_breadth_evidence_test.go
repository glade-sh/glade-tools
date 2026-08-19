package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func TestPlan7BreadthClassificationClosesFiveHostedFamiliesWithoutParity(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "fixtures", "plan7-breadth-hosted-explicit-unsupported.json")
	fixture, err := compat.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Command.Kind != "policy-evidence" || len(fixture.Source) != 0 {
		t.Fatalf("classification-only fixture = %#v", fixture)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw struct {
		SalesforceEligible        *bool  `json:"salesforceEligible"`
		SalesforceExclusionClass  string `json:"salesforceExclusionClass"`
		SalesforceExclusionReason string `json:"salesforceExclusionReason"`
		Evidence                  []struct {
			SurfaceID string `json:"surfaceId"`
			Kind      string `json:"kind"`
			Notes     string `json:"notes"`
		} `json:"evidence"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if raw.SalesforceEligible == nil || *raw.SalesforceEligible || raw.SalesforceExclusionClass != "policy-local-only" || raw.SalesforceExclusionReason == "" {
		t.Fatalf("Salesforce boundary = %#v", raw)
	}

	wantPrefixes := map[string]int{
		"data-reference:":  66,
		"lwc:":             3,
		"tooling:":         16,
		"metadata-api:":    4,
		"platform-events:": 3,
	}
	gotPrefixes := map[string]int{}
	seen := map[string]bool{}
	for _, item := range raw.Evidence {
		if seen[item.SurfaceID] || item.Kind != "unsupported" || item.Notes == "" {
			t.Fatalf("invalid evidence row %#v", item)
		}
		seen[item.SurfaceID] = true
		for prefix := range wantPrefixes {
			if strings.HasPrefix(item.SurfaceID, prefix) {
				gotPrefixes[prefix]++
				break
			}
		}
	}
	if len(seen) != 92 {
		t.Fatalf("unique evidence rows = %d, want 92", len(seen))
	}
	for prefix, want := range wantPrefixes {
		if got := gotPrefixes[prefix]; got != want {
			t.Fatalf("%s rows = %d, want %d", prefix, got, want)
		}
	}

	rows, err := BuildEvidenceSnapshot([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 92 {
		t.Fatalf("snapshot rows = %d, want 92", len(rows))
	}
	for _, row := range rows {
		if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorUnsupported {
			t.Fatalf("%s evidence/behavior = %s/%s", row.SurfaceID, row.Evidence, row.GladeBehavior)
		}
	}
}
