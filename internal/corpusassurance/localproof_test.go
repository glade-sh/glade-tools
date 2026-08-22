package corpusassurance

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
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
		{"exec", "--project", ".", "--json", "new Runtime().run(); System.assert(true);"},
		{"check", "--project", ".", "--json", "--no-progress"},
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("commands = %v, want %v", got, want)
	}
	if got, want := surfaceIDs(proof.Surfaces), []string{"apex:Mock.run", "apex:Runtime.run", "apex:Shape.run"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("proof surface IDs = %v, want %v", got, want)
	}
	decision := readLocalProofDecision(t, request.DecisionPath)
	if proof.Candidate != request.Candidate || proof.Tools != request.Tools || proof.FixtureManifestSHA256 != decision.FixtureManifestSHA256 {
		t.Fatalf("proof bindings = %#v", proof)
	}
	if !proof.Surfaces[0].BehaviorObserved || !proof.Surfaces[1].RuntimeObserved || !proof.Surfaces[2].CompilePassed {
		t.Fatalf("receipt-derived observations = %#v", proof.Surfaces)
	}
	if _, err := os.Stat(request.OutputPath); err != nil {
		t.Fatalf("proof output was not written: %v", err)
	}
}

func TestApexPagesContextTailFixturesOwnExactPlus34Rows(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "docs", "fixtures"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]struct {
		count       int
		disposition string
		ids         []string
	}{
		"current-base-apexpages-context-tail-runtime-api67.json": {
			count:       33,
			disposition: localRuntimeRequired,
			ids: []string{
				"apex:ApexPages.Component.getComponentById(String)",
				"apex:ApexPages.ComponentIteration.getComponentById(String)",
				"apex:ApexPages.IdeaStandardController.addFields(List<String>)",
				"apex:ApexPages.IdeaStandardController.getCommentList()",
				"apex:ApexPages.IdeaStandardController.getId()",
				"apex:ApexPages.IdeaStandardSetController.addFields(List<String>)",
				"apex:ApexPages.IdeaStandardSetController.cancel()",
				"apex:ApexPages.IdeaStandardSetController.first()",
				"apex:ApexPages.IdeaStandardSetController.getCompleteResult()",
				"apex:ApexPages.IdeaStandardSetController.getHasNext()",
				"apex:ApexPages.IdeaStandardSetController.getHasPrevious()",
				"apex:ApexPages.IdeaStandardSetController.getIdeaList()",
				"apex:ApexPages.IdeaStandardSetController.getListViewOptions()",
				"apex:ApexPages.IdeaStandardSetController.getPageNumber()",
				"apex:ApexPages.IdeaStandardSetController.getRecord()",
				"apex:ApexPages.IdeaStandardSetController.getRecords()",
				"apex:ApexPages.IdeaStandardSetController.getResultSize()",
				"apex:ApexPages.IdeaStandardSetController.hashCode()",
				"apex:ApexPages.IdeaStandardSetController.last()",
				"apex:ApexPages.IdeaStandardSetController.next()",
				"apex:ApexPages.IdeaStandardSetController.previous()",
				"apex:ApexPages.IdeaStandardSetController.save()",
				"apex:ApexPages.IdeaStandardSetController.setPageNumber(Integer)",
				"apex:ApexPages.IdeaStandardSetController.toString()",
				"apex:ApexPages.KnowledgeArticleVersionStandardController.addFields(List<String>)",
				"apex:ApexPages.KnowledgeArticleVersionStandardController.cancel()",
				"apex:ApexPages.KnowledgeArticleVersionStandardController.equals(Object)",
				"apex:ApexPages.KnowledgeArticleVersionStandardController.getId()",
				"apex:ApexPages.KnowledgeArticleVersionStandardController.getSourceId()",
				"apex:ApexPages.KnowledgeArticleVersionStandardController.selectDataCategory(String,String)",
				"apex:ApexPages.KnowledgeArticleVersionStandardController.setDataCategory(String,String)",
				"apex:ApexPages.KnowledgeArticleVersionStandardController.view()",
				"apex:ApexPages.StandardSetController.equals(Object)",
			},
		},
		"current-base-flow-interview-context-tail-deterministic-api67.json": {
			count:       1,
			disposition: deterministicMockRequired,
			ids:         []string{"apex:Flow.Interview.Interview()"},
		},
	}
	for filename, want := range want {
		t.Run(filename, func(t *testing.T) {
			path, err := filepath.Abs(filepath.Join(root, filename))
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var envelope struct {
				Candidate struct {
					Commit string `json:"commit"`
					SHA256 string `json:"sha256"`
				} `json:"candidate"`
			}
			if err := json.Unmarshal(data, &envelope); err != nil {
				t.Fatal(err)
			}
			if envelope.Candidate.Commit != "3409c4c85827b19712e9df83fc8905aa02bd1dc8" || envelope.Candidate.SHA256 != "960ac9f26fa92aae6054cbe0e59f9c4ab1f84397df67bd8a89528068d02a1fce" {
				t.Fatalf("candidate binding = %#v", envelope.Candidate)
			}
			fixture, metadata, err := decodeLocalProofFixtureWithMetadata(data)
			if err != nil {
				t.Fatal(err)
			}
			if err := compat.Validate(fixture); err != nil {
				t.Fatal(err)
			}
			if fixture.Command.Kind != "exec" || len(fixture.Source) != 1 || len(fixture.Command.Args) != 1 || fixture.Source[0].Content != fixture.Command.Args[0] {
				t.Fatalf("source/command mismatch: %#v", fixture)
			}
			got := make([]string, 0, len(fixture.Evidence))
			for _, evidence := range fixture.Evidence {
				if evidence.Kind != "exec" {
					t.Fatalf("evidence kind = %q", evidence.Kind)
				}
				got = append(got, evidence.SurfaceID)
			}
			sort.Strings(got)
			sort.Strings(want.ids)
			if !reflect.DeepEqual(got, want.ids) || len(got) != want.count {
				t.Fatalf("owned IDs = %v, want %v", got, want.ids)
			}
			entry := LocalProofFixture{ID: fixture.Name, Name: fixture.Name, Path: path, OwnedSurfaceIDs: got, Disposition: want.disposition, Operation: "exec", SalesforceEligible: metadata.Eligible, SalesforceExclusionClass: metadata.ExclusionClass, SalesforceExclusionReason: metadata.ExclusionReason}
			if err := validateLocalProofFixtureIdentity(entry, fixture); err != nil {
				t.Fatalf("identity: %v", err)
			}
			if err := validateLocalProofFixtureSalesforceMetadata(entry, metadata); err != nil {
				t.Fatalf("metadata: %v", err)
			}
			required := make(map[string]string, len(want.ids))
			for _, surfaceID := range want.ids {
				required[surfaceID] = want.disposition
			}
			candidates, err := discoverLocalProofFixtures(root, required)
			if err != nil {
				t.Fatal(err)
			}
			for _, candidate := range candidates {
				if candidate.entry.ID == fixture.Name {
					if !reflect.DeepEqual(candidate.entry.OwnedSurfaceIDs, got) {
						t.Fatalf("candidate ownership = %v, want %v", candidate.entry.OwnedSurfaceIDs, got)
					}
					return
				}
			}
			t.Fatal("fixture is not candidate-runnable")
		})
	}
}

func TestNormalizeLocalProofStdoutRemovesVolatileExecutionFields(t *testing.T) {
	left := []byte(`{"durationMs":12,"data":{"project":{"root":"/var/folders/a/T/glade-assurance-fixture-1"},"durationMs":4,"dur":5,"cpuTimeMs":10,"operationId":"one","file":"/var/folders/a/T/glade-assurance-fixture-1/force-app/main/default/classes/Example.cls"},"status":"passed"}`)
	right := []byte(`{"durationMs":98,"data":{"project":{"root":"/var/folders/b/T/glade-assurance-fixture-2"},"durationMs":7,"dur":17,"cpuTimeMs":11,"operationId":"two","file":"/var/folders/b/T/glade-assurance-fixture-2/force-app/main/default/classes/Example.cls"},"status":"passed"}`)

	normalizedLeft, err := normalizeLocalProofStdout(left)
	if err != nil {
		t.Fatal(err)
	}
	normalizedRight, err := normalizeLocalProofStdout(right)
	if err != nil {
		t.Fatal(err)
	}
	if string(normalizedLeft) != string(normalizedRight) {
		t.Fatalf("normalized output differs:\n%s\n%s", normalizedLeft, normalizedRight)
	}
	if strings.Contains(string(normalizedLeft), "durationMs") || strings.Contains(string(normalizedLeft), "dur") || strings.Contains(string(normalizedLeft), "cpuTimeMs") || strings.Contains(string(normalizedLeft), "operationId") || strings.Contains(string(normalizedLeft), "glade-assurance-fixture") {
		t.Fatalf("volatile fields retained: %s", normalizedLeft)
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
		{localRuntimeRequired, "exec", "test", "Runtime.run"},
		{deterministicMockRequired, "test", "test", "Runtime.run"},
		{deterministicMockRequired, "exec", "exec", "Runtime.run"},
		{deterministicMockRequired, "exec", "test", "Runtime.run"},
		{compileShapeRequired, "check", "shape", "Runtime.run"},
	} {
		t.Run(test.disposition, func(t *testing.T) {
			entry := LocalProofFixture{ID: "fixture", Name: "fixture", Path: filepath.Join(t.TempDir(), "fixture.json"), SHA256: strings.Repeat("a", 64), OwnedSurfaceIDs: []string{"apex:Runtime.run"}, Disposition: test.disposition, Operation: test.command}
			invocation := compat.Invocation{Kind: test.command}
			if test.command == "exec" {
				invocation.Args = []string{"new Runtime().run(); System.assert(true);"}
			}
			fixture := compat.Fixture{Name: entry.Name, Command: invocation, Evidence: []compat.FixtureEvidence{{SurfaceID: "apex:Runtime.run", Kind: test.kind, Symbol: test.symbol}}, Source: []compat.SourceFile{{Path: "force-app/main/classes/Fixture.cls", Content: "public class Runtime { public void run() {} }"}}}
			if err := validateLocalProofFixtureIdentity(entry, fixture); err != nil {
				t.Fatalf("validateLocalProofFixtureIdentity() error = %v", err)
			}
		})
	}
}

func TestLocalProofPlannerAcceptsExecutableDeterministicEvidence(t *testing.T) {
	if !localProofCommandMatchesDisposition(deterministicMockRequired, "exec", "apex:Auth.JWT.JWT()") {
		t.Fatal("planner rejected an executable deterministic behavior observation")
	}
	if !localProofOperationMatches(deterministicMockRequired, "exec") {
		t.Fatal("proof validator rejected an executable deterministic behavior observation")
	}
}

func TestLocalProofRejectsDeclarationOnlyRuntimeExecution(t *testing.T) {
	entry := LocalProofFixture{
		ID: "declaration-only", Name: "declaration-only", Path: filepath.Join(t.TempDir(), "fixture.json"),
		OwnedSurfaceIDs: []string{"apex:Schema.ChildRelationship"}, Disposition: localRuntimeRequired,
	}
	fixture := compat.Fixture{
		Name:     entry.Name,
		Command:  compat.Invocation{Kind: "exec", Args: []string{"Schema.ChildRelationship childRelationship;"}},
		Evidence: []compat.FixtureEvidence{{SurfaceID: "apex:Schema.ChildRelationship", Kind: "exec", Symbol: "Schema.ChildRelationship"}},
	}
	if err := validateLocalProofFixtureIdentity(entry, fixture); err == nil {
		t.Fatal("validateLocalProofFixtureIdentity accepted declaration-only runtime evidence")
	}
}

func TestLocalProofConstructorRequiresNewWitness(t *testing.T) {
	entry := LocalProofFixture{ID: "constructor", Name: "constructor", Path: filepath.Join(t.TempDir(), "fixture.json"), OwnedSurfaceIDs: []string{"apex:System.UserInfo.UserInfo()"}, Disposition: localRuntimeRequired, Operation: "exec"}
	for name, source := range map[string]string{
		"method call":   "System.UserInfo.getName();",
		"comment decoy": "// new UserInfo()\nSystem.UserInfo.getName();",
		"string decoy":  "String decoy = 'new UserInfo()'; System.UserInfo.getName();",
	} {
		t.Run(name, func(t *testing.T) {
			fixture := compat.Fixture{Name: entry.Name, Command: compat.Invocation{Kind: "exec", Args: []string{source}}, Evidence: []compat.FixtureEvidence{{SurfaceID: entry.OwnedSurfaceIDs[0], Kind: "exec", Symbol: entry.OwnedSurfaceIDs[0]}}}
			if err := validateLocalProofFixtureIdentity(entry, fixture); err == nil {
				t.Fatal("constructor evidence accepted without a new expression")
			}
		})
	}
	for name, test := range map[string]struct {
		surfaceID string
		source    string
	}{
		"qualified":          {"apex:System.UserInfo.UserInfo()", "new System.UserInfo();"},
		"generic":            {"apex:System.Map.Map()", "new Map<String, Object>();"},
		"collection literal": {"apex:System.Set.Set(Object)", "System.assert(true); new Set<Object>{null};"},
	} {
		t.Run(name, func(t *testing.T) {
			entry.OwnedSurfaceIDs = []string{test.surfaceID}
			fixture := compat.Fixture{Name: entry.Name, Command: compat.Invocation{Kind: "exec", Args: []string{test.source}}, Evidence: []compat.FixtureEvidence{{SurfaceID: test.surfaceID, Kind: "exec", Symbol: test.surfaceID}}}
			if err := validateLocalProofFixtureIdentity(entry, fixture); err != nil {
				t.Fatalf("constructor new expression rejected: %v", err)
			}
		})
	}
}

func TestLocalProofAcceptsAnonymousDeterministicWitness(t *testing.T) {
	entry := LocalProofFixture{ID: "anonymous", Name: "anonymous", Path: filepath.Join(t.TempDir(), "fixture.json"), OwnedSurfaceIDs: []string{"apex:Metadata.StatusCode.INTERNAL_ERROR"}, Disposition: deterministicMockRequired, Operation: "exec"}
	fixture := compat.Fixture{
		Name:     entry.Name,
		Command:  compat.Invocation{Kind: "exec", Args: []string{"System.assertNotEquals(null, Metadata.StatusCode.INTERNAL_ERROR);"}},
		Evidence: []compat.FixtureEvidence{{SurfaceID: "apex:Metadata.StatusCode.INTERNAL_ERROR", Kind: "exec", Symbol: "Metadata.StatusCode.INTERNAL_ERROR"}},
	}
	if err := validateLocalProofFixtureIdentity(entry, fixture); err != nil {
		t.Fatalf("anonymous deterministic witness was rejected: %v", err)
	}
}

func TestDiscoverLocalProofFixturesDoesNotMixDispositions(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "mixed.json")
	data := `{"name":"mixed","evidence":[{"surfaceId":"apex:Runtime.one()","symbol":"Runtime.one","kind":"exec"},{"surfaceId":"apex:Mock.one()","symbol":"Mock.one","kind":"exec"},{"surfaceId":"apex:Mock.two()","symbol":"Mock.two","kind":"exec"}],"command":{"kind":"exec","args":["Runtime.one(); Mock.one(); Mock.two();"]},"salesforceEligible":false,"salesforceExclusionClass":"policy-local-only","salesforceExclusionReason":"test"}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	candidates, err := discoverLocalProofFixtures(root, map[string]string{"apex:Runtime.one()": localRuntimeRequired, "apex:Mock.one()": deterministicMockRequired, "apex:Mock.two()": deterministicMockRequired})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].entry.Disposition != deterministicMockRequired || !reflect.DeepEqual(candidates[0].entry.OwnedSurfaceIDs, []string{"apex:Mock.one()", "apex:Mock.two()"}) {
		t.Fatalf("mixed candidate = %#v", candidates)
	}
}

func TestDiscoverLocalProofFixturesKeepsIndependentlyWitnessedSurfaces(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "partial.json")
	data := `{"name":"partial","evidence":[{"surfaceId":"apex:Witnessed.run()","symbol":"Witnessed.run","kind":"exec"},{"surfaceId":"apex:Unwitnessed.stop()","symbol":"Unwitnessed.stop","kind":"exec"}],"command":{"kind":"exec","args":["new Witnessed().run();"]},"salesforceEligible":false,"salesforceExclusionClass":"policy-local-only","salesforceExclusionReason":"test"}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	candidates, err := discoverLocalProofFixtures(root, map[string]string{"apex:Witnessed.run()": localRuntimeRequired, "apex:Unwitnessed.stop()": localRuntimeRequired})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || !reflect.DeepEqual(candidates[0].entry.OwnedSurfaceIDs, []string{"apex:Witnessed.run()"}) {
		t.Fatalf("partial candidate = %#v", candidates)
	}
}

func TestArchivedEnumFixturesAreFullyCandidateRunnable(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "docs", "fixtures"))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name     string
		filename string
		count    int
		prefixes []string
		excludes []string
	}{
		{"SoapType", "current-base-cb198-schema-soaptype-positive-api67.json", 1308, []string{"apex:Schema.SoapType"}, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(root, test.filename)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			fixture, metadata, err := decodeLocalProofFixtureWithMetadata(data)
			if err != nil {
				t.Fatal(err)
			}
			if err := compat.Validate(fixture); err != nil {
				t.Fatal(err)
			}
			required := make(map[string]string, len(fixture.Evidence))
			owned := make([]string, 0, len(fixture.Evidence))
			for _, evidence := range fixture.Evidence {
				for _, prefix := range test.prefixes {
					if strings.HasPrefix(evidence.SurfaceID, prefix) {
						excluded := false
						for _, fragment := range test.excludes {
							excluded = excluded || strings.Contains(evidence.SurfaceID, fragment)
						}
						if excluded {
							break
						}
						required[evidence.SurfaceID] = localRuntimeRequired
						owned = append(owned, evidence.SurfaceID)
						break
					}
				}
			}
			entry := LocalProofFixture{ID: fixture.Name, Name: fixture.Name, Path: path, OwnedSurfaceIDs: owned, Disposition: localRuntimeRequired, Operation: "exec", SalesforceEligible: metadata.Eligible, SalesforceExclusionClass: metadata.ExclusionClass, SalesforceExclusionReason: metadata.ExclusionReason}
			if err := validateLocalProofFixtureIdentity(entry, fixture); err != nil {
				t.Fatalf("identity: %v", err)
			}
			if err := validateLocalProofFixtureSalesforceMetadata(entry, metadata); err != nil {
				t.Fatalf("metadata: %v", err)
			}
			candidates, err := discoverLocalProofFixtures(root, required)
			if err != nil {
				t.Fatal(err)
			}
			for _, candidate := range candidates {
				if candidate.entry.ID == fixture.Name {
					if len(candidate.entry.OwnedSurfaceIDs) != test.count || candidate.entry.SalesforceEligible == nil || !*candidate.entry.SalesforceEligible {
						t.Fatalf("candidate = %#v", candidate.entry)
					}
					return
				}
			}
			t.Fatal("fixture is not candidate-runnable")
		})
	}
}

func TestCoreRuntimeFixturesAreFullyCandidateRunnable(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "docs", "fixtures"))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		filename string
		count    int
	}{
		{"core-datetime-stdlib.json", 45},
		{"core-collection-stdlib.json", 40},
		{"core-type-id-url-stdlib.json", 31},
		{"core-string-stdlib.json", 28},
		{"core-string-completion-stdlib.json", 31},
		{"core-process-sparkplug-runtime.json", 3},
		{"core-json-raw-runtime.json", 2},
		{"core-runtime-dml-options-duplicate-rule-local.json", 3},
		{"async-finalizer-unsupported.json", 2},
		{"core-integer-valueof-object-runtime.json", 1},
		{"core-string-abbreviate-offset-runtime.json", 1},
		{"integration-eventbus-unsupported.json", 2},
		{"data-platform-sobjectfield-describe-runtime.json", 1},
		{"core-cache-email-runtime.json", 3},
		{"core-system-enum-types-runtime.json", 2},
		{"core-runtime-system-enum-families-api67.json", 104},
		{"core-runtime-database-cursor-sync-tail-api67.json", 10},
		{"core-runtime-apex-schema-tail-api67.json", 13},
		{"core-runtime-database-options-request-tail-api67.json", 6},
		{"core-runtime-database-result-request-tail-local-api67.json", 8},
		{"core-runtime-database-result-error-accessors-api67.json", 10},
		{"core-runtime-database-result-error-constructors-local-api67.json", 4},
		{"core-runtime-database-query-locator-api67.json", 4},
		{"core-runtime-database-query-locator-local-only-api67.json", 9},
		{"core-runtime-eventbus-callback-result-tail-api67.json", 8},
		{"core-runtime-eventbus-test-service-tail-api67.json", 5},
		{"core-runtime-eventbus-trigger-context-api67.json", 1},
		{"core-runtime-jsonparser-tail-local-evidence.json", 6},
		{"core-runtime-database-duplicate-recycle-savepoint-api67.json", 12},
		{"core-messaging-sendemail-error-fields-runtime.json", 2},
		{"core-messaging-mass-email-fields-runtime.json", 3},
		{"integration-pagereference-accessors-runtime.json", 3},
		{"core-http-types-runtime.json", 2},
		{"core-dml-exception-accessors-runtime.json", 8},
		{"core-database-upsert-mode-runtime.json", 8},
		{"core-database-upsert-sobjectfield-runtime.json", 6},
		{"data-database-deleted-window-runtime.json", 6},
		{"data-database-dml-accesslevel-runtime.json", 7},
		{"data-database-merge-request-runtime.json", 6},
		{"data-database-treesave-runtime.json", 22},
		{"data-schema-child-relationships-runtime.json", 8},
		{"data-schema-describe-token-edges-runtime.json", 4},
		{"data-database-delete-undelete-id-runtime.json", 16},
		{"data-database-merge-overloads-runtime.json", 16},
		{"data-schema-describe-field-properties-runtime.json", 25},
		{"data-schema-describe-sobject-properties-runtime.json", 11},
		{"core-dom-xml-runtime-depth.json", 30},
		{"integration-eventbus-change-header-properties-runtime.json", 24},
		{"integration-eventbus-change-header-accessors-runtime.json", 16},
		{"ui-apexpages-component-runtime-depth.json", 15},
		{"data-database-delete-undelete-object-runtime.json", 13},
		{"data-database-insert-update-list-runtime.json", 8},
		{"data-database-query-locator-access-runtime.json", 11},
		{"data-database-empty-recycle-bin-runtime.json", 4},
		{"core-xmlstreamreader-runtime-depth.json", 30},
		{"core-http-request-runtime-depth.json", 21},
		{"core-http-response-runtime-depth.json", 17},
		{"data-schema-child-relationship-aliases-runtime.json", 11},
		{"data-database-convert-lead-runtime.json", 9},
		{"data-database-savepoint-lifecycle-runtime.json", 4},
		{"core-pattern-matcher-stdlib.json", 25},
		{"integration-rest-context-stdlib.json", 23},
		{"async-batchable-impl-and-chunk-iterator.json", 6},
		{"data-platform-schema-describe-dependent-picklists.json", 5},
		{"core-runtime-sobject-clone-source.json", 2},
		{"core-runtime-businesshours-license-local-evidence.json", 7},
		{"core-type-exception-url-followup.json", 6},
		{"core-string-entity-edge-stdlib.json", 11},
		{"core-pattern-quote-stdlib.json", 1},
		{"data-platform-schema-describe-data-categories.json", 48},
		{"core-datetime-runtime-depth.json", 14},
		{"core-decimal-runtime-depth.json", 13},
		{"data-database-dmloptions-runtime.json", 9},
		{"data-database-query-locator-modes-runtime.json", 7},
		{"data-schema-field-metadata-aliases-runtime.json", 13},
		{"data-schema-filtered-lookup-info-runtime.json", 9},
		{"core-date-timezone-runtime-depth.json", 13},
		{"core-list-set-collection-runtime-depth.json", 15},
		{"data-database-cursor-runtime-depth.json", 3},
		{"data-database-pagination-updated-runtime.json", 14},
		{"data-schema-field-scalars-picklist-runtime.json", 17},
		{"data-schema-sobject-recordtype-filtered-runtime.json", 13},
		{"ui-apexpages-page-state-messages-runtime.json", 6},
		{"data-database-batch-boolean-runtime.json", 3},
		{"data-database-immediate-dml-runtime.json", 6},
		{"data-database-async-dml-runtime.json", 9},
		{"data-schema-displaytype-token-runtime.json", 22},
		{"core-exception-object-methods-runtime.json", 4},
		{"core-noaccess-exception-object-methods-local-runtime.json", 4},
		{"core-cookie-map-runtime-depth.json", 10},
		{"ui-apexpages-current-page-test-runtime.json", 1},
		{"core-runtime-string-transform-depth.json", 12},
		{"data-database-leadconvert-config-runtime.json", 25},
		{"data-database-query-binds-runtime.json", 2},
		{"core-describe-sobject-result-runtime-depth.json", 5},
		{"data-schema-field-behavior-runtime.json", 11},
		{"ui-apexpages-standard-set-pagination-runtime.json", 23},
		{"data-platform-schema-describe-tab-result-runtime-depth.json", 23},
		{"core-runtime-string-encoding-rewrite-depth.json", 15},
		{"data-schema-field-residual-runtime.json", 2},
		{"core-sobject-field-clone-runtime-depth.json", 11},
		{"ui-apexpages-idea-standard-set-state-runtime.json", 6},
		{"data-database-leadconvert-result-runtime.json", 15},
		{"data-database-async-dml-list-runtime.json", 6},
		{"data-database-cursor-object-runtime-depth.json", 4},
		{"ui-apexpages-standard-controller-lifecycle-runtime.json", 5},
		{"data-schema-sobject-identity-runtime.json", 5},
		{"ui-apexpages-knowledge-article-controller-record-runtime.json", 2},
		{"ui-apexpages-current-page-parameters-runtime.json", 1},
		{"ui-apexpages-action-root-runtime.json", 1},
		{"data-database-unit-of-work-runtime.json", 12},
		{"core-runtime-date-valueof-depth.json", 1},
		{"data-database-dmloptions-state-runtime-depth.json", 9},
		{"data-database-result-field-state-runtime.json", 9},
		{"ui-apexpages-standard-controller-actions-runtime.json", 4},
		{"data-schema-sobject-field-permissions-runtime.json", 3},
		{"current-base-cb207-metadata-layout-deterministic-api67-runtime.json", 28},
		{"current-base-cb208-metadata-layout-deterministic-api67-runtime.json", 25},
		{"current-base-cb209-metadata-layout-item-section-deterministic-api67-runtime.json", 24},
		{"current-base-cb210-metadata-feed-deterministic-api67-runtime.json", 40},
		{"current-base-cb339-system-b03-api67-runtime.json", 39},
		{"current-base-schema-g02-cb146-api67-20260803-runtime.json", 6},
		{"core-runtime-messaging-single-email-accessors-runtime.json", 16},
		{"core-system-runtime-evidence-closeout-runtime.json", 29},
		{"current-base-process-parameter-type-runtime.json", 9},
		{"current-base-process-plugin-parameter-type-runtime.json", 7},
		{"data-platform-database-async-immediate-dml-runtime.json", 15},
		{"data-platform-schema-describe-fieldsets-runtime.json", 28},
		{"core-runtime-enum-families-wave15-runtime.json", 28},
		{"core-runtime-system-enum-exception-tail-api67.json", 26},
		{"core-runtime-exception-families-wave15-runtime.json", 39},
		{"data-platform-database-dto-family-one-wave15-runtime.json", 20},
		{"data-platform-database-dto-family-two-wave15-runtime.json", 17},
		{"data-platform-database-leadconvert-accessors-wave15-runtime.json", 19},
		{"data-platform-database-result-contracts-wave15-runtime.json", 11},
		{"data-runtime-sobject-helper-wave15-runtime.json", 10},
		{"integration-eventbus-callbacks-wave15-runtime.json", 3},
		{"core-runtime-exception-tail-wave16-runtime.json", 17},
		{"core-runtime-limits-tail-wave16-runtime.json", 14},
		{"core-runtime-primitive-values-wave16-runtime.json", 32},
		{"core-runtime-string-tail-wave16-runtime.json", 5},
		{"core-runtime-value-objects-wave16-runtime.json", 17},
		{"data-platform-schema-describe-results-wave16-runtime.json", 56},
		{"core-runtime-apexpages-controller-wave17-runtime.json", 8},
		{"current-base-apexpages-context-tail-runtime-api67.json", 33},
		{"current-base-flow-interview-context-tail-deterministic-api67.json", 1},
		{"core-runtime-dom-value-semantics-wave17-runtime.json", 7},
		{"core-runtime-static-resource-callout-mocks-wave17-runtime.json", 11},
		{"data-platform-schema-presentation-results-wave17-runtime.json", 42},
		{"data-platform-schema-record-type-info-wave17-runtime.json", 8},
		{"integration-metadata-core-dtos-wave17-runtime.json", 21},
		{"core-runtime-exception-constructor-family-wave18-runtime.json", 26},
		{"data-platform-database-dto-wave18-runtime.json", 31},
		{"data-platform-database-result-dto-wave18-runtime.json", 30},
		{"integration-eventbus-state-wave18-runtime.json", 13},
		{"ui-apexpages-idea-controller-wave18-runtime.json", 6},
		{"core-runtime-dom-xmlreader-value-contracts-wave19-runtime.json", 13},
		{"core-runtime-system-001-wave19-runtime.json", 11},
		{"core-runtime-system-002-wave19-runtime.json", 4},
		{"core-runtime-system-primitive-tail-api67.json", 33},
		{"core-runtime-userprovisioning-deterministic-wave19.json", 3},
		{"core-runtime-search-suggest-deterministic-mock.json", 2},
		{"core-runtime-messaging-dto-mock-api67.json", 14},
		{"data-platform-database-pagination-cursor-wave19-runtime.json", 10},
		{"data-platform-schema-residual-wave19-runtime.json", 20},
	} {
		t.Run(test.filename, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(root, test.filename))
			if err != nil {
				t.Fatal(err)
			}
			fixture, _, err := decodeLocalProofFixtureWithMetadata(data)
			if err != nil {
				t.Fatal(err)
			}
			required := make(map[string]string)
			disposition := localRuntimeRequired
			if fixture.Name == "core-runtime-userprovisioning-deterministic-wave19" || fixture.Name == "core-runtime-search-suggest-deterministic-mock" || fixture.Name == "core-runtime-messaging-dto-mock-api67" || fixture.Name == "current-base-flow-interview-context-tail-deterministic-api67" {
				disposition = deterministicMockRequired
			}
			for _, evidence := range fixture.Evidence {
				required[evidence.SurfaceID] = disposition
			}
			candidates, err := discoverLocalProofFixtures(root, required)
			if err != nil {
				t.Fatal(err)
			}
			for _, candidate := range candidates {
				if candidate.entry.ID != fixture.Name {
					continue
				}
				if len(candidate.entry.OwnedSurfaceIDs) == test.count {
					return
				}
				t.Fatalf("fixture owns %d rows, want %d: %v", len(candidate.entry.OwnedSurfaceIDs), test.count, candidate.entry.OwnedSurfaceIDs)
			}
			t.Fatalf("fixture is not candidate-runnable for all %d rows", test.count)
		})
	}
}

func TestSearchMockCloseoutOwnsExactlyTwoRows(t *testing.T) {
	root := filepath.Join("..", "..", "docs", "fixtures")
	want := map[string]string{"core-runtime-search-suggest-deterministic-mock.json": "apex:System.Search.suggest(String,String,Object)"}
	seen := map[string]string{}
	for file, id := range want {
		data, err := os.ReadFile(filepath.Join(root, file))
		if err != nil {
			t.Fatal(err)
		}
		fixture, metadata, err := decodeLocalProofFixtureWithMetadata(data)
		if err != nil {
			t.Fatal(err)
		}
		if fixture.Command.Kind != "test" || metadata.Eligible == nil || *metadata.Eligible || metadata.ExclusionClass != "policy-local-only" {
			t.Fatalf("%s metadata/command invalid", file)
		}
		found := false
		for _, e := range fixture.Evidence {
			if e.SurfaceID == id && e.Symbol == id && e.Kind == "test" {
				found = true
			}
			if e.SurfaceID == id {
				seen[id] = file
			}
		}
		if !found {
			t.Fatalf("%s lacks direct witness for %s", file, id)
		}
		if file == "core-runtime-search-suggest-deterministic-mock.json" {
			id2 := "apex:System.Search.suggest(String,String,Object,Object)"
			for _, e := range fixture.Evidence {
				if e.SurfaceID == id2 && e.Symbol == id2 && e.Kind == "test" {
					seen[id2] = file
				}
			}
		}
	}
	if len(seen) != 2 {
		t.Fatalf("seen = %#v", seen)
	}
}

func TestLocalProofAcceptsTestExecutionForTestContextRuntimeSurface(t *testing.T) {
	if !localProofEvidenceKindMatches(localRuntimeRequired, "test", "test") {
		t.Fatal("local runtime test fixture was rejected")
	}
	for _, surfaceID := range []string{
		"apex:System.Test.setMock",
		"apex:System.StaticResourceCalloutMock.setStatusCode(Integer)",
		"apex:System.MultiStaticResourceCalloutMock.setStaticResource(String,String)",
	} {
		if !localProofCommandMatchesDisposition(localRuntimeRequired, "test", surfaceID) {
			t.Fatalf("test-context runtime fixture %q was rejected", surfaceID)
		}
	}
}

func TestLocalProofAcceptsEventBusTestContextRuntimeSurfaces(t *testing.T) {
	for _, surfaceID := range []string{
		"apex:eventbus.TestBroker.clone()",
		"apex:eventbus.TestBroker.deliver()",
		"apex:eventbus.TestBroker.fail()",
		"apex:eventbus.TestEventService.clone()",
		"apex:eventbus.TestEventService.publishEvent(String,Map<String,Object>)",
	} {
		if !localProofCommandMatchesDisposition(localRuntimeRequired, "test", surfaceID) {
			t.Fatalf("EventBus test-context runtime fixture %q was rejected", surfaceID)
		}
	}
	for _, surfaceID := range []string{
		"apex:eventbus.ChangeEventHeader.getChangeType()",
		"apex:System.String.valueOf(Object)",
	} {
		if localProofCommandMatchesDisposition(localRuntimeRequired, "test", surfaceID) {
			t.Fatalf("unrelated test fixture %q was accepted for local-runtime-required proof", surfaceID)
		}
	}
}

func TestLocalProofRejectsGenericTestFixtureAsRuntimeProof(t *testing.T) {
	for _, surfaceID := range []string{"apex:System.Location", "apex:System.StaticResourceCalloutMockBuilder.run()"} {
		if localProofCommandMatchesDisposition(localRuntimeRequired, "test", surfaceID) {
			t.Fatalf("generic test fixture %q was accepted as runtime proof", surfaceID)
		}
	}
}

func TestLocalProofAcceptsSourceBackedTestContextRuntimeFixture(t *testing.T) {
	entry := LocalProofFixture{ID: "test-context", Name: "test-context", Path: filepath.Join(t.TempDir(), "fixture.json"), OwnedSurfaceIDs: []string{"apex:System.Test.setMock"}, Disposition: localRuntimeRequired, Operation: "test"}
	fixture := compat.Fixture{
		Name:    entry.Name,
		Command: compat.Invocation{Kind: "test"},
		Evidence: []compat.FixtureEvidence{{
			SurfaceID: "apex:System.Test.setMock",
			Kind:      "test",
			Symbol:    "Test.setMock",
		}},
		Source: []compat.SourceFile{{Path: "force-app/main/default/classes/TestContext.cls", Content: "Test.setMock('HttpCalloutMock', mock);"}},
	}
	if err := validateLocalProofFixtureIdentity(entry, fixture); err != nil {
		t.Fatalf("validateLocalProofFixtureIdentity() error = %v", err)
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

func TestLocalProofFixtureEvidenceOnlyEligibility(t *testing.T) {
	for _, test := range []struct {
		name          string
		value         any
		wantError     string
		wantCandidate bool
	}{
		{"true", true, "evidence-only fixture is not eligible for local proof", false},
		{"false", false, "", true},
		{"string", "true", "invalid evidenceOnly", false},
		{"null", nil, "invalid evidenceOnly", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			fixture := localProofFixture(t, root, "runtime", []string{"apex:Runtime.run"}, localRuntimeRequired)
			var document map[string]any
			data, err := os.ReadFile(fixture.Path)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(data, &document); err != nil {
				t.Fatal(err)
			}
			document["evidenceOnly"] = test.value
			writeLocalProofJSON(t, fixture.Path, document)
			fixture.SHA256 = localProofFileSHA256(t, fixture.Path)

			_, err = loadLocalProofFixture(fixture)
			if test.wantError == "" && err != nil {
				t.Fatalf("loadLocalProofFixture rejected eligible fixture: %v", err)
			}
			if test.wantError != "" && (err == nil || !strings.Contains(err.Error(), test.wantError)) {
				t.Errorf("loadLocalProofFixture error = %v, want %q", err, test.wantError)
			}

			candidates, err := discoverLocalProofFixtures(root, map[string]string{"apex:Runtime.run": localRuntimeRequired})
			if err != nil {
				t.Fatal(err)
			}
			if got := len(candidates) == 1; got != test.wantCandidate {
				t.Fatalf("candidate discovered = %t, want %t", got, test.wantCandidate)
			}
		})
	}
}

func TestLocalProofFixtureAcceptsSalesforceMetadataExtensions(t *testing.T) {
	root := t.TempDir()
	fixture := localProofFixture(t, root, "metadata", []string{"apex:Metadata.run"}, compileShapeRequired)
	data, err := os.ReadFile(fixture.Path)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data[:len(data)-1], []byte(`,"apiVersion":"67.0","salesforceEligible":false,"salesforceExclusionClass":"org-configuration-required","salesforceExclusionReason":"requires explicit scratch-org configuration"}`)...)
	if err := os.WriteFile(fixture.Path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.SHA256 = localProofFileSHA256(t, fixture.Path)
	ineligible := false
	fixture.SalesforceEligible = &ineligible
	fixture.SalesforceExclusionClass = "org-configuration-required"
	fixture.SalesforceExclusionReason = "requires explicit scratch-org configuration"
	if _, err := loadLocalProofFixture(fixture); err != nil {
		t.Fatalf("loadLocalProofFixture rejected maintenance metadata: %v", err)
	}
}

func TestSelectSalesforceFixturesRequiresExplicitEligibility(t *testing.T) {
	fixture := LocalProofFixture{ID: "fixture", Name: "fixture"}
	if _, err := selectSalesforceFixtures([]LocalProofFixture{fixture}); err == nil {
		t.Fatal("selectSalesforceFixtures accepted a fixture without explicit eligibility")
	}
	eligible := true
	fixture.SalesforceEligible = &eligible
	fixtures, err := selectSalesforceFixtures([]LocalProofFixture{fixture})
	if err != nil || len(fixtures) != 1 {
		t.Fatalf("selectSalesforceFixtures = %#v, %v", fixtures, err)
	}
}

func TestLocalProofFixtureAcceptsCandidateSummaryExpectation(t *testing.T) {
	root := t.TempDir()
	fixture := localProofFixture(t, root, "summary", []string{"apex:Summary.run"}, deterministicMockRequired)
	var document map[string]any
	data, err := os.ReadFile(fixture.Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	document["expected"] = map[string]any{"result": map[string]any{"status": "passed", "exitCode": 0}}
	writeLocalProofJSON(t, fixture.Path, document)
	fixture.SHA256 = localProofFileSHA256(t, fixture.Path)
	if _, err := loadLocalProofFixture(fixture); err != nil {
		t.Fatalf("loadLocalProofFixture rejected candidate summary expectation: %v", err)
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
		localProofFixture(t, root, "runtime", []string{"apex:Runtime.run"}, localRuntimeRequired),
		localProofFixture(t, root, "mock", []string{"apex:Mock.run"}, deterministicMockRequired),
		localProofFixture(t, root, "shape", []string{"apex:Shape.run"}, compileShapeRequired),
	}
	manifestPath := filepath.Join(root, "fixtures.json")
	writeLocalProofManifest(t, manifestPath, LocalProofFixtureManifest{Fixtures: fixtures, SalesforceFixtures: []LocalProofFixture{fixtures[1]}})
	selected := []string{"apex:Shape.run", "apex:Mock.run", "apex:Runtime.run"}
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
	operation := "check"
	source := "public class " + strings.Title(name) + " { public void run() {} public void extra() {} }"
	if disposition == localRuntimeRequired {
		program := "new Runtime().run(); System.assert(true);"
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
		operation = "exec"
	} else if disposition == deterministicMockRequired {
		command = `{"kind":"test"}`
		operation = "test"
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
	data := `{"name":"` + name + `","evidence":` + string(evidenceJSON) + `,"source":[{"path":"force-app/main/default/classes/` + strings.Title(name) + `.cls","content":` + mustJSON(t, source) + `}],"command":` + command + `,"salesforceEligible":true}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	eligible := true
	return LocalProofFixture{ID: name, Name: name, Path: path, SHA256: localProofFileSHA256(t, path), OwnedSurfaceIDs: surfaceIDs, Disposition: disposition, Operation: operation, SalesforceEligible: &eligible}
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
