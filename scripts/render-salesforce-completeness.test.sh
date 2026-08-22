#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
renderer="$root/scripts/render-salesforce-completeness.sh"
tmp="$(mktemp -d "${TMPDIR:-/tmp}/glade-completeness-test.XXXXXX")"
trap 'find "$tmp" -depth -delete' EXIT

write_inputs() {
  printf '%s\n' '{"rows":[{"surfaceId":"a"},{"surfaceId":"b"},{"surfaceId":"c"},{"surfaceId":"d"},{"surfaceId":"e"},{"surfaceId":"f"},{"surfaceId":"hosted"}]}' >"$tmp/ledger.json"
  printf '%s\n' '{"totalOpenRows":2,"packets":[]}' >"$tmp/packet.json"
  printf '%s\n' '{"status":"exact-candidate-bound","historicalCredit":false,"candidate":{"path":"/candidate","commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","binarySha256":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc","clean":true},"tools":{"path":"/tools","commit":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","binarySha256":"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd","clean":true},"moduleReplacement":{"resolvedPath":"/candidate","requiredPath":"/candidate"}}' >"$tmp/binding.json"
  printf '%s\n' '{"candidate":{"commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"tools":{"commit":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}}' >"$tmp/attempt.json"
  printf '%s\n' '{"candidate":{"commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"tools":{"commit":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},"commands":[{"passed":true,"exitCode":0},{"passed":true,"exitCode":0},{"passed":true,"exitCode":0}]}' >"$tmp/release.json"
  printf '%s\n' '{"repositorySurfaceRows":[{"repositoryId":"one","runtimeParityReady":true,"nonParity":false},{"repositoryId":"two","runtimeParityReady":false,"nonParity":false}],"repositorySummaries":[{"repositoryId":"one","surfaceCount":1,"runtimeParityReady":true,"nonParity":false},{"repositoryId":"two","surfaceCount":1,"runtimeParityReady":false,"nonParity":true}]}' >"$tmp/assurance.json"
  printf '%s\n' '{"status":"current-candidate-diagnostic","candidate":{"gladeCommit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","toolsCommit":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},"completion":{"definition":"Every frozen project passes cold check and full local tests.","completeProjects":6,"totalProjects":10,"percent":60.0,"remainingProjects":4},"validSourceSupport":{"completeProjects":6,"totalProjects":8,"percent":75.0,"remainingProjects":2},"accounting":{"accountedProjects":10,"totalProjects":10,"percent":100.0,"unclassifiedProjects":0},"checks":{"successfulCommands":7,"totalProjects":10,"percent":70.0},"observedTests":{"scope":"Eligible projects only.","total":17503,"passed":17471,"failed":13,"unsupported":19,"passPercent":99.8},"creditBoundary":"Diagnostic only; zero Salesforce parity credit."}' >"$tmp/private-status.json"
  printf '%s\n' '{"total":7,"byDisposition":{"compile-shape-required":2,"deterministic-mock-required":2,"local-runtime-required":2,"hosted-deferred":1},"rows":[{"surfaceId":"a","disposition":"compile-shape-required","ledgerShape":"signature-known","behavior":"supported","evidence":"fixture"},{"surfaceId":"b","disposition":"compile-shape-required","ledgerShape":"signature-known","behavior":"passive","evidence":"none","gapClass":"missing-evidence"},{"surfaceId":"c","disposition":"deterministic-mock-required","ledgerShape":"signature-known","behavior":"supported","evidence":"fixture-and-oracle"},{"surfaceId":"d","disposition":"deterministic-mock-required","ledgerShape":"signature-known","behavior":"supported","evidence":"fixture"},{"surfaceId":"e","disposition":"local-runtime-required","ledgerShape":"signature-known","behavior":"supported","evidence":"fixture"},{"surfaceId":"f","disposition":"local-runtime-required","ledgerShape":"signature-known","behavior":"supported","evidence":"none","gapClass":"missing-evidence"},{"surfaceId":"hosted","disposition":"hosted-deferred","ledgerShape":"type-known","behavior":"passive","evidence":"none"}],"inputs":{"files":[{"name":"policy","sha256":"eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"}]}}' >"$tmp/profile.json"
  printf '%s\n' '{"schemaVersion":1,"generatedAt":"2026-08-20T20:00:00Z","alias":"glade-dev-hub","expectedOrgId":"00D000000000001","workers":[{"name":"private-host-a","host":"ssh-user@worker-a.example.internal","healthy":true,"reachable":true,"devHub":{"connected":true,"alias":"glade-dev-hub","orgId":"00D000000000001","username":"worker@example.invalid","activeScratchOrgsRemaining":3,"dailyScratchOrgsRemaining":6},"diskFreeBytes":107374182400,"run":null,"issues":[]},{"name":"private-host-b","host":"ssh-user@worker-b.example.internal","healthy":false,"reachable":false,"devHub":{"connected":false,"alias":"glade-dev-hub","orgId":null,"username":null,"activeScratchOrgsRemaining":null,"dailyScratchOrgsRemaining":null},"diskFreeBytes":null,"run":null,"issues":["unreachable"]}]}' >"$tmp/worker-health.json"
}

run_renderer() {
  local index_args=()
  [[ -f "$tmp/salesforce-index.json" ]] && index_args=(--salesforce-index "$tmp/salesforce-index.json" --salesforce-scope "$tmp/salesforce-scope.json")
  "$renderer" \
    --ledger "$tmp/ledger.json" \
    --profile "$tmp/profile.json" \
    --packet "$tmp/packet.json" \
    --binding "$tmp/binding.json" \
    --corpus "$tmp/assurance.json" \
    --attempt "$tmp/attempt.json" \
    --private-corpus-status "$tmp/private-status.json" \
    --release "$tmp/release.json" \
    --worker-health "$tmp/worker-health.json" \
    --output "$tmp/status.md" \
    --json-output "$tmp/status.json" \
    "${index_args[@]}"
}

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

write_salesforce_index() {
  printf '%s\n' "$(jq -n \
    --arg profile "$(sha256_file "$tmp/profile.json")" \
    --arg ledger "$(sha256_file "$tmp/ledger.json")" \
    --arg policy "$(jq -r '[.inputs.files[] | select(.name == "policy") | .sha256] | .[0]' "$tmp/profile.json")" \
    '{schemaVersion:1,kind:"all-runtime",sourceProfileSha256:$profile,ledgerSha256:$ledger,policySha256:$policy,total:3,rows:[{surfaceId:"c",disposition:"deterministic-mock-required"},{surfaceId:"d",disposition:"local-runtime-required"},{surfaceId:"e",disposition:"local-runtime-required"}]}')" >"$tmp/salesforce-scope.json"
  printf '%s\n' "$(jq -n \
    --arg candidate "$(jq -r '.candidate.commit' "$tmp/binding.json")" \
    --arg tools "$(jq -r '.tools.commit' "$tmp/binding.json")" \
    --arg candidateSha256 "$(jq -r '.candidate.binarySha256' "$tmp/binding.json")" \
    --arg toolsSha256 "$(jq -r '.tools.binarySha256' "$tmp/binding.json")" \
    --arg profile "$(sha256_file "$tmp/profile.json")" \
    --arg ledger "$(sha256_file "$tmp/ledger.json")" \
    --arg scope "$(sha256_file "$tmp/salesforce-scope.json")" \
    '{schemaVersion:1,kind:"all-runtime",candidate:{commit:$candidate,binarySha256:$candidateSha256},tools:{commit:$tools,binarySha256:$toolsSha256},sourceProfileSha256:$profile,ledgerSha256:$ledger,scopeSha256:$scope,policySha256:"eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",total:3,counts:{adjudicated:2,matched:2,explicitNonParity:0,productMismatch:0,inconclusive:0,open:1},rows:[{surfaceId:"c",state:"matched"},{surfaceId:"d",state:"matched"},{surfaceId:"e",state:"open"}],runtimeBatches:[{bindingsSha256:"1111111111111111111111111111111111111111111111111111111111111111",finalAuditSha256:"2222222222222222222222222222222222222222222222222222222222222222",localSummarySha256:"3333333333333333333333333333333333333333333333333333333333333333",manifestSha256:"4444444444444444444444444444444444444444444444444444444444444444",mismatchReviewSha256:"5555555555555555555555555555555555555555555555555555555555555555",oracleResultsSha256:"6666666666666666666666666666666666666666666666666666666666666666",profileSha256:"7777777777777777777777777777777777777777777777777777777777777777",rawReconciliationSha256:"8888888888888888888888888888888888888888888888888888888888888888",surfaceIds:["c","d"]}]}')" >"$tmp/salesforce-index.json"
}

write_inputs
run_renderer
grep -F 'Surface proof completion: **57.1%** (8 / 14 required checkpoints)' "$tmp/status.md"
grep -F 'Remaining to 100%: **6 required checkpoints**' "$tmp/status.md"
grep -F 'Inventory accounting: **71.4%** (5 / 7 rows accounted)' "$tmp/status.md"
grep -F 'Local evidence: **66.7%** (4 / 6 required rows)' "$tmp/status.md"
grep -F 'Salesforce comparison: **0.0%** (0 / 4 runtime rows)' "$tmp/status.md"
grep -F 'Formal corpus assurance: **50.0%** (1 / 2 repositories complete)' "$tmp/status.md"
grep -F 'Private project completion: **60.0%** (6 / 10 complete; 4 remaining)' "$tmp/status.md"
grep -F 'Salesforce-valid private support: **75.0%** (6 / 8 complete; 2 remaining)' "$tmp/status.md"
grep -F 'Private project accounting: **100.0%** (10 / 10 classified; 0 unclassified)' "$tmp/status.md"
grep -F 'Private project check readiness: **70.0%** (7 / 10 checks passed)' "$tmp/status.md"
grep -F 'Eligible private test pass rate: **99.8%** (17,471 / 17,503; diagnostic subset)' "$tmp/status.md"
grep -F 'Release validation: **100.0%** (3 / 3 commands passed)' "$tmp/status.md"
grep -F 'Program status: **NOT DONE**' "$tmp/status.md"
grep -F '100% means every required surface checkpoint, current private repository, and release command is complete.' "$tmp/status.md"
jq -e '
  .schemaVersion == 1
  and .programStatus == "NOT DONE"
  and .completion == {percent:57.1, complete:8, required:14, remaining:6}
  and .candidate.glade == "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  and .candidate.tools == "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
  and .salesforce.state == "not-started"
  and .salesforce.outcomes == {adjudicated:0, matched:0, explicitNonParity:0, productMismatch:0, inconclusive:0, open:4}
  and (.machines | length) == 2
  and [.machines[].name] == ["worker-1", "worker-2"]
  and .machines[0].healthy == true
  and .machines[1].issues == ["unreachable"]
  and ([.machines[] | has("host")] | any | not)
  and ([.machines[].devHub | has("orgId") or has("username")] | any | not)
  and .action.summary == "Restore worker health"
  and (.action.reason | contains("worker-2"))
  and (.action.action | length) > 0
  and (.action.clearsWhen | length) > 0
' "$tmp/status.json"

printf '%s\n' '{"rows":[{"surfaceId":"a"},{"surfaceId":"c"},{"surfaceId":"d"},{"surfaceId":"e"}]}' >"$tmp/ledger.json"
printf '%s\n' '{"totalOpenRows":0,"packets":[]}' >"$tmp/packet.json"
printf '%s\n' '{"repositorySurfaceRows":[{"repositoryId":"one","runtimeParityReady":true,"nonParity":false},{"repositoryId":"two","runtimeParityReady":false,"nonParity":true}],"repositorySummaries":[{"repositoryId":"one","surfaceCount":1,"runtimeParityReady":true,"nonParity":false},{"repositoryId":"two","surfaceCount":1,"runtimeParityReady":false,"nonParity":true}]}' >"$tmp/assurance.json"
printf '%s\n' '{"total":4,"byDisposition":{"compile-shape-required":1,"deterministic-mock-required":1,"local-runtime-required":2,"hosted-deferred":0},"rows":[{"surfaceId":"a","disposition":"compile-shape-required","ledgerShape":"signature-known","behavior":"supported","evidence":"fixture"},{"surfaceId":"c","disposition":"deterministic-mock-required","ledgerShape":"signature-known","behavior":"supported","evidence":"fixture-and-oracle"},{"surfaceId":"d","disposition":"local-runtime-required","ledgerShape":"signature-known","behavior":"supported","evidence":"fixture-and-oracle"},{"surfaceId":"e","disposition":"local-runtime-required","ledgerShape":"signature-known","behavior":"passive","evidence":"fixture-and-oracle"}],"inputs":{"files":[{"name":"policy","sha256":"eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"}]}}' >"$tmp/profile.json"
run_renderer
grep -F 'Surface proof completion: **70.0%** (7 / 10 required checkpoints)' "$tmp/status.md"
grep -F 'Remaining to 100%: **3 required checkpoints**' "$tmp/status.md"
grep -F 'Program status: **NOT DONE**' "$tmp/status.md"

write_salesforce_index
run_renderer
jq -e '.salesforce.state == "in-progress" and .salesforce.outcomes == {adjudicated:2,matched:2,explicitNonParity:0,productMismatch:0,inconclusive:0,open:1} and .tiers.salesforceComparison == {complete:2,required:3}' "$tmp/status.json"

good_index="$tmp/salesforce-index.good.json"
cp "$tmp/salesforce-index.json" "$good_index"
expect_index_rejected() {
  local label="$1"
  if run_renderer 2>"$tmp/$label.err"; then
    echo "renderer accepted invalid Salesforce index: $label" >&2
    exit 1
  fi
  grep -F 'Salesforce index is stale or invalid' "$tmp/$label.err"
  cp "$good_index" "$tmp/salesforce-index.json"
}

jq '.candidate.commit = "cccccccccccccccccccccccccccccccccccccccc"' "$good_index" >"$tmp/salesforce-index.json"
expect_index_rejected stale-candidate
jq '.candidate.binarySha256 = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"' "$good_index" >"$tmp/salesforce-index.json"
expect_index_rejected binary-hash-drift
jq 'del(.runtimeBatches)' "$good_index" >"$tmp/salesforce-index.json"
expect_index_rejected missing-runtime-batches
jq '.rows += [{surfaceId:"forged",state:"matched"}] | .total = 4 | .counts.adjudicated = 3 | .counts.matched = 3' "$good_index" >"$tmp/salesforce-index.json"
expect_index_rejected forged-matched-row
jq '.policySha256 = "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"' "$good_index" >"$tmp/salesforce-index.json"
expect_index_rejected policy-sha-drift
jq '.rows[0].state = "explicit-nonparity"' "$good_index" >"$tmp/salesforce-index.json"
expect_index_rejected non-match-state
cp "$tmp/binding.json" "$tmp/binding.good.json"
jq 'del(.candidate.binarySha256)' "$tmp/binding.good.json" >"$tmp/binding.json"
expect_index_rejected missing-binary-hash
cp "$tmp/binding.good.json" "$tmp/binding.json"
cp "$tmp/profile.json" "$tmp/profile.good.json"
jq '.drift = true' "$tmp/profile.good.json" >"$tmp/profile.json"
expect_index_rejected stale-profile
cp "$tmp/profile.good.json" "$tmp/profile.json"
cp "$tmp/ledger.json" "$tmp/ledger.good.json"
jq '.drift = true' "$tmp/ledger.good.json" >"$tmp/ledger.json"
expect_index_rejected stale-ledger
cp "$tmp/ledger.good.json" "$tmp/ledger.json"
cp "$tmp/salesforce-scope.json" "$tmp/salesforce-scope.good.json"
jq '.drift = true' "$tmp/salesforce-scope.good.json" >"$tmp/salesforce-scope.json"
expect_index_rejected scope-byte-drift
cp "$tmp/salesforce-scope.good.json" "$tmp/salesforce-scope.json"
cp "$tmp/profile.json" "$tmp/profile.good.json"
jq '.inputs.files[0].sha256 = "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"' "$tmp/profile.good.json" >"$tmp/profile.json"
expect_index_rejected policy-binding-drift
cp "$tmp/profile.good.json" "$tmp/profile.json"
jq '.rows[1].surfaceId = .rows[0].surfaceId' "$good_index" >"$tmp/salesforce-index.json"
expect_index_rejected duplicate-row
jq '.rows |= reverse' "$good_index" >"$tmp/salesforce-index.json"
expect_index_rejected unsorted-row
jq '.rows[0].surfaceId = "unknown"' "$good_index" >"$tmp/salesforce-index.json"
expect_index_rejected unknown-row
jq '.rows |= .[0:2]' "$good_index" >"$tmp/salesforce-index.json"
expect_index_rejected missing-row
jq '.counts.matched = 1' "$good_index" >"$tmp/salesforce-index.json"
expect_index_rejected count-drift

printf '%s\n' '{"status":"current-candidate-diagnostic","candidate":{"gladeCommit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","toolsCommit":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},"completion":{"definition":"Every frozen project passes cold check and full local tests.","completeProjects":10,"totalProjects":10,"percent":100.0,"remainingProjects":0},"checks":{"successfulCommands":10,"totalProjects":10,"percent":100.0},"observedTests":{"scope":"All projects.","total":18000,"passed":18000,"failed":0,"unsupported":0,"passPercent":100.0},"creditBoundary":"Diagnostic only; zero Salesforce parity credit."}' >"$tmp/private-status.json"
printf '%s\n' '{"schemaVersion":1,"kind":"all-runtime","candidate":{"commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","binarySha256":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"},"tools":{"commit":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","binarySha256":"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"},"sourceProfileSha256":"'"$(sha256_file "$tmp/profile.json")"'","ledgerSha256":"'"$(sha256_file "$tmp/ledger.json")"'","scopeSha256":"'"$(sha256_file "$tmp/salesforce-scope.json")"'","policySha256":"eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee","total":3,"counts":{"adjudicated":3,"matched":3,"explicitNonParity":0,"productMismatch":0,"inconclusive":0,"open":0},"rows":[{"surfaceId":"c","state":"matched"},{"surfaceId":"d","state":"matched"},{"surfaceId":"e","state":"matched"}],"runtimeBatches":[{"bindingsSha256":"1111111111111111111111111111111111111111111111111111111111111111","finalAuditSha256":"2222222222222222222222222222222222222222222222222222222222222222","localSummarySha256":"3333333333333333333333333333333333333333333333333333333333333333","manifestSha256":"4444444444444444444444444444444444444444444444444444444444444444","mismatchReviewSha256":"5555555555555555555555555555555555555555555555555555555555555555","oracleResultsSha256":"6666666666666666666666666666666666666666666666666666666666666666","profileSha256":"7777777777777777777777777777777777777777777777777777777777777777","rawReconciliationSha256":"8888888888888888888888888888888888888888888888888888888888888888","surfaceIds":["c","d","e"]}]}' >"$tmp/salesforce-index.json"
run_renderer
grep -F 'Program status: **DONE**' "$tmp/status.md"
jq -e '.programStatus == "DONE" and .salesforce.state == "complete" and .salesforce.outcomes.matched == 3' "$tmp/status.json"

printf '%s\n' '{"candidate":{"commit":"cccccccccccccccccccccccccccccccccccccccc"},"tools":{"commit":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}}' >"$tmp/attempt.json"
if run_renderer 2>"$tmp/stale.err"; then
  echo 'renderer accepted a stale corpus attempt' >&2
  exit 1
fi
grep -F 'corpus attempt candidate does not match source binding' "$tmp/stale.err"

echo 'salesforce completeness renderer tests passed'
