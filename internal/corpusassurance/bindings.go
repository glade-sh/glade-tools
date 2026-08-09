package corpusassurance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type SealedHostInputs struct {
	Inventory InventorySpec
	Root      InventoryManifest
	Host      HostManifest
	Bindings  ReplayBindings
}

func LoadSealedHostInputs(inventoryPath, rootPath, hostPath, expectedHost string) (SealedHostInputs, error) {
	if !filepath.IsAbs(inventoryPath) || !filepath.IsAbs(rootPath) || !filepath.IsAbs(hostPath) || (expectedHost != "local" && expectedHost != "casper") {
		return SealedHostInputs{}, fmt.Errorf("sealed manifest paths and host are required")
	}
	inventory, inventoryBytes, err := readInventorySpec(inventoryPath)
	if err != nil {
		return SealedHostInputs{}, err
	}
	root, rootBytes, err := readExactJSONBytes[InventoryManifest](rootPath)
	if err != nil {
		return SealedHostInputs{}, err
	}
	host, hostBytes, err := readExactJSONBytes[HostManifest](hostPath)
	if err != nil {
		return SealedHostInputs{}, err
	}
	inventorySHA256 := replayBytesSHA256(inventoryBytes)
	rootSHA256 := replayBytesSHA256(rootBytes)
	hostSHA256 := replayBytesSHA256(hostBytes)
	if root.SchemaVersion != 1 || root.InventorySHA256 != inventorySHA256 || ValidateAssuranceAttempt(root.Attempt) != nil || root.Attempt.InventorySHA256 != inventorySHA256 || !sha256Pattern.MatchString(rootSHA256) || host.SchemaVersion != 1 || host.Host != expectedHost || host.RootManifestSHA256 != rootSHA256 {
		return SealedHostInputs{}, fmt.Errorf("sealed manifest bindings do not match")
	}
	if err := ValidateInventoryCoverage(inventory, root.Repositories); err != nil {
		return SealedHostInputs{}, err
	}
	expected := make(map[string]RepositorySpec)
	for _, repository := range root.Repositories {
		if repository.AssignedHost == expectedHost {
			expected[repository.ID] = repository
		}
	}
	if len(host.Repositories) != len(expected) {
		return SealedHostInputs{}, fmt.Errorf("host manifest repository count mismatch")
	}
	for _, repository := range host.Repositories {
		if expected[repository.ID] != repository {
			return SealedHostInputs{}, fmt.Errorf("host manifest repository %q does not match root", repository.ID)
		}
		delete(expected, repository.ID)
	}
	if len(expected) != 0 {
		return SealedHostInputs{}, fmt.Errorf("host manifest is missing repositories")
	}
	return SealedHostInputs{Inventory: inventory, Root: root, Host: host, Bindings: ReplayBindings{InventorySHA256: inventorySHA256, RootManifestSHA256: rootSHA256, HostManifestSHA256: hostSHA256}}, nil
}

func readExactJSON[T any](path string) (T, error) {
	value, _, err := readExactJSONBytes[T](path)
	return value, err
}

func readExactJSONBytes[T any](path string) (T, []byte, error) {
	var value T
	data, err := os.ReadFile(path)
	if err != nil {
		return value, nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return value, nil, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return value, nil, fmt.Errorf("multiple JSON values")
		}
		return value, nil, err
	}
	return value, data, nil
}
