package corpusassurance

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// OracleBundleRequest names the sealed inputs staged for Razor. Every input is
// rehashed before publication; OutputPath must be a new private directory.
type OracleBundleRequest struct {
	ProfilePath           string
	PlanPath              string
	AuthorityPath         string
	ReleaseValidationPath string
	LocalProofPath        string
	FixtureManifestPath   string
	FilterScriptPath      string
	ScratchDefinitionPath string
	ToolsAMD64Path        string
	OutputPath            string
}

// OracleBundle is the acyclic receipt for the self-contained Razor staging
// tree. Its SHA-256 is the bundle identity used by every Salesforce shard.
type OracleBundle struct {
	SchemaVersion            int                   `json:"schemaVersion"`
	Candidate                RuntimeArtifact       `json:"candidate"`
	Tools                    RuntimeArtifact       `json:"tools"`
	ProfileSHA256            string                `json:"profileSha256"`
	OraclePlanSHA256         string                `json:"oraclePlanSha256"`
	ExclusionAuthoritySHA256 string                `json:"exclusionAuthoritySha256"`
	ReleaseValidationSHA256  string                `json:"releaseValidationSha256"`
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
	paths := []string{request.ProfilePath, request.PlanPath, request.AuthorityPath, request.ReleaseValidationPath, request.LocalProofPath, request.FixtureManifestPath, request.FilterScriptPath, request.ScratchDefinitionPath, request.ToolsAMD64Path, request.OutputPath}
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
	profileSHA, planSHA, authoritySHA, proofSHA, manifestSHA := replayBytesSHA256(profileBytes), replayBytesSHA256(planBytes), replayBytesSHA256(authorityBytes), replayBytesSHA256(proofBytes), replayBytesSHA256(manifestBytes)
	if profile.SchemaVersion != 1 || profile.FixtureManifestSHA256 != manifestSHA || profile.LocalProofSHA256 != proofSHA || plan.ProfileSHA256 != profileSHA || plan.Candidate != proof.Candidate || plan.Tools != proof.Tools || authority.Candidate != plan.Candidate || authority.Tools != plan.Tools || authority.PlanSHA256 != planSHA || authority.ProfileSHA256 != profileSHA || authority.LocalProofSHA256 != proofSHA {
		return OracleBundle{}, fmt.Errorf("oracle bundle inputs do not bind")
	}
	if err := ValidateLocalProof(proof, manifest); err != nil {
		return OracleBundle{}, fmt.Errorf("validate local proof: %w", err)
	}
	fixtures, err := oracleBundleFixtures(plan, manifest)
	if err != nil {
		return OracleBundle{}, err
	}
	inputs := map[string]string{request.ProfilePath: profileSHA, request.PlanPath: planSHA, request.AuthorityPath: authoritySHA, request.LocalProofPath: proofSHA, request.FixtureManifestPath: manifestSHA}
	for _, path := range []string{request.ReleaseValidationPath, request.FilterScriptPath, request.ScratchDefinitionPath, request.ToolsAMD64Path} {
		hash, err := sha256File(path)
		if err != nil {
			return OracleBundle{}, err
		}
		inputs[path] = hash
	}
	scratchDefinition, err := os.ReadFile(request.ScratchDefinitionPath)
	if err != nil {
		return OracleBundle{}, err
	}
	if !json.Valid(scratchDefinition) {
		return OracleBundle{}, fmt.Errorf("scratch definition is not valid JSON")
	}
	if inputs[request.ToolsAMD64Path] != plan.Tools.SHA256 {
		return OracleBundle{}, fmt.Errorf("amd64 tools binary does not match sealed tools artifact")
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
	for _, item := range []struct{ path, name string }{{request.ProfilePath, "profile.json"}, {request.PlanPath, "ORACLE_PLAN.json"}, {request.AuthorityPath, "EXCLUSION_AUTHORITY.json"}, {request.ReleaseValidationPath, "RELEASE_VALIDATION.json"}, {request.ScratchDefinitionPath, "corpus-assurance-scratch-def.json"}} {
		if err := copyOracleBundleFile(item.path, filepath.Join(bundleRoot, item.name), 0o600); err != nil {
			return OracleBundle{}, err
		}
	}
	if err := copyOracleBundleFile(request.FilterScriptPath, filepath.Join(transportRoot, "salesforce-first-filter.py"), 0o700); err != nil {
		return OracleBundle{}, err
	}
	if err := copyOracleBundleFile(request.ToolsAMD64Path, filepath.Join(binRoot, "glade-tools-darwin-amd64"), 0o700); err != nil {
		return OracleBundle{}, err
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
	bundle := OracleBundle{SchemaVersion: 1, Candidate: plan.Candidate, Tools: plan.Tools, ProfileSHA256: profileSHA, OraclePlanSHA256: planSHA, ExclusionAuthoritySHA256: authoritySHA, ReleaseValidationSHA256: inputs[request.ReleaseValidationPath], LocalProofSHA256: proofSHA, LocalProofSummarySHA256: summarySHA, FixtureManifestSHA256: manifestSHA, TransportManifestSHA256: transportSHA, FilterSHA256: inputs[request.FilterScriptPath], ScratchDefinitionSHA256: inputs[request.ScratchDefinitionPath], ToolsAMD64SHA256: inputs[request.ToolsAMD64Path], Fixtures: fixtures}
	if err := WriteNewJSON(filepath.Join(bundleRoot, "bundle.json"), bundle); err != nil {
		return OracleBundle{}, err
	}
	if err := os.Rename(temp, request.OutputPath); err != nil {
		return OracleBundle{}, err
	}
	return bundle, nil
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
