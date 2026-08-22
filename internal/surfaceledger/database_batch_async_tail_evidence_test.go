package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

const databaseBatchAsyncTailFixture = "core-runtime-database-batch-async-tail-api67.json"

var databaseBatchAsyncTailIDs = []string{
	"apex:Database.Batchable.execute(Database.BatchableContext,List<Object>)",
	"apex:Database.Batchable.finish(Database.BatchableContext)",
	"apex:Database.Batchable.start(Database.BatchableContext)",
	"apex:Database.BatchableContext.getChildJobId()",
	"apex:Database.BatchableContext.getJobId()",
	"apex:Database.BatchableContextImpl",
	"apex:Database.BatchableContextImpl.BatchableContextImpl()",
	"apex:System.Database.executeBatch(Object)",
}

func TestDatabaseBatchAsyncTailHasExactOwnershipAndProvenance(t *testing.T) {
	root := filepath.Join("..", "..")
	path := filepath.Join(root, "docs", "fixtures", databaseBatchAsyncTailFixture)
	fixture, err := compat.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Name != strings.TrimSuffix(databaseBatchAsyncTailFixture, ".json") || fixture.Command.Kind != "test" || len(fixture.Source) != 2 {
		t.Fatalf("fixture envelope = %#v", fixture)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var metadata struct {
		APIVersion   string `json:"apiVersion"`
		Mode         string `json:"mode"`
		EvidenceOnly bool   `json:"evidenceOnly"`
		Candidate    struct {
			Commit string `json:"commit"`
			SHA256 string `json:"sha256"`
		} `json:"candidate"`
		SalesforceEligible *bool  `json:"salesforceEligible"`
		ExclusionClass     string `json:"salesforceExclusionClass"`
		ExclusionReason    string `json:"salesforceExclusionReason"`
		Profile            struct {
			CandidateCommit string `json:"candidateCommit"`
			CandidateSHA256 string `json:"candidateSha256"`
			SelectedRows    int    `json:"selectedRowCount"`
		} `json:"profile"`
		Command struct {
			Kind string `json:"kind"`
		} `json:"command"`
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.APIVersion != "67.0" || metadata.Mode != "local-runtime" || metadata.EvidenceOnly || metadata.Command.Kind != "test" || metadata.Candidate.Commit != "3409c4c85827b19712e9df83fc8905aa02bd1dc8" || metadata.Candidate.SHA256 != "960ac9f26fa92aae6054cbe0e59f9c4ab1f84397df67bd8a89528068d02a1fce" || metadata.SalesforceEligible == nil || *metadata.SalesforceEligible || metadata.ExclusionClass != "policy-local-only" || !strings.Contains(strings.ToLower(metadata.ExclusionReason), "zero hosted") || metadata.Profile.CandidateCommit != metadata.Candidate.Commit || metadata.Profile.CandidateSHA256 != metadata.Candidate.SHA256 || metadata.Profile.SelectedRows != len(databaseBatchAsyncTailIDs) {
		t.Fatalf("fixture provenance = %#v", metadata)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"salesforce", "comparisons", "selectedOrg", "salesforceEvidencePaths", "orgAlias", "orgId"} {
		if _, ok := fields[key]; ok {
			t.Fatalf("hosted field %q present", key)
		}
	}

	want := make(map[string]bool, len(databaseBatchAsyncTailIDs))
	for _, id := range databaseBatchAsyncTailIDs {
		want[id] = true
	}
	seen := make(map[string]bool, len(want))
	for _, evidence := range fixture.Evidence {
		if evidence.Kind != "test" || !want[evidence.SurfaceID] || seen[evidence.SurfaceID] {
			t.Fatalf("unexpected or duplicate evidence row: %#v", evidence)
		}
		seen[evidence.SurfaceID] = true
	}
	if len(seen) != len(want) {
		t.Fatalf("evidence rows = %d, want %d", len(seen), len(want))
	}

	evidence, err := BuildEvidenceSnapshot([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	assertExactSurfaceSet(t, evidence, databaseBatchAsyncTailIDs)
	for _, row := range evidence {
		if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported || len(row.Sources) != 1 || row.Sources[0] != "fixture:"+strings.TrimSuffix(databaseBatchAsyncTailFixture, ".json") {
			t.Fatalf("row = %#v", row)
		}
	}

	for _, witness := range []string{
		"implements Database.Batchable<Account>",
		"start(Database.BatchableContext",
		"execute(Database.BatchableContext",
		"finish(Database.BatchableContext",
		"context.getJobId()",
		"context.getChildJobId()",
		"new Database.BatchableContextImpl()",
		"Database.executeBatch(new DatabaseBatchAsyncTailProbe())",
	} {
		found := false
		for _, source := range fixture.Source {
			found = found || strings.Contains(source.Content, witness)
		}
		if !found {
			t.Fatalf("source missing executable witness %q", witness)
		}
	}
	if result, err := compat.Run(fixture); err != nil || !result.OK {
		t.Fatalf("fixture execution = %#v, error = %v", result, err)
	}

	paths, err := filepath.Glob(filepath.Join(root, "docs", "fixtures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	owners := make(map[string]int, len(want))
	for _, candidate := range paths {
		var header struct {
			EvidenceOnly bool `json:"evidenceOnly"`
			Evidence     []struct {
				SurfaceID string `json:"surfaceId"`
			} `json:"evidence"`
		}
		readJSON(t, candidate, &header)
		if header.EvidenceOnly {
			continue
		}
		for _, row := range header.Evidence {
			if want[row.SurfaceID] {
				owners[row.SurfaceID]++
			}
		}
	}
	for _, id := range databaseBatchAsyncTailIDs {
		if owners[id] != 1 {
			t.Fatalf("fixture ownership for %s = %d, want exactly one", id, owners[id])
		}
	}
}
