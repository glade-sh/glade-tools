package corpusassurance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/surfaceledger"
)

func TestSystemStatusCodeFixturesCloseNativeLocalProofPlan(t *testing.T) {
	root := filepath.Join("..", "..")
	fixtureRoot, err := filepath.Abs(filepath.Join(root, "docs", "fixtures"))
	if err != nil {
		t.Fatal(err)
	}
	ids := systemStatusCodeSourceIDs(t, filepath.Join(fixtureRoot, "current-base-cb191-system-rebind-positive-api67.json"))
	ledgerPath := filepath.Join(t.TempDir(), "SURFACE_LEDGER.json")
	policyPath := filepath.Join(filepath.Dir(ledgerPath), "SUPPORT_POLICY.json")
	profilePath := filepath.Join(filepath.Dir(ledgerPath), "SOURCE_PROFILE.json")
	scopePath := filepath.Join(filepath.Dir(ledgerPath), "SURFACE_ORACLE_SCOPE.json")
	rows := make([]surfaceledger.SurfaceLedgerRow, 0, len(ids))
	for _, id := range ids {
		rows = append(rows, surfaceledger.SurfaceLedgerRow{SurfaceID: id, Product: surfaceledger.ProductApex, Area: surfaceledger.AreaRuntime, Namespace: "System", TypeName: "System.StatusCode", Kind: surfaceledger.KindType})
	}
	ledger := surfaceledger.SurfaceLedger{SchemaVersion: surfaceledger.SchemaVersion, Rows: rows, Summary: surfaceledger.LedgerSummary{Gaps: map[string]int{}, Failures: map[string]int{}}}
	policy := surfaceledger.SupportPolicy{Rules: []surfaceledger.SupportPolicyRule{{SurfacePrefix: "apex:System.StatusCode", Disposition: surfaceledger.DispositionLocalRuntimeRequired, Reason: "native runtime coverage"}}}
	if err := WriteNewJSON(ledgerPath, ledger); err != nil {
		t.Fatal(err)
	}
	if err := WriteNewJSON(policyPath, policy); err != nil {
		t.Fatal(err)
	}
	profile := surfaceledger.ComputeSupportProfile(rows, policy, nil)
	profile.Inputs = &surfaceledger.SupportProfileInputs{Files: []surfaceledger.SupportProfileInput{
		{Name: "ledger", Path: ledgerPath, SHA256: localProofFileSHA256(t, ledgerPath)},
		{Name: "policy", Path: policyPath, SHA256: localProofFileSHA256(t, policyPath)},
	}}
	if err := WriteNewJSON(profilePath, profile); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildSurfaceOracleScope(profilePath, ledgerPath, policyPath, scopePath); err != nil {
		t.Fatal(err)
	}
	outputRoot := filepath.Dir(ledgerPath)
	manifest, coverage, err := BuildSurfaceLocalProofPlan(SurfaceLocalProofPlanRequest{
		ScopePath: scopePath, SourceProfilePath: profilePath, LedgerPath: ledgerPath, PolicyPath: policyPath, FixtureRoot: fixtureRoot,
		ProfilePath: filepath.Join(outputRoot, "LOCAL_PROFILE.json"), UsagePath: filepath.Join(outputRoot, "LOCAL_USAGE.json"), LocalDecisionPath: filepath.Join(outputRoot, "LOCAL_DECISION.json"), ManifestPath: filepath.Join(outputRoot, "FIXTURES.json"), CoveragePath: filepath.Join(outputRoot, "COVERAGE.json"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if coverage.Total != 628 || coverage.Covered != 628 || coverage.MissingCount != 0 || len(coverage.Missing) != 0 {
		t.Fatalf("coverage = %#v", coverage)
	}
	if len(manifest.Fixtures) != 8 || len(manifest.SalesforceFixtures) != 8 {
		t.Fatalf("fixture manifest counts = %d/%d", len(manifest.Fixtures), len(manifest.SalesforceFixtures))
	}
}

func systemStatusCodeSourceIDs(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Evidence []struct {
			SurfaceID string `json:"surfaceId"`
		} `json:"evidence"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(fixture.Evidence))
	for _, row := range fixture.Evidence {
		if strings.HasPrefix(row.SurfaceID, "apex:System.StatusCode") {
			ids = append(ids, row.SurfaceID)
		}
	}
	if len(ids) != 628 {
		t.Fatalf("source IDs = %d, want 628", len(ids))
	}
	return ids
}
