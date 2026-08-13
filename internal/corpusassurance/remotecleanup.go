package corpusassurance

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const remoteCleanupTimeout = 30 * time.Second
const remoteCleanupBinary = "/usr/bin/ssh"

var safeRemoteSSHHost = regexp.MustCompile(`^[A-Za-z0-9._-]+@[A-Za-z0-9._-]+$`)

type RemoteAttemptCleanupRequest struct {
	AttemptPath string
	BindingPath string
	OutputPath  string
	runner      salesforceCommandRunner
}

// RemoteAttemptAuthority is an independently sealed private operator input for
// one host and one remote attempt root. Cleanup refuses caller-selected flags.
type RemoteAttemptAuthority struct {
	SchemaVersion int    `json:"schemaVersion"`
	AttemptSHA256 string `json:"attemptSha256"`
	Role          string `json:"role"`
	Host          string `json:"host"`
	Parent        string `json:"parent"`
	AttemptRoot   string `json:"attemptRoot"`
}

// RemoteAttemptCleanupReceipt seals the one exact ssh command, its execution
// result, and the unchanged binding hash for a completed remote attempt.
type RemoteAttemptCleanupReceipt struct {
	SchemaVersion     int           `json:"schemaVersion"`
	AttemptSHA256     string        `json:"attemptSha256"`
	Role              string        `json:"role"`
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
	if err := validateRemoteAttemptCleanupRequest(request); err != nil {
		return RemoteAttemptCleanupReceipt{}, err
	}
	if _, err := os.Lstat(request.OutputPath); err == nil {
		return RemoteAttemptCleanupReceipt{}, fmt.Errorf("remote cleanup output already exists: %s", request.OutputPath)
	} else if !os.IsNotExist(err) {
		return RemoteAttemptCleanupReceipt{}, err
	}
	authority, bindingBytes, err := readRemoteAttemptAuthority(request.BindingPath)
	if err != nil {
		return RemoteAttemptCleanupReceipt{}, err
	}
	attempt, attemptBytes, err := readExactJSONBytes[AssuranceAttempt](request.AttemptPath)
	bindingSHA := replayBytesSHA256(bindingBytes)
	if err != nil || ValidateAssuranceAttempt(attempt) != nil || !remoteCleanupAuthorityMatches(attempt, authority, bindingSHA) {
		return RemoteAttemptCleanupReceipt{}, fmt.Errorf("remote cleanup authority is not bound to the sealed attempt")
	}

	command := remoteAttemptCleanupShellCommand(authority.Parent, filepath.Base(authority.AttemptRoot))
	runner := request.runner
	if runner == nil {
		runner = runSalesforceCLI
	}
	ctx, cancel := context.WithTimeout(context.Background(), remoteCleanupTimeout)
	defer cancel()
	started := time.Now()
	executableSHA, err := sha256File(remoteCleanupBinary)
	if err != nil {
		return RemoteAttemptCleanupReceipt{}, fmt.Errorf("hash remote cleanup executable: %w", err)
	}
	args := []string{remoteCleanupBinary, "-o", "BatchMode=yes", authority.Host, command}
	output, runErr := runner(ctx, args[0], args[1:]...)
	executableAfterSHA, afterErr := sha256File(remoteCleanupBinary)
	receipt := CommandResult{
		Command:               append([]string(nil), args...),
		CommandSpecSHA256:     commandSpecSHA256(ReplayCommand{Path: remoteCleanupBinary, Args: args, Timeout: remoteCleanupTimeout}),
		ExecutableSHA256:      executableSHA,
		ExecutableAfterSHA256: executableAfterSHA,
		ExitCode:              output.ExitCode,
		DurationMS:            time.Since(started).Milliseconds(),
		StdoutSHA256:          replayBytesSHA256(output.Stdout),
		StderrSHA256:          replayBytesSHA256(output.Stderr),
		Output:                retainedCommandOutput(output),
		Passed:                runErr == nil && output.ExitCode == 0,
		TimedOut:              ctx.Err() == context.DeadlineExceeded,
	}
	if runErr != nil || afterErr != nil || receipt.TimedOut || !receipt.Passed || executableSHA != executableAfterSHA {
		return RemoteAttemptCleanupReceipt{}, fmt.Errorf("remote cleanup command failed")
	}

	postAuthority, postBindingBytes, err := readRemoteAttemptAuthority(request.BindingPath)
	if err != nil {
		return RemoteAttemptCleanupReceipt{}, err
	}
	postBindingSHA := replayBytesSHA256(postBindingBytes)
	postAttempt, postAttemptBytes, err := readExactJSONBytes[AssuranceAttempt](request.AttemptPath)
	if postAuthority != authority || postBindingSHA != bindingSHA || ValidateAssuranceAttempt(postAttempt) != nil || attemptBindingHash(postAttempt) != attemptBindingHash(attempt) || replayBytesSHA256(postAttemptBytes) != replayBytesSHA256(attemptBytes) {
		return RemoteAttemptCleanupReceipt{}, fmt.Errorf("remote cleanup binding changed during execution")
	}

	cleanup := RemoteAttemptCleanupReceipt{
		SchemaVersion:     1,
		AttemptSHA256:     attemptBindingHash(attempt),
		Role:              authority.Role,
		Host:              authority.Host,
		Parent:            authority.Parent,
		AttemptRoot:       authority.AttemptRoot,
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

func validateRemoteAttemptCleanupRequest(request RemoteAttemptCleanupRequest) error {
	if !filepath.IsAbs(request.AttemptPath) || !filepath.IsAbs(request.BindingPath) || !filepath.IsAbs(request.OutputPath) {
		return fmt.Errorf("absolute remote cleanup paths are required")
	}
	return nil
}

func validateRemoteAttemptTarget(attemptSHA, role, host, parent, attemptRoot string) error {
	if role != "replay-worker" && role != "salesforce-worker" || !safeRemoteSSHHost.MatchString(host) || !filepath.IsAbs(parent) || !filepath.IsAbs(attemptRoot) {
		return fmt.Errorf("invalid remote cleanup authority target")
	}
	if !sha256Pattern.MatchString(attemptSHA) {
		return fmt.Errorf("invalid remote cleanup authority attempt hash")
	}
	if !validRemoteAttemptParent(parent) || strings.IndexFunc(parent, func(r rune) bool { return r == '\n' || r == '\r' || r == '\t' || r == ' ' }) >= 0 || strings.IndexFunc(attemptRoot, func(r rune) bool { return r == '\n' || r == '\r' || r == '\t' || r == ' ' }) >= 0 || filepath.Dir(filepath.Clean(attemptRoot)) != filepath.Clean(parent) {
		return fmt.Errorf("attempt root must be a direct child of parent")
	}
	if basename := filepath.Base(filepath.Clean(attemptRoot)); !strings.HasPrefix(basename, "assurance-"+attemptSHA[:16]+"-") || strings.ContainsRune(basename, '/') {
		return fmt.Errorf("invalid attempt basename")
	}
	return nil
}

func validRemoteAttemptParent(parent string) bool {
	clean := filepath.Clean(parent)
	return filepath.IsAbs(clean) && strings.HasPrefix(filepath.Base(clean), "glade-assurance-")
}

func remoteAttemptCleanupShellCommand(parentPath, basename string) string {
	parent := shellQuote(parentPath)
	attempt := shellQuote("./" + basename)
	return "set -e\n" +
		"test -d " + parent + " && test ! -L " + parent + " && cd " + parent + "\n" +
		"test -d " + attempt + " && test ! -L " + attempt + "\n" +
		"rm -r -- " + attempt + "\n" +
		"test ! -e " + attempt + " && test ! -L " + attempt
}

func remoteAttemptAbsenceShellCommand(parentPath, basename string) string {
	parent := shellQuote(parentPath)
	attempt := shellQuote("./" + basename)
	return "set -e\n" +
		"test -d " + parent + " && test ! -L " + parent + " && cd " + parent + "\n" +
		"test ! -e " + attempt + " && test ! -L " + attempt
}

func verifyRemoteAttemptAbsent(authority RemoteAttemptAuthority, runner salesforceCommandRunner) error {
	if runner == nil {
		runner = runSalesforceCLI
	}
	ctx, cancel := context.WithTimeout(context.Background(), remoteCleanupTimeout)
	defer cancel()
	args := []string{remoteCleanupBinary, "-o", "BatchMode=yes", authority.Host, remoteAttemptAbsenceShellCommand(authority.Parent, filepath.Base(authority.AttemptRoot))}
	output, err := runner(ctx, args[0], args[1:]...)
	if err != nil || ctx.Err() != nil || output.ExitCode != 0 {
		return fmt.Errorf("remote cleanup residue check failed")
	}
	return nil
}

func readRemoteAttemptAuthority(path string) (RemoteAttemptAuthority, []byte, error) {
	authority, data, err := readExactJSONBytes[RemoteAttemptAuthority](path)
	if err != nil || authority.SchemaVersion != 1 || !sha256Pattern.MatchString(authority.AttemptSHA256) || validateRemoteAttemptTarget(authority.AttemptSHA256, authority.Role, authority.Host, authority.Parent, authority.AttemptRoot) != nil {
		return RemoteAttemptAuthority{}, nil, fmt.Errorf("invalid remote cleanup authority")
	}
	return authority, data, nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
