package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

const systemScalarAdderrorTailFixture = "core-runtime-system-scalar-adderror-tail-api67.json"

var systemScalarAdderrorTailIDs = []string{
	"apex:System.Boolean.addError(Exception)", "apex:System.Boolean.addError(Exception,Boolean)", "apex:System.Boolean.addError(String)", "apex:System.Boolean.addError(String,Boolean)",
	"apex:System.Date.addError(Exception)", "apex:System.Date.addError(Exception,Boolean)", "apex:System.Date.addError(String)", "apex:System.Date.addError(String,Boolean)",
	"apex:System.Datetime.addError(Exception)", "apex:System.Datetime.addError(Exception,Boolean)", "apex:System.Datetime.addError(String)", "apex:System.Datetime.addError(String,Boolean)",
	"apex:System.Decimal.addError(Exception)", "apex:System.Decimal.addError(Exception,Boolean)", "apex:System.Decimal.addError(String)", "apex:System.Decimal.addError(String,Boolean)",
	"apex:System.Double.addError(Exception)", "apex:System.Double.addError(Exception,Boolean)", "apex:System.Double.addError(String)", "apex:System.Double.addError(String,Boolean)",
	"apex:System.Id.addError(Exception)", "apex:System.Id.addError(Exception,Boolean)", "apex:System.Id.addError(String)", "apex:System.Id.addError(String,Boolean)",
	"apex:System.Integer.addError(Exception)", "apex:System.Integer.addError(Exception,Boolean)", "apex:System.Integer.addError(String)", "apex:System.Integer.addError(String,Boolean)",
	"apex:System.Long.addError(Exception)", "apex:System.Long.addError(Exception,Boolean)", "apex:System.Long.addError(String)", "apex:System.Long.addError(String,Boolean)",
	"apex:System.Time.addError(Exception)", "apex:System.Time.addError(Exception,Boolean)", "apex:System.Time.addError(String)", "apex:System.Time.addError(String,Boolean)",
}

func TestSystemScalarAdderrorTailHasExactCandidateLocalEvidence(t *testing.T) {
	root := filepath.Join("..", "..")
	path := filepath.Join(root, "docs", "fixtures", systemScalarAdderrorTailFixture)
	fixture, err := compat.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Name != strings.TrimSuffix(systemScalarAdderrorTailFixture, ".json") || fixture.Command.Kind != "test" || len(fixture.Source) != 11 || fixture.Project.SourceAPIVersion != "67.0" {
		t.Fatalf("fixture envelope = %#v", fixture)
	}
	if len(fixture.Evidence) != len(systemScalarAdderrorTailIDs) {
		t.Fatalf("evidence rows = %d, want %d", len(fixture.Evidence), len(systemScalarAdderrorTailIDs))
	}
	if result, err := compat.Run(fixture); err != nil || !result.OK {
		t.Fatalf("fixture execution = %#v, error = %v", result, err)
	}

	var metadata struct {
		APIVersion string `json:"apiVersion"`
		Mode string `json:"mode"`
		EvidenceOnly bool `json:"evidenceOnly"`
		Eligible *bool `json:"salesforceEligible"`
		ExclusionClass string `json:"salesforceExclusionClass"`
		ExclusionReason string `json:"salesforceExclusionReason"`
		Salesforce any `json:"salesforce"`
		Comparisons any `json:"comparisons"`
		Notes string `json:"notes"`
		Profile struct {
			CandidateCommit string `json:"candidateCommit"`
			CandidateSHA256 string `json:"candidateSha256"`
			LaneID string `json:"laneId"`
			SelectedRows int `json:"selectedRowCount"`
		} `json:"profile"`
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.APIVersion != "67.0" || metadata.Mode != "local-runtime" || metadata.EvidenceOnly || metadata.Eligible == nil || *metadata.Eligible || metadata.ExclusionClass != "policy-local-only" || !strings.Contains(strings.ToLower(metadata.ExclusionReason), "zero hosted salesforce parity") || metadata.Profile.CandidateCommit != "3409c4c85827b19712e9df83fc8905aa02bd1dc8" || metadata.Profile.CandidateSHA256 != "960ac9f26fa92aae6054cbe0e59f9c4ab1f84397df67bd8a89528068d02a1fce" || metadata.Profile.LaneID == "" || metadata.Profile.SelectedRows != len(systemScalarAdderrorTailIDs) || metadata.Salesforce != nil || metadata.Comparisons != nil || !strings.Contains(metadata.Notes, "no hosted Salesforce execution or parity claim") {
		t.Fatalf("fixture provenance = %#v", metadata)
	}

	want := mapFromIDs(systemScalarAdderrorTailIDs)
	seen := make(map[string]bool, len(want))
	for _, row := range fixture.Evidence {
		if row.Kind != "test" || !want[row.SurfaceID] || seen[row.SurfaceID] {
			t.Fatalf("unexpected or duplicate evidence row = %#v", row)
		}
		seen[row.SurfaceID] = true
	}
	if len(seen) != len(want) {
		t.Fatalf("evidence ownership = %d, want %d", len(seen), len(want))
	}

	// Older unclassified addError material remains historical; only an exact
	// candidate fixture can own these rows for local-proof status.
	paths, err := filepath.Glob(filepath.Join(root, "docs", "fixtures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	owners := make(map[string]int, len(want))
	for _, candidate := range paths {
		var header struct {
			EvidenceOnly bool `json:"evidenceOnly"`
			Profile json.RawMessage `json:"profile"`
			Evidence []struct { SurfaceID string `json:"surfaceId"` } `json:"evidence"`
		}
		readJSON(t, candidate, &header)
		if header.EvidenceOnly || len(header.Profile) == 0 || header.Profile[0] != '{' {
			continue
		}
		var profile struct { CandidateSHA256 string `json:"candidateSha256"` }
		if err := json.Unmarshal(header.Profile, &profile); err != nil || profile.CandidateSHA256 == "" {
			continue
		}
		for _, row := range header.Evidence {
			if want[row.SurfaceID] {
				owners[row.SurfaceID]++
			}
		}
	}
	for _, id := range systemScalarAdderrorTailIDs {
		if owners[id] != 1 {
			t.Fatalf("exact-candidate fixture ownership for %s = %d, want 1", id, owners[id])
		}
	}
}
