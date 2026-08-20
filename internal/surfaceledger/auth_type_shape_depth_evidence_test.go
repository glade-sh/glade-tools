package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

const authTypeShapeDepthFixture = "core-runtime-g3-auth-type-shape-depth.json"

var authTypeShapeDepthTypes = []string{
	"AuthConfig",
	"AuthProviderCallbackState",
	"AuthProviderPlugin",
	"AuthProviderPluginClass",
	"AuthProviderTokenResponse",
	"ConfigurableSelfRegHandler",
	"ConfirmUserRegistrationHandler",
	"ConnectedAppPlugin",
	"CustomOneTimePasswordDeliveryHandler",
	"CustomOneTimePasswordDeliveryResult",
	"ExternalClientAppOauthHandler",
	"GeneratedUserData",
	"HeadlessSelfRegistrationHandler",
	"HeadlessUserDiscoveryHandler",
	"HeadlessUserDiscoveryResponse",
	"HttpCalloutMockUtil",
	"IntegratingAppType",
	"InvocationContext",
	"JWS",
	"JWTBearerTokenExchange",
	"JWTUtil",
	"JsonValueOutput",
	"LightningLoginEligibility",
	"LoginDiscoveryHandler",
	"LoginDiscoveryMethod",
	"MyDomainLoginDiscoveryHandler",
	"OAuth2TokenExchangeType",
	"OAuthRefreshResult",
	"Oauth2TokenExchangeHandler",
	"OauthToken",
	"OauthTokenType",
	"SamlJitHandler",
	"SessionLevel",
	"TokenValidationResult",
	"UserOrgInfo",
	"VerificationAction",
	"VerificationPolicy",
}

func TestAuthTypeShapeDepthHasExactLocalFixtureOwnership(t *testing.T) {
	root := filepath.Join("..", "..")
	fixturePath := filepath.Join(root, "docs", "fixtures", authTypeShapeDepthFixture)
	fixture, err := compat.LoadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Name != strings.TrimSuffix(authTypeShapeDepthFixture, ".json") || fixture.Command.Kind != "check" || len(fixture.Source) != 1 || len(fixture.Command.Args) != 1 || fixture.Source[0].Content != fixture.Command.Args[0] {
		t.Fatalf("fixture execution envelope = %#v", fixture)
	}

	wantIDs := make([]string, 0, len(authTypeShapeDepthTypes))
	for _, typeName := range authTypeShapeDepthTypes {
		wantIDs = append(wantIDs, "apex:Auth."+typeName)
	}
	if len(fixture.Evidence) != len(wantIDs) {
		t.Fatalf("raw evidence rows = %d, want %d", len(fixture.Evidence), len(wantIDs))
	}
	for _, item := range fixture.Evidence {
		if item.Kind != "shape" {
			t.Fatalf("raw evidence %s kind = %q, want shape", item.SurfaceID, item.Kind)
		}
	}
	evidence, err := BuildEvidenceSnapshot([]string{fixturePath})
	if err != nil {
		t.Fatal(err)
	}
	assertExactSurfaceSet(t, evidence, wantIDs)
	for _, row := range evidence {
		if row.Evidence != EvidenceFixture || row.GladeShape == ShapeAbsent || row.GladeBehavior != BehaviorNone || len(row.Sources) != 1 || row.Sources[0] != "fixture:"+strings.TrimSuffix(authTypeShapeDepthFixture, ".json") {
			t.Fatalf("%s evidence/shape/behavior = %s/%s/%s, want fixture/present/none", row.SurfaceID, row.Evidence, row.GladeShape, row.GladeBehavior)
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
		Mode                      string `json:"mode"`
	}
	if err := json.Unmarshal(data, &policy); err != nil {
		t.Fatal(err)
	}
	if policy.Mode != "compile-shape" || policy.SalesforceEligible == nil || *policy.SalesforceEligible || policy.SalesforceExclusionClass != "policy-local-only" || !strings.Contains(policy.SalesforceExclusionReason, "zero Salesforce parity") {
		t.Fatalf("fixture policy = %#v", policy)
	}

	source := fixture.Source[0].Content
	for _, typeName := range authTypeShapeDepthTypes {
		if !strings.Contains(source, "Auth."+typeName+" ") {
			t.Fatalf("source lacks direct Auth.%s declaration", typeName)
		}
	}

	paths, err := filepath.Glob(filepath.Join(root, "docs", "fixtures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	wantSet := make(map[string]bool, len(wantIDs))
	for _, id := range wantIDs {
		wantSet[id] = true
	}
	counts := make(map[string]int, len(wantIDs))
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
			if wantSet[item.SurfaceID] {
				counts[item.SurfaceID]++
			}
		}
	}
	for _, id := range wantIDs {
		if counts[id] != 1 {
			t.Fatalf("fixture ownership for %s = %d, want exactly one non-evidenceOnly owner", id, counts[id])
		}
	}

	if result, err := compat.Run(fixture); err != nil || !result.OK {
		t.Fatalf("fixture check = %#v, error = %v", result, err)
	}
}
