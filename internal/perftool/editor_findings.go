package perftool

import (
	"github.com/glade-sh/glade/tools/internal/editorfindings"
	"github.com/glade-sh/glade/tools/internal/perfscan"
)

func performanceEditorFindings(report perfscan.Report) editorfindings.Payload {
	findings := make([]editorfindings.Finding, 0, len(report.Findings))
	for _, finding := range report.Findings {
		location := finding.Location
		if location.File == "" && finding.EntryPoint.File != "" {
			location.File = finding.EntryPoint.File
			location.Line = finding.EntryPoint.Line
		}
		findings = append(findings, editorfindings.Finding{
			Severity: performanceEditorSeverity(finding.Severity),
			Message:  finding.Message,
			File:     location.File,
			Line:     location.Line,
			Column:   location.Column,
			RuleID:   finding.ID,
			Source:   "performance",
		})
	}
	return editorfindings.New(findings, nil)
}

func performanceEditorSeverity(severity perfscan.Severity) string {
	switch severity {
	case perfscan.SeverityHigh:
		return "error"
	case perfscan.SeverityLow:
		return "info"
	default:
		return "warning"
	}
}
