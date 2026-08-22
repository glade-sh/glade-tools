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
	miscContextCandidateCommit = "3409c4c85827b19712e9df83fc8905aa02bd1dc8"
	miscContextCandidateSHA    = "960ac9f26fa92aae6054cbe0e59f9c4ab1f84397df67bd8a89528068d02a1fce"
)

var miscContextTailFixtures = map[string][]string{
	"core-runtime-answers-find-similar-local-evidence.json": {
		"apex:Answers.findSimilar",
		"apex:Answers.findSimilar(Object)",
	},
	"core-runtime-cache-tail-local-evidence-api67.json": {
		"apex:Cache.ScanResult.isDone",
		"apex:Cache.ScanResult.result",
		"apex:Cache.ScanResult.scanLocator",
	},
	"core-runtime-process-plugin-tail-local-evidence-api67.json": {
		"apex:Process.Plugin.describe()",
		"apex:Process.Plugin.invoke(Process.PluginRequest)",
		"apex:Process.PluginDescribeResult.InputParameter",
		"apex:Process.PluginDescribeResult.OutputParameter",
	},
	"core-runtime-queueable-duplicate-signature-tail-local-evidence-api67.json": {
		"apex:QueueableDuplicateSignature",
		"apex:QueueableDuplicateSignature.Builder",
		"apex:QueueableDuplicateSignature.Builder.Builder()",
		"apex:QueueableDuplicateSignature.Builder.clone()",
		"apex:QueueableDuplicateSignature.clone()",
	},
}

func TestMiscContextTailHasExactCandidateLocalEvidence(t *testing.T) {
	root := filepath.Join("..", "..")
	owners := make(map[string]string, 14)
	for filename, wantIDs := range miscContextTailFixtures {
		path := filepath.Join(root, "docs", "fixtures", filename)
		fixture, err := compat.LoadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := compat.Validate(fixture); err != nil {
			t.Fatal(err)
		}
		mode, commandKind := "deterministic-mock", "test"
		if filename == "core-runtime-queueable-duplicate-signature-tail-local-evidence-api67.json" {
			mode, commandKind = "local-runtime", "exec"
		}
		if fixture.Command.Kind != commandKind || commandKind == "test" && len(fixture.Command.Args) != 0 || commandKind == "exec" && (len(fixture.Command.Args) != 1 || len(fixture.Source) != 1 || fixture.Command.Args[0] != fixture.Source[0].Content) || len(fixture.Source) == 0 {
			t.Fatalf("%s execution boundary = %#v", filename, fixture)
		}
		if result, err := compat.Run(fixture); err != nil || !result.OK {
			t.Fatalf("%s execution = %#v, error = %v", filename, result, err)
		}
		evidence, err := BuildEvidenceSnapshot([]string{path})
		if err != nil {
			t.Fatal(err)
		}
		assertExactSurfaceSet(t, evidence, wantIDs)
		for _, row := range fixture.Evidence {
			if row.Kind != commandKind {
				t.Fatalf("%s evidence kind for %s = %q, want %q", filename, row.SurfaceID, row.Kind, commandKind)
			}
		}
		for _, row := range evidence {
			if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported || len(row.Sources) != 1 || row.Sources[0] != "fixture:"+fixture.Name {
				t.Fatalf("%s row = %#v", row.SurfaceID, row)
			}
			owners[row.SurfaceID] = filename
		}

		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var metadata struct {
			APIVersion   string `json:"apiVersion"`
			Mode         string `json:"mode"`
			EvidenceOnly bool   `json:"evidenceOnly"`
			Candidate    struct {
				Commit string `json:"commit"`
				SHA    string `json:"sha256"`
			} `json:"candidate"`
			Profile struct {
				CandidateCommit string `json:"candidateCommit"`
				CandidateSHA256 string `json:"candidateSha256"`
				LaneID          string `json:"laneId"`
				SelectedRows    int    `json:"selectedRowCount"`
			} `json:"profile"`
			SalesforceEligible        *bool  `json:"salesforceEligible"`
			SalesforceExclusionClass  string `json:"salesforceExclusionClass"`
			SalesforceExclusionReason string `json:"salesforceExclusionReason"`
			Salesforce                any    `json:"salesforce"`
			Comparisons               any    `json:"comparisons"`
		}
		if err := json.Unmarshal(data, &metadata); err != nil {
			t.Fatal(err)
		}
		if metadata.APIVersion != "67.0" || metadata.Mode != mode || metadata.EvidenceOnly || metadata.Candidate.Commit != miscContextCandidateCommit || metadata.Candidate.SHA != miscContextCandidateSHA || metadata.Profile.CandidateCommit != miscContextCandidateCommit || metadata.Profile.CandidateSHA256 != miscContextCandidateSHA || metadata.Profile.LaneID == "" || metadata.Profile.SelectedRows != len(wantIDs) || metadata.SalesforceEligible == nil || *metadata.SalesforceEligible || metadata.SalesforceExclusionClass != "policy-local-only" || !strings.Contains(strings.ToLower(metadata.SalesforceExclusionReason), "zero salesforce parity") || metadata.Salesforce != nil || metadata.Comparisons != nil {
			t.Fatalf("%s provenance = %#v", filename, metadata)
		}
	}
	if len(owners) != 14 {
		t.Fatalf("target owner count = %d, want 14", len(owners))
	}

	paths, err := filepath.Glob(filepath.Join(root, "docs", "fixtures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	targets := make(map[string]bool, len(owners))
	for id := range owners {
		targets[id] = true
	}
	counts := make(map[string]int, len(targets))
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
			if targets[row.SurfaceID] {
				counts[row.SurfaceID]++
			}
		}
	}
	for id := range targets {
		if counts[id] != 1 {
			t.Fatalf("non-evidenceOnly ownership for %s = %d, want exactly one", id, counts[id])
		}
	}
}

func TestMiscContextTailLeavesCacheValidateKeysAsNonParity(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "fixtures", "current-base-cache-negative-api67.json")
	fixture, err := compat.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil || fixture.Expected.Error == nil || fixture.Expected.Error.Type != "UnsupportedOperationException" {
		t.Fatalf("Cache rejection fixture = %#v, validation error = %v", fixture, err)
	}
	want := map[string]bool{
		"apex:Cache.OrgPartition.validateKeys(Boolean,List<String>)":     true,
		"apex:Cache.OrgPartition.validateKeys(Boolean,Set<String>)":      true,
		"apex:Cache.Partition.validateKeys(Boolean,List<String>)":        true,
		"apex:Cache.Partition.validateKeys(Boolean,Set<String>)":         true,
		"apex:Cache.SessionPartition.validateKeys(Boolean,List<String>)": true,
		"apex:Cache.SessionPartition.validateKeys(Boolean,Set<String>)":  true,
	}
	seen := make(map[string]bool, len(want))
	for _, evidence := range fixture.Evidence {
		if want[evidence.SurfaceID] {
			seen[evidence.SurfaceID] = true
		}
	}
	if len(seen) != len(want) {
		t.Fatalf("Cache.validateKeys rows = %d, want six", len(seen))
	}
	data, err := os.ReadFile(path)
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
	if policy.SalesforceEligible == nil || *policy.SalesforceEligible || policy.SalesforceExclusionClass != "policy-local-only" || !strings.Contains(strings.ToLower(policy.SalesforceExclusionReason), "zero salesforce parity") {
		t.Fatalf("Cache.validateKeys policy = %#v", policy)
	}
}
