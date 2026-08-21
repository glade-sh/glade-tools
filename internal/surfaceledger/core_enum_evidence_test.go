package surfaceledger

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func TestCoreEnumEvidenceFixtureIsExecutableAndShapeBacked(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "fixtures", "core-runtime-enum-families-evidence.json")
	fixture, err := compat.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if fixture.Name != "core-runtime-enum-families-evidence" {
		t.Fatalf("fixture name = %q", fixture.Name)
	}
	if fixture.Command.Kind != "test" {
		t.Fatalf("command kind = %q, want test", fixture.Command.Kind)
	}
	if len(fixture.Source) != 1 {
		t.Fatalf("source files = %d, want 1", len(fixture.Source))
	}

	wantIDs := coreEnumEvidenceIDs()
	for _, transferred := range []string{
		"apex:System.AccessType.CREATABLE",
		"apex:System.AccessType.UPDATABLE",
		"apex:System.Quiddity.AURA",
		"apex:System.Quiddity.QUICK_ACTION",
		"apex:System.Quiddity.VF",
		"apex:System.RoundingMode.CEILING",
		"apex:System.RoundingMode.FLOOR",
		"apex:System.RoundingMode.UNNECESSARY",
		"apex:System.RoundingMode.equals(Object)",
		"apex:System.RoundingMode.hashCode()",
		"apex:System.RoundingMode.ordinal()",
		"apex:System.RoundingMode.valueOf(String)",
		"apex:System.RoundingMode.values()",
	} {
		delete(wantIDs, transferred)
	}
	gotIDs := make(map[string]bool, len(fixture.Evidence))
	source := fixture.Source[0].Content
	for _, evidence := range fixture.Evidence {
		if evidence.Kind != "test" {
			t.Fatalf("%s kind = %q, want test", evidence.SurfaceID, evidence.Kind)
		}
		if !wantIDs[evidence.SurfaceID] {
			t.Fatalf("unexpected evidence ID %q", evidence.SurfaceID)
		}
		if gotIDs[evidence.SurfaceID] {
			t.Fatalf("duplicate evidence ID %q", evidence.SurfaceID)
		}
		gotIDs[evidence.SurfaceID] = true
		assertCoreEnumEvidenceSourceBacked(t, source, evidence.SurfaceID)
	}
	if len(gotIDs) != len(wantIDs) {
		t.Fatalf("evidence IDs = %d, want %d", len(gotIDs), len(wantIDs))
	}
	for id := range wantIDs {
		if !gotIDs[id] {
			t.Fatalf("missing evidence ID %q", id)
		}
	}

	evidenceRows, err := BuildEvidenceSnapshot([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(evidenceRows) != len(gotIDs) {
		t.Fatalf("evidence snapshot rows = %d, want %d", len(evidenceRows), len(gotIDs))
	}
	evidenceRowsByID := rowsByID(evidenceRows)
	for id := range wantIDs {
		row, ok := evidenceRowsByID[id]
		if !ok {
			t.Fatalf("declared evidence ID %q is absent from the evidence snapshot", id)
		}
		if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported {
			t.Fatalf("%s evidence/behavior = %s/%s, want fixture/supported", row.SurfaceID, row.Evidence, row.GladeBehavior)
		}
	}

	shapeRows := rowsByID(BuildGladeSnapshot())
	for id := range wantIDs {
		row, ok := shapeRows[id]
		if !ok {
			t.Fatalf("evidence ID %q is absent from the checked Glade shape", id)
		}
		if row.GladeShape == ShapeAbsent || row.GladeBehavior != BehaviorSupported {
			t.Fatalf("%s shape/behavior = %s/%s, want present/supported", id, row.GladeShape, row.GladeBehavior)
		}
	}

	for _, id := range []string{
		"apex:System.LoggingLevel.INFO",
		"apex:System.TriggerOperation.BEFORE_INSERT",
		"apex:System.JSONToken.END_OBJECT",
		"apex:System.RoundingMode.HALF_UP",
		"apex:System.AccessType.READABLE",
		"apex:System.ParentJobResult.SUCCESS",
		"apex:System.Quiddity.REMOTE_ACTION",
	} {
		if !gotIDs[id] {
			t.Fatalf("representative constant %q is not evidenced", id)
		}
	}
	assertCoreEnumOrderAndOrdinals(t, source)
}

func assertCoreEnumOrderAndOrdinals(t *testing.T, source string) {
	t.Helper()
	expectedOrder := map[string][]string{
		"LoggingLevel": {
			"NONE", "INTERNAL", "FINEST", "FINER", "FINE", "DEBUG", "INFO", "WARN", "ERROR",
		},
		"TriggerOperation": {
			"BEFORE_INSERT", "AFTER_INSERT", "BEFORE_UPDATE", "AFTER_UPDATE", "BEFORE_DELETE", "AFTER_DELETE", "AFTER_UNDELETE",
		},
		"JSONToken": {
			"NOT_AVAILABLE", "START_OBJECT", "END_OBJECT", "START_ARRAY", "END_ARRAY", "FIELD_NAME", "VALUE_EMBEDDED_OBJECT", "VALUE_STRING", "VALUE_NUMBER_INT", "VALUE_NUMBER_FLOAT", "VALUE_TRUE", "VALUE_FALSE", "VALUE_NULL",
		},
		"RoundingMode": {
			"UP", "DOWN", "CEILING", "FLOOR", "HALF_UP", "HALF_DOWN", "HALF_EVEN", "UNNECESSARY",
		},
		"AccessType":      {"CREATABLE", "READABLE", "UPDATABLE", "UPSERTABLE"},
		"ParentJobResult": {"SUCCESS", "UNHANDLED_EXCEPTION"},
		"Quiddity": {
			"BATCH_ACS", "BATCH_CHUNK_SERIAL", "BATCH_CHUNK_PARALLEL", "FUTURE", "SCHEDULED", "SYNCHRONOUS", "RUN_INTEGRATION_TESTS", "RUNTEST_SYNC", "RUNTEST_ASYNC", "RUNTEST_DEPLOY", "VF", "QUEUEABLE", "REMOTE_ACTION", "AURA", "QUICK_ACTION", "BULK_API", "SOAP", "REST", "INVOCABLE_ACTION", "ANONYMOUS", "INBOUND_EMAIL_SERVICE", "BATCH_APEX", "DISCOVERABLE_LOGIN", "IOT", "COMMERCE_INTEGRATION", "TRANSACTION_FINALIZER_QUEUEABLE", "FUNCTION_CALLBACK", "POST_INSTALL_SCRIPT", "PLATFORM_EVENT_PUBLISH_CALLBACK", "EXTERNAL_SERVICE_CALLBACK", "TRANSACTION_SECURITY_POLICY", "UNDEFINED",
		},
	}
	for typeName, names := range expectedOrder {
		for ordinal, name := range names {
			assertSourceContains(t, source, "System.assertEquals("+typeName+"."+name+", declared["+strconv.Itoa(ordinal)+"]);")
		}
	}

	expectedOrdinals := map[string]struct {
		name    string
		ordinal int
	}{
		"LoggingLevel":     {name: "INFO", ordinal: 6},
		"TriggerOperation": {name: "BEFORE_INSERT", ordinal: 0},
		"JSONToken":        {name: "END_OBJECT", ordinal: 2},
		"RoundingMode":     {name: "HALF_UP", ordinal: 4},
		"AccessType":       {name: "CREATABLE", ordinal: 0},
		"ParentJobResult":  {name: "SUCCESS", ordinal: 0},
		"Quiddity":         {name: "REMOTE_ACTION", ordinal: 12},
	}
	for typeName, expected := range expectedOrdinals {
		assertSourceContains(t, source, "System.assertEquals("+strconv.Itoa(expected.ordinal)+", "+typeName+"."+expected.name+".ordinal());")
	}
}

func assertSourceContains(t *testing.T, source, expected string) {
	t.Helper()
	if !strings.Contains(source, expected) {
		t.Fatalf("fixture source is missing %q", expected)
	}
}

func coreEnumEvidenceIDs() map[string]bool {
	constants := map[string][]string{
		"LoggingLevel": {
			"DEBUG", "ERROR", "FINE", "FINER", "FINEST", "INFO", "INTERNAL", "NONE", "WARN",
		},
		"TriggerOperation": {
			"AFTER_DELETE", "AFTER_INSERT", "AFTER_UNDELETE", "AFTER_UPDATE",
			"BEFORE_DELETE", "BEFORE_INSERT", "BEFORE_UPDATE",
		},
		"JSONToken": {
			"END_ARRAY", "END_OBJECT", "FIELD_NAME", "NOT_AVAILABLE", "START_ARRAY", "START_OBJECT",
			"VALUE_EMBEDDED_OBJECT", "VALUE_FALSE", "VALUE_NULL", "VALUE_NUMBER_FLOAT", "VALUE_NUMBER_INT", "VALUE_STRING", "VALUE_TRUE",
		},
		"RoundingMode": {
			"CEILING", "DOWN", "FLOOR", "HALF_DOWN", "HALF_EVEN", "HALF_UP", "UNNECESSARY", "UP",
		},
		"AccessType":      {"CREATABLE", "READABLE", "UPDATABLE", "UPSERTABLE"},
		"ParentJobResult": {"SUCCESS", "UNHANDLED_EXCEPTION"},
		"Quiddity": {
			"ANONYMOUS", "AURA", "BATCH_ACS", "BATCH_APEX", "BATCH_CHUNK_PARALLEL", "BATCH_CHUNK_SERIAL",
			"BULK_API", "COMMERCE_INTEGRATION", "DISCOVERABLE_LOGIN", "EXTERNAL_SERVICE_CALLBACK", "FUNCTION_CALLBACK",
			"FUTURE", "INBOUND_EMAIL_SERVICE", "INVOCABLE_ACTION", "IOT", "PLATFORM_EVENT_PUBLISH_CALLBACK",
			"POST_INSTALL_SCRIPT", "QUEUEABLE", "QUICK_ACTION", "REMOTE_ACTION", "REST", "RUNTEST_ASYNC",
			"RUNTEST_DEPLOY", "RUNTEST_SYNC", "RUN_INTEGRATION_TESTS", "SCHEDULED", "SOAP", "SYNCHRONOUS",
			"TRANSACTION_FINALIZER_QUEUEABLE", "TRANSACTION_SECURITY_POLICY", "UNDEFINED", "VF",
		},
	}
	methods := map[string]map[string][]string{
		"values":   {},
		"valueOf":  {"String": {}},
		"equals":   {"Object": {}},
		"hashCode": {},
		"ordinal":  {},
	}
	ids := make(map[string]bool)
	for typeName, names := range constants {
		for _, name := range names {
			ids[ApexMemberID("System", typeName, name, nil)] = true
		}
		for method, parameters := range methods {
			var params []string
			for parameterType := range parameters {
				params = []string{parameterType}
			}
			ids[ApexMemberID("System", typeName, method, paramsOrEmpty(params))] = true
		}
	}
	return ids
}

func paramsOrEmpty(params []string) []string {
	if params == nil {
		return []string{}
	}
	return params
}

func assertCoreEnumEvidenceSourceBacked(t *testing.T, source, surfaceID string) {
	t.Helper()
	rest := strings.TrimPrefix(surfaceID, "apex:System.")
	parts := strings.SplitN(rest, ".", 2)
	if len(parts) != 2 {
		t.Fatalf("unexpected core enum surface ID %q", surfaceID)
	}
	typeName, member := parts[0], parts[1]
	if open := strings.IndexByte(member, '('); open >= 0 {
		method := member[:open]
		if method == "values" || method == "valueOf" {
			assertSourceContains(t, source, typeName+"."+method+"(")
			return
		}
		for _, line := range strings.Split(source, "\n") {
			if strings.Contains(line, typeName+".") && strings.Contains(line, "."+method+"(") {
				return
			}
		}
		t.Fatalf("%s is not visibly executed by %s source", surfaceID, typeName)
		return
	}
	marker := typeName + "." + member
	if !strings.Contains(source, marker) {
		t.Fatalf("%s is not visibly referenced by fixture source; missing %q", surfaceID, marker)
	}
}
