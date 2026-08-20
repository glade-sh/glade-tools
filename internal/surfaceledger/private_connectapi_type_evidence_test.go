package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func TestPrivateConnectApiTypesUseExistingIdentityFixture(t *testing.T) {
	ids := map[string]bool{
		"apex:ConnectApi.Communities":  false,
		"apex:ConnectApi.TimeZone":     false,
		"apex:ConnectApi.UserProfiles": false,
		"apex:ConnectApi.UserSettings": false,
	}
	const owner = "apex-connectapi-identity"
	root := filepath.Join("..", "..", "docs", "fixtures")
	paths, err := filepath.Glob(filepath.Join(root, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := BuildEvidenceSnapshot(paths)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if _, ok := ids[row.SurfaceID]; !ok {
			continue
		}
		if ids[row.SurfaceID] {
			t.Fatalf("duplicate fixture row for %s", row.SurfaceID)
		}
		ids[row.SurfaceID] = true
		if len(row.Sources) != 1 || row.Sources[0] != "fixture:"+owner || row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported {
			t.Fatalf("%s evidence row = %#v", row.SurfaceID, row)
		}
	}
	for id, found := range ids {
		if !found {
			t.Errorf("missing exact fixture row for %s", id)
		}
	}
	path := filepath.Join(root, owner+".json")
	fixture, err := compat.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(fixture.Source) != 1 {
		t.Fatalf("fixture source files = %d, want one", len(fixture.Source))
	}
	source := fixture.Source[0].Content
	for _, witness := range []string{
		"ConnectApi.Communities.getCommunity(",
		"ConnectApi.UserProfiles.getUserProfile(",
		"ConnectApi.UserSettings settings =",
		"ConnectApi.TimeZone timeZone =",
	} {
		if !strings.Contains(source, witness) {
			t.Errorf("fixture lacks direct type witness %q", witness)
		}
	}
	result, err := compat.Run(fixture)
	if err != nil || !result.OK {
		t.Fatalf("fixture run = %#v, error = %v", result, err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var policy struct {
		Eligible *bool  `json:"salesforceEligible"`
		Class    string `json:"salesforceExclusionClass"`
	}
	if err := json.Unmarshal(raw, &policy); err != nil {
		t.Fatal(err)
	}
	if policy.Eligible == nil || *policy.Eligible || policy.Class != "org-configuration-required" {
		t.Fatalf("fixture Salesforce policy = %#v", policy)
	}
}
