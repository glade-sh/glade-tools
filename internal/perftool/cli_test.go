package perftool

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestManifestJSONListsPerformanceScan(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"manifest", "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run returned %d, stderr=%s", code, stderr.String())
	}

	var manifest struct {
		APIVersion string `json:"apiVersion"`
		Name       string `json:"name"`
		Commands   []struct {
			Path []string `json:"path"`
		} `json:"commands"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &manifest); err != nil {
		t.Fatalf("manifest is not JSON: %v\n%s", err, stdout.String())
	}
	if manifest.APIVersion != "glade.plugin.v1" || manifest.Name != "performance" {
		t.Fatalf("unexpected manifest identity: %#v", manifest)
	}
	if len(manifest.Commands) != 1 || len(manifest.Commands[0].Path) != 1 || manifest.Commands[0].Path[0] != "performance" {
		t.Fatalf("unexpected commands: %#v", manifest.Commands)
	}
}

func TestPerformanceScanJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	project := filepath.Join("..", "perfscan", "testdata", "perf-project")
	code := Run(context.Background(), []string{"performance", "scan", "--project", project, "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run returned %d, stderr=%s", code, stderr.String())
	}

	var report struct {
		SchemaVersion int `json:"schemaVersion"`
		Summary       struct {
			Findings int `json:"findings"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("report is not JSON: %v\n%s", err, stdout.String())
	}
	if report.SchemaVersion == 0 || report.Summary.Findings == 0 {
		t.Fatalf("expected performance findings, got %#v", report)
	}
}
