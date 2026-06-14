package toolcli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/glade-sh/glade/tools/internal/compat"
	"github.com/glade-sh/glade/tools/internal/editorfindings"
	"github.com/glade-sh/glade/tools/internal/projectscan"
)

func postParityEditorFindings(report projectscan.Report) editorfindings.Payload {
	findings := make([]editorfindings.Finding, 0, len(report.Findings))
	for _, finding := range report.Findings {
		message := "Unsupported local surface: " + finding.Capability
		if finding.Evidence != "" {
			message += " (" + finding.Evidence + ")"
		}
		findings = append(findings, editorfindings.Finding{
			Severity: "warning",
			Message:  message,
			File:     finding.File,
			Line:     finding.Line,
			RuleID:   finding.Capability,
			Source:   "compat",
		})
	}
	return editorfindings.New(findings, nil)
}

func visualforceCaptureEditorFindings(report compat.VisualforceCaptureReport, outPath string) editorfindings.Payload {
	var findings []editorfindings.Finding
	for _, page := range report.Pages {
		findings = append(findings, visualforceRenderedEditorFinding(report.Project, page, "html", page.HTML)...)
		findings = append(findings, visualforceRenderedEditorFinding(report.Project, page, "pdf", page.PDF)...)
	}
	artifacts := editorArtifacts("Visualforce capture report", outPath)
	return editorfindings.New(findings, artifacts)
}

func visualforceRenderedEditorFinding(project string, page compat.VisualforcePageCapture, variant string, rendered compat.VisualforceRenderedCapture) []editorfindings.Finding {
	status := strings.TrimSpace(rendered.Status)
	if status == "" || status == "pass" {
		return nil
	}
	message := fmt.Sprintf("Visualforce %s capture %s for %s", variant, status, page.Name)
	if rendered.Error != "" {
		message += ": " + rendered.Error
	}
	return []editorfindings.Finding{{
		Severity: "warning",
		Message:  message,
		File:     visualforcePagePath(project, page.Name),
		RuleID:   "visualforce.capture." + variant,
		Source:   "compat",
	}}
}

func visualforcePagePath(project, name string) string {
	if strings.TrimSpace(project) == "" || strings.TrimSpace(name) == "" {
		return ""
	}
	rel := filepath.Join("force-app", "main", "default", "pages", name+".page")
	if _, err := os.Stat(filepath.Join(project, rel)); err == nil {
		return rel
	}
	return ""
}

func lwcCaptureEditorFindings(report compat.LwcCaptureReport, captureErr error) editorfindings.Payload {
	var findings []editorfindings.Finding
	if captureErr != nil {
		findings = append(findings, editorfindings.Finding{
			Severity: "warning",
			Message:  fmt.Sprintf("LWC capture deploy failed for %s: %v", report.TargetOrg, captureErr),
			RuleID:   "lwc.capture.deploy",
			Source:   "compat",
		})
	}
	for _, c := range report.Cases {
		status := strings.TrimSpace(c.Status)
		if status == "" || status == "pass" || status == "prepared" {
			continue
		}
		findings = append(findings, editorfindings.Finding{
			Severity: "warning",
			Message:  fmt.Sprintf("LWC capture %s for %s", status, c.Name),
			RuleID:   "lwc.capture." + status,
			Source:   "compat",
		})
	}
	return editorfindings.New(findings, editorArtifacts("LWC capture report", report.Artifacts.Report))
}

func editorArtifacts(label, path string) []editorfindings.Artifact {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	return []editorfindings.Artifact{{Label: label, Path: path}}
}
