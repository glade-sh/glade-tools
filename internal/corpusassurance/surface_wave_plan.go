package corpusassurance

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

const (
	defaultSurfaceWaveFixtures = 32
	defaultSurfaceWaveShards   = 2
	maxSurfaceWaveShards       = 9
)

type SurfaceWavePlanRequest struct {
	ScopePath             string
	ProfilePath           string
	LocalProofPath        string
	FixtureManifestPath   string
	CoveragePath          string
	TerminalAuthorityPath string
	PredecessorIndexPath  string
	FixtureIDs            []string
	MaxFixtures           int
	ShardCount            int
	OutputPath            string
}

type SurfaceWavePlan struct {
	SchemaVersion           int                    `json:"schemaVersion"`
	Kind                    string                 `json:"kind"`
	ScopeSHA256             string                 `json:"scopeSha256"`
	ProfileSHA256           string                 `json:"profileSha256"`
	LocalProofSHA256        string                 `json:"localProofSha256"`
	FixtureManifestSHA256   string                 `json:"fixtureManifestSha256"`
	CoverageSHA256          string                 `json:"coverageSha256"`
	TerminalAuthoritySHA256 string                 `json:"terminalAuthoritySha256,omitempty"`
	PredecessorIndexSHA256  string                 `json:"predecessorIndexSha256,omitempty"`
	Candidate               RuntimeArtifact        `json:"candidate"`
	Tools                   RuntimeArtifact        `json:"tools"`
	FixtureIDs              []string               `json:"fixtureIds,omitempty"`
	MaxFixtures             int                    `json:"maxFixtures"`
	ShardCount              int                    `json:"shardCount"`
	EligibleRows            int                    `json:"eligibleRows"`
	IneligibleRows          int                    `json:"ineligibleRows"`
	SelectedFixtures        int                    `json:"selectedFixtures"`
	SelectedRows            int                    `json:"selectedRows"`
	RemainingOpen           int                    `json:"remainingOpen"`
	Shards                  []SurfaceWavePlanShard `json:"shards"`
}

type SurfaceWavePlanShard struct {
	Index      int                 `json:"index"`
	Fixtures   []LocalProofFixture `json:"fixtures"`
	SurfaceIDs []string            `json:"surfaceIds"`
}

// BuildSurfaceWavePlan selects bounded whole fixtures from the exact open
// all-runtime scope. Callers cannot hand-pick SurfaceIDs.
func BuildSurfaceWavePlan(request SurfaceWavePlanRequest) (SurfaceWavePlan, error) {
	paths := []string{request.ScopePath, request.ProfilePath, request.LocalProofPath, request.FixtureManifestPath, request.CoveragePath, request.OutputPath}
	for _, path := range paths {
		if !filepath.IsAbs(path) {
			return SurfaceWavePlan{}, fmt.Errorf("absolute surface-wave-plan paths are required")
		}
	}
	for _, path := range []string{request.TerminalAuthorityPath, request.PredecessorIndexPath} {
		if path != "" && !filepath.IsAbs(path) {
			return SurfaceWavePlan{}, fmt.Errorf("absolute surface-wave-plan paths are required")
		}
	}
	if _, err := os.Lstat(request.OutputPath); err == nil {
		return SurfaceWavePlan{}, fmt.Errorf("surface wave plan output already exists: %s", request.OutputPath)
	} else if !os.IsNotExist(err) {
		return SurfaceWavePlan{}, err
	}
	maxFixtures := request.MaxFixtures
	if maxFixtures == 0 {
		maxFixtures = defaultSurfaceWaveFixtures
	}
	if maxFixtures < 1 || maxFixtures > defaultSurfaceWaveFixtures {
		return SurfaceWavePlan{}, fmt.Errorf("max fixtures must be between 1 and %d", defaultSurfaceWaveFixtures)
	}
	requestedFixtures := make(map[string]bool, len(request.FixtureIDs))
	for _, fixtureID := range request.FixtureIDs {
		if fixtureID == "" || requestedFixtures[fixtureID] {
			return SurfaceWavePlan{}, fmt.Errorf("invalid or duplicate requested fixture %q", fixtureID)
		}
		requestedFixtures[fixtureID] = true
	}
	if len(requestedFixtures) > maxFixtures {
		return SurfaceWavePlan{}, fmt.Errorf("requested fixtures exceed max fixtures")
	}
	shardCount := request.ShardCount
	if shardCount == 0 {
		shardCount = defaultSurfaceWaveShards
	}
	if shardCount < 1 || shardCount > maxSurfaceWaveShards {
		return SurfaceWavePlan{}, fmt.Errorf("shard count must be between 1 and %d", maxSurfaceWaveShards)
	}

	scope, scopeBytes, err := readExactJSONBytes[SurfaceOracleScope](request.ScopePath)
	if err != nil {
		return SurfaceWavePlan{}, err
	}
	profile, profileBytes, err := readExactJSONBytes[LocalProofProfile](request.ProfilePath)
	if err != nil {
		return SurfaceWavePlan{}, err
	}
	proof, proofBytes, err := readExactJSONBytes[LocalProof](request.LocalProofPath)
	if err != nil {
		return SurfaceWavePlan{}, err
	}
	manifest, manifestBytes, err := readExactJSONBytes[LocalProofFixtureManifest](request.FixtureManifestPath)
	if err != nil {
		return SurfaceWavePlan{}, err
	}
	coverage, coverageBytes, err := readExactJSONBytes[SurfaceLocalProofCoverage](request.CoveragePath)
	if err != nil {
		return SurfaceWavePlan{}, err
	}
	scopeSHA, profileSHA := replayBytesSHA256(scopeBytes), replayBytesSHA256(profileBytes)
	proofSHA, manifestSHA, coverageSHA := replayBytesSHA256(proofBytes), replayBytesSHA256(manifestBytes), replayBytesSHA256(coverageBytes)
	if err := validateSurfaceOracleScope(scope); err != nil || scope.Kind != "all-runtime" {
		return SurfaceWavePlan{}, fmt.Errorf("surface wave scope is invalid")
	}
	if err := ValidateLocalProof(proof, manifest); err != nil {
		return SurfaceWavePlan{}, err
	}
	if profile.SchemaVersion != 1 || proof.ProfileSHA256 != profileSHA || proof.FixtureManifestSHA256 != manifestSHA {
		return SurfaceWavePlan{}, fmt.Errorf("surface wave local-proof bindings are invalid")
	}
	if coverage.SchemaVersion != 1 || coverage.ScopeSHA256 != scopeSHA || coverage.Total != scope.Total || coverage.Covered+coverage.MissingCount != coverage.Total || coverage.MissingCount != len(coverage.Missing) || coverage.Covered != len(profile.Rows) {
		return SurfaceWavePlan{}, fmt.Errorf("surface wave coverage bindings are invalid")
	}

	excluded := make(map[string]bool, coverage.MissingCount)
	authoritySHA := ""
	if request.TerminalAuthorityPath == "" {
		if coverage.MissingCount != 0 || coverage.TerminalAccounting != nil {
			return SurfaceWavePlan{}, fmt.Errorf("surface wave terminal authority is required")
		}
	} else {
		authority, authorityBytes, err := readExactJSONBytes[SurfaceTerminalAuthority](request.TerminalAuthorityPath)
		if err != nil {
			return SurfaceWavePlan{}, err
		}
		authoritySHA = replayBytesSHA256(authorityBytes)
		accounting, err := ApplySurfaceTerminalAuthority(coverage, authority, authoritySHA, authority.FixtureSetSHA256)
		if err != nil || accounting.Remaining != 0 || coverage.TerminalAccounting == nil || !reflect.DeepEqual(*coverage.TerminalAccounting, accounting) || authority.ScopeSHA256 != scopeSHA || authority.SourceProfileSHA256 != scope.SourceProfileSHA256 || authority.LedgerSHA256 != scope.LedgerSHA256 || authority.SupportPolicySHA256 != scope.PolicySHA256 {
			return SurfaceWavePlan{}, fmt.Errorf("surface wave terminal authority bindings are invalid")
		}
		for _, row := range authority.Rows {
			excluded[row.SurfaceID] = true
		}
	}

	scopeRows := make(map[string]SurfaceOracleScopeRow, len(scope.Rows))
	for _, row := range scope.Rows {
		scopeRows[row.SurfaceID] = row
	}
	proofRows := make(map[string]string, len(proof.Surfaces))
	for _, row := range proof.Surfaces {
		proofRows[row.SurfaceID] = row.Disposition
	}
	profileRows := make(map[string]bool, len(profile.Rows))
	for _, row := range profile.Rows {
		scopeRow := scopeRows[row.SurfaceID]
		if scopeRow.SurfaceID == "" || profileRows[row.SurfaceID] || excluded[row.SurfaceID] || proofRows[row.SurfaceID] != scopeRow.Disposition {
			return SurfaceWavePlan{}, fmt.Errorf("invalid or duplicate surface wave profile row %q", row.SurfaceID)
		}
		profileRows[row.SurfaceID] = true
	}
	if len(proofRows) != len(profileRows) || len(profileRows)+len(excluded) != len(scopeRows) {
		return SurfaceWavePlan{}, fmt.Errorf("surface wave profile and terminal rows do not partition scope")
	}

	predecessorStates := make(map[string]string, len(scope.Rows))
	predecessorSHA := ""
	predecessorTerminalAuthoritySHA := ""
	if request.PredecessorIndexPath != "" {
		predecessor, predecessorBytes, err := readExactJSONBytes[SurfaceOracleIndex](request.PredecessorIndexPath)
		if err != nil {
			return SurfaceWavePlan{}, err
		}
		if err := ValidateSurfaceOracleIndex(predecessor); err != nil {
			return SurfaceWavePlan{}, err
		}
		if predecessor.ScopeSHA256 != scopeSHA || predecessor.SourceProfileSHA256 != scope.SourceProfileSHA256 || predecessor.LedgerSHA256 != scope.LedgerSHA256 || predecessor.PolicySHA256 != scope.PolicySHA256 || predecessor.Candidate.Commit != proof.Candidate.Commit || predecessor.Candidate.BinarySHA256 != proof.Candidate.SHA256 || predecessor.Tools.Commit != proof.Tools.Commit || predecessor.Tools.BinarySHA256 != proof.Tools.SHA256 || len(predecessor.Rows) != len(scope.Rows) {
			return SurfaceWavePlan{}, fmt.Errorf("surface wave predecessor bindings are invalid")
		}
		for i, row := range predecessor.Rows {
			if row.SurfaceID != scope.Rows[i].SurfaceID {
				return SurfaceWavePlan{}, fmt.Errorf("surface wave predecessor row set differs from scope")
			}
			predecessorStates[row.SurfaceID] = row.State
		}
		predecessorSHA = replayBytesSHA256(predecessorBytes)
		predecessorTerminalAuthoritySHA = predecessor.TerminalAuthoritySHA256
	}
	if request.PredecessorIndexPath != "" {
		if request.TerminalAuthorityPath != "" && predecessorTerminalAuthoritySHA != authoritySHA {
			return SurfaceWavePlan{}, fmt.Errorf("surface wave predecessor terminal authority binding is invalid")
		}
		for surfaceID := range excluded {
			if predecessorStates[surfaceID] == "open" {
				return SurfaceWavePlan{}, fmt.Errorf("terminal surface %q remains open in predecessor", surfaceID)
			}
		}
	}

	allFixtures := make(map[string]LocalProofFixture, len(manifest.Fixtures))
	for _, fixture := range manifest.Fixtures {
		if fixture.ID == "" || allFixtures[fixture.ID].ID != "" {
			return SurfaceWavePlan{}, fmt.Errorf("invalid or duplicate fixture %q", fixture.ID)
		}
		allFixtures[fixture.ID] = fixture
	}
	owners := make(map[string]string, len(profileRows))
	for _, fixture := range manifest.Fixtures {
		for _, surfaceID := range fixture.OwnedSurfaceIDs {
			row, inScope := scopeRows[surfaceID]
			if !inScope {
				return SurfaceWavePlan{}, fmt.Errorf("fixture %q owns out-of-scope surface %q", fixture.ID, surfaceID)
			}
			if excluded[surfaceID] {
				continue
			}
			if row.Disposition != fixture.Disposition || !profileRows[surfaceID] || owners[surfaceID] != "" {
				return SurfaceWavePlan{}, fmt.Errorf("surface %q does not have one exact fixture owner", surfaceID)
			}
			owners[surfaceID] = fixture.ID
		}
	}
	if len(owners) != len(profileRows) {
		return SurfaceWavePlan{}, fmt.Errorf("surface wave fixture ownership is incomplete")
	}

	fixtureIDs := make(map[string]bool, len(manifest.SalesforceFixtures))
	fixtures := make([]LocalProofFixture, 0, len(manifest.SalesforceFixtures))
	eligibleRows := make(map[string]bool)
	for _, fixture := range manifest.SalesforceFixtures {
		canonical, ok := allFixtures[fixture.ID]
		if !ok || fixtureIDs[fixture.ID] || !reflect.DeepEqual(canonical, fixture) || fixture.SalesforceEligible == nil || !*fixture.SalesforceEligible {
			return SurfaceWavePlan{}, fmt.Errorf("invalid or duplicate Salesforce fixture %q", fixture.ID)
		}
		fixtureIDs[fixture.ID] = true
		for _, surfaceID := range fixture.OwnedSurfaceIDs {
			if !excluded[surfaceID] {
				eligibleRows[surfaceID] = true
			}
		}
	}
	openRows := make(map[string]bool, len(eligibleRows))
	for surfaceID := range eligibleRows {
		if request.PredecessorIndexPath == "" || predecessorStates[surfaceID] == "open" {
			openRows[surfaceID] = true
		}
	}
	availableFixtures := make(map[string]bool, len(manifest.SalesforceFixtures))
	for _, fixture := range manifest.SalesforceFixtures {
		ownedOpen, ownedTerminal := 0, 0
		for _, surfaceID := range fixture.OwnedSurfaceIDs {
			if excluded[surfaceID] {
				ownedTerminal++
				continue
			}
			if openRows[surfaceID] {
				ownedOpen++
			} else {
				ownedTerminal++
			}
		}
		if ownedOpen > 0 && ownedTerminal > 0 {
			return SurfaceWavePlan{}, fmt.Errorf("predecessor splits fixture %q", fixture.ID)
		}
		if ownedOpen > 0 {
			availableFixtures[fixture.ID] = true
			if len(requestedFixtures) == 0 || requestedFixtures[fixture.ID] {
				fixtures = append(fixtures, fixture)
			}
		}
	}
	for fixtureID := range requestedFixtures {
		if !availableFixtures[fixtureID] {
			return SurfaceWavePlan{}, fmt.Errorf("requested fixture %q is not open and Salesforce eligible", fixtureID)
		}
	}
	sort.Slice(fixtures, func(i, j int) bool {
		left, right := surfaceWaveDispositionOrder(fixtures[i].Disposition), surfaceWaveDispositionOrder(fixtures[j].Disposition)
		if left != right {
			return left < right
		}
		leftID, rightID := surfaceWaveFixtureFirstID(fixtures[i]), surfaceWaveFixtureFirstID(fixtures[j])
		leftNamespace, rightNamespace := surfaceWaveSurfaceNamespace(leftID), surfaceWaveSurfaceNamespace(rightID)
		if leftNamespace != rightNamespace {
			return leftNamespace < rightNamespace
		}
		if fixtures[i].Name != fixtures[j].Name {
			return fixtures[i].Name < fixtures[j].Name
		}
		if leftID != rightID {
			return leftID < rightID
		}
		return fixtures[i].ID < fixtures[j].ID
	})
	if len(fixtures) > maxFixtures {
		fixtures = fixtures[:maxFixtures]
	}
	shards := make([]SurfaceWavePlanShard, shardCount)
	for i := range shards {
		shards[i].Index = i
	}
	activeShards := shardCount
	if len(fixtures) < activeShards {
		activeShards = len(fixtures)
	}
	selectedRows := make(map[string]bool)
	for i, fixture := range fixtures {
		shard := i % activeShards
		shards[shard].Fixtures = append(shards[shard].Fixtures, fixture)
		for _, surfaceID := range fixture.OwnedSurfaceIDs {
			if openRows[surfaceID] {
				shards[shard].SurfaceIDs = append(shards[shard].SurfaceIDs, surfaceID)
				selectedRows[surfaceID] = true
			}
		}
	}
	for i := range shards {
		sort.Strings(shards[i].SurfaceIDs)
	}
	selectedFixtureIDs := append([]string(nil), request.FixtureIDs...)
	sort.Strings(selectedFixtureIDs)
	plan := SurfaceWavePlan{
		SchemaVersion: 1, Kind: "all-runtime-wave", ScopeSHA256: scopeSHA, ProfileSHA256: profileSHA, LocalProofSHA256: proofSHA,
		FixtureManifestSHA256: manifestSHA, CoverageSHA256: coverageSHA, TerminalAuthoritySHA256: authoritySHA, PredecessorIndexSHA256: predecessorSHA,
		Candidate: proof.Candidate, Tools: proof.Tools, FixtureIDs: selectedFixtureIDs, MaxFixtures: maxFixtures, ShardCount: shardCount, EligibleRows: len(eligibleRows), IneligibleRows: len(profileRows) - len(eligibleRows), SelectedFixtures: len(fixtures), SelectedRows: len(selectedRows), RemainingOpen: len(openRows) - len(selectedRows), Shards: shards,
	}
	if err := WriteNewJSON(request.OutputPath, plan); err != nil {
		return SurfaceWavePlan{}, err
	}
	return plan, nil
}

func surfaceWaveDispositionOrder(disposition string) int {
	if disposition == localRuntimeRequired {
		return 0
	}
	return 1
}

func surfaceWaveFixtureFirstID(fixture LocalProofFixture) string {
	ids := append([]string(nil), fixture.OwnedSurfaceIDs...)
	sort.Strings(ids)
	if len(ids) == 0 {
		return ""
	}
	return ids[0]
}

func surfaceWaveSurfaceNamespace(surfaceID string) string {
	value := strings.TrimPrefix(surfaceID, "apex:")
	if index := strings.IndexByte(value, '.'); index >= 0 {
		return value[:index]
	}
	return value
}
