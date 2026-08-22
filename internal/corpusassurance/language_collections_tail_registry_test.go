package corpusassurance

import (
	"path/filepath"
	"testing"
)

func TestLanguageCollectionsTailIsSelectedByLocalProofRegistry(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "docs", "fixtures"))
	if err != nil {
		t.Fatal(err)
	}
	required := map[string]string{
		"apex-language:SchemaNamespaceImplicitImport": localRuntimeRequired,
		"apex:System.Iterable":                        localRuntimeRequired,
		"apex:System.JSON.createParser":               localRuntimeRequired,
		"apex:System.List":                            localRuntimeRequired,
		"apex:System.Matcher":                         localRuntimeRequired,
		"apex:System.Matcher.clone()":                 localRuntimeRequired,
		"apex:System.Pattern.clone()":                 localRuntimeRequired,
		"apex:System.Set":                             localRuntimeRequired,
		"apex:System.Type.clone()":                    localRuntimeRequired,
	}
	manifest, missing, err := analyzeLocalProofFixtures(root, required)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 0 {
		t.Fatalf("registry missing = %v", missing)
	}
	if len(manifest.Fixtures) != 1 {
		t.Fatalf("selected fixtures = %#v, want one exact owner", manifest.Fixtures)
	}
	fixture := manifest.Fixtures[0]
	if fixture.ID != "core-language-collections-tail-local-api67" || fixture.Disposition != localRuntimeRequired || len(fixture.OwnedSurfaceIDs) != len(required) || fixture.SalesforceEligible == nil || *fixture.SalesforceEligible || fixture.SalesforceExclusionClass != "policy-local-only" {
		t.Fatalf("selected owner = %#v", fixture)
	}
	for _, surfaceID := range fixture.OwnedSurfaceIDs {
		delete(required, surfaceID)
	}
	if len(required) != 0 {
		t.Fatalf("selected owner omitted = %v", required)
	}
}
