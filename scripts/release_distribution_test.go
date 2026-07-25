package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCIWorkflowResolvesGladeRefBeforeCheckout(t *testing.T) {
	workflowPath := filepath.Join("..", ".github", "workflows", "ci.yml")
	workflow, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("read %s: %v", workflowPath, err)
	}
	workflowText := string(workflow)
	for _, want := range []string{
		"Resolve glade ref",
		"scripts/resolve-sibling-ref.sh",
		"steps.glade-ref.outputs.ref",
	} {
		if !strings.Contains(workflowText, want) {
			t.Fatalf("ci.yml missing %q", want)
		}
	}
	if strings.Contains(workflowText, "ref: ${{ startsWith(github.ref, 'refs/tags/') && github.ref_name || 'main' }}") {
		t.Fatalf("ci.yml should not require every glade-tools tag to exist in glade")
	}
}

func TestReleaseWorkflowResolvesGladeRefAndUsesNotesFile(t *testing.T) {
	workflowPath := filepath.Join("..", ".github", "workflows", "release.yml")
	workflow, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("read %s: %v", workflowPath, err)
	}
	workflowText := string(workflow)
	for _, want := range []string{
		"Resolve glade ref",
		"scripts/resolve-sibling-ref.sh",
		"steps.glade-ref.outputs.ref",
		"Build release notes",
		`scripts/release-notes.sh "$GITHUB_REF_NAME" > release-notes.md`,
		"--notes-file release-notes.md",
	} {
		if !strings.Contains(workflowText, want) {
			t.Fatalf("release.yml missing %q", want)
		}
	}
	if strings.Contains(workflowText, `--notes "`) {
		t.Fatalf("release.yml should not publish inline release notes")
	}
}

func TestReleaseWorkflowDoesNotOverwritePublishedAssets(t *testing.T) {
	workflowPath := filepath.Join("..", ".github", "workflows", "release.yml")
	workflow, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("read %s: %v", workflowPath, err)
	}
	workflowText := string(workflow)

	if strings.Contains(workflowText, "gh release edit") {
		t.Fatal("release workflow must not mutate an existing release")
	}
	uploads := releaseWorkflowCommands(workflowText, "upload")
	for _, command := range uploads {
		if strings.Contains(command, "--clobber") {
			t.Fatalf("release upload can replace a published asset: %q", command)
		}
	}

	prepare := releaseWorkflowJobBlock(t, workflowText, "prepare", "build")
	if !strings.Contains(prepare, `gh release view "$GITHUB_REF_NAME" >/dev/null 2>&1`) ||
		!strings.Contains(prepare, `echo "Release $GITHUB_REF_NAME already exists; reusing it without mutation"`) {
		t.Fatal("prepare job must explicitly reuse an existing release without changing it")
	}
	if got := strings.Count(prepare, "gh release create"); got != 1 {
		t.Fatalf("prepare release create count = %d, want 1", got)
	}
	for _, want := range []string{`--title "$GITHUB_REF_NAME"`, "--notes-file release-notes.md"} {
		if !strings.Contains(prepare, want) {
			t.Fatalf("new release creation missing checked release metadata %q", want)
		}
	}

	build := releaseWorkflowJobBlock(t, workflowText, "build", "publish")
	publish := releaseWorkflowJobBlockUntilEnd(t, workflowText, "publish")
	buildUploads := releaseWorkflowCommands(build, "upload")
	if len(buildUploads) != 1 {
		t.Fatalf("platform upload command count = %d, want 1", len(buildUploads))
	}
	requireExactReleaseWorkflowAssets(t, releaseWorkflowUploadAssets(buildUploads[0]), []string{
		"dist/plugins/glade-plugin-compat_${VERSION#v}_${{ matrix.asset_suffix }}.tar.gz",
		"dist/plugins/glade-plugin-orgpackage_${VERSION#v}_${{ matrix.asset_suffix }}.tar.gz",
		"dist/plugins/glade-plugin-performance_${VERSION#v}_${{ matrix.asset_suffix }}.tar.gz",
		"dist/plugins/checksums-${{ matrix.artifact }}.txt",
	})
	if got := strings.Count(build, "asset_suffix:"); got != 4 {
		t.Fatalf("platform matrix asset suffix count = %d, want 4", got)
	}
	matrixRows := []struct {
		target, artifact, assetSuffix string
	}{
		{"linux/amd64", "linux-amd64", "linux_amd64"},
		{"linux/arm64", "linux-arm64", "linux_arm64"},
		{"darwin/amd64", "darwin-amd64", "darwin_amd64"},
		{"darwin/arm64", "darwin-arm64", "darwin_arm64"},
	}
	for _, row := range matrixRows {
		want := strings.Join([]string{
			"target: " + row.target,
			"            artifact: " + row.artifact,
			"            asset_suffix: " + row.assetSuffix,
		}, "\n")
		if !strings.Contains(build, want) {
			t.Fatalf("platform matrix missing target/artifact/asset suffix ownership %q", want)
		}
	}

	publishUploads := releaseWorkflowCommands(publish, "upload")
	if len(publishUploads) != 1 {
		t.Fatalf("final publish upload command count = %d, want 1", len(publishUploads))
	}
	requireExactReleaseWorkflowAssets(t, releaseWorkflowUploadAssets(publishUploads[0]), []string{
		"dist/plugins/checksums.txt",
		"dist/plugins/index.json",
	})
	downloads := releaseWorkflowCommands(publish, "download")
	if len(downloads) != 1 || !strings.Contains(downloads[0], "--clobber") {
		t.Fatal("local release download command must retain --clobber for a clean workspace")
	}
	if strings.Contains(publish, "find dist/plugins") {
		t.Fatal("final publish must name its aggregate assets explicitly")
	}
}

func releaseWorkflowCommands(workflow, operation string) []string {
	prefix := "gh release " + operation
	var commands []string
	var command []string
	for _, line := range strings.Split(workflow, "\n") {
		trimmed := strings.TrimSpace(line)
		if command == nil {
			if strings.HasPrefix(trimmed, prefix+" ") {
				command = append(command, trimmed)
			}
			continue
		}
		command = append(command, trimmed)
		if !strings.HasSuffix(strings.TrimRight(trimmed, " \t"), "\\") {
			commands = append(commands, strings.Join(command, "\n"))
			command = nil
		}
	}
	if len(command) > 0 {
		commands = append(commands, strings.Join(command, "\n"))
	}
	return commands
}

func releaseWorkflowUploadAssets(command string) []string {
	var assets []string
	for _, line := range strings.Split(command, "\n") {
		asset := strings.TrimSpace(line)
		asset = strings.TrimSpace(strings.TrimSuffix(asset, "\\"))
		if strings.HasPrefix(asset, "dist/") {
			assets = append(assets, asset)
		}
	}
	return assets
}

func requireExactReleaseWorkflowAssets(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("release upload asset count = %d, want %d; got %#v", len(got), len(want), got)
	}
	wanted := make(map[string]struct{}, len(want))
	for _, asset := range want {
		wanted[asset] = struct{}{}
	}
	for _, asset := range got {
		if _, exists := wanted[asset]; !exists {
			t.Fatalf("release upload includes unexpected asset %q; got %#v", asset, got)
		}
		delete(wanted, asset)
	}
	if len(wanted) > 0 {
		t.Fatalf("release upload omits assets %#v", wanted)
	}
}

func releaseWorkflowJobBlock(t *testing.T, workflow, start, end string) string {
	t.Helper()
	startMarker := "  " + start + ":"
	startAt := strings.Index(workflow, startMarker)
	if startAt < 0 {
		t.Fatalf("workflow missing job %q", start)
	}
	endMarker := "\n  " + end + ":"
	endAt := strings.Index(workflow[startAt:], endMarker)
	if endAt < 0 {
		t.Fatalf("workflow job %q missing following job %q", start, end)
	}
	return workflow[startAt : startAt+endAt]
}

func releaseWorkflowJobBlockUntilEnd(t *testing.T, workflow, start string) string {
	t.Helper()
	startMarker := "  " + start + ":"
	startAt := strings.Index(workflow, startMarker)
	if startAt < 0 {
		t.Fatalf("workflow missing job %q", start)
	}
	return workflow[startAt:]
}

func TestResolveSiblingRefScript(t *testing.T) {
	remoteWithTag := makeGitRemote(t, "v9.9.9")
	if got := runResolveSiblingRef(t, remoteWithTag, "v9.9.9", "main"); got != "v9.9.9" {
		t.Fatalf("tagged remote resolved %q, want v9.9.9", got)
	}

	remoteWithoutTag := makeGitRemote(t, "")
	if got := runResolveSiblingRef(t, remoteWithoutTag, "v9.9.9", "main"); got != "main" {
		t.Fatalf("untagged remote resolved %q, want main", got)
	}
}

func TestReleaseNotesScriptProducesRealLineBreaks(t *testing.T) {
	cmd := exec.Command("bash", "release-notes.sh", "v9.9.9")
	cmd.Dir = "."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("release-notes.sh v9.9.9 failed: %v\n%s", err, out)
	}
	notes := string(out)
	for _, want := range []string{
		"Glade tools v9.9.9 ships first-party Glade plugin archives.",
		"Release artifacts include",
		"macOS and Linux",
	} {
		if !strings.Contains(notes, want) {
			t.Fatalf("release notes missing %q\n%s", want, notes)
		}
	}
	if strings.Contains(notes, `\n`) {
		t.Fatalf("release notes contain a literal backslash-n sequence:\n%s", notes)
	}
}

func runResolveSiblingRef(t *testing.T, remote, requested, fallback string) string {
	t.Helper()
	outputPath := filepath.Join(t.TempDir(), "github-output")
	cmd := exec.Command("bash", "resolve-sibling-ref.sh", remote, requested, fallback)
	cmd.Dir = "."
	cmd.Env = append(os.Environ(), "GITHUB_OUTPUT="+outputPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("resolve-sibling-ref.sh failed: %v\n%s", err, out)
	}
	got := strings.TrimSpace(string(out))
	outputBytes, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read GITHUB_OUTPUT: %v", err)
	}
	if want := "ref=" + got; !strings.Contains(string(outputBytes), want) {
		t.Fatalf("GITHUB_OUTPUT missing %q in %q", want, outputBytes)
	}
	return got
}

func makeGitRemote(t *testing.T, tag string) string {
	t.Helper()
	root := t.TempDir()
	work := filepath.Join(root, "work")
	if err := os.Mkdir(work, 0o755); err != nil {
		t.Fatalf("mkdir work repo: %v", err)
	}
	runGit(t, work, "init")
	runGit(t, work, "config", "user.email", "glade-test@example.com")
	runGit(t, work, "config", "user.name", "Glade Test")
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("# test\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGit(t, work, "add", "README.md")
	runGit(t, work, "commit", "-m", "initial")
	runGit(t, work, "branch", "-M", "main")
	if tag != "" {
		runGit(t, work, "tag", tag)
	}
	remote := filepath.Join(root, "remote.git")
	runCommand(t, "", "git", "clone", "--bare", work, remote)
	return remote
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	runCommand(t, dir, "git", args...)
}

func runCommand(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s failed: %v\n%s", name, strings.Join(args, " "), err, out)
	}
}
