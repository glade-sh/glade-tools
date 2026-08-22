package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

const systemStringTailFixture = "core-runtime-system-string-template-value-map-api67.json"

var systemStringTailIDs = []string{"apex:System.String.template(valueMap)"}

func TestSystemStringTailHasExactSealedCandidateEvidence(t *testing.T) {
	root := filepath.Join("..", "..")
	fixturePath := filepath.Join(root, "docs", "fixtures", systemStringTailFixture)
	fixture, err := compat.LoadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Name != strings.TrimSuffix(systemStringTailFixture, ".json") || fixture.Command.Kind != "exec" || len(fixture.Command.Args) != 1 || len(fixture.Source) != 1 || fixture.Source[0].Content != fixture.Command.Args[0] {
		t.Fatalf("fixture execution envelope = %#v", fixture)
	}
	if result, err := compat.Run(fixture); err != nil || !result.OK {
		t.Fatalf("fixture execution = %#v, error = %v", result, err)
	}
	if len(fixture.Evidence) != len(systemStringTailIDs) || fixture.Evidence[0].SurfaceID != systemStringTailIDs[0] || fixture.Evidence[0].Kind != "exec" {
		t.Fatalf("fixture evidence = %#v", fixture.Evidence)
	}

	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	var metadata struct {
		APIVersion                string `json:"apiVersion"`
		Mode                      string `json:"mode"`
		Notes                     string `json:"notes"`
		EvidenceOnly              bool   `json:"evidenceOnly"`
		SalesforceEligible        *bool  `json:"salesforceEligible"`
		SalesforceExclusionClass  string `json:"salesforceExclusionClass"`
		SalesforceExclusionReason string `json:"salesforceExclusionReason"`
		Salesforce                any    `json:"salesforce"`
		Comparisons               any    `json:"comparisons"`
		Profile                   struct {
			CandidateCommit string `json:"candidateCommit"`
			CandidateSHA256 string `json:"candidateSha256"`
			SelectedRows    int    `json:"selectedRowCount"`
		} `json:"profile"`
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.APIVersion != "67.0" || metadata.Mode != "local-runtime" || metadata.EvidenceOnly || metadata.SalesforceEligible == nil || *metadata.SalesforceEligible || metadata.SalesforceExclusionClass != "policy-local-only" || !strings.Contains(metadata.SalesforceExclusionReason, "zero Salesforce parity") || metadata.Profile.CandidateCommit != "3409c4c85827b19712e9df83fc8905aa02bd1dc8" || metadata.Profile.CandidateSHA256 != "960ac9f26fa92aae6054cbe0e59f9c4ab1f84397df67bd8a89528068d02a1fce" || metadata.Profile.SelectedRows != len(systemStringTailIDs) || metadata.Salesforce != nil || metadata.Comparisons != nil || !strings.Contains(metadata.Notes, "no hosted Salesforce execution or parity claim") {
		t.Fatalf("fixture provenance = %#v", metadata)
	}

	evidence, err := BuildEvidenceSnapshot([]string{fixturePath})
	if err != nil {
		t.Fatal(err)
	}
	assertExactSurfaceSet(t, evidence, systemStringTailIDs)
	if evidence[0].Evidence != EvidenceFixture || evidence[0].GladeBehavior != BehaviorSupported || len(evidence[0].Sources) != 1 || evidence[0].Sources[0] != "fixture:"+strings.TrimSuffix(fixture.Name, ".json") {
		t.Fatalf("evidence row = %#v", evidence[0])
	}

	paths, err := filepath.Glob(filepath.Join(root, "docs", "fixtures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	owners := 0
	for _, path := range paths {
		var header struct {
			EvidenceOnly bool `json:"evidenceOnly"`
			Evidence     []struct {
				SurfaceID string `json:"surfaceId"`
			} `json:"evidence"`
		}
		readJSON(t, path, &header)
		if header.EvidenceOnly {
			continue
		}
		for _, row := range header.Evidence {
			if row.SurfaceID == systemStringTailIDs[0] {
				owners++
			}
		}
	}
	if owners != 1 {
		t.Fatalf("fixture ownership for %s = %d, want exactly one active owner", systemStringTailIDs[0], owners)
	}
}
