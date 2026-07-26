package apexrules

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestToolingPayloadCarriesRuleAPIVersion(t *testing.T) {
	_, payload, err := toolingPayload(Rule{
		ID:         "APEX-API-VERSION",
		APIVersion: 65,
		SourceKind: "class",
		Source:     "public class Probe {}",
	})
	if err != nil {
		t.Fatalf("toolingPayload: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(payload), &body); err != nil {
		t.Fatalf("decode tooling payload: %v", err)
	}
	if got := body["ApiVersion"]; got != float64(65) {
		t.Fatalf("ApiVersion = %#v, want 65", got)
	}
}

func TestToolingPayloadAcceptsInterfaceDependencies(t *testing.T) {
	object, payload, err := toolingPayload(Rule{
		ID:         "APEX-INTERFACE-DEPENDENCY",
		APIVersion: 66,
		SourceKind: "class",
		Source:     "public interface ProbeDependency {}",
	})
	if err != nil {
		t.Fatalf("toolingPayload: %v", err)
	}
	if object != "ApexClass" {
		t.Fatalf("object = %q, want ApexClass", object)
	}
	if !strings.Contains(payload, `"Name":"ProbeDependency"`) {
		t.Fatalf("payload = %s, want interface name", payload)
	}
}

func TestToolingPayloadAcceptsEnums(t *testing.T) {
	object, payload, err := toolingPayload(Rule{
		ID:         "APEX-ENUM-PROBE",
		APIVersion: 66,
		SourceKind: "class",
		Source:     "public enum ProbeEnumeration { One }",
	})
	if err != nil {
		t.Fatalf("toolingPayload: %v", err)
	}
	if object != "ApexClass" {
		t.Fatalf("object = %q, want ApexClass", object)
	}
	if !strings.Contains(payload, `"Name":"ProbeEnumeration"`) {
		t.Fatalf("payload = %s, want enum name", payload)
	}
}

func TestValidateRejectsIncompleteAndDuplicateSupportedRules(t *testing.T) {
	valid := Catalog{Rules: []Rule{{
		ID: "APEX-001", Area: "identifiers", DocsPath: "apex_ref", DocsLines: "1", SourceKind: "class", Source: "public class Probe {}", Oracle: OutcomeReject, Owner: "parser", Status: StatusSupported, ProductTest: "internal/apexast/parser_test.go:TestReserved",
	}}}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid catalog: %v", err)
	}
	invalid := valid
	invalid.Rules = append(invalid.Rules, valid.Rules[0])
	if err := invalid.Validate(); err == nil {
		t.Fatal("duplicate rule accepted")
	}
	invalid = Catalog{Rules: []Rule{{ID: "APEX-002", Oracle: OutcomeReject, Status: StatusSupported}}}
	if err := invalid.Validate(); err == nil {
		t.Fatal("incomplete supported rule accepted")
	}
	invalid = valid
	invalid.Rules[0].DocsPath = "example-projects/Salesforce Docs Scraper/salesforce-docs-expanded-run/apex_reserved_words.md"
	if err := invalid.Validate(); err == nil {
		t.Fatal("Salesforce docs path without a docset directory was accepted")
	}
}

func TestCheckedApexLanguageRulesCatalogCoversEveryReservedIdentifier(t *testing.T) {
	catalog, err := LoadCatalog(filepath.Join("..", "..", "docs", "fixtures", "apex-language-rules.json"))
	if err != nil {
		t.Fatalf("load checked catalog: %v", err)
	}
	if got := len(catalog.Rules); got != 372 {
		t.Fatalf("catalog rows = %d, want 121 reserved identifier probes plus 25 prior and 226 recovered oracle-backed language rules", got)
	}
	seen := make(map[string]bool, len(catalog.Rules))
	reservedCount := 0
	for _, rule := range catalog.Rules {
		if !strings.HasPrefix(rule.ID, "APEX-RESERVED-") {
			continue
		}
		reservedCount++
		if rule.Status != StatusSupported || rule.Oracle != OutcomeReject {
			t.Fatalf("reserved row %s = %#v", rule.ID, rule)
		}
		seen[rule.ID] = true
	}
	if reservedCount != 121 {
		t.Fatalf("reserved row count = %d, want 121", reservedCount)
	}
	for _, word := range []string{"CURRENCY", "VOID", "TRIGGER", "WEBSERVICE"} {
		if !seen["APEX-RESERVED-"+word] {
			t.Fatalf("missing reserved-word probe %s", word)
		}
	}
	recovered := 0
	for _, rule := range catalog.Rules {
		if strings.HasPrefix(rule.ID, "APEX-AUDIT-") {
			recovered++
		}
	}
	if recovered != 226 {
		t.Fatalf("recovered audit rows = %d, want 226", recovered)
	}
}

func TestCompareUsesOutcomeRatherThanCompilerWording(t *testing.T) {
	rules := []Rule{{ID: "APEX-001", Oracle: OutcomeReject, Status: StatusSupported}, {ID: "APEX-002", Oracle: OutcomeAccept, Status: StatusSupported}}
	results := Compare(rules, map[string]Outcome{"APEX-001": OutcomeReject, "APEX-002": OutcomeReject})
	if len(results) != 2 || !results[0].Matched || results[1].Matched {
		t.Fatalf("results = %#v", results)
	}
}

func TestCompareObservedReportsCatalogOracleDriftSeparatelyFromGladeParity(t *testing.T) {
	rules := []Rule{{ID: "APEX-001", Oracle: OutcomeReject, Status: StatusSupported}}
	results := CompareObserved(
		rules,
		map[string]SalesforceResult{"APEX-001": {Outcome: OutcomeAccept}},
		map[string]Outcome{"APEX-001": OutcomeAccept},
	)
	if len(results) != 1 {
		t.Fatalf("results = %#v", results)
	}
	result := results[0]
	if result.CatalogOracle != OutcomeReject || result.Salesforce != OutcomeAccept || result.Oracle != OutcomeAccept {
		t.Fatalf("oracle identities were collapsed: %#v", result)
	}
	if result.OracleMatched || !result.Matched {
		t.Fatalf("catalog drift and Glade parity were conflated: %#v", result)
	}
}

func TestToolingIDAcceptsSalesforceCLIWarningPreamble(t *testing.T) {
	output := []byte("Warning: This command is currently in beta.\n{\n  \"id\": \"01p000000000001AAA\",\n  \"success\": true\n}\n")
	if got := toolingID(output); got != "01p000000000001AAA" {
		t.Fatalf("toolingID() = %q", got)
	}
}

func TestCompilerProblemsRejectsOperationalSalesforceErrors(t *testing.T) {
	for _, code := range []string{"INVALID_SESSION_ID", "REQUEST_LIMIT_EXCEEDED", "NOT_FOUND", "DUPLICATE_VALUE"} {
		t.Run(code, func(t *testing.T) {
			output := []byte(`[{"message":"request failed","errorCode":"` + code + `"}]`)
			if got := compilerProblems(output); len(got) != 0 {
				t.Fatalf("compilerProblems(%s) = %#v, want operational failure", code, got)
			}
		})
	}
	compiler := []byte(`[{"message":"Unexpected token 'currency'","errorCode":"INVALID_FIELD_FOR_INSERT_UPDATE"}]`)
	if got := compilerProblems(compiler); len(got) != 1 || got[0] != "Unexpected token 'currency'" {
		t.Fatalf("compilerProblems(compiler) = %#v", got)
	}
}

func TestRunGladeBuildsIsolatedProjectAndRecordsOutcomes(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "fake-glade")
	if err := os.WriteFile(bin, []byte(`#!/bin/sh
set -eu
project=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "--project" ]; then project="$2"; shift 2; continue; fi
  shift
done
test -f "$project/sfdx-project.json"
if grep -R "REJECT_ME" "$project/force-app/main/default" >/dev/null; then
  printf '{"status":"failed","exitCode":1,"diagnostics":[{"severity":"error","code":"GLADESEMA","message":"rejected"}]}\n'
  exit 1
fi
printf '{"status":"passed","exitCode":0,"diagnostics":[]}\n'
`), 0o700); err != nil {
		t.Fatal(err)
	}
	rules := []Rule{
		{ID: "APEX-001", SourceKind: "class", Source: "public class Good {}", APIVersion: 66},
		{ID: "APEX-002", SourceKind: "trigger", Source: "trigger Bad on Account (before insert) { REJECT_ME; }", APIVersion: 65},
	}
	outcomes, err := RunGlade(context.Background(), bin, rules)
	if err != nil {
		t.Fatalf("RunGlade: %v", err)
	}
	if outcomes["APEX-001"] != OutcomeAccept || outcomes["APEX-002"] != OutcomeReject {
		t.Fatalf("outcomes = %#v", outcomes)
	}
}

func TestRunGladeClassifiesOnlyCompletedDiagnosticFailuresAsRejects(t *testing.T) {
	for _, test := range []struct {
		name      string
		output    string
		exitCode  int
		want      Outcome
		wantError bool
	}{
		{name: "diagnostic rejection", output: `{"status":"failed","exitCode":1,"diagnostics":[{"severity":"error","code":"APEXPARSE002","message":"bad"}]}`, exitCode: 1, want: OutcomeReject},
		{name: "diagnostic rejection with warning preamble", output: "warning: cache disabled\n" + `{"status":"failed","exitCode":1,"diagnostics":[{"severity":"error","code":"APEXPARSE002","message":"bad"}]}`, exitCode: 1, want: OutcomeReject},
		{name: "operational exit", output: `{"status":"failed","exitCode":2,"diagnostics":[]}`, exitCode: 2, wantError: true},
		{name: "missing report", output: `glade crashed`, exitCode: 1, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "fake-glade")
			script := "#!/bin/sh\nprintf '%s\\n' '" + test.output + "'\nexit " + fmt.Sprint(test.exitCode) + "\n"
			if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
				t.Fatal(err)
			}
			outcome, err := runGladeRule(context.Background(), bin, t.TempDir(), Rule{ID: "Probe", SourceKind: "class", Source: "public class Probe {}"})
			if (err != nil) != test.wantError || outcome != test.want {
				t.Fatalf("runGladeRule() = %q, %v; want %q error=%v", outcome, err, test.want, test.wantError)
			}
		})
	}
}

func TestWriteGladeRuleProjectWritesProjectFilesWithoutToolingDependencies(t *testing.T) {
	project := t.TempDir()
	rule := Rule{
		ID:         "ProjectFilesProbe",
		SourceKind: "class",
		Source:     "public class ProjectFilesProbe {}",
		ProjectFiles: []SourceFile{{
			Path:    "force-app/main/default/objects/Account/fields/Name.field-meta.xml",
			Content: "<CustomField xmlns=\"http://soap.sforce.com/2006/04/metadata\"><fullName>Name</fullName></CustomField>",
		}},
	}
	if err := writeGladeRuleProject(project, rule); err != nil {
		t.Fatalf("writeGladeRuleProject: %v", err)
	}
	path := filepath.Join(project, rule.ProjectFiles[0].Path)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read project file: %v", err)
	}
	if string(content) != rule.ProjectFiles[0].Content {
		t.Fatalf("project file = %q, want %q", content, rule.ProjectFiles[0].Content)
	}
}

func TestRunSalesforceRecordsProblemsAndDeletesAcceptedProbes(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "sf.log")
	sf := filepath.Join(dir, "sf")
	if err := os.WriteFile(sf, []byte(`#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$APEX_RULE_SF_LOG"
case "$*" in
  *BadProbe*) printf '{"message":"Unexpected token REJECT_ME"}\n' >&2; exit 1 ;;
  *delete*) printf '{"status":0}\n' ;;
  *) printf '{"id":"01p000000000001AAA","success":true}\n' ;;
esac
`), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("APEX_RULE_SF_LOG", logPath)
	rules := []Rule{
		{ID: "GoodProbe", SourceKind: "class", Source: "public class GoodProbe {}", APIVersion: 66},
		{ID: "BadProbe", SourceKind: "class", Source: "public class BadProbe { REJECT_ME; }", APIVersion: 66},
	}
	results, err := RunSalesforce(context.Background(), "scratch", rules)
	if err != nil {
		t.Fatalf("RunSalesforce: %v", err)
	}
	if results["GoodProbe"].Outcome != OutcomeAccept || results["BadProbe"].Outcome != OutcomeReject {
		t.Fatalf("results = %#v", results)
	}
	if len(results["BadProbe"].Problems) != 1 || results["BadProbe"].Problems[0] != "Unexpected token REJECT_ME" {
		t.Fatalf("problems = %#v", results["BadProbe"].Problems)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(log) == "" || !contains(string(log), "--file") || !contains(string(log), "glade-apex-rule-delete-") {
		t.Fatalf("accepted probe was not deleted: %s", log)
	}
	if !contains(string(log), "request rest /services/data/v66.0/tooling/sobjects/ApexClass --method POST") || contains(string(log), "--url") {
		t.Fatalf("Tooling API request did not use Salesforce CLI positional URL syntax: %s", log)
	}
}

func TestRunSalesforceRetriesTransientAcceptedProbeCleanup(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "sf.log")
	deleteCountPath := filepath.Join(dir, "delete-count")
	sf := filepath.Join(dir, "sf")
	if err := os.WriteFile(sf, []byte(`#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$APEX_RULE_SF_LOG"
case "$*" in
  *glade-apex-rule-delete-*)
    count=0
    if [ -f "$APEX_RULE_DELETE_COUNT" ]; then count=$(cat "$APEX_RULE_DELETE_COUNT"); fi
    count=$((count + 1))
    printf '%s' "$count" > "$APEX_RULE_DELETE_COUNT"
    if [ "$count" -eq 1 ]; then exit 1; fi
    printf '{"status":0}\n'
    ;;
  *) printf '{"id":"01p000000000001AAA","success":true}\n' ;;
esac
`), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("APEX_RULE_SF_LOG", logPath)
	t.Setenv("APEX_RULE_DELETE_COUNT", deleteCountPath)
	results, err := RunSalesforce(context.Background(), "scratch", []Rule{{
		ID: "TransientCleanup", SourceKind: "class", Source: "public class TransientCleanup {}", APIVersion: 66,
	}})
	if err != nil {
		t.Fatalf("RunSalesforce: %v", err)
	}
	if results["TransientCleanup"].Outcome != OutcomeAccept {
		t.Fatalf("results = %#v", results)
	}
	count, err := os.ReadFile(deleteCountPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(count) != "2" {
		t.Fatalf("delete attempts = %s, want 2", count)
	}
}

func TestRunSalesforceAcceptsCleanupErrorWhenToolingRecordIsGone(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "sf.log")
	sf := filepath.Join(dir, "sf")
	if err := os.WriteFile(sf, []byte(`#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$APEX_RULE_SF_LOG"
case "$*" in
  *glade-apex-rule-delete-*|*/ApexClass/01p000000000001AAA*)
    printf '[{"message":"invalid cross reference id","errorCode":"INVALID_CROSS_REFERENCE_KEY"}]\n' >&2
    exit 1
    ;;
  *) printf '{"id":"01p000000000001AAA","success":true}\n' ;;
esac
`), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("APEX_RULE_SF_LOG", logPath)
	results, err := RunSalesforce(context.Background(), "scratch", []Rule{{
		ID: "AlreadyDeleted", SourceKind: "class", Source: "public class AlreadyDeleted {}", APIVersion: 66,
	}})
	if err != nil {
		t.Fatalf("RunSalesforce: %v", err)
	}
	if results["AlreadyDeleted"].Outcome != OutcomeAccept {
		t.Fatalf("results = %#v", results)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(log), "glade-apex-rule-delete-"); got != 1 {
		t.Fatalf("delete attempts = %d, want one confirmed-gone cleanup", got)
	}
}

func TestRunSalesforceReturnsTransportFailuresInsteadOfCallingThemCompilerRejects(t *testing.T) {
	dir := t.TempDir()
	sf := filepath.Join(dir, "sf")
	if err := os.WriteFile(sf, []byte(`#!/bin/sh
printf '%s\n' 'Error (1): HTTP response contains html content.' >&2
exit 1
`), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	_, err := RunSalesforce(context.Background(), "scratch", []Rule{{
		ID: "TransportFailure", SourceKind: "class", Source: "public class TransportFailure {}", APIVersion: 66,
	}})
	if err == nil || !strings.Contains(err.Error(), "Salesforce compiler request") {
		t.Fatalf("RunSalesforce error = %v, want transport failure", err)
	}
}

func TestRunSalesforceCompilesAndDeletesRuleDependencies(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "sf.log")
	sf := filepath.Join(dir, "sf")
	if err := os.WriteFile(sf, []byte(`#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$APEX_RULE_SF_LOG"
case "$*" in
  *delete*) printf '{"status":0}\n' ;;
  *) printf '{"id":"01p000000000001AAA","success":true}\n' ;;
esac
`), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("APEX_RULE_SF_LOG", logPath)
	rules := []Rule{{
		ID: "DependentProbe", SourceKind: "class", Source: "public class DependentProbe extends ProbeBase {}", APIVersion: 66,
		Dependencies: []SourceFile{{Path: "force-app/main/default/classes/ProbeBase.cls", Content: "public virtual class ProbeBase {}"}},
	}}
	results, err := RunSalesforce(context.Background(), "scratch", rules)
	if err != nil {
		t.Fatalf("RunSalesforce: %v", err)
	}
	if results["DependentProbe"].Outcome != OutcomeAccept {
		t.Fatalf("results = %#v", results)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(log)
	base := strings.Index(text, "ProbeBase")
	probe := strings.Index(text, "DependentProbe")
	if base < 0 || probe < 0 || base > probe {
		t.Fatalf("dependency was not compiled before probe: %s", text)
	}
	if got := strings.Count(text, "glade-apex-rule-delete-"); got != 2 {
		t.Fatalf("delete calls = %d, want dependency and probe cleanup: %s", got, text)
	}
}

func TestRunSalesforceReturnsDependencyCompilerFailuresInsteadOfClassifyingProbe(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "sf.log")
	sf := filepath.Join(dir, "sf")
	if err := os.WriteFile(sf, []byte(`#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$APEX_RULE_SF_LOG"
case "$*" in
  *BadBase*) printf '{"message":"dependency does not compile"}\n' >&2; exit 1 ;;
  *DependentProbe*) printf '%s\n' 'probe must not be compiled' >&2; exit 9 ;;
  *delete*) printf '{"status":0}\n' ;;
  *) printf '{"id":"01p000000000001AAA","success":true}\n' ;;
esac
`), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("APEX_RULE_SF_LOG", logPath)
	_, err := RunSalesforce(context.Background(), "scratch", []Rule{{
		ID:         "DependentProbe",
		SourceKind: "class",
		Source:     "public class DependentProbe extends GoodBase {}",
		APIVersion: 66,
		Dependencies: []SourceFile{
			{Path: "force-app/main/default/classes/GoodBase.cls", Content: "public virtual class GoodBase {}"},
			{Path: "force-app/main/default/classes/BadBase.cls", Content: "public class BadBase { INVALID; }"},
		},
	}})
	if err == nil || !strings.Contains(err.Error(), "Salesforce dependency for DependentProbe was rejected") {
		t.Fatalf("RunSalesforce error = %v, want dependency compiler failure", err)
	}
	log, readErr := os.ReadFile(logPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	text := string(log)
	if strings.Contains(text, "--body {\"ApiVersion\":66,\"Body\":\"public class DependentProbe") {
		t.Fatalf("probe compiled after dependency rejection: %s", text)
	}
	if got := strings.Count(text, "glade-apex-rule-delete-"); got != 1 {
		t.Fatalf("delete calls = %d, want cleanup for accepted dependency: %s", got, text)
	}
}

func TestRunSalesforceCleansAcceptedDependenciesAfterTransportFailure(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "sf.log")
	sf := filepath.Join(dir, "sf")
	if err := os.WriteFile(sf, []byte(`#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$APEX_RULE_SF_LOG"
case "$*" in
  *BrokenBase*) printf '%s\n' 'gateway unavailable' >&2; exit 1 ;;
  *delete*) printf '{"status":0}\n' ;;
  *) printf '{"id":"01p000000000001AAA","success":true}\n' ;;
esac
`), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("APEX_RULE_SF_LOG", logPath)
	_, err := RunSalesforce(context.Background(), "scratch", []Rule{{
		ID:         "DependentProbe",
		SourceKind: "class",
		Source:     "public class DependentProbe {}",
		APIVersion: 66,
		Dependencies: []SourceFile{
			{Path: "force-app/main/default/classes/GoodBase.cls", Content: "public class GoodBase {}"},
			{Path: "force-app/main/default/classes/BrokenBase.cls", Content: "public class BrokenBase {}"},
		},
	}})
	if err == nil || !strings.Contains(err.Error(), "Salesforce compiler request") {
		t.Fatalf("RunSalesforce error = %v, want transport failure", err)
	}
	log, readErr := os.ReadFile(logPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if got := strings.Count(string(log), "glade-apex-rule-delete-"); got != 1 {
		t.Fatalf("cleanup calls = %d, want accepted dependency cleanup: %s", got, log)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
