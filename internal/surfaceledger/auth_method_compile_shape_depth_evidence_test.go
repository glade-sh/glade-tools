package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

const authMethodCompileShapeDepthFixture = "core-runtime-auth-method-compile-shape-depth"

var authMethodCompileShapeDepthIDs = []string{
	"apex:Auth.AuthProviderPlugin.getCustomMetadataType()",
	"apex:Auth.AuthProviderPlugin.getUserInfo(Map<String,String>,Auth.AuthProviderTokenResponse)",
	"apex:Auth.AuthProviderPlugin.handleCallback(Map<String,String>,Auth.AuthProviderCallbackState)",
	"apex:Auth.AuthProviderPlugin.initiate(Map<String,String>,String)",
	"apex:Auth.AuthProviderPluginClass.refresh(Map<String,String>,String)",
	"apex:Auth.CommunitiesUtil.getLogoutUrl()",
	"apex:Auth.CommunitiesUtil.getUserDisplayName()",
	"apex:Auth.CommunitiesUtil.isInternalUser()",
	"apex:Auth.ConfigurableSelfRegHandler.createUser(Id,Id,Map<Schema.SObjectField,String>,String)",
	"apex:Auth.ConfirmUserRegistrationHandler.confirmUser(Id,Id,Id,Auth.UserData)",
	"apex:Auth.ConnectedAppPlugin.authorize(Id,Id,Boolean)",
	"apex:Auth.ConnectedAppPlugin.authorize(Id,Id,Boolean,Auth.InvocationContext)",
	"apex:Auth.ConnectedAppPlugin.customAttributes(Id,Id,Map<String,String>,Auth.InvocationContext)",
	"apex:Auth.ConnectedAppPlugin.modifySAMLResponse(Map<String,String>,Id,dom.XmlNode)",
	"apex:Auth.ConnectedAppPlugin.refresh(Id,Id)",
	"apex:Auth.ConnectedAppPlugin.refresh(Id,Id,Auth.InvocationContext)",
	"apex:Auth.CustomOneTimePasswordDeliveryHandler.sendOneTimePassword(Id,String,String,String,Id,String)",
	"apex:Auth.ExternalClientAppOauthHandler.authorize(Id,Id,Boolean,Auth.InvocationContext)",
	"apex:Auth.ExternalClientAppOauthHandler.customAttributes(Id,Id,Map<String,String>,Auth.InvocationContext)",
	"apex:Auth.ExternalClientAppOauthHandler.refresh(Id,Id,Auth.InvocationContext)",
	"apex:Auth.HeadlessSelfRegistrationHandler.createUser(Id,Auth.UserData,String,String,String)",
	"apex:Auth.HeadlessUserDiscoveryHandler.discoverUserFromLoginHint(Id,String,Auth.VerificationAction,String,Map<String,String>)",
	"apex:Auth.HttpCalloutMockUtil.setHttpMock(HttpCalloutMock)",
	"apex:Auth.JWTUtil.validateJWTWithCert(String,String)",
	"apex:Auth.JWTUtil.validateJWTWithKey(String,String)",
	"apex:Auth.LoginDiscoveryHandler.login(String,String,Map<String,String>)",
	"apex:Auth.MyDomainLoginDiscoveryHandler.login(String,String,Map<String,String>)",
	"apex:Auth.Oauth2TokenExchangeHandler.getUserForTokenSubject(Id,Auth.TokenValidationResult,Boolean,String,Auth.IntegratingAppType)",
	"apex:Auth.Oauth2TokenExchangeHandler.validateIncomingToken(String,Auth.IntegratingAppType,String,Auth.OAuth2TokenExchangeType)",
	"apex:Auth.OauthToken.revokeToken(Auth.OauthTokenType,String)",
	"apex:Auth.RegistrationHandler.createUser(Id,Auth.UserData)",
	"apex:Auth.RegistrationHandler.updateUser(Id,Id,Auth.UserData)",
	"apex:Auth.SamlJitHandler.createUser(Id,Id,Id,String,Map<String,String>,String)",
	"apex:Auth.SamlJitHandler.updateUser(Id,Id,Id,Id,String,Map<String,String>,String)",
}

func TestAuthMethodCompileShapeDepthHasExactLocalFixtureOwnership(t *testing.T) {
	root := filepath.Join("..", "..")
	fixturePath := filepath.Join(root, "docs", "fixtures", authMethodCompileShapeDepthFixture+".json")
	fixture, err := compat.LoadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Name != authMethodCompileShapeDepthFixture || fixture.Command.Kind != "check" || len(fixture.Command.Args) != 0 || len(fixture.Source) != 1 {
		t.Fatalf("fixture envelope = %#v", fixture)
	}
	if len(fixture.Evidence) != len(authMethodCompileShapeDepthIDs) {
		t.Fatalf("raw evidence rows = %d, want %d", len(fixture.Evidence), len(authMethodCompileShapeDepthIDs))
	}
	want := make(map[string]bool, len(authMethodCompileShapeDepthIDs))
	for _, id := range authMethodCompileShapeDepthIDs {
		want[id] = true
	}
	for _, item := range fixture.Evidence {
		if !want[item.SurfaceID] || item.Kind != "shape" {
			t.Fatalf("unexpected evidence row = %#v", item)
		}
	}

	evidence, err := BuildEvidenceSnapshot([]string{fixturePath})
	if err != nil {
		t.Fatal(err)
	}
	assertExactSurfaceSet(t, evidence, authMethodCompileShapeDepthIDs)
	for _, row := range evidence {
		if row.Evidence != EvidenceFixture || row.GladeShape != ShapeTypeKnown || row.GladeBehavior != BehaviorNone || len(row.Sources) != 1 || row.Sources[0] != "fixture:"+authMethodCompileShapeDepthFixture {
			t.Fatalf("%s evidence/shape/behavior/source = %s/%s/%s/%v", row.SurfaceID, row.Evidence, row.GladeShape, row.GladeBehavior, row.Sources)
		}
	}

	source := fixture.Source[0].Content
	for _, witness := range []string{
		"implements Auth.AuthProviderPlugin", "getCustomMetadataType()", "getUserInfo(Map<String,String>", "handleCallback(Map<String,String>", "initiate(Map<String,String>",
		"extends Auth.AuthProviderPluginClass", "override Auth.OAuthRefreshResult refresh(Map<String,String>",
		"Auth.CommunitiesUtil.getLogoutUrl()", "Auth.CommunitiesUtil.getUserDisplayName()", "Auth.CommunitiesUtil.isInternalUser()",
		"Auth.ConfigurableSelfRegHandler,", "Auth.ConfirmUserRegistrationHandler,", "Auth.ConnectedAppPlugin connected = null;",
		"connected.authorize(null, null, false)", "connected.authorize(null, null, false, null)", "connected.customAttributes(null, null, null, null)", "connected.modifySAMLResponse(null, null, null)", "connected.refresh(null, null)", "connected.refresh(null, null, null)",
		"Auth.CustomOneTimePasswordDeliveryHandler,", "Auth.ExternalClientAppOauthHandler,", "Auth.HeadlessSelfRegistrationHandler,", "Auth.HeadlessUserDiscoveryHandler,",
		"Auth.HttpCalloutMockUtil.setHttpMock(null)", "Auth.JWTUtil.validateJWTWithCert(null, null)", "Auth.JWTUtil.validateJWTWithKey(null, null)",
		"Auth.LoginDiscoveryHandler,", "Auth.MyDomainLoginDiscoveryHandler,", "Auth.Oauth2TokenExchangeHandler,",
		"Auth.OauthToken.revokeToken(null, null)", "Auth.RegistrationHandler,", "Auth.SamlJitHandler {",
	} {
		if !strings.Contains(source, witness) {
			t.Fatalf("source missing direct shape witness %q", witness)
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
	for _, id := range authMethodCompileShapeDepthIDs {
		if ownership[id] != 1 {
			t.Fatalf("fixture ownership for %s = %d, want exactly one", id, ownership[id])
		}
	}

	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	var policy struct {
		SalesforceEligible        *bool  `json:"salesforceEligible"`
		SalesforceExclusionClass  string `json:"salesforceExclusionClass"`
		SalesforceExclusionReason string `json:"salesforceExclusionReason"`
	}
	if err := json.Unmarshal(data, &policy); err != nil {
		t.Fatal(err)
	}
	if policy.SalesforceEligible == nil || *policy.SalesforceEligible || policy.SalesforceExclusionClass != "policy-local-only" || !strings.Contains(policy.SalesforceExclusionReason, "zero Salesforce parity") || !strings.Contains(policy.SalesforceExclusionReason, "hosted") {
		t.Fatalf("fixture policy = %#v", policy)
	}
	if result, err := compat.Run(fixture); err != nil || !result.OK {
		t.Fatalf("fixture check = %#v, error = %v", result, err)
	}
}
