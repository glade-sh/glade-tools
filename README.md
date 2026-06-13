# Glade Tools

Maintenance commands for the sibling `glade` project.

This project owns the shop work:

- compatibility fixtures and fixture runners
- capability catalogs, dashboards, known-gap reports, and stdlib ledgers
- surface ledger refresh, packet, and post-parity scanners
- example-project and large-corpus readiness scans
- Salesforce docs inventory, catalog reconcile, stub reports, and generated
  maintenance artifacts

`glade-tools` may depend on `../glade`. The `glade` product repo must not depend
on this project.

## Build

```bash
go run ./cmd/glade-tools --help
go run ./cmd/glade-plugin-compat manifest --json
go run ./cmd/glade-plugin-performance manifest --json
go run ./cmd/glade-tools local-tests --project ../glade/testdata/local-tests/basic --json
go run ./cmd/glade-tools dashboard --output ../glade/docs/COMPATIBILITY_DASHBOARD.md
```

For old scripts, `glade-tools compat <command>` is accepted as a compatibility
alias.

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

## Plugin migration

`glade-tools` remains for one migration release. New installs should use the
first-party plugin binaries:

```bash
glade plugins install @glade/compat
glade plugins install @glade/performance
glade compat local-tests --project . --json
glade performance scan --project . --json
```

The short aliases still resolve for first-party installs:

```bash
glade plugins install compat
glade plugins install performance
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
scripts/build-plugin-archives.sh 0.1.0
```

The version argument is written into each archive name, archived `plugin.json`,
binary `manifest --json` response, and registry row.

Set `PLUGIN_ASSET_BASE_URL` to write a registry `index.json` next to the
archives. The index uses canonical `@glade/*` names, first-party trust metadata,
platform asset URLs, and archive SHA-256 values.

```bash
OUT_DIR=dist/plugins TARGETS="darwin/arm64 linux/amd64" PLUGIN_ASSET_BASE_URL="https://plugins.glade.sh/v0.1.0" scripts/build-plugin-archives.sh 0.1.0
```
