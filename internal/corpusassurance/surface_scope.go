package corpusassurance

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/glade-sh/glade/tools/internal/surfaceledger"
)

type SurfaceOracleScope struct {
	SchemaVersion       int                     `json:"schemaVersion"`
	Kind                string                  `json:"kind"`
	SourceProfileSHA256 string                  `json:"sourceProfileSha256"`
	LedgerSHA256        string                  `json:"ledgerSha256"`
	PolicySHA256        string                  `json:"policySha256"`
	Total               int                     `json:"total"`
	ByDisposition       map[string]int          `json:"byDisposition"`
	Rows                []SurfaceOracleScopeRow `json:"rows"`
}

type SurfaceOracleScopeRow struct {
	SurfaceID   string `json:"surfaceId"`
	Disposition string `json:"disposition"`
}

func BuildSurfaceOracleScope(profilePath, ledgerPath, policyPath, outputPath string) (SurfaceOracleScope, error) {
	for _, path := range []string{profilePath, ledgerPath, policyPath, outputPath} {
		if !filepath.IsAbs(path) {
			return SurfaceOracleScope{}, fmt.Errorf("absolute surface-scope paths are required")
		}
	}
	if _, err := os.Lstat(outputPath); err == nil {
		return SurfaceOracleScope{}, fmt.Errorf("surface-scope output already exists: %s", outputPath)
	} else if !os.IsNotExist(err) {
		return SurfaceOracleScope{}, err
	}
	profileFile, err := readRegularFileSnapshot(profilePath)
	if err != nil {
		return SurfaceOracleScope{}, err
	}
	ledgerFile, err := readRegularFileSnapshot(ledgerPath)
	if err != nil {
		return SurfaceOracleScope{}, err
	}
	policyFile, err := readRegularFileSnapshot(policyPath)
	if err != nil {
		return SurfaceOracleScope{}, err
	}
	var profile surfaceledger.SupportProfile
	var ledger surfaceledger.SurfaceLedger
	var policy surfaceledger.SupportPolicy
	if err := decodeExactJSON(profileFile.Data, &profile); err != nil {
		return SurfaceOracleScope{}, fmt.Errorf("source profile: %w", err)
	}
	if err := decodeExactJSON(ledgerFile.Data, &ledger); err != nil {
		return SurfaceOracleScope{}, fmt.Errorf("surface ledger: %w", err)
	}
	if err := decodeExactJSON(policyFile.Data, &policy); err != nil {
		return SurfaceOracleScope{}, fmt.Errorf("support policy: %w", err)
	}
	ledgerSHA, policySHA := replayBytesSHA256(ledgerFile.Data), replayBytesSHA256(policyFile.Data)
	boundLedger, ledgerOK := boundProfileInput(profile.Inputs, "ledger")
	boundPolicy, policyOK := boundProfileInput(profile.Inputs, "policy")
	if !ledgerOK || !policyOK || boundLedger != ledgerSHA || boundPolicy != policySHA {
		return SurfaceOracleScope{}, fmt.Errorf("source profile does not bind ledger and policy")
	}
	if ledger.SchemaVersion != surfaceledger.SchemaVersion || profile.Total != len(profile.Rows) || len(profile.ValidationErrors) != 0 {
		return SurfaceOracleScope{}, fmt.Errorf("source profile or ledger is invalid")
	}
	ledgerIDs := make(map[string]bool, len(ledger.Rows))
	for _, row := range ledger.Rows {
		if strings.TrimSpace(row.SurfaceID) == "" || ledgerIDs[row.SurfaceID] {
			return SurfaceOracleScope{}, fmt.Errorf("invalid or duplicate ledger surface %q", row.SurfaceID)
		}
		ledgerIDs[row.SurfaceID] = true
	}
	expected := surfaceledger.ComputeSupportProfile(ledger.Rows, policy, nil)
	if len(expected.ValidationErrors) != 0 || expected.Total != profile.Total || len(expected.Rows) != len(profile.Rows) {
		return SurfaceOracleScope{}, fmt.Errorf("source profile does not match ledger and policy")
	}
	expectedByID := make(map[string]surfaceledger.SupportDisposition, len(expected.Rows))
	for _, row := range expected.Rows {
		expectedByID[row.SurfaceID] = row.Disposition
	}
	seen := make(map[string]bool, len(profile.Rows))
	profileCounts := make(map[string]int)
	scope := SurfaceOracleScope{
		SchemaVersion:       1,
		Kind:                "all-runtime",
		SourceProfileSHA256: replayBytesSHA256(profileFile.Data),
		LedgerSHA256:        ledgerSHA,
		PolicySHA256:        policySHA,
		ByDisposition:       map[string]int{deterministicMockRequired: 0, localRuntimeRequired: 0},
	}
	for _, row := range profile.Rows {
		disposition := string(row.Disposition)
		if strings.TrimSpace(row.SurfaceID) == "" || seen[row.SurfaceID] || expectedByID[row.SurfaceID] != row.Disposition {
			return SurfaceOracleScope{}, fmt.Errorf("invalid or duplicate source profile surface %q", row.SurfaceID)
		}
		seen[row.SurfaceID] = true
		profileCounts[disposition]++
		if disposition != deterministicMockRequired && disposition != localRuntimeRequired {
			continue
		}
		scope.Rows = append(scope.Rows, SurfaceOracleScopeRow{SurfaceID: row.SurfaceID, Disposition: disposition})
		scope.ByDisposition[disposition]++
	}
	if !supportProfileCountsMatch(profile.ByDisposition, profileCounts) {
		return SurfaceOracleScope{}, fmt.Errorf("source profile disposition counts do not reconcile")
	}
	sort.Slice(scope.Rows, func(i, j int) bool { return scope.Rows[i].SurfaceID < scope.Rows[j].SurfaceID })
	scope.Total = len(scope.Rows)
	if err := WriteNewJSON(outputPath, scope); err != nil {
		return SurfaceOracleScope{}, err
	}
	return scope, nil
}

func boundProfileInput(profileInputs *surfaceledger.SupportProfileInputs, name string) (string, bool) {
	if profileInputs == nil {
		return "", false
	}
	value := ""
	for _, input := range profileInputs.Files {
		if input.Name != name {
			continue
		}
		if value != "" || input.SHA256 == "" {
			return "", false
		}
		value = input.SHA256
	}
	return value, value != ""
}

func supportProfileCountsMatch(want map[surfaceledger.SupportDisposition]int, got map[string]int) bool {
	if len(want) != len(got) {
		return false
	}
	for disposition, count := range want {
		if got[string(disposition)] != count {
			return false
		}
	}
	return true
}
