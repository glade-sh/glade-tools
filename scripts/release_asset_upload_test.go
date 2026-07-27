package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseAssetUploadSkipsExistingIdenticalBytes(t *testing.T) {
	root := t.TempDir()
	asset := writeReleaseAssetUploadFile(t, root, "plugin.tar.gz", "same bytes")
	existing := filepath.Join(root, "existing")
	if err := os.Mkdir(existing, 0o755); err != nil {
		t.Fatal(err)
	}
	writeReleaseAssetUploadFile(t, existing, "plugin.tar.gz", "same bytes")
	log := filepath.Join(root, "gh.log")
	command := releaseAssetUploadCommand(t, root, asset, existing, "plugin.tar.gz", log)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("release asset upload: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "already has identical bytes; skipping") {
		t.Fatalf("output = %q, want exact-byte skip", output)
	}
	assertReleaseAssetUploadLog(t, log, "release view", "release download")
	if strings.Contains(readReleaseAssetUploadFile(t, log), "release upload") {
		t.Fatalf("identical asset must not be uploaded:\n%s", readReleaseAssetUploadFile(t, log))
	}
}

func TestReleaseAssetUploadRejectsExistingDifferentBytes(t *testing.T) {
	root := t.TempDir()
	asset := writeReleaseAssetUploadFile(t, root, "plugin.tar.gz", "candidate bytes")
	existing := filepath.Join(root, "existing")
	if err := os.Mkdir(existing, 0o755); err != nil {
		t.Fatal(err)
	}
	writeReleaseAssetUploadFile(t, existing, "plugin.tar.gz", "published bytes")
	log := filepath.Join(root, "gh.log")
	command := releaseAssetUploadCommand(t, root, asset, existing, "plugin.tar.gz", log)
	output, err := command.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "published asset differs") {
		t.Fatalf("release asset upload should reject a differing existing asset; err=%v\n%s", err, output)
	}
	if strings.Contains(readReleaseAssetUploadFile(t, log), "release upload") {
		t.Fatalf("different asset must never be uploaded:\n%s", readReleaseAssetUploadFile(t, log))
	}
}

func TestReleaseAssetUploadHandlesExistingOptionLikeName(t *testing.T) {
	for _, test := range []struct {
		name      string
		candidate string
		published string
		want      string
	}{
		{name: "identical", candidate: "same bytes", published: "same bytes", want: "already has identical bytes; skipping"},
		{name: "different", candidate: "candidate bytes", published: "published bytes", want: "published asset differs"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			asset := writeReleaseAssetUploadFile(t, root, "-e", test.candidate)
			existing := filepath.Join(root, "existing")
			if err := os.Mkdir(existing, 0o755); err != nil {
				t.Fatal(err)
			}
			writeReleaseAssetUploadFile(t, existing, "-e", test.published)
			log := filepath.Join(root, "gh.log")
			command := releaseAssetUploadCommand(t, root, asset, existing, "-e", log)
			output, err := command.CombinedOutput()
			if test.name == "identical" && err != nil {
				t.Fatalf("release asset upload: %v\n%s", err, output)
			}
			if test.name == "different" && err == nil {
				t.Fatalf("release asset upload should reject different bytes:\n%s", output)
			}
			if !strings.Contains(string(output), test.want) {
				t.Fatalf("output = %q, want %q", output, test.want)
			}
			if strings.Contains(readReleaseAssetUploadFile(t, log), "release upload") {
				t.Fatalf("existing option-like asset must not be uploaded:\n%s", readReleaseAssetUploadFile(t, log))
			}
		})
	}
}

func TestReleaseAssetUploadUploadsMissingAssetWithoutClobber(t *testing.T) {
	root := t.TempDir()
	asset := writeReleaseAssetUploadFile(t, root, "plugin.tar.gz", "new bytes")
	log := filepath.Join(root, "gh.log")
	command := releaseAssetUploadCommand(t, root, asset, filepath.Join(root, "existing"), "", log)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("release asset upload: %v\n%s", err, output)
	}
	contents := readReleaseAssetUploadFile(t, log)
	if !strings.Contains(contents, "release upload v9.9.9 "+asset) {
		t.Fatalf("missing asset was not uploaded:\n%s", contents)
	}
	if strings.Contains(contents, "--clobber") {
		t.Fatalf("asset upload must not permit overwrite:\n%s", contents)
	}
}

func TestReleaseAssetUploadRejectsDuplicateBasenames(t *testing.T) {
	for _, name := range []string{"plugin.tar.gz", "-e"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			firstDir := filepath.Join(root, "first")
			secondDir := filepath.Join(root, "second")
			if err := os.MkdirAll(firstDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(secondDir, 0o755); err != nil {
				t.Fatal(err)
			}
			first := writeReleaseAssetUploadFile(t, firstDir, name, "first")
			second := writeReleaseAssetUploadFile(t, secondDir, name, "second")
			command := exec.Command("bash", "release-asset-upload.sh", "v9.9.9", first, second)
			command.Dir = "."
			output, err := command.CombinedOutput()
			if err == nil || !strings.Contains(string(output), "duplicate release asset basename: "+name) {
				t.Fatalf("release asset upload should reject duplicate basenames; err=%v\n%s", err, output)
			}
		})
	}
}

func TestReleaseAssetUploadRejectsLineBreakInBasename(t *testing.T) {
	for _, name := range []string{"plugin\narchive.tar.gz", "plugin\n"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			asset := writeReleaseAssetUploadFile(t, root, name, "bytes")
			command := exec.Command("bash", "release-asset-upload.sh", "v9.9.9", asset)
			command.Dir = "."
			output, err := command.CombinedOutput()
			if err == nil || !strings.Contains(string(output), "release asset basename contains a line break") {
				t.Fatalf("release asset upload should reject line breaks; err=%v\n%s", err, output)
			}
		})
	}
}

func releaseAssetUploadCommand(t *testing.T, root, asset, existing, assets, log string) *exec.Cmd {
	t.Helper()
	fakeGH := filepath.Join(root, "gh")
	fake := `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$MOCK_GH_LOG"
case "$1 $2" in
  "release view")
    printf '%s\n' "${MOCK_GH_ASSETS:-}"
    ;;
  "release download")
    pattern=""
    destination=""
    shift 2
    while (($#)); do
      case "$1" in
        --pattern) pattern="$2"; shift 2 ;;
        --dir) destination="$2"; shift 2 ;;
        *) shift ;;
      esac
    done
    mkdir -p "$destination"
    cp "$MOCK_GH_EXISTING/$pattern" "$destination/$pattern"
    ;;
  "release upload") ;;
  *) echo "unexpected gh command: $*" >&2; exit 2 ;;
esac
`
	if err := os.WriteFile(fakeGH, []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("bash", "release-asset-upload.sh", "v9.9.9", asset)
	command.Dir = "."
	command.Env = append(os.Environ(),
		"GH_BIN="+fakeGH,
		"MOCK_GH_ASSETS="+assets,
		"MOCK_GH_EXISTING="+existing,
		"MOCK_GH_LOG="+log,
	)
	return command
}

func writeReleaseAssetUploadFile(t *testing.T, dir, name, contents string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func readReleaseAssetUploadFile(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(contents)
}

func assertReleaseAssetUploadLog(t *testing.T, path string, wants ...string) {
	t.Helper()
	contents := readReleaseAssetUploadFile(t, path)
	for _, want := range wants {
		if !strings.Contains(contents, want) {
			t.Fatalf("gh log missing %q:\n%s", want, contents)
		}
	}
}
