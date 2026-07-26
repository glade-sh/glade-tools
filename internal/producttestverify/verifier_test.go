package producttestverify

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/apexrules"
)

func TestVerifyResolvesExactTestsAndSharedSubcases(t *testing.T) {
	gladeRoot := t.TempDir()
	testPath := filepath.Join(gladeRoot, "internal", "sema", "contracts_test.go")
	if err := os.MkdirAll(filepath.Dir(testPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(testPath, []byte(`package sema

import "testing"

func TestExactRule(t *testing.T) {}

func TestSharedRules(t *testing.T) {
	for _, name := range []string{"reject duplicate local", "accept valid local"} {
		t.Run(name, func(t *testing.T) {})
	}
}
`), 0o600); err != nil {
		t.Fatal(err)
	}

	catalog := apexrules.Catalog{GladeCommit: strings.Repeat("a", 40), Rules: []apexrules.Rule{
		{ID: "APEX-EXACT", Status: apexrules.StatusSupported, ProductTest: "internal/sema/contracts_test.go:TestExactRule"},
		{ID: "APEX-SHARED-REJECT", Status: apexrules.StatusSupported, ProductTest: "internal/sema/contracts_test.go:TestSharedRules/reject duplicate local"},
		{ID: "APEX-SHARED-ACCEPT", Status: apexrules.StatusSupported, ProductTest: "internal/sema/contracts_test.go:TestSharedRules/accept valid local"},
	}}

	if findings := Verify(catalog, gladeRoot); len(findings) != 0 {
		t.Fatalf("Verify() findings = %v", findings)
	}
}

func TestVerifyRejectsMissingAndAmbiguousEvidence(t *testing.T) {
	gladeRoot := t.TempDir()
	testPath := filepath.Join(gladeRoot, "internal", "sema", "contracts_test.go")
	if err := os.MkdirAll(filepath.Dir(testPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(testPath, []byte(`package sema

import "testing"

func TestSharedRules(t *testing.T) {
	t.Run("known case", func(t *testing.T) {})
}
`), 0o600); err != nil {
		t.Fatal(err)
	}

	catalog := apexrules.Catalog{GladeCommit: strings.Repeat("a", 40), Rules: []apexrules.Rule{
		{ID: "APEX-AMBIGUOUS-ONE", Status: apexrules.StatusSupported, ProductTest: "internal/sema/contracts_test.go:TestSharedRules"},
		{ID: "APEX-AMBIGUOUS-TWO", Status: apexrules.StatusSupported, ProductTest: "internal/sema/contracts_test.go:TestSharedRules"},
		{ID: "APEX-MISSING-TEST", Status: apexrules.StatusSupported, ProductTest: "internal/sema/contracts_test.go:TestDoesNotExist"},
		{ID: "APEX-MISSING-SUBCASE", Status: apexrules.StatusSupported, ProductTest: "internal/sema/contracts_test.go:TestSharedRules/unknown case"},
		{ID: "APEX-MISSING-FILE", Status: apexrules.StatusSupported, ProductTest: "internal/sema/missing_test.go:TestMissing"},
		{ID: "APEX-DUPLICATE-ONE", Status: apexrules.StatusSupported, ProductTest: "internal/sema/contracts_test.go:TestSharedRules/known case"},
		{ID: "APEX-DUPLICATE-TWO", Status: apexrules.StatusSupported, ProductTest: "internal/sema/contracts_test.go:TestSharedRules/known case"},
	}}

	findings := Verify(catalog, gladeRoot)
	text := FormatFindings(findings)
	for _, want := range []string{
		"APEX-AMBIGUOUS-ONE",
		"shared product test requires an explicit /subcase",
		"APEX-MISSING-TEST",
		`test "TestDoesNotExist" was not found`,
		"APEX-MISSING-SUBCASE",
		`subcase "unknown case" was not found`,
		"APEX-MISSING-FILE",
		"product test file does not exist",
		"APEX-DUPLICATE-TWO",
		"duplicate product test requires productTestAliasOf",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("FormatFindings() = %q, want %q", text, want)
		}
	}
}

func TestVerifyAllowsDeclaredSemanticAlias(t *testing.T) {
	gladeRoot := t.TempDir()
	testPath := filepath.Join(gladeRoot, "internal", "sema", "contracts_test.go")
	if err := os.MkdirAll(filepath.Dir(testPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(testPath, []byte(`package sema

import "testing"

func TestRule(t *testing.T) {}
`), 0o600); err != nil {
		t.Fatal(err)
	}

	catalog := apexrules.Catalog{GladeCommit: strings.Repeat("a", 40), Rules: []apexrules.Rule{
		{ID: "APEX-CANONICAL", Status: apexrules.StatusSupported, ProductTest: "internal/sema/contracts_test.go:TestRule"},
		{ID: "APEX-ALIAS", Status: apexrules.StatusSupported, ProductTest: "internal/sema/contracts_test.go:TestRule", ProductTestAliasOf: "APEX-CANONICAL"},
	}}
	if findings := Verify(catalog, gladeRoot); len(findings) != 0 {
		t.Fatalf("Verify() findings = %v", findings)
	}
}

func TestVerifyRejectsUnsafeProductTestPaths(t *testing.T) {
	catalog := apexrules.Catalog{GladeCommit: strings.Repeat("a", 40), Rules: []apexrules.Rule{{
		ID:          "APEX-UNSAFE",
		Status:      apexrules.StatusSupported,
		ProductTest: "../outside_test.go:TestOutside",
	}}}
	findings := Verify(catalog, t.TempDir())
	if text := FormatFindings(findings); !strings.Contains(text, "unsafe product test path") {
		t.Fatalf("FormatFindings() = %q, want unsafe path finding", text)
	}
}

func TestVerifyRequiresPinnedGladeCommit(t *testing.T) {
	for name, commit := range map[string]string{
		"missing": "",
		"short":   "7cd5b603",
		"branch":  "feature/apex-language-rule-compatibility",
	} {
		t.Run(name, func(t *testing.T) {
			findings := Verify(apexrules.Catalog{GladeCommit: commit}, t.TempDir())
			if text := FormatFindings(findings); !strings.Contains(text, "catalog gladeCommit must be a full 40-character Git SHA") {
				t.Fatalf("FormatFindings() = %q, want pinned commit finding", text)
			}
		})
	}
}

func TestCheckedCatalogProductTestsResolveAgainstSiblingGlade(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := apexrules.LoadCatalog(filepath.Join(repoRoot, "docs", "fixtures", "apex-language-rules.json"))
	if err != nil {
		t.Fatal(err)
	}
	gladeRoot := filepath.Join(repoRoot, "..", "glade")
	findings := Verify(catalog, gladeRoot)
	if len(findings) != 0 {
		t.Fatalf("%d Apex product-test evidence findings:\n%s", len(findings), FormatFindings(findings))
	}
}

func TestFreshReviewCatalogProductTestsResolveAgainstSiblingGlade(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := apexrules.LoadCatalog(filepath.Join(repoRoot, "docs", "fixtures", "apex-language-rules.json"))
	if err != nil {
		t.Fatal(err)
	}
	wanted := map[string]bool{
		"APEX-REVIEW-HTTP-GET-POSITIONAL-ARGUMENT":        true,
		"APEX-REVIEW-HTTP-VERB-WITHOUT-REST-RESOURCE":     true,
		"APEX-REVIEW-LARGE-DECIMAL-LITERAL":               true,
		"APEX-REVIEW-RAW-LIST-MIXED-CASE":                 true,
		"APEX-REVIEW-REST-NONTERMINAL-SLASH-WILDCARD":     true,
		"APEX-REVIEW-SOQL-LOCAL-SHADOWS-FIELD":            true,
		"APEX-REVIEW-SOQL-PARAMETER-SHADOWS-FIELD":        true,
		"APEX-REVIEW-SUPPRESSWARNINGS-MULTIPLE-ARGUMENTS": true,
		"APEX-REVIEW-SUPPRESSWARNINGS-NONSTRING-ARGUMENT": true,
		"APEX-REVIEW-SWITCH-DATETIME-SELECTOR":            true,
		"APEX-REVIEW-SWITCH-MISMATCHED-BRANCH":            true,
		"APEX-REVIEW-WEBSERVICE-ANNOTATION-SYNTAX":        true,
	}
	reviewCatalog := apexrules.Catalog{GladeCommit: catalog.GladeCommit}
	for _, rule := range catalog.Rules {
		if wanted[rule.ID] {
			reviewCatalog.Rules = append(reviewCatalog.Rules, rule)
		}
	}
	if len(reviewCatalog.Rules) != len(wanted) {
		t.Fatalf("fresh review catalog row count = %d, want %d", len(reviewCatalog.Rules), len(wanted))
	}
	gladeRoot := filepath.Join(repoRoot, "..", "glade")
	findings := Verify(reviewCatalog, gladeRoot)
	if len(findings) != 0 {
		t.Fatalf("%d fresh-review product-test evidence findings:\n%s", len(findings), FormatFindings(findings))
	}
}
