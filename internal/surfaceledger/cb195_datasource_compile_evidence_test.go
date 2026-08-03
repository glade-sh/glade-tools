package surfaceledger

import (
	"bytes"
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
	cb195CandidateCommit      = "fd687589b6af3f11706011c67f15e694224f09d3"
	cb195CandidateSHA256      = "b4b4f6797a9d661699a0d2bf4662fe3c64d8b7ec9e9716c855704ecb89876d72"
	cb195ProfilePath          = "evidence/current-base/profile-fd687589-cb198-20260802-1632/apex-support-profile.json"
	cb195ProfileSHA256        = "bdb20b5ae8e253a29b1648153e83f3906f21aab00c1b7e7ab8e03457d6b9a811"
	cb195SnapshotPath         = "evidence/current-base/surface-fd687589-cb198-20260802-1632/GLADE_SNAPSHOT.json"
	cb195SnapshotSHA256       = "b38951af9b4a8eb18d33f3043a52a54c020787b714ac168f7074de871a4d2a58"
	cb195RowMapPath           = "evidence/current-base/cb195-datasource-compile-shape-api67/row-map.json"
	cb195ProjectPath          = "evidence/current-base/cb195-datasource-compile-shape-api67/project"
	cb195CandidateCheckPath   = "evidence/current-base/cb195-datasource-compile-shape-api67/retry-hub2-exec4/candidate-check.json"
	cb195DeployPath           = "evidence/current-base/cb195-datasource-compile-shape-api67/retry-hub2-exec4/deploy.json"
	cb195CreatePath           = "evidence/current-base/cb195-datasource-compile-shape-api67/retry-hub2-exec4/create.json"
	cb195DeletePath           = "evidence/current-base/cb195-datasource-compile-shape-api67/retry-hub2-exec4/delete.json"
	cb195PostDeletePath       = "evidence/current-base/cb195-datasource-compile-shape-api67/retry-hub2-exec4/post-delete-display.json"
	cb195OrgAlias             = "cb195-h2-e4-20260802171000"
	cb195DeploymentID         = "0Afcb00000ARGJ3CAP"
	cb195EnumsSourcePath      = "evidence/current-base/cb195-datasource-compile-shape-api67/project/force-app/main/default/classes/CB195DataSourceEnums.cls"
	cb195DtosSourcePath       = "evidence/current-base/cb195-datasource-compile-shape-api67/project/force-app/main/default/classes/CB195DataSourceDtos.cls"
	cb195ServicesSourcePath   = "evidence/current-base/cb195-datasource-compile-shape-api67/project/force-app/main/default/classes/CB195DataSourceServices.cls"
	cb195EnumsSourceSHA256    = "8ef64a796d408a8125911d504a7c668142fcf416ebedf6e4f11c43c19893f69c"
	cb195DtosSourceSHA256     = "af3e3f1928f9ad33c6b0ebd76dedbb5faab60a3141cfc22c9b5624715dc77c9b"
	cb195ServicesSourceSHA256 = "c4fca0f8eb7d98f1bbd27abc18069591a17160ad5d4ec4e96f9210e36bdfa17b"
)

type cb195Identity struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type cb195Candidate struct {
	Commit string `json:"commit"`
	SHA256 string `json:"sha256"`
}

type cb195Profile struct {
	Path             string   `json:"path"`
	SHA256           string   `json:"sha256"`
	Selection        string   `json:"selection"`
	Areas            []string `json:"areas"`
	SelectedRowCount int      `json:"selectedRowCount"`
}

type cb195Scenario struct {
	ProjectPath  string   `json:"projectPath"`
	SourcePaths  []string `json:"sourcePaths"`
	SourceSHA256 []string `json:"sourceSHA256"`
}

type cb195Replay struct {
	Path         string `json:"path"`
	CandidateSHA string `json:"candidateSHA256"`
	Status       string `json:"status"`
	Diagnostics  int    `json:"diagnostics"`
}

type cb195Cleanup struct {
	CreatePath     string `json:"createPath"`
	DeletePath     string `json:"deletePath"`
	PostDeletePath string `json:"postDeletePath"`
	OrgAlias       string `json:"orgAlias"`
	DeployID       string `json:"deployId"`
}

type cb195Retained struct {
	RowMapPath           string       `json:"rowMapPath"`
	ProjectPath          string       `json:"projectPath"`
	CandidateCheckPath   string       `json:"candidateCheckPath"`
	SalesforceDeployPath string       `json:"salesforceDeployPath"`
	Cleanup              cb195Cleanup `json:"cleanup"`
}

type cb195EvidenceRow struct {
	SurfaceID string `json:"surfaceId"`
	Symbol    string `json:"symbol"`
	Kind      string `json:"kind"`
}

type cb195Fixture struct {
	Name              string             `json:"name"`
	PacketID          string             `json:"packetId"`
	APIVersion        string             `json:"apiVersion"`
	Mode              string             `json:"mode"`
	Candidate         cb195Candidate     `json:"candidate"`
	Profile           cb195Profile       `json:"profile"`
	CanonicalSnapshot cb195Identity      `json:"canonicalSnapshot"`
	Scenario          cb195Scenario      `json:"scenario"`
	Replay            cb195Replay        `json:"replay"`
	Retained          cb195Retained      `json:"retained"`
	CreditScope       string             `json:"creditScope"`
	NotCredited       []string           `json:"notCredited"`
	Evidence          []cb195EvidenceRow `json:"evidence"`
}

type cb195Comparison struct {
	CaseID           string   `json:"caseId"`
	Status           string   `json:"status"`
	SurfaceIDs       []string `json:"surfaceIds"`
	SFObservation    string   `json:"sfObservation"`
	GladeObservation string   `json:"gladeObservation"`
}

type cb195Excluded struct {
	CaseID     string   `json:"caseId"`
	Status     string   `json:"status"`
	Credited   bool     `json:"credited"`
	SurfaceIDs []string `json:"surfaceIds"`
	Reason     string   `json:"reason"`
}

type cb195Salesforce struct {
	Execution        string `json:"execution"`
	APIVersion       string `json:"apiVersion"`
	ResultPath       string `json:"resultPath"`
	Result           string `json:"result"`
	DeploymentID     string `json:"deploymentId"`
	OrgAlias         string `json:"orgAlias"`
	CreateResultPath string `json:"createResultPath"`
	DeleteResultPath string `json:"deleteResultPath"`
	PostDeletePath   string `json:"postDeletePath"`
}

type cb195ComparisonEnvelope struct {
	Name              string            `json:"name"`
	PacketID          string            `json:"packetId"`
	APIVersion        string            `json:"apiVersion"`
	Mode              string            `json:"mode"`
	Candidate         cb195Candidate    `json:"candidate"`
	Profile           cb195Profile      `json:"profile"`
	CanonicalSnapshot cb195Identity     `json:"canonicalSnapshot"`
	Scenario          cb195Scenario     `json:"scenario"`
	Replay            cb195Replay       `json:"replay"`
	Retained          cb195Retained     `json:"retained"`
	CreditScope       string            `json:"creditScope"`
	NotCredited       []string          `json:"notCredited"`
	Salesforce        cb195Salesforce   `json:"salesforce"`
	Comparisons       []cb195Comparison `json:"comparisons"`
	Excluded          []cb195Excluded   `json:"excluded"`
	Mismatches        []any             `json:"mismatches"`
}

type cb195RowMap struct {
	Contract      string `json:"contract"`
	ProfilePath   string `json:"profilePath"`
	ProfileSHA256 string `json:"profileSHA256"`
	RowCount      int    `json:"rowCount"`
	Shards        struct {
		Enum     int `json:"enum"`
		DTO      int `json:"dto"`
		Abstract int `json:"abstract"`
	} `json:"shards"`
	Rows []struct {
		SurfaceID  string `json:"surfaceId"`
		ClassName  string `json:"className"`
		Category   string `json:"category"`
		RowOrdinal int    `json:"rowOrdinal"`
		Expression string `json:"expression"`
		SourceLine int    `json:"sourceLine"`
	} `json:"rows"`
}

type cb195CandidateCheck struct {
	Command  string `json:"command"`
	Status   string `json:"status"`
	ExitCode int    `json:"exitCode"`
	Project  struct {
		Root             string `json:"root"`
		SourceAPIVersion string `json:"sourceApiVersion"`
	} `json:"project"`
	Summary struct {
		Types       int `json:"types"`
		Diagnostics int `json:"diagnostics"`
	} `json:"summary"`
}

type cb195Deploy struct {
	Status int `json:"status"`
	Result struct {
		CheckOnly                bool   `json:"checkOnly"`
		ID                       string `json:"id"`
		Status                   string `json:"status"`
		Success                  bool   `json:"success"`
		NumberComponentsDeployed int    `json:"numberComponentsDeployed"`
		NumberComponentErrors    int    `json:"numberComponentErrors"`
		NumberTestErrors         int    `json:"numberTestErrors"`
		NumberTestsTotal         int    `json:"numberTestsTotal"`
		RunTestsEnabled          bool   `json:"runTestsEnabled"`
		Details                  struct {
			ComponentSuccesses []struct {
				FullName string `json:"fullName"`
				Success  bool   `json:"success"`
			} `json:"componentSuccesses"`
			RunTestResult struct {
				NumTestsRun int `json:"numTestsRun"`
			} `json:"runTestResult"`
			ComponentFailures []any `json:"componentFailures"`
		} `json:"details"`
	} `json:"result"`
}

type cb195Create struct {
	Status int `json:"status"`
	Result struct {
		Username string `json:"username"`
		OrgID    string `json:"orgId"`
		Auth     struct {
			InstanceAPIVersion string `json:"instanceApiVersion"`
		} `json:"authFields"`
	} `json:"result"`
}

type cb195Delete struct {
	Status int `json:"status"`
	Result struct {
		Username string `json:"username"`
		OrgID    string `json:"orgId"`
	} `json:"result"`
}

func TestCB195DataSourceCompileRowsHaveExactDualEvidence(t *testing.T) {
	root := filepath.Join("..", "..")
	evidenceRoot, err := filepath.Abs(filepath.Join(root, "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	fixturePath := filepath.Join(root, "docs", "fixtures", "current-base-cb195-datasource-compile-positive-api67.json")
	comparisonPath := filepath.Join(root, "docs", "fixtures", "salesforce-cb195-datasource-compile-comparisons.json")

	var fixture cb195Fixture
	var comparison cb195ComparisonEnvelope
	readCB195JSON(t, fixturePath, &fixture)
	readCB195JSON(t, comparisonPath, &comparison)
	if fixture.Name != "current-base-cb195-datasource-compile-positive-api67" || fixture.PacketID != "SF-CB195" || fixture.APIVersion != "67.0" || fixture.Mode != "compile-shape" {
		t.Fatalf("fixture metadata = %#v", fixture)
	}
	if comparison.Name != "salesforce-cb195-datasource-compile-comparisons" || comparison.PacketID != "SF-CB195" || comparison.APIVersion != "67.0" || comparison.Mode != "compile-shape" {
		t.Fatalf("comparison metadata = %#v", comparison)
	}
	if fixture.CreditScope != "DataSource compile shape only" || comparison.CreditScope != fixture.CreditScope || !slices.Equal(fixture.NotCredited, comparison.NotCredited) {
		t.Fatalf("credit boundary = fixture %#v comparison %#v", fixture.NotCredited, comparison.NotCredited)
	}
	for _, text := range []string{"LIMIT_EXCEEDED", "Hub-2", "runtime", "hosted"} {
		if !containsAny(strings.Join(fixture.NotCredited, " "), text) {
			t.Fatalf("fixture notCredited omits %q: %#v", text, fixture.NotCredited)
		}
	}
	if len(fixture.NotCredited) != 3 || len(comparison.NotCredited) != 3 {
		t.Fatalf("notCredited counts = %d/%d", len(fixture.NotCredited), len(comparison.NotCredited))
	}

	assertCB195Candidate(t, fixture.Candidate)
	assertCB195Candidate(t, comparison.Candidate)
	assertCB195Profile(t, fixture.Profile)
	assertCB195Profile(t, comparison.Profile)
	assertCB195Snapshot(t, fixture.CanonicalSnapshot)
	assertCB195Snapshot(t, comparison.CanonicalSnapshot)
	if fixture.Scenario.ProjectPath != comparison.Scenario.ProjectPath || !slices.Equal(fixture.Scenario.SourcePaths, comparison.Scenario.SourcePaths) || !slices.Equal(fixture.Scenario.SourceSHA256, comparison.Scenario.SourceSHA256) || fixture.Scenario.ProjectPath != cb195ProjectPath || !slices.Equal(fixture.Scenario.SourcePaths, []string{cb195EnumsSourcePath, cb195DtosSourcePath, cb195ServicesSourcePath}) || !slices.Equal(fixture.Scenario.SourceSHA256, []string{cb195EnumsSourceSHA256, cb195DtosSourceSHA256, cb195ServicesSourceSHA256}) {
		t.Fatalf("source identity = %#v / %#v", fixture.Scenario, comparison.Scenario)
	}
	if fixture.Replay != comparison.Replay || fixture.Retained != comparison.Retained {
		t.Fatalf("replay/retained identity differs")
	}
	if fixture.Replay.Path != cb195CandidateCheckPath || fixture.Replay.CandidateSHA != cb195CandidateSHA256 || fixture.Replay.Status != "passed" || fixture.Replay.Diagnostics != 0 {
		t.Fatalf("candidate check identity = %#v", fixture.Replay)
	}
	if fixture.Retained.RowMapPath != cb195RowMapPath || fixture.Retained.ProjectPath != cb195ProjectPath || fixture.Retained.CandidateCheckPath != cb195CandidateCheckPath || fixture.Retained.SalesforceDeployPath != cb195DeployPath || fixture.Retained.Cleanup != (cb195Cleanup{cb195CreatePath, cb195DeletePath, cb195PostDeletePath, cb195OrgAlias, cb195DeploymentID}) {
		t.Fatalf("retained identity = %#v", fixture.Retained)
	}
	if comparison.Salesforce.Execution != "compile-only-deploy" || comparison.Salesforce.APIVersion != "67.0" || comparison.Salesforce.ResultPath != cb195DeployPath || comparison.Salesforce.Result != "Pass" || comparison.Salesforce.DeploymentID != cb195DeploymentID || comparison.Salesforce.OrgAlias != cb195OrgAlias || comparison.Salesforce.CreateResultPath != cb195CreatePath || comparison.Salesforce.DeleteResultPath != cb195DeletePath || comparison.Salesforce.PostDeletePath != cb195PostDeletePath {
		t.Fatalf("Salesforce evidence = %#v", comparison.Salesforce)
	}

	var rowMap cb195RowMap
	readCB195JSON(t, filepath.Join(evidenceRoot, cb195RowMapPath), &rowMap)
	if rowMap.Contract != "DataSource API 67 compile-shape matrix" || rowMap.ProfilePath != cb195ProfilePath || rowMap.ProfileSHA256 != cb195ProfileSHA256 || rowMap.RowCount != 241 || len(rowMap.Rows) != 241 || rowMap.Shards.Enum != 107 || rowMap.Shards.DTO != 116 || rowMap.Shards.Abstract != 18 {
		t.Fatalf("row map metadata = %#v", rowMap)
	}
	wantIDs := make([]string, 0, len(rowMap.Rows))
	lastOrdinalByClass := map[string]int{}
	for index, row := range rowMap.Rows {
		if row.RowOrdinal != lastOrdinalByClass[row.ClassName]+1 || row.SurfaceID == "" || strings.ContainsAny(row.SurfaceID, "*?") || !strings.HasPrefix(row.SurfaceID, "apex:DataSource.") || row.Expression == "" || row.SourceLine <= 0 || row.ClassName == "" || row.Category == "" {
			t.Fatalf("row map row %d = %#v", index, row)
		}
		lastOrdinalByClass[row.ClassName] = row.RowOrdinal
		wantIDs = append(wantIDs, row.SurfaceID)
	}
	if len(uniqueCB195(wantIDs)) != 241 {
		t.Fatalf("row map IDs are not unique")
	}

	fixtureIDs := make([]string, 0, len(fixture.Evidence))
	for _, row := range fixture.Evidence {
		if row.Kind != "shape" || row.SurfaceID == "" || strings.ContainsAny(row.SurfaceID, "*?") || row.Symbol == "" || row.SurfaceID != "apex:"+row.Symbol {
			t.Fatalf("fixture evidence row = %#v", row)
		}
		fixtureIDs = append(fixtureIDs, row.SurfaceID)
	}
	if len(fixtureIDs) != 241 || !slices.Equal(sortedCB195IDs(fixtureIDs), sortedCB195IDs(wantIDs)) {
		t.Fatalf("fixture IDs do not exactly match 241-row map")
	}
	if len(comparison.Comparisons) != 1 || comparison.Comparisons[0].CaseID != "cb195-datasource-compile-shape-contract" || comparison.Comparisons[0].Status != "pass" || comparison.Comparisons[0].SFObservation == "" || comparison.Comparisons[0].GladeObservation == "" || !slices.Equal(sortedCB195IDs(comparison.Comparisons[0].SurfaceIDs), sortedCB195IDs(wantIDs)) {
		t.Fatalf("comparisons = %#v", comparison.Comparisons)
	}
	if len(comparison.Mismatches) != 0 || len(comparison.Excluded) != 2 {
		t.Fatalf("mismatches/excluded counts = %d/%d", len(comparison.Mismatches), len(comparison.Excluded))
	}
	assertCB195Excluded(t, comparison.Excluded)

	var check cb195CandidateCheck
	readCB195JSON(t, filepath.Join(evidenceRoot, cb195CandidateCheckPath), &check)
	if check.Command != "check" || check.Status != "passed" || check.ExitCode != 0 || check.Project.SourceAPIVersion != "67.0" || check.Summary.Types != 3 || check.Summary.Diagnostics != 0 {
		t.Fatalf("candidate check result = %#v", check)
	}
	if check.Project.Root != filepath.Join(evidenceRoot, cb195ProjectPath) {
		t.Fatalf("candidate check project root = %q, want %q", check.Project.Root, filepath.Join(evidenceRoot, cb195ProjectPath))
	}

	var deploy cb195Deploy
	readCB195JSON(t, filepath.Join(evidenceRoot, cb195DeployPath), &deploy)
	if deploy.Status != 0 || deploy.Result.CheckOnly || deploy.Result.ID != cb195DeploymentID || deploy.Result.Status != "Succeeded" || !deploy.Result.Success || deploy.Result.NumberComponentsDeployed != 3 || deploy.Result.NumberComponentErrors != 0 || deploy.Result.NumberTestErrors != 0 || deploy.Result.NumberTestsTotal != 0 || deploy.Result.RunTestsEnabled || deploy.Result.Details.RunTestResult.NumTestsRun != 0 || len(deploy.Result.Details.ComponentFailures) != 0 {
		t.Fatalf("deployment = %#v", deploy)
	}
	deployedClasses := make([]string, 0, len(deploy.Result.Details.ComponentSuccesses))
	for _, component := range deploy.Result.Details.ComponentSuccesses {
		if !component.Success {
			t.Fatalf("component deployment failure = %#v", component)
		}
		if component.FullName != "package.xml" {
			deployedClasses = append(deployedClasses, component.FullName)
		}
	}
	if !slices.Equal(sortedCB195IDs(deployedClasses), []string{"CB195DataSourceDtos", "CB195DataSourceEnums", "CB195DataSourceServices"}) {
		t.Fatalf("deployed classes = %#v", deployedClasses)
	}

	var create cb195Create
	readCB195JSON(t, filepath.Join(evidenceRoot, cb195CreatePath), &create)
	if create.Status != 0 || create.Result.Username == "" || create.Result.OrgID == "" || create.Result.Auth.InstanceAPIVersion != "67.0" {
		t.Fatalf("scratch creation = %#v", create)
	}
	var deleted cb195Delete
	readCB195JSON(t, filepath.Join(evidenceRoot, cb195DeletePath), &deleted)
	if deleted.Status != 0 || deleted.Result.Username != create.Result.Username || deleted.Result.OrgID != create.Result.OrgID {
		t.Fatalf("scratch deletion = %#v", deleted)
	}
	postDelete, err := os.ReadFile(filepath.Join(evidenceRoot, cb195PostDeletePath))
	if err != nil {
		t.Fatal(err)
	}
	postDeleteText := string(postDelete)
	if !strings.Contains(postDeleteText, "NamedOrgNotFoundError") || !strings.Contains(postDeleteText, cb195OrgAlias) || !strings.Contains(postDeleteText, "\"exitCode\": 2") || !strings.Contains(postDeleteText, "OrgDisplayCommand") {
		t.Fatalf("post-delete proof = %s", postDeleteText)
	}

	assertCB195Artifact(t, filepath.Join(evidenceRoot, "evidence", "glade-candidate-fd687589"), "candidate binary", cb195CandidateSHA256)
	assertCB195Artifact(t, filepath.Join(evidenceRoot, cb195ProfilePath), "profile", cb195ProfileSHA256)
	assertCB195Artifact(t, filepath.Join(evidenceRoot, cb195SnapshotPath), "snapshot", cb195SnapshotSHA256)
	for path, hash := range map[string]string{cb195EnumsSourcePath: cb195EnumsSourceSHA256, cb195DtosSourcePath: cb195DtosSourceSHA256, cb195ServicesSourcePath: cb195ServicesSourceSHA256} {
		assertCB195Artifact(t, filepath.Join(evidenceRoot, path), "source", hash)
	}

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
	for _, row := range fixtureEvidence {
		if row.GladeShape != ShapeTypeKnown || row.GladeBehavior != BehaviorNone {
			t.Fatalf("fixture granted non-shape credit for %s: shape %s, behavior %s", row.SurfaceID, row.GladeShape, row.GladeBehavior)
		}
	}
	for _, row := range oracleEvidence {
		if row.GladeBehavior != BehaviorNone {
			t.Fatalf("oracle granted behavior credit for %s: %s", row.SurfaceID, row.GladeBehavior)
		}
	}
	ledger := Merge(nil, nil, BuildGladeSnapshot(), append(fixtureEvidence, oracleEvidence...))
	for _, id := range wantIDs {
		row, ok := rowsByID(ledger.Rows)[id]
		if !ok || row.Evidence != EvidenceFixtureAndOracle || row.GladeShape == ShapeAbsent {
			t.Fatalf("%s ledger evidence/behavior = %#v", id, row)
		}
	}
}

func assertCB195Candidate(t *testing.T, candidate cb195Candidate) {
	t.Helper()
	if candidate.Commit != cb195CandidateCommit || candidate.SHA256 != cb195CandidateSHA256 {
		t.Fatalf("candidate = %#v", candidate)
	}
}

func assertCB195Profile(t *testing.T, profile cb195Profile) {
	t.Helper()
	if profile.Path != cb195ProfilePath || profile.SHA256 != cb195ProfileSHA256 || profile.Selection != "compile-shape-required" || !slices.Equal(profile.Areas, []string{"DataSource"}) || profile.SelectedRowCount != 241 {
		t.Fatalf("profile = %#v", profile)
	}
}

func assertCB195Snapshot(t *testing.T, snapshot cb195Identity) {
	t.Helper()
	if snapshot.Path != cb195SnapshotPath || snapshot.SHA256 != cb195SnapshotSHA256 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func assertCB195Excluded(t *testing.T, excluded []cb195Excluded) {
	t.Helper()
	want := map[string]string{
		"cb195-primary-glade-dev-hub-limit-exceeded": "failed attempt",
		"cb195-hub2-pre-exec4-attempts":              "Earlier Hub-2",
	}
	for _, item := range excluded {
		wantReason, ok := want[item.CaseID]
		if !ok || item.Status != "inconclusive" || item.Credited || len(item.SurfaceIDs) != 0 || !strings.Contains(item.Reason, wantReason) {
			t.Fatalf("excluded case = %#v", item)
		}
		delete(want, item.CaseID)
	}
	if len(want) != 0 {
		t.Fatalf("missing excluded cases = %#v", want)
	}
}

func readCB195JSON(t *testing.T, path string, target any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if start := bytes.IndexByte(data, '{'); start >= 0 {
		data = data[start:]
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatal(err)
	}
}

func assertCB195Artifact(t *testing.T, path, label, wantSHA256 string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s %s: %v", label, path, err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(data)); got != wantSHA256 {
		t.Fatalf("%s SHA-256 = %s, want %s", label, got, wantSHA256)
	}
}

func sortedCB195IDs(ids []string) []string {
	out := append([]string(nil), ids...)
	slices.Sort(out)
	return out
}

func uniqueCB195(ids []string) []string {
	out := append([]string(nil), ids...)
	slices.Sort(out)
	return slices.Compact(out)
}

func containsAny(value, needle string) bool {
	return strings.Contains(strings.ToLower(value), strings.ToLower(needle))
}
