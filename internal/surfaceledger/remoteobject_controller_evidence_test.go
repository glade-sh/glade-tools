package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

var remoteObjectControllerEvidenceIDs = []string{
	"apex:System.RemoteObjectController.create(String,Map<String,Object>)",
	"apex:System.RemoteObjectController.retrieve(String,List<String>,Map<String,Object>)",
	"apex:System.RemoteObjectController.update(String,List<String>,Map<String,Object>)",
	"apex:System.RemoteObjectController.del(String,List<String>)",
}

func TestRemoteObjectControllerLocalFixtureIsExecutableAndLocalOnly(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "fixtures", "core-runtime-remote-object-controller-local.json")
	fixture, err := compat.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var metadata struct {
		SalesforceEligible        *bool  `json:"salesforceEligible"`
		SalesforceExclusionClass  string `json:"salesforceExclusionClass"`
		SalesforceExclusionReason string `json:"salesforceExclusionReason"`
	}
	if err := json.Unmarshal(raw, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.SalesforceEligible == nil || *metadata.SalesforceEligible || metadata.SalesforceExclusionClass != "policy-local-only" || !strings.Contains(metadata.SalesforceExclusionReason, "zero Salesforce parity") {
		t.Fatalf("Salesforce policy = %#v", metadata)
	}
	if fixture.Command.Kind != "test" || len(fixture.Source) != 1 {
		t.Fatalf("fixture command/source = %q/%d", fixture.Command.Kind, len(fixture.Source))
	}
	for _, witness := range []string{"RemoteObjectController.create", "RemoteObjectController.retrieve", "RemoteObjectController.update", "RemoteObjectController.del", "invalidId", "deleteRows[0].get('id')", "deleteRows[1].get('id')", "deleteRows[1].get('errors')", "'ids' => new List<String>{id, secondId}"} {
		assertSourceContains(t, fixture.Source[0].Content, witness)
	}
	result, err := compat.Run(fixture)
	if err != nil || !result.OK {
		t.Fatalf("fixture execution = %#v, error = %v", result, err)
	}
	rows, err := BuildEvidenceSnapshot([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	assertExactSurfaceSet(t, rows, remoteObjectControllerEvidenceIDs)
	for _, row := range rows {
		if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported {
			t.Fatalf("%s evidence/behavior = %s/%s, want fixture/supported", row.SurfaceID, row.Evidence, row.GladeBehavior)
		}
	}

	root := filepath.Join("..", "..")
	owners := make(map[string][]string, len(remoteObjectControllerEvidenceIDs))
	for _, id := range remoteObjectControllerEvidenceIDs {
		owners[id] = nil
	}
	paths, err := filepath.Glob(filepath.Join(root, "docs", "fixtures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, candidatePath := range paths {
		data, err := os.ReadFile(candidatePath)
		if err != nil {
			t.Fatalf("load fixture %s: %v", candidatePath, err)
		}
		var candidate struct {
			Evidence []compat.FixtureEvidence `json:"evidence"`
		}
		if err := json.Unmarshal(data, &candidate); err != nil {
			t.Fatalf("parse fixture %s: %v", candidatePath, err)
		}
		for _, row := range candidate.Evidence {
			if _, ok := owners[row.SurfaceID]; ok && strings.EqualFold(row.Kind, "test") {
				owners[row.SurfaceID] = append(owners[row.SurfaceID], filepath.Base(candidatePath))
			}
		}
	}
	for id, paths := range owners {
		if len(paths) != 1 || paths[0] != filepath.Base(path) {
			t.Fatalf("%s fixture owners = %v, want only %s", id, paths, filepath.Base(path))
		}
	}

	gladeByID := rowsByID(BuildGladeSnapshot())
	wantGladeBehavior := map[string]BehaviorState{
		remoteObjectControllerEvidenceIDs[0]: BehaviorStubNoOp,
		remoteObjectControllerEvidenceIDs[1]: BehaviorStubNoOp,
		remoteObjectControllerEvidenceIDs[2]: BehaviorUnsupported,
		remoteObjectControllerEvidenceIDs[3]: BehaviorStubNoOp,
	}
	for _, id := range remoteObjectControllerEvidenceIDs {
		row, ok := gladeByID[id]
		if !ok || row.GladeBehavior != wantGladeBehavior[id] {
			t.Fatalf("Glade row %s = %#v, want current %s classification", id, row, wantGladeBehavior[id])
		}
	}
	policy, err := LoadSupportPolicy(filepath.Join(root, "docs", "fixtures", "apex-local-support-policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	ledger := Merge(nil, nil, BuildGladeSnapshot(), rows)
	mergedByID := rowsByID(ledger.Rows)
	for _, id := range remoteObjectControllerEvidenceIDs {
		row, ok := mergedByID[id]
		if !ok || row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported || row.GapClass != "" {
			t.Fatalf("merged row %s = %#v, want fixture evidence/supported behavior", id, row)
		}
	}
	profile := ComputeSupportProfile(ledger.Rows, policy, nil)
	for _, id := range remoteObjectControllerEvidenceIDs {
		var found SupportProfileRow
		for _, row := range profile.Rows {
			if row.SurfaceID == id {
				found = row
				break
			}
		}
		if found.SurfaceID == "" || found.Disposition != DispositionCompileShapeRequired || found.GapClass != "" {
			t.Fatalf("%s profile = disposition:%s gap:%s, want compile-shape-required/no-gap (zero oracle)", id, found.Disposition, found.GapClass)
		}
	}
}
