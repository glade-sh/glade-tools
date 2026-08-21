#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
renderer="$root/scripts/render-salesforce-completeness.sh"
tmp="$(mktemp -d "${TMPDIR:-/tmp}/glade-completeness-test.XXXXXX")"
trap 'find "$tmp" -depth -delete' EXIT

write_inputs() {
  printf '%s\n' '{"rows":[{"surfaceId":"a"},{"surfaceId":"b"},{"surfaceId":"c"},{"surfaceId":"d"},{"surfaceId":"e"},{"surfaceId":"f"},{"surfaceId":"hosted"}]}' >"$tmp/ledger.json"
  printf '%s\n' '{"totalOpenRows":2,"packets":[]}' >"$tmp/packet.json"
  printf '%s\n' '{"status":"exact-candidate-bound","historicalCredit":false,"candidate":{"path":"/candidate","commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","clean":true},"tools":{"path":"/tools","commit":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","clean":true},"moduleReplacement":{"resolvedPath":"/candidate","requiredPath":"/candidate"}}' >"$tmp/binding.json"
  printf '%s\n' '{"candidate":{"commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"tools":{"commit":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}}' >"$tmp/attempt.json"
  printf '%s\n' '{"candidate":{"commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"tools":{"commit":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},"commands":[{"passed":true,"exitCode":0},{"passed":true,"exitCode":0},{"passed":true,"exitCode":0}]}' >"$tmp/release.json"
  printf '%s\n' '{"repositorySurfaceRows":[{"repositoryId":"one","runtimeParityReady":true,"nonParity":false},{"repositoryId":"two","runtimeParityReady":false,"nonParity":false}],"repositorySummaries":[{"repositoryId":"one","surfaceCount":1,"runtimeParityReady":true,"nonParity":false},{"repositoryId":"two","surfaceCount":1,"runtimeParityReady":false,"nonParity":true}]}' >"$tmp/assurance.json"
  printf '%s\n' '{"status":"current-candidate-diagnostic","candidate":{"gladeCommit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","toolsCommit":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},"completion":{"definition":"Every frozen project passes cold check and full local tests.","completeProjects":6,"totalProjects":10,"percent":60.0,"remainingProjects":4},"validSourceSupport":{"completeProjects":6,"totalProjects":8,"percent":75.0,"remainingProjects":2},"accounting":{"accountedProjects":10,"totalProjects":10,"percent":100.0,"unclassifiedProjects":0},"checks":{"successfulCommands":7,"totalProjects":10,"percent":70.0},"observedTests":{"scope":"Eligible projects only.","total":17503,"passed":17471,"failed":13,"unsupported":19,"passPercent":99.8},"creditBoundary":"Diagnostic only; zero Salesforce parity credit."}' >"$tmp/private-status.json"
  printf '%s\n' '{"total":7,"byDisposition":{"compile-shape-required":2,"deterministic-mock-required":2,"local-runtime-required":2,"hosted-deferred":1},"rows":[{"surfaceId":"a","disposition":"compile-shape-required","ledgerShape":"signature-known","behavior":"supported","evidence":"fixture"},{"surfaceId":"b","disposition":"compile-shape-required","ledgerShape":"signature-known","behavior":"passive","evidence":"none","gapClass":"missing-evidence"},{"surfaceId":"c","disposition":"deterministic-mock-required","ledgerShape":"signature-known","behavior":"supported","evidence":"fixture-and-oracle"},{"surfaceId":"d","disposition":"deterministic-mock-required","ledgerShape":"signature-known","behavior":"supported","evidence":"fixture"},{"surfaceId":"e","disposition":"local-runtime-required","ledgerShape":"signature-known","behavior":"supported","evidence":"fixture"},{"surfaceId":"f","disposition":"local-runtime-required","ledgerShape":"signature-known","behavior":"supported","evidence":"none","gapClass":"missing-evidence"},{"surfaceId":"hosted","disposition":"hosted-deferred","ledgerShape":"type-known","behavior":"passive","evidence":"none"}]}' >"$tmp/profile.json"
  printf '%s\n' '{"schemaVersion":1,"generatedAt":"2026-08-20T20:00:00Z","alias":"glade-dev-hub","expectedOrgId":"00D000000000001","workers":[{"name":"private-host-a","host":"ssh-user@worker-a.example.internal","healthy":true,"reachable":true,"devHub":{"connected":true,"alias":"glade-dev-hub","orgId":"00D000000000001","username":"worker@example.invalid","activeScratchOrgsRemaining":3,"dailyScratchOrgsRemaining":6},"diskFreeBytes":107374182400,"run":null,"issues":[]},{"name":"private-host-b","host":"ssh-user@worker-b.example.internal","healthy":false,"reachable":false,"devHub":{"connected":false,"alias":"glade-dev-hub","orgId":null,"username":null,"activeScratchOrgsRemaining":null,"dailyScratchOrgsRemaining":null},"diskFreeBytes":null,"run":null,"issues":["unreachable"]}]}' >"$tmp/worker-health.json"
}

run_renderer() {
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
    --json-output "$tmp/status.json"
}

write_inputs
run_renderer
grep -F 'Surface proof completion: **64.3%** (9 / 14 required checkpoints)' "$tmp/status.md"
grep -F 'Remaining to 100%: **5 required checkpoints**' "$tmp/status.md"
grep -F 'Inventory accounting: **71.4%** (5 / 7 rows accounted)' "$tmp/status.md"
grep -F 'Local evidence: **66.7%** (4 / 6 required rows)' "$tmp/status.md"
grep -F 'Salesforce comparison: **25.0%** (1 / 4 runtime rows)' "$tmp/status.md"
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
  and .completion == {percent:64.3, complete:9, required:14, remaining:5}
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
printf '%s\n' '{"total":4,"byDisposition":{"compile-shape-required":1,"deterministic-mock-required":1,"local-runtime-required":2,"hosted-deferred":0},"rows":[{"surfaceId":"a","disposition":"compile-shape-required","ledgerShape":"signature-known","behavior":"supported","evidence":"fixture"},{"surfaceId":"c","disposition":"deterministic-mock-required","ledgerShape":"signature-known","behavior":"supported","evidence":"fixture-and-oracle"},{"surfaceId":"d","disposition":"local-runtime-required","ledgerShape":"signature-known","behavior":"supported","evidence":"fixture-and-oracle"},{"surfaceId":"e","disposition":"local-runtime-required","ledgerShape":"signature-known","behavior":"passive","evidence":"fixture-and-oracle"}]}' >"$tmp/profile.json"
run_renderer
grep -F 'Surface proof completion: **100.0%** (10 / 10 required checkpoints)' "$tmp/status.md"
grep -F 'Remaining to 100%: **0 required checkpoints**' "$tmp/status.md"
grep -F 'Program status: **NOT DONE**' "$tmp/status.md"

printf '%s\n' '{"status":"current-candidate-diagnostic","candidate":{"gladeCommit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","toolsCommit":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},"completion":{"definition":"Every frozen project passes cold check and full local tests.","completeProjects":10,"totalProjects":10,"percent":100.0,"remainingProjects":0},"checks":{"successfulCommands":10,"totalProjects":10,"percent":100.0},"observedTests":{"scope":"All projects.","total":18000,"passed":18000,"failed":0,"unsupported":0,"passPercent":100.0},"creditBoundary":"Diagnostic only; zero Salesforce parity credit."}' >"$tmp/private-status.json"
run_renderer
grep -F 'Program status: **DONE**' "$tmp/status.md"

printf '%s\n' '{"candidate":{"commit":"cccccccccccccccccccccccccccccccccccccccc"},"tools":{"commit":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}}' >"$tmp/attempt.json"
if run_renderer 2>"$tmp/stale.err"; then
  echo 'renderer accepted a stale corpus attempt' >&2
  exit 1
fi
grep -F 'corpus attempt candidate does not match source binding' "$tmp/stale.err"

echo 'salesforce completeness renderer tests passed'
