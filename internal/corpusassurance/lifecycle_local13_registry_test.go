package corpusassurance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func TestLifecycleLocal13DedicatedFixtureRunsSealedCandidate(t *testing.T) {
	candidatePath := os.Getenv("GLADE_CANDIDATE")
	if candidatePath == "" {
		t.Skip("set GLADE_CANDIDATE to run the sealed-candidate regression")
	}
	if !filepath.IsAbs(candidatePath) || localProofFileSHA256(t, candidatePath) != "0aa758618a8908550aa468c4c9eabd1fcdd06f9f6a7d317ccce45a077380d29a" {
		t.Fatalf("candidate is not the sealed lifecycle runtime: %q", candidatePath)
	}

	const fixtureName = "core-runtime-local-sandbox-request-evidence-api67"
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "docs", "fixtures", fixtureName+".json")
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
	owned := make([]string, 0, len(fixture.Evidence))
	for _, evidence := range fixture.Evidence {
		owned = append(owned, evidence.SurfaceID)
	}
	entry := LocalProofFixture{
		ID: fixtureName, Name: fixtureName, Path: path, SHA256: replayBytesSHA256(data),
		OwnedSurfaceIDs: owned, Disposition: localRuntimeRequired, Operation: "test",
		SalesforceEligible: metadata.Eligible, SalesforceExclusionClass: metadata.ExclusionClass, SalesforceExclusionReason: metadata.ExclusionReason,
	}
	if err := validateLocalProofFixtureIdentity(entry, fixture); err != nil {
		t.Fatal(err)
	}
	command, cleanup, err := materializeLocalProofFixture(entry, candidatePath)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	execution := runLocalProofCommand(command)
	if !execution.Validated {
		t.Fatalf("sealed candidate execution failed: exit=%d stderr=%s stdout=%s", execution.Receipt.ExitCode, execution.Stderr, execution.Stdout)
	}
	var result struct {
		Status   string `json:"status"`
		ExitCode int    `json:"exitCode"`
		Summary  struct {
			Errors int `json:"errors"`
			Failed int `json:"failed"`
			Passed int `json:"passed"`
			Total  int `json:"total"`
		} `json:"summary"`
	}
	if err := json.Unmarshal([]byte(execution.Stdout), &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "passed" || result.ExitCode != 0 || result.Summary != (struct {
		Errors int `json:"errors"`
		Failed int `json:"failed"`
		Passed int `json:"passed"`
		Total  int `json:"total"`
	}{Errors: 0, Failed: 0, Passed: 1, Total: 1}) {
		t.Fatalf("sealed candidate result = %#v", result)
	}
}
