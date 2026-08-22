package scripts

import (
	"crypto/sha256"
	"encoding/json"
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
	dir := t.TempDir()
	f := filepath.Join(dir, "glade-tools")
	os.WriteFile(f, []byte(`#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$@" >> "${FAKE_TOOL_ARGS_FILE:-/dev/null}"
cmd="${1:-}"; shift; out=""
case "$cmd" in
  salesforce)
    sub="${1:-}"; shift
    if [[ "$sub" == release ]]; then exit "${TEST_RELEASE_EXIT:-0}"; fi
    proof=""
    while [[ $# -gt 0 ]]; do case "$1" in --out) out="$2"; shift 2 ;; --product-version-proof) proof="$2"; shift 2 ;; *) shift ;; esac; done
    if [[ -n "${TEST_TAMPER_PRODUCT_PROOF:-}" ]]; then
      python3 - "$proof" <<'PY'
import json, sys
p=sys.argv[1]; d=json.load(open(p)); d['testEventsSHA256']='0'*64; json.dump(d, open(p,'w'))
PY
    fi
    mkdir -p "$(dirname "$out")" 2>/dev/null || true
    cp "${TEST_VERIFIER_JSON:?}" "$out"
    exit "${TEST_VERIFIER_EXIT:-0}"
    ;;
  corpus)
    while [[ $# -gt 0 ]]; do case "$1" in --out) out="$2"; shift 2 ;; *) shift ;; esac; done
    src="${TEST_CORPUS_DIR:?}"; mkdir -p "$out" 2>/dev/null || true; cp -r "$src"/* "$out/" 2>/dev/null || true; exit "${TEST_CORPUS_EXIT:-0}"
    ;;
  *) echo "unknown: $cmd" >&2; exit 127 ;;
esac
`), 0755)
	os.WriteFile(filepath.Join(dir, "go"), []byte(`#!/usr/bin/env bash
if [[ "${1:-}" == "version" && "${2:-}" == "-m" ]]; then
  case "${3:-}" in
    "${TEST_TOOLS_BIN:?}") revision="${TEST_TOOLS_REVISION:?}"; modified="${TEST_FAKE_TOOLS_MODIFIED:-false}" ;;
    "${TEST_GLADE_BIN:?}") revision="${TEST_GLADE_REVISION:?}"; modified="${TEST_FAKE_GLADE_MODIFIED:-false}" ;;
    *) exit 1 ;;
  esac
  printf '\tbuild\tvcs.revision=%s\n\tbuild\tvcs.modified=%s\n' "$revision" "$modified"
  exit 0
fi
printf '%s\n' '{"Action":"pass","Package":"github.com/glade-sh/glade/internal/sema","Test":"TestGeneratedPlatformAvailabilitySurface/apex:system.probe"}'
exit "${TEST_PRODUCT_TEST_EXIT:-0}"
`), 0755)
	os.WriteFile(filepath.Join(dir, "npm"), []byte("#!/usr/bin/env bash\nexit \"${TEST_NPM_EXIT:-0}\"\n"), 0755)
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
	toolsRoot := filepath.Dir(filepath.Dir(scriptPath))
	toolsRevision := os.Getenv("TEST_FAKE_TOOLS_REVISION")
	if toolsRevision == "" {
		toolsRevision = gitHead(t, toolsRoot)
	}
	gladeRevision := os.Getenv("TEST_FAKE_GLADE_REVISION")
	if gladeRevision == "" {
		gladeRevision = gitHead(t, gladeRoot)
	}
	c := exec.Command("bash", scriptPath, fakeBin, gladeRoot, gladeBin, targetOrg, outDir)
	c.Env = append(os.Environ(),
		"PATH="+filepath.Dir(fakeBin)+string(os.PathListSeparator)+os.Getenv("PATH"),
		"FAKE_TOOL_ARGS_FILE="+argsFile,
		"TEST_TOOLS_BIN="+fakeBin,
		"TEST_TOOLS_REVISION="+toolsRevision,
		"TEST_GLADE_BIN="+gladeBin,
		"TEST_GLADE_REVISION="+gladeRevision,
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
	cp, cf := 410, 0
	sp, sf := 475, 0
	cs := "pass"
	if status != "pass" {
		cp, cf, sp, sf, cs = 0, 410, 64, 410, "fail"
	}
	j := fmt.Sprintf(`{"schemaVersion":2,"release":"Summer '26","apiVersion":"67.0","status":"%s","glade":{"commit":"%s","dirty":false},"gladeTools":{"commit":"%s","dirty":false},"candidate":{"path":"glade","sha256Before":"%s","sha256After":"%s"},"inputs":[],"compiler":{"status":"%s","summary":{"required":410,"pass":%d,"fail":%d,"inconclusive":0}},"runtime":{"status":"pass","summary":{"required":53,"pass":53,"fail":0,"inconclusive":0}},"lifecycle":{"status":"pass","summary":{"required":12,"pass":12,"fail":0,"inconclusive":0}},"summary":{"required":475,"pass":%d,"fail":%d,"inconclusive":0},"releaseCompleteness":{"schemaVersion":1,"status":"pass","previousRelease":"Spring '26","currentRelease":"Summer '26","surfaceDelta":{"total":519,"classified":519,"implemented":516,"proved":519,"explicitNonParity":3},"behaviorDelta":{"total":22,"classified":22,"implemented":16,"proved":22,"explicitNonParity":6},"changeInventory":{"total":3990,"routed":3990,"outOfScope":3962},"sourceVersions":{"advertised":["65.0","66.0","67.0"],"passing":["65.0","66.0","67.0"]},"endpointVersions":{"advertised":["60.0","65.0","66.0","67.0"],"passing":["60.0","65.0","66.0","67.0"]},"orgProfiles":{"advertised":["default"],"passing":["default"]},"silentFallbacks":0,"unclassified":[],"ranges":{}}}`,
		status, gladeCommit, toolsCommit, shaBefore, shaAfter, cs, cp, cf, sp, sf)
	p := filepath.Join(dir, "verifier.json")
	os.WriteFile(p, []byte(j), 0644)
	return p
}

func mutateVerifierJSON(t *testing.T, source string, mutate func(map[string]any)) string {
	t.Helper()
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	var report map[string]any
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	mutate(report)
	path := filepath.Join(t.TempDir(), "verifier.json")
	data, err = json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	return path
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
	sourceScript := filepath.Join(toolsRoot(t), "scripts", "salesforce-correctness-gate.sh")
	script, err := os.ReadFile(sourceScript)
	if err != nil {
		t.Fatal(err)
	}
	f.tr, _ = makeGitRepo(t)
	f.sp = filepath.Join(f.tr, "scripts", "salesforce-correctness-gate.sh")
	if err := os.MkdirAll(filepath.Dir(f.sp), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.sp, script, 0o755); err != nil {
		t.Fatal(err)
	}
	runCmd(t, f.tr, "git", "add", "scripts/salesforce-correctness-gate.sh")
	runCmd(t, f.tr, "git", "commit", "-m", "add gate")
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
	for _, rel := range []string{"candidate-provenance.json", "product-tests.jsonl", "product-version-proof.json", "salesforce-verification.json", "corpus/summary.tsv", "SHA256SUMS.txt"} {
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
		"salesforce", "release",
		"--contract", "docs/fixtures/salesforce-release-contract.json",
		"--glade-root", f.gr,
		"--check",
		"salesforce", "verify",
		"--release-contract", "docs/fixtures/salesforce-release-contract.json",
		"--product-version-proof", od + "/product-version-proof.json",
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

func TestSalesforceCorrectnessGateRejectsUnboundBinaries(t *testing.T) {
	for _, test := range []struct {
		name string
		env  string
		want string
	}{
		{name: "glade", env: "TEST_FAKE_GLADE_REVISION", want: "glade candidate is not built from clean commit"},
		{name: "glade-tools", env: "TEST_FAKE_TOOLS_REVISION", want: "glade-tools binary is not built from clean commit"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(test.env, strings.Repeat("0", 40))
			f := newGateFixture(t)
			code, out := runGate(t, f.sp, f.fk, filepath.Join(t.TempDir(), "args.txt"), f.gr, f.gb, "test-org", filepath.Join(t.TempDir(), "evidence"), f.vj, f.cd, 0, 0)
			if code == 0 || !strings.Contains(out, test.want) {
				t.Fatalf("gate accepted unbound binary: code=%d output=%s", code, out)
			}
		})
	}
}

func TestSalesforceCorrectnessGateRejectsDirtySourceTrees(t *testing.T) {
	for _, name := range []string{"glade", "glade-tools"} {
		t.Run(name, func(t *testing.T) {
			f := newGateFixture(t)
			root := f.gr
			if name == "glade-tools" {
				root = f.tr
			}
			if err := os.WriteFile(filepath.Join(root, "dirty.txt"), []byte("dirty"), 0o644); err != nil {
				t.Fatal(err)
			}
			code, out := runGate(t, f.sp, f.fk, filepath.Join(t.TempDir(), "args.txt"), f.gr, f.gb, "test-org", filepath.Join(t.TempDir(), "evidence"), f.vj, f.cd, 0, 0)
			if code == 0 || !strings.Contains(out, name+" worktree must be clean") {
				t.Fatalf("gate accepted dirty source tree: code=%d output=%s", code, out)
			}
		})
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
	for _, rel := range []string{"candidate-provenance.json", "product-tests.jsonl", "product-version-proof.json", "salesforce-verification.json", "corpus/summary.tsv", "SHA256SUMS.txt"} {
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

func TestSalesforceCorrectnessGateRejectsOpenReleaseCompleteness(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"unclassified surface", func(report map[string]any) {
			report["releaseCompleteness"].(map[string]any)["surfaceDelta"].(map[string]any)["classified"] = float64(518)
		}},
		{"unproved behavior", func(report map[string]any) {
			report["releaseCompleteness"].(map[string]any)["behaviorDelta"].(map[string]any)["proved"] = float64(21)
		}},
		{"missing source version", func(report map[string]any) {
			report["releaseCompleteness"].(map[string]any)["sourceVersions"].(map[string]any)["passing"] = []any{"65.0", "66.0"}
		}},
		{"missing endpoint version", func(report map[string]any) {
			report["releaseCompleteness"].(map[string]any)["endpointVersions"].(map[string]any)["passing"] = []any{"65.0", "66.0", "67.0"}
		}},
		{"missing org profile", func(report map[string]any) {
			report["releaseCompleteness"].(map[string]any)["orgProfiles"].(map[string]any)["passing"] = []any{}
		}},
		{"silent fallback", func(report map[string]any) {
			report["releaseCompleteness"].(map[string]any)["silentFallbacks"] = float64(1)
		}},
		{"unrouted release note", func(report map[string]any) {
			report["releaseCompleteness"].(map[string]any)["changeInventory"].(map[string]any)["routed"] = float64(3989)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newGateFixture(t)
			f.vj = mutateVerifierJSON(t, f.vj, test.mutate)
			code, out := runGate(t, f.sp, f.fk, filepath.Join(t.TempDir(), "args.txt"), f.gr, f.gb, "test-org", filepath.Join(t.TempDir(), "evidence"), f.vj, f.cd, 0, 0)
			if code == 0 {
				t.Fatalf("gate accepted open release completeness: %s", out)
			}
		})
	}
}

func TestSalesforceCorrectnessGateGeneratedDriftFails(t *testing.T) {
	t.Setenv("TEST_RELEASE_EXIT", "1")
	f := newGateFixture(t)
	code, out := runGate(t, f.sp, f.fk, filepath.Join(t.TempDir(), "args.txt"), f.gr, f.gb, "test-org", filepath.Join(t.TempDir(), "evidence"), f.vj, f.cd, 0, 0)
	if code == 0 || !strings.Contains(out, "release generation check failed") {
		t.Fatalf("gate accepted generated drift: code=%d output=%s", code, out)
	}
}

func TestSalesforceCorrectnessGateProductTestFailureFails(t *testing.T) {
	t.Setenv("TEST_PRODUCT_TEST_EXIT", "1")
	f := newGateFixture(t)
	code, out := runGate(t, f.sp, f.fk, filepath.Join(t.TempDir(), "args.txt"), f.gr, f.gb, "test-org", filepath.Join(t.TempDir(), "evidence"), f.vj, f.cd, 0, 0)
	if code == 0 {
		t.Fatalf("gate accepted failed product tests: %s", out)
	}
}

func TestSalesforceCorrectnessGateProductProofHashMismatchFails(t *testing.T) {
	t.Setenv("TEST_TAMPER_PRODUCT_PROOF", "1")
	f := newGateFixture(t)
	code, out := runGate(t, f.sp, f.fk, filepath.Join(t.TempDir(), "args.txt"), f.gr, f.gb, "test-org", filepath.Join(t.TempDir(), "evidence"), f.vj, f.cd, 0, 0)
	if code == 0 || !strings.Contains(out, "product-version proof validation failed") {
		t.Fatalf("gate accepted tampered product proof: code=%d output=%s", code, out)
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
		`--alias glade-dev-hub \`, `sf org create scratch`, `--target-dev-hub glade-dev-hub`, `--alias "$SF_SCRATCH_ALIAS"`, `--name "$SF_SCRATCH_MARKER"`, "scripts/release-build.sh",
		"RELEASE_SHARED_PAYLOAD_ARCHIVE", "RELEASE_SHARED_PAYLOAD_SHA256",
		`glade_${VERSION}_linux_amd64.tar.gz`, "./cmd/glade-tools",
		`scripts/salesforce-correctness-gate.sh \
            "$TOOLS_BIN" \
            "$(realpath ../glade)" \
            "$GLADE_CANDIDATE" \
            "$SF_SCRATCH_ALIAS" \
            "$RUNNER_TEMP/salesforce-correctness-evidence/gate"`,
		`if: always()
        with:
          name: salesforce-correctness-evidence`, "retention-days: 90",
		`name: Delete scratch org`, `if: always()`, `sf org delete scratch`,
		`FROM ScratchOrgInfo WHERE OrgName`, `FROM ActiveScratchOrg WHERE ScratchOrg`,
		`--sobject ActiveScratchOrg`, `remaining ActiveScratchOrg residue`,
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
