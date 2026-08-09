package corpusassurance

import (
	"os"
	"path/filepath"
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

func TestPlanOracleForUsageTreatsUndirectedMockAsDeployable(t *testing.T) {
	reconciled := UsageReconciliation{Usage: []ReconciledUsageEntry{{UsageEntry: UsageEntry{UsageKey: "Cache.read", PrivateProdRefs: 1}, Class: usageClassExact, SurfaceID: "apex:Cache.read()"}}}
	profile := []OracleProfileRow{{SurfaceID: "apex:Cache.read()", Disposition: deterministicMockRequired}}
	proof := LocalProof{Surfaces: []LocalSurfaceProof{{SurfaceID: "apex:Cache.read()", Disposition: deterministicMockRequired, BehaviorObserved: true}}}
	plan, err := planOracleForUsage(reconciled, profile, proof, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Rows) != 1 || plan.Rows[0].Action != oracleCompile {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestPlanOracleFromFilesBindsFreshInputs(t *testing.T) {
	root := t.TempDir()
	profilePath := filepath.Join(root, "ASSURANCE_PROFILE.json")
	sealedUsagePath := filepath.Join(root, "CORPUS_USAGE.json")
	proofPath := filepath.Join(root, "proof.json")
	directivePath := filepath.Join(root, "directives.json")
	outputPath := filepath.Join(root, "ORACLE_PLAN.json")
	sealedUsage := SealedCorpusUsage{SchemaVersion: 1, Reconciliation: UsageReconciliation{Usage: []ReconciledUsageEntry{{UsageEntry: UsageEntry{UsageKey: "System.run", PrivateProdRefs: 1}, Class: usageClassExact, SurfaceID: "apex:System.run()"}}}}
	if err := WriteNewJSON(sealedUsagePath, sealedUsage); err != nil {
		t.Fatal(err)
	}
	candidate := RuntimeArtifact{Commit: strings.Repeat("a", 40), OS: "darwin", Arch: "arm64", SHA256: strings.Repeat("b", 64)}
	tools := RuntimeArtifact{Commit: strings.Repeat("c", 40), OS: "darwin", Arch: "arm64", SHA256: strings.Repeat("d", 64)}
	proof := LocalProof{Status: "pass", Candidate: candidate, Tools: tools, FixtureManifestSHA256: strings.Repeat("a", 64), Surfaces: []LocalSurfaceProof{{SurfaceID: "apex:System.run()", Disposition: localRuntimeRequired, RuntimeObserved: true}}}
	if err := WriteNewJSON(proofPath, proof); err != nil {
		t.Fatal(err)
	}
	profile := AssuranceProfile{SchemaVersion: 1, SourceProfileSHA256: strings.Repeat("e", 64), SealedUsageSHA256: localProofFileSHA256(t, sealedUsagePath), LedgerSHA256: strings.Repeat("f", 64), FixtureManifestSHA256: strings.Repeat("a", 64), LocalProofSHA256: localProofFileSHA256(t, proofPath), Total: 1, ByDisposition: map[string]int{localRuntimeRequired: 1}, NonDeferredGaps: []AssuranceProfileRow{{SurfaceID: "apex:System.run()", Disposition: localRuntimeRequired}}, Rows: []AssuranceProfileRow{{SurfaceID: "apex:System.run()", Disposition: localRuntimeRequired}}}
	if err := WriteNewJSON(profilePath, profile); err != nil {
		t.Fatal(err)
	}
	directives := OracleDirectiveFile{SchemaVersion: 1, ProfileSHA256: strings.Repeat("e", 64), SealedUsageSHA256: localProofFileSHA256(t, sealedUsagePath), LocalProofSHA256: localProofFileSHA256(t, proofPath)}
	if err := WriteNewJSON(directivePath, directives); err != nil {
		t.Fatal(err)
	}
	plan, err := PlanOracleFromFiles(profilePath, sealedUsagePath, proofPath, directivePath, outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Rows) != 1 || plan.Rows[0].Action != oracleRuntime || plan.ProfileSHA256 != localProofFileSHA256(t, profilePath) || plan.Candidate != candidate || plan.Tools != tools || localProofFileSHA256(t, outputPath) == "" {
		t.Fatalf("plan = %#v", plan)
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
	if err := WriteNewJSON(usagePath, SealedCorpusUsage{SchemaVersion: 1, ProfileSHA256: localProofFileSHA256(t, profilePath), LedgerSHA256: strings.Repeat("a", 64), Reconciliation: UsageReconciliation{Usage: []ReconciledUsageEntry{
		{UsageEntry: UsageEntry{UsageKey: "System.run", PrivateProdRefs: 1}, Class: usageClassExact, SurfaceID: "apex:System.run()"},
		{UsageEntry: UsageEntry{UsageKey: "Auth.hosted", PrivateProdRefs: 1}, Class: usageClassExact, SurfaceID: "apex:Auth.hosted()"},
	}}}); err != nil {
		t.Fatal(err)
	}
	if err := WriteNewJSON(ledgerPath, surfaceledger.SurfaceLedger{SchemaVersion: 1, Rows: []surfaceledger.SurfaceLedgerRow{{SurfaceID: "apex:System.run()"}, {SurfaceID: "apex:Auth.hosted()"}}}); err != nil {
		t.Fatal(err)
	}
	if err := WriteNewJSON(manifestPath, LocalProofFixtureManifest{Fixtures: []LocalProofFixture{{ID: "system", Name: "system", SHA256: strings.Repeat("b", 64), OwnedSurfaceIDs: []string{"apex:System.run()"}}, {ID: "auth", Name: "auth", SHA256: strings.Repeat("c", 64), OwnedSurfaceIDs: []string{"apex:Auth.hosted()"}}}}); err != nil {
		t.Fatal(err)
	}
	candidate := RuntimeArtifact{Commit: strings.Repeat("d", 40), OS: "darwin", Arch: "arm64", SHA256: strings.Repeat("e", 64)}
	if err := WriteNewJSON(proofPath, LocalProof{Status: "pass", Candidate: candidate, Tools: candidate, FixtureManifestSHA256: localProofFileSHA256(t, manifestPath), Surfaces: []LocalSurfaceProof{{SurfaceID: "apex:System.run()", Disposition: localRuntimeRequired, RuntimeObserved: true}}}); err != nil {
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
	profile, err := BuildAssuranceProfile(profilePath, usagePath, ledgerPath, manifestPath, proofPath, outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if profile.Total != 2 || len(profile.Rows) != 2 || len(profile.NonDeferredGaps) != 1 || len(profile.HostedDeferred) != 1 || profile.ByDisposition[localRuntimeRequired] != 1 || profile.ByDisposition["hosted-deferred"] != 1 {
		t.Fatalf("profile = %#v", profile)
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "corpusUsage") {
		t.Fatalf("projected profile retained stale corpus usage: %s", data)
	}
}

func TestAuthorizeExclusionsFromFilesSealsExactNonParityRows(t *testing.T) {
	root := t.TempDir()
	planPath, usagePath, policyPath, outputPath := filepath.Join(root, "plan.json"), filepath.Join(root, "usage.json"), filepath.Join(root, "policy.json"), filepath.Join(root, "authority.json")
	if err := WriteNewJSON(usagePath, SealedCorpusUsage{SchemaVersion: 1}); err != nil {
		t.Fatal(err)
	}
	candidate := RuntimeArtifact{Commit: strings.Repeat("b", 40), OS: "darwin", Arch: "arm64", SHA256: strings.Repeat("c", 64)}
	tools := RuntimeArtifact{Commit: strings.Repeat("d", 40), OS: "darwin", Arch: "amd64", SHA256: strings.Repeat("e", 64)}
	plan := OraclePlan{Candidate: candidate, Tools: tools, SealedUsageSHA256: localProofFileSHA256(t, usagePath), Rows: []OraclePlanRow{{SurfaceID: "apex:Auth.hosted", Action: oracleWaiver, ExclusionClass: "hosted-identity", ExclusionReason: "requires credentials"}}}
	if err := WriteNewJSON(planPath, plan); err != nil {
		t.Fatal(err)
	}
	if err := WriteNewJSON(policyPath, ExclusionPolicy{SchemaVersion: 1, Rows: []ExclusionPolicyRow{{SurfaceID: "apex:Auth.hosted", Class: "hosted-identity", Reason: "requires credentials"}}}); err != nil {
		t.Fatal(err)
	}
	authority, err := AuthorizeExclusionsFromFiles(planPath, usagePath, policyPath, candidate, outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if authority.PlanSHA256 != localProofFileSHA256(t, planPath) || authority.SalesforceParityCredit != 0 || len(authority.Rows) != 1 {
		t.Fatalf("authority = %#v", authority)
	}
}
