package corpusassurance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManifestRejectsUnsafeInventory(t *testing.T) {
	valid := InventorySpec{
		SchemaVersion: 1,
		Scope:         "private-corpus-assurance",
		Repositories: []InventoryEntry{{
			ID:             "private-corpus-001",
			CheckoutPath:   "/private/input/repo",
			ExpectedCommit: strings.Repeat("a", 40),
		}},
	}
	if err := ValidateInventorySpec(valid); err != nil {
		t.Fatalf("ValidateInventorySpec(valid): %v", err)
	}

	for name, mutate := range map[string]func(*InventorySpec){
		"duplicate IDs":  func(spec *InventorySpec) { spec.Repositories = append(spec.Repositories, spec.Repositories[0]) },
		"non-neutral ID": func(spec *InventorySpec) { spec.Repositories[0].ID = "skip" },
		"wrong commit":   func(spec *InventorySpec) { spec.Repositories[0].ExpectedCommit = "not-a-commit" },
		"blank checkout": func(spec *InventorySpec) { spec.Repositories[0].CheckoutPath = "" },
	} {
		t.Run(name, func(t *testing.T) {
			spec := valid
			spec.Repositories = append([]InventoryEntry(nil), valid.Repositories...)
			mutate(&spec)
			if err := ValidateInventorySpec(spec); err == nil {
				t.Fatal("ValidateInventorySpec accepted invalid input")
			}
		})
	}
}

func TestManifestValidatesRuntimeAndRedactsPrivatePaths(t *testing.T) {
	runtime := RuntimeArtifact{Commit: strings.Repeat("b", 40), OS: "darwin", Arch: "arm64", SHA256: strings.Repeat("c", 64)}
	if err := ValidateRuntimeArtifact(runtime); err != nil {
		t.Fatalf("ValidateRuntimeArtifact(valid): %v", err)
	}
	runtime.OS = "linux"
	if err := ValidateRuntimeArtifact(runtime); err != nil {
		t.Fatalf("ValidateRuntimeArtifact(linux): %v", err)
	}
	runtime.Arch = "armv7"
	if err := ValidateRuntimeArtifact(runtime); err == nil {
		t.Fatal("ValidateRuntimeArtifact accepted unsupported architecture")
	}

	projected, err := PublicRepositorySpec(RepositorySpec{
		ID: "private-corpus-001", ExpectedCommit: strings.Repeat("a", 40), ArchiveSHA256: strings.Repeat("b", 64), TreeSHA256: strings.Repeat("c", 64), AssignedHost: "local", SnapshotPath: "snapshots/private-corpus-001", LocalTests: "tests-not-present", LocalTestsReason: "no test source files",
	})
	if err != nil {
		t.Fatalf("PublicRepositorySpec: %v", err)
	}
	if strings.Contains(projected, "/private/input/repo") {
		t.Fatalf("public projection leaked private path: %s", projected)
	}
}

func TestWriteNewJSONIsCreateOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "artifact.json")
	if err := WriteNewJSON(path, map[string]string{"status": "ok"}); err != nil {
		t.Fatalf("first WriteNewJSON: %v", err)
	}
	if err := WriteNewJSON(path, map[string]string{"status": "changed"}); err == nil {
		t.Fatal("second WriteNewJSON unexpectedly overwrote artifact")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 600", info.Mode().Perm())
	}
}

func TestReadInventorySpecRejectsTrailingJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "IN_SCOPE.json")
	data, err := json.Marshal(InventorySpec{SchemaVersion: 1, Scope: "private-corpus-assurance", Repositories: []InventoryEntry{{ID: "private-corpus-001", CheckoutPath: "/private/input/repo", ExpectedCommit: strings.Repeat("a", 40)}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, []byte("\n{}\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadInventorySpec(path); err == nil {
		t.Fatal("ReadInventorySpec accepted a second JSON value")
	}
}

func TestRepositorySpecRejectsUnsafeBindings(t *testing.T) {
	valid := RepositorySpec{ID: "private-corpus-001", ExpectedCommit: strings.Repeat("a", 40), ArchiveSHA256: strings.Repeat("b", 64), TreeSHA256: strings.Repeat("c", 64), AssignedHost: "local", SnapshotPath: "snapshots/private-corpus-001", LocalTests: "required"}
	for name, mutate := range map[string]func(*RepositorySpec){
		"absolute snapshot": func(spec *RepositorySpec) { spec.SnapshotPath = "/private/tmp/snapshot" },
		"unsupported host":  func(spec *RepositorySpec) { spec.AssignedHost = "salesforce-worker" },
		"no test reason":    func(spec *RepositorySpec) { spec.LocalTests, spec.LocalTestsReason = "tests-not-present", "" },
	} {
		t.Run(name, func(t *testing.T) {
			spec := valid
			mutate(&spec)
			if err := ValidateRepositorySpec(spec); err == nil {
				t.Fatal("ValidateRepositorySpec accepted invalid binding")
			}
		})
	}
}

func TestPublicRepositorySpecOmitsFreeFormPrivateReason(t *testing.T) {
	projected, err := PublicRepositorySpec(RepositorySpec{
		ID:               "private-corpus-001",
		ExpectedCommit:   strings.Repeat("a", 40),
		ArchiveSHA256:    strings.Repeat("b", 64),
		TreeSHA256:       strings.Repeat("c", 64),
		AssignedHost:     "local",
		SnapshotPath:     "snapshots/private-corpus-001",
		LocalTests:       "tests-not-present",
		LocalTestsReason: "private checkout reason",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(projected, "private checkout reason") {
		t.Fatalf("public projection leaked local reason: %s", projected)
	}
}
