# Salesforce Release Scope & Reason Reference

This document defines the investigation scopes, dispositions, and reason anchors
used across the Winter-to-Spring and Spring-to-Summer '26 release inventory
classification files. Every
`deterministic-mock` or `explicit-unsupported` row in the classification file
references a real heading from this document.  Classifications are **triage
truth**, not completion truth.  A surface classified as `new-case`,
`deterministic-mock`, or `existing-case` remains release work until its
corresponding oracle case, deterministic mock, or matching product evidence is
green.

## Scope vocabulary

### t0 — Apex Runtime & Language

Apex language constructs, Apex runtime behavior, test execution, SOQL/DML,
schema surfaces, and any behavior required for local Apex compilation or local
test execution.  T0 surfaces must compile in the Glade Apex compiler and must
pass their oracle cases when run against a target org and the Glade runtime.

### t1 — UI Component Execution

LWC modules, Aura references, and Visualforce components that must load and
behave correctly under a local component/page execution engine.  T1 surfaces
require shape support for component metadata plus behavioral oracle cases
covering attributes, events, and rendering.

### t2 — Hosted-Service API and Apex Namespace

REST, Tooling, ConnectApi, and metadata-backed Apex namespace surfaces whose
underlying operation is backed by hosted Salesforce services. T2 surfaces
cannot produce real hosted results locally. They are satisfied by deterministic
mocks that expose the documented types and return documented response shapes
for known inputs.

### outside-claim — Hosted-Only or Administration

Surfaces related to administration, deployment infrastructure, or hosted-only
operations that user code cannot meaningfully execute in a local environment.
Outside-claim surfaces are marked `explicit-unsupported` and require a
`reasonRef` to a heading below.

## Disposition vocabulary

- **existing-case**: A checked compiler, runtime, or lifecycle fixture already
  covers this exact SurfaceID, or a checked Glade product-test subtest names the
  exact canonical SurfaceID. A label or reason alone is not evidence.
- **new-case**: The surface is in scope for local execution and requires a new
  oracle case or exact product-test subtest. One case may cover many exact
  SurfaceIDs only when its result carries every listed ID.
- **deterministic-mock**: The surface is a hosted-service operation (T2) that
  cannot execute without a Salesforce instance.  The local project should execute
  against a deterministic mock.  `reasonRef` must point to a non-empty heading
  below.
- **explicit-unsupported**: The surface is `outside-claim` and is explicitly not
  targeted for local execution.  `reasonRef` must point to a non-empty heading
  below justifying the exclusion.

## Reason references

### Deterministic Mock for ConnectApi

Summer '26 ConnectApi input and response types must exist locally, but commerce,
routing, quote, and prompt results come from hosted services. A deterministic
ConnectApi mock must expose the documented DTO shapes and return stable
documented response shapes for known inputs.

### Deterministic Mock for Invocable Action Metadata

`Invocable.Action.getDescribe()` and its nested metadata result types depend on
org-hosted action definitions. The local runtime must expose the documented
types and return deterministic describe metadata supplied by the test project.

### Deterministic Mock for Tooling API

API-versioned Tooling objects are hosted Salesforce service schemas. Local
support exposes their documented shapes and stable mock responses where the
server models the object.

### Deterministic Mock for Hosted Apex Namespaces

ConnectApi, Invocable, RichMessaging, sfdw, sfsqlquery, and wave operations
depend on hosted Salesforce services. Local support exposes the exact
versioned types and signatures, then returns stable documented shapes for
known test inputs.

### LWC Version-Gated Module

An LWC module is available only to bundles whose metadata `apiVersion` is
inside the module's documented range. The installed compiler, not a source
project default, enforces each bundle boundary.

### Explicitly Unsupported Tooling Platform Event Migration

`PlatformEventMigration` is a hosted Tooling object with no local migration
model. Glade rejects it explicitly at API 67.0 instead of returning an empty or
fabricated migration record.

### Explicitly Unsupported IntegrationTest Preview

`System.IntegrationTest` is a Salesforce-hosted developer-preview transaction
service. Glade keeps its API 67.0 shape version-aware and rejects local runtime
execution explicitly.

## Moving-correctness release workflow

Glade supports the checked moving window, not every historical source version.
The current source window is 65.0, 66.0, and 67.0. The endpoint window is 60.0,
65.0, 66.0, and 67.0. Source, endpoint, org profile, and LWC bundle versions are
independent axes and never supply fallback values for one another.
Release-mode compiler verification runs only catalog rules inside the checked
source window. Historical rules remain audit coverage, not product support
credit.

For each Salesforce release:

1. Export the versioned Atlas families and release notes with the existing
   inventory and `scripts/salesforce-release` exporters. Generate the checked
   source receipt from that export. Keep current-only LWC provenance separate
   from versioned Atlas documents.
2. Add the release manifest, classify every adjacent surface delta, and route
   every release-note document to exact surface or behavior IDs, or to a
   concrete out-of-scope reason.
3. Run `glade-tools salesforce release --write` to regenerate the source,
   semantic, LWC, endpoint, and Tooling availability tables in Glade. Commit
   those generated files with the checked contract.
4. Run the full Glade product suite with the real LWC compiler installed. The
   verifier credits only observed passing cases and exact file/test/subtest
   bindings from the checksummed Go event log. The gate rejects candidate
   binaries whose embedded Git revision is not the named clean commit.
5. Promote only when every advertised axis passes, every surface and behavior
   is proved, every release note is routed, generated files match, and silent
   fallback count is zero.

`salesforce release --check` is the drift check. The static release report
cannot claim completion; only `salesforce verify` can attach execution proof.
