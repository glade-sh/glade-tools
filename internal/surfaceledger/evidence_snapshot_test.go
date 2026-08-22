package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildEvidenceSnapshotReadsSurfaceID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.json")
	data := `{
  "name": "label_get",
  "evidence": [{"symbol": "System.Label.get", "surfaceId": "apex:System.Label.get(String,String)"}],
  "source": [{"path": "force-app/main/default/classes/T.cls", "content": "class T {}"}],
  "command": {"kind": "exec"},
  "expected": {"stdout": ""}
}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err := BuildEvidenceSnapshot([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	id := ApexMemberID("System", "Label", "get", []string{"String", "String"})
	if rowsByID(rows)[id].Evidence != EvidenceFixture {
		t.Fatalf("evidence row missing surface id %s: %#v", id, rows)
	}
}

func TestBuildEvidenceSnapshotKeepsNamespacedApexTypeIdentity(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.json")
	data := `{
  "name": "type_identity",
  "evidence": [{"symbol": "System.Label", "surfaceId": "apex:System.Label", "kind": "test"}],
  "command": {"kind": "test"},
  "expected": {"result": {"ok": true}}
}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err := BuildEvidenceSnapshot([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	row := rowsByID(rows)["apex:System.Label"]
	if row.Namespace != "System" || row.TypeName != "Label" || row.MemberName != "" {
		t.Fatalf("identity = namespace:%q type:%q member:%q, want System/Label/<empty>: %#v", row.Namespace, row.TypeName, row.MemberName, rows)
	}
}

func TestBuildEvidenceSnapshotMarksRuntimeGuideEvidenceSupported(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.json")
	data := `{
  "name": "query-runtime-soqlsosl-orderby",
  "evidence": [
    {"symbol": "SOQL ORDER BY", "surfaceId": "unknown:sforce_api_calls_soql_select_orderby", "kind": "test"},
    {"symbol": "Dynamic SOQL", "surfaceId": "unknown:apex_dynamic_soql", "kind": "test"}
  ],
  "command": {"kind": "test"},
  "expected": {"result": {"ok": true}}
}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err := BuildEvidenceSnapshot([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	row := rowsByID(rows)["unknown:sforce_api_calls_soql_select_orderby"]
	if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported {
		t.Fatalf("guide row evidence/behavior = %s/%s, want fixture/supported: %#v", row.Evidence, row.GladeBehavior, rows)
	}
	row = rowsByID(rows)["unknown:apex_dynamic_soql"]
	if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported {
		t.Fatalf("dynamic row evidence/behavior = %s/%s, want fixture/supported: %#v", row.Evidence, row.GladeBehavior, rows)
	}
}

func TestBuildEvidenceSnapshotDoesNotPromoteNonQueryUnknownEvidence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.json")
	data := `{
  "name": "other_runtime_unknown",
  "evidence": [{"symbol": "Other runtime guide", "surfaceId": "unknown:some_other_runtime_guide", "kind": "test"}],
  "command": {"kind": "test"},
  "expected": {"result": {"ok": true}}
}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err := BuildEvidenceSnapshot([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	row := rowsByID(rows)["unknown:some_other_runtime_guide"]
	if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorNone {
		t.Fatalf("non-query unknown evidence/behavior = %s/%s, want fixture/none: %#v", row.Evidence, row.GladeBehavior, rows)
	}
}

func TestBuildEvidenceSnapshotMarksLWCBridgeEvidenceSupported(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.json")
	data := `{
  "name": "ui-lwc-vf-local-bridge-evidence",
  "evidence": [{
    "symbol": "LWC Apex wire SObject JSON",
    "surfaceId": "lwc:apex-wire.sobject-json",
    "kind": "test"
  }],
  "command": {"kind": "test"},
  "expected": {"result": {"ok": true}}
}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err := BuildEvidenceSnapshot([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	row := rowsByID(rows)["lwc:apex-wire.sobject-json"]
	if row.Product != ProductLWC || row.Area != AreaUI || row.GladeBehavior != BehaviorSupported || row.Evidence != EvidenceFixture {
		t.Fatalf("lwc row = product:%s area:%s behavior:%s evidence:%s rows:%#v", row.Product, row.Area, row.GladeBehavior, row.Evidence, rows)
	}
}

func TestBuildEvidenceSnapshotMarksRESTServerEvidenceSupported(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.json")
	data := `{
  "name": "server-black-box",
  "evidence": [{
    "symbol": "REST versions",
    "surfaceId": "rest:dome_versions.get",
    "kind": "server"
  }],
  "serverRequests": [{"name": "versions", "method": "GET", "path": "/services/data", "status": 200}],
  "command": {"kind": "server"},
  "expected": {"result": {"ok": true}}
}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err := BuildEvidenceSnapshot([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	row := rowsByID(rows)["rest:dome_versions.get"]
	if row.Product != ProductREST || row.Area != AreaServer || row.GladeShape == ShapeAbsent || row.GladeBehavior != BehaviorSupported || row.Evidence != EvidenceFixture {
		t.Fatalf("rest row = product:%s area:%s shape:%s behavior:%s evidence:%s rows:%#v", row.Product, row.Area, row.GladeShape, row.GladeBehavior, row.Evidence, rows)
	}
}

func TestBuildEvidenceSnapshotMarksToolingObjectEvidenceAsType(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.json")
	data := `{
  "name": "tooling-source-metadata-server-evidence",
  "evidence": [{
    "symbol": "Tooling ApexClass",
    "surfaceId": "tooling:ApexClass",
    "kind": "server"
  }],
  "serverRequests": [{"name": "query", "method": "GET", "path": "/services/data/v65.0/tooling/query", "status": 200}],
  "command": {"kind": "server"},
  "expected": {"result": {"ok": true}}
}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err := BuildEvidenceSnapshot([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	row := rowsByID(rows)["tooling:ApexClass"]
	if row.Product != ProductTooling || row.Area != AreaServer || row.Kind != KindType || row.GladeBehavior != BehaviorSupported || row.Evidence != EvidenceFixture {
		t.Fatalf("tooling row = product:%s area:%s kind:%s behavior:%s evidence:%s rows:%#v", row.Product, row.Area, row.Kind, row.GladeBehavior, row.Evidence, rows)
	}
}

func TestBuildEvidenceSnapshotMarksSuccessfulApexFixtureEvidenceSupported(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.json")
	data := `{
  "name": "integration-auth-session-current",
  "evidence": [{
    "symbol": "Auth.SessionManagement.getCurrentSession",
    "surfaceId": "apex:Auth.SessionManagement.getCurrentSession",
    "kind": "exec"
  }],
  "source": [{"path": "anonymous.apex", "content": "Auth.SessionManagement.getCurrentSession();"}],
  "command": {"kind": "exec", "args": ["Auth.SessionManagement.getCurrentSession();"]},
  "expected": {"result": {"ok": true}}
}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err := BuildEvidenceSnapshot([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	row := rowsByID(rows)["apex:Auth.SessionManagement.getCurrentSession"]
	if row.Product != ProductApex || row.Area != AreaRuntime || row.GladeBehavior != BehaviorSupported || row.Evidence != EvidenceFixture {
		t.Fatalf("apex fixture row = product:%s area:%s behavior:%s evidence:%s rows:%#v", row.Product, row.Area, row.GladeBehavior, row.Evidence, rows)
	}
}

func TestBuildEvidenceSnapshotMarksApexShapeEvidenceAsShapeOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.json")
	data := `{
  "name": "shape-only",
  "evidence": [{
    "symbol": "System.Address.getDistance(Location,String)",
    "surfaceId": "apex:System.Address.getDistance(Location,String)",
    "kind": "shape"
  }],
  "command": {"kind": "test"},
  "expected": {"result": {"ok": true}}
}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err := BuildEvidenceSnapshot([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	row := rowsByID(rows)["apex:System.Address.getDistance(Location,String)"]
	if row.Product != ProductApex || row.GladeShape == ShapeAbsent || row.GladeBehavior != BehaviorNone || row.Evidence != EvidenceFixture {
		t.Fatalf("shape row = product:%s shape:%s behavior:%s evidence:%s rows:%#v", row.Product, row.GladeShape, row.GladeBehavior, row.Evidence, rows)
	}
}

func TestBuildEvidenceSnapshotExcludesAPI67RemovedSiteHelpers(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "fixtures", "core-runtime-site-local-contracts.json")
	rows, err := BuildEvidenceSnapshot([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	byID := rowsByID(rows)
	for _, id := range []string{
		"apex:System.Site.getCurrentSiteUrl",
		"apex:System.Site.getCustomWebAddress",
		"apex:System.Site.getPrefix",
	} {
		if _, present := byID[id]; present {
			t.Errorf("removed Site helper entered positive evidence snapshot: %s", id)
		}
		if _, present := byID[id+"()"]; present {
			t.Errorf("removed Site helper entered positive evidence snapshot: %s()", id)
		}
	}
}

func TestBuildEvidenceSnapshotReadsTrailblazerIdentityFixture(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "fixtures", "core-runtime-trailblazer-identity-local-evidence.json")
	rows, err := BuildEvidenceSnapshot([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	byID := rowsByID(rows)
	for _, id := range []string{
		"apex:System.TrailblazerIdentity.generateUserEmailVerificationToken(String,String,String)",
		"apex:System.TrailblazerIdentity.getUserOrgInfo(List<String>)",
		"apex:System.TrailblazerIdentity.splunkLog(String,String)",
	} {
		row := byID[id]
		if row.GladeBehavior != BehaviorSupported || row.Evidence != EvidenceFixture {
			t.Fatalf("%s evidence/behavior = %s/%s, want fixture/supported", id, row.Evidence, row.GladeBehavior)
		}
	}
}

func TestBuildEvidenceSnapshotReadsUIShapePacketFixtures(t *testing.T) {
	unsupportedPath := filepath.Join("..", "..", "docs", "fixtures", "ui-front-end-hosted-unsupported-surfaces.json")
	controllerPath := filepath.Join("..", "..", "docs", "fixtures", "ui-apexpages-controller-local-evidence.json")
	rows, err := BuildEvidenceSnapshot([]string{unsupportedPath, controllerPath})
	if err != nil {
		t.Fatal(err)
	}
	byID := rowsByID(rows)
	for _, id := range []string{
		"ui-api:ui_api_resources_record_get",
		"ui-api:ui_api_responses_platform_action.lwcComponent",
		"site-references:commerce/salesforce-commerce/comm-cart-ref",
		"lightning:ref_jsapi_AuraLocalizationService_formatDate",
	} {
		row, ok := byID[id]
		if !ok {
			t.Fatalf("missing hosted UI packet row %s", id)
		}
		if row.GladeBehavior != BehaviorUnsupported || row.Evidence != EvidenceFixture {
			t.Fatalf("%s behavior/evidence = %s/%s, want unsupported/fixture", id, row.GladeBehavior, row.Evidence)
		}
	}
	for _, id := range []string{
		ApexMemberID("ApexPages", "StandardController", "getId", []string{}),
		ApexMemberID("ApexPages", "StandardSetController", "setSelected", []string{"List<Object>"}),
	} {
		row, ok := byID[id]
		if !ok {
			t.Fatalf("missing local ApexPages packet row %s", id)
		}
		if row.Product != ProductApex || row.GladeBehavior != BehaviorSupported || row.Evidence != EvidenceFixture {
			t.Fatalf("%s product/behavior/evidence = %s/%s/%s, want apex/supported/fixture", id, row.Product, row.GladeBehavior, row.Evidence)
		}
	}
}

func TestBuildEvidenceSnapshotReadsMiscSourceFamilyEvidence(t *testing.T) {
	fixtures := []string{
		"sourcefamily-cli-reference-unsupported.json",
		"sourcefamily-analytics-cli-reference-unsupported.json",
		"sourcefamily-commerce-cli-reference-unsupported.json",
		"sourcefamily-service-connector-api-reference-unsupported.json",
		"sourcefamily-limits-reference-evidence.json",
		"sourcefamily-reference-coverage-unsupported.json",
		"integration-pubsub-current-manifest-unsupported.json",
		"integration-salesforce-connect-amazon-rds-current-manifest-unsupported.json",
		"ai-agentforce-current-manifest-unsupported.json",
		"external-marketing-cloud-ampscript-current-manifest-unsupported.json",
		"external-marketing-cloud-handlebars-current-manifest-unsupported.json",
		"platform-events-current-manifest-evidence.json",
	}
	var paths []string
	for _, fixture := range fixtures {
		path := filepath.Join("..", "..", "docs", "fixtures", fixture)
		assertFixtureSurfaceIDsHaveNoHiddenFormatMarks(t, path)
		paths = append(paths, path)
	}
	rows, err := BuildEvidenceSnapshot(paths)
	if err != nil {
		t.Fatal(err)
	}
	byID := rowsByID(rows)
	for _, id := range []string{
		"cli-reference:cli_reference_apex_commands_unified",
		"analytics-cli-reference:bi_cli_reference_analytics_dashboard",
		"commerce-cli-reference:comm_cli_reference_commerce_store",
		"service-connector-api-reference:service_connector_interface_getactivecalls",
		"unknown:REFERENCE_COVERAGE",
		"platform-events:platform_events_publish_pubsub_api",
		"site-references:platform/pub-sub-api/index",
		"streaming-api:pubsub_api_streaming_api_comparison",
		"site-references:platform/sf-connect-amazon-rds/index",
		"site-references:ai/agentforce/agent-api",
		"site-references:marketing/marketing-cloud-ampscript/mc-ampscript-api/mc-ampscript-reference-api",
		"site-references:marketing/handlebars-for-marketing-cloud-next/mcn-handlebars-string-references/mcn-handlebars-reference-string",
	} {
		row := byID[id]
		if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorUnsupported {
			t.Fatalf("%s evidence/behavior = %s/%s, want fixture/unsupported", id, row.Evidence, row.GladeBehavior)
		}
	}
	for _, id := range []string{
		"platform-events:platform_events_publish",
		"platform-events:platform_events_subscribe_apex",
		"platform-events:platform_events_trigger_reco",
	} {
		row := byID[id]
		if row.Evidence != EvidenceFixture || row.GladeBehavior == BehaviorUnsupported {
			t.Fatalf("%s evidence/behavior = %s/%s, want fixture/non-unsupported", id, row.Evidence, row.GladeBehavior)
		}
	}
	platformLocal := byID["platform-events:platform_events_test_deliver"]
	if platformLocal.Evidence != EvidenceFixture || platformLocal.GladeBehavior != BehaviorNone {
		t.Fatalf("platform local source row evidence/behavior = %s/%s, want fixture/none", platformLocal.Evidence, platformLocal.GladeBehavior)
	}
	limitsLocal := byID["unknown:salesforce_app_limits_platform_apexgov"]
	if limitsLocal.Evidence != EvidenceFixture || limitsLocal.GladeBehavior != BehaviorNone {
		t.Fatalf("limits local source row evidence/behavior = %s/%s, want fixture/none", limitsLocal.Evidence, limitsLocal.GladeBehavior)
	}
}

func TestBuildEvidenceSnapshotReadsMiscLocalRuntimeEvidence(t *testing.T) {
	paths := []string{
		filepath.Join("..", "..", "docs", "fixtures", "core-runtime-context-industriescontext-local-evidence.json"),
		filepath.Join("..", "..", "docs", "fixtures", "query-runtime-soqlsosl-healthcloudext-soslsearch-local-evidence.json"),
	}
	for _, path := range paths {
		assertFixtureSurfaceIDsHaveNoHiddenFormatMarks(t, path)
	}
	rows, err := BuildEvidenceSnapshot(paths)
	if err != nil {
		t.Fatal(err)
	}
	byID := rowsByID(rows)
	for _, id := range []string{
		"apex:Context.IndustriesContext.addRecordsToContext(Map<String,Object>)",
		"apex:Context.IndustriesContext.buildContext(Map<String,Object>)",
		"apex:Context.IndustriesContext.filteringContext(Map<String,Object>)",
		"apex:Context.IndustriesContext.getContext(Map<String,Object>)",
		"apex:Context.IndustriesContext.getContextTranslation(Map<String,Object>)",
		"apex:Context.IndustriesContext.leanerQueryTags(Map<String,Object>)",
		"apex:Context.IndustriesContext.persistContext(Map<String,Object>)",
		"apex:Context.IndustriesContext.queryContextRecordsAndChildren(Map<String,Object>)",
		"apex:Context.IndustriesContext.queryRecordStatus(Map<String,Object>)",
		"apex:Context.IndustriesContext.queryTags(Map<String,Object>)",
		"apex:Context.IndustriesContext.updateContextAttributes(Map<String,Object>)",
		"apex:healthcloudext.IntegratedCareManagementApexHelper.getSOSLSearch(String,String,String,String)",
	} {
		row := byID[id]
		if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported {
			t.Fatalf("%s evidence/behavior = %s/%s, want fixture/supported", id, row.Evidence, row.GladeBehavior)
		}
	}
}

func TestBuildEvidenceSnapshotReadsApexTailShapeEvidence(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "fixtures", "apex-tail-supported-shape-evidence.json")
	assertFixtureSurfaceIDsHaveNoHiddenFormatMarks(t, path)
	rows, err := BuildEvidenceSnapshot([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	byID := rowsByID(rows)
	for _, id := range []string{
		"apex:ApexPages.KnowledgeArticleVersionStandardController.setDataCategory()",
		"apex:Approval.process(List<Approval.ProcessRequest>)",
		"apex:Approval.process(List<Approval.ProcessRequest>,Boolean)",
		"apex:DataSource.AsyncDeleteCallback.processDelete()",
		"apex:DataSource.AsyncSaveCallback.processSave()",
		"apex:Messaging.SingleEmailMessage.setDocumentAttachments()",
		"apex:Schema.DescribeFieldResult",
		"apex:System.Address.getDistance(Location,String)",
		"apex:System.HttpRequest client certificate local mock metadata",
		"apex:System.Location.getDistance()",
		"apex:System.Location.newInstance()",
		"apex:System.QuickAction.describeAvailableActions",
		"apex:System.QuickAction.describeAvailableQuickActions()",
		"apex:System.QuickAction.describeQuickActions()",
		"apex:System.QuickAction.performQuickAction()",
		"apex:System.QuickAction.performQuickActions()",
		"apex:System.SObject.addError(Exception)",
		"apex:System.Set.addAll(Set<Object>)",
		"apex:System.Set.containsAll(Set<Object>)",
		"apex:System.Set.removeAll(Set<Object>)",
	} {
		row, ok := byID[id]
		if !ok {
			t.Fatalf("missing Apex tail shape evidence row %s", id)
		}
		if row.Product != ProductApex || row.GladeShape == ShapeAbsent || row.GladeBehavior != BehaviorNone || row.Evidence != EvidenceFixture {
			t.Fatalf("%s product/shape/behavior/evidence = %s/%s/%s/%s, want apex/non-absent/none/fixture", id, row.Product, row.GladeShape, row.GladeBehavior, row.Evidence)
		}
	}
}

func assertFixtureSurfaceIDsHaveNoHiddenFormatMarks(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw struct {
		Evidence []struct {
			SurfaceID string `json:"surfaceId"`
		} `json:"evidence"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	for _, evidence := range raw.Evidence {
		if strings.ContainsAny(evidence.SurfaceID, "\u200b\u200c\u200d\ufeff") {
			t.Fatalf("%s contains hidden format mark in surface id %q", path, evidence.SurfaceID)
		}
	}
}

func TestBuildEvidenceSnapshotDoesNotMarkUnsupportedRuntimeGuideSupported(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.json")
	data := `{
  "name": "unsupported_soql_guide",
  "evidence": [{"symbol": "SOQL guide", "surfaceId": "unknown:unsupported_soql_statements", "kind": "test"}],
  "command": {"kind": "test"},
  "expected": {"error": {"type": "UnsupportedFeature"}}
}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err := BuildEvidenceSnapshot([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	row := rowsByID(rows)["unknown:unsupported_soql_statements"]
	if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorNone {
		t.Fatalf("unsupported guide row evidence/behavior = %s/%s, want fixture/none: %#v", row.Evidence, row.GladeBehavior, rows)
	}
}

func TestBuildEvidenceSnapshotMarksUnsupportedEvidenceUnsupported(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.json")
	data := `{
  "name": "service_bound_unsupported",
  "evidence": [{"symbol": "WebStoreContext.getCommerceContext", "surfaceId": "apex:System.WebStoreContext.getCommerceContext()", "kind": "unsupported"}],
  "command": {"kind": "exec"},
  "expected": {"error": {"type": "UnsupportedFeature"}}
}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err := BuildEvidenceSnapshot([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	id := ApexMemberID("System", "WebStoreContext", "getCommerceContext", []string{})
	row := rowsByID(rows)[id]
	if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorUnsupported {
		t.Fatalf("unsupported evidence/behavior = %s/%s, want fixture/unsupported: %#v", row.Evidence, row.GladeBehavior, rows)
	}
}

func TestBuildEvidenceSnapshotMarksUnsupportedFeatureErrorUnsupported(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.json")
	data := `{
  "name": "integration-auth-jwt-unsupported",
  "evidence": [{
    "symbol": "Auth.JWTUtil.validateJWTWithKeysEndpoint",
    "surfaceId": "apex:Auth.JWTUtil.validateJWTWithKeysEndpoint",
    "kind": "exec"
  }],
  "command": {"kind": "exec"},
  "expected": {"error": {"type": "UnsupportedFeature"}}
}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err := BuildEvidenceSnapshot([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	row := rowsByID(rows)["apex:Auth.JWTUtil.validateJWTWithKeysEndpoint"]
	if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorUnsupported {
		t.Fatalf("unsupported feature row evidence/behavior = %s/%s, want fixture/unsupported: %#v", row.Evidence, row.GladeBehavior, rows)
	}
}

func TestBuildEvidenceSnapshotParsesQualifiedParameterSurfaceID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.json")
	data := `{
  "name": "async-test-harness-local-evidence",
  "evidence": [{
    "surfaceId": "apex:System.Test.testNotificationActionHandler(Messaging.NotificationActionHandler,Messaging.ActionableNotification)",
    "symbol": "Test.testNotificationActionHandler",
    "kind": "test"
  }],
  "command": {"kind": "test"},
  "expected": {"result": {"ok": true}}
}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err := BuildEvidenceSnapshot([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	row := rowsByID(rows)["apex:System.Test.testNotificationActionHandler(Messaging.NotificationActionHandler,Messaging.ActionableNotification)"]
	if row.Namespace != "System" || row.TypeName != "Test" || row.MemberName != "testNotificationActionHandler" {
		t.Fatalf("identity = namespace:%q type:%q member:%q, want System/Test/testNotificationActionHandler: %#v", row.Namespace, row.TypeName, row.MemberName, rows)
	}
	want := []string{"Messaging.NotificationActionHandler", "Messaging.ActionableNotification"}
	if len(row.Parameters) != len(want) {
		t.Fatalf("parameters = %#v, want %#v", row.Parameters, want)
	}
	for i := range want {
		if row.Parameters[i] != want[i] {
			t.Fatalf("parameters = %#v, want %#v", row.Parameters, want)
		}
	}
}

func TestBuildEvidenceSnapshotClassifiesDataReferenceEvidenceAsShape(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.json")
	data := `{
  "name": "data-reference-fixture",
  "evidence": [{
    "surfaceId": "data-reference:NU__Thing__c",
    "symbol": "NU__Thing__c",
    "kind": "check"
  }],
  "schema": [{
    "path": "force-app/main/default/objects/NU__Thing__c/NU__Thing__c.object-meta.xml",
    "content": "<CustomObject><label>Thing</label></CustomObject>"
  }],
  "source": [{
    "path": "force-app/main/default/classes/ManagedRef.cls",
    "content": "public class ManagedRef { public NU__Thing__c makeThing() { return new NU__Thing__c(); } }"
  }],
  "command": {"kind": "check"},
  "expected": {"result": {"ok": true}}
}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	rows, err := BuildEvidenceSnapshot([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	byID := rowsByID(rows)
	row, ok := byID[DataObjectID("NU__Thing__c")]
	if !ok {
		t.Fatalf("missing data-reference evidence row: %#v", rows)
	}
	if row.Product != ProductDataRef || row.Area != AreaData || row.Kind != KindType {
		t.Fatalf("row identity = product:%s area:%s kind:%s", row.Product, row.Area, row.Kind)
	}
	if row.GladeShape == ShapeAbsent || row.GladeBehavior != BehaviorSupported || row.Evidence != EvidenceFixture {
		t.Fatalf("row states = shape:%s behavior:%s evidence:%s", row.GladeShape, row.GladeBehavior, row.Evidence)
	}
}

func TestBuildEvidenceSnapshotClassifiesBareMethodEvidenceIDs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.json")
	data := `{
  "name": "bare_method_ids",
  "evidence": [
    {"symbol": "RestRequest.getHeader", "surfaceId": "apex:System.RestRequest.getHeader", "kind": "exec"},
    {"symbol": "Limits.getAsyncCalls", "surfaceId": "apex:System.Limits.getAsyncCalls", "kind": "exec"},
    {"symbol": "Test.isRunningTest", "surfaceId": "apex:System.Test.isRunningTest", "kind": "test"},
    {"symbol": "Test.createStubQueryRows", "surfaceId": "apex:System.Test.createStubQueryRows", "kind": "test"},
    {"symbol": "Type.getName", "surfaceId": "apex:System.Type.getName", "kind": "exec"},
    {"symbol": "RestResponse.statusCode", "surfaceId": "apex:System.RestResponse.statusCode", "kind": "exec"}
  ],
  "command": {"kind": "exec"},
  "expected": {"stdout": ""}
}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err := BuildEvidenceSnapshot([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	byID := rowsByID(rows)
	for _, id := range []string{"apex:System.RestRequest.getHeader", "apex:System.Limits.getAsyncCalls", "apex:System.Test.isRunningTest", "apex:System.Test.createStubQueryRows", "apex:System.Type.getName"} {
		if row := byID[id]; row.Kind != KindMethod {
			t.Fatalf("%s kind = %s, want method: %#v", id, row.Kind, rows)
		}
	}
	if row := byID["apex:System.RestResponse.statusCode"]; row.Kind != KindProperty {
		t.Fatalf("RestResponse.statusCode kind = %s, want property: %#v", row.Kind, rows)
	}
}

func TestBuildEvidenceSnapshotSkipsCorpusManifest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "local-tests-corpus.json")
	if err := os.WriteFile(path, []byte(`{"target":"local Apex test execution corpus","project":"example","projects":[{"project":"example","summary":{"total":1}}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err := BuildEvidenceSnapshot([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("rows = %d, want 0", len(rows))
	}
}

func TestBuildEvidenceSnapshotInfersKnownNamespaceType(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.json")
	data := `{
  "name": "schema_record_type_info",
  "evidence": [{"symbol": "Schema.RecordTypeInfo"}],
  "command": {"kind": "test"},
  "expected": {"result": {"ok": true}}
}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err := BuildEvidenceSnapshot([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if rowsByID(rows)[ApexTypeID("Schema", "RecordTypeInfo")].Evidence != EvidenceFixture {
		t.Fatalf("Schema.RecordTypeInfo did not infer known namespace type: %#v", rows)
	}
}

func TestBuildEvidenceSnapshotSkipsDatabaseFamilyLabelsWithoutSurfaceID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.json")
	data := `{
  "name": "database_family",
  "evidence": [{"symbol": "Database.query"}],
  "command": {"kind": "test"},
  "expected": {"result": {"ok": true}}
}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err := BuildEvidenceSnapshot([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("Database.query family label inferred fake surface rows: %#v", rows)
	}
}

func TestBuildEvidenceSnapshotSkipsKnownNamespaceLowercaseMemberLabelsWithoutSurfaceID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.json")
	data := `{
  "name": "system_runtime_label",
  "evidence": [{"symbol": "System.isFuture"}],
  "command": {"kind": "exec"},
  "expected": {"stdout": ""}
}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err := BuildEvidenceSnapshot([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("System.isFuture label inferred fake type rows: %#v", rows)
	}
}

func TestBuildEvidenceSnapshotSkipsHumanBehaviorLabelsWithoutSurfaceID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.json")
	data := `{
  "name": "soql_behavior_packet",
  "evidence": [{"symbol": "SOQL aggregate GROUP BY/HAVING"}],
  "command": {"kind": "test"},
  "expected": {"result": {"ok": true}}
}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err := BuildEvidenceSnapshot([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("human behavior label inferred fake surface rows: %#v", rows)
	}
}

func TestBuildEvidenceSnapshotInfersKnownZeroArgMethods(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.json")
	data := `{
  "name": "schema_describe_methods",
  "evidence": [{"symbol": "Schema.DescribeFieldResult.getLabel"}],
  "command": {"kind": "test"},
  "expected": {"result": {"ok": true}}
}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err := BuildEvidenceSnapshot([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	id := ApexMemberID("Schema", "DescribeFieldResult", "getLabel", []string{})
	if rowsByID(rows)[id].Evidence != EvidenceFixture {
		t.Fatalf("Schema.DescribeFieldResult.getLabel did not infer zero-arg method: %#v", rows)
	}
}

func TestBuildEvidenceSnapshotTreatsNoParenDescribeMemberAsProperty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.json")
	data := `{
  "name": "schema_describe_tab_properties",
  "evidence": [{"symbol": "Schema.DescribeTabSetResult.name", "surfaceId": "apex:Schema.DescribeTabSetResult.name"}],
  "command": {"kind": "test"},
  "expected": {"result": {"ok": true}}
}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err := BuildEvidenceSnapshot([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	id := ApexMemberID("Schema", "DescribeTabSetResult", "name", nil)
	row := rowsByID(rows)[id]
	if row.Kind != KindProperty || row.Namespace != "Schema" || row.TypeName != "DescribeTabSetResult" || row.MemberName != "name" {
		t.Fatalf("property row = kind:%s namespace:%s type:%s member:%s rows:%#v", row.Kind, row.Namespace, row.TypeName, row.MemberName, rows)
	}
}

func TestBuildEvidenceSnapshotReadsApexPagesSeverityLocalEvidence(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "fixtures", "apex-apexpages-severity-local-evidence.json")
	rows, err := BuildEvidenceSnapshot([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	byID := rowsByID(rows)
	for _, id := range []string{
		"apex:ApexPages.Severity.ERROR",
		"apex:ApexPages.Severity.INFO",
		"apex:ApexPages.Severity.WARNING",
		"apex:ApexPages.Severity.CONFIRM",
		"apex:ApexPages.Severity.FATAL",
	} {
		row := byID[id]
		if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported {
			t.Fatalf("%s evidence/behavior = %s/%s, want fixture/supported", id, row.Evidence, row.GladeBehavior)
		}
	}
}

func TestBuildEvidenceSnapshotReadsAnswersFindSimilarLocalEvidence(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "fixtures", "core-runtime-answers-find-similar-local-evidence.json")
	rows, err := BuildEvidenceSnapshot([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	byID := rowsByID(rows)

	exactRow := byID["apex:Answers.findSimilar(Object)"]
	if exactRow.Evidence != EvidenceFixture || exactRow.GladeBehavior != BehaviorSupported {
		t.Fatalf("exact Answers.findSimilar(Object) evidence/behavior = %s/%s, want fixture/supported", exactRow.Evidence, exactRow.GladeBehavior)
	}

	// Raw evidence has two family rows: one shape-only, one behavior-supported.
	familyID := "apex:Answers.findSimilar"
	var familyShapeRow, familyTestRow *SurfaceLedgerRow
	for i := range rows {
		if rows[i].SurfaceID == familyID {
			if rows[i].GladeBehavior == BehaviorNone {
				familyShapeRow = &rows[i]
			} else if rows[i].GladeBehavior == BehaviorSupported {
				familyTestRow = &rows[i]
			}
		}
	}
	if familyShapeRow == nil {
		t.Fatalf("missing shape-only family row for %s", familyID)
	}
	if familyShapeRow.GladeShape == ShapeAbsent || familyShapeRow.GladeBehavior != BehaviorNone || familyShapeRow.Evidence != EvidenceFixture {
		t.Fatalf("shape-only family row shape/behavior/evidence = %s/%s/%s, want non-absent/none/fixture", familyShapeRow.GladeShape, familyShapeRow.GladeBehavior, familyShapeRow.Evidence)
	}
	if familyTestRow == nil {
		t.Fatalf("missing behavior-supported family row for %s", familyID)
	}
	if familyTestRow.GladeBehavior != BehaviorSupported || familyTestRow.Evidence != EvidenceFixture {
		t.Fatalf("behavior family row behavior/evidence = %s/%s, want supported/fixture", familyTestRow.GladeBehavior, familyTestRow.Evidence)
	}

	// After merge, the single family row closes the gap.
	ledger := Merge(nil, nil, nil, rows)
	mergedByID := rowsByID(ledger.Rows)
	familyMerged := mergedByID[familyID]
	if familyMerged.GladeShape == ShapeAbsent {
		t.Fatalf("merged family row shape is absent, want non-absent")
	}
	if familyMerged.GladeBehavior != BehaviorSupported {
		t.Fatalf("merged family row behavior = %s, want %s", familyMerged.GladeBehavior, BehaviorSupported)
	}
	if familyMerged.Evidence != EvidenceFixture {
		t.Fatalf("merged family row evidence = %s, want %s", familyMerged.Evidence, EvidenceFixture)
	}
	if familyMerged.Bucket != BucketImplemented {
		t.Fatalf("merged family row bucket = %s, want %s", familyMerged.Bucket, BucketImplemented)
	}
	if familyMerged.GapClass != "" {
		t.Fatalf("merged family row gap = %s, want empty", familyMerged.GapClass)
	}
}
