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
	databaseResultAccessorsFixture    = "core-runtime-database-result-error-accessors-api67.json"
	databaseResultConstructorsFixture = "core-runtime-database-result-error-constructors-local-api67.json"
)

var databaseResultAccessorTailIDs = []string{
	"apex:Database.DeleteResult.getErrors()",
	"apex:Database.DeleteResult.getId()",
	"apex:Database.DeleteResult.isSuccess()",
	"apex:Database.Error.getFields()",
	"apex:Database.Error.getMessage()",
	"apex:Database.Error.getStatusCode()",
	"apex:Database.UndeleteResult.getErrors()",
	"apex:Database.UndeleteResult.getId()",
	"apex:Database.UndeleteResult.isSuccess()",
}

var databaseResultConstructorTailIDs = []string{
	"apex:Database.DeleteResult.DeleteResult()",
	"apex:Database.Errors",
	"apex:Database.SaveResult.SaveResult()",
	"apex:Database.UndeleteResult.UndeleteResult()",
}

func TestDatabaseResultErrorTailHasExactExecutableLocalEvidence(t *testing.T) {
	root := filepath.Join("..", "..")
	tests := []struct {
		filename string
		ids      []string
		eligible bool
		witness  []string
	}{
		{
			filename: databaseResultAccessorsFixture,
			ids:      databaseResultAccessorTailIDs,
			eligible: true,
			witness: []string{
				"Database.DeleteResult deleted = Database.delete(record, false);",
				"System.assertNotEquals(null, deleted.getErrors());",
				"System.assertEquals(0, deleted.getErrors().size());",
				"deleted.getId()", "deleted.isSuccess()",
				"Database.Error error = invalid.getErrors().get(0);",
				"error.getFields()", "error.getMessage()", "error.getStatusCode()",
				"Database.UndeleteResult restored = Database.undelete(record, false);",
				"Database.UndeleteResult restoreAgain = Database.undelete(record, false);",
				"System.assertNotEquals(null, restoreAgain.getErrors());",
				"System.assert(restoreAgain.getErrors().size() > 0);",
				"restored.getId()", "restored.isSuccess()",
			},
		},
		{
			filename: databaseResultConstructorsFixture,
			ids:      databaseResultConstructorTailIDs,
			eligible: false,
			witness: []string{
				"Database.DeleteResult deleted = new Database.DeleteResult();",
				"Object errorsType = (Database.Errors)null;",
				"Database.SaveResult saved = new Database.SaveResult();",
				"Database.UndeleteResult restored = new Database.UndeleteResult();",
			},
		},
	}
	allIDs := append(append([]string{}, databaseResultAccessorTailIDs...), databaseResultConstructorTailIDs...)
	for _, tc := range tests {
		t.Run(tc.filename, func(t *testing.T) {
			path := filepath.Join(root, "docs", "fixtures", tc.filename)
			fixture, err := compat.LoadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := compat.Validate(fixture); err != nil {
				t.Fatal(err)
			}
			if fixture.Name != strings.TrimSuffix(tc.filename, ".json") || fixture.Command.Kind != "exec" || len(fixture.Command.Args) != 1 || len(fixture.Source) != 1 || fixture.Source[0].Path != "anonymous.apex" || fixture.Source[0].Content != fixture.Command.Args[0] {
				t.Fatalf("fixture execution envelope = %#v", fixture)
			}
			var metadata struct {
				APIVersion         string `json:"apiVersion"`
				Mode               string `json:"mode"`
				Notes              string `json:"notes"`
				EvidenceOnly       bool   `json:"evidenceOnly"`
				SalesforceEligible *bool  `json:"salesforceEligible"`
				Salesforce         any    `json:"salesforce"`
				Comparisons        any    `json:"comparisons"`
				ExclusionClass     string `json:"salesforceExclusionClass"`
				ExclusionReason    string `json:"salesforceExclusionReason"`
				Profile            struct {
					CandidateCommit string `json:"candidateCommit"`
					CandidateSHA256 string `json:"candidateSha256"`
					SelectedRows    int    `json:"selectedRowCount"`
				} `json:"profile"`
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(data, &metadata); err != nil {
				t.Fatal(err)
			}
			if metadata.APIVersion != "67.0" || metadata.Mode != "local-runtime" || metadata.EvidenceOnly || metadata.SalesforceEligible == nil || *metadata.SalesforceEligible != tc.eligible || metadata.Profile.CandidateCommit != "3409c4c85827b19712e9df83fc8905aa02bd1dc8" || metadata.Profile.CandidateSHA256 != "960ac9f26fa92aae6054cbe0e59f9c4ab1f84397df67bd8a89528068d02a1fce" || metadata.Profile.SelectedRows != len(tc.ids) {
				t.Fatalf("fixture provenance = %#v", metadata)
			}
			if metadata.Salesforce != nil || metadata.Comparisons != nil || !strings.Contains(metadata.Notes, "no hosted Salesforce execution or parity claim") {
				t.Fatalf("fixture hosted metadata = %#v", metadata)
			}
			if tc.eligible && (metadata.ExclusionClass != "" || metadata.ExclusionReason != "") {
				t.Fatalf("eligible fixture has exclusion metadata: %#v", metadata)
			}
			if !tc.eligible && (metadata.ExclusionClass != "policy-local-only" || !strings.Contains(metadata.ExclusionReason, "not a portable hosted anonymous Apex runtime probe") || !strings.Contains(metadata.ExclusionReason, "no hosted Salesforce parity")) {
				t.Fatalf("local-only exclusion metadata = %#v", metadata)
			}
			want, seen := map[string]bool{}, map[string]bool{}
			for _, id := range tc.ids {
				want[id] = true
			}
			for _, evidence := range fixture.Evidence {
				if evidence.Kind != "exec" || !want[evidence.SurfaceID] || seen[evidence.SurfaceID] {
					t.Fatalf("unexpected or duplicate evidence row: %#v", evidence)
				}
				seen[evidence.SurfaceID] = true
			}
			if len(seen) != len(tc.ids) {
				t.Fatalf("evidence rows = %d, want %d", len(seen), len(tc.ids))
			}
			for _, witness := range tc.witness {
				if !strings.Contains(fixture.Source[0].Content, witness) {
					t.Fatalf("source missing executable witness %q", witness)
				}
			}
			if result, err := compat.Run(fixture); err != nil || !result.OK {
				t.Fatalf("fixture execution = %#v, error = %v", result, err)
			}
		})
	}

	owners := make(map[string]int, len(allIDs))
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
			for _, id := range allIDs {
				if row.SurfaceID == id {
					owners[id]++
				}
			}
		}
	}
	for _, id := range allIDs {
		if owners[id] != 1 {
			t.Fatalf("fixture ownership for %s = %d, want exactly one", id, owners[id])
		}
	}
}

func TestDatabaseResultErrorTailLegacyOwnersRemainRunnable(t *testing.T) {
	root := filepath.Join("..", "..", "docs", "fixtures")
	for _, filename := range []string{
		"current-base-local-runtime-required-database-001.json",
		"current-base-local-runtime-required-database-002.json",
		"current-base-local-runtime-required-database-003.json",
		"data-dml-database-result-accessors.json",
		"data-platform-database-dml-results.json",
	} {
		t.Run(filename, func(t *testing.T) {
			fixture, err := compat.LoadFile(filepath.Join(root, filename))
			if err != nil {
				t.Fatal(err)
			}
			if err := compat.Validate(fixture); err != nil {
				t.Fatal(err)
			}
			if result, err := compat.Run(fixture); err != nil || !result.OK {
				t.Fatalf("fixture execution = %#v, error = %v", result, err)
			}
		})
	}
}
