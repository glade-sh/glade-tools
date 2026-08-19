package corpusassurance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type CampaignSpec struct {
	SchemaVersion      int               `json:"schemaVersion"`
	CampaignID         string            `json:"campaignId"`
	SelectedSurfaceIDs []string          `json:"selectedSurfaceIds"`
	Bindings           []CampaignBinding `json:"bindings"`
	Phases             []CampaignPhase   `json:"phases"`
}

type CampaignBinding struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type CampaignPhase struct {
	ID         string   `json:"id"`
	Kind       string   `json:"kind,omitempty"`
	Family     string   `json:"family"`
	ProofClass string   `json:"proofClass"`
	DependsOn  []string `json:"dependsOn,omitempty"`
	CWD        string   `json:"cwd"`
	Argv       []string `json:"argv"`
	Env        []string `json:"env,omitempty"`
	Log        string   `json:"log"`
	Outputs    []string `json:"outputs,omitempty"`
}

type CampaignState struct {
	SchemaVersion int                  `json:"schemaVersion"`
	CampaignID    string               `json:"campaignId"`
	SpecSHA256    string               `json:"specSha256"`
	Phases        []CampaignPhaseState `json:"phases"`
}

type CampaignPhaseState struct {
	ID       string            `json:"id"`
	Status   string            `json:"status"`
	Attempts []CampaignAttempt `json:"attempts"`
}

type CampaignAttempt struct {
	Number           int                `json:"number"`
	Status           string             `json:"status"`
	ExitCode         int                `json:"exitCode,omitempty"`
	ExecutableSHA256 string             `json:"executableSha256"`
	Log              CampaignArtifact   `json:"log,omitempty"`
	Outputs          []CampaignArtifact `json:"outputs,omitempty"`
	StartedAt        string             `json:"startedAt"`
	FinishedAt       string             `json:"finishedAt,omitempty"`
}

type CampaignArtifact struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}

type CampaignResult struct {
	CampaignID string `json:"campaignId"`
	Total      int    `json:"total"`
	Passed     int    `json:"passed"`
	Skipped    int    `json:"skipped"`
	StatePath  string `json:"statePath"`
}

type CampaignPromotion struct {
	SchemaVersion      int                `json:"schemaVersion"`
	Status             string             `json:"status"`
	CampaignID         string             `json:"campaignId"`
	SpecSHA256         string             `json:"specSha256"`
	StateSHA256        string             `json:"stateSha256"`
	SelectedSurfaceIDs []string           `json:"selectedSurfaceIds"`
	Artifacts          []CampaignArtifact `json:"artifacts"`
}

func RunCampaign(ctx context.Context, specPath, statePath string) (CampaignResult, error) {
	var result CampaignResult
	if !filepath.IsAbs(specPath) || !filepath.IsAbs(statePath) {
		return result, errors.New("campaign spec and state paths must be absolute")
	}
	var err error
	specPath, err = canonicalCampaignPath(specPath)
	if err != nil {
		return result, err
	}
	statePath, err = canonicalCampaignPath(statePath)
	if err != nil {
		return result, err
	}
	releaseLock, err := acquireCampaignLock(statePath)
	if err != nil {
		return result, err
	}
	defer releaseLock()
	spec, specBytes, err := readExactJSONBytes[CampaignSpec](specPath)
	if err != nil {
		return result, fmt.Errorf("read campaign spec: %w", err)
	}
	if err := validateCampaignSpec(spec); err != nil {
		return result, err
	}
	if err := canonicalizeCampaignSpec(&spec); err != nil {
		return result, err
	}
	if err := validateCampaignSpec(spec); err != nil {
		return result, err
	}
	if err := validateCampaignReservedPaths(spec, specPath, statePath); err != nil {
		return result, err
	}
	if err := verifyCampaignBindings(spec.Bindings); err != nil {
		return result, err
	}
	specHash := replayBytesSHA256(specBytes)
	state, exists, err := loadCampaignState(statePath, spec, specHash)
	if err != nil {
		return result, err
	}
	if recovered, err := recoverInterruptedCampaign(&state, spec); err != nil {
		return result, err
	} else if recovered {
		if err := rewriteCampaignState(statePath, state); err != nil {
			return result, err
		}
	}
	if err := preflightCampaign(spec, state); err != nil {
		return result, err
	}
	if !exists {
		if err := WriteNewJSON(statePath, state); err != nil {
			return result, fmt.Errorf("create campaign state: %w", err)
		}
	}

	result = CampaignResult{CampaignID: spec.CampaignID, Total: len(spec.Phases), StatePath: statePath}
	for index, phase := range spec.Phases {
		phaseState := &state.Phases[index]
		if phaseState.Status == "passed" {
			result.Passed++
			result.Skipped++
			continue
		}
		if err := ctx.Err(); err != nil {
			return result, err
		}
		executableSHA256, err := sha256File(phase.Argv[0])
		if err != nil {
			return result, err
		}
		attemptNumber := len(phaseState.Attempts) + 1
		logPath := campaignAttemptLog(phase.Log, attemptNumber)
		phaseState.Status = "running"
		phaseState.Attempts = append(phaseState.Attempts, CampaignAttempt{Number: attemptNumber, Status: "running", ExecutableSHA256: executableSHA256, StartedAt: time.Now().UTC().Format(time.RFC3339Nano)})
		if err := rewriteCampaignState(statePath, state); err != nil {
			return result, err
		}

		attempt := &phaseState.Attempts[len(phaseState.Attempts)-1]
		exitCode, runErr := runCampaignPhase(ctx, phase, logPath)
		attempt.ExitCode = exitCode
		if integrityErr := verifyCampaignBindings(spec.Bindings); integrityErr != nil {
			runErr = errors.Join(runErr, integrityErr)
		}
		if afterSHA256, integrityErr := sha256File(phase.Argv[0]); integrityErr != nil || afterSHA256 != attempt.ExecutableSHA256 {
			runErr = errors.Join(runErr, errors.New("campaign phase executable changed during execution"), integrityErr)
		}
		attempt.Log, err = campaignArtifact(logPath)
		if err != nil {
			return result, fmt.Errorf("hash campaign phase %q log: %w", phase.ID, err)
		}
		attempt.Outputs, err = campaignArtifacts(phase.Outputs)
		if err != nil {
			phaseState.Status = "failed"
			attempt.Status = "failed"
			attempt.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
			if stateErr := rewriteCampaignState(statePath, state); stateErr != nil {
				return result, errors.Join(err, stateErr)
			}
			return result, fmt.Errorf("campaign phase %q outputs: %w", phase.ID, err)
		}
		if runErr != nil {
			phaseState.Status = "failed"
			attempt.Status = "failed"
			attempt.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
			if err := rewriteCampaignState(statePath, state); err != nil {
				return result, errors.Join(runErr, err)
			}
			return result, fmt.Errorf("campaign phase %q exit %d: %w", phase.ID, exitCode, runErr)
		}
		phaseState.Status = "passed"
		attempt.Status = "passed"
		attempt.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
		if err := rewriteCampaignState(statePath, state); err != nil {
			return result, err
		}
		result.Passed++
	}
	return result, nil
}

func PromoteCampaign(specPath, statePath, outputDir string) (CampaignPromotion, error) {
	var receipt CampaignPromotion
	if !filepath.IsAbs(specPath) || !filepath.IsAbs(statePath) || !filepath.IsAbs(outputDir) {
		return receipt, errors.New("campaign promotion paths must be absolute")
	}
	var err error
	specPath, err = canonicalCampaignPath(specPath)
	if err != nil {
		return receipt, err
	}
	statePath, err = canonicalCampaignPath(statePath)
	if err != nil {
		return receipt, err
	}
	outputDir, err = canonicalCampaignPath(outputDir)
	if err != nil {
		return receipt, err
	}
	releaseLock, err := acquireCampaignLock(statePath)
	if err != nil {
		return receipt, err
	}
	defer releaseLock()
	spec, specBytes, err := readExactJSONBytes[CampaignSpec](specPath)
	if err != nil {
		return receipt, err
	}
	if err := validateCampaignSpec(spec); err != nil {
		return receipt, err
	}
	if err := canonicalizeCampaignSpec(&spec); err != nil {
		return receipt, err
	}
	if err := validateCampaignSpec(spec); err != nil {
		return receipt, err
	}
	if err := validateCampaignReservedPaths(spec, specPath, statePath); err != nil {
		return receipt, err
	}
	if err := verifyCampaignBindings(spec.Bindings); err != nil {
		return receipt, err
	}
	state, stateBytes, err := readExactJSONBytes[CampaignState](statePath)
	if err != nil {
		return receipt, err
	}
	if state.SpecSHA256 != replayBytesSHA256(specBytes) || state.CampaignID != spec.CampaignID {
		return receipt, errors.New("campaign state does not bind the exact spec")
	}
	if err := validateCampaignState(spec, state); err != nil {
		return receipt, err
	}
	if err := preflightCampaign(spec, state); err != nil {
		return receipt, err
	}
	for _, phase := range state.Phases {
		if phase.Status != "passed" {
			return receipt, fmt.Errorf("campaign phase %q is not passed", phase.ID)
		}
	}
	artifacts := campaignStateArtifacts(state)
	receipt = CampaignPromotion{
		SchemaVersion:      1,
		Status:             "ready-for-promotion",
		CampaignID:         spec.CampaignID,
		SpecSHA256:         replayBytesSHA256(specBytes),
		StateSHA256:        replayBytesSHA256(stateBytes),
		SelectedSurfaceIDs: append([]string(nil), spec.SelectedSurfaceIDs...),
		Artifacts:          artifacts,
	}
	if err := os.Mkdir(outputDir, 0o700); err != nil {
		return CampaignPromotion{}, fmt.Errorf("create campaign promotion root: %w", err)
	}
	if err := writeNewBytes(filepath.Join(outputDir, "CAMPAIGN.json"), specBytes); err != nil {
		return CampaignPromotion{}, err
	}
	if err := writeNewBytes(filepath.Join(outputDir, "CAMPAIGN_STATE.json"), stateBytes); err != nil {
		return CampaignPromotion{}, err
	}
	if err := WriteNewJSON(filepath.Join(outputDir, "CAMPAIGN_PROMOTION.json"), receipt); err != nil {
		return CampaignPromotion{}, err
	}
	return receipt, nil
}

func validateCampaignSpec(spec CampaignSpec) error {
	if spec.SchemaVersion != 1 || strings.TrimSpace(spec.CampaignID) == "" {
		return errors.New("campaign requires schemaVersion 1 and campaignId")
	}
	if len(spec.SelectedSurfaceIDs) == 0 || len(spec.Bindings) < 2 || len(spec.Phases) == 0 {
		return errors.New("campaign requires selected surfaces, candidate/tools bindings, and phases")
	}
	seenSurface := map[string]bool{}
	for _, id := range spec.SelectedSurfaceIDs {
		if strings.TrimSpace(id) == "" || seenSurface[id] {
			return errors.New("campaign selected surface IDs must be nonempty and unique")
		}
		seenSurface[id] = true
	}
	seenBinding := map[string]bool{}
	for _, binding := range spec.Bindings {
		if strings.TrimSpace(binding.Name) == "" || seenBinding[binding.Name] || !filepath.IsAbs(binding.Path) || !sha256Pattern.MatchString(binding.SHA256) {
			return errors.New("campaign bindings require unique names, absolute paths, and SHA-256")
		}
		seenBinding[binding.Name] = true
	}
	if !seenBinding["candidate"] || !seenBinding["tools"] {
		return errors.New("campaign bindings require candidate and tools")
	}
	phaseIndex := map[string]int{}
	ownedPath := map[string]string{}
	for index, phase := range spec.Phases {
		if strings.TrimSpace(phase.ID) == "" || strings.TrimSpace(phase.Family) == "" || strings.TrimSpace(phase.ProofClass) == "" {
			return fmt.Errorf("campaign phase %d requires id, family, and proofClass", index)
		}
		if _, ok := phaseIndex[phase.ID]; ok {
			return fmt.Errorf("duplicate campaign phase %q", phase.ID)
		}
		phaseIndex[phase.ID] = index
		if !filepath.IsAbs(phase.CWD) || len(phase.Argv) == 0 || !filepath.IsAbs(phase.Argv[0]) || !filepath.IsAbs(phase.Log) {
			return fmt.Errorf("campaign phase %q requires absolute cwd, executable, and log", phase.ID)
		}
		info, err := os.Stat(phase.CWD)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("campaign phase %q cwd is not a directory", phase.ID)
		}
		info, err = os.Stat(phase.Argv[0])
		if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
			return fmt.Errorf("campaign phase %q executable is invalid", phase.ID)
		}
		paths := append([]string{phase.Log}, phase.Outputs...)
		for _, path := range paths {
			if !filepath.IsAbs(path) {
				return fmt.Errorf("campaign phase %q output paths must be absolute", phase.ID)
			}
			if owner, ok := ownedPath[path]; ok {
				return fmt.Errorf("campaign path %q is owned by both %q and %q", path, owner, phase.ID)
			}
			ownedPath[path] = phase.ID
		}
	}
	for index, phase := range spec.Phases {
		for _, dependency := range phase.DependsOn {
			dependencyIndex, ok := phaseIndex[dependency]
			if !ok {
				return fmt.Errorf("campaign phase %q has unknown dependency %q", phase.ID, dependency)
			}
			if dependencyIndex >= index {
				return fmt.Errorf("campaign phase %q dependency %q must precede it", phase.ID, dependency)
			}
		}
	}
	return validateCampaignUsageOrder(spec, phaseIndex)
}

func validateCampaignReservedPaths(spec CampaignSpec, specPath, statePath string) error {
	reserved := map[string]bool{specPath: true, statePath: true, statePath + ".lock": true}
	for _, phase := range spec.Phases {
		for _, path := range append([]string{phase.Log}, phase.Outputs...) {
			if reserved[path] {
				return fmt.Errorf("campaign phase %q collides with campaign control path %s", phase.ID, path)
			}
		}
	}
	return nil
}

func canonicalizeCampaignSpec(spec *CampaignSpec) error {
	for index := range spec.Bindings {
		path, err := canonicalCampaignPath(spec.Bindings[index].Path)
		if err != nil {
			return err
		}
		spec.Bindings[index].Path = path
	}
	for index := range spec.Phases {
		phase := &spec.Phases[index]
		var err error
		phase.CWD, err = canonicalCampaignPath(phase.CWD)
		if err != nil {
			return err
		}
		phase.Argv[0], err = canonicalCampaignPath(phase.Argv[0])
		if err != nil {
			return err
		}
		phase.Log, err = canonicalCampaignPath(phase.Log)
		if err != nil {
			return err
		}
		for outputIndex := range phase.Outputs {
			phase.Outputs[outputIndex], err = canonicalCampaignPath(phase.Outputs[outputIndex])
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func canonicalCampaignPath(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("campaign path must be absolute: %s", path)
	}
	path = filepath.Clean(path)
	existing := path
	var suffix []string
	for {
		if _, err := os.Lstat(existing); err == nil {
			resolved, err := filepath.EvalSymlinks(existing)
			if err != nil {
				return "", err
			}
			for index := len(suffix) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, suffix[index])
			}
			return filepath.Clean(resolved), nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return "", fmt.Errorf("campaign path has no existing ancestor: %s", path)
		}
		suffix = append(suffix, filepath.Base(existing))
		existing = parent
	}
}

func verifyCampaignBindings(bindings []CampaignBinding) error {
	for _, binding := range bindings {
		actual, err := sha256File(binding.Path)
		if err != nil {
			return fmt.Errorf("campaign binding %q: %w", binding.Name, err)
		}
		if actual != binding.SHA256 {
			return fmt.Errorf("campaign binding %q changed", binding.Name)
		}
	}
	return nil
}

func validateCampaignUsageOrder(spec CampaignSpec, phaseIndex map[string]int) error {
	kinds := map[string][]string{}
	for _, phase := range spec.Phases {
		if phase.Kind != "" {
			kinds[phase.Kind] = append(kinds[phase.Kind], phase.ID)
		}
	}
	_, raw := kinds["raw-corpus-usage"]
	_, profile := kinds["sealed-profile"]
	_, draft := kinds["usage-draft"]
	if !raw && !profile && !draft {
		return nil
	}
	if len(kinds["raw-corpus-usage"]) != 1 || len(kinds["sealed-profile"]) != 1 || len(kinds["usage-draft"]) != 1 {
		return errors.New("campaign assurance ordering requires exactly one raw-corpus-usage, sealed-profile, and usage-draft phase")
	}
	rawID, profileID, draftID := kinds["raw-corpus-usage"][0], kinds["sealed-profile"][0], kinds["usage-draft"][0]
	if phaseIndex[rawID] >= phaseIndex[profileID] || phaseIndex[profileID] >= phaseIndex[draftID] || !campaignDependsOn(spec, phaseIndex, profileID, rawID) || !campaignDependsOn(spec, phaseIndex, draftID, profileID) {
		return errors.New("campaign assurance order must be raw-corpus-usage -> sealed-profile -> usage-draft")
	}
	return nil
}

func campaignDependsOn(spec CampaignSpec, phaseIndex map[string]int, phaseID, dependencyID string) bool {
	seen := map[string]bool{}
	var walk func(string) bool
	walk = func(id string) bool {
		if seen[id] {
			return false
		}
		seen[id] = true
		for _, dependency := range spec.Phases[phaseIndex[id]].DependsOn {
			if dependency == dependencyID || walk(dependency) {
				return true
			}
		}
		return false
	}
	return walk(phaseID)
}

func loadCampaignState(path string, spec CampaignSpec, specHash string) (CampaignState, bool, error) {
	state := CampaignState{SchemaVersion: 1, CampaignID: spec.CampaignID, SpecSHA256: specHash}
	for _, phase := range spec.Phases {
		state.Phases = append(state.Phases, CampaignPhaseState{ID: phase.ID, Status: "pending"})
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return state, false, nil
	} else if err != nil {
		return CampaignState{}, false, err
	}
	loaded, _, err := readExactJSONBytes[CampaignState](path)
	if err != nil {
		return CampaignState{}, false, fmt.Errorf("read campaign state: %w", err)
	}
	if loaded.CampaignID != spec.CampaignID || loaded.SpecSHA256 != specHash {
		return CampaignState{}, false, errors.New("campaign state does not bind the exact spec")
	}
	if err := validateCampaignState(spec, loaded); err != nil {
		return CampaignState{}, false, err
	}
	return loaded, true, nil
}

func validateCampaignState(spec CampaignSpec, state CampaignState) error {
	if state.SchemaVersion != 1 || len(state.Phases) != len(spec.Phases) {
		return errors.New("campaign state schema or phase count is invalid")
	}
	for index, phase := range state.Phases {
		if phase.ID != spec.Phases[index].ID {
			return errors.New("campaign state phase order does not match spec")
		}
		if phase.Status != "pending" && phase.Status != "running" && phase.Status != "passed" && phase.Status != "failed" {
			return fmt.Errorf("campaign phase %q has invalid state", phase.ID)
		}
		for attemptIndex, attempt := range phase.Attempts {
			finalAttempt := attemptIndex == len(phase.Attempts)-1
			if attempt.Number != attemptIndex+1 || attempt.StartedAt == "" || !sha256Pattern.MatchString(attempt.ExecutableSHA256) {
				return fmt.Errorf("campaign phase %q attempt history is invalid", phase.ID)
			}
			if attempt.Status != "running" && attempt.Status != "passed" && attempt.Status != "failed" && attempt.Status != "interrupted" {
				return fmt.Errorf("campaign phase %q attempt status is invalid", phase.ID)
			}
			if attempt.Status != "running" {
				if attempt.FinishedAt == "" {
					return fmt.Errorf("campaign phase %q finished attempt lacks time", phase.ID)
				}
			} else if attempt.FinishedAt != "" {
				return fmt.Errorf("campaign phase %q running attempt has finish time", phase.ID)
			}
			if !finalAttempt && attempt.Status != "failed" && attempt.Status != "interrupted" {
				return fmt.Errorf("campaign phase %q has impossible non-final attempt", phase.ID)
			}
			if attempt.Status == "running" && (!finalAttempt || phase.Status != "running") {
				return fmt.Errorf("campaign phase %q has misplaced running attempt", phase.ID)
			}
			if attempt.Status == "passed" && (!finalAttempt || phase.Status != "passed") {
				return fmt.Errorf("campaign phase %q has misplaced passed attempt", phase.ID)
			}
		}
		switch phase.Status {
		case "pending":
			if len(phase.Attempts) != 0 {
				return fmt.Errorf("pending campaign phase %q has attempts", phase.ID)
			}
		case "running":
			if len(phase.Attempts) == 0 || phase.Attempts[len(phase.Attempts)-1].Status != "running" {
				return fmt.Errorf("running campaign phase %q lacks running attempt", phase.ID)
			}
		case "passed":
			if len(phase.Attempts) == 0 || phase.Attempts[len(phase.Attempts)-1].Status != "passed" {
				return fmt.Errorf("passed campaign phase %q lacks passed attempt", phase.ID)
			}
		case "failed":
			if len(phase.Attempts) == 0 || (phase.Attempts[len(phase.Attempts)-1].Status != "failed" && phase.Attempts[len(phase.Attempts)-1].Status != "interrupted") {
				return fmt.Errorf("failed campaign phase %q lacks failed attempt", phase.ID)
			}
		}
	}
	return nil
}

func recoverInterruptedCampaign(state *CampaignState, spec CampaignSpec) (bool, error) {
	recovered := false
	for index := range state.Phases {
		phaseState := &state.Phases[index]
		if phaseState.Status != "running" {
			continue
		}
		attempt := &phaseState.Attempts[len(phaseState.Attempts)-1]
		attempt.Status = "interrupted"
		attempt.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
		logPath := campaignAttemptLog(spec.Phases[index].Log, attempt.Number)
		if _, err := os.Lstat(logPath); err == nil {
			artifact, err := campaignArtifact(logPath)
			if err != nil {
				return false, err
			}
			attempt.Log = artifact
		} else if !os.IsNotExist(err) {
			return false, err
		}
		outputs, err := campaignExistingArtifacts(spec.Phases[index].Outputs)
		if err != nil {
			return false, err
		}
		attempt.Outputs = outputs
		phaseState.Status = "failed"
		recovered = true
	}
	if recovered {
		if err := validateCampaignState(spec, *state); err != nil {
			return false, err
		}
	}
	return recovered, nil
}

func campaignExistingArtifacts(paths []string) ([]CampaignArtifact, error) {
	var artifacts []CampaignArtifact
	for _, path := range paths {
		if _, err := os.Lstat(path); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return nil, err
		}
		artifact, err := campaignArtifact(path)
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, artifact)
	}
	return artifacts, nil
}

func preflightCampaign(spec CampaignSpec, state CampaignState) error {
	passed := map[string]bool{}
	for index, phase := range spec.Phases {
		phaseState := state.Phases[index]
		if err := verifyCampaignAttemptHistory(phase, phaseState); err != nil {
			return err
		}
		if phaseState.Status == "running" {
			return fmt.Errorf("campaign phase %q was interrupted while running", phase.ID)
		}
		for _, dependency := range phase.DependsOn {
			if phaseState.Status == "passed" && !passed[dependency] {
				return fmt.Errorf("campaign phase %q passed before dependency %q", phase.ID, dependency)
			}
		}
		switch phaseState.Status {
		case "passed":
			attempt := phaseState.Attempts[len(phaseState.Attempts)-1]
			executableSHA256, err := sha256File(phase.Argv[0])
			if err != nil || executableSHA256 != attempt.ExecutableSHA256 {
				return fmt.Errorf("campaign phase %q executable changed", phase.ID)
			}
			if err := verifyCampaignArtifact(attempt.Log); err != nil {
				return fmt.Errorf("campaign phase %q log changed: %w", phase.ID, err)
			}
			if len(attempt.Outputs) != len(phase.Outputs) {
				return fmt.Errorf("campaign phase %q output receipt count changed", phase.ID)
			}
			for outputIndex, artifact := range attempt.Outputs {
				if artifact.Path != phase.Outputs[outputIndex] {
					return fmt.Errorf("campaign phase %q output path changed", phase.ID)
				}
				if err := verifyCampaignArtifact(artifact); err != nil {
					return fmt.Errorf("campaign phase %q output changed: %w", phase.ID, err)
				}
			}
			passed[phase.ID] = true
		case "pending", "failed":
			for _, output := range phase.Outputs {
				if _, err := os.Lstat(output); err == nil {
					return fmt.Errorf("campaign pending output already exists: %s", output)
				} else if !os.IsNotExist(err) {
					return err
				}
			}
			logPath := campaignAttemptLog(phase.Log, len(phaseState.Attempts)+1)
			if _, err := os.Lstat(logPath); err == nil {
				return fmt.Errorf("campaign pending log already exists: %s", logPath)
			} else if !os.IsNotExist(err) {
				return err
			}
		}
	}
	return nil
}

func verifyCampaignAttemptHistory(phase CampaignPhase, state CampaignPhaseState) error {
	declaredOutputs := map[string]bool{}
	for _, path := range phase.Outputs {
		declaredOutputs[path] = true
	}
	for _, attempt := range state.Attempts {
		expectedLog := campaignAttemptLog(phase.Log, attempt.Number)
		if attempt.Log.Path != "" {
			if attempt.Log.Path != expectedLog {
				return fmt.Errorf("campaign phase %q attempt %d log path changed", phase.ID, attempt.Number)
			}
			if err := verifyCampaignArtifact(attempt.Log); err != nil {
				return fmt.Errorf("campaign phase %q attempt %d log changed: %w", phase.ID, attempt.Number, err)
			}
		} else if attempt.Status == "passed" || attempt.Status == "failed" {
			return fmt.Errorf("campaign phase %q attempt %d lacks retained log", phase.ID, attempt.Number)
		}
		seen := map[string]bool{}
		for _, artifact := range attempt.Outputs {
			if !declaredOutputs[artifact.Path] || seen[artifact.Path] {
				return fmt.Errorf("campaign phase %q attempt %d has invalid output receipt", phase.ID, attempt.Number)
			}
			seen[artifact.Path] = true
			if err := verifyCampaignArtifact(artifact); err != nil {
				return fmt.Errorf("campaign phase %q attempt %d output changed: %w", phase.ID, attempt.Number, err)
			}
		}
	}
	return nil
}

func runCampaignPhase(ctx context.Context, phase CampaignPhase, logPath string) (int, error) {
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		return -1, err
	}
	logFile, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return -1, err
	}
	command := exec.CommandContext(ctx, phase.Argv[0], phase.Argv[1:]...)
	command.Dir = phase.CWD
	command.Stdout = logFile
	command.Stderr = logFile
	command.Env = make([]string, len(phase.Env))
	copy(command.Env, phase.Env)
	runErr := command.Run()
	closeErr := logFile.Close()
	if runErr == nil && closeErr != nil {
		runErr = closeErr
	}
	exitCode := 0
	if runErr != nil {
		exitCode = -1
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
	}
	return exitCode, runErr
}

func campaignAttemptLog(base string, attempt int) string {
	if attempt <= 1 {
		return base
	}
	extension := filepath.Ext(base)
	return strings.TrimSuffix(base, extension) + fmt.Sprintf(".attempt-%d", attempt) + extension
}

func rewriteCampaignState(path string, state CampaignState) error {
	temp, err := os.CreateTemp(filepath.Dir(path), ".campaign-state-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return err
	}
	encoder := json.NewEncoder(temp)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(state); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}

func campaignArtifacts(paths []string) ([]CampaignArtifact, error) {
	artifacts := make([]CampaignArtifact, 0, len(paths))
	for _, path := range paths {
		artifact, err := campaignArtifact(path)
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, artifact)
	}
	return artifacts, nil
}

func campaignArtifact(path string) (CampaignArtifact, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return CampaignArtifact{}, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return CampaignArtifact{}, err
	}
	if !info.Mode().IsRegular() {
		return CampaignArtifact{}, fmt.Errorf("campaign artifact is not a regular file: %s", path)
	}
	return CampaignArtifact{Path: path, SHA256: replayBytesSHA256(data), Bytes: int64(len(data))}, nil
}

func verifyCampaignArtifact(expected CampaignArtifact) error {
	actual, err := campaignArtifact(expected.Path)
	if err != nil {
		return err
	}
	if actual.SHA256 != expected.SHA256 || actual.Bytes != expected.Bytes {
		return errors.New("artifact hash or size mismatch")
	}
	return nil
}

func campaignStateArtifacts(state CampaignState) []CampaignArtifact {
	var artifacts []CampaignArtifact
	for _, phase := range state.Phases {
		for _, attempt := range phase.Attempts {
			if attempt.Log.Path != "" {
				artifacts = append(artifacts, attempt.Log)
			}
			artifacts = append(artifacts, attempt.Outputs...)
		}
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Path < artifacts[j].Path })
	return artifacts
}

func writeNewBytes(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

func acquireCampaignLock(statePath string) (func(), error) {
	lockPath := statePath + ".lock"
	file, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return nil, fmt.Errorf("campaign is active or requires stale-lock recovery: %s", lockPath)
		}
		return nil, err
	}
	if _, err := fmt.Fprintf(file, "%d\n", os.Getpid()); err != nil {
		file.Close()
		_ = os.Remove(lockPath)
		return nil, err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(lockPath)
		return nil, err
	}
	return func() { _ = os.Remove(lockPath) }, nil
}
