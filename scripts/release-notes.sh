#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
	echo "usage: scripts/release-notes.sh <version>" >&2
	exit 2
fi

version="$1"

notes="$(
	cat <<EOF
Glade tools ${version} ships first-party Glade plugin archives.

Release artifacts include compat, orgpackage, and performance plugin archives
for macOS and Linux on amd64 and arm64, checksums, index.json, and the GitHub
release manifests used before publishing to plugins.glade.sh.
EOF
)"

if [[ "${notes}" == *\\n* ]]; then
	echo "release notes for ${version} contain a literal \\n sequence" >&2
	exit 1
fi

printf '%s\n' "${notes}"
