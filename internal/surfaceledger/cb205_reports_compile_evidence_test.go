package surfaceledger

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const (
	cb205ReportsFixturePath    = "docs/fixtures/current-base-reports-compile-positive-api67.json"
	cb205ReportsComparisonPath = "docs/fixtures/salesforce-reports-compile-comparisons.json"
	cb205ReportsProfilePath    = "evidence/current-base/profile-sitefix-final/apex-support-profile.json"
	cb205ReportsProfileSHA256  = "137beb73af4db348ed2f60c2f4e464306922a77beddd5c737da5949fe8c19c55"
	cb205ReportsCandidatePath  = "evidence/glade-candidate-sitefix"
	cb205ReportsCandidateSHA   = "ab3b86d0fb96c92068de7ba82d56378e6ebac468ebe5129813c600a7e832607a"
)

type cb205ReportsFixture struct {
	Name       string `json:"name"`
	PacketID   string `json:"packetId"`
	APIVersion string `json:"apiVersion"`
	Mode       string `json:"mode"`
	Candidate  struct {
		Commit string `json:"commit"`
		SHA256 string `json:"sha256"`
	} `json:"candidate"`
	Profile struct {
		Path             string `json:"path"`
		SHA256           string `json:"sha256"`
		Selection        string `json:"selection"`
		Namespace        string `json:"namespace"`
		SelectedRowCount int    `json:"selectedRowCount"`
	} `json:"profile"`
	Replay struct {
		Root               string   `json:"root"`
		CandidateCheckPath string   `json:"candidateCheckPath"`
		SalesforceDeploy   string   `json:"salesforceDeployPath"`
		SalesforceDelete   string   `json:"salesforceDeletePath"`
		SourcePaths        []string `json:"sourcePaths"`
		SourceSHA256       []string `json:"sourceSHA256"`
	} `json:"replay"`
	Evidence []struct {
		SurfaceID string `json:"surfaceId"`
		Symbol    string `json:"symbol"`
		Kind      string `json:"kind"`
	} `json:"evidence"`
}

type cb205ReportsComparison struct {
	Name        string `json:"name"`
	PacketID    string `json:"packetId"`
	APIVersion  string `json:"apiVersion"`
	Mode        string `json:"mode"`
	Comparisons []struct {
		CaseID           string   `json:"caseId"`
		Status           string   `json:"status"`
		SurfaceIDs       []string `json:"surfaceIds"`
		SFObservation    string   `json:"sfObservation"`
		GladeObservation string   `json:"gladeObservation"`
	} `json:"comparisons"`
}

type cb205ReportsProfile struct {
	Rows []struct {
		SurfaceID string `json:"surfaceId"`
		Namespace string `json:"namespace"`
		GapClass  string `json:"gapClass"`
	} `json:"rows"`
}

func TestCB205ReportsCompileRowsHaveExactCurrentDualEvidence(t *testing.T) {
	toolsRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	evidenceRoot := cb205ReportsEvidenceRoot(t)
	fixturePath := filepath.Join(toolsRoot, cb205ReportsFixturePath)
	comparisonPath := filepath.Join(toolsRoot, cb205ReportsComparisonPath)

	var fixture cb205ReportsFixture
	var comparison cb205ReportsComparison
	cb205ReportsReadJSON(t, fixturePath, &fixture)
	cb205ReportsReadJSON(t, comparisonPath, &comparison)
	if fixture.Name != "current-base-reports-compile-positive-api67" || fixture.PacketID != "SF-CB205" || fixture.APIVersion != "67.0" || fixture.Mode != "compile-shape" {
		t.Fatalf("fixture metadata = %#v", fixture)
	}
	if comparison.Name != "salesforce-reports-compile-comparisons" || comparison.PacketID != fixture.PacketID || comparison.APIVersion != fixture.APIVersion || comparison.Mode != fixture.Mode {
		t.Fatalf("comparison metadata = %#v", comparison)
	}
	if fixture.Candidate.Commit != "98ef0103" || fixture.Candidate.SHA256 != cb205ReportsCandidateSHA {
		t.Fatalf("candidate identity = %#v", fixture.Candidate)
	}
	if fixture.Profile.Path != cb205ReportsProfilePath || fixture.Profile.SHA256 != cb205ReportsProfileSHA256 || fixture.Profile.Selection != "compile-shape-required" || fixture.Profile.Namespace != "reports" || fixture.Profile.SelectedRowCount != 584 {
		t.Fatalf("profile identity = %#v", fixture.Profile)
	}

	wantIDs := cb205ReportsCurrentGapIDs(t, filepath.Join(evidenceRoot, fixture.Profile.Path))
	if len(wantIDs) != 584 {
		t.Fatalf("current Reports gap rows = %d, want 584", len(wantIDs))
	}
	fixtureIDs := make([]string, 0, len(fixture.Evidence))
	for _, row := range fixture.Evidence {
		if row.Kind != "shape" || row.SurfaceID == "" || row.Symbol != strings.TrimPrefix(row.SurfaceID, "apex:") {
			t.Fatalf("fixture evidence row = %#v", row)
		}
		fixtureIDs = append(fixtureIDs, row.SurfaceID)
	}
	if !slices.Equal(cb205ReportsSorted(fixtureIDs), wantIDs) {
		t.Fatalf("fixture IDs do not exactly match current Reports profile rows")
	}
	if len(comparison.Comparisons) != 1 || comparison.Comparisons[0].CaseID != "reports-api67-compile-shape-current" || comparison.Comparisons[0].Status != "pass" || comparison.Comparisons[0].SFObservation == "" || comparison.Comparisons[0].GladeObservation == "" || !slices.Equal(cb205ReportsSorted(comparison.Comparisons[0].SurfaceIDs), wantIDs) {
		t.Fatalf("comparison = %#v", comparison.Comparisons)
	}

	cb205ReportsAssertSHA256(t, filepath.Join(evidenceRoot, cb205ReportsCandidatePath), cb205ReportsCandidateSHA)
	cb205ReportsAssertSHA256(t, filepath.Join(evidenceRoot, fixture.Profile.Path), cb205ReportsProfileSHA256)
	wantSourcePaths := []string{
		"evidence/current-base/cb199-reports-compile-shape-api67/sitefix-replay-20260803T/project/force-app/main/default/classes/CB205ReportsShard01.cls",
		"evidence/current-base/cb199-reports-compile-shape-api67/sitefix-replay-20260803T/project/force-app/main/default/classes/CB205ReportsShard02.cls",
		"evidence/current-base/cb199-reports-compile-shape-api67/sitefix-replay-20260803T/project/force-app/main/default/classes/CB205ReportsShard03.cls",
		"evidence/current-base/cb199-reports-compile-shape-api67/sitefix-replay-20260803T/project/force-app/main/default/classes/CB205ReportsShard04.cls",
		"evidence/current-base/cb199-reports-compile-shape-api67/sitefix-replay-20260803T/project/force-app/main/default/classes/CB205ReportsShard05.cls",
		"evidence/current-base/cb199-reports-compile-shape-api67/sitefix-replay-20260803T/project/force-app/main/default/classes/CB205ReportsShard06.cls",
		"evidence/current-base/cb199-reports-compile-shape-api67/sitefix-replay-20260803T/project/force-app/main/default/classes/CB205ReportsShard07.cls",
		"evidence/current-base/cb199-reports-compile-shape-api67/sitefix-replay-20260803T/project/force-app/main/default/classes/CB205ReportsShard08.cls",
		"evidence/current-base/cb199-reports-compile-shape-api67/sitefix-replay-20260803T/project/force-app/main/default/classes/CB205ReportsShard09.cls",
	}
	if !slices.Equal(fixture.Replay.SourcePaths, wantSourcePaths) || len(fixture.Replay.SourceSHA256) != len(fixture.Replay.SourcePaths) {
		t.Fatalf("source identity = %#v", fixture.Replay)
	}
	for index, sourcePath := range fixture.Replay.SourcePaths {
		cb205ReportsAssertSHA256(t, filepath.Join(evidenceRoot, sourcePath), fixture.Replay.SourceSHA256[index])
	}
	cb205ReportsAssertReplay(t, evidenceRoot, fixture.Replay)

	fixtureEvidence, err := BuildEvidenceSnapshot([]string{fixturePath})
	if err != nil {
		t.Fatal(err)
	}
	oracleEvidence, err := BuildOracleEvidenceSnapshot([]string{comparisonPath})
	if err != nil {
		t.Fatal(err)
	}
	assertExactSurfaceSet(t, fixtureEvidence, wantIDs)
	assertExactSurfaceSet(t, oracleEvidence, wantIDs)
	ledger := Merge(nil, nil, BuildGladeSnapshot(), append(fixtureEvidence, oracleEvidence...))
	bySurfaceKey := rowsBySurfaceKey(ledger.Rows)
	for _, id := range wantIDs {
		row, ok := bySurfaceKey[surfaceIDKey(id)]
		if !ok || row.Evidence != EvidenceFixtureAndOracle || row.GladeShape == ShapeAbsent {
			t.Fatalf("%s ledger evidence/shape/behavior = %#v", id, row)
		}
	}
}

func cb205ReportsEvidenceRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for range 5 {
		if _, err := os.Stat(filepath.Join(root, cb205ReportsProfilePath)); err == nil {
			return root
		}
		root = filepath.Dir(root)
	}
	t.Fatalf("find current-base evidence root")
	return ""
}

func cb205ReportsCurrentGapIDs(t *testing.T, path string) []string {
	t.Helper()
	var profile cb205ReportsProfile
	cb205ReportsReadJSON(t, path, &profile)
	var ids []string
	for _, row := range profile.Rows {
		if strings.EqualFold(row.Namespace, "reports") && row.GapClass == GapMissingEvidence {
			ids = append(ids, row.SurfaceID)
		}
	}
	return cb205ReportsSorted(ids)
}

func cb205ReportsAssertReplay(t *testing.T, root string, replay struct {
	Root               string   `json:"root"`
	CandidateCheckPath string   `json:"candidateCheckPath"`
	SalesforceDeploy   string   `json:"salesforceDeployPath"`
	SalesforceDelete   string   `json:"salesforceDeletePath"`
	SourcePaths        []string `json:"sourcePaths"`
	SourceSHA256       []string `json:"sourceSHA256"`
}) {
	t.Helper()
	var candidate struct {
		Status  string `json:"status"`
		Summary struct {
			Types       int `json:"types"`
			Diagnostics int `json:"diagnostics"`
		} `json:"summary"`
	}
	cb205ReportsReadJSON(t, filepath.Join(root, replay.CandidateCheckPath), &candidate)
	if candidate.Status != "passed" || candidate.Summary.Types != 9 || candidate.Summary.Diagnostics != 0 {
		t.Fatalf("candidate replay = %#v", candidate)
	}
	for _, path := range []string{replay.SalesforceDeploy, replay.SalesforceDelete} {
		var result struct {
			Status int `json:"status"`
			Result struct {
				Success                  bool `json:"success"`
				NumberComponentsDeployed int  `json:"numberComponentsDeployed"`
				NumberComponentErrors    int  `json:"numberComponentErrors"`
			} `json:"result"`
		}
		cb205ReportsReadJSON(t, filepath.Join(root, path), &result)
		if result.Status != 0 || !result.Result.Success || result.Result.NumberComponentsDeployed != 9 || result.Result.NumberComponentErrors != 0 {
			t.Fatalf("Salesforce replay %s = %#v", path, result)
		}
	}
}

func cb205ReportsReadJSON(t *testing.T, path string, target any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
}

func cb205ReportsAssertSHA256(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(data)); got != want {
		t.Fatalf("%s SHA-256 = %s, want %s", path, got, want)
	}
}

func cb205ReportsSorted(ids []string) []string {
	out := append([]string(nil), ids...)
	slices.Sort(out)
	return out
}
