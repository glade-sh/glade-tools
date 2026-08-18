package toolcli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/glade-sh/glade/tools/internal/surfaceledger"
)

func TestCompatSurfaceDeltaVerifyRejectsProjectedSetAndPreservesFailureReport(t *testing.T) {
	root := t.TempDir()
	basePath := filepath.Join(root, "base.json")
	currentPath := filepath.Join(root, "current.json")
	expectedPath := filepath.Join(root, "expected.json")
	outputPath := filepath.Join(root, "DELTA_VERIFICATION.json")
	writeJSON(t, basePath, surfaceledger.SurfaceLedger{Rows: []surfaceledger.SurfaceLedgerRow{
		{SurfaceID: "apex:System.Selected", Evidence: surfaceledger.EvidenceNone},
	}})
	writeJSON(t, currentPath, surfaceledger.SurfaceLedger{Rows: []surfaceledger.SurfaceLedgerRow{
		{SurfaceID: "apex:System.Selected", Evidence: surfaceledger.EvidenceFixture},
		{SurfaceID: "apex:System.Unrelated", Evidence: surfaceledger.EvidenceFixture},
	}})
	writeJSON(t, expectedPath, []string{"apex:System.Selected"})
	baseSHA, err := sha256File(basePath)
	if err != nil {
		t.Fatal(err)
	}
	currentSHA, err := sha256File(currentPath)
	if err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{
		"compat", "surface", "delta-verify",
		"--base-ledger", basePath,
		"--base-sha256", baseSHA,
		"--current-ledger", currentPath,
		"--current-sha256", currentSHA,
		"--expected-ids", expectedPath,
		"--output", outputPath,
	}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected failure, stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	var report surfaceledger.ExactLedgerDeltaReport
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	if report.Status != "fail" || len(report.UnexpectedSurfaceIDs) != 1 || report.UnexpectedSurfaceIDs[0] != "apex:System.Unrelated" {
		t.Fatalf("report = %+v", report)
	}

	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{
		"compat", "surface", "delta-verify",
		"--base-ledger", basePath,
		"--base-sha256", baseSHA,
		"--current-ledger", currentPath,
		"--current-sha256", currentSHA,
		"--expected-ids", expectedPath,
		"--output", outputPath,
	}, &stdout, &stderr)
	if code == 0 || !bytes.Contains(stderr.Bytes(), []byte("already exists")) {
		t.Fatalf("existing output: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	staleOutput := filepath.Join(root, "stale.json")
	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{
		"compat", "surface", "delta-verify",
		"--base-ledger", basePath,
		"--base-sha256", "stale",
		"--current-ledger", currentPath,
		"--current-sha256", currentSHA,
		"--expected-ids", expectedPath,
		"--output", staleOutput,
	}, &stdout, &stderr)
	if code == 0 || !bytes.Contains(stderr.Bytes(), []byte("base ledger SHA-256 mismatch")) {
		t.Fatalf("stale base: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(staleOutput); !os.IsNotExist(err) {
		t.Fatalf("stale binding created output: %v", err)
	}

	staleCurrentOutput := filepath.Join(root, "stale-current.json")
	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{
		"compat", "surface", "delta-verify",
		"--base-ledger", basePath,
		"--base-sha256", baseSHA,
		"--current-ledger", currentPath,
		"--current-sha256", "stale",
		"--expected-ids", expectedPath,
		"--output", staleCurrentOutput,
	}, &stdout, &stderr)
	if code == 0 || !bytes.Contains(stderr.Bytes(), []byte("current ledger SHA-256 mismatch")) {
		t.Fatalf("stale current: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(staleCurrentOutput); !os.IsNotExist(err) {
		t.Fatalf("stale current binding created output: %v", err)
	}
}
