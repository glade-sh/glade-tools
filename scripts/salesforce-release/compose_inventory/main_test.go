package main

import (
	"github.com/glade-sh/glade/tools/internal/apexdocs"
	"reflect"
	"testing"
)

func TestComposeInventoryPreservesExactCarriedUnionAndFiltersFuture(t *testing.T) {
	prior := apexdocs.Inventory{Documents: []apexdocs.Document{
		{SourcePath: "apex/old.md", Name: "Old"},
		{SourcePath: "tooling-api/old.md", Name: "Old"},
		{SourcePath: "lightning-aura/keep.md", Name: "Aura"},
		{SourcePath: "lwc/keep.md", Name: "LWC"},
		{SourcePath: "rest-api/keep.md", Name: "REST"},
		{SourcePath: "visualforce/keep.md", Name: "VF"},
	}}
	current := apexdocs.Inventory{Documents: []apexdocs.Document{
		{SourcePath: "apex/new.md", Name: "New", Members: []apexdocs.Member{{Name: "valid", APIVersion: "68.0"}, {Name: "bad", APIVersion: "369.0"}}},
		{SourcePath: "object-reference/future.md", Name: "Future", APIVersion: "69.0"},
		{SourcePath: "tooling-api/new.md", Name: "NewTooling"},
	}}
	got, excluded := composeInventory(current, prior)
	want := []string{"apex/new.md", "lightning-aura/keep.md", "lwc/keep.md", "rest-api/keep.md", "tooling-api/new.md", "visualforce/keep.md"}
	var paths []string
	for _, d := range got.Documents {
		paths = append(paths, d.SourcePath)
	}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("coverage %v; want %v", paths, want)
	}
	if got.TotalFiles != 6 || got.TotalMembers != 1 {
		t.Fatalf("count %d/%d", got.TotalFiles, got.TotalMembers)
	}
	if len(got.Documents[0].Members) != 1 || got.Documents[0].Members[0].Name != "valid" {
		t.Fatalf("future member retained: %v", got.Documents[0])
	}
	if !reflect.DeepEqual(excluded, []string{"apex/new.md:bad: API369.0", "object-reference/future.md: API69.0"}) {
		t.Fatalf("future exclusion record %v", excluded)
	}
	for _, d := range prior.Documents[2:] {
		found := false
		for _, newDoc := range got.Documents {
			if newDoc.SourcePath == d.SourcePath {
				found = true
				if !reflect.DeepEqual(d, newDoc) {
					t.Fatalf("carry changed %s", d.SourcePath)
				}
			}
		}
		if !found {
			t.Fatalf("carry dropped %s", d.SourcePath)
		}
	}
}

func TestComposeInventoryIsDeterministic(t *testing.T) {
	a := apexdocs.Inventory{Documents: []apexdocs.Document{{SourcePath: "apex/z.md", Name: "Z"}, {SourcePath: "apex/a.md", Name: "A"}}}
	b := apexdocs.Inventory{Documents: []apexdocs.Document{{SourcePath: "apex/a.md", Name: "A"}, {SourcePath: "apex/z.md", Name: "Z"}}}
	first, _ := composeInventory(a, apexdocs.Inventory{})
	second, _ := composeInventory(b, apexdocs.Inventory{})
	if !reflect.DeepEqual(first, second) {
		t.Fatal("inventory depends on input order")
	}
	if apexdocs.CanonicalDigest(first) != apexdocs.CanonicalDigest(second) {
		t.Fatal("digest depends on input order")
	}
}

func TestComposeInventoryKeepsDeclaredReferenceFamilyDenominator(t *testing.T) {
	current := apexdocs.Inventory{Documents: []apexdocs.Document{
		{SourcePath: "apex/a.md", Name: "A"},
		{SourcePath: "apex-guide/narrative.md", Name: "Narrative"},
		{SourcePath: "metadata-api/deploy.md", Name: "Deploy"},
		{SourcePath: "object-reference/account.md", Name: "Account"},
	}}
	got, _ := composeInventory(current, apexdocs.Inventory{})
	if len(got.Documents) != 1 || got.Documents[0].SourcePath != "apex/a.md" {
		t.Fatalf("supplementary sources changed the six-family contract denominator: %#v", got.Documents)
	}
}

func TestHistoricalManifestUsesCanonicalInventoryAndExactSeason(t *testing.T) {
	inv := apexdocs.Inventory{SchemaVersion: 1, TotalFiles: 1, Documents: []apexdocs.Document{{SourcePath: "apex/example.md", Name: "Example"}}}
	manifest, slug, err := historicalManifest(inv, "62.0")
	if err != nil || slug != "winter-25" || manifest.Release != "Winter '25" || manifest.APIVersion != "62.0" || manifest.Digest != apexdocs.CanonicalDigest(inv) || len(manifest.SourceFamilies) != 6 {
		t.Fatalf("manifest=%+v slug=%s error=%v", manifest, slug, err)
	}
	if _, _, err := historicalManifest(inv, "68.0"); err == nil {
		t.Fatal("historical mode must not bypass the API 68 carry composition")
	}
}

func TestFreshLWCDoesNotRetainStaleCarriedDocuments(t *testing.T) {
	prior := apexdocs.Inventory{Documents: []apexdocs.Document{{SourcePath: "lwc/old.md"}, {SourcePath: "rest-api/keep.md"}}}
	fresh := apexdocs.Inventory{Documents: []apexdocs.Document{{SourcePath: "lwc/new.md"}}}
	inv, previous := replaceLWCCarry(apexdocs.Inventory{}, prior, fresh)
	got, _ := composeInventory(inv, previous)
	if len(got.Documents) != 2 || got.Documents[0].SourcePath != "lwc/new.md" || got.Documents[1].SourcePath != "rest-api/keep.md" {
		t.Fatalf("fresh source=%+v", got.Documents)
	}
}
