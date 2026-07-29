# Apex language-rule evidence

`docs/fixtures/apex-language-rules.json` is the checked compiler compatibility
catalog for Glade. It contains 422 checked rows:

- 121 reserved identifiers;
- 301 other compiler controls;
- 68 accept controls; and
- 354 rejection controls.

The current status breakdown is 410 supported rows, 3 confirmed Glade gaps, and
9 oracle-pending rows. A supported row must point to an exact Glade product
regression test. The remaining release controls keep their non-supported status
explicit instead of overstating product compatibility.

The catalog includes `APEX-RESERVED-CURRENCY`, which proves that Salesforce and
Glade reject `currency` as a variable name. Reserved identifier matching is
case-insensitive.

## Row contract

Each row records:

- a stable `id` and language `area`;
- the source or evidence provenance, such as public documentation, an isolated
  scratch result, or a recorded corpus lead;
- the source API version, source kind, complete Apex program, and any dependency
  source files;
- the expected Salesforce `oracle` outcome: `accept` or `reject`;
- the owning Glade subsystem;
- the compatibility `status`; and
- the exact `productTest` that protects supported behavior.

The top-level `gladeCommit` is a full 40-character SHA. It pins the product
checkout whose tests and binary match the catalog. Do not replace it with a
branch name or an unverified newer commit.

## Fast validation

Build Glade Tools beside the pinned Glade checkout so the repository's
`../glade` module replacement resolves. Validate the catalog without contacting
Salesforce:

```bash
go run ./cmd/glade-tools apex-rules validate \
  --catalog docs/fixtures/apex-language-rules.json
```

Validation rejects malformed evidence, duplicate IDs, unknown outcomes or
statuses, supported rows without product tests, and a non-SHA `gladeCommit`.

Routine pull-request CI also resolves the pinned Glade commit, verifies the
checkout SHA, checks every product-test pointer, and runs repository tests. It
has a five-minute job timeout and does not wait for Salesforce authentication
or scratch-org creation.

## Salesforce comparison

Use a Salesforce scratch org when adding rules, refreshing expectations, or
auditing drift:

```bash
(cd ../glade && go build -o /tmp/glade-apex-rules ./cmd/glade)
go run ./cmd/glade-tools apex-rules compare \
  --catalog docs/fixtures/apex-language-rules.json \
  --target-org <scratch-alias> \
  --glade-bin /tmp/glade-apex-rules \
  --json
```

The command sends each program to the Salesforce scratch org, runs the same row
through Glade, and reports:

- `supportedMismatches` when Glade differs from the live Salesforce result; and
- `oracleDrifts` when the current Salesforce result differs from the stored
  outcome.

Either count makes the command fail. An org listing or scratch connection can
take about a minute, so this authenticated comparison is an explicit maintainer
operation rather than part of the routine pull-request gate.

## Updating the catalog

For every added or changed row:

1. Record public Salesforce documentation or an isolated scratch-org result.
2. Store the smallest complete Apex program that proves one rule.
3. Add both rejection and acceptance controls when context changes the result.
4. Add the exact product regression test before marking the row `supported`.
5. Run catalog validation and focused product tests.
6. Run the full Salesforce comparison for changed oracle expectations.
7. Update `gladeCommit` only after the referenced product commit exists.

Do not infer Salesforce behavior from a corpus failure alone. Convert the
candidate into an isolated program and verify it with Salesforce first.
