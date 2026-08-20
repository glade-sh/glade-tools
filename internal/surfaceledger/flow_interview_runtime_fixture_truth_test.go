package surfaceledger

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func TestFlowInterviewRuntimeFixtureProvesMissingFlowBoundary(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "fixtures", "core-runtime-flow-interview-runtime-contracts.json")
	fixture, err := compat.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(fixture.Source) != 1 {
		t.Fatalf("source files = %d, want 1", len(fixture.Source))
	}
	source := fixture.Source[0].Content
	for _, witness := range []string{
		"try { interview.start(); } catch (Exception e)",
		"System.assert(caught)",
		"System.assertEquals(42, interview.getVariableValue('answer'))",
	} {
		if !strings.Contains(source, witness) {
			t.Errorf("fixture source lacks %q", witness)
		}
	}
	for _, row := range fixture.Evidence {
		if row.SurfaceID == "apex:Flow.Interview.start()" && !strings.Contains(row.Notes, "rejects a missing Flow definition") {
			t.Fatalf("start evidence note overclaims runtime: %q", row.Notes)
		}
	}
	result, err := compat.Run(fixture)
	if err != nil || !result.OK {
		t.Fatalf("fixture run = %#v, error = %v", result, err)
	}
}
