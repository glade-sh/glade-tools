package corpusassurance

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func TestLocalProofDerivesBindingsRunsFixedCommandsAndNormalizesEverySelectedSurface(t *testing.T) {
	request, calls := localProofRequest(t)
	proof, err := RunLocalProof(request)
	if err != nil {
		t.Fatalf("RunLocalProof: %v", err)
	}
	if proof.Status != "pass" {
		t.Fatalf("status = %q", proof.Status)
	}
	if got, want := localProofCommandShapes(*calls), [][]string{
		{"test", "--project", ".", "--json", "--no-progress"},
		{"exec", "--project", ".", "--json", "new Runtime().run(); new Runtime().extra();"},
		{"check", "--project", ".", "--json", "--no-progress"},
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("commands = %v, want %v", got, want)
	}
	if got, want := surfaceIDs(proof.Surfaces), []string{"apex:Mock.run", "apex:Runtime.extra", "apex:Runtime.run", "apex:Shape.run"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("proof surface IDs = %v, want %v", got, want)
	}
	decision := readLocalProofDecision(t, request.DecisionPath)
	if proof.Candidate != request.Candidate || proof.Tools != request.Tools || proof.FixtureManifestSHA256 != decision.FixtureManifestSHA256 {
		t.Fatalf("proof bindings = %#v", proof)
	}
	if !proof.Surfaces[0].BehaviorObserved || !proof.Surfaces[1].RuntimeObserved || !proof.Surfaces[3].CompilePassed {
		t.Fatalf("receipt-derived observations = %#v", proof.Surfaces)
	}
	if _, err := os.Stat(request.OutputPath); err != nil {
		t.Fatalf("proof output was not written: %v", err)
	}
}

func TestLocalProofUsesCandidateCLIAndValidatesJSONResult(t *testing.T) {
	request, _ := localProofRequest(t)
	request.executor = nil
	if err := os.WriteFile(request.CandidatePath, []byte("#!/bin/sh\nfor arg in \"$@\"; do [ \"$arg\" != --fixture ] || exit 17; done\ncase \"$1\" in\ntest) printf '{\"status\":\"passed\",\"exitCode\":0,\"summary\":{\"total\":1,\"passed\":1,\"failed\":0,\"errors\":0,\"compileErrors\":0,\"runtimeErrors\":0},\"tests\":[{}]}' ;;\ncheck) printf '{\"status\":\"passed\",\"exitCode\":0,\"summary\":{\"types\":1,\"triggers\":0}}' ;;\n*) printf '{\"status\":\"passed\",\"exitCode\":0}' ;;\nesac\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	request.Candidate.SHA256 = localProofFileSHA256(t, request.CandidatePath)
	replaceAssuranceAttemptForRuntimes(t, request.AttemptPath, request.Candidate, request.Tools)
	proof, err := RunLocalProof(request)
	if err != nil {
		t.Fatalf("RunLocalProof: %v", err)
	}
	if proof.Status != "pass" || len(proof.RawFixtureResults) != 3 {
		t.Fatalf("proof = %#v", proof)
	}
}

func TestLocalProofRejectsFixtureSurfaceWithoutSourceWitness(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "witness.json")
	data := `{"name":"witness","evidence":[{"symbol":"NotInSource.run","surfaceId":"apex:NotInSource.run","kind":"compile"}],"source":[{"path":"force-app/main/default/classes/Witness.cls","content":"// NotInSource.run\nString value = 'NotInSource.run';"}],"command":{"kind":"check"}}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	entry := LocalProofFixture{ID: "witness", Name: "witness", Path: path, SHA256: localProofFileSHA256(t, path), OwnedSurfaceIDs: []string{"apex:NotInSource.run"}, Disposition: compileShapeRequired}
	if _, err := loadLocalProofFixture(entry); err == nil {
		t.Fatal("loadLocalProofFixture accepted a surface absent from materialized Apex")
	}
}

func TestLocalProofAcceptsCompatEvidenceKindsForDisposition(t *testing.T) {
	for _, test := range []struct {
		disposition string
		command     string
		kind        string
		symbol      string
	}{
		{localRuntimeRequired, "exec", "exec", "Runtime.run"},
		{deterministicMockRequired, "test", "test", "Runtime.run"},
		{compileShapeRequired, "check", "shape", "Runtime.run"},
	} {
		t.Run(test.disposition, func(t *testing.T) {
			entry := LocalProofFixture{ID: "fixture", Name: "fixture", Path: filepath.Join(t.TempDir(), "fixture.json"), SHA256: strings.Repeat("a", 64), OwnedSurfaceIDs: []string{"apex:Runtime.run"}, Disposition: test.disposition}
			invocation := compat.Invocation{Kind: test.command}
			if test.command == "exec" {
				invocation.Args = []string{"new Runtime().run();"}
			}
			fixture := compat.Fixture{Name: entry.Name, Command: invocation, Evidence: []compat.FixtureEvidence{{SurfaceID: "apex:Runtime.run", Kind: test.kind, Symbol: test.symbol}}, Source: []compat.SourceFile{{Path: "force-app/main/classes/Fixture.cls", Content: "public class Runtime { public void run() {} }"}}}
			if err := validateLocalProofFixtureIdentity(entry, fixture); err != nil {
				t.Fatalf("validateLocalProofFixtureIdentity() error = %v", err)
			}
		})
	}
}

func TestLocalProofExecutesAStagedCandidateCopy(t *testing.T) {
	request, calls := localProofRequest(t)
	if _, err := RunLocalProof(request); err != nil {
		t.Fatal(err)
	}
	if len(*calls) == 0 || (*calls)[0].Path == request.CandidatePath {
		t.Fatalf("proof command used mutable candidate path: %#v", *calls)
	}
}

func TestValidatesCandidateJSONUsesFrozenCandidateSummaryContract(t *testing.T) {
	if !validatesCandidateJSON([]byte(`{"status":"passed","exitCode":0,"summary":{"total":2,"passed":2,"failed":0,"errors":0,"compileErrors":0,"runtimeErrors":0},"tests":[{},{}]}`), "test", 0) {
		t.Fatal("validatesCandidateJSON rejected a passing frozen-candidate test result")
	}
	if validatesCandidateJSON([]byte(`{"status":"passed","exitCode":0,"summary":{"types":0,"triggers":0}}`), "check", 1) {
		t.Fatal("validatesCandidateJSON accepted zero-work check output")
	}
}

func TestLocalProofPersistsFixtureReceipt(t *testing.T) {
	request, _ := localProofRequest(t)
	if _, err := RunLocalProof(request); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(request.OutputPath)
	if err != nil {
		t.Fatal(err)
	}
	var retained LocalProof
	if err := json.Unmarshal(data, &retained); err != nil {
		t.Fatal(err)
	}
	for _, result := range retained.RawFixtureResults {
		if len(result.Receipt.Command) != 1 || result.Receipt.Command[0] != result.Operation || !sha256Pattern.MatchString(result.Receipt.CommandSpecSHA256) || !sha256Pattern.MatchString(result.Receipt.StdoutSHA256) || !sha256Pattern.MatchString(result.Receipt.StderrSHA256) {
			t.Fatalf("persisted receipt for %q = %#v", result.FixtureID, result.Receipt)
		}
	}
}

func TestValidateLocalProofRejectsIncompleteNormalizedEvidence(t *testing.T) {
	for name, mutate := range map[string]func(*LocalProof){
		"incomplete fixture receipts": func(proof *LocalProof) { proof.RawFixtureResults = proof.RawFixtureResults[:1] },
		"forged candidate binding":    func(proof *LocalProof) { proof.RawFixtureResults[0].CandidateSHA256 = strings.Repeat("f", 64) },
		"forged surface fixture hash": func(proof *LocalProof) { proof.Surfaces[0].FixtureSHA256 = strings.Repeat("f", 64) },
		"wrong fixture operation": func(proof *LocalProof) {
			for i := range proof.RawFixtureResults {
				if proof.RawFixtureResults[i].FixtureID == "runtime" {
					proof.RawFixtureResults[i].Operation = "test"
				}
			}
		},
		"forged receipt command specification": func(proof *LocalProof) {
			proof.RawFixtureResults[0].Receipt.CommandSpecSHA256 = strings.Repeat("f", 64)
		},
		"forged receipt stdout digest": func(proof *LocalProof) {
			proof.RawFixtureResults[0].Receipt.StdoutSHA256 = strings.Repeat("f", 64)
		},
		"forged output and digest": func(proof *LocalProof) {
			proof.RawFixtureResults[0].Stdout = `{}`
			proof.RawFixtureResults[0].StdoutSHA256 = replayBytesSHA256([]byte(`{}`))
		},
	} {
		t.Run(name, func(t *testing.T) {
			request, _ := localProofRequest(t)
			proof, err := RunLocalProof(request)
			if err != nil {
				t.Fatal(err)
			}
			manifest := readLocalProofManifest(t, request.FixtureManifestPath)
			mutate(&proof)
			if err := ValidateLocalProof(proof, manifest); err == nil {
				t.Fatal("ValidateLocalProof accepted forged normalized evidence")
			}
		})
	}
}

func TestLocalProofFixtureRejectsUnknownJSONFields(t *testing.T) {
	root := t.TempDir()
	fixture := localProofFixture(t, root, "strict", []string{"apex:Strict.run"}, compileShapeRequired)
	data, err := os.ReadFile(fixture.Path)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data[:len(data)-1], []byte(`,"unexpected":true}`)...)
	if err := os.WriteFile(fixture.Path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.SHA256 = localProofFileSHA256(t, fixture.Path)
	if _, err := loadLocalProofFixture(fixture); err == nil {
		t.Fatal("loadLocalProofFixture accepted an unknown fixture field")
	}
}

func TestLocalProofFixtureAcceptsSalesforceMetadataExtensions(t *testing.T) {
	root := t.TempDir()
	fixture := localProofFixture(t, root, "metadata", []string{"apex:Metadata.run"}, compileShapeRequired)
	data, err := os.ReadFile(fixture.Path)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data[:len(data)-1], []byte(`,"apiVersion":"67.0","salesforceEligible":false,"salesforceExclusionClass":"org-configuration-required"}`)...)
	if err := os.WriteFile(fixture.Path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.SHA256 = localProofFileSHA256(t, fixture.Path)
	if _, err := loadLocalProofFixture(fixture); err != nil {
		t.Fatalf("loadLocalProofFixture rejected maintenance metadata: %v", err)
	}
}

func TestWriteLocalProofProjectRejectsApexOutsidePackageDirectory(t *testing.T) {
	root := t.TempDir()
	fixture := localProofFixture(t, root, "outside", []string{"apex:Outside.run"}, compileShapeRequired)
	definition, err := loadLocalProofFixture(fixture)
	if err != nil {
		t.Fatal(err)
	}
	definition.Source[0].Path = "outside/Outside.cls"
	project := filepath.Join(root, "project")
	if err := os.Mkdir(project, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := writeLocalProofProject(project, definition); err == nil {
		t.Fatal("writeLocalProofProject accepted Apex outside packageDirectories")
	}
}

func TestVerifyLocalProofReplayRejectsForgedRetainedOutput(t *testing.T) {
	request, _ := localProofRequest(t)
	if err := os.WriteFile(request.CandidatePath, []byte("#!/bin/sh\ncase \"$1\" in\ntest) printf '{\"status\":\"passed\",\"exitCode\":0,\"summary\":{\"total\":1,\"passed\":1,\"failed\":0,\"errors\":0,\"compileErrors\":0,\"runtimeErrors\":0},\"tests\":[{}]}' ;;\ncheck) printf '{\"status\":\"passed\",\"exitCode\":0,\"summary\":{\"types\":1,\"triggers\":0}}' ;;\n*) printf '{\"status\":\"passed\",\"exitCode\":0}' ;;\nesac\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	request.Candidate.SHA256 = localProofFileSHA256(t, request.CandidatePath)
	replaceAssuranceAttemptForRuntimes(t, request.AttemptPath, request.Candidate, request.Tools)
	proof, err := RunLocalProof(request)
	if err != nil {
		t.Fatal(err)
	}
	manifest := readLocalProofManifest(t, request.FixtureManifestPath)
	if err := verifyLocalProofReplay(proof, manifest, request.CandidatePath, request.ToolsPath, request.architecture); err != nil {
		t.Fatalf("VerifyLocalProofReplay(valid): %v", err)
	}
	proof.RawFixtureResults[0].Stdout = `{"status":"passed","exitCode":0,"tests":{"total":2,"failed":0,"errors":0}}`
	proof.RawFixtureResults[0].StdoutSHA256 = replayBytesSHA256([]byte(proof.RawFixtureResults[0].Stdout))
	if err := verifyLocalProofReplay(proof, manifest, request.CandidatePath, request.ToolsPath, request.architecture); err == nil {
		t.Fatal("VerifyLocalProofReplay accepted forged retained output")
	}
}

func TestLocalProofRejectsNarrowedOrUnboundSealedSurfaceInputs(t *testing.T) {
	for _, updateDecision := range []bool{false, true} {
		t.Run(map[bool]string{false: "stale decision hash", true: "narrowed usage set"}[updateDecision], func(t *testing.T) {
			request, _ := localProofRequest(t)
			writeLocalProofJSON(t, request.UsagePath, LocalProofUsage{SchemaVersion: 1, Usage: []LocalProofUsageEntry{{SurfaceID: "apex:Mock.run"}}})
			if updateDecision {
				decision := readLocalProofDecision(t, request.DecisionPath)
				decision.UsageSHA256 = localProofFileSHA256(t, request.UsagePath)
				writeLocalProofJSON(t, request.DecisionPath, decision)
			}
			if _, err := RunLocalProof(request); err == nil {
				t.Fatal("RunLocalProof accepted a caller-narrowed required surface set")
			}
		})
	}
}

func TestLocalProofRejectsFixtureStateItsAdapterDoesNotMaterialize(t *testing.T) {
	request, _ := localProofRequest(t)
	path := requestFixturePath(t, request, "runtime")
	var fixture map[string]any
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	fixture["expected"] = map[string]any{"stdout": "must be checked"}
	writeLocalProofJSON(t, path, fixture)
	manifest := readLocalProofManifest(t, request.FixtureManifestPath)
	for i := range manifest.Fixtures {
		if manifest.Fixtures[i].ID == "runtime" {
			manifest.Fixtures[i].SHA256 = localProofFileSHA256(t, path)
		}
	}
	writeLocalProofManifest(t, request.FixtureManifestPath, manifest)
	updateLocalProofDecisionFixtureHash(t, &request)
	if _, err := RunLocalProof(request); err == nil {
		t.Fatal("RunLocalProof accepted fixture state that the adapter drops")
	}
}

func TestLocalProofRejectsTamperingWrongExecutablesInvalidReceiptsAndExistingOutput(t *testing.T) {
	for name, mutate := range map[string]func(t *testing.T, request *LocalProofRequest){
		"modified fixture": func(t *testing.T, request *LocalProofRequest) {
			t.Helper()
			if err := os.WriteFile(requestFixturePath(t, *request, "runtime"), []byte(`{"name":"runtime","changed":true}`), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"manifest hash tampering": func(t *testing.T, request *LocalProofRequest) {
			t.Helper()
			manifest := readLocalProofManifest(t, request.FixtureManifestPath)
			manifest.Fixtures[0].OwnedSurfaceIDs = []string{"apex:Tampered.run"}
			writeLocalProofManifest(t, request.FixtureManifestPath, manifest)
		},
		"fixture name ownership tampering": func(t *testing.T, request *LocalProofRequest) {
			t.Helper()
			manifest := readLocalProofManifest(t, request.FixtureManifestPath)
			manifest.Fixtures[0].Name = "different"
			writeLocalProofManifest(t, request.FixtureManifestPath, manifest)
			updateLocalProofDecisionFixtureHash(t, request)
		},
		"wrong candidate executable": func(t *testing.T, request *LocalProofRequest) {
			t.Helper()
			writeLocalProofExecutable(t, request.CandidatePath, "candidate replacement")
		},
		"wrong tools executable": func(t *testing.T, request *LocalProofRequest) {
			t.Helper()
			path := filepath.Join(filepath.Dir(request.OutputPath), "other-tools")
			writeLocalProofExecutable(t, path, "tools replacement")
			request.ToolsPath = path
		},
		"claimed pass without a validated result": func(t *testing.T, request *LocalProofRequest) {
			t.Helper()
			request.executor = func(command localProofCommand) localProofExecution {
				return localProofExecution{Receipt: CommandResult{Command: []string{command.Args[0]}, ExitCode: 0, DurationMS: 0, Passed: true}}
			}
		},
		"well-formed wrong command specification": func(t *testing.T, request *LocalProofRequest) {
			t.Helper()
			request.executor = func(command localProofCommand) localProofExecution {
				result := localProofReceipt(command)
				result.CommandSpecSHA256 = strings.Repeat("f", 64)
				return localProofExecution{Receipt: result, Validated: true, Stdout: localProofSuccessOutputFor(command.Args[0])}
			}
		},
		"create only": func(t *testing.T, request *LocalProofRequest) {
			t.Helper()
			if err := os.WriteFile(request.OutputPath, []byte("keep"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			request, calls := localProofRequest(t)
			mutate(t, &request)
			if _, err := RunLocalProof(request); err == nil {
				t.Fatal("RunLocalProof accepted invalid local evidence")
			}
			if name == "create only" {
				if got, err := os.ReadFile(request.OutputPath); err != nil || string(got) != "keep" {
					t.Fatalf("output = %q, %v", got, err)
				}
				if len(*calls) != 0 {
					t.Fatalf("executor calls = %v, want none", *calls)
				}
				return
			}
			if _, err := os.Stat(request.OutputPath); !os.IsNotExist(err) {
				t.Fatalf("invalid proof wrote output: %v", err)
			}
		})
	}
}

func localProofRequest(t *testing.T) (LocalProofRequest, *[]localProofCommand) {
	t.Helper()
	root := t.TempDir()
	candidatePath := filepath.Join(root, "candidate")
	writeLocalProofExecutable(t, candidatePath, "candidate")
	toolsPath, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	fixtures := []LocalProofFixture{
		localProofFixture(t, root, "unused", []string{"apex:Unused.run"}, compileShapeRequired),
		localProofFixture(t, root, "runtime", []string{"apex:Runtime.run", "apex:Runtime.extra"}, localRuntimeRequired),
		localProofFixture(t, root, "mock", []string{"apex:Mock.run"}, deterministicMockRequired),
		localProofFixture(t, root, "shape", []string{"apex:Shape.run"}, compileShapeRequired),
	}
	manifestPath := filepath.Join(root, "fixtures.json")
	writeLocalProofManifest(t, manifestPath, LocalProofFixtureManifest{Fixtures: fixtures})
	selected := []string{"apex:Runtime.extra", "apex:Shape.run", "apex:Mock.run", "apex:Runtime.run"}
	profilePath := filepath.Join(root, "ASSURANCE_PROFILE.json")
	usagePath := filepath.Join(root, "USAGE_RECONCILIATION.json")
	decisionPath := filepath.Join(root, "DECISIONS.json")
	profileRows := make([]LocalProofProfileRow, 0, len(selected))
	usageRows := make([]LocalProofUsageEntry, 0, len(selected))
	decisionRows := make([]LocalProofDecisionRow, 0, len(selected))
	for _, id := range selected {
		profileRows = append(profileRows, LocalProofProfileRow{SurfaceID: id})
		usageRows = append(usageRows, LocalProofUsageEntry{SurfaceID: id})
		decisionRows = append(decisionRows, LocalProofDecisionRow{SurfaceID: id, RequireLocalProof: true})
	}
	writeLocalProofJSON(t, profilePath, LocalProofProfile{SchemaVersion: 1, Rows: profileRows})
	writeLocalProofJSON(t, usagePath, LocalProofUsage{SchemaVersion: 1, Usage: usageRows})
	writeLocalProofJSON(t, decisionPath, LocalProofDecision{SchemaVersion: 1, ProfileSHA256: localProofFileSHA256(t, profilePath), UsageSHA256: localProofFileSHA256(t, usagePath), FixtureManifestSHA256: localProofFileSHA256(t, manifestPath), Decisions: decisionRows})
	calls := []localProofCommand{}
	request := LocalProofRequest{
		AttemptPath:         assuranceAttemptForRuntimes(t, root, localProofRuntime(t, candidatePath, "a"), localProofRuntime(t, toolsPath, "b")),
		ProfilePath:         profilePath,
		UsagePath:           usagePath,
		DecisionPath:        decisionPath,
		FixtureManifestPath: manifestPath,
		Candidate:           localProofRuntime(t, candidatePath, "a"),
		CandidatePath:       candidatePath,
		Tools:               localProofRuntime(t, toolsPath, "b"),
		ToolsPath:           toolsPath,
		OutputPath:          filepath.Join(root, "LOCAL_PROOF.json"),
		architecture:        func(string) (string, error) { return runtime.GOARCH, nil },
	}
	request.executor = func(command localProofCommand) localProofExecution {
		calls = append(calls, command)
		return localProofExecution{Receipt: localProofReceipt(command), Validated: true, Stdout: localProofSuccessOutputFor(command.Args[0])}
	}
	return request, &calls
}

func localProofFixture(t *testing.T, root, name string, surfaceIDs []string, disposition string) LocalProofFixture {
	t.Helper()
	path := filepath.Join(root, name+".json")
	command := `{"kind":"check"}`
	source := "public class " + strings.Title(name) + " { public void run() {} public void extra() {} }"
	if disposition == localRuntimeRequired {
		program := "new Runtime().run(); new Runtime().extra();"
		if name != "runtime" {
			calls := make([]string, 0, len(surfaceIDs))
			for _, surfaceID := range surfaceIDs {
				symbol := strings.TrimPrefix(surfaceID, "apex:")
				if index := strings.IndexByte(symbol, '('); index >= 0 {
					symbol = symbol[:index]
				}
				calls = append(calls, symbol+"();")
			}
			program = strings.Join(calls, " ")
		}
		command = `{"kind":"exec","args":[` + mustJSON(t, program) + `]}`
	} else if disposition == deterministicMockRequired {
		command = `{"kind":"test"}`
		source = "@IsTest private class " + strings.Title(name) + " { public void run() {} @IsTest static void prove() { new Mock().run(); } }"
	}
	evidence := make([]map[string]string, 0, len(surfaceIDs))
	for _, id := range surfaceIDs {
		evidence = append(evidence, map[string]string{"symbol": id, "surfaceId": id, "kind": localProofEvidenceKind(disposition)})
	}
	evidenceJSON, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	data := `{"name":"` + name + `","evidence":` + string(evidenceJSON) + `,"source":[{"path":"force-app/main/default/classes/` + strings.Title(name) + `.cls","content":` + mustJSON(t, source) + `}],"command":` + command + `}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	return LocalProofFixture{ID: name, Name: name, Path: path, SHA256: localProofFileSHA256(t, path), OwnedSurfaceIDs: surfaceIDs, Disposition: disposition}
}

func mustJSON(t *testing.T, value string) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func localProofRuntime(t *testing.T, path, commitByte string) RuntimeArtifact {
	t.Helper()
	return RuntimeArtifact{Commit: strings.Repeat(commitByte, 40), OS: runtime.GOOS, Arch: runtime.GOARCH, SHA256: localProofFileSHA256(t, path)}
}

func localProofSuccessOutputFor(operation string) string {
	if operation == "test" {
		return `{"status":"passed","exitCode":0,"summary":{"total":1,"passed":1,"failed":0,"errors":0,"compileErrors":0,"runtimeErrors":0},"tests":[{}]}`
	}
	if operation == "check" {
		return `{"status":"passed","exitCode":0,"summary":{"types":1,"triggers":0}}`
	}
	return `{"status":"passed","exitCode":0}`
}

func localProofReceipt(command localProofCommand) CommandResult {
	executableSHA256, _ := sha256File(command.Path)
	return CommandResult{
		Command: []string{command.Args[0]}, ExecutableSHA256: executableSHA256, ExecutableAfterSHA256: executableSHA256, CommandSpecSHA256: localProofReceiptSpecSHA256(command, executableSHA256),
		ExitCode: 0, DurationMS: 0, Passed: true,
		StdoutSHA256: replayBytesSHA256([]byte(localProofSuccessOutputFor(command.Args[0]))), StderrSHA256: replayBytesSHA256(nil),
	}
}

func localProofCommands(commands []localProofCommand) [][]string {
	result := make([][]string, 0, len(commands))
	for _, command := range commands {
		result = append(result, command.Args)
	}
	return result
}

func localProofCommandShapes(commands []localProofCommand) [][]string {
	result := localProofCommands(commands)
	for _, command := range result {
		if len(command) >= 3 && command[1] == "--project" {
			command[2] = "."
		}
	}
	return result
}

func requestFixturePath(t *testing.T, request LocalProofRequest, id string) string {
	t.Helper()
	for _, fixture := range readLocalProofManifest(t, request.FixtureManifestPath).Fixtures {
		if fixture.ID == id {
			return fixture.Path
		}
	}
	t.Fatalf("fixture %q not found", id)
	return ""
}

func readLocalProofManifest(t *testing.T, path string) LocalProofFixtureManifest {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var manifest LocalProofFixtureManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	return manifest
}

func writeLocalProofManifest(t *testing.T, path string, manifest LocalProofFixtureManifest) {
	t.Helper()
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeLocalProofJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func readLocalProofDecision(t *testing.T, path string) LocalProofDecision {
	t.Helper()
	var decision LocalProofDecision
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &decision); err != nil {
		t.Fatal(err)
	}
	return decision
}

func updateLocalProofDecisionFixtureHash(t *testing.T, request *LocalProofRequest) {
	t.Helper()
	decision := readLocalProofDecision(t, request.DecisionPath)
	decision.FixtureManifestSHA256 = localProofFileSHA256(t, request.FixtureManifestPath)
	writeLocalProofJSON(t, request.DecisionPath, decision)
}

func writeLocalProofExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
}

func localProofFileSHA256(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func surfaceIDs(proofs []LocalSurfaceProof) []string {
	ids := make([]string, 0, len(proofs))
	for _, proof := range proofs {
		ids = append(ids, proof.SurfaceID)
	}
	return ids
}
