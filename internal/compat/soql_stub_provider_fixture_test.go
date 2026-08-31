package compat

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecutableSoqlStubProvidersUseSalesforceClassShape(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "docs", "fixtures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	var providers []string
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var fixture struct {
			Command struct {
				Kind string `json:"kind"`
			} `json:"command"`
			Source []struct {
				Content string `json:"content"`
			} `json:"source"`
		}
		if err := json.Unmarshal(data, &fixture); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if fixture.Command.Kind != "test" && fixture.Command.Kind != "exec" {
			continue
		}
		for _, source := range fixture.Source {
			implements := strings.Contains(source.Content, "implements SoqlStubProvider") || strings.Contains(source.Content, "implements System.SoqlStubProvider")
			extends := strings.Contains(source.Content, "extends SoqlStubProvider") || strings.Contains(source.Content, "extends System.SoqlStubProvider")
			if !implements && !extends {
				continue
			}
			providers = append(providers, filepath.Base(path))
			if implements {
				t.Errorf("%s implements SoqlStubProvider; Salesforce requires extending the class", path)
			}
			if !extends {
				t.Errorf("%s does not extend SoqlStubProvider", path)
			}
			if !strings.Contains(source.Content, "override List<SObject> handleSoqlQuery") {
				t.Errorf("%s callback does not override handleSoqlQuery", path)
			}
		}
	}
	if len(providers) != 4 {
		t.Fatalf("provider fixture count = %d (%v), want 4", len(providers), providers)
	}
}

func TestSoqlStubCallbackUsesCurrentQueryWithBindsShape(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "fixtures", "test-helper-soql-stub-provider-callback.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Database.queryWithBinds('SELECT Id, Name FROM Account WHERE Name = :name', new Map<String,Object>{'name' => name}, AccessLevel.SYSTEM_MODE)") {
		t.Fatal("callback fixture does not use the API 67 queryWithBinds overload")
	}
}
