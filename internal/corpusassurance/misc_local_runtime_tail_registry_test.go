package corpusassurance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
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
		"apex:Schema.DescribeDataCategoryGroupResult":          "sealed candidate has no supported executable constructor witness",
		"apex:Schema.DescribeDataCategoryGroupStructureResult": "sealed candidate has no supported executable constructor witness",
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
		"apex:System.SObject.getSObjects(Schema.SObjectField)": "dropped: the supported fixture cannot populate a child relationship through a Schema.SObjectField token",
	}
	if len(rejected) != 15 {
		t.Fatalf("rejected rows = %d, want exact 15: %#v", len(rejected), rejected)
	}
	if len(required)+len(rejected) != 26 {
		t.Fatalf("scoped rows = %d, want exact 26", len(required)+len(rejected))
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
	if len(manifest.Fixtures) != 1 {
		t.Fatalf("selected fixtures = %#v, want one exact owner", manifest.Fixtures)
	}
	fixture := manifest.Fixtures[0]
	got := append([]string(nil), fixture.OwnedSurfaceIDs...)
	sort.Strings(got)
	want := make([]string, 0, len(required))
	for surfaceID := range required {
		want = append(want, surfaceID)
	}
	sort.Strings(want)
	if fixture.ID != "core-runtime-misc-local-runtime-tail-api67" || fixture.Disposition != localRuntimeRequired || !reflect.DeepEqual(got, want) || fixture.SalesforceEligible == nil || *fixture.SalesforceEligible || fixture.SalesforceExclusionClass != "policy-local-only" {
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
	if len(decoded.Source) != 1 || decoded.Source[0].Path != "anonymous.apex" || len(decoded.Command.Args) != 1 || decoded.Command.Args[0] != decoded.Source[0].Content {
		t.Fatal("anonymous source and command diverged")
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
	if envelope.Candidate.Commit != "3409c4c85827b19712e9df83fc8905aa02bd1dc8" || envelope.Candidate.SHA256 != "960ac9f26fa92aae6054cbe0e59f9c4ab1f84397df67bd8a89528068d02a1fce" || envelope.Profile.CandidateCommit != envelope.Candidate.Commit || envelope.Profile.CandidateSHA256 != envelope.Candidate.SHA256 || envelope.Profile.SelectedRowCount != len(required) || metadata.Eligible == nil || *metadata.Eligible || metadata.ExclusionClass != "policy-local-only" {
		t.Fatalf("sealed provenance/metadata = %#v/%#v/%#v", envelope.Candidate, envelope.Profile, metadata)
	}
}
