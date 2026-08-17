package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

var g3CacheHostedIDs = []string{
	"apex:Cache.Org.getAvgValueSize()",
	"apex:Cache.Org.getMaxValueSize()",
	"apex:Cache.Org.isAvailable()",
	"apex:Cache.OrgPartition.getAvgValueSize()",
	"apex:Cache.OrgPartition.getMaxValueSize()",
	"apex:Cache.Partition.getAvgValueSize()",
	"apex:Cache.Partition.getMaxValueSize()",
	"apex:Cache.Session.getAvgValueSize()",
	"apex:Cache.Session.getMaxValueSize()",
	"apex:Cache.SessionPartition.getAvgValueSize()",
	"apex:Cache.SessionPartition.getMaxValueSize()",
}

const g3CachePolicyReason = "org-backed Cache capacity and availability behavior is not modeled locally; retain the Salesforce shape with zero parity credit"

func TestG3CacheRowsRemainRetainedExplicitNonparity(t *testing.T) {
	root := filepath.Join("..", "..")
	policy, err := LoadSupportPolicy(filepath.Join(root, "docs", "fixtures", "apex-local-support-policy.json"))
	if err != nil {
		t.Fatal(err)
	}

	seen := map[string]bool{}
	for _, rule := range policy.Rules {
		if !g3CacheContains(g3CacheHostedIDs, rule.SurfaceID) {
			continue
		}
		if seen[rule.SurfaceID] {
			t.Fatalf("duplicate Cache exact policy rule %q", rule.SurfaceID)
		}
		if !rule.Override || rule.Disposition != DispositionHostedDeferred || rule.Reason != g3CachePolicyReason {
			t.Fatalf("Cache policy rule %q = %#v", rule.SurfaceID, rule)
		}
		seen[rule.SurfaceID] = true
	}
	if len(seen) != len(g3CacheHostedIDs) {
		t.Fatalf("Cache exact hosted rules = %d, want %d", len(seen), len(g3CacheHostedIDs))
	}

	fixturePath := filepath.Join(root, "docs", "fixtures", "current-base-cache-negative-api67.json")
	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	var raw struct {
		SalesforceEligible        *bool  `json:"salesforceEligible"`
		SalesforceExclusionClass  string `json:"salesforceExclusionClass"`
		SalesforceExclusionReason string `json:"salesforceExclusionReason"`
		Expected                  struct {
			Error struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error"`
		} `json:"expected"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	wantFixtureReason := "This fixture does not establish Salesforce availability or removal for these Cache members; Glade does not model the Cache statistics, availability, and key-validation behavior exercised here; it proves only the local rejection contract and grants zero Salesforce parity credit."
	if raw.SalesforceEligible == nil || *raw.SalesforceEligible || raw.SalesforceExclusionClass != "policy-local-only" || raw.SalesforceExclusionReason != wantFixtureReason {
		t.Fatalf("Cache Salesforce proof boundary = %#v", raw)
	}
	if raw.Expected.Error.Type != "UnsupportedOperationException" || raw.Expected.Error.Message != "local stub surface" {
		t.Fatalf("Cache local rejection = %#v", raw.Expected.Error)
	}

	evidence, err := BuildEvidenceSnapshot([]string{fixturePath})
	if err != nil {
		t.Fatal(err)
	}
	profile := ComputeSupportProfile(evidence, policy, nil)
	byID := make(map[string]SupportProfileRow, len(profile.Rows))
	for _, row := range profile.Rows {
		byID[row.SurfaceID] = row
	}
	for _, id := range g3CacheHostedIDs {
		row, ok := byID[id]
		if !ok {
			t.Fatalf("retained Cache row disappeared: %s", id)
		}
		if row.Disposition != DispositionHostedDeferred || row.MatchRule != "surfaceId="+id {
			t.Fatalf("Cache profile %s = %#v", id, row)
		}
	}
}

func g3CacheContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
