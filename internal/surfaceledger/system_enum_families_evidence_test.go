package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

const systemEnumFamiliesFixture = "core-runtime-system-enum-families-api67.json"

var systemEnumFamilyCounts = map[string]int{
	"apex:System.AccessType":       7,
	"apex:System.JSONToken":        18,
	"apex:System.LoggingLevel":     11,
	"apex:System.Quiddity":         34,
	"apex:System.TriggerOperation": 13,
	"apex:System.XmlTag":           21,
}

func TestSystemEnumFamiliesHaveExactExecutableLocalEvidence(t *testing.T) {
	root := filepath.Join("..", "..")
	sourcePath := filepath.Join(root, "docs", "fixtures", "current-base-cb191-system-rebind-positive-api67.json")
	fixturePath := filepath.Join(root, "docs", "fixtures", systemEnumFamiliesFixture)
	legacyPath := filepath.Join(root, "docs", "fixtures", "core-runtime-enum-families-evidence.json")
	wantIDs := enumFamilyIDs(t, sourcePath)
	if len(wantIDs) != 104 {
		t.Fatalf("source enum-family rows = %d, want 104", len(wantIDs))
	}
	fixture, err := compat.LoadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Name != strings.TrimSuffix(systemEnumFamiliesFixture, ".json") || fixture.Command.Kind != "exec" || len(fixture.Command.Args) != 1 || len(fixture.Source) != 1 || fixture.Source[0].Content != fixture.Command.Args[0] {
		t.Fatalf("fixture execution envelope = %#v", fixture)
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
	if metadata.APIVersion != "67.0" || metadata.Mode != "local-runtime" || metadata.EvidenceOnly || metadata.SalesforceEligible == nil || !*metadata.SalesforceEligible || metadata.Profile.CandidateCommit != "3409c4c85827b19712e9df83fc8905aa02bd1dc8" || metadata.Profile.CandidateSHA256 != "960ac9f26fa92aae6054cbe0e59f9c4ab1f84397df67bd8a89528068d02a1fce" || metadata.Profile.SelectedRows != 104 {
		t.Fatalf("fixture provenance = %#v", metadata)
	}
	if metadata.Salesforce != nil || metadata.Comparisons != nil || !strings.Contains(metadata.Notes, "no hosted Salesforce execution or parity claim") {
		t.Fatalf("fixture makes an unsupported Salesforce parity claim: %#v", metadata)
	}
	legacy, err := compat.LoadFile(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(legacy); err != nil {
		t.Fatal(err)
	}
	if legacy.Name != "core-runtime-enum-families-evidence" || legacy.Command.Kind != "test" || len(legacy.Source) != 1 {
		t.Fatalf("legacy fixture envelope = %#v", legacy)
	}
	var legacyMetadata struct {
		APIVersion         string `json:"apiVersion"`
		Mode               string `json:"mode"`
		Notes              string `json:"notes"`
		SalesforceEligible *bool  `json:"salesforceEligible"`
		Salesforce         any    `json:"salesforce"`
		Comparisons        any    `json:"comparisons"`
		Profile            struct {
			CandidateCommit string `json:"candidateCommit"`
			CandidateSHA256 string `json:"candidateSha256"`
			SelectedRows    int    `json:"selectedRowCount"`
		} `json:"profile"`
	}
	readJSON(t, legacyPath, &legacyMetadata)
	if legacyMetadata.APIVersion != "67.0" || legacyMetadata.Mode != "local-runtime" || legacyMetadata.SalesforceEligible == nil || !*legacyMetadata.SalesforceEligible || legacyMetadata.Profile.CandidateCommit != "3409c4c85827b19712e9df83fc8905aa02bd1dc8" || legacyMetadata.Profile.CandidateSHA256 != "960ac9f26fa92aae6054cbe0e59f9c4ab1f84397df67bd8a89528068d02a1fce" || legacyMetadata.Profile.SelectedRows != 97 || legacyMetadata.Salesforce != nil || legacyMetadata.Comparisons != nil || !strings.Contains(legacyMetadata.Notes, "no hosted Salesforce execution or parity claim") {
		t.Fatalf("legacy fixture provenance = %#v", legacyMetadata)
	}
	legacyIDs := make(map[string]bool, len(legacy.Evidence))
	for _, row := range legacy.Evidence {
		legacyIDs[row.SurfaceID] = true
	}
	if len(legacyIDs) != 17 || countIntersection(legacyIDs, mapFromIDs(wantIDs)) != 0 {
		t.Fatalf("legacy retained rows/target overlap = %d/%d, want 17/0", len(legacyIDs), countIntersection(legacyIDs, mapFromIDs(wantIDs)))
	}
	if result, err := compat.Run(legacy); err != nil || !result.OK {
		t.Fatalf("legacy fixture execution = %#v, error = %v", result, err)
	}

	evidence, err := BuildEvidenceSnapshot([]string{fixturePath, legacyPath})
	if err != nil {
		t.Fatal(err)
	}
	if len(evidence) != len(fixture.Evidence)+len(legacy.Evidence) {
		t.Fatalf("combined evidence rows = %d, want %d", len(evidence), len(fixture.Evidence)+len(legacy.Evidence))
	}
	for _, row := range evidence {
		if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported {
			t.Fatalf("%s evidence/behavior = %s/%s, want fixture/supported", row.SurfaceID, row.Evidence, row.GladeBehavior)
		}
	}
	for _, witness := range []string{
		"System.AccessType.values()", "System.JSONToken.values()", "System.LoggingLevel.values()",
		"System.Quiddity.values()", "System.TriggerOperation.values()", "System.XmlTag.values()",
	} {
		if !strings.Contains(fixture.Source[0].Content, witness) {
			t.Fatalf("source missing executable assertion %q", witness)
		}
	}

	owners := make(map[string]int, len(wantIDs))
	want := mapFromIDs(wantIDs)
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
	for _, id := range wantIDs {
		if owners[id] != 1 {
			t.Fatalf("fixture ownership for %s = %d, want exactly one non-evidenceOnly owner", id, owners[id])
		}
	}
	if result, err := compat.Run(fixture); err != nil || !result.OK {
		t.Fatalf("fixture execution = %#v, error = %v", result, err)
	}
}

func countIntersection(a, b map[string]bool) int {
	n := 0
	for id := range a {
		if b[id] {
			n++
		}
	}
	return n
}

func enumFamilyIDs(t *testing.T, path string) []string {
	t.Helper()
	var fixture struct {
		Evidence []struct {
			SurfaceID string `json:"surfaceId"`
		} `json:"evidence"`
	}
	readJSON(t, path, &fixture)
	ids := make([]string, 0, 104)
	counts := make(map[string]int)
	for _, row := range fixture.Evidence {
		for family, want := range systemEnumFamilyCounts {
			if row.SurfaceID == "apex:System.JSONToken" {
				continue
			}
			if row.SurfaceID == family || strings.HasPrefix(row.SurfaceID, family+".") {
				ids = append(ids, row.SurfaceID)
				counts[family]++
				if counts[family] > want {
					t.Fatalf("source %s rows exceed %d", family, want)
				}
			}
		}
	}
	for family, want := range systemEnumFamilyCounts {
		if counts[family] != want {
			t.Fatalf("source %s rows = %d, want %d", family, counts[family], want)
		}
	}
	return ids
}
