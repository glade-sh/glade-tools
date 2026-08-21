# Salesforce surface adoption workflow

This is the operator handoff for a bounded Salesforce surface packet. Every
packet has one exact candidate/tools pair, one reviewed input set, and one
durable attempt root. A later run never repairs or relabels an earlier root.

## Evidence levels

1. **Packet gate** — focused tests and the exact SurfaceID delta for one
   bounded feature family.
2. **Wave gate** — packet gates, affected fixture sweeps, and broad product
   tests for the wave.
3. **Candidate gate** — clean candidate/tools bindings, candidate-bound local
   proof, release validation, and fresh Salesforce evidence only for rows
   receiving runtime-parity credit.

The readiness sets are independent: `compile-ready`, `test-ready`,
`runtime-parity-ready`, and `non-parity`. Local evidence never earns
runtime-parity credit.

## Shared Dev Hub login

Keep only public `age` recipients and encrypted Salesforce auth URLs in the
private `$HOME/Dev/glade-proof-auth` Git repository. Each host keeps its own
mode-0600 private identity at
`$HOME/.config/glade-proof-auth/identity.txt`. Never copy identities, passwords,
CSV credential exports, or plaintext auth URLs into Git, SMB storage, logs, or
evidence roots.

Initialize each host once:

```bash
scripts/corpus-assurance/dev-hub-auth.sh init-host \
  --identity "$HOME/.config/glade-proof-auth/identity.txt"
```

Add the printed public recipients to `recipients.txt`. With
`glade-dev-hub` connected on the operator workstation, the only credential command is:

```bash
scripts/corpus-assurance/dev-hub-auth.sh put \
  --store "$HOME/Dev/glade-proof-auth" \
  --alias glade-dev-hub \
  --source-alias glade-dev-hub \
  --sf-bin /usr/local/bin/sf
```

Workers authenticate noninteractively with `login` and confirm the live alias
with `verify`. Worker commands that invoke `sf` over SSH must set
`SF_USE_GENERIC_UNIX_KEYCHAIN=true`, matching the helper's headless login.
Back up only an encrypted Git bundle to an operator-owned private location;
the bundle must not contain a host identity.

Generate the secret-filtered worker status in one bounded parallel probe:

```bash
python3 scripts/corpus-assurance/worker-health.py \
  --host worker-a=ssh-user@worker-a.example.internal \
  --host worker-b=ssh-user@worker-b.example.internal \
  --disk worker-a=/proof-data \
  --alias glade-dev-hub \
  --expected-org-id 00D000000000001 \
  --output /absolute/status/WORKER_HEALTH.json
```

The output records connectivity, shared Dev Hub identity, scratch-org quota,
disk, issues, and the optional mode-0600
`$HOME/.config/glade-proof/run.json` marker. A missing marker means the worker
is idle. Raw Salesforce output, SSH stderr, tokens, auth URLs, cookies, and
environment variables are never copied into the status file.

## Live status page

Render the current candidate inputs to Markdown, publishable JSON, and a
static HTML page:

```bash
scripts/render-salesforce-completeness.sh \
  --ledger /absolute/inputs/SURFACE_LEDGER.json \
  --profile /absolute/inputs/SOURCE_PROFILE.json \
  --packet /absolute/inputs/SURFACE_PACKET_MANIFEST.json \
  --binding /absolute/inputs/SOURCE_BINDING.json \
  --worker-health /absolute/status/WORKER_HEALTH.json \
  --output /absolute/status/STATUS.md \
  --json-output /absolute/status/STATUS.json
python3 scripts/render-salesforce-dashboard.py \
  --status /absolute/status/STATUS.json \
  --output /absolute/status/STATUS.html
```

Open `STATUS.html`; it refreshes every 30 seconds. The publishable projection
omits SSH targets, usernames, org IDs, and other private worker identity. If no
current-candidate Salesforce index exists, Salesforce comparison is shown as
`not-started` with every runtime-required row open.

Keep private host mappings and input paths in one operator-owned refresh
command, then run the bounded loop:

```bash
scripts/corpus-assurance/watch-salesforce-status.sh \
  --interval 30 \
  --status /absolute/status/STATUS.json \
  --html /absolute/status/STATUS.html \
  -- /absolute/private/refresh-salesforce-status
```

The refresh command updates worker health, Markdown, and JSON; the watcher
atomically replaces the HTML. Use `--once` for a single refresh. The loop exits
when pipeline status is `closed` or `blocked`; a failed refresh exits nonzero
and leaves the last good page intact.

## Canonical attempt layout

```text
/absolute/glade-evidence/salesforce-adoption/<attempt>/
  glade/
  glade-tools/
  bootstrap/
  bin/
  artifacts/
    inputs/ bindings/ surface/ packets/ inventory/ local-proof/
    salesforce/ validation/ logs/
```

The source worktrees are clean siblings. The attempt root is not a Git
repository and is not used for unrelated work. All paths in authoritative
receipts are absolute and immutable after they are bound.

## Command order

Run the commands in this order. Each producer is create-only and its output is
validated before the next producer starts.

```text
candidate-build
candidate-authority
attempt-init
prepare
usage-draft
usage
replay / merge-replay
surface-scope
surface-local-proof-plan
local-proof-plan
local-proof
release-validate
oracle-profile
oracle-directives-draft
oracle-plan
exclusion-request
authorize-exclusions
dev-hub-authority
oracle-bundle
org-create / org-preflight
salesforce-dispatch / salesforce-run / org-cleanup
salesforce-reconcile
remote-failure-preserve, when a remote phase fails
review-index, after preserving a failed or resumed attempt
cleanup, only after the review index is verified and retained evidence is reviewed
report
```

`attempt` remains available for authorities created by an older operator
step. New runs use `attempt-init` so the cleanup-authority bootstrap does not
require copied hashes.

## Resumable family development

Use `campaign` for fixture-family development before promotion. It is a thin
local runner, not a second assurance authority:

```text
glade-tools corpus assurance campaign \
  --spec /absolute/path/CAMPAIGN.json \
  --state /absolute/path/CAMPAIGN_STATE.json

glade-tools corpus assurance campaign \
  --spec /absolute/path/CAMPAIGN.json \
  --state /absolute/path/CAMPAIGN_STATE.json \
  --promote --out /absolute/path/new-promotion-root
```

The spec freezes unique SurfaceIDs plus required SHA-256 bindings named
`candidate` and `tools`; add every controlled input to the same binding list.
Each phase declares an absolute working directory, direct argv, exact
environment, create-only log, outputs, and earlier dependencies. The runner
preflights the complete graph before executing, hashes executables and passed
outputs, retains every retry, resumes without rerunning unchanged phases, and
refuses concurrent state access. A stale lock must be removed only after
confirming that its recorded process has exited; the next run retains an
abandoned running attempt as interrupted. If raw corpus usage, a sealed support
profile, and a usage draft are present, their dependency order is mandatory.

Promotion copies only the exact spec, state, and compact promotion receipt into
a new directory. Candidate build, inventory preparation, full surface refresh,
release validation, and independent review still run once through the existing
sealed workflow below. Do not run them per fixture family.

`surface-scope` derives the exact Salesforce runtime denominator from a bound
source profile. It includes every `deterministic-mock-required` and
`local-runtime-required` SurfaceID and grants no parity credit.

`surface-local-proof-plan` passes that denominator through the existing strict
fixture planner. Its coverage output retains the exact missing SurfaceIDs when
the plan is incomplete; authoritative local-proof inputs are written only at
100% candidate-runnable coverage.

`candidate-build` starts the product and tools builds concurrently. Its exact
commit binding selects reusable commit-scoped Go build and module caches, so a
retry for the same candidate pair does not pay the full cold-cache cost again.
The command writes progress to
`artifacts/logs/candidate-build.log`. If either build or a later binding step
fails, it also writes the create-only
`artifacts/logs/candidate-build-failure.json`, including the failing stage,
command, exit code, and compiler stderr when available. A failed root remains
the record of that attempt; the next run still uses a successor root.

`review-index` creates a compact review aid after the attempt evidence is
present. Pass each retained raw file with `--artifact`; the index binds the
exact `ATTEMPT.json` bytes, records every absolute source path and hash, and
lists identical bytes once in its object table. For a successor attempt, pass
the prior index with `--predecessor` to retain the exact predecessor hash.
The command never copies, moves, deletes, or relabels raw evidence. Verify the
source files before review with `review-index --verify --index`. The index is a
review aid, not parity proof; the failed attempt root remains the authoritative
record.

## Artifact categories

- **Controlled inputs:** scope inventory, support policy, reviewed docs
  manifest, source profile, fixture inputs, and human decisions. Copy them
  into `artifacts/inputs/` before binding.
- **Planning outputs:** ledgers, profiles, packet Markdown, progress reports,
  and decision drafts. They are regenerable and never replace a receipt.
- **Diagnostics:** named command logs under `artifacts/logs/`. Logs aid
  recovery; they are not parity proof.
- **Review index:** `REVIEW_INDEX.json` and its retained source paths provide a
  compact, deduplicated hash map for reviewers. It does not replace the raw
  failure or Salesforce receipts.
- **Local proof:** candidate-bound fixtures, local decisions, and
  `LOCAL_PROOF.json`.
- **Salesforce proof:** the bound authority, oracle plan, bundle, paired
  lifecycle receipts, shard evidence, and reconciliation packet.
- **Final validation:** release validation and the final report/receipt.

## Retry and successor rule

Authoritative JSON, bound inputs, binaries, freeze files, and reviewed
decisions are create-only. If a command fails after writing any output, leave
the complete attempt intact and start a new attempt root with a new run ID.
Never overwrite, rename, repair, or relabel the failed receipt. If source,
tools, fixture, policy, decision, org, API version, command contract, or
binary changes, start a successor attempt. Delete a failed root only after a
validated successor records it as superseded.

## Resume handoff

`HANDOFF.md` is mutable operator state, not parity proof. Create it in the
packet directory with this shape:

```text
Attempt root: /absolute/attempt
Candidate ref: <ref>
Candidate commit: <recorded by authority>
Tools ref: <ref>
Tools commit: <recorded by authority>
SurfaceIDs: <exact sorted packet row set>
Obligation: <local-runtime-required|deterministic-mock-required|compile-shape-required|explicit-unsupported|hosted-deferred>
Owner: <product or evidence worker>
Current gate: preflight
Last completed command: <command and status>
Next command: <exact next command>
```

Allowed `Current gate:` transitions are:

```text
preflight -> candidate-build -> candidate-authority -> attempt-init
-> prepare -> usage-draft -> usage -> replay / merge-replay
-> local-proof-plan -> local-proof -> release-validate
-> oracle-profile -> oracle-directives-draft -> oracle-plan
-> exclusion-request -> authorize-exclusions -> dev-hub-authority
-> oracle-bundle -> org-create -> org-preflight
-> salesforce-dispatch -> salesforce-run -> org-cleanup
-> salesforce-reconcile -> cleanup -> report -> closed
```

If a remote phase fails, branch from `salesforce-run` or `org-cleanup` to
`remote-failure-preserve -> blocked`; recovery starts in a successor attempt
after the retained root is reviewed. `attempt` is a legacy reader path for
authorities created before `attempt-init`; it is not part of a new run.

On a failed remote phase, `remote-failure-preserve` first writes
`NEXT_ACTION.md`, retains the remote tree and its mode-bearing manifest, then
atomically changes the handoff to `Current gate: blocked`. The next command is
the receipt-specific recovery instruction. No remote root is deleted by the
preservation helper.

## Salesforce reconciliation handoff

`oracle-bundle` requires the attempt-bound
`SALESFORCE_REMOTE_CLEANUP_AUTHORITY.json` and stages it unchanged. After both
shards and both org-cleanup receipts pass, create the durable reconciliation
before deleting the remote attempt root:

```text
glade-tools corpus assurance salesforce-reconcile \
  --oracle-plan /absolute/ORACLE_PLAN.json \
  --shard /absolute/SALESFORCE_SHARD_0.json \
  --dispatch /absolute/SALESFORCE_DISPATCH_0.json \
  --preflight /absolute/ORG_PREFLIGHT_0.json \
  --creation /absolute/ORG_CREATION_0.json \
  --cleanup /absolute/ORG_CLEANUP_0.json \
  --shard /absolute/SALESFORCE_SHARD_1.json \
  --dispatch /absolute/SALESFORCE_DISPATCH_1.json \
  --preflight /absolute/ORG_PREFLIGHT_1.json \
  --creation /absolute/ORG_CREATION_1.json \
  --cleanup /absolute/ORG_CLEANUP_1.json \
  --packet-output /absolute/salesforce-reconciliation-packet \
  --output /absolute/SALESFORCE_RECONCILIATION.json
```

After copying the packet back, verify with `--receipt` and `--packet`. This
mode reads only the retained bytes and modes. `report` accepts the same receipt
and packet through `--salesforce-reconciliation` and `--salesforce-packet`, so
it does not need the deleted executor roots.

## Packet return format

```text
Packet:
Baseline commit:
Product commit:
Owned files:
SurfaceIDs before:
SurfaceIDs after:
Focused RED command and failure:
Focused GREEN command and result:
Fixture command and result:
Ratchet command and result:
Explicit non-parity:
Next highest-priority row:
```

The row set is reconciled by exact SurfaceID, not by estimated counts or
member-name presence. Every selected row has one reviewed disposition and one
owner. Unsupported and hosted behavior remains explicit.

Public documentation changes only after final reconciliation and after the
four readiness sets and exact exclusions are recorded. A local pass is never
described as Salesforce runtime parity.
