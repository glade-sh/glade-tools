package corpusassurance

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func TestMatcherReplaceAllRegionProbeIsIsolated(t *testing.T) {
	root := filepath.Join("..", "..", "docs", "fixtures")
	matcher, err := compat.LoadFile(filepath.Join(root, "core-pattern-matcher-stdlib.json"))
	if err != nil {
		t.Fatal(err)
	}
	region, err := compat.LoadFile(filepath.Join(root, "core-pattern-matcher-replaceall-region-stdlib.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matcher.Evidence) != 24 {
		t.Fatalf("matcher evidence rows = %d, want 24", len(matcher.Evidence))
	}
	for _, row := range matcher.Evidence {
		if row.SurfaceID == "apex:System.Matcher.replaceAll" {
			t.Fatal("matcher fixture retains the replaceAll row")
		}
	}
	if len(matcher.Source) != 1 || len(matcher.Command.Args) != 1 || matcher.Source[0].Content != matcher.Command.Args[0] || strings.Contains(matcher.Source[0].Content, "regionReplace.replaceAll('x')") {
		t.Fatal("matcher fixture retains the region-sensitive replaceAll probe or its source and command differ")
	}
	if len(region.Evidence) != 1 || region.Evidence[0].SurfaceID != "apex:System.Matcher.replaceAll" || region.Evidence[0].Kind != "exec" {
		t.Fatalf("region evidence = %#v, want only Matcher.replaceAll", region.Evidence)
	}
	if len(region.Source) != 1 || len(region.Command.Args) != 1 || region.Source[0].Content != region.Command.Args[0] {
		t.Fatal("region source and command differ")
	}
	source := region.Source[0].Content
	if !strings.Contains(source, "regionReplace.region(3, 10)") || !strings.Contains(source, "System.assertEquals('aa x bb x cc', regionReplace.replaceAll('x'))") || strings.Contains(source, "aa x bb DEF cc") {
		t.Fatalf("region source does not pin full-input replacement: %q", source)
	}
	if result, err := compat.Run(region); err != nil || !result.OK {
		t.Fatalf("region execution = %#v, error = %v", result, err)
	}
}
