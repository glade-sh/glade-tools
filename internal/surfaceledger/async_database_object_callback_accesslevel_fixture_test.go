package surfaceledger

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

var asyncDatabaseObjectCallbackAccessLevelIDs = []string{
	"apex:System.Database.insertAsync(List<Object>,DataSource.AsyncSaveCallback,AccessLevel)",
	"apex:System.Database.insertAsync(Object,DataSource.AsyncSaveCallback,AccessLevel)",
	"apex:System.Database.updateAsync(List<Object>,DataSource.AsyncSaveCallback,AccessLevel)",
	"apex:System.Database.updateAsync(Object,DataSource.AsyncSaveCallback,AccessLevel)",
	"apex:System.Database.deleteAsync(List<Object>,DataSource.AsyncDeleteCallback,AccessLevel)",
	"apex:System.Database.deleteAsync(Object,DataSource.AsyncDeleteCallback,AccessLevel)",
}

var staleAsyncDatabaseObjectCallbackAccessLevelIDs = []string{
	"apex:System.Database.insertAsync(List<Object>,Object,AccessLevel)",
	"apex:System.Database.insertAsync(Object,Object,AccessLevel)",
	"apex:System.Database.updateAsync(List<Object>,Object,AccessLevel)",
	"apex:System.Database.updateAsync(Object,Object,AccessLevel)",
	"apex:System.Database.deleteAsync(List<Object>,Object,AccessLevel)",
	"apex:System.Database.deleteAsync(Object,Object,AccessLevel)",
}

func TestAsyncDatabaseObjectCallbackAccessLevelFixtureUsesCanonicalTypedCallbacks(t *testing.T) {
	fixturePath := filepath.Join("..", "..", "docs", "fixtures", "async-database-object-callback-accesslevel.json")
	fixture, err := compat.LoadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := BuildEvidenceSnapshot([]string{fixturePath})
	if err != nil {
		t.Fatal(err)
	}
	counts := make(map[string]int, len(evidence))
	for _, row := range evidence {
		counts[row.SurfaceID]++
	}
	for _, id := range staleAsyncDatabaseObjectCallbackAccessLevelIDs {
		if counts[id] != 0 {
			t.Errorf("stale Object callback fixture ID %s count = %d, want 0", id, counts[id])
		}
	}
	gladeByID := rowsByID(BuildGladeSnapshot())
	for _, id := range asyncDatabaseObjectCallbackAccessLevelIDs {
		if counts[id] != 1 {
			t.Errorf("typed callback fixture ID %s count = %d, want 1", id, counts[id])
		}
		if _, ok := gladeByID[id]; !ok {
			t.Errorf("missing canonical Glade snapshot row %s", id)
		}
	}
	source := fixtureSource(fixture)
	for _, token := range []string{
		"extends DataSource.AsyncSaveCallback",
		"extends DataSource.AsyncDeleteCallback",
		"override void processSave(Database.SaveResult result)",
		"override void processDelete(Database.DeleteResult result)",
		"DataSource.AsyncSaveCallback saveCallback",
		"DataSource.AsyncDeleteCallback deleteCallback",
	} {
		if !strings.Contains(source, token) {
			t.Errorf("fixture source is missing %q", token)
		}
	}
	if strings.Contains(source, "implements DataSource.Async") {
		t.Error("fixture source must extend the abstract callback classes")
	}
	if result, err := compat.Run(fixture); err != nil || !result.OK {
		t.Fatalf("fixture execution = %#v, error = %v", result, err)
	}
}
