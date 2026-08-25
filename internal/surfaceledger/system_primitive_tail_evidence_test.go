package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

const systemPrimitiveTailFixture = "core-runtime-system-primitive-tail-api67.json"

var systemPrimitiveTailIDs = []string{
	"apex:System.Blob",
	"apex:System.Blob.equals(Object)",
	"apex:System.Blob.hashCode()",
	"apex:System.Blob.toString",
	"apex:System.Crypto",
	"apex:System.Crypto.Crypto()",
	"apex:System.Crypto.clone()",
	"apex:System.Date",
	"apex:System.Datetime",
	"apex:System.Decimal",
	"apex:System.EncodingUtil",
	"apex:System.EncodingUtil.EncodingUtil()",
	"apex:System.EncodingUtil.base64Decode",
	"apex:System.EncodingUtil.clone()",
	"apex:System.EncodingUtil.convertFromHex",
	"apex:System.JSON.JSON()",
	"apex:System.JSON.clone()",
	"apex:System.JSON.deserializeStrict",
	"apex:System.JSONGenerator",
	"apex:System.JSONGenerator.clone()",
	"apex:System.Math",
	"apex:System.Math.E",
	"apex:System.Math.Math()",
	"apex:System.Math.PI",
	"apex:System.Math.clone()",
	"apex:System.Time",
	"apex:System.URL.getCurrentRequestUrl",
	"apex:System.URL.getSalesforceBaseUrl",
	"apex:System.URL.getSalesforceBaseUrl()",
	"apex:System.URL.Url(String,String,Integer,String)",
	"apex:System.URL.Url(String,String,String)",
	"apex:System.Url.clone()",
	"apex:System.Url.toString()",
}

func TestSystemPrimitiveTailHasExactExecutableLocalEvidence(t *testing.T) {
	root := filepath.Join("..", "..")
	fixturePath := filepath.Join(root, "docs", "fixtures", systemPrimitiveTailFixture)
	fixture, err := compat.LoadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Name != strings.TrimSuffix(systemPrimitiveTailFixture, ".json") || fixture.Command.Kind != "exec" || len(fixture.Command.Args) != 1 || len(fixture.Source) != 1 || fixture.Source[0].Path != "anonymous.apex" || fixture.Source[0].Content != fixture.Command.Args[0] {
		t.Fatalf("fixture execution envelope = %#v", fixture)
	}
	if len(fixture.Evidence) != len(systemPrimitiveTailIDs) {
		t.Fatalf("fixture evidence rows = %d, want %d", len(fixture.Evidence), len(systemPrimitiveTailIDs))
	}
	want := mapFromIDs(systemPrimitiveTailIDs)
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
		ExclusionClass     string `json:"salesforceExclusionClass"`
		ExclusionReason    string `json:"salesforceExclusionReason"`
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
	if metadata.APIVersion != "67.0" || metadata.Mode != "local-runtime" || metadata.EvidenceOnly || metadata.SalesforceEligible == nil || *metadata.SalesforceEligible || metadata.ExclusionClass != "policy-local-only" || !strings.Contains(metadata.ExclusionReason, "zero hosted Salesforce parity") || metadata.Profile.CandidateCommit != "3409c4c85827b19712e9df83fc8905aa02bd1dc8" || metadata.Profile.CandidateSHA256 != "960ac9f26fa92aae6054cbe0e59f9c4ab1f84397df67bd8a89528068d02a1fce" || metadata.Profile.SelectedRows != len(systemPrimitiveTailIDs) {
		t.Fatalf("fixture provenance = %#v", metadata)
	}
	if metadata.Salesforce != nil || metadata.Comparisons != nil || !strings.Contains(metadata.Notes, "no hosted Salesforce execution or parity claim") {
		t.Fatalf("fixture makes an unsupported Salesforce parity claim: %#v", metadata)
	}

	evidence, err := BuildEvidenceSnapshot([]string{fixturePath})
	if err != nil {
		t.Fatal(err)
	}
	assertExactSurfaceSet(t, evidence, systemPrimitiveTailIDs)
	for _, row := range evidence {
		if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported {
			t.Fatalf("%s evidence/behavior = %s/%s, want fixture/supported", row.SurfaceID, row.Evidence, row.GladeBehavior)
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
	for _, id := range systemPrimitiveTailIDs {
		if owners[id] != 1 {
			t.Fatalf("fixture ownership for %s = %d, want exactly one non-evidenceOnly owner", id, owners[id])
		}
	}

	if result, err := compat.Run(fixture); err != nil || !result.OK {
		t.Fatalf("fixture execution = %#v, error = %v", result, err)
	}
}
