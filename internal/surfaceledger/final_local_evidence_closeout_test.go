package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func TestFinalLocalEvidenceRowsHaveExactExecutableOwnership(t *testing.T) {
	wantOwners := map[string]string{
		"apex:Apex.Stack":                      "core-runtime-apex-stack-local-evidence",
		"apex:Apex.Stack.Stack()":              "core-runtime-apex-stack-local-evidence",
		"apex:Apex.Stack.clone()":              "core-runtime-apex-stack-local-evidence",
		"apex:ApexPages.Action.Action(String)": "current-base-apexpages-local-runtime-001-api67",
		"apex:System.Site.Site()":              "core-runtime-site-value-depth",
		"apex:System.Site.clone()":             "core-runtime-site-value-depth",
	}
	root := filepath.Join("..", "..")
	paths, err := filepath.Glob(filepath.Join(root, "docs", "fixtures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := BuildEvidenceSnapshot(paths)
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]int, len(wantOwners))
	for _, row := range rows {
		owner, ok := wantOwners[row.SurfaceID]
		if !ok {
			continue
		}
		seen[row.SurfaceID]++
		if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported || len(row.Sources) != 1 || row.Sources[0] != "fixture:"+owner {
			t.Fatalf("%s evidence row = %#v", row.SurfaceID, row)
		}
	}
	for id := range wantOwners {
		if seen[id] != 1 {
			t.Fatalf("fixture ownership for %s = %d, want exactly one", id, seen[id])
		}
	}

	checks := map[string][]string{
		"core-runtime-apex-stack-local-evidence": {
			"Apex.Stack stack = new Apex.Stack();",
			"Apex.Stack cloned = (Apex.Stack)stack.clone();",
			"System.assertEquals('two', cloned.peek());",
		},
		"current-base-apexpages-local-runtime-001-api67": {
			"ApexPages.Action action = new ApexPages.Action('{!list}');",
			"System.assertEquals(action.getExpression(), cloned.getExpression());",
		},
		"core-runtime-site-value-depth": {
			"Site site = new Site();",
			"Site clonedSite = (Site)site.clone();",
			"System.assertNotEquals(null, clonedSite);",
		},
	}
	for name, witnesses := range checks {
		fixturePath := filepath.Join(root, "docs", "fixtures", name+".json")
		fixture, err := compat.LoadFile(fixturePath)
		if err != nil {
			t.Fatal(err)
		}
		if err := compat.Validate(fixture); err != nil {
			t.Fatal(err)
		}
		if fixture.Command.Kind != "exec" || len(fixture.Source) != 1 || len(fixture.Command.Args) != 1 || fixture.Source[0].Content != fixture.Command.Args[0] {
			t.Fatalf("%s execution envelope = %#v", name, fixture)
		}
		if result, err := compat.Run(fixture); err != nil || !result.OK {
			t.Fatalf("run %s = %#v, error = %v", name, result, err)
		}
		for _, witness := range witnesses {
			if !strings.Contains(fixture.Source[0].Content, witness) {
				t.Fatalf("%s source missing %q", name, witness)
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
		if policy.SalesforceEligible == nil || *policy.SalesforceEligible || policy.SalesforceExclusionClass == "" || policy.SalesforceExclusionReason == "" {
			t.Fatalf("%s policy = %#v", name, policy)
		}
	}
}
