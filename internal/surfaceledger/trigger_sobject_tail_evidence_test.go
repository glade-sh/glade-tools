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
	triggerTailFixture = "core-runtime-trigger-sobject-tail-api67.json"
	sObjectTailFixture = "core-runtime-sobject-tail-api67.json"
)

var triggerTailIDs = []string{
	"apex:System.Trigger",
	"apex:System.Trigger.isAfter",
	"apex:System.Trigger.isBefore",
	"apex:System.Trigger.isDelete",
	"apex:System.Trigger.isExecuting",
	"apex:System.Trigger.isInsert",
	"apex:System.Trigger.isUndelete",
	"apex:System.Trigger.isUpdate",
	"apex:System.Trigger.new",
	"apex:System.Trigger.newMap",
	"apex:System.Trigger.old",
	"apex:System.Trigger.oldMap",
	"apex:System.Trigger.operationType",
	"apex:System.Trigger.size",
}

var sObjectTailIDs = []string{
	"apex:System.SObject",
	"apex:System.SObject.addError(Exception)",
	"apex:System.SObject.getSObject(Schema.SObjectField)",
	"apex:System.SObject.getSObjects(Schema.SObjectField)",
	"apex:System.SObject.hashCode()",
	"apex:System.SObject.recalculateFormulas()",
	"apex:System.SObject.toString()",
}

func TestSystemTriggerSObjectTailHasExactExecutableLocalEvidence(t *testing.T) {
	root := filepath.Join("..", "..")
	triggerPath := filepath.Join(root, "docs", "fixtures", triggerTailFixture)
	sObjectPath := filepath.Join(root, "docs", "fixtures", sObjectTailFixture)
	triggerFixture := loadTailFixture(t, triggerPath)
	sObjectFixture := loadTailFixture(t, sObjectPath)
	if triggerFixture.Command.Kind != "test" || len(triggerFixture.Command.Args) != 0 || len(triggerFixture.Source) == 0 {
		t.Fatalf("trigger fixture envelope = %#v", triggerFixture)
	}
	if sObjectFixture.Command.Kind != "exec" || len(sObjectFixture.Command.Args) != 1 || len(sObjectFixture.Source) == 0 {
		t.Fatalf("SObject fixture envelope = %#v", sObjectFixture)
	}

	allIDs := append(append([]string{}, triggerTailIDs...), sObjectTailIDs...)
	rows, err := BuildEvidenceSnapshot([]string{triggerPath, sObjectPath})
	if err != nil {
		t.Fatal(err)
	}
	assertExactSurfaceSet(t, rows, allIDs)
	for _, item := range []struct {
		fixture compat.Fixture
		kind    string
	}{
		{triggerFixture, "test"},
		{sObjectFixture, "exec"},
	} {
		for _, row := range item.fixture.Evidence {
			if row.Kind != item.kind {
				t.Fatalf("evidence kind for %s = %q, want %s", row.SurfaceID, row.Kind, item.kind)
			}
		}
	}

	assertTailFixtureMetadata(t, triggerPath, "local-runtime", len(triggerTailIDs))
	assertTailFixtureMetadata(t, sObjectPath, "local-runtime", len(sObjectTailIDs))

	var source strings.Builder
	source.WriteString(sObjectFixture.Command.Args[0])
	source.WriteByte('\n')
	for _, fixture := range []compat.Fixture{triggerFixture, sObjectFixture} {
		for _, file := range fixture.Source {
			source.WriteString(file.Content)
			source.WriteByte('\n')
		}
	}
	for _, witness := range []string{
		"Trigger.new", "Trigger.newMap", "Trigger.old", "Trigger.oldMap", "Trigger.isAfter", "Trigger.isBefore", "Trigger.isDelete", "Trigger.isExecuting", "Trigger.isInsert", "Trigger.isUndelete", "Trigger.isUpdate", "Trigger.operationType", "Trigger.size",
		"SObject row = new Account", "class TailException extends Exception", "row.addError(new SObjectTailSupport.TailException", "child.getSObject(accountField)", "row.hashCode()", "row.toString()", "row.recalculateFormulas()",
	} {
		if !strings.Contains(source.String(), witness) {
			t.Fatalf("source missing direct assertion %q", witness)
		}
	}

	owners := make(map[string]int, len(allIDs))
	want := mapFromIDs(allIDs)
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
	for _, id := range allIDs {
		if owners[id] != 1 {
			t.Fatalf("non-evidenceOnly ownership for %s = %d, want exactly one", id, owners[id])
		}
	}
	for _, fixture := range []compat.Fixture{triggerFixture, sObjectFixture} {
		if result, err := compat.Run(fixture); err != nil || !result.OK {
			t.Fatalf("fixture %s execution = %#v, error = %v", fixture.Name, result, err)
		}
	}
}

func loadTailFixture(t *testing.T, path string) compat.Fixture {
	t.Helper()
	fixture, err := compat.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Name != strings.TrimSuffix(filepath.Base(path), ".json") {
		t.Fatalf("fixture name = %q", fixture.Name)
	}
	return fixture
}

func assertTailFixtureMetadata(t *testing.T, path, mode string, selectedRows int) {
	t.Helper()
	raw, err := os.ReadFile(path)
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
		Candidate                 struct {
			Commit string `json:"commit"`
			SHA256 string `json:"sha256"`
		} `json:"candidate"`
		Profile struct {
			CandidateCommit string `json:"candidateCommit"`
			CandidateSHA256 string `json:"candidateSha256"`
			SelectedRows    int    `json:"selectedRowCount"`
		} `json:"profile"`
	}
	if err := json.Unmarshal(raw, &metadata); err != nil {
		t.Fatal(err)
	}
	wantCommit := "3409c4c85827b19712e9df83fc8905aa02bd1dc8"
	wantSHA := "960ac9f26fa92aae6054cbe0e59f9c4ab1f84397df67bd8a89528068d02a1fce"
	if filepath.Base(path) == sObjectTailFixture {
		wantCommit = "86ec4226e33f205bf7a42f6f00cc40aa57fc11b5"
		wantSHA = "0aa758618a8908550aa468c4c9eabd1fcdd06f9f6a7d317ccce45a077380d29a"
	}
	if metadata.APIVersion != "67.0" || metadata.Mode != mode || metadata.EvidenceOnly || metadata.SalesforceEligible == nil || *metadata.SalesforceEligible || metadata.SalesforceExclusionClass != "policy-local-only" || !strings.Contains(metadata.SalesforceExclusionReason, "zero hosted Salesforce parity") || metadata.Candidate.Commit != wantCommit || metadata.Candidate.SHA256 != wantSHA || metadata.Profile.CandidateCommit != metadata.Candidate.Commit || metadata.Profile.CandidateSHA256 != metadata.Candidate.SHA256 || metadata.Profile.SelectedRows != selectedRows || metadata.Salesforce != nil || metadata.Comparisons != nil || !strings.Contains(metadata.Notes, "no hosted Salesforce execution or parity claim") {
		t.Fatalf("fixture provenance = %#v", metadata)
	}
}
