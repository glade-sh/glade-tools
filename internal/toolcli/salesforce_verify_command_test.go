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

	"github.com/glade-sh/glade/tools/internal/apexrules"
	"github.com/glade-sh/glade/tools/internal/oracleprobe"
)

// fakeSalesforceVerifyDeps replaces all external I/O with controlled results
// so tests never touch a real Salesforce org, Glade binary, or Git repo.
type fakeSalesforceVerifyDeps struct {
	gitHeadFn       func(ctx context.Context, dir string) (string, error)
	gitIsDirtyFn    func(ctx context.Context, dir string) (bool, error)
	sfCompilerFn    func(ctx context.Context, targetOrg string, rules []apexrules.Rule) (map[string]apexrules.SalesforceResult, error)
	gladeCompilerFn func(ctx context.Context, binary string, rules []apexrules.Rule) (map[string]apexrules.Outcome, error)
	sfStdlibFn      func(ctx context.Context, targetOrg, workDir string, cases []oracleprobe.Case) (oracleprobe.Report, error)
	gladeStdlibFn   func(ctx context.Context, gladeBin, projectDir string, cases []oracleprobe.Case) (oracleprobe.Report, error)
	sfProjectFn     func(ctx context.Context, projectDir, targetOrg string, cases []oracleprobe.Case) (oracleprobe.Report, error)
	gladeProjectFn  func(ctx context.Context, gladeBin, projectDir string, cases []oracleprobe.Case) (oracleprobe.Report, error)
}

// Helper to produce a simple "all pass" stdlib report seeded from the given cases.
func allPassStdlibReport(cases []oracleprobe.Case) oracleprobe.Report {
	results := make([]oracleprobe.Result, len(cases))
	for i, c := range cases {
		v := "value-text"
		results[i] = oracleprobe.Result{
			ID:        c.ID,
			Area:      c.Area,
			API:       c.API,
			Mode:      c.Mode,
			HasValue:  true,
			Value:     &v,
			ValueType: c.ValueType,
		}
	}
	return oracleprobe.Report{Results: results}
}

// allPassProjectReport seeds twelve project results.
func allPassProjectReport(cases []oracleprobe.Case) oracleprobe.Report {
	results := make([]oracleprobe.Result, len(cases))
	for i, c := range cases {
		v := "pass"
		results[i] = oracleprobe.Result{
			ID:        c.ID,
			Area:      "Lifecycle",
			API:       "CorrectnessProbeTest." + c.Statements[0],
			Mode:      oracleprobe.ModeDeploy,
			HasValue:  true,
			Value:     &v,
			ValueType: "assertion",
		}
	}
	return oracleprobe.Report{Results: results}
}

func allPassDeps() *fakeSalesforceVerifyDeps {
	return &fakeSalesforceVerifyDeps{
		gitHeadFn: func(ctx context.Context, dir string) (string, error) {
			return "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", nil
		},
		gitIsDirtyFn: func(ctx context.Context, dir string) (bool, error) { return false, nil },
		sfCompilerFn: func(ctx context.Context, targetOrg string, rules []apexrules.Rule) (map[string]apexrules.SalesforceResult, error) {
			results := make(map[string]apexrules.SalesforceResult, len(rules))
			for _, rule := range rules {
				results[rule.ID] = apexrules.SalesforceResult{Outcome: rule.Oracle}
			}
			return results, nil
		},
		gladeCompilerFn: func(ctx context.Context, binary string, rules []apexrules.Rule) (map[string]apexrules.Outcome, error) {
			results := make(map[string]apexrules.Outcome, len(rules))
			for _, rule := range rules {
				results[rule.ID] = rule.Oracle
			}
			return results, nil
		},
		sfStdlibFn: func(ctx context.Context, targetOrg, workDir string, cases []oracleprobe.Case) (oracleprobe.Report, error) {
			return allPassStdlibReport(cases), nil
		},
		gladeStdlibFn: func(ctx context.Context, gladeBin, projectDir string, cases []oracleprobe.Case) (oracleprobe.Report, error) {
			return allPassStdlibReport(cases), nil
		},
		sfProjectFn: func(ctx context.Context, projectDir, targetOrg string, cases []oracleprobe.Case) (oracleprobe.Report, error) {
			return allPassProjectReport(cases), nil
		},
		gladeProjectFn: func(ctx context.Context, gladeBin, projectDir string, cases []oracleprobe.Case) (oracleprobe.Report, error) {
			return allPassProjectReport(cases), nil
		},
	}
}

// ----- helper functions for creating test fixtures -----

func writeTempFile(t *testing.T, dir, name string, content []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func makeCatalog(t *testing.T, dir string) string {
	t.Helper()
	content := []byte(`{
  "gladeCommit": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
  "rules": [
    {
      "id": "APEX-RULE-001",
      "area": "Compilation",
      "docsPath": "salesforce-docs-expanded-run/apex/apex_ref.md",
      "docsLines": "1-5",
      "apiVersion": 66.0,
      "sourceKind": "class",
      "source": "public class Test {}",
      "oracle": "accept",
      "owner": "compiler",
      "status": "supported",
      "productTest": "CompilerParityTest.compileAcceptsValidClass"
    },
    {
      "id": "APEX-RULE-002",
      "area": "Compilation",
      "docsPath": "salesforce-docs-expanded-run/apex/apex_ref.md",
      "docsLines": "6-10",
      "apiVersion": 67,
      "sourceKind": "class",
      "source": "public class Test { int x }",
      "oracle": "reject",
      "owner": "compiler",
      "status": "supported",
      "productTest": "CompilerParityTest.compileRejectsInvalidClass"
    }
  ]
}`)
	return writeTempFile(t, dir, "catalog.json", content)
}

func makeReleaseManifest(t *testing.T, dir string) string {
	t.Helper()
	content := []byte(`{
  "schemaVersion": 1,
  "release": "Summer '26",
  "apiVersion": "67.0",
  "digest": "0000000000000000000000000000000000000000000000000000000000000000",
  "acquisition": "test",
  "sourceFamilies": ["apex-reference"]
}`)
	return writeTempFile(t, dir, "release.json", content)
}

func makeRuntimeFixture(t *testing.T, dir string) string {
	t.Helper()
	content := []byte(`{"apiVersion":"67.0","comparisons":[{"caseId":"decimal-round-default-positive-tie","status":"inconclusive"}]}`)
	return writeTempFile(t, dir, "runtime-fixture.json", content)
}

func makeTestProject(t *testing.T, dir string) string {
	t.Helper()
	projectDir := filepath.Join(dir, "test-project")
	if err := os.MkdirAll(filepath.Join(projectDir, "force-app", "main", "default", "classes"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "sfdx-project.json"), []byte(`{"packageDirectories":[{"path":"force-app","default":true}],"sourceApiVersion":"67.0"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "force-app", "main", "default", "classes", "CorrectnessProbeTest.cls"), []byte("public class CorrectnessProbeTest {}"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "force-app", "main", "default", "classes", "CorrectnessProbeTest.cls-meta.xml"), []byte(`<?xml version="1.0" encoding="UTF-8"?>
<ApexClass xmlns="http://soap.sforce.com/2006/04/metadata">
    <apiVersion>67.0</apiVersion>
    <status>Active</status>
</ApexClass>`), 0644); err != nil {
		t.Fatal(err)
	}
	return projectDir
}

func makeCandidate(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "glade")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho fake-glade\n"), 0755); err != nil {
		t.Fatal(err)
	}
	return path
}

func makeTempGitRepo(t *testing.T, dir string) (string, string) {
	t.Helper()
	repoDir := filepath.Join(dir, "repo")
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatal(err)
	}
	cmds := [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "test@test.test"},
		{"config", "user.name", "test"},
	}
	for _, args := range cmds {
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s\n%s", args, out, err)
		}
	}
	// Create a file and commit so we have a real commit SHA
	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	addCmd := exec.Command("git", "add", "README.md")
	addCmd.Dir = repoDir
	if out, err := addCmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %s\n%s", out, err)
	}
	commitCmd := exec.Command("git", "commit", "--allow-empty", "-m", "init")
	commitCmd.Dir = repoDir
	if out, err := commitCmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %s\n%s", out, err)
	}
	revCmd := exec.Command("git", "rev-parse", "HEAD")
	revCmd.Dir = repoDir
	out, err := revCmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse: %v", err)
	}
	return repoDir, strings.TrimSpace(string(out))
}

func makeDirtyGitRepo(t *testing.T, dir string) string {
	t.Helper()
	repoDir := filepath.Join(dir, "dirty-repo")
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatal(err)
	}
	cmds := [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "test@test.test"},
		{"config", "user.name", "test"},
	}
	for _, args := range cmds {
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s\n%s", args, out, err)
		}
	}
	if err := os.WriteFile(filepath.Join(repoDir, "clean.txt"), []byte("clean\n"), 0644); err != nil {
		t.Fatal(err)
	}
	addCmd := exec.Command("git", "add", "clean.txt")
	addCmd.Dir = repoDir
	if out, err := addCmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %s\n%s", out, err)
	}
	commitCmd := exec.Command("git", "commit", "--allow-empty", "-m", "clean")
	commitCmd.Dir = repoDir
	if out, err := commitCmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %s\n%s", out, err)
	}
	// Make it dirty
	if err := os.WriteFile(filepath.Join(repoDir, "dirty.txt"), []byte("dirty\n"), 0644); err != nil {
		t.Fatal(err)
	}
	return repoDir
}

// ----- helper to invoke the verify core directly with deps -----

func runVerifyWithDeps(t *testing.T, opts salesforceVerifyOptions, deps *fakeSalesforceVerifyDeps) verifyReport {
	t.Helper()
	ctx := context.Background()
	report, err := executeSalesforceVerify(ctx, opts, &salesforceVerifyDeps{
		gitHead:    deps.gitHeadFn,
		gitIsDirty: deps.gitIsDirtyFn,
		runSFCompiler: func(ctx context.Context, targetOrg string, rules []apexrules.Rule) (map[string]apexrules.SalesforceResult, error) {
			return deps.sfCompilerFn(ctx, targetOrg, rules)
		},
		runGladeCompiler: func(ctx context.Context, binary string, rules []apexrules.Rule) (map[string]apexrules.Outcome, error) {
			return deps.gladeCompilerFn(ctx, binary, rules)
		},
		runSFStdlib: func(ctx context.Context, targetOrg, workDir string, cases []oracleprobe.Case) (oracleprobe.Report, error) {
			return deps.sfStdlibFn(ctx, targetOrg, workDir, cases)
		},
		runGladeStdlib: func(ctx context.Context, gladeBin, projectDir string, cases []oracleprobe.Case) (oracleprobe.Report, error) {
			return deps.gladeStdlibFn(ctx, gladeBin, projectDir, cases)
		},
		runSFProject: func(ctx context.Context, projectDir, targetOrg string, cases []oracleprobe.Case) (oracleprobe.Report, error) {
			return deps.sfProjectFn(ctx, projectDir, targetOrg, cases)
		},
		runGladeProject: func(ctx context.Context, gladeBin, projectDir string, cases []oracleprobe.Case) (oracleprobe.Report, error) {
			return deps.gladeProjectFn(ctx, gladeBin, projectDir, cases)
		},
	})
	if err != nil && report.Status == "" {
		t.Fatalf("executeSalesforceVerify: %v", err)
	}
	// Write the report artifact (same atomic write as the CLI path)
	if report.Status != "" {
		if err := writeReport(opts.Out, report); err != nil {
			t.Fatalf("writeReport: %v", err)
		}
	}
	return report
}

// ============ Scenario 1: Missing or duplicate flags fail before execution ============

func TestSalesforceVerify_MissingRequiredFlags(t *testing.T) {
	for _, missing := range []string{
		"--release-manifest", "--catalog", "--runtime-cases",
		"--test-project", "--target-org", "--glade-bin", "--glade-root", "--out",
	} {
		t.Run("missing "+missing, func(t *testing.T) {
			args := allVerifyFlags(t)
			args = removeFlag(args, missing)
			var stdout, stderr bytes.Buffer
			code := Run(context.Background(), append([]string{"salesforce", "verify"}, args...), &stdout, &stderr)
			if code == 0 {
				t.Fatalf("expected nonzero exit for missing %s", missing)
			}
			if !strings.Contains(strings.ToLower(stderr.String()), "required") {
				t.Fatalf("error should mention required flag, got: %q", stderr.String())
			}
		})
	}
}

func TestSalesforceVerify_DuplicateFlags(t *testing.T) {
	for _, flag := range []string{
		"--release-manifest", "--catalog", "--runtime-cases",
		"--test-project", "--target-org", "--glade-bin", "--glade-root", "--out",
	} {
		t.Run("duplicate "+flag, func(t *testing.T) {
			args := allVerifyFlags(t)
			args = append(args, flag, "duplicate-value")
			var stdout, stderr bytes.Buffer
			code := Run(context.Background(), append([]string{"salesforce", "verify"}, args...), &stdout, &stderr)
			if code == 0 {
				t.Fatalf("expected nonzero exit for duplicate %s", flag)
			}
			if !strings.Contains(strings.ToLower(stderr.String()), "duplicate") {
				t.Fatalf("error should mention duplicate, got: %q", stderr.String())
			}
		})
	}
}

func TestSalesforceVerify_UnknownFlag(t *testing.T) {
	args := allVerifyFlags(t)
	args = append(args, "--unknown-flag", "value")
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), append([]string{"salesforce", "verify"}, args...), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected nonzero exit for unknown flag")
	}
	if !strings.Contains(strings.ToLower(stderr.String()), "unknown") {
		t.Fatalf("error should mention unknown, got: %q", stderr.String())
	}
}

// ============ Helper: build all required flags from a temp directory ============

func allVerifyFlags(t *testing.T) []string {
	t.Helper()
	dir := t.TempDir()
	releasePath := makeReleaseManifest(t, dir)
	catalogPath := makeCatalog(t, dir)
	runtimePath := makeRuntimeFixture(t, dir)
	projectPath := makeTestProject(t, dir)
	candidatePath := makeCandidate(t, dir)
	outPath := filepath.Join(dir, "out", "report.json")
	return []string{
		"--release-manifest", releasePath,
		"--catalog", catalogPath,
		"--runtime-cases", runtimePath,
		"--test-project", projectPath,
		"--target-org", "test-org-alias",
		"--glade-bin", candidatePath,
		"--glade-root", dir,
		"--out", outPath,
	}
}

func removeFlag(args []string, flag string) []string {
	result := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		if args[i] == flag {
			i++ // skip value too
			continue
		}
		result = append(result, args[i])
	}
	return result
}

// ============ Scenario 2: Pass, behavioral fail, and operational inconclusive are distinct ============

func TestSalesforceVerify_StatusesAreDistinct(t *testing.T) {
	dir := t.TempDir()
	releasePath := makeReleaseManifest(t, dir)
	catalogPath := makeCatalog(t, dir)
	runtimePath := makeRuntimeFixture(t, dir)
	projectPath := makeTestProject(t, dir)
	candidatePath := makeCandidate(t, dir)
	outPath := filepath.Join(dir, "out", "report.json")

	// Pass: all runners succeed with matching reports
	passDeps := allPassDeps()
	passOpts := salesforceVerifyOptions{
		ReleaseManifest: releasePath,
		Catalog:         catalogPath,
		RuntimeCases:    runtimePath,
		TestProject:     projectPath,
		TargetOrg:       "test-org",
		GladeBin:        candidatePath,
		GladeRoot:       dir,
		Out:             outPath + ".pass.json",
	}
	passReport := runVerifyWithDeps(t, passOpts, passDeps)
	if passReport.Status != "pass" {
		t.Fatalf("expected pass, got %s", passReport.Status)
	}

	// Fail: runtime mismatch (SF returns different value than Glade)
	failDeps := allPassDeps()
	failDeps.sfStdlibFn = func(ctx context.Context, targetOrg, workDir string, cases []oracleprobe.Case) (oracleprobe.Report, error) {
		r := allPassStdlibReport(cases)
		if len(r.Results) > 0 {
			v := "different-value"
			r.Results[0].Value = &v
		}
		return r, nil
	}
	failOpts := salesforceVerifyOptions{
		ReleaseManifest: releasePath,
		Catalog:         catalogPath,
		RuntimeCases:    runtimePath,
		TestProject:     projectPath,
		TargetOrg:       "test-org",
		GladeBin:        candidatePath,
		GladeRoot:       dir,
		Out:             outPath + ".fail.json",
	}
	failReport := runVerifyWithDeps(t, failOpts, failDeps)
	if failReport.Status != "fail" {
		t.Fatalf("expected fail, got %s", failReport.Status)
	}

	// Inconclusive: runtime SF runner fails operationally
	inconclusiveDeps := allPassDeps()
	inconclusiveDeps.sfStdlibFn = func(ctx context.Context, targetOrg, workDir string, cases []oracleprobe.Case) (oracleprobe.Report, error) {
		return oracleprobe.Report{}, fmt.Errorf("sf command failed")
	}
	inconclusiveOpts := salesforceVerifyOptions{
		ReleaseManifest: releasePath,
		Catalog:         catalogPath,
		RuntimeCases:    runtimePath,
		TestProject:     projectPath,
		TargetOrg:       "test-org",
		GladeBin:        candidatePath,
		GladeRoot:       dir,
		Out:             outPath + ".inconclusive.json",
	}
	inconclusiveReport := runVerifyWithDeps(t, inconclusiveOpts, inconclusiveDeps)
	if inconclusiveReport.Status != "inconclusive" {
		t.Fatalf("expected inconclusive, got %s", inconclusiveReport.Status)
	}

	if passReport.Status == failReport.Status || failReport.Status == inconclusiveReport.Status || passReport.Status == inconclusiveReport.Status {
		t.Fatal("pass, fail, and inconclusive should be distinct")
	}
}

// ============ Scenario 3: Every required case appears exactly once ============

func TestSalesforceVerify_AllCasesPresentExactlyOnce(t *testing.T) {
	dir := t.TempDir()
	releasePath := makeReleaseManifest(t, dir)
	catalogPath := makeCatalog(t, dir)
	runtimePath := makeRuntimeFixture(t, dir)
	projectPath := makeTestProject(t, dir)
	candidatePath := makeCandidate(t, dir)
	outPath := filepath.Join(dir, "out", "report.json")

	deps := allPassDeps()
	opts := salesforceVerifyOptions{
		ReleaseManifest: releasePath,
		Catalog:         catalogPath,
		RuntimeCases:    runtimePath,
		TestProject:     projectPath,
		TargetOrg:       "test-org",
		GladeBin:        candidatePath,
		GladeRoot:       dir,
		Out:             outPath,
	}
	report := runVerifyWithDeps(t, opts, deps)

	// Compiler cases: 2 from the catalog
	if len(report.Compiler.Cases) != 2 {
		t.Fatalf("compiler cases: got %d, want 2", len(report.Compiler.Cases))
	}
	compilerIDs := make(map[string]bool)
	for _, c := range report.Compiler.Cases {
		if compilerIDs[c.ID] {
			t.Fatalf("duplicate compiler case ID %q", c.ID)
		}
		compilerIDs[c.ID] = true
	}
	if !compilerIDs["APEX-RULE-001"] || !compilerIDs["APEX-RULE-002"] {
		t.Fatalf("missing expected compiler case IDs: %v", compilerIDs)
	}

	// Runtime cases: all StdlibCases()
	stdlibCases := oracleprobe.StdlibCases()
	if len(report.Runtime.Cases) != len(stdlibCases) {
		t.Fatalf("runtime cases: got %d, want %d", len(report.Runtime.Cases), len(stdlibCases))
	}
	runtimeIDs := make(map[string]bool)
	for _, c := range report.Runtime.Cases {
		if runtimeIDs[c.ID] {
			t.Fatalf("duplicate runtime case ID %q", c.ID)
		}
		runtimeIDs[c.ID] = true
	}
	for _, c := range stdlibCases {
		if !runtimeIDs[c.ID] {
			t.Fatalf("missing runtime case ID %q", c.ID)
		}
	}

	// Lifecycle cases: all 12 ProjectOracleCases()
	projectCases := oracleprobe.ProjectOracleCases()
	if len(report.Lifecycle.Cases) != len(projectCases) {
		t.Fatalf("lifecycle cases: got %d, want %d", len(report.Lifecycle.Cases), len(projectCases))
	}
	lifecycleIDs := make(map[string]bool)
	for _, c := range report.Lifecycle.Cases {
		if lifecycleIDs[c.ID] {
			t.Fatalf("duplicate lifecycle case ID %q", c.ID)
		}
		lifecycleIDs[c.ID] = true
	}
	for _, c := range projectCases {
		if !lifecycleIDs[c.ID] {
			t.Fatalf("missing lifecycle case ID %q", c.ID)
		}
	}
}

// ============ Scenario 4: One section's behavioral failure does not stop later sections ============

func TestSalesforceVerify_CompilerFailureDoesNotStopRuntime(t *testing.T) {
	dir := t.TempDir()
	releasePath := makeReleaseManifest(t, dir)
	catalogPath := makeCatalog(t, dir)
	runtimePath := makeRuntimeFixture(t, dir)
	projectPath := makeTestProject(t, dir)
	candidatePath := makeCandidate(t, dir)
	outPath := filepath.Join(dir, "out", "report.json")

	deps := allPassDeps()
	// Make compiler fail: Glade returns a different outcome than SF
	deps.gladeCompilerFn = func(ctx context.Context, binary string, rules []apexrules.Rule) (map[string]apexrules.Outcome, error) {
		results := make(map[string]apexrules.Outcome, len(rules))
		for _, rule := range rules {
			// Flip the outcome so SF and Glade disagree
			if rule.Oracle == apexrules.OutcomeAccept {
				results[rule.ID] = apexrules.OutcomeReject
			} else {
				results[rule.ID] = apexrules.OutcomeAccept
			}
		}
		return results, nil
	}

	opts := salesforceVerifyOptions{
		ReleaseManifest: releasePath,
		Catalog:         catalogPath,
		RuntimeCases:    runtimePath,
		TestProject:     projectPath,
		TargetOrg:       "test-org",
		GladeBin:        candidatePath,
		GladeRoot:       dir,
		Out:             outPath,
	}
	report := runVerifyWithDeps(t, opts, deps)

	if len(report.Runtime.Cases) == 0 {
		t.Fatal("runtime section should have run even if compiler section failed")
	}
	if len(report.Lifecycle.Cases) == 0 {
		t.Fatal("lifecycle section should have run even if compiler section failed")
	}
}

func TestSalesforceVerify_RuntimeFailureDoesNotStopLifecycle(t *testing.T) {
	dir := t.TempDir()
	releasePath := makeReleaseManifest(t, dir)
	catalogPath := makeCatalog(t, dir)
	runtimePath := makeRuntimeFixture(t, dir)
	projectPath := makeTestProject(t, dir)
	candidatePath := makeCandidate(t, dir)
	outPath := filepath.Join(dir, "out", "report.json")

	deps := allPassDeps()
	// Runtime SF fails
	deps.sfStdlibFn = func(ctx context.Context, targetOrg, workDir string, cases []oracleprobe.Case) (oracleprobe.Report, error) {
		return oracleprobe.Report{}, fmt.Errorf("sf command failed")
	}

	opts := salesforceVerifyOptions{
		ReleaseManifest: releasePath,
		Catalog:         catalogPath,
		RuntimeCases:    runtimePath,
		TestProject:     projectPath,
		TargetOrg:       "test-org",
		GladeBin:        candidatePath,
		GladeRoot:       dir,
		Out:             outPath,
	}
	report := runVerifyWithDeps(t, opts, deps)

	if len(report.Lifecycle.Cases) == 0 {
		t.Fatal("lifecycle section should have run even after runtime failure")
	}
	if len(report.Runtime.Cases) == 0 {
		t.Fatal("runtime section should produce cases even when failing")
	}
}

// ============ Scenario 5: Operational failure marks required cases inconclusive ============

func TestSalesforceVerify_OperationalFailureMarksInconclusive(t *testing.T) {
	dir := t.TempDir()
	releasePath := makeReleaseManifest(t, dir)
	catalogPath := makeCatalog(t, dir)
	runtimePath := makeRuntimeFixture(t, dir)
	projectPath := makeTestProject(t, dir)
	candidatePath := makeCandidate(t, dir)
	outPath := filepath.Join(dir, "out", "report.json")

	deps := allPassDeps()
	// Simulate operational failure: SF stdlib command errors
	deps.sfStdlibFn = func(ctx context.Context, targetOrg, workDir string, cases []oracleprobe.Case) (oracleprobe.Report, error) {
		return oracleprobe.Report{}, fmt.Errorf("sf command failed")
	}

	opts := salesforceVerifyOptions{
		ReleaseManifest: releasePath,
		Catalog:         catalogPath,
		RuntimeCases:    runtimePath,
		TestProject:     projectPath,
		TargetOrg:       "test-org",
		GladeBin:        candidatePath,
		GladeRoot:       dir,
		Out:             outPath,
	}
	report := runVerifyWithDeps(t, opts, deps)

	for _, c := range report.Runtime.Cases {
		if c.Status != "inconclusive" {
			t.Fatalf("runtime case %q: expected inconclusive, got %s", c.ID, c.Status)
		}
	}
	// Later sections should still run
	if len(report.Lifecycle.Cases) == 0 {
		t.Fatal("lifecycle section should have run after runtime operational failure")
	}
}

// ============ Scenario 6: Candidate SHA256 recorded before and after all execution ============

func TestSalesforceVerify_CandidateHashesRecorded(t *testing.T) {
	dir := t.TempDir()
	releasePath := makeReleaseManifest(t, dir)
	catalogPath := makeCatalog(t, dir)
	runtimePath := makeRuntimeFixture(t, dir)
	projectPath := makeTestProject(t, dir)
	candidatePath := makeCandidate(t, dir)
	outPath := filepath.Join(dir, "out", "report.json")

	deps := allPassDeps()
	opts := salesforceVerifyOptions{
		ReleaseManifest: releasePath,
		Catalog:         catalogPath,
		RuntimeCases:    runtimePath,
		TestProject:     projectPath,
		TargetOrg:       "test-org",
		GladeBin:        candidatePath,
		GladeRoot:       dir,
		Out:             outPath,
	}
	report := runVerifyWithDeps(t, opts, deps)

	if report.Candidate.SHA256Before == "" {
		t.Fatal("candidate sha256Before must not be empty")
	}
	if report.Candidate.SHA256After == "" {
		t.Fatal("candidate sha256After must not be empty")
	}
	if report.Candidate.Path != "glade" {
		t.Fatalf("candidate path should be basename only, got %q", report.Candidate.Path)
	}

	// The hashes should match since the candidate didn't change
	if report.Candidate.SHA256Before != report.Candidate.SHA256After {
		t.Fatalf("sha256Before %s != sha256After %s", report.Candidate.SHA256Before, report.Candidate.SHA256After)
	}

	expectedHash := fmt.Sprintf("%x", sha256.Sum256(mustReadFile(t, candidatePath)))
	if report.Candidate.SHA256Before != expectedHash {
		t.Fatalf("sha256Before %s != expected %s", report.Candidate.SHA256Before, expectedHash)
	}
}

// ============ Scenario 7: A changed candidate cannot pass ============

func TestSalesforceVerify_ChangedCandidateFails(t *testing.T) {
	dir := t.TempDir()
	releasePath := makeReleaseManifest(t, dir)
	catalogPath := makeCatalog(t, dir)
	runtimePath := makeRuntimeFixture(t, dir)
	projectPath := makeTestProject(t, dir)
	candidatePath := makeCandidate(t, dir)
	outPath := filepath.Join(dir, "out", "report.json")

	// We need to corrupt the candidate during execution. We'll do this by
	// creating deps that modify the candidate file between the before/after hash.
	deps := allPassDeps()
	// Override one of the runners to modify the candidate during execution
	originalSFStdlib := deps.sfStdlibFn
	deps.sfStdlibFn = func(ctx context.Context, targetOrg, workDir string, cases []oracleprobe.Case) (oracleprobe.Report, error) {
		// Modify the candidate during execution
		if err := os.WriteFile(candidatePath, []byte("modified-content"), 0755); err != nil {
			t.Fatalf("failed to modify candidate: %v", err)
		}
		return originalSFStdlib(ctx, targetOrg, workDir, cases)
	}

	opts := salesforceVerifyOptions{
		ReleaseManifest: releasePath,
		Catalog:         catalogPath,
		RuntimeCases:    runtimePath,
		TestProject:     projectPath,
		TargetOrg:       "test-org",
		GladeBin:        candidatePath,
		GladeRoot:       dir,
		Out:             outPath,
	}
	report := runVerifyWithDeps(t, opts, deps)

	// Candidate changed - should not pass
	if report.Status == "pass" {
		t.Fatal("changed candidate should not pass")
	}
	if report.Candidate.SHA256Before == report.Candidate.SHA256After {
		t.Fatal("candidate before and after hashes should differ")
	}
}

// ============ Scenario 8: Input hashes are recorded ============

func TestSalesforceVerify_InputHashesRecorded(t *testing.T) {
	dir := t.TempDir()
	releasePath := makeReleaseManifest(t, dir)
	catalogPath := makeCatalog(t, dir)
	runtimePath := makeRuntimeFixture(t, dir)
	projectPath := makeTestProject(t, dir)
	candidatePath := makeCandidate(t, dir)
	outPath := filepath.Join(dir, "out", "report.json")

	deps := allPassDeps()
	opts := salesforceVerifyOptions{
		ReleaseManifest: releasePath,
		Catalog:         catalogPath,
		RuntimeCases:    runtimePath,
		TestProject:     projectPath,
		TargetOrg:       "test-org",
		GladeBin:        candidatePath,
		GladeRoot:       dir,
		Out:             outPath,
	}
	report := runVerifyWithDeps(t, opts, deps)

	if len(report.Inputs) != 4 {
		t.Fatalf("expected 4 inputs, got %d", len(report.Inputs))
	}
	kindMap := make(map[string]string)
	for _, input := range report.Inputs {
		if input.SHA256 == "" {
			t.Fatalf("input %s has empty sha256", input.Kind)
		}
		kindMap[input.Kind] = input.SHA256
	}
	for _, kind := range []string{"release-manifest", "compiler-catalog", "runtime-cases", "test-project"} {
		if _, ok := kindMap[kind]; !ok {
			t.Fatalf("missing input kind %q", kind)
		}
	}
}

// ============ Scenario 9: Test-project hashing ignores .glade cache ============

func TestSalesforceVerify_TestProjectHashingIgnoresGladeCache(t *testing.T) {
	dir := t.TempDir()

	// Create two identical projects, one with .glade dir
	projectA := filepath.Join(dir, "project-a")
	projectB := filepath.Join(dir, "project-b")
	for _, proj := range []string{projectA, projectB} {
		if err := os.MkdirAll(filepath.Join(proj, "force-app", "main", "default", "classes"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(proj, "sfdx-project.json"), []byte(`{"packageDirectories":[{"path":"force-app","default":true}]}`), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(proj, "force-app", "main", "default", "classes", "CorrectnessProbeTest.cls"), []byte("public class CorrectnessProbeTest {}"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	// Add .glade cache in project A
	if err := os.MkdirAll(filepath.Join(projectA, ".glade", "cache"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectA, ".glade", "cache", "data.bin"), []byte("cache-data"), 0644); err != nil {
		t.Fatal(err)
	}
	// Add .git in both (should be ignored too)
	if err := os.MkdirAll(filepath.Join(projectA, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(projectB, ".git"), 0755); err != nil {
		t.Fatal(err)
	}

	hashA, err := hashTestProjectDir(projectA)
	if err != nil {
		t.Fatal(err)
	}
	hashB, err := hashTestProjectDir(projectB)
	if err != nil {
		t.Fatal(err)
	}

	if hashA != hashB {
		t.Fatalf("project hashes should match despite .glade cache: A=%s B=%s", hashA, hashB)
	}
}

// ============ Scenario 10: Git commits and dirty states recorded ============

func TestSalesforceVerify_GitCommitsRecorded(t *testing.T) {
	dir := t.TempDir()
	releasePath := makeReleaseManifest(t, dir)
	catalogPath := makeCatalog(t, dir)
	runtimePath := makeRuntimeFixture(t, dir)
	projectPath := makeTestProject(t, dir)
	candidatePath := makeCandidate(t, dir)
	outPath := filepath.Join(dir, "out", "report.json")

	// Create clean git repos
	gladeRepo, gladeSha := makeTempGitRepo(t, dir)

	deps := &fakeSalesforceVerifyDeps{
		gitHeadFn: func(ctx context.Context, repoDir string) (string, error) {
			if strings.Contains(repoDir, "glade-root") || repoDir == gladeRepo {
				return gladeSha, nil
			}
			return "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", nil
		},
		gitIsDirtyFn: func(ctx context.Context, repoDir string) (bool, error) {
			return false, nil
		},
		sfCompilerFn: func(ctx context.Context, targetOrg string, rules []apexrules.Rule) (map[string]apexrules.SalesforceResult, error) {
			results := make(map[string]apexrules.SalesforceResult, len(rules))
			for _, rule := range rules {
				results[rule.ID] = apexrules.SalesforceResult{Outcome: rule.Oracle}
			}
			return results, nil
		},
		gladeCompilerFn: func(ctx context.Context, binary string, rules []apexrules.Rule) (map[string]apexrules.Outcome, error) {
			results := make(map[string]apexrules.Outcome, len(rules))
			for _, rule := range rules {
				results[rule.ID] = rule.Oracle
			}
			return results, nil
		},
		sfStdlibFn: func(ctx context.Context, targetOrg, workDir string, cases []oracleprobe.Case) (oracleprobe.Report, error) {
			return allPassStdlibReport(cases), nil
		},
		gladeStdlibFn: func(ctx context.Context, gladeBin, projectDir string, cases []oracleprobe.Case) (oracleprobe.Report, error) {
			return allPassStdlibReport(cases), nil
		},
		sfProjectFn: func(ctx context.Context, projectDir, targetOrg string, cases []oracleprobe.Case) (oracleprobe.Report, error) {
			return allPassProjectReport(cases), nil
		},
		gladeProjectFn: func(ctx context.Context, gladeBin, projectDir string, cases []oracleprobe.Case) (oracleprobe.Report, error) {
			return allPassProjectReport(cases), nil
		},
	}

	opts := salesforceVerifyOptions{
		ReleaseManifest: releasePath,
		Catalog:         catalogPath,
		RuntimeCases:    runtimePath,
		TestProject:     projectPath,
		TargetOrg:       "test-org",
		GladeBin:        candidatePath,
		GladeRoot:       gladeRepo,
		Out:             outPath,
	}
	report := runVerifyWithDeps(t, opts, deps)

	if report.Glade.Commit == "" {
		t.Fatal("glade commit should be recorded")
	}
	if report.GladeTools.Commit == "" {
		t.Fatal("glade-tools commit should be recorded")
	}
	if report.Glade.Dirty {
		t.Fatal("clean glade repo should not be marked dirty")
	}
	if report.GladeTools.Dirty {
		t.Fatal("clean glade-tools repo should not be marked dirty")
	}
}

// ============ Scenario 11: Release mode rejects dirty source root ============

func TestSalesforceVerify_ReleaseModeRejectsDirtyRoot(t *testing.T) {
	dir := t.TempDir()
	dirtyRepo := makeDirtyGitRepo(t, dir)
	releasePath := makeReleaseManifest(t, dir)
	catalogPath := makeCatalog(t, dir)
	runtimePath := makeRuntimeFixture(t, dir)
	projectPath := makeTestProject(t, dir)
	candidatePath := makeCandidate(t, dir)
	outPath := filepath.Join(dir, "out", "report.json")

	var stdout, stderr bytes.Buffer
	// Use the real Run to test flag parsing with dirty root
	args := []string{
		"salesforce", "verify",
		"--release-manifest", releasePath,
		"--catalog", catalogPath,
		"--runtime-cases", runtimePath,
		"--test-project", projectPath,
		"--target-org", "test-org",
		"--glade-bin", candidatePath,
		"--glade-root", dirtyRepo,
		"--out", outPath,
	}
	code := Run(context.Background(), args, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected nonzero exit for dirty root in release mode, stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	errText := strings.ToLower(stderr.String())
	if !strings.Contains(errText, "dirty") || !strings.Contains(errText, "release") {
		t.Fatalf("error should mention dirty and release mode: %q", stderr.String())
	}
}

// ============ Scenario 12: Developer mode permits dirty roots ============

func TestSalesforceVerify_DeveloperModePermitsDirtyRoots(t *testing.T) {
	dir := t.TempDir()
	dirtyRepo := makeDirtyGitRepo(t, dir)
	releasePath := makeReleaseManifest(t, dir)
	catalogPath := makeCatalog(t, dir)
	runtimePath := makeRuntimeFixture(t, dir)
	projectPath := makeTestProject(t, dir)
	candidatePath := makeCandidate(t, dir)
	outPath := filepath.Join(dir, "out", "report.json")

	deps := allPassDeps()
	deps.gitHeadFn = func(ctx context.Context, repoDir string) (string, error) {
		return "cccccccccccccccccccccccccccccccccccccccc", nil
	}
	deps.gitIsDirtyFn = func(ctx context.Context, repoDir string) (bool, error) {
		if strings.Contains(repoDir, "dirty-repo") || repoDir == dirtyRepo {
			return true, nil
		}
		return false, nil
	}

	opts := salesforceVerifyOptions{
		ReleaseManifest: releasePath,
		Catalog:         catalogPath,
		RuntimeCases:    runtimePath,
		TestProject:     projectPath,
		TargetOrg:       "test-org",
		GladeBin:        candidatePath,
		GladeRoot:       dirtyRepo,
		Out:             outPath,
		Developer:       true,
	}
	report := runVerifyWithDeps(t, opts, deps)

	if !report.Glade.Dirty {
		t.Fatal("dirty glade root should be recorded as dirty in developer mode")
	}
}

// ============ Scenario 13: Malformed/missing input, zero required cases, etc. cannot pass ============

func TestSalesforceVerify_MalformedReleaseManifestFails(t *testing.T) {
	dir := t.TempDir()
	releasePath := writeTempFile(t, dir, "bad-release.json", []byte("not json"))
	catalogPath := makeCatalog(t, dir)
	runtimePath := makeRuntimeFixture(t, dir)
	projectPath := makeTestProject(t, dir)
	candidatePath := makeCandidate(t, dir)
	outPath := filepath.Join(dir, "out", "report.json")

	var stdout, stderr bytes.Buffer
	args := []string{
		"salesforce", "verify",
		"--release-manifest", releasePath,
		"--catalog", catalogPath,
		"--runtime-cases", runtimePath,
		"--test-project", projectPath,
		"--target-org", "test-org",
		"--glade-bin", candidatePath,
		"--glade-root", dir,
		"--out", outPath,
	}
	code := Run(context.Background(), args, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected nonzero exit for malformed manifest")
	}
}

func TestSalesforceVerify_MissingInputFileFails(t *testing.T) {
	dir := t.TempDir()
	catalogPath := makeCatalog(t, dir)
	runtimePath := makeRuntimeFixture(t, dir)
	projectPath := makeTestProject(t, dir)
	candidatePath := makeCandidate(t, dir)
	outPath := filepath.Join(dir, "out", "report.json")

	var stdout, stderr bytes.Buffer
	args := []string{
		"salesforce", "verify",
		"--release-manifest", filepath.Join(dir, "nonexistent.json"),
		"--catalog", catalogPath,
		"--runtime-cases", runtimePath,
		"--test-project", projectPath,
		"--target-org", "test-org",
		"--glade-bin", candidatePath,
		"--glade-root", dir,
		"--out", outPath,
	}
	code := Run(context.Background(), args, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected nonzero exit for missing input file")
	}
}

// ============ Scenario 14: Artifact redacts credentials ============

func TestSalesforceVerify_ArtifactRedactsCredentials(t *testing.T) {
	dir := t.TempDir()
	releasePath := makeReleaseManifest(t, dir)
	catalogPath := makeCatalog(t, dir)
	runtimePath := makeRuntimeFixture(t, dir)
	projectPath := makeTestProject(t, dir)
	candidatePath := makeCandidate(t, dir)
	outPath := filepath.Join(dir, "out", "report.json")

	deps := allPassDeps()
	opts := salesforceVerifyOptions{
		ReleaseManifest: releasePath,
		Catalog:         catalogPath,
		RuntimeCases:    runtimePath,
		TestProject:     projectPath,
		TargetOrg:       "my-org-alias",
		GladeBin:        candidatePath,
		GladeRoot:       dir,
		Out:             outPath,
	}
	report := runVerifyWithDeps(t, opts, deps)

	// Check that the report JSON contains no credentials
	var raw map[string]interface{}
	reportJSON, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(reportJSON, &raw); err != nil {
		t.Fatal(err)
	}
	reportStr := string(reportJSON)
	for _, banned := range []string{
		"my-org-alias",
		"username",
		"orgId",
		"orgID",
		"accessToken",
		"auth",
	} {
		if strings.Contains(strings.ToLower(reportStr), strings.ToLower(banned)) {
			t.Fatalf("report contains banned term %q", banned)
		}
	}
}

// ============ Scenario 15: Atomic write ============

func TestSalesforceVerify_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	releasePath := makeReleaseManifest(t, dir)
	catalogPath := makeCatalog(t, dir)
	runtimePath := makeRuntimeFixture(t, dir)
	projectPath := makeTestProject(t, dir)
	candidatePath := makeCandidate(t, dir)

	outDir := filepath.Join(dir, "out")
	outPath := filepath.Join(outDir, "report.json")

	deps := allPassDeps()
	opts := salesforceVerifyOptions{
		ReleaseManifest: releasePath,
		Catalog:         catalogPath,
		RuntimeCases:    runtimePath,
		TestProject:     projectPath,
		TargetOrg:       "test-org",
		GladeBin:        candidatePath,
		GladeRoot:       dir,
		Out:             outPath,
	}
	report := runVerifyWithDeps(t, opts, deps)

	// Output should exist at the expected path
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("output file not found: %v", err)
	}
	var decoded verifyReport
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if decoded.Status != "pass" {
		t.Fatalf("expected pass, got %s", decoded.Status)
	}

	_ = report
	// No temporary files should remain in the output directory
	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") || strings.Contains(entry.Name(), "tmp") {
			t.Fatalf("temporary file left behind: %s", entry.Name())
		}
	}
}

// ============ Scenario 16: Output-write failure leaves no success artifact ============

// ============ Scenario 18: CLI help and plugin manifest expose exactly salesforce verify ============

func TestSalesforceVerify_HelpExposesCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"salesforce", "verify", "--help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected zero exit for help, got %d, stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "salesforce verify") {
		t.Fatalf("help should mention 'salesforce verify':\n%s", out)
	}
	for _, flag := range []string{"--release-manifest", "--catalog", "--runtime-cases", "--test-project", "--target-org", "--glade-bin", "--glade-root", "--out"} {
		if !strings.Contains(out, flag) {
			t.Fatalf("help should include %s:\n%s", flag, out)
		}
	}
}

func TestSalesforceVerify_ManifestIncludesCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"manifest", "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run returned %d, stderr=%s", code, stderr.String())
	}

	var manifest struct {
		Commands []struct {
			Path    []string `json:"path"`
			Summary string   `json:"summary"`
		} `json:"commands"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &manifest); err != nil {
		t.Fatalf("manifest is not JSON: %v", err)
	}

	found := false
	for _, cmd := range manifest.Commands {
		if len(cmd.Path) == 2 && cmd.Path[0] == "salesforce" && cmd.Path[1] == "verify" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("manifest does not include 'salesforce verify'")
	}
}

// ============ Additional tests for corrected behavior ============

// P0: dirty detection with only an untracked file
func TestSalesforceVerify_DirtyUntrackedFile(t *testing.T) {
	dir := t.TempDir()
	repoDir := filepath.Join(dir, "untracked-repo")
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "test@test.test"},
		{"config", "user.name", "test"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		if err := cmd.Run(); err != nil {
			t.Skipf("git %v failed: %v", args, err)
		}
	}
	// Commit one file so we have a HEAD
	if err := os.WriteFile(filepath.Join(repoDir, "tracked.txt"), []byte("tracked\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"add", "tracked.txt"},
		{"commit", "--allow-empty", "-m", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s\n%s", args, out, err)
		}
	}
	// Add only an untracked file — no staged changes, no modified tracked files
	if err := os.WriteFile(filepath.Join(repoDir, "untracked.txt"), []byte("untracked\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// realGitIsDirty should detect this as dirty
	dirty, err := realGitIsDirty(context.Background(), repoDir)
	if err != nil {
		t.Fatalf("realGitIsDirty error: %v", err)
	}
	if !dirty {
		t.Fatal("expected dirty with untracked file")
	}
}

// P0: git error propagates
func TestSalesforceVerify_GitErrorPropagates(t *testing.T) {
	dir := t.TempDir()
	nonRepo := filepath.Join(dir, "not-a-repo")
	if err := os.MkdirAll(nonRepo, 0755); err != nil {
		t.Fatal(err)
	}

	_, err := realGitHead(context.Background(), nonRepo)
	if err == nil {
		t.Fatal("expected error from non-git directory")
	}

	_, err = realGitIsDirty(context.Background(), nonRepo)
	if err == nil {
		t.Fatal("expected error from non-git directory")
	}
}

// P0: catalog oracle drift — SF and Glade agree but both differ from catalog
func TestSalesforceVerify_CatalogOracleDriftFails(t *testing.T) {
	dir := t.TempDir()
	releasePath := makeReleaseManifest(t, dir)
	catalogPath := makeCatalog(t, dir)
	runtimePath := makeRuntimeFixture(t, dir)
	projectPath := makeTestProject(t, dir)
	candidatePath := makeCandidate(t, dir)

	// The catalog says rule APEX-RULE-002 expects "reject".
	// Make both SF and Glade return "accept" — they agree with each other
	// but both differ from the catalog oracle. This is the three-way drift.
	deps := allPassDeps()
	deps.sfCompilerFn = func(ctx context.Context, targetOrg string, rules []apexrules.Rule) (map[string]apexrules.SalesforceResult, error) {
		results := make(map[string]apexrules.SalesforceResult, len(rules))
		for _, rule := range rules {
			results[rule.ID] = apexrules.SalesforceResult{Outcome: apexrules.OutcomeAccept}
		}
		return results, nil
	}
	deps.gladeCompilerFn = func(ctx context.Context, binary string, rules []apexrules.Rule) (map[string]apexrules.Outcome, error) {
		results := make(map[string]apexrules.Outcome, len(rules))
		for _, rule := range rules {
			results[rule.ID] = apexrules.OutcomeAccept
		}
		return results, nil
	}

	outPath := filepath.Join(dir, "out", "report.json")
	opts := salesforceVerifyOptions{
		ReleaseManifest: releasePath, Catalog: catalogPath, RuntimeCases: runtimePath,
		TestProject: projectPath, TargetOrg: "test-org", GladeBin: candidatePath,
		GladeRoot: dir, Out: outPath,
	}
	report := runVerifyWithDeps(t, opts, deps)

	if report.Compiler.OracleDrift != true {
		t.Fatal("expected oracleDrift to be true when SF differs from catalog")
	}
	if report.Compiler.Status != "fail" {
		t.Fatalf("compiler section should be fail due to oracle drift, got %s", report.Compiler.Status)
	}
	// APEX-RULE-002: catalog oracle is "reject", both SF and Glade returned "accept" → drift → fail
	drifter := findCase(report.Compiler.Cases, "APEX-RULE-002")
	if drifter == nil || drifter.Status != "fail" {
		t.Fatalf("APEX-RULE-002 should be fail due to oracle drift, got %v", drifter)
	}
	if report.Summary.Fail == 0 {
		t.Fatal("overall summary should have failures from oracle drift")
	}
	if report.Status != "fail" {
		t.Fatalf("overall status should be fail due to oracle drift, got %s", report.Status)
	}
}

func findCase(cases []verifyCase, id string) *verifyCase {
	for i := range cases {
		if cases[i].ID == id {
			return &cases[i]
		}
	}
	return nil
}

// P0: invalid-data tests — report validation blocks pass
// P1: Exit codes tested through CLI path with injected deps
func TestSalesforceVerify_ExitCodesThroughCLI(t *testing.T) {
	dir := t.TempDir()
	releasePath := makeReleaseManifest(t, dir)
	catalogPath := makeCatalog(t, dir)
	runtimePath := makeRuntimeFixture(t, dir)
	projectPath := makeTestProject(t, dir)
	candidatePath := makeCandidate(t, dir)

	makeCLIOpts := func(outPath string) salesforceVerifyOptions {
		return salesforceVerifyOptions{
			ReleaseManifest: releasePath, Catalog: catalogPath, RuntimeCases: runtimePath,
			TestProject: projectPath, TargetOrg: "test-org", GladeBin: candidatePath,
			GladeRoot: dir, Out: outPath, Developer: true,
		}
	}

	// Pass: exit 0
	passOut := filepath.Join(dir, "pass.json")
	passOpts := makeCLIOpts(passOut)
	code := runVerifyAndExit(t, passOpts, allPassDeps())
	if code != 0 {
		t.Fatalf("pass exit code: got %d, want 0", code)
	}

	// Fail: exit 1
	failOut := filepath.Join(dir, "fail.json")
	failDeps := allPassDeps()
	failDeps.sfStdlibFn = func(ctx context.Context, targetOrg, workDir string, cases []oracleprobe.Case) (oracleprobe.Report, error) {
		r := allPassStdlibReport(cases)
		if len(r.Results) > 0 {
			different := "mismatch"
			r.Results[0].Value = &different
		}
		return r, nil
	}
	failOpts := makeCLIOpts(failOut)
	code = runVerifyAndExit(t, failOpts, failDeps)
	if code == 0 {
		t.Fatal("fail exit code: got 0, want nonzero")
	}

	// Inconclusive: exit 1
	inconOut := filepath.Join(dir, "inconclusive.json")
	inconDeps := allPassDeps()
	inconDeps.sfStdlibFn = func(ctx context.Context, targetOrg, workDir string, cases []oracleprobe.Case) (oracleprobe.Report, error) {
		return oracleprobe.Report{}, fmt.Errorf("sf command failed")
	}
	inconOpts := makeCLIOpts(inconOut)
	code = runVerifyAndExit(t, inconOpts, inconDeps)
	if code == 0 {
		t.Fatal("inconclusive exit code: got 0, want nonzero")
	}
}

// P1: Reliable write-failure test — write to a path inside a non-existent parent
// whose creation is controlled. We use an empty file as the parent to make os.MkdirAll fail.
func TestSalesforceVerify_WriteFailureNoArtifact(t *testing.T) {
	dir := t.TempDir()
	// Create a regular file where a directory is needed
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("block"), 0644); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(blocker, "report.json") // blocker is a file, not a dir

	minimal := verifyReport{
		SchemaVersion: 1, Status: "pass", Release: "t", APIVersion: "v",
		Compiler:  verifySection{Status: "pass", Cases: []verifyCase{{ID: "c1", Status: "pass", SalesforceObservation: "sf", GladeObservation: "gl"}}, Summary: verifySummary{Pass: 1}},
		Runtime:   verifySection{Status: "pass", Cases: []verifyCase{{ID: "r1", Status: "pass", SalesforceObservation: "sf", GladeObservation: "gl"}}, Summary: verifySummary{Pass: 1}},
		Lifecycle: verifySection{Status: "pass", Cases: []verifyCase{{ID: "l1", Status: "pass", SalesforceObservation: "sf", GladeObservation: "gl"}}, Summary: verifySummary{Pass: 1}},
		Summary:   verifySummary{Pass: 3},
	}
	err := writeReport(outPath, minimal)
	if err == nil {
		t.Fatal("expected write failure when output parent is a file")
	}
	if _, statErr := os.Stat(outPath); statErr == nil {
		t.Fatal("artifact should not exist after write failure")
	}
}

// writeReport rejects structurally invalid reports (closure finding #2)
func TestSalesforceVerify_WriteReportRejectsInvalidReports(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "report.json")

	for _, tt := range []struct {
		name   string
		report verifyReport
	}{
		{"zero cases", verifyReport{SchemaVersion: 1, Status: "pass", Release: "t", APIVersion: "66.0",
			Compiler: verifySection{Status: "pass", Cases: []verifyCase{}}}},
		{"duplicate IDs", verifyReport{SchemaVersion: 1, Status: "pass", Release: "t", APIVersion: "66.0",
			Compiler: verifySection{Status: "pass", Cases: []verifyCase{
				{ID: "dup", Status: "pass", SalesforceObservation: "sf", GladeObservation: "gl"},
				{ID: "dup", Status: "pass", SalesforceObservation: "sf", GladeObservation: "gl"},
			}, Summary: verifySummary{Pass: 2}}}},
		{"invalid status", verifyReport{SchemaVersion: 1, Status: "bogus", Release: "t", APIVersion: "66.0",
			Compiler: verifySection{Status: "pass", Cases: []verifyCase{{ID: "c1", Status: "pass", SalesforceObservation: "sf", GladeObservation: "gl"}}, Summary: verifySummary{Pass: 1}}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			// Minimal sections to pass the section-level checks for other sections
			minSection := verifySection{Status: "pass", Cases: []verifyCase{{ID: "x", Status: "pass", SalesforceObservation: "sf", GladeObservation: "gl"}}, Summary: verifySummary{Pass: 1}}
			if tt.report.Compiler.Cases == nil {
				tt.report.Compiler = minSection
			}
			if tt.report.Runtime.Cases == nil {
				tt.report.Runtime = minSection
			}
			if tt.report.Lifecycle.Cases == nil {
				tt.report.Lifecycle = minSection
			}
			if tt.report.Summary.Pass == 0 {
				tt.report.Summary = verifySummary{Pass: 3}
			}
			err := writeReport(outPath, tt.report)
			if err == nil {
				t.Fatal("expected writeReport to reject invalid report")
			}
			if _, statErr := os.Stat(outPath); statErr == nil {
				t.Fatal("artifact should not exist after rejected write")
			}
		})
	}
}

// Acquired result identity validation — duplicate/missing/unknown IDs block comparison
func TestSalesforceVerify_AcquiredResultIdentityBlocksPass(t *testing.T) {
	dir := t.TempDir()
	releasePath := makeReleaseManifest(t, dir)
	catalogPath := makeCatalog(t, dir)
	runtimePath := makeRuntimeFixture(t, dir)
	projectPath := makeTestProject(t, dir)
	candidatePath := makeCandidate(t, dir)

	runtimeCases := oracleprobe.StdlibCases()
	lifecycleCases := oracleprobe.ProjectOracleCases()

	// Duplicate IDs in SF runtime report
	t.Run("duplicate runtime result", func(t *testing.T) {
		deps := allPassDeps()
		deps.sfStdlibFn = func(ctx context.Context, targetOrg, workDir string, cases []oracleprobe.Case) (oracleprobe.Report, error) {
			r := allPassStdlibReport(cases)
			if len(r.Results) >= 2 {
				r.Results[1] = r.Results[0] // duplicate
			}
			return r, nil
		}
		outPath := filepath.Join(dir, "dup-runtime.json")
		opts := salesforceVerifyOptions{ReleaseManifest: releasePath, Catalog: catalogPath, RuntimeCases: runtimePath,
			TestProject: projectPath, TargetOrg: "test-org", GladeBin: candidatePath, GladeRoot: dir, Out: outPath}
		report := runVerifyWithDeps(t, opts, deps)
		for _, c := range report.Runtime.Cases {
			if c.Status != "inconclusive" {
				t.Fatalf("duplicate runtime result should mark cases inconclusive, got %s", c.Status)
			}
		}
	})

	// Missing IDs in Glade lifecycle report
	t.Run("missing lifecycle result", func(t *testing.T) {
		deps := allPassDeps()
		deps.gladeProjectFn = func(ctx context.Context, gladeBin, projectDir string, cases []oracleprobe.Case) (oracleprobe.Report, error) {
			r := allPassProjectReport(cases)
			r.Results = r.Results[1:] // drop first
			return r, nil
		}
		outPath := filepath.Join(dir, "missing-lifecycle.json")
		opts := salesforceVerifyOptions{ReleaseManifest: releasePath, Catalog: catalogPath, RuntimeCases: runtimePath,
			TestProject: projectPath, TargetOrg: "test-org", GladeBin: candidatePath, GladeRoot: dir, Out: outPath}
		report := runVerifyWithDeps(t, opts, deps)
		if report.Lifecycle.Status != "inconclusive" {
			t.Fatalf("missing lifecycle result should make section inconclusive, got %s", report.Lifecycle.Status)
		}
	})

	// Unknown extra ID in SF runtime report
	t.Run("unknown runtime result", func(t *testing.T) {
		deps := allPassDeps()
		deps.sfStdlibFn = func(ctx context.Context, targetOrg, workDir string, cases []oracleprobe.Case) (oracleprobe.Report, error) {
			r := allPassStdlibReport(cases)
			r.Results[0].ID = "UNKNOWN-EXTRA-ID"
			return r, nil
		}
		outPath := filepath.Join(dir, "unknown-runtime.json")
		opts := salesforceVerifyOptions{ReleaseManifest: releasePath, Catalog: catalogPath, RuntimeCases: runtimePath,
			TestProject: projectPath, TargetOrg: "test-org", GladeBin: candidatePath, GladeRoot: dir, Out: outPath}
		report := runVerifyWithDeps(t, opts, deps)
		if report.Runtime.Status != "inconclusive" {
			t.Fatalf("unknown runtime result should make section inconclusive, got %s", report.Runtime.Status)
		}
	})

	_ = lifecycleCases
	_ = runtimeCases
}

// Table-drive the malformed-report validation checks
func TestSalesforceVerify_ReportValidationTable(t *testing.T) {
	minSection := verifySection{Status: "pass", Cases: []verifyCase{{ID: "x", Status: "pass", SalesforceObservation: "sf-obs", GladeObservation: "gl-obs"}}, Summary: verifySummary{Pass: 1}}
	for _, tt := range []struct {
		name string
		r    verifyReport
		want string
	}{
		{"zero cases", verifyReport{SchemaVersion: 1, Status: "pass", Release: "t", APIVersion: "v",
			Compiler: verifySection{Status: "pass", Cases: []verifyCase{}}}, "zero"},
		{"duplicate IDs", verifyReport{SchemaVersion: 1, Status: "pass", Release: "t", APIVersion: "v",
			Compiler: verifySection{Status: "pass", Cases: []verifyCase{
				{ID: "d", Status: "pass", SalesforceObservation: "sf", GladeObservation: "gl"},
				{ID: "d", Status: "pass", SalesforceObservation: "sf", GladeObservation: "gl"},
			}, Summary: verifySummary{Pass: 2}}}, "duplicate"},
		{"invalid status", verifyReport{SchemaVersion: 1, Status: "nope", Release: "t", APIVersion: "v",
			Compiler: minSection}, "invalid"},
		{"summary mismatch", verifyReport{SchemaVersion: 1, Status: "pass", Release: "t", APIVersion: "v",
			Compiler: minSection, Summary: verifySummary{Pass: 999}}, "mismatch"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.r.Runtime.Cases == nil {
				tt.r.Runtime = minSection
			}
			if tt.r.Lifecycle.Cases == nil {
				tt.r.Lifecycle = minSection
			}
			if tt.r.Summary.Pass == 0 {
				tt.r.Summary = verifySummary{Pass: 3}
			}
			err := validateReport(tt.r)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected error containing %q, got: %v", tt.want, err)
			}
		})
	}
}

// shared helper used by exit-code tests — calls the production runVerifyAndWrite path.
func runVerifyAndExit(t *testing.T, opts salesforceVerifyOptions, fdeps *fakeSalesforceVerifyDeps) int {
	t.Helper()
	code, err := runVerifyAndWrite(context.Background(), opts, toRealDeps(fdeps))
	if err != nil && !strings.Contains(err.Error(), "report validation") {
		t.Logf("runVerifyAndWrite error: %v", err)
	}
	return code
}

// toRealDeps converts fake test deps into the verifier's dependency struct.
func toRealDeps(fdeps *fakeSalesforceVerifyDeps) *salesforceVerifyDeps {
	return &salesforceVerifyDeps{
		gitHead:    fdeps.gitHeadFn,
		gitIsDirty: fdeps.gitIsDirtyFn,
		runSFCompiler: func(ctx context.Context, targetOrg string, rules []apexrules.Rule) (map[string]apexrules.SalesforceResult, error) {
			return fdeps.sfCompilerFn(ctx, targetOrg, rules)
		},
		runGladeCompiler: func(ctx context.Context, binary string, rules []apexrules.Rule) (map[string]apexrules.Outcome, error) {
			return fdeps.gladeCompilerFn(ctx, binary, rules)
		},
		runSFStdlib: func(ctx context.Context, targetOrg, workDir string, cases []oracleprobe.Case) (oracleprobe.Report, error) {
			return fdeps.sfStdlibFn(ctx, targetOrg, workDir, cases)
		},
		runGladeStdlib: func(ctx context.Context, gladeBin, projectDir string, cases []oracleprobe.Case) (oracleprobe.Report, error) {
			return fdeps.gladeStdlibFn(ctx, gladeBin, projectDir, cases)
		},
		runSFProject: func(ctx context.Context, projectDir, targetOrg string, cases []oracleprobe.Case) (oracleprobe.Report, error) {
			return fdeps.sfProjectFn(ctx, projectDir, targetOrg, cases)
		},
		runGladeProject: func(ctx context.Context, gladeBin, projectDir string, cases []oracleprobe.Case) (oracleprobe.Report, error) {
			return fdeps.gladeProjectFn(ctx, gladeBin, projectDir, cases)
		},
	}
}

// Validate the repository's real runtime fixture
func TestSalesforceVerify_RealRuntimeFixtureIsValid(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "fixtures", "salesforce-runtime-correctness.json")
	if err := validateRuntimeFixture(path); err != nil {
		t.Fatalf("real runtime fixture should validate: %v", err)
	}
}

// ============ Helpers ============

// ============ SF-08: Dual observations in every conclusive case ============

func TestSalesforceVerify_DualObservationsInPassingCompilerCase(t *testing.T) {
	dir := t.TempDir()
	releasePath := makeReleaseManifest(t, dir)
	catalogPath := makeCatalog(t, dir)
	runtimePath := makeRuntimeFixture(t, dir)
	projectPath := makeTestProject(t, dir)
	candidatePath := makeCandidate(t, dir)
	outPath := filepath.Join(dir, "out", "report.json")

	deps := allPassDeps()
	opts := salesforceVerifyOptions{
		ReleaseManifest: releasePath, Catalog: catalogPath, RuntimeCases: runtimePath,
		TestProject: projectPath, TargetOrg: "test-org", GladeBin: candidatePath,
		GladeRoot: dir, Out: outPath,
	}
	report := runVerifyWithDeps(t, opts, deps)

	for _, c := range report.Compiler.Cases {
		if c.Status == "pass" || c.Status == "fail" {
			if c.SalesforceObservation == "" {
				t.Fatalf("compiler pass/fail case %q missing salesforceObservation", c.ID)
			}
			if c.GladeObservation == "" {
				t.Fatalf("compiler pass/fail case %q missing gladeObservation", c.ID)
			}
		}
	}
}

func TestSalesforceVerify_DualObservationsInPassingRuntimeCase(t *testing.T) {
	dir := t.TempDir()
	releasePath := makeReleaseManifest(t, dir)
	catalogPath := makeCatalog(t, dir)
	runtimePath := makeRuntimeFixture(t, dir)
	projectPath := makeTestProject(t, dir)
	candidatePath := makeCandidate(t, dir)
	outPath := filepath.Join(dir, "out", "report.json")

	deps := allPassDeps()
	opts := salesforceVerifyOptions{
		ReleaseManifest: releasePath, Catalog: catalogPath, RuntimeCases: runtimePath,
		TestProject: projectPath, TargetOrg: "test-org", GladeBin: candidatePath,
		GladeRoot: dir, Out: outPath,
	}
	report := runVerifyWithDeps(t, opts, deps)

	for _, c := range report.Runtime.Cases {
		if c.Status == "pass" || c.Status == "fail" {
			if c.SalesforceObservation == "" {
				t.Fatalf("runtime pass/fail case %q missing salesforceObservation", c.ID)
			}
			if c.GladeObservation == "" {
				t.Fatalf("runtime pass/fail case %q missing gladeObservation", c.ID)
			}
		}
	}
}

func TestSalesforceVerify_DualObservationsInPassingLifecycleCase(t *testing.T) {
	dir := t.TempDir()
	releasePath := makeReleaseManifest(t, dir)
	catalogPath := makeCatalog(t, dir)
	runtimePath := makeRuntimeFixture(t, dir)
	projectPath := makeTestProject(t, dir)
	candidatePath := makeCandidate(t, dir)
	outPath := filepath.Join(dir, "out", "report.json")

	deps := allPassDeps()
	opts := salesforceVerifyOptions{
		ReleaseManifest: releasePath, Catalog: catalogPath, RuntimeCases: runtimePath,
		TestProject: projectPath, TargetOrg: "test-org", GladeBin: candidatePath,
		GladeRoot: dir, Out: outPath,
	}
	report := runVerifyWithDeps(t, opts, deps)

	for _, c := range report.Lifecycle.Cases {
		if c.Status == "pass" || c.Status == "fail" {
			if c.SalesforceObservation == "" {
				t.Fatalf("lifecycle pass/fail case %q missing salesforceObservation", c.ID)
			}
			if c.GladeObservation == "" {
				t.Fatalf("lifecycle pass/fail case %q missing gladeObservation", c.ID)
			}
		}
	}
}

func TestSalesforceVerify_ValidationRejectsPassWithoutDualObservations(t *testing.T) {
	// A pass case missing either salesforceObservation or gladeObservation must fail validation.
	r := verifyReport{
		SchemaVersion: 1, Status: "pass", Release: "t", APIVersion: "v",
		Compiler: verifySection{
			Status:  "pass",
			Cases:   []verifyCase{{ID: "c1", Status: "pass"}},
			Summary: verifySummary{Pass: 1},
		},
		Runtime: verifySection{
			Status:  "pass",
			Cases:   []verifyCase{{ID: "r1", Status: "pass"}},
			Summary: verifySummary{Pass: 1},
		},
		Lifecycle: verifySection{
			Status:  "pass",
			Cases:   []verifyCase{{ID: "l1", Status: "pass"}},
			Summary: verifySummary{Pass: 1},
		},
		Summary: verifySummary{Pass: 3},
	}
	err := validateReport(r)
	if err == nil {
		t.Fatal("expected validation to reject pass case without dual observations")
	}
}

func TestSalesforceVerify_InconclusiveCaseMayOmitDualObservations(t *testing.T) {
	// Inconclusive cases are operationally inconclusive — they may omit both sides.
	r := verifyReport{
		SchemaVersion: 1, Status: "inconclusive", Release: "t", APIVersion: "v",
		Compiler: verifySection{
			Status:  "inconclusive",
			Cases:   []verifyCase{{ID: "c1", Status: "inconclusive"}},
			Summary: verifySummary{Inconclusive: 1},
		},
		Runtime: verifySection{
			Status:  "inconclusive",
			Cases:   []verifyCase{{ID: "r1", Status: "inconclusive"}},
			Summary: verifySummary{Inconclusive: 1},
		},
		Lifecycle: verifySection{
			Status:  "inconclusive",
			Cases:   []verifyCase{{ID: "l1", Status: "inconclusive"}},
			Summary: verifySummary{Inconclusive: 1},
		},
		Summary: verifySummary{Inconclusive: 3},
	}
	err := validateReport(r)
	if err != nil {
		t.Fatalf("inconclusive cases should be allowed to omit dual observations: %v", err)
	}
}

// ============ SF-16: Lifecycle fixture failure guard ============

func TestSalesforceVerify_LifecycleFixtureFailureBlocked(t *testing.T) {
	dir := t.TempDir()
	releasePath := makeReleaseManifest(t, dir)
	catalogPath := makeCatalog(t, dir)
	runtimePath := makeRuntimeFixture(t, dir)
	projectPath := makeTestProject(t, dir)
	candidatePath := makeCandidate(t, dir)
	outPath := filepath.Join(dir, "out", "report.json")

	lifecycleCases := oracleprobe.ProjectOracleCases()

	// A single normal normalized assertion failure among otherwise passing results.
	// This is an assertion failure, not a deploy or compile failure.
	// A single failed result is sufficient to prove any failure blocks the whole fixture.
	sfFailedReport := allPassProjectReport(lifecycleCases)
	if len(sfFailedReport.Results) > 0 {
		failVal := "fail"
		sfFailedReport.Results[0].Value = &failVal
		// HasValue and ValueType are already correct from allPassProjectReport.
	}

	// If Glade were called, it would return the identical assertion failure report
	// and the general comparator would match — yielding a lifecycle pass.
	// The verifier must stop before that can happen.
	gladeFailedReport := sfFailedReport

	gladeProjectCalled := false
	deps := allPassDeps()
	deps.sfProjectFn = func(ctx context.Context, projectDir, targetOrg string, cases []oracleprobe.Case) (oracleprobe.Report, error) {
		return sfFailedReport, nil
	}
	deps.gladeProjectFn = func(ctx context.Context, gladeBin, projectDir string, cases []oracleprobe.Case) (oracleprobe.Report, error) {
		gladeProjectCalled = true
		return gladeFailedReport, nil
	}

	opts := salesforceVerifyOptions{
		ReleaseManifest: releasePath, Catalog: catalogPath, RuntimeCases: runtimePath,
		TestProject: projectPath, TargetOrg: "test-org", GladeBin: candidatePath,
		GladeRoot: dir, Out: outPath,
	}
	report := runVerifyWithDeps(t, opts, deps)

	// Glade lifecycle probe must NOT have been called.
	if gladeProjectCalled {
		t.Fatal("Glade lifecycle probe was called despite SF fixture assertion failure — should have been skipped")
	}

	// All twelve lifecycle cases must be inconclusive with category "fixture-failed".
	if len(report.Lifecycle.Cases) != len(lifecycleCases) {
		t.Fatalf("lifecycle cases: got %d, want %d", len(report.Lifecycle.Cases), len(lifecycleCases))
	}
	for _, c := range report.Lifecycle.Cases {
		if c.Status != "inconclusive" {
			t.Fatalf("lifecycle case %q: expected inconclusive, got %s", c.ID, c.Status)
		}
		if c.Category != "fixture-failed" {
			t.Fatalf("lifecycle case %q: expected category fixture-failed, got %q", c.ID, c.Category)
		}
	}

	// Lifecycle section must be inconclusive.
	if report.Lifecycle.Status != "inconclusive" {
		t.Fatalf("lifecycle section status: got %s, want inconclusive", report.Lifecycle.Status)
	}

	// Overall report must NOT be pass — identical assertion failures must not sneak through.
	if report.Status == "pass" {
		t.Fatal("overall report must not be pass when lifecycle fixture has an assertion failure")
	}
}

func TestSalesforceVerify_RejectsAPIProvenanceBeforeExecution(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(t *testing.T, opts salesforceVerifyOptions)
		want    string
	}{
		{"runtime mismatch", func(t *testing.T, opts salesforceVerifyOptions) {
			writeFile(t, opts.RuntimeCases, `{"apiVersion":"66.0","comparisons":[]}`)
		}, "runtime fixture"},
		{"project mismatch", func(t *testing.T, opts salesforceVerifyOptions) {
			writeFile(t, filepath.Join(opts.TestProject, "sfdx-project.json"), `{"sourceApiVersion":"66.0"}`)
		}, "sourceApiVersion"},
		{"project version missing", func(t *testing.T, opts salesforceVerifyOptions) {
			writeFile(t, filepath.Join(opts.TestProject, "sfdx-project.json"), `{}`)
		}, "invalid apiVersion"},
		{"class metadata mismatch", func(t *testing.T, opts salesforceVerifyOptions) {
			writeFile(t, filepath.Join(opts.TestProject, "force-app/main/default/classes/CorrectnessProbeTest.cls-meta.xml"),
				`<ApexClass><apiVersion>66.0</apiVersion></ApexClass>`)
		}, "Apex class metadata"},
		{"class metadata version missing", func(t *testing.T, opts salesforceVerifyOptions) {
			writeFile(t, filepath.Join(opts.TestProject, "force-app/main/default/classes/CorrectnessProbeTest.cls-meta.xml"),
				`<ApexClass/>`)
		}, "invalid apiVersion"},
		{"catalog newer", func(t *testing.T, opts salesforceVerifyOptions) {
			replaceFile(t, opts.Catalog, `"apiVersion": 67`, `"apiVersion": 68`)
		}, "newer than"},
		{"catalog missing current", func(t *testing.T, opts salesforceVerifyOptions) {
			replaceFile(t, opts.Catalog, `"apiVersion": 67`, `"apiVersion": 66`)
		}, "no rule"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			opts := salesforceVerifyOptions{
				ReleaseManifest: makeReleaseManifest(t, dir),
				Catalog:         makeCatalog(t, dir),
				RuntimeCases:    makeRuntimeFixture(t, dir),
				TestProject:     makeTestProject(t, dir),
				TargetOrg:       "test-org",
				GladeBin:        makeCandidate(t, dir),
				GladeRoot:       dir,
				Out:             filepath.Join(dir, "report.json"),
			}
			tt.prepare(t, opts)
			_, err := executeSalesforceVerify(context.Background(), opts, failOnExternalRunner(t))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestSalesforceVerify_AllowsHistoricalRulesAndNumericCurrentVersion(t *testing.T) {
	dir := t.TempDir()
	opts := salesforceVerifyOptions{
		ReleaseManifest: makeReleaseManifest(t, dir), // "67.0"
		Catalog:         makeCatalog(t, dir),         // rules 66 and 67
		RuntimeCases:    makeRuntimeFixture(t, dir),
		TestProject:     makeTestProject(t, dir),
		TargetOrg:       "test-org",
		GladeBin:        makeCandidate(t, dir),
		GladeRoot:       dir,
		Out:             filepath.Join(dir, "report.json"),
	}
	report, err := executeSalesforceVerify(context.Background(), opts, toRealDeps(allPassDeps()))
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "pass" {
		t.Fatalf("status = %q, want pass", report.Status)
	}
}

func failOnExternalRunner(t *testing.T) *salesforceVerifyDeps {
	t.Helper()
	deps := toRealDeps(allPassDeps())
	deps.runSFCompiler = func(context.Context, string, []apexrules.Rule) (map[string]apexrules.SalesforceResult, error) {
		t.Fatal("Salesforce compiler ran before provenance passed")
		return nil, nil
	}
	deps.runGladeCompiler = func(context.Context, string, []apexrules.Rule) (map[string]apexrules.Outcome, error) {
		t.Fatal("Glade compiler ran before provenance passed")
		return nil, nil
	}
	deps.runSFStdlib = func(context.Context, string, string, []oracleprobe.Case) (oracleprobe.Report, error) {
		t.Fatal("Salesforce runtime ran before provenance passed")
		return oracleprobe.Report{}, nil
	}
	deps.runGladeStdlib = func(context.Context, string, string, []oracleprobe.Case) (oracleprobe.Report, error) {
		t.Fatal("Glade runtime ran before provenance passed")
		return oracleprobe.Report{}, nil
	}
	deps.runSFProject = func(context.Context, string, string, []oracleprobe.Case) (oracleprobe.Report, error) {
		t.Fatal("Salesforce lifecycle ran before provenance passed")
		return oracleprobe.Report{}, nil
	}
	deps.runGladeProject = func(context.Context, string, string, []oracleprobe.Case) (oracleprobe.Report, error) {
		t.Fatal("Glade lifecycle ran before provenance passed")
		return oracleprobe.Report{}, nil
	}
	return deps
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func replaceFile(t *testing.T, path, old, new string) {
	t.Helper()
	content := string(mustReadFile(t, path))
	if !strings.Contains(content, old) {
		t.Fatalf("%s does not contain %q", path, old)
	}
	writeFile(t, path, strings.Replace(content, old, new, 1))
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
