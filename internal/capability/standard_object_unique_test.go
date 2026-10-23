package capability

import (
	"strings"
	"testing"
)

func TestStandardObjectCoverageBuilderRouteCountsCanonicalObjectsOnce(t *testing.T) {
	report := BuildStandardObjectCoverageReport()
	seen := make(map[string]bool)
	var want StandardObjectCoverageTotals
	for i, entry := range report.Objects {
		key := strings.ToLower(entry.Object)
		if seen[key] {
			t.Errorf("duplicate canonical object %q", entry.Object)
			continue
		}
		seen[key] = true
		if i > 0 && report.Objects[i-1].Object > entry.Object {
			t.Errorf("object rows are not sorted at %q", entry.Object)
		}
		want.Objects++
		want.ShapeObjects++
		if entry.Coverage == StandardObjectCoverageBehavior {
			want.BehaviorObjects++
		}
		if entry.KeyPrefix != "" {
			want.KeyPrefixes++
		}
		want.Fields += entry.Fields
		want.Relationships += entry.Relationships
		want.RecordTypes += entry.RecordTypes
		want.Picklists += entry.Picklists
		want.References += entry.References
	}
	if !seen["feedsignal"] {
		t.Error("case-aliased object was dropped instead of counted once")
	}
	if report.Totals != want {
		t.Errorf("totals = %+v, want unique-row totals %+v", report.Totals, want)
	}
}

func TestStubInventoryBuilderRouteCountsCanonicalObjectsOnce(t *testing.T) {
	coverage := BuildStandardObjectCoverageReport()
	seen := make(map[string]bool)
	var want StubInventoryActive
	for _, entry := range coverage.Objects {
		key := strings.ToLower(entry.Object)
		if seen[key] {
			continue
		}
		seen[key] = true
		want.StandardObjects++
		want.StandardObjectFields += entry.Fields
		want.StandardRelationships += entry.Relationships
		want.StandardRecordTypes += entry.RecordTypes
		if entry.KeyPrefix != "" {
			want.StandardKeyPrefixes++
		}
	}
	report, err := BuildStubInventoryReport("")
	if err != nil {
		t.Fatal(err)
	}
	got := report.Active
	// Feature-specific fields have a separate denominator.
	got.StandardObjectFieldsWithFeatures = 0
	if got != want {
		t.Errorf("inventory active totals = %+v, want unique-row totals %+v", got, want)
	}
}
