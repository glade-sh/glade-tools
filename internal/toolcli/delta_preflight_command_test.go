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

func TestCompatSurfaceDeltaPreflightJSON(t *testing.T) {
	root := t.TempDir()
	basePath := filepath.Join(root, "base.json")
	additionsPath := filepath.Join(root, "additions.json")
	tombstonesPath := filepath.Join(root, "tombstones.json")
	policyPath := filepath.Join(root, "policy.json")
	writeJSON(t, basePath, surfaceledger.SurfaceLedger{
		SchemaVersion: surfaceledger.SchemaVersion,
		Rows: []surfaceledger.SurfaceLedgerRow{{
			SurfaceID:     "apex:System.Base",
			Product:       surfaceledger.ProductApex,
			Kind:          surfaceledger.KindType,
			GladeShape:    surfaceledger.ShapeTypeKnown,
			GladeBehavior: surfaceledger.BehaviorSupported,
			Evidence:      surfaceledger.EvidenceFixture,
		}},
	})
	writeJSON(t, additionsPath, []surfaceledger.SurfaceLedgerRow{{
		SurfaceID:     "apex:System.Added",
		Product:       surfaceledger.ProductApex,
		Kind:          surfaceledger.KindType,
		GladeShape:    surfaceledger.ShapeTypeKnown,
		GladeBehavior: surfaceledger.BehaviorSupported,
		Evidence:      surfaceledger.EvidenceFixture,
	}})
	writeJSON(t, tombstonesPath, map[string]any{"removals": []map[string]string{{"surfaceId": "apex:System.Base"}}})
	writeJSON(t, policyPath, surfaceledger.SupportPolicy{Rules: []surfaceledger.SupportPolicyRule{{
		Namespace:   "System",
		Disposition: surfaceledger.DispositionLocalRuntimeRequired,
		Reason:      "local test",
	}}})

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{
		"compat", "surface", "delta-preflight",
		"--base-ledger", basePath,
		"--additions", additionsPath,
		"--tombstones", tombstonesPath,
		"--policy", policyPath,
		"--json",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d stderr=%q stdout=%q", code, stderr.String(), stdout.String())
	}
	var result surfaceledger.DeltaPreflightResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("parse result: %v; output=%q", err, stdout.String())
	}
	if result.BaseRows != 1 || result.ResultRows != 1 {
		t.Fatalf("row counts = %d -> %d, want 1 -> 1", result.BaseRows, result.ResultRows)
	}
	if len(result.AddedIDs) != 1 || result.AddedIDs[0] != "apex:System.Added" {
		t.Fatalf("added IDs = %v", result.AddedIDs)
	}
	if len(result.RemovedIDs) != 1 || result.RemovedIDs[0] != "apex:System.Base" {
		t.Fatalf("removed IDs = %v", result.RemovedIDs)
	}
	if len(result.TombstoneIDs) != 1 || result.TombstoneIDs[0] != "apex:System.Base" {
		t.Fatalf("tombstone IDs = %v", result.TombstoneIDs)
	}
}

func TestCompatSurfaceDeltaPreflightWritesCompactOutput(t *testing.T) {
	root := t.TempDir()
	basePath := filepath.Join(root, "base.json")
	outputPath := filepath.Join(root, "nested", "delta.json")
	// Raw row arrays are also accepted because evidence bundles retain
	// BASE_LEDGER_ROWS.json next to the wrapped SURFACE_LEDGER.json.
	writeJSON(t, basePath, []surfaceledger.SurfaceLedgerRow(nil))
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{
		"compat", "surface", "delta-preflight",
		"--base-ledger", basePath,
		"--output", outputPath,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d stderr=%q stdout=%q", code, stderr.String(), stdout.String())
	}
	if _, err := os.Stat(outputPath); err != nil {
		t.Fatalf("output missing: %v", err)
	}
	var result surfaceledger.DeltaPreflightResult
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("parse output: %v", err)
	}
	if result.BaseRows != 0 || result.ResultRows != 0 {
		t.Fatalf("empty result counts = %d -> %d", result.BaseRows, result.ResultRows)
	}
}

func TestCompatSurfaceDeltaPreflightReadsNestedTombstones(t *testing.T) {
	root := t.TempDir()
	basePath := filepath.Join(root, "base.json")
	tombstonesPath := filepath.Join(root, "manifest.json")
	writeJSON(t, basePath, surfaceledger.SurfaceLedger{Rows: []surfaceledger.SurfaceLedgerRow{{
		SurfaceID: "apex:System.Invalid", Product: surfaceledger.ProductApex,
	}}})
	writeJSON(t, tombstonesPath, map[string]any{
		"setChecks": map[string]any{
			"api67NegativeTombstones": map[string]any{
				"ids": []string{"apex:System.Invalid"},
			},
		},
	})
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{
		"compat", "surface", "delta-preflight",
		"--base-ledger", basePath,
		"--tombstones", tombstonesPath,
		"--json",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d stderr=%q stdout=%q", code, stderr.String(), stdout.String())
	}
	var result surfaceledger.DeltaPreflightResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if len(result.TombstoneIDs) != 1 || result.TombstoneIDs[0] != "apex:System.Invalid" {
		t.Fatalf("tombstone IDs = %v", result.TombstoneIDs)
	}
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
