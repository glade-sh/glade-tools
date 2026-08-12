package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

const (
	cb193CandidateCommit = "381e27e47e720ac7dbecb906bf275d2d5bcdd37f"
	cb193CandidateSHA256 = "976c2ceceb2a0936889129d2dc7314d2065fbc538caf54bbb534f20e4f588166"
)

type cb193FixtureEnvelope struct {
	Name       string `json:"name"`
	PacketID   string `json:"packetId"`
	APIVersion string `json:"apiVersion"`
	Candidate  struct {
		Commit string `json:"commit"`
		SHA256 string `json:"sha256"`
	} `json:"candidate"`
}

func TestCB193LocalMockCoreFixtureIsExact(t *testing.T) {
	root := filepath.Join("..", "..")
	fixturePath := filepath.Join(root, "docs", "fixtures", "current-base-cb193-local-mock-core-positive.json")
	fixture, err := compat.LoadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if fixture.Name != "current-base-cb193-local-mock-core-positive" {
		t.Fatalf("fixture name = %q", fixture.Name)
	}
	if len(fixture.Evidence) != 49 {
		t.Fatalf("fixture evidence rows = %d, want 49 direct successful rows", len(fixture.Evidence))
	}

	var meta cb193FixtureEnvelope
	readCB193JSON(t, fixturePath, &meta)
	if meta.Name != fixture.Name || meta.PacketID != "CB193" || meta.APIVersion != "67.0" {
		t.Fatalf("fixture metadata = %#v", meta)
	}
	if meta.Candidate.Commit != cb193CandidateCommit || meta.Candidate.SHA256 != cb193CandidateSHA256 {
		t.Fatalf("candidate = %#v", meta.Candidate)
	}
	if fixture.Command.Kind != "exec" || fixture.Command.LimitMode != "permissive" || len(fixture.Source) != 1 {
		t.Fatalf("fixture execution envelope = %#v", fixture)
	}

	evidence, err := BuildEvidenceSnapshot([]string{fixturePath})
	if err != nil {
		t.Fatal(err)
	}
	if len(evidence) != len(fixture.Evidence) {
		t.Fatalf("snapshot rows = %d, fixture rows = %d", len(evidence), len(fixture.Evidence))
	}

	gladeByID := rowsBySurfaceKey(BuildGladeSnapshot())
	seen := map[string]bool{}
	counts := map[string]int{}
	for _, row := range evidence {
		if seen[row.SurfaceID] {
			t.Fatalf("duplicate fixture surface %s", row.SurfaceID)
		}
		seen[row.SurfaceID] = true
		canonical, ok := gladeByID[surfaceIDKey(row.SurfaceID)]
		if !ok {
			t.Errorf("fixture surface %s is absent from current Glade snapshot", row.SurfaceID)
			continue
		}
		if row.SurfaceID != canonical.SurfaceID {
			t.Errorf("fixture surface %s is not canonical snapshot ID %s", row.SurfaceID, canonical.SurfaceID)
		}
		if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported {
			t.Errorf("%s evidence/behavior = %s/%s, want fixture/supported", row.SurfaceID, row.Evidence, row.GladeBehavior)
		}
		counts[cb193Family(row.SurfaceID)]++
	}
	if counts["Cache"] != 16 || counts["Metadata"] != 13 || counts["Messaging"] != 15 || counts["UserProvisioning"] != 5 {
		t.Fatalf("family counts = %#v, want Cache 16, Metadata 13, Messaging 15, UserProvisioning 5", counts)
	}
}

func TestCB193RetainedEvidenceMatchesFixture(t *testing.T) {
	evidenceRoot, ok := findCB193EvidenceRoot(t)
	if !ok {
		t.Skip("retained CB193 evidence is not mounted")
	}
	fixture, err := compat.LoadFile(filepath.Join("..", "..", "docs", "fixtures", "current-base-cb193-local-mock-core-positive.json"))
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool, len(fixture.Evidence))
	for _, row := range fixture.Evidence {
		seen[row.SurfaceID] = true
	}
	manifestPath := filepath.Join(evidenceRoot, "manifest.json")
	var manifest struct {
		CreditedRows   int `json:"creditedRows"`
		UncreditedRows []struct {
			SurfaceID string `json:"surfaceId"`
			Status    string `json:"status"`
		} `json:"uncreditedRows"`
	}
	readCB193JSON(t, manifestPath, &manifest)
	if manifest.CreditedRows != 49 || len(manifest.UncreditedRows) != 3 {
		t.Fatalf("manifest counts = credited %d, uncredited %d", manifest.CreditedRows, len(manifest.UncreditedRows))
	}
	for _, row := range manifest.UncreditedRows {
		if seen[row.SurfaceID] || row.Status != "mismatch" {
			t.Errorf("uncredited row = %#v; positive rows and mismatches must stay separate", row)
		}
	}
}

func cb193Family(surfaceID string) string {
	for prefix, family := range map[string]string{
		"apex:Cache.":            "Cache",
		"apex:Metadata.":         "Metadata",
		"apex:Messaging.Email.":  "Messaging",
		"apex:UserProvisioning.": "UserProvisioning",
		"apex:userprovisioning.": "UserProvisioning",
	} {
		if strings.HasPrefix(surfaceID, prefix) {
			return family
		}
	}
	return "unknown"
}

func findCB193EvidenceRoot(t *testing.T) (string, bool) {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		candidate := filepath.Join(dir, "evidence", "current-base", "cb193-local-mock-core-contracts")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", false
}

func readCB193JSON(t *testing.T, path string, target any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatal(err)
	}
}
