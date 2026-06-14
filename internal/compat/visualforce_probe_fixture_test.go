package compat

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestVisualforceProbeFixture(t *testing.T) {
	root := filepath.Join("..", "..", "docs", "fixtures", "visualforce", "probe-project")
	requireFile(t, root, "sfdx-project.json")
	requireFile(t, root, "force-app", "main", "default", "classes", "ProbeController.cls")
	requireFile(t, root, "force-app", "main", "default", "classes", "ProbeRemotingController.cls")
	requireFile(t, root, "force-app", "main", "default", "classes", "ProbeStandardSetControllerExtension.cls")
	requireFile(t, root, "force-app", "main", "default", "components", "ProbeBadge.component")
	requireFile(t, root, "force-app", "main", "default", "components", "ProbePanel.component")
	requireFile(t, root, "force-app", "main", "default", "staticresources", "vfProbeCss.resource")
	requireFile(t, root, "force-app", "main", "default", "staticresources", "vfProbeJs.resource")

	var project struct {
		PackageDirectories []struct {
			Path    string `json:"path"`
			Default bool   `json:"default"`
		} `json:"packageDirectories"`
		SourceAPIVersion string `json:"sourceApiVersion"`
	}
	readJSONFile(t, filepath.Join(root, "sfdx-project.json"), &project)
	if project.SourceAPIVersion == "" {
		t.Fatal("sfdx-project.json omitted sourceApiVersion")
	}
	if len(project.PackageDirectories) != 1 || project.PackageDirectories[0].Path != "force-app" || !project.PackageDirectories[0].Default {
		t.Fatalf("packageDirectories = %#v, want default force-app", project.PackageDirectories)
	}

	pages, err := filepath.Glob(filepath.Join(root, "force-app", "main", "default", "pages", "*.page"))
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 50 {
		t.Fatalf("page count = %d, want 50", len(pages))
	}
	pageNames := make(map[string]struct{}, len(pages))
	for _, page := range pages {
		name := strings.TrimSuffix(filepath.Base(page), ".page")
		pageNames[name] = struct{}{}
		requireFile(t, root, "force-app", "main", "default", "pages", name+".page-meta.xml")
	}

	var index struct {
		Summary struct {
			PageCount  int      `json:"pageCount"`
			GroupCount int      `json:"groupCount"`
			Owners     []string `json:"owners"`
			Categories []string `json:"categories"`
		} `json:"summary"`
		Pages []struct {
			Name       string   `json:"name"`
			Group      string   `json:"group"`
			Owner      string   `json:"owner"`
			Category   string   `json:"category"`
			Components []string `json:"components"`
		} `json:"pages"`
		Groups []struct {
			Name     string   `json:"name"`
			Owner    string   `json:"owner"`
			Category string   `json:"category"`
			Pages    []string `json:"pages"`
		} `json:"groups"`
	}
	readJSONFile(t, filepath.Join(root, "visualforce-probe-index.json"), &index)
	if len(index.Pages) != 50 {
		t.Fatalf("index page count = %d, want 50", len(index.Pages))
	}

	wantGroups := []string{
		"lifecycle",
		"fields",
		"tables",
		"AJAX",
		"templates",
		"custom components",
		"static resources",
		"remoting",
		"remote objects",
		"upload",
		"flow",
		"Lightning Out",
		"PDF",
		"standard controller",
		"standard set controller",
		"expressions/globals",
		"security/errors",
	}
	if index.Summary.PageCount != 50 || index.Summary.GroupCount != len(wantGroups) {
		t.Fatalf("summary = %#v, want 50 pages and %d groups", index.Summary, len(wantGroups))
	}
	if !slices.Contains(index.Summary.Owners, "oracle/corpus") {
		t.Fatalf("summary owners = %#v", index.Summary.Owners)
	}
	for _, category := range []string{"phase1", "broad-corpus"} {
		if !slices.Contains(index.Summary.Categories, category) {
			t.Fatalf("summary categories omitted %q: %#v", category, index.Summary.Categories)
		}
	}
	gotGroups := make(map[string][]string, len(index.Groups))
	for _, group := range index.Groups {
		gotGroups[group.Name] = group.Pages
	}
	for _, group := range wantGroups {
		names, ok := gotGroups[group]
		if !ok {
			t.Fatalf("index omitted group %q", group)
		}
		if len(names) == 0 {
			t.Fatalf("index group %q has no pages", group)
		}
	}
	wantPhase1Groups := []string{
		"lifecycle",
		"fields",
		"tables",
		"AJAX",
		"templates",
		"custom components",
		"static resources",
		"remoting",
		"remote objects",
		"upload",
		"flow",
		"Lightning Out",
		"PDF",
		"standard controller",
		"standard set controller",
	}
	groupCategory := make(map[string]string, len(index.Groups))
	for _, group := range index.Groups {
		if group.Owner != "oracle/corpus" {
			t.Fatalf("group %q owner = %q", group.Name, group.Owner)
		}
		if group.Category == "" {
			t.Fatalf("group %q omitted category", group.Name)
		}
		groupCategory[group.Name] = group.Category
	}
	for _, group := range wantPhase1Groups {
		if got := groupCategory[group]; got != "phase1" {
			t.Fatalf("group %q category = %q, want phase1", group, got)
		}
	}

	for _, entry := range index.Pages {
		if _, ok := pageNames[entry.Name]; !ok {
			t.Fatalf("index page %q has no .page file", entry.Name)
		}
		if !slices.Contains(wantGroups, entry.Group) {
			t.Fatalf("index page %q uses unknown group %q", entry.Name, entry.Group)
		}
		if entry.Owner != "oracle/corpus" {
			t.Fatalf("index page %q owner = %q", entry.Name, entry.Owner)
		}
		if entry.Category == "" {
			t.Fatalf("index page %q omitted category", entry.Name)
		}
		if entry.Category != groupCategory[entry.Group] {
			t.Fatalf("index page %q category = %q, want group category %q", entry.Name, entry.Category, groupCategory[entry.Group])
		}
		if !slices.Contains(gotGroups[entry.Group], entry.Name) {
			t.Fatalf("index group %q does not list page %q", entry.Group, entry.Name)
		}
	}
	for page := range pageNames {
		found := false
		for _, entry := range index.Pages {
			if entry.Name == page {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("page file %q missing from index", page)
		}
	}
	pageComponents := make(map[string][]string, len(index.Pages))
	for _, entry := range index.Pages {
		pageComponents[entry.Name] = entry.Components
	}
	componentExpectations := map[string][]string{
		"ProbeFlowInterview":       {"apex:outputLink"},
		"ProbeFlowFinishLocation":  {"apex:outputLink"},
		"ProbeLightningOutInclude": {"apex:includeLightning"},
		"ProbeLightningOutCreate":  {"apex:includeLightning", "$Lightning.use", "$Lightning.createComponent", "lwc:probeLwc"},
	}
	for page, want := range componentExpectations {
		if got := pageComponents[page]; !slices.Equal(got, want) {
			t.Fatalf("index page %q components = %#v, want %#v", page, got, want)
		}
	}

	var scoreboard struct {
		Kind               string `json:"kind"`
		SourceIndex        string `json:"sourceIndex"`
		LatestOracleResult struct {
			Status             string `json:"status"`
			TargetOrg          string `json:"targetOrg"`
			PageCountCompared  int    `json:"pageCountCompared"`
			MissingPageCount   int    `json:"missingPageCount"`
			DifferingPageCount int    `json:"differingPageCount"`
			DiffCount          int    `json:"diffCount"`
		} `json:"latestOracleResult"`
		Scoreboard struct {
			Owners []struct {
				Name         string `json:"name"`
				PageCount    int    `json:"pageCount"`
				PassCount    int    `json:"passCount"`
				FailCount    int    `json:"failCount"`
				MissingCount int    `json:"missingCount"`
				DiffCount    int    `json:"diffCount"`
			} `json:"owners"`
			Categories []struct {
				Name         string `json:"name"`
				PageCount    int    `json:"pageCount"`
				PassCount    int    `json:"passCount"`
				FailCount    int    `json:"failCount"`
				MissingCount int    `json:"missingCount"`
				DiffCount    int    `json:"diffCount"`
			} `json:"categories"`
			Groups []struct {
				Name         string `json:"name"`
				PageCount    int    `json:"pageCount"`
				PassCount    int    `json:"passCount"`
				FailCount    int    `json:"failCount"`
				MissingCount int    `json:"missingCount"`
				DiffCount    int    `json:"diffCount"`
			} `json:"groups"`
		} `json:"scoreboard"`
	}
	readJSONFile(t, filepath.Join("..", "..", "docs", "fixtures", "visualforce", "oracle-scoreboard-latest.json"), &scoreboard)
	if scoreboard.Kind != "visualforce-oracle-scoreboard" || scoreboard.SourceIndex == "" {
		t.Fatalf("scoreboard header = %#v", scoreboard)
	}
	if scoreboard.LatestOracleResult.Status != "pass" || scoreboard.LatestOracleResult.TargetOrg != "oaer-probe-max" {
		t.Fatalf("latest oracle result = %#v", scoreboard.LatestOracleResult)
	}
	if scoreboard.LatestOracleResult.PageCountCompared != len(index.Pages) || scoreboard.LatestOracleResult.MissingPageCount != 0 || scoreboard.LatestOracleResult.DifferingPageCount != 0 || scoreboard.LatestOracleResult.DiffCount != 0 {
		t.Fatalf("latest oracle result = %#v, index pages = %d", scoreboard.LatestOracleResult, len(index.Pages))
	}
	if len(scoreboard.Scoreboard.Owners) != 1 || scoreboard.Scoreboard.Owners[0].Name != "oracle/corpus" || scoreboard.Scoreboard.Owners[0].PageCount != len(index.Pages) || scoreboard.Scoreboard.Owners[0].PassCount != len(index.Pages) || scoreboard.Scoreboard.Owners[0].FailCount != 0 || scoreboard.Scoreboard.Owners[0].MissingCount != 0 || scoreboard.Scoreboard.Owners[0].DiffCount != 0 {
		t.Fatalf("scoreboard owners = %#v", scoreboard.Scoreboard.Owners)
	}
	if len(scoreboard.Scoreboard.Groups) != len(index.Groups) {
		t.Fatalf("scoreboard groups = %d, want %d", len(scoreboard.Scoreboard.Groups), len(index.Groups))
	}
}

func requireFile(t *testing.T, root string, elems ...string) {
	t.Helper()
	path := filepath.Join(append([]string{root}, elems...)...)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.IsDir() {
		t.Fatalf("%s is a directory, want file", path)
	}
}

func readJSONFile(t *testing.T, path string, v any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}
