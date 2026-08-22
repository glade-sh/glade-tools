package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

const (
	deterministicTailCandidateCommit = "86ec4226e33f205bf7a42f6f00cc40aa57fc11b5"
	deterministicTailCandidateSHA    = "0aa758618a8908550aa468c4c9eabd1fcdd06f9f6a7d317ccce45a077380d29a"
)

func TestDeterministicTailEvidenceHasExactLocalRows(t *testing.T) {
	root := filepath.Join("..", "..")
	tests := []struct {
		name string
		mode string
		kind string
		ids  []string
	}{
		{
			name: "core-runtime-deterministic-tail-local-evidence-api67.json",
			mode: "deterministic-mock",
			kind: "test",
			ids: []string{
				"apex:System.FeatureManagement.checkPermission",
				"apex:System.FeatureManagement.checkPermission(String)",
				"apex:System.Http.send(HttpRequest)",
			},
		},
	}

	seen := map[string]string{}
	for _, test := range tests {
		path := filepath.Join(root, "docs", "fixtures", test.name)
		fixture, err := compat.LoadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := compat.Validate(fixture); err != nil {
			t.Fatal(err)
		}
		if fixture.Command.Kind != test.kind {
			t.Fatalf("%s envelope = kind:%q", test.name, fixture.Command.Kind)
		}
		result, err := compat.Run(fixture)
		if err != nil || !result.OK {
			t.Fatalf("%s execution = %#v, error = %v", test.name, result, err)
		}
		evidence, err := BuildEvidenceSnapshot([]string{path})
		if err != nil {
			t.Fatal(err)
		}
		assertExactSurfaceSet(t, evidence, test.ids)
		for _, row := range evidence {
			if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported || len(row.Sources) != 1 || row.Sources[0] != "fixture:"+fixture.Name {
				t.Fatalf("%s row = %#v", row.SurfaceID, row)
			}
			if seen[row.SurfaceID] != "" {
				t.Fatalf("duplicate target owner for %s: %s and %s", row.SurfaceID, seen[row.SurfaceID], test.name)
			}
			seen[row.SurfaceID] = test.name
		}

		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var metadata struct {
			APIVersion   string `json:"apiVersion"`
			Mode         string `json:"mode"`
			EvidenceOnly bool   `json:"evidenceOnly"`
			Candidate    struct {
				Commit string `json:"commit"`
				SHA    string `json:"sha256"`
			} `json:"candidate"`
			Profile struct {
				CandidateCommit string `json:"candidateCommit"`
				CandidateSHA    string `json:"candidateSha256"`
				SelectedRows    int    `json:"selectedRowCount"`
			} `json:"profile"`
			SalesforceEligible        *bool  `json:"salesforceEligible"`
			SalesforceExclusionClass  string `json:"salesforceExclusionClass"`
			SalesforceExclusionReason string `json:"salesforceExclusionReason"`
		}
		if err := json.Unmarshal(data, &metadata); err != nil {
			t.Fatal(err)
		}
		if metadata.APIVersion != "67.0" || metadata.Mode != test.mode || metadata.EvidenceOnly || metadata.Candidate.Commit != deterministicTailCandidateCommit || metadata.Candidate.SHA != deterministicTailCandidateSHA || metadata.Profile.CandidateCommit != deterministicTailCandidateCommit || metadata.Profile.CandidateSHA != deterministicTailCandidateSHA || metadata.Profile.SelectedRows != len(test.ids) || metadata.SalesforceEligible == nil || *metadata.SalesforceEligible || metadata.SalesforceExclusionClass != "policy-local-only" || !strings.Contains(strings.ToLower(metadata.SalesforceExclusionReason), "zero salesforce parity") {
			t.Fatalf("%s provenance = %#v", test.name, metadata)
		}
	}
	if len(seen) != 3 {
		t.Fatalf("deterministic tail accepted rows = %d, want 3", len(seen))
	}
}

func TestDeterministicTailGlobalNonEvidenceOnlyOwnership(t *testing.T) {
	const fixtureName = "core-runtime-deterministic-tail-local-evidence-api67.json"
	targets := []string{
		"apex:System.FeatureManagement.checkPermission",
		"apex:System.FeatureManagement.checkPermission(String)",
		"apex:System.Http.send(HttpRequest)",
	}

	type evidenceRow struct {
		SurfaceID string `json:"surfaceId"`
	}
	var envelope struct {
		EvidenceOnly bool          `json:"evidenceOnly"`
		Evidence     []evidenceRow `json:"evidence"`
	}
	owners := make(map[string][]string, len(targets))
	fixtureRoot := filepath.Join("..", "..", "docs", "fixtures")
	paths, err := filepath.Glob(filepath.Join(fixtureRoot, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		envelope = struct {
			EvidenceOnly bool          `json:"evidenceOnly"`
			Evidence     []evidenceRow `json:"evidence"`
		}{}
		if err := json.Unmarshal(data, &envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.EvidenceOnly {
			continue
		}
		for _, row := range envelope.Evidence {
			for _, target := range targets {
				if row.SurfaceID == target {
					owners[target] = append(owners[target], filepath.Base(path))
				}
			}
		}
	}
	for _, target := range targets {
		if got := owners[target]; len(got) != 1 || got[0] != fixtureName {
			t.Fatalf("non-evidenceOnly owners for %s = %v, want [%s]", target, got, fixtureName)
		}
	}
}
