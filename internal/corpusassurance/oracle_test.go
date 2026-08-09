package corpusassurance

import (
	"os"
	"path/filepath"
	"testing"
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
	profilePath := filepath.Join(root, "profile.json")
	sealedUsagePath := filepath.Join(root, "CORPUS_USAGE.json")
	proofPath := filepath.Join(root, "proof.json")
	directivePath := filepath.Join(root, "directives.json")
	if err := os.WriteFile(profilePath, []byte(`{"rows":[{"surfaceId":"apex:System.run()","usageKey":"System.run","disposition":"local-runtime-required"}],"corpusUsage":["old"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	sealedUsage := SealedCorpusUsage{SchemaVersion: 1, ProfileSHA256: localProofFileSHA256(t, profilePath), Reconciliation: UsageReconciliation{Usage: []ReconciledUsageEntry{{UsageEntry: UsageEntry{UsageKey: "System.run", PrivateProdRefs: 1}, Class: usageClassExact, SurfaceID: "apex:System.run()"}}}}
	if err := WriteNewJSON(sealedUsagePath, sealedUsage); err != nil {
		t.Fatal(err)
	}
	proof := LocalProof{Surfaces: []LocalSurfaceProof{{SurfaceID: "apex:System.run()", Disposition: localRuntimeRequired, RuntimeObserved: true}}}
	if err := WriteNewJSON(proofPath, proof); err != nil {
		t.Fatal(err)
	}
	directives := OracleDirectiveFile{SchemaVersion: 1, ProfileSHA256: localProofFileSHA256(t, profilePath), SealedUsageSHA256: localProofFileSHA256(t, sealedUsagePath), LocalProofSHA256: localProofFileSHA256(t, proofPath)}
	if err := WriteNewJSON(directivePath, directives); err != nil {
		t.Fatal(err)
	}
	plan, err := PlanOracleFromFiles(profilePath, sealedUsagePath, proofPath, directivePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Rows) != 1 || plan.Rows[0].Action != oracleRuntime || plan.ProfileSHA256 != directives.ProfileSHA256 {
		t.Fatalf("plan = %#v", plan)
	}
}
