#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT}"

# Keep the release gate bounded to packages whose current-base fixtures and
# provenance are checked into this repository. surfaceledger also contains
# archive/worktree evidence probes; run that maintenance suite when its
# external evidence roots are mounted rather than treating absent evidence as
# a current-base parity failure.
packages=(
	-count=1
	./internal/apexdocs
	./internal/apexrules
	./internal/capability
	./internal/compat
	./internal/corpuscheck
	./internal/examplescan
	./internal/lwcparity
	./internal/metadata
	./internal/oracleprobe
	./internal/orgpackage
	./internal/perfscan
	./internal/perftool
	./internal/producttestverify
	./internal/projectscan
	./internal/uicontroller
	./internal/toolcli
	./internal/corpusassurance
)

case "${1:-all}" in
	core)
		git diff --check
		node --test scripts/*.test.mjs
		go test "${packages[@]}"
		;;
	release)
		git diff --check
		go test -count=1 ./scripts
		go test -count=1 ./internal/surfaceledger -run 'TestCB(23MergedFamilyEvidenceClosesTargetRows|56HostedPolicyCoversOnlyDeclaredServiceEffects|65EventBusAccessLevelSurfaceIDsAreCanonicalAndUnique|65EventBusFixtureExercisesAndMergesAllFourOverloads|193LocalMockCoreFixtureIsExact|206FixtureAndComparisonAreExact)$'
		go run ./cmd/glade-plugin-compat manifest --json >/tmp/glade-plugin-compat-manifest.json
		go run ./cmd/glade-plugin-performance manifest --json >/tmp/glade-plugin-performance-manifest.json
		go run ./cmd/glade-plugin-orgpackage manifest --json >/tmp/glade-plugin-orgpackage-manifest.json
		;;
	all) "$ROOT/scripts/release-check.sh" core; "$ROOT/scripts/release-check.sh" release ;;
	*) echo "usage: $0 [all|core|release]" >&2; exit 2 ;;
esac
