package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

const pushUpgradeShapeDepthFixture = "core-runtime-push-upgrade-customization-repository-shape-depth"

var pushUpgradeShapeDepthIDs = []string{
	"apex:PushUpgradeCustomizationRepository",
	"apex:PushUpgradeCustomizationRepository.PushUpgradeCustomizationRepository()",
	"apex:PushUpgradeCustomizationRepository.clone()",
	"apex:PushUpgradeCustomizationRepository.create(String,String,Boolean)",
	"apex:PushUpgradeCustomizationRepository.deleteById(String)",
	"apex:PushUpgradeCustomizationRepository.deleteByIndex(String,String)",
	"apex:PushUpgradeCustomizationRepository.getCustomUpgradeAllowedForId(String)",
	"apex:PushUpgradeCustomizationRepository.getCustomUpgradeAllowedForIndex(String,String)",
	"apex:PushUpgradeCustomizationRepository.getCustomUpgradeTypeForId(String)",
	"apex:PushUpgradeCustomizationRepository.getCustomUpgradeTypeForIndex(String,String)",
	"apex:PushUpgradeCustomizationRepository.setCustomUpgradeAllowedForId(String,Boolean)",
	"apex:PushUpgradeCustomizationRepository.setCustomUpgradeAllowedForIndex(String,String,Boolean)",
}

func TestPushUpgradeCustomizationRepositoryShapeDepthHasExactLocalFixtureOwnership(t *testing.T) {
	root := filepath.Join("..", "..")
	fixturePath := filepath.Join(root, "docs", "fixtures", pushUpgradeShapeDepthFixture+".json")
	fixture, err := compat.LoadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Name != pushUpgradeShapeDepthFixture || fixture.Command.Kind != "check" || len(fixture.Command.Args) != 0 || len(fixture.Source) != 1 {
		t.Fatalf("fixture envelope = %#v", fixture)
	}
	if len(fixture.Evidence) != len(pushUpgradeShapeDepthIDs) {
		t.Fatalf("raw evidence rows = %d, want %d", len(fixture.Evidence), len(pushUpgradeShapeDepthIDs))
	}
	want := make(map[string]bool, len(pushUpgradeShapeDepthIDs))
	for _, id := range pushUpgradeShapeDepthIDs {
		want[id] = true
	}
	for _, item := range fixture.Evidence {
		if !want[item.SurfaceID] || item.Kind != "shape" {
			t.Fatalf("unexpected evidence row = %#v", item)
		}
	}

	evidence, err := BuildEvidenceSnapshot([]string{fixturePath})
	if err != nil {
		t.Fatal(err)
	}
	assertExactSurfaceSet(t, evidence, pushUpgradeShapeDepthIDs)
	for _, row := range evidence {
		if row.Evidence != EvidenceFixture || row.GladeShape != ShapeTypeKnown || row.GladeBehavior != BehaviorNone || len(row.Sources) != 1 || row.Sources[0] != "fixture:"+pushUpgradeShapeDepthFixture {
			t.Fatalf("%s evidence/shape/behavior/source = %s/%s/%s/%v", row.SurfaceID, row.Evidence, row.GladeShape, row.GladeBehavior, row.Sources)
		}
	}

	source := fixture.Source[0].Content
	for _, witness := range []string{
		"new PushUpgradeCustomizationRepository()",
		"Object cloned = repository.clone();",
		"String created = PushUpgradeCustomizationRepository.create(packageId, subscriberOrgId, allowed);",
		"PushUpgradeCustomizationRepository.deleteById(recordId);",
		"PushUpgradeCustomizationRepository.deleteByIndex(packageId, subscriberOrgId);",
		"Boolean allowedById = PushUpgradeCustomizationRepository.getCustomUpgradeAllowedForId(recordId);",
		"Boolean allowedByIndex = PushUpgradeCustomizationRepository.getCustomUpgradeAllowedForIndex(packageId, subscriberOrgId);",
		"CustomizationType typeById = PushUpgradeCustomizationRepository.getCustomUpgradeTypeForId(recordId);",
		"CustomizationType typeByIndex = PushUpgradeCustomizationRepository.getCustomUpgradeTypeForIndex(packageId, subscriberOrgId);",
		"PushUpgradeCustomizationRepository.setCustomUpgradeAllowedForId(recordId, allowed);",
		"PushUpgradeCustomizationRepository.setCustomUpgradeAllowedForIndex(packageId, subscriberOrgId, allowed);",
	} {
		if !strings.Contains(source, witness) {
			t.Fatalf("source missing direct shape witness %q", witness)
		}
	}

	paths, err := filepath.Glob(filepath.Join(root, "docs", "fixtures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	owners := make(map[string]int, len(want))
	for _, path := range paths {
		data, err := os.ReadFile(path)
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
			if want[item.SurfaceID] {
				owners[item.SurfaceID]++
			}
		}
	}
	for _, id := range pushUpgradeShapeDepthIDs {
		if owners[id] != 1 {
			t.Fatalf("fixture ownership for %s = %d, want exactly one", id, owners[id])
		}
	}

	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	var policy struct {
		SalesforceEligible        *bool  `json:"salesforceEligible"`
		SalesforceExclusionClass  string `json:"salesforceExclusionClass"`
		SalesforceExclusionReason string `json:"salesforceExclusionReason"`
	}
	if err := json.Unmarshal(data, &policy); err != nil {
		t.Fatal(err)
	}
	if policy.SalesforceEligible == nil || *policy.SalesforceEligible || policy.SalesforceExclusionClass != "policy-local-only" || !strings.Contains(policy.SalesforceExclusionReason, "zero Salesforce parity") || !strings.Contains(policy.SalesforceExclusionReason, "subscriber package") {
		t.Fatalf("fixture policy = %#v", policy)
	}
	if result, err := compat.Run(fixture); err != nil || !result.OK {
		t.Fatalf("fixture check = %#v, error = %v", result, err)
	}

	unsupportedPath := filepath.Join(root, "docs", "fixtures", "core-runtime-push-upgrade-customization-repository-unsupported.json")
	unsupported, err := compat.LoadFile(unsupportedPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(unsupported.Evidence) != 0 {
		t.Fatalf("runtime boundary fixture retains stale evidence aliases = %#v", unsupported.Evidence)
	}
	if result, err := compat.Run(unsupported); err != nil || !result.OK {
		t.Fatalf("unsupported runtime boundary = %#v, error = %v", result, err)
	}
}
