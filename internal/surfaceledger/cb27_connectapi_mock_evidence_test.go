package surfaceledger

import (
	"path/filepath"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func TestCB27ConnectAPIMockOverloadsAreSignatureKnownSupportedAndEvidenced(t *testing.T) {
	targets := []string{
		"apex:ConnectApi.ChatterUsers.getFollowings(String,String)",
		"apex:ConnectApi.ChatterUsers.getFollowings(String,String,Integer)",
		"apex:ConnectApi.ChatterUsers.getFollowings(String,String,Integer,Integer)",
		"apex:ConnectApi.ChatterUsers.getFollowings(String,String,String)",
		"apex:ConnectApi.ChatterUsers.getFollowings(String,String,String,Integer)",
		"apex:ConnectApi.ChatterUsers.getFollowings(String,String,String,Integer,Integer)",
		"apex:ConnectApi.ManagedContent.getAllManagedContent(String,Integer,Integer,String,String)",
		"apex:ConnectApi.ManagedContent.getAllManagedContent(String,Integer,Integer,String,String,Boolean)",
		"apex:ConnectApi.ManagedContent.getManagedContentByContentKeys(String,List<String>,Integer,Integer,String,String,Boolean)",
		"apex:ConnectApi.UserProfiles.setPhoto(String,String,ConnectApi.BinaryInput)",
		"apex:ConnectApi.UserProfiles.setPhoto(String,String,String,Object)",
	}
	fixturePath := func(name string) string {
		return filepath.Join("..", "..", "docs", "fixtures", name)
	}

	identityPath := fixturePath("apex-connectapi-identity.json")
	tailPath := fixturePath("apex-connectapi-tail-unsupported-surfaces.json")
	mockPath := fixturePath("apex-connectapi-corpus-local-mocks.json")
	identity, err := compat.LoadFile(identityPath)
	if err != nil {
		t.Fatal(err)
	}
	tail, err := compat.LoadFile(tailPath)
	if err != nil {
		t.Fatal(err)
	}

	identityIDs := make(map[string]bool, len(identity.Evidence))
	for _, evidence := range identity.Evidence {
		identityIDs[evidence.SurfaceID] = true
	}
	wantObjectID := targets[len(targets)-1]
	if !identityIDs[wantObjectID] {
		t.Errorf("identity fixture is missing corrected UserProfiles.setPhoto Object signature %s", wantObjectID)
	}
	if identityIDs["apex:ConnectApi.UserProfiles.setPhoto(String,String,String,Integer)"] {
		t.Errorf("identity fixture retains wrong UserProfiles.setPhoto Integer signature")
	}

	tailIDs := make(map[string]bool, len(tail.Evidence))
	for _, evidence := range tail.Evidence {
		tailIDs[evidence.SurfaceID] = true
	}
	for _, id := range targets[:9] {
		if tailIDs[id] {
			t.Errorf("unsupported-tail fixture still contains supported target %s", id)
		}
	}

	evidence, err := BuildEvidenceSnapshot([]string{identityPath, mockPath, tailPath})
	if err != nil {
		t.Fatal(err)
	}
	ledger := Merge(nil, nil, BuildGladeSnapshot(), evidence)
	byID := rowsByID(ledger.Rows)
	for _, id := range targets {
		row, ok := byID[id]
		if !ok {
			t.Errorf("missing target row %s", id)
			continue
		}
		if row.GladeShape != ShapeSignatureKnown || row.GladeBehavior != BehaviorSupported || row.Evidence != EvidenceFixture || row.GapClass != "" {
			t.Errorf("%s merged state = shape:%s behavior:%s evidence:%s gap:%s", id, row.GladeShape, row.GladeBehavior, row.Evidence, row.GapClass)
		}
	}
}
