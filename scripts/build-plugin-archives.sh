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

case "$VERSION" in
  ""|"."|".."|*..*|*/*|*\\*|*[!A-Za-z0-9._-]*)
    echo "invalid plugin version: $VERSION" >&2
    exit 1
    ;;
esac

mkdir -p "$OUT_DIR"
rm -f "$OUT_DIR/checksums.txt"
rm -f "$OUT_DIR/index.json"

INDEX_ROWS=()

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
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
    COPYFILE_DISABLE=1 tar -czf "$archive" bin plugin.json checksums.txt
  )

  local archive_sum
  archive_sum="$(sha256_file "$archive")"
  printf "%s  %s\n" "$archive_sum" "$(basename "$archive")" >> "$OUT_DIR/checksums.txt"
  INDEX_ROWS+=("$name|$VERSION|$goos|$goarch|$(basename "$archive")|$archive_sum")
  rm -rf "$stage"
}

for target in $TARGETS; do
  build_archive compat "$target"
  build_archive performance "$target"
done

if [[ -n "${PLUGIN_ASSET_BASE_URL:-}" ]]; then
  {
    printf '{\n  "version": 1,\n  "plugins": [\n'
    for plugin_name in compat performance; do
      case "$plugin_name" in
        compat)
          canonical="@glade/compat"
          aliases='["compat"]'
          summary="Compatibility fixtures, surface ledgers, and maintenance scanners."
          commands='["compat","surface","matrix","mvp","local-tests","post-parity","examples","replay","ui-controllers","server-examples","dashboard","gaps","stdlib","docs-inventory","catalog","reconcile","doc-contracts","salesforce-coverage","standard-objects","stub-contracts","stub-behavior","stub-inventory","product-namespaces","tooling-fixtures","evidence"]'
          docs="https://glade.sh/guide/plugins/first-party"
          ;;
        performance)
          canonical="@glade/performance"
          aliases='["performance"]'
          summary="Advisory Salesforce performance scanner."
          commands='["performance"]'
          docs="https://glade.sh/guide/plugins/first-party"
          ;;
      esac
      [[ "$plugin_name" == "compat" ]] || printf ',\n'
      printf '    {\n'
      printf '      "name": "%s",\n' "$canonical"
      printf '      "aliases": %s,\n' "$aliases"
      printf '      "version": "%s",\n' "$VERSION"
      printf '      "publisher": "glade",\n'
      printf '      "trust": "first-party",\n'
      printf '      "summary": "%s",\n' "$summary"
      printf '      "commands": %s,\n' "$commands"
      printf '      "docsURL": "%s",\n' "$docs"
      printf '      "sourceURL": "https://github.com/glade-sh/glade-tools",\n'
      printf '      "minimumGladeVersion": "0.1.0",\n'
      printf '      "assets": [\n'
      first_asset=1
      for row in "${INDEX_ROWS[@]}"; do
        IFS='|' read -r row_name row_version row_goos row_goarch row_archive row_sum <<< "$row"
        [[ "$row_name" == "$plugin_name" ]] || continue
        [[ "$first_asset" -eq 1 ]] || printf ',\n'
        first_asset=0
        printf '        {"os":"%s","arch":"%s","url":"%s/%s","sha256":"%s"}' "$row_goos" "$row_goarch" "${PLUGIN_ASSET_BASE_URL%/}" "$row_archive" "$row_sum"
      done
      printf '\n      ]\n'
      printf '    }'
    done
    printf '\n  ]\n}\n'
  } > "$OUT_DIR/index.json"
fi

echo "Wrote plugin archives to $OUT_DIR"
