package surfaceledger

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

const milestone2PacketFixture = "milestone2-api-defaults-packet.json"

var milestone2PacketSurfaceIDs = []string{
	"unknown:milestone2-api66-body-caller66-database-default",
	"unknown:milestone2-api66-body-caller67-database-default",
	"unknown:milestone2-api67-body-caller66-database-default",
	"unknown:milestone2-api67-body-caller67-database-default",
	"unknown:milestone2-api66-sharing-default",
	"unknown:milestone2-api67-sharing-default",
	"unknown:milestone2-api67-trigger-database-user-mode",
	"unknown:milestone2-explicit-user-dml",
	"unknown:milestone2-explicit-system-dml",
	"unknown:milestone2-api66-multiline",
	"unknown:milestone2-api67-multiline",
}

func TestMilestone2PacketLedgerUsesExactVersionCases(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "fixtures", milestone2PacketFixture)
	fixture, err := compat.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Command.Kind != "policy-evidence" {
		t.Fatalf("command kind = %q, want policy-evidence", fixture.Command.Kind)
	}
	if len(fixture.Evidence) != len(milestone2PacketSurfaceIDs) {
		t.Fatalf("packet evidence rows = %d, want %d", len(fixture.Evidence), len(milestone2PacketSurfaceIDs))
	}
	want := make(map[string]bool, len(milestone2PacketSurfaceIDs))
	for _, id := range milestone2PacketSurfaceIDs {
		want[id] = true
	}
	for _, evidence := range fixture.Evidence {
		if !want[evidence.SurfaceID] {
			t.Fatalf("unexpected packet surface %q", evidence.SurfaceID)
		}
		if strings.ContainsAny(evidence.SurfaceID, "*?") || strings.Contains(strings.ToLower(evidence.Notes), "wildcard") {
			t.Fatalf("packet row %q uses wildcard credit", evidence.SurfaceID)
		}
		if !strings.Contains(evidence.Notes, "case=") {
			t.Fatalf("packet row %q lacks an exact case id", evidence.SurfaceID)
		}
	}
	rows, err := BuildEvidenceSnapshot([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != len(milestone2PacketSurfaceIDs) {
		t.Fatalf("ledger rows = %d, want %d", len(rows), len(milestone2PacketSurfaceIDs))
	}
}
