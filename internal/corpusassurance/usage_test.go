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
	if err := WriteNewJSON(ledgerPath, surfaceledger.SurfaceLedger{SchemaVersion: surfaceledger.SchemaVersion, Rows: []surfaceledger.SurfaceLedgerRow{{SurfaceID: "apex:System.debug", Product: surfaceledger.ProductApex, Namespace: "System"}}}); err != nil {
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

func TestReconcileUsageFromFilesBindsSingleReadInputs(t *testing.T) {
	root := t.TempDir()
	profilePath := filepath.Join(root, "profile.json")
	usagePath := filepath.Join(root, "usage.json")
	decisionPath := filepath.Join(root, "decisions.json")
	if err := os.WriteFile(profilePath, []byte(`{"rows":[{"surfaceId":"apex:System.debug(Object)","usageKey":"System.debug"}],"corpusUsage":["ignored-source-history"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	usage := CombinedRepositoryUsage{Usage: []UsageEntry{{UsageKey: "System.debug", Namespace: "System", PrivateProdRefs: 1}}}
	if err := WriteNewJSON(usagePath, usage); err != nil {
		t.Fatal(err)
	}
	decision := UsageDecisionFile{SchemaVersion: 1, ProfileSHA256: localProofFileSHA256(t, profilePath), UsageSHA256: localProofFileSHA256(t, usagePath)}
	if err := WriteNewJSON(decisionPath, decision); err != nil {
		t.Fatal(err)
	}
	reconciled, err := reconcileUsageFromFiles(profilePath, usagePath, decisionPath)
	if err != nil {
		t.Fatal(err)
	}
	if reconciled.ProfileSHA256 != decision.ProfileSHA256 || reconciled.UsageSHA256 != decision.UsageSHA256 || reconciled.DecisionSHA256 != localProofFileSHA256(t, decisionPath) {
		t.Fatalf("reconciliation bindings = %#v", reconciled)
	}
	if len(reconciled.Usage) != 1 || reconciled.Usage[0].Class != usageClassExact {
		t.Fatalf("reconciliation usage = %#v", reconciled.Usage)
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
	if err := WriteNewJSON(manifestPath, InventoryManifest{SchemaVersion: 1, InventorySHA256: localProofFileSHA256(t, inventoryPath), Repositories: []RepositorySpec{repository}}); err != nil {
		t.Fatal(err)
	}
	ledgerPath := filepath.Join(root, "LEDGER.json")
	if err := WriteNewJSON(ledgerPath, surfaceledger.SurfaceLedger{SchemaVersion: surfaceledger.SchemaVersion, Rows: []surfaceledger.SurfaceLedgerRow{{SurfaceID: "apex:System.debug", Product: surfaceledger.ProductApex, Namespace: "System"}}}); err != nil {
		t.Fatal(err)
	}
	profilePath := filepath.Join(root, "profile.json")
	if err := os.WriteFile(profilePath, []byte(`{"rows":[{"surfaceId":"apex:System.debug","usageKey":"System.debug"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	usage, err := ExtractRepositoryUsage([]surfaceledger.SurfaceLedgerRow{{SurfaceID: "apex:System.debug", Product: surfaceledger.ProductApex, Namespace: "System"}}, repository, snapshot)
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
	decisionPath := filepath.Join(root, "decisions.json")
	if err := WriteNewJSON(decisionPath, UsageDecisionFile{SchemaVersion: 1, ProfileSHA256: localProofFileSHA256(t, profilePath), UsageSHA256: replayBytesSHA256(rawBytes)}); err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(root, "CORPUS_USAGE.json")
	artifact, err := BuildSealedCorpusUsage(inventoryPath, ledgerPath, manifestPath, profilePath, decisionPath, outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if artifact.RawUsageSHA256 != replayBytesSHA256(rawBytes) || len(artifact.Reconciliation.Usage) != 2 || artifact.Reconciliation.Usage[1].Class != usageClassExact {
		t.Fatalf("artifact = %#v", artifact)
	}
	if _, err := BuildSealedCorpusUsage(inventoryPath, ledgerPath, manifestPath, profilePath, decisionPath, outputPath); err == nil {
		t.Fatal("BuildSealedCorpusUsage overwrote its output")
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
