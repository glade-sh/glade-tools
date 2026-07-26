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
	Editor              *pluginEditorManifest   `json:"editor,omitempty"`
	MinimumGladeVersion string                  `json:"minimumGladeVersion"`
	Source              string                  `json:"source"`
}

type pluginCommandManifest struct {
	Path    []string `json:"path"`
	Summary string   `json:"summary"`
}

type pluginEditorManifest struct {
	Actions []pluginEditorActionManifest `json:"actions,omitempty"`
}

type pluginEditorActionManifest struct {
	ID       string                      `json:"id"`
	Title    string                      `json:"title"`
	View     string                      `json:"view"`
	Contexts []string                    `json:"contexts,omitempty"`
	Command  []string                    `json:"command"`
	Args     []string                    `json:"args,omitempty"`
	Inputs   []pluginEditorInputManifest `json:"inputs,omitempty"`
	Output   string                      `json:"output"`
	Icon     string                      `json:"icon,omitempty"`
}

type pluginEditorInputManifest struct {
	Name     string `json:"name"`
	Label    string `json:"label"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
}

func writeCompatManifest(w io.Writer) error {
	manifest := pluginManifest{
		APIVersion: pluginAPIVersion,
		Name:       "compat",
		Version:    pluginVersion,
		Summary:    "Compatibility fixtures, surface ledgers, and maintenance scanners.",
		Commands: []pluginCommandManifest{
			{Path: []string{"compat"}, Summary: "Run compatibility fixture and report commands."},
			{Path: []string{"compat", "lwc"}, Summary: "Capture LWC shell evidence and native API parity."},
			{Path: []string{"compat", "lwc", "capture"}, Summary: "Write LWC target, browser, and support evidence."},
			{Path: []string{"compat", "lwc", "corpus"}, Summary: "Scan package-first LWC corpus support gaps."},
			{Path: []string{"compat", "lwc", "parity"}, Summary: "Generate the Native LWC API parity ledger."},
			{Path: []string{"surface"}, Summary: "Refresh and inspect the Salesforce surface ledger."},
			{Path: []string{"corpus"}, Summary: "Run Glade over public corpora and classify diagnostics."},
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
			{Path: []string{"declaration-contracts"}, Summary: "Export docs declaration shapes for generated stubs."},
			{Path: []string{"salesforce-coverage"}, Summary: "Generate Salesforce coverage manifests."},
			{Path: []string{"standard-objects"}, Summary: "Report generated standard object coverage."},
			{Path: []string{"stub-contracts"}, Summary: "Report generated stub behavioral contract policy."},
			{Path: []string{"stub-behavior"}, Summary: "Report generated platform stub behavior status."},
			{Path: []string{"stub-inventory"}, Summary: "Compare stub source with generated shapes."},
			{Path: []string{"product-namespaces"}, Summary: "Report product namespace coverage."},
			{Path: []string{"tooling-fixtures"}, Summary: "Summarize tooling fixture reports."},
			{Path: []string{"evidence"}, Summary: "Compare fixture evidence with a catalog."},
			{Path: []string{"oracle-stdlib"}, Summary: "Run scratch-org standard-library oracle probes."},
			{Path: []string{"apex-rules"}, Summary: "Compare checked Apex language-rule probes with Salesforce."},
		},
		Editor: &pluginEditorManifest{
			Actions: []pluginEditorActionManifest{
				{
					ID:       "compat.postParity",
					Title:    "Scan Unsupported Local Surfaces",
					View:     "runs",
					Contexts: []string{"project"},
					Command:  []string{"post-parity"},
					Args:     []string{"--project", "${projectRoot}", "--json", "--editor-findings"},
					Output:   "glade.findings.v1",
					Icon:     "search",
				},
				{
					ID:       "compat.visualforceLocalCapture",
					Title:    "Capture Local Visualforce Evidence",
					View:     "preview",
					Contexts: []string{"project", "vfServerRunning"},
					Command:  []string{"visualforce", "capture"},
					Args:     []string{"--local", "--glade-bin", "glade", "--project", "${projectRoot}", "--out", "${outputDir}/visualforce-local.json", "--json", "--editor-findings"},
					Output:   "glade.findings.v1",
					Icon:     "record",
				},
				{
					ID:       "compat.lwcCapture",
					Title:    "Capture LWC Browser Evidence",
					View:     "preview",
					Contexts: []string{"project", "lwcServerRunning"},
					Command:  []string{"compat", "lwc", "capture"},
					Inputs: []pluginEditorInputManifest{
						{Name: "targetOrg", Label: "Target org alias", Type: "text", Required: true},
						{Name: "localBaseUrl", Label: "Local LWC shell URL", Type: "text", Required: true},
					},
					Args:   []string{"--target-org", "${input.targetOrg}", "--project", "${projectRoot}", "--local-browser-capture", "--local-base-url", "${input.localBaseUrl}", "--browser-capture", "--out", "${outputDir}/lwc-browser-capture.json", "--json", "--editor-findings"},
					Output: "glade.findings.v1",
					Icon:   "cloud-download",
				},
			},
		},
		MinimumGladeVersion: "0.1.0",
		Source:              pluginSource,
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(manifest)
}

func writeOrgPackageManifest(w io.Writer) error {
	manifest := pluginManifest{
		APIVersion: pluginAPIVersion,
		Name:       "orgpackage",
		Version:    pluginVersion,
		Summary:    "Capture installed Salesforce package artifacts from an org.",
		Commands: []pluginCommandManifest{
			{Path: []string{"orgpackage"}, Summary: "Capture and inspect org-backed package artifacts."},
			{Path: []string{"orgpackage", "capture"}, Summary: "Capture an installed package artifact from a Salesforce org."},
		},
		MinimumGladeVersion: "0.1.0",
		Source:              pluginSource,
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(manifest)
}
