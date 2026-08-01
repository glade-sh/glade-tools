package surfaceledger

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func TestStringFormatAPI67ErrorEvidenceIsExecutableAndClosesSupportedSurface(t *testing.T) {
	fixturesDir := filepath.Join("..", "..", "docs", "fixtures")
	path := filepath.Join(fixturesDir, "core-string-format-messageformat-api67-errors.json")
	oldPath := filepath.Join(fixturesDir, "core-string-format-messageformat-unsupported.json")

	if _, err := os.Stat(oldPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale unsupported String.format fixture still exists: %s", oldPath)
	}

	fixture, err := compat.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if fixture.Name != "core-string-format-messageformat-api67-errors" {
		t.Fatalf("fixture name = %q", fixture.Name)
	}
	var expectedResult struct {
		Debug any  `json:"debug"`
		OK    bool `json:"ok"`
	}
	if err := json.Unmarshal(fixture.Expected.Result, &expectedResult); err != nil {
		t.Fatalf("fixture expected result is invalid JSON: %v", err)
	}
	if fixture.Command.Kind != "exec" || len(fixture.Command.Args) != 1 || fixture.Expected.Error != nil || expectedResult.Debug != nil || !expectedResult.OK {
		t.Fatalf("fixture command/expected result = %#v/%s", fixture.Command, fixture.Expected.Result)
	}
	if len(fixture.Evidence) != 1 || fixture.Evidence[0].Symbol != "String.format" || fixture.Evidence[0].Kind != "exec" {
		t.Fatalf("fixture evidence = %#v", fixture.Evidence)
	}
	if len(fixture.Source) != 1 || fixture.Source[0].Content != fixture.Command.Args[0] {
		t.Fatalf("fixture source/command mismatch")
	}
	source := fixture.Source[0].Content
	for _, marker := range []string{
		"String.format('{0,number,#.00}', numberArgs);",
		"Cannot format given Object as a Number",
		"String.format('{0,date,yyyy-MM-dd}', dateArgs);",
		"Cannot format given Object (java.lang.String) as a Date",
		"String.format('{0,choice,0#none|1#one|1<many}', numberArgs);",
		"'''2'' is not a Number",
		"String.format('{0,unknown}', numberArgs);",
		"Unknown format type \"unknown\"",
		"String.format('{0,}', new List<Object>{ '2' });",
		"Bad argument syntax: [at pattern index 1] \"0,}\"",
		"System.assertEquals('{3}', String.format('{3,number}', new List<Object>{ '2' }));",
		"String.format('{3,unknown}', new List<Object>{ '2' });",
	} {
		if !strings.Contains(source, marker) {
			t.Fatalf("fixture source is missing %q", marker)
		}
	}

	evidence, err := BuildEvidenceSnapshot([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	ledger := Merge(nil, nil, BuildGladeSnapshot(), evidence)
	row, ok := rowsByID(ledger.Rows)["apex:System.String.format"]
	if !ok {
		t.Fatal("missing apex:System.String.format row")
	}
	if row.GladeShape == ShapeAbsent || row.GladeBehavior != BehaviorSupported || row.Evidence != EvidenceFixture || row.GapClass != "" {
		t.Fatalf("String.format merged state = shape:%s behavior:%s evidence:%s gap:%s bucket:%s", row.GladeShape, row.GladeBehavior, row.Evidence, row.GapClass, row.Bucket)
	}

	paths, err := filepath.Glob(filepath.Join(fixturesDir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, fixturePath := range paths {
		data, err := os.ReadFile(fixturePath)
		if err != nil {
			t.Fatal(err)
		}
		var candidate struct {
			Evidence []struct {
				Symbol string `json:"symbol"`
				Kind   string `json:"kind"`
			} `json:"evidence"`
			Expected struct {
				Error *struct {
					Type string `json:"type"`
				} `json:"error"`
			} `json:"expected"`
		}
		if err := json.Unmarshal(data, &candidate); err != nil {
			t.Fatal(err)
		}
		for _, item := range candidate.Evidence {
			isStringFormat := strings.EqualFold(item.Symbol, "String.format") || strings.EqualFold(item.Symbol, "apex:System.String.format")
			if isStringFormat && (strings.EqualFold(item.Kind, "unsupported") || (candidate.Expected.Error != nil && strings.EqualFold(candidate.Expected.Error.Type, "UnsupportedFeature"))) {
				t.Fatalf("unsupported String.format evidence reintroduced in %s", filepath.Base(fixturePath))
			}
		}
	}
}
