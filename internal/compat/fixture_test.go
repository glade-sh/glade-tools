package compat

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/internal/storage"
	"github.com/glade-sh/glade/internal/vm"
)

const fullDocumentedFixturesEnv = "GLADE_TOOLS_RUN_FULL_COMPAT_FIXTURES"

var documentedFixtureExampleProjectsName = strings.Join([]string{"local-tests", "example", "projects"}, "-")

var documentedFixtureSmokeNames = map[string]struct{}{
	"parser-smoke":                 {},
	"unsupported-exec-call":        {},
	"unsupported-parse-diagnostic": {},
}

func TestFixtureJSONRoundTrip(t *testing.T) {
	in := Fixture{
		Name: "parser-smoke",
		Project: ProjectConfig{
			Namespace:        "pkg",
			SourceAPIVersion: "65.0",
			PackageDirectories: []PackageDirectory{
				{Path: "force-app", Default: true},
				{Path: "modules/core"},
			},
		},
		Schema: []SchemaFile{{
			Path:    "force-app/main/default/objects/Account/Account.object-meta.xml",
			Content: "<CustomObject><label>Account</label></CustomObject>",
		}},
		Source: []SourceFile{{
			Path:    "classes/Hello.cls",
			Content: "public class Hello {}",
		}},
		Command: Invocation{Kind: "parse", Args: []string{"classes/Hello.cls"}, LimitMode: "strict"},
		Expected: ExpectedBehavior{
			Result: json.RawMessage(`{"ok":true}`),
		},
	}

	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}

	var out Fixture
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.Name != in.Name || out.Command.Kind != "parse" || out.Command.LimitMode != "strict" || out.Schema[0].Content != in.Schema[0].Content || out.Project.Namespace != "pkg" || len(out.Project.PackageDirectories) != 2 {
		t.Fatalf("unexpected fixture after round trip: %#v", out)
	}
}

func TestDocumentedFixtureExecutionSelection(t *testing.T) {
	t.Setenv(fullDocumentedFixturesEnv, "")
	if !shouldRunDocumentedFixture("parser-smoke") {
		t.Fatal("parser-smoke should run in the default documented fixture smoke set")
	}
	if shouldRunDocumentedFixture("core-string-stdlib") {
		t.Fatal("full fixture should not run without the opt-in environment variable")
	}

	t.Setenv(fullDocumentedFixturesEnv, "1")
	if !shouldRunDocumentedFixture("core-string-stdlib") {
		t.Fatal("full fixture should run when the opt-in environment variable is set")
	}
	if shouldRunDocumentedFixture("local-tests-corpus") {
		t.Fatal("focused compat baseline fixture should stay out of documented fixture execution")
	}
	if shouldRunDocumentedFixture("apex-language-rules") {
		t.Fatal("Apex language rule catalog should stay out of documented fixture execution")
	}
	if shouldRunDocumentedFixture("apex-local-support-policy") {
		t.Fatal("apex-local-support-policy should stay out of documented fixture execution")
	}
	if shouldRunDocumentedFixture("salesforce-runtime-correctness") {
		t.Fatal("Salesforce runtime correctness catalog should stay out of documented fixture execution")
	}
	if shouldRunDocumentedFixture("salesforce-release-next") {
		t.Fatal("Salesforce release inventory should stay out of documented fixture execution")
	}
	if shouldRunDocumentedFixture("salesforce-release-current") {
		t.Fatal("Salesforce release current should stay out of documented fixture execution")
	}
	if shouldRunDocumentedFixture("salesforce-docs-inventory-spring-26") {
		t.Fatal("Salesforce docs inventory spring 26 should stay out of documented fixture execution")
	}
	if shouldRunDocumentedFixture("salesforce-docs-inventory-summer-26") {
		t.Fatal("Salesforce docs inventory summer 26 should stay out of documented fixture execution")
	}
	if shouldRunDocumentedFixture("salesforce-release-classifications-spring-to-summer-26") {
		t.Fatal("Salesforce release classifications should stay out of documented fixture execution")
	}
	if shouldRunDocumentedFixture("salesforce-release-previous") {
		t.Fatal("Salesforce release previous should stay out of documented fixture execution")
	}
	if shouldRunDocumentedFixture("core-runtime-workflow-txnsecurity-local-defaults") {
		t.Fatal("policy-evidence fixture should stay out of documented fixture execution")
	}
	for _, name := range []string{
		"salesforce-cb187-system-assert-comparisons",
		"apex-api67-removals",
	} {
		if !skipDocumentedFixture(name) {
			t.Fatalf("oracle evidence document %s should stay out of runnable fixture validation", name)
		}
		if shouldRunDocumentedFixture(name) {
			t.Fatalf("oracle evidence document %s should never run as compat fixture", name)
		}
	}
	for _, path := range documentedFixturePaths(t) {
		name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		switch name {
		case "current-base-cb192-system-test-limits-positive-api67",
			"core-runtime-exception-api67-safe-family-accessors":
			t.Fatalf("evidence-only document %s should be excluded by schema", name)
		}
	}
}

func TestDocumentedFixtureJSONLoadAndValidate(t *testing.T) {
	paths := documentedFixturePaths(t)
	for _, path := range paths {
		path := path
		name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		t.Run(name, func(t *testing.T) {
			if skipDocumentedFixture(name) {
				t.Skip("special compat baseline is validated by its focused check test")
			}
			fixture, err := LoadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := Validate(fixture); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRunDocumentedFixtures(t *testing.T) {
	paths := documentedFixturePaths(t)
	for _, path := range paths {
		path := path
		name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		t.Run(name, func(t *testing.T) {
			if !shouldRunDocumentedFixture(name) {
				t.Skipf("documented fixture execution skipped; set %s=1 to run the full sweep", fullDocumentedFixturesEnv)
			}
			fixture, err := LoadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			result, err := Run(fixture)
			if err != nil {
				t.Fatalf("%s: %v", path, err)
			}
			if !result.OK {
				t.Fatalf("%s result = %#v", path, result)
			}
		})
	}
}

func documentedFixturePaths(t *testing.T) []string {
	t.Helper()
	paths, err := filepath.Glob("../../docs/fixtures/*.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no documented fixtures matched ../../docs/fixtures/*.json")
	}
	runnable := make([]string, 0, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var document struct {
			Command struct {
				Kind string `json:"kind"`
			} `json:"command"`
			Source         []json.RawMessage `json:"source"`
			Schema         []json.RawMessage `json:"schema"`
			Metadata       json.RawMessage   `json:"metadata"`
			SeedData       []json.RawMessage `json:"seedData"`
			ServerRequests []json.RawMessage `json:"serverRequests"`
		}
		if err := json.Unmarshal(data, &document); err != nil {
			t.Fatal(err)
		}
		if isRunnableDocument(document) {
			runnable = append(runnable, path)
		}
	}
	if len(runnable) == 0 {
		t.Fatal("no runnable documented fixtures matched ../../docs/fixtures/*.json")
	}
	return runnable
}

func isRunnableDocument(document struct {
	Command struct {
		Kind string `json:"kind"`
	} `json:"command"`
	Source         []json.RawMessage `json:"source"`
	Schema         []json.RawMessage `json:"schema"`
	Metadata       json.RawMessage   `json:"metadata"`
	SeedData       []json.RawMessage `json:"seedData"`
	ServerRequests []json.RawMessage `json:"serverRequests"`
}) bool {
	if document.Command.Kind == "policy-evidence" {
		return true
	}
	return len(document.Source) > 0 ||
		len(document.Schema) > 0 ||
		(len(document.Metadata) > 0 && string(document.Metadata) != "{}") ||
		len(document.SeedData) > 0 ||
		len(document.ServerRequests) > 0
}

func skipDocumentedFixture(name string) bool {
	if isOracleEvidenceDocument(name) {
		return true
	}
	switch name {
	case "apex-language-rules",
		"apex-local-support-policy",
		"async-test-harness-local-evidence",
		"core-runtime-workflow-txnsecurity-local-defaults",
		"core-runtime-json-dto-lwc-evidence",
		"data-platform-schema-lwc-record-wire-evidence",
		"local-tests-corpus",
		documentedFixtureExampleProjectsName,
		"query-runtime-local-search-sosl-evidence",
		"salesforce-docs-inventory-spring-26",
		"salesforce-docs-inventory-summer-26",
		"salesforce-release-classifications-spring-to-summer-26",
		"salesforce-release-current",
		"salesforce-release-next",
		"salesforce-release-previous",
		"salesforce-runtime-correctness",
		"ui-controller-discovery",
		"ui-lwc-vf-local-bridge-evidence",
		"post-parity-trace-events":
		return true
	default:
		return false
	}
}

// isOracleEvidenceDocument identifies catalog, comparison, and API-67 oracle
// packets that are consumed by focused evidence tests rather than by the
// generic runnable-fixture sweep. Keep this schema-family rule centralized so
// new oracle packets do not become accidental local test inputs.
func isOracleEvidenceDocument(name string) bool {
	return strings.HasSuffix(name, "-comparisons") ||
		name == "apex-api67-removals"
}

func shouldRunDocumentedFixture(name string) bool {
	if skipDocumentedFixture(name) {
		return false
	}
	if os.Getenv(fullDocumentedFixturesEnv) != "" {
		return true
	}
	_, ok := documentedFixtureSmokeNames[name]
	return ok
}

func TestValidateFixture(t *testing.T) {
	fixture := Fixture{
		Name:    "parser-smoke",
		Source:  []SourceFile{{Path: "classes/Hello.cls"}},
		Command: Invocation{Kind: "parse"},
	}
	if err := Validate(fixture); err != nil {
		t.Fatal(err)
	}
}

func TestValidatePolicyEvidenceOnlyFixture(t *testing.T) {
	fixture := Fixture{
		Name:    "hosted-policy-evidence",
		Command: Invocation{Kind: "policy-evidence"},
		Evidence: []FixtureEvidence{{
			Symbol:    "soap-api:Account.create",
			SurfaceID: "soap-api:Account.create",
			Kind:      "unsupported",
		}, {
			Symbol:    "apex:System.String",
			SurfaceID: "apex:System.String",
			Kind:      "shape",
		}},
	}
	if err := Validate(fixture); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRejectsEvidenceOnlyRunnableFixture(t *testing.T) {
	fixture := Fixture{
		Name:    "hosted-policy-evidence",
		Command: Invocation{Kind: "exec"},
		Evidence: []FixtureEvidence{{
			Symbol:    "soap-api:Account.create",
			SurfaceID: "soap-api:Account.create",
			Kind:      "unsupported",
		}},
	}
	if err := Validate(fixture); err == nil {
		t.Fatal("expected runnable evidence-only fixture to fail validation")
	}
}

func TestRunParseFixture(t *testing.T) {
	fixture := Fixture{
		Name:    "parser-smoke",
		Source:  []SourceFile{{Path: "Hello.cls", Content: "public class Hello {}"}},
		Command: Invocation{Kind: "parse"},
		Expected: ExpectedBehavior{
			Result: json.RawMessage(`{"diagnostics":0,"files":1,"ok":true}`),
		},
	}
	result, err := Run(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK {
		t.Fatalf("result = %#v", result)
	}
}

func TestRunCheckFixtureRejectsEscapingPath(t *testing.T) {
	fixture := Fixture{
		Name:    "escaping-check",
		Source:  []SourceFile{{Path: "../Hello.cls", Content: "public class Hello {}"}},
		Command: Invocation{Kind: "check"},
	}
	if _, err := Run(fixture); err == nil {
		t.Fatal("expected escaping fixture path to fail")
	}
}

func TestRunCheckFixture(t *testing.T) {
	fixture := Fixture{
		Name: "check-smoke",
		Source: []SourceFile{
			{Path: "classes/Greeter.cls", Content: "public interface Greeter { String greet(); }"},
			{Path: "classes/DefaultGreeter.cls", Content: "public class DefaultGreeter implements Greeter { public String greet() { return 'hello'; } }"},
			{Path: "classes/GreeterService.cls", Content: "public class GreeterService { public String run(Greeter greeter) { return greeter.greet(); } }"},
		},
		Command: Invocation{Kind: "check"},
		Expected: ExpectedBehavior{
			Result: json.RawMessage(`{"diagnostics":0,"files":3,"ok":true,"types":3}`),
		},
	}
	result, err := Run(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK {
		t.Fatalf("result = %#v", result)
	}
}

func TestRunExecFixture(t *testing.T) {
	fixture := Fixture{
		Name:    "exec-smoke",
		Source:  []SourceFile{{Path: "anonymous.apex", Content: "System.debug('hello');"}},
		Command: Invocation{Kind: "exec", Args: []string{"System.debug('hello');"}},
		Expected: ExpectedBehavior{
			Stdout: "hello\n",
			Result: json.RawMessage(`{"debug":["hello"],"ok":true}`),
		},
	}
	result, err := Run(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK {
		t.Fatalf("result = %#v", result)
	}
}

func TestRunExecFixtureUsesProjectSourceAPIVersion(t *testing.T) {
	fixture := Fixture{
		Name: "exec-project-api-version",
		Project: ProjectConfig{
			SourceAPIVersion: "67.0",
		},
		Source:  []SourceFile{{Path: "anonymous.apex", Content: "System.assertEquals('67.0.0', System.requestVersion().toString());"}},
		Command: Invocation{Kind: "exec", Args: []string{"System.assertEquals('67.0.0', System.requestVersion().toString());"}},
		Expected: ExpectedBehavior{
			Result: json.RawMessage(`{"debug":null,"ok":true}`),
		},
	}
	result, err := Run(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK {
		t.Fatalf("result = %#v", result)
	}
}

func TestRunExecFixtureFailsWhenExpectedErrorButExecutionSucceeds(t *testing.T) {
	fixture := Fixture{
		Name:    "exec-expected-error",
		Source:  []SourceFile{{Path: "anonymous.apex", Content: "Integer x = 1;"}},
		Command: Invocation{Kind: "exec", Args: []string{"Integer x = 1;"}},
		Expected: ExpectedBehavior{
			Error: &ExpectedError{Type: "UnsupportedFeature", Message: "unsupported call \"old gap\""},
		},
	}
	if _, err := Run(fixture); err == nil || !strings.Contains(err.Error(), "expected error") {
		t.Fatalf("Run err = %v, want expected error mismatch", err)
	}
}

func TestRunExecFixtureLoadsMetadataRegistry(t *testing.T) {
	fixture := Fixture{
		Name: "exec-metadata-registry",
		Metadata: storage.MetadataRegistry{DataCategoryGroups: []storage.DataCategoryGroup{{
			Name:        "Products",
			Label:       "Products",
			Description: "Product categories",
			SObjectName: "Knowledge__kav",
			Categories: []storage.DataCategory{{
				Name:  "Hardware",
				Label: "Hardware",
				Children: []storage.DataCategory{{
					Name:  "Laptops",
					Label: "Laptops",
				}},
			}},
		}}},
		Command: Invocation{Kind: "exec", Args: []string{`
List<Object> groups = Schema.describeDataCategoryGroups(new List<String>{'Knowledge__kav'});
System.assertEquals(1, groups.size());
System.assertEquals('Products', groups[0].getName());
System.assertEquals(2, groups[0].getCategoryCount());
`}},
		Expected: ExpectedBehavior{
			Result: json.RawMessage(`{"debug":null,"ok":true}`),
		},
	}
	result, err := Run(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK {
		t.Fatalf("result = %#v", result)
	}
}

func TestRunExecFixtureWithLimitMode(t *testing.T) {
	fixture := Fixture{
		Name:    "exec-strict-smoke",
		Source:  []SourceFile{{Path: "anonymous.apex", Content: "System.debug('hello');"}},
		Command: Invocation{Kind: "exec", Args: []string{"System.debug('hello');"}, LimitMode: "strict"},
		Expected: ExpectedBehavior{
			Stdout: "hello\n",
			Result: json.RawMessage(`{"debug":["hello"],"ok":true}`),
		},
	}
	result, err := Run(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK {
		t.Fatalf("result = %#v", result)
	}
}

func TestRunExecFixtureRegistersSourceDTOClasses(t *testing.T) {
	fixture := Fixture{
		Name: "exec-source-dto",
		Source: []SourceFile{
			{Path: "force-app/main/default/classes/JsonDTOBase.cls", Content: "public class JsonDTOBase { public String ExternalId; }"},
			{Path: "force-app/main/default/classes/JsonDTOAddress.cls", Content: "public class JsonDTOAddress { public String City { get; set; } public Integer Zip; }"},
			{Path: "force-app/main/default/classes/JsonDTO.cls", Content: "public class JsonDTO extends JsonDTOBase { public String Name { get; set; } public JsonDTOAddress Primary; public List<JsonDTOAddress> Addresses; public Set<String> Tags; public Map<String, Integer> Scores; public String Missing; }"},
			{Path: "anonymous.apex", Content: "JsonDTO dto = JSON.deserialize('{\"ExternalId\":\"E-7\",\"Name\":\"Ada\",\"Primary\":{\"City\":\"Delta\",\"Zip\":99501},\"Addresses\":[{\"City\":\"Port\",\"Zip\":1}],\"Tags\":[\"north\",\"north\",\"south\"],\"Scores\":{\"trail\":10}}', JsonDTO.class);"},
		},
		Command: Invocation{Kind: "exec", Args: []string{`
JsonDTO dto = JSON.deserialize('{"ExternalId":"E-7","Name":"Ada","Primary":{"City":"Delta","Zip":99501},"Addresses":[{"City":"Port","Zip":1}],"Tags":["north","north","south"],"Scores":{"trail":10}}', JsonDTO.class);
System.assertEquals('E-7', dto.ExternalId);
System.assertEquals('Delta', dto.Primary.City);
JsonDTOAddress first = dto.Addresses.get(0);
System.assertEquals('Port', first.City);
System.assertEquals(2, dto.Tags.size());
System.assertEquals(10, dto.Scores.get('trail'));
System.assertEquals(null, dto.Missing);
Boolean strictCaught = false;
try {
  JsonDTO bad = JSON.deserializeStrict('{"Name":"Ada","Primary":{"City":"Delta","Extra":"nope"}}', JsonDTO.class);
} catch (JSONException e) {
  strictCaught = e.getMessage().contains('unknown field "Extra"');
}
System.assert(strictCaught);
String roundtrip = JSON.serialize(dto, true);
System.assert(roundtrip.contains('"ExternalId":"E-7"'));
System.assert(roundtrip.contains('"Primary"'));
System.assert(!roundtrip.contains('"Missing"'));
`}},
		Expected: ExpectedBehavior{
			Result: json.RawMessage(`{"debug":null,"ok":true}`),
		},
	}
	result, err := Run(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK {
		t.Fatalf("result = %#v", result)
	}
}

func TestRunUnsupportedExecFixtureMatchesExpectedError(t *testing.T) {
	fixture := Fixture{
		Name:    "unsupported-exec-call",
		Source:  []SourceFile{{Path: "anonymous.apex", Content: "System.nope();"}},
		Command: Invocation{Kind: "exec", Args: []string{"System.nope();"}},
		Expected: ExpectedBehavior{
			Error: &ExpectedError{
				Type:    "UnsupportedFeature",
				Message: `unsupported call "System.nope"`,
			},
		},
	}
	result, err := Run(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK || result.Error == nil || result.Error.Type != "UnsupportedFeature" {
		t.Fatalf("result = %#v", result)
	}
}

func TestClassifyErrorUsesRuntimeErrorTypeOnly(t *testing.T) {
	unsupported := classifyError(&vm.RuntimeError{Type: "UnsupportedFeature", Message: `unsupported call "System.nope"`})
	if unsupported.Type != "UnsupportedFeature" || unsupported.Message != `unsupported call "System.nope"` {
		t.Fatalf("unsupported = %#v", unsupported)
	}
	ordinary := classifyError(errors.New("unsupported internal shape"))
	if ordinary.Type != "Error" || ordinary.Message != "unsupported internal shape" {
		t.Fatalf("ordinary = %#v", ordinary)
	}
}
