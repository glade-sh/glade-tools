package compat

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanLwcCorpusSummarizesPackageFirstRepos(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo-a")
	writeLwcCorpusTestFile(t, filepath.Join(repo, "sfdx-project.json"), `{
  "packageDirectories": [
    {"path": "packages/core", "package": "Core UI", "default": true},
    {"path": "packages/empty", "package": "Empty UI"}
  ]
}`)
	bundle := filepath.Join(repo, "packages", "core", "main", "default", "lwc", "trailCard")
	writeLwcCorpusTestFile(t, filepath.Join(bundle, "trailCard.js"), `
import { LightningElement, api } from 'lwc';
import navigate from 'lightning/navigation';
import loadTrail from '@salesforce/apex/TrailController.loadTrail';
import helper from 'c/helper';
export default class TrailCard extends LightningElement {}
`)
	writeLwcCorpusTestFile(t, filepath.Join(bundle, "trailCard.html"), `
<template>
  <lightning-card>
    <lightning-button label="Open"></lightning-button>
    <trail-map></trail-map>
  </lightning-card>
</template>
`)
	writeLwcCorpusTestFile(t, filepath.Join(bundle, "trailCard.js-meta.xml"), `
<LightningComponentBundle>
  <isExposed>true</isExposed>
  <targets>
    <target>lightning__RecordPage</target>
    <target>lightning__AppPage</target>
  </targets>
  <targetConfigs>
    <targetConfig targets="lightning__RecordPage">
      <property name="recordId" type="String"/>
      <property name="showMap" type="Boolean"/>
      <example>trailCardRecordPage</example>
    </targetConfig>
  </targetConfigs>
</LightningComponentBundle>
`)
	writeLwcCorpusTestFile(t, filepath.Join(root, "repo-b", "force-app", "main", "default", "lwc", "ignored", "ignored.js"), "import x from 'lightning/toast';")

	report, err := ScanLwcCorpus(LwcCorpusScanOptions{
		Root:         root,
		IncludeRepos: []string{"repo-a", "repo-empty"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Command != "glade compat lwc corpus" || report.Root != root {
		t.Fatalf("report identity = %#v", report)
	}
	if report.Counts.Meta != 1 || report.Counts.JS != 1 || report.Counts.HTML != 1 || report.Counts.Bundles != 1 {
		t.Fatalf("counts = %#v", report.Counts)
	}
	assertLwcCorpusCount(t, report.Targets, "lightning__RecordPage", 1)
	assertLwcCorpusCount(t, report.Targets, "lightning__AppPage", 1)
	assertLwcCorpusCount(t, report.Imports, "lightning/navigation", 1)
	assertLwcCorpusCount(t, report.Imports, "@salesforce/apex/TrailController.loadTrail", 1)
	assertLwcCorpusCount(t, report.Imports, "c/helper", 1)
	assertLwcCorpusCount(t, report.LightningTags, "lightning-card", 1)
	assertLwcCorpusCount(t, report.LightningTags, "lightning-button", 1)
	assertLwcCorpusCount(t, report.PropertyTypes, "String", 1)
	assertLwcCorpusCount(t, report.PropertyTypes, "Boolean", 1)
	assertLwcCorpusCount(t, report.UnsupportedTags, "trail-map", 1)
	assertLwcCorpusCount(t, report.Examples, "trailCardRecordPage", 1)

	if len(report.Repositories) != 2 {
		t.Fatalf("repositories = %#v", report.Repositories)
	}
	if report.Repositories[0].Name != "repo-a" || report.Repositories[0].Counts.Bundles != 1 {
		t.Fatalf("repo-a summary = %#v", report.Repositories[0])
	}
	if report.Repositories[0].Counts.Imports != 4 || report.Repositories[0].Counts.LightningTags != 2 || report.Repositories[0].Counts.Targets != 2 || report.Repositories[0].Counts.PropertyTypes != 2 || report.Repositories[0].Counts.UnsupportedTags != 1 || report.Repositories[0].Counts.Examples != 1 {
		t.Fatalf("repo-a detailed counts = %#v", report.Repositories[0].Counts)
	}
	if report.Repositories[1].Name != "repo-empty" || report.Repositories[1].Counts.Bundles != 0 {
		t.Fatalf("repo-empty summary = %#v", report.Repositories[1])
	}
	if len(report.Packages) != 2 {
		t.Fatalf("packages = %#v", report.Packages)
	}
	if report.Packages[0].Repository != "repo-a" || report.Packages[0].Name != "Core UI" || report.Packages[0].Counts.Bundles != 1 {
		t.Fatalf("core package = %#v", report.Packages[0])
	}
	if report.Packages[0].Counts.Imports != 4 || report.Packages[0].Counts.LightningTags != 2 || report.Packages[0].Counts.Targets != 2 || report.Packages[0].Counts.PropertyTypes != 2 || report.Packages[0].Counts.UnsupportedTags != 1 || report.Packages[0].Counts.Examples != 1 {
		t.Fatalf("core package detailed counts = %#v", report.Packages[0].Counts)
	}
	if report.Packages[1].Repository != "repo-a" || report.Packages[1].Name != "Empty UI" || report.Packages[1].Counts.Bundles != 0 {
		t.Fatalf("empty package = %#v", report.Packages[1])
	}
}

func TestScanLwcCorpusUsesRootAsRepoWhenRootIsAProject(t *testing.T) {
	root := t.TempDir()
	writeLwcCorpusTestFile(t, filepath.Join(root, "sfdx-project.json"), `{"packageDirectories":[{"path":"force-app","default":true}]}`)
	writeLwcCorpusTestFile(t, filepath.Join(root, "force-app", "main", "default", "lwc", "solo", "solo.js"), "import { LightningElement } from 'lwc';")

	report, err := ScanLwcCorpus(LwcCorpusScanOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Repositories) != 1 || report.Repositories[0].Name != filepath.Base(root) || report.Repositories[0].Counts.JS != 1 {
		t.Fatalf("repositories = %#v", report.Repositories)
	}
	if len(report.Packages) != 1 || report.Packages[0].Path != "force-app" || report.Packages[0].Counts.JS != 1 {
		t.Fatalf("packages = %#v", report.Packages)
	}
}

func assertLwcCorpusCount(t *testing.T, rows []LwcCorpusCountRow, name string, count int) {
	t.Helper()
	for _, row := range rows {
		if row.Name == name {
			if row.Count != count {
				t.Fatalf("%s count = %d, want %d", name, row.Count, count)
			}
			return
		}
	}
	t.Fatalf("missing %s in %#v", name, rows)
}

func writeLwcCorpusTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
