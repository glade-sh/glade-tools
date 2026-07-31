package scripts

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fakeTool writes one Bash fake that copies fixtures from env vars.
func fakeTool(t *testing.T) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "glade-tools")
	os.WriteFile(f, []byte(`#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$@" >> "${FAKE_TOOL_ARGS_FILE:-/dev/null}"
cmd="${1:-}"; shift; out=""
while [[ $# -gt 0 ]]; do case "$1" in --out) out="$2"; shift 2 ;; *) shift ;; esac; done
case "$cmd" in
  salesforce) mkdir -p "$(dirname "$out")" 2>/dev/null || true; cp "${TEST_VERIFIER_JSON:?}" "$out"; exit "${TEST_VERIFIER_EXIT:-0}" ;;
  corpus)     src="${TEST_CORPUS_DIR:?}"; mkdir -p "$out" 2>/dev/null || true; cp -r "$src"/* "$out/" 2>/dev/null || true; exit "${TEST_CORPUS_EXIT:-0}" ;;
  *) echo "unknown: $cmd" >&2; exit 127 ;;
esac
`), 0755)
	return f
}

func makeGitRepo(t *testing.T) (string, string) {
	t.Helper()
	r := t.TempDir()
	runCmd(t, r, "git", "init")
	runCmd(t, r, "git", "config", "user.email", "test@example.com")
	runCmd(t, r, "git", "config", "user.name", "Test")
	os.WriteFile(filepath.Join(r, "README.md"), []byte("# glade\n"), 0644)
	runCmd(t, r, "git", "add", "README.md")
	runCmd(t, r, "git", "commit", "-m", "initial")
	b, _ := exec.Command("git", "-C", r, "rev-parse", "HEAD").Output()
	return r, strings.TrimSpace(string(b))
}

func runGate(t *testing.T, scriptPath, fakeBin, argsFile, gladeRoot, gladeBin, targetOrg, outDir, vJSON, cDir string, vExit, cExit int) (int, string) {
	t.Helper()
	os.Remove(argsFile)
	c := exec.Command("bash", scriptPath, fakeBin, gladeRoot, gladeBin, targetOrg, outDir)
	c.Env = append(os.Environ(),
		"FAKE_TOOL_ARGS_FILE="+argsFile,
		"TEST_VERIFIER_JSON="+vJSON,
		"TEST_CORPUS_DIR="+cDir,
		fmt.Sprintf("TEST_VERIFIER_EXIT=%d", vExit),
		fmt.Sprintf("TEST_CORPUS_EXIT=%d", cExit),
	)
	out, err := c.CombinedOutput()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode(), string(out)
		}
		t.Fatalf("gate launch error: %v", err)
	}
	return 0, string(out)
}

func writeVerifierJSON(t *testing.T, dir, status, gladeCommit, toolsCommit, shaBefore, shaAfter string) string {
	t.Helper()
	if shaAfter == "" {
		shaAfter = shaBefore
	}
	cp, cf := 422, 0
	sp, sf := 486, 0
	cs := "pass"
	if status != "pass" {
		cp, cf, sp, sf, cs = 0, 422, 64, 422, "fail"
	}
	j := fmt.Sprintf(`{"schemaVersion":1,"release":"Summer '26","apiVersion":"66.0","status":"%s","glade":{"commit":"%s","dirty":false},"gladeTools":{"commit":"%s","dirty":false},"candidate":{"path":"glade","sha256Before":"%s","sha256After":"%s"},"inputs":[],"compiler":{"status":"%s","summary":{"pass":%d,"fail":%d,"inconclusive":0}},"runtime":{"status":"pass","summary":{"pass":52,"fail":0,"inconclusive":0}},"lifecycle":{"status":"pass","summary":{"pass":12,"fail":0,"inconclusive":0}},"summary":{"pass":%d,"fail":%d,"inconclusive":0}}`,
		status, gladeCommit, toolsCommit, shaBefore, shaAfter, cs, cp, cf, sp, sf)
	p := filepath.Join(dir, "verifier.json")
	os.WriteFile(p, []byte(j), 0644)
	return p
}

func writeCorpusDir(t *testing.T, dir, project string, exitCode, diagnostics int, errMsg string) string {
	t.Helper()
	cd := filepath.Join(dir, "corpus_fixture")
	os.MkdirAll(cd, 0755)
	r := fmt.Sprintf("%s\ttestdata/salesforce-correctness\t%d\t%d\t%s\n", project, exitCode, diagnostics, errMsg)
	os.WriteFile(filepath.Join(cd, "summary.tsv"), []byte("project\tpath\texitCode\tdiagnostics\terror\n"+r), 0644)
	for _, n := range []string{"by_code.tsv", "by_project_code.tsv", "by_stem.tsv", "classified.tsv", "diagnostics.tsv"} {
		os.WriteFile(filepath.Join(cd, n), []byte("key\tcount\n"), 0644)
	}
	return cd
}

func runCmd(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	c := exec.Command(name, args...)
	c.Dir = dir
	if o, e := c.CombinedOutput(); e != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, e, o)
	}
}

func toolsRoot(t *testing.T) string {
	t.Helper()
	o, _ := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	return strings.TrimSpace(string(o))
}

func gitHead(t *testing.T, dir string) string {
	t.Helper()
	o, _ := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	return strings.TrimSpace(string(o))
}

func makeGladeBin(t *testing.T) (string, string) {
	t.Helper()
	gb := filepath.Join(t.TempDir(), "glade")
	os.WriteFile(gb, []byte("fake glade binary"), 0755)
	return gb, fmt.Sprintf("%x", sha256.Sum256([]byte("fake glade binary")))
}

type gateFixture struct {
	tr, sp, gr, gc, tc, gb, sha, fk, vj, cd string
}

func newGateFixture(t *testing.T) gateFixture {
	t.Helper()
	f := gateFixture{}
	f.tr = toolsRoot(t)
	f.sp = filepath.Join(f.tr, "scripts", "salesforce-correctness-gate.sh")
	if _, err := os.Stat(f.sp); err != nil {
		t.Fatalf("gate script missing: %v", err)
	}
	f.gr, f.gc = makeGitRepo(t)
	f.tc = gitHead(t, f.tr)
	f.gb, f.sha = makeGladeBin(t)
	f.fk = fakeTool(t)
	f.vj = writeVerifierJSON(t, t.TempDir(), "pass", f.gc, f.tc, f.sha, "")
	f.cd = writeCorpusDir(t, t.TempDir(), "salesforce-correctness", 0, 0, "")
	return f
}

func TestSalesforceCorrectnessGateSuccess(t *testing.T) {
	f := newGateFixture(t)
	od := filepath.Join(t.TempDir(), "evidence")
	argsFile := filepath.Join(t.TempDir(), "args.txt")
	code, out := runGate(t, f.sp, f.fk, argsFile, f.gr, f.gb, "test-org", od, f.vj, f.cd, 0, 0)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	for _, rel := range []string{"salesforce-verification.json", "corpus/summary.tsv", "SHA256SUMS.txt"} {
		if _, err := os.Stat(filepath.Join(od, rel)); err != nil {
			t.Fatalf("missing %s: %v", rel, err)
		}
	}
	argsRaw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("cannot read args file: %v", err)
	}
	argv := strings.Split(strings.TrimSpace(string(argsRaw)), "\n")
	want := []string{
		"salesforce", "verify",
		"--release-manifest", "docs/fixtures/salesforce-release-current.json",
		"--catalog", "docs/fixtures/apex-language-rules.json",
		"--runtime-cases", "docs/fixtures/salesforce-runtime-correctness.json",
		"--test-project", "testdata/salesforce-correctness",
		"--target-org", "test-org",
		"--glade-bin", f.gb,
		"--glade-root", f.gr,
		"--out", od + "/salesforce-verification.json",
		"corpus", "check",
		"--root", "testdata/salesforce-correctness",
		"--glade", f.gb,
		"--out", od + "/corpus",
		"--fail-on-unclassified",
		"--max-unclassified", "0",
		"--fail-on-check-closure",
	}
	if !slices.Equal(argv, want) {
		t.Fatalf("tool argv mismatch:\n  got:  %q\n  want: %q", argv, want)
	}
	od2 := filepath.Join(t.TempDir(), "evidence2")
	code2, out2 := runGate(t, f.sp, f.fk, filepath.Join(t.TempDir(), "a2.txt"), f.gr, f.gb, "test-org", od2, f.vj, f.cd, 0, 0)
	if code2 != 0 {
		t.Fatalf("run2 exit %d: %s", code2, out2)
	}
	s1, _ := os.ReadFile(filepath.Join(od, "SHA256SUMS.txt"))
	s2, _ := os.ReadFile(filepath.Join(od2, "SHA256SUMS.txt"))
	if string(s1) != string(s2) {
		t.Fatalf("checksums not stable:\n%s\n%s", s1, s2)
	}
}

func TestSalesforceCorrectnessGateRelativeOutDir(t *testing.T) {
	f := newGateFixture(t)
	odAbs := filepath.Join(t.TempDir(), "relative-evidence")
	relOd, err := filepath.Rel(f.tr, odAbs)
	if err != nil {
		t.Fatalf("cannot compute relative path: %v", err)
	}
	code, out := runGate(t, f.sp, f.fk, filepath.Join(t.TempDir(), "args.txt"), f.gr, f.gb, "test-org", relOd, f.vj, f.cd, 0, 0)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	for _, rel := range []string{"salesforce-verification.json", "corpus/summary.tsv", "SHA256SUMS.txt"} {
		if _, err := os.Stat(filepath.Join(odAbs, rel)); err != nil {
			t.Fatalf("missing %s: %v", rel, err)
		}
	}
}

func TestSalesforceCorrectnessGateVerifierFail(t *testing.T) {
	f := newGateFixture(t)
	f.vj = writeVerifierJSON(t, t.TempDir(), "fail", f.gc, f.tc, f.sha, "")
	code, out := runGate(t, f.sp, f.fk, filepath.Join(t.TempDir(), "a.txt"), f.gr, f.gb, "test-org", filepath.Join(t.TempDir(), "evidence"), f.vj, f.cd, 0, 0)
	if code == 0 {
		t.Fatalf("should fail on non-pass: %s", out)
	}
}

func TestSalesforceCorrectnessGateHashMismatch(t *testing.T) {
	f := newGateFixture(t)
	f.vj = writeVerifierJSON(t, t.TempDir(), "pass", f.gc, f.tc, f.sha, strings.Repeat("0", 64))
	code, out := runGate(t, f.sp, f.fk, filepath.Join(t.TempDir(), "a.txt"), f.gr, f.gb, "test-org", filepath.Join(t.TempDir(), "evidence"), f.vj, f.cd, 0, 0)
	if code == 0 {
		t.Fatalf("should fail on hash mismatch: %s", out)
	}
}

func TestSalesforceCorrectnessGateWrongGladeCommit(t *testing.T) {
	f := newGateFixture(t)
	f.vj = writeVerifierJSON(t, t.TempDir(), "pass", strings.Repeat("0", 40), f.tc, f.sha, "")
	code, out := runGate(t, f.sp, f.fk, filepath.Join(t.TempDir(), "a.txt"), f.gr, f.gb, "test-org", filepath.Join(t.TempDir(), "evidence"), f.vj, f.cd, 0, 0)
	if code == 0 {
		t.Fatalf("should fail on wrong glade commit: %s", out)
	}
}

func TestSalesforceCorrectnessGateCorpusFail(t *testing.T) {
	f := newGateFixture(t)
	od := filepath.Join(t.TempDir(), "evidence")
	code, out := runGate(t, f.sp, f.fk, filepath.Join(t.TempDir(), "a.txt"), f.gr, f.gb, "test-org", od, f.vj, f.cd, 0, 1)
	if code == 0 {
		t.Fatalf("should fail on corpus failure: %s", out)
	}
	if _, err := os.Stat(filepath.Join(od, "salesforce-verification.json")); err != nil {
		t.Fatalf("verifier evidence must be preserved: %v", err)
	}
}

func TestSalesforceCorrectnessGateCorpusBadSummary(t *testing.T) {
	f := newGateFixture(t)
	f.cd = writeCorpusDir(t, t.TempDir(), "salesforce-correctness", 1, 5, "some error")
	code, out := runGate(t, f.sp, f.fk, filepath.Join(t.TempDir(), "a.txt"), f.gr, f.gb, "test-org", filepath.Join(t.TempDir(), "evidence"), f.vj, f.cd, 0, 0)
	if code == 0 {
		t.Fatalf("should fail on nonzero corpus exit: %s", out)
	}
	if err := os.WriteFile(filepath.Join(f.cd, "summary.tsv"), []byte("project\tpath\texitCode\tdiagnostics\terror\n"), 0644); err != nil {
		t.Fatal(err)
	}
	code, out = runGate(t, f.sp, f.fk, filepath.Join(t.TempDir(), "zero.txt"), f.gr, f.gb, "test-org", filepath.Join(t.TempDir(), "zero-evidence"), f.vj, f.cd, 0, 0)
	if code == 0 {
		t.Fatalf("should fail on zero corpus projects: %s", out)
	}
}

func TestSalesforceCorrectnessGateSensitiveTargetOrgNotInChecksums(t *testing.T) {
	f := newGateFixture(t)
	tg := "my-sensitive-org-alias"
	od := filepath.Join(t.TempDir(), "evidence")
	code, out := runGate(t, f.sp, f.fk, filepath.Join(t.TempDir(), "a.txt"), f.gr, f.gb, tg, od, f.vj, f.cd, 0, 0)
	if code != 0 {
		t.Fatalf("gate failed: %d: %s", code, out)
	}
	chk, _ := os.ReadFile(filepath.Join(od, "SHA256SUMS.txt"))
	if strings.Contains(string(chk), tg) {
		t.Fatalf("SHA256SUMS.txt must not contain target org: %s", chk)
	}
}

func TestSalesforceCorrectnessGateWorkflowContract(t *testing.T) {
	tr := toolsRoot(t)
	wp := filepath.Join(tr, ".github", "workflows", "salesforce-correctness.yml")
	b, err := os.ReadFile(wp)
	if err != nil {
		t.Fatalf("workflow file missing: %v", err)
	}
	tx := string(b)

	must := func(want string) {
		t.Helper()
		if !strings.Contains(tx, want) {
			t.Fatalf("workflow missing %q", want)
		}
	}
	forbid := func(have string) {
		t.Helper()
		if strings.Contains(tx, have) {
			t.Fatalf("workflow unexpectedly contains %q", have)
		}
	}
	for _, marker := range []string{
		"name: Salesforce Correctness", "workflow_dispatch:", "glade_sha:", "required: true",
		`^([0-9a-fA-F]{40})$`, `"${GLADE_SHA,,}"`, `echo "GLADE_SHA=${GLADE_SHA}" >> "$GITHUB_ENV"`,
		`test "$(git -C ../glade rev-parse HEAD)" = "$GLADE_SHA"`,
		"salesforce-correctness:", "ubuntu-latest", "timeout-minutes: 75", "contents: read",
		`repository: glade-sh/glade
          path: glade
          ref: ${{ inputs.glade_sha }}`,
		`node-version: "22"`, "1.26.5", "@salesforce/cli@2.145.6",
		`AUTH_URL: ${{ secrets.SF_SFDX_AUTH_URL }}`, `AUTH_FILE="$RUNNER_TEMP/sfdx-auth-url.txt"`,
		`trap 'rm -f "$AUTH_FILE" "$LOGIN_RESULT" "$LOGIN_ERROR"' EXIT`,
		`printf '%s' "$AUTH_URL" > "$AUTH_FILE"`, `chmod 600 "$AUTH_FILE"`,
		`--alias glade-salesforce-correctness \`, "scripts/release-build.sh",
		"RELEASE_SHARED_PAYLOAD_ARCHIVE", "RELEASE_SHARED_PAYLOAD_SHA256",
		`glade_${VERSION}_linux_amd64.tar.gz`, "./cmd/glade-tools",
		`scripts/salesforce-correctness-gate.sh \
            "$TOOLS_BIN" \
            "$(realpath ../glade)" \
            "$GLADE_CANDIDATE" \
            glade-salesforce-correctness \
            "$RUNNER_TEMP/salesforce-correctness-evidence/gate"`,
		`if: always()
        with:
          name: salesforce-correctness-evidence`, "retention-days: 90",
	} {
		must(marker)
	}
	for _, f := range []string{"pull_request:", "push:", "schedule:", "workflow_call:", "continue-on-error"} {
		forbid(f)
	}
	for _, f := range []string{"--developer", "go build ./cmd/glade\n"} {
		forbid(f)
	}
	pins := map[string]string{
		"actions/checkout":                "df4cb1c069e1874edd31b4311f1884172cec0e10",
		"actions/create-github-app-token": "bcd2ba49218906704ab6c1aa796996da409d3eb1",
		"actions/setup-go":                "924ae3a1cded613372ab5595356fb5720e22ba16",
		"actions/setup-node":              "249970729cb0ef3589644e2896645e5dc5ba9c38",
		"actions/upload-artifact":         "ea165f8d65b6e75b540449e92b4886f43607fa02",
	}
	for a, s := range pins {
		must(a + "@" + s)
	}
}
