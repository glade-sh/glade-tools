package corpusassurance

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInventoryCreatesSnapshotsAndHostManifests(t *testing.T) {
	first := newInventoryRepository(t, map[string]string{
		"classes/AccountTest.cls": "@IsTest private class AccountTest {}\n",
		"classes/Account.cls":     "public class Account {}\n",
	})
	second := newInventoryRepository(t, map[string]string{
		"classes/Contact.cls": "public class Contact {}\n",
	})
	firstCommit := testGitOutput(t, first, "rev-parse", "HEAD")
	secondCommit := testGitOutput(t, second, "rev-parse", "HEAD")

	input := writeInventoryInput(t, InventorySpec{
		SchemaVersion: 1,
		Scope:         "private-corpus-assurance",
		Repositories: []InventoryEntry{
			{ID: "private-corpus-002", CheckoutPath: second, ExpectedCommit: secondCommit},
			{ID: "private-corpus-001", CheckoutPath: first, ExpectedCommit: firstCommit},
		},
	})
	output := filepath.Join(t.TempDir(), "prepared")

	manifest, err := PrepareInventory(input, assuranceAttemptForTest(t, input), output)
	if err != nil {
		t.Fatalf("PrepareInventory: %v", err)
	}
	if got, want := manifest.InventorySHA256, sha256FileForTest(t, input); got != want {
		t.Fatalf("inventory sha = %q, want %q", got, want)
	}
	if got, want := repositoryIDs(manifest.Repositories), []string{"private-corpus-001", "private-corpus-002"}; !sameStrings(got, want) {
		t.Fatalf("manifest repository ids = %v, want %v", got, want)
	}

	byID := make(map[string]RepositorySpec, len(manifest.Repositories))
	for _, repo := range manifest.Repositories {
		byID[repo.ID] = repo
		if repo.ArchiveSHA256 == "" || repo.TreeSHA256 == "" {
			t.Fatalf("repository %s has an empty snapshot hash", repo.ID)
		}
		if filepath.IsAbs(repo.SnapshotPath) {
			t.Fatalf("repository %s has absolute snapshot path %q", repo.ID, repo.SnapshotPath)
		}
	}
	if got := byID["private-corpus-001"].AssignedHost; got != "local" {
		t.Fatalf("private-corpus-001 host = %q, want local", got)
	}
	if got := byID["private-corpus-002"].AssignedHost; got != "casper" {
		t.Fatalf("private-corpus-002 host = %q, want casper", got)
	}
	if got := byID["private-corpus-001"].LocalTests; got != "required" {
		t.Fatalf("private-corpus-001 local tests = %q, want required", got)
	}
	if repo := byID["private-corpus-002"]; repo.LocalTests != "tests-not-present" || repo.LocalTestsReason == "" {
		t.Fatalf("private-corpus-002 local test state = %#v, want reason-coded tests-not-present", repo)
	}

	rootPath := filepath.Join(output, "MANIFEST.json")
	rootSHA := sha256FileForTest(t, rootPath)
	for _, host := range []string{"local", "casper"} {
		path := filepath.Join(output, "hosts", host, "manifest.json")
		var hostManifest HostManifest
		readJSONForTest(t, path, &hostManifest)
		if hostManifest.Host != host {
			t.Fatalf("host manifest host = %q, want %q", hostManifest.Host, host)
		}
		if hostManifest.RootManifestSHA256 != rootSHA {
			t.Fatalf("host manifest root sha = %q, want %q", hostManifest.RootManifestSHA256, rootSHA)
		}
		for _, repo := range hostManifest.Repositories {
			if repo.AssignedHost != host {
				t.Fatalf("host %s includes repository assigned to %s", host, repo.AssignedHost)
			}
			if err := verifyRepositorySnapshot(filepath.Join(output, "hosts", host), repo); err != nil {
				t.Fatalf("verify host %s repository %s: %v", host, repo.ID, err)
			}
		}
	}
}

func TestInventoryRejectsDirtyOrWrongCommitRepository(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		mutate func(t *testing.T, repository string, entry *InventoryEntry)
	}{
		{
			name: "dirty tracked file",
			mutate: func(t *testing.T, repository string, _ *InventoryEntry) {
				t.Helper()
				writeFixtureFile(t, repository, "classes/Tracked.cls", "public class Tracked { Integer value; }\n")
			},
		},
		{
			name: "untracked file",
			mutate: func(t *testing.T, repository string, _ *InventoryEntry) {
				t.Helper()
				writeFixtureFile(t, repository, "classes/Untracked.cls", "public class Untracked {}\n")
			},
		},
		{
			name: "wrong commit",
			mutate: func(t *testing.T, _ string, entry *InventoryEntry) {
				t.Helper()
				entry.ExpectedCommit = strings.Repeat("0", 40)
			},
		},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			repository := newInventoryRepository(t, map[string]string{"classes/Tracked.cls": "public class Tracked {}\n"})
			entry := InventoryEntry{ID: "private-corpus-001", CheckoutPath: repository, ExpectedCommit: testGitOutput(t, repository, "rev-parse", "HEAD")}
			scenario.mutate(t, repository, &entry)
			input := writeInventoryInput(t, InventorySpec{SchemaVersion: 1, Scope: "private-corpus-assurance", Repositories: []InventoryEntry{entry}})
			if _, err := PrepareInventory(input, assuranceAttemptForTest(t, input), filepath.Join(t.TempDir(), "prepared")); err == nil {
				t.Fatal("PrepareInventory accepted an unsafe repository")
			}
		})
	}
}

func TestPrepareInventoryRejectsDuplicateCheckoutRoot(t *testing.T) {
	for _, scenario := range []struct {
		name  string
		alias func(t *testing.T, repository string) string
	}{
		{
			name:  "same path",
			alias: func(_ *testing.T, repository string) string { return repository },
		},
		{
			name: "symlink alias",
			alias: func(t *testing.T, repository string) string {
				t.Helper()
				alias := filepath.Join(t.TempDir(), "repository-alias")
				if err := os.Symlink(repository, alias); err != nil {
					t.Fatal(err)
				}
				return alias
			},
		},
		{
			name:  "subdirectory alias",
			alias: func(_ *testing.T, repository string) string { return filepath.Join(repository, "classes") },
		},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			repository := newInventoryRepository(t, map[string]string{"classes/Only.cls": "public class Only {}\n"})
			commit := testGitOutput(t, repository, "rev-parse", "HEAD")
			input := writeInventoryInput(t, InventorySpec{SchemaVersion: 1, Scope: "private-corpus-assurance", Repositories: []InventoryEntry{
				{ID: "private-corpus-001", CheckoutPath: repository, ExpectedCommit: commit},
				{ID: "private-corpus-002", CheckoutPath: scenario.alias(t, repository), ExpectedCommit: commit},
			}})
			if _, err := PrepareInventory(input, assuranceAttemptForTest(t, input), filepath.Join(t.TempDir(), "prepared")); err == nil {
				t.Fatal("PrepareInventory accepted two aliases for the same checkout")
			}
		})
	}
}

func TestCreateSnapshotArchivesExpectedCommit(t *testing.T) {
	repository := newInventoryRepository(t, map[string]string{"classes/Only.cls": "public class OldVersion {}\n"})
	expectedCommit := testGitOutput(t, repository, "rev-parse", "HEAD")
	writeFixtureFile(t, repository, "classes/Only.cls", "public class NewVersion {}\n")
	gitRun(t, repository, "add", ".")
	gitRun(t, repository, "commit", "--quiet", "-m", "advance head")

	output := t.TempDir()
	if err := os.Mkdir(filepath.Join(output, "archives"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(output, "snapshots"), 0o700); err != nil {
		t.Fatal(err)
	}
	snapshot, err := createSnapshot(output, checkout{entry: InventoryEntry{ID: "private-corpus-001", ExpectedCommit: expectedCommit}, root: repository})
	if err != nil {
		t.Fatalf("createSnapshot: %v", err)
	}
	contents, err := os.ReadFile(filepath.Join(output, snapshot.SnapshotPath, "classes", "Only.cls"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(contents), "public class OldVersion {}\n"; got != want {
		t.Fatalf("snapshot contents = %q, want expected-commit contents %q", got, want)
	}
}

func TestDiscoverLocalTestsUsesCodeTokenBoundaries(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		wantTest string
	}{
		{
			name:     "embedded identifier",
			content:  "private class Marker { void contestMethod() {} }\n",
			wantTest: "tests-not-present",
		},
		{
			name: "comments and strings",
			content: `// @iStEsT
/* TeStMeThOd */
private class Marker { String marker = '@iStEsT TeStMeThOd'; }
`,
			wantTest: "tests-not-present",
		},
		{
			name:     "mixed case annotation",
			content:  "@iStEsT private class Marker {}\n",
			wantTest: "required",
		},
		{
			name:     "mixed case legacy keyword",
			content:  "private class Marker { TeStMeThOd static void verify() {} }\n",
			wantTest: "required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "Marker.cls"), []byte(tt.content), 0o600); err != nil {
				t.Fatal(err)
			}
			got, _, err := discoverLocalTests(root)
			if err != nil {
				t.Fatalf("discoverLocalTests: %v", err)
			}
			if got != tt.wantTest {
				t.Fatalf("local tests = %q, want %q", got, tt.wantTest)
			}
		})
	}
}

func TestInventoryCoverageRejectsMissingExtraAndDuplicateRepositories(t *testing.T) {
	spec := InventorySpec{
		SchemaVersion: 1,
		Scope:         "private-corpus-assurance",
		Repositories: []InventoryEntry{
			{ID: "private-corpus-001", CheckoutPath: "/private/one", ExpectedCommit: strings.Repeat("a", 40)},
			{ID: "private-corpus-002", CheckoutPath: "/private/two", ExpectedCommit: strings.Repeat("b", 40)},
		},
	}
	valid := []RepositorySpec{
		validRepositorySpec("private-corpus-001", strings.Repeat("a", 40)),
		validRepositorySpec("private-corpus-002", strings.Repeat("b", 40)),
	}
	if err := ValidateInventoryCoverage(spec, valid); err != nil {
		t.Fatalf("ValidateInventoryCoverage(valid): %v", err)
	}

	for name, repositories := range map[string][]RepositorySpec{
		"missing spec entry":       {valid[0]},
		"extra discovered project": append(append([]RepositorySpec(nil), valid...), validRepositorySpec("private-corpus-003", strings.Repeat("c", 40))),
		"duplicate project":        {valid[0], valid[0], valid[1]},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateInventoryCoverage(spec, repositories); err == nil {
				t.Fatal("ValidateInventoryCoverage accepted mismatched repositories")
			}
		})
	}
}

func TestSnapshotVerificationRejectsModifiedArchiveAndTree(t *testing.T) {
	repository := newInventoryRepository(t, map[string]string{"classes/Only.cls": "public class Only {}\n"})
	input := writeInventoryInput(t, InventorySpec{SchemaVersion: 1, Scope: "private-corpus-assurance", Repositories: []InventoryEntry{{ID: "private-corpus-001", CheckoutPath: repository, ExpectedCommit: testGitOutput(t, repository, "rev-parse", "HEAD")}}})
	output := filepath.Join(t.TempDir(), "prepared")
	manifest, err := PrepareInventory(input, assuranceAttemptForTest(t, input), output)
	if err != nil {
		t.Fatalf("PrepareInventory: %v", err)
	}
	repo := manifest.Repositories[0]
	if err := verifyRepositorySnapshot(output, repo); err != nil {
		t.Fatalf("verifyRepositorySnapshot(before mutation): %v", err)
	}

	archivePath := filepath.Join(output, "archives", repo.ID+".tar")
	if err := os.WriteFile(archivePath, []byte("not an archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyRepositorySnapshot(output, repo); err == nil {
		t.Fatal("verifyRepositorySnapshot accepted a changed archive")
	}

	if _, err := PrepareInventory(input, assuranceAttemptForTest(t, input), filepath.Join(t.TempDir(), "prepared")); err != nil {
		t.Fatalf("PrepareInventory(second output): %v", err)
	}
	output = filepath.Join(filepath.Dir(output), "tree-prepared")
	manifest, err = PrepareInventory(input, assuranceAttemptForTest(t, input), output)
	if err != nil {
		t.Fatalf("PrepareInventory(tree output): %v", err)
	}
	repo = manifest.Repositories[0]
	writeFixtureFile(t, filepath.Join(output, repo.SnapshotPath), "classes/Only.cls", "public class Only { Integer changed; }\n")
	if err := verifyRepositorySnapshot(output, repo); err == nil {
		t.Fatal("verifyRepositorySnapshot accepted a changed tree")
	}
}

func TestExtractTarRejectsEscapingSymlinkTargets(t *testing.T) {
	for _, test := range []struct {
		name    string
		target  string
		wantErr bool
	}{
		{name: "absolute target", target: "/private/target", wantErr: true},
		{name: "escaping target", target: "../../outside", wantErr: true},
		{name: "internal target", target: "Target.cls"},
	} {
		t.Run(test.name, func(t *testing.T) {
			archivePath := filepath.Join(t.TempDir(), "source.tar")
			writeSymlinkArchive(t, archivePath, test.target)
			destination := filepath.Join(t.TempDir(), "snapshot")
			err := extractTar(archivePath, destination)
			if test.wantErr {
				if err == nil {
					t.Fatalf("extractTar accepted symlink target %q", test.target)
				}
				return
			}
			if err != nil {
				t.Fatalf("extractTar(%q): %v", test.target, err)
			}
			link, err := os.Readlink(filepath.Join(destination, "classes", "Link.cls"))
			if err != nil {
				t.Fatal(err)
			}
			if link != test.target {
				t.Fatalf("link target = %q, want %q", link, test.target)
			}
		})
	}
}

func TestExtractTarNormalizesRegularFileModes(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "source.tar")
	writeRegularArchive(t, archivePath, 0o600)
	destination := filepath.Join(t.TempDir(), "snapshot")
	if err := extractTar(archivePath, destination); err != nil {
		t.Fatalf("extractTar: %v", err)
	}
	info, err := os.Stat(filepath.Join(destination, "classes", "Mode.cls"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o644); got != want {
		t.Fatalf("regular file mode = %04o, want %04o", got, want)
	}
}

func TestAssignRepositoriesUsesNeutralIDRoundRobin(t *testing.T) {
	repositories := []RepositorySpec{
		validRepositorySpec("private-corpus-003", strings.Repeat("c", 40)),
		validRepositorySpec("private-corpus-001", strings.Repeat("a", 40)),
		validRepositorySpec("private-corpus-002", strings.Repeat("b", 40)),
	}
	assigned, err := AssignRepositories(repositories)
	if err != nil {
		t.Fatalf("AssignRepositories: %v", err)
	}
	if got, want := repositoryIDs(assigned), []string{"private-corpus-001", "private-corpus-002", "private-corpus-003"}; !sameStrings(got, want) {
		t.Fatalf("assigned IDs = %v, want %v", got, want)
	}
	if got, want := []string{assigned[0].AssignedHost, assigned[1].AssignedHost, assigned[2].AssignedHost}, []string{"local", "casper", "local"}; !sameStrings(got, want) {
		t.Fatalf("assigned hosts = %v, want %v", got, want)
	}
}

func validRepositorySpec(id, commit string) RepositorySpec {
	return RepositorySpec{ID: id, ExpectedCommit: commit, ArchiveSHA256: strings.Repeat("d", 64), TreeSHA256: strings.Repeat("e", 64), AssignedHost: "local", SnapshotPath: filepath.Join("snapshots", id), LocalTests: "required"}
}

func writeInventoryInput(t *testing.T, spec InventorySpec) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "IN_SCOPE.json")
	if err := WriteNewJSON(path, spec); err != nil {
		t.Fatalf("WriteNewJSON(%s): %v", path, err)
	}
	return path
}

func newInventoryRepository(t *testing.T, files map[string]string) string {
	t.Helper()
	repository := t.TempDir()
	gitRun(t, repository, "init", "--quiet")
	gitRun(t, repository, "config", "user.email", "inventory@example.test")
	gitRun(t, repository, "config", "user.name", "Inventory Test")
	for path, content := range files {
		writeFixtureFile(t, repository, path, content)
	}
	gitRun(t, repository, "add", ".")
	gitRun(t, repository, "commit", "--quiet", "-m", "fixture")
	return repository
}

func writeFixtureFile(t *testing.T, root, relativePath, content string) {
	t.Helper()
	path := filepath.Join(root, relativePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeSymlinkArchive(t *testing.T, path, target string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := tar.NewWriter(file)
	if err := writer.WriteHeader(&tar.Header{Name: "classes/Target.cls", Mode: 0o644, Size: 15, Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("public class X\n")); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteHeader(&tar.Header{Name: "classes/Link.cls", Mode: 0o777, Typeflag: tar.TypeSymlink, Linkname: target}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeRegularArchive(t *testing.T, path string, mode int64) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := tar.NewWriter(file)
	if err := writer.WriteHeader(&tar.Header{Name: "classes/Mode.cls", Mode: mode, Size: 15, Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("public class X\n")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func gitRun(t *testing.T, directory string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", directory}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func testGitOutput(t *testing.T, directory string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", directory}, args...)...)
	output, err := command.Output()
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(output))
}

func sha256FileForTest(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func readJSONForTest(t *testing.T, path string, value any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, value); err != nil {
		t.Fatal(err)
	}
}

func repositoryIDs(repositories []RepositorySpec) []string {
	ids := make([]string, len(repositories))
	for index, repo := range repositories {
		ids[index] = repo.ID
	}
	return ids
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}
