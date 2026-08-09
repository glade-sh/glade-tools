package corpusassurance

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSealedHostInputsBindsExactManifestFiles(t *testing.T) {
	directory := t.TempDir()
	repository := validRepositorySpec("private-corpus-001", strings.Repeat("a", 40))
	repository.AssignedHost = "local"
	scope := InventorySpec{SchemaVersion: 1, Scope: "private-corpus-assurance", Repositories: []InventoryEntry{{ID: repository.ID, CheckoutPath: filepath.Join(directory, "checkout"), ExpectedCommit: repository.ExpectedCommit}}}
	scopePath := filepath.Join(directory, "IN_SCOPE.json")
	if err := WriteNewJSON(scopePath, scope); err != nil {
		t.Fatal(err)
	}
	root := InventoryManifest{SchemaVersion: 1, InventorySHA256: sha256FileForTest(t, scopePath), Repositories: []RepositorySpec{repository}}
	rootPath := filepath.Join(directory, "MANIFEST.json")
	if err := WriteNewJSON(rootPath, root); err != nil {
		t.Fatal(err)
	}
	host := HostManifest{SchemaVersion: 1, Host: "local", RootManifestSHA256: sha256FileForTest(t, rootPath), Repositories: []RepositorySpec{repository}}
	hostPath := filepath.Join(directory, "host.json")
	if err := WriteNewJSON(hostPath, host); err != nil {
		t.Fatal(err)
	}

	inputs, err := LoadSealedHostInputs(scopePath, rootPath, hostPath, "local")
	if err != nil {
		t.Fatal(err)
	}
	if inputs.Bindings.InventorySHA256 != root.InventorySHA256 || inputs.Bindings.RootManifestSHA256 != host.RootManifestSHA256 || inputs.Host.Repositories[0].ID != repository.ID {
		t.Fatalf("sealed inputs = %#v", inputs)
	}
}

func TestLoadSealedHostInputsRejectsTamperedRootAndPartition(t *testing.T) {
	// The success test proves the format. These mutations must be rejected before replay.
	directory := t.TempDir()
	repository := validRepositorySpec("private-corpus-001", strings.Repeat("a", 40))
	repository.AssignedHost = "local"
	scope := InventorySpec{SchemaVersion: 1, Scope: "private-corpus-assurance", Repositories: []InventoryEntry{{ID: repository.ID, CheckoutPath: filepath.Join(directory, "checkout"), ExpectedCommit: repository.ExpectedCommit}}}
	scopePath := filepath.Join(directory, "IN_SCOPE.json")
	if err := WriteNewJSON(scopePath, scope); err != nil {
		t.Fatal(err)
	}
	rootPath := filepath.Join(directory, "MANIFEST.json")
	if err := WriteNewJSON(rootPath, InventoryManifest{SchemaVersion: 1, InventorySHA256: strings.Repeat("a", 64), Repositories: []RepositorySpec{repository}}); err != nil {
		t.Fatal(err)
	}
	hostPath := filepath.Join(directory, "host.json")
	if err := WriteNewJSON(hostPath, HostManifest{SchemaVersion: 1, Host: "casper", RootManifestSHA256: strings.Repeat("b", 64), Repositories: []RepositorySpec{repository}}); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSealedHostInputs(scopePath, rootPath, hostPath, "local"); err == nil {
		t.Fatal("tampered manifest inputs were accepted")
	}
}
