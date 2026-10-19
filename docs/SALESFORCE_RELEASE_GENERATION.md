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

### Online checks required before publication

The ordinary CLI command below requires anonymous HTTPS access to Salesforce
documentation. Run it against the selected contract and the final product
checkout with that variant's generated-table patches applied:

```bash
glade-tools salesforce release \
  --contract docs/fixtures/salesforce-release-contract.json \
  --glade-root "$GLADE_ROOT" --check
```

Both current variants carry the API 67 (Atlas 262.0) Aura, REST and Visualforce
references into API 68 (Atlas 264.0). The command performs a GET for each actual
current publication endpoint, in this order:

- `https://developer.salesforce.com/docs/get_document/atlas.en-us.lightning.meta`
- `https://developer.salesforce.com/docs/get_document/atlas.en-us.api_rest.meta`
- `https://developer.salesforce.com/docs/get_document/atlas.en-us.pages.meta`

Each response must be HTTP 200, parse as publication JSON, identify Atlas
262.0 (with matching API 67.0 when the structured version form is returned),
and contain a non-null TOC array. Atlas 264.0 being published requires replacing
the carry with a fresh snapshot. A different, absent or ambiguous identity,
empty/invalid response, non-200 status, timeout or network failure fails closed
before the five generated product tables are compared. There is no offline
override; saved receipts, direct generation and injected-client tests do not
replace this online check. The client timeout is 30 seconds per request.

The current LWC input is freshly acquired and has no carry, so these contracts
do not request an LWC freshness check. If an explicit LWC carry is introduced,
the same command also checks
`https://developer.salesforce.com/docs/platform/lwc/guide/reference-configuration-tags.html`
for an unambiguous current API/release marker proving that the prior guide is
still current. Unavailable or unrecognized content fails closed there too.

The tools CI core and default release lanes exercise deterministic checks and
fake HTTP clients; their success does not establish live publication freshness.
The final Salesforce Correctness gate invokes the ordinary `--check` before
product tests and native verification. That run must have access to the three
public endpoints above and separately satisfy its product/native proof gates.
These documentation requests use no org credentials and acquire no excluded
ConnectApi pages. No release-mode, byte-check or proof requirement is waived.

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
or executed correctness proof. ConnectApi-only reference failures are now explicitly policy-excluded; retained
HTTP403 records supply acquisition evidence, not declaration or parity credit. Winter
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

For APIs 62.0, 63.0 and 64.0, assemble the five completed Atlas families with
the existing availability-filtered LWC assembler. Generate the normalized
inventory and canonical manifest with the same inventory library as APIs 65-67:

```bash
python3 scripts/salesforce-release/assemble_versioned_docs.py \
  --source "$VERSIONED_SOURCE" --lwc-source "$CURRENT_LWC_SOURCE" \
  --output "$ASSEMBLED_SNAPSHOT" --target-api-version 62.0
go run ./scripts/salesforce-release/compose_inventory \
  --atlas "$ASSEMBLED_SNAPSHOT" --historical-api 62.0 --output "$OUTPUT"
python3 scripts/salesforce-release/export_source_receipt.py \
  --snapshot "$ASSEMBLED_SNAPSHOT" \
  --inventory "$OUTPUT/salesforce-docs-inventory-winter-25.json" \
  --manifest "$OUTPUT/salesforce-release-winter-25.json" \
  --output "$OUTPUT/salesforce-release-source-winter-25.json"
glade-tools docs-inventory --source "$ASSEMBLED_SNAPSHOT" \
  --check "$OUTPUT/salesforce-docs-inventory-winter-25.json"
```

Use API 63.0 / `spring-25` and API 64.0 / `summer-25` for the other
historical snapshots. Historical mode deliberately cannot compose API 68 or
silently substitute historical release-note inventories.

## API 68 checked source inputs, acquired 2026-10-19 UTC

This contract checks Apex source 62.0-68.0 and discrete endpoint versions
60.0/65.0/66.0/67.0/68.0, with default 65.0 for both axes. Static declaration
classification and generated-table agreement do not establish executed
Salesforce correctness; the analyzer deliberately retains zero execution credit.

Apex: 1,926 acquired pages plus 405 unavailable ConnectApi references cover
all 2,331 TOC references; the retained per-release evidence has 100 ConnectApi
403 URLs. No denied page was retried. Tooling has 347 acquired pages. Aura,
REST and Visualforce bind the exact checked API 67 inventories/receipts, and
normal `--check` rechecks their actual unversioned publication endpoints.

- [Apex source TOC](https://developer.salesforce.com/docs/get_document/atlas.en-us.264.0.apexref.meta).
- [Tooling source TOC](https://developer.salesforce.com/docs/get_document/atlas.en-us.264.0.api_tooling.meta).
- [LWC reference](https://developer.salesforce.com/docs/platform/lwc/guide/reference-api-modules.html).
- [API 68 LWC publication note](https://help.salesforce.com/s/articleView?id=release-notes.rn_lwc_versioning.htm&language=en_US&release=264&type=5).

The earlier nominally successful current-only LWC export contained publisher
Oops pages and is rejected. A normal recapture with the existing scraper
completed all 28 pages with HTTP 200, no empty/failed documents and no publisher
error pages. No cookies, header workarounds or 403 retries were used. Retained
HTTP metadata binds each requested URL, status and fetch time. Its completion
UTC was 2026-10-19T07:03:30.340318Z. LWC remains a current-release-only,
availability-filtered view: the exact seasonal note inventory/hash and note
identity bind its API 68 publication. No Atlas-numbered LWC guide is invented.

```bash
go run ./scripts/salesforce-release/compose_inventory \
  --atlas "$ATLAS_INPUT" --notes "$NOTES_INPUT" \
  --lwc-source "$CURRENT_LWC_ROOT" \
  --previous-inventory docs/fixtures/salesforce-docs-inventory-summer-26.json \
  --output "$OUTPUT"
python3 scripts/salesforce-release/compose_winter27.py \
  --atlas "$ATLAS_INPUT" --notes "$NOTES_INPUT" \
  --lwc-source "$CURRENT_LWC_ROOT/lwc" \
  --notes-inventory docs/fixtures/salesforce-release-notes-inventory-winter-27.json \
  --previous-receipt docs/fixtures/salesforce-release-source-summer-26.json \
  --previous-inventory docs/fixtures/salesforce-docs-inventory-summer-26.json \
  --inventory "$OUTPUT/salesforce-docs-inventory-winter-27.json" \
  --manifest "$OUTPUT/salesforce-release-winter-27.json" \
  --composition-output "$OUTPUT/salesforce-release-composition-winter-27.json" \
  --receipt-output "$OUTPUT/salesforce-release-source-winter-27.json"
```

The normalized inventory has 2929 documents and 5677 members. The
namespace policy explicitly marks all 2,235 acquired known ConnectApi surfaces
unsupported, preserves 2,613 historical API 67 exclusions as historical evidence,
and separately lists all 405 unavailable reference identities. Its inventory
hash and complete current declaration coverage have a dedicated fixture test.

The reviewed API 67-to-68 delta has 626 rows: 119 supported declaration/CBC
cases with exact product tests, and 507 explicit unsupported/not-claimed cases.
Existing implemented declaration shapes use `preserveAvailability` and exact
generated shape subtests; first capture or namespace correction does not invent
an introduction/retirement boundary or assert hosted-service behavior.
Unimplemented QueryOptions, DPoP, JWE/Crypto overloads and new Tooling resources
retain their exact checked declarations, public URLs, product absence evidence,
reasons and structural controls in generated classification data.

ConnectApi, Slack and DataWeave are excluded by scope policy. Historical delta
classifications and the two conflicting historical behavior claims were
reconciled to that policy. Other historical classifications retain their
existing exact product-test bindings; no new proof is credited.

Every API 68 article has an explicit reviewed route override; every branch
requires explicit review. Core Apex/API/LWC notes have specific declaration or
behavior bindings, or exact reasons for not claiming the described feature.
Other application/service/setup items carry article-specific scope decisions.
Increased heap caps are implemented in candidate product but are not newly admitted as a
cap/enforcement parity claim here; the dedicated candidate product conformance test is outside
the retained published CI pin. Existing bounded heap-accounting claims remain.

Regenerate classification files with `classify_delta --contract ... --release
... --review ... --output ...` using the checked per-item review inputs. Generate
API 68 routes with `generate_release_routes.py`, its checked policy, the exact
seasonal TOC and note inventory. The historical Spring route policy was also
regenerated against its complete 1,829-entry public release-260 TOC.

Static totals: 1,145/1,145 classified surfaces; 22/22 classified behaviors;
5,824/5,824 routed articles; no unclassified entries. Correctness execution
remains a separate gate against the final product commit.

## API 62–68 contract, public acquisition on 2026-10-19 UTC

The baseline is Winter '25/API62, followed by Spring '25/API63, Summer '25/API64,
Winter '26/API65, Spring '26/API66, Summer '26/API67 and Winter '27/API68.
Default source/endpoint remain65.0. Endpoints remain the discrete
60.0/65.0/66.0/67.0/68.0 set; source62–64 does not add HTTP endpoints62–64.
Existing public65–67 source receipts and inventories remain unchanged.

All historical LWC views were regenerated from the valid28-page current-only
recapture described above. Publisher error pages are rejected before assembly.
The existing availability filter is also applied to historical reference
inventories: every excluded future-labeled declaration is retained in a generated
inventory-filter receipt. Raw public source bytes remain unchanged. Normalized
inventory manifests and source receipts bind the filtered digest.

| API / Atlas | Acquired Apex | Policy-excluded refs | Retained403 URLs | Documents / members | Future-labeled rows filtered |
| --- | ---: | ---: | ---: | ---: | ---: |
| 62 /252 |1692|245|100|2659 /4823|1|
| 63 /254 |1694|308|99|2662 /4865|1|
| 64 /256 |1702|443|106|2675 /4986|4|

The filtered ContentHub API369.0 label is impossible and has an explicit
source correction to the earliest captured API62 declaration; ConnectApi remains
excluded. The API64 CdpSegment rows explicitly sayAPI65 and are not relabeled.
Existing declarations of Composite REST, CleanRule, FieldMappingField/Row and
DeleteFilter are captured atAPI62, so later annotation changes do not invent
API65 introductions. DeleteFilter's existing API67 non-parity boundary remains.
Every correction identifies its exact public declaration, source file and URL.

Use the assembler, `compose_inventory --historical-api`, source receipt exporter
and namespace policy exporter commands above. The same canonical inventory
library and classification exporter produce these files. Re-running the compose
command into an empty output directory must reproduce the exact normalized
inventories, manifests and filter records; a direct unfiltered inventory check
is not a check of the explicitly filtered output.

### Historical release-note coverage

Winter '26 notes were acquired anonymously with the existing exporter:

```bash
python3 scripts/salesforce-release/export_release_notes.py \
  --release 258 --output "$WINTER_NOTES_OUTPUT" \
  --concurrency 2 --request-delay 0.1 --attempts 1
glade-tools docs-inventory --source "$WINTER_NOTES_OUTPUT" \
  --output docs/fixtures/salesforce-release-notes-inventory-winter-26.json
```

The commands use no org access, credentials or denied-reference retries. Exact seasonal source:
https://help.salesforce.com/s/articleView?id=release-notes.salesforce_release_notes.htm&language=en_US&release=258&type=5

All1794 articles were acquired, resolved revision258.0.0. The retained export
records every topic ID, URL, article revision, publication/fetch dates and hash.
Spring/Summer '25 inventories above cover1509/1550 articles. Each of the three
transitions has an explicit per-article policy with requireExplicit branches;
the same `generate_release_routes.py` exporter produces all routes. The six
transitions total10677 inventory documents, all routed. The static contract has
1724/1724 classified surface deltas and28/28 classified behaviors, with zero
execution credit. Qualified declaration/test evidence lives in generated
classifications, backed by checked review inputs.

The source63/64/65 LWC publication notes bind exact version-selection tests.
API65 abstract-method access changes bind the existing compiler rule and exact
sema tests. QueryLocator/Crypto parameter-label changes bind unchanged exact
argument/return shapes and existing runtime tests; they do not invent new
runtime behavior or source introductions. Documentation disappear/reappear
edges require explicit reviews on both sides. A preserved capture gap cannot
erase an independently reviewed earlier introduction date.

The eight-argument TaxLineItemRequest constructor is captured atAPI62/63,
replaced in one export by a nine-argument productSKU form, and captured again at
API65. The implemented, exact shape-tested eight-argument declaration is
preserved. Its unsupported alternate signature is separately classified. These
are declaration decisions, not native per-version constructor-execution proof.
The GraphQL module has a local implementation subset and Node tests, but no
admitted exact Go product-test binding at the published tools product pin for
the whole public module surface. It remains explicitly not claimed in this
contract; generic LWC version tests are not substituted for module proof.

### Unavailable Winter '25 baseline article

Exact seasonal URL:
https://help.salesforce.com/s/articleView?id=release-notes.rn_sales_features_core_leads_linkedinleadgen.htm&language=en_US&release=252&type=5

A polite existing-scraper check at2026-10-19T07:32:39Z returned HTTP200 from both
the article API and public page. The API had no versioned record for252.15.0
(versionNone); the public page had no visible content heading after30 seconds.
The retained1471/1472-article export is incomplete and is not committed as a
complete note inventory. This is not a ConnectApi exception.

The unchanged contract schema treats its first release as a reference baseline
and forbids classifications, changeInventory and changeRoutes on that release.
It checks62->63 onward, not61->62 changes. Therefore a local full-window table
check supplies no coverage or proof for this missing baseline article, and it
must never be described as a complete Winter '25 note acquisition. A publication
policy requiring complete baseline notes would still block that selection;
the unavailable source is retained explicitly for the coordinator's decision.

Winter26 export completedUTC: `2026-10-19T07:42:48Z`.
ExporterSHA256: `5e0ab9545024ee05826b043a731638e7decddee64e5ac61ee75966dcb31164c2`.
Retained export metadataSHA256: `b4c1dc8cb8b6b4a3094b6251dff9ed8e26ba8ccf206845ce46b40efec1d94203`.

## Verifier execution targets

The real runtime seed and lifecycle SFDX project must target the contract's
current release, independently of the source-window floor. Generate API68
request metadata and its public-document-derived compiler case with:

```bash
python3 scripts/salesforce-release/generate_verifier_inputs.py \
  --contract docs/fixtures/salesforce-release-contract.json \
  --catalog docs/fixtures/apex-language-rules.json \
  --runtime-cases docs/fixtures/salesforce-runtime-correctness.json \
  --test-project testdata/salesforce-correctness --write
```

Use `--check` for a read-only byte check. This preserves the published product
CI pin and every existing compiler-catalog byte. The added API68 Crypto case
comes from its exact checked public declaration and existing product test. It
supplies a future compiler request and source/behavior proof requirement, not an
observation. Runtime comparisons remain inconclusive with empty observations;
the generator refuses to relabel observed or conclusive data. The SFDX sources
remain unchanged; only request-version metadata advances. The real verifier
provenance regression checks these inputs together before any org operation.
The final Correctness run must still collect native and candidate results.

The final return-type audit compared364 newly supported member declarations
against the candidate's registered types. It found33 exact historical/current
property rows modeled as Object instead of their documented String, Date or
specific public type. Those rows are explicitly not claimed, with both types
recorded per item; a name/argument shape test does not prove a typed property.
The short QueryLocator versus Database.QueryLocator spelling is a reviewed
qualified-name equivalence, protected by an existing runtime test. API68 now
has119 supported declaration/behavior rows and507 explicitly unsupported or
not-claimed rows. Historical repeats are reviewed per transition.
