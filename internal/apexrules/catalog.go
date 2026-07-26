package apexrules

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

func LoadCatalog(path string) (Catalog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Catalog{}, err
	}
	var catalog Catalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		return Catalog{}, err
	}
	return catalog, catalog.Validate()
}

func (catalog Catalog) Validate() error {
	if !IsFullGitSHA(catalog.GladeCommit) {
		return fmt.Errorf("catalog gladeCommit must be a full 40-character lowercase Git SHA")
	}
	seen := map[string]bool{}
	for _, rule := range catalog.Rules {
		if strings.TrimSpace(rule.ID) == "" || strings.TrimSpace(rule.Area) == "" || strings.TrimSpace(rule.DocsPath) == "" || strings.TrimSpace(rule.DocsLines) == "" || strings.TrimSpace(rule.SourceKind) == "" || strings.TrimSpace(rule.Source) == "" || strings.TrimSpace(rule.Owner) == "" {
			return fmt.Errorf("rule %q is missing required evidence", rule.ID)
		}
		if seen[rule.ID] {
			return fmt.Errorf("duplicate rule ID %q", rule.ID)
		}
		seen[rule.ID] = true
		const expandedDocsRoot = "salesforce-docs-expanded-run/"
		if _, remainder, found := strings.Cut(rule.DocsPath, expandedDocsRoot); found && !strings.Contains(remainder, "/") {
			return fmt.Errorf("rule %q docsPath is missing its Salesforce docset directory", rule.ID)
		}
		if rule.Oracle != OutcomeAccept && rule.Oracle != OutcomeReject {
			return fmt.Errorf("rule %q has unknown oracle outcome %q", rule.ID, rule.Oracle)
		}
		switch rule.Status {
		case StatusSupported, StatusConfirmedGap, StatusRuntimeOnly, StatusPackageHistoryPending, StatusPreviewDisabled, StatusOraclePending:
		default:
			return fmt.Errorf("rule %q has unknown status %q", rule.ID, rule.Status)
		}
		if rule.Status == StatusSupported && strings.TrimSpace(rule.ProductTest) == "" {
			return fmt.Errorf("supported rule %q is missing productTest", rule.ID)
		}
	}
	return nil
}

func IsFullGitSHA(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			if char < 'a' || char > 'f' {
				return false
			}
		}
	}
	return true
}
