package toolcli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/surfaceledger"
)

func TestCompatSurfaceDeltaVerifyRejectsFreshlyHashedProjectedLedgers(t *testing.T) {
	root := t.TempDir()
	fullBasePath, fullBaseSnapshots, _ := writeClosureLedger(t, filepath.Join(root, "full-base"), nil)
	fullCurrentPath, fullCurrentSnapshots, _ := writeClosureLedger(t, filepath.Join(root, "full-current"), []surfaceledger.SurfaceLedgerRow{
		{SurfaceID: "apex:System.Selected", Evidence: surfaceledger.EvidenceFixture},
		{SurfaceID: "apex:System.Unrelated", Evidence: surfaceledger.EvidenceFixture},
	})
	expectedPath := filepath.Join(root, "expected.json")
	writeJSON(t, expectedPath, []string{"apex:System.Selected"})
	authorityPath := writeDeltaAuthority(t, root, fullBasePath, fullBaseSnapshots, fullCurrentPath, fullCurrentSnapshots, expectedPath)
	sealedAuthorityPath, attemptPath, toolsRoot, trustedAttemptSHA := sealDeltaAuthority(t, filepath.Join(root, "trusted-tools"), authorityPath)
	projectedBase, baseSnapshots, _ := writeClosureLedgerFromRows(t, filepath.Join(root, "projected-base"), []surfaceledger.SurfaceLedgerRow{{SurfaceID: "apex:System.Selected", Docs: surfaceledger.SourcePresent}}, nil)
	projectedCurrent, currentSnapshots, _ := writeClosureLedgerFromRows(t, filepath.Join(root, "projected-current"), []surfaceledger.SurfaceLedgerRow{{SurfaceID: "apex:System.Selected", Docs: surfaceledger.SourcePresent}}, []surfaceledger.SurfaceLedgerRow{{SurfaceID: "apex:System.Selected", Evidence: surfaceledger.EvidenceFixture}})
	authoritySHA, _ := sha256File(sealedAuthorityPath)
	outputPath := filepath.Join(root, "DELTA_VERIFICATION.json")
	baseSHA, _ := sha256File(projectedBase)
	currentSHA, _ := sha256File(projectedCurrent)

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{
		"compat", "surface", "delta-verify",
		"--base-ledger", projectedBase,
		"--base-sha256", baseSHA,
		"--base-snapshot-dir", baseSnapshots,
		"--current-ledger", projectedCurrent,
		"--current-sha256", currentSHA,
		"--current-snapshot-dir", currentSnapshots,
		"--expected-ids", expectedPath,
		"--authority-manifest", sealedAuthorityPath,
		"--authority-sha256", authoritySHA,
		"--attempt", attemptPath,
		"--attempt-sha256", trustedAttemptSHA,
		"--candidate-root", toolsRoot + "-candidate",
		"--candidate", toolsRoot + "-candidate-bin",
		"--tools-root", toolsRoot,
		"--output", outputPath,
	}, &stdout, &stderr)
	if code == 0 || bytes.Contains(stderr.Bytes(), []byte("unknown flag")) || !bytes.Contains(stderr.Bytes(), []byte("authority")) {
		t.Fatalf("projected closure: code=%d stdout=%q stderr=%q base=%s current=%s", code, stdout.String(), stderr.String(), projectedBase, projectedCurrent)
	}
	if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
		t.Fatalf("projected closure created output: %v", err)
	}
}

func TestCompatSurfaceDeltaVerifyRejectsGitReplacementOfTrustedCommit(t *testing.T) {
	root := t.TempDir()
	basePath, baseSnapshots, _ := writeClosureLedger(t, filepath.Join(root, "base"), nil)
	currentPath, currentSnapshots, _ := writeClosureLedger(t, filepath.Join(root, "current"), []surfaceledger.SurfaceLedgerRow{{SurfaceID: "apex:System.Selected", Evidence: surfaceledger.EvidenceFixture}})
	expectedPath := filepath.Join(root, "expected.json")
	writeJSON(t, expectedPath, []string{"apex:System.Selected"})
	authorityPath := writeDeltaAuthority(t, root, basePath, baseSnapshots, currentPath, currentSnapshots, expectedPath)
	sealedPath, attemptPath, toolsRoot, attemptSHA := sealDeltaAuthority(t, filepath.Join(root, "tools"), authorityPath)
	trustedCommit, err := exec.Command("git", "-C", toolsRoot, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(sealedPath)
	if err != nil {
		t.Fatal(err)
	}
	var forged map[string]any
	if err := json.Unmarshal(data, &forged); err != nil {
		t.Fatal(err)
	}
	forged["expectedIDsSha256"] = strings.Repeat("f", 64)
	writeJSON(t, sealedPath, forged)
	for _, args := range [][]string{{"add", "."}, {"commit", "-m", "forged authority"}} {
		if output, err := exec.Command("git", append([]string{"-C", toolsRoot}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	forgedCommit, err := exec.Command("git", "-C", toolsRoot, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	trusted := strings.TrimSpace(string(trustedCommit))
	forgedID := strings.TrimSpace(string(forgedCommit))
	for _, args := range [][]string{{"replace", trusted, forgedID}, {"reset", "--hard", trusted}} {
		if output, err := exec.Command("git", append([]string{"-C", toolsRoot}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	authoritySHA, _ := sha256File(sealedPath)
	baseSHA, _ := sha256File(basePath)
	currentSHA, _ := sha256File(currentPath)
	outputPath := filepath.Join(root, "DELTA_VERIFICATION.json")
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{
		"compat", "surface", "delta-verify",
		"--base-ledger", basePath, "--base-sha256", baseSHA, "--base-snapshot-dir", baseSnapshots,
		"--current-ledger", currentPath, "--current-sha256", currentSHA, "--current-snapshot-dir", currentSnapshots,
		"--expected-ids", expectedPath,
		"--authority-manifest", sealedPath, "--authority-sha256", authoritySHA,
		"--attempt", attemptPath, "--attempt-sha256", attemptSHA,
		"--candidate-root", toolsRoot + "-candidate", "--candidate", toolsRoot + "-candidate-bin",
		"--tools-root", toolsRoot,
		"--output", outputPath,
	}, &stdout, &stderr)
	if code == 0 || (!bytes.Contains(stderr.Bytes(), []byte("not clean")) && !bytes.Contains(stderr.Bytes(), []byte("sealed tools commit"))) {
		t.Fatalf("replace attack: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
		t.Fatalf("replace attack created output: %v", err)
	}
}

func TestCompatSurfaceDeltaVerifyRejectsSameToolsForgedCandidateAttempt(t *testing.T) {
	root := t.TempDir()
	basePath, baseSnapshots, _ := writeClosureLedger(t, filepath.Join(root, "base"), nil)
	currentPath, currentSnapshots, _ := writeClosureLedger(t, filepath.Join(root, "current"), []surfaceledger.SurfaceLedgerRow{{SurfaceID: "apex:System.Selected", Evidence: surfaceledger.EvidenceFixture}})
	expectedPath := filepath.Join(root, "expected.json")
	writeJSON(t, expectedPath, []string{"apex:System.Selected"})
	authorityPath := writeDeltaAuthority(t, root, basePath, baseSnapshots, currentPath, currentSnapshots, expectedPath)
	sealedPath, attemptPath, toolsRoot, _ := sealDeltaAuthority(t, filepath.Join(root, "tools"), authorityPath)

	data, err := os.ReadFile(attemptPath)
	if err != nil {
		t.Fatal(err)
	}
	authoritySHA, _ := sha256File(sealedPath)
	baseSHA, _ := sha256File(basePath)
	currentSHA, _ := sha256File(currentPath)
	run := func(t *testing.T, attempt string, candidateRoot string) {
		t.Helper()
		attemptSHA, _ := sha256File(attempt)
		outputPath := filepath.Join(root, t.Name()+"-ACCEPTANCE.json")
		var stdout, stderr bytes.Buffer
		code := Run(context.Background(), []string{
			"compat", "surface", "delta-verify",
			"--base-ledger", basePath, "--base-sha256", baseSHA, "--base-snapshot-dir", baseSnapshots,
			"--current-ledger", currentPath, "--current-sha256", currentSHA, "--current-snapshot-dir", currentSnapshots,
			"--expected-ids", expectedPath,
			"--authority-manifest", sealedPath, "--authority-sha256", authoritySHA,
			"--attempt", attempt, "--attempt-sha256", attemptSHA,
			"--candidate-root", candidateRoot, "--candidate", toolsRoot + "-candidate-bin",
			"--tools-root", toolsRoot,
			"--output", outputPath,
		}, &stdout, &stderr)
		if code == 0 {
			t.Fatalf("forged authority input passed: stdout=%q stderr=%q", stdout.String(), stderr.String())
		}
		if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
			t.Fatalf("forged authority input created output: %v", err)
		}
	}
	for name, mutate := range map[string]func(map[string]any){
		"candidate-commit": func(attempt map[string]any) {
			attempt["candidate"].(map[string]any)["commit"] = strings.Repeat("0", 40)
		},
		"candidate-sha": func(attempt map[string]any) {
			attempt["candidate"].(map[string]any)["sha256"] = strings.Repeat("f", 64)
		},
		"tools-sha": func(attempt map[string]any) { attempt["tools"].(map[string]any)["sha256"] = strings.Repeat("f", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			var forged map[string]any
			if err := json.Unmarshal(data, &forged); err != nil {
				t.Fatal(err)
			}
			mutate(forged)
			forgedAttempt := filepath.Join(root, name+"-ATTEMPT.json")
			writeJSON(t, forgedAttempt, forged)
			run(t, forgedAttempt, toolsRoot+"-candidate")
		})
	}
	t.Run("candidate-root", func(t *testing.T) {
		candidateRoot := toolsRoot + "-candidate"
		if output, err := exec.Command("git", "-C", candidateRoot, "switch", "-c", "wrong-root").CombinedOutput(); err != nil {
			t.Fatalf("attach wrong candidate root: %v: %s", err, output)
		}
		if err := os.WriteFile(filepath.Join(candidateRoot, "README.md"), []byte("wrong candidate tree\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{{"add", "."}, {"commit", "-m", "wrong candidate tree"}} {
			if output, err := exec.Command("git", append([]string{"-C", candidateRoot}, args...)...).CombinedOutput(); err != nil {
				t.Fatalf("wrong candidate git %v: %v: %s", args, err, output)
			}
		}
		wrongCommit := gitOutput(t, candidateRoot, "rev-parse", "HEAD")
		if output, err := exec.Command("git", "-C", candidateRoot, "switch", "--detach", wrongCommit).CombinedOutput(); err != nil {
			t.Fatalf("detach wrong candidate root: %v: %s", err, output)
		}
		run(t, attemptPath, candidateRoot)
	})
}

func writeClosureLedger(t *testing.T, root string, evidence []surfaceledger.SurfaceLedgerRow) (string, string, surfaceledger.SurfaceLedger) {
	t.Helper()
	docs := []surfaceledger.SurfaceLedgerRow{
		{SurfaceID: "apex:System.Selected", Docs: surfaceledger.SourcePresent},
		{SurfaceID: "apex:System.Unrelated", Docs: surfaceledger.SourcePresent},
	}
	return writeClosureLedgerFromRows(t, root, docs, evidence)
}

func writeClosureLedgerFromRows(t *testing.T, root string, docs, evidence []surfaceledger.SurfaceLedgerRow) (string, string, surfaceledger.SurfaceLedger) {
	t.Helper()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string][]surfaceledger.SurfaceLedgerRow{
		"DOCS_SNAPSHOT.json":     docs,
		"ORG_SNAPSHOT.json":      nil,
		"GLADE_SNAPSHOT.json":    nil,
		"EVIDENCE_SNAPSHOT.json": evidence,
	}
	bindings := surfaceledger.SourceSnapshotBindings{Files: map[string]string{}}
	for name, rows := range files {
		path := filepath.Join(root, name)
		writeJSON(t, path, rows)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		bindings.Files[name] = fmt.Sprintf("%x", sha256.Sum256(data))
	}
	ledger := surfaceledger.Merge(docs, nil, nil, evidence)
	surfaceledger.AssignPriorities(ledger.Rows)
	ledger.Summary = surfaceledger.Summarize(ledger.Rows)
	ledger.SourceSnapshotBindings = &bindings
	path := filepath.Join(root, "SURFACE_LEDGER.json")
	writeJSON(t, path, ledger)
	return path, root, ledger
}

func writeDeltaAuthority(t *testing.T, root, basePath, baseSnapshots, currentPath, currentSnapshots, expectedPath string) string {
	t.Helper()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	entry := func(ledgerPath, snapshotDir string) map[string]any {
		ledgerSHA, err := sha256File(ledgerPath)
		if err != nil {
			t.Fatal(err)
		}
		snapshots := map[string]string{}
		for _, name := range []string{"DOCS_SNAPSHOT.json", "ORG_SNAPSHOT.json", "GLADE_SNAPSHOT.json", "EVIDENCE_SNAPSHOT.json"} {
			sha, err := sha256File(filepath.Join(snapshotDir, name))
			if err != nil {
				t.Fatal(err)
			}
			snapshots[name] = sha
		}
		return map[string]any{"ledgerSha256": ledgerSHA, "snapshotSha256": snapshots, "sourceIdentity": nil}
	}
	expectedSHA, err := sha256File(expectedPath)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "DELTA_AUTHORITY.json")
	writeJSON(t, path, map[string]any{"schemaVersion": 1, "status": "externally-reviewed", "base": entry(basePath, baseSnapshots), "current": entry(currentPath, currentSnapshots), "expectedIDsSha256": expectedSHA})
	return path
}

func sealDeltaAuthority(t *testing.T, toolsRoot, authorityPath string) (string, string, string, string) {
	t.Helper()
	candidateRoot := toolsRoot + "-candidate"
	if err := os.MkdirAll(candidateRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(candidateRoot, "README.md"), []byte("candidate\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init"}, {"config", "user.email", "test@example.com"}, {"config", "user.name", "Test"}, {"add", "."}, {"commit", "-m", "candidate"}} {
		if output, err := exec.Command("git", append([]string{"-C", candidateRoot}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("candidate git %v: %v: %s", args, err, output)
		}
	}
	candidateCommit := gitOutput(t, candidateRoot, "rev-parse", "HEAD")
	candidateTree := gitOutput(t, candidateRoot, "rev-parse", "HEAD^{tree}")
	if output, err := exec.Command("git", "-C", candidateRoot, "switch", "--detach", candidateCommit).CombinedOutput(); err != nil {
		t.Fatalf("detach candidate: %v: %s", err, output)
	}
	candidateBinary := toolsRoot + "-candidate-bin"
	if err := os.WriteFile(candidateBinary, []byte("candidate binary\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	candidateSHA, err := sha256File(candidateBinary)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(toolsRoot, "docs", "reviews"), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(authorityPath)
	if err != nil {
		t.Fatal(err)
	}
	var authority map[string]any
	if err := json.Unmarshal(data, &authority); err != nil {
		t.Fatal(err)
	}
	authority["candidate"] = map[string]string{"commit": candidateCommit, "tree": candidateTree, "sha256": candidateSHA}
	data, err = json.MarshalIndent(authority, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	sealedPath := filepath.Join(toolsRoot, "docs", "reviews", "DELTA_AUTHORITY.json")
	if err := os.WriteFile(sealedPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init"}, {"config", "user.email", "test@example.com"}, {"config", "user.name", "Test"}, {"add", "."}, {"commit", "-m", "seal authority"}} {
		if output, err := exec.Command("git", append([]string{"-C", toolsRoot}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	commit, err := exec.Command("git", "-C", toolsRoot, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	attemptPath := filepath.Join(filepath.Dir(toolsRoot), filepath.Base(toolsRoot)+"-ATTEMPT.json")
	sha := strings.Repeat("a", 64)
	runningTools, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	runningToolsSHA, err := sha256File(runningTools)
	if err != nil {
		t.Fatal(err)
	}
	writeJSON(t, attemptPath, map[string]any{
		"schemaVersion":                1,
		"inventorySha256":              sha,
		"candidateAuthoritySha256":     sha,
		"candidate":                    map[string]string{"commit": candidateCommit, "os": "darwin", "arch": "arm64", "sha256": candidateSHA},
		"tools":                        map[string]string{"commit": strings.TrimSpace(string(commit)), "os": "darwin", "arch": "arm64", "sha256": runningToolsSHA},
		"remoteCleanupAuthoritySha256": map[string]string{"replay-worker": sha, "salesforce-worker": sha},
	})
	if output, err := exec.Command("git", "-C", toolsRoot, "switch", "--detach", strings.TrimSpace(string(commit))).CombinedOutput(); err != nil {
		t.Fatalf("detach tools: %v: %s", err, output)
	}
	attemptSHA, err := sha256File(attemptPath)
	if err != nil {
		t.Fatal(err)
	}
	return sealedPath, attemptPath, toolsRoot, attemptSHA
}

func gitOutput(t *testing.T, root string, args ...string) string {
	t.Helper()
	output, err := exec.Command("git", append([]string{"-C", root}, args...)...).Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(output))
}

func TestCompatSurfaceDeltaVerifyRejectsProjectedSetAndPreservesFailureReport(t *testing.T) {
	root := t.TempDir()
	basePath, baseSnapshots, _ := writeClosureLedger(t, filepath.Join(root, "base"), nil)
	currentPath, currentSnapshots, _ := writeClosureLedger(t, filepath.Join(root, "current"), []surfaceledger.SurfaceLedgerRow{
		{SurfaceID: "apex:System.Selected", Evidence: surfaceledger.EvidenceFixture},
		{SurfaceID: "apex:System.Unrelated", Evidence: surfaceledger.EvidenceFixture},
	})
	expectedPath := filepath.Join(root, "expected.json")
	outputPath := filepath.Join(root, "DELTA_VERIFICATION.json")
	writeJSON(t, expectedPath, []string{"apex:System.Selected"})
	authorityPath := writeDeltaAuthority(t, root, basePath, baseSnapshots, currentPath, currentSnapshots, expectedPath)
	sealedAuthorityPath, attemptPath, toolsRoot, attemptSHA := sealDeltaAuthority(t, filepath.Join(root, "tools"), authorityPath)
	authoritySHA, _ := sha256File(sealedAuthorityPath)
	baseSHA, err := sha256File(basePath)
	if err != nil {
		t.Fatal(err)
	}
	currentSHA, err := sha256File(currentPath)
	if err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{
		"compat", "surface", "delta-verify",
		"--base-ledger", basePath,
		"--base-sha256", baseSHA,
		"--base-snapshot-dir", baseSnapshots,
		"--current-ledger", currentPath,
		"--current-sha256", currentSHA,
		"--current-snapshot-dir", currentSnapshots,
		"--expected-ids", expectedPath,
		"--authority-manifest", sealedAuthorityPath,
		"--authority-sha256", authoritySHA,
		"--attempt", attemptPath,
		"--attempt-sha256", attemptSHA,
		"--candidate-root", toolsRoot + "-candidate",
		"--candidate", toolsRoot + "-candidate-bin",
		"--tools-root", toolsRoot,
		"--output", outputPath,
	}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected failure, stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	var report surfaceledger.ExactLedgerDeltaReport
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	if report.Status != "fail" || len(report.UnexpectedSurfaceIDs) != 1 || report.UnexpectedSurfaceIDs[0] != "apex:System.Unrelated" {
		t.Fatalf("report = %+v", report)
	}
	if report.AuthorityCandidate.Commit == "" || report.ActualCandidate.Commit != report.AuthorityCandidate.Commit || report.ActualCandidate.Tree != report.AuthorityCandidate.Tree || report.ActualCandidate.SHA256 != report.AuthorityCandidate.SHA256 || report.RunningToolsSHA256 == "" || report.AuthorityToolsCommit == "" {
		t.Fatalf("report authority binding = %+v", report)
	}

	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{
		"compat", "surface", "delta-verify",
		"--base-ledger", basePath,
		"--base-sha256", baseSHA,
		"--base-snapshot-dir", baseSnapshots,
		"--current-ledger", currentPath,
		"--current-sha256", currentSHA,
		"--current-snapshot-dir", currentSnapshots,
		"--expected-ids", expectedPath,
		"--authority-manifest", sealedAuthorityPath,
		"--authority-sha256", authoritySHA,
		"--attempt", attemptPath,
		"--attempt-sha256", attemptSHA,
		"--candidate-root", toolsRoot + "-candidate",
		"--candidate", toolsRoot + "-candidate-bin",
		"--tools-root", toolsRoot,
		"--output", outputPath,
	}, &stdout, &stderr)
	if code == 0 || !bytes.Contains(stderr.Bytes(), []byte("already exists")) {
		t.Fatalf("existing output: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	staleOutput := filepath.Join(root, "stale.json")
	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{
		"compat", "surface", "delta-verify",
		"--base-ledger", basePath,
		"--base-sha256", "stale",
		"--base-snapshot-dir", baseSnapshots,
		"--current-ledger", currentPath,
		"--current-sha256", currentSHA,
		"--current-snapshot-dir", currentSnapshots,
		"--expected-ids", expectedPath,
		"--authority-manifest", sealedAuthorityPath,
		"--authority-sha256", authoritySHA,
		"--attempt", attemptPath,
		"--attempt-sha256", attemptSHA,
		"--candidate-root", toolsRoot + "-candidate",
		"--candidate", toolsRoot + "-candidate-bin",
		"--tools-root", toolsRoot,
		"--output", staleOutput,
	}, &stdout, &stderr)
	if code == 0 || !bytes.Contains(stderr.Bytes(), []byte("base ledger SHA-256 mismatch")) {
		t.Fatalf("stale base: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(staleOutput); !os.IsNotExist(err) {
		t.Fatalf("stale binding created output: %v", err)
	}

	staleCurrentOutput := filepath.Join(root, "stale-current.json")
	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{
		"compat", "surface", "delta-verify",
		"--base-ledger", basePath,
		"--base-sha256", baseSHA,
		"--base-snapshot-dir", baseSnapshots,
		"--current-ledger", currentPath,
		"--current-sha256", "stale",
		"--current-snapshot-dir", currentSnapshots,
		"--expected-ids", expectedPath,
		"--authority-manifest", sealedAuthorityPath,
		"--authority-sha256", authoritySHA,
		"--attempt", attemptPath,
		"--attempt-sha256", attemptSHA,
		"--candidate-root", toolsRoot + "-candidate",
		"--candidate", toolsRoot + "-candidate-bin",
		"--tools-root", toolsRoot,
		"--output", staleCurrentOutput,
	}, &stdout, &stderr)
	if code == 0 || !bytes.Contains(stderr.Bytes(), []byte("current ledger SHA-256 mismatch")) {
		t.Fatalf("stale current: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(staleCurrentOutput); !os.IsNotExist(err) {
		t.Fatalf("stale current binding created output: %v", err)
	}
}
