package releasecontract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCurrentOnlyLWCPublicationRequiresExactBoundSeasonalNote(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "notes.json")
	raw := []byte(`{"schemaVersion":1,"totalFiles":1,"totalMembers":0,"namespaces":[],"documents":[{"sourcePath":"rn_lwc_versioning.md","kind":"release-note","name":"Get","title":"Get the Latest LWC Changes with LWC API Version 68.0"}]}`)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	hash, _ := sourceFileSHA256(path)
	good := LWCSourceReceipt{CurrentOnlyPublication: &LWCPublicationReceipt{APIVersion: "68.0", Inventory: SourceToolReceipt{Path: "notes.json", SHA256: hash}, SourcePath: "rn_lwc_versioning.md", SourceURL: "https://help.salesforce.com/s/articleView?id=release-notes.rn_lwc_versioning.htm&language=en_US&release=264&type=5"}}
	release := Release{APIVersion: "68.0", ChangeInventory: "notes.json"}
	for _, test := range []struct {
		name   string
		change func(*LWCSourceReceipt)
	}{
		{"wrong API", func(l *LWCSourceReceipt) { l.CurrentOnlyPublication.APIVersion = "67.0" }},
		{"wrong hash", func(l *LWCSourceReceipt) { l.CurrentOnlyPublication.Inventory.SHA256 = "bad" }},
		{"carry", func(l *LWCSourceReceipt) { l.CarriedFrom = "262.0" }},
		{"invented Atlas publication", func(l *LWCSourceReceipt) { l.PublishedAtlasVersion = "264.0" }},
		{"unrelated note", func(l *LWCSourceReceipt) { l.CurrentOnlyPublication.SourcePath = "other.md" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			b, _ := json.Marshal(good)
			var changed LWCSourceReceipt
			json.Unmarshal(b, &changed)
			test.change(&changed)
			if validateCurrentLWCPublication(root, release, changed) == nil {
				t.Fatal("invalid publication accepted")
			}
		})
	}
	if err := validateCurrentLWCPublication(root, release, good); err != nil {
		t.Fatal(err)
	}
	if err := validateLWCCarry(good, "264.0", nil, ""); err != nil {
		t.Fatal(err)
	}
}
