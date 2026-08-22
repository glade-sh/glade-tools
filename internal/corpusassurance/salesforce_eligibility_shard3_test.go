package corpusassurance

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestSalesforceEligibilityShard3FixturesAreExplicitlyClassified(t *testing.T) {
	type classification struct {
		name           string
		eligible       bool
		exclusionClass string
	}
	fixtures := map[string]classification{
		"async-test-semantics.json":                                 {"async-test-semantics", false, "policy-local-only"},
		"core-blob-crypto-stdlib.json":                              {"core-blob-crypto-stdlib", true, ""},
		"core-database-upsert-sobjectfield-runtime.json":            {"core-database-upsert-sobjectfield-runtime", false, "org-configuration-required"},
		"core-http-request-runtime-depth.json":                      {"core-http-request-runtime-depth", false, "policy-local-only"},
		"core-json-stdlib.json":                                     {"core-json-stdlib", false, "policy-local-only"},
		"core-pattern-matcher-stdlib.json":                          {"core-pattern-matcher-stdlib", true, ""},
		"core-runtime-accesslevel-permission-set-unsupported.json":  {"core-runtime-accesslevel-permission-set-local", false, "org-configuration-required"},
		"core-runtime-cb72-frozen-behavior-local-evidence.json":     {"core-runtime-cb72-frozen-behavior-local-evidence", false, "policy-local-only"},
		"core-runtime-domain-cookie-value-objects.json":             {"core-runtime-domain-cookie-value-objects", false, "policy-local-only"},
		"core-runtime-exception-more-accessors.json":                {"core-runtime-exception-more-accessors", false, "policy-local-only"},
		"core-runtime-math-location-exact-evidence.json":            {"core-runtime-math-location-exact-evidence", false, "policy-local-only"},
		"core-runtime-messaging-notification-handler-evidence.json": {"core-runtime-messaging-notification-handler-evidence", true, ""},
		"core-runtime-metadata-status-code-constants-api67-05.json": {"core-runtime-metadata-status-code-constants-api67-05", true, ""},
		"core-runtime-page-reference-redirect-evidence.json":        {"core-runtime-page-reference-redirect-evidence", true, ""},
		"core-runtime-small-helper-value-objects.json":              {"core-runtime-small-helper-value-objects", false, "policy-local-only"},
		"core-runtime-system-stdlib-cut3-evidence.json":             {"core-runtime-system-stdlib-cut3-evidence", true, ""},
		"core-string-abbreviate-offset-runtime.json":                {"core-string-abbreviate-offset-runtime", true, ""},
		"core-string-search-between-stdlib.json":                    {"core-string-search-between-stdlib", true, ""},
		"core-system-string-exact-evidence.json":                    {"core-system-string-exact-evidence", true, ""},
		"current-base-cb185-database-schema-positive-api67.json":    {"current-base-cb185-database-schema-positive-api67", true, ""},
		"current-base-system-exception-constructor-probe.json":      {"current-base-system-exception-constructor-probe", false, "policy-local-only"},
		"data-database-deleted-window-runtime.json":                 {"data-database-deleted-window-runtime", true, ""},
		"data-database-savepoint-lifecycle-runtime.json":            {"data-database-savepoint-lifecycle-runtime", true, ""},
		"data-platform-sobjectfield-describe-runtime.json":          {"data-platform-sobjectfield-describe-runtime", true, ""},
		"data-schema-describe-sobject-properties-runtime.json":      {"data-schema-describe-sobject-properties-runtime", true, ""},
		"integration-eventbus-change-header-accessors-runtime.json": {"integration-eventbus-change-header-accessors-runtime", false, "policy-local-only"},
		"test-helper-clear-messages-requires-test-context.json":     {"test-helper-clear-messages-requires-test-context", false, "policy-local-only"},
		"ui-apexpages-component-runtime-depth.json":                 {"ui-apexpages-component-runtime-depth", false, "policy-local-only"},
	}

	root := filepath.Join("..", "..", "docs", "fixtures")
	missing := make([]string, 0)
	for filename, want := range fixtures {
		data, err := os.ReadFile(filepath.Join(root, filename))
		if err != nil {
			t.Fatal(err)
		}
		fixture, metadata, err := decodeLocalProofFixtureWithMetadata(data)
		if err != nil {
			t.Fatalf("%s: %v", filename, err)
		}
		if fixture.Name != want.name {
			t.Errorf("%s: fixture name = %q, want %q", filename, fixture.Name, want.name)
		}
		if metadata.Eligible == nil {
			missing = append(missing, filename)
			continue
		}
		if *metadata.Eligible != want.eligible {
			t.Errorf("%s: salesforceEligible = %t, want %t", filename, *metadata.Eligible, want.eligible)
		}
		if metadata.ExclusionClass != want.exclusionClass {
			t.Errorf("%s: salesforceExclusionClass = %q, want %q", filename, metadata.ExclusionClass, want.exclusionClass)
		}
		if want.eligible && metadata.ExclusionReason != "" {
			t.Errorf("%s: eligible fixture has Salesforce exclusion reason %q", filename, metadata.ExclusionReason)
		}
		if !want.eligible && !strings.Contains(metadata.ExclusionReason, "zero Salesforce parity") {
			t.Errorf("%s: exclusion reason must state zero Salesforce parity", filename)
		}
	}
	if len(missing) != 0 {
		sort.Strings(missing)
		t.Fatalf("missing explicit Salesforce metadata for %d fixtures: %s", len(missing), strings.Join(missing, ", "))
	}
}
