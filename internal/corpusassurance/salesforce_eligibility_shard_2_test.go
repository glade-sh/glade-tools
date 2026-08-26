package corpusassurance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSalesforceEligibilityShard2FixturesAreExplicit(t *testing.T) {
	tests := []struct {
		id       string
		file     string
		eligible bool
	}{
		{"async-finalizer-context-getters", "async-finalizer-unsupported.json", false},
		{"core-blob-crypto-partial-encrypt-unsupported-decrypt-sign-verify", "core-blob-crypto-partial-encrypt-unsupported-decrypt-sign-verify.json", false},
		{"core-database-upsert-mode-runtime", "core-database-upsert-mode-runtime.json", true},
		{"core-feature-management", "core-feature-management.json", false},
		{"core-json-raw-runtime", "core-json-raw-runtime.json", false},
		{"core-pattern-dialect-flags-stdlib", "core-pattern-dialect-flags-stdlib.json", true},
		{"core-process-sparkplug-runtime", "core-process-sparkplug-runtime.json", true},
		{"core-runtime-cache-builder-evidence", "core-runtime-cache-builder-evidence.json", true},
		{"core-runtime-decimal-extra-accessors", "core-runtime-decimal-extra-accessors.json", true},
		{"core-runtime-exception-family-accessors", "core-runtime-exception-family-accessors.json", false},
		{"core-runtime-matcher-exact-evidence", "core-runtime-matcher-exact-evidence.json", true},
		{"core-runtime-messaging-mass-email-accessors-evidence", "core-runtime-messaging-mass-email-accessors-evidence.json", true},
		{"core-runtime-metadata-flow-approval-enum-contracts", "core-runtime-metadata-flow-approval-enum-contracts.json", true},
		{"core-runtime-page-reference-redirect-code-evidence", "core-runtime-page-reference-redirect-code-evidence.json", true},
		{"core-runtime-service-noops-and-random", "core-runtime-service-noops-and-random.json", false},
		{"core-runtime-system-operating-closeout", "core-runtime-system-operating-closeout.json", false},
		{"core-stdlib-supported-closeout", "core-stdlib-supported-closeout.json", true},
		{"core-string-more-stdlib", "core-string-more-stdlib.json", true},
		{"core-system-enum-types-runtime", "core-system-enum-types-runtime.json", true},
		{"core-xmlstreamreader-runtime-depth", "core-xmlstreamreader-runtime-depth.json", true},
		{"current-base-cb206-metadata-messaging-deterministic-api67", "current-base-cb206-metadata-messaging-deterministic-api67.json", true},
		{"data-database-delete-undelete-object-runtime", "data-database-delete-undelete-object-runtime.json", true},
		{"data-database-query-locator-access-runtime", "data-database-query-locator-access-runtime.json", true},
		{"data-platform-schema-describe-dependent-picklists", "data-platform-schema-describe-dependent-picklists.json", false},
		{"data-schema-describe-field-properties-runtime", "data-schema-describe-field-properties-runtime.json", true},
		{"integration-auth-token-local-mocks", "integration-auth-token-local-mocks.json", false},
		{"limits-publish-immediate-dml-unsupported", "limits-publish-immediate-dml-unsupported.json", true},
		{"test-helper-unsupported-fixed-search-results", "test-helper-unsupported-fixed-search-results.json", false},
	}

	if len(tests) != 28 {
		t.Fatalf("fixture count = %d, want 28", len(tests))
	}
	for _, test := range tests {
		t.Run(test.id, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("..", "..", "docs", "fixtures", test.file))
			if err != nil {
				t.Fatal(err)
			}
			fixture, metadata, err := decodeLocalProofFixtureWithMetadata(data)
			if err != nil {
				t.Fatal(err)
			}
			if fixture.Name != test.id {
				t.Fatalf("fixture name = %q, want %q", fixture.Name, test.id)
			}
			if metadata.Eligible == nil {
				t.Fatal("salesforceEligible is missing")
			}
			if *metadata.Eligible != test.eligible {
				t.Fatalf("salesforceEligible = %t, want %t", *metadata.Eligible, test.eligible)
			}
			if test.eligible {
				if metadata.ExclusionClass != "" || metadata.ExclusionReason != "" {
					t.Fatalf("eligible fixture carries exclusion %q: %q", metadata.ExclusionClass, metadata.ExclusionReason)
				}
				return
			}
			if metadata.ExclusionClass != "policy-local-only" || !strings.Contains(strings.ToLower(metadata.ExclusionReason), "zero salesforce parity") {
				t.Fatalf("local-only exclusion = %q: %q", metadata.ExclusionClass, metadata.ExclusionReason)
			}
		})
	}
}
