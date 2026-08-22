package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

const userProvisioningInheritedRuntimeFixture = "core-runtime-userprovisioning-inherited-tail-api67.json"

var userProvisioningInheritedRuntimeIDs = []string{
	"apex:UserProvisioning.FlowProvisionBase.getFlowName()",
	"apex:UserProvisioning.FlowProvisionBase.getFlowNamespace()",
	"apex:UserProvisioning.FlowProvisionBase.hasFlow()",
	"apex:UserProvisioning.FlowProvisionBase.hasFlowOrApex()",
	"apex:UserProvisioning.UserProvisioningPlugin.UserProvisioningPlugin()",
	"apex:UserProvisioning.UserProvisioningPlugin.buildDescribeCall()",
	"apex:UserProvisioning.UserProvisioningPlugin.describe()",
	"apex:UserProvisioning.UserProvisioningPlugin.getPluginClassName()",
}

func TestUserProvisioningInheritedRuntimeHasExactCandidateEvidence(t *testing.T) {
	root := filepath.Join("..", "..")
	path := filepath.Join(root, "docs", "fixtures", userProvisioningInheritedRuntimeFixture)
	fixture, err := compat.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Name != strings.TrimSuffix(userProvisioningInheritedRuntimeFixture, ".json") || fixture.Command.Kind != "test" || len(fixture.Command.Args) != 0 || len(fixture.Source) != 2 || fixture.Project.SourceAPIVersion != "67.0" {
		t.Fatalf("fixture envelope = %#v", fixture)
	}
	if result, err := compat.Run(fixture); err != nil || !result.OK {
		t.Fatalf("fixture execution = %#v, error = %v", result, err)
	}

	want := mapFromIDs(userProvisioningInheritedRuntimeIDs)
	seen := make(map[string]bool, len(want))
	for _, row := range fixture.Evidence {
		if row.Kind != "test" || !want[row.SurfaceID] || seen[row.SurfaceID] {
			t.Fatalf("unexpected or duplicate evidence row = %#v", row)
		}
		seen[row.SurfaceID] = true
	}
	if len(seen) != len(want) || len(fixture.Evidence) != len(want) {
		t.Fatalf("evidence ownership = %d rows, want exactly %d", len(seen), len(want))
	}
	paths, err := filepath.Glob(filepath.Join(root, "docs", "fixtures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := BuildEvidenceSnapshot(paths)
	if err != nil {
		t.Fatal(err)
	}
	rows := evidenceRowsForFixture(snapshot, userProvisioningInheritedRuntimeIDs, strings.TrimSuffix(userProvisioningInheritedRuntimeFixture, ".json"))
	assertExactSurfaceSet(t, rows, userProvisioningInheritedRuntimeIDs)
	for _, row := range rows {
		if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported {
			t.Fatalf("%s registry evidence = %#v", row.SurfaceID, row)
		}
	}
	owners := make(map[string]int, len(want))
	for _, candidate := range paths {
		var header struct {
			EvidenceOnly bool            `json:"evidenceOnly"`
			Profile      json.RawMessage `json:"profile"`
			Evidence     []struct {
				Kind      string `json:"kind"`
				SurfaceID string `json:"surfaceId"`
			} `json:"evidence"`
		}
		readJSON(t, candidate, &header)
		if header.EvidenceOnly || len(header.Profile) == 0 || header.Profile[0] != '{' {
			continue
		}
		for _, row := range header.Evidence {
			if row.Kind == "test" && want[row.SurfaceID] {
				owners[row.SurfaceID]++
			}
		}
	}
	for _, id := range userProvisioningInheritedRuntimeIDs {
		if owners[id] != 1 {
			t.Fatalf("exact executable ownership for %s = %d, want 1", id, owners[id])
		}
	}

	source := fixture.Source[0].Content + fixture.Source[1].Content
	for _, witness := range []string{
		"extends UserProvisioning.FlowProvisionBase",
		"super('')",
		"implements Database.Batchable<SObject>",
		"extends UserProvisioning.UserProvisioningPlugin",
		"super()",
		"override Process.PluginDescribeResult buildDescribeCall()",
		"override Process.PluginResult invoke(Process.PluginRequest request)",
		"System.assertEquals(null, flow.getFlowName())",
		"System.assertEquals(null, flow.getFlowNamespace())",
		"System.assertEquals(false, flow.hasFlow())",
		"System.assertEquals(false, flow.hasFlowOrApex())",
		"System.assertEquals(null, plugin.buildDescribeCall())",
		"System.assertNotEquals(null, plugin.describe())",
		"System.assertEquals('ConcretePlugin', plugin.getPluginClassName())",
	} {
		if !strings.Contains(source, witness) {
			t.Fatalf("source lacks direct witness %q", witness)
		}
	}
	if strings.Contains(source, "new UserProvisioning.UserProvisioningPlugin(") || strings.Contains(source, "new UserProvisioning.FlowProvisionBase(") {
		t.Fatal("source directly constructs an abstract UserProvisioning base")
	}

	var metadata struct {
		APIVersion      string `json:"apiVersion"`
		Mode            string `json:"mode"`
		EvidenceOnly    bool   `json:"evidenceOnly"`
		Eligible        *bool  `json:"salesforceEligible"`
		ExclusionClass  string `json:"salesforceExclusionClass"`
		ExclusionReason string `json:"salesforceExclusionReason"`
		Profile         struct {
			CandidateCommit string `json:"candidateCommit"`
			CandidateSHA256 string `json:"candidateSha256"`
			LaneID          string `json:"laneId"`
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
	if metadata.APIVersion != "67.0" || metadata.Mode != "local-runtime" || metadata.EvidenceOnly || metadata.Eligible == nil || *metadata.Eligible || metadata.ExclusionClass != "policy-local-only" || !strings.Contains(strings.ToLower(metadata.ExclusionReason), "zero hosted salesforce parity") || metadata.Profile.CandidateCommit != userProvisioningCandidateCommit || metadata.Profile.CandidateSHA256 != userProvisioningCandidateSHA256 || metadata.Profile.LaneID == "" || metadata.Profile.SelectedRows != len(userProvisioningInheritedRuntimeIDs) {
		t.Fatalf("fixture provenance = %#v", metadata)
	}
}
