package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

const approvalCompileShapeDepthFixture = "core-runtime-approval-compile-shape-depth"

var approvalCompileShapeDepthIDs = []string{
	"apex:Approval",
	"apex:Approval.LockResult",
	"apex:Approval.LockResult.equals(Object)",
	"apex:Approval.LockResult.errors",
	"apex:Approval.LockResult.hashCode()",
	"apex:Approval.LockResult.id",
	"apex:Approval.LockResult.success",
	"apex:Approval.LockResult.toString()",
	"apex:Approval.ProcessRequest",
	"apex:Approval.ProcessRequest.comments",
	"apex:Approval.ProcessRequest.equals(Object)",
	"apex:Approval.ProcessRequest.hashCode()",
	"apex:Approval.ProcessRequest.nextapproverids",
	"apex:Approval.ProcessRequest.toString()",
	"apex:Approval.ProcessResult",
	"apex:Approval.ProcessResult.actorids",
	"apex:Approval.ProcessResult.entityid",
	"apex:Approval.ProcessResult.equals(Object)",
	"apex:Approval.ProcessResult.errors",
	"apex:Approval.ProcessResult.hashCode()",
	"apex:Approval.ProcessResult.instanceid",
	"apex:Approval.ProcessResult.instancestatus",
	"apex:Approval.ProcessResult.newworkitemids",
	"apex:Approval.ProcessResult.success",
	"apex:Approval.ProcessResult.toString()",
	"apex:Approval.ProcessSubmitRequest",
	"apex:Approval.ProcessSubmitRequest.ProcessSubmitRequest()",
	"apex:Approval.ProcessSubmitRequest.equals(Object)",
	"apex:Approval.ProcessSubmitRequest.hashCode()",
	"apex:Approval.ProcessSubmitRequest.objectid",
	"apex:Approval.ProcessSubmitRequest.processdefinitionnameorid",
	"apex:Approval.ProcessSubmitRequest.skipentrycriteria",
	"apex:Approval.ProcessSubmitRequest.submitterid",
	"apex:Approval.ProcessSubmitRequest.toString()",
	"apex:Approval.ProcessWorkitemRequest",
	"apex:Approval.ProcessWorkitemRequest.ProcessWorkitemRequest()",
	"apex:Approval.ProcessWorkitemRequest.action",
	"apex:Approval.ProcessWorkitemRequest.equals(Object)",
	"apex:Approval.ProcessWorkitemRequest.hashCode()",
	"apex:Approval.ProcessWorkitemRequest.toString()",
	"apex:Approval.ProcessWorkitemRequest.workitemid",
	"apex:Approval.UnlockResult",
	"apex:Approval.UnlockResult.equals(Object)",
	"apex:Approval.UnlockResult.errors",
	"apex:Approval.UnlockResult.hashCode()",
	"apex:Approval.UnlockResult.id",
	"apex:Approval.UnlockResult.success",
	"apex:Approval.UnlockResult.toString()",
}

func TestApprovalCompileShapeDepthHasExactLocalFixtureOwnership(t *testing.T) {
	root := filepath.Join("..", "..")
	fixturePath := filepath.Join(root, "docs", "fixtures", approvalCompileShapeDepthFixture+".json")
	fixture, err := compat.LoadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Name != approvalCompileShapeDepthFixture || fixture.Command.Kind != "check" || len(fixture.Command.Args) != 0 || len(fixture.Source) != 1 {
		t.Fatalf("fixture envelope = %#v", fixture)
	}
	if len(fixture.Evidence) != len(approvalCompileShapeDepthIDs) {
		t.Fatalf("raw evidence rows = %d, want %d", len(fixture.Evidence), len(approvalCompileShapeDepthIDs))
	}
	want := make(map[string]bool, len(approvalCompileShapeDepthIDs))
	for _, id := range approvalCompileShapeDepthIDs {
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
	assertExactSurfaceSet(t, evidence, approvalCompileShapeDepthIDs)
	for _, row := range evidence {
		if row.Evidence != EvidenceFixture || row.GladeShape != ShapeTypeKnown || row.GladeBehavior != BehaviorNone || len(row.Sources) != 1 || row.Sources[0] != "fixture:"+approvalCompileShapeDepthFixture {
			t.Fatalf("%s evidence/shape/behavior/source = %s/%s/%s/%v", row.SurfaceID, row.Evidence, row.GladeShape, row.GladeBehavior, row.Sources)
		}
	}

	source := fixture.Source[0].Content
	for _, witness := range []string{
		"Approval.LockResult lockResult = null;", "lockResult.errors;", "lockResult.id;", "lockResult.success;", "lockResult.equals(lockResult);", "lockResult.hashCode();", "lockResult.toString();",
		"Approval.ProcessRequest request = null;", "request.comments;", "request.nextApproverIds;", "request.equals(request);", "request.hashCode();", "request.toString();",
		"Approval.ProcessResult result = null;", "result.actorIds;", "result.entityId;", "result.errors;", "result.instanceId;", "result.instanceStatus;", "result.newWorkitemIds;", "result.success;", "result.equals(result);", "result.hashCode();", "result.toString();",
		"new Approval.ProcessSubmitRequest();", "submit.objectId;", "submit.processDefinitionNameOrId;", "submit.skipEntryCriteria;", "submit.submitterId;", "submit.equals(submit);", "submit.hashCode();", "submit.toString();",
		"new Approval.ProcessWorkitemRequest();", "work.action;", "work.workitemId;", "work.equals(work);", "work.hashCode();", "work.toString();",
		"Approval.UnlockResult unlockResult = null;", "unlockResult.errors;", "unlockResult.id;", "unlockResult.success;", "unlockResult.equals(unlockResult);", "unlockResult.hashCode();", "unlockResult.toString();",
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
	for _, id := range approvalCompileShapeDepthIDs {
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
	if policy.SalesforceEligible == nil || *policy.SalesforceEligible || policy.SalesforceExclusionClass != "policy-local-only" || !strings.Contains(policy.SalesforceExclusionReason, "zero Salesforce parity") || !strings.Contains(policy.SalesforceExclusionReason, "workflow") {
		t.Fatalf("fixture policy = %#v", policy)
	}
	if result, err := compat.Run(fixture); err != nil || !result.OK {
		t.Fatalf("fixture check = %#v, error = %v", result, err)
	}
}
