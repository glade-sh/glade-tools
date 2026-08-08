package toolcli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var workspaceRoot = "../.."

func fixturesPath(rel string) string {
	return filepath.Join(workspaceRoot, rel)
}

// ---- Fixture test ----

func TestSalesforceReleaseFixtures(t *testing.T) {
	prevManifestPath := fixturesPath("docs/fixtures/salesforce-release-previous.json")
	currManifestPath := fixturesPath("docs/fixtures/salesforce-release-current.json")
	prevInvPath := fixturesPath("docs/fixtures/salesforce-docs-inventory-spring-26.json")
	currInvPath := fixturesPath("docs/fixtures/salesforce-docs-inventory-summer-26.json")
	classPath := fixturesPath("docs/fixtures/salesforce-release-classifications-spring-to-summer-26.json")

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

	expectedSpring := "5027360634685096ec3144da477673445270c58f510b604346b11fdcfaf7d22e"
	expectedSummer := "a0b3d7cb6bde10acdd719ee93cb54d5daed6facb0b34a34377d0d42697f81d7c"

	if prevManifest.Digest != expectedSpring {
		t.Errorf("Spring digest: got %s, want %s", prevManifest.Digest, expectedSpring)
	}
	if currManifest.Digest != expectedSummer {
		t.Errorf("Summer digest: got %s, want %s", currManifest.Digest, expectedSummer)
	}

	delta, err := computeReleaseDelta(salesforceVerifyOptions{
		PreviousReleaseManifest: prevManifestPath,
		PreviousInventory:       prevInvPath,
		CurrentInventory:        currInvPath,
		ReleaseClassifications:  classPath,
	}, currManifest)
	if err != nil {
		t.Fatalf("compute release delta: %v", err)
	}
	if delta.Status != "pass" {
		t.Fatalf("release delta status = %s: %s", delta.Status, delta.Error)
	}

	if len(delta.Added) != 171 {
		t.Errorf("added: %d, want 171", len(delta.Added))
	}
	if len(delta.Removed) != 0 {
		t.Errorf("removed: %d, want 0", len(delta.Removed))
	}
	if len(delta.Changed) != 0 {
		t.Errorf("changed: %d, want 0", len(delta.Changed))
	}
	if len(delta.Unchanged) != 8095 {
		t.Errorf("unchanged: %d, want 8095", len(delta.Unchanged))
	}
}
