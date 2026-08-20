package surfaceledger

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

const exceptionObjectMethodsFixture = "core-runtime-g3-exception-object-method-depth.json"

var exceptionObjectMethodsTypes = []string{
	"AsyncException",
	"Exception",
	"ExternalObjectException",
	"FatalCursorException",
	"FinalException",
	"FlowException",
	"FormulaEvaluationException",
	"FormulaValidationException",
	"HandledException",
	"InvalidReadOnlyUserDmlException",
	"JSONException",
	"LicenseException",
	"LimitException",
	"ListException",
	"MathException",
	"NoSuchElementException",
	"NullPointerException",
	"PlatformCacheException",
	"PolyglotException",
	"ProcedureException",
	"QueryException",
	"RequiredFeatureMissingException",
	"SObjectException",
	"SearchException",
	"SecurityException",
	"SerializationException",
	"StringException",
	"TransientCursorException",
	"TypeException",
	"UnexpectedException",
	"XmlException",
}

var exceptionObjectMethodsSkippedIDs = map[string]bool{
	"apex:System.AsyncException.equals(Object)":          true,
	"apex:System.AsyncException.hashCode()":              true,
	"apex:System.Exception.toString()":                   true,
	"apex:System.ExternalObjectException.equals(Object)": true,
	"apex:System.ExternalObjectException.hashCode()":     true,
}

func TestExceptionObjectMethodsHaveExactLocalFixtureOwnership(t *testing.T) {
	root := filepath.Join("..", "..")
	fixturePath := filepath.Join(root, "docs", "fixtures", exceptionObjectMethodsFixture)
	fixture, err := compat.LoadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Name != strings.TrimSuffix(exceptionObjectMethodsFixture, ".json") || fixture.Command.Kind != "exec" || len(fixture.Source) != 1 || len(fixture.Command.Args) != 1 || fixture.Source[0].Content != fixture.Command.Args[0] {
		t.Fatalf("fixture execution envelope = %#v", fixture)
	}

	wantIDs := make([]string, 0, len(exceptionObjectMethodsTypes)*3-len(exceptionObjectMethodsSkippedIDs))
	for _, typeName := range exceptionObjectMethodsTypes {
		ids := []string{
			"apex:System." + typeName + ".equals(Object)",
			"apex:System." + typeName + ".hashCode()",
			"apex:System." + typeName + ".toString()",
		}
		for _, id := range ids {
			if !exceptionObjectMethodsSkippedIDs[id] {
				wantIDs = append(wantIDs, id)
			}
		}
	}
	if len(fixture.Evidence) != len(wantIDs) {
		t.Fatalf("raw evidence rows = %d, want %d", len(fixture.Evidence), len(wantIDs))
	}
	evidence, err := BuildEvidenceSnapshot([]string{fixturePath})
	if err != nil {
		t.Fatal(err)
	}
	assertExactSurfaceSet(t, evidence, wantIDs)
	for _, row := range evidence {
		if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported {
			t.Fatalf("%s evidence/behavior = %s/%s, want fixture/supported", row.SurfaceID, row.Evidence, row.GladeBehavior)
		}
	}

	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	var policy struct {
		SalesforceEligible        *bool  `json:"salesforceEligible"`
		SalesforceExclusionClass  string `json:"salesforceExclusionClass"`
		SalesforceExclusionReason string `json:"salesforceExclusionReason"`
	}
	if err := json.Unmarshal(data, &policy); err != nil {
		t.Fatal(err)
	}
	if policy.SalesforceEligible == nil || *policy.SalesforceEligible || policy.SalesforceExclusionClass != "policy-local-only" || policy.SalesforceExclusionReason == "" {
		t.Fatalf("local-only metadata = %#v", policy)
	}

	source := fixture.Source[0].Content
	for _, typeName := range exceptionObjectMethodsTypes {
		if typeName == "Exception" {
			block := `{
  Exception value = new AsyncException('object-depth');
  System.assertEquals(true, value.equals(value));
  System.assertEquals(false, value.equals(null));
  System.assertEquals(value.hashCode(), value.hashCode());
  System.assertNotEquals(null, value.hashCode());
}`
			if !strings.Contains(source, block) {
				t.Fatal("source lacks concrete-subclass Object-method witness block for abstract Exception")
			}
			continue
		}
		block := fmt.Sprintf(`{
  %[1]s value = new %[1]s('object-depth');
  System.assertEquals(true, value.equals(value));
  System.assertEquals(false, value.equals(null));
  System.assertEquals(value.hashCode(), value.hashCode());
  System.assertNotEquals(null, value.hashCode());
  System.assertNotEquals(null, value.toString());
  System.assert(value.toString().contains('System.%[1]s'));
}`, typeName)
		if !strings.Contains(source, block) {
			t.Fatalf("source lacks complete direct Object-method witness block for %s", typeName)
		}
	}

	paths, err := filepath.Glob(filepath.Join(root, "docs", "fixtures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	counts := make(map[string]int, len(wantIDs))
	wantSet := mapFromIDs(wantIDs)
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var header struct {
			EvidenceOnly bool `json:"evidenceOnly"`
			Evidence     []struct {
				SurfaceID string `json:"surfaceId"`
			} `json:"evidence"`
		}
		if err := json.Unmarshal(data, &header); err != nil {
			t.Fatal(err)
		}
		if header.EvidenceOnly {
			continue
		}
		for _, item := range header.Evidence {
			if _, ok := wantSet[item.SurfaceID]; ok {
				counts[item.SurfaceID]++
			}
		}
	}
	for _, id := range wantIDs {
		if counts[id] != 1 {
			t.Fatalf("fixture ownership for %s = %d, want exactly one non-evidenceOnly owner", id, counts[id])
		}
	}

	ledger := Merge(nil, nil, BuildGladeSnapshot(), evidence)
	for _, id := range wantIDs {
		row, ok := rowsByID(ledger.Rows)[id]
		if !ok || row.GladeShape == ShapeAbsent || row.GladeBehavior != BehaviorSupported || row.Evidence != EvidenceFixture {
			t.Fatalf("merged %s row = %#v, want shaped fixture/supported", id, row)
		}
	}
	if result, err := compat.Run(fixture); err != nil || !result.OK {
		t.Fatalf("fixture execution = %#v, error = %v", result, err)
	}
}

func mapFromIDs(ids []string) map[string]bool {
	result := make(map[string]bool, len(ids))
	for _, id := range ids {
		result[id] = true
	}
	return result
}
