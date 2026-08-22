package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

const systemStatusCodeFixture = "core-runtime-system-status-code-api67.json"

func TestSystemStatusCodeHasExactExecutableLocalEvidence(t *testing.T) {
	root := filepath.Join("..", "..")
	sourcePath := filepath.Join(root, "docs", "fixtures", "current-base-cb191-system-rebind-positive-api67.json")
	fixturePath := filepath.Join(root, "docs", "fixtures", systemStatusCodeFixture)

	wantIDs := statusCodeIDs(t, sourcePath)
	if len(wantIDs) != 628 {
		t.Fatalf("source StatusCode rows = %d, want 628", len(wantIDs))
	}
	wantSet := mapFromIDs(wantIDs)
	for _, id := range []string{
		"apex:System.StatusCode",
		"apex:System.StatusCode.equals(Object)",
		"apex:System.StatusCode.hashCode()",
		"apex:System.StatusCode.ordinal()",
		"apex:System.StatusCode.valueOf(String)",
		"apex:System.StatusCode.values()",
	} {
		if !wantSet[id] {
			t.Fatalf("source StatusCode family missing %s", id)
		}
	}
	if len(wantIDs)-6 != 622 {
		t.Fatalf("source StatusCode constants = %d, want 622", len(wantIDs)-6)
	}
	fixture, err := compat.LoadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Name != strings.TrimSuffix(systemStatusCodeFixture, ".json") || fixture.Command.Kind != "exec" || len(fixture.Command.Args) != 1 || len(fixture.Source) != 1 || fixture.Source[0].Content != fixture.Command.Args[0] {
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
	if metadata.APIVersion != "67.0" || metadata.Mode != "local-runtime" || metadata.EvidenceOnly || metadata.SalesforceEligible == nil || !*metadata.SalesforceEligible || metadata.Profile.CandidateCommit != "3409c4c85827b19712e9df83fc8905aa02bd1dc8" || metadata.Profile.CandidateSHA256 != "960ac9f26fa92aae6054cbe0e59f9c4ab1f84397df67bd8a89528068d02a1fce" || metadata.Profile.SelectedRows != 628 {
		t.Fatalf("fixture provenance = %#v", metadata)
	}
	if metadata.Salesforce != nil || metadata.Comparisons != nil || !strings.Contains(metadata.Notes, "no hosted Salesforce execution or parity claim") {
		t.Fatalf("fixture makes an unsupported Salesforce parity claim: %#v", metadata)
	}

	evidence, err := BuildEvidenceSnapshot([]string{fixturePath})
	if err != nil {
		t.Fatal(err)
	}
	assertExactSurfaceSet(t, evidence, wantIDs)
	for _, row := range evidence {
		if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported {
			t.Fatalf("%s evidence/behavior = %s/%s, want fixture/supported", row.SurfaceID, row.Evidence, row.GladeBehavior)
		}
	}
	source := fixture.Source[0].Content
	for _, assertion := range []string{
		"System.assertEquals(622, System.StatusCode.values().size())",
		"value.equals(value)",
		"value.hashCode()",
		"value.ordinal()",
		"System.StatusCode.valueOf('ALERT_NOTIFICATION_LIMIT_EXCEEDED')",
	} {
		if !strings.Contains(source, assertion) {
			t.Fatalf("source missing executable assertion %q", assertion)
		}
	}

	owners := make(map[string]int, len(wantIDs))
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
			if wantSet[row.SurfaceID] {
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

func statusCodeIDs(t *testing.T, path string) []string {
	t.Helper()
	var fixture struct {
		EvidenceOnly bool `json:"evidenceOnly"`
		Evidence     []struct {
			SurfaceID string `json:"surfaceId"`
		} `json:"evidence"`
	}
	readJSON(t, path, &fixture)
	if !fixture.EvidenceOnly {
		t.Fatalf("source fixture %s is not evidenceOnly", path)
	}
	ids := make([]string, 0, 628)
	for _, row := range fixture.Evidence {
		if strings.HasPrefix(row.SurfaceID, "apex:System.StatusCode") {
			ids = append(ids, row.SurfaceID)
		}
	}
	return ids
}
