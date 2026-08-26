package toolcli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/corpusassurance"
	"github.com/glade-sh/glade/tools/internal/surfaceledger"
)

func TestCorpusAssuranceHelpListsSealedWorkflow(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"corpus", "assurance", "--help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run returned %d, stderr=%s", code, stderr.String())
	}
	commands := []string{"campaign", "candidate-build", "candidate-authority", "attempt-init", "attempt", "prepare", "usage-draft", "usage", "replay", "merge-replay", "surface-scope", "surface-terminal-authority", "surface-local-proof-plan", "surface-wave-plan", "surface-oracle-index", "local-proof-plan", "local-proof", "release-validate", "oracle-profile", "oracle-directives-draft", "oracle-plan", "exclusion-request", "authorize-exclusions", "dev-hub-authority", "oracle-bundle", "org-create", "org-preflight", "salesforce-dispatch", "salesforce-run", "org-cleanup", "salesforce-reconcile", "remote-failure-preserve", "review-index", "cleanup", "report"}
	for _, command := range commands {
		if !strings.Contains(stdout.String(), "glade-tools corpus assurance "+command+" ") {
			t.Fatalf("help omits %q:\n%s", command, stdout.String())
		}
	}
	last := -1
	for _, command := range []string{"campaign", "candidate-build", "candidate-authority", "attempt-init", "prepare", "usage-draft", "usage", "surface-scope", "surface-terminal-authority", "surface-local-proof-plan", "surface-wave-plan", "surface-oracle-index", "local-proof-plan", "local-proof", "release-validate", "oracle-profile", "oracle-directives-draft", "oracle-plan", "exclusion-request", "authorize-exclusions", "dev-hub-authority", "oracle-bundle", "salesforce-dispatch", "salesforce-run", "salesforce-reconcile", "remote-failure-preserve", "review-index", "cleanup", "report"} {
		position := strings.Index(stdout.String(), "glade-tools corpus assurance "+command+" ")
		if position <= last {
			t.Fatalf("help order moved %q after position %d:\n%s", command, last, stdout.String())
		}
		last = position
	}
	for _, flag := range []string{"--replay-host-manifest <manifest.json>", "--replay-shard <REPLAY_SHARD.json>"} {
		if !strings.Contains(stdout.String(), flag) {
			t.Fatalf("help omits %q:\n%s", flag, stdout.String())
		}
	}
	if !strings.Contains(stdout.String(), "attempt --inventory-spec <IN_SCOPE.json> --candidate-authority <CANDIDATE_AUTHORITY.json> --candidate <glade> --candidate-root <glade-root> --tools <glade-tools> --tools-root <glade-tools-root> --replay-cleanup-authority") {
		t.Fatalf("attempt help omits pre-bound cleanup authorities:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "candidate-authority --candidate-root <glade-root> --tools-root <glade-tools-root>") {
		t.Fatalf("candidate-authority help omits the tools source binding:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "[--org-preflight <ORG_PREFLIGHT.json>]") {
		t.Fatalf("org-cleanup help does not document invalidated-receipt recovery:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "oracle-bundle --attempt <ATTEMPT.json> --remote-cleanup-authority <SALESFORCE_REMOTE_CLEANUP_AUTHORITY.json>") {
		t.Fatalf("oracle-bundle help omits the bound Salesforce cleanup authority:\n%s", stdout.String())
	}
}

func TestCorpusAssuranceSurfaceOracleIndexRejectsInvalidUsage(t *testing.T) {
	valid := []string{
		"corpus", "assurance", "surface-oracle-index",
		"--scope", "/scope.json",
		"--reviewed-runtime-batch", "/runtime-batch",
		"--output", "/index.json",
	}
	for _, missing := range []string{"--scope", "--output"} {
		t.Run("missing "+missing, func(t *testing.T) {
			args := removeCorpusAssuranceFlag(valid, missing)
			var stdout, stderr bytes.Buffer
			if code := Run(context.Background(), args, &stdout, &stderr); code == 0 || !strings.Contains(stderr.String(), "required corpus assurance flag is missing") {
				t.Fatalf("missing %s accepted: code=%d stdout=%q stderr=%q", missing, code, stdout.String(), stderr.String())
			}
		})
	}
	missingBatch := removeCorpusAssuranceFlag(valid, "--reviewed-runtime-batch")
	var missingBatchStdout, missingBatchStderr bytes.Buffer
	if code := Run(context.Background(), missingBatch, &missingBatchStdout, &missingBatchStderr); code == 0 || !strings.Contains(strings.ToLower(missingBatchStderr.String()), "reviewed runtime batch") {
		t.Fatalf("missing reviewed runtime batch accepted: code=%d stdout=%q stderr=%q", code, missingBatchStdout.String(), missingBatchStderr.String())
	}
	for _, missingValue := range []string{"--scope", "--reviewed-runtime-batch", "--output"} {
		t.Run("missing value "+missingValue, func(t *testing.T) {
			args := append(removeCorpusAssuranceFlag(valid, missingValue), missingValue)
			var stdout, stderr bytes.Buffer
			if code := Run(context.Background(), args, &stdout, &stderr); code == 0 {
				t.Fatalf("missing value for %s accepted: stdout=%q stderr=%q", missingValue, stdout.String(), stderr.String())
			}
		})
	}
	for _, duplicate := range []string{"--scope", "--output"} {
		t.Run("duplicate "+duplicate, func(t *testing.T) {
			args := append(append([]string(nil), valid...), duplicate, "duplicate-value")
			var stdout, stderr bytes.Buffer
			if code := Run(context.Background(), args, &stdout, &stderr); code == 0 || !strings.Contains(strings.ToLower(stderr.String()), "duplicate") {
				t.Fatalf("duplicate %s accepted: code=%d stdout=%q stderr=%q", duplicate, code, stdout.String(), stderr.String())
			}
		})
	}
	duplicateRoot := append(append([]string(nil), valid...), "--reviewed-runtime-batch", "/runtime-batch")
	var duplicateStdout, duplicateStderr bytes.Buffer
	if code := Run(context.Background(), duplicateRoot, &duplicateStdout, &duplicateStderr); code == 0 || !strings.Contains(strings.ToLower(duplicateStderr.String()), "duplicate") {
		t.Fatalf("duplicate reviewed runtime batch accepted: code=%d stdout=%q stderr=%q", code, duplicateStdout.String(), duplicateStderr.String())
	}
	distinctRoots := append(append([]string(nil), valid...), "--reviewed-runtime-batch", "/another-runtime-batch")
	var distinctStdout, distinctStderr bytes.Buffer
	if code := Run(context.Background(), distinctRoots, &distinctStdout, &distinctStderr); code == 0 || strings.Contains(strings.ToLower(distinctStderr.String()), "duplicate") {
		t.Fatalf("distinct reviewed runtime batches were not accepted by the parser: code=%d stdout=%q stderr=%q", code, distinctStdout.String(), distinctStderr.String())
	}
	for _, unknown := range []string{"--unknown", "--predecessor"} {
		t.Run("unknown "+unknown, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			args := append(append([]string(nil), valid...), unknown, "value")
			code := Run(context.Background(), args, &stdout, &stderr)
			message := strings.ToLower(stderr.String())
			if code == 0 || (!strings.Contains(message, "unknown") && !strings.Contains(message, "not defined")) {
				t.Fatalf("unknown flag %s accepted: code=%d stdout=%q stderr=%q", unknown, code, stdout.String(), stderr.String())
			}
		})
	}
}

func TestCorpusAssuranceSurfaceWavePlanRejectsPositionalSurfaceIDs(t *testing.T) {
	args := []string{
		"corpus", "assurance", "surface-wave-plan",
		"--scope", "/scope.json", "--profile", "/profile.json", "--local-proof", "/proof.json",
		"--fixture-manifest", "/fixtures.json", "--coverage", "/coverage.json", "--output", "/wave.json",
		"apex:System.run",
	}
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), args, &stdout, &stderr); code == 0 || !strings.Contains(stderr.String(), "does not accept positional SurfaceIDs") {
		t.Fatalf("positional SurfaceID accepted: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestCorpusAssuranceSurfaceWavePlanRejectsMoreThanNineShards(t *testing.T) {
	args := []string{
		"corpus", "assurance", "surface-wave-plan",
		"--scope", "/scope.json", "--profile", "/profile.json", "--local-proof", "/proof.json",
		"--fixture-manifest", "/fixtures.json", "--coverage", "/coverage.json", "--shards", "10", "--output", "/wave.json",
	}
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), args, &stdout, &stderr); code == 0 || !strings.Contains(stderr.String(), "shard count") {
		t.Fatalf("invalid shard count accepted: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func removeCorpusAssuranceFlag(args []string, name string) []string {
	result := make([]string, 0, len(args)-2)
	for index := 0; index < len(args); index++ {
		if args[index] == name {
			index++
			continue
		}
		result = append(result, args[index])
	}
	return result
}

func TestCorpusAssuranceSubcommandHelpPrintsUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"corpus", "assurance", "local-proof", "--help"}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "glade-tools corpus assurance local-proof ") {
		t.Fatalf("subcommand help code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestCorpusAssuranceCampaignRequiresSpecAndState(t *testing.T) {
	assertCorpusAssuranceCommandRejectsMissingFlags(t, "campaign")

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"corpus", "assurance", "campaign", "--promote", "--out", filepath.Join(t.TempDir(), "promotion")}, &stdout, &stderr)
	if code == 0 || !strings.Contains(stderr.String(), "required corpus assurance flag is missing") {
		t.Fatalf("campaign promotion accepted missing spec/state: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestCorpusAssuranceOracleProfile(t *testing.T) {
	assertCorpusAssuranceCommandRejectsMissingFlags(t, "oracle-profile")
}

func TestCorpusAssuranceCandidate(t *testing.T) {
	assertCorpusAssuranceCommandRejectsMissingFlags(t, "candidate-build")
	assertCorpusAssuranceCommandRejectsMissingFlags(t, "candidate-authority")
}

func TestCorpusAssuranceSurfaceScope(t *testing.T) {
	assertCorpusAssuranceCommandRejectsMissingFlags(t, "surface-scope")
	assertCorpusAssuranceCommandRejectsMissingFlags(t, "surface-local-proof-plan")
}

func TestCorpusAssuranceSurfaceScopeFromOraclePlan(t *testing.T) {
	root := t.TempDir()
	profilePath := filepath.Join(root, "profile.json")
	profile := corpusassurance.AssuranceProfile{
		SchemaVersion: 1, SourceProfileSHA256: strings.Repeat("1", 64), LedgerSHA256: strings.Repeat("2", 64), PolicySHA256: strings.Repeat("3", 64),
		Total: 5, ByDisposition: map[string]int{"local-runtime-required": 2, "deterministic-mock-required": 2, "compile-shape-required": 1},
		Rows: []corpusassurance.AssuranceProfileRow{
			{SurfaceID: "apex:System.Boolean", Disposition: "local-runtime-required"},
			{SurfaceID: "apex:System.Integer", Disposition: "local-runtime-required"},
			{SurfaceID: "apex:System.Long", Disposition: "deterministic-mock-required"},
			{SurfaceID: "apex:System.String", Disposition: "deterministic-mock-required"},
			{SurfaceID: "apex:System.System", Disposition: "compile-shape-required"},
		},
	}
	writeCorpusAssuranceJSON(t, profilePath, profile)
	artifact := corpusassurance.RuntimeArtifact{Commit: strings.Repeat("a", 40), OS: "darwin", Arch: "arm64", SHA256: strings.Repeat("b", 64)}
	planPath := filepath.Join(root, "ORACLE_PLAN.json")
	writeCorpusAssuranceJSON(t, planPath, corpusassurance.OraclePlan{
		Candidate: artifact, Tools: artifact, ProfileSHA256: corpusAssuranceFileSHA256(t, profilePath),
		Rows: []corpusassurance.OraclePlanRow{
			{SurfaceID: "apex:System.Boolean", Action: "runtime"},
			{SurfaceID: "apex:System.Integer", Action: "local-contract-only", ExclusionClass: "local-only", ExclusionReason: "not Salesforce parity"},
			{SurfaceID: "apex:System.Long", Action: "local-contract-only", ExclusionClass: "local-only", ExclusionReason: "not Salesforce parity"},
			{SurfaceID: "apex:System.String", Action: "local-contract-only", ExclusionClass: "local-only", ExclusionReason: "not Salesforce parity"},
			{SurfaceID: "apex:System.System", Action: "compile"},
		},
	})
	outputPath := filepath.Join(root, "scope.json")
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"corpus", "assurance", "surface-scope", "--oracle-plan", planPath, "--profile", profilePath, "--output", outputPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("surface-scope code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var scope corpusassurance.SurfaceOracleScope
	data, err := os.ReadFile(outputPath)
	if err != nil || json.Unmarshal(data, &scope) != nil {
		t.Fatalf("read campaign scope: %v", err)
	}
	if scope.Kind != "oracle-plan" || scope.Total != 2 || len(scope.Rows) != 2 || scope.Rows[0].Action != "runtime" || scope.Rows[1].SurfaceID != "apex:System.System" || scope.Rows[1].Disposition != "compile-shape-required" || scope.Rows[1].Action != "compile" {
		t.Fatalf("campaign scope = %#v", scope)
	}

	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"corpus", "assurance", "surface-scope", "--oracle-plan", planPath, "--profile", profilePath, "--source-profile", profilePath, "--output", filepath.Join(root, "mixed.json")}, &stdout, &stderr)
	if code == 0 || !strings.Contains(stderr.String(), "cannot be combined") {
		t.Fatalf("mixed surface-scope modes: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestCorpusAssuranceAttempt(t *testing.T) {
	assertCorpusAssuranceCommandRejectsMissingFlags(t, "attempt-init")
	assertCorpusAssuranceCommandRejectsMissingFlags(t, "attempt")
}

func TestCorpusAssuranceOracleDirectives(t *testing.T) {
	assertCorpusAssuranceCommandRejectsMissingFlags(t, "oracle-directives-draft")
}

func TestCorpusAssuranceOraclePlan(t *testing.T) {
	assertCorpusAssuranceCommandRejectsMissingFlags(t, "oracle-plan")
}

func TestCorpusAssuranceDevHubAuthority(t *testing.T) {
	assertCorpusAssuranceCommandRejectsMissingFlags(t, "dev-hub-authority")
}

func TestCorpusAssuranceSalesforceReconcile(t *testing.T) {
	assertCorpusAssuranceCommandRejectsMissingFlags(t, "salesforce-reconcile")
}

func TestCorpusAssuranceReport(t *testing.T) {
	assertCorpusAssuranceCommandRejectsMissingFlags(t, "report")
}

func TestCorpusAssuranceReportRequiresRetainedSalesforceReconciliation(t *testing.T) {
	if err := requireRetainedSalesforceReconciliation("", ""); err == nil {
		t.Fatal("report accepted live Salesforce shard inputs without retained reconciliation")
	}
}

func TestCorpusAssuranceRemoteFailurePreserve(t *testing.T) {
	assertCorpusAssuranceCommandRejectsMissingFlags(t, "remote-failure-preserve")
}

func TestCorpusAssuranceReviewIndex(t *testing.T) {
	assertCorpusAssuranceCommandRejectsMissingFlags(t, "review-index")
	root := t.TempDir()
	attemptPath := filepath.Join(root, "ATTEMPT.json")
	attempt := corpusassurance.AssuranceAttempt{
		SchemaVersion:            1,
		InventorySHA256:          strings.Repeat("a", 64),
		CandidateAuthoritySHA256: strings.Repeat("b", 64),
		Candidate:                corpusassurance.RuntimeArtifact{Commit: strings.Repeat("c", 40), OS: "darwin", Arch: "arm64", SHA256: strings.Repeat("d", 64)},
		Tools:                    corpusassurance.RuntimeArtifact{Commit: strings.Repeat("e", 40), OS: "darwin", Arch: "arm64", SHA256: strings.Repeat("f", 64)},
		RemoteCleanupAuthoritySHA256: map[string]string{
			"replay-worker":     strings.Repeat("0", 64),
			"salesforce-worker": strings.Repeat("1", 64),
		},
	}
	writeCorpusAssuranceJSON(t, attemptPath, attempt)
	artifact := filepath.Join(root, "failure.log")
	if err := os.WriteFile(artifact, []byte("retained\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "REVIEW_INDEX.json")
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"corpus", "assurance", "review-index", "--attempt", attemptPath, "--artifact", artifact, "--output", output}, &stdout, &stderr); code != 0 {
		t.Fatalf("review-index code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "review-index: rows=1") {
		t.Fatalf("review-index output=%q", stdout.String())
	}
	if _, err := corpusassurance.LoadReviewIndex(output); err != nil {
		t.Fatalf("LoadReviewIndex: %v", err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run(context.Background(), []string{"corpus", "assurance", "review-index", "--verify", "--index", output}, &stdout, &stderr); code != 0 {
		t.Fatalf("review-index verify code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "review-index-verify: rows=1") {
		t.Fatalf("review-index verify output=%q", stdout.String())
	}
}

func assertCorpusAssuranceCommandRejectsMissingFlags(t *testing.T, command string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"corpus", "assurance", command}, &stdout, &stderr); code == 0 {
		t.Fatalf("%s unexpectedly accepted missing flags", command)
	}
}

func TestCorpusAssuranceUsageDraftExecutes(t *testing.T) {
	root := t.TempDir()
	checkout := filepath.Join(root, "checkout")
	for _, args := range [][]string{{"init", checkout}, {"-C", checkout, "config", "user.email", "test@example.invalid"}, {"-C", checkout, "config", "user.name", "Test"}} {
		if output, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %q: %v\n%s", args, err, output)
		}
	}
	if err := os.WriteFile(filepath.Join(checkout, "Sample.cls"), []byte("public class Sample { void run() { System.debug('one'); } }"), 0o600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("git", "-C", checkout, "add", ".").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, output)
	}
	if output, err := exec.Command("git", "-C", checkout, "commit", "-m", "fixture").CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, output)
	}
	head, err := exec.Command("git", "-C", checkout, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	inventoryPath := filepath.Join(root, "IN_SCOPE.json")
	inventory := corpusassurance.InventorySpec{SchemaVersion: 1, Scope: "private-corpus-assurance", Repositories: []corpusassurance.InventoryEntry{{ID: "private-corpus-001", CheckoutPath: checkout, ExpectedCommit: strings.TrimSpace(string(head))}}}
	writeCorpusAssuranceJSON(t, inventoryPath, inventory)
	inventoryHash := corpusAssuranceFileSHA256(t, inventoryPath)
	attemptPath := filepath.Join(root, "ATTEMPT.json")
	attempt := corpusassurance.AssuranceAttempt{SchemaVersion: 1, InventorySHA256: inventoryHash, CandidateAuthoritySHA256: strings.Repeat("a", 64), Candidate: corpusassurance.RuntimeArtifact{Commit: strings.Repeat("b", 40), OS: "darwin", Arch: "arm64", SHA256: strings.Repeat("c", 64)}, Tools: corpusassurance.RuntimeArtifact{Commit: strings.Repeat("d", 40), OS: "darwin", Arch: "arm64", SHA256: strings.Repeat("e", 64)}, RemoteCleanupAuthoritySHA256: map[string]string{"replay-worker": strings.Repeat("0", 64), "salesforce-worker": strings.Repeat("0", 64)}}
	writeCorpusAssuranceJSON(t, attemptPath, attempt)
	prepared := filepath.Join(root, "prepared")
	if _, err := corpusassurance.PrepareInventory(inventoryPath, attemptPath, prepared); err != nil {
		t.Fatal(err)
	}
	ledgerPath := filepath.Join(root, "LEDGER.json")
	ledger := surfaceledger.SurfaceLedger{SchemaVersion: surfaceledger.SchemaVersion, Rows: []surfaceledger.SurfaceLedgerRow{{SurfaceID: "apex:System.debug", Product: surfaceledger.ProductApex, Namespace: "System", MemberName: "debug"}}}
	policyPath := filepath.Join(root, "POLICY.json")
	policy := surfaceledger.SupportPolicy{Rules: []surfaceledger.SupportPolicyRule{{Namespace: "System", Disposition: surfaceledger.DispositionLocalRuntimeRequired, Reason: "test"}}}
	writeCorpusAssuranceJSON(t, policyPath, policy)
	profilePath := filepath.Join(root, "PROFILE.json")
	corpusUsagePath := filepath.Join(root, "CORPUS_USAGE.json")
	if err := os.WriteFile(corpusUsagePath, []byte(`{"usage":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshotBindings := make(map[string]string, 4)
	for _, name := range []string{"DOCS_SNAPSHOT.json", "ORG_SNAPSHOT.json", "GLADE_SNAPSHOT.json", "EVIDENCE_SNAPSHOT.json"} {
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
		snapshotBindings[name] = corpusAssuranceFileSHA256(t, path)
	}
	ledger.SourceSnapshotBindings = &surfaceledger.SourceSnapshotBindings{Files: snapshotBindings}
	writeCorpusAssuranceJSON(t, ledgerPath, ledger)
	profileInputs := []surfaceledger.SupportProfileInput{{Name: "ledger", Path: ledgerPath, SHA256: corpusAssuranceFileSHA256(t, ledgerPath)}, {Name: "policy", Path: policyPath, SHA256: corpusAssuranceFileSHA256(t, policyPath)}, {Name: "corpus-usage", Path: corpusUsagePath, SHA256: corpusAssuranceFileSHA256(t, corpusUsagePath)}}
	for _, name := range []string{"DOCS_SNAPSHOT.json", "ORG_SNAPSHOT.json", "GLADE_SNAPSHOT.json", "EVIDENCE_SNAPSHOT.json"} {
		path := filepath.Join(root, name)
		profileInputs = append(profileInputs, surfaceledger.SupportProfileInput{Name: name, Path: path, SHA256: corpusAssuranceFileSHA256(t, path)})
	}
	profile := surfaceledger.SupportProfile{Rows: []surfaceledger.SupportProfileRow{{SurfaceID: "apex:System.debug", Disposition: surfaceledger.DispositionLocalRuntimeRequired}}, Inputs: &surfaceledger.SupportProfileInputs{Files: profileInputs}}
	writeCorpusAssuranceJSON(t, profilePath, profile)
	outputPath := filepath.Join(root, "USAGE_DECISION_DRAFT.json")
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"corpus", "assurance", "usage-draft", "--inventory-spec", inventoryPath, "--ledger", ledgerPath, "--manifest", filepath.Join(prepared, "MANIFEST.json"), "--profile", profilePath, "--policy", policyPath, "--output", outputPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("usage-draft exit %d stderr=%q stdout=%q", code, stderr.String(), stdout.String())
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	var draft corpusassurance.UsageDecisionDraft
	if err := json.Unmarshal(data, &draft); err != nil || draft.RawUsageSHA256 == "" || len(draft.Unresolved) != 0 {
		t.Fatalf("draft=%#v err=%v", draft, err)
	}
}

func writeCorpusAssuranceJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func corpusAssuranceFileSHA256(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
