package compat

import (
	"testing"

	"github.com/glade-sh/glade/internal/storage"
)

func TestStorageFixtureUsesSeedDataIDAsRecordKey(t *testing.T) {
	fixture := Fixture{
		Name: "seed-id",
		SeedData: []SeedData{
			{
				Object: "PackageLicense",
				Records: []map[string]any{
					{
						"Id":              "050000000000001",
						"NamespacePrefix": "pkg",
					},
				},
			},
		},
	}

	got := storageFixture(fixture)
	if len(got.Objects) != 1 || len(got.Objects[0].Records) != 1 {
		t.Fatalf("storage fixture objects = %#v", got.Objects)
	}
	record := got.Objects[0].Records[0]
	if record.ID != storage.ID("050000000000001") {
		t.Fatalf("record ID = %q", record.ID)
	}
	if value := record.Fields["Id"]; value.Kind != storage.ValueID || value.ID != storage.ID("050000000000001") {
		t.Fatalf("Id field = %#v", value)
	}
}
