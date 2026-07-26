package apexrules

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// SalesforceResult preserves compiler problems for the ledger while keeping
// parity comparison intentionally limited to accept/reject behavior.
type SalesforceResult struct {
	Outcome  Outcome  `json:"outcome"`
	Problems []string `json:"problems,omitempty"`
}

var (
	classNamePattern   = regexp.MustCompile(`(?i)\b(?:class|interface|enum)\s+([A-Za-z_][A-Za-z0-9_]*)`)
	triggerNamePattern = regexp.MustCompile(`(?is)\btrigger\s+([A-Za-z_][A-Za-z0-9_]*)\s+on\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(([^)]*)\)`)
)

const acceptedToolingDeleteAttempts = 3

// RunSalesforce compiles disposable Tooling API records in the target org.
// Accepted records are deleted before this call returns. Command output stays
// in-process: callers receive only compiler problems, never CLI credentials.
func RunSalesforce(ctx context.Context, targetOrg string, rules []Rule) (map[string]SalesforceResult, error) {
	if targetOrg == "" {
		return nil, fmt.Errorf("target org is required")
	}
	results := make(map[string]SalesforceResult, len(rules))
	for _, rule := range rules {
		var created []toolingRecord
		result := SalesforceResult{Outcome: OutcomeAccept}
		for _, dependency := range rule.Dependencies {
			dependencyRule, err := toolingDependencyRule(rule, dependency)
			if err != nil {
				return nil, fmt.Errorf("prepare Salesforce dependency for %s: %w", rule.ID, err)
			}
			record, dependencyResult, err := compileToolingRule(ctx, targetOrg, dependencyRule)
			if err != nil {
				return nil, err
			}
			if dependencyResult.Outcome == OutcomeReject {
				result = dependencyResult
				break
			}
			created = append(created, record)
		}
		if result.Outcome == OutcomeAccept {
			record, probeResult, err := compileToolingRule(ctx, targetOrg, rule)
			if err != nil {
				return nil, err
			}
			result = probeResult
			if result.Outcome == OutcomeAccept {
				created = append(created, record)
			}
		}
		if err := deleteToolingRecords(ctx, targetOrg, rule.APIVersion, created); err != nil {
			return nil, fmt.Errorf("delete accepted Salesforce probe %s: %w", rule.ID, err)
		}
		results[rule.ID] = result
	}
	return results, nil
}

type toolingRecord struct {
	object string
	id     string
}

func toolingDependencyRule(rule Rule, dependency SourceFile) (Rule, error) {
	sourceKind := ""
	switch {
	case strings.HasSuffix(strings.ToLower(dependency.Path), ".cls"):
		sourceKind = "class"
	case strings.HasSuffix(strings.ToLower(dependency.Path), ".trigger"):
		sourceKind = "trigger"
	default:
		return Rule{}, fmt.Errorf("unsupported dependency path %q", dependency.Path)
	}
	return Rule{ID: rule.ID + " dependency", APIVersion: rule.APIVersion, SourceKind: sourceKind, Source: dependency.Content}, nil
}

func compileToolingRule(ctx context.Context, targetOrg string, rule Rule) (toolingRecord, SalesforceResult, error) {
	object, payload, err := toolingPayload(rule)
	if err != nil {
		return toolingRecord{}, SalesforceResult{}, fmt.Errorf("prepare Salesforce probe %s: %w", rule.ID, err)
	}
	out, err := runSF(ctx, "api", "request", "rest", toolingURL(rule.APIVersion, object), "--method", "POST", "--body", payload, "--target-org", targetOrg)
	if err != nil {
		problems := compilerProblems(out)
		if len(problems) == 0 {
			return toolingRecord{}, SalesforceResult{}, fmt.Errorf("Salesforce compiler request %s: %w", rule.ID, err)
		}
		return toolingRecord{}, SalesforceResult{Outcome: OutcomeReject, Problems: problems}, nil
	}
	id := toolingID(out)
	if id == "" {
		return toolingRecord{}, SalesforceResult{}, fmt.Errorf("Salesforce accepted %s without a Tooling API record id", rule.ID)
	}
	return toolingRecord{object: object, id: id}, SalesforceResult{Outcome: OutcomeAccept}, nil
}

func deleteToolingRecords(ctx context.Context, targetOrg string, apiVersion float64, records []toolingRecord) error {
	for index := len(records) - 1; index >= 0; index-- {
		record := records[index]
		if err := deleteToolingRecord(ctx, targetOrg, toolingURL(apiVersion, record.object)+"/"+record.id); err != nil {
			return err
		}
	}
	return nil
}

func deleteToolingRecord(ctx context.Context, targetOrg, url string) error {
	payload, err := json.Marshal(map[string]any{
		"url":    url,
		"method": "DELETE",
		"body": map[string]any{
			"mode": "raw",
			"raw":  "",
		},
	})
	if err != nil {
		return err
	}
	file, err := os.CreateTemp("", "glade-apex-rule-delete-*.json")
	if err != nil {
		return err
	}
	path := file.Name()
	defer os.Remove(path)
	if _, err := file.Write(payload); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	for attempt := 0; attempt < acceptedToolingDeleteAttempts; attempt++ {
		_, err = runSF(ctx, "api", "request", "rest", "--file", path, "--target-org", targetOrg)
		if err == nil {
			return nil
		}
		if toolingRecordIsGone(ctx, targetOrg, url) {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if attempt+1 < acceptedToolingDeleteAttempts {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt+1) * time.Second):
			}
		}
	}
	return err
}

func toolingRecordIsGone(ctx context.Context, targetOrg, url string) bool {
	out, err := runSF(ctx, "api", "request", "rest", url, "--target-org", targetOrg)
	if err == nil {
		return false
	}
	return toolingHasErrorCode(out, "INVALID_CROSS_REFERENCE_KEY")
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
	apiVersion := rule.APIVersion
	if apiVersion == 0 {
		apiVersion = 66
	}
	payload := map[string]any{"Body": rule.Source, "ApiVersion": apiVersion}
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
	value, ok := toolingJSON(output)
	if !ok {
		return ""
	}
	return findJSONField(value, "id")
}

func compilerProblems(output []byte) []string {
	value, ok := toolingJSON(output)
	if !ok {
		return nil
	}
	problems := collectJSONFields(value, map[string]bool{"message": true, "problem": true})
	return uniqueStrings(problems)
}

func toolingHasErrorCode(output []byte, expected string) bool {
	value, ok := toolingJSON(output)
	if !ok {
		return false
	}
	for _, code := range collectJSONFields(value, map[string]bool{"errorcode": true}) {
		if strings.EqualFold(code, expected) {
			return true
		}
	}
	return false
}

func toolingJSON(output []byte) (any, bool) {
	start := bytes.IndexByte(output, '{')
	end := bytes.LastIndexByte(output, '}')
	if start < 0 || end < start {
		return nil, false
	}
	var value any
	if err := json.Unmarshal(output[start:end+1], &value); err != nil {
		return nil, false
	}
	return value, true
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
