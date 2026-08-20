package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

const systemGlobalTypeShapesFixture = "core-runtime-system-global-type-shapes"

var systemGlobalTypeShapesIDs = []string{
	"apex:System.ActivationPlatformStatusEnum",
	"apex:System.AuthConfig",
	"apex:System.AuthProvider",
	"apex:System.Communities",
	"apex:System.CompressionFormatEnum",
	"apex:System.ConnectedApplication",
	"apex:System.ContactPointPrefEnum",
	"apex:System.ContactPointTypeRepresentationEnum",
	"apex:System.ContentDocumentLink",
	"apex:System.CronJobDetail",
	"apex:System.CronTrigger",
	"apex:System.Custom",
	"apex:System.CustomizationType",
}

func TestSystemGlobalTypeShapesHasExactLocalFixtureOwnership(t *testing.T) {
	root := filepath.Join("..", "..")
	fixturePath := filepath.Join(root, "docs", "fixtures", systemGlobalTypeShapesFixture+".json")
	fixture, err := compat.LoadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Name != systemGlobalTypeShapesFixture || fixture.Command.Kind != "check" || len(fixture.Command.Args) != 0 || len(fixture.Source) != 1 || fixture.Source[0].Path == "" {
		t.Fatalf("fixture envelope = %#v", fixture)
	}
	if len(fixture.Evidence) != len(systemGlobalTypeShapesIDs) {
		t.Fatalf("raw evidence rows = %d, want %d", len(fixture.Evidence), len(systemGlobalTypeShapesIDs))
	}
	want := make(map[string]bool, len(systemGlobalTypeShapesIDs))
	for _, id := range systemGlobalTypeShapesIDs {
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
	assertExactSurfaceSet(t, evidence, systemGlobalTypeShapesIDs)
	for _, row := range evidence {
		if row.Evidence != EvidenceFixture || row.GladeShape != ShapeTypeKnown || row.GladeBehavior != BehaviorNone || len(row.Sources) != 1 || row.Sources[0] != "fixture:"+systemGlobalTypeShapesFixture {
			t.Fatalf("%s evidence/shape/behavior/source = %s/%s/%s/%v", row.SurfaceID, row.Evidence, row.GladeShape, row.GladeBehavior, row.Sources)
		}
	}

	source := fixture.Source[0].Content
	for _, witness := range []string{
		"ActivationPlatformStatusEnum activationPlatformStatusEnum;",
		"AuthConfig authConfig;",
		"AuthProvider authProvider;",
		"Communities communities;",
		"CompressionFormatEnum compressionFormatEnum;",
		"ConnectedApplication connectedApplication;",
		"ContactPointPrefEnum contactPointPrefEnum;",
		"ContactPointTypeRepresentationEnum contactPointTypeRepresentationEnum;",
		"ContentDocumentLink contentDocumentLink;",
		"CronJobDetail cronJobDetail;",
		"CronTrigger cronTrigger;",
		"Custom custom;",
		"CustomizationType customizationType;",
	} {
		if !strings.Contains(source, witness) {
			t.Fatalf("source missing direct unqualified declaration %q", witness)
		}
	}

	paths, err := filepath.Glob(filepath.Join(root, "docs", "fixtures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	ownership := make(map[string]int, len(want))
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
				ownership[item.SurfaceID]++
			}
		}
	}
	for _, id := range systemGlobalTypeShapesIDs {
		if ownership[id] != 1 {
			t.Fatalf("fixture ownership for %s = %d, want exactly one", id, ownership[id])
		}
	}

	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	var policy struct {
		Mode                      string `json:"mode"`
		SalesforceEligible        *bool  `json:"salesforceEligible"`
		SalesforceExclusionClass  string `json:"salesforceExclusionClass"`
		SalesforceExclusionReason string `json:"salesforceExclusionReason"`
	}
	if err := json.Unmarshal(data, &policy); err != nil {
		t.Fatal(err)
	}
	if policy.Mode != "compile-shape" || policy.SalesforceEligible == nil || *policy.SalesforceEligible || policy.SalesforceExclusionClass != "policy-local-only" || !strings.Contains(policy.SalesforceExclusionReason, "zero parity") || !strings.Contains(policy.SalesforceExclusionReason, "identity") {
		t.Fatalf("fixture policy = %#v", policy)
	}
	if result, err := compat.Run(fixture); err != nil || !result.OK {
		t.Fatalf("fixture check = %#v, error = %v", result, err)
	}
}
