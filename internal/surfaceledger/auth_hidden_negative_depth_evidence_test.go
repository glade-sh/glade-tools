package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/internal/project"
	"github.com/glade-sh/glade/internal/schema"
	"github.com/glade-sh/glade/internal/sema"
	"github.com/glade-sh/glade/internal/typesys"
	"github.com/glade-sh/glade/tools/internal/compat"
)

const authHiddenNegativeDepthFixture = "core-runtime-g3-auth-hidden-negative-depth.json"

type authHiddenNegativeCase struct {
	id      string
	path    string
	source  string
	message string
	start   int
	end     int
}

var authHiddenNegativeCases = []authHiddenNegativeCase{
	{
		id:      "apex:Auth.AuthProviderPluginClass.getCustomMetadataType()",
		path:    "force-app/main/default/classes/AuthHiddenCustomMetadataProbe.cls",
		source:  `Auth.AuthProviderPluginClass value = null; Object result = value.getCustomMetadataType();`,
		message: `method "execute" uses unsupported local feature "Auth.AuthProviderPluginClass.getCustomMetadataType"`,
		start:   113,
		end:     140,
	},
	{
		id:      "apex:Auth.AuthProviderPluginClass.getUserInfo(Map<String,String>,Auth.AuthProviderTokenResponse)",
		path:    "force-app/main/default/classes/AuthHiddenUserInfoProbe.cls",
		source:  `Auth.AuthProviderPluginClass value = null; Object result = value.getUserInfo(null, null);`,
		message: `method "execute" uses unsupported local feature "Auth.AuthProviderPluginClass.getUserInfo"`,
		start:   107,
		end:     124,
	},
	{
		id:      "apex:Auth.AuthProviderPluginClass.initiate(Map<String,String>,String)",
		path:    "force-app/main/default/classes/AuthHiddenInitiateProbe.cls",
		source:  `Auth.AuthProviderPluginClass value = null; Object result = value.initiate(null, null);`,
		message: `method "execute" uses unsupported local feature "Auth.AuthProviderPluginClass.initiate"`,
		start:   107,
		end:     121,
	},
	{
		id:      "apex:Auth.ConnectedAppPlugin.customAttributes(Id,Id,Map<String,String>)",
		path:    "force-app/main/default/classes/AuthHiddenCustomAttributesProbe.cls",
		source:  `Auth.ConnectedAppPlugin value = null; Object result = value.customAttributes(null, null, null);`,
		message: `method "execute" uses unsupported local feature "Auth.ConnectedAppPlugin.customAttributes"`,
		start:   110,
		end:     132,
	},
}

func TestAuthHiddenNegativeDepthHasExactLocalFixtureOwnership(t *testing.T) {
	root := filepath.Join("..", "..")
	fixturePath := filepath.Join(root, "docs", "fixtures", authHiddenNegativeDepthFixture)
	fixture, err := compat.LoadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Name != strings.TrimSuffix(authHiddenNegativeDepthFixture, ".json") || fixture.Command.Kind != "check" || len(fixture.Source) != len(authHiddenNegativeCases) {
		t.Fatalf("fixture execution envelope = %#v", fixture)
	}
	if len(fixture.Evidence) != len(authHiddenNegativeCases) {
		t.Fatalf("raw evidence rows = %d, want %d", len(fixture.Evidence), len(authHiddenNegativeCases))
	}
	want := make(map[string]bool, len(authHiddenNegativeCases))
	byPath := make(map[string]authHiddenNegativeCase, len(authHiddenNegativeCases))
	for _, testCase := range authHiddenNegativeCases {
		want[testCase.id] = true
		byPath[testCase.path] = testCase
	}
	counts := make(map[string]int, len(fixture.Evidence))
	for _, item := range fixture.Evidence {
		counts[item.SurfaceID]++
		if !want[item.SurfaceID] || item.Kind != "unsupported" {
			t.Fatalf("unexpected evidence row = %#v", item)
		}
	}
	for _, testCase := range authHiddenNegativeCases {
		if counts[testCase.id] != 1 {
			t.Fatalf("%s raw rows = %d, want exactly one", testCase.id, counts[testCase.id])
		}
	}

	for _, source := range fixture.Source {
		testCase, ok := byPath[source.Path]
		if !ok || !strings.Contains(source.Content, testCase.source) {
			t.Fatalf("unexpected source = %#v", source)
		}
		temp := t.TempDir()
		sourcePath := filepath.Join(temp, source.Path)
		if err := os.MkdirAll(filepath.Dir(sourcePath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(sourcePath, []byte(source.Content), 0o644); err != nil {
			t.Fatal(err)
		}
		result := sema.Analyze(typesys.Build(project.Project{Root: temp, ApexFiles: []string{sourcePath}}, schema.Schema{}))
		if len(result.Diagnostics) != 1 {
			t.Fatalf("%s diagnostics = %#v, want exactly one", testCase.id, result.Diagnostics)
		}
		diagnostic := result.Diagnostics[0]
		if diagnostic.Code != "GLADESEMA028" || diagnostic.Message != testCase.message || filepath.Base(diagnostic.File) != filepath.Base(source.Path) || diagnostic.Range == nil || diagnostic.Range.Start.Line != 1 || diagnostic.Range.Start.Column != testCase.start || diagnostic.Range.Start.Offset != testCase.start-1 || diagnostic.Range.End.Line != 1 || diagnostic.Range.End.Column != testCase.end || diagnostic.Range.End.Offset != testCase.end-1 {
			t.Fatalf("%s diagnostic = %#v", testCase.id, diagnostic)
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
	if policy.SalesforceEligible == nil || *policy.SalesforceEligible || policy.SalesforceExclusionClass != "policy-local-only" || !strings.Contains(policy.SalesforceExclusionReason, "zero Salesforce parity") {
		t.Fatalf("fixture policy = %#v", policy)
	}

	evidence, err := BuildEvidenceSnapshot([]string{fixturePath})
	if err != nil {
		t.Fatal(err)
	}
	assertExactSurfaceSet(t, evidence, keysInOrder(want, authHiddenNegativeCases))
	for _, row := range evidence {
		if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorUnsupported || len(row.Sources) != 1 || row.Sources[0] != "fixture:"+strings.TrimSuffix(authHiddenNegativeDepthFixture, ".json") {
			t.Fatalf("%s snapshot = %#v", row.SurfaceID, row)
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
	for _, testCase := range authHiddenNegativeCases {
		if ownership[testCase.id] != 1 {
			t.Fatalf("fixture ownership for %s = %d, want exactly one non-evidenceOnly owner", testCase.id, ownership[testCase.id])
		}
	}

	if result, err := compat.Run(fixture); err != nil || !result.OK {
		t.Fatalf("fixture check = %#v, error = %v", result, err)
	}
}

func keysInOrder(set map[string]bool, cases []authHiddenNegativeCase) []string {
	ids := make([]string, 0, len(set))
	for _, testCase := range cases {
		if set[testCase.id] {
			ids = append(ids, testCase.id)
		}
	}
	return ids
}
