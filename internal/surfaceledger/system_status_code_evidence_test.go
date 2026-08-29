package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

var systemStatusCodeFixtures = []string{
	"core-runtime-system-status-code-enum-api67.json",
	"core-runtime-system-status-code-constants-api67-00.json",
	"core-runtime-system-status-code-constants-api67-01.json",
	"core-runtime-system-status-code-constants-api67-02.json",
	"core-runtime-system-status-code-constants-api67-03.json",
	"core-runtime-system-status-code-constants-api67-04.json",
	"core-runtime-system-status-code-constants-api67-05.json",
	"core-runtime-system-status-code-constants-api67-06.json",
}

func TestSystemStatusCodeHasExactExecutableLocalEvidence(t *testing.T) {
	root := filepath.Join("..", "..")
	sourcePath := filepath.Join(root, "docs", "fixtures", "current-base-cb191-system-rebind-positive-api67.json")
	fixturePaths := make([]string, len(systemStatusCodeFixtures))
	for i, name := range systemStatusCodeFixtures {
		fixturePaths[i] = filepath.Join(root, "docs", "fixtures", name)
	}

	wantIDs := statusCodeIDs(t, sourcePath)
	if len(wantIDs) != 629 {
		t.Fatalf("source StatusCode rows = %d, want 629", len(wantIDs))
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
	if len(wantIDs)-6 != 623 {
		t.Fatalf("source StatusCode constants = %d, want 623", len(wantIDs)-6)
	}
	if !wantSet["apex:System.StatusCode.CART_OPERATION_IN_PROGRESS"] {
		t.Fatal("source StatusCode family missing CART_OPERATION_IN_PROGRESS")
	}
	selectedRows := 0
	wantFixtureRows := []int{6, 101, 100, 100, 100, 100, 100, 22}
	for i, fixturePath := range fixturePaths {
		fixture, err := compat.LoadFile(fixturePath)
		if err != nil {
			t.Fatal(err)
		}
		if err := compat.Validate(fixture); err != nil {
			t.Fatal(err)
		}
		if fixture.Name != strings.TrimSuffix(systemStatusCodeFixtures[i], ".json") || fixture.Command.Kind != "exec" || len(fixture.Command.Args) != 1 || len(fixture.Source) != 1 || fixture.Source[0].Content != fixture.Command.Args[0] {
			t.Fatalf("fixture execution envelope = %#v", fixture)
		}
		if len(fixture.Evidence) != wantFixtureRows[i] {
			t.Fatalf("fixture %s evidence rows = %d, want %d", fixture.Name, len(fixture.Evidence), wantFixtureRows[i])
		}
		if len(fixture.Source[0].Content) > 12*1024 {
			t.Fatalf("fixture %s anonymous Apex bytes = %d, want at most 12288", fixture.Name, len(fixture.Source[0].Content))
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
		if metadata.APIVersion != "67.0" || metadata.Mode != "local-runtime" || metadata.EvidenceOnly || metadata.SalesforceEligible == nil || !*metadata.SalesforceEligible || metadata.Profile.CandidateCommit != "dfe2c9891b33c90b31b0893ee79dc2af27d9d91b" || metadata.Profile.CandidateSHA256 != "ef08b4486ee18bca2d006c15936c3442db59fefa755722cce1656de6440324fd" {
			t.Fatalf("fixture provenance = %#v", metadata)
		}
		if metadata.Profile.SelectedRows != wantFixtureRows[i] {
			t.Fatalf("fixture %s selected rows = %d, want %d", fixture.Name, metadata.Profile.SelectedRows, wantFixtureRows[i])
		}
		selectedRows += metadata.Profile.SelectedRows
		if metadata.Salesforce != nil || metadata.Comparisons != nil || !strings.Contains(metadata.Notes, "no hosted Salesforce execution or parity claim") {
			t.Fatalf("fixture makes an unsupported Salesforce parity claim: %#v", metadata)
		}
		if result, err := compat.Run(fixture); err != nil || !result.OK {
			t.Fatalf("fixture execution = %#v, error = %v", result, err)
		}
	}
	if selectedRows != 629 {
		t.Fatalf("fixture selected rows = %d, want 629", selectedRows)
	}

	evidence, err := BuildEvidenceSnapshot(fixturePaths)
	if err != nil {
		t.Fatal(err)
	}
	assertExactSurfaceSet(t, evidence, wantIDs)
	for _, row := range evidence {
		if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported {
			t.Fatalf("%s evidence/behavior = %s/%s, want fixture/supported", row.SurfaceID, row.Evidence, row.GladeBehavior)
		}
	}
	fixture, err := compat.LoadFile(fixturePaths[0])
	if err != nil {
		t.Fatal(err)
	}
	source := fixture.Source[0].Content
	for _, assertion := range []string{
		"System.assertEquals(623, System.StatusCode.values().size())",
		"System.assertEquals(410, value.ordinal())",
		"value.equals(value)",
		"value.hashCode()",
		"value.ordinal()",
		"System.StatusCode.valueOf('CART_OPERATION_IN_PROGRESS')",
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
	ids := make([]string, 0, 629)
	for _, row := range fixture.Evidence {
		if strings.HasPrefix(row.SurfaceID, "apex:System.StatusCode") {
			ids = append(ids, row.SurfaceID)
		}
	}
	return ids
}
