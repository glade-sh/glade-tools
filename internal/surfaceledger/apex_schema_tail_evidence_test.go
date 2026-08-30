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
const apexSchemaFieldsGetFixture = "core-runtime-schema-sobjecttypefields-get-api67.json"

const apexSchemaFieldsGetID = "apex:Schema.SObjectTypeFields.get(String)"

var apexSchemaTailIDs = []string{
	"apex:Schema.SObjectType.newSObject(Id)",
	"apex:Schema.SObjectTypeFieldSets.getMap()",
	"apex:Schema.SObjectTypeFields.getMap()",
	"apex:Schema.Schema.describeSObjects(List<String>,Object)",
}

func TestApexSchemaFieldsGetIsSingletonForSalesforceFilter(t *testing.T) {
	root := filepath.Join("..", "..", "docs", "fixtures")
	owners := 0
	paths, err := filepath.Glob(filepath.Join(root, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		var fixture struct {
			Command struct {
				Kind string `json:"kind"`
			} `json:"command"`
			EvidenceOnly       bool                     `json:"evidenceOnly"`
			Evidence           []compat.FixtureEvidence `json:"evidence"`
			SalesforceEligible bool                     `json:"salesforceEligible"`
			ExclusionClass     string                   `json:"salesforceExclusionClass"`
			ExclusionReason    string                   `json:"salesforceExclusionReason"`
		}
		readJSON(t, path, &fixture)
		if fixture.EvidenceOnly {
			continue
		}
		for _, row := range fixture.Evidence {
			if row.SurfaceID == apexSchemaFieldsGetID {
				owners++
				if filepath.Base(path) != apexSchemaFieldsGetFixture || fixture.Command.Kind != "exec" || len(fixture.Evidence) != 1 || fixture.SalesforceEligible || fixture.ExclusionClass != "policy-local-only" || fixture.ExclusionReason != "Salesforce API 67 rejects direct Schema.SObjectTypeFields.get(String); this local exec witness is policy-local-only and grants zero Salesforce parity credit." {
					t.Fatalf("Salesforce filter fixture = %s %#v", path, fixture)
				}
			}
		}
	}
	if owners != 1 {
		t.Fatalf("fixture ownership for %s = %d, want one singleton owner", apexSchemaFieldsGetID, owners)
	}
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
		"SObject withId = accountType.newSObject(accountId);",
		"accountDescribe.fieldSets.getMap()",
		"accountDescribe.fields.getMap()",
		"Schema.describeSObjects(new List<String>{'Account'}, Schema.SObjectDescribeOptions.DEFERRED)",
	} {
		if !strings.Contains(source, witness) {
			t.Fatalf("source missing direct assertion %q", witness)
		}
	}
	if strings.Contains(source, "fieldSets.get('GladeAbsentFieldSet')") {
		t.Fatal("source retains API-67-absent SObjectTypeFieldSets.get(String) call")
	}
	if strings.Contains(source, "fields.get('Name')") {
		t.Fatal("source retains Salesforce-absent SObjectTypeFields.get(String) call")
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
