package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func TestPrivateMetadataAndDatacloudTypesUseExistingExecutableOwners(t *testing.T) {
	targets := map[string]struct {
		owner   string
		witness string
	}{
		"apex:System.Metadata": {
			owner:   "current-base-metadata-system-alias-deterministic",
			witness: "System.Metadata metadata = new System.Metadata();",
		},
		"apex:Datacloud.FindDuplicates": {
			owner:   "commerce-industry-tail-local-evidence",
			witness: "Datacloud.FindDuplicates.findDuplicates(",
		},
	}
	root := filepath.Join("..", "..", "docs", "fixtures")
	paths, err := filepath.Glob(filepath.Join(root, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := BuildEvidenceSnapshot(paths)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, row := range rows {
		target, ok := targets[row.SurfaceID]
		if !ok {
			continue
		}
		counts[row.SurfaceID]++
		if len(row.Sources) != 1 || row.Sources[0] != "fixture:"+target.owner || row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported {
			t.Fatalf("%s evidence row = %#v", row.SurfaceID, row)
		}
	}
	for id, target := range targets {
		if counts[id] != 1 {
			t.Fatalf("%s fixture rows = %d, want exactly one", id, counts[id])
		}
		path := filepath.Join(root, target.owner+".json")
		fixture, err := compat.LoadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		source := strings.Join(fixture.Command.Args, "\n")
		for _, file := range fixture.Source {
			source += "\n" + file.Content
		}
		if !strings.Contains(source, target.witness) {
			t.Fatalf("%s fixture lacks direct witness %q", id, target.witness)
		}
		result, err := compat.Run(fixture)
		if err != nil || !result.OK {
			t.Fatalf("%s fixture run = %#v, error = %v", id, result, err)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var policy struct {
			Eligible *bool  `json:"salesforceEligible"`
			Class    string `json:"salesforceExclusionClass"`
		}
		if err := json.Unmarshal(raw, &policy); err != nil {
			t.Fatal(err)
		}
		if policy.Eligible == nil || *policy.Eligible || policy.Class != "policy-local-only" {
			t.Fatalf("%s fixture Salesforce policy is not local-only", id)
		}
	}
}
