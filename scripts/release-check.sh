#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT}"

git diff --check
if [[ ! -d "${ROOT}/../glade" ]]; then
	echo "glade sibling repo not found at ${ROOT}/../glade" >&2
	exit 1
fi
# Keep the release gate bounded to packages whose current-base fixtures and
# provenance are checked into this repository. surfaceledger also contains
# archive/worktree evidence probes; run that maintenance suite when its
# external evidence roots are mounted rather than treating absent evidence as
# a current-base parity failure.
go test \
	./internal/apexdocs \
	./internal/apexrules \
	./internal/capability \
	./internal/compat \
	./internal/corpusassurance \
	./internal/corpuscheck \
	./internal/examplescan \
	./internal/lwcparity \
	./internal/metadata \
	./internal/oracleprobe \
	./internal/orgpackage \
	./internal/perfscan \
	./internal/perftool \
	./internal/producttestverify \
	./internal/projectscan \
	./internal/uicontroller \
	./internal/toolcli \
	./scripts
go run ./cmd/glade-plugin-compat manifest --json >/tmp/glade-plugin-compat-manifest.json
go run ./cmd/glade-plugin-performance manifest --json >/tmp/glade-plugin-performance-manifest.json
go run ./cmd/glade-plugin-orgpackage manifest --json >/tmp/glade-plugin-orgpackage-manifest.json
OUT_DIR=/tmp/glade-plugin-release TARGETS="$(go env GOOS)/$(go env GOARCH)" CHECK=1 scripts/build-plugin-archives.sh 0.2.0
