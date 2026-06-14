package perfscan

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestReportModelSortsFindingsByScore(t *testing.T) {
	report := Report{SchemaVersion: SchemaVersion, Project: "/tmp/project"}
	report.AddFinding(Finding{
		ID:         "perf.apex.describe.repeated",
		Category:   CategoryDescribe,
		Severity:   SeverityMedium,
		Confidence: ConfidenceStatic,
		Score:      35,
		Message:    "Repeated describe calls can burn CPU and heap.",
		Location:   Location{File: "B.cls", Line: 2},
	})
	report.AddFinding(Finding{
		ID:         "perf.soql.loop",
		Category:   CategorySOQL,
		Severity:   SeverityHigh,
		Confidence: ConfidenceStatic,
		Score:      90,
		Message:    "SOQL inside a loop can exceed query limits.",
		Location:   Location{File: "A.cls", Line: 10},
	})

	report.Finalize()

	if report.Summary.Findings != 2 || report.Summary.High != 1 || report.Summary.Medium != 1 {
		t.Fatalf("summary = %#v", report.Summary)
	}
	if report.Findings[0].ID != "perf.soql.loop" {
		t.Fatalf("first finding = %#v", report.Findings[0])
	}
	if report.Summary.Categories[string(CategorySOQL)] != 1 {
		t.Fatalf("categories = %#v", report.Summary.Categories)
	}

	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"schemaVersion":2`) || !strings.Contains(string(data), `"confidence":"static"`) {
		t.Fatalf("json missing stable fields: %s", string(data))
	}
	if strings.Contains(string(data), `"resourceRisk":{}`) {
		t.Fatalf("json includes empty resource risk: %s", string(data))
	}
}

func TestFindingCarriesTransactionEvidence(t *testing.T) {
	report := Report{SchemaVersion: SchemaVersion, Project: "/tmp/project"}
	report.AddFinding(Finding{
		ID:         "perf.soql.loop.interprocedural",
		Category:   CategorySOQL,
		Severity:   SeverityHigh,
		Confidence: ConfidenceCombined,
		Score:      96,
		EntryPoint: EntryPoint{Kind: EntryTrigger, Name: "AccountTrigger"},
		Path: []PathStep{
			{Kind: "trigger", Name: "AccountTrigger"},
			{Kind: "method", Name: "PricingService.reprice"},
			{Kind: "soql", Name: "SELECT Id FROM Product2"},
		},
		Evidence: []Evidence{
			{Kind: "static", Message: "loop multiplicity", Value: "per-record"},
			{Kind: "trace", Message: "duration ms", Value: "421"},
			{Kind: "metadata", Message: "record-triggered flows", Value: "2"},
		},
		ResourceRisk: ResourceRisk{CPU: true, DBRows: true, SharedLimit: true},
		Acceptance:   "For 200 trigger records, query count stays O(1) and selected fields match the read path.",
	})
	report.Finalize()

	if report.SchemaVersion < 2 {
		t.Fatalf("schema version = %d, want at least 2", report.SchemaVersion)
	}
	if report.Findings[0].ResourceRisk.SharedLimit != true {
		t.Fatalf("missing shared limit risk: %#v", report.Findings[0])
	}
	if report.Findings[0].Acceptance == "" {
		t.Fatalf("missing acceptance check: %#v", report.Findings[0])
	}
}

func TestMarkdownReportIncludesEvidenceAndFix(t *testing.T) {
	report := Report{SchemaVersion: SchemaVersion, Project: "/tmp/project"}
	report.AddFinding(Finding{
		ID:         "perf.soql.loop",
		Category:   CategorySOQL,
		Severity:   SeverityHigh,
		Confidence: ConfidenceStatic,
		Score:      90,
		EntryPoint: EntryPoint{Kind: EntryTrigger, Name: "AccountTrigger"},
		Message:    "SOQL inside a loop can exceed query limits.",
		Location:   Location{File: "force-app/main/default/classes/Selector.cls", Line: 12},
		Path: []PathStep{
			{Kind: "trigger", Name: "AccountTrigger"},
			{Kind: "method", Name: "PricingService.reprice"},
			{Kind: "soql", Name: "SELECT Id FROM Product2"},
		},
		Multiplicity: "per-record",
		ResourceRisk: ResourceRisk{CPU: true, DBRows: true, SharedLimit: true},
		Evidence: []Evidence{{
			Kind:    "apex",
			Message: "query executes inside loop depth 1",
		}},
		Fix:        "Move the query outside the loop and use a keyed map.",
		Acceptance: "For 200 trigger records, query count stays O(1) and selected fields match the read path.",
	})
	report.Finalize()

	var out strings.Builder
	if err := WriteMarkdown(&out, report); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{
		"# Performance Scan",
		"Findings: 1",
		"`perf.soql.loop`",
		"AccountTrigger",
		"Selector.cls:12",
		"Path: trigger AccountTrigger -> method PricingService.reprice -> soql SELECT Id FROM Product2",
		"Multiplicity: per-record",
		"Resource risk: CPU, DB rows, shared limits",
		"Move the query outside the loop",
		"Acceptance: For 200 trigger records",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("markdown missing %q:\n%s", want, text)
		}
	}
}
