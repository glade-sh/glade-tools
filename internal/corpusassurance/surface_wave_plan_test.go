package corpusassurance

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/surfaceledger"
)

type surfaceWaveFixtureDefinition struct {
	name        string
	surfaceIDs  []string
	disposition string
	ineligible  bool
}

func TestBuildSurfaceWavePlanSelectsWholeFixturesInDeterministicOrder(t *testing.T) {
	request, _, _ := surfaceWavePlanRequest(t)
	plan, err := BuildSurfaceWavePlan(request)
	if err != nil {
		t.Fatalf("BuildSurfaceWavePlan: %v", err)
	}
	if plan.SelectedFixtures != 2 || plan.SelectedRows != 2 || plan.RemainingOpen != 0 || len(plan.Shards) != 2 {
		t.Fatalf("plan counts = %#v", plan)
	}
	if got := plan.Shards[0].Fixtures[0].ID; got != "runtime" {
		t.Fatalf("first fixture = %q, want runtime", got)
	}
	if got := plan.Shards[1].Fixtures[0].ID; got != "mock" {
		t.Fatalf("second fixture = %q, want mock", got)
	}
	if plan.ScopeSHA256 != localProofFileSHA256(t, request.ScopePath) || plan.ProfileSHA256 != localProofFileSHA256(t, request.ProfilePath) || plan.LocalProofSHA256 != localProofFileSHA256(t, request.LocalProofPath) || plan.FixtureManifestSHA256 != localProofFileSHA256(t, request.FixtureManifestPath) || plan.CoverageSHA256 != localProofFileSHA256(t, request.CoveragePath) {
		t.Fatalf("plan bindings = %#v", plan)
	}
}

func TestBuildSurfaceWavePlanConsumesCanonicalTerminalAuthority(t *testing.T) {
	planRequest := surfaceLocalProofPlanRequest(t)
	localProofFixture(t, planRequest.FixtureRoot, "runtime", []string{"apex:System.Runtime.run()"}, localRuntimeRequired)
	if _, _, err := BuildSurfaceLocalProofPlan(planRequest); err == nil {
		t.Fatal("incomplete full-scope plan unexpectedly passed")
	}
	classificationPath := filepath.Join(filepath.Dir(planRequest.CoveragePath), "terminal-classifications.json")
	writeLocalProofJSON(t, classificationPath, ExclusionPolicy{SchemaVersion: 1, Rows: []ExclusionPolicyRow{{SurfaceID: "apex:System.Mock.run()", Class: terminalHostedContext, Reason: "requires hosted context"}}})
	authorityPath := filepath.Join(filepath.Dir(planRequest.CoveragePath), "terminal-authority.json")
	if _, err := CreateSurfaceTerminalAuthority(SurfaceTerminalAuthorityRequest{ScopePath: planRequest.ScopePath, CoveragePath: planRequest.CoveragePath, LedgerPath: planRequest.LedgerPath, SupportPolicyPath: planRequest.PolicyPath, FixtureRoot: planRequest.FixtureRoot, ClassificationPath: classificationPath, OutputPath: authorityPath}); err != nil {
		t.Fatal(err)
	}
	planRequest.TerminalAuthorityPath = authorityPath
	planRequest.ProfilePath += ".terminal"
	planRequest.UsagePath += ".terminal"
	planRequest.LocalDecisionPath += ".terminal"
	planRequest.ManifestPath += ".terminal"
	planRequest.CoveragePath += ".terminal"
	if _, _, err := BuildSurfaceLocalProofPlan(planRequest); err != nil {
		t.Fatal(err)
	}
	request, _, _ := surfaceWaveRequestFromLocalPlan(t, planRequest)
	request.TerminalAuthorityPath = authorityPath
	plan, err := BuildSurfaceWavePlan(request)
	if err != nil {
		t.Fatal(err)
	}
	if plan.SelectedFixtures != 1 || plan.SelectedRows != 1 || len(plan.Shards) != 2 || plan.Shards[0].Fixtures[0].ID != "runtime" || len(plan.Shards[1].Fixtures) != 0 || plan.TerminalAuthoritySHA256 != localProofFileSHA256(t, authorityPath) {
		t.Fatalf("terminal plan = %#v", plan)
	}
}

func TestBuildSurfaceWavePlanSelectsOnlyPredecessorOpenRows(t *testing.T) {
	request, proof, scope := surfaceWavePlanRequest(t)
	predecessorPath := filepath.Join(filepath.Dir(request.OutputPath), "SURFACE_ORACLE_INDEX.json")
	writeSurfaceWavePredecessor(t, predecessorPath, request, proof, scope, map[string]string{"apex:Mock.run": "open", "apex:Runtime.run": "matched"})
	request.PredecessorIndexPath = predecessorPath
	request.OutputPath = filepath.Join(filepath.Dir(request.OutputPath), "NEXT_SURFACE_WAVE_PLAN.json")

	plan, err := BuildSurfaceWavePlan(request)
	if err != nil {
		t.Fatalf("BuildSurfaceWavePlan: %v", err)
	}
	if plan.SelectedFixtures != 1 || plan.SelectedRows != 1 || plan.RemainingOpen != 0 || len(plan.Shards) != 2 || plan.Shards[0].Fixtures[0].ID != "mock" || len(plan.Shards[1].Fixtures) != 0 {
		t.Fatalf("successor plan = %#v", plan)
	}
}

func TestBuildSurfaceWavePlanAccountsForIneligibleLocalRows(t *testing.T) {
	request, _, _ := buildSurfaceWavePlanRequest(t, []surfaceWaveFixtureDefinition{
		{name: "eligible", surfaceIDs: []string{"apex:Eligible.run"}, disposition: localRuntimeRequired},
		{name: "local-only", surfaceIDs: []string{"apex:LocalOnly.run"}, disposition: localRuntimeRequired, ineligible: true},
	}, nil)
	plan, err := BuildSurfaceWavePlan(request)
	if err != nil {
		t.Fatal(err)
	}
	if plan.EligibleRows != 1 || plan.IneligibleRows != 1 || plan.SelectedFixtures != 1 || plan.SelectedRows != 1 || plan.RemainingOpen != 0 {
		t.Fatalf("eligibility accounting = %#v", plan)
	}
}

func TestBuildSurfaceWavePlanRejectsSplitAndOutOfScopeFixtures(t *testing.T) {
	t.Run("predecessor split", func(t *testing.T) {
		definitions := []surfaceWaveFixtureDefinition{{name: "split", surfaceIDs: []string{"apex:Split.extra", "apex:Split.run"}, disposition: localRuntimeRequired}}
		request, proof, scope := buildSurfaceWavePlanRequest(t, definitions, nil)
		predecessorPath := filepath.Join(filepath.Dir(request.OutputPath), "SURFACE_ORACLE_INDEX.json")
		writeSurfaceWavePredecessor(t, predecessorPath, request, proof, scope, map[string]string{"apex:Split.extra": "matched", "apex:Split.run": "open"})
		request.PredecessorIndexPath = predecessorPath
		if _, err := BuildSurfaceWavePlan(request); err == nil || !strings.Contains(err.Error(), "splits fixture") {
			t.Fatalf("split fixture error = %v", err)
		}
	})

	t.Run("out of scope", func(t *testing.T) {
		definitions := []surfaceWaveFixtureDefinition{{name: "split", surfaceIDs: []string{"apex:Split.extra", "apex:Split.run"}, disposition: localRuntimeRequired}}
		request, proof, _ := buildSurfaceWavePlanRequest(t, definitions, map[string]bool{"apex:Split.run": true})
		manifest, _, err := readExactJSONBytes[LocalProofFixtureManifest](request.FixtureManifestPath)
		if err != nil {
			t.Fatal(err)
		}
		manifest.Fixtures[0].OwnedSurfaceIDs = definitions[0].surfaceIDs
		manifest.SalesforceFixtures[0].OwnedSurfaceIDs = definitions[0].surfaceIDs
		writeLocalProofJSON(t, request.FixtureManifestPath, manifest)
		proof.FixtureManifestSHA256 = localProofFileSHA256(t, request.FixtureManifestPath)
		writeLocalProofJSON(t, request.LocalProofPath, proof)
		if _, err := BuildSurfaceWavePlan(request); err == nil || !strings.Contains(err.Error(), "out-of-scope") {
			t.Fatalf("out-of-scope fixture error = %v", err)
		}
	})
}

func TestBuildSurfaceWavePlanDefaultsToTwoSixteenFixtureShards(t *testing.T) {
	definitions := make([]surfaceWaveFixtureDefinition, 33)
	for i := range definitions {
		name := fmt.Sprintf("fixture%02d", i)
		definitions[i] = surfaceWaveFixtureDefinition{name: name, surfaceIDs: []string{"apex:" + strings.Title(name) + ".run"}, disposition: localRuntimeRequired}
	}
	request, _, _ := buildSurfaceWavePlanRequest(t, definitions, nil)
	request.MaxFixtures = 0
	plan, err := BuildSurfaceWavePlan(request)
	if err != nil {
		t.Fatal(err)
	}
	if plan.MaxFixtures != 32 || plan.SelectedFixtures != 32 || plan.SelectedRows != 32 || plan.RemainingOpen != 1 || len(plan.Shards) != 2 || len(plan.Shards[0].Fixtures) != 16 || len(plan.Shards[1].Fixtures) != 16 {
		t.Fatalf("default shard plan = %#v", plan)
	}
}

func TestBuildSurfaceWavePlanBalancesFixturesAcrossNineShards(t *testing.T) {
	definitions := make([]surfaceWaveFixtureDefinition, 18)
	for i := range definitions {
		name := fmt.Sprintf("fixture%02d", i)
		definitions[i] = surfaceWaveFixtureDefinition{name: name, surfaceIDs: []string{"apex:" + strings.Title(name) + ".run"}, disposition: localRuntimeRequired}
	}
	request, _, _ := buildSurfaceWavePlanRequest(t, definitions, nil)
	request.ShardCount = 9
	plan, err := BuildSurfaceWavePlan(request)
	if err != nil {
		t.Fatal(err)
	}
	if plan.ShardCount != 9 || len(plan.Shards) != 9 {
		t.Fatalf("shard count = %d/%d, want 9/9", plan.ShardCount, len(plan.Shards))
	}
	for _, shard := range plan.Shards {
		if len(shard.Fixtures) != 2 || len(shard.SurfaceIDs) != 2 {
			t.Fatalf("unbalanced shard = %#v", shard)
		}
	}
}

func TestBuildSurfaceWavePlanIsCreateOnly(t *testing.T) {
	request, _, _ := surfaceWavePlanRequest(t)
	if _, err := BuildSurfaceWavePlan(request); err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(request.OutputPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := BuildSurfaceWavePlan(request); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("second write error = %v", err)
	}
	got, err := os.ReadFile(request.OutputPath)
	if err != nil || string(got) != string(want) {
		t.Fatalf("create-only output changed: err=%v", err)
	}
}

func surfaceWavePlanRequest(t *testing.T) (SurfaceWavePlanRequest, LocalProof, SurfaceOracleScope) {
	return buildSurfaceWavePlanRequest(t, []surfaceWaveFixtureDefinition{
		{name: "runtime", surfaceIDs: []string{"apex:Runtime.run"}, disposition: localRuntimeRequired},
		{name: "mock", surfaceIDs: []string{"apex:Mock.run"}, disposition: deterministicMockRequired},
	}, nil)
}

func buildSurfaceWavePlanRequest(t *testing.T, definitions []surfaceWaveFixtureDefinition, scopeOnly map[string]bool) (SurfaceWavePlanRequest, LocalProof, SurfaceOracleScope) {
	t.Helper()
	root := t.TempDir()
	fixtureRoot := filepath.Join(root, "fixtures")
	if err := os.Mkdir(fixtureRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	ledger := surfaceledger.SurfaceLedger{SchemaVersion: surfaceledger.SchemaVersion, Summary: surfaceledger.LedgerSummary{Gaps: map[string]int{}, Failures: map[string]int{}}}
	policy := surfaceledger.SupportPolicy{}
	for _, definition := range definitions {
		localProofFixtureWithEligibility(t, fixtureRoot, definition.name, definition.surfaceIDs, definition.disposition, !definition.ineligible)
		for _, surfaceID := range definition.surfaceIDs {
			if scopeOnly != nil && !scopeOnly[surfaceID] {
				continue
			}
			ledger.Rows = append(ledger.Rows, surfaceledger.SurfaceLedgerRow{SurfaceID: surfaceID, Product: surfaceledger.ProductApex, Namespace: surfaceWaveSurfaceNamespace(surfaceID)})
			policy.Rules = append(policy.Rules, surfaceledger.SupportPolicyRule{SurfaceID: surfaceID, Disposition: surfaceledger.SupportDisposition(definition.disposition), Reason: "wave test"})
		}
	}
	ledgerPath, policyPath := filepath.Join(root, "ledger.json"), filepath.Join(root, "policy.json")
	writeLocalProofJSON(t, ledgerPath, ledger)
	writeLocalProofJSON(t, policyPath, policy)
	profile := surfaceledger.ComputeSupportProfile(ledger.Rows, policy, nil)
	profile.Inputs = &surfaceledger.SupportProfileInputs{Files: []surfaceledger.SupportProfileInput{
		{Name: "ledger", Path: ledgerPath, SHA256: sha256FileForTest(t, ledgerPath)},
		{Name: "policy", Path: policyPath, SHA256: sha256FileForTest(t, policyPath)},
	}}
	sourceProfilePath := filepath.Join(root, "source-profile.json")
	writeLocalProofJSON(t, sourceProfilePath, profile)
	scopePath := filepath.Join(root, "scope.json")
	if _, err := BuildSurfaceOracleScope(sourceProfilePath, ledgerPath, policyPath, scopePath); err != nil {
		t.Fatal(err)
	}
	planRequest := SurfaceLocalProofPlanRequest{
		ScopePath: scopePath, SourceProfilePath: sourceProfilePath, LedgerPath: ledgerPath, PolicyPath: policyPath, FixtureRoot: fixtureRoot,
		ProfilePath: filepath.Join(root, "local-profile.json"), UsagePath: filepath.Join(root, "local-usage.json"), LocalDecisionPath: filepath.Join(root, "local-decision.json"), ManifestPath: filepath.Join(root, "manifest.json"), CoveragePath: filepath.Join(root, "coverage.json"),
	}
	if _, _, err := BuildSurfaceLocalProofPlan(planRequest); err != nil {
		t.Fatal(err)
	}
	return surfaceWaveRequestFromLocalPlan(t, planRequest)
}

func surfaceWaveRequestFromLocalPlan(t *testing.T, planRequest SurfaceLocalProofPlanRequest) (SurfaceWavePlanRequest, LocalProof, SurfaceOracleScope) {
	t.Helper()
	proofRequest, _ := localProofRequest(t)
	proofRequest.ProfilePath = planRequest.ProfilePath
	proofRequest.UsagePath = planRequest.UsagePath
	proofRequest.DecisionPath = planRequest.LocalDecisionPath
	proofRequest.FixtureManifestPath = planRequest.ManifestPath
	proofRequest.OutputPath = filepath.Join(filepath.Dir(planRequest.ProfilePath), "LOCAL_PROOF.json")
	proof, err := RunLocalProof(proofRequest)
	if err != nil {
		t.Fatal(err)
	}
	scope, _, err := readExactJSONBytes[SurfaceOracleScope](planRequest.ScopePath)
	if err != nil {
		t.Fatal(err)
	}
	return SurfaceWavePlanRequest{
		ScopePath: planRequest.ScopePath, ProfilePath: planRequest.ProfilePath, LocalProofPath: proofRequest.OutputPath,
		FixtureManifestPath: planRequest.ManifestPath, CoveragePath: planRequest.CoveragePath, MaxFixtures: 32,
		OutputPath: filepath.Join(filepath.Dir(planRequest.ProfilePath), "SURFACE_WAVE_PLAN.json"),
	}, proof, scope
}

func writeSurfaceWavePredecessor(t *testing.T, path string, request SurfaceWavePlanRequest, proof LocalProof, scope SurfaceOracleScope, states map[string]string) {
	t.Helper()
	rows := make([]SurfaceOracleIndexRow, len(scope.Rows))
	matched := []string{}
	for i, row := range scope.Rows {
		rows[i] = SurfaceOracleIndexRow{SurfaceID: row.SurfaceID, State: states[row.SurfaceID]}
		if states[row.SurfaceID] == "matched" {
			matched = append(matched, row.SurfaceID)
		}
	}
	batches := []SurfaceOracleIndexRuntimeBatch{}
	if len(matched) != 0 {
		batches = append(batches, SurfaceOracleIndexRuntimeBatch{
			ManifestSHA256: strings.Repeat("6", 64), ProfileSHA256: strings.Repeat("7", 64), BindingsSHA256: strings.Repeat("8", 64), LocalSummarySHA256: strings.Repeat("9", 64),
			OracleResultsSHA256: strings.Repeat("a", 64), RawReconciliationSHA256: strings.Repeat("b", 64), MismatchReviewSHA256: strings.Repeat("c", 64), FinalAuditSHA256: strings.Repeat("d", 64), SurfaceIDs: matched,
		})
	}
	index := SurfaceOracleIndex{
		SchemaVersion: 1, Kind: "all-runtime", ScopeSHA256: localProofFileSHA256(t, request.ScopePath), SourceProfileSHA256: scope.SourceProfileSHA256, LedgerSHA256: scope.LedgerSHA256, PolicySHA256: scope.PolicySHA256,
		Candidate: SurfaceOracleIndexArtifact{Commit: proof.Candidate.Commit, BinarySHA256: proof.Candidate.SHA256}, Tools: SurfaceOracleIndexArtifact{Commit: proof.Tools.Commit, BinarySHA256: proof.Tools.SHA256},
		RuntimeBatches: batches, Total: len(rows), Rows: rows, Counts: surfaceOracleIndexCounts(rows),
	}
	writeLocalProofJSON(t, path, index)
}
