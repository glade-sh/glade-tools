package corpusassurance

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	localRuntimeRequired      = "local-runtime-required"
	deterministicMockRequired = "deterministic-mock-required"
	compileShapeRequired      = "compile-shape-required"
)

// LocalProofFixtureManifest records the explicit surfaces a fixture owns.
// Ownership is the only source of fixture-to-surface credit.
type LocalProofFixtureManifest struct {
	Fixtures []LocalProofFixture `json:"fixtures"`
}

type LocalProofFixture struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Path            string   `json:"path"`
	SHA256          string   `json:"sha256"`
	OwnedSurfaceIDs []string `json:"ownedSurfaceIds"`
	Disposition     string   `json:"disposition"`
}

// LocalProofProfile and LocalProofUsage independently name every surface that
// needs local evidence. The decision file binds their exact bytes and the
// selected fixture manifest, so callers cannot narrow a proof run.
type LocalProofProfile struct {
	SchemaVersion int                    `json:"schemaVersion"`
	Rows          []LocalProofProfileRow `json:"rows"`
}

type LocalProofProfileRow struct {
	SurfaceID string `json:"surfaceId"`
}

type LocalProofUsage struct {
	SchemaVersion int                    `json:"schemaVersion"`
	Usage         []LocalProofUsageEntry `json:"usage"`
}

type LocalProofUsageEntry struct {
	SurfaceID string `json:"surfaceId"`
}

type LocalProofDecision struct {
	SchemaVersion         int                     `json:"schemaVersion"`
	ProfileSHA256         string                  `json:"profileSha256"`
	UsageSHA256           string                  `json:"usageSha256"`
	FixtureManifestSHA256 string                  `json:"fixtureManifestSha256"`
	Decisions             []LocalProofDecisionRow `json:"decisions"`
}

type LocalProofDecisionRow struct {
	SurfaceID         string `json:"surfaceId"`
	RequireLocalProof bool   `json:"requireLocalProof"`
}

type localProofInputs struct {
	ProfileSHA256         string
	UsageSHA256           string
	DecisionSHA256        string
	FixtureManifestSHA256 string
	RequiredSurfaceIDs    []string
}

// LocalProofFixtureResult is the receipt produced once per selected fixture.
type LocalProofFixtureResult struct {
	FixtureID       string        `json:"fixtureId"`
	FixtureSHA256   string        `json:"fixtureSha256"`
	Disposition     string        `json:"disposition"`
	CandidateSHA256 string        `json:"candidateSha256"`
	ToolsSHA256     string        `json:"toolsSha256"`
	Receipt         CommandResult `json:"receipt"`
	Operation       string        `json:"operation"`
	StdoutSHA256    string        `json:"stdoutSha256"`
	Stdout          string        `json:"stdout"`
	StderrSHA256    string        `json:"stderrSha256"`
	Stderr          string        `json:"stderr"`
}

// LocalSurfaceProof is the normalized local receipt for one required surface.
type LocalSurfaceProof struct {
	SurfaceID        string `json:"surfaceId"`
	FixtureID        string `json:"fixtureId"`
	FixtureSHA256    string `json:"fixtureSha256"`
	Disposition      string `json:"disposition"`
	CandidateSHA256  string `json:"candidateSha256"`
	ToolsSHA256      string `json:"toolsSha256"`
	RuntimeObserved  bool   `json:"runtimeObserved,omitempty"`
	BehaviorObserved bool   `json:"behaviorObserved,omitempty"`
	CompilePassed    bool   `json:"compilePassed,omitempty"`
	CheckPassed      bool   `json:"checkPassed,omitempty"`
}

type LocalProofRequest struct {
	AttemptPath         string          `json:"attemptPath"`
	ProfilePath         string          `json:"profilePath"`
	UsagePath           string          `json:"usagePath"`
	DecisionPath        string          `json:"decisionPath"`
	FixtureManifestPath string          `json:"fixtureManifestPath"`
	Candidate           RuntimeArtifact `json:"candidate"`
	CandidatePath       string          `json:"candidatePath"`
	Tools               RuntimeArtifact `json:"tools"`
	ToolsPath           string          `json:"toolsPath"`
	ProfileSHA256       string          `json:"profileSha256"`
	UsageSHA256         string          `json:"usageSha256"`
	DecisionSHA256      string          `json:"decisionSha256"`
	OutputPath          string          `json:"-"`
	architecture        func(string) (string, error)
	executor            localProofExecutor
}

type LocalProof struct {
	Status                string                    `json:"status"`
	AttemptSHA256         string                    `json:"attemptSha256"`
	Candidate             RuntimeArtifact           `json:"candidate"`
	Tools                 RuntimeArtifact           `json:"tools"`
	ProfileSHA256         string                    `json:"profileSha256"`
	UsageSHA256           string                    `json:"usageSha256"`
	DecisionSHA256        string                    `json:"decisionSha256"`
	FixtureManifestSHA256 string                    `json:"fixtureManifestSha256"`
	ProfilePath           string                    `json:"profilePath"`
	UsagePath             string                    `json:"usagePath"`
	DecisionPath          string                    `json:"decisionPath"`
	FixtureManifestPath   string                    `json:"fixtureManifestPath"`
	AttemptPath           string                    `json:"attemptPath"`
	CandidatePath         string                    `json:"candidatePath"`
	ToolsPath             string                    `json:"toolsPath"`
	SelectedSurfaceIDs    []string                  `json:"selectedSurfaceIds"`
	RawFixtureResults     []LocalProofFixtureResult `json:"rawFixtureResults"`
	Surfaces              []LocalSurfaceProof       `json:"surfaces"`
}

type localProofCommand struct {
	Path       string
	Args       []string
	Dir        string
	ApexInputs int
}

type localProofExecution struct {
	Receipt   CommandResult
	Validated bool
	Stdout    string
	Stderr    string
}

// ValidateLocalProof rechecks the complete normalized fixture/receipt graph
// before another workflow stage may rely on local evidence.
func ValidateLocalProof(proof LocalProof, manifest LocalProofFixtureManifest) error {
	attempt, err := LoadAssuranceAttempt(proof.AttemptPath)
	if err != nil || proof.AttemptSHA256 != attemptHash(attempt) || proof.Candidate != attempt.Candidate || proof.Tools != attempt.Tools {
		return fmt.Errorf("local proof attempt binding is invalid")
	}
	if proof.Status != "pass" || ValidateRuntimeArtifact(proof.Candidate) != nil || ValidateRuntimeArtifact(proof.Tools) != nil || !sha256Pattern.MatchString(proof.ProfileSHA256) || !sha256Pattern.MatchString(proof.UsageSHA256) || !sha256Pattern.MatchString(proof.DecisionSHA256) || !sha256Pattern.MatchString(proof.FixtureManifestSHA256) {
		return fmt.Errorf("invalid local proof bindings")
	}
	fixtures := make(map[string]LocalProofFixture, len(manifest.Fixtures))
	receiptSpecs := make(map[string]string, len(manifest.Fixtures))
	apexInputs := make(map[string]int, len(manifest.Fixtures))
	owned := make(map[string]LocalProofFixture)
	for _, fixture := range manifest.Fixtures {
		if fixture.ID == "" || fixture.Name == "" || !sha256Pattern.MatchString(fixture.SHA256) || !validLocalProofDisposition(fixture.Disposition) || fixtures[fixture.ID].ID != "" || len(fixture.OwnedSurfaceIDs) == 0 {
			return fmt.Errorf("invalid or duplicate fixture %q", fixture.ID)
		}
		definition, err := loadLocalProofFixture(fixture)
		if err != nil {
			return err
		}
		command, err := localProofCommandForFixture(fixture, definition, "", ".")
		if err != nil {
			return err
		}
		command.ApexInputs, err = localProofApexInputCount(definition)
		if err != nil {
			return err
		}
		fixtures[fixture.ID] = fixture
		receiptSpecs[fixture.ID] = localProofReceiptSpecSHA256(command)
		apexInputs[fixture.ID] = command.ApexInputs
		for _, surfaceID := range fixture.OwnedSurfaceIDs {
			if surfaceID == "" || owned[surfaceID].ID != "" {
				return fmt.Errorf("invalid or duplicate fixture-owned surface %q", surfaceID)
			}
			owned[surfaceID] = fixture
		}
	}
	if len(proof.SelectedSurfaceIDs) == 0 || !sort.StringsAreSorted(proof.SelectedSurfaceIDs) {
		return fmt.Errorf("local proof selected surfaces are missing or unsorted")
	}
	selected := make(map[string]bool, len(proof.SelectedSurfaceIDs))
	selectedFixtures := make(map[string]bool)
	for _, surfaceID := range proof.SelectedSurfaceIDs {
		fixture, exists := owned[surfaceID]
		if surfaceID == "" || selected[surfaceID] || !exists {
			return fmt.Errorf("invalid or unowned selected surface %q", surfaceID)
		}
		selected[surfaceID], selectedFixtures[fixture.ID] = true, true
	}
	raw := make(map[string]LocalProofFixtureResult, len(proof.RawFixtureResults))
	for _, result := range proof.RawFixtureResults {
		fixture, exists := fixtures[result.FixtureID]
		if !exists || raw[result.FixtureID].FixtureID != "" || !selectedFixtures[result.FixtureID] || result.FixtureSHA256 != fixture.SHA256 || result.Disposition != fixture.Disposition || result.CandidateSHA256 != proof.Candidate.SHA256 || result.ToolsSHA256 != proof.Tools.SHA256 || result.Operation != localProofOperation(fixture.Disposition) || !validLocalProofReceipt(result.Receipt, result.Operation, receiptSpecs[result.FixtureID]) || result.StdoutSHA256 != result.Receipt.StdoutSHA256 || result.StderrSHA256 != result.Receipt.StderrSHA256 || replayBytesSHA256([]byte(result.Stdout)) != result.StdoutSHA256 || replayBytesSHA256([]byte(result.Stderr)) != result.StderrSHA256 || !validatesCandidateJSON([]byte(result.Stdout), result.Operation, apexInputs[result.FixtureID]) {
			return fmt.Errorf("invalid local proof fixture receipt %q", result.FixtureID)
		}
		raw[result.FixtureID] = result
	}
	if len(raw) != len(selectedFixtures) {
		return fmt.Errorf("local proof fixture receipt coverage is incomplete")
	}
	surfaces := make(map[string]bool, len(proof.Surfaces))
	for _, surface := range proof.Surfaces {
		fixture, exists := owned[surface.SurfaceID]
		if !exists || !selected[surface.SurfaceID] || surfaces[surface.SurfaceID] || surface.FixtureID != fixture.ID || surface.FixtureSHA256 != fixture.SHA256 || surface.Disposition != fixture.Disposition || surface.CandidateSHA256 != proof.Candidate.SHA256 || surface.ToolsSHA256 != proof.Tools.SHA256 || !validSurfaceObservation(surface) {
			return fmt.Errorf("invalid local proof surface %q", surface.SurfaceID)
		}
		surfaces[surface.SurfaceID] = true
	}
	if len(surfaces) != len(selected) {
		return fmt.Errorf("local proof surface coverage is incomplete")
	}
	return nil
}

// VerifyLocalProofReplay reruns the sealed fixture set and compares its
// retained structured candidate output before a later stage grants credit.
func VerifyLocalProofReplay(proof LocalProof, manifest LocalProofFixtureManifest) error {
	return verifyLocalProofReplay(proof, manifest, proof.CandidatePath, proof.ToolsPath, nil)
}

func verifyLocalProofReplay(proof LocalProof, manifest LocalProofFixtureManifest, candidatePath, toolsPath string, architecture func(string) (string, error)) error {
	if err := ValidateLocalProof(proof, manifest); err != nil {
		return err
	}
	for _, path := range []string{proof.ProfilePath, proof.UsagePath, proof.DecisionPath, proof.FixtureManifestPath, candidatePath, toolsPath} {
		if !filepath.IsAbs(path) {
			return fmt.Errorf("recorded local proof paths must be absolute")
		}
	}
	temp, err := os.MkdirTemp("", "glade-assurance-proof-replay-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)
	replayed, err := RunLocalProof(LocalProofRequest{AttemptPath: proof.AttemptPath, ProfilePath: proof.ProfilePath, UsagePath: proof.UsagePath, DecisionPath: proof.DecisionPath, FixtureManifestPath: proof.FixtureManifestPath, CandidatePath: candidatePath, ToolsPath: toolsPath, OutputPath: filepath.Join(temp, "LOCAL_PROOF.json"), architecture: architecture})
	if err != nil {
		return fmt.Errorf("replay local proof: %w", err)
	}
	if len(replayed.RawFixtureResults) != len(proof.RawFixtureResults) {
		return fmt.Errorf("replayed local proof fixture count changed")
	}
	for i, result := range proof.RawFixtureResults {
		actual := replayed.RawFixtureResults[i]
		if result.FixtureID != actual.FixtureID || result.FixtureSHA256 != actual.FixtureSHA256 || result.Disposition != actual.Disposition || result.Operation != actual.Operation || result.Stdout != actual.Stdout || result.StdoutSHA256 != actual.StdoutSHA256 || result.Stderr != actual.Stderr || result.StderrSHA256 != actual.StderrSHA256 || !sameLocalProofReceipt(result.Receipt, actual.Receipt) {
			return fmt.Errorf("replayed local proof differs for fixture %q", result.FixtureID)
		}
	}
	return nil
}

func sameLocalProofReceipt(left, right CommandResult) bool {
	return equalStrings(left.Command, right.Command) && left.CommandSpecSHA256 == right.CommandSpecSHA256 && left.ExitCode == right.ExitCode && left.StdoutSHA256 == right.StdoutSHA256 && left.StderrSHA256 == right.StderrSHA256 && left.Passed == right.Passed && left.TimedOut == right.TimedOut
}

func validLocalProofReceipt(receipt CommandResult, operation, expectedSpecSHA256 string) bool {
	return validReplayReceipt(receipt, operation, expectedSpecSHA256)
}

func localProofOperation(disposition string) string {
	switch disposition {
	case localRuntimeRequired:
		return "exec"
	case deterministicMockRequired:
		return "test"
	case compileShapeRequired:
		return "check"
	default:
		return ""
	}
}

func validSurfaceObservation(surface LocalSurfaceProof) bool {
	switch surface.Disposition {
	case localRuntimeRequired:
		return surface.RuntimeObserved && !surface.BehaviorObserved && !surface.CompilePassed
	case deterministicMockRequired:
		return !surface.RuntimeObserved && surface.BehaviorObserved && !surface.CompilePassed
	case compileShapeRequired:
		return !surface.RuntimeObserved && !surface.BehaviorObserved && surface.CompilePassed
	default:
		return false
	}
}

type localProofExecutor func(localProofCommand) localProofExecution

// RunLocalProof runs each explicitly mapped fixture once and writes a
// create-only normalized proof after every selected surface has valid evidence.
func RunLocalProof(request LocalProofRequest) (LocalProof, error) {
	attempt, err := LoadAssuranceAttempt(request.AttemptPath)
	if err != nil {
		return LocalProof{}, fmt.Errorf("load assurance attempt: %w", err)
	}
	request.Candidate, request.Tools = attempt.Candidate, attempt.Tools
	inputs, fixturesBySurface, fixtures, selected, err := validateLocalProofRequest(request)
	if err != nil {
		return LocalProof{}, err
	}
	stagedCandidate, cleanupCandidate, err := stageLocalProofCandidate(request.CandidatePath, request.Candidate)
	if err != nil {
		return LocalProof{}, err
	}
	defer cleanupCandidate()
	executionRequest := request
	executionRequest.CandidatePath = stagedCandidate
	if _, err := os.Lstat(request.OutputPath); err == nil {
		return LocalProof{}, fmt.Errorf("local proof output already exists: %s", request.OutputPath)
	} else if !os.IsNotExist(err) {
		return LocalProof{}, err
	}

	executor := request.executor
	if executor == nil {
		executor = runLocalProofCommand
	}
	raw := make([]LocalProofFixtureResult, 0, len(fixtures))
	byFixtureID := make(map[string]LocalProofFixtureResult, len(fixtures))
	for _, fixture := range fixtures {
		command, cleanup, err := materializeLocalProofFixture(fixture, executionRequest.CandidatePath)
		if err != nil {
			return LocalProof{}, err
		}
		if err := validateReplayRuntimeBindings(ReplayRequest{Candidate: executionRequest.Candidate, CandidatePath: executionRequest.CandidatePath, Tools: executionRequest.Tools, ToolsPath: executionRequest.ToolsPath, architecture: executionRequest.architecture}); err != nil {
			cleanup()
			return LocalProof{}, err
		}
		execution := executor(command)
		cleanup()
		result := LocalProofFixtureResult{
			FixtureID: fixture.ID, FixtureSHA256: fixture.SHA256, Disposition: fixture.Disposition,
			CandidateSHA256: request.Candidate.SHA256, ToolsSHA256: request.Tools.SHA256,
			Receipt: execution.Receipt, Operation: command.Args[0],
			StdoutSHA256: execution.Receipt.StdoutSHA256, Stdout: execution.Stdout,
			StderrSHA256: execution.Receipt.StderrSHA256, Stderr: execution.Stderr,
		}
		if err := validateLocalProofFixtureResult(fixture, result, command, execution.Validated); err != nil {
			return LocalProof{}, err
		}
		result.Receipt.CommandSpecSHA256 = localProofReceiptSpecSHA256(command)
		raw = append(raw, result)
		byFixtureID[fixture.ID] = result
	}
	if err := verifyLocalProofFiles(request, inputs, fixtures); err != nil {
		return LocalProof{}, err
	}
	if err := validateReplayRuntimeBindings(ReplayRequest{Candidate: executionRequest.Candidate, CandidatePath: executionRequest.CandidatePath, Tools: executionRequest.Tools, ToolsPath: executionRequest.ToolsPath, architecture: executionRequest.architecture}); err != nil {
		return LocalProof{}, err
	}
	if err := validateReplayRuntimeBindings(ReplayRequest{Candidate: request.Candidate, CandidatePath: request.CandidatePath, Tools: request.Tools, ToolsPath: request.ToolsPath, architecture: request.architecture}); err != nil {
		return LocalProof{}, err
	}

	proof := LocalProof{
		Status: "pass", AttemptSHA256: attemptHash(attempt), Candidate: request.Candidate, Tools: request.Tools,
		ProfileSHA256: inputs.ProfileSHA256, UsageSHA256: inputs.UsageSHA256,
		DecisionSHA256: inputs.DecisionSHA256, FixtureManifestSHA256: inputs.FixtureManifestSHA256,
		ProfilePath: request.ProfilePath, UsagePath: request.UsagePath, DecisionPath: request.DecisionPath,
		FixtureManifestPath: request.FixtureManifestPath, AttemptPath: request.AttemptPath, CandidatePath: request.CandidatePath, ToolsPath: request.ToolsPath,
		SelectedSurfaceIDs: selected, RawFixtureResults: raw,
	}
	for _, surfaceID := range selected {
		fixture := fixturesBySurface[surfaceID]
		result := byFixtureID[fixture.ID]
		surface := LocalSurfaceProof{
			SurfaceID: surfaceID, FixtureID: fixture.ID, FixtureSHA256: fixture.SHA256, Disposition: fixture.Disposition,
			CandidateSHA256: result.CandidateSHA256, ToolsSHA256: result.ToolsSHA256,
		}
		switch fixture.Disposition {
		case localRuntimeRequired:
			surface.RuntimeObserved = true
		case deterministicMockRequired:
			surface.BehaviorObserved = true
		case compileShapeRequired:
			surface.CompilePassed = true
		}
		proof.Surfaces = append(proof.Surfaces, surface)
	}
	if err := ValidateLocalProof(proof, LocalProofFixtureManifest{Fixtures: fixtures}); err != nil {
		return LocalProof{}, err
	}
	if err := WriteNewJSON(request.OutputPath, proof); err != nil {
		return LocalProof{}, fmt.Errorf("write local proof: %w", err)
	}
	return proof, nil
}

func stageLocalProofCandidate(path string, artifact RuntimeArtifact) (string, func(), error) {
	source, err := os.Open(path)
	if err != nil {
		return "", nil, err
	}
	defer source.Close()
	directory, err := os.MkdirTemp("", "glade-assurance-candidate-*")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(directory) }
	staged := filepath.Join(directory, "glade")
	target, err := os.OpenFile(staged, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o500)
	if err != nil {
		cleanup()
		return "", nil, err
	}
	_, copyErr := io.Copy(target, source)
	syncErr := target.Sync()
	closeErr := target.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil {
		cleanup()
		return "", nil, fmt.Errorf("stage candidate: %v %v %v", copyErr, syncErr, closeErr)
	}
	sha256, err := replayFileSHA256(staged)
	if err != nil || sha256 != artifact.SHA256 {
		cleanup()
		return "", nil, fmt.Errorf("staged candidate binding mismatch")
	}
	return staged, cleanup, nil
}

func validateLocalProofRequest(request LocalProofRequest) (localProofInputs, map[string]LocalProofFixture, []LocalProofFixture, []string, error) {
	if request.OutputPath == "" || !filepath.IsAbs(request.OutputPath) || request.ProfilePath == "" || !filepath.IsAbs(request.ProfilePath) || request.UsagePath == "" || !filepath.IsAbs(request.UsagePath) || request.DecisionPath == "" || !filepath.IsAbs(request.DecisionPath) || request.FixtureManifestPath == "" || !filepath.IsAbs(request.FixtureManifestPath) {
		return localProofInputs{}, nil, nil, nil, fmt.Errorf("absolute input and output paths are required")
	}
	if err := ValidateRuntimeArtifact(request.Candidate); err != nil {
		return localProofInputs{}, nil, nil, nil, fmt.Errorf("candidate: %w", err)
	}
	if err := ValidateRuntimeArtifact(request.Tools); err != nil {
		return localProofInputs{}, nil, nil, nil, fmt.Errorf("tools: %w", err)
	}
	if err := validateReplayRuntimeBindings(ReplayRequest{Candidate: request.Candidate, CandidatePath: request.CandidatePath, Tools: request.Tools, ToolsPath: request.ToolsPath, architecture: request.architecture}); err != nil {
		return localProofInputs{}, nil, nil, nil, err
	}
	inputs, err := loadLocalProofInputs(request)
	if err != nil {
		return localProofInputs{}, nil, nil, nil, err
	}

	manifest, err := loadLocalProofFixtureManifest(request.FixtureManifestPath, inputs.FixtureManifestSHA256)
	if err != nil {
		return localProofInputs{}, nil, nil, nil, err
	}
	selected := append([]string(nil), inputs.RequiredSurfaceIDs...)
	selectedSet := stringSet(selected)

	bySurface := make(map[string]LocalProofFixture, len(selectedSet))
	selectedFixtures := make([]LocalProofFixture, 0, len(manifest.Fixtures))
	fixtureIDs := make(map[string]bool, len(manifest.Fixtures))
	for _, fixture := range manifest.Fixtures {
		if fixture.ID == "" || fixture.Name == "" || fixtureIDs[fixture.ID] || !sha256Pattern.MatchString(fixture.SHA256) || !validLocalProofDisposition(fixture.Disposition) {
			return localProofInputs{}, nil, nil, nil, fmt.Errorf("invalid or duplicate fixture %q", fixture.ID)
		}
		fixtureIDs[fixture.ID] = true
		owned := make(map[string]bool, len(fixture.OwnedSurfaceIDs))
		selectedFixture := false
		for _, surfaceID := range fixture.OwnedSurfaceIDs {
			if surfaceID == "" || owned[surfaceID] {
				return localProofInputs{}, nil, nil, nil, fmt.Errorf("fixture %q has invalid or duplicate owned surface %q", fixture.ID, surfaceID)
			}
			owned[surfaceID] = true
			if !selectedSet[surfaceID] {
				continue
			}
			if _, exists := bySurface[surfaceID]; exists {
				return localProofInputs{}, nil, nil, nil, fmt.Errorf("duplicate fixture evidence for %q", surfaceID)
			}
			bySurface[surfaceID] = fixture
			selectedFixture = true
		}
		if len(owned) == 0 {
			return localProofInputs{}, nil, nil, nil, fmt.Errorf("fixture %q has no owned surfaces", fixture.ID)
		}
		if selectedFixture {
			selectedFixtures = append(selectedFixtures, fixture)
		}
	}
	for _, surfaceID := range selected {
		if _, exists := bySurface[surfaceID]; !exists {
			return localProofInputs{}, nil, nil, nil, fmt.Errorf("missing fixture evidence for %q", surfaceID)
		}
	}
	selectedFixtureIDs := make(map[string]bool, len(selectedFixtures))
	for _, fixture := range selectedFixtures {
		selectedFixtureIDs[fixture.ID] = true
	}
	for _, fixture := range manifest.Fixtures {
		if !selectedFixtureIDs[fixture.ID] {
			if _, err := loadLocalProofFixture(fixture); err != nil {
				return localProofInputs{}, nil, nil, nil, err
			}
		}
	}
	sort.Slice(selectedFixtures, func(i, j int) bool { return selectedFixtures[i].ID < selectedFixtures[j].ID })
	return inputs, bySurface, selectedFixtures, selected, nil
}

func loadLocalProofFixtureManifest(path, expectedSHA256 string) (LocalProofFixtureManifest, error) {
	manifest, data, err := readExactJSONBytes[LocalProofFixtureManifest](path)
	if err != nil {
		return LocalProofFixtureManifest{}, fmt.Errorf("read fixture manifest: %w", err)
	}
	if replayBytesSHA256(data) != expectedSHA256 {
		return LocalProofFixtureManifest{}, fmt.Errorf("fixture manifest binding mismatch")
	}
	return manifest, nil
}

func loadLocalProofInputs(request LocalProofRequest) (localProofInputs, error) {
	profile, profileBytes, err := readExactJSONBytes[LocalProofProfile](request.ProfilePath)
	if err != nil {
		return localProofInputs{}, fmt.Errorf("read local-proof profile: %w", err)
	}
	usage, usageBytes, err := readExactJSONBytes[LocalProofUsage](request.UsagePath)
	if err != nil {
		return localProofInputs{}, fmt.Errorf("read local-proof usage: %w", err)
	}
	decision, decisionBytes, err := readExactJSONBytes[LocalProofDecision](request.DecisionPath)
	if err != nil {
		return localProofInputs{}, fmt.Errorf("read local-proof decision: %w", err)
	}
	inputs := localProofInputs{
		ProfileSHA256:  replayBytesSHA256(profileBytes),
		UsageSHA256:    replayBytesSHA256(usageBytes),
		DecisionSHA256: replayBytesSHA256(decisionBytes),
	}
	if profile.SchemaVersion != 1 || usage.SchemaVersion != 1 || decision.SchemaVersion != 1 || decision.ProfileSHA256 != inputs.ProfileSHA256 || decision.UsageSHA256 != inputs.UsageSHA256 || !sha256Pattern.MatchString(decision.FixtureManifestSHA256) {
		return localProofInputs{}, fmt.Errorf("sealed local-proof inputs do not bind")
	}
	profileIDs, err := localProofProfileIDs(profile.Rows)
	if err != nil {
		return localProofInputs{}, err
	}
	usageIDs, err := localProofUsageIDs(usage.Usage)
	if err != nil {
		return localProofInputs{}, err
	}
	decisions, err := localProofDecisions(decision.Decisions)
	if err != nil {
		return localProofInputs{}, err
	}
	usageSet := stringSet(usageIDs)
	for _, surfaceID := range usageIDs {
		if !profileIDs[surfaceID] {
			return localProofInputs{}, fmt.Errorf("usage surface %q is absent from profile", surfaceID)
		}
	}
	required := make([]string, 0, len(profileIDs))
	for surfaceID := range profileIDs {
		if !usageSet[surfaceID] {
			return localProofInputs{}, fmt.Errorf("profile surface %q is absent from usage", surfaceID)
		}
		require, ok := decisions[surfaceID]
		if !ok || !require {
			return localProofInputs{}, fmt.Errorf("profile surface %q is not required by decisions", surfaceID)
		}
		required = append(required, surfaceID)
	}
	sort.Strings(required)
	if len(required) == 0 {
		return localProofInputs{}, fmt.Errorf("no usage surface requires local proof")
	}
	inputs.FixtureManifestSHA256 = decision.FixtureManifestSHA256
	inputs.RequiredSurfaceIDs = required
	return inputs, nil
}

func localProofProfileIDs(rows []LocalProofProfileRow) (map[string]bool, error) {
	ids := make(map[string]bool, len(rows))
	for _, row := range rows {
		if row.SurfaceID == "" || ids[row.SurfaceID] {
			return nil, fmt.Errorf("invalid or duplicate profile surface %q", row.SurfaceID)
		}
		ids[row.SurfaceID] = true
	}
	return ids, nil
}
func localProofUsageIDs(rows []LocalProofUsageEntry) ([]string, error) {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.SurfaceID)
	}
	return normalizedSurfaceIDs(ids)
}
func localProofDecisions(rows []LocalProofDecisionRow) (map[string]bool, error) {
	values := make(map[string]bool, len(rows))
	for _, row := range rows {
		if row.SurfaceID == "" {
			return nil, fmt.Errorf("invalid decision surface")
		}
		if _, exists := values[row.SurfaceID]; exists {
			return nil, fmt.Errorf("duplicate decision surface %q", row.SurfaceID)
		}
		values[row.SurfaceID] = row.RequireLocalProof
	}
	return values, nil
}

func normalizedSurfaceIDs(ids []string) ([]string, error) {
	if len(ids) == 0 {
		return nil, fmt.Errorf("at least one required surface is needed")
	}
	result := append([]string(nil), ids...)
	sort.Strings(result)
	for i, id := range result {
		if id == "" || i > 0 && id == result[i-1] {
			return nil, fmt.Errorf("invalid or duplicate required surface %q", id)
		}
	}
	return result, nil
}

func stringSet(values []string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func verifyLocalProofFiles(request LocalProofRequest, inputs localProofInputs, fixtures []LocalProofFixture) error {
	refreshed, err := loadLocalProofInputs(request)
	if err != nil || refreshed.ProfileSHA256 != inputs.ProfileSHA256 || refreshed.UsageSHA256 != inputs.UsageSHA256 || refreshed.DecisionSHA256 != inputs.DecisionSHA256 || refreshed.FixtureManifestSHA256 != inputs.FixtureManifestSHA256 || !equalStrings(refreshed.RequiredSurfaceIDs, inputs.RequiredSurfaceIDs) {
		return fmt.Errorf("sealed local-proof inputs changed during execution")
	}
	if _, err := loadLocalProofFixtureManifest(request.FixtureManifestPath, inputs.FixtureManifestSHA256); err != nil {
		return err
	}
	return nil
}

func localProofEvidenceKind(disposition string) string {
	switch disposition {
	case localRuntimeRequired:
		return "runtime"
	case deterministicMockRequired:
		return "behavior"
	case compileShapeRequired:
		return "compile"
	default:
		return ""
	}
}

func runLocalProofCommand(command localProofCommand) localProofExecution {
	receipt, stdout, stderr := runReplayCommandOutput(command.Dir, ReplayCommand{Path: command.Path, Args: command.Args, Env: append([]string(nil), fixedReplayEnvironment...), Timeout: 2 * time.Minute})
	return localProofExecution{Receipt: receipt, Validated: receipt.Passed && validatesCandidateJSON(stdout, command.Args[0], command.ApexInputs), Stdout: string(stdout), Stderr: string(stderr)}
}

func validateLocalProofFixtureResult(fixture LocalProofFixture, result LocalProofFixtureResult, command localProofCommand, validated bool) error {
	if result.FixtureID != fixture.ID || result.FixtureSHA256 != fixture.SHA256 || result.Disposition != fixture.Disposition {
		return fmt.Errorf("stale fixture result for %q", fixture.ID)
	}
	operation := command.Args[0]
	expectedSpecSHA256 := commandSpecSHA256(ReplayCommand{Path: command.Path, Args: command.Args, Env: fixedReplayEnvironment, Timeout: 2 * time.Minute})
	if !validated || !validReplayReceipt(result.Receipt, operation, expectedSpecSHA256) {
		return fmt.Errorf("fixture %q lacks a valid %s receipt", fixture.ID, operation)
	}
	return nil
}

func localProofReceiptSpecSHA256(command localProofCommand) string {
	args := append([]string(nil), command.Args...)
	if len(args) >= 3 && args[1] == "--project" {
		args[2] = "."
	}
	return commandSpecSHA256(ReplayCommand{Path: command.Path, Args: args, Env: fixedReplayEnvironment, Timeout: 2 * time.Minute})
}

func validLocalProofDisposition(disposition string) bool {
	switch disposition {
	case localRuntimeRequired, deterministicMockRequired, compileShapeRequired:
		return true
	default:
		return false
	}
}
