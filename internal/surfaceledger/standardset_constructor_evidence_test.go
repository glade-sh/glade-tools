package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

var standardSetControllerConstructorEvidenceIDs = []string{
	"apex:ApexPages.StandardSetController.StandardSetController(Database.QueryLocator)",
	"apex:ApexPages.StandardSetController.StandardSetController(List<Object>)",
}

func TestStandardSetControllerConstructorsHaveExecutableLocalEvidence(t *testing.T) {
	root := filepath.Join("..", "..")
	fixturePath := filepath.Join(root, "docs", "fixtures", "core-runtime-apexpages-standard-set-controller-evidence.json")
	fixture, err := compat.LoadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	var metadata struct {
		SalesforceEligible        *bool  `json:"salesforceEligible"`
		SalesforceExclusionClass  string `json:"salesforceExclusionClass"`
		SalesforceExclusionReason string `json:"salesforceExclusionReason"`
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.SalesforceEligible == nil || *metadata.SalesforceEligible || metadata.SalesforceExclusionClass != "policy-local-only" || !strings.Contains(metadata.SalesforceExclusionReason, "zero Salesforce parity") {
		t.Fatalf("Salesforce policy = %#v", metadata)
	}

	if result, err := compat.Run(fixture); err != nil || !result.OK {
		t.Fatalf("fixture execution = %#v, error = %v", result, err)
	}
	evidence, err := BuildEvidenceSnapshot([]string{fixturePath})
	if err != nil {
		t.Fatal(err)
	}
	evidenceByID := rowsByID(evidence)
	for _, id := range standardSetControllerConstructorEvidenceIDs {
		row, ok := evidenceByID[id]
		if !ok {
			t.Fatalf("fixture evidence missing %s", id)
		}
		if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported {
			t.Fatalf("%s evidence/behavior = %s/%s, want fixture/supported", row.SurfaceID, row.Evidence, row.GladeBehavior)
		}
	}
	owners := make(map[string][]string, len(standardSetControllerConstructorEvidenceIDs))
	for _, id := range standardSetControllerConstructorEvidenceIDs {
		owners[id] = nil
	}
	paths, err := filepath.Glob(filepath.Join(root, "docs", "fixtures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("load fixture %s: %v", path, err)
		}
		var candidate struct {
			Evidence []compat.FixtureEvidence `json:"evidence"`
		}
		if err := json.Unmarshal(data, &candidate); err != nil {
			t.Fatalf("parse fixture %s: %v", path, err)
		}
		for _, row := range candidate.Evidence {
			if _, ok := owners[row.SurfaceID]; ok {
				owners[row.SurfaceID] = append(owners[row.SurfaceID], filepath.Base(path))
			}
		}
	}
	for id, paths := range owners {
		if len(paths) != 1 || paths[0] != filepath.Base(fixturePath) {
			t.Fatalf("%s fixture owners = %v, want only %s", id, paths, filepath.Base(fixturePath))
		}
	}

	gladeByID := rowsByID(BuildGladeSnapshot())
	wantParameters := map[string][]string{
		standardSetControllerConstructorEvidenceIDs[0]: {"Database.QueryLocator"},
		standardSetControllerConstructorEvidenceIDs[1]: {"List<Object>"},
	}
	for _, id := range standardSetControllerConstructorEvidenceIDs {
		row, ok := gladeByID[id]
		if !ok {
			t.Fatalf("Glade snapshot missing %s", id)
		}
		if row.MemberName != "StandardSetController" || !reflect.DeepEqual(row.Parameters, wantParameters[id]) || row.GladeShape != ShapeSignatureKnown || row.GladeBehavior != BehaviorSupported || !containsString(row.Sources, "standard-symbols") {
			t.Fatalf("Glade row %s = %#v, want StandardSetController/%v/signature-known/supported/standard-symbols", id, row, wantParameters[id])
		}
	}

	policy, err := LoadSupportPolicy(filepath.Join(root, "docs", "fixtures", "apex-local-support-policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	ledger := Merge(nil, nil, BuildGladeSnapshot(), evidence)
	mergedByID := rowsByID(ledger.Rows)
	for _, id := range standardSetControllerConstructorEvidenceIDs {
		row, ok := mergedByID[id]
		if !ok || row.Evidence != EvidenceFixture || row.GapClass != "" {
			t.Fatalf("merged row %s = %#v, want fixture evidence/no gap", id, row)
		}
	}
	profile := ComputeSupportProfile(ledger.Rows, policy, nil)
	for _, id := range standardSetControllerConstructorEvidenceIDs {
		var row SupportProfileRow
		found := false
		for _, candidate := range profile.Rows {
			if candidate.SurfaceID == id {
				row = candidate
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("support profile missing %s", id)
		}
		if row.Disposition != DispositionLocalRuntimeRequired {
			t.Fatalf("%s profile = disposition:%s gap:%s, want local-runtime-required", id, row.Disposition, row.GapClass)
		}
	}
}
