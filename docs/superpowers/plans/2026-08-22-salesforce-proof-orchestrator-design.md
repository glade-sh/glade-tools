# Salesforce Proof Orchestrator Design

This design supersedes only the orchestration, dashboard, and Dev Hub
credential sections of `2026-08-20-salesforce-surface-proof-completion.md`.
Its candidate binding, receipt retention, cleanup, and proof-class semantics
remain authoritative.

## Truthful starting point

The current candidate has **Salesforce credit `0 / 8,216`; `8,216` open**.
The retained `203 / 8,216` Salesforce rows are historical evidence bound to
candidate `9452`; they are displayed separately and do not count for the
current candidate. Other tracks remain separate: `8,216 / 8,216` accounted,
`8,177 / 8,216` direct local proof, and `39` terminal local-only rows with zero
runtime/parity credit.

The status importer rejects a snapshot that claims both current-candidate
credit and the retained historical `203` without the distinct historical
binding. A changed candidate invalidates current Salesforce credit.

## Shape

Keep the coordinator in `glade-tools`. One always-on coordinator stores a
SQLite database on its own local disk with WAL enabled. It plans one immutable
campaign and leases one allowlisted `surface-runtime-shard` wrapper per
attempt. It does not distribute the internal 18-step proof pipeline as 18
independent jobs.

The wrapper runs the existing selector, lifecycle, Salesforce execution,
cleanup, reconciliation, and full runtime-batch sealing commands. It transfers
the complete sanitized immutable runtime-batch directory to a coordinator-local
evidence root. The coordinator runs the existing deterministic validator there;
only a cleanup-closed, candidate-bound, validated final batch can add credit.
Workers retain raw logs locally and never upload them.

The required campaign inputs and outputs reuse actual `corpus assurance`
commands:

```text
candidate-build, candidate-authority
attempt-init, prepare
usage-draft, usage
replay, merge-replay
surface-scope, surface-terminal-authority, surface-local-proof-plan
local-proof-plan, local-proof
release-validate
oracle-profile, oracle-directives-draft, oracle-plan
exclusion-request, authorize-exclusions
dev-hub-authority, oracle-bundle
surface-runtime-shard:
  org-create, org-preflight, salesforce-dispatch, salesforce-run,
  org-cleanup, salesforce-reconcile, sealed runtime-batch directory
surface-oracle-index, review-index, cleanup, report
remote-failure-preserve on a failed remote phase
```

Profile, directive, exclusion, replay, release, mismatch, and final-audit
artifacts are campaign-bound inputs or retained outputs. The retained output
set includes `RELEASE_VALIDATION.json`, `ASSURANCE_PROFILE.json`,
`ORACLE_DIRECTIVES.json`, `EXCLUSION_REQUEST.json`,
`EXCLUSION_AUTHORITY.json`, `REPLAY.json`, `SALESFORCE_RECONCILIATION.json`,
`REVIEW_INDEX.json`, and the final `ASSURANCE.json`/receipt pair. Mismatch
rows are inputs to reconciliation and outputs in its row set; they are never
silently converted to credit.

## Durable state and credit

The local database has `campaigns`, `jobs`, `attempts`, `hub_observations`,
`receipts`, `proof_credits`, `hub_capacity`, `scratch_allocations`,
`cleanup_journal`, and `actions`.

- A campaign binds candidate binary plus commit, Tools binary plus commit,
  exact scope, and every controlled input hash.
- Attempts are at-least-once and uniquely keyed by
  `(campaign_id, job_id, generation)`.
- Proof credit is uniquely keyed by `UNIQUE(campaign_id, surface_id)`.
- Credit insertion requires a validated final batch, exact campaign bindings,
  and cleanup state `closed`.
- Work (`queued/running/retryable/failed/closed`), credit
  (`unseen/accepted/rejected`), and cleanup (`pending/running/closed/
  action_required`) are separate projections.

Before creating a scratch org, the coordinator atomically reserves a globally
unique allocation alias and a capacity slot in `hub_capacity`. The allocation
and reservation are recorded before creation. A worker crash before creation,
after creation, after execution, or before credit leaves a journaled attempt.
Another worker may take over cleanup using the journal and reservation; it
cannot take over proof credit from an older generation.

## Credential store

There is one canonical private bare Git remote for the existing age-encrypted
store. Its tracked content is ciphertext per alias plus public recipients.
Workers cache ciphertext only, with one local age identity per worker. Only
the operator writes the remote. The helper exposes `list`, `replace`, `rotate`,
and `rewrap-all` operations in addition to login/verify. Removing a recipient
requires rotating the affected Salesforce credentials first.

Every alias has a ciphertext hash, connection health, and quarantine state.
An alias login or quota failure quarantines that alias and creates an action;
it does not stop healthy aliases. Store-integrity failure (remote divergence,
hash mismatch, decrypt failure, or recipient-set tamper) pauses new Salesforce
work globally. Cleanup, evidence transfer, and status collection continue.

No plaintext credential, auth URL, private identity, or raw CLI output enters
the remote, worker cache, evidence root, dashboard, or public artifacts.

## Transport and visibility

Reuse existing SSH with `BatchMode=yes`. The coordinator dispatches and polls
workers over outbound SSH; workers do not expose an inbound HTTP API. The
coordinator provides runnable `serve` and `worker-once`/SSH entrypoints. In v1,
`serve` binds only to loopback; remote dashboard access uses SSH port
forwarding. The static dashboard uses the standard library and polls redacted
JSON.

The dashboard shows current candidate credit, retained historical credit,
accounting, jobs, attempts/generations, hub health/quota/quarantine, scratch
allocations, cleanup debt, transfer/validation failures, PR/CI state, exact
human actions, and measured ETA. Reuse `worker-health.py` for worker disk
capacity and measure the coordinator evidence root too. Below configured free
space thresholds, pause only new shard leases and show one exact cleanup or
capacity action; cleanup, transfer, and status work continue. A native
`launchd` or user `systemd`
supervisor restarts `serve`; the documented SSH worker command is rerunnable
after a host reboot. No framework, SSE, Kubernetes, HA, shared database, or
arbitrary shell API is needed.

## Rollout boundary

Finish and independently review the current local receipt milestone before
spending broad Salesforce quota. Then run two workers and two manually supplied
Dev Hub aliases through a canary with cleanup and validation closed. Add slots
toward ten Dev Hubs only after the canary is green and the action queue is
empty. Every wave has an immutable campaign, retained batch directory, review,
and dashboard delta.
