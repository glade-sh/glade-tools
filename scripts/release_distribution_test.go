package scripts

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
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

func TestReleaseWorkflowPinsCatalogGladeCommitAndUsesNotesFile(t *testing.T) {
	workflowPath := filepath.Join("..", ".github", "workflows", "release.yml")
	workflow, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("read %s: %v", workflowPath, err)
	}
	workflowText := string(workflow)
	for _, want := range []string{
		`RELEASE_TAG: ${{ github.ref_name }}`,
		`TOOLS_SHA: ${{ github.sha }}`,
		"Read pinned glade ref",
		`requested_ref="$(jq -er '.gladeCommit | select(type == "string" and test("^[0-9a-f]{40}$"))' docs/fixtures/apex-language-rules.json)"`,
		`printf 'ref=%s\n' "$requested_ref" >> "$GITHUB_OUTPUT"`,
		"steps.glade-ref.outputs.ref",
		"Verify pinned glade checkout",
		`test "$(git -C ../glade rev-parse HEAD)" = "$(jq -r '.gladeCommit' docs/fixtures/apex-language-rules.json)"`,
		"Build release notes",
		`scripts/release-notes.sh "$RELEASE_TAG" > release-notes.md`,
		"--notes-file release-notes.md",
	} {
		if !strings.Contains(workflowText, want) {
			t.Fatalf("release.yml missing %q", want)
		}
	}
	if strings.Contains(workflowText, `--notes "`) {
		t.Fatalf("release.yml should not publish inline release notes")
	}
	for _, stale := range []string{"scripts/resolve-sibling-ref.sh", "GLADE_REMOTE", "REQUESTED_REF", `startsWith(github.ref, 'refs/tags/') && github.ref_name || 'main'`} {
		if strings.Contains(workflowText, stale) {
			t.Fatalf("release.yml retains mutable Glade fallback %q", stale)
		}
	}
	resolveAt := strings.Index(workflowText, `requested_ref="$(jq -er '.gladeCommit | select(type == "string" and test("^[0-9a-f]{40}$"))' docs/fixtures/apex-language-rules.json)"`)
	checkoutAt := strings.Index(workflowText, "repository: glade-sh/glade")
	verifyAt := strings.Index(workflowText, "Verify pinned glade checkout")
	buildAt := strings.Index(workflowText, "Build plugin archives")
	if resolveAt < 0 || checkoutAt < resolveAt || verifyAt < checkoutAt || buildAt < verifyAt {
		t.Fatal("release build must resolve, check out, and verify the catalog-pinned Glade commit before building")
	}
}

func TestReleaseWorkflowTagPushUsesExactTagAndSHA(t *testing.T) {
	workflow, err := os.ReadFile(filepath.Join("..", ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	workflowText := string(workflow)
	for _, want := range []string{
		`ref: ${{ env.TOOLS_SHA }}`,
		`refs/tags/$RELEASE_TAG^{}`,
		`test "$tag_sha" = "$TOOLS_SHA"`,
		`if: startsWith(github.ref, 'refs/tags/')`,
		`scripts/verify-release-gates.sh "$GITHUB_REPOSITORY" "$TOOLS_SHA" > required-gates.json`,
		`gh release view "$RELEASE_TAG" --json targetCommitish --jq '.targetCommitish'`,
		`--target "$TOOLS_SHA"`,
		`env -u GH_TOKEN -u GITHUB_TOKEN`,
		`https://api.github.com/repos/glade-sh/glade/commits/$requested_ref/check-runs?per_page=100&filter=latest&check_name=Salesforce%20Correctness`,
		`cat "$PUBLIC_CHECK_RUNS_RESPONSE"`,
		`export PUBLIC_CHECK_RUNS_RESPONSE="$public_check_runs_response"`,
		`../glade/scripts/verify-salesforce-check.sh "$requested_ref" "$TOOLS_SHA"`,
	} {
		if !strings.Contains(workflowText, want) {
			t.Fatalf("tag release missing %q", want)
		}
	}
	if strings.Contains(workflowText, `test "$1" = api`) {
		t.Fatal("public check-runs adapter must replay the frozen response without argv parsing")
	}
	for _, retired := range []string{"workflow_dispatch", "v0.2.12", "18dd0e23cb540fdacdaaafa51b69c35d25426436"} {
		if strings.Contains(workflowText, retired) {
			t.Fatalf("tag release retains retired recovery value %q", retired)
		}
	}
}

func TestReleaseWorkflowPublishesOnlyTheVerifiedAssetSet(t *testing.T) {
	workflowPath := filepath.Join("..", ".github", "workflows", "release.yml")
	workflow, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("read %s: %v", workflowPath, err)
	}
	workflowText := string(workflow)

	if strings.Contains(workflowText, "gh release upload") {
		t.Fatal("release workflow must route every asset through checksum-aware release-asset-upload.sh")
	}
	if !strings.Contains(workflowText, "scripts/release-asset-upload.sh") {
		t.Fatal("release workflow must use the checksum-aware release asset uploader")
	}

	prepare := releaseWorkflowJobBlock(t, workflowText, "prepare", "build")
	if !strings.Contains(prepare, `gh release view "$RELEASE_TAG" >/dev/null 2>&1`) ||
		!strings.Contains(prepare, `echo "Release $RELEASE_TAG already exists; reusing it without mutation"`) {
		t.Fatal("prepare job must explicitly reuse an existing release without changing it")
	}
	if got := strings.Count(prepare, "gh release create"); got != 1 {
		t.Fatalf("prepare release create count = %d, want 1", got)
	}
	for _, want := range []string{`--title "$RELEASE_TAG"`, `--target "$TOOLS_SHA"`, "--notes-file release-notes.md", "--draft"} {
		if !strings.Contains(prepare, want) {
			t.Fatalf("new release creation missing checked release metadata %q", want)
		}
	}

	build := releaseWorkflowJobBlock(t, workflowText, "build", "publish")
	publish := releaseWorkflowJobBlockUntilEnd(t, workflowText, "publish")
	buildUploads := releaseWorkflowScriptCommands(build, "scripts/release-asset-upload.sh")
	if len(buildUploads) != 1 {
		t.Fatalf("platform checksum-aware upload command count = %d, want 1", len(buildUploads))
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

	publishUploads := releaseWorkflowScriptCommands(publish, "scripts/release-asset-upload.sh")
	if len(publishUploads) != 1 {
		t.Fatalf("final checksum-aware upload command count = %d, want 1", len(publishUploads))
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
	for _, want := range []string{
		`gh release view "$RELEASE_TAG" --json assets --jq '.assets[].name' | LC_ALL=C sort`,
		`gh release view "$RELEASE_TAG" --json isDraft --jq '.isDraft'`,
		`gh release edit "$RELEASE_TAG" --draft=false`,
		`if [[ "$is_draft" == true ]]; then`,
		`elif [[ "$is_draft" != false ]]; then`,
	} {
		if !strings.Contains(publish, want) {
			t.Fatalf("final publish missing immutable-release guard %q", want)
		}
	}
	if strings.Index(publish, `test "$actual_assets" = "$expected_assets"`) > strings.Index(publish, `gh release view "$RELEASE_TAG" --json isDraft`) {
		t.Fatal("final publish must verify the exact asset set before reading or publishing draft state")
	}
	for _, want := range []string{
		"glade-plugin-compat_${RELEASE_TAG#v}_linux_amd64.tar.gz",
		"glade-plugin-orgpackage_${RELEASE_TAG#v}_linux_amd64.tar.gz",
		"glade-plugin-performance_${RELEASE_TAG#v}_linux_amd64.tar.gz",
		"glade-plugin-compat_${RELEASE_TAG#v}_linux_arm64.tar.gz",
		"glade-plugin-orgpackage_${RELEASE_TAG#v}_linux_arm64.tar.gz",
		"glade-plugin-performance_${RELEASE_TAG#v}_linux_arm64.tar.gz",
		"glade-plugin-compat_${RELEASE_TAG#v}_darwin_amd64.tar.gz",
		"glade-plugin-orgpackage_${RELEASE_TAG#v}_darwin_amd64.tar.gz",
		"glade-plugin-performance_${RELEASE_TAG#v}_darwin_amd64.tar.gz",
		"glade-plugin-compat_${RELEASE_TAG#v}_darwin_arm64.tar.gz",
		"glade-plugin-orgpackage_${RELEASE_TAG#v}_darwin_arm64.tar.gz",
		"glade-plugin-performance_${RELEASE_TAG#v}_darwin_arm64.tar.gz",
		"checksums-linux-amd64.txt",
		"checksums-linux-arm64.txt",
		"checksums-darwin-amd64.txt",
		"checksums-darwin-arm64.txt",
		"checksums.txt",
		"index.json",
	} {
		if !strings.Contains(publish, want) {
			t.Fatalf("final publish expected asset missing %q", want)
		}
	}
}

func TestPluginArchiveBuildIsByteIdenticalAcrossCleanBuilds(t *testing.T) {
	version := "9.9.9"
	target := runtime.GOOS + "/" + runtime.GOARCH
	first := t.TempDir()
	second := t.TempDir()
	buildPluginArchives(t, version, target, first)
	buildPluginArchives(t, version, target, second)

	for _, plugin := range []string{"compat", "orgpackage", "performance"} {
		binary := "glade-plugin-" + plugin
		archiveName := fmt.Sprintf("glade-plugin-%s_%s_%s_%s.tar.gz", plugin, version, runtime.GOOS, runtime.GOARCH)
		firstArchive := filepath.Join(first, archiveName)
		secondArchive := filepath.Join(second, archiveName)
		firstBytes, err := os.ReadFile(firstArchive)
		if err != nil {
			t.Fatalf("read first %s archive: %v", plugin, err)
		}
		secondBytes, err := os.ReadFile(secondArchive)
		if err != nil {
			t.Fatalf("read second %s archive: %v", plugin, err)
		}
		if !bytes.Equal(firstBytes, secondBytes) {
			t.Fatalf("%s archive differs across clean builds", plugin)
		}

		validateDeterministicPluginArchive(t, firstArchive, plugin, binary, version)
	}
}

func TestReleaseCheckSplitsCoreAndReleaseEntrypointsWithoutDirectArchiveBuild(t *testing.T) {
	check, err := os.ReadFile("release-check.sh")
	if err != nil {
		t.Fatalf("read release-check.sh: %v", err)
	}
	checkText := string(check)
	if err := releaseCheckContractError(checkText); err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{
		"node --test scripts/*.test.mjs",
		"go test \"${packages[@]}\"",
		"go test -count=1 ./scripts",
		"go test -count=1 ./internal/surfaceledger -run 'TestCB(23MergedFamilyEvidenceClosesTargetRows|56HostedPolicyCoversOnlyDeclaredServiceEffects|65EventBusAccessLevelSurfaceIDsAreCanonicalAndUnique|65EventBusFixtureExercisesAndMergesAllFourOverloads|193LocalMockCoreFixtureIsExact|206FixtureAndComparisonAreExact)$'",
		"go run ./cmd/glade-plugin-compat manifest --json",
		"go run ./cmd/glade-plugin-performance manifest --json",
		"go run ./cmd/glade-plugin-orgpackage manifest --json",
	} {
		t.Run("rejects commented "+line, func(t *testing.T) {
			if err := releaseCheckContractError(strings.Replace(checkText, line, "# "+line, 1)); err == nil {
				t.Fatalf("release-check.sh accepted commented %q", line)
			}
		})
	}
	t.Run("rejects another shared active command", func(t *testing.T) {
		shared := strings.ReplaceAll(checkText, "git diff --check", "git diff --check\n\t\techo shared")
		if err := releaseCheckContractError(shared); err == nil {
			t.Fatal("release-check.sh accepted another shared active command")
		}
	})
	t.Run("rejects caller-directory-dependent default dispatch", func(t *testing.T) {
		dispatch := `all) "$ROOT/scripts/release-check.sh" core; "$ROOT/scripts/release-check.sh" release ;;`
		if err := releaseCheckContractError(strings.Replace(checkText, dispatch, `all) "$0" core; "$0" release ;;`, 1)); err == nil {
			t.Fatal("release-check.sh accepted caller-directory-dependent default dispatch")
		}
	})
	t.Run("rejects ignored manifest failure", func(t *testing.T) {
		line := "go run ./cmd/glade-plugin-compat manifest --json >/tmp/glade-plugin-compat-manifest.json"
		if err := releaseCheckContractError(strings.Replace(checkText, line, line+" || true", 1)); err == nil {
			t.Fatal("release-check.sh accepted an ignored manifest failure")
		}
	})
}

func releaseCheckContractError(checkText string) error {
	if strings.Contains(checkText, "scripts/build-plugin-archives.sh") {
		return fmt.Errorf("release-check.sh must not directly build plugin archives")
	}
	if strings.Contains(checkText, `[[ -d "${ROOT}/../glade" ]]`) {
		return fmt.Errorf("release-check.sh must not repeat the sibling directory preflight per lane")
	}
	if !strings.Contains(checkText, "case \"${1:-all}\" in") ||
		!strings.Contains(checkText, `all) "$ROOT/scripts/release-check.sh" core; "$ROOT/scripts/release-check.sh" release ;;`) ||
		!strings.Contains(checkText, `*) echo "usage: $0 [all|core|release]" >&2; exit 2 ;;`) {
		return fmt.Errorf("release-check.sh must accept only core, release, and their default union")
	}
	if !strings.Contains(checkText, `[[ "${GLADE_RELEASE_BIN:+set}" == "${GLADE_SOURCE_ROOT:+set}" ]]`) {
		return fmt.Errorf("release-check.sh must require both local Apex gate bindings or neither")
	}
	wantCore := []string{
		"git diff --check",
		"node --test scripts/*.test.mjs",
		`go test "${packages[@]}"`,
		"go test -count=1 ./internal/corpusassurance",
	}
	wantRelease := []string{
		"git diff --check",
		"go test -count=1 ./scripts",
		"go test -count=1 ./internal/surfaceledger -run 'TestCB(23MergedFamilyEvidenceClosesTargetRows|56HostedPolicyCoversOnlyDeclaredServiceEffects|65EventBusAccessLevelSurfaceIDsAreCanonicalAndUnique|65EventBusFixtureExercisesAndMergesAllFourOverloads|193LocalMockCoreFixtureIsExact|206FixtureAndComparisonAreExact)$'",
		"go run ./cmd/glade-plugin-compat manifest --json >/tmp/glade-plugin-compat-manifest.json",
		"go run ./cmd/glade-plugin-performance manifest --json >/tmp/glade-plugin-performance-manifest.json",
		"go run ./cmd/glade-plugin-orgpackage manifest --json >/tmp/glade-plugin-orgpackage-manifest.json",
		`[[ -z "${GLADE_RELEASE_BIN:-}" ]] || "$ROOT/scripts/release-local-apex-check.sh" "$GLADE_RELEASE_BIN" "$GLADE_SOURCE_ROOT"`,
	}
	gotCore := releaseCheckActiveLines(checkText, "core")
	gotRelease := releaseCheckActiveLines(checkText, "release")
	if strings.Join(gotCore, "\n") != strings.Join(wantCore, "\n") || strings.Join(gotRelease, "\n") != strings.Join(wantRelease, "\n") {
		return fmt.Errorf("release-check.sh lane commands = core %q, release %q", gotCore, gotRelease)
	}
	if strings.Join(releaseCheckSharedLines(gotCore, gotRelease), "\n") != "git diff --check" {
		return fmt.Errorf("release-check.sh must share only git diff --check")
	}
	wantPackages := []string{
		"-count=1", "./internal/apexdocs", "./internal/apexrules", "./internal/capability", "./internal/compat",
		"./internal/corpuscheck", "./internal/examplescan", "./internal/lwcparity", "./internal/metadata", "./internal/oracleprobe",
		"./internal/orgpackage", "./internal/perfscan", "./internal/perftool", "./internal/producttestverify", "./internal/projectscan",
		"./internal/uicontroller", "./internal/toolcli",
	}
	var gotPackages []string
	inPackages := false
	for _, line := range strings.Split(checkText, "\n") {
		line = strings.TrimSpace(line)
		if line == "packages=(" {
			inPackages = true
			continue
		}
		if inPackages && line == ")" {
			break
		}
		if inPackages && line != "" && !strings.HasPrefix(line, "#") {
			gotPackages = append(gotPackages, line)
		}
	}
	if strings.Join(gotPackages, "\n") != strings.Join(wantPackages, "\n") {
		return fmt.Errorf("core packages = %q, want %q", gotPackages, wantPackages)
	}
	return nil
}

func releaseCheckLane(checkText, lane string) string {
	start := "\t" + lane + ")\n"
	startAt := strings.Index(checkText, start)
	if startAt < 0 {
		return ""
	}
	section := checkText[startAt+len(start):]
	endAt := strings.Index(section, "\t\t;;")
	if endAt < 0 {
		return ""
	}
	return section[:endAt]
}

func releaseCheckActiveLines(checkText, lane string) (active []string) {
	for _, line := range strings.Split(releaseCheckLane(checkText, lane), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			active = append(active, line)
		}
	}
	return active
}

func releaseCheckSharedLines(first, second []string) (shared []string) {
	for _, line := range first {
		for _, other := range second {
			if line == other {
				shared = append(shared, line)
			}
		}
	}
	return shared
}

func buildPluginArchives(t *testing.T, version, target, output string) {
	t.Helper()
	cmd := exec.Command("bash", "build-plugin-archives.sh", version)
	cmd.Dir = "."
	cmd.Env = append(os.Environ(), "TARGETS="+target, "OUT_DIR="+output)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build plugin archives: %v\n%s", err, out)
	}
}

func validateDeterministicPluginArchive(t *testing.T, archivePath, plugin, binary, version string) {
	t.Helper()
	file, err := os.Open(archivePath)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatalf("open gzip archive: %v", err)
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)

	var names []string
	files := map[string][]byte{}
	for {
		header, err := tarReader.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("read tar archive: %v", err)
		}
		names = append(names, header.Name)
		if !header.ModTime.Equal(time.Unix(0, 0)) || header.Uid != 0 || header.Gid != 0 || header.Uname != "" || header.Gname != "" {
			t.Fatalf("archive member %s has non-deterministic metadata: mtime=%s uid=%d gid=%d user=%q group=%q", header.Name, header.ModTime, header.Uid, header.Gid, header.Uname, header.Gname)
		}
		if header.Typeflag == tar.TypeReg {
			contents, err := io.ReadAll(tarReader)
			if err != nil {
				t.Fatalf("read %s: %v", header.Name, err)
			}
			files[header.Name] = contents
		}
	}
	wantNames := []string{"bin/", "bin/" + binary, "plugin.json", "checksums.txt"}
	if strings.Join(names, "\n") != strings.Join(wantNames, "\n") {
		t.Fatalf("archive members = %v, want %v", names, wantNames)
	}
	if len(files["bin/"+binary]) == 0 {
		t.Fatalf("archive binary %s is empty", binary)
	}
	var manifest struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(files["plugin.json"], &manifest); err != nil {
		t.Fatalf("read plugin manifest: %v", err)
	}
	if manifest.Name != plugin || manifest.Version != version {
		t.Fatalf("manifest = %#v, want name=%q version=%q", manifest, plugin, version)
	}
	wantChecksums := fmt.Sprintf("%x  bin/%s\n%x  plugin.json\n", sha256.Sum256(files["bin/"+binary]), binary, sha256.Sum256(files["plugin.json"]))
	if string(files["checksums.txt"]) != wantChecksums {
		t.Fatalf("archive checksums = %q, want %q", files["checksums.txt"], wantChecksums)
	}
}

func TestReleaseWorkflowHelpersAreExecutable(t *testing.T) {
	for _, path := range []string{"release-asset-upload.sh", "verify-release-gates.sh", "build-plugin-registry.py"} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if info.Mode().Perm()&0o100 == 0 {
			t.Errorf("%s must be executable by the release workflow", path)
		}
	}
}

func TestReleaseRegistryCommandsComeFromPackagedManifests(t *testing.T) {
	workflowPath := filepath.Join("..", ".github", "workflows", "release.yml")
	workflow, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("read %s: %v", workflowPath, err)
	}
	buildScriptPath := "build-plugin-archives.sh"
	buildScript, err := os.ReadFile(buildScriptPath)
	if err != nil {
		t.Fatalf("read %s: %v", buildScriptPath, err)
	}

	for path, contents := range map[string]string{
		workflowPath:    string(workflow),
		buildScriptPath: string(buildScript),
	} {
		if !strings.Contains(contents, "scripts/build-plugin-registry.py") {
			t.Errorf("%s does not use the shared packaged-manifest registry builder", path)
		}
		if strings.Contains(contents, `"commands": ["compat", "surface", "local-tests"`) ||
			strings.Contains(contents, `commands='["compat","lwc","surface"`) {
			t.Errorf("%s still hard-codes compat command roots", path)
		}
	}

	registryBuilderPath := "build-plugin-registry.py"
	registryBuilder, err := os.ReadFile(registryBuilderPath)
	if err != nil {
		t.Fatalf("read %s: %v", registryBuilderPath, err)
	}
	builderText := string(registryBuilder)
	for _, want := range []string{
		"tarfile.open",
		"plugin.json",
		`command["path"][0]`,
		"manifest command roots differ across platform archives",
	} {
		if !strings.Contains(builderText, want) {
			t.Errorf("%s does not enforce %q", registryBuilderPath, want)
		}
	}
}

func TestPluginRegistryBuilderDerivesRootsAndRejectsPlatformDisagreement(t *testing.T) {
	archiveDir := t.TempDir()
	version := "9.9.9"
	linuxArchive := writePluginArchive(t, archiveDir, "compat", version, "linux", "amd64", []string{"surface", "compat", "surface"})
	darwinArchive := writePluginArchive(t, archiveDir, "compat", version, "darwin", "arm64", []string{"compat", "surface"})
	orgpackageArchive := writePluginArchive(t, archiveDir, "orgpackage", version, "linux", "amd64", []string{"orgpackage"})
	performanceArchive := writePluginArchive(t, archiveDir, "performance", version, "linux", "amd64", []string{"performance"})
	writeArchiveChecksums(t, archiveDir, linuxArchive, darwinArchive, orgpackageArchive, performanceArchive)

	output := filepath.Join(archiveDir, "index.json")
	cmd := exec.Command("python3", "build-plugin-registry.py", "--version", version, "--asset-base-url", "https://plugins.example/v9.9.9", "--archive-dir", archiveDir, "--checksums", filepath.Join(archiveDir, "checksums.txt"), "--output", output)
	cmd.Dir = "."
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build-plugin-registry.py should accept equivalent platform manifests: %v\n%s", err, out)
	}
	index, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("read registry index: %v", err)
	}
	if !strings.Contains(string(index), "\"commands\": [\n        \"compat\",\n        \"surface\"\n      ]") {
		t.Fatalf("registry did not write sorted unique command roots:\n%s", index)
	}

	writePluginArchive(t, archiveDir, "compat", version, "darwin", "arm64", []string{"compat", "different"})
	writeArchiveChecksums(t, archiveDir, linuxArchive, darwinArchive, orgpackageArchive, performanceArchive)
	cmd = exec.Command("python3", "build-plugin-registry.py", "--version", version, "--asset-base-url", "https://plugins.example/v9.9.9", "--archive-dir", archiveDir, "--checksums", filepath.Join(archiveDir, "checksums.txt"), "--output", output)
	cmd.Dir = "."
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "manifest command roots differ across platform archives") {
		t.Fatalf("registry builder should reject platform command disagreement; err=%v\n%s", err, out)
	}
}

func TestPluginRegistryBuilderRejectsIncompleteOrUnsafeInputs(t *testing.T) {
	version := "9.9.9"
	archiveDir := t.TempDir()
	compatArchive := writePluginArchive(t, archiveDir, "compat", version, "linux", "amd64", []string{"compat"})
	writeArchiveChecksums(t, archiveDir, compatArchive)
	output := filepath.Join(archiveDir, "index.json")

	for _, check := range []struct {
		name, want string
		args       []string
	}{
		{
			name: "missing first-party archives",
			want: "missing required first-party plugin archives",
			args: []string{"--version", version, "--asset-base-url", "https://plugins.example/v9.9.9", "--archive-dir", archiveDir, "--checksums", filepath.Join(archiveDir, "checksums.txt"), "--output", output},
		},
		{
			name: "invalid version",
			want: "invalid version",
			args: []string{"--version", "9/9/9", "--asset-base-url", "https://plugins.example/v9.9.9", "--archive-dir", archiveDir, "--checksums", filepath.Join(archiveDir, "checksums.txt"), "--output", output},
		},
	} {
		t.Run(check.name, func(t *testing.T) {
			cmd := exec.Command("python3", append([]string{"build-plugin-registry.py"}, check.args...)...)
			cmd.Dir = "."
			out, err := cmd.CombinedOutput()
			if err == nil || !strings.Contains(string(out), check.want) {
				t.Fatalf("registry builder should reject %s; err=%v\n%s", check.name, err, out)
			}
		})
	}
}

func TestPluginRegistryBuilderRejectsUnsafeAssetBaseURL(t *testing.T) {
	version := "9.9.9"
	archiveDir := t.TempDir()
	archives := []string{
		writePluginArchive(t, archiveDir, "compat", version, "linux", "amd64", []string{"compat"}),
		writePluginArchive(t, archiveDir, "orgpackage", version, "linux", "amd64", []string{"orgpackage"}),
		writePluginArchive(t, archiveDir, "performance", version, "linux", "amd64", []string{"performance"}),
	}
	writeArchiveChecksums(t, archiveDir, archives...)

	for _, baseURL := range []string{"", "plugins.example/v9.9.9", "http://plugins.example/v9.9.9"} {
		cmd := exec.Command("python3", "build-plugin-registry.py", "--version", version, "--asset-base-url", baseURL, "--archive-dir", archiveDir, "--checksums", filepath.Join(archiveDir, "checksums.txt"), "--output", filepath.Join(archiveDir, "index.json"))
		cmd.Dir = "."
		out, err := cmd.CombinedOutput()
		if err == nil || !strings.Contains(string(out), "asset base URL") {
			t.Fatalf("registry builder should reject asset base URL %q; err=%v\n%s", baseURL, err, out)
		}
	}
}

func TestPluginRegistryBuilderRefusesToOverwriteOutput(t *testing.T) {
	version := "9.9.9"
	archiveDir := t.TempDir()
	archives := []string{
		writePluginArchive(t, archiveDir, "compat", version, "linux", "amd64", []string{"compat"}),
		writePluginArchive(t, archiveDir, "orgpackage", version, "linux", "amd64", []string{"orgpackage"}),
		writePluginArchive(t, archiveDir, "performance", version, "linux", "amd64", []string{"performance"}),
	}
	writeArchiveChecksums(t, archiveDir, archives...)
	output := filepath.Join(archiveDir, "index.json")
	previous := []byte("{\"published\":true}\n")
	if err := os.WriteFile(output, previous, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("python3", "build-plugin-registry.py", "--version", version, "--asset-base-url", "https://plugins.example/v9.9.9", "--archive-dir", archiveDir, "--checksums", filepath.Join(archiveDir, "checksums.txt"), "--output", output)
	cmd.Dir = "."
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "output already exists") {
		t.Fatalf("registry builder should refuse output overwrite; err=%v\n%s", err, out)
	}
	got, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(previous) {
		t.Fatalf("registry builder changed existing output: got %q, want %q", got, previous)
	}
}

func TestPluginRegistryBuilderRejectsDuplicateManifestAndExtraChecksumRows(t *testing.T) {
	version := "9.9.9"
	for _, check := range []struct {
		name, want string
		prepare    func(t *testing.T, archiveDir string) []string
	}{
		{
			name: "duplicate plugin manifest",
			want: "contains duplicate plugin.json members",
			prepare: func(t *testing.T, archiveDir string) []string {
				return []string{
					writePluginArchiveWithManifestCopies(t, archiveDir, "compat", version, "linux", "amd64", []string{"compat"}, 2),
					writePluginArchive(t, archiveDir, "orgpackage", version, "linux", "amd64", []string{"orgpackage"}),
					writePluginArchive(t, archiveDir, "performance", version, "linux", "amd64", []string{"performance"}),
				}
			},
		},
		{
			name: "extra checksum row",
			want: "checksum rows do not exactly cover plugin archives",
			prepare: func(t *testing.T, archiveDir string) []string {
				return []string{
					writePluginArchive(t, archiveDir, "compat", version, "linux", "amd64", []string{"compat"}),
					writePluginArchive(t, archiveDir, "orgpackage", version, "linux", "amd64", []string{"orgpackage"}),
					writePluginArchive(t, archiveDir, "performance", version, "linux", "amd64", []string{"performance"}),
				}
			},
		},
	} {
		t.Run(check.name, func(t *testing.T) {
			archiveDir := t.TempDir()
			archives := check.prepare(t, archiveDir)
			writeArchiveChecksums(t, archiveDir, archives...)
			if check.name == "extra checksum row" {
				checksums := filepath.Join(archiveDir, "checksums.txt")
				contents, err := os.ReadFile(checksums)
				if err != nil {
					t.Fatalf("read checksums: %v", err)
				}
				if err := os.WriteFile(checksums, append(contents, []byte(strings.Repeat("0", 64)+"  unrelated.tar.gz\n")...), 0o644); err != nil {
					t.Fatalf("write extra checksum: %v", err)
				}
			}

			cmd := exec.Command("python3", "build-plugin-registry.py", "--version", version, "--asset-base-url", "https://plugins.example/v9.9.9", "--archive-dir", archiveDir, "--checksums", filepath.Join(archiveDir, "checksums.txt"), "--output", filepath.Join(archiveDir, "index.json"))
			cmd.Dir = "."
			out, err := cmd.CombinedOutput()
			if err == nil || !strings.Contains(string(out), check.want) {
				t.Fatalf("registry builder should reject %s; err=%v\n%s", check.name, err, out)
			}
		})
	}
}

func writePluginArchive(t *testing.T, dir, plugin, version, goos, goarch string, roots []string) string {
	return writePluginArchiveWithManifestCopies(t, dir, plugin, version, goos, goarch, roots, 1)
}

func writePluginArchiveWithManifestCopies(t *testing.T, dir, plugin, version, goos, goarch string, roots []string, copies int) string {
	t.Helper()
	archiveName := "glade-plugin-" + plugin + "_" + version + "_" + goos + "_" + goarch + ".tar.gz"
	path := filepath.Join(dir, archiveName)
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	defer file.Close()
	gzipWriter := gzip.NewWriter(file)
	defer gzipWriter.Close()
	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	var commands []string
	for _, root := range roots {
		commands = append(commands, `{"path":["`+root+`"]}`)
	}
	manifest := `{"name":"` + plugin + `","version":"` + version + `","commands":[` + strings.Join(commands, ",") + `]}`
	for range copies {
		if err := tarWriter.WriteHeader(&tar.Header{Name: "plugin.json", Mode: 0o644, Size: int64(len(manifest))}); err != nil {
			t.Fatalf("write manifest header: %v", err)
		}
		if _, err := tarWriter.Write([]byte(manifest)); err != nil {
			t.Fatalf("write manifest: %v", err)
		}
	}
	return archiveName
}

func writeArchiveChecksums(t *testing.T, dir string, names ...string) {
	t.Helper()
	var rows []string
	for _, name := range names {
		contents, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read archive %s: %v", name, err)
		}
		sum := sha256.Sum256(contents)
		rows = append(rows, hex.EncodeToString(sum[:])+"  "+name)
	}
	if err := os.WriteFile(filepath.Join(dir, "checksums.txt"), []byte(strings.Join(rows, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write checksums: %v", err)
	}
}

func releaseWorkflowCommands(workflow, operation string) []string {
	return releaseWorkflowCommandsWithPrefix(workflow, "gh release "+operation)
}

func releaseWorkflowScriptCommands(workflow, script string) []string {
	return releaseWorkflowCommandsWithPrefix(workflow, script)
}

func releaseWorkflowCommandsWithPrefix(workflow, prefix string) []string {
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
	commitOutput, err := exec.Command("git", "--git-dir", remoteWithTag, "rev-parse", "refs/heads/main").Output()
	if err != nil {
		t.Fatalf("resolve remote main commit: %v", err)
	}
	commit := strings.TrimSpace(string(commitOutput))
	if got := runResolveSiblingRef(t, remoteWithTag, commit, "main"); got != commit {
		t.Fatalf("pinned commit resolved %q, want %q", got, commit)
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
