package corpusassurance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

var (
	commitPattern   = regexp.MustCompile(`^[0-9a-f]{40}$`)
	sha256Pattern   = regexp.MustCompile(`^[0-9a-f]{64}$`)
	repositoryIDPat = regexp.MustCompile(`^private-corpus-[0-9]{3,}$`)
)

type RuntimeArtifact struct {
	Commit string `json:"commit"`
	OS     string `json:"os"`
	Arch   string `json:"arch"`
	SHA256 string `json:"sha256"`
}

type InventoryEntry struct {
	ID             string `json:"id"`
	CheckoutPath   string `json:"checkoutPath"`
	ExpectedCommit string `json:"expectedCommit"`
}

type InventorySpec struct {
	SchemaVersion int              `json:"schemaVersion"`
	Scope         string           `json:"scope"`
	Repositories  []InventoryEntry `json:"repositories"`
}

type RepositorySpec struct {
	ID               string `json:"id"`
	ExpectedCommit   string `json:"expectedCommit"`
	ArchiveSHA256    string `json:"archiveSha256"`
	TreeSHA256       string `json:"treeSha256"`
	AssignedHost     string `json:"assignedHost"`
	SnapshotPath     string `json:"snapshotPath"`
	LocalTests       string `json:"localTests"`
	LocalTestsReason string `json:"localTestsReason,omitempty"`
	TestShardCount   int    `json:"testShardCount,omitempty"`
}

func ValidateRuntimeArtifact(artifact RuntimeArtifact) error {
	if !commitPattern.MatchString(artifact.Commit) || !sha256Pattern.MatchString(artifact.SHA256) {
		return fmt.Errorf("runtime artifact must bind a 40-character commit and sha256")
	}
	if (artifact.OS != "darwin" && artifact.OS != "linux") || (artifact.Arch != "arm64" && artifact.Arch != "amd64") {
		return fmt.Errorf("unsupported runtime %s/%s", artifact.OS, artifact.Arch)
	}
	return nil
}

func ValidateInventorySpec(spec InventorySpec) error {
	if spec.SchemaVersion != 1 || spec.Scope != "private-corpus-assurance" || len(spec.Repositories) == 0 {
		return fmt.Errorf("invalid inventory schema, scope, or repository set")
	}
	seen := make(map[string]bool, len(spec.Repositories))
	for _, repo := range spec.Repositories {
		if !repositoryIDPat.MatchString(repo.ID) || seen[repo.ID] || repo.CheckoutPath == "" || !filepath.IsAbs(repo.CheckoutPath) || !commitPattern.MatchString(repo.ExpectedCommit) {
			return fmt.Errorf("invalid inventory repository %q", repo.ID)
		}
		seen[repo.ID] = true
	}
	return nil
}

func ReadInventorySpec(path string) (InventorySpec, error) {
	spec, _, err := readInventorySpec(path)
	return spec, err
}

func readInventorySpec(path string) (InventorySpec, []byte, error) {
	data, err := readAssuranceFile(path)
	if err != nil {
		return InventorySpec{}, nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var spec InventorySpec
	if err := decoder.Decode(&spec); err != nil {
		return InventorySpec{}, nil, err
	}
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return InventorySpec{}, nil, fmt.Errorf("inventory contains multiple JSON values")
	} else if err != io.EOF {
		return InventorySpec{}, nil, err
	}
	if err := ValidateInventorySpec(spec); err != nil {
		return InventorySpec{}, nil, err
	}
	return spec, data, nil
}

func ValidateRepositorySpec(repo RepositorySpec) error {
	if !repositoryIDPat.MatchString(repo.ID) || !commitPattern.MatchString(repo.ExpectedCommit) || !sha256Pattern.MatchString(repo.ArchiveSHA256) || !sha256Pattern.MatchString(repo.TreeSHA256) {
		return fmt.Errorf("invalid repository bindings for %q", repo.ID)
	}
	if repo.AssignedHost != "local" && repo.AssignedHost != "replay-worker" {
		return fmt.Errorf("unsupported repository host %q", repo.AssignedHost)
	}
	if repo.SnapshotPath == "" || filepath.IsAbs(repo.SnapshotPath) {
		return fmt.Errorf("snapshot path must be relative")
	}
	if filepath.Clean(repo.SnapshotPath) != filepath.Join("snapshots", repo.ID) {
		return fmt.Errorf("snapshot path must be generated from repository id")
	}
	if repo.LocalTests != "required" && repo.LocalTests != "tests-not-present" {
		return fmt.Errorf("invalid local test state %q", repo.LocalTests)
	}
	if repo.LocalTests == "tests-not-present" && repo.LocalTestsReason == "" {
		return fmt.Errorf("tests-not-present requires a reason")
	}
	if repo.LocalTests == "tests-not-present" && repo.TestShardCount != 0 {
		return fmt.Errorf("tests-not-present cannot carry test shards")
	}
	if repo.TestShardCount < 0 || repo.TestShardCount > 1024 {
		return fmt.Errorf("invalid test shard count for %q", repo.ID)
	}
	return nil
}

func PublicRepositorySpec(repo RepositorySpec) (string, error) {
	if err := ValidateRepositorySpec(repo); err != nil {
		return "", err
	}
	data, err := json.Marshal(struct {
		ID             string `json:"id"`
		ExpectedCommit string `json:"expectedCommit"`
		ArchiveSHA256  string `json:"archiveSha256"`
		TreeSHA256     string `json:"treeSha256"`
		AssignedHost   string `json:"assignedHost"`
		SnapshotPath   string `json:"snapshotPath"`
		LocalTests     string `json:"localTests"`
		TestShardCount int    `json:"testShardCount,omitempty"`
	}{repo.ID, repo.ExpectedCommit, repo.ArchiveSHA256, repo.TreeSHA256, repo.AssignedHost, repo.SnapshotPath, repo.LocalTests, repo.TestShardCount})
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func WriteNewJSON(path string, value any) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, "."+filepath.Base(path)+".tmp-")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if err := json.NewEncoder(temporary).Encode(value); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Link(temporaryPath, path); err != nil {
		return err
	}
	if err := os.Remove(temporaryPath); err != nil {
		return err
	}
	directoryFile, err := os.Open(directory)
	if err != nil {
		return err
	}
	if err := directoryFile.Sync(); err != nil {
		directoryFile.Close()
		return err
	}
	return directoryFile.Close()
}
