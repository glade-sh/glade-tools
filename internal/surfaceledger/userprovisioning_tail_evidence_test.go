package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

const userProvisioningDeterministicTailFixture = "current-base-userprovisioning-deterministic-mock-003-api67.json"

var userProvisioningDeterministicTailIDs = []string{
	"apex:UserProvisioning.ConnectorTestUtil.clone()",
	"apex:UserProvisioning.ConnectorTestUtil.createConnectedApp(String)",
	"apex:UserProvisioning.DummyConnectorApexHandler.clone()",
	"apex:UserProvisioning.FlowProvisionBase.clone()",
	"apex:UserProvisioning.UserProvisioningLog.clone()",
	"apex:UserProvisioning.UserProvisioningProcessHandler.clone()",
}

var userProvisioningOpenTailIDs = []string{
	"apex:UserProvisioning.FlowProvisionBase.getFlowName()",
	"apex:UserProvisioning.FlowProvisionBase.getFlowNamespace()",
	"apex:UserProvisioning.FlowProvisionBase.hasFlow()",
	"apex:UserProvisioning.FlowProvisionBase.hasFlowOrApex()",
}

const userProvisioningDeterministicMissingFixture = "current-base-userprovisioning-deterministic-mock-004-api67.json"

const (
	userProvisioningCandidateCommit = "86ec4226e33f205bf7a42f6f00cc40aa57fc11b5"
	userProvisioningCandidateSHA256 = "0aa758618a8908550aa468c4c9eabd1fcdd06f9f6a7d317ccce45a077380d29a"
)

var userProvisioningDeterministicMissingIDs = []string{
	"apex:UserProvisioning.PluginBatchable",
	"apex:UserProvisioning.UserProvisioningPlugin",
	"apex:UserProvisioning.UserProvisioningPlugin.clone()",
}

func TestUserProvisioningCurrentMissingRowsHaveExactDeterministicEvidence(t *testing.T) {
	root := filepath.Join("..", "..")
	path := filepath.Join(root, "docs", "fixtures", userProvisioningDeterministicMissingFixture)
	fixture, err := compat.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Command.Kind != "test" || len(fixture.Source) == 0 || len(fixture.Command.Args) != 0 {
		t.Fatalf("fixture envelope = %#v", fixture)
	}
	if result, err := compat.Run(fixture); err != nil || !result.OK {
		t.Fatalf("fixture execution = %#v, error = %v", result, err)
	}
	missingSource := fixture.Source[0].Content + fixture.Source[1].Content + fixture.Source[2].Content
	if !strings.Contains(missingSource, "extends UserProvisioning.UserProvisioningPlugin") || !strings.Contains(missingSource, "override Process.PluginResult invoke(Process.PluginRequest request)") || strings.Contains(missingSource, "new UserProvisioning.UserProvisioningPlugin(") {
		t.Fatalf("plugin subclass witness = %q", missingSource)
	}
	owned := make([]string, 0, len(fixture.Evidence))
	for _, evidence := range fixture.Evidence {
		if evidence.Kind != "test" {
			t.Fatalf("evidence kind = %q", evidence.Kind)
		}
		owned = append(owned, evidence.SurfaceID)
	}
	sort.Strings(owned)
	want := append([]string(nil), userProvisioningDeterministicMissingIDs...)
	sort.Strings(want)
	if !reflect.DeepEqual(owned, want) {
		t.Fatalf("fixture ownership = %v, want %v", owned, want)
	}
	paths, err := filepath.Glob(filepath.Join(root, "docs", "fixtures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := BuildEvidenceSnapshot(paths)
	if err != nil {
		t.Fatal(err)
	}
	rows := evidenceRowsForFixture(evidence, userProvisioningDeterministicMissingIDs, strings.TrimSuffix(userProvisioningDeterministicMissingFixture, ".json"))
	assertExactSurfaceSet(t, rows, userProvisioningDeterministicMissingIDs)
	for _, row := range rows {
		if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported || !strings.Contains(strings.Join(row.Sources, ","), "fixture:"+strings.TrimSuffix(userProvisioningDeterministicMissingFixture, ".json")) {
			t.Fatalf("%s evidence/behavior/sources = %s/%s/%v", row.SurfaceID, row.Evidence, row.GladeBehavior, row.Sources)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var metadata struct {
		APIVersion                string `json:"apiVersion"`
		Mode                      string `json:"mode"`
		Notes                     string `json:"notes"`
		SalesforceEligible        *bool  `json:"salesforceEligible"`
		SalesforceExclusionClass  string `json:"salesforceExclusionClass"`
		SalesforceExclusionReason string `json:"salesforceExclusionReason"`
		Candidate                 struct {
			Commit string `json:"commit"`
			SHA256 string `json:"sha256"`
		} `json:"candidate"`
		Profile struct {
			CandidateCommit string `json:"candidateCommit"`
			CandidateSHA256 string `json:"candidateSha256"`
			LaneID          string `json:"laneId"`
			SelectedRows    int    `json:"selectedRowCount"`
		} `json:"profile"`
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.APIVersion != "67.0" || metadata.Mode != "deterministic-mock" || !strings.Contains(strings.ToLower(metadata.Notes), "canonical") || metadata.SalesforceEligible == nil || *metadata.SalesforceEligible || metadata.SalesforceExclusionClass != "policy-local-only" || !strings.Contains(strings.ToLower(metadata.SalesforceExclusionReason), "zero hosted") || metadata.Candidate.Commit != userProvisioningCandidateCommit || metadata.Candidate.SHA256 != userProvisioningCandidateSHA256 || metadata.Profile.CandidateCommit != userProvisioningCandidateCommit || metadata.Profile.CandidateSHA256 != userProvisioningCandidateSHA256 || metadata.Profile.LaneID == "" || metadata.Profile.SelectedRows != len(userProvisioningDeterministicMissingIDs) {
		t.Fatalf("fixture metadata = %#v", metadata)
	}
}

func TestUserProvisioningDeterministicTailHasExactLocalEvidence(t *testing.T) {
	root := filepath.Join("..", "..")
	path := filepath.Join(root, "docs", "fixtures", userProvisioningDeterministicTailFixture)
	fixture, err := compat.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Command.Kind != "test" || len(fixture.Source) != 1 || len(fixture.Command.Args) != 0 {
		t.Fatalf("fixture envelope = %#v", fixture)
	}
	if result, err := compat.Run(fixture); err != nil || !result.OK {
		t.Fatalf("fixture execution = %#v, error = %v", result, err)
	}
	if source := fixture.Source[0].Content; !strings.Contains(source, "class ConcreteFlow extends UserProvisioning.FlowProvisionBase") || !strings.Contains(source, "new ConcreteFlow()") || strings.Contains(source, "new UserProvisioning.FlowProvisionBase(") {
		t.Fatalf("flow subclass witness = %q", source)
	}

	paths, err := filepath.Glob(filepath.Join(root, "docs", "fixtures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := BuildEvidenceSnapshot(paths)
	if err != nil {
		t.Fatal(err)
	}
	assertExactSurfaceSet(t, evidenceRowsForIDs(evidence, userProvisioningDeterministicTailIDs), userProvisioningDeterministicTailIDs)
	want := map[string]bool{}
	for _, id := range userProvisioningDeterministicTailIDs {
		want[id] = true
	}
	for _, row := range evidence {
		if !want[row.SurfaceID] {
			continue
		}
		if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported || len(row.Sources) != 1 || row.Sources[0] != "fixture:"+strings.TrimSuffix(userProvisioningDeterministicTailFixture, ".json") {
			t.Fatalf("%s evidence row = %#v", row.SurfaceID, row)
		}
	}
	openRows := evidenceRowsForFixture(evidence, userProvisioningOpenTailIDs, "core-runtime-userprovisioning-tail-evidence")
	assertExactSurfaceSet(t, openRows, userProvisioningOpenTailIDs)
	for _, row := range openRows {
		if row.GladeBehavior != BehaviorNone || row.Evidence != EvidenceFixture {
			t.Fatalf("rejected UserProvisioning row is not shape-only: %#v", row)
		}
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var metadata struct {
		APIVersion                string `json:"apiVersion"`
		Mode                      string `json:"mode"`
		SalesforceEligible        *bool  `json:"salesforceEligible"`
		SalesforceExclusionClass  string `json:"salesforceExclusionClass"`
		SalesforceExclusionReason string `json:"salesforceExclusionReason"`
		Candidate                 struct {
			Commit string `json:"commit"`
			SHA256 string `json:"sha256"`
		} `json:"candidate"`
		Profile struct {
			CandidateCommit string `json:"candidateCommit"`
			CandidateSHA256 string `json:"candidateSha256"`
			LaneID          string `json:"laneId"`
			SelectedRows    int    `json:"selectedRowCount"`
		} `json:"profile"`
	}
	if err := json.Unmarshal(raw, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.APIVersion != "67.0" || metadata.Mode != "deterministic-mock" || metadata.SalesforceEligible == nil || *metadata.SalesforceEligible || metadata.SalesforceExclusionClass != "policy-local-only" || !strings.Contains(strings.ToLower(metadata.SalesforceExclusionReason), "no hosted") || metadata.Candidate.Commit != userProvisioningCandidateCommit || metadata.Candidate.SHA256 != userProvisioningCandidateSHA256 || metadata.Profile.CandidateCommit != metadata.Candidate.Commit || metadata.Profile.CandidateSHA256 != metadata.Candidate.SHA256 || metadata.Profile.LaneID == "" || metadata.Profile.SelectedRows != len(userProvisioningDeterministicTailIDs) {
		t.Fatalf("fixture metadata = %#v", metadata)
	}
}

func evidenceRowsForIDs(rows []SurfaceLedgerRow, ids []string) []SurfaceLedgerRow {
	want := make(map[string]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	selected := make([]SurfaceLedgerRow, 0, len(ids))
	for _, row := range rows {
		if want[row.SurfaceID] {
			selected = append(selected, row)
		}
	}
	return selected
}

func evidenceRowsForFixture(rows []SurfaceLedgerRow, ids []string, fixture string) []SurfaceLedgerRow {
	selected := evidenceRowsForIDs(rows, ids)
	owned := make([]SurfaceLedgerRow, 0, len(selected))
	for _, row := range selected {
		if len(row.Sources) == 1 && row.Sources[0] == "fixture:"+fixture {
			owned = append(owned, row)
		}
	}
	return owned
}
