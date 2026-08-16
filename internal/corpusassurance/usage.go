package corpusassurance

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/glade-sh/glade/tools/internal/surfaceledger"
)

var buildCorpusUsage = surfaceledger.BuildCorpusUsage

var deriveUsageForDraft = deriveSealedCombinedUsage

var deriveUsageForSeal = deriveSealedCombinedUsage

type RepositoryUsage struct {
	RepositoryID       string                           `json:"repositoryId"`
	RootTreeSHA256     string                           `json:"rootTreeSha256"`
	LedgerSHA256       string                           `json:"ledgerSha256,omitempty"`
	RootManifestSHA256 string                           `json:"rootManifestSha256,omitempty"`
	Usage              []surfaceledger.CorpusUsageEntry `json:"usage"`
}

type UsageEntry struct {
	UsageKey        string   `json:"usageKey"`
	Namespace       string   `json:"namespace"`
	TypeName        string   `json:"typeName,omitempty"`
	MemberName      string   `json:"memberName,omitempty"`
	PrivateProdRefs int      `json:"privateProdRefs"`
	PrivateTestRefs int      `json:"privateTestRefs"`
	RepositoryIDs   []string `json:"repositoryIds"`
}

type CombinedRepositoryUsage struct {
	Usage                        []UsageEntry `json:"usage"`
	RepositoryTreeBindingsSHA256 string       `json:"repositoryTreeBindingsSha256"`
	RootManifestSHA256           string       `json:"rootManifestSha256,omitempty"`
	RepositoryUsageSHA256        []string     `json:"repositoryUsageSha256,omitempty"`
}

const (
	usageClassExact                  = "exact"
	usageClassCaseAlias              = "case-alias"
	usageClassAggregateParent        = "aggregate-parent"
	usageClassCanonicalAlias         = "canonical-alias"
	usageClassLocalSymbol            = "local-symbol"
	usageClassNonSalesforceGenerated = "non-salesforce-generated"
)

// UsageProfileRow is the allowlisted profile information needed to reconcile
// one fresh corpus usage key. It deliberately excludes prior corpus totals.
type UsageProfileRow struct {
	SurfaceID   string `json:"surfaceId"`
	UsageKey    string `json:"usageKey"`
	Disposition string `json:"disposition,omitempty"`
}

type UsageDecision struct {
	UsageKey  string `json:"usageKey"`
	Class     string `json:"class"`
	SurfaceID string `json:"surfaceId,omitempty"`
	Reason    string `json:"reason"`
}

type UsageDecisionFile struct {
	SchemaVersion  int             `json:"schemaVersion"`
	ProfileSHA256  string          `json:"profileSha256"`
	PolicySHA256   string          `json:"policySha256"`
	UsageSHA256    string          `json:"usageSha256,omitempty"`
	RawUsageSHA256 string          `json:"rawUsageSha256,omitempty"`
	Decisions      []UsageDecision `json:"decisions"`
}

type ReconciledUsageEntry struct {
	UsageEntry
	Class     string `json:"class"`
	SurfaceID string `json:"surfaceId,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

type UsageReconciliation struct {
	ProfileSHA256  string                 `json:"profileSha256,omitempty"`
	UsageSHA256    string                 `json:"usageSha256,omitempty"`
	DecisionSHA256 string                 `json:"decisionSha256,omitempty"`
	Usage          []ReconciledUsageEntry `json:"usage"`
}

type SealedCorpusUsage struct {
	SchemaVersion      int                     `json:"schemaVersion"`
	InventorySHA256    string                  `json:"inventorySha256"`
	RootManifestSHA256 string                  `json:"rootManifestSha256"`
	LedgerSHA256       string                  `json:"ledgerSha256"`
	ProfileSHA256      string                  `json:"profileSha256"`
	PolicySHA256       string                  `json:"policySha256"`
	DecisionSHA256     string                  `json:"decisionSha256"`
	RawUsageSHA256     string                  `json:"rawUsageSha256"`
	Raw                CombinedRepositoryUsage `json:"raw"`
	Reconciliation     UsageReconciliation     `json:"reconciliation"`
}

// UsageDecisionDraft exposes the exact fresh usage hash and only the keys
// that cannot be reconciled from the sealed profile. It does not classify
// unresolved keys or select profile surfaces on the caller's behalf.
type UsageDecisionDraft struct {
	SchemaVersion      int                    `json:"schemaVersion"`
	InventorySHA256    string                 `json:"inventorySha256"`
	RootManifestSHA256 string                 `json:"rootManifestSha256"`
	LedgerSHA256       string                 `json:"ledgerSha256"`
	ProfileSHA256      string                 `json:"profileSha256"`
	PolicySHA256       string                 `json:"policySha256"`
	RawUsageSHA256     string                 `json:"rawUsageSha256"`
	Automatic          []ReconciledUsageEntry `json:"automatic"`
	Unresolved         []UsageEntry           `json:"unresolved"`
}

// DraftUsageDecisions derives fresh raw usage twice from sealed inputs. Its
// output may be used to author an explicit decision file that binds this raw
// usage hash; it is not itself a decision authority.
func DraftUsageDecisions(inventoryPath, ledgerPath, manifestPath, profilePath, policyPath, outputPath string) (UsageDecisionDraft, error) {
	return DraftUsageDecisionsWithTemplate(inventoryPath, ledgerPath, manifestPath, profilePath, policyPath, outputPath, "")
}

// DraftUsageDecisionsWithTemplate also creates a create-only semantic decision
// template bound to the exact draft inputs. Empty class, surface, and reason
// fields are intentional review gates for unresolved usage.
func DraftUsageDecisionsWithTemplate(inventoryPath, ledgerPath, manifestPath, profilePath, policyPath, outputPath, decisionTemplatePath string) (UsageDecisionDraft, error) {
	for _, path := range []string{inventoryPath, ledgerPath, manifestPath, profilePath, policyPath, outputPath} {
		if !filepath.IsAbs(path) {
			return UsageDecisionDraft{}, fmt.Errorf("usage draft paths must be absolute")
		}
	}
	if decisionTemplatePath != "" && !filepath.IsAbs(decisionTemplatePath) {
		return UsageDecisionDraft{}, fmt.Errorf("usage decision template path must be absolute")
	}
	if _, err := os.Lstat(outputPath); err == nil {
		return UsageDecisionDraft{}, fmt.Errorf("usage decision draft output already exists: %s", outputPath)
	} else if !os.IsNotExist(err) {
		return UsageDecisionDraft{}, err
	}
	if decisionTemplatePath != "" {
		if filepath.Clean(decisionTemplatePath) == filepath.Clean(outputPath) {
			return UsageDecisionDraft{}, fmt.Errorf("usage draft outputs must be distinct")
		}
		if _, err := os.Lstat(decisionTemplatePath); err == nil {
			return UsageDecisionDraft{}, fmt.Errorf("usage decision template output already exists: %s", decisionTemplatePath)
		} else if !os.IsNotExist(err) {
			return UsageDecisionDraft{}, err
		}
	}
	inventory, inventoryBytes, err := readInventorySpec(inventoryPath)
	if err != nil {
		return UsageDecisionDraft{}, err
	}
	ledger, ledgerBytes, err := readExactJSONBytes[surfaceledger.SurfaceLedger](ledgerPath)
	if err != nil {
		return UsageDecisionDraft{}, err
	}
	manifest, manifestBytes, err := readExactJSONBytes[InventoryManifest](manifestPath)
	if err != nil {
		return UsageDecisionDraft{}, err
	}
	if manifest.SchemaVersion != 1 || manifest.InventorySHA256 != replayBytesSHA256(inventoryBytes) || ValidateInventoryCoverage(inventory, manifest.Repositories) != nil {
		return UsageDecisionDraft{}, fmt.Errorf("invalid sealed inventory manifest")
	}
	if err := validateSealedUsageOutput(outputPath, manifest, filepath.Dir(manifestPath)); err != nil {
		return UsageDecisionDraft{}, err
	}
	first, firstBytes, err := deriveUsageForDraft(ledger.Rows, manifest, filepath.Dir(manifestPath))
	if err != nil {
		return UsageDecisionDraft{}, err
	}
	_, secondBytes, err := deriveUsageForDraft(ledger.Rows, manifest, filepath.Dir(manifestPath))
	if err != nil || !bytes.Equal(firstBytes, secondBytes) {
		return UsageDecisionDraft{}, fmt.Errorf("corpus usage extraction is not byte-identical")
	}
	profile, profileInputs, profileBytes, err := readUsageProfileRows(profilePath)
	if err != nil {
		return UsageDecisionDraft{}, err
	}
	policy, policyBytes, err := readExactJSONBytes[surfaceledger.SupportPolicy](policyPath)
	if err != nil {
		return UsageDecisionDraft{}, fmt.Errorf("read support policy: %w", err)
	}
	if len(policy.Rules) == 0 {
		return UsageDecisionDraft{}, fmt.Errorf("support policy rules are required")
	}
	profileSnapshotInputs, err := verifyUsageProfileInputs(profileInputs, ledgerBytes, policyBytes)
	if err != nil {
		return UsageDecisionDraft{}, err
	}
	profile, err = usageProfileRowsFromLedger(profile, ledger.Rows, policy)
	if err != nil {
		return UsageDecisionDraft{}, err
	}
	automatic, unresolved, err := draftUsageReconciliation(profile, first.Usage)
	if err != nil {
		return UsageDecisionDraft{}, err
	}
	draft := UsageDecisionDraft{SchemaVersion: 1, InventorySHA256: replayBytesSHA256(inventoryBytes), RootManifestSHA256: replayBytesSHA256(manifestBytes), LedgerSHA256: replayBytesSHA256(ledgerBytes), ProfileSHA256: replayBytesSHA256(profileBytes), PolicySHA256: replayBytesSHA256(policyBytes), RawUsageSHA256: replayBytesSHA256(firstBytes), Automatic: automatic, Unresolved: unresolved}
	postflightInputs := []sealedUsageInput{{inventoryPath, inventoryBytes}, {ledgerPath, ledgerBytes}, {manifestPath, manifestBytes}, {profilePath, profileBytes}, {policyPath, policyBytes}}
	postflightInputs = append(postflightInputs, profileSnapshotInputs...)
	if err := verifySealedUsagePostflight(postflightInputs, manifest, filepath.Dir(manifestPath)); err != nil {
		return UsageDecisionDraft{}, err
	}
	if err := WriteNewJSON(outputPath, draft); err != nil {
		return UsageDecisionDraft{}, err
	}
	if decisionTemplatePath != "" {
		decisions := make([]UsageDecision, len(unresolved))
		for i, entry := range unresolved {
			decisions[i] = UsageDecision{UsageKey: entry.UsageKey}
		}
		template := UsageDecisionFile{SchemaVersion: 2, ProfileSHA256: draft.ProfileSHA256, PolicySHA256: draft.PolicySHA256, RawUsageSHA256: draft.RawUsageSHA256, Decisions: decisions}
		if err := WriteNewJSON(decisionTemplatePath, template); err != nil {
			return UsageDecisionDraft{}, err
		}
	}
	return draft, nil
}

func draftUsageReconciliation(profile []UsageProfileRow, usage []UsageEntry) ([]ReconciledUsageEntry, []UsageEntry, error) {
	profilesByKey := make(map[string][]UsageProfileRow, len(profile))
	profilesByFoldedKey := make(map[string][]UsageProfileRow, len(profile))
	for _, row := range profile {
		if row.SurfaceID == "" || row.UsageKey == "" {
			return nil, nil, fmt.Errorf("invalid profile row")
		}
		profilesByKey[row.UsageKey] = append(profilesByKey[row.UsageKey], row)
		profilesByFoldedKey[strings.ToLower(row.UsageKey)] = append(profilesByFoldedKey[strings.ToLower(row.UsageKey)], row)
	}
	automatic := make([]ReconciledUsageEntry, 0, len(usage))
	unresolved := make([]UsageEntry, 0)
	seen := make(map[string]bool, len(usage))
	for _, entry := range usage {
		if entry.UsageKey == "" || seen[entry.UsageKey] || entry.PrivateProdRefs+entry.PrivateTestRefs <= 0 {
			return nil, nil, fmt.Errorf("invalid or duplicate fresh usage key %q", entry.UsageKey)
		}
		seen[entry.UsageKey] = true
		row := ReconciledUsageEntry{UsageEntry: entry}
		if candidate, ok := canonicalUsageProfileCandidate(profilesByKey[entry.UsageKey]); ok {
			row.Class, row.SurfaceID = usageClassExact, candidate.SurfaceID
		} else if candidate, ok := canonicalUsageProfileCandidate(profilesByFoldedKey[strings.ToLower(entry.UsageKey)]); ok {
			row.Class, row.SurfaceID = usageClassCaseAlias, candidate.SurfaceID
		} else if candidates := aggregateUsageCandidates(entry.UsageKey, profile); len(candidates) == 1 {
			row.Class, row.SurfaceID = usageClassAggregateParent, candidates[0].SurfaceID
		} else {
			unresolved = append(unresolved, entry)
			continue
		}
		automatic = append(automatic, row)
	}
	sort.Slice(automatic, func(i, j int) bool { return automatic[i].UsageKey < automatic[j].UsageKey })
	sort.Slice(unresolved, func(i, j int) bool { return unresolved[i].UsageKey < unresolved[j].UsageKey })
	return automatic, unresolved, nil
}

// BuildSealedCorpusUsage is the Task 3 trust boundary. It derives every
// repository from the sealed inventory manifest twice, requires identical
// canonical raw usage, reconciles every positive key, and writes once.
func BuildSealedCorpusUsage(inventoryPath, ledgerPath, manifestPath, profilePath, policyPath, decisionPath, outputPath string) (SealedCorpusUsage, error) {
	for _, path := range []string{inventoryPath, ledgerPath, manifestPath, profilePath, policyPath, decisionPath, outputPath} {
		if !filepath.IsAbs(path) {
			return SealedCorpusUsage{}, fmt.Errorf("sealed usage paths must be absolute")
		}
	}
	if _, err := os.Lstat(outputPath); err == nil {
		return SealedCorpusUsage{}, fmt.Errorf("sealed corpus usage output already exists: %s", outputPath)
	} else if !os.IsNotExist(err) {
		return SealedCorpusUsage{}, err
	}
	inventory, inventoryBytes, err := readInventorySpec(inventoryPath)
	if err != nil {
		return SealedCorpusUsage{}, err
	}
	ledger, ledgerBytes, err := readExactJSONBytes[surfaceledger.SurfaceLedger](ledgerPath)
	if err != nil {
		return SealedCorpusUsage{}, err
	}
	manifest, manifestBytes, err := readExactJSONBytes[InventoryManifest](manifestPath)
	if err != nil {
		return SealedCorpusUsage{}, err
	}
	if manifest.SchemaVersion != 1 || manifest.InventorySHA256 != replayBytesSHA256(inventoryBytes) || ValidateInventoryCoverage(inventory, manifest.Repositories) != nil {
		return SealedCorpusUsage{}, fmt.Errorf("invalid sealed inventory manifest")
	}
	if err := validateSealedUsageOutput(outputPath, manifest, filepath.Dir(manifestPath)); err != nil {
		return SealedCorpusUsage{}, err
	}
	first, firstBytes, err := deriveUsageForSeal(ledger.Rows, manifest, filepath.Dir(manifestPath))
	if err != nil {
		return SealedCorpusUsage{}, err
	}
	second, secondBytes, err := deriveUsageForSeal(ledger.Rows, manifest, filepath.Dir(manifestPath))
	if err != nil {
		return SealedCorpusUsage{}, err
	}
	if !bytes.Equal(firstBytes, secondBytes) {
		return SealedCorpusUsage{}, fmt.Errorf("corpus usage extraction is not byte-identical")
	}
	profile, profileInputs, profileBytes, err := readUsageProfileRows(profilePath)
	if err != nil {
		return SealedCorpusUsage{}, err
	}
	policy, policyBytes, err := readExactJSONBytes[surfaceledger.SupportPolicy](policyPath)
	if err != nil {
		return SealedCorpusUsage{}, fmt.Errorf("read support policy: %w", err)
	}
	if len(policy.Rules) == 0 {
		return SealedCorpusUsage{}, fmt.Errorf("support policy rules are required")
	}
	profileSnapshotInputs, err := verifyUsageProfileInputs(profileInputs, ledgerBytes, policyBytes)
	if err != nil {
		return SealedCorpusUsage{}, err
	}
	profile, err = usageProfileRowsFromLedger(profile, ledger.Rows, policy)
	if err != nil {
		return SealedCorpusUsage{}, err
	}
	decision, decisionBytes, err := readExactJSONBytes[UsageDecisionFile](decisionPath)
	if err != nil {
		return SealedCorpusUsage{}, err
	}
	decisionUsageSHA := decision.UsageSHA256
	if decision.RawUsageSHA256 != "" {
		if decisionUsageSHA != "" && decisionUsageSHA != decision.RawUsageSHA256 {
			return SealedCorpusUsage{}, fmt.Errorf("usage decisions contain conflicting raw usage hashes")
		}
		decisionUsageSHA = decision.RawUsageSHA256
	}
	if (decision.SchemaVersion != 1 && decision.SchemaVersion != 2) || decision.ProfileSHA256 != replayBytesSHA256(profileBytes) || decision.PolicySHA256 != replayBytesSHA256(policyBytes) || decisionUsageSHA != replayBytesSHA256(firstBytes) {
		return SealedCorpusUsage{}, fmt.Errorf("usage decisions do not bind fresh extraction")
	}
	reconciliation, err := reconcileUsage(profile, first.Usage, decision.Decisions)
	if err != nil {
		return SealedCorpusUsage{}, err
	}
	reconciliation.ProfileSHA256, reconciliation.UsageSHA256, reconciliation.DecisionSHA256 = replayBytesSHA256(profileBytes), replayBytesSHA256(firstBytes), replayBytesSHA256(decisionBytes)
	artifact := SealedCorpusUsage{SchemaVersion: 1, InventorySHA256: replayBytesSHA256(inventoryBytes), RootManifestSHA256: replayBytesSHA256(manifestBytes), LedgerSHA256: replayBytesSHA256(ledgerBytes), ProfileSHA256: replayBytesSHA256(profileBytes), PolicySHA256: replayBytesSHA256(policyBytes), DecisionSHA256: replayBytesSHA256(decisionBytes), RawUsageSHA256: replayBytesSHA256(firstBytes), Raw: second, Reconciliation: reconciliation}
	postflightInputs := []sealedUsageInput{{inventoryPath, inventoryBytes}, {ledgerPath, ledgerBytes}, {manifestPath, manifestBytes}, {profilePath, profileBytes}, {policyPath, policyBytes}, {decisionPath, decisionBytes}}
	postflightInputs = append(postflightInputs, profileSnapshotInputs...)
	if err := verifySealedUsagePostflight(postflightInputs, manifest, filepath.Dir(manifestPath)); err != nil {
		return SealedCorpusUsage{}, err
	}
	if err := WriteNewJSON(outputPath, artifact); err != nil {
		return SealedCorpusUsage{}, err
	}
	return artifact, nil
}

func deriveSealedCombinedUsage(ledger []surfaceledger.SurfaceLedgerRow, manifest InventoryManifest, root string) (CombinedRepositoryUsage, []byte, error) {
	repositories := make([]RepositoryUsage, 0, len(manifest.Repositories))
	for _, repository := range manifest.Repositories {
		snapshot, err := rootedPath(root, repository.SnapshotPath)
		if err != nil {
			return CombinedRepositoryUsage{}, nil, err
		}
		usage, err := ExtractRepositoryUsage(ledger, repository, snapshot)
		if err != nil {
			return CombinedRepositoryUsage{}, nil, err
		}
		repositories = append(repositories, usage)
	}
	combined, err := CombineRepositoryUsage(manifest, repositories)
	if err != nil {
		return CombinedRepositoryUsage{}, nil, err
	}
	data, err := json.Marshal(combined)
	return combined, data, err
}

type sealedUsageInput struct {
	path string
	data []byte
}

func verifySealedUsageInputs(inputs []sealedUsageInput) error {
	for _, input := range inputs {
		if got, err := os.ReadFile(input.path); err != nil || !bytes.Equal(got, input.data) {
			return fmt.Errorf("sealed usage input changed during extraction")
		}
	}
	return nil
}

func verifySealedUsagePostflight(inputs []sealedUsageInput, manifest InventoryManifest, root string) error {
	if err := verifySealedUsageInputs(inputs); err != nil {
		return err
	}
	for _, repository := range manifest.Repositories {
		snapshot, err := rootedPath(root, repository.SnapshotPath)
		if err != nil {
			return err
		}
		got, err := canonicalTreeSHA256(snapshot)
		if err != nil || got != repository.TreeSHA256 {
			return fmt.Errorf("snapshot tree changed during usage extraction for %q", repository.ID)
		}
	}
	return nil
}

func validateSealedUsageOutput(outputPath string, manifest InventoryManifest, root string) error {
	parent, err := filepath.EvalSymlinks(filepath.Dir(outputPath))
	if err != nil {
		return fmt.Errorf("resolve usage output parent: %w", err)
	}
	output := filepath.Join(parent, filepath.Base(outputPath))
	for _, repository := range manifest.Repositories {
		snapshot, err := rootedPath(root, repository.SnapshotPath)
		if err != nil {
			return err
		}
		snapshot, err = filepath.EvalSymlinks(snapshot)
		if err != nil {
			return fmt.Errorf("resolve snapshot for %q: %w", repository.ID, err)
		}
		relative, err := filepath.Rel(snapshot, output)
		if err != nil {
			return err
		}
		if relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))) {
			return fmt.Errorf("usage output must not overlap sealed snapshot %q", repository.ID)
		}
	}
	return nil
}

func readUsageProfileRows(path string) ([]UsageProfileRow, []surfaceledger.SupportProfileInput, []byte, error) {
	profile, data, err := readExactJSONBytes[surfaceledger.SupportProfile](path)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(profile.ValidationErrors) != 0 || profile.Inputs == nil {
		return nil, nil, nil, fmt.Errorf("support profile has validation errors")
	}
	rows := make([]UsageProfileRow, len(profile.Rows))
	for i, row := range profile.Rows {
		rows[i] = UsageProfileRow{SurfaceID: row.SurfaceID, UsageKey: row.UsageKey, Disposition: string(row.Disposition)}
	}
	return rows, profile.Inputs.Files, data, nil
}

func verifyUsageProfileInputs(inputs []surfaceledger.SupportProfileInput, ledgerBytes, policyBytes []byte) ([]sealedUsageInput, error) {
	want := map[string]string{"ledger": replayBytesSHA256(ledgerBytes), "policy": replayBytesSHA256(policyBytes)}
	corpusUsageNames := map[string]struct{}{"corpus-usage": {}}
	snapshotNames := map[string]struct{}{
		"DOCS_SNAPSHOT.json":     {},
		"ORG_SNAPSHOT.json":      {},
		"GLADE_SNAPSHOT.json":    {},
		"EVIDENCE_SNAPSHOT.json": {},
	}
	seenSnapshots := make(map[string]struct{}, len(snapshotNames))
	seenArtifacts := make(map[string]string, len(snapshotNames)+len(corpusUsageNames))
	sealedSnapshots := make([]sealedUsageInput, 0, len(snapshotNames))
	for _, input := range inputs {
		expected, ok := want[input.Name]
		if ok {
			if input.SHA256 != expected {
				return nil, fmt.Errorf("support profile input %q does not bind supplied bytes", input.Name)
			}
			delete(want, input.Name)
			continue
		}
		_, isCorpusUsage := corpusUsageNames[input.Name]
		_, isSnapshot := snapshotNames[input.Name]
		if (!isCorpusUsage && !isSnapshot) || !filepath.IsAbs(input.Path) {
			return nil, fmt.Errorf("support profile input %q does not bind supplied bytes", input.Name)
		}
		if isSnapshot {
			if _, ok := seenSnapshots[input.Name]; ok {
				return nil, fmt.Errorf("support profile input %q is duplicated", input.Name)
			}
			seenSnapshots[input.Name] = struct{}{}
		}
		data, err := os.ReadFile(input.Path)
		if err != nil {
			return nil, fmt.Errorf("read support profile input %q: %w", input.Name, err)
		}
		if input.SHA256 != replayBytesSHA256(data) {
			return nil, fmt.Errorf("support profile input %q does not bind supplied bytes", input.Name)
		}
		resolved, err := filepath.EvalSymlinks(input.Path)
		if err != nil {
			return nil, fmt.Errorf("resolve support profile input %q: %w", input.Name, err)
		}
		if previous, ok := seenArtifacts[resolved]; ok {
			return nil, fmt.Errorf("support profile inputs %q and %q alias the same artifact", previous, input.Name)
		}
		seenArtifacts[resolved] = input.Name
		sealedSnapshots = append(sealedSnapshots, sealedUsageInput{path: input.Path, data: data})
	}
	if len(seenSnapshots) != len(snapshotNames) {
		return nil, fmt.Errorf("support profile surface snapshots are incomplete")
	}
	if len(want) != 0 {
		return nil, fmt.Errorf("support profile lacks sealed ledger and policy inputs")
	}
	return sealedSnapshots, nil
}

// usageProfileRowsFromLedger assigns the canonical usage key from the sealed
// ledger. Source-profile usage keys are ignored so they cannot select or
// redirect private-corpus reconciliation.
func usageProfileRowsFromLedger(profile []UsageProfileRow, ledger []surfaceledger.SurfaceLedgerRow, policy surfaceledger.SupportPolicy) ([]UsageProfileRow, error) {
	expectedProfile := surfaceledger.ComputeSupportProfile(ledger, policy, nil)
	if len(expectedProfile.ValidationErrors) != 0 {
		return nil, fmt.Errorf("support policy does not produce a valid profile")
	}
	expected := make(map[string]surfaceledger.SupportProfileRow, len(expectedProfile.Rows))
	for _, row := range expectedProfile.Rows {
		expected[row.SurfaceID] = row
	}
	ledgerByID := make(map[string]surfaceledger.SurfaceLedgerRow, len(ledger))
	for _, row := range ledger {
		if row.SurfaceID == "" || ledgerByID[row.SurfaceID].SurfaceID != "" {
			return nil, fmt.Errorf("invalid or duplicate ledger surface %q", row.SurfaceID)
		}
		ledgerByID[row.SurfaceID] = row
	}
	source := make(map[string]UsageProfileRow, len(profile))
	for _, row := range profile {
		if row.SurfaceID == "" || source[row.SurfaceID].SurfaceID != "" {
			return nil, fmt.Errorf("invalid or duplicate profile surface %q", row.SurfaceID)
		}
		source[row.SurfaceID] = row
	}
	derived := make([]UsageProfileRow, 0, len(profile))
	for _, ledgerRow := range ledgerByID {
		if ledgerRow.Product != surfaceledger.ProductApex || ledgerRow.Namespace == "" {
			continue
		}
		row, exists := source[ledgerRow.SurfaceID]
		if !exists {
			return nil, fmt.Errorf("scanner-representable ledger surface %q is absent from profile", ledgerRow.SurfaceID)
		}
		if expectedRow, exists := expected[ledgerRow.SurfaceID]; !exists || row.Disposition != string(expectedRow.Disposition) {
			return nil, fmt.Errorf("profile disposition for %q does not match sealed policy", ledgerRow.SurfaceID)
		}
		row.UsageKey = surfaceledger.UsageKeyForRow(ledgerRow, policy)
		if row.UsageKey == "" {
			return nil, fmt.Errorf("scanner-representable ledger surface %q lacks a usage key", ledgerRow.SurfaceID)
		}
		derived = append(derived, row)
	}
	sort.Slice(derived, func(i, j int) bool { return derived[i].SurfaceID < derived[j].SurfaceID })
	return derived, nil
}

// ReconcileUsage binds every fresh private usage key to exactly one profile
// surface or an explicit non-Salesforce decision. Exact, case-only, and
// unambiguous aggregate-parent matches are derived, never caller-selected.
func reconcileUsage(profile []UsageProfileRow, usage []UsageEntry, decisions []UsageDecision) (UsageReconciliation, error) {
	profilesByKey := make(map[string][]UsageProfileRow, len(profile))
	profilesByFoldedKey := make(map[string][]UsageProfileRow, len(profile))
	profilesByID := make(map[string]bool, len(profile))
	for _, row := range profile {
		if row.SurfaceID == "" || row.UsageKey == "" || profilesByID[row.SurfaceID] {
			return UsageReconciliation{}, fmt.Errorf("invalid or duplicate profile surface %q", row.SurfaceID)
		}
		profilesByID[row.SurfaceID] = true
		profilesByKey[row.UsageKey] = append(profilesByKey[row.UsageKey], row)
		profilesByFoldedKey[strings.ToLower(row.UsageKey)] = append(profilesByFoldedKey[strings.ToLower(row.UsageKey)], row)
	}
	if len(profilesByID) == 0 {
		return UsageReconciliation{}, fmt.Errorf("profile rows are required")
	}
	decisionsByKey := make(map[string]UsageDecision, len(decisions))
	for _, decision := range decisions {
		if decision.UsageKey == "" || decision.Reason == "" || decisionsByKey[decision.UsageKey].UsageKey != "" || !manualUsageClass(decision.Class) {
			return UsageReconciliation{}, fmt.Errorf("invalid or duplicate usage decision %q", decision.UsageKey)
		}
		if decision.Class == usageClassCanonicalAlias {
			if decision.SurfaceID == "" || !profilesByID[decision.SurfaceID] {
				return UsageReconciliation{}, fmt.Errorf("usage decision %q selects an unknown surface", decision.UsageKey)
			}
		} else if decision.SurfaceID != "" {
			return UsageReconciliation{}, fmt.Errorf("non-Salesforce usage decision %q must not select a surface", decision.UsageKey)
		}
		decisionsByKey[decision.UsageKey] = decision
	}

	seenUsage := make(map[string]bool, len(usage))
	result := UsageReconciliation{Usage: make([]ReconciledUsageEntry, 0, len(usage))}
	for _, entry := range usage {
		if entry.UsageKey == "" || seenUsage[entry.UsageKey] || entry.PrivateProdRefs+entry.PrivateTestRefs <= 0 {
			return UsageReconciliation{}, fmt.Errorf("invalid or duplicate fresh usage key %q", entry.UsageKey)
		}
		seenUsage[entry.UsageKey] = true
		row := ReconciledUsageEntry{UsageEntry: entry}
		if candidate, ok := canonicalUsageProfileCandidate(profilesByKey[entry.UsageKey]); ok {
			row.Class, row.SurfaceID = usageClassExact, candidate.SurfaceID
		} else if candidate, ok := canonicalUsageProfileCandidate(profilesByFoldedKey[strings.ToLower(entry.UsageKey)]); ok {
			row.Class, row.SurfaceID = usageClassCaseAlias, candidate.SurfaceID
		} else if candidates := aggregateUsageCandidates(entry.UsageKey, profile); len(candidates) == 1 {
			row.Class, row.SurfaceID = usageClassAggregateParent, candidates[0].SurfaceID
		} else {
			decision, ok := decisionsByKey[entry.UsageKey]
			if !ok {
				return UsageReconciliation{}, fmt.Errorf("unclassified fresh usage key %q", entry.UsageKey)
			}
			row.Class, row.SurfaceID, row.Reason = decision.Class, decision.SurfaceID, decision.Reason
			delete(decisionsByKey, entry.UsageKey)
		}
		result.Usage = append(result.Usage, row)
	}
	if len(decisionsByKey) != 0 {
		return UsageReconciliation{}, fmt.Errorf("usage decisions contain absent keys")
	}
	sort.Slice(result.Usage, func(i, j int) bool { return result.Usage[i].UsageKey < result.Usage[j].UsageKey })
	return result, nil
}

func manualUsageClass(class string) bool {
	return class == usageClassCanonicalAlias || class == usageClassLocalSymbol || class == usageClassNonSalesforceGenerated
}

func aggregateUsageCandidates(key string, profile []UsageProfileRow) []UsageProfileRow {
	prefix := key + "."
	var candidates []UsageProfileRow
	for _, row := range profile {
		if strings.HasPrefix(row.UsageKey, prefix) {
			candidates = append(candidates, row)
		}
	}
	return candidates
}

func canonicalUsageProfileCandidate(candidates []UsageProfileRow) (UsageProfileRow, bool) {
	if len(candidates) == 1 {
		return candidates[0], true
	}
	var canonical UsageProfileRow
	for _, candidate := range candidates {
		if strings.Contains(candidate.SurfaceID, "(") {
			continue
		}
		if canonical.SurfaceID != "" {
			return UsageProfileRow{}, false
		}
		canonical = candidate
	}
	if canonical.SurfaceID == "" {
		return UsageProfileRow{}, false
	}
	for _, candidate := range candidates {
		if candidate.Disposition != canonical.Disposition {
			return UsageProfileRow{}, false
		}
	}
	return canonical, true
}

// ExtractRepositoryUsageFromFiles is the workflow entrypoint. It obtains the
// ledger and repository binding from immutable files, rather than accepting a
// caller-built ledger or repository object.
func ExtractRepositoryUsageFromFiles(inventoryPath, ledgerPath, rootManifestPath, repositoryID string) (RepositoryUsage, error) {
	inventory, inventoryBytes, err := readInventorySpec(inventoryPath)
	if err != nil {
		return RepositoryUsage{}, fmt.Errorf("read IN_SCOPE inventory: %w", err)
	}
	ledger, ledgerBytes, err := readExactJSONBytes[surfaceledger.SurfaceLedger](ledgerPath)
	if err != nil {
		return RepositoryUsage{}, fmt.Errorf("read surface ledger: %w", err)
	}
	manifest, manifestBytes, err := readExactJSONBytes[InventoryManifest](rootManifestPath)
	if err != nil {
		return RepositoryUsage{}, fmt.Errorf("read inventory manifest: %w", err)
	}
	if manifest.SchemaVersion != 1 || manifest.InventorySHA256 != replayBytesSHA256(inventoryBytes) || ValidateInventoryCoverage(inventory, manifest.Repositories) != nil {
		return RepositoryUsage{}, fmt.Errorf("invalid inventory manifest")
	}
	var repository RepositorySpec
	for _, candidate := range manifest.Repositories {
		if candidate.ID == repositoryID {
			repository = candidate
			break
		}
	}
	if repository.ID == "" {
		return RepositoryUsage{}, fmt.Errorf("repository %q is not in the inventory manifest", repositoryID)
	}
	root, err := rootedPath(filepath.Dir(rootManifestPath), repository.SnapshotPath)
	if err != nil {
		return RepositoryUsage{}, err
	}
	result, err := ExtractRepositoryUsage(ledger.Rows, repository, root)
	if err != nil {
		return RepositoryUsage{}, err
	}
	_, postLedger, err := readExactJSONBytes[surfaceledger.SurfaceLedger](ledgerPath)
	if err != nil || replayBytesSHA256(postLedger) != replayBytesSHA256(ledgerBytes) {
		return RepositoryUsage{}, fmt.Errorf("surface ledger changed during usage scan")
	}
	_, postManifest, err := readExactJSONBytes[InventoryManifest](rootManifestPath)
	if err != nil || replayBytesSHA256(postManifest) != replayBytesSHA256(manifestBytes) {
		return RepositoryUsage{}, fmt.Errorf("inventory manifest changed during usage scan")
	}
	_, postInventory, err := readInventorySpec(inventoryPath)
	if err != nil || replayBytesSHA256(postInventory) != replayBytesSHA256(inventoryBytes) {
		return RepositoryUsage{}, fmt.Errorf("IN_SCOPE inventory changed during usage scan")
	}
	result.LedgerSHA256 = replayBytesSHA256(ledgerBytes)
	result.RootManifestSHA256 = replayBytesSHA256(manifestBytes)
	return result, nil
}

// CombineRepositoryUsageFromFiles performs the merge from sealed raw files and
// records their exact hashes for downstream reconciliation.
func combineRepositoryUsageFromFiles(inventoryPath, ledgerPath, rootManifestPath string, repositoryUsagePaths []string) (CombinedRepositoryUsage, error) {
	inventory, inventoryBytes, err := readInventorySpec(inventoryPath)
	if err != nil {
		return CombinedRepositoryUsage{}, fmt.Errorf("read IN_SCOPE inventory: %w", err)
	}
	manifest, manifestBytes, err := readExactJSONBytes[InventoryManifest](rootManifestPath)
	if err != nil {
		return CombinedRepositoryUsage{}, fmt.Errorf("read inventory manifest: %w", err)
	}
	if manifest.SchemaVersion != 1 || manifest.InventorySHA256 != replayBytesSHA256(inventoryBytes) || ValidateInventoryCoverage(inventory, manifest.Repositories) != nil {
		return CombinedRepositoryUsage{}, fmt.Errorf("invalid inventory manifest")
	}
	_, ledgerBytes, err := readExactJSONBytes[surfaceledger.SurfaceLedger](ledgerPath)
	if err != nil {
		return CombinedRepositoryUsage{}, fmt.Errorf("read surface ledger: %w", err)
	}
	ledgerSHA256 := replayBytesSHA256(ledgerBytes)
	if len(repositoryUsagePaths) == 0 {
		return CombinedRepositoryUsage{}, fmt.Errorf("repository usage files are required")
	}
	repositories := make([]RepositoryUsage, 0, len(repositoryUsagePaths))
	hashes := make([]string, 0, len(repositoryUsagePaths))
	for _, path := range repositoryUsagePaths {
		repository, data, err := readExactJSONBytes[RepositoryUsage](path)
		if err != nil {
			return CombinedRepositoryUsage{}, fmt.Errorf("read repository usage: %w", err)
		}
		if repository.RootManifestSHA256 != replayBytesSHA256(manifestBytes) || repository.LedgerSHA256 != ledgerSHA256 {
			return CombinedRepositoryUsage{}, fmt.Errorf("repository usage %q does not bind the inventory manifest", repository.RepositoryID)
		}
		repositories = append(repositories, repository)
		hashes = append(hashes, replayBytesSHA256(data))
	}
	result, err := CombineRepositoryUsage(manifest, repositories)
	if err != nil {
		return CombinedRepositoryUsage{}, err
	}
	_, postManifest, err := readExactJSONBytes[InventoryManifest](rootManifestPath)
	if err != nil || replayBytesSHA256(postManifest) != replayBytesSHA256(manifestBytes) {
		return CombinedRepositoryUsage{}, fmt.Errorf("inventory manifest changed during usage reconciliation")
	}
	_, postInventory, err := readInventorySpec(inventoryPath)
	if err != nil || replayBytesSHA256(postInventory) != replayBytesSHA256(inventoryBytes) {
		return CombinedRepositoryUsage{}, fmt.Errorf("IN_SCOPE inventory changed during usage reconciliation")
	}
	_, postLedger, err := readExactJSONBytes[surfaceledger.SurfaceLedger](ledgerPath)
	if err != nil || replayBytesSHA256(postLedger) != ledgerSHA256 {
		return CombinedRepositoryUsage{}, fmt.Errorf("surface ledger changed during usage reconciliation")
	}
	for index, path := range repositoryUsagePaths {
		_, data, err := readExactJSONBytes[RepositoryUsage](path)
		if err != nil || replayBytesSHA256(data) != hashes[index] {
			return CombinedRepositoryUsage{}, fmt.Errorf("repository usage changed during reconciliation")
		}
	}
	sort.Strings(hashes)
	result.RootManifestSHA256 = replayBytesSHA256(manifestBytes)
	result.RepositoryUsageSHA256 = hashes
	return result, nil
}

func ExtractRepositoryUsage(ledger []surfaceledger.SurfaceLedgerRow, repository RepositorySpec, root string) (RepositoryUsage, error) {
	if err := ValidateRepositorySpec(repository); err != nil {
		return RepositoryUsage{}, err
	}
	treeSHA256, err := canonicalTreeSHA256(root)
	if err != nil {
		return RepositoryUsage{}, fmt.Errorf("hash repository %q root: %w", repository.ID, err)
	}
	if treeSHA256 != repository.TreeSHA256 {
		return RepositoryUsage{}, fmt.Errorf("repository %q root tree hash does not match sealed snapshot", repository.ID)
	}
	usage, err := buildCorpusUsage(ledger, "", "", root)
	if err != nil {
		return RepositoryUsage{}, err
	}
	postScanTreeSHA256, err := canonicalTreeSHA256(root)
	if err != nil {
		return RepositoryUsage{}, fmt.Errorf("rehash repository %q root: %w", repository.ID, err)
	}
	if postScanTreeSHA256 != repository.TreeSHA256 {
		return RepositoryUsage{}, fmt.Errorf("repository %q root tree hash changed during usage scan", repository.ID)
	}
	result := RepositoryUsage{RepositoryID: repository.ID, RootTreeSHA256: postScanTreeSHA256}
	for _, entry := range usage.Usage {
		if entry.PrivProdRefs+entry.PrivTestRefs > 0 {
			result.Usage = append(result.Usage, entry)
		}
	}
	return result, nil
}

func CombineRepositoryUsage(inventory InventoryManifest, repositories []RepositoryUsage) (CombinedRepositoryUsage, error) {
	if inventory.SchemaVersion != 1 || !sha256Pattern.MatchString(inventory.InventorySHA256) || len(inventory.Repositories) == 0 {
		return CombinedRepositoryUsage{}, fmt.Errorf("invalid inventory manifest")
	}
	expectedTrees := make(map[string]string, len(inventory.Repositories))
	for _, repository := range inventory.Repositories {
		if err := ValidateRepositorySpec(repository); err != nil {
			return CombinedRepositoryUsage{}, err
		}
		if _, exists := expectedTrees[repository.ID]; exists {
			return CombinedRepositoryUsage{}, fmt.Errorf("inventory repository %q appears more than once", repository.ID)
		}
		expectedTrees[repository.ID] = repository.TreeSHA256
	}
	if len(repositories) != len(expectedTrees) {
		return CombinedRepositoryUsage{}, fmt.Errorf("usage repository count does not match inventory")
	}

	byKey := make(map[string]*UsageEntry)
	seen := make(map[string]bool, len(repositories))
	for _, repository := range repositories {
		expectedTreeSHA256, exists := expectedTrees[repository.RepositoryID]
		if !repositoryIDPat.MatchString(repository.RepositoryID) || seen[repository.RepositoryID] || !sha256Pattern.MatchString(repository.RootTreeSHA256) || !exists || repository.RootTreeSHA256 != expectedTreeSHA256 {
			return CombinedRepositoryUsage{}, fmt.Errorf("invalid, duplicate, or unsealed repository id %q", repository.RepositoryID)
		}
		seen[repository.RepositoryID] = true
		seenUsage := make(map[string]bool, len(repository.Usage))
		for _, entry := range repository.Usage {
			if corpusUsageHasNegativeCounts(entry) {
				return CombinedRepositoryUsage{}, fmt.Errorf("repository %q has negative usage counts for %q", repository.RepositoryID, entry.UsageKey)
			}
			if entry.PrivProdRefs+entry.PrivTestRefs == 0 {
				continue
			}
			if entry.UsageKey == "" || seenUsage[entry.UsageKey] {
				return CombinedRepositoryUsage{}, fmt.Errorf("repository %q has duplicate or invalid usage key %q", repository.RepositoryID, entry.UsageKey)
			}
			seenUsage[entry.UsageKey] = true
			combined := byKey[entry.UsageKey]
			if combined == nil {
				combined = &UsageEntry{UsageKey: entry.UsageKey, Namespace: entry.Namespace, TypeName: entry.TypeName, MemberName: entry.MemberName}
				byKey[entry.UsageKey] = combined
			} else if combined.Namespace != entry.Namespace || combined.TypeName != entry.TypeName || combined.MemberName != entry.MemberName {
				return CombinedRepositoryUsage{}, fmt.Errorf("inconsistent usage identity for %q", entry.UsageKey)
			}
			combined.PrivateProdRefs += entry.PrivProdRefs
			combined.PrivateTestRefs += entry.PrivTestRefs
			combined.RepositoryIDs = append(combined.RepositoryIDs, repository.RepositoryID)
		}
	}
	result := make([]UsageEntry, 0, len(byKey))
	for _, entry := range byKey {
		sort.Strings(entry.RepositoryIDs)
		result = append(result, *entry)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].UsageKey < result[j].UsageKey })
	return CombinedRepositoryUsage{Usage: result, RepositoryTreeBindingsSHA256: repositoryTreeBindingsSHA256(repositories)}, nil
}

func corpusUsageHasNegativeCounts(entry surfaceledger.CorpusUsageEntry) bool {
	return entry.PubProdRefs < 0 || entry.PubTestRefs < 0 || entry.PubFailRefs < 0 || entry.PrivProdRefs < 0 || entry.PrivTestRefs < 0 ||
		entry.PubProdFiles < 0 || entry.PubTestFiles < 0 || entry.PubFailFiles < 0 || entry.PrivProdFiles < 0 || entry.PrivTestFiles < 0 ||
		entry.PubProdProjects < 0 || entry.PubTestProjects < 0 || entry.PubFailProjects < 0 || entry.PrivProdProjects < 0 || entry.PrivTestProjects < 0
}

func repositoryTreeBindingsSHA256(repositories []RepositoryUsage) string {
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
