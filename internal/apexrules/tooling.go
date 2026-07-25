package apexrules

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// SalesforceResult preserves compiler problems for the ledger while keeping
// parity comparison intentionally limited to accept/reject behavior.
type SalesforceResult struct {
	Outcome  Outcome  `json:"outcome"`
	Problems []string `json:"problems,omitempty"`
}

var (
	classNamePattern   = regexp.MustCompile(`(?i)\bclass\s+([A-Za-z_][A-Za-z0-9_]*)`)
	triggerNamePattern = regexp.MustCompile(`(?is)\btrigger\s+([A-Za-z_][A-Za-z0-9_]*)\s+on\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(([^)]*)\)`)
)

// RunSalesforce compiles disposable Tooling API records in the target org.
// Accepted records are deleted before this call returns. Command output stays
// in-process: callers receive only compiler problems, never CLI credentials.
func RunSalesforce(ctx context.Context, targetOrg string, rules []Rule) (map[string]SalesforceResult, error) {
	if targetOrg == "" {
		return nil, fmt.Errorf("target org is required")
	}
	results := make(map[string]SalesforceResult, len(rules))
	for _, rule := range rules {
		object, payload, err := toolingPayload(rule)
		if err != nil {
			return nil, fmt.Errorf("prepare Salesforce probe %s: %w", rule.ID, err)
		}
		out, err := runSF(ctx, "api", "request", "rest", "--method", "POST", "--url", toolingURL(rule.APIVersion, object), "--body", payload, "--target-org", targetOrg)
		if err != nil {
			results[rule.ID] = SalesforceResult{Outcome: OutcomeReject, Problems: compilerProblems(out)}
			continue
		}
		id := toolingID(out)
		if id == "" {
			return nil, fmt.Errorf("Salesforce accepted %s without a Tooling API record id", rule.ID)
		}
		if _, err := runSF(ctx, "api", "request", "rest", "--method", "DELETE", "--url", toolingURL(rule.APIVersion, object)+"/"+id, "--target-org", targetOrg); err != nil {
			return nil, fmt.Errorf("delete accepted Salesforce probe %s: %w", rule.ID, err)
		}
		results[rule.ID] = SalesforceResult{Outcome: OutcomeAccept}
	}
	return results, nil
}

func runSF(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "sf", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Do not wrap the output. It can contain environment-specific details,
		// including an accidentally echoed credential.
		return out, fmt.Errorf("Salesforce CLI command failed")
	}
	return out, nil
}

func toolingURL(apiVersion float64, object string) string {
	if apiVersion == 0 {
		apiVersion = 66
	}
	return fmt.Sprintf("/services/data/v%.1f/tooling/sobjects/%s", apiVersion, object)
}

func toolingPayload(rule Rule) (string, string, error) {
	payload := map[string]any{"Body": rule.Source}
	switch rule.SourceKind {
	case "class":
		match := classNamePattern.FindStringSubmatch(rule.Source)
		if len(match) != 2 {
			return "", "", fmt.Errorf("class source has no class name")
		}
		payload["Name"] = match[1]
		return "ApexClass", marshalPayload(payload), nil
	case "trigger":
		match := triggerNamePattern.FindStringSubmatch(rule.Source)
		if len(match) != 4 {
			return "", "", fmt.Errorf("trigger source has no trigger header")
		}
		payload["Name"] = match[1]
		payload["TableEnumOrId"] = match[2]
		for _, event := range strings.Split(match[3], ",") {
			if flag := triggerUsageFlag(event); flag != "" {
				payload[flag] = true
			}
		}
		return "ApexTrigger", marshalPayload(payload), nil
	default:
		return "", "", fmt.Errorf("unsupported source kind %q", rule.SourceKind)
	}
}

func marshalPayload(payload map[string]any) string {
	encoded, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

func triggerUsageFlag(event string) string {
	switch strings.ToLower(strings.Join(strings.Fields(event), " ")) {
	case "before insert":
		return "UsageBeforeInsert"
	case "before update":
		return "UsageBeforeUpdate"
	case "before delete":
		return "UsageBeforeDelete"
	case "after insert":
		return "UsageAfterInsert"
	case "after update":
		return "UsageAfterUpdate"
	case "after delete":
		return "UsageAfterDelete"
	case "after undelete":
		return "UsageAfterUndelete"
	default:
		return ""
	}
}

func toolingID(output []byte) string {
	var value any
	if json.Unmarshal(output, &value) != nil {
		return ""
	}
	return findJSONField(value, "id")
}

func compilerProblems(output []byte) []string {
	var value any
	if json.Unmarshal(output, &value) != nil {
		return nil
	}
	problems := collectJSONFields(value, map[string]bool{"message": true, "problem": true})
	return uniqueStrings(problems)
}

func findJSONField(value any, name string) string {
	switch item := value.(type) {
	case map[string]any:
		for key, value := range item {
			if strings.EqualFold(key, name) {
				if text, ok := value.(string); ok {
					return text
				}
			}
			if found := findJSONField(value, name); found != "" {
				return found
			}
		}
	case []any:
		for _, value := range item {
			if found := findJSONField(value, name); found != "" {
				return found
			}
		}
	}
	return ""
}

func collectJSONFields(value any, names map[string]bool) []string {
	var found []string
	switch item := value.(type) {
	case map[string]any:
		for key, value := range item {
			if names[strings.ToLower(key)] {
				if text, ok := value.(string); ok && text != "" {
					found = append(found, text)
				}
			}
			found = append(found, collectJSONFields(value, names)...)
		}
	case []any:
		for _, value := range item {
			found = append(found, collectJSONFields(value, names)...)
		}
	}
	return found
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; !ok {
			seen[value] = struct{}{}
			result = append(result, value)
		}
	}
	return result
}
