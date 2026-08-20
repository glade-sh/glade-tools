package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func TestDuplicateRuleHeaderHasExecutableLocalNoopEvidence(t *testing.T) {
	root := filepath.Join("..", "..")
	path := filepath.Join(root, "docs", "fixtures", "core-runtime-dml-options-duplicate-rule-local.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var metadata struct {
		SalesforceEligible        *bool  `json:"salesforceEligible"`
		SalesforceExclusionClass  string `json:"salesforceExclusionClass"`
		SalesforceExclusionReason string `json:"salesforceExclusionReason"`
		Source                    []struct {
			Content string `json:"content"`
		} `json:"source"`
		Command struct {
			Args []string `json:"args"`
		} `json:"command"`
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.SalesforceEligible == nil || *metadata.SalesforceEligible || metadata.SalesforceExclusionClass != "policy-local-only" || !strings.Contains(metadata.SalesforceExclusionReason, "zero Salesforce") {
		t.Fatalf("local-only metadata = %#v", metadata)
	}
	if len(metadata.Source) != 1 || len(metadata.Command.Args) != 1 || metadata.Source[0].Content != metadata.Command.Args[0] {
		t.Fatal("fixture source and command must be identical")
	}
	for _, witness := range []string{"AllowSave = true", "RunAsCurrentUser = true", "Database.insert", "result.isSuccess()", "SELECT COUNT()"} {
		if !strings.Contains(metadata.Source[0].Content, witness) {
			t.Fatalf("fixture source missing %q", witness)
		}
	}
	fixture, err := compat.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	result, err := compat.Run(fixture)
	if err != nil || !result.OK {
		t.Fatalf("fixture run = %#v, %v", result, err)
	}
	rows, err := BuildEvidenceSnapshot([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"apex:Database.DMLOptions.DuplicateRuleHeader",
		"apex:Database.DMLOptions.DuplicateRuleHeader.allowSave",
		"apex:Database.DMLOptions.DuplicateRuleHeader.runAsCurrentUser",
	}
	got := make([]string, len(rows))
	for i, row := range rows {
		got[i] = row.SurfaceID
		if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported {
			t.Fatalf("%s evidence/behavior = %s/%s", row.SurfaceID, row.Evidence, row.GladeBehavior)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("surface IDs = %#v, want %#v", got, want)
	}
	owners := make(map[string][]string, len(want))
	for _, id := range want {
		owners[id] = nil
	}
	paths, err := filepath.Glob(filepath.Join(root, "docs", "fixtures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, candidatePath := range paths {
		candidateData, err := os.ReadFile(candidatePath)
		if err != nil {
			t.Fatal(err)
		}
		var candidate struct {
			Evidence []compat.FixtureEvidence `json:"evidence"`
		}
		if err := json.Unmarshal(candidateData, &candidate); err != nil {
			t.Fatal(err)
		}
		for _, row := range candidate.Evidence {
			if _, target := owners[row.SurfaceID]; target {
				owners[row.SurfaceID] = append(owners[row.SurfaceID], filepath.Base(candidatePath))
			}
		}
	}
	for id, paths := range owners {
		if len(paths) != 1 || paths[0] != filepath.Base(path) {
			t.Fatalf("%s owners = %v, want only %s", id, paths, filepath.Base(path))
		}
	}
	policy, err := LoadSupportPolicy(filepath.Join(root, "docs", "fixtures", "apex-local-support-policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	profile := ComputeSupportProfile(Merge(nil, nil, BuildGladeSnapshot(), rows).Rows, policy, nil)
	byID := make(map[string]SupportProfileRow, len(profile.Rows))
	for _, row := range profile.Rows {
		byID[row.SurfaceID] = row
	}
	for _, id := range want {
		row := byID[id]
		if row.Disposition != DispositionLocalRuntimeRequired || row.MatchRule != "namespace=Database" || row.GapClass != GapMissingEvidence {
			t.Fatalf("%s profile = %#v", id, row)
		}
	}
}
