package corpusassurance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func TestCacheTailHasOneExecutableLocalOwner(t *testing.T) {
	const filename = "current-base-cache-tail-deterministic-api67.json"
	path := filepath.Join("..", "..", "docs", "fixtures", filename)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := compat.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Command.Kind != "exec" || len(fixture.Command.Args) != 1 || len(fixture.Source) != 1 || fixture.Source[0].Path != "anonymous.apex" || fixture.Source[0].Content != fixture.Command.Args[0] {
		t.Fatalf("source/command provenance = %#v", fixture)
	}
	want := []string{
		"apex:Cache.Org.Org()",
		"apex:Cache.Session.Session()",
	}
	rejected := []string{
		"apex:Cache.OrgPartition.validateKeys(Boolean,List<String>)",
		"apex:Cache.OrgPartition.validateKeys(Boolean,Set<String>)",
		"apex:Cache.Partition.validateKeys(Boolean,List<String>)",
		"apex:Cache.Partition.validateKeys(Boolean,Set<String>)",
		"apex:Cache.SessionPartition.validateKeys(Boolean,List<String>)",
		"apex:Cache.SessionPartition.validateKeys(Boolean,Set<String>)",
	}
	got := make([]string, 0, len(fixture.Evidence))
	for _, evidence := range fixture.Evidence {
		if evidence.Kind != "exec" {
			t.Fatalf("evidence kind = %q", evidence.Kind)
		}
		got = append(got, evidence.SurfaceID)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("owned IDs = %v, want %v", got, want)
	}
	var metadata struct {
		Mode      string `json:"mode"`
		Candidate struct {
			Commit string `json:"commit"`
			SHA256 string `json:"sha256"`
		} `json:"candidate"`
		SalesforceEligible        *bool  `json:"salesforceEligible"`
		SalesforceExclusionClass  string `json:"salesforceExclusionClass"`
		SalesforceExclusionReason string `json:"salesforceExclusionReason"`
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.Mode != "deterministic-mock" || metadata.Candidate.Commit != "3409c4c85827b19712e9df83fc8905aa02bd1dc8" || metadata.Candidate.SHA256 != "960ac9f26fa92aae6054cbe0e59f9c4ab1f84397df67bd8a89528068d02a1fce" || metadata.SalesforceEligible == nil || *metadata.SalesforceEligible || metadata.SalesforceExclusionClass != "policy-local-only" || !strings.Contains(strings.ToLower(metadata.SalesforceExclusionReason), "zero salesforce parity") {
		t.Fatalf("fixture provenance = %#v", metadata)
	}
	root, err := filepath.Abs(filepath.Join("..", "..", "docs", "fixtures"))
	if err != nil {
		t.Fatal(err)
	}
	required := make(map[string]string, len(want)+len(rejected))
	for _, surfaceID := range append(append([]string{}, want...), rejected...) {
		required[surfaceID] = deterministicMockRequired
	}
	preManifest, preMissing, err := analyzeLocalProofFixtures(root, required)
	if err != nil {
		t.Fatal(err)
	}
	if len(preMissing) != len(rejected) || len(preManifest.Fixtures) != 1 || !reflect.DeepEqual(preManifest.Fixtures[0].OwnedSurfaceIDs, want) {
		t.Fatalf("pre-admission manifest = %#v, missing = %v", preManifest, preMissing)
	}
	if result, err := compat.Run(fixture); err != nil || !result.OK {
		t.Fatalf("fixture execution = %#v, error = %v", result, err)
	}
	negative, err := compat.LoadFile(filepath.Join("..", "..", "docs", "fixtures", "current-base-cache-negative-api67.json"))
	if err != nil {
		t.Fatal(err)
	}
	if negative.Expected.Error == nil || negative.Expected.Error.Type != "UnsupportedOperationException" || negative.Expected.Error.Message != "local stub surface" {
		t.Fatalf("unsupported Cache rejection = %#v", negative.Expected.Error)
	}
	negativeIDs := make([]string, 0, len(rejected))
	for _, evidence := range negative.Evidence {
		for _, surfaceID := range rejected {
			if evidence.SurfaceID == surfaceID {
				negativeIDs = append(negativeIDs, surfaceID)
			}
		}
	}
	if !reflect.DeepEqual(negativeIDs, rejected) {
		t.Fatalf("unsupported Cache IDs = %v, want %v", negativeIDs, rejected)
	}

	manifest, missing, err := analyzeLocalProofFixtures(root, required)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(missing, rejected) || len(manifest.Fixtures) != 1 || manifest.Fixtures[0].ID != fixture.Name || !reflect.DeepEqual(manifest.Fixtures[0].OwnedSurfaceIDs, want) {
		t.Fatalf("admission manifest = %#v, missing = %v", manifest, missing)
	}
}
