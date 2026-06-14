package perfscan

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

func WriteJSON(w io.Writer, report Report) error {
	report.Finalize()
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

func WriteMarkdown(w io.Writer, report Report) error {
	report.Finalize()
	if _, err := fmt.Fprintf(w, "# Performance Scan\n\nProject: `%s`\n\nFindings: %d\n\nHigh: %d\n\nMedium: %d\n\nLow: %d\n\n", report.Project, report.Summary.Findings, report.Summary.High, report.Summary.Medium, report.Summary.Low); err != nil {
		return err
	}
	if len(report.Findings) == 0 {
		_, err := fmt.Fprintln(w, "No performance findings.")
		return err
	}
	if _, err := fmt.Fprintln(w, "## Findings"); err != nil {
		return err
	}
	for i, finding := range report.Findings {
		location := finding.Location.File
		if finding.Location.Line > 0 {
			location = fmt.Sprintf("%s:%d", filepath.Base(finding.Location.File), finding.Location.Line)
		}
		if _, err := fmt.Fprintf(w, "%d. `%s` [%s/%s] score=%d\n\n", i+1, finding.ID, finding.Severity, finding.Confidence, finding.Score); err != nil {
			return err
		}
		if finding.EntryPoint.Name != "" {
			if _, err := fmt.Fprintf(w, "   Entry point: `%s` `%s`\n\n", finding.EntryPoint.Kind, finding.EntryPoint.Name); err != nil {
				return err
			}
		}
		if location != "" {
			if _, err := fmt.Fprintf(w, "   Location: `%s`\n\n", location); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(w, "   %s\n\n", finding.Message); err != nil {
			return err
		}
		if len(finding.Path) > 0 {
			if _, err := fmt.Fprintf(w, "   Path: %s\n\n", formatPath(finding.Path)); err != nil {
				return err
			}
		}
		if len(finding.NamespacePath) > 0 {
			if _, err := fmt.Fprintf(w, "   Namespace path: %s\n\n", strings.Join(finding.NamespacePath, " -> ")); err != nil {
				return err
			}
		}
		if finding.Multiplicity != "" {
			if _, err := fmt.Fprintf(w, "   Multiplicity: %s\n\n", finding.Multiplicity); err != nil {
				return err
			}
		}
		if risk := formatResourceRisk(finding.ResourceRisk); risk != "" {
			if _, err := fmt.Fprintf(w, "   Resource risk: %s\n\n", risk); err != nil {
				return err
			}
		}
		for _, evidence := range finding.Evidence {
			if _, err := fmt.Fprintf(w, "   Evidence: %s", evidence.Message); err != nil {
				return err
			}
			if evidence.Value != "" {
				if _, err := fmt.Fprintf(w, " `%s`", evidence.Value); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprintln(w); err != nil {
				return err
			}
		}
		if finding.Fix != "" {
			if _, err := fmt.Fprintf(w, "\n   Fix: %s\n\n", finding.Fix); err != nil {
				return err
			}
		}
		if finding.Acceptance != "" {
			if _, err := fmt.Fprintf(w, "   Acceptance: %s\n\n", finding.Acceptance); err != nil {
				return err
			}
		}
	}
	return nil
}

func formatPath(path []PathStep) string {
	parts := make([]string, 0, len(path))
	for _, step := range path {
		switch {
		case step.Kind != "" && step.Name != "":
			parts = append(parts, step.Kind+" "+step.Name)
		case step.Kind != "":
			parts = append(parts, step.Kind)
		case step.Name != "":
			parts = append(parts, step.Name)
		}
	}
	return strings.Join(parts, " -> ")
}

func formatResourceRisk(risk ResourceRisk) string {
	parts := make([]string, 0, 6)
	if risk.CPU {
		parts = append(parts, "CPU")
	}
	if risk.Heap {
		parts = append(parts, "heap")
	}
	if risk.DBTime {
		parts = append(parts, "DB time")
	}
	if risk.DBRows {
		parts = append(parts, "DB rows")
	}
	if risk.Locks {
		parts = append(parts, "locks")
	}
	if risk.SharedLimit {
		parts = append(parts, "shared limits")
	}
	return strings.Join(parts, ", ")
}
