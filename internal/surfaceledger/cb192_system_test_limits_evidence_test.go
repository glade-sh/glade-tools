package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

const (
	cb192CandidateCommit = "381e27e47e720ac7dbecb906bf275d2d5bcdd37f"
	cb192CandidateSHA256 = "976c2ceceb2a0936889129d2dc7314d2065fbc538caf54bbb534f20e4f588166"
	cb192ProfilePath     = "evidence/current-base/profile-381e27e4-cb191-20260802-1505/apex-support-profile.json"
	cb192ProfileSHA256   = "d50cde1597365eb721c796e6e071d30e924e23e0e5b42181a3296dfc5a4c7200"
	cb192SnapshotName    = "GLADE_SNAPSHOT.json"
	cb192SnapshotPath    = "evidence/current-base/surface-381e27e4-cb191-20260802-1505/GLADE_SNAPSHOT.json"
	cb192SnapshotSHA256  = "07389a70201b1e14020278c7bea88e24afaa98398864861ddfc36b02ae8dd7e4"
	cb192SourceSHA256    = "e18201c21093b8cfa418f83b60c136b1e1b50ef044a378619ededf2757d01cf1"
)

var cb192SystemTestLimitsSurfaceIDs = []string{
	"apex:System.Test.isRunningTest()",
	"apex:System.Test.setMock(Type,Object)",
	"apex:System.Test.startTest()",
	"apex:System.Test.stopTest()",
	"apex:System.Limits.getDmlRows",
	"apex:System.Limits.getDmlStatements",
	"apex:System.Limits.getQueries()",
	"apex:System.Limits.getQueryRows()",
	"apex:System.Assert.areEqual(Object,Object)",
}

type cb192CandidateIdentity struct {
	Commit string `json:"commit"`
	SHA256 string `json:"sha256"`
}

type cb192SnapshotIdentity struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type cb192FixtureScenario struct {
	ClassName      string `json:"className"`
	MethodName     string `json:"methodName"`
	SourcePath     string `json:"sourcePath"`
	SourceSHA256   string `json:"sourceSha256"`
	SalesforceMode string `json:"salesforceMode"`
	LocalMode      string `json:"localMode"`
}

type cb192FixtureEnvelope struct {
	Name       string                 `json:"name"`
	PacketID   string                 `json:"packetId"`
	APIVersion string                 `json:"apiVersion"`
	Mode       string                 `json:"mode"`
	Candidate  cb192CandidateIdentity `json:"candidate"`
	Profile    struct {
		Path             string   `json:"path"`
		SHA256           string   `json:"sha256"`
		Selection        string   `json:"selection"`
		Areas            []string `json:"areas"`
		SelectedRowCount int      `json:"selectedRowCount"`
	} `json:"profile"`
	CanonicalSnapshot cb192SnapshotIdentity `json:"canonicalSnapshot"`
	Scenario          cb192FixtureScenario  `json:"scenario"`
}

type cb192ComparisonRow struct {
	CaseID     string   `json:"caseId"`
	Status     string   `json:"status"`
	SurfaceIDs []string `json:"surfaceIds"`
	SF         string   `json:"sfObservation"`
	Glade      string   `json:"gladeObservation"`
}

type cb192ExcludedCase struct {
	CaseID   string `json:"caseId"`
	Status   string `json:"status"`
	Credited bool   `json:"credited"`
	Reason   string `json:"reason"`
}

type cb192ComparisonEnvelope struct {
	Name                string                 `json:"name"`
	PacketID            string                 `json:"packetId"`
	APIVersion          string                 `json:"apiVersion"`
	Mode                string                 `json:"mode"`
	Candidate           cb192CandidateIdentity `json:"candidate"`
	CanonicalSnapshot   cb192SnapshotIdentity  `json:"canonicalSnapshot"`
	SalesforceExecution struct {
		OrgAlias      string `json:"orgAlias"`
		OrgID         string `json:"orgId"`
		OrgAPIVersion string `json:"orgApiVersion"`
		ClassName     string `json:"className"`
		MethodName    string `json:"methodName"`
		Result        string `json:"result"`
		DebugLog      string `json:"debugLog"`
		DebugLogID    string `json:"debugLogId"`
	} `json:"salesforceExecution"`
	Comparisons []cb192ComparisonRow `json:"comparisons"`
	Excluded    []cb192ExcludedCase  `json:"excluded"`
	Mismatches  []any                `json:"mismatches"`
}

func TestCB192SystemTestLimitsRowsHaveExactDualEvidence(t *testing.T) {
	root := filepath.Join("..", "..")
	fixturePath := filepath.Join(root, "docs", "fixtures", "current-base-cb192-system-test-limits-positive-api67.json")
	comparisonPath := filepath.Join(root, "docs", "fixtures", "salesforce-cb192-system-test-limits-comparisons.json")

	var fixtureMeta cb192FixtureEnvelope
	cb192ReadJSON(t, fixturePath, &fixtureMeta)
	if fixtureMeta.Name != "current-base-cb192-system-test-limits-positive-api67" || fixtureMeta.PacketID != "CB192" || fixtureMeta.APIVersion != "67.0" || fixtureMeta.Mode != "test-class" {
		t.Fatalf("fixture metadata = %#v", fixtureMeta)
	}
	cb192AssertCandidate(t, fixtureMeta.Candidate)
	if fixtureMeta.Profile.Path != cb192ProfilePath || fixtureMeta.Profile.SHA256 != cb192ProfileSHA256 || fixtureMeta.Profile.Selection != "local-runtime-required" || !slices.Equal(fixtureMeta.Profile.Areas, []string{"System.Test", "System.Limits"}) || fixtureMeta.Profile.SelectedRowCount != 132 {
		t.Fatalf("fixture profile identity = %#v", fixtureMeta.Profile)
	}
	cb192AssertSnapshot(t, fixtureMeta.CanonicalSnapshot)
	if fixtureMeta.Scenario.ClassName != "CB192SystemTestLimitsOracle" || fixtureMeta.Scenario.MethodName != "broadSystemTestLimits" || fixtureMeta.Scenario.SourceSHA256 != cb192SourceSHA256 || fixtureMeta.Scenario.SalesforceMode != "test-class" || fixtureMeta.Scenario.LocalMode != "test-class" {
		t.Fatalf("fixture scenario = %#v", fixtureMeta.Scenario)
	}

	fixture, err := compat.LoadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if fixture.Name != fixtureMeta.Name {
		t.Fatalf("loaded fixture name = %q, metadata name = %q", fixture.Name, fixtureMeta.Name)
	}
	fixtureEvidence, err := BuildEvidenceSnapshot([]string{fixturePath})
	if err != nil {
		t.Fatal(err)
	}
	if got := cb192ExactSurfaceIDs(fixtureEvidence); !slices.Equal(got, sortedCB192SurfaceIDs()) {
		t.Fatalf("fixture surface IDs = %#v, want %#v", got, sortedCB192SurfaceIDs())
	}
	cb192AssertCanonicalSurfaceIDs(t, fixtureEvidence)

	var comparisonMeta cb192ComparisonEnvelope
	cb192ReadJSON(t, comparisonPath, &comparisonMeta)
	if comparisonMeta.Name != "salesforce-cb192-system-test-limits-comparisons" || comparisonMeta.PacketID != "CB192" || comparisonMeta.APIVersion != "67.0" || comparisonMeta.Mode != "test-class" {
		t.Fatalf("comparison metadata = %#v", comparisonMeta)
	}
	cb192AssertCandidate(t, comparisonMeta.Candidate)
	cb192AssertSnapshot(t, comparisonMeta.CanonicalSnapshot)
	if comparisonMeta.SalesforceExecution.OrgAlias != "glade-cb190-schema-20260802" || comparisonMeta.SalesforceExecution.OrgAPIVersion != "67.0" || comparisonMeta.SalesforceExecution.ClassName != "CB192SystemTestLimitsOracle" || comparisonMeta.SalesforceExecution.MethodName != "broadSystemTestLimits" || comparisonMeta.SalesforceExecution.Result != "Pass" || comparisonMeta.SalesforceExecution.DebugLogID == "" {
		t.Fatalf("Salesforce execution = %#v", comparisonMeta.SalesforceExecution)
	}
	if len(comparisonMeta.Comparisons) != 1 || len(comparisonMeta.Excluded) != 1 || len(comparisonMeta.Mismatches) != 0 {
		t.Fatalf("comparison counts = comparisons %d excluded %d mismatches %d", len(comparisonMeta.Comparisons), len(comparisonMeta.Excluded), len(comparisonMeta.Mismatches))
	}
	comparison := comparisonMeta.Comparisons[0]
	if comparison.CaseID != "cb192-broad-test-class-system-test-limits" || comparison.Status != "pass" || comparison.SF == "" || comparison.Glade == "" || !slices.Equal(cb192SortedIDs(comparison.SurfaceIDs), sortedCB192SurfaceIDs()) {
		t.Fatalf("comparison = %#v", comparison)
	}
	if comparisonMeta.Excluded[0].Credited || comparisonMeta.Excluded[0].Status != "inconclusive" || comparisonMeta.Excluded[0].Reason == "" {
		t.Fatalf("excluded = %#v", comparisonMeta.Excluded[0])
	}

	oracleEvidence, err := BuildOracleEvidenceSnapshot([]string{comparisonPath})
	if err != nil {
		t.Fatal(err)
	}
	if got := cb192ExactSurfaceIDs(oracleEvidence); !slices.Equal(got, sortedCB192SurfaceIDs()) {
		t.Fatalf("oracle surface IDs = %#v, want %#v", got, sortedCB192SurfaceIDs())
	}
	cb192AssertCanonicalSurfaceIDs(t, oracleEvidence)

	ledger := Merge(nil, nil, BuildGladeSnapshot(), append(fixtureEvidence, oracleEvidence...))
	byID := rowsBySurfaceKey(ledger.Rows)
	for _, id := range sortedCB192SurfaceIDs() {
		row, ok := byID[surfaceIDKey(id)]
		if !ok {
			t.Fatalf("missing CB192 evidence row %s", id)
		}
		if row.Evidence != EvidenceFixtureAndOracle {
			t.Errorf("%s evidence = %s, want fixture-and-oracle", id, row.Evidence)
		}
		if row.GladeBehavior != BehaviorSupported {
			t.Errorf("%s behavior = %s, want supported", id, row.GladeBehavior)
		}
	}
}

func cb192AssertCandidate(t *testing.T, candidate cb192CandidateIdentity) {
	t.Helper()
	if candidate.Commit != cb192CandidateCommit || candidate.SHA256 != cb192CandidateSHA256 {
		t.Fatalf("candidate = %#v", candidate)
	}
}

func cb192AssertSnapshot(t *testing.T, snapshot cb192SnapshotIdentity) {
	t.Helper()
	if snapshot.Name != cb192SnapshotName || snapshot.Path != cb192SnapshotPath || snapshot.SHA256 != cb192SnapshotSHA256 {
		t.Fatalf("canonical snapshot = %#v", snapshot)
	}
}

func cb192AssertCanonicalSurfaceIDs(t *testing.T, rows []SurfaceLedgerRow) {
	t.Helper()
	canonical := rowsBySurfaceKey(BuildGladeSnapshot())
	for _, row := range rows {
		current, ok := canonical[surfaceIDKey(row.SurfaceID)]
		if !ok || current.SurfaceID != row.SurfaceID {
			t.Errorf("%s is not a canonical pre-merge snapshot surface ID", row.SurfaceID)
		}
	}
}

func cb192ExactSurfaceIDs(rows []SurfaceLedgerRow) []string {
	ids := make([]string, 0, len(rows))
	seen := map[string]bool{}
	for _, row := range rows {
		if !seen[row.SurfaceID] {
			seen[row.SurfaceID] = true
			ids = append(ids, row.SurfaceID)
		}
	}
	slices.Sort(ids)
	return ids
}

func cb192SortedIDs(ids []string) []string {
	out := append([]string(nil), ids...)
	slices.Sort(out)
	return out
}

func sortedCB192SurfaceIDs() []string {
	return cb192SortedIDs(cb192SystemTestLimitsSurfaceIDs)
}

func cb192ReadJSON(t *testing.T, path string, target any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatal(err)
	}
}
