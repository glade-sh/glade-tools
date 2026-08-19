package surfaceledger

import (
	"path/filepath"
	"strings"
	"testing"
)

var g3ResidualHostedPolicyReasons = map[string]string{
	"apex:Database.DMLOptions.DuplicateRuleHeader":                    "DTO/property shape exists, but configured duplicate-rule effects depend on hosted org DML and are locally rejected.",
	"apex:Database.DMLOptions.LocalizeErrors":                         "DTO/property shape exists, but configured locale error effects depend on hosted org DML and are locally rejected.",
	"apex:Database.DMLOptions.assignmentRuleHeader":                   "DTO/property shape exists, but configured assignment-rule effects depend on hosted org DML and are locally rejected.",
	"apex:Database.DMLOptions.emailHeader":                            "DTO/property shape exists, but configured email effects depend on hosted org DML and are locally rejected.",
	"apex:Database.DMLOptions.localeOptions":                          "DTO/property shape exists, but configured locale effects depend on hosted org DML and are locally rejected.",
	"apex:System.DMLOptions.assignmentRuleHeader":                     "DTO/property shape exists, but configured assignment-rule effects depend on hosted org DML and are locally rejected.",
	"apex:System.DMLOptions.emailHeader":                              "DTO/property shape exists, but configured email effects depend on hosted org DML and are locally rejected.",
	"apex:System.DMLOptions.localeOptions":                            "DTO/property shape exists, but configured locale effects depend on hosted org DML and are locally rejected.",
	"apex:IsvPartners.AppAnalytics.logCustomInteraction(Object)":      "local telemetry call is a deliberate no-op and grants no Salesforce analytics parity.",
	"apex:IsvPartners.AppAnalytics.logCustomInteraction(Object,Id)":   "local telemetry call is a deliberate no-op and grants no Salesforce analytics parity.",
	"apex:IsvPartners.AppAnalytics.logCustomInteraction(Object,UUID)": "local telemetry call is a deliberate no-op and grants no Salesforce analytics parity.",
}

func TestG3ResidualHostedPolicyOverridesAreExactAndCloseExistingUnsupportedEvidence(t *testing.T) {
	root := filepath.Join("..", "..")
	policy, err := LoadSupportPolicy(filepath.Join(root, "docs", "fixtures", "apex-local-support-policy.json"))
	if err != nil {
		t.Fatal(err)
	}

	seenOverrides := make(map[string]bool, len(g3ResidualHostedPolicyReasons))
	for _, rule := range policy.Rules {
		wantReason, target := g3ResidualHostedPolicyReasons[rule.SurfaceID]
		if !target {
			continue
		}
		if seenOverrides[rule.SurfaceID] {
			t.Fatalf("duplicate exact policy override %q", rule.SurfaceID)
		}
		if rule.SurfacePrefix != "" || !rule.Override || rule.Disposition != DispositionHostedDeferred || rule.Reason != wantReason {
			t.Fatalf("policy override %q = %#v", rule.SurfaceID, rule)
		}
		seenOverrides[rule.SurfaceID] = true
	}
	if len(seenOverrides) != len(g3ResidualHostedPolicyReasons) {
		t.Fatalf("exact hosted-deferred overrides = %#v, want %#v", seenOverrides, g3ResidualHostedPolicyReasons)
	}

	fixturePaths := []string{
		filepath.Join(root, "docs", "fixtures", "core-runtime-dml-options-duplicate-rule-unsupported.json"),
		filepath.Join(root, "docs", "fixtures", "core-runtime-dml-options-localize-errors-unsupported.json"),
		filepath.Join(root, "docs", "fixtures", "core-runtime-dml-options-unsupported.json"),
		filepath.Join(root, "docs", "fixtures", "core-runtime-dml-options-email-unsupported.json"),
		filepath.Join(root, "docs", "fixtures", "core-runtime-dml-options-locale-unsupported.json"),
		filepath.Join(root, "docs", "fixtures", "commerce-industry-tail-local-evidence.json"),
	}
	evidence, err := BuildEvidenceSnapshot(fixturePaths)
	if err != nil {
		t.Fatal(err)
	}
	ledger := Merge(nil, nil, BuildGladeSnapshot(), evidence)
	byID := rowsByID(ledger.Rows)
	for id := range g3ResidualHostedPolicyReasons {
		row, ok := byID[id]
		if !ok {
			t.Fatalf("missing existing evidence row %s", id)
		}
		if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorUnsupported || row.Bucket != BucketExplicitUnsupported {
			t.Fatalf("%s evidence/behavior/bucket = %s/%s/%s, want existing explicit unsupported evidence", id, row.Evidence, row.GladeBehavior, row.Bucket)
		}
	}

	profile := ComputeSupportProfile(ledger.Rows, policy, nil)
	profileByID := make(map[string]SupportProfileRow, len(profile.Rows))
	for _, row := range profile.Rows {
		profileByID[row.SurfaceID] = row
	}
	for id := range g3ResidualHostedPolicyReasons {
		row := profileByID[id]
		if row.Disposition != DispositionHostedDeferred || row.GapClass != "" || row.MatchRule != "surfaceId="+id {
			t.Fatalf("%s profile = %#v, want exact hosted-deferred closure", id, row)
		}
	}
	for _, row := range profile.NonDeferredGaps {
		if _, target := g3ResidualHostedPolicyReasons[row.SurfaceID]; target {
			t.Fatalf("non-deferred gap remains for %s: %#v", row.SurfaceID, row)
		}
	}
	for _, id := range []string{
		"apex:Database.DMLOptions.DuplicateRuleHeader.allowSave",
		"apex:Database.DMLOptions.DuplicateRuleHeader.runAsCurrentUser",
	} {
		if row := profileByID[id]; row.Disposition != DispositionLocalRuntimeRequired || row.MatchRule != "namespace=Database" {
			t.Fatalf("child DTO-storage profile %s = %#v, want unchanged Database classification", id, row)
		}
	}
	selected := make(map[string]bool, len(g3ResidualHostedPolicyReasons))
	for _, row := range profile.Rows {
		if strings.HasPrefix(row.MatchRule, "surfaceId=") {
			if row.Disposition != DispositionHostedDeferred {
				continue
			}
			if _, target := g3ResidualHostedPolicyReasons[row.SurfaceID]; !target {
				t.Fatalf("unexpected G3 exact policy match: %#v", row)
			}
			selected[row.SurfaceID] = true
		}
	}
	for id := range g3ResidualHostedPolicyReasons {
		if !selected[id] {
			t.Fatalf("selected G3 exact policy row missing: %s", id)
		}
	}
	if len(selected) != len(g3ResidualHostedPolicyReasons) {
		t.Fatalf("selected G3 exact policy rows = %#v, want only hosted targets", selected)
	}

	for id, want := range map[string]SupportDisposition{
		"apex:Database.DMLOptions.optAllOrNone": DispositionLocalRuntimeRequired,
		"apex:System.DMLOptions.optAllOrNone":   DispositionLocalRuntimeRequired,
		"apex:IsvPartners.AppAnalytics":         DispositionDeterministicMockRequired,
	} {
		if row := profileByID[id]; row.Disposition != want {
			t.Fatalf("adjacent %s disposition = %s, want %s", id, row.Disposition, want)
		}
	}
}
