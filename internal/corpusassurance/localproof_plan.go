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
	for _, surfaceID := range required {
		disposition := dispositions[surfaceID]
		if disposition == "" {
			return LocalProofFixtureManifest{}, fmt.Errorf("required surface %q is absent from source profile", surfaceID)
		}
		if !assuranceProfileRequiresFixture(AssuranceProfileRow{SurfaceID: surfaceID, Disposition: disposition}) {
			continue
		}
		localRequired[surfaceID] = disposition
	}
	manifest, missing, err := analyzeLocalProofFixtures(request.FixtureRoot, localRequired)
	if err != nil {
		return LocalProofFixtureManifest{}, err
	}
	manifest.SalesforceFixtures, err = selectSalesforceFixtures(manifest.Fixtures)
	if err != nil {
		return LocalProofFixtureManifest{}, err
	}
	if len(missing) != 0 {
		return LocalProofFixtureManifest{}, fmt.Errorf("missing local-proof fixtures: %s", strings.Join(missing, ", "))
	}
	if err := writeLocalProofPlan(localRequired, manifest, request.ProfilePath, request.UsagePath, request.LocalDecisionPath, request.ManifestPath); err != nil {
		return LocalProofFixtureManifest{}, err
	}
	if replayBytesSHA256(sourceBytes) != sealed.ProfileSHA256 {
		return LocalProofFixtureManifest{}, fmt.Errorf("sealed local-proof lineage changed during planning")
	}
	return manifest, nil
}

func analyzeLocalProofFixtures(root string, required map[string]string) (LocalProofFixtureManifest, []string, error) {
	entries, err := discoverLocalProofFixtures(root, required)
	if err != nil {
		return LocalProofFixtureManifest{}, nil, err
	}
	manifest := selectLocalProofFixtures(entries)
	owned := make(map[string]bool)
	for _, fixture := range manifest.Fixtures {
		for _, surfaceID := range fixture.OwnedSurfaceIDs {
			owned[surfaceID] = true
		}
	}
	missing := make([]string, 0)
	for surfaceID := range required {
		if !owned[surfaceID] {
			missing = append(missing, surfaceID)
		}
	}
	sort.Strings(missing)
	return manifest, missing, nil
}

func writeLocalProofPlan(required map[string]string, manifest LocalProofFixtureManifest, profilePath, usagePath, decisionPath, manifestPath string) error {
	ids := make([]string, 0, len(required))
	for surfaceID := range required {
		ids = append(ids, surfaceID)
	}
	sort.Strings(ids)
	profile := LocalProofProfile{SchemaVersion: 1, Rows: make([]LocalProofProfileRow, 0, len(ids))}
	usage := LocalProofUsage{SchemaVersion: 1, Usage: make([]LocalProofUsageEntry, 0, len(ids))}
	decision := LocalProofDecision{SchemaVersion: 1}
	for _, surfaceID := range ids {
		profile.Rows = append(profile.Rows, LocalProofProfileRow{SurfaceID: surfaceID})
		usage.Usage = append(usage.Usage, LocalProofUsageEntry{SurfaceID: surfaceID})
		decision.Decisions = append(decision.Decisions, LocalProofDecisionRow{SurfaceID: surfaceID, RequireLocalProof: true})
	}
	if err := WriteNewJSON(profilePath, profile); err != nil {
		return err
	}
	if err := WriteNewJSON(usagePath, usage); err != nil {
		return err
	}
	if err := WriteNewJSON(manifestPath, manifest); err != nil {
		return err
	}
	var err error
	decision.ProfileSHA256, err = proofInputSHA256(profilePath)
	if err != nil {
		return err
	}
	decision.UsageSHA256, err = proofInputSHA256(usagePath)
	if err != nil {
		return err
	}
	decision.FixtureManifestSHA256, err = proofInputSHA256(manifestPath)
	if err != nil {
		return err
	}
	return WriteNewJSON(decisionPath, decision)
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
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		fixture, metadata, err := decodeLocalProofFixtureWithMetadata(data)
		if err != nil {
			continue
		}
		if fixture.Name == "" {
			continue
		}
		if err := compat.Validate(fixture); err != nil {
			continue
		}
		entry := LocalProofFixture{ID: fixture.Name, Name: fixture.Name, Path: path, SHA256: replayBytesSHA256(data), Operation: fixture.Command.Kind, SalesforceEligible: metadata.Eligible, SalesforceExclusionClass: metadata.ExclusionClass, SalesforceExclusionReason: metadata.ExclusionReason}
		ownedByDisposition := make(map[string]map[string]bool)
		for surfaceID, disposition := range required {
			if !localProofCommandMatchesDisposition(disposition, fixture.Command.Kind, surfaceID) || !fixtureOwnsSurface(fixture, surfaceID) {
				continue
			}
			if !localProofEvidenceKindMatches(disposition, fixture.Command.Kind, fixtureEvidenceKind(fixture, surfaceID)) {
				continue
			}
			if ownedByDisposition[disposition] == nil {
				ownedByDisposition[disposition] = make(map[string]bool)
			}
			ownedByDisposition[disposition][surfaceID] = true
		}
		owned := map[string]bool{}
		for disposition, surfaces := range ownedByDisposition {
			if len(surfaces) > len(owned) || (len(surfaces) == len(owned) && disposition < entry.Disposition) {
				entry.Disposition, owned = disposition, surfaces
			}
		}
		if len(owned) == 0 {
			continue
		}
		entry.OwnedSurfaceIDs = sortedSet(owned)
		if err := validateLocalProofFixtureIdentity(entry, fixture); err != nil {
			filtered := make(map[string]bool)
			for surfaceID := range owned {
				single := entry
				single.OwnedSurfaceIDs = []string{surfaceID}
				if validateLocalProofFixtureIdentity(single, fixture) == nil {
					filtered[surfaceID] = true
				}
			}
			if len(filtered) == 0 {
				continue
			}
			owned = filtered
			entry.OwnedSurfaceIDs = sortedSet(owned)
		}
		if err := validateLocalProofFixtureSalesforceMetadata(entry, metadata); err != nil {
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

func selectSalesforceFixtures(fixtures []LocalProofFixture) ([]LocalProofFixture, error) {
	selected := make([]LocalProofFixture, 0)
	for _, fixture := range fixtures {
		if fixture.SalesforceEligible == nil {
			return nil, fmt.Errorf("fixture %q lacks explicit Salesforce eligibility", fixture.ID)
		}
		if *fixture.SalesforceEligible {
			selected = append(selected, fixture)
		}
	}
	return selected, nil
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

func localProofCommandMatchesDisposition(disposition, command, surfaceID string) bool {
	switch disposition {
	case localRuntimeRequired:
		return command == "exec" || (command == "test" && (strings.HasPrefix(surfaceID, "apex:System.Test.") ||
			surfaceID == "apex:System.StaticResourceCalloutMock" || strings.HasPrefix(surfaceID, "apex:System.StaticResourceCalloutMock.") ||
			surfaceID == "apex:System.MultiStaticResourceCalloutMock" || strings.HasPrefix(surfaceID, "apex:System.MultiStaticResourceCalloutMock.")))
	case deterministicMockRequired:
		return command == "exec" || command == "test"
	case compileShapeRequired:
		return command == "check"
	default:
		return false
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
