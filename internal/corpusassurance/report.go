package corpusassurance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// AssuranceReport is the public, neutral release outcome projection.
type AssuranceReport struct {
	SchemaVersion int                   `json:"schemaVersion"`
	Rows          []AssuranceSurfaceRow `json:"rows"`
}

// AssuranceReceipt deliberately does not hash itself, keeping the sealed
// artifact graph acyclic.
type AssuranceReceipt struct {
	SchemaVersion   int               `json:"schemaVersion"`
	AssuranceSHA256 string            `json:"assuranceSha256"`
	HTMLSHA256      string            `json:"htmlSha256"`
	InputsSHA256    map[string]string `json:"inputsSha256"`
	ReceiptSHA256   string            `json:"receiptSha256,omitempty"`
}

// AssuranceReportRequest names every direct, sealed input required to publish
// the final public readiness projection.
type AssuranceReportRequest struct {
	InventoryPath       string
	RootManifestPath    string
	UsagePath           string
	ProfilePath         string
	FixtureManifestPath string
	ReplayPath          string
	LocalProofPath      string
	OraclePlanPath      string
	AuthorityPath       string
	BundlePath          string
	SalesforceFiles     []SalesforceShardFiles
	JSONPath            string
	HTMLPath            string
	ReceiptPath         string
}

// BuildAssuranceReport derives public outcomes only from sealed files, then
// writes the report, explorer, and acyclic receipt once.
func BuildAssuranceReport(request AssuranceReportRequest) (AssuranceReceipt, error) {
	paths := []string{request.InventoryPath, request.RootManifestPath, request.UsagePath, request.ProfilePath, request.FixtureManifestPath, request.ReplayPath, request.LocalProofPath, request.OraclePlanPath, request.AuthorityPath, request.BundlePath, request.JSONPath, request.HTMLPath, request.ReceiptPath}
	for _, path := range paths {
		if !filepath.IsAbs(path) {
			return AssuranceReceipt{}, fmt.Errorf("absolute direct assurance paths are required")
		}
	}
	if len(request.SalesforceFiles) == 0 {
		return AssuranceReceipt{}, fmt.Errorf("Salesforce lifecycle evidence is required")
	}
	inventory, inventoryBytes, err := readInventorySpec(request.InventoryPath)
	if err != nil {
		return AssuranceReceipt{}, err
	}
	root, rootBytes, err := readExactJSONBytes[InventoryManifest](request.RootManifestPath)
	if err != nil || root.InventorySHA256 != replayBytesSHA256(inventoryBytes) || ValidateInventoryCoverage(inventory, root.Repositories) != nil {
		return AssuranceReceipt{}, fmt.Errorf("root manifest does not bind frozen inventory")
	}
	usage, usageBytes, err := readExactJSONBytes[SealedCorpusUsage](request.UsagePath)
	if err != nil || usage.InventorySHA256 != replayBytesSHA256(inventoryBytes) || usage.RootManifestSHA256 != replayBytesSHA256(rootBytes) {
		return AssuranceReceipt{}, fmt.Errorf("sealed usage does not bind frozen inventory")
	}
	profile, profileBytes, err := readExactJSONBytes[AssuranceProfile](request.ProfilePath)
	if err != nil || profile.SchemaVersion != 1 || profile.SealedUsageSHA256 != replayBytesSHA256(usageBytes) {
		return AssuranceReceipt{}, fmt.Errorf("assurance profile does not bind sealed usage")
	}
	manifest, manifestBytes, err := readExactJSONBytes[LocalProofFixtureManifest](request.FixtureManifestPath)
	if err != nil || profile.FixtureManifestSHA256 != replayBytesSHA256(manifestBytes) {
		return AssuranceReceipt{}, fmt.Errorf("fixture manifest does not bind assurance profile")
	}
	proof, proofBytes, err := readExactJSONBytes[LocalProof](request.LocalProofPath)
	if err != nil || profile.LocalProofSHA256 != replayBytesSHA256(proofBytes) || ValidateLocalProof(proof, manifest) != nil {
		return AssuranceReceipt{}, fmt.Errorf("local proof does not bind assurance profile")
	}
	plan, planBytes, err := readExactJSONBytes[OraclePlan](request.OraclePlanPath)
	if err != nil || plan.ProfileSHA256 != replayBytesSHA256(profileBytes) || plan.SealedUsageSHA256 != replayBytesSHA256(usageBytes) || plan.LocalProofSHA256 != replayBytesSHA256(proofBytes) {
		return AssuranceReceipt{}, fmt.Errorf("oracle plan does not bind current evidence")
	}
	authority, authorityBytes, err := readExactJSONBytes[ExclusionAuthority](request.AuthorityPath)
	if err != nil || authority.PlanSHA256 != replayBytesSHA256(planBytes) || authority.ProfileSHA256 != replayBytesSHA256(profileBytes) || authority.SealedUsageSHA256 != replayBytesSHA256(usageBytes) || authority.LocalProofSHA256 != replayBytesSHA256(proofBytes) || authority.SalesforceParityCredit != 0 {
		return AssuranceReceipt{}, fmt.Errorf("exclusion authority does not bind current evidence")
	}
	if err := ValidateOracleBundle(request.BundlePath); err != nil {
		return AssuranceReceipt{}, err
	}
	bundle, bundleBytes, err := readExactJSONBytes[OracleBundle](request.BundlePath)
	if err != nil || bundle.OraclePlanSHA256 != replayBytesSHA256(planBytes) || bundle.Candidate != plan.Candidate || bundle.Tools != plan.Tools {
		return AssuranceReceipt{}, fmt.Errorf("oracle bundle does not bind current plan")
	}
	stagedPlanPath := filepath.Join(filepath.Dir(request.BundlePath), "ORACLE_PLAN.json")
	if err := ValidateSalesforceShardFiles(stagedPlanPath, request.SalesforceFiles); err != nil {
		return AssuranceReceipt{}, err
	}
	shards := make([]SalesforceShard, 0, len(request.SalesforceFiles))
	for _, files := range request.SalesforceFiles {
		shard, _, err := readExactJSONBytes[SalesforceShard](files.ShardPath)
		if err != nil {
			return AssuranceReceipt{}, err
		}
		shards = append(shards, shard)
	}
	merge, mergeBytes, err := readExactJSONBytes[ReplayMerge](request.ReplayPath)
	if err != nil || merge.RootManifestSHA256 != replayBytesSHA256(rootBytes) || merge.Candidate != plan.Candidate || merge.Tools != plan.Tools || len(merge.TestReadyByRepository) != len(root.Repositories) {
		return AssuranceReceipt{}, fmt.Errorf("replay merge does not bind current evidence")
	}
	rows, err := deriveAssuranceRows(usage.Reconciliation, profile, proof, plan, shards, merge.TestReadyByRepository)
	if err != nil {
		return AssuranceReceipt{}, err
	}
	inputs := map[string]string{request.InventoryPath: replayBytesSHA256(inventoryBytes), request.RootManifestPath: replayBytesSHA256(rootBytes), request.UsagePath: replayBytesSHA256(usageBytes), request.ProfilePath: replayBytesSHA256(profileBytes), request.FixtureManifestPath: replayBytesSHA256(manifestBytes), request.ReplayPath: replayBytesSHA256(mergeBytes), request.LocalProofPath: replayBytesSHA256(proofBytes), request.OraclePlanPath: replayBytesSHA256(planBytes), request.AuthorityPath: replayBytesSHA256(authorityBytes), request.BundlePath: replayBytesSHA256(bundleBytes)}
	for _, files := range request.SalesforceFiles {
		for _, path := range []string{files.ShardPath, files.DispatchPath, files.CreationPath, files.CleanupPath} {
			hash, err := sha256File(path)
			if err != nil {
				return AssuranceReceipt{}, err
			}
			inputs[path] = hash
		}
	}
	return writeAssuranceArtifacts(AssuranceReport{SchemaVersion: 1, Rows: rows}, request.JSONPath, request.HTMLPath, request.ReceiptPath, inputs)
}

// AssuranceSurfaceRow is the public, neutral per-surface release outcome.
// Readiness is cumulative: runtime parity implies test and compile readiness;
// explicit non-parity is mutually exclusive with all readiness claims.
type AssuranceSurfaceRow struct {
	Namespace          string   `json:"namespace"`
	SurfaceID          string   `json:"surfaceId"`
	UsageKeys          []string `json:"usageKeys"`
	RepositoryIDs      []string `json:"repositoryIds"`
	PrivateProdRefs    int      `json:"privateProdRefs"`
	PrivateTestRefs    int      `json:"privateTestRefs"`
	Disposition        string   `json:"disposition"`
	LocalEvidence      string   `json:"localEvidence"`
	SalesforceAction   string   `json:"salesforceAction"`
	SalesforceEvidence string   `json:"salesforceEvidence"`
	ExclusionClass     string   `json:"exclusionClass,omitempty"`
	ExclusionReason    string   `json:"exclusionReason,omitempty"`
	FixtureIDs         []string `json:"fixtureIds"`
	CompileReady       bool     `json:"compileReady"`
	TestReady          bool     `json:"testReady"`
	RuntimeParityReady bool     `json:"runtimeParityReady"`
	NonParity          bool     `json:"nonParity"`
}

func ValidateAssuranceOutcomes(rows []AssuranceSurfaceRow) error {
	if len(rows) == 0 {
		return fmt.Errorf("assurance outcome rows are required")
	}
	seen := make(map[string]bool, len(rows))
	for _, row := range rows {
		if row.SurfaceID == "" || seen[row.SurfaceID] {
			return fmt.Errorf("invalid or duplicate assurance surface %q", row.SurfaceID)
		}
		seen[row.SurfaceID] = true
		if row.NonParity {
			if row.CompileReady || row.TestReady || row.RuntimeParityReady || row.ExclusionClass == "" || row.ExclusionReason == "" {
				return fmt.Errorf("invalid non-parity outcome for %q", row.SurfaceID)
			}
			continue
		}
		if !row.CompileReady || (row.TestReady && !row.CompileReady) || (row.RuntimeParityReady && !row.TestReady) {
			return fmt.Errorf("invalid readiness outcome for %q", row.SurfaceID)
		}
		if row.ExclusionClass != "" || row.ExclusionReason != "" {
			return fmt.Errorf("parity outcome carries exclusion for %q", row.SurfaceID)
		}
	}
	return nil
}

func repositoryTestReadiness(merge ReplayMerge, shards []ReplayShard) (map[string]bool, error) {
	repositories := make(map[string]RepositorySpec, len(merge.Repositories))
	for _, repository := range merge.Repositories {
		if repository.ID == "" || repositories[repository.ID].ID != "" || (repository.LocalTests != "required" && repository.LocalTests != "none") {
			return nil, fmt.Errorf("invalid replay repository %q", repository.ID)
		}
		repositories[repository.ID] = repository
	}
	ready := make(map[string]bool, len(repositories))
	for _, shard := range shards {
		for _, result := range shard.Repositories {
			repository, exists := repositories[result.RepositoryID]
			if !exists || ready[result.RepositoryID] || !result.Check.Passed || result.Check.ExitCode != 0 {
				return nil, fmt.Errorf("invalid replay result for %q", result.RepositoryID)
			}
			if repository.LocalTests == "required" {
				if result.LocalTest == nil || !result.LocalTest.Passed || result.LocalTest.ExitCode != 0 {
					return nil, fmt.Errorf("required local test failed for %q", result.RepositoryID)
				}
				ready[result.RepositoryID] = true
			} else if result.LocalTest != nil {
				return nil, fmt.Errorf("repository %q unexpectedly ran local tests", result.RepositoryID)
			} else {
				ready[result.RepositoryID] = false
			}
		}
	}
	if len(ready) != len(repositories) {
		return nil, fmt.Errorf("replay result coverage is incomplete")
	}
	return ready, nil
}

// deriveAssuranceRows joins only the sealed reconciliation inputs. It never
// accepts a caller-selected surface list; every mapped usage surface gets one
// explicit readiness or non-parity outcome.
func deriveAssuranceRows(usage UsageReconciliation, profile AssuranceProfile, proof LocalProof, plan OraclePlan, shards []SalesforceShard, repositoryTests map[string]bool) ([]AssuranceSurfaceRow, error) {
	type usageAggregate struct {
		namespace    string
		usageKeys    []string
		repositories map[string]bool
		prod, test   int
	}
	aggregates := map[string]*usageAggregate{}
	for _, entry := range usage.Usage {
		switch entry.Class {
		case usageClassExact, usageClassCaseAlias, usageClassAggregateParent, usageClassCanonicalAlias:
			if entry.SurfaceID == "" || entry.Namespace == "" || len(entry.RepositoryIDs) == 0 {
				return nil, fmt.Errorf("mapped usage %q is incomplete", entry.UsageKey)
			}
			row := aggregates[entry.SurfaceID]
			if row == nil {
				row = &usageAggregate{namespace: entry.Namespace, repositories: map[string]bool{}}
				aggregates[entry.SurfaceID] = row
			}
			if row.namespace != entry.Namespace {
				return nil, fmt.Errorf("surface %q has conflicting namespaces", entry.SurfaceID)
			}
			row.usageKeys = append(row.usageKeys, entry.UsageKey)
			row.prod += entry.PrivateProdRefs
			row.test += entry.PrivateTestRefs
			for _, repositoryID := range entry.RepositoryIDs {
				row.repositories[repositoryID] = true
			}
		case usageClassLocalSymbol, usageClassNonSalesforceGenerated:
			if entry.SurfaceID != "" {
				return nil, fmt.Errorf("non-Salesforce usage %q selects a surface", entry.UsageKey)
			}
		default:
			return nil, fmt.Errorf("unknown usage class %q", entry.Class)
		}
	}
	if len(aggregates) == 0 {
		return nil, fmt.Errorf("no mapped assurance surfaces")
	}
	profiles := map[string]AssuranceProfileRow{}
	for _, row := range profile.Rows {
		if row.SurfaceID == "" || row.Disposition == "" || profiles[row.SurfaceID].SurfaceID != "" {
			return nil, fmt.Errorf("invalid or duplicate assurance profile surface %q", row.SurfaceID)
		}
		profiles[row.SurfaceID] = row
	}
	plans := map[string]OraclePlanRow{}
	for _, row := range plan.Rows {
		if row.SurfaceID == "" || plans[row.SurfaceID].SurfaceID != "" {
			return nil, fmt.Errorf("invalid or duplicate oracle plan surface %q", row.SurfaceID)
		}
		plans[row.SurfaceID] = row
	}
	proofs := map[string]LocalSurfaceProof{}
	for _, row := range proof.Surfaces {
		if row.SurfaceID == "" || proofs[row.SurfaceID].SurfaceID != "" {
			return nil, fmt.Errorf("invalid or duplicate local proof surface %q", row.SurfaceID)
		}
		proofs[row.SurfaceID] = row
	}
	remote := map[string]SalesforceSurfaceResult{}
	for _, shard := range shards {
		for _, row := range shard.Results {
			if row.SurfaceID == "" || remote[row.SurfaceID].SurfaceID != "" {
				return nil, fmt.Errorf("invalid or duplicate Salesforce surface %q", row.SurfaceID)
			}
			remote[row.SurfaceID] = row
		}
	}
	ids := make([]string, 0, len(aggregates))
	for surfaceID := range aggregates {
		ids = append(ids, surfaceID)
	}
	sort.Strings(ids)
	rows := make([]AssuranceSurfaceRow, 0, len(ids))
	for _, surfaceID := range ids {
		aggregate, profileRow, planRow := aggregates[surfaceID], profiles[surfaceID], plans[surfaceID]
		if profileRow.SurfaceID == "" || planRow.SurfaceID == "" {
			return nil, fmt.Errorf("surface %q lacks profile or oracle plan", surfaceID)
		}
		repositories := make([]string, 0, len(aggregate.repositories))
		testReady := true
		for repositoryID := range aggregate.repositories {
			ready, exists := repositoryTests[repositoryID]
			if !exists {
				return nil, fmt.Errorf("surface %q lacks replay test outcome for %q", surfaceID, repositoryID)
			}
			repositories, testReady = append(repositories, repositoryID), testReady && ready
		}
		sort.Strings(repositories)
		sort.Strings(aggregate.usageKeys)
		row := AssuranceSurfaceRow{Namespace: profileRow.Namespace, SurfaceID: surfaceID, UsageKeys: append([]string(nil), aggregate.usageKeys...), RepositoryIDs: repositories, PrivateProdRefs: aggregate.prod, PrivateTestRefs: aggregate.test, Disposition: profileRow.Disposition, SalesforceAction: planRow.Action}
		if row.Namespace == "" {
			row.Namespace = aggregate.namespace
		}
		local := proofs[surfaceID]
		if local.SurfaceID != "" {
			row.FixtureIDs = []string{local.FixtureID}
		}
		switch planRow.Action {
		case oracleRuntime:
			if local.SurfaceID == "" || local.Disposition != profileRow.Disposition || !local.RuntimeObserved || !(local.CompilePassed || local.CheckPassed) || remote[surfaceID].Kind != oracleRuntime || !remote[surfaceID].Passed {
				return nil, fmt.Errorf("runtime surface %q lacks complete local or Salesforce evidence", surfaceID)
			}
			row.LocalEvidence, row.SalesforceEvidence = "runtime", "runtime"
			row.CompileReady, row.TestReady = true, testReady
			row.RuntimeParityReady = row.TestReady
		case oracleCompile:
			if local.SurfaceID == "" || local.Disposition != profileRow.Disposition || !(local.CompilePassed || local.CheckPassed) || remote[surfaceID].Kind != oracleCompile || !remote[surfaceID].Passed {
				return nil, fmt.Errorf("compile surface %q lacks complete local or Salesforce evidence", surfaceID)
			}
			row.LocalEvidence, row.SalesforceEvidence = "compile", "compile"
			row.CompileReady, row.TestReady = true, testReady
		case oracleLocalContractOnly, oracleWaiver:
			if planRow.ExclusionClass == "" || planRow.ExclusionReason == "" || remote[surfaceID].SurfaceID != "" {
				return nil, fmt.Errorf("non-parity surface %q lacks an explicit exclusion", surfaceID)
			}
			row.LocalEvidence, row.SalesforceEvidence = "local-contract", "non-parity"
			row.ExclusionClass, row.ExclusionReason, row.NonParity = planRow.ExclusionClass, planRow.ExclusionReason, true
		default:
			return nil, fmt.Errorf("surface %q has unsupported oracle action %q", surfaceID, planRow.Action)
		}
		rows = append(rows, row)
	}
	return rows, ValidateAssuranceOutcomes(rows)
}

// WriteAssuranceArtifacts writes the report, its offline explorer, and then
// the acyclic receipt. All targets are preflighted to avoid known no-clobber
// failures before the first file is created.
func WriteAssuranceArtifacts(report AssuranceReport, jsonPath, htmlPath, receiptPath string) (AssuranceReceipt, error) {
	return writeAssuranceArtifacts(report, jsonPath, htmlPath, receiptPath, map[string]string{})
}

func writeAssuranceArtifacts(report AssuranceReport, jsonPath, htmlPath, receiptPath string, inputs map[string]string) (AssuranceReceipt, error) {
	if report.SchemaVersion != 1 {
		return AssuranceReceipt{}, fmt.Errorf("unsupported assurance report schema version %d", report.SchemaVersion)
	}
	if err := ValidateAssuranceOutcomes(report.Rows); err != nil {
		return AssuranceReceipt{}, err
	}
	reportJSON, err := json.Marshal(report)
	if err != nil {
		return AssuranceReceipt{}, err
	}
	if containsPrivateReportPath(reportJSON) {
		return AssuranceReceipt{}, fmt.Errorf("assurance report is not safe for public output")
	}
	paths := []string{jsonPath, htmlPath, receiptPath}
	for index, path := range paths {
		if !filepath.IsAbs(path) {
			return AssuranceReceipt{}, fmt.Errorf("absolute assurance artifact paths are required")
		}
		for _, earlier := range paths[:index] {
			if path == earlier {
				return AssuranceReceipt{}, fmt.Errorf("assurance artifact paths must be distinct")
			}
		}
		if _, err := os.Lstat(path); err == nil {
			return AssuranceReceipt{}, fmt.Errorf("assurance artifact output already exists: %s", path)
		} else if !os.IsNotExist(err) {
			return AssuranceReceipt{}, err
		}
	}
	if err := WriteNewJSON(jsonPath, report); err != nil {
		return AssuranceReceipt{}, err
	}
	if err := WriteAssuranceHTML(jsonPath, htmlPath); err != nil {
		return AssuranceReceipt{}, err
	}
	assuranceHash, err := sha256File(jsonPath)
	if err != nil {
		return AssuranceReceipt{}, err
	}
	htmlHash, err := sha256File(htmlPath)
	if err != nil {
		return AssuranceReceipt{}, err
	}
	receipt := AssuranceReceipt{SchemaVersion: 1, AssuranceSHA256: assuranceHash, HTMLSHA256: htmlHash, InputsSHA256: inputs}
	if err := WriteNewJSON(receiptPath, receipt); err != nil {
		return AssuranceReceipt{}, err
	}
	return receipt, nil
}
