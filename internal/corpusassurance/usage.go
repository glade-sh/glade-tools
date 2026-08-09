package corpusassurance

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sort"

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
func CombineRepositoryUsageFromFiles(inventoryPath, ledgerPath, rootManifestPath string, repositoryUsagePaths []string) (CombinedRepositoryUsage, error) {
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
