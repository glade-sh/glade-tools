package corpusassurance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"

	"github.com/glade-sh/glade/tools/internal/surfaceledger"
)

const (
	oracleRuntime           = "runtime"
	oracleCompile           = "compile"
	oracleLocalContractOnly = "local-contract-only"
	oracleWaiver            = "waiver"
	oracleUnknown           = "unknown"
)

type OracleInputRow struct {
	SurfaceID        string `json:"surfaceId"`
	Disposition      string `json:"disposition"`
	RuntimeObserved  bool   `json:"runtimeObserved,omitempty"`
	BehaviorObserved bool   `json:"behaviorObserved,omitempty"`
	CompilePassed    bool   `json:"compilePassed,omitempty"`
	Deployable       bool   `json:"deployable,omitempty"`
	ExclusionClass   string `json:"exclusionClass,omitempty"`
	ExclusionReason  string `json:"exclusionReason,omitempty"`
}

type OraclePlanRow struct {
	SurfaceID       string `json:"surfaceId"`
	Action          string `json:"action"`
	ExclusionClass  string `json:"exclusionClass,omitempty"`
	ExclusionReason string `json:"exclusionReason,omitempty"`
}

type OraclePlan struct {
	Candidate         RuntimeArtifact `json:"candidate"`
	Tools             RuntimeArtifact `json:"tools"`
	ProfileSHA256     string          `json:"profileSha256,omitempty"`
	SealedUsageSHA256 string          `json:"sealedUsageSha256,omitempty"`
	LocalProofSHA256  string          `json:"localProofSha256,omitempty"`
	DirectiveSHA256   string          `json:"directiveSha256,omitempty"`
	Rows              []OraclePlanRow `json:"rows"`
}

type OracleProfileRow struct {
	SurfaceID   string `json:"surfaceId"`
	Disposition string `json:"disposition"`
}

// AssuranceProfile is the fresh, reduced profile sent to the Salesforce
// oracle. It contains only current required rows and the bindings that made
// them eligible; historical corpus and queue data is intentionally absent.
type AssuranceProfile struct {
	SchemaVersion         int                   `json:"schemaVersion"`
	SourceProfileSHA256   string                `json:"sourceProfileSha256"`
	SealedUsageSHA256     string                `json:"sealedUsageSha256"`
	LedgerSHA256          string                `json:"ledgerSha256"`
	PolicySHA256          string                `json:"policySha256"`
	FixtureManifestSHA256 string                `json:"fixtureManifestSha256"`
	LocalProofSHA256      string                `json:"localProofSha256"`
	Total                 int                   `json:"total"`
	ByDisposition         map[string]int        `json:"byDisposition"`
	NonDeferredGaps       []AssuranceProfileRow `json:"nonDeferredGaps"`
	HostedDeferred        []AssuranceProfileRow `json:"hostedDeferred"`
	Rows                  []AssuranceProfileRow `json:"rows"`
}

// AssuranceProfileRow is the allowlisted, public support-profile projection.
type AssuranceProfileRow struct {
	SurfaceID   string `json:"surfaceId"`
	Namespace   string `json:"namespace,omitempty"`
	TypeFamily  string `json:"typeFamily,omitempty"`
	LedgerShape string `json:"ledgerShape,omitempty"`
	Behavior    string `json:"behavior,omitempty"`
	Evidence    string `json:"evidence,omitempty"`
	Disposition string `json:"disposition"`
	MatchRule   string `json:"matchRule,omitempty"`
	Reason      string `json:"reason,omitempty"`
	Obligation  string `json:"obligation,omitempty"`
	GapClass    string `json:"gapClass,omitempty"`
}

// OracleDirective records the classification not available from local proof:
// a deterministic mock is deployable unless it names an explicit exclusion.
type OracleDirective struct {
	SurfaceID       string `json:"surfaceId"`
	ExclusionClass  string `json:"exclusionClass,omitempty"`
	ExclusionReason string `json:"exclusionReason,omitempty"`
}

type OracleDirectiveFile struct {
	SchemaVersion     int               `json:"schemaVersion"`
	ProfileSHA256     string            `json:"profileSha256"` // sealed source-profile bytes; projection does not exist yet
	SealedUsageSHA256 string            `json:"sealedUsageSha256"`
	LocalProofSHA256  string            `json:"localProofSha256"`
	Directives        []OracleDirective `json:"directives"`
}

type ExclusionPolicyRow struct {
	SurfaceID string `json:"surfaceId"`
	Class     string `json:"class"`
	Reason    string `json:"reason"`
}

type ExclusionPolicy struct {
	SchemaVersion int                  `json:"schemaVersion"`
	Rows          []ExclusionPolicyRow `json:"rows"`
}

// ExclusionRequest is the planner's non-authoritative list of explicit
// non-parity rows. A separately checked policy is still required to grant it.
type ExclusionRequest struct {
	SchemaVersion     int                  `json:"schemaVersion"`
	Candidate         RuntimeArtifact      `json:"candidate"`
	Tools             RuntimeArtifact      `json:"tools"`
	PlanSHA256        string               `json:"planSha256"`
	ProfileSHA256     string               `json:"profileSha256"`
	SealedUsageSHA256 string               `json:"sealedUsageSha256"`
	DecisionSHA256    string               `json:"decisionSha256"`
	LocalProofSHA256  string               `json:"localProofSha256"`
	Rows              []ExclusionPolicyRow `json:"rows"`
}

type ExclusionAuthority struct {
	Candidate              RuntimeArtifact      `json:"candidate"`
	Tools                  RuntimeArtifact      `json:"tools"`
	PlanSHA256             string               `json:"planSha256"`
	ProfileSHA256          string               `json:"profileSha256"`
	SealedUsageSHA256      string               `json:"sealedUsageSha256"`
	DecisionSHA256         string               `json:"decisionSha256"`
	LocalProofSHA256       string               `json:"localProofSha256"`
	PolicySHA256           string               `json:"policySha256"`
	SalesforceParityCredit int                  `json:"salesforceParityCredit"`
	Rows                   []ExclusionPolicyRow `json:"rows"`
}

// BuildExclusionRequest derives every non-parity row from the sealed plan.
// It cannot accept a caller-selected subset and has no authority to grant
// Salesforce credit.
func BuildExclusionRequest(planPath, profilePath, sealedUsagePath, outputPath string) (ExclusionRequest, error) {
	for _, path := range []string{planPath, profilePath, sealedUsagePath, outputPath} {
		if !filepath.IsAbs(path) {
			return ExclusionRequest{}, fmt.Errorf("absolute exclusion-request paths are required")
		}
	}
	if _, err := os.Lstat(outputPath); err == nil {
		return ExclusionRequest{}, fmt.Errorf("exclusion request output already exists: %s", outputPath)
	} else if !os.IsNotExist(err) {
		return ExclusionRequest{}, err
	}
	plan, planBytes, err := readExactJSONBytes[OraclePlan](planPath)
	if err != nil {
		return ExclusionRequest{}, err
	}
	profile, profileBytes, err := readExactJSONBytes[AssuranceProfile](profilePath)
	if err != nil {
		return ExclusionRequest{}, err
	}
	usage, usageBytes, err := readExactJSONBytes[SealedCorpusUsage](sealedUsagePath)
	if err != nil {
		return ExclusionRequest{}, err
	}
	planSHA, profileSHA, usageSHA := replayBytesSHA256(planBytes), replayBytesSHA256(profileBytes), replayBytesSHA256(usageBytes)
	if ValidateRuntimeArtifact(plan.Candidate) != nil || ValidateRuntimeArtifact(plan.Tools) != nil || profile.SchemaVersion != 1 || profile.SealedUsageSHA256 != usageSHA || plan.ProfileSHA256 != profileSHA || plan.SealedUsageSHA256 != usageSHA || plan.LocalProofSHA256 != profile.LocalProofSHA256 || !sha256Pattern.MatchString(usage.DecisionSHA256) {
		return ExclusionRequest{}, fmt.Errorf("exclusion request inputs do not bind")
	}
	rows, err := exclusionRowsFromPlan(plan)
	if err != nil {
		return ExclusionRequest{}, err
	}
	request := ExclusionRequest{SchemaVersion: 1, Candidate: plan.Candidate, Tools: plan.Tools, PlanSHA256: planSHA, ProfileSHA256: profileSHA, SealedUsageSHA256: usageSHA, DecisionSHA256: usage.DecisionSHA256, LocalProofSHA256: plan.LocalProofSHA256, Rows: rows}
	if err := verifyExclusionRequestInputs(planPath, profilePath, sealedUsagePath, planSHA, profileSHA, usageSHA); err != nil {
		return ExclusionRequest{}, err
	}
	if err := WriteNewJSON(outputPath, request); err != nil {
		return ExclusionRequest{}, err
	}
	return request, nil
}

func exclusionRowsFromPlan(plan OraclePlan) ([]ExclusionPolicyRow, error) {
	seen := make(map[string]bool, len(plan.Rows))
	rows := make([]ExclusionPolicyRow, 0)
	for _, row := range plan.Rows {
		if row.SurfaceID == "" || seen[row.SurfaceID] {
			return nil, fmt.Errorf("invalid or duplicate oracle plan surface %q", row.SurfaceID)
		}
		seen[row.SurfaceID] = true
		switch row.Action {
		case oracleRuntime, oracleCompile:
			if row.ExclusionClass != "" || row.ExclusionReason != "" {
				return nil, fmt.Errorf("Salesforce row %q carries an exclusion", row.SurfaceID)
			}
		case oracleLocalContractOnly, oracleWaiver:
			if row.ExclusionClass == "" || row.ExclusionReason == "" {
				return nil, fmt.Errorf("non-parity row %q lacks an exclusion", row.SurfaceID)
			}
			rows = append(rows, ExclusionPolicyRow{SurfaceID: row.SurfaceID, Class: row.ExclusionClass, Reason: row.ExclusionReason})
		default:
			return nil, fmt.Errorf("oracle plan row %q has unsupported action %q", row.SurfaceID, row.Action)
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].SurfaceID < rows[j].SurfaceID })
	return rows, nil
}

func verifyExclusionRequestInputs(planPath, profilePath, usagePath, planSHA, profileSHA, usageSHA string) error {
	for _, input := range []struct{ path, sha string }{{planPath, planSHA}, {profilePath, profileSHA}, {usagePath, usageSHA}} {
		data, err := os.ReadFile(input.path)
		if err != nil || replayBytesSHA256(data) != input.sha {
			return fmt.Errorf("exclusion request input changed during planning")
		}
	}
	return nil
}

// BuildAssuranceProfile rebuilds the Salesforce profile from the current
// private-required set. It accepts no caller-selected rows and omits all
// historical profile fields not needed by the oracle.
func BuildAssuranceProfile(inventoryPath, rootManifestPath, sourceProfilePath, sealedUsagePath, ledgerPath, policyPath, decisionPath, fixtureManifestPath, localProofPath, outputPath string) (AssuranceProfile, error) {
	for _, path := range []string{inventoryPath, rootManifestPath, sourceProfilePath, sealedUsagePath, ledgerPath, policyPath, decisionPath, fixtureManifestPath, localProofPath, outputPath} {
		if !filepath.IsAbs(path) {
			return AssuranceProfile{}, fmt.Errorf("absolute assurance-profile paths are required")
		}
	}
	if _, err := os.Lstat(outputPath); err == nil {
		return AssuranceProfile{}, fmt.Errorf("assurance profile output already exists: %s", outputPath)
	} else if !os.IsNotExist(err) {
		return AssuranceProfile{}, err
	}
	sourceRows, sourceBytes, err := readAssuranceProfileRows(sourceProfilePath)
	if err != nil {
		return AssuranceProfile{}, fmt.Errorf("read source profile: %w", err)
	}
	sealedUsage, sealedUsageBytes, err := readExactJSONBytes[SealedCorpusUsage](sealedUsagePath)
	if err != nil {
		return AssuranceProfile{}, fmt.Errorf("read sealed usage: %w", err)
	}
	temp, err := os.MkdirTemp("", "glade-assurance-usage-*")
	if err != nil {
		return AssuranceProfile{}, err
	}
	defer os.RemoveAll(temp)
	rebuilt, err := BuildSealedCorpusUsage(inventoryPath, ledgerPath, rootManifestPath, sourceProfilePath, policyPath, decisionPath, filepath.Join(temp, "CORPUS_USAGE.json"))
	if err != nil || !reflect.DeepEqual(rebuilt, sealedUsage) {
		return AssuranceProfile{}, fmt.Errorf("sealed usage does not match authoritative recomputation")
	}
	ledger, ledgerBytes, err := readExactJSONBytes[surfaceledger.SurfaceLedger](ledgerPath)
	if err != nil {
		return AssuranceProfile{}, fmt.Errorf("read ledger: %w", err)
	}
	manifest, manifestBytes, err := readExactJSONBytes[LocalProofFixtureManifest](fixtureManifestPath)
	if err != nil {
		return AssuranceProfile{}, fmt.Errorf("read fixture manifest: %w", err)
	}
	proof, proofBytes, err := readExactJSONBytes[LocalProof](localProofPath)
	if err != nil {
		return AssuranceProfile{}, fmt.Errorf("read local proof: %w", err)
	}
	sourceSHA, usageSHA := replayBytesSHA256(sourceBytes), replayBytesSHA256(sealedUsageBytes)
	ledgerSHA, manifestSHA, proofSHA := replayBytesSHA256(ledgerBytes), replayBytesSHA256(manifestBytes), replayBytesSHA256(proofBytes)
	if sealedUsage.SchemaVersion != 1 || sealedUsage.ProfileSHA256 != sourceSHA || sealedUsage.LedgerSHA256 != ledgerSHA || !sha256Pattern.MatchString(sealedUsage.PolicySHA256) || proof.FixtureManifestSHA256 != manifestSHA {
		return AssuranceProfile{}, fmt.Errorf("assurance profile inputs do not bind")
	}
	if err := VerifyLocalProofReplay(proof, manifest); err != nil {
		return AssuranceProfile{}, fmt.Errorf("verify local proof replay: %w", err)
	}
	required, err := oracleRequiredSurfaceIDs(sealedUsage.Reconciliation)
	if err != nil {
		return AssuranceProfile{}, err
	}
	source := make(map[string]AssuranceProfileRow, len(sourceRows))
	for _, row := range sourceRows {
		if row.SurfaceID == "" || row.Disposition == "" || source[row.SurfaceID].SurfaceID != "" {
			return AssuranceProfile{}, fmt.Errorf("invalid or duplicate source profile surface %q", row.SurfaceID)
		}
		source[row.SurfaceID] = row
	}
	ledgerIDs := make(map[string]bool, len(ledger.Rows))
	for _, row := range ledger.Rows {
		if row.SurfaceID == "" || ledgerIDs[row.SurfaceID] {
			return AssuranceProfile{}, fmt.Errorf("invalid or duplicate ledger surface %q", row.SurfaceID)
		}
		ledgerIDs[row.SurfaceID] = true
	}
	owned, err := ownedFixtureSurfaces(manifest)
	if err != nil {
		return AssuranceProfile{}, err
	}
	local := make(map[string]LocalSurfaceProof, len(proof.Surfaces))
	for _, row := range proof.Surfaces {
		if row.SurfaceID == "" || local[row.SurfaceID].SurfaceID != "" {
			return AssuranceProfile{}, fmt.Errorf("invalid or duplicate local proof surface %q", row.SurfaceID)
		}
		local[row.SurfaceID] = row
	}
	result := AssuranceProfile{SchemaVersion: 1, SourceProfileSHA256: sourceSHA, SealedUsageSHA256: usageSHA, LedgerSHA256: ledgerSHA, PolicySHA256: sealedUsage.PolicySHA256, FixtureManifestSHA256: manifestSHA, LocalProofSHA256: proofSHA, ByDisposition: map[string]int{}}
	for _, surfaceID := range required {
		row, exists := source[surfaceID]
		if !exists || !ledgerIDs[surfaceID] || !owned[surfaceID] {
			return AssuranceProfile{}, fmt.Errorf("required surface %q is not current-profile, ledger, and fixture owned", surfaceID)
		}
		if row.Disposition != "hosted-deferred" {
			proofRow, exists := local[surfaceID]
			if !exists || proofRow.Disposition != row.Disposition {
				return AssuranceProfile{}, fmt.Errorf("required surface %q lacks matching local proof", surfaceID)
			}
		}
		result.Rows = append(result.Rows, row)
		result.ByDisposition[row.Disposition]++
		if row.Disposition == "hosted-deferred" {
			result.HostedDeferred = append(result.HostedDeferred, row)
		} else {
			result.NonDeferredGaps = append(result.NonDeferredGaps, row)
		}
	}
	sort.Slice(result.Rows, func(i, j int) bool { return result.Rows[i].SurfaceID < result.Rows[j].SurfaceID })
	sort.Slice(result.NonDeferredGaps, func(i, j int) bool { return result.NonDeferredGaps[i].SurfaceID < result.NonDeferredGaps[j].SurfaceID })
	sort.Slice(result.HostedDeferred, func(i, j int) bool { return result.HostedDeferred[i].SurfaceID < result.HostedDeferred[j].SurfaceID })
	result.Total = len(result.Rows)
	if err := verifyAssuranceProfileInputs(sourceProfilePath, sealedUsagePath, ledgerPath, fixtureManifestPath, localProofPath, sourceSHA, usageSHA, ledgerSHA, manifestSHA, proofSHA); err != nil {
		return AssuranceProfile{}, err
	}
	if err := WriteNewJSON(outputPath, result); err != nil {
		return AssuranceProfile{}, err
	}
	return result, nil
}

func oracleRequiredSurfaceIDs(reconciled UsageReconciliation) ([]string, error) {
	set := make(map[string]bool, len(reconciled.Usage))
	for _, row := range reconciled.Usage {
		switch row.Class {
		case usageClassExact, usageClassCaseAlias, usageClassAggregateParent, usageClassCanonicalAlias:
			if row.SurfaceID == "" {
				return nil, fmt.Errorf("reconciled usage %q lacks a surface", row.UsageKey)
			}
			set[row.SurfaceID] = true
		case usageClassLocalSymbol, usageClassNonSalesforceGenerated:
			if row.SurfaceID != "" {
				return nil, fmt.Errorf("non-Salesforce usage %q selects a surface", row.UsageKey)
			}
		default:
			return nil, fmt.Errorf("reconciled usage %q has unknown class %q", row.UsageKey, row.Class)
		}
	}
	if len(set) == 0 {
		return nil, fmt.Errorf("no reconciled Salesforce surfaces require an assurance profile")
	}
	ids := make([]string, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}

func ownedFixtureSurfaces(manifest LocalProofFixtureManifest) (map[string]bool, error) {
	owned := make(map[string]bool)
	fixtures := make(map[string]bool, len(manifest.Fixtures))
	for _, fixture := range manifest.Fixtures {
		if fixture.ID == "" || fixtures[fixture.ID] || !sha256Pattern.MatchString(fixture.SHA256) || len(fixture.OwnedSurfaceIDs) == 0 {
			return nil, fmt.Errorf("invalid or duplicate fixture %q", fixture.ID)
		}
		fixtures[fixture.ID] = true
		for _, surfaceID := range fixture.OwnedSurfaceIDs {
			if surfaceID == "" || owned[surfaceID] {
				return nil, fmt.Errorf("invalid or duplicate fixture-owned surface %q", surfaceID)
			}
			owned[surfaceID] = true
		}
	}
	return owned, nil
}

func readAssuranceProfileRows(path string) ([]AssuranceProfileRow, []byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	var document struct {
		Rows []AssuranceProfileRow `json:"rows"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&document); err != nil {
		return nil, nil, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, nil, fmt.Errorf("multiple JSON values")
		}
		return nil, nil, err
	}
	return document.Rows, data, nil
}

func verifyAssuranceProfileInputs(sourceProfilePath, sealedUsagePath, ledgerPath, fixtureManifestPath, localProofPath, sourceSHA, usageSHA, ledgerSHA, manifestSHA, proofSHA string) error {
	for _, input := range []struct{ path, sha string }{{sourceProfilePath, sourceSHA}, {sealedUsagePath, usageSHA}, {ledgerPath, ledgerSHA}, {fixtureManifestPath, manifestSHA}, {localProofPath, proofSHA}} {
		data, err := os.ReadFile(input.path)
		if err != nil || replayBytesSHA256(data) != input.sha {
			return fmt.Errorf("assurance profile input changed during projection")
		}
	}
	return nil
}

// PlanOracle assigns exactly one Salesforce action to every locally proven
// surface. Exclusions receive no parity action and require a reason.
func planOracle(rows []OracleInputRow) (OraclePlan, error) {
	if len(rows) == 0 {
		return OraclePlan{}, fmt.Errorf("oracle rows are required")
	}
	seen := make(map[string]bool, len(rows))
	plan := OraclePlan{Rows: make([]OraclePlanRow, 0, len(rows))}
	for _, row := range rows {
		if row.SurfaceID == "" || seen[row.SurfaceID] {
			return OraclePlan{}, fmt.Errorf("invalid or duplicate oracle surface %q", row.SurfaceID)
		}
		seen[row.SurfaceID] = true
		out := OraclePlanRow{SurfaceID: row.SurfaceID}
		switch row.Disposition {
		case localRuntimeRequired:
			if !row.RuntimeObserved {
				return OraclePlan{}, fmt.Errorf("%s lacks local runtime evidence", row.SurfaceID)
			}
			out.Action = oracleRuntime
		case compileShapeRequired:
			if !row.CompilePassed {
				return OraclePlan{}, fmt.Errorf("%s lacks local compile evidence", row.SurfaceID)
			}
			out.Action = oracleCompile
		case deterministicMockRequired:
			if !row.BehaviorObserved {
				return OraclePlan{}, fmt.Errorf("%s lacks local behavioral evidence", row.SurfaceID)
			}
			if row.Deployable {
				out.Action = oracleCompile
			} else if err := setOracleExclusion(&out, row); err != nil {
				return OraclePlan{}, err
			} else {
				out.Action = oracleLocalContractOnly
			}
		case "hosted-deferred":
			if err := setOracleExclusion(&out, row); err != nil {
				return OraclePlan{}, err
			}
			out.Action = oracleWaiver
		case oracleUnknown:
			out.Action = oracleUnknown
		default:
			return OraclePlan{}, fmt.Errorf("%s has unknown disposition %q", row.SurfaceID, row.Disposition)
		}
		plan.Rows = append(plan.Rows, out)
	}
	sort.Slice(plan.Rows, func(i, j int) bool { return plan.Rows[i].SurfaceID < plan.Rows[j].SurfaceID })
	return plan, nil
}

// PlanOracleForUsage derives the complete private-required set from fresh
// reconciliation, profile rows, and local proof. Directives may only refine
// deterministic-mock and hosted rows; they cannot select a usage subset.
func planOracleForUsage(reconciled UsageReconciliation, profile []OracleProfileRow, proof LocalProof, directives []OracleDirective) (OraclePlan, error) {
	profiles := make(map[string]OracleProfileRow, len(profile))
	for _, row := range profile {
		if row.SurfaceID == "" || row.Disposition == "" || profiles[row.SurfaceID].SurfaceID != "" {
			return OraclePlan{}, fmt.Errorf("invalid or duplicate oracle profile surface %q", row.SurfaceID)
		}
		profiles[row.SurfaceID] = row
	}
	proofs := make(map[string]LocalSurfaceProof, len(proof.Surfaces))
	for _, row := range proof.Surfaces {
		if row.SurfaceID == "" || proofs[row.SurfaceID].SurfaceID != "" {
			return OraclePlan{}, fmt.Errorf("invalid or duplicate local proof surface %q", row.SurfaceID)
		}
		proofs[row.SurfaceID] = row
	}
	directiveBySurface := make(map[string]OracleDirective, len(directives))
	for _, directive := range directives {
		if directive.SurfaceID == "" || directiveBySurface[directive.SurfaceID].SurfaceID != "" || (directive.ExclusionClass == "") != (directive.ExclusionReason == "") {
			return OraclePlan{}, fmt.Errorf("invalid or duplicate oracle directive %q", directive.SurfaceID)
		}
		directiveBySurface[directive.SurfaceID] = directive
	}

	required := make(map[string]bool, len(reconciled.Usage))
	for _, row := range reconciled.Usage {
		switch row.Class {
		case usageClassExact, usageClassCaseAlias, usageClassAggregateParent, usageClassCanonicalAlias:
			if row.SurfaceID == "" {
				return OraclePlan{}, fmt.Errorf("reconciled usage %q lacks a surface", row.UsageKey)
			}
			required[row.SurfaceID] = true
		case usageClassLocalSymbol, usageClassNonSalesforceGenerated:
			if row.SurfaceID != "" {
				return OraclePlan{}, fmt.Errorf("non-Salesforce usage %q selects a surface", row.UsageKey)
			}
		default:
			return OraclePlan{}, fmt.Errorf("reconciled usage %q has unknown class %q", row.UsageKey, row.Class)
		}
	}
	if len(required) == 0 {
		return OraclePlan{}, fmt.Errorf("no reconciled Salesforce surfaces require an oracle action")
	}
	inputs := make([]OracleInputRow, 0, len(required))
	for surfaceID := range required {
		profileRow, ok := profiles[surfaceID]
		if !ok {
			return OraclePlan{}, fmt.Errorf("reconciled surface %q is absent from profile", surfaceID)
		}
		directive := directiveBySurface[surfaceID]
		input := OracleInputRow{SurfaceID: surfaceID, Disposition: profileRow.Disposition, ExclusionClass: directive.ExclusionClass, ExclusionReason: directive.ExclusionReason}
		if profileRow.Disposition == deterministicMockRequired {
			if directive.SurfaceID == "" {
				return OraclePlan{}, fmt.Errorf("deterministic mock %q requires an explicit deployability directive", surfaceID)
			}
			input.Deployable = directive.ExclusionClass == ""
		} else if profileRow.Disposition != "hosted-deferred" && directive.SurfaceID != "" {
			return OraclePlan{}, fmt.Errorf("oracle directive is not allowed for %q", surfaceID)
		}
		if profileRow.Disposition != "hosted-deferred" {
			local, ok := proofs[surfaceID]
			if !ok || local.Disposition != profileRow.Disposition {
				return OraclePlan{}, fmt.Errorf("profile surface %q lacks matching local proof", surfaceID)
			}
			input.RuntimeObserved, input.BehaviorObserved, input.CompilePassed = local.RuntimeObserved, local.BehaviorObserved, local.CompilePassed
		}
		inputs = append(inputs, input)
		delete(directiveBySurface, surfaceID)
	}
	if len(directiveBySurface) != 0 {
		return OraclePlan{}, fmt.Errorf("oracle directives contain absent surfaces")
	}
	return planOracle(inputs)
}

// PlanOracleFromFiles loads only the sealed assurance profile, fresh usage,
// local proof, and directives. It cannot plan from a caller-selected source
// profile or write over a previous plan.
func PlanOracleFromFiles(profilePath, sealedUsagePath, fixtureManifestPath, proofPath, directivePath, outputPath string) (OraclePlan, error) {
	for _, path := range []string{profilePath, sealedUsagePath, fixtureManifestPath, proofPath, directivePath, outputPath} {
		if !filepath.IsAbs(path) {
			return OraclePlan{}, fmt.Errorf("absolute oracle input and output paths are required")
		}
	}
	if _, err := os.Lstat(outputPath); err == nil {
		return OraclePlan{}, fmt.Errorf("oracle plan output already exists: %s", outputPath)
	} else if !os.IsNotExist(err) {
		return OraclePlan{}, err
	}
	profile, profileBytes, err := readExactJSONBytes[AssuranceProfile](profilePath)
	if err != nil {
		return OraclePlan{}, fmt.Errorf("read assurance profile: %w", err)
	}
	sealedUsage, sealedUsageBytes, err := readExactJSONBytes[SealedCorpusUsage](sealedUsagePath)
	if err != nil {
		return OraclePlan{}, fmt.Errorf("read sealed corpus usage: %w", err)
	}
	manifest, manifestBytes, err := readExactJSONBytes[LocalProofFixtureManifest](fixtureManifestPath)
	if err != nil {
		return OraclePlan{}, fmt.Errorf("read fixture manifest: %w", err)
	}
	proof, proofBytes, err := readExactJSONBytes[LocalProof](proofPath)
	if err != nil {
		return OraclePlan{}, fmt.Errorf("read local proof: %w", err)
	}
	if proof.Status != "pass" || ValidateRuntimeArtifact(proof.Candidate) != nil || ValidateRuntimeArtifact(proof.Tools) != nil {
		return OraclePlan{}, fmt.Errorf("local proof has invalid runtime bindings")
	}
	directive, directiveBytes, err := readExactJSONBytes[OracleDirectiveFile](directivePath)
	if err != nil {
		return OraclePlan{}, fmt.Errorf("read oracle directives: %w", err)
	}
	profileSHA256, sealedUsageSHA256 := replayBytesSHA256(profileBytes), replayBytesSHA256(sealedUsageBytes)
	manifestSHA256, proofSHA256, directiveSHA256 := replayBytesSHA256(manifestBytes), replayBytesSHA256(proofBytes), replayBytesSHA256(directiveBytes)
	if profile.FixtureManifestSHA256 != manifestSHA256 || proof.FixtureManifestSHA256 != manifestSHA256 {
		return OraclePlan{}, fmt.Errorf("oracle fixture manifest does not bind profile and proof")
	}
	if err := ValidateLocalProof(proof, manifest); err != nil {
		return OraclePlan{}, fmt.Errorf("validate local proof: %w", err)
	}
	if err := validateAssuranceOracleProfile(profile, sealedUsage, proof, sealedUsageSHA256, proofSHA256); err != nil {
		return OraclePlan{}, err
	}
	if directive.SchemaVersion != 1 || directive.ProfileSHA256 != profile.SourceProfileSHA256 || directive.SealedUsageSHA256 != sealedUsageSHA256 || directive.LocalProofSHA256 != proofSHA256 {
		return OraclePlan{}, fmt.Errorf("oracle directives do not bind authoritative inputs")
	}
	projected := make([]OracleProfileRow, len(profile.Rows))
	for i, row := range profile.Rows {
		projected[i] = OracleProfileRow{SurfaceID: row.SurfaceID, Disposition: row.Disposition}
	}
	plan, err := planOracleForUsage(sealedUsage.Reconciliation, projected, proof, directive.Directives)
	if err != nil {
		return OraclePlan{}, err
	}
	if _, after, err := readExactJSONBytes[AssuranceProfile](profilePath); err != nil || replayBytesSHA256(after) != profileSHA256 {
		return OraclePlan{}, fmt.Errorf("profile changed during oracle planning")
	}
	if _, after, err := readExactJSONBytes[SealedCorpusUsage](sealedUsagePath); err != nil || replayBytesSHA256(after) != sealedUsageSHA256 {
		return OraclePlan{}, fmt.Errorf("sealed corpus usage changed during oracle planning")
	}
	if _, after, err := readExactJSONBytes[LocalProofFixtureManifest](fixtureManifestPath); err != nil || replayBytesSHA256(after) != manifestSHA256 {
		return OraclePlan{}, fmt.Errorf("fixture manifest changed during oracle planning")
	}
	if _, after, err := readExactJSONBytes[LocalProof](proofPath); err != nil || replayBytesSHA256(after) != proofSHA256 {
		return OraclePlan{}, fmt.Errorf("local proof changed during oracle planning")
	}
	if _, after, err := readExactJSONBytes[OracleDirectiveFile](directivePath); err != nil || replayBytesSHA256(after) != directiveSHA256 {
		return OraclePlan{}, fmt.Errorf("oracle directives changed during planning")
	}
	plan.Candidate, plan.Tools = proof.Candidate, proof.Tools
	plan.ProfileSHA256, plan.SealedUsageSHA256, plan.LocalProofSHA256, plan.DirectiveSHA256 = profileSHA256, sealedUsageSHA256, proofSHA256, directiveSHA256
	if err := WriteNewJSON(outputPath, plan); err != nil {
		return OraclePlan{}, err
	}
	return plan, nil
}

func validateAssuranceOracleProfile(profile AssuranceProfile, usage SealedCorpusUsage, proof LocalProof, usageSHA, proofSHA string) error {
	if profile.SchemaVersion != 1 || !sha256Pattern.MatchString(profile.SourceProfileSHA256) || !sha256Pattern.MatchString(profile.LedgerSHA256) || !sha256Pattern.MatchString(profile.PolicySHA256) || !sha256Pattern.MatchString(profile.FixtureManifestSHA256) || profile.SealedUsageSHA256 != usageSHA || profile.SourceProfileSHA256 != usage.ProfileSHA256 || profile.LedgerSHA256 != usage.LedgerSHA256 || profile.PolicySHA256 != usage.PolicySHA256 || profile.LocalProofSHA256 != proofSHA || proof.FixtureManifestSHA256 != profile.FixtureManifestSHA256 {
		return fmt.Errorf("assurance profile does not bind authoritative inputs")
	}
	required, err := oracleRequiredSurfaceIDs(usage.Reconciliation)
	if err != nil {
		return err
	}
	if profile.Total != len(required) || len(profile.Rows) != len(required) {
		return fmt.Errorf("assurance profile row count does not match sealed usage")
	}
	seen := make(map[string]bool, len(profile.Rows))
	rowsByID := make(map[string]AssuranceProfileRow, len(profile.Rows))
	counts := make(map[string]int)
	nonDeferred, hosted := make(map[string]bool), make(map[string]bool)
	for _, row := range profile.Rows {
		if row.SurfaceID == "" || row.Disposition == "" || seen[row.SurfaceID] {
			return fmt.Errorf("invalid or duplicate assurance profile surface %q", row.SurfaceID)
		}
		seen[row.SurfaceID], rowsByID[row.SurfaceID], counts[row.Disposition] = true, row, counts[row.Disposition]+1
	}
	for _, row := range profile.NonDeferredGaps {
		if row.SurfaceID == "" || row.Disposition == "hosted-deferred" || nonDeferred[row.SurfaceID] || !seen[row.SurfaceID] || row != rowsByID[row.SurfaceID] {
			return fmt.Errorf("invalid assurance non-deferred surface %q", row.SurfaceID)
		}
		nonDeferred[row.SurfaceID] = true
	}
	for _, row := range profile.HostedDeferred {
		if row.SurfaceID == "" || row.Disposition != "hosted-deferred" || hosted[row.SurfaceID] || !seen[row.SurfaceID] || row != rowsByID[row.SurfaceID] {
			return fmt.Errorf("invalid assurance hosted surface %q", row.SurfaceID)
		}
		hosted[row.SurfaceID] = true
	}
	for _, id := range required {
		if !seen[id] || (nonDeferred[id] == hosted[id]) {
			return fmt.Errorf("assurance profile does not partition required surface %q", id)
		}
	}
	proofBySurface := make(map[string]LocalSurfaceProof, len(proof.Surfaces))
	for _, surface := range proof.Surfaces {
		profileRow, exists := rowsByID[surface.SurfaceID]
		if !exists || profileRow.Disposition == "hosted-deferred" || proofBySurface[surface.SurfaceID].SurfaceID != "" || surface.Disposition != profileRow.Disposition {
			return fmt.Errorf("local proof does not match assurance profile surface %q", surface.SurfaceID)
		}
		proofBySurface[surface.SurfaceID] = surface
	}
	for _, row := range profile.Rows {
		if row.Disposition != "hosted-deferred" && proofBySurface[row.SurfaceID].SurfaceID == "" {
			return fmt.Errorf("assurance profile non-hosted surface %q lacks local proof", row.SurfaceID)
		}
	}
	if len(profile.ByDisposition) != len(counts) {
		return fmt.Errorf("assurance profile disposition count mismatch")
	}
	for disposition, count := range counts {
		if profile.ByDisposition[disposition] != count {
			return fmt.Errorf("assurance profile disposition count mismatch")
		}
	}
	return nil
}

func setOracleExclusion(out *OraclePlanRow, input OracleInputRow) error {
	if input.ExclusionClass == "" || input.ExclusionReason == "" {
		return fmt.Errorf("%s requires an exclusion class and reason", input.SurfaceID)
	}
	out.ExclusionClass = input.ExclusionClass
	out.ExclusionReason = input.ExclusionReason
	return nil
}

// AuthorizeExclusions selects only the plan's explicit non-parity rows from
// the independently supplied policy. No runtime or compile row can appear in
// the authority.
func authorizeExclusions(plan OraclePlan, policy ExclusionPolicy) (ExclusionAuthority, error) {
	if policy.SchemaVersion != 1 {
		return ExclusionAuthority{}, fmt.Errorf("invalid exclusion policy schema")
	}
	allowed := make(map[string]ExclusionPolicyRow, len(policy.Rows))
	for _, row := range policy.Rows {
		if row.SurfaceID == "" || row.Class == "" || row.Reason == "" || allowed[row.SurfaceID].SurfaceID != "" {
			return ExclusionAuthority{}, fmt.Errorf("invalid or duplicate exclusion policy surface %q", row.SurfaceID)
		}
		allowed[row.SurfaceID] = row
	}
	authority := ExclusionAuthority{}
	seen := make(map[string]bool, len(plan.Rows))
	for _, row := range plan.Rows {
		if row.SurfaceID == "" || seen[row.SurfaceID] {
			return ExclusionAuthority{}, fmt.Errorf("invalid or duplicate oracle plan surface %q", row.SurfaceID)
		}
		seen[row.SurfaceID] = true
		if row.Action != oracleLocalContractOnly && row.Action != oracleWaiver {
			continue
		}
		policyRow, ok := allowed[row.SurfaceID]
		if !ok || policyRow.Class != row.ExclusionClass || policyRow.Reason != row.ExclusionReason {
			return ExclusionAuthority{}, fmt.Errorf("exclusion policy does not authorize %q", row.SurfaceID)
		}
		authority.Rows = append(authority.Rows, policyRow)
	}
	sort.Slice(authority.Rows, func(i, j int) bool { return authority.Rows[i].SurfaceID < authority.Rows[j].SurfaceID })
	return authority, nil
}

// AuthorizeExclusionsFromFiles seals only the plan-derived request against an
// independently checked policy. It derives all bindings from current files;
// no caller-selected artifact or parity credit is accepted.
func AuthorizeExclusionsFromFiles(requestPath, planPath, profilePath, sealedUsagePath, policyPath, outputPath string) (ExclusionAuthority, error) {
	for _, path := range []string{requestPath, planPath, profilePath, sealedUsagePath, policyPath, outputPath} {
		if !filepath.IsAbs(path) {
			return ExclusionAuthority{}, fmt.Errorf("absolute exclusion paths are required")
		}
	}
	if _, err := os.Lstat(outputPath); err == nil {
		return ExclusionAuthority{}, fmt.Errorf("exclusion authority output already exists: %s", outputPath)
	} else if !os.IsNotExist(err) {
		return ExclusionAuthority{}, err
	}
	request, requestBytes, err := readExactJSONBytes[ExclusionRequest](requestPath)
	if err != nil {
		return ExclusionAuthority{}, err
	}
	plan, planBytes, err := readExactJSONBytes[OraclePlan](planPath)
	if err != nil {
		return ExclusionAuthority{}, err
	}
	profile, profileBytes, err := readExactJSONBytes[AssuranceProfile](profilePath)
	if err != nil {
		return ExclusionAuthority{}, err
	}
	usage, usageBytes, err := readExactJSONBytes[SealedCorpusUsage](sealedUsagePath)
	if err != nil {
		return ExclusionAuthority{}, err
	}
	policy, policyBytes, err := readExactJSONBytes[ExclusionPolicy](policyPath)
	if err != nil {
		return ExclusionAuthority{}, err
	}
	planSHA, profileSHA, usageSHA, policySHA := replayBytesSHA256(planBytes), replayBytesSHA256(profileBytes), replayBytesSHA256(usageBytes), replayBytesSHA256(policyBytes)
	if request.SchemaVersion != 1 || ValidateRuntimeArtifact(request.Candidate) != nil || ValidateRuntimeArtifact(request.Tools) != nil || usage.SchemaVersion != 1 || profile.SchemaVersion != 1 || request.PlanSHA256 != planSHA || request.ProfileSHA256 != profileSHA || request.SealedUsageSHA256 != usageSHA || request.DecisionSHA256 != usage.DecisionSHA256 || request.Candidate != plan.Candidate || request.Tools != plan.Tools || request.LocalProofSHA256 != plan.LocalProofSHA256 || plan.ProfileSHA256 != profileSHA || plan.SealedUsageSHA256 != usageSHA || profile.SealedUsageSHA256 != usageSHA || profile.LocalProofSHA256 != plan.LocalProofSHA256 {
		return ExclusionAuthority{}, fmt.Errorf("exclusion request does not bind current authority inputs")
	}
	expected, err := exclusionRowsFromPlan(plan)
	if err != nil || !sameExclusionRows(request.Rows, expected) {
		return ExclusionAuthority{}, fmt.Errorf("exclusion request does not match oracle plan")
	}
	authority, err := authorizeExclusions(plan, policy)
	if err != nil {
		return ExclusionAuthority{}, err
	}
	if !sameExclusionRows(authority.Rows, request.Rows) {
		return ExclusionAuthority{}, fmt.Errorf("exclusion policy changed requested rows")
	}
	authority.Candidate, authority.Tools = request.Candidate, request.Tools
	authority.PlanSHA256, authority.ProfileSHA256, authority.SealedUsageSHA256 = planSHA, profileSHA, usageSHA
	authority.DecisionSHA256, authority.LocalProofSHA256, authority.PolicySHA256 = usage.DecisionSHA256, plan.LocalProofSHA256, policySHA
	if err := verifyExclusionAuthorityInputs([]sealedUsageInput{{requestPath, requestBytes}, {planPath, planBytes}, {profilePath, profileBytes}, {sealedUsagePath, usageBytes}, {policyPath, policyBytes}}); err != nil {
		return ExclusionAuthority{}, err
	}
	if err := WriteNewJSON(outputPath, authority); err != nil {
		return ExclusionAuthority{}, err
	}
	return authority, nil
}

func sameExclusionRows(left, right []ExclusionPolicyRow) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func verifyExclusionAuthorityInputs(inputs []sealedUsageInput) error {
	for _, input := range inputs {
		data, err := os.ReadFile(input.path)
		if err != nil || replayBytesSHA256(data) != replayBytesSHA256(input.data) {
			return fmt.Errorf("exclusion authority input changed during authorization")
		}
	}
	return nil
}
