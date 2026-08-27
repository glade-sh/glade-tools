package corpusassurance

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
)

type SurfaceOraclePlanRequest struct {
	WavePlanPath          string
	ScopePath             string
	ProfilePath           string
	LocalProofPath        string
	FixtureManifestPath   string
	CoveragePath          string
	TerminalAuthorityPath string
	PredecessorIndexPath  string
	OutputPath            string
}

func surfaceWaveBundleManifest(plan OraclePlan, profile AssuranceProfile, proof LocalProof, manifest LocalProofFixtureManifest, wave SurfaceWavePlan, waveSHA, proofSHA, manifestSHA string) (LocalProofFixtureManifest, error) {
	if plan.SurfaceWavePlanSHA256 != waveSHA || profile.SurfaceWavePlanSHA256 != waveSHA || wave.LocalProofSHA256 != proofSHA || wave.FixtureManifestSHA256 != manifestSHA || wave.Candidate != proof.Candidate || wave.Tools != proof.Tools {
		return LocalProofFixtureManifest{}, fmt.Errorf("surface wave bundle inputs do not bind")
	}
	fixtures := make(map[string]LocalProofFixture, len(manifest.SalesforceFixtures))
	for _, fixture := range manifest.SalesforceFixtures {
		fixtures[fixture.ID] = fixture
	}
	selectedSurfaces := make(map[string]bool, wave.SelectedRows)
	ownedSurfaces := make(map[string]bool, wave.SelectedRows)
	selectedFixtures := make([]LocalProofFixture, 0, wave.SelectedFixtures)
	seenFixtures := make(map[string]bool, wave.SelectedFixtures)
	for _, shard := range wave.Shards {
		for _, fixture := range shard.Fixtures {
			if seenFixtures[fixture.ID] || !reflect.DeepEqual(fixtures[fixture.ID], fixture) {
				return LocalProofFixtureManifest{}, fmt.Errorf("surface wave fixture %q does not bind manifest", fixture.ID)
			}
			seenFixtures[fixture.ID] = true
			selectedFixtures = append(selectedFixtures, fixture)
			for _, surfaceID := range fixture.OwnedSurfaceIDs {
				if ownedSurfaces[surfaceID] {
					return LocalProofFixtureManifest{}, fmt.Errorf("duplicate surface wave fixture owner %q", surfaceID)
				}
				ownedSurfaces[surfaceID] = true
			}
		}
		for _, surfaceID := range shard.SurfaceIDs {
			if selectedSurfaces[surfaceID] {
				return LocalProofFixtureManifest{}, fmt.Errorf("duplicate surface wave row %q", surfaceID)
			}
			selectedSurfaces[surfaceID] = true
		}
	}
	if len(selectedFixtures) != wave.SelectedFixtures || len(selectedSurfaces) != wave.SelectedRows || len(plan.Rows) != len(selectedSurfaces) || !reflect.DeepEqual(selectedSurfaces, ownedSurfaces) || len(profile.Rows) != len(selectedSurfaces) {
		return LocalProofFixtureManifest{}, fmt.Errorf("surface wave bundle selection is incomplete")
	}
	profileSurfaces := make(map[string]bool, len(profile.Rows))
	for _, row := range profile.Rows {
		if !selectedSurfaces[row.SurfaceID] || profileSurfaces[row.SurfaceID] {
			return LocalProofFixtureManifest{}, fmt.Errorf("assurance profile row %q is outside surface wave", row.SurfaceID)
		}
		profileSurfaces[row.SurfaceID] = true
	}
	for _, row := range plan.Rows {
		if !selectedSurfaces[row.SurfaceID] {
			return LocalProofFixtureManifest{}, fmt.Errorf("oracle plan row %q is outside surface wave", row.SurfaceID)
		}
	}
	return LocalProofFixtureManifest{Fixtures: selectedFixtures, SalesforceFixtures: append([]LocalProofFixture(nil), selectedFixtures...)}, nil
}

type SurfaceOraclePlanArtifacts struct {
	Profile   AssuranceProfile
	Plan      OraclePlan
	Authority ExclusionAuthority
}

// BuildSurfaceOraclePlan converts one exact whole-fixture wave into the three
// inputs consumed by the existing Salesforce executor.
func BuildSurfaceOraclePlan(request SurfaceOraclePlanRequest) (SurfaceOraclePlanArtifacts, error) {
	for _, path := range []string{request.WavePlanPath, request.ScopePath, request.ProfilePath, request.LocalProofPath, request.FixtureManifestPath, request.CoveragePath, request.OutputPath} {
		if !filepath.IsAbs(path) {
			return SurfaceOraclePlanArtifacts{}, fmt.Errorf("absolute surface-oracle-plan paths are required")
		}
	}
	for _, path := range []string{request.TerminalAuthorityPath, request.PredecessorIndexPath} {
		if path != "" && !filepath.IsAbs(path) {
			return SurfaceOraclePlanArtifacts{}, fmt.Errorf("absolute surface-oracle-plan paths are required")
		}
	}
	if _, err := os.Lstat(request.OutputPath); err == nil {
		return SurfaceOraclePlanArtifacts{}, fmt.Errorf("surface oracle plan output already exists: %s", request.OutputPath)
	} else if !os.IsNotExist(err) {
		return SurfaceOraclePlanArtifacts{}, err
	}

	wave, waveBytes, err := readExactJSONBytes[SurfaceWavePlan](request.WavePlanPath)
	if err != nil {
		return SurfaceOraclePlanArtifacts{}, err
	}
	temp, err := os.MkdirTemp(filepath.Dir(request.OutputPath), ".surface-oracle-plan-")
	if err != nil {
		return SurfaceOraclePlanArtifacts{}, err
	}
	defer os.RemoveAll(temp)
	if err := os.Chmod(temp, 0o700); err != nil {
		return SurfaceOraclePlanArtifacts{}, err
	}
	rebuiltPath := filepath.Join(temp, "SURFACE_WAVE_PLAN.json")
	if _, err := BuildSurfaceWavePlan(SurfaceWavePlanRequest{
		ScopePath: request.ScopePath, ProfilePath: request.ProfilePath, LocalProofPath: request.LocalProofPath,
		FixtureManifestPath: request.FixtureManifestPath, CoveragePath: request.CoveragePath,
		TerminalAuthorityPath: request.TerminalAuthorityPath, PredecessorIndexPath: request.PredecessorIndexPath,
		MaxFixtures: wave.MaxFixtures, ShardCount: wave.ShardCount, OutputPath: rebuiltPath,
	}); err != nil {
		return SurfaceOraclePlanArtifacts{}, err
	}
	rebuilt, err := os.ReadFile(rebuiltPath)
	if err != nil || !bytes.Equal(rebuilt, waveBytes) {
		return SurfaceOraclePlanArtifacts{}, fmt.Errorf("surface wave plan does not match authoritative recomputation")
	}

	scope, _, err := readExactJSONBytes[SurfaceOracleScope](request.ScopePath)
	if err != nil {
		return SurfaceOraclePlanArtifacts{}, err
	}
	proof, proofBytes, err := readExactJSONBytes[LocalProof](request.LocalProofPath)
	if err != nil {
		return SurfaceOraclePlanArtifacts{}, err
	}
	manifest, manifestBytes, err := readExactJSONBytes[LocalProofFixtureManifest](request.FixtureManifestPath)
	if err != nil {
		return SurfaceOraclePlanArtifacts{}, err
	}
	if err := ValidateLocalProof(proof, manifest); err != nil {
		return SurfaceOraclePlanArtifacts{}, err
	}
	scopeByID := make(map[string]SurfaceOracleScopeRow, len(scope.Rows))
	for _, row := range scope.Rows {
		scopeByID[row.SurfaceID] = row
	}
	proofByID := make(map[string]LocalSurfaceProof, len(proof.Surfaces))
	for _, row := range proof.Surfaces {
		proofByID[row.SurfaceID] = row
	}
	selected := make(map[string]bool, wave.SelectedRows)
	for _, shard := range wave.Shards {
		for _, surfaceID := range shard.SurfaceIDs {
			selected[surfaceID] = true
		}
	}
	if len(selected) != wave.SelectedRows {
		return SurfaceOraclePlanArtifacts{}, fmt.Errorf("surface wave selection is invalid")
	}
	ids := make([]string, 0, len(selected))
	for surfaceID := range selected {
		ids = append(ids, surfaceID)
	}
	sort.Strings(ids)

	waveSHA, proofSHA, manifestSHA := replayBytesSHA256(waveBytes), replayBytesSHA256(proofBytes), replayBytesSHA256(manifestBytes)
	profile := AssuranceProfile{
		SchemaVersion: 1, SurfaceWavePlanSHA256: waveSHA, SourceProfileSHA256: scope.SourceProfileSHA256,
		LedgerSHA256: scope.LedgerSHA256, PolicySHA256: scope.PolicySHA256,
		FixtureManifestSHA256: manifestSHA, LocalProofSHA256: proofSHA,
		Total: len(ids), ByDisposition: map[string]int{}, Rows: make([]AssuranceProfileRow, 0, len(ids)),
		NonDeferredGaps: make([]AssuranceProfileRow, 0, len(ids)), HostedDeferred: []AssuranceProfileRow{},
	}
	inputs := make([]OracleInputRow, 0, len(ids))
	for _, surfaceID := range ids {
		scopeRow, scopeOK := scopeByID[surfaceID]
		proofRow, proofOK := proofByID[surfaceID]
		if !scopeOK || !proofOK || scopeRow.Disposition != proofRow.Disposition {
			return SurfaceOraclePlanArtifacts{}, fmt.Errorf("surface wave row %q lacks matching local proof", surfaceID)
		}
		profileRow := AssuranceProfileRow{SurfaceID: surfaceID, Disposition: scopeRow.Disposition}
		profile.Rows = append(profile.Rows, profileRow)
		profile.NonDeferredGaps = append(profile.NonDeferredGaps, profileRow)
		profile.ByDisposition[scopeRow.Disposition]++
		inputs = append(inputs, OracleInputRow{
			SurfaceID: surfaceID, Disposition: scopeRow.Disposition,
			RuntimeObserved: proofRow.RuntimeObserved, BehaviorObserved: proofRow.BehaviorObserved,
			CompilePassed: proofRow.CompilePassed, Deployable: scopeRow.Disposition == deterministicMockRequired,
		})
	}
	plan, err := planOracle(inputs)
	if err != nil {
		return SurfaceOraclePlanArtifacts{}, err
	}
	plan.Candidate, plan.Tools = proof.Candidate, proof.Tools
	plan.SurfaceWavePlanSHA256, plan.LocalProofSHA256 = waveSHA, proofSHA
	profilePath := filepath.Join(temp, "ASSURANCE_PROFILE.json")
	if err := WriteNewJSON(profilePath, profile); err != nil {
		return SurfaceOraclePlanArtifacts{}, err
	}
	profileBytes, err := os.ReadFile(profilePath)
	if err != nil {
		return SurfaceOraclePlanArtifacts{}, err
	}
	plan.ProfileSHA256 = replayBytesSHA256(profileBytes)
	planPath := filepath.Join(temp, "ORACLE_PLAN.json")
	if err := WriteNewJSON(planPath, plan); err != nil {
		return SurfaceOraclePlanArtifacts{}, err
	}
	planBytes, err := os.ReadFile(planPath)
	if err != nil {
		return SurfaceOraclePlanArtifacts{}, err
	}
	authority := ExclusionAuthority{
		Candidate: plan.Candidate, Tools: plan.Tools, PlanSHA256: replayBytesSHA256(planBytes),
		ProfileSHA256: plan.ProfileSHA256, LocalProofSHA256: proofSHA, PolicySHA256: scope.PolicySHA256,
		SalesforceParityCredit: 0, Rows: []ExclusionPolicyRow{},
	}
	if err := WriteNewJSON(filepath.Join(temp, "EXCLUSION_AUTHORITY.json"), authority); err != nil {
		return SurfaceOraclePlanArtifacts{}, err
	}
	if err := os.Remove(rebuiltPath); err != nil {
		return SurfaceOraclePlanArtifacts{}, err
	}
	if err := os.Rename(temp, request.OutputPath); err != nil {
		return SurfaceOraclePlanArtifacts{}, err
	}
	return SurfaceOraclePlanArtifacts{Profile: profile, Plan: plan, Authority: authority}, nil
}
