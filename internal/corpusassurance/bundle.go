package corpusassurance

import (
	"debug/macho"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// OracleBundleRequest names the sealed inputs staged for Razor. Every input is
// rehashed before publication; OutputPath must be a new private directory.
type OracleBundleRequest struct {
	AttemptPath           string
	ProfilePath           string
	PlanPath              string
	AuthorityPath         string
	ReleaseValidationPath string
	LocalProofPath        string
	FixtureManifestPath   string
	FilterScriptPath      string
	ScratchDefinitionPath string
	ToolsRoot             string
	OutputPath            string
	expectedFilterSHA256  string
}

// OracleBundle is the acyclic receipt for the self-contained Razor staging
// tree. Its SHA-256 is the bundle identity used by every Salesforce shard.
type OracleBundle struct {
	SchemaVersion            int                   `json:"schemaVersion"`
	Candidate                RuntimeArtifact       `json:"candidate"`
	Tools                    RuntimeArtifact       `json:"tools"`
	ToolsAMD64               RuntimeArtifact       `json:"toolsAmd64"`
	ProfileSHA256            string                `json:"profileSha256"`
	OraclePlanSHA256         string                `json:"oraclePlanSha256"`
	ExclusionAuthoritySHA256 string                `json:"exclusionAuthoritySha256"`
	ReleaseValidationSHA256  string                `json:"releaseValidationSha256"`
	AttemptSHA256            string                `json:"attemptSha256"`
	LocalProofSHA256         string                `json:"localProofSha256"`
	LocalProofSummarySHA256  string                `json:"localProofSummarySha256"`
	FixtureManifestSHA256    string                `json:"fixtureManifestSha256"`
	TransportManifestSHA256  string                `json:"transportManifestSha256"`
	FilterSHA256             string                `json:"filterSha256"`
	ScratchDefinitionSHA256  string                `json:"scratchDefinitionSha256"`
	ToolsAMD64SHA256         string                `json:"toolsAmd64Sha256"`
	Fixtures                 []OracleBundleFixture `json:"fixtures"`
}

type oracleTransportManifest struct {
	Fixtures []oracleTransportFixture `json:"fixtures"`
}

type oracleTransportFixture struct {
	ID                 string             `json:"id"`
	Fixture            string             `json:"fixture"`
	Path               string             `json:"path"`
	SHA256             string             `json:"sha256"`
	SourceFiles        []oracleSourceFile `json:"sourceFiles"`
	SurfaceIDs         []string           `json:"surfaceIds"`
	SalesforceEligible bool               `json:"salesforceEligible"`
}

type oracleLocalProofSummary struct {
	Sealed         bool                            `json:"sealed"`
	ManifestSHA256 string                          `json:"manifestSha256"`
	Results        []oracleLocalProofSummaryResult `json:"results"`
}

type oracleLocalProofSummaryResult struct {
	Fixture string          `json:"fixture"`
	Path    string          `json:"path"`
	Status  string          `json:"status"`
	Kind    string          `json:"kind"`
	Result  json.RawMessage `json:"result"`
}

type oracleSourceFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// BuildOracleBundle stages only the derived Salesforce-required fixtures and
// their sealed dependencies, then atomically publishes the new output tree.
func BuildOracleBundle(request OracleBundleRequest) (OracleBundle, error) {
	paths := []string{request.AttemptPath, request.ProfilePath, request.PlanPath, request.AuthorityPath, request.ReleaseValidationPath, request.LocalProofPath, request.FixtureManifestPath, request.FilterScriptPath, request.ScratchDefinitionPath, request.ToolsRoot, request.OutputPath}
	for _, path := range paths {
		if !filepath.IsAbs(path) {
			return OracleBundle{}, fmt.Errorf("absolute oracle bundle paths are required")
		}
	}
	if _, err := os.Lstat(request.OutputPath); err == nil {
		return OracleBundle{}, fmt.Errorf("oracle bundle output already exists: %s", request.OutputPath)
	} else if !os.IsNotExist(err) {
		return OracleBundle{}, err
	}
	profile, profileBytes, err := readExactJSONBytes[AssuranceProfile](request.ProfilePath)
	if err != nil {
		return OracleBundle{}, fmt.Errorf("read assurance profile: %w", err)
	}
	plan, planBytes, err := readExactJSONBytes[OraclePlan](request.PlanPath)
	if err != nil {
		return OracleBundle{}, fmt.Errorf("read oracle plan: %w", err)
	}
	authority, authorityBytes, err := readExactJSONBytes[ExclusionAuthority](request.AuthorityPath)
	if err != nil {
		return OracleBundle{}, fmt.Errorf("read exclusion authority: %w", err)
	}
	proof, proofBytes, err := readExactJSONBytes[LocalProof](request.LocalProofPath)
	if err != nil {
		return OracleBundle{}, fmt.Errorf("read local proof: %w", err)
	}
	manifest, manifestBytes, err := readExactJSONBytes[LocalProofFixtureManifest](request.FixtureManifestPath)
	if err != nil {
		return OracleBundle{}, fmt.Errorf("read fixture manifest: %w", err)
	}
	release, releaseBytes, err := readExactJSONBytes[ReleaseValidation](request.ReleaseValidationPath)
	if err != nil {
		return OracleBundle{}, fmt.Errorf("read release validation: %w", err)
	}
	attempt, attemptBytes, err := readExactJSONBytes[AssuranceAttempt](request.AttemptPath)
	if err != nil || ValidateAssuranceAttempt(attempt) != nil {
		return OracleBundle{}, fmt.Errorf("read sealed assurance attempt")
	}
	if release.AttemptSHA256 != replayBytesSHA256(attemptBytes) || release.Candidate != attempt.Candidate || release.Tools != attempt.Tools {
		return OracleBundle{}, fmt.Errorf("release validation does not bind sealed assurance attempt")
	}
	proofAttempt, proofAttemptBytes, err := readExactJSONBytes[AssuranceAttempt](proof.AttemptPath)
	if err != nil || ValidateAssuranceAttempt(proofAttempt) != nil || proof.AttemptSHA256 != attemptHash(proofAttempt) || replayBytesSHA256(proofAttemptBytes) != replayBytesSHA256(attemptBytes) {
		return OracleBundle{}, fmt.Errorf("local proof does not bind sealed assurance attempt")
	}
	releaseSHA := replayBytesSHA256(releaseBytes)
	profileSHA, planSHA, authoritySHA, proofSHA, manifestSHA := replayBytesSHA256(profileBytes), replayBytesSHA256(planBytes), replayBytesSHA256(authorityBytes), replayBytesSHA256(proofBytes), replayBytesSHA256(manifestBytes)
	if profile.SchemaVersion != 1 || profile.FixtureManifestSHA256 != manifestSHA || profile.LocalProofSHA256 != proofSHA || plan.ProfileSHA256 != profileSHA || plan.Candidate != proof.Candidate || plan.Tools != proof.Tools || authority.Candidate != plan.Candidate || authority.Tools != plan.Tools || authority.PlanSHA256 != planSHA || authority.ProfileSHA256 != profileSHA || authority.LocalProofSHA256 != proofSHA {
		return OracleBundle{}, fmt.Errorf("oracle bundle inputs do not bind")
	}
	if err := ValidateLocalProof(proof, manifest); err != nil {
		return OracleBundle{}, fmt.Errorf("validate local proof: %w", err)
	}
	if err := validateCleanGitRoot(request.ToolsRoot, attempt.Tools.Commit); err != nil {
		return OracleBundle{}, fmt.Errorf("tools source: %w", err)
	}
	if current, err := runtimeArtifactFor(proof.CandidatePath, attempt.Candidate.Commit); err != nil || current != attempt.Candidate {
		return OracleBundle{}, fmt.Errorf("candidate does not match sealed attempt")
	}
	if current, err := executingToolsArtifact(attempt.Tools.Commit); err != nil || current != attempt.Tools {
		return OracleBundle{}, fmt.Errorf("executing tools do not match sealed attempt")
	}
	if err := validateOracleReleaseValidation(release, plan); err != nil {
		return OracleBundle{}, fmt.Errorf("validate release validation: %w", err)
	}
	if err := validateOracleReleaseSources(release, plan); err != nil {
		return OracleBundle{}, fmt.Errorf("validate release provenance: %w", err)
	}
	if current, err := sha256File(request.ReleaseValidationPath); err != nil || current != releaseSHA {
		return OracleBundle{}, fmt.Errorf("release validation changed after validation")
	}
	fixtures, err := oracleBundleFixtures(plan, manifest)
	if err != nil {
		return OracleBundle{}, err
	}
	inputs := map[string]string{request.AttemptPath: replayBytesSHA256(attemptBytes), proof.AttemptPath: replayBytesSHA256(attemptBytes), request.ProfilePath: profileSHA, request.PlanPath: planSHA, request.AuthorityPath: authoritySHA, request.ReleaseValidationPath: releaseSHA, request.LocalProofPath: proofSHA, request.FixtureManifestPath: manifestSHA}
	for _, path := range []string{request.FilterScriptPath, request.ScratchDefinitionPath} {
		hash, err := sha256File(path)
		if err != nil {
			return OracleBundle{}, err
		}
		inputs[path] = hash
	}
	expectedFilterSHA256 := request.expectedFilterSHA256
	if expectedFilterSHA256 == "" {
		expectedFilterSHA256 = "494f1def8b631c415b413e748672707eac038f213657c1ac8cada39a6ea2cd5d"
	}
	if err := validateOracleFilterContract(request.FilterScriptPath, expectedFilterSHA256); err != nil {
		return OracleBundle{}, err
	}
	scratchDefinition, err := os.ReadFile(request.ScratchDefinitionPath)
	if err != nil {
		return OracleBundle{}, err
	}
	if !json.Valid(scratchDefinition) {
		return OracleBundle{}, fmt.Errorf("scratch definition is not valid JSON")
	}
	for _, fixture := range fixtures {
		if hash, err := sha256File(fixture.Path); err != nil || hash != fixture.SHA256 {
			return OracleBundle{}, fmt.Errorf("fixture %q does not match its sealed hash", fixture.ID)
		}
		inputs[fixture.Path] = fixture.SHA256
	}
	parent := filepath.Dir(request.OutputPath)
	temp, err := os.MkdirTemp(parent, ".oracle-bundle-")
	if err != nil {
		return OracleBundle{}, err
	}
	defer os.RemoveAll(temp)
	if err := os.Chmod(temp, 0o700); err != nil {
		return OracleBundle{}, err
	}
	bundleRoot, transportRoot, binRoot := filepath.Join(temp, "bundle"), filepath.Join(temp, "transport"), filepath.Join(temp, "bin")
	for _, path := range []string{bundleRoot, filepath.Join(bundleRoot, "fixtures"), transportRoot, binRoot} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return OracleBundle{}, err
		}
	}
	for _, item := range []struct{ path, name string }{{request.AttemptPath, "ATTEMPT.json"}, {request.ProfilePath, "profile.json"}, {request.PlanPath, "ORACLE_PLAN.json"}, {request.AuthorityPath, "EXCLUSION_AUTHORITY.json"}, {request.ReleaseValidationPath, "RELEASE_VALIDATION.json"}, {request.ScratchDefinitionPath, "corpus-assurance-scratch-def.json"}} {
		if err := copyOracleBundleFile(item.path, filepath.Join(bundleRoot, item.name), 0o600); err != nil {
			return OracleBundle{}, err
		}
	}
	if err := copyOracleBundleFile(request.FilterScriptPath, filepath.Join(transportRoot, "salesforce-first-filter.py"), 0o700); err != nil {
		return OracleBundle{}, err
	}
	toolsAMD64Path := filepath.Join(binRoot, "glade-tools-darwin-amd64")
	toolsAMD64, err := buildAMD64Tools(request.ToolsRoot, toolsAMD64Path, attempt.Tools.Commit)
	if err != nil {
		return OracleBundle{}, fmt.Errorf("build amd64 tools from sealed source: %w", err)
	}
	if toolsAMD64.Commit != plan.Tools.Commit {
		return OracleBundle{}, fmt.Errorf("built amd64 tools do not bind sealed tools commit")
	}
	transport := oracleTransportManifest{Fixtures: make([]oracleTransportFixture, 0, len(fixtures))}
	for _, fixture := range fixtures {
		staged, err := stageOracleBundleFixture(bundleRoot, fixture)
		if err != nil {
			return OracleBundle{}, err
		}
		transport.Fixtures = append(transport.Fixtures, staged)
	}
	transportPath := filepath.Join(bundleRoot, "fixture-manifest.json")
	if err := WriteNewJSON(transportPath, transport); err != nil {
		return OracleBundle{}, err
	}
	transportSHA, err := sha256File(transportPath)
	if err != nil {
		return OracleBundle{}, err
	}
	summary, err := oracleLocalProofSummaryForTransport(proof, transport, transportSHA)
	if err != nil {
		return OracleBundle{}, err
	}
	summaryPath := filepath.Join(bundleRoot, "LOCAL_PROOF_SUMMARY.json")
	if err := WriteNewJSON(summaryPath, summary); err != nil {
		return OracleBundle{}, err
	}
	summarySHA, err := sha256File(summaryPath)
	if err != nil {
		return OracleBundle{}, err
	}
	if err := verifyOracleBundleInputs(inputs); err != nil {
		return OracleBundle{}, err
	}
	if err := validateOracleReleaseSources(release, plan); err != nil {
		return OracleBundle{}, fmt.Errorf("release provenance changed during staging: %w", err)
	}
	if err := validateCleanGitRoot(request.ToolsRoot, attempt.Tools.Commit); err != nil {
		return OracleBundle{}, fmt.Errorf("tools source changed during staging: %w", err)
	}
	if current, err := runtimeArtifactFor(proof.CandidatePath, attempt.Candidate.Commit); err != nil || current != attempt.Candidate {
		return OracleBundle{}, fmt.Errorf("candidate changed during staging")
	}
	if current, err := executingToolsArtifact(attempt.Tools.Commit); err != nil || current != attempt.Tools {
		return OracleBundle{}, fmt.Errorf("executing tools changed during staging")
	}
	if current, err := amd64ToolsArtifactFor(toolsAMD64Path, attempt.Tools.Commit); err != nil || current != toolsAMD64 {
		return OracleBundle{}, fmt.Errorf("amd64 tools changed during staging")
	}
	bundle := OracleBundle{SchemaVersion: 1, Candidate: plan.Candidate, Tools: plan.Tools, ToolsAMD64: toolsAMD64, ProfileSHA256: profileSHA, OraclePlanSHA256: planSHA, ExclusionAuthoritySHA256: authoritySHA, ReleaseValidationSHA256: inputs[request.ReleaseValidationPath], AttemptSHA256: inputs[request.AttemptPath], LocalProofSHA256: proofSHA, LocalProofSummarySHA256: summarySHA, FixtureManifestSHA256: manifestSHA, TransportManifestSHA256: transportSHA, FilterSHA256: inputs[request.FilterScriptPath], ScratchDefinitionSHA256: inputs[request.ScratchDefinitionPath], ToolsAMD64SHA256: toolsAMD64.SHA256, Fixtures: fixtures}
	if err := WriteNewJSON(filepath.Join(bundleRoot, "bundle.json"), bundle); err != nil {
		return OracleBundle{}, err
	}
	if err := os.Rename(temp, request.OutputPath); err != nil {
		return OracleBundle{}, err
	}
	return bundle, nil
}

func validateOracleFilterContract(path, expectedSHA256 string) error {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("Salesforce filter is unavailable")
	}
	hash, hashErr := sha256File(path)
	if hashErr != nil || hash != expectedSHA256 {
		return fmt.Errorf("Salesforce filter lacks the sealed amd64 tools contract")
	}
	return nil
}

func executingToolsArtifact(commit string) (RuntimeArtifact, error) {
	path, err := os.Executable()
	if err != nil {
		return RuntimeArtifact{}, err
	}
	return releaseExecutingTools(path, commit)
}

func buildAMD64Tools(root, output, commit string) (RuntimeArtifact, error) {
	if err := validateCleanGitRoot(root, commit); err != nil {
		return RuntimeArtifact{}, err
	}
	command := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "build", "-o", output, "./cmd/glade-tools")
	command.Dir = root
	command.Env = toolsAMD64BuildEnvironment()
	if data, err := command.CombinedOutput(); err != nil {
		return RuntimeArtifact{}, fmt.Errorf("build darwin/amd64 glade-tools: %w: %s", err, data)
	}
	return amd64ToolsArtifactFor(output, commit)
}

func toolsAMD64BuildEnvironment() []string {
	environment := make([]string, 0, len(os.Environ())+5)
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "CGO_ENABLED=") && !strings.HasPrefix(entry, "GOOS=") && !strings.HasPrefix(entry, "GOARCH=") && !strings.HasPrefix(entry, "GOFLAGS=") && !strings.HasPrefix(entry, "GOWORK=") {
			environment = append(environment, entry)
		}
	}
	return append(environment, "CGO_ENABLED=0", "GOOS=darwin", "GOARCH=amd64", "GOFLAGS=", "GOWORK=off")
}

func validateOracleReleaseValidation(validation ReleaseValidation, plan OraclePlan) error {
	if validation.SchemaVersion != 1 || !filepath.IsAbs(validation.GladeRoot) || !filepath.IsAbs(validation.CandidatePath) || !filepath.IsAbs(validation.ToolsRoot) || !filepath.IsAbs(validation.ToolsPath) {
		return fmt.Errorf("release validation schema is invalid")
	}
	if len(validation.Commands) != 4 {
		return fmt.Errorf("release validation must seal four fixed release checks")
	}
	if validation.Candidate != plan.Candidate || validation.Tools != plan.Tools {
		return fmt.Errorf("release validation artifacts do not match oracle plan")
	}
	if !sha256Pattern.MatchString(validation.AttemptSHA256) || !sha256Pattern.MatchString(validation.ToolsFreezeSHA256) {
		return fmt.Errorf("release validation freeze hash is invalid")
	}
	for index, result := range validation.Commands {
		if !validOracleReleaseCommand(validation, index, result) {
			return fmt.Errorf("release validation check %d is not a passing fixed release check", index+1)
		}
	}
	return nil
}

func validOracleReleaseCommand(validation ReleaseValidation, index int, result ReleaseCommandResult) bool {
	if !result.Passed || result.ExitCode != 0 || result.TimedOut || result.TimeoutMS != releaseValidationTimeout.Milliseconds() || len(result.Command) == 0 || !equalStrings(result.Environment, fixedReleaseEnvironment()) || !sha256Pattern.MatchString(result.ExecutableSHA256) || result.ExecutableAfterSHA256 != result.ExecutableSHA256 || !sha256Pattern.MatchString(result.CommandSpecSHA256) || !sha256Pattern.MatchString(result.StdoutSHA256) || !sha256Pattern.MatchString(result.StderrSHA256) {
		return false
	}
	switch index {
	case 0:
		if len(result.Command) != 3 || result.Command[1] != "test" || result.Command[2] != "./..." || result.WorkingDirectory != validation.GladeRoot {
			return false
		}
	case 1:
		if len(result.Command) != 1 || result.Command[0] != filepath.Join(validation.GladeRoot, "scripts", "smoke.sh") || result.WorkingDirectory != validation.GladeRoot {
			return false
		}
	case 2:
		if len(result.Command) != 3 || result.Command[1] != "test" || result.Command[2] != "./..." || result.WorkingDirectory != validation.ToolsRoot {
			return false
		}
	case 3:
		if len(result.Command) != 1 || result.Command[0] != filepath.Join(validation.ToolsRoot, "scripts", "release-check.sh") || result.WorkingDirectory != validation.ToolsRoot {
			return false
		}
	default:
		return false
	}
	spec := releaseCommandSpecSHA256(releaseCommand{Path: result.Command[0], Args: result.Command[1:], WorkingDirectory: result.WorkingDirectory, Environment: result.Environment, Timeout: time.Duration(result.TimeoutMS) * time.Millisecond})
	return result.CommandSpecSHA256 == spec
}

// ValidateOracleBundle rehashes the staged Razor tree before any Salesforce
// command may rely on it.
func ValidateOracleBundle(bundlePath string) error {
	if !filepath.IsAbs(bundlePath) {
		return fmt.Errorf("absolute bundle path is required")
	}
	bundle, bundleBytes, err := readExactJSONBytes[OracleBundle](bundlePath)
	if err != nil {
		return fmt.Errorf("read oracle bundle: %w", err)
	}
	if len(bundleBytes) == 0 || bundle.SchemaVersion != 1 || ValidateRuntimeArtifact(bundle.Candidate) != nil || ValidateRuntimeArtifact(bundle.Tools) != nil || ValidateRuntimeArtifact(bundle.ToolsAMD64) != nil || bundle.Tools.Arch != "arm64" || bundle.ToolsAMD64.Arch != "amd64" || bundle.ToolsAMD64.Commit != bundle.Tools.Commit || bundle.ToolsAMD64.SHA256 != bundle.ToolsAMD64SHA256 {
		return fmt.Errorf("invalid oracle bundle")
	}
	bundleRoot, outputRoot := filepath.Dir(bundlePath), filepath.Dir(filepath.Dir(bundlePath))
	if staged, err := amd64ToolsArtifactFor(filepath.Join(outputRoot, "bin", "glade-tools-darwin-amd64"), bundle.Tools.Commit); err != nil || staged != bundle.ToolsAMD64 {
		return fmt.Errorf("invalid staged amd64 tools")
	}
	for _, item := range []struct {
		path string
		hash string
	}{
		{filepath.Join(bundleRoot, "profile.json"), bundle.ProfileSHA256},
		{filepath.Join(bundleRoot, "ORACLE_PLAN.json"), bundle.OraclePlanSHA256},
		{filepath.Join(bundleRoot, "EXCLUSION_AUTHORITY.json"), bundle.ExclusionAuthoritySHA256},
		{filepath.Join(bundleRoot, "RELEASE_VALIDATION.json"), bundle.ReleaseValidationSHA256},
		{filepath.Join(bundleRoot, "ATTEMPT.json"), bundle.AttemptSHA256},
		{filepath.Join(bundleRoot, "LOCAL_PROOF_SUMMARY.json"), bundle.LocalProofSummarySHA256},
		{filepath.Join(bundleRoot, "fixture-manifest.json"), bundle.TransportManifestSHA256},
		{filepath.Join(bundleRoot, "corpus-assurance-scratch-def.json"), bundle.ScratchDefinitionSHA256},
		{filepath.Join(outputRoot, "transport", "salesforce-first-filter.py"), bundle.FilterSHA256},
		{filepath.Join(outputRoot, "bin", "glade-tools-darwin-amd64"), bundle.ToolsAMD64SHA256},
	} {
		if !sha256Pattern.MatchString(item.hash) {
			return fmt.Errorf("invalid oracle bundle hash")
		}
		actual, err := sha256File(item.path)
		if err != nil || actual != item.hash {
			return fmt.Errorf("oracle bundle staged input changed")
		}
	}
	profile, _, err := readExactJSONBytes[AssuranceProfile](filepath.Join(bundleRoot, "profile.json"))
	if err != nil || profile.SchemaVersion != 1 || profile.LocalProofSHA256 != bundle.LocalProofSHA256 {
		return fmt.Errorf("invalid staged assurance profile")
	}
	plan, _, err := readExactJSONBytes[OraclePlan](filepath.Join(bundleRoot, "ORACLE_PLAN.json"))
	if err != nil || plan.Candidate != bundle.Candidate || plan.Tools != bundle.Tools || plan.ProfileSHA256 != bundle.ProfileSHA256 {
		return fmt.Errorf("invalid staged oracle plan")
	}
	authority, _, err := readExactJSONBytes[ExclusionAuthority](filepath.Join(bundleRoot, "EXCLUSION_AUTHORITY.json"))
	if err != nil || authority.Candidate != bundle.Candidate || authority.Tools != bundle.Tools || authority.PlanSHA256 != bundle.OraclePlanSHA256 || authority.ProfileSHA256 != bundle.ProfileSHA256 || authority.LocalProofSHA256 != bundle.LocalProofSHA256 {
		return fmt.Errorf("invalid staged exclusion authority")
	}
	attempt, attemptBytes, err := readExactJSONBytes[AssuranceAttempt](filepath.Join(bundleRoot, "ATTEMPT.json"))
	if err != nil || ValidateAssuranceAttempt(attempt) != nil || attempt.Candidate != bundle.Candidate || attempt.Tools != bundle.Tools {
		return fmt.Errorf("invalid staged assurance attempt")
	}
	release, _, err := readExactJSONBytes[ReleaseValidation](filepath.Join(bundleRoot, "RELEASE_VALIDATION.json"))
	if err != nil || replayBytesSHA256(attemptBytes) != release.AttemptSHA256 || validateOracleReleaseValidation(release, plan) != nil {
		return fmt.Errorf("invalid staged release validation")
	}
	manifest, _, err := readExactJSONBytes[oracleTransportManifest](filepath.Join(bundleRoot, "fixture-manifest.json"))
	if err != nil || !validOracleTransportManifest(bundleRoot, manifest, bundle.Fixtures) {
		return fmt.Errorf("invalid staged transport manifest")
	}
	summary, _, err := readExactJSONBytes[oracleLocalProofSummary](filepath.Join(bundleRoot, "LOCAL_PROOF_SUMMARY.json"))
	if err != nil || !summary.Sealed || summary.ManifestSHA256 != bundle.TransportManifestSHA256 || !validOracleLocalSummary(summary, manifest) {
		return fmt.Errorf("invalid staged local proof summary")
	}
	return nil
}

func amd64ToolsArtifactFor(path, commit string) (RuntimeArtifact, error) {
	if !commitPattern.MatchString(commit) {
		return RuntimeArtifact{}, fmt.Errorf("invalid tools commit")
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return RuntimeArtifact{}, fmt.Errorf("amd64 tools must be an executable regular file")
	}
	file, err := macho.Open(path)
	if err != nil || file.Cpu != macho.CpuAmd64 {
		return RuntimeArtifact{}, fmt.Errorf("tools binary is not darwin/amd64")
	}
	hash, err := sha256File(path)
	if err != nil {
		return RuntimeArtifact{}, err
	}
	return RuntimeArtifact{Commit: commit, OS: "darwin", Arch: "amd64", SHA256: hash}, nil
}

func validOracleTransportManifest(bundleRoot string, manifest oracleTransportManifest, selected []OracleBundleFixture) bool {
	if len(manifest.Fixtures) != len(selected) {
		return false
	}
	expected := make(map[string][]string, len(selected))
	for _, fixture := range selected {
		if fixture.ID == "" || expected[fixture.ID] != nil {
			return false
		}
		expected[fixture.ID] = fixture.SurfaceIDs
	}
	for _, fixture := range manifest.Fixtures {
		if fixture.ID == "" || !fixture.SalesforceEligible || !equalStrings(fixture.SurfaceIDs, expected[fixture.ID]) || fixture.Path == "" || filepath.IsAbs(fixture.Path) || strings.HasPrefix(filepath.Clean(fixture.Path), "..") || !sha256Pattern.MatchString(fixture.SHA256) {
			return false
		}
		actual, err := sha256File(filepath.Join(bundleRoot, filepath.FromSlash(fixture.Path)))
		if err != nil || actual != fixture.SHA256 {
			return false
		}
		for _, source := range fixture.SourceFiles {
			if source.Path == "" || filepath.IsAbs(source.Path) || strings.HasPrefix(filepath.Clean(source.Path), "..") || !sha256Pattern.MatchString(source.SHA256) {
				return false
			}
			actual, err := sha256File(filepath.Join(bundleRoot, filepath.FromSlash(source.Path)))
			if err != nil || actual != source.SHA256 {
				return false
			}
		}
		delete(expected, fixture.ID)
	}
	return len(expected) == 0
}

func validOracleLocalSummary(summary oracleLocalProofSummary, manifest oracleTransportManifest) bool {
	fixtures := make(map[string]bool, len(manifest.Fixtures))
	for _, fixture := range manifest.Fixtures {
		fixtures[fixture.Fixture], fixtures[fixture.Path] = true, true
	}
	if len(summary.Results) != len(manifest.Fixtures) {
		return false
	}
	seen := make(map[string]bool, len(summary.Results))
	for _, result := range summary.Results {
		if result.Fixture == "" || result.Path == "" || seen[result.Fixture] || !fixtures[result.Fixture] || !fixtures[result.Path] || result.Status != "exit-0" || (result.Kind != "exec" && result.Kind != "test" && result.Kind != "check") || !json.Valid(result.Result) {
			return false
		}
		seen[result.Fixture] = true
	}
	return len(seen) == len(manifest.Fixtures)
}

func verifyOracleBundleInputs(inputs map[string]string) error {
	for path, expected := range inputs {
		actual, err := sha256File(path)
		if err != nil || actual != expected {
			return fmt.Errorf("oracle bundle input changed during staging")
		}
	}
	return nil
}

func copyOracleBundleFile(source, destination string, mode os.FileMode) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func stageOracleBundleFixture(bundleRoot string, fixture OracleBundleFixture) (oracleTransportFixture, error) {
	data, err := os.ReadFile(fixture.Path)
	if err != nil {
		return oracleTransportFixture{}, err
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		return oracleTransportFixture{}, err
	}
	sources := make([]oracleSourceFile, 0)
	for _, section := range []string{"source", "schema"} {
		rows, ok := document[section].([]any)
		if !ok {
			continue
		}
		for index, raw := range rows {
			row, ok := raw.(map[string]any)
			if !ok || row["content"] != nil {
				continue
			}
			relative, ok := row["path"].(string)
			if !ok || relative == "" || filepath.IsAbs(relative) || strings.HasPrefix(filepath.Clean(relative), "..") {
				return oracleTransportFixture{}, fmt.Errorf("fixture %q has unsafe external source", fixture.ID)
			}
			sourcePath := filepath.Join(filepath.Dir(fixture.Path), relative)
			source, err := os.ReadFile(sourcePath)
			if err != nil {
				return oracleTransportFixture{}, err
			}
			stagedRelative := filepath.ToSlash(filepath.Join("fixtures", "sources", fixture.SHA256[:16], fmt.Sprintf("%s-%d-%s", section, index, filepath.Base(relative))))
			stagedPath := filepath.Join(bundleRoot, filepath.FromSlash(stagedRelative))
			if err := os.MkdirAll(filepath.Dir(stagedPath), 0o700); err != nil {
				return oracleTransportFixture{}, err
			}
			if err := os.WriteFile(stagedPath, source, 0o600); err != nil {
				return oracleTransportFixture{}, err
			}
			row["path"] = stagedRelative
			rows[index] = row
			sources = append(sources, oracleSourceFile{Path: stagedRelative, SHA256: replayBytesSHA256(source)})
		}
		document[section] = rows
	}
	stagedName := "fixture-" + fixture.SHA256[:16] + ".json"
	stagedRelative := filepath.ToSlash(filepath.Join("fixtures", stagedName))
	stagedData, err := json.Marshal(document)
	if err != nil {
		return oracleTransportFixture{}, err
	}
	stagedPath := filepath.Join(bundleRoot, filepath.FromSlash(stagedRelative))
	if err := os.WriteFile(stagedPath, stagedData, 0o600); err != nil {
		return oracleTransportFixture{}, err
	}
	return oracleTransportFixture{ID: fixture.ID, Fixture: stagedName, Path: stagedRelative, SHA256: replayBytesSHA256(stagedData), SourceFiles: sources, SurfaceIDs: fixture.SurfaceIDs, SalesforceEligible: true}, nil
}

func oracleLocalProofSummaryForTransport(proof LocalProof, transport oracleTransportManifest, manifestSHA string) (oracleLocalProofSummary, error) {
	byID := make(map[string]oracleTransportFixture, len(transport.Fixtures))
	for _, fixture := range transport.Fixtures {
		if fixture.ID == "" || byID[fixture.ID].ID != "" {
			return oracleLocalProofSummary{}, fmt.Errorf("invalid transport fixture %q", fixture.ID)
		}
		byID[fixture.ID] = fixture
	}
	raw := make(map[string]LocalProofFixtureResult, len(proof.RawFixtureResults))
	for _, result := range proof.RawFixtureResults {
		if result.FixtureID == "" || raw[result.FixtureID].FixtureID != "" {
			return oracleLocalProofSummary{}, fmt.Errorf("invalid local proof fixture receipt %q", result.FixtureID)
		}
		raw[result.FixtureID] = result
	}
	summary := oracleLocalProofSummary{Sealed: true, ManifestSHA256: manifestSHA, Results: make([]oracleLocalProofSummaryResult, 0, len(transport.Fixtures))}
	for _, fixture := range transport.Fixtures {
		result, exists := raw[fixture.ID]
		if !exists || !result.Receipt.Passed || result.Receipt.ExitCode != 0 || !json.Valid([]byte(result.Stdout)) {
			return oracleLocalProofSummary{}, fmt.Errorf("transport fixture %q lacks a passing local proof result", fixture.ID)
		}
		summary.Results = append(summary.Results, oracleLocalProofSummaryResult{Fixture: fixture.Fixture, Path: fixture.Path, Status: "exit-0", Kind: result.Operation, Result: json.RawMessage(result.Stdout)})
	}
	return summary, nil
}
