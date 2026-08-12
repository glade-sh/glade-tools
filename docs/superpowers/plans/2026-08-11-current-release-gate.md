# Current Release Gate Plan

**Goal:** Validate current Glade Tools release packages without making an unavailable archive-evidence mount a release prerequisite.

**Approach:** Keep the four sealed release checks. Replace only the third check (`glade-tools go test ./...`) with the current assurance/tool CLI packages. The existing `scripts/release-check.sh` remains the full current release gate and already excludes archival `surfaceledger` evidence probes.

### Task 1: Seal the intended tools command

**Files:**

- Modify: `internal/corpusassurance/release.go`
- Modify: `internal/corpusassurance/release_test.go`
- Modify: `internal/corpusassurance/bundle.go`

- [x] Add a test requiring the third fixed command to use only the current assurance/tool CLI packages, never `./...`.
- [x] Run the focused test and observe it fail against the broad archival command.
- [x] Change the one fixed command and its receipt validator; retain four checks and all archive tests unchanged.
- [ ] Run focused corpus-assurance tests and commit the frozen toolchain.

### Task 2: Renew current proof

- [ ] Issue a fresh candidate and attempt bound to the committed toolchain and frozen `IN_SCOPE.json`.
- [ ] Run local and Casper exact-candidate replay, then the sealed release validation.

### Task 3: Salesforce and closeout

- [ ] Run two fresh Razor Oracle shards and cleanup receipts.
- [ ] Reconcile fail-closed, generate `ASSURANCE.json`, `RECEIPT.json`, and the self-contained HTML explorer.
- [ ] Obtain one final adversarial review, push one ready review-only PR, and leave hosts/processes/orgs clean.
