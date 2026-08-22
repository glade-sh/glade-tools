package surfaceledger

import (
 "encoding/json"
 "os"
 "path/filepath"
 "strings"
 "testing"
 "github.com/glade-sh/glade/tools/internal/compat"
)

func TestJSONParserTailEvidence(t *testing.T) {
 root := filepath.Join("..", "..")
 path := filepath.Join(root, "docs", "fixtures", "core-runtime-jsonparser-tail-local-evidence.json")
 fixture, err := compat.LoadFile(path); if err != nil { t.Fatal(err) }
 if err := compat.Validate(fixture); err != nil { t.Fatal(err) }
 if len(fixture.Source) != 1 || len(fixture.Command.Args) != 1 || fixture.Source[0].Content != fixture.Command.Args[0] { t.Fatal("source/command mismatch") }
 if result, err := compat.Run(fixture); err != nil || !result.OK { t.Fatalf("run=%#v err=%v", result, err) }
 source := fixture.Source[0].Content
 for _, witness := range []string{"JSON.createParser", ".clone()", ".getBooleanValue()", ".getDateValue()", ".getText()", "START_OBJECT"} { if !strings.Contains(source, witness) { t.Fatalf("source missing %q", witness) } }
 raw, err := os.ReadFile(path); if err != nil { t.Fatal(err) }
 var meta struct { APIVersion string `json:"apiVersion"`; Mode string `json:"mode"`; EvidenceOnly bool `json:"evidenceOnly"`; Candidate struct { Commit string `json:"commit"`; SHA string `json:"sha256"` } `json:"candidate"`; Profile struct { CandidateCommit string `json:"candidateCommit"`; CandidateSHA string `json:"candidateSha256"`; Lane string `json:"laneId"`; Count int `json:"selectedRowCount"` } `json:"profile"`; Eligible *bool `json:"salesforceEligible"`; Class string `json:"salesforceExclusionClass"`; Reason string `json:"salesforceExclusionReason"` }
 if err := json.Unmarshal(raw, &meta); err != nil { t.Fatal(err) }
 if meta.APIVersion != "67.0" || meta.Mode != "local-runtime" || meta.EvidenceOnly || meta.Eligible == nil || *meta.Eligible || meta.Class != "policy-local-only" || !strings.Contains(strings.ToLower(meta.Reason), "no hosted") || meta.Profile.CandidateCommit != "3409c4c85827b19712e9df83fc8905aa02bd1dc8" || meta.Profile.CandidateSHA != "960ac9f26fa92aae6054cbe0e59f9c4ab1f84397df67bd8a89528068d02a1fce" || meta.Profile.Lane == "" || meta.Profile.Count != 6 { t.Fatalf("metadata=%#v", meta) }
 var fields map[string]json.RawMessage; if err := json.Unmarshal(raw, &fields); err != nil { t.Fatal(err) }; for _, key := range []string{"salesforce", "comparisons", "selectedOrg", "salesforceEvidencePaths", "orgAlias", "orgId"} { if _, ok := fields[key]; ok { t.Fatalf("hosted field %q present", key) } }
 paths, err := filepath.Glob(filepath.Join(root, "docs", "fixtures", "*.json")); if err != nil { t.Fatal(err) }
 var eligiblePaths []string
 for _, candidate := range paths { data, err := os.ReadFile(candidate); if err != nil { t.Fatal(err) }; var header struct{ EvidenceOnly bool `json:"evidenceOnly"` }; if err := json.Unmarshal(data, &header); err != nil { t.Fatal(err) }; if !header.EvidenceOnly { eligiblePaths = append(eligiblePaths, candidate) } }
 rows, err := BuildEvidenceSnapshot(eligiblePaths); if err != nil { t.Fatal(err) }
 want := map[string]bool{"apex:System.JSONParser":true,"apex:System.JSONParser.JSONParser()":true,"apex:System.JSONParser.clone()":true,"apex:System.JSONParser.getBooleanValue":true,"apex:System.JSONParser.getDateValue":true,"apex:System.JSONParser.getText":true}; seen:=map[string]int{}
 for _, row := range rows { if want[row.SurfaceID] { seen[row.SurfaceID]++; if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported || len(row.Sources) != 1 || row.Sources[0] != "fixture:core-runtime-jsonparser-tail-local-evidence" { t.Fatalf("%s row=%#v", row.SurfaceID, row) } } }
 for id := range want { if seen[id] != 1 { t.Fatalf("%s owners=%d", id, seen[id]) } }
}
