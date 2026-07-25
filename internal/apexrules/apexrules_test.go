package apexrules

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

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
}

func TestCompareUsesOutcomeRatherThanCompilerWording(t *testing.T) {
	rules := []Rule{{ID: "APEX-001", Oracle: OutcomeReject, Status: StatusSupported}, {ID: "APEX-002", Oracle: OutcomeAccept, Status: StatusSupported}}
	results := Compare(rules, map[string]Outcome{"APEX-001": OutcomeReject, "APEX-002": OutcomeReject})
	if len(results) != 2 || !results[0].Matched || results[1].Matched {
		t.Fatalf("results = %#v", results)
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
if grep -R "REJECT_ME" "$project/force-app/main/default" >/dev/null; then exit 1; fi
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
	if string(log) == "" || !contains(string(log), "--method DELETE") {
		t.Fatalf("accepted probe was not deleted: %s", log)
	}
	if !contains(string(log), "request rest /services/data/v66.0/tooling/sobjects/ApexClass --method POST") || contains(string(log), "--url") {
		t.Fatalf("Tooling API request did not use Salesforce CLI positional URL syntax: %s", log)
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
