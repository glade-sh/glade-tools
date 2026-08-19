package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func TestApprovalTypedProcessEvidenceIsLocalOnly(t *testing.T) {
	fixturesDir := filepath.Join("..", "..", "docs", "fixtures")
	path := filepath.Join(fixturesDir, "core-runtime-approval-local-engine-full.json")
	fixture, err := compat.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := compat.Run(fixture); err != nil || !result.OK {
		t.Fatalf("fixture execution = %#v, error = %v", result, err)
	}
	var metadata struct {
		SalesforceEligible *bool  `json:"salesforceEligible"`
		ExclusionClass     string `json:"salesforceExclusionClass"`
		ExclusionReason    string `json:"salesforceExclusionReason"`
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.SalesforceEligible == nil || *metadata.SalesforceEligible || metadata.ExclusionClass != "policy-local-only" || !strings.Contains(metadata.ExclusionReason, "zero Salesforce workflow parity claim") {
		t.Fatalf("Salesforce policy = %#v", metadata)
	}
	evidence, err := BuildEvidenceSnapshot([]string{
		path,
	})
	if err != nil {
		t.Fatal(err)
	}
	byID := rowsByID(evidence)
	want := []string{
		"apex:Approval.process(Approval.ProcessSubmitRequest)",
		"apex:Approval.process(Approval.ProcessSubmitRequest,Boolean)",
		"apex:Approval.process(Approval.ProcessWorkitemRequest)",
		"apex:Approval.process(Approval.ProcessWorkitemRequest,Boolean)",
		"apex:Approval.process(List<Approval.ProcessSubmitRequest>)",
		"apex:Approval.process(List<Approval.ProcessWorkitemRequest>)",
	}
	for _, id := range want {
		row, ok := byID[id]
		if !ok {
			t.Fatalf("missing typed Approval.process evidence row %s", id)
		}
		if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported {
			t.Fatalf("%s evidence/behavior = %s/%s, want fixture/supported", id, row.Evidence, row.GladeBehavior)
		}
	}

	policy, err := LoadSupportPolicy(filepath.Join(fixturesDir, "apex-local-support-policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	ledger := Merge(nil, nil, BuildGladeSnapshot(), evidence)
	profile := ComputeSupportProfile(ledger.Rows, policy, nil)
	const syntheticHostedID = "apex:Approval.process hosted approval engine routing"
	foundSynthetic := false
	for _, row := range profile.Rows {
		if row.SurfaceID == syntheticHostedID {
			foundSynthetic = true
			if row.Disposition != DispositionHostedDeferred || row.GapClass != "" {
				t.Fatalf("%s profile = disposition:%s gap:%s, want hosted-deferred/no-gap", syntheticHostedID, row.Disposition, row.GapClass)
			}
		}
	}
	if !foundSynthetic {
		t.Fatalf("missing synthetic hosted Approval row %s", syntheticHostedID)
	}
	for _, id := range want {
		found := false
		for _, row := range profile.Rows {
			if row.SurfaceID == id {
				found = true
				if row.Disposition != DispositionCompileShapeRequired || row.GapClass != "" {
					t.Fatalf("%s profile = disposition:%s gap:%s, want compile-shape-required/no-gap", id, row.Disposition, row.GapClass)
				}
			}
		}
		if !found {
			t.Fatalf("missing typed Approval.process profile row %s", id)
		}
	}
}
