# LWC Native API Full Parity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn the checked LWC native API parity ledger into verified local support for documented LWC modules, PageReference types, base components, and shell context.

**Architecture:** `glade-tools` owns inventory, oracle fixtures, docs correlation, and checked reports. `glade` owns the local runtime: import maps, `lwcruntime` shims, shell services, local UI API, Apex calls, seeded org data, and browser behavior. Every row moves through docs inventory, local implementation, local browser proof, and `oaer-probe-max` oracle proof.

**Tech Stack:** Go, `glade-tools` compat plugin, `glade` LWC browser runtime, Node test runner under `lwcruntime`, Playwright/browser capture, local Glade org SQLite stores, Salesforce scratch org `oaer-probe-max`, local Salesforce docs scrape.

---

## Definition Of Full Parity

Full local parity means each documented row has one of these verified outcomes:

- `supported-local`: the local import, method shape, event shape, DOM behavior, and error behavior match Salesforce for the covered contract.
- `supported-local-proxy`: the local shell serves the behavior from Glade data, Apex controllers, org config, or local metadata instead of Salesforce network calls.
- `supported-local-simulated`: the behavior requires a Salesforce-only container or product license, so Glade provides a deterministic simulator with the same public contract and clear diagnostics.
- `not-practical-local`: the API depends on Salesforce hosted authoring surfaces, native mobile bridges, paid clouds, or server-only services. Imports still work. Calls return Salesforce-shaped unavailable errors. The ledger records why deeper local behavior is not useful.

Do not call a row done because it imports. A row is done when docs, local implementation, fixture proof, and oracle comparison agree.

Current ledger baseline:

- Total rows: 157.
- `supported-local`: 27.
- `partial-local`: 15.
- `docs-only`: 15.
- `unsupported-local`: 1.
- `local-only`: 99 base component rows awaiting docs correlation and oracle proof.

Current `docs-only` and `unsupported-local` rows:

- API modules: `experience/blockBuilderApi`, `experience/cmsDeliveryApi`, `experience/cmsEditorApi`, `lightning/graphql`, `lightning/platformUtilityBarApi`, `lightning/uiAppsApi`, `lightning/uiGraphQLApi`, `lightning/uiLearningPlatformApi`, `lightning/uiListsApi`.
- PageReference types: `standard__externalRecordPage`, `standard__externalRecordRelationshipPage`, `standard__flow`, `standard__knowledgeArticlePage`.
- Salesforce modules: `@salesforce/apexContinuation`, `@salesforce/site/activeLanguages`, `@salesforce/userPermission/`.

Current `partial-local` rows:

- API modules: `lightning/analyticsWaveApi`, `lightning/cmsDeliveryApi`, `lightning/conversationToolkitApi`, `lightning/industriesEducationPublicApi`, `lightning/mobileCapabilities`, `lightning/serviceCloudVoiceToolkitApi`, `lightning/serviceKnowledgeApi`, `lightning/uiListApi`.
- Salesforce modules: `@salesforce/community/`, `@salesforce/i18n/`, `@salesforce/i18n/dir`, `@salesforce/i18n/lang`, `@salesforce/site/`, `@salesforce/user/Id`, `@salesforce/user/isGuest`.

## Execution Model

Use parallel squads, but keep the repositories separated.

- Squad A: `glade-tools` inventory, ledger, oracle fixture generator, report gates.
- Squad B: `glade` UI API, GraphQL, Apex, permissions, user, site, i18n, LDS cache.
- Squad C: `glade` base components and source-backed component behavior.
- Squad D: `glade` shell context, navigation, pages, tabs, console, community, mobile simulation.
- Squad E: docs, site, generated support pages, final verification, cleanup.

Each squad works in an isolated worktree. Merge through one integration worktree only after focused tests pass.

## Phase 0: Complete The Inventory Contract

**Purpose:** Make the ledger authoritative enough to drive work.

**Files:**

- Modify: `/Users/matt/Dev/glade-tools/internal/lwcparity/report.go`
- Create: `/Users/matt/Dev/glade-tools/internal/lwcparity/docs_inventory.go`
- Create: `/Users/matt/Dev/glade-tools/internal/lwcparity/member_inventory.go`
- Modify: `/Users/matt/Dev/glade-tools/internal/lwcparity/report_test.go`
- Modify: `/Users/matt/Dev/glade-tools/docs/generated/LWC_NATIVE_API_PARITY.md`
- Modify: `/Users/matt/Dev/glade-tools/docs/generated/LWC_NATIVE_API_PARITY.json`

- [ ] **Step 1: Add a row model that can represent member-level parity**

Add fields to `lwcparity.Row`: `Container`, `Members`, `ParityTier`, `LocalTest`, `OracleTest`, `DocsURL`, `LastVerified`. Keep `schemaVersion` at `2`.

- [ ] **Step 2: Write the failing schema test**

Run:

```bash
go test ./internal/lwcparity -run TestBuildEmitsMemberLevelRows -count=1
```

Expected: fail because `Members`, `ParityTier`, and `schemaVersion: 2` are missing.

- [ ] **Step 3: Parse Component Reference and per-module docs**

Read from:

```text
../glade/example-projects/Salesforce Docs Scraper/salesforce-docs-expanded-run/lwc
```

Required inventory inputs:

- `reference-api-modules.md`
- `reference-salesforce-modules.md`
- `reference-page-reference-type.md`
- `reference-ui-api.md`
- Component Reference pages when the scrape contains them.

If Component Reference pages are absent, emit `inventory-gap` rows for base components instead of pretending docs are complete.

- [ ] **Step 4: Add docs refresh command**

Add:

```bash
glade-tools lwc parity refresh-docs --source <lwc-docs> --output docs/generated/LWC_NATIVE_API_PARITY.json
```

It must fail when expected docs pages are absent unless `--allow-inventory-gaps` is passed.

- [ ] **Step 5: Regenerate checked reports**

Run:

```bash
go run ./cmd/glade-tools lwc parity --docs "../glade/example-projects/Salesforce Docs Scraper/salesforce-docs-expanded-run/lwc" --output docs/generated/LWC_NATIVE_API_PARITY.md
go run ./cmd/glade-tools lwc parity --docs "../glade/example-projects/Salesforce Docs Scraper/salesforce-docs-expanded-run/lwc" --json > docs/generated/LWC_NATIVE_API_PARITY.json
go run ./cmd/glade-tools lwc parity --docs "../glade/example-projects/Salesforce Docs Scraper/salesforce-docs-expanded-run/lwc" --check docs/generated/LWC_NATIVE_API_PARITY.md
```

Exit criteria:

- The ledger names every module, every exported documented member, every PageReference type, every base component, and every base component event/property that the docs expose.
- No `local-only` base component rows remain unless the docs scrape lacks Component Reference inputs and the row is marked `inventory-gap`.

## Phase 1: Build The Native API Oracle Harness

**Purpose:** Stop guessing. Generate fixture LWCs from the ledger and compare Glade with Salesforce.

**Files:**

- Create: `/Users/matt/Dev/glade-tools/internal/lwcparity/oracle_fixtures.go`
- Create: `/Users/matt/Dev/glade-tools/internal/lwcparity/oracle_fixtures_test.go`
- Modify: `/Users/matt/Dev/glade-tools/internal/toolcli/compat_command.go`
- Modify: `/Users/matt/Dev/glade-tools/internal/compat/lwc_capture.go`
- Create: `/Users/matt/Dev/glade-tools/docs/fixtures/lwc-native-api-oracle/README.md`

- [ ] **Step 1: Add fixture generator command**

Command:

```bash
glade-tools lwc parity fixtures --ledger docs/generated/LWC_NATIVE_API_PARITY.json --out docs/fixtures/lwc-native-api-oracle
```

The command writes one minimal LWC bundle per ledger row, plus a manifest with the target host, required org setup, expected import, expected call, expected DOM text, and expected error shape.

- [ ] **Step 2: Write failing generator tests**

Test cases:

- `lightning/uiRecordApi.getRecord` produces a record fixture that renders a seeded field.
- `lightning/uiAppsApi.getNavItems` produces an app/nav fixture.
- `standard__flow` produces a navigation fixture.
- `@salesforce/site/activeLanguages` produces a site fixture.
- `lightning/button` produces a property/event fixture.

Run:

```bash
go test ./internal/lwcparity -run TestOracleFixtureGenerator -count=1
```

- [ ] **Step 3: Extend capture to consume generated fixture manifests**

Add:

```bash
glade-tools lwc capture --target-org oaer-probe-max --project docs/fixtures/lwc-native-api-oracle --manifest docs/fixtures/lwc-native-api-oracle/glade-lwc-oracle.json --browser-capture --local-browser-capture --out /tmp/glade-lwc-native-api-oracle.json
```

- [ ] **Step 4: Write comparison back into the ledger**

Add:

```bash
glade-tools lwc parity reconcile-oracle --ledger docs/generated/LWC_NATIVE_API_PARITY.json --capture /tmp/glade-lwc-native-api-oracle.json --output docs/generated/LWC_NATIVE_API_PARITY.md
```

Exit criteria:

- `oaer-probe-max` can deploy the generated fixture project.
- Capture records local DOM, Salesforce DOM, console errors, page errors, and normalized comparison for every fixture.
- Ledger rows have `oracleStatus`, `oracleTest`, and `lastVerified`.

## Phase 2: Implement High-Value Missing Native APIs

**Purpose:** Move the most useful `docs-only` rows to real local support.

**Files:**

- Modify: `/Users/matt/Dev/glade/internal/lwcbrowser/salesforce_modules.go`
- Modify: `/Users/matt/Dev/glade/internal/lwcbrowser/salesforce_modules_test.go`
- Create: `/Users/matt/Dev/glade/lwcruntime/src/lightning/uiAppsApi.mjs`
- Create: `/Users/matt/Dev/glade/lwcruntime/src/lightning/uiListsApi.mjs`
- Create: `/Users/matt/Dev/glade/lwcruntime/src/lightning/graphql.mjs`
- Create: `/Users/matt/Dev/glade/lwcruntime/src/lightning/uiGraphQLApi.mjs`
- Create: `/Users/matt/Dev/glade/lwcruntime/src/lightning/platformUtilityBarApi.mjs`
- Create: `/Users/matt/Dev/glade/lwcruntime/test/lwc-native-api-ui.test.mjs`
- Modify: `/Users/matt/Dev/glade/internal/server`

- [ ] **Step 1: Implement `lightning/uiAppsApi` from local app metadata**

Support:

- `getNavItems`
- `getAppMenuItems`
- `getAppMenuItem`

Data source:

- Custom tabs.
- FlexiPages.
- App metadata.
- Local shell app config.

Test:

```bash
go test ./internal/lwcbrowser ./internal/server -run 'Test.*UIAppsApi' -count=1
node --test lwcruntime/test/lwc-native-api-ui.test.mjs --test-name-pattern uiAppsApi
```

- [ ] **Step 2: Implement `lightning/uiListsApi` and make `uiListApi` a compatibility wrapper**

Support:

- `getListInfosByObjectName`
- `getListInfoByName`
- `getListRecordsByName`
- `getListPreferences`
- `updateListInfoByName`

Use local object describe, seeded records, and saved list-view metadata. Return Salesforce-shaped errors for unsupported list actions.

- [ ] **Step 3: Implement `lightning/graphql` and `lightning/uiGraphQLApi`**

Start with read-only UI API graph support:

- `query`
- record nodes by `Id`
- object fields
- list edges
- pagination cursors
- GraphQL error envelope

Deeper GraphQL semantics are gated by oracle fixtures. Do not claim full GraphQL parity until aliases, fragments, variables, errors, pagination, and nullability match Salesforce captures.

- [ ] **Step 4: Implement `lightning/platformUtilityBarApi`**

Back it with the local shell utility-bar service.

Support:

- utility item discovery
- open, close, minimize, focus
- label, icon, highlighted state
- event callbacks

- [ ] **Step 5: Reconcile oracle**

Run:

```bash
go run ./cmd/glade-tools lwc parity fixtures --ledger docs/generated/LWC_NATIVE_API_PARITY.json --out docs/fixtures/lwc-native-api-oracle
go run ./cmd/glade-tools lwc capture --target-org oaer-probe-max --project docs/fixtures/lwc-native-api-oracle --browser-capture --local-browser-capture --out /tmp/glade-lwc-native-api-phase2.json
go run ./cmd/glade-tools lwc parity reconcile-oracle --ledger docs/generated/LWC_NATIVE_API_PARITY.json --capture /tmp/glade-lwc-native-api-phase2.json --output docs/generated/LWC_NATIVE_API_PARITY.md
```

Exit criteria:

- `lightning/uiAppsApi`, `lightning/uiListsApi`, `lightning/graphql`, `lightning/uiGraphQLApi`, and `lightning/platformUtilityBarApi` no longer appear as `docs-only`.
- Each row has local unit tests and browser/oracle proof.

## Phase 3: Fill Salesforce Module Gaps

**Purpose:** Make `@salesforce/*` modules match local org context.

**Files:**

- Modify: `/Users/matt/Dev/glade/internal/lwcbrowser/salesforce_modules.go`
- Modify: `/Users/matt/Dev/glade/lwcruntime/src/shims/site.mjs`
- Modify: `/Users/matt/Dev/glade/lwcruntime/src/shims/community.mjs`
- Create: `/Users/matt/Dev/glade/lwcruntime/src/shims/user-permission.mjs`
- Create: `/Users/matt/Dev/glade/lwcruntime/src/shims/apex-continuation.mjs`
- Create: `/Users/matt/Dev/glade/lwcruntime/test/lwc-salesforce-modules.test.mjs`

- [ ] **Step 1: Implement `@salesforce/userPermission/*`**

Read from local org permission sets and profile data. Return `true` or `false` as Salesforce does for static permission imports.

- [ ] **Step 2: Implement `@salesforce/site/activeLanguages`**

Read from local site/community metadata. Return an array of language records with stable shape. If no site context exists, return the Salesforce-shaped unavailable value captured from `oaer-probe-max`.

- [ ] **Step 3: Expand `@salesforce/site/*` and `@salesforce/community/*`**

Cover:

- site id
- active languages
- base path
- community id
- community name
- community URL context
- LWR versus Aura container flags when known

- [ ] **Step 4: Expand `@salesforce/i18n/*`**

Cover the full documented identifier list from docs scrape and oracle captures. Values come from local org/user settings, with deterministic defaults when absent.

- [ ] **Step 5: Implement `@salesforce/apexContinuation`**

Provide practical local parity:

- import works
- continuation method call returns a Promise
- callback resolution matches imperative Apex shape
- timeout and server error envelopes match Salesforce captures

True Salesforce servlet continuation scheduling is not locally useful. Mark the deep scheduling behavior `supported-local-simulated`.

Exit criteria:

- No `@salesforce/*` row remains `docs-only` or `unsupported-local`.
- Permission, site, community, i18n, user, and continuation fixtures pass local and `oaer-probe-max` comparison.

## Phase 4: PageReference And Shell Navigation Parity

**Purpose:** Make every documented PageReference type useful in the local shell.

**Files:**

- Modify: `/Users/matt/Dev/glade/lwcruntime/src/shell/navigation-service.mjs`
- Modify: `/Users/matt/Dev/glade/lwcruntime/src/shell/router.mjs`
- Modify: `/Users/matt/Dev/glade/lwcruntime/src/shell/workbench-builder.mjs`
- Modify: `/Users/matt/Dev/glade/lwcruntime/test/lwc-navigation-services.test.mjs`
- Modify: `/Users/matt/Dev/glade/internal/lwcbrowser/manifest.go`

- [ ] **Step 1: Add `standard__flow` routing**

Route to the local flow shell. Pass flow inputs. Match Salesforce URL state and event behavior for launch, finish, pause, and error.

- [ ] **Step 2: Add external record page types**

Support:

- `standard__externalRecordPage`
- `standard__externalRecordRelationshipPage`

If the external connection is not configured, render a Salesforce-shaped unavailable state rather than a broken route.

- [ ] **Step 3: Add `standard__knowledgeArticlePage`**

Back it with local Knowledge metadata when present. Otherwise render a shell page with the article type, URL name, language, and unavailable diagnostic.

- [ ] **Step 4: Generate navigation oracle fixtures**

Each PageReference fixture must call:

- `NavigationMixin.Navigate`
- `NavigationMixin.GenerateUrl`
- route state round trip
- back/forward browser history

Exit criteria:

- Every PageReference row is at least `supported-local-simulated`.
- The local landing page, tabs, component routes, and builder never trap the user without a Home route.

## Phase 5: Base Component Source Parity

**Purpose:** Convert the 99 base component rows from import support into documented behavior support.

**Files:**

- Modify: `/Users/matt/Dev/glade/internal/lwcbrowser/base_components.go`
- Modify: `/Users/matt/Dev/glade/internal/lwcbrowser/base_components_test.go`
- Modify: `/Users/matt/Dev/glade/internal/lwcbrowser/source_backed_components.go`
- Modify: `/Users/matt/Dev/glade/lwcruntime/src/lightning/*.mjs`
- Create: `/Users/matt/Dev/glade/lwcruntime/test/lwc-base-components-conformance.test.mjs`
- Create: `/Users/matt/Dev/glade-tools/internal/lwcparity/base_component_docs.go`

- [ ] **Step 1: Bring in source-backed implementations where licensing permits**

Use these sources in order:

- `lightning-base-components` package for open source base component implementations.
- `jerry-wang12/lightning-demo` as reference material, then reinterpret behavior into Glade code.
- Existing Glade `lwcruntime/src/lightning` shims where they already pass oracle fixtures.

- [ ] **Step 2: Build component conformance matrix**

For each component, track:

- public properties
- public methods
- slots
- events
- validity API
- keyboard behavior
- focus behavior
- ARIA behavior
- rendered SLDS structure
- error and disabled states

- [ ] **Step 3: Prioritize data and form components first**

Finish:

- `lightning-record-form`
- `lightning-record-edit-form`
- `lightning-record-view-form`
- `lightning-input-field`
- `lightning-output-field`
- `lightning-record-picker`
- `lightning-messages`

These are the components most likely to break real LWCs using seeded data.

- [ ] **Step 4: Then finish high-use UI components**

Finish:

- `lightning-button`
- `lightning-button-icon`
- `lightning-input`
- `lightning-textarea`
- `lightning-combobox`
- `lightning-checkbox-group`
- `lightning-radio-group`
- `lightning-datatable`
- `lightning-tree-grid`
- `lightning-modal`
- `lightning-toast`
- `lightning-tabset`
- `lightning-card`

- [ ] **Step 5: Run browser conformance**

Run:

```bash
node --test lwcruntime/test/lwc-base-components-conformance.test.mjs
go test ./internal/lwcbrowser -run TestSupportedLightningBaseComponents -count=1
```

Exit criteria:

- Each base component row has docs correlation and at least one conformance fixture.
- The base-components-recipes project loads without console or page errors.
- Real project corpus LWCs no longer fail because of missing base component behavior.

## Phase 6: Partial Product And Container APIs

**Purpose:** Handle APIs that depend on Salesforce clouds, console, mobile, CMS, or builder containers.

**Files:**

- Create: `/Users/matt/Dev/glade/lwcruntime/src/shell/product-service.mjs`
- Create: `/Users/matt/Dev/glade/lwcruntime/src/lightning/mobileCapabilities.mjs`
- Create: `/Users/matt/Dev/glade/lwcruntime/src/lightning/cmsDeliveryApi.mjs`
- Create: `/Users/matt/Dev/glade/lwcruntime/src/lightning/serviceKnowledgeApi.mjs`
- Create: `/Users/matt/Dev/glade/lwcruntime/src/lightning/conversationToolkitApi.mjs`
- Create: `/Users/matt/Dev/glade/lwcruntime/src/lightning/serviceCloudVoiceToolkitApi.mjs`
- Modify: `/Users/matt/Dev/glade/lwcruntime/src/shell/workspace-service.mjs`

- [ ] **Step 1: Add a product service registry**

The shell context defines enabled services:

- console
- utility bar
- mobile
- CMS
- Experience Builder
- Knowledge
- Conversation Toolkit
- Service Cloud Voice
- Analytics
- Industries Education

Each service has a deterministic local provider and an unavailable provider.

- [ ] **Step 2: Implement console and utility APIs as real local services**

Support workspace tabs, subtabs, focused tab, tab labels, tab icons, highlights, and utility item state.

- [ ] **Step 3: Implement CMS and Knowledge as local metadata-backed services**

Use local content metadata when present. Return documented unavailable or empty states when absent.

- [ ] **Step 4: Implement mobile capabilities simulator**

Support form factor, geolocation availability, barcode scanner availability, and offline indicators as configurable shell context. Do not claim native mobile parity when running in desktop Chrome.

- [ ] **Step 5: Mark cloud-only APIs with honest support tiers**

Use `supported-local-simulated` or `not-practical-local` for:

- `lightning/analyticsWaveApi`
- `lightning/industriesEducationPublicApi`
- `lightning/serviceCloudVoiceToolkitApi`
- `experience/blockBuilderApi`
- `experience/cmsEditorApi`
- `lightning/uiLearningPlatformApi`

Exit criteria:

- No import fails for product/container APIs.
- Every cloud-only row has a captured Salesforce unavailable shape or a captured happy path from an org with that feature enabled.
- Local diagnostics tell the developer which shell context flag or org feature is missing.

## Phase 7: Data, LDS, GraphQL, And Apex Controller Fidelity

**Purpose:** Make real project LWCs work with seeded Glade org data and actual Apex controllers.

**Files:**

- Modify: `/Users/matt/Dev/glade/lwcruntime/src/shims/lds-cache.mjs`
- Modify: `/Users/matt/Dev/glade/lwcruntime/src/shims/wire-adapter.mjs`
- Modify: `/Users/matt/Dev/glade/internal/lwcbrowser/wire.go`
- Modify: `/Users/matt/Dev/glade/internal/server`
- Modify: `/Users/matt/Dev/glade/internal/storage`
- Create: `/Users/matt/Dev/glade/lwcruntime/test/lwc-lds-graphql-apex.test.mjs`

- [ ] **Step 1: Make local record context authoritative**

The builder's object and record id must flow to:

- `@api recordId`
- `@api objectApiName`
- LDS wire adapters
- record forms
- output fields
- navigation state
- Apex method arguments when components pass them through

- [ ] **Step 2: Implement complete LDS cache semantics**

Support:

- wire cache identity
- refresh
- invalidation after create/update/delete
- `notifyRecordUpdateAvailable`
- `getFieldValue`
- `getFieldDisplayValue`
- field-level errors
- layout mode differences

- [ ] **Step 3: Use actual Apex controllers**

Local shell Apex calls must execute through the existing Glade Apex runtime and seeded org database. Mock only when the user configures a mock.

- [ ] **Step 4: Add seeded DB runbook**

Document and test:

```bash
sf data import tree --plan data/plan.json --target-org <org>
glade org pull-data <alias> --project . --out .glade/orgs/<alias>.sqlite
glade dev lwc --project . --org <alias> --port 8080
```

Exit criteria:

- Record View Form and Record Edit Form recipes render seeded values.
- Real project LWCs that call Apex controllers work without per-component mocks.
- Network, console, and page errors are zero for the priority project set.

## Phase 8: Project Corpus Gate

**Purpose:** Keep parity pointed at real packages, not just generated fixtures.

**Files:**

- Modify: `/Users/matt/Dev/glade-tools/internal/compat/lwc_corpus_scan.go`
- Modify: `/Users/matt/Dev/glade-tools/internal/compat/lwc_corpus_scan_test.go`
- Create: `/Users/matt/Dev/glade-tools/docs/generated/LWC_PRIORITY_PROJECT_READINESS.md`

- [ ] **Step 1: Lock priority projects**

Use this set:

- `src-nbm-solhub-develop`
- `src-nmb-namz-prog-develop`
- `src-nmb-nc-develop`
- `src-nmb-nu-develop`
- `src-nmb-nudev-develop`
- `src-nmb-nuq-develop`
- `src-nmb-nutpl-develop`
- `src-nmb-nutplx-master`
- `sf-cred-pkg-develop`
- `base-components-recipes`

- [ ] **Step 2: Add project launch checks**

For every LWC bundle:

- compile
- load direct component route
- load declared target route
- provide record context when target requires it
- capture console/page errors
- record unsupported imports, tags, and wire adapters

- [ ] **Step 3: Add corpus readiness command**

Run:

```bash
glade-tools lwc corpus --root /Users/matt/.sf-repo-analysis/repos --include-repos src-nbm-solhub-develop,src-nmb-namz-prog-develop,src-nmb-nc-develop,src-nmb-nu-develop,src-nmb-nudev-develop,src-nmb-nuq-develop,src-nmb-nutpl-develop,src-nmb-nutplx-master,sf-cred-pkg-develop,base-components-recipes --json
```

Exit criteria:

- Every priority project has a generated readiness row.
- No priority project has missing base components, missing native modules, broken route context, or unhandled shell errors.

## Phase 9: CI, Ratchets, Docs, And Site

**Purpose:** Keep parity from sliding back.

**Files:**

- Modify: `/Users/matt/Dev/glade-tools/internal/toolcli/compat_command.go`
- Modify: `/Users/matt/Dev/glade-tools/README.md`
- Modify: `/Users/matt/Dev/glade/docs/generated/LWC_SHELL_SUPPORT.md`
- Modify: `/Users/matt/Dev/glade/site`

- [ ] **Step 1: Add parity gate flags**

Add:

```bash
glade-tools lwc parity --docs <lwc-docs> --check docs/generated/LWC_NATIVE_API_PARITY.md --fail-on docs-only,unsupported-local --require-oracle
```

- [ ] **Step 2: Add fixture gate**

Add:

```bash
glade-tools lwc parity fixtures --ledger docs/generated/LWC_NATIVE_API_PARITY.json --check docs/fixtures/lwc-native-api-oracle
```

- [ ] **Step 3: Publish support docs**

Update:

- local LWC preview docs
- seeded org docs
- native API support matrix
- base component support matrix
- shell context docs
- priority project readiness report

- [ ] **Step 4: Add site pages**

The public site must say:

- which APIs are real local support
- which APIs are simulated
- which APIs require seeded data
- which APIs require shell context
- how to run `glade dev lwc --project . --org <alias> --port 8080`
- how to read the parity report

Exit criteria:

- `go test ./...` passes in `glade-tools`.
- Focused `glade` runtime tests pass.
- LWC parity check passes.
- Priority project gate passes.
- Public docs match checked generated reports.

## Phase 10: Final Full-Parity Certification

**Purpose:** Mark the milestone with proof, not a claim.

- [ ] **Step 1: Run generated oracle against `oaer-probe-max`**

```bash
go run ./cmd/glade-tools lwc capture --target-org oaer-probe-max --project docs/fixtures/lwc-native-api-oracle --browser-capture --local-browser-capture --out /tmp/glade-lwc-native-api-final.json --json
```

- [ ] **Step 2: Reconcile the capture**

```bash
go run ./cmd/glade-tools lwc parity reconcile-oracle --ledger docs/generated/LWC_NATIVE_API_PARITY.json --capture /tmp/glade-lwc-native-api-final.json --output docs/generated/LWC_NATIVE_API_PARITY.md
```

- [ ] **Step 3: Run base components recipes**

```bash
glade dev lwc --project /Users/matt/.sf-repo-analysis/repos/base-components-recipes --port 8080
```

Use browser capture to verify every recipe route loads without console or page errors.

- [ ] **Step 4: Run priority corpus**

```bash
glade-tools lwc corpus --root /Users/matt/.sf-repo-analysis/repos --include-repos src-nbm-solhub-develop,src-nmb-namz-prog-develop,src-nmb-nc-develop,src-nmb-nu-develop,src-nmb-nudev-develop,src-nmb-nuq-develop,src-nmb-nutpl-develop,src-nmb-nutplx-master,sf-cred-pkg-develop,base-components-recipes --check
```

- [ ] **Step 5: Commit in layers**

Commit order:

1. `feat: expand LWC parity inventory`
2. `feat: add LWC native API oracle fixtures`
3. `feat: implement LWC native data APIs`
4. `feat: expand Salesforce module shims`
5. `feat: complete LWC navigation parity`
6. `feat: harden Lightning base component parity`
7. `docs: publish LWC native API parity`

Full certification is done only when no row remains `docs-only` or `unsupported-local`, no row remains `partial-local` without a scoped explanation, and every `simulated` or `not-practical-local` row has oracle-backed reason text.

## Subagent Dispatch Map

Dispatch these in parallel after Phase 0:

- Agent 1: docs inventory and member-level ledger.
- Agent 2: oracle fixture generator and capture reconciliation.
- Agent 3: UI API and GraphQL modules.
- Agent 4: Salesforce modules, permissions, site, community, i18n.
- Agent 5: PageReference routing and shell context.
- Agent 6: base component conformance and source-backed replacements.
- Agent 7: priority corpus launch gate.
- Agent 8: docs, site, generated reports, final review.

Integration agent responsibilities:

- Keep `glade-tools` and `glade` worktrees sibling paths.
- Run `go test ./...` in `glade-tools` after every merge.
- Run focused `glade` tests after runtime merges.
- Regenerate ledgers only from commands.
- Never hand-edit generated support reports.

