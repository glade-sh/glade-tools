package toolcli

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
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
	"github.com/glade-sh/glade/tools/internal/releasecontract"
	"github.com/glade-sh/glade/tools/internal/surfaceledger"
)

// ----- options -----

// salesforceVerifyOptions holds the parsed CLI arguments for salesforce verify.
type salesforceVerifyOptions struct {
	ReleaseManifest, Catalog, RuntimeCases, TestProject                                  string
	ReleaseContract, ProductVersionProof                                                 string
	TargetOrg, GladeBin, GladeRoot, Out                                                  string
	Developer                                                                            bool
	PreviousReleaseManifest, PreviousInventory, CurrentInventory, ReleaseClassifications string
	releaseSourceVersions                                                                []string
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

const verifyReportSchemaVersion = 2

type verifyReport struct {
	SchemaVersion       int                    `json:"schemaVersion"`
	Release             string                 `json:"release"`
	APIVersion          string                 `json:"apiVersion"`
	Status              string                 `json:"status"`
	Glade               verifyGitProvenance    `json:"glade"`
	GladeTools          verifyGitProvenance    `json:"gladeTools"`
	Candidate           verifyCandidate        `json:"candidate"`
	Inputs              []verifyInput          `json:"inputs"`
	ReleaseDelta        *verifyReleaseDelta    `json:"releaseDelta,omitempty"`
	ReleaseCompleteness releasecontract.Report `json:"releaseCompleteness"`
	Compiler            verifySection          `json:"compiler"`
	Runtime             verifySection          `json:"runtime"`
	Lifecycle           verifySection          `json:"lifecycle"`
	Summary             verifySummary          `json:"summary"`
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
	SurfaceID    string   `json:"surfaceId"`
	Scope        string   `json:"scope"`
	Disposition  string   `json:"disposition"`
	CaseID       string   `json:"caseId,omitempty"`
	ReasonRef    string   `json:"reasonRef,omitempty"`
	ProductTests []string `json:"productTests,omitempty"`
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
	ID                    string   `json:"id"`
	Status                string   `json:"status"`
	SurfaceIDs            []string `json:"surfaceIds,omitempty"`
	Observation           string   `json:"observation,omitempty"`
	Category              string   `json:"category,omitempty"`
	SalesforceObservation string   `json:"salesforceObservation,omitempty"`
	GladeObservation      string   `json:"gladeObservation,omitempty"`
}

type verifySummary struct {
	Required     int `json:"required"`
	Pass         int `json:"pass"`
	Fail         int `json:"fail"`
	Inconclusive int `json:"inconclusive"`
}

type productVersionProof struct {
	SchemaVersion    int      `json:"schemaVersion"`
	GladeCommit      string   `json:"gladeCommit"`
	Status           string   `json:"status"`
	Command          []string `json:"command"`
	TestEvents       string   `json:"testEvents"`
	TestEventsSHA256 string   `json:"testEventsSHA256"`
}

type productTestEvidence struct {
	gladeRoot string
	module    string
	passes    map[string]bool
	bindings  map[string]productTestBinding
}

type productTestBinding struct {
	packagePath string
	testName    string
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
		case "--release-contract", "--product-version-proof", "--catalog", "--runtime-cases", "--test-project",
			"--target-org", "--glade-bin", "--glade-root", "--out":
			if seen[flag] {
				return opts, fmt.Errorf("duplicate flag: %s", flag)
			}
			if i+1 >= len(args) {
				return opts, fmt.Errorf("required value missing for %s", flag)
			}
			val := args[i+1]
			switch flag {
			case "--release-contract":
				opts.ReleaseContract = val
			case "--product-version-proof":
				opts.ProductVersionProof = val
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
		"--release-contract", "--product-version-proof", "--catalog", "--runtime-cases",
		"--test-project", "--target-org", "--glade-bin", "--glade-root", "--out",
	} {
		if !seen[required] {
			return opts, fmt.Errorf("required flag missing: %s", required)
		}
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
	var releaseAnalysis *releasecontract.Analysis
	if opts.ReleaseContract != "" {
		analysis, err := releasecontract.Analyze(opts.ReleaseContract)
		if err != nil {
			return report, fmt.Errorf("release contract: %w", err)
		}
		if len(analysis.Contract.Releases) == 0 {
			return report, fmt.Errorf("release contract has no releases")
		}
		contractRoot, err := filepath.Abs(filepath.Dir(opts.ReleaseContract))
		if err != nil {
			return report, err
		}
		opts.ReleaseManifest = filepath.Join(contractRoot, analysis.Contract.Releases[len(analysis.Contract.Releases)-1].Manifest)
		releaseAnalysis = &analysis
		report.ReleaseCompleteness = analysis.Report
		for _, version := range analysis.Contract.Windows.Source {
			opts.releaseSourceVersions = append(opts.releaseSourceVersions, version.Version)
		}
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
	if releaseAnalysis != nil {
		for _, path := range []string{opts.ReleaseContract, opts.ProductVersionProof} {
			if _, err := os.Stat(path); err != nil {
				return report, fmt.Errorf("input file not found: %s", path)
			}
		}
	}

	// 2. Load and validate release manifest
	rm, err := loadReleaseManifest(opts.ReleaseManifest)
	if err != nil {
		return report, fmt.Errorf("release manifest: %v", err)
	}
	report.Release = rm.Release
	report.APIVersion = rm.APIVersion

	var releaseDelta *verifyReleaseDelta
	if releaseAnalysis == nil && opts.deltaMode() {
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
	var productEvidence *productTestEvidence

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
	if releaseAnalysis != nil {
		evidence, err := loadProductTestEvidence(opts.ProductVersionProof, opts.GladeRoot, gladeCommit)
		if err != nil {
			return report, fmt.Errorf("product version proof: %w", err)
		}
		productEvidence = &evidence
		if err := preflightReleaseBindings(*releaseAnalysis, opts.Catalog, productEvidence); err != nil {
			return report, fmt.Errorf("release bindings: %w", err)
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
	if releaseAnalysis != nil {
		contractSHA, err := sha256File(opts.ReleaseContract)
		if err != nil {
			return report, err
		}
		proofSHA, err := sha256File(opts.ProductVersionProof)
		if err != nil {
			return report, err
		}
		report.Inputs = append(report.Inputs,
			verifyInput{Kind: "release-contract", SHA256: contractSHA},
			verifyInput{Kind: "product-version-proof", SHA256: proofSHA},
		)
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
	if releaseAnalysis != nil {
		report.ReleaseCompleteness = creditReleaseEvidence(*releaseAnalysis, report, productEvidence)
	}

	// 10. Candidate identity: SHA256 after
	candidateAfter, err := sha256File(opts.GladeBin)
	if err != nil {
		return report, err
	}
	report.Candidate.SHA256After = candidateAfter

	// 11. Overall status and summary
	report.Summary = verifySummary{
		Required:     report.Compiler.Summary.Required + report.Runtime.Summary.Required + report.Lifecycle.Summary.Required,
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
	} else if releaseAnalysis != nil && report.ReleaseCompleteness.Status != "pass" {
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
	if len(opts.releaseSourceVersions) > 0 {
		filtered := catalog.Rules[:0]
		for _, rule := range catalog.Rules {
			if slices.Contains(opts.releaseSourceVersions, fmt.Sprintf("%.1f", rule.APIVersion)) {
				filtered = append(filtered, rule)
			}
		}
		catalog.Rules = filtered
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
		section.Summary.Required = len(catalog.Rules)
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
		section.Summary.Required = len(catalog.Rules)
		section.Summary.Inconclusive = len(catalog.Rules)
		return section
	}

	results := apexrules.CompareObserved(catalog.Rules, sfResults, gladeResults)
	rulesByID := make(map[string]apexrules.Rule, len(catalog.Rules))
	for _, rule := range catalog.Rules {
		rulesByID[rule.ID] = rule
	}
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
			SurfaceIDs:            append([]string(nil), rulesByID[r.ID].SurfaceIDs...),
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
	section.Summary = verifySummary{Required: len(catalog.Rules), Pass: pass, Fail: fail, Inconclusive: inconclusive}
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
			SurfaceIDs:            append([]string(nil), cmp.SurfaceIDs...),
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
	section.Summary = verifySummary{Required: len(cases), Pass: pass, Fail: fail, Inconclusive: inconclusive}
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
		fixtureCases := make([]verifyCase, len(cases))
		for i, c := range cases {
			fixtureCases[i] = verifyCase{
				ID:         c.ID,
				Status:     "inconclusive",
				Category:   "fixture-failed",
				SurfaceIDs: append([]string(nil), c.SurfaceIDs...),
			}
		}
		return verifySection{
			Status:  "inconclusive",
			Cases:   fixtureCases,
			Summary: verifySummary{Required: len(cases), Inconclusive: len(cases)},
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
			SurfaceIDs:            append([]string(nil), cmp.SurfaceIDs...),
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
	section.Summary = verifySummary{Required: len(cases), Pass: pass, Fail: fail, Inconclusive: inconclusive}
	if fail > 0 {
		section.Status = "fail"
	} else if inconclusive > 0 {
		section.Status = "inconclusive"
	}
	return section
}

type releaseCaseDefinition struct {
	surfaceIDs []string
}

func preflightReleaseBindings(analysis releasecontract.Analysis, catalogPath string, evidence *productTestEvidence) error {
	catalog, err := apexrules.LoadCatalog(catalogPath)
	if err != nil {
		return err
	}
	definitions := map[string][]releaseCaseDefinition{}
	for _, rule := range catalog.Rules {
		definitions[rule.ID] = append(definitions[rule.ID], releaseCaseDefinition{surfaceIDs: rule.SurfaceIDs})
	}
	for _, cases := range [][]oracleprobe.Case{oracleprobe.StdlibCases(), oracleprobe.ProjectOracleCases()} {
		for _, item := range cases {
			definitions[item.ID] = append(definitions[item.ID], releaseCaseDefinition{surfaceIDs: item.SurfaceIDs})
		}
	}
	requireCase := func(id string) (releaseCaseDefinition, error) {
		matches := definitions[id]
		if len(matches) != 1 {
			return releaseCaseDefinition{}, fmt.Errorf("case %q has %d definitions", id, len(matches))
		}
		return matches[0], nil
	}
	for _, change := range analysis.SurfaceChanges {
		classification := change.Classification
		if classification == nil || classification.CaseID == "" {
			continue
		}
		definition, err := requireCase(classification.CaseID)
		if err != nil {
			return err
		}
		if !containsCanonicalSurface(definition.surfaceIDs, classification.SurfaceID) {
			return fmt.Errorf("case %q does not carry surface %q", classification.CaseID, classification.SurfaceID)
		}
	}
	for _, behavior := range analysis.Contract.Behaviors {
		for _, id := range behavior.ProofCases {
			if _, err := requireCase(id); err != nil {
				return err
			}
		}
	}
	for _, window := range [][]releasecontract.VersionProof{analysis.Contract.Windows.Source, analysis.Contract.Windows.Endpoint} {
		for _, entry := range window {
			for _, id := range entry.ProofCases {
				if _, err := requireCase(id); err != nil {
					return err
				}
			}
		}
	}
	for _, profile := range analysis.Contract.Windows.OrgProfiles {
		for _, id := range profile.ProofCases {
			if _, err := requireCase(id); err != nil {
				return err
			}
		}
	}
	for _, binding := range releaseProductTests(analysis) {
		if _, err := evidence.binding(binding); err != nil {
			return err
		}
	}
	return nil
}

func releaseProductTests(analysis releasecontract.Analysis) []string {
	var out []string
	for _, change := range analysis.SurfaceChanges {
		if change.Classification != nil {
			out = append(out, change.Classification.ProductTests...)
		}
	}
	for _, behavior := range analysis.Contract.Behaviors {
		out = append(out, behavior.ProductTests...)
	}
	for _, entry := range analysis.Contract.Windows.Source {
		out = append(out, entry.ProductTests...)
	}
	for _, entry := range analysis.Contract.Windows.Endpoint {
		out = append(out, entry.ProductTests...)
	}
	for _, entry := range analysis.Contract.Windows.OrgProfiles {
		out = append(out, entry.ProductTests...)
	}
	out = append(out, analysis.Contract.NoFallbackProductTests...)
	slices.Sort(out)
	return slices.Compact(out)
}

func creditReleaseEvidence(analysis releasecontract.Analysis, verify verifyReport, evidence *productTestEvidence) releasecontract.Report {
	report := analysis.Report
	report.SurfaceDelta.Implemented = 0
	report.SurfaceDelta.Proved = 0
	report.BehaviorDelta.Implemented = 0
	report.BehaviorDelta.Proved = 0
	passedCases := map[string][]verifyCase{}
	for _, section := range []verifySection{verify.Compiler, verify.Runtime, verify.Lifecycle} {
		for _, item := range section.Cases {
			passedCases[item.ID] = append(passedCases[item.ID], item)
		}
	}
	casePasses := func(id string) bool {
		matches := passedCases[id]
		return len(matches) == 1 && matches[0].Status == "pass"
	}
	caseProvesSurface := func(id, surfaceID string) bool {
		matches := passedCases[id]
		return len(matches) == 1 && matches[0].Status == "pass" && containsCanonicalSurface(matches[0].SurfaceIDs, surfaceID)
	}
	testsPass := func(bindings []string) bool {
		for _, binding := range bindings {
			if !evidence.passed(binding) {
				return false
			}
		}
		return true
	}
	for _, change := range analysis.SurfaceChanges {
		classification := change.Classification
		if classification == nil {
			continue
		}
		proved := testsPass(classification.ProductTests)
		if classification.CaseID != "" {
			proved = proved && caseProvesSurface(classification.CaseID, classification.SurfaceID)
		}
		if !proved {
			continue
		}
		report.SurfaceDelta.Proved++
		if classification.Disposition != surfaceledger.DispoExplicitUnsupported {
			report.SurfaceDelta.Implemented++
		}
	}
	for _, behavior := range analysis.Contract.Behaviors {
		proved := testsPass(behavior.ProductTests)
		for _, id := range behavior.ProofCases {
			proved = proved && casePasses(id)
		}
		if !proved {
			continue
		}
		report.BehaviorDelta.Proved++
		if behavior.Outcome != "explicit-non-parity" {
			report.BehaviorDelta.Implemented++
		}
	}
	report.SourceVersions.Passing = passingVersions(analysis.Contract.Windows.Source, casePasses, testsPass)
	report.EndpointVersions.Passing = passingVersions(analysis.Contract.Windows.Endpoint, casePasses, testsPass)
	for _, profile := range analysis.Contract.Windows.OrgProfiles {
		if proofsPass(profile.ProofCases, profile.ProductTests, casePasses, testsPass) {
			report.OrgProfiles.Passing = append(report.OrgProfiles.Passing, profile.Name)
		}
	}
	report.SilentFallbacks = 0
	for _, binding := range analysis.Contract.NoFallbackProductTests {
		if !evidence.passed(binding) {
			report.SilentFallbacks++
		}
	}
	if releaseCompletenessClosed(report) {
		report.Status = "pass"
	} else {
		report.Status = "fail"
	}
	return report
}

func passingVersions(entries []releasecontract.VersionProof, casePasses func(string) bool, testsPass func([]string) bool) []string {
	var passing []string
	for _, entry := range entries {
		if proofsPass(entry.ProofCases, entry.ProductTests, casePasses, testsPass) {
			passing = append(passing, entry.Version)
		}
	}
	return passing
}

func proofsPass(caseIDs, productTests []string, casePasses func(string) bool, testsPass func([]string) bool) bool {
	if !testsPass(productTests) {
		return false
	}
	for _, id := range caseIDs {
		if !casePasses(id) {
			return false
		}
	}
	return true
}

func containsCanonicalSurface(surfaceIDs []string, expected string) bool {
	expected = surfaceledger.CanonicalSurfaceIDKey(expected)
	for _, surfaceID := range surfaceIDs {
		if surfaceledger.CanonicalSurfaceIDKey(surfaceID) == expected {
			return true
		}
	}
	return false
}

func releaseCompletenessClosed(report releasecontract.Report) bool {
	return report.SurfaceDelta.Total == report.SurfaceDelta.Classified &&
		report.SurfaceDelta.Total == report.SurfaceDelta.Proved &&
		report.SurfaceDelta.Total == report.SurfaceDelta.Implemented+report.SurfaceDelta.ExplicitNonParity &&
		report.BehaviorDelta.Total == report.BehaviorDelta.Classified &&
		report.BehaviorDelta.Total == report.BehaviorDelta.Proved &&
		report.BehaviorDelta.Total == report.BehaviorDelta.Implemented+report.BehaviorDelta.ExplicitNonParity &&
		report.ChangeInventory.Total == report.ChangeInventory.Routed &&
		slices.Equal(report.SourceVersions.Advertised, report.SourceVersions.Passing) &&
		slices.Equal(report.EndpointVersions.Advertised, report.EndpointVersions.Passing) &&
		slices.Equal(report.OrgProfiles.Advertised, report.OrgProfiles.Passing) &&
		report.SilentFallbacks == 0 && len(report.Unclassified) == 0
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

func loadProductTestEvidence(proofPath, gladeRoot, gladeCommit string) (productTestEvidence, error) {
	var proof productVersionProof
	if err := readExactJSONFile(proofPath, &proof); err != nil {
		return productTestEvidence{}, err
	}
	root, err := filepath.Abs(gladeRoot)
	if err != nil {
		return productTestEvidence{}, err
	}
	root = filepath.Clean(root)
	if proof.SchemaVersion != 1 || proof.Status != "pass" || proof.GladeCommit != gladeCommit {
		return productTestEvidence{}, fmt.Errorf("proof identity or status mismatch")
	}
	wantCommand := []string{"go", "-C", root, "test", "-json", "-count=1", "-p", "4", "-timeout=30m", "./..."}
	if !slices.Equal(proof.Command, wantCommand) {
		return productTestEvidence{}, fmt.Errorf("proof command must be %q", wantCommand)
	}
	if filepath.IsAbs(proof.TestEvents) || strings.TrimSpace(proof.TestEvents) == "" {
		return productTestEvidence{}, fmt.Errorf("testEvents must be a relative path")
	}
	eventsPath, err := pathBelow(filepath.Dir(proofPath), proof.TestEvents)
	if err != nil {
		return productTestEvidence{}, err
	}
	digest, err := sha256File(eventsPath)
	if err != nil {
		return productTestEvidence{}, err
	}
	if digest != proof.TestEventsSHA256 {
		return productTestEvidence{}, fmt.Errorf("testEvents SHA-256 mismatch")
	}
	module, err := goModulePath(root)
	if err != nil {
		return productTestEvidence{}, err
	}
	file, err := os.Open(eventsPath)
	if err != nil {
		return productTestEvidence{}, err
	}
	defer file.Close()
	passes := map[string]bool{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var event struct {
			Action  string `json:"Action"`
			Package string `json:"Package"`
			Test    string `json:"Test"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return productTestEvidence{}, fmt.Errorf("invalid go test event: %w", err)
		}
		if event.Action == "pass" && event.Package != "" && event.Test != "" {
			passes[event.Package+"\x00"+event.Test] = true
		}
	}
	if err := scanner.Err(); err != nil {
		return productTestEvidence{}, err
	}
	return productTestEvidence{gladeRoot: root, module: module, passes: passes, bindings: map[string]productTestBinding{}}, nil
}

func (e *productTestEvidence) binding(raw string) (productTestBinding, error) {
	if binding, ok := e.bindings[raw]; ok {
		return binding, nil
	}
	fileName, testName, ok := strings.Cut(raw, ":")
	if !ok || strings.TrimSpace(fileName) == "" || strings.TrimSpace(testName) == "" {
		return productTestBinding{}, fmt.Errorf("invalid product test binding %q", raw)
	}
	path, err := pathBelow(e.gladeRoot, fileName)
	if err != nil {
		return productTestBinding{}, err
	}
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		return productTestBinding{}, fmt.Errorf("parse product test %q: %w", raw, err)
	}
	topLevel := strings.Split(testName, "/")[0]
	object := parsed.Scope.Lookup(topLevel)
	if object == nil {
		return productTestBinding{}, fmt.Errorf("product test %q is not a top-level Test function", raw)
	}
	declaration, isFunction := object.Decl.(*ast.FuncDecl)
	if !isFunction || object.Kind != ast.Fun || declaration.Recv != nil || !strings.HasPrefix(topLevel, "Test") {
		return productTestBinding{}, fmt.Errorf("product test %q is not a top-level Test function", raw)
	}
	directory, err := filepath.Rel(e.gladeRoot, filepath.Dir(path))
	if err != nil {
		return productTestBinding{}, err
	}
	packagePath := e.module
	if directory != "." {
		packagePath += "/" + filepath.ToSlash(directory)
	}
	binding := productTestBinding{packagePath: packagePath, testName: testName}
	e.bindings[raw] = binding
	return binding, nil
}

func (e *productTestEvidence) passed(raw string) bool {
	binding, err := e.binding(raw)
	return err == nil && e.passes[binding.packagePath+"\x00"+binding.testName]
}

func pathBelow(root, relative string) (string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if filepath.IsAbs(relative) {
		return "", fmt.Errorf("path %q must be relative", relative)
	}
	path := filepath.Join(root, filepath.Clean(relative))
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes root", relative)
	}
	return path, nil
}

func goModulePath(root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if fields := strings.Fields(line); len(fields) == 2 && fields[0] == "module" {
			return fields[1], nil
		}
	}
	return "", fmt.Errorf("go.mod has no module directive")
}

// ----- release manifest -----

func loadReleaseManifest(path string) (releaseManifest, error) {
	var rm releaseManifest
	if err := readExactJSONFile(path, &rm); err != nil {
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
	var prevInv, currInv apexdocs.Inventory
	if err := readExactJSONFile(opts.PreviousInventory, &prevInv); err != nil {
		return nil, fmt.Errorf("previous inventory: %w", err)
	}
	if err := readExactJSONFile(opts.CurrentInventory, &currInv); err != nil {
		return nil, fmt.Errorf("current inventory: %w", err)
	}
	if d := apexdocs.CanonicalDigest(prevInv); d != prev.Digest {
		return nil, fmt.Errorf("prev inventory digest != manifest")
	}
	if d := apexdocs.CanonicalDigest(currInv); d != curr.Digest {
		return nil, fmt.Errorf("current inventory digest != manifest")
	}
	prevLedger, err := surfaceledger.MergeReleaseSnapshot(surfaceledger.RowsFromDocsInventory(prevInv), prev.APIVersion)
	if err != nil {
		return nil, fmt.Errorf("previous inventory snapshot: %w", err)
	}
	currLedger, err := surfaceledger.MergeReleaseSnapshot(surfaceledger.RowsFromDocsInventory(currInv), curr.APIVersion)
	if err != nil {
		return nil, fmt.Errorf("current inventory snapshot: %w", err)
	}
	prevRows := prevLedger.Rows
	currRows := currLedger.Rows
	classFile, err := loadClassificationsFile(opts.ReleaseClassifications)
	if err != nil {
		return nil, err
	}
	if classFile.SchemaVersion != 1 && classFile.SchemaVersion != 2 {
		return nil, fmt.Errorf("classifications schemaVersion must be 1 or 2")
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
	var f releaseClassificationsFile
	if err := readExactJSONFile(path, &f); err != nil {
		return releaseClassificationsFile{}, fmt.Errorf("invalid classifications JSON: %w", err)
	}
	return f, nil
}

func readExactJSONFile(path string, value any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return releasecontract.DecodeExactJSON(data, value)
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
		if err := validateSection(s, r.SchemaVersion >= 2); err != nil {
			return err
		}
	}
	counts := verifySummary{}
	for _, s := range []verifySection{r.Compiler, r.Runtime, r.Lifecycle} {
		counts.Required += s.Summary.Required
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

func validateSection(s verifySection, requireRequired bool) error {
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
	if requireRequired {
		statusCounts.Required = len(s.Cases)
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
	cases := make([]verifyCase, len(required))
	for i, c := range required {
		cases[i] = verifyCase{ID: c.ID, Status: "inconclusive", Observation: "execution-error", Category: "operational", SurfaceIDs: append([]string(nil), c.SurfaceIDs...)}
	}
	return verifySection{
		Status:  "inconclusive",
		Cases:   cases,
		Summary: verifySummary{Required: len(required), Inconclusive: len(required)},
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
  --release-contract <path>   Salesforce release contract JSON.
  --product-version-proof <path>  Passing Glade product-test proof JSON.
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
