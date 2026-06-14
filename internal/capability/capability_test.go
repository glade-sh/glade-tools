package capability

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestMVPReportIsReadyWhenRequiredFeaturesAreSupported(t *testing.T) {
	report := MVPReport()
	if !report.Ready {
		t.Fatalf("MVP report should be ready once required features are supported: %#v", report)
	}
	if report.Required == 0 || report.Incomplete != 0 || report.Complete != report.Required {
		t.Fatalf("report = %#v", report)
	}
	for _, feature := range report.Features {
		if feature.Required && feature.Status != StatusSupported {
			t.Fatalf("%s status = %s, want %s", feature.ID, feature.Status, StatusSupported)
		}
	}
}

func TestMVPReportIncludesStatusCounts(t *testing.T) {
	report := MVPReport()
	total := 0
	for _, status := range []Status{StatusSupported, StatusPartial, StatusStub, StatusUnsupported, StatusUnknown} {
		total += report.StatusCounts[status]
	}
	if total != report.Total {
		t.Fatalf("status count total = %d, want %d", total, report.Total)
	}
	if report.StatusCounts[StatusSupported] != report.Complete {
		t.Fatalf("supported count = %d, want complete count %d", report.StatusCounts[StatusSupported], report.Complete)
	}
}

func TestLocalMVPDXRowsAreSupportedWithPostMVPTails(t *testing.T) {
	report := MVPReport()
	features := map[string]Feature{}
	for _, feature := range report.Features {
		features[feature.ID] = feature
	}

	postMVP := map[string]string{
		"dap.command":      "dap.live-ide-orchestration",
		"lsp.command":      "lsp.context-completion",
		"profile.native":   "profile.pprof-and-timing",
		"watch.command":    "watch.profile-trace-reports",
		"server.local-api": "server.rest-breadth",
	}
	for id, postID := range postMVP {
		feature, ok := features[id]
		if !ok {
			t.Fatalf("missing local MVP feature %s", id)
		}
		if !feature.Required || feature.Status != StatusSupported {
			t.Fatalf("%s = required %v status %s, want required supported", id, feature.Required, feature.Status)
		}
		if !strings.Contains(feature.Notes, "Local MVP") {
			t.Fatalf("%s notes do not describe the local MVP contract: %s", id, feature.Notes)
		}

		tail, ok := features[postID]
		if !ok {
			t.Fatalf("missing post-MVP feature %s for %s", postID, id)
		}
		if tail.Required || tail.Status != StatusPartial {
			t.Fatalf("%s = required %v status %s, want optional partial", postID, tail.Required, tail.Status)
		}
		if !strings.Contains(tail.Notes, "Post-MVP") {
			t.Fatalf("%s notes do not identify the post-MVP contract: %s", postID, tail.Notes)
		}
	}
}

func TestServerRESTBreadthNotesTrackCompositeBatchEvidence(t *testing.T) {
	for _, feature := range MVPFeatures() {
		if feature.ID != "server.rest-breadth" {
			continue
		}
		if feature.Status != StatusPartial {
			t.Fatalf("%s status = %s, want %s", feature.ID, feature.Status, StatusPartial)
		}
		if strings.Contains(feature.Notes, "Composite Batch/Graph") {
			t.Fatalf("server.rest-breadth notes still group Batch with unsupported Graph: %s", feature.Notes)
		}
		if !strings.Contains(feature.Notes, "Composite Batch local subrequests") {
			t.Fatalf("server.rest-breadth notes missing Batch local evidence: %s", feature.Notes)
		}
		if !strings.Contains(feature.Notes, "Bulk API v2 simple scalar query job create/status/whole-result CSV") || !strings.Contains(feature.Notes, "locator paging") {
			t.Fatalf("server.rest-breadth notes missing Bulk local evidence and remaining fence: %s", feature.Notes)
		}
		return
	}
	t.Fatal("missing server.rest-breadth feature")
}

func TestLSPContextCompletionNotesTestBackedSOQLSelectRanking(t *testing.T) {
	feature := findMVPFeatureForTest(t, "lsp.context-completion")
	if feature.Status != StatusPartial {
		t.Fatalf("status = %s, want %s", feature.Status, StatusPartial)
	}
	if !strings.Contains(feature.Notes, "SOQL SELECT") || !strings.Contains(feature.Notes, "SObject fields") {
		t.Fatalf("notes do not name the test-backed context: %s", feature.Notes)
	}
}

func TestProfileTimingNotesTrackWallClockSummaryEvidence(t *testing.T) {
	feature := findMVPFeatureForTest(t, "profile.pprof-and-timing")
	if feature.Status != StatusPartial {
		t.Fatalf("status = %s, want %s", feature.Status, StatusPartial)
	}
	if !strings.Contains(feature.Notes, "wall-clock summary") || !strings.Contains(feature.Notes, "pprof-compatible CPU output") {
		t.Fatalf("notes do not name the test-backed timing slice and remaining fence: %s", feature.Notes)
	}
}

func TestStdlibCatalogHasNoPartialRows(t *testing.T) {
	for _, row := range StdlibMatrix() {
		if row.Status == StatusPartial {
			t.Fatalf("%s %s is partial: %s", row.Area, row.API, row.Notes)
		}
	}
}

func findMVPFeatureForTest(t *testing.T, id string) Feature {
	t.Helper()
	for _, feature := range MVPFeatures() {
		if feature.ID == id {
			return feature
		}
	}
	t.Fatalf("missing %s feature", id)
	return Feature{}
}

func TestCoreRuntimeMVPFeaturesAreSupportedWithPostMVPTails(t *testing.T) {
	report := MVPReport()
	features := map[string]Feature{}
	for _, feature := range report.Features {
		features[feature.ID] = feature
	}

	postMVP := map[string]string{
		"limits.core": "limits.exact-accounting",
		"stdlib.core": "stdlib.platform-breadth",
	}
	for id, postID := range postMVP {
		feature, ok := features[id]
		if !ok {
			t.Fatalf("missing core runtime MVP feature %s", id)
		}
		if !feature.Required || feature.Status != StatusSupported {
			t.Fatalf("%s = required %v status %s, want required supported", id, feature.Required, feature.Status)
		}
		notes := strings.ToLower(feature.Notes)
		for _, blocked := range []string{"incomplete", "broader", "exact salesforce accounting", "unimplemented platform"} {
			if strings.Contains(notes, blocked) {
				t.Fatalf("%s supported notes still carry open gap language %q: %s", id, blocked, feature.Notes)
			}
		}

		tail, ok := features[postID]
		if !ok {
			t.Fatalf("missing post-MVP feature %s for %s", postID, id)
		}
		if tail.Required || tail.Status != StatusPartial {
			t.Fatalf("%s = required %v status %s, want optional partial", postID, tail.Required, tail.Status)
		}
		if !strings.Contains(tail.Notes, "Post-MVP") {
			t.Fatalf("%s notes do not identify the post-MVP contract: %s", postID, tail.Notes)
		}
	}
}

func TestWriteJSON(t *testing.T) {
	var out bytes.Buffer
	if err := WriteJSON(&out, MVPReport()); err != nil {
		t.Fatal(err)
	}
	var decoded Report
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Target != "full-featured glade-parity MVP" {
		t.Fatalf("target = %q", decoded.Target)
	}
	if decoded.StatusCounts[StatusSupported] != decoded.Complete {
		t.Fatalf("supported count = %d, want complete count %d", decoded.StatusCounts[StatusSupported], decoded.Complete)
	}
}

func TestWriteText(t *testing.T) {
	var out bytes.Buffer
	if err := WriteText(&out, MVPReport()); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, "MVP readiness: ready") || !strings.Contains(text, "Required complete: 21/21") {
		t.Fatalf("text output = %q", text)
	}
}

func TestWriteMarkdown(t *testing.T) {
	var out bytes.Buffer
	if err := WriteMarkdown(&out, MVPReport()); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{
		"# Compatibility Dashboard",
		"Generated from the first-party compat plugin capability catalog.",
		"Required complete:",
		"| Area | ID | Status | Capability | Notes |",
		"`triggers.runtime`",
		"## Tracked Post-MVP Capabilities",
		"`server.rest-breadth`",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("markdown output missing %q: %q", want, text)
		}
	}
}

func TestWriteKnownGapsMarkdown(t *testing.T) {
	var out bytes.Buffer
	if err := WriteKnownGapsMarkdown(&out, MVPReport()); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{
		"# Known Gaps",
		"Generated from the first-party compat plugin capability catalog.",
		"All required capabilities are currently `supported`.",
		"No required MVP capability gaps are currently tracked.",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("known gaps output missing %q: %q", want, text)
		}
	}
}

func TestLocalApexExecutionMVPFeaturesAreSupported(t *testing.T) {
	targets := map[string]bool{
		"apex.parser.project-scale": true,
		"apex.sema.body":            true,
		"dml.apex":                  true,
		"fixtures.persistence":      true,
		"sobject.apex":              true,
		"soql.apex":                 true,
		"triggers.runtime":          true,
	}
	for _, feature := range MVPFeatures() {
		if !targets[feature.ID] {
			continue
		}
		delete(targets, feature.ID)
		if feature.Status != StatusSupported {
			t.Fatalf("%s status = %s, want %s", feature.ID, feature.Status, StatusSupported)
		}
		notes := strings.ToLower(feature.Notes)
		for _, blocked := range []string{"remain incomplete", "remains incomplete", "full ", "broader "} {
			if strings.Contains(notes, blocked) {
				t.Fatalf("%s supported notes still carry open gap language %q: %s", feature.ID, blocked, feature.Notes)
			}
		}
	}
	for id := range targets {
		t.Fatalf("missing local Apex execution MVP feature %s", id)
	}
}

func TestCoreRuntimeControlFlowCapabilityIsSupported(t *testing.T) {
	for _, feature := range MVPFeatures() {
		if feature.ID != "vm.control-flow" {
			continue
		}
		if feature.Status != StatusSupported {
			t.Fatalf("%s status = %s, want %s", feature.ID, feature.Status, StatusSupported)
		}
		notes := strings.ToLower(feature.Notes)
		for _, blocked := range []string{"remaining gap", "remain incomplete", "remains incomplete", "outside control-flow"} {
			if strings.Contains(notes, blocked) {
				t.Fatalf("%s supported notes still carry open gap language %q: %s", feature.ID, blocked, feature.Notes)
			}
		}
		return
	}
	t.Fatal("missing vm.control-flow MVP feature")
}

func TestDatabaseStdlibRowsAreLocallyPromotedOrFenced(t *testing.T) {
	for _, entry := range StdlibMatrix() {
		if entry.Area != "Database" {
			continue
		}
		if entry.Status == StatusPartial {
			t.Fatalf("Database stdlib row %s remains partial: %s", entry.API, entry.Notes)
		}
		if entry.Status == StatusSupported && entry.Notes == "" {
			t.Fatalf("Database stdlib row %s needs local-model notes", entry.API)
		}
	}
}

func TestDatetimeArithmeticStdlibRowsAreSupported(t *testing.T) {
	watched := map[string]bool{
		"Datetime.addHours":   true,
		"Datetime.addMinutes": true,
		"Datetime.addSeconds": true,
	}
	for _, entry := range StdlibMatrix() {
		if !watched[entry.API] {
			continue
		}
		delete(watched, entry.API)
		if entry.Status != StatusSupported {
			t.Fatalf("%s status = %s, want %s", entry.API, entry.Status, StatusSupported)
		}
		if !strings.Contains(entry.Notes, "UTC-local") {
			t.Fatalf("%s notes do not describe local datetime arithmetic: %s", entry.API, entry.Notes)
		}
	}
	for api := range watched {
		t.Fatalf("missing stdlib row %s", api)
	}
}

func TestDeterministicURLStdlibRowsAreSupported(t *testing.T) {
	watched := map[string]bool{
		"URL.getOrgDomainUrl":      true,
		"URL.getSalesforceBaseUrl": true,
	}
	for _, entry := range StdlibMatrix() {
		if !watched[entry.API] {
			continue
		}
		delete(watched, entry.API)
		if entry.Status != StatusSupported {
			t.Fatalf("%s status = %s, want %s", entry.API, entry.Status, StatusSupported)
		}
		if !strings.Contains(entry.Notes, "Deterministic local") {
			t.Fatalf("%s notes do not describe deterministic local URL behavior: %s", entry.API, entry.Notes)
		}
	}
	for api := range watched {
		t.Fatalf("missing stdlib row %s", api)
	}
}

func TestUserInfoLocalIdentityStdlibRowsAreSupported(t *testing.T) {
	watched := map[string]bool{
		"UserInfo.getFirstName":                true,
		"UserInfo.getLanguage":                 true,
		"UserInfo.getLastName":                 true,
		"UserInfo.getLocale":                   true,
		"UserInfo.getName":                     true,
		"UserInfo.getOrganizationId":           true,
		"UserInfo.getProfileId":                true,
		"UserInfo.getSessionId":                true,
		"UserInfo.getUserEmail":                true,
		"UserInfo.getUserId":                   true,
		"UserInfo.getUserName":                 true,
		"UserInfo.getUserType":                 true,
		"UserInfo.isMultiCurrencyOrganization": true,
	}
	for _, entry := range StdlibMatrix() {
		if !watched[entry.API] {
			continue
		}
		delete(watched, entry.API)
		if entry.Status != StatusSupported {
			t.Fatalf("%s status = %s, want %s", entry.API, entry.Status, StatusSupported)
		}
		notes := strings.ToLower(entry.Notes)
		if !strings.Contains(notes, "local") && !strings.Contains(notes, "runas") {
			t.Fatalf("%s notes do not describe the local user identity model: %s", entry.API, entry.Notes)
		}
	}
	for api := range watched {
		t.Fatalf("missing stdlib row %s", api)
	}
}

func TestStdlibSupportedRowsDoNotClaimPlaceholderOrNoOpBehavior(t *testing.T) {
	for _, entry := range StdlibMatrix() {
		if entry.Status != StatusSupported {
			continue
		}
		notes := strings.ToLower(entry.Notes)
		if strings.Contains(notes, "placeholder") || strings.Contains(notes, "no-op") {
			t.Fatalf("stdlib row %s is supported with placeholder/no-op notes: %s", entry.API, entry.Notes)
		}
	}
}

func TestCoreServiceContextStdlibRowsAreExplicitUnsupported(t *testing.T) {
	watched := map[string]bool{
		"Answers.findSimilar(Question)":     true,
		"ResetPasswordResult.getPassword()": true,
	}
	for _, entry := range StdlibMatrix() {
		if !watched[entry.API] {
			continue
		}
		if entry.Status != StatusUnsupported {
			t.Fatalf("%s = %s, want unsupported", entry.API, entry.Status)
		}
		delete(watched, entry.API)
	}
	if len(watched) > 0 {
		t.Fatalf("missing explicit unsupported core service/context rows: %#v", watched)
	}
}

func TestNoPartialRowsRemainForNoPartialsCloseoutLanes(t *testing.T) {
	targetAreas := map[string]bool{
		"ApexPages":         true,
		"Approval":          true,
		"BusinessHours":     true,
		"FeatureManagement": true,
		"HTTP":              true,
		"Limits":            true,
		"Messaging":         true,
		"PageReference":     true,
		"QuickAction":       true,
		"Schema":            true,
		"Search":            true,
		"System":            true,
		"Test":              true,
		"Type":              true,
		"WebServiceCallout": true,
	}
	for _, row := range StdlibMatrix() {
		if !targetAreas[row.Area] {
			continue
		}
		if row.Status == StatusPartial {
			t.Errorf("%s %s is still partial: %s", row.Area, row.API, row.Notes)
		}
	}
}

func assertStdlibStatuses(t *testing.T, watched map[string]Status) {
	t.Helper()
	for _, entry := range StdlibMatrix() {
		want, ok := watched[entry.API]
		if !ok {
			continue
		}
		delete(watched, entry.API)
		if entry.Status != want {
			t.Fatalf("%s = %s, want %s: %s", entry.API, entry.Status, want, entry.Notes)
		}
		if entry.Notes == "" {
			t.Fatalf("%s needs local-model notes", entry.API)
		}
	}
	if len(watched) > 0 {
		t.Fatalf("missing stdlib rows: %#v", watched)
	}
}

func TestQuickActionStdlibRowsAreLocalPartial(t *testing.T) {
	watched := map[string]Status{
		"QuickAction.describeAvailableActions":                                          StatusSupported,
		"QuickAction.describeAvailableQuickActions(String)":                             StatusSupported,
		"QuickAction.describeQuickActions(List<String>)":                                StatusSupported,
		"QuickAction.retrieveQuickActionTemplate(String,Id)":                            StatusSupported,
		"QuickAction.retrieveQuickActionTemplates(List<String>,Id)":                     StatusSupported,
		"QuickAction.performQuickAction":                                                StatusSupported,
		"QuickAction.performQuickAction(QuickAction.QuickActionRequest)":                StatusSupported,
		"QuickAction.performQuickAction(QuickAction.QuickActionRequest,Boolean)":        StatusSupported,
		"QuickAction.performQuickActions(List<QuickAction.QuickActionRequest>)":         StatusSupported,
		"QuickAction.performQuickActions(List<QuickAction.QuickActionRequest>,Boolean)": StatusSupported,
		"Test.newSendEmailQuickActionDefaults(Id,Id)":                                   StatusSupported,
	}
	assertStdlibStatuses(t, watched)
}

func TestLocalContextStdlibRowsArePromoted(t *testing.T) {
	watched := map[string]Status{
		"Request.getCurrent()":                               StatusSupported,
		"RequestImpl.getCurrent()":                           StatusSupported,
		"Request.getRequestId()":                             StatusSupported,
		"Request.getQuiddity()":                              StatusSupported,
		"UIRequest.getCurrent()":                             StatusSupported,
		"UIRequest.getRequestHeader(String)":                 StatusSupported,
		"UserInfo.hasPackageLicense(Id)":                     StatusSupported,
		"UserInfo.isCurrentUserLicensedForPackage(Id)":       StatusSupported,
		"Test.enableChangeDataCapture()":                     StatusSupported,
		"Test.getEventBus()":                                 StatusSupported,
		"Test.getExternalService()":                          StatusSupported,
		"Test.setContinuationResponse(String,HttpResponse)":  StatusSupported,
		"Test.invokeContinuationMethod(Object,Continuation)": StatusSupported,
		"Test.testInstall(InstallHandler,Version)":           StatusSupported,
		"Test.testInstall(InstallHandler,Version,Boolean)":   StatusSupported,
		"Test.testUninstall(UninstallHandler)":               StatusSupported,
		"Test.testNotificationActionHandler(Messaging.NotificationActionHandler,Messaging.ActionableNotification)": StatusSupported,
		"Test.testSandboxPostCopyScript(SandboxPostCopy,Id,Id,String)":                                             StatusSupported,
		"Test.testSandboxPostCopyScript(SandboxPostCopy,Id,Id,String,Boolean)":                                     StatusSupported,
		"SandboxPostCopy.runApexClass(SandboxContext)":                                                             StatusSupported,
		"SandboxContext.organizationId()":                                                                          StatusSupported,
		"SandboxContext.sandboxId()":                                                                               StatusSupported,
		"SandboxContext.sandboxName()":                                                                             StatusSupported,
	}
	assertStdlibStatuses(t, watched)
}

func TestAsyncSearchApprovalBusinessHoursStdlibRowsArePromoted(t *testing.T) {
	watched := map[string]Status{
		"System.schedule(String,String,Object)":                                        StatusSupported,
		"Schedulable.execute(SchedulableContext)":                                      StatusSupported,
		"SchedulableContext.getTriggerId()":                                            StatusSupported,
		"Test.setCurrentPageReference(Object)":                                         StatusSupported,
		"Search.find(String,Object)":                                                   StatusSupported,
		"Search.query(String,Object)":                                                  StatusSupported,
		"Search.suggest(String,String,Object)":                                         StatusSupported,
		"Search.suggest(String,String,Object,Object)":                                  StatusSupported,
		"System.enqueueJob(Object,Object)":                                             StatusSupported,
		"AccessLevel.withPermissionSetId(String)":                                      StatusSupported,
		"System.runAs(Object,Object)":                                                  StatusSupported,
		"System.runAs(Package.Version)":                                                StatusSupported,
		"Approval.process(Approval.ProcessRequest)":                                    StatusSupported,
		"Approval.process(Approval.ProcessRequest, Boolean)":                           StatusSupported,
		"TrailblazerIdentity.generateUserEmailVerificationToken(String,String,String)": StatusSupported,
		"TrailblazerIdentity.getUserOrgInfo(List<String>)":                             StatusSupported,
		"TrailblazerIdentity.splunkLog(String,String)":                                 StatusSupported,
		"BusinessHours.add(String, Datetime, Long)":                                    StatusSupported,
		"BusinessHours.addGmt(String, Datetime, Long)":                                 StatusSupported,
		"BusinessHours.diff(String, Datetime, Datetime)":                               StatusSupported,
		"BusinessHours.isWithin(String, Datetime)":                                     StatusSupported,
		"BusinessHours.nextStartDate(String, Datetime)":                                StatusSupported,
	}
	assertStdlibStatuses(t, watched)
}

func TestLWCStdlibIntegrationRowsArePromotedOrBounded(t *testing.T) {
	watched := map[string]Status{
		"AccessLevel.withPermissionSetId(String)":               StatusSupported,
		"Type.newInstance":                                      StatusSupported,
		"PageReference(record)":                                 StatusSupported,
		"JSON.deserialize":                                      StatusSupported,
		"JSON.deserializeStrict":                                StatusSupported,
		"JSON.deserializeUntyped":                               StatusSupported,
		"JSON.serialize":                                        StatusSupported,
		"JSON.serializePretty":                                  StatusSupported,
		"Schema.getGlobalDescribe()":                            StatusSupported,
		"Schema.describeSObjects(List<String>)":                 StatusSupported,
		"DescribeFieldResult":                                   StatusSupported,
		"DescribeSObjectResult":                                 StatusSupported,
		"Search.query / SOSL FIND":                              StatusSupported,
		"Search.query(String,AccessLevel)":                      StatusSupported,
		"Search.find":                                           StatusSupported,
		"Search.find(String,AccessLevel)":                       StatusSupported,
		"Search.suggest":                                        StatusSupported,
		"Search.suggest(String,String,Search.SuggestionOption)": StatusSupported,
		"Search.suggest(String,String,Search.SuggestionOption,AccessLevel)": StatusSupported,
		"ApexPages.Message":                                StatusSupported,
		"System.enqueueJob(Object,Object)":                 StatusSupported,
		"Test.getEventBus()":                               StatusSupported,
		"Test.getExternalService()":                        StatusSupported,
		"Test.setMock":                                     StatusSupported,
		"WebServiceCallout.invoke(Object,Object,Map,List)": StatusSupported,
		"WebServiceCallout.invoke(Object,Object,Map<String,Object>,List<String>)": StatusSupported,
	}
	for _, entry := range StdlibMatrix() {
		want, ok := watched[entry.API]
		if !ok {
			continue
		}
		delete(watched, entry.API)
		if entry.Status != want {
			t.Fatalf("%s = %s, want %s: %s", entry.API, entry.Status, want, entry.Notes)
		}
		notes := strings.ToLower(entry.Notes)
		if want == StatusSupported {
			if strings.Contains(notes, "not modeled") || strings.Contains(notes, "no full") || strings.Contains(notes, "unsupported") {
				t.Fatalf("%s supported notes carry open-gap language: %s", entry.API, entry.Notes)
			}
			continue
		}
		if !strings.Contains(notes, "no ") && !strings.Contains(notes, "not modeled") && !strings.Contains(notes, "not executed") && !strings.Contains(notes, "remain") && !strings.Contains(notes, "fences") {
			t.Fatalf("%s partial notes do not name a boundary: %s", entry.API, entry.Notes)
		}
	}
	if len(watched) > 0 {
		t.Fatalf("missing LWC stdlib integration rows: %#v", watched)
	}
}

func TestWebServiceCalloutStdlibRowsAreLocallyPromotedOrFenced(t *testing.T) {
	watched := map[string]Status{
		"WebServiceCallout.invoke(Object,Object,Map,List)":                        StatusSupported,
		"WebServiceCallout.invoke(Object,Object,Map<String,Object>,List<String>)": StatusSupported,
	}
	for _, entry := range StdlibMatrix() {
		want, ok := watched[entry.API]
		if !ok {
			continue
		}
		delete(watched, entry.API)
		if entry.Status != want {
			t.Fatalf("%s = %s, want %s: %s", entry.API, entry.Status, want, entry.Notes)
		}
	}
	if len(watched) > 0 {
		t.Fatalf("missing WebServiceCallout stdlib rows: %#v", watched)
	}
}

func TestSecondTierStdlibRowsAreEvidenceBackedOrBounded(t *testing.T) {
	watched := map[string]struct {
		status Status
		notes  []string
	}{
		"Crypto.generateDigest": {
			status: StatusSupported,
			notes:  []string{"SHA-384", "SecurityException"},
		},
		"Decimal.round": {
			status: StatusSupported,
			notes:  []string{"HALF_EVEN", "RoundingMode"},
		},
		"Decimal.setScale": {
			status: StatusSupported,
			notes:  []string{"negative scale", "Salesforce scale bounds"},
		},
		"EncodingUtil.urlDecode": {
			status: StatusSupported,
			notes:  []string{"US-ASCII replacement", "UTF-16"},
		},
		"EncodingUtil.urlEncode": {
			status: StatusSupported,
			notes:  []string{"ISO-8859-1", "UTF-16"},
		},
		"Limits.get*": {
			status: StatusSupported,
			notes:  []string{"deterministic local counters", "zero-valued local service counters"},
		},
		"Messaging.SingleEmailMessage": {
			status: StatusSupported,
			notes:  []string{"DTO setters", "local file attachment"},
		},
		"Messaging.sendEmail": {
			status: StatusSupported,
			notes:  []string{"SendEmailResult", "email limits"},
		},
		"Messaging.renderStoredEmailTemplate(String,String,String,Messaging.AttachmentRetrievalOption)": {
			status: StatusSupported,
			notes:  []string{"static-resource attachments", "METADATA_WITH_BODY"},
		},
		"Messaging.renderStoredEmailTemplate(String,String,String,Messaging.AttachmentRetrievalOption,Boolean)": {
			status: StatusSupported,
			notes:  []string{"updateEmailTemplateUsage", "remote usage mutation"},
		},
		"Matcher.find": {
			status: StatusSupported,
			notes:  []string{"regexp2", "UAX #29"},
		},
		"Matcher.group": {
			status: StatusSupported,
			notes:  []string{"whole-match", "Apex UTF-16"},
		},
		"Matcher.matches": {
			status: StatusSupported,
			notes:  []string{"whole-region", "regexp2"},
		},
		"Pattern.compile": {
			status: StatusSupported,
			notes:  []string{"regexp2", "(?U)", "nested class algebra"},
		},
		"Pattern.matches": {
			status: StatusSupported,
			notes:  []string{"whole-string", "UAX #29"},
		},
		"String.split": {
			status: StatusSupported,
			notes:  []string{"empty-pattern", "nullable delimiters", "numeric backreference"},
		},
		"Test.loadData": {
			status: StatusSupported,
			notes:  []string{"CSV static-resource", "bad-header diagnostics"},
		},
		"Test.startTest": {
			status: StatusSupported,
			notes:  []string{"governor window", "local counters"},
		},
		"Test.stopTest": {
			status: StatusSupported,
			notes:  []string{"outer governor counters", "local async"},
		},
	}
	for _, entry := range StdlibMatrix() {
		want, ok := watched[entry.API]
		if !ok {
			continue
		}
		delete(watched, entry.API)
		if entry.Status != want.status {
			t.Fatalf("%s = %s, want %s: %s", entry.API, entry.Status, want.status, entry.Notes)
		}
		notes := strings.ToLower(entry.Notes)
		for _, note := range want.notes {
			if !strings.Contains(notes, strings.ToLower(note)) {
				t.Fatalf("%s notes missing %q: %s", entry.API, note, entry.Notes)
			}
		}
	}
	if len(watched) > 0 {
		t.Fatalf("missing second-tier stdlib rows: %#v", watched)
	}
}

func TestHTTPStdlibRowsAreLocallyPromotedOrFenced(t *testing.T) {
	watched := map[string]Status{
		"Http.send(HttpRequest)": StatusSupported,
	}
	for _, entry := range StdlibMatrix() {
		want, ok := watched[entry.API]
		if !ok {
			continue
		}
		delete(watched, entry.API)
		if entry.Status != want {
			t.Fatalf("%s = %s, want %s: %s", entry.API, entry.Status, want, entry.Notes)
		}
		if entry.Notes == "" {
			t.Fatalf("%s needs local-model notes", entry.API)
		}
	}
	for api := range watched {
		t.Fatalf("missing HTTP stdlib row %s", api)
	}
}

func TestDateDatetimeTimeZoneRowsAreLocallyPromotedOrFenced(t *testing.T) {
	watched := map[string]bool{
		"Date.addMonths":          true,
		"Date.addYears":           true,
		"Date.today":              true,
		"Datetime.addDays":        true,
		"Datetime.addMonths":      true,
		"Datetime.addYears":       true,
		"Datetime.format":         true,
		"Datetime.formatGmt":      true,
		"Datetime.now":            true,
		"TimeZone.getDisplayName": true,
		"TimeZone.getID":          true,
		"TimeZone.getOffset":      true,
		"TimeZone.getTimeZone":    true,
		"UserInfo.getTimeZone":    true,
	}
	for _, entry := range StdlibMatrix() {
		if !watched[entry.API] {
			continue
		}
		if entry.Status != StatusSupported {
			t.Fatalf("%s remains %s: %s", entry.API, entry.Status, entry.Notes)
		}
		if entry.Notes == "" {
			t.Fatalf("%s needs local-model notes", entry.API)
		}
	}
}
