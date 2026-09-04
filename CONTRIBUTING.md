# Contributing to Glade Tools

Use [Issues](https://github.com/glade-sh/glade-tools/issues) for public workflow
questions and minimal reproductions. Use [private vulnerability reporting](https://github.com/glade-sh/glade-tools/security/advisories/new)
for security issues. Never include proprietary source, private package names,
credentials, customer records, or unredacted support bundles.

Follow [Glade's contribution and evidence rules](https://github.com/glade-sh/glade/blob/main/CONTRIBUTING.md).
This repository owns compatibility fixtures/scanners, catalogs, reports, and
first-party plugin sources; the product owns runtime/CLI behavior. Tools may
depend on Glade, never the reverse. Keep an owned Glade checkout beside Tools
when building from source.

Reproduce a bug with an owned/public fixture before fixing it. Run the changed
package first, for example `go test ./internal/capability -count=1`, then
`go test ./scripts -count=1` for distribution changes. Use the existing release
checks for integrated validation. Do not launch Salesforce/private-corpus
campaigns or claim parity from local-only checks.

Report exact input SHAs, commands, denominators, and test results in the PR.
Keep private evidence outside public commits. License terms for this repository
are pending owner clarification; do not assume the product's license grants
rights to this separate source/distribution.
