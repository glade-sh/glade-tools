package corpusassurance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/surfaceledger"
)

func TestBuildSurfaceLocalProofPlanSealsCompleteScope(t *testing.T) {
	request := surfaceLocalProofPlanRequest(t)
	localProofFixture(t, request.FixtureRoot, "runtime", []string{"apex:System.Runtime.run()"}, localRuntimeRequired)
	localProofFixture(t, request.FixtureRoot, "mock", []string{"apex:System.Mock.run()"}, deterministicMockRequired)

	manifest, coverage, err := BuildSurfaceLocalProofPlan(request)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Fixtures) != 2 || coverage.Total != 2 || coverage.Covered != 2 || coverage.MissingCount != 0 || len(coverage.Missing) != 0 || len(coverage.UnclassifiedFixtures) != 0 {
		t.Fatalf("manifest=%#v coverage=%#v", manifest, coverage)
	}
	for _, path := range []string{request.ProfilePath, request.UsagePath, request.LocalDecisionPath, request.ManifestPath, request.CoveragePath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("missing plan output %s: %v", path, err)
		}
	}
}

func TestBuildSurfaceLocalProofPlanRetainsExactMissingCoverage(t *testing.T) {
	request := surfaceLocalProofPlanRequest(t)
	localProofFixture(t, request.FixtureRoot, "runtime", []string{"apex:System.Runtime.run()"}, localRuntimeRequired)

	_, coverage, err := BuildSurfaceLocalProofPlan(request)
	if err == nil || !strings.Contains(err.Error(), "covered=1 missing=1") {
		t.Fatalf("incomplete plan error = %v", err)
	}
	if coverage.Total != 2 || coverage.Covered != 1 || len(coverage.Missing) != 1 || coverage.Missing[0].SurfaceID != "apex:System.Mock.run()" {
		t.Fatalf("coverage = %#v", coverage)
	}
	if _, err := os.Stat(request.CoveragePath); err != nil {
		t.Fatalf("missing retained coverage: %v", err)
	}
	for _, path := range []string{request.ProfilePath, request.UsagePath, request.LocalDecisionPath, request.ManifestPath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("incomplete plan wrote authoritative output %s", path)
		}
	}
}

func TestBuildSurfaceLocalProofPlanDoesNotCreditUnmaterializableFixtures(t *testing.T) {
	request := surfaceLocalProofPlanRequest(t)
	runtime := localProofFixture(t, request.FixtureRoot, "runtime", []string{"apex:System.Runtime.run()"}, localRuntimeRequired)
	localProofFixture(t, request.FixtureRoot, "mock", []string{"apex:System.Mock.run()"}, deterministicMockRequired)

	data, err := os.ReadFile(runtime.Path)
	if err != nil {
		t.Fatal(err)
	}
	var fixture map[string]any
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	fixture["expected"] = map[string]any{"error": map[string]any{"type": "Error", "message": "expected failure"}}
	writeLocalProofJSON(t, runtime.Path, fixture)

	_, coverage, err := BuildSurfaceLocalProofPlan(request)
	if err == nil || !strings.Contains(err.Error(), "covered=1 missing=1") {
		t.Fatalf("incomplete plan error = %v", err)
	}
	if len(coverage.Missing) != 1 || coverage.Missing[0].SurfaceID != "apex:System.Runtime.run()" {
		t.Fatalf("coverage = %#v", coverage)
	}
}

func TestCanonicalPositiveFixturesRemainLocalProofCandidates(t *testing.T) {
	want := []string{
		"apex:System.Database.executeBatch(Object,Integer)",
		"apex:System.System.attachFinalizer(Object)",
		"apex:System.Test.clearApexPageMessages()",
		"apex:System.Test.createStub(Type,StubProvider)",
		"apex:System.Test.setFixedSearchResults(List<Id>)",
	}
	required := map[string]string{}
	for _, surfaceID := range want {
		required[surfaceID] = localRuntimeRequired
	}
	fixtureRoot, err := filepath.Abs(filepath.Join("..", "..", "docs", "fixtures"))
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := discoverLocalProofFixtures(fixtureRoot, required)
	if err != nil {
		t.Fatal(err)
	}
	owners := map[string]string{}
	for _, fixture := range selectLocalProofFixtures(candidates).Fixtures {
		for _, surfaceID := range fixture.OwnedSurfaceIDs {
			owners[surfaceID] = fixture.ID
		}
	}
	for _, surfaceID := range want {
		if owners[surfaceID] == "" {
			t.Errorf("%s has no selected owner", surfaceID)
		}
	}
	_, missing, err := analyzeLocalProofFixtures(fixtureRoot, required)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 0 {
		t.Fatalf("canonical positive fixtures missing after selection: %v", missing)
	}
}

func TestBuildSurfaceLocalProofPlanRetainsUnclassifiedFixtures(t *testing.T) {
	request := surfaceLocalProofPlanRequest(t)
	runtimeFixture := localProofFixture(t, request.FixtureRoot, "runtime", []string{"apex:System.Runtime.run()"}, localRuntimeRequired)
	localProofFixture(t, request.FixtureRoot, "mock", []string{"apex:System.Mock.run()"}, deterministicMockRequired)
	data, err := os.ReadFile(runtimeFixture.Path)
	if err != nil {
		t.Fatal(err)
	}
	var fixture map[string]any
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	delete(fixture, "salesforceEligible")
	writeLocalProofJSON(t, runtimeFixture.Path, fixture)

	_, coverage, err := BuildSurfaceLocalProofPlan(request)
	if err == nil || !strings.Contains(err.Error(), "unclassified-fixtures=1") {
		t.Fatalf("unclassified plan error = %v", err)
	}
	if coverage.Covered != 2 || coverage.MissingCount != 0 || len(coverage.UnclassifiedFixtures) != 1 || coverage.UnclassifiedFixtures[0] != "runtime" {
		t.Fatalf("coverage = %#v", coverage)
	}
	if _, err := os.Stat(request.ManifestPath); !os.IsNotExist(err) {
		t.Fatal("unclassified plan wrote authoritative manifest")
	}
}

func TestBuildSurfaceLocalProofPlanConsumesTerminalAuthorityWithoutProofCredit(t *testing.T) {
	request := surfaceLocalProofPlanRequest(t)
	localProofFixture(t, request.FixtureRoot, "runtime", []string{"apex:System.Runtime.run()"}, localRuntimeRequired)
	if _, _, err := BuildSurfaceLocalProofPlan(request); err == nil {
		t.Fatal("initial incomplete plan unexpectedly passed")
	}
	classificationPath := filepath.Join(filepath.Dir(request.CoveragePath), "terminal-classifications.json")
	if err := WriteNewJSON(classificationPath, ExclusionPolicy{SchemaVersion: 1, Rows: []ExclusionPolicyRow{{SurfaceID: "apex:System.Mock.run()", Class: terminalHostedContext, Reason: "requires hosted context"}}}); err != nil {
		t.Fatal(err)
	}
	authorityPath := filepath.Join(filepath.Dir(request.CoveragePath), "terminal-authority.json")
	if _, err := CreateSurfaceTerminalAuthority(SurfaceTerminalAuthorityRequest{ScopePath: request.ScopePath, CoveragePath: request.CoveragePath, LedgerPath: request.LedgerPath, SupportPolicyPath: request.PolicyPath, FixtureRoot: request.FixtureRoot, ClassificationPath: classificationPath, OutputPath: authorityPath}); err != nil {
		t.Fatal(err)
	}
	request.TerminalAuthorityPath = authorityPath
	request.ProfilePath += ".terminal"
	request.UsagePath += ".terminal"
	request.LocalDecisionPath += ".terminal"
	request.ManifestPath += ".terminal"
	request.CoveragePath += ".terminal"
	_, coverage, err := BuildSurfaceLocalProofPlan(request)
	if err != nil {
		t.Fatal(err)
	}
	if coverage.Covered != 1 || coverage.MissingCount != 1 || coverage.TerminalAccounting == nil || coverage.TerminalAccounting.DirectLocalProof != 1 || coverage.TerminalAccounting.TerminalAccounted != 1 || coverage.TerminalAccounting.Accounted != 2 || coverage.TerminalAccounting.Remaining != 0 || coverage.TerminalAccounting.LocalRuntimeCredit != 0 || coverage.TerminalAccounting.SalesforceParityCredit != 0 {
		t.Fatalf("coverage = %#v", coverage)
	}
	profile, err := readExactJSON[LocalProofProfile](request.ProfilePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(profile.Rows) != 1 || profile.Rows[0].SurfaceID != "apex:System.Runtime.run()" {
		t.Fatalf("runnable profile = %#v", profile.Rows)
	}
}

func surfaceLocalProofPlanRequest(t *testing.T) SurfaceLocalProofPlanRequest {
	t.Helper()
	root := t.TempDir()
	ledgerPath := filepath.Join(root, "ledger.json")
	policyPath := filepath.Join(root, "policy.json")
	profilePath := filepath.Join(root, "profile.json")
	scopePath := filepath.Join(root, "scope.json")
	fixtureRoot := filepath.Join(root, "fixtures")
	if err := os.Mkdir(fixtureRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	ledger := surfaceledger.SurfaceLedger{SchemaVersion: surfaceledger.SchemaVersion, Summary: surfaceledger.LedgerSummary{Gaps: map[string]int{}, Failures: map[string]int{}}, Rows: []surfaceledger.SurfaceLedgerRow{
		{SurfaceID: "apex:System.Runtime.run()", Product: surfaceledger.ProductApex, Namespace: "System"},
		{SurfaceID: "apex:System.Mock.run()", Product: surfaceledger.ProductApex, Namespace: "System"},
	}}
	policy := surfaceledger.SupportPolicy{Rules: []surfaceledger.SupportPolicyRule{
		{SurfaceID: "apex:System.Runtime.run()", Disposition: surfaceledger.DispositionLocalRuntimeRequired, Reason: "test"},
		{SurfaceID: "apex:System.Mock.run()", Disposition: surfaceledger.DispositionDeterministicMockRequired, Reason: "test"},
	}}
	if err := WriteNewJSON(ledgerPath, ledger); err != nil {
		t.Fatal(err)
	}
	if err := WriteNewJSON(policyPath, policy); err != nil {
		t.Fatal(err)
	}
	profile := surfaceledger.ComputeSupportProfile(ledger.Rows, policy, nil)
	profile.Inputs = &surfaceledger.SupportProfileInputs{Files: []surfaceledger.SupportProfileInput{
		{Name: "ledger", Path: ledgerPath, SHA256: sha256FileForTest(t, ledgerPath)},
		{Name: "policy", Path: policyPath, SHA256: sha256FileForTest(t, policyPath)},
	}}
	if err := WriteNewJSON(profilePath, profile); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildSurfaceOracleScope(profilePath, ledgerPath, policyPath, scopePath); err != nil {
		t.Fatal(err)
	}
	return SurfaceLocalProofPlanRequest{
		ScopePath: scopePath, SourceProfilePath: profilePath, LedgerPath: ledgerPath, PolicyPath: policyPath, FixtureRoot: fixtureRoot,
		ProfilePath: filepath.Join(root, "local-profile.json"), UsagePath: filepath.Join(root, "local-usage.json"), LocalDecisionPath: filepath.Join(root, "local-decision.json"), ManifestPath: filepath.Join(root, "manifest.json"), CoveragePath: filepath.Join(root, "coverage.json"),
	}
}
