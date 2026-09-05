<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/glade-sh/glade/main/site/docs-src/public/logo-mark-topo.svg">
    <img src="https://raw.githubusercontent.com/glade-sh/glade/main/site/docs-src/public/logo-mark-topo-light.svg" alt="Glade logo" width="96" height="96">
  </picture>
</p>

# Glade Tools

First-party plugins and maintenance commands for [Glade](https://glade.sh).

Source is hosted in the public `glade-sh/glade-tools` repository. The module
path remains `github.com/glade-sh/glade/tools` so this repo can import
`github.com/glade-sh/glade/internal/...` packages while it is built beside
`../glade`.

## Choose a plugin

| Plugin | Purpose |
| --- | --- |
| `@glade/performance` | Advisory source and trace-based performance scans |
| `@glade/orgpackage` | Package-contract workflows for projects with package dependencies |
| `@glade/compat` | Maintainer-facing compatibility fixtures, scanners, and evidence reports |

Start with the [product quickstart](https://glade.sh/guide/quickstart). These
plugins are optional; base Glade runs local Apex checks and tests without them.

```bash
glade plugins available
glade plugins install @glade/performance
glade plugins list --json
```

See [installation and trust](https://glade.sh/guide/plugins/install-manage)
and [registry/distribution details](docs/plugin-registry.md). Product and plugin
versions are independent. The public-readiness review exercised performance
0.2.13 with product v0.2.14 on a basic fixture; that does not certify every plugin
workflow or platform. Pin the pairing your team actually validates.

Glade Tools is licensed under the [Apache License 2.0](LICENSE). Unless a path
says otherwise, that license covers this project's source, documentation,
fixtures, generated reports, and first-party plugin binaries. Third-party
material retains its own terms and is not relicensed by this project.

Glade Tools is an independent open-source project. It is not affiliated with,
sponsored by, or endorsed by Salesforce. Salesforce and Apex are trademarks of
Salesforce, Inc.

## Maintenance ownership

This project owns:

- compatibility fixtures and fixture runners
- capability catalogs, dashboards, known-gap reports, and stdlib ledgers
- surface ledger refresh, packet, and post-parity scanners
- example-project and large-corpus readiness scans
- Salesforce docs inventory, catalog reconcile, stub reports, and generated
  maintenance artifacts

`glade-tools` may depend on `../glade`. The `glade` product repo must not depend
on this project.

Current Salesforce surface expansion work is tracked in
[`docs/CAPABILITY_WORK_QUEUE.md`](docs/CAPABILITY_WORK_QUEUE.md). Checked
generated reports live under `docs/generated/`.

The checked Apex compiler catalog and scratch-org comparison workflow are
documented in
[Apex language-rule evidence](docs/apex-language-rules.md).

## Build

```bash
go run ./cmd/glade-tools --help
go run ./cmd/glade-plugin-compat manifest --json
go run ./cmd/glade-plugin-performance manifest --json
go run ./cmd/glade-tools local-tests --project ../glade/testdata/local-tests/basic --json
go run ./cmd/glade-tools dashboard --output ../glade/docs/COMPATIBILITY_DASHBOARD.md
go run ./cmd/glade-tools visualforce summary --project docs/fixtures/visualforce/probe-project --json
go run ./cmd/glade-tools lwc parity --docs "../glade/example-projects/Salesforce Docs Scraper/salesforce-docs-expanded-run/lwc" --check docs/generated/LWC_NATIVE_API_PARITY.md
```

For old scripts, `glade-tools compat <command>` is accepted as a compatibility
alias.

Visualforce scratch-org capture and local renderer diffing use the probe
project under `docs/fixtures/visualforce/probe-project`. The working guide is
[`docs/visualforce-oracle.md`](docs/visualforce-oracle.md), including the
`oaer-probe-max` capture path and phase filters.

LWC native API parity is tracked in
[`docs/generated/LWC_NATIVE_API_PARITY.md`](docs/generated/LWC_NATIVE_API_PARITY.md).
Refresh it from the local Salesforce LWC docs scrape with
`glade-tools lwc parity --docs <lwc-docs> --output docs/generated/LWC_NATIVE_API_PARITY.md`.
Use `--json` for the machine-readable report.

## Tests

The default compat package tests keep the frequent path bounded. They validate
documented fixture JSON, execute a small documented-fixture smoke set, and skip
large local-test readiness fixtures.

Run the full documented fixture sweep when changing fixture runner behavior:

```bash
GLADE_TOOLS_RUN_FULL_COMPAT_FIXTURES=1 go test ./internal/compat -run '^TestRunDocumentedFixtures$' -count=1 -timeout=10m
```

Run the full local-test readiness sweep when changing local Apex test behavior:

```bash
GLADE_TOOLS_RUN_FULL_LOCAL_TEST_FIXTURES=1 go test ./internal/compat -run 'TestRunLocalTests.*FixtureReady|TestCheckLocalTestCorpusFixture' -count=1 -timeout=10m
```

For private corpus baselines, keep checked output redacted by passing
`<label>=<project-root>` entries:

```bash
GLADE_BASELINE_PROJECTS='example-projects/private-corpus-a=/path/to/private/project' node scripts/baseline-local-tests-example-projects.mjs
```

Checked Salesforce coverage output needs an explicit docs input. Use
`--source`, `--inventory`, `--catalog`, or set `GLADE_SALESFORCE_DOCS_SOURCE`:

```bash
GLADE_SALESFORCE_DOCS_SOURCE=/path/to/salesforce-docs go run ./cmd/glade-tools salesforce-coverage --check docs/generated/SALESFORCE_COVERAGE_MANIFEST.json
```

## Compare local-test candidates

`glade compat local-tests compare` compares two compat plugin executables
against the same copied Salesforce project and an external target manifest. It
runs exactly five cold pairs for every target in `AB, BA, AB, BA, AB` order,
requires the base and candidate safety contracts to match, and writes a
deterministic `summary.json` plus the raw result, performance, metrics, and
stderr artifacts beneath a new private output directory.

```bash
glade compat local-tests compare \
  --base-bin ./base/glade-plugin-compat \
  --candidate-bin ./candidate/glade-plugin-compat \
  --project path/to/sfdx-project \
  --out /tmp/glade-local-test-compare \
  --workers 2 \
  --runs 5 \
  --manifest targets.json \
  --json
```

The output directory must not exist. The worker count is explicit, and
`--runs` must be `5`. A schema-1 target manifest supplies unique target IDs and
optional class/method selectors:

```json
{
  "schemaVersion": 1,
  "targets": [
    {"id": "whole-project", "cpuProfile": false},
    {"id": "focused-class", "class": "RefinementServiceTest", "cpuProfile": true}
  ]
}
```

Requested CPU profiles run after the timed samples. They are diagnostic only
and are excluded from comparison timings.

## Generate standard describe packs

Generate the product's deterministic standard-describe catalog, reverse pack,
and Go index from a plain or gzip-compressed describe JSON response:

```bash
mkdir -p ../glade/internal/storage
node scripts/generate-standard-describe-pack.mjs \
  INPUT \
  CATALOG_PACK \
  REVERSE_PACK \
  GO_INDEX
```

Create every output parent first. The generator canonicalizes the input and
publishes all three outputs as one rollback-protected set. Do not hand-edit the
generated files.

## Plugin release rail

`glade-tools` is the source for first-party plugin binaries. Product release
assets go to `downloads.glade.sh`. Plugin assets go to `plugins.glade.sh`. Both
rails can share the same version tag, but they publish to different hosts.

`@glade/compat` keeps its package name for this release. Treat it as
maintainer support tools, fixtures, surface ledgers, and parity scanners rather
than first-run user setup.

```bash
glade plugins install @glade/compat
glade plugins install @glade/performance
glade plugins install @glade/orgpackage
glade compat local-tests --project . --json
glade performance scan --project . --json
```

The performance plugin reads local source, metadata, optional Glade trace JSON,
and optional org/data-shape snapshots:

```bash
glade performance scan --project . --format json
glade performance scan --project . --trace reports/slow.trace.json --top 10
glade performance scan --project . --org-facts reports/org-facts.json --fail-on high
glade performance scan --project . --format sarif > reports/glade-performance.sarif
```

Static findings are leads. Trace-backed findings carry measured local evidence.
Org facts are local JSON snapshots; the plugin does not call Salesforce.

The short aliases still resolve for first-party installs:

```bash
glade plugins install compat
glade plugins install performance
glade plugins install orgpackage
```

During local development, link built binaries instead:

```bash
go build -o ./glade-plugin-compat ./cmd/glade-plugin-compat
go build -o ./glade-plugin-performance ./cmd/glade-plugin-performance
glade plugins link --exec ./glade-plugin-compat
glade plugins link --exec ./glade-plugin-performance
glade compat local-tests --project . --json
glade performance scan --project . --json
```

Release archives come from:

```bash
scripts/release-check.sh
scripts/build-plugin-archives.sh X.Y.Z
```

Bind a built Glade candidate to the composed local Apex release gate with both
variables. The gate runs `check` and `test` across five fixtures and writes the
binary hash, source commits, and exact `9/9` count to
`${TMPDIR:-/tmp}/release-local-apex-summary.json`.

```bash
GLADE_RELEASE_BIN=/path/to/glade GLADE_SOURCE_ROOT=/path/to/glade-source scripts/release-check.sh release
```

The version argument is written into each archive name, archived `plugin.json`,
binary `manifest --json` response, and registry row.
Archive entry order and metadata are fixed so clean reruns for the same source,
version, and target produce identical bytes. Every archive includes this
project's `LICENSE` and `NOTICE`, plus a manifest and license/notice files for
the Go components linked into that plugin binary.

Set `PLUGIN_ASSET_BASE_URL` to write a registry `index.json` next to the
archives. The index uses canonical `@glade/*` names, first-party trust metadata,
platform asset URLs, and archive SHA-256 values.

```bash
OUT_DIR=dist/plugins TARGETS="darwin/arm64 linux/amd64" PLUGIN_ASSET_BASE_URL="https://plugins.glade.sh/vX.Y.Z" scripts/build-plugin-archives.sh X.Y.Z
```

The default public registry is live at
`https://plugins.glade.sh/index.json`. Direct archives and linked executables
remain available for offline, private, and development use.

Published plugin release metadata, notes, and asset names are immutable.
Workflow reruns reuse the existing release, skip an existing asset only when
its bytes match, and fail if the published and candidate bytes differ. Cut a
new version when published plugin bytes or metadata need correction.

See [`docs/plugin-registry.md`](docs/plugin-registry.md) for the private source
and registry endpoint setup.
