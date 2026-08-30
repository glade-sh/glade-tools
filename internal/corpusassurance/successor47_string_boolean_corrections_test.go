package corpusassurance

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func TestSuccessor47StringAndBooleanFixturesMatchSalesforceSemantics(t *testing.T) {
	root := filepath.Join("..", "..", "docs", "fixtures")
	stringFixture := loadSuccessor47Fixture(t, filepath.Join(root, "core-string-more-stdlib.json"))
	booleanFixture := loadSuccessor47Fixture(t, filepath.Join(root, "core-system-boolean-exact-evidence.json"))

	if stringFixture.Command.Kind != "exec" || len(stringFixture.Command.Args) != 1 || stringFixture.Command.Args[0] != stringFixture.Source[0].Content {
		t.Fatalf("string command/source mismatch: %#v / %#v", stringFixture.Command, stringFixture.Source)
	}
	for _, assertion := range []string{
		"System.assert(!lowerWithDigits.isAllLowerCase());",
		"System.assert(!upperWithDigits.isAllUpperCase());",
	} {
		if !strings.Contains(stringFixture.Source[0].Content, assertion) {
			t.Fatalf("string fixture missing %q", assertion)
		}
	}

	if booleanFixture.Command.Kind != "exec" || len(booleanFixture.Command.Args) != 1 || booleanFixture.Command.Args[0] != booleanFixture.Source[0].Content {
		t.Fatalf("boolean command/source mismatch: %#v / %#v", booleanFixture.Command, booleanFixture.Source)
	}
	for _, assertion := range []string{
		"Boolean truthy = Boolean.valueOf('true');",
		"Boolean padded = Boolean.valueOf(' TRUE ');",
		"System.assert(!padded);",
		"Object raw = false;",
		"Boolean falsy = Boolean.valueOf(raw);",
		"System.assert(!falsy);",
	} {
		if !strings.Contains(booleanFixture.Source[0].Content, assertion) {
			t.Fatalf("boolean fixture missing %q", assertion)
		}
	}
}

func loadSuccessor47Fixture(t *testing.T, path string) compat.Fixture {
	t.Helper()
	fixture, err := compat.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	return fixture
}
