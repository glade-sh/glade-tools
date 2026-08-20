package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

const dataWeaveShapeDepthFixture = "core-runtime-dataweave-object-depth"

var finalCompileShapeDepthIDs = []string{
	"apex:Package.Version",
	"apex:dataweave.Result",
	"apex:dataweave.Result.clone()",
	"apex:dataweave.Result.getValue()",
	"apex:dataweave.Result.getValueAsString()",
	"apex:dataweave.Result.toString()",
	"apex:dataweave.Script",
	"apex:dataweave.Script.clone()",
	"apex:dataweave.Script.toString()",
}

func TestFinalCompileShapeDepthHasExactLocalFixtureOwnership(t *testing.T) {
	root := filepath.Join("..", "..")
	dataWeavePath := filepath.Join(root, "docs", "fixtures", dataWeaveShapeDepthFixture+".json")
	packagePath := filepath.Join(root, "docs", "fixtures", "core-package-version-compile-shape.json")
	dataWeave, err := compat.LoadFile(dataWeavePath)
	if err != nil {
		t.Fatal(err)
	}
	packageVersion, err := compat.LoadFile(packagePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []compat.Fixture{dataWeave, packageVersion} {
		if err := compat.Validate(fixture); err != nil {
			t.Fatal(err)
		}
	}
	if dataWeave.Name != dataWeaveShapeDepthFixture || dataWeave.Command.Kind != "exec" || len(dataWeave.Command.Args) != 1 || len(dataWeave.Source) != 1 || dataWeave.Source[0].Content != dataWeave.Command.Args[0] {
		t.Fatalf("DataWeave fixture envelope = %#v", dataWeave)
	}

	want := make(map[string]bool, len(finalCompileShapeDepthIDs))
	for _, id := range finalCompileShapeDepthIDs {
		want[id] = true
	}
	evidence, err := BuildEvidenceSnapshot([]string{dataWeavePath, packagePath})
	if err != nil {
		t.Fatal(err)
	}
	selected := make([]SurfaceLedgerRow, 0, len(want))
	for _, row := range evidence {
		if want[row.SurfaceID] {
			selected = append(selected, row)
		}
	}
	assertExactSurfaceSet(t, selected, finalCompileShapeDepthIDs)
	for _, row := range selected {
		if row.Evidence != EvidenceFixture || len(row.Sources) != 1 {
			t.Fatalf("%s evidence/source = %s/%v", row.SurfaceID, row.Evidence, row.Sources)
		}
		if row.SurfaceID == "apex:Package.Version" {
			if row.GladeShape != ShapeTypeKnown || row.GladeBehavior != BehaviorNone || row.Sources[0] != "fixture:core-package-version-compile-shape" {
				t.Fatalf("Package.Version row = %#v", row)
			}
		} else if row.GladeBehavior != BehaviorSupported || row.Sources[0] != "fixture:"+dataWeaveShapeDepthFixture {
			t.Fatalf("%s behavior/source = %s/%v", row.SurfaceID, row.GladeBehavior, row.Sources)
		}
	}

	source := dataWeave.Source[0].Content
	for _, witness := range []string{
		"dataweave.Script script = dataweave.Script.createScript('helloWorld');",
		"Object scriptClone = script.clone();",
		"System.assertNotEquals(script, scriptClone);",
		"String scriptText = script.toString();",
		"dataweave.Result result = script.execute();",
		"Object value = result.getValue();",
		"String valueText = result.getValueAsString();",
		"Object resultClone = result.clone();",
		"System.assertNotEquals(result, resultClone);",
		"String resultText = result.toString();",
	} {
		if !strings.Contains(source, witness) {
			t.Fatalf("source missing direct executable witness %q", witness)
		}
	}
	if !strings.Contains(packageVersion.Source[0].Content, "Package.Version value = (Package.Version)null;") {
		t.Fatal("Package.Version fixture lacks its direct type witness")
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
	for _, id := range finalCompileShapeDepthIDs {
		if owners[id] != 1 {
			t.Fatalf("fixture ownership for %s = %d, want exactly one", id, owners[id])
		}
	}

	data, err := os.ReadFile(dataWeavePath)
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
	if policy.SalesforceEligible == nil || *policy.SalesforceEligible || policy.SalesforceExclusionClass != "policy-local-only" || !strings.Contains(policy.SalesforceExclusionReason, "zero Salesforce parity") || !strings.Contains(policy.SalesforceExclusionReason, "local DataWeave") {
		t.Fatalf("DataWeave fixture policy = %#v", policy)
	}
	for _, fixture := range []compat.Fixture{dataWeave, packageVersion} {
		if result, err := compat.Run(fixture); err != nil || !result.OK {
			t.Fatalf("fixture %s = %#v, error = %v", fixture.Name, result, err)
		}
	}
}
