package surfaceledger

import (
	"path/filepath"
	"testing"
)

var g3bCompileNegativeSurfaceIDs = []string{
	"apex:ApexPages.KnowledgeArticleVersionStandardController.setDataCategory()",
	"apex:Schema.DescribeSObjectResult.getFieldSets()",
	"apex:Schema.DescribeSObjectResult.getFields()",
	"apex:System.Crypto.areEqualConstantTime(Blob,Blob)",
	"apex:System.String.escapeXml10",
	"apex:System.String.escapeXml11",
	"apex:System.String.unescapeXml10",
	"apex:System.String.unescapeXml11",
	"apex:System.TouchHandledException.TouchHandledException()",
	"apex:System.TouchHandledException.TouchHandledException(String,Exception)",
	"apex:workflow.Action.invoke(workflow.Context)",
	"apex:workflow.ActionDml.invoke()",
}

func TestG3BCompileNegativePolicyOverridesAreExact(t *testing.T) {
	targets := make(map[string]bool, len(g3bCompileNegativeSurfaceIDs))
	for _, id := range g3bCompileNegativeSurfaceIDs {
		targets[id] = false
	}
	policy, err := LoadSupportPolicy(filepath.Join("..", "..", "docs", "fixtures", "apex-local-support-policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, rule := range policy.Rules {
		if _, ok := targets[rule.SurfaceID]; !ok {
			continue
		}
		if targets[rule.SurfaceID] {
			t.Fatalf("duplicate exact compile-negative rule %s", rule.SurfaceID)
		}
		if rule.SurfacePrefix != "" || !rule.Override || rule.Disposition != DispositionCompileShapeRequired || rule.Reason == "" {
			t.Fatalf("compile-negative rule %s = %#v", rule.SurfaceID, rule)
		}
		targets[rule.SurfaceID] = true
	}
	for id, seen := range targets {
		if !seen {
			t.Errorf("missing exact compile-negative rule %s", id)
		}
	}
}

func TestG3BCompileNegativeFixtureRowsCloseInSupportProfile(t *testing.T) {
	root := filepath.Join("..", "..")
	fixtureRoot := filepath.Join(root, "docs", "fixtures")
	policy, err := LoadSupportPolicy(filepath.Join(fixtureRoot, "apex-local-support-policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := BuildEvidenceSnapshot([]string{
		filepath.Join(fixtureRoot, "apex-tail-supported-shape-evidence.json"),
		filepath.Join(fixtureRoot, "current-base-apexpages-set-data-category-zero-negative-api67.json"),
		filepath.Join(fixtureRoot, "current-base-string-entity-negative-api67.json"),
		filepath.Join(fixtureRoot, "data-platform-schema-describe-edges.json"),
		filepath.Join(fixtureRoot, "data-platform-schema-describe-fieldsets.json"),
		filepath.Join(fixtureRoot, "core-blob-crypto-unsupported-constant-time.json"),
		filepath.Join(fixtureRoot, "current-base-system-exception-tail-20260805-unsupported-constructors.json"),
		filepath.Join(fixtureRoot, "core-runtime-workflow-txnsecurity-local-defaults.json"),
	})
	if err != nil {
		t.Fatal(err)
	}
	profile := ComputeSupportProfile(Merge(nil, nil, BuildGladeSnapshot(), evidence).Rows, policy, nil)
	byID := make(map[string]SupportProfileRow, len(profile.Rows))
	for _, row := range profile.Rows {
		byID[row.SurfaceID] = row
	}
	for _, id := range g3bCompileNegativeSurfaceIDs {
		row, ok := byID[id]
		if !ok {
			t.Errorf("missing compile-negative profile row %s", id)
			continue
		}
		if row.Disposition != DispositionCompileShapeRequired || row.Evidence != EvidenceFixture || row.GapClass != "" || row.MatchRule != "surfaceId="+id {
			t.Errorf("compile-negative profile row %s = %#v", id, row)
		}
	}
}
