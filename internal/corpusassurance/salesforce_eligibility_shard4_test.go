package corpusassurance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func TestSalesforceEligibilityShard4FixturesHaveExplicitMetadata(t *testing.T) {
	tests := []struct {
		name     string
		eligible bool
	}{
		{"async-userprovisioning-batchable-lifecycle.json", false},
		{"core-cache-email-runtime.json", false},
		{"core-dml-exception-accessors-runtime.json", true},
		{"core-http-types-runtime.json", true},
		{"core-messaging-mass-email-fields-runtime.json", false},
		{"core-pattern-quote-stdlib.json", true},
		{"core-runtime-asyncinfo-context-unsupported.json", true},
		{"core-runtime-date-extra-accessors.json", true},
		{"core-runtime-double-accessors.json", true},
		{"core-runtime-exception-more-family-accessors.json", false},
		{"core-runtime-messaging-attachment-accessors-evidence.json", false},
		{"core-runtime-messaging-send-capture-full-local.json", false},
		{"core-runtime-page-reference-anchor-evidence.json", true},
		{"core-runtime-page-reference-resource-evidence.json", true},
		{"core-runtime-sobject-clone-source.json", false},
		{"core-runtime-system-stdlib-scalar-closeout.json", true},
		{"core-string-completion-stdlib.json", true},
		{"core-string-stdlib.json", true},
		{"core-type-exception-url-followup.json", false},
		{"current-base-cb188-schema-describe-positive-api67.json", true},
		{"data-database-convert-lead-runtime.json", false},
		{"data-database-dml-accesslevel-runtime.json", true},
		{"data-database-treesave-runtime.json", false},
		{"data-schema-child-relationship-aliases-runtime.json", false},
		{"data-schema-describe-token-edges-runtime.json", true},
		{"integration-eventbus-change-header-properties-runtime.json", false},
		{"test-helper-load-data-fixed-search-evidence.json", true},
	}

	trueCount := 0
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("..", "..", "docs", "fixtures", test.name))
			if err != nil {
				t.Fatal(err)
			}
			fixture, metadata, err := decodeLocalProofFixtureWithMetadata(data)
			if err != nil {
				t.Fatal(err)
			}
			if err := compat.Validate(fixture); err != nil {
				t.Fatal(err)
			}
			if metadata.Eligible == nil {
				t.Fatal("salesforceEligible is not explicit")
			}
			if *metadata.Eligible != test.eligible {
				t.Fatalf("salesforceEligible = %v, want %v", *metadata.Eligible, test.eligible)
			}
			if test.eligible {
				trueCount++
				return
			}
			if metadata.ExclusionClass != "policy-local-only" {
				t.Fatalf("salesforceExclusionClass = %q, want policy-local-only", metadata.ExclusionClass)
			}
			if !strings.Contains(metadata.ExclusionReason, "zero Salesforce parity") {
				t.Fatalf("salesforceExclusionReason = %q, want explicit zero Salesforce parity", metadata.ExclusionReason)
			}
		})
	}
	if trueCount != 15 || len(tests)-trueCount != 12 {
		t.Fatalf("classification counts = %d true/%d false, want 15/12", trueCount, len(tests)-trueCount)
	}
}
