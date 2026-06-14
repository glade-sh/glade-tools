package perftool

import (
	"encoding/json"
	"io"
)

type pluginManifest struct {
	APIVersion          string                  `json:"apiVersion"`
	Name                string                  `json:"name"`
	Version             string                  `json:"version"`
	Summary             string                  `json:"summary"`
	Commands            []pluginCommandManifest `json:"commands"`
	Editor              pluginEditorManifest    `json:"editor,omitempty"`
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
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	View     string   `json:"view"`
	Contexts []string `json:"contexts,omitempty"`
	Command  []string `json:"command"`
	Args     []string `json:"args,omitempty"`
	Output   string   `json:"output"`
	Icon     string   `json:"icon,omitempty"`
}

var pluginVersion = "0.1.0"

func writeManifest(w io.Writer) error {
	manifest := pluginManifest{
		APIVersion: "glade.plugin.v1",
		Name:       "performance",
		Version:    pluginVersion,
		Summary:    "Trace-aware Salesforce transaction performance analyzer.",
		Commands: []pluginCommandManifest{
			{Path: []string{"performance"}, Summary: "Correlate Apex, metadata, trace, and org-shape facts for performance risks."},
		},
		Editor: pluginEditorManifest{
			Actions: []pluginEditorActionManifest{
				{
					ID:       "performance.scanProject",
					Title:    "Scan Performance Risks",
					View:     "startHere",
					Contexts: []string{"project"},
					Command:  []string{"performance"},
					Args:     []string{"--project", "${projectRoot}", "--json", "--editor-findings"},
					Output:   "glade.findings.v1",
					Icon:     "pulse",
				},
			},
		},
		MinimumGladeVersion: "0.1.0",
		Source:              "github.com/glade-sh/glade/tools",
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(manifest)
}
