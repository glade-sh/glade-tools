package corpusassurance

import (
	"os"
	"path/filepath"
	"strings"
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
}

func TestBuildAssuranceReportRequiresDirectEvidencePaths(t *testing.T) {
	if _, err := BuildAssuranceReport(AssuranceReportRequest{}); err == nil {
		t.Fatal("BuildAssuranceReport accepted missing direct evidence paths")
	}
}

func TestWriteAssuranceHTMLIsSelfContainedAndCreateOnly(t *testing.T) {
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
	for _, text := range []string{"private-corpus-001", "apex:Example.run", "id=\"assurance-data\"", "id=\"namespace\"", "id=\"disposition\"", "id=\"repository\"", "id=\"evidence\"", "id=\"exclusion\"", "id=\"text\""} {
		if !strings.Contains(string(html), text) {
			t.Fatalf("HTML misses %q", text)
		}
	}
	if err := WriteAssuranceHTML(reportPath, outputPath); err == nil {
		t.Fatal("WriteAssuranceHTML overwrote output")
	}
}

func TestWriteAssuranceArtifactsWritesAnAcyclicReceiptLast(t *testing.T) {
	root := t.TempDir()
	jsonPath, htmlPath, receiptPath := filepath.Join(root, "ASSURANCE.json"), filepath.Join(root, "ASSURANCE.html"), filepath.Join(root, "RECEIPT.json")
	report := AssuranceReport{SchemaVersion: 1, Rows: []AssuranceSurfaceRow{{SurfaceID: "apex:Example.run", CompileReady: true}}}
	receipt, err := WriteAssuranceArtifacts(report, jsonPath, htmlPath, receiptPath)
	if err != nil {
		t.Fatalf("WriteAssuranceArtifacts: %v", err)
	}
	if receipt.AssuranceSHA256 != localProofFileSHA256(t, jsonPath) || receipt.HTMLSHA256 != localProofFileSHA256(t, htmlPath) || receipt.ReceiptSHA256 != "" {
		t.Fatalf("receipt = %#v", receipt)
	}
	if _, err := os.Stat(receiptPath); err != nil {
		t.Fatal(err)
	}
}

func TestWriteAssuranceArtifactsRejectsPrivateReceiptKeys(t *testing.T) {
	root := t.TempDir()
	report := AssuranceReport{SchemaVersion: 1, Rows: []AssuranceSurfaceRow{{SurfaceID: "apex:Example.run", CompileReady: true}}}
	if _, err := writeAssuranceArtifacts(report, filepath.Join(root, "ASSURANCE.json"), filepath.Join(root, "ASSURANCE.html"), filepath.Join(root, "RECEIPT.json"), map[string]string{"/Users/matt/private": strings.Repeat("a", 64)}); err == nil {
		t.Fatal("WriteAssuranceArtifacts accepted a private receipt key")
	}
}

func TestDeriveAssuranceRowsSeparatesCompileTestRuntimeAndNonParity(t *testing.T) {
	usage := UsageReconciliation{Usage: []ReconciledUsageEntry{
		{UsageEntry: UsageEntry{UsageKey: "Runtime.run", Namespace: "Runtime", PrivateProdRefs: 1, RepositoryIDs: []string{"private-corpus-001"}}, Class: usageClassExact, SurfaceID: "apex:Runtime.run()"},
		{UsageEntry: UsageEntry{UsageKey: "Compile.only", Namespace: "Compile", PrivateTestRefs: 1, RepositoryIDs: []string{"private-corpus-002"}}, Class: usageClassExact, SurfaceID: "apex:Compile.only()"},
		{UsageEntry: UsageEntry{UsageKey: "Hosted.only", Namespace: "Hosted", PrivateProdRefs: 1, RepositoryIDs: []string{"private-corpus-002"}}, Class: usageClassExact, SurfaceID: "apex:Hosted.only()"},
	}}
	profile := AssuranceProfile{Rows: []AssuranceProfileRow{{SurfaceID: "apex:Runtime.run()", Namespace: "Runtime", Disposition: localRuntimeRequired}, {SurfaceID: "apex:Compile.only()", Namespace: "Compile", Disposition: compileShapeRequired}, {SurfaceID: "apex:Hosted.only()", Namespace: "Hosted", Disposition: "hosted-deferred"}}}
	proof := LocalProof{Surfaces: []LocalSurfaceProof{{SurfaceID: "apex:Runtime.run()", FixtureID: "runtime", Disposition: localRuntimeRequired, RuntimeObserved: true, CompilePassed: true, CheckPassed: true}, {SurfaceID: "apex:Compile.only()", FixtureID: "compile", Disposition: compileShapeRequired, CompilePassed: true, CheckPassed: true}}}
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

func TestRepositoryTestReadinessKeepsRepositoriesWithoutTestsCompileOnly(t *testing.T) {
	artifact := RuntimeArtifact{Commit: strings.Repeat("a", 40), OS: "darwin", Arch: "arm64", SHA256: strings.Repeat("b", 64)}
	merge := ReplayMerge{Candidate: artifact, Tools: artifact, Inventory: InventoryManifest{SchemaVersion: 1, InventorySHA256: strings.Repeat("c", 64)}, Repositories: []RepositorySpec{{ID: "private-corpus-001", LocalTests: "required"}, {ID: "private-corpus-002", LocalTests: "none"}}}
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
