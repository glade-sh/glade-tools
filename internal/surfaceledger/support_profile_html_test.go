package surfaceledger

import (
	"bytes"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestWriteSupportProfileHTMLEmbedsEveryProfileRowOnce(t *testing.T) {
	ledger, policy, corpus := supportProfileHTMLFixture()
	profile := ComputeSupportProfile(ledger.Rows, policy, &corpus)

	var out bytes.Buffer
	if err := WriteSupportProfileHTML(&out, profile, ledger); err != nil {
		t.Fatalf("WriteSupportProfileHTML: %v", err)
	}

	payloadJSON := extractSupportProfileHTMLData(t, out.String())
	var payload SupportProfileHTMLPage
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		t.Fatalf("decode embedded page data: %v\n%s", err, payloadJSON)
	}

	wantIDs := make([]string, 0, len(profile.Rows))
	for _, row := range profile.Rows {
		wantIDs = append(wantIDs, row.SurfaceID)
	}
	sort.Strings(wantIDs)
	gotIDs := make([]string, 0, len(payload.Rows))
	seen := make(map[string]int, len(payload.Rows))
	for _, row := range payload.Rows {
		gotIDs = append(gotIDs, row.SurfaceID)
		seen[row.SurfaceID]++
	}
	sort.Strings(gotIDs)
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("embedded row IDs = %#v, want %#v", gotIDs, wantIDs)
	}
	for _, id := range wantIDs {
		if seen[id] != 1 {
			t.Fatalf("embedded row %q appears %d times, want exactly once", id, seen[id])
		}
		encodedID, err := json.Marshal(id)
		if err != nil {
			t.Fatal(err)
		}
		if count := strings.Count(payloadJSON, string(encodedID)); count != 1 {
			t.Fatalf("encoded row ID %q appears %d times in embedded data, want once", id, count)
		}
	}
}

func TestWriteSupportProfileHTMLReconcilesCountsAndJoinsEvidence(t *testing.T) {
	ledger, policy, corpus := supportProfileHTMLFixture()
	profile := ComputeSupportProfile(ledger.Rows, policy, &corpus)
	profile.Inputs = &SupportProfileInputs{Files: []SupportProfileInput{{
		Name:   "ledger",
		Path:   "/pinned/current-base/SURFACE_LEDGER.json",
		SHA256: strings.Repeat("a", 64),
	}}}

	var out bytes.Buffer
	if err := WriteSupportProfileHTML(&out, profile, ledger); err != nil {
		t.Fatalf("WriteSupportProfileHTML: %v", err)
	}
	var payload SupportProfileHTMLPage
	if err := json.Unmarshal([]byte(extractSupportProfileHTMLData(t, out.String())), &payload); err != nil {
		t.Fatalf("decode embedded page data: %v", err)
	}

	if payload.Total != profile.Total {
		t.Fatalf("embedded total = %d, want %d", payload.Total, profile.Total)
	}
	wantDisposition := map[SupportDisposition]int{
		DispositionLocalRuntimeRequired:      profile.ByDisposition[DispositionLocalRuntimeRequired],
		DispositionDeterministicMockRequired: profile.ByDisposition[DispositionDeterministicMockRequired],
		DispositionCompileShapeRequired:      profile.ByDisposition[DispositionCompileShapeRequired],
		DispositionHostedDeferred:            profile.ByDisposition[DispositionHostedDeferred],
	}
	if !reflect.DeepEqual(payload.ByDisposition, wantDisposition) {
		t.Fatalf("embedded dispositions = %#v, want %#v", payload.ByDisposition, wantDisposition)
	}
	if !reflect.DeepEqual(payload.ByGapClass, profile.ByGapClass) {
		t.Fatalf("embedded gaps = %#v, want %#v", payload.ByGapClass, profile.ByGapClass)
	}
	wantDeliveryStates := map[string]int{}
	for _, row := range payload.Rows {
		for _, state := range row.DeliveryStates {
			wantDeliveryStates[state]++
		}
	}
	if !reflect.DeepEqual(payload.ByDeliveryState, wantDeliveryStates) {
		t.Fatalf("embedded delivery states = %#v, want %#v", payload.ByDeliveryState, wantDeliveryStates)
	}
	if !reflect.DeepEqual(payload.Inputs, profile.Inputs) {
		t.Fatalf("embedded inputs = %#v, want %#v", payload.Inputs, profile.Inputs)
	}
	if !strings.Contains(string(extractSupportProfileHTMLData(t, out.String())), "/pinned/current-base/SURFACE_LEDGER.json") ||
		!strings.Contains(string(extractSupportProfileHTMLData(t, out.String())), strings.Repeat("a", 64)) {
		t.Fatal("embedded input path/hash missing")
	}

	var joined *SupportProfileHTMLRow
	for i := range payload.Rows {
		if payload.Rows[i].SurfaceID == "apex:System.Widget.run(String)" {
			joined = &payload.Rows[i]
			break
		}
	}
	if joined == nil {
		t.Fatal("missing corpus-backed sample row")
	}
	if joined.Namespace != "System" || joined.TypeName != "Widget" || joined.MemberName != "run" ||
		joined.Kind != KindMethod || joined.Signature != "run(String)" {
		t.Fatalf("joined identity = %#v", joined)
	}
	if joined.Docs != SourcePresent || joined.Org != SourcePresent {
		t.Fatalf("joined docs/org = %s/%s, want present/present", joined.Docs, joined.Org)
	}
	if !reflect.DeepEqual(joined.Sources, []string{"docs:widget", "fixture:widget-run"}) {
		t.Fatalf("joined sources = %#v", joined.Sources)
	}
	if joined.Corpus == nil {
		t.Fatal("corpus usage was not joined")
	}
	if joined.Corpus.UsageKey != "System.Widget.run" ||
		joined.Corpus.PubProdRefs != 2 ||
		joined.Corpus.PubTestRefs != 3 ||
		joined.Corpus.PubFailRefs != 5 ||
		joined.Corpus.PrivProdRefs != 7 ||
		joined.Corpus.PrivTestRefs != 11 ||
		joined.Corpus.PubProdProjects != 1 ||
		joined.Corpus.PubTestProjects != 2 ||
		joined.Corpus.PubFailProjects != 3 ||
		joined.Corpus.PrivProdProjects != 4 ||
		joined.Corpus.PrivTestProjects != 5 {
		t.Fatalf("joined corpus split = %#v", joined.Corpus)
	}
	if joined.Disposition != DispositionLocalRuntimeRequired ||
		joined.MatchRule != "namespace=System" ||
		joined.Reason != "local widget runtime" ||
		joined.GapClass != "" ||
		joined.Open ||
		joined.NextAction != "no current-base action" {
		t.Fatalf("joined profile state = %#v", joined)
	}
	if joined.DocsSource != "docs/widget.md" ||
		joined.ShapeSource != "shape-fixture" ||
		joined.BehaviorSource != "behavior-fixture" ||
		joined.ImplementationDecision != "runtime" ||
		joined.Notes != "safe sample note" {
		t.Fatalf("joined evidence details = %#v", joined)
	}
}

func TestWriteSupportProfileHTMLContainsRequiredFiltersLegendAndActions(t *testing.T) {
	ledger, policy, corpus := supportProfileHTMLFixture()
	profile := ComputeSupportProfile(ledger.Rows, policy, &corpus)

	var out bytes.Buffer
	if err := WriteSupportProfileHTML(&out, profile, ledger); err != nil {
		t.Fatalf("WriteSupportProfileHTML: %v", err)
	}
	html := strings.ToLower(out.String())
	for _, required := range []string{
		"completion-gate",
		"blocked",
		"behavior-filter",
		"evidence-filter",
		"gap-filter",
		"namespace-filter",
		"family-filter",
		"corpus-filter",
		"locally executable",
		"local runtime",
		"deterministic mock",
		"compile-only",
		"compile shape",
		"hosted deferred",
		"not locally implemented",
		"covered",
		"explicit unsupported",
		"open / blocking",
		"unimplemented/open",
		"not locally implemented",
		"inventory-failure",
		"reconcile docs/org/glade shape or parser failure",
		"hosted-deferred is inventoried but not locally implemented",
		"deterministic mock is executable local behavior but not the hosted service",
		"compile shape is not a runtime claim",
		"open rows block completeness",
		"implement/correct shape",
		"implement local behavior or the declared mock",
		"add the exact evidence required by the disposition",
		"monitor release/corpus and retain the stated reason",
		"no current-base action",
		"shown-count",
		"delivery totals",
		"derived from every row; states may overlap",
		"mock implemented",
		"explicitly unsupported",
		"page-size",
		"profile validation errors",
		"evidence/source ids",
		"docs return type",
		"org return type",
		"glade return type",
	} {
		if !strings.Contains(html, required) {
			t.Fatalf("HTML missing required text %q", required)
		}
	}
}

func TestWriteSupportProfileHTMLDerivesExhaustiveFacetTotalsAndBlockingGate(t *testing.T) {
	ledger, policy, corpus := supportProfileHTMLFixture()
	profile := ComputeSupportProfile(ledger.Rows, policy, &corpus)

	var out bytes.Buffer
	if err := WriteSupportProfileHTML(&out, profile, ledger); err != nil {
		t.Fatalf("WriteSupportProfileHTML: %v", err)
	}
	var page SupportProfileHTMLPage
	if err := json.Unmarshal([]byte(extractSupportProfileHTMLData(t, out.String())), &page); err != nil {
		t.Fatalf("decode page: %v", err)
	}

	if page.Gate.Status != htmlGateBlocked || page.Gate.Passed || page.Gate.BlockingRowCount != 3 || page.Gate.ValidationErrorCount != 0 {
		t.Fatalf("gate = %#v, want blocked with three blocking rows", page.Gate)
	}
	if page.ByObligation["local-runtime-required"] != 1 ||
		page.ByObligation["deterministic-mock-required"] != 2 ||
		page.ByObligation["compile-shape-required"] != 1 ||
		page.ByObligation["hosted-deferred"] != 1 {
		t.Fatalf("obligation totals = %#v", page.ByObligation)
	}
	if page.ByBehavior[string(BehaviorSupported)] != 1 ||
		page.ByBehavior[string(BehaviorStubNoOp)] != 1 ||
		page.ByBehavior[string(BehaviorPassive)] != 1 ||
		page.ByBehavior[string(BehaviorNone)] != 1 ||
		page.ByBehavior[string(BehaviorUnsupported)] != 1 {
		t.Fatalf("behavior totals = %#v", page.ByBehavior)
	}
	if page.ByEvidence[string(EvidenceFixtureAndOracle)] != 1 ||
		page.ByEvidence[string(EvidenceFixture)] != 1 ||
		page.ByEvidence[string(EvidenceDocs)] != 1 ||
		page.ByEvidence[string(EvidenceNone)] != 2 {
		t.Fatalf("evidence totals = %#v", page.ByEvidence)
	}
	if page.ByGapState[htmlGapStateBlocked] != 3 || page.ByGapState[htmlGapStateDeferred] != 1 || page.ByGapState[htmlGapStateClosed] != 1 {
		t.Fatalf("gap state totals = %#v", page.ByGapState)
	}
	if page.ByCorpusUse[htmlCorpusUseUsed] != 1 || page.ByCorpusUse[htmlCorpusUseZero] != 4 {
		t.Fatalf("corpus totals = %#v", page.ByCorpusUse)
	}

	for _, row := range page.Rows {
		wantBlocking := row.Disposition != DispositionHostedDeferred && (row.Disposition == "" || row.GapClass != "")
		if row.Blocking != wantBlocking || row.Open != wantBlocking {
			t.Fatalf("row %s blocking/open = %t/%t, want %t", row.SurfaceID, row.Blocking, row.Open, wantBlocking)
		}
		if row.Disposition == DispositionHostedDeferred && containsString(row.DeliveryStates, htmlDeliveryCovered) {
			t.Fatalf("deferred row %s was covered: %#v", row.SurfaceID, row.DeliveryStates)
		}
	}
}

func TestWriteSupportProfileHTMLGatePassesOnlyForClosedProfile(t *testing.T) {
	ledger, policy, corpus := supportProfileHTMLFixture()
	ledger.Rows = ledger.Rows[:1]
	profile := ComputeSupportProfile(ledger.Rows, policy, &corpus)
	profile.ValidationErrors = nil

	var out bytes.Buffer
	if err := WriteSupportProfileHTML(&out, profile, ledger); err != nil {
		t.Fatalf("WriteSupportProfileHTML: %v", err)
	}
	var page SupportProfileHTMLPage
	if err := json.Unmarshal([]byte(extractSupportProfileHTMLData(t, out.String())), &page); err != nil {
		t.Fatalf("decode page: %v", err)
	}
	if page.Gate.Status != htmlGatePass || !page.Gate.Passed || page.Gate.BlockingRowCount != 0 {
		t.Fatalf("gate = %#v, want PASS", page.Gate)
	}
	if !strings.Contains(strings.ToLower(out.String()), `data-gate-status="pass"`) {
		t.Fatalf("missing visible PASS gate banner")
	}
}

func TestWriteSupportProfileHTMLInventoryFailureBlocksHostedDeferredRow(t *testing.T) {
	ledger := SurfaceLedger{
		SchemaVersion: SchemaVersion,
		Rows: []SurfaceLedgerRow{{
			SurfaceID:     "apex:ConnectApi.Remote",
			Product:       ProductApex,
			Area:          AreaRuntime,
			Namespace:     "ConnectApi",
			TypeName:      "Remote",
			Kind:          KindType,
			Docs:          SourcePresent,
			Org:           SourcePresent,
			GladeShape:    ShapeTypeKnown,
			GladeBehavior: BehaviorSupported,
			Evidence:      EvidenceFixtureAndOracle,
			Sources:       []string{"fixture:connect-api"},
			GapClass:      GapDocsOrgMismatch,
			Bucket:        BucketFailure,
		}},
		Summary: LedgerSummary{Failures: map[string]int{GapDocsOrgMismatch: 1}, Total: 1},
	}
	policy := SupportPolicy{Rules: []SupportPolicyRule{{
		Namespace:   "ConnectApi",
		Disposition: DispositionHostedDeferred,
		Reason:      "hosted Connect API",
	}}}
	profile := ComputeSupportProfile(ledger.Rows, policy, nil)

	var out bytes.Buffer
	if err := WriteSupportProfileHTML(&out, profile, ledger); err != nil {
		t.Fatalf("WriteSupportProfileHTML: %v", err)
	}
	var page struct {
		InventoryFailureCounts map[string]int `json:"inventoryFailureCounts"`
		InventoryFailureCount  int            `json:"inventoryFailureCount"`
		Gate                   struct {
			Status                string `json:"status"`
			Passed                bool   `json:"passed"`
			BlockingRowCount      int    `json:"blockingRowCount"`
			InventoryFailureCount int    `json:"inventoryFailureCount"`
		} `json:"gate"`
		Rows []struct {
			GapClass       string   `json:"gapClass"`
			LedgerGapClass string   `json:"ledgerGapClass"`
			Bucket         string   `json:"bucket"`
			Open           bool     `json:"open"`
			Blocking       bool     `json:"blocking"`
			GapState       string   `json:"gapState"`
			DeliveryStates []string `json:"deliveryStates"`
			NextActionKey  string   `json:"nextActionKey"`
			NextAction     string   `json:"nextAction"`
		} `json:"rows"`
	}
	if err := json.Unmarshal([]byte(extractSupportProfileHTMLData(t, out.String())), &page); err != nil {
		t.Fatalf("decode page: %v", err)
	}
	if page.InventoryFailureCount != 1 || page.InventoryFailureCounts[GapDocsOrgMismatch] != 1 {
		t.Fatalf("inventory failure totals = %d/%#v, want 1/%#v", page.InventoryFailureCount, page.InventoryFailureCounts, map[string]int{GapDocsOrgMismatch: 1})
	}
	if page.Gate.Status != htmlGateBlocked || page.Gate.Passed || page.Gate.BlockingRowCount != 1 || page.Gate.InventoryFailureCount != 1 {
		t.Fatalf("gate = %#v, want blocked with one inventory failure", page.Gate)
	}
	if len(page.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(page.Rows))
	}
	row := page.Rows[0]
	if row.GapClass != "" || row.LedgerGapClass != GapDocsOrgMismatch || row.Bucket != BucketFailure {
		t.Fatalf("profile/ledger gaps = %q/%q bucket=%q", row.GapClass, row.LedgerGapClass, row.Bucket)
	}
	if !row.Open || !row.Blocking || row.GapState != htmlGapStateBlocked || row.NextActionKey != "inventory-failure" || !strings.Contains(row.NextAction, "reconcile") {
		t.Fatalf("inventory failure row state = %#v", row)
	}
	if containsString(row.DeliveryStates, htmlDeliveryCovered) ||
		!containsString(row.DeliveryStates, htmlDeliveryHostedDeferred) ||
		!containsString(row.DeliveryStates, htmlDeliveryNotLocallyImplemented) ||
		!containsString(row.DeliveryStates, htmlDeliveryOpen) {
		t.Fatalf("hosted-deferred inventory failure delivery states = %#v", row.DeliveryStates)
	}
}

func TestWriteSupportProfileHTMLInventorySummaryFailureBlocksWithoutApexRow(t *testing.T) {
	ledger, policy, corpus := supportProfileHTMLFixture()
	ledger.Rows = ledger.Rows[:1]
	ledger.Summary = LedgerSummary{Failures: map[string]int{"parser": 2}, Total: 1}
	profile := ComputeSupportProfile(ledger.Rows, policy, &corpus)

	var out bytes.Buffer
	if err := WriteSupportProfileHTML(&out, profile, ledger); err != nil {
		t.Fatalf("WriteSupportProfileHTML: %v", err)
	}
	var page struct {
		InventoryFailureCounts map[string]int `json:"inventoryFailureCounts"`
		InventoryFailureCount  int            `json:"inventoryFailureCount"`
		Gate                   struct {
			Status                string `json:"status"`
			Passed                bool   `json:"passed"`
			BlockingRowCount      int    `json:"blockingRowCount"`
			InventoryFailureCount int    `json:"inventoryFailureCount"`
		} `json:"gate"`
	}
	if err := json.Unmarshal([]byte(extractSupportProfileHTMLData(t, out.String())), &page); err != nil {
		t.Fatalf("decode page: %v", err)
	}
	if page.InventoryFailureCount != 2 || page.InventoryFailureCounts["parser"] != 2 {
		t.Fatalf("parser inventory failure totals = %d/%#v, want 2/%#v", page.InventoryFailureCount, page.InventoryFailureCounts, map[string]int{"parser": 2})
	}
	if page.Gate.Status != htmlGateBlocked || page.Gate.Passed || page.Gate.BlockingRowCount != 0 || page.Gate.InventoryFailureCount != 2 {
		t.Fatalf("parser-only failure gate = %#v, want blocked without a blocking row", page.Gate)
	}
}

func TestSupportProfileHTMLDeliveryStatesDoNotClaimMissingImplementations(t *testing.T) {
	missingMock := SupportProfileRow{Disposition: DispositionDeterministicMockRequired, GapClass: GapMissingBehavior}
	if got := supportProfileHTMLDeliveryStates(missingMock, SurfaceLedgerRow{}); !reflect.DeepEqual(got, []string{htmlDeliveryOpen}) {
		t.Fatalf("missing mock delivery states = %#v", got)
	}

	deferred := SupportProfileRow{Disposition: DispositionHostedDeferred}
	wantDeferred := []string{htmlDeliveryHostedDeferred, htmlDeliveryNotLocallyImplemented}
	if got := supportProfileHTMLDeliveryStates(deferred, SurfaceLedgerRow{}); !reflect.DeepEqual(got, wantDeferred) {
		t.Fatalf("hosted-deferred delivery states = %#v, want %#v", got, wantDeferred)
	}

	unverifiedRuntime := SupportProfileRow{Disposition: DispositionLocalRuntimeRequired, GapClass: GapMissingEvidence}
	wantRuntime := []string{htmlDeliveryLocalRuntime, htmlDeliveryOpen}
	if got := supportProfileHTMLDeliveryStates(unverifiedRuntime, SurfaceLedgerRow{}); !reflect.DeepEqual(got, wantRuntime) {
		t.Fatalf("unverified runtime delivery states = %#v, want %#v", got, wantRuntime)
	}
}

func TestWriteSupportProfileHTMLEscapesEmbeddedJSON(t *testing.T) {
	ledger, policy, corpus := supportProfileHTMLFixture()
	profile := ComputeSupportProfile(ledger.Rows, policy, &corpus)

	var out bytes.Buffer
	if err := WriteSupportProfileHTML(&out, profile, ledger); err != nil {
		t.Fatalf("WriteSupportProfileHTML: %v", err)
	}
	payloadJSON := extractSupportProfileHTMLData(t, out.String())
	if strings.Contains(payloadJSON, "</script>") || strings.Contains(payloadJSON, "<script>") {
		t.Fatalf("dangerous raw script text found in embedded data: %q", payloadJSON)
	}
	if !strings.Contains(payloadJSON, "\\u003c/script\\u003e") {
		t.Fatalf("embedded JSON did not HTML-escape the dangerous surface ID: %q", payloadJSON)
	}
}

func extractSupportProfileHTMLData(t *testing.T, html string) string {
	t.Helper()
	const open = "<script id=\"page-data\" type=\"application/json\">"
	start := strings.Index(html, open)
	if start < 0 {
		t.Fatalf("missing page-data script")
	}
	start += len(open)
	end := strings.Index(html[start:], "</script>")
	if end < 0 {
		t.Fatalf("page-data script has no closing tag")
	}
	return html[start : start+end]
}

func supportProfileHTMLFixture() (SurfaceLedger, SupportPolicy, CorpusUsage) {
	ledger := SurfaceLedger{
		SchemaVersion: SchemaVersion,
		Rows: []SurfaceLedgerRow{
			{
				SurfaceID:               "apex:System.Widget.run(String)",
				Product:                 ProductApex,
				Area:                    AreaRuntime,
				Namespace:               "System",
				TypeName:                "Widget",
				MemberName:              "run",
				Kind:                    KindMethod,
				Signature:               "run(String)",
				ReturnType:              "String",
				Parameters:              []string{"String"},
				DocsReturnType:          "String",
				OrgReturnType:           "String",
				GladeReturnType:         "String",
				DocsParameters:          []string{"String"},
				OrgParameters:           []string{"String"},
				GladeParameters:         []string{"String"},
				Docs:                    SourcePresent,
				Org:                     SourcePresent,
				GladeShape:              ShapeSignatureKnown,
				GladeBehavior:           BehaviorSupported,
				Evidence:                EvidenceFixtureAndOracle,
				DocsSource:              "docs/widget.md",
				Sources:                 []string{"docs:widget", "fixture:widget-run"},
				SalesforceSurfaceFamily: "apex-runtime",
				ShapeSource:             "shape-fixture",
				BehaviorSource:          "behavior-fixture",
				ImplementationDecision:  "runtime",
				Notes:                   "safe sample note",
			},
			{
				SurfaceID:               "apex:Messaging.Email.send()",
				Product:                 ProductApex,
				Area:                    AreaRuntime,
				Namespace:               "Messaging",
				TypeName:                "Email",
				MemberName:              "send",
				Kind:                    KindMethod,
				Signature:               "send()",
				Docs:                    SourcePresent,
				Org:                     SourceAbsent,
				GladeShape:              ShapeSignatureKnown,
				GladeBehavior:           BehaviorStubNoOp,
				Evidence:                EvidenceFixture,
				Sources:                 []string{"docs:email", "fixture:email-mock"},
				SalesforceSurfaceFamily: "apex-runtime",
				ShapeSource:             "shape-fixture",
				BehaviorSource:          "mock-fixture",
				ImplementationDecision:  "deterministic mock",
			},
			{
				SurfaceID:               "apex:Reports.ReportInfo",
				Product:                 ProductApex,
				Area:                    AreaRuntime,
				Namespace:               "Reports",
				TypeName:                "ReportInfo",
				Kind:                    KindType,
				Docs:                    SourcePresent,
				Org:                     SourcePresent,
				GladeShape:              ShapeTypeKnown,
				GladeBehavior:           BehaviorPassive,
				Evidence:                EvidenceDocs,
				Sources:                 []string{"docs:reports"},
				SalesforceSurfaceFamily: "apex-runtime",
			},
			{
				SurfaceID:               "apex:Cache.CacheService",
				Product:                 ProductApex,
				Area:                    AreaRuntime,
				Namespace:               "Cache",
				TypeName:                "CacheService",
				Kind:                    KindType,
				Docs:                    SourcePresent,
				Org:                     SourceAbsent,
				GladeShape:              ShapeAbsent,
				GladeBehavior:           BehaviorNone,
				Evidence:                EvidenceNone,
				Sources:                 []string{"docs:cache"},
				SalesforceSurfaceFamily: "apex-runtime",
			},
			{
				SurfaceID:               "apex:ConnectApi.Remote</script><script>alert(1)</script>",
				Product:                 ProductApex,
				Area:                    AreaRuntime,
				Namespace:               "ConnectApi",
				TypeName:                "Remote",
				Kind:                    KindType,
				Docs:                    SourcePresent,
				Org:                     SourceAbsent,
				GladeShape:              ShapeAbsent,
				GladeBehavior:           BehaviorUnsupported,
				Evidence:                EvidenceNone,
				Sources:                 []string{"docs:connect-api"},
				SalesforceSurfaceFamily: "apex-runtime",
			},
		},
	}
	policy := SupportPolicy{Rules: []SupportPolicyRule{
		{Namespace: "System", Disposition: DispositionLocalRuntimeRequired, Reason: "local widget runtime"},
		{Namespace: "Messaging", Disposition: DispositionDeterministicMockRequired, Reason: "declared email mock"},
		{Namespace: "Cache", Disposition: DispositionDeterministicMockRequired, Reason: "declared cache mock"},
		{Namespace: "Reports", Disposition: DispositionCompileShapeRequired, Reason: "compile shape only"},
		{Namespace: "ConnectApi", Disposition: DispositionHostedDeferred, Reason: "hosted connect-api"},
	}}
	corpus := CorpusUsage{Usage: []CorpusUsageEntry{
		{
			UsageKey:         "System.Widget.run",
			Namespace:        "System",
			TypeName:         "Widget",
			MemberName:       "run",
			PubProdRefs:      2,
			PubTestRefs:      3,
			PubFailRefs:      5,
			PrivProdRefs:     7,
			PrivTestRefs:     11,
			PubProdProjects:  1,
			PubTestProjects:  2,
			PubFailProjects:  3,
			PrivProdProjects: 4,
			PrivTestProjects: 5,
		},
	}}
	return ledger, policy, corpus
}
