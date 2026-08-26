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

func TestCacheContainerConstructorsAreTerminalNegativeEvidence(t *testing.T) {
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
	if fixture.Command.Kind != "check" || len(fixture.Source) != 1 || fixture.Source[0].Path != "force-app/main/default/classes/CacheContainerConstructorNegative.cls" {
		t.Fatalf("source/command provenance = %#v", fixture)
	}
	want := []string{
		"apex:Cache.Org.Org()",
		"apex:Cache.Session.Session()",
	}
	accepted := []string{
		"apex:Cache.OrgPartition.validateKeys(Boolean,Set<String>)",
		"apex:Cache.Partition.validateKeys(Boolean,Set<String>)",
		"apex:Cache.SessionPartition.validateKeys(Boolean,Set<String>)",
	}
	rejected := []string{
		"apex:Cache.OrgPartition.validateKeys(Boolean,List<String>)",
		"apex:Cache.Partition.validateKeys(Boolean,List<String>)",
		"apex:Cache.SessionPartition.validateKeys(Boolean,List<String>)",
	}
	got := make([]string, 0, len(fixture.Evidence))
	for _, evidence := range fixture.Evidence {
		if evidence.Kind != "check" {
			t.Fatalf("evidence kind = %q", evidence.Kind)
		}
		got = append(got, evidence.SurfaceID)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("owned IDs = %v, want %v", got, want)
	}
	var metadata struct {
		Mode                      string `json:"mode"`
		EvidenceOnly              bool   `json:"evidenceOnly"`
		SalesforceEligible        *bool  `json:"salesforceEligible"`
		SalesforceExclusionClass  string `json:"salesforceExclusionClass"`
		SalesforceExclusionReason string `json:"salesforceExclusionReason"`
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.Mode != "compile-shape" || !metadata.EvidenceOnly || metadata.SalesforceEligible == nil || *metadata.SalesforceEligible || metadata.SalesforceExclusionClass != "policy-local-only" || !strings.Contains(strings.ToLower(metadata.SalesforceExclusionReason), "zero salesforce parity") {
		t.Fatalf("fixture provenance = %#v", metadata)
	}
	root, err := filepath.Abs(filepath.Join("..", "..", "docs", "fixtures"))
	if err != nil {
		t.Fatal(err)
	}
	required := make(map[string]string, len(want)+len(accepted)+len(rejected))
	for _, surfaceID := range append(append(append([]string{}, want...), accepted...), rejected...) {
		required[surfaceID] = deterministicMockRequired
	}
	preManifest, preMissing, err := analyzeLocalProofFixtures(root, required)
	if err != nil {
		t.Fatal(err)
	}
	if len(preMissing) != len(want)+len(rejected) || len(preManifest.Fixtures) != 1 {
		t.Fatalf("pre-admission manifest = %#v, missing = %v", preManifest, preMissing)
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
	if len(missing) != len(want)+len(rejected) || len(manifest.Fixtures) != 1 || manifest.Fixtures[0].ID != "core-runtime-cache-validatekeys-set-api67" || !reflect.DeepEqual(manifest.Fixtures[0].OwnedSurfaceIDs, accepted) {
		t.Fatalf("admission manifest = %#v, missing = %v", manifest, missing)
	}
}

func TestCacheSObjectRowsRunSealedCandidateCLIJSON(t *testing.T) {
	candidatePath := os.Getenv("GLADE_CANDIDATE")
	if candidatePath == "" {
		t.Skip("set GLADE_CANDIDATE to run the sealed-candidate regression")
	}
	const candidateSHA = "0aa758618a8908550aa468c4c9eabd1fcdd06f9f6a7d317ccce45a077380d29a"
	if !filepath.IsAbs(candidatePath) || localProofFileSHA256(t, candidatePath) != candidateSHA {
		t.Fatalf("candidate is not the sealed runtime: %q", candidatePath)
	}
	root, err := filepath.Abs(filepath.Join("..", "..", "docs", "fixtures"))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		ids  []string
	}{
		{
			name: "core-runtime-cache-validatekeys-set-api67",
			ids: []string{
				"apex:Cache.OrgPartition.validateKeys(Boolean,Set<String>)",
				"apex:Cache.Partition.validateKeys(Boolean,Set<String>)",
				"apex:Cache.SessionPartition.validateKeys(Boolean,Set<String>)",
			},
		},
		{
			name: "core-runtime-sobject-tail-api67",
			ids:  []string{"apex:System.SObject.getSObjects(Schema.SObjectField)"},
		},
	}
	for _, tc := range cases {
		path := filepath.Join(root, tc.name+".json")
		data, err := os.ReadFile(path)
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
		entry := LocalProofFixture{
			ID: tc.name, Name: tc.name, Path: path, SHA256: replayBytesSHA256(data),
			OwnedSurfaceIDs: tc.ids, Disposition: localRuntimeRequired, Operation: "exec",
			SalesforceEligible: metadata.Eligible, SalesforceExclusionClass: metadata.ExclusionClass, SalesforceExclusionReason: metadata.ExclusionReason,
		}
		command, cleanup, err := materializeLocalProofFixture(entry, candidatePath)
		if err != nil {
			t.Fatal(err)
		}
		execution := runLocalProofCommand(command)
		cleanup()
		if !execution.Validated {
			t.Fatalf("sealed candidate execution failed for %s: exit=%d stderr=%s stdout=%s", tc.name, execution.Receipt.ExitCode, execution.Stderr, execution.Stdout)
		}
	}
}
