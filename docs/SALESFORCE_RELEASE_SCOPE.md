# Salesforce Release Scope & Reason Reference

This document defines the investigation scopes, dispositions, and reason anchors
used in the Spring-to-Summer '26 release inventory classification file.  Every
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
  covers this exact SurfaceID.  The case ID is the existing stable identifier.
- **new-case**: The surface is in scope for local execution and requires a new
  oracle case. One behavior-level case ID may cover many exact SurfaceIDs when
  the fixture asserts every listed type, member, and signature. The ID does not
  imply the case already exists in fixtures.
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

The Tooling API `PlatformEventMigration` object is a hosted Salesforce service.
A deterministic mock returning documented resource shapes is acceptable for
local project compilation and test execution.  The mock must reflect the
Summer '26 documented schema.

### Explicitly Unsupported — Reserved

No `outside-claim` surfaces appear in the Spring-to-Summer '26 delta.  This
heading exists so the vocabulary is complete.
