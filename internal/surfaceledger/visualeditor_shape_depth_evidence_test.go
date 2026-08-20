package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

const visualEditorShapeDepthFixture = "core-runtime-visualeditor-shape-depth"

var visualEditorShapeDepthIDs = []string{
	"apex:VisualEditor.DataRow.clone()",
	"apex:VisualEditor.DesignTimePageContext",
	"apex:VisualEditor.DesignTimePageContext.DesignTimePageContext()",
	"apex:VisualEditor.DesignTimePageContext.clone()",
	"apex:VisualEditor.DesignTimePageContext.entityName",
	"apex:VisualEditor.DesignTimePageContext.pageType",
	"apex:VisualEditor.DynamicPickList.clone()",
	"apex:VisualEditor.DynamicPickList.getDefaultValue()",
	"apex:VisualEditor.DynamicPickList.getLabel(Object)",
	"apex:VisualEditor.DynamicPickList.getValues()",
	"apex:VisualEditor.DynamicPickList.getValuesForSemanticSearch(String)",
	"apex:VisualEditor.DynamicPickList.isValid(Object)",
	"apex:VisualEditor.DynamicPickListRows.clone()",
}

func TestVisualEditorShapeDepthHasExactLocalFixtureOwnership(t *testing.T) {
	root := filepath.Join("..", "..")
	fixturePath := filepath.Join(root, "docs", "fixtures", visualEditorShapeDepthFixture+".json")
	fixture, err := compat.LoadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Name != visualEditorShapeDepthFixture || fixture.Command.Kind != "check" || len(fixture.Command.Args) != 0 || len(fixture.Source) != 1 {
		t.Fatalf("fixture envelope = %#v", fixture)
	}
	if len(fixture.Evidence) != len(visualEditorShapeDepthIDs) {
		t.Fatalf("raw evidence rows = %d, want %d", len(fixture.Evidence), len(visualEditorShapeDepthIDs))
	}
	want := make(map[string]bool, len(visualEditorShapeDepthIDs))
	for _, id := range visualEditorShapeDepthIDs {
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
	assertExactSurfaceSet(t, evidence, visualEditorShapeDepthIDs)
	for _, row := range evidence {
		if row.Evidence != EvidenceFixture || row.GladeShape != ShapeTypeKnown || row.GladeBehavior != BehaviorNone || len(row.Sources) != 1 || row.Sources[0] != "fixture:"+visualEditorShapeDepthFixture {
			t.Fatalf("%s evidence/shape/behavior/source = %s/%s/%s/%v", row.SurfaceID, row.Evidence, row.GladeShape, row.GladeBehavior, row.Sources)
		}
	}

	source := fixture.Source[0].Content
	for _, witness := range []string{
		"new VisualEditor.DataRow('label', 'value')",
		"dataRow.clone();",
		"new VisualEditor.DesignTimePageContext()",
		"String entityName = context.entityName;",
		"String pageType = context.pageType;",
		"context.clone();",
		"VisualEditor.DynamicPickList pickList = null;",
		"pickList.clone();",
		"Object attributeValue = null;",
		"String semanticSearchQuery = null;",
		"VisualEditor.DataRow defaultValue = pickList.getDefaultValue();",
		"String label = pickList.getLabel(attributeValue);",
		"VisualEditor.DynamicPickListRows values = pickList.getValues();",
		"VisualEditor.DynamicPickListRows semanticValues = pickList.getValuesForSemanticSearch(semanticSearchQuery);",
		"Boolean valid = pickList.isValid(attributeValue);",
		"new VisualEditor.DynamicPickListRows()",
		"rows.clone();",
	} {
		if !strings.Contains(source, witness) {
			t.Fatalf("source missing direct shape witness %q", witness)
		}
	}
	if strings.Contains(source, "new VisualEditor.DynamicPickList()") {
		t.Fatal("fixture must not construct the abstract DynamicPickList type")
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
	for _, id := range visualEditorShapeDepthIDs {
		if ownership[id] != 1 {
			t.Fatalf("fixture ownership for %s = %d, want exactly one", id, ownership[id])
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
	if policy.SalesforceEligible == nil || *policy.SalesforceEligible || policy.SalesforceExclusionClass != "policy-local-only" || !strings.Contains(policy.SalesforceExclusionReason, "zero Salesforce parity") || !strings.Contains(policy.SalesforceExclusionReason, "render") {
		t.Fatalf("fixture policy = %#v", policy)
	}
	if result, err := compat.Run(fixture); err != nil || !result.OK {
		t.Fatalf("fixture check = %#v, error = %v", result, err)
	}
}
