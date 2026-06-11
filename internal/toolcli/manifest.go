package toolcli

import (
	"encoding/json"
	"io"
)

const (
	pluginAPIVersion = "glade.plugin.v1"
	pluginSource     = "github.com/glade-sh/glade/tools"
)

var pluginVersion = "0.1.0"

type pluginManifest struct {
	APIVersion          string                  `json:"apiVersion"`
	Name                string                  `json:"name"`
	Version             string                  `json:"version"`
	Summary             string                  `json:"summary"`
	Commands            []pluginCommandManifest `json:"commands"`
	MinimumGladeVersion string                  `json:"minimumGladeVersion"`
	Source              string                  `json:"source"`
}

type pluginCommandManifest struct {
	Path    []string `json:"path"`
	Summary string   `json:"summary"`
}

func writeCompatManifest(w io.Writer) error {
	manifest := pluginManifest{
		APIVersion: pluginAPIVersion,
		Name:       "compat",
		Version:    pluginVersion,
		Summary:    "Compatibility fixtures, surface ledgers, and maintenance scanners.",
		Commands: []pluginCommandManifest{
			{Path: []string{"compat"}, Summary: "Run compatibility fixture and report commands."},
			{Path: []string{"surface"}, Summary: "Refresh and inspect the Salesforce surface ledger."},
			{Path: []string{"local-tests"}, Summary: "Report local Apex test execution readiness."},
			{Path: []string{"post-parity"}, Summary: "Scan a project for unsupported surfaces."},
			{Path: []string{"examples"}, Summary: "Scan example projects and report support status."},
			{Path: []string{"dashboard"}, Summary: "Generate compatibility dashboard output."},
			{Path: []string{"gaps"}, Summary: "Generate known-gaps output."},
			{Path: []string{"stdlib"}, Summary: "Generate standard-library coverage output."},
		},
		MinimumGladeVersion: "0.1.0",
		Source:              pluginSource,
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(manifest)
}
