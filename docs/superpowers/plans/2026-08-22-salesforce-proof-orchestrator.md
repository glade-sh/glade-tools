# Salesforce Proof Orchestrator Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Run the existing Salesforce proof pipeline through a durable, redacted, multi-machine coordinator with one canonical encrypted Dev Hub store and exact current-candidate proof accounting.

**Architecture:** Add a small coordinator to `glade-tools` using `database/sql`, the already-present `modernc.org/sqlite`, `net/http`, and existing `corpus assurance` commands. The coordinator plans once and leases one `surface-runtime-shard` wrapper. Existing receipt validators run on a coordinator-local transferred batch directory before credit. Reuse outbound SSH; workers do not expose an inbound HTTP service.

**Tech Stack:** Go 1.26, `modernc.org/sqlite`, standard-library HTTP/JSON, `age`, Salesforce CLI, existing Python status scripts, SSH BatchMode, and native `launchd`/user `systemd` supervision.

---

## Scope and authority

This plan supersedes only the orchestration, dashboard, and Dev Hub credential sections of `docs/superpowers/plans/2026-08-20-salesforce-surface-proof-completion.md`. Preserve its candidate binding, receipt retention, cleanup, and separate local/Salesforce/context-bound/evidence-only/non-parity semantics.

The first status snapshot must say:

```text
current candidate Salesforce credit: 0/8216
current candidate Salesforce open: 8216
retained historical Salesforce evidence: 203/8216, bound candidate 9452, not current credit
accounted: 8216/8216
direct local: 8177/8216
terminal local-only: 39, zero runtime/parity credit
```

The importer must reject a current-candidate snapshot that also counts the retained historical `203` without its separate candidate binding. A changed candidate resets current Salesforce credit to zero.

Use a clean worktree from current `origin/main`; never touch the dirty primary checkout. Configure the exact author before every commit:

```bash
git config user.name mattsimonis
git config user.email 720686+mattsimonis@users.noreply.github.com
test "$(git config user.name)" = mattsimonis
test "$(git config user.email)" = 720686+mattsimonis@users.noreply.github.com
```

Before push, `git log --format='%an%x09%ae' origin/main..HEAD | sort -u` must print only `mattsimonis\t720686+mattsimonis@users.noreply.github.com`. Public artifacts must contain no machine names, users, private paths, org IDs, auth URLs, credential values, or credential-key literals.

## PR order

1. Harden the existing canonical encrypted-store helper and redaction.
2. Add campaign planning, SQLite WAL state, leases, reservations, and exact credit.
3. Add the allowlisted `surface-runtime-shard` worker, batch transfer, validation, cleanup takeover, and SSH entrypoints.
4. Add `serve`, the authenticated dashboard/API, status import guards, and native-supervisor instructions.
5. Run the two-worker/two-hub canary, then bounded waves toward ten hubs.

### Task 1: Canonical encrypted Dev Hub store

**Files:**

- Modify: `scripts/corpus-assurance/dev-hub-auth.sh`
- Modify: `scripts/corpus-assurance/dev-hub-auth.test.sh`
- Modify: `scripts/corpus-assurance/worker-health.py`
- Modify: `scripts/corpus-assurance/worker-health_test.py`
- Create: `scripts/corpus-assurance/public-privacy.test.sh`
- Modify: `docs/SALESFORCE_ADOPTION_WORKFLOW.md`

- [ ] **Step 1: Write RED tests for the existing store.** Extend the fake `age`, `sf`, and `git` harness for `recipients.txt` plus per-alias ciphertext. Test operator-only `list`, atomic `replace`, `rotate`, and `rewrap-all`; preservation of unrelated aliases; failed temporary-file cleanup; recipient removal requiring prior credential rotation; and per-alias hash/health/quarantine. Test pull, decrypt, and login failures without persisting credential values.

- [ ] **Step 2: Verify RED.** Run `bash scripts/corpus-assurance/dev-hub-auth.test.sh`; expect failure for the new management and hash-reload assertions.

- [ ] **Step 3: Implement the minimal helper extension.** Keep `init-host`, `put`, `login`, and `verify`; add the four management operations and deterministic store/alias hashes. Only the operator may write the canonical private bare Git remote. Workers pull ciphertext-only caches and load a local Salesforce CLI alias through stdin when the expected hash changes. Use atomic replacement, `set -euo pipefail`, `set +x`, `umask 077`, `git pull --ff-only`, and mode-0600 identities. A store-integrity failure pauses new Salesforce dispatch globally; one alias login/quota failure quarantines only that alias.

- [ ] **Step 4: Add privacy tests.** Make `public-privacy.test.sh` scan both new plan documents and all generated public projections for machine names, private paths, auth URLs, credential values, and credential-key literals. The test must fail closed and print only the offending relative path and category.

- [ ] **Step 5: Verify and commit.** Run the auth, worker-health, and privacy tests plus `git diff --check`; commit `Harden canonical Dev Hub credential store` with the exact author identity. No credential test fixture enters Git.

### Task 2: Campaign planner, SQLite state, and exact credit

**Files:**

- Create: `internal/corpusassurance/orchestrator.go`
- Create: `internal/corpusassurance/orchestrator_test.go`
- Modify: `internal/toolcli/corpus_assurance_command.go`
- Modify: `internal/toolcli/corpus_assurance_command_test.go`
- Modify: `go.mod`
- Modify: `go.sum`

- [ ] **Step 1: Write RED schema tests.** With `t.TempDir()`, assert local open enables WAL; campaign creation binds candidate binary and commit, Tools binary and commit, exact scope, and all controlled input hashes; planner emits one immutable `surface-runtime-shard` job per disjoint shard; unknown kinds and drift are rejected; leases are at-least-once; attempts are unique by `(campaign_id, job_id, generation)`; proof credit is unique by `(campaign_id, surface_id)`; and credit is refused unless the final batch is validated and cleanup is `closed`.

- [ ] **Step 2: Write RED status contradiction tests.** Assert that `0/8216` current plus historical `203/8216` bound candidate `9452` imports successfully, while a single blended `203/8216` current result is rejected.

- [ ] **Step 3: Verify RED.** Run `go test ./internal/corpusassurance -run 'TestOrchestrator'`; expect failure because the coordinator API does not exist.

- [ ] **Step 4: Implement the local schema.** Use `database/sql` and `modernc.org/sqlite`, absolute local database paths, `PRAGMA journal_mode=WAL`, and a bounded busy timeout. Create `campaigns`, `jobs`, `attempts`, `hub_observations`, `receipts`, `proof_credits`, `hub_capacity`, `scratch_allocations`, `cleanup_journal`, and `actions`. Atomically reserve a globally unique allocation alias and hub-capacity slot before scratch creation. Keep work, credit, and cleanup states separate.

- [ ] **Step 5: Implement typed operations and CLI.** Provide `plan`, `init`, `enqueue`, `status`, `lease`, `heartbeat`, `receipt`, `reserve`, and `cleanup` under `glade-tools corpus assurance orchestrator`; reject arbitrary command strings. `RecordReceipt` runs the deterministic validator against the coordinator-local batch directory and inserts credit only for a cleanup-closed final batch.

The plan input/output graph must reuse these existing commands and artifacts, once per campaign rather than once per job: `candidate-build`/`candidate-authority`; `attempt-init`/`prepare`; `usage-draft`/`usage`; `replay`/`merge-replay`; `surface-scope`/`surface-terminal-authority`/`surface-local-proof-plan`; `local-proof-plan`/`local-proof`; `release-validate`; `oracle-profile`/`oracle-directives-draft`/`oracle-plan`; `exclusion-request`/`authorize-exclusions`; `dev-hub-authority`/`oracle-bundle`; and final `surface-oracle-index`/`review-index`/`cleanup`/`report`. The job wrapper owns `org-create`, `org-preflight`, `salesforce-dispatch`, `salesforce-run`, `org-cleanup`, `salesforce-reconcile`, mismatch retention, and sealed runtime-batch output.

The campaign retains these named artifacts: `RELEASE_VALIDATION.json`,
`ASSURANCE_PROFILE.json`, `ORACLE_DIRECTIVES.json`, `EXCLUSION_REQUEST.json`,
`EXCLUSION_AUTHORITY.json`, `REPLAY.json`,
`SALESFORCE_RECONCILIATION.json`, `REVIEW_INDEX.json`, and the final
`ASSURANCE.json`/receipt pair. Mismatch rows are retained in
`SALESFORCE_RECONCILIATION.json` and its transferred batch inputs/outputs;
they never become credit without a valid matched result.

- [ ] **Step 6: Verify and commit.** Run `go test ./internal/corpusassurance -run 'TestOrchestrator'`, `go test ./internal/toolcli -run 'TestCorpusAssuranceOrchestrator'`, and `git diff --check`; commit `Add candidate-bound orchestrator state`.

### Task 3: Runtime shard wrapper, transfer, cleanup takeover, and SSH

**Files:**

- Create: `internal/corpusassurance/orchestrator_worker.go`
- Create: `internal/corpusassurance/orchestrator_worker_test.go`
- Modify: `internal/toolcli/corpus_assurance_command.go`
- Modify: `docs/SALESFORCE_ADOPTION_WORKFLOW.md`

- [ ] **Step 1: Write crash and transfer RED tests.** Test worker crashes before scratch creation, after creation, after Salesforce execution, and before credit. Each case must retain its attempt, reserve state, cleanup journal, and no-credit result. Test atomic transfer of a complete sanitized immutable runtime-batch directory to the coordinator-local evidence root; reject partial, unbound, duplicate, or candidate-drifted directories.

- [ ] **Step 2: Verify RED.** Run `go test ./internal/corpusassurance -run 'TestOrchestratorWorker'`.

- [ ] **Step 3: Implement `surface-runtime-shard`.** The worker receives one immutable lease and fixed wrapper inputs, runs the selector/lifecycle/execution/cleanup/full-batch sealing sequence, journals allocation before creation, sends heartbeats, and writes raw logs only locally. It transfers the complete sanitized batch directory over existing outbound SSH to a temporary coordinator-local directory, fsyncs/renames it atomically, and reports its manifest hash. The coordinator runs `salesforce-reconcile` verification and the existing batch/index validators locally before credit.

- [ ] **Step 4: Implement cleanup takeover.** A new worker can claim an open cleanup journal after lease expiry, validate the reservation and scratch alias, run existing `org-cleanup` and reconciliation, and close the journal. Cleanup takeover cannot alter an older generation's proof credit.

- [ ] **Step 5: Add runnable entrypoints.** Add `worker-once` and an SSH dispatch path under `glade-tools corpus assurance orchestrator`. Use `ssh -o BatchMode=yes` for coordinator-to-worker execution; workers expose no inbound HTTP API. Preserve stable failure codes and private raw logs.

- [ ] **Step 6: Verify and commit.** Run focused worker/CLI tests and `git diff --check`; commit `Run sealed runtime shards over SSH`.

### Task 4: Serve, dashboard, status guards, and supervision

**Files:**

- Create: `internal/corpusassurance/orchestrator_http.go`
- Create: `internal/corpusassurance/orchestrator_http_test.go`
- Modify: `internal/toolcli/corpus_assurance_command.go`
- Modify: `scripts/render-salesforce-dashboard.py`
- Modify: `scripts/render_salesforce_dashboard_test.py`
- Modify: `scripts/corpus-assurance/watch-salesforce-status.sh`
- Modify: `scripts/corpus-assurance/watch-salesforce-status.test.sh`
- Modify: `scripts/corpus-assurance/public-privacy.test.sh`
- Modify: `docs/SALESFORCE_ADOPTION_WORKFLOW.md`

- [ ] **Step 1: Write RED API/status tests.** With `httptest.NewServer`, require authentication for loopback/private `serve` endpoints `GET /api/status`, `GET /api/jobs`, and `GET /api/actions`; worker mutations remain SSH-backed. Reject bad JSON, stale generations, candidate drift, unredacted fields, and the current/historical `203` contradiction.

- [ ] **Step 2: Verify RED.** Run `go test ./internal/corpusassurance -run 'TestOrchestratorHTTP'`.

- [ ] **Step 3: Implement `serve`.** Use `net/http` and `encoding/json`, bounded requests, no authorization-header logging, and a loopback-only listener. Document SSH port forwarding for remote dashboard access. Serve the existing static renderer and redacted JSON. Do not add TLS machinery, a frontend dependency, SSE, or worker HTTP listener.

- [ ] **Step 4: Update the dashboard.** Show current `0/8216` and `8216 open`, retained historical `203` with candidate `9452` separately, accounting/local/terminal tracks, shard attempts/generations, alias health/quarantine/quota, reservations, scratch cleanup, transfer/validation errors, PR/CI, exact action cards, and measured ETA. Reuse `worker-health.py` for worker disk capacity and measure the coordinator evidence root. Pause only new shard leases below configured free-space thresholds and emit one exact cleanup or capacity action. Preserve atomic refresh and the existing 30-second watch loop.

- [ ] **Step 5: Document native restart.** Add exact `launchd` instructions for the coordinator Mac and user `systemd` instructions for Linux workers, including start, status, logs, stop, and restart commands. The only worker operation is the rerunnable SSH `worker-once` entrypoint.

- [ ] **Step 6: Verify and commit.** Run focused Go tests, `python3 -m unittest scripts/render_salesforce_dashboard_test.py`, `bash scripts/corpus-assurance/watch-salesforce-status.test.sh`, `bash scripts/corpus-assurance/public-privacy.test.sh`, and `git diff --check`; commit `Serve redacted proof orchestration status`.

### Task 5: Two-worker canary and scale gate

**Files:**

- Modify: `docs/SALESFORCE_ADOPTION_WORKFLOW.md`
- Create: private, untracked canary inputs, transferred batch directories, and receipts outside the repository.

- [ ] **Step 1: Plan once.** Freeze candidate binary/commit, Tools binary/commit, scope, release/profile/directive/exclusion/replay inputs, final audit inputs, and all controlled hashes. Run the existing once-per-campaign command graph from Task 2; enqueue only disjoint `surface-runtime-shard` jobs.

- [ ] **Step 2: Run two workers and two manually supplied aliases.** Require store hash agreement, pre-creation allocation reservation, heartbeats, complete batch transfer, local validation, Salesforce reconciliation, cleanup closure, and no duplicate credit. A green execution is not parity until independent review passes.

- [ ] **Step 3: Verify.** Run existing reconciliation, index, review, cleanup, report, and privacy scans. Inspect mismatch and inconclusive rows rather than crediting them. Test each crash point in the canary harness.

- [ ] **Step 4: Scale safely.** Add aliases/worker slots one at a time toward ten healthy Dev Hubs. A failed alias is quarantined; a store-integrity failure pauses all new Salesforce leases. Cleanup takeover remains available. Every wave has an immutable campaign, complete coordinator-local batch directory, review, and dashboard delta.

- [ ] **Step 5: Publish the milestone.** Report current-candidate credit, retained historical evidence, open rows, exact bindings, cleanup state, actions, throughput, and ETA. Never blend historical or terminal local-only rows into current Salesforce credit.

## Self-review checklist

- Current candidate starts at `0/8216`; historical `203` is separately bound to `9452`.
- One canonical private age-encrypted Git remote; operator-only writes; ciphertext-only worker caches.
- Alias rotation/listing/rewrapping and quarantine are explicit; only store-integrity failure pauses globally.
- One planned `surface-runtime-shard` wrapper; no distributed internal phase DAG.
- Complete sanitized batches transfer to the coordinator; validators run there before credit.
- Attempts and credits use the required campaign-scoped uniqueness constraints.
- Allocation aliases/reservations are global, pre-created, and cleanup-takeover safe.
- Transport is outbound SSH; `serve` is for dashboard/API only.
- Public privacy tests include both new documents and generated projections.
