package corpusassurance

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/surfaceledger"
)

func TestBuildSurfaceOracleCampaignScopePartitionsExactOraclePlan(t *testing.T) {
	root := t.TempDir()
	profilePath := filepath.Join(root, "profile.json")
	planPath := filepath.Join(root, "ORACLE_PLAN.json")
	outputPath := filepath.Join(root, "scope.json")
	profile := AssuranceProfile{
		SchemaVersion: 1, SourceProfileSHA256: strings.Repeat("1", 64), LedgerSHA256: strings.Repeat("2", 64), PolicySHA256: strings.Repeat("3", 64),
		Total: 5, ByDisposition: map[string]int{localRuntimeRequired: 2, deterministicMockRequired: 2, compileShapeRequired: 1},
		Rows: []AssuranceProfileRow{
			{SurfaceID: "apex:System.Boolean", Disposition: localRuntimeRequired},
			{SurfaceID: "apex:System.Integer", Disposition: localRuntimeRequired},
			{SurfaceID: "apex:System.Long", Disposition: deterministicMockRequired},
			{SurfaceID: "apex:System.String", Disposition: deterministicMockRequired},
			{SurfaceID: "apex:System.System", Disposition: compileShapeRequired},
		},
	}
	if err := WriteNewJSON(profilePath, profile); err != nil {
		t.Fatal(err)
	}
	artifact := RuntimeArtifact{Commit: strings.Repeat("a", 40), OS: "darwin", Arch: "arm64", SHA256: strings.Repeat("b", 64)}
	oraclePlan := OraclePlan{
		Candidate: artifact, Tools: artifact, ProfileSHA256: sha256FileForTest(t, profilePath),
		Rows: []OraclePlanRow{
			{SurfaceID: "apex:System.Boolean", Action: oracleRuntime},
			{SurfaceID: "apex:System.Integer", Action: oracleRuntime},
			{SurfaceID: "apex:System.Long", Action: oracleLocalContractOnly, ExclusionClass: "local-only", ExclusionReason: "not Salesforce parity"},
			{SurfaceID: "apex:System.String", Action: oracleLocalContractOnly, ExclusionClass: "local-only", ExclusionReason: "not Salesforce parity"},
			{SurfaceID: "apex:System.System", Action: oracleLocalContractOnly, ExclusionClass: "local-only", ExclusionReason: "not Salesforce parity"},
		},
	}
	if err := WriteNewJSON(planPath, oraclePlan); err != nil {
		t.Fatal(err)
	}

	scope, err := BuildSurfaceOracleCampaignScope(planPath, profilePath, outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if scope.Kind != "oracle-plan" || scope.OraclePlanSHA256 != sha256FileForTest(t, planPath) || scope.Total != 2 {
		t.Fatalf("campaign scope binding = %#v", scope)
	}
	if scope.SourceProfileSHA256 != profile.SourceProfileSHA256 || scope.LedgerSHA256 != profile.LedgerSHA256 || scope.PolicySHA256 != profile.PolicySHA256 {
		t.Fatalf("campaign scope authority = %#v", scope)
	}
	if got := scope.Rows; len(got) != 2 || got[0] != (SurfaceOracleScopeRow{SurfaceID: "apex:System.Boolean", Disposition: localRuntimeRequired, Action: oracleRuntime}) || got[1] != (SurfaceOracleScopeRow{SurfaceID: "apex:System.Integer", Disposition: localRuntimeRequired, Action: oracleRuntime}) {
		t.Fatalf("campaign scope rows = %#v", got)
	}
	if scope.ByDisposition[localRuntimeRequired] != 2 || scope.ByDisposition[compileShapeRequired] != 0 || scope.ByDisposition[deterministicMockRequired] != 0 {
		t.Fatalf("campaign scope dispositions = %#v", scope.ByDisposition)
	}

	definition := OrchestratorCampaignDefinition{
		Candidate: OrchestratorArtifact{Commit: artifact.Commit, SHA256: artifact.SHA256},
		Tools:     OrchestratorArtifact{Commit: artifact.Commit, SHA256: artifact.SHA256},
		ScopePath: outputPath, ScopeSHA256: sha256FileForTest(t, outputPath),
		ControlledInputSHA256: map[string]string{"oracle-plan": sha256FileForTest(t, planPath)},
		Shards:                [2][]string{{"apex:System.Boolean"}, {"apex:System.Integer"}},
	}
	forged := definition
	forged.ControlledInputSHA256 = map[string]string{"oracle-plan": strings.Repeat("f", 64)}
	if _, err := PlanOrchestratorCampaign(forged); err == nil {
		t.Fatal("orchestrator accepted a campaign scope bound to a different Oracle plan")
	}
	campaign, err := PlanOrchestratorCampaign(definition)
	if err != nil {
		t.Fatal(err)
	}
	var union []string
	for _, job := range campaign.Jobs {
		lease := OrchestratorLease{CampaignID: campaign.CampaignID, JobID: job.ID, Kind: job.Kind, ShardIndex: job.ShardIndex, SurfaceIDs: job.SurfaceIDs, Generation: 1, Worker: "worker-a", DurationMS: 1}
		ids, err := orchestratorSalesforceExpectedSurfaceIDs(oraclePlan, campaign, lease)
		if err != nil {
			t.Fatal(err)
		}
		union = append(union, ids...)
	}
	wantKinds, err := oracleSalesforceResultKinds(oraclePlan)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := map[string]bool{}, map[string]bool{}; func() bool {
		for _, id := range union {
			got[id] = true
		}
		for id := range wantKinds {
			want[id] = true
		}
		return reflect.DeepEqual(got, want)
	}() == false {
		t.Fatalf("orchestrator Salesforce union = %#v, want exact Oracle plan %#v", got, want)
	}

	planCampaign := func(name string, rows []SurfaceOracleScopeRow, shards [2][]string) (OrchestratorCampaignPlan, error) {
		t.Helper()
		path := filepath.Join(root, name+".json")
		value := SurfaceOracleScope{
			SchemaVersion: 1, Kind: "oracle-plan", OraclePlanSHA256: sha256FileForTest(t, planPath),
			SourceProfileSHA256: profile.SourceProfileSHA256, LedgerSHA256: profile.LedgerSHA256, PolicySHA256: profile.PolicySHA256,
			Total: len(rows), ByDisposition: map[string]int{localRuntimeRequired: 0, deterministicMockRequired: 0, compileShapeRequired: 0}, Rows: rows,
		}
		for _, row := range rows {
			value.ByDisposition[row.Disposition]++
		}
		if err := WriteNewJSON(path, value); err != nil {
			t.Fatal(err)
		}
		candidate := definition
		candidate.ScopePath, candidate.ScopeSHA256, candidate.Shards = path, sha256FileForTest(t, path), shards
		return PlanOrchestratorCampaign(candidate)
	}
	for name, test := range map[string]struct {
		rows   []SurfaceOracleScopeRow
		shards [2][]string
	}{
		"missing projection row": {
			rows:   []SurfaceOracleScopeRow{{SurfaceID: "apex:System.Boolean", Disposition: localRuntimeRequired, Action: oracleRuntime}, {SurfaceID: "apex:System.Long", Disposition: deterministicMockRequired, Action: oracleCompile}},
			shards: [2][]string{{"apex:System.Boolean"}, {"apex:System.Long"}},
		},
		"extra excluded row": {
			rows: []SurfaceOracleScopeRow{
				{SurfaceID: "apex:System.Boolean", Disposition: localRuntimeRequired, Action: oracleRuntime},
				{SurfaceID: "apex:System.Integer", Disposition: localRuntimeRequired, Action: oracleRuntime},
				{SurfaceID: "apex:System.Long", Disposition: deterministicMockRequired, Action: oracleCompile},
			},
			shards: [2][]string{{"apex:System.Boolean", "apex:System.Integer"}, {"apex:System.Long"}},
		},
		"action mismatch": {
			rows:   []SurfaceOracleScopeRow{{SurfaceID: "apex:System.Boolean", Disposition: deterministicMockRequired, Action: oracleCompile}, {SurfaceID: "apex:System.Integer", Disposition: localRuntimeRequired, Action: oracleRuntime}},
			shards: [2][]string{{"apex:System.Boolean"}, {"apex:System.Integer"}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			forgedCampaign, err := planCampaign(strings.ReplaceAll(name, " ", "-"), test.rows, test.shards)
			if err != nil {
				t.Fatal(err)
			}
			job := forgedCampaign.Jobs[0]
			lease := OrchestratorLease{CampaignID: forgedCampaign.CampaignID, JobID: job.ID, Kind: job.Kind, ShardIndex: job.ShardIndex, SurfaceIDs: job.SurfaceIDs, Generation: 1, Worker: "worker-a", DurationMS: 1}
			if _, err := orchestratorSalesforceExpectedSurfaceIDs(oraclePlan, forgedCampaign, lease); err == nil {
				t.Fatal("forged campaign scope matched the Oracle plan")
			}
		})
	}
	for name, rows := range map[string][]SurfaceOracleScopeRow{
		"duplicate": {{SurfaceID: "apex:System.Boolean", Disposition: localRuntimeRequired, Action: oracleRuntime}, {SurfaceID: "apex:System.Boolean", Disposition: localRuntimeRequired, Action: oracleRuntime}},
		"unsorted":  {{SurfaceID: "apex:System.System", Disposition: compileShapeRequired, Action: oracleCompile}, {SurfaceID: "apex:System.Boolean", Disposition: localRuntimeRequired, Action: oracleRuntime}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := planCampaign(name, rows, [2][]string{{"apex:System.Boolean"}, {"apex:System.Integer"}}); err == nil {
				t.Fatal("invalid campaign scope was accepted")
			}
		})
	}
}

func TestBuildSurfaceOracleCampaignScopePreservesCompileAction(t *testing.T) {
	root := t.TempDir()
	profilePath := filepath.Join(root, "profile.json")
	profile := AssuranceProfile{
		SchemaVersion: 1, SourceProfileSHA256: strings.Repeat("1", 64), LedgerSHA256: strings.Repeat("2", 64), PolicySHA256: strings.Repeat("3", 64),
		Total: 1, ByDisposition: map[string]int{compileShapeRequired: 1}, Rows: []AssuranceProfileRow{{SurfaceID: "apex:System.System", Disposition: compileShapeRequired}},
	}
	if err := WriteNewJSON(profilePath, profile); err != nil {
		t.Fatal(err)
	}
	artifact := RuntimeArtifact{Commit: strings.Repeat("a", 40), OS: "darwin", Arch: "arm64", SHA256: strings.Repeat("b", 64)}
	planPath := filepath.Join(root, "ORACLE_PLAN.json")
	if err := WriteNewJSON(planPath, OraclePlan{Candidate: artifact, Tools: artifact, ProfileSHA256: sha256FileForTest(t, profilePath), Rows: []OraclePlanRow{{SurfaceID: "apex:System.System", Action: oracleCompile}}}); err != nil {
		t.Fatal(err)
	}
	scope, err := BuildSurfaceOracleCampaignScope(planPath, profilePath, filepath.Join(root, "scope.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(scope.Rows) != 1 || scope.Rows[0].Action != oracleCompile || scope.Rows[0].Disposition != compileShapeRequired {
		t.Fatalf("compile campaign scope = %#v", scope)
	}
}

func TestBuildSurfaceOracleCampaignScopeRejectsInvalidPlanRows(t *testing.T) {
	for _, test := range []struct {
		name string
		rows []OraclePlanRow
	}{
		{name: "empty", rows: []OraclePlanRow{{SurfaceID: "", Action: oracleRuntime}}},
		{name: "duplicate", rows: []OraclePlanRow{{SurfaceID: "apex:System.Boolean", Action: oracleRuntime}, {SurfaceID: "apex:System.Boolean", Action: oracleRuntime}}},
		{name: "malformed action", rows: []OraclePlanRow{{SurfaceID: "apex:System.Boolean", Action: "selected"}}},
		{name: "false disposition", rows: []OraclePlanRow{{SurfaceID: "apex:System.System", Action: oracleRuntime}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			profilePath := filepath.Join(root, "profile.json")
			profile := AssuranceProfile{
				SchemaVersion: 1, SourceProfileSHA256: strings.Repeat("1", 64), LedgerSHA256: strings.Repeat("2", 64), PolicySHA256: strings.Repeat("3", 64),
				Total: len(test.rows), ByDisposition: map[string]int{localRuntimeRequired: len(test.rows)}, Rows: make([]AssuranceProfileRow, len(test.rows)),
			}
			for i, row := range test.rows {
				profile.Rows[i] = AssuranceProfileRow{SurfaceID: row.SurfaceID, Disposition: localRuntimeRequired}
				if row.SurfaceID == "apex:System.System" {
					profile.Rows[i].Disposition = compileShapeRequired
					profile.ByDisposition = map[string]int{compileShapeRequired: 1}
				}
			}
			if err := WriteNewJSON(profilePath, profile); err != nil {
				t.Fatal(err)
			}
			artifact := RuntimeArtifact{Commit: strings.Repeat("a", 40), OS: "darwin", Arch: "arm64", SHA256: strings.Repeat("b", 64)}
			planPath := filepath.Join(root, "ORACLE_PLAN.json")
			if err := WriteNewJSON(planPath, OraclePlan{Candidate: artifact, Tools: artifact, ProfileSHA256: sha256FileForTest(t, profilePath), Rows: test.rows}); err != nil {
				t.Fatal(err)
			}
			if _, err := BuildSurfaceOracleCampaignScope(planPath, profilePath, filepath.Join(root, "scope.json")); err == nil {
				t.Fatal("invalid Oracle plan rows produced a campaign scope")
			}
		})
	}
}

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
