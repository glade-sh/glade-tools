package corpusassurance

import (
	"encoding/json"
	"fmt"
)

// Only exact, reviewed Salesforce keys may be published. Unknown keys are
// org-defined even when they contain no custom-name suffix.
var publicUsageKeys = map[string]struct{}{
	"System.debug": {},
}

const orgDefinedUsage = "org-defined"

func publicUsageKey(key string) string {
	if _, ok := publicUsageKeys[key]; ok {
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
