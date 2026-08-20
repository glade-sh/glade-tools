package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

const invocableCompileShapeDepthFixture = "core-runtime-invocable-compile-shape-depth"

var invocableCompileShapeDepthIDs = []string{
	"apex:Invocable.Action",
	"apex:Invocable.Action.AdditionalAttribute.clone()",
	"apex:Invocable.Action.DescribeResult",
	"apex:Invocable.Action.DescribeResult.clone()",
	"apex:Invocable.Action.Error",
	"apex:Invocable.Action.GenericType",
	"apex:Invocable.Action.GenericType.clone()",
	"apex:Invocable.Action.InputParameter",
	"apex:Invocable.Action.InputParameter.clone()",
	"apex:Invocable.Action.OutputParameter",
	"apex:Invocable.Action.OutputParameter.clone()",
	"apex:Invocable.Action.PicklistValue",
	"apex:Invocable.Action.PicklistValue.clone()",
	"apex:Invocable.Action.Result",
	"apex:Invocable.Action.clone()",
	"apex:Invocable.ActionInvoker",
	"apex:Invocable.ActionInvoker.ActionInvoker()",
	"apex:Invocable.ActionInvoker.Result",
	"apex:Invocable.ActionInvoker.clone()",
	"apex:Invocable.ActionInvoker.invokeCustomAction(String,String,String,Map<String,Object>,Double)",
	"apex:Invocable.ConsentRequestInput",
	"apex:Invocable.ConsentRequestInput.ConsentRequestInput()",
	"apex:Invocable.ConsentRequestInput.clone()",
	"apex:Invocable.ConsentRequestInput.recordList",
	"apex:Invocable.ConsentStatusRecord",
	"apex:Invocable.ConsentStatusRecord.ConsentStatusRecord()",
	"apex:Invocable.ConsentStatusRecord.assetName",
	"apex:Invocable.ConsentStatusRecord.clone()",
	"apex:Invocable.ConsentStatusRecord.commSubscriptionChannelTypeId",
	"apex:Invocable.ConsentStatusRecord.commSubscriptionId",
	"apex:Invocable.ConsentStatusRecord.consentStatus",
	"apex:Invocable.ConsentStatusRecord.contactPointValue",
	"apex:Invocable.ConsentStatusRecord.engagementChannelTypeId",
	"apex:Invocable.ConsentStatusRecord.flowDataSpaceApiName",
	"apex:Invocable.ConsentStatusRecord.flowName",
	"apex:Invocable.ConsentStatusRecord.flowTriggerType",
	"apex:Invocable.ConsentStatusRecord.senderCode",
	"apex:Invocable.ResourceAnnotationMap",
	"apex:Invocable.ResourceAnnotationMap.ResourceAnnotationMap()",
	"apex:Invocable.ResourceAnnotationMap.clone()",
	"apex:Invocable.ResourceAnnotationMap.collection",
	"apex:Invocable.ResourceAnnotationMap.name",
	"apex:Invocable.ResourceAnnotationMap.resourceField",
	"apex:Invocable.ResourceAnnotationMap.resourceName",
	"apex:Invocable.ResourceAnnotationMap.resourceType",
	"apex:Invocable.ResourceDescriptor",
	"apex:Invocable.ResourceDescriptor.ResourceDescriptor()",
	"apex:Invocable.ResourceDescriptor.clone()",
	"apex:Invocable.ResourceDescriptor.resolvedOutput",
	"apex:Invocable.ResourceDescriptor.resourceAnnotationMap",
	"apex:Invocable.ResourceDescriptor.resourceTemplate",
}

func TestInvocableCompileShapeDepthHasExactLocalFixtureOwnership(t *testing.T) {
	root := filepath.Join("..", "..")
	fixturePath := filepath.Join(root, "docs", "fixtures", invocableCompileShapeDepthFixture+".json")
	fixture, err := compat.LoadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Name != invocableCompileShapeDepthFixture || fixture.Command.Kind != "check" || len(fixture.Command.Args) != 0 || len(fixture.Source) != 1 {
		t.Fatalf("fixture envelope = %#v", fixture)
	}
	if len(fixture.Evidence) != len(invocableCompileShapeDepthIDs) {
		t.Fatalf("raw evidence rows = %d, want %d", len(fixture.Evidence), len(invocableCompileShapeDepthIDs))
	}
	want := make(map[string]bool, len(invocableCompileShapeDepthIDs))
	for _, id := range invocableCompileShapeDepthIDs {
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
	assertExactSurfaceSet(t, evidence, invocableCompileShapeDepthIDs)
	for _, row := range evidence {
		if row.Evidence != EvidenceFixture || row.GladeShape != ShapeTypeKnown || row.GladeBehavior != BehaviorNone || len(row.Sources) != 1 || row.Sources[0] != "fixture:"+invocableCompileShapeDepthFixture {
			t.Fatalf("%s evidence/shape/behavior/source = %s/%s/%s/%v", row.SurfaceID, row.Evidence, row.GladeShape, row.GladeBehavior, row.Sources)
		}
	}

	source := fixture.Source[0].Content
	for _, witness := range []string{
		"Invocable.Action action = null;", "action.clone();",
		"Invocable.Action.AdditionalAttribute additionalAttribute = null;", "additionalAttribute.clone();",
		"Invocable.Action.DescribeResult describeResult = null;", "describeResult.clone();",
		"Invocable.Action.Error actionError = null;",
		"Invocable.Action.GenericType genericType = null;", "genericType.clone();",
		"Invocable.Action.InputParameter inputParameter = null;", "inputParameter.clone();",
		"Invocable.Action.OutputParameter outputParameter = null;", "outputParameter.clone();",
		"Invocable.Action.PicklistValue picklistValue = null;", "picklistValue.clone();",
		"Invocable.Action.Result actionResult = null;",
		"new Invocable.ActionInvoker();", "actionInvoker.clone();", "Invocable.ActionInvoker.invokeCustomAction('type', 'namespace', 'name', new Map<String,Object>(), 67.0);", "Invocable.ActionInvoker.Result invocationResult",
		"new Invocable.ConsentRequestInput();", "consentRequest.clone();", "consentRequest.recordList;",
		"new Invocable.ConsentStatusRecord();", "consentStatus.clone();", "consentStatus.assetName;", "consentStatus.commSubscriptionChannelTypeId;", "consentStatus.commSubscriptionId;", "consentStatus.consentStatus;", "consentStatus.contactPointValue;", "consentStatus.engagementChannelTypeId;", "consentStatus.flowDataSpaceApiName;", "consentStatus.flowName;", "consentStatus.flowTriggerType;", "consentStatus.senderCode;",
		"new Invocable.ResourceAnnotationMap();", "annotationMap.clone();", "annotationMap.collection;", "annotationMap.name;", "annotationMap.resourceField;", "annotationMap.resourceName;", "annotationMap.resourceType;",
		"new Invocable.ResourceDescriptor();", "descriptor.clone();", "descriptor.resolvedOutput;", "descriptor.resourceAnnotationMap;", "descriptor.resourceTemplate;",
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
	for _, id := range invocableCompileShapeDepthIDs {
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
	if policy.SalesforceEligible == nil || *policy.SalesforceEligible || policy.SalesforceExclusionClass != "policy-local-only" || !strings.Contains(policy.SalesforceExclusionReason, "zero Salesforce parity") || !strings.Contains(policy.SalesforceExclusionReason, "hosted action") {
		t.Fatalf("fixture policy = %#v", policy)
	}
	if result, err := compat.Run(fixture); err != nil || !result.OK {
		t.Fatalf("fixture check = %#v, error = %v", result, err)
	}
}
