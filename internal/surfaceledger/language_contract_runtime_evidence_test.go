package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func TestLanguageContractFixturesHaveExactExecutableEvidence(t *testing.T) {
	root := filepath.Join("..", "..")
	cases := []struct {
		file string
		kind string
		ids  []string
	}{
		{"core-runtime-namespace-variable-precedence-api67.json", "test", []string{"apex-language:NamespaceClassVariablePrecedence"}},
		{"core-runtime-callable-interface-dispatch.json", "test", []string{"apex:System.Callable.call(String,Map<String,Object>)"}},
		{"core-runtime-comparable-comparator-dispatch.json", "test", []string{"apex:System.Comparable", "apex:System.Comparable.compareTo(Object)", "apex:System.Comparator", "apex:System.Comparator.compare(Object,Object)"}},
	}
	wanted := map[string]int{}
	for _, test := range cases {
		path := filepath.Join(root, "docs", "fixtures", test.file)
		fixture, err := compat.LoadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := compat.Validate(fixture); err != nil {
			t.Fatal(err)
		}
		if fixture.Command.Kind != test.kind || len(fixture.Evidence) != len(test.ids) {
			t.Fatalf("%s envelope = %#v", test.file, fixture)
		}
		if len(fixture.Source) == 0 {
			t.Fatalf("%s has no source", test.file)
		}
		if result, err := compat.Run(fixture); err != nil || !result.OK {
			t.Fatalf("%s result = %#v, error = %v", test.file, result, err)
		}
		rows, err := BuildEvidenceSnapshot([]string{path})
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]bool{}
		for _, row := range rows {
			got[row.SurfaceID] = row.Evidence == EvidenceFixture && (!strings.HasPrefix(row.SurfaceID, "apex:") || row.GladeBehavior == BehaviorSupported)
		}
		for _, id := range test.ids {
			if !got[id] {
				t.Fatalf("%s missing supported fixture row %s: %#v", test.file, id, rows)
			}
			wanted[id] = 0
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
			Eligible        *bool  `json:"salesforceEligible"`
			ExclusionClass  string `json:"salesforceExclusionClass"`
			ExclusionReason string `json:"salesforceExclusionReason"`
		}
		if err := json.Unmarshal(data, &metadata); err != nil {
			t.Fatal(err)
		}
		if metadata.APIVersion != "67.0" || metadata.Mode != "local-runtime" || metadata.EvidenceOnly || metadata.Candidate.Commit != "693bc1b8652907eee1c40c1c9f4604637f06a172" || metadata.Candidate.SHA != "235ef5f5fd6b35a9eec2ab81c129c2639c0282ff66573e8dbace80e991481bc3" || metadata.Profile.CandidateCommit != metadata.Candidate.Commit || metadata.Profile.CandidateSHA != metadata.Candidate.SHA || metadata.Profile.SelectedRows != len(test.ids) || metadata.Eligible == nil || *metadata.Eligible || metadata.ExclusionClass != "policy-local-only" || !strings.Contains(metadata.ExclusionReason, "zero hosted Salesforce parity") {
			t.Fatalf("%s provenance = %#v", test.file, metadata)
		}
	}

	paths, err := filepath.Glob(filepath.Join(root, "docs", "fixtures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		var fixture struct {
			EvidenceOnly bool `json:"evidenceOnly"`
			Evidence     []struct {
				SurfaceID string `json:"surfaceId"`
			} `json:"evidence"`
		}
		readJSON(t, path, &fixture)
		if fixture.EvidenceOnly {
			continue
		}
		for _, row := range fixture.Evidence {
			if _, ok := wanted[row.SurfaceID]; ok {
				wanted[row.SurfaceID]++
			}
		}
	}
	for id, count := range wanted {
		if count != 1 {
			t.Errorf("non-evidenceOnly owners for %s = %d, want 1", id, count)
		}
	}
}
