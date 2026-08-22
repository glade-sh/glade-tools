package surfaceledger

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func semanticJSONSHA256(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}

func TestDatabaseRecycleFixtureExecutablePayloadUnchanged(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "fixtures", "data-dml-approval-recycle-results.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	// These semantic payload hashes are the untouched HEAD baseline. The wave
	// transfers evidence ownership only; executable behavior and expectations
	// remain byte-for-byte equivalent semantically.
	want := map[string]string{
		"command":       "12aaed84d588265b7ffe1b482a573a47e271256da638c627b78b38b4606fd1ba",
		"source":        "3effc8f1c29a48cc58237438d0046a0cdcb98e1a6c44a04438734712fbe48d41",
		"schemaVersion": "74234e98afe7498fb5daf1f36ac2d78acc339464f950703b8c019892f982b90b",
		"expected":      "7d46423c8c2492c60b8bf907f721c7e23cf6a05bb051159ce146a2a6be473696",
	}
	for key, expected := range want {
		if got := semanticJSONSHA256(t, payload[key]); got != expected {
			t.Fatalf("%s semantic payload hash = %s, want untouched HEAD %s", key, got, expected)
		}
	}
}

const databaseDuplicateRecycleSavepointFixture = "core-runtime-database-duplicate-recycle-savepoint-api67.json"

var databaseDuplicateRecycleSavepointIDs = []string{
	"apex:Database.DuplicateError.duplicateresult",
	"apex:Database.DuplicateError.getDuplicateResult()",
	"apex:Database.DuplicateError.getFields()",
	"apex:Database.DuplicateError.getMessage()",
	"apex:Database.DuplicateError.getStatusCode()",
	"apex:Database.EmptyRecycleBinResult.EmptyRecycleBinResult()",
	"apex:Database.EmptyRecycleBinResult.errors",
	"apex:Database.EmptyRecycleBinResult.getErrors()",
	"apex:Database.EmptyRecycleBinResult.getId()",
	"apex:Database.EmptyRecycleBinResult.isSuccess()",
	"apex:Database.MergeRequest.MergeRequest()",
	"apex:Database.Savepoint",
}

func TestDatabaseDuplicateRecycleSavepointHasExactExecutableLocalEvidence(t *testing.T) {
	root := filepath.Join("..", "..")
	fixturePath := filepath.Join(root, "docs", "fixtures", databaseDuplicateRecycleSavepointFixture)
	fixture, err := compat.LoadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Name != strings.TrimSuffix(databaseDuplicateRecycleSavepointFixture, ".json") || fixture.Command.Kind != "exec" || len(fixture.Command.Args) != 1 || len(fixture.Source) != 1 || fixture.Source[0].Content != fixture.Command.Args[0] {
		t.Fatalf("fixture envelope = %#v", fixture)
	}
	evidence, err := BuildEvidenceSnapshot([]string{fixturePath})
	if err != nil {
		t.Fatal(err)
	}
	assertExactSurfaceSet(t, evidence, databaseDuplicateRecycleSavepointIDs)

	var metadata struct {
		APIVersion                string `json:"apiVersion"`
		Mode                      string `json:"mode"`
		Notes                     string `json:"notes"`
		SalesforceEligible        *bool  `json:"salesforceEligible"`
		SalesforceExclusionClass  string `json:"salesforceExclusionClass"`
		SalesforceExclusionReason string `json:"salesforceExclusionReason"`
		Salesforce                any    `json:"salesforce"`
		Comparisons               any    `json:"comparisons"`
		Profile                   struct {
			CandidateCommit string `json:"candidateCommit"`
			CandidateSHA256 string `json:"candidateSha256"`
			SelectedRows    int    `json:"selectedRowCount"`
		} `json:"profile"`
	}
	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.APIVersion != "67.0" || metadata.Mode != "local-runtime" || metadata.SalesforceEligible == nil || *metadata.SalesforceEligible || metadata.SalesforceExclusionClass != "policy-local-only" || !strings.Contains(metadata.SalesforceExclusionReason, "invalid hosted Salesforce compile/probe paths") || metadata.Profile.SelectedRows != 12 || metadata.Profile.CandidateCommit != "3409c4c85827b19712e9df83fc8905aa02bd1dc8" || metadata.Profile.CandidateSHA256 != "960ac9f26fa92aae6054cbe0e59f9c4ab1f84397df67bd8a89528068d02a1fce" || metadata.Salesforce != nil || metadata.Comparisons != nil || !strings.Contains(metadata.Notes, "no hosted Salesforce execution or parity claim") {
		t.Fatalf("fixture provenance = %#v", metadata)
	}
	if result, err := compat.Run(fixture); err != nil || !result.OK {
		t.Fatalf("fixture execution = %#v, error = %v", result, err)
	}
	for _, witness := range []string{
		"duplicateError.duplicateresult", "duplicateError.getDuplicateResult()", "duplicateError.getFields()", "duplicateError.getMessage()", "duplicateError.getStatusCode()",
		"Database.EmptyRecycleBinResult emptyResult = new Database.EmptyRecycleBinResult()", "emptyResult.errors", "emptyResult.getErrors()", "emptyResult.getId()", "emptyResult.isSuccess()",
		"Database.MergeRequest request = new Database.MergeRequest()", "Database.Savepoint savepoint = (Database.Savepoint)null",
	} {
		if !strings.Contains(fixture.Source[0].Content, witness) {
			t.Fatalf("source missing %q", witness)
		}
	}
	owners := make(map[string]int, len(databaseDuplicateRecycleSavepointIDs))
	want := map[string]bool{}
	for _, id := range databaseDuplicateRecycleSavepointIDs {
		want[id] = true
	}
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
	for _, id := range databaseDuplicateRecycleSavepointIDs {
		if owners[id] != 1 {
			t.Fatalf("non-evidenceOnly ownership for %s = %d, want exactly one", id, owners[id])
		}
	}
}
