package surfaceledger

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// SupportDisposition is the single obligation assigned to each Apex surface.
type SupportDisposition string

const (
	DispositionLocalRuntimeRequired      SupportDisposition = "local-runtime-required"
	DispositionDeterministicMockRequired SupportDisposition = "deterministic-mock-required"
	DispositionCompileShapeRequired      SupportDisposition = "compile-shape-required"
	DispositionHostedDeferred            SupportDisposition = "hosted-deferred"
)

// SupportPolicyRule encodes one classification rule: a namespace,
// type-family pattern, or surface-prefix selector; a disposition;
// a reason; and optional member exceptions that elevate specific
// members to a different disposition. When Override is true, the
// rule is an explicit narrower override that wins over broader
// matches during overlap resolution.
type SupportPolicyRule struct {
	Namespace        string                         `json:"namespace,omitempty"`
	TypeFamily       string                         `json:"typeFamily,omitempty"`
	SurfacePrefix    string                         `json:"surfacePrefix,omitempty"`
	Disposition      SupportDisposition             `json:"disposition"`
	Reason           string                         `json:"reason"`
	Override         bool                           `json:"override,omitempty"`
	MemberExceptions []SupportPolicyMemberException `json:"memberExceptions,omitempty"`
}

// SupportPolicyMemberException elevates (or preserves) a specific member
// when the parent rule would otherwise classify it.
type SupportPolicyMemberException struct {
	TypeName    string             `json:"typeName"`
	MemberName  string             `json:"memberName,omitempty"`
	Kind        string             `json:"kind,omitempty"`
	Disposition SupportDisposition `json:"disposition,omitempty"`
	Reason      string             `json:"reason,omitempty"`
}

// SupportPolicy is a loaded policy fixture.
type SupportPolicy struct {
	Rules []SupportPolicyRule `json:"rules"`
}

// SupportProfileRow is one Apex surface with its assigned disposition.
type SupportProfileRow struct {
	SurfaceID             string             `json:"surfaceId"`
	Namespace             string             `json:"namespace,omitempty"`
	TypeFamily            string             `json:"typeFamily,omitempty"`
	LedgerShape           ShapeState         `json:"ledgerShape"`
	Behavior              BehaviorState      `json:"behavior"`
	Evidence              EvidenceState      `json:"evidence"`
	Disposition           SupportDisposition `json:"disposition"`
	MatchRule             string             `json:"matchRule"`
	Reason                string             `json:"reason"`
	Obligation            string             `json:"obligation"`
	GapClass              string             `json:"gapClass,omitempty"`
	UsageKey              string             `json:"usageKey,omitempty"`
	CorpusPassingRefs     int                `json:"corpusPassingRefs,omitempty"`
	CorpusFailureRefs     int                `json:"corpusFailureRefs,omitempty"`
	CorpusPassingProjects int                `json:"corpusPassingProjects,omitempty"`
}

// SupportProfileInputs records the exact bytes used to build a profile.
type SupportProfileInputs struct {
	Files []SupportProfileInput `json:"files"`
}

type SupportProfileInput struct {
	Name   string `json:"name"`
	Path   string `json:"path,omitempty"`
	SHA256 string `json:"sha256"`
}

// SupportProfile is the computed result over a Surface Ledger.
type SupportProfile struct {
	Total            int                        `json:"total"`
	ByDisposition    map[SupportDisposition]int `json:"byDisposition"`
	ByGapClass       map[string]int             `json:"byGapClass"`
	UnclassifiedRows []SupportProfileRow        `json:"unclassifiedRows,omitempty"`
	NonDeferredGaps  []SupportProfileRow        `json:"nonDeferredGaps,omitempty"`
	HostedDeferred   []SupportProfileRow        `json:"hostedDeferred,omitempty"`
	Rows             []SupportProfileRow        `json:"rows"`
	ValidationErrors []string                   `json:"validationErrors,omitempty"`
	CorpusUsage      []CorpusUsageEntry         `json:"corpusUsage,omitempty"`
	Inputs           *SupportProfileInputs      `json:"inputs,omitempty"`
}

// ComputeSupportProfile computes a SupportProfile from ledger rows, a policy,
// and an optional corpus usage. Only Apex rows (product=apex) are classified.
// Non-Apex rows are ignored. Rules are matched in order — first match wins.
// Member exceptions within a matched rule can override the parent disposition
// for specific type.member pairs. If corpusUsage is non-nil, every row receives
// a stable UsageKey and the profile includes the full corpus usage breakdown.
func ComputeSupportProfile(rows []SurfaceLedgerRow, policy SupportPolicy, corpusUsage *CorpusUsage) SupportProfile {
	// Validate policy for overlaps.
	seenNS := map[string]int{}
	seenTF := map[string]int{}
	seenSP := map[string]int{}
	for i, rule := range policy.Rules {
		if rule.Namespace != "" {
			if prev, ok := seenNS[rule.Namespace]; ok {
				// Overlapping namespace rules detected — last wins in classification
				// but we flag it for validation.
				_ = prev
			}
			seenNS[rule.Namespace] = i
		}
		if rule.TypeFamily != "" {
			if prev, ok := seenTF[rule.TypeFamily]; ok {
				_ = prev
			}
			seenTF[rule.TypeFamily] = i
		}
		if rule.SurfacePrefix != "" {
			if prev, ok := seenSP[rule.SurfacePrefix]; ok {
				_ = prev
			}
			seenSP[rule.SurfacePrefix] = i
		}
	}

	// Detect overlapping rules (same namespace or type-family in multiple rules).
	overlapDetected := map[string]bool{}
	nsCount := map[string]int{}
	tfCount := map[string]int{}
	spCount := map[string]int{}
	for _, rule := range policy.Rules {
		if rule.Namespace != "" {
			nsCount[rule.Namespace]++
		}
		if rule.TypeFamily != "" {
			tfCount[rule.TypeFamily]++
		}
		if rule.SurfacePrefix != "" {
			spCount[rule.SurfacePrefix]++
		}
	}

	var validationErrors []string
	for ns, count := range nsCount {
		if count > 1 {
			validationErrors = append(validationErrors, fmt.Sprintf("overlapping namespace rule: %s", ns))
			overlapDetected[ns] = true
		}
	}
	for tf, count := range tfCount {
		if count > 1 {
			validationErrors = append(validationErrors, fmt.Sprintf("overlapping type-family rule: %s", tf))
			overlapDetected[tf] = true
		}
	}
	for sp, count := range spCount {
		if count > 1 {
			validationErrors = append(validationErrors, fmt.Sprintf("overlapping surface-prefix rule: %s", sp))
			overlapDetected[sp] = true
		}
	}

	// Track which member exceptions were used.
	exceptionMatched := map[string]bool{}

	profile := SupportProfile{
		ByDisposition: make(map[SupportDisposition]int),
		ByGapClass:    make(map[string]int),
	}

	var apexRows []SurfaceLedgerRow
	for _, row := range rows {
		if row.Product == ProductApex {
			apexRows = append(apexRows, row)
		}
	}

	// Build corpus usage lookup maps if usage is provided.
	var surfaceKey map[string]string
	var usageIdx map[string]*CorpusUsageEntry
	if corpusUsage != nil {
		surfaceKey = make(map[string]string, len(apexRows))
		for _, ar := range apexRows {
			surfaceKey[ar.SurfaceID] = usageKeyForSurface(ar.Namespace, ar.TypeName, ar.MemberName)
		}
		usageIdx = make(map[string]*CorpusUsageEntry, len(corpusUsage.Usage))
		for i := range corpusUsage.Usage {
			usageIdx[corpusUsage.Usage[i].UsageKey] = &corpusUsage.Usage[i]
		}
	}

	for _, row := range apexRows {
		pr, rowErrors := classifyRow(row, policy, exceptionMatched)
		validationErrors = append(validationErrors, rowErrors...)

		pr.GapClass = classifyGap(pr)

		// Populate corpus fields if usage is supplied.
		if corpusUsage != nil {
			if key, ok := surfaceKey[row.SurfaceID]; ok && key != "" {
				pr.UsageKey = key
				if entry, ok := usageIdx[key]; ok {
					pr.CorpusPassingRefs = entry.PubProdRefs + entry.PubTestRefs + entry.PrivProdRefs + entry.PrivTestRefs
					pr.CorpusFailureRefs = entry.PubFailRefs
					pr.CorpusPassingProjects = entry.PubProdProjects + entry.PubTestProjects + entry.PrivProdProjects + entry.PrivTestProjects
				}
			}
		}

		profile.Rows = append(profile.Rows, pr)
		if pr.Disposition != "" {
			profile.ByDisposition[pr.Disposition]++
		}

		if pr.GapClass != "" {
			profile.ByGapClass[pr.GapClass]++
		}

		if pr.Disposition == "" {
			profile.UnclassifiedRows = append(profile.UnclassifiedRows, pr)
		} else if pr.Disposition == DispositionHostedDeferred {
			profile.HostedDeferred = append(profile.HostedDeferred, pr)
		} else if pr.GapClass != "" {
			profile.NonDeferredGaps = append(profile.NonDeferredGaps, pr)
		}
	}

	profile.Total = len(profile.Rows)

	// Include full corpus usage in the profile.
	if corpusUsage != nil {
		profile.CorpusUsage = corpusUsage.Usage
	}

	// Detect stale member exceptions.
	for _, rule := range policy.Rules {
		for _, exc := range rule.MemberExceptions {
			key := ruleMatchKey(rule.Namespace, rule.TypeFamily, rule.SurfacePrefix, exc.TypeName, exc.MemberName, exc.Kind)
			if !exceptionMatched[key] {
				validationErrors = append(validationErrors,
					fmt.Sprintf("stale member exception: %s.%s (namespace=%s typeFamily=%s)",
						exc.TypeName, exc.MemberName, rule.Namespace, rule.TypeFamily))
			}
		}
	}

	// Detect unclassified rows.
	if len(profile.UnclassifiedRows) > 0 {
		for _, row := range profile.UnclassifiedRows {
			validationErrors = append(validationErrors,
				fmt.Sprintf("unclassified Apex row: %s", row.SurfaceID))
		}
	}

	// Sort everything deterministically.
	sort.Slice(profile.Rows, func(i, j int) bool {
		return profile.Rows[i].SurfaceID < profile.Rows[j].SurfaceID
	})
	sort.Slice(profile.UnclassifiedRows, func(i, j int) bool {
		return profile.UnclassifiedRows[i].SurfaceID < profile.UnclassifiedRows[j].SurfaceID
	})
	if corpusUsage != nil {
		sort.Slice(profile.NonDeferredGaps, func(i, j int) bool {
			a, b := &profile.NonDeferredGaps[i], &profile.NonDeferredGaps[j]
			if a.CorpusPassingRefs != b.CorpusPassingRefs {
				return a.CorpusPassingRefs > b.CorpusPassingRefs
			}
			if a.CorpusPassingProjects != b.CorpusPassingProjects {
				return a.CorpusPassingProjects > b.CorpusPassingProjects
			}
			if a.CorpusFailureRefs != b.CorpusFailureRefs {
				return a.CorpusFailureRefs > b.CorpusFailureRefs
			}
			return a.SurfaceID < b.SurfaceID
		})
	} else {
		sort.Slice(profile.NonDeferredGaps, func(i, j int) bool {
			return profile.NonDeferredGaps[i].SurfaceID < profile.NonDeferredGaps[j].SurfaceID
		})
	}
	sort.Slice(profile.HostedDeferred, func(i, j int) bool {
		return profile.HostedDeferred[i].SurfaceID < profile.HostedDeferred[j].SurfaceID
	})
	sort.Strings(validationErrors)

	profile.ValidationErrors = validationErrors
	return profile
}

// matchInfo records one matching rule (possibly with an activated member exception).
type matchInfo struct {
	rule        SupportPolicyRule
	ruleIndex   int
	isException bool
	disposition SupportDisposition
	reason      string
}

// ruleMatchesRow returns true when the rule's selectors match the given ledger row.
func ruleMatchesRow(rule SupportPolicyRule, row SurfaceLedgerRow) bool {
	if rule.Namespace != "" {
		if strings.EqualFold(row.Namespace, rule.Namespace) {
			return true
		}
		if descendantMatch(rule.Namespace, row.Namespace) {
			return true
		}
	}
	if rule.TypeFamily != "" {
		if matchTypeFamily(rule.TypeFamily, row.SalesforceSurfaceFamily) {
			return true
		}
		if matchTypeFamily(rule.TypeFamily, row.Namespace) {
			return true
		}
	}
	if rule.SurfacePrefix != "" && strings.HasPrefix(row.SurfaceID, rule.SurfacePrefix) {
		return true
	}
	return false
}

func classifyRow(row SurfaceLedgerRow, policy SupportPolicy, exceptionMatched map[string]bool) (SupportProfileRow, []string) {
	pr := SupportProfileRow{
		SurfaceID:   row.SurfaceID,
		Namespace:   row.Namespace,
		LedgerShape: row.GladeShape,
		Behavior:    row.GladeBehavior,
		Evidence:    row.Evidence,
	}

	if row.SalesforceSurfaceFamily != "" && row.SalesforceSurfaceFamily != surfaceFamilyForProduct(ProductApex) {
		pr.TypeFamily = row.SalesforceSurfaceFamily
	}

	// Collect every rule that matches this row.
	var matches []matchInfo
	for i, rule := range policy.Rules {
		if !ruleMatchesRow(rule, row) {
			continue
		}

		// Check member exceptions first.
		var selectedException *SupportPolicyMemberException
		selectedSpecificity := -1
		for j := range rule.MemberExceptions {
			exc := &rule.MemberExceptions[j]
			excTypeMatch := exc.TypeName == "" || strings.EqualFold(row.TypeName, exc.TypeName)
			excMemberMatch := exc.MemberName == "" || strings.EqualFold(row.MemberName, exc.MemberName)
			excKindMatch := exc.Kind == "" || strings.EqualFold(row.Kind, exc.Kind)

			if excTypeMatch && excMemberMatch && excKindMatch {
				exceptionMatched[ruleMatchKey(rule.Namespace, rule.TypeFamily, rule.SurfacePrefix, exc.TypeName, exc.MemberName, exc.Kind)] = true
				specificity := 0
				if exc.TypeName != "" {
					specificity++
				}
				if exc.MemberName != "" {
					specificity++
				}
				if exc.Kind != "" {
					specificity++
				}
				if specificity > selectedSpecificity {
					selectedException = exc
					selectedSpecificity = specificity
				}
			}
		}

		if selectedException == nil {
			matches = append(matches, matchInfo{rule: rule, ruleIndex: i, isException: false, disposition: rule.Disposition, reason: rule.Reason})
		} else {
			disp := selectedException.Disposition
			if disp == "" {
				disp = rule.Disposition
			}
			reason := selectedException.Reason
			if reason == "" {
				reason = rule.Reason
			}
			matches = append(matches, matchInfo{rule: rule, ruleIndex: i, isException: true, disposition: disp, reason: reason})
		}
	}

	// No matches — unclassified.
	if len(matches) == 0 {
		pr.MatchRule = "none"
		pr.Reason = "no matching policy rule"
		pr.Obligation = "unclassified"
		return pr, nil
	}

	// Single match — clean classification.
	if len(matches) == 1 {
		m := matches[0]
		pr.Disposition = m.disposition
		pr.MatchRule = ruleMatchLabel(m.rule, m.isException)
		pr.Reason = m.reason
		pr.Obligation = string(m.disposition)
		return pr, nil
	}

	// Multiple matches — resolve with override logic.
	var overrides []matchInfo
	for _, m := range matches {
		if m.rule.Override {
			overrides = append(overrides, m)
		}
	}

	var errors []string

	if len(overrides) == 0 {
		// Multiple matches, no override — error.
		disps := map[SupportDisposition]bool{}
		for _, m := range matches {
			disps[m.disposition] = true
		}
		if len(disps) > 1 {
			errors = append(errors, fmt.Sprintf("conflicting classifications for row %s: %d matching rules, %d distinct dispositions",
				row.SurfaceID, len(matches), len(disps)))
		} else {
			errors = append(errors, fmt.Sprintf("ambiguous classification for row %s: %d matching rules with same disposition, no override",
				row.SurfaceID, len(matches)))
		}
		// Use first match for row data.
		m := matches[0]
		pr.Disposition = m.disposition
		pr.MatchRule = "ambiguous"
		pr.Reason = m.reason
		pr.Obligation = string(m.disposition)
	} else if len(overrides) == 1 {
		// Exactly one override wins.
		m := overrides[0]
		pr.Disposition = m.disposition
		pr.MatchRule = ruleMatchLabel(m.rule, m.isException)
		pr.Reason = m.reason
		pr.Obligation = string(m.disposition)
	} else {
		// Multiple overrides — error.
		errors = append(errors, fmt.Sprintf("multiple overrides match row %s: %d override rules",
			row.SurfaceID, len(overrides)))
		m := overrides[0]
		pr.Disposition = m.disposition
		pr.MatchRule = "ambiguous"
		pr.Reason = m.reason
		pr.Obligation = string(m.disposition)
	}

	return pr, errors
}

// classifyGap assigns a gap class (missing-shape, missing-behavior, missing-evidence)
// to a classified row, or returns "" when the row is closed under its disposition.
func classifyGap(row SupportProfileRow) string {
	// Hosted-deferred rows never enter the gap queue.
	if row.Disposition == DispositionHostedDeferred {
		return ""
	}

	// Every non-deferred disposition requires a non-absent shape.
	if row.LedgerShape == ShapeAbsent || row.LedgerShape == "" {
		return GapMissingShape
	}

	// compile-shape-required closes with present shape and local fixture evidence.
	if row.Disposition == DispositionCompileShapeRequired {
		if row.Evidence == EvidenceFixture || row.Evidence == EvidenceFixtureAndOracle {
			return ""
		}
		return GapMissingEvidence
	}

	// local-runtime-required and deterministic-mock-required:
	// passive behavior still requires both local fixture and Salesforce oracle evidence.
	if row.Behavior == BehaviorPassive {
		if row.Evidence == EvidenceFixtureAndOracle {
			return ""
		}
		return GapMissingEvidence
	}

	// none, stub-noop, unsupported, and partial are missing-behavior.
	switch row.Behavior {
	case BehaviorNone, BehaviorStubNoOp, BehaviorUnsupported, BehaviorPartial:
		return GapMissingBehavior
	}

	// supported behavior requires both local fixture and Salesforce oracle evidence.
	if row.Behavior == BehaviorSupported {
		if row.Evidence == EvidenceFixtureAndOracle {
			return ""
		}
		return GapMissingEvidence
	}

	// Default: missing behavior.
	return GapMissingBehavior
}

// descendantMatch returns true when child starts with parent + ".",
// case-insensitively. "Database" matches "Database.Cursor" but not "DatabaseX".
func descendantMatch(parent, child string) bool {
	if len(child) <= len(parent)+1 {
		return false
	}
	if child[len(parent)] != '.' {
		return false
	}
	return strings.EqualFold(child[:len(parent)], parent)
}

func matchTypeFamily(pattern, value string) bool {
	if pattern == "" || value == "" {
		return false
	}
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(strings.ToLower(value), strings.ToLower(prefix))
	}
	return strings.EqualFold(pattern, value)
}

func ruleMatchLabel(rule SupportPolicyRule, isException bool) string {
	if isException {
		if rule.Namespace != "" {
			return "namespace=" + rule.Namespace + " (member exception)"
		}
		if rule.SurfacePrefix != "" {
			return "surfacePrefix=" + rule.SurfacePrefix + " (member exception)"
		}
		return "typeFamily=" + rule.TypeFamily + " (member exception)"
	}
	if rule.Namespace != "" {
		return "namespace=" + rule.Namespace
	}
	if rule.SurfacePrefix != "" {
		return "surfacePrefix=" + rule.SurfacePrefix
	}
	return "typeFamily=" + rule.TypeFamily
}

func ruleMatchKey(namespace, typeFamily, surfacePrefix, typeName, memberName, kind string) string {
	scope := namespace
	if scope == "" {
		scope = typeFamily
	}
	if scope == "" {
		scope = surfacePrefix
	}
	return scope + ":" + typeName + "." + memberName + ":" + kind
}

// LoadSupportPolicy reads a SupportPolicy from a JSON file.
func LoadSupportPolicy(path string) (SupportPolicy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return SupportPolicy{}, err
	}
	var policy SupportPolicy
	if err := json.Unmarshal(data, &policy); err != nil {
		return SupportPolicy{}, err
	}
	return policy, nil
}

// WriteSupportProfileJSON writes the profile as indented JSON with a trailing newline.
func WriteSupportProfileJSON(w io.Writer, profile SupportProfile) error {
	data, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		return err
	}
	_, err = w.Write(append(data, '\n'))
	return err
}

// WriteSupportProfileMarkdown writes a concise Markdown summary.
func WriteSupportProfileMarkdown(w io.Writer, profile SupportProfile) error {
	var b strings.Builder

	fmt.Fprintln(&b, "# Apex Local Support Profile")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "- Total Apex rows: %d\n", profile.Total)
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "## Dispositions")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| Disposition | Count |")
	fmt.Fprintln(&b, "| --- | ---: |")
	for _, disp := range []SupportDisposition{
		DispositionLocalRuntimeRequired,
		DispositionDeterministicMockRequired,
		DispositionCompileShapeRequired,
		DispositionHostedDeferred,
	} {
		count := profile.ByDisposition[disp]
		if count > 0 || disp == DispositionHostedDeferred {
			fmt.Fprintf(&b, "| %s | %d |\n", disp, count)
		}
	}
	fmt.Fprintln(&b)

	if len(profile.UnclassifiedRows) > 0 {
		fmt.Fprintf(&b, "- Unclassified rows: %d\n", len(profile.UnclassifiedRows))
		fmt.Fprintln(&b)
	}

	if len(profile.NonDeferredGaps) > 0 {
		fmt.Fprintln(&b, "## Non-Deferred Gap Rows")
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, "| Surface ID | Disposition | Shape | Behavior | Evidence |")
		fmt.Fprintln(&b, "| --- | --- | --- | --- | --- |")
		for _, row := range profile.NonDeferredGaps {
			fmt.Fprintf(&b, "| `%s` | %s | %s | %s | %s |\n",
				row.SurfaceID, row.Disposition, row.LedgerShape, row.Behavior, row.Evidence)
		}
		fmt.Fprintln(&b)
	}

	if len(profile.HostedDeferred) > 0 {
		fmt.Fprintln(&b, "## Hosted-Deferred Rows")
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, "| Surface ID | Namespace | Type Family | Match Rule |")
		fmt.Fprintln(&b, "| --- | --- | --- | --- |")
		for _, row := range profile.HostedDeferred {
			fmt.Fprintf(&b, "| `%s` | %s | %s | %s |\n",
				row.SurfaceID, row.Namespace, row.TypeFamily, row.MatchRule)
		}
		fmt.Fprintln(&b)
	}

	if len(profile.ValidationErrors) > 0 {
		fmt.Fprintln(&b, "## Validation Errors")
		fmt.Fprintln(&b)
		for _, err := range profile.ValidationErrors {
			fmt.Fprintf(&b, "- %s\n", err)
		}
		fmt.Fprintln(&b)
	}

	fmt.Fprintln(&b, "## Counts by Disposition and Closure State")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| Disposition | Count | Closure |")
	fmt.Fprintln(&b, "| --- | ---: | --- |")
	for _, disp := range []SupportDisposition{
		DispositionLocalRuntimeRequired,
		DispositionDeterministicMockRequired,
		DispositionCompileShapeRequired,
		DispositionHostedDeferred,
	} {
		count := profile.ByDisposition[disp]
		closure := "open"
		if disp == DispositionHostedDeferred {
			closure = "deferred"
		}
		fmt.Fprintf(&b, "| %s | %d | %s |\n", disp, count, closure)
	}
	if len(profile.UnclassifiedRows) > 0 {
		fmt.Fprintf(&b, "| unclassified | %d | open |\n", len(profile.UnclassifiedRows))
	}
	fmt.Fprintln(&b)

	_, err := fmt.Fprint(w, b.String())
	return err
}

// usageKeyForSurface returns a stable join key for a namespace/type/member.
func usageKeyForSurface(namespace, typeName, memberName string) string {
	if namespace == "" {
		return ""
	}
	if typeName == "" {
		return namespace
	}
	if memberName == "" {
		return namespace + "." + typeName
	}
	return namespace + "." + typeName + "." + memberName
}
