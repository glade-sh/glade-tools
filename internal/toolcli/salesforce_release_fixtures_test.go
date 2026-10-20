package toolcli

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/apexdocs"
	"github.com/glade-sh/glade/tools/internal/releasecontract"
	"github.com/glade-sh/glade/tools/internal/surfaceledger"
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
	if got := analysis.Report.SurfaceDelta; got.Total != 1709 || got.Classified != 1709 || got.Implemented != 0 || got.Proved != 0 || got.ExplicitNonParity != 861 {
		t.Errorf("surface delta = %#v", got)
	}
	if got := analysis.Report.BehaviorDelta; got.Total != 28 || got.Classified != 28 || got.Implemented != 0 || got.Proved != 0 || got.ExplicitNonParity != 5 {
		t.Errorf("behavior delta = %#v", got)
	}
	if got := analysis.Report.ChangeInventory; got.Total != 10677 || got.Routed != 10677 || got.OutOfScope != 10634 {
		t.Errorf("change inventory = %#v", got)
	}
	if len(analysis.Report.Unclassified) != 0 {
		t.Errorf("unclassified = %v", analysis.Report.Unclassified)
	}
}

func TestAPI68NamespacePolicyCoversEveryKnownConnectApiDeclaration(t *testing.T) {
	path := fixturesPath("docs/fixtures/salesforce-docs-inventory-winter-27.json")
	inv, err := apexdocs.ReadInventory(path)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(fixturesPath("docs/fixtures/salesforce-release-namespace-policy-winter-27.json"))
	if err != nil {
		t.Fatal(err)
	}
	var policy struct {
		Namespace       string                                `json:"namespace"`
		InventorySHA256 string                                `json:"inventorySHA256"`
		Classifications []surfaceledger.ReleaseClassification `json:"classifications"`
	}
	if err := json.Unmarshal(data, &policy); err != nil {
		t.Fatal(err)
	}
	if policy.Namespace != "ConnectApi" || policy.InventorySHA256 != fmt.Sprintf("%x", sha256.Sum256(raw)) {
		t.Fatal("namespace policy does not bind the checked inventory")
	}
	expected := map[string]bool{}
	for _, row := range surfaceledger.ReleaseRowsFromDocsInventory(inv) {
		if row.Namespace == "ConnectApi" {
			expected[surfaceledger.CanonicalSurfaceIDKey(row.SurfaceID)] = true
		}
	}
	for _, c := range policy.Classifications {
		key := surfaceledger.CanonicalSurfaceIDKey(c.SurfaceID)
		if !expected[key] || c.Scope != surfaceledger.ScopeOutsideClaim || c.Disposition != surfaceledger.DispoExplicitUnsupported || c.ReasonRef == "" {
			t.Fatalf("invalid or duplicate namespace policy: %+v", c)
		}
		delete(expected, key)
	}
	if len(expected) != 0 {
		t.Fatalf("namespace policy omitted %d known declarations", len(expected))
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
