package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func canvasCloseoutOwners() map[string]string {
	owners := map[string]string{
		"apex:Canvas.ContextTypeEnum.USER":                  "current-base-canvas-001-deterministic-api67",
		"apex:Canvas.ContextTypeEnum.equals(Object)":        "current-base-canvas-001-deterministic-api67",
		"apex:Canvas.ContextTypeEnum.hashCode()":            "current-base-canvas-001-deterministic-api67",
		"apex:Canvas.ContextTypeEnum.ordinal()":             "current-base-canvas-001-deterministic-api67",
		"apex:Canvas.ContextTypeEnum.valueOf(String)":       "current-base-canvas-001-deterministic-api67",
		"apex:Canvas.ContextTypeEnum.values()":              "current-base-canvas-001-deterministic-api67",
		"apex:Canvas.RenderContext.getApplicationContext()": "core-runtime-canvas-local-evidence",
		"apex:Canvas.RenderContext.getEnvironmentContext()": "core-runtime-canvas-local-evidence",
	}
	for _, method := range []string{"getCanvasUrl()", "getDeveloperName()", "getName()", "getNamespace()", "getVersion()", "setCanvasUrlPath(String)"} {
		owners["apex:Canvas.ApplicationContext."+method] = "core-runtime-canvas-local-evidence"
	}
	for _, method := range []string{"addEntityField(String)", "addEntityFields(Set<String>)", "getDisplayLocation()", "getEntityFields()", "getLocationUrl()", "getParametersAsJSON()", "getSublocation()", "setParametersAsJSON(String)"} {
		owners["apex:Canvas.EnvironmentContext."+method] = "core-runtime-canvas-local-evidence"
	}
	return owners
}

func TestCanvasCloseoutHasExactExecutableOwnership(t *testing.T) {
	root := filepath.Join("..", "..")
	want := canvasCloseoutOwners()
	if len(want) != 22 {
		t.Fatalf("Canvas closeout IDs = %d, want 22", len(want))
	}
	paths, err := filepath.Glob(filepath.Join(root, "docs", "fixtures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := BuildEvidenceSnapshot(paths)
	if err != nil {
		t.Fatal(err)
	}
	wantIDs := make([]string, 0, len(want))
	var selected []SurfaceLedgerRow
	for id := range want {
		wantIDs = append(wantIDs, id)
	}
	for _, row := range evidence {
		if _, ok := want[row.SurfaceID]; ok {
			selected = append(selected, row)
		}
	}
	assertExactSurfaceSet(t, selected, wantIDs)
	for _, row := range selected {
		owner := "fixture:" + want[row.SurfaceID]
		if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported || len(row.Sources) != 1 || row.Sources[0] != owner {
			t.Fatalf("%s evidence/behavior/source = %s/%s/%v", row.SurfaceID, row.Evidence, row.GladeBehavior, row.Sources)
		}
	}

	fixtures := []string{"core-runtime-canvas-local-evidence", "current-base-canvas-001-deterministic-api67"}
	sources := make(map[string]string, len(fixtures))
	for _, name := range fixtures {
		path := filepath.Join(root, "docs", "fixtures", name+".json")
		fixture, err := compat.LoadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := compat.Validate(fixture); err != nil {
			t.Fatal(err)
		}
		if result, err := compat.Run(fixture); err != nil || !result.OK {
			t.Fatalf("fixture %s execution = %#v, error = %v", name, result, err)
		}
		data, err := os.ReadFile(path)
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
		if policy.SalesforceEligible == nil || *policy.SalesforceEligible || policy.SalesforceExclusionClass != "policy-local-only" || !strings.Contains(policy.SalesforceExclusionReason, "parity") {
			t.Fatalf("fixture %s policy = %#v", name, policy)
		}
		var source strings.Builder
		for _, file := range fixture.Source {
			source.WriteString(file.Content)
		}
		sources[name] = source.String()
	}

	for _, witness := range []string{
		"Canvas.ApplicationContext application = ctx.getApplicationContext();",
		"application.setCanvasUrlPath('/apex/Changed');",
		"Canvas.EnvironmentContext environment = ctx.getEnvironmentContext();",
		"environment.addEntityField('Account.Name');",
		"environment.addEntityFields(new Set<String>{'Account.Id', 'Account.Type'});",
		"environment.setParametersAsJSON('{\"mode\":\"local\"}');",
	} {
		if !strings.Contains(sources[fixtures[0]], witness) {
			t.Fatalf("Canvas runtime source missing %q", witness)
		}
	}
	for _, witness := range []string{
		"Canvas.ContextTypeEnum user = Canvas.ContextTypeEnum.USER;",
		"user.equals(Canvas.ContextTypeEnum.ORGANIZATION)",
		"user.equals(null)",
		"Canvas.ContextTypeEnum.valueOf('USER')",
		"Canvas.ContextTypeEnum.values()",
	} {
		if !strings.Contains(sources[fixtures[1]], witness) {
			t.Fatalf("Canvas enum source missing %q", witness)
		}
	}
}
