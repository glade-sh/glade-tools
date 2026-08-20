package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

const authPassiveDTOShapeDepthFixture = "core-runtime-g3-auth-passive-dto-shape-depth.json"

var authPassiveDTOShapeDepthIDs = []string{
	"apex:Auth.AuthProviderTokenResponse.AuthProviderTokenResponse(String,String,String,String)",
	"apex:Auth.GeneratedUserData.GeneratedUserData(String,String,String,String,String,String,String,String,String)",
	"apex:Auth.HeadlessUserDiscoveryResponse.HeadlessUserDiscoveryResponse(Set<Id>,String)",
	"apex:Auth.JWS.JWS(Auth.JWT,String)",
	"apex:Auth.JWS.JWS(String,String)",
	"apex:Auth.JWS.getCompactSerialization()",
	"apex:Auth.JWTBearerTokenExchange.JWTBearerTokenExchange()",
	"apex:Auth.JWTBearerTokenExchange.JWTBearerTokenExchange(String,Auth.JWS)",
	"apex:Auth.JWTBearerTokenExchange.getAccessToken()",
	"apex:Auth.JWTBearerTokenExchange.getGrantType()",
	"apex:Auth.JWTBearerTokenExchange.getHttpResponse()",
	"apex:Auth.JWTBearerTokenExchange.getJWS()",
	"apex:Auth.JWTBearerTokenExchange.getTokenEndpoint()",
	"apex:Auth.JWTBearerTokenExchange.setGrantType(String)",
	"apex:Auth.JWTBearerTokenExchange.setJWS(Auth.JWS)",
	"apex:Auth.JWTBearerTokenExchange.setTokenEndpoint(String)",
	"apex:Auth.JsonValueOutput.JsonValueOutput(String,Boolean,Integer,Double,String,String)",
	"apex:Auth.OAuthRefreshResult.OAuthRefreshResult(String,String)",
	"apex:Auth.OAuthRefreshResult.OAuthRefreshResult(String,String,String)",
	"apex:Auth.TokenValidationResult.TokenValidationResult(Boolean)",
	"apex:Auth.TokenValidationResult.TokenValidationResult(Boolean,Object,Auth.UserData,String,Auth.OAuth2TokenExchangeType,String)",
	"apex:Auth.TokenValidationResult.getCustomErrorMessage()",
	"apex:Auth.TokenValidationResult.getData()",
	"apex:Auth.TokenValidationResult.getToken()",
	"apex:Auth.TokenValidationResult.getTokenType()",
	"apex:Auth.TokenValidationResult.getUserData()",
	"apex:Auth.UserData.UserData(String,String,String,String,String,String,String,String,String,String,Map<String,String>)",
	"apex:Auth.UserData.UserData(String,String,String,String,String,String,String,String,String,String,Map<String,String>,String,String)",
	"apex:Auth.VerificationResult.VerificationResult(PageReference,Boolean,String)",
}

func TestAuthPassiveDTOShapeDepthHasExactLocalFixtureOwnership(t *testing.T) {
	root := filepath.Join("..", "..")
	fixturePath := filepath.Join(root, "docs", "fixtures", authPassiveDTOShapeDepthFixture)
	fixture, err := compat.LoadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Name != strings.TrimSuffix(authPassiveDTOShapeDepthFixture, ".json") || fixture.Command.Kind != "check" || len(fixture.Source) != 1 || len(fixture.Command.Args) != 1 || fixture.Source[0].Content != fixture.Command.Args[0] {
		t.Fatalf("fixture execution envelope = %#v", fixture)
	}
	if len(fixture.Evidence) != len(authPassiveDTOShapeDepthIDs) {
		t.Fatalf("raw evidence rows = %d, want %d", len(fixture.Evidence), len(authPassiveDTOShapeDepthIDs))
	}
	want := make(map[string]bool, len(authPassiveDTOShapeDepthIDs))
	for _, id := range authPassiveDTOShapeDepthIDs {
		want[id] = true
	}
	counts := make(map[string]int, len(fixture.Evidence))
	for _, item := range fixture.Evidence {
		counts[item.SurfaceID]++
		if item.Kind != "shape" {
			t.Fatalf("%s evidence kind = %q, want shape", item.SurfaceID, item.Kind)
		}
	}
	for _, id := range authPassiveDTOShapeDepthIDs {
		if counts[id] != 1 {
			t.Fatalf("%s raw rows = %d, want exactly one", id, counts[id])
		}
	}

	witnesses := map[string]string{
		"apex:Auth.AuthProviderTokenResponse.AuthProviderTokenResponse(String,String,String,String)":                    "new Auth.AuthProviderTokenResponse('provider', 'oauth', 'refresh', 'state')",
		"apex:Auth.GeneratedUserData.GeneratedUserData(String,String,String,String,String,String,String,String,String)": "new Auth.GeneratedUserData('first', 'last', 'email@example.invalid', 'username', 'alias', 'en_US', '00', 'UTF-8', 'America/Los_Angeles')",
		"apex:Auth.HeadlessUserDiscoveryResponse.HeadlessUserDiscoveryResponse(Set<Id>,String)":                         "new Auth.HeadlessUserDiscoveryResponse(new Set<Id>(), 'message')",
		"apex:Auth.JWS.JWS(Auth.JWT,String)":                                                                                                  "new Auth.JWS(new Auth.JWT(), 'cert')",
		"apex:Auth.JWS.JWS(String,String)":                                                                                                    "new Auth.JWS('payload', 'cert')",
		"apex:Auth.JWS.getCompactSerialization()":                                                                                             "jwsFromPayload.getCompactSerialization()",
		"apex:Auth.JWTBearerTokenExchange.JWTBearerTokenExchange()":                                                                           "new Auth.JWTBearerTokenExchange()",
		"apex:Auth.JWTBearerTokenExchange.JWTBearerTokenExchange(String,Auth.JWS)":                                                            "new Auth.JWTBearerTokenExchange('endpoint', jwsFromPayload)",
		"apex:Auth.JWTBearerTokenExchange.getAccessToken()":                                                                                   "jwtBearerTokenExchange.getAccessToken()",
		"apex:Auth.JWTBearerTokenExchange.getGrantType()":                                                                                     "jwtBearerTokenExchange.getGrantType()",
		"apex:Auth.JWTBearerTokenExchange.getHttpResponse()":                                                                                  "jwtBearerTokenExchange.getHttpResponse()",
		"apex:Auth.JWTBearerTokenExchange.getJWS()":                                                                                           "jwtBearerTokenExchange.getJWS()",
		"apex:Auth.JWTBearerTokenExchange.getTokenEndpoint()":                                                                                 "jwtBearerTokenExchange.getTokenEndpoint()",
		"apex:Auth.JWTBearerTokenExchange.setGrantType(String)":                                                                               "jwtBearerTokenExchange.setGrantType('grant')",
		"apex:Auth.JWTBearerTokenExchange.setJWS(Auth.JWS)":                                                                                   "jwtBearerTokenExchange.setJWS(jwsFromPayload)",
		"apex:Auth.JWTBearerTokenExchange.setTokenEndpoint(String)":                                                                           "jwtBearerTokenExchange.setTokenEndpoint('endpoint')",
		"apex:Auth.JsonValueOutput.JsonValueOutput(String,Boolean,Integer,Double,String,String)":                                              "new Auth.JsonValueOutput('string', true, 1, 1.0, 'json', '[]')",
		"apex:Auth.OAuthRefreshResult.OAuthRefreshResult(String,String)":                                                                      "new Auth.OAuthRefreshResult('access', 'refresh')",
		"apex:Auth.OAuthRefreshResult.OAuthRefreshResult(String,String,String)":                                                               "new Auth.OAuthRefreshResult('access', 'refresh', 'error')",
		"apex:Auth.TokenValidationResult.TokenValidationResult(Boolean)":                                                                      "new Auth.TokenValidationResult(true)",
		"apex:Auth.TokenValidationResult.TokenValidationResult(Boolean,Object,Auth.UserData,String,Auth.OAuth2TokenExchangeType,String)":      "new Auth.TokenValidationResult(true, data, userData, 'token', tokenType, 'error')",
		"apex:Auth.TokenValidationResult.getCustomErrorMessage()":                                                                             "tokenValidationResult.getCustomErrorMessage()",
		"apex:Auth.TokenValidationResult.getData()":                                                                                           "tokenValidationResult.getData()",
		"apex:Auth.TokenValidationResult.getToken()":                                                                                          "tokenValidationResult.getToken()",
		"apex:Auth.TokenValidationResult.getTokenType()":                                                                                      "tokenValidationResult.getTokenType()",
		"apex:Auth.TokenValidationResult.getUserData()":                                                                                       "tokenValidationResult.getUserData()",
		"apex:Auth.UserData.UserData(String,String,String,String,String,String,String,String,String,String,Map<String,String>)":               "new Auth.UserData('id', 'first', 'last', 'full', 'email@example.invalid', 'link', 'username', 'en_US', 'provider', 'site', new Map<String,String>())",
		"apex:Auth.UserData.UserData(String,String,String,String,String,String,String,String,String,String,Map<String,String>,String,String)": "new Auth.UserData('id', 'first', 'last', 'full', 'email@example.invalid', 'link', 'username', 'en_US', 'provider', 'site', new Map<String,String>(), 'id-token', '{}')",
		"apex:Auth.VerificationResult.VerificationResult(PageReference,Boolean,String)":                                                       "new Auth.VerificationResult(new PageReference('/start'), true, 'ok')",
	}
	for _, id := range authPassiveDTOShapeDepthIDs {
		if witness := witnesses[id]; witness == "" || !strings.Contains(fixture.Source[0].Content, witness) {
			t.Fatalf("%s source missing direct witness %q", id, witness)
		}
	}

	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	var policy struct {
		Mode                      string `json:"mode"`
		SalesforceEligible        *bool  `json:"salesforceEligible"`
		SalesforceExclusionClass  string `json:"salesforceExclusionClass"`
		SalesforceExclusionReason string `json:"salesforceExclusionReason"`
	}
	if err := json.Unmarshal(data, &policy); err != nil {
		t.Fatal(err)
	}
	if policy.Mode != "compile-shape" || policy.SalesforceEligible == nil || *policy.SalesforceEligible || policy.SalesforceExclusionClass != "policy-local-only" || !strings.Contains(policy.SalesforceExclusionReason, "zero Salesforce parity") {
		t.Fatalf("fixture policy = %#v", policy)
	}

	evidence, err := BuildEvidenceSnapshot([]string{fixturePath})
	if err != nil {
		t.Fatal(err)
	}
	assertExactSurfaceSet(t, evidence, authPassiveDTOShapeDepthIDs)
	for _, row := range evidence {
		if row.Evidence != EvidenceFixture || row.GladeShape == ShapeAbsent || row.GladeBehavior != BehaviorNone || len(row.Sources) != 1 || row.Sources[0] != "fixture:"+strings.TrimSuffix(authPassiveDTOShapeDepthFixture, ".json") {
			t.Fatalf("%s snapshot = %#v", row.SurfaceID, row)
		}
	}

	paths, err := filepath.Glob(filepath.Join(root, "docs", "fixtures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	ownership := make(map[string]int, len(want))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var header struct {
			EvidenceOnly bool `json:"evidenceOnly"`
			Evidence     []struct {
				SurfaceID string `json:"surfaceId"`
			} `json:"evidence"`
		}
		if err := json.Unmarshal(data, &header); err != nil {
			t.Fatal(err)
		}
		if header.EvidenceOnly {
			continue
		}
		for _, item := range header.Evidence {
			if want[item.SurfaceID] {
				ownership[item.SurfaceID]++
			}
		}
	}
	for _, id := range authPassiveDTOShapeDepthIDs {
		if ownership[id] != 1 {
			t.Fatalf("fixture ownership for %s = %d, want exactly one non-evidenceOnly owner", id, ownership[id])
		}
	}

	if result, err := compat.Run(fixture); err != nil || !result.OK {
		t.Fatalf("fixture check = %#v, error = %v", result, err)
	}
}
