package perfscan

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/internal/apexast"
	"github.com/glade-sh/glade/internal/project"
	gladeschema "github.com/glade-sh/glade/internal/schema"
	"github.com/glade-sh/glade/internal/trace"
	"github.com/glade-sh/glade/internal/typesys"
)

func testPerfProject(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "sfdx-project.json"), `{"packageDirectories":[{"path":"force-app","default":true}],"sourceApiVersion":"63.0"}`)
	for rel, body := range files {
		writeTestFile(t, filepath.Join(root, filepath.FromSlash(rel)), body)
	}
	return root
}

func testPerfProjectWithMetadata(t *testing.T) string {
	t.Helper()
	return testPerfProject(t, map[string]string{
		"force-app/main/default/triggers/AccountTrigger.trigger":         "trigger AccountTrigger on Account (after update) { update Trigger.new; }",
		"force-app/main/default/flows/Account_After_Save.flow-meta.xml":  "<Flow><start><object>Account</object><triggerType>RecordAfterSave</triggerType></start><recordUpdates><name>Update_Account</name></recordUpdates></Flow>",
		"force-app/main/default/workflows/Account.workflow-meta.xml":     "<Workflow><rules><fullName>Active_Rule</fullName><active>true</active></rules></Workflow>",
		"force-app/main/default/objects/Account/Account.object-meta.xml": "<CustomObject xmlns=\"http://soap.sforce.com/2006/04/metadata\"><label>Account</label></CustomObject>",
	})
}

func analyzeTestProject(t *testing.T, root string, options Options) Report {
	t.Helper()
	options.ProjectRoot = root
	report, err := AnalyzeProject(options)
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func buildTestSourceGraph(t *testing.T, root string) *Graph {
	t.Helper()
	p, err := project.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	schema, err := gladeschema.LoadProject(p)
	if err != nil {
		t.Fatal(err)
	}
	parser := apexast.NewParser()
	parsed := apexast.Result{Files: make([]apexast.File, 0, len(p.ApexFiles))}
	for _, path := range p.ApexFiles {
		file, err := parser.ParseFile(path)
		if err != nil {
			t.Fatal(err)
		}
		parsed.Files = append(parsed.Files, file)
	}
	return BuildSourceGraph(parsed, typesys.Build(p, schema))
}

func requireFinding(t *testing.T, report Report, id string) Finding {
	t.Helper()
	for _, finding := range report.Findings {
		if finding.ID == id {
			return finding
		}
	}
	t.Fatalf("missing finding %s in %#v", id, report.Findings)
	return Finding{}
}

func requireEvidence(t *testing.T, finding Finding, kind, messagePart string) {
	t.Helper()
	for _, evidence := range finding.Evidence {
		if evidence.Kind == kind && strings.Contains(evidence.Message, messagePart) {
			return
		}
	}
	t.Fatalf("missing evidence %s/%s in %#v", kind, messagePart, finding.Evidence)
}

func writeTrace(t *testing.T, events []trace.Event) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "trace.json")
	var buf bytes.Buffer
	if err := trace.WriteJSON(&buf, trace.NewDocument(events)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeOrgFactsFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "org-facts.json")
	writeTestFile(t, path, `{
  "schemaVersion": 1,
  "objects": {
    "Account": {
      "estimatedRows": 1200000,
      "sharingModel": "Private",
      "fields": {
        "External_Id__c": {"indexed": true, "unique": true},
        "Formula_Key__c": {"formula": true}
      }
    },
    "Contact": {
      "estimatedRows": 900000,
      "parentSkew": [{"field": "AccountId", "parentId": "001xx000003DHP0", "count": 24000}]
    }
  }
}`)
	return path
}

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func marshalReport(t *testing.T, report Report) []byte {
	t.Helper()
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(data, '\n')
}
