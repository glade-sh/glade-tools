package corpusassurance

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

type SurfaceLocalProofPlanRequest struct {
	ScopePath         string
	SourceProfilePath string
	LedgerPath        string
	PolicyPath        string
	FixtureRoot       string
	ProfilePath       string
	UsagePath         string
	LocalDecisionPath string
	ManifestPath      string
	CoveragePath      string
}

type SurfaceLocalProofCoverage struct {
	SchemaVersion        int                     `json:"schemaVersion"`
	ScopeSHA256          string                  `json:"scopeSha256"`
	Total                int                     `json:"total"`
	Covered              int                     `json:"covered"`
	MissingCount         int                     `json:"missingCount"`
	Missing              []SurfaceOracleScopeRow `json:"missing"`
	UnclassifiedFixtures []string                `json:"unclassifiedFixtures"`
}

func BuildSurfaceLocalProofPlan(request SurfaceLocalProofPlanRequest) (LocalProofFixtureManifest, SurfaceLocalProofCoverage, error) {
	paths := []string{request.ScopePath, request.SourceProfilePath, request.LedgerPath, request.PolicyPath, request.FixtureRoot, request.ProfilePath, request.UsagePath, request.LocalDecisionPath, request.ManifestPath, request.CoveragePath}
	for _, path := range paths {
		if !filepath.IsAbs(path) {
			return LocalProofFixtureManifest{}, SurfaceLocalProofCoverage{}, fmt.Errorf("absolute surface local-proof plan paths are required")
		}
	}
	for _, path := range []string{request.ProfilePath, request.UsagePath, request.LocalDecisionPath, request.ManifestPath, request.CoveragePath} {
		if _, err := os.Lstat(path); err == nil {
			return LocalProofFixtureManifest{}, SurfaceLocalProofCoverage{}, fmt.Errorf("surface local-proof plan output already exists: %s", path)
		} else if !os.IsNotExist(err) {
			return LocalProofFixtureManifest{}, SurfaceLocalProofCoverage{}, err
		}
	}
	if stat, err := os.Stat(request.FixtureRoot); err != nil || !stat.IsDir() {
		return LocalProofFixtureManifest{}, SurfaceLocalProofCoverage{}, fmt.Errorf("fixture root is not a directory: %s", request.FixtureRoot)
	}
	scope, scopeBytes, err := readExactJSONBytes[SurfaceOracleScope](request.ScopePath)
	if err != nil {
		return LocalProofFixtureManifest{}, SurfaceLocalProofCoverage{}, fmt.Errorf("read surface scope: %w", err)
	}
	temp, err := os.MkdirTemp("", "glade-assurance-surface-local-proof-plan-*")
	if err != nil {
		return LocalProofFixtureManifest{}, SurfaceLocalProofCoverage{}, err
	}
	defer os.RemoveAll(temp)
	rebuiltPath := filepath.Join(temp, "SURFACE_ORACLE_SCOPE.json")
	if _, err := BuildSurfaceOracleScope(request.SourceProfilePath, request.LedgerPath, request.PolicyPath, rebuiltPath); err != nil {
		return LocalProofFixtureManifest{}, SurfaceLocalProofCoverage{}, err
	}
	rebuiltBytes, err := os.ReadFile(rebuiltPath)
	if err != nil || !bytes.Equal(scopeBytes, rebuiltBytes) {
		return LocalProofFixtureManifest{}, SurfaceLocalProofCoverage{}, fmt.Errorf("surface scope does not match authoritative recomputation")
	}
	required := make(map[string]string, len(scope.Rows))
	for _, row := range scope.Rows {
		required[row.SurfaceID] = row.Disposition
	}
	manifest, missingIDs, err := analyzeLocalProofFixtures(request.FixtureRoot, required)
	if err != nil {
		return LocalProofFixtureManifest{}, SurfaceLocalProofCoverage{}, err
	}
	missing := make(map[string]bool, len(missingIDs))
	for _, surfaceID := range missingIDs {
		missing[surfaceID] = true
	}
	unclassified := make([]string, 0)
	for _, fixture := range manifest.Fixtures {
		if fixture.SalesforceEligible == nil {
			unclassified = append(unclassified, fixture.ID)
			continue
		}
		if *fixture.SalesforceEligible {
			manifest.SalesforceFixtures = append(manifest.SalesforceFixtures, fixture)
		}
	}
	sort.Strings(unclassified)
	coverage := SurfaceLocalProofCoverage{SchemaVersion: 1, ScopeSHA256: replayBytesSHA256(scopeBytes), Total: scope.Total, Covered: scope.Total - len(missingIDs), MissingCount: len(missingIDs), Missing: []SurfaceOracleScopeRow{}, UnclassifiedFixtures: unclassified}
	for _, row := range scope.Rows {
		if missing[row.SurfaceID] {
			coverage.Missing = append(coverage.Missing, row)
		}
	}
	if err := verifySurfaceLocalProofInputs(map[string]string{request.ScopePath: coverage.ScopeSHA256, request.SourceProfilePath: scope.SourceProfileSHA256, request.LedgerPath: scope.LedgerSHA256, request.PolicyPath: scope.PolicySHA256}); err != nil {
		return LocalProofFixtureManifest{}, SurfaceLocalProofCoverage{}, err
	}
	if err := WriteNewJSON(request.CoveragePath, coverage); err != nil {
		return LocalProofFixtureManifest{}, SurfaceLocalProofCoverage{}, err
	}
	if coverage.MissingCount != 0 || len(coverage.UnclassifiedFixtures) != 0 {
		return manifest, coverage, fmt.Errorf("surface local-proof coverage incomplete: covered=%d missing=%d unclassified-fixtures=%d output=%s", coverage.Covered, coverage.MissingCount, len(coverage.UnclassifiedFixtures), request.CoveragePath)
	}
	if err := writeLocalProofPlan(required, manifest, request.ProfilePath, request.UsagePath, request.LocalDecisionPath, request.ManifestPath); err != nil {
		return LocalProofFixtureManifest{}, coverage, err
	}
	return manifest, coverage, nil
}

func verifySurfaceLocalProofInputs(bindings map[string]string) error {
	for path, want := range bindings {
		got, err := proofInputSHA256(path)
		if err != nil || got != want {
			return fmt.Errorf("surface local-proof inputs changed during planning")
		}
	}
	return nil
}
