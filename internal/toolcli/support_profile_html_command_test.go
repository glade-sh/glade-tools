package toolcli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/surfaceledger"
)

func TestCompatSurfaceSupportProfileWritesJSONAndHTMLTogether(t *testing.T) {
	root := t.TempDir()
	ledger, policy := writeSupportProfileCLIInputs(t, root, false)
	jsonPath := filepath.Join(root, "nested", "apex-support-profile.json")
	htmlPath := filepath.Join(root, "nested", "apex-surface-status.html")

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{
		"surface", "support-profile",
		"--ledger", ledger,
		"--policy", policy,
		"--output", jsonPath,
		"--html-output", htmlPath,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d stderr=%q stdout=%q", code, stderr.String(), stdout.String())
	}
	for _, path := range []string{jsonPath, htmlPath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("output %s missing: %v", path, err)
		}
	}

	var profile struct {
		Total         int
		ByDisposition map[string]int
		ByGapClass    map[string]int
		Inputs        struct {
			Files []struct {
				Name   string
				Path   string
				SHA256 string
			}
		}
	}
	profileData, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(profileData, &profile); err != nil {
		t.Fatalf("decode profile JSON: %v", err)
	}
	var page struct {
		Total         int
		ByDisposition map[string]int
		ByGapClass    map[string]int
		Rows          []struct {
			SurfaceID string
		}
	}
	pageData := extractPageDataForCLI(t, readFile(t, htmlPath))
	if err := json.Unmarshal([]byte(pageData), &page); err != nil {
		t.Fatalf("decode HTML page data: %v", err)
	}
	if page.Total != profile.Total || len(page.Rows) != profile.Total {
		t.Fatalf("HTML total/rows = %d/%d, JSON total = %d", page.Total, len(page.Rows), profile.Total)
	}
	wantDisposition := map[string]int{
		string(surfaceledger.DispositionLocalRuntimeRequired):      profile.ByDisposition[string(surfaceledger.DispositionLocalRuntimeRequired)],
		string(surfaceledger.DispositionDeterministicMockRequired): profile.ByDisposition[string(surfaceledger.DispositionDeterministicMockRequired)],
		string(surfaceledger.DispositionCompileShapeRequired):      profile.ByDisposition[string(surfaceledger.DispositionCompileShapeRequired)],
		string(surfaceledger.DispositionHostedDeferred):            profile.ByDisposition[string(surfaceledger.DispositionHostedDeferred)],
	}
	if !reflect.DeepEqual(page.ByDisposition, wantDisposition) {
		t.Fatalf("HTML dispositions = %#v, want %#v", page.ByDisposition, wantDisposition)
	}
	if !reflect.DeepEqual(page.ByGapClass, profile.ByGapClass) {
		t.Fatalf("HTML gaps = %#v, JSON gaps = %#v", page.ByGapClass, profile.ByGapClass)
	}
	if len(profile.Inputs.Files) != 2 || profile.Inputs.Files[0].Path == "" || len(profile.Inputs.Files[0].SHA256) != 64 ||
		profile.Inputs.Files[1].Path == "" || len(profile.Inputs.Files[1].SHA256) != 64 {
		t.Fatalf("profile input provenance = %#v", profile.Inputs.Files)
	}
}

func TestCompatSurfaceSupportProfileRejectsInvalidHTMLOutputCombinations(t *testing.T) {
	cases := []struct {
		name string
		args func(ledger, policy string) []string
	}{
		{
			name: "html requires json output",
			args: func(ledger, policy string) []string {
				return []string{"surface", "support-profile", "--ledger", ledger, "--policy", policy, "--html-output", "status.html"}
			},
		},
		{
			name: "json stdout conflicts with output",
			args: func(ledger, policy string) []string {
				return []string{"surface", "support-profile", "--ledger", ledger, "--policy", policy, "--output", "profile.json", "--json"}
			},
		},
		{
			name: "json stdout conflicts with html",
			args: func(ledger, policy string) []string {
				return []string{"surface", "support-profile", "--ledger", ledger, "--policy", policy, "--html-output", "status.html", "--json"}
			},
		},
		{
			name: "same output path",
			args: func(ledger, policy string) []string {
				return []string{"surface", "support-profile", "--ledger", ledger, "--policy", policy, "--output", "same.out", "--html-output", "same.out"}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			ledger, policy := writeSupportProfileCLIInputs(t, root, false)
			var stdout, stderr bytes.Buffer
			if code := Run(context.Background(), tc.args(ledger, policy), &stdout, &stderr); code == 0 {
				t.Fatalf("expected rejection stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
			entries, err := os.ReadDir(root)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 2 {
				t.Fatalf("invalid flags created output entries: %#v", entries)
			}
		})
	}
}

func TestCompatSurfaceSupportProfileWritesHTMLBeforeValidationErrorExit(t *testing.T) {
	root := t.TempDir()
	ledger, policy := writeSupportProfileCLIInputs(t, root, true)
	jsonPath := filepath.Join(root, "nested", "profile.json")
	htmlPath := filepath.Join(root, "nested", "status.html")

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{
		"surface", "support-profile",
		"--ledger", ledger,
		"--policy", policy,
		"--output", jsonPath,
		"--html-output", htmlPath,
	}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected validation error stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "support profile validation failed") {
		t.Fatalf("missing validation diagnostic: %q", stderr.String())
	}
	if _, err := os.Stat(jsonPath); err != nil {
		t.Fatalf("JSON was not written before validation exit: %v", err)
	}
	if _, err := os.Stat(htmlPath); err != nil {
		t.Fatalf("HTML was not written before validation exit: %v", err)
	}

	var profile struct {
		Total            int
		ValidationErrors []string
	}
	if err := json.Unmarshal(readFile(t, jsonPath), &profile); err != nil {
		t.Fatalf("decode invalid profile: %v", err)
	}
	if profile.Total != 2 || len(profile.ValidationErrors) == 0 {
		t.Fatalf("invalid profile output = %#v", profile)
	}
	var page struct {
		Total int
		Rows  []struct {
			SurfaceID string
			Open      bool
		}
	}
	if err := json.Unmarshal([]byte(extractPageDataForCLI(t, readFile(t, htmlPath))), &page); err != nil {
		t.Fatalf("decode invalid HTML page data: %v", err)
	}
	openFound := false
	for _, row := range page.Rows {
		if row.SurfaceID == "apex:Unknown.SomeType" && row.Open {
			openFound = true
		}
	}
	if page.Total != 2 || len(page.Rows) != 2 || !openFound {
		t.Fatalf("invalid HTML page = %#v", page)
	}
}

func writeSupportProfileCLIInputs(t *testing.T, root string, invalid bool) (string, string) {
	t.Helper()
	rows := []surfaceledger.SurfaceLedgerRow{{
		SurfaceID:     "apex:System.String",
		Product:       surfaceledger.ProductApex,
		Area:          surfaceledger.AreaRuntime,
		Namespace:     "System",
		TypeName:      "String",
		Kind:          surfaceledger.KindType,
		Docs:          surfaceledger.SourcePresent,
		Org:           surfaceledger.SourcePresent,
		GladeShape:    surfaceledger.ShapeTypeKnown,
		GladeBehavior: surfaceledger.BehaviorSupported,
		Evidence:      surfaceledger.EvidenceFixtureAndOracle,
		Sources:       []string{"fixture:system-string"},
	}}
	policy := surfaceledger.SupportPolicy{Rules: []surfaceledger.SupportPolicyRule{{
		Namespace:   "System",
		Disposition: surfaceledger.DispositionLocalRuntimeRequired,
		Reason:      "system runtime",
	}}}
	if invalid {
		rows = append(rows, surfaceledger.SurfaceLedgerRow{
			SurfaceID:     "apex:Unknown.SomeType",
			Product:       surfaceledger.ProductApex,
			Area:          surfaceledger.AreaRuntime,
			Namespace:     "Unknown",
			TypeName:      "SomeType",
			Kind:          surfaceledger.KindType,
			GladeShape:    surfaceledger.ShapeTypeKnown,
			GladeBehavior: surfaceledger.BehaviorSupported,
			Evidence:      surfaceledger.EvidenceFixture,
		})
	}
	ledgerData, err := json.Marshal(surfaceledger.SurfaceLedger{SchemaVersion: surfaceledger.SchemaVersion, Rows: rows})
	if err != nil {
		t.Fatal(err)
	}
	policyData, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	ledgerPath := filepath.Join(root, "ledger.json")
	policyPath := filepath.Join(root, "policy.json")
	if err := os.WriteFile(ledgerPath, ledgerData, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(policyPath, policyData, 0o644); err != nil {
		t.Fatal(err)
	}
	return ledgerPath, policyPath
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func extractPageDataForCLI(t *testing.T, html []byte) string {
	t.Helper()
	const open = "<script id=\"page-data\" type=\"application/json\">"
	text := string(html)
	start := strings.Index(text, open)
	if start < 0 {
		t.Fatal("missing page-data script")
	}
	start += len(open)
	end := strings.Index(text[start:], "</script>")
	if end < 0 {
		t.Fatal("missing page-data closing tag")
	}
	return text[start : start+end]
}
