package corpusassurance

import (
	"path/filepath"
	"testing"
)

func TestLanguageContractRuntimeFixturesAreSelected(t *testing.T) {
	required := map[string]string{
		"apex-language:NamespaceClassVariablePrecedence":       localRuntimeRequired,
		"apex:System.Callable.call(String,Map<String,Object>)": localRuntimeRequired,
		"apex:System.Comparable":                               localRuntimeRequired,
		"apex:System.Comparable.compareTo(Object)":             localRuntimeRequired,
		"apex:System.Comparator":                               localRuntimeRequired,
		"apex:System.Comparator.compare(Object,Object)":        localRuntimeRequired,
	}
	root, err := filepath.Abs(filepath.Join("..", "..", "docs", "fixtures"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, missing, err := analyzeLocalProofFixtures(root, required)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 0 {
		t.Fatalf("registry missing = %v", missing)
	}
	wantFixtures := map[string]int{
		"core-runtime-namespace-variable-precedence-api67": 1,
		"core-runtime-callable-interface-dispatch":         1,
		"core-runtime-comparable-comparator-dispatch":      4,
	}
	seen := map[string]int{}
	for _, fixture := range manifest.Fixtures {
		if wantFixtures[fixture.ID] == 0 || fixture.Operation != "test" || fixture.Disposition != localRuntimeRequired || fixture.SalesforceEligible == nil || *fixture.SalesforceEligible || fixture.SalesforceExclusionClass != "policy-local-only" {
			t.Fatalf("selected fixture = %#v", fixture)
		}
		seen[fixture.ID] = len(fixture.OwnedSurfaceIDs)
	}
	for fixture, count := range wantFixtures {
		if seen[fixture] != count {
			t.Errorf("%s selected rows = %d, want %d", fixture, seen[fixture], count)
		}
	}
}
