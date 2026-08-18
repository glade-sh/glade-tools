package surfaceledger

import (
	"path/filepath"
	"slices"
	"testing"
)

var limitsScheduledJobSurfaceIDs = []string{
	"apex:System.Limits.getScheduledJobs()",
	"apex:System.Limits.getLimitScheduledJobs()",
}

func TestLimitsScheduledJobSurfaceIDsAreCanonicalAcrossSnapshotAndFixtures(t *testing.T) {
	rows := BuildGladeSnapshot()
	assertLimitsScheduledJobSurfaceIDs(t, "Glade snapshot", rows)
	for _, id := range limitsScheduledJobSurfaceIDs {
		for _, row := range rows {
			if row.SurfaceID != id {
				continue
			}
			if row.ReturnType != "Integer" || row.GladeReturnType != "Integer" {
				t.Errorf("Glade snapshot %s return types = %q/%q, want Integer/Integer", id, row.ReturnType, row.GladeReturnType)
			}
			if !slices.Contains(row.Sources, "standard-symbols") || !slices.Contains(row.Sources, "stub-behavior") {
				t.Errorf("Glade snapshot %s sources = %v, want standard-symbols and stub-behavior", id, row.Sources)
			}
		}
	}

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
