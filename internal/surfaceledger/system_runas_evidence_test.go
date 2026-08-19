package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func TestSystemRunAsUserHasExecutableLocalEvidence(t *testing.T) {
	const id = "apex:System.System.runAs(User)"
	root := filepath.Join("..", "..", "docs", "fixtures")
	path := filepath.Join(root, "core-runtime-system-runas-local-evidence.json")
	fixture, err := compat.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if fixture.Command.Kind != "test" || len(fixture.Source) != 1 {
		t.Fatalf("command/source = %q/%d, want test/1", fixture.Command.Kind, len(fixture.Source))
	}
	for _, witness := range []string{"System.runAs(testUser)", "WHERE Name = :accountName", "System.assertEquals(seeded.Id, rows[0].Id)"} {
		if !strings.Contains(fixture.Source[0].Content, witness) {
			t.Fatalf("source missing %q", witness)
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var policy struct {
		Eligible *bool  `json:"salesforceEligible"`
		Class    string `json:"salesforceExclusionClass"`
		Reason   string `json:"salesforceExclusionReason"`
	}
	if err := json.Unmarshal(raw, &policy); err != nil {
		t.Fatal(err)
	}
	if policy.Eligible == nil || *policy.Eligible || policy.Class != "policy-local-only" || !strings.Contains(policy.Reason, "zero Salesforce parity") {
		t.Fatalf("fixture policy = %#v", policy)
	}
	result, err := compat.Run(fixture)
	if err != nil || !result.OK {
		t.Fatalf("fixture execution = %#v, error = %v", result, err)
	}

	paths, err := filepath.Glob(filepath.Join(root, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := BuildEvidenceSnapshot(paths)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, row := range rows {
		if row.SurfaceID != id {
			continue
		}
		count++
		if len(row.Sources) != 1 || row.Sources[0] != "fixture:"+fixture.Name || row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported {
			t.Fatalf("runAs row = %#v", row)
		}
	}
	if count != 1 {
		t.Fatalf("runAs fixture rows = %d, want exactly one", count)
	}
}
