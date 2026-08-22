package corpusassurance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func TestExceptionInaccessibleFieldsHasOneExecutableLocalOwner(t *testing.T) {
	const fixtureName = "core-runtime-exception-inaccessible-fields-tail-local-api67"
	want := []string{
		"apex:System.NoAccessException.getInaccessibleFields()",
		"apex:System.NoDataFoundException.getInaccessibleFields()",
		"apex:System.TouchHandledException.getInaccessibleFields()",
		"apex:System.VisualforceException.getInaccessibleFields()",
		"apex:System.WaveTemplateException.getInaccessibleFields()",
	}
	root, err := filepath.Abs(filepath.Join("..", "..", "docs", "fixtures"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, fixtureName+".json")
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
	if fixture.Name != fixtureName || fixture.Command.Kind != "exec" || len(fixture.Source) != 1 || fixture.Source[0].Path != "anonymous.apex" || len(fixture.Command.Args) != 1 || fixture.Source[0].Content != fixture.Command.Args[0] {
		t.Fatalf("execution envelope = %#v", fixture)
	}
	got := make([]string, 0, len(fixture.Evidence))
	for _, evidence := range fixture.Evidence {
		if evidence.Kind != "exec" {
			t.Fatalf("evidence kind = %q", evidence.Kind)
		}
		got = append(got, evidence.SurfaceID)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("owned IDs = %v, want %v", got, want)
	}
	if metadata.Eligible == nil || *metadata.Eligible || metadata.ExclusionClass != "policy-local-only" || !strings.Contains(strings.ToLower(metadata.ExclusionReason), "zero salesforce parity") {
		t.Fatalf("fixture policy = %#v", metadata)
	}
	source := fixture.Source[0].Content
	for _, witness := range []string{
		"NoAccessException noAccess = new NoAccessException();",
		"NoDataFoundException noData = new NoDataFoundException();",
		"TouchHandledException touch = new TouchHandledException('touch handled');",
		"VisualforceException visualforce = new VisualforceException();",
		"WaveTemplateException wave = new WaveTemplateException();",
		"System.assertEquals('System.TypeException', noAccessFailure.getTypeName());",
		"System.assertEquals('System.TypeException', noDataFailure.getTypeName());",
		"System.assertEquals('System.TypeException', touchFailure.getTypeName());",
		"System.assertEquals('System.TypeException', visualforceFailure.getTypeName());",
		"System.assertEquals('System.TypeException', waveFailure.getTypeName());",
	} {
		if !strings.Contains(source, witness) {
			t.Fatalf("source missing direct witness %q", witness)
		}
	}
	required := make(map[string]string, len(want))
	for _, id := range want {
		required[id] = localRuntimeRequired
	}
	manifest, missing, err := analyzeLocalProofFixtures(root, required)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 0 || len(manifest.Fixtures) != 1 || manifest.Fixtures[0].ID != fixtureName || !reflect.DeepEqual(manifest.Fixtures[0].OwnedSurfaceIDs, want) {
		t.Fatalf("owner manifest = %#v, missing = %v", manifest, missing)
	}
	if candidatePath := os.Getenv("GLADE_CANDIDATE"); candidatePath != "" {
		if !filepath.IsAbs(candidatePath) || localProofFileSHA256(t, candidatePath) != "960ac9f26fa92aae6054cbe0e59f9c4ab1f84397df67bd8a89528068d02a1fce" {
			t.Fatalf("candidate is not the sealed runtime: %q", candidatePath)
		}
		entry := LocalProofFixture{ID: fixtureName, Name: fixtureName, Path: path, SHA256: replayBytesSHA256(data), OwnedSurfaceIDs: want, Disposition: localRuntimeRequired, Operation: "exec", SalesforceEligible: metadata.Eligible, SalesforceExclusionClass: metadata.ExclusionClass, SalesforceExclusionReason: metadata.ExclusionReason}
		command, cleanup, err := materializeLocalProofFixture(entry, candidatePath)
		if err != nil {
			t.Fatal(err)
		}
		defer cleanup()
		execution := runLocalProofCommand(command)
		if !execution.Validated {
			t.Fatalf("sealed candidate execution failed: exit=%d stderr=%s stdout=%s", execution.Receipt.ExitCode, execution.Stderr, execution.Stdout)
		}
	}
	var provenance struct {
		Candidate struct {
			Commit string `json:"commit"`
			SHA256 string `json:"sha256"`
		} `json:"candidate"`
	}
	if err := json.Unmarshal(data, &provenance); err != nil {
		t.Fatal(err)
	}
	if provenance.Candidate.Commit != "3409c4c85827b19712e9df83fc8905aa02bd1dc8" || provenance.Candidate.SHA256 != "960ac9f26fa92aae6054cbe0e59f9c4ab1f84397df67bd8a89528068d02a1fce" {
		t.Fatalf("candidate provenance = %#v", provenance.Candidate)
	}
}
