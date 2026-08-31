package corpusassurance

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func TestMatcherEndStateProbeIsIsolated(t *testing.T) {
	root := filepath.Join("..", "..", "docs", "fixtures")
	matcher, err := compat.LoadFile(filepath.Join(root, "core-runtime-matcher-exact-evidence.json"))
	if err != nil {
		t.Fatal(err)
	}
	endState, err := compat.LoadFile(filepath.Join(root, "core-runtime-matcher-end-state-exact-evidence.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matcher.Evidence) != 25 {
		t.Fatalf("matcher evidence rows = %d, want 25", len(matcher.Evidence))
	}
	for _, row := range matcher.Evidence {
		if row.SurfaceID == "apex:System.Matcher.hitEnd()" || row.SurfaceID == "apex:System.Matcher.requireEnd()" {
			t.Fatalf("matcher fixture retains end-state row %s", row.SurfaceID)
		}
	}
	if len(matcher.Source) != 1 || len(matcher.Command.Args) != 1 || matcher.Source[0].Content != matcher.Command.Args[0] || strings.Contains(matcher.Source[0].Content, "tail.requireEnd()") {
		t.Fatal("matcher fixture retains the requireEnd probe or its source and command differ")
	}
	for _, token := range []string{
		"System.assertNotEquals(region, region.region(4, 7))",
		"System.assertNotEquals(region, region.useAnchoringBounds(false))",
		"System.assertNotEquals(region, region.useTransparentBounds(true))",
		"System.assertNotEquals(region, region.usePattern(Pattern.compile('\\\\d+')))",
		"System.assert(!tail.find())",
		"System.assert(tail.hitEnd())",
	} {
		if !strings.Contains(matcher.Source[0].Content, token) {
			t.Fatalf("matcher source missing Salesforce expectation %q", token)
		}
	}
	want := map[string]bool{
		"apex:System.Matcher.hitEnd()":     true,
		"apex:System.Matcher.requireEnd()": true,
	}
	if len(endState.Evidence) != len(want) {
		t.Fatalf("end-state evidence rows = %d, want %d", len(endState.Evidence), len(want))
	}
	for _, row := range endState.Evidence {
		if !want[row.SurfaceID] {
			t.Fatalf("unexpected end-state row %s", row.SurfaceID)
		}
		delete(want, row.SurfaceID)
	}
	if len(endState.Source) != 1 || len(endState.Command.Args) != 1 || endState.Source[0].Content != endState.Command.Args[0] {
		t.Fatal("end-state source and command differ")
	}
	source := endState.Source[0].Content
	for _, token := range []string{"System.assert(!tail.find())", "System.assert(tail.hitEnd())", "System.assert(!tail.requireEnd())"} {
		if !strings.Contains(source, token) {
			t.Fatalf("end-state source missing %q", token)
		}
	}
}
