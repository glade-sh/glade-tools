package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

const userInfoRequestTailFixture = "core-runtime-userinfo-request-tail-api67.json"

var userInfoRequestTailIDs = []string{
	"apex:System.Request",
	"apex:System.Request.clone()",
	"apex:System.Request.getCurrent()",
	"apex:System.Request.getQuiddity()",
	"apex:System.Request.getRequestId()",
	"apex:System.UserInfo",
	"apex:System.UserInfo.UserInfo()",
	"apex:System.UserInfo.clone()",
	"apex:System.UserInfo.getCurrentUvid()",
	"apex:System.UserInfo.getDefaultCurrency()",
	"apex:System.UserInfo.getFirstName",
	"apex:System.UserInfo.getFirstName()",
	"apex:System.UserInfo.getLanguage",
	"apex:System.UserInfo.getLanguage()",
	"apex:System.UserInfo.getLastName",
	"apex:System.UserInfo.getLastName()",
	"apex:System.UserInfo.getLocale",
	"apex:System.UserInfo.getLocale()",
	"apex:System.UserInfo.getName",
	"apex:System.UserInfo.getName()",
	"apex:System.UserInfo.getOrganizationId",
	"apex:System.UserInfo.getOrganizationId()",
	"apex:System.UserInfo.getOrganizationName()",
	"apex:System.UserInfo.getProfileId",
	"apex:System.UserInfo.getProfileId()",
	"apex:System.UserInfo.getSessionId",
	"apex:System.UserInfo.getSessionId()",
	"apex:System.UserInfo.getTimeZone",
	"apex:System.UserInfo.getTimeZone()",
	"apex:System.UserInfo.getUiTheme()",
	"apex:System.UserInfo.getUiThemeDisplayed()",
	"apex:System.UserInfo.getUserEmail",
	"apex:System.UserInfo.getUserEmail()",
	"apex:System.UserInfo.getUserId",
	"apex:System.UserInfo.getUserId()",
	"apex:System.UserInfo.getUserName",
	"apex:System.UserInfo.getUserName()",
	"apex:System.UserInfo.getUserRoleId()",
	"apex:System.UserInfo.getUserType",
	"apex:System.UserInfo.getUserType()",
	"apex:System.UserInfo.isCurrentUserLicensed(String)",
	"apex:System.UserInfo.isMultiCurrencyOrganization",
	"apex:System.UserInfo.isMultiCurrencyOrganization()",
}

func TestUserInfoRequestTailHasExactExecutableLocalEvidence(t *testing.T) {
	root := filepath.Join("..", "..")
	if len(userInfoRequestTailIDs) != 43 {
		t.Fatalf("UserInfo/Request IDs = %d, want 43", len(userInfoRequestTailIDs))
	}
	fixturePath := filepath.Join(root, "docs", "fixtures", userInfoRequestTailFixture)
	fixture, err := compat.LoadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Name != strings.TrimSuffix(userInfoRequestTailFixture, ".json") || fixture.Command.Kind != "exec" || len(fixture.Command.Args) != 1 || len(fixture.Source) != 1 || fixture.Source[0].Path != "anonymous.apex" || fixture.Source[0].Content != fixture.Command.Args[0] {
		t.Fatalf("fixture execution envelope = %#v", fixture)
	}

	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	var metadata struct {
		APIVersion                string `json:"apiVersion"`
		Mode                      string `json:"mode"`
		Notes                     string `json:"notes"`
		EvidenceOnly              bool   `json:"evidenceOnly"`
		SalesforceEligible        *bool  `json:"salesforceEligible"`
		SalesforceExclusionClass  string `json:"salesforceExclusionClass"`
		SalesforceExclusionReason string `json:"salesforceExclusionReason"`
		Salesforce                any    `json:"salesforce"`
		Comparisons               any    `json:"comparisons"`
		Candidate                 struct {
			Commit string `json:"commit"`
			SHA256 string `json:"sha256"`
		} `json:"candidate"`
		Profile struct {
			CandidateCommit string `json:"candidateCommit"`
			CandidateSHA256 string `json:"candidateSha256"`
			SelectedRows    int    `json:"selectedRowCount"`
		} `json:"profile"`
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.APIVersion != "67.0" || metadata.Mode != "local-runtime" || metadata.EvidenceOnly || metadata.SalesforceEligible == nil || *metadata.SalesforceEligible || metadata.SalesforceExclusionClass != "policy-local-only" || !strings.Contains(metadata.SalesforceExclusionReason, "zero Salesforce parity") || metadata.Candidate.Commit != "3409c4c85827b19712e9df83fc8905aa02bd1dc8" || metadata.Candidate.SHA256 != "960ac9f26fa92aae6054cbe0e59f9c4ab1f84397df67bd8a89528068d02a1fce" || metadata.Profile.CandidateCommit != metadata.Candidate.Commit || metadata.Profile.CandidateSHA256 != metadata.Candidate.SHA256 || metadata.Profile.SelectedRows != len(userInfoRequestTailIDs) {
		t.Fatalf("fixture provenance = %#v", metadata)
	}
	if metadata.Salesforce != nil || metadata.Comparisons != nil || !strings.Contains(metadata.Notes, "deterministic local identity/request context") || !strings.Contains(metadata.Notes, "no real org/session/OAuth") {
		t.Fatalf("fixture makes an unsupported Salesforce claim: %#v", metadata)
	}

	want := mapFromIDs(userInfoRequestTailIDs)
	seen := make(map[string]bool, len(want))
	for _, row := range fixture.Evidence {
		if row.Kind != "exec" || !want[row.SurfaceID] || seen[row.SurfaceID] {
			t.Fatalf("unexpected or duplicate evidence row: %#v", row)
		}
		seen[row.SurfaceID] = true
	}
	if len(seen) != len(userInfoRequestTailIDs) {
		t.Fatalf("fixture evidence rows = %d, want %d", len(seen), len(userInfoRequestTailIDs))
	}

	evidence, err := BuildEvidenceSnapshot([]string{fixturePath})
	if err != nil {
		t.Fatal(err)
	}
	assertExactSurfaceSet(t, evidence, userInfoRequestTailIDs)
	for _, row := range evidence {
		if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported {
			t.Fatalf("%s evidence/behavior = %s/%s, want fixture/supported", row.SurfaceID, row.Evidence, row.GladeBehavior)
		}
	}

	owners := make(map[string]int, len(want))
	paths, err := filepath.Glob(filepath.Join(root, "docs", "fixtures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		var header struct {
			EvidenceOnly bool `json:"evidenceOnly"`
			Evidence     []struct {
				SurfaceID string `json:"surfaceId"`
			} `json:"evidence"`
		}
		readJSON(t, path, &header)
		if header.EvidenceOnly {
			continue
		}
		for _, row := range header.Evidence {
			if want[row.SurfaceID] {
				owners[row.SurfaceID]++
			}
		}
	}
	for _, id := range userInfoRequestTailIDs {
		if owners[id] != 1 {
			t.Fatalf("fixture ownership for %s = %d, want exactly one non-evidenceOnly owner", id, owners[id])
		}
	}

	if result, err := compat.Run(fixture); err != nil || !result.OK {
		t.Fatalf("fixture execution = %#v, error = %v", result, err)
	}
}
