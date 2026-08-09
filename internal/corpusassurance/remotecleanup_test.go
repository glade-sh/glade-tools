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

func TestRunRemoteAttemptCleanupRejectsNonAuthoritativeHost(t *testing.T) {
	root := t.TempDir()
	bindingPath := filepath.Join(root, "binding.json")
	if err := os.WriteFile(bindingPath, []byte(`{"binding":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"", "matt@casper.example", "root@casper.local", "matt@razor.local:22"} {
		t.Run(host, func(t *testing.T) {
			outputPath := filepath.Join(root, "REMOTE_CLEANUP.json")
			_, err := RunRemoteAttemptCleanup(RemoteAttemptCleanupRequest{
				Host:        host,
				Parent:      remoteCleanupParent,
				AttemptRoot: filepath.Join(remoteCleanupParent, "assurance-1afce500-test"),
				BindingPath: bindingPath,
				OutputPath:  outputPath,
				runner: func(context.Context, string, ...string) (salesforceCommandOutput, error) {
					t.Fatal("runner called for non-authoritative host")
					return salesforceCommandOutput{}, nil
				},
			})
			if err == nil || !strings.Contains(err.Error(), "non-authoritative remote cleanup host") {
				t.Fatalf("RunRemoteAttemptCleanup error = %v", err)
			}
		})
	}
}

func TestRunRemoteAttemptCleanupRejectsNonAuthoritativePath(t *testing.T) {
	root := t.TempDir()
	bindingPath := filepath.Join(root, "binding.json")
	if err := os.WriteFile(bindingPath, []byte(`{"binding":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	valid := RemoteAttemptCleanupRequest{
		Host:        "matt@casper.local",
		Parent:      remoteCleanupParent,
		AttemptRoot: filepath.Join(remoteCleanupParent, "assurance-1afce500-test"),
		BindingPath: bindingPath,
		OutputPath:  filepath.Join(root, "REMOTE_CLEANUP.json"),
	}
	cases := []struct {
		name   string
		mutate func(*RemoteAttemptCleanupRequest)
	}{
		{"parent mismatch", func(request *RemoteAttemptCleanupRequest) { request.Parent = "/private/tmp/other" }},
		{"relative binding", func(request *RemoteAttemptCleanupRequest) { request.BindingPath = "binding.json" }},
		{"relative output", func(request *RemoteAttemptCleanupRequest) { request.OutputPath = "REMOTE_CLEANUP.json" }},
		{"relative attempt", func(request *RemoteAttemptCleanupRequest) { request.AttemptRoot = "assurance-1afce500-test" }},
		{"nested attempt", func(request *RemoteAttemptCleanupRequest) {
			request.AttemptRoot = filepath.Join(remoteCleanupParent, "assurance-1afce500-test", "child")
		}},
		{"wrong basename prefix", func(request *RemoteAttemptCleanupRequest) {
			request.AttemptRoot = filepath.Join(remoteCleanupParent, "assurance-other-test")
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			request := valid
			testCase.mutate(&request)
			_, err := RunRemoteAttemptCleanup(RemoteAttemptCleanupRequest{
				Host:        request.Host,
				Parent:      request.Parent,
				AttemptRoot: request.AttemptRoot,
				BindingPath: request.BindingPath,
				OutputPath:  request.OutputPath,
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
		Host:        "matt@casper.local",
		Parent:      remoteCleanupParent,
		AttemptRoot: filepath.Join(remoteCleanupParent, "assurance-1afce500-test"),
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
	binding := []byte(`{"manifestSha256":"` + strings.Repeat("a", 64) + `"}`)
	if err := os.WriteFile(bindingPath, binding, 0o600); err != nil {
		t.Fatal(err)
	}
	basename := "assurance-1afce500-20260809T000000Z"
	outputPath := filepath.Join(root, "REMOTE_CLEANUP_CASPER.json")
	var calledBinary string
	var calledArgs []string
	cleanup, err := RunRemoteAttemptCleanup(RemoteAttemptCleanupRequest{
		Host:        "matt@casper.local",
		Parent:      remoteCleanupParent,
		AttemptRoot: filepath.Join(remoteCleanupParent, basename),
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
	wantCommand := remoteAttemptCleanupShellCommand(basename)
	if calledBinary != "ssh" || !reflect.DeepEqual(calledArgs, []string{"-o", "BatchMode=yes", "matt@casper.local", wantCommand}) {
		t.Fatalf("ssh invocation = %q %#v, want ssh -o BatchMode=yes with %q", calledBinary, calledArgs, wantCommand)
	}
	for _, fragment := range []string{
		"test -d '/private/tmp/glade-assurance-1afce500' && test ! -L '/private/tmp/glade-assurance-1afce500'",
		"test -d '/private/tmp/glade-assurance-1afce500/" + basename + "' && test ! -L '/private/tmp/glade-assurance-1afce500/" + basename + "'",
		"rm -r -- '/private/tmp/glade-assurance-1afce500/" + basename + "'",
		"test ! -e '/private/tmp/glade-assurance-1afce500/" + basename + "'",
	} {
		if !strings.Contains(wantCommand, fragment) {
			t.Fatalf("remote cleanup command missing %q: %q", fragment, wantCommand)
		}
	}
	if cleanup.SchemaVersion != 1 || cleanup.Host != "matt@casper.local" || cleanup.Parent != remoteCleanupParent || cleanup.AttemptRoot != filepath.Join(remoteCleanupParent, basename) {
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
	if err := os.WriteFile(bindingPath, []byte(`{"binding":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(root, "REMOTE_CLEANUP.json")
	_, err := RunRemoteAttemptCleanup(RemoteAttemptCleanupRequest{
		Host:        "matt@razor.local",
		Parent:      remoteCleanupParent,
		AttemptRoot: filepath.Join(remoteCleanupParent, "assurance-1afce500-test"),
		BindingPath: bindingPath,
		OutputPath:  outputPath,
		runner: func(_ context.Context, _ string, _ ...string) (salesforceCommandOutput, error) {
			if err := os.WriteFile(bindingPath, []byte(`{"binding":false}`), 0o600); err != nil {
				t.Fatal(err)
			}
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
	if err := os.WriteFile(bindingPath, []byte(`{"binding":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(root, "REMOTE_CLEANUP.json")
	_, err := RunRemoteAttemptCleanup(RemoteAttemptCleanupRequest{
		Host:        "matt@casper.local",
		Parent:      remoteCleanupParent,
		AttemptRoot: filepath.Join(remoteCleanupParent, "assurance-1afce500-test"),
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
	basename := "assurance-1afce500-casper's $(rm -rf /) attempt"
	command := remoteAttemptCleanupShellCommand(basename)
	wantAttempt := "'/private/tmp/glade-assurance-1afce500/assurance-1afce500-casper'\\''s $(rm -rf /) attempt'"
	if !strings.Contains(command, "rm -r -- "+wantAttempt) {
		t.Fatalf("command does not safely quote basename: %q", command)
	}
}
