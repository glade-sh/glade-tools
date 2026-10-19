package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/apexdocs"
)

func TestNamespacePolicyEnumeratesEveryKnownConnectApiSurface(t *testing.T) {
	inv := apexdocs.Inventory{Documents: []apexdocs.Document{
		{SourcePath: "apex/apex_connectapi_example.md", Kind: "class", Namespace: "ConnectApi", Name: "Example", Title: "Example Class", Members: []apexdocs.Member{{Kind: "method", Name: "execute", Signature: "public static void execute()"}}},
		{SourcePath: "apex/apex_System_Crypto.md", Kind: "class", Namespace: "System", Name: "Crypto"},
	}}
	rows := connectAPIClassifications(inv, "264.0")
	if len(rows) != 2 {
		t.Fatalf("classifications = %+v", rows)
	}
	for _, row := range rows {
		if !strings.HasPrefix(row.SurfaceID, "apex:ConnectApi.") || row.Disposition != "explicit-unsupported" || row.Scope != "outside-claim" || !strings.Contains(row.ReasonRef, "excluded and unsupported") || !strings.Contains(row.ReasonRef, "https://developer.salesforce.com/") {
			t.Fatalf("policy classification = %+v", row)
		}
	}
}

func TestNamespacePolicyRejectsOtherNamespacesAndMismatchedEvidence(t *testing.T) {
	root := t.TempDir()
	inventory, acquisition, output := filepath.Join(root, "inventory.json"), filepath.Join(root, "acquisition.json"), filepath.Join(root, "output.json")
	data, _ := json.Marshal(apexdocs.Inventory{SchemaVersion: 1})
	if err := os.WriteFile(inventory, data, 0600); err != nil {
		t.Fatal(err)
	}
	valid := `{"schemaVersion":1,"atlasVersion":"264.0","policy":"ConnectApi is excluded and unsupported","tocSHA256":"` + strings.Repeat("a", 64) + `","references":[{"page":"apex_connectapi_example.htm","sourceURL":"https://developer.salesforce.com/docs/get_document_content/apexref/apex_connectapi_example.htm/en-us/264.0","namespace":"ConnectApi","acquisition":"policy-excluded","disposition":"explicitly-unsupported","reason":"excluded and unsupported; documentation not acquired"}],"httpEvidence":{"statusCounts":{"403":1},"responses":[{"url":"https://developer.salesforce.com/docs/get_document_content/apexref/apex_connectapi_example.htm/en-us/264.0","status":403}]}}`
	for _, test := range []struct {
		name, input string
		invalid     bool
	}{
		{"valid", valid, false},
		{"namespace", strings.Replace(valid, `"namespace":"ConnectApi"`, `"namespace":"System"`, 1), true},
		{"reference", strings.ReplaceAll(valid, "apex_connectapi_example", "apex_System_Crypto"), true},
		{"status", strings.Replace(valid, `"status":403`, `"status":200`, 1), true},
		{"count", strings.Replace(valid, `"403":1`, `"403":2`, 1), true},
		{"trailing", valid + `{}`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := os.WriteFile(acquisition, []byte(test.input), 0600); err != nil {
				t.Fatal(err)
			}
			err := exportNamespacePolicy(inventory, acquisition, output)
			if (err != nil) != test.invalid {
				t.Fatalf("error = %v", err)
			}
		})
	}
}
