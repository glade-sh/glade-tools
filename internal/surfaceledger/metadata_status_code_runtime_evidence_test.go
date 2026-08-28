package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

var metadataStatusCodeRuntimeSurfaceIDs = []string{
	"apex:Metadata.StatusCode",
	"apex:Metadata.StatusCode.equals(Object)",
	"apex:Metadata.StatusCode.hashCode()",
	"apex:Metadata.StatusCode.ordinal()",
	"apex:Metadata.StatusCode.valueOf(String)",
	"apex:Metadata.StatusCode.values()",
}

func TestMetadataStatusCodeIsAnExecutableRuntimeFamily(t *testing.T) {
	root := filepath.Join("..", "..")
	fixturePath := filepath.Join(root, "docs", "fixtures", "core-runtime-metadata-status-code-enum-api67.json")
	policy, err := LoadSupportPolicy(filepath.Join(root, "docs", "fixtures", "apex-local-support-policy.json"))
	if err != nil {
		t.Fatal(err)
	}

	profile := ComputeSupportProfile(BuildGladeSnapshot(), policy, nil)
	statusCodeRows := 0
	for _, row := range profile.Rows {
		if row.Namespace != "Metadata" {
			continue
		}
		if row.SurfaceID == "apex:Metadata.StatusCode" || strings.HasPrefix(row.SurfaceID, "apex:Metadata.StatusCode.") {
			statusCodeRows++
			if row.Disposition != DispositionLocalRuntimeRequired || !strings.Contains(row.MatchRule, "member exception") {
				t.Fatalf("%s disposition/match = %s/%q", row.SurfaceID, row.Disposition, row.MatchRule)
			}
		} else if row.Disposition != DispositionDeterministicMockRequired {
			t.Fatalf("unrelated Metadata row %s changed to %s", row.SurfaceID, row.Disposition)
		}
	}
	if statusCodeRows != 519 {
		t.Fatalf("Metadata.StatusCode rows = %d, want 519", statusCodeRows)
	}

	fixture, err := compat.LoadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Command.Kind != "exec" || len(fixture.Command.Args) != 1 || len(fixture.Source) != 1 || fixture.Source[0].Content != fixture.Command.Args[0] {
		t.Fatalf("fixture execution envelope = %#v", fixture)
	}
	evidence, err := BuildEvidenceSnapshot([]string{fixturePath})
	if err != nil {
		t.Fatal(err)
	}
	assertExactSurfaceSet(t, evidence, metadataStatusCodeRuntimeSurfaceIDs)

	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	var metadata struct {
		Mode               string `json:"mode"`
		EvidenceOnly       bool   `json:"evidenceOnly"`
		SalesforceEligible *bool  `json:"salesforceEligible"`
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.Mode != "local-runtime" || metadata.EvidenceOnly || metadata.SalesforceEligible == nil || !*metadata.SalesforceEligible {
		t.Fatalf("fixture authority = %#v", metadata)
	}
	for _, assertion := range []string{
		"Metadata.StatusCode other = Metadata.StatusCode.INTERNAL_ERROR",
		"!value.equals(other)",
		"System.assertNotEquals(value.hashCode(), other.hashCode())",
		"System.assertNotEquals(value.ordinal(), other.ordinal())",
		"System.assertEquals(513, values.size())",
		"values[value.ordinal()]",
		"values[other.ordinal()]",
		"Metadata.StatusCode.valueOf('APEX_FAILED')",
		"Metadata.StatusCode.valueOf('INTERNAL_ERROR')",
	} {
		if !strings.Contains(fixture.Source[0].Content, assertion) {
			t.Fatalf("fixture source missing %q", assertion)
		}
	}
	if result, err := compat.Run(fixture); err != nil || !result.OK {
		t.Fatalf("fixture execution = %#v, error = %v", result, err)
	}

	want := mapFromIDs(metadataStatusCodeRuntimeSurfaceIDs)
	constantRows := 0
	for i, wantRows := range []int{100, 100, 100, 100, 100, 13} {
		path := filepath.Join(root, "docs", "fixtures", "core-runtime-metadata-status-code-constants-api67-0"+string(rune('0'+i))+".json")
		rows, err := BuildEvidenceSnapshot([]string{path})
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != wantRows {
			t.Fatalf("constant fixture %d rows = %d, want %d", i, len(rows), wantRows)
		}
		for _, row := range rows {
			if !strings.HasPrefix(row.SurfaceID, "apex:Metadata.StatusCode.") || want[row.SurfaceID] {
				t.Fatalf("constant fixture %d owns non-constant %s", i, row.SurfaceID)
			}
		}
		constantRows += len(rows)
	}
	if constantRows != 513 {
		t.Fatalf("Metadata.StatusCode constant rows = %d, want 513", constantRows)
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
	for _, id := range metadataStatusCodeRuntimeSurfaceIDs {
		if owners[id] != 1 {
			t.Fatalf("fixture ownership for %s = %d, want 1", id, owners[id])
		}
	}
}
