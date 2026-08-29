package surfaceledger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/apexdocs"
)

func TestMergeExcludesReviewedLegacyCacheStatisticsAtAPI67(t *testing.T) {
	inventory, err := apexdocs.ReadInventory(filepath.Join("..", "..", "docs", "fixtures", "salesforce-docs-inventory-summer-26.json"))
	if err != nil {
		t.Fatal(err)
	}

	legacyDocs := 0
	for _, doc := range inventory.Documents {
		if !strings.EqualFold(doc.Namespace, "cache") {
			continue
		}
		for _, member := range doc.Members {
			if member.Name != "getAvgValueSize" && member.Name != "getMaxValueSize" {
				continue
			}
			legacyDocs++
			if !strings.Contains(member.Description, "available only in API versions 49.0 and earlier") {
				t.Fatalf("%s.%s lacks the reviewed API boundary: %q", doc.Name, member.Name, member.Description)
			}
		}
		if doc.Name == "Org" {
			for _, member := range doc.Members {
				if member.Name == "isAvailable" {
					t.Fatal("Cache.Org.isAvailable unexpectedly exists in the reviewed current docs")
				}
			}
		}
	}
	if legacyDocs != 6 {
		t.Fatalf("reviewed legacy Cache statistic declarations = %d, want 6", legacyDocs)
	}

	ids := []string{
		"apex:Cache.Org.getAvgValueSize()",
		"apex:Cache.Org.getMaxValueSize()",
		"apex:Cache.Org.isAvailable()",
		"apex:Cache.OrgPartition.getAvgValueSize()",
		"apex:Cache.OrgPartition.getMaxValueSize()",
		"apex:Cache.Partition.getAvgValueSize()",
		"apex:Cache.Partition.getMaxValueSize()",
		"apex:Cache.Session.getAvgValueSize()",
		"apex:Cache.Session.getMaxValueSize()",
		"apex:Cache.SessionPartition.getAvgValueSize()",
		"apex:Cache.SessionPartition.getMaxValueSize()",
	}
	rows := make([]SurfaceLedgerRow, 0, len(ids))
	for _, id := range ids {
		rows = append(rows, SurfaceLedgerRow{SurfaceID: id, Product: ProductApex})
	}
	if got := Merge(rows, rows, rows, rows).Rows; len(got) != 0 {
		t.Fatalf("legacy Cache rows entered the API-67 ledger: %#v", got)
	}
}

func TestMergeExcludesAPI67RemovedSiteHelpers(t *testing.T) {
	rows := Merge(
		[]SurfaceLedgerRow{{SurfaceID: "apex:System.Site.getPrefix", Product: ProductApex}},
		[]SurfaceLedgerRow{{SurfaceID: "apex:System.Site.getPrefix()", Product: ProductApex}},
		nil,
		nil,
	).Rows
	for _, row := range rows {
		if isAPI67RemovedSurfaceID(row.SurfaceID) {
			t.Fatalf("removed Site helper entered current-base ledger: %s", row.SurfaceID)
		}
	}
}

func TestMergeExcludesAPI67LegacyDeleteFilterAndStatusAlias(t *testing.T) {
	ids := []string{
		"apex:Database.DeleteFilter",
		"apex:Database.DeleteFilter.DELETED_ROWS_ONLY",
		"apex:Database.DeleteFilter.NO_DELETED_ROWS",
		"apex:Database.DeleteFilter.NO_DELETED_SHARING_ROWS",
		"apex:Database.DeleteFilter.NO_FILTER",
		"apex:Database.DeleteFilter.equals(Object)",
		"apex:Database.DeleteFilter.hashCode()",
		"apex:Database.DeleteFilter.ordinal()",
		"apex:Database.DeleteFilter.valueOf(String)",
		"apex:Database.DeleteFilter.values()",
		"apex:Metadata.DeployStatus.IN_PROGRESS",
		"apex:Schema.SObjectTypeFieldSets.get(String)",
	}
	rows := make([]SurfaceLedgerRow, 0, len(ids))
	for _, id := range ids {
		rows = append(rows, SurfaceLedgerRow{SurfaceID: id, Product: ProductApex})
	}
	merged := Merge(rows, nil, nil, nil).Rows
	if len(merged) != 0 {
		t.Fatalf("API-67 removed surfaces entered current-base ledger: %#v", merged)
	}
	for _, row := range merged {
		if isAPI67RemovedSurfaceID(row.SurfaceID) {
			t.Fatalf("API-67 removed surface entered current-base ledger: %s", row.SurfaceID)
		}
	}
}

func TestBuildEvidenceSnapshotExcludesAPI67AbsentSObjectTypeFieldSetsGet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schema-fieldsets-get.json")
	data := []byte(`{"name":"schema-fieldsets-get","command":{"kind":"policy-evidence"},"evidence":[{"kind":"unsupported","surfaceId":"apex:Schema.SObjectTypeFieldSets.get(String)","symbol":"Schema.SObjectTypeFieldSets.get(String)"}]}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	rows, err := BuildEvidenceSnapshot([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("API-67 absent Schema row entered evidence snapshot: %#v", rows)
	}
}

func TestMergeExcludesAPI67InvalidDatabaseAllowCalloutsRows(t *testing.T) {
	ids := []string{
		"apex:System.Database.insertAsync(Object,Database.AllowCallouts,AccessLevel)",
		"apex:System.Database.insertAsync(List<Object>,Database.AllowCallouts,AccessLevel)",
		"apex:System.Database.updateAsync(Object,Database.AllowCallouts,AccessLevel)",
		"apex:System.Database.updateAsync(List<Object>,Database.AllowCallouts,AccessLevel)",
		"apex:System.Database.deleteAsync(Object,Database.AllowCallouts,AccessLevel)",
		"apex:System.Database.deleteAsync(List<Object>,Database.AllowCallouts,AccessLevel)",
	}
	rows := make([]SurfaceLedgerRow, 0, len(ids))
	for _, id := range ids {
		rows = append(rows, SurfaceLedgerRow{SurfaceID: id, Product: ProductApex})
	}
	merged := Merge(rows, rows, rows, rows)
	if len(merged.Rows) != 0 {
		t.Fatalf("invalid API67 Database.AllowCallouts rows entered current-base ledger: %#v", merged.Rows)
	}
}

func TestMergeExcludesAPI67InvalidSystemDebugObjectObjectRow(t *testing.T) {
	rows := []SurfaceLedgerRow{{
		SurfaceID: "apex:System.System.debug(Object,Object)",
		Product:   ProductApex,
	}}
	merged := Merge(rows, rows, rows, rows)
	if len(merged.Rows) != 0 {
		t.Fatalf("invalid API67 System.debug(Object,Object) row entered current-base ledger: %#v", merged.Rows)
	}
}

func TestBuildEvidenceSnapshotExcludesAPI67InvalidDatabaseAllowCalloutsRows(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "fixtures", "async-database-allowcallouts-unsupported.json")
	rows, err := BuildEvidenceSnapshot([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if isAPI67RemovedSurfaceID(row.SurfaceID) {
			t.Fatalf("invalid API67 Database.AllowCallouts row entered evidence snapshot: %s", row.SurfaceID)
		}
	}
}
