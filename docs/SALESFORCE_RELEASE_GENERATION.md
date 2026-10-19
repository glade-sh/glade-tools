# Reproducing Salesforce release inputs

The release contract requires a separately acquired snapshot for every checked
source API version. A version list is not documentation provenance. Keep the
raw public documentation and exporter metadata outside this repository; commit
the generated inventories, manifests and source receipts with their reviewed
classification and routing inputs.

Use the existing `docs-inventory` command and the exporters in
`scripts/salesforce-release`. The source receipt exporter records the assembler
and exporter hashes, Atlas family versions and hashes, inventory hash, and the
limitations of the current-only LWC reference. Release-note routes come from
`generate_release_routes.py` with an inventory, its exported TOC and a reviewed
policy. A generator does not decide whether a feature is supported.

## Public sources

Atlas reference metadata is available at
`https://developer.salesforce.com/docs/get_document/atlas.en-us.<ATLAS>.apexref.meta`.
Use `lightning`, `api_rest`, `api_tooling` and `pages` for the other Atlas
families. The Atlas number is twice the API major version plus 128: API 62,
63, 64 and 68 use Atlas 252, 254, 256 and 264 respectively.

Seasonal release notes use
`https://help.salesforce.com/s/articleView?id=release-notes.salesforce_release_notes.htm&language=en_US&release=<ATLAS>&type=5`.
The current-only LWC reference is
[the Lightning Web Components guide](https://developer.salesforce.com/docs/platform/lwc/guide/reference.html).
Record actual acquisition dates and URLs in the retained scrape metadata.
Do not relabel a current page as a historical snapshot.

## Winter '27 composition

When a family is unpublished at Atlas 264, an explicit carry may bind its
unchanged normalized inventory and source receipt from the immediately
previous GA release. This records documentation provenance only. The ordinary
`salesforce release --check` rechecks anonymous publication metadata and fails
if a carried family is published, unavailable or ambiguous. Unit tests use
fake HTTP clients.

Given retained Atlas and release-note exports, generate the inventory first:

```bash
go run ./scripts/salesforce-release/compose_inventory \
  --atlas "$ATLAS_INPUT" --notes "$NOTES_INPUT" \
  --previous-inventory docs/fixtures/salesforce-docs-inventory-summer-26.json \
  --output "$OUTPUT"
python3 scripts/salesforce-release/compose_winter27.py \
  --atlas "$ATLAS_INPUT" --notes "$NOTES_INPUT" \
  --previous-receipt docs/fixtures/salesforce-release-source-summer-26.json \
  --previous-inventory docs/fixtures/salesforce-docs-inventory-summer-26.json \
  --inventory "$OUTPUT/salesforce-docs-inventory-winter-27.json" \
  --manifest "$OUTPUT/salesforce-release-winter-27.json" \
  --composition-output "$OUTPUT/salesforce-release-composition-winter-27.json" \
  --receipt-output "$OUTPUT/salesforce-release-source-winter-27.json"
```

The composition binds the physical Atlas source, additional reference families,
release notes and exact previous inventory/receipt. It records exclusions for
declarations whose documented API is later than the target. Supplementary Apex
guide, Metadata API and object-reference sources remain acquisition context;
normalized surface inventory covers the six families declared by every checked
release manifest, matching the API 65-67 denominator. Its snapshot hash
identifies that composition, rather than a reconstructed historical source tree.

Review each new surface classification and release-note route before updating
the contract. Documentation-only classifications must not create runtime
rejection gates; runtime rejection requires an explicit non-parity behavior
binding. A changed declaration still requires a behavior binding. Then run
`salesforce release --check` against the exact product checkout. Generation and
classification alone supply no executed correctness credit.

## Winter '27 release-note acquisition, 2026-10-19 UTC

Source: [Salesforce Winter '27 Release Notes](https://help.salesforce.com/s/articleView?id=release-notes.salesforce_release_notes.htm&language=en_US&release=264&type=5).
The existing exporter completed at `2026-10-19T04:55:39Z` with
1,834 seasonal articles, 1,834 successful exports and zero failures. Every
exported record identifies release `264.0.0`; every recorded Markdown hash
was checked before inventory generation.

```bash
python3 scripts/salesforce-release/export_release_notes.py \
  --release 264 --output "$NOTES_OUTPUT" \
  --concurrency 2 --request-delay 0.1
glade-tools docs-inventory --source "$NOTES_OUTPUT" \
  --output docs/fixtures/salesforce-release-notes-inventory-winter-27.json
glade-tools docs-inventory --source "$NOTES_OUTPUT" \
  --check docs/fixtures/salesforce-release-notes-inventory-winter-27.json
```

- Exporter SHA-256: `2778601e07f0b717c07d4c424710f62b9d56328a96a824457763c846dbe4fadf`.
- Retained export metadata SHA-256: `8542b327b9ce0622b5be69cfb496da7f57c663c40ec952ddea9100069364e5dc`.
- Generated inventory SHA-256: `8b56f1c1ab9b819d789ada6b826749d7fd32f51e3e7832835cbf68c7a71f0102`.
- Export dependencies: beautifulsoup4 4.15.0, lxml 6.1.3, markdownify 1.2.3, playwright 1.64.0.

The inventory contains 1,834 documents and 195 extracted members. A fresh TOC
comparison with the earlier 1,701-article candidate found 142 added paths and
nine removed paths. Individual routes require review against these current
articles. Reference-family snapshots and a source receipt remain separate
required contract inputs before release admission.

## Per-item classification review

Export the exact delta, including checked surface corrections, before reviewing
product support. Keep one explicit decision for each canonical surface ID; the
exporter rejects missing, duplicate, stale and wildcard decisions. The review
records the exact public declaration and URL, product implementation or absence
evidence, decision reason and product test bindings. The generated schema-2
classification retains all this evidence in `reasonRef`; it is not proof that
the bound tests ran or that Salesforce parity was observed.

```bash
go run ./scripts/salesforce-release/classify_delta \
  --contract docs/fixtures/salesforce-release-contract.json \
  --release "Winter '27" --delta-output "$DELTA_OUTPUT"
go run ./scripts/salesforce-release/classify_delta \
  --contract docs/fixtures/salesforce-release-contract.json \
  --release "Winter '27" --review "$REVIEW_INPUT" \
  --output "$CLASSIFICATIONS_OUTPUT"
```

For an inventory pair before contract assembly, use `--previous-inventory`,
`--current-inventory`, `--previous-api`, `--current-api`, `--previous-release`
and `--release` instead of `--contract`. Recheck against the assembled contract
before committing, because standalone pairs do not apply surface corrections.
The review object has schema version 1, `previousRelease`, `currentRelease` and
`decisions`. Each decision has `surfaceId`, `scope`, `disposition`, `declaration`,
`sourceURL`, `productEvidence`, `reason` and `productTests`. There are no inferred
support decisions or default classification rules.
