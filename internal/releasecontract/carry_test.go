package releasecontract

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func rewriteCarriedFixture(t *testing.T, root string, mutate func(*ReleaseSourceReceipt)) {
	t.Helper()
	path := filepath.Join(root, "source-68.json")
	var current, previous ReleaseSourceReceipt
	if err := readStrict(path, &current); err != nil {
		t.Fatal(err)
	}
	if err := readStrict(filepath.Join(root, "source-67.json"), &previous); err != nil {
		t.Fatal(err)
	}
	hash, err := sourceFileSHA256(filepath.Join(root, "source-67.json"))
	if err != nil {
		t.Fatal(err)
	}
	for i := range current.Snapshot.Families {
		family := &current.Snapshot.Families[i]
		if family.Name == "apex" || family.Name == "tooling-api" {
			continue
		}
		for _, prior := range previous.Snapshot.Families {
			if prior.Name == family.Name {
				*family = prior
			}
		}
		family.CarriedFrom, family.CarriedReceiptSHA256, family.Unpublished = "262.0", hash, true
	}
	current.Snapshot.LWC = previous.Snapshot.LWC
	current.Snapshot.LWC.CarriedFrom, current.Snapshot.LWC.CarriedReceiptSHA256, current.Snapshot.LWC.Unpublished = "262.0", hash, true
	if mutate != nil {
		mutate(&current)
	}
	raw, err := json.Marshal(current)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	contractPath := filepath.Join(root, "contract.json")
	var contract Contract
	if err := readStrict(contractPath, &contract); err != nil {
		t.Fatal(err)
	}
	contract.Releases[len(contract.Releases)-1].SourceReceiptSHA256 = fmt.Sprintf("%x", sha256.Sum256(raw))
	raw, err = json.Marshal(contract)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(contractPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestAnalyzeExplicitPreviousGACarry(t *testing.T) {
	root := t.TempDir()
	writeAnalysisFixtureWith(t, releaseAnalysisFixture{root: root, reappear: true}, true)
	rewriteCarriedFixture(t, root, nil)
	if _, err := Analyze(filepath.Join(root, "contract.json")); err != nil {
		t.Fatal(err)
	}
}

func TestAnalyzeRejectsInvalidCarries(t *testing.T) {
	cases := []struct {
		name, want string
		mutate     func(*ReleaseSourceReceipt)
	}{
		{"implicit", "implicit carry", func(r *ReleaseSourceReceipt) {
			f := &r.Snapshot.Families[1]
			f.CarriedFrom = ""
			f.CarriedReceiptSHA256 = ""
			f.Unpublished = false
		}},
		{"missing hash", "SHA-256 mismatch", func(r *ReleaseSourceReceipt) { r.Snapshot.Families[1].CarriedReceiptSHA256 = "" }},
		{"wrong hash", "SHA-256 mismatch", func(r *ReleaseSourceReceipt) { r.Snapshot.Families[1].CarriedReceiptSHA256 = strings.Repeat("9", 64) }},
		{"older release", "previous GA", func(r *ReleaseSourceReceipt) { r.Snapshot.Families[1].CarriedFrom = "260.0" }},
		{"published", "unpublished", func(r *ReleaseSourceReceipt) { r.Snapshot.Families[1].Unpublished = false }},
		{"changed payload", "differs", func(r *ReleaseSourceReceipt) { r.Snapshot.Families[1].FileCount++ }},
		{"lwc implicit", "implicit carry", func(r *ReleaseSourceReceipt) {
			r.Snapshot.LWC.CarriedFrom = ""
			r.Snapshot.LWC.CarriedReceiptSHA256 = ""
			r.Snapshot.LWC.Unpublished = false
		}},
		{"lwc implicit modified hash", "implicit carry", func(r *ReleaseSourceReceipt) {
			r.Snapshot.LWC.CarriedFrom = ""
			r.Snapshot.LWC.CarriedReceiptSHA256 = ""
			r.Snapshot.LWC.Unpublished = false
			r.Snapshot.LWC.SourceVersionSHA256 = strings.Repeat("9", 64)
		}},
		{"lwc missing hash", "SHA-256 mismatch", func(r *ReleaseSourceReceipt) { r.Snapshot.LWC.CarriedReceiptSHA256 = "" }},
		{"lwc changed payload", "differs", func(r *ReleaseSourceReceipt) { r.Snapshot.LWC.CopiedMarkdownFiles++ }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			writeAnalysisFixtureWith(t, releaseAnalysisFixture{root: root, reappear: true}, true)
			rewriteCarriedFixture(t, root, c.mutate)
			if _, err := Analyze(filepath.Join(root, "contract.json")); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want %q, got %v", c.want, err)
			}
		})
	}
}

func TestAnalyzeBindsCompositionReceipt(t *testing.T) {
	for _, test := range []struct {
		name            string
		tamper, diverge bool
	}{
		{"bound", false, false}, {"tampered file", true, false}, {"unrelated bound composition", false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			writeAnalysisFixtureWith(t, releaseAnalysisFixture{root: root, reappear: true}, true)
			compositionPath := filepath.Join(root, "composition.json")
			var previous ReleaseSourceReceipt
			if err := readStrict(filepath.Join(root, "source-67.json"), &previous); err != nil {
				t.Fatal(err)
			}
			previousHash, err := sourceFileSHA256(filepath.Join(root, "source-67.json"))
			if err != nil {
				t.Fatal(err)
			}
			rewriteCarriedFixture(t, root, func(r *ReleaseSourceReceipt) {
				r.Generator.Path = "scripts/salesforce-release/compose_winter27.py"
				r.Snapshot.Assembler.Path = "scripts/salesforce-release/compose_winter27.py"
				r.Snapshot.AdditionalFamilies = []SourceFamilyReceipt{{Name: "metadata-api", Version: "264.0", FileCount: 5, SHA256: strings.Repeat("a", 64)}}
				c := sourceComposition{SchemaVersion: 1, TargetAPIVersion: r.APIVersion, AtlasVersion: r.Snapshot.AtlasVersion, CarriedInventorySHA256: previous.InventorySHA256, PreviousSourceReceiptSHA256: previousHash, VersionedSourceSHA256: r.Snapshot.VersionedSourceSHA256, Families: r.Snapshot.Families, AdditionalFamilies: r.Snapshot.AdditionalFamilies, LWC: r.Snapshot.LWC}
				raw, err := json.Marshal(c)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(compositionPath, raw, 0600); err != nil {
					t.Fatal(err)
				}
				hash := fmt.Sprintf("%x", sha256.Sum256(raw))
				r.Snapshot.MetadataSHA256 = hash
				r.Snapshot.CompositionReceipt = &SourceToolReceipt{Path: "composition.json", SHA256: hash}
				if test.diverge {
					r.Snapshot.Families[0].SHA256 = strings.Repeat("9", 64)
				}
			})
			if test.tamper {
				if err := os.WriteFile(compositionPath, []byte(`{"changed":true}`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			_, err = Analyze(filepath.Join(root, "contract.json"))
			if !test.tamper && !test.diverge && err != nil {
				t.Fatal(err)
			}
			if test.tamper && (err == nil || !strings.Contains(err.Error(), "composition receipt SHA-256 mismatch")) {
				t.Fatalf("tampered composition: %v", err)
			}
			if test.diverge && (err == nil || !strings.Contains(err.Error(), "composition source identity or payload mismatch")) {
				t.Fatalf("unrelated composition: %v", err)
			}
		})
	}
}

func TestLWCFutureReleaseRejectsImplicitCarry(t *testing.T) {
	previous := ReleaseSourceReceipt{Snapshot: SourceSnapshotReceipt{AtlasVersion: "264.0"}}
	current := LWCSourceReceipt{SourceVersion: "latest", SourceVersionSHA256: strings.Repeat("9", 64)}
	if err := validateLWCCarry(current, "266.0", &previous, strings.Repeat("a", 64)); err == nil || !strings.Contains(err.Error(), "implicit carry") {
		t.Fatalf("future implicit carry: %v", err)
	}
	current.PublishedAtlasVersion = "266.0"
	if err := validateLWCCarry(current, "266.0", &previous, strings.Repeat("a", 64)); err != nil {
		t.Fatalf("fresh current publication: %v", err)
	}
}
