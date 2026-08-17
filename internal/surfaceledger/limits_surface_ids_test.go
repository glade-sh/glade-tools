package surfaceledger

import (
	"path/filepath"
	"testing"
)

var limitsScheduledJobSurfaceIDs = []string{
	"apex:System.Limits.getScheduledJobs()",
	"apex:System.Limits.getLimitScheduledJobs()",
}

func TestLimitsScheduledJobSurfaceIDsAreCanonicalAcrossSnapshotAndFixtures(t *testing.T) {
	assertLimitsScheduledJobSurfaceIDs(t, "Glade snapshot", BuildGladeSnapshot())

	root := filepath.Join("..", "..")
	for _, name := range []string{
		"current-base-system-003-local-runtime-api67.json",
		"limits-window-and-getters.json",
	} {
		path := filepath.Join(root, "docs", "fixtures", name)
		rows, err := BuildEvidenceSnapshot([]string{path})
		if err != nil {
			t.Fatal(err)
		}
		assertLimitsScheduledJobSurfaceIDs(t, name, rows)
	}
}

func assertLimitsScheduledJobSurfaceIDs(t *testing.T, source string, rows []SurfaceLedgerRow) {
	t.Helper()
	counts := map[string]int{}
	for _, row := range rows {
		counts[row.SurfaceID]++
	}
	for _, id := range limitsScheduledJobSurfaceIDs {
		if counts[id] != 1 {
			t.Errorf("%s %s count = %d, want 1", source, id, counts[id])
		}
		if bareID := id[:len(id)-2]; counts[bareID] != 0 {
			t.Errorf("%s retains unparenthesized ID %s", source, bareID)
		}
	}
}
