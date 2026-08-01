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

// SupportPolicyRule encodes one classification rule: a namespace or
// type-family pattern, a disposition, a reason, and optional member
// exceptions that elevate specific members to a different disposition.
type SupportPolicyRule struct {
	Namespace        string                          `json:"namespace,omitempty"`
	TypeFamily       string                          `json:"typeFamily,omitempty"`
	Disposition      SupportDisposition              `json:"disposition"`
	Reason           string                          `json:"reason"`
	MemberExceptions []SupportPolicyMemberException  `json:"memberExceptions,omitempty"`
}

// SupportPolicyMemberException elevates (or preserves) a specific member
// when the parent rule would otherwise classify it.
type SupportPolicyMemberException struct {
	TypeName    string             `json:"typeName"`
	MemberName  string             `json:"memberName,omitempty"`
	Disposition SupportDisposition `json:"disposition,omitempty"`
	Reason      string             `json:"reason,omitempty"`
}

// SupportPolicy is a loaded policy fixture.
type SupportPolicy struct {
	Rules []SupportPolicyRule `json:"rules"`
}

// SupportProfileRow is one Apex surface with its assigned disposition.
type SupportProfileRow struct {
	SurfaceID   string             `json:"surfaceId"`
	Namespace   string             `json:"namespace,omitempty"`
	TypeFamily  string             `json:"typeFamily,omitempty"`
	LedgerShape ShapeState         `json:"ledgerShape"`
	Behavior    BehaviorState      `json:"behavior"`
	Evidence    EvidenceState      `json:"evidence"`
	Disposition SupportDisposition `json:"disposition"`
	MatchRule   string             `json:"matchRule"`
	Reason      string             `json:"reason"`
	Obligation  string             `json:"obligation"`
	UsageKey    string             `json:"usageKey,omitempty"`
}

// SupportProfile is the computed result over a Surface Ledger.
type SupportProfile struct {
	Total            int                        `json:"total"`
	ByDisposition    map[SupportDisposition]int `json:"byDisposition"`
	UnclassifiedRows []SupportProfileRow        `json:"unclassifiedRows,omitempty"`
	NonDeferredGaps  []SupportProfileRow        `json:"nonDeferredGaps,omitempty"`
	HostedDeferred   []SupportProfileRow        `json:"hostedDeferred,omitempty"`
	Rows             []SupportProfileRow        `json:"rows"`
	ValidationErrors []string                   `json:"validationErrors,omitempty"`
	CorpusUsage      []CorpusUsageEntry         `json:"corpusUsage,omitempty"`
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
	}

	// Detect overlapping rules (same namespace or type-family in multiple rules).
	overlapDetected := map[string]bool{}
	nsCount := map[string]int{}
	tfCount := map[string]int{}
	for _, rule := range policy.Rules {
		if rule.Namespace != "" {
			nsCount[rule.Namespace]++
		}
		if rule.TypeFamily != "" {
			tfCount[rule.TypeFamily]++
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

	// Track which member exceptions were used.
	exceptionMatched := map[string]bool{}

	profile := SupportProfile{
		ByDisposition: make(map[SupportDisposition]int),
	}

	var apexRows []SurfaceLedgerRow
	for _, row := range rows {
		if row.Product == ProductApex {
			apexRows = append(apexRows, row)
		}
	}

	for _, row := range apexRows {
		pr := classifyRow(row, policy, exceptionMatched)
		profile.Rows = append(profile.Rows, pr)
		profile.ByDisposition[pr.Disposition]++

		if pr.Disposition == "" {
			profile.UnclassifiedRows = append(profile.UnclassifiedRows, pr)
		} else if pr.Disposition == DispositionHostedDeferred {
			profile.HostedDeferred = append(profile.HostedDeferred, pr)
		} else {
			profile.NonDeferredGaps = append(profile.NonDeferredGaps, pr)
		}
	}

	profile.Total = len(profile.Rows)

	// Join corpus usage if provided.
	if corpusUsage != nil {
		for i := range profile.Rows {
			// Determine the source row to extract namespace/type/member.
			for _, ar := range apexRows {
				if ar.SurfaceID == profile.Rows[i].SurfaceID {
					profile.Rows[i].UsageKey = usageKeyForSurface(ar.Namespace, ar.TypeName, ar.MemberName)
					break
				}
			}
		}
		profile.CorpusUsage = corpusUsage.Usage
	}

	// Detect stale member exceptions.
	for _, rule := range policy.Rules {
		for _, exc := range rule.MemberExceptions {
			key := ruleMatchKey(rule.Namespace, rule.TypeFamily, exc.TypeName, exc.MemberName)
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
	sort.Slice(profile.NonDeferredGaps, func(i, j int) bool {
		return profile.NonDeferredGaps[i].SurfaceID < profile.NonDeferredGaps[j].SurfaceID
	})
	sort.Slice(profile.HostedDeferred, func(i, j int) bool {
		return profile.HostedDeferred[i].SurfaceID < profile.HostedDeferred[j].SurfaceID
	})
	sort.Strings(validationErrors)

	profile.ValidationErrors = validationErrors
	return profile
}

func classifyRow(row SurfaceLedgerRow, policy SupportPolicy, exceptionMatched map[string]bool) SupportProfileRow {
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

	// Match rules in order — first match wins.
	for _, rule := range policy.Rules {
		matched := false

		if rule.Namespace != "" && strings.EqualFold(row.Namespace, rule.Namespace) {
			matched = true
		}
		if !matched && rule.TypeFamily != "" {
			if matchTypeFamily(rule.TypeFamily, row.SalesforceSurfaceFamily) {
				matched = true
			}
			if !matched && matchTypeFamily(rule.TypeFamily, row.Namespace) {
				matched = true
			}
		}

		if !matched {
			continue
		}

		// Check member exceptions.
		for _, exc := range rule.MemberExceptions {
			excTypeMatch := exc.TypeName == "" || strings.EqualFold(row.TypeName, exc.TypeName)
			excMemberMatch := exc.MemberName == "" || strings.EqualFold(row.MemberName, exc.MemberName)

			if excTypeMatch && excMemberMatch {
				exceptionMatched[ruleMatchKey(rule.Namespace, rule.TypeFamily, exc.TypeName, exc.MemberName)] = true
				disp := exc.Disposition
				if disp == "" {
					disp = rule.Disposition
				}
				reason := exc.Reason
				if reason == "" {
					reason = rule.Reason
				}
				pr.Disposition = disp
				pr.MatchRule = ruleMatchLabel(rule, true)
				pr.Reason = reason
				pr.Obligation = string(disp)
				return pr
			}
		}

		pr.Disposition = rule.Disposition
		pr.MatchRule = ruleMatchLabel(rule, false)
		pr.Reason = rule.Reason
		pr.Obligation = string(rule.Disposition)
		return pr
	}

	// Unclassified.
	pr.MatchRule = "none"
	pr.Reason = "no matching policy rule"
	pr.Obligation = "unclassified"
	return pr
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
		return "typeFamily=" + rule.TypeFamily + " (member exception)"
	}
	if rule.Namespace != "" {
		return "namespace=" + rule.Namespace
	}
	return "typeFamily=" + rule.TypeFamily
}

func ruleMatchKey(namespace, typeFamily, typeName, memberName string) string {
	scope := namespace
	if scope == "" {
		scope = typeFamily
	}
	return scope + ":" + typeName + "." + memberName
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
