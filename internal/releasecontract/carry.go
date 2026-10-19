package releasecontract

import (
	"fmt"
	"path/filepath"
	"reflect"
)

// Carry receipts are explicit previous-GA documentation provenance, not a
// declaration that the previous release's behavior matches this release.
func validateCarryIdentity(name, from, hash string, unpublished bool, target string, previous *ReleaseSourceReceipt, previousHash string) error {
	if from == "" {
		if hash != "" || unpublished {
			return fmt.Errorf("%s carry requires explicit carriedFrom", name)
		}
		return nil
	}
	if previous == nil || from != previous.Snapshot.AtlasVersion || from != fmt.Sprintf("%d.0", apiInt(target)-2) {
		return fmt.Errorf("%s carriedFrom must be the previous GA Atlas release", name)
	}
	if !unpublished {
		return fmt.Errorf("%s carry requires explicit unpublished documentation status", name)
	}
	if !sha256Pattern.MatchString(hash) || hash != previousHash {
		return fmt.Errorf("%s carry previous GA receipt SHA-256 mismatch", name)
	}
	return nil
}

func validateFamilyCarry(family SourceFamilyReceipt, target string, previous *ReleaseSourceReceipt, previousHash string) error {
	if err := validateCarryIdentity(family.Name, family.CarriedFrom, family.CarriedReceiptSHA256, family.Unpublished, target, previous, previousHash); err != nil {
		return err
	}
	if family.CarriedFrom == "" {
		if family.Version != target {
			return fmt.Errorf("invalid Atlas family %q: implicit carry is forbidden", family.Name)
		}
		return nil
	}
	for _, prior := range previous.Snapshot.Families {
		if prior.Name == family.Name {
			if prior.CarriedFrom != "" || family.Version != prior.Version || family.FileCount != prior.FileCount || family.SHA256 != prior.SHA256 {
				return fmt.Errorf("%s carry differs from the previous GA family receipt", family.Name)
			}
			return nil
		}
	}
	return fmt.Errorf("%s carry is absent from the previous GA receipt", family.Name)
}

func validateLWCCarry(lwc LWCSourceReceipt, target string, previous *ReleaseSourceReceipt, previousHash string) error {
	if err := validateCarryIdentity("lwc", lwc.CarriedFrom, lwc.CarriedReceiptSHA256, lwc.Unpublished, target, previous, previousHash); err != nil {
		return err
	}
	if lwc.CarriedFrom == "" {
		if apiInt(target) >= 264 && lwc.PublishedAtlasVersion != target {
			return fmt.Errorf("lwc implicit carry is forbidden; publication identity or explicit previous GA provenance is required")
		}
		if lwc.PublishedAtlasVersion != "" && lwc.PublishedAtlasVersion != target {
			return fmt.Errorf("lwc publication identity mismatch")
		}
		return nil
	}
	if lwc.PublishedAtlasVersion != "" && lwc.PublishedAtlasVersion != lwc.CarriedFrom {
		return fmt.Errorf("lwc carry cannot also declare current documentation published")
	}
	carried := lwc
	carried.CarriedFrom, carried.CarriedReceiptSHA256, carried.Unpublished = "", "", false
	if previous.Snapshot.LWC.CarriedFrom != "" || !reflect.DeepEqual(carried, previous.Snapshot.LWC) {
		return fmt.Errorf("lwc carry differs from the previous GA family receipt")
	}
	return nil
}

// A composed source receipt must describe exactly the checked composition,
// rather than merely binding an unrelated file with a valid digest.
type sourceComposition struct {
	SchemaVersion               int                   `json:"schemaVersion"`
	TargetAPIVersion            string                `json:"targetAPIVersion"`
	AtlasVersion                string                `json:"atlasVersion"`
	CarriedInventorySHA256      string                `json:"carriedInventorySHA256"`
	PreviousSourceReceiptSHA256 string                `json:"previousSourceReceiptSHA256"`
	VersionedSourceSHA256       string                `json:"versionedSourceSHA256"`
	Families                    []SourceFamilyReceipt `json:"families"`
	AdditionalFamilies          []SourceFamilyReceipt `json:"additionalFamilies"`
	LWC                         LWCSourceReceipt      `json:"lwc"`
}

func validateSourceComposition(root string, receipt ReleaseSourceReceipt, previous *ReleaseSourceReceipt, previousHash string) error {
	var composition sourceComposition
	if err := readStrict(filepath.Join(root, receipt.Snapshot.CompositionReceipt.Path), &composition); err != nil {
		return fmt.Errorf("composition receipt: %w", err)
	}
	if previous == nil || composition.SchemaVersion != 1 || composition.TargetAPIVersion != receipt.APIVersion || composition.AtlasVersion != receipt.Snapshot.AtlasVersion || composition.VersionedSourceSHA256 != receipt.Snapshot.VersionedSourceSHA256 || composition.PreviousSourceReceiptSHA256 != previousHash || composition.CarriedInventorySHA256 != previous.InventorySHA256 || !reflect.DeepEqual(composition.Families, receipt.Snapshot.Families) || !reflect.DeepEqual(composition.AdditionalFamilies, receipt.Snapshot.AdditionalFamilies) || !reflect.DeepEqual(composition.LWC, receipt.Snapshot.LWC) {
		return fmt.Errorf("composition source identity or payload mismatch")
	}
	return nil
}
