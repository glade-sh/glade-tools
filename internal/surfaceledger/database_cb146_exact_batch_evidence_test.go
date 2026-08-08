package surfaceledger

import (
	"path/filepath"
	"testing"
)

type databaseCB146ExactBatchEnvelope struct {
	Candidate struct {
		Commit string `json:"commit"`
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	} `json:"candidate"`
	Profile   string `json:"profile"`
	Selection struct {
		Count  int `json:"count"`
		Groups struct {
			G05 int `json:"G05"`
			G06 int `json:"G06"`
			G12 int `json:"G12"`
		} `json:"groups"`
	} `json:"selection"`
	IDs   []string `json:"ids"`
	Probe struct {
		Source           string `json:"source"`
		SourceSHA256     string `json:"sourceSha256"`
		LocalReport      string `json:"localReport"`
		LocalSHA256      string `json:"localSha256"`
		SalesforceReport string `json:"salesforceReport"`
		SalesforceSHA256 string `json:"salesforceSha256"`
		Comparison       string `json:"comparison"`
		ComparisonSHA256 string `json:"comparisonSha256"`
		NormalizedEqual  bool   `json:"normalizedEqual"`
		LocalRows        int    `json:"localRows"`
		SalesforceRows   int    `json:"salesforceRows"`
	} `json:"probe"`
	Cleanup struct {
		ProofPath          string `json:"proofPath"`
		ProofSHA256        string `json:"proofSha256"`
		TimelinePath       string `json:"timelinePath"`
		TimelineSHA256     string `json:"timelineSha256"`
		ArtifactHashesPath string `json:"artifactHashesPath"`
		PostCleanup        struct {
			ProbeClasses       int `json:"probeClasses"`
			PermissionSets     int `json:"permissionSets"`
			Assignments        int `json:"assignments"`
			ProbeRows          int `json:"probeRows"`
			PreExistingObjects int `json:"preExistingObjects"`
		} `json:"postCleanup"`
	} `json:"cleanup"`
}

func TestDatabaseCB146ExactBatchPinsCurrentCandidateDualOracleAndCleanup(t *testing.T) {
	toolsRoot := filepath.Join("..", "..")
	evidenceRoot, err := filepath.Abs(filepath.Join(toolsRoot, "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	var envelope databaseCB146ExactBatchEnvelope
	fixturePath := filepath.Join(toolsRoot, "docs", "fixtures", "salesforce-current-base-database-cb146-exact-api67-20260803-comparisons.json")
	currentBaseRebindReadJSON(t, fixturePath, &envelope)
	if envelope.Candidate.Commit != "0a0f624e9c6fc82f8efc824852aef2808cd823fa" || envelope.Candidate.SHA256 != currentBaseRebindCandidateSHA256 || envelope.Selection.Count != 31 || len(envelope.IDs) != 31 || envelope.Selection.Groups.G05 != 17 || envelope.Selection.Groups.G06 != 12 || envelope.Selection.Groups.G12 != 2 {
		t.Fatalf("batch selection/provenance = %#v", envelope)
	}
	if envelope.Profile != "evidence/current-base/canonical-bundle-current-base-0a0f624-cgo-20260803/apex-support-profile.json" {
		t.Fatalf("profile = %q", envelope.Profile)
	}
	if !envelope.Probe.NormalizedEqual || envelope.Probe.LocalRows != 31 || envelope.Probe.SalesforceRows != 31 {
		t.Fatalf("probe parity = %#v", envelope.Probe)
	}
	if envelope.Cleanup.PostCleanup.ProbeClasses != 0 || envelope.Cleanup.PostCleanup.PermissionSets != 0 || envelope.Cleanup.PostCleanup.Assignments != 0 || envelope.Cleanup.PostCleanup.ProbeRows != 0 || envelope.Cleanup.PostCleanup.PreExistingObjects != 2 {
		t.Fatalf("cleanup proof summary = %#v", envelope.Cleanup.PostCleanup)
	}
	for _, artifact := range []struct{ path, sha string }{
		{envelope.Candidate.Path, envelope.Candidate.SHA256},
		{envelope.Probe.Source, envelope.Probe.SourceSHA256},
		{envelope.Probe.LocalReport, envelope.Probe.LocalSHA256},
		{envelope.Probe.SalesforceReport, envelope.Probe.SalesforceSHA256},
		{envelope.Probe.Comparison, envelope.Probe.ComparisonSHA256},
		{envelope.Cleanup.ProofPath, envelope.Cleanup.ProofSHA256},
		{envelope.Cleanup.TimelinePath, envelope.Cleanup.TimelineSHA256},
		{envelope.Cleanup.ArtifactHashesPath, ""},
	} {
		if artifact.path == "" {
			t.Fatalf("missing artifact path: %#v", artifact)
		}
		if artifact.sha != "" {
			if got := currentBaseRebindSHA256(t, filepath.Join(evidenceRoot, artifact.path)); got != artifact.sha {
				t.Fatalf("%s SHA = %s, want %s", artifact.path, got, artifact.sha)
			}
		}
	}
}
