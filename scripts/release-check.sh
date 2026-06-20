#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT}"

git diff --check
go test ./...
go run ./cmd/glade-plugin-compat manifest --json >/tmp/glade-plugin-compat-manifest.json
go run ./cmd/glade-plugin-performance manifest --json >/tmp/glade-plugin-performance-manifest.json
go run ./cmd/glade-plugin-orgpackage manifest --json >/tmp/glade-plugin-orgpackage-manifest.json
OUT_DIR=/tmp/glade-plugin-release TARGETS="$(go env GOOS)/$(go env GOARCH)" CHECK=1 scripts/build-plugin-archives.sh 0.2.0
