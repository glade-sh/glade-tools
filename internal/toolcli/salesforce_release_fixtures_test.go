package toolcli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/releasecontract"
)

var workspaceRoot = "../.."

func fixturesPath(rel string) string {
	return filepath.Join(workspaceRoot, rel)
}

// ---- Fixture test ----

func TestSalesforceReleaseFixtures(t *testing.T) {
	prevManifestPath := fixturesPath("docs/fixtures/salesforce-release-previous.json")
	currManifestPath := fixturesPath("docs/fixtures/salesforce-release-current.json")

	// Stale next manifest must not exist.
	nextPath := fixturesPath("docs/fixtures/salesforce-release-next.json")
	if _, err := os.Stat(nextPath); err == nil {
		t.Fatal("salesforce-release-next.json must be deleted; it is a stale placeholder")
	} else if !os.IsNotExist(err) {
		t.Fatalf("unexpected error checking salesforce-release-next.json: %v", err)
	}

	prevManifest, err := loadReleaseManifest(prevManifestPath)
	if err != nil {
		t.Fatalf("load previous manifest: %v", err)
	}
	currManifest, err := loadReleaseManifest(currManifestPath)
	if err != nil {
		t.Fatalf("load current manifest: %v", err)
	}

	// Provenance: Spring '26 / 66.0 -> Summer '26 / 67.0
	if prevManifest.Release != "Spring '26" {
		t.Errorf("previous release: got %q, want Spring '26", prevManifest.Release)
	}
	if prevManifest.APIVersion != "66.0" {
		t.Errorf("previous apiVersion: got %q, want 66.0", prevManifest.APIVersion)
	}
	if currManifest.Release != "Summer '26" {
		t.Errorf("current release: got %q, want Summer '26", currManifest.Release)
	}
	if currManifest.APIVersion != "67.0" {
		t.Errorf("current apiVersion: got %q, want 67.0", currManifest.APIVersion)
	}

	expectedFamilies := []string{
		"apex-reference", "aura-reference", "lwc-reference",
		"rest-api-reference", "tooling-api-reference", "visualforce-reference",
	}
	if !setsEqual(prevManifest.SourceFamilies, expectedFamilies) {
		t.Errorf("prev source families: got %v, want %v", prevManifest.SourceFamilies, expectedFamilies)
	}
	if !setsEqual(currManifest.SourceFamilies, expectedFamilies) {
		t.Errorf("curr source families: got %v, want %v", currManifest.SourceFamilies, expectedFamilies)
	}

	if !strings.Contains(prevManifest.Acquisition, "Spring 260.0") {
		t.Errorf("prev acquisition should mention Spring 260.0: %q", prevManifest.Acquisition)
	}
	if !strings.Contains(currManifest.Acquisition, "Summer 262.0") {
		t.Errorf("curr acquisition should mention Summer 262.0: %q", currManifest.Acquisition)
	}

	expectedSpring := "fb9752440ce2a1189edc69c12b1c2ce57050b435655c0df6a77d53579d0a7813"
	expectedSummer := "ff9c3e06f85100754143a4bcfd82a3c40a24dd81c6248e5da5f7ee691cf9cf99"

	if prevManifest.Digest != expectedSpring {
		t.Errorf("Spring digest: got %s, want %s", prevManifest.Digest, expectedSpring)
	}
	if currManifest.Digest != expectedSummer {
		t.Errorf("Summer digest: got %s, want %s", currManifest.Digest, expectedSummer)
	}

	analysis, err := releasecontract.Analyze(fixturesPath("docs/fixtures/salesforce-release-contract.json"))
	if err != nil {
		t.Fatalf("analyze checked release contract: %v", err)
	}
	if analysis.Report.Status != "fail" {
		t.Fatalf("static release status = %s, want fail before evidence execution", analysis.Report.Status)
	}
	if got := analysis.Report.SurfaceDelta; got.Total != 519 || got.Classified != 519 || got.Implemented != 0 || got.Proved != 0 || got.ExplicitNonParity != 3 {
		t.Errorf("surface delta = %#v", got)
	}
	if got := analysis.Report.BehaviorDelta; got.Total != 22 || got.Classified != 22 || got.Implemented != 0 || got.Proved != 0 || got.ExplicitNonParity != 6 {
		t.Errorf("behavior delta = %#v", got)
	}
	if got := analysis.Report.ChangeInventory; got.Total != 3990 || got.Routed != 3990 || got.OutOfScope != 3962 {
		t.Errorf("change inventory = %#v", got)
	}
	if len(analysis.Report.Unclassified) != 0 {
		t.Errorf("unclassified = %v", analysis.Report.Unclassified)
	}
}

func TestSalesforceReleaseCaseBindingsPreflight(t *testing.T) {
	analysis, err := releasecontract.Analyze(fixturesPath("docs/fixtures/salesforce-release-contract.json"))
	if err != nil {
		t.Fatal(err)
	}
	gladeRoot, err := filepath.Abs(filepath.Join(workspaceRoot, "..", "glade"))
	if err != nil {
		t.Fatal(err)
	}
	module, err := goModulePath(gladeRoot)
	if err != nil {
		t.Fatal(err)
	}
	evidence := &productTestEvidence{gladeRoot: gladeRoot, module: module, bindings: map[string]productTestBinding{}}
	if err := preflightReleaseBindings(analysis, fixturesPath("docs/fixtures/apex-language-rules.json"), evidence); err != nil {
		t.Fatal(err)
	}
}
