package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

const databaseCursorSyncTailFixture = "core-runtime-database-cursor-sync-tail-api67.json"

var databaseCursorSyncTailIDs = []string{
	"apex:Database.Cursor.Cursor()",
	"apex:Database.Cursor.DeleteFilter",
	"apex:Database.CursorFetchResult.getNextIndex()",
	"apex:Database.CursorFetchResult.getNumDeletedRecords()",
	"apex:Database.CursorFetchResult.getRecords()",
	"apex:Database.CursorFetchResult.isDone()",
	"apex:Database.GetDeletedResult.getDeletedRecords()",
	"apex:Database.GetDeletedResult.getEarliestDateAvailable()",
	"apex:Database.GetDeletedResult.getLatestDateCovered()",
	"apex:Database.PaginationCursor.fetchDeleted(Integer,Integer)",
}

func TestDatabaseCursorSyncTailHasExactExecutableLocalEvidence(t *testing.T) {
	root := filepath.Join("..", "..")
	fixturePath := filepath.Join(root, "docs", "fixtures", databaseCursorSyncTailFixture)
	fixture, err := compat.LoadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Name != strings.TrimSuffix(databaseCursorSyncTailFixture, ".json") || fixture.Command.Kind != "exec" || len(fixture.Command.Args) != 1 || len(fixture.Source) != 1 || fixture.Source[0].Content != fixture.Command.Args[0] {
		t.Fatalf("fixture execution envelope = %#v", fixture)
	}

	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	var metadata struct {
		APIVersion         string `json:"apiVersion"`
		Mode               string `json:"mode"`
		Notes              string `json:"notes"`
		EvidenceOnly       bool   `json:"evidenceOnly"`
		SalesforceEligible *bool  `json:"salesforceEligible"`
		Salesforce         any    `json:"salesforce"`
		Comparisons        any    `json:"comparisons"`
		Profile            struct {
			CandidateCommit string `json:"candidateCommit"`
			CandidateSHA256 string `json:"candidateSha256"`
			SelectedRows    int    `json:"selectedRowCount"`
		} `json:"profile"`
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.APIVersion != "67.0" || metadata.Mode != "local-runtime" || metadata.EvidenceOnly || metadata.SalesforceEligible == nil || !*metadata.SalesforceEligible || metadata.Profile.CandidateCommit != "3409c4c85827b19712e9df83fc8905aa02bd1dc8" || metadata.Profile.CandidateSHA256 != "960ac9f26fa92aae6054cbe0e59f9c4ab1f84397df67bd8a89528068d02a1fce" || metadata.Profile.SelectedRows != len(databaseCursorSyncTailIDs) {
		t.Fatalf("fixture provenance = %#v", metadata)
	}
	if metadata.Salesforce != nil || metadata.Comparisons != nil || !strings.Contains(metadata.Notes, "no hosted Salesforce execution or parity claim") {
		t.Fatalf("fixture makes an unsupported Salesforce parity claim: %#v", metadata)
	}

	evidence, err := BuildEvidenceSnapshot([]string{fixturePath})
	if err != nil {
		t.Fatal(err)
	}
	assertExactSurfaceSet(t, evidence, databaseCursorSyncTailIDs)
	for _, row := range evidence {
		if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported {
			t.Fatalf("%s evidence/behavior = %s/%s, want fixture/supported", row.SurfaceID, row.Evidence, row.GladeBehavior)
		}
	}
	for _, witness := range []string{
		"new Database.Cursor()",
		"Database.Cursor.DeleteFilter.NO_FILTER",
		"page.fetchPage(0, 2)",
		"pageResult.getRecords()",
		"pageResult.getNextIndex()",
		"pageResult.getNumDeletedRecords()",
		"pageResult.isDone()",
		"page.fetchDeleted(0, 1)",
		"deleted.getDeletedRecords()",
		"deleted.getEarliestDateAvailable()",
		"deleted.getLatestDateCovered()",
	} {
		if !strings.Contains(fixture.Source[0].Content, witness) {
			t.Fatalf("source missing executable witness %q", witness)
		}
	}

	want := mapFromIDs(databaseCursorSyncTailIDs)
	owners := make(map[string]int, len(want))
	paths, err := filepath.Glob(filepath.Join(root, "docs", "fixtures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
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
			if want[row.SurfaceID] {
				owners[row.SurfaceID]++
			}
		}
	}
	for _, id := range databaseCursorSyncTailIDs {
		if owners[id] != 1 {
			t.Fatalf("fixture ownership for %s = %d, want exactly one non-evidenceOnly owner", id, owners[id])
		}
	}

	if result, err := compat.Run(fixture); err != nil || !result.OK {
		t.Fatalf("fixture execution = %#v, error = %v", result, err)
	}
}
