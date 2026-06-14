package perfscan

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteSARIFIncludesRulesAndLocations(t *testing.T) {
	report := Report{SchemaVersion: SchemaVersion, Project: "/tmp/project"}
	report.AddFinding(Finding{
		ID:         "perf.soql.loop",
		Severity:   SeverityHigh,
		Confidence: ConfidenceStatic,
		Message:    "SOQL inside a loop can exceed query limits.",
		Location:   Location{File: "/tmp/project/force-app/main/default/classes/Risk.cls", Line: 7, Column: 5},
	})
	report.Finalize()

	var out bytes.Buffer
	if err := WriteSARIF(&out, report); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{`"version": "2.1.0"`, `"ruleId": "perf.soql.loop"`, `"uri": "force-app/main/default/classes/Risk.cls"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %s in %s", want, text)
		}
	}
}
