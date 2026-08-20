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

func TestAuthConfigurationRightFrameURLHasExactNegativeEvidence(t *testing.T) {
	const fixtureName = "core-runtime-auth-configuration-negative.json"
	const id = "apex:Auth.AuthConfiguration.getRightFrameUrl()"
	root := filepath.Join("..", "..")
	path := filepath.Join(root, "docs", "fixtures", fixtureName)

	fixture, err := compat.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Command.Kind != "check" || len(fixture.Source) != 1 || fixture.Expected.Error != nil || !strings.Contains(fixture.Source[0].Content, "cfg.getRightFrameUrl()") {
		t.Fatalf("negative execution contract = %#v", fixture)
	}
	var expected struct {
		Diagnostics int  `json:"diagnostics"`
		OK          bool `json:"ok"`
	}
	if err := json.Unmarshal(fixture.Expected.Result, &expected); err != nil || expected.Diagnostics != 1 || expected.OK {
		t.Fatalf("negative expected result = %#v, error = %v", expected, err)
	}
	temp := t.TempDir()
	sourcePath := filepath.Join(temp, fixture.Source[0].Path)
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourcePath, []byte(fixture.Source[0].Content), 0o644); err != nil {
		t.Fatal(err)
	}
	result := sema.Analyze(typesys.Build(project.Project{Root: temp, ApexFiles: []string{sourcePath}}, schema.Schema{}))
	if len(result.Diagnostics) != 1 {
		t.Fatalf("semantic diagnostics = %#v, want exactly one", result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "GLADESEMA028" || diagnostic.Message != `method "probe" uses unsupported local feature "Auth.AuthConfiguration.getRightFrameUrl"` || filepath.Base(diagnostic.File) != filepath.Base(fixture.Source[0].Path) || diagnostic.Range == nil || diagnostic.Range.Start.Line != 4 || diagnostic.Range.Start.Column != 5 {
		t.Fatalf("negative diagnostic = %#v", diagnostic)
	}
	if len(fixture.Evidence) != 1 || fixture.Evidence[0].SurfaceID != id || fixture.Evidence[0].Kind != "unsupported" {
		t.Fatalf("negative evidence = %#v", fixture.Evidence)
	}

	rows, err := BuildEvidenceSnapshot([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].SurfaceID != id || rows[0].Evidence != EvidenceFixture || rows[0].GladeBehavior != BehaviorUnsupported || rows[0].Sources[0] != "fixture:"+strings.TrimSuffix(fixtureName, ".json") {
		t.Fatalf("negative snapshot = %#v", rows)
	}
	owners := 0
	paths, err := filepath.Glob(filepath.Join(root, "docs", "fixtures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range paths {
		data, err := os.ReadFile(candidate)
		if err != nil {
			t.Fatal(err)
		}
		var header struct {
			EvidenceOnly bool                     `json:"evidenceOnly"`
			Evidence     []compat.FixtureEvidence `json:"evidence"`
		}
		if err := json.Unmarshal(data, &header); err != nil {
			t.Fatal(err)
		}
		for _, item := range header.Evidence {
			if !header.EvidenceOnly && item.SurfaceID == id {
				owners++
			}
		}
	}
	if owners != 1 {
		t.Fatalf("negative fixture owners = %d, want exactly one", owners)
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
	if policy.SalesforceEligible == nil || *policy.SalesforceEligible || policy.SalesforceExclusionClass != "policy-local-only" || !strings.Contains(policy.SalesforceExclusionReason, "zero Salesforce parity") {
		t.Fatalf("negative policy = %#v", policy)
	}

	if result, err := compat.Run(fixture); err != nil || !result.OK {
		t.Fatalf("negative fixture = %#v, error = %v", result, err)
	}
}
