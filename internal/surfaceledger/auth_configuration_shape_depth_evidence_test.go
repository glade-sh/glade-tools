package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

const authConfigurationShapeDepthFixture = "core-runtime-auth-approval-shape.json"
const authConfigurationUnsupportedID = "apex:Auth.AuthConfiguration.getRightFrameUrl()"

// Accepted depth02 has 26 AuthConfiguration method/constructor shape gaps. This
// success fixture covers 25; getRightFrameUrl is an explicit local
// GLADESEMA028 rejection and requires separate negative evidence.
var authConfigurationShapeDepthIDs = []string{
	"apex:Auth.AuthConfiguration.AuthConfiguration(String,String)",
	"apex:Auth.AuthConfiguration.getAllowInternalUserLoginEnabled()",
	"apex:Auth.AuthConfiguration.getAuthConfig()",
	"apex:Auth.AuthConfiguration.getAuthConfigProviders()",
	"apex:Auth.AuthConfiguration.getAuthProviderSsoDomainUrl(String,String,String)",
	"apex:Auth.AuthConfiguration.getAuthProviders()",
	"apex:Auth.AuthConfiguration.getBackgroundColor()",
	"apex:Auth.AuthConfiguration.getCertificateLoginEnabled(String)",
	"apex:Auth.AuthConfiguration.getCertificateLoginUrl(String,String)",
	"apex:Auth.AuthConfiguration.getDefaultProfileForRegistration()",
	"apex:Auth.AuthConfiguration.getFooterText()",
	"apex:Auth.AuthConfiguration.getForgotPasswordUrl()",
	"apex:Auth.AuthConfiguration.getHeadlessForgotPasswordEnabled()",
	"apex:Auth.AuthConfiguration.getHeadlessFrgtPswEnabled()",
	"apex:Auth.AuthConfiguration.getHeadlessPasswordlessLoginEnabled()",
	"apex:Auth.AuthConfiguration.getHeadlessRegistrationEnabled()",
	"apex:Auth.AuthConfiguration.getLoginRightFrameUrl()",
	"apex:Auth.AuthConfiguration.getLogoUrl()",
	"apex:Auth.AuthConfiguration.getSamlProviders()",
	"apex:Auth.AuthConfiguration.getSamlSsoUrl(String,String,String)",
	"apex:Auth.AuthConfiguration.getSelfRegistrationEnabled()",
	"apex:Auth.AuthConfiguration.getSelfRegistrationUrl()",
	"apex:Auth.AuthConfiguration.getStartUrl()",
	"apex:Auth.AuthConfiguration.getUsernamePasswordEnabled()",
	"apex:Auth.AuthConfiguration.isCommunityUsingSiteAsContainer()",
}

var authConfigurationExistingIDs = []string{
	"apex:Auth.AuthConfiguration.getAuthProviderSsoUrl(String,String,String)",
	"apex:Auth.AuthProviderCallbackState.AuthProviderCallbackState(Map<String,String>,String,Map<String,String>)",
	"apex:Auth.AuthProviderPluginClass.handleCallback(Map<String,String>,Auth.AuthProviderCallbackState)",
}

func TestAuthConfigurationShapeDepthHasExactLocalFixtureOwnership(t *testing.T) {
	root := filepath.Join("..", "..")
	fixturePath := filepath.Join(root, "docs", "fixtures", authConfigurationShapeDepthFixture)
	fixture, err := compat.LoadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Command.Kind != "check" || len(fixture.Source) != 2 || len(fixture.Command.Args) != 1 {
		t.Fatalf("fixture execution envelope = %#v", fixture)
	}
	if fixture.Source[1].Content != fixture.Command.Args[0] {
		t.Fatal("AuthConfiguration source != command.args[0]")
	}

	want := make(map[string]bool, len(authConfigurationShapeDepthIDs))
	for _, id := range authConfigurationShapeDepthIDs {
		want[id] = true
	}
	if len(fixture.Evidence) != len(authConfigurationShapeDepthIDs)+len(authConfigurationExistingIDs) {
		t.Fatalf("raw evidence rows = %d, want %d", len(fixture.Evidence), len(authConfigurationShapeDepthIDs)+len(authConfigurationExistingIDs))
	}
	counts := make(map[string]int, len(fixture.Evidence))
	for _, item := range fixture.Evidence {
		if item.SurfaceID == authConfigurationUnsupportedID {
			t.Fatalf("unsupported %s is not eligible for success evidence", item.SurfaceID)
		}
		counts[item.SurfaceID]++
		if want[item.SurfaceID] && item.Kind != "shape" {
			t.Fatalf("target %s kind = %q, want shape", item.SurfaceID, item.Kind)
		}
	}
	for _, id := range authConfigurationShapeDepthIDs {
		if counts[id] != 1 {
			t.Fatalf("target %s raw rows = %d, want exactly one", id, counts[id])
		}
	}
	for _, id := range authConfigurationExistingIDs {
		if counts[id] != 1 {
			t.Fatalf("existing %s raw rows = %d, want exactly one", id, counts[id])
		}
	}

	source := fixture.Source[1].Content
	witnesses := map[string]string{
		"apex:Auth.AuthConfiguration.AuthConfiguration(String,String)":                  "new Auth.AuthConfiguration('https://community.example.invalid', '/start')",
		"apex:Auth.AuthConfiguration.getAllowInternalUserLoginEnabled()":                "cfg.getAllowInternalUserLoginEnabled()",
		"apex:Auth.AuthConfiguration.getAuthConfig()":                                   "cfg.getAuthConfig()",
		"apex:Auth.AuthConfiguration.getAuthConfigProviders()":                          "cfg.getAuthConfigProviders()",
		"apex:Auth.AuthConfiguration.getAuthProviderSsoDomainUrl(String,String,String)": "Auth.AuthConfiguration.getAuthProviderSsoDomainUrl('provider', '/start', 'site')",
		"apex:Auth.AuthConfiguration.getAuthProviders()":                                "cfg.getAuthProviders()",
		"apex:Auth.AuthConfiguration.getBackgroundColor()":                              "cfg.getBackgroundColor()",
		"apex:Auth.AuthConfiguration.getCertificateLoginEnabled(String)":                "cfg.getCertificateLoginEnabled('site')",
		"apex:Auth.AuthConfiguration.getCertificateLoginUrl(String,String)":             "Auth.AuthConfiguration.getCertificateLoginUrl('site', '/start')",
		"apex:Auth.AuthConfiguration.getDefaultProfileForRegistration()":                "cfg.getDefaultProfileForRegistration()",
		"apex:Auth.AuthConfiguration.getFooterText()":                                   "cfg.getFooterText()",
		"apex:Auth.AuthConfiguration.getForgotPasswordUrl()":                            "cfg.getForgotPasswordUrl()",
		"apex:Auth.AuthConfiguration.getHeadlessForgotPasswordEnabled()":                "cfg.getHeadlessForgotPasswordEnabled()",
		"apex:Auth.AuthConfiguration.getHeadlessFrgtPswEnabled()":                       "cfg.getHeadlessFrgtPswEnabled()",
		"apex:Auth.AuthConfiguration.getHeadlessPasswordlessLoginEnabled()":             "cfg.getHeadlessPasswordlessLoginEnabled()",
		"apex:Auth.AuthConfiguration.getHeadlessRegistrationEnabled()":                  "cfg.getHeadlessRegistrationEnabled()",
		"apex:Auth.AuthConfiguration.getLoginRightFrameUrl()":                           "cfg.getLoginRightFrameUrl()",
		"apex:Auth.AuthConfiguration.getLogoUrl()":                                      "cfg.getLogoUrl()",
		"apex:Auth.AuthConfiguration.getSamlProviders()":                                "cfg.getSamlProviders()",
		"apex:Auth.AuthConfiguration.getSamlSsoUrl(String,String,String)":               "Auth.AuthConfiguration.getSamlSsoUrl('provider', '/start', 'site')",
		"apex:Auth.AuthConfiguration.getSelfRegistrationEnabled()":                      "cfg.getSelfRegistrationEnabled()",
		"apex:Auth.AuthConfiguration.getSelfRegistrationUrl()":                          "cfg.getSelfRegistrationUrl()",
		"apex:Auth.AuthConfiguration.getStartUrl()":                                     "cfg.getStartUrl()",
		"apex:Auth.AuthConfiguration.getUsernamePasswordEnabled()":                      "cfg.getUsernamePasswordEnabled()",
		"apex:Auth.AuthConfiguration.isCommunityUsingSiteAsContainer()":                 "cfg.isCommunityUsingSiteAsContainer()",
	}
	for _, id := range authConfigurationShapeDepthIDs {
		if witness := witnesses[id]; witness == "" || !strings.Contains(source, witness) {
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
	for _, id := range authConfigurationShapeDepthIDs {
		rows := rowsBySurfaceID(evidence, id)
		if len(rows) != 1 || rows[0].Evidence != EvidenceFixture || rows[0].GladeShape == ShapeAbsent || rows[0].GladeBehavior != BehaviorNone || len(rows[0].Sources) != 1 || rows[0].Sources[0] != "fixture:"+strings.TrimSuffix(authConfigurationShapeDepthFixture, ".json") {
			t.Fatalf("%s snapshot rows = %#v", id, rows)
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
	for _, id := range authConfigurationShapeDepthIDs {
		if ownership[id] != 1 {
			t.Fatalf("fixture ownership for %s = %d, want exactly one non-evidenceOnly owner", id, ownership[id])
		}
	}

	if result, err := compat.Run(fixture); err != nil || !result.OK {
		t.Fatalf("fixture check = %#v, error = %v", result, err)
	}
}

func rowsBySurfaceID(rows []SurfaceLedgerRow, surfaceID string) []SurfaceLedgerRow {
	var matches []SurfaceLedgerRow
	for _, row := range rows {
		if row.SurfaceID == surfaceID {
			matches = append(matches, row)
		}
	}
	return matches
}
