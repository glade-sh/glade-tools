package perfscan

import (
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
)

type sarifLog struct {
	Version string     `json:"version"`
	Schema  string     `json:"$schema"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name  string      `json:"name"`
	Rules []sarifRule `json:"rules,omitempty"`
}

type sarifRule struct {
	ID        string       `json:"id"`
	Name      string       `json:"name,omitempty"`
	ShortDesc sarifMessage `json:"shortDescription,omitempty"`
}

type sarifResult struct {
	RuleID    string          `json:"ruleId,omitempty"`
	Level     string          `json:"level"`
	Message   sarifMessage    `json:"message"`
	Locations []sarifLocation `json:"locations,omitempty"`
}

type sarifMessage struct {
	Text string `json:"text"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysicalLocation `json:"physicalLocation"`
}

type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifactLocation `json:"artifactLocation,omitempty"`
	Region           sarifRegion           `json:"region,omitempty"`
}

type sarifArtifactLocation struct {
	URI string `json:"uri"`
}

type sarifRegion struct {
	StartLine   int `json:"startLine,omitempty"`
	StartColumn int `json:"startColumn,omitempty"`
}

func WriteSARIF(w io.Writer, report Report) error {
	report.Finalize()
	rules := make([]sarifRule, 0)
	seenRules := make(map[string]bool)
	results := make([]sarifResult, 0, len(report.Findings))
	for _, finding := range report.Findings {
		if finding.ID != "" && !seenRules[finding.ID] {
			seenRules[finding.ID] = true
			rules = append(rules, sarifRule{
				ID:        finding.ID,
				Name:      finding.ID,
				ShortDesc: sarifMessage{Text: finding.Message},
			})
		}
		result := sarifResult{
			RuleID:  finding.ID,
			Level:   sarifLevel(finding.Severity),
			Message: sarifMessage{Text: finding.Message},
		}
		if finding.Location.File != "" {
			location := sarifLocation{PhysicalLocation: sarifPhysicalLocation{
				ArtifactLocation: sarifArtifactLocation{URI: findingSARIFURI(report.Project, finding.Location.File)},
			}}
			if finding.Location.Line > 0 {
				location.PhysicalLocation.Region = sarifRegion{
					StartLine:   finding.Location.Line,
					StartColumn: finding.Location.Column,
				}
			}
			result.Locations = []sarifLocation{location}
		}
		results = append(results, result)
	}
	log := sarifLog{
		Version: "2.1.0",
		Schema:  "https://json.schemastore.org/sarif-2.1.0.json",
		Runs: []sarifRun{{
			Tool:    sarifTool{Driver: sarifDriver{Name: "glade performance", Rules: rules}},
			Results: results,
		}},
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(log)
}

func sarifLevel(severity Severity) string {
	switch severity {
	case SeverityHigh:
		return "error"
	case SeverityMedium:
		return "warning"
	default:
		return "note"
	}
}

func filepathToURI(path string) string {
	return strings.ReplaceAll(path, `\`, `/`)
}

func findingSARIFURI(project, path string) string {
	if project != "" {
		if rel, err := filepath.Rel(project, path); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			return filepathToURI(rel)
		}
	}
	return filepathToURI(path)
}
