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

## Plugin migration

`glade-tools` remains for one migration release. New installs should use the
first-party plugin binaries:

```bash
glade plugins install compat
glade plugins install performance
glade compat local-tests --project . --json
glade performance scan --project . --json
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
