package releasecontract

import (
	"fmt"
	"os"
	"path/filepath"
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
		if inv.SchemaVersion != apexdocs.InventorySchemaVersion {
			return a, fmt.Errorf("release %s inventory schemaVersion must be %d", release.Name, apexdocs.InventorySchemaVersion)
		}
		rows, err := surfaceledger.MergeReleaseSnapshot(surfaceledger.RowsFromDocsInventory(inv), release.APIVersion)
		if err != nil {
			return a, fmt.Errorf("release %s snapshot: %w", release.Name, err)
		}
		releases[i] = loadedRelease{release: release, manifest: manifest, inventory: inv, rows: rows.Rows}
		if i > 0 && !sameStrings(releases[i-1].manifest.SourceFamilies, manifest.SourceFamilies) {
			return a, fmt.Errorf("source families differ between releases")
		}
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
			if strings.TrimSpace(route.OutOfScopeReason) != "" {
				a.Report.ChangeInventory.OutOfScope++
			} else {
				a.Report.ChangeInventory.Routed++
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
