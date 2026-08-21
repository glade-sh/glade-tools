package corpusassurance

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/surfaceledger"
)

func TestBuildSurfaceOracleScopeDerivesEveryRuntimeRow(t *testing.T) {
	root := t.TempDir()
	ledgerPath := filepath.Join(root, "ledger.json")
	policyPath := filepath.Join(root, "policy.json")
	profilePath := filepath.Join(root, "profile.json")
	outputPath := filepath.Join(root, "scope.json")
	ledger := surfaceledger.SurfaceLedger{SchemaVersion: 1, Summary: surfaceledger.LedgerSummary{Gaps: map[string]int{}, Failures: map[string]int{}}, Rows: []surfaceledger.SurfaceLedgerRow{
		{SurfaceID: "apex:Runtime.z()", Product: surfaceledger.ProductApex, Namespace: "System"},
		{SurfaceID: "apex:Mock.a()", Product: surfaceledger.ProductApex, Namespace: "System"},
		{SurfaceID: "apex:Compile.only()", Product: surfaceledger.ProductApex, Namespace: "System"},
		{SurfaceID: "apex:Hosted.only()", Product: surfaceledger.ProductApex, Namespace: "System"},
	}}
	policy := surfaceledger.SupportPolicy{Rules: []surfaceledger.SupportPolicyRule{
		{SurfaceID: "apex:Runtime.z()", Disposition: surfaceledger.DispositionLocalRuntimeRequired, Reason: "test"},
		{SurfaceID: "apex:Mock.a()", Disposition: surfaceledger.DispositionDeterministicMockRequired, Reason: "test"},
		{SurfaceID: "apex:Compile.only()", Disposition: surfaceledger.DispositionCompileShapeRequired, Reason: "test"},
		{SurfaceID: "apex:Hosted.only()", Disposition: surfaceledger.DispositionHostedDeferred, Reason: "test"},
	}}
	if err := WriteNewJSON(ledgerPath, ledger); err != nil {
		t.Fatal(err)
	}
	if err := WriteNewJSON(policyPath, policy); err != nil {
		t.Fatal(err)
	}
	profile := surfaceledger.SupportProfile{
		Total: 4,
		ByDisposition: map[surfaceledger.SupportDisposition]int{
			surfaceledger.DispositionCompileShapeRequired:      1,
			surfaceledger.DispositionDeterministicMockRequired: 1,
			surfaceledger.DispositionHostedDeferred:            1,
			surfaceledger.DispositionLocalRuntimeRequired:      1,
		},
		ByGapClass: map[string]int{},
		Rows: []surfaceledger.SupportProfileRow{
			{SurfaceID: "apex:Runtime.z()", Disposition: surfaceledger.DispositionLocalRuntimeRequired},
			{SurfaceID: "apex:Hosted.only()", Disposition: surfaceledger.DispositionHostedDeferred},
			{SurfaceID: "apex:Compile.only()", Disposition: surfaceledger.DispositionCompileShapeRequired},
			{SurfaceID: "apex:Mock.a()", Disposition: surfaceledger.DispositionDeterministicMockRequired},
		},
		Inputs: &surfaceledger.SupportProfileInputs{Files: []surfaceledger.SupportProfileInput{
			{Name: "ledger", Path: ledgerPath, SHA256: sha256FileForTest(t, ledgerPath)},
			{Name: "policy", Path: policyPath, SHA256: sha256FileForTest(t, policyPath)},
		}},
	}
	if err := WriteNewJSON(profilePath, profile); err != nil {
		t.Fatal(err)
	}

	scope, err := BuildSurfaceOracleScope(profilePath, ledgerPath, policyPath, outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if scope.SchemaVersion != 1 || scope.Kind != "all-runtime" || scope.Total != 2 || scope.SourceProfileSHA256 != sha256FileForTest(t, profilePath) || scope.LedgerSHA256 != sha256FileForTest(t, ledgerPath) || scope.PolicySHA256 != sha256FileForTest(t, policyPath) {
		t.Fatalf("scope binding = %#v", scope)
	}
	if got := scope.Rows; len(got) != 2 || got[0].SurfaceID != "apex:Mock.a()" || got[0].Disposition != deterministicMockRequired || got[1].SurfaceID != "apex:Runtime.z()" || got[1].Disposition != localRuntimeRequired {
		t.Fatalf("scope rows = %#v", got)
	}
	if scope.ByDisposition[deterministicMockRequired] != 1 || scope.ByDisposition[localRuntimeRequired] != 1 {
		t.Fatalf("scope counts = %#v", scope.ByDisposition)
	}
	if _, err := BuildSurfaceOracleScope(profilePath, ledgerPath, policyPath, outputPath); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("scope overwrite error = %v", err)
	}
}

func TestBuildSurfaceOracleScopeRejectsUnboundOrInvalidRows(t *testing.T) {
	for _, test := range []struct {
		name        string
		profileRows []surfaceledger.SupportProfileRow
		ledgerRows  []surfaceledger.SurfaceLedgerRow
		ledgerHash  string
	}{
		{name: "duplicate", profileRows: []surfaceledger.SupportProfileRow{{SurfaceID: "apex:Runtime.run()", Disposition: surfaceledger.DispositionLocalRuntimeRequired}, {SurfaceID: "apex:Runtime.run()", Disposition: surfaceledger.DispositionLocalRuntimeRequired}}, ledgerRows: []surfaceledger.SurfaceLedgerRow{{SurfaceID: "apex:Runtime.run()", Product: surfaceledger.ProductApex, Namespace: "System"}}},
		{name: "unknown disposition", profileRows: []surfaceledger.SupportProfileRow{{SurfaceID: "apex:Runtime.run()", Disposition: "unknown"}}, ledgerRows: []surfaceledger.SurfaceLedgerRow{{SurfaceID: "apex:Runtime.run()", Product: surfaceledger.ProductApex, Namespace: "System"}}},
		{name: "missing ledger row", profileRows: []surfaceledger.SupportProfileRow{{SurfaceID: "apex:Runtime.run()", Disposition: surfaceledger.DispositionLocalRuntimeRequired}}, ledgerRows: []surfaceledger.SurfaceLedgerRow{{SurfaceID: "apex:Other.run()", Product: surfaceledger.ProductApex, Namespace: "System"}}},
		{name: "stale ledger binding", profileRows: []surfaceledger.SupportProfileRow{{SurfaceID: "apex:Runtime.run()", Disposition: surfaceledger.DispositionLocalRuntimeRequired}}, ledgerRows: []surfaceledger.SurfaceLedgerRow{{SurfaceID: "apex:Runtime.run()", Product: surfaceledger.ProductApex, Namespace: "System"}}, ledgerHash: strings.Repeat("0", 64)},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			ledgerPath, policyPath := filepath.Join(root, "ledger.json"), filepath.Join(root, "policy.json")
			if err := WriteNewJSON(ledgerPath, surfaceledger.SurfaceLedger{SchemaVersion: 1, Rows: test.ledgerRows, Summary: surfaceledger.LedgerSummary{Gaps: map[string]int{}, Failures: map[string]int{}}}); err != nil {
				t.Fatal(err)
			}
			if err := WriteNewJSON(policyPath, surfaceledger.SupportPolicy{Rules: []surfaceledger.SupportPolicyRule{{Namespace: "System", Disposition: surfaceledger.DispositionLocalRuntimeRequired, Reason: "test"}}}); err != nil {
				t.Fatal(err)
			}
			ledgerHash := test.ledgerHash
			if ledgerHash == "" {
				ledgerHash = sha256FileForTest(t, ledgerPath)
			}
			counts := map[surfaceledger.SupportDisposition]int{}
			for _, row := range test.profileRows {
				counts[row.Disposition]++
			}
			profile := surfaceledger.SupportProfile{Total: len(test.profileRows), ByDisposition: counts, ByGapClass: map[string]int{}, Rows: test.profileRows, Inputs: &surfaceledger.SupportProfileInputs{Files: []surfaceledger.SupportProfileInput{{Name: "ledger", SHA256: ledgerHash}, {Name: "policy", SHA256: sha256FileForTest(t, policyPath)}}}}
			profilePath := filepath.Join(root, "profile.json")
			if err := WriteNewJSON(profilePath, profile); err != nil {
				t.Fatal(err)
			}
			if _, err := BuildSurfaceOracleScope(profilePath, ledgerPath, policyPath, filepath.Join(root, "scope.json")); err == nil {
				t.Fatal("invalid scope inputs accepted")
			}
		})
	}
}
