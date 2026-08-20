package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func searchCloseoutIDs() []string {
	ids := []string{"apex:Search.SuggestionResult", "apex:Search.SuggestionResults"}
	for _, typeName := range []string{"KnowledgeSuggestionFilter", "QuestionSuggestionFilter", "SuggestionOption"} {
		for _, member := range []string{"", "." + typeName + "()", ".equals(Object)", ".hashCode()", ".toString()"} {
			ids = append(ids, "apex:Search."+typeName+member)
		}
	}
	return ids
}

func TestSearchCloseoutHasExactExecutableOwnership(t *testing.T) {
	root := filepath.Join("..", "..")
	wantIDs := searchCloseoutIDs()
	if len(wantIDs) != 17 {
		t.Fatalf("Search closeout IDs = %d, want 17", len(wantIDs))
	}
	paths, err := filepath.Glob(filepath.Join(root, "docs", "fixtures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := BuildEvidenceSnapshot(paths)
	if err != nil {
		t.Fatal(err)
	}
	want := make(map[string]struct{}, len(wantIDs))
	for _, id := range wantIDs {
		want[id] = struct{}{}
	}
	var selected []SurfaceLedgerRow
	for _, row := range evidence {
		if _, ok := want[row.SurfaceID]; ok {
			selected = append(selected, row)
		}
	}
	assertExactSurfaceSet(t, selected, wantIDs)
	for _, row := range selected {
		if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported || len(row.Sources) != 1 || row.Sources[0] != "fixture:data-platform-search-result-suggestion-dtos" {
			t.Fatalf("%s evidence/behavior/source = %s/%s/%v", row.SurfaceID, row.Evidence, row.GladeBehavior, row.Sources)
		}
	}

	fixturePath := filepath.Join(root, "docs", "fixtures", "data-platform-search-result-suggestion-dtos.json")
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
	var source strings.Builder
	for _, file := range fixture.Source {
		source.WriteString(file.Content)
	}
	for _, witness := range []string{
		"Search.KnowledgeSuggestionFilter filter = new Search.KnowledgeSuggestionFilter();",
		"System.assert(filter.equals(filter));",
		"Search.QuestionSuggestionFilter questionFilter = new Search.QuestionSuggestionFilter();",
		"System.assert(questionFilter.equals(questionFilter));",
		"Search.SuggestionOption option = new Search.SuggestionOption();",
		"System.assert(option.equals(option));",
		"Search.SuggestionResult suggestionType = new Search.SuggestionResult();",
		"Search.SuggestionResults suggestionResultsType = new Search.SuggestionResults();",
	} {
		if !strings.Contains(source.String(), witness) {
			t.Fatalf("Search source missing %q", witness)
		}
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
	if policy.SalesforceEligible == nil || *policy.SalesforceEligible || policy.SalesforceExclusionClass != "org-configuration-required" || !strings.Contains(policy.SalesforceExclusionReason, "exclude hosted runtime") {
		t.Fatalf("fixture policy = %#v", policy)
	}
}
