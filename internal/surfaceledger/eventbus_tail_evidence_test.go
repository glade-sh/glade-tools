package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

const (
	eventBusCallbackResultFixture = "core-runtime-eventbus-callback-result-tail-api67.json"
	eventBusTestServiceFixture    = "core-runtime-eventbus-test-service-tail-api67.json"
	eventBusTriggerContextFixture = "core-runtime-eventbus-trigger-context-api67.json"
)

var eventBusCallbackResultIDs = []string{
	"apex:eventbus.EventPublishFailureCallback",
	"apex:eventbus.EventPublishSuccessCallback",
	"apex:eventbus.FailureResult",
	"apex:eventbus.FailureResult.FailureResult()",
	"apex:eventbus.FailureResult.getEventUuids()",
	"apex:eventbus.SuccessResult",
	"apex:eventbus.SuccessResult.SuccessResult()",
	"apex:eventbus.SuccessResult.getEventUuids()",
}

var eventBusTestServiceIDs = []string{
	"apex:eventbus.TestBroker.clone()",
	"apex:eventbus.TestBroker.deliver()",
	"apex:eventbus.TestBroker.fail()",
	"apex:eventbus.TestEventService.clone()",
	"apex:eventbus.TestEventService.publishEvent(String,Map<String,Object>)",
}

var eventBusTriggerContextIDs = []string{
	"apex:eventbus.TriggerContext.currentContext()",
}

func TestEventBusTailHasExactExecutableLocalEvidence(t *testing.T) {
	root := filepath.Join("..", "..")
	resultPath := filepath.Join(root, "docs", "fixtures", eventBusCallbackResultFixture)
	servicePath := filepath.Join(root, "docs", "fixtures", eventBusTestServiceFixture)
	contextPath := filepath.Join(root, "docs", "fixtures", eventBusTriggerContextFixture)
	result := loadExactCandidateEventBusFixture(t, resultPath, eventBusCallbackResultFixture, "exec", len(eventBusCallbackResultIDs))
	service := loadExactCandidateEventBusFixture(t, servicePath, eventBusTestServiceFixture, "test", len(eventBusTestServiceIDs))
	context := loadExactCandidateEventBusFixture(t, contextPath, eventBusTriggerContextFixture, "exec", len(eventBusTriggerContextIDs))
	if len(result.Command.Args) != 1 || len(result.Source) != 1 || result.Source[0].Content != result.Command.Args[0] {
		t.Fatalf("callback/result execution envelope = %#v", result)
	}
	if len(context.Command.Args) != 1 || len(context.Source) != 1 || context.Source[0].Content != context.Command.Args[0] {
		t.Fatalf("TriggerContext execution envelope = %#v", context)
	}

	resultEvidence, err := BuildEvidenceSnapshot([]string{resultPath})
	if err != nil {
		t.Fatal(err)
	}
	assertExactSurfaceSet(t, resultEvidence, eventBusCallbackResultIDs)
	serviceEvidence, err := BuildEvidenceSnapshot([]string{servicePath})
	if err != nil {
		t.Fatal(err)
	}
	assertExactSurfaceSet(t, serviceEvidence, eventBusTestServiceIDs)
	contextEvidence, err := BuildEvidenceSnapshot([]string{contextPath})
	if err != nil {
		t.Fatal(err)
	}
	assertExactSurfaceSet(t, contextEvidence, eventBusTriggerContextIDs)
	allEvidence := append(append(resultEvidence, serviceEvidence...), contextEvidence...)
	for _, row := range allEvidence {
		if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported {
			t.Fatalf("%s evidence/behavior = %s/%s, want fixture/supported", row.SurfaceID, row.Evidence, row.GladeBehavior)
		}
	}

	for _, witness := range []string{
		"eventbus.EventPublishFailureCallback failureCallback",
		"eventbus.EventPublishSuccessCallback successCallback",
		"new eventbus.FailureResult()",
		"failure.getEventUuids()",
		"new eventbus.SuccessResult()",
		"success.getEventUuids()",
	} {
		if !strings.Contains(result.Source[0].Content, witness) {
			t.Fatalf("callback/result source missing executable witness %q", witness)
		}
	}
	serviceSource := ""
	for _, source := range service.Source {
		serviceSource += source.Content
	}
	for _, witness := range []string{
		"broker.clone()",
		"broker.deliver()",
		"Test.getEventBus().fail()",
		"service.clone()",
		"eventbus.TestEventService.publishEvent",
	} {
		if !strings.Contains(serviceSource, witness) {
			t.Fatalf("test-service source missing executable witness %q", witness)
		}
	}
	if !strings.Contains(context.Source[0].Content, "eventbus.TriggerContext.currentContext()") || !strings.Contains(context.Source[0].Content, "System.assertNotEquals(null, context)") {
		t.Fatalf("TriggerContext source does not observe the local current-context result: %q", context.Source[0].Content)
	}

	wantIDs := append(append(append([]string{}, eventBusCallbackResultIDs...), eventBusTestServiceIDs...), eventBusTriggerContextIDs...)
	want := mapFromIDs(wantIDs)
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
	for _, id := range wantIDs {
		if owners[id] != 1 {
			t.Fatalf("fixture ownership for %s = %d, want exactly one non-evidenceOnly owner", id, owners[id])
		}
	}

	for _, fixture := range []compat.Fixture{result, service, context} {
		if result, err := compat.Run(fixture); err != nil || !result.OK {
			t.Fatalf("fixture %s execution = %#v, error = %v", fixture.Name, result, err)
		}
	}
}

func loadExactCandidateEventBusFixture(t *testing.T, path, filename, commandKind string, rows int) compat.Fixture {
	t.Helper()
	fixture, err := compat.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Name != strings.TrimSuffix(filename, ".json") || fixture.Command.Kind != commandKind {
		t.Fatalf("fixture execution envelope = %#v", fixture)
	}
	data, err := os.ReadFile(path)
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
		Profile                   struct {
			CandidateCommit string `json:"candidateCommit"`
			CandidateSHA256 string `json:"candidateSha256"`
			SelectedRows    int    `json:"selectedRowCount"`
		} `json:"profile"`
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.APIVersion != "67.0" || metadata.Mode != "local-runtime" || metadata.EvidenceOnly || metadata.SalesforceEligible == nil || *metadata.SalesforceEligible || metadata.SalesforceExclusionClass != "policy-local-only" || !strings.Contains(metadata.SalesforceExclusionReason, "zero Salesforce parity") || metadata.Profile.CandidateCommit != "3409c4c85827b19712e9df83fc8905aa02bd1dc8" || metadata.Profile.CandidateSHA256 != "960ac9f26fa92aae6054cbe0e59f9c4ab1f84397df67bd8a89528068d02a1fce" || metadata.Profile.SelectedRows != rows {
		t.Fatalf("fixture provenance = %#v", metadata)
	}
	if metadata.Salesforce != nil || metadata.Comparisons != nil || !strings.Contains(metadata.Notes, "no hosted Salesforce execution or parity claim") {
		t.Fatalf("fixture makes an unsupported Salesforce parity claim: %#v", metadata)
	}
	return fixture
}
