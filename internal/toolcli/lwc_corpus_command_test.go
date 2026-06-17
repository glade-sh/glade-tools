package toolcli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunCompatLWCCorpusWritesJSONToStdoutAndOut(t *testing.T) {
	root := t.TempDir()
	writeLWCCorpusCommandTestFile(t, filepath.Join(root, "repo-a", "sfdx-project.json"), `{"packageDirectories":[{"path":"force-app","package":"Main"}]}`)
	writeLWCCorpusCommandTestFile(t, filepath.Join(root, "repo-a", "force-app", "main", "default", "lwc", "statusPanel", "statusPanel.js"), `import { LightningElement } from 'lwc';`)
	writeLWCCorpusCommandTestFile(t, filepath.Join(root, "repo-a", "force-app", "main", "default", "lwc", "statusPanel", "statusPanel.html"), `<template><lightning-badge label="Ready"></lightning-badge></template>`)
	writeLWCCorpusCommandTestFile(t, filepath.Join(root, "repo-a", "force-app", "main", "default", "lwc", "statusPanel", "statusPanel.js-meta.xml"), `<LightningComponentBundle><targets><target>lightning__HomePage</target></targets></LightningComponentBundle>`)
	outPath := filepath.Join(root, "reports", "lwc-corpus.json")

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{
		"compat", "lwc", "corpus",
		"--root", root,
		"--out", outPath,
		"--include-repos", "repo-a,repo-empty",
		"--json",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run returned %d, stderr=%s", code, stderr.String())
	}
	var payload struct {
		Command string `json:"command"`
		Counts  struct {
			Meta    int `json:"meta"`
			JS      int `json:"js"`
			HTML    int `json:"html"`
			Bundles int `json:"bundles"`
		} `json:"counts"`
		Repositories []struct {
			Name   string `json:"name"`
			Counts struct {
				Bundles int `json:"bundles"`
			} `json:"counts"`
		} `json:"repositories"`
		Packages []struct {
			Repository string `json:"repository"`
			Name       string `json:"name"`
			Counts     struct {
				Bundles int `json:"bundles"`
			} `json:"counts"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout.String())
	}
	if payload.Command != "glade compat lwc corpus" || payload.Counts.Meta != 1 || payload.Counts.JS != 1 || payload.Counts.HTML != 1 || payload.Counts.Bundles != 1 {
		t.Fatalf("payload = %#v", payload)
	}
	if len(payload.Repositories) != 2 || payload.Repositories[1].Name != "repo-empty" || payload.Repositories[1].Counts.Bundles != 0 {
		t.Fatalf("repositories = %#v", payload.Repositories)
	}
	if len(payload.Packages) != 1 || payload.Packages[0].Repository != "repo-a" || payload.Packages[0].Name != "Main" || payload.Packages[0].Counts.Bundles != 1 {
		t.Fatalf("packages = %#v", payload.Packages)
	}
	outData, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.TrimSpace(stdout.Bytes()), bytes.TrimSpace(outData)) {
		t.Fatalf("out file differs from stdout\nstdout=%s\nout=%s", stdout.String(), string(outData))
	}
}

func TestLWCHelpListsCorpusCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"lwc", "--help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run returned %d, stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"lwc corpus --root <path> --out <path> --json",
		"--include-repos <a,b>",
		"LWC corpus report",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("help omitted %q:\n%s", want, out)
		}
	}
}

func TestManifestListsLWCCorpusCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"manifest", "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run returned %d, stderr=%s", code, stderr.String())
	}
	var manifest pluginManifestFile
	if err := json.Unmarshal(stdout.Bytes(), &manifest); err != nil {
		t.Fatalf("manifest is not JSON: %v\n%s", err, stdout.String())
	}
	summaries := packagedCommandSummaryByPath(manifest.Commands)
	if got := summaries["compat lwc corpus"]; got != "Scan package-first LWC corpus support gaps." {
		t.Fatalf("compat/lwc/corpus summary = %q", got)
	}
}

func writeLWCCorpusCommandTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
