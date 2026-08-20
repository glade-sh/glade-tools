package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

const authPropertyShapeDepthFixture = "core-runtime-g3-auth-property-shape-depth.json"

// Frozen from accepted depth02 SOURCE_PROFILE_PRIVATE_USAGE plus SURFACE_LEDGER:
// namespace Auth, compile-shape-required, evidence none, ledger kind property.
var authPropertyShapeDepthIDs = []string{
	"apex:Auth.AuthProviderCallbackState.body",
	"apex:Auth.AuthProviderCallbackState.headers",
	"apex:Auth.AuthProviderCallbackState.queryParameters",
	"apex:Auth.AuthProviderTokenResponse.idToken",
	"apex:Auth.AuthProviderTokenResponse.oauthSecretOrRefreshToken",
	"apex:Auth.AuthProviderTokenResponse.oauthToken",
	"apex:Auth.AuthProviderTokenResponse.provider",
	"apex:Auth.AuthProviderTokenResponse.state",
	"apex:Auth.GeneratedUserData.alias",
	"apex:Auth.GeneratedUserData.email",
	"apex:Auth.GeneratedUserData.emailEncodingKey",
	"apex:Auth.GeneratedUserData.firstName",
	"apex:Auth.GeneratedUserData.languageLocaleKey",
	"apex:Auth.GeneratedUserData.lastName",
	"apex:Auth.GeneratedUserData.localesIdKey",
	"apex:Auth.GeneratedUserData.timeZoneSidKey",
	"apex:Auth.GeneratedUserData.username",
	"apex:Auth.HeadlessUserDiscoveryResponse.customErrorMessage",
	"apex:Auth.HeadlessUserDiscoveryResponse.userIds",
	"apex:Auth.JsonValueOutput.booleanValue",
	"apex:Auth.JsonValueOutput.doubleValue",
	"apex:Auth.JsonValueOutput.integerValue",
	"apex:Auth.JsonValueOutput.jsonArrayValue",
	"apex:Auth.JsonValueOutput.jsonStringValue",
	"apex:Auth.JsonValueOutput.stringValue",
	"apex:Auth.OAuthRefreshResult.accessToken",
	"apex:Auth.OAuthRefreshResult.error",
	"apex:Auth.OAuthRefreshResult.refreshToken",
	"apex:Auth.TokenValidationResult.customErrorMsg",
	"apex:Auth.TokenValidationResult.data",
	"apex:Auth.TokenValidationResult.isValid",
	"apex:Auth.TokenValidationResult.token",
	"apex:Auth.TokenValidationResult.tokenType",
	"apex:Auth.TokenValidationResult.userData",
	"apex:Auth.UserData.attributeMap",
	"apex:Auth.UserData.email",
	"apex:Auth.UserData.firstName",
	"apex:Auth.UserData.fullName",
	"apex:Auth.UserData.idToken",
	"apex:Auth.UserData.idTokenJSONString",
	"apex:Auth.UserData.identifier",
	"apex:Auth.UserData.lastName",
	"apex:Auth.UserData.link",
	"apex:Auth.UserData.locale",
	"apex:Auth.UserData.provider",
	"apex:Auth.UserData.siteLoginUrl",
	"apex:Auth.UserData.userInfoJSONString",
	"apex:Auth.UserData.username",
	"apex:Auth.VerificationResult.message",
	"apex:Auth.VerificationResult.redirect",
	"apex:Auth.VerificationResult.success",
}

func TestAuthPropertyShapeDepthHasExactLocalFixtureOwnership(t *testing.T) {
	root := filepath.Join("..", "..")
	fixturePath := filepath.Join(root, "docs", "fixtures", authPropertyShapeDepthFixture)
	fixture, err := compat.LoadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Name != strings.TrimSuffix(authPropertyShapeDepthFixture, ".json") || fixture.Command.Kind != "check" || len(fixture.Source) != 1 || len(fixture.Command.Args) != 1 || fixture.Source[0].Content != fixture.Command.Args[0] {
		t.Fatalf("fixture execution envelope = %#v", fixture)
	}
	if len(fixture.Evidence) != len(authPropertyShapeDepthIDs) {
		t.Fatalf("raw evidence rows = %d, want %d", len(fixture.Evidence), len(authPropertyShapeDepthIDs))
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
	assertExactSurfaceSet(t, evidence, authPropertyShapeDepthIDs)
	owner := "fixture:" + strings.TrimSuffix(authPropertyShapeDepthFixture, ".json")
	for _, row := range evidence {
		if row.Evidence != EvidenceFixture || row.GladeShape == ShapeAbsent || row.GladeBehavior != BehaviorNone || len(row.Sources) != 1 || row.Sources[0] != owner {
			t.Fatalf("%s evidence/shape/behavior/source = %s/%s/%s/%v, want fixture/present/none/%s", row.SurfaceID, row.Evidence, row.GladeShape, row.GladeBehavior, row.Sources, owner)
		}
	}

	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	var policy struct {
		Mode                      string `json:"mode"`
		ProofObligation           string `json:"proofObligation"`
		SalesforceEligible        *bool  `json:"salesforceEligible"`
		SalesforceExclusionClass  string `json:"salesforceExclusionClass"`
		SalesforceExclusionReason string `json:"salesforceExclusionReason"`
	}
	if err := json.Unmarshal(data, &policy); err != nil {
		t.Fatal(err)
	}
	if policy.Mode != "compile-shape" || policy.ProofObligation != "compile-shape-required" || policy.SalesforceEligible == nil || *policy.SalesforceEligible || policy.SalesforceExclusionClass != "policy-local-only" || !strings.Contains(policy.SalesforceExclusionReason, "zero Salesforce parity") {
		t.Fatalf("fixture policy = %#v", policy)
	}

	source := fixture.Source[0].Content
	for _, id := range authPropertyShapeDepthIDs {
		typeName, property := authPropertyParts(id)
		variable := lowerFirst(typeName)
		for _, witness := range []string{
			"Auth." + typeName + " " + variable + " = null;",
			"Object " + variable + "_" + property + " = " + variable + "." + property + ";",
		} {
			if !strings.Contains(source, witness) {
				t.Fatalf("source lacks direct %s witness for %s", witness, id)
			}
		}
	}

	paths, err := filepath.Glob(filepath.Join(root, "docs", "fixtures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	wantSet := make(map[string]bool, len(authPropertyShapeDepthIDs))
	for _, id := range authPropertyShapeDepthIDs {
		wantSet[id] = true
	}
	counts := make(map[string]int, len(authPropertyShapeDepthIDs))
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
	for _, id := range authPropertyShapeDepthIDs {
		if counts[id] != 1 {
			t.Fatalf("fixture ownership for %s = %d, want exactly one non-evidenceOnly owner", id, counts[id])
		}
	}

	if result, err := compat.Run(fixture); err != nil || !result.OK {
		t.Fatalf("fixture check = %#v, error = %v", result, err)
	}
}

func authPropertyParts(id string) (string, string) {
	parts := strings.SplitN(strings.TrimPrefix(id, "apex:Auth."), ".", 2)
	return parts[0], parts[1]
}

func lowerFirst(value string) string {
	return strings.ToLower(value[:1]) + value[1:]
}
