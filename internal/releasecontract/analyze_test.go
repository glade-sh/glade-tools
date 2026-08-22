package releasecontract

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/apexdocs"
	"github.com/glade-sh/glade/tools/internal/surfaceledger"
)

func TestAnalyzeAdjacentReleases(t *testing.T) {
	root := t.TempDir()
	paths := writeAnalysisFixture(t, root, true)

	got, err := Analyze(paths.contract)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if got.Report.SurfaceDelta != (Denominator{Total: 3, Classified: 3}) {
		t.Fatalf("surface delta = %#v", got.Report.SurfaceDelta)
	}
	if got.Report.ChangeInventory != (ChangeInventoryDenominator{Total: 2, Routed: 2}) {
		t.Fatalf("change inventory = %#v", got.Report.ChangeInventory)
	}
	if got.Report.PreviousRelease != "Spring '26" || got.Report.CurrentRelease != "Summer '26" {
		t.Fatalf("report releases = %q -> %q", got.Report.PreviousRelease, got.Report.CurrentRelease)
	}
	wantRanges := map[string]Range{
		"apex:system.new66":  {Since: 66, Until: 67},
		"apex:system.new67":  {Since: 67},
		"apex:system.stable": {Since: 65},
	}
	if !reflect.DeepEqual(got.Report.Ranges, wantRanges) {
		t.Fatalf("ranges = %#v, want %#v", got.Report.Ranges, wantRanges)
	}
	if got.Report.SourceVersions.Passing == nil || got.Report.EndpointVersions.Passing == nil || got.Report.OrgProfiles.Passing == nil {
		t.Fatal("axis passing lists must be non-nil")
	}
	data, err := json.Marshal(got.Report)
	if err != nil || strings.Contains(string(data), `"passing":null`) {
		t.Fatalf("report JSON = %s", data)
	}
}

func TestAnalyzeSortsAdvertisedAxes(t *testing.T) {
	root := t.TempDir()
	paths := writeAnalysisFixture(t, root, true)
	contract, _, err := Load(paths.contract)
	if err != nil {
		t.Fatal(err)
	}
	contract.Windows.Source[0], contract.Windows.Source[2] = contract.Windows.Source[2], contract.Windows.Source[0]
	contract.Windows.Endpoint = append([]VersionProof{{Version: "64.0", ProductTests: []string{"x_test.go:Test64"}}}, contract.Windows.Endpoint...)
	contract.Windows.OrgProfiles = append(contract.Windows.OrgProfiles, ProfileProof{Name: "alpha", ProductTests: []string{"x_test.go:TestAlpha"}})
	data, _ := json.Marshal(contract)
	if err := os.WriteFile(paths.contract, data, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Analyze(paths.contract)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Report.SourceVersions.Advertised, []string{"65.0", "66.0", "67.0"}) || !reflect.DeepEqual(got.Report.EndpointVersions.Advertised, []string{"64.0", "65.0"}) || !reflect.DeepEqual(got.Report.OrgProfiles.Advertised, []string{"alpha", "default"}) {
		t.Fatalf("axes = %#v %#v", got.Report.SourceVersions, got.Report.EndpointVersions)
	}
}

func TestAnalyzeMissingClassificationKeepsTotalAndPrefixesUnclassified(t *testing.T) {
	root := t.TempDir()
	paths := writeAnalysisFixture(t, root, false)
	got, err := Analyze(paths.contract)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if got.Report.SurfaceDelta.Total != 3 || got.Report.SurfaceDelta.Classified != 2 || got.Report.Status != "fail" {
		t.Fatalf("surface delta = %#v, status=%q", got.Report.SurfaceDelta, got.Report.Status)
	}
	if !reflect.DeepEqual(got.Report.Unclassified, []string{"Spring '26 -> Summer '26: apex:System.New66"}) {
		t.Fatalf("unclassified = %#v", got.Report.Unclassified)
	}
}

func TestAnalyzeRejectsStaleAndMixedRoutes(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*releaseAnalysisFixture)
		want   string
	}{
		{"stale route", func(f *releaseAnalysisFixture) { f.staleRoute = true }, "stale-route.json"},
		{"mixed route", func(f *releaseAnalysisFixture) { f.mixedRoute = true }, "outOfScopeReason"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			f := releaseAnalysisFixture{root: root}
			test.mutate(&f)
			paths := writeAnalysisFixtureWith(t, f, true)
			if _, err := Analyze(paths.contract); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Analyze error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestAnalyzeRejectsRouteIdentityAndDigest(t *testing.T) {
	tests := []struct{ name, field, value string }{
		{"previous", "previousRelease", "Wrong"},
		{"current", "currentRelease", "Wrong"},
		{"digest", "inventoryDigest", strings.Repeat("0", 64)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			writeAnalysisFixture(t, root, true)
			path := filepath.Join(root, "routes-67.json")
			var doc map[string]any
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(raw, &doc); err != nil {
				t.Fatal(err)
			}
			doc[test.field] = test.value
			raw, _ = json.Marshal(doc)
			if err := os.WriteFile(path, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Analyze(filepath.Join(root, "contract.json")); err == nil {
				t.Fatal("Analyze unexpectedly succeeded")
			}
		})
	}
}

func TestAnalyzeRejectsDuplicateEquivalentBehaviorIDs(t *testing.T) {
	root := t.TempDir()
	writeAnalysisFixture(t, root, true)
	path := filepath.Join(root, "routes-67.json")
	var doc map[string]any
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	routes := doc["routes"].([]any)
	route := routes[0].(map[string]any)
	route["behaviorIds"] = []string{"behavior-67", "behavior-67"}
	raw, _ = json.Marshal(doc)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Analyze(filepath.Join(root, "contract.json")); err == nil || !strings.Contains(err.Error(), "duplicates behavior") {
		t.Fatalf("Analyze error = %v", err)
	}
}

func TestValidateRouteAcceptsExplicitLateDocumentationRelease(t *testing.T) {
	route := ReleaseNoteRoute{SourcePath: "rn_lwc_modules.md", BehaviorIDs: []string{"lwc.block-builder"}}
	behavior := Behavior{
		ID: "lwc.block-builder", Axis: "source", Kind: "added", Outcome: "supported",
		Since: "66.0", DocumentedIn: "67.0", Maturity: "ga",
	}

	if err := validateRoute(route, nil, map[string]Behavior{behavior.ID: behavior}, "67.0"); err != nil {
		t.Fatalf("validateRoute: %v", err)
	}
	behavior.DocumentedIn = ""
	if err := validateRoute(route, nil, map[string]Behavior{behavior.ID: behavior}, "67.0"); err == nil || !strings.Contains(err.Error(), "not bound") {
		t.Fatalf("validateRoute error = %v, want not bound", err)
	}
}

func TestAnalyzeRejectsExactRouteJSON(t *testing.T) {
	for _, payload := range []string{
		`{"schemaVersion":1,"previousRelease":"Spring '26","currentRelease":"Summer '26","inventoryDigest":"x","routes":[],"routes":[]}`,
		`{"schemaVersion":1,"previousRelease":"Spring '26","currentRelease":"Summer '26","inventoryDigest":"x","routes":[{"sourcePath":"notes/67.0-a.md","SurfaceIDs":["apex:System.New67"]}]}`,
	} {
		root := t.TempDir()
		writeAnalysisFixture(t, root, true)
		if err := os.WriteFile(filepath.Join(root, "routes-67.json"), []byte(payload), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Analyze(filepath.Join(root, "contract.json")); err == nil {
			t.Fatal("Analyze unexpectedly succeeded")
		}
	}
}

func TestAnalyzeRejectsBlankClassificationProductTest(t *testing.T) {
	root := t.TempDir()
	writeAnalysisFixture(t, root, true)
	path := filepath.Join(root, "class-66.json")
	var doc map[string]any
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	entries := doc["classifications"].([]any)
	entry := entries[0].(map[string]any)
	delete(entry, "caseId")
	entry["disposition"] = "deterministic-mock"
	entry["reasonRef"] = "manual"
	entry["productTests"] = []string{" "}
	raw, _ = json.Marshal(doc)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Analyze(filepath.Join(root, "contract.json")); err == nil || !strings.Contains(err.Error(), "classifications[0].productTests[0]") {
		t.Fatalf("Analyze error = %v", err)
	}
}

func TestAnalyzeMissingRouteKeepsInventoryTotal(t *testing.T) {
	root := t.TempDir()
	paths := writeAnalysisFixtureWith(t, releaseAnalysisFixture{root: root, dropRoute: true, omitBehavior: true}, true)
	got, err := Analyze(paths.contract)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if got.Report.ChangeInventory != (ChangeInventoryDenominator{Total: 2, Routed: 1}) || got.Report.Status != "fail" {
		t.Fatalf("change inventory = %#v, status=%q", got.Report.ChangeInventory, got.Report.Status)
	}
	if !strings.Contains(strings.Join(got.Report.Unclassified, "\n"), "change inventory") {
		t.Fatalf("unclassified = %#v", got.Report.Unclassified)
	}
}

func TestAnalyzeCountsOutOfScopeDocumentsAsRouted(t *testing.T) {
	root := t.TempDir()
	paths := writeAnalysisFixture(t, root, true)
	path := filepath.Join(root, "routes-66.json")
	var routes ReleaseNoteRoutesFile
	if err := readStrict(path, &routes); err != nil {
		t.Fatal(err)
	}
	routes.Routes[0].SurfaceIDs = nil
	routes.Routes[0].OutOfScopeReason = "not in scope"
	data, _ := json.Marshal(routes)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := Analyze(paths.contract)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if got.Report.ChangeInventory != (ChangeInventoryDenominator{Total: 2, Routed: 2, OutOfScope: 1}) {
		t.Fatalf("change inventory = %#v", got.Report.ChangeInventory)
	}
}

func TestAnalyzeRejectsUnroutedBehaviorButAllowsRetired(t *testing.T) {
	root := t.TempDir()
	writeAnalysisFixtureWith(t, releaseAnalysisFixture{root: root, dropRoute: true}, true)
	if _, err := Analyze(filepath.Join(root, "contract.json")); err == nil || !strings.Contains(err.Error(), "unrouted behavior: behavior-67") {
		t.Fatalf("Analyze error = %v", err)
	}
	root = t.TempDir()
	writeAnalysisFixtureWith(t, releaseAnalysisFixture{root: root, dropRoute: true, retiredBehavior: true}, true)
	if _, err := Analyze(filepath.Join(root, "contract.json")); err != nil {
		t.Fatalf("retired Analyze: %v", err)
	}
}

func TestAnalyzeRejectsManifestIdentityDigestAndSourceFamilies(t *testing.T) {
	for _, test := range []struct{ name, field, value string }{
		{"identity", "release", "Wrong"},
		{"digest", "digest", strings.Repeat("0", 64)},
		{"source families", "sourceFamilies", "other"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			writeAnalysisFixture(t, root, true)
			path := filepath.Join(root, "manifest-66.json")
			var data map[string]any
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(raw, &data); err != nil {
				t.Fatal(err)
			}
			if test.field == "sourceFamilies" {
				data[test.field] = []string{test.value}
			} else {
				data[test.field] = test.value
			}
			raw, _ = json.Marshal(data)
			if err := os.WriteFile(path, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Analyze(filepath.Join(root, "contract.json")); err == nil {
				t.Fatal("Analyze unexpectedly succeeded")
			}
		})
	}
}

func TestAnalyzeRejectsSourceReceiptIdentity(t *testing.T) {
	root := t.TempDir()
	writeAnalysisFixture(t, root, true)
	path := filepath.Join(root, "source-66.json")
	var data map[string]any
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	data["release"] = "Wrong"
	raw, _ = json.Marshal(data)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Analyze(filepath.Join(root, "contract.json")); err == nil || !strings.Contains(err.Error(), "source receipt SHA-256") {
		t.Fatalf("Analyze error = %v", err)
	}
}

func TestAnalyzeFlagsSurfaceThatDisappearsAndReappears(t *testing.T) {
	root := t.TempDir()
	writeAnalysisFixtureWith(t, releaseAnalysisFixture{root: root, reappear: true}, true)
	got, err := Analyze(filepath.Join(root, "contract.json"))
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if got.Report.Ranges["apex:system.new66"] != (Range{Since: 66, Until: 67}) {
		t.Fatalf("ranges = %#v", got.Report.Ranges)
	}
	if !strings.Contains(strings.Join(got.Report.Unclassified, "\n"), "reappeared") {
		t.Fatalf("unclassified = %#v", got.Report.Unclassified)
	}
}

func TestBackfillDocumentedRowsOnlyRepairsEarlierSnapshots(t *testing.T) {
	row := surfaceRow("apex:ConnectApi.Late", "65.0")
	releases := []loadedRelease{
		{release: Release{Name: "Winter '26", APIVersion: "65.0"}},
		{release: Release{Name: "Spring '26", APIVersion: "66.0"}, rows: []surfaceledger.SurfaceLedgerRow{row}},
		{release: Release{Name: "Summer '26", APIVersion: "67.0"}},
	}
	if err := backfillDocumentedRows(releases); err != nil {
		t.Fatal(err)
	}
	if len(releases[0].rows) != 1 || releases[0].rows[0].SurfaceID != row.SurfaceID {
		t.Fatalf("Winter rows = %#v", releases[0].rows)
	}
	if len(releases[2].rows) != 0 {
		t.Fatalf("Summer rows = %#v, later absences must remain removals", releases[2].rows)
	}
}

func TestApplySurfaceCorrectionsFillsOnlyReviewedReleaseRange(t *testing.T) {
	finalizer := surfaceRow("apex:System.Finalizer", "")
	finalizer.DocsSource = "apex/apex_interface_System_Finalizer.md"
	deleteFilter := surfaceRow("apex:Database.DeleteFilter", "")
	deleteFilter.DocsSource = "apex/apex_class_Database_DeleteFilter.md"
	releases := []loadedRelease{
		{release: Release{Name: "Winter '26", APIVersion: "65.0"}, rows: []surfaceledger.SurfaceLedgerRow{deleteFilter}},
		{release: Release{Name: "Spring '26", APIVersion: "66.0"}},
		{release: Release{Name: "Summer '26", APIVersion: "67.0"}, rows: []surfaceledger.SurfaceLedgerRow{finalizer}},
	}
	file := SurfaceCorrectionsFile{SchemaVersion: 1, Corrections: []SurfaceCorrection{
		{
			SurfaceID: "apex:System.Finalizer", Since: "65.0", SourceAPIVersion: "67.0",
			SourcePath: finalizer.DocsSource, SourceRefs: []string{"https://developer.salesforce.com/docs/atlas.en-us.apexref.meta/apexref/apex_interface_System_Finalizer.htm"}, Reason: "Historical exports omit the page.",
		},
		{
			SurfaceID: "apex:Database.DeleteFilter", Since: "65.0", Until: "67.0", SourceAPIVersion: "65.0",
			SourcePath: deleteFilter.DocsSource, SourceRefs: []string{"https://developer.salesforce.com/docs/atlas.en-us.apexref.meta/apexref/apex_class_Database_DeleteFilter.htm"}, Reason: "The Spring export omits the page.",
		},
	}}

	if err := applySurfaceCorrections(releases, file); err != nil {
		t.Fatal(err)
	}
	for index := range releases {
		rows := map[string]surfaceledger.SurfaceLedgerRow{}
		for _, row := range releases[index].rows {
			rows[row.SurfaceID] = row
		}
		if rows["apex:System.Finalizer"].APIVersion != "65.0" {
			t.Fatalf("release %s Finalizer = %#v", releases[index].release.Name, rows["apex:System.Finalizer"])
		}
		_, hasDeleteFilter := rows["apex:Database.DeleteFilter"]
		if want := index < 2; hasDeleteFilter != want {
			t.Fatalf("release %s DeleteFilter present = %v, want %v", releases[index].release.Name, hasDeleteFilter, want)
		}
	}
}

func TestApplySurfaceCorrectionsRejectsUnboundAuthority(t *testing.T) {
	row := surfaceRow("apex:System.Finalizer", "")
	releases := []loadedRelease{
		{release: Release{Name: "Winter '26", APIVersion: "65.0"}},
		{release: Release{Name: "Summer '26", APIVersion: "67.0"}, rows: []surfaceledger.SurfaceLedgerRow{row}},
	}
	valid := SurfaceCorrection{
		SurfaceID: row.SurfaceID, Since: "65.0", SourceAPIVersion: "67.0", SourcePath: row.DocsSource,
		SourceRefs: []string{"https://developer.salesforce.com/docs/finalizer"}, Reason: "Historical export omission.",
	}
	for _, test := range []struct {
		name string
		edit func(*SurfaceCorrection)
		want string
	}{
		{"unknown source release", func(c *SurfaceCorrection) { c.SourceAPIVersion = "66.0" }, "sourceApiVersion"},
		{"wrong source path", func(c *SurfaceCorrection) { c.SourcePath = "apex/wrong.md" }, "sourcePath"},
		{"non Salesforce URL", func(c *SurfaceCorrection) { c.SourceRefs = []string{"https://example.com/finalizer"} }, "salesforce.com"},
		{"empty reason", func(c *SurfaceCorrection) { c.Reason = " " }, "reason"},
		{"source outside range", func(c *SurfaceCorrection) { c.Until = "67.0" }, "outside correction range"},
	} {
		t.Run(test.name, func(t *testing.T) {
			correction := valid
			test.edit(&correction)
			err := applySurfaceCorrections(releases, SurfaceCorrectionsFile{SchemaVersion: 1, Corrections: []SurfaceCorrection{correction}})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("applySurfaceCorrections error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestApplySurfaceCorrectionsReplacesImpossibleDocumentedVersion(t *testing.T) {
	row := surfaceRow("apex:ConnectApi.ContentHub.getRepository(String)", "369.0")
	releases := []loadedRelease{{release: Release{Name: "Winter '26", APIVersion: "65.0"}, rows: []surfaceledger.SurfaceLedgerRow{row}}}
	correction := SurfaceCorrection{
		SurfaceID: row.SurfaceID, Since: "65.0", SourceAPIVersion: "65.0", SourcePath: row.DocsSource,
		SourceRefs: []string{"https://resources.docs.salesforce.com/258/latest/en-us/sfdc/pdf/salesforce_apex_reference_guide.pdf"}, Reason: "The source says 369.0 inside an API 65.0 snapshot.",
	}
	if err := applySurfaceCorrections(releases, SurfaceCorrectionsFile{SchemaVersion: 1, Corrections: []SurfaceCorrection{correction}}); err != nil {
		t.Fatal(err)
	}
	if got := releases[0].rows[0].APIVersion; got != "65.0" {
		t.Fatalf("API version = %q", got)
	}
}

func TestCheckedSurfaceCorrectionsMatchReleaseInventories(t *testing.T) {
	root := filepath.Join("..", "..", "docs", "fixtures")
	specs := []Release{
		{Name: "Winter '26", APIVersion: "65.0", Inventory: "salesforce-docs-inventory-winter-26.json"},
		{Name: "Spring '26", APIVersion: "66.0", Inventory: "salesforce-docs-inventory-spring-26.json"},
		{Name: "Summer '26", APIVersion: "67.0", Inventory: "salesforce-docs-inventory-summer-26.json"},
	}
	releases := make([]loadedRelease, len(specs))
	for index, spec := range specs {
		var inventory apexdocs.Inventory
		if err := readStrict(filepath.Join(root, spec.Inventory), &inventory); err != nil {
			t.Fatal(err)
		}
		merged, err := surfaceledger.MergeReleaseSnapshot(surfaceledger.ReleaseRowsFromDocsInventory(inventory), spec.APIVersion)
		if err != nil {
			t.Fatal(err)
		}
		releases[index] = loadedRelease{release: spec, inventory: inventory, rows: merged.Rows}
	}
	corrections, err := loadSurfaceCorrections(filepath.Join(root, "salesforce-release-surface-corrections.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := applySurfaceCorrections(releases, corrections); err != nil {
		t.Fatal(err)
	}
	if err := backfillDocumentedRows(releases); err != nil {
		t.Fatal(err)
	}

	for index, want := range [][3]int{{147, 2, 2}, {367, 1, 0}} {
		added, removed, changed, _, err := surfaceledger.DiffReleaseRows(releases[index].rows, releases[index+1].rows)
		if err != nil {
			t.Fatal(err)
		}
		if got := [3]int{len(added), len(removed), len(changed)}; got != want {
			t.Fatalf("%s -> %s delta = %v, want %v", specs[index].Name, specs[index+1].Name, got, want)
		}
	}
}

func surfaceRow(id, apiVersion string) surfaceledger.SurfaceLedgerRow {
	return surfaceledger.SurfaceLedgerRow{SurfaceID: id, Product: surfaceledger.ProductApex, Kind: surfaceledger.KindType, APIVersion: apiVersion, DocsSource: "apex/source.md"}
}

type analysisPaths struct{ contract string }
type releaseAnalysisFixture struct {
	root            string
	staleRoute      bool
	mixedRoute      bool
	classifyNew66   bool
	dropRoute       bool
	reappear        bool
	retiredBehavior bool
	omitBehavior    bool
}

func writeAnalysisFixture(t *testing.T, root string, classifyNew66 bool) analysisPaths {
	return writeAnalysisFixtureWith(t, releaseAnalysisFixture{root: root, classifyNew66: classifyNew66}, classifyNew66)
}

func writeAnalysisFixtureWith(t *testing.T, f releaseAnalysisFixture, classifyNew66 bool) analysisPaths {
	root := f.root
	f.classifyNew66 = classifyNew66
	f.write(t)
	return analysisPaths{contract: filepath.Join(root, "contract.json")}
}

func (f *releaseAnalysisFixture) write(t *testing.T) {
	inv := func(names ...string) apexdocs.Inventory {
		docs := make([]apexdocs.Document, 0, len(names))
		for _, name := range names {
			docs = append(docs, apexdocs.Document{SourcePath: "apex/apex_class_System_" + name + ".md", Kind: "class", Namespace: "System", Name: name})
		}
		return apexdocs.Inventory{SchemaVersion: 1, Documents: docs}
	}
	writeJSON := func(name string, value any) string {
		path := filepath.Join(f.root, name)
		data, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	writeInv := func(name string, value apexdocs.Inventory) string { return writeJSON(name, value) }

	invs := []apexdocs.Inventory{inv("Stable"), inv("Stable", "New66"), inv("Stable", "New67")}
	releases := []Release{
		{Name: "Winter '26", APIVersion: "65.0", Maturity: "ga", Manifest: "manifest-65.json", Inventory: "inventory-65.json", SourceReceipt: "source-65.json"},
		{Name: "Spring '26", APIVersion: "66.0", Maturity: "ga", Manifest: "manifest-66.json", Inventory: "inventory-66.json", SourceReceipt: "source-66.json", Classifications: "class-66.json", ChangeInventory: "notes-66.json", ChangeRoutes: "routes-66.json"},
		{Name: "Summer '26", APIVersion: "67.0", Maturity: "ga", Manifest: "manifest-67.json", Inventory: "inventory-67.json", SourceReceipt: "source-67.json", Classifications: "class-67.json", ChangeInventory: "notes-67.json", ChangeRoutes: "routes-67.json"},
	}
	if f.reappear {
		invs = append(invs, inv("Stable", "New66", "New67"))
		releases = append(releases, Release{Name: "Winter '27", APIVersion: "68.0", Maturity: "ga", Manifest: "manifest-68.json", Inventory: "inventory-68.json", SourceReceipt: "source-68.json", Classifications: "class-68.json", ChangeInventory: "notes-68.json", ChangeRoutes: "routes-68.json"})
	}
	for i, release := range releases {
		path := writeInv(filepath.Base(release.Inventory), invs[i])
		digest := apexdocs.CanonicalDigest(invs[i])
		writeJSON(release.Manifest, map[string]any{"schemaVersion": 1, "release": release.Name, "apiVersion": release.APIVersion, "digest": digest, "acquisition": "test", "sourceFamilies": []string{"apex-reference", "aura-reference", "lwc-reference", "rest-api-reference", "tooling-api-reference", "visualforce-reference"}})
		atlasVersion := fmt.Sprintf("%d.0", 2*(65+i)+128)
		families := []map[string]any{}
		for _, family := range []string{"apex", "lightning-aura", "rest-api", "tooling-api", "visualforce"} {
			families = append(families, map[string]any{"name": family, "version": atlasVersion, "fileCount": 1, "sha256": strings.Repeat("a", 64)})
		}
		receiptPath := writeJSON(release.SourceReceipt, map[string]any{
			"schemaVersion": 1, "release": release.Name, "apiVersion": release.APIVersion, "manifestDigest": digest,
			"inventorySHA256": fmt.Sprintf("%x", sha256.Sum256(mustReadFile(t, path))),
			"generator":       map[string]any{"path": "scripts/salesforce-release/export_source_receipt.py", "sha256": strings.Repeat("b", 64)},
			"snapshot": map[string]any{
				"sha256": strings.Repeat("c", 64), "metadataSHA256": strings.Repeat("d", 64), "atlasVersion": atlasVersion,
				"atlasVersionLabel": "API v" + release.APIVersion, "targetAPIVersion": release.APIVersion, "totalPages": 6,
				"assembler":             map[string]any{"path": "scripts/salesforce-release/assemble_versioned_docs.py", "sha256": strings.Repeat("e", 64)},
				"versionedSourceSHA256": strings.Repeat("f", 64), "families": families,
				"lwc": map[string]any{
					"filterReceiptSHA256": strings.Repeat("1", 64), "sourceVersion": "latest", "sourceVersionSHA256": strings.Repeat("2", 64),
					"sourceVersionMetadata": map[string]any{"file": "_version.json", "version": "latest", "sha256": strings.Repeat("3", 64)},
					"availabilityTable":     "reference-api-modules.md", "availabilityTableSHA256": strings.Repeat("4", 64),
					"copiedMarkdownFiles": 1, "copied": []map[string]any{{"path": "reference-api-modules.md", "sha256": strings.Repeat("5", 64)}},
					"excluded": []any{}, "limitation": "Salesforce publishes the LWC reference as current-release-only; this is an availability-filtered view.",
				},
				"limitations": []string{"Salesforce publishes the LWC reference as current-release-only; this is an availability-filtered view."},
			},
		})
		receiptData := mustReadFile(t, receiptPath)
		releases[i].SourceReceiptSHA256 = fmt.Sprintf("%x", sha256.Sum256(receiptData))
		if i == 0 {
			continue
		}
		deltas := []string{"apex:System.New66"}
		if i == 2 {
			deltas = []string{"apex:System.New66", "apex:System.New67"}
		}
		entries := make([]map[string]any, 0, len(deltas))
		for _, id := range deltas {
			if id == "apex:System.New66" && i == 2 && !f.classifyNew66 {
				continue
			}
			entries = append(entries, map[string]any{"surfaceId": id, "scope": "t0", "disposition": "new-case", "caseId": "CASE-" + id})
		}
		writeJSON(release.Classifications, map[string]any{"schemaVersion": 2, "previousRelease": releases[i-1].Name, "currentRelease": release.Name, "classifications": entries})
		note := apexdocs.Inventory{SchemaVersion: 1, Documents: []apexdocs.Document{{SourcePath: "notes/" + release.APIVersion + "-a.md", Kind: "document", Name: "a"}}}
		writeInv(release.ChangeInventory, note)
		routes := []ReleaseNoteRoute{{SourcePath: note.Documents[0].SourcePath, SurfaceIDs: []string{deltas[0]}}}
		if i == 2 {
			routes[0].SurfaceIDs = deltas
			routes[0].BehaviorIDs = []string{"behavior-67"}
		}
		if f.dropRoute && i == 2 {
			routes = nil
		}
		if f.staleRoute {
			routes = append(routes, ReleaseNoteRoute{SourcePath: "stale-route.json", SurfaceIDs: []string{deltas[0]}})
		}
		if f.mixedRoute {
			routes[0].OutOfScopeReason = "not in scope"
		}
		writeJSON(release.ChangeRoutes, map[string]any{"schemaVersion": 1, "previousRelease": releases[i-1].Name, "currentRelease": release.Name, "inventoryDigest": apexdocs.CanonicalDigest(note), "routes": routes})
	}
	source := []VersionProof{{Version: "65.0", ProductTests: []string{"x_test.go:Test65"}}, {Version: "66.0", ProductTests: []string{"x_test.go:Test66"}}, {Version: "67.0", ProductTests: []string{"x_test.go:Test67"}}}
	if f.reappear {
		source = append(source, VersionProof{Version: "68.0", ProductTests: []string{"x_test.go:Test68"}})
	}
	behaviors := []Behavior{{ID: "behavior-67", Axis: "source", Kind: "added", Outcome: "supported", Since: "67.0", Maturity: "ga", SourceRefs: []string{"https://salesforce.com/release"}, ProductTests: []string{"x_test.go:TestBehavior"}}}
	if f.omitBehavior {
		behaviors = nil
	}
	if f.retiredBehavior {
		behaviors[0].Kind = "retired"
	}
	contract := Contract{SchemaVersion: 1, Defaults: Defaults{Source: "65.0", Endpoint: "65.0", OrgProfile: "default"}, Windows: Windows{Source: source, Endpoint: []VersionProof{{Version: "65.0", ProductTests: []string{"x_test.go:TestEndpoint"}}}, OrgProfiles: []ProfileProof{{Name: "default", ProductTests: []string{"x_test.go:TestOrg"}}}}, Releases: releases, Behaviors: behaviors, NoFallbackProductTests: []string{"x_test.go:TestFallback"}}
	writeJSON("contract.json", contract)
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
