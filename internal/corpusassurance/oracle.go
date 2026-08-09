package corpusassurance

import (
	"fmt"
	"path/filepath"
	"sort"
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

// OracleDirective records the classification not available from local proof:
// a deterministic mock is deployable unless it names an explicit exclusion.
type OracleDirective struct {
	SurfaceID       string `json:"surfaceId"`
	ExclusionClass  string `json:"exclusionClass,omitempty"`
	ExclusionReason string `json:"exclusionReason,omitempty"`
}

type OracleDirectiveFile struct {
	SchemaVersion     int               `json:"schemaVersion"`
	ProfileSHA256     string            `json:"profileSha256"`
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

type ExclusionAuthority struct {
	Rows []ExclusionPolicyRow `json:"rows"`
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

// PlanOracleFromFiles loads the profile, fresh reconciliation, local proof,
// and directives from one sealed byte sequence each, then revalidates them.
func PlanOracleFromFiles(profilePath, sealedUsagePath, proofPath, directivePath string) (OraclePlan, error) {
	if !filepath.IsAbs(profilePath) || !filepath.IsAbs(sealedUsagePath) || !filepath.IsAbs(proofPath) || !filepath.IsAbs(directivePath) {
		return OraclePlan{}, fmt.Errorf("absolute oracle input paths are required")
	}
	profile, profileBytes, err := readUsageProfileRows(profilePath)
	if err != nil {
		return OraclePlan{}, fmt.Errorf("read oracle profile: %w", err)
	}
	sealedUsage, sealedUsageBytes, err := readExactJSONBytes[SealedCorpusUsage](sealedUsagePath)
	if err != nil {
		return OraclePlan{}, fmt.Errorf("read sealed corpus usage: %w", err)
	}
	proof, proofBytes, err := readExactJSONBytes[LocalProof](proofPath)
	if err != nil {
		return OraclePlan{}, fmt.Errorf("read local proof: %w", err)
	}
	directive, directiveBytes, err := readExactJSONBytes[OracleDirectiveFile](directivePath)
	if err != nil {
		return OraclePlan{}, fmt.Errorf("read oracle directives: %w", err)
	}
	profileSHA256, sealedUsageSHA256 := replayBytesSHA256(profileBytes), replayBytesSHA256(sealedUsageBytes)
	proofSHA256, directiveSHA256 := replayBytesSHA256(proofBytes), replayBytesSHA256(directiveBytes)
	if sealedUsage.SchemaVersion != 1 || sealedUsage.ProfileSHA256 != profileSHA256 || directive.SchemaVersion != 1 || directive.ProfileSHA256 != profileSHA256 || directive.SealedUsageSHA256 != sealedUsageSHA256 || directive.LocalProofSHA256 != proofSHA256 {
		return OraclePlan{}, fmt.Errorf("oracle directives do not bind authoritative inputs")
	}
	projected := make([]OracleProfileRow, len(profile))
	for i, row := range profile {
		projected[i] = OracleProfileRow{SurfaceID: row.SurfaceID, Disposition: row.Disposition}
	}
	plan, err := planOracleForUsage(sealedUsage.Reconciliation, projected, proof, directive.Directives)
	if err != nil {
		return OraclePlan{}, err
	}
	if _, after, err := readUsageProfileRows(profilePath); err != nil || replayBytesSHA256(after) != profileSHA256 {
		return OraclePlan{}, fmt.Errorf("profile changed during oracle planning")
	}
	if _, after, err := readExactJSONBytes[SealedCorpusUsage](sealedUsagePath); err != nil || replayBytesSHA256(after) != sealedUsageSHA256 {
		return OraclePlan{}, fmt.Errorf("sealed corpus usage changed during oracle planning")
	}
	if _, after, err := readExactJSONBytes[LocalProof](proofPath); err != nil || replayBytesSHA256(after) != proofSHA256 {
		return OraclePlan{}, fmt.Errorf("local proof changed during oracle planning")
	}
	if _, after, err := readExactJSONBytes[OracleDirectiveFile](directivePath); err != nil || replayBytesSHA256(after) != directiveSHA256 {
		return OraclePlan{}, fmt.Errorf("oracle directives changed during planning")
	}
	plan.ProfileSHA256, plan.SealedUsageSHA256, plan.LocalProofSHA256, plan.DirectiveSHA256 = profileSHA256, sealedUsageSHA256, proofSHA256, directiveSHA256
	return plan, nil
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
