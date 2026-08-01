package surfaceledger

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"
	"testing"
)

func buildSeedPolicy() SupportPolicy {
	return SupportPolicy{
		Rules: []SupportPolicyRule{
			{
				Namespace:   "System",
				Disposition: DispositionLocalRuntimeRequired,
				Reason:      "language/compiler runtime",
			},
			{
				Namespace:   "Schema",
				Disposition: DispositionLocalRuntimeRequired,
				Reason:      "schema describe runtime",
			},
			{
				Namespace:   "Database",
				Disposition: DispositionLocalRuntimeRequired,
				Reason:      "DML runtime",
			},
			{
				Namespace:   "Messaging",
				Disposition: DispositionDeterministicMockRequired,
				Reason:      "messaging mock",
			},
			{
				Namespace:   "Cache",
				Disposition: DispositionDeterministicMockRequired,
				Reason:      "cache mock",
			},
			{
				Namespace:   "ConnectApi",
				Disposition: DispositionHostedDeferred,
				Reason:      "connect-api deferred",
				MemberExceptions: []SupportPolicyMemberException{
					{TypeName: "Organization", MemberName: "getSettings", Disposition: DispositionDeterministicMockRequired, Reason: "observed corpus usage"},
					{TypeName: "UserProfiles", MemberName: "setPhoto", Disposition: DispositionDeterministicMockRequired, Reason: "observed corpus usage"},
					{TypeName: "UserProfiles", MemberName: "deletePhoto", Disposition: DispositionDeterministicMockRequired, Reason: "observed corpus usage"},
					{TypeName: "Communities", MemberName: "getCommunity", Disposition: DispositionDeterministicMockRequired, Reason: "observed corpus usage"},
					{TypeName: "ChatterUsers", MemberName: "getFollowings", Disposition: DispositionDeterministicMockRequired, Reason: "observed corpus usage"},
				},
			},
			{
				Namespace:   "Reports",
				Disposition: DispositionCompileShapeRequired,
				Reason:      "compile shape pending runtime evidence",
			},
			{
				Namespace:   "Slack",
				Disposition: DispositionHostedDeferred,
				Reason:      "hosted deferred",
			},
			{
				TypeFamily:  "commerce*",
				Disposition: DispositionHostedDeferred,
				Reason:      "commerce hosted deferred",
			},
		},
	}
}

func apexRow(id, namespace, typeName string) SurfaceLedgerRow {
	return SurfaceLedgerRow{
		SurfaceID:     id,
		Product:       ProductApex,
		Area:          AreaRuntime,
		Kind:          KindType,
		Namespace:     namespace,
		TypeName:      typeName,
		GladeShape:    ShapeTypeKnown,
		GladeBehavior: BehaviorSupported,
		Evidence:      EvidenceFixture,
	}
}

func apexMemberRow(id, namespace, typeName, memberName string) SurfaceLedgerRow {
	return SurfaceLedgerRow{
		SurfaceID:     id,
		Product:       ProductApex,
		Area:          AreaRuntime,
		Kind:          KindMethod,
		Namespace:     namespace,
		TypeName:      typeName,
		MemberName:    memberName,
		GladeShape:    ShapeSignatureKnown,
		GladeBehavior: BehaviorSupported,
		Evidence:      EvidenceFixture,
	}
}

// 1. Test all four dispositions
func TestSupportProfileFourDispositions(t *testing.T) {
	policy := buildSeedPolicy()
	rows := []SurfaceLedgerRow{
		apexRow("apex:System.String", "System", "String"),
		apexRow("apex:Messaging.Email", "Messaging", "Email"),
		apexRow("apex:Reports.ReportManager", "Reports", "ReportManager"),
		apexRow("apex:Slack.Conversation", "Slack", "Conversation"),
	}

	profile := ComputeSupportProfile(rows, policy, nil)

	if profile.Total != 4 {
		t.Fatalf("total: want 4 got %d", profile.Total)
	}
	if profile.ByDisposition[DispositionLocalRuntimeRequired] != 1 {
		t.Fatalf("local-runtime-required: want 1 got %d", profile.ByDisposition[DispositionLocalRuntimeRequired])
	}
	if profile.ByDisposition[DispositionDeterministicMockRequired] != 1 {
		t.Fatalf("deterministic-mock-required: want 1 got %d", profile.ByDisposition[DispositionDeterministicMockRequired])
	}
	if profile.ByDisposition[DispositionCompileShapeRequired] != 1 {
		t.Fatalf("compile-shape-required: want 1 got %d", profile.ByDisposition[DispositionCompileShapeRequired])
	}
	if profile.ByDisposition[DispositionHostedDeferred] != 1 {
		t.Fatalf("hosted-deferred: want 1 got %d", profile.ByDisposition[DispositionHostedDeferred])
	}

	dispByID := map[string]SupportDisposition{}
	for _, r := range profile.Rows {
		dispByID[r.SurfaceID] = r.Disposition
	}
	if dispByID["apex:System.String"] != DispositionLocalRuntimeRequired {
		t.Fatalf("System.String: got %q", dispByID["apex:System.String"])
	}
	if dispByID["apex:Messaging.Email"] != DispositionDeterministicMockRequired {
		t.Fatalf("Messaging.Email: got %q", dispByID["apex:Messaging.Email"])
	}
	if dispByID["apex:Reports.ReportManager"] != DispositionCompileShapeRequired {
		t.Fatalf("Reports.ReportManager: got %q", dispByID["apex:Reports.ReportManager"])
	}
	if dispByID["apex:Slack.Conversation"] != DispositionHostedDeferred {
		t.Fatalf("Slack.Conversation: got %q", dispByID["apex:Slack.Conversation"])
	}
}

// 2. Test an unclassified Apex row
func TestSupportProfileUnclassifiedApexRow(t *testing.T) {
	policy := buildSeedPolicy()
	rows := []SurfaceLedgerRow{
		apexRow("apex:UnknownNS.SomeType", "UnknownNS", "SomeType"),
	}

	profile := ComputeSupportProfile(rows, policy, nil)

	if len(profile.UnclassifiedRows) != 1 {
		t.Fatalf("unclassified rows: want 1 got %d", len(profile.UnclassifiedRows))
	}
	if profile.UnclassifiedRows[0].SurfaceID != "apex:UnknownNS.SomeType" {
		t.Fatalf("unclassified row: got %q", profile.UnclassifiedRows[0].SurfaceID)
	}
}

// 3. Test overlapping policy rules (first match wins)
func TestSupportProfileOverlappingPolicyRules(t *testing.T) {
	policy := SupportPolicy{
		Rules: []SupportPolicyRule{
			{
				Namespace:   "System",
				Disposition: DispositionLocalRuntimeRequired,
				Reason:      "system runtime",
			},
			{
				TypeFamily:  "system-stdlib",
				Disposition: DispositionCompileShapeRequired,
				Reason:      "second rule should not apply",
			},
		},
	}
	rows := []SurfaceLedgerRow{
		func() SurfaceLedgerRow {
			r := apexRow("apex:System.String", "System", "String")
			r.SalesforceSurfaceFamily = "system-stdlib"
			return r
		}(),
	}

	profile := ComputeSupportProfile(rows, policy, nil)

	if len(profile.Rows) != 1 {
		t.Fatalf("rows: want 1 got %d", len(profile.Rows))
	}
	// First matching rule should win — System namespace matches first rule.
	if profile.Rows[0].Disposition != DispositionLocalRuntimeRequired {
		t.Fatalf("disposition: want %s got %s (first rule should win)", DispositionLocalRuntimeRequired, profile.Rows[0].Disposition)
	}
	if profile.Rows[0].MatchRule != "namespace=System" {
		t.Fatalf("match rule: want namespace=System got %q", profile.Rows[0].MatchRule)
	}
}

// 4. Test stale member exception matches no ledger row
func TestSupportProfileStaleMemberException(t *testing.T) {
	policy := SupportPolicy{
		Rules: []SupportPolicyRule{
			{
				Namespace:   "ConnectApi",
				Disposition: DispositionHostedDeferred,
				Reason:      "connect-api deferred",
				MemberExceptions: []SupportPolicyMemberException{
					{TypeName: "NonexistentType", MemberName: "noSuchMethod", Disposition: DispositionDeterministicMockRequired, Reason: "stale"},
				},
			},
		},
	}
	rows := []SurfaceLedgerRow{
		apexRow("apex:ConnectApi.SomeDTO", "ConnectApi", "SomeDTO"),
	}

	// Should still classify the row as hosted-deferred, and report the stale exception.
	profile := ComputeSupportProfile(rows, policy, nil)

	if len(profile.Rows) != 1 {
		t.Fatalf("rows: want 1 got %d", len(profile.Rows))
	}
	if profile.Rows[0].Disposition != DispositionHostedDeferred {
		t.Fatalf("disposition: want hosted-deferred got %s", profile.Rows[0].Disposition)
	}
	if len(profile.ValidationErrors) == 0 {
		t.Fatalf("expected stale member exception validation error")
	}
	foundStale := false
	for _, err := range profile.ValidationErrors {
		if strings.Contains(err, "stale member exception") && strings.Contains(err, "NonexistentType.noSuchMethod") {
			foundStale = true
			break
		}
	}
	if !foundStale {
		t.Fatalf("expected stale member exception in validation errors, got: %v", profile.ValidationErrors)
	}
}

// 5. Test product rows outside Apex remain visible but excluded from Apex classification
func TestSupportProfileNonApexRowsExcluded(t *testing.T) {
	policy := buildSeedPolicy()
	rows := []SurfaceLedgerRow{
		apexRow("apex:System.String", "System", "String"),
		{
			SurfaceID: "rest:/services/data/vXX.X/sobjects",
			Product:   ProductREST,
			Area:      AreaServer,
			Kind:      KindResource,
		},
		{
			SurfaceID: "lwc:lightning-button",
			Product:   ProductLWC,
			Area:      AreaUI,
			Kind:      KindModule,
		},
	}

	profile := ComputeSupportProfile(rows, policy, nil)

	// Only the Apex row should be classified.
	if profile.Total != 1 {
		t.Fatalf("total: want 1 (apex only) got %d", profile.Total)
	}
	if len(profile.Rows) != 1 {
		t.Fatalf("rows: want 1 got %d", len(profile.Rows))
	}
	if profile.Rows[0].SurfaceID != "apex:System.String" {
		t.Fatalf("expected only Apex row, got %q", profile.Rows[0].SurfaceID)
	}
}

// 6. Test deterministic JSON ordering
func TestSupportProfileDeterministicJSONOrdering(t *testing.T) {
	policy := buildSeedPolicy()
	rows := []SurfaceLedgerRow{
		apexRow("apex:System.String", "System", "String"),
		apexRow("apex:Schema.SObjectType", "Schema", "SObjectType"),
		apexRow("apex:Messaging.Email", "Messaging", "Email"),
	}

	// Run twice and verify identical JSON output.
	var buf1, buf2 bytes.Buffer
	profile1 := ComputeSupportProfile(rows, policy, nil)
	if err := WriteSupportProfileJSON(&buf1, profile1); err != nil {
		t.Fatalf("first write: %v", err)
	}
	profile2 := ComputeSupportProfile(rows, policy, nil)
	if err := WriteSupportProfileJSON(&buf2, profile2); err != nil {
		t.Fatalf("second write: %v", err)
	}

	if buf1.String() != buf2.String() {
		t.Fatalf("JSON output not deterministic\nfirst:\n%s\nsecond:\n%s", buf1.String(), buf2.String())
	}

	// Verify rows are sorted by SurfaceID.
	var decoded struct {
		Rows []struct {
			SurfaceID string `json:"surfaceId"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(buf1.Bytes(), &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(decoded.Rows) != 3 {
		t.Fatalf("decoded rows: want 3 got %d", len(decoded.Rows))
	}
	ids := make([]string, len(decoded.Rows))
	for i, r := range decoded.Rows {
		ids[i] = r.SurfaceID
	}
	if !sort.StringsAreSorted(ids) {
		t.Fatalf("rows not sorted by surfaceId: %v", ids)
	}
}

// 7. Test JSON and Markdown profile output
func TestSupportProfileJSONAndMarkdownOutput(t *testing.T) {
	policy := buildSeedPolicy()
	rows := []SurfaceLedgerRow{
		apexRow("apex:System.String", "System", "String"),
		apexRow("apex:Reports.ReportManager", "Reports", "ReportManager"),
	}

	profile := ComputeSupportProfile(rows, policy, nil)

	// Test JSON output.
	var jsonBuf bytes.Buffer
	if err := WriteSupportProfileJSON(&jsonBuf, profile); err != nil {
		t.Fatalf("write JSON: %v", err)
	}
	out := jsonBuf.String()
	if !strings.HasSuffix(out, "\n") {
		t.Fatalf("JSON must end with newline, got %q", out)
	}
	if !strings.Contains(out, "  ") {
		t.Fatalf("JSON must be indented, got %q", out)
	}

	var decoded SupportProfile
	if err := json.Unmarshal(jsonBuf.Bytes(), &decoded); err != nil {
		t.Fatalf("decode JSON: %v\n%s", err, out)
	}
	if decoded.Total != 2 {
		t.Fatalf("decoded total: want 2 got %d", decoded.Total)
	}
	if len(decoded.Rows) != 2 {
		t.Fatalf("decoded rows: want 2 got %d", len(decoded.Rows))
	}

	// Test Markdown output.
	var mdBuf bytes.Buffer
	if err := WriteSupportProfileMarkdown(&mdBuf, profile); err != nil {
		t.Fatalf("write Markdown: %v", err)
	}
	md := mdBuf.String()
	for _, want := range []string{
		"Support Profile",
		"local-runtime-required",
		"compile-shape-required",
	} {
		if !strings.Contains(md, want) {
			t.Fatalf("Markdown missing %q:\n%s", want, md)
		}
	}
}

// 8. Test CLI failure on any unclassified, overlapping, or stale rule
func TestSupportProfileValidationRejectsUnclassified(t *testing.T) {
	policy := buildSeedPolicy()
	rows := []SurfaceLedgerRow{
		apexRow("apex:UnknownNS.SomeType", "UnknownNS", "SomeType"),
	}

	profile := ComputeSupportProfile(rows, policy, nil)

	if len(profile.UnclassifiedRows) == 0 {
		t.Fatalf("expected unclassified rows")
	}
	if len(profile.ValidationErrors) == 0 {
		t.Fatalf("expected validation error for unclassified row")
	}
	foundUnclassified := false
	for _, err := range profile.ValidationErrors {
		if strings.Contains(err, "unclassified Apex row: apex:UnknownNS.SomeType") {
			foundUnclassified = true
			break
		}
	}
	if !foundUnclassified {
		t.Fatalf("expected unclassified validation error, got: %v", profile.ValidationErrors)
	}
}

func TestSupportProfileValidationRejectsOverlappingRules(t *testing.T) {
	policy := SupportPolicy{
		Rules: []SupportPolicyRule{
			{
				Namespace:   "System",
				Disposition: DispositionLocalRuntimeRequired,
				Reason:      "system runtime",
			},
			{
				Namespace:   "System",
				Disposition: DispositionHostedDeferred,
				Reason:      "overlapping rule",
			},
		},
	}
	// Policy-level validation should detect overlapping rules.
	// For now, the test asserts the profile's validation errors.
	rows := []SurfaceLedgerRow{
		apexRow("apex:System.String", "System", "String"),
	}

	profile := ComputeSupportProfile(rows, policy, nil)

	// The overlapping detection should be flagged.
	foundOverlap := false
	for _, err := range profile.ValidationErrors {
		if strings.Contains(err, "overlapping namespace rule: System") {
			foundOverlap = true
			break
		}
	}
	if !foundOverlap {
		t.Fatalf("expected overlapping namespace rule validation error, got: %v", profile.ValidationErrors)
	}
}

// Test member exception handling within ConnectApi namespace.
func TestSupportProfileConnectApiMemberExceptions(t *testing.T) {
	policy := buildSeedPolicy()
	rows := []SurfaceLedgerRow{
		apexMemberRow("apex:ConnectApi.Organization.getSettings", "ConnectApi", "Organization", "getSettings"),
		apexMemberRow("apex:ConnectApi.UserProfiles.setPhoto", "ConnectApi", "UserProfiles", "setPhoto"),
		apexRow("apex:ConnectApi.SomeDTO", "ConnectApi", "SomeDTO"),
	}

	profile := ComputeSupportProfile(rows, policy, nil)

	// Organization.getSettings should be deterministic-mock-required.
	// UserProfiles.setPhoto should be deterministic-mock-required.
	// SomeDTO should be hosted-deferred.
	dispByID := map[string]SupportDisposition{}
	for _, r := range profile.Rows {
		dispByID[r.SurfaceID] = r.Disposition
	}
	if dispByID["apex:ConnectApi.Organization.getSettings"] != DispositionDeterministicMockRequired {
		t.Fatalf("Organization.getSettings: want deterministic-mock-required got %q", dispByID["apex:ConnectApi.Organization.getSettings"])
	}
	if dispByID["apex:ConnectApi.UserProfiles.setPhoto"] != DispositionDeterministicMockRequired {
		t.Fatalf("UserProfiles.setPhoto: want deterministic-mock-required got %q", dispByID["apex:ConnectApi.UserProfiles.setPhoto"])
	}
	if dispByID["apex:ConnectApi.SomeDTO"] != DispositionHostedDeferred {
		t.Fatalf("SomeDTO: want hosted-deferred got %q", dispByID["apex:ConnectApi.SomeDTO"])
	}
}

// Test type-family matching.
func TestSupportProfileTypeFamilyMatch(t *testing.T) {
	policy := SupportPolicy{
		Rules: []SupportPolicyRule{
			{
				TypeFamily:  "commerce*",
				Disposition: DispositionHostedDeferred,
				Reason:      "commerce deferred",
			},
		},
	}
	rows := []SurfaceLedgerRow{
		func() SurfaceLedgerRow {
			r := apexRow("apex:commercepayments.Payment", "commercepayments", "Payment")
			r.SalesforceSurfaceFamily = "commercepayments"
			return r
		}(),
	}

	profile := ComputeSupportProfile(rows, policy, nil)

	if len(profile.Rows) != 1 {
		t.Fatalf("rows: want 1 got %d", len(profile.Rows))
	}
	if profile.Rows[0].Disposition != DispositionHostedDeferred {
		t.Fatalf("disposition: want hosted-deferred got %s", profile.Rows[0].Disposition)
	}
}

// 8. support-profile requires and joins the corpus-usage input
func TestSupportProfileJoinsCorpusUsage(t *testing.T) {
	policy := buildSeedPolicy()
	rows := []SurfaceLedgerRow{
		apexMemberRow("apex:ConnectApi.ChatterUsers.getFollowings", "ConnectApi", "ChatterUsers", "getFollowings"),
		apexRow("apex:ConnectApi.SomeDTO", "ConnectApi", "SomeDTO"),
		apexRow("apex:System.String", "System", "String"),
	}

	cu := CorpusUsage{
		Usage: []CorpusUsageEntry{
			{
				UsageKey:  "ConnectApi.ChatterUsers.getFollowings",
				Namespace: "ConnectApi",
				TypeName:  "ChatterUsers",
				MemberName: "getFollowings",
				PubProdRefs: 5,
				PubProdFiles: 3,
				PubProdProjects: 2,
			},
			{
				UsageKey:  "ConnectApi.SomeDTO",
				Namespace: "ConnectApi",
				TypeName:  "SomeDTO",
				PubProdRefs: 1,
			},
			{
				UsageKey:  "System.String",
				Namespace: "System",
				TypeName:  "String",
				PubTestRefs: 10,
			},
		},
	}

	profile := ComputeSupportProfile(rows, policy, &cu)

	// Every row must have a UsageKey.
	for _, row := range profile.Rows {
		if row.UsageKey == "" {
			t.Fatalf("row %s has empty UsageKey", row.SurfaceID)
		}
	}

	// Profile must include the corpus usage.
	if len(profile.CorpusUsage) != 3 {
		t.Fatalf("corpus usage entries: want 3 got %d", len(profile.CorpusUsage))
	}

	// Verify specific keys.
	keys := map[string]CorpusUsageEntry{}
	for _, e := range profile.CorpusUsage {
		keys[e.UsageKey] = e
	}
	if e, ok := keys["ConnectApi.ChatterUsers.getFollowings"]; !ok {
		t.Fatalf("missing ConnectApi.ChatterUsers.getFollowings in corpusUsage")
	} else if e.PubProdRefs != 5 {
		t.Fatalf("ChatterUsers.getFollowings PubProdRefs: want 5 got %d", e.PubProdRefs)
	}

	// Verify row keys match.
	byID := map[string]string{}
	for _, row := range profile.Rows {
		byID[row.SurfaceID] = row.UsageKey
	}
	if byID["apex:ConnectApi.ChatterUsers.getFollowings"] != "ConnectApi.ChatterUsers.getFollowings" {
		t.Fatalf("wrong usage key for getFollowings: %q", byID["apex:ConnectApi.ChatterUsers.getFollowings"])
	}
	if byID["apex:ConnectApi.SomeDTO"] != "ConnectApi.SomeDTO" {
		t.Fatalf("wrong usage key for SomeDTO: %q", byID["apex:ConnectApi.SomeDTO"])
	}
	if byID["apex:System.String"] != "System.String" {
		t.Fatalf("wrong usage key for System.String: %q", byID["apex:System.String"])
	}

	// Compute without corpus usage — UsageKey must be empty but profile works.
	profileNoCU := ComputeSupportProfile(rows, policy, nil)
	for _, row := range profileNoCU.Rows {
		if row.UsageKey != "" {
			t.Fatalf("row %s should have empty UsageKey without corpus input", row.SurfaceID)
		}
	}
	if len(profileNoCU.CorpusUsage) != 0 {
		t.Fatalf("corpusUsage should be empty without corpus input")
	}
}
