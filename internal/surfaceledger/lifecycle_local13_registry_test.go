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
	lifecycleLocal13CandidateCommit = "86ec4226e33f205bf7a42f6f00cc40aa57fc11b5"
	lifecycleLocal13CandidateSHA    = "0aa758618a8908550aa468c4c9eabd1fcdd06f9f6a7d317ccce45a077380d29a"
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
	"core-runtime-local-service-evidence-closeout.json": {
		"apex:System.SandboxContext.organizationId()",
		"apex:System.SandboxContext.sandboxId()",
		"apex:System.SandboxContext.sandboxName()",
		"apex:System.SandboxPostCopy.runApexClass(SandboxContext)",
	},
	"current-base-residual-runtime-api67.json": {
		"apex:System.SandboxPostCopy",
	},
	"current-base-platform-lifecycle-shape-api67.json": {
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

	got := make([]string, 0, len(wantOwners))
	for surfaceID := range wantOwners {
		got = append(got, surfaceID)
	}
	sort.Strings(got)
	if len(got) != 13 {
		t.Fatalf("sorted lifecycle accounting rows = %d, want 13", len(got))
	}
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
