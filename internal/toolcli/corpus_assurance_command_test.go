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
	for _, command := range []string{"prepare", "usage-draft", "usage", "replay", "merge-replay", "local-proof", "release-validate", "oracle-plan", "exclusion-request", "authorize-exclusions", "oracle-bundle", "org-create", "org-preflight", "salesforce-run", "org-cleanup", "report", "cleanup"} {
		if !strings.Contains(stdout.String(), "glade-tools corpus assurance "+command+" ") {
			t.Fatalf("help omits %q:\n%s", command, stdout.String())
		}
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
	attempt := corpusassurance.AssuranceAttempt{SchemaVersion: 1, InventorySHA256: inventoryHash, CandidateAuthoritySHA256: strings.Repeat("a", 64), Candidate: corpusassurance.RuntimeArtifact{Commit: strings.Repeat("b", 40), OS: "darwin", Arch: "arm64", SHA256: strings.Repeat("c", 64)}, Tools: corpusassurance.RuntimeArtifact{Commit: strings.Repeat("d", 40), OS: "darwin", Arch: "arm64", SHA256: strings.Repeat("e", 64)}}
	writeCorpusAssuranceJSON(t, attemptPath, attempt)
	prepared := filepath.Join(root, "prepared")
	if _, err := corpusassurance.PrepareInventory(inventoryPath, attemptPath, prepared); err != nil {
		t.Fatal(err)
	}
	ledgerPath := filepath.Join(root, "LEDGER.json")
	ledger := surfaceledger.SurfaceLedger{SchemaVersion: surfaceledger.SchemaVersion, Rows: []surfaceledger.SurfaceLedgerRow{{SurfaceID: "apex:System.debug", Product: surfaceledger.ProductApex, Namespace: "System", MemberName: "debug"}}}
	writeCorpusAssuranceJSON(t, ledgerPath, ledger)
	policyPath := filepath.Join(root, "POLICY.json")
	policy := surfaceledger.SupportPolicy{Rules: []surfaceledger.SupportPolicyRule{{Namespace: "System", Disposition: surfaceledger.DispositionLocalRuntimeRequired, Reason: "test"}}}
	writeCorpusAssuranceJSON(t, policyPath, policy)
	profilePath := filepath.Join(root, "PROFILE.json")
	profile := surfaceledger.SupportProfile{Rows: []surfaceledger.SupportProfileRow{{SurfaceID: "apex:System.debug", Disposition: surfaceledger.DispositionLocalRuntimeRequired}}, Inputs: &surfaceledger.SupportProfileInputs{Files: []surfaceledger.SupportProfileInput{{Name: "ledger", SHA256: corpusAssuranceFileSHA256(t, ledgerPath)}, {Name: "policy", SHA256: corpusAssuranceFileSHA256(t, policyPath)}}}}
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
