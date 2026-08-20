package surfaceledger

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func TestSOSLReturningWhereFixtureCoversInPredicate(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "fixtures", "query-runtime-soqlsosl-guide-behavior.json")
	fixture, err := compat.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if fixture.Command.Kind != "test" {
		t.Fatalf("command kind = %q, want test", fixture.Command.Kind)
	}
	source := ""
	for _, file := range fixture.Source {
		if strings.HasSuffix(file.Path, "QueryRuntimeSOSLReturningWhereInTest.cls") {
			source = file.Content
		}
	}
	for _, want := range []string{"WHERE Name IN (\\'Beta\\')", "System.assertEquals(beta.Id, soslInRows[0].Id)"} {
		if !strings.Contains(source, want) {
			t.Fatalf("fixture source missing %q", want)
		}
	}
	result, err := compat.Run(fixture)
	if err != nil || !result.OK {
		t.Fatalf("fixture run = %#v, %v", result, err)
	}
}
