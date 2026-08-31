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
	for _, name := range []string{"core-runtime-exception-more-accessors.json", "core-runtime-exception-more-family-accessors.json", "core-runtime-exception-remaining-family-accessors.json", "core-runtime-g3-exception-object-method-depth.json", "current-base-invalid-parameter-value-fields-api67.json"} {
		t.Run(name, func(t *testing.T) {
			fixture, err := compat.LoadFile(filepath.Join("..", "..", "docs", "fixtures", name))
			if err != nil {
				t.Fatal(err)
			}
			if fixture.Command.Kind != "exec" || len(fixture.Source) != 1 || len(fixture.Command.Args) != 1 || fixture.Source[0].Content != fixture.Command.Args[0] {
				t.Fatal("fixture source and command must be one identical exec program")
			}
			if name == "core-runtime-g3-exception-object-method-depth.json" && !strings.Contains(fixture.Source[0].Content, "new NullPointerException()") {
				t.Fatal("NullPointerException must use its current zero-argument constructor")
			}
			result, err := compat.Run(fixture)
			if err != nil || !result.OK {
				t.Fatalf("fixture run = %#v, %v", result, err)
			}
		})
	}
}

func TestInstallContextFixtureUsesCurrentInstallerIdentity(t *testing.T) {
	fixture, err := compat.LoadFile(filepath.Join("..", "..", "docs", "fixtures", "core-runtime-install-context-accessors.json"))
	if err != nil {
		t.Fatal(err)
	}
	if fixture.Command.Kind != "test" || len(fixture.Source) != 2 {
		t.Fatal("install context fixture must remain a two-class test project")
	}
	source := fixture.Source[0].Content
	if !strings.Contains(source, "context.installerId() == UserInfo.getUserId()") || strings.Contains(source, "context.installerId() == null") {
		t.Fatal("install context fixture must expect the current installer identity")
	}
	result, err := compat.Run(fixture)
	if err != nil || !result.OK {
		t.Fatalf("fixture run = %#v, %v", result, err)
	}
}

func TestInstallHandlerFixtureUsesCurrentInstallerIdentity(t *testing.T) {
	fixture, err := compat.LoadFile(filepath.Join("..", "..", "docs", "fixtures", "current-base-system-002-local-runtime-api67.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(fixture.Source) != 2 || !strings.Contains(fixture.Source[0].Content, "context.installerId() == UserInfo.getUserId()") || strings.Contains(fixture.Source[0].Content, "context.installerId() == null") {
		t.Fatal("install handler fixture must expect the current installer identity")
	}
}

func TestUserProvisioningFixtureConstructsFlowBaseThroughConcreteSubclass(t *testing.T) {
	fixture, err := compat.LoadFile(filepath.Join("..", "..", "docs", "fixtures", "current-base-userprovisioning-deterministic-mock-002-api67.json"))
	if err != nil {
		t.Fatal(err)
	}
	var source strings.Builder
	for _, file := range fixture.Source {
		source.WriteString(file.Content)
	}
	if strings.Contains(source.String(), "new UserProvisioning.FlowProvisionBase(") || !strings.Contains(source.String(), "extends UserProvisioning.FlowProvisionBase") || !strings.Contains(source.String(), "super('');") {
		t.Fatal("FlowProvisionBase fixture must exercise its constructor through a concrete subclass")
	}
}

func TestConvertLeadFixtureBindsResultIDsThroughLocals(t *testing.T) {
	fixture, err := compat.LoadFile(filepath.Join("..", "..", "docs", "fixtures", "data-database-convert-lead-runtime.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(fixture.Source) != 1 || len(fixture.Command.Args) != 1 || fixture.Source[0].Content != fixture.Command.Args[0] {
		t.Fatal("convertLead source and command must remain identical")
	}
	source := fixture.Source[0].Content
	if strings.Contains(source, ":r0.getAccountId()") || strings.Contains(source, ":r0.getContactId()") || !strings.Contains(source, "Id accountId0 = r0.getAccountId();") || !strings.Contains(source, "Id contactId0 = r0.getContactId();") {
		t.Fatal("convertLead query binds must use declared Id locals")
	}
}

func TestDMLOptionsFixtureBindsUpdatedIDsThroughLocalSet(t *testing.T) {
	fixture, err := compat.LoadFile(filepath.Join("..", "..", "docs", "fixtures", "data-database-dmloptions-runtime.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(fixture.Source) != 1 || len(fixture.Command.Args) != 1 || fixture.Source[0].Content != fixture.Command.Args[0] {
		t.Fatal("DML options source and command must remain identical")
	}
	source := fixture.Source[0].Content
	if strings.Contains(source, "IN :new Set<Id>") || !strings.Contains(source, "Set<Id> updatedIds=new Set<Id>") || !strings.Contains(source, "WHERE Id IN :updatedIds") {
		t.Fatal("DML options query must bind a declared Set<Id>")
	}
}

func TestQuickActionDefaultsFixtureImplementsHandlerInterface(t *testing.T) {
	fixture, err := compat.LoadFile(filepath.Join("..", "..", "docs", "fixtures", "core-runtime-local-service-evidence-closeout.json"))
	if err != nil {
		t.Fatal(err)
	}
	var source strings.Builder
	for _, file := range fixture.Source {
		source.WriteString(file.Content)
	}
	if !strings.Contains(source.String(), "public class CoreRuntimeLocalQuickActionDefaultsHandler implements QuickAction.QuickActionDefaultsHandler") || strings.Contains(source.String(), "new QuickAction.QuickActionDefaultsHandler()") {
		t.Fatal("QuickAction defaults fixture must implement, not instantiate, the documented handler interface")
	}
}

func TestCurrentMessagingFixturesUseAPI67(t *testing.T) {
	for _, name := range []string{"core-runtime-messaging-notification-handler-evidence.json", "core-runtime-messaging-send-capture-full-local.json"} {
		fixture, err := compat.LoadFile(filepath.Join("..", "..", "docs", "fixtures", name))
		if err != nil {
			t.Fatal(err)
		}
		if fixture.APIVersion != "67.0" {
			t.Fatalf("%s API version = %q, want 67.0", name, fixture.APIVersion)
		}
		if name == "core-runtime-messaging-send-capture-full-local.json" {
			source := fixture.Source[0].Content
			if strings.Contains(source, "Messaging.SendEmailOptions") || !strings.Contains(source, "Messaging.sendEmail(new List<Messaging.Email>") {
				t.Fatal("sendEmail fixture must use the documented Messaging.Email list without local-only SendEmailOptions")
			}
		}
	}
}

func TestSystemLocalControlFixtureUsesLocalRequestVersion(t *testing.T) {
	fixture, err := compat.LoadFile(filepath.Join("..", "..", "docs", "fixtures", "core-runtime-system-local-control.json"))
	if err != nil {
		t.Fatal(err)
	}
	if fixture.Project.SourceAPIVersion != "67.0" {
		t.Fatalf("system local control source API version = %q, want 67.0", fixture.Project.SourceAPIVersion)
	}
	source := fixture.Source[0].Content
	if !strings.Contains(source, "System.assertEquals('65.0.0', System.requestVersion().toString())") || strings.Contains(source, "System.assertEquals('67.0.0', System.requestVersion().toString())") {
		t.Fatal("system local control fixture must keep request version separate from source API version")
	}
}

func TestPageReferenceResourceFixtureDeclaresStaticResource(t *testing.T) {
	fixture, err := compat.LoadFile(filepath.Join("..", "..", "docs", "fixtures", "core-runtime-page-reference-resource-evidence.json"))
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string]bool{}
	for _, file := range fixture.Source {
		paths[file.Path] = true
	}
	for _, path := range []string{"force-app/main/default/staticresources/Images.resource", "force-app/main/default/staticresources/Images.resource-meta.xml"} {
		if !paths[path] {
			t.Fatalf("fixture source missing %s", path)
		}
	}
}

func TestSelectOptionFixtureUsesDocumentedConstructor(t *testing.T) {
	fixture, err := compat.LoadFile(filepath.Join("..", "..", "docs", "fixtures", "core-runtime-select-option-accessors.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(fixture.Source) != 1 || len(fixture.Command.Args) != 1 || fixture.Source[0].Content != fixture.Command.Args[0] {
		t.Fatal("SelectOption source and command must remain identical")
	}
	source := fixture.Source[0].Content
	if !strings.Contains(source, "new SelectOption('1', 'One', true)") || strings.Contains(source, "new SelectOption('1', 'One', true, false)") {
		t.Fatal("SelectOption fixture must use its documented three-argument constructor")
	}
	if !strings.Contains(source, "option.setEscapeItem(false);") {
		t.Fatal("SelectOption fixture must explicitly initialize the non-default escape state")
	}
}

func TestSmallHelperFixtureUsesStaticBlobToPdf(t *testing.T) {
	fixture, err := compat.LoadFile(filepath.Join("..", "..", "docs", "fixtures", "core-runtime-small-helper-value-objects.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(fixture.Source) != 1 || len(fixture.Command.Args) != 1 || fixture.Source[0].Content != fixture.Command.Args[0] {
		t.Fatal("small helper source and command must remain identical")
	}
	source := fixture.Source[0].Content
	if !strings.Contains(source, "Blob.toPdf('stub')") || strings.Contains(source, "Blob.valueOf('glade').toPdf('stub')") {
		t.Fatal("Blob.toPdf fixture must use the documented static call")
	}
}

func TestStringCompletionFixtureUsesInstanceLevenshteinDistance(t *testing.T) {
	fixture, err := compat.LoadFile(filepath.Join("..", "..", "docs", "fixtures", "core-string-completion-stdlib.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(fixture.Source) != 1 || len(fixture.Command.Args) != 1 || fixture.Source[0].Content != fixture.Command.Args[0] {
		t.Fatal("string completion source and command must remain identical")
	}
	source := fixture.Source[0].Content
	if !strings.Contains(source, "kitten.getLevenshteinDistance('sitting')") || strings.Contains(source, "String.getLevenshteinDistance('kitten', 'sitting')") {
		t.Fatal("Levenshtein fixture must use the documented instance method")
	}
	if !strings.Contains(source, "String upperOmega = String.fromCharArray(new List<Integer>{937});") ||
		!strings.Contains(source, "String lowerOmega = String.fromCharArray(new List<Integer>{969});") ||
		strings.ContainsAny(source, "Ωω") {
		t.Fatal("string completion fixture must construct Omega characters without raw non-ASCII Apex literals")
	}
}

func TestStringMoreFixtureUsesCaseDistinctLocals(t *testing.T) {
	fixture, err := compat.LoadFile(filepath.Join("..", "..", "docs", "fixtures", "core-string-more-stdlib.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(fixture.Source) != 1 || len(fixture.Command.Args) != 1 || fixture.Source[0].Content != fixture.Command.Args[0] {
		t.Fatal("string more source and command must remain identical")
	}
	if strings.Contains(fixture.Source[0].Content, "String ABCDE =") {
		t.Fatal("Apex local names are case-insensitive; uppercase witness must not redeclare abcde")
	}
}

func TestLimitsFixtureUsesPaginationCursorRowLimit(t *testing.T) {
	fixture, err := compat.LoadFile(filepath.Join("..", "..", "docs", "fixtures", "core-system-limits-exact-evidence.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(fixture.Source) != 1 || len(fixture.Command.Args) != 1 || fixture.Source[0].Content != fixture.Command.Args[0] {
		t.Fatal("limits source and command must remain identical")
	}
	if !strings.Contains(fixture.Source[0].Content, "System.assertEquals(100000, Limits.getLimitApexPaginationCursorRows())") {
		t.Fatal("limits fixture must use the current pagination cursor row limit")
	}
}

func TestSystemStringFixtureUsesAnonymousApexSource(t *testing.T) {
	fixture, err := compat.LoadFile(filepath.Join("..", "..", "docs", "fixtures", "core-system-string-exact-evidence.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(fixture.Source) != 1 || fixture.Source[0].Path != "anonymous.apex" {
		t.Fatalf("source = %#v, want one anonymous.apex source", fixture.Source)
	}
}

func TestPaginationUpdatedFixtureUsesAPI67(t *testing.T) {
	fixture, err := compat.LoadFile(filepath.Join("..", "..", "docs", "fixtures", "data-database-pagination-updated-runtime.json"))
	if err != nil {
		t.Fatal(err)
	}
	if fixture.APIVersion != "67.0" {
		t.Fatalf("pagination updated API version = %q, want 67.0", fixture.APIVersion)
	}
}
