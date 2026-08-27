package corpusassurance

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

const orchestratorProductionBatchSchema = 3

type BuildOrchestratorProductionBatchRequest struct {
	PlanPath                     string `json:"planPath"`
	LeasePath                    string `json:"leasePath"`
	LocalProofPath               string `json:"localProofPath"`
	ReviewPath                   string `json:"reviewPath"`
	OracleBundleRoot             string `json:"oracleBundleRoot"`
	RawRoot                      string `json:"rawRoot"`
	SalesforceReconciliationPath string `json:"salesforceReconciliationPath"`
	SalesforcePacketPath         string `json:"salesforcePacketPath"`
	SSHDispatchPath              string `json:"sshDispatchPath,omitempty"`
	SSHFetchPath                 string `json:"sshFetchPath,omitempty"`
	SSHTreeManifestPath          string `json:"sshTreeManifestPath,omitempty"`
	OutputPath                   string `json:"outputPath"`
}

type ProductionRuntimeReview struct {
	SchemaVersion        int                          `json:"schemaVersion"`
	PlanSHA256           string                       `json:"planSha256"`
	LeaseSHA256          string                       `json:"leaseSha256"`
	LocalProofSHA256     string                       `json:"localProofSha256"`
	ReconciliationSHA256 string                       `json:"reconciliationSha256"`
	Rows                 []ProductionRuntimeReviewRow `json:"rows"`
}

type ProductionRuntimeReviewRow struct {
	SurfaceID         string `json:"surfaceId"`
	Action            string `json:"action"`
	Classification    string `json:"classification"`
	ReviewDisposition string `json:"reviewDisposition"`
}

type ProductionRuntimeBatchFile struct {
	Path   string `json:"path"`
	Mode   string `json:"mode"`
	SHA256 string `json:"sha256"`
}

type ProductionRuntimeBatch struct {
	SchemaVersion int                          `json:"schemaVersion"`
	Candidate     RuntimeArtifact              `json:"candidate"`
	NativeTools   RuntimeArtifact              `json:"nativeTools"`
	ExecutedTools RuntimeArtifact              `json:"executedTools"`
	Files         []ProductionRuntimeBatchFile `json:"files"`
}

func BuildOrchestratorProductionBatch(request BuildOrchestratorProductionBatchRequest) (ProductionRuntimeBatch, error) {
	required := []string{request.PlanPath, request.LeasePath, request.LocalProofPath, request.ReviewPath, request.OracleBundleRoot, request.RawRoot, request.SalesforceReconciliationPath, request.SalesforcePacketPath, request.OutputPath}
	for _, path := range required {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return ProductionRuntimeBatch{}, fmt.Errorf("absolute clean production batch paths are required")
		}
	}
	sshCount := 0
	for _, path := range []string{request.SSHDispatchPath, request.SSHFetchPath, request.SSHTreeManifestPath} {
		if path != "" {
			sshCount++
			if !filepath.IsAbs(path) || filepath.Clean(path) != path {
				return ProductionRuntimeBatch{}, fmt.Errorf("absolute clean production SSH paths are required")
			}
		}
	}
	if sshCount != 0 && sshCount != 3 {
		return ProductionRuntimeBatch{}, fmt.Errorf("production SSH receipts are all-or-none")
	}
	if overlap, err := productionPathsOverlap(request.OutputPath, request.SalesforcePacketPath); err != nil {
		return ProductionRuntimeBatch{}, err
	} else if overlap {
		return ProductionRuntimeBatch{}, fmt.Errorf("production batch output and recursively copied Salesforce packet overlap")
	}
	plan, _, err := readMode0600JSON[OrchestratorCampaignPlan](request.PlanPath)
	if err != nil {
		return ProductionRuntimeBatch{}, err
	}
	lease, _, err := readMode0600JSON[OrchestratorLease](request.LeasePath)
	if err != nil || validateOrchestratorWorkerPlanLease(plan, lease) != nil {
		return ProductionRuntimeBatch{}, fmt.Errorf("production plan and lease binding is invalid")
	}
	if info, err := os.Lstat(request.OutputPath); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return ProductionRuntimeBatch{}, fmt.Errorf("production batch output is not a directory")
		}
		validated, err := validateOrchestratorProductionBatch(request.OutputPath, plan, lease)
		if err != nil {
			return ProductionRuntimeBatch{}, fmt.Errorf("validate published production batch: %w", err)
		}
		return validated.manifest, nil
	} else if !os.IsNotExist(err) {
		return ProductionRuntimeBatch{}, err
	}
	executedTools, err := validateProductionSources(request, plan, lease, sshCount == 3)
	if err != nil {
		return ProductionRuntimeBatch{}, err
	}
	proof, _, err := readMode0600JSON[LocalProof](request.LocalProofPath)
	if err != nil {
		return ProductionRuntimeBatch{}, err
	}
	parent := filepath.Dir(request.OutputPath)
	temporary, err := os.MkdirTemp(parent, ".production-batch-")
	if err != nil {
		return ProductionRuntimeBatch{}, err
	}
	defer os.RemoveAll(temporary)
	if err := os.Chmod(temporary, 0o700); err != nil {
		return ProductionRuntimeBatch{}, err
	}
	production := filepath.Join(temporary, "production")
	if err := os.Mkdir(production, 0o700); err != nil {
		return ProductionRuntimeBatch{}, err
	}
	files := map[string]string{
		"ORCHESTRATOR_PLAN.json":  request.PlanPath,
		"ORCHESTRATOR_LEASE.json": request.LeasePath,
		"LOCAL_PROOF.json":        request.LocalProofPath,
		"PRODUCTION_REVIEW.json":  request.ReviewPath,
	}
	for relative, source := range files {
		if err := copyProductionFile(source, filepath.Join(production, relative)); err != nil {
			return ProductionRuntimeBatch{}, err
		}
	}
	if err := copyProductionLocalProofInputs(proof, filepath.Join(production, "local-proof")); err != nil {
		return ProductionRuntimeBatch{}, err
	}
	oraclePaths, err := oracleBundleControlledPaths(request.OracleBundleRoot)
	if err != nil {
		return ProductionRuntimeBatch{}, err
	}
	if err := copyProductionNamedFiles(request.OracleBundleRoot, filepath.Join(production, "oracle-bundle"), oraclePaths); err != nil {
		return ProductionRuntimeBatch{}, err
	}
	if err := copyProductionNamedFiles(request.RawRoot, filepath.Join(production, "raw"), orchestratorSSHRawFileNames()); err != nil {
		return ProductionRuntimeBatch{}, err
	}
	if err := os.Mkdir(filepath.Join(production, "salesforce"), 0o700); err != nil {
		return ProductionRuntimeBatch{}, err
	}
	if err := copyProductionFile(request.SalesforceReconciliationPath, filepath.Join(production, "salesforce", "SALESFORCE_RECONCILIATION.json")); err != nil {
		return ProductionRuntimeBatch{}, err
	}
	if err := copyProductionTree(request.SalesforcePacketPath, filepath.Join(production, "salesforce", "packet")); err != nil {
		return ProductionRuntimeBatch{}, err
	}
	if sshCount == 3 {
		if err := os.Mkdir(filepath.Join(production, "ssh"), 0o700); err != nil {
			return ProductionRuntimeBatch{}, err
		}
		for name, source := range map[string]string{"SSH_DISPATCH.json": request.SSHDispatchPath, "SSH_FETCH.json": request.SSHFetchPath, "TREE_MANIFEST.json": request.SSHTreeManifestPath} {
			if err := copyProductionFile(source, filepath.Join(production, "ssh", name)); err != nil {
				return ProductionRuntimeBatch{}, err
			}
		}
	}
	manifest := ProductionRuntimeBatch{SchemaVersion: orchestratorProductionBatchSchema, Candidate: proof.Candidate, NativeTools: proof.Tools, ExecutedTools: executedTools}
	manifest.Files, err = productionManifestFiles(production)
	if err != nil {
		return ProductionRuntimeBatch{}, err
	}
	if err := WriteNewJSON(filepath.Join(production, "PRODUCTION_RUNTIME_BATCH.json"), manifest); err != nil {
		return ProductionRuntimeBatch{}, err
	}
	validated, err := validateOrchestratorProductionBatch(temporary, plan, lease)
	if err != nil || !reflect.DeepEqual(validated.manifest, manifest) {
		return ProductionRuntimeBatch{}, fmt.Errorf("validate staged production batch: %w", err)
	}
	if err := syncOrchestratorWorkerTree(temporary); err != nil {
		return ProductionRuntimeBatch{}, err
	}
	if err := os.Rename(temporary, request.OutputPath); err != nil {
		return ProductionRuntimeBatch{}, err
	}
	// Rename is the publication point. A parent-directory sync failure cannot
	// safely turn a fully published batch into an error result.
	_ = syncOrchestratorWorkerDirectory(parent)
	return manifest, nil
}

func productionPathsOverlap(left, right string) (bool, error) {
	left, err := canonicalProductionPath(left)
	if err != nil {
		return false, err
	}
	right, err = canonicalProductionPath(right)
	if err != nil {
		return false, err
	}
	return productionPathContains(left, right) || productionPathContains(right, left), nil
}

func canonicalProductionPath(path string) (string, error) {
	current := path
	missing := []string{}
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			for index := len(missing) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, missing[index])
			}
			return filepath.Clean(resolved), nil
		}
		if !os.IsNotExist(err) || filepath.Dir(current) == current {
			return "", err
		}
		missing = append(missing, filepath.Base(current))
		current = filepath.Dir(current)
	}
}

func productionPathContains(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	return err == nil && !filepath.IsAbs(relative) && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

type validatedProductionBatch struct {
	manifest       ProductionRuntimeBatch
	manifestSHA256 string
	proofStates    map[string]string
	paths          []string
	OrgAlias       string
	DevHub         string
}

func validateOrchestratorSalesforceReconciliationSemantics(plan OrchestratorCampaignPlan, lease OrchestratorLease, receiptPath, packetPath, retainedBundlePath string) (SalesforceReconciliation, []salesforceShardEvidenceSnapshot, error) {
	retainedBundle, retainedBundleBytes, err := readExactJSONBytes[OracleBundle](retainedBundlePath)
	if err != nil {
		return SalesforceReconciliation{}, nil, fmt.Errorf("read retained Salesforce bundle: %w", err)
	}
	var loaded loadedSalesforceReconciliation
	if _, err := loadOrchestratorSalesforceReconciliation(plan, lease, receiptPath, packetPath, &loaded); err != nil {
		return SalesforceReconciliation{}, nil, fmt.Errorf("verify retained Salesforce reconciliation: %w", err)
	}
	return validateOrchestratorSalesforceReconciliationSemanticsLoaded(plan, lease, loaded, retainedBundle, retainedBundleBytes, filepath.Dir(retainedBundlePath))
}

func validateOrchestratorSalesforceReconciliationSemanticsLoaded(plan OrchestratorCampaignPlan, lease OrchestratorLease, loaded loadedSalesforceReconciliation, retainedBundle OracleBundle, retainedBundleBytes []byte, retainedBundleRoot string) (SalesforceReconciliation, []salesforceShardEvidenceSnapshot, error) {
	snapshots := loaded.Snapshots
	if !bytes.Equal(loaded.BundleBytes, retainedBundleBytes) || replayBytesSHA256(loaded.BundleBytes) != replayBytesSHA256(retainedBundleBytes) {
		return SalesforceReconciliation{}, nil, fmt.Errorf("retained Salesforce packet bundle bytes drift")
	}
	if loaded.Receipt.BundleSHA256 != replayBytesSHA256(retainedBundleBytes) {
		return SalesforceReconciliation{}, nil, fmt.Errorf("retained Salesforce reconciliation bundle drift")
	}
	replayPlan := loaded.OraclePlan
	if replayBytesSHA256(loaded.OraclePlanBytes) != loaded.Receipt.OraclePlanSHA256 {
		return SalesforceReconciliation{}, nil, fmt.Errorf("retained Salesforce Oracle plan drift")
	}
	if len(snapshots) != 1 {
		return SalesforceReconciliation{}, nil, fmt.Errorf("production batch must retain one Salesforce shard")
	}
	for _, replay := range snapshots {
		postflight, _, postflightErr := decodeReconciliationJSON[SalesforceOrgPreflight](replay.Executor.Files["postflight.json"])
		if postflightErr != nil {
			return SalesforceReconciliation{}, nil, fmt.Errorf("retained Salesforce postflight is not typed")
		}
		if err := validateSalesforceShardSemanticsAt(replayPlan, retainedBundle, retainedBundleRoot, loaded.Receipt.BundleSHA256, retainedBundle.SalesforceExecution, replay.Creation.Command.WorkingDirectory, filepath.Join(replay.Creation.Command.WorkingDirectory, "bundle.json"), replay.Shard, replay.Dispatch, replay.Creation, replay.Cleanup, replay.Shard.Preflight, postflight, replay.Executor, replay.Inputs); err != nil {
			return SalesforceReconciliation{}, nil, fmt.Errorf("validate retained Salesforce lifecycle semantics: %w", err)
		}
	}
	return loaded.Receipt, snapshots, nil
}

func validateOrchestratorProductionBatch(root string, plan OrchestratorCampaignPlan, lease OrchestratorLease) (validatedProductionBatch, error) {
	production := filepath.Join(root, "production")
	manifest, manifestBytes, err := readMode0600JSON[ProductionRuntimeBatch](filepath.Join(production, "PRODUCTION_RUNTIME_BATCH.json"))
	if err != nil || manifest.SchemaVersion != orchestratorProductionBatchSchema || ValidateRuntimeArtifact(manifest.Candidate) != nil || ValidateRuntimeArtifact(manifest.NativeTools) != nil || ValidateRuntimeArtifact(manifest.ExecutedTools) != nil {
		return validatedProductionBatch{}, fmt.Errorf("invalid production runtime batch manifest")
	}
	actualFiles, err := productionManifestFiles(production)
	if err != nil || !reflect.DeepEqual(actualFiles, manifest.Files) {
		return validatedProductionBatch{}, fmt.Errorf("production runtime batch file manifest drift")
	}
	planPath, leasePath := filepath.Join(production, "ORCHESTRATOR_PLAN.json"), filepath.Join(production, "ORCHESTRATOR_LEASE.json")
	retainedPlan, planBytes, err := readMode0600JSON[OrchestratorCampaignPlan](planPath)
	retainedPlanForCompare, planForCompare := retainedPlan, plan
	retainedMaxAttempts, retainedMaxErr := normalizedOrchestratorMaxAttemptsPerJob(retainedPlan.MaxAttemptsPerJob)
	planMaxAttempts, planMaxErr := normalizedOrchestratorMaxAttemptsPerJob(plan.MaxAttemptsPerJob)
	retainedPlanForCompare.MaxAttemptsPerJob, planForCompare.MaxAttemptsPerJob = retainedMaxAttempts, planMaxAttempts
	if err != nil || retainedMaxErr != nil || planMaxErr != nil || !reflect.DeepEqual(retainedPlanForCompare, planForCompare) {
		return validatedProductionBatch{}, fmt.Errorf("production orchestrator plan drift")
	}
	retainedLease, leaseBytes, err := readMode0600JSON[OrchestratorLease](leasePath)
	if err != nil || !reflect.DeepEqual(retainedLease, lease) || validateOrchestratorWorkerPlanLease(retainedPlanForCompare, retainedLease) != nil {
		return validatedProductionBatch{}, fmt.Errorf("production orchestrator lease drift")
	}
	proofPath := filepath.Join(production, "LOCAL_PROOF.json")
	proof, proofBytes, err := readMode0600JSON[LocalProof](proofPath)
	if err != nil {
		return validatedProductionBatch{}, err
	}
	bundlePath := filepath.Join(production, "oracle-bundle", "bundle", "bundle.json")
	if err := validateExactOracleBundleTree(filepath.Join(production, "oracle-bundle")); err != nil {
		return validatedProductionBatch{}, err
	}
	if err := ValidateOracleBundle(bundlePath); err != nil {
		return validatedProductionBatch{}, fmt.Errorf("validate retained oracle bundle: %w", err)
	}
	bundle, bundleBytes, err := readExactJSONBytes[OracleBundle](bundlePath)
	if err != nil {
		return validatedProductionBatch{}, fmt.Errorf("read retained oracle bundle: %w", err)
	}
	if err := validateRetainedProductionLocalProof(proof, proofBytes, filepath.Join(production, "local-proof"), bundle); err != nil {
		return validatedProductionBatch{}, fmt.Errorf("validate retained local proof: %w", err)
	}
	if proof.Candidate != bundle.Candidate || proof.Tools != bundle.Tools || manifest.Candidate != proof.Candidate || manifest.NativeTools != proof.Tools || manifest.Candidate.Commit != plan.Definition.Candidate.Commit || manifest.Candidate.SHA256 != plan.Definition.Candidate.SHA256 || manifest.NativeTools.Commit != plan.Definition.Tools.Commit || manifest.NativeTools.SHA256 != plan.Definition.Tools.SHA256 {
		return validatedProductionBatch{}, fmt.Errorf("production runtime artifacts drift")
	}
	receiptPath, packetPath := filepath.Join(production, "salesforce", "SALESFORCE_RECONCILIATION.json"), filepath.Join(production, "salesforce", "packet")
	var loadedReconciliation loadedSalesforceReconciliation
	if _, err := loadOrchestratorSalesforceReconciliation(retainedPlan, retainedLease, receiptPath, packetPath, &loadedReconciliation); err != nil {
		return validatedProductionBatch{}, err
	}
	reconciliation, reconciliationBytes := loadedReconciliation.Receipt, loadedReconciliation.ReceiptBytes
	if _, _, err := validateOrchestratorSalesforceReconciliationSemanticsLoaded(retainedPlan, retainedLease, loadedReconciliation, bundle, bundleBytes, filepath.Dir(bundlePath)); err != nil {
		return validatedProductionBatch{}, err
	}
	review, _, err := readMode0600JSON[ProductionRuntimeReview](filepath.Join(production, "PRODUCTION_REVIEW.json"))
	if err != nil || review.SchemaVersion != 1 || review.PlanSHA256 != replayBytesSHA256(planBytes) || review.LeaseSHA256 != replayBytesSHA256(leaseBytes) || review.LocalProofSHA256 != replayBytesSHA256(proofBytes) || review.ReconciliationSHA256 != replayBytesSHA256(reconciliationBytes) {
		return validatedProductionBatch{}, fmt.Errorf("production review authority drift")
	}
	expectedRows := make([]ProductionRuntimeReviewRow, len(reconciliation.Rows))
	states := make(map[string]string, len(reconciliation.Rows))
	for index, row := range reconciliation.Rows {
		if states[row.SurfaceID] != "" {
			return validatedProductionBatch{}, fmt.Errorf("production receipt requires unique rows")
		}
		classification, disposition, state := "mismatch", "confirmed-mismatch", "rejected"
		if row.Passed {
			classification, disposition, state = "match", "confirmed-match", "accepted"
		}
		expectedRows[index] = ProductionRuntimeReviewRow{SurfaceID: row.SurfaceID, Action: row.Action, Classification: classification, ReviewDisposition: disposition}
		states[row.SurfaceID] = state
	}
	if !reflect.DeepEqual(review.Rows, expectedRows) || len(states) != len(lease.SurfaceIDs) {
		return validatedProductionBatch{}, fmt.Errorf("production review rows do not match exact lease")
	}
	for _, surfaceID := range lease.SurfaceIDs {
		if states[surfaceID] != "accepted" && states[surfaceID] != "rejected" {
			return validatedProductionBatch{}, fmt.Errorf("production review rows do not match exact lease")
		}
	}
	if err := validateProductionRaw(filepath.Join(production, "raw"), retainedPlan, retainedLease, reconciliation, bundle); err != nil {
		return validatedProductionBatch{}, err
	}
	sshRoot := filepath.Join(production, "ssh")
	if _, err := os.Lstat(sshRoot); err == nil {
		if err := validateProductionSSH(sshRoot, filepath.Join(production, "raw"), retainedPlan, retainedLease, replayBytesSHA256(planBytes), replayBytesSHA256(leaseBytes), manifest.NativeTools, manifest.ExecutedTools); err != nil {
			return validatedProductionBatch{}, err
		}
	} else if !os.IsNotExist(err) {
		return validatedProductionBatch{}, err
	} else if manifest.ExecutedTools != manifest.NativeTools {
		return validatedProductionBatch{}, fmt.Errorf("local production batch executed Tools drift")
	}
	paths := make([]string, 0, len(manifest.Files)+1)
	for _, file := range manifest.Files {
		paths = append(paths, filepath.ToSlash(filepath.Join("production", file.Path)))
	}
	paths = append(paths, "production/PRODUCTION_RUNTIME_BATCH.json")
	sort.Strings(paths)
	return validatedProductionBatch{manifest: manifest, manifestSHA256: replayBytesSHA256(manifestBytes), proofStates: states, paths: paths, OrgAlias: reconciliation.Shards[0].OrgAlias, DevHub: bundle.DevHub}, nil
}

func validateProductionSources(request BuildOrchestratorProductionBatchRequest, plan OrchestratorCampaignPlan, lease OrchestratorLease, ssh bool) (RuntimeArtifact, error) {
	proof, proofBytes, err := readMode0600JSON[LocalProof](request.LocalProofPath)
	if err != nil {
		return RuntimeArtifact{}, err
	}
	fixtureManifest, fixtureManifestBytes, err := readExactJSONBytes[LocalProofFixtureManifest](proof.FixtureManifestPath)
	if err != nil || ValidateLocalProof(proof, fixtureManifest) != nil {
		return RuntimeArtifact{}, fmt.Errorf("validate local proof")
	}
	bundlePath := filepath.Join(request.OracleBundleRoot, "bundle", "bundle.json")
	if _, err := oracleBundleControlledPaths(request.OracleBundleRoot); err != nil {
		return RuntimeArtifact{}, err
	}
	if err := ValidateOracleBundle(bundlePath); err != nil {
		return RuntimeArtifact{}, fmt.Errorf("validate exact oracle bundle: %w", err)
	}
	bundle, _, err := readExactJSONBytes[OracleBundle](bundlePath)
	attemptBytes, attemptErr := os.ReadFile(proof.AttemptPath)
	if err != nil || attemptErr != nil || replayBytesSHA256(proofBytes) != bundle.LocalProofSHA256 || replayBytesSHA256(fixtureManifestBytes) != bundle.FixtureManifestSHA256 || replayBytesSHA256(attemptBytes) != bundle.AttemptSHA256 || proof.Candidate != bundle.Candidate || proof.Tools != bundle.Tools {
		return RuntimeArtifact{}, fmt.Errorf("local proof and oracle bundle drift")
	}
	if err := VerifyOrchestratorSalesforceReconciliation(plan, lease, request.SalesforceReconciliationPath, request.SalesforcePacketPath); err != nil {
		return RuntimeArtifact{}, err
	}
	reconciliation, reconciliationBytes, err := readMode0600JSON[SalesforceReconciliation](request.SalesforceReconciliationPath)
	if err != nil || validateProductionRaw(request.RawRoot, plan, lease, reconciliation, bundle) != nil {
		return RuntimeArtifact{}, fmt.Errorf("validate production raw evidence")
	}
	planBytes, _ := os.ReadFile(request.PlanPath)
	leaseBytes, _ := os.ReadFile(request.LeasePath)
	review, _, err := readMode0600JSON[ProductionRuntimeReview](request.ReviewPath)
	if err != nil || review.SchemaVersion != 1 || review.PlanSHA256 != replayBytesSHA256(planBytes) || review.LeaseSHA256 != replayBytesSHA256(leaseBytes) || review.LocalProofSHA256 != replayBytesSHA256(proofBytes) || review.ReconciliationSHA256 != replayBytesSHA256(reconciliationBytes) {
		return RuntimeArtifact{}, fmt.Errorf("production review authority drift")
	}
	executed := proof.Tools
	if ssh {
		dispatch, _, err := readMode0600JSON[OrchestratorSSHDispatchReceipt](request.SSHDispatchPath)
		if err != nil || dispatch.ExecutedTools == (RuntimeArtifact{}) {
			return RuntimeArtifact{}, fmt.Errorf("production SSH dispatch lacks executed Tools")
		}
		executed = dispatch.ExecutedTools
		if err := validateProductionSSHFiles(request.SSHDispatchPath, request.SSHFetchPath, request.SSHTreeManifestPath, request.RawRoot, plan, lease, replayBytesSHA256(planBytes), replayBytesSHA256(leaseBytes), proof.Tools, executed); err != nil {
			return RuntimeArtifact{}, err
		}
	}
	return executed, nil
}

func copyProductionLocalProofInputs(proof LocalProof, destination string) error {
	manifest, _, err := readExactJSONBytes[LocalProofFixtureManifest](proof.FixtureManifestPath)
	if err != nil {
		return err
	}
	if err := os.Mkdir(destination, 0o700); err != nil {
		return err
	}
	if err := copyProductionFile(proof.AttemptPath, filepath.Join(destination, "ATTEMPT.json")); err != nil {
		return err
	}
	if err := copyProductionFile(proof.FixtureManifestPath, filepath.Join(destination, "FIXTURE_MANIFEST.json")); err != nil {
		return err
	}
	fixtures := filepath.Join(destination, "fixtures")
	if err := os.Mkdir(fixtures, 0o700); err != nil {
		return err
	}
	for index, fixture := range manifest.Fixtures {
		if err := copyProductionFile(fixture.Path, filepath.Join(fixtures, fmt.Sprintf("%06d.json", index))); err != nil {
			return err
		}
	}
	return nil
}

func validateRetainedProductionLocalProof(proof LocalProof, proofBytes []byte, root string, bundle OracleBundle) error {
	manifestPath, attemptPath := filepath.Join(root, "FIXTURE_MANIFEST.json"), filepath.Join(root, "ATTEMPT.json")
	manifest, manifestBytes, err := readExactJSONBytes[LocalProofFixtureManifest](manifestPath)
	if err != nil {
		return err
	}
	_, attemptBytes, err := readExactJSONBytes[AssuranceAttempt](attemptPath)
	if err != nil {
		return err
	}
	for name, pair := range map[string][2]string{
		"proof":    {replayBytesSHA256(proofBytes), bundle.LocalProofSHA256},
		"manifest": {replayBytesSHA256(manifestBytes), bundle.FixtureManifestSHA256},
		"attempt":  {replayBytesSHA256(attemptBytes), bundle.AttemptSHA256},
	} {
		if pair[0] != pair[1] {
			return fmt.Errorf("retained local proof %s authority drift", name)
		}
	}
	want := []string{"ATTEMPT.json", "FIXTURE_MANIFEST.json"}
	retainedManifest := manifest
	retainedManifest.Fixtures = append([]LocalProofFixture(nil), manifest.Fixtures...)
	for index := range retainedManifest.Fixtures {
		relative := filepath.ToSlash(filepath.Join("fixtures", fmt.Sprintf("%06d.json", index)))
		path := filepath.Join(root, filepath.FromSlash(relative))
		hash, hashErr := sha256File(path)
		if hashErr != nil || hash != retainedManifest.Fixtures[index].SHA256 {
			return fmt.Errorf("retained local proof fixture drift")
		}
		retainedManifest.Fixtures[index].Path = path
		want = append(want, relative)
	}
	sort.Strings(want)
	actual, err := regularTreePaths(root)
	if err != nil || !reflect.DeepEqual(actual, want) {
		return fmt.Errorf("retained local proof input tree drift")
	}
	retainedProof := proof
	retainedProof.AttemptPath = attemptPath
	retainedProof.FixtureManifestPath = manifestPath
	retainedProof.ProfilePath, retainedProof.UsagePath, retainedProof.DecisionPath = "", "", ""
	retainedProof.CandidatePath, retainedProof.ToolsPath = "", ""
	return ValidateLocalProof(retainedProof, retainedManifest)
}

func validateProductionRaw(root string, plan OrchestratorCampaignPlan, lease OrchestratorLease, reconciliation SalesforceReconciliation, bundle OracleBundle) error {
	entries, err := os.ReadDir(root)
	want := orchestratorSSHRawFileNames()
	if err != nil || len(entries) != len(want) || len(reconciliation.Shards) != 1 {
		return fmt.Errorf("production raw file set is not exact")
	}
	modes := map[string]os.FileMode{"ORCHESTRATOR_BINDING.json": 0o400}
	for _, name := range want {
		if modes[name] == 0 {
			modes[name] = 0o600
		}
		info, statErr := os.Lstat(filepath.Join(root, name))
		if statErr != nil || !info.Mode().IsRegular() || info.Mode().Perm() != modes[name] {
			return fmt.Errorf("production raw file set is invalid")
		}
	}
	shard := reconciliation.Shards[0]
	hashes := map[string]string{"ORCHESTRATOR_BINDING.json": reconciliation.OrchestratorBindingSHA256, "ORG_CREATION.json": shard.InputSHA256["creation"], "ORG_PREFLIGHT.json": shard.InputSHA256["preflight"], "SALESFORCE_DISPATCH.json": shard.InputSHA256["dispatch"], "SALESFORCE_SHARD.json": shard.InputSHA256["shard"], "ORG_CLEANUP.json": shard.InputSHA256["cleanup"]}
	for name, hash := range hashes {
		if actual, hashErr := sha256File(filepath.Join(root, name)); hashErr != nil || actual != hash {
			return fmt.Errorf("production raw hash drift")
		}
	}
	binding, _, err := readExactJSONBytes[OrchestratorBatchBinding](filepath.Join(root, "ORCHESTRATOR_BINDING.json"))
	wantBinding, wantErr := expectedOrchestratorBatchBinding(plan, lease)
	if err != nil || wantErr != nil || !reflect.DeepEqual(binding, wantBinding) {
		return fmt.Errorf("production raw orchestrator binding drift")
	}
	reservation, _, err := readExactJSONBytes[salesforceOrgReservation](filepath.Join(root, "ORG_CREATION.json.reservation"))
	creation, _, creationErr := readExactJSONBytes[SalesforceOrgCreation](filepath.Join(root, "ORG_CREATION.json"))
	if err != nil || creationErr != nil || reservation.SchemaVersion != 1 || reservation.BundleSHA256 != reconciliation.BundleSHA256 || reservation.DevHub != bundle.DevHub || reservation.Alias != creation.Alias || reservation.Marker != creation.Marker || !validSalesforceScratchMarker(reservation.Marker) || !validRetainedCommandOutput(reservation.AliasAbsent) || reservation.AliasAbsent.Passed || reservation.AliasAbsent.ExitCode == 0 || reservation.AliasAbsent.TimedOut || !validSalesforceOrgDisplayFailure(reservation.AliasAbsent.Output.Stdout) {
		return fmt.Errorf("production raw reservation drift")
	}
	return nil
}

func validateProductionSSH(root, rawRoot string, plan OrchestratorCampaignPlan, lease OrchestratorLease, planSHA, leaseSHA string, native, executed RuntimeArtifact) error {
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 3 {
		return fmt.Errorf("production SSH receipts are not exact")
	}
	return validateProductionSSHFiles(filepath.Join(root, "SSH_DISPATCH.json"), filepath.Join(root, "SSH_FETCH.json"), filepath.Join(root, "TREE_MANIFEST.json"), rawRoot, plan, lease, planSHA, leaseSHA, native, executed)
}

func validateProductionSSHFiles(dispatchPath, fetchPath, treePath, rawRoot string, plan OrchestratorCampaignPlan, lease OrchestratorLease, planSHA, leaseSHA string, native, executed RuntimeArtifact) error {
	dispatch, dispatchBytes, err := readMode0600JSON[OrchestratorSSHDispatchReceipt](dispatchPath)
	fetch, _, fetchErr := readMode0600JSON[OrchestratorSSHRawFetchReceipt](fetchPath)
	tree, treeBytes, treeErr := readMode0600JSON[[]orchestratorSSHRawManifestEntry](treePath)
	if err != nil || fetchErr != nil || treeErr != nil || dispatch.ExecutedTools != executed || fetch.ExecutedTools != executed || !validProductionExecutedTools(executed, native, plan) {
		return fmt.Errorf("invalid typed production SSH receipts")
	}
	if dispatch.SchemaVersion != 1 || !dispatch.Passed || dispatch.Status != "worker-complete" || dispatch.ExitCode != 0 || dispatch.DurationMS < 0 || dispatch.TimeoutMS != orchestratorSSHTimeout.Milliseconds() || dispatch.TimedOut || dispatch.FailureCode != "" || dispatch.ActionRequired || dispatch.ActionCode != "" || dispatch.CampaignID != lease.CampaignID || dispatch.JobID != lease.JobID || dispatch.ShardIndex != lease.ShardIndex || dispatch.Generation != lease.Generation || dispatch.SpecSHA256 != plan.SpecSHA256 || dispatch.PlanSHA256 != planSHA || dispatch.LeaseSHA256 != leaseSHA || !sha256Pattern.MatchString(dispatch.CommandSHA256) || !sha256Pattern.MatchString(dispatch.StdoutSHA256) || !sha256Pattern.MatchString(dispatch.StderrSHA256) || !sha256Pattern.MatchString(dispatch.OrchestratorBindingSHA256) || !sha256Pattern.MatchString(dispatch.SalesforceShardSHA256) || !sha256Pattern.MatchString(dispatch.OrgCleanupSHA256) {
		return fmt.Errorf("production SSH dispatch drift")
	}
	if fetch.SchemaVersion != 1 || !fetch.Passed || fetch.Status != "fetched" || fetch.CampaignID != lease.CampaignID || fetch.JobID != lease.JobID || fetch.ShardIndex != lease.ShardIndex || fetch.Generation != lease.Generation || fetch.SpecSHA256 != plan.SpecSHA256 || fetch.PlanSHA256 != planSHA || fetch.LeaseSHA256 != leaseSHA || fetch.SSHReceiptSHA256 != replayBytesSHA256(dispatchBytes) || fetch.TreeManifestSHA256 != replayBytesSHA256(treeBytes) || fetch.OrchestratorBindingSHA256 != dispatch.OrchestratorBindingSHA256 || fetch.SalesforceShardSHA256 != dispatch.SalesforceShardSHA256 || fetch.OrgCleanupSHA256 != dispatch.OrgCleanupSHA256 || !sha256Pattern.MatchString(fetch.CopyStdoutSHA256) || !sha256Pattern.MatchString(fetch.CopyStderrSHA256) || !sha256Pattern.MatchString(fetch.ChecksumStdoutSHA256) || !sha256Pattern.MatchString(fetch.ChecksumStderrSHA256) {
		return fmt.Errorf("production SSH fetch drift")
	}
	expected := make([]orchestratorSSHRawManifestEntry, 0, len(orchestratorSSHRawFileNames()))
	for _, name := range orchestratorSSHRawFileNames() {
		info, statErr := os.Lstat(filepath.Join(rawRoot, name))
		hash, hashErr := sha256File(filepath.Join(rawRoot, name))
		if statErr != nil || hashErr != nil {
			return fmt.Errorf("production SSH raw tree drift")
		}
		expected = append(expected, orchestratorSSHRawManifestEntry{Path: name, Mode: fmt.Sprintf("%04o", info.Mode().Perm()), SHA256: hash})
	}
	sort.Slice(expected, func(i, j int) bool { return expected[i].Path < expected[j].Path })
	if !reflect.DeepEqual(tree, expected) {
		return fmt.Errorf("production SSH tree manifest drift")
	}
	return nil
}

func validProductionExecutedTools(executed, native RuntimeArtifact, plan OrchestratorCampaignPlan) bool {
	if executed == native {
		return true
	}
	return ValidateRuntimeArtifact(executed) == nil && executed.Commit == plan.Definition.Tools.Commit && executed.OS == "darwin" && executed.Arch == "amd64" && plan.Definition.ControlledInputSHA256[OrchestratorToolsAMD64Input] != "" && executed.SHA256 == plan.Definition.ControlledInputSHA256[OrchestratorToolsAMD64Input]
}

func readMode0600JSON[T any](path string) (T, []byte, error) {
	var value T
	snapshot, err := readRegularFileSnapshot(path)
	if err != nil || snapshot.Mode.Perm() != 0o600 {
		return value, nil, fmt.Errorf("mode-0600 regular JSON authority is required")
	}
	if err := decodeExactJSON(snapshot.Data, &value); err != nil {
		return value, nil, err
	}
	return value, snapshot.Data, nil
}

func productionManifestFiles(root string) ([]ProductionRuntimeBatchFile, error) {
	files := []ProductionRuntimeBatchFile{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || path == root {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
			return fmt.Errorf("production batch contains unsupported file type")
		}
		if info.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if filepath.ToSlash(relative) == "PRODUCTION_RUNTIME_BATCH.json" {
			return nil
		}
		hash, err := sha256File(path)
		if err != nil {
			return err
		}
		files = append(files, ProductionRuntimeBatchFile{Path: filepath.ToSlash(relative), Mode: fmt.Sprintf("%04o", info.Mode().Perm()), SHA256: hash})
		return nil
	})
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, err
}

func verifyProductionBatchIdentity(root, manifestSHA256 string, files []ProductionRuntimeBatchFile) error {
	manifest, err := readRegularFileSnapshot(filepath.Join(root, "production", "PRODUCTION_RUNTIME_BATCH.json"))
	if err != nil || manifest.Mode.Perm() != 0o600 || replayBytesSHA256(manifest.Data) != manifestSHA256 {
		return fmt.Errorf("production runtime batch manifest identity drift")
	}
	actual, err := productionManifestFiles(filepath.Join(root, "production"))
	if err != nil || !reflect.DeepEqual(actual, files) {
		return fmt.Errorf("production runtime batch retained file drift")
	}
	return nil
}

func copyProductionTree(source, destination string) error {
	info, err := os.Lstat(source)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("production tree source must be a directory")
	}
	return filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
			return fmt.Errorf("production tree source contains unsupported file type")
		}
		if info.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		return copyProductionFile(path, target)
	})
}

func copyProductionNamedFiles(sourceRoot, destinationRoot string, names []string) error {
	if err := os.Mkdir(destinationRoot, 0o700); err != nil {
		return err
	}
	for _, name := range names {
		if err := copyProductionFile(filepath.Join(sourceRoot, name), filepath.Join(destinationRoot, name)); err != nil {
			return err
		}
	}
	return nil
}

func copyProductionFile(source, destination string) error {
	snapshot, err := readRegularFileSnapshot(source)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, snapshot.Mode.Perm())
	if err != nil {
		return err
	}
	if _, err = file.Write(snapshot.Data); err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	return err
}

func validateExactOracleBundleTree(root string) error {
	want, err := oracleBundleControlledPaths(root)
	if err != nil {
		return err
	}
	actual, err := regularTreePaths(root)
	if err != nil || !reflect.DeepEqual(actual, want) {
		return fmt.Errorf("oracle bundle controlled tree is not exact: got %v want %v", actual, want)
	}
	return nil
}

func oracleBundleControlledPaths(root string) ([]string, error) {
	bundle, _, err := readExactJSONBytes[OracleBundle](filepath.Join(root, "bundle", "bundle.json"))
	manifest, _, manifestErr := readExactJSONBytes[oracleTransportManifest](filepath.Join(root, "bundle", "fixture-manifest.json"))
	if err != nil || manifestErr != nil {
		return nil, fmt.Errorf("read oracle bundle controlled tree")
	}
	want := map[string]bool{}
	for _, path := range []string{"bundle/bundle.json", "bundle/profile.json", "bundle/ORACLE_PLAN.json", "bundle/EXCLUSION_AUTHORITY.json", "bundle/RELEASE_VALIDATION.json", "bundle/ATTEMPT.json", "bundle/DEV_HUB_AUTHORITY.json", "bundle/LOCAL_PROOF_SUMMARY.json", "bundle/fixture-manifest.json", "bundle/corpus-assurance-scratch-def.json", "transport/salesforce-first-filter.py", "bin/glade-tools-darwin-amd64"} {
		want[path] = true
	}
	if bundle.SalesforceRemoteCleanupAuthoritySHA256 != "" {
		want["bundle/SALESFORCE_REMOTE_CLEANUP_AUTHORITY.json"] = true
	}
	for _, fixture := range manifest.Fixtures {
		want[filepath.ToSlash(filepath.Join("bundle", fixture.Path))] = true
		for _, source := range fixture.SourceFiles {
			want[filepath.ToSlash(filepath.Join("bundle", source.Path))] = true
		}
	}
	expected := sortedBoolKeys(want)
	for _, relative := range expected {
		info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("oracle bundle controlled file is unavailable")
		}
	}
	return expected, nil
}

func regularTreePaths(root string) ([]string, error) {
	paths := []string{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || path == root {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
			return fmt.Errorf("unsupported controlled tree file type")
		}
		if info.Mode().IsRegular() {
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			paths = append(paths, filepath.ToSlash(relative))
		}
		return nil
	})
	sort.Strings(paths)
	return paths, err
}

func sortedBoolKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
