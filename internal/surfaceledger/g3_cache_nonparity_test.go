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

func TestG3CacheRowsLeaveCurrentLedgerAsReviewedAPIRemovals(t *testing.T) {
	root := filepath.Join("..", "..")
	policy, err := LoadSupportPolicy(filepath.Join(root, "docs", "fixtures", "apex-local-support-policy.json"))
	if err != nil {
		t.Fatal(err)
	}

	for _, rule := range policy.Rules {
		if g3CacheContains(g3CacheHostedIDs, rule.SurfaceID) {
			t.Fatalf("removed Cache surface retains a current support rule: %s", rule.SurfaceID)
		}
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
	for _, id := range g3CacheHostedIDs {
		for _, row := range evidence {
			if surfaceIDKey(row.SurfaceID) == surfaceIDKey(id) {
				t.Fatalf("removed Cache row entered current evidence: %s", id)
			}
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
