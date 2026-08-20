package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func TestPrivateQueueableTypesUseAsyncLifecycleFixture(t *testing.T) {
	const owner = "async-context-job-record-edges"
	want := map[string]string{
		"apex:System.Queueable":        "implements Queueable",
		"apex:System.QueueableContext": "execute(QueueableContext qc)",
	}
	root := filepath.Join("..", "..", "docs", "fixtures")
	paths, err := filepath.Glob(filepath.Join(root, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := BuildEvidenceSnapshot(paths)
	if err != nil {
		t.Fatal(err)
	}
	for id := range want {
		count := 0
		for _, row := range rows {
			if row.SurfaceID != id {
				continue
			}
			count++
			if len(row.Sources) != 1 || row.Sources[0] != "fixture:"+owner || row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported {
				t.Fatalf("%s evidence row = %#v", id, row)
			}
		}
		if count != 1 {
			t.Fatalf("%s fixture rows = %d, want exactly one", id, count)
		}
	}
	path := filepath.Join(root, owner+".json")
	fixture, err := compat.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if fixture.Command.Kind != "test" || len(fixture.Evidence) != 9 {
		t.Fatalf("fixture envelope = %#v", fixture)
	}
	source := ""
	for _, file := range fixture.Source {
		source += file.Content
	}
	for id, witness := range want {
		if !strings.Contains(source, witness) {
			t.Fatalf("%s source missing %q", id, witness)
		}
	}
	result, err := compat.Run(fixture)
	if err != nil || !result.OK {
		t.Fatalf("fixture run = %#v, error = %v", result, err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var policy struct {
		Eligible *bool  `json:"salesforceEligible"`
		Class    string `json:"salesforceExclusionClass"`
		Reason   string `json:"salesforceExclusionReason"`
	}
	if err := json.Unmarshal(raw, &policy); err != nil {
		t.Fatal(err)
	}
	if policy.Eligible == nil || *policy.Eligible || policy.Class != "policy-local-only" || !strings.Contains(policy.Reason, "zero Salesforce parity") {
		t.Fatalf("fixture policy = %#v", policy)
	}
}
