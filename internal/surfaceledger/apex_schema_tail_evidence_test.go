package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

const apexSchemaTailFixture = "core-runtime-apex-schema-tail-api67.json"

var apexSchemaTailIDs = []string{
	"apex:Apex.EmptyStackException",
	"apex:Apex.EmptyStackException.EmptyStackException()",
	"apex:Apex.EmptyStackException.EmptyStackException(Exception)",
	"apex:Apex.EmptyStackException.EmptyStackException(String)",
	"apex:Apex.EmptyStackException.EmptyStackException(String,Exception)",
	"apex:Apex.EmptyStackException.clone()",
	"apex:Schema.SObjectType.newSObject(Id)",
	"apex:Schema.SObjectTypeFieldSets.get(String)",
	"apex:Schema.SObjectTypeFieldSets.getMap()",
	"apex:Schema.SObjectTypeFields.get(String)",
	"apex:Schema.SObjectTypeFields.getMap()",
	"apex:Schema.Schema.Schema()",
	"apex:Schema.Schema.describeSObjects(List<String>,Object)",
}

func TestApexSchemaTailHasExactExecutableLocalEvidence(t *testing.T) {
	root := filepath.Join("..", "..")
	fixturePath := filepath.Join(root, "docs", "fixtures", apexSchemaTailFixture)
	fixture, err := compat.LoadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Name != strings.TrimSuffix(apexSchemaTailFixture, ".json") || fixture.Command.Kind != "exec" || len(fixture.Command.Args) != 1 || len(fixture.Source) != 1 || fixture.Source[0].Path != "anonymous.apex" || fixture.Source[0].Content != fixture.Command.Args[0] {
		t.Fatalf("fixture execution envelope = %#v", fixture)
	}
	if len(fixture.Evidence) != len(apexSchemaTailIDs) {
		t.Fatalf("fixture evidence rows = %d, want %d", len(fixture.Evidence), len(apexSchemaTailIDs))
	}
	want := make(map[string]bool, len(apexSchemaTailIDs))
	for _, id := range apexSchemaTailIDs {
		want[id] = true
	}
	for _, row := range fixture.Evidence {
		if !want[row.SurfaceID] || row.Kind != "exec" {
			t.Fatalf("unexpected evidence row = %#v", row)
		}
	}

	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	var metadata struct {
		APIVersion         string `json:"apiVersion"`
		Mode               string `json:"mode"`
		Notes              string `json:"notes"`
		EvidenceOnly       bool   `json:"evidenceOnly"`
		SalesforceEligible *bool  `json:"salesforceEligible"`
		Salesforce         any    `json:"salesforce"`
		Comparisons        any    `json:"comparisons"`
		Profile            struct {
			CandidateCommit string `json:"candidateCommit"`
			CandidateSHA256 string `json:"candidateSha256"`
			SelectedRows    int    `json:"selectedRowCount"`
		} `json:"profile"`
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.APIVersion != "67.0" || metadata.Mode != "local-runtime" || metadata.EvidenceOnly || metadata.SalesforceEligible == nil || !*metadata.SalesforceEligible || metadata.Profile.CandidateCommit != "3409c4c85827b19712e9df83fc8905aa02bd1dc8" || metadata.Profile.CandidateSHA256 != "960ac9f26fa92aae6054cbe0e59f9c4ab1f84397df67bd8a89528068d02a1fce" || metadata.Profile.SelectedRows != len(apexSchemaTailIDs) {
		t.Fatalf("fixture provenance = %#v", metadata)
	}
	if metadata.Salesforce != nil || metadata.Comparisons != nil || !strings.Contains(metadata.Notes, "no hosted Salesforce execution or parity claim") {
		t.Fatalf("fixture makes an unsupported Salesforce parity claim: %#v", metadata)
	}

	source := fixture.Source[0].Content
	for _, witness := range []string{
		"Apex.EmptyStackException defaultException = new Apex.EmptyStackException();",
		"Apex.EmptyStackException causedException = new Apex.EmptyStackException(rootCause);",
		"Apex.EmptyStackException messageException = new Apex.EmptyStackException('empty stack');",
		"Apex.EmptyStackException wrappedException = new Apex.EmptyStackException('wrapped empty stack', rootCause);",
		"System.assertEquals('Script-thrown exception', causedException.getMessage());",
		"System.assertEquals('Script-thrown exception', messageException.getMessage());",
		"System.assertEquals(null, wrappedException.getCause());",
		"Apex.EmptyStackException clonedException = (Apex.EmptyStackException)wrappedException.clone();",
		"System.assertNotEquals(wrappedException, clonedException);",
		"System.assertEquals(null, clonedException.getCause());",
		"SObject withId = accountType.newSObject(accountId);",
		"accountDescribe.fieldSets.get('GladeAbsentFieldSet')",
		"accountDescribe.fieldSets.getMap()",
		"accountDescribe.fields.get('Name')",
		"accountDescribe.fields.getMap()",
		"Schema.Schema schemaValue = new Schema.Schema();",
		"Schema.describeSObjects(new List<String>{'Account'}, Schema.SObjectDescribeOptions.DEFERRED)",
	} {
		if !strings.Contains(source, witness) {
			t.Fatalf("source missing direct assertion %q", witness)
		}
	}

	owners := make(map[string]int, len(want))
	paths, err := filepath.Glob(filepath.Join(root, "docs", "fixtures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		var header struct {
			EvidenceOnly bool `json:"evidenceOnly"`
			Evidence     []struct {
				SurfaceID string `json:"surfaceId"`
			} `json:"evidence"`
		}
		readJSON(t, path, &header)
		if header.EvidenceOnly {
			continue
		}
		for _, row := range header.Evidence {
			if want[row.SurfaceID] {
				owners[row.SurfaceID]++
			}
		}
	}
	for _, id := range apexSchemaTailIDs {
		if owners[id] != 1 {
			t.Fatalf("fixture ownership for %s = %d, want exactly one non-evidenceOnly owner", id, owners[id])
		}
	}
	if result, err := compat.Run(fixture); err != nil || !result.OK {
		t.Fatalf("fixture execution = %#v, error = %v", result, err)
	}
}
