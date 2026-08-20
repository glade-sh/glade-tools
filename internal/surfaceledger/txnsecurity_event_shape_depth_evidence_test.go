package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

const txnSecurityEventShapeDepthFixture = "core-runtime-txnsecurity-event-shape-depth"

var txnSecurityEventShapeDepthIDs = []string{
	"apex:TxnSecurity.Event",
	"apex:TxnSecurity.Event.Event(String,String,String,String,String,String,Datetime,Map<String,String>)",
	"apex:TxnSecurity.Event.action",
	"apex:TxnSecurity.Event.clone()",
	"apex:TxnSecurity.Event.data",
	"apex:TxnSecurity.Event.entityId",
	"apex:TxnSecurity.Event.entityName",
	"apex:TxnSecurity.Event.organizationId",
	"apex:TxnSecurity.Event.resourceType",
	"apex:TxnSecurity.Event.timeStamp",
	"apex:TxnSecurity.Event.userId",
	"apex:TxnSecurity.EventCondition",
	"apex:TxnSecurity.PolicyCondition",
}

func TestTxnSecurityEventShapeDepthHasExactLocalFixtureOwnership(t *testing.T) {
	root := filepath.Join("..", "..")
	fixturePath := filepath.Join(root, "docs", "fixtures", txnSecurityEventShapeDepthFixture+".json")
	fixture, err := compat.LoadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Name != txnSecurityEventShapeDepthFixture || fixture.Command.Kind != "check" || len(fixture.Command.Args) != 0 || len(fixture.Source) != 1 {
		t.Fatalf("fixture envelope = %#v", fixture)
	}
	if len(fixture.Evidence) != len(txnSecurityEventShapeDepthIDs) {
		t.Fatalf("raw evidence rows = %d, want %d", len(fixture.Evidence), len(txnSecurityEventShapeDepthIDs))
	}
	want := make(map[string]bool, len(txnSecurityEventShapeDepthIDs))
	for _, id := range txnSecurityEventShapeDepthIDs {
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
	assertExactSurfaceSet(t, evidence, txnSecurityEventShapeDepthIDs)
	for _, row := range evidence {
		if row.Evidence != EvidenceFixture || row.GladeShape != ShapeTypeKnown || row.GladeBehavior != BehaviorNone || len(row.Sources) != 1 || row.Sources[0] != "fixture:"+txnSecurityEventShapeDepthFixture {
			t.Fatalf("%s evidence/shape/behavior/source = %s/%s/%s/%v", row.SurfaceID, row.Evidence, row.GladeShape, row.GladeBehavior, row.Sources)
		}
	}

	source := fixture.Source[0].Content
	for _, witness := range []string{
		"new TxnSecurity.Event('org', 'user', 'entity', 'action', 'resource', 'record', Datetime.now(), new Map<String,String>())",
		"eventValue.action;",
		"eventValue.data;",
		"eventValue.entityId;",
		"eventValue.entityName;",
		"eventValue.organizationId;",
		"eventValue.resourceType;",
		"eventValue.timeStamp;",
		"eventValue.userId;",
		"eventValue.clone();",
		"TxnSecurity.EventCondition eventCondition;",
		"TxnSecurity.PolicyCondition policyCondition;",
	} {
		if !strings.Contains(source, witness) {
			t.Fatalf("source missing direct shape witness %q", witness)
		}
	}
	if strings.Contains(source, ".evaluate(") {
		t.Fatal("compile-shape fixture must not claim hosted transaction-policy evaluation")
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
	for _, id := range txnSecurityEventShapeDepthIDs {
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
	if policy.SalesforceEligible == nil || *policy.SalesforceEligible || policy.SalesforceExclusionClass != "policy-local-only" || !strings.Contains(policy.SalesforceExclusionReason, "zero Salesforce parity") || !strings.Contains(policy.SalesforceExclusionReason, "policy evaluation") {
		t.Fatalf("fixture policy = %#v", policy)
	}
	if result, err := compat.Run(fixture); err != nil || !result.OK {
		t.Fatalf("fixture check = %#v, error = %v", result, err)
	}
}
