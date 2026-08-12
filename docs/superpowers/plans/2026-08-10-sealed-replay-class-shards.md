# Sealed Replay Class Shards Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prove required local tests through a complete, deterministic, sealed class-shard partition without extending the 15-minute per-command limit.

**Architecture:** Keep Glade's existing `test --shard-count/--shard-index` selection authoritative. The assurance adapter derives a fixed partition from the sealed host manifest, records every shard receipt with exact candidate/tool/cache bindings, and marks a repository test-ready only when the complete partition has exactly one passing receipt per index.

**Tech Stack:** Go, existing `internal/corpusassurance` receipt model, existing Glade CLI class sharding.

---

### Task 1: Specify and test a complete sealed partition

**Files:**

- Modify: `internal/corpusassurance/replay_test.go`
- Modify: `internal/corpusassurance/replay.go`

- [ ] **Step 1: Write the failing test**

Add a minimal replay-merge case with a required-test repository carrying two class-shard receipts. Make the valid case contain indices `0` and `1` for count `2`; mutate it to omit, duplicate, or alter an index and require `ValidateReplayMerge` to reject each case.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/corpusassurance -run '^TestValidateReplayMergeRejectsIncompleteClassShardPartition$' -count=1`

Expected: FAIL because the current receipt model accepts only one full `test` command.

- [ ] **Step 3: Write minimal implementation**

Extend only the replay receipt/result validation needed to represent an internally-derived class-shard partition. Derive fixed `test --shard-count N --shard-index I --perf-json ...` commands in `replay.go`; require every receipt to retain normal command, cache, hash, workspace, candidate, and tool bindings. Keep the existing unsharded path for repositories whose complete test command fits the limit.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/corpusassurance -run '^TestValidateReplayMergeRejectsIncompleteClassShardPartition$' -count=1`

Expected: PASS.

### Task 2: Bind host staging and reconciliation

**Files:**

- Modify: `internal/corpusassurance/inventory.go`
- Modify: `internal/corpusassurance/replay.go`
- Modify: `internal/corpusassurance/replay_test.go`

- [ ] **Step 1: Write the failing test**

Add a manifest/replay test that proves the partition comes from sealed input, stages the exact snapshot on each required host, and rejects a caller-selected shard count or a replay result from an unassigned host.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/corpusassurance -run '^TestReplayClassShardPartitionIsSealedAndHostBound$' -count=1`

Expected: FAIL because host manifests presently have exclusive repository ownership.

- [ ] **Step 3: Write minimal implementation**

Permit only the sealed required-test partition to stage an identical immutable snapshot on the designated replay hosts. Each test shard must retain its own preceding cache-seeding check receipt; reconciliation accepts the repository only after the complete derived test partition passes.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/corpusassurance -run '^TestReplayClassShardPartitionIsSealedAndHostBound$' -count=1`

Expected: PASS.

### Task 3: Verify and freeze the adapter

**Files:**

- Modify: `internal/corpusassurance/replay_test.go`

- [ ] **Step 1: Add focused regressions**

Cover timeout retention, candidate/tool postflight mutation rejection, cache proof for every test shard, and failure propagation from any partition member.

- [ ] **Step 2: Run focused verification**

Run: `go test ./internal/corpusassurance -run 'Test(Replay|ValidateReplayMerge|MergeReplayFromFiles)' -count=1`

Expected: PASS.

- [ ] **Step 3: Commit the toolchain before external execution**

Run: `git add internal/corpusassurance/replay.go internal/corpusassurance/inventory.go internal/corpusassurance/replay_test.go docs/superpowers/plans/2026-08-10-sealed-replay-class-shards.md && git commit -m 'seal replay class shard partitions'`

Expected: a clean committed toolchain that can seed a new immutable attempt.
