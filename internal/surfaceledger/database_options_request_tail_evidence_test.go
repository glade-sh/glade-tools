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
	databaseOptionsRequestTailFixture = "core-runtime-database-options-request-tail-api67.json"
	databaseResultRequestTailFixture  = "core-runtime-database-result-request-tail-local-api67.json"
)

var databaseOptionsRequestTailIDs = []string{"apex:Database.DmlOptions.AssignmentRuleHeader.assignmentRuleID", "apex:Database.DmlOptions.AssignmentRuleHeader.useDefaultRule", "apex:Database.DmlOptions.EmailHeader.triggerAutoResponseEmail", "apex:Database.DmlOptions.EmailHeader.triggerOtherEmail", "apex:Database.DmlOptions.EmailHeader.triggerUserEmail", "apex:Database.LeadConvert.relatedpersonaccountid"}
var databaseResultRequestTailIDs = []string{"apex:Database.LeadConvertResult.getErrors()", "apex:Database.LeadConvertResult.getRelatedPersonAccountId()", "apex:Database.LocaleOptions", "apex:Database.LocaleOptions.LocaleOptions()", "apex:Database.MergeRequest.additionalinformationmap", "apex:Database.MergeRequest.masterrecord", "apex:Database.MergeRequest.recordtomergeids", "apex:Database.MergeResult.getErrors()"}

func TestDatabaseOptionsRequestTailHasExactExecutableLocalEvidence(t *testing.T) {
	root := filepath.Join("..", "..")
	tests := []struct {
		filename string
		ids      []string
		eligible bool
		witness  []string
	}{
		{databaseOptionsRequestTailFixture, databaseOptionsRequestTailIDs, true, []string{"useDefaultRule", "triggerAutoResponseEmail", "triggerOtherEmail", "triggerUserEmail", "relatedpersonaccountid"}},
		{databaseResultRequestTailFixture, databaseResultRequestTailIDs, false, []string{"getErrors()", "getRelatedPersonAccountId()", "new Database.LocaleOptions()", "additionalinformationmap", "recordtomergeids"}},
	}
	allIDs := append(append([]string{}, databaseOptionsRequestTailIDs...), databaseResultRequestTailIDs...)
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
			if fixture.Name != strings.TrimSuffix(tc.filename, ".json") || fixture.Command.Kind != "exec" || len(fixture.Command.Args) != 1 || len(fixture.Source) != 1 || fixture.Source[0].Content != fixture.Command.Args[0] {
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
			if !tc.eligible && (metadata.ExclusionClass != "policy-local-only" || !strings.Contains(metadata.ExclusionReason, "MergeRequest") || !strings.Contains(metadata.ExclusionReason, "Internal Salesforce Error")) {
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
			evidence, err := BuildEvidenceSnapshot([]string{path})
			if err != nil {
				t.Fatal(err)
			}
			if len(evidence) != len(tc.ids) {
				t.Fatalf("snapshot rows = %d, want %d", len(evidence), len(tc.ids))
			}
			for _, row := range evidence {
				if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported {
					t.Fatalf("%s evidence/behavior = %s/%s", row.SurfaceID, row.Evidence, row.GladeBehavior)
				}
			}
			if result, err := compat.Run(fixture); err != nil || !result.OK {
				t.Fatalf("fixture execution = %#v, error = %v", result, err)
			}
		})
	}
	allEvidence, err := BuildEvidenceSnapshot([]string{filepath.Join(root, "docs", "fixtures", databaseOptionsRequestTailFixture), filepath.Join(root, "docs", "fixtures", databaseResultRequestTailFixture)})
	if err != nil {
		t.Fatal(err)
	}
	if len(allEvidence) != len(allIDs) {
		t.Fatalf("combined snapshot rows = %d, want %d", len(allEvidence), len(allIDs))
	}
	owners := map[string]int{}
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
