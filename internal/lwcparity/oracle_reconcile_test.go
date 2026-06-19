package lwcparity

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReconcileOracleCaptureRowsUpdatesLedgerRows(t *testing.T) {
	report := Report{
		SchemaVersion: SchemaVersion,
		Rows: []Row{{
			ID:           rowID(CategoryAPIModule, "lightning/uiRecordApi"),
			Category:     CategoryAPIModule,
			Name:         "lightning/uiRecordApi",
			Status:       StatusSupportedLocal,
			OracleStatus: StatusOracleMissing,
		}},
	}
	capture := []byte(`{
  "capturedAt": "2026-06-19T12:34:56Z",
  "rows": [
    {
      "category": "api-module",
      "name": "lightning/uiRecordApi",
      "oracleTest": "uiRecordApiOracle",
      "oracleStatus": "pass"
    }
  ]
}`)

	reconciled, count, err := ReconcileOracleCapture(report, capture)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("count = %d", count)
	}
	row := reconciled.Rows[0]
	if row.OracleTest != "uiRecordApiOracle" || row.OracleStatus != "pass" || row.LastVerified != "2026-06-19T12:34:56Z" {
		t.Fatalf("row = %#v", row)
	}
	var buf bytes.Buffer
	if err := WriteJSON(&buf, reconciled); err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
}

func TestReconcileOracleCaptureRowsAcceptsTypeKey(t *testing.T) {
	report := Report{
		SchemaVersion: SchemaVersion,
		Rows: []Row{{
			ID:           rowID(CategoryPageReference, "standard__recordPage"),
			Category:     CategoryPageReference,
			Name:         "standard__recordPage",
			Status:       StatusSupportedLocal,
			OracleStatus: StatusOracleMissing,
		}},
	}
	capture := []byte(`{
  "rows": [
    {
      "type": "page-reference",
      "name": "standard__recordPage",
      "test": "standardRecordPageOracle",
      "status": "captured",
      "lastVerified": "2026-06-19"
    }
  ]
}`)

	reconciled, count, err := ReconcileOracleCapture(report, capture)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("count = %d", count)
	}
	row := reconciled.Rows[0]
	if row.OracleTest != "standardRecordPageOracle" || row.OracleStatus != "captured" || row.LastVerified != "2026-06-19" {
		t.Fatalf("row = %#v", row)
	}
}

func TestCheckRequiredInventoryRejectsLocalOnlyRows(t *testing.T) {
	report := Report{
		SchemaVersion: SchemaVersion,
		Rows: []Row{{
			ID:           rowID(CategoryBaseComponent, "lightning/button"),
			Category:     CategoryBaseComponent,
			Name:         "lightning/button",
			Status:       StatusLocalOnly,
			OracleStatus: StatusOracleMissing,
		}},
	}

	err := CheckRequiredInventory(report)
	if err == nil || !strings.Contains(err.Error(), "inventory gaps") {
		t.Fatalf("err = %v", err)
	}
}

func TestCheckRequiredInventoryReportsMissingDocCategories(t *testing.T) {
	report := Report{
		SchemaVersion: SchemaVersion,
		Rows: []Row{{
			ID:           rowID(CategoryAPIModule, "lightning/uiRecordApi"),
			Category:     CategoryAPIModule,
			Name:         "lightning/uiRecordApi",
			Status:       StatusSupportedLocal,
			OracleStatus: StatusOracleMissing,
		}},
	}

	err := CheckRequiredInventory(report)
	if err == nil || !strings.Contains(err.Error(), CategorySalesforceModule) || !strings.Contains(err.Error(), CategoryPageReference) {
		t.Fatalf("err = %v", err)
	}
}

func TestCheckOracleFixturesIgnoresGladeCache(t *testing.T) {
	report := Report{
		SchemaVersion: SchemaVersion,
		Rows: []Row{{
			ID:           rowID(CategoryAPIModule, "lightning/uiRecordApi"),
			Category:     CategoryAPIModule,
			Name:         "lightning/uiRecordApi",
			Status:       StatusSupportedLocal,
			OracleStatus: StatusOracleMissing,
		}},
	}
	fixtureDir := t.TempDir()
	if _, err := WriteOracleFixtures(report, fixtureDir); err != nil {
		t.Fatal(err)
	}
	cachePath := filepath.Join(fixtureDir, ".glade", "test", "startup.meta.json")
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, []byte(`{"cache":true}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := CheckOracleFixtures(report, fixtureDir); err != nil {
		t.Fatalf("CheckOracleFixtures counted local cache drift: %v", err)
	}
}
