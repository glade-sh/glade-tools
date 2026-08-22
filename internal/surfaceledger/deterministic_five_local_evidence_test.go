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
	deterministicFiveCandidateCommit = "86ec4226e33f205bf7a42f6f00cc40aa57fc11b5"
	deterministicFiveCandidateSHA    = "0aa758618a8908550aa468c4c9eabd1fcdd06f9f6a7d317ccce45a077380d29a"
)

func TestDeterministicFiveLocalEvidenceAdmission(t *testing.T) {
	root := filepath.Join("..", "..")
	want := map[string]string{
		"apex:Schema.DescribeDataCategoryGroupResult":          "core-runtime-local-metadata-search-evidence",
		"apex:Schema.DescribeDataCategoryGroupStructureResult": "core-runtime-local-metadata-search-evidence",
	}
	rejected := []string{
		"apex:Canvas.Test.Test()",
		"apex:Messaging.ActionResult.ActionResult()",
		"apex:Messaging.ActionableNotification.ActionableNotification()",
	}
	fixtures := map[string][]string{
		"core-runtime-local-metadata-search-evidence": {
			"Schema.DescribeDataCategoryGroupResult groupResult = (Schema.DescribeDataCategoryGroupResult)groups[0];",
			"Schema.DescribeDataCategoryGroupStructureResult)structures[0]",
		},
	}
	tracked := make(map[string]bool, len(want)+len(rejected))
	for id := range want {
		tracked[id] = true
	}
	for _, id := range rejected {
		tracked[id] = true
	}
	owners := make(map[string][]string, len(tracked))
	paths, err := filepath.Glob(filepath.Join(root, "docs", "fixtures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var header struct {
			EvidenceOnly bool `json:"evidenceOnly"`
			Evidence     []struct {
				SurfaceID string `json:"surfaceId"`
			} `json:"evidence"`
		}
		if err := json.Unmarshal(data, &header); err != nil {
			t.Fatal(err)
		}
		if header.EvidenceOnly {
			continue
		}
		for _, row := range header.Evidence {
			if tracked[row.SurfaceID] {
				owners[row.SurfaceID] = append(owners[row.SurfaceID], strings.TrimSuffix(filepath.Base(path), ".json"))
			}
		}
	}
	for id, owner := range want {
		if got := owners[id]; len(got) != 1 || got[0] != owner {
			t.Fatalf("non-evidenceOnly ownership for %s = %v, want [%s]", id, got, owner)
		}
	}
	for _, id := range rejected {
		if got := owners[id]; len(got) != 0 {
			t.Fatalf("non-runnable projected row %s retains non-evidenceOnly owners %v", id, got)
		}
	}

	for fixtureName, witnesses := range fixtures {
		path := filepath.Join(root, "docs", "fixtures", fixtureName+".json")
		fixture, err := compat.LoadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := compat.Validate(fixture); err != nil {
			t.Fatal(err)
		}
		if fixture.Command.Kind != "test" || len(fixture.Source) == 0 || len(fixture.Command.Args) != 0 {
			t.Fatalf("%s command/source = %#v/%#v", fixtureName, fixture.Command, fixture.Source)
		}
		var source strings.Builder
		for _, file := range fixture.Source {
			source.WriteString(file.Content)
		}
		for _, witness := range witnesses {
			if !strings.Contains(source.String(), witness) {
				t.Fatalf("%s source missing %q", fixtureName, witness)
			}
		}
		result, err := compat.Run(fixture)
		if err != nil || !result.OK {
			t.Fatalf("%s execution = %#v, error = %v", fixtureName, result, err)
		}
		evidence, err := BuildEvidenceSnapshot([]string{path})
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range evidence {
			if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported || len(row.Sources) != 1 || row.Sources[0] != "fixture:"+fixture.Name {
				t.Fatalf("%s row = %#v", fixtureName, row)
			}
		}

		var metadata struct {
			Candidate struct {
				Commit string `json:"commit"`
				SHA    string `json:"sha256"`
			} `json:"candidate"`
			Profile struct {
				CandidateCommit string `json:"candidateCommit"`
				CandidateSHA    string `json:"candidateSha256"`
			} `json:"profile"`
			SalesforceEligible        *bool           `json:"salesforceEligible"`
			SalesforceExclusionClass  string          `json:"salesforceExclusionClass"`
			SalesforceExclusionReason string          `json:"salesforceExclusionReason"`
			Salesforce                json.RawMessage `json:"salesforce"`
			Comparisons               json.RawMessage `json:"comparisons"`
		}
		if err := json.Unmarshal(dataForFixture(t, path), &metadata); err != nil {
			t.Fatal(err)
		}
		if metadata.Candidate.Commit != deterministicFiveCandidateCommit || metadata.Candidate.SHA != deterministicFiveCandidateSHA || metadata.Profile.CandidateCommit != deterministicFiveCandidateCommit || metadata.Profile.CandidateSHA != deterministicFiveCandidateSHA || metadata.SalesforceEligible == nil || *metadata.SalesforceEligible || metadata.SalesforceExclusionClass != "policy-local-only" || !strings.Contains(strings.ToLower(metadata.SalesforceExclusionReason), "zero salesforce parity") || metadata.Salesforce != nil || metadata.Comparisons != nil {
			t.Fatalf("%s provenance = %#v", fixtureName, metadata)
		}
	}
}

func dataForFixture(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
