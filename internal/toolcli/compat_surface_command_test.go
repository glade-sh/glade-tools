package toolcli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompatSurfaceRefreshWritesReports(t *testing.T) {
	root := t.TempDir()
	docs := filepath.Join(root, "docs")
	out := filepath.Join(root, "out")
	if err := os.MkdirAll(filepath.Join(docs, "apex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docs, "apex", "system_label.md"), []byte("# Label Class\n\n### get(String section, String key)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tooling := filepath.Join(root, "tooling.json")
	if err := os.WriteFile(tooling, []byte(`{"publicDeclarations":{"System":{"Label":{"methods":[{"name":"get","returnType":"String","parameters":[{"type":"String"},{"type":"String"}]}]}}}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"compat", "surface", "refresh", "--docs", docs, "--tooling-completions", tooling, "--out", out}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d stderr=%q stdout=%q", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "surface refresh: ok") {
		t.Fatalf("compact summary missing: %q", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(out, "SURFACE_LEDGER.json")); err != nil {
		t.Fatalf("ledger missing: %v", err)
	}
}

func TestCompatSurfaceDryRunPrintsTempDir(t *testing.T) {
	root := t.TempDir()
	docs := filepath.Join(root, "docs")
	if err := os.MkdirAll(filepath.Join(docs, "apex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docs, "apex", "system_object.md"), []byte("# Object Class\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"compat", "surface", "refresh", "--docs", docs, "--dry-run"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d stderr=%q stdout=%q", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "dryRunOut=") {
		t.Fatalf("dry-run path missing: %q", stdout.String())
	}
}

func TestCompatSurfaceSourcesReportsCompleteDocsShelf(t *testing.T) {
	root := t.TempDir()
	docs := filepath.Join(root, "docs")
	writeSurfaceSourceFixture(t, docs)

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"compat", "surface", "sources", "--docs", docs}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d stderr=%q stdout=%q", code, stderr.String(), stdout.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"surface sources: ok",
		"atlas: pinned=21 missing=0 partial=0",
		"nonAtlas: lwc=present siteReferences=present",
		"files: manifest=present searchIndex=present missingLocalMarkdown=0",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

func TestCompatSurfaceSourcesCheckComparesAndPreservesBaseline(t *testing.T) {
	root := t.TempDir()
	docs := filepath.Join(root, "docs")
	writeSurfaceSourceFixture(t, docs)
	checkPath := filepath.Join(root, "sources.md")

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"compat", "surface", "sources", "--docs", docs, "--output", checkPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("output exit %d stderr=%q stdout=%q", code, stderr.String(), stdout.String())
	}
	baseline, err := os.ReadFile(checkPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(docs, "field-reference")); err != nil {
		t.Fatal(err)
	}

	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"compat", "surface", "sources", "--docs", docs, "--check", checkPath}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected failure stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "surface sources drift") {
		t.Fatalf("missing failure summary: %q", stderr.String())
	}
	data, err := os.ReadFile(checkPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(baseline) {
		t.Fatalf("check mutated baseline\nbefore:\n%s\nafter:\n%s", string(baseline), string(data))
	}
}

func TestCompatSurfaceSourcesRejectsJSONWithCheck(t *testing.T) {
	root := t.TempDir()
	docs := filepath.Join(root, "docs")
	writeSurfaceSourceFixture(t, docs)

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"compat", "surface", "sources", "--docs", docs, "--json", "--check", filepath.Join(root, "sources.md")}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected failure stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "use only one of --json, --output, or --check") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestCompatSurfacePacketWritesAreaPacket(t *testing.T) {
	root := t.TempDir()
	ledger := filepath.Join(root, "ledger.json")
	out := filepath.Join(root, "docs", "agent-packets", "salesforce", "FeatureManagement.md")
	data := `{
  "schemaVersion": 1,
  "rows": [
    {"surfaceId":"apex:System.FeatureManagement.checkPermission(String)","product":"apex","area":"runtime","namespace":"System","typeName":"FeatureManagement","memberName":"checkPermission","kind":"method","docs":"present","gladeShape":"absent","evidence":"none","gapClass":"missing-shape","priority":10},
    {"surfaceId":"apex:System.Database.executeBatch(Object,Integer)","product":"apex","area":"runtime","namespace":"System","typeName":"Database","memberName":"executeBatch","kind":"method","docs":"present","gladeShape":"absent","evidence":"none","gapClass":"missing-shape","priority":20}
  ],
  "summary": {"gaps":{"missing-shape":2},"failures":{},"total":2}
}`
	if err := os.WriteFile(ledger, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"compat", "surface", "packet", "--ledger", ledger, "--area", "Core.Runtime.System.FeatureManagement", "--out", out}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d stderr=%q stdout=%q", code, stderr.String(), stdout.String())
	}
	packet, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	text := string(packet)
	for _, want := range []string{
		"# Salesforce Surface Packet: Core.Runtime.System.FeatureManagement",
		"dependsOn",
		"exclusiveFiles",
		"Area ratchet command",
		"apex:System.FeatureManagement.checkPermission(String)",
		"go test ./internal/vm ./glade-tools/internal/capability ./internal/repoguard",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("packet missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "executeBatch") {
		t.Fatalf("packet included a row from another area:\n%s", text)
	}
}

func TestCompatSurfacePacketRejectsUnknownArea(t *testing.T) {
	root := t.TempDir()
	ledger := filepath.Join(root, "ledger.json")
	if err := os.WriteFile(ledger, []byte(`{"schemaVersion":1,"rows":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"compat", "surface", "packet", "--ledger", ledger, "--area", "No.Such.Area"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected failure stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "unknown surface area") {
		t.Fatalf("missing unknown area error: %q", stderr.String())
	}
}

func TestCompatSurfacePacketWritesManifest(t *testing.T) {
	root := t.TempDir()
	ledger := filepath.Join(root, "ledger.json")
	manifest := filepath.Join(root, "manifest", "packets.json")
	data := `{
  "schemaVersion": 1,
  "rows": [
    {"surfaceId":"apex:System.FeatureManagement.checkPermission(String)","product":"apex","area":"runtime","namespace":"System","typeName":"FeatureManagement","memberName":"checkPermission","kind":"method","docs":"present","gladeShape":"absent","evidence":"none","gapClass":"missing-shape","priority":10},
    {"surfaceId":"rest:/services/data/vXX.X/sobjects","product":"rest","area":"server","kind":"resource","docs":"present","gladeShape":"signature-known","docsReturnType":"List","gladeReturnType":"Object","priority":20},
    {"surfaceId":"apex:System.Done","product":"apex","area":"runtime","namespace":"System","typeName":"Done","kind":"type","gladeBehavior":"supported","evidence":"fixture"}
  ]
}`
	if err := os.WriteFile(ledger, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"compat", "surface", "packet", "--ledger", ledger, "--manifest", manifest}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d stderr=%q stdout=%q", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "surface packet manifest:") {
		t.Fatalf("manifest summary missing: %q", stdout.String())
	}
	dataOut, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		TotalOpenRows int `json:"totalOpenRows"`
		Packets       []struct {
			ID           string   `json:"id"`
			Owner        string   `json:"owner"`
			SourceFamily string   `json:"sourceFamily"`
			SourceDir    string   `json:"sourceDir"`
			RowIDs       []string `json:"rowIds"`
		} `json:"packets"`
		UnassignedRows []string `json:"unassignedRows"`
	}
	if err := json.Unmarshal(dataOut, &decoded); err != nil {
		t.Fatalf("manifest JSON decode: %v\n%s", err, dataOut)
	}
	if decoded.TotalOpenRows != 2 {
		t.Fatalf("totalOpenRows = %d, want 2", decoded.TotalOpenRows)
	}
	if len(decoded.UnassignedRows) != 0 {
		t.Fatalf("unassignedRows = %#v", decoded.UnassignedRows)
	}
	seen := map[string]string{}
	for _, packet := range decoded.Packets {
		if packet.ID == "" || packet.Owner == "" {
			t.Fatalf("packet missing id or owner: %#v", packet)
		}
		if packet.SourceFamily == "" && packet.SourceDir == "" {
			t.Fatalf("packet %s missing source family or source dir", packet.ID)
		}
		for _, rowID := range packet.RowIDs {
			if previous := seen[rowID]; previous != "" {
				t.Fatalf("row %s assigned twice: %s and %s", rowID, previous, packet.ID)
			}
			seen[rowID] = packet.ID
		}
	}
	for _, want := range []string{
		"apex:System.FeatureManagement.checkPermission(String)",
		"rest:/services/data/vXX.X/sobjects",
	} {
		if seen[want] == "" {
			t.Fatalf("row %s not assigned in manifest: %#v", want, decoded)
		}
	}
}

func TestCompatSurfaceCheckAcceptsTypeMismatchRatchets(t *testing.T) {
	root := t.TempDir()
	ledger := filepath.Join(root, "ledger.json")
	data := `{
  "schemaVersion": 1,
  "rows": [
    {"surfaceId":"apex:System.BadReturn","product":"apex","area":"runtime","kind":"method","docs":"present","gladeShape":"signature-known","gladeBehavior":"supported","evidence":"fixture","docsReturnType":"String","gladeReturnType":"Boolean"},
    {"surfaceId":"apex:System.BadParams","product":"apex","area":"runtime","kind":"method","docs":"present","gladeShape":"signature-known","gladeBehavior":"supported","evidence":"fixture","docsParameters":["Set<String>"],"gladeParameters":["List<String>"]}
  ]
}`
	if err := os.WriteFile(ledger, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"compat", "surface", "check", "--ledger", ledger, "--max-missing-shape", "0", "--max-missing-behavior", "0", "--max-parser-failures", "0"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("old ratchets should ignore mismatch ceilings until those flags are explicit, exit %d stderr=%q stdout=%q", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "surface check: ok") {
		t.Fatalf("stdout = %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"compat", "surface", "check", "--ledger", ledger, "--max-return-type-mismatch", "1", "--max-parameter-mismatch", "1"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d stderr=%q stdout=%q", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "surface check: ok") {
		t.Fatalf("stdout = %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"compat", "surface", "check", "--ledger", ledger, "--max-return-type-mismatch", "0", "--max-parameter-mismatch", "1"}, &stdout, &stderr)
	if code == 0 || !strings.Contains(stderr.String(), "return-type-mismatch=1 exceeds max 0") {
		t.Fatalf("expected return-type ratchet failure, exit=%d stderr=%q stdout=%q", code, stderr.String(), stdout.String())
	}
}

func TestCompatSurfaceCheckStrictRejectsOpenRows(t *testing.T) {
	root := t.TempDir()
	ledger := filepath.Join(root, "ledger.json")
	data := `{
  "schemaVersion": 1,
  "rows": [
    {"surfaceId":"apex:System.Missing","product":"apex","area":"runtime","kind":"method","docs":"present","gladeShape":"absent","priority":10},
    {"surfaceId":"apex:System.BadReturn","product":"apex","area":"runtime","kind":"method","docs":"present","gladeShape":"signature-known","docsReturnType":"String","gladeReturnType":"Object","priority":20}
  ]
}`
	if err := os.WriteFile(ledger, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"compat", "surface", "check", "--ledger", ledger, "--strict", "--max-missing-shape", "1", "--max-return-type-mismatch", "1"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected strict failure stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	errText := stderr.String()
	for _, want := range []string{"open surface rows=2", "apex:System.Missing", "missing-shape"} {
		if !strings.Contains(errText, want) {
			t.Fatalf("strict error missing %q: %q", want, errText)
		}
	}

	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"compat", "surface", "check", "--ledger", ledger, "--strict"}, &stdout, &stderr)
	if code == 0 || !strings.Contains(stderr.String(), "open surface rows=2") {
		t.Fatalf("strict without ratchets should report open rows, exit=%d stderr=%q stdout=%q", code, stderr.String(), stdout.String())
	}
}

func writeSurfaceSourceFixture(t *testing.T, docs string) {
	t.Helper()
	docsets := []string{
		"apex", "apex-guide", "visualforce", "lightning", "rest-api", "tooling-api",
		"object-reference", "field-reference", "soql-sosl", "metadata-api", "soap-api",
		"bulk-api", "ui-api", "platform-events", "streaming-api", "connect-rest-api",
		"service-connector-api-reference", "limits-reference", "cli-reference",
		"analytics-cli-reference", "commerce-cli-reference", "lwc",
	}
	var manifest []string
	for _, docset := range docsets {
		dir := filepath.Join(docs, docset)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "index.md"), []byte("# "+docset+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "_version.json"), []byte(`{"pages":{"success":1,"empty":0,"failed":0,"total":1}}`), 0o644); err != nil {
			t.Fatal(err)
		}
		manifest = append(manifest, `{"path":"`+docset+`/index.md"}`)
	}
	site := filepath.Join(docs, "site-references")
	if err := os.MkdirAll(site, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(site, "_catalog.md"), []byte("# Catalog\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(site, "_references.json"), []byte(`[]`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(site, "_version.json"), []byte(`{"pages":{"success":1,"empty":0,"failed":0,"total":1}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(site, "index.md"), []byte("# Site references\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest = append(manifest, `{"path":"site-references/index.md"}`)
	manifest = append(manifest, `{"path":"site-references/_catalog.md"}`)
	for _, project := range []struct {
		brand string
		name  string
	}{
		{"platform", "pub-sub-api"},
		{"platform", "graphql"},
		{"ai", "agentforce"},
		{"marketing", "marketing-cloud-ampscript"},
		{"platform", "sf-connect-amazon-rds"},
	} {
		if err := os.MkdirAll(filepath.Join(site, project.brand, project.name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(docs, "manifest.json"), []byte("["+strings.Join(manifest, ",")+"]"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docs, "search-index.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCompatSurfaceStrictCurrentBasePrintsJSON(t *testing.T) {
	root := t.TempDir()
	ledger := filepath.Join(root, "ledger.json")
	// One strict-closed supported+evidenced row, one open missing-shape row.
	data := `{
  "schemaVersion": 1,
  "rows": [
    {"surfaceId":"apex:System.Done.doIt","product":"apex","area":"runtime","kind":"method","namespace":"System","typeName":"Done","memberName":"doIt","signature":"String doIt()","returnType":"String","gladeReturnType":"String","gladeShape":"signature-known","gladeBehavior":"supported","evidence":"fixture"},
    {"surfaceId":"apex:System.Missing.kind","product":"apex","area":"runtime","kind":"method","docs":"present","gladeShape":"absent","priority":10}
  ]
}`
	if err := os.WriteFile(ledger, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"compat", "surface", "strict-current-base", "--ledger", ledger, "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d stderr=%q stdout=%q", code, stderr.String(), stdout.String())
	}

	var decoded struct {
		Total        int `json:"total"`
		StrictClosed int `json:"strictClosed"`
		StrictOpen   int `json:"strictOpen"`
		OpenRows     []struct {
			SurfaceID string   `json:"surfaceId"`
			Reasons   []string `json:"reasons"`
		} `json:"openRows"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("decode JSON: %v\n%s", err, stdout.String())
	}
	if decoded.Total != 2 || decoded.StrictClosed != 1 || decoded.StrictOpen != 1 {
		t.Fatalf("counts: total=%d closed=%d open=%d", decoded.Total, decoded.StrictClosed, decoded.StrictOpen)
	}
	if len(decoded.OpenRows) != 1 || decoded.OpenRows[0].SurfaceID != "apex:System.Missing.kind" {
		t.Fatalf("openRows: %#v", decoded.OpenRows)
	}
	if len(decoded.OpenRows[0].Reasons) == 0 || decoded.OpenRows[0].Reasons[0] != "missing-shape" {
		t.Fatalf("open reasons: %#v", decoded.OpenRows[0].Reasons)
	}
}

func TestCompatSurfaceStrictCurrentBaseWritesOutputAtomically(t *testing.T) {
	root := t.TempDir()
	ledger := filepath.Join(root, "ledger.json")
	out := filepath.Join(root, "nested", "strict-current-base.json")
	data := `{
  "schemaVersion": 1,
  "rows": [
    {"surfaceId":"apex:System.Done.doIt","product":"apex","area":"runtime","kind":"method","namespace":"System","typeName":"Done","memberName":"doIt","signature":"String doIt()","returnType":"String","gladeReturnType":"String","gladeShape":"signature-known","gladeBehavior":"supported","evidence":"fixture"}
  ]
}`
	if err := os.WriteFile(ledger, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"compat", "surface", "strict-current-base", "--ledger", ledger, "--output", out}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d stderr=%q stdout=%q", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "surface strict-current-base: ") {
		t.Fatalf("summary missing: %q", stdout.String())
	}
	written, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	var decoded struct {
		Total        int `json:"total"`
		StrictClosed int `json:"strictClosed"`
	}
	if err := json.Unmarshal(written, &decoded); err != nil {
		t.Fatalf("decode output: %v\n%s", err, written)
	}
	if decoded.Total != 1 || decoded.StrictClosed != 1 {
		t.Fatalf("counts: total=%d closed=%d", decoded.Total, decoded.StrictClosed)
	}
}

func TestCompatSurfaceStrictCurrentBaseRejectsOutputAndJSON(t *testing.T) {
	root := t.TempDir()
	ledger := filepath.Join(root, "ledger.json")
	if err := os.WriteFile(ledger, []byte(`{"schemaVersion":1,"rows":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"compat", "surface", "strict-current-base", "--ledger", ledger, "--output", filepath.Join(root, "out.json"), "--json"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected failure stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "use only one of --output or --json") {
		t.Fatalf("missing mutual-exclusion error: %q", stderr.String())
	}
}

func TestCompatSurfaceStrictCurrentBaseRequiresLedger(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"compat", "surface", "strict-current-base", "--json"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected failure stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "--ledger is required") {
		t.Fatalf("missing ledger-required error: %q", stderr.String())
	}
}

func TestCompatSurfaceSupportProfileJSONOutput(t *testing.T) {
	root := t.TempDir()
	ledger := filepath.Join(root, "ledger.json")
	policy := filepath.Join(root, "policy.json")

	ledgerData := `{
  "schemaVersion": 1,
  "rows": [
    {"surfaceId":"apex:System.String","product":"apex","area":"runtime","namespace":"System","typeName":"String","kind":"type","gladeShape":"type-known","gladeBehavior":"supported","evidence":"fixture"},
    {"surfaceId":"apex:Messaging.Email","product":"apex","area":"runtime","namespace":"Messaging","typeName":"Email","kind":"type","gladeShape":"type-known","gladeBehavior":"supported","evidence":"fixture"}
  ]
}`
	policyData := `{
  "rules": [
    {"namespace":"System","disposition":"local-runtime-required","reason":"system runtime"},
    {"namespace":"Messaging","disposition":"deterministic-mock-required","reason":"messaging mock"}
  ]
}`
	if err := os.WriteFile(ledger, []byte(ledgerData), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(policy, []byte(policyData), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"compat", "surface", "support-profile", "--ledger", ledger, "--policy", policy, "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d stderr=%q stdout=%q", code, stderr.String(), stdout.String())
	}

	var decoded struct {
		Total int `json:"total"`
		Rows  []struct {
			SurfaceID   string `json:"surfaceId"`
			Disposition string `json:"disposition"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("decode JSON: %v\n%s", err, stdout.String())
	}
	if decoded.Total != 2 {
		t.Fatalf("total: want 2 got %d", decoded.Total)
	}
	dispByID := map[string]string{}
	for _, r := range decoded.Rows {
		dispByID[r.SurfaceID] = r.Disposition
	}
	if dispByID["apex:System.String"] != "local-runtime-required" {
		t.Fatalf("System.String disposition: got %q", dispByID["apex:System.String"])
	}
	if dispByID["apex:Messaging.Email"] != "deterministic-mock-required" {
		t.Fatalf("Messaging.Email disposition: got %q", dispByID["apex:Messaging.Email"])
	}
}

func TestCompatSurfaceSupportProfileMarkdownOutput(t *testing.T) {
	root := t.TempDir()
	ledger := filepath.Join(root, "ledger.json")
	policy := filepath.Join(root, "policy.json")

	ledgerData := `{
  "schemaVersion": 1,
  "rows": [
    {"surfaceId":"apex:System.String","product":"apex","area":"runtime","namespace":"System","typeName":"String","kind":"type","gladeShape":"type-known","gladeBehavior":"supported","evidence":"fixture"}
  ]
}`
	policyData := `{
  "rules": [
    {"namespace":"System","disposition":"local-runtime-required","reason":"system runtime"}
  ]
}`
	if err := os.WriteFile(ledger, []byte(ledgerData), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(policy, []byte(policyData), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"compat", "surface", "support-profile", "--ledger", ledger, "--policy", policy, "--markdown"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d stderr=%q stdout=%q", code, stderr.String(), stdout.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"# Apex Local Support Profile",
		"local-runtime-required",
		"Total Apex rows: 1",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("Markdown missing %q:\n%s", want, out)
		}
	}
}

func TestCompatSurfaceSupportProfileFailsOnUnclassified(t *testing.T) {
	root := t.TempDir()
	ledger := filepath.Join(root, "ledger.json")
	policy := filepath.Join(root, "policy.json")

	ledgerData := `{
  "schemaVersion": 1,
  "rows": [
    {"surfaceId":"apex:UnknownNS.SomeType","product":"apex","area":"runtime","namespace":"UnknownNS","typeName":"SomeType","kind":"type","gladeShape":"type-known","gladeBehavior":"supported","evidence":"fixture"}
  ]
}`
	policyData := `{
  "rules": [
    {"namespace":"System","disposition":"local-runtime-required","reason":"system runtime"}
  ]
}`
	if err := os.WriteFile(ledger, []byte(ledgerData), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(policy, []byte(policyData), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"compat", "surface", "support-profile", "--ledger", ledger, "--policy", policy, "--json"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected failure for unclassified row, got stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "support profile validation failed") {
		t.Fatalf("missing validation failure: %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "apex:UnknownNS.SomeType") {
		t.Fatalf("missing unclassified row ID: %q", stderr.String())
	}
}

func TestCompatSurfaceSupportProfileFailsOnOverlappingRules(t *testing.T) {
	root := t.TempDir()
	ledger := filepath.Join(root, "ledger.json")
	policy := filepath.Join(root, "policy.json")

	ledgerData := `{
  "schemaVersion": 1,
  "rows": [
    {"surfaceId":"apex:System.String","product":"apex","area":"runtime","namespace":"System","typeName":"String","kind":"type","gladeShape":"type-known","gladeBehavior":"supported","evidence":"fixture"}
  ]
}`
	policyData := `{
  "rules": [
    {"namespace":"System","disposition":"local-runtime-required","reason":"system runtime"},
    {"namespace":"System","disposition":"hosted-deferred","reason":"overlapping"}
  ]
}`
	if err := os.WriteFile(ledger, []byte(ledgerData), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(policy, []byte(policyData), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"compat", "surface", "support-profile", "--ledger", ledger, "--policy", policy, "--json"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected failure for overlapping rules, got stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "support profile validation failed") {
		t.Fatalf("missing validation failure: %q", stderr.String())
	}
}

func TestCompatSurfaceSupportProfileFailsOnStaleException(t *testing.T) {
	root := t.TempDir()
	ledger := filepath.Join(root, "ledger.json")
	policy := filepath.Join(root, "policy.json")

	ledgerData := `{
  "schemaVersion": 1,
  "rows": [
    {"surfaceId":"apex:ConnectApi.SomeDTO","product":"apex","area":"runtime","namespace":"ConnectApi","typeName":"SomeDTO","kind":"type","gladeShape":"type-known","gladeBehavior":"supported","evidence":"fixture"}
  ]
}`
	policyData := `{
  "rules": [
    {
      "namespace":"ConnectApi",
      "disposition":"hosted-deferred",
      "reason":"connect-api deferred",
      "memberExceptions": [
        {"typeName":"NonexistentType","memberName":"noSuchMethod","disposition":"deterministic-mock-required","reason":"stale"}
      ]
    }
  ]
}`
	if err := os.WriteFile(ledger, []byte(ledgerData), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(policy, []byte(policyData), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"compat", "surface", "support-profile", "--ledger", ledger, "--policy", policy, "--json"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected failure for stale exception, got stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "stale member exception") {
		t.Fatalf("missing stale exception error: %q", stderr.String())
	}
}

func TestCompatSurfaceSupportProfileRequiresLedger(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"compat", "surface", "support-profile", "--policy", "policy.json", "--json"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected failure stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "--ledger is required") {
		t.Fatalf("missing ledger-required error: %q", stderr.String())
	}
}

func TestCompatSurfaceSupportProfileRequiresPolicy(t *testing.T) {
	root := t.TempDir()
	ledger := filepath.Join(root, "ledger.json")
	if err := os.WriteFile(ledger, []byte(`{"schemaVersion":1,"rows":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"compat", "surface", "support-profile", "--ledger", ledger, "--json"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected failure stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "--policy is required") {
		t.Fatalf("missing policy-required error: %q", stderr.String())
	}
}

func TestCompatSurfaceSupportProfileRejectsJSONAndMarkdown(t *testing.T) {
	root := t.TempDir()
	ledger := filepath.Join(root, "ledger.json")
	policy := filepath.Join(root, "policy.json")
	if err := os.WriteFile(ledger, []byte(`{"schemaVersion":1,"rows":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(policy, []byte(`{"rules":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"compat", "surface", "support-profile", "--ledger", ledger, "--policy", policy, "--json", "--markdown"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected failure stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "use only one of --json or --markdown") {
		t.Fatalf("missing mutual-exclusion error: %q", stderr.String())
	}
}

func TestCompatSurfaceSupportProfileWritesOutputAtomically(t *testing.T) {
	root := t.TempDir()
	ledger := filepath.Join(root, "ledger.json")
	policy := filepath.Join(root, "policy.json")
	out := filepath.Join(root, "nested", "support-profile.json")

	ledgerData := `{
  "schemaVersion": 1,
  "rows": [
    {"surfaceId":"apex:System.String","product":"apex","area":"runtime","namespace":"System","typeName":"String","kind":"type","gladeShape":"type-known","gladeBehavior":"supported","evidence":"fixture"}
  ]
}`
	policyData := `{
  "rules": [
    {"namespace":"System","disposition":"local-runtime-required","reason":"system runtime"}
  ]
}`
	if err := os.WriteFile(ledger, []byte(ledgerData), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(policy, []byte(policyData), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"compat", "surface", "support-profile", "--ledger", ledger, "--policy", policy, "--output", out}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d stderr=%q stdout=%q", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "surface support-profile: ") {
		t.Fatalf("summary missing: %q", stdout.String())
	}
	written, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	var decoded struct {
		Total int `json:"total"`
	}
	if err := json.Unmarshal(written, &decoded); err != nil {
		t.Fatalf("decode output: %v\n%s", err, written)
	}
	if decoded.Total != 1 {
		t.Fatalf("total: want 1 got %d", decoded.Total)
	}
}

func TestCompatSurfaceSupportProfileNonApexRowsIgnored(t *testing.T) {
	root := t.TempDir()
	ledger := filepath.Join(root, "ledger.json")
	policy := filepath.Join(root, "policy.json")

	ledgerData := `{
  "schemaVersion": 1,
  "rows": [
    {"surfaceId":"apex:System.String","product":"apex","area":"runtime","namespace":"System","typeName":"String","kind":"type","gladeShape":"type-known","gladeBehavior":"supported","evidence":"fixture"},
    {"surfaceId":"rest:/services/data","product":"rest","area":"server","kind":"resource","docs":"present"},
    {"surfaceId":"lwc:lightning-button","product":"lwc","area":"ui","kind":"module"}
  ]
}`
	policyData := `{
  "rules": [
    {"namespace":"System","disposition":"local-runtime-required","reason":"system runtime"}
  ]
}`
	if err := os.WriteFile(ledger, []byte(ledgerData), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(policy, []byte(policyData), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"compat", "surface", "support-profile", "--ledger", ledger, "--policy", policy, "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d stderr=%q stdout=%q", code, stderr.String(), stdout.String())
	}

	var decoded struct {
		Total int `json:"total"`
		Rows  []struct {
			SurfaceID string `json:"surfaceId"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("decode JSON: %v\n%s", err, stdout.String())
	}
	if decoded.Total != 1 {
		t.Fatalf("total: want 1 (apex only) got %d", decoded.Total)
	}
	if len(decoded.Rows) != 1 {
		t.Fatalf("rows: want 1 got %d", len(decoded.Rows))
	}
	if decoded.Rows[0].SurfaceID != "apex:System.String" {
		t.Fatalf("expected apex row, got %q", decoded.Rows[0].SurfaceID)
	}
}

// --- corpus-usage CLI tests ---

func TestCompatSurfaceCorpusUsageRequiresLedger(t *testing.T) {
	root := t.TempDir()

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"compat", "surface", "corpus-usage", "--public-root", root, "--output", filepath.Join(root, "out.json")}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected failure stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "--ledger") {
		t.Fatalf("missing ledger-required error: %q", stderr.String())
	}
}

func TestCompatSurfaceCorpusUsageRequiresOutput(t *testing.T) {
	root := t.TempDir()
	ledger := filepath.Join(root, "ledger.json")
	if err := os.WriteFile(ledger, []byte(`{"schemaVersion":1,"rows":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"compat", "surface", "corpus-usage", "--ledger", ledger, "--public-root", root}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected failure stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "--output") {
		t.Fatalf("missing output-required error: %q", stderr.String())
	}
}

func TestCompatSurfaceCorpusUsageWritesJSON(t *testing.T) {
	root := t.TempDir()
	pubRoot := filepath.Join(root, "public")
	ledger := filepath.Join(root, "ledger.json")
	out := filepath.Join(root, "out.json")

	// Create a ledger with one namespace.
	ledgerData := `{
  "schemaVersion": 1,
  "rows": [
    {"surfaceId":"apex:System.String","product":"apex","area":"runtime","namespace":"System","typeName":"String","kind":"type","gladeShape":"type-known","gladeBehavior":"supported","evidence":"fixture"}
  ]
}`
	if err := os.WriteFile(ledger, []byte(ledgerData), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create a public Apex project.
	projDir := filepath.Join(pubRoot, "myproj")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projDir, "MyClass.cls"), []byte(`public class MyClass { public void m() { System.debug('hi'); } }`), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"compat", "surface", "corpus-usage", "--ledger", ledger, "--public-root", pubRoot, "--output", out}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d stderr=%q stdout=%q", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "surface corpus-usage:") {
		t.Fatalf("summary missing: %q", stdout.String())
	}

	// Verify output file exists and is valid JSON.
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	var decoded CorpusUsageValidation
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("decode output: %v\n%s", err, data)
	}
	if len(decoded.Usage) == 0 {
		t.Fatalf("usage must not be empty")
	}
	if decoded.PublicRootSHA256 == "" {
		t.Fatalf("publicRootSha256 must be populated")
	}
}

// CorpusUsageValidation matches the output structure for validation.
type CorpusUsageValidation struct {
	PublicRootSHA256     string `json:"publicRootSha256,omitempty"`
	PublicFailRootSHA256 string `json:"publicFailRootSha256,omitempty"`
	PrivateRootSHA256    string `json:"privateRootSha256,omitempty"`
	Usage                []struct {
		UsageKey  string `json:"usageKey"`
		Namespace string `json:"namespace"`
	} `json:"usage"`
}

// --- support-profile with --corpus-usage tests ---

func TestCompatSurfaceSupportProfileWithCorpusUsage(t *testing.T) {
	root := t.TempDir()
	ledger := filepath.Join(root, "ledger.json")
	policy := filepath.Join(root, "policy.json")
	cuPath := filepath.Join(root, "corpus-usage.json")

	ledgerData := `{
  "schemaVersion": 1,
  "rows": [
    {"surfaceId":"apex:System.String","product":"apex","area":"runtime","namespace":"System","typeName":"String","kind":"type","gladeShape":"type-known","gladeBehavior":"supported","evidence":"fixture"}
  ]
}`
	policyData := `{
  "rules": [
    {"namespace":"System","disposition":"local-runtime-required","reason":"system runtime"}
  ]
}`
	cuData := `{
  "publicRootSha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "usage": [
    {"usageKey":"System.String","namespace":"System","typeName":"String","pubProdRefs":42}
  ]
}`
	if err := os.WriteFile(ledger, []byte(ledgerData), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(policy, []byte(policyData), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cuPath, []byte(cuData), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"compat", "surface", "support-profile", "--ledger", ledger, "--policy", policy, "--corpus-usage", cuPath, "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d stderr=%q stdout=%q", code, stderr.String(), stdout.String())
	}

	var decoded struct {
		Total       int `json:"total"`
		CorpusUsage []struct {
			UsageKey string `json:"usageKey"`
		} `json:"corpusUsage"`
		Rows []struct {
			SurfaceID string `json:"surfaceId"`
			UsageKey  string `json:"usageKey"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("decode JSON: %v\n%s", err, stdout.String())
	}
	if decoded.Total != 1 {
		t.Fatalf("total: want 1 got %d", decoded.Total)
	}
	if decoded.Rows[0].UsageKey != "System.String" {
		t.Fatalf("UsageKey: want System.String got %q", decoded.Rows[0].UsageKey)
	}
	if len(decoded.CorpusUsage) != 1 {
		t.Fatalf("corpusUsage entries: want 1 got %d", len(decoded.CorpusUsage))
	}
}

// 9. an invalid support profile is written atomically by --output but the CLI
//    still exits nonzero.
func TestCompatSurfaceSupportProfileAtomicWriteOnInvalid(t *testing.T) {
	root := t.TempDir()
	ledger := filepath.Join(root, "ledger.json")
	policy := filepath.Join(root, "policy.json")
	out := filepath.Join(root, "nested", "profile.json")

	// Ledger has an unclassified row.
	ledgerData := `{
  "schemaVersion": 1,
  "rows": [
    {"surfaceId":"apex:UnknownNS.SomeType","product":"apex","area":"runtime","namespace":"UnknownNS","typeName":"SomeType","kind":"type","gladeShape":"type-known","gladeBehavior":"supported","evidence":"fixture"}
  ]
}`
	policyData := `{
  "rules": [
    {"namespace":"System","disposition":"local-runtime-required","reason":"system runtime"}
  ]
}`
	if err := os.WriteFile(ledger, []byte(ledgerData), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(policy, []byte(policyData), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"compat", "surface", "support-profile", "--ledger", ledger, "--policy", policy, "--output", out}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected nonzero exit for invalid profile, got stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "support profile validation failed") {
		t.Fatalf("missing validation failure: %q", stderr.String())
	}

	// The output file must exist and contain the invalid profile.
	written, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("output file must exist: %v", err)
	}
	var decoded struct {
		Total            int `json:"total"`
		UnclassifiedRows []struct {
			SurfaceID string `json:"surfaceId"`
		} `json:"unclassifiedRows"`
		ValidationErrors []string `json:"validationErrors"`
	}
	if err := json.Unmarshal(written, &decoded); err != nil {
		t.Fatalf("decode output: %v\n%s", err, written)
	}
	if decoded.Total != 1 {
		t.Fatalf("total: want 1 got %d", decoded.Total)
	}
	if len(decoded.UnclassifiedRows) != 1 {
		t.Fatalf("unclassified rows: want 1 got %d", len(decoded.UnclassifiedRows))
	}
	if decoded.UnclassifiedRows[0].SurfaceID != "apex:UnknownNS.SomeType" {
		t.Fatalf("unclassified row: got %q", decoded.UnclassifiedRows[0].SurfaceID)
	}
	if len(decoded.ValidationErrors) == 0 {
		t.Fatalf("validation errors must be present")
	}
}
