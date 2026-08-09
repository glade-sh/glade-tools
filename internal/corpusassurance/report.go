package corpusassurance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// AssuranceReport is the public, neutral release outcome projection.
type AssuranceReport struct {
	SchemaVersion int                   `json:"schemaVersion"`
	Rows          []AssuranceSurfaceRow `json:"rows"`
}

// AssuranceReceipt deliberately does not hash itself, keeping the sealed
// artifact graph acyclic.
type AssuranceReceipt struct {
	SchemaVersion   int    `json:"schemaVersion"`
	AssuranceSHA256 string `json:"assuranceSha256"`
	HTMLSHA256      string `json:"htmlSha256"`
	ReceiptSHA256   string `json:"receiptSha256,omitempty"`
}

// AssuranceSurfaceRow is the public, neutral per-surface release outcome.
// Readiness is cumulative: runtime parity implies test and compile readiness;
// explicit non-parity is mutually exclusive with all readiness claims.
type AssuranceSurfaceRow struct {
	Namespace          string   `json:"namespace"`
	SurfaceID          string   `json:"surfaceId"`
	UsageKeys          []string `json:"usageKeys"`
	RepositoryIDs      []string `json:"repositoryIds"`
	PrivateProdRefs    int      `json:"privateProdRefs"`
	PrivateTestRefs    int      `json:"privateTestRefs"`
	Disposition        string   `json:"disposition"`
	LocalEvidence      string   `json:"localEvidence"`
	SalesforceAction   string   `json:"salesforceAction"`
	SalesforceEvidence string   `json:"salesforceEvidence"`
	ExclusionClass     string   `json:"exclusionClass,omitempty"`
	ExclusionReason    string   `json:"exclusionReason,omitempty"`
	FixtureIDs         []string `json:"fixtureIds"`
	CompileReady       bool     `json:"compileReady"`
	TestReady          bool     `json:"testReady"`
	RuntimeParityReady bool     `json:"runtimeParityReady"`
	NonParity          bool     `json:"nonParity"`
}

func ValidateAssuranceOutcomes(rows []AssuranceSurfaceRow) error {
	if len(rows) == 0 {
		return fmt.Errorf("assurance outcome rows are required")
	}
	seen := make(map[string]bool, len(rows))
	for _, row := range rows {
		if row.SurfaceID == "" || seen[row.SurfaceID] {
			return fmt.Errorf("invalid or duplicate assurance surface %q", row.SurfaceID)
		}
		seen[row.SurfaceID] = true
		if row.NonParity {
			if row.CompileReady || row.TestReady || row.RuntimeParityReady || row.ExclusionClass == "" || row.ExclusionReason == "" {
				return fmt.Errorf("invalid non-parity outcome for %q", row.SurfaceID)
			}
			continue
		}
		if !row.CompileReady || (row.TestReady && !row.CompileReady) || (row.RuntimeParityReady && !row.TestReady) {
			return fmt.Errorf("invalid readiness outcome for %q", row.SurfaceID)
		}
		if row.ExclusionClass != "" || row.ExclusionReason != "" {
			return fmt.Errorf("parity outcome carries exclusion for %q", row.SurfaceID)
		}
	}
	return nil
}

// WriteAssuranceArtifacts writes the report, its offline explorer, and then
// the acyclic receipt. All targets are preflighted to avoid known no-clobber
// failures before the first file is created.
func WriteAssuranceArtifacts(report AssuranceReport, jsonPath, htmlPath, receiptPath string) (AssuranceReceipt, error) {
	if report.SchemaVersion != 1 {
		return AssuranceReceipt{}, fmt.Errorf("unsupported assurance report schema version %d", report.SchemaVersion)
	}
	if err := ValidateAssuranceOutcomes(report.Rows); err != nil {
		return AssuranceReceipt{}, err
	}
	reportJSON, err := json.Marshal(report)
	if err != nil {
		return AssuranceReceipt{}, err
	}
	if containsPrivateReportPath(reportJSON) {
		return AssuranceReceipt{}, fmt.Errorf("assurance report is not safe for public output")
	}
	paths := []string{jsonPath, htmlPath, receiptPath}
	for index, path := range paths {
		if !filepath.IsAbs(path) {
			return AssuranceReceipt{}, fmt.Errorf("absolute assurance artifact paths are required")
		}
		for _, earlier := range paths[:index] {
			if path == earlier {
				return AssuranceReceipt{}, fmt.Errorf("assurance artifact paths must be distinct")
			}
		}
		if _, err := os.Lstat(path); err == nil {
			return AssuranceReceipt{}, fmt.Errorf("assurance artifact output already exists: %s", path)
		} else if !os.IsNotExist(err) {
			return AssuranceReceipt{}, err
		}
	}
	if err := WriteNewJSON(jsonPath, report); err != nil {
		return AssuranceReceipt{}, err
	}
	if err := WriteAssuranceHTML(jsonPath, htmlPath); err != nil {
		return AssuranceReceipt{}, err
	}
	assuranceHash, err := sha256File(jsonPath)
	if err != nil {
		return AssuranceReceipt{}, err
	}
	htmlHash, err := sha256File(htmlPath)
	if err != nil {
		return AssuranceReceipt{}, err
	}
	receipt := AssuranceReceipt{SchemaVersion: 1, AssuranceSHA256: assuranceHash, HTMLSHA256: htmlHash}
	if err := WriteNewJSON(receiptPath, receipt); err != nil {
		return AssuranceReceipt{}, err
	}
	return receipt, nil
}
