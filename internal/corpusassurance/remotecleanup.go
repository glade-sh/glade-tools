package corpusassurance

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	remoteCleanupParent  = "/private/tmp/glade-assurance-1afce500"
	remoteCleanupPrefix  = "assurance-1afce500-"
	remoteCleanupTimeout = 30 * time.Second
)

type RemoteAttemptCleanupRequest struct {
	Host        string
	Parent      string
	AttemptRoot string
	BindingPath string
	OutputPath  string
	runner      salesforceCommandRunner
}

// RemoteAttemptCleanupReceipt seals the one exact ssh command, its execution
// result, and the unchanged binding hash for a completed remote attempt.
type RemoteAttemptCleanupReceipt struct {
	SchemaVersion     int           `json:"schemaVersion"`
	Host              string        `json:"host"`
	Parent            string        `json:"parent"`
	AttemptRoot       string        `json:"attemptRoot"`
	BindingSHA256     string        `json:"bindingSha256"`
	BindingPostSHA256 string        `json:"bindingPostSha256"`
	Command           CommandResult `json:"command"`
	TimeoutMS         int64         `json:"timeoutMs"`
	ResidueAbsent     bool          `json:"residueAbsent"`
}

// RunRemoteAttemptCleanup removes exactly one completed remote assurance
// attempt below the fixed parent on an authoritative host. The create-only
// receipt is written only after the fixed ssh command succeeds, the attempt is
// absent, and the binding file has the same single-read SHA before and after.
func RunRemoteAttemptCleanup(request RemoteAttemptCleanupRequest) (RemoteAttemptCleanupReceipt, error) {
	attemptRoot := filepath.Clean(request.AttemptRoot)
	if err := validateRemoteAttemptCleanupRequest(request, attemptRoot); err != nil {
		return RemoteAttemptCleanupReceipt{}, err
	}
	if _, err := os.Lstat(request.OutputPath); err == nil {
		return RemoteAttemptCleanupReceipt{}, fmt.Errorf("remote cleanup output already exists: %s", request.OutputPath)
	} else if !os.IsNotExist(err) {
		return RemoteAttemptCleanupReceipt{}, err
	}
	bindingBytes, err := os.ReadFile(request.BindingPath)
	if err != nil {
		return RemoteAttemptCleanupReceipt{}, fmt.Errorf("read remote cleanup binding: %w", err)
	}
	bindingSHA := replayBytesSHA256(bindingBytes)

	command := remoteAttemptCleanupShellCommand(filepath.Base(attemptRoot))
	runner := request.runner
	if runner == nil {
		runner = runSalesforceCLI
	}
	ctx, cancel := context.WithTimeout(context.Background(), remoteCleanupTimeout)
	defer cancel()
	started := time.Now()
	args := []string{"ssh", "-o", "BatchMode=yes", request.Host, command}
	output, runErr := runner(ctx, args[0], args[1:]...)
	receipt := CommandResult{
		Command:           append([]string(nil), args...),
		CommandSpecSHA256: commandSpecSHA256(ReplayCommand{Path: "ssh", Args: args, Timeout: remoteCleanupTimeout}),
		ExitCode:          output.ExitCode,
		DurationMS:        time.Since(started).Milliseconds(),
		StdoutSHA256:      replayBytesSHA256(output.Stdout),
		StderrSHA256:      replayBytesSHA256(output.Stderr),
		Passed:            runErr == nil && output.ExitCode == 0,
		TimedOut:          ctx.Err() == context.DeadlineExceeded,
	}
	if runErr != nil || receipt.TimedOut || !receipt.Passed {
		return RemoteAttemptCleanupReceipt{}, fmt.Errorf("remote cleanup command failed")
	}

	postBindingBytes, err := os.ReadFile(request.BindingPath)
	if err != nil {
		return RemoteAttemptCleanupReceipt{}, fmt.Errorf("re-read remote cleanup binding: %w", err)
	}
	postBindingSHA := replayBytesSHA256(postBindingBytes)
	if postBindingSHA != bindingSHA {
		return RemoteAttemptCleanupReceipt{}, fmt.Errorf("remote cleanup binding changed during execution")
	}

	cleanup := RemoteAttemptCleanupReceipt{
		SchemaVersion:     1,
		Host:              request.Host,
		Parent:            remoteCleanupParent,
		AttemptRoot:       attemptRoot,
		BindingSHA256:     bindingSHA,
		BindingPostSHA256: postBindingSHA,
		Command:           receipt,
		TimeoutMS:         remoteCleanupTimeout.Milliseconds(),
		ResidueAbsent:     true,
	}
	if err := WriteNewJSON(request.OutputPath, cleanup); err != nil {
		return RemoteAttemptCleanupReceipt{}, err
	}
	return cleanup, nil
}

func validateRemoteAttemptCleanupRequest(request RemoteAttemptCleanupRequest, attemptRoot string) error {
	if request.Host != "matt@casper.local" && request.Host != "matt@razor.local" {
		return fmt.Errorf("non-authoritative remote cleanup host %q", request.Host)
	}
	if request.Parent != remoteCleanupParent {
		return fmt.Errorf("remote cleanup parent must be %s", remoteCleanupParent)
	}
	if !filepath.IsAbs(attemptRoot) || !filepath.IsAbs(request.BindingPath) || !filepath.IsAbs(request.OutputPath) {
		return fmt.Errorf("absolute remote cleanup paths are required")
	}
	if filepath.Dir(attemptRoot) != remoteCleanupParent {
		return fmt.Errorf("attempt root %s must be a direct child of %s", attemptRoot, remoteCleanupParent)
	}
	basename := filepath.Base(attemptRoot)
	if !strings.HasPrefix(basename, remoteCleanupPrefix) || strings.ContainsRune(basename, '/') {
		return fmt.Errorf("attempt basename must start with %q and contain no slash", remoteCleanupPrefix)
	}
	return nil
}

func remoteAttemptCleanupShellCommand(basename string) string {
	parent := shellQuote(remoteCleanupParent)
	attempt := shellQuote(remoteCleanupParent + "/" + basename)
	return "set -e\n" +
		"test -d " + parent + " && test ! -L " + parent + "\n" +
		"test -d " + attempt + " && test ! -L " + attempt + "\n" +
		"rm -r -- " + attempt + "\n" +
		"test ! -e " + attempt
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
