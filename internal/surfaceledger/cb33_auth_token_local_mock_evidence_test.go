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

func TestAuthTokenLocalMockEvidenceClosesExactlyFourSupportedSignatures(t *testing.T) {
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
	wantIDs := []string{
		"apex:Auth.AuthToken.getAccessToken(String,String)",
		"apex:Auth.AuthToken.getAccessTokenMap(String,String)",
		"apex:Auth.AuthToken.refreshAccessToken(String,String,String)",
		"apex:Auth.AuthToken.revokeAccess(String,String,String,String)",
	}
	if fixture.Name != "integration-auth-token-local-mocks" {
		t.Fatalf("fixture name = %q", fixture.Name)
	}
	if len(fixture.Evidence) != len(wantIDs) {
		t.Fatalf("evidence rows = %d, want exactly %d: %#v", len(fixture.Evidence), len(wantIDs), fixture.Evidence)
	}
	wantEvidence := make(map[string]bool, len(wantIDs))
	for _, id := range wantIDs {
		wantEvidence[id] = true
	}
	for _, evidence := range fixture.Evidence {
		if !wantEvidence[evidence.SurfaceID] || evidence.Kind != "exec" {
			t.Errorf("unexpected AuthToken evidence row = %#v", evidence)
		}
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
		"String accessToken = Auth.AuthToken.getAccessToken('provider', 'local');",
		"Map<String,String> accessTokenMap = Auth.AuthToken.getAccessTokenMap('provider', 'local');",
		"Auth.OAuthRefreshResult refresh = Auth.AuthToken.refreshAccessToken('provider', 'local', accessToken);",
		"System.assertEquals(true, Auth.AuthToken.revokeAccess('provider', 'local', 'user', 'remote'));",
		"System.assertEquals('local-auth-token', accessToken);",
		"System.assertEquals('local-auth-token', accessTokenMap.get('access_token'));",
		"System.assertEquals('local-refresh-token', accessTokenMap.get('refresh_token'));",
		"System.assertEquals('Bearer', accessTokenMap.get('token_type'));",
		"System.assertEquals('local-auth-token', refresh.accessToken);",
		"System.assertEquals('local-refresh-token', refresh.refreshToken);",
		"System.assertEquals(null, refresh.error);",
	} {
		if !strings.Contains(fixture.Source[0].Content, marker) {
			t.Errorf("fixture source is missing %q", marker)
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
	for _, id := range wantIDs {
		row, ok := byID[id]
		if !ok {
			t.Fatalf("missing AuthToken target row %s", id)
		}
		if row.GladeShape != ShapeSignatureKnown || row.GladeBehavior != BehaviorSupported || row.Evidence != EvidenceFixture || row.GapClass != "" {
			t.Errorf("%s merged state = shape:%s behavior:%s evidence:%s gap:%s", id, row.GladeShape, row.GladeBehavior, row.Evidence, row.GapClass)
		}
	}
}
