package corpusassurance

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const remoteCleanupParent = "/private/tmp/glade-assurance-worker"

func TestRunRemoteAttemptCleanupRejectsAnArbitraryBindingFile(t *testing.T) {
	root := t.TempDir()
	bindingPath := filepath.Join(root, "binding.json")
	if err := os.WriteFile(bindingPath, []byte(`{"binding":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := RunRemoteAttemptCleanup(RemoteAttemptCleanupRequest{
		AttemptPath: bindingPath + ".attempt", BindingPath: bindingPath, OutputPath: filepath.Join(root, "REMOTE_CLEANUP.json"),
		runner: func(context.Context, string, ...string) (salesforceCommandOutput, error) {
			t.Fatal("runner called for arbitrary binding")
			return salesforceCommandOutput{}, nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "invalid remote cleanup authority") {
		t.Fatalf("RunRemoteAttemptCleanup error = %v", err)
	}
}

func TestRunRemoteAttemptCleanupAcceptsAuthorityBoundWorker(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "glade-assurance-worker")
	attempt := filepath.Join(parent, "assurance-1234")
	bindingPath := filepath.Join(root, "binding.json")
	writeRemoteCleanupAuthorityAt(t, bindingPath, "salesforce-worker", "operator@salesforce-worker", parent, attempt)
	_, err := RunRemoteAttemptCleanup(RemoteAttemptCleanupRequest{AttemptPath: bindingPath + ".attempt", BindingPath: bindingPath, OutputPath: filepath.Join(root, "REMOTE_CLEANUP.json"), runner: func(_ context.Context, binary string, args ...string) (salesforceCommandOutput, error) {
		if binary != "/usr/bin/ssh" || len(args) != 4 || args[2] != "operator@salesforce-worker" {
			t.Fatalf("cleanup invocation = %q %q", binary, args)
		}
		return salesforceCommandOutput{}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRunRemoteAttemptCleanupRejectsAuthorityBoundAttemptWithoutAuthorityMap(t *testing.T) {
	root := t.TempDir()
	bindingPath := filepath.Join(root, "binding.json")
	requestedRoot := filepath.Join(remoteCleanupParent, "worker-without-attempt-map")
	writeRemoteCleanupAuthority(t, bindingPath, "operator@replay-worker", requestedRoot)
	attemptPath := bindingPath + ".attempt"
	attempt, _, err := readExactJSONBytes[AssuranceAttempt](attemptPath)
	if err != nil {
		t.Fatal(err)
	}
	attempt.RemoteCleanupAuthoritySHA256 = nil
	attemptBytes, err := json.Marshal(attempt)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(attemptPath, append(attemptBytes, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := RunRemoteAttemptCleanup(RemoteAttemptCleanupRequest{
		AttemptPath: attemptPath,
		BindingPath: bindingPath,
		OutputPath:  filepath.Join(root, "REMOTE_CLEANUP.json"),
		runner: func(context.Context, string, ...string) (salesforceCommandOutput, error) {
			return salesforceCommandOutput{}, nil
		},
	}); err == nil {
		t.Fatal("RunRemoteAttemptCleanup accepted an attempt without pre-bound cleanup authorities")
	}
}

func TestRunRemoteAttemptCleanupRejectsBroadParent(t *testing.T) {
	root := t.TempDir()
	bindingPath := filepath.Join(root, "binding.json")
	if err := os.WriteFile(bindingPath, []byte(`{"schemaVersion":1,"role":"replay-worker","host":"operator@replay-worker","parent":"/","attemptRoot":"/assurance-1234"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := RunRemoteAttemptCleanup(RemoteAttemptCleanupRequest{AttemptPath: bindingPath + ".attempt", BindingPath: bindingPath, OutputPath: filepath.Join(root, "REMOTE_CLEANUP.json"), runner: func(context.Context, string, ...string) (salesforceCommandOutput, error) {
		t.Fatal("runner called for broad parent")
		return salesforceCommandOutput{}, nil
	}})
	if err == nil {
		t.Fatal("accepted broad remote cleanup parent")
	}
}

func TestRunRemoteAttemptCleanupRejectsNonAuthoritativePath(t *testing.T) {
	root := t.TempDir()
	bindingPath := filepath.Join(root, "binding.json")
	if err := os.WriteFile(bindingPath, []byte(`{"binding":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	valid := RemoteAttemptCleanupRequest{AttemptPath: bindingPath + ".attempt", BindingPath: bindingPath, OutputPath: filepath.Join(root, "REMOTE_CLEANUP.json")}
	cases := []struct {
		name   string
		mutate func(*RemoteAttemptCleanupRequest)
	}{
		{"relative binding", func(request *RemoteAttemptCleanupRequest) { request.BindingPath = "binding.json" }},
		{"relative output", func(request *RemoteAttemptCleanupRequest) { request.OutputPath = "REMOTE_CLEANUP.json" }},
		{"relative attempt", func(request *RemoteAttemptCleanupRequest) { request.AttemptPath = "ATTEMPT.json" }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			request := valid
			testCase.mutate(&request)
			_, err := RunRemoteAttemptCleanup(RemoteAttemptCleanupRequest{
				AttemptPath: request.AttemptPath, BindingPath: request.BindingPath, OutputPath: request.OutputPath,
				runner: func(context.Context, string, ...string) (salesforceCommandOutput, error) {
					t.Fatal("runner called for non-authoritative path")
					return salesforceCommandOutput{}, nil
				},
			})
			if err == nil {
				t.Fatalf("RunRemoteAttemptCleanup accepted non-authoritative path %#v", request)
			}
			if _, statErr := os.Lstat(request.OutputPath); !os.IsNotExist(statErr) {
				t.Fatalf("output created for rejected request: %v", statErr)
			}
		})
	}
}

func TestRunRemoteAttemptCleanupDoesNotClobberExistingOutput(t *testing.T) {
	root := t.TempDir()
	bindingPath := filepath.Join(root, "binding.json")
	if err := os.WriteFile(bindingPath, []byte(`{"binding":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(root, "REMOTE_CLEANUP.json")
	if err := os.WriteFile(outputPath, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := RunRemoteAttemptCleanup(RemoteAttemptCleanupRequest{
		AttemptPath: bindingPath + ".attempt",
		BindingPath: bindingPath,
		OutputPath:  outputPath,
		runner: func(context.Context, string, ...string) (salesforceCommandOutput, error) {
			t.Fatal("runner called when output already exists")
			return salesforceCommandOutput{}, nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("RunRemoteAttemptCleanup error = %v", err)
	}
	if data, readErr := os.ReadFile(outputPath); readErr != nil || string(data) != "existing" {
		t.Fatalf("existing output was clobbered: %q, %v", data, readErr)
	}
}

func TestRunRemoteAttemptCleanupSuccessCommandShape(t *testing.T) {
	root := t.TempDir()
	bindingPath := filepath.Join(root, "binding.json")
	requestedRoot := filepath.Join(remoteCleanupParent, "worker-20260809T000000Z")
	writeRemoteCleanupAuthority(t, bindingPath, "operator@replay-worker", requestedRoot)
	attemptRoot := remoteCleanupAttemptRoot(t, bindingPath+".attempt", remoteCleanupParent, requestedRoot)
	basename := filepath.Base(attemptRoot)
	outputPath := filepath.Join(root, "REMOTE_CLEANUP_REPLAY_WORKER.json")
	var calledBinary string
	var calledArgs []string
	cleanup, err := RunRemoteAttemptCleanup(RemoteAttemptCleanupRequest{
		AttemptPath: bindingPath + ".attempt",
		BindingPath: bindingPath,
		OutputPath:  outputPath,
		runner: func(_ context.Context, binary string, args ...string) (salesforceCommandOutput, error) {
			calledBinary = binary
			calledArgs = append([]string(nil), args...)
			return salesforceCommandOutput{Stdout: []byte("removed\n")}, nil
		},
	})
	if err != nil {
		t.Fatalf("RunRemoteAttemptCleanup: %v", err)
	}
	wantCommand := remoteAttemptCleanupShellCommand(remoteCleanupParent, basename)
	if calledBinary != "/usr/bin/ssh" || !reflect.DeepEqual(calledArgs, []string{"-o", "BatchMode=yes", "operator@replay-worker", wantCommand}) {
		t.Fatalf("ssh invocation = %q %#v, want /usr/bin/ssh -o BatchMode=yes with %q", calledBinary, calledArgs, wantCommand)
	}
	for _, fragment := range []string{
		"test -d '" + remoteCleanupParent + "' && test ! -L '" + remoteCleanupParent + "' && cd '" + remoteCleanupParent + "'",
		"test -d './" + basename + "' && test ! -L './" + basename + "'",
		"rm -r -- './" + basename + "'",
		"test ! -e './" + basename + "' && test ! -L './" + basename + "'",
	} {
		if !strings.Contains(wantCommand, fragment) {
			t.Fatalf("remote cleanup command missing %q: %q", fragment, wantCommand)
		}
	}
	if cleanup.SchemaVersion != 1 || cleanup.Host != "operator@replay-worker" || cleanup.Parent != remoteCleanupParent || cleanup.AttemptRoot != filepath.Join(remoteCleanupParent, basename) {
		t.Fatalf("cleanup identity = %#v", cleanup)
	}
	if cleanup.BindingSHA256 != cleanup.BindingPostSHA256 || cleanup.BindingSHA256 != sha256FileForTest(t, bindingPath) {
		t.Fatalf("binding hashes = before %q post %q", cleanup.BindingSHA256, cleanup.BindingPostSHA256)
	}
	if cleanup.TimeoutMS != remoteCleanupTimeout.Milliseconds() || !cleanup.Command.Passed || cleanup.Command.TimedOut || cleanup.Command.CommandSpecSHA256 == "" || !cleanup.ResidueAbsent {
		t.Fatalf("cleanup receipt = %#v", cleanup)
	}
	data, readErr := os.ReadFile(outputPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	var sealed RemoteAttemptCleanupReceipt
	if json.Unmarshal(data, &sealed) != nil || !reflect.DeepEqual(sealed, cleanup) {
		t.Fatalf("sealed receipt = %#v, want %#v", sealed, cleanup)
	}
}

func TestRunRemoteAttemptCleanupRejectsPostBindingMutation(t *testing.T) {
	root := t.TempDir()
	bindingPath := filepath.Join(root, "binding.json")
	requestedRoot := filepath.Join(remoteCleanupParent, "test")
	writeRemoteCleanupAuthority(t, bindingPath, "operator@salesforce-worker", requestedRoot)
	attemptRoot := remoteCleanupAttemptRoot(t, bindingPath+".attempt", remoteCleanupParent, requestedRoot)
	outputPath := filepath.Join(root, "REMOTE_CLEANUP.json")
	_, err := RunRemoteAttemptCleanup(RemoteAttemptCleanupRequest{
		AttemptPath: bindingPath + ".attempt",
		BindingPath: bindingPath,
		OutputPath:  outputPath,
		runner: func(_ context.Context, _ string, _ ...string) (salesforceCommandOutput, error) {
			if err := os.Remove(bindingPath); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(bindingPath + ".attempt"); err != nil {
				t.Fatal(err)
			}
			writeRemoteCleanupAuthority(t, bindingPath, "operator@replay-worker", attemptRoot)
			return salesforceCommandOutput{ExitCode: 0}, nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "binding changed") {
		t.Fatalf("RunRemoteAttemptCleanup error = %v", err)
	}
	if _, statErr := os.Lstat(outputPath); !os.IsNotExist(statErr) {
		t.Fatalf("receipt written after binding mutation: %v", statErr)
	}
}

func TestRunRemoteAttemptCleanupRejectsFailedSSH(t *testing.T) {
	root := t.TempDir()
	bindingPath := filepath.Join(root, "binding.json")
	writeRemoteCleanupAuthority(t, bindingPath, "operator@replay-worker", filepath.Join(remoteCleanupParent, "test"))
	outputPath := filepath.Join(root, "REMOTE_CLEANUP.json")
	_, err := RunRemoteAttemptCleanup(RemoteAttemptCleanupRequest{
		AttemptPath: bindingPath + ".attempt",
		BindingPath: bindingPath,
		OutputPath:  outputPath,
		runner: func(context.Context, string, ...string) (salesforceCommandOutput, error) {
			return salesforceCommandOutput{Stderr: []byte("Permission denied"), ExitCode: 255}, nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "command failed") {
		t.Fatalf("RunRemoteAttemptCleanup error = %v", err)
	}
	if _, statErr := os.Lstat(outputPath); !os.IsNotExist(statErr) {
		t.Fatalf("receipt written after failed ssh: %v", statErr)
	}
}

func TestRemoteAttemptCleanupShellCommandQuotesBasename(t *testing.T) {
	basename := "assurance-1afce500-worker's $(rm -rf /) attempt"
	command := remoteAttemptCleanupShellCommand(remoteCleanupParent, basename)
	wantAttempt := "'./assurance-1afce500-worker'\\''s $(rm -rf /) attempt'"
	if !strings.Contains(command, "rm -r -- "+wantAttempt) {
		t.Fatalf("command does not safely quote basename: %q", command)
	}
}

func writeRemoteCleanupAuthority(t *testing.T, path, host, attemptRoot string) {
	t.Helper()
	writeRemoteCleanupAuthorityAt(t, path, "replay-worker", host, remoteCleanupParent, attemptRoot)
}

func writeRemoteCleanupAuthorityAt(t *testing.T, path, role, host, parent, attemptRoot string) {
	t.Helper()
	attempt := AssuranceAttempt{SchemaVersion: 1, InventorySHA256: strings.Repeat("a", 64), CandidateAuthoritySHA256: strings.Repeat("b", 64), Candidate: RuntimeArtifact{Commit: strings.Repeat("c", 40), OS: "darwin", Arch: "arm64", SHA256: strings.Repeat("d", 64)}, Tools: RuntimeArtifact{Commit: strings.Repeat("e", 40), OS: "darwin", Arch: "amd64", SHA256: strings.Repeat("f", 64)}}
	attemptBytes, err := json.Marshal(attempt)
	if err != nil {
		t.Fatal(err)
	}
	bindingSHA := replayBytesSHA256(attemptBytes)
	attemptRoot = filepath.Join(parent, "assurance-"+bindingSHA[:16]+"-"+filepath.Base(attemptRoot))
	authority := RemoteAttemptAuthority{SchemaVersion: 1, AttemptSHA256: bindingSHA, Role: role, Host: host, Parent: filepath.Clean(parent), AttemptRoot: filepath.Clean(attemptRoot)}
	if err := WriteNewJSON(path, authority); err != nil {
		t.Fatal(err)
	}
	authorityBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	attempt.RemoteCleanupAuthoritySHA256 = map[string]string{"replay-worker": strings.Repeat("0", 64), "salesforce-worker": strings.Repeat("0", 64)}
	attempt.RemoteCleanupAuthoritySHA256[role] = replayBytesSHA256(authorityBytes)
	data, err := json.Marshal(attempt)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".attempt", append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func remoteCleanupAttemptRoot(t *testing.T, attemptPath, parent, requestedRoot string) string {
	t.Helper()
	attempt, _, err := readExactJSONBytes[AssuranceAttempt](attemptPath)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(parent, "assurance-"+attemptBindingHash(attempt)[:16]+"-"+filepath.Base(requestedRoot))
}
