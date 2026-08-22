package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

const systemTestEventBusLifecycleTailFixture = "core-runtime-system-test-eventbus-lifecycle-tail-api67.json"

var systemTestEventBusLifecycleTailIDs = []string{
	"apex:System.ExternalServiceTest.sendCallback(HttpRequest)",
	"apex:System.System.attachFinalizer(finalizer)",
	"apex:System.Test.getEventBus()",
	"apex:System.Test.getExternalService()",
	"apex:System.Test.getStandardPricebookId",
	"apex:System.Test.getStandardPricebookId()",
	"apex:System.Test.isRunningTest()",
	"apex:System.Test.isSoqlStubDefined(Schema.SObjectType)",
	"apex:System.Test.setMock(Type,Object)",
	"apex:System.Test.startTest",
	"apex:System.Test.stopTest",
	"apex:System.Test.testNotificationActionHandler(Messaging.NotificationActionHandler,Messaging.ActionableNotification)",
	"apex:System.TestAsyncHttp.executeHttpRequest(HttpRequest)",
}
func TestSystemTestEventBusLifecycleTailHasExactCandidateLocalEvidence(t *testing.T) {
	root := filepath.Join("..", "..")
	path := filepath.Join(root, "docs", "fixtures", systemTestEventBusLifecycleTailFixture)
	fixture, err := compat.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Command.Kind != "test" || len(fixture.Source) != 5 || len(fixture.Evidence) != len(systemTestEventBusLifecycleTailIDs) || fixture.Project.SourceAPIVersion != "67.0" {
		t.Fatalf("fixture envelope = %#v", fixture)
	}
	want := mapFromIDs(systemTestEventBusLifecycleTailIDs)
	seen := make(map[string]bool, len(want))
	for _, row := range fixture.Evidence {
		if row.Kind != "test" || !want[row.SurfaceID] || seen[row.SurfaceID] {
			t.Fatalf("unexpected or duplicate evidence row = %#v", row)
		}
		seen[row.SurfaceID] = true
	}
	if len(seen) != len(want) {
		t.Fatalf("evidence ownership = %d, want %d", len(seen), len(want))
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var metadata struct {
		APIVersion string `json:"apiVersion"`
		Mode string `json:"mode"`
		Notes string `json:"notes"`
		EvidenceOnly bool `json:"evidenceOnly"`
		Eligible *bool `json:"salesforceEligible"`
		ExclusionClass string `json:"salesforceExclusionClass"`
		ExclusionReason string `json:"salesforceExclusionReason"`
		Salesforce any `json:"salesforce"`
		Comparisons any `json:"comparisons"`
		Candidate struct { Commit, SHA256 string } `json:"candidate"`
		Profile struct { CandidateCommit, CandidateSHA256, LaneID string; SelectedRows int `json:"selectedRowCount"` } `json:"profile"`
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.APIVersion != "67.0" || metadata.Mode != "local-runtime" || metadata.EvidenceOnly || metadata.Eligible == nil || *metadata.Eligible || metadata.ExclusionClass != "policy-local-only" || !strings.Contains(strings.ToLower(metadata.ExclusionReason), "zero hosted salesforce parity") || metadata.Candidate.Commit != "3409c4c85827b19712e9df83fc8905aa02bd1dc8" || metadata.Candidate.SHA256 != "960ac9f26fa92aae6054cbe0e59f9c4ab1f84397df67bd8a89528068d02a1fce" || metadata.Profile.CandidateCommit != metadata.Candidate.Commit || metadata.Profile.CandidateSHA256 != metadata.Candidate.SHA256 || metadata.Profile.LaneID == "" || metadata.Profile.SelectedRows != len(want) || metadata.Salesforce != nil || metadata.Comparisons != nil || !strings.Contains(metadata.Notes, "deterministic local") {
		t.Fatalf("fixture provenance = %#v", metadata)
	}
	joinedSource := ""
	for _, source := range fixture.Source {
		if source.Content == "" {
			t.Fatalf("empty source content: %#v", source)
		}
		joinedSource += source.Content
	}
	for _, witness := range []string{
		"Test.setMock(HttpCalloutMock.class, new SystemTestEventBusLifecycleMock())",
		"HttpResponse mockResponse = new Http().send(mockRequest)",
		"System.assertEquals(201, mockResponse.getStatusCode())",
		"System.assertEquals('system-test-eventbus-mock', mockResponse.getBody())",
		"System.assertEquals(0, SystemTestEventBusLifecycleState.delivered)",
		"Test.getEventBus().deliver()",
		"System.assertEquals(1, SystemTestEventBusLifecycleState.delivered)",
	} {
		if !strings.Contains(joinedSource, witness) {
			t.Fatalf("source missing observable lifecycle witness %q", witness)
		}
	}
	if result, err := compat.Run(fixture); err != nil || !result.OK {
		t.Fatalf("fixture execution = %#v, error = %v", result, err)
	}

	evidence, err := BuildEvidenceSnapshot([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(evidence) != len(want) {
		t.Fatalf("snapshot rows = %d, want %d", len(evidence), len(want))
	}
	for _, row := range evidence {
		if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported {
			t.Fatalf("%s snapshot = %#v", row.SurfaceID, row)
		}
	}

	paths, err := filepath.Glob(filepath.Join(root, "docs", "fixtures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	owners := make(map[string]int, len(want))
	for _, candidate := range paths {
		var header struct {
			EvidenceOnly bool `json:"evidenceOnly"`
			Profile json.RawMessage `json:"profile"`
			Evidence []struct { SurfaceID string `json:"surfaceId"` } `json:"evidence"`
		}
		readJSON(t, candidate, &header)
		if header.EvidenceOnly || len(header.Profile) == 0 || header.Profile[0] != '{' {
			continue
		}
		var profile struct { CandidateSHA256 string `json:"candidateSha256"` }
		if err := json.Unmarshal(header.Profile, &profile); err != nil || profile.CandidateSHA256 == "" {
			continue
		}
		for _, row := range header.Evidence {
			if want[row.SurfaceID] {
				owners[row.SurfaceID]++
			}
		}
	}
	for _, id := range systemTestEventBusLifecycleTailIDs {
		if owners[id] != 1 {
			t.Fatalf("exact-candidate fixture ownership for %s = %d, want 1", id, owners[id])
		}
	}
}
