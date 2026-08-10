package corpusassurance

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/glade-sh/glade/internal/sema"
	"github.com/glade-sh/glade/internal/semanticcache"
)

func TestReplayRunsExactArgumentsAndWritesBoundReceipt(t *testing.T) {
	request, capture := replayRequest(t, "local", "0")
	sealedRoot := filepath.Join(filepath.Dir(request.HostManifestPath), "snapshots", "private-corpus-001")
	expectedTreeSHA256, err := canonicalTreeSHA256(sealedRoot)
	if err != nil {
		t.Fatal(err)
	}
	shard, err := RunReplay(request)
	if err != nil {
		t.Fatalf("RunReplay: %v", err)
	}
	if shard.Status != "pass" || len(shard.Repositories) != 1 {
		t.Fatalf("shard = %#v", shard)
	}
	if got, want := readLines(t, capture), []string{"check", "--project", ".", "--json", "--no-progress"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("argv = %q, want %q", got, want)
	}
	if got, want := readLines(t, capture+".test"), []string{"test", "--project", ".", "--json", "--no-progress", "--perf-json", replayTestPerfPath}; !reflect.DeepEqual(got, want) {
		t.Fatalf("test argv = %q, want %q", got, want)
	}
	workspaces := readLines(t, capture+".working")
	if len(workspaces) != 2 || workspaces[0] != workspaces[1] {
		t.Fatalf("replay workspaces = %q", workspaces)
	}
	for _, workspace := range workspaces {
		if workspace == sealedRoot {
			t.Fatal("replay executed in the sealed snapshot")
		}
		if _, err := os.Stat(workspace); !os.IsNotExist(err) {
			t.Fatalf("replay workspace remains after cleanup: %s (%v)", workspace, err)
		}
	}
	result := shard.Repositories[0]
	if result.SourceSHA256 == "" || result.CandidateSHA256 != request.Candidate.SHA256 || result.ToolsSHA256 != request.Tools.SHA256 {
		t.Fatalf("bindings = %#v", result)
	}
	if got, want := result.Check.Command, []string{"check"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("recorded command = %q, want %q", got, want)
	}
	if result.Check.ExitCode != 0 || result.Check.DurationMS < 0 || result.Check.StdoutSHA256 != stringSHA256("check stdout") || result.Check.StderrSHA256 != stringSHA256("check stderr") {
		t.Fatalf("check = %#v", result.Check)
	}
	if result.Check.Output == nil || string(result.Check.Output.Stdout) != "check stdout" || string(result.Check.Output.Stderr) != "check stderr" {
		t.Fatalf("check output = %#v", result.Check.Output)
	}
	if result.LocalTest == nil || !result.LocalTest.Passed {
		t.Fatalf("local test = %#v", result.LocalTest)
	}
	if !strings.HasPrefix(result.SemanticCache.Path, replaySemanticCachePath+"/result-") || result.SemanticCache.DiskHits == 0 || result.SemanticCache.SHA256 == "" || result.SemanticCache.PerfSHA256 == "" {
		t.Fatalf("semantic cache proof = %#v", result.SemanticCache)
	}
	if got, err := canonicalTreeSHA256(sealedRoot); err != nil || got != expectedTreeSHA256 {
		t.Fatalf("sealed snapshot changed after replay: %q, %v", got, err)
	}
	if _, err := os.Stat(request.OutputPath); err != nil {
		t.Fatalf("receipt was not written: %v", err)
	}
}

func TestReplayRejectsCleanupFailure(t *testing.T) {
	request, _ := replayRequest(t, "local", "0")
	original := removeReplayWorkspace
	removeReplayWorkspace = func(path string) error {
		if err := os.RemoveAll(path); err != nil {
			return err
		}
		return errors.New("forced cleanup failure")
	}
	t.Cleanup(func() { removeReplayWorkspace = original })
	if _, err := RunReplay(request); err == nil {
		t.Fatal("RunReplay accepted a cleanup failure")
	}
	if _, err := os.Stat(request.OutputPath); !os.IsNotExist(err) {
		t.Fatalf("cleanup failure wrote receipt: %v", err)
	}
}

func TestReplayRecordsNonzero(t *testing.T) {
	request, _ := replayRequest(t, "local", "7")
	shard, err := RunReplay(request)
	if err != nil {
		t.Fatalf("RunReplay: %v", err)
	}
	result := shard.Repositories[0]
	if shard.Status != "fail" || result.Check.Passed || result.Check.ExitCode != 7 {
		t.Fatalf("shard = %#v", shard)
	}
}

func TestReplayRejectsMissingTestsSourceTamperingAndWrongArchitecture(t *testing.T) {
	for name, mutate := range map[string]func(*ReplayRequest){
		"candidate tampering": func(request *ReplayRequest) {
			if err := os.WriteFile(request.CandidatePath, []byte("changed candidate"), 0o700); err != nil {
				t.Fatal(err)
			}
		},
		"wrong tools executable": func(request *ReplayRequest) {
			path := filepath.Join(filepath.Dir(request.OutputPath), "other-tools")
			if err := os.WriteFile(path, []byte("changed tools"), 0o700); err != nil {
				t.Fatal(err)
			}
			request.ToolsPath = path
		},
		"source tampering": func(request *ReplayRequest) {
			if err := os.WriteFile(filepath.Join(filepath.Dir(request.RootManifestPath), "archives", "private-corpus-001.tar"), []byte("changed"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"wrong architecture": func(request *ReplayRequest) {
			request.architecture = func(string) (string, error) { return otherArch(), nil }
		},
		"missing manifest binding": func(request *ReplayRequest) { request.RootManifestPath = "" },
		"unsealed snapshot": func(request *ReplayRequest) {
			if err := os.WriteFile(filepath.Join(filepath.Dir(request.HostManifestPath), "snapshots", "private-corpus-001", "Source.cls"), []byte("changed"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			request, capture := replayRequest(t, "local", "0")
			mutate(&request)
			if _, err := RunReplay(request); err == nil {
				t.Fatal("RunReplay accepted invalid preflight")
			}
			if _, err := os.Stat(request.OutputPath); !os.IsNotExist(err) {
				t.Fatalf("invalid preflight wrote receipt: %v", err)
			}
			if _, err := os.Stat(capture); !os.IsNotExist(err) {
				t.Fatalf("invalid preflight ran a repository command: %v", err)
			}
		})
	}
}

func TestReplayDefaultArchitectureProbeRejectsScript(t *testing.T) {
	request, _ := replayRequest(t, "local", "0")
	request.architecture = nil
	if _, err := RunReplay(request); err == nil {
		t.Fatal("shell script was accepted as a candidate runtime")
	}
}

func TestValidateReplayMergeRejectsEmptyOrNonPassingReceipts(t *testing.T) {
	merge, _ := validReplayMerge()
	merge.Repositories = nil
	if err := ValidateReplayMerge(merge, nil); err == nil {
		t.Fatal("empty replay merge was accepted")
	}

	merge, shards := validReplayMerge()
	shards[0].Status = "fail"
	if err := ValidateReplayMerge(merge, shards); err == nil {
		t.Fatal("failed shard was accepted")
	}
	merge, shards = validReplayMerge()
	shards[0].Repositories[0].Check = CommandResult{Passed: true, ExitCode: 1}
	if err := ValidateReplayMerge(merge, shards); err == nil {
		t.Fatal("nonzero check receipt was accepted")
	}
	merge, shards = validReplayMerge()
	merge.RootManifestSHA256 = ""
	if err := ValidateReplayMerge(merge, shards); err == nil {
		t.Fatal("merge without root manifest binding was accepted")
	}
	merge, shards = validReplayMerge()
	shards[0].Bindings.HostManifestSHA256 = strings.Repeat("f", 64)
	if err := ValidateReplayMerge(merge, shards); err == nil {
		t.Fatal("shard with wrong host manifest binding was accepted")
	}
	merge, shards = validReplayMerge()
	shards[0].Repositories[0].Check.CommandSpecSHA256 = ""
	if err := ValidateReplayMerge(merge, shards); err == nil {
		t.Fatal("tampered command specification was accepted")
	}
	merge, shards = validReplayMerge()
	shards[0].Repositories[0].SemanticCache.DiskHits = 0
	if err := ValidateReplayMerge(merge, shards); err == nil {
		t.Fatal("required local test without a disk cache proof was accepted")
	}
	merge, shards = validReplayMerge()
	shards[0].Repositories[0].Check.CommandSpecSHA256 = strings.Repeat("e", 64)
	if err := ValidateReplayMerge(merge, shards); err == nil {
		t.Fatal("well-formed but wrong command specification was accepted")
	}
	merge, shards = validReplayMerge()
	forged := commandSpecSHA256(ReplayCommand{Args: []string{"check", "--project", "/unsealed", "--json", "--no-progress"}, Timeout: replayTimeout})
	shards[0].Repositories[0].CheckSpecSHA256 = forged
	shards[0].Repositories[0].Check.CommandSpecSHA256 = forged
	if err := ValidateReplayMerge(merge, shards); err == nil {
		t.Fatal("paired forged command hashes were accepted")
	}
}

func TestReplayDoesNotClobberExistingOutput(t *testing.T) {
	request, capture := replayRequest(t, "local", "0")
	if err := os.WriteFile(request.OutputPath, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := RunReplay(request); err == nil {
		t.Fatal("RunReplay overwrote an existing output")
	}
	if got, err := os.ReadFile(request.OutputPath); err != nil || string(got) != "keep" {
		t.Fatalf("output = %q, %v", got, err)
	}
	if _, err := os.Stat(capture); !os.IsNotExist(err) {
		t.Fatalf("check ran before no-clobber preflight: %v", err)
	}
}

func TestValidateReplayMergeRejectsInvalidShards(t *testing.T) {
	merge, shards := validReplayMerge()
	if err := ValidateReplayMerge(merge, shards); err != nil {
		t.Fatalf("ValidateReplayMerge(valid): %v", err)
	}
	for name, mutate := range map[string]func(*ReplayMerge, *[]ReplayShard){
		"missing repository": func(_ *ReplayMerge, shards *[]ReplayShard) { *shards = (*shards)[:1] },
		"duplicate across hosts": func(_ *ReplayMerge, shards *[]ReplayShard) {
			duplicate := (*shards)[0]
			duplicate.Host = "replay-worker"
			*shards = append(*shards, duplicate)
		},
		"unexpected repository": func(_ *ReplayMerge, shards *[]ReplayShard) {
			(*shards)[0].Repositories[0].RepositoryID = "private-corpus-999"
		},
		"binding mismatch": func(_ *ReplayMerge, shards *[]ReplayShard) { (*shards)[0].Candidate.SHA256 = strings.Repeat("f", 64) },
		"execution tree mismatch": func(_ *ReplayMerge, shards *[]ReplayShard) {
			(*shards)[0].Repositories[0].ExecutionTreeSHA256 = strings.Repeat("f", 64)
		},
		"workspace mismatch": func(_ *ReplayMerge, shards *[]ReplayShard) {
			(*shards)[0].Repositories[0].Check.WorkingDirectory = "sealed-snapshot"
		},
		"missing retained output": func(_ *ReplayMerge, shards *[]ReplayShard) {
			(*shards)[0].Repositories[0].Check.Output = nil
		},
		"forged retained output": func(_ *ReplayMerge, shards *[]ReplayShard) {
			(*shards)[0].Repositories[0].Check.Output.Stdout = []byte("forged")
		},
		"no-tests repository marked test ready": func(merge *ReplayMerge, _ *[]ReplayShard) {
			merge.Repositories[1].LocalTests, merge.Repositories[1].LocalTestsReason = "tests-not-present", "no Apex test classes found in snapshot"
			merge.Inventory.Repositories[1] = merge.Repositories[1]
			merge.TestReadyByRepository["private-corpus-002"] = true
		},
		"failed required test": func(_ *ReplayMerge, shards *[]ReplayShard) { (*shards)[0].Repositories[0].LocalTest.Passed = false },
	} {
		t.Run(name, func(t *testing.T) {
			gotMerge, gotShards := validReplayMerge()
			mutate(&gotMerge, &gotShards)
			if err := ValidateReplayMerge(gotMerge, gotShards); err == nil {
				t.Fatal("ValidateReplayMerge accepted invalid shards")
			}
		})
	}
}

func TestValidateReplayMergeAcceptsCompleteClassShardPartition(t *testing.T) {
	merge, shards := completeClassShardMerge()
	if err := ValidateReplayMerge(merge, shards); err != nil {
		t.Fatalf("ValidateReplayMerge(complete class shard partition): %v", err)
	}
}

func TestValidateReplayMergeRejectsInvalidClassShardPartitions(t *testing.T) {
	for name, mutate := range map[string]func(*ReplayMerge, *[]ReplayShard){
		"missing": func(_ *ReplayMerge, shards *[]ReplayShard) { (*shards)[1].Repositories = (*shards)[1].Repositories[:1] },
		"duplicate": func(_ *ReplayMerge, shards *[]ReplayShard) {
			(*shards)[0].Repositories = append((*shards)[0].Repositories, (*shards)[0].Repositories[0])
		},
		"wrong host":         func(_ *ReplayMerge, shards *[]ReplayShard) { (*shards)[0].Repositories[0].TestShardIndex = 1 },
		"inconsistent count": func(_ *ReplayMerge, shards *[]ReplayShard) { (*shards)[1].Repositories[1].TestShardCount = 3 },
		"timed out":          func(_ *ReplayMerge, shards *[]ReplayShard) { (*shards)[1].Repositories[1].LocalTest.TimedOut = true },
	} {
		t.Run(name, func(t *testing.T) {
			merge, shards := completeClassShardMerge()
			mutate(&merge, &shards)
			if err := ValidateReplayMerge(merge, shards); err == nil {
				t.Fatal("ValidateReplayMerge accepted an invalid class shard partition")
			}
		})
	}
}

func TestValidateReplayMergeRejectsUnmanifestedPartitionHost(t *testing.T) {
	merge, shards := completeClassShardMerge()
	merge.Repositories = merge.Repositories[:1]
	merge.Inventory.Repositories = merge.Inventory.Repositories[:1]
	delete(merge.TestReadyByRepository, "private-corpus-002")
	shards[1].Repositories = shards[1].Repositories[1:]
	delete(merge.HostManifestSHA256, "replay-worker")
	shards[1].Bindings.HostManifestSHA256 = ""

	if err := ValidateReplayMerge(merge, shards); err == nil {
		t.Fatal("ValidateReplayMerge accepted an unmanifested partition host")
	}
}

func completeClassShardMerge() (ReplayMerge, []ReplayShard) {
	merge, shards := validReplayMerge()
	merge.Repositories[0].TestShardCount = 2
	merge.Inventory.Repositories[0].TestShardCount = 2
	shards[0].Repositories[0].TestShardCount = 2
	shards[0].Repositories[0].TestShardIndex = 0
	shards[0].Repositories[0].LocalTestSpecSHA256 = replayCommandSpecSHA256("test", merge.Candidate.SHA256, 2, 0)
	shards[0].Repositories[0].LocalTest.CommandSpecSHA256 = shards[0].Repositories[0].LocalTestSpecSHA256
	second := shards[0].Repositories[0]
	second.TestShardIndex = 1
	second.LocalTest = successfulReceiptPointer("test")
	second.LocalTestSpecSHA256 = replayCommandSpecSHA256("test", merge.Candidate.SHA256, 2, 1)
	second.LocalTest.CommandSpecSHA256 = second.LocalTestSpecSHA256
	shards[1].Repositories = append(shards[1].Repositories, second)
	return merge, shards
}

func TestValidateReplayRootBindingRejectsTamperedDenominators(t *testing.T) {
	for name, mutate := range map[string]func(*ReplayMerge){
		"duplicate repository":       func(merge *ReplayMerge) { merge.Repositories[1] = merge.Repositories[0] },
		"altered embedded inventory": func(merge *ReplayMerge) { merge.Inventory.Repositories[1].ExpectedCommit = strings.Repeat("f", 40) },
	} {
		t.Run(name, func(t *testing.T) {
			merge, _ := validReplayMerge()
			root := merge.Inventory
			mutate(&merge)
			if err := validateReplayRootBinding(merge, root); err == nil {
				t.Fatal("validateReplayRootBinding accepted a tampered replay denominator")
			}
		})
	}
}

func TestValidateReplayRootBindingRejectsAttemptArtifactSwap(t *testing.T) {
	merge, _ := validReplayMerge()
	merge.Inventory.Attempt = AssuranceAttempt{SchemaVersion: 1, InventorySHA256: merge.Inventory.InventorySHA256, CandidateAuthoritySHA256: strings.Repeat("a", 64), Candidate: replayRuntime("f"), Tools: merge.Tools, RemoteCleanupAuthoritySHA256: testCleanupAuthorityHashes()}
	if err := validateReplayRootBinding(merge, merge.Inventory); err == nil {
		t.Fatal("validateReplayRootBinding accepted a receipt candidate that differs from the sealed attempt")
	}
}

func TestValidateReplayMergeAcceptsRetainedEmptyStreams(t *testing.T) {
	merge, shards := validReplayMerge()
	shards[0].Repositories[0].Check.Output.Stderr = []byte{}
	shards[0].Repositories[0].Check.StderrSHA256 = replayBytesSHA256([]byte{})
	if err := ValidateReplayMerge(merge, shards); err != nil {
		t.Fatalf("ValidateReplayMerge rejected an empty retained stream: %v", err)
	}
}

func TestRunReplayCommandRetainsEmptyStreams(t *testing.T) {
	result := runReplayCommand(t.TempDir(), ReplayCommand{Path: "/bin/sh", Args: []string{"-c", "printf stdout"}, Env: append([]string(nil), fixedReplayEnvironment...), Timeout: replayTimeout})
	if result.Output == nil || result.Output.Stdout == nil || result.Output.Stderr == nil {
		t.Fatalf("empty replay stream was not retained: %#v", result.Output)
	}
	if string(result.Output.Stdout) != "stdout" || len(result.Output.Stderr) != 0 || !validRetainedCommandOutput(result) {
		t.Fatalf("retained output = %#v", result.Output)
	}
}

func TestReplayTimeoutCoversLargePrivateCorpus(t *testing.T) {
	if replayTimeout < 15*time.Minute {
		t.Fatalf("replay timeout = %s, want at least 15m for large private corpus", replayTimeout)
	}
}

func TestValidateReplayFilesLoadsAuthoritativeInputs(t *testing.T) {
	merge, shards := validReplayMerge()
	root := t.TempDir()
	inventoryPath := filepath.Join(root, "IN_SCOPE.json")
	inventoryEntries := make([]InventoryEntry, 0, len(merge.Inventory.Repositories))
	for _, repository := range merge.Inventory.Repositories {
		inventoryEntries = append(inventoryEntries, InventoryEntry{ID: repository.ID, CheckoutPath: filepath.Join(root, "checkouts", repository.ID), ExpectedCommit: repository.ExpectedCommit})
	}
	if err := WriteNewJSON(inventoryPath, InventorySpec{SchemaVersion: 1, Scope: "private-corpus-assurance", Repositories: inventoryEntries}); err != nil {
		t.Fatal(err)
	}
	merge.Inventory.InventorySHA256 = fileSHA256(t, inventoryPath)
	merge.Inventory.Attempt = AssuranceAttempt{SchemaVersion: 1, InventorySHA256: merge.Inventory.InventorySHA256, CandidateAuthoritySHA256: strings.Repeat("a", 64), Candidate: merge.Candidate, Tools: merge.Tools, RemoteCleanupAuthoritySHA256: testCleanupAuthorityHashes()}
	for index := range shards {
		shards[index].Bindings.InventorySHA256 = merge.Inventory.InventorySHA256
	}
	rootPath := filepath.Join(root, "MANIFEST.json")
	if err := WriteNewJSON(rootPath, merge.Inventory); err != nil {
		t.Fatal(err)
	}
	rootSHA := fileSHA256(t, rootPath)
	hostPaths := make([]string, 0, 2)
	for _, host := range []string{"local", "replay-worker"} {
		path := filepath.Join(root, host+".json")
		repositories := []RepositorySpec{}
		for _, repository := range merge.Inventory.Repositories {
			if repository.AssignedHost == host {
				repositories = append(repositories, repository)
			}
		}
		if err := WriteNewJSON(path, HostManifest{SchemaVersion: 1, Host: host, RootManifestSHA256: rootSHA, Repositories: repositories}); err != nil {
			t.Fatal(err)
		}
		hostPaths = append(hostPaths, path)
		for index := range shards {
			if shards[index].Host == host {
				shards[index].Bindings.RootManifestSHA256 = rootSHA
				shards[index].Bindings.HostManifestSHA256 = fileSHA256(t, path)
			}
		}
	}
	shardPaths := make([]string, 0, len(shards))
	for index, shard := range shards {
		path := filepath.Join(root, "shard-"+string(rune('0'+index))+".json")
		if err := WriteNewJSON(path, shard); err != nil {
			t.Fatal(err)
		}
		shardPaths = append(shardPaths, path)
	}
	if err := ValidateReplayFiles(inventoryPath, rootPath, hostPaths, shardPaths); err != nil {
		t.Fatalf("ValidateReplayFiles: %v", err)
	}
	outputPath := filepath.Join(root, "REPLAY.json")
	merged, err := MergeReplayFromFiles(inventoryPath, rootPath, hostPaths, shardPaths, outputPath)
	if err != nil {
		t.Fatalf("MergeReplayFromFiles: %v", err)
	}
	if len(merged.Repositories) != 2 || !merged.TestReadyByRepository["private-corpus-001"] || !merged.TestReadyByRepository["private-corpus-002"] || fileSHA256(t, outputPath) == "" {
		t.Fatalf("merged replay = %#v", merged)
	}
	request := AssuranceReportRequest{InventoryPath: inventoryPath, RootManifestPath: rootPath, ReplayHostManifestPaths: hostPaths, ReplayShardPaths: shardPaths}
	if _, err := validateReportReplayEvidence(request, merged); err != nil {
		t.Fatalf("validateReportReplayEvidence(valid): %v", err)
	}
	data, err := os.ReadFile(shardPaths[0])
	if err != nil {
		t.Fatal(err)
	}
	var tampered ReplayShard
	if err := json.Unmarshal(data, &tampered); err != nil {
		t.Fatal(err)
	}
	tampered.Repositories[0].Check.Output = nil
	data, err = json.Marshal(tampered)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shardPaths[0], data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := validateReportReplayEvidence(request, merged); err == nil {
		t.Fatal("validateReportReplayEvidence accepted a replay shard with removed retained output")
	}
}

func replayRequest(t *testing.T, host, exit string) (ReplayRequest, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "archives"), 0o700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "archives", "private-corpus-001.tar")
	if err := os.WriteFile(source, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(root, "argv.txt")
	cacheTemplateRoot := filepath.Join(root, "cache-template")
	if err := os.MkdirAll(cacheTemplateRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	cacheIdentity := semanticcache.Identity{
		ProjectContentSHA256: strings.Repeat("a", 64), SchemaContentSHA256: strings.Repeat("b", 64), DependencySHA256: strings.Repeat("c", 64),
		SemanticABI: sema.SemanticABI, PlatformABI: sema.PlatformABI, OptionsFingerprint: strings.Repeat("d", 64),
	}
	if err := semanticcache.Store(cacheTemplateRoot, filepath.Join(replaySemanticCachePath, "result-proof.json"), cacheIdentity, sema.Result{}); err != nil {
		t.Fatal(err)
	}
	cacheTemplate := filepath.Join(cacheTemplateRoot, replaySemanticCachePath, "result-proof.json")
	candidate, candidatePath := stagedRuntime(t, root, "candidate", replayRuntime("c"))
	script := "#!/bin/sh\nif [ \"$1\" = check ]; then printf '%s\\n' \"$@\" > '" + capture + "'; pwd > '" + capture + ".working'; mkdir -p .glade/semantic; cp '" + cacheTemplate + "' .glade/semantic/result-proof.json; fi\nif [ \"$1\" = test ]; then printf '%s\\n' \"$@\" > '" + capture + ".test'; pwd >> '" + capture + ".working'; printf '{\"apexPerf\":{\"phases\":{\"semanticDiskCacheHits\":1}}}' > .glade/assurance-test-perf.json; fi\nprintf '%s stdout' \"$1\"\nprintf '%s stderr' \"$1\" >&2\nif [ \"$1\" = check ]; then exit " + exit + "; fi\nexit 0\n"
	if err := os.WriteFile(candidatePath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	candidate.SHA256 = fileSHA256(t, candidatePath)
	toolsPath, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	tools := replayRuntime("d")
	tools.SHA256 = fileSHA256(t, toolsPath)
	snapshotRoot := filepath.Join(root, "hosts", host, "snapshots", "private-corpus-001")
	if err := os.MkdirAll(snapshotRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(snapshotRoot, "Source.cls"), []byte("public class Source {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	treeSHA256, err := canonicalTreeSHA256(snapshotRoot)
	if err != nil {
		t.Fatal(err)
	}
	repo := RepositorySpec{
		ID: "private-corpus-001", ExpectedCommit: strings.Repeat("a", 40), ArchiveSHA256: fileSHA256(t, source), TreeSHA256: treeSHA256, AssignedHost: host, SnapshotPath: "snapshots/private-corpus-001", LocalTests: "required",
	}
	inventoryPath, rootManifestPath, hostManifestPath := writeReplayManifests(t, root, repo, AssuranceAttempt{SchemaVersion: 1, InventorySHA256: strings.Repeat("a", 64), CandidateAuthoritySHA256: strings.Repeat("a", 64), Candidate: candidate, Tools: tools, RemoteCleanupAuthoritySHA256: testCleanupAuthorityHashes()})
	return ReplayRequest{
		Host: host, Candidate: candidate, CandidatePath: candidatePath, Tools: tools, ToolsPath: toolsPath, OutputPath: filepath.Join(root, "shard.json"), InventoryPath: inventoryPath, RootManifestPath: rootManifestPath, HostManifestPath: hostManifestPath, architecture: func(string) (string, error) { return runtime.GOARCH, nil },
	}, capture
}

func writeReplayManifests(t *testing.T, root string, repository RepositorySpec, attempt AssuranceAttempt) (string, string, string) {
	t.Helper()
	inventoryPath := filepath.Join(root, "IN_SCOPE.json")
	inventory := InventorySpec{SchemaVersion: 1, Scope: "private-corpus-assurance", Repositories: []InventoryEntry{{ID: repository.ID, CheckoutPath: filepath.Join(root, "checkout"), ExpectedCommit: repository.ExpectedCommit}}}
	if err := WriteNewJSON(inventoryPath, inventory); err != nil {
		t.Fatal(err)
	}
	rootPath := filepath.Join(root, "MANIFEST.json")
	attempt.InventorySHA256 = fileSHA256(t, inventoryPath)
	if err := WriteNewJSON(rootPath, InventoryManifest{SchemaVersion: 1, InventorySHA256: fileSHA256(t, inventoryPath), Attempt: attempt, Repositories: []RepositorySpec{repository}}); err != nil {
		t.Fatal(err)
	}
	hostPath := filepath.Join(root, "hosts", repository.AssignedHost, "manifest.json")
	if err := os.MkdirAll(filepath.Dir(hostPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := WriteNewJSON(hostPath, HostManifest{SchemaVersion: 1, Host: repository.AssignedHost, RootManifestSHA256: fileSHA256(t, rootPath), Repositories: []RepositorySpec{repository}}); err != nil {
		t.Fatal(err)
	}
	return inventoryPath, rootPath, hostPath
}

func stagedRuntime(t *testing.T, root, name string, artifact RuntimeArtifact) (RuntimeArtifact, string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte(name), 0o700); err != nil {
		t.Fatal(err)
	}
	artifact.SHA256 = fileSHA256(t, path)
	return artifact, path
}

func validReplayMerge() (ReplayMerge, []ReplayShard) {
	first := RepositorySpec{ID: "private-corpus-001", ExpectedCommit: strings.Repeat("a", 40), ArchiveSHA256: strings.Repeat("b", 64), TreeSHA256: strings.Repeat("c", 64), AssignedHost: "local", SnapshotPath: "snapshots/private-corpus-001", LocalTests: "required"}
	second := first
	second.ID, second.AssignedHost, second.SnapshotPath = "private-corpus-002", "replay-worker", "snapshots/private-corpus-002"
	merge := ReplayMerge{Candidate: replayRuntime("d"), Tools: replayRuntime("e"), Repositories: []RepositorySpec{first, second}, Inventory: InventoryManifest{SchemaVersion: 1, InventorySHA256: strings.Repeat("a", 64), Repositories: []RepositorySpec{first, second}}, RootManifestSHA256: strings.Repeat("b", 64), HostManifestSHA256: map[string]string{"local": strings.Repeat("c", 64), "replay-worker": strings.Repeat("d", 64)}, TestReadyByRepository: map[string]bool{first.ID: true, second.ID: true}}
	shards := []ReplayShard{
		{Status: "pass", Host: "local", OS: merge.Candidate.OS, Arch: merge.Candidate.Arch, Candidate: merge.Candidate, Tools: merge.Tools, Bindings: ReplayBindings{InventorySHA256: merge.Inventory.InventorySHA256, RootManifestSHA256: merge.RootManifestSHA256, HostManifestSHA256: merge.HostManifestSHA256["local"]}, Repositories: []ReplayRepositoryResult{{RepositoryID: first.ID, SourceSHA256: first.ArchiveSHA256, ExecutionTreeSHA256: first.TreeSHA256, CandidateSHA256: merge.Candidate.SHA256, ToolsSHA256: merge.Tools.SHA256, CheckSpecSHA256: successfulReceipt("check").CommandSpecSHA256, LocalTestSpecSHA256: successfulReceipt("test").CommandSpecSHA256, SemanticCache: successfulSemanticCacheEvidence(), Check: successfulReceipt("check"), LocalTest: successfulReceiptPointer("test")}}},
		{Status: "pass", Host: "replay-worker", OS: merge.Candidate.OS, Arch: merge.Candidate.Arch, Candidate: merge.Candidate, Tools: merge.Tools, Bindings: ReplayBindings{InventorySHA256: merge.Inventory.InventorySHA256, RootManifestSHA256: merge.RootManifestSHA256, HostManifestSHA256: merge.HostManifestSHA256["replay-worker"]}, Repositories: []ReplayRepositoryResult{{RepositoryID: second.ID, SourceSHA256: second.ArchiveSHA256, ExecutionTreeSHA256: second.TreeSHA256, CandidateSHA256: merge.Candidate.SHA256, ToolsSHA256: merge.Tools.SHA256, CheckSpecSHA256: successfulReceipt("check").CommandSpecSHA256, LocalTestSpecSHA256: successfulReceipt("test").CommandSpecSHA256, SemanticCache: successfulSemanticCacheEvidence(), Check: successfulReceipt("check"), LocalTest: successfulReceiptPointer("test")}}},
	}
	return merge, shards
}

func successfulReceipt(operation string) CommandResult {
	output := &RetainedCommandOutput{Stdout: []byte("stdout"), Stderr: []byte("stderr")}
	executableSHA256 := strings.Repeat("d", 64)
	return CommandResult{Command: []string{operation}, ExecutableSHA256: executableSHA256, ExecutableAfterSHA256: executableSHA256, CommandSpecSHA256: replayCommandSpecSHA256(operation, executableSHA256), WorkingDirectory: replayWorkspaceIdentity, ExitCode: 0, DurationMS: 1, StdoutSHA256: replayBytesSHA256(output.Stdout), StderrSHA256: replayBytesSHA256(output.Stderr), Output: output, Passed: true}
}

func successfulReceiptPointer(operation string) *CommandResult {
	result := successfulReceipt(operation)
	return &result
}

func successfulSemanticCacheEvidence() ReplaySemanticCacheEvidence {
	return ReplaySemanticCacheEvidence{Path: replaySemanticCachePath + "/result-" + strings.Repeat("a", 64) + ".json", SHA256: strings.Repeat("b", 64), IdentitySHA256: strings.Repeat("c", 64), PerfSHA256: strings.Repeat("d", 64), DiskHits: 1}
}

func replayRuntime(hash string) RuntimeArtifact {
	return RuntimeArtifact{Commit: strings.Repeat(hash, 40), OS: runtime.GOOS, Arch: runtime.GOARCH, SHA256: strings.Repeat(hash, 64)}
}

func otherArch() string {
	if runtime.GOARCH == "arm64" {
		return "amd64"
	}
	return "arm64"
}

func fileSHA256(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return stringSHA256(string(data))
}

func stringSHA256(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Fields(string(data))
}
