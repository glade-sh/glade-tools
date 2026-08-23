package corpusassurance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateSurfaceOracleIndex(t *testing.T) {
	root := t.TempDir()
	scopePath, batchRoot := writeSurfaceOracleIndexInputs(t, root)
	output := filepath.Join(root, "SURFACE_ORACLE_INDEX.json")
	index, err := CreateSurfaceOracleIndex(SurfaceOracleIndexRequest{ScopePath: scopePath, RuntimeBatchRoots: []string{batchRoot}, OutputPath: output})
	if err != nil {
		t.Fatalf("CreateSurfaceOracleIndex: %v", err)
	}
	if index.Total != 3 || index.Counts.Open != 1 || index.Counts.Matched != 2 {
		t.Fatalf("counts = %#v, total = %d", index.Counts, index.Total)
	}
	want := []SurfaceOracleIndexRow{
		{SurfaceID: "apex:System.One", State: "matched"},
		{SurfaceID: "apex:System.Three", State: "open"},
		{SurfaceID: "apex:System.Two", State: "matched"},
	}
	for i := range want {
		if index.Rows[i] != want[i] {
			t.Fatalf("row %d = %#v, want %#v", i, index.Rows[i], want[i])
		}
	}
	if index.ScopeSHA256 != surfaceOracleFileSHA256(t, scopePath) || len(index.RuntimeBatches) != 1 || index.RuntimeBatches[0].ManifestSHA256 != surfaceOracleFileSHA256(t, filepath.Join(batchRoot, "inputs", "RUNTIME_BATCH_MANIFEST.json")) {
		t.Fatalf("index bindings = %#v", index)
	}
	if strings.Join(index.RuntimeBatches[0].SurfaceIDs, ",") != "apex:System.One,apex:System.Two" {
		t.Fatalf("runtime batch surface IDs are not sorted: %#v", index.RuntimeBatches[0].SurfaceIDs)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var exact map[string]any
	if err := json.Unmarshal(data, &exact); err != nil {
		t.Fatal(err)
	}
	row := exact["rows"].([]any)[0].(map[string]any)
	if exact["kind"] != "all-runtime" || len(row) != 2 || row["state"] != "matched" || exact["candidate"].(map[string]any)["binarySha256"] == nil || exact["tools"].(map[string]any)["binarySha256"] == nil {
		t.Fatalf("public index shape = %#v", exact)
	}
	if _, err := CreateSurfaceOracleIndex(SurfaceOracleIndexRequest{ScopePath: scopePath, RuntimeBatchRoots: []string{batchRoot}, OutputPath: output}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("create-only error = %v", err)
	}
}

func TestCreateSurfaceOracleIndexRejectsUnsealedEvidence(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, string, string)
		want   string
	}{
		{name: "forged audit", want: "final audit", mutate: func(t *testing.T, _, batch string) {
			updateJSONMap(t, filepath.Join(batch, "evidence", "FINAL_AUDIT.json"), func(value map[string]any) { value["passed"] = false })
		}},
		{name: "hash drift", want: "local summary", mutate: func(t *testing.T, _, batch string) {
			updateJSONMap(t, filepath.Join(batch, "local-proof", "LOCAL_RUNTIME_SUMMARY.json"), func(value map[string]any) { value["selectedRows"] = float64(1) })
		}},
		{name: "symlink", want: "regular file", mutate: func(t *testing.T, root, batch string) {
			path := filepath.Join(batch, "evidence", "RECONCILIATION.json")
			copyPath := filepath.Join(root, "reconciliation-copy.json")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(copyPath, data, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(copyPath, path); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "count", want: "manifest", mutate: func(t *testing.T, _, batch string) {
			updateJSONMap(t, filepath.Join(batch, "inputs", "RUNTIME_BATCH_MANIFEST.json"), func(value map[string]any) { value["surfaceRowCount"] = float64(1) })
			resealSurfaceOracleBatch(t, batch)
		}},
		{name: "row set", want: "row set", mutate: func(t *testing.T, _, batch string) {
			updateJSONMap(t, filepath.Join(batch, "evidence", "MISMATCH_REVIEW.json"), func(value map[string]any) {
				value["rows"].([]any)[0].(map[string]any)["surfaceId"] = "apex:System.Forged"
			})
			resealSurfaceOracleBatch(t, batch)
		}},
		{name: "fixture traversal", want: "escapes root", mutate: func(t *testing.T, _, batch string) {
			outside := filepath.Join(batch, "source", "outside.json")
			writeJSONValue(t, outside, map[string]any{"fixture": "synthetic"})
			updateJSONMap(t, filepath.Join(batch, "inputs", "RUNTIME_BATCH_MANIFEST.json"), func(value map[string]any) {
				fixture := value["fixtures"].([]any)[0].(map[string]any)
				fixture["fixture"], fixture["path"], fixture["sha256"] = "outside", "../outside.json", surfaceOracleFileSHA256(t, outside)
			})
			resealSurfaceOracleBatch(t, batch)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			scope, batch := writeSurfaceOracleIndexInputs(t, root)
			test.mutate(t, root, batch)
			_, err := CreateSurfaceOracleIndex(SurfaceOracleIndexRequest{ScopePath: scope, RuntimeBatchRoots: []string{batch}, OutputPath: filepath.Join(root, "index.json")})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestCreateSurfaceOracleIndexAccumulatesOnlyDisjointIdenticalCandidateBatches(t *testing.T) {
	root := t.TempDir()
	scope, _ := writeSurfaceOracleIndexInputs(t, root)
	first := writeSurfaceOracleBatch(t, root, "first", []string{"apex:System.One"})
	second := writeSurfaceOracleBatch(t, root, "second", []string{"apex:System.Two"})
	index, err := CreateSurfaceOracleIndex(SurfaceOracleIndexRequest{ScopePath: scope, RuntimeBatchRoots: []string{second, first}, OutputPath: filepath.Join(root, "index.json")})
	if err != nil {
		t.Fatal(err)
	}
	if index.Counts.Matched != 2 || len(index.RuntimeBatches) != 2 || index.RuntimeBatches[0].ManifestSHA256 >= index.RuntimeBatches[1].ManifestSHA256 {
		t.Fatalf("cumulative index = %#v", index)
	}
	if _, err := CreateSurfaceOracleIndex(SurfaceOracleIndexRequest{ScopePath: scope, RuntimeBatchRoots: []string{first, first}, OutputPath: filepath.Join(root, "duplicate.json")}); err == nil || !strings.Contains(err.Error(), "more than one runtime batch") {
		t.Fatalf("duplicate credit error = %v", err)
	}
	writeFile(t, filepath.Join(second, "bin", "glade-sealed"), "different candidate")
	resealSurfaceOracleBatch(t, second)
	if _, err := CreateSurfaceOracleIndex(SurfaceOracleIndexRequest{ScopePath: scope, RuntimeBatchRoots: []string{first, second}, OutputPath: filepath.Join(root, "candidate.json")}); err == nil || !strings.Contains(err.Error(), "candidate or tools") {
		t.Fatalf("changed candidate error = %v", err)
	}
}

func TestValidateSurfaceOracleIndexRejectsForgedMatchedRow(t *testing.T) {
	root := t.TempDir()
	scope, batch := writeSurfaceOracleIndexInputs(t, root)
	if _, err := CreateSurfaceOracleIndex(SurfaceOracleIndexRequest{ScopePath: scope, OutputPath: filepath.Join(root, "empty.json")}); err == nil || !strings.Contains(err.Error(), "at least one") {
		t.Fatalf("empty batch set error = %v", err)
	}
	index, err := CreateSurfaceOracleIndex(SurfaceOracleIndexRequest{ScopePath: scope, RuntimeBatchRoots: []string{batch}, OutputPath: filepath.Join(root, "index.json")})
	if err != nil {
		t.Fatal(err)
	}
	index.Rows[1].State = "matched"
	index.Counts = surfaceOracleIndexCounts(index.Rows)
	if err := ValidateSurfaceOracleIndex(index); err == nil || !strings.Contains(err.Error(), "matched row set") {
		t.Fatalf("forged matched row error = %v", err)
	}
	index.Rows[1].State = "inconclusive"
	index.Counts = surfaceOracleIndexCounts(index.Rows)
	if err := ValidateSurfaceOracleIndex(index); err == nil || !strings.Contains(err.Error(), "invalid or unsorted") {
		t.Fatalf("speculative state error = %v", err)
	}
}

func writeSurfaceOracleIndexInputs(t *testing.T, root string) (string, string) {
	t.Helper()
	hashA, hashB, hashC := strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)
	scope := SurfaceOracleScope{SchemaVersion: 1, Kind: "all-runtime", SourceProfileSHA256: hashA, LedgerSHA256: hashB, PolicySHA256: hashC, Total: 3, ByDisposition: map[string]int{deterministicMockRequired: 1, localRuntimeRequired: 2}, Rows: []SurfaceOracleScopeRow{{SurfaceID: "apex:System.One", Disposition: deterministicMockRequired}, {SurfaceID: "apex:System.Three", Disposition: localRuntimeRequired}, {SurfaceID: "apex:System.Two", Disposition: localRuntimeRequired}}}
	scopePath := filepath.Join(root, "scope.json")
	writeJSONValue(t, scopePath, scope)
	return scopePath, writeSurfaceOracleBatch(t, root, "batch", []string{"apex:System.Two", "apex:System.One"})
}

func writeSurfaceOracleBatch(t *testing.T, root, name string, ids []string) string {
	t.Helper()
	batch := filepath.Join(root, name)
	if err := os.MkdirAll(batch, 0o700); err != nil {
		t.Fatal(err)
	}
	idsJSON, rawRows, reviewRows := make([]any, len(ids)), make([]any, len(ids)), make([]any, len(ids))
	for i, id := range ids {
		idsJSON[i], rawRows[i], reviewRows[i] = id, surfaceOracleRawRow(id), surfaceOracleReviewRow(id)
	}
	profilePath := filepath.Join(batch, "inputs", "RUNTIME_BATCH_PROFILE.json")
	writeJSONValue(t, profilePath, map[string]any{"profile": "synthetic"})
	fixturePath := filepath.Join(batch, "source", "glade-tools", "docs", "fixtures", "fixture-one.json")
	writeJSONValue(t, fixturePath, map[string]any{"fixture": "synthetic", "command": map[string]any{"kind": "exec"}})
	writeJSONValue(t, filepath.Join(batch, "inputs", "RUNTIME_BATCH_MANIFEST.json"), map[string]any{
		"schemaVersion": 1, "selectionPolicy": "whole, disjoint, inline anonymous-runtime fixtures with assertion-bearing programs", "surfaceRowCount": len(ids),
		"fixtures": []any{map[string]any{"fixture": "fixture-one", "path": "docs/fixtures/fixture-one.json", "rowCount": len(ids), "salesforceEligible": true, "sha256": surfaceOracleFileSHA256(t, fixturePath), "sourceFiles": []any{}, "surfaceIds": idsJSON}},
	})
	if err := os.Chmod(filepath.Join(batch, "inputs", "RUNTIME_BATCH_MANIFEST.json"), 0o400); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(batch, "bin", "glade-sealed"), "candidate")
	writeFile(t, filepath.Join(batch, "bin", "glade-tools"), "tools")
	writeFile(t, filepath.Join(batch, "source", "glade-tools", "scripts", "corpus-assurance", "salesforce-first-filter.py"), "# synthetic\n")
	writeJSONValue(t, filepath.Join(batch, "evidence", "RUN_SUMMARY.json"), map[string]any{})
	writeJSONValue(t, filepath.Join(batch, "evidence", "ORG_CLEANUP.json"), map[string]any{})
	writeJSONValue(t, filepath.Join(batch, "local-proof", "LOCAL_RUNTIME_SUMMARY.json"), map[string]any{
		"schemaVersion": 1, "sealed": true, "candidateSha256": "", "manifestSha256": "", "selectedFixtures": 1, "selectedRows": len(ids),
		"startedAtUnixNs": 1, "endedAtUnixNs": 2, "durationMs": 1.0,
		"results": []any{map[string]any{"candidateExitCode": 0, "candidateStatus": "passed", "durationMs": 1.0, "endedAtUnixNs": 2, "exitCode": 0, "fixture": "fixture-one.json", "kind": "exec", "path": "docs/fixtures/fixture-one.json", "result": map[string]any{}, "startedAtUnixNs": 1, "status": "exit-0", "stderrSha256": strings.Repeat("e", 64), "stdoutSha256": strings.Repeat("f", 64)}},
	})
	writeJSONValue(t, filepath.Join(batch, "evidence", "RECONCILIATION.json"), map[string]any{
		"schemaVersion": 1, "sealed": true, "manifestSha256": "", "runtimeRequested": true, "orgPostflightMatched": true, "runnerError": nil,
		"counts": map[string]any{"environment": 0, "match": len(ids), "mismatch": 0},
		"rows":   rawRows,
	})
	writeJSONValue(t, filepath.Join(batch, "evidence", "MISMATCH_REVIEW.json"), map[string]any{
		"schemaVersion": 1, "sealed": true, "manifestSha256": "", "oracleResultsSha256": "", "rawReconciliationSha256": "",
		"rawClassifications": map[string]any{"environment": 0, "match": len(ids), "mismatch": 0}, "reviewCounts": map[string]any{"confirmedMatch": len(ids)},
		"groups": []any{map[string]any{"confirmedMatchRows": len(ids), "fixture": "fixture-one.json"}},
		"rows":   reviewRows,
	})
	writeJSONValue(t, filepath.Join(batch, "oracle", "results.json"), surfaceOracleResultsValue(strings.Repeat("1", 40), strings.Repeat("2", 40), idsJSON))
	writeJSONValue(t, filepath.Join(batch, "evidence", "BINDINGS.json"), map[string]any{})
	writeJSONValue(t, filepath.Join(batch, "evidence", "FINAL_AUDIT.json"), surfaceOracleFinalAudit())
	resealSurfaceOracleBatch(t, batch)
	return batch
}

func surfaceOracleRawRow(id string) map[string]any {
	return map[string]any{"classification": "match", "fixture": "fixture-one.json", "local": map[string]any{"candidateExitCode": 0, "candidateStatus": "passed", "status": "exit-0"}, "reason": "local-and-salesforce-runtime-passed", "salesforce": map[string]any{"componentFailures": []any{}, "deployable": true, "exitCode": 0, "runtimeExitCode": nil, "runtimePassed": true, "runtimeRequested": true, "runtimeStatus": "Passed", "status": "Succeeded"}, "surfaceId": id}
}

func surfaceOracleReviewRow(id string) map[string]any {
	return map[string]any{"fixture": "fixture-one.json", "reviewDisposition": "confirmed-match", "sealedClassification": "match", "surfaceId": id}
}

func surfaceOracleResultsValue(candidateCommit, toolsCommit string, ids []any) map[string]any {
	result := map[string]any{"manifestIndex": 0, "fixture": "fixture-one.json", "fixtureSha256": strings.Repeat("d", 64), "manifestSha256": "", "candidateCommit": candidateCommit, "candidateSha256": "", "toolsCommit": toolsCommit, "workflowScriptSha256": "", "status": "Succeeded", "deployable": true, "exitCode": 0, "runtimeRequested": true, "runtimePassed": true, "runtimeExitCode": nil, "runtimeStatus": "Passed", "surfaceIds": ids, "classNameMap": map[string]any{}, "componentFailures": []any{}, "componentSuccesses": []any{}, "coverage": map[string]any{}, "execution": map[string]any{}, "invocation": map[string]any{}, "kind": "exec", "org": map[string]any{}, "orgCleanup": map[string]any{}, "orgIdentity": map[string]any{}, "project": map[string]any{}, "projectManifest": map[string]any{}, "runtimeResult": map[string]any{}, "sourceFiles": []any{}, "testClasses": []any{}}
	return map[string]any{"schemaVersion": 1, "sealed": true, "binding": map[string]any{"manifestSha256": "", "profileSha256": "", "queueSha256": nil, "selectorSha256": nil, "selectorReceiptSha256": nil, "selectionSha256": strings.Repeat("3", 64), "candidateCommit": candidateCommit, "candidateSha256": "", "toolsCommit": toolsCommit, "toolsAmd64Sha256": "", "workflowScriptSha256": "", "orgPreflightSha256": strings.Repeat("4", 64), "localSummarySha256": ""}, "selectedFixtures": 1, "excludedFixtures": 0, "selectedRows": len(ids), "excludedRows": 0, "runtimeRequested": true, "orgIdentities": map[string]any{}, "workerExecution": map[string]any{}, "orgPreflightSha256": strings.Repeat("4", 64), "localSummarySha256": "", "selectionSha256": strings.Repeat("3", 64), "selectedManifestIndexes": []any{0}, "orgPostflight": map[string]any{}, "skippedDeferredFixtures": []any{}, "orgs": []any{}, "results": []any{result}}
}

func surfaceOracleFinalAudit() map[string]any {
	checks := map[string]any{}
	for _, name := range []string{"candidateHashMatched", "cleanupReceiptPassed", "credentialScanClean", "finalActiveRecordResidueZero", "finalOrgDisplayRejected", "finalQuotaMatchedReceipt", "localRowsAllPassed", "manifestHashMatched", "manifestMode0400", "manifestRowsBoundedUnique", "oraclePostflightMatched", "oracleSealedRuntime", "privateRootMode0700", "reconciliationSealed", "runtimeReviewSealed", "sourceHeadsCleanExact", "toolsHashMatched", "workflowHashMatched"} {
		checks[name] = true
	}
	return map[string]any{"schemaVersion": 1, "passed": true, "artifactHashes": map[string]any{}, "checks": checks, "finalQuota": map[string]any{"activeScratchOrgs": map[string]any{"max": 3, "remaining": 3}, "dailyScratchOrgs": map[string]any{"max": 6, "remaining": 2}}, "finalResidueCount": 0, "privacyScan": map[string]any{"credentialJsonKeyHits": 0, "credentialPatternHits": 0, "scannedFiles": 1}, "sourceChecks": map[string]any{"glade": map[string]any{"clean": true, "headMatched": true}, "glade-tools": map[string]any{"clean": true, "headMatched": true}}}
}

func resealSurfaceOracleBatch(t *testing.T, batch string) {
	t.Helper()
	manifest, profile := filepath.Join(batch, "inputs", "RUNTIME_BATCH_MANIFEST.json"), filepath.Join(batch, "inputs", "RUNTIME_BATCH_PROFILE.json")
	local, oracle := filepath.Join(batch, "local-proof", "LOCAL_RUNTIME_SUMMARY.json"), filepath.Join(batch, "oracle", "results.json")
	reconciliation, review := filepath.Join(batch, "evidence", "RECONCILIATION.json"), filepath.Join(batch, "evidence", "MISMATCH_REVIEW.json")
	candidate, tools, workflow := filepath.Join(batch, "bin", "glade-sealed"), filepath.Join(batch, "bin", "glade-tools"), filepath.Join(batch, "source", "glade-tools", "scripts", "corpus-assurance", "salesforce-first-filter.py")
	manifestHash, candidateHash, toolsHash, workflowHash := surfaceOracleFileSHA256(t, manifest), surfaceOracleFileSHA256(t, candidate), surfaceOracleFileSHA256(t, tools), surfaceOracleFileSHA256(t, workflow)
	updateJSONMap(t, local, func(v map[string]any) { v["manifestSha256"], v["candidateSha256"] = manifestHash, candidateHash })
	localHash := surfaceOracleFileSHA256(t, local)
	updateJSONMap(t, oracle, func(v map[string]any) {
		v["localSummarySha256"] = localHash
		b := v["binding"].(map[string]any)
		b["manifestSha256"], b["profileSha256"], b["candidateSha256"], b["toolsAmd64Sha256"], b["workflowScriptSha256"], b["localSummarySha256"] = manifestHash, surfaceOracleFileSHA256(t, profile), candidateHash, toolsHash, workflowHash, localHash
		fixtureSHA := readJSONMap(t, manifest)["fixtures"].([]any)[0].(map[string]any)["sha256"]
		for _, item := range v["results"].([]any) {
			r := item.(map[string]any)
			r["manifestSha256"], r["candidateSha256"], r["workflowScriptSha256"] = manifestHash, candidateHash, workflowHash
			r["fixtureSha256"] = fixtureSHA
		}
	})
	updateJSONMap(t, reconciliation, func(v map[string]any) { v["manifestSha256"] = manifestHash })
	updateJSONMap(t, review, func(v map[string]any) {
		v["manifestSha256"], v["oracleResultsSha256"], v["rawReconciliationSha256"] = manifestHash, surfaceOracleFileSHA256(t, oracle), surfaceOracleFileSHA256(t, reconciliation)
	})
	bindings := map[string]any{"candidateCommit": strings.Repeat("1", 40), "candidateSha256": candidateHash, "localSummarySha256": localHash, "manifestSha256": manifestHash, "profileSha256": surfaceOracleFileSHA256(t, profile), "scratchDefinitionSha256": strings.Repeat("5", 64), "sfCliSha256": strings.Repeat("6", 64), "toolsCommit": strings.Repeat("2", 40), "toolsSha256": toolsHash, "workflowScriptSha256": workflowHash}
	writeJSONValue(t, filepath.Join(batch, "evidence", "BINDINGS.json"), bindings)
	auditPath := filepath.Join(batch, "evidence", "FINAL_AUDIT.json")
	updateJSONMap(t, auditPath, func(v map[string]any) {
		v["artifactHashes"] = map[string]any{"bindings": surfaceOracleFileSHA256(t, filepath.Join(batch, "evidence", "BINDINGS.json")), "cleanup": surfaceOracleFileSHA256(t, filepath.Join(batch, "evidence", "ORG_CLEANUP.json")), "localSummary": localHash, "manifest": manifestHash, "mismatchReview": surfaceOracleFileSHA256(t, review), "oracleResults": surfaceOracleFileSHA256(t, oracle), "reconciliation": surfaceOracleFileSHA256(t, reconciliation), "runSummary": surfaceOracleFileSHA256(t, filepath.Join(batch, "evidence", "RUN_SUMMARY.json"))}
	})
}

func updateJSONMap(t *testing.T, path string, update func(map[string]any)) {
	t.Helper()
	value := readJSONMap(t, path)
	update(value)
	mode := os.FileMode(0o600)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	writeJSONValue(t, path, value)
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func readJSONMap(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func writeJSONValue(t *testing.T, path string, value any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, value string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0o700); err != nil {
		t.Fatal(err)
	}
}

func surfaceOracleFileSHA256(t *testing.T, path string) string {
	t.Helper()
	hash, err := sha256FileDirect(path)
	if err != nil {
		t.Fatal(err)
	}
	return hash
}
