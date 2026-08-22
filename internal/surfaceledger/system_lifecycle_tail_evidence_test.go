package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

var systemLifecycleTailFixtures = map[string][]string{
	"core-runtime-async-context-tail-api67.json": {
		"apex:System.Queueable",
		"apex:System.Queueable.execute(QueueableContext)",
		"apex:System.QueueableContext",
		"apex:System.QueueableContext.getJobId()",
		"apex:System.Schedulable",
		"apex:System.Schedulable.execute(SchedulableContext)",
		"apex:System.SchedulableContext",
		"apex:System.SchedulableContext.getTriggerId()",
	},
	"core-runtime-finalizer-context-tail-api67.json": {
		"apex:System.Finalizer",
		"apex:System.Finalizer.execute(FinalizerContext)",
		"apex:System.FinalizerContext",
		"apex:System.FinalizerContext.getException()",
		"apex:System.FinalizerContext.getRequestId()",
		"apex:System.FinalizerContext.getResult()",
	},
}

func TestSystemLifecycleTailHasExactLocalEvidenceAndOwnership(t *testing.T) {
	root := filepath.Join("..", "..")
	paths, err := filepath.Glob(filepath.Join(root, "docs", "fixtures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{}
	for fixtureName, ids := range systemLifecycleTailFixtures {
		path := filepath.Join(root, "docs", "fixtures", fixtureName)
		fixture, err := compat.LoadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := compat.Validate(fixture); err != nil {
			t.Fatal(err)
		}
		if fixture.Command.Kind != "test" || len(fixture.Source) == 0 || len(fixture.Evidence) != len(ids) {
			t.Fatalf("%s envelope = %#v", fixtureName, fixture)
		}
		for _, id := range ids {
			want[id] = fixtureName
		}
		for _, source := range fixture.Source {
			if strings.Contains(source.Content, "new System.FinalizerContextImpl()") {
				t.Fatalf("%s constructs unsupported no-arg FinalizerContextImpl", fixtureName)
			}
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var metadata struct {
			APIVersion         string `json:"apiVersion"`
			Mode               string `json:"mode"`
			Notes              string `json:"notes"`
			EvidenceOnly       bool   `json:"evidenceOnly"`
			SalesforceEligible *bool  `json:"salesforceEligible"`
			ExclusionClass     string `json:"salesforceExclusionClass"`
			ExclusionReason    string `json:"salesforceExclusionReason"`
			Salesforce         any    `json:"salesforce"`
			Comparisons        any    `json:"comparisons"`
			Candidate          struct {
				Commit string `json:"commit"`
				SHA256 string `json:"sha256"`
			} `json:"candidate"`
			Profile            struct {
				CandidateCommit string `json:"candidateCommit"`
				CandidateSHA256 string `json:"candidateSha256"`
				SelectedRows    int    `json:"selectedRowCount"`
			} `json:"profile"`
		}
		if err := json.Unmarshal(data, &metadata); err != nil {
			t.Fatal(err)
		}
		if metadata.APIVersion != "67.0" || metadata.Mode != "local-runtime" || metadata.EvidenceOnly || metadata.SalesforceEligible == nil || *metadata.SalesforceEligible || metadata.ExclusionClass != "policy-local-only" || !strings.Contains(metadata.ExclusionReason, "zero hosted Salesforce parity") || metadata.Candidate.Commit != "3409c4c85827b19712e9df83fc8905aa02bd1dc8" || metadata.Candidate.SHA256 != "960ac9f26fa92aae6054cbe0e59f9c4ab1f84397df67bd8a89528068d02a1fce" || metadata.Profile.CandidateCommit != metadata.Candidate.Commit || metadata.Profile.CandidateSHA256 != metadata.Candidate.SHA256 || metadata.Profile.SelectedRows != len(ids) || metadata.Salesforce != nil || metadata.Comparisons != nil || !strings.Contains(metadata.Notes, "deterministic local") {
			t.Fatalf("%s provenance = %#v", fixtureName, metadata)
		}
		seen := map[string]bool{}
		for _, evidence := range fixture.Evidence {
			if evidence.Kind != "test" || want[evidence.SurfaceID] != fixtureName || seen[evidence.SurfaceID] {
				t.Fatalf("%s evidence = %#v", fixtureName, evidence)
			}
			seen[evidence.SurfaceID] = true
		}
		if result, err := compat.Run(fixture); err != nil || !result.OK {
			t.Fatalf("%s execution = %#v, error = %v", fixtureName, result, err)
		}
	}

	owners := map[string]int{}
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
		for _, evidence := range header.Evidence {
			if _, ok := want[evidence.SurfaceID]; ok {
				owners[evidence.SurfaceID]++
			}
		}
	}
	for id := range want {
		if owners[id] != 1 {
			t.Fatalf("fixture ownership for %s = %d, want exactly one non-evidenceOnly owner", id, owners[id])
		}
	}

	evidencePaths := make([]string, 0, len(systemLifecycleTailFixtures))
	for fixtureName := range systemLifecycleTailFixtures {
		evidencePaths = append(evidencePaths, filepath.Join(root, "docs", "fixtures", fixtureName))
	}
	evidence, err := BuildEvidenceSnapshot(evidencePaths)
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
}
