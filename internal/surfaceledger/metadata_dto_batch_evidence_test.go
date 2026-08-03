package surfaceledger

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
)

const metadataDTOBatchComparisonPath = "docs/fixtures/salesforce-metadata-dto-batch-20260803-comparisons.json"

var metadataDTOBatchSurfaceIDs = []string{
	"apex:Metadata.CustomMetadata",
	"apex:Metadata.CustomMetadataValue",
	"apex:Metadata.Metadata",
	"apex:Metadata.DeployContainer",
	"apex:Metadata.DeployResult",
	"apex:Metadata.DeployStatus",
	"apex:Metadata.DeployStatus.Succeeded",
	"apex:Metadata.DeployStatus.Failed",
	"apex:Metadata.DeployStatus.InProgress",
	"apex:Metadata.MetadataType",
	"apex:Metadata.MetadataType.CustomMetadata",
}

type metadataDTOBatchComparison struct {
	CaseID           string   `json:"caseId"`
	Status           string   `json:"status"`
	SurfaceIDs       []string `json:"surfaceIds"`
	SFObservation    string   `json:"sfObservation"`
	GladeObservation string   `json:"gladeObservation"`
}

type metadataDTOBatchEnvelope struct {
	Candidate struct {
		Commit string `json:"commit"`
		SHA256 string `json:"sha256"`
	} `json:"candidate"`
	SalesforceExecution string `json:"salesforceExecution"`
	Salesforce          struct {
		TargetOrgAlias string `json:"targetOrgAlias"`
		APIVersion     string `json:"apiVersion"`
		ReportPath     string `json:"reportPath"`
		ReportSHA256   string `json:"reportSha256"`
	} `json:"salesforce"`
	Local struct {
		CandidatePath   string `json:"candidatePath"`
		CandidateCommit string `json:"candidateCommit"`
		CandidateSHA256 string `json:"candidateSha256"`
		Command         string `json:"command"`
		ReportPath      string `json:"reportPath"`
		ReportSHA256    string `json:"reportSha256"`
	} `json:"local"`
	Predecessor struct {
		Bundle                 string `json:"bundle"`
		ProfileNonDeferredGaps int    `json:"profileNonDeferredGaps"`
		NetNewRows             int    `json:"netNewRows"`
	} `json:"predecessor"`
	SalesforceReport struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	} `json:"salesforceReport"`
	GladeReport struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	} `json:"gladeReport"`
	LocalFixtures []struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	} `json:"localFixtures"`
	Comparisons []metadataDTOBatchComparison `json:"comparisons"`
}

func TestMetadataDTOBatchRowsHaveExactFixtureAndFreshOracleEvidence(t *testing.T) {
	toolsRoot := filepath.Join("..", "..")
	comparisonPath := filepath.Join(toolsRoot, metadataDTOBatchComparisonPath)
	var comparison metadataDTOBatchEnvelope
	readJSON(t, comparisonPath, &comparison)
	if comparison.Candidate.Commit != "6419bf1e8ede470d9fd5c6c789aede9ef5d2713d" || comparison.Candidate.SHA256 == "" || comparison.SalesforceExecution != "fresh-api67" {
		t.Fatalf("comparison provenance = %#v", comparison)
	}
	if comparison.Salesforce.TargetOrgAlias != "glade-sf-correctness" || comparison.Salesforce.APIVersion != "67.0" || comparison.Salesforce.ReportPath != comparison.SalesforceReport.Path || comparison.Salesforce.ReportSHA256 != comparison.SalesforceReport.SHA256 {
		t.Fatalf("Salesforce provenance = %#v", comparison.Salesforce)
	}
	if comparison.Local.CandidatePath != "evidence/glade-candidate-metadata-fix" || comparison.Local.CandidateCommit != comparison.Candidate.Commit || comparison.Local.CandidateSHA256 != comparison.Candidate.SHA256 || comparison.Local.Command == "" || comparison.Local.ReportPath != comparison.GladeReport.Path || comparison.Local.ReportSHA256 != comparison.GladeReport.SHA256 {
		t.Fatalf("local provenance = %#v", comparison.Local)
	}
	if comparison.Predecessor.ProfileNonDeferredGaps != 5417 || comparison.Predecessor.NetNewRows != 9 || comparison.Predecessor.Bundle == "" {
		t.Fatalf("predecessor = %#v", comparison.Predecessor)
	}
	if len(comparison.LocalFixtures) != 2 || len(comparison.Comparisons) != 3 {
		t.Fatalf("comparison batch shape = %#v", comparison)
	}

	fixturePaths := make([]string, 0, len(comparison.LocalFixtures))
	for _, source := range comparison.LocalFixtures {
		path := filepath.Join(toolsRoot, source.Path)
		assertMetadataDTOBatchSHA256(t, path, source.SHA256)
		fixturePaths = append(fixturePaths, path)
	}

	fixtureEvidence, err := BuildEvidenceSnapshot(fixturePaths)
	if err != nil {
		t.Fatal(err)
	}
	assertExactIDs(t, evidenceIDsInSet(fixtureEvidence, metadataDTOBatchSurfaceIDs), metadataDTOBatchSurfaceIDs)

	var allComparisonIDs []string
	for _, row := range comparison.Comparisons {
		if row.Status != "pass" || row.CaseID == "" || row.SFObservation == "" || row.GladeObservation == "" {
			t.Fatalf("comparison row = %#v", row)
		}
		allComparisonIDs = append(allComparisonIDs, row.SurfaceIDs...)
	}
	assertExactIDs(t, allComparisonIDs, metadataDTOBatchSurfaceIDs)
	oracleEvidence, err := BuildOracleEvidenceSnapshot([]string{comparisonPath})
	if err != nil {
		t.Fatal(err)
	}
	assertExactIDs(t, evidenceIDsInSet(oracleEvidence, metadataDTOBatchSurfaceIDs), metadataDTOBatchSurfaceIDs)

	evidenceRoot, err := filepath.Abs(filepath.Join(toolsRoot, "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	assertMetadataDTOBatchSHA256(t, filepath.Join(evidenceRoot, "evidence", "glade-candidate-metadata-fix"), comparison.Candidate.SHA256)
	for _, report := range []struct{ path, sum string }{
		{comparison.SalesforceReport.Path, comparison.SalesforceReport.SHA256},
		{comparison.GladeReport.Path, comparison.GladeReport.SHA256},
	} {
		assertMetadataDTOBatchSHA256(t, filepath.Join(evidenceRoot, report.path), report.sum)
	}
	metadataDTOBatchAssertReports(t, evidenceRoot, comparison)

	ledger := Merge(nil, nil, BuildGladeSnapshot(), append(fixtureEvidence, oracleEvidence...))
	byID := rowsBySurfaceKey(ledger.Rows)
	for _, id := range metadataDTOBatchSurfaceIDs {
		row, ok := byID[surfaceIDKey(id)]
		if !ok || row.Evidence != EvidenceFixtureAndOracle || row.GladeShape == ShapeAbsent || row.GladeBehavior != BehaviorSupported {
			t.Fatalf("%s ledger row = %#v", id, row)
		}
	}
}

func metadataDTOBatchAssertReports(t *testing.T, evidenceRoot string, comparison metadataDTOBatchEnvelope) {
	t.Helper()
	var sf struct {
		TargetOrg string `json:"targetOrg"`
		Results   []struct {
			ID        string `json:"id"`
			Value     string `json:"value"`
			ValueType string `json:"valueType"`
		} `json:"results"`
	}
	readJSON(t, filepath.Join(evidenceRoot, comparison.Salesforce.ReportPath), &sf)
	if sf.TargetOrg != comparison.Salesforce.TargetOrgAlias || len(sf.Results) != 3 {
		t.Fatalf("Salesforce report identity/results = %#v", sf)
	}
	sfValues := make(map[string]string, len(sf.Results))
	for _, result := range sf.Results {
		sfValues[result.ID] = result.ValueType + ":" + result.Value
	}
	wantValues := map[string]string{
		"metadata-dto-values-and-status":       "String:[\"Feature.Default\",\"Feature.Default\",\"Enabled__c\",true,\"Succeeded\",true]",
		"metadata-deploystatus-queued-values":  "String:[\"Failed\",\"InProgress\"]",
		"metadata-metadatatype-custommetadata": "Metadata.MetadataType:CustomMetadata",
	}
	sfIDs, wantIDs := mapsKeys(sfValues), mapsKeys(wantValues)
	sort.Strings(sfIDs)
	sort.Strings(wantIDs)
	if !slices.Equal(sfIDs, wantIDs) {
		t.Fatalf("Salesforce result IDs = %v", mapsKeys(sfValues))
	}
	for id, want := range wantValues {
		if sfValues[id] != want {
			t.Fatalf("Salesforce result %s = %q, want %q", id, sfValues[id], want)
		}
	}
	var local struct {
		Status   string `json:"status"`
		ExitCode int    `json:"exitCode"`
		Data     struct {
			DebugEvents []struct {
				Message string `json:"message"`
			} `json:"debugEvents"`
		} `json:"data"`
	}
	readJSON(t, filepath.Join(evidenceRoot, comparison.Local.ReportPath), &local)
	if local.Status != "passed" || local.ExitCode != 0 || len(local.Data.DebugEvents) != 1 {
		t.Fatalf("local report status/results = %#v", local)
	}
	const prefix = "GLADE_STDLIB_ORACLE:"
	message := local.Data.DebugEvents[0].Message
	if !strings.HasPrefix(message, prefix) {
		t.Fatalf("local report debug message = %q", message)
	}
	var localResults []struct {
		ID        string `json:"id"`
		Value     string `json:"value"`
		ValueType string `json:"valueType"`
	}
	if err := json.Unmarshal([]byte(strings.TrimPrefix(message, prefix)), &localResults); err != nil {
		t.Fatal(err)
	}
	for _, result := range localResults {
		if sfValues[result.ID] != result.ValueType+":"+result.Value {
			t.Fatalf("local/Salesforce result mismatch for %s", result.ID)
		}
	}
}

func mapsKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}

func assertMetadataDTOBatchSHA256(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != want {
		t.Fatalf("SHA-256 %s = %s, want %s", path, got, want)
	}
}

func assertExactIDs(t *testing.T, got, want []string) {
	t.Helper()
	got = append([]string(nil), got...)
	want = append([]string(nil), want...)
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(slices.Compact(got), slices.Compact(want)) {
		t.Fatalf("surface IDs = %v, want %v", got, want)
	}
}
