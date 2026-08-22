package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func TestDatetimeRuntimeOracleUsesPairedTimezoneOperations(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "fixtures", "core-datetime-runtime-depth.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var policy struct {
		Eligible *bool `json:"salesforceEligible"`
	}
	if err := json.Unmarshal(data, &policy); err != nil {
		t.Fatal(err)
	}
	fixture, err := compat.LoadData(data)
	if err != nil {
		t.Fatal(err)
	}
	if policy.Eligible == nil || !*policy.Eligible || len(fixture.Evidence) != 14 || len(fixture.Source) != 0 || fixture.Command.Kind != "exec" || len(fixture.Command.Args) != 1 {
		t.Fatalf("fixture contract = eligible %v, rows %d, sources %d, command %#v", policy.Eligible, len(fixture.Evidence), len(fixture.Source), fixture.Command)
	}

	program := fixture.Command.Args[0]
	for _, witness := range []string{
		"Datetime.newInstanceGmt(stamp.dateGMT(), stamp.timeGMT())",
		"Datetime.newInstance(localStamp.date(), localStamp.time())",
		"System.assertEquals(stamp, Datetime.valueOfGmt('2024-02-29 23:05:06'))",
		"stamp.format('yyyy-MM-dd', 'GMT')",
	} {
		if !strings.Contains(program, witness) {
			t.Errorf("fixture command lacks %q", witness)
		}
	}
	for _, invalid := range []string{
		"System.assertEquals(stamp.date(), stamp.dateGMT())",
		"Datetime.newInstanceGmt(stamp.dateGMT(), stamp.time())",
		"Datetime.valueOfGmt('2024-02-29 23:05:06'), Datetime.valueOf('2024-02-29 23:05:06')",
		"stamp.format('yyyy-MM-dd')",
	} {
		if strings.Contains(program, invalid) {
			t.Errorf("fixture command retains timezone-dependent contract %q", invalid)
		}
	}
	for _, row := range fixture.Evidence {
		if row.SurfaceID == "apex:System.Datetime.time()" {
			t.Fatal("fixture must not claim Datetime.time() evidence")
		}
	}
}
