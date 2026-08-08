package surfaceledger

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

const currentBaseRebindCandidateSHA256 = "773bd1ddc0d1a41c2972032837321714bba3255dbc21187a43fc52d306dee4e4"

type currentBaseRebindEnvelope struct {
	Candidate struct {
		Commit string `json:"commit"`
		SHA256 string `json:"sha256"`
	} `json:"candidate"`
	Local struct {
		CandidatePath string `json:"candidatePath"`
		CandidateSHA  string `json:"candidateSha256"`
		SourcePath    string `json:"sourcePath"`
		SourceSHA     string `json:"sourceSha256"`
		ReportPath    string `json:"reportPath"`
		ReportSHA     string `json:"reportSha256"`
	} `json:"local"`
	Comparisons []struct {
		SourcePath      string `json:"sourcePath"`
		SourceSHA       string `json:"sourceSha256"`
		GladeReportPath string `json:"gladeReportPath"`
		GladeReportSHA  string `json:"gladeReportSha256"`
	} `json:"comparisons"`
}

func TestCurrentBaseRebindEvidencePinsFreshCandidateAndPassingLocalRuns(t *testing.T) {
	toolsRoot := filepath.Join("..", "..")
	evidenceRoot, err := filepath.Abs(filepath.Join(toolsRoot, "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	candidate := filepath.Join(evidenceRoot, "evidence", "glade-candidate-current-base-0a0f624-cgo")
	if got := currentBaseRebindSHA256(t, candidate); got != currentBaseRebindCandidateSHA256 {
		t.Fatalf("candidate SHA-256 = %s, want %s", got, currentBaseRebindCandidateSHA256)
	}

	paths := []string{
		"docs/fixtures/salesforce-metadata-enum-batch-20260803-rebind-0a0f624-comparisons.json",
		"docs/fixtures/salesforce-system-exception-api67-safe-family-rebind-0a0f624-comparisons.json",
		"docs/fixtures/salesforce-metadata-status-code-batch-20260803-rebind-0a0f624-comparisons.json",
	}
	for _, relative := range paths {
		var envelope currentBaseRebindEnvelope
		currentBaseRebindReadJSON(t, filepath.Join(toolsRoot, relative), &envelope)
		if envelope.Candidate.Commit != "0a0f624e9c6fc82f8efc824852aef2808cd823fa" || envelope.Candidate.SHA256 != currentBaseRebindCandidateSHA256 || envelope.Local.CandidateSHA != currentBaseRebindCandidateSHA256 {
			t.Fatalf("%s candidate provenance = %#v", relative, envelope.Candidate)
		}
		if envelope.Local.CandidatePath != "evidence/glade-candidate-current-base-0a0f624-cgo" {
			t.Fatalf("%s candidate path = %q", relative, envelope.Local.CandidatePath)
		}
		if envelope.Local.SourcePath != "" {
			currentBaseRebindAssertArtifact(t, evidenceRoot, envelope.Local.SourcePath, envelope.Local.SourceSHA)
			currentBaseRebindAssertPassedReport(t, evidenceRoot, envelope.Local.ReportPath, envelope.Local.ReportSHA)
		}
		for _, comparison := range envelope.Comparisons {
			if comparison.SourcePath != "" {
				currentBaseRebindAssertArtifact(t, evidenceRoot, comparison.SourcePath, comparison.SourceSHA)
				currentBaseRebindAssertPassedReport(t, evidenceRoot, comparison.GladeReportPath, comparison.GladeReportSHA)
			}
		}
	}
}

func currentBaseRebindSHA256(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func currentBaseRebindReadJSON(t *testing.T, path string, out any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, out); err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
}

func currentBaseRebindAssertArtifact(t *testing.T, root, relative, want string) {
	t.Helper()
	if relative == "" || want == "" {
		t.Fatalf("missing artifact provenance: path=%q sha=%q", relative, want)
	}
	path := filepath.Join(root, relative)
	if got := currentBaseRebindSHA256(t, path); got != want {
		t.Fatalf("%s SHA-256 = %s, want %s", relative, got, want)
	}
}

func currentBaseRebindAssertPassedReport(t *testing.T, root, relative, want string) {
	t.Helper()
	currentBaseRebindAssertArtifact(t, root, relative, want)
	var report struct {
		Status   string `json:"status"`
		ExitCode int    `json:"exitCode"`
		Summary  struct {
			Passed int `json:"passed"`
		} `json:"summary"`
	}
	currentBaseRebindReadJSON(t, filepath.Join(root, relative), &report)
	if report.Status != "passed" || report.ExitCode != 0 {
		t.Fatalf("local report %s = %#v", relative, report)
	}
}
