package corpusassurance

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSalesforceEligibilityShard1FixturesAreExplicit(t *testing.T) {
	tests := []struct {
		file, class, reason string
		eligible            bool
	}{
		{"async-execute-batch-scope-validation.json", "policy-local-only", "The anonymous command references an unmaterialized BatchWorker and asserts Glade's local scope-validation error; this fixture is policy-local-only and grants zero Salesforce parity credit.", false},
		{"core-blob-crypto-aes-key.json", "policy-local-only", "The command asserts a fixed AES key, while Salesforce generates nondeterministic key bytes; this fixture is policy-local-only and grants zero Salesforce parity credit.", false},
		{"core-collection-stdlib-sobject-deepclone.json", "", "", true},
		{"core-dom-xml-runtime-depth.json", "policy-local-only", "The command directly constructs Dom.XmlNode, which Salesforce API 67 does not permit; this fixture is policy-local-only and grants zero Salesforce parity credit.", false},
		{"core-integer-valueof-object-runtime.json", "", "", true},
		{"core-messaging-sendemail-error-fields-runtime.json", "", "", true},
		{"core-pattern-split-nullable-stdlib.json", "", "", true},
		{"core-runtime-businesshours-license-local-evidence.json", "policy-local-only", "The command relies on synthetic BusinessHours and package-license IDs that the adapter does not materialize in Salesforce; this fixture is policy-local-only and grants zero Salesforce parity credit.", false},
		{"core-runtime-datetime-decimal-extra.json", "policy-local-only", "The command assumes Glade's fixed GMT and en-US context for Datetime.newInstance, format, and parse; this fixture is policy-local-only and grants zero Salesforce parity credit.", false},
		{"core-runtime-exception-accessors.json", "policy-local-only", "The command asserts Glade-local source locations and stack traces for constructed AssertException and AsyncException values; this fixture is policy-local-only and grants zero Salesforce parity credit.", false},
		{"core-runtime-invocable-process-plugin-constructors.json", "policy-local-only", "The test exercises deterministic local Process.PluginDescribeResult parameter DTO construction rather than a hosted process invocation; it grants zero Salesforce parity credit.", false},
		{"core-runtime-messaging-email-base-accessors-evidence.json", "policy-local-only", "The test depends on Glade-local Messaging.Email public fields and object identity, hash, and string behavior; it grants zero Salesforce parity credit.", false},
		{"core-runtime-metadata-deploy-dto-api67.json", "", "", true},
		{"core-runtime-page-reference-cookies-evidence.json", "", "", true},
		{"core-runtime-select-option-accessors.json", "", "", true},
		{"core-runtime-string-template.json", "", "", true},
		{"core-runtime-xmlstreamwriter-evidence.json", "", "", true},
		{"core-string-entity-edge-stdlib.json", "", "", true},
		{"core-system-boolean-exact-evidence.json", "", "", true},
		{"core-url-current-request.json", "policy-local-only", "The command asserts Glade's synthetic local request URL rather than a Salesforce request-context URL; this fixture is policy-local-only and grants zero Salesforce parity credit.", false},
		{"current-base-cb190-schema-broad-positive-api67.json", "", "", true},
		{"data-database-delete-undelete-id-runtime.json", "", "", true},
		{"data-database-empty-recycle-bin-runtime.json", "", "", true},
		{"data-platform-schema-describe-data-categories.json", "org-configuration-required", "The fixture requires synthetic data-category metadata that the adapter does not materialize into the fixed Salesforce org; it grants zero Salesforce parity credit.", false},
		{"data-schema-child-relationships-runtime.json", "org-configuration-required", "The fixture requires synthetic polymorphic custom-lookup metadata that is not portable to the fixed Salesforce org; it grants zero Salesforce parity credit.", false},
		{"integration-auth-session-current.json", "policy-local-only", "The command asserts Glade's synthetic session sentinel instead of a real Salesforce session value; this fixture is policy-local-only and grants zero Salesforce parity credit.", false},
		{"integration-pagereference-accessors-runtime.json", "", "", true},
		{"test-helper-unsupported-create-stub.json", "policy-local-only", "The command asserts Glade's local UnsupportedFeature result from an anonymous-only Test.createStub call; this fixture is policy-local-only and grants zero Salesforce parity credit.", false},
	}
	if len(tests) != 28 {
		t.Fatalf("fixture count = %d, want 28", len(tests))
	}
	for _, test := range tests {
		t.Run(test.file, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("..", "..", "docs", "fixtures", test.file))
			if err != nil {
				t.Fatal(err)
			}
			_, metadata, err := decodeLocalProofFixtureWithMetadata(data)
			if err != nil {
				t.Fatal(err)
			}
			if metadata.Eligible == nil {
				t.Error("salesforceEligible is not explicit")
				return
			}
			if *metadata.Eligible != test.eligible || metadata.ExclusionClass != test.class || metadata.ExclusionReason != test.reason {
				t.Errorf("metadata = (%t, %q, %q), want (%t, %q, %q)", *metadata.Eligible, metadata.ExclusionClass, metadata.ExclusionReason, test.eligible, test.class, test.reason)
			}
		})
	}
}
