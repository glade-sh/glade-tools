package corpusassurance

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/surfaceledger"
)

func TestPlanOracleClassifiesEverySupportedDisposition(t *testing.T) {
	plan, err := planOracle([]OracleInputRow{
		{SurfaceID: "apex:System.runtime", Disposition: localRuntimeRequired, RuntimeObserved: true},
		{SurfaceID: "apex:System.compile", Disposition: compileShapeRequired, CompilePassed: true},
		{SurfaceID: "apex:System.mock", Disposition: deterministicMockRequired, BehaviorObserved: true, Deployable: true},
		{SurfaceID: "apex:ConnectApi.mock", Disposition: deterministicMockRequired, BehaviorObserved: true, ExclusionClass: "nonportable-mock", ExclusionReason: "requires hosted identity"},
		{SurfaceID: "apex:Auth.hosted", Disposition: "hosted-deferred", ExclusionClass: "hosted-identity", ExclusionReason: "requires org credentials"},
		{SurfaceID: "apex:Unknown.surface", Disposition: "unknown"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"apex:System.runtime":  "runtime",
		"apex:System.compile":  "compile",
		"apex:System.mock":     "compile",
		"apex:ConnectApi.mock": "local-contract-only",
		"apex:Auth.hosted":     "waiver",
		"apex:Unknown.surface": "unknown",
	}
	if len(plan.Rows) != len(want) {
		t.Fatalf("row count = %d, want %d", len(plan.Rows), len(want))
	}
	for _, row := range plan.Rows {
		if row.Action != want[row.SurfaceID] {
			t.Errorf("%s action = %q, want %q", row.SurfaceID, row.Action, want[row.SurfaceID])
		}
		if row.Action == "local-contract-only" || row.Action == "waiver" {
			if row.ExclusionClass == "" || row.ExclusionReason == "" {
				t.Errorf("%s omission lacks an exclusion", row.SurfaceID)
			}
		}
	}
}

func TestPlanOracleRejectsMissingEvidenceAndUnjustifiedExclusions(t *testing.T) {
	for _, row := range []OracleInputRow{
		{SurfaceID: "apex:System.runtime", Disposition: localRuntimeRequired},
		{SurfaceID: "apex:System.mock", Disposition: deterministicMockRequired, BehaviorObserved: true, ExclusionClass: "nonportable-mock"},
		{SurfaceID: "apex:Auth.hosted", Disposition: "hosted-deferred", ExclusionReason: "requires credentials"},
	} {
		if _, err := planOracle([]OracleInputRow{row}); err == nil {
			t.Fatalf("planOracle accepted %#v", row)
		}
	}
}

func TestOracleBundleFixtureSelectionDerivesOnlySalesforceRequiredOwnedFixtures(t *testing.T) {
	eligible := true
	plan := OraclePlan{
		Candidate: RuntimeArtifact{Commit: strings.Repeat("a", 40), OS: "darwin", Arch: "arm64", SHA256: strings.Repeat("b", 64)},
		Tools:     RuntimeArtifact{Commit: strings.Repeat("c", 40), OS: "darwin", Arch: "amd64", SHA256: strings.Repeat("d", 64)},
		Rows: []OraclePlanRow{
			{SurfaceID: "apex:System.compile()", Action: oracleCompile},
			{SurfaceID: "apex:System.run()", Action: oracleRuntime},
			{SurfaceID: "apex:Hosted.only", Action: oracleWaiver, ExclusionClass: "hosted", ExclusionReason: "identity"},
		},
	}
	manifest := LocalProofFixtureManifest{Fixtures: []LocalProofFixture{
		{ID: "system", Name: "system", Path: "system.json", SHA256: strings.Repeat("e", 64), OwnedSurfaceIDs: []string{"apex:System.run()"}, Disposition: localRuntimeRequired, Operation: "exec", SalesforceEligible: &eligible},
		{ID: "compiler", Name: "compiler", Path: "compiler.json", SHA256: strings.Repeat("a", 64), OwnedSurfaceIDs: []string{"apex:System.compile()"}, Disposition: compileShapeRequired, Operation: "check", SalesforceEligible: &eligible},
		{ID: "hosted", Name: "hosted", Path: "hosted.json", SHA256: strings.Repeat("f", 64), OwnedSurfaceIDs: []string{"apex:Hosted.only"}, Disposition: compileShapeRequired},
	}, SalesforceFixtures: []LocalProofFixture{
		{ID: "system", Name: "system", Path: "system.json", SHA256: strings.Repeat("e", 64), OwnedSurfaceIDs: []string{"apex:System.run()"}, Disposition: localRuntimeRequired, Operation: "exec", SalesforceEligible: &eligible},
		{ID: "compiler", Name: "compiler", Path: "compiler.json", SHA256: strings.Repeat("a", 64), OwnedSurfaceIDs: []string{"apex:System.compile()"}, Disposition: compileShapeRequired, Operation: "check", SalesforceEligible: &eligible},
	}}
	fixtures, err := oracleBundleFixtures(plan, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if len(fixtures) != 2 || fixtures[0].ID != "compiler" || fixtures[1].ID != "system" || !reflect.DeepEqual(fixtures[1].SurfaceIDs, []string{"apex:System.run()"}) {
		t.Fatalf("fixtures = %#v", fixtures)
	}
}

func TestOracleBundleFixtureSelectionRejectsLocalFixturePromotion(t *testing.T) {
	eligible := true
	plan := OraclePlan{Candidate: RuntimeArtifact{Commit: strings.Repeat("a", 40), OS: "darwin", Arch: "arm64", SHA256: strings.Repeat("b", 64)}, Tools: RuntimeArtifact{Commit: strings.Repeat("c", 40), OS: "darwin", Arch: "arm64", SHA256: strings.Repeat("d", 64)}, Rows: []OraclePlanRow{{SurfaceID: "apex:System.run()", Action: oracleRuntime}}}
	local := LocalProofFixture{ID: "system", Name: "system", Path: "system.json", SHA256: strings.Repeat("a", 64), OwnedSurfaceIDs: []string{"apex:System.run()"}, Disposition: localRuntimeRequired, Operation: "exec", SalesforceEligible: &eligible}
	if _, err := oracleBundleFixtures(plan, LocalProofFixtureManifest{Fixtures: []LocalProofFixture{local}}); err == nil {
		t.Fatal("oracleBundleFixtures promoted a local fixture without a separate Salesforce declaration")
	}
	manifest := LocalProofFixtureManifest{Fixtures: []LocalProofFixture{local}, SalesforceFixtures: []LocalProofFixture{{ID: local.ID, Name: local.Name, Path: local.Path, SHA256: local.SHA256, OwnedSurfaceIDs: local.OwnedSurfaceIDs, Disposition: local.Disposition, Operation: local.Operation, SalesforceEligible: &eligible}}}
	fixtures, err := oracleBundleFixtures(plan, manifest)
	if err != nil || len(fixtures) != 1 || fixtures[0].ID != local.ID {
		t.Fatalf("oracleBundleFixtures = %#v, %v", fixtures, err)
	}
}

func TestPlanOracleForUsageAllowsExplicitLocalOnlyDirective(t *testing.T) {
	plan, err := planOracleForUsage(
		UsageReconciliation{Usage: []ReconciledUsageEntry{{UsageEntry: UsageEntry{UsageKey: "System.run"}, Class: usageClassExact, SurfaceID: "apex:System.run()"}}},
		[]OracleProfileRow{{SurfaceID: "apex:System.run()", Disposition: localRuntimeRequired}},
		LocalProof{Surfaces: []LocalSurfaceProof{{SurfaceID: "apex:System.run()", Disposition: localRuntimeRequired, RuntimeObserved: true}}},
		[]OracleDirective{{SurfaceID: "apex:System.run()", ExclusionClass: "policy-local-only", ExclusionReason: "portable hosted execution is not applicable"}},
	)
	if err != nil || len(plan.Rows) != 1 || plan.Rows[0].Action != oracleLocalContractOnly {
		t.Fatalf("planOracleForUsage = %#v, %v", plan, err)
	}
}

func TestBuildOracleBundleStagesOnlySealedDerivedTransportInputs(t *testing.T) {
	inputs := oracleBundleTestInputsForLocalProof(t)
	writeSealedReleaseValidation(t, inputs, inputs.attemptPath)
	root := filepath.Dir(inputs.releasePath)
	outputPath := filepath.Join(root, "salesforce-worker")
	bundle, err := BuildOracleBundle(inputs.request(outputPath))
	if err != nil {
		t.Fatalf("BuildOracleBundle: %v", err)
	}
	if len(bundle.Fixtures) != 1 || bundle.Fixtures[0].ID != "runtime" || bundle.TransportManifestSHA256 == "" || bundle.LocalProofSummarySHA256 == "" {
		t.Fatalf("bundle = %#v", bundle)
	}
	for _, path := range []string{filepath.Join(outputPath, "bundle", "bundle.json"), filepath.Join(outputPath, "bundle", "profile.json"), filepath.Join(outputPath, "bundle", "fixture-manifest.json"), filepath.Join(outputPath, "bundle", "LOCAL_PROOF_SUMMARY.json"), filepath.Join(outputPath, "transport", "salesforce-first-filter.py"), filepath.Join(outputPath, "bin", "glade-tools-darwin-amd64")} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("missing staged bundle input %s: %v", path, err)
		}
	}
	bundlePath := filepath.Join(outputPath, "bundle", "bundle.json")
	if err := ValidateOracleBundle(bundlePath); err != nil {
		t.Fatalf("ValidateOracleBundle: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outputPath, "bundle", "profile.json"), []byte(`{"tampered":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateOracleBundle(bundlePath); err == nil {
		t.Fatal("accepted a bundle whose staged profile changed")
	}
	wrongToolsRequest := inputs.request(filepath.Join(root, "wrong-salesforce-worker"))
	wrongToolsRequest.ToolsRoot = newInventoryRepository(t, map[string]string{"go.mod": "module example.com/wrong\n\ngo 1.23.0\n", "cmd/glade-tools/main.go": "package main\nfunc main() {}\n"})
	if _, err := BuildOracleBundle(wrongToolsRequest); err == nil {
		t.Fatal("accepted a tools source root that does not match the sealed tools artifact")
	}
}

func TestAuthorizeExclusionsRequiresExactNonParityPolicy(t *testing.T) {
	plan := OraclePlan{Rows: []OraclePlanRow{
		{SurfaceID: "apex:ConnectApi.mock", Action: oracleLocalContractOnly, ExclusionClass: "nonportable-mock", ExclusionReason: "requires hosted identity"},
		{SurfaceID: "apex:Auth.hosted", Action: oracleWaiver, ExclusionClass: "hosted-identity", ExclusionReason: "requires org credentials"},
		{SurfaceID: "apex:System.runtime", Action: oracleRuntime},
	}}
	authority, err := authorizeExclusions(plan, ExclusionPolicy{SchemaVersion: 1, Rows: []ExclusionPolicyRow{
		{SurfaceID: "apex:Auth.hosted", Class: "hosted-identity", Reason: "requires org credentials"},
		{SurfaceID: "apex:ConnectApi.mock", Class: "nonportable-mock", Reason: "requires hosted identity"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(authority.Rows) != 2 || authority.Rows[0].SurfaceID != "apex:Auth.hosted" || authority.Rows[1].SurfaceID != "apex:ConnectApi.mock" {
		t.Fatalf("authority = %#v", authority)
	}
}

func TestAuthorizeExclusionsRejectsMissingAndMismatchedRows(t *testing.T) {
	plan := OraclePlan{Rows: []OraclePlanRow{{SurfaceID: "apex:Auth.hosted", Action: oracleWaiver, ExclusionClass: "hosted-identity", ExclusionReason: "requires credentials"}}}
	for _, policy := range []ExclusionPolicy{
		{SchemaVersion: 1},
		{SchemaVersion: 1, Rows: []ExclusionPolicyRow{{SurfaceID: "apex:Auth.hosted", Class: "hosted-identity", Reason: "different"}}},
		{SchemaVersion: 1, Rows: []ExclusionPolicyRow{{SurfaceID: "apex:Auth.hosted", Class: "hosted-identity", Reason: "requires credentials"}, {SurfaceID: "apex:Auth.hosted", Class: "hosted-identity", Reason: "requires credentials"}}},
	} {
		if _, err := authorizeExclusions(plan, policy); err == nil {
			t.Fatalf("authorizeExclusions accepted %#v", policy)
		}
	}
}

func TestPlanOracleForUsageRequiresReconciledProfileAndLocalEvidence(t *testing.T) {
	reconciled := UsageReconciliation{Usage: []ReconciledUsageEntry{
		{UsageEntry: UsageEntry{UsageKey: "System.run", PrivateProdRefs: 1}, Class: usageClassExact, SurfaceID: "apex:System.run()"},
		{UsageEntry: UsageEntry{UsageKey: "ConnectApi.fetch", PrivateProdRefs: 1}, Class: usageClassCanonicalAlias, SurfaceID: "apex:ConnectApi.fetch()"},
		{UsageEntry: UsageEntry{UsageKey: "App.Local", PrivateProdRefs: 1}, Class: usageClassLocalSymbol, Reason: "application helper"},
	}}
	profile := []OracleProfileRow{
		{SurfaceID: "apex:System.run()", Disposition: localRuntimeRequired},
		{SurfaceID: "apex:ConnectApi.fetch()", Disposition: deterministicMockRequired},
	}
	proof := LocalProof{Surfaces: []LocalSurfaceProof{
		{SurfaceID: "apex:System.run()", Disposition: localRuntimeRequired, RuntimeObserved: true},
		{SurfaceID: "apex:ConnectApi.fetch()", Disposition: deterministicMockRequired, BehaviorObserved: true},
	}}
	plan, err := planOracleForUsage(reconciled, profile, proof, []OracleDirective{{SurfaceID: "apex:ConnectApi.fetch()", ExclusionClass: "nonportable-mock", ExclusionReason: "requires hosted identity"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Rows) != 2 || plan.Rows[0].Action != oracleLocalContractOnly || plan.Rows[1].Action != oracleRuntime {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestPlanOracleForUsageRejectsMissingOrMismatchedEvidence(t *testing.T) {
	reconciled := UsageReconciliation{Usage: []ReconciledUsageEntry{{UsageEntry: UsageEntry{UsageKey: "System.run", PrivateProdRefs: 1}, Class: usageClassExact, SurfaceID: "apex:System.run()"}}}
	profile := []OracleProfileRow{{SurfaceID: "apex:System.run()", Disposition: localRuntimeRequired}}
	if _, err := planOracleForUsage(reconciled, profile, LocalProof{}, nil); err == nil {
		t.Fatal("planOracleForUsage accepted missing local proof")
	}
	if _, err := planOracleForUsage(reconciled, profile, LocalProof{Surfaces: []LocalSurfaceProof{{SurfaceID: "apex:System.run()", Disposition: compileShapeRequired, CompilePassed: true}}}, nil); err == nil {
		t.Fatal("planOracleForUsage accepted mismatched proof disposition")
	}
}

func TestPlanOracleForUsageRejectsUndirectedMock(t *testing.T) {
	reconciled := UsageReconciliation{Usage: []ReconciledUsageEntry{{UsageEntry: UsageEntry{UsageKey: "Cache.read", PrivateProdRefs: 1}, Class: usageClassExact, SurfaceID: "apex:Cache.read()"}}}
	profile := []OracleProfileRow{{SurfaceID: "apex:Cache.read()", Disposition: deterministicMockRequired}}
	proof := LocalProof{Surfaces: []LocalSurfaceProof{{SurfaceID: "apex:Cache.read()", Disposition: deterministicMockRequired, BehaviorObserved: true}}}
	if _, err := planOracleForUsage(reconciled, profile, proof, nil); err == nil {
		t.Fatal("planOracleForUsage accepted an undirected deterministic mock")
	}
}

func TestPlanOracleFromFilesBindsFreshInputs(t *testing.T) {
	root := t.TempDir()
	profilePath := filepath.Join(root, "ASSURANCE_PROFILE.json")
	sealedUsagePath := filepath.Join(root, "CORPUS_USAGE.json")
	fixtureManifestPath := filepath.Join(root, "fixtures.json")
	proofPath := filepath.Join(root, "proof.json")
	directivePath := filepath.Join(root, "directives.json")
	outputPath := filepath.Join(root, "ORACLE_PLAN.json")
	sealedUsage := SealedCorpusUsage{SchemaVersion: 1, ProfileSHA256: strings.Repeat("e", 64), LedgerSHA256: strings.Repeat("f", 64), PolicySHA256: strings.Repeat("1", 64), Reconciliation: UsageReconciliation{Usage: []ReconciledUsageEntry{{UsageEntry: UsageEntry{UsageKey: "System.run", PrivateProdRefs: 1}, Class: usageClassExact, SurfaceID: "apex:System.run()"}}}}
	if err := WriteNewJSON(sealedUsagePath, sealedUsage); err != nil {
		t.Fatal(err)
	}
	candidate := RuntimeArtifact{Commit: strings.Repeat("a", 40), OS: "darwin", Arch: "arm64", SHA256: strings.Repeat("b", 64)}
	tools := RuntimeArtifact{Commit: strings.Repeat("c", 40), OS: "darwin", Arch: "arm64", SHA256: strings.Repeat("d", 64)}
	attemptPath := assuranceAttemptForRuntimes(t, root, candidate, tools)
	attempt, err := LoadAssuranceAttempt(attemptPath)
	if err != nil {
		t.Fatal(err)
	}
	fixture := localProofFixture(t, root, "system", []string{"apex:System.run()"}, localRuntimeRequired)
	if err := WriteNewJSON(fixtureManifestPath, LocalProofFixtureManifest{Fixtures: []LocalProofFixture{fixture}, SalesforceFixtures: []LocalProofFixture{fixture}}); err != nil {
		t.Fatal(err)
	}
	definition, err := loadLocalProofFixture(fixture)
	if err != nil {
		t.Fatal(err)
	}
	command, err := localProofCommandForFixture(fixture, definition, "", ".")
	if err != nil {
		t.Fatal(err)
	}
	stdout := `{"status":"passed","exitCode":0}`
	receipt := CommandResult{Command: []string{"exec"}, ExecutableSHA256: candidate.SHA256, ExecutableAfterSHA256: candidate.SHA256, CommandSpecSHA256: localProofReceiptSpecSHA256(command, candidate.SHA256), ExitCode: 0, DurationMS: 1, StdoutSHA256: replayBytesSHA256([]byte(stdout)), StderrSHA256: replayBytesSHA256(nil), Passed: true}
	proof := LocalProof{Status: "pass", AttemptSHA256: attemptHash(attempt), AttemptPath: attemptPath, Candidate: candidate, Tools: tools, ProfileSHA256: strings.Repeat("b", 64), UsageSHA256: strings.Repeat("c", 64), DecisionSHA256: strings.Repeat("d", 64), FixtureManifestSHA256: localProofFileSHA256(t, fixtureManifestPath), SelectedSurfaceIDs: []string{"apex:System.run()"}, RawFixtureResults: []LocalProofFixtureResult{{FixtureID: "system", FixtureSHA256: fixture.SHA256, Disposition: localRuntimeRequired, CandidateSHA256: candidate.SHA256, ToolsSHA256: tools.SHA256, Receipt: receipt, Operation: "exec", StdoutSHA256: receipt.StdoutSHA256, Stdout: stdout, StderrSHA256: receipt.StderrSHA256}}, Surfaces: []LocalSurfaceProof{{SurfaceID: "apex:System.run()", FixtureID: "system", FixtureSHA256: fixture.SHA256, Disposition: localRuntimeRequired, CandidateSHA256: candidate.SHA256, ToolsSHA256: tools.SHA256, RuntimeObserved: true}}}
	if err := WriteNewJSON(proofPath, proof); err != nil {
		t.Fatal(err)
	}
	profile := AssuranceProfile{SchemaVersion: 1, SourceProfileSHA256: strings.Repeat("e", 64), SealedUsageSHA256: localProofFileSHA256(t, sealedUsagePath), LedgerSHA256: strings.Repeat("f", 64), PolicySHA256: strings.Repeat("1", 64), FixtureManifestSHA256: localProofFileSHA256(t, fixtureManifestPath), LocalProofSHA256: localProofFileSHA256(t, proofPath), Total: 1, ByDisposition: map[string]int{localRuntimeRequired: 1}, NonDeferredGaps: []AssuranceProfileRow{{SurfaceID: "apex:System.run()", Disposition: localRuntimeRequired}}, Rows: []AssuranceProfileRow{{SurfaceID: "apex:System.run()", Disposition: localRuntimeRequired}}}
	if err := WriteNewJSON(profilePath, profile); err != nil {
		t.Fatal(err)
	}
	directives := OracleDirectiveFile{SchemaVersion: 1, ProfileSHA256: strings.Repeat("e", 64), SealedUsageSHA256: localProofFileSHA256(t, sealedUsagePath), LocalProofSHA256: localProofFileSHA256(t, proofPath)}
	if err := WriteNewJSON(directivePath, directives); err != nil {
		t.Fatal(err)
	}
	plan, err := PlanOracleFromFiles(profilePath, sealedUsagePath, fixtureManifestPath, proofPath, directivePath, outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Rows) != 1 || plan.Rows[0].Action != oracleRuntime || plan.ProfileSHA256 != localProofFileSHA256(t, profilePath) || plan.Candidate != candidate || plan.Tools != tools || localProofFileSHA256(t, outputPath) == "" {
		t.Fatalf("plan = %#v", plan)
	}
	if err := os.Remove(outputPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(profilePath); err != nil {
		t.Fatal(err)
	}
	profile.PolicySHA256 = strings.Repeat("0", 64)
	if err := WriteNewJSON(profilePath, profile); err != nil {
		t.Fatal(err)
	}
	if _, err := PlanOracleFromFiles(profilePath, sealedUsagePath, fixtureManifestPath, proofPath, directivePath, outputPath); err == nil {
		t.Fatal("PlanOracleFromFiles accepted a profile with forged sealed policy lineage")
	}
}

func TestValidateAssuranceOracleProfileRejectsMismatchedPartitions(t *testing.T) {
	candidate := RuntimeArtifact{Commit: strings.Repeat("a", 40), OS: "darwin", Arch: "arm64", SHA256: strings.Repeat("b", 64)}
	usage := SealedCorpusUsage{SchemaVersion: 1, ProfileSHA256: strings.Repeat("c", 64), LedgerSHA256: strings.Repeat("d", 64), PolicySHA256: strings.Repeat("e", 64), Reconciliation: UsageReconciliation{Usage: []ReconciledUsageEntry{{UsageEntry: UsageEntry{UsageKey: "System.run", PrivateProdRefs: 1}, Class: usageClassExact, SurfaceID: "apex:System.run()"}}}}
	profile := AssuranceProfile{SchemaVersion: 1, SourceProfileSHA256: usage.ProfileSHA256, SealedUsageSHA256: strings.Repeat("f", 64), LedgerSHA256: usage.LedgerSHA256, PolicySHA256: usage.PolicySHA256, FixtureManifestSHA256: strings.Repeat("1", 64), LocalProofSHA256: strings.Repeat("2", 64), Total: 1, ByDisposition: map[string]int{"hosted-deferred": 1}, NonDeferredGaps: []AssuranceProfileRow{{SurfaceID: "apex:System.run()", Disposition: localRuntimeRequired}}, Rows: []AssuranceProfileRow{{SurfaceID: "apex:System.run()", Disposition: "hosted-deferred"}}}
	proof := LocalProof{Status: "pass", Candidate: candidate, Tools: candidate, FixtureManifestSHA256: profile.FixtureManifestSHA256}
	if err := validateAssuranceOracleProfile(profile, usage, proof, profile.SealedUsageSHA256, profile.LocalProofSHA256); err == nil {
		t.Fatal("validateAssuranceOracleProfile accepted mismatched profile partitions")
	}
}

func TestValidateAssuranceOracleProfileRejectsMissingNonHostedProof(t *testing.T) {
	candidate := RuntimeArtifact{Commit: strings.Repeat("a", 40), OS: "darwin", Arch: "arm64", SHA256: strings.Repeat("b", 64)}
	usage := SealedCorpusUsage{SchemaVersion: 1, ProfileSHA256: strings.Repeat("c", 64), LedgerSHA256: strings.Repeat("d", 64), PolicySHA256: strings.Repeat("e", 64), Reconciliation: UsageReconciliation{Usage: []ReconciledUsageEntry{{UsageEntry: UsageEntry{UsageKey: "System.run", PrivateProdRefs: 1}, Class: usageClassExact, SurfaceID: "apex:System.run()"}}}}
	profile := AssuranceProfile{SchemaVersion: 1, SourceProfileSHA256: usage.ProfileSHA256, SealedUsageSHA256: strings.Repeat("f", 64), LedgerSHA256: usage.LedgerSHA256, PolicySHA256: usage.PolicySHA256, FixtureManifestSHA256: strings.Repeat("1", 64), LocalProofSHA256: strings.Repeat("2", 64), Total: 1, ByDisposition: map[string]int{localRuntimeRequired: 1}, NonDeferredGaps: []AssuranceProfileRow{{SurfaceID: "apex:System.run()", Disposition: localRuntimeRequired}}, Rows: []AssuranceProfileRow{{SurfaceID: "apex:System.run()", Disposition: localRuntimeRequired}}}
	proof := LocalProof{Status: "pass", Candidate: candidate, Tools: candidate, FixtureManifestSHA256: profile.FixtureManifestSHA256}
	if err := validateAssuranceOracleProfile(profile, usage, proof, profile.SealedUsageSHA256, profile.LocalProofSHA256); err == nil {
		t.Fatal("validateAssuranceOracleProfile accepted missing non-hosted proof")
	}
}

func TestHostedDeferredDoesNotRequireLocalFixture(t *testing.T) {
	if assuranceProfileRequiresFixture(AssuranceProfileRow{SurfaceID: "apex:System.Auth", Disposition: "hosted-deferred"}) {
		t.Fatal("hosted-deferred surface requires a local fixture")
	}
	if !assuranceProfileRequiresFixture(AssuranceProfileRow{SurfaceID: "apex:System.Test", Disposition: localRuntimeRequired}) {
		t.Fatal("non-hosted surface does not require a local fixture")
	}
}

func TestAssuranceAttemptFileSHA256BindsExactSealedBytes(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "ATTEMPT.json")
	attempt := AssuranceAttempt{SchemaVersion: 1, InventorySHA256: strings.Repeat("a", 64), CandidateAuthoritySHA256: strings.Repeat("b", 64), Candidate: replayRuntime("c"), Tools: replayRuntime("d"), RemoteCleanupAuthoritySHA256: testCleanupAuthorityHashes()}
	if err := WriteNewJSON(path, attempt); err != nil {
		t.Fatal(err)
	}
	want := localProofFileSHA256(t, path)
	got, err := assuranceAttemptFileSHA256(path, attempt)
	if err != nil || got != want {
		t.Fatalf("assuranceAttemptFileSHA256 = %q, %v; want %q", got, err, want)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, err := assuranceAttemptFileSHA256(path, attempt)
	if err != nil || changed == want {
		t.Fatalf("assuranceAttemptFileSHA256 did not detect byte change: %q, %v", changed, err)
	}
}

func TestBuildAssuranceProfileProjectsOnlyFreshOwnedRows(t *testing.T) {
	root := t.TempDir()
	profilePath := filepath.Join(root, "profile.json")
	usagePath := filepath.Join(root, "usage.json")
	ledgerPath := filepath.Join(root, "ledger.json")
	manifestPath := filepath.Join(root, "fixtures.json")
	proofPath := filepath.Join(root, "proof.json")
	outputPath := filepath.Join(root, "ASSURANCE_PROFILE.json")
	if err := os.WriteFile(profilePath, []byte(`{"rows":[{"surfaceId":"apex:System.run()","namespace":"System","disposition":"local-runtime-required","reason":"current","corpusUsage":["stale"]},{"surfaceId":"apex:Auth.hosted()","namespace":"Auth","disposition":"hosted-deferred","reason":"hosted"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteNewJSON(usagePath, SealedCorpusUsage{SchemaVersion: 1, ProfileSHA256: localProofFileSHA256(t, profilePath), LedgerSHA256: strings.Repeat("a", 64), PolicySHA256: strings.Repeat("b", 64), Reconciliation: UsageReconciliation{Usage: []ReconciledUsageEntry{
		{UsageEntry: UsageEntry{UsageKey: "System.run", PrivateProdRefs: 1}, Class: usageClassExact, SurfaceID: "apex:System.run()"},
		{UsageEntry: UsageEntry{UsageKey: "Auth.hosted", PrivateProdRefs: 1}, Class: usageClassExact, SurfaceID: "apex:Auth.hosted()"},
	}}}); err != nil {
		t.Fatal(err)
	}
	if err := WriteNewJSON(ledgerPath, surfaceledger.SurfaceLedger{SchemaVersion: 1, Rows: []surfaceledger.SurfaceLedgerRow{{SurfaceID: "apex:System.run()"}, {SurfaceID: "apex:Auth.hosted()"}}}); err != nil {
		t.Fatal(err)
	}
	if err := WriteNewJSON(manifestPath, LocalProofFixtureManifest{Fixtures: []LocalProofFixture{{ID: "system", Name: "system", SHA256: strings.Repeat("c", 64), OwnedSurfaceIDs: []string{"apex:System.run()"}, Disposition: localRuntimeRequired}, {ID: "auth", Name: "auth", SHA256: strings.Repeat("d", 64), OwnedSurfaceIDs: []string{"apex:Auth.hosted()"}, Disposition: compileShapeRequired}}}); err != nil {
		t.Fatal(err)
	}
	candidate := RuntimeArtifact{Commit: strings.Repeat("e", 40), OS: "darwin", Arch: "arm64", SHA256: strings.Repeat("f", 64)}
	stdout := `{"status":"passed","exitCode":0}`
	receipt := CommandResult{Command: []string{"exec"}, CommandSpecSHA256: strings.Repeat("1", 64), ExitCode: 0, DurationMS: 1, StdoutSHA256: replayBytesSHA256([]byte(stdout)), StderrSHA256: strings.Repeat("3", 64), Passed: true}
	if err := WriteNewJSON(proofPath, LocalProof{Status: "pass", Candidate: candidate, Tools: candidate, ProfileSHA256: strings.Repeat("4", 64), UsageSHA256: strings.Repeat("5", 64), DecisionSHA256: strings.Repeat("6", 64), FixtureManifestSHA256: localProofFileSHA256(t, manifestPath), SelectedSurfaceIDs: []string{"apex:System.run()"}, RawFixtureResults: []LocalProofFixtureResult{{FixtureID: "system", FixtureSHA256: strings.Repeat("c", 64), Disposition: localRuntimeRequired, CandidateSHA256: candidate.SHA256, ToolsSHA256: candidate.SHA256, Operation: "exec", StdoutSHA256: receipt.StdoutSHA256, Stdout: stdout}}, Surfaces: []LocalSurfaceProof{{SurfaceID: "apex:System.run()", FixtureID: "system", FixtureSHA256: strings.Repeat("c", 64), Disposition: localRuntimeRequired, CandidateSHA256: candidate.SHA256, ToolsSHA256: candidate.SHA256, RuntimeObserved: true}}}); err != nil {
		t.Fatal(err)
	}
	usage, err := readExactJSON[SealedCorpusUsage](usagePath)
	if err != nil {
		t.Fatal(err)
	}
	usage.LedgerSHA256 = localProofFileSHA256(t, ledgerPath)
	if err := os.Remove(usagePath); err != nil {
		t.Fatal(err)
	}
	if err := WriteNewJSON(usagePath, usage); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildAssuranceProfile(profilePath, usagePath, profilePath, usagePath, ledgerPath, usagePath, usagePath, profilePath, usagePath, usagePath, manifestPath, proofPath, profilePath, usagePath, outputPath); err == nil {
		t.Fatal("BuildAssuranceProfile accepted detached synthetic local proof")
	}
}

func TestAuthorizeExclusionsFromFilesSealsExactNonParityRows(t *testing.T) {
	root := t.TempDir()
	planPath, profilePath, usagePath := filepath.Join(root, "plan.json"), filepath.Join(root, "profile.json"), filepath.Join(root, "usage.json")
	requestPath, policyPath, outputPath := filepath.Join(root, "request.json"), filepath.Join(root, "policy.json"), filepath.Join(root, "authority.json")
	if err := WriteNewJSON(usagePath, SealedCorpusUsage{SchemaVersion: 1, DecisionSHA256: strings.Repeat("a", 64)}); err != nil {
		t.Fatal(err)
	}
	candidate := RuntimeArtifact{Commit: strings.Repeat("b", 40), OS: "darwin", Arch: "arm64", SHA256: strings.Repeat("c", 64)}
	tools := RuntimeArtifact{Commit: strings.Repeat("d", 40), OS: "darwin", Arch: "amd64", SHA256: strings.Repeat("e", 64)}
	if err := WriteNewJSON(profilePath, AssuranceProfile{SchemaVersion: 1, SourceProfileSHA256: strings.Repeat("f", 64), SealedUsageSHA256: localProofFileSHA256(t, usagePath), LedgerSHA256: strings.Repeat("1", 64), FixtureManifestSHA256: strings.Repeat("2", 64), LocalProofSHA256: strings.Repeat("3", 64), Total: 1, ByDisposition: map[string]int{"hosted-deferred": 1}, HostedDeferred: []AssuranceProfileRow{{SurfaceID: "apex:Auth.hosted", Disposition: "hosted-deferred"}}, Rows: []AssuranceProfileRow{{SurfaceID: "apex:Auth.hosted", Disposition: "hosted-deferred"}}}); err != nil {
		t.Fatal(err)
	}
	plan := OraclePlan{Candidate: candidate, Tools: tools, ProfileSHA256: localProofFileSHA256(t, profilePath), SealedUsageSHA256: localProofFileSHA256(t, usagePath), LocalProofSHA256: strings.Repeat("3", 64), Rows: []OraclePlanRow{{SurfaceID: "apex:Auth.hosted", Action: oracleWaiver, ExclusionClass: "hosted-identity", ExclusionReason: "requires credentials"}}}
	if err := WriteNewJSON(planPath, plan); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildExclusionRequest(planPath, profilePath, usagePath, requestPath); err != nil {
		t.Fatal(err)
	}
	if err := WriteNewJSON(policyPath, ExclusionPolicy{SchemaVersion: 1, Rows: []ExclusionPolicyRow{{SurfaceID: "apex:Auth.hosted", Class: "hosted-identity", Reason: "requires credentials"}}}); err != nil {
		t.Fatal(err)
	}
	authority, err := AuthorizeExclusionsFromFiles(requestPath, planPath, profilePath, usagePath, policyPath, outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if authority.PlanSHA256 != localProofFileSHA256(t, planPath) || authority.ProfileSHA256 != localProofFileSHA256(t, profilePath) || authority.Tools != tools || authority.DecisionSHA256 != strings.Repeat("a", 64) || authority.SalesforceParityCredit != 0 || len(authority.Rows) != 1 {
		t.Fatalf("authority = %#v", authority)
	}
}

func TestBuildExclusionRequestBindsCurrentPlanProfileAndUsage(t *testing.T) {
	root := t.TempDir()
	usagePath := filepath.Join(root, "usage.json")
	profilePath := filepath.Join(root, "profile.json")
	planPath := filepath.Join(root, "plan.json")
	outputPath := filepath.Join(root, "EXCLUSION_REQUEST.json")
	candidate := RuntimeArtifact{Commit: strings.Repeat("a", 40), OS: "darwin", Arch: "arm64", SHA256: strings.Repeat("b", 64)}
	tools := RuntimeArtifact{Commit: strings.Repeat("c", 40), OS: "darwin", Arch: "amd64", SHA256: strings.Repeat("d", 64)}
	if err := WriteNewJSON(usagePath, SealedCorpusUsage{SchemaVersion: 1, DecisionSHA256: strings.Repeat("e", 64)}); err != nil {
		t.Fatal(err)
	}
	if err := WriteNewJSON(profilePath, AssuranceProfile{SchemaVersion: 1, SourceProfileSHA256: strings.Repeat("f", 64), SealedUsageSHA256: localProofFileSHA256(t, usagePath), LedgerSHA256: strings.Repeat("1", 64), FixtureManifestSHA256: strings.Repeat("2", 64), LocalProofSHA256: strings.Repeat("3", 64), Total: 1, ByDisposition: map[string]int{"hosted-deferred": 1}, HostedDeferred: []AssuranceProfileRow{{SurfaceID: "apex:Auth.hosted()", Disposition: "hosted-deferred"}}, Rows: []AssuranceProfileRow{{SurfaceID: "apex:Auth.hosted()", Disposition: "hosted-deferred"}}}); err != nil {
		t.Fatal(err)
	}
	if err := WriteNewJSON(planPath, OraclePlan{Candidate: candidate, Tools: tools, ProfileSHA256: localProofFileSHA256(t, profilePath), SealedUsageSHA256: localProofFileSHA256(t, usagePath), LocalProofSHA256: strings.Repeat("3", 64), Rows: []OraclePlanRow{{SurfaceID: "apex:Auth.hosted()", Action: oracleWaiver, ExclusionClass: "hosted-identity", ExclusionReason: "requires credentials"}}}); err != nil {
		t.Fatal(err)
	}
	request, err := BuildExclusionRequest(planPath, profilePath, usagePath, outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if request.Candidate != candidate || request.Tools != tools || request.DecisionSHA256 != strings.Repeat("e", 64) || len(request.Rows) != 1 || request.Rows[0].SurfaceID != "apex:Auth.hosted()" || localProofFileSHA256(t, outputPath) == "" {
		t.Fatalf("request = %#v", request)
	}
}
