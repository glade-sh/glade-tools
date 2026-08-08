package surfaceledger

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func buildSeedPolicy() SupportPolicy {
	return SupportPolicy{
		Rules: []SupportPolicyRule{
			{
				Namespace:   "System",
				Disposition: DispositionLocalRuntimeRequired,
				Reason:      "language/compiler runtime",
			},
			{
				Namespace:   "Schema",
				Disposition: DispositionLocalRuntimeRequired,
				Reason:      "schema describe runtime",
			},
			{
				Namespace:   "Database",
				Disposition: DispositionLocalRuntimeRequired,
				Reason:      "DML runtime",
			},
			{
				Namespace:   "Messaging",
				Disposition: DispositionDeterministicMockRequired,
				Reason:      "messaging mock",
			},
			{
				Namespace:   "Cache",
				Disposition: DispositionDeterministicMockRequired,
				Reason:      "cache mock",
			},
			{
				Namespace:   "ConnectApi",
				Disposition: DispositionHostedDeferred,
				Reason:      "connect-api deferred",
				MemberExceptions: []SupportPolicyMemberException{
					{TypeName: "Organization", MemberName: "getSettings", Disposition: DispositionDeterministicMockRequired, Reason: "observed corpus usage"},
					{TypeName: "UserProfiles", MemberName: "setPhoto", Disposition: DispositionDeterministicMockRequired, Reason: "observed corpus usage"},
					{TypeName: "UserProfiles", MemberName: "deletePhoto", Disposition: DispositionDeterministicMockRequired, Reason: "observed corpus usage"},
					{TypeName: "Communities", MemberName: "getCommunity", Disposition: DispositionDeterministicMockRequired, Reason: "observed corpus usage"},
					{TypeName: "ChatterUsers", MemberName: "getFollowings", Disposition: DispositionDeterministicMockRequired, Reason: "observed corpus usage"},
				},
			},
			{
				Namespace:   "Reports",
				Disposition: DispositionCompileShapeRequired,
				Reason:      "compile shape pending runtime evidence",
			},
			{
				Namespace:   "Slack",
				Disposition: DispositionHostedDeferred,
				Reason:      "hosted deferred",
			},
			{
				TypeFamily:  "commerce*",
				Disposition: DispositionHostedDeferred,
				Reason:      "commerce hosted deferred",
			},
		},
	}
}

func TestRealAuthPolicyClassifiesAuthTokenOperations(t *testing.T) {
	policy, err := LoadSupportPolicy(filepath.Join("..", "..", "docs", "fixtures", "apex-local-support-policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	var authRule SupportPolicyRule
	for _, rule := range policy.Rules {
		if rule.Namespace == "Auth" {
			authRule = rule
			break
		}
	}
	if authRule.Namespace == "" {
		t.Fatal("missing Auth rule in real support policy")
	}
	rows := []SurfaceLedgerRow{
		apexMemberRow("apex:Auth.AuthToken.getAccessToken(String,String)", "Auth", "AuthToken", "getAccessToken"),
		apexMemberRow("apex:Auth.AuthToken.getAccessTokenMap(String,String)", "Auth", "AuthToken", "getAccessTokenMap"),
		apexMemberRow("apex:Auth.AuthToken.refreshAccessToken(String,String,String)", "Auth", "AuthToken", "refreshAccessToken"),
		apexMemberRow("apex:Auth.AuthToken.revokeAccess(String,String,String,String)", "Auth", "AuthToken", "revokeAccess"),
		apexRow("apex:Auth.JWT", "Auth", "JWT"),
		apexMemberRow("apex:Auth.JWTUtil.parseJWTFromStringWithoutValidation(String)", "Auth", "JWTUtil", "parseJWTFromStringWithoutValidation"),
		apexMemberRow("apex:Auth.CommunitiesUtil.isGuestUser()", "Auth", "CommunitiesUtil", "isGuestUser"),
		apexMemberRow("apex:Auth.SessionManagement.getCurrentSession()", "Auth", "SessionManagement", "getCurrentSession"),
	}
	rows = appendPolicyExceptionRows(rows, []SupportPolicyRule{authRule})
	profile := ComputeSupportProfile(rows, SupportPolicy{Rules: []SupportPolicyRule{authRule}}, nil)
	if len(profile.ValidationErrors) != 0 {
		t.Fatalf("expected no validation errors, got: %v", profile.ValidationErrors)
	}
	want := map[string]SupportDisposition{
		"apex:Auth.AuthToken.getAccessToken(String,String)":             DispositionHostedDeferred,
		"apex:Auth.AuthToken.getAccessTokenMap(String,String)":          DispositionHostedDeferred,
		"apex:Auth.AuthToken.refreshAccessToken(String,String,String)":  DispositionHostedDeferred,
		"apex:Auth.AuthToken.revokeAccess(String,String,String,String)": DispositionDeterministicMockRequired,
	}
	byID := make(map[string]SupportProfileRow, len(profile.Rows))
	for _, row := range profile.Rows {
		byID[row.SurfaceID] = row
	}
	for _, row := range profile.Rows {
		if _, ok := want[row.SurfaceID]; !ok {
			continue
		}
		wantDisposition := want[row.SurfaceID]
		if row.Disposition != wantDisposition {
			t.Errorf("%s disposition = %s, want %s", row.SurfaceID, row.Disposition, wantDisposition)
		}
		if wantDisposition == DispositionHostedDeferred && (!strings.Contains(row.Reason, "hosted") || !strings.Contains(row.Reason, "corpus")) {
			t.Errorf("%s reason = %q, want hosted-state/no-corpus reason", row.SurfaceID, row.Reason)
		}
		if wantDisposition == DispositionDeterministicMockRequired && !strings.Contains(row.Reason, "exact") {
			t.Errorf("%s reason = %q, want exact local contract reason", row.SurfaceID, row.Reason)
		}
	}
	for id := range want {
		if _, ok := byID[id]; !ok {
			t.Fatalf("missing AuthToken profile row %s", id)
		}
	}
}

func TestRealSupportPolicyClassifiesReviewedConnectApiShapes(t *testing.T) {
	policy, err := LoadSupportPolicy(filepath.Join("..", "..", "docs", "fixtures", "apex-local-support-policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	var relevantRules []SupportPolicyRule
	for _, rule := range policy.Rules {
		if rule.Namespace == "ConnectApi" {
			relevantRules = append(relevantRules, rule)
			continue
		}
		if rule.Namespace == "Auth" {
			for _, exception := range rule.MemberExceptions {
				if exception.TypeName == "AuthConfiguration" && exception.MemberName == "getAuthProviderSsoUrl" {
					rule.MemberExceptions = []SupportPolicyMemberException{exception}
					relevantRules = append(relevantRules, rule)
					break
				}
			}
		}
	}

	type reviewedSurface struct {
		id       string
		typeName string
		member   string
	}
	reviewed := []reviewedSurface{
		{"apex:ConnectApi.ChatterUsers", "ChatterUsers", ""},
		{"apex:ConnectApi.Communities", "Communities", ""},
		{"apex:ConnectApi.ConnectApiException", "ConnectApiException", ""},
		{"apex:ConnectApi.CredentialAuthenticationProtocol", "CredentialAuthenticationProtocol", ""},
		{"apex:ConnectApi.CredentialAuthenticationProtocol.Custom", "CredentialAuthenticationProtocol", "Custom"},
		{"apex:ConnectApi.CredentialPrincipalType", "CredentialPrincipalType", ""},
		{"apex:ConnectApi.CredentialPrincipalType.NamedPrincipal", "CredentialPrincipalType", "NamedPrincipal"},
		{"apex:ConnectApi.ExternalCredential", "ExternalCredential", ""},
		{"apex:ConnectApi.ExternalCredentialInput", "ExternalCredentialInput", ""},
		{"apex:ConnectApi.ExternalCredentialPrincipal", "ExternalCredentialPrincipal", ""},
		{"apex:ConnectApi.ExternalCredentialPrincipalInput", "ExternalCredentialPrincipalInput", ""},
		{"apex:ConnectApi.ManagedContent", "ManagedContent", ""},
		{"apex:ConnectApi.ManagedContentNodeValue", "ManagedContentNodeValue", ""},
		{"apex:ConnectApi.ManagedContentVersion", "ManagedContentVersion", ""},
		{"apex:ConnectApi.ManagedContentVersionCollection", "ManagedContentVersionCollection", ""},
		{"apex:ConnectApi.NamedCredential", "NamedCredential", ""},
		{"apex:ConnectApi.NamedCredentialCalloutOptions", "NamedCredentialCalloutOptions", ""},
		{"apex:ConnectApi.NamedCredentialCalloutOptionsInput", "NamedCredentialCalloutOptionsInput", ""},
		{"apex:ConnectApi.NamedCredentialInput", "NamedCredentialInput", ""},
		{"apex:ConnectApi.NamedCredentialType", "NamedCredentialType", ""},
		{"apex:ConnectApi.NamedCredentialType.SecuredEndpoint", "NamedCredentialType", "SecuredEndpoint"},
		{"apex:ConnectApi.NamedCredentials", "NamedCredentials", ""},
		{"apex:ConnectApi.Organization", "Organization", ""},
		{"apex:ConnectApi.OrganizationSettings", "OrganizationSettings", ""},
		{"apex:ConnectApi.TimeZone", "TimeZone", ""},
		{"apex:ConnectApi.UserProfiles", "UserProfiles", ""},
		{"apex:ConnectApi.UserSettings", "UserSettings", ""},
	}
	rows := make([]SurfaceLedgerRow, 0, len(reviewed)+10)
	for _, surface := range reviewed {
		if surface.member == "" {
			rows = append(rows, apexRow(surface.id, "ConnectApi", surface.typeName))
		} else {
			rows = append(rows, apexPropertyRow(surface.id, "ConnectApi", surface.typeName, surface.member))
		}
	}
	for _, service := range []struct{ typeName, member string }{
		{"Organization", "getSettings"},
		{"UserProfiles", "setPhoto"},
		{"UserProfiles", "deletePhoto"},
		{"Communities", "getCommunity"},
		{"ChatterUsers", "getFollowings"},
		{"NamedCredentials", "createExternalCredential"},
		{"NamedCredentials", "createNamedCredential"},
		{"NamedCredentials", "getExternalCredential"},
		{"ManagedContent", "getAllManagedContent"},
		{"ManagedContent", "getManagedContentByContentKeys"},
	} {
		rows = append(rows, apexMemberRow("apex:ConnectApi."+service.typeName+"."+service.member, "ConnectApi", service.typeName, service.member))
	}
	rows = append(rows, apexMemberRow("apex:Auth.AuthConfiguration.getAuthProviderSsoUrl(String,String,String)", "Auth", "AuthConfiguration", "getAuthProviderSsoUrl"))

	profile := ComputeSupportProfile(rows, SupportPolicy{Rules: relevantRules}, nil)
	if len(profile.ValidationErrors) != 0 {
		t.Fatalf("expected no validation errors, got: %v", profile.ValidationErrors)
	}
	byID := make(map[string]SupportProfileRow, len(profile.Rows))
	for _, row := range profile.Rows {
		byID[row.SurfaceID] = row
	}
	if len(reviewed) != 27 {
		t.Fatalf("test fixture must cover 27 reviewed surfaces, got %d", len(reviewed))
	}
	for _, surface := range reviewed {
		row, ok := byID[surface.id]
		if !ok {
			t.Fatalf("missing reviewed ConnectApi row %s", surface.id)
		}
		if row.Disposition != DispositionCompileShapeRequired {
			t.Errorf("%s disposition = %s, want %s", surface.id, row.Disposition, DispositionCompileShapeRequired)
		}
	}
	if profile.ByDisposition[DispositionCompileShapeRequired] != len(reviewed) {
		t.Errorf("compile-shape-required count = %d, want %d", profile.ByDisposition[DispositionCompileShapeRequired], len(reviewed))
	}
	for _, service := range []string{
		"apex:ConnectApi.Organization.getSettings",
		"apex:ConnectApi.UserProfiles.setPhoto",
		"apex:ConnectApi.UserProfiles.deletePhoto",
		"apex:ConnectApi.Communities.getCommunity",
		"apex:ConnectApi.ChatterUsers.getFollowings",
		"apex:ConnectApi.NamedCredentials.createExternalCredential",
		"apex:ConnectApi.NamedCredentials.createNamedCredential",
		"apex:ConnectApi.NamedCredentials.getExternalCredential",
		"apex:ConnectApi.ManagedContent.getAllManagedContent",
		"apex:ConnectApi.ManagedContent.getManagedContentByContentKeys",
	} {
		if row := byID[service]; row.Disposition != DispositionDeterministicMockRequired {
			t.Errorf("%s disposition = %s, want %s", service, row.Disposition, DispositionDeterministicMockRequired)
		}
	}
	unreviewed := "apex:ConnectApi.ChatterUsers.getReputation"
	rows = append(rows, apexMemberRow(unreviewed, "ConnectApi", "ChatterUsers", "getReputation"))
	profile = ComputeSupportProfile(rows, SupportPolicy{Rules: relevantRules}, nil)
	byID = make(map[string]SupportProfileRow, len(profile.Rows))
	for _, row := range profile.Rows {
		byID[row.SurfaceID] = row
	}
	if row := byID[unreviewed]; row.Disposition != DispositionHostedDeferred {
		t.Errorf("%s disposition = %s, want %s", unreviewed, row.Disposition, DispositionHostedDeferred)
	}
	sso := byID["apex:Auth.AuthConfiguration.getAuthProviderSsoUrl(String,String,String)"]
	if sso.Disposition != DispositionHostedDeferred {
		t.Errorf("Auth SSO disposition = %s, want %s", sso.Disposition, DispositionHostedDeferred)
	}
}

func TestRealSupportPolicyDefersFeatureGatedIndustriesContext(t *testing.T) {
	policy, err := LoadSupportPolicy(filepath.Join("..", "..", "docs", "fixtures", "apex-local-support-policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	var contextRules []SupportPolicyRule
	for _, rule := range policy.Rules {
		if rule.Namespace == "Context" {
			contextRules = append(contextRules, rule)
		}
	}
	rows := []SurfaceLedgerRow{
		apexRow("apex:Context.IndustriesContext", "Context", "IndustriesContext"),
		apexMemberRow("apex:Context.IndustriesContext.buildContext(Map<String,Object>)", "Context", "IndustriesContext", "buildContext"),
		apexMemberRow("apex:Context.IndustriesContext.deleteRecords(Map<String,Object>)", "Context", "IndustriesContext", "deleteRecords"),
	}
	profile := ComputeSupportProfile(rows, SupportPolicy{Rules: contextRules}, nil)
	if len(profile.ValidationErrors) != 0 {
		t.Fatalf("expected no validation errors, got: %v", profile.ValidationErrors)
	}
	if len(profile.NonDeferredGaps) != 0 {
		t.Fatalf("feature-gated IndustriesContext retained non-deferred gaps: %#v", profile.NonDeferredGaps)
	}
	for _, row := range profile.Rows {
		if row.Disposition != DispositionHostedDeferred {
			t.Errorf("%s disposition = %s, want %s", row.SurfaceID, row.Disposition, DispositionHostedDeferred)
		}
	}
}

func TestRealSupportPolicyHonestSystemAndAuthBoundaries(t *testing.T) {
	policy, err := LoadSupportPolicy(filepath.Join("..", "..", "docs", "fixtures", "apex-local-support-policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	var relevantRules []SupportPolicyRule
	for _, rule := range policy.Rules {
		if rule.Namespace == "System" || rule.Namespace == "Auth" || rule.Namespace == "Database" {
			relevantRules = append(relevantRules, rule)
		}
	}
	policy.Rules = relevantRules
	rows := []SurfaceLedgerRow{
		apexRow("apex:System.String", "System", "String"),
		apexRow("apex:System.List", "System", "List"),
		apexRow("apex:System.Set", "System", "Set"),
		apexRow("apex:System.Map", "System", "Map"),
		apexRow("apex:System.JSON", "System", "JSON"),
		apexRow("apex:System.JSONGenerator", "System", "JSONGenerator"),
		apexRow("apex:System.JSONParser", "System", "JSONParser"),
		apexRow("apex:System.Test", "System", "Test"),
		apexRow("apex:System.Limits", "System", "Limits"),
		apexRow("apex:System.BusinessHours", "System", "BusinessHours"),
		apexMemberRow("apex:System.Date.today()", "System", "Date", "today"),
		apexMemberRow("apex:System.Datetime.now()", "System", "Datetime", "now"),
		apexRow("apex:System.Exception", "System", "Exception"),
		apexRow("apex:System.SerializationException", "System", "SerializationException"),
		apexRow("apex:System.Label", "System", "Label"),
		apexRow("apex:System.LoggingLevel", "System", "LoggingLevel"),
		apexRow("apex:System.UserInfo", "System", "UserInfo"),
		apexRow("apex:System.Cookie", "System", "Cookie"),
		apexRow("apex:System.Location", "System", "Location"),
		apexRow("apex:System.StubProvider", "System", "StubProvider"),
		apexRow("apex:System.Callable", "System", "Callable"),
		apexRow("apex:System.Savepoint", "System", "Savepoint"),
		apexRow("apex:System.Address", "System", "Address"),
		apexRow("apex:System.Quiddity", "System", "Quiddity"),
		apexRow("apex:System.DmlException", "System", "DmlException"),
		apexRow("apex:System.Queueable", "System", "Queueable"),
		apexRow("apex:System.QueueableContext", "System", "QueueableContext"),
		apexRow("apex:System.Schedulable", "System", "Schedulable"),
		apexRow("apex:System.SchedulableContext", "System", "SchedulableContext"),
		apexRow("apex:System.Finalizer", "System", "Finalizer"),
		apexRow("apex:System.FinalizerContext", "System", "FinalizerContext"),
		apexRow("apex:System.InstallHandler", "System", "InstallHandler"),
		apexRow("apex:System.InstallContext", "System", "InstallContext"),
		apexRow("apex:System.UninstallHandler", "System", "UninstallHandler"),
		apexRow("apex:System.UninstallContext", "System", "UninstallContext"),
		apexRow("apex:System.SandboxPostCopy", "System", "SandboxPostCopy"),
		apexRow("apex:System.SandboxContext", "System", "SandboxContext"),
		apexRow("apex:System.HttpCalloutMock", "System", "HttpCalloutMock"),
		apexRow("apex:System.WebServiceMock", "System", "WebServiceMock"),
		apexRow("apex:System.SoqlStubProvider", "System", "SoqlStubProvider"),
		apexRow("apex:System.Trigger", "System", "Trigger"),
		apexRow("apex:System.TriggerContext", "System", "TriggerContext"),
		apexRow("apex:System.TriggerOperation", "System", "TriggerOperation"),
		apexRow("apex:System.RestContext", "System", "RestContext"),
		apexRow("apex:System.RestRequest", "System", "RestRequest"),
		apexRow("apex:System.RestResponse", "System", "RestResponse"),
		apexRow("apex:System.HttpRequest", "System", "HttpRequest"),
		apexRow("apex:System.HttpResponse", "System", "HttpResponse"),
		apexRow("apex:System.PageReference", "System", "PageReference"),
		apexRow("apex:System.SelectOption", "System", "SelectOption"),
		apexRow("apex:System.SObjectAccessDecision", "System", "SObjectAccessDecision"),
		apexRow("apex:System.DMLOptions", "System", "DMLOptions"),
		apexRow("apex:System.TimeZone", "System", "TimeZone"),
		apexRow("apex:System.Security", "System", "Security"),
		apexRow("apex:System.DMLException", "System", "DMLException"),
		apexRow("apex:System.HostedService", "System", "HostedService"),
		apexMemberRow("apex:System.Http.send", "System", "Http", "send"),
		apexMemberRow("apex:System.HttpRequest.setClientCertificate", "System", "HttpRequest", "setClientCertificate"),
		apexMemberRow("apex:System.HttpRequest.setClientCertificateName", "System", "HttpRequest", "setClientCertificateName"),
		apexMemberRow("apex:System.PageReference.getContent", "System", "PageReference", "getContent"),
		apexMemberRow("apex:System.PageReference.getContentAsPDF()", "System", "PageReference", "getContentAsPDF"),
		apexMemberRow("apex:System.PageReference.getParameters()", "System", "PageReference", "getParameters"),
		apexMemberRow("apex:Auth.AuthToken.getAccessToken(String,String)", "Auth", "AuthToken", "getAccessToken"),
		apexMemberRow("apex:Auth.AuthToken.getAccessTokenMap(String,String)", "Auth", "AuthToken", "getAccessTokenMap"),
		apexMemberRow("apex:Auth.AuthToken.refreshAccessToken(String,String,String)", "Auth", "AuthToken", "refreshAccessToken"),
		apexMemberRow("apex:Auth.AuthToken.revokeAccess(String,String,String,String)", "Auth", "AuthToken", "revokeAccess"),
		apexRow("apex:Auth.JWT", "Auth", "JWT"),
		apexMemberRow("apex:Auth.JWTUtil.parseJWTFromStringWithoutValidation(String)", "Auth", "JWTUtil", "parseJWTFromStringWithoutValidation"),
		apexMemberRow("apex:Auth.CommunitiesUtil.isGuestUser()", "Auth", "CommunitiesUtil", "isGuestUser"),
		apexMemberRow("apex:Auth.SessionManagement.getCurrentSession()", "Auth", "SessionManagement", "getCurrentSession"),
		apexMemberRow("apex:Auth.SessionManagement.finishLoginFlow()", "Auth", "SessionManagement", "finishLoginFlow"),
		apexMemberRow("apex:Auth.AuthProviderPlugin.initiate(Map<String,String>,String)", "Auth", "AuthProviderPlugin", "initiate"),
		apexRow("apex:Auth.JWTBearerTokenExchange", "Auth", "JWTBearerTokenExchange"),
	}
	rows = appendPolicyExceptionRows(rows, relevantRules)

	profile := ComputeSupportProfile(rows, policy, nil)
	if len(profile.ValidationErrors) != 0 {
		t.Fatalf("expected no validation errors, got: %v", profile.ValidationErrors)
	}

	want := map[string]SupportDisposition{
		"apex:System.String":                                               DispositionLocalRuntimeRequired,
		"apex:System.List":                                                 DispositionLocalRuntimeRequired,
		"apex:System.Set":                                                  DispositionLocalRuntimeRequired,
		"apex:System.Map":                                                  DispositionLocalRuntimeRequired,
		"apex:System.JSON":                                                 DispositionLocalRuntimeRequired,
		"apex:System.JSONGenerator":                                        DispositionLocalRuntimeRequired,
		"apex:System.JSONParser":                                           DispositionLocalRuntimeRequired,
		"apex:System.Test":                                                 DispositionLocalRuntimeRequired,
		"apex:System.Limits":                                               DispositionLocalRuntimeRequired,
		"apex:System.BusinessHours":                                        DispositionLocalRuntimeRequired,
		"apex:System.Date.today()":                                         DispositionLocalRuntimeRequired,
		"apex:System.Datetime.now()":                                       DispositionLocalRuntimeRequired,
		"apex:System.Exception":                                            DispositionLocalRuntimeRequired,
		"apex:System.SerializationException":                               DispositionLocalRuntimeRequired,
		"apex:System.Label":                                                DispositionLocalRuntimeRequired,
		"apex:System.LoggingLevel":                                         DispositionLocalRuntimeRequired,
		"apex:System.UserInfo":                                             DispositionLocalRuntimeRequired,
		"apex:System.Cookie":                                               DispositionLocalRuntimeRequired,
		"apex:System.Location":                                             DispositionLocalRuntimeRequired,
		"apex:System.StubProvider":                                         DispositionLocalRuntimeRequired,
		"apex:System.Callable":                                             DispositionLocalRuntimeRequired,
		"apex:System.Savepoint":                                            DispositionLocalRuntimeRequired,
		"apex:System.Address":                                              DispositionLocalRuntimeRequired,
		"apex:System.Quiddity":                                             DispositionLocalRuntimeRequired,
		"apex:System.DmlException":                                         DispositionLocalRuntimeRequired,
		"apex:System.Queueable":                                            DispositionLocalRuntimeRequired,
		"apex:System.QueueableContext":                                     DispositionLocalRuntimeRequired,
		"apex:System.Schedulable":                                          DispositionLocalRuntimeRequired,
		"apex:System.SchedulableContext":                                   DispositionLocalRuntimeRequired,
		"apex:System.Finalizer":                                            DispositionLocalRuntimeRequired,
		"apex:System.FinalizerContext":                                     DispositionLocalRuntimeRequired,
		"apex:System.InstallHandler":                                       DispositionLocalRuntimeRequired,
		"apex:System.InstallContext":                                       DispositionLocalRuntimeRequired,
		"apex:System.UninstallHandler":                                     DispositionLocalRuntimeRequired,
		"apex:System.UninstallContext":                                     DispositionLocalRuntimeRequired,
		"apex:System.SandboxPostCopy":                                      DispositionLocalRuntimeRequired,
		"apex:System.SandboxContext":                                       DispositionLocalRuntimeRequired,
		"apex:System.HttpCalloutMock":                                      DispositionLocalRuntimeRequired,
		"apex:System.WebServiceMock":                                       DispositionLocalRuntimeRequired,
		"apex:System.SoqlStubProvider":                                     DispositionLocalRuntimeRequired,
		"apex:System.Trigger":                                              DispositionLocalRuntimeRequired,
		"apex:System.TriggerContext":                                       DispositionLocalRuntimeRequired,
		"apex:System.TriggerOperation":                                     DispositionLocalRuntimeRequired,
		"apex:System.RestContext":                                          DispositionLocalRuntimeRequired,
		"apex:System.RestRequest":                                          DispositionLocalRuntimeRequired,
		"apex:System.RestResponse":                                         DispositionLocalRuntimeRequired,
		"apex:System.HttpRequest":                                          DispositionLocalRuntimeRequired,
		"apex:System.HttpResponse":                                         DispositionLocalRuntimeRequired,
		"apex:System.PageReference":                                        DispositionLocalRuntimeRequired,
		"apex:System.PageReference.getParameters()":                        DispositionLocalRuntimeRequired,
		"apex:System.SelectOption":                                         DispositionLocalRuntimeRequired,
		"apex:System.SObjectAccessDecision":                                DispositionLocalRuntimeRequired,
		"apex:System.DMLOptions":                                           DispositionLocalRuntimeRequired,
		"apex:System.TimeZone":                                             DispositionLocalRuntimeRequired,
		"apex:System.Security":                                             DispositionLocalRuntimeRequired,
		"apex:System.FeatureManagement":                                    DispositionDeterministicMockRequired,
		"apex:System.DMLException":                                         DispositionLocalRuntimeRequired,
		"apex:System.HostedService":                                        DispositionCompileShapeRequired,
		"apex:System.Http.send":                                            DispositionDeterministicMockRequired,
		"apex:System.HttpRequest.setClientCertificate":                     DispositionHostedDeferred,
		"apex:System.PageReference.getContent":                             DispositionHostedDeferred,
		"apex:System.PageReference.getContentAsPDF()":                      DispositionHostedDeferred,
		"apex:Auth.JWT":                                                    DispositionDeterministicMockRequired,
		"apex:Auth.JWTUtil.parseJWTFromStringWithoutValidation(String)":    DispositionDeterministicMockRequired,
		"apex:Auth.CommunitiesUtil.isGuestUser()":                          DispositionDeterministicMockRequired,
		"apex:Auth.SessionManagement.getCurrentSession()":                  DispositionDeterministicMockRequired,
		"apex:Auth.AuthToken.revokeAccess(String,String,String,String)":    DispositionDeterministicMockRequired,
		"apex:Auth.SessionManagement.finishLoginFlow()":                    DispositionCompileShapeRequired,
		"apex:Auth.AuthProviderPlugin.initiate(Map<String,String>,String)": DispositionCompileShapeRequired,
		"apex:Auth.JWTBearerTokenExchange":                                 DispositionCompileShapeRequired,
	}
	byID := make(map[string]SupportProfileRow, len(profile.Rows))
	for _, row := range profile.Rows {
		byID[row.SurfaceID] = row
	}
	for id, wantDisposition := range want {
		row, ok := byID[id]
		if !ok {
			t.Fatalf("missing policy regression row %s", id)
		}
		if row.Disposition != wantDisposition {
			t.Errorf("%s disposition = %s, want %s", id, row.Disposition, wantDisposition)
		}
		if id == "apex:System.FeatureManagement" &&
			(!strings.Contains(row.Reason, "permission") || !strings.Contains(row.Reason, "package feature")) {
			t.Errorf("%s reason = %q, want local permission and package feature state rationale", id, row.Reason)
		}
	}
}

func appendPolicyExceptionRows(rows []SurfaceLedgerRow, rules []SupportPolicyRule) []SurfaceLedgerRow {
	for _, rule := range rules {
		if rule.Namespace == "" {
			continue
		}
		for _, exc := range rule.MemberExceptions {
			if exc.TypeName == "" {
				continue
			}
			id := "apex:" + rule.Namespace + "." + exc.TypeName
			if exc.MemberName == "" {
				rows = append(rows, apexRow(id, rule.Namespace, exc.TypeName))
				continue
			}
			if exc.Kind == KindProperty {
				rows = append(rows, apexPropertyRow(id+"."+exc.MemberName, rule.Namespace, exc.TypeName, exc.MemberName))
			} else {
				rows = append(rows, apexMemberRow(id+"."+exc.MemberName+"()", rule.Namespace, exc.TypeName, exc.MemberName))
			}
		}
	}
	return rows
}

func apexRow(id, namespace, typeName string) SurfaceLedgerRow {
	return SurfaceLedgerRow{
		SurfaceID:     id,
		Product:       ProductApex,
		Area:          AreaRuntime,
		Kind:          KindType,
		Namespace:     namespace,
		TypeName:      typeName,
		GladeShape:    ShapeTypeKnown,
		GladeBehavior: BehaviorSupported,
		Evidence:      EvidenceFixture,
	}
}

func apexMemberRow(id, namespace, typeName, memberName string) SurfaceLedgerRow {
	return SurfaceLedgerRow{
		SurfaceID:     id,
		Product:       ProductApex,
		Area:          AreaRuntime,
		Kind:          KindMethod,
		Namespace:     namespace,
		TypeName:      typeName,
		MemberName:    memberName,
		GladeShape:    ShapeSignatureKnown,
		GladeBehavior: BehaviorSupported,
		Evidence:      EvidenceFixture,
	}
}

func apexPropertyRow(id, namespace, typeName, memberName string) SurfaceLedgerRow {
	row := apexMemberRow(id, namespace, typeName, memberName)
	row.Kind = KindProperty
	return row
}

// 1. Test all four dispositions
func TestSupportProfileFourDispositions(t *testing.T) {
	policy := buildSeedPolicy()
	rows := []SurfaceLedgerRow{
		apexRow("apex:System.String", "System", "String"),
		apexRow("apex:Messaging.Email", "Messaging", "Email"),
		apexRow("apex:Reports.ReportManager", "Reports", "ReportManager"),
		apexRow("apex:Slack.Conversation", "Slack", "Conversation"),
	}

	profile := ComputeSupportProfile(rows, policy, nil)

	if profile.Total != 4 {
		t.Fatalf("total: want 4 got %d", profile.Total)
	}
	if profile.ByDisposition[DispositionLocalRuntimeRequired] != 1 {
		t.Fatalf("local-runtime-required: want 1 got %d", profile.ByDisposition[DispositionLocalRuntimeRequired])
	}
	if profile.ByDisposition[DispositionDeterministicMockRequired] != 1 {
		t.Fatalf("deterministic-mock-required: want 1 got %d", profile.ByDisposition[DispositionDeterministicMockRequired])
	}
	if profile.ByDisposition[DispositionCompileShapeRequired] != 1 {
		t.Fatalf("compile-shape-required: want 1 got %d", profile.ByDisposition[DispositionCompileShapeRequired])
	}
	if profile.ByDisposition[DispositionHostedDeferred] != 1 {
		t.Fatalf("hosted-deferred: want 1 got %d", profile.ByDisposition[DispositionHostedDeferred])
	}

	dispByID := map[string]SupportDisposition{}
	for _, r := range profile.Rows {
		dispByID[r.SurfaceID] = r.Disposition
	}
	if dispByID["apex:System.String"] != DispositionLocalRuntimeRequired {
		t.Fatalf("System.String: got %q", dispByID["apex:System.String"])
	}
	if dispByID["apex:Messaging.Email"] != DispositionDeterministicMockRequired {
		t.Fatalf("Messaging.Email: got %q", dispByID["apex:Messaging.Email"])
	}
	if dispByID["apex:Reports.ReportManager"] != DispositionCompileShapeRequired {
		t.Fatalf("Reports.ReportManager: got %q", dispByID["apex:Reports.ReportManager"])
	}
	if dispByID["apex:Slack.Conversation"] != DispositionHostedDeferred {
		t.Fatalf("Slack.Conversation: got %q", dispByID["apex:Slack.Conversation"])
	}
}

// 2. Test an unclassified Apex row
func TestSupportProfileUnclassifiedApexRow(t *testing.T) {
	policy := buildSeedPolicy()
	rows := []SurfaceLedgerRow{
		apexRow("apex:UnknownNS.SomeType", "UnknownNS", "SomeType"),
	}

	profile := ComputeSupportProfile(rows, policy, nil)

	if len(profile.UnclassifiedRows) != 1 {
		t.Fatalf("unclassified rows: want 1 got %d", len(profile.UnclassifiedRows))
	}
	if profile.UnclassifiedRows[0].SurfaceID != "apex:UnknownNS.SomeType" {
		t.Fatalf("unclassified row: got %q", profile.UnclassifiedRows[0].SurfaceID)
	}
}

// 3. Test overlapping policy rules (cross-type, no override) produces a validation error.
func TestSupportProfileOverlappingPolicyRules(t *testing.T) {
	policy := SupportPolicy{
		Rules: []SupportPolicyRule{
			{
				Namespace:   "System",
				Disposition: DispositionLocalRuntimeRequired,
				Reason:      "system runtime",
			},
			{
				TypeFamily:  "system-stdlib",
				Disposition: DispositionCompileShapeRequired,
				Reason:      "second rule should not apply",
			},
		},
	}
	rows := []SurfaceLedgerRow{
		func() SurfaceLedgerRow {
			r := apexRow("apex:System.String", "System", "String")
			r.SalesforceSurfaceFamily = "system-stdlib"
			return r
		}(),
	}

	profile := ComputeSupportProfile(rows, policy, nil)

	if len(profile.Rows) != 1 {
		t.Fatalf("rows: want 1 got %d", len(profile.Rows))
	}

	// Cross-type overlap without override must produce a validation error.
	foundConflict := false
	for _, err := range profile.ValidationErrors {
		if strings.Contains(err, "conflicting classifications for row apex:System.String") {
			foundConflict = true
			break
		}
	}
	if !foundConflict {
		t.Fatalf("expected conflicting classification validation error, got: %v", profile.ValidationErrors)
	}

	// Row is still classified but match rule is ambiguous.
	if profile.Rows[0].MatchRule != "ambiguous" {
		t.Fatalf("match rule: want ambiguous got %q", profile.Rows[0].MatchRule)
	}
}

// 4. Test stale member exception matches no ledger row
func TestSupportProfileStaleMemberException(t *testing.T) {
	policy := SupportPolicy{
		Rules: []SupportPolicyRule{
			{
				Namespace:   "ConnectApi",
				Disposition: DispositionHostedDeferred,
				Reason:      "connect-api deferred",
				MemberExceptions: []SupportPolicyMemberException{
					{TypeName: "NonexistentType", MemberName: "noSuchMethod", Disposition: DispositionDeterministicMockRequired, Reason: "stale"},
				},
			},
		},
	}
	rows := []SurfaceLedgerRow{
		apexRow("apex:ConnectApi.SomeDTO", "ConnectApi", "SomeDTO"),
	}

	// Should still classify the row as hosted-deferred, and report the stale exception.
	profile := ComputeSupportProfile(rows, policy, nil)

	if len(profile.Rows) != 1 {
		t.Fatalf("rows: want 1 got %d", len(profile.Rows))
	}
	if profile.Rows[0].Disposition != DispositionHostedDeferred {
		t.Fatalf("disposition: want hosted-deferred got %s", profile.Rows[0].Disposition)
	}
	if len(profile.ValidationErrors) == 0 {
		t.Fatalf("expected stale member exception validation error")
	}
	foundStale := false
	for _, err := range profile.ValidationErrors {
		if strings.Contains(err, "stale member exception") && strings.Contains(err, "NonexistentType.noSuchMethod") {
			foundStale = true
			break
		}
	}
	if !foundStale {
		t.Fatalf("expected stale member exception in validation errors, got: %v", profile.ValidationErrors)
	}
}

// 5. Test product rows outside Apex remain visible but excluded from Apex classification
func TestSupportProfileNonApexRowsExcluded(t *testing.T) {
	policy := buildSeedPolicy()
	rows := []SurfaceLedgerRow{
		apexRow("apex:System.String", "System", "String"),
		{
			SurfaceID: "rest:/services/data/vXX.X/sobjects",
			Product:   ProductREST,
			Area:      AreaServer,
			Kind:      KindResource,
		},
		{
			SurfaceID: "lwc:lightning-button",
			Product:   ProductLWC,
			Area:      AreaUI,
			Kind:      KindModule,
		},
	}

	profile := ComputeSupportProfile(rows, policy, nil)

	// Only the Apex row should be classified.
	if profile.Total != 1 {
		t.Fatalf("total: want 1 (apex only) got %d", profile.Total)
	}
	if len(profile.Rows) != 1 {
		t.Fatalf("rows: want 1 got %d", len(profile.Rows))
	}
	if profile.Rows[0].SurfaceID != "apex:System.String" {
		t.Fatalf("expected only Apex row, got %q", profile.Rows[0].SurfaceID)
	}
}

// 6. Test deterministic JSON ordering
func TestSupportProfileDeterministicJSONOrdering(t *testing.T) {
	policy := buildSeedPolicy()
	rows := []SurfaceLedgerRow{
		apexRow("apex:System.String", "System", "String"),
		apexRow("apex:Schema.SObjectType", "Schema", "SObjectType"),
		apexRow("apex:Messaging.Email", "Messaging", "Email"),
	}

	// Run twice and verify identical JSON output.
	var buf1, buf2 bytes.Buffer
	profile1 := ComputeSupportProfile(rows, policy, nil)
	if err := WriteSupportProfileJSON(&buf1, profile1); err != nil {
		t.Fatalf("first write: %v", err)
	}
	profile2 := ComputeSupportProfile(rows, policy, nil)
	if err := WriteSupportProfileJSON(&buf2, profile2); err != nil {
		t.Fatalf("second write: %v", err)
	}

	if buf1.String() != buf2.String() {
		t.Fatalf("JSON output not deterministic\nfirst:\n%s\nsecond:\n%s", buf1.String(), buf2.String())
	}

	// Verify rows are sorted by SurfaceID.
	var decoded struct {
		Rows []struct {
			SurfaceID string `json:"surfaceId"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(buf1.Bytes(), &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(decoded.Rows) != 3 {
		t.Fatalf("decoded rows: want 3 got %d", len(decoded.Rows))
	}
	ids := make([]string, len(decoded.Rows))
	for i, r := range decoded.Rows {
		ids[i] = r.SurfaceID
	}
	if !sort.StringsAreSorted(ids) {
		t.Fatalf("rows not sorted by surfaceId: %v", ids)
	}
}

// 7. Test JSON and Markdown profile output
func TestSupportProfileJSONAndMarkdownOutput(t *testing.T) {
	policy := buildSeedPolicy()
	rows := []SurfaceLedgerRow{
		apexRow("apex:System.String", "System", "String"),
		apexRow("apex:Reports.ReportManager", "Reports", "ReportManager"),
	}

	profile := ComputeSupportProfile(rows, policy, nil)

	// Test JSON output.
	var jsonBuf bytes.Buffer
	if err := WriteSupportProfileJSON(&jsonBuf, profile); err != nil {
		t.Fatalf("write JSON: %v", err)
	}
	out := jsonBuf.String()
	if !strings.HasSuffix(out, "\n") {
		t.Fatalf("JSON must end with newline, got %q", out)
	}
	if !strings.Contains(out, "  ") {
		t.Fatalf("JSON must be indented, got %q", out)
	}

	var decoded SupportProfile
	if err := json.Unmarshal(jsonBuf.Bytes(), &decoded); err != nil {
		t.Fatalf("decode JSON: %v\n%s", err, out)
	}
	if decoded.Total != 2 {
		t.Fatalf("decoded total: want 2 got %d", decoded.Total)
	}
	if len(decoded.Rows) != 2 {
		t.Fatalf("decoded rows: want 2 got %d", len(decoded.Rows))
	}

	// Test Markdown output.
	var mdBuf bytes.Buffer
	if err := WriteSupportProfileMarkdown(&mdBuf, profile); err != nil {
		t.Fatalf("write Markdown: %v", err)
	}
	md := mdBuf.String()
	for _, want := range []string{
		"Support Profile",
		"local-runtime-required",
		"compile-shape-required",
	} {
		if !strings.Contains(md, want) {
			t.Fatalf("Markdown missing %q:\n%s", want, md)
		}
	}
}

// 8. Test CLI failure on any unclassified, overlapping, or stale rule
func TestSupportProfileValidationRejectsUnclassified(t *testing.T) {
	policy := buildSeedPolicy()
	rows := []SurfaceLedgerRow{
		apexRow("apex:UnknownNS.SomeType", "UnknownNS", "SomeType"),
	}

	profile := ComputeSupportProfile(rows, policy, nil)

	if len(profile.UnclassifiedRows) == 0 {
		t.Fatalf("expected unclassified rows")
	}
	if len(profile.ValidationErrors) == 0 {
		t.Fatalf("expected validation error for unclassified row")
	}
	foundUnclassified := false
	for _, err := range profile.ValidationErrors {
		if strings.Contains(err, "unclassified Apex row: apex:UnknownNS.SomeType") {
			foundUnclassified = true
			break
		}
	}
	if !foundUnclassified {
		t.Fatalf("expected unclassified validation error, got: %v", profile.ValidationErrors)
	}
}

func TestSupportProfileValidationRejectsOverlappingRules(t *testing.T) {
	policy := SupportPolicy{
		Rules: []SupportPolicyRule{
			{
				Namespace:   "System",
				Disposition: DispositionLocalRuntimeRequired,
				Reason:      "system runtime",
			},
			{
				Namespace:   "System",
				Disposition: DispositionHostedDeferred,
				Reason:      "overlapping rule",
			},
		},
	}
	// Policy-level validation should detect overlapping rules.
	// For now, the test asserts the profile's validation errors.
	rows := []SurfaceLedgerRow{
		apexRow("apex:System.String", "System", "String"),
	}

	profile := ComputeSupportProfile(rows, policy, nil)

	// The overlapping detection should be flagged.
	foundOverlap := false
	for _, err := range profile.ValidationErrors {
		if strings.Contains(err, "overlapping namespace rule: System") {
			foundOverlap = true
			break
		}
	}
	if !foundOverlap {
		t.Fatalf("expected overlapping namespace rule validation error, got: %v", profile.ValidationErrors)
	}
}

// Test member exception handling within ConnectApi namespace.
func TestSupportProfileConnectApiMemberExceptions(t *testing.T) {
	policy := buildSeedPolicy()
	rows := []SurfaceLedgerRow{
		apexMemberRow("apex:ConnectApi.Organization.getSettings", "ConnectApi", "Organization", "getSettings"),
		apexMemberRow("apex:ConnectApi.UserProfiles.setPhoto", "ConnectApi", "UserProfiles", "setPhoto"),
		apexRow("apex:ConnectApi.SomeDTO", "ConnectApi", "SomeDTO"),
	}

	profile := ComputeSupportProfile(rows, policy, nil)

	// Organization.getSettings should be deterministic-mock-required.
	// UserProfiles.setPhoto should be deterministic-mock-required.
	// SomeDTO should be hosted-deferred.
	dispByID := map[string]SupportDisposition{}
	for _, r := range profile.Rows {
		dispByID[r.SurfaceID] = r.Disposition
	}
	if dispByID["apex:ConnectApi.Organization.getSettings"] != DispositionDeterministicMockRequired {
		t.Fatalf("Organization.getSettings: want deterministic-mock-required got %q", dispByID["apex:ConnectApi.Organization.getSettings"])
	}
	if dispByID["apex:ConnectApi.UserProfiles.setPhoto"] != DispositionDeterministicMockRequired {
		t.Fatalf("UserProfiles.setPhoto: want deterministic-mock-required got %q", dispByID["apex:ConnectApi.UserProfiles.setPhoto"])
	}
	if dispByID["apex:ConnectApi.SomeDTO"] != DispositionHostedDeferred {
		t.Fatalf("SomeDTO: want hosted-deferred got %q", dispByID["apex:ConnectApi.SomeDTO"])
	}
}

// Test type-family matching.
func TestSupportProfileTypeFamilyMatch(t *testing.T) {
	policy := SupportPolicy{
		Rules: []SupportPolicyRule{
			{
				TypeFamily:  "commerce*",
				Disposition: DispositionHostedDeferred,
				Reason:      "commerce deferred",
			},
		},
	}
	rows := []SurfaceLedgerRow{
		func() SurfaceLedgerRow {
			r := apexRow("apex:commercepayments.Payment", "commercepayments", "Payment")
			r.SalesforceSurfaceFamily = "commercepayments"
			return r
		}(),
	}

	profile := ComputeSupportProfile(rows, policy, nil)

	if len(profile.Rows) != 1 {
		t.Fatalf("rows: want 1 got %d", len(profile.Rows))
	}
	if profile.Rows[0].Disposition != DispositionHostedDeferred {
		t.Fatalf("disposition: want hosted-deferred got %s", profile.Rows[0].Disposition)
	}
}

// 8. support-profile requires and joins the corpus-usage input
func TestSupportProfileJoinsCorpusUsage(t *testing.T) {
	policy := buildSeedPolicy()
	rows := []SurfaceLedgerRow{
		apexMemberRow("apex:ConnectApi.ChatterUsers.getFollowings", "ConnectApi", "ChatterUsers", "getFollowings"),
		apexRow("apex:ConnectApi.SomeDTO", "ConnectApi", "SomeDTO"),
		apexRow("apex:System.String", "System", "String"),
	}

	cu := CorpusUsage{
		Usage: []CorpusUsageEntry{
			{
				UsageKey:        "ConnectApi.ChatterUsers.getFollowings",
				Namespace:       "ConnectApi",
				TypeName:        "ChatterUsers",
				MemberName:      "getFollowings",
				PubProdRefs:     5,
				PubProdFiles:    3,
				PubProdProjects: 2,
			},
			{
				UsageKey:    "ConnectApi.SomeDTO",
				Namespace:   "ConnectApi",
				TypeName:    "SomeDTO",
				PubProdRefs: 1,
			},
			{
				UsageKey:    "System.String",
				Namespace:   "System",
				TypeName:    "String",
				PubTestRefs: 10,
			},
		},
	}

	profile := ComputeSupportProfile(rows, policy, &cu)

	// Every row must have a UsageKey.
	for _, row := range profile.Rows {
		if row.UsageKey == "" {
			t.Fatalf("row %s has empty UsageKey", row.SurfaceID)
		}
	}

	// Profile must include the corpus usage.
	if len(profile.CorpusUsage) != 3 {
		t.Fatalf("corpus usage entries: want 3 got %d", len(profile.CorpusUsage))
	}

	// Verify specific keys.
	keys := map[string]CorpusUsageEntry{}
	for _, e := range profile.CorpusUsage {
		keys[e.UsageKey] = e
	}
	if e, ok := keys["ConnectApi.ChatterUsers.getFollowings"]; !ok {
		t.Fatalf("missing ConnectApi.ChatterUsers.getFollowings in corpusUsage")
	} else if e.PubProdRefs != 5 {
		t.Fatalf("ChatterUsers.getFollowings PubProdRefs: want 5 got %d", e.PubProdRefs)
	}

	// Verify row keys match.
	byID := map[string]string{}
	for _, row := range profile.Rows {
		byID[row.SurfaceID] = row.UsageKey
	}
	if byID["apex:ConnectApi.ChatterUsers.getFollowings"] != "ConnectApi.ChatterUsers.getFollowings" {
		t.Fatalf("wrong usage key for getFollowings: %q", byID["apex:ConnectApi.ChatterUsers.getFollowings"])
	}
	if byID["apex:ConnectApi.SomeDTO"] != "ConnectApi.SomeDTO" {
		t.Fatalf("wrong usage key for SomeDTO: %q", byID["apex:ConnectApi.SomeDTO"])
	}
	if byID["apex:System.String"] != "System.String" {
		t.Fatalf("wrong usage key for System.String: %q", byID["apex:System.String"])
	}

	// Compute without corpus usage — UsageKey must be empty but profile works.
	profileNoCU := ComputeSupportProfile(rows, policy, nil)
	for _, row := range profileNoCU.Rows {
		if row.UsageKey != "" {
			t.Fatalf("row %s should have empty UsageKey without corpus input", row.SurfaceID)
		}
	}
	if len(profileNoCU.CorpusUsage) != 0 {
		t.Fatalf("corpusUsage should be empty without corpus input")
	}
}

// RED 1: namespace=Database classifies a Database.Cursor row (descendant namespace match).
func TestSupportProfileDescendantNamespaceMatch(t *testing.T) {
	policy := SupportPolicy{
		Rules: []SupportPolicyRule{
			{
				Namespace:   "Database",
				Disposition: DispositionLocalRuntimeRequired,
				Reason:      "DML runtime",
			},
		},
	}
	rows := []SurfaceLedgerRow{
		apexRow("apex:Database.Cursor", "Database.Cursor", "Cursor"),
	}

	profile := ComputeSupportProfile(rows, policy, nil)

	if profile.Total != 1 {
		t.Fatalf("total: want 1 got %d", profile.Total)
	}
	if profile.Rows[0].Disposition != DispositionLocalRuntimeRequired {
		t.Fatalf("disposition: want local-runtime-required got %s", profile.Rows[0].Disposition)
	}
	if profile.Rows[0].MatchRule != "namespace=Database" {
		t.Fatalf("match rule: want namespace=Database got %q", profile.Rows[0].MatchRule)
	}
	if len(profile.UnclassifiedRows) != 0 {
		t.Fatalf("unclassified: want 0 got %d", len(profile.UnclassifiedRows))
	}
}

// RED 2: namespace=Database does NOT classify DatabaseX (no dot separator).
func TestSupportProfileDescendantNamespaceBoundary(t *testing.T) {
	policy := SupportPolicy{
		Rules: []SupportPolicyRule{
			{
				Namespace:   "Database",
				Disposition: DispositionLocalRuntimeRequired,
				Reason:      "DML runtime",
			},
		},
	}
	rows := []SurfaceLedgerRow{
		apexRow("apex:DatabaseX.Foo", "DatabaseX", "Foo"),
	}

	profile := ComputeSupportProfile(rows, policy, nil)

	if len(profile.UnclassifiedRows) != 1 {
		t.Fatalf("unclassified: want 1 got %d (DatabaseX must not match Database)", len(profile.UnclassifiedRows))
	}
	if profile.UnclassifiedRows[0].SurfaceID != "apex:DatabaseX.Foo" {
		t.Fatalf("unclassified row: got %q", profile.UnclassifiedRows[0].SurfaceID)
	}
}

func TestSupportProfileUsesCanonicalDatabaseAndSchemaNamespace(t *testing.T) {
	policy := SupportPolicy{Rules: []SupportPolicyRule{
		{Namespace: "System", Disposition: DispositionCompileShapeRequired, Reason: "system fallback"},
		{Namespace: "Schema", Disposition: DispositionLocalRuntimeRequired, Reason: "schema runtime"},
		{Namespace: "Database", Disposition: DispositionLocalRuntimeRequired, Reason: "database runtime"},
	}}
	rows := []SurfaceLedgerRow{
		apexMemberRow("apex:Schema.DescribeFieldResult.getLabel()", "System", "DescribeFieldResult", "getLabel"),
		apexMemberRow("apex:Database.QueryLocator.iterator()", "System", "QueryLocator", "iterator"),
		apexMemberRow("apex:Answers.Answers()", "System", "Answers", "Answers"),
	}

	profile := ComputeSupportProfile(rows, policy, nil)
	byID := make(map[string]SupportProfileRow, len(profile.Rows))
	for _, row := range profile.Rows {
		byID[row.SurfaceID] = row
	}
	for _, id := range []string{"apex:Schema.DescribeFieldResult.getLabel()", "apex:Database.QueryLocator.iterator()"} {
		row := byID[id]
		if row.Namespace == "System" || row.Disposition != DispositionLocalRuntimeRequired {
			t.Errorf("%s classified as namespace=%q disposition=%q; want canonical local runtime", id, row.Namespace, row.Disposition)
		}
	}
	if row := byID["apex:Answers.Answers()"]; row.Namespace != "System" || row.Disposition != DispositionCompileShapeRequired {
		t.Errorf("System.Answers identity changed: namespace=%q disposition=%q", row.Namespace, row.Disposition)
	}
}

// RED 3: a member exception works on a descendant namespace row.
func TestSupportProfileMemberExceptionOnDescendantNamespace(t *testing.T) {
	policy := SupportPolicy{
		Rules: []SupportPolicyRule{
			{
				Namespace:   "ConnectApi",
				Disposition: DispositionHostedDeferred,
				Reason:      "connect-api deferred",
				MemberExceptions: []SupportPolicyMemberException{
					{
						TypeName:    "Organization",
						MemberName:  "getSettings",
						Disposition: DispositionDeterministicMockRequired,
						Reason:      "observed corpus usage",
					},
				},
			},
		},
	}
	// Descendant namespace: ConnectApi.Internal
	rows := []SurfaceLedgerRow{
		apexMemberRow("apex:ConnectApi.Internal.Organization.getSettings", "ConnectApi.Internal", "Organization", "getSettings"),
	}

	profile := ComputeSupportProfile(rows, policy, nil)

	if profile.Total != 1 {
		t.Fatalf("total: want 1 got %d", profile.Total)
	}
	if profile.Rows[0].Disposition != DispositionDeterministicMockRequired {
		t.Fatalf("disposition: want deterministic-mock-required got %s", profile.Rows[0].Disposition)
	}
	if !strings.Contains(profile.Rows[0].MatchRule, "member exception") {
		t.Fatalf("match rule must indicate member exception, got %q", profile.Rows[0].MatchRule)
	}
}

// RED 4: surfacePrefix=apex-language: classifies a namespace-less language row.
func TestSupportProfileSurfacePrefixMatch(t *testing.T) {
	policy := SupportPolicy{
		Rules: []SupportPolicyRule{
			{
				SurfacePrefix: "apex-language:",
				Disposition:   DispositionLocalRuntimeRequired,
				Reason:        "language constructs",
			},
		},
	}
	rows := []SurfaceLedgerRow{
		{
			SurfaceID:     "apex-language:for:loop",
			Product:       ProductApex,
			Area:          AreaRuntime,
			Kind:          KindType,
			Namespace:     "",
			TypeName:      "for:loop",
			GladeShape:    ShapeTypeKnown,
			GladeBehavior: BehaviorSupported,
			Evidence:      EvidenceFixture,
		},
	}

	profile := ComputeSupportProfile(rows, policy, nil)

	if profile.Total != 1 {
		t.Fatalf("total: want 1 got %d", profile.Total)
	}
	if profile.Rows[0].Disposition != DispositionLocalRuntimeRequired {
		t.Fatalf("disposition: want local-runtime-required got %s", profile.Rows[0].Disposition)
	}
	if profile.Rows[0].MatchRule != "surfacePrefix=apex-language:" {
		t.Fatalf("match rule: want surfacePrefix=apex-language: got %q", profile.Rows[0].MatchRule)
	}
}

// RED 5: nonmatching surface prefix leaves row unclassified.
func TestSupportProfileSurfacePrefixNoMatch(t *testing.T) {
	policy := SupportPolicy{
		Rules: []SupportPolicyRule{
			{
				SurfacePrefix: "apex-language:",
				Disposition:   DispositionLocalRuntimeRequired,
				Reason:        "language constructs",
			},
		},
	}
	rows := []SurfaceLedgerRow{
		{
			SurfaceID:     "apex:System.String",
			Product:       ProductApex,
			Area:          AreaRuntime,
			Kind:          KindType,
			Namespace:     "System",
			TypeName:      "String",
			GladeShape:    ShapeTypeKnown,
			GladeBehavior: BehaviorSupported,
			Evidence:      EvidenceFixture,
		},
	}

	profile := ComputeSupportProfile(rows, policy, nil)

	// The System.String row does not start with apex-language: — it should be unclassified.
	if len(profile.UnclassifiedRows) != 1 {
		t.Fatalf("unclassified: want 1 got %d", len(profile.UnclassifiedRows))
	}
}

// RED 6: duplicate surface-prefix rules produce a deterministic validation error.
func TestSupportProfileDuplicateSurfacePrefix(t *testing.T) {
	policy := SupportPolicy{
		Rules: []SupportPolicyRule{
			{
				SurfacePrefix: "apex-language:",
				Disposition:   DispositionLocalRuntimeRequired,
				Reason:        "language constructs",
			},
			{
				SurfacePrefix: "apex-language:",
				Disposition:   DispositionCompileShapeRequired,
				Reason:        "duplicate",
			},
		},
	}
	rows := []SurfaceLedgerRow{
		{
			SurfaceID:     "apex-language:for:loop",
			Product:       ProductApex,
			Area:          AreaRuntime,
			Kind:          KindType,
			Namespace:     "",
			TypeName:      "for:loop",
			GladeShape:    ShapeTypeKnown,
			GladeBehavior: BehaviorSupported,
			Evidence:      EvidenceFixture,
		},
	}

	profile := ComputeSupportProfile(rows, policy, nil)

	foundOverlap := false
	for _, err := range profile.ValidationErrors {
		if strings.Contains(err, "overlapping surface-prefix rule: apex-language:") {
			foundOverlap = true
			break
		}
	}
	if !foundOverlap {
		t.Fatalf("expected overlapping surface-prefix validation error, got: %v", profile.ValidationErrors)
	}
}

func TestSupportProfileNormalizesSystemFamilyAliases(t *testing.T) {
	policy := SupportPolicy{Rules: []SupportPolicyRule{
		{Namespace: "System", Disposition: DispositionCompileShapeRequired, Reason: "fallback"},
		{Namespace: "Database", Disposition: DispositionLocalRuntimeRequired, Reason: "database"},
		{Namespace: "Messaging", Disposition: DispositionDeterministicMockRequired, Reason: "messaging"},
		{Namespace: "ApexPages", Disposition: DispositionLocalRuntimeRequired, Reason: "pages"},
		{Namespace: "QuickAction", Disposition: DispositionHostedDeferred, Reason: "hosted"},
	}}
	rows := []SurfaceLedgerRow{
		{SurfaceID: "apex:System.Database.getQueryLocator", Product: ProductApex, Namespace: "System", TypeName: "Database", MemberName: "getQueryLocator", GladeShape: ShapeSignatureKnown},
		{SurfaceID: "apex:System.Database.DeletedRecord.getId", Product: ProductApex, Namespace: "System.Database", TypeName: "DeletedRecord", MemberName: "getId", GladeShape: ShapeSignatureKnown},
		{SurfaceID: "apex:System.Messaging.sendEmail", Product: ProductApex, Namespace: "System", TypeName: "Messaging", MemberName: "sendEmail", GladeShape: ShapeSignatureKnown},
		{SurfaceID: "apex:System.ApexPages.currentPage", Product: ProductApex, Namespace: "System", TypeName: "ApexPages", MemberName: "currentPage", GladeShape: ShapeSignatureKnown},
		{SurfaceID: "apex:System.QuickAction.execute", Product: ProductApex, Namespace: "System", TypeName: "QuickAction", MemberName: "execute", GladeShape: ShapeSignatureKnown},
		{SurfaceID: "apex:System.System.assertEquals", Product: ProductApex, Namespace: "System", TypeName: "System", MemberName: "assertEquals", GladeShape: ShapeSignatureKnown},
		{SurfaceID: "apex:System.Answers.findSimilar", Product: ProductApex, Namespace: "System", TypeName: "Answers", MemberName: "findSimilar", GladeShape: ShapeSignatureKnown},
	}
	profile := ComputeSupportProfile(rows, policy, &CorpusUsage{})
	byID := make(map[string]SupportProfileRow, len(profile.Rows))
	for _, row := range profile.Rows {
		byID[row.SurfaceID] = row
	}
	for _, tc := range []struct {
		id, namespace, disposition, usageKey string
	}{
		{"apex:System.Database.getQueryLocator", "Database", string(DispositionLocalRuntimeRequired), "Database.getQueryLocator"},
		{"apex:System.Database.DeletedRecord.getId", "Database", string(DispositionLocalRuntimeRequired), "Database.DeletedRecord.getId"},
		{"apex:System.Messaging.sendEmail", "Messaging", string(DispositionDeterministicMockRequired), "Messaging.sendEmail"},
		{"apex:System.ApexPages.currentPage", "ApexPages", string(DispositionLocalRuntimeRequired), "ApexPages.currentPage"},
		{"apex:System.QuickAction.execute", "QuickAction", string(DispositionHostedDeferred), "QuickAction.execute"},
		{"apex:System.System.assertEquals", "System", string(DispositionCompileShapeRequired), "System.assertEquals"},
		{"apex:System.Answers.findSimilar", "System", string(DispositionCompileShapeRequired), "System.Answers.findSimilar"},
	} {
		row, ok := byID[tc.id]
		if !ok {
			t.Fatalf("missing row %s", tc.id)
		}
		if row.Namespace != tc.namespace || string(row.Disposition) != tc.disposition || row.UsageKey != tc.usageKey {
			t.Errorf("%s = namespace=%q disposition=%q usage=%q, want namespace=%q disposition=%q usage=%q", tc.id, row.Namespace, row.Disposition, row.UsageKey, tc.namespace, tc.disposition, tc.usageKey)
		}
	}
}

func TestSupportPolicyNamespaceCanonicalizesSystemDMLOptions(t *testing.T) {
	policy := SupportPolicy{Rules: []SupportPolicyRule{{Namespace: "System"}, {Namespace: "Database"}}}
	row := SurfaceLedgerRow{Namespace: "System", TypeName: "DMLOptions"}
	if got := supportPolicyNamespace(row, policy); got != "Database" {
		t.Fatalf("namespace = %q, want Database", got)
	}
}

func TestSupportProfileSystemFamilyAliasJoinsCorpusUsage(t *testing.T) {
	policy := SupportPolicy{Rules: []SupportPolicyRule{
		{Namespace: "System", Disposition: DispositionCompileShapeRequired, Reason: "fallback"},
		{Namespace: "Database", Disposition: DispositionLocalRuntimeRequired, Reason: "database"},
	}}
	rows := []SurfaceLedgerRow{{
		SurfaceID: "apex:System.Database.getQueryLocator", Product: ProductApex,
		Namespace: "System", TypeName: "Database", MemberName: "getQueryLocator",
		GladeShape: ShapeSignatureKnown,
	}}
	usage := &CorpusUsage{Usage: []CorpusUsageEntry{{UsageKey: "Database.getQueryLocator", PubProdRefs: 34, PrivProdRefs: 91}}}
	profile := ComputeSupportProfile(rows, policy, usage)
	if len(profile.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(profile.Rows))
	}
	row := profile.Rows[0]
	if row.UsageKey != "Database.getQueryLocator" || row.CorpusPassingRefs != 125 {
		t.Fatalf("corpus join = key=%q refs=%d, want key=Database.getQueryLocator refs=125", row.UsageKey, row.CorpusPassingRefs)
	}
}

// SF-CB14 RED 1: a surfacePrefix rule with a matched member exception is not reported stale.
func TestSupportProfileSurfacePrefixMemberExceptionNotStale(t *testing.T) {
	policy := SupportPolicy{
		Rules: []SupportPolicyRule{
			{
				SurfacePrefix: "apex:Approval.process",
				Disposition:   DispositionHostedDeferred,
				Reason:        "approval process hosted",
				MemberExceptions: []SupportPolicyMemberException{
					{
						TypeName:    "SubmitAction",
						MemberName:  "submit",
						Disposition: DispositionDeterministicMockRequired,
						Reason:      "observed corpus usage",
					},
				},
			},
		},
	}
	rows := []SurfaceLedgerRow{
		apexMemberRow("apex:Approval.process.SubmitAction.submit", "Approval.process", "SubmitAction", "submit"),
	}

	profile := ComputeSupportProfile(rows, policy, nil)

	if profile.Total != 1 {
		t.Fatalf("total: want 1 got %d", profile.Total)
	}
	if profile.Rows[0].Disposition != DispositionDeterministicMockRequired {
		t.Fatalf("disposition: want deterministic-mock-required got %s", profile.Rows[0].Disposition)
	}
	if !strings.Contains(profile.Rows[0].MatchRule, "member exception") {
		t.Fatalf("match rule must indicate member exception, got %q", profile.Rows[0].MatchRule)
	}

	// Must NOT report stale member exception.
	for _, err := range profile.ValidationErrors {
		if strings.Contains(err, "stale member exception") {
			t.Fatalf("unexpected stale member exception: %s", err)
		}
	}
}

func TestCurrentPolicyDefersIntentionalHostedBoundaryRows(t *testing.T) {
	policy, err := LoadSupportPolicy(filepath.Join("..", "..", "docs", "fixtures", "apex-local-support-policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	rows := []SurfaceLedgerRow{
		{SurfaceID: "apex:System.Canvas.*", Product: ProductApex, Namespace: "System", TypeName: "Canvas", MemberName: "*", Kind: KindMethod, GladeShape: ShapeSignatureKnown, GladeBehavior: BehaviorUnsupported, Evidence: EvidenceFixture},
		{SurfaceID: "apex:System.Database.deleteAsync(List<Object>,Database.AllowCallouts,AccessLevel)", Product: ProductApex, Namespace: "System", TypeName: "Database", MemberName: "deleteAsync", Kind: KindMethod, GladeShape: ShapeSignatureKnown, GladeBehavior: BehaviorUnsupported, Evidence: EvidenceFixture},
		{SurfaceID: "apex:System.Database.deleteAsync(Object,Database.AllowCallouts,AccessLevel)", Product: ProductApex, Namespace: "System", TypeName: "Database", MemberName: "deleteAsync", Kind: KindMethod, GladeShape: ShapeSignatureKnown, GladeBehavior: BehaviorUnsupported, Evidence: EvidenceFixture},
		{SurfaceID: "apex:System.Database.insertAsync(List<Object>,Database.AllowCallouts,AccessLevel)", Product: ProductApex, Namespace: "System", TypeName: "Database", MemberName: "insertAsync", Kind: KindMethod, GladeShape: ShapeSignatureKnown, GladeBehavior: BehaviorUnsupported, Evidence: EvidenceFixture},
		{SurfaceID: "apex:System.Database.insertAsync(Object,Database.AllowCallouts,AccessLevel)", Product: ProductApex, Namespace: "System", TypeName: "Database", MemberName: "insertAsync", Kind: KindMethod, GladeShape: ShapeSignatureKnown, GladeBehavior: BehaviorUnsupported, Evidence: EvidenceFixture},
		{SurfaceID: "apex:System.Database.updateAsync(List<Object>,Database.AllowCallouts,AccessLevel)", Product: ProductApex, Namespace: "System", TypeName: "Database", MemberName: "updateAsync", Kind: KindMethod, GladeShape: ShapeSignatureKnown, GladeBehavior: BehaviorUnsupported, Evidence: EvidenceFixture},
		{SurfaceID: "apex:System.Database.updateAsync(Object,Database.AllowCallouts,AccessLevel)", Product: ProductApex, Namespace: "System", TypeName: "Database", MemberName: "updateAsync", Kind: KindMethod, GladeShape: ShapeSignatureKnown, GladeBehavior: BehaviorUnsupported, Evidence: EvidenceFixture},
		{SurfaceID: "apex:System.EventBus.*", Product: ProductApex, Namespace: "System", TypeName: "EventBus", MemberName: "*", Kind: KindMethod, GladeShape: ShapeSignatureKnown, GladeBehavior: BehaviorUnsupported, Evidence: EvidenceFixture},
		{SurfaceID: "apex:System.EventBus.getOperationId(Object)", Product: ProductApex, Namespace: "System", TypeName: "EventBus", MemberName: "getOperationId", Kind: KindMethod, GladeShape: ShapeSignatureKnown, GladeBehavior: BehaviorUnsupported, Evidence: EvidenceFixture},
		{SurfaceID: "apex:System.EventBus.publishAfterCommit", Product: ProductApex, Namespace: "System", TypeName: "EventBus", MemberName: "publishAfterCommit", Kind: KindMethod, GladeShape: ShapeSignatureKnown, GladeBehavior: BehaviorUnsupported, Evidence: EvidenceFixture},
		{SurfaceID: "apex:System.Messaging.sendPushNotification", Product: ProductApex, Namespace: "System", TypeName: "Messaging", MemberName: "sendPushNotification", Kind: KindMethod, GladeShape: ShapeSignatureKnown, GladeBehavior: BehaviorUnsupported, Evidence: EvidenceFixture},
	}
	profile := ComputeSupportProfile(rows, policy, nil)
	for _, row := range profile.Rows {
		if row.Disposition != DispositionHostedDeferred || row.GapClass != "" {
			t.Errorf("%s disposition/gap = %s/%s, want hosted-deferred/none", row.SurfaceID, row.Disposition, row.GapClass)
		}
	}
}

// SF-CB14 RED 2: exception keys for two distinct surface prefixes remain distinct.
func TestSupportProfileSurfacePrefixDistinctExceptionKeys(t *testing.T) {
	policy := SupportPolicy{
		Rules: []SupportPolicyRule{
			{
				SurfacePrefix: "apex:ConnectApi.",
				Disposition:   DispositionHostedDeferred,
				Reason:        "connect-api hosted",
				MemberExceptions: []SupportPolicyMemberException{
					{
						TypeName:    "Organization",
						MemberName:  "getSettings",
						Disposition: DispositionDeterministicMockRequired,
						Reason:      "org settings mock",
					},
				},
			},
			{
				SurfacePrefix: "apex:Connect.",
				Disposition:   DispositionHostedDeferred,
				Reason:        "connect hosted",
				MemberExceptions: []SupportPolicyMemberException{
					{
						TypeName:    "Organization",
						MemberName:  "getSettings",
						Disposition: DispositionDeterministicMockRequired,
						Reason:      "connect org settings mock",
					},
				},
			},
		},
	}
	rows := []SurfaceLedgerRow{
		apexMemberRow("apex:ConnectApi.Organization.getSettings", "ConnectApi", "Organization", "getSettings"),
		apexMemberRow("apex:Connect.Organization.getSettings", "Connect", "Organization", "getSettings"),
	}

	profile := ComputeSupportProfile(rows, policy, nil)

	if profile.Total != 2 {
		t.Fatalf("total: want 2 got %d", profile.Total)
	}

	// Both rows should be classified by their respective member exceptions.
	for _, r := range profile.Rows {
		if r.Disposition != DispositionDeterministicMockRequired {
			t.Fatalf("row %s: want deterministic-mock-required got %s", r.SurfaceID, r.Disposition)
		}
		if !strings.Contains(r.MatchRule, "member exception") {
			t.Fatalf("row %s: match rule must indicate member exception, got %q", r.SurfaceID, r.MatchRule)
		}
	}

	// Must NOT report stale member exceptions — each surfacePrefix scopes its own key.
	for _, err := range profile.ValidationErrors {
		if strings.Contains(err, "stale member exception") {
			t.Fatalf("unexpected stale member exception: %s", err)
		}
	}
}

// --- SF-CB15 obligation queue tests ---

// RED 1: compile-shape rows require local fixture evidence when their shape is present.
func TestGapClassCompileShapeClosedWithFixtureEvidence(t *testing.T) {
	policy := SupportPolicy{
		Rules: []SupportPolicyRule{
			{Namespace: "Reports", Disposition: DispositionCompileShapeRequired, Reason: "compile shape"},
		},
	}
	rows := []SurfaceLedgerRow{
		apexRow("apex:Reports.ReportManager", "Reports", "ReportManager"), // type-known shape
		func() SurfaceLedgerRow {
			r := apexRow("apex:Reports.Broken", "Reports", "Broken")
			r.GladeShape = ShapeAbsent
			return r
		}(),
	}

	profile := ComputeSupportProfile(rows, policy, nil)

	if profile.Total != 2 {
		t.Fatalf("total: want 2 got %d", profile.Total)
	}

	// Known shape → closed (no gap).
	byID := map[string]SupportProfileRow{}
	for _, r := range profile.Rows {
		byID[r.SurfaceID] = r
	}
	if byID["apex:Reports.ReportManager"].GapClass != "" {
		t.Fatalf("Reports.ReportManager with type-known shape should be closed, got gapClass=%q", byID["apex:Reports.ReportManager"].GapClass)
	}
	// Absent shape → missing-shape gap.
	if byID["apex:Reports.Broken"].GapClass != "missing-shape" {
		t.Fatalf("Reports.Broken with absent shape should be missing-shape gap, got gapClass=%q", byID["apex:Reports.Broken"].GapClass)
	}

	// NonDeferredGaps should only contain the broken row.
	if len(profile.NonDeferredGaps) != 1 {
		t.Fatalf("NonDeferredGaps: want 1 got %d", len(profile.NonDeferredGaps))
	}
	if profile.NonDeferredGaps[0].SurfaceID != "apex:Reports.Broken" {
		t.Fatalf("NonDeferredGaps[0]: want apex:Reports.Broken got %q", profile.NonDeferredGaps[0].SurfaceID)
	}

	// byGapClass counts.
	if profile.ByGapClass["missing-shape"] != 1 {
		t.Fatalf("byGapClass[missing-shape]: want 1 got %d", profile.ByGapClass["missing-shape"])
	}
}

func TestGapClassCompileShapeRequiresLocalFixtureEvidence(t *testing.T) {
	policy := SupportPolicy{
		Rules: []SupportPolicyRule{
			{Namespace: "Reports", Disposition: DispositionCompileShapeRequired, Reason: "compile shape"},
		},
	}
	cases := []struct {
		name     string
		evidence EvidenceState
		wantGap  string
	}{
		{name: "fixture", evidence: EvidenceFixture, wantGap: ""},
		{name: "fixture-and-oracle", evidence: EvidenceFixtureAndOracle, wantGap: ""},
		{name: "oracle-only", evidence: EvidenceOracle, wantGap: GapMissingEvidence},
		{name: "docs-only", evidence: EvidenceDocs, wantGap: GapMissingEvidence},
		{name: "corpus-only", evidence: EvidenceCorpus, wantGap: GapMissingEvidence},
		{name: "no-evidence", evidence: EvidenceNone, wantGap: GapMissingEvidence},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := apexRow("apex:Reports."+tc.name, "Reports", tc.name)
			r.Evidence = tc.evidence

			profile := ComputeSupportProfile([]SurfaceLedgerRow{r}, policy, nil)

			if got := profile.Rows[0].GapClass; got != tc.wantGap {
				t.Fatalf("gapClass: want %q got %q for evidence %q", tc.wantGap, got, tc.evidence)
			}
		})
	}
}

// RED 2: passive rows require both local fixture and Salesforce oracle evidence.
func TestGapClassPassiveBehaviorRequiresDualEvidence(t *testing.T) {
	policy := SupportPolicy{
		Rules: []SupportPolicyRule{
			{Namespace: "System", Disposition: DispositionLocalRuntimeRequired, Reason: "runtime"},
			{Namespace: "Cache", Disposition: DispositionDeterministicMockRequired, Reason: "mock"},
		},
	}
	cases := []struct {
		name        string
		namespace   string
		disposition SupportDisposition
		evidence    EvidenceState
		wantGap     string
	}{
		{name: "runtime-no-evidence", namespace: "System", disposition: DispositionLocalRuntimeRequired, evidence: EvidenceNone, wantGap: GapMissingEvidence},
		{name: "runtime-fixture-only", namespace: "System", disposition: DispositionLocalRuntimeRequired, evidence: EvidenceFixture, wantGap: GapMissingEvidence},
		{name: "runtime-oracle-only", namespace: "System", disposition: DispositionLocalRuntimeRequired, evidence: EvidenceOracle, wantGap: GapMissingEvidence},
		{name: "runtime-docs-only", namespace: "System", disposition: DispositionLocalRuntimeRequired, evidence: EvidenceDocs, wantGap: GapMissingEvidence},
		{name: "runtime-corpus-only", namespace: "System", disposition: DispositionLocalRuntimeRequired, evidence: EvidenceCorpus, wantGap: GapMissingEvidence},
		{name: "runtime-dual", namespace: "System", disposition: DispositionLocalRuntimeRequired, evidence: EvidenceFixtureAndOracle, wantGap: ""},
		{name: "mock-no-evidence", namespace: "Cache", disposition: DispositionDeterministicMockRequired, evidence: EvidenceNone, wantGap: GapMissingEvidence},
		{name: "mock-fixture-only", namespace: "Cache", disposition: DispositionDeterministicMockRequired, evidence: EvidenceFixture, wantGap: GapMissingEvidence},
		{name: "mock-oracle-only", namespace: "Cache", disposition: DispositionDeterministicMockRequired, evidence: EvidenceOracle, wantGap: GapMissingEvidence},
		{name: "mock-docs-only", namespace: "Cache", disposition: DispositionDeterministicMockRequired, evidence: EvidenceDocs, wantGap: GapMissingEvidence},
		{name: "mock-corpus-only", namespace: "Cache", disposition: DispositionDeterministicMockRequired, evidence: EvidenceCorpus, wantGap: GapMissingEvidence},
		{name: "mock-dual", namespace: "Cache", disposition: DispositionDeterministicMockRequired, evidence: EvidenceFixtureAndOracle, wantGap: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := apexRow("apex:"+tc.namespace+".PassiveHelper", tc.namespace, "PassiveHelper")
			r.GladeShape = ShapeTypeKnown
			r.GladeBehavior = BehaviorPassive
			r.Evidence = tc.evidence

			profile := ComputeSupportProfile([]SurfaceLedgerRow{r}, policy, nil)

			if got := profile.Rows[0].GapClass; got != tc.wantGap {
				t.Fatalf("gapClass: want %q got %q for %s/%s", tc.wantGap, got, tc.disposition, tc.evidence)
			}
		})
	}
}

// RED 3: none, stub-noop, unsupported, and partial runtime/mock rows are behavior gaps.
func TestGapClassBehaviorGaps(t *testing.T) {
	policy := SupportPolicy{
		Rules: []SupportPolicyRule{
			{Namespace: "System", Disposition: DispositionLocalRuntimeRequired, Reason: "runtime"},
		},
	}
	behaviors := []BehaviorState{BehaviorNone, BehaviorStubNoOp, BehaviorUnsupported, BehaviorPartial}
	evidences := []EvidenceState{EvidenceNone, EvidenceFixture, EvidenceOracle, EvidenceDocs, EvidenceCorpus, EvidenceFixtureAndOracle}
	for _, bh := range behaviors {
		for _, ev := range evidences {
			bh, ev := bh, ev
			t.Run(string(bh)+"/"+string(ev), func(t *testing.T) {
				r := apexRow("apex:System.Test", "System", "Test")
				r.GladeShape = ShapeTypeKnown
				r.GladeBehavior = bh
				r.Evidence = ev

				profile := ComputeSupportProfile([]SurfaceLedgerRow{r}, policy, nil)

				if profile.Total != 1 {
					t.Fatalf("total: want 1 got %d", profile.Total)
				}
				if len(profile.NonDeferredGaps) != 1 {
					t.Fatalf("NonDeferredGaps: want 1 got %d for behavior/evidence %q/%q", len(profile.NonDeferredGaps), bh, ev)
				}
				if profile.Rows[0].GapClass != GapMissingBehavior {
					t.Fatalf("gapClass: want %s got %q for behavior/evidence %q/%q", GapMissingBehavior, profile.Rows[0].GapClass, bh, ev)
				}
				if profile.ByGapClass[GapMissingBehavior] != 1 {
					t.Fatalf("byGapClass[%s]: want 1 got %d", GapMissingBehavior, profile.ByGapClass[GapMissingBehavior])
				}
			})
		}
	}
}

// RED 4: supported runtime/mock behavior requires both local fixture and Salesforce oracle evidence.
func TestGapClassSupportedRequiresDualEvidence(t *testing.T) {
	policy := SupportPolicy{
		Rules: []SupportPolicyRule{
			{Namespace: "System", Disposition: DispositionLocalRuntimeRequired, Reason: "runtime"},
			{Namespace: "Cache", Disposition: DispositionDeterministicMockRequired, Reason: "mock"},
		},
	}
	cases := []struct {
		name      string
		namespace string
		evidence  EvidenceState
		wantGap   string
	}{
		{name: "runtime-fixture-only", namespace: "System", evidence: EvidenceFixture, wantGap: GapMissingEvidence},
		{name: "runtime-oracle-only", namespace: "System", evidence: EvidenceOracle, wantGap: GapMissingEvidence},
		{name: "runtime-no-evidence", namespace: "System", evidence: EvidenceNone, wantGap: GapMissingEvidence},
		{name: "runtime-docs-only", namespace: "System", evidence: EvidenceDocs, wantGap: GapMissingEvidence},
		{name: "runtime-corpus-only", namespace: "System", evidence: EvidenceCorpus, wantGap: GapMissingEvidence},
		{name: "runtime-dual", namespace: "System", evidence: EvidenceFixtureAndOracle, wantGap: ""},
		{name: "mock-fixture-only", namespace: "Cache", evidence: EvidenceFixture, wantGap: GapMissingEvidence},
		{name: "mock-oracle-only", namespace: "Cache", evidence: EvidenceOracle, wantGap: GapMissingEvidence},
		{name: "mock-no-evidence", namespace: "Cache", evidence: EvidenceNone, wantGap: GapMissingEvidence},
		{name: "mock-docs-only", namespace: "Cache", evidence: EvidenceDocs, wantGap: GapMissingEvidence},
		{name: "mock-corpus-only", namespace: "Cache", evidence: EvidenceCorpus, wantGap: GapMissingEvidence},
		{name: "mock-dual", namespace: "Cache", evidence: EvidenceFixtureAndOracle, wantGap: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := apexRow("apex:"+tc.namespace+".Test", tc.namespace, "Test")
			r.GladeShape = ShapeTypeKnown
			r.GladeBehavior = BehaviorSupported
			r.Evidence = tc.evidence

			profile := ComputeSupportProfile([]SurfaceLedgerRow{r}, policy, nil)

			if profile.Total != 1 {
				t.Fatalf("total: want 1 got %d", profile.Total)
			}
			if got := profile.Rows[0].GapClass; got != tc.wantGap {
				t.Fatalf("gapClass: want %q got %q for %s/%s", tc.wantGap, got, tc.namespace, tc.evidence)
			}
		})
	}
}

// RED 5: supported behavior with none/docs/corpus evidence is an evidence gap.
func TestGapClassSupportedWithNonExecutableEvidenceIsEvidenceGap(t *testing.T) {
	policy := SupportPolicy{
		Rules: []SupportPolicyRule{
			{Namespace: "System", Disposition: DispositionLocalRuntimeRequired, Reason: "runtime"},
		},
	}
	nonExecEvidence := []EvidenceState{EvidenceNone, EvidenceDocs, EvidenceCorpus}
	// fixture-based rows may have empty evidence defaulting to none
	for _, ev := range nonExecEvidence {
		t.Run(string(ev), func(t *testing.T) {
			r := apexRow("apex:System.Test", "System", "Test")
			r.GladeShape = ShapeTypeKnown
			r.GladeBehavior = BehaviorSupported
			r.Evidence = ev

			profile := ComputeSupportProfile([]SurfaceLedgerRow{r}, policy, nil)

			if profile.Total != 1 {
				t.Fatalf("total: want 1 got %d", profile.Total)
			}
			if len(profile.NonDeferredGaps) != 1 {
				t.Fatalf("supported+%s should be an evidence gap, got %d gaps", ev, len(profile.NonDeferredGaps))
			}
			if profile.Rows[0].GapClass != "missing-evidence" {
				t.Fatalf("gapClass: want missing-evidence got %q for evidence %q", profile.Rows[0].GapClass, ev)
			}
			if profile.ByGapClass["missing-evidence"] != 1 {
				t.Fatalf("byGapClass[missing-evidence]: want 1 got %d", profile.ByGapClass["missing-evidence"])
			}
		})
	}
}

// RED 6: hosted-deferred rows never enter the gap queue.
func TestGapClassHostedDeferredNeverInGaps(t *testing.T) {
	policy := buildSeedPolicy()
	// ConnectApi.SomeDTO is hosted-deferred; give it absent shape — still must NOT be in gaps.
	rows := []SurfaceLedgerRow{
		func() SurfaceLedgerRow {
			r := apexRow("apex:ConnectApi.SomeDTO", "ConnectApi", "SomeDTO")
			r.GladeShape = ShapeAbsent
			r.GladeBehavior = BehaviorNone
			r.Evidence = EvidenceNone
			return r
		}(),
	}

	profile := ComputeSupportProfile(rows, policy, nil)

	if profile.Total != 1 {
		t.Fatalf("total: want 1 got %d", profile.Total)
	}
	if len(profile.NonDeferredGaps) != 0 {
		t.Fatalf("hosted-deferred row must not enter NonDeferredGaps, got %d", len(profile.NonDeferredGaps))
	}
	if len(profile.HostedDeferred) != 1 {
		t.Fatalf("hosted-deferred row must be in HostedDeferred, got %d", len(profile.HostedDeferred))
	}
	if profile.Rows[0].GapClass != "" {
		t.Fatalf("hosted-deferred gapClass should be empty, got %q", profile.Rows[0].GapClass)
	}
}

// RED 8: queue ordering follows the four ranking keys.
func TestGapClassCorpusBackedOrdering(t *testing.T) {
	policy := SupportPolicy{
		Rules: []SupportPolicyRule{
			{Namespace: "System", Disposition: DispositionLocalRuntimeRequired, Reason: "runtime"},
		},
	}

	rows := []SurfaceLedgerRow{
		func() SurfaceLedgerRow {
			r := apexMemberRow("apex:System.A.alpha", "System", "A", "alpha")
			r.GladeShape = ShapeAbsent
			r.GladeBehavior = BehaviorNone
			r.Evidence = EvidenceNone
			return r
		}(),
		func() SurfaceLedgerRow {
			r := apexMemberRow("apex:System.B.beta", "System", "B", "beta")
			r.GladeShape = ShapeAbsent
			r.GladeBehavior = BehaviorNone
			r.Evidence = EvidenceNone
			return r
		}(),
		func() SurfaceLedgerRow {
			r := apexMemberRow("apex:System.C.gamma", "System", "C", "gamma")
			r.GladeShape = ShapeAbsent
			r.GladeBehavior = BehaviorNone
			r.Evidence = EvidenceNone
			return r
		}(),
	}

	cu := &CorpusUsage{
		Usage: []CorpusUsageEntry{
			{UsageKey: "System.A.alpha", Namespace: "System", TypeName: "A", MemberName: "alpha",
				PubProdRefs: 10, PubTestRefs: 5, PubFailRefs: 0, PrivProdRefs: 2, PrivTestRefs: 1,
				PubProdProjects: 2, PubTestProjects: 1},
			{UsageKey: "System.B.beta", Namespace: "System", TypeName: "B", MemberName: "beta",
				PubProdRefs: 10, PubTestRefs: 5, PubFailRefs: 3, PrivProdRefs: 2, PrivTestRefs: 1,
				PubProdProjects: 2, PubTestProjects: 1},
			{UsageKey: "System.C.gamma", Namespace: "System", TypeName: "C", MemberName: "gamma",
				PubProdRefs: 5, PubTestRefs: 0, PubFailRefs: 0, PrivProdRefs: 0, PrivTestRefs: 0,
				PubProdProjects: 1, PubTestProjects: 0},
		},
	}

	profile := ComputeSupportProfile(rows, policy, cu)

	if len(profile.NonDeferredGaps) != 3 {
		t.Fatalf("NonDeferredGaps: want 3 got %d", len(profile.NonDeferredGaps))
	}

	// Ordering: corpusPassingRefs desc, corpusPassingProjects desc, corpusFailureRefs desc, surfaceId asc
	// A.alpha: passingRefs=10+5+2+1=18, passingProjects=2+1=3, failureRefs=0
	// B.beta:  passingRefs=10+5+2+1=18, passingProjects=2+1=3, failureRefs=3
	// C.gamma: passingRefs=5+0+0+0=5,  passingProjects=1+0=1, failureRefs=0
	// A.alpha and B.beta tie on passingRefs and passingProjects → B.beta comes first due to higher failureRefs
	// Then A.alpha, then C.gamma.
	expectedOrder := []string{
		"apex:System.B.beta",
		"apex:System.A.alpha",
		"apex:System.C.gamma",
	}
	for i, want := range expectedOrder {
		if profile.NonDeferredGaps[i].SurfaceID != want {
			t.Fatalf("NonDeferredGaps[%d]: want %q got %q", i, want, profile.NonDeferredGaps[i].SurfaceID)
		}
	}

	// rows must remain surfaceId ordered.
	for i := 1; i < len(profile.Rows); i++ {
		if profile.Rows[i-1].SurfaceID >= profile.Rows[i].SurfaceID {
			t.Fatalf("Rows not sorted by surfaceId: [%d]=%q >= [%d]=%q",
				i-1, profile.Rows[i-1].SurfaceID, i, profile.Rows[i].SurfaceID)
		}
	}
}

// RED 9: profile without corpus usage keeps usage and aggregate fields empty/zero.
func TestGapClassNoCorpusUsageKeepsFieldsEmpty(t *testing.T) {
	policy := SupportPolicy{
		Rules: []SupportPolicyRule{
			{Namespace: "System", Disposition: DispositionLocalRuntimeRequired, Reason: "runtime"},
		},
	}
	r := apexRow("apex:System.Test", "System", "Test")
	r.GladeShape = ShapeAbsent
	r.GladeBehavior = BehaviorNone
	r.Evidence = EvidenceNone

	profile := ComputeSupportProfile([]SurfaceLedgerRow{r}, policy, nil)

	if profile.Total != 1 {
		t.Fatalf("total: want 1 got %d", profile.Total)
	}
	if profile.Rows[0].UsageKey != "" {
		t.Fatalf("UsageKey should be empty without corpus, got %q", profile.Rows[0].UsageKey)
	}
	if profile.Rows[0].CorpusPassingRefs != 0 {
		t.Fatalf("CorpusPassingRefs should be 0, got %d", profile.Rows[0].CorpusPassingRefs)
	}
	if profile.Rows[0].CorpusFailureRefs != 0 {
		t.Fatalf("CorpusFailureRefs should be 0, got %d", profile.Rows[0].CorpusFailureRefs)
	}
	if profile.Rows[0].CorpusPassingProjects != 0 {
		t.Fatalf("CorpusPassingProjects should be 0, got %d", profile.Rows[0].CorpusPassingProjects)
	}
	if len(profile.CorpusUsage) != 0 {
		t.Fatalf("CorpusUsage should be empty, got %d entries", len(profile.CorpusUsage))
	}
}

// RED 7: aggregate corpus fields use no private identity data (no project names, paths, source excerpts).
func TestGapClassCorpusFieldsDoNotLeakIdentity(t *testing.T) {
	policy := SupportPolicy{
		Rules: []SupportPolicyRule{
			{Namespace: "System", Disposition: DispositionLocalRuntimeRequired, Reason: "runtime"},
		},
	}
	r := apexRow("apex:System.Test", "System", "Test")
	r.GladeShape = ShapeAbsent
	r.GladeBehavior = BehaviorNone
	r.Evidence = EvidenceNone

	cu := &CorpusUsage{
		Usage: []CorpusUsageEntry{
			{UsageKey: "System.Test", Namespace: "System", TypeName: "Test",
				PubProdRefs: 3, PubTestRefs: 2, PubFailRefs: 1, PrivProdRefs: 1, PrivTestRefs: 1,
				PubProdProjects: 1, PubTestProjects: 1, PubFailProjects: 1, PrivProdProjects: 1, PrivTestProjects: 1},
		},
	}

	profile := ComputeSupportProfile([]SurfaceLedgerRow{r}, policy, cu)

	// Row-level fields must be privacy-safe aggregates.
	row := profile.Rows[0]
	// corpusPassingRefs = pubProd + pubTest + privProd + privTest = 3+2+1+1 = 7
	if row.CorpusPassingRefs != 7 {
		t.Fatalf("CorpusPassingRefs: want 7 got %d", row.CorpusPassingRefs)
	}
	// corpusFailureRefs = pubFail = 1
	if row.CorpusFailureRefs != 1 {
		t.Fatalf("CorpusFailureRefs: want 1 got %d", row.CorpusFailureRefs)
	}
	// corpusPassingProjects = pubProdProj + pubTestProj + privProdProj + privTestProj = 1+1+1+1 = 4
	if row.CorpusPassingProjects != 4 {
		t.Fatalf("CorpusPassingProjects: want 4 got %d", row.CorpusPassingProjects)
	}

	// Row JSON must NOT contain project names, paths, or source excerpts.
	// The corpusUsage section may contain aggregate counts — that's expected.
	// We only check that row-level fields are privacy-safe.
	var buf bytes.Buffer
	if err := WriteSupportProfileJSON(&buf, profile); err != nil {
		t.Fatalf("write JSON: %v", err)
	}
	// Only the row's own JSON fields must not contain identity data.
	// Serialize just one row to verify.
	rowJSON, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("marshal row: %v", err)
	}
	rowStr := string(rowJSON)
	if strings.Contains(rowStr, "privProdFiles") {
		t.Fatalf("row JSON must not expose privProdFiles")
	}
	if strings.Contains(rowStr, "projectName") {
		t.Fatalf("row JSON must not expose project names")
	}
}

// RED 10: existing support-profile test still works with new fields.
func TestGapClassDeterministicJSONStillWorks(t *testing.T) {
	// This verifies the new fields serialize/deserialize cleanly and don't break existing tests.
	policy := buildSeedPolicy()
	rows := []SurfaceLedgerRow{
		apexRow("apex:System.String", "System", "String"),
		apexRow("apex:Reports.ReportManager", "Reports", "ReportManager"),
	}

	profile := ComputeSupportProfile(rows, policy, nil)

	// ByGapClass must be initialized (not nil).
	if profile.ByGapClass == nil {
		t.Fatalf("ByGapClass must be initialized")
	}

	// Runtime closure requires dual evidence; compile-shape closure accepts local fixture evidence.
	wantGapByID := map[string]string{
		"apex:System.String":         GapMissingEvidence,
		"apex:Reports.ReportManager": "",
	}
	for _, r := range profile.Rows {
		if want := wantGapByID[r.SurfaceID]; r.GapClass != want {
			t.Fatalf("row %s gapClass: want %q got %q", r.SurfaceID, want, r.GapClass)
		}
	}

	// Deterministic JSON.
	var buf1, buf2 bytes.Buffer
	profile1 := ComputeSupportProfile(rows, policy, nil)
	if err := WriteSupportProfileJSON(&buf1, profile1); err != nil {
		t.Fatalf("first write: %v", err)
	}
	profile2 := ComputeSupportProfile(rows, policy, nil)
	if err := WriteSupportProfileJSON(&buf2, profile2); err != nil {
		t.Fatalf("second write: %v", err)
	}
	if buf1.String() != buf2.String() {
		t.Fatalf("JSON output not deterministic")
	}
}

// --- SF-CB16 phase-0 Terra rework tests ---

// RED 1: A namespace and type-family rule that both match one row without an
// override fails validation even when their dispositions are the same.
func TestOverlapSameDispositionNoOverrideFails(t *testing.T) {
	policy := SupportPolicy{
		Rules: []SupportPolicyRule{
			{
				Namespace:   "MyNS",
				Disposition: DispositionLocalRuntimeRequired,
				Reason:      "ns rule",
			},
			{
				TypeFamily:  "my-family",
				Disposition: DispositionLocalRuntimeRequired,
				Reason:      "tf rule",
			},
		},
	}
	rows := []SurfaceLedgerRow{
		func() SurfaceLedgerRow {
			r := apexRow("apex:MyNS.MyType", "MyNS", "MyType")
			r.SalesforceSurfaceFamily = "my-family"
			return r
		}(),
	}

	profile := ComputeSupportProfile(rows, policy, nil)

	if len(profile.Rows) != 1 {
		t.Fatalf("rows: want 1 got %d", len(profile.Rows))
	}

	// Must report an ambiguity validation error.
	found := false
	for _, err := range profile.ValidationErrors {
		if strings.Contains(err, "ambiguous classification for row apex:MyNS.MyType") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected same-disposition ambiguity validation error, got: %v", profile.ValidationErrors)
	}

	if profile.Rows[0].MatchRule != "ambiguous" {
		t.Fatalf("match rule: want ambiguous got %q", profile.Rows[0].MatchRule)
	}
}

// RED 2: Conflicting matching rules without an override fail validation.
func TestOverlapConflictingNoOverrideFails(t *testing.T) {
	policy := SupportPolicy{
		Rules: []SupportPolicyRule{
			{
				Namespace:   "MyNS",
				Disposition: DispositionLocalRuntimeRequired,
				Reason:      "ns rule",
			},
			{
				TypeFamily:  "my-family",
				Disposition: DispositionHostedDeferred,
				Reason:      "tf rule",
			},
		},
	}
	rows := []SurfaceLedgerRow{
		func() SurfaceLedgerRow {
			r := apexRow("apex:MyNS.MyType", "MyNS", "MyType")
			r.SalesforceSurfaceFamily = "my-family"
			return r
		}(),
	}

	profile := ComputeSupportProfile(rows, policy, nil)

	if len(profile.Rows) != 1 {
		t.Fatalf("rows: want 1 got %d", len(profile.Rows))
	}

	found := false
	for _, err := range profile.ValidationErrors {
		if strings.Contains(err, "conflicting classifications for row apex:MyNS.MyType") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected conflicting classification validation error, got: %v", profile.ValidationErrors)
	}

	if profile.Rows[0].MatchRule != "ambiguous" {
		t.Fatalf("match rule: want ambiguous got %q", profile.Rows[0].MatchRule)
	}
}

// RED 3: Exactly one explicit narrower override selects that rule independent of
// policy array order and produces no overlap error.
func TestOverlapOneOverrideWins(t *testing.T) {
	// Put the override LAST in the rules array to prove array-order independence.
	policy := SupportPolicy{
		Rules: []SupportPolicyRule{
			{
				Namespace:   "System",
				Disposition: DispositionLocalRuntimeRequired,
				Reason:      "system runtime",
			},
			{
				SurfacePrefix: "apex:System.Hosted",
				Disposition:   DispositionHostedDeferred,
				Override:      true,
				Reason:        "hosted surface",
			},
		},
	}
	rows := []SurfaceLedgerRow{
		func() SurfaceLedgerRow {
			r := apexMemberRow("apex:System.Hosted.HostedClass.doIt", "System.Hosted", "HostedClass", "doIt")
			return r
		}(),
	}

	profile := ComputeSupportProfile(rows, policy, nil)

	if len(profile.Rows) != 1 {
		t.Fatalf("rows: want 1 got %d", len(profile.Rows))
	}

	// Override must win: hosted-deferred.
	if profile.Rows[0].Disposition != DispositionHostedDeferred {
		t.Fatalf("disposition: want hosted-deferred (override) got %s", profile.Rows[0].Disposition)
	}
	if !strings.Contains(profile.Rows[0].MatchRule, "surfacePrefix=apex:System.Hosted") {
		t.Fatalf("match rule: want surfacePrefix=apex:System.Hosted got %q", profile.Rows[0].MatchRule)
	}

	// No validation error.
	if len(profile.ValidationErrors) != 0 {
		t.Fatalf("expected zero validation errors with one override, got: %v", profile.ValidationErrors)
	}
}

// RED 4: Two matching overrides fail validation.
func TestOverlapTwoOverridesFails(t *testing.T) {
	policy := SupportPolicy{
		Rules: []SupportPolicyRule{
			{
				Namespace:   "System",
				Disposition: DispositionLocalRuntimeRequired,
				Reason:      "system runtime",
			},
			{
				SurfacePrefix: "apex:System.Hosted",
				Disposition:   DispositionHostedDeferred,
				Override:      true,
				Reason:        "hosted override A",
			},
			{
				SurfacePrefix: "apex:System.Hosted",
				Disposition:   DispositionCompileShapeRequired,
				Override:      true,
				Reason:        "hosted override B",
			},
		},
	}
	rows := []SurfaceLedgerRow{
		func() SurfaceLedgerRow {
			r := apexMemberRow("apex:System.Hosted.HostedClass.doIt", "System.Hosted", "HostedClass", "doIt")
			return r
		}(),
	}

	profile := ComputeSupportProfile(rows, policy, nil)

	if len(profile.Rows) != 1 {
		t.Fatalf("rows: want 1 got %d", len(profile.Rows))
	}

	found := false
	for _, err := range profile.ValidationErrors {
		if strings.Contains(err, "multiple overrides match row apex:System.Hosted.HostedClass.doIt") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected multiple overrides validation error, got: %v", profile.ValidationErrors)
	}

	if profile.Rows[0].MatchRule != "ambiguous" {
		t.Fatalf("match rule: want ambiguous got %q", profile.Rows[0].MatchRule)
	}
}

// RED 5: An unclassified future row remains rejected and does not create an
// empty byDisposition key.
func TestUnclassifiedRowNoEmptyDispositionKey(t *testing.T) {
	policy := SupportPolicy{
		Rules: []SupportPolicyRule{
			{
				Namespace:   "System",
				Disposition: DispositionLocalRuntimeRequired,
				Reason:      "system runtime",
			},
		},
	}
	rows := []SurfaceLedgerRow{
		apexRow("apex:FutureNS.SomeType", "FutureNS", "SomeType"),
	}

	profile := ComputeSupportProfile(rows, policy, nil)

	if len(profile.UnclassifiedRows) != 1 {
		t.Fatalf("unclassified rows: want 1 got %d", len(profile.UnclassifiedRows))
	}
	if profile.UnclassifiedRows[0].SurfaceID != "apex:FutureNS.SomeType" {
		t.Fatalf("unclassified row: got %q", profile.UnclassifiedRows[0].SurfaceID)
	}

	// No empty disposition key.
	if _, exists := profile.ByDisposition[""]; exists {
		t.Fatalf("byDisposition must not contain empty key, got: %v", profile.ByDisposition)
	}

	// Validation error must still report the unclassified row.
	found := false
	for _, err := range profile.ValidationErrors {
		if strings.Contains(err, "unclassified Apex row: apex:FutureNS.SomeType") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected unclassified validation error, got: %v", profile.ValidationErrors)
	}
}

// RED 6: All six ledger rows for the five retained ConnectApi methods classify as
// deterministic-mock-required; the remaining ConnectApi surface stays hosted-deferred.
func TestConnectApiSixRetainedMethodsDeterministicMock(t *testing.T) {
	policy := SupportPolicy{
		Rules: []SupportPolicyRule{
			{
				Namespace:   "ConnectApi",
				Disposition: DispositionHostedDeferred,
				Reason:      "connect-api deferred",
				MemberExceptions: []SupportPolicyMemberException{
					{TypeName: "Organization", MemberName: "getSettings", Disposition: DispositionDeterministicMockRequired, Reason: "observed corpus usage"},
					{TypeName: "UserProfiles", MemberName: "setPhoto", Disposition: DispositionDeterministicMockRequired, Reason: "observed corpus usage"},
					{TypeName: "UserProfiles", MemberName: "deletePhoto", Disposition: DispositionDeterministicMockRequired, Reason: "observed corpus usage"},
					{TypeName: "Communities", MemberName: "getCommunity", Disposition: DispositionDeterministicMockRequired, Reason: "observed corpus usage"},
					{TypeName: "ChatterUsers", MemberName: "getFollowings", Disposition: DispositionDeterministicMockRequired, Reason: "observed corpus usage"},
					{TypeName: "NamedCredentials", MemberName: "createExternalCredential", Disposition: DispositionDeterministicMockRequired, Reason: "observed expected-success corpus usage"},
					{TypeName: "NamedCredentials", MemberName: "createNamedCredential", Disposition: DispositionDeterministicMockRequired, Reason: "observed expected-success corpus usage"},
					{TypeName: "NamedCredentials", MemberName: "getExternalCredential", Disposition: DispositionDeterministicMockRequired, Reason: "observed expected-success corpus usage"},
					{TypeName: "ManagedContent", MemberName: "getAllManagedContent", Disposition: DispositionDeterministicMockRequired, Reason: "observed expected-success corpus usage"},
					{TypeName: "ManagedContent", MemberName: "getManagedContentByContentKeys", Disposition: DispositionDeterministicMockRequired, Reason: "observed expected-success corpus usage"},
				},
			},
		},
	}
	rows := []SurfaceLedgerRow{
		// Five existing retained rows (to prevent stale-exception noise).
		apexMemberRow("apex:ConnectApi.Organization.getSettings", "ConnectApi", "Organization", "getSettings"),
		apexMemberRow("apex:ConnectApi.UserProfiles.setPhoto", "ConnectApi", "UserProfiles", "setPhoto"),
		apexMemberRow("apex:ConnectApi.UserProfiles.deletePhoto", "ConnectApi", "UserProfiles", "deletePhoto"),
		apexMemberRow("apex:ConnectApi.Communities.getCommunity", "ConnectApi", "Communities", "getCommunity"),
		apexMemberRow("apex:ConnectApi.ChatterUsers.getFollowings", "ConnectApi", "ChatterUsers", "getFollowings"),
		// Six retained rows from the new five method families.
		apexMemberRow("apex:ConnectApi.NamedCredentials.createExternalCredential", "ConnectApi", "NamedCredentials", "createExternalCredential"),
		apexMemberRow("apex:ConnectApi.NamedCredentials.createNamedCredential", "ConnectApi", "NamedCredentials", "createNamedCredential"),
		apexMemberRow("apex:ConnectApi.NamedCredentials.getExternalCredential", "ConnectApi", "NamedCredentials", "getExternalCredential"),
		apexMemberRow("apex:ConnectApi.ManagedContent.getAllManagedContent", "ConnectApi", "ManagedContent", "getAllManagedContent"),
		apexMemberRow("apex:ConnectApi.ManagedContent.getManagedContentByContentKeys", "ConnectApi", "ManagedContent", "getManagedContentByContentKeys"),
		// Second overload row (one of the five names has an overload variant).
		apexMemberRow("apex:ConnectApi.ManagedContent.getManagedContentByContentKeys(List)", "ConnectApi", "ManagedContent", "getManagedContentByContentKeys"),
		// Remaining ConnectApi surface.
		apexRow("apex:ConnectApi.SomeDTO", "ConnectApi", "SomeDTO"),
	}

	profile := ComputeSupportProfile(rows, policy, nil)

	if profile.Total != 12 {
		t.Fatalf("total: want 12 got %d", profile.Total)
	}

	// Eleven rows must be deterministic-mock-required (5 existing + 6 new).
	dmCount := 0
	for _, r := range profile.Rows {
		if r.Disposition == DispositionDeterministicMockRequired {
			dmCount++
		}
	}
	if dmCount != 11 {
		t.Fatalf("deterministic-mock-required rows: want 11 got %d", dmCount)
	}

	// The remaining ConnectApi row must be hosted-deferred.
	for _, r := range profile.Rows {
		if r.SurfaceID == "apex:ConnectApi.SomeDTO" {
			if r.Disposition != DispositionHostedDeferred {
				t.Fatalf("ConnectApi.SomeDTO: want hosted-deferred got %s", r.Disposition)
			}
		}
	}

	// No validation errors.
	if len(profile.ValidationErrors) != 0 {
		t.Fatalf("expected zero validation errors, got: %v", profile.ValidationErrors)
	}

	// No stale member exceptions.
	for _, err := range profile.ValidationErrors {
		if strings.Contains(err, "stale member exception") {
			t.Fatalf("unexpected stale member exception: %s", err)
		}
	}
}
