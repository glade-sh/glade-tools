package toolcli

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/glade-sh/glade/tools/internal/apexdocs"
	"github.com/glade-sh/glade/tools/internal/apexrules"
	"github.com/glade-sh/glade/tools/internal/oracleprobe"
	"github.com/glade-sh/glade/tools/internal/surfaceledger"
)

// ----- options -----

// salesforceVerifyOptions holds the parsed CLI arguments for salesforce verify.
type salesforceVerifyOptions struct {
	ReleaseManifest, Catalog, RuntimeCases, TestProject                                  string
	TargetOrg, GladeBin, GladeRoot, Out                                                  string
	Developer                                                                            bool
	PreviousReleaseManifest, PreviousInventory, CurrentInventory, ReleaseClassifications string
}

func (o salesforceVerifyOptions) deltaMode() bool {
	return o.PreviousReleaseManifest != "" || o.PreviousInventory != "" ||
		o.CurrentInventory != "" || o.ReleaseClassifications != ""
}

// ----- dependency seam -----

// salesforceVerifyDeps holds all external operations the verifier needs.
// Tests inject fakes; production populates real exec-based implementations.
type salesforceVerifyDeps struct {
	gitHead          func(ctx context.Context, dir string) (string, error)
	gitIsDirty       func(ctx context.Context, dir string) (bool, error)
	runSFCompiler    func(ctx context.Context, targetOrg string, rules []apexrules.Rule) (map[string]apexrules.SalesforceResult, error)
	runGladeCompiler func(ctx context.Context, binary string, rules []apexrules.Rule) (map[string]apexrules.Outcome, error)
	runSFStdlib      func(ctx context.Context, targetOrg, workDir string, cases []oracleprobe.Case) (oracleprobe.Report, error)
	runGladeStdlib   func(ctx context.Context, gladeBin, projectDir string, cases []oracleprobe.Case) (oracleprobe.Report, error)
	runSFProject     func(ctx context.Context, projectDir, targetOrg string, cases []oracleprobe.Case) (oracleprobe.Report, error)
	runGladeProject  func(ctx context.Context, gladeBin, projectDir string, cases []oracleprobe.Case) (oracleprobe.Report, error)
}

func realGitHead(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse failed")
	}
	return strings.TrimSpace(string(out)), nil
}

func realGitIsDirty(ctx context.Context, dir string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "status", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("git status failed")
	}
	return len(strings.TrimSpace(string(out))) > 0, nil
}

func realDeps() *salesforceVerifyDeps {
	return &salesforceVerifyDeps{
		gitHead:          realGitHead,
		gitIsDirty:       realGitIsDirty,
		runSFCompiler:    apexrules.RunSalesforce,
		runGladeCompiler: apexrules.RunGlade,
		runSFStdlib: func(ctx context.Context, targetOrg, workDir string, cases []oracleprobe.Case) (oracleprobe.Report, error) {
			report, err := oracleprobe.RunAnonymous(ctx, cases, oracleprobe.Options{TargetOrg: targetOrg, WorkDir: workDir})
			if err != nil {
				return oracleprobe.Report{}, err
			}
			return oracleprobe.RedactReport(report), nil
		},
		runGladeStdlib: func(ctx context.Context, gladeBin, projectDir string, cases []oracleprobe.Case) (oracleprobe.Report, error) {
			return oracleprobe.RunGlade(ctx, oracleprobe.OSExecRunner{}, oracleprobe.GladeOptions{GladeBin: gladeBin, ProjectDir: projectDir}, cases)
		},
		runSFProject: func(ctx context.Context, projectDir, targetOrg string, cases []oracleprobe.Case) (oracleprobe.Report, error) {
			return oracleprobe.RunProjectOracle(ctx, oracleprobe.RealProjectRunner{}, projectDir, targetOrg, cases)
		},
		runGladeProject: func(ctx context.Context, gladeBin, projectDir string, cases []oracleprobe.Case) (oracleprobe.Report, error) {
			return oracleprobe.RunGladeProject(ctx, oracleprobe.OSExecRunner{}, oracleprobe.GladeProjectOptions{GladeBin: gladeBin, ProjectDir: projectDir}, cases)
		},
	}
}

// ----- report schema -----

const verifyReportSchemaVersion = 1

type verifyReport struct {
	SchemaVersion int                 `json:"schemaVersion"`
	Release       string              `json:"release"`
	APIVersion    string              `json:"apiVersion"`
	Status        string              `json:"status"`
	Glade         verifyGitProvenance `json:"glade"`
	GladeTools    verifyGitProvenance `json:"gladeTools"`
	Candidate     verifyCandidate     `json:"candidate"`
	Inputs        []verifyInput       `json:"inputs"`
	ReleaseDelta  *verifyReleaseDelta `json:"releaseDelta,omitempty"`
	Compiler      verifySection       `json:"compiler"`
	Runtime       verifySection       `json:"runtime"`
	Lifecycle     verifySection       `json:"lifecycle"`
	Summary       verifySummary       `json:"summary"`
}

type verifyReleaseDelta struct {
	Status             string   `json:"status"`
	PreviousRelease    string   `json:"previousRelease"`
	PreviousApiVersion string   `json:"previousApiVersion"`
	PreviousDigest     string   `json:"previousDigest"`
	CurrentRelease     string   `json:"currentRelease"`
	CurrentApiVersion  string   `json:"currentApiVersion"`
	CurrentDigest      string   `json:"currentDigest"`
	Added              []string `json:"added"`
	Removed            []string `json:"removed"`
	Changed            []string `json:"changed"`
	Unchanged          []string `json:"unchanged"`
	Error              string   `json:"error"`
}

type releaseClassificationsFile struct {
	SchemaVersion   int                          `json:"schemaVersion"`
	PreviousRelease string                       `json:"previousRelease"`
	CurrentRelease  string                       `json:"currentRelease"`
	Classifications []releaseClassificationEntry `json:"classifications"`
}

type releaseClassificationEntry struct {
	SurfaceID   string `json:"surfaceId"`
	Scope       string `json:"scope"`
	Disposition string `json:"disposition"`
	CaseID      string `json:"caseId,omitempty"`
	ReasonRef   string `json:"reasonRef,omitempty"`
}

type verifyGitProvenance struct {
	Commit string `json:"commit"`
	Dirty  bool   `json:"dirty"`
}

type verifyCandidate struct {
	Path         string `json:"path"`
	SHA256Before string `json:"sha256Before"`
	SHA256After  string `json:"sha256After"`
}

type verifyInput struct {
	Kind   string `json:"kind"`
	SHA256 string `json:"sha256"`
}

type verifySection struct {
	Status            string        `json:"status"`
	OracleDrift       bool          `json:"oracleDrift"`
	NormalizerChanged bool          `json:"normalizerChanged"`
	Summary           verifySummary `json:"summary"`
	Cases             []verifyCase  `json:"cases"`
}

type verifyCase struct {
	ID                    string `json:"id"`
	Status                string `json:"status"`
	Observation           string `json:"observation,omitempty"`
	Category              string `json:"category,omitempty"`
	SalesforceObservation string `json:"salesforceObservation,omitempty"`
	GladeObservation      string `json:"gladeObservation,omitempty"`
}

type verifySummary struct {
	Pass         int `json:"pass"`
	Fail         int `json:"fail"`
	Inconclusive int `json:"inconclusive"`
}

// ----- release manifest schema -----

type releaseManifest struct {
	SchemaVersion  int      `json:"schemaVersion"`
	Release        string   `json:"release"`
	APIVersion     string   `json:"apiVersion"`
	Digest         string   `json:"digest"`
	Acquisition    string   `json:"acquisition"`
	SourceFamilies []string `json:"sourceFamilies"`
}

// ----- CLI entry point -----

func runSalesforceVerify(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && isHelpArg(args[0]) {
		printSalesforceVerifyHelp(stdout)
		return 0
	}
	if len(args) > 1 && args[0] == "verify" && isHelpArg(args[1]) {
		printSalesforceVerifyHelp(stdout)
		return 0
	}

	opts, err := parseSalesforceVerifyFlags(args)
	if err != nil {
		fmt.Fprintf(stderr, "glade-tools: %v\n", err)
		return 1
	}

	if err := os.MkdirAll(filepath.Dir(opts.Out), 0755); err != nil {
		fmt.Fprintf(stderr, "glade-tools: %v\n", err)
		return 1
	}

	code, err := runVerifyAndWrite(ctx, opts, realDeps())
	if err != nil {
		fmt.Fprintf(stderr, "glade-tools: %v\n", err)
	}
	return code
}

// runVerifyAndWrite executes verification and writes the artifact.
// Returns the CLI exit code. Exported for tests to exercise the full
// production write/exit path with injected dependencies.
func runVerifyAndWrite(ctx context.Context, opts salesforceVerifyOptions, deps *salesforceVerifyDeps) (int, error) {
	report, err := executeSalesforceVerify(ctx, opts, deps)
	if err != nil {
		if report.Status != "" {
			writeReport(opts.Out, report)
		}
		return 1, err
	}
	if err := writeReport(opts.Out, report); err != nil {
		return 1, err
	}
	if report.Status == "pass" {
		return 0, nil
	}
	return 1, nil
}

func parseSalesforceVerifyFlags(args []string) (salesforceVerifyOptions, error) {
	opts := salesforceVerifyOptions{}
	seen := make(map[string]bool)

	i := 0
	for i < len(args) {
		if args[i] == "verify" {
			i++
			continue
		}
		flag := args[i]
		switch flag {
		case "--release-manifest", "--catalog", "--runtime-cases", "--test-project",
			"--target-org", "--glade-bin", "--glade-root", "--out",
			"--previous-release-manifest", "--previous-inventory",
			"--current-inventory", "--release-classifications":
			if seen[flag] {
				return opts, fmt.Errorf("duplicate flag: %s", flag)
			}
			if i+1 >= len(args) {
				return opts, fmt.Errorf("required value missing for %s", flag)
			}
			val := args[i+1]
			switch flag {
			case "--release-manifest":
				opts.ReleaseManifest = val
			case "--catalog":
				opts.Catalog = val
			case "--runtime-cases":
				opts.RuntimeCases = val
			case "--test-project":
				opts.TestProject = val
			case "--target-org":
				opts.TargetOrg = val
			case "--glade-bin":
				opts.GladeBin = val
			case "--glade-root":
				opts.GladeRoot = val
			case "--out":
				opts.Out = val
			case "--previous-release-manifest":
				opts.PreviousReleaseManifest = val
			case "--previous-inventory":
				opts.PreviousInventory = val
			case "--current-inventory":
				opts.CurrentInventory = val
			case "--release-classifications":
				opts.ReleaseClassifications = val
			}
			seen[flag] = true
			i += 2
		case "--developer":
			opts.Developer = true
			i++
		default:
			return opts, fmt.Errorf("unknown flag: %s", flag)
		}
	}

	// Validate required flags
	for _, required := range []string{
		"--release-manifest", "--catalog", "--runtime-cases",
		"--test-project", "--target-org", "--glade-bin", "--glade-root", "--out",
	} {
		if !seen[required] {
			return opts, fmt.Errorf("required flag missing: %s", required)
		}
	}

	if opts.deltaMode() && (opts.PreviousReleaseManifest == "" || opts.PreviousInventory == "" ||
		opts.CurrentInventory == "" || opts.ReleaseClassifications == "") {
		return opts, fmt.Errorf("release-delta requires all four flags: --previous-release-manifest, --previous-inventory, --current-inventory, --release-classifications")
	}

	return opts, nil
}

// ----- core engine -----

// executeSalesforceVerify runs the full three-section verification.
// It returns the report even when status is fail or inconclusive so the
// caller can write the artifact and decide the exit code.
func executeSalesforceVerify(ctx context.Context, opts salesforceVerifyOptions, deps *salesforceVerifyDeps) (verifyReport, error) {
	report := verifyReport{
		SchemaVersion: verifyReportSchemaVersion,
	}

	// 1. Validate input files exist
	for _, path := range []string{opts.ReleaseManifest, opts.Catalog, opts.RuntimeCases} {
		if _, err := os.Stat(path); err != nil {
			return report, fmt.Errorf("input file not found: %s", path)
		}
	}
	if info, err := os.Stat(opts.TestProject); err != nil || !info.IsDir() {
		return report, fmt.Errorf("test project not found: %s", opts.TestProject)
	}

	// 2. Load and validate release manifest
	rm, err := loadReleaseManifest(opts.ReleaseManifest)
	if err != nil {
		return report, fmt.Errorf("release manifest: %v", err)
	}
	report.Release = rm.Release
	report.APIVersion = rm.APIVersion

	var releaseDelta *verifyReleaseDelta
	if opts.deltaMode() {
		delta, err := computeReleaseDelta(opts, rm)
		if err != nil {
			return report, err
		}
		releaseDelta = delta
	}
	report.ReleaseDelta = releaseDelta

	// 3. Validate all release/API inputs before any Salesforce or Glade runner.
	if err := checkAPIVersionProvenance(rm, opts); err != nil {
		return report, err
	}

	// 4. Git provenance
	gladeCommit, gladeDirty, err := gitProvenance(ctx, deps.gitHead, deps.gitIsDirty, opts.GladeRoot)
	if err != nil {
		return report, fmt.Errorf("glade git provenance: %v", err)
	}
	report.Glade = verifyGitProvenance{Commit: gladeCommit, Dirty: gladeDirty}

	toolsRoot, err := discoverToolsRoot(ctx)
	if err != nil {
		return report, fmt.Errorf("glade-tools root discovery: %v", err)
	}
	toolsCommit, toolsDirty, err := gitProvenance(ctx, deps.gitHead, deps.gitIsDirty, toolsRoot)
	if err != nil {
		return report, fmt.Errorf("glade-tools git provenance: %v", err)
	}
	report.GladeTools = verifyGitProvenance{Commit: toolsCommit, Dirty: toolsDirty}

	// 4. Release mode: reject dirty roots
	if !opts.Developer {
		if report.Glade.Dirty {
			return report, fmt.Errorf("release mode rejects dirty Glade root")
		}
		if report.GladeTools.Dirty {
			return report, fmt.Errorf("release mode rejects dirty glade-tools root")
		}
	}

	// 5. Candidate identity: SHA256 before
	candidateBefore, err := sha256File(opts.GladeBin)
	if err != nil {
		return report, err
	}
	candidateBasename := filepath.Base(opts.GladeBin)
	report.Candidate = verifyCandidate{
		Path:         candidateBasename,
		SHA256Before: candidateBefore,
	}

	// 6. Input hashes — all four must be readable and valid
	relSha, err := sha256File(opts.ReleaseManifest)
	if err != nil {
		return report, fmt.Errorf("release manifest hash: %w", err)
	}
	catSha, err := sha256File(opts.Catalog)
	if err != nil {
		return report, fmt.Errorf("catalog hash: %w", err)
	}
	rtSha, err := sha256File(opts.RuntimeCases)
	if err != nil {
		return report, fmt.Errorf("runtime fixture hash: %w", err)
	}
	projSha, err := hashTestProjectDir(opts.TestProject)
	if err != nil {
		return report, fmt.Errorf("test project hash: %w", err)
	}
	report.Inputs = []verifyInput{
		{Kind: "release-manifest", SHA256: relSha},
		{Kind: "compiler-catalog", SHA256: catSha},
		{Kind: "runtime-cases", SHA256: rtSha},
		{Kind: "test-project", SHA256: projSha},
	}
	if opts.deltaMode() {
		for _, p := range [][2]string{
			{"previous-release-manifest", opts.PreviousReleaseManifest},
			{"previous-inventory", opts.PreviousInventory},
			{"current-inventory", opts.CurrentInventory},
			{"release-classifications", opts.ReleaseClassifications},
		} {
			h, err := sha256File(p[1])
			if err != nil {
				return report, fmt.Errorf("%s hash: %w", p[0], err)
			}
			report.Inputs = append(report.Inputs, verifyInput{Kind: p[0], SHA256: h})
		}
	}

	// 7. Compiler section
	compilerSection := runCompilerSection(ctx, opts, deps)
	report.Compiler = compilerSection

	// 8. Runtime section
	runtimeSection := runRuntimeSection(ctx, opts, deps)
	report.Runtime = runtimeSection

	// 9. Lifecycle section
	lifecycleSection := runLifecycleSection(ctx, opts, deps)
	report.Lifecycle = lifecycleSection

	// 10. Candidate identity: SHA256 after
	candidateAfter, err := sha256File(opts.GladeBin)
	if err != nil {
		return report, err
	}
	report.Candidate.SHA256After = candidateAfter

	// 11. Overall status and summary
	report.Summary = verifySummary{
		Pass:         report.Compiler.Summary.Pass + report.Runtime.Summary.Pass + report.Lifecycle.Summary.Pass,
		Fail:         report.Compiler.Summary.Fail + report.Runtime.Summary.Fail + report.Lifecycle.Summary.Fail,
		Inconclusive: report.Compiler.Summary.Inconclusive + report.Runtime.Summary.Inconclusive + report.Lifecycle.Summary.Inconclusive,
	}

	if report.Summary.Fail > 0 {
		report.Status = "fail"
	} else if report.Summary.Inconclusive > 0 {
		report.Status = "inconclusive"
	} else if report.Candidate.SHA256Before != report.Candidate.SHA256After {
		report.Status = "fail"
	} else {
		report.Status = "pass"
	}

	if releaseDelta != nil && releaseDelta.Status == "fail" {
		report.Status = "fail"
	}

	// 12. Validate the final report before it can pass or be renamed
	if err := validateReport(report); err != nil {
		if report.Status == "pass" {
			report.Status = "inconclusive"
		}
		report.Summary.Inconclusive += report.Summary.Pass
		report.Summary.Pass = 0
		return report, fmt.Errorf("report validation: %w", err)
	}

	return report, nil
}

// ----- section execution -----

func runCompilerSection(ctx context.Context, opts salesforceVerifyOptions, deps *salesforceVerifyDeps) verifySection {
	section := verifySection{Status: "pass"}

	catalog, err := apexrules.LoadCatalog(opts.Catalog)
	if err != nil {
		section.Status = "inconclusive"
		return section
	}
	if len(catalog.Rules) == 0 {
		section.Status = "inconclusive"
		return section
	}

	sfResults, sfErr := deps.runSFCompiler(ctx, opts.TargetOrg, catalog.Rules)
	if sfErr != nil {
		ids := make([]string, len(catalog.Rules))
		for i, r := range catalog.Rules {
			ids[i] = r.ID
		}
		section.Cases = errorCases(ids)
		section.Status = "inconclusive"
		section.Summary.Inconclusive = len(catalog.Rules)
		return section
	}

	gladeResults, gladeErr := deps.runGladeCompiler(ctx, opts.GladeBin, catalog.Rules)
	if gladeErr != nil {
		ids := make([]string, len(catalog.Rules))
		for i, r := range catalog.Rules {
			ids[i] = r.ID
		}
		section.Cases = errorCases(ids)
		section.Status = "inconclusive"
		section.Summary.Inconclusive = len(catalog.Rules)
		return section
	}

	results := apexrules.CompareObserved(catalog.Rules, sfResults, gladeResults)
	cases := make([]verifyCase, len(results))
	oracleDrift := false
	pass, fail, inconclusive := 0, 0, 0
	for i, r := range results {
		status := ""
		if r.ExecStatus == "inconclusive" {
			status = "inconclusive"
		} else if !r.OracleMatched {
			status = "fail" // catalog drift is always a failure
		} else if r.Matched {
			status = "pass"
		} else {
			status = "fail"
		}
		obs := ""
		if r.Salesforce != "" {
			obs = string(r.Salesforce)
		} else {
			obs = string(r.Glade)
		}
		sfObs := string(r.Salesforce)
		glObs := string(r.Glade)
		cat := r.Status
		if !r.OracleMatched {
			oracleDrift = true
		}
		cases[i] = verifyCase{
			ID:                    r.ID,
			Status:                status,
			Observation:           obs,
			Category:              cat,
			SalesforceObservation: sfObs,
			GladeObservation:      glObs,
		}
		switch status {
		case "pass":
			pass++
		case "fail":
			fail++
		case "inconclusive":
			inconclusive++
		}
	}
	section.Cases = cases
	section.OracleDrift = oracleDrift
	section.Summary = verifySummary{Pass: pass, Fail: fail, Inconclusive: inconclusive}
	if fail > 0 || oracleDrift {
		section.Status = "fail"
	} else if inconclusive > 0 {
		section.Status = "inconclusive"
	}
	return section
}

func runRuntimeSection(ctx context.Context, opts salesforceVerifyOptions, deps *salesforceVerifyDeps) verifySection {
	section := verifySection{Status: "pass"}
	cases := oracleprobe.StdlibCases()

	sfReport, sfErr := deps.runSFStdlib(ctx, opts.TargetOrg, "", cases)
	if sfErr != nil {
		section = inconclusiveSection(cases)
		return section
	}
	if err := validateAcquiredReport(sfReport, cases); err != nil {
		section = inconclusiveSection(cases)
		return section
	}

	gladeReport, gladeErr := deps.runGladeStdlib(ctx, opts.GladeBin, "", cases)
	if gladeErr != nil {
		section = inconclusiveSection(cases)
		return section
	}
	if err := validateAcquiredReport(gladeReport, cases); err != nil {
		section = inconclusiveSection(cases)
		return section
	}

	comparisons := oracleprobe.CompareReports(sfReport, gladeReport, cases)
	verCases := make([]verifyCase, len(comparisons))
	pass, fail, inconclusive := 0, 0, 0
	for i, cmp := range comparisons {
		verCases[i] = verifyCase{
			ID:                    cmp.CaseID,
			Status:                string(cmp.Status),
			Observation:           cmp.SFObservation,
			Category:              cmp.ExpectedObservation,
			SalesforceObservation: cmp.SFObservation,
			GladeObservation:      cmp.GladeObservation,
		}
		switch cmp.Status {
		case oracleprobe.StatusPass:
			pass++
		case oracleprobe.StatusFail:
			fail++
		case oracleprobe.StatusInconclusive:
			inconclusive++
		}
	}
	section.Cases = verCases
	section.Summary = verifySummary{Pass: pass, Fail: fail, Inconclusive: inconclusive}
	if fail > 0 {
		section.Status = "fail"
	} else if inconclusive > 0 {
		section.Status = "inconclusive"
	}
	return section
}

// lifecycleFixturePassing checks that every Salesforce lifecycle result
// is a passing assertion: HasValue==true, non-nil Value, *Value=="pass",
// ValueType=="assertion". If any result fails this contract, the fixture itself
// is broken and the section must be blocked.
func lifecycleFixturePassing(sfReport oracleprobe.Report) bool {
	for _, r := range sfReport.Results {
		if !r.HasValue || r.Value == nil || *r.Value != "pass" || r.ValueType != "assertion" {
			return false
		}
	}
	return true
}

func runLifecycleSection(ctx context.Context, opts salesforceVerifyOptions, deps *salesforceVerifyDeps) verifySection {
	section := verifySection{Status: "pass"}
	cases := oracleprobe.ProjectOracleCases()

	sfReport, sfErr := deps.runSFProject(ctx, opts.TestProject, opts.TargetOrg, cases)
	if sfErr != nil {
		section = inconclusiveSection(cases)
		return section
	}
	if err := validateAcquiredReport(sfReport, cases); err != nil {
		section = inconclusiveSection(cases)
		return section
	}

	// SF-16: Fail closed if the lifecycle fixture itself did not pass.
	if !lifecycleFixturePassing(sfReport) {
		ids := make([]string, len(cases))
		for i, c := range cases {
			ids[i] = c.ID
		}
		fixtureCases := make([]verifyCase, len(ids))
		for i, id := range ids {
			fixtureCases[i] = verifyCase{
				ID:       id,
				Status:   "inconclusive",
				Category: "fixture-failed",
			}
		}
		return verifySection{
			Status:  "inconclusive",
			Cases:   fixtureCases,
			Summary: verifySummary{Inconclusive: len(cases)},
		}
	}

	gladeReport, gladeErr := deps.runGladeProject(ctx, opts.GladeBin, opts.TestProject, cases)
	if gladeErr != nil {
		section = inconclusiveSection(cases)
		return section
	}
	if err := validateAcquiredReport(gladeReport, cases); err != nil {
		section = inconclusiveSection(cases)
		return section
	}

	comparisons := oracleprobe.CompareReports(sfReport, gladeReport, cases)
	verCases := make([]verifyCase, len(comparisons))
	pass, fail, inconclusive := 0, 0, 0
	for i, cmp := range comparisons {
		verCases[i] = verifyCase{
			ID:                    cmp.CaseID,
			Status:                string(cmp.Status),
			Observation:           cmp.SFObservation,
			Category:              cmp.ExpectedObservation,
			SalesforceObservation: cmp.SFObservation,
			GladeObservation:      cmp.GladeObservation,
		}
		switch cmp.Status {
		case oracleprobe.StatusPass:
			pass++
		case oracleprobe.StatusFail:
			fail++
		case oracleprobe.StatusInconclusive:
			inconclusive++
		}
	}
	section.Cases = verCases
	section.Summary = verifySummary{Pass: pass, Fail: fail, Inconclusive: inconclusive}
	if fail > 0 {
		section.Status = "fail"
	} else if inconclusive > 0 {
		section.Status = "inconclusive"
	}
	return section
}

// ----- git helpers -----

func gitProvenance(ctx context.Context, headFn func(context.Context, string) (string, error), dirtyFn func(context.Context, string) (bool, error), dir string) (string, bool, error) {
	commit, err := headFn(ctx, dir)
	if err != nil {
		return "", false, err
	}
	dirty, err := dirtyFn(ctx, dir)
	if err != nil {
		return "", false, err
	}
	return commit, dirty, nil
}

func discoverToolsRoot(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("unable to discover glade-tools root")
	}
	return strings.TrimSpace(string(out)), nil
}

// ----- hashing -----

func sha256File(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h), nil
}

func hashTestProjectDir(root string) (string, error) {
	type fileEntry struct {
		relPath string
		bytes   []byte
	}
	var entries []fileEntry
	var walkErr error

	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			walkErr = err
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			walkErr = err
			return err
		}
		if d.IsDir() {
			base := filepath.Base(path)
			if base == ".git" || base == ".glade" {
				return filepath.SkipDir
			}
			return nil
		}
		for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
			if part == ".git" || part == ".glade" {
				return nil
			}
		}
		data, err := os.ReadFile(path)
		if err != nil {
			walkErr = err
			return err
		}
		entries = append(entries, fileEntry{relPath: filepath.ToSlash(rel), bytes: data})
		return nil
	})
	if walkErr != nil {
		return "", fmt.Errorf("walk test project: %w", walkErr)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].relPath < entries[j].relPath
	})

	h := sha256.New()
	for _, entry := range entries {
		h.Write([]byte(entry.relPath))
		h.Write([]byte{0})
		h.Write(entry.bytes)
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// ----- release manifest -----

func loadReleaseManifest(path string) (releaseManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return releaseManifest{}, err
	}
	var rm releaseManifest
	if err := json.Unmarshal(data, &rm); err != nil {
		return releaseManifest{}, fmt.Errorf("invalid release manifest JSON: %w", err)
	}
	if rm.SchemaVersion != 1 {
		return releaseManifest{}, fmt.Errorf("release manifest schemaVersion must be 1, got %d", rm.SchemaVersion)
	}
	if strings.TrimSpace(rm.Release) == "" {
		return releaseManifest{}, fmt.Errorf("release manifest missing release field")
	}
	if strings.TrimSpace(rm.APIVersion) == "" {
		return releaseManifest{}, fmt.Errorf("release manifest missing apiVersion")
	}
	if !isLowerHex64(rm.Digest) {
		return releaseManifest{}, fmt.Errorf("release manifest digest must be 64 lowercase hex chars")
	}
	if strings.TrimSpace(rm.Acquisition) == "" {
		return releaseManifest{}, fmt.Errorf("release manifest missing acquisition")
	}
	if len(rm.SourceFamilies) == 0 {
		return releaseManifest{}, fmt.Errorf("release manifest missing sourceFamilies")
	}
	for _, sf := range rm.SourceFamilies {
		if strings.TrimSpace(sf) == "" {
			return releaseManifest{}, fmt.Errorf("release manifest has blank source family")
		}
	}
	return rm, nil
}

func isLowerHex64(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

func computeReleaseDelta(opts salesforceVerifyOptions, curr releaseManifest) (*verifyReleaseDelta, error) {
	prev, err := loadReleaseManifest(opts.PreviousReleaseManifest)
	if err != nil {
		return nil, fmt.Errorf("previous release manifest: %w", err)
	}
	if prev.Digest == strings.Repeat("0", 64) {
		return nil, fmt.Errorf("previous manifest digest is all zeros")
	}
	if curr.Digest == strings.Repeat("0", 64) {
		return nil, fmt.Errorf("current manifest digest is all zeros")
	}
	if !setsEqual(prev.SourceFamilies, curr.SourceFamilies) {
		return nil, fmt.Errorf("source families differ between releases")
	}
	prevAPI, err := parseAPIVersion("previous", prev.APIVersion)
	if err != nil {
		return nil, err
	}
	currAPI, err := parseAPIVersion("current", curr.APIVersion)
	if err != nil {
		return nil, err
	}
	if prevAPI >= currAPI {
		return nil, fmt.Errorf("prev API %s not lower than current %s", prev.APIVersion, curr.APIVersion)
	}
	prevInv, err := apexdocs.ReadInventory(opts.PreviousInventory)
	if err != nil {
		return nil, fmt.Errorf("previous inventory: %w", err)
	}
	currInv, err := apexdocs.ReadInventory(opts.CurrentInventory)
	if err != nil {
		return nil, fmt.Errorf("current inventory: %w", err)
	}
	if d := apexdocs.CanonicalDigest(prevInv); d != prev.Digest {
		return nil, fmt.Errorf("prev inventory digest != manifest")
	}
	if d := apexdocs.CanonicalDigest(currInv); d != curr.Digest {
		return nil, fmt.Errorf("current inventory digest != manifest")
	}
	prevRows := surfaceledger.Merge(surfaceledger.RowsFromDocsInventory(prevInv), nil, nil, nil).Rows
	currRows := surfaceledger.Merge(surfaceledger.RowsFromDocsInventory(currInv), nil, nil, nil).Rows
	classFile, err := loadClassificationsFile(opts.ReleaseClassifications)
	if err != nil {
		return nil, err
	}
	if classFile.SchemaVersion != 1 {
		return nil, fmt.Errorf("classifications schemaVersion must be 1")
	}
	if classFile.PreviousRelease != prev.Release || classFile.CurrentRelease != curr.Release {
		return nil, fmt.Errorf("classifications release names mismatch")
	}
	classifications := make([]surfaceledger.ReleaseClassification, len(classFile.Classifications))
	for i, e := range classFile.Classifications {
		classifications[i] = surfaceledger.ReleaseClassification{
			SurfaceID: e.SurfaceID, Scope: surfaceledger.ReleaseScope(e.Scope),
			Disposition: surfaceledger.ReleaseDisposition(e.Disposition),
			CaseID:      e.CaseID, ReasonRef: e.ReasonRef,
		}
	}
	added, removed, changed, unchanged, deltaErr := surfaceledger.ComputeReleaseDelta(prevRows, currRows, nil)
	if added == nil || removed == nil || changed == nil || unchanged == nil {
		return nil, deltaErr
	}
	if len(classifications) > 0 {
		a, r, c, u, err := surfaceledger.ComputeReleaseDelta(prevRows, currRows, classifications)
		if a != nil {
			added, removed, changed, unchanged = a, r, c, u
		}
		deltaErr = err
	}
	delta := &verifyReleaseDelta{
		Status: "pass", PreviousRelease: prev.Release, PreviousApiVersion: prev.APIVersion,
		PreviousDigest: prev.Digest, CurrentRelease: curr.Release, CurrentApiVersion: curr.APIVersion,
		CurrentDigest: curr.Digest,
		Added:         deltaIDs(added), Removed: deltaIDs(removed),
		Changed: deltaIDs(changed), Unchanged: deltaIDs(unchanged),
	}
	if deltaErr != nil {
		delta.Status = "fail"
		delta.Error = deltaErr.Error()
	}
	return delta, nil
}

func setsEqual(a, b []string) bool {
	a, b = append([]string(nil), a...), append([]string(nil), b...)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}

func deltaIDs(entries []surfaceledger.DeltaEntry) []string {
	ids := make([]string, len(entries))
	for i, e := range entries {
		ids[i] = e.SurfaceID
	}
	return ids
}

func loadClassificationsFile(path string) (releaseClassificationsFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return releaseClassificationsFile{}, err
	}
	var f releaseClassificationsFile
	if err := json.Unmarshal(data, &f); err != nil {
		return releaseClassificationsFile{}, fmt.Errorf("invalid classifications JSON: %w", err)
	}
	return f, nil
}

// ----- atomic write -----

func writeReport(outPath string, report verifyReport) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(outPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	tmpFile, err := os.CreateTemp(dir, ".verify-report-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	// Validate: decode back and run full report validation
	var decoded verifyReport
	if err := json.Unmarshal(data, &decoded); err != nil {
		tmpFile.Close()
		return fmt.Errorf("validate report: %w", err)
	}
	if err := validateReport(decoded); err != nil {
		tmpFile.Close()
		return fmt.Errorf("validate report: %w", err)
	}

	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}

	if err := os.Rename(tmpPath, outPath); err != nil {
		return fmt.Errorf("rename temp to output: %w", err)
	}

	return nil
}

// ----- validation -----

var validStatuses = map[string]bool{"pass": true, "fail": true, "inconclusive": true}

func validateReport(r verifyReport) error {
	for _, s := range []verifySection{r.Compiler, r.Runtime, r.Lifecycle} {
		if err := validateSection(s); err != nil {
			return err
		}
	}
	counts := verifySummary{}
	for _, s := range []verifySection{r.Compiler, r.Runtime, r.Lifecycle} {
		counts.Pass += s.Summary.Pass
		counts.Fail += s.Summary.Fail
		counts.Inconclusive += s.Summary.Inconclusive
	}
	if counts != r.Summary {
		return fmt.Errorf("summary mismatch: overall %+v != sections %+v", r.Summary, counts)
	}
	if !validStatuses[r.Status] {
		return fmt.Errorf("invalid overall status: %q", r.Status)
	}
	return nil
}

func validateSection(s verifySection) error {
	if len(s.Cases) == 0 {
		return fmt.Errorf("section has zero required cases")
	}
	if !validStatuses[s.Status] {
		return fmt.Errorf("invalid section status: %q", s.Status)
	}
	seen := map[string]bool{}
	statusCounts := verifySummary{}
	for _, c := range s.Cases {
		if seen[c.ID] {
			return fmt.Errorf("duplicate case ID: %q", c.ID)
		}
		seen[c.ID] = true
		if !validStatuses[c.Status] {
			return fmt.Errorf("invalid case status %q for %q", c.Status, c.ID)
		}
		if c.Status == "pass" || c.Status == "fail" {
			if c.SalesforceObservation == "" || c.GladeObservation == "" {
				return fmt.Errorf("pass/fail case %q missing dual observations: salesforce=%q glade=%q", c.ID, c.SalesforceObservation, c.GladeObservation)
			}
		}
		switch c.Status {
		case "pass":
			statusCounts.Pass++
		case "fail":
			statusCounts.Fail++
		case "inconclusive":
			statusCounts.Inconclusive++
		}
	}
	if statusCounts != s.Summary {
		return fmt.Errorf("section summary mismatch: summary %+v != cases %+v", s.Summary, statusCounts)
	}
	return nil
}

func validateRuntimeFixture(path string) error {
	_, err := parseRuntimeFixture(path)
	return err
}

// parseRuntimeFixture reads and validates the runtime fixture, returning its apiVersion.
func parseRuntimeFixture(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var fixture struct {
		APIVersion  string `json:"apiVersion"`
		Comparisons []struct {
			CaseID string `json:"caseId"`
		} `json:"comparisons"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		return "", fmt.Errorf("invalid runtime fixture JSON: %w", err)
	}
	if strings.TrimSpace(fixture.APIVersion) == "" {
		return "", fmt.Errorf("runtime fixture missing apiVersion")
	}
	if fixture.Comparisons == nil {
		return "", fmt.Errorf("runtime fixture missing comparisons array")
	}
	for _, c := range fixture.Comparisons {
		if strings.TrimSpace(c.CaseID) == "" {
			return "", fmt.Errorf("runtime fixture comparison missing caseId")
		}
	}
	return fixture.APIVersion, nil
}

func checkAPIVersionProvenance(rm releaseManifest, opts salesforceVerifyOptions) error {
	manifestVersion, err := parseAPIVersion("release manifest", rm.APIVersion)
	if err != nil {
		return err
	}
	rtVersion, err := parseRuntimeFixture(opts.RuntimeCases)
	if err != nil {
		return fmt.Errorf("runtime fixture: %w", err)
	}
	if err := requireAPIVersion("runtime fixture", rtVersion, manifestVersion, rm.APIVersion); err != nil {
		return err
	}

	projectData, err := os.ReadFile(filepath.Join(opts.TestProject, "sfdx-project.json"))
	if err != nil {
		return fmt.Errorf("read lifecycle project sfdx-project.json: %w", err)
	}
	var project struct {
		SourceAPIVersion string `json:"sourceApiVersion"`
	}
	if err := json.Unmarshal(projectData, &project); err != nil {
		return fmt.Errorf("invalid lifecycle project sfdx-project.json: %w", err)
	}
	if err := requireAPIVersion("lifecycle project sourceApiVersion", project.SourceAPIVersion, manifestVersion, rm.APIVersion); err != nil {
		return err
	}

	metaCount := 0
	err = filepath.WalkDir(filepath.Join(opts.TestProject, "force-app"), func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".cls-meta.xml") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var metadata struct {
			APIVersion string `xml:"apiVersion"`
		}
		if err := xml.Unmarshal(data, &metadata); err != nil {
			return fmt.Errorf("invalid Apex class metadata %s: %w", filepath.Base(path), err)
		}
		metaCount++
		return requireAPIVersion("Apex class metadata "+filepath.Base(path), metadata.APIVersion, manifestVersion, rm.APIVersion)
	})
	if err != nil {
		return err
	}
	if metaCount == 0 {
		return fmt.Errorf("lifecycle project has no Apex class metadata")
	}

	catalog, err := apexrules.LoadCatalog(opts.Catalog)
	if err != nil {
		return fmt.Errorf("compiler catalog: %w", err)
	}
	hasCurrentVersion := false
	for _, rule := range catalog.Rules {
		if rule.APIVersion > manifestVersion {
			return fmt.Errorf("compiler rule %s apiVersion %.1f is newer than release manifest %s", rule.ID, rule.APIVersion, rm.APIVersion)
		}
		hasCurrentVersion = hasCurrentVersion || rule.APIVersion == manifestVersion
	}
	if !hasCurrentVersion {
		return fmt.Errorf("compiler catalog has no rule for release manifest apiVersion %s", rm.APIVersion)
	}
	return nil
}

func parseAPIVersion(label, value string) (float64, error) {
	version, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || version <= 0 {
		return 0, fmt.Errorf("%s has invalid apiVersion %q", label, value)
	}
	return version, nil
}

func requireAPIVersion(label, value string, want float64, wantText string) error {
	version, err := parseAPIVersion(label, value)
	if err != nil {
		return err
	}
	if version != want {
		return fmt.Errorf("%s %q does not match release manifest %q", label, value, wantText)
	}
	return nil
}

func errorCases(ids []string) []verifyCase {
	cases := make([]verifyCase, len(ids))
	for i, id := range ids {
		cases[i] = verifyCase{ID: id, Status: "inconclusive", Observation: "execution-error", Category: "operational"}
	}
	return cases
}

// inconclusiveSection returns a verifySection where every required case is
// inconclusive. Used when an acquired report fails identity validation.
func inconclusiveSection(required []oracleprobe.Case) verifySection {
	ids := make([]string, len(required))
	for i, c := range required {
		ids[i] = c.ID
	}
	return verifySection{
		Status:  "inconclusive",
		Cases:   errorCases(ids),
		Summary: verifySummary{Inconclusive: len(required)},
	}
}

// validateAcquiredReport checks that every required case ID appears exactly once.
// Duplicate IDs, unknown IDs, or missing IDs force an error so the section
// becomes operationally inconclusive.
func validateAcquiredReport(report oracleprobe.Report, required []oracleprobe.Case) error {
	if len(report.Results) != len(required) {
		return fmt.Errorf("result count %d != required %d", len(report.Results), len(required))
	}
	seen := map[string]bool{}
	for _, r := range report.Results {
		if seen[r.ID] {
			return fmt.Errorf("duplicate result ID %q", r.ID)
		}
		seen[r.ID] = true
	}
	reqSet := map[string]bool{}
	for _, c := range required {
		reqSet[c.ID] = true
	}
	for _, r := range report.Results {
		if !reqSet[r.ID] {
			return fmt.Errorf("unknown result ID %q", r.ID)
		}
	}
	for _, c := range required {
		if !seen[c.ID] {
			return fmt.Errorf("missing required case ID %q", c.ID)
		}
	}
	return nil
}

// ----- help -----

func printSalesforceVerifyHelp(w io.Writer) {
	fmt.Fprint(w, `Run a unified Salesforce correctness verification across compiler,
runtime, and lifecycle sections. Writes one atomic JSON artifact.

Usage:
  glade-tools salesforce verify [flags]

Required flags:
  --release-manifest <path>   Release manifest (salesforce-release-current.json).
  --catalog <path>            Compiler catalog JSON.
  --runtime-cases <path>      Runtime fixture/provenance file.
  --test-project <path>       Project root for lifecycle oracle probes.
  --target-org <alias>        Target Salesforce org alias.
  --glade-bin <path>          Candidate Glade binary.
  --glade-root <path>         Glade source root for Git provenance.
  --out <path>                Output JSON artifact path.

Optional flags:
  --developer                 Permit dirty source roots (recorded in artifact).
`)
}
