package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

const valueObjectsTailFixture = "core-runtime-value-objects-tail-api67.json"

var valueObjectsTailIDs = []string{
	"apex:System.AccessLevel",
	"apex:System.AccessLevel.SYSTEM_MODE",
	"apex:System.AccessLevel.USER_MODE",
	"apex:System.AccessLevel.clone()",
	"apex:System.Address.equals(Object)",
	"apex:System.Address.hashCode()",
	"apex:System.Address.toString()",
	"apex:System.AggregateResult",
	"apex:System.Cookie.hashCode()",
	"apex:System.Label.get(String,String)",
	"apex:System.Label.get(String,String,String)",
	"apex:System.Label.translationExists(string,string,string)",
	"apex:System.Location.equals(Object)",
	"apex:System.Location.hashCode()",
	"apex:System.Location.toString()",
	"apex:System.PageReference.PageReference(String)",
	"apex:System.PageReference.equals(Object)",
	"apex:System.PageReference.hashCode()",
	"apex:System.PageReference.toString()",
	"apex:System.SObjectAccessDecision",
	"apex:System.SObjectAccessDecision.getModifiedIndexes()",
	"apex:System.SObjectAccessDecision.getRemovedFields()",
	"apex:System.SelectOption",
	"apex:System.SelectOption.equals(Object)",
	"apex:System.SelectOption.hashCode()",
	"apex:System.SelectOption.toString()",
	"apex:System.Savepoint.equals(Object)",
	"apex:System.Savepoint.hashCode()",
	"apex:System.Savepoint.toString()",
}

var valueObjectsTailRejected = map[string]string{
	"apex:System.Cookie.equals(Object)": "candidate dispatch exposes Cookie.equals with zero arguments, so the Object overload is not executable evidence",
}

func TestValueObjectsTailHasExactExecutableLocalEvidence(t *testing.T) {
	root := filepath.Join("..", "..")
	path := filepath.Join(root, "docs", "fixtures", valueObjectsTailFixture)
	fixture, err := compat.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Command.Kind != "exec" || len(fixture.Command.Args) != 1 || len(fixture.Source) != 1 || fixture.Source[0].Content != fixture.Command.Args[0] {
		t.Fatalf("fixture execution envelope = %#v", fixture)
	}
	if result, err := compat.Run(fixture); err != nil || !result.OK {
		t.Fatalf("fixture execution = %#v, error = %v", result, err)
	}
	if len(fixture.Evidence) != len(valueObjectsTailIDs) {
		t.Fatalf("raw evidence rows = %d, want %d", len(fixture.Evidence), len(valueObjectsTailIDs))
	}
	evidence, err := BuildEvidenceSnapshot([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	assertExactSurfaceSet(t, evidence, valueObjectsTailIDs)
	for _, row := range fixture.Evidence {
		if reason, rejected := valueObjectsTailRejected[row.SurfaceID]; rejected {
			t.Fatalf("rejected row %s was included: %s", row.SurfaceID, reason)
		}
	}
	for _, row := range evidence {
		if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported || len(row.Sources) != 1 || row.Sources[0] != "fixture:"+strings.TrimSuffix(valueObjectsTailFixture, ".json") {
			t.Fatalf("%s evidence = %#v", row.SurfaceID, row)
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var metadata struct {
		APIVersion                string `json:"apiVersion"`
		Mode                      string `json:"mode"`
		EvidenceOnly              bool   `json:"evidenceOnly"`
		SalesforceEligible        *bool  `json:"salesforceEligible"`
		SalesforceExclusionClass  string `json:"salesforceExclusionClass"`
		SalesforceExclusionReason string `json:"salesforceExclusionReason"`
		Candidate                 struct {
			Commit string `json:"commit"`
			SHA    string `json:"sha256"`
		} `json:"candidate"`
		Profile struct {
			CandidateCommit string `json:"candidateCommit"`
			CandidateSHA    string `json:"candidateSha256"`
			LaneID          string `json:"laneId"`
			SelectedRows    int    `json:"selectedRowCount"`
		} `json:"profile"`
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.APIVersion != "67.0" || metadata.Mode != "local-runtime" || metadata.EvidenceOnly || metadata.SalesforceEligible == nil || *metadata.SalesforceEligible || metadata.SalesforceExclusionClass != "policy-local-only" || !strings.Contains(strings.ToLower(metadata.SalesforceExclusionReason), "zero hosted salesforce parity") || metadata.Candidate.Commit != "3409c4c85827b19712e9df83fc8905aa02bd1dc8" || metadata.Candidate.SHA != "960ac9f26fa92aae6054cbe0e59f9c4ab1f84397df67bd8a89528068d02a1fce" || metadata.Profile.CandidateCommit != metadata.Candidate.Commit || metadata.Profile.CandidateSHA != metadata.Candidate.SHA || metadata.Profile.LaneID == "" || metadata.Profile.SelectedRows != len(valueObjectsTailIDs) {
		t.Fatalf("fixture provenance = %#v", metadata)
	}
	paths, err := filepath.Glob(filepath.Join(root, "docs", "fixtures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	wanted := make(map[string]struct{}, len(valueObjectsTailIDs))
	for _, id := range valueObjectsTailIDs {
		wanted[id] = struct{}{}
	}
	owners := make(map[string]int, len(wanted))
	for _, fixturePath := range paths {
		data, err := os.ReadFile(fixturePath)
		if err != nil {
			t.Fatal(err)
		}
		var header struct {
			EvidenceOnly bool `json:"evidenceOnly"`
			Evidence     []struct {
				SurfaceID string `json:"surfaceId"`
			} `json:"evidence"`
		}
		if err := json.Unmarshal(data, &header); err != nil {
			t.Fatal(err)
		}
		if header.EvidenceOnly {
			continue
		}
		for _, item := range header.Evidence {
			if _, ok := wanted[item.SurfaceID]; ok {
				owners[item.SurfaceID]++
			}
		}
	}
	for _, id := range valueObjectsTailIDs {
		if owners[id] != 1 {
			t.Fatalf("non-evidenceOnly fixture ownership for %s = %d, want exactly one", id, owners[id])
		}
	}
}
