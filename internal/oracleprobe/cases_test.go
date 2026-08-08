package oracleprobe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadCasesReadsAndValidatesManifestArray(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cases.json")
	if err := os.WriteFile(path, []byte(`[{"id":"assert-true","area":"System.Assert","api":"System.Assert.isTrue","mode":"anonymous","surfaceIds":["apex:System.Assert.isTrue(Boolean)"],"expression":"true"}]`), 0o600); err != nil {
		t.Fatal(err)
	}

	cases, err := LoadCases(path)
	if err != nil {
		t.Fatalf("LoadCases: %v", err)
	}
	if len(cases) != 1 || cases[0].ID != "assert-true" || cases[0].Mode != ModeAnonymous {
		t.Fatalf("cases = %#v", cases)
	}
}

func TestLoadCasesRejectsUnknownManifestFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cases.json")
	if err := os.WriteFile(path, []byte(`[{"id":"assert-true","area":"System.Assert","api":"System.Assert.isTrue","mode":"anonymous","surfaceIds":["apex:System.Assert.isTrue(Boolean)"],"expression":"true","unexpected":true}]`), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadCases(path)
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("LoadCases error = %v, want unknown field", err)
	}
}

func TestValidateCasesRejectsUnsafeManifestValues(t *testing.T) {
	tests := []struct {
		name  string
		value Case
		want  string
	}{
		{name: "empty", value: Case{}, want: "nonempty"},
		{name: "duplicate id", value: Case{ID: "duplicate", Mode: ModeAnonymous, Expression: "true", SurfaceIDs: []string{"apex:System.Assert.isTrue(Boolean)"}}, want: "duplicate case ID"},
		{name: "wrong mode", value: Case{ID: "wrong-mode", Mode: ModeDeploy}, want: "anonymous"},
		{name: "empty area", value: Case{ID: "empty-area", API: "System.Assert.isTrue", Mode: ModeAnonymous, Expression: "true", SurfaceIDs: []string{"apex:System.Assert.isTrue(Boolean)"}}, want: "area"},
		{name: "empty API", value: Case{ID: "empty-api", Area: "System.Assert", Mode: ModeAnonymous, Expression: "true", SurfaceIDs: []string{"apex:System.Assert.isTrue(Boolean)"}}, want: "API"},
		{name: "empty expression", value: Case{ID: "empty-expression", Mode: ModeAnonymous}, want: "expression"},
		{name: "empty surface IDs", value: Case{ID: "empty-surfaces", Mode: ModeAnonymous, Expression: "true"}, want: "surface ID"},
		{name: "wildcard surface ID", value: Case{ID: "wildcard", Mode: ModeAnonymous, Expression: "true", SurfaceIDs: []string{"apex:System.Assert.*"}}, want: "canonical"},
		{name: "prose surface ID", value: Case{ID: "prose", Mode: ModeAnonymous, Expression: "true", SurfaceIDs: []string{"tested assertion"}}, want: "canonical"},
		{name: "duplicate surface ID", value: Case{ID: "duplicate-surface", Mode: ModeAnonymous, Expression: "true", SurfaceIDs: []string{"apex:System.Assert.isTrue(Boolean)", "apex:System.Assert.isTrue(Boolean)"}}, want: "duplicate surface ID"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name != "empty area" {
				tt.value.Area = "System.Assert"
			}
			if tt.name != "empty API" {
				tt.value.API = "System.Assert.isTrue"
			}
			cases := []Case{tt.value}
			if tt.name == "duplicate id" {
				cases = append(cases, tt.value)
			}
			err := ValidateCases(cases)
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tt.want)) {
				t.Fatalf("ValidateCases error = %v, want substring %q", err, tt.want)
			}
		})
	}
}
