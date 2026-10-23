package capability

import (
	"testing"

	"github.com/glade-sh/glade/internal/storage"
)

func TestStandardObjectCoverageBuilderAliasRoutePreservesCanonicalMetadata(t *testing.T) {
	definition, ok := storage.StandardObjectDefinition("FeedSignal")
	if !ok {
		t.Fatal("alias definition missing")
	}
	canonical, ok := storage.StandardObjectDefinition("feedSignal")
	if !ok || definition.APIName != canonical.APIName {
		t.Fatal("lookup aliases no longer share a canonical definition")
	}
	count := 0
	for _, entry := range BuildStandardObjectCoverageReport().Objects {
		if entry.Object != canonical.APIName {
			continue
		}
		count++
		if entry.Label != canonical.Label || entry.PluralLabel != canonical.PluralLabel ||
			entry.KeyPrefix != canonical.KeyPrefix || entry.Fields != len(canonical.Fields) ||
			entry.Relationships != len(canonical.Relations) || entry.RecordTypes != len(canonical.RecordTypes) {
			t.Errorf("canonical metadata changed: %+v", entry)
		}
	}
	if count != 1 {
		t.Fatalf("canonical definition appears %d times, want once", count)
	}
}
