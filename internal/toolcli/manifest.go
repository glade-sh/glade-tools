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
			{Path: []string{"matrix"}, Summary: "Print the full capability matrix."},
			{Path: []string{"mvp"}, Summary: "Print MVP readiness status."},
			{Path: []string{"local-tests"}, Summary: "Report local Apex test execution readiness."},
			{Path: []string{"post-parity"}, Summary: "Scan a project for unsupported surfaces."},
			{Path: []string{"examples"}, Summary: "Scan example projects and report support status."},
			{Path: []string{"replay"}, Summary: "Replay checked run bundles."},
			{Path: []string{"ui-controllers"}, Summary: "Discover Visualforce controller surfaces."},
			{Path: []string{"server-examples"}, Summary: "Probe checked server route examples."},
			{Path: []string{"visualforce"}, Summary: "Capture and score Visualforce rendering evidence."},
			{Path: []string{"dashboard"}, Summary: "Generate compatibility dashboard output."},
			{Path: []string{"gaps"}, Summary: "Generate known-gaps output."},
			{Path: []string{"stdlib"}, Summary: "Generate standard-library coverage output."},
			{Path: []string{"docs-inventory"}, Summary: "Inventory Salesforce docs."},
			{Path: []string{"catalog"}, Summary: "Build a capability catalog."},
			{Path: []string{"reconcile"}, Summary: "Reconcile docs inventory with the catalog."},
			{Path: []string{"doc-contracts"}, Summary: "Report Salesforce docs behavior contracts."},
			{Path: []string{"salesforce-coverage"}, Summary: "Generate Salesforce coverage manifests."},
			{Path: []string{"standard-objects"}, Summary: "Report generated standard object coverage."},
			{Path: []string{"stub-contracts"}, Summary: "Report generated stub behavioral contract policy."},
			{Path: []string{"stub-behavior"}, Summary: "Report generated platform stub behavior status."},
			{Path: []string{"stub-inventory"}, Summary: "Compare stub source with generated shapes."},
			{Path: []string{"product-namespaces"}, Summary: "Report product namespace coverage."},
			{Path: []string{"tooling-fixtures"}, Summary: "Summarize tooling fixture reports."},
			{Path: []string{"evidence"}, Summary: "Compare fixture evidence with a catalog."},
			{Path: []string{"oracle-stdlib"}, Summary: "Run scratch-org standard-library oracle probes."},
		},
		MinimumGladeVersion: "0.1.0",
		Source:              pluginSource,
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(manifest)
}
