package releasecontract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/apexdocs"
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
		{Name: "Winter '26", APIVersion: "65.0", Maturity: "ga", Manifest: "manifest-65.json", Inventory: "inventory-65.json"},
		{Name: "Spring '26", APIVersion: "66.0", Maturity: "ga", Manifest: "manifest-66.json", Inventory: "inventory-66.json", Classifications: "class-66.json", ChangeInventory: "notes-66.json", ChangeRoutes: "routes-66.json"},
		{Name: "Summer '26", APIVersion: "67.0", Maturity: "ga", Manifest: "manifest-67.json", Inventory: "inventory-67.json", Classifications: "class-67.json", ChangeInventory: "notes-67.json", ChangeRoutes: "routes-67.json"},
	}
	if f.reappear {
		invs = append(invs, inv("Stable", "New66", "New67"))
		releases = append(releases, Release{Name: "Winter '27", APIVersion: "68.0", Maturity: "ga", Manifest: "manifest-68.json", Inventory: "inventory-68.json", Classifications: "class-68.json", ChangeInventory: "notes-68.json", ChangeRoutes: "routes-68.json"})
	}
	for i, release := range releases {
		path := writeInv(filepath.Base(release.Inventory), invs[i])
		digest := apexdocs.CanonicalDigest(invs[i])
		_ = path
		writeJSON(release.Manifest, map[string]any{"schemaVersion": 1, "release": release.Name, "apiVersion": release.APIVersion, "digest": digest, "acquisition": "test", "sourceFamilies": []string{"apex"}})
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
