package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
)

type surfaceStatusRow struct {
	SurfaceID             string   `json:"surfaceId"`
	Disposition           string   `json:"disposition"`
	Obligation            string   `json:"obligation"`
	Namespace             string   `json:"namespace,omitempty"`
	TypeFamily            string   `json:"typeFamily,omitempty"`
	Family                string   `json:"family,omitempty"`
	LedgerShape           string   `json:"ledgerShape"`
	Behavior              string   `json:"behavior"`
	Evidence              string   `json:"evidence"`
	GapClass              string   `json:"gapClass,omitempty"`
	LedgerGapClass        string   `json:"ledgerGapClass,omitempty"`
	Bucket                string   `json:"bucket,omitempty"`
	MatchRule             string   `json:"matchRule"`
	Reason                string   `json:"reason"`
	UsageKey              string   `json:"usageKey,omitempty"`
	CorpusPassingRefs     int      `json:"corpusPassingRefs,omitempty"`
	CorpusFailureRefs     int      `json:"corpusFailureRefs,omitempty"`
	CorpusPassingProjects int      `json:"corpusPassingProjects,omitempty"`
	GapState              string   `json:"gapState"`
	Open                  bool     `json:"open"`
	Blocking              bool     `json:"blocking"`
	CorpusUsed            bool     `json:"corpusUsed"`
	NextActionKey         string   `json:"nextActionKey"`
	Sources               []string `json:"sources,omitempty"`
	DeliveryStates        []string `json:"deliveryStates,omitempty"`
}

type supportProfileInput struct {
	Name   string `json:"name"`
	Path   string `json:"path,omitempty"`
	SHA256 string `json:"sha256"`
}

type supportProfileInputs struct {
	Files []supportProfileInput `json:"files"`
}

type supportProfileArtifact struct {
	Total            int                   `json:"total"`
	ByDisposition    map[string]int        `json:"byDisposition"`
	ByGapClass       map[string]int        `json:"byGapClass"`
	ValidationErrors []string              `json:"validationErrors"`
	Inputs           *supportProfileInputs `json:"inputs,omitempty"`
	Rows             []surfaceStatusRow    `json:"rows"`
}

type pinnedLedgerRow struct {
	SurfaceID string `json:"surfaceId"`
	Product   string `json:"product"`
	GapClass  string `json:"gapClass,omitempty"`
	Bucket    string `json:"bucket,omitempty"`
}

type pinnedLedgerArtifact struct {
	Rows    []pinnedLedgerRow `json:"rows"`
	Summary struct {
		Failures map[string]int `json:"failures"`
	} `json:"summary"`
}

type surfaceStatusGate struct {
	Status                string   `json:"status"`
	Passed                bool     `json:"passed"`
	ValidationErrorCount  int      `json:"validationErrorCount"`
	BlockingRowCount      int      `json:"blockingRowCount"`
	InventoryFailureCount int      `json:"inventoryFailureCount"`
	BlockingReasons       []string `json:"blockingReasons,omitempty"`
}

type surfaceStatusArtifact struct {
	Total                  int                   `json:"total"`
	ByDisposition          map[string]int        `json:"byDisposition"`
	ByObligation           map[string]int        `json:"byObligation"`
	ByGapClass             map[string]int        `json:"byGapClass"`
	InventoryFailureCounts map[string]int        `json:"inventoryFailureCounts"`
	InventoryFailureCount  int                   `json:"inventoryFailureCount"`
	ByGapCategory          map[string]int        `json:"byGapCategory"`
	ByGapState             map[string]int        `json:"byGapState"`
	ByDeliveryState        map[string]int        `json:"byDeliveryState"`
	ByBehavior             map[string]int        `json:"byBehavior"`
	ByEvidence             map[string]int        `json:"byEvidence"`
	ByNamespace            map[string]int        `json:"byNamespace"`
	ByFamily               map[string]int        `json:"byFamily"`
	ByNextAction           map[string]int        `json:"byNextAction"`
	ByCorpusUse            map[string]int        `json:"byCorpusUse"`
	Gate                   surfaceStatusGate     `json:"gate"`
	Inputs                 *supportProfileInputs `json:"inputs,omitempty"`
	ValidationErrors       []string              `json:"validationErrors"`
	Rows                   []surfaceStatusRow    `json:"rows"`
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: go run ./scripts/verify-apex-surface-status.go <profile.json> <status.html>")
		os.Exit(2)
	}
	profileData, err := os.ReadFile(os.Args[1])
	if err != nil {
		fail(err)
	}
	var profile supportProfileArtifact
	if err := json.Unmarshal(profileData, &profile); err != nil {
		fail(fmt.Errorf("decode profile JSON: %w", err))
	}
	htmlData, err := os.ReadFile(os.Args[2])
	if err != nil {
		fail(err)
	}
	payload, err := extractPageData(string(htmlData))
	if err != nil {
		fail(err)
	}
	var page surfaceStatusArtifact
	if err := json.Unmarshal([]byte(payload), &page); err != nil {
		fail(fmt.Errorf("decode embedded page data: %w", err))
	}
	if err := reconcile(profile, page); err != nil {
		fail(err)
	}
	if err := validateHTMLShell(string(htmlData)); err != nil {
		fail(err)
	}
	if err := validateVisibleGate(string(htmlData), page.Gate); err != nil {
		fail(err)
	}
	fmt.Printf("verified Apex surface status: status=%s total=%d rows=%d dispositions=%d gaps=%d\n", page.Gate.Status, page.Total, len(page.Rows), len(page.ByDisposition), len(page.ByGapClass))
}

func extractPageData(html string) (string, error) {
	const open = `<script id="page-data" type="application/json">`
	if strings.Count(html, open) != 1 {
		return "", fmt.Errorf("page-data script count=%d, want 1", strings.Count(html, open))
	}
	start := strings.Index(html, open)
	if start < 0 {
		return "", fmt.Errorf("missing page-data script")
	}
	start += len(open)
	end := strings.Index(html[start:], "</script>")
	if end < 0 {
		return "", fmt.Errorf("missing page-data closing tag")
	}
	payload := html[start : start+end]
	if strings.Contains(payload, "</script>") {
		return "", fmt.Errorf("embedded JSON contains an unescaped script close")
	}
	return payload, nil
}

func reconcile(profile supportProfileArtifact, page surfaceStatusArtifact) error {
	if page.Total != profile.Total {
		return fmt.Errorf("total mismatch: profile=%d page=%d", profile.Total, page.Total)
	}
	if len(profile.Rows) != profile.Total || len(page.Rows) != page.Total {
		return fmt.Errorf("row count mismatch: profile total/rows=%d/%d page total/rows=%d/%d", profile.Total, len(profile.Rows), page.Total, len(page.Rows))
	}
	if err := compareCounts("disposition", profile.ByDisposition, page.ByDisposition); err != nil {
		return err
	}
	if len(page.ByDisposition) != 4 {
		return fmt.Errorf("page disposition key count=%d, want 4", len(page.ByDisposition))
	}
	if err := compareCounts("gap", profile.ByGapClass, page.ByGapClass); err != nil {
		return err
	}
	profileIDs, err := uniqueSortedIDs(profile.Rows)
	if err != nil {
		return fmt.Errorf("profile rows: %w", err)
	}
	pageIDs, err := uniqueSortedIDs(page.Rows)
	if err != nil {
		return fmt.Errorf("page rows: %w", err)
	}
	if len(profileIDs) != len(pageIDs) {
		return fmt.Errorf("unique row count mismatch: profile=%d page=%d", len(profileIDs), len(pageIDs))
	}
	for i := range profileIDs {
		if profileIDs[i] != pageIDs[i] {
			return fmt.Errorf("row ID mismatch at index %d: profile=%q page=%q", i, profileIDs[i], pageIDs[i])
		}
	}
	if !reflect.DeepEqual(profile.Inputs, page.Inputs) {
		return fmt.Errorf("pinned input metadata differs between profile and page")
	}
	if err := validateInputs(profile.Inputs); err != nil {
		return err
	}
	ledger, err := readPinnedLedger(profile.Inputs)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(profile.ValidationErrors, page.ValidationErrors) {
		return fmt.Errorf("validation errors differ between profile and page")
	}
	if err := validateEvidenceReferences(page.Rows, profile.Inputs); err != nil {
		return err
	}
	if err := validateInventory(page, ledger); err != nil {
		return err
	}
	if err := validateRows(profile, page, ledger); err != nil {
		return err
	}
	if err := validateDerivedCounts(profile, page); err != nil {
		return err
	}
	expectedGate := deriveGate(profile.Rows, profile.ValidationErrors, ledger)
	if page.Gate.Status != expectedGate.Status || page.Gate.Passed != expectedGate.Passed ||
		page.Gate.ValidationErrorCount != expectedGate.ValidationErrorCount || page.Gate.BlockingRowCount != expectedGate.BlockingRowCount ||
		page.Gate.InventoryFailureCount != expectedGate.InventoryFailureCount ||
		!reflect.DeepEqual(page.Gate.BlockingReasons, expectedGate.BlockingReasons) {
		return fmt.Errorf("gate mismatch: expected=%s/%t validation=%d blocking=%d inventory=%d got=%s/%t validation=%d blocking=%d inventory=%d",
			expectedGate.Status, expectedGate.Passed, expectedGate.ValidationErrorCount, expectedGate.BlockingRowCount, expectedGate.InventoryFailureCount,
			page.Gate.Status, page.Gate.Passed, page.Gate.ValidationErrorCount, page.Gate.BlockingRowCount, page.Gate.InventoryFailureCount)
	}
	if page.Gate.Passed && page.Gate.Status != "PASS" {
		return fmt.Errorf("passed gate must be labeled PASS")
	}
	if !page.Gate.Passed && page.Gate.Status != "BLOCKED" {
		return fmt.Errorf("blocked gate must be labeled BLOCKED")
	}
	return nil
}

func compareCounts(label string, want, got map[string]int) error {
	keys := make(map[string]bool, len(want)+len(got))
	for key := range want {
		keys[key] = true
	}
	for key := range got {
		keys[key] = true
	}
	for key := range keys {
		if want[key] != got[key] {
			return fmt.Errorf("%s mismatch for %q: want=%d got=%d", label, key, want[key], got[key])
		}
	}
	return nil
}

func readPinnedLedger(inputs *supportProfileInputs) (pinnedLedgerArtifact, error) {
	var ledger pinnedLedgerArtifact
	var ledgerInput *supportProfileInput
	for i := range inputs.Files {
		if inputs.Files[i].Name == "ledger" {
			if ledgerInput != nil {
				return ledger, fmt.Errorf("duplicate pinned input name %q", "ledger")
			}
			ledgerInput = &inputs.Files[i]
		}
	}
	if ledgerInput == nil {
		return ledger, fmt.Errorf("pinned inputs do not include input named %q", "ledger")
	}
	data, err := os.ReadFile(ledgerInput.Path)
	if err != nil {
		return ledger, fmt.Errorf("read pinned ledger: %w", err)
	}
	if err := json.Unmarshal(data, &ledger); err != nil {
		return ledger, fmt.Errorf("decode pinned ledger: %w", err)
	}
	return ledger, nil
}

func validateInventory(page surfaceStatusArtifact, ledger pinnedLedgerArtifact) error {
	ledgerByID := make(map[string]pinnedLedgerRow, len(ledger.Rows))
	var apexRows []pinnedLedgerRow
	for _, row := range ledger.Rows {
		if row.SurfaceID == "" {
			return fmt.Errorf("pinned ledger contains an empty surface ID")
		}
		if _, exists := ledgerByID[row.SurfaceID]; exists {
			return fmt.Errorf("pinned ledger contains duplicate surface ID %q", row.SurfaceID)
		}
		ledgerByID[row.SurfaceID] = row
		if row.Product == "apex" {
			apexRows = append(apexRows, row)
		}
	}
	ledgerIDs, err := uniqueSortedPinnedApexIDs(apexRows)
	if err != nil {
		return err
	}
	pageIDs, err := uniqueSortedIDs(page.Rows)
	if err != nil {
		return fmt.Errorf("page rows: %w", err)
	}
	if len(ledgerIDs) != len(pageIDs) {
		return fmt.Errorf("canonical Apex ID count mismatch: ledger=%d page=%d", len(ledgerIDs), len(pageIDs))
	}
	for i := range ledgerIDs {
		if ledgerIDs[i] != pageIDs[i] {
			return fmt.Errorf("canonical Apex ID mismatch at index %d: ledger=%q page=%q", i, ledgerIDs[i], pageIDs[i])
		}
	}

	rowFailures := make(map[string]int)
	for _, row := range ledger.Rows {
		if row.Bucket == "failure" {
			rowFailures[row.GapClass]++
		}
	}
	for gap, count := range rowFailures {
		if ledger.Summary.Failures[gap] < count {
			return fmt.Errorf("pinned ledger summary failure count for %q=%d is below row failure count %d", gap, ledger.Summary.Failures[gap], count)
		}
	}
	if err := compareCounts("inventory failure counts", ledger.Summary.Failures, page.InventoryFailureCounts); err != nil {
		return err
	}
	wantFailureCount := failureCount(ledger.Summary.Failures)
	if page.InventoryFailureCount != wantFailureCount {
		return fmt.Errorf("inventory failure total mismatch: ledger=%d page=%d", wantFailureCount, page.InventoryFailureCount)
	}
	if page.Gate.InventoryFailureCount != wantFailureCount {
		return fmt.Errorf("gate inventory failure total mismatch: ledger=%d gate=%d", wantFailureCount, page.Gate.InventoryFailureCount)
	}
	for _, row := range page.Rows {
		ledgerRow, ok := ledgerByID[row.SurfaceID]
		if !ok || ledgerRow.Product != "apex" {
			return fmt.Errorf("page row %q is not backed by a pinned Apex ledger row", row.SurfaceID)
		}
		if row.Bucket != ledgerRow.Bucket || row.LedgerGapClass != ledgerRow.GapClass {
			return fmt.Errorf("row %q ledger facts differ: page bucket/gap=%q/%q ledger=%q/%q", row.SurfaceID, row.Bucket, row.LedgerGapClass, ledgerRow.Bucket, ledgerRow.GapClass)
		}
	}
	return nil
}

func uniqueSortedPinnedApexIDs(rows []pinnedLedgerRow) ([]string, error) {
	ids := make([]string, 0, len(rows))
	seen := make(map[string]bool, len(rows))
	for _, row := range rows {
		if seen[row.SurfaceID] {
			return nil, fmt.Errorf("pinned ledger contains duplicate Apex surface ID %q", row.SurfaceID)
		}
		seen[row.SurfaceID] = true
		ids = append(ids, row.SurfaceID)
	}
	sort.Strings(ids)
	return ids, nil
}

func failureCount(counts map[string]int) int {
	total := 0
	for _, count := range counts {
		if count > 0 {
			total += count
		}
	}
	return total
}

func validateRows(profile supportProfileArtifact, page surfaceStatusArtifact, ledger pinnedLedgerArtifact) error {
	profileByID := make(map[string]surfaceStatusRow, len(profile.Rows))
	for _, row := range profile.Rows {
		profileByID[row.SurfaceID] = row
	}
	validDispositions := map[string]bool{
		"local-runtime-required":      true,
		"deterministic-mock-required": true,
		"compile-shape-required":      true,
		"hosted-deferred":             true,
	}
	ledgerByID := make(map[string]pinnedLedgerRow, len(ledger.Rows))
	for _, ledgerRow := range ledger.Rows {
		ledgerByID[ledgerRow.SurfaceID] = ledgerRow
	}
	for _, row := range page.Rows {
		profileRow := profileByID[row.SurfaceID]
		if !validDispositions[row.Disposition] {
			return fmt.Errorf("row %q has no exactly-one policy disposition: %q", row.SurfaceID, row.Disposition)
		}
		if row.Obligation != row.Disposition {
			return fmt.Errorf("row %q obligation=%q does not equal disposition=%q", row.SurfaceID, row.Obligation, row.Disposition)
		}
		if profileRow.Disposition != row.Disposition || profileRow.Obligation != row.Obligation ||
			profileRow.Namespace != row.Namespace ||
			(profileRow.TypeFamily != "" && profileRow.TypeFamily != row.Family) ||
			profileRow.LedgerShape != row.LedgerShape || profileRow.Behavior != row.Behavior ||
			profileRow.Evidence != row.Evidence || profileRow.GapClass != row.GapClass ||
			profileRow.MatchRule != row.MatchRule || profileRow.Reason != row.Reason ||
			profileRow.UsageKey != row.UsageKey ||
			profileRow.CorpusPassingRefs != row.CorpusPassingRefs ||
			profileRow.CorpusFailureRefs != row.CorpusFailureRefs ||
			profileRow.CorpusPassingProjects != row.CorpusPassingProjects {
			return fmt.Errorf("row %q claim fields differ between profile and page", row.SurfaceID)
		}
		if row.CorpusUsed != corpusUsed(profileRow) {
			return fmt.Errorf("row %q corpus-used=%t does not match profile counts", row.SurfaceID, row.CorpusUsed)
		}
		ledgerRow, ok := ledgerByID[row.SurfaceID]
		if !ok {
			return fmt.Errorf("row %q is missing from pinned ledger", row.SurfaceID)
		}
		inventoryFailure := ledgerRow.Bucket == "failure"
		wantBlocking := inventoryFailure || row.Disposition != "hosted-deferred" && (row.Disposition == "" || row.GapClass != "")
		if row.Blocking != wantBlocking || row.Open != wantBlocking {
			return fmt.Errorf("row %q blocking/open=%t/%t want=%t", row.SurfaceID, row.Blocking, row.Open, wantBlocking)
		}
		wantGapState := "closed"
		if inventoryFailure {
			wantGapState = "blocked"
		} else if row.Disposition == "hosted-deferred" {
			wantGapState = "deferred"
		} else if wantBlocking {
			wantGapState = "blocked"
		}
		if row.GapState != wantGapState {
			return fmt.Errorf("row %q gap state=%q want=%q", row.SurfaceID, row.GapState, wantGapState)
		}
		if row.NextActionKey != nextActionKey(row) {
			return fmt.Errorf("row %q next action=%q want=%q", row.SurfaceID, row.NextActionKey, nextActionKey(row))
		}
		if !uniqueStrings(row.DeliveryStates) {
			return fmt.Errorf("row %q has duplicate delivery states", row.SurfaceID)
		}
		covered := hasState(row.DeliveryStates, "covered")
		open := hasState(row.DeliveryStates, "unimplemented/open")
		if covered && (wantBlocking || row.Disposition == "hosted-deferred") {
			return fmt.Errorf("row %q contradicts covered and open/deferred state", row.SurfaceID)
		}
		if open != wantBlocking {
			return fmt.Errorf("row %q open delivery=%t want=%t", row.SurfaceID, open, wantBlocking)
		}
		switch row.Disposition {
		case "hosted-deferred":
			for _, required := range []string{"hosted-deferred", "not-locally-implemented"} {
				if !hasState(row.DeliveryStates, required) {
					return fmt.Errorf("row %q hosted-deferred row missing %q label", row.SurfaceID, required)
				}
			}
			for _, forbidden := range []string{"local-runtime", "deterministic-mock", "compile-shape", "covered"} {
				if hasState(row.DeliveryStates, forbidden) {
					return fmt.Errorf("row %q hosted-deferred row has forbidden %q state", row.SurfaceID, forbidden)
				}
			}
			if !inventoryFailure && hasState(row.DeliveryStates, "unimplemented/open") {
				return fmt.Errorf("row %q hosted-deferred row has forbidden %q state", row.SurfaceID, "unimplemented/open")
			}
		case "local-runtime-required":
			if row.GapClass != "missing-shape" && row.GapClass != "missing-behavior" && !hasState(row.DeliveryStates, "local-runtime") {
				return fmt.Errorf("row %q local-runtime row missing locally executable label", row.SurfaceID)
			}
		case "deterministic-mock-required":
			if row.GapClass == "missing-shape" || row.GapClass == "missing-behavior" {
				if hasState(row.DeliveryStates, "deterministic-mock") {
					return fmt.Errorf("row %q missing mock must not be labeled as implemented deterministic mock", row.SurfaceID)
				}
			} else if !hasState(row.DeliveryStates, "deterministic-mock") {
				return fmt.Errorf("row %q deterministic-mock row missing mock label", row.SurfaceID)
			}
		case "compile-shape-required":
			if row.GapClass != "missing-shape" && !hasState(row.DeliveryStates, "compile-shape") {
				return fmt.Errorf("row %q compile-shape row missing compile-only label", row.SurfaceID)
			}
		}
		if hasState(row.DeliveryStates, "explicit-unsupported") && row.Behavior != "unsupported" {
			return fmt.Errorf("row %q explicit-unsupported state has behavior=%q", row.SurfaceID, row.Behavior)
		}
		if row.Evidence != "none" && len(row.Sources) == 0 {
			return fmt.Errorf("row %q evidence=%q has no exact source reference", row.SurfaceID, row.Evidence)
		}
	}
	return nil
}

func validateDerivedCounts(profile supportProfileArtifact, page surfaceStatusArtifact) error {
	dispositions := make(map[string]int)
	obligations := make(map[string]int)
	gaps := make(map[string]int)
	gapCategories := map[string]int{
		"missing-shape":      0,
		"missing-behavior":   0,
		"missing-evidence":   0,
		"mismatch":           0,
		"stale/inconclusive": 0,
		"unclassified":       0,
		"inventory-failure":  0,
	}
	gapStates := map[string]int{"blocked": 0, "deferred": 0, "closed": 0}
	delivery := make(map[string]int)
	behavior := make(map[string]int)
	evidence := make(map[string]int)
	namespaces := make(map[string]int)
	families := make(map[string]int)
	nextActions := make(map[string]int)
	corpusUse := map[string]int{"used": 0, "zero-use": 0}
	for _, row := range page.Rows {
		dispositions[row.Disposition]++
		obligations[row.Obligation]++
		behavior[row.Behavior]++
		evidence[row.Evidence]++
		namespaces[row.Namespace]++
		families[row.Family]++
		nextActions[row.NextActionKey]++
		gapStates[row.GapState]++
		for _, state := range row.DeliveryStates {
			delivery[state]++
		}
		if row.CorpusUsed {
			corpusUse["used"]++
		} else {
			corpusUse["zero-use"]++
		}
		if row.GapClass != "" {
			gaps[row.GapClass]++
		}
		if category := gapCategory(row); category != "" {
			gapCategories[category]++
		}
	}
	for label, want := range map[string]map[string]int{
		"disposition":  dispositions,
		"obligation":   obligations,
		"gap":          gaps,
		"gap category": gapCategories,
		"gap state":    gapStates,
		"delivery":     delivery,
		"behavior":     behavior,
		"evidence":     evidence,
		"namespace":    namespaces,
		"family":       families,
		"next action":  nextActions,
		"corpus use":   corpusUse,
	} {
		var got map[string]int
		switch label {
		case "disposition":
			got = page.ByDisposition
		case "obligation":
			got = page.ByObligation
		case "gap":
			got = page.ByGapClass
		case "gap category":
			got = page.ByGapCategory
		case "gap state":
			got = page.ByGapState
		case "delivery":
			got = page.ByDeliveryState
		case "behavior":
			got = page.ByBehavior
		case "evidence":
			got = page.ByEvidence
		case "namespace":
			got = page.ByNamespace
		case "family":
			got = page.ByFamily
		case "next action":
			got = page.ByNextAction
		case "corpus use":
			got = page.ByCorpusUse
		}
		if err := compareCounts(label, want, got); err != nil {
			return err
		}
	}
	return compareCounts("profile disposition", profile.ByDisposition, dispositions)
}

func gapCategory(row surfaceStatusRow) string {
	if row.Bucket == "failure" {
		return "inventory-failure"
	}
	if row.Disposition == "" {
		return "unclassified"
	}
	switch row.GapClass {
	case "":
		return ""
	case "missing-shape":
		return "missing-shape"
	case "missing-behavior":
		return "missing-behavior"
	case "missing-evidence":
		return "missing-evidence"
	case "stale-glade-shape", "api-version-change":
		return "stale/inconclusive"
	case "docs-org-mismatch", "return-type-mismatch", "parameter-mismatch", "signature-changed":
		return "mismatch"
	default:
		return "unclassified"
	}
}

func deriveGate(rows []surfaceStatusRow, validationErrors []string, ledger pinnedLedgerArtifact) surfaceStatusGate {
	blocking := 0
	ledgerByID := make(map[string]pinnedLedgerRow, len(ledger.Rows))
	for _, ledgerRow := range ledger.Rows {
		ledgerByID[ledgerRow.SurfaceID] = ledgerRow
	}
	for _, row := range rows {
		ledgerRow := ledgerByID[row.SurfaceID]
		if ledgerRow.Bucket == "failure" || row.Disposition != "hosted-deferred" && (row.Disposition == "" || row.GapClass != "") {
			blocking++
		}
	}
	gate := surfaceStatusGate{
		Status:                "PASS",
		Passed:                true,
		ValidationErrorCount:  len(validationErrors),
		BlockingRowCount:      blocking,
		InventoryFailureCount: failureCount(ledger.Summary.Failures),
	}
	if len(validationErrors) > 0 {
		gate.Status = "BLOCKED"
		gate.Passed = false
		gate.BlockingReasons = append(gate.BlockingReasons, "profile validation errors")
	}
	if blocking > 0 {
		gate.Status = "BLOCKED"
		gate.Passed = false
		gate.BlockingReasons = append(gate.BlockingReasons, "non-deferred gap/open rows")
	}
	if gate.InventoryFailureCount > 0 {
		gate.Status = "BLOCKED"
		gate.Passed = false
		gate.BlockingReasons = append(gate.BlockingReasons, "pinned ledger inventory failures")
	}
	return gate
}

func corpusUsed(row surfaceStatusRow) bool {
	return row.CorpusPassingRefs > 0 || row.CorpusFailureRefs > 0 || row.CorpusPassingProjects > 0
}

func nextActionKey(row surfaceStatusRow) string {
	if row.Bucket == "failure" {
		return "inventory-failure"
	}
	if row.Disposition == "hosted-deferred" {
		return "hosted-deferred"
	}
	switch row.GapClass {
	case "missing-shape":
		return "missing-shape"
	case "missing-behavior":
		return "missing-behavior"
	case "missing-evidence":
		return "missing-evidence"
	}
	if row.Disposition == "" {
		return "open"
	}
	return "closed"
}

func validateHTMLShell(html string) error {
	lower := strings.ToLower(html)
	for _, marker := range []string{
		`id="search"`, `id="obligation-filter"`, `id="delivery-filter"`, `id="behavior-filter"`,
		`id="evidence-filter"`, `id="gap-filter"`, `id="namespace-filter"`, `id="family-filter"`,
		`id="corpus-filter"`, `id="action-filter"`, `id="completion-gate"`, `locally executable`,
		`deterministic mock`, `compile-only`, `explicitly unsupported`, `hosted deferred`,
		`not locally implemented`, `covered`, `open / blocking`, `inventory-failure`,
		`reconcile docs/org/glade shape or parser failure`,
	} {
		if !strings.Contains(lower, strings.ToLower(marker)) {
			return fmt.Errorf("HTML missing required status-page marker %q", marker)
		}
	}
	for _, marker := range []string{
		`<script src=`, `<link href=`, `<link rel=`, `<img src=`, `<iframe src=`, `<object data=`,
		`url(http://`, `url(https://`,
	} {
		if strings.Contains(lower, marker) {
			return fmt.Errorf("HTML contains a required network asset marker %q", marker)
		}
	}
	return nil
}

func validateVisibleGate(html string, gate surfaceStatusGate) error {
	lower := strings.ToLower(html)
	wantStatus := strings.ToLower(gate.Status)
	dataMarker := `data-gate-status="` + wantStatus + `"`
	if strings.Count(lower, `data-gate-status="`) != 1 || !strings.Contains(lower, dataMarker) {
		return fmt.Errorf("visible gate status does not match embedded status %q", gate.Status)
	}
	strongMarker := "<strong>" + wantStatus + "</strong>"
	if strings.Count(lower, "<strong>") == 0 || !strings.Contains(lower, strongMarker) {
		return fmt.Errorf("visible gate label does not match embedded status %q", gate.Status)
	}
	if gate.InventoryFailureCount > 0 && strings.Contains(lower, `data-gate-status="pass"`) {
		return fmt.Errorf("visible gate cannot PASS with %d inventory failures", gate.InventoryFailureCount)
	}
	return nil
}

func validateInputs(inputs *supportProfileInputs) error {
	if inputs == nil || len(inputs.Files) == 0 {
		return fmt.Errorf("profile has no pinned input paths/hashes")
	}
	seenNames := make(map[string]bool, len(inputs.Files))
	for _, input := range inputs.Files {
		if input.Name == "" || input.Path == "" {
			return fmt.Errorf("pinned input %q must include name and path", input.Name)
		}
		if len(input.SHA256) != 64 {
			return fmt.Errorf("pinned input %q has SHA-256 length %d, want 64", input.Name, len(input.SHA256))
		}
		if _, err := hex.DecodeString(input.SHA256); err != nil {
			return fmt.Errorf("pinned input %q has invalid SHA-256: %w", input.Name, err)
		}
		if seenNames[input.Name] {
			return fmt.Errorf("duplicate pinned input name %q", input.Name)
		}
		seenNames[input.Name] = true
		file, err := os.Open(input.Path)
		if err != nil {
			return fmt.Errorf("read pinned input %q: %w", input.Name, err)
		}
		hash := sha256.New()
		_, copyErr := io.Copy(hash, file)
		closeErr := file.Close()
		if copyErr != nil {
			return fmt.Errorf("read pinned input %q: %w", input.Name, copyErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close pinned input %q: %w", input.Name, closeErr)
		}
		if got := hex.EncodeToString(hash.Sum(nil)); got != strings.ToLower(input.SHA256) {
			return fmt.Errorf("pinned input %q SHA-256 mismatch: profile=%s actual=%s", input.Name, input.SHA256, got)
		}
	}
	return nil
}

func validateEvidenceReferences(rows []surfaceStatusRow, inputs *supportProfileInputs) error {
	references := make(map[string]bool)
	maxReferenceLength := 0
	for _, row := range rows {
		if row.Evidence == "none" {
			continue
		}
		for _, source := range row.Sources {
			if source == "" {
				return fmt.Errorf("row %q has an empty evidence reference", row.SurfaceID)
			}
			references[source] = false
			if len(source) > maxReferenceLength {
				maxReferenceLength = len(source)
			}
		}
	}
	if len(references) == 0 {
		return nil
	}
	patterns := make([]string, 0, len(references))
	for source := range references {
		patterns = append(patterns, regexp.QuoteMeta(source))
	}
	sort.Slice(patterns, func(i, j int) bool {
		if len(patterns[i]) != len(patterns[j]) {
			return len(patterns[i]) > len(patterns[j])
		}
		return patterns[i] < patterns[j]
	})
	matcher, err := regexp.Compile("(?:" + strings.Join(patterns, "|") + ")")
	if err != nil {
		return fmt.Errorf("compile evidence reference matcher: %w", err)
	}
	remaining := len(references)
	for _, input := range inputs.Files {
		file, err := os.Open(input.Path)
		if err != nil {
			return fmt.Errorf("read pinned input %q for evidence references: %w", input.Name, err)
		}
		chunk := make([]byte, 1024*1024)
		carry := make([]byte, 0, maxReferenceLength)
		for {
			n, readErr := file.Read(chunk)
			if n > 0 {
				combined := make([]byte, len(carry)+n)
				copy(combined, carry)
				copy(combined[len(carry):], chunk[:n])
				for _, match := range matcher.FindAll(combined, -1) {
					source := string(match)
					if !references[source] {
						references[source] = true
						remaining--
					}
				}
				if remaining == 0 {
					_ = file.Close()
					return nil
				}
				if maxReferenceLength > 1 {
					keep := maxReferenceLength - 1
					if len(combined) < keep {
						keep = len(combined)
					}
					carry = append(carry[:0], combined[len(combined)-keep:]...)
				}
			}
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				_ = file.Close()
				return fmt.Errorf("read pinned input %q for evidence references: %w", input.Name, readErr)
			}
		}
		if err := file.Close(); err != nil {
			return fmt.Errorf("close pinned input %q for evidence references: %w", input.Name, err)
		}
	}
	for source, found := range references {
		if !found {
			return fmt.Errorf("evidence reference %q is absent from pinned inputs", source)
		}
	}
	return nil
}

func hasState(states []string, want string) bool {
	for _, state := range states {
		if state == want {
			return true
		}
	}
	return false
}

func uniqueStrings(values []string) bool {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if seen[value] {
			return false
		}
		seen[value] = true
	}
	return true
}

func uniqueSortedIDs(rows []surfaceStatusRow) ([]string, error) {
	ids := make([]string, 0, len(rows))
	seen := make(map[string]bool, len(rows))
	for _, row := range rows {
		if row.SurfaceID == "" {
			return nil, fmt.Errorf("empty surface ID")
		}
		if seen[row.SurfaceID] {
			return nil, fmt.Errorf("duplicate surface ID %q", row.SurfaceID)
		}
		seen[row.SurfaceID] = true
		ids = append(ids, row.SurfaceID)
	}
	sort.Strings(ids)
	return ids, nil
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
