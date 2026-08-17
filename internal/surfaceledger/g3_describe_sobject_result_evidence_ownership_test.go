package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func TestG3DescribeSObjectResultEvidenceHasExactFixtureOwnership(t *testing.T) {
	root := filepath.Join("..", "..")
	fixturePaths := map[string]string{
		"edges":     filepath.Join(root, "docs", "fixtures", "data-platform-schema-describe-edges.json"),
		"fieldsets": filepath.Join(root, "docs", "fixtures", "data-platform-schema-describe-fieldsets.json"),
	}
	wantOwner := map[string]string{
		"apex:Schema.DescribeSObjectResult.fields":         "edges",
		"apex:Schema.DescribeSObjectResult.fieldSets":      "fieldsets",
		"apex:Schema.DescribeSObjectResult.getFields()":    "edges",
		"apex:Schema.DescribeSObjectResult.getFieldSets()": "fieldsets",
	}
	type evidence struct {
		Kind      string `json:"kind"`
		Notes     string `json:"notes"`
		SurfaceID string `json:"surfaceId"`
	}
	type rawFixture struct {
		Evidence []evidence `json:"evidence"`
	}

	counts := make(map[string]int, len(wantOwner))
	owners := make(map[string]string, len(wantOwner))
	for name, fixturePath := range fixturePaths {
		data, err := os.ReadFile(fixturePath)
		if err != nil {
			t.Fatal(err)
		}
		var raw rawFixture
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatal(err)
		}
		for _, row := range raw.Evidence {
			if _, tracked := wantOwner[row.SurfaceID]; !tracked {
				continue
			}
			counts[row.SurfaceID]++
			owners[row.SurfaceID] = name
			if strings.HasPrefix(row.SurfaceID, "apex:Schema.DescribeSObjectResult.get") && (row.Kind != "unsupported" || !strings.Contains(row.Notes, "Salesforce API 67") || !strings.Contains(strings.ToLower(row.Notes), "reject")) {
				t.Fatalf("%s negative getter evidence = %#v, want API-67 rejected unsupported evidence", row.SurfaceID, row)
			}
		}
	}
	for surfaceID, owner := range wantOwner {
		if counts[surfaceID] != 1 || owners[surfaceID] != owner {
			t.Fatalf("%s raw evidence count/owner = %d/%q, want 1/%q", surfaceID, counts[surfaceID], owners[surfaceID], owner)
		}
	}

	for _, fixturePath := range fixturePaths {
		fixture, err := compat.LoadFile(fixturePath)
		if err != nil {
			t.Fatal(err)
		}
		if err := compat.Validate(fixture); err != nil {
			t.Fatal(err)
		}
		if result, err := compat.Run(fixture); err != nil || !result.OK {
			t.Fatalf("fixture %s execution = %#v, error = %v", filepath.Base(fixturePath), result, err)
		}
	}
}
