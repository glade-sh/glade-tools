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
	MinimumGladeVersion string                  `json:"minimumGladeVersion"`
	Source              string                  `json:"source"`
}

type pluginCommandManifest struct {
	Path    []string `json:"path"`
	Summary string   `json:"summary"`
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
		MinimumGladeVersion: "0.1.0",
		Source:              "github.com/glade-sh/glade/tools",
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(manifest)
}
