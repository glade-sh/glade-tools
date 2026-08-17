package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

var g3ExceptionInaccessibleFieldsIDs = []string{
	"apex:System.InvalidReadOnlyUserDmlException.getInaccessibleFields()",
	"apex:System.LicenseException.getInaccessibleFields()",
	"apex:System.LimitException.getInaccessibleFields()",
	"apex:System.ListException.getInaccessibleFields()",
	"apex:System.NoSuchElementException.getInaccessibleFields()",
	"apex:System.NullPointerException.getInaccessibleFields()",
	"apex:System.PlatformCacheException.getInaccessibleFields()",
	"apex:System.PolyglotException.getInaccessibleFields()",
	"apex:System.ProcedureException.getInaccessibleFields()",
	"apex:System.RequiredFeatureMissingException.getInaccessibleFields()",
	"apex:System.SObjectException.getInaccessibleFields()",
	"apex:System.SearchException.getInaccessibleFields()",
	"apex:System.SecurityException.getInaccessibleFields()",
	"apex:System.SerializationException.getInaccessibleFields()",
	"apex:System.StringException.getInaccessibleFields()",
	"apex:System.TransientCursorException.getInaccessibleFields()",
	"apex:System.TypeException.getInaccessibleFields()",
	"apex:System.UnexpectedException.getInaccessibleFields()",
	"apex:System.XmlException.getInaccessibleFields()",
}

func TestG3PrimaryFixturesKeepExactLocalEvidenceContracts(t *testing.T) {
	root := filepath.Join("..", "..")
	exceptionPath := filepath.Join(root, "docs", "fixtures", "core-runtime-g3-exception-inaccessible-fields.json")
	exceptionFixture, err := compat.LoadFile(exceptionPath)
	if err != nil {
		t.Fatal(err)
	}
	if exceptionFixture.Name != "core-runtime-g3-exception-inaccessible-fields" || exceptionFixture.Command.Kind != "exec" {
		t.Fatalf("exception fixture identity = %#v", exceptionFixture)
	}
	exceptionEvidence, err := BuildEvidenceSnapshot([]string{exceptionPath})
	if err != nil {
		t.Fatal(err)
	}
	assertExactSurfaceSet(t, exceptionEvidence, g3ExceptionInaccessibleFieldsIDs)
	assertFixtureOnlyEvidence(t, exceptionEvidence)
	assertG3LocalOnlyEligibility(t, exceptionPath)
	exceptionSource := fixtureSource(exceptionFixture)
	assertFixtureCommandMatchesSource(t, exceptionFixture, exceptionSource)
	if strings.Contains(strings.ToLower(exceptionSource), "oracle") || strings.Contains(strings.ToLower(exceptionSource), "salesforce") {
		t.Fatalf("exception source must remain local-only: %q", exceptionSource)
	}
	for _, id := range g3ExceptionInaccessibleFieldsIDs {
		typeName := strings.TrimSuffix(strings.TrimPrefix(id, "apex:System."), ".getInaccessibleFields()")
		call := regexp.MustCompile(`new\s+` + regexp.QuoteMeta(typeName) + `\s*\([^\n]*\)\.getInaccessibleFields\(\)`)
		if !call.MatchString(exceptionSource) {
			t.Fatalf("exception source is missing exact %s call", typeName)
		}
	}
	if strings.Count(exceptionSource, "catch (TypeException") != len(g3ExceptionInaccessibleFieldsIDs) || strings.Count(exceptionSource, "Procedure is only valid for System.QueryException") != len(g3ExceptionInaccessibleFieldsIDs) {
		t.Fatalf("exception source must assert the TypeException contract for every type")
	}
	if result, err := compat.Run(exceptionFixture); err != nil || !result.OK {
		t.Fatalf("exception fixture run = %#v, error = %v", result, err)
	}

	listPath := filepath.Join(root, "docs", "fixtures", "core-collection-stdlib-list-deepclone-options.json")
	listFixture, err := compat.LoadFile(listPath)
	if err != nil {
		t.Fatal(err)
	}
	if listFixture.Name != "core-collection-stdlib-list-deepclone-options" || listFixture.Command.Kind != "exec" {
		t.Fatalf("list fixture identity = %#v", listFixture)
	}
	listEvidence, err := BuildEvidenceSnapshot([]string{listPath})
	if err != nil {
		t.Fatal(err)
	}
	assertExactSurfaceSet(t, listEvidence, []string{"apex:System.List.deepClone(Boolean,Boolean,Boolean)"})
	assertFixtureOnlyEvidence(t, listEvidence)
	assertG3LocalOnlyEligibility(t, listPath)
	listSource := fixtureSource(listFixture)
	assertFixtureCommandMatchesSource(t, listFixture, listSource)
	if strings.Contains(listSource, "deepClone()") || !strings.Contains(listSource, ".deepClone(true, true, true)") || !strings.Contains(listSource, ".deepClone(false, false, false)") {
		t.Fatalf("list source must own only the three-Boolean deepClone form: %q", listSource)
	}
	for _, assertion := range []string{
		"System.assertEquals(original.Id, preserved.get(0).Id);",
		"System.assertEquals(null, cleared.get(0).Id);",
		"System.assertEquals('Acme', original.Name);",
		"System.assertEquals('Clone', preserved.get(0).Name);",
	} {
		if !strings.Contains(listSource, assertion) {
			t.Fatalf("list source is missing %q", assertion)
		}
	}
	if result, err := compat.Run(listFixture); err != nil || !result.OK {
		t.Fatalf("list fixture run = %#v, error = %v", result, err)
	}
}

func fixtureSource(fixture compat.Fixture) string {
	parts := make([]string, 0, len(fixture.Source))
	for _, source := range fixture.Source {
		parts = append(parts, source.Content)
	}
	return strings.Join(parts, "\n")
}

func assertFixtureCommandMatchesSource(t *testing.T, fixture compat.Fixture, source string) {
	t.Helper()
	if len(fixture.Source) != 1 || len(fixture.Command.Args) != 1 || fixture.Command.Args[0] != source {
		t.Fatalf("fixture source/command must be one identical local program: %#v", fixture)
	}
}

func assertFixtureOnlyEvidence(t *testing.T, rows []SurfaceLedgerRow) {
	t.Helper()
	for _, row := range rows {
		if row.Evidence != EvidenceFixture {
			t.Fatalf("%s evidence = %s, want fixture-only", row.SurfaceID, row.Evidence)
		}
	}
}

func assertG3LocalOnlyEligibility(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		SalesforceEligible        *bool  `json:"salesforceEligible"`
		SalesforceExclusionClass  string `json:"salesforceExclusionClass"`
		SalesforceExclusionReason string `json:"salesforceExclusionReason"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.SalesforceEligible == nil || *fixture.SalesforceEligible || fixture.SalesforceExclusionClass != "policy-local-only" || strings.TrimSpace(fixture.SalesforceExclusionReason) == "" {
		t.Fatalf("local-only eligibility metadata = %#v", fixture)
	}
}
