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
	"runtime"
	"sort"
	"time"
)

type ReplayCommand struct {
	Path    string
	Args    []string
	Env     []string
	Timeout time.Duration
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

const replayTimeout = 5 * time.Minute

var fixedReplayEnvironment = []string{"HOME=/var/empty", "PATH=/usr/bin:/bin", "TMPDIR=/tmp"}

type CommandResult struct {
	Command           []string `json:"command"`
	CommandSpecSHA256 string   `json:"commandSpecSha256"`
	ExitCode          int      `json:"exitCode"`
	DurationMS        int64    `json:"durationMs"`
	StdoutSHA256      string   `json:"stdoutSha256"`
	StderrSHA256      string   `json:"stderrSha256"`
	Passed            bool     `json:"passed"`
	TimedOut          bool     `json:"timedOut,omitempty"`
}

type ReplayRepositoryResult struct {
	RepositoryID        string         `json:"repositoryId"`
	SourceSHA256        string         `json:"sourceSha256"`
	CandidateSHA256     string         `json:"candidateSha256"`
	ToolsSHA256         string         `json:"toolsSha256"`
	CheckSpecSHA256     string         `json:"checkSpecSha256"`
	LocalTestSpecSHA256 string         `json:"localTestSpecSha256,omitempty"`
	Check               CommandResult  `json:"check"`
	LocalTest           *CommandResult `json:"localTest,omitempty"`
}

type ReplayShard struct {
	Status       string                   `json:"status"`
	Host         string                   `json:"host"`
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
	Candidate          RuntimeArtifact
	Tools              RuntimeArtifact
	Inventory          InventoryManifest
	RootManifestSHA256 string
	HostManifestSHA256 map[string]string
	Repositories       []RepositorySpec
}

// ValidateReplayFiles is the reconciliation entrypoint. It reads each sealed
// manifest and raw shard once, derives all hash bindings from those bytes, and
// rejects a postflight change before accepting the merge.
func ValidateReplayFiles(inventoryPath, rootManifestPath string, hostManifestPaths, shardPaths []string) error {
	inventory, inventoryBytes, err := readInventorySpec(inventoryPath)
	if err != nil {
		return fmt.Errorf("read IN_SCOPE inventory: %w", err)
	}
	root, rootBytes, err := readExactJSONBytes[InventoryManifest](rootManifestPath)
	if err != nil {
		return fmt.Errorf("read root manifest: %w", err)
	}
	if root.SchemaVersion != 1 || root.InventorySHA256 != replayBytesSHA256(inventoryBytes) || ValidateInventoryCoverage(inventory, root.Repositories) != nil {
		return fmt.Errorf("invalid root manifest")
	}
	if len(hostManifestPaths) == 0 || len(shardPaths) == 0 {
		return fmt.Errorf("host manifests and replay shards are required")
	}
	hostHashes := make(map[string]string, len(hostManifestPaths))
	hostFileHashes := make([]string, 0, len(hostManifestPaths))
	for _, path := range hostManifestPaths {
		host, data, err := readExactJSONBytes[HostManifest](path)
		if err != nil {
			return fmt.Errorf("read host manifest: %w", err)
		}
		if host.SchemaVersion != 1 || host.RootManifestSHA256 != replayBytesSHA256(rootBytes) || (host.Host != "local" && host.Host != "casper") {
			return fmt.Errorf("invalid host manifest")
		}
		expected := make(map[string]RepositorySpec)
		for _, repository := range root.Repositories {
			if repository.AssignedHost == host.Host {
				expected[repository.ID] = repository
			}
		}
		if len(host.Repositories) != len(expected) {
			return fmt.Errorf("host manifest repository count mismatch")
		}
		for _, repository := range host.Repositories {
			if expected[repository.ID] != repository {
				return fmt.Errorf("host manifest repository does not match root")
			}
			delete(expected, repository.ID)
		}
		if len(expected) != 0 {
			return fmt.Errorf("host manifest is missing root repositories")
		}
		if _, duplicate := hostHashes[host.Host]; duplicate {
			return fmt.Errorf("duplicate host manifest %q", host.Host)
		}
		hostHashes[host.Host] = replayBytesSHA256(data)
		hostFileHashes = append(hostFileHashes, replayBytesSHA256(data))
	}
	shards := make([]ReplayShard, 0, len(shardPaths))
	shardHashes := make([]string, 0, len(shardPaths))
	for _, path := range shardPaths {
		shard, data, err := readExactJSONBytes[ReplayShard](path)
		if err != nil {
			return fmt.Errorf("read replay shard: %w", err)
		}
		shards = append(shards, shard)
		shardHashes = append(shardHashes, replayBytesSHA256(data))
	}
	if len(shards) == 0 {
		return fmt.Errorf("replay shards are required")
	}
	merge := ReplayMerge{Candidate: shards[0].Candidate, Tools: shards[0].Tools, Inventory: root, RootManifestSHA256: replayBytesSHA256(rootBytes), HostManifestSHA256: hostHashes, Repositories: root.Repositories}
	if err := ValidateReplayMerge(merge, shards); err != nil {
		return err
	}
	_, postRoot, err := readExactJSONBytes[InventoryManifest](rootManifestPath)
	if err != nil || replayBytesSHA256(postRoot) != replayBytesSHA256(rootBytes) {
		return fmt.Errorf("root manifest changed during replay reconciliation")
	}
	_, postInventory, err := readInventorySpec(inventoryPath)
	if err != nil || replayBytesSHA256(postInventory) != replayBytesSHA256(inventoryBytes) {
		return fmt.Errorf("IN_SCOPE inventory changed during replay reconciliation")
	}
	for index, path := range hostManifestPaths {
		_, data, err := readExactJSONBytes[HostManifest](path)
		if err != nil || replayBytesSHA256(data) != hostFileHashes[index] {
			return fmt.Errorf("host manifest changed during replay reconciliation")
		}
	}
	for index, path := range shardPaths {
		_, data, err := readExactJSONBytes[ReplayShard](path)
		if err != nil || replayBytesSHA256(data) != shardHashes[index] {
			return fmt.Errorf("replay shard changed during reconciliation")
		}
	}
	return nil
}

func RunReplay(request ReplayRequest) (ReplayShard, error) {
	inputs, err := LoadSealedHostInputs(request.InventoryPath, request.RootManifestPath, request.HostManifestPath, request.Host)
	if err != nil {
		return ReplayShard{}, err
	}
	repositories, err := validateReplayRequest(request, inputs)
	if err != nil {
		return ReplayShard{}, err
	}
	if _, err := os.Lstat(request.OutputPath); err == nil {
		return ReplayShard{}, fmt.Errorf("replay output already exists: %s", request.OutputPath)
	} else if !os.IsNotExist(err) {
		return ReplayShard{}, err
	}

	shard := ReplayShard{Host: request.Host, OS: runtime.GOOS, Arch: runtime.GOARCH, Candidate: request.Candidate, Tools: request.Tools, Bindings: inputs.Bindings, Status: "pass"}
	for _, repository := range repositories {
		check := replayCommandFor(request.CandidatePath, "check")
		if err := validateReplayRuntimeBindings(request); err != nil {
			return ReplayShard{}, err
		}
		result := ReplayRepositoryResult{
			RepositoryID: repository.Repository.ID, SourceSHA256: repository.Repository.ArchiveSHA256,
			CandidateSHA256: request.Candidate.SHA256, ToolsSHA256: request.Tools.SHA256,
			CheckSpecSHA256: commandSpecSHA256(check),
			Check:           runReplayCommand(repository.SnapshotRoot, check),
		}
		if repository.Repository.LocalTests == "required" {
			localTestCommand := replayCommandFor(request.CandidatePath, "test")
			if err := validateReplayRuntimeBindings(request); err != nil {
				return ReplayShard{}, err
			}
			localTest := runReplayCommand(repository.SnapshotRoot, localTestCommand)
			result.LocalTest = &localTest
			result.LocalTestSpecSHA256 = commandSpecSHA256(localTestCommand)
		}
		if !validReplayReceipt(result.Check, "check", commandSpecSHA256(replayCommandFor(request.CandidatePath, "check"))) || (result.LocalTest != nil && !validReplayReceipt(*result.LocalTest, "test", commandSpecSHA256(replayCommandFor(request.CandidatePath, "test")))) {
			shard.Status = "fail"
		}
		shard.Repositories = append(shard.Repositories, result)
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
	expected := repositoryIndex(merge.Inventory.Repositories)
	seen := make(map[string]bool, len(expected))
	seenHosts := make(map[string]bool, len(merge.HostManifestSHA256))
	for _, shard := range shards {
		if shard.Candidate != merge.Candidate || shard.Tools != merge.Tools {
			return fmt.Errorf("artifact binding mismatch for host %q", shard.Host)
		}
		if shard.Status != "pass" || shard.OS != merge.Candidate.OS || shard.Arch != merge.Candidate.Arch {
			return fmt.Errorf("invalid replay shard state for host %q", shard.Host)
		}
		if shard.Bindings.InventorySHA256 != merge.Inventory.InventorySHA256 || shard.Bindings.RootManifestSHA256 != merge.RootManifestSHA256 || shard.Bindings.HostManifestSHA256 != merge.HostManifestSHA256[shard.Host] {
			return fmt.Errorf("manifest binding mismatch for host %q", shard.Host)
		}
		seenHosts[shard.Host] = true
		for _, result := range shard.Repositories {
			repository, exists := expected[result.RepositoryID]
			if !exists {
				return fmt.Errorf("unexpected repository %q", result.RepositoryID)
			}
			if seen[result.RepositoryID] {
				return fmt.Errorf("duplicate repository result %q", result.RepositoryID)
			}
			seen[result.RepositoryID] = true
			if repository.AssignedHost != shard.Host || result.SourceSHA256 != repository.ArchiveSHA256 || result.CandidateSHA256 != merge.Candidate.SHA256 || result.ToolsSHA256 != merge.Tools.SHA256 {
				return fmt.Errorf("repository binding mismatch for %q", result.RepositoryID)
			}
			if result.CheckSpecSHA256 != commandSpecSHA256(replayCommandFor("", "check")) || !validReplayReceipt(result.Check, "check", commandSpecSHA256(replayCommandFor("", "check"))) {
				return fmt.Errorf("check failed for %q", result.RepositoryID)
			}
			if repository.LocalTests == "required" && (result.LocalTest == nil || result.LocalTestSpecSHA256 != commandSpecSHA256(replayCommandFor("", "test")) || !validReplayReceipt(*result.LocalTest, "test", commandSpecSHA256(replayCommandFor("", "test")))) {
				return fmt.Errorf("required local test failed for %q", result.RepositoryID)
			}
		}
	}
	for id := range expected {
		if !seen[id] {
			return fmt.Errorf("missing repository result %q", id)
		}
	}
	for host := range merge.HostManifestSHA256 {
		if !seenHosts[host] {
			return fmt.Errorf("missing replay shard for host %q", host)
		}
	}
	return nil
}

func validateReplayDenominator(merge ReplayMerge) error {
	if merge.Inventory.SchemaVersion != 1 || !sha256Pattern.MatchString(merge.Inventory.InventorySHA256) || !sha256Pattern.MatchString(merge.RootManifestSHA256) || len(merge.Inventory.Repositories) == 0 || len(merge.HostManifestSHA256) == 0 {
		return fmt.Errorf("invalid replay manifest denominator")
	}
	for host, hash := range merge.HostManifestSHA256 {
		if (host != "local" && host != "casper") || !sha256Pattern.MatchString(hash) {
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
		if _, ok := merge.HostManifestSHA256[repository.AssignedHost]; !ok {
			return fmt.Errorf("missing host manifest binding for %q", repository.AssignedHost)
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

func validReplayReceipt(result CommandResult, operation, expectedSpecSHA256 string) bool {
	return result.Passed && !result.TimedOut && result.ExitCode == 0 && result.DurationMS >= 0 && len(result.Command) == 1 && result.Command[0] == operation && result.CommandSpecSHA256 == expectedSpecSHA256 && sha256Pattern.MatchString(expectedSpecSHA256) && sha256Pattern.MatchString(result.StdoutSHA256) && sha256Pattern.MatchString(result.StderrSHA256)
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

func replayCommandFor(candidatePath, operation string) ReplayCommand {
	return ReplayCommand{
		Path:    candidatePath,
		Args:    []string{operation, "--project", ".", "--json", "--no-progress"},
		Env:     append([]string(nil), fixedReplayEnvironment...),
		Timeout: replayTimeout,
	}
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
	result, _, _ := runReplayCommandOutput(workingDir, command)
	return result
}

func runReplayCommandOutput(workingDir string, command ReplayCommand) (CommandResult, []byte, []byte) {
	ctx, cancel := context.WithTimeout(context.Background(), command.Timeout)
	defer cancel()
	var stdout, stderr bytes.Buffer
	started := time.Now()
	cmd := exec.CommandContext(ctx, command.Path, command.Args...)
	cmd.Dir, cmd.Stdout, cmd.Stderr = workingDir, &stdout, &stderr
	cmd.Env = append([]string(nil), command.Env...)
	err := cmd.Run()
	result := CommandResult{
		Command: replayCommandLine(command), CommandSpecSHA256: commandSpecSHA256(command), ExitCode: -1, DurationMS: time.Since(started).Milliseconds(),
		StdoutSHA256: replayBytesSHA256(stdout.Bytes()), StderrSHA256: replayBytesSHA256(stderr.Bytes()), TimedOut: ctx.Err() == context.DeadlineExceeded,
	}
	if exitError, ok := err.(*exec.ExitError); ok {
		result.ExitCode = exitError.ExitCode()
	} else if err == nil {
		result.ExitCode = 0
	}
	result.Passed = err == nil && !result.TimedOut
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
		Operation   string   `json:"operation"`
		Arguments   []string `json:"arguments"`
		Environment []string `json:"environment"`
		TimeoutNS   int64    `json:"timeoutNs"`
	}{command.Args[0], command.Args, environment, command.Timeout.Nanoseconds()})
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
