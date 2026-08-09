package corpusassurance

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/glade-sh/glade/tools/internal/surfaceledger"
)

var buildCorpusUsage = surfaceledger.BuildCorpusUsage

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
	SchemaVersion int             `json:"schemaVersion"`
	ProfileSHA256 string          `json:"profileSha256"`
	UsageSHA256   string          `json:"usageSha256"`
	Decisions     []UsageDecision `json:"decisions"`
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
	DecisionSHA256     string                  `json:"decisionSha256"`
	RawUsageSHA256     string                  `json:"rawUsageSha256"`
	Raw                CombinedRepositoryUsage `json:"raw"`
	Reconciliation     UsageReconciliation     `json:"reconciliation"`
}

// BuildSealedCorpusUsage is the Task 3 trust boundary. It derives every
// repository from the sealed inventory manifest twice, requires identical
// canonical raw usage, reconciles every positive key, and writes once.
func BuildSealedCorpusUsage(inventoryPath, ledgerPath, manifestPath, profilePath, decisionPath, outputPath string) (SealedCorpusUsage, error) {
	for _, path := range []string{inventoryPath, ledgerPath, manifestPath, profilePath, decisionPath, outputPath} {
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
	first, firstBytes, err := deriveSealedCombinedUsage(ledger.Rows, manifest, filepath.Dir(manifestPath))
	if err != nil {
		return SealedCorpusUsage{}, err
	}
	second, secondBytes, err := deriveSealedCombinedUsage(ledger.Rows, manifest, filepath.Dir(manifestPath))
	if err != nil {
		return SealedCorpusUsage{}, err
	}
	if !bytes.Equal(firstBytes, secondBytes) {
		return SealedCorpusUsage{}, fmt.Errorf("corpus usage extraction is not byte-identical")
	}
	profile, profileBytes, err := readUsageProfileRows(profilePath)
	if err != nil {
		return SealedCorpusUsage{}, err
	}
	profile, err = usageProfileRowsFromLedger(profile, ledger.Rows)
	if err != nil {
		return SealedCorpusUsage{}, err
	}
	decision, decisionBytes, err := readExactJSONBytes[UsageDecisionFile](decisionPath)
	if err != nil {
		return SealedCorpusUsage{}, err
	}
	if decision.SchemaVersion != 1 || decision.ProfileSHA256 != replayBytesSHA256(profileBytes) || decision.UsageSHA256 != replayBytesSHA256(firstBytes) {
		return SealedCorpusUsage{}, fmt.Errorf("usage decisions do not bind fresh extraction")
	}
	reconciliation, err := reconcileUsage(profile, first.Usage, decision.Decisions)
	if err != nil {
		return SealedCorpusUsage{}, err
	}
	reconciliation.ProfileSHA256, reconciliation.UsageSHA256, reconciliation.DecisionSHA256 = replayBytesSHA256(profileBytes), replayBytesSHA256(firstBytes), replayBytesSHA256(decisionBytes)
	artifact := SealedCorpusUsage{SchemaVersion: 1, InventorySHA256: replayBytesSHA256(inventoryBytes), RootManifestSHA256: replayBytesSHA256(manifestBytes), LedgerSHA256: replayBytesSHA256(ledgerBytes), ProfileSHA256: replayBytesSHA256(profileBytes), DecisionSHA256: replayBytesSHA256(decisionBytes), RawUsageSHA256: replayBytesSHA256(firstBytes), Raw: second, Reconciliation: reconciliation}
	if err := verifySealedUsageInputs([]sealedUsageInput{{inventoryPath, inventoryBytes}, {ledgerPath, ledgerBytes}, {manifestPath, manifestBytes}, {profilePath, profileBytes}, {decisionPath, decisionBytes}}); err != nil {
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

// ReconcileUsageFromFiles reads and binds the authoritative profile, fresh
// usage, and decision bytes before returning a reconciliation. Source-profile
// fields outside the allowlisted row projection are deliberately ignored.
func reconcileUsageFromFiles(profilePath, usagePath, decisionPath string) (UsageReconciliation, error) {
	if !filepath.IsAbs(profilePath) || !filepath.IsAbs(usagePath) || !filepath.IsAbs(decisionPath) {
		return UsageReconciliation{}, fmt.Errorf("absolute profile, usage, and decision paths are required")
	}
	profile, profileBytes, err := readUsageProfileRows(profilePath)
	if err != nil {
		return UsageReconciliation{}, fmt.Errorf("read usage profile: %w", err)
	}
	usage, usageBytes, err := readExactJSONBytes[CombinedRepositoryUsage](usagePath)
	if err != nil {
		return UsageReconciliation{}, fmt.Errorf("read fresh usage: %w", err)
	}
	decision, decisionBytes, err := readExactJSONBytes[UsageDecisionFile](decisionPath)
	if err != nil {
		return UsageReconciliation{}, fmt.Errorf("read usage decisions: %w", err)
	}
	profileSHA256, usageSHA256, decisionSHA256 := replayBytesSHA256(profileBytes), replayBytesSHA256(usageBytes), replayBytesSHA256(decisionBytes)
	if decision.SchemaVersion != 1 || decision.ProfileSHA256 != profileSHA256 || decision.UsageSHA256 != usageSHA256 {
		return UsageReconciliation{}, fmt.Errorf("usage decisions do not bind authoritative inputs")
	}
	reconciled, err := reconcileUsage(profile, usage.Usage, decision.Decisions)
	if err != nil {
		return UsageReconciliation{}, err
	}
	if _, after, err := readUsageProfileRows(profilePath); err != nil || replayBytesSHA256(after) != profileSHA256 {
		return UsageReconciliation{}, fmt.Errorf("profile changed during usage reconciliation")
	}
	if _, after, err := readExactJSONBytes[CombinedRepositoryUsage](usagePath); err != nil || replayBytesSHA256(after) != usageSHA256 {
		return UsageReconciliation{}, fmt.Errorf("fresh usage changed during reconciliation")
	}
	if _, after, err := readExactJSONBytes[UsageDecisionFile](decisionPath); err != nil || replayBytesSHA256(after) != decisionSHA256 {
		return UsageReconciliation{}, fmt.Errorf("usage decisions changed during reconciliation")
	}
	reconciled.ProfileSHA256, reconciled.UsageSHA256, reconciled.DecisionSHA256 = profileSHA256, usageSHA256, decisionSHA256
	return reconciled, nil
}

func readUsageProfileRows(path string) ([]UsageProfileRow, []byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	var profile struct {
		Rows []UsageProfileRow `json:"rows"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&profile); err != nil {
		return nil, nil, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, nil, fmt.Errorf("multiple JSON values")
		}
		return nil, nil, err
	}
	return profile.Rows, data, nil
}

// usageProfileRowsFromLedger assigns the canonical usage key from the sealed
// ledger. Source-profile usage keys are ignored so they cannot select or
// redirect private-corpus reconciliation.
func usageProfileRowsFromLedger(profile []UsageProfileRow, ledger []surfaceledger.SurfaceLedgerRow) ([]UsageProfileRow, error) {
	ledgerByID := make(map[string]surfaceledger.SurfaceLedgerRow, len(ledger))
	for _, row := range ledger {
		if row.SurfaceID == "" || ledgerByID[row.SurfaceID].SurfaceID != "" {
			return nil, fmt.Errorf("invalid or duplicate ledger surface %q", row.SurfaceID)
		}
		ledgerByID[row.SurfaceID] = row
	}
	derived := make([]UsageProfileRow, len(profile))
	for i, row := range profile {
		ledgerRow, exists := ledgerByID[row.SurfaceID]
		if row.SurfaceID == "" || !exists {
			return nil, fmt.Errorf("profile surface %q is absent from ledger", row.SurfaceID)
		}
		row.UsageKey = surfaceledger.UsageKeyForRow(ledgerRow)
		if row.UsageKey == "" {
			return nil, fmt.Errorf("ledger surface %q lacks a usage key", row.SurfaceID)
		}
		derived[i] = row
	}
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
		if candidates := profilesByKey[entry.UsageKey]; len(candidates) == 1 {
			row.Class, row.SurfaceID = usageClassExact, candidates[0].SurfaceID
		} else if candidates := profilesByFoldedKey[strings.ToLower(entry.UsageKey)]; len(candidates) == 1 {
			row.Class, row.SurfaceID = usageClassCaseAlias, candidates[0].SurfaceID
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
