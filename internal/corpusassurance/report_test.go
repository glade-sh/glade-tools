package corpusassurance

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestValidateAssuranceOutcomesRequiresOneDefensibleOutcomePerSurface(t *testing.T) {
	rows := []AssuranceSurfaceRow{
		{SurfaceID: "apex:Compile.only", CompileReady: true},
		{SurfaceID: "apex:Test.ready", CompileReady: true, TestReady: true},
		{SurfaceID: "apex:Runtime.ready", CompileReady: true, TestReady: true, RuntimeParityReady: true},
		{SurfaceID: "apex:Hosted.only", NonParity: true, ExclusionClass: "hosted", ExclusionReason: "requires org identity"},
	}
	if err := ValidateAssuranceOutcomes(rows); err != nil {
		t.Fatalf("ValidateAssuranceOutcomes: %v", err)
	}
	rows[1].NonParity = true
	if err := ValidateAssuranceOutcomes(rows); err == nil {
		t.Fatal("accepted a surface with parity and non-parity outcomes")
	}
	rows[1] = AssuranceSurfaceRow{SurfaceID: "apex:Local.only", CompileReady: true, TestReady: true, NonParity: true, ExclusionClass: "policy-local-only", ExclusionReason: "hosted execution is intentionally excluded"}
	if err := ValidateAssuranceOutcomes(rows); err != nil {
		t.Fatalf("ValidateAssuranceOutcomes rejected local readiness alongside explicit non-parity: %v", err)
	}
}

func TestBuildReportRequiresDirectEvidencePaths(t *testing.T) {
	if _, err := BuildAssuranceReport(AssuranceReportRequest{}); err == nil {
		t.Fatal("BuildAssuranceReport accepted missing direct evidence paths")
	}
	root := t.TempDir()
	path := func(name string) string { return filepath.Join(root, name) }
	request := AssuranceReportRequest{InventoryPath: path("IN_SCOPE.json"), RootManifestPath: path("MANIFEST.json"), LedgerPath: path("ledger.json"), SourceProfilePath: path("source-profile.json"), PolicyPath: path("support-policy.json"), DecisionPath: path("USAGE_DECISIONS.json"), UsagePath: path("CORPUS_USAGE.json"), ProfilePath: path("ASSURANCE_PROFILE.json"), FixtureManifestPath: path("fixtures.json"), ReplayPath: path("REPLAY.json"), ReplayHostManifestPaths: []string{path("local-manifest.json"), path("replay-worker-manifest.json")}, ReplayShardPaths: []string{path("local-shard.json"), path("replay-worker-shard.json")}, AttemptPath: path("ATTEMPT.json"), LocalProofPath: path("LOCAL_PROOF.json"), OraclePlanPath: path("ORACLE_PLAN.json"), ExclusionRequestPath: path("EXCLUSION_REQUEST.json"), ExclusionPolicyPath: path("exclusion-policy.json"), AuthorityPath: path("EXCLUSION_AUTHORITY.json"), ReleaseValidationPath: path("RELEASE_VALIDATION.json"), BundlePath: path("bundle.json"), FilterScriptPath: path("filter.py"), ScratchDefinitionPath: path("scratch.json"), ToolsAMD64Path: path("tools-amd64"), SalesforceFiles: []SalesforceShardFiles{{ShardPath: path("shard.json"), DispatchPath: path("dispatch.json"), PreflightPath: path("preflight.json"), CreationPath: path("creation.json"), CleanupPath: path("cleanup.json")}}, RemoteCleanupPaths: []string{path("replay-worker-cleanup.json"), path("salesforce-worker-cleanup.json")}, RemoteCleanupAuthorityPaths: []string{path("replay-worker-authority.json"), path("salesforce-worker-authority.json")}, JSONPath: path("ASSURANCE.json"), HTMLPath: path("ASSURANCE.html"), ReceiptPath: path("RECEIPT.json"), PacketPath: path("packet")}
	if err := requiredReportEvidencePaths(request); err != nil {
		t.Fatalf("complete direct evidence paths rejected: %v", err)
	}
	request.ReleaseValidationPath = ""
	if err := requiredReportEvidencePaths(request); err == nil {
		t.Fatal("report accepted omitted release validation path")
	}
}

func TestReportPostflightHashReadsDiskAfterSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "input.json")
	old := []byte(`{"version":1}`)
	if err := os.WriteFile(path, old, 0o600); err != nil {
		t.Fatal(err)
	}
	setReportSnapshot(map[string]reportInputSnapshot{path: {Data: append([]byte(nil), old...), Mode: 0o600}})
	t.Cleanup(clearReportSnapshot)
	expected := map[string]string{path: replayBytesSHA256(old)}
	if err := os.WriteFile(path, []byte(`{"version":2}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := revalidateReportInputHashes(singleReportInputRequest(path), expected); err == nil {
		t.Fatal("postflight revalidation accepted a changed on-disk input")
	}
}

func TestReportPostflightRejectsModeChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "input.json")
	data := []byte(`{"version":1}`)
	if err := os.WriteFile(path, data, 0o640); err != nil {
		t.Fatal(err)
	}
	setReportSnapshot(map[string]reportInputSnapshot{path: {Data: append([]byte(nil), data...), Mode: 0o640}})
	t.Cleanup(clearReportSnapshot)
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := revalidateReportInputHashes(singleReportInputRequest(path), map[string]string{path: replayBytesSHA256(data)}); err == nil {
		t.Fatal("postflight revalidation accepted a changed on-disk mode")
	}
}

func TestReadRegularFileSnapshotRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	target, link := filepath.Join(root, "target"), filepath.Join(root, "link")
	if err := os.WriteFile(target, []byte("sealed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readRegularFileSnapshot(link); err == nil {
		t.Fatal("snapshot accepted a symlink")
	}
}

func singleReportInputRequest(path string) AssuranceReportRequest {
	return AssuranceReportRequest{
		InventoryPath: path, RootManifestPath: path, LedgerPath: path, SourceProfilePath: path,
		PolicyPath: path, DecisionPath: path, UsagePath: path, ProfilePath: path,
		FixtureManifestPath: path, ReplayPath: path, AttemptPath: path, LocalProofPath: path,
		OraclePlanPath: path, ExclusionRequestPath: path, ExclusionPolicyPath: path,
		AuthorityPath: path, ReleaseValidationPath: path, BundlePath: path,
		FilterScriptPath: path, ScratchDefinitionPath: path, ToolsAMD64Path: path,
	}
}

func TestTrackedGeneratedDocsContainNoPrivateCorpusIdentifiers(t *testing.T) {
	root := filepath.Join("..", "..", "docs", "generated")
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if containsPrivateReportPath(data) {
			return fmt.Errorf("private identifier in generated public artifact %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestExpectedReportCleanupRootsAreDerivedFromExecutionEvidence(t *testing.T) {
	root := t.TempDir()
	replayPath := filepath.Join(root, "replay.json")
	salesforcePath := filepath.Join(root, "salesforce.json")
	replayRoot := filepath.Join(root, "replay-root")
	if err := WriteNewJSON(replayPath, ReplayShard{Host: "replay-worker", AttemptRoot: filepath.Join(replayRoot, "prepared", "hosts", "replay-worker")}); err != nil {
		t.Fatal(err)
	}
	if err := WriteNewJSON(salesforcePath, SalesforceShard{ExecutorRoot: filepath.Join(root, "salesforce-root", "executor", "shard-0")}); err != nil {
		t.Fatal(err)
	}
	roots, err := expectedReportCleanupRoots(AssuranceReportRequest{ReplayShardPaths: []string{replayPath}, SalesforceFiles: []SalesforceShardFiles{{ShardPath: salesforcePath}}})
	if err != nil || roots["replay-worker"] != replayRoot || roots["salesforce-worker"] != filepath.Join(root, "salesforce-root") {
		t.Fatalf("roots = %#v, err = %v", roots, err)
	}
	if err := WriteNewJSON(filepath.Join(root, "salesforce-1.json"), SalesforceShard{ExecutorRoot: filepath.Join(root, "other-root", "executor", "shard-1")}); err != nil {
		t.Fatal(err)
	}
	request := AssuranceReportRequest{ReplayShardPaths: []string{replayPath}, SalesforceFiles: []SalesforceShardFiles{{ShardPath: salesforcePath}, {ShardPath: filepath.Join(root, "salesforce-1.json")}}}
	if _, err := expectedReportCleanupRoots(request); err == nil {
		t.Fatal("accepted Salesforce shard roots from different cleanup attempts")
	}
}

func TestWriteHTMLIsSelfContainedAndCreateOnly(t *testing.T) {
	root := t.TempDir()
	reportPath := filepath.Join(root, "ASSURANCE.json")
	outputPath := filepath.Join(root, "ASSURANCE.html")
	data := []byte(`{"schemaVersion":1,"rows":[{"surfaceId":"apex:Example.run","repositoryIds":["private-corpus-001"]}]}`)
	if err := os.WriteFile(reportPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteAssuranceHTML(reportPath, outputPath); err != nil {
		t.Fatalf("WriteAssuranceHTML: %v", err)
	}
	html, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"private-corpus-001", "apex:Example.run", "id=\"assurance-data\"", "id=\"namespace\"", "id=\"disposition\"", "id=\"repository\"", "id=\"evidence\"", "id=\"exclusion\"", "id=\"text\"", "id=\"repository-rows\"", "id=\"repository-summaries\""} {
		if !strings.Contains(string(html), text) {
			t.Fatalf("HTML misses %q", text)
		}
	}
	if err := WriteAssuranceHTML(reportPath, outputPath); err == nil {
		t.Fatal("WriteAssuranceHTML overwrote output")
	}
}

func TestHTMLShowsReadinessAndNonParityIndependently(t *testing.T) {
	report := []byte(`{"schemaVersion":1,"rows":[{"surfaceId":"apex:Example.run","compileReady":true,"testReady":true,"nonParity":true,"exclusionReason":"hosted execution"}],"repositorySurfaceRows":[{"repositoryId":"private-corpus-001","surfaceId":"apex:Example.run","compileReady":true,"testReady":true,"nonParity":true,"nonParityReason":"hosted execution"}]}`)
	html, err := renderAssuranceHTML(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<th>Readiness</th><th>Non-parity</th>", "function nonparity(x)", "nonparity(x)"} {
		if !strings.Contains(string(html), want) {
			t.Fatalf("HTML does not independently render readiness and non-parity: missing %q", want)
		}
	}
}

func TestReceiptWritesAnAcyclicReceiptLast(t *testing.T) {
	root := t.TempDir()
	jsonPath, htmlPath, receiptPath := filepath.Join(root, "ASSURANCE.json"), filepath.Join(root, "ASSURANCE.html"), filepath.Join(root, "RECEIPT.json")
	report := AssuranceReport{SchemaVersion: 1, Rows: []AssuranceSurfaceRow{{SurfaceID: "apex:Example.run", CompileReady: true}}}
	receipt, err := writeAssuranceArtifacts(report, jsonPath, htmlPath, receiptPath, map[string]string{"IN_SCOPE.json": strings.Repeat("a", 64)})
	if err != nil {
		t.Fatalf("writeAssuranceArtifacts: %v", err)
	}
	if receipt.AssuranceSHA256 != localProofFileSHA256(t, jsonPath) || receipt.HTMLSHA256 != localProofFileSHA256(t, htmlPath) || receipt.ReceiptSHA256 != "" {
		t.Fatalf("receipt = %#v", receipt)
	}
	if _, err := os.Stat(receiptPath); err != nil {
		t.Fatal(err)
	}
}

func TestRetainReportPacketCopiesSnapshotBytesAndModes(t *testing.T) {
	oldUmask := syscall.Umask(0o077)
	defer syscall.Umask(oldUmask)
	root := t.TempDir()
	source := filepath.Join(root, "input.json")
	if err := os.WriteFile(source, []byte("sealed\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	setReportSnapshot(map[string]reportInputSnapshot{source: {Data: []byte("sealed\n"), Mode: 0o640}})
	defer clearReportSnapshot()
	packet := filepath.Join(root, "packet")
	hash, err := retainReportPacket(packet, []string{source}, map[string]string{source: replayBytesSHA256([]byte("sealed\n"))})
	if err != nil || !sha256Pattern.MatchString(hash) {
		t.Fatalf("retainReportPacket = %q, %v", hash, err)
	}
	data, err := os.ReadFile(filepath.Join(packet, "DIRECT_INPUT_000"))
	info, statErr := os.Stat(filepath.Join(packet, "DIRECT_INPUT_000"))
	if err != nil || statErr != nil || string(data) != "sealed\n" || info.Mode().Perm() != 0o640 {
		t.Fatalf("retained input = %q, %v, %v", data, err, statErr)
	}
	if localProofFileSHA256(t, filepath.Join(packet, "MANIFEST.json")) != hash {
		t.Fatal("packet manifest hash does not match retained manifest")
	}
}

func TestRetainReportPacketIncludesSealedSalesforceExecutorTree(t *testing.T) {
	root := t.TempDir()
	direct := filepath.Join(root, "input.json")
	if err := os.WriteFile(direct, []byte("direct\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	setReportSnapshot(map[string]reportInputSnapshot{direct: {Data: []byte("direct\n"), Mode: 0o600}})
	defer clearReportSnapshot()
	extra := map[string]reportInputSnapshot{
		"salesforce-executor-000/EXECUTOR_MANIFEST.json": {Data: []byte("manifest\n"), Mode: 0o640},
		"salesforce-executor-000/filter/results.json":    {Data: []byte("results\n"), Mode: 0o600},
	}
	packet := filepath.Join(root, "packet")
	if _, err := retainReportPacket(packet, []string{direct}, map[string]string{direct: replayBytesSHA256([]byte("direct\n"))}, extra); err != nil {
		t.Fatalf("retainReportPacket: %v", err)
	}
	for name, want := range extra {
		data, err := os.ReadFile(filepath.Join(packet, filepath.FromSlash(name)))
		info, statErr := os.Stat(filepath.Join(packet, filepath.FromSlash(name)))
		if err != nil || statErr != nil || string(data) != string(want.Data) || info.Mode().Perm() != want.Mode.Perm() {
			t.Fatalf("retained executor input %q = %q mode=%v, err=%v statErr=%v", name, data, info.Mode(), err, statErr)
		}
	}
}

func TestReportSalesforceExecutorSnapshotsUseStableShardPaths(t *testing.T) {
	snapshots := []salesforceShardEvidenceSnapshot{{Shard: SalesforceShard{ShardIndex: 1}, Executor: salesforceExecutorSnapshot{
		ManifestSHA256: replayBytesSHA256([]byte("manifest\n")),
		Manifest:       reportInputSnapshot{Data: []byte("manifest\n"), Mode: 0o640},
		Snapshots: map[string]reportInputSnapshot{
			"filter/results.json": {Data: []byte("results\n"), Mode: 0o600},
		},
	}}}
	got, err := reportSalesforceExecutorSnapshots(snapshots)
	if err != nil {
		t.Fatal(err)
	}
	if string(got["salesforce-executor-001/EXECUTOR_MANIFEST.json"].Data) != "manifest\n" || string(got["salesforce-executor-001/filter/results.json"].Data) != "results\n" {
		t.Fatalf("executor packet snapshots = %#v", got)
	}
}

func TestReceiptRejectsPrivateOrMissingReceiptKeys(t *testing.T) {
	root := t.TempDir()
	report := AssuranceReport{SchemaVersion: 1, Rows: []AssuranceSurfaceRow{{SurfaceID: "apex:Example.run", CompileReady: true}}}
	if _, err := writeAssuranceArtifacts(report, filepath.Join(root, "MISSING.json"), filepath.Join(root, "MISSING.html"), filepath.Join(root, "MISSING_RECEIPT.json"), nil); err == nil {
		t.Fatal("writeAssuranceArtifacts accepted missing receipt inputs")
	}
	if _, err := writeAssuranceArtifacts(report, filepath.Join(root, "ASSURANCE.json"), filepath.Join(root, "ASSURANCE.html"), filepath.Join(root, "RECEIPT.json"), map[string]string{"/private/input": strings.Repeat("a", 64)}); err == nil {
		t.Fatal("writeAssuranceArtifacts accepted a private receipt key")
	}
}

func TestBuildReportRequiresAuthorizedExclusionRows(t *testing.T) {
	row := ExclusionPolicyRow{SurfaceID: "apex:Hosted.only()", Class: "hosted", Reason: "requires org identity"}
	if !policyAuthorizesRows([]ExclusionPolicyRow{row}, []ExclusionPolicyRow{row}) {
		t.Fatal("authorized exclusion row was rejected")
	}
	if policyAuthorizesRows([]ExclusionPolicyRow{row}, []ExclusionPolicyRow{{SurfaceID: row.SurfaceID, Class: row.Class, Reason: "altered"}}) {
		t.Fatal("unauthorized exclusion row was accepted")
	}
}

func TestReportExclusionPartitionBindsExclusionPolicyHash(t *testing.T) {
	candidate := RuntimeArtifact{Commit: strings.Repeat("a", 40), OS: "darwin", Arch: "arm64", SHA256: strings.Repeat("b", 64)}
	tools := RuntimeArtifact{Commit: strings.Repeat("c", 40), OS: "darwin", Arch: "arm64", SHA256: strings.Repeat("d", 64)}
	plan := OraclePlan{Candidate: candidate, Tools: tools, LocalProofSHA256: strings.Repeat("e", 64), Rows: []OraclePlanRow{{SurfaceID: "apex:Hosted.only()", Action: oracleWaiver, ExclusionClass: "hosted", ExclusionReason: "requires org identity"}}}
	rows, err := exclusionRowsFromPlan(plan)
	if err != nil {
		t.Fatal(err)
	}
	const (
		planSHA            = "plan"
		profileSHA         = "profile"
		usageSHA           = "usage"
		decisionSHA        = "decision"
		exclusionPolicySHA = "exclusion-policy"
	)
	exclusion := ExclusionRequest{Candidate: candidate, Tools: tools, PlanSHA256: planSHA, ProfileSHA256: profileSHA, SealedUsageSHA256: usageSHA, DecisionSHA256: decisionSHA, LocalProofSHA256: plan.LocalProofSHA256, Rows: rows}
	authority := ExclusionAuthority{Candidate: candidate, Tools: tools, DecisionSHA256: decisionSHA, PolicySHA256: exclusionPolicySHA, Rows: rows}
	if !validReportExclusionPartition(exclusion, authority, plan, planSHA, profileSHA, usageSHA, decisionSHA, exclusionPolicySHA) {
		t.Fatal("rejected exclusion authority bound to its separate policy")
	}
	if validReportExclusionPartition(exclusion, authority, plan, planSHA, profileSHA, usageSHA, decisionSHA, "other-policy") {
		t.Fatal("accepted authority bound to a different exclusion policy")
	}
}

func TestDeriveAssuranceRowsSeparatesCompileTestRuntimeAndNonParity(t *testing.T) {
	usage := UsageReconciliation{Usage: []ReconciledUsageEntry{
		{UsageEntry: UsageEntry{UsageKey: "Runtime.run", Namespace: "Runtime", PrivateProdRefs: 1, RepositoryIDs: []string{"private-corpus-001"}}, Class: usageClassExact, SurfaceID: "apex:Runtime.run()"},
		{UsageEntry: UsageEntry{UsageKey: "Compile.only", Namespace: "Compile", PrivateTestRefs: 1, RepositoryIDs: []string{"private-corpus-002"}}, Class: usageClassExact, SurfaceID: "apex:Compile.only()"},
		{UsageEntry: UsageEntry{UsageKey: "Hosted.only", Namespace: "Hosted", PrivateProdRefs: 1, RepositoryIDs: []string{"private-corpus-002"}}, Class: usageClassExact, SurfaceID: "apex:Hosted.only()"},
	}}
	profile := AssuranceProfile{Rows: []AssuranceProfileRow{{SurfaceID: "apex:Runtime.run()", Namespace: "Runtime", Disposition: localRuntimeRequired}, {SurfaceID: "apex:Compile.only()", Namespace: "Compile", Disposition: compileShapeRequired}, {SurfaceID: "apex:Hosted.only()", Namespace: "Hosted", Disposition: "hosted-deferred"}}}
	proof := LocalProof{Surfaces: []LocalSurfaceProof{{SurfaceID: "apex:Runtime.run()", FixtureID: "runtime", Disposition: localRuntimeRequired, RuntimeObserved: true}, {SurfaceID: "apex:Compile.only()", FixtureID: "compile", Disposition: compileShapeRequired, CompilePassed: true}}}
	plan := OraclePlan{Rows: []OraclePlanRow{{SurfaceID: "apex:Runtime.run()", Action: oracleRuntime}, {SurfaceID: "apex:Compile.only()", Action: oracleCompile}, {SurfaceID: "apex:Hosted.only()", Action: oracleWaiver, ExclusionClass: "hosted", ExclusionReason: "requires org identity"}}}
	shards := []SalesforceShard{{Results: []SalesforceSurfaceResult{{SurfaceID: "apex:Runtime.run()", Kind: oracleRuntime, Passed: true}, {SurfaceID: "apex:Compile.only()", Kind: oracleCompile, Passed: true}}}}
	rows, err := deriveAssuranceRows(usage, profile, proof, plan, shards, map[string]bool{"private-corpus-001": true, "private-corpus-002": false})
	if err != nil {
		t.Fatalf("deriveAssuranceRows: %v", err)
	}
	bySurface := map[string]AssuranceSurfaceRow{}
	for _, row := range rows {
		bySurface[row.SurfaceID] = row
	}
	runtime, compile, hosted := bySurface["apex:Runtime.run()"], bySurface["apex:Compile.only()"], bySurface["apex:Hosted.only()"]
	if len(rows) != 3 || !runtime.CompileReady || !runtime.TestReady || !runtime.RuntimeParityReady || !compile.CompileReady || compile.TestReady || compile.RuntimeParityReady || !hosted.NonParity {
		t.Fatalf("rows = %#v", rows)
	}
}

func TestDeriveAssuranceRowsPreservesLocalReadinessForNonParity(t *testing.T) {
	usage := UsageReconciliation{Usage: []ReconciledUsageEntry{{UsageEntry: UsageEntry{UsageKey: "Local.run", Namespace: "Local", RepositoryIDs: []string{"private-corpus-001"}}, Class: usageClassExact, SurfaceID: "apex:Local.run()"}}}
	profile := AssuranceProfile{Rows: []AssuranceProfileRow{{SurfaceID: "apex:Local.run()", Namespace: "Local", Disposition: localRuntimeRequired}}}
	proof := LocalProof{Surfaces: []LocalSurfaceProof{{SurfaceID: "apex:Local.run()", FixtureID: "local", Disposition: localRuntimeRequired, RuntimeObserved: true}}}
	plan := OraclePlan{Rows: []OraclePlanRow{{SurfaceID: "apex:Local.run()", Action: oracleLocalContractOnly, ExclusionClass: "policy-local-only", ExclusionReason: "portable hosted execution is not applicable"}}}
	rows, err := deriveAssuranceRows(usage, profile, proof, plan, nil, map[string]bool{"private-corpus-001": true})
	if err != nil || len(rows) != 1 || !rows[0].CompileReady || !rows[0].TestReady || rows[0].RuntimeParityReady || !rows[0].NonParity {
		t.Fatalf("deriveAssuranceRows = %#v, %v", rows, err)
	}
}

func TestDeriveAssuranceRowsUsesCanonicalProfileNamespaceForAliases(t *testing.T) {
	usage := UsageReconciliation{Usage: []ReconciledUsageEntry{
		{UsageEntry: UsageEntry{UsageKey: "Apex", Namespace: "Apex", RepositoryIDs: []string{"private-corpus-001"}}, Class: usageClassCanonicalAlias, SurfaceID: "apex:Component.apex.page"},
		{UsageEntry: UsageEntry{UsageKey: "Component.apex", Namespace: "Component.apex", RepositoryIDs: []string{"private-corpus-001"}}, Class: usageClassAggregateParent, SurfaceID: "apex:Component.apex.page"},
	}}
	profile := AssuranceProfile{Rows: []AssuranceProfileRow{{SurfaceID: "apex:Component.apex.page", Namespace: "Component.apex", Disposition: compileShapeRequired}}}
	proof := LocalProof{Surfaces: []LocalSurfaceProof{{SurfaceID: "apex:Component.apex.page", FixtureID: "component", Disposition: compileShapeRequired, CompilePassed: true}}}
	plan := OraclePlan{Rows: []OraclePlanRow{{SurfaceID: "apex:Component.apex.page", Action: oracleLocalContractOnly, ExclusionClass: "policy-local-only", ExclusionReason: "hosted execution is intentionally excluded"}}}
	rows, err := deriveAssuranceRows(usage, profile, proof, plan, nil, map[string]bool{"private-corpus-001": true})
	if err != nil || len(rows) != 1 || rows[0].Namespace != "Component.apex" {
		t.Fatalf("deriveAssuranceRows = %#v, %v", rows, err)
	}
}

func TestRepositoryTestReadinessKeepsRepositoriesWithoutTestsCompileOnly(t *testing.T) {
	artifact := RuntimeArtifact{Commit: strings.Repeat("a", 40), OS: "darwin", Arch: "arm64", SHA256: strings.Repeat("b", 64)}
	merge := ReplayMerge{Candidate: artifact, Tools: artifact, Inventory: InventoryManifest{SchemaVersion: 1, InventorySHA256: strings.Repeat("c", 64)}, Repositories: []RepositorySpec{{ID: "private-corpus-001", AssignedHost: "local", LocalTests: "required"}, {ID: "private-corpus-002", AssignedHost: "local", LocalTests: "tests-not-present", LocalTestsReason: "no Apex test classes found in snapshot"}}}
	check := CommandResult{Command: []string{"check"}, CommandSpecSHA256: commandSpecSHA256(replayCommandFor("", "check")), ExitCode: 0, Passed: true, StdoutSHA256: strings.Repeat("d", 64), StderrSHA256: strings.Repeat("e", 64)}
	test := CommandResult{Command: []string{"test"}, CommandSpecSHA256: commandSpecSHA256(replayCommandFor("", "test")), ExitCode: 0, Passed: true, StdoutSHA256: strings.Repeat("f", 64), StderrSHA256: strings.Repeat("1", 64)}
	shards := []ReplayShard{{Candidate: artifact, Tools: artifact, Host: "local", OS: "darwin", Arch: "arm64", Status: "pass", Repositories: []ReplayRepositoryResult{{RepositoryID: "private-corpus-001", Check: check, CheckSpecSHA256: check.CommandSpecSHA256, LocalTest: &test, LocalTestSpecSHA256: test.CommandSpecSHA256}, {RepositoryID: "private-corpus-002", Check: check, CheckSpecSHA256: check.CommandSpecSHA256}}}}
	ready, err := repositoryTestReadiness(merge, shards)
	if err != nil {
		t.Fatal(err)
	}
	if !ready["private-corpus-001"] || ready["private-corpus-002"] {
		t.Fatalf("readiness = %#v", ready)
	}
}

func TestDeriveRepositoryAssuranceRowsKeepsEveryRepositoryAndSurfaceDistinct(t *testing.T) {
	rows := []AssuranceSurfaceRow{
		{SurfaceID: "apex:Shared.run()", RepositoryIDs: []string{"private-corpus-001", "private-corpus-002"}, CompileReady: true, TestReady: true, RuntimeParityReady: true},
		{SurfaceID: "apex:OnlyOne.run()", RepositoryIDs: []string{"private-corpus-001"}, CompileReady: true},
	}
	pairs, summaries, err := deriveRepositoryAssuranceRows(InventorySpec{Repositories: []InventoryEntry{{ID: "private-corpus-001"}, {ID: "private-corpus-002"}}}, rows, map[string]bool{"private-corpus-001": true, "private-corpus-002": false})
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 3 || len(summaries) != 2 {
		t.Fatalf("pairs=%#v summaries=%#v", pairs, summaries)
	}
	if pairs[0].RepositoryID == pairs[1].RepositoryID && pairs[0].SurfaceID == pairs[1].SurfaceID {
		t.Fatalf("repository/surface pair was collapsed: %#v", pairs)
	}
	for _, pair := range pairs {
		if pair.RepositoryID == "private-corpus-002" && pair.TestReady {
			t.Fatalf("pair incorrectly inherited another repository's test readiness: %#v", pair)
		}
	}
	if summaries[1].RepositoryID != "private-corpus-002" || summaries[1].SurfaceCount != 1 || !summaries[1].CompileReady || summaries[1].TestReady || summaries[1].RuntimeParityReady {
		t.Fatalf("repository summary lost per-repository readiness: %#v", summaries)
	}
	if summaries[0].RuntimeParityReady {
		t.Fatalf("repository summary widened runtime readiness across a compile-only surface: %#v", summaries[0])
	}
}

func TestDeriveRepositoryAssuranceRowsMakesNonParityMutuallyExclusive(t *testing.T) {
	rows := []AssuranceSurfaceRow{
		{SurfaceID: "apex:Runtime.run()", RepositoryIDs: []string{"private-corpus-001"}, CompileReady: true, TestReady: true, RuntimeParityReady: true},
		{SurfaceID: "apex:Hosted.run()", RepositoryIDs: []string{"private-corpus-001"}, NonParity: true, ExclusionClass: "hosted", ExclusionReason: "requires org identity"},
	}
	_, summaries, err := deriveRepositoryAssuranceRows(InventorySpec{Repositories: []InventoryEntry{{ID: "private-corpus-001"}}}, rows, map[string]bool{"private-corpus-001": true})
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 || !summaries[0].NonParity || summaries[0].CompileReady || summaries[0].TestReady || summaries[0].RuntimeParityReady {
		t.Fatalf("summary retained readiness alongside non-parity: %#v", summaries)
	}
}
