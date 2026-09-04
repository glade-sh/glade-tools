# Contributing to Glade Tools

Use [Issues](https://github.com/glade-sh/glade-tools/issues) for public workflow
questions and minimal reproductions. Use [private vulnerability reporting](https://github.com/glade-sh/glade-tools/security/advisories/new)
for security issues. Never include proprietary source, private package names,
credentials, customer records, or unredacted support bundles.

Send private conduct concerns to [conduct@glade.sh](mailto:conduct@glade.sh).
This designated launch alias must be verified before these instructions are
published.

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
Keep private evidence outside public commits. Contributions intentionally
submitted for inclusion are licensed under the [Apache License 2.0](LICENSE),
as described in section 5 of that license. Do not submit copied third-party or
proprietary material unless its provenance and compatible terms are recorded.
