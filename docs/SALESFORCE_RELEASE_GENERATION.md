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

## Bounded reference acquisition

The public Atlas scraper can lose its anonymous session during a long export.
`acquire_versioned_docs.py` calls that same scraper in batches of 100 pages,
with one worker and a fresh anonymous browser session per batch. It resumes
retained successful Markdown pages and stops on any failed or empty page.
It verifies the API/Atlas publication identity and derives the season label
from fetched metadata, avoiding stale hard-coded season labels in the scraper.
Only a complete acquisition writes final root and family snapshot metadata.

```bash
python3 scripts/salesforce-release/acquire_versioned_docs.py \
  --scraper "$PUBLIC_ATLAS_SCRAPER" --atlas 252.0 \
  --output "$RAW_REFERENCE_OUTPUT" \
  --batch-size 100 --concurrency 1 --request-delay 0.5
```

Use Atlas 254.0 and 256.0 for API 63 and 64. For the physical API 68 source,
use Atlas 264.0 with `--families apex tooling-api`; the six-family composer
separately binds explicit previous GA carries. Optional supplementary guides
are included as context only when present and completely exported. The composer
rejects failed, empty, missing or mismatched family page counts before writing
a source receipt. Release notes must have a complete matching export receipt,
exact article versions and unchanged file hashes. Retain `_acquisition-batched.json`, publication JSON, TOCs and family
version metadata outside the repository. The receipt records scraper/wrapper
hashes, acquisition times, publication identities, batch counts and exact missing
URLs on failure. Archive an incomplete receipt under a dated name before retrying;
never relabel or commit an incomplete source as a complete release snapshot.

## Historical seasonal publication revisions

A requested Atlas season can resolve to a revised publication, such as Atlas
252 resolving to release-note version `252.15.0`. The exporter preserves the
exact revision in the public page's anonymous article request and requires every
returned record to match that revision and topic. It rejects a template from a
different Atlas season. Do not replace the revision with an invented `.0.0`
version or accept current-release content for a historical season.

The exporter checks the seasonal root before starting the article queue. Its
progress output distinguishes completed attempts, successful exports and failed
articles. `_export-meta.json` retains the requested Atlas season, exact resolved
release, source URLs, returned versions, publication dates and content hashes.
An error record or a missing version leaves the export incomplete.

## Spring and Summer '25 seasonal notes, 2026-10-19 UTC

The corrected exporter completed the anonymous public
[Spring '25 release notes](https://help.salesforce.com/s/articleView?id=release-notes.salesforce_release_notes.htm&language=en_US&release=254&type=5)
and [Summer '25 release notes](https://help.salesforce.com/s/articleView?id=release-notes.salesforce_release_notes.htm&language=en_US&release=256&type=5)
with zero failures. Before inventory generation, every article hash, exact
resolved version, seasonal URL and topic ID was checked against the complete
export metadata and TOC. Inventories were generated and checked with the same
`docs-inventory` command used for the existing public releases.

```bash
python3 scripts/salesforce-release/export_release_notes.py \
  --release 254 --output "$SPRING_NOTES_OUTPUT" \
  --concurrency 2 --request-delay 0.1
glade-tools docs-inventory --source "$SPRING_NOTES_OUTPUT" \
  --output docs/fixtures/salesforce-release-notes-inventory-spring-25.json
glade-tools docs-inventory --source "$SPRING_NOTES_OUTPUT" \
  --check docs/fixtures/salesforce-release-notes-inventory-spring-25.json
```

Repeat with release 256, the Summer output and
`salesforce-release-notes-inventory-summer-25.json`.

| Input | Resolved version | Completed UTC | Articles | Members |
| --- | --- | --- | ---: | ---: |
| Spring '25 | `254.14.0` | `2026-10-19T05:57:00Z` | 1509 | 32 |
| Summer '25 | `256.12.0` | `2026-10-19T05:57:18Z` | 1550 | 20 |

Exporter SHA-256: `5e0ab9545024ee05826b043a731638e7decddee64e5ac61ee75966dcb31164c2`.
Dependencies: beautifulsoup4 4.15.0, lxml 6.1.3, markdownify 1.2.3, playwright 1.64.0.

- Spring export metadata SHA-256: `0817e5818bbaafc06d21dd2a85c5cd1d191a5383be2f3d5af30d4a7d4d892438`.
- Spring generated inventory SHA-256: `b665e92adbcb32d7472d576861e16e02dc4b70d53005777fa07d6a93594de5bf`.

- Summer export metadata SHA-256: `71c1eefcdc8600f09b586f900437b3e330e6bf3b3828d8e8a261ce81353149be`.
- Summer generated inventory SHA-256: `d37948ca89da3c14fcd6ad54dc85653935007b58f54361af86aef240748e4233`.

These note inventories are separate inputs, not complete GA reference snapshots
or executed correctness proof. Historical and API 68 reference acquisitions
remain incomplete after persistent public content HTTP 403 responses. Winter
'25 notes exported 1,471 of 1,472 TOC articles; the LinkedIn Lead Gen article
returned no versioned record. Its incomplete export is retained externally and
is not admitted as a complete inventory or source receipt.

## ConnectApi acquisition policy

ConnectApi is excluded and unsupported. Do not retry its denied documentation
requests or supply authentication, cookies or header workarounds. Retain the
anonymous HTTP 403 observations and distinguish unavailable references from
acquired declarations. The bounded wrapper can reuse retained public TOCs and
Markdown without requesting them again:

```bash
python3 scripts/salesforce-release/acquire_versioned_docs.py \
  --scraper "$PUBLIC_SCRAPER" --atlas 264.0 --families apex \
  --retained-input "$RETAINED_PUBLIC_INPUT" --output "$NEW_OUTPUT" \
  --failure-evidence "$RETAINED_HTTP_STATUSES" --exclude-connectapi
```

Repeat for Atlas 252.0, 254.0 and 256.0. The retained status input is an array
of `{atlas, url, status}` records; local diagnostic fields are not exported.
Only the `apex_connectapi_*` and `connectAPI_*` public Apex reference filenames
are excluded. Other namespaces remain subject to normal acquisition and review.
Each family keeps its complete TOC, acquired Markdown, `_version.json`, and
`_policy-excluded.json`. The latter names every unavailable reference with
namespace `ConnectApi`, disposition `explicitly-unsupported`, and reason
`excluded and unsupported; documentation not acquired`, plus the retained 403
URLs and status counts. The acquisition receipt binds the TOC, policy ledger,
retained input and HTTP evidence hashes. Acquired and excluded reference counts
must cover the TOC exactly; an exclusion never supplies an invented signature.

Before assembly, verify the retained family coverage and export its policy
classifications using the same canonical surface normalization as the gate:

```bash
python3 scripts/salesforce-release/validate_policy_exclusions.py "$NEW_OUTPUT/apex"
go run ./scripts/salesforce-release/classify_delta \
  --current-inventory "$NORMALIZED_INVENTORY" \
  --namespace-policy-receipt "$NEW_OUTPUT/apex/_policy-excluded.json" \
  --namespace-policy-output "$GENERATED_NAMESPACE_POLICY"
```

The generated namespace policy enumerates **every known ConnectApi surface**,
including unchanged declarations, as `outside-claim` / `explicit-unsupported`.
It also retains the unavailable TOC references and 403 evidence separately.
These records assert an exclusion from product scope, not runtime parity or
knowledge of declarations in pages that were not acquired. Other namespaces
require their own per-item declaration and implementation review. The policy
export is a source input; it does not replace delta classifications, release
routes, complete source receipts, or an executed release check.
