package corpusassurance

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/glade-sh/glade/tools/internal/compat"
)

// LocalProofPlanRequest derives the complete local-proof input set from the
// sealed assurance inputs. It has no surface-selection field by design.
type LocalProofPlanRequest struct {
	InventoryPath     string
	RootManifestPath  string
	SourceProfilePath string
	SealedUsagePath   string
	LedgerPath        string
	PolicyPath        string
	DecisionPath      string
	FixtureRoot       string
	ProfilePath       string
	UsagePath         string
	LocalDecisionPath string
	ManifestPath      string
}

type localProofFixtureCandidate struct {
	entry LocalProofFixture
	owned map[string]bool
}

// BuildLocalProofPlan seals the local profile, usage, decision, and fixture
// manifest from the current authoritative inputs. Hosted-deferred rows are
// intentionally omitted because they are exclusions, not local proof work.
func BuildLocalProofPlan(request LocalProofPlanRequest) (LocalProofFixtureManifest, error) {
	paths := []string{request.InventoryPath, request.RootManifestPath, request.SourceProfilePath, request.SealedUsagePath, request.LedgerPath, request.PolicyPath, request.DecisionPath, request.FixtureRoot, request.ProfilePath, request.UsagePath, request.LocalDecisionPath, request.ManifestPath}
	for _, path := range paths {
		if !filepath.IsAbs(path) {
			return LocalProofFixtureManifest{}, fmt.Errorf("absolute local-proof plan paths are required")
		}
	}
	for _, path := range []string{request.ProfilePath, request.UsagePath, request.LocalDecisionPath, request.ManifestPath} {
		if _, err := os.Lstat(path); err == nil {
			return LocalProofFixtureManifest{}, fmt.Errorf("local-proof plan output already exists: %s", path)
		} else if !os.IsNotExist(err) {
			return LocalProofFixtureManifest{}, err
		}
	}
	if stat, err := os.Stat(request.FixtureRoot); err != nil || !stat.IsDir() {
		return LocalProofFixtureManifest{}, fmt.Errorf("fixture root is not a directory: %s", request.FixtureRoot)
	}

	sealed, _, err := readExactJSONBytes[SealedCorpusUsage](request.SealedUsagePath)
	if err != nil {
		return LocalProofFixtureManifest{}, fmt.Errorf("read sealed usage: %w", err)
	}
	temp, err := os.MkdirTemp("", "glade-assurance-local-proof-plan-*")
	if err != nil {
		return LocalProofFixtureManifest{}, err
	}
	defer os.RemoveAll(temp)
	rebuilt, err := BuildSealedCorpusUsage(request.InventoryPath, request.LedgerPath, request.RootManifestPath, request.SourceProfilePath, request.PolicyPath, request.DecisionPath, filepath.Join(temp, "CORPUS_USAGE.json"))
	if err != nil || !reflect.DeepEqual(rebuilt, sealed) {
		return LocalProofFixtureManifest{}, fmt.Errorf("sealed usage does not match authoritative recomputation")
	}
	sourceRows, sourceBytes, err := readAssuranceProfileRows(request.SourceProfilePath)
	if err != nil {
		return LocalProofFixtureManifest{}, fmt.Errorf("read source profile: %w", err)
	}
	required, err := oracleRequiredSurfaceIDs(sealed.Reconciliation)
	if err != nil {
		return LocalProofFixtureManifest{}, err
	}
	dispositions := make(map[string]string, len(sourceRows))
	for _, row := range sourceRows {
		if row.SurfaceID == "" || row.Disposition == "" || dispositions[row.SurfaceID] != "" {
			return LocalProofFixtureManifest{}, fmt.Errorf("invalid or duplicate source profile surface %q", row.SurfaceID)
		}
		dispositions[row.SurfaceID] = row.Disposition
	}
	localRequired := make(map[string]string)
	localProfileRows := make([]LocalProofProfileRow, 0, len(required))
	localUsageRows := make([]LocalProofUsageEntry, 0, len(required))
	for _, surfaceID := range required {
		disposition := dispositions[surfaceID]
		if disposition == "" {
			return LocalProofFixtureManifest{}, fmt.Errorf("required surface %q is absent from source profile", surfaceID)
		}
		if !assuranceProfileRequiresFixture(AssuranceProfileRow{SurfaceID: surfaceID, Disposition: disposition}) {
			continue
		}
		localRequired[surfaceID] = disposition
		localProfileRows = append(localProfileRows, LocalProofProfileRow{SurfaceID: surfaceID})
		localUsageRows = append(localUsageRows, LocalProofUsageEntry{SurfaceID: surfaceID})
	}
	sort.Slice(localProfileRows, func(i, j int) bool { return localProfileRows[i].SurfaceID < localProfileRows[j].SurfaceID })
	sort.Slice(localUsageRows, func(i, j int) bool { return localUsageRows[i].SurfaceID < localUsageRows[j].SurfaceID })

	entries, err := discoverLocalProofFixtures(request.FixtureRoot, localRequired)
	if err != nil {
		return LocalProofFixtureManifest{}, err
	}
	manifest := selectLocalProofFixtures(entries)
	owned := make(map[string]bool)
	for _, fixture := range manifest.Fixtures {
		for _, surfaceID := range fixture.OwnedSurfaceIDs {
			owned[surfaceID] = true
		}
	}
	missing := make([]string, 0)
	for surfaceID := range localRequired {
		if !owned[surfaceID] {
			missing = append(missing, surfaceID)
		}
	}
	if len(missing) != 0 {
		sort.Strings(missing)
		return LocalProofFixtureManifest{}, fmt.Errorf("missing local-proof fixtures: %s", strings.Join(missing, ", "))
	}

	profile := LocalProofProfile{SchemaVersion: 1, Rows: localProfileRows}
	usage := LocalProofUsage{SchemaVersion: 1, Usage: localUsageRows}
	if err := WriteNewJSON(request.ProfilePath, profile); err != nil {
		return LocalProofFixtureManifest{}, err
	}
	if err := WriteNewJSON(request.UsagePath, usage); err != nil {
		return LocalProofFixtureManifest{}, err
	}
	if err := WriteNewJSON(request.ManifestPath, manifest); err != nil {
		return LocalProofFixtureManifest{}, err
	}
	profileSHA, err := proofInputSHA256(request.ProfilePath)
	if err != nil {
		return LocalProofFixtureManifest{}, err
	}
	usageSHA, err := proofInputSHA256(request.UsagePath)
	if err != nil {
		return LocalProofFixtureManifest{}, err
	}
	manifestSHA, err := proofInputSHA256(request.ManifestPath)
	if err != nil {
		return LocalProofFixtureManifest{}, err
	}
	decision := LocalProofDecision{SchemaVersion: 1, ProfileSHA256: profileSHA, UsageSHA256: usageSHA, FixtureManifestSHA256: manifestSHA}
	for _, row := range localProfileRows {
		decision.Decisions = append(decision.Decisions, LocalProofDecisionRow{SurfaceID: row.SurfaceID, RequireLocalProof: true})
	}
	if err := WriteNewJSON(request.LocalDecisionPath, decision); err != nil {
		return LocalProofFixtureManifest{}, err
	}
	if replayBytesSHA256(sourceBytes) != sealed.ProfileSHA256 {
		return LocalProofFixtureManifest{}, fmt.Errorf("sealed local-proof lineage changed during planning")
	}
	return manifest, nil
}

func discoverLocalProofFixtures(root string, required map[string]string) ([]localProofFixtureCandidate, error) {
	items, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	candidates := make([]localProofFixtureCandidate, 0)
	for _, item := range items {
		if item.IsDir() || !strings.HasSuffix(item.Name(), ".json") {
			continue
		}
		path := filepath.Join(root, item.Name())
		fixture, err := compat.LoadFile(path)
		if err != nil {
			continue
		}
		if fixture.Name == "" {
			continue
		}
		if err := compat.Validate(fixture); err != nil {
			continue
		}
		sha256, err := localProofPlanFileSHA256(path)
		if err != nil {
			return nil, err
		}
		entry := LocalProofFixture{ID: fixture.Name, Name: fixture.Name, Path: path, SHA256: sha256}
		owned := make(map[string]bool)
		for surfaceID, disposition := range required {
			if disposition != localProofDispositionForCommand(fixture.Command.Kind) || !fixtureOwnsSurface(fixture, surfaceID) {
				continue
			}
			if !localProofEvidenceKindMatches(disposition, fixture.Command.Kind, fixtureEvidenceKind(fixture, surfaceID)) {
				continue
			}
			entry.Disposition = disposition
			owned[surfaceID] = true
		}
		if len(owned) == 0 {
			continue
		}
		entry.OwnedSurfaceIDs = sortedSet(owned)
		if err := validateLocalProofFixtureIdentity(entry, fixture); err != nil {
			continue
		}
		candidates = append(candidates, localProofFixtureCandidate{entry: entry, owned: owned})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if len(candidates[i].owned) != len(candidates[j].owned) {
			return len(candidates[i].owned) > len(candidates[j].owned)
		}
		return candidates[i].entry.ID < candidates[j].entry.ID
	})
	return candidates, nil
}

func selectLocalProofFixtures(candidates []localProofFixtureCandidate) LocalProofFixtureManifest {
	covered := make(map[string]bool)
	manifest := LocalProofFixtureManifest{}
	for _, candidate := range candidates {
		selected := false
		for surfaceID := range candidate.owned {
			if !covered[surfaceID] {
				selected = true
				break
			}
		}
		if !selected {
			continue
		}
		manifest.Fixtures = append(manifest.Fixtures, candidate.entry)
		for surfaceID := range candidate.owned {
			covered[surfaceID] = true
		}
	}
	sort.Slice(manifest.Fixtures, func(i, j int) bool { return manifest.Fixtures[i].ID < manifest.Fixtures[j].ID })
	return manifest
}

func fixtureOwnsSurface(fixture compat.Fixture, surfaceID string) bool {
	for _, evidence := range fixture.Evidence {
		if evidence.SurfaceID == surfaceID {
			return true
		}
	}
	return false
}

func fixtureEvidenceKind(fixture compat.Fixture, surfaceID string) string {
	for _, evidence := range fixture.Evidence {
		if evidence.SurfaceID == surfaceID {
			return evidence.Kind
		}
	}
	return ""
}

func localProofDispositionForCommand(command string) string {
	switch command {
	case "exec":
		return localRuntimeRequired
	case "test":
		return deterministicMockRequired
	case "check":
		return compileShapeRequired
	default:
		return ""
	}
}

func sortedSet(values map[string]bool) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func localProofPlanFileSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return replayBytesSHA256(data), nil
}
