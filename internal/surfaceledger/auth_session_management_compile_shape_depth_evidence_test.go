package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

const authSessionManagementCompileShapeDepthFixture = "core-runtime-auth-session-management-compile-shape"

var authSessionManagementCompileShapeDepthIDs = []string{
	"apex:Auth.SessionManagement.finishLoginDiscovery(Auth.LoginDiscoveryMethod,Id)",
	"apex:Auth.SessionManagement.finishLoginFlow()",
	"apex:Auth.SessionManagement.finishLoginFlow(String)",
	"apex:Auth.SessionManagement.generateVerificationUrl(Auth.VerificationPolicy,String,String)",
	"apex:Auth.SessionManagement.getLightningLoginEligibility(Id)",
	"apex:Auth.SessionManagement.getQrCode()",
	"apex:Auth.SessionManagement.getRequiredSessionLevelForProfile(String)",
	"apex:Auth.SessionManagement.ignoreForConcurrentSessionLimit(Object)",
	"apex:Auth.SessionManagement.inOrgNetworkRange(String)",
	"apex:Auth.SessionManagement.isIpAllowedForProfile(String,String)",
	"apex:Auth.SessionManagement.setSessionLevel(Auth.SessionLevel)",
	"apex:Auth.SessionManagement.validateTotpTokenForKey(String,String)",
	"apex:Auth.SessionManagement.validateTotpTokenForKey(String,String,String)",
	"apex:Auth.SessionManagement.validateTotpTokenForUser(String)",
	"apex:Auth.SessionManagement.validateTotpTokenForUser(String,String)",
	"apex:Auth.SessionManagement.verifyDeviceFlow(String,String)",
}

func TestAuthSessionManagementCompileShapeDepthHasExactLocalFixtureOwnership(t *testing.T) {
	root := filepath.Join("..", "..")
	fixturePath := filepath.Join(root, "docs", "fixtures", authSessionManagementCompileShapeDepthFixture+".json")
	fixture, err := compat.LoadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Command.Kind != "check" || len(fixture.Source) != 1 || len(fixture.Command.Args) != 0 {
		t.Fatalf("fixture envelope = %#v", fixture)
	}
	if len(fixture.Evidence) != len(authSessionManagementCompileShapeDepthIDs) {
		t.Fatalf("raw evidence rows = %d, want %d", len(fixture.Evidence), len(authSessionManagementCompileShapeDepthIDs))
	}
	for _, item := range fixture.Evidence {
		if item.Kind != "shape" {
			t.Fatalf("%s evidence kind = %q, want shape", item.SurfaceID, item.Kind)
		}
	}

	want := make(map[string]bool, len(authSessionManagementCompileShapeDepthIDs))
	for _, id := range authSessionManagementCompileShapeDepthIDs {
		want[id] = true
	}
	evidence, err := BuildEvidenceSnapshot([]string{fixturePath})
	if err != nil {
		t.Fatal(err)
	}
	assertExactSurfaceSet(t, evidence, authSessionManagementCompileShapeDepthIDs)
	for _, row := range evidence {
		if !want[row.SurfaceID] || row.Evidence != EvidenceFixture || row.GladeShape == ShapeAbsent || row.GladeBehavior != BehaviorNone || len(row.Sources) != 1 || row.Sources[0] != "fixture:"+authSessionManagementCompileShapeDepthFixture {
			t.Fatalf("%s evidence/shape/behavior/source = %s/%s/%s/%v", row.SurfaceID, row.Evidence, row.GladeShape, row.GladeBehavior, row.Sources)
		}
	}

	source := fixture.Source[0].Content
	witnesses := []string{
		"Auth.SessionManagement.finishLoginDiscovery(null, null);",
		"Auth.SessionManagement.finishLoginFlow();",
		"Auth.SessionManagement.finishLoginFlow('start');",
		"Auth.SessionManagement.generateVerificationUrl(null, 'description', '/destination');",
		"Auth.SessionManagement.getLightningLoginEligibility(null);",
		"Auth.SessionManagement.getQrCode();",
		"Auth.SessionManagement.getRequiredSessionLevelForProfile('profile');",
		"Auth.SessionManagement.ignoreForConcurrentSessionLimit(null);",
		"Auth.SessionManagement.inOrgNetworkRange('10.0.0.1');",
		"Auth.SessionManagement.isIpAllowedForProfile('10.0.0.1', 'profile');",
		"Auth.SessionManagement.setSessionLevel(null);",
		"Auth.SessionManagement.validateTotpTokenForKey('key', 'token');",
		"Auth.SessionManagement.validateTotpTokenForKey('key', 'token', 'user');",
		"Auth.SessionManagement.validateTotpTokenForUser('user');",
		"Auth.SessionManagement.validateTotpTokenForUser('user', 'token');",
		"Auth.SessionManagement.verifyDeviceFlow('user-code', 'start');",
	}
	for _, witness := range witnesses {
		if !strings.Contains(source, witness) {
			t.Fatalf("source missing direct witness %q", witness)
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
	if policy.Mode != "compile-shape" || policy.SalesforceEligible == nil || *policy.SalesforceEligible || policy.SalesforceExclusionClass != "policy-local-only" || !strings.Contains(policy.SalesforceExclusionReason, "zero Salesforce parity") || !strings.Contains(policy.SalesforceExclusionReason, "execution remains unsupported") {
		t.Fatalf("fixture policy = %#v", policy)
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
	for _, id := range authSessionManagementCompileShapeDepthIDs {
		if ownership[id] != 1 {
			t.Fatalf("fixture ownership for %s = %d, want exactly one", id, ownership[id])
		}
	}

	if result, err := compat.Run(fixture); err != nil || !result.OK {
		t.Fatalf("fixture check = %#v, error = %v", result, err)
	}
}
