package surfaceledger

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func TestAuthTokenLocalMockEvidenceClosesOnlyRevokeAccess(t *testing.T) {
	fixturesDir := filepath.Join("..", "..", "docs", "fixtures")
	oldPath := filepath.Join(fixturesDir, "integration-auth-token-unsupported.json")
	path := filepath.Join(fixturesDir, "integration-auth-token-local-mocks.json")

	if _, err := os.Stat(oldPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale unsupported AuthToken fixture still exists: %s", oldPath)
	}

	fixture, err := compat.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	wantID := "apex:Auth.AuthToken.revokeAccess(String,String,String,String)"
	if fixture.Name != "integration-auth-token-local-mocks" {
		t.Fatalf("fixture name = %q", fixture.Name)
	}
	if len(fixture.Evidence) != 1 {
		t.Fatalf("evidence rows = %d, want exactly one: %#v", len(fixture.Evidence), fixture.Evidence)
	}
	if fixture.Evidence[0].SurfaceID != wantID || fixture.Evidence[0].Kind != "exec" {
		t.Fatalf("unexpected AuthToken evidence row = %#v", fixture.Evidence[0])
	}

	if fixture.Command.Kind != "exec" || len(fixture.Command.Args) != 1 || fixture.Expected.Error != nil {
		t.Fatalf("fixture command/expected error = %#v/%#v", fixture.Command, fixture.Expected.Error)
	}
	var expectedResult struct {
		Debug any  `json:"debug"`
		OK    bool `json:"ok"`
	}
	if err := json.Unmarshal(fixture.Expected.Result, &expectedResult); err != nil {
		t.Fatalf("fixture expected result is invalid JSON: %v", err)
	}
	if expectedResult.Debug != nil || !expectedResult.OK {
		t.Fatalf("fixture expected result = %#v, want debug:null and ok:true", expectedResult)
	}
	if len(fixture.Source) != 1 || fixture.Source[0].Path != "anonymous.apex" || fixture.Source[0].Content != fixture.Command.Args[0] {
		t.Fatalf("fixture source/command mismatch: source=%#v command=%#v", fixture.Source, fixture.Command)
	}
	for _, marker := range []string{
		"System.assertEquals(true, Auth.AuthToken.revokeAccess('005000000000001', 'provider', null, 'remote'));",
		"local-auth-token",
		"local-refresh-token",
		"OAuthRefreshResult",
	} {
		if marker == "System.assertEquals(true, Auth.AuthToken.revokeAccess('005000000000001', 'provider', null, 'remote'));" {
			if !strings.Contains(fixture.Source[0].Content, marker) {
				t.Errorf("fixture source is missing %q", marker)
			}
		} else if strings.Contains(fixture.Source[0].Content, marker) {
			t.Errorf("fixture source contains fake AuthToken marker %q", marker)
		}
	}

	if result, err := compat.Run(fixture); err != nil || !result.OK {
		t.Fatalf("fixture execution = %#v, error = %v", result, err)
	}

	paths, err := filepath.Glob(filepath.Join(fixturesDir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, fixturePath := range paths {
		data, err := os.ReadFile(fixturePath)
		if err != nil {
			t.Fatal(err)
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatal(err)
		}
		if _, hasEvidence := raw["evidence"]; !hasEvidence {
			continue
		}
		if _, hasCommand := raw["command"]; !hasCommand {
			continue
		}
		candidate, err := compat.LoadFile(fixturePath)
		if err != nil {
			t.Fatal(err)
		}
		for _, evidence := range candidate.Evidence {
			if strings.HasPrefix(evidence.SurfaceID, "apex:Auth.AuthToken.") && (strings.EqualFold(evidence.Kind, "unsupported") || (candidate.Expected.Error != nil && strings.EqualFold(candidate.Expected.Error.Type, "UnsupportedFeature"))) {
				t.Fatalf("unsupported AuthToken evidence reintroduced in %s: %#v", filepath.Base(fixturePath), evidence)
			}
		}
	}

	evidence, err := BuildEvidenceSnapshot([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	ledger := Merge(nil, nil, BuildGladeSnapshot(), evidence)
	byID := rowsByID(ledger.Rows)
	wantBehavior := map[string]BehaviorState{
		"apex:Auth.AuthToken.getAccessToken(String,String)":            BehaviorUnsupported,
		"apex:Auth.AuthToken.getAccessTokenMap(String,String)":         BehaviorUnsupported,
		"apex:Auth.AuthToken.refreshAccessToken(String,String,String)": BehaviorUnsupported,
		wantID: BehaviorSupported,
	}
	for id, behavior := range wantBehavior {
		row, ok := byID[id]
		if !ok {
			t.Fatalf("missing AuthToken target row %s", id)
		}
		if row.GladeShape != ShapeSignatureKnown || row.GladeBehavior != behavior {
			t.Errorf("%s merged shape/behavior = %s/%s, want signature-known/%s", id, row.GladeShape, row.GladeBehavior, behavior)
		}
		if id == wantID && (row.Evidence != EvidenceFixture || row.GapClass != "") {
			t.Errorf("%s merged state = shape:%s behavior:%s evidence:%s gap:%s", id, row.GladeShape, row.GladeBehavior, row.Evidence, row.GapClass)
		}
		if id != wantID && row.Evidence == EvidenceFixture {
			t.Errorf("%s received local evidence credit", id)
		}
	}
}
