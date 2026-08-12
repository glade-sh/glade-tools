package surfaceledger

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

type oracleComparisonEvidence struct {
	CaseID           string   `json:"caseId"`
	Status           string   `json:"status"`
	SFObservation    string   `json:"sfObservation"`
	GladeObservation string   `json:"gladeObservation"`
	SurfaceIDs       []string `json:"surfaceIds"`
}

type oracleComparisonEnvelope struct {
	Comparisons []oracleComparisonEvidence `json:"comparisons"`
}

type oracleVerificationReport struct {
	Runtime   oracleVerificationSection `json:"runtime"`
	Lifecycle oracleVerificationSection `json:"lifecycle"`
}

type oracleVerificationSection struct {
	Cases []oracleVerificationCase `json:"cases"`
}

type oracleVerificationCase struct {
	ID                    string   `json:"id"`
	Status                string   `json:"status"`
	SalesforceObservation string   `json:"salesforceObservation"`
	GladeObservation      string   `json:"gladeObservation"`
	SurfaceIDs            []string `json:"surfaceIds"`
}

// BuildOracleEvidenceSnapshot reads linked comparison artifacts and emits only
// behavior evidence justified by a passing dual-observation comparison.
func BuildOracleEvidenceSnapshot(paths []string) ([]SurfaceLedgerRow, error) {
	var rows []SurfaceLedgerRow
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		comparisons, recognized, err := oracleComparisons(data)
		if err != nil {
			return nil, fmt.Errorf("oracle evidence %s: %w", path, err)
		}
		if !recognized {
			continue
		}
		for _, comparison := range comparisons {
			comparisonRows, err := oracleRowsForComparison(comparison, filepath.Clean(path))
			if err != nil {
				return nil, fmt.Errorf("oracle evidence %s case %q: %w", path, comparison.CaseID, err)
			}
			rows = append(rows, comparisonRows...)
		}
	}
	sortRows(rows)
	return rows, nil
}

func oracleComparisons(data []byte) ([]oracleComparisonEvidence, bool, error) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil, false, nil
	}
	if strings.HasPrefix(trimmed, "[") {
		var comparisons []oracleComparisonEvidence
		if err := json.Unmarshal(data, &comparisons); err != nil {
			return nil, true, fmt.Errorf("invalid comparison array: %w", err)
		}
		return comparisons, true, nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, true, fmt.Errorf("invalid JSON: %w", err)
	}
	if _, ok := raw["comparisons"]; ok {
		var envelope oracleComparisonEnvelope
		if err := json.Unmarshal(data, &envelope); err != nil {
			return nil, true, fmt.Errorf("invalid comparison envelope: %w", err)
		}
		return envelope.Comparisons, true, nil
	}
	if _, runtime := raw["runtime"]; runtime {
		var report oracleVerificationReport
		if err := json.Unmarshal(data, &report); err != nil {
			return nil, true, fmt.Errorf("invalid verification report: %w", err)
		}
		return append(verificationCases(report.Runtime.Cases), verificationCases(report.Lifecycle.Cases)...), true, nil
	}
	if _, lifecycle := raw["lifecycle"]; lifecycle {
		var report oracleVerificationReport
		if err := json.Unmarshal(data, &report); err != nil {
			return nil, true, fmt.Errorf("invalid verification report: %w", err)
		}
		return verificationCases(report.Lifecycle.Cases), true, nil
	}
	return nil, false, nil
}

func verificationCases(cases []oracleVerificationCase) []oracleComparisonEvidence {
	comparisons := make([]oracleComparisonEvidence, 0, len(cases))
	for _, c := range cases {
		comparisons = append(comparisons, oracleComparisonEvidence{
			CaseID:           c.ID,
			Status:           c.Status,
			SFObservation:    c.SalesforceObservation,
			GladeObservation: c.GladeObservation,
			SurfaceIDs:       append([]string(nil), c.SurfaceIDs...),
		})
	}
	return comparisons
}

func oracleRowsForComparison(comparison oracleComparisonEvidence, artifactPath string) ([]SurfaceLedgerRow, error) {
	if len(comparison.SurfaceIDs) == 0 {
		return nil, nil
	}
	if comparison.CaseID == "" {
		return nil, fmt.Errorf("linked comparison missing caseId")
	}
	for _, id := range comparison.SurfaceIDs {
		if err := validateOracleSurfaceID(id); err != nil {
			return nil, err
		}
	}
	if comparison.Status != string(statusPass) || strings.TrimSpace(comparison.SFObservation) == "" || strings.TrimSpace(comparison.GladeObservation) == "" {
		return nil, nil
	}

	source := fmt.Sprintf("oracle:%s:%s", comparison.CaseID, artifactPath)
	notes := fmt.Sprintf("Salesforce/Glade oracle comparison case %s from %s", comparison.CaseID, artifactPath)
	rows := make([]SurfaceLedgerRow, 0, len(comparison.SurfaceIDs))
	for _, id := range comparison.SurfaceIDs {
		row := SurfaceLedgerRow{
			SurfaceID:      id,
			Product:        productFromID(id),
			Area:           evidenceAreaForProduct(productFromID(id)),
			Kind:           evidenceKindFromSurfaceID(id),
			Evidence:       EvidenceOracle,
			Sources:        []string{source},
			BehaviorSource: "oracle",
			Notes:          notes,
		}
		fillFromDataReferenceID(&row)
		fillFromApexID(&row)
		rows = append(rows, withDefaults(row))
	}
	return rows, nil
}

const statusPass = "pass"

func validateOracleSurfaceID(id string) error {
	if id == "" || strings.TrimSpace(id) != id || strings.IndexFunc(id, unicode.IsSpace) >= 0 || strings.IndexFunc(id, unicode.IsControl) >= 0 {
		return fmt.Errorf("malformed surfaceId %q", id)
	}
	colon := strings.IndexByte(id, ':')
	if colon <= 0 || colon == len(id)-1 {
		return fmt.Errorf("malformed surfaceId %q", id)
	}
	prefix := id[:colon]
	switch prefix {
	case "apex", "apex-language", "aura", "bulk-api", "cli-reference", "commerce-cli-reference", "connect-rest-api", "analytics-cli-reference", "data-reference", "lightning", "lwc", "metadata-api", "platform-events", "rest", "service-connector-api-reference", "site-references", "soap-api", "streaming-api", "tooling", "ui-api", "unknown", "visualforce":
		return nil
	default:
		return fmt.Errorf("malformed surfaceId %q: unknown product prefix", id)
	}
}
