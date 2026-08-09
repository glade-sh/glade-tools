# Private Corpus Salesforce Assurance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Keep one integrator. Close each completed worker immediately.

**Goal:** Produce release-bound proof that every in-scope private repository passes Glade, every extracted Salesforce usage is classified, and every required used surface has exact-candidate local evidence plus the appropriate Salesforce proof or an explicit non-parity exclusion.

**Architecture:** Add one manifest-driven `glade-tools corpus assurance` workflow. Fresh immutable source snapshots feed deterministic usage extraction and two-host repository replay. Exact-candidate fixture execution produces per-surface local proof. Razor runs the complete private-required Salesforce oracle set in two scratch-org shards. One local fail-closed reconciler validates raw receipts and writes JSON, a self-contained HTML explorer, and one acyclic create-only receipt.

**Tech Stack:** Go standard library, existing `surfaceledger` and fixture-runner packages, existing current-base Salesforce transport scripts, Salesforce CLI, SSH/rsync, and self-contained HTML with no external dependencies.

---

## Frozen baseline

- Glade commit: `1afce5007001aa79f25fd4d76d1c8d8821790a4f`
- Glade darwin/arm64 binary: `/private/tmp/glade-current-final`
- Glade binary SHA-256: `d0a8d428321c8a3640861cf7546e066143f395052f3ee163561b8fb43dbc393a`
- Support profile SHA-256: `d6d7b19adfee2285f3abcbf58d227debc923bee86e8849db9f266e7b18855e31`
- Retained private-root digest for drift comparison: `98acad6d14a6051a28f22f331fd92029e12aca21da0acf1c6f7669f0a5df2a04`
- Runtime evidence root: `/Users/matt/.config/superpowers/worktrees/glade-salesforce/sf-20260730-01/evidence/current-base/private-corpus-assurance/`
- Remote staging parent on both hosts: `/private/tmp/glade-assurance-1afce500/`; every run uses one new mode-0700 attempt child.

The private runtime manifest, source snapshots, usage decisions, and evidence bundle stay outside git. Checked code, tests, docs, and HTML examples use neutral `private-corpus-NNN` IDs only.

## Host ownership

| Host | Architecture | Work |
|---|---|---|
| local | darwin/arm64 | build, inventory, half of repository replay, usage reconciliation, local fixture proof, merge, report |
| `casper.local` | darwin/arm64 | other half of repository replay using the exact Glade binary |
| `razor.local` | darwin/x86_64 | Salesforce CLI preflight and two scratch-org oracle shards; never execute the arm64 Glade candidate |

This uses all three machines without pretending architecture-specific binaries are identical. Glade is one exact arm64 artifact. `glade-tools` has arm64 and amd64 builds from one tools commit, each bound by `(OS, architecture, SHA-256)`.

## Acceptance contract

The release passes only when all conditions hold on one frozen input set:

1. A frozen private inventory specification defines the complete in-scope denominator. Every listed repository is a clean pinned checkout, has an immutable source snapshot, and is replayed exactly once on local or Casper.
2. Every repository runs `glade check`; local tests run when present. A repository without local tests has an explicit `tests-not-present` reason, never a generic skip.
3. Corpus usage is regenerated from the immutable snapshots twice with byte-identical output. Every private-reference usage entry belongs to exactly one reconciliation class. Unknown count is zero.
4. Every mapped required surface has exact-candidate local fixture proof appropriate to its disposition.
5. Every release reruns the complete private-required Salesforce oracle set. No prior release receives parity credit.
6. Portable runtime rows have Salesforce runtime proof; compile-shape rows have Salesforce compile proof; deterministic mocks have local behavioral proof and Salesforce shape proof when deployable.
7. Hosted identity, managed data, license, password, SSO, and org-configuration behavior is an explicit non-parity exclusion and is never counted as Salesforce parity.
8. Raw Salesforce shards bind the org, selector, fixture, candidate, tools, profile, usage, decisions, pre/post inventory, command results, and cleanup result.
9. `ASSURANCE.json` and `ASSURANCE.html` are immutable. `RECEIPT.json` is written last, hashes every input and output except itself, and is never rewritten.
10. Public output contains neutral repository IDs only. Final Sol x-high review passes. The user reviews and signs off through a ready, review-only PR.

## File map

Create or modify only first-party maintenance files in `glade-tools`:

- Create `internal/corpusassurance/model.go` and `model_test.go`.
- Create `internal/corpusassurance/inventory.go` and `inventory_test.go`.
- Create `internal/corpusassurance/usage.go` and `usage_test.go`.
- Modify `internal/surfaceledger/corpus_usage.go` and `corpus_usage_test.go`.
- Create `internal/corpusassurance/replay.go` and `replay_test.go`.
- Create `internal/corpusassurance/localproof.go` and `localproof_test.go`.
- Create `internal/corpusassurance/oracle.go` and `oracle_test.go`.
- Create `internal/corpusassurance/salesforce.go` and `salesforce_test.go`.
- Create `internal/corpusassurance/report.go` and `report_test.go`.
- Create `internal/corpusassurance/html.go` and `html_test.go`.
- Create `internal/toolcli/corpus_assurance_command.go` and `corpus_assurance_command_test.go`.
- Modify `internal/toolcli/compat_command.go` and `internal/toolcli/manifest.go`.
- Create `docs/fixtures/corpus-assurance-exclusion-policy.json`.
- Create `docs/fixtures/corpus-assurance-scratch-def.json`.
- Create `docs/corpus-assurance.md`.
- Modify `README.md` and `scripts/release-check.sh`.

## Task 1: Define the immutable manifest and artifact model

**Files:** `internal/corpusassurance/model.go`, `internal/corpusassurance/model_test.go`

- [ ] Write tests for unique neutral repository IDs, SHA-256 validation, runtime architecture bindings, no private paths in public projections, and create-only output.

```go
type RuntimeArtifact struct {
	Commit string `json:"commit"`
	OS     string `json:"os"`
	Arch   string `json:"arch"`
	SHA256 string `json:"sha256"`
}

type InventoryEntry struct {
	ID             string `json:"id"`
	CheckoutPath   string `json:"checkoutPath"` // private input only
	ExpectedCommit string `json:"expectedCommit"`
}

type InventorySpec struct {
	SchemaVersion int              `json:"schemaVersion"`
	Scope         string           `json:"scope"`
	Repositories  []InventoryEntry `json:"repositories"`
}

type RepositorySpec struct {
	ID               string `json:"id"`
	ExpectedCommit   string `json:"expectedCommit"`
	ArchiveSHA256    string `json:"archiveSha256"`
	TreeSHA256       string `json:"treeSha256"`
	AssignedHost     string `json:"assignedHost"`
	SnapshotPath     string `json:"snapshotPath"`
	LocalTests       string `json:"localTests"` // required | tests-not-present
	LocalTestsReason string `json:"localTestsReason,omitempty"`
}
```

- [ ] Reject `skip`, unsupported hosts, duplicate IDs, blank archive/tree/commit bindings, `tests-not-present` without a reason, absolute snapshot paths in host manifests, and a second JSON value. Private checkout paths must never appear in public projections.
- [ ] Implement `WriteNewJSON` with `os.OpenFile(..., os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)`. Never overwrite a shard, report, bundle, or receipt.
- [ ] Run `go test ./internal/corpusassurance -run 'TestManifest|TestWriteNew' -count=1`.
- [ ] Commit: `feat: define corpus assurance bindings`.

## Task 2: Inventory clean repositories and create replay snapshots

**Files:** `internal/corpusassurance/inventory.go`, `internal/corpusassurance/inventory_test.go`

- [ ] Test inventory with clean, dirty tracked, untracked, wrong-commit, no-tests, missing-spec-entry, extra-discovered-project, and duplicate-project fixtures.
- [ ] Require a create-only `IN_SCOPE.json` before discovery. It contains neutral IDs, private checkout paths, and expected commits. Its SHA-256 is the authoritative denominator and is bound into every later artifact. The produced manifest ID set must equal it exactly.
- [ ] Require `git status --porcelain=v1 --untracked-files=all` to be empty and `git rev-parse HEAD` to match the manifest commit. Ignored build output does not enter the snapshot.
- [ ] Create each source artifact with deterministic `git archive --format=tar HEAD`. Record the tar bytes as `archiveSha256`. Extract beneath `snapshots/private-corpus-NNN/` and compute `treeSha256` from the sorted canonical stream `relative-path NUL mode NUL file-sha256 NUL`. These are separate bindings.
- [ ] Discover local-test capability from the snapshot. Require `required`, or record `tests-not-present` with a specific discovery reason.
- [ ] Assign repositories only to `local` and `casper`. Use stable longest-processing-time assignment when historical durations exist; otherwise stable round-robin by neutral ID.
- [ ] Write one private root manifest plus host-resolved `hosts/local/manifest.json` and `hosts/casper/manifest.json`. Each host manifest binds the root-manifest SHA and uses relative snapshot paths.
- [ ] Recompute every archive SHA and canonical extracted-tree SHA before accepting either host manifest.
- [ ] Run `go test ./internal/corpusassurance -run 'TestInventory|TestSnapshot|TestAssign' -count=1`.
- [ ] Commit: `feat: snapshot and assign private corpus`.

## Task 3: Regenerate and reconcile every private usage key

**Files:** `internal/corpusassurance/usage.go`, `internal/corpusassurance/usage_test.go`, `internal/surfaceledger/corpus_usage.go`, `internal/surfaceledger/corpus_usage_test.go`

- [ ] Reuse and extend the existing `surfaceledger` corpus scanner. Add an exported per-repository result keyed by neutral repository ID; do not build a second Apex usage parser.
- [ ] Make every tracked Apex-file read fail closed. Remove the existing silent `os.ReadFile` skip and test unreadable/missing tracked files.
- [ ] Run extraction from the immutable snapshots twice into separate temporary files. Require byte-identical canonical JSON before writing `CORPUS_USAGE.json` create-only.
- [ ] Run the scanner deterministically once per neutral repository, then union those explicit results. Keep only entries with `privateProdRefs + privateTestRefs > 0`; derive each row's sorted `repositoryIds` directly from the contributing per-repository results.
- [ ] Classify each entry exactly once as `exact`, `case-alias`, `aggregate-parent`, `canonical-alias`, `local-symbol`, or `non-salesforce-generated`.
- [ ] Automatic classes are `exact`, unambiguous `case-alias`, and unambiguous `aggregate-parent`. The other classes require a reason-coded decision file.
- [ ] Reject unknown decision classes, decisions for absent keys, duplicate decisions, aliases to missing surfaces, ambiguous aliases, and partition arithmetic where class counts do not equal total private-reference entries.
- [ ] Require `unknown == 0`; bind snapshot-root digest, usage bytes, profile SHA, and decision SHA.
- [ ] Run `go test ./internal/corpusassurance -run 'TestExtractUsage|TestReconcileUsage' -count=1`.
- [ ] Commit: `feat: reconcile private corpus usage`.

## Task 4: Replay every repository on the exact candidate

**Files:** `internal/corpusassurance/replay.go`, `internal/corpusassurance/replay_test.go`

- [ ] Use fake Glade/tools binaries to test exact argument order, nonzero exits, timeouts, missing tests, source tampering, wrong architecture, and no-clobber output.
- [ ] Preflight host OS/architecture and candidate/tools hashes before any repository runs.
- [ ] For every assigned repository, run the existing `glade check` contract and the existing local Apex test contract when `localTests=required`.
- [ ] Capture command, exit code, duration, stdout/stderr SHA-256, repository ID, source SHA, candidate SHA, and tools SHA. Do not embed private paths or command output in public results.
- [ ] A shard passes only when every assigned repository has one passing check and its required test result.
- [ ] Merge rejects missing repositories, duplicates across hosts, unexpected IDs, binding mismatches, and any failed required outcome.
- [ ] Run `go test ./internal/corpusassurance -run 'TestReplay|TestMergeReplay' -count=1`.
- [ ] Commit: `feat: replay exact candidate across corpus`.

## Task 5: Produce per-surface exact-candidate local proof

**Files:** `internal/corpusassurance/localproof.go`, `internal/corpusassurance/localproof_test.go`

- [ ] Select fixtures only for mapped private-required surfaces. Reuse the accepted-evidence and fixture-runner contracts already in `glade-tools`.
- [ ] Run selected fixtures against the exact Glade binary. Produce one raw result per fixture and one normalized result per surface ID.
- [ ] Require runtime observation for `local-runtime-required`, deterministic behavioral observation for `deterministic-mock-required`, and compile/check success for `compile-shape-required`.
- [ ] A fixture may cover multiple surface IDs only when the fixture manifest explicitly owns all of them. Reject inferred or aggregate credit.
- [ ] Bind candidate, tools, profile, usage, decisions, fixture bytes, fixture manifest, and selected surface IDs in `LOCAL_PROOF.json`.
- [ ] Reject missing, duplicate, stale, or wrong-disposition local evidence.
- [ ] Run `go test ./internal/corpusassurance -run 'TestLocalProof' -count=1`.
- [ ] Commit: `feat: produce local surface proof`.

## Task 6: Plan the complete private-required Salesforce set

**Files:** `internal/corpusassurance/oracle.go`, `internal/corpusassurance/oracle_test.go`

- [ ] Use only `runtime`, `compile`, `local-contract-only`, `waiver`, and `unknown` actions.
- [ ] Map portable local runtime rows to `runtime`; compile-shape and deployable deterministic mocks to `compile`; non-portable deterministic mocks to reason-coded `local-contract-only`; hosted rows to reason-coded `waiver`; contradictory or unowned rows to `unknown`.
- [ ] Require every `local-contract-only` and `waiver` row to carry an allowed non-parity exclusion class plus a non-empty reason. Require `runtime`, `compile`, `local-contract-only`, `waiver`, and `unknown` sets to be complete and pairwise disjoint. Only `runtime` and `compile` can receive Salesforce parity credit.
- [ ] Have the planner emit `EXCLUSION_REQUEST.json`; it cannot authorize its own exclusions. A separate `authorize-exclusions` step validates every requested ID/class/reason against the checked `corpus-assurance-exclusion-policy.json` and writes create-only `EXCLUSION_AUTHORITY.json`. The authority binds candidate, fresh usage, decisions, projected profile, oracle plan, policy SHA, exact sorted IDs, and zero parity credit.
- [ ] Do not retain Salesforce credit from a prior release. Every candidate release executes the full `runtime + compile` private-required set. A previous receipt is comparison-only.
- [ ] Rebuild `ASSURANCE_PROFILE.json` from an allowlist of canonical source-profile row fields plus fresh usage and decisions. Recompute `rows`, `nonDeferredGaps`, `hostedDeferred`, totals, and disposition counts from the fresh required ID set. Reject and omit embedded old corpus usage, `classificationBinding`, old oracle-mismatch receipts, prior queue/selector fields, and any row not owned by the current surface ledger and fixture manifest.
- [ ] Materialize an assurance-specific transport bundle for direct `salesforce-first-filter-20260805.py` execution. Include `ASSURANCE_PROFILE.json`, oracle plan, exclusion authority, fresh release-validation receipt, local-proof summary, fixture manifest, fixture sources, filter script, `corpus-assurance-scratch-def.json`, candidate/tools/usage/decision bindings, and bundle receipt. Do not pass stale queue, selector, selector-receipt, model-routing, release-validation, corpus-usage, or classification lineage from the previous closure.
- [ ] Copy fixture sources into the bundle and rewrite paths relative to the bundle. Bind the exact hashes of the filter script and every projected input.
- [ ] Test the bundle against the filter's manifest/profile/local-summary validation and both modulus partitions before SSH dispatch.
- [ ] Run `go test ./internal/corpusassurance -run 'TestOraclePlan|TestOracleBundle' -count=1`.
- [ ] Commit: `feat: materialize private Salesforce oracle`.

## Task 7: Run and validate raw Salesforce shards

**Files:** `internal/corpusassurance/salesforce.go`, `internal/corpusassurance/salesforce_test.go`

- [ ] Cross-build `glade-tools` for Razor from the same tools commit. Bind its darwin/amd64 SHA separately from the darwin/arm64 build.
- [ ] Add `salesforce-run` as a thin wrapper around the staged `salesforce-first-filter-20260805.py` and assurance bundle. It must invoke the filter with this complete inner command shape, substituting only the shard index, org alias, and create-only attempt paths:

```text
python3 transport/salesforce-first-filter-20260805.py
  --profile bundle/profile.json
  --fixtures bundle/fixtures
  --manifest bundle/fixture-manifest.json
  --root bundle
  --out executor/shard-N/filter
  --limit <fixture-count>
  --orgs <org-alias>
  --ssh-host razor.local
  --ssh-user matt
  --remote-root <attempt-root>/executor/shard-N
  --remote-run-id <attempt-id>-shard-N
  --remote-sf-bin /usr/local/bin/sf
  --candidate-commit 1afce5007001aa79f25fd4d76d1c8d8821790a4f
  --candidate-sha256 d0a8d428321c8a3640861cf7546e066143f395052f3ee163561b8fb43dbc393a
  --tools-commit <final-tools-commit>
  --queue-sha256 <oracle-plan-sha256>
  --selector-sha256 <oracle-plan-sha256>
  --selector-receipt-sha256 <bundle-receipt-sha256>
  --runtime
  --local-summary bundle/LOCAL_PROOF_SUMMARY.json
  --manifest-index-modulus 2
  --manifest-index-remainder N
```

`salesforce-run` records this exact argv and the filter script SHA, then normalizes raw evidence itself. There is no legacy `PACKET.json` or reconciliation authority in this workflow.
- [ ] Define a create-only `SalesforceShard` containing:

```go
type SalesforceShard struct {
	Bindings      SalesforceBindings `json:"bindings"`
	ShardIndex    int                `json:"shardIndex"`
	ShardCount    int                `json:"shardCount"`
	OrgAlias      string             `json:"orgAlias"`
	OrgID         string             `json:"orgId"`
	OrgStatus     string             `json:"orgStatus"`
	PreInventory  FileBinding        `json:"preInventory"`
	Commands      []CommandReceipt   `json:"commands"`
	Results       []SurfaceResult    `json:"results"`
	PostInventory FileBinding        `json:"postInventory"`
	Cleanup       CleanupReceipt     `json:"cleanup"`
}
```

- [ ] Before invoking the filter, query all eight inventory types: `ApexClass`, `ApexPage`, `ApexTrigger`, `CustomObject`, `CustomField`, `FieldSet`, `StaticResource`, and `PlatformCachePartition`. Require every filtered `totalSize` to be zero; equality alone is insufficient. If an existing org is not empty, reject it and use a new minimal scratch org.
- [ ] Require org status `Active`, exact distinct org IDs, zero eight-type preflight inventory, disjoint selector partitions, exact fixture/result identity, expected runtime/compile result kinds, pre/post inventory equality, filter remote cleanup residue absence, and create-only output.
- [ ] Bind candidate SHA as release identity even though Razor does not execute that arm64 binary. Bind the amd64 tools binary and transport script hashes that Razor does execute.
- [ ] Add tamper tests for every binding and receipt. Run `go test ./internal/corpusassurance -run 'TestSalesforceShard' -count=1`.
- [ ] Commit: `feat: normalize Salesforce assurance shards`.

## Task 8: Build the final JSON, HTML, and acyclic receipt

**Files:** `internal/corpusassurance/report.go`, `report_test.go`, `html.go`, `html_test.go`

- [ ] Make `report` consume direct paths for `IN_SCOPE.json`, the root manifest, source profile, `ASSURANCE_PROFILE.json`, raw twice-extracted `CORPUS_USAGE.json`, `USAGE_RECONCILIATION.json`, decisions, exclusion request/policy/authority, release validation, fixture manifest, replay merge, local proof, oracle plan, bundle receipt/checksums, staged filter script, scratch definition, executed amd64 tools binary, org creation/preflight/cleanup receipts, remote cleanup receipts, and every raw Salesforce shard.
- [ ] Recompute all hashes and all set arithmetic. Require the manifest IDs to equal the inventory denominator. Require Salesforce parity, local-contract-only exclusions, and hosted waivers to be pairwise disjoint and complete; local-contract-only always has zero Salesforce parity credit.
- [ ] Include neutral `repositoryIds` in each surface row so repository filtering is backed by the usage model.

```go
type AssuranceSurfaceRow struct {
	Namespace          string   `json:"namespace"`
	SurfaceID          string   `json:"surfaceId"`
	UsageKeys          []string `json:"usageKeys"`
	RepositoryIDs      []string `json:"repositoryIds"`
	PrivateProdRefs    int      `json:"privateProdRefs"`
	PrivateTestRefs    int      `json:"privateTestRefs"`
	Disposition        string   `json:"disposition"`
	LocalEvidence      string   `json:"localEvidence"`
	SalesforceAction   string   `json:"salesforceAction"`
	SalesforceEvidence string   `json:"salesforceEvidence"`
	ExclusionClass     string   `json:"exclusionClass,omitempty"`
	ExclusionReason    string   `json:"exclusionReason,omitempty"`
	FixtureIDs         []string `json:"fixtureIds"`
}
```

- [ ] Write `ASSURANCE.json` and `ASSURANCE.html` create-only. The HTML embeds the exact report JSON and filters by namespace, disposition, neutral repository ID, evidence kind, exclusion class, and text.
- [ ] Write `RECEIPT.json` last with create-only semantics. It hashes all immutable inputs, raw shards, `ASSURANCE.json`, and `ASSURANCE.html`, but not itself. Do not create a self-referential `SHA256SUMS`.
- [ ] Search JSON/HTML for private manifest paths and prohibited project names. Require only neutral repository IDs.
- [ ] Test report/HTML parity, every arithmetic gate, path redaction, evidence-link existence, tampering, and attempted overwrite.
- [ ] Run `go test ./internal/corpusassurance -run 'TestBuildReport|TestWriteHTML|TestReceipt' -count=1`.
- [ ] Commit: `feat: publish sealed corpus assurance`.

## Task 9: Expose the CLI and operator contract

**Files:** `internal/toolcli/corpus_assurance_command.go`, `corpus_assurance_command_test.go`, `compat_command.go`, `manifest.go`, `docs/corpus-assurance.md`, `README.md`

- [ ] Add `glade-tools corpus assurance` dispatch with these exact modes:

```text
prepare          validate frozen IN_SCOPE.json; clean inventory, snapshots, fresh usage, host manifests
usage            reconcile fresh usage with reason-coded decisions
replay           run one host-resolved repository shard
merge-replay     validate complete disjoint replay coverage
local-proof      execute exact-candidate per-surface fixtures
release-validate run and seal broad checks against the final candidate and tools commit
oracle-plan      classify every private-required Salesforce obligation
authorize-exclusions seal exact non-parity IDs against the checked policy
oracle-bundle    materialize exact filter inputs and fixture sources
org-preflight    query and seal the zero eight-type org inventory gate
salesforce-run   execute and normalize one raw Salesforce shard
org-cleanup      delete only attempt-created scratch orgs and seal absence
cleanup          verify, remove, and receipt one exact remote attempt root
report           validate all direct inputs and seal JSON, HTML, and receipt
```

- [ ] Make help list every required path/binding flag. `prepare` requires `--inventory-spec`, `--profile`, `--candidate-*`, `--tools-*`, and a new attempt output. `release-validate` requires the exact Glade source root, candidate binary, tools root/commit, and output; it rejects a dirty Glade or tools worktree and rejects a Glade HEAD other than the candidate commit. Require explicit `--output`; every final output is create-only.
- [ ] Document the host architecture split, exact `/private/tmp/glade-assurance-1afce500/` remote path, `/usr/local/bin/sf`, `SF_USE_GENERIC_UNIX_KEYCHAIN=true`, scratch-org serialization, no-clobber rule, privacy boundary, and full Salesforce rerun policy.
- [ ] Add bounded synthetic assurance tests to `scripts/release-check.sh`. Public CI must not require private paths, SSH, or live Salesforce.
- [ ] Run `go test ./internal/toolcli -run TestCorpusAssurance -count=1`.
- [ ] Run `go test ./... -count=1`, `scripts/release-check.sh`, and `git diff --check`.
- [ ] Commit every implementation, test, documentation, policy, and release-check change. Record this immutable final tools commit before Task 10. No checked file changes are allowed between this boundary and Salesforce dispatch.
- [ ] From the clean committed tools worktree, write the 40-character HEAD once to `/Users/matt/.config/superpowers/worktrees/glade-salesforce/sf-20260730-01/evidence/current-base/private-corpus-assurance/input/FINAL_TOOLS_COMMIT` using shell noclobber (`set -C`), then make it mode `0400`. Task 10 treats this separately frozen file—not live `rev-parse` output—as authority.

## Task 10: Execute the initial three-machine assurance run

Runtime variables below are shell notation for the operator ledger. `ATTEMPT_ID` is generated once, recorded, and never reused:

```bash
ASSURANCE_PARENT=/Users/matt/.config/superpowers/worktrees/glade-salesforce/sf-20260730-01/evidence/current-base/private-corpus-assurance
ATTEMPT_ID=assurance-1afce500-20260808T000000Z
ATTEMPT_ROOT="$ASSURANCE_PARENT/$ATTEMPT_ID"
REMOTE_PARENT=/private/tmp/glade-assurance-1afce500
REMOTE_ATTEMPT="$REMOTE_PARENT/$ATTEMPT_ID"
TOOLS_ROOT=/Users/matt/.config/superpowers/worktrees/glade-salesforce/sf-20260730-01/integration/glade-tools
GLADE_ROOT=/Users/matt/.config/superpowers/worktrees/glade-salesforce/sf-20260730-01/integration/glade
PROFILE=/Users/matt/.config/superpowers/worktrees/glade-salesforce/sf-20260730-01/evidence/current-base/recovery-dispatch-20260807/wave37/classified-workflow-bound-final-v79/apex-support-profile.json
IN_SCOPE="$ASSURANCE_PARENT/input/IN_SCOPE.json"
DECISIONS="$ASSURANCE_PARENT/input/USAGE_DECISIONS.json"
FIXTURES="$TOOLS_ROOT/docs/fixtures"
FILTER_SCRIPT=/Users/matt/.config/superpowers/worktrees/glade-salesforce/sf-20260730-01/evidence/current-base/salesforce-first-filter-20260805.py
FINAL_TOOLS_COMMIT="$ASSURANCE_PARENT/input/FINAL_TOOLS_COMMIT"
TOOLS_COMMIT="$(tr -d '\n' < "$FINAL_TOOLS_COMMIT")"
SF_ORG_0="$ATTEMPT_ID-sf0"
SF_ORG_1="$ATTEMPT_ID-sf1"
```

- [ ] Freeze `IN_SCOPE.json` before discovery. It must contain the complete user-reviewable neutral-ID, checkout-path, expected-commit denominator recovered from the private corpus. If this file is unavailable, stop and ask the user for the authoritative corpus root/list; do not infer completeness from whichever directories happen to be mounted.
- [ ] Create one private local attempt directory without reuse:

```bash
umask 077
test -f "$IN_SCOPE"
test ! -e "$ATTEMPT_ROOT"
mkdir -m 700 "$ATTEMPT_ROOT"
mkdir -m 700 "$ATTEMPT_ROOT/run"
```

- [ ] Confirm hosts and tools before dispatch:

```bash
uname -s && uname -m
ssh -o BatchMode=yes matt@casper.local 'uname -s && uname -m'
ssh -o BatchMode=yes matt@razor.local 'uname -s && uname -m && test -x /usr/local/bin/sf'
```

Expected: local and Casper are `Darwin arm64`; Razor is `Darwin x86_64`.

- [ ] Require exact clean source state before building:

```bash
test -z "$(git -C "$GLADE_ROOT" status --porcelain=v1 --untracked-files=all)"
test "$(git -C "$GLADE_ROOT" rev-parse HEAD)" = 1afce5007001aa79f25fd4d76d1c8d8821790a4f
test -z "$(git -C "$TOOLS_ROOT" status --porcelain=v1 --untracked-files=all)"
test "$(git -C "$TOOLS_ROOT" rev-parse HEAD)" = "$TOOLS_COMMIT"
test "$(shasum -a 256 /private/tmp/glade-current-final | awk '{print $1}')" = d0a8d428321c8a3640861cf7546e066143f395052f3ee163561b8fb43dbc393a
```

Expected: both status commands emit nothing; Glade HEAD is exactly `1afce5007001aa79f25fd4d76d1c8d8821790a4f`; tools HEAD is the recorded final implementation commit. `release-validate` repeats and enforces these checks.

- [ ] Build and verify architecture-specific tools into the new attempt. Keep the exact existing Glade candidate:

```bash
mkdir -m 700 "$ATTEMPT_ROOT/bin"
cp /private/tmp/glade-current-final "$ATTEMPT_ROOT/bin/glade-darwin-arm64"
cd "$TOOLS_ROOT"
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o "$ATTEMPT_ROOT/bin/glade-tools-darwin-arm64" ./cmd/glade-tools
CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -o "$ATTEMPT_ROOT/bin/glade-tools-darwin-amd64" ./cmd/glade-tools
file "$ATTEMPT_ROOT/bin/"*
shasum -a 256 "$ATTEMPT_ROOT/bin/"*
```

Expected: the Glade hash equals the frozen SHA; both tools builds share one tools commit but have separate architecture/SHA bindings.

- [ ] Run `prepare` with the frozen denominator, then `usage`. `prepare` must regenerate usage twice from snapshots and verify byte identity:

```bash
"$ATTEMPT_ROOT/bin/glade-tools-darwin-arm64" corpus assurance prepare --attempt-id "$ATTEMPT_ID" --reserved-org "$SF_ORG_0" --reserved-org "$SF_ORG_1" --inventory-spec "$IN_SCOPE" --profile "$PROFILE" --candidate-bin "$ATTEMPT_ROOT/bin/glade-darwin-arm64" --candidate-commit 1afce5007001aa79f25fd4d76d1c8d8821790a4f --tools-root "$TOOLS_ROOT" --tools-arm64 "$ATTEMPT_ROOT/bin/glade-tools-darwin-arm64" --tools-amd64 "$ATTEMPT_ROOT/bin/glade-tools-darwin-amd64" --output "$ATTEMPT_ROOT/prepared"
```

Resolve only the remaining reason-coded decisions until `unknown=0`.

```bash
"$ATTEMPT_ROOT/bin/glade-tools-darwin-arm64" corpus assurance usage --manifest "$ATTEMPT_ROOT/prepared/MANIFEST.json" --profile "$PROFILE" --usage "$ATTEMPT_ROOT/prepared/CORPUS_USAGE.json" --decisions "$DECISIONS" --max-unknown 0 --output "$ATTEMPT_ROOT/run/USAGE_RECONCILIATION.json"
```
- [ ] Materialize self-contained host directories:

```text
$ATTEMPT_ROOT/prepared/hosts/local/manifest.json
$ATTEMPT_ROOT/prepared/hosts/local/snapshots/private-corpus-NNN/...
$ATTEMPT_ROOT/prepared/hosts/casper/manifest.json
$ATTEMPT_ROOT/prepared/hosts/casper/snapshots/private-corpus-NNN/...
```

- [ ] Create a new mode-0700 Casper attempt root, then stage the exact host directory without flattening paths:

```bash
ssh -o BatchMode=yes matt@casper.local "umask 077; test ! -e '$REMOTE_ATTEMPT'; test -d '$REMOTE_PARENT' || mkdir -m 700 '$REMOTE_PARENT'; mkdir -m 700 '$REMOTE_ATTEMPT'"
rsync -a "$ATTEMPT_ROOT/prepared/hosts/casper/" matt@casper.local:"$REMOTE_ATTEMPT/host/"
rsync -a "$ATTEMPT_ROOT/bin/glade-darwin-arm64" "$ATTEMPT_ROOT/bin/glade-tools-darwin-arm64" matt@casper.local:"$REMOTE_ATTEMPT/host/"
ssh matt@casper.local "stat -f '%Lp' '$REMOTE_ATTEMPT'; shasum -a 256 '$REMOTE_ATTEMPT'/host/glade-* '$REMOTE_ATTEMPT'/host/manifest.json"
```

Expected mode is `700`. Require the remote manifest, archive/tree, and artifact hashes to match their local staged copies before execution.

- [ ] Run local and Casper replay concurrently with exact paths:

```bash
"$ATTEMPT_ROOT/bin/glade-tools-darwin-arm64" corpus assurance replay --manifest "$ATTEMPT_ROOT/prepared/hosts/local/manifest.json" --host local --candidate-bin "$ATTEMPT_ROOT/bin/glade-darwin-arm64" --tools-bin "$ATTEMPT_ROOT/bin/glade-tools-darwin-arm64" --output "$ATTEMPT_ROOT/run/replay-local.json"
ssh matt@casper.local "'$REMOTE_ATTEMPT/host/glade-tools-darwin-arm64' corpus assurance replay --manifest '$REMOTE_ATTEMPT/host/manifest.json' --host casper --candidate-bin '$REMOTE_ATTEMPT/host/glade-darwin-arm64' --tools-bin '$REMOTE_ATTEMPT/host/glade-tools-darwin-arm64' --output '$REMOTE_ATTEMPT/host/replay-casper.json'"
```

- [ ] While replay runs, execute `local-proof` locally:

```bash
"$ATTEMPT_ROOT/bin/glade-tools-darwin-arm64" corpus assurance local-proof --manifest "$ATTEMPT_ROOT/prepared/MANIFEST.json" --profile "$PROFILE" --usage "$ATTEMPT_ROOT/run/USAGE_RECONCILIATION.json" --fixtures "$FIXTURES" --candidate-bin "$ATTEMPT_ROOT/bin/glade-darwin-arm64" --tools-bin "$ATTEMPT_ROOT/bin/glade-tools-darwin-arm64" --fixture-manifest-output "$ATTEMPT_ROOT/run/FIXTURE_MANIFEST.json" --summary-output "$ATTEMPT_ROOT/run/LOCAL_PROOF_SUMMARY.json" --output "$ATTEMPT_ROOT/run/LOCAL_PROOF.json"
```

- [ ] Copy Casper's shard back with an independently captured hash, then merge replay:

```bash
ssh matt@casper.local "cd '$REMOTE_ATTEMPT/host' && shasum -a 256 replay-casper.json" > "$ATTEMPT_ROOT/run/replay-casper.remote.sha256"
rsync -a matt@casper.local:"$REMOTE_ATTEMPT/host/replay-casper.json" "$ATTEMPT_ROOT/run/replay-casper.json"
cd "$ATTEMPT_ROOT/run" && shasum -a 256 -c replay-casper.remote.sha256
"$ATTEMPT_ROOT/bin/glade-tools-darwin-arm64" corpus assurance merge-replay --manifest "$ATTEMPT_ROOT/prepared/MANIFEST.json" --shard "$ATTEMPT_ROOT/run/replay-local.json" --shard "$ATTEMPT_ROOT/run/replay-casper.json" --output "$ATTEMPT_ROOT/run/REPLAY.json"
```

- [ ] After the final tools commit, run broad release validation before any Salesforce dispatch:

```bash
"$ATTEMPT_ROOT/bin/glade-tools-darwin-arm64" corpus assurance release-validate --glade-root "$GLADE_ROOT" --candidate-bin "$ATTEMPT_ROOT/bin/glade-darwin-arm64" --candidate-commit 1afce5007001aa79f25fd4d76d1c8d8821790a4f --tools-root "$TOOLS_ROOT" --tools-commit "$TOOLS_COMMIT" --tools-freeze "$FINAL_TOOLS_COMMIT" --output "$ATTEMPT_ROOT/run/RELEASE_VALIDATION.json"
```

This producer runs the documented broad Glade checks and tools checks, records exact argv/exit/output hashes, and seals only fresh final-commit results.

- [ ] Build the complete oracle plan and create separate exclusion authority before the Razor bundle:

```bash
"$ATTEMPT_ROOT/bin/glade-tools-darwin-arm64" corpus assurance oracle-plan --manifest "$ATTEMPT_ROOT/prepared/MANIFEST.json" --source-profile "$PROFILE" --usage "$ATTEMPT_ROOT/run/USAGE_RECONCILIATION.json" --local-proof "$ATTEMPT_ROOT/run/LOCAL_PROOF.json" --fixture-manifest "$ATTEMPT_ROOT/run/FIXTURE_MANIFEST.json" --output "$ATTEMPT_ROOT/run/ORACLE_PLAN.json" --profile-output "$ATTEMPT_ROOT/run/ASSURANCE_PROFILE.json" --exclusion-request-output "$ATTEMPT_ROOT/run/EXCLUSION_REQUEST.json"
"$ATTEMPT_ROOT/bin/glade-tools-darwin-arm64" corpus assurance authorize-exclusions --request "$ATTEMPT_ROOT/run/EXCLUSION_REQUEST.json" --policy "$TOOLS_ROOT/docs/fixtures/corpus-assurance-exclusion-policy.json" --candidate-bin "$ATTEMPT_ROOT/bin/glade-darwin-arm64" --usage "$ATTEMPT_ROOT/prepared/CORPUS_USAGE.json" --decisions "$DECISIONS" --profile "$ATTEMPT_ROOT/run/ASSURANCE_PROFILE.json" --oracle-plan "$ATTEMPT_ROOT/run/ORACLE_PLAN.json" --output "$ATTEMPT_ROOT/run/EXCLUSION_AUTHORITY.json"
"$ATTEMPT_ROOT/bin/glade-tools-darwin-arm64" corpus assurance oracle-bundle --manifest "$ATTEMPT_ROOT/prepared/MANIFEST.json" --profile "$ATTEMPT_ROOT/run/ASSURANCE_PROFILE.json" --oracle-plan "$ATTEMPT_ROOT/run/ORACLE_PLAN.json" --exclusion-authority "$ATTEMPT_ROOT/run/EXCLUSION_AUTHORITY.json" --release-validation "$ATTEMPT_ROOT/run/RELEASE_VALIDATION.json" --local-summary "$ATTEMPT_ROOT/run/LOCAL_PROOF_SUMMARY.json" --fixture-manifest "$ATTEMPT_ROOT/run/FIXTURE_MANIFEST.json" --fixture-root "$FIXTURES" --filter-script "$FILTER_SCRIPT" --scratch-def "$TOOLS_ROOT/docs/fixtures/corpus-assurance-scratch-def.json" --tools-amd64 "$ATTEMPT_ROOT/bin/glade-tools-darwin-amd64" --output "$ATTEMPT_ROOT/razor"
```

The Razor bundle contains the fresh projected profile, exclusion authority, release validation, local-proof summary, fixture manifest/sources, oracle plan, filter script, amd64 tools binary, and bundle receipt. No previous queue/selector/model-routing/corpus/classification lineage is copied.
- [ ] Create a new mode-0700 Razor attempt root, then stage the self-contained directory without flattening paths:

```bash
ssh -o BatchMode=yes matt@razor.local "umask 077; test ! -e '$REMOTE_ATTEMPT'; test -d '$REMOTE_PARENT' || mkdir -m 700 '$REMOTE_PARENT'; mkdir -m 700 '$REMOTE_ATTEMPT'"
rsync -a "$ATTEMPT_ROOT/razor/" matt@razor.local:"$REMOTE_ATTEMPT/"
ssh matt@razor.local "stat -f '%Lp' '$REMOTE_ATTEMPT'; cd '$REMOTE_ATTEMPT' && shasum -a 256 -c BUNDLE_SHA256SUMS"
ssh matt@razor.local "umask 077; mkdir -m 700 '$REMOTE_ATTEMPT/output' '$REMOTE_ATTEMPT/executor'"
```

- [ ] Create two fresh minimal orgs from `glade-dev-hub4`; do not reuse the known nonzero h3 inventories:

```bash
ssh matt@razor.local "SF_USE_GENERIC_UNIX_KEYCHAIN=true /usr/local/bin/sf org create scratch --target-dev-hub glade-dev-hub4 --definition-file '$REMOTE_ATTEMPT/bundle/corpus-assurance-scratch-def.json' --alias '$SF_ORG_0' --duration-days 1 --json > '$REMOTE_ATTEMPT/output/org-create-0.json'"
ssh matt@razor.local "SF_USE_GENERIC_UNIX_KEYCHAIN=true /usr/local/bin/sf org create scratch --target-dev-hub glade-dev-hub4 --definition-file '$REMOTE_ATTEMPT/bundle/corpus-assurance-scratch-def.json' --alias '$SF_ORG_1' --duration-days 1 --json > '$REMOTE_ATTEMPT/output/org-create-1.json'"
ssh matt@razor.local "SF_USE_GENERIC_UNIX_KEYCHAIN=true /usr/local/bin/sf org display --target-org '$SF_ORG_0' --json"
ssh matt@razor.local "SF_USE_GENERIC_UNIX_KEYCHAIN=true /usr/local/bin/sf org display --target-org '$SF_ORG_1' --json"
```

Expected: each returns `status: Active` and a distinct org ID. Seal the executable eight-type zero-inventory gate with the explicit CLI mode:

```bash
ssh matt@razor.local "cd '$REMOTE_ATTEMPT' && SF_USE_GENERIC_UNIX_KEYCHAIN=true ./bin/glade-tools-darwin-amd64 corpus assurance org-preflight --bundle bundle/bundle.json --target-org '$SF_ORG_0' --self-ssh-host razor.local --sf-bin /usr/local/bin/sf --executor-root '$REMOTE_ATTEMPT/executor/preflight-0' --output output/org-preflight-0.json"
ssh matt@razor.local "cd '$REMOTE_ATTEMPT' && SF_USE_GENERIC_UNIX_KEYCHAIN=true ./bin/glade-tools-darwin-amd64 corpus assurance org-preflight --bundle bundle/bundle.json --target-org '$SF_ORG_1' --self-ssh-host razor.local --sf-bin /usr/local/bin/sf --executor-root '$REMOTE_ATTEMPT/executor/preflight-1' --output output/org-preflight-1.json"
```

Each receipt must show zero `ApexClass`, `ApexPage`, `ApexTrigger`, `CustomObject`, `CustomField`, `FieldSet`, `StaticResource`, and `PlatformCachePartition` records. Any nonzero type aborts the attempt.

- [ ] Launch these two commands concurrently on Razor:

```bash
ssh matt@razor.local "cd '$REMOTE_ATTEMPT' && SF_USE_GENERIC_UNIX_KEYCHAIN=true ./bin/glade-tools-darwin-amd64 corpus assurance salesforce-run --bundle bundle/bundle.json --org-preflight output/org-preflight-0.json --target-org '$SF_ORG_0' --self-ssh-host razor.local --sf-bin /usr/local/bin/sf --executor-root '$REMOTE_ATTEMPT/executor/shard-0' --shard-index 0 --shard-count 2 --output output/salesforce-shard-0.json"
ssh matt@razor.local "cd '$REMOTE_ATTEMPT' && SF_USE_GENERIC_UNIX_KEYCHAIN=true ./bin/glade-tools-darwin-amd64 corpus assurance salesforce-run --bundle bundle/bundle.json --org-preflight output/org-preflight-1.json --target-org '$SF_ORG_1' --self-ssh-host razor.local --sf-bin /usr/local/bin/sf --executor-root '$REMOTE_ATTEMPT/executor/shard-1' --shard-index 1 --shard-count 2 --output output/salesforce-shard-1.json"
```

- [ ] Copy both raw shards back with remote hash receipts:

```bash
ssh matt@razor.local "cd '$REMOTE_ATTEMPT' && shasum -a 256 output/org-create-0.json output/org-create-1.json output/org-preflight-0.json output/org-preflight-1.json output/salesforce-shard-0.json output/salesforce-shard-1.json" > "$ATTEMPT_ROOT/run/salesforce.remote.sha256"
rsync -a matt@razor.local:"$REMOTE_ATTEMPT/output/" "$ATTEMPT_ROOT/run/output/"
cd "$ATTEMPT_ROOT/run" && shasum -a 256 -c salesforce.remote.sha256
```

- [ ] After verified copy-back, delete only the two attempt-created scratch orgs and seal the result:

```bash
"$ATTEMPT_ROOT/bin/glade-tools-darwin-arm64" corpus assurance org-cleanup --host matt@razor.local --attempt-manifest "$ATTEMPT_ROOT/prepared/MANIFEST.json" --bundle "$ATTEMPT_ROOT/razor/bundle/bundle.json" --creation-receipt "$ATTEMPT_ROOT/run/output/org-create-0.json" --creation-receipt "$ATTEMPT_ROOT/run/output/org-create-1.json" --org-preflight "$ATTEMPT_ROOT/run/output/org-preflight-0.json" --org-preflight "$ATTEMPT_ROOT/run/output/org-preflight-1.json" --target-org "$SF_ORG_0" --target-org "$SF_ORG_1" --dev-hub glade-dev-hub4 --sf-bin /usr/local/bin/sf --output "$ATTEMPT_ROOT/run/ORG_CLEANUP.json"
```

The command requires aliases and org IDs to match the attempt, bundle, creation, and preflight receipts, runs `sf org delete scratch --no-prompt`, verifies absence, and cannot target a pre-existing org. On partial creation or preflight failure, run the same mode immediately with the available creation receipts; it may discover only reserved aliases from the attempt manifest and must verify their `glade-dev-hub4` scratch-org creation time is after the attempt start before deletion.

- [ ] Clean both exact remote attempt roots:

```bash
"$ATTEMPT_ROOT/bin/glade-tools-darwin-arm64" corpus assurance cleanup --host matt@casper.local --attempt-root "$REMOTE_ATTEMPT" --parent "$REMOTE_PARENT" --binding "$ATTEMPT_ROOT/prepared/hosts/casper/manifest.json" --output "$ATTEMPT_ROOT/run/REMOTE_CLEANUP_CASPER.json"
"$ATTEMPT_ROOT/bin/glade-tools-darwin-arm64" corpus assurance cleanup --host matt@razor.local --attempt-root "$REMOTE_ATTEMPT" --parent "$REMOTE_PARENT" --binding "$ATTEMPT_ROOT/razor/bundle/bundle.json" --output "$ATTEMPT_ROOT/run/REMOTE_CLEANUP_RAZOR.json"
```

`cleanup` validates the exact attempt path below the fixed parent, binds the staged manifest/bundle receipt, removes only that attempt child, verifies absence, and writes a create-only receipt. No private source snapshot remains remotely.
- [ ] Seal the final report from direct inputs:

```bash
"$ATTEMPT_ROOT/bin/glade-tools-darwin-arm64" corpus assurance report --inventory-spec "$IN_SCOPE" --manifest "$ATTEMPT_ROOT/prepared/MANIFEST.json" --source-profile "$PROFILE" --profile "$ATTEMPT_ROOT/run/ASSURANCE_PROFILE.json" --raw-usage "$ATTEMPT_ROOT/prepared/CORPUS_USAGE.json" --usage "$ATTEMPT_ROOT/run/USAGE_RECONCILIATION.json" --decisions "$DECISIONS" --exclusion-request "$ATTEMPT_ROOT/run/EXCLUSION_REQUEST.json" --exclusion-policy "$TOOLS_ROOT/docs/fixtures/corpus-assurance-exclusion-policy.json" --exclusion-authority "$ATTEMPT_ROOT/run/EXCLUSION_AUTHORITY.json" --release-validation "$ATTEMPT_ROOT/run/RELEASE_VALIDATION.json" --fixture-manifest "$ATTEMPT_ROOT/run/FIXTURE_MANIFEST.json" --replay "$ATTEMPT_ROOT/run/REPLAY.json" --local-proof "$ATTEMPT_ROOT/run/LOCAL_PROOF.json" --oracle-plan "$ATTEMPT_ROOT/run/ORACLE_PLAN.json" --bundle-receipt "$ATTEMPT_ROOT/razor/bundle/bundle.json" --bundle-checksums "$ATTEMPT_ROOT/razor/BUNDLE_SHA256SUMS" --filter-script "$ATTEMPT_ROOT/razor/transport/salesforce-first-filter-20260805.py" --scratch-def "$ATTEMPT_ROOT/razor/bundle/corpus-assurance-scratch-def.json" --tools-amd64 "$ATTEMPT_ROOT/razor/bin/glade-tools-darwin-amd64" --org-creation "$ATTEMPT_ROOT/run/output/org-create-0.json" --org-creation "$ATTEMPT_ROOT/run/output/org-create-1.json" --org-preflight "$ATTEMPT_ROOT/run/output/org-preflight-0.json" --org-preflight "$ATTEMPT_ROOT/run/output/org-preflight-1.json" --salesforce-shard "$ATTEMPT_ROOT/run/output/salesforce-shard-0.json" --salesforce-shard "$ATTEMPT_ROOT/run/output/salesforce-shard-1.json" --org-cleanup "$ATTEMPT_ROOT/run/ORG_CLEANUP.json" --remote-cleanup "$ATTEMPT_ROOT/run/REMOTE_CLEANUP_CASPER.json" --remote-cleanup "$ATTEMPT_ROOT/run/REMOTE_CLEANUP_RAZOR.json" --output "$ATTEMPT_ROOT/ASSURANCE.json" --html-output "$ATTEMPT_ROOT/ASSURANCE.html" --receipt-output "$ATTEMPT_ROOT/RECEIPT.json"
```
- [ ] Require `status=passed`, zero unknown usage, zero replay gaps, zero missing local proof, zero unclosed Salesforce obligations, zero cleanup residue, and no private leakage.
- [ ] Open `ASSURANCE.html`; verify namespace/repository filters and row totals against `ASSURANCE.json`. Verify `RECEIPT.json` hashes every input and both report files.

## Task 11: Verify, review, and prepare the user-signoff PR

- [ ] Run focused checks:

```bash
go test ./internal/corpusassurance ./internal/toolcli -count=1
```

- [ ] Run repository checks:

```bash
go test ./... -count=1
scripts/release-check.sh
git diff --check
```

- [ ] Audit checked files and synthetic output for private names/paths. Require only `private-corpus-NNN` labels.
- [ ] Run one independent Sol x-high review against the exact branch SHA, final `RECEIPT.json`, `ASSURANCE.json`, `ASSURANCE.html`, and verification logs. Let it finish. Fix only reproducible findings, rerun affected checks, and repeat until PASS.
- [ ] Any code, fixture, profile, decision, or tools-commit change after Salesforce dispatch invalidates the attempt. Start Task 10 again under a new attempt ID; never rebind old shards.
- [ ] Close every completed reviewer/worker immediately. Confirm no remote replay or Salesforce process remains and both scratch orgs have zero cleanup residue.
- [ ] Push the branch and open one ready, review-only PR. Include the acceptance arithmetic, candidate/tools/receipt hashes, verification commands, Sol verdict, known non-parity exclusions, and the HTML explorer location. Do not merge. User approval is final sign-off.

## Execution checkpoints

1. **Tooling:** command and synthetic tests pass.
2. **Corpus:** fresh usage has zero unknowns; every repository replayed exactly once.
3. **Local proof:** every mapped required surface has exact-candidate fixture evidence.
4. **Salesforce:** full private-required runtime/compile set passes; exclusions are explicit and disjoint.
5. **Seal:** `ASSURANCE.json`, HTML, and acyclic receipt agree byte-for-byte.
6. **Review:** Sol x-high PASS and ready PR await user sign-off.

Do not call the goal complete at the tooling checkpoint or from the prior 1,618-row closure. Completion requires the sealed private-corpus assurance receipt and the user's PR approval.
