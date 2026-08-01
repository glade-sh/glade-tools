package surfaceledger

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestBusinessHoursAPI67SignaturesHaveGapFreeEvidence(t *testing.T) {
	tests := []struct {
		name   string
		params []string
	}{
		{name: "add", params: []string{"Id", "Datetime", "Long"}},
		{name: "addGmt", params: []string{"Id", "Datetime", "Long"}},
		{name: "diff", params: []string{"String", "Datetime", "Datetime"}},
		{name: "isWithin", params: []string{"String", "Datetime"}},
		{name: "nextStartDate", params: []string{"Id", "Datetime"}},
	}

	gladeRows := BuildGladeSnapshot()
	assertNoBusinessHoursOppositeFirstParameterRows(t, gladeRows, tests, "Glade snapshot")
	gladeByID := rowsByID(gladeRows)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := ApexMemberID("", "BusinessHours", tt.name, tt.params)
			row, ok := gladeByID[id]
			if !ok {
				t.Fatalf("missing BusinessHours row %s", id)
			}
			if row.GladeShape != ShapeSignatureKnown || row.GladeBehavior != BehaviorSupported {
				t.Fatalf("%s shape/behavior = %s/%s, want signature-known/supported", id, row.GladeShape, row.GladeBehavior)
			}

		})
	}

	fixturesDir := filepath.Join("..", "..", "docs", "fixtures")
	paths := []string{
		filepath.Join(fixturesDir, "core-runtime-approval-businesshours-unsupported.json"),
		filepath.Join(fixturesDir, "core-runtime-businesshours-full-local-calendar.json"),
		filepath.Join(fixturesDir, "core-runtime-businesshours-license-local-evidence.json"),
		filepath.Join(fixturesDir, "core-runtime-metadata-backed-stdlib-closeout.json"),
	}
	evidence, err := BuildEvidenceSnapshot(paths)
	if err != nil {
		t.Fatal(err)
	}
	ledger := Merge(nil, nil, gladeRows, evidence)
	assertNoBusinessHoursOppositeFirstParameterRows(t, ledger.Rows, tests, "merged fixture ledger")
	mergedByID := rowsByID(ledger.Rows)
	for _, tt := range tests {
		id := ApexMemberID("", "BusinessHours", tt.name, tt.params)
		row, ok := mergedByID[id]
		if !ok {
			t.Fatalf("missing merged BusinessHours row %s", id)
		}
		if row.Evidence != EvidenceFixture || row.GapClass != "" {
			t.Fatalf("%s evidence/gap = %s/%s, want fixture/<empty>", id, row.Evidence, row.GapClass)
		}
	}
}

func assertNoBusinessHoursOppositeFirstParameterRows(t *testing.T, rows []SurfaceLedgerRow, tests []struct {
	name   string
	params []string
}, source string) {
	t.Helper()
	staleIDs := make(map[string]struct{}, len(tests))
	for _, tt := range tests {
		wrongFirstParameter := "Id"
		if tt.params[0] == "Id" {
			wrongFirstParameter = "String"
		}
		wrongParams := append([]string{wrongFirstParameter}, tt.params[1:]...)
		// Build the deliberately noncanonical identity directly. ApexMemberID
		// canonicalizes the stale docs String overloads to the API 67 Id shape.
		staleIDs["apex:BusinessHours."+tt.name+"("+strings.Join(wrongParams, ",")+")"] = struct{}{}
	}
	for _, row := range rows {
		if _, ok := staleIDs[row.SurfaceID]; ok {
			t.Fatalf("opposite first-parameter BusinessHours row %s is present in %s", row.SurfaceID, source)
		}
	}
}
