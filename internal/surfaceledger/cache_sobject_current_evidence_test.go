package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

const (
	cacheSObjectCandidateCommit = "86ec4226e33f205bf7a42f6f00cc40aa57fc11b5"
	cacheSObjectCandidateSHA    = "0aa758618a8908550aa468c4c9eabd1fcdd06f9f6a7d317ccce45a077380d29a"
)

func TestCacheSObjectCurrentEvidenceHasExactOwnershipAndAssertions(t *testing.T) {
	root := filepath.Join("..", "..", "docs", "fixtures")
	fixtures := map[string][]string{
		"core-runtime-cache-validatekeys-set-api67.json": {
			"apex:Cache.OrgPartition.validateKeys(Boolean,Set<String>)",
			"apex:Cache.Partition.validateKeys(Boolean,Set<String>)",
			"apex:Cache.SessionPartition.validateKeys(Boolean,Set<String>)",
		},
		"core-runtime-sobject-tail-api67.json": {
			"apex:System.SObject.getSObjects(Schema.SObjectField)",
		},
	}
	owners := make(map[string]int, 4)
	for filename, targets := range fixtures {
		path := filepath.Join(root, filename)
		fixture, err := compat.LoadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := compat.Validate(fixture); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var metadata struct {
			APIVersion string                          `json:"apiVersion"`
			Mode       string                          `json:"mode"`
			Candidate  struct{ Commit, SHA256 string } `json:"candidate"`
			Profile    struct {
				CandidateCommit string `json:"candidateCommit"`
				CandidateSHA256 string `json:"candidateSha256"`
				SelectedRows    int    `json:"selectedRowCount"`
			} `json:"profile"`
			Notes                     string `json:"notes"`
			SalesforceEligible        *bool  `json:"salesforceEligible"`
			SalesforceExclusionClass  string `json:"salesforceExclusionClass"`
			SalesforceExclusionReason string `json:"salesforceExclusionReason"`
			Salesforce                any    `json:"salesforce"`
			Comparisons               any    `json:"comparisons"`
		}
		if err := json.Unmarshal(data, &metadata); err != nil {
			t.Fatal(err)
		}
		notes := strings.ToLower(metadata.Notes)
		reason := strings.ToLower(metadata.SalesforceExclusionReason)
		if metadata.APIVersion != "67.0" || metadata.Mode != "local-runtime" || metadata.Candidate.Commit != cacheSObjectCandidateCommit || metadata.Candidate.SHA256 != cacheSObjectCandidateSHA || metadata.Profile.CandidateCommit != cacheSObjectCandidateCommit || metadata.Profile.CandidateSHA256 != cacheSObjectCandidateSHA || metadata.Profile.SelectedRows != len(fixture.Evidence) || !strings.Contains(notes, "api 67") || metadata.SalesforceEligible == nil || *metadata.SalesforceEligible || metadata.SalesforceExclusionClass != "policy-local-only" || !strings.Contains(reason, "zero hosted salesforce parity") || metadata.Salesforce != nil || metadata.Comparisons != nil {
			t.Fatalf("%s provenance = %#v", filename, metadata)
		}
		if len(fixture.Source) != 1 && filename == "core-runtime-cache-validatekeys-set-api67.json" || len(fixture.Command.Args) != 1 {
			t.Fatalf("%s command/source boundary = %#v", filename, fixture)
		}
		if filename == "core-runtime-cache-validatekeys-set-api67.json" && fixture.Source[0].Content != fixture.Command.Args[0] {
			t.Fatalf("%s anonymous source and command diverged", filename)
		}
		want := mapFromIDs(targets)
		for _, row := range fixture.Evidence {
			if want[row.SurfaceID] {
				owners[row.SurfaceID]++
			}
		}
	}

	cache, err := os.ReadFile(filepath.Join(root, "core-runtime-cache-validatekeys-set-api67.json"))
	if err != nil {
		t.Fatal(err)
	}
	cacheSource := string(cache)
	for _, call := range []string{
		"Cache.OrgPartition.validateKeys(false, keys)",
		"Cache.Partition.validateKeys(false, keys)",
		"Cache.SessionPartition.validateKeys(false, keys)",
	} {
		if !strings.Contains(cacheSource, call) {
			t.Fatalf("Cache fixture missing static Set call %q", call)
		}
	}
	if strings.Contains(cacheSource, "new List<String>") || strings.Contains(cacheSource, "orgPartition.validateKeys") || strings.Contains(cacheSource, "sessionPartition.validateKeys") {
		t.Fatal("Cache fixture includes excluded List or instance validateKeys form")
	}

	sobject, err := os.ReadFile(filepath.Join(root, "core-runtime-sobject-tail-api67.json"))
	if err != nil {
		t.Fatal(err)
	}
	sobjectSource := string(sobject)
	for _, assertion := range []string{
		"unqueried.getSObjects(Contact.AccountId)",
		"emptyRow.getSObjects(Contact.AccountId)",
		"populatedRow.getSObjects(Contact.AccountId)",
		"(SELECT Id FROM Contacts)",
		"(SELECT Id, LastName FROM Contacts)",
	} {
		if !strings.Contains(sobjectSource, assertion) {
			t.Fatalf("SObject fixture missing relationship assertion %q", assertion)
		}
	}
	if len(owners) != 4 {
		t.Fatalf("exact packet ownership = %d, want 4", len(owners))
	}
	for id, count := range owners {
		if count != 1 {
			t.Fatalf("exact fixture ownership for %s = %d, want 1", id, count)
		}
	}
	paths, err := filepath.Glob(filepath.Join(root, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	allOwners := make(map[string]int, len(owners))
	for _, path := range paths {
		var header struct {
			EvidenceOnly bool `json:"evidenceOnly"`
			Evidence     []struct {
				SurfaceID string `json:"surfaceId"`
			} `json:"evidence"`
		}
		readJSON(t, path, &header)
		if header.EvidenceOnly {
			continue
		}
		for _, row := range header.Evidence {
			if _, ok := owners[row.SurfaceID]; ok {
				allOwners[row.SurfaceID]++
			}
		}
	}
	for id, count := range allOwners {
		if count != 1 {
			t.Fatalf("non-evidenceOnly ownership for %s = %d, want 1", id, count)
		}
	}
}
