package orgpackage

import (
	"encoding/json"
	"fmt"
	"io"
)

type Summary struct {
	Namespace        string   `json:"namespace"`
	PackageName      string   `json:"packageName,omitempty"`
	Version          string   `json:"version,omitempty"`
	ApexClasses      int      `json:"apexClasses"`
	Objects          int      `json:"objects"`
	Labels           int      `json:"labels"`
	StaticResources  int      `json:"staticResources"`
	LightningBundles int      `json:"lightningBundles"`
	Warnings         []string `json:"warnings,omitempty"`
}

func Summarize(capture Capture, warnings []string) Summary {
	return Summary{
		Namespace:        capture.Package.Namespace,
		PackageName:      capture.Package.Name,
		Version:          capture.Package.Version,
		ApexClasses:      len(capture.ApexClasses),
		Objects:          len(capture.Objects),
		Labels:           len(capture.Labels),
		StaticResources:  len(capture.StaticResources),
		LightningBundles: len(capture.LightningBundles),
		Warnings:         append([]string(nil), warnings...),
	}
}

func WriteSummary(w io.Writer, summary Summary, jsonOutput bool) error {
	if jsonOutput {
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(summary)
	}
	_, err := fmt.Fprintf(w, "%s %s: %d Apex classes, %d objects, %d labels, %d static resources, %d Lightning bundles\n", summary.Namespace, summary.Version, summary.ApexClasses, summary.Objects, summary.Labels, summary.StaticResources, summary.LightningBundles)
	return err
}
