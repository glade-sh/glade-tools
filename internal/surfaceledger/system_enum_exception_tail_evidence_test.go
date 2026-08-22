package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

const systemEnumExceptionTailFixture = "core-runtime-system-enum-exception-tail-api67.json"

var systemEnumExceptionTailIDs = []string{
	"apex:System.Enum",
	"apex:System.RoundingMode.name",
	"apex:System.RoundingMode.ordinal",
	"apex:System.RoundingMode.toString",
	"apex:System.RoundingMode.valueOf",
	"apex:System.RoundingMode.values",
	"apex:System.AsyncException",
	"apex:System.ListException",
	"apex:System.NoSuchElementException",
	"apex:System.PatternSyntaxException",
	"apex:System.QueryException",
	"apex:System.SObjectException",
	"apex:System.SecurityException",
	"apex:System.InvalidParameterValueException.equals(Object)",
	"apex:System.InvalidParameterValueException.hashCode()",
	"apex:System.InvalidParameterValueException.toString()",
	"apex:System.TouchHandledException",
	"apex:System.TouchHandledException.equals(Object)",
	"apex:System.TouchHandledException.hashCode()",
	"apex:System.TouchHandledException.toString()",
	"apex:System.VisualforceException",
	"apex:System.VisualforceException.equals(Object)",
	"apex:System.VisualforceException.hashCode()",
	"apex:System.WaveTemplateException",
	"apex:System.WaveTemplateException.equals(Object)",
	"apex:System.WaveTemplateException.hashCode()",
}

var systemEnumExceptionTailGapIDs = []string{
	"apex:System.NoAccessException.getInaccessibleFields()",
	"apex:System.NoDataFoundException.getInaccessibleFields()",
	"apex:System.TouchHandledException.getInaccessibleFields()",
	"apex:System.VisualforceException.getInaccessibleFields()",
	"apex:System.WaveTemplateException.getInaccessibleFields()",
}

func TestSystemEnumExceptionTailHasExactSealedCandidateEvidence(t *testing.T) {
	root := filepath.Join("..", "..")
	fixturePath := filepath.Join(root, "docs", "fixtures", systemEnumExceptionTailFixture)
	fixture, err := compat.LoadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Name != strings.TrimSuffix(systemEnumExceptionTailFixture, ".json") || fixture.Command.Kind != "exec" || len(fixture.Command.Args) != 1 || len(fixture.Source) != 1 || fixture.Source[0].Content != fixture.Command.Args[0] {
		t.Fatalf("fixture execution envelope = %#v", fixture)
	}
	if len(fixture.Evidence) != len(systemEnumExceptionTailIDs) {
		t.Fatalf("fixture evidence rows = %d, want %d", len(fixture.Evidence), len(systemEnumExceptionTailIDs))
	}
	if result, err := compat.Run(fixture); err != nil || !result.OK {
		t.Fatalf("fixture execution = %#v, error = %v", result, err)
	}

	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	var metadata struct {
		APIVersion                string `json:"apiVersion"`
		Mode                      string `json:"mode"`
		EvidenceOnly              bool   `json:"evidenceOnly"`
		SalesforceEligible        *bool  `json:"salesforceEligible"`
		SalesforceExclusionClass  string `json:"salesforceExclusionClass"`
		SalesforceExclusionReason string `json:"salesforceExclusionReason"`
		Salesforce                any    `json:"salesforce"`
		Comparisons               any    `json:"comparisons"`
		Notes                     string `json:"notes"`
		Profile                   struct {
			CandidateCommit string `json:"candidateCommit"`
			CandidateSHA256 string `json:"candidateSha256"`
			SelectedRows    int    `json:"selectedRowCount"`
		} `json:"profile"`
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.APIVersion != "67.0" || metadata.Mode != "local-runtime" || metadata.EvidenceOnly || metadata.SalesforceEligible == nil || *metadata.SalesforceEligible || metadata.SalesforceExclusionClass != "policy-local-only" || !strings.Contains(metadata.SalesforceExclusionReason, "no hosted Salesforce parity") || metadata.Profile.CandidateCommit != "3409c4c85827b19712e9df83fc8905aa02bd1dc8" || metadata.Profile.CandidateSHA256 != "960ac9f26fa92aae6054cbe0e59f9c4ab1f84397df67bd8a89528068d02a1fce" || metadata.Profile.SelectedRows != len(systemEnumExceptionTailIDs) || metadata.Salesforce != nil || metadata.Comparisons != nil || !strings.Contains(metadata.Notes, "no hosted Salesforce execution or parity claim") {
		t.Fatalf("fixture provenance = %#v", metadata)
	}

	want := mapFromIDs(systemEnumExceptionTailIDs)
	got := make(map[string]bool, len(fixture.Evidence))
	for _, row := range fixture.Evidence {
		if !want[row.SurfaceID] || got[row.SurfaceID] {
			t.Fatalf("unexpected or duplicate evidence row %q", row.SurfaceID)
		}
		got[row.SurfaceID] = true
	}
	for _, id := range systemEnumExceptionTailIDs {
		if !got[id] {
			t.Fatalf("missing evidence row %q", id)
		}
	}
	for _, witness := range []string{
		"System.RoundingMode.valueOf('HALF_UP').ordinal()",
		"System.RoundingMode.values()",
		"new PatternSyntaxException('bad')",
		"Auth.AuthToken.getAccessToken('provider', 'local')",
		"catch (InvalidParameterValueException invalidValue)",
		"new TouchHandledException('touch')",
		"new VisualforceException()",
		"new WaveTemplateException()",
	} {
		if !strings.Contains(fixture.Source[0].Content, witness) {
			t.Fatalf("source missing direct witness %q", witness)
		}
	}

	paths, err := filepath.Glob(filepath.Join(root, "docs", "fixtures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	owners := make(map[string]int, len(systemEnumExceptionTailIDs))
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
	for _, id := range systemEnumExceptionTailIDs {
		if owners[id] != 1 {
			t.Fatalf("fixture ownership for %s = %d, want exactly one active owner", id, owners[id])
		}
	}

	for _, id := range systemEnumExceptionTailGapIDs {
		if owners[id] != 0 {
			t.Fatalf("context-required gap %s has active owner count %d", id, owners[id])
		}
	}
	legacyTail := filepath.Join(root, "docs", "fixtures", "current-base-system-exception-tail-20260805.json")
	var legacyHeader struct {
		EvidenceOnly bool `json:"evidenceOnly"`
		Evidence     []struct {
			SurfaceID string `json:"surfaceId"`
		} `json:"evidence"`
	}
	readJSON(t, legacyTail, &legacyHeader)
	if !legacyHeader.EvidenceOnly {
		t.Fatalf("stale exception-tail fixture must remain evidenceOnly")
	}
	legacyIDs := make(map[string]bool, len(legacyHeader.Evidence))
	for _, row := range legacyHeader.Evidence {
		legacyIDs[row.SurfaceID] = true
	}
	for _, id := range systemEnumExceptionTailGapIDs {
		if !legacyIDs[id] {
			t.Fatalf("explicit gap %s was removed from retained historical fixture", id)
		}
	}
}
