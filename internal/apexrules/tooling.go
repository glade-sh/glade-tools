package apexrules

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"
)

// SalesforceResult preserves compiler problems for the ledger while keeping
// parity comparison intentionally limited to accept/reject behavior.
type SalesforceResult struct {
	Outcome  Outcome  `json:"outcome"`
	Problems []string `json:"problems,omitempty"`
}

// SalesforceOperationalDiagnostic identifies an inconclusive Salesforce
// observation without retaining CLI output or environment-specific details.
type SalesforceOperationalDiagnostic struct {
	RuleOrdinal    int      `json:"ruleOrdinal"`
	RuleID         string   `json:"ruleId"`
	Stage          string   `json:"stage"`
	APIVersion     float64  `json:"apiVersion"`
	FailureKind    string   `json:"failureKind"`
	ExitCode       int      `json:"exitCode"`
	TimedOut       bool     `json:"timedOut"`
	ErrorCodes     []string `json:"salesforceCodes,omitempty"`
	ResponseSHA256 string   `json:"responseSHA256"`
}

// SalesforceOperationalError carries only the sanitized diagnostic.
type SalesforceOperationalError struct {
	Diagnostic SalesforceOperationalDiagnostic `json:"diagnostic"`
}

func (e *SalesforceOperationalError) Error() string {
	d := e.Diagnostic
	return fmt.Sprintf("Salesforce compiler request failed: ruleOrdinal=%d ruleId=%q stage=%s apiVersion=%.1f failureKind=%s exitCode=%d timedOut=%t errorCodes=%v responseSha256=%s", d.RuleOrdinal, d.RuleID, d.Stage, d.APIVersion, d.FailureKind, d.ExitCode, d.TimedOut, d.ErrorCodes, d.ResponseSHA256)
}

var (
	classNamePattern           = regexp.MustCompile(`(?i)\b(?:class|interface|enum)\s+([A-Za-z_][A-Za-z0-9_]*)`)
	triggerNamePattern         = regexp.MustCompile(`(?is)\btrigger\s+([A-Za-z_][A-Za-z0-9_]*)\s+on\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(([^)]*)\)`)
	salesforceErrorCodePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
)

const (
	acceptedToolingDeleteAttempts = 3
	acceptedToolingCleanupTimeout = 30 * time.Second
)

// RunSalesforce compiles disposable Tooling API records in the target org.
// Accepted records are deleted before this call returns. Command output stays
// in-process: callers receive only compiler problems, never CLI credentials.
func RunSalesforce(ctx context.Context, targetOrg string, rules []Rule) (map[string]SalesforceResult, error) {
	if targetOrg == "" {
		return nil, fmt.Errorf("target org is required")
	}
	results := make(map[string]SalesforceResult, len(rules))
	for index, rule := range rules {
		result, err := runSalesforceRule(ctx, targetOrg, index+1, rule)
		if err != nil {
			if result.Outcome != "" {
				results[rule.ID] = result
			}
			return results, err
		}
		results[rule.ID] = result
	}
	return results, nil
}

func runSalesforceRule(ctx context.Context, targetOrg string, ruleOrdinal int, rule Rule) (SalesforceResult, error) {
	var created []toolingRecord
	var primaryErr error
	result := SalesforceResult{}
	for _, dependency := range rule.Dependencies {
		dependencyRule, err := toolingDependencyRule(rule, dependency)
		if err != nil {
			primaryErr = newSalesforceOperationalError(ctx, ruleOrdinal, rule, "prepare", nil, err, "prepare", -1)
			break
		}
		record, dependencyResult, err := compileToolingRule(ctx, targetOrg, ruleOrdinal, rule, dependencyRule, "dependency-post")
		if err != nil {
			primaryErr = err
			break
		}
		if dependencyResult.Outcome == OutcomeReject {
			primaryErr = fmt.Errorf("Salesforce dependency for %s was rejected: %s", rule.ID, strings.Join(dependencyResult.Problems, "; "))
			break
		}
		created = append(created, record)
	}
	if primaryErr == nil {
		record, probeResult, err := compileToolingRule(ctx, targetOrg, ruleOrdinal, rule, rule, "probe-post")
		if err != nil {
			primaryErr = err
		} else {
			result = probeResult
			if result.Outcome == OutcomeAccept {
				created = append(created, record)
			}
		}
	}
	cleanupCtx, cancelCleanup := context.WithTimeout(context.WithoutCancel(ctx), acceptedToolingCleanupTimeout)
	defer cancelCleanup()
	cleanupErr := deleteToolingRecords(cleanupCtx, targetOrg, ruleOrdinal, rule, created)
	return result, errors.Join(primaryErr, cleanupErr)
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

func compileToolingRule(ctx context.Context, targetOrg string, ruleOrdinal int, parentRule, rule Rule, stage string) (toolingRecord, SalesforceResult, error) {
	object, payload, err := toolingPayload(rule)
	if err != nil {
		return toolingRecord{}, SalesforceResult{}, newSalesforceOperationalError(ctx, ruleOrdinal, parentRule, "prepare", nil, err, "prepare", -1)
	}
	out, err := runSF(ctx, "api", "request", "rest", toolingURL(rule.APIVersion, object), "--method", "POST", "--body", payload, "--target-org", targetOrg)
	if err != nil {
		problems := compilerProblems(out)
		if len(problems) == 0 {
			return toolingRecord{}, SalesforceResult{}, newSalesforceOperationalError(ctx, ruleOrdinal, parentRule, stage, out, err, "", -1)
		}
		return toolingRecord{}, SalesforceResult{Outcome: OutcomeReject, Problems: problems}, nil
	}
	id := toolingID(out)
	if id == "" {
		return toolingRecord{}, SalesforceResult{}, newSalesforceOperationalError(ctx, ruleOrdinal, parentRule, stage, out, nil, "response", 0)
	}
	return toolingRecord{object: object, id: id}, SalesforceResult{Outcome: OutcomeAccept}, nil
}

func deleteToolingRecords(ctx context.Context, targetOrg string, ruleOrdinal int, rule Rule, records []toolingRecord) error {
	var firstErr error
	for index := len(records) - 1; index >= 0; index-- {
		record := records[index]
		if failure := deleteToolingRecord(ctx, targetOrg, toolingURL(rule.APIVersion, record.object)+"/"+record.id); failure != nil && firstErr == nil {
			firstErr = &SalesforceOperationalError{Diagnostic: SalesforceOperationalDiagnostic{
				RuleOrdinal: ruleOrdinal, RuleID: rule.ID, Stage: "accepted-delete", APIVersion: effectiveAPIVersion(rule.APIVersion),
				FailureKind: failure.kind, ExitCode: failure.exitCode, TimedOut: failure.timedOut,
				ErrorCodes: failure.errorCodes, ResponseSHA256: failure.responseSHA256,
			}}
		}
	}
	return firstErr
}

type salesforceCommandFailure struct {
	kind           string
	exitCode       int
	timedOut       bool
	errorCodes     []string
	responseSHA256 string
}

func deleteToolingRecord(ctx context.Context, targetOrg, url string) *salesforceCommandFailure {
	payload, err := json.Marshal(map[string]any{
		"url":    url,
		"method": "DELETE",
		"body": map[string]any{
			"mode": "raw",
			"raw":  "",
		},
	})
	if err != nil {
		return commandFailure(ctx, nil, err, "prepare", -1)
	}
	file, err := os.CreateTemp("", "glade-apex-rule-delete-*.json")
	if err != nil {
		return commandFailure(ctx, nil, err, "prepare", -1)
	}
	path := file.Name()
	defer os.Remove(path)
	if _, err := file.Write(payload); err != nil {
		file.Close()
		return commandFailure(ctx, nil, err, "prepare", -1)
	}
	if err := file.Close(); err != nil {
		return commandFailure(ctx, nil, err, "prepare", -1)
	}
	var lastFailure *salesforceCommandFailure
	for attempt := 0; attempt < acceptedToolingDeleteAttempts; attempt++ {
		out, err := runSF(ctx, "api", "request", "rest", "--file", path, "--target-org", targetOrg)
		if err == nil {
			return nil
		}
		lastFailure = commandFailure(ctx, out, err, "", -1)
		if toolingRecordIsGone(ctx, targetOrg, url) {
			return nil
		}
		if ctx.Err() != nil {
			lastFailure.kind = failureKind(ctx, ctx.Err())
			lastFailure.timedOut = errors.Is(ctx.Err(), context.DeadlineExceeded)
			return lastFailure
		}
		if attempt+1 < acceptedToolingDeleteAttempts {
			select {
			case <-ctx.Done():
				lastFailure.kind = failureKind(ctx, ctx.Err())
				lastFailure.timedOut = errors.Is(ctx.Err(), context.DeadlineExceeded)
				return lastFailure
			case <-time.After(time.Duration(attempt+1) * time.Second):
			}
		}
	}
	return lastFailure
}

func toolingRecordIsGone(ctx context.Context, targetOrg, url string) bool {
	out, err := runSF(ctx, "api", "request", "rest", url, "--target-org", targetOrg)
	if err == nil {
		return false
	}
	return toolingHasErrorCode(out, "INVALID_CROSS_REFERENCE_KEY") ||
		toolingHasErrorCode(out, "NOT_FOUND")
}

func runSF(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "sf", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, err
	}
	return out, nil
}

func newSalesforceOperationalError(ctx context.Context, ruleOrdinal int, rule Rule, stage string, output []byte, err error, kind string, exitCode int) *SalesforceOperationalError {
	failure := commandFailure(ctx, output, err, kind, exitCode)
	return &SalesforceOperationalError{Diagnostic: SalesforceOperationalDiagnostic{
		RuleOrdinal: ruleOrdinal, RuleID: rule.ID, Stage: stage, APIVersion: effectiveAPIVersion(rule.APIVersion),
		FailureKind: failure.kind, ExitCode: failure.exitCode, TimedOut: failure.timedOut,
		ErrorCodes: failure.errorCodes, ResponseSHA256: failure.responseSHA256,
	}}
}

func commandFailure(ctx context.Context, output []byte, err error, kind string, exitCode int) *salesforceCommandFailure {
	if kind == "" {
		kind = failureKind(ctx, err)
	}
	if exitErr := (*exec.ExitError)(nil); errors.As(err, &exitErr) {
		exitCode = exitErr.ExitCode()
	}
	return &salesforceCommandFailure{
		kind: kind, exitCode: exitCode, timedOut: errors.Is(ctx.Err(), context.DeadlineExceeded),
		errorCodes: toolingErrorCodes(output), responseSHA256: fmt.Sprintf("%x", sha256.Sum256(output)),
	}
}

func failureKind(ctx context.Context, err error) string {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return "canceled"
	}
	if exitErr := (*exec.ExitError)(nil); errors.As(err, &exitErr) {
		return "exit"
	}
	return "execution"
}

func effectiveAPIVersion(apiVersion float64) float64 {
	if apiVersion == 0 {
		return 66
	}
	return apiVersion
}

func toolingURL(apiVersion float64, object string) string {
	apiVersion = effectiveAPIVersion(apiVersion)
	return fmt.Sprintf("/services/data/v%.1f/tooling/sobjects/%s", apiVersion, object)
}

func toolingPayload(rule Rule) (string, string, error) {
	apiVersion := effectiveAPIVersion(rule.APIVersion)
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
	compilerFailure := false
	for _, code := range collectJSONFields(value, map[string]bool{"errorcode": true}) {
		if strings.EqualFold(code, "INVALID_FIELD_FOR_INSERT_UPDATE") {
			compilerFailure = true
			break
		}
	}
	if !compilerFailure {
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

func toolingErrorCodes(output []byte) []string {
	value, ok := toolingJSON(output)
	if !ok {
		return nil
	}
	var codes []string
	for _, code := range collectJSONFields(value, map[string]bool{"errorcode": true}) {
		if salesforceErrorCodePattern.MatchString(code) {
			codes = append(codes, code)
		}
	}
	codes = uniqueStrings(codes)
	sort.Strings(codes)
	return codes
}

func toolingJSON(output []byte) (any, bool) {
	for start, marker := range output {
		if marker != '{' && marker != '[' {
			continue
		}
		var value any
		if err := json.NewDecoder(bytes.NewReader(output[start:])).Decode(&value); err == nil {
			return value, true
		}
	}
	return nil, false
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
