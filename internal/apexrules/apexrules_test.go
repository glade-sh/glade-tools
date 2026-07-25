package apexrules

import "testing"

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
