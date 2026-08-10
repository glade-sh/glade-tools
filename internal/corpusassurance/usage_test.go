package corpusassurance

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/surfaceledger"
)

func TestExtractUsageKeepsRepositoryOwnership(t *testing.T) {
	ledger := []surfaceledger.SurfaceLedgerRow{{SurfaceID: "apex:System.debug", Product: surfaceledger.ProductApex, Namespace: "System"}}
	first := writeUsageRepo(t, "public class First { void run() { System.debug('one'); } }")
	second := writeUsageRepo(t, "@isTest private class SecondTest { static void run() { System.debug('two'); } }")

	one, err := ExtractRepositoryUsage(ledger, usageRepositorySpec(t, "private-corpus-001", first), first)
	if err != nil {
		t.Fatal(err)
	}
	two, err := ExtractRepositoryUsage(ledger, usageRepositorySpec(t, "private-corpus-002", second), second)
	if err != nil {
		t.Fatal(err)
	}
	combined, err := CombineRepositoryUsage(usageInventory(t, usageRepositorySpec(t, "private-corpus-001", first), usageRepositorySpec(t, "private-corpus-002", second)), []RepositoryUsage{two, one})
	if err != nil {
		t.Fatal(err)
	}
	var got UsageEntry
	for _, entry := range combined.Usage {
		if entry.UsageKey == "System.debug" {
			got = entry
			break
		}
	}
	if got.UsageKey == "" {
		t.Fatalf("combined usage missing System.debug: %#v", combined.Usage)
	}
	if got.UsageKey != "System.debug" || got.PrivateProdRefs != 1 || got.PrivateTestRefs != 1 {
		t.Fatalf("combined usage = %#v", got)
	}
	if want := []string{"private-corpus-001", "private-corpus-002"}; !reflect.DeepEqual(got.RepositoryIDs, want) {
		t.Fatalf("repository IDs = %v, want %v", got.RepositoryIDs, want)
	}
	if want := usageBindingsSHA256(one, two); combined.RepositoryTreeBindingsSHA256 != want {
		t.Fatalf("repository tree bindings sha256 = %q, want %q", combined.RepositoryTreeBindingsSHA256, want)
	}
}

func TestExtractUsageRequiresMatchingSealedSnapshot(t *testing.T) {
	ledger := []surfaceledger.SurfaceLedgerRow{{SurfaceID: "apex:System.debug", Product: surfaceledger.ProductApex, Namespace: "System"}}
	root := writeUsageRepo(t, "public class Sample { void run() { System.debug('one'); } }")
	repository := usageRepositorySpec(t, "private-corpus-001", root)

	usage, err := ExtractRepositoryUsage(ledger, repository, root)
	if err != nil {
		t.Fatalf("ExtractRepositoryUsage: %v", err)
	}
	if got, want := usage.RootTreeSHA256, repository.TreeSHA256; got != want {
		t.Fatalf("usage root tree hash = %q, want %q", got, want)
	}
	writeUsageFixtureFile(t, root, "classes/Sample.cls", "public class Sample { void run() { System.debug('changed'); } }")
	if _, err := ExtractRepositoryUsage(ledger, repository, root); err == nil {
		t.Fatal("ExtractRepositoryUsage accepted a root that does not match its sealed tree hash")
	}
}

func TestExtractUsageRequiresTreeToRemainSealedAfterScan(t *testing.T) {
	ledger := []surfaceledger.SurfaceLedgerRow{{SurfaceID: "apex:System.debug", Product: surfaceledger.ProductApex, Namespace: "System"}}
	root := writeUsageRepo(t, "public class Sample { void run() { System.debug('one'); } }")
	repository := usageRepositorySpec(t, "private-corpus-001", root)

	original := buildCorpusUsage
	buildCorpusUsage = func(ledger []surfaceledger.SurfaceLedgerRow, publicRoot, publicFailRoot, privateRoot string) (surfaceledger.CorpusUsage, error) {
		usage, err := original(ledger, publicRoot, publicFailRoot, privateRoot)
		writeUsageFixtureFile(t, privateRoot, "classes/Sample.cls", "public class Sample { void run() { System.debug('changed'); } }")
		return usage, err
	}
	t.Cleanup(func() { buildCorpusUsage = original })

	if _, err := ExtractRepositoryUsage(ledger, repository, root); err == nil {
		t.Fatal("ExtractRepositoryUsage accepted a root changed after scanning")
	}
}

func TestUsageReconciliationLoadsAndBindsAuthoritativeFiles(t *testing.T) {
	attemptRoot := t.TempDir()
	root := filepath.Join(attemptRoot, "snapshots", "private-corpus-001")
	if err := os.MkdirAll(filepath.Join(root, "classes"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "classes", "Sample.cls"), []byte("public class Sample { void run() { System.debug('one'); } }"), 0o600); err != nil {
		t.Fatal(err)
	}
	repository := usageRepositorySpec(t, "private-corpus-001", root)
	inventoryPath := filepath.Join(attemptRoot, "IN_SCOPE.json")
	if err := WriteNewJSON(inventoryPath, InventorySpec{SchemaVersion: 1, Scope: "private-corpus-assurance", Repositories: []InventoryEntry{{ID: repository.ID, CheckoutPath: filepath.Join(attemptRoot, "checkout"), ExpectedCommit: repository.ExpectedCommit}}}); err != nil {
		t.Fatal(err)
	}
	ledgerPath := filepath.Join(t.TempDir(), "SURFACE_LEDGER.json")
	if err := WriteNewJSON(ledgerPath, surfaceledger.SurfaceLedger{SchemaVersion: surfaceledger.SchemaVersion, Rows: []surfaceledger.SurfaceLedgerRow{{SurfaceID: "apex:System.debug", Product: surfaceledger.ProductApex, Namespace: "System", MemberName: "debug"}}}); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(attemptRoot, "MANIFEST.json")
	if err := WriteNewJSON(manifestPath, InventoryManifest{SchemaVersion: 1, InventorySHA256: localProofFileSHA256(t, inventoryPath), Repositories: []RepositorySpec{repository}}); err != nil {
		t.Fatal(err)
	}
	usage, err := ExtractRepositoryUsageFromFiles(inventoryPath, ledgerPath, manifestPath, repository.ID)
	if err != nil {
		t.Fatalf("ExtractRepositoryUsageFromFiles: %v", err)
	}
	if usage.LedgerSHA256 != localProofFileSHA256(t, ledgerPath) || usage.RootManifestSHA256 != localProofFileSHA256(t, manifestPath) {
		t.Fatalf("file bindings = %#v", usage)
	}
	usagePath := filepath.Join(t.TempDir(), "repository-usage.json")
	if err := WriteNewJSON(usagePath, usage); err != nil {
		t.Fatal(err)
	}
	combined, err := combineRepositoryUsageFromFiles(inventoryPath, ledgerPath, manifestPath, []string{usagePath})
	if err != nil {
		t.Fatalf("CombineRepositoryUsageFromFiles: %v", err)
	}
	if combined.RootManifestSHA256 != localProofFileSHA256(t, manifestPath) || len(combined.RepositoryUsageSHA256) != 1 || combined.RepositoryUsageSHA256[0] != localProofFileSHA256(t, usagePath) {
		t.Fatalf("reconciliation bindings = %#v", combined)
	}
}

func TestCombineRepositoryUsageRejectsUnsealedRepositoryOwnership(t *testing.T) {
	first := validRepositoryUsage("private-corpus-001")
	second := RepositoryUsage{RepositoryID: "private-corpus-002", RootTreeSHA256: strings.Repeat("b", 64)}
	inventory := usageInventory(t, usageRepositoryBinding("private-corpus-001", strings.Repeat("a", 64)), usageRepositoryBinding("private-corpus-002", strings.Repeat("b", 64)))
	for name, repositories := range map[string][]RepositoryUsage{
		"missing inventory repository":      {first},
		"extra repository":                  {first, {RepositoryID: "private-corpus-003", RootTreeSHA256: strings.Repeat("c", 64)}},
		"mismatched repository tree":        {{RepositoryID: first.RepositoryID, RootTreeSHA256: strings.Repeat("c", 64)}, second},
		"duplicate repository ownership":    {first, first, second},
		"inconsistent repository ownership": {first, {RepositoryID: first.RepositoryID, RootTreeSHA256: strings.Repeat("c", 64)}, second},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := CombineRepositoryUsage(inventory, repositories); err == nil {
				t.Fatal("CombineRepositoryUsage accepted unsealed repository ownership")
			}
		})
	}
}

func TestCombineRepositoryUsageRejectsNegativeAndInconsistentUsage(t *testing.T) {
	usage := surfaceledger.CorpusUsageEntry{UsageKey: "System.debug", Namespace: "System", MemberName: "debug", PrivProdRefs: 1}
	inventory := usageInventory(t, usageRepositoryBinding("private-corpus-001", strings.Repeat("a", 64)), usageRepositoryBinding("private-corpus-002", strings.Repeat("b", 64)))
	for name, repositories := range map[string][]RepositoryUsage{
		"negative production count": {{RepositoryID: "private-corpus-001", RootTreeSHA256: strings.Repeat("a", 64), Usage: []surfaceledger.CorpusUsageEntry{{UsageKey: usage.UsageKey, Namespace: usage.Namespace, MemberName: usage.MemberName, PrivProdRefs: -1}}}, {RepositoryID: "private-corpus-002", RootTreeSHA256: strings.Repeat("b", 64)}},
		"negative test count":       {{RepositoryID: "private-corpus-001", RootTreeSHA256: strings.Repeat("a", 64), Usage: []surfaceledger.CorpusUsageEntry{{UsageKey: usage.UsageKey, Namespace: usage.Namespace, MemberName: usage.MemberName, PrivTestRefs: -1}}}, {RepositoryID: "private-corpus-002", RootTreeSHA256: strings.Repeat("b", 64)}},
		"duplicate usage ownership": {{RepositoryID: "private-corpus-001", RootTreeSHA256: strings.Repeat("a", 64), Usage: []surfaceledger.CorpusUsageEntry{usage, usage}}, {RepositoryID: "private-corpus-002", RootTreeSHA256: strings.Repeat("b", 64)}},
		"inconsistent usage identity": {
			{RepositoryID: "private-corpus-001", RootTreeSHA256: strings.Repeat("a", 64), Usage: []surfaceledger.CorpusUsageEntry{usage}},
			{RepositoryID: "private-corpus-002", RootTreeSHA256: strings.Repeat("b", 64), Usage: []surfaceledger.CorpusUsageEntry{{UsageKey: "System.debug", Namespace: "Other", MemberName: "debug", PrivProdRefs: 1}}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := CombineRepositoryUsage(inventory, repositories); err == nil {
				t.Fatal("CombineRepositoryUsage accepted invalid usage ownership")
			}
		})
	}
}

func TestReconcileUsageDerivesAutomaticMatchesAndRequiresManualDecisions(t *testing.T) {
	profile := []UsageProfileRow{
		{SurfaceID: "apex:System.debug(Object)", UsageKey: "System.debug"},
		{SurfaceID: "apex:Schema.Account", UsageKey: "Schema.Account"},
		{SurfaceID: "apex:Schema.Account.Name", UsageKey: "Schema.Account"},
	}
	usage := []UsageEntry{
		{UsageKey: "System.debug", Namespace: "System", PrivateProdRefs: 1},
		{UsageKey: "Schema.Account", Namespace: "Schema", PrivateTestRefs: 1},
		{UsageKey: "Local.Thing", Namespace: "Local", PrivateProdRefs: 1},
	}
	reconciled, err := reconcileUsage(profile, usage, []UsageDecision{
		{UsageKey: "Schema.Account", Class: usageClassCanonicalAlias, SurfaceID: "apex:Schema.Account", Reason: "aggregate source reference"},
		{UsageKey: "Local.Thing", Class: usageClassLocalSymbol, Reason: "application symbol"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(reconciled.Usage) != 3 {
		t.Fatalf("reconciled count = %d", len(reconciled.Usage))
	}
	want := map[string]struct{ class, surface string }{
		"System.debug":   {usageClassExact, "apex:System.debug(Object)"},
		"Schema.Account": {usageClassCanonicalAlias, "apex:Schema.Account"},
		"Local.Thing":    {usageClassLocalSymbol, ""},
	}
	for _, row := range reconciled.Usage {
		if got := want[row.UsageKey]; row.Class != got.class || row.SurfaceID != got.surface {
			t.Errorf("%s = %#v, want class=%q surface=%q", row.UsageKey, row, got.class, got.surface)
		}
	}
}

func TestReconcileUsageRejectsUnclassifiedAndInvalidDecisions(t *testing.T) {
	profile := []UsageProfileRow{{SurfaceID: "apex:Schema.Account", UsageKey: "Schema.Account"}, {SurfaceID: "apex:Schema.Account.Name", UsageKey: "Schema.Account"}}
	usage := []UsageEntry{{UsageKey: "Schema.Account", Namespace: "Schema", PrivateProdRefs: 1}}
	if _, err := reconcileUsage(profile, usage, nil); err == nil {
		t.Fatal("reconcileUsage accepted an ambiguous usage key")
	}
	for _, decisions := range [][]UsageDecision{
		{{UsageKey: "Schema.Account", Class: "unknown", SurfaceID: "apex:Schema.Account", Reason: "bad"}},
		{{UsageKey: "Schema.Account", Class: usageClassCanonicalAlias, SurfaceID: "apex:Missing", Reason: "bad"}},
		{{UsageKey: "Schema.Account", Class: usageClassCanonicalAlias, SurfaceID: "apex:Schema.Account", Reason: ""}},
		{{UsageKey: "Schema.Account", Class: usageClassCanonicalAlias, SurfaceID: "apex:Schema.Account", Reason: "one"}, {UsageKey: "Schema.Account", Class: usageClassCanonicalAlias, SurfaceID: "apex:Schema.Account", Reason: "two"}},
	} {
		if _, err := reconcileUsage(profile, usage, decisions); err == nil {
			t.Fatalf("reconcileUsage accepted %#v", decisions)
		}
	}
}

func TestReadUsageProfileRowsRejectsMissingSealedInputs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.json")
	if err := os.WriteFile(path, []byte(`{"rows":[{"surfaceId":"apex:System.debug"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := readUsageProfileRows(path); err == nil {
		t.Fatal("readUsageProfileRows accepted a profile without sealed ledger and policy inputs")
	}
}

func TestBuildSealedCorpusUsageDerivesEveryRepositoryTwice(t *testing.T) {
	root := t.TempDir()
	snapshot := filepath.Join(root, "snapshots", "private-corpus-001")
	if err := os.MkdirAll(filepath.Join(snapshot, "classes"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(snapshot, "classes", "Sample.cls"), []byte("public class Sample { void run() { System.debug('one'); } }"), 0o600); err != nil {
		t.Fatal(err)
	}
	repository := usageRepositorySpec(t, "private-corpus-001", snapshot)
	inventoryPath := filepath.Join(root, "IN_SCOPE.json")
	if err := WriteNewJSON(inventoryPath, InventorySpec{SchemaVersion: 1, Scope: "private-corpus-assurance", Repositories: []InventoryEntry{{ID: repository.ID, CheckoutPath: filepath.Join(root, "checkout"), ExpectedCommit: repository.ExpectedCommit}}}); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(root, "MANIFEST.json")
	manifest := InventoryManifest{SchemaVersion: 1, InventorySHA256: localProofFileSHA256(t, inventoryPath), Repositories: []RepositorySpec{repository}}
	if err := WriteNewJSON(manifestPath, manifest); err != nil {
		t.Fatal(err)
	}
	ledgerPath := filepath.Join(root, "LEDGER.json")
	ledger := surfaceledger.SurfaceLedger{SchemaVersion: surfaceledger.SchemaVersion, Rows: []surfaceledger.SurfaceLedgerRow{{SurfaceID: "apex:System.debug", Product: surfaceledger.ProductApex, Namespace: "System", MemberName: "debug"}}}
	if err := WriteNewJSON(ledgerPath, ledger); err != nil {
		t.Fatal(err)
	}
	policyPath := filepath.Join(root, "policy.json")
	if err := WriteNewJSON(policyPath, surfaceledger.SupportPolicy{Rules: []surfaceledger.SupportPolicyRule{{Namespace: "System", Disposition: surfaceledger.DispositionLocalRuntimeRequired, Reason: "test"}}}); err != nil {
		t.Fatal(err)
	}
	profilePath := filepath.Join(root, "profile.json")
	if err := WriteNewJSON(profilePath, surfaceledger.SupportProfile{Rows: []surfaceledger.SupportProfileRow{{SurfaceID: "apex:System.debug", Disposition: surfaceledger.DispositionLocalRuntimeRequired}, {SurfaceID: "apex-language:NamespaceClassVariablePrecedence"}}, Inputs: &surfaceledger.SupportProfileInputs{Files: []surfaceledger.SupportProfileInput{{Name: "ledger", SHA256: localProofFileSHA256(t, ledgerPath)}, {Name: "policy", SHA256: localProofFileSHA256(t, policyPath)}}}}); err != nil {
		t.Fatal(err)
	}
	usage, err := ExtractRepositoryUsage([]surfaceledger.SurfaceLedgerRow{{SurfaceID: "apex:System.debug", Product: surfaceledger.ProductApex, Namespace: "System", MemberName: "debug"}}, repository, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := CombineRepositoryUsage(InventoryManifest{SchemaVersion: 1, InventorySHA256: localProofFileSHA256(t, inventoryPath), Repositories: []RepositorySpec{repository}}, []RepositoryUsage{usage})
	if err != nil {
		t.Fatal(err)
	}
	rawBytes, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	draftPath := filepath.Join(root, "USAGE_DECISION_DRAFT.json")
	draft, err := DraftUsageDecisions(inventoryPath, ledgerPath, manifestPath, profilePath, policyPath, draftPath)
	if err != nil {
		t.Fatal(err)
	}
	if draft.RawUsageSHA256 != replayBytesSHA256(rawBytes) || len(draft.Unresolved) != 0 || len(draft.Automatic) != len(raw.Usage) {
		t.Fatalf("draft = %#v", draft)
	}
	if _, err := DraftUsageDecisions(inventoryPath, ledgerPath, manifestPath, profilePath, policyPath, draftPath); err == nil {
		t.Fatal("DraftUsageDecisions overwrote its output")
	}
	if _, err := BuildSealedCorpusUsage(inventoryPath, ledgerPath, manifestPath, profilePath, policyPath, draftPath, filepath.Join(root, "draft-as-usage.json")); err == nil {
		t.Fatal("BuildSealedCorpusUsage accepted a draft as decision authority")
	}
	decisionPath := filepath.Join(root, "decisions.json")
	if err := WriteNewJSON(decisionPath, UsageDecisionFile{SchemaVersion: 1, ProfileSHA256: localProofFileSHA256(t, profilePath), PolicySHA256: localProofFileSHA256(t, policyPath), UsageSHA256: replayBytesSHA256(rawBytes)}); err != nil {
		t.Fatal(err)
	}
	replacementPolicyPath := filepath.Join(root, "replacement-policy.json")
	if err := WriteNewJSON(replacementPolicyPath, surfaceledger.SupportPolicy{Rules: []surfaceledger.SupportPolicyRule{{Namespace: "System", Disposition: surfaceledger.DispositionHostedDeferred, Reason: "forged"}}}); err != nil {
		t.Fatal(err)
	}
	replacementDecisionPath := filepath.Join(root, "replacement-decisions.json")
	if err := WriteNewJSON(replacementDecisionPath, UsageDecisionFile{SchemaVersion: 1, ProfileSHA256: localProofFileSHA256(t, profilePath), PolicySHA256: localProofFileSHA256(t, replacementPolicyPath), UsageSHA256: replayBytesSHA256(rawBytes)}); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildSealedCorpusUsage(inventoryPath, ledgerPath, manifestPath, profilePath, replacementPolicyPath, replacementDecisionPath, filepath.Join(root, "forged-CORPUS_USAGE.json")); err == nil {
		t.Fatal("BuildSealedCorpusUsage accepted a policy and decision pair not sealed by the support profile")
	}
	outputPath := filepath.Join(root, "CORPUS_USAGE.json")
	artifact, err := BuildSealedCorpusUsage(inventoryPath, ledgerPath, manifestPath, profilePath, policyPath, decisionPath, outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if artifact.RawUsageSHA256 != replayBytesSHA256(rawBytes) || len(artifact.Reconciliation.Usage) != 2 || artifact.Reconciliation.Usage[1].Class != usageClassExact {
		t.Fatalf("artifact = %#v", artifact)
	}
	if _, err := BuildSealedCorpusUsage(inventoryPath, ledgerPath, manifestPath, profilePath, policyPath, decisionPath, outputPath); err == nil {
		t.Fatal("BuildSealedCorpusUsage overwrote its output")
	}
	insideSnapshot := filepath.Join(snapshot, "USAGE_DECISION_DRAFT.json")
	if _, err := DraftUsageDecisions(inventoryPath, ledgerPath, manifestPath, profilePath, policyPath, insideSnapshot); err == nil {
		t.Fatal("DraftUsageDecisions wrote inside a sealed snapshot")
	}
	if _, err := os.Stat(insideSnapshot); !os.IsNotExist(err) {
		t.Fatalf("draft output mutated sealed snapshot: %v", err)
	}
	insideFinalUsage := filepath.Join(snapshot, "CORPUS_USAGE.json")
	if _, err := BuildSealedCorpusUsage(inventoryPath, ledgerPath, manifestPath, profilePath, policyPath, decisionPath, insideFinalUsage); err == nil {
		t.Fatal("BuildSealedCorpusUsage wrote inside a sealed snapshot")
	}
	if _, err := os.Stat(insideFinalUsage); !os.IsNotExist(err) {
		t.Fatalf("final usage output mutated sealed snapshot: %v", err)
	}
	alias := filepath.Join(root, "snapshot-alias")
	if err := os.Symlink(snapshot, alias); err != nil {
		t.Fatal(err)
	}
	for _, output := range []string{filepath.Join(alias, "alias-draft.json"), filepath.Join(alias, "alias-usage.json")} {
		if _, err := DraftUsageDecisions(inventoryPath, ledgerPath, manifestPath, profilePath, policyPath, output); err == nil {
			t.Fatalf("DraftUsageDecisions accepted symlinked snapshot output %q", output)
		}
		if _, err := BuildSealedCorpusUsage(inventoryPath, ledgerPath, manifestPath, profilePath, policyPath, decisionPath, output); err == nil {
			t.Fatalf("BuildSealedCorpusUsage accepted symlinked snapshot output %q", output)
		}
		if _, err := os.Stat(output); !os.IsNotExist(err) {
			t.Fatalf("symlinked output mutated sealed snapshot: %v", err)
		}
	}
	originalBuildCorpusUsage := buildCorpusUsage
	buildCorpusUsage = func([]surfaceledger.SurfaceLedgerRow, string, string, string) (surfaceledger.CorpusUsage, error) {
		return surfaceledger.CorpusUsage{Usage: []surfaceledger.CorpusUsageEntry{{UsageKey: "Unknown.work", Namespace: "Unknown", MemberName: "work", PrivProdRefs: 1}}}, nil
	}
	t.Cleanup(func() { buildCorpusUsage = originalBuildCorpusUsage })
	_, unresolvedRaw, err := deriveSealedCombinedUsage(ledger.Rows, manifest, root)
	if err != nil {
		t.Fatal(err)
	}
	unresolvedDraftPath := filepath.Join(root, "unresolved-draft.json")
	unresolvedDraft, err := DraftUsageDecisions(inventoryPath, ledgerPath, manifestPath, profilePath, policyPath, unresolvedDraftPath)
	if err != nil {
		t.Fatal(err)
	}
	unresolvedData, err := os.ReadFile(unresolvedDraftPath)
	if err != nil {
		t.Fatal(err)
	}
	var decodedDraft UsageDecisionDraft
	expectedUnresolved := UsageEntry{UsageKey: "Unknown.work", Namespace: "Unknown", MemberName: "work", PrivateProdRefs: 1, RepositoryIDs: []string{repository.ID}}
	if err := json.Unmarshal(unresolvedData, &decodedDraft); err != nil || decodedDraft.RawUsageSHA256 != replayBytesSHA256(unresolvedRaw) || decodedDraft.RawUsageSHA256 != unresolvedDraft.RawUsageSHA256 || len(decodedDraft.Unresolved) != 1 || !reflect.DeepEqual(decodedDraft.Unresolved[0], expectedUnresolved) {
		t.Fatalf("decoded draft = %#v err=%v", decodedDraft, err)
	}
	buildCorpusUsage = originalBuildCorpusUsage
	originalSource, err := os.ReadFile(filepath.Join(snapshot, "classes", "Sample.cls"))
	if err != nil {
		t.Fatal(err)
	}
	originalDerive := deriveUsageForDraft
	calls := 0
	deriveUsageForDraft = func(rows []surfaceledger.SurfaceLedgerRow, boundManifest InventoryManifest, root string) (CombinedRepositoryUsage, []byte, error) {
		usage, data, err := originalDerive(rows, boundManifest, root)
		calls++
		if calls == 2 && err == nil {
			err = os.WriteFile(filepath.Join(snapshot, "classes", "Sample.cls"), []byte("changed"), 0o600)
		}
		return usage, data, err
	}
	t.Cleanup(func() { deriveUsageForDraft = originalDerive })
	lateDraft := filepath.Join(root, "late-draft.json")
	if _, err := DraftUsageDecisions(inventoryPath, ledgerPath, manifestPath, profilePath, policyPath, lateDraft); err == nil {
		t.Fatal("DraftUsageDecisions accepted a post-scan snapshot mutation")
	}
	if _, err := os.Stat(lateDraft); !os.IsNotExist(err) {
		t.Fatalf("postflight mutation wrote draft: %v", err)
	}
	if err := os.WriteFile(filepath.Join(snapshot, "classes", "Sample.cls"), originalSource, 0o600); err != nil {
		t.Fatal(err)
	}
	originalSealDerive := deriveUsageForSeal
	sealCalls := 0
	deriveUsageForSeal = func(rows []surfaceledger.SurfaceLedgerRow, boundManifest InventoryManifest, root string) (CombinedRepositoryUsage, []byte, error) {
		usage, data, err := originalSealDerive(rows, boundManifest, root)
		sealCalls++
		if sealCalls == 2 && err == nil {
			err = os.WriteFile(filepath.Join(snapshot, "classes", "Sample.cls"), []byte("changed"), 0o600)
		}
		return usage, data, err
	}
	t.Cleanup(func() { deriveUsageForSeal = originalSealDerive })
	lateUsage := filepath.Join(root, "late-usage.json")
	if _, err := BuildSealedCorpusUsage(inventoryPath, ledgerPath, manifestPath, profilePath, policyPath, decisionPath, lateUsage); err == nil {
		t.Fatal("BuildSealedCorpusUsage accepted a post-scan snapshot mutation")
	}
	if _, err := os.Stat(lateUsage); !os.IsNotExist(err) {
		t.Fatalf("postflight mutation wrote final usage: %v", err)
	}
}

func TestDraftUsageReconciliationReportsOnlyUnresolvedKeys(t *testing.T) {
	usage := UsageEntry{UsageKey: "Unknown.work", Namespace: "Unknown", PrivateProdRefs: 1, RepositoryIDs: []string{"private-corpus-001"}}
	automatic, unresolved, err := draftUsageReconciliation([]UsageProfileRow{{SurfaceID: "apex:System.debug", UsageKey: "System.debug"}}, []UsageEntry{usage})
	if err != nil || len(automatic) != 0 || len(unresolved) != 1 || unresolved[0].UsageKey != usage.UsageKey || unresolved[0].PrivateProdRefs != usage.PrivateProdRefs {
		t.Fatalf("automatic=%#v unresolved=%#v err=%v", automatic, unresolved, err)
	}
}

func TestDraftUsageReconciliationUsesCanonicalMemberBeforeOverloads(t *testing.T) {
	usage := UsageEntry{UsageKey: "ApexPages.currentPage", Namespace: "ApexPages", TypeName: "currentPage", PrivateProdRefs: 1, RepositoryIDs: []string{"private-corpus-001"}}
	profile := []UsageProfileRow{
		{SurfaceID: "apex:System.ApexPages.currentPage", UsageKey: usage.UsageKey},
		{SurfaceID: "apex:System.ApexPages.currentPage()", UsageKey: usage.UsageKey},
	}
	automatic, unresolved, err := draftUsageReconciliation(profile, []UsageEntry{usage})
	if err != nil || len(unresolved) != 0 || len(automatic) != 1 || automatic[0].SurfaceID != "apex:System.ApexPages.currentPage" {
		t.Fatalf("automatic=%#v unresolved=%#v err=%v", automatic, unresolved, err)
	}
}

func writeUsageRepo(t *testing.T, source string) string {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "classes", "Sample.cls")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func writeUsageFixtureFile(t *testing.T, root, relative, contents string) {
	t.Helper()
	path := filepath.Join(root, relative)
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func usageRepositorySpec(t *testing.T, id, root string) RepositorySpec {
	t.Helper()
	treeSHA256, err := canonicalTreeSHA256(root)
	if err != nil {
		t.Fatal(err)
	}
	return RepositorySpec{ID: id, ExpectedCommit: strings.Repeat("a", 40), ArchiveSHA256: strings.Repeat("b", 64), TreeSHA256: treeSHA256, AssignedHost: "local", SnapshotPath: "snapshots/" + id, LocalTests: "required"}
}

func validRepositoryUsage(id string) RepositoryUsage {
	return RepositoryUsage{RepositoryID: id, RootTreeSHA256: strings.Repeat("a", 64)}
}

func usageRepositoryBinding(id, treeSHA256 string) RepositorySpec {
	return RepositorySpec{ID: id, ExpectedCommit: strings.Repeat("a", 40), ArchiveSHA256: strings.Repeat("b", 64), TreeSHA256: treeSHA256, AssignedHost: "local", SnapshotPath: "snapshots/" + id, LocalTests: "required"}
}

func usageInventory(t *testing.T, repositories ...RepositorySpec) InventoryManifest {
	t.Helper()
	return InventoryManifest{SchemaVersion: 1, InventorySHA256: strings.Repeat("c", 64), Repositories: repositories}
}

func usageBindingsSHA256(repositories ...RepositoryUsage) string {
	bindings := append([]RepositoryUsage(nil), repositories...)
	sort.Slice(bindings, func(i, j int) bool { return bindings[i].RepositoryID < bindings[j].RepositoryID })
	hash := sha256.New()
	for _, repository := range bindings {
		hash.Write([]byte(repository.RepositoryID))
		hash.Write([]byte{0})
		hash.Write([]byte(repository.RootTreeSHA256))
		hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}
