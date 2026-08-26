package surfaceledger

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func TestDateValueOfObjectFixtureMatchesSalesforceTypeException(t *testing.T) {
	fixture, err := compat.LoadFile(filepath.Join("..", "..", "docs", "fixtures", "core-runtime-date-extra-accessors.json"))
	if err != nil {
		t.Fatal(err)
	}
	if fixture.Command.Kind != "exec" || len(fixture.Source) != 1 || len(fixture.Command.Args) != 1 || fixture.Source[0].Content != fixture.Command.Args[0] {
		t.Fatal("fixture source and command must be one identical exec program")
	}
	source := fixture.Source[0].Content
	for _, want := range []string{"catch (TypeException e)", "System.assertEquals('Invalid date: 2024-02-29', dateObjectError)"} {
		if !strings.Contains(source, want) {
			t.Fatalf("fixture source missing %q", want)
		}
	}
}

func TestDecimalValueOfFixtureUsesDoubleOverload(t *testing.T) {
	fixture, err := compat.LoadFile(filepath.Join("..", "..", "docs", "fixtures", "core-runtime-decimal-extra-accessors.json"))
	if err != nil {
		t.Fatal(err)
	}
	if fixture.Command.Kind != "exec" || len(fixture.Source) != 1 || len(fixture.Command.Args) != 1 || fixture.Source[0].Content != fixture.Command.Args[0] {
		t.Fatal("fixture source and command must be one identical exec program")
	}
	source := fixture.Source[0].Content
	if !strings.Contains(source, "Double doubleValue = 12.5;") || !strings.Contains(source, "Decimal.valueOf(doubleValue)") {
		t.Fatal("fixture must exercise Decimal.valueOf(Double) with a Double-typed value")
	}
}

func TestConstructedExceptionFixtureUsesCurrentLocalLocations(t *testing.T) {
	fixture, err := compat.LoadFile(filepath.Join("..", "..", "docs", "fixtures", "core-runtime-exception-accessors.json"))
	if err != nil {
		t.Fatal(err)
	}
	if fixture.Command.Kind != "exec" || len(fixture.Source) != 1 || len(fixture.Command.Args) != 1 || fixture.Source[0].Content != fixture.Command.Args[0] {
		t.Fatal("fixture source and command must be one identical exec program")
	}
	for _, want := range []string{
		"System.assertEquals(1, assertEx.getLineNumber())",
		"System.assertEquals('AnonymousBlock: line 1, column 1', assertEx.getStackTraceString())",
		"System.assertEquals('Procedure is only valid for System.QueryException', assertFieldsError)",
		"assertEx.initCause(assertCause);",
		"System.assertEquals(14, asyncEx.getLineNumber())",
		"System.assertEquals('AnonymousBlock: line 14, column 1', asyncEx.getStackTraceString())",
		"System.assertEquals('Procedure is only valid for System.QueryException', asyncFieldsError)",
		"asyncEx.initCause(asyncCause);",
	} {
		if !strings.Contains(fixture.Source[0].Content, want) {
			t.Fatalf("fixture source missing %q", want)
		}
	}
	if strings.Contains(fixture.Source[0].Content, "System.assertEquals(assertEx, assertEx.initCause") || strings.Contains(fixture.Source[0].Content, "System.assertEquals(asyncEx, asyncEx.initCause") {
		t.Fatal("Exception.initCause is void and must not be used as an assertion value")
	}
}

func TestExceptionFamilyFixtureUsesCurrentLocalContracts(t *testing.T) {
	fixture, err := compat.LoadFile(filepath.Join("..", "..", "docs", "fixtures", "core-runtime-exception-family-accessors.json"))
	if err != nil {
		t.Fatal(err)
	}
	if fixture.Command.Kind != "exec" || len(fixture.Source) != 1 || len(fixture.Command.Args) != 1 || fixture.Source[0].Content != fixture.Command.Args[0] {
		t.Fatal("fixture source and command must be one identical exec program")
	}
	result, err := compat.Run(fixture)
	if err != nil || !result.OK {
		t.Fatalf("fixture run = %#v, %v", result, err)
	}
}

func TestExceptionHeaderFixtureRunsItsCurrentSource(t *testing.T) {
	fixture, err := compat.LoadFile(filepath.Join("..", "..", "docs", "fixtures", "core-runtime-exception-header-readonly-accessors.json"))
	if err != nil {
		t.Fatal(err)
	}
	if fixture.Command.Kind != "exec" || len(fixture.Source) != 1 || len(fixture.Command.Args) != 1 || fixture.Source[0].Content != fixture.Command.Args[0] {
		t.Fatal("fixture source and command must be one identical exec program")
	}
	result, err := compat.Run(fixture)
	if err != nil || !result.OK {
		t.Fatalf("fixture run = %#v, %v", result, err)
	}
}

func TestAdditionalExceptionFamilyFixturesUseCurrentLocalContracts(t *testing.T) {
	for _, name := range []string{"core-runtime-exception-more-accessors.json", "core-runtime-exception-more-family-accessors.json", "core-runtime-exception-remaining-family-accessors.json", "current-base-invalid-parameter-value-fields-api67.json"} {
		t.Run(name, func(t *testing.T) {
			fixture, err := compat.LoadFile(filepath.Join("..", "..", "docs", "fixtures", name))
			if err != nil {
				t.Fatal(err)
			}
			if fixture.Command.Kind != "exec" || len(fixture.Source) != 1 || len(fixture.Command.Args) != 1 || fixture.Source[0].Content != fixture.Command.Args[0] {
				t.Fatal("fixture source and command must be one identical exec program")
			}
			result, err := compat.Run(fixture)
			if err != nil || !result.OK {
				t.Fatalf("fixture run = %#v, %v", result, err)
			}
		})
	}
}
