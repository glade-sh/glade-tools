package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

var g3PrimaryExplicitNonparityIDs = []string{
	"apex:System.SoqlStubProvider.SoqlStubProvider()",
}

func TestG3PrimaryExplicitNonparityEvidenceIsExactAndHostedDeferred(t *testing.T) {
	root := filepath.Join("..", "..")
	fixturePath := filepath.Join(root, "docs", "fixtures", "g3-primary-explicit-nonparity-policy-evidence.json")
	fixture, err := compat.LoadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Name != "g3-primary-explicit-nonparity-policy-evidence" || fixture.Command.Kind != "policy-evidence" || fixture.Expected.Error != nil {
		t.Fatalf("classification-only fixture contract = %#v", fixture)
	}
	if len(fixture.Source) != 0 {
		t.Fatalf("classification-only fixture source = %#v, want none", fixture.Source)
	}

	evidence, err := BuildEvidenceSnapshot([]string{fixturePath})
	if err != nil {
		t.Fatal(err)
	}
	assertExactSurfaceSet(t, evidence, g3PrimaryExplicitNonparityIDs)
	for _, row := range evidence {
		if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorUnsupported {
			t.Fatalf("%s evidence/behavior = %s/%s, want fixture/unsupported", row.SurfaceID, row.Evidence, row.GladeBehavior)
		}
	}

	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	var raw struct {
		SalesforceEligible        *bool  `json:"salesforceEligible"`
		SalesforceExclusionClass  string `json:"salesforceExclusionClass"`
		SalesforceExclusionReason string `json:"salesforceExclusionReason"`
		Evidence                  []struct {
			SurfaceID string `json:"surfaceId"`
			Kind      string `json:"kind"`
		} `json:"evidence"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if raw.SalesforceEligible == nil || *raw.SalesforceEligible || raw.SalesforceExclusionClass != "policy-local-only" || raw.SalesforceExclusionReason == "" {
		t.Fatalf("salesforce eligibility metadata = %#v", raw)
	}
	if len(raw.Evidence) != len(g3PrimaryExplicitNonparityIDs) {
		t.Fatalf("raw evidence rows = %d, want %d", len(raw.Evidence), len(g3PrimaryExplicitNonparityIDs))
	}
	for _, item := range raw.Evidence {
		if item.Kind != "unsupported" {
			t.Fatalf("%s evidence kind = %q, want unsupported", item.SurfaceID, item.Kind)
		}
	}

	policy, err := LoadSupportPolicy(filepath.Join(root, "docs", "fixtures", "apex-local-support-policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	wantReasons := map[string]string{
		"apex:System.SoqlStubProvider.SoqlStubProvider()": "SoqlStubProvider construction is a Salesforce test-harness contract, not a local runtime parity claim",
	}
	seenOverrides := map[string]bool{}
	for _, rule := range policy.Rules {
		wantReason, target := wantReasons[rule.SurfacePrefix]
		if !target {
			continue
		}
		if !rule.Override || rule.Disposition != DispositionHostedDeferred || rule.Reason != wantReason {
			t.Fatalf("policy override %q = %#v", rule.SurfacePrefix, rule)
		}
		seenOverrides[rule.SurfacePrefix] = true
	}
	if len(seenOverrides) != len(wantReasons) {
		t.Fatalf("exact hosted-deferred overrides = %#v, want %#v", seenOverrides, wantReasons)
	}

	nearby := []SurfaceLedgerRow{
		{SurfaceID: "apex:System.Messaging.renderEmailTemplate(String,String,List<String>)", Product: ProductApex, Area: AreaRuntime, Kind: KindMethod},
		{SurfaceID: "apex:System.SoqlStubProvider.handleSoqlQuery(Schema.SObjectType,String,Map<String,Object>)", Product: ProductApex, Area: AreaRuntime, Kind: KindMethod},
	}
	for i := range nearby {
		fillFromApexID(&nearby[i])
	}
	profile := ComputeSupportProfile(append(evidence, nearby...), policy, nil)
	byID := make(map[string]SupportProfileRow, len(profile.Rows))
	for _, row := range profile.Rows {
		byID[row.SurfaceID] = row
	}
	for _, id := range g3PrimaryExplicitNonparityIDs {
		row := byID[id]
		if row.Disposition != DispositionHostedDeferred || row.GapClass != "" || row.MatchRule != "surfacePrefix="+id {
			t.Fatalf("%s profile = %#v, want exact hosted-deferred override", id, row)
		}
	}
	if row := byID[nearby[0].SurfaceID]; row.Disposition != DispositionDeterministicMockRequired {
		t.Fatalf("nearby Messaging row = %#v, want deterministic-mock-required", row)
	}
	if row := byID[nearby[1].SurfaceID]; row.Disposition != DispositionLocalRuntimeRequired {
		t.Fatalf("nearby SoqlStubProvider row = %#v, want local-runtime-required", row)
	}
}
