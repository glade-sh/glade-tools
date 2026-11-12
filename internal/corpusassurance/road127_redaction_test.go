package corpusassurance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRoad127PublicUsageRedaction(t *testing.T) {
	root := t.TempDir()
	report := AssuranceReport{
		SchemaVersion: 1,
		Rows: []AssuranceSurfaceRow{{
			SurfaceID:    "apex:Example.run",
			CompileReady: true,
			UsageKeys: []string{
				"Flow.InvoiceApproval",
				"Component.WidgetPanel",
				"Schema.Account.FieldSets.OrderFields",
			},
		}},
	}
	_, err := writeAssuranceArtifacts(report, filepath.Join(root, "report.json"), filepath.Join(root, "report.html"), filepath.Join(root, "receipt.json"), map[string]string{"input": strings.Repeat("a", 64)})
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"report.json", "report.html"} {
		data, err := os.ReadFile(filepath.Join(root, file))
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range report.Rows[0].UsageKeys {
			if strings.Contains(string(data), secret) {
				t.Errorf("%s exposes org-defined usage key %q", file, secret)
			}
		}
		if !strings.Contains(string(data), "org-defined") {
			t.Errorf("%s omits org-defined marker", file)
		}
	}
}

func TestRoad127RendererRejectsUnclassifiedUsage(t *testing.T) {
	for _, name := range []string{"Flow.InvoiceApproval", "Component.WidgetPanel", "Schema.Account.FieldSets.OrderFields"} {
		report := `{"schemaVersion":1,"rows":[{"usageKeys":["` + name + `"]}]}`
		if _, err := renderAssuranceHTML([]byte(report)); err == nil {
			t.Errorf("renderer accepted %q", name)
		}
	}
}

func TestRoad127StableRedactedHash(t *testing.T) {
	makeReport := func(key string) AssuranceReceipt {
		root := t.TempDir()
		report := AssuranceReport{
			SchemaVersion: 1,
			Rows: []AssuranceSurfaceRow{{
				SurfaceID:    "apex:Example.run",
				CompileReady: true,
				UsageKeys:    []string{key},
			}},
		}
		receipt, err := writeAssuranceArtifacts(report, filepath.Join(root, "r.json"), filepath.Join(root, "r.html"), filepath.Join(root, "receipt.json"), map[string]string{"input": strings.Repeat("a", 64)})
		if err != nil {
			t.Fatal(err)
		}
		return receipt
	}
	a, b, c := makeReport("Flow.InvoiceApproval"), makeReport("Flow.InvoiceApproval"), makeReport("System.debug")
	if a.AssuranceSHA256 != b.AssuranceSHA256 || a.HTMLSHA256 != b.HTMLSHA256 {
		t.Fatal("identical public payload hashes differ")
	}
	if a.AssuranceSHA256 == c.AssuranceSHA256 || a.HTMLSHA256 == c.HTMLSHA256 {
		t.Fatal("changed public payload hashes match")
	}
}

func TestRoad127RepositoryRowsAndAllowlist(t *testing.T) {
	root := t.TempDir()
	row := AssuranceSurfaceRow{
		SurfaceID:    "apex:Example.run",
		CompileReady: true,
		UsageKeys: []string{
			"System.debug",
			"Flow.ExampleProcess",
			"Component.ExampleWidget",
			"Schema.Account.FieldSets.ExampleFields",
		},
	}
	report := AssuranceReport{
		SchemaVersion:         1,
		Rows:                  []AssuranceSurfaceRow{row},
		RepositorySurfaceRows: []AssuranceRepositorySurfaceRow{{RepositoryID: "example", AssuranceSurfaceRow: row}},
	}
	_, err := writeAssuranceArtifacts(report, filepath.Join(root, "report.json"), filepath.Join(root, "report.html"), filepath.Join(root, "receipt.json"), map[string]string{"input": strings.Repeat("a", 64)})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), "System.debug") != 2 || strings.Count(string(data), "org-defined") != 6 {
		t.Fatalf("unexpected public usage projection: %s", data)
	}
	if row.UsageKeys[1] != "Flow.ExampleProcess" {
		t.Fatal("projection mutated its input")
	}
}

func TestRoad127CatalogPublishesSalesforceDefinedKeys(t *testing.T) {
	for _, key := range []string{
		"System.debug", "system.DEBUG", "Database.query", "System.String", "String",
		"Account.Name", "account.name", "Account", "Schema.Account", "Schema.Account.Name",
		"Contact.Id",
	} {
		if got := publicUsageKey(key); got != key {
			t.Errorf("publicUsageKey(%q) = %q, want it published", key, got)
		}
		report := `{"schemaVersion":1,"rows":[{"usageKeys":["` + key + `"]}]}`
		if _, err := renderAssuranceHTML([]byte(report)); err != nil {
			t.Errorf("renderer refused Salesforce-defined %q: %v", key, err)
		}
	}
}

func TestRoad127CatalogRefusesOrgDefinedKeysWithoutCustomSuffix(t *testing.T) {
	for _, key := range []string{
		"Flow.InvoiceApproval", "Component.WidgetPanel", "Schema.Account.FieldSets.OrderFields",
		"Account.NoSuchStandardField", "Database.noSuchMethod", "InvoiceService.run",
		"System.NoSuchType", "Widget__c.Name", "Label.Greeting", "Page.Home", "",
	} {
		if got := publicUsageKey(key); got != orgDefinedUsage {
			t.Errorf("publicUsageKey(%q) = %q, want org-defined", key, got)
		}
		report := `{"schemaVersion":1,"rows":[{"usageKeys":["` + key + `"]}]}`
		if _, err := renderAssuranceHTML([]byte(report)); err == nil {
			t.Errorf("renderer accepted org-defined %q", key)
		}
	}
}
