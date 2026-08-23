package corpusassurance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type SurfaceOracleIndexRequest struct {
	ScopePath         string
	RuntimeBatchRoots []string
	OutputPath        string
}

type SurfaceOracleIndex struct {
	SchemaVersion       int                              `json:"schemaVersion"`
	Kind                string                           `json:"kind"`
	ScopeSHA256         string                           `json:"scopeSha256"`
	SourceProfileSHA256 string                           `json:"sourceProfileSha256"`
	LedgerSHA256        string                           `json:"ledgerSha256"`
	PolicySHA256        string                           `json:"policySha256"`
	Candidate           SurfaceOracleIndexArtifact       `json:"candidate"`
	Tools               SurfaceOracleIndexArtifact       `json:"tools"`
	RuntimeBatches      []SurfaceOracleIndexRuntimeBatch `json:"runtimeBatches"`
	Total               int                              `json:"total"`
	Counts              SurfaceOracleIndexCounts         `json:"counts"`
	Rows                []SurfaceOracleIndexRow          `json:"rows"`
}

type SurfaceOracleIndexRuntimeBatch struct {
	ManifestSHA256          string   `json:"manifestSha256"`
	ProfileSHA256           string   `json:"profileSha256"`
	BindingsSHA256          string   `json:"bindingsSha256"`
	LocalSummarySHA256      string   `json:"localSummarySha256"`
	OracleResultsSHA256     string   `json:"oracleResultsSha256"`
	RawReconciliationSHA256 string   `json:"rawReconciliationSha256"`
	MismatchReviewSHA256    string   `json:"mismatchReviewSha256"`
	FinalAuditSHA256        string   `json:"finalAuditSha256"`
	SurfaceIDs              []string `json:"surfaceIds"`
	candidateCommit         string
	candidateSHA256         string
	toolsCommit             string
	toolsSHA256             string
}

type SurfaceOracleIndexArtifact struct {
	Commit       string `json:"commit"`
	BinarySHA256 string `json:"binarySha256"`
}

type SurfaceOracleIndexCounts struct {
	Adjudicated       int `json:"adjudicated"`
	Matched           int `json:"matched"`
	ExplicitNonParity int `json:"explicitNonParity"`
	ProductMismatch   int `json:"productMismatch"`
	Inconclusive      int `json:"inconclusive"`
	Open              int `json:"open"`
}

type SurfaceOracleIndexRow struct {
	SurfaceID string `json:"surfaceId"`
	State     string `json:"state"`
}

type surfaceRuntimeManifest struct {
	SchemaVersion   int                     `json:"schemaVersion"`
	Fixtures        []surfaceRuntimeFixture `json:"fixtures"`
	SelectionPolicy string                  `json:"selectionPolicy"`
	SurfaceRowCount int                     `json:"surfaceRowCount"`
}

type surfaceRuntimeFixture struct {
	Fixture            string   `json:"fixture"`
	Path               string   `json:"path"`
	RowCount           int      `json:"rowCount"`
	SalesforceEligible bool     `json:"salesforceEligible"`
	SHA256             string   `json:"sha256"`
	SourceFiles        []string `json:"sourceFiles"`
	SurfaceIDs         []string `json:"surfaceIds"`
}

type surfaceRuntimeBindings struct {
	CandidateCommit         string `json:"candidateCommit"`
	CandidateSHA256         string `json:"candidateSha256"`
	LocalSummarySHA256      string `json:"localSummarySha256"`
	ManifestSHA256          string `json:"manifestSha256"`
	ProfileSHA256           string `json:"profileSha256"`
	ScratchDefinitionSHA256 string `json:"scratchDefinitionSha256"`
	SFCLISHA256             string `json:"sfCliSha256"`
	ToolsCommit             string `json:"toolsCommit"`
	ToolsSHA256             string `json:"toolsSha256"`
	WorkflowScriptSHA256    string `json:"workflowScriptSha256"`
}

type surfaceLocalSummary struct {
	CandidateSHA256  string                      `json:"candidateSha256"`
	DurationMS       float64                     `json:"durationMs"`
	EndedAtUnixNS    int64                       `json:"endedAtUnixNs"`
	ManifestSHA256   string                      `json:"manifestSha256"`
	Results          []surfaceLocalSummaryResult `json:"results"`
	SchemaVersion    int                         `json:"schemaVersion"`
	Sealed           bool                        `json:"sealed"`
	SelectedFixtures int                         `json:"selectedFixtures"`
	SelectedRows     int                         `json:"selectedRows"`
	StartedAtUnixNS  int64                       `json:"startedAtUnixNs"`
}

type surfaceLocalSummaryResult struct {
	CandidateExitCode int     `json:"candidateExitCode"`
	CandidateStatus   string  `json:"candidateStatus"`
	DurationMS        float64 `json:"durationMs"`
	EndedAtUnixNS     int64   `json:"endedAtUnixNs"`
	ExitCode          int     `json:"exitCode"`
	Fixture           string  `json:"fixture"`
	Kind              string  `json:"kind"`
	Path              string  `json:"path"`
	Result            any     `json:"result"`
	StartedAtUnixNS   int64   `json:"startedAtUnixNs"`
	Status            string  `json:"status"`
	StderrSHA256      string  `json:"stderrSha256"`
	StdoutSHA256      string  `json:"stdoutSha256"`
}

type surfaceRawReconciliation struct {
	Counts               surfaceRawCounts              `json:"counts"`
	ManifestSHA256       string                        `json:"manifestSha256"`
	OrgPostflightMatched bool                          `json:"orgPostflightMatched"`
	Rows                 []surfaceRawReconciliationRow `json:"rows"`
	RunnerError          *string                       `json:"runnerError"`
	RuntimeRequested     bool                          `json:"runtimeRequested"`
	SchemaVersion        int                           `json:"schemaVersion"`
	Sealed               bool                          `json:"sealed"`
}

type surfaceRawCounts struct {
	Environment int `json:"environment"`
	Match       int `json:"match"`
	Mismatch    int `json:"mismatch"`
}

type surfaceRawReconciliationRow struct {
	Classification string               `json:"classification"`
	Fixture        string               `json:"fixture"`
	Local          surfaceRawLocal      `json:"local"`
	Reason         string               `json:"reason"`
	Salesforce     surfaceRawSalesforce `json:"salesforce"`
	SurfaceID      string               `json:"surfaceId"`
}

type surfaceRawLocal struct {
	CandidateExitCode int    `json:"candidateExitCode"`
	CandidateStatus   string `json:"candidateStatus"`
	Status            string `json:"status"`
}

type surfaceRawSalesforce struct {
	ComponentFailures []any  `json:"componentFailures"`
	Deployable        bool   `json:"deployable"`
	ExitCode          int    `json:"exitCode"`
	RuntimeExitCode   *int   `json:"runtimeExitCode"`
	RuntimePassed     bool   `json:"runtimePassed"`
	RuntimeRequested  bool   `json:"runtimeRequested"`
	RuntimeStatus     string `json:"runtimeStatus"`
	Status            string `json:"status"`
}

type surfaceMismatchReview struct {
	Groups                  []surfaceMismatchReviewGroup `json:"groups"`
	ManifestSHA256          string                       `json:"manifestSha256"`
	OracleResultsSHA256     string                       `json:"oracleResultsSha256"`
	RawClassifications      surfaceRawCounts             `json:"rawClassifications"`
	RawReconciliationSHA256 string                       `json:"rawReconciliationSha256"`
	ReviewCounts            surfaceReviewCounts          `json:"reviewCounts"`
	Rows                    []surfaceMismatchReviewRow   `json:"rows"`
	SchemaVersion           int                          `json:"schemaVersion"`
	Sealed                  bool                         `json:"sealed"`
}

type surfaceMismatchReviewGroup struct {
	ConfirmedMatchRows    int    `json:"confirmedMatchRows"`
	ConfirmedMismatchRows int    `json:"confirmedMismatchRows,omitempty"`
	InconclusiveRows      int    `json:"inconclusiveRows,omitempty"`
	Fixture               string `json:"fixture"`
}

type surfaceReviewCounts struct {
	ConfirmedMatch    int `json:"confirmedMatch"`
	ConfirmedMismatch int `json:"confirmedMismatch,omitempty"`
	Inconclusive      int `json:"inconclusive,omitempty"`
}

type surfaceMismatchReviewRow struct {
	Fixture              string `json:"fixture"`
	ReviewDisposition    string `json:"reviewDisposition"`
	SealedClassification string `json:"sealedClassification"`
	SurfaceID            string `json:"surfaceId"`
}

type surfaceOracleResults struct {
	Binding                 surfaceOracleBinding  `json:"binding"`
	ExcludedFixtures        int                   `json:"excludedFixtures"`
	ExcludedRows            int                   `json:"excludedRows"`
	LocalSummarySHA256      string                `json:"localSummarySha256"`
	OrgIdentities           any                   `json:"orgIdentities"`
	OrgPostflight           any                   `json:"orgPostflight"`
	OrgPreflightSHA256      string                `json:"orgPreflightSha256"`
	Orgs                    any                   `json:"orgs"`
	Results                 []surfaceOracleResult `json:"results"`
	RuntimeRequested        bool                  `json:"runtimeRequested"`
	SchemaVersion           int                   `json:"schemaVersion"`
	Sealed                  bool                  `json:"sealed"`
	SelectedFixtures        int                   `json:"selectedFixtures"`
	SelectedManifestIndexes []int                 `json:"selectedManifestIndexes"`
	SelectedRows            int                   `json:"selectedRows"`
	SelectionSHA256         string                `json:"selectionSha256"`
	SkippedDeferredFixtures any                   `json:"skippedDeferredFixtures"`
	WorkerExecution         any                   `json:"workerExecution"`
}

type surfaceOracleBinding struct {
	CandidateCommit       string  `json:"candidateCommit"`
	CandidateSHA256       string  `json:"candidateSha256"`
	LocalSummarySHA256    string  `json:"localSummarySha256"`
	ManifestSHA256        string  `json:"manifestSha256"`
	OrgPreflightSHA256    string  `json:"orgPreflightSha256"`
	ProfileSHA256         string  `json:"profileSha256"`
	QueueSHA256           *string `json:"queueSha256"`
	SelectionSHA256       string  `json:"selectionSha256"`
	SelectorReceiptSHA256 *string `json:"selectorReceiptSha256"`
	SelectorSHA256        *string `json:"selectorSha256"`
	ToolsAMD64SHA256      string  `json:"toolsAmd64Sha256"`
	ToolsCommit           string  `json:"toolsCommit"`
	WorkflowScriptSHA256  string  `json:"workflowScriptSha256"`
}

type surfaceOracleResult struct {
	CandidateCommit      string   `json:"candidateCommit"`
	CandidateSHA256      string   `json:"candidateSha256"`
	ClassNameMap         any      `json:"classNameMap"`
	ComponentFailures    []any    `json:"componentFailures"`
	ComponentSuccesses   any      `json:"componentSuccesses"`
	Coverage             any      `json:"coverage"`
	Deployable           bool     `json:"deployable"`
	Execution            any      `json:"execution"`
	ExitCode             int      `json:"exitCode"`
	Fixture              string   `json:"fixture"`
	FixtureSHA256        string   `json:"fixtureSha256"`
	Invocation           any      `json:"invocation"`
	Kind                 string   `json:"kind"`
	ManifestIndex        int      `json:"manifestIndex"`
	ManifestSHA256       string   `json:"manifestSha256"`
	Org                  any      `json:"org"`
	OrgCleanup           any      `json:"orgCleanup"`
	OrgIdentity          any      `json:"orgIdentity"`
	Project              any      `json:"project"`
	ProjectManifest      any      `json:"projectManifest"`
	RuntimeExitCode      *int     `json:"runtimeExitCode"`
	RuntimePassed        bool     `json:"runtimePassed"`
	RuntimeRequested     bool     `json:"runtimeRequested"`
	RuntimeResult        any      `json:"runtimeResult"`
	RuntimeStatus        string   `json:"runtimeStatus"`
	SourceFiles          any      `json:"sourceFiles"`
	Status               string   `json:"status"`
	SurfaceIDs           []string `json:"surfaceIds"`
	TestClasses          any      `json:"testClasses"`
	ToolsCommit          string   `json:"toolsCommit"`
	WorkflowScriptSHA256 string   `json:"workflowScriptSha256"`
}

type surfaceFinalAudit struct {
	ArtifactHashes    surfaceFinalArtifactHashes `json:"artifactHashes"`
	Checks            surfaceFinalChecks         `json:"checks"`
	FinalQuota        surfaceFinalQuota          `json:"finalQuota"`
	FinalResidueCount int                        `json:"finalResidueCount"`
	Passed            bool                       `json:"passed"`
	PrivacyScan       surfaceFinalPrivacyScan    `json:"privacyScan"`
	SchemaVersion     int                        `json:"schemaVersion"`
	SourceChecks      surfaceFinalSourceChecks   `json:"sourceChecks"`
}

type surfaceFinalArtifactHashes struct {
	Bindings       string `json:"bindings"`
	Cleanup        string `json:"cleanup"`
	LocalSummary   string `json:"localSummary"`
	Manifest       string `json:"manifest"`
	MismatchReview string `json:"mismatchReview"`
	OracleResults  string `json:"oracleResults"`
	Reconciliation string `json:"reconciliation"`
	RunSummary     string `json:"runSummary"`
}

type surfaceFinalChecks struct {
	CandidateHashMatched         bool `json:"candidateHashMatched"`
	CleanupReceiptPassed         bool `json:"cleanupReceiptPassed"`
	CredentialScanClean          bool `json:"credentialScanClean"`
	FinalActiveRecordResidueZero bool `json:"finalActiveRecordResidueZero"`
	FinalOrgDisplayRejected      bool `json:"finalOrgDisplayRejected"`
	FinalQuotaMatchedReceipt     bool `json:"finalQuotaMatchedReceipt"`
	LocalRowsAllPassed           bool `json:"localRowsAllPassed"`
	ManifestHashMatched          bool `json:"manifestHashMatched"`
	ManifestMode0400             bool `json:"manifestMode0400"`
	ManifestRowsBoundedUnique    bool `json:"manifestRowsBoundedUnique"`
	OraclePostflightMatched      bool `json:"oraclePostflightMatched"`
	OracleSealedRuntime          bool `json:"oracleSealedRuntime"`
	PrivateRootMode0700          bool `json:"privateRootMode0700"`
	ReconciliationSealed         bool `json:"reconciliationSealed"`
	RuntimeReviewSealed          bool `json:"runtimeReviewSealed"`
	SourceHeadsCleanExact        bool `json:"sourceHeadsCleanExact"`
	ToolsHashMatched             bool `json:"toolsHashMatched"`
	WorkflowHashMatched          bool `json:"workflowHashMatched"`
}

type surfaceFinalQuota struct {
	ActiveScratchOrgs surfaceFinalQuotaValue `json:"activeScratchOrgs"`
	DailyScratchOrgs  surfaceFinalQuotaValue `json:"dailyScratchOrgs"`
}

type surfaceFinalQuotaValue struct {
	Max       int `json:"max"`
	Remaining int `json:"remaining"`
}

type surfaceFinalPrivacyScan struct {
	CredentialJSONKeyHits int `json:"credentialJsonKeyHits"`
	CredentialPatternHits int `json:"credentialPatternHits"`
	ScannedFiles          int `json:"scannedFiles"`
}

type surfaceFinalSourceChecks struct {
	Glade      surfaceFinalSourceCheck `json:"glade"`
	GladeTools surfaceFinalSourceCheck `json:"glade-tools"`
}

type surfaceFinalSourceCheck struct {
	Clean       bool `json:"clean"`
	HeadMatched bool `json:"headMatched"`
}

// CreateSurfaceOracleIndex advances only rows backed by an exact, reviewed
// Salesforce runtime match. It emits no private paths or org identity data.
func CreateSurfaceOracleIndex(request SurfaceOracleIndexRequest) (SurfaceOracleIndex, error) {
	for _, input := range []struct{ path, label string }{{request.ScopePath, "surface scope"}, {request.OutputPath, "surface oracle index output"}} {
		if err := validateCleanReviewPath(input.path, input.label); err != nil {
			return SurfaceOracleIndex{}, err
		}
	}
	if len(request.RuntimeBatchRoots) == 0 {
		return SurfaceOracleIndex{}, fmt.Errorf("at least one runtime batch root is required")
	}
	for _, root := range request.RuntimeBatchRoots {
		if err := validateCleanReviewPath(root, "runtime batch root"); err != nil {
			return SurfaceOracleIndex{}, err
		}
	}
	if _, err := os.Lstat(request.OutputPath); err == nil {
		return SurfaceOracleIndex{}, fmt.Errorf("surface oracle index output already exists: %s", request.OutputPath)
	} else if !os.IsNotExist(err) {
		return SurfaceOracleIndex{}, err
	}

	scope, scopeBytes, err := readSurfaceOracleJSON[SurfaceOracleScope](request.ScopePath, "surface scope")
	if err != nil {
		return SurfaceOracleIndex{}, err
	}
	if err := validateSurfaceOracleScope(scope); err != nil {
		return SurfaceOracleIndex{}, err
	}
	if scope.Kind != "all-runtime" {
		return SurfaceOracleIndex{}, fmt.Errorf("surface oracle index requires an all-runtime scope")
	}
	credited := make(map[string]bool)
	batches := make([]SurfaceOracleIndexRuntimeBatch, 0, len(request.RuntimeBatchRoots))
	var candidate, tools SurfaceOracleIndexArtifact
	for i, root := range request.RuntimeBatchRoots {
		batch, batchCredit, err := validateSurfaceRuntimeBatch(root, scope)
		if err != nil {
			return SurfaceOracleIndex{}, err
		}
		batchCandidate := SurfaceOracleIndexArtifact{Commit: batch.candidateCommit, BinarySHA256: batch.candidateSHA256}
		batchTools := SurfaceOracleIndexArtifact{Commit: batch.toolsCommit, BinarySHA256: batch.toolsSHA256}
		if i == 0 {
			candidate, tools = batchCandidate, batchTools
		} else if candidate != batchCandidate || tools != batchTools {
			return SurfaceOracleIndex{}, fmt.Errorf("runtime batch candidate or tools bindings differ")
		}
		for id := range batchCredit {
			if credited[id] {
				return SurfaceOracleIndex{}, fmt.Errorf("surface %q is credited by more than one runtime batch", id)
			}
			credited[id] = true
			batch.SurfaceIDs = append(batch.SurfaceIDs, id)
		}
		sort.Strings(batch.SurfaceIDs)
		batches = append(batches, batch)
	}
	sort.Slice(batches, func(i, j int) bool { return batches[i].ManifestSHA256 < batches[j].ManifestSHA256 })
	rows := make([]SurfaceOracleIndexRow, len(scope.Rows))
	for i, row := range scope.Rows {
		rows[i] = SurfaceOracleIndexRow{SurfaceID: row.SurfaceID, State: "open"}
		if credited[row.SurfaceID] {
			rows[i].State = "matched"
		}
	}
	index := SurfaceOracleIndex{SchemaVersion: 1, Kind: "all-runtime", ScopeSHA256: replayBytesSHA256(scopeBytes), SourceProfileSHA256: scope.SourceProfileSHA256, LedgerSHA256: scope.LedgerSHA256, PolicySHA256: scope.PolicySHA256, Candidate: candidate, Tools: tools, RuntimeBatches: batches, Total: len(rows), Rows: rows}
	index.Counts = surfaceOracleIndexCounts(rows)
	if err := ValidateSurfaceOracleIndex(index); err != nil {
		return SurfaceOracleIndex{}, err
	}
	if err := WriteNewJSON(request.OutputPath, index); err != nil {
		return SurfaceOracleIndex{}, err
	}
	return index, nil
}

func ValidateSurfaceOracleIndex(index SurfaceOracleIndex) error {
	if index.SchemaVersion != 1 || index.Kind != "all-runtime" || !sha256Pattern.MatchString(index.ScopeSHA256) || !sha256Pattern.MatchString(index.SourceProfileSHA256) || !sha256Pattern.MatchString(index.LedgerSHA256) || !sha256Pattern.MatchString(index.PolicySHA256) || index.Total != len(index.Rows) || len(index.RuntimeBatches) == 0 {
		return fmt.Errorf("invalid surface oracle index bindings")
	}
	for _, value := range []string{index.Candidate.BinarySHA256, index.Tools.BinarySHA256} {
		if !sha256Pattern.MatchString(value) {
			return fmt.Errorf("invalid surface oracle runtime batch hash")
		}
	}
	if !commitPattern.MatchString(index.Candidate.Commit) || !commitPattern.MatchString(index.Tools.Commit) {
		return fmt.Errorf("invalid surface oracle runtime batch commit")
	}
	credited := make(map[string]bool)
	for i, batch := range index.RuntimeBatches {
		for _, value := range []string{batch.ManifestSHA256, batch.ProfileSHA256, batch.BindingsSHA256, batch.LocalSummarySHA256, batch.OracleResultsSHA256, batch.RawReconciliationSHA256, batch.MismatchReviewSHA256, batch.FinalAuditSHA256} {
			if !sha256Pattern.MatchString(value) {
				return fmt.Errorf("invalid surface oracle runtime batch receipt hash")
			}
		}
		if len(batch.SurfaceIDs) == 0 || (i > 0 && index.RuntimeBatches[i-1].ManifestSHA256 >= batch.ManifestSHA256) {
			return fmt.Errorf("runtime batches are empty, duplicate, or unsorted")
		}
		for j, id := range batch.SurfaceIDs {
			if strings.TrimSpace(id) == "" || credited[id] || (j > 0 && batch.SurfaceIDs[j-1] >= id) {
				return fmt.Errorf("runtime batch surface IDs are duplicate or unsorted")
			}
			credited[id] = true
		}
	}
	seen, matched := make(map[string]bool, len(index.Rows)), make(map[string]bool)
	for i, row := range index.Rows {
		if strings.TrimSpace(row.SurfaceID) == "" || seen[row.SurfaceID] || !surfaceOracleStatusValid(row.State) || (i > 0 && index.Rows[i-1].SurfaceID >= row.SurfaceID) {
			return fmt.Errorf("invalid or unsorted surface oracle index row %q", row.SurfaceID)
		}
		seen[row.SurfaceID] = true
		if row.State == "matched" {
			matched[row.SurfaceID] = true
		}
	}
	if len(matched) != len(credited) {
		return fmt.Errorf("surface oracle matched row set does not equal runtime batch credit")
	}
	for id := range matched {
		if !credited[id] {
			return fmt.Errorf("surface oracle matched row set does not equal runtime batch credit")
		}
	}
	if index.Counts != surfaceOracleIndexCounts(index.Rows) {
		return fmt.Errorf("surface oracle index counts do not reconcile")
	}
	return nil
}

func validateSurfaceOracleScope(scope SurfaceOracleScope) error {
	if scope.SchemaVersion != 1 || !sha256Pattern.MatchString(scope.SourceProfileSHA256) || !sha256Pattern.MatchString(scope.LedgerSHA256) || !sha256Pattern.MatchString(scope.PolicySHA256) || scope.Total != len(scope.Rows) {
		return fmt.Errorf("invalid surface scope bindings")
	}
	counts := map[string]int{}
	switch scope.Kind {
	case "all-runtime":
		if scope.OraclePlanSHA256 != "" {
			return fmt.Errorf("invalid surface scope bindings")
		}
		counts[deterministicMockRequired], counts[localRuntimeRequired] = 0, 0
	case "oracle-plan":
		if !sha256Pattern.MatchString(scope.OraclePlanSHA256) {
			return fmt.Errorf("invalid surface scope bindings")
		}
		for _, disposition := range []string{deterministicMockRequired, localRuntimeRequired, compileShapeRequired} {
			counts[disposition] = 0
		}
	default:
		return fmt.Errorf("invalid surface scope bindings")
	}
	seen := make(map[string]bool, len(scope.Rows))
	for i, row := range scope.Rows {
		if strings.TrimSpace(row.SurfaceID) == "" || seen[row.SurfaceID] || !surfaceScopeDispositionAllowed(scope.Kind, row.Disposition) || scope.Kind == "all-runtime" && row.Action != "" || scope.Kind == "oracle-plan" && (row.Action != oracleRuntime && row.Action != oracleCompile || !oracleActionMatchesDisposition(row.Action, row.Disposition)) || (i > 0 && scope.Rows[i-1].SurfaceID >= row.SurfaceID) {
			return fmt.Errorf("invalid or unsorted surface scope row %q", row.SurfaceID)
		}
		seen[row.SurfaceID] = true
		counts[row.Disposition]++
	}
	if len(scope.ByDisposition) != len(counts) {
		return fmt.Errorf("surface scope counts do not reconcile")
	}
	for disposition, count := range counts {
		if scope.ByDisposition[disposition] != count {
			return fmt.Errorf("surface scope counts do not reconcile")
		}
	}
	return nil
}

func surfaceScopeDispositionAllowed(kind, disposition string) bool {
	if disposition == deterministicMockRequired || disposition == localRuntimeRequired {
		return true
	}
	return kind == "oracle-plan" && disposition == compileShapeRequired
}

func validateSurfaceRuntimeBatch(root string, scope SurfaceOracleScope) (SurfaceOracleIndexRuntimeBatch, map[string]bool, error) {
	batch, states, err := validateSurfaceRuntimeAdjudications(root, scope)
	if err != nil {
		return SurfaceOracleIndexRuntimeBatch{}, nil, err
	}
	credited := make(map[string]bool, len(states))
	for id, state := range states {
		if state != "matched" {
			return SurfaceOracleIndexRuntimeBatch{}, nil, fmt.Errorf("runtime batch contains non-match row %q", id)
		}
		credited[id] = true
	}
	return batch, credited, nil
}

func validateSurfaceRuntimeAdjudications(root string, scope SurfaceOracleScope) (SurfaceOracleIndexRuntimeBatch, map[string]string, error) {
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 {
		return SurfaceOracleIndexRuntimeBatch{}, nil, fmt.Errorf("runtime batch root must be a directory")
	}
	paths := map[string]string{
		"manifest": filepath.Join("inputs", "RUNTIME_BATCH_MANIFEST.json"), "profile": filepath.Join("inputs", "RUNTIME_BATCH_PROFILE.json"),
		"bindings": filepath.Join("evidence", "BINDINGS.json"), "local": filepath.Join("local-proof", "LOCAL_RUNTIME_SUMMARY.json"),
		"reconciliation": filepath.Join("evidence", "RECONCILIATION.json"), "review": filepath.Join("evidence", "MISMATCH_REVIEW.json"),
		"audit": filepath.Join("evidence", "FINAL_AUDIT.json"), "oracle": filepath.Join("oracle", "results.json"),
		"cleanup": filepath.Join("evidence", "ORG_CLEANUP.json"), "runSummary": filepath.Join("evidence", "RUN_SUMMARY.json"),
		"candidate": filepath.Join("bin", "glade-sealed"), "tools": filepath.Join("bin", "glade-tools"), "workflow": filepath.Join("source", "glade-tools", "scripts", "corpus-assurance", "salesforce-first-filter.py"),
	}
	snapshots := make(map[string]reportInputSnapshot, len(paths))
	for name, relative := range paths {
		snapshot, err := readSurfaceBatchFile(root, relative)
		if err != nil {
			return SurfaceOracleIndexRuntimeBatch{}, nil, err
		}
		snapshots[name] = snapshot
	}
	if snapshots["manifest"].Mode.Perm() != 0o400 {
		return SurfaceOracleIndexRuntimeBatch{}, nil, fmt.Errorf("runtime batch manifest mode is not 0400")
	}
	decode := func(name string, value any) error {
		if err := decodeExactJSON(snapshots[name].Data, value); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		return nil
	}
	var manifest surfaceRuntimeManifest
	var bindings surfaceRuntimeBindings
	var local surfaceLocalSummary
	var reconciliation surfaceRawReconciliation
	var review surfaceMismatchReview
	var audit surfaceFinalAudit
	var oracle surfaceOracleResults
	for _, item := range []struct {
		key   string
		value any
	}{{"manifest", &manifest}, {"bindings", &bindings}, {"local", &local}, {"reconciliation", &reconciliation}, {"review", &review}, {"audit", &audit}, {"oracle", &oracle}} {
		if err := decode(item.key, item.value); err != nil {
			return SurfaceOracleIndexRuntimeBatch{}, nil, err
		}
	}
	hash := func(name string) string { return replayBytesSHA256(snapshots[name].Data) }
	if !commitPattern.MatchString(bindings.CandidateCommit) || !commitPattern.MatchString(bindings.ToolsCommit) || !sha256Pattern.MatchString(bindings.ScratchDefinitionSHA256) || !sha256Pattern.MatchString(bindings.SFCLISHA256) {
		return SurfaceOracleIndexRuntimeBatch{}, nil, fmt.Errorf("runtime batch bindings do not reconcile")
	}
	if bindings.ManifestSHA256 != hash("manifest") {
		return SurfaceOracleIndexRuntimeBatch{}, nil, fmt.Errorf("runtime batch manifest hash does not match bindings")
	}
	if bindings.LocalSummarySHA256 != hash("local") {
		return SurfaceOracleIndexRuntimeBatch{}, nil, fmt.Errorf("runtime batch local summary hash does not match bindings")
	}
	if bindings.ProfileSHA256 != hash("profile") {
		return SurfaceOracleIndexRuntimeBatch{}, nil, fmt.Errorf("runtime batch profile hash does not match bindings")
	}
	if bindings.CandidateSHA256 != hash("candidate") || bindings.ToolsSHA256 != hash("tools") || bindings.WorkflowScriptSHA256 != hash("workflow") {
		return SurfaceOracleIndexRuntimeBatch{}, nil, fmt.Errorf("runtime batch binary or workflow hash does not match bindings")
	}
	if err := validateSurfaceManifest(manifest); err != nil {
		return SurfaceOracleIndexRuntimeBatch{}, nil, err
	}
	fixtureKinds := make(map[string]string, len(manifest.Fixtures))
	for _, fixture := range manifest.Fixtures {
		sourceRoot, err := rootedPath(root, filepath.Join("source", "glade-tools"))
		if err != nil {
			return SurfaceOracleIndexRuntimeBatch{}, nil, err
		}
		snapshot, err := readSurfaceBatchFile(sourceRoot, fixture.Path)
		if err != nil || replayBytesSHA256(snapshot.Data) != fixture.SHA256 {
			return SurfaceOracleIndexRuntimeBatch{}, nil, fmt.Errorf("runtime batch manifest fixture %q changed", fixture.Fixture)
		}
		var source struct {
			Command struct {
				Kind string `json:"kind"`
			} `json:"command"`
		}
		if err := json.Unmarshal(snapshot.Data, &source); err != nil || (source.Command.Kind != "exec" && source.Command.Kind != "test") {
			return SurfaceOracleIndexRuntimeBatch{}, nil, fmt.Errorf("runtime batch fixture %q has invalid command kind", fixture.Fixture)
		}
		fixtureKinds[filepath.Base(fixture.Path)] = source.Command.Kind
	}
	fixtureRows := surfaceManifestRows(manifest)
	if err := validateSurfaceLocalSummary(local, manifest, bindings, fixtureKinds); err != nil {
		return SurfaceOracleIndexRuntimeBatch{}, nil, err
	}
	oracleStates, err := validateSurfaceOracleResults(oracle, manifest, bindings, fixtureKinds, hash("local"))
	if err != nil {
		return SurfaceOracleIndexRuntimeBatch{}, nil, err
	}
	if err := validateSurfaceReconciliation(reconciliation, fixtureRows, oracleStates, hash("manifest")); err != nil {
		return SurfaceOracleIndexRuntimeBatch{}, nil, err
	}
	states, err := validateSurfaceReview(review, reconciliation, fixtureRows, hash("manifest"), hash("oracle"), hash("reconciliation"))
	if err != nil {
		return SurfaceOracleIndexRuntimeBatch{}, nil, err
	}
	if err := validateSurfaceFinalAudit(audit, snapshots); err != nil {
		return SurfaceOracleIndexRuntimeBatch{}, nil, err
	}
	scopeIDs := make(map[string]bool, len(scope.Rows))
	for _, row := range scope.Rows {
		scopeIDs[row.SurfaceID] = true
	}
	for id := range states {
		if !scopeIDs[id] {
			return SurfaceOracleIndexRuntimeBatch{}, nil, fmt.Errorf("adjudicated surface %q is not in scope", id)
		}
	}
	return SurfaceOracleIndexRuntimeBatch{ManifestSHA256: hash("manifest"), ProfileSHA256: hash("profile"), BindingsSHA256: hash("bindings"), LocalSummarySHA256: hash("local"), OracleResultsSHA256: hash("oracle"), RawReconciliationSHA256: hash("reconciliation"), MismatchReviewSHA256: hash("review"), FinalAuditSHA256: hash("audit"), candidateCommit: bindings.CandidateCommit, candidateSHA256: bindings.CandidateSHA256, toolsCommit: bindings.ToolsCommit, toolsSHA256: bindings.ToolsSHA256}, states, nil
}

func validateSurfaceManifest(manifest surfaceRuntimeManifest) error {
	if manifest.SchemaVersion != 1 || manifest.SelectionPolicy != "whole, disjoint, inline anonymous-runtime fixtures with assertion-bearing programs" || len(manifest.Fixtures) == 0 {
		return fmt.Errorf("invalid runtime batch manifest")
	}
	seenFixtures, seenRows, total := map[string]bool{}, map[string]bool{}, 0
	for _, fixture := range manifest.Fixtures {
		if _, err := rootedPath(".", fixture.Path); err != nil {
			return fmt.Errorf("invalid runtime batch manifest fixture %q: %w", fixture.Fixture, err)
		}
		if fixture.Fixture == "" || seenFixtures[fixture.Fixture] || filepath.Clean(fixture.Path) != fixture.Path || strings.TrimSuffix(filepath.Base(fixture.Path), ".json") != fixture.Fixture || !fixture.SalesforceEligible || !sha256Pattern.MatchString(fixture.SHA256) || fixture.RowCount != len(fixture.SurfaceIDs) || fixture.RowCount == 0 {
			return fmt.Errorf("invalid runtime batch manifest fixture %q", fixture.Fixture)
		}
		seenFixtures[fixture.Fixture] = true
		for _, id := range fixture.SurfaceIDs {
			if strings.TrimSpace(id) == "" || seenRows[id] {
				return fmt.Errorf("invalid or duplicate runtime batch manifest row %q", id)
			}
			seenRows[id] = true
		}
		total += fixture.RowCount
	}
	if total != manifest.SurfaceRowCount {
		return fmt.Errorf("runtime batch manifest count does not reconcile")
	}
	return nil
}

func validateSurfaceLocalSummary(local surfaceLocalSummary, manifest surfaceRuntimeManifest, bindings surfaceRuntimeBindings, fixtureKinds map[string]string) error {
	if local.SchemaVersion != 1 || !local.Sealed || local.ManifestSHA256 != bindings.ManifestSHA256 || local.CandidateSHA256 != bindings.CandidateSHA256 || local.SelectedFixtures != len(manifest.Fixtures) || local.SelectedRows != manifest.SurfaceRowCount || len(local.Results) != len(manifest.Fixtures) {
		return fmt.Errorf("local summary bindings or counts do not reconcile")
	}
	seen := map[string]bool{}
	for _, result := range local.Results {
		if result.CandidateExitCode != 0 || result.CandidateStatus != "passed" || result.ExitCode != 0 || result.Status != "exit-0" || result.Fixture != filepath.Base(result.Path) || result.Kind != fixtureKinds[result.Fixture] || !sha256Pattern.MatchString(result.StderrSHA256) || !sha256Pattern.MatchString(result.StdoutSHA256) || seen[result.Path] {
			return fmt.Errorf("local summary contains an unpassed or duplicate result")
		}
		seen[result.Path] = true
	}
	for _, fixture := range manifest.Fixtures {
		if !seen[fixture.Path] {
			return fmt.Errorf("local summary fixture set does not match manifest")
		}
	}
	return nil
}

func validateSurfaceOracleResults(oracle surfaceOracleResults, manifest surfaceRuntimeManifest, bindings surfaceRuntimeBindings, fixtureKinds map[string]string, localHash string) (map[string]string, error) {
	b := oracle.Binding
	if oracle.SchemaVersion != 1 || !oracle.Sealed || !oracle.RuntimeRequested || oracle.SelectedFixtures != len(manifest.Fixtures) || oracle.SelectedRows != manifest.SurfaceRowCount || oracle.ExcludedFixtures != 0 || oracle.ExcludedRows != 0 || len(oracle.Results) != len(manifest.Fixtures) || len(oracle.SelectedManifestIndexes) != len(manifest.Fixtures) || oracle.LocalSummarySHA256 != localHash || b.ManifestSHA256 != bindings.ManifestSHA256 || b.ProfileSHA256 != bindings.ProfileSHA256 || b.LocalSummarySHA256 != localHash || b.CandidateCommit != bindings.CandidateCommit || b.CandidateSHA256 != bindings.CandidateSHA256 || b.ToolsCommit != bindings.ToolsCommit || b.ToolsAMD64SHA256 != bindings.ToolsSHA256 || b.WorkflowScriptSHA256 != bindings.WorkflowScriptSHA256 || oracle.SelectionSHA256 != b.SelectionSHA256 || oracle.OrgPreflightSHA256 != b.OrgPreflightSHA256 {
		return nil, fmt.Errorf("oracle results bindings or counts do not reconcile")
	}
	seen := map[int]bool{}
	states := make(map[string]string, len(oracle.Results))
	for _, result := range oracle.Results {
		if result.ManifestIndex < 0 || result.ManifestIndex >= len(manifest.Fixtures) || seen[result.ManifestIndex] {
			return nil, fmt.Errorf("oracle results contain an invalid manifest index")
		}
		seen[result.ManifestIndex] = true
		fixture := manifest.Fixtures[result.ManifestIndex]
		if result.Fixture != filepath.Base(fixture.Path) || result.FixtureSHA256 != fixture.SHA256 || result.ManifestSHA256 != bindings.ManifestSHA256 || result.CandidateCommit != bindings.CandidateCommit || result.CandidateSHA256 != bindings.CandidateSHA256 || result.ToolsCommit != bindings.ToolsCommit || result.WorkflowScriptSHA256 != bindings.WorkflowScriptSHA256 || !result.RuntimeRequested || result.Kind != fixtureKinds[result.Fixture] || !equalStringSet(result.SurfaceIDs, fixture.SurfaceIDs) {
			return nil, fmt.Errorf("oracle result does not match manifest fixture %q", fixture.Fixture)
		}
		switch {
		case result.Status == "Succeeded" && result.Deployable && result.ExitCode == 0 && result.RuntimePassed && result.RuntimeStatus == "Passed" && len(result.ComponentFailures) == 0 && (result.Kind == "exec" && result.RuntimeExitCode == nil || result.Kind == "test" && result.RuntimeExitCode != nil && *result.RuntimeExitCode == 0):
			states[result.Fixture] = "matched"
		case result.Kind == "test" && result.Status == "Succeeded" && result.Deployable && result.ExitCode == 0 && !result.RuntimePassed && result.RuntimeStatus == "Failed" && result.RuntimeExitCode != nil && *result.RuntimeExitCode != 0 && len(result.ComponentFailures) == 0:
			states[result.Fixture] = "test-failed"
		case result.Status == "Failed" && !result.Deployable && !result.RuntimePassed && result.RuntimeStatus == "Failed" && (result.ExitCode != 0 || len(result.ComponentFailures) != 0):
			states[result.Fixture] = "operational-failed"
		default:
			return nil, fmt.Errorf("oracle result has no trusted adjudication for fixture %q", fixture.Fixture)
		}
	}
	for i, index := range oracle.SelectedManifestIndexes {
		if index != i {
			return nil, fmt.Errorf("oracle selected manifest indexes do not reconcile")
		}
	}
	return states, nil
}

func validateSurfaceReconciliation(value surfaceRawReconciliation, rows map[string]string, oracleStates map[string]string, manifestHash string) error {
	if value.SchemaVersion != 1 || !value.Sealed || !value.RuntimeRequested || !value.OrgPostflightMatched || value.RunnerError != nil || value.ManifestSHA256 != manifestHash || len(value.Rows) != len(rows) {
		return fmt.Errorf("raw reconciliation bindings do not reconcile")
	}
	seen, counts := map[string]bool{}, surfaceRawCounts{}
	fixtureClassifications := map[string]map[string]bool{}
	for _, row := range value.Rows {
		fixture, exists := rows[row.SurfaceID]
		if seen[row.SurfaceID] || !exists || fixture != row.Fixture {
			return fmt.Errorf("raw reconciliation row set does not match manifest")
		}
		seen[row.SurfaceID] = true
		if fixtureClassifications[row.Fixture] == nil {
			fixtureClassifications[row.Fixture] = map[string]bool{}
		}
		fixtureClassifications[row.Fixture][row.Classification] = true
		switch row.Classification {
		case "match":
			counts.Match++
			if row.Local.CandidateExitCode != 0 || row.Local.CandidateStatus != "passed" || row.Local.Status != "exit-0" || row.Reason != "local-and-salesforce-runtime-passed" || len(row.Salesforce.ComponentFailures) != 0 || !row.Salesforce.Deployable || row.Salesforce.ExitCode != 0 || row.Salesforce.RuntimeExitCode != nil && *row.Salesforce.RuntimeExitCode != 0 || !row.Salesforce.RuntimePassed || !row.Salesforce.RuntimeRequested || row.Salesforce.RuntimeStatus != "Passed" || row.Salesforce.Status != "Succeeded" {
				return fmt.Errorf("raw reconciliation row %q is not a runtime match", row.SurfaceID)
			}
		case "mismatch":
			counts.Mismatch++
			if oracleStates[row.Fixture] != "test-failed" || row.Local.CandidateExitCode != 0 || row.Local.CandidateStatus != "passed" || row.Local.Status != "exit-0" || row.Reason != "salesforce-runtime-assertion-differed" || len(row.Salesforce.ComponentFailures) != 0 || !row.Salesforce.Deployable || row.Salesforce.ExitCode != 0 || row.Salesforce.RuntimeExitCode == nil || *row.Salesforce.RuntimeExitCode == 0 || row.Salesforce.RuntimePassed || !row.Salesforce.RuntimeRequested || row.Salesforce.RuntimeStatus != "Failed" || row.Salesforce.Status != "Succeeded" {
				return fmt.Errorf("raw reconciliation row %q is not a proven runtime mismatch", row.SurfaceID)
			}
		case "environment":
			counts.Environment++
			if oracleStates[row.Fixture] != "test-failed" && oracleStates[row.Fixture] != "operational-failed" || row.Reason != "salesforce-runtime-infrastructure-failed" || row.Salesforce.RuntimePassed || !row.Salesforce.RuntimeRequested || row.Salesforce.RuntimeStatus != "Failed" || row.Salesforce.RuntimeExitCode == nil && row.Salesforce.Status != "Failed" && row.Salesforce.Deployable && row.Salesforce.ExitCode == 0 && len(row.Salesforce.ComponentFailures) == 0 {
				return fmt.Errorf("raw reconciliation row %q is not operationally inconclusive", row.SurfaceID)
			}
		default:
			return fmt.Errorf("raw reconciliation contains unknown classification %q", row.Classification)
		}
	}
	if counts != value.Counts {
		return fmt.Errorf("raw reconciliation counts do not reconcile")
	}
	for fixture, state := range oracleStates {
		if state == "test-failed" && !fixtureClassifications[fixture]["mismatch"] && !fixtureClassifications[fixture]["environment"] || state == "operational-failed" && !fixtureClassifications[fixture]["environment"] {
			return fmt.Errorf("oracle result does not reconcile with per-surface classifications")
		}
	}
	return nil
}

func validateSurfaceReview(review surfaceMismatchReview, raw surfaceRawReconciliation, rows map[string]string, manifestHash, oracleHash, rawHash string) (map[string]string, error) {
	if review.SchemaVersion != 1 || !review.Sealed || review.ManifestSHA256 != manifestHash || review.OracleResultsSHA256 != oracleHash || review.RawReconciliationSHA256 != rawHash || review.RawClassifications != raw.Counts || len(review.Rows) != len(rows) {
		return nil, fmt.Errorf("mismatch review bindings do not reconcile")
	}
	rawClassifications := make(map[string]string, len(raw.Rows))
	for _, row := range raw.Rows {
		rawClassifications[row.SurfaceID] = row.Classification
	}
	seen := map[string]bool{}
	groups := map[string]surfaceMismatchReviewGroup{}
	counts := surfaceReviewCounts{}
	states := make(map[string]string, len(review.Rows))
	for _, row := range review.Rows {
		fixture, exists := rows[row.SurfaceID]
		if seen[row.SurfaceID] || !exists || fixture != row.Fixture {
			return nil, fmt.Errorf("mismatch review row set does not match manifest")
		}
		seen[row.SurfaceID] = true
		if row.SealedClassification != rawClassifications[row.SurfaceID] {
			return nil, fmt.Errorf("mismatch review classification does not match reconciliation")
		}
		group := groups[row.Fixture]
		group.Fixture = row.Fixture
		switch row.SealedClassification {
		case "match":
			if row.ReviewDisposition != "confirmed-match" {
				return nil, fmt.Errorf("mismatch review contains an unconfirmed match")
			}
			counts.ConfirmedMatch++
			group.ConfirmedMatchRows++
			states[row.SurfaceID] = "matched"
		case "mismatch":
			if row.ReviewDisposition != "confirmed-mismatch" {
				return nil, fmt.Errorf("mismatch review contains an unconfirmed mismatch")
			}
			counts.ConfirmedMismatch++
			group.ConfirmedMismatchRows++
			states[row.SurfaceID] = "product-mismatch"
		case "environment":
			if row.ReviewDisposition != "inconclusive" {
				return nil, fmt.Errorf("mismatch review treats inconclusive evidence as terminal")
			}
			counts.Inconclusive++
			group.InconclusiveRows++
			states[row.SurfaceID] = "inconclusive"
		default:
			return nil, fmt.Errorf("mismatch review contains an unknown classification")
		}
		groups[row.Fixture] = group
	}
	if review.ReviewCounts != counts || len(review.Groups) != len(manifestFixtureNames(rows)) {
		return nil, fmt.Errorf("mismatch review counts do not reconcile")
	}
	seenGroups := map[string]bool{}
	for _, group := range review.Groups {
		if seenGroups[group.Fixture] || group != groups[group.Fixture] {
			return nil, fmt.Errorf("mismatch review group counts do not reconcile")
		}
		seenGroups[group.Fixture] = true
	}
	return states, nil
}

func validateSurfaceFinalAudit(audit surfaceFinalAudit, snapshots map[string]reportInputSnapshot) error {
	hash := func(name string) string { return replayBytesSHA256(snapshots[name].Data) }
	want := surfaceFinalArtifactHashes{Bindings: hash("bindings"), Cleanup: hash("cleanup"), LocalSummary: hash("local"), Manifest: hash("manifest"), MismatchReview: hash("review"), OracleResults: hash("oracle"), Reconciliation: hash("reconciliation"), RunSummary: hash("runSummary")}
	c := audit.Checks
	allChecks := c.CandidateHashMatched && c.CleanupReceiptPassed && c.CredentialScanClean && c.FinalActiveRecordResidueZero && c.FinalOrgDisplayRejected && c.FinalQuotaMatchedReceipt && c.LocalRowsAllPassed && c.ManifestHashMatched && c.ManifestMode0400 && c.ManifestRowsBoundedUnique && c.OraclePostflightMatched && c.OracleSealedRuntime && c.PrivateRootMode0700 && c.ReconciliationSealed && c.RuntimeReviewSealed && c.SourceHeadsCleanExact && c.ToolsHashMatched && c.WorkflowHashMatched
	if audit.SchemaVersion != 1 || !audit.Passed || audit.ArtifactHashes != want || !allChecks || audit.FinalResidueCount != 0 || audit.PrivacyScan.CredentialJSONKeyHits != 0 || audit.PrivacyScan.CredentialPatternHits != 0 || audit.PrivacyScan.ScannedFiles < 1 || !audit.SourceChecks.Glade.Clean || !audit.SourceChecks.Glade.HeadMatched || !audit.SourceChecks.GladeTools.Clean || !audit.SourceChecks.GladeTools.HeadMatched || audit.FinalQuota.ActiveScratchOrgs.Remaining < 0 || audit.FinalQuota.ActiveScratchOrgs.Remaining > audit.FinalQuota.ActiveScratchOrgs.Max || audit.FinalQuota.DailyScratchOrgs.Remaining < 0 || audit.FinalQuota.DailyScratchOrgs.Remaining > audit.FinalQuota.DailyScratchOrgs.Max {
		return fmt.Errorf("final audit does not independently validate")
	}
	return nil
}

func readSurfaceOracleJSON[T any](path, label string) (T, []byte, error) {
	var value T
	snapshot, err := readRegularFileSnapshot(path)
	if err != nil {
		return value, nil, fmt.Errorf("%s: %w", label, err)
	}
	if err := decodeExactJSON(snapshot.Data, &value); err != nil {
		return value, nil, fmt.Errorf("%s: %w", label, err)
	}
	return value, snapshot.Data, nil
}

func readSurfaceBatchFile(root, relative string) (reportInputSnapshot, error) {
	target, err := rootedPath(root, relative)
	if err != nil {
		return reportInputSnapshot{}, err
	}
	contained, err := filepath.Rel(root, target)
	if err != nil {
		return reportInputSnapshot{}, err
	}
	current := root
	for _, part := range strings.Split(contained, string(os.PathSeparator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return reportInputSnapshot{}, fmt.Errorf("runtime batch input must be a regular file: %s", relative)
		}
	}
	snapshot, err := readRegularFileSnapshot(target)
	if err != nil {
		return reportInputSnapshot{}, fmt.Errorf("runtime batch input must be a regular file: %s", relative)
	}
	return snapshot, nil
}

func surfaceManifestRows(manifest surfaceRuntimeManifest) map[string]string {
	rows := make(map[string]string, manifest.SurfaceRowCount)
	for _, fixture := range manifest.Fixtures {
		for _, id := range fixture.SurfaceIDs {
			rows[id] = filepath.Base(fixture.Path)
		}
	}
	return rows
}

func manifestFixtureNames(rows map[string]string) map[string]bool {
	fixtures := map[string]bool{}
	for _, fixture := range rows {
		fixtures[fixture] = true
	}
	return fixtures
}

func equalStringSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	seen := make(map[string]bool, len(left))
	for _, value := range left {
		if seen[value] {
			return false
		}
		seen[value] = true
	}
	for _, value := range right {
		if !seen[value] {
			return false
		}
	}
	return true
}

func surfaceOracleStatusValid(status string) bool {
	return status == "open" || status == "matched"
}

func surfaceOracleIndexCounts(rows []SurfaceOracleIndexRow) SurfaceOracleIndexCounts {
	counts := SurfaceOracleIndexCounts{}
	for _, row := range rows {
		switch row.State {
		case "open":
			counts.Open++
		case "matched":
			counts.Matched++
		}
	}
	counts.Adjudicated = len(rows) - counts.Open
	return counts
}
