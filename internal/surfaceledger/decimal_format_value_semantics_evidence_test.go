package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func TestDecimalFormatFixtureMatchesSalesforceValueSemantics(t *testing.T) {
	const id = "apex:System.Decimal.format()"
	root := filepath.Join("..", "..", "docs", "fixtures")
	path := filepath.Join(root, "core-numeric-decimal-format-value-semantics.json")
	fixture, err := compat.LoadFile(path)
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
	paths, err := filepath.Glob(filepath.Join(root, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	owners := []string{}
	for _, candidatePath := range paths {
		data, err := os.ReadFile(candidatePath)
		if err != nil {
			t.Fatal(err)
		}
		var candidate struct {
			EvidenceOnly bool                     `json:"evidenceOnly"`
			Evidence     []compat.FixtureEvidence `json:"evidence"`
		}
		if err := json.Unmarshal(data, &candidate); err != nil {
			t.Fatal(err)
		}
		if candidate.EvidenceOnly {
			continue
		}
		for _, row := range candidate.Evidence {
			if row.SurfaceID == id || (row.SurfaceID == "" && row.Symbol == "Decimal.format()") {
				owners = append(owners, filepath.Base(candidatePath))
			}
		}
	}
	if len(owners) != 1 || owners[0] != filepath.Base(path) {
		t.Fatalf("%s owners = %v, want only %s", id, owners, filepath.Base(path))
	}
	rows, err := BuildEvidenceSnapshot([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].SurfaceID != id || rows[0].Evidence != EvidenceFixture || rows[0].GladeBehavior != BehaviorSupported {
		t.Fatalf("evidence rows = %#v", rows)
	}
}
