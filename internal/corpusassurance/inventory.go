package corpusassurance

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type InventoryManifest struct {
	SchemaVersion   int              `json:"schemaVersion"`
	InventorySHA256 string           `json:"inventorySha256"`
	Repositories    []RepositorySpec `json:"repositories"`
}

type HostManifest struct {
	SchemaVersion      int              `json:"schemaVersion"`
	Host               string           `json:"host"`
	RootManifestSHA256 string           `json:"rootManifestSha256"`
	Repositories       []RepositorySpec `json:"repositories"`
}

type checkout struct {
	entry InventoryEntry
	root  string
}

// PrepareInventory creates immutable source snapshots and sealed per-host manifests.
// inventoryPath must name an already-created, valid IN_SCOPE.json.
func PrepareInventory(inventoryPath, output string) (InventoryManifest, error) {
	if filepath.Base(inventoryPath) != "IN_SCOPE.json" {
		return InventoryManifest{}, fmt.Errorf("inventory input must be named IN_SCOPE.json")
	}
	spec, inventoryBytes, err := readInventorySpec(inventoryPath)
	if err != nil {
		return InventoryManifest{}, fmt.Errorf("read inventory: %w", err)
	}
	inventorySHA256 := replayBytesSHA256(inventoryBytes)

	checkouts := make([]checkout, 0, len(spec.Repositories))
	seenRoots := make([]checkout, 0, len(spec.Repositories))
	for _, entry := range spec.Repositories {
		root, err := validateCheckout(entry)
		if err != nil {
			return InventoryManifest{}, err
		}
		root, err = filepath.EvalSymlinks(root)
		if err != nil {
			return InventoryManifest{}, fmt.Errorf("resolve repository %q root: %w", entry.ID, err)
		}
		info, err := os.Stat(root)
		if err != nil {
			return InventoryManifest{}, fmt.Errorf("stat repository %q root: %w", entry.ID, err)
		}
		for _, existing := range seenRoots {
			existingInfo, err := os.Stat(existing.root)
			if err != nil {
				return InventoryManifest{}, fmt.Errorf("stat repository %q root: %w", existing.entry.ID, err)
			}
			if os.SameFile(info, existingInfo) {
				return InventoryManifest{}, fmt.Errorf("repositories %q and %q resolve to the same checkout", existing.entry.ID, entry.ID)
			}
		}
		resolved := checkout{entry: entry, root: root}
		seenRoots = append(seenRoots, resolved)
		checkouts = append(checkouts, resolved)
	}
	sort.Slice(checkouts, func(i, j int) bool { return checkouts[i].entry.ID < checkouts[j].entry.ID })

	if err := os.Mkdir(output, 0o700); err != nil {
		return InventoryManifest{}, fmt.Errorf("create inventory output: %w", err)
	}
	if err := os.Mkdir(filepath.Join(output, "archives"), 0o700); err != nil {
		return InventoryManifest{}, fmt.Errorf("create archive directory: %w", err)
	}
	if err := os.Mkdir(filepath.Join(output, "snapshots"), 0o700); err != nil {
		return InventoryManifest{}, fmt.Errorf("create snapshot directory: %w", err)
	}

	repositories := make([]RepositorySpec, 0, len(checkouts))
	for _, source := range checkouts {
		repository, err := createSnapshot(output, source)
		if err != nil {
			return InventoryManifest{}, err
		}
		repositories = append(repositories, repository)
	}
	repositories, err = AssignRepositories(repositories)
	if err != nil {
		return InventoryManifest{}, err
	}
	if err := ValidateInventoryCoverage(spec, repositories); err != nil {
		return InventoryManifest{}, err
	}
	_, postflightBytes, err := readInventorySpec(inventoryPath)
	if err != nil {
		return InventoryManifest{}, fmt.Errorf("revalidate inventory: %w", err)
	}
	if replayBytesSHA256(postflightBytes) != inventorySHA256 {
		return InventoryManifest{}, fmt.Errorf("inventory changed while snapshots were prepared")
	}

	manifest := InventoryManifest{
		SchemaVersion:   1,
		InventorySHA256: inventorySHA256,
		Repositories:    repositories,
	}
	rootPath := filepath.Join(output, "MANIFEST.json")
	if err := WriteNewJSON(rootPath, manifest); err != nil {
		return InventoryManifest{}, fmt.Errorf("write root manifest: %w", err)
	}
	rootSHA256, err := sha256File(rootPath)
	if err != nil {
		return InventoryManifest{}, fmt.Errorf("hash root manifest: %w", err)
	}
	if err := writeHostManifests(output, repositories, rootSHA256); err != nil {
		return InventoryManifest{}, err
	}
	return manifest, nil
}

// ValidateInventoryCoverage proves that generated repositories exactly match the
// frozen inventory denominator.
func ValidateInventoryCoverage(spec InventorySpec, repositories []RepositorySpec) error {
	if err := ValidateInventorySpec(spec); err != nil {
		return err
	}
	if len(spec.Repositories) != len(repositories) {
		return fmt.Errorf("inventory repository count mismatch")
	}
	expected := make(map[string]InventoryEntry, len(spec.Repositories))
	for _, entry := range spec.Repositories {
		expected[entry.ID] = entry
	}
	seen := make(map[string]bool, len(repositories))
	for _, repository := range repositories {
		if err := ValidateRepositorySpec(repository); err != nil {
			return err
		}
		entry, ok := expected[repository.ID]
		if !ok {
			return fmt.Errorf("repository %q is not in inventory", repository.ID)
		}
		if seen[repository.ID] {
			return fmt.Errorf("repository %q appears more than once", repository.ID)
		}
		if repository.ExpectedCommit != entry.ExpectedCommit {
			return fmt.Errorf("repository %q has an unexpected commit", repository.ID)
		}
		seen[repository.ID] = true
	}
	for _, entry := range spec.Repositories {
		if !seen[entry.ID] {
			return fmt.Errorf("inventory repository %q is missing", entry.ID)
		}
	}
	return nil
}

// AssignRepositories provides a stable fallback assignment when no duration
// history is available. Repositories are ordered by neutral ID before they are
// alternated between the two supported hosts.
func AssignRepositories(repositories []RepositorySpec) ([]RepositorySpec, error) {
	assigned := append([]RepositorySpec(nil), repositories...)
	sort.Slice(assigned, func(i, j int) bool { return assigned[i].ID < assigned[j].ID })
	seen := make(map[string]bool, len(assigned))
	for index := range assigned {
		if !repositoryIDPat.MatchString(assigned[index].ID) || seen[assigned[index].ID] {
			return nil, fmt.Errorf("invalid or duplicate repository %q", assigned[index].ID)
		}
		seen[assigned[index].ID] = true
		if index%2 == 0 {
			assigned[index].AssignedHost = "local"
		} else {
			assigned[index].AssignedHost = "casper"
		}
	}
	return assigned, nil
}

func validateCheckout(entry InventoryEntry) (string, error) {
	root, err := gitOutput(entry.CheckoutPath, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("repository %q is not a git checkout: %w", entry.ID, err)
	}
	status, err := gitOutput(entry.CheckoutPath, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return "", fmt.Errorf("read git status for %q: %w", entry.ID, err)
	}
	if status != "" {
		return "", fmt.Errorf("repository %q has tracked or untracked changes", entry.ID)
	}
	head, err := gitOutput(entry.CheckoutPath, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("read git head for %q: %w", entry.ID, err)
	}
	if head != entry.ExpectedCommit {
		return "", fmt.Errorf("repository %q head does not match expected commit", entry.ID)
	}
	return root, nil
}

func createSnapshot(output string, source checkout) (RepositorySpec, error) {
	archivePath := filepath.Join(output, "archives", source.entry.ID+".tar")
	if err := gitArchive(source.root, source.entry.ExpectedCommit, archivePath); err != nil {
		return RepositorySpec{}, fmt.Errorf("archive repository %q: %w", source.entry.ID, err)
	}
	archiveSHA256, err := sha256File(archivePath)
	if err != nil {
		return RepositorySpec{}, fmt.Errorf("hash archive for %q: %w", source.entry.ID, err)
	}
	snapshotPath := filepath.Join("snapshots", source.entry.ID)
	snapshotRoot, err := rootedPath(output, snapshotPath)
	if err != nil {
		return RepositorySpec{}, err
	}
	if err := extractTar(archivePath, snapshotRoot); err != nil {
		return RepositorySpec{}, fmt.Errorf("extract archive for %q: %w", source.entry.ID, err)
	}
	treeSHA256, err := canonicalTreeSHA256(snapshotRoot)
	if err != nil {
		return RepositorySpec{}, fmt.Errorf("hash snapshot tree for %q: %w", source.entry.ID, err)
	}
	localTests, reason, err := discoverLocalTests(snapshotRoot)
	if err != nil {
		return RepositorySpec{}, fmt.Errorf("discover local tests for %q: %w", source.entry.ID, err)
	}
	return RepositorySpec{
		ID:               source.entry.ID,
		ExpectedCommit:   source.entry.ExpectedCommit,
		ArchiveSHA256:    archiveSHA256,
		TreeSHA256:       treeSHA256,
		SnapshotPath:     snapshotPath,
		LocalTests:       localTests,
		LocalTestsReason: reason,
	}, nil
}

func writeHostManifests(output string, repositories []RepositorySpec, rootSHA256 string) error {
	for _, host := range []string{"local", "casper"} {
		hostRoot := filepath.Join(output, "hosts", host)
		if err := os.MkdirAll(hostRoot, 0o700); err != nil {
			return fmt.Errorf("create %s host output: %w", host, err)
		}
		hostRepositories := make([]RepositorySpec, 0, len(repositories))
		for _, repository := range repositories {
			if repository.AssignedHost != host {
				continue
			}
			if err := verifyRepositorySnapshot(output, repository); err != nil {
				return fmt.Errorf("verify source snapshot for host %s: %w", host, err)
			}
			if err := copyRepositorySnapshot(output, hostRoot, repository); err != nil {
				return fmt.Errorf("copy snapshot for host %s: %w", host, err)
			}
			if err := verifyRepositorySnapshot(hostRoot, repository); err != nil {
				return fmt.Errorf("verify host snapshot for %s: %w", host, err)
			}
			hostRepositories = append(hostRepositories, repository)
		}
		manifest := HostManifest{
			SchemaVersion:      1,
			Host:               host,
			RootManifestSHA256: rootSHA256,
			Repositories:       hostRepositories,
		}
		if err := WriteNewJSON(filepath.Join(hostRoot, "manifest.json"), manifest); err != nil {
			return fmt.Errorf("write %s host manifest: %w", host, err)
		}
	}
	return nil
}

func verifyRepositorySnapshot(root string, repository RepositorySpec) error {
	if err := ValidateRepositorySpec(repository); err != nil {
		return err
	}
	archivePath, err := rootedPath(root, filepath.Join("archives", repository.ID+".tar"))
	if err != nil {
		return err
	}
	archiveSHA256, err := sha256File(archivePath)
	if err != nil {
		return err
	}
	if archiveSHA256 != repository.ArchiveSHA256 {
		return fmt.Errorf("archive hash mismatch for %q", repository.ID)
	}
	snapshotRoot, err := rootedPath(root, repository.SnapshotPath)
	if err != nil {
		return err
	}
	treeSHA256, err := canonicalTreeSHA256(snapshotRoot)
	if err != nil {
		return err
	}
	if treeSHA256 != repository.TreeSHA256 {
		return fmt.Errorf("tree hash mismatch for %q", repository.ID)
	}
	return nil
}

func gitArchive(repository, commit, archivePath string) error {
	archive, err := os.OpenFile(archivePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	command := exec.Command("git", "-C", repository, "archive", "--format=tar", commit)
	command.Stdout = archive
	var stderr bytes.Buffer
	command.Stderr = &stderr
	runErr := command.Run()
	closeErr := archive.Close()
	if runErr != nil {
		return fmt.Errorf("git archive: %w: %s", runErr, strings.TrimSpace(stderr.String()))
	}
	return closeErr
}

func gitOutput(repository string, arguments ...string) (string, error) {
	arguments = append([]string{"-C", repository}, arguments...)
	output, err := exec.Command("git", arguments...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(arguments[2:], " "), err)
	}
	return strings.TrimSpace(string(output)), nil
}

func extractTar(archivePath, destination string) error {
	archive, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer archive.Close()
	if err := os.Mkdir(destination, 0o700); err != nil {
		return err
	}
	reader := tar.NewReader(archive)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if header.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		path, err := tarPath(destination, header.Name)
		if err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(path, os.FileMode(header.Mode)&os.ModePerm); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				return err
			}
			file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(file, reader)
			modeErr := file.Chmod(regularFileMode(header.Mode))
			closeErr := file.Close()
			if copyErr != nil {
				return copyErr
			}
			if modeErr != nil {
				return modeErr
			}
			if closeErr != nil {
				return closeErr
			}
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				return err
			}
			if err := validateSymlinkTarget(destination, path, header.Linkname); err != nil {
				return err
			}
			if err := os.Symlink(header.Linkname, path); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported tar entry %q", header.Name)
		}
	}
}

func regularFileMode(mode int64) os.FileMode {
	if mode&0o111 != 0 {
		return 0o755
	}
	return 0o644
}

func tarPath(root, name string) (string, error) {
	if filepath.IsAbs(name) {
		return "", fmt.Errorf("tar entry %q is absolute", name)
	}
	return rootedPath(root, name)
}

func validateSymlinkTarget(root, linkPath, target string) error {
	if target == "" || filepath.IsAbs(target) {
		return fmt.Errorf("symlink target %q must be a non-empty relative path", target)
	}
	resolved := filepath.Clean(filepath.Join(filepath.Dir(linkPath), target))
	relative, err := filepath.Rel(root, resolved)
	if err != nil {
		return err
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("symlink target %q escapes snapshot", target)
	}
	return nil
}

func copyRepositorySnapshot(sourceRoot, destinationRoot string, repository RepositorySpec) error {
	sourceArchive, err := rootedPath(sourceRoot, filepath.Join("archives", repository.ID+".tar"))
	if err != nil {
		return err
	}
	destinationArchive, err := rootedPath(destinationRoot, filepath.Join("archives", repository.ID+".tar"))
	if err != nil {
		return err
	}
	if err := copyFile(sourceArchive, destinationArchive); err != nil {
		return err
	}
	source, err := rootedPath(sourceRoot, repository.SnapshotPath)
	if err != nil {
		return err
	}
	destination, err := rootedPath(destinationRoot, repository.SnapshotPath)
	if err != nil {
		return err
	}
	return copyTree(source, destination)
}

func copyFile(source, destination string) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode()&os.ModePerm)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	modeErr := output.Chmod(info.Mode() & os.ModePerm)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	if modeErr != nil {
		return modeErr
	}
	return closeErr
}

func copyTree(source, destination string) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	if err := os.Mkdir(destination, 0o700); err != nil {
		return err
	}
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || path == source {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target, err := rootedPath(destination, relative)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return os.Mkdir(target, info.Mode()&os.ModePerm)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported snapshot entry %q", relative)
		}
		return copyFile(path, target)
	})
}

func canonicalTreeSHA256(root string) (string, error) {
	type treeEntry struct {
		path   string
		mode   string
		digest string
	}
	var entries []treeEntry
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || path == root {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		var digest string
		switch {
		case info.Mode().IsRegular():
			digest, err = sha256File(path)
		case info.Mode()&os.ModeSymlink != 0:
			link, linkErr := os.Readlink(path)
			if linkErr != nil {
				return linkErr
			}
			sum := sha256.Sum256([]byte(link))
			digest = hex.EncodeToString(sum[:])
		default:
			return fmt.Errorf("unsupported snapshot entry %q", relative)
		}
		if err != nil {
			return err
		}
		entries = append(entries, treeEntry{
			path:   filepath.ToSlash(relative),
			mode:   fmt.Sprintf("%04o", info.Mode()&os.ModePerm),
			digest: digest,
		})
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].path < entries[j].path })
	hash := sha256.New()
	for _, entry := range entries {
		for _, value := range []string{entry.path, entry.mode, entry.digest} {
			if _, err := io.WriteString(hash, value); err != nil {
				return "", err
			}
			if _, err := hash.Write([]byte{0}); err != nil {
				return "", err
			}
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func discoverLocalTests(root string) (string, string, error) {
	found := false
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || found || entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !strings.EqualFold(filepath.Ext(path), ".cls") {
			return walkErr
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		found = containsApexTestMarker(stripApexCommentsAndStrings(string(contents)))
		return nil
	})
	if err != nil {
		return "", "", err
	}
	if found {
		return "required", "", nil
	}
	return "tests-not-present", "no Apex test classes found in snapshot", nil
}

func containsApexTestMarker(source string) bool {
	lower := strings.ToLower(source)
	return containsApexToken(lower, "@istest") || containsApexToken(lower, "testmethod")
}

func containsApexToken(source, token string) bool {
	for offset := 0; ; {
		index := strings.Index(source[offset:], token)
		if index < 0 {
			return false
		}
		index += offset
		end := index + len(token)
		if (index == 0 || !isApexIdentChar(source[index-1])) && (end == len(source) || !isApexIdentChar(source[end])) {
			return true
		}
		offset = end
	}
}

func isApexIdentChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_'
}

func stripApexCommentsAndStrings(source string) string {
	var out strings.Builder
	out.Grow(len(source))
	for i := 0; i < len(source); {
		if source[i] == '\'' {
			i++
			for i < len(source) {
				if source[i] == '\\' {
					i += 2
					continue
				}
				if source[i] == '\'' {
					i++
					break
				}
				i++
			}
			out.WriteByte(' ')
			continue
		}
		if i+1 < len(source) && source[i] == '/' && source[i+1] == '/' {
			for i < len(source) && source[i] != '\n' {
				i++
			}
			out.WriteByte(' ')
			continue
		}
		if i+1 < len(source) && source[i] == '/' && source[i+1] == '*' {
			i += 2
			for i+1 < len(source) {
				if source[i] == '*' && source[i+1] == '/' {
					i += 2
					break
				}
				i++
			}
			out.WriteByte(' ')
			continue
		}
		out.WriteByte(source[i])
		i++
	}
	return out.String()
}

func sha256File(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func rootedPath(root, relative string) (string, error) {
	if filepath.IsAbs(relative) {
		return "", fmt.Errorf("path %q must be relative", relative)
	}
	path := filepath.Join(root, relative)
	contained, err := filepath.Rel(root, path)
	if err != nil {
		return "", err
	}
	if contained == ".." || strings.HasPrefix(contained, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes root", relative)
	}
	return path, nil
}
