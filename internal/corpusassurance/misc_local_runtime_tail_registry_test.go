package corpusassurance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestMiscLocalRuntimeTailIsSelectedByLocalProofRegistry(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "docs", "fixtures"))
	if err != nil {
		t.Fatal(err)
	}
	required := map[string]string{
		"apex-language:TypeResolutionSystemNamespace":                         localRuntimeRequired,
		"apex:Schema.DataCategory":                                            localRuntimeRequired,
		"apex:Schema.DataCategory.DataCategory()":                             localRuntimeRequired,
		"apex:Schema.DescribeDataCategoryGroupResult":                         localRuntimeRequired,
		"apex:Schema.DescribeDataCategoryGroupStructureResult":                localRuntimeRequired,
		"apex:System.ApexPages.ApexPages()":                                   localRuntimeRequired,
		"apex:System.Assert.Assert()":                                         localRuntimeRequired,
		"apex:System.Assert.clone()":                                          localRuntimeRequired,
		"apex:System.AsyncOptions.setMinimumQueueableDelayInMinutes(Integer)": localRuntimeRequired,
		"apex:System.Iterable.iterator()":                                     localRuntimeRequired,
		"apex:System.Object":                                                  localRuntimeRequired,
		"apex:System.Pattern":                                                 localRuntimeRequired,
		"apex:System.TimeZone":                                                localRuntimeRequired,
	}
	// These are the rows from the 8080/8216 receipt still rejected after later
	// exact evidence closed other rows. Keep reasons here so a fixture change
	// cannot silently turn unsupported behavior into local-proof credit.
	rejected := map[string]string{
		"apex-language:NamespaceClassVariablePrecedence":       "compat run rejects the source-class method path; no direct anonymous witness is claimed",
		"apex:System.Callable.call(String,Map<String,Object>)": "requires a user class implementation; standard fixture execution cannot register source classes",
		"apex:System.Comparable":                               "requires a user class implementation; standard fixture execution cannot register source classes",
		"apex:System.Comparable.compareTo(Object)":             "requires a user class implementation; standard fixture execution cannot register source classes",
		"apex:System.Comparator":                               "requires a user class implementation; standard fixture execution cannot register source classes",
		"apex:System.Comparator.compare(Object,Object)":        "requires a user class implementation; standard fixture execution cannot register source classes",
		"apex:System.Date.Date()":                              "Salesforce and the current candidate reject this scalar constructor",
		"apex:System.Datetime.Datetime()":                      "Salesforce and the current candidate reject this scalar constructor",
		"apex:System.Decimal.Decimal()":                        "Salesforce and the current candidate reject this scalar constructor",
		"apex:System.Double.Double()":                          "Salesforce and the current candidate reject this scalar constructor",
		"apex:System.Id.to18":                                  "sealed candidate does not execute this conversion contract",
		"apex:System.Integer.doubleValue":                      "sealed candidate does not execute this conversion contract",
	}
	if len(rejected) != 12 {
		t.Fatalf("rejected rows = %d, want exact 12: %#v", len(rejected), rejected)
	}
	if len(required)+len(rejected) != 25 {
		t.Fatalf("scoped rows = %d, want exact 25", len(required)+len(rejected))
	}
	for surfaceID, reason := range rejected {
		if reason == "" {
			t.Fatalf("rejected surface %q has no reason", surfaceID)
		}
	}
	const attachFinalizer = "apex:System.System.attachFinalizer(finalizer)"
	if _, ok := required[attachFinalizer]; ok {
		t.Fatal("attachFinalizer is covered by PR192 and must remain outside this lane")
	}
	if _, ok := rejected[attachFinalizer]; ok {
		t.Fatal("attachFinalizer is covered by PR192 and must remain outside the rejected set")
	}
	for surfaceID := range required {
		if _, ok := rejected[surfaceID]; ok {
			t.Fatalf("surface %q is both accepted and rejected", surfaceID)
		}
	}
	manifest, missing, err := analyzeLocalProofFixtures(root, required)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 0 {
		t.Fatalf("registry missing = %v", missing)
	}
	if len(manifest.Fixtures) != 2 {
		t.Fatalf("selected fixtures = %#v, want two exact owners", manifest.Fixtures)
	}
	for _, fixture := range manifest.Fixtures {
		got := append([]string(nil), fixture.OwnedSurfaceIDs...)
		sort.Strings(got)
		if fixture.Disposition != localRuntimeRequired || fixture.SalesforceEligible == nil || *fixture.SalesforceEligible || fixture.SalesforceExclusionClass != "policy-local-only" {
			t.Fatalf("selected owner = %#v", fixture)
		}
		for _, surfaceID := range fixture.OwnedSurfaceIDs {
			if reason, ok := rejected[surfaceID]; ok {
				t.Fatalf("rejected surface %q was credited: %s", surfaceID, reason)
			}
		}
		path := filepath.Join(root, fixture.ID+".json")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		decoded, metadata, err := decodeLocalProofFixtureWithMetadata(data)
		if err != nil {
			t.Fatal(err)
		}
		reason := strings.ToLower(metadata.ExclusionReason)
		if metadata.Eligible == nil || *metadata.Eligible || metadata.ExclusionClass != "policy-local-only" || !strings.Contains(reason, "zero") || !strings.Contains(reason, "salesforce") || !strings.Contains(reason, "parity") {
			t.Fatalf("sealed metadata = %#v", metadata)
		}
		var envelope struct {
			Candidate struct {
				Commit string `json:"commit"`
				SHA256 string `json:"sha256"`
			} `json:"candidate"`
			Profile struct {
				CandidateCommit  string `json:"candidateCommit"`
				CandidateSHA256  string `json:"candidateSha256"`
				SelectedRowCount int    `json:"selectedRowCount"`
			} `json:"profile"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.Candidate.Commit == "" || envelope.Candidate.SHA256 == "" || envelope.Profile.CandidateCommit != envelope.Candidate.Commit || envelope.Profile.CandidateSHA256 != envelope.Candidate.SHA256 || envelope.Profile.SelectedRowCount < len(got) {
			t.Fatalf("sealed provenance = %#v", envelope)
		}
		if fixture.ID == "core-runtime-local-metadata-search-evidence" && (envelope.Candidate.Commit != "86ec4226e33f205bf7a42f6f00cc40aa57fc11b5" || envelope.Candidate.SHA256 != "0aa758618a8908550aa468c4c9eabd1fcdd06f9f6a7d317ccce45a077380d29a") {
			t.Fatalf("metadata candidate binding = %#v", envelope.Candidate)
		}
		if fixture.ID == "core-runtime-misc-local-runtime-tail-api67" {
			if len(decoded.Source) != 1 || decoded.Source[0].Path != "anonymous.apex" || len(decoded.Command.Args) != 1 || decoded.Command.Args[0] != decoded.Source[0].Content {
				t.Fatal("anonymous source and command diverged")
			}
		} else if fixture.ID != "core-runtime-local-metadata-search-evidence" || len(decoded.Source) == 0 || decoded.Command.Kind != "test" || len(decoded.Command.Args) != 0 {
			t.Fatalf("metadata owner = %#v", fixture)
		}
	}
	sobjectManifest, sobjectMissing, err := analyzeLocalProofFixtures(root, map[string]string{
		"apex:System.SObject.getSObjects(Schema.SObjectField)": localRuntimeRequired,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(sobjectMissing) != 0 || len(sobjectManifest.Fixtures) != 1 || sobjectManifest.Fixtures[0].ID != "core-runtime-sobject-tail-api67" || !reflect.DeepEqual(sobjectManifest.Fixtures[0].OwnedSurfaceIDs, []string{"apex:System.SObject.getSObjects(Schema.SObjectField)"}) {
		t.Fatalf("SObject field-token local owner = %#v, missing = %v", sobjectManifest, sobjectMissing)
	}
}
