package toolcli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type editorFindingsPayload struct {
	Kind      string                   `json:"kind"`
	Summary   string                   `json:"summary"`
	Findings  []editorFindingsItem     `json:"findings"`
	Artifacts []editorFindingsArtifact `json:"artifacts"`
}

type editorFindingsItem struct {
	Severity string `json:"severity"`
	Message  string `json:"message"`
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
	Column   int    `json:"column,omitempty"`
	RuleID   string `json:"ruleId"`
	Source   string `json:"source"`
}

type editorFindingsArtifact struct {
	Label string `json:"label"`
	Path  string `json:"path"`
}

func TestCompatPostParityEditorFindingsJSON(t *testing.T) {
	root := t.TempDir()
	writeEditorFindingsTestFile(t, filepath.Join(root, "sfdx-project.json"), `{"packageDirectories":[{"path":"force-app","default":true}]}`)
	writeEditorFindingsTestFile(t, filepath.Join(root, "force-app/main/default/pages/Controller.page"), `<apex:page controller="MissingController"><apex:form /></apex:page>`)

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"post-parity", "--project", root, "--json", "--editor-findings"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run returned %d, stderr=%s", code, stderr.String())
	}
	payload := decodeEditorFindingsPayload(t, stdout.Bytes())
	if payload.Kind != "glade.findings.v1" {
		t.Fatalf("kind = %q, want glade.findings.v1; json=%s", payload.Kind, stdout.String())
	}
	if len(payload.Findings) == 0 {
		t.Fatalf("expected editor findings, got %s", stdout.String())
	}
	finding := payload.Findings[0]
	if finding.Source != "compat" || finding.RuleID != "visualforce.controller-test" || finding.File != "force-app/main/default/pages/Controller.page" || finding.Severity != "warning" {
		t.Fatalf("unexpected first finding: %#v", finding)
	}
	if !strings.Contains(payload.Summary, "finding") {
		t.Fatalf("summary = %q", payload.Summary)
	}
}

func TestCompatLwcCaptureEditorFindingsJSON(t *testing.T) {
	root := t.TempDir()
	outPath := filepath.Join(root, "reports", "lwc-org-capture.json")
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{
		"compat", "lwc", "capture",
		"--target-org", "dummy",
		"--project", root,
		"--targets", "direct-component",
		"--skip-deploy",
		"--out", outPath,
		"--json",
		"--editor-findings",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run returned %d, stderr=%s", code, stderr.String())
	}
	payload := decodeEditorFindingsPayload(t, stdout.Bytes())
	if payload.Kind != "glade.findings.v1" || len(payload.Findings) != 0 {
		t.Fatalf("unexpected editor findings payload: %s", stdout.String())
	}
	if len(payload.Artifacts) != 1 || payload.Artifacts[0].Path != outPath {
		t.Fatalf("artifacts = %#v, want %s", payload.Artifacts, outPath)
	}
	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("expected partial report artifact at %s: %v", outPath, err)
	}
}

func TestCompatLwcCaptureEditorFindingsJSONOnDeployFailure(t *testing.T) {
	root := t.TempDir()
	outPath := filepath.Join(root, "reports", "lwc-org-capture.json")
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{
		"compat", "lwc", "capture",
		"--target-org", "dummy",
		"--project", root,
		"--targets", "direct-component",
		"--out", outPath,
		"--json",
		"--editor-findings",
	}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected deploy failure, stdout=%s", stdout.String())
	}
	payload := decodeEditorFindingsPayload(t, stdout.Bytes())
	if payload.Kind != "glade.findings.v1" || len(payload.Findings) == 0 {
		t.Fatalf("expected editor findings payload, got %s", stdout.String())
	}
	finding := payload.Findings[0]
	if finding.Source != "compat" || finding.RuleID != "lwc.capture.deploy" || finding.Severity != "warning" ||
		!strings.Contains(finding.Message, "dummy") {
		t.Fatalf("unexpected deploy finding: %#v", finding)
	}
	if len(payload.Artifacts) != 1 || payload.Artifacts[0].Path != outPath {
		t.Fatalf("artifacts = %#v, want %s", payload.Artifacts, outPath)
	}
	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("expected partial report artifact at %s: %v", outPath, err)
	}
}

func TestVisualforceLocalCaptureEditorFindingsJSON(t *testing.T) {
	root := t.TempDir()
	pageDir := filepath.Join(root, "force-app", "main", "default", "pages")
	if err := os.MkdirAll(pageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeEditorFindingsTestFile(t, filepath.Join(pageDir, "Core.page"), `<apex:page>Core</apex:page>`)
	outPath := filepath.Join(root, "visualforce-local.json")
	termFile := filepath.Join(t.TempDir(), "terminated.txt")
	t.Setenv("GLADE_TOOLS_FAKE_GLADE_VF", "1")
	t.Setenv("GLADE_TOOLS_FAKE_GLADE_VF_TERM", termFile)

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"visualforce", "capture", "--local", "--glade-bin", os.Args[0], "--project", root, "--pages", "Core", "--out", outPath, "--json", "--editor-findings"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected failed capture report to return nonzero, stdout=%s", stdout.String())
	}
	payload := decodeEditorFindingsPayload(t, stdout.Bytes())
	if payload.Kind != "glade.findings.v1" || len(payload.Findings) == 0 {
		t.Fatalf("expected visualforce editor findings, got %s", stdout.String())
	}
	if payload.Findings[0].Source != "compat" || !strings.Contains(payload.Findings[0].Message, "Core") {
		t.Fatalf("unexpected first finding: %#v", payload.Findings[0])
	}
	if len(payload.Artifacts) != 1 || payload.Artifacts[0].Path != outPath {
		t.Fatalf("artifacts = %#v, want %s", payload.Artifacts, outPath)
	}
	waitForFile(t, termFile)
}

func decodeEditorFindingsPayload(t *testing.T, data []byte) editorFindingsPayload {
	t.Helper()
	var payload editorFindingsPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("payload is not JSON: %v\n%s", err, string(data))
	}
	return payload
}

func writeEditorFindingsTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
