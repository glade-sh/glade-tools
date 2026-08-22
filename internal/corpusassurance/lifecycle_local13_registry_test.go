package corpusassurance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func TestLifecycleLocal13FixturesAreCanonicalPlannerRunnable(t *testing.T) {
	required := map[string]string{
		"apex:System.InstallContext.InstallerId":                     localRuntimeRequired,
		"apex:System.InstallContext.installerId()":                   localRuntimeRequired,
		"apex:System.InstallContext.isPush()":                        localRuntimeRequired,
		"apex:System.InstallContext.previousVersion()":               localRuntimeRequired,
		"apex:System.InstallHandler.onInstall(InstallContext)":       localRuntimeRequired,
		"apex:System.SandboxContext.organizationId()":                localRuntimeRequired,
		"apex:System.SandboxContext.sandboxId()":                     localRuntimeRequired,
		"apex:System.SandboxContext.sandboxName()":                   localRuntimeRequired,
		"apex:System.SandboxPostCopy":                                localRuntimeRequired,
		"apex:System.SandboxPostCopy.runApexClass(SandboxContext)":   localRuntimeRequired,
		"apex:System.UninstallContext":                               localRuntimeRequired,
		"apex:System.UninstallContext.OrganizationId":                localRuntimeRequired,
		"apex:System.UninstallContext.organizationId()":              localRuntimeRequired,
		"apex:System.UninstallHandler":                               localRuntimeRequired,
		"apex:System.UninstallHandler.onUninstall(UninstallContext)": localRuntimeRequired,
	}

	root, err := filepath.Abs(filepath.Join("..", "..", "docs", "fixtures"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, missing, err := analyzeLocalProofFixtures(root, required)
	if err != nil {
		t.Fatal(err)
	}
	wantMissing := []string{"apex:System.InstallContext.InstallerId", "apex:System.UninstallContext.OrganizationId"}
	if !equalStrings(missing, wantMissing) {
		t.Fatalf("missing lifecycle surfaces = %v, want %v", missing, wantMissing)
	}
	covered := map[string]bool{}
	for _, fixture := range manifest.Fixtures {
		for _, surfaceID := range fixture.OwnedSurfaceIDs {
			covered[surfaceID] = true
		}
	}
	if len(covered) != 13 {
		t.Fatalf("canonical lifecycle coverage = %d, want 13: %v", len(covered), covered)
	}
}

func TestLifecycleLocal13DedicatedFixtureRunsSealedCandidate(t *testing.T) {
	candidatePath := os.Getenv("GLADE_CANDIDATE")
	if candidatePath == "" {
		t.Skip("set GLADE_CANDIDATE to run the sealed-candidate regression")
	}
	if !filepath.IsAbs(candidatePath) || localProofFileSHA256(t, candidatePath) != "7ffd4f2a68b78d39621072dd8b09a5b75bf2f96e1f14883f2c73e844ec7e862e" {
		t.Fatalf("candidate is not the sealed lifecycle runtime: %q", candidatePath)
	}

	for _, fixtureName := range []string{"core-runtime-install-context-accessors", "current-base-system-002-local-runtime-api67", "core-runtime-local-sandbox-request-evidence-api67", "core-runtime-local-uninstall-evidence-api67"} {
		t.Run(fixtureName, func(t *testing.T) {
			runLifecycleLocal13Fixture(t, fixtureName, candidatePath)
		})
	}
}

func runLifecycleLocal13Fixture(t *testing.T, fixtureName, candidatePath string) {
	t.Helper()
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
		if evidence.Kind == "test" {
			owned = append(owned, evidence.SurfaceID)
		}
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
