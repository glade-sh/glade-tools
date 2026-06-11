#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  echo "usage: scripts/build-plugin-archives.sh <version>"
  exit 0
fi
VERSION="${1:-${VERSION:-0.1.0}}"
OUT_DIR="${OUT_DIR:-$ROOT/dist/plugins}"
TARGETS="${TARGETS:-darwin/arm64 darwin/amd64 linux/arm64 linux/amd64}"

mkdir -p "$OUT_DIR"
rm -f "$OUT_DIR/checksums.txt"

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

build_archive() {
  local name="$1"
  local target="$2"
  local goos="${target%/*}"
  local goarch="${target#*/}"
  local binary="glade-plugin-$name"
  local stage="$OUT_DIR/.stage-$name-$goos-$goarch"
  local archive="$OUT_DIR/${binary}_${VERSION}_${goos}_${goarch}.tar.gz"

  rm -rf "$stage"
  mkdir -p "$stage/bin"

  (
    cd "$ROOT"
    CGO_ENABLED="${CGO_ENABLED:-1}" GOOS="$goos" GOARCH="$goarch" go build -trimpath -o "$stage/bin/$binary" "./cmd/$binary"
  )

  cp "$ROOT/plugins/$name/plugin.json" "$stage/plugin.json"
  (
    cd "$stage"
    {
      printf "%s  %s\n" "$(sha256_file "bin/$binary")" "bin/$binary"
      printf "%s  %s\n" "$(sha256_file "plugin.json")" "plugin.json"
    } > checksums.txt
    COPYFILE_DISABLE=1 tar -czf "$archive" bin plugin.json checksums.txt
  )

  printf "%s  %s\n" "$(sha256_file "$archive")" "$(basename "$archive")" >> "$OUT_DIR/checksums.txt"
  rm -rf "$stage"
}

for target in $TARGETS; do
  build_archive compat "$target"
  build_archive performance "$target"
done

echo "Wrote plugin archives to $OUT_DIR"
