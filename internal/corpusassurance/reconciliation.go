package corpusassurance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
)

type SalesforceReconciliationRequest struct {
	OraclePlanPath string
	ShardFiles     []SalesforceShardFiles
	PacketOutput   string
	OutputPath     string
}

type SalesforceReconciliation struct {
	SchemaVersion        int                             `json:"schemaVersion"`
	Status               string                          `json:"status"`
	Candidate            RuntimeArtifact                 `json:"candidate"`
	Tools                RuntimeArtifact                 `json:"tools"`
	OraclePlanSHA256     string                          `json:"oraclePlanSha256"`
	BundleSHA256         string                          `json:"bundleSha256"`
	Rows                 []SalesforceReconciliationRow   `json:"rows"`
	Shards               []SalesforceReconciliationShard `json:"shards"`
	PacketManifestSHA256 string                          `json:"packetManifestSha256"`
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

func CreateSalesforceReconciliation(request SalesforceReconciliationRequest) (SalesforceReconciliation, error) {
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
	if err := validateSalesforceShardFiles(request.OraclePlanPath, request.ShardFiles, &snapshots); err != nil {
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
	packetFiles := []reconciliationPacketFile{}
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
	receipt := SalesforceReconciliation{SchemaVersion: 1, Status: "pass", Candidate: plan.Candidate, Tools: plan.Tools, OraclePlanSHA256: replayBytesSHA256(planBytes), BundleSHA256: replayBytesSHA256(bundleBytes), Shards: make([]SalesforceReconciliationShard, 0, len(snapshots))}
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
	for _, path := range []string{oraclePlanPath, receiptPath, packetPath} {
		if !filepath.IsAbs(path) {
			return nil, fmt.Errorf("absolute Salesforce reconciliation paths are required")
		}
	}
	receipt, receiptBytes, err := readExactJSONBytes[SalesforceReconciliation](receiptPath)
	if err != nil || receipt.SchemaVersion != 1 || receipt.Status != "pass" || len(receipt.Shards) == 0 || !sha256Pattern.MatchString(receipt.OraclePlanSHA256) || !sha256Pattern.MatchString(receipt.BundleSHA256) || !sha256Pattern.MatchString(receipt.PacketManifestSHA256) {
		return nil, fmt.Errorf("invalid Salesforce reconciliation receipt")
	}
	plan, planBytes, err := readExactJSONBytes[OraclePlan](oraclePlanPath)
	if err != nil || replayBytesSHA256(planBytes) != receipt.OraclePlanSHA256 || plan.Candidate != receipt.Candidate || plan.Tools != receipt.Tools {
		return nil, fmt.Errorf("Salesforce reconciliation receipt does not bind the oracle plan")
	}
	files, err := readReconciliationPacket(packetPath, receipt.PacketManifestSHA256)
	if err != nil {
		return nil, err
	}
	if replayBytesSHA256(files["ORACLE_PLAN.json"].Data) != receipt.OraclePlanSHA256 || replayBytesSHA256(files["bundle.json"].Data) != receipt.BundleSHA256 {
		return nil, fmt.Errorf("retained Salesforce reconciliation packet does not bind the receipt")
	}
	bundle, _, err := decodeReconciliationJSON[OracleBundle](files["bundle.json"].Data)
	if err != nil || bundle.Candidate != receipt.Candidate || bundle.Tools != receipt.Tools {
		return nil, fmt.Errorf("retained Oracle bundle does not bind the receipt")
	}
	rows := make([]SalesforceReconciliationRow, 0)
	expectedKinds, err := oracleSalesforceResultKinds(plan)
	if err != nil {
		return nil, err
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
		snapshots = append(snapshots, salesforceShardEvidenceSnapshot{Shard: shard, Inputs: inputs, Executor: executor})
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
	if err := ValidateSalesforceShards(shards, sortedMapKeys(expectedKinds)); err != nil {
		return nil, fmt.Errorf("retained Salesforce shards do not validate: %w", err)
	}
	return snapshots, nil
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
	manifest, manifestBytes, err := readExactJSONBytes[reportPacketManifest](manifestPath)
	if err != nil || manifest.SchemaVersion != 1 || replayBytesSHA256(manifestBytes) != expectedManifestSHA {
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
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return value, data, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return value, data, fmt.Errorf("multiple JSON values")
		}
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
