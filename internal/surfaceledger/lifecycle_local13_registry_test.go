package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

const (
	lifecycleLocal13CandidateCommit = "e47c5550d551e0cef6bfc7706ba59e68f7e71331"
	lifecycleLocal13CandidateSHA    = "7ffd4f2a68b78d39621072dd8b09a5b75bf2f96e1f14883f2c73e844ec7e862e"
)

var lifecycleLocal13Owners = map[string][]string{
	"core-runtime-install-context-accessors.json": {
		"apex:System.InstallContext.installerId()",
		"apex:System.InstallContext.isPush()",
		"apex:System.InstallContext.previousVersion()",
	},
	"current-base-system-002-local-runtime-api67.json": {
		"apex:System.InstallHandler.onInstall(InstallContext)",
	},
	"core-runtime-local-sandbox-request-evidence-api67.json": {
		"apex:System.SandboxContext.organizationId()",
		"apex:System.SandboxContext.sandboxId()",
		"apex:System.SandboxContext.sandboxName()",
		"apex:System.SandboxPostCopy",
		"apex:System.SandboxPostCopy.runApexClass(SandboxContext)",
	},
	"core-runtime-local-uninstall-evidence-api67.json": {
		"apex:System.UninstallContext",
		"apex:System.UninstallContext.organizationId()",
		"apex:System.UninstallHandler",
		"apex:System.UninstallHandler.onUninstall(UninstallContext)",
	},
}

var lifecycleLocal13Excluded = map[string]string{
	"apex:System.InstallContext.InstallerId":      "uppercase property is API-version-bound and earns no execution credit",
	"apex:System.UninstallContext.OrganizationId": "uppercase property is API-version-bound and earns no execution credit",
}

func TestLifecycleLocal13RegistryAccountsThirteenPositiveRows(t *testing.T) {
	root := filepath.Join("..", "..", "docs", "fixtures")
	paths, err := filepath.Glob(filepath.Join(root, "*.json"))
	if err != nil {
		t.Fatal(err)
	}

	wantOwners := map[string]string{}
	for fixtureName, surfaceIDs := range lifecycleLocal13Owners {
		path := filepath.Join(root, fixtureName)
		fixture, err := compat.LoadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := compat.Validate(fixture); err != nil {
			t.Fatal(err)
		}
		assertLifecycleLocal13Metadata(t, path, len(surfaceIDs))
		seen := map[string]bool{}
		for _, evidence := range fixture.Evidence {
			if _, ok := wantOwners[evidence.SurfaceID]; ok {
				t.Fatalf("duplicate lifecycle row owner %s", evidence.SurfaceID)
			}
			for _, surfaceID := range surfaceIDs {
				if evidence.SurfaceID == surfaceID {
					if seen[surfaceID] {
						t.Fatalf("duplicate lifecycle row %s in %s", surfaceID, fixtureName)
					}
					seen[surfaceID] = true
					wantOwners[surfaceID] = fixtureName
				}
			}
		}
		if len(seen) != len(surfaceIDs) {
			t.Fatalf("%s owns %d lifecycle rows, want %d", fixtureName, len(seen), len(surfaceIDs))
		}
	}
	if len(wantOwners) != 13 {
		t.Fatalf("positive lifecycle rows = %d, want 13", len(wantOwners))
	}

	for _, path := range paths {
		var header struct {
			Evidence []struct {
				SurfaceID string `json:"surfaceId"`
			} `json:"evidence"`
		}
		readJSON(t, path, &header)
		for _, evidence := range header.Evidence {
			if fixtureName, ok := wantOwners[evidence.SurfaceID]; ok && filepath.Base(path) != fixtureName {
				t.Fatalf("lifecycle row %s also appears in %s", evidence.SurfaceID, filepath.Base(path))
			}
		}
	}

	for surfaceID, reason := range lifecycleLocal13Excluded {
		if reason == "" || wantOwners[surfaceID] != "" {
			t.Fatalf("excluded lifecycle row %s was credited: %s", surfaceID, reason)
		}
		owners := 0
		for _, path := range paths {
			var header struct {
				Evidence []struct {
					SurfaceID string `json:"surfaceId"`
				} `json:"evidence"`
			}
			readJSON(t, path, &header)
			for _, evidence := range header.Evidence {
				if evidence.SurfaceID == surfaceID {
					owners++
				}
			}
		}
		if owners != 1 {
			t.Fatalf("excluded lifecycle row %s has %d fixture owners, want 1", surfaceID, owners)
		}
	}

	evidencePaths := make([]string, 0, len(lifecycleLocal13Owners))
	for fixtureName := range lifecycleLocal13Owners {
		evidencePaths = append(evidencePaths, filepath.Join(root, fixtureName))
	}
	evidencePaths = append(evidencePaths, filepath.Join(root, "current-base-platform-lifecycle-shape-api67.json"))
	snapshot, err := BuildEvidenceSnapshot(evidencePaths)
	if err != nil {
		t.Fatal(err)
	}
	snapshotByID := rowsByID(snapshot)
	snapshotCredits := map[string]SurfaceLedgerRow{}
	for surfaceID := range wantOwners {
		row, ok := snapshotByID[surfaceID]
		if !ok {
			t.Fatalf("positive lifecycle row %s is absent from evidence snapshot", surfaceID)
		}
		if !lifecycleLocal13SnapshotCredits(row) {
			t.Fatalf("positive lifecycle row %s snapshot = %#v", surfaceID, row)
		}
		snapshotCredits[surfaceID] = row
	}
	if len(snapshotCredits) != 13 {
		t.Fatalf("snapshot lifecycle credit = %d, want 13", len(snapshotCredits))
	}
	for surfaceID, reason := range lifecycleLocal13Excluded {
		row, ok := snapshotByID[surfaceID]
		if !ok {
			t.Fatalf("excluded lifecycle row %s is absent from evidence snapshot", surfaceID)
		}
		if lifecycleLocal13SnapshotCredits(row) || row.GladeBehavior != BehaviorUnsupported || row.GladeShape != ShapeAbsent {
			t.Fatalf("excluded lifecycle row %s received snapshot credit (%s): %#v", surfaceID, reason, row)
		}
	}

	got := make([]string, 0, len(wantOwners))
	for surfaceID := range wantOwners {
		got = append(got, surfaceID)
	}
	sort.Strings(got)
	if len(got) != 13 {
		t.Fatalf("sorted lifecycle accounting rows = %d, want 13", len(got))
	}
}

func lifecycleLocal13SnapshotCredits(row SurfaceLedgerRow) bool {
	return row.Evidence == EvidenceFixture && (row.GladeBehavior == BehaviorSupported || row.GladeShape != ShapeAbsent)
}

func assertLifecycleLocal13Metadata(t *testing.T, path string, selectedRows int) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Candidate struct {
			Commit string `json:"commit"`
			SHA256 string `json:"sha256"`
		} `json:"candidate"`
		Profile struct {
			CandidateCommit  string `json:"candidateCommit"`
			CandidateSHA256  string `json:"candidateSha256"`
			LaneID           string `json:"laneId"`
			SelectedRowCount int    `json:"selectedRowCount"`
		} `json:"profile"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Candidate.Commit != lifecycleLocal13CandidateCommit || envelope.Candidate.SHA256 != lifecycleLocal13CandidateSHA || envelope.Profile.CandidateCommit != lifecycleLocal13CandidateCommit || envelope.Profile.CandidateSHA256 != lifecycleLocal13CandidateSHA || envelope.Profile.LaneID != "lifecycle-local13" || envelope.Profile.SelectedRowCount != selectedRows {
		t.Fatalf("%s lifecycle provenance = %#v/%#v", path, envelope.Candidate, envelope.Profile)
	}
}
