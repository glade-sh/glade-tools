package corpusassurance

import (
	"bytes"
	"context"
	"crypto/sha256"
	"debug/macho"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/glade-sh/glade/internal/semanticcache"
)

type ReplayCommand struct {
	Path                  string
	Args                  []string
	Env                   []string
	WorkingDirectory      string
	Timeout               time.Duration
	ExecutableSHA256      string
	ExecutableAfterSHA256 string
}

type ReplayRepository struct {
	Repository   RepositorySpec
	SourcePath   string
	SnapshotRoot string
}

type ReplayRequest struct {
	Host             string
	Candidate        RuntimeArtifact
	CandidatePath    string
	Tools            RuntimeArtifact
	ToolsPath        string
	InventoryPath    string
	RootManifestPath string
	HostManifestPath string
	OutputPath       string
	architecture     func(string) (string, error)
}

const replayTimeout = 15 * time.Minute

const replayWorkspaceIdentity = "isolated-sealed-snapshot"

const replaySemanticCachePath = ".glade/semantic"

const replayTestPerfPath = ".glade/assurance-test-perf.json"

var fixedReplayEnvironment = []string{"HOME=/var/empty", "PATH=/usr/bin:/bin", "TMPDIR=/tmp"}

var removeReplayWorkspace = os.RemoveAll

type CommandResult struct {
	Command               []string               `json:"command"`
	CommandSpecSHA256     string                 `json:"commandSpecSha256"`
	WorkingDirectory      string                 `json:"workingDirectory,omitempty"`
	Environment           []string               `json:"environment,omitempty"`
	ExecutableSHA256      string                 `json:"executableSha256,omitempty"`
	ExecutableAfterSHA256 string                 `json:"executableAfterSha256,omitempty"`
	ExitCode              int                    `json:"exitCode"`
	DurationMS            int64                  `json:"durationMs"`
	StdoutSHA256          string                 `json:"stdoutSha256"`
	StderrSHA256          string                 `json:"stderrSha256"`
	Output                *RetainedCommandOutput `json:"output,omitempty"`
	Passed                bool                   `json:"passed"`
	TimedOut              bool                   `json:"timedOut,omitempty"`
}

// RetainedCommandOutput is private execution evidence. It is omitted from
// public reports, but lets Salesforce reconciliation verify observed facts.
type RetainedCommandOutput struct {
	Stdout []byte `json:"stdout"`
	Stderr []byte `json:"stderr"`
}

type ReplayRepositoryResult struct {
	RepositoryID        string                      `json:"repositoryId"`
	SourceSHA256        string                      `json:"sourceSha256"`
	ExecutionTreeSHA256 string                      `json:"executionTreeSha256"`
	CandidateSHA256     string                      `json:"candidateSha256"`
	ToolsSHA256         string                      `json:"toolsSha256"`
	CheckSpecSHA256     string                      `json:"checkSpecSha256"`
	LocalTestSpecSHA256 string                      `json:"localTestSpecSha256,omitempty"`
	TestShardCount      int                         `json:"testShardCount,omitempty"`
	TestShardIndex      int                         `json:"testShardIndex,omitempty"`
	SemanticCache       ReplaySemanticCacheEvidence `json:"semanticCache,omitempty"`
	Check               CommandResult               `json:"check"`
	LocalTest           *CommandResult              `json:"localTest,omitempty"`
}

// ReplaySemanticCacheEvidence binds a required local test to the exact cache
// the preceding check created in its isolated workspace.
type ReplaySemanticCacheEvidence struct {
	Path           string `json:"path"`
	SHA256         string `json:"sha256"`
	IdentitySHA256 string `json:"identitySha256"`
	PerfSHA256     string `json:"perfSha256"`
	DiskHits       uint64 `json:"diskHits"`
}

type ReplayShard struct {
	Status       string                   `json:"status"`
	Host         string                   `json:"host"`
	AttemptRoot  string                   `json:"attemptRoot"`
	OS           string                   `json:"os"`
	Arch         string                   `json:"arch"`
	Candidate    RuntimeArtifact          `json:"candidate"`
	Tools        RuntimeArtifact          `json:"tools"`
	Bindings     ReplayBindings           `json:"bindings"`
	Repositories []ReplayRepositoryResult `json:"repositories"`
}

type ReplayBindings struct {
	InventorySHA256    string `json:"inventorySha256"`
	RootManifestSHA256 string `json:"rootManifestSha256"`
	HostManifestSHA256 string `json:"hostManifestSha256"`
}

type ReplayMerge struct {
	Candidate             RuntimeArtifact   `json:"candidate"`
	Tools                 RuntimeArtifact   `json:"tools"`
	Inventory             InventoryManifest `json:"inventory"`
	RootManifestSHA256    string            `json:"rootManifestSha256"`
	HostManifestSHA256    map[string]string `json:"hostManifestSha256"`
	Repositories          []RepositorySpec  `json:"repositories"`
	TestReadyByRepository map[string]bool   `json:"testReadyByRepository"`
}

// ValidateReplayFiles is the reconciliation entrypoint. It reads each sealed
// manifest and raw shard once, derives all hash bindings from those bytes, and
// rejects a postflight change before accepting the merge.
func ValidateReplayFiles(inventoryPath, rootManifestPath string, hostManifestPaths, shardPaths []string) error {
	_, err := loadReplayMergeFromFiles(inventoryPath, rootManifestPath, hostManifestPaths, shardPaths)
	return err
}

// MergeReplayFromFiles seals the validated local/replay-worker merge as a
// create-only receipt for the final assurance report.
func MergeReplayFromFiles(inventoryPath, rootManifestPath string, hostManifestPaths, shardPaths []string, outputPath string) (ReplayMerge, error) {
	if !filepath.IsAbs(outputPath) {
		return ReplayMerge{}, fmt.Errorf("replay merge output must be absolute")
	}
	if _, err := os.Lstat(outputPath); err == nil {
		return ReplayMerge{}, fmt.Errorf("replay merge output already exists: %s", outputPath)
	} else if !os.IsNotExist(err) {
		return ReplayMerge{}, err
	}
	merge, err := loadReplayMergeFromFiles(inventoryPath, rootManifestPath, hostManifestPaths, shardPaths)
	if err != nil {
		return ReplayMerge{}, err
	}
	if err := WriteNewJSON(outputPath, merge); err != nil {
		return ReplayMerge{}, err
	}
	return merge, nil
}

func loadReplayMergeFromFiles(inventoryPath, rootManifestPath string, hostManifestPaths, shardPaths []string) (ReplayMerge, error) {
	inventory, inventoryBytes, err := readInventorySpec(inventoryPath)
	if err != nil {
		return ReplayMerge{}, fmt.Errorf("read IN_SCOPE inventory: %w", err)
	}
	root, rootBytes, err := readExactJSONBytes[InventoryManifest](rootManifestPath)
	if err != nil {
		return ReplayMerge{}, fmt.Errorf("read root manifest: %w", err)
	}
	if root.SchemaVersion != 1 || root.InventorySHA256 != replayBytesSHA256(inventoryBytes) || ValidateAssuranceAttempt(root.Attempt) != nil || root.Attempt.InventorySHA256 != root.InventorySHA256 || ValidateInventoryCoverage(inventory, root.Repositories) != nil {
		return ReplayMerge{}, fmt.Errorf("invalid root manifest")
	}
	if len(hostManifestPaths) == 0 || len(shardPaths) == 0 {
		return ReplayMerge{}, fmt.Errorf("host manifests and replay shards are required")
	}
	hostHashes := make(map[string]string, len(hostManifestPaths))
	hostFileHashes := make([]string, 0, len(hostManifestPaths))
	for _, path := range hostManifestPaths {
		host, data, err := readExactJSONBytes[HostManifest](path)
		if err != nil {
			return ReplayMerge{}, fmt.Errorf("read host manifest: %w", err)
		}
		if host.SchemaVersion != 1 || host.RootManifestSHA256 != replayBytesSHA256(rootBytes) || (host.Host != "local" && host.Host != "replay-worker") {
			return ReplayMerge{}, fmt.Errorf("invalid host manifest")
		}
		expected := make(map[string]RepositorySpec)
		for _, repository := range root.Repositories {
			if repositoryReplaysOnHost(repository, host.Host) {
				expected[repository.ID] = repository
			}
		}
		if len(host.Repositories) != len(expected) {
			return ReplayMerge{}, fmt.Errorf("host manifest repository count mismatch")
		}
		for _, repository := range host.Repositories {
			if expected[repository.ID] != repository {
				return ReplayMerge{}, fmt.Errorf("host manifest repository does not match root")
			}
			delete(expected, repository.ID)
		}
		if len(expected) != 0 {
			return ReplayMerge{}, fmt.Errorf("host manifest is missing root repositories")
		}
		if _, duplicate := hostHashes[host.Host]; duplicate {
			return ReplayMerge{}, fmt.Errorf("duplicate host manifest %q", host.Host)
		}
		hostHashes[host.Host] = replayBytesSHA256(data)
		hostFileHashes = append(hostFileHashes, replayBytesSHA256(data))
	}
	shards := make([]ReplayShard, 0, len(shardPaths))
	shardHashes := make([]string, 0, len(shardPaths))
	for _, path := range shardPaths {
		shard, data, err := readExactJSONBytes[ReplayShard](path)
		if err != nil {
			return ReplayMerge{}, fmt.Errorf("read replay shard: %w", err)
		}
		shards = append(shards, shard)
		shardHashes = append(shardHashes, replayBytesSHA256(data))
	}
	if len(shards) == 0 {
		return ReplayMerge{}, fmt.Errorf("replay shards are required")
	}
	merge := ReplayMerge{Candidate: root.Attempt.Candidate, Tools: root.Attempt.Tools, Inventory: root, RootManifestSHA256: replayBytesSHA256(rootBytes), HostManifestSHA256: hostHashes, Repositories: root.Repositories}
	testReady, err := repositoryTestReadiness(merge, shards)
	if err != nil {
		return ReplayMerge{}, err
	}
	merge.TestReadyByRepository = testReady
	if err := ValidateReplayMerge(merge, shards); err != nil {
		return ReplayMerge{}, err
	}
	_, postRoot, err := readExactJSONBytes[InventoryManifest](rootManifestPath)
	if err != nil || replayBytesSHA256(postRoot) != replayBytesSHA256(rootBytes) {
		return ReplayMerge{}, fmt.Errorf("root manifest changed during replay reconciliation")
	}
	_, postInventory, err := readInventorySpec(inventoryPath)
	if err != nil || replayBytesSHA256(postInventory) != replayBytesSHA256(inventoryBytes) {
		return ReplayMerge{}, fmt.Errorf("IN_SCOPE inventory changed during replay reconciliation")
	}
	for index, path := range hostManifestPaths {
		_, data, err := readExactJSONBytes[HostManifest](path)
		if err != nil || replayBytesSHA256(data) != hostFileHashes[index] {
			return ReplayMerge{}, fmt.Errorf("host manifest changed during replay reconciliation")
		}
	}
	for index, path := range shardPaths {
		_, data, err := readExactJSONBytes[ReplayShard](path)
		if err != nil || replayBytesSHA256(data) != shardHashes[index] {
			return ReplayMerge{}, fmt.Errorf("replay shard changed during reconciliation")
		}
	}
	return merge, nil
}

func RunReplay(request ReplayRequest) (ReplayShard, error) {
	inputs, err := LoadSealedHostInputs(request.InventoryPath, request.RootManifestPath, request.HostManifestPath, request.Host)
	if err != nil {
		return ReplayShard{}, err
	}
	request.Candidate, request.Tools = inputs.Root.Attempt.Candidate, inputs.Root.Attempt.Tools
	repositories, err := validateReplayRequest(request, inputs)
	if err != nil {
		return ReplayShard{}, err
	}
	if _, err := os.Lstat(request.OutputPath); err == nil {
		return ReplayShard{}, fmt.Errorf("replay output already exists: %s", request.OutputPath)
	} else if !os.IsNotExist(err) {
		return ReplayShard{}, err
	}

	attemptRoot, err := canonicalReplayAttemptRoot(request.HostManifestPath)
	if err != nil {
		return ReplayShard{}, err
	}
	shard := ReplayShard{Host: request.Host, AttemptRoot: attemptRoot, OS: runtime.GOOS, Arch: runtime.GOARCH, Candidate: request.Candidate, Tools: request.Tools, Bindings: inputs.Bindings, Status: "pass"}
	for _, repository := range repositories {
		for _, testShardIndex := range replayTestShardIndices(repository.Repository, request.Host) {
			result, err := runReplayRepository(repository, request, testShardIndex)
			if err != nil {
				return ReplayShard{}, err
			}
			if !validIsolatedReplayReceipt(result.Check, "check", request.Candidate.SHA256, replayCommandSpecSHA256("check", request.Candidate.SHA256)) || (result.LocalTest != nil && !validIsolatedReplayReceipt(*result.LocalTest, "test", request.Candidate.SHA256, replayCommandSpecSHA256("test", request.Candidate.SHA256, result.TestShardCount, result.TestShardIndex), result.TestShardCount, result.TestShardIndex)) {
				shard.Status = "fail"
			}
			shard.Repositories = append(shard.Repositories, result)
		}
	}
	for _, repository := range repositories {
		got, err := replayFileSHA256(repository.SourcePath)
		if err != nil {
			return ReplayShard{}, err
		}
		if got != repository.Repository.ArchiveSHA256 {
			return ReplayShard{}, fmt.Errorf("source changed during replay for %q", repository.Repository.ID)
		}
		if err := verifyReplayTree(repository); err != nil {
			return ReplayShard{}, err
		}
	}
	refreshed, err := LoadSealedHostInputs(request.InventoryPath, request.RootManifestPath, request.HostManifestPath, request.Host)
	if err != nil || refreshed.Bindings != inputs.Bindings {
		return ReplayShard{}, fmt.Errorf("sealed manifests changed during replay")
	}
	if err := validateReplayRuntimeBindings(request); err != nil {
		return ReplayShard{}, err
	}
	if err := WriteNewJSON(request.OutputPath, shard); err != nil {
		return ReplayShard{}, err
	}
	return shard, nil
}

func canonicalReplayAttemptRoot(hostManifestPath string) (string, error) {
	if !filepath.IsAbs(hostManifestPath) {
		return "", fmt.Errorf("replay host manifest path must be absolute")
	}
	resolved, err := filepath.EvalSymlinks(hostManifestPath)
	if err != nil {
		return "", fmt.Errorf("resolve replay host manifest: %w", err)
	}
	root := filepath.Dir(resolved)
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("replay attempt root is not absolute")
	}
	return root, nil
}

func runReplayRepository(repository ReplayRepository, request ReplayRequest, testShardIndex int) (result ReplayRepositoryResult, err error) {
	workspace, err := os.MkdirTemp("", "glade-assurance-replay-*")
	if err != nil {
		return ReplayRepositoryResult{}, err
	}
	defer func() {
		if cleanupErr := removeReplayWorkspace(workspace); cleanupErr != nil {
			result = ReplayRepositoryResult{}
			if err == nil {
				err = fmt.Errorf("cleanup replay workspace for %q: %w", repository.Repository.ID, cleanupErr)
			} else {
				err = fmt.Errorf("%v; cleanup replay workspace for %q: %w", err, repository.Repository.ID, cleanupErr)
			}
		}
	}()
	workingRoot := filepath.Join(workspace, "snapshot")
	if err := copyTree(repository.SnapshotRoot, workingRoot); err != nil {
		return ReplayRepositoryResult{}, fmt.Errorf("copy sealed snapshot for %q: %w", repository.Repository.ID, err)
	}
	executionTreeSHA256, err := canonicalTreeSHA256(workingRoot)
	if err != nil || executionTreeSHA256 != repository.Repository.TreeSHA256 {
		return ReplayRepositoryResult{}, fmt.Errorf("isolated snapshot binding mismatch for %q", repository.Repository.ID)
	}
	if err := requireReplayCacheAbsent(workingRoot); err != nil {
		return ReplayRepositoryResult{}, fmt.Errorf("isolated cache for %q: %w", repository.Repository.ID, err)
	}
	check := replayCommandFor(request.CandidatePath, "check")
	if err := validateReplayRuntimeBindings(request); err != nil {
		return ReplayRepositoryResult{}, err
	}
	result = ReplayRepositoryResult{
		RepositoryID: repository.Repository.ID, SourceSHA256: repository.Repository.ArchiveSHA256,
		ExecutionTreeSHA256: executionTreeSHA256,
		CandidateSHA256:     request.Candidate.SHA256, ToolsSHA256: request.Tools.SHA256,
		Check: runReplayCommand(workingRoot, check),
	}
	result.CheckSpecSHA256 = result.Check.CommandSpecSHA256
	if repository.Repository.LocalTests == "required" {
		var cache ReplaySemanticCacheEvidence
		if result.Check.Passed {
			cache, err = replaySemanticCacheEvidence(workingRoot)
			if err != nil {
				return ReplayRepositoryResult{}, fmt.Errorf("check cache proof for %q: %w", repository.Repository.ID, err)
			}
		}
		result.TestShardCount, result.TestShardIndex = repository.Repository.TestShardCount, testShardIndex
		localTestCommand := replayCommandFor(request.CandidatePath, "test", result.TestShardCount, result.TestShardIndex)
		if err := validateReplayRuntimeBindings(request); err != nil {
			return ReplayRepositoryResult{}, err
		}
		localTest := runReplayCommand(workingRoot, localTestCommand)
		result.LocalTest = &localTest
		result.LocalTestSpecSHA256 = localTest.CommandSpecSHA256
		if result.Check.Passed && localTest.Passed {
			perf, err := replayTestSemanticDiskHits(workingRoot)
			if err != nil {
				return ReplayRepositoryResult{}, fmt.Errorf("test cache proof for %q: %w", repository.Repository.ID, err)
			}
			if perf.DiskHits == 0 {
				return ReplayRepositoryResult{}, fmt.Errorf("test did not use the sealed semantic cache for %q", repository.Repository.ID)
			}
			postTestCache, err := replaySemanticCacheEvidence(workingRoot)
			if err != nil || postTestCache.Path != cache.Path || postTestCache.SHA256 != cache.SHA256 || postTestCache.IdentitySHA256 != cache.IdentitySHA256 {
				return ReplayRepositoryResult{}, fmt.Errorf("semantic cache changed during test for %q", repository.Repository.ID)
			}
			cache.PerfSHA256, cache.DiskHits = perf.SHA256, perf.DiskHits
			result.SemanticCache = cache
		}
	}
	return result, nil
}

func ValidateReplayMerge(merge ReplayMerge, shards []ReplayShard) error {
	if err := ValidateRuntimeArtifact(merge.Candidate); err != nil {
		return fmt.Errorf("candidate: %w", err)
	}
	if err := ValidateRuntimeArtifact(merge.Tools); err != nil {
		return fmt.Errorf("tools: %w", err)
	}
	if err := validateReplayDenominator(merge); err != nil {
		return err
	}
	if err := validateReplayTestReadiness(merge.Repositories, merge.TestReadyByRepository); err != nil {
		return err
	}
	expected := repositoryIndex(merge.Inventory.Repositories)
	seen := make(map[string]map[int]bool, len(expected))
	seenHosts := make(map[string]bool, len(merge.HostManifestSHA256))
	for _, shard := range shards {
		if shard.Candidate != merge.Candidate || shard.Tools != merge.Tools {
			return fmt.Errorf("artifact binding mismatch for host %q", shard.Host)
		}
		if shard.Status != "pass" || shard.OS != merge.Candidate.OS || shard.Arch != merge.Candidate.Arch {
			return fmt.Errorf("invalid replay shard state for host %q", shard.Host)
		}
		hostManifestSHA256, exists := merge.HostManifestSHA256[shard.Host]
		if !exists || shard.Bindings.InventorySHA256 != merge.Inventory.InventorySHA256 || shard.Bindings.RootManifestSHA256 != merge.RootManifestSHA256 || shard.Bindings.HostManifestSHA256 != hostManifestSHA256 {
			return fmt.Errorf("manifest binding mismatch for host %q", shard.Host)
		}
		seenHosts[shard.Host] = true
		for _, result := range shard.Repositories {
			repository, exists := expected[result.RepositoryID]
			if !exists {
				return fmt.Errorf("unexpected repository %q", result.RepositoryID)
			}
			if result.SourceSHA256 != repository.ArchiveSHA256 || result.ExecutionTreeSHA256 != repository.TreeSHA256 || result.CandidateSHA256 != merge.Candidate.SHA256 || result.ToolsSHA256 != merge.Tools.SHA256 {
				return fmt.Errorf("repository binding mismatch for %q", result.RepositoryID)
			}
			if err := validateReplayResultShard(repository, shard.Host, result); err != nil {
				return err
			}
			if seen[result.RepositoryID] == nil {
				seen[result.RepositoryID] = map[int]bool{}
			}
			if seen[result.RepositoryID][result.TestShardIndex] {
				return fmt.Errorf("duplicate test shard %d for %q", result.TestShardIndex, result.RepositoryID)
			}
			seen[result.RepositoryID][result.TestShardIndex] = true
			if result.CheckSpecSHA256 != replayCommandSpecSHA256("check", merge.Candidate.SHA256) || !validIsolatedReplayReceipt(result.Check, "check", merge.Candidate.SHA256, replayCommandSpecSHA256("check", merge.Candidate.SHA256)) {
				return fmt.Errorf("check failed for %q", result.RepositoryID)
			}
			if repository.LocalTests == "required" && (result.LocalTest == nil || result.LocalTestSpecSHA256 != replayCommandSpecSHA256("test", merge.Candidate.SHA256, result.TestShardCount, result.TestShardIndex) || !validIsolatedReplayReceipt(*result.LocalTest, "test", merge.Candidate.SHA256, replayCommandSpecSHA256("test", merge.Candidate.SHA256, result.TestShardCount, result.TestShardIndex), result.TestShardCount, result.TestShardIndex) || !validReplaySemanticCacheEvidence(result.SemanticCache)) {
				return fmt.Errorf("required local test failed for %q", result.RepositoryID)
			}
		}
	}
	for id, repository := range expected {
		if len(seen[id]) != replayResultCount(repository) {
			return fmt.Errorf("missing repository result %q", id)
		}
		if _, exists := merge.TestReadyByRepository[id]; !exists {
			return fmt.Errorf("missing repository test readiness for %q", id)
		}
	}
	for host := range merge.HostManifestSHA256 {
		if !seenHosts[host] {
			return fmt.Errorf("missing replay shard for host %q", host)
		}
	}
	return nil
}

func replayResultCount(repository RepositorySpec) int {
	if repository.LocalTests == "required" && repository.TestShardCount > 0 {
		return repository.TestShardCount
	}
	return 1
}

func validateReplayResultShard(repository RepositorySpec, host string, result ReplayRepositoryResult) error {
	if repository.LocalTests != "required" {
		if host != repository.AssignedHost || result.TestShardCount != 0 || result.TestShardIndex != 0 || result.LocalTest != nil {
			return fmt.Errorf("invalid no-test replay result for %q", repository.ID)
		}
		return nil
	}
	if repository.TestShardCount == 0 {
		if host != repository.AssignedHost || result.TestShardCount != 0 || result.TestShardIndex != 0 {
			return fmt.Errorf("invalid unsharded replay result for %q", repository.ID)
		}
		return nil
	}
	if result.TestShardCount != repository.TestShardCount || result.TestShardIndex < 0 || result.TestShardIndex >= repository.TestShardCount {
		return fmt.Errorf("invalid test shard for %q", repository.ID)
	}
	for _, index := range replayTestShardIndices(repository, host) {
		if index == result.TestShardIndex {
			return nil
		}
	}
	return fmt.Errorf("test shard %d runs on the wrong host for %q", result.TestShardIndex, repository.ID)
}

func validateReplayTestReadiness(repositories []RepositorySpec, readiness map[string]bool) error {
	if len(readiness) != len(repositories) {
		return fmt.Errorf("replay merge test readiness is incomplete")
	}
	for _, repository := range repositories {
		expected := repository.LocalTests == "required"
		actual, exists := readiness[repository.ID]
		if !exists || actual != expected {
			return fmt.Errorf("invalid replay test readiness for %q", repository.ID)
		}
	}
	return nil
}

func validateReplayRootBinding(merge ReplayMerge, root InventoryManifest) error {
	if !equalInventoryManifest(merge.Inventory, root) || len(merge.Repositories) != len(root.Repositories) {
		return fmt.Errorf("replay merge repository denominator does not match root manifest")
	}
	if err := ValidateAssuranceAttempt(root.Attempt); err != nil || root.Attempt.InventorySHA256 != root.InventorySHA256 || merge.Candidate != root.Attempt.Candidate || merge.Tools != root.Attempt.Tools {
		return fmt.Errorf("replay merge artifacts do not match the sealed attempt")
	}
	expected := repositoryIndex(root.Repositories)
	seen := make(map[string]bool, len(expected))
	for _, repository := range merge.Repositories {
		canonical, exists := expected[repository.ID]
		if !exists || seen[repository.ID] || canonical != repository {
			return fmt.Errorf("replay merge repository does not match root manifest for %q", repository.ID)
		}
		seen[repository.ID] = true
	}
	return validateReplayTestReadiness(root.Repositories, merge.TestReadyByRepository)
}

func equalInventoryManifest(left, right InventoryManifest) bool {
	if left.SchemaVersion != right.SchemaVersion || left.InventorySHA256 != right.InventorySHA256 || !reflect.DeepEqual(left.Attempt, right.Attempt) || len(left.Repositories) != len(right.Repositories) {
		return false
	}
	for index := range left.Repositories {
		if left.Repositories[index] != right.Repositories[index] {
			return false
		}
	}
	return true
}

func validateReplayDenominator(merge ReplayMerge) error {
	if merge.Inventory.SchemaVersion != 1 || !sha256Pattern.MatchString(merge.Inventory.InventorySHA256) || !sha256Pattern.MatchString(merge.RootManifestSHA256) || len(merge.Inventory.Repositories) == 0 || len(merge.HostManifestSHA256) == 0 {
		return fmt.Errorf("invalid replay manifest denominator")
	}
	for host, hash := range merge.HostManifestSHA256 {
		if (host != "local" && host != "replay-worker") || !sha256Pattern.MatchString(hash) {
			return fmt.Errorf("invalid host manifest binding for %q", host)
		}
	}
	inventory := repositoryIndex(merge.Inventory.Repositories)
	if len(inventory) != len(merge.Inventory.Repositories) || len(merge.Repositories) != len(merge.Inventory.Repositories) {
		return fmt.Errorf("replay repositories do not match inventory denominator")
	}
	repositories := repositoryIndex(merge.Repositories)
	if len(repositories) != len(merge.Repositories) {
		return fmt.Errorf("duplicate replay repository")
	}
	for _, repository := range merge.Inventory.Repositories {
		if err := ValidateRepositorySpec(repository); err != nil {
			return err
		}
		for _, host := range []string{"local", "replay-worker"} {
			if repositoryReplaysOnHost(repository, host) {
				if _, ok := merge.HostManifestSHA256[host]; !ok {
					return fmt.Errorf("missing host manifest binding for %q", host)
				}
			}
		}
		candidate, ok := repositories[repository.ID]
		if !ok || candidate != repository {
			return fmt.Errorf("replay repository %q does not match inventory denominator", repository.ID)
		}
	}
	return nil
}

func repositoryIndex(repositories []RepositorySpec) map[string]RepositorySpec {
	indexed := make(map[string]RepositorySpec, len(repositories))
	for _, repository := range repositories {
		indexed[repository.ID] = repository
	}
	return indexed
}

func validReplayReceipt(result CommandResult, operation, candidateSHA256, expectedSpecSHA256 string, testShard ...int) bool {
	return result.Passed && !result.TimedOut && result.ExitCode == 0 && result.DurationMS >= 0 && len(result.Command) == 1 && result.Command[0] == operation && result.CommandSpecSHA256 == expectedSpecSHA256 && result.CommandSpecSHA256 == replayCommandSpecSHA256(operation, result.ExecutableSHA256, testShard...) && sha256Pattern.MatchString(candidateSHA256) && result.ExecutableSHA256 == candidateSHA256 && sha256Pattern.MatchString(expectedSpecSHA256) && sha256Pattern.MatchString(result.ExecutableSHA256) && result.ExecutableSHA256 == result.ExecutableAfterSHA256 && sha256Pattern.MatchString(result.StdoutSHA256) && sha256Pattern.MatchString(result.StderrSHA256)
}

func validIsolatedReplayReceipt(result CommandResult, operation, candidateSHA256, expectedSpecSHA256 string, testShard ...int) bool {
	return result.WorkingDirectory == replayWorkspaceIdentity && validRetainedCommandOutput(result) && validReplayReceipt(result, operation, candidateSHA256, expectedSpecSHA256, testShard...)
}

func repositoryReplaysOnHost(repository RepositorySpec, host string) bool {
	return len(replayTestShardIndices(repository, host)) != 0
}

func replayTestShardIndices(repository RepositorySpec, host string) []int {
	if repository.LocalTests != "required" || repository.TestShardCount == 0 {
		if repository.AssignedHost == host {
			return []int{0}
		}
		return nil
	}
	indices := make([]int, 0, (repository.TestShardCount+1)/2)
	for index := 0; index < repository.TestShardCount; index++ {
		assigned := "local"
		if index%2 == 1 {
			assigned = "replay-worker"
		}
		if assigned == host {
			indices = append(indices, index)
		}
	}
	return indices
}

func validateReplayRequest(request ReplayRequest, inputs SealedHostInputs) ([]ReplayRepository, error) {
	if request.Host == "" || request.OutputPath == "" {
		return nil, fmt.Errorf("host and output path are required")
	}
	if err := ValidateRuntimeArtifact(request.Candidate); err != nil {
		return nil, fmt.Errorf("candidate: %w", err)
	}
	if err := ValidateRuntimeArtifact(request.Tools); err != nil {
		return nil, fmt.Errorf("tools: %w", err)
	}
	if request.Candidate.OS != runtime.GOOS || request.Candidate.Arch != runtime.GOARCH || request.Tools.OS != runtime.GOOS || request.Tools.Arch != runtime.GOARCH {
		return nil, fmt.Errorf("candidate and tools must match host %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	if err := validateReplayRuntimeBindings(request); err != nil {
		return nil, err
	}
	if len(inputs.Host.Repositories) == 0 {
		return nil, fmt.Errorf("host manifest has no repositories")
	}
	root, err := filepath.Abs(filepath.Dir(request.RootManifestPath))
	if err != nil {
		return nil, err
	}
	hostRoot, err := filepath.Abs(filepath.Dir(request.HostManifestPath))
	if err != nil {
		return nil, err
	}
	repositories := make([]ReplayRepository, 0, len(inputs.Host.Repositories))
	for _, manifestRepository := range inputs.Host.Repositories {
		snapshotRoot, err := rootedPath(hostRoot, manifestRepository.SnapshotPath)
		if err != nil {
			return nil, err
		}
		repository := ReplayRepository{
			Repository:   manifestRepository,
			SourcePath:   filepath.Join(root, "archives", manifestRepository.ID+".tar"),
			SnapshotRoot: snapshotRoot,
		}
		if got, err := replayFileSHA256(repository.SourcePath); err != nil || got != manifestRepository.ArchiveSHA256 {
			return nil, fmt.Errorf("source binding mismatch for %q", manifestRepository.ID)
		}
		if err := verifyReplayTree(repository); err != nil {
			return nil, err
		}
		repositories = append(repositories, repository)
	}
	return repositories, nil
}

func validateReplayRuntimeBindings(request ReplayRequest) error {
	if err := validateStagedRuntime("candidate", request.CandidatePath, request.Candidate, request.architecture); err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate executing glade-tools binary: %w", err)
	}
	requested, err := filepath.EvalSymlinks(request.ToolsPath)
	if err != nil {
		return fmt.Errorf("resolve tools path: %w", err)
	}
	executing, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return fmt.Errorf("resolve executing glade-tools binary: %w", err)
	}
	if filepath.Clean(requested) != filepath.Clean(executing) {
		return fmt.Errorf("tools path does not identify the executing glade-tools binary")
	}
	return validateStagedRuntime("tools", executable, request.Tools, request.architecture)
}

func replayCommandFor(candidatePath, operation string, testShard ...int) ReplayCommand {
	args := []string{operation, "--project", ".", "--json", "--no-progress"}
	if operation == "test" {
		args = append(args, "--perf-json", replayTestPerfPath)
		if len(testShard) == 2 && testShard[0] > 0 {
			args = append(args, "--shard-count", strconv.Itoa(testShard[0]), "--shard-index", strconv.Itoa(testShard[1]))
		}
	}
	return ReplayCommand{
		Path:             candidatePath,
		Args:             args,
		Env:              append([]string(nil), fixedReplayEnvironment...),
		WorkingDirectory: replayWorkspaceIdentity,
		Timeout:          replayTimeout,
	}
}

func requireReplayCacheAbsent(workingRoot string) error {
	for _, relativePath := range []string{replaySemanticCachePath, replayTestPerfPath} {
		if _, err := os.Lstat(filepath.Join(workingRoot, relativePath)); err == nil {
			return fmt.Errorf("preexisting %s", relativePath)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func replaySemanticCacheEvidence(workingRoot string) (ReplaySemanticCacheEvidence, error) {
	cachePath := filepath.Join(workingRoot, replaySemanticCachePath)
	entries, err := os.ReadDir(cachePath)
	if err != nil {
		return ReplaySemanticCacheEvidence{}, err
	}
	if len(entries) != 1 || entries[0].IsDir() || entries[0].Type()&os.ModeSymlink != 0 || !strings.HasPrefix(entries[0].Name(), "result-") || !strings.HasSuffix(entries[0].Name(), ".json") {
		return ReplaySemanticCacheEvidence{}, fmt.Errorf("expected one semantic result envelope")
	}
	data, err := os.ReadFile(filepath.Join(cachePath, entries[0].Name()))
	if err != nil {
		return ReplaySemanticCacheEvidence{}, err
	}
	var envelope struct {
		Identity semanticcache.Identity `json:"identity"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return ReplaySemanticCacheEvidence{}, fmt.Errorf("invalid semantic result envelope")
	}
	relativePath := filepath.Join(replaySemanticCachePath, entries[0].Name())
	if _, err := semanticcache.Load(workingRoot, relativePath, envelope.Identity); err != nil {
		return ReplaySemanticCacheEvidence{}, fmt.Errorf("validate semantic result envelope: %w", err)
	}
	identity, err := json.Marshal(envelope.Identity)
	if err != nil {
		return ReplaySemanticCacheEvidence{}, err
	}
	return ReplaySemanticCacheEvidence{Path: relativePath, SHA256: replayBytesSHA256(data), IdentitySHA256: replayBytesSHA256(identity)}, nil
}

func replayTestSemanticDiskHits(workingRoot string) (struct {
	SHA256   string
	DiskHits uint64
}, error) {
	data, err := os.ReadFile(filepath.Join(workingRoot, replayTestPerfPath))
	if err != nil {
		return struct {
			SHA256   string
			DiskHits uint64
		}{}, err
	}
	var perf struct {
		ApexPerf struct {
			Phases struct {
				SemanticDiskCacheHits uint64 `json:"semanticDiskCacheHits"`
			} `json:"phases"`
		} `json:"apexPerf"`
	}
	if err := json.Unmarshal(data, &perf); err != nil {
		return struct {
			SHA256   string
			DiskHits uint64
		}{}, err
	}
	return struct {
		SHA256   string
		DiskHits uint64
	}{SHA256: replayBytesSHA256(data), DiskHits: perf.ApexPerf.Phases.SemanticDiskCacheHits}, nil
}

func validReplaySemanticCacheEvidence(cache ReplaySemanticCacheEvidence) bool {
	return strings.HasPrefix(cache.Path, replaySemanticCachePath+"/result-") && strings.HasSuffix(cache.Path, ".json") && sha256Pattern.MatchString(cache.SHA256) && sha256Pattern.MatchString(cache.IdentitySHA256) && sha256Pattern.MatchString(cache.PerfSHA256) && cache.DiskHits > 0
}

func replayCommandSpecSHA256(operation, executableSHA256 string, testShard ...int) string {
	command := replayCommandFor("", operation, testShard...)
	command.ExecutableSHA256 = executableSHA256
	command.ExecutableAfterSHA256 = executableSHA256
	return commandSpecSHA256(command)
}

func validateStagedRuntime(name, path string, artifact RuntimeArtifact, inspect func(string) (string, error)) error {
	if path == "" || !filepath.IsAbs(path) {
		return fmt.Errorf("%s path must be absolute", name)
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("%s stat: %w", name, err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("%s must be an executable regular file", name)
	}
	got, err := replayFileSHA256(path)
	if err != nil {
		return fmt.Errorf("%s hash: %w", name, err)
	}
	if got != artifact.SHA256 {
		return fmt.Errorf("%s binding mismatch", name)
	}
	if inspect == nil {
		inspect = inspectMachOArchitecture
	}
	arch, err := inspect(path)
	if err != nil {
		return fmt.Errorf("%s architecture: %w", name, err)
	}
	if arch != artifact.Arch {
		return fmt.Errorf("%s architecture mismatch", name)
	}
	return nil
}

func inspectMachOArchitecture(path string) (string, error) {
	file, err := macho.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	switch file.Cpu {
	case macho.CpuArm64:
		return "arm64", nil
	case macho.CpuAmd64:
		return "amd64", nil
	default:
		return "", fmt.Errorf("unsupported cpu %v", file.Cpu)
	}
}

func verifyReplayTree(repository ReplayRepository) error {
	got, err := canonicalTreeSHA256(repository.SnapshotRoot)
	if err != nil {
		return fmt.Errorf("snapshot tree for %q: %w", repository.Repository.ID, err)
	}
	if got != repository.Repository.TreeSHA256 {
		return fmt.Errorf("snapshot tree changed for %q", repository.Repository.ID)
	}
	return nil
}

func validateReplayCommand(command ReplayCommand, operation string) error {
	if command.Path == "" || command.Timeout <= 0 || len(command.Args) == 0 || command.Args[0] != operation {
		return fmt.Errorf("%s command and positive timeout are required", operation)
	}
	return nil
}

func runReplayCommand(workingDir string, command ReplayCommand) CommandResult {
	result, stdout, stderr := runReplayCommandOutput(workingDir, command)
	result.Output = &RetainedCommandOutput{Stdout: append([]byte{}, stdout...), Stderr: append([]byte{}, stderr...)}
	return result
}

func runReplayCommandOutput(workingDir string, command ReplayCommand) (CommandResult, []byte, []byte) {
	ctx, cancel := context.WithTimeout(context.Background(), command.Timeout)
	defer cancel()
	var stdout, stderr bytes.Buffer
	started := time.Now()
	executableSHA256, hashErr := sha256File(command.Path)
	cmd := exec.CommandContext(ctx, command.Path, command.Args...)
	cmd.Dir, cmd.Stdout, cmd.Stderr = workingDir, &stdout, &stderr
	cmd.Env = append([]string(nil), command.Env...)
	err := cmd.Run()
	executableAfterSHA256, afterHashErr := sha256File(command.Path)
	command.ExecutableSHA256, command.ExecutableAfterSHA256 = executableSHA256, executableAfterSHA256
	result := CommandResult{
		Command: replayCommandLine(command), CommandSpecSHA256: commandSpecSHA256(command), WorkingDirectory: command.WorkingDirectory, ExecutableSHA256: executableSHA256, ExecutableAfterSHA256: executableAfterSHA256, ExitCode: -1, DurationMS: time.Since(started).Milliseconds(),
		StdoutSHA256: replayBytesSHA256(stdout.Bytes()), StderrSHA256: replayBytesSHA256(stderr.Bytes()), TimedOut: ctx.Err() == context.DeadlineExceeded,
	}
	if exitError, ok := err.(*exec.ExitError); ok {
		result.ExitCode = exitError.ExitCode()
	} else if err == nil {
		result.ExitCode = 0
	}
	result.Passed = err == nil && !result.TimedOut && hashErr == nil && afterHashErr == nil && executableSHA256 == executableAfterSHA256
	return result, stdout.Bytes(), stderr.Bytes()
}

func replayCommandLine(command ReplayCommand) []string {
	if len(command.Args) == 0 {
		return nil
	}
	return []string{command.Args[0]}
}

func commandSpecSHA256(command ReplayCommand) string {
	environment := append([]string(nil), command.Env...)
	sort.Strings(environment)
	data, _ := json.Marshal(struct {
		Operation             string   `json:"operation"`
		Arguments             []string `json:"arguments"`
		Environment           []string `json:"environment"`
		WorkingDirectory      string   `json:"workingDirectory"`
		TimeoutNS             int64    `json:"timeoutNs"`
		ExecutableSHA256      string   `json:"executableSha256"`
		ExecutableAfterSHA256 string   `json:"executableAfterSha256"`
	}{command.Args[0], command.Args, environment, command.WorkingDirectory, command.Timeout.Nanoseconds(), command.ExecutableSHA256, command.ExecutableAfterSHA256})
	return replayBytesSHA256(data)
}

func replayFileSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return replayBytesSHA256(data), nil
}

func replayBytesSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
