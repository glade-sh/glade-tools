package corpusassurance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

type SalesforceReconciliationRequest struct {
	OraclePlanPath string
	ShardFiles     []SalesforceShardFiles
	PacketOutput   string
	OutputPath     string
}

type OrchestratorSalesforceReconciliationRequest struct {
	Plan           OrchestratorCampaignPlan
	Lease          OrchestratorLease
	ScopePath      string
	OraclePlanPath string
	BindingPath    string
	ShardFiles     SalesforceShardFiles
	PacketOutput   string
	OutputPath     string
}

type SalesforceReconciliation struct {
	SchemaVersion             int                             `json:"schemaVersion"`
	Status                    string                          `json:"status"`
	Candidate                 RuntimeArtifact                 `json:"candidate"`
	Tools                     RuntimeArtifact                 `json:"tools"`
	OraclePlanSHA256          string                          `json:"oraclePlanSha256"`
	OrchestratorPlanSHA256    string                          `json:"orchestratorPlanSha256,omitempty"`
	OrchestratorBindingSHA256 string                          `json:"orchestratorBindingSha256,omitempty"`
	BundleSHA256              string                          `json:"bundleSha256"`
	Rows                      []SalesforceReconciliationRow   `json:"rows"`
	Shards                    []SalesforceReconciliationShard `json:"shards"`
	PacketManifestSHA256      string                          `json:"packetManifestSha256"`
}

type SalesforceReconciliationRow struct {
	SurfaceID string `json:"surfaceId"`
	Action    string `json:"action"`
	Passed    bool   `json:"passed"`
}

type SalesforceReconciliationShard struct {
	ShardIndex             int               `json:"shardIndex"`
	RunID                  string            `json:"runId"`
	OrgAlias               string            `json:"orgAlias"`
	OrgID                  string            `json:"orgId"`
	ExecutorManifestSHA256 string            `json:"executorManifestSha256"`
	InputSHA256            map[string]string `json:"inputSha256"`
}

type reconciliationPacketFile struct {
	Name   string
	Source string
	Data   []byte
	Mode   os.FileMode
}

const reconciliationPacketManifestName = "MANIFEST.json"

func CreateOrchestratorSalesforceReconciliation(request OrchestratorSalesforceReconciliationRequest) (SalesforceReconciliation, error) {
	scopePath := request.ScopePath
	if scopePath == "" {
		scopePath = request.Plan.Definition.ScopePath
	}
	if err := validateOrchestratorWorkerPlanLeaseAtScope(request.Plan, request.Lease, scopePath); err != nil {
		return SalesforceReconciliation{}, err
	}
	if !filepath.IsAbs(request.BindingPath) {
		return SalesforceReconciliation{}, fmt.Errorf("absolute orchestrator binding path is required")
	}
	plan, planBytes, err := readExactJSONBytes[OraclePlan](request.OraclePlanPath)
	if err != nil || request.Plan.Definition.ControlledInputSHA256["oracle-plan"] != replayBytesSHA256(planBytes) {
		return SalesforceReconciliation{}, fmt.Errorf("controlled oracle plan binding drift")
	}
	if request.Plan.Definition.Candidate.Commit != plan.Candidate.Commit || request.Plan.Definition.Candidate.SHA256 != plan.Candidate.SHA256 || request.Plan.Definition.Tools.Commit != plan.Tools.Commit || request.Plan.Definition.Tools.SHA256 != plan.Tools.SHA256 {
		return SalesforceReconciliation{}, fmt.Errorf("orchestrator plan does not bind oracle artifacts")
	}
	expected, err := orchestratorSalesforceExpectedSurfaceIDsAtScope(plan, request.Plan, request.Lease, scopePath)
	if err != nil {
		return SalesforceReconciliation{}, err
	}
	bindingSnapshot, err := readRegularFileSnapshot(request.BindingPath)
	if err != nil || bindingSnapshot.Mode.Perm() != 0o400 {
		return SalesforceReconciliation{}, fmt.Errorf("orchestrator binding must be a mode 0400 regular file")
	}
	var binding OrchestratorBatchBinding
	if err := decodeExactJSON(bindingSnapshot.Data, &binding); err != nil {
		return SalesforceReconciliation{}, fmt.Errorf("invalid orchestrator binding: %w", err)
	}
	wantBinding, err := expectedOrchestratorBatchBindingAtScope(request.Plan, request.Lease, scopePath)
	if err != nil || !reflect.DeepEqual(binding, wantBinding) {
		return SalesforceReconciliation{}, fmt.Errorf("orchestrator binding drift")
	}
	orchestratorPlanBytes, err := json.Marshal(request.Plan)
	if err != nil {
		return SalesforceReconciliation{}, err
	}
	orchestratorPlanBytes = append(orchestratorPlanBytes, '\n')
	return createSalesforceReconciliation(
		SalesforceReconciliationRequest{OraclePlanPath: request.OraclePlanPath, ShardFiles: []SalesforceShardFiles{request.ShardFiles}, PacketOutput: request.PacketOutput, OutputPath: request.OutputPath},
		2, salesforceShardValidationScope{ExpectedSurfaceIDs: expected, LogicalShardCount: len(request.Plan.Jobs)},
		[]reconciliationPacketFile{
			{Name: "ORCHESTRATOR_BINDING.json", Source: request.BindingPath, Data: bindingSnapshot.Data, Mode: bindingSnapshot.Mode},
		}, replayBytesSHA256(orchestratorPlanBytes), replayBytesSHA256(bindingSnapshot.Data),
	)
}

func CreateSalesforceReconciliation(request SalesforceReconciliationRequest) (SalesforceReconciliation, error) {
	return createSalesforceReconciliation(request, 1, salesforceShardValidationScope{}, nil, "", "")
}

func createSalesforceReconciliation(request SalesforceReconciliationRequest, schemaVersion int, scope salesforceShardValidationScope, extraPacketFiles []reconciliationPacketFile, orchestratorPlanSHA, orchestratorBindingSHA string) (SalesforceReconciliation, error) {
	for _, path := range []string{request.OraclePlanPath, request.PacketOutput, request.OutputPath} {
		if !filepath.IsAbs(path) {
			return SalesforceReconciliation{}, fmt.Errorf("absolute Salesforce reconciliation paths are required")
		}
	}
	if len(request.ShardFiles) == 0 {
		return SalesforceReconciliation{}, fmt.Errorf("Salesforce shard files are required")
	}
	if err := requireNewPath(request.PacketOutput); err != nil {
		return SalesforceReconciliation{}, err
	}
	if err := requireNewPath(request.OutputPath); err != nil {
		return SalesforceReconciliation{}, err
	}
	var snapshots []salesforceShardEvidenceSnapshot
	if schemaVersion == 1 {
		if err := validateSalesforceShardFiles(request.OraclePlanPath, request.ShardFiles, &snapshots); err != nil {
			return SalesforceReconciliation{}, err
		}
	} else if err := validateSalesforceShardFiles(request.OraclePlanPath, request.ShardFiles, &snapshots, scope); err != nil {
		return SalesforceReconciliation{}, err
	}
	plan, planBytes, err := readExactJSONBytes[OraclePlan](request.OraclePlanPath)
	if err != nil {
		return SalesforceReconciliation{}, err
	}
	bundlePath := filepath.Join(filepath.Dir(request.OraclePlanPath), "bundle.json")
	_, bundleBytes, err := readExactJSONBytes[OracleBundle](bundlePath)
	if err != nil {
		return SalesforceReconciliation{}, fmt.Errorf("read staged Oracle bundle: %w", err)
	}
	packetFiles := append([]reconciliationPacketFile(nil), extraPacketFiles...)
	addSource := func(name, source string) error {
		snapshot, err := readRegularFileSnapshot(source)
		if err != nil {
			return err
		}
		packetFiles = append(packetFiles, reconciliationPacketFile{Name: name, Source: source, Data: snapshot.Data, Mode: snapshot.Mode})
		return nil
	}
	if err := addSource("ORACLE_PLAN.json", request.OraclePlanPath); err != nil {
		return SalesforceReconciliation{}, err
	}
	if err := addSource("bundle.json", bundlePath); err != nil {
		return SalesforceReconciliation{}, err
	}
	receipt := SalesforceReconciliation{SchemaVersion: schemaVersion, Status: "pass", Candidate: plan.Candidate, Tools: plan.Tools, OraclePlanSHA256: replayBytesSHA256(planBytes), OrchestratorPlanSHA256: orchestratorPlanSHA, OrchestratorBindingSHA256: orchestratorBindingSHA, BundleSHA256: replayBytesSHA256(bundleBytes), Shards: make([]SalesforceReconciliationShard, 0, len(snapshots))}
	rowSet := make([]SalesforceReconciliationRow, 0)
	for index, snapshot := range snapshots {
		shardPath := request.ShardFiles[index]
		for name, source := range map[string]string{
			fmt.Sprintf("shards/%02d/SALESFORCE_SHARD.json", snapshot.Shard.ShardIndex):    shardPath.ShardPath,
			fmt.Sprintf("shards/%02d/SALESFORCE_DISPATCH.json", snapshot.Shard.ShardIndex): shardPath.DispatchPath,
			fmt.Sprintf("shards/%02d/ORG_PREFLIGHT.json", snapshot.Shard.ShardIndex):       shardPath.PreflightPath,
			fmt.Sprintf("shards/%02d/ORG_CREATION.json", snapshot.Shard.ShardIndex):        shardPath.CreationPath,
			fmt.Sprintf("shards/%02d/ORG_CLEANUP.json", snapshot.Shard.ShardIndex):         shardPath.CleanupPath,
		} {
			if err := addSource(name, source); err != nil {
				return SalesforceReconciliation{}, err
			}
		}
		manifestName := fmt.Sprintf("shards/%02d/%s", snapshot.Shard.ShardIndex, salesforceExecutorManifestName)
		packetFiles = append(packetFiles, reconciliationPacketFile{Name: manifestName, Data: snapshot.Executor.Manifest.Data, Mode: snapshot.Executor.Manifest.Mode})
		for path, file := range snapshot.Executor.Snapshots {
			packetFiles = append(packetFiles, reconciliationPacketFile{Name: fmt.Sprintf("shards/%02d/executor/%s", snapshot.Shard.ShardIndex, path), Data: file.Data, Mode: file.Mode})
		}
		inputHashes := map[string]string{}
		for name, hash := range snapshot.Inputs {
			inputHashes[name] = hash
		}
		receipt.Shards = append(receipt.Shards, SalesforceReconciliationShard{ShardIndex: snapshot.Shard.ShardIndex, RunID: snapshot.Shard.RunID, OrgAlias: snapshot.Shard.OrgAlias, OrgID: snapshot.Shard.OrgID, ExecutorManifestSHA256: snapshot.Executor.ManifestSHA256, InputSHA256: inputHashes})
		for _, result := range snapshot.Shard.Results {
			rowSet = append(rowSet, SalesforceReconciliationRow{SurfaceID: result.SurfaceID, Action: result.Kind, Passed: result.Passed})
		}
	}
	sort.Slice(receipt.Shards, func(left, right int) bool { return receipt.Shards[left].ShardIndex < receipt.Shards[right].ShardIndex })
	sort.Slice(rowSet, func(left, right int) bool {
		if rowSet[left].SurfaceID != rowSet[right].SurfaceID {
			return rowSet[left].SurfaceID < rowSet[right].SurfaceID
		}
		return rowSet[left].Action < rowSet[right].Action
	})
	receipt.Rows = rowSet
	packetManifest, err := writeReconciliationPacket(request.PacketOutput, packetFiles)
	if err != nil {
		return SalesforceReconciliation{}, err
	}
	receipt.PacketManifestSHA256 = packetManifest
	tempReceipt, err := os.CreateTemp(filepath.Dir(request.OutputPath), ".salesforce-reconciliation-*")
	if err != nil {
		return SalesforceReconciliation{}, err
	}
	tempReceiptPath := tempReceipt.Name()
	if err := tempReceipt.Close(); err != nil {
		_ = os.Remove(tempReceiptPath)
		return SalesforceReconciliation{}, err
	}
	if err := os.Remove(tempReceiptPath); err != nil {
		return SalesforceReconciliation{}, err
	}
	defer os.Remove(tempReceiptPath)
	if err := WriteNewJSON(tempReceiptPath, receipt); err != nil {
		return SalesforceReconciliation{}, err
	}
	if err := os.Rename(tempReceiptPath, request.OutputPath); err != nil {
		return SalesforceReconciliation{}, err
	}
	return receipt, nil
}

func VerifySalesforceReconciliation(oraclePlanPath, receiptPath, packetPath string) error {
	_, err := loadSalesforceReconciliation(oraclePlanPath, receiptPath, packetPath)
	return err
}

func loadSalesforceReconciliation(oraclePlanPath, receiptPath, packetPath string) ([]salesforceShardEvidenceSnapshot, error) {
	return loadSalesforceReconciliationVersion(oraclePlanPath, receiptPath, packetPath, 1, nil, 0, nil)
}

func VerifyOrchestratorSalesforceReconciliation(plan OrchestratorCampaignPlan, lease OrchestratorLease, receiptPath, packetPath string) error {
	_, err := loadOrchestratorSalesforceReconciliation(plan, lease, receiptPath, packetPath)
	return err
}

type loadedSalesforceReconciliation struct {
	Receipt         SalesforceReconciliation
	ReceiptBytes    []byte
	OraclePlan      OraclePlan
	OraclePlanBytes []byte
	Bundle          OracleBundle
	BundleBytes     []byte
	Snapshots       []salesforceShardEvidenceSnapshot
}

func loadOrchestratorSalesforceReconciliation(plan OrchestratorCampaignPlan, lease OrchestratorLease, receiptPath, packetPath string, loadedOut ...*loadedSalesforceReconciliation) ([]salesforceShardEvidenceSnapshot, error) {
	if err := validateOrchestratorWorkerPlanLease(plan, lease); err != nil {
		return nil, err
	}
	var loaded loadedSalesforceReconciliation
	snapshots, err := loadSalesforceReconciliationVersion("", receiptPath, packetPath, 2, func(oraclePlan OraclePlan) ([]string, error) {
		return orchestratorSalesforceExpectedSurfaceIDs(oraclePlan, plan, lease)
	}, len(plan.Jobs), func(receipt SalesforceReconciliation, files map[string]reportInputSnapshot, oraclePlan OraclePlan, oraclePlanBytes []byte) error {
		planBytes, marshalErr := json.Marshal(plan)
		if marshalErr != nil {
			return marshalErr
		}
		planBytes = append(planBytes, '\n')
		binding, _, bindingErr := decodeReconciliationJSON[OrchestratorBatchBinding](files["ORCHESTRATOR_BINDING.json"].Data)
		wantBinding, wantErr := expectedOrchestratorBatchBinding(plan, lease)
		if replayBytesSHA256(planBytes) != receipt.OrchestratorPlanSHA256 {
			return fmt.Errorf("orchestrator plan does not bind the receipt")
		}
		if bindingErr != nil || wantErr != nil || !reflect.DeepEqual(binding, wantBinding) || files["ORCHESTRATOR_BINDING.json"].Mode.Perm() != 0o400 || replayBytesSHA256(files["ORCHESTRATOR_BINDING.json"].Data) != receipt.OrchestratorBindingSHA256 {
			return fmt.Errorf("retained orchestrator binding does not bind the receipt")
		}
		if plan.Definition.ControlledInputSHA256["oracle-plan"] != replayBytesSHA256(oraclePlanBytes) || plan.Definition.Candidate.Commit != oraclePlan.Candidate.Commit || plan.Definition.Candidate.SHA256 != oraclePlan.Candidate.SHA256 || plan.Definition.Tools.Commit != oraclePlan.Tools.Commit || plan.Definition.Tools.SHA256 != oraclePlan.Tools.SHA256 {
			return fmt.Errorf("controlled oracle plan binding drift")
		}
		return validateOrchestratorReconciliationPacket(packetPath, files, receipt)
	}, &loaded)
	if err == nil && len(loadedOut) > 0 && loadedOut[0] != nil {
		*loadedOut[0] = loaded
	}
	return snapshots, err
}

type reconciliationPacketValidator func(SalesforceReconciliation, map[string]reportInputSnapshot, OraclePlan, []byte) error

func loadSalesforceReconciliationVersion(oraclePlanPath, receiptPath, packetPath string, schemaVersion int, expectedSurfaceIDs func(OraclePlan) ([]string, error), logicalShardCount int, validatePacket reconciliationPacketValidator, loadedOut ...*loadedSalesforceReconciliation) ([]salesforceShardEvidenceSnapshot, error) {
	paths := []string{receiptPath, packetPath}
	if oraclePlanPath != "" {
		paths = append(paths, oraclePlanPath)
	}
	for _, path := range paths {
		if !filepath.IsAbs(path) {
			return nil, fmt.Errorf("absolute Salesforce reconciliation paths are required")
		}
	}
	var receipt SalesforceReconciliation
	var receiptBytes []byte
	var err error
	if schemaVersion == 2 {
		var snapshot reportInputSnapshot
		snapshot, err = readRegularFileSnapshot(receiptPath)
		receiptBytes = snapshot.Data
		if err == nil && snapshot.Mode.Perm() != 0o600 {
			err = fmt.Errorf("v2 Salesforce reconciliation receipt must be mode 0600")
		}
		if err == nil {
			err = decodeExactJSON(receiptBytes, &receipt)
		}
	} else {
		receipt, receiptBytes, err = readExactJSONBytes[SalesforceReconciliation](receiptPath)
	}
	if err != nil || receipt.SchemaVersion != schemaVersion || receipt.Status != "pass" || len(receipt.Shards) == 0 || !sha256Pattern.MatchString(receipt.OraclePlanSHA256) || !sha256Pattern.MatchString(receipt.BundleSHA256) || !sha256Pattern.MatchString(receipt.PacketManifestSHA256) || (schemaVersion == 1 && (receipt.OrchestratorPlanSHA256 != "" || receipt.OrchestratorBindingSHA256 != "")) || (schemaVersion == 2 && (len(receipt.Shards) != 1 || !sha256Pattern.MatchString(receipt.OrchestratorPlanSHA256) || !sha256Pattern.MatchString(receipt.OrchestratorBindingSHA256))) {
		return nil, fmt.Errorf("invalid Salesforce reconciliation receipt")
	}
	if schemaVersion == 2 {
		if err := preflightOrchestratorReconciliationPacket(packetPath); err != nil {
			return nil, err
		}
	}
	files, err := readReconciliationPacket(packetPath, receipt.PacketManifestSHA256)
	if err != nil {
		return nil, err
	}
	if replayBytesSHA256(files["ORACLE_PLAN.json"].Data) != receipt.OraclePlanSHA256 || replayBytesSHA256(files["bundle.json"].Data) != receipt.BundleSHA256 {
		return nil, fmt.Errorf("retained Salesforce reconciliation packet does not bind the receipt")
	}
	var plan OraclePlan
	var planBytes []byte
	if oraclePlanPath == "" {
		plan, planBytes, err = decodeReconciliationJSON[OraclePlan](files["ORACLE_PLAN.json"].Data)
	} else {
		plan, planBytes, err = readExactJSONBytes[OraclePlan](oraclePlanPath)
	}
	if err != nil || replayBytesSHA256(planBytes) != receipt.OraclePlanSHA256 || plan.Candidate != receipt.Candidate || plan.Tools != receipt.Tools {
		return nil, fmt.Errorf("Salesforce reconciliation receipt does not bind the oracle plan")
	}
	bundle, bundleBytes, err := decodeReconciliationJSON[OracleBundle](files["bundle.json"].Data)
	if err != nil || bundle.Candidate != receipt.Candidate || bundle.Tools != receipt.Tools {
		return nil, fmt.Errorf("retained Oracle bundle does not bind the receipt")
	}
	if validatePacket != nil {
		if err := validatePacket(receipt, files, plan, planBytes); err != nil {
			return nil, err
		}
	}
	rows := make([]SalesforceReconciliationRow, 0)
	expectedKinds, err := oracleSalesforceResultKinds(plan)
	if err != nil {
		return nil, err
	}
	expected := sortedMapKeys(expectedKinds)
	if expectedSurfaceIDs != nil {
		expected, err = expectedSurfaceIDs(plan)
		if err != nil {
			return nil, err
		}
	}
	shards := make([]SalesforceShard, 0, len(receipt.Shards))
	snapshots := make([]salesforceShardEvidenceSnapshot, 0, len(receipt.Shards))
	seenIndexes := map[int]bool{}
	for _, retained := range receipt.Shards {
		if retained.ShardIndex < 0 || seenIndexes[retained.ShardIndex] {
			return nil, fmt.Errorf("invalid retained Salesforce shard index")
		}
		seenIndexes[retained.ShardIndex] = true
		prefix := fmt.Sprintf("shards/%02d", retained.ShardIndex)
		shard, shardBytes, err := decodeReconciliationJSON[SalesforceShard](files[prefix+"/SALESFORCE_SHARD.json"].Data)
		if err != nil || replayBytesSHA256(shardBytes) != retained.InputSHA256["shard"] || shard.ShardIndex != retained.ShardIndex || shard.RunID != retained.RunID || shard.OrgAlias != retained.OrgAlias || shard.OrgID != retained.OrgID || shard.ExecutorManifestSHA256 != retained.ExecutorManifestSHA256 {
			return nil, fmt.Errorf("retained Salesforce shard does not bind the receipt")
		}
		inputNames := map[string]string{"dispatch": "SALESFORCE_DISPATCH.json", "preflight": "ORG_PREFLIGHT.json", "creation": "ORG_CREATION.json", "cleanup": "ORG_CLEANUP.json"}
		inputs := map[string]string{"shard": replayBytesSHA256(shardBytes)}
		dispatch, _, dispatchErr := decodeReconciliationJSON[SalesforceDispatch](files[prefix+"/SALESFORCE_DISPATCH.json"].Data)
		preflight, _, preflightErr := decodeReconciliationJSON[SalesforceOrgPreflight](files[prefix+"/ORG_PREFLIGHT.json"].Data)
		creation, _, creationErr := decodeReconciliationJSON[SalesforceOrgCreation](files[prefix+"/ORG_CREATION.json"].Data)
		cleanup, _, cleanupErr := decodeReconciliationJSON[SalesforceOrgCleanup](files[prefix+"/ORG_CLEANUP.json"].Data)
		if dispatchErr != nil || preflightErr != nil || creationErr != nil || cleanupErr != nil || !reflect.DeepEqual(preflight, shard.Preflight) {
			return nil, fmt.Errorf("retained Salesforce lifecycle input is not typed")
		}
		for key, name := range inputNames {
			data := files[prefix+"/"+name].Data
			if data == nil || replayBytesSHA256(data) != retained.InputSHA256[key] {
				return nil, fmt.Errorf("retained Salesforce lifecycle input does not bind the receipt")
			}
			inputs[key] = replayBytesSHA256(data)
		}
		manifest := files[prefix+"/"+salesforceExecutorManifestName]
		if replayBytesSHA256(manifest.Data) != retained.ExecutorManifestSHA256 {
			return nil, fmt.Errorf("retained Salesforce executor manifest does not bind the receipt")
		}
		executorFiles := map[string][]byte{}
		executorSnapshots := map[string]reportInputSnapshot{}
		for name, file := range files {
			prefixPath := prefix + "/executor/"
			if len(name) > len(prefixPath) && name[:len(prefixPath)] == prefixPath {
				relative := name[len(prefixPath):]
				executorFiles[relative] = append([]byte(nil), file.Data...)
				executorSnapshots[relative] = file
			}
		}
		manifestValue, _, manifestErr := decodeReconciliationJSON[salesforceExecutorManifest](manifest.Data)
		entries := make([]salesforceExecutorFile, 0, len(executorSnapshots))
		for relative, file := range executorSnapshots {
			entries = append(entries, salesforceExecutorFile{Path: relative, SHA256: replayBytesSHA256(file.Data)})
		}
		sort.Slice(entries, func(left, right int) bool { return entries[left].Path < entries[right].Path })
		if manifestErr != nil || manifestValue.SchemaVersion != 1 || !reflect.DeepEqual(entries, manifestValue.Files) {
			return nil, fmt.Errorf("retained Salesforce executor manifest does not match its files")
		}
		executor := salesforceExecutorSnapshot{ManifestSHA256: retained.ExecutorManifestSHA256, Manifest: manifest, Files: executorFiles, Snapshots: executorSnapshots}
		shards = append(shards, shard)
		for _, result := range shard.Results {
			rows = append(rows, SalesforceReconciliationRow{SurfaceID: result.SurfaceID, Action: result.Kind, Passed: result.Passed})
		}
		_, _, postflightErr := decodeReconciliationJSON[SalesforceOrgPreflight](executorFiles["postflight.json"])
		if postflightErr != nil {
			return nil, fmt.Errorf("retained Salesforce postflight is not typed")
		}
		snapshots = append(snapshots, salesforceShardEvidenceSnapshot{Shard: shard, Dispatch: dispatch, Creation: creation, Cleanup: cleanup, Inputs: inputs, Executor: executor})
	}
	sort.Slice(rows, func(left, right int) bool {
		if rows[left].SurfaceID != rows[right].SurfaceID {
			return rows[left].SurfaceID < rows[right].SurfaceID
		}
		return rows[left].Action < rows[right].Action
	})
	if !equalReconciliationRows(rows, receipt.Rows) || len(receiptBytes) == 0 {
		return nil, fmt.Errorf("retained Salesforce result rows do not bind the receipt")
	}
	if logicalShardCount == 0 {
		logicalShardCount = len(shards)
	}
	if err := validateSalesforceShards(shards, expected, logicalShardCount); err != nil {
		return nil, fmt.Errorf("retained Salesforce shards do not validate: %w", err)
	}
	for _, shard := range shards {
		for _, result := range shard.Results {
			if result.Kind != expectedKinds[result.SurfaceID] {
				return nil, fmt.Errorf("retained Salesforce result %q has wrong oracle action", result.SurfaceID)
			}
		}
	}
	if len(loadedOut) > 0 && loadedOut[0] != nil {
		*loadedOut[0] = loadedSalesforceReconciliation{
			Receipt: receipt, ReceiptBytes: append([]byte(nil), receiptBytes...),
			OraclePlan: plan, OraclePlanBytes: append([]byte(nil), planBytes...),
			Bundle: bundle, BundleBytes: append([]byte(nil), bundleBytes...), Snapshots: snapshots,
		}
	}
	return snapshots, nil
}

func orchestratorSalesforceExpectedSurfaceIDs(oraclePlan OraclePlan, campaignPlan OrchestratorCampaignPlan, lease OrchestratorLease) ([]string, error) {
	return orchestratorSalesforceExpectedSurfaceIDsAtScope(oraclePlan, campaignPlan, lease, campaignPlan.Definition.ScopePath)
}

func orchestratorSalesforceExpectedSurfaceIDsAtScope(oraclePlan OraclePlan, campaignPlan OrchestratorCampaignPlan, lease OrchestratorLease, scopePath string) ([]string, error) {
	if err := validateOrchestratorWorkerPlanLeaseAtScope(campaignPlan, lease, scopePath); err != nil {
		return nil, err
	}
	kinds, err := oracleSalesforceResultKinds(oraclePlan)
	if err != nil {
		return nil, err
	}
	scope, scopeBytes, err := readExactJSONBytes[SurfaceOracleScope](scopePath)
	if err != nil || replayBytesSHA256(scopeBytes) != campaignPlan.Definition.ScopeSHA256 {
		return nil, fmt.Errorf("orchestrator campaign scope binding drift")
	}
	campaignSurfaces := map[string]bool{}
	for _, job := range campaignPlan.Jobs {
		for _, surfaceID := range job.SurfaceIDs {
			campaignSurfaces[surfaceID] = true
		}
	}
	if scope.Kind == "oracle-plan" {
		if len(campaignSurfaces) != len(kinds) {
			return nil, fmt.Errorf("orchestrator campaign must partition the exact Salesforce-required Oracle projection")
		}
		scopeRows := make(map[string]SurfaceOracleScopeRow, len(scope.Rows))
		for _, row := range scope.Rows {
			scopeRows[row.SurfaceID] = row
		}
		for surfaceID, action := range kinds {
			if !campaignSurfaces[surfaceID] || scopeRows[surfaceID].Action != action {
				return nil, fmt.Errorf("Salesforce-required Oracle surface %q does not match orchestrator campaign", surfaceID)
			}
		}
		for surfaceID := range campaignSurfaces {
			if kinds[surfaceID] == "" {
				return nil, fmt.Errorf("non-Salesforce Oracle surface %q is inside orchestrator campaign", surfaceID)
			}
		}
	} else {
		if len(campaignSurfaces) != len(oraclePlan.Rows) {
			return nil, fmt.Errorf("orchestrator campaign must partition the exact Oracle plan")
		}
		for _, row := range oraclePlan.Rows {
			if !campaignSurfaces[row.SurfaceID] {
				return nil, fmt.Errorf("Oracle surface %q is outside orchestrator campaign", row.SurfaceID)
			}
		}
	}
	expected := make([]string, 0, len(lease.SurfaceIDs))
	for _, surfaceID := range lease.SurfaceIDs {
		if kinds[surfaceID] != "" {
			expected = append(expected, surfaceID)
		}
	}
	if len(expected) == 0 {
		return nil, fmt.Errorf("orchestrator shard has no Salesforce-required surfaces")
	}
	return expected, nil
}

func expectedOrchestratorBatchBinding(plan OrchestratorCampaignPlan, lease OrchestratorLease) (OrchestratorBatchBinding, error) {
	return expectedOrchestratorBatchBindingAtScope(plan, lease, plan.Definition.ScopePath)
}

func expectedOrchestratorBatchBindingAtScope(plan OrchestratorCampaignPlan, lease OrchestratorLease, scopePath string) (OrchestratorBatchBinding, error) {
	if err := validateOrchestratorWorkerPlanLeaseAtScope(plan, lease, scopePath); err != nil {
		return OrchestratorBatchBinding{}, err
	}
	return OrchestratorBatchBinding{
		SchemaVersion: 1, CampaignID: plan.CampaignID, SpecSHA256: plan.SpecSHA256,
		Candidate: plan.Definition.Candidate, Tools: plan.Definition.Tools, ScopeSHA256: plan.Definition.ScopeSHA256,
		ControlledInputSHA256: plan.Definition.ControlledInputSHA256, JobID: lease.JobID, JobKind: lease.Kind,
		Generation: lease.Generation, ShardIndex: lease.ShardIndex, SurfaceIDs: lease.SurfaceIDs,
	}, nil
}

func validateOrchestratorReconciliationPacket(packetPath string, files map[string]reportInputSnapshot, receipt SalesforceReconciliation) error {
	if len(receipt.Shards) != 1 || len(receipt.Shards[0].InputSHA256) != 5 {
		return fmt.Errorf("orchestrator packet must retain exactly one typed Salesforce shard")
	}
	prefix := fmt.Sprintf("shards/%02d", receipt.Shards[0].ShardIndex)
	required := map[string]bool{
		"ORACLE_PLAN.json": true, "bundle.json": true, "ORCHESTRATOR_BINDING.json": true,
		prefix + "/SALESFORCE_SHARD.json": true, prefix + "/SALESFORCE_DISPATCH.json": true,
		prefix + "/ORG_PREFLIGHT.json": true, prefix + "/ORG_CREATION.json": true,
		prefix + "/ORG_CLEANUP.json": true, prefix + "/" + salesforceExecutorManifestName: true,
	}
	for name := range files {
		if required[name] || strings.HasPrefix(name, prefix+"/executor/") {
			delete(required, name)
			continue
		}
		return fmt.Errorf("orchestrator packet contains unexpected evidence %q", name)
	}
	if len(required) != 0 {
		return fmt.Errorf("orchestrator packet is missing required evidence")
	}
	wantFiles := map[string]bool{reconciliationPacketManifestName: true}
	wantDirectories := map[string]bool{".": true}
	for name := range files {
		wantFiles[name] = true
		for directory := filepath.ToSlash(filepath.Dir(name)); directory != "."; directory = filepath.ToSlash(filepath.Dir(directory)) {
			wantDirectories[directory] = true
		}
	}
	seenFiles, seenDirectories := map[string]bool{}, map[string]bool{}
	err := filepath.WalkDir(packetPath, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(packetPath, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("orchestrator packet contains symlink component")
		}
		if entry.IsDir() {
			if !wantDirectories[relative] {
				return fmt.Errorf("orchestrator packet contains unexpected directory")
			}
			seenDirectories[relative] = true
			return nil
		}
		if !entry.Type().IsRegular() || !wantFiles[relative] {
			return fmt.Errorf("orchestrator packet contains unexpected file")
		}
		seenFiles[relative] = true
		return nil
	})
	if err != nil {
		return fmt.Errorf("invalid orchestrator reconciliation packet: %w", err)
	}
	if len(seenFiles) != len(wantFiles) || len(seenDirectories) != len(wantDirectories) {
		return fmt.Errorf("invalid orchestrator reconciliation packet")
	}
	return nil
}

func preflightOrchestratorReconciliationPacket(packetPath string) error {
	err := filepath.WalkDir(packetPath, func(_ string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("orchestrator packet contains symlink component")
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if info.Mode().Perm() != 0o700 {
				return fmt.Errorf("orchestrator packet directory must be mode 0700")
			}
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("orchestrator packet contains non-regular component")
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("invalid orchestrator reconciliation packet: %w", err)
	}
	return nil
}

func sortedMapKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func equalReconciliationRows(left, right []SalesforceReconciliationRow) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func writeReconciliationPacket(output string, files []reconciliationPacketFile) (string, error) {
	parent := filepath.Dir(output)
	temp, err := os.MkdirTemp(parent, ".salesforce-reconciliation-packet-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(temp)
	if err := os.Chmod(temp, 0o700); err != nil {
		return "", err
	}
	manifest := reportPacketManifest{SchemaVersion: 1, Files: make([]reportPacketManifestFile, 0, len(files))}
	seen := map[string]bool{}
	for _, file := range files {
		if file.Name == "" || file.Name == reconciliationPacketManifestName || filepath.IsAbs(file.Name) || filepath.ToSlash(filepath.Clean(file.Name)) != file.Name || seen[file.Name] || !file.Mode.IsRegular() {
			return "", fmt.Errorf("invalid retained Salesforce packet file %q", file.Name)
		}
		path, pathErr := rootedPath(temp, filepath.FromSlash(file.Name))
		if pathErr != nil {
			return "", fmt.Errorf("invalid retained Salesforce packet file %q", file.Name)
		}
		seen[file.Name] = true
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return "", err
		}
		if err := os.WriteFile(path, file.Data, file.Mode.Perm()); err != nil {
			return "", err
		}
		if err := os.Chmod(path, file.Mode.Perm()); err != nil {
			return "", err
		}
		manifest.Files = append(manifest.Files, reportPacketManifestFile{Name: file.Name, SHA256: replayBytesSHA256(file.Data), Mode: file.Mode.Perm()})
	}
	sort.Slice(manifest.Files, func(left, right int) bool { return manifest.Files[left].Name < manifest.Files[right].Name })
	manifestPath := filepath.Join(temp, reconciliationPacketManifestName)
	if err := WriteNewJSON(manifestPath, manifest); err != nil {
		return "", err
	}
	manifestSHA, err := sha256File(manifestPath)
	if err != nil {
		return "", err
	}
	if err := os.Rename(temp, output); err != nil {
		return "", err
	}
	return manifestSHA, nil
}

func readReconciliationPacket(packetPath, expectedManifestSHA string) (map[string]reportInputSnapshot, error) {
	manifestPath := filepath.Join(packetPath, reconciliationPacketManifestName)
	manifestSnapshot, err := readRegularFileSnapshot(manifestPath)
	var manifest reportPacketManifest
	if err == nil && manifestSnapshot.Mode.Perm() == 0o600 {
		err = decodeExactJSON(manifestSnapshot.Data, &manifest)
	}
	if err != nil || manifestSnapshot.Mode.Perm() != 0o600 || manifest.SchemaVersion != 1 || replayBytesSHA256(manifestSnapshot.Data) != expectedManifestSHA {
		return nil, fmt.Errorf("invalid retained Salesforce packet manifest")
	}
	files := map[string]reportInputSnapshot{}
	for _, entry := range manifest.Files {
		if entry.Name == "" || filepath.IsAbs(entry.Name) || filepath.ToSlash(filepath.Clean(entry.Name)) != entry.Name || files[entry.Name].Data != nil {
			return nil, fmt.Errorf("invalid retained Salesforce packet manifest entry")
		}
		entryPath, pathErr := rootedPath(packetPath, filepath.FromSlash(entry.Name))
		if pathErr != nil {
			return nil, fmt.Errorf("invalid retained Salesforce packet manifest entry")
		}
		snapshot, err := readRegularFileSnapshot(entryPath)
		if err != nil || snapshot.Mode.Perm() != entry.Mode.Perm() || replayBytesSHA256(snapshot.Data) != entry.SHA256 {
			return nil, fmt.Errorf("retained Salesforce packet changed")
		}
		files[entry.Name] = snapshot
	}
	return files, nil
}

func decodeReconciliationJSON[T any](data []byte) (T, []byte, error) {
	var value T
	if err := decodeExactJSON(data, &value); err != nil {
		return value, data, err
	}
	return value, data, nil
}

func requireNewPath(path string) error {
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("output already exists: %s", path)
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}
