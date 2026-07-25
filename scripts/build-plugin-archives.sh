#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  echo "usage: scripts/build-plugin-archives.sh [--check] <version>"
  exit 0
fi
CHECK="${CHECK:-0}"
if [[ "${1:-}" == "--check" ]]; then
  CHECK=1
  shift
fi
VERSION="${1:-${VERSION:-0.1.0}}"
OUT_DIR="${OUT_DIR:-$ROOT/dist/plugins}"
TARGETS="${TARGETS:-darwin/arm64 darwin/amd64 linux/arm64 linux/amd64}"

case "$VERSION" in
  ""|"."|".."|*..*|*/*|*\\*|*[!A-Za-z0-9._-]*)
    echo "invalid plugin version: $VERSION" >&2
    exit 1
    ;;
esac

mkdir -p "$OUT_DIR"
rm -f "$OUT_DIR/checksums.txt"
rm -f "$OUT_DIR/index.json"

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

validate_archive() {
  local archive="$1"
  local binary="$2"
  local listing
  listing="$(tar -tzf "$archive")"
  for required in "bin/$binary" "plugin.json" "checksums.txt"; do
    if [[ "$listing" != *"$required"* ]]; then
      echo "archive $archive missing $required" >&2
      exit 1
    fi
  done
}

write_plugin_manifest() {
  local name="$1"
  local dest="$2"
  awk -v version="$VERSION" '
    !done && $0 ~ /"version"[[:space:]]*:/ {
      sub(/"version"[[:space:]]*:[[:space:]]*"[^"]*"/, "\"version\": \"" version "\"")
      done = 1
    }
    { print }
  ' "$ROOT/plugins/$name/plugin.json" > "$dest"
}

build_archive() {
  local name="$1"
  local target="$2"
  local goos="${target%/*}"
  local goarch="${target#*/}"
  local binary="glade-plugin-$name"
  local stage="$OUT_DIR/.stage-$name-$goos-$goarch"
  local archive="$OUT_DIR/${binary}_${VERSION}_${goos}_${goarch}.tar.gz"
  local ldflags

  rm -rf "$stage"
  mkdir -p "$stage/bin"

  case "$name" in
    compat)
      ldflags="-X github.com/glade-sh/glade/tools/internal/toolcli.pluginVersion=$VERSION"
      ;;
    orgpackage)
      ldflags="-X github.com/glade-sh/glade/tools/internal/toolcli.pluginVersion=$VERSION"
      ;;
    performance)
      ldflags="-X github.com/glade-sh/glade/tools/internal/perftool.pluginVersion=$VERSION"
      ;;
    *)
      echo "unknown plugin: $name" >&2
      exit 1
      ;;
  esac

  (
    cd "$ROOT"
    CGO_ENABLED="${CGO_ENABLED:-1}" GOOS="$goos" GOARCH="$goarch" go build -trimpath -ldflags "$ldflags" -o "$stage/bin/$binary" "./cmd/$binary"
  )

  write_plugin_manifest "$name" "$stage/plugin.json"
  (
    cd "$stage"
    {
      printf "%s  %s\n" "$(sha256_file "bin/$binary")" "bin/$binary"
      printf "%s  %s\n" "$(sha256_file "plugin.json")" "plugin.json"
    } > checksums.txt
    TZ=UTC touch -t 197001010000 "bin" "bin/$binary" plugin.json checksums.txt
    COPYFILE_DISABLE=1 python3 - "$archive" "$binary" <<'PY'
import gzip
import os
import sys
import tarfile

archive_path, binary = sys.argv[1:]
with open(archive_path, "wb") as raw:
    with gzip.GzipFile(fileobj=raw, mode="wb", filename="", mtime=0) as compressed:
        with tarfile.open(fileobj=compressed, mode="w", format=tarfile.USTAR_FORMAT) as archive:
            directory = tarfile.TarInfo("bin/")
            directory.type = tarfile.DIRTYPE
            directory.mode = 0o755
            directory.uid = directory.gid = directory.mtime = 0
            archive.addfile(directory)
            for name in (f"bin/{binary}", "plugin.json", "checksums.txt"):
                info = archive.gettarinfo(name, arcname=name)
                info.uid = info.gid = info.mtime = 0
                info.uname = info.gname = ""
                info.pax_headers = {}
                with open(name, "rb") as source:
                    archive.addfile(info, source)
PY
  )
  validate_archive "$archive" "$binary"

  local archive_sum
  archive_sum="$(sha256_file "$archive")"
  printf "%s  %s\n" "$archive_sum" "$(basename "$archive")" >> "$OUT_DIR/checksums.txt"
  rm -rf "$stage"
}

for target in $TARGETS; do
  build_archive compat "$target"
  build_archive orgpackage "$target"
  build_archive performance "$target"
done
LC_ALL=C sort "$OUT_DIR/checksums.txt" -o "$OUT_DIR/checksums.txt"

if [[ -n "${PLUGIN_ASSET_BASE_URL:-}" ]]; then
  python3 "$ROOT/scripts/build-plugin-registry.py" \
    --version "$VERSION" \
    --asset-base-url "$PLUGIN_ASSET_BASE_URL" \
    --archive-dir "$OUT_DIR" \
    --checksums "$OUT_DIR/checksums.txt" \
    --output "$OUT_DIR/index.json"
fi

if [[ "$CHECK" == "1" ]]; then
  echo "Plugin archive check passed for $TARGETS"
fi
echo "Wrote plugin archives to $OUT_DIR"
