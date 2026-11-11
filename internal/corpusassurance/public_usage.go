package corpusassurance

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/glade-sh/glade/internal/storage"
	"github.com/glade-sh/glade/internal/typesys"
)

// A usage key is published only when it names Salesforce-defined surface that
// the shipped Glade catalogs list: Apex platform types and members, and
// standard sObjects and their fields. Matching is exact and case-insensitive,
// as Apex is. Anything else, including names without a custom suffix, is
// org-defined and is not published.
const orgDefinedUsage = "org-defined"

var (
	platformUsageOnce sync.Once
	platformUsageKeys map[string]struct{}
)

func loadPlatformUsageKeys() map[string]struct{} {
	platformUsageOnce.Do(func() {
		keys := map[string]struct{}{}
		schemaNames := map[string]bool{}
		for _, name := range typesys.StandardSchemaNamespaceTypeNames() {
			schemaNames[strings.ToLower(name)] = true
		}
		add := func(prefixes []string, name string, members []typesys.MemberSymbol) {
			for _, prefix := range prefixes {
				base := strings.ToLower(prefix + name)
				keys[base] = struct{}{}
				for _, member := range members {
					keys[base+"."+strings.ToLower(member.Name)] = struct{}{}
				}
			}
		}
		for _, symbol := range typesys.StandardPlatformSymbolView() {
			if symbol.Name == "" {
				continue
			}
			switch {
			case symbol.Namespace != "":
				add([]string{symbol.Namespace + "."}, symbol.Name, symbol.Members)
			case schemaNames[strings.ToLower(symbol.Name)]:
				add([]string{"", "Schema."}, symbol.Name, symbol.Members)
			default:
				add([]string{"", "System."}, symbol.Name, symbol.Members)
			}
		}
		platformUsageKeys = keys
	})
	return platformUsageKeys
}

func standardSObjectUsageKey(key string) bool {
	parts := strings.Split(key, ".")
	if len(parts) > 1 && strings.EqualFold(parts[0], "Schema") {
		parts = parts[1:]
	}
	if len(parts) == 0 || len(parts) > 2 {
		return false
	}
	definition, ok := storage.StandardObjectDefinition(parts[0])
	if !ok {
		return false
	}
	if len(parts) == 1 {
		return true
	}
	_, ok = storage.ResolveFieldName(definition, "", parts[1])
	return ok
}

func isSalesforceDefinedUsage(key string) bool {
	if key == "" || strings.Contains(key, "__") || strings.TrimSpace(key) != key {
		return false
	}
	if _, ok := loadPlatformUsageKeys()[strings.ToLower(key)]; ok {
		return true
	}
	return standardSObjectUsageKey(key)
}

func publicUsageKey(key string) string {
	if isSalesforceDefinedUsage(key) {
		return key
	}
	return orgDefinedUsage
}

func redactAssuranceUsage(report AssuranceReport) AssuranceReport {
	redact := func(row AssuranceSurfaceRow) AssuranceSurfaceRow {
		row.UsageKeys = append([]string(nil), row.UsageKeys...)
		for i, key := range row.UsageKeys {
			row.UsageKeys[i] = publicUsageKey(key)
		}
		return row
	}
	report.Rows = append([]AssuranceSurfaceRow(nil), report.Rows...)
	for i, row := range report.Rows {
		report.Rows[i] = redact(row)
	}
	report.RepositorySurfaceRows = append([]AssuranceRepositorySurfaceRow(nil), report.RepositorySurfaceRows...)
	for i, row := range report.RepositorySurfaceRows {
		report.RepositorySurfaceRows[i].AssuranceSurfaceRow = redact(row.AssuranceSurfaceRow)
	}
	return report
}

// The standalone HTML entry point must not render unclassified keys, even if
// it is invoked directly with JSON that did not pass through the generator.
func validatePublicUsageJSON(data []byte) error {
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	var walk func(any) error
	walk = func(value any) error {
		switch v := value.(type) {
		case map[string]any:
			for key, child := range v {
				if key == "usageKeys" {
					keys, ok := child.([]any)
					if !ok && child != nil {
						return fmt.Errorf("invalid public usage keys")
					}
					for _, item := range keys {
						s, ok := item.(string)
						if !ok || (s != orgDefinedUsage && publicUsageKey(s) != s) {
							return fmt.Errorf("unclassified public usage key")
						}
					}
				}
				if err := walk(child); err != nil {
					return err
				}
			}
		case []any:
			for _, child := range v {
				if err := walk(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(value)
}
