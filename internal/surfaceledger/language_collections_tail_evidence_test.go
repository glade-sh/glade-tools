package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

var languageCollectionsTailIDs = []string{
	"apex-language:SchemaNamespaceImplicitImport",
	"apex:System.Iterable",
	"apex:System.JSON.createParser",
	"apex:System.List",
	"apex:System.Matcher",
	"apex:System.Matcher.clone()",
	"apex:System.Pattern.clone()",
	"apex:System.Set",
	"apex:System.Type.clone()",
}

var languageCollectionsTailRejected = map[string]string{
	"apex:System.List.List(Integer)": "candidate execution fails with cannot assign integer to String; excluded as a masking failure",
}

func TestLanguageCollectionsTailHasExactExecutableLocalEvidence(t *testing.T) {
	root := filepath.Join("..", "..")
	fixtureName := "core-language-collections-tail-local-api67.json"
	path := filepath.Join(root, "docs", "fixtures", fixtureName)
	fixture, err := compat.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Command.Kind != "exec" || len(fixture.Source) != 1 || len(fixture.Evidence) != len(languageCollectionsTailIDs) {
		t.Fatalf("fixture envelope = %#v", fixture)
	}
	want := make(map[string]bool, len(languageCollectionsTailIDs))
	for _, id := range languageCollectionsTailIDs {
		want[id] = true
	}
	seen := map[string]bool{}
	for _, evidence := range fixture.Evidence {
		if !want[evidence.SurfaceID] || evidence.Kind != "exec" || seen[evidence.SurfaceID] {
			t.Fatalf("fixture evidence = %#v", evidence)
		}
		seen[evidence.SurfaceID] = true
	}
	if len(seen) != len(want) {
		t.Fatalf("fixture evidence IDs = %#v, want %#v", seen, want)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var metadata struct {
		APIVersion         string `json:"apiVersion"`
		Mode               string `json:"mode"`
		Notes              string `json:"notes"`
		EvidenceOnly       bool   `json:"evidenceOnly"`
		SalesforceEligible *bool  `json:"salesforceEligible"`
		ExclusionClass     string `json:"salesforceExclusionClass"`
		ExclusionReason    string `json:"salesforceExclusionReason"`
		Salesforce         any    `json:"salesforce"`
		Comparisons        any    `json:"comparisons"`
		Candidate          struct {
			Commit string `json:"commit"`
			SHA256 string `json:"sha256"`
		} `json:"candidate"`
		Profile struct {
			CandidateCommit string `json:"candidateCommit"`
			CandidateSHA256 string `json:"candidateSha256"`
			SelectedRows    int    `json:"selectedRowCount"`
		} `json:"profile"`
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.APIVersion != "67.0" || metadata.Mode != "local-runtime" || metadata.EvidenceOnly || metadata.SalesforceEligible == nil || *metadata.SalesforceEligible || metadata.ExclusionClass != "policy-local-only" || !strings.Contains(metadata.ExclusionReason, "zero hosted Salesforce parity") || metadata.Candidate.Commit != "3409c4c85827b19712e9df83fc8905aa02bd1dc8" || metadata.Candidate.SHA256 != "960ac9f26fa92aae6054cbe0e59f9c4ab1f84397df67bd8a89528068d02a1fce" || metadata.Profile.CandidateCommit != metadata.Candidate.Commit || metadata.Profile.CandidateSHA256 != metadata.Candidate.SHA256 || metadata.Profile.SelectedRows != len(want) || metadata.Salesforce != nil || metadata.Comparisons != nil || !strings.Contains(metadata.Notes, "deterministic local") {
		t.Fatalf("fixture provenance = %#v", metadata)
	}
	if result, err := compat.Run(fixture); err != nil || !result.OK {
		t.Fatalf("fixture execution = %#v, error = %v", result, err)
	}

	paths, err := filepath.Glob(filepath.Join(root, "docs", "fixtures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	owners := map[string]int{}
	activeRows := map[string]int{}
	for _, candidatePath := range paths {
		var header struct {
			EvidenceOnly bool `json:"evidenceOnly"`
			Evidence     []struct {
				SurfaceID string `json:"surfaceId"`
			} `json:"evidence"`
		}
		readJSON(t, candidatePath, &header)
		if header.EvidenceOnly {
			continue
		}
		for _, evidence := range header.Evidence {
			activeRows[evidence.SurfaceID]++
			if want[evidence.SurfaceID] {
				owners[evidence.SurfaceID]++
			}
		}
	}
	for id := range want {
		if owners[id] != 1 {
			t.Fatalf("fixture ownership for %s = %d, want exactly one", id, owners[id])
		}
	}
	for id := range languageCollectionsTailRejected {
		if activeRows[id] != 0 {
			t.Fatalf("rejected active fixture ownership for %s = %d, want zero", id, activeRows[id])
		}
	}
}
