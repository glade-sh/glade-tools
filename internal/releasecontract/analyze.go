package releasecontract

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/glade-sh/glade/tools/internal/apexdocs"
	"github.com/glade-sh/glade/tools/internal/surfaceledger"
)

type Denominator struct {
	Total             int `json:"total"`
	Classified        int `json:"classified"`
	Implemented       int `json:"implemented"`
	Proved            int `json:"proved"`
	ExplicitNonParity int `json:"explicitNonParity"`
}

type Range struct {
	Since int `json:"since,omitempty"`
	Until int `json:"until,omitempty"`
}

type AxisReport struct {
	Advertised []string `json:"advertised"`
	Passing    []string `json:"passing"`
}
type ChangeInventoryDenominator struct {
	Total      int `json:"total"`
	Routed     int `json:"routed"`
	OutOfScope int `json:"outOfScope"`
}

type Report struct {
	SchemaVersion    int                        `json:"schemaVersion"`
	Status           string                     `json:"status"`
	PreviousRelease  string                     `json:"previousRelease"`
	CurrentRelease   string                     `json:"currentRelease"`
	SurfaceDelta     Denominator                `json:"surfaceDelta"`
	BehaviorDelta    Denominator                `json:"behaviorDelta"`
	ChangeInventory  ChangeInventoryDenominator `json:"changeInventory"`
	SourceVersions   AxisReport                 `json:"sourceVersions"`
	EndpointVersions AxisReport                 `json:"endpointVersions"`
	OrgProfiles      AxisReport                 `json:"orgProfiles"`
	SilentFallbacks  int                        `json:"silentFallbacks"`
	Unclassified     []string                   `json:"unclassified,omitempty"`
	Ranges           map[string]Range           `json:"ranges"`
}

type SurfaceChange struct {
	PreviousRelease string                               `json:"previousRelease"`
	CurrentRelease  string                               `json:"currentRelease"`
	Entry           surfaceledger.DeltaEntry             `json:"entry"`
	Classification  *surfaceledger.ReleaseClassification `json:"classification,omitempty"`
}

type Analysis struct {
	Contract       Contract           `json:"-"`
	Report         Report             `json:"report"`
	SurfaceChanges []SurfaceChange    `json:"surfaceChanges"`
	ChangeRoutes   []ReleaseNoteRoute `json:"changeRoutes"`
}

type ClassificationFile struct {
	SchemaVersion   int                                   `json:"schemaVersion"`
	PreviousRelease string                                `json:"previousRelease"`
	CurrentRelease  string                                `json:"currentRelease"`
	Classifications []surfaceledger.ReleaseClassification `json:"classifications"`
}

type ReleaseNoteRoutesFile struct {
	SchemaVersion   int                `json:"schemaVersion"`
	PreviousRelease string             `json:"previousRelease"`
	CurrentRelease  string             `json:"currentRelease"`
	InventoryDigest string             `json:"inventoryDigest"`
	Routes          []ReleaseNoteRoute `json:"routes"`
}

type ReleaseNoteRoute struct {
	SourcePath       string   `json:"sourcePath"`
	BehaviorIDs      []string `json:"behaviorIds,omitempty"`
	SurfaceIDs       []string `json:"surfaceIds,omitempty"`
	OutOfScopeReason string   `json:"outOfScopeReason,omitempty"`
}

type SurfaceCorrectionsFile struct {
	SchemaVersion int                 `json:"schemaVersion"`
	Corrections   []SurfaceCorrection `json:"corrections"`
}

type SurfaceCorrection struct {
	SurfaceID        string   `json:"surfaceId"`
	Since            string   `json:"since"`
	Until            string   `json:"until,omitempty"`
	SourceAPIVersion string   `json:"sourceApiVersion"`
	SourcePath       string   `json:"sourcePath"`
	SourceRefs       []string `json:"sourceRefs"`
	Reason           string   `json:"reason"`
}

type ReleaseSourceReceipt struct {
	SchemaVersion   int                   `json:"schemaVersion"`
	Release         string                `json:"release"`
	APIVersion      string                `json:"apiVersion"`
	ManifestDigest  string                `json:"manifestDigest"`
	InventorySHA256 string                `json:"inventorySHA256"`
	Generator       SourceToolReceipt     `json:"generator"`
	Snapshot        SourceSnapshotReceipt `json:"snapshot"`
}

type SourceToolReceipt struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type SourceSnapshotReceipt struct {
	SHA256                string                `json:"sha256"`
	MetadataSHA256        string                `json:"metadataSHA256"`
	AtlasVersion          string                `json:"atlasVersion"`
	AtlasVersionLabel     string                `json:"atlasVersionLabel"`
	TargetAPIVersion      string                `json:"targetAPIVersion"`
	TotalPages            int                   `json:"totalPages"`
	Assembler             SourceToolReceipt     `json:"assembler"`
	VersionedSourceSHA256 string                `json:"versionedSourceSHA256"`
	Families              []SourceFamilyReceipt `json:"families"`
	LWC                   LWCSourceReceipt      `json:"lwc"`
	Limitations           []string              `json:"limitations"`
}

type SourceFamilyReceipt struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	FileCount int    `json:"fileCount"`
	SHA256    string `json:"sha256"`
}

type LWCSourceReceipt struct {
	FilterReceiptSHA256     string                      `json:"filterReceiptSHA256"`
	SourceVersion           string                      `json:"sourceVersion"`
	SourceVersionSHA256     string                      `json:"sourceVersionSHA256"`
	SourceVersionMetadata   LWCSourceVersionReceipt     `json:"sourceVersionMetadata"`
	AvailabilityTable       string                      `json:"availabilityTable"`
	AvailabilityTableSHA256 string                      `json:"availabilityTableSHA256"`
	CopiedMarkdownFiles     int                         `json:"copiedMarkdownFiles"`
	Copied                  []SourceFileReceipt         `json:"copied"`
	Excluded                []ExcludedSourceFileReceipt `json:"excluded"`
	Limitation              string                      `json:"limitation"`
}

type LWCSourceVersionReceipt struct {
	File    string `json:"file"`
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

type SourceFileReceipt struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type ExcludedSourceFileReceipt struct {
	File                     string `json:"file"`
	SourceSHA256             string `json:"sourceSHA256"`
	Module                   string `json:"module"`
	FirstAvailableAPIVersion string `json:"firstAvailableAPIVersion"`
}

// Analyze checks the cross-file bindings in a release contract and returns a
// static report. It deliberately leaves evidence credit at zero: runtime and
// product-test proof belongs to the verifier.
func Analyze(contractPath string) (Analysis, error) {
	contract, root, err := Load(contractPath)
	if err != nil {
		return Analysis{}, err
	}
	a := Analysis{Contract: contract, Report: Report{SchemaVersion: 1, Status: "fail", Ranges: map[string]Range{}}}
	a.Report.SourceVersions = AxisReport{Advertised: versionStrings(contract.Windows.Source), Passing: []string{}}
	a.Report.EndpointVersions = AxisReport{Advertised: versionStrings(contract.Windows.Endpoint), Passing: []string{}}
	a.Report.OrgProfiles = AxisReport{Advertised: profileStrings(contract.Windows.OrgProfiles), Passing: []string{}}

	releases := make([]loadedRelease, len(contract.Releases))
	for i, release := range contract.Releases {
		manifest, err := loadManifest(filepath.Join(root, release.Manifest))
		if err != nil {
			return a, fmt.Errorf("release %s manifest: %w", release.Name, err)
		}
		if manifest.Release != release.Name || manifest.APIVersion != release.APIVersion {
			return a, fmt.Errorf("manifest identity mismatch for %s", release.Name)
		}
		var inv apexdocs.Inventory
		if err := readStrict(filepath.Join(root, release.Inventory), &inv); err != nil {
			return a, fmt.Errorf("release %s inventory: %w", release.Name, err)
		}
		if digest := apexdocs.CanonicalDigest(inv); digest != manifest.Digest {
			return a, fmt.Errorf("release %s inventory digest %s does not match manifest %s", release.Name, digest, manifest.Digest)
		}
		receiptPath := filepath.Join(root, release.SourceReceipt)
		receiptSHA256, err := sourceFileSHA256(receiptPath)
		if err != nil {
			return a, fmt.Errorf("release %s source receipt: %w", release.Name, err)
		}
		if receiptSHA256 != release.SourceReceiptSHA256 {
			return a, fmt.Errorf("release %s source receipt SHA-256 mismatch", release.Name)
		}
		var receipt ReleaseSourceReceipt
		if err := readStrict(receiptPath, &receipt); err != nil {
			return a, fmt.Errorf("release %s source receipt: %w", release.Name, err)
		}
		inventorySHA256, err := sourceFileSHA256(filepath.Join(root, release.Inventory))
		if err != nil {
			return a, err
		}
		if err := validateReleaseSourceReceipt(root, release, manifest, inventorySHA256, receipt); err != nil {
			return a, fmt.Errorf("release %s source receipt: %w", release.Name, err)
		}
		if inv.SchemaVersion != apexdocs.InventorySchemaVersion {
			return a, fmt.Errorf("release %s inventory schemaVersion must be %d", release.Name, apexdocs.InventorySchemaVersion)
		}
		rows, err := surfaceledger.MergeReleaseSnapshot(surfaceledger.ReleaseRowsFromDocsInventory(inv), release.APIVersion)
		if err != nil {
			return a, fmt.Errorf("release %s snapshot: %w", release.Name, err)
		}
		releases[i] = loadedRelease{release: release, manifest: manifest, inventory: inv, rows: rows.Rows}
		if i > 0 && !sameStrings(releases[i-1].manifest.SourceFamilies, manifest.SourceFamilies) {
			return a, fmt.Errorf("source families differ between releases")
		}
	}
	if contract.SurfaceCorrections != "" {
		corrections, err := loadSurfaceCorrections(filepath.Join(root, contract.SurfaceCorrections))
		if err != nil {
			return a, err
		}
		if err := applySurfaceCorrections(releases, corrections); err != nil {
			return a, err
		}
	}
	if err := backfillDocumentedRows(releases); err != nil {
		return a, err
	}

	presence := make(map[string][]bool)
	for i, release := range releases {
		for _, row := range release.rows {
			key := surfaceledger.CanonicalSurfaceIDKey(row.SurfaceID)
			if len(presence[key]) == 0 {
				presence[key] = make([]bool, len(releases))
			}
			presence[key][i] = true
		}
	}
	for key, seen := range presence {
		first := -1
		for i, yes := range seen {
			if yes {
				first = i
				break
			}
		}
		if first < 0 {
			continue
		}
		r := Range{Since: apiInt(releases[first].release.APIVersion)}
		for i := first + 1; i < len(seen); i++ {
			if !seen[i] && r.Until == 0 {
				r.Until = apiInt(releases[i].release.APIVersion)
			}
			if seen[i] && r.Until != 0 && i > first+1 {
				a.Report.Unclassified = append(a.Report.Unclassified, fmt.Sprintf("%s -> %s: %s (reappeared)", releases[i-1].release.Name, releases[i].release.Name, key))
			}
		}
		a.Report.Ranges[key] = r
	}

	behaviorByID := make(map[string]Behavior, len(contract.Behaviors))
	reachedBehaviors := make(map[string]bool)
	for _, behavior := range contract.Behaviors {
		behaviorByID[behavior.ID] = behavior
	}
	a.Report.BehaviorDelta = Denominator{Total: len(contract.Behaviors), Classified: len(contract.Behaviors)}
	for _, behavior := range contract.Behaviors {
		if behavior.Outcome == "explicit-non-parity" {
			a.Report.BehaviorDelta.ExplicitNonParity++
		}
	}

	for i := 1; i < len(releases); i++ {
		prev, curr := releases[i-1], releases[i]
		added, removed, changed, _, err := surfaceledger.DiffReleaseRows(prev.rows, curr.rows)
		if err != nil {
			return a, fmt.Errorf("diff %s -> %s: %w", prev.release.Name, curr.release.Name, err)
		}
		entries := append(append(append([]surfaceledger.DeltaEntry{}, added...), removed...), changed...)
		sort.Slice(entries, func(i, j int) bool { return entries[i].SurfaceID < entries[j].SurfaceID })
		allIDs := make(map[string]bool, len(entries))
		for _, entry := range entries {
			allIDs[surfaceledger.CanonicalSurfaceIDKey(entry.SurfaceID)] = true
		}
		classFile, err := loadClassification(filepath.Join(root, curr.release.Classifications))
		if err != nil {
			return a, err
		}
		if classFile.SchemaVersion != 2 {
			return a, fmt.Errorf("classifications schemaVersion must be 2")
		}
		if classFile.PreviousRelease != prev.release.Name || classFile.CurrentRelease != curr.release.Name {
			return a, fmt.Errorf("classifications release names mismatch")
		}
		classByID := map[string]surfaceledger.ReleaseClassification{}
		for classIndex, c := range classFile.Classifications {
			if err := surfaceledger.ValidateReleaseClassification(c); err != nil {
				return a, err
			}
			if strings.TrimSpace(c.CaseID) == "" && len(c.ProductTests) == 0 {
				return a, fmt.Errorf("classification %s requires caseId or productTests", c.SurfaceID)
			}
			for productIndex, productTest := range c.ProductTests {
				if err := validateProductTest(root, fmt.Sprintf("classifications[%d].productTests[%d]", classIndex, productIndex), productTest); err != nil {
					return a, err
				}
			}
			key := surfaceledger.CanonicalSurfaceIDKey(c.SurfaceID)
			if !allIDs[key] {
				return a, fmt.Errorf("classification for row not in added, removed, or changed: %s", key)
			}
			if _, exists := classByID[key]; exists {
				return a, fmt.Errorf("duplicate classification for: %s", key)
			}
			classByID[key] = c
		}
		for _, entry := range entries {
			key := surfaceledger.CanonicalSurfaceIDKey(entry.SurfaceID)
			c, ok := classByID[key]
			if ok {
				a.Report.SurfaceDelta.Classified++
				if c.Disposition == surfaceledger.DispoExplicitUnsupported {
					a.Report.SurfaceDelta.ExplicitNonParity++
				}
			} else {
				a.Report.Unclassified = append(a.Report.Unclassified, fmt.Sprintf("%s -> %s: %s", prev.release.Name, curr.release.Name, entry.SurfaceID))
			}
			cc := c
			if !ok {
				a.SurfaceChanges = append(a.SurfaceChanges, SurfaceChange{PreviousRelease: prev.release.Name, CurrentRelease: curr.release.Name, Entry: entry})
			} else {
				a.SurfaceChanges = append(a.SurfaceChanges, SurfaceChange{PreviousRelease: prev.release.Name, CurrentRelease: curr.release.Name, Entry: entry, Classification: &cc})
			}
		}
		a.Report.SurfaceDelta.Total += len(entries)
		noteInv, routes, err := loadRoutes(root, prev.release, curr.release)
		if err != nil {
			return a, err
		}
		a.Report.ChangeInventory.Total += len(noteInv.Documents)
		for _, route := range routes.Routes {
			if err := validateRoute(route, allIDs, behaviorByID, curr.release.APIVersion); err != nil {
				return a, err
			}
			a.Report.ChangeInventory.Routed++
			if strings.TrimSpace(route.OutOfScopeReason) != "" {
				a.Report.ChangeInventory.OutOfScope++
			} else {
				for _, id := range route.BehaviorIDs {
					reachedBehaviors[id] = true
				}
			}
			a.ChangeRoutes = append(a.ChangeRoutes, route)
		}
		for _, doc := range noteInv.Documents {
			found := false
			for _, route := range routes.Routes {
				if route.SourcePath == doc.SourcePath {
					found = true
					break
				}
			}
			if !found {
				a.Report.Unclassified = append(a.Report.Unclassified, fmt.Sprintf("%s -> %s: change inventory %s", prev.release.Name, curr.release.Name, doc.SourcePath))
			}
		}
	}
	for _, behavior := range contract.Behaviors {
		if behavior.Kind != "retired" && !reachedBehaviors[behavior.ID] {
			label := "unrouted behavior: " + behavior.ID
			boundary := behavior.Since
			if boundary == "" {
				boundary = behavior.Until
			}
			for i := 1; i < len(releases); i++ {
				if releases[i].release.APIVersion == boundary {
					label = fmt.Sprintf("%s -> %s: %s", releases[i-1].release.Name, releases[i].release.Name, label)
					break
				}
			}
			return a, fmt.Errorf("%s", label)
		}
	}
	if len(releases) > 1 {
		a.Report.PreviousRelease, a.Report.CurrentRelease = releases[len(releases)-2].release.Name, releases[len(releases)-1].release.Name
	}
	sort.Strings(a.Report.Unclassified)
	sort.Slice(a.ChangeRoutes, func(i, j int) bool { return a.ChangeRoutes[i].SourcePath < a.ChangeRoutes[j].SourcePath })
	return a, nil
}

type loadedRelease struct {
	release   Release
	manifest  apexdocs.ReleaseManifest
	inventory apexdocs.Inventory
	rows      []surfaceledger.SurfaceLedgerRow
}

// backfillDocumentedRows repairs an earlier captured snapshot only when a
// later exact row carries an official API availability boundary. It never
// fills a later absence, which remains a possible removal.
func backfillDocumentedRows(releases []loadedRelease) error {
	indexes := make([]map[string]bool, len(releases))
	for i := range releases {
		indexes[i] = make(map[string]bool, len(releases[i].rows))
		for _, row := range releases[i].rows {
			indexes[i][surfaceledger.CanonicalSurfaceIDKey(row.SurfaceID)] = true
		}
	}
	for sourceIndex := range releases {
		for _, row := range releases[sourceIndex].rows {
			if row.APIVersion == "" {
				continue
			}
			if err := validateAPIVersion("documented surface "+row.SurfaceID, row.APIVersion); err != nil {
				return err
			}
			if compareAPIVersions(row.APIVersion, releases[sourceIndex].release.APIVersion) > 0 {
				return fmt.Errorf("documented surface %s requires API %s after source release %s", row.SurfaceID, row.APIVersion, releases[sourceIndex].release.APIVersion)
			}
			key := surfaceledger.CanonicalSurfaceIDKey(row.SurfaceID)
			for targetIndex := 0; targetIndex < sourceIndex; targetIndex++ {
				if compareAPIVersions(releases[targetIndex].release.APIVersion, row.APIVersion) < 0 || indexes[targetIndex][key] {
					continue
				}
				releases[targetIndex].rows = append(releases[targetIndex].rows, row)
				indexes[targetIndex][key] = true
			}
		}
	}
	for i := range releases {
		sort.SliceStable(releases[i].rows, func(left, right int) bool {
			return releases[i].rows[left].SurfaceID < releases[i].rows[right].SurfaceID
		})
	}
	return nil
}

func loadSurfaceCorrections(path string) (SurfaceCorrectionsFile, error) {
	var file SurfaceCorrectionsFile
	if err := readStrict(path, &file); err != nil {
		return file, fmt.Errorf("surface corrections: %w", err)
	}
	return file, nil
}

// applySurfaceCorrections fills exact rows that a reviewed historical export
// omitted. The checked range is authoritative; no row shape is synthesized.
func applySurfaceCorrections(releases []loadedRelease, file SurfaceCorrectionsFile) error {
	if file.SchemaVersion != 1 {
		return fmt.Errorf("surface corrections schemaVersion must be 1")
	}
	releaseByAPI := make(map[string]int, len(releases))
	indexes := make([]map[string]int, len(releases))
	for releaseIndex := range releases {
		releaseByAPI[releases[releaseIndex].release.APIVersion] = releaseIndex
		indexes[releaseIndex] = make(map[string]int, len(releases[releaseIndex].rows))
		for rowIndex, row := range releases[releaseIndex].rows {
			indexes[releaseIndex][surfaceledger.CanonicalSurfaceIDKey(row.SurfaceID)] = rowIndex
		}
	}
	seen := make(map[string]bool, len(file.Corrections))
	for correctionIndex, correction := range file.Corrections {
		label := fmt.Sprintf("surface corrections[%d]", correctionIndex)
		key := surfaceledger.CanonicalSurfaceIDKey(correction.SurfaceID)
		if key == "" {
			return fmt.Errorf("%s surfaceId is empty", label)
		}
		if seen[key] {
			return fmt.Errorf("duplicate surface correction for %s", correction.SurfaceID)
		}
		seen[key] = true
		if err := validateAPIVersion(label+".since", correction.Since); err != nil {
			return err
		}
		if _, ok := releaseByAPI[correction.Since]; !ok {
			return fmt.Errorf("%s since %s has no release", label, correction.Since)
		}
		if correction.Until != "" {
			if err := validateAPIVersion(label+".until", correction.Until); err != nil {
				return err
			}
			if _, ok := releaseByAPI[correction.Until]; !ok {
				return fmt.Errorf("%s until %s has no release", label, correction.Until)
			}
			if compareAPIVersions(correction.Since, correction.Until) >= 0 {
				return fmt.Errorf("%s since must be before until", label)
			}
		}
		if err := validateAPIVersion(label+".sourceApiVersion", correction.SourceAPIVersion); err != nil {
			return err
		}
		sourceIndex, ok := releaseByAPI[correction.SourceAPIVersion]
		if !ok {
			return fmt.Errorf("%s sourceApiVersion %s has no release", label, correction.SourceAPIVersion)
		}
		if compareAPIVersions(correction.SourceAPIVersion, correction.Since) < 0 || correction.Until != "" && compareAPIVersions(correction.SourceAPIVersion, correction.Until) >= 0 {
			return fmt.Errorf("%s sourceApiVersion is outside correction range", label)
		}
		sourceRowIndex, ok := indexes[sourceIndex][key]
		if !ok {
			return fmt.Errorf("%s sourceApiVersion has no exact row %s", label, correction.SurfaceID)
		}
		sourceRow := releases[sourceIndex].rows[sourceRowIndex]
		if sourceRow.SurfaceID != correction.SurfaceID {
			return fmt.Errorf("%s surfaceId must exactly match source row %s", label, sourceRow.SurfaceID)
		}
		if strings.TrimSpace(correction.SourcePath) == "" || sourceRow.DocsSource != correction.SourcePath {
			return fmt.Errorf("%s sourcePath does not match source row", label)
		}
		if strings.TrimSpace(correction.Reason) == "" {
			return fmt.Errorf("%s reason is empty", label)
		}
		if len(correction.SourceRefs) == 0 {
			return fmt.Errorf("%s sourceRefs is empty", label)
		}
		for sourceRefIndex, sourceRef := range correction.SourceRefs {
			if err := validateSalesforceURL(fmt.Sprintf("%s.sourceRefs[%d]", label, sourceRefIndex), sourceRef); err != nil {
				return err
			}
		}
		sourceRow.APIVersion = correction.Since
		for targetIndex := range releases {
			targetAPI := releases[targetIndex].release.APIVersion
			if compareAPIVersions(targetAPI, correction.Since) < 0 || correction.Until != "" && compareAPIVersions(targetAPI, correction.Until) >= 0 {
				continue
			}
			if rowIndex, exists := indexes[targetIndex][key]; exists {
				releases[targetIndex].rows[rowIndex].APIVersion = correction.Since
				continue
			}
			releases[targetIndex].rows = append(releases[targetIndex].rows, sourceRow)
			indexes[targetIndex][key] = len(releases[targetIndex].rows) - 1
		}
	}
	for releaseIndex := range releases {
		sort.SliceStable(releases[releaseIndex].rows, func(left, right int) bool {
			return releases[releaseIndex].rows[left].SurfaceID < releases[releaseIndex].rows[right].SurfaceID
		})
	}
	return nil
}

func sourceFileSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(data)), nil
}

func validateReleaseSourceReceipt(root string, release Release, manifest apexdocs.ReleaseManifest, inventorySHA256 string, receipt ReleaseSourceReceipt) error {
	if receipt.SchemaVersion != 1 || receipt.Release != release.Name || receipt.APIVersion != release.APIVersion || receipt.Snapshot.TargetAPIVersion != release.APIVersion {
		return fmt.Errorf("identity mismatch")
	}
	if receipt.ManifestDigest != manifest.Digest || receipt.InventorySHA256 != inventorySHA256 {
		return fmt.Errorf("manifest or inventory binding mismatch")
	}
	wantManifestFamilies := []string{"apex-reference", "aura-reference", "lwc-reference", "rest-api-reference", "tooling-api-reference", "visualforce-reference"}
	if !sameStrings(manifest.SourceFamilies, wantManifestFamilies) {
		return fmt.Errorf("manifest source families do not match the receipted six-family snapshot")
	}
	wantAtlasVersion := fmt.Sprintf("%d.0", 2*apiInt(release.APIVersion)+128)
	if receipt.Snapshot.AtlasVersion != wantAtlasVersion || !strings.Contains(receipt.Snapshot.AtlasVersionLabel, "API v"+release.APIVersion) || receipt.Snapshot.TotalPages <= 0 {
		return fmt.Errorf("Atlas source identity mismatch")
	}
	if receipt.Generator.Path != "scripts/salesforce-release/export_source_receipt.py" || receipt.Snapshot.Assembler.Path != "scripts/salesforce-release/assemble_versioned_docs.py" {
		return fmt.Errorf("source tool path mismatch")
	}
	for label, value := range map[string]string{
		"generator":              receipt.Generator.SHA256,
		"snapshot":               receipt.Snapshot.SHA256,
		"snapshot metadata":      receipt.Snapshot.MetadataSHA256,
		"assembler":              receipt.Snapshot.Assembler.SHA256,
		"versioned source":       receipt.Snapshot.VersionedSourceSHA256,
		"LWC filter receipt":     receipt.Snapshot.LWC.FilterReceiptSHA256,
		"LWC source":             receipt.Snapshot.LWC.SourceVersionSHA256,
		"LWC version metadata":   receipt.Snapshot.LWC.SourceVersionMetadata.SHA256,
		"LWC availability table": receipt.Snapshot.LWC.AvailabilityTableSHA256,
	} {
		if !sha256Pattern.MatchString(value) {
			return fmt.Errorf("%s SHA-256 is invalid", label)
		}
	}

	wantFamilies := []string{"apex", "lightning-aura", "rest-api", "tooling-api", "visualforce"}
	if len(receipt.Snapshot.Families) != len(wantFamilies) {
		return fmt.Errorf("Atlas family count mismatch")
	}
	seenFamilies := map[string]bool{}
	for _, family := range receipt.Snapshot.Families {
		if seenFamilies[family.Name] || !slices.Contains(wantFamilies, family.Name) || family.Version != wantAtlasVersion || family.FileCount <= 0 || !sha256Pattern.MatchString(family.SHA256) {
			return fmt.Errorf("invalid Atlas family %q", family.Name)
		}
		seenFamilies[family.Name] = true
	}

	lwc := receipt.Snapshot.LWC
	if lwc.SourceVersion != "latest" || lwc.SourceVersionMetadata.Version != "latest" || lwc.SourceVersionMetadata.File != "_version.json" || lwc.AvailabilityTable != "reference-api-modules.md" {
		return fmt.Errorf("LWC source must retain latest current-only provenance")
	}
	if !strings.Contains(lwc.Limitation, "current-release-only") || !strings.Contains(lwc.Limitation, "availability-filtered") {
		return fmt.Errorf("LWC limitation is incomplete")
	}
	foundLimitation := false
	for _, limitation := range receipt.Snapshot.Limitations {
		foundLimitation = foundLimitation || limitation == lwc.Limitation
	}
	if !foundLimitation {
		return fmt.Errorf("snapshot does not retain the LWC limitation")
	}
	copiedMarkdown := 0
	seenCopied := map[string]bool{}
	for index, file := range lwc.Copied {
		if err := validateRepositoryPath(root, fmt.Sprintf("copied[%d].path", index), file.Path); err != nil {
			return err
		}
		if seenCopied[file.Path] || !sha256Pattern.MatchString(file.SHA256) {
			return fmt.Errorf("invalid copied LWC file %q", file.Path)
		}
		seenCopied[file.Path] = true
		if strings.HasSuffix(file.Path, ".md") {
			copiedMarkdown++
		}
	}
	if copiedMarkdown == 0 || copiedMarkdown != lwc.CopiedMarkdownFiles {
		return fmt.Errorf("copied LWC Markdown count mismatch")
	}
	for index, file := range lwc.Excluded {
		if err := validateRepositoryPath(root, fmt.Sprintf("excluded[%d].file", index), file.File); err != nil {
			return err
		}
		if !sha256Pattern.MatchString(file.SourceSHA256) || strings.TrimSpace(file.Module) == "" {
			return fmt.Errorf("invalid excluded LWC file %q", file.File)
		}
		if err := validateAPIVersion(fmt.Sprintf("excluded[%d].firstAvailableAPIVersion", index), file.FirstAvailableAPIVersion); err != nil {
			return err
		}
	}
	return nil
}

func loadManifest(path string) (apexdocs.ReleaseManifest, error) {
	var m apexdocs.ReleaseManifest
	if err := readStrict(path, &m); err != nil {
		return m, err
	}
	if m.SchemaVersion != apexdocs.InventorySchemaVersion {
		return m, fmt.Errorf("manifest schemaVersion must be 1")
	}
	if strings.TrimSpace(m.Release) == "" || strings.TrimSpace(m.APIVersion) == "" || m.Digest == "" || strings.TrimSpace(m.Acquisition) == "" || len(m.SourceFamilies) == 0 {
		return m, fmt.Errorf("manifest missing identity fields")
	}
	for _, family := range m.SourceFamilies {
		if strings.TrimSpace(family) == "" {
			return m, fmt.Errorf("manifest has blank source family")
		}
	}
	return m, nil
}
func loadClassification(path string) (ClassificationFile, error) {
	var f ClassificationFile
	if err := readStrict(path, &f); err != nil {
		return f, fmt.Errorf("classifications: %w", err)
	}
	return f, nil
}
func loadRoutes(root string, prev, curr Release) (apexdocs.Inventory, ReleaseNoteRoutesFile, error) {
	var inv apexdocs.Inventory
	if err := readStrict(filepath.Join(root, curr.ChangeInventory), &inv); err != nil {
		return inv, ReleaseNoteRoutesFile{}, fmt.Errorf("change inventory: %w", err)
	}
	if inv.SchemaVersion != apexdocs.InventorySchemaVersion {
		return inv, ReleaseNoteRoutesFile{}, fmt.Errorf("change inventory schemaVersion must be 1")
	}
	var routes ReleaseNoteRoutesFile
	if err := readStrict(filepath.Join(root, curr.ChangeRoutes), &routes); err != nil {
		return inv, routes, fmt.Errorf("change routes: %w", err)
	}
	if routes.SchemaVersion != 1 || routes.PreviousRelease != prev.Name || routes.CurrentRelease != curr.Name {
		return inv, routes, fmt.Errorf("change routes identity mismatch")
	}
	if routes.InventoryDigest != apexdocs.CanonicalDigest(inv) {
		return inv, routes, fmt.Errorf("change routes inventory digest mismatch")
	}
	seen := map[string]bool{}
	for _, route := range routes.Routes {
		if seen[route.SourcePath] {
			return inv, routes, fmt.Errorf("duplicate route %q", route.SourcePath)
		}
		seen[route.SourcePath] = true
	}
	for _, route := range routes.Routes {
		found := false
		for _, doc := range inv.Documents {
			if route.SourcePath == doc.SourcePath {
				found = true
				break
			}
		}
		if !found {
			return inv, routes, fmt.Errorf("stale route %q", route.SourcePath)
		}
	}
	return inv, routes, nil
}
func validateRoute(route ReleaseNoteRoute, delta map[string]bool, behaviors map[string]Behavior, api string) error {
	reason := strings.TrimSpace(route.OutOfScopeReason)
	hasIDs := len(route.SurfaceIDs) > 0 || len(route.BehaviorIDs) > 0
	if reason != "" && hasIDs {
		return fmt.Errorf("route %q mixes IDs and outOfScopeReason", route.SourcePath)
	}
	if reason == "" && !hasIDs {
		return fmt.Errorf("route %q is empty", route.SourcePath)
	}
	if reason != "" {
		return nil
	}
	seen := map[string]bool{}
	seenBehavior := map[string]bool{}
	for _, id := range route.SurfaceIDs {
		key := surfaceledger.CanonicalSurfaceIDKey(id)
		if !delta[key] {
			return fmt.Errorf("route %q references unknown surface %q", route.SourcePath, id)
		}
		if seen[key] {
			return fmt.Errorf("route %q duplicates surface %q", route.SourcePath, id)
		}
		seen[key] = true
	}
	for _, id := range route.BehaviorIDs {
		behaviorKey := id
		if seenBehavior[behaviorKey] {
			return fmt.Errorf("route %q duplicates behavior %q", route.SourcePath, id)
		}
		b, ok := behaviors[id]
		if !ok {
			return fmt.Errorf("route %q references unknown behavior %q", route.SourcePath, id)
		}
		bound := b.DocumentedIn
		if bound == "" && (b.Since == api || b.Until == api) {
			bound = api
		}
		if bound != api {
			return fmt.Errorf("route %q behavior %q is not bound to %s", route.SourcePath, id, api)
		}
		seenBehavior[behaviorKey] = true
	}
	return nil
}

func readStrict(path string, dst any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return DecodeExactJSON(data, dst)
}
func apiInt(version string) int {
	i, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimSpace(version), ".0"))
	return i
}
func versionStrings(entries []VersionProof) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Version
	}
	sort.SliceStable(out, func(i, j int) bool { return apiInt(out[i]) < apiInt(out[j]) })
	return out
}
func profileStrings(entries []ProfileProof) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Name
	}
	sort.Strings(out)
	return out
}
func sameStrings(a, b []string) bool {
	aa, bb := append([]string(nil), a...), append([]string(nil), b...)
	sort.Strings(aa)
	sort.Strings(bb)
	return strings.Join(aa, "\x00") == strings.Join(bb, "\x00")
}
