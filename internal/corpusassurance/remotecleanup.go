package corpusassurance

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
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

type RemoteFailurePreserveRequest struct {
	AttemptPath string
	BindingPath string
	Phase       string
	PhaseExit   int
	HandoffPath string
	OutputPath  string
	runner      remoteFailureCopyRunner
}

type RemoteFailureCopyCommand struct {
	Command string `json:"command"`
	Passed  bool   `json:"passed"`
	Output  string `json:"output,omitempty"`
}

type RemoteFailurePreservation struct {
	SchemaVersion      int                      `json:"schemaVersion"`
	Status             string                   `json:"status"`
	Phase              string                   `json:"phase"`
	PhaseExit          int                      `json:"phaseExit"`
	AttemptSHA256      string                   `json:"attemptSha256"`
	AuthoritySHA256    string                   `json:"authoritySha256"`
	Host               string                   `json:"host"`
	RemoteRoot         string                   `json:"remoteRoot"`
	Copy               RemoteFailureCopyCommand `json:"copy"`
	Checksum           RemoteFailureCopyCommand `json:"checksum"`
	TreeManifestSHA256 string                   `json:"treeManifestSha256,omitempty"`
}

type remoteFailureCopyRunner func(context.Context, string, string, string, bool) (salesforceCommandOutput, error)

const remoteFailureCopyBinary = "/usr/bin/rsync"

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

// PreserveRemoteFailure retains a failed remote phase before any reviewed
// cleanup. It never deletes or invokes Salesforce commands on the remote host.
func PreserveRemoteFailure(request RemoteFailurePreserveRequest) (RemoteFailurePreservation, error) {
	if err := validateRemoteFailureRequest(request); err != nil {
		return RemoteFailurePreservation{}, err
	}
	if _, err := os.Lstat(request.OutputPath); err == nil {
		return RemoteFailurePreservation{}, fmt.Errorf("remote failure output already exists: %s", request.OutputPath)
	} else if !os.IsNotExist(err) {
		return RemoteFailurePreservation{}, err
	}
	if err := os.Mkdir(request.OutputPath, 0o700); err != nil {
		return RemoteFailurePreservation{}, err
	}
	attempt, _, err := readExactJSONBytes[AssuranceAttempt](request.AttemptPath)
	if err != nil || ValidateAssuranceAttempt(attempt) != nil {
		return RemoteFailurePreservation{}, fmt.Errorf("invalid sealed attempt")
	}
	authority, authorityBytes, err := readRemoteAttemptAuthority(request.BindingPath)
	if err != nil || !remoteCleanupAuthorityMatches(attempt, authority, replayBytesSHA256(authorityBytes)) {
		return RemoteFailurePreservation{}, fmt.Errorf("remote failure authority is not bound to the sealed attempt")
	}
	if err := validateRemoteAttemptTarget(authority.AttemptSHA256, authority.Role, authority.Host, authority.Parent, authority.AttemptRoot); err != nil {
		return RemoteFailurePreservation{}, err
	}
	remoteRoot := filepath.Join(request.OutputPath, "remote-root")
	if err := os.Mkdir(remoteRoot, 0o700); err != nil {
		return RemoteFailurePreservation{}, err
	}
	source := authority.Host + ":" + authority.AttemptRoot + "/"
	copyCommand := "rsync -a " + source + " " + remoteRoot + "/"
	checksumCommand := "rsync -a --checksum --dry-run " + source + " " + remoteRoot + "/"
	runner := request.runner
	if runner == nil {
		runner = runRemoteFailureCopy
	}
	ctx, cancel := context.WithTimeout(context.Background(), remoteCleanupTimeout)
	defer cancel()
	copyOutput, copyErr := runner(ctx, source, remoteRoot, copyCommand, false)
	copy := RemoteFailureCopyCommand{Command: copyCommand, Passed: copyErr == nil && copyOutput.ExitCode == 0, Output: remoteFailureOutputText(copyOutput)}
	checksum := RemoteFailureCopyCommand{Command: checksumCommand}
	if copy.Passed {
		checksumOutput, checksumErr := runner(ctx, source, remoteRoot, checksumCommand, true)
		checksum.Passed = checksumErr == nil && checksumOutput.ExitCode == 0 && strings.TrimSpace(remoteFailureOutputText(checksumOutput)) == ""
		checksum.Output = remoteFailureOutputText(checksumOutput)
	}
	status := "preservation-failed"
	var treeSHA string
	if copy.Passed && checksum.Passed {
		status = "preserved"
		if err := writeModeBearingTreeManifest(remoteRoot); err != nil {
			status = "preservation-failed"
		} else {
			treeSHA, _ = sha256File(filepath.Join(remoteRoot, "TREE_MANIFEST.json"))
		}
	}
	next := fmt.Sprintf("Recover remote root %s before deletion; inspect %s and start a successor attempt. Do not reuse this attempt.", authority.AttemptRoot, filepath.Join(request.OutputPath, "REMOTE_FAILURE.json"))
	if err := writeNewText(filepath.Join(request.OutputPath, "NEXT_ACTION.md"), next+"\n", 0o600); err != nil {
		return RemoteFailurePreservation{}, err
	}
	if err := updateBlockedHandoff(request.HandoffPath, "failed phase: "+request.Phase+" (exit "+fmt.Sprint(request.PhaseExit)+", "+status+")", next); err != nil {
		status = "preservation-failed"
	}
	receipt := RemoteFailurePreservation{SchemaVersion: 1, Status: status, Phase: request.Phase, PhaseExit: request.PhaseExit, AttemptSHA256: attemptBindingHash(attempt), AuthoritySHA256: replayBytesSHA256(authorityBytes), Host: authority.Host, RemoteRoot: authority.AttemptRoot, Copy: copy, Checksum: checksum, TreeManifestSHA256: treeSHA}
	if err := WriteNewJSON(filepath.Join(request.OutputPath, "REMOTE_FAILURE.json"), receipt); err != nil {
		return RemoteFailurePreservation{}, err
	}
	if status != "preserved" {
		return receipt, fmt.Errorf("remote failure preservation failed")
	}
	return receipt, nil
}

func remoteFailureOutputText(output salesforceCommandOutput) string {
	return string(append(append([]byte{}, output.Stdout...), output.Stderr...))
}

func validateRemoteFailureRequest(request RemoteFailurePreserveRequest) error {
	for _, path := range []string{request.AttemptPath, request.BindingPath, request.HandoffPath, request.OutputPath} {
		if !filepath.IsAbs(path) {
			return fmt.Errorf("absolute remote failure paths are required")
		}
	}
	if request.PhaseExit == 0 || !safeAttemptRunID(request.Phase) || request.Phase == "" {
		return fmt.Errorf("nonzero phase exit and safe phase are required")
	}
	if _, err := os.Stat(request.HandoffPath); err != nil {
		return fmt.Errorf("handoff is unavailable: %w", err)
	}
	return nil
}

func runRemoteFailureCopy(ctx context.Context, source, destination, _ string, checksum bool) (salesforceCommandOutput, error) {
	args := []string{"-a"}
	if checksum {
		args = append(args, "--checksum", "--dry-run")
	}
	args = append(args, source, destination+string(filepath.Separator))
	command := exec.CommandContext(ctx, remoteFailureCopyBinary, args...)
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	command.Stdout, command.Stderr = stdout, stderr
	err := command.Run()
	output := salesforceCommandOutput{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	if exit, ok := err.(*exec.ExitError); ok {
		output.ExitCode = exit.ExitCode()
		return output, nil
	}
	return output, err
}

func updateBlockedHandoff(path, last, next string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	set := func(prefix, value string) {
		for i, line := range lines {
			if strings.HasPrefix(line, prefix) {
				lines[i] = prefix + value
				return
			}
		}
		lines = append(lines, prefix+value)
	}
	set("Current gate: ", "blocked")
	set("Last completed command: ", last)
	set("Next command: ", next)
	temp := path + ".next"
	if err := os.WriteFile(temp, []byte(strings.Join(lines, "\n")), 0o600); err != nil {
		return err
	}
	return os.Rename(temp, path)
}

func writeModeBearingTreeManifest(root string) error {
	type entry struct {
		Path   string `json:"path"`
		Mode   string `json:"mode"`
		SHA256 string `json:"sha256"`
	}
	entries := []entry{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || path == root || filepath.Base(path) == "TREE_MANIFEST.json" {
			return err
		}
		if info.IsDir() {
			return nil
		}
		sha, err := sha256FileDirect(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		entries = append(entries, entry{Path: filepath.ToSlash(rel), Mode: fmt.Sprintf("%04o", info.Mode()&os.ModePerm), SHA256: sha})
		return nil
	})
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return WriteNewJSON(filepath.Join(root, "TREE_MANIFEST.json"), entries)
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
