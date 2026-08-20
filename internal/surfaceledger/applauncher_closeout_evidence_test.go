package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func appLauncherCloseoutIDs() []string {
	ids := make([]string, 0, 21)
	for _, method := range []string{"getCity()", "getCountry()", "getExtraFields(String)", "getFirstName()", "getLanguage()", "getLastName()", "getLocale()", "getMobilePhone()", "getPostalCode()", "getState()", "getStreet()", "getTimeZone()", "getUserEmail()", "getWorkPhone()"} {
		ids = append(ids, "apex:applauncher.AccountSettingsController."+method)
	}
	for _, method := range []string{"getGuestUser()", "getInternalUser()", "getLoginUrl()", "getLogoutUrl()", "getPhotoUrl()", "getUserDisplayName()", "getUserId()"} {
		ids = append(ids, "apex:applauncher.IdentityHeaderController."+method)
	}
	return ids
}

func appLauncherCorrectedUnsupportedIDs() []string {
	return []string{
		"apex:applauncher.AppMenu.setAppVisibility(Id,Boolean)",
		"apex:applauncher.EmployeeLoginLinkController.getEmployeeLoginUrl(String)",
	}
}

func TestAppLauncherCloseoutHasExactExecutableOwnership(t *testing.T) {
	root := filepath.Join("..", "..")
	wantIDs := appLauncherCloseoutIDs()
	if len(wantIDs) != 21 {
		t.Fatalf("applauncher closeout IDs = %d, want 21", len(wantIDs))
	}
	allIDs := append(append([]string{}, wantIDs...), appLauncherCorrectedUnsupportedIDs()...)
	want := make(map[string]struct{}, len(allIDs))
	for _, id := range allIDs {
		want[id] = struct{}{}
	}
	paths, err := filepath.Glob(filepath.Join(root, "docs", "fixtures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := BuildEvidenceSnapshot(paths)
	if err != nil {
		t.Fatal(err)
	}
	var selected []SurfaceLedgerRow
	for _, row := range evidence {
		if _, ok := want[row.SurfaceID]; ok {
			selected = append(selected, row)
		}
	}
	assertExactSurfaceSet(t, selected, allIDs)
	for _, row := range selected {
		behavior := BehaviorSupported
		owner := "fixture:current-base-applauncher-deterministic-001-api67"
		switch row.SurfaceID {
		case "apex:applauncher.AccountSettingsController.getExtraFields(String)":
			behavior = BehaviorUnsupported
			owner = "fixture:core-runtime-applauncher-account-settings-extra-fields-unsupported"
		case "apex:applauncher.AppMenu.setAppVisibility(Id,Boolean)":
			behavior = BehaviorUnsupported
			owner = "fixture:core-runtime-applauncher-app-menu-visibility-unsupported"
		case "apex:applauncher.EmployeeLoginLinkController.getEmployeeLoginUrl(String)":
			behavior = BehaviorUnsupported
			owner = "fixture:core-runtime-applauncher-employee-login-url-unsupported"
		}
		if row.Evidence != EvidenceFixture || row.GladeBehavior != behavior || len(row.Sources) != 1 || row.Sources[0] != owner {
			t.Fatalf("%s evidence/behavior/source = %s/%s/%v", row.SurfaceID, row.Evidence, row.GladeBehavior, row.Sources)
		}
	}

	fixturePath := filepath.Join(root, "docs", "fixtures", "current-base-applauncher-deterministic-001-api67.json")
	fixture, err := compat.LoadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	if result, err := compat.Run(fixture); err != nil || !result.OK {
		t.Fatalf("fixture execution = %#v, error = %v", result, err)
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
	if policy.SalesforceEligible == nil || *policy.SalesforceEligible || policy.SalesforceExclusionClass != "policy-local-only" || !strings.Contains(policy.SalesforceExclusionReason, "without claiming") {
		t.Fatalf("fixture policy = %#v", policy)
	}
	var source strings.Builder
	for _, file := range fixture.Source {
		source.WriteString(file.Content)
	}
	for _, witness := range []string{
		"String city = applauncher.AccountSettingsController.getCity();",
		"String workPhone = applauncher.AccountSettingsController.getWorkPhone();",
		"Boolean guestUser = applauncher.IdentityHeaderController.getGuestUser();",
		"Boolean internalUser = applauncher.IdentityHeaderController.getInternalUser();",
		"String loginUrl = applauncher.IdentityHeaderController.getLoginUrl();",
		"String userId = applauncher.IdentityHeaderController.getUserId();",
		"applauncher.AppMenu.setOrgSortOrder(new List<Id>",
		"applauncher.AppMenu.setUserSortOrder(new List<Id>",
		"applauncher.EmployeeLoginLinkController.getIsAllowInternalUserLoginEnabled()",
	} {
		if !strings.Contains(source.String(), witness) {
			t.Fatalf("applauncher source missing %q", witness)
		}
	}

	for _, name := range []string{
		"core-runtime-applauncher-account-settings-extra-fields-unsupported",
		"core-runtime-applauncher-app-menu-visibility-unsupported",
		"core-runtime-applauncher-employee-login-url-unsupported",
	} {
		negativePath := filepath.Join(root, "docs", "fixtures", name+".json")
		negative, err := compat.LoadFile(negativePath)
		if err != nil {
			t.Fatal(err)
		}
		if err := compat.Validate(negative); err != nil {
			t.Fatal(err)
		}
		if result, err := compat.Run(negative); err != nil || !result.OK {
			t.Fatalf("%s execution = %#v, error = %v", name, result, err)
		}
		if len(negative.Command.Args) != 1 || len(negative.Source) != 1 || negative.Command.Args[0] != negative.Source[0].Content {
			t.Fatalf("%s source/command = %#v / %#v", name, negative.Source, negative.Command.Args)
		}
		data, err = os.ReadFile(negativePath)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &policy); err != nil {
			t.Fatal(err)
		}
		if policy.SalesforceEligible == nil || *policy.SalesforceEligible || policy.SalesforceExclusionClass != "policy-local-only" || !strings.Contains(policy.SalesforceExclusionReason, "zero Salesforce parity") {
			t.Fatalf("%s policy = %#v", name, policy)
		}
	}
}
