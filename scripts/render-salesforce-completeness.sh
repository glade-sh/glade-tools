#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "usage: $0 --ledger <SURFACE_LEDGER.json> --profile <SOURCE_PROFILE.json> --packet <SURFACE_PACKET_MANIFEST.json> --binding <SOURCE_BINDING.json> [--corpus <ASSURANCE.json> --attempt <ATTEMPT.json>] [--private-corpus-status <PRIVATE_CORPUS_STATUS.json>] [--release <RELEASE_VALIDATION.json>] [--worker-health <WORKER_HEALTH.json>] --output <STATUS.md> [--json-output <STATUS.json>]" >&2
  exit 2
}

ledger=""
profile=""
packet=""
binding=""
corpus=""
attempt=""
private_status=""
release=""
worker_health=""
output=""
json_output=""
while (($#)); do
  case "$1" in
    --ledger) ledger="${2:-}"; shift 2 ;;
    --profile) profile="${2:-}"; shift 2 ;;
    --packet) packet="${2:-}"; shift 2 ;;
    --binding) binding="${2:-}"; shift 2 ;;
    --corpus) corpus="${2:-}"; shift 2 ;;
    --attempt) attempt="${2:-}"; shift 2 ;;
    --private-corpus-status) private_status="${2:-}"; shift 2 ;;
    --release) release="${2:-}"; shift 2 ;;
    --worker-health) worker_health="${2:-}"; shift 2 ;;
    --output) output="${2:-}"; shift 2 ;;
    --json-output) json_output="${2:-}"; shift 2 ;;
    -h|--help) usage ;;
    *) usage ;;
  esac
done

for required in "$ledger" "$profile" "$packet" "$binding" "$output"; do
  [[ -n "$required" ]] || usage
done
for input in "$ledger" "$profile" "$packet" "$binding"; do
  [[ -f "$input" ]] || { echo "required input is not a regular file: $input" >&2; exit 1; }
  jq -e . "$input" >/dev/null
done
if [[ -n "$corpus" || -n "$attempt" ]]; then
  [[ -n "$corpus" && -n "$attempt" ]] || { echo "corpus assurance and attempt must be supplied together" >&2; exit 1; }
  [[ -f "$corpus" && -f "$attempt" ]] || { echo "corpus assurance input is unavailable" >&2; exit 1; }
  jq -e . "$corpus" "$attempt" >/dev/null
fi
if [[ -n "$private_status" ]]; then
  [[ -f "$private_status" ]] || { echo "private corpus status input is unavailable" >&2; exit 1; }
  jq -e '
    .status == "current-candidate-diagnostic"
    and ([.completion.completeProjects, .completion.totalProjects, .completion.remainingProjects,
          .checks.successfulCommands, .checks.totalProjects,
          .observedTests.passed, .observedTests.total]
         | all(type == "number" and isfinite and floor == . and . >= 0))
    and ((.validSourceSupport // null) == null or
         ([.validSourceSupport.completeProjects, .validSourceSupport.totalProjects, .validSourceSupport.remainingProjects]
          | all(type == "number" and isfinite and floor == . and . >= 0)))
    and ((.accounting // null) == null or
         ([.accounting.accountedProjects, .accounting.totalProjects, .accounting.unclassifiedProjects]
          | all(type == "number" and isfinite and floor == . and . >= 0)))
  ' "$private_status" >/dev/null || { echo "private corpus status is invalid" >&2; exit 1; }
fi
if [[ -n "$release" ]]; then
  [[ -f "$release" ]] || { echo "release validation input is unavailable" >&2; exit 1; }
  jq -e . "$release" >/dev/null
fi
if [[ -n "$worker_health" ]]; then
  [[ -f "$worker_health" ]] || { echo "worker health input is unavailable" >&2; exit 1; }
  jq -e '
    . as $document
    | .alias as $alias
    | .expectedOrgId as $orgId
    | (
      .schemaVersion == 1
      and (.generatedAt | type == "string" and length > 0)
      and (.alias | type == "string" and length > 0)
      and (.expectedOrgId | type == "string" and length > 0)
      and (.workers | type == "array")
      and all(.workers[];
        (.name | type == "string" and length > 0)
        and (.host | type == "string" and length > 0)
        and (.healthy | type == "boolean")
        and (.reachable | type == "boolean")
        and (.issues | type == "array")
        and .devHub.alias == $alias
        and ((.devHub.connected != true) or .devHub.orgId == $orgId))
      and ([$document | .. | objects | keys[]] | all(. != "accessToken" and . != "sfdxAuthUrl" and . != "cookie" and . != "environment"))
    )
  ' "$worker_health" >/dev/null || { echo "worker health input is invalid" >&2; exit 1; }
fi

jq -e '
  .status == "exact-candidate-bound"
  and .historicalCredit == false
  and .candidate.clean == true
  and .tools.clean == true
  and (.candidate.commit | test("^[0-9a-f]{40}$"))
  and (.tools.commit | test("^[0-9a-f]{40}$"))
  and .moduleReplacement.resolvedPath == .moduleReplacement.requiredPath
  and .moduleReplacement.requiredPath == .candidate.path
' "$binding" >/dev/null || { echo "source binding is not an exact clean candidate pair" >&2; exit 1; }

candidate_commit="$(jq -r '.candidate.commit' "$binding")"
tools_commit="$(jq -r '.tools.commit' "$binding")"
if [[ -n "$attempt" ]]; then
  [[ "$(jq -r '.candidate.commit' "$attempt")" == "$candidate_commit" ]] || { echo "corpus attempt candidate does not match source binding" >&2; exit 1; }
  [[ "$(jq -r '.tools.commit' "$attempt")" == "$tools_commit" ]] || { echo "corpus attempt tools do not match source binding" >&2; exit 1; }
fi
if [[ -n "$private_status" ]]; then
  [[ "$(jq -r '.candidate.gladeCommit' "$private_status")" == "$candidate_commit" ]] || { echo "private corpus status candidate does not match source binding" >&2; exit 1; }
  [[ "$(jq -r '.candidate.toolsCommit' "$private_status")" == "$tools_commit" ]] || { echo "private corpus status tools do not match source binding" >&2; exit 1; }
fi
if [[ -n "$release" ]]; then
  [[ "$(jq -r '.candidate.commit' "$release")" == "$candidate_commit" ]] || { echo "release candidate does not match source binding" >&2; exit 1; }
  [[ "$(jq -r '.tools.commit' "$release")" == "$tools_commit" ]] || { echo "release tools do not match source binding" >&2; exit 1; }
fi

ledger_total="$(jq -r '.rows | length' "$ledger")"
open_rows="$(jq -r '.totalOpenRows' "$packet")"
[[ "$ledger_total" =~ ^[0-9]+$ && "$open_rows" =~ ^[0-9]+$ && "$open_rows" -le "$ledger_total" ]] || { echo "invalid ledger or packet counts" >&2; exit 1; }
accounted=$((ledger_total - open_rows))

read -r profile_total compile_total deterministic_total runtime_total hosted_total <<EOF
$(jq -r '[.total, (.byDisposition["compile-shape-required"] // 0), (.byDisposition["deterministic-mock-required"] // 0), (.byDisposition["local-runtime-required"] // 0), (.byDisposition["hosted-deferred"] // 0)] | @tsv' "$profile")
EOF
[[ $((compile_total + deterministic_total + runtime_total + hosted_total)) -eq "$profile_total" ]] || { echo "support-profile disposition totals do not reconcile" >&2; exit 1; }

compile_closed="$(jq -r '[.rows[] | select(.disposition == "compile-shape-required" and ((.gapClass // "") == ""))] | length' "$profile")"
runtime_shape="$(jq -r '[.rows[] | select((.disposition == "deterministic-mock-required" or .disposition == "local-runtime-required") and (.ledgerShape != null and .ledgerShape != "" and .ledgerShape != "absent"))] | length' "$profile")"
local_complete="$(jq -r '[.rows[] | select((.disposition == "deterministic-mock-required" or .disposition == "local-runtime-required") and (.behavior == "supported" or .behavior == "passive") and (.evidence == "fixture" or .evidence == "fixture-and-oracle"))] | length' "$profile")"
parity_complete="$(jq -r '[.rows[] | select((.disposition == "deterministic-mock-required" or .disposition == "local-runtime-required") and (.behavior == "supported" or .behavior == "passive") and .evidence == "fixture-and-oracle")] | length' "$profile")"

runtime_required=$((deterministic_total + runtime_total))
required_rows=$((compile_total + runtime_required))
required_checkpoints=$((compile_total + 3 * runtime_required))
completed_checkpoints=$((compile_closed + runtime_shape + local_complete + parity_complete))
remaining_checkpoints=$((required_checkpoints - completed_checkpoints))
local_required_complete=$((compile_closed + local_complete))

percent() {
  awk -v numerator="$1" -v denominator="$2" 'BEGIN { if (denominator == 0) printf "100.0"; else printf "%.1f", 100 * numerator / denominator }'
}

commify() {
  local value="$1"
  local result=""
  while ((${#value} > 3)); do
    result=",${value: -3}${result}"
    value="${value:0:${#value}-3}"
  done
  printf '%s%s' "$value" "$result"
}

surface_percent="$(percent "$completed_checkpoints" "$required_checkpoints")"
inventory_percent="$(percent "$accounted" "$ledger_total")"
local_percent="$(percent "$local_required_complete" "$required_rows")"
parity_percent="$(percent "$parity_complete" "$runtime_required")"

corpus_line='Formal corpus assurance: **STALE / MISSING** — run the current candidate before claiming completion.'
corpus_done=0
if [[ -n "$corpus" ]]; then
  corpus_total="$(jq -r '.repositorySummaries | length' "$corpus")"
  corpus_complete="$(jq -r '
    [.repositorySummaries[] as $summary
      | ([.repositorySurfaceRows[] | select(.repositoryId == $summary.repositoryId)]) as $rows
      | select(
          (($summary.surfaceCount // -1) == 0 and $summary.nonParity == true)
          or (($summary.surfaceCount // -1) > 0
              and ($rows | length) == $summary.surfaceCount
              and ([$rows[] | select(.runtimeParityReady == true or .nonParity == true)] | length) == $summary.surfaceCount)
        )
    ] | length
  ' "$corpus")"
  corpus_line="Formal corpus assurance: **$(percent "$corpus_complete" "$corpus_total")%** ($(commify "$corpus_complete") / $(commify "$corpus_total") repositories complete)"
  [[ "$corpus_total" -gt 0 && "$corpus_complete" -eq "$corpus_total" ]] && corpus_done=1
fi

private_project_lines='Private project completion: **STALE / MISSING** — run cold checks and full local tests on the current candidate.'
private_project_done=1
if [[ -n "$private_status" ]]; then
  read -r private_complete private_total private_remaining private_checks private_check_total private_tests_passed private_tests_total <<EOF
$(jq -r '[.completion.completeProjects, .completion.totalProjects, .completion.remainingProjects, .checks.successfulCommands, .checks.totalProjects, .observedTests.passed, .observedTests.total] | @tsv' "$private_status")
EOF
  [[ "$private_total" -gt 0 && "$private_complete" -ge 0 && "$private_complete" -le "$private_total" && "$private_remaining" -eq $((private_total - private_complete)) ]] || { echo "private corpus completion counts do not reconcile" >&2; exit 1; }
  [[ "$private_check_total" -eq "$private_total" && "$private_checks" -ge 0 && "$private_checks" -le "$private_check_total" ]] || { echo "private corpus check counts do not reconcile" >&2; exit 1; }
  [[ "$private_tests_total" -ge 0 && "$private_tests_passed" -ge 0 && "$private_tests_passed" -le "$private_tests_total" ]] || { echo "private corpus test counts do not reconcile" >&2; exit 1; }
  private_project_lines=$(printf 'Private project completion: **%s%%** (%s / %s complete; %s remaining)\n- Private project check readiness: **%s%%** (%s / %s checks passed)\n- Eligible private test pass rate: **%s%%** (%s / %s; diagnostic subset)' \
    "$(percent "$private_complete" "$private_total")" "$(commify "$private_complete")" "$(commify "$private_total")" "$(commify "$private_remaining")" \
    "$(percent "$private_checks" "$private_check_total")" "$(commify "$private_checks")" "$(commify "$private_check_total")" \
    "$(percent "$private_tests_passed" "$private_tests_total")" "$(commify "$private_tests_passed")" "$(commify "$private_tests_total")")
  if jq -e '.validSourceSupport != null' "$private_status" >/dev/null; then
    read -r valid_complete valid_total valid_remaining <<EOF
$(jq -r '[.validSourceSupport.completeProjects, .validSourceSupport.totalProjects, .validSourceSupport.remainingProjects] | @tsv' "$private_status")
EOF
    [[ "$valid_total" -gt 0 && "$valid_complete" -le "$valid_total" && "$valid_remaining" -eq $((valid_total - valid_complete)) ]] || { echo "valid-source private support counts do not reconcile" >&2; exit 1; }
    private_project_lines+=$(printf '\n- Salesforce-valid private support: **%s%%** (%s / %s complete; %s remaining)' \
      "$(percent "$valid_complete" "$valid_total")" "$(commify "$valid_complete")" "$(commify "$valid_total")" "$(commify "$valid_remaining")")
  fi
  if jq -e '.accounting != null' "$private_status" >/dev/null; then
    read -r classified classified_total unclassified <<EOF
$(jq -r '[.accounting.accountedProjects, .accounting.totalProjects, .accounting.unclassifiedProjects] | @tsv' "$private_status")
EOF
    [[ "$classified_total" -gt 0 && "$classified" -le "$classified_total" && "$unclassified" -eq $((classified_total - classified)) ]] || { echo "private project accounting counts do not reconcile" >&2; exit 1; }
    private_project_lines+=$(printf '\n- Private project accounting: **%s%%** (%s / %s classified; %s unclassified)' \
      "$(percent "$classified" "$classified_total")" "$(commify "$classified")" "$(commify "$classified_total")" "$(commify "$unclassified")")
  fi
  [[ "$private_complete" -eq "$private_total" ]] || private_project_done=0
fi

release_line='Release validation: **STALE / MISSING** — run the fixed release commands for this candidate.'
release_done=0
if [[ -n "$release" ]]; then
  release_total="$(jq -r '.commands | length' "$release")"
  release_complete="$(jq -r '[.commands[] | select(.passed == true and .exitCode == 0)] | length' "$release")"
  release_line="Release validation: **$(percent "$release_complete" "$release_total")%** ($(commify "$release_complete") / $(commify "$release_total") commands passed)"
  [[ "$release_total" -gt 0 && "$release_complete" -eq "$release_total" ]] && release_done=1
fi

program_status='NOT DONE'
if [[ "$accounted" -eq "$ledger_total" && "$completed_checkpoints" -eq "$required_checkpoints" && "$corpus_done" -eq 1 && "$private_project_done" -eq 1 && "$release_done" -eq 1 ]]; then
  program_status='DONE'
fi

mkdir -p "$(dirname "$output")"
tmp_output="$(mktemp "${output}.tmp.XXXXXX")"
tmp_json=""
trap '[[ ! -e "$tmp_output" ]] || unlink "$tmp_output"; [[ -z "$tmp_json" || ! -e "$tmp_json" ]] || unlink "$tmp_json"' EXIT
{
  printf '# Salesforce Completeness Status\n\n'
  printf 'Program status: **%s**\n\n' "$program_status"
  printf 'Surface proof completion: **%s%%** (%s / %s required checkpoints)\n\n' "$surface_percent" "$(commify "$completed_checkpoints")" "$(commify "$required_checkpoints")"
  printf 'Remaining to 100%%: **%s required checkpoints**\n\n' "$(commify "$remaining_checkpoints")"
  printf '> 100%% means every required surface checkpoint, current private repository, and release command is complete.\n\n'
  printf '## Candidate\n\n'
  printf -- '- Glade: `%s`\n' "$candidate_commit"
  printf -- '- Glade Tools: `%s`\n\n' "$tools_commit"
  printf '## Completion dimensions\n\n'
  printf -- '- Inventory accounting: **%s%%** (%s / %s rows accounted)\n' "$inventory_percent" "$(commify "$accounted")" "$(commify "$ledger_total")"
  printf -- '- Local evidence: **%s%%** (%s / %s required rows)\n' "$local_percent" "$(commify "$local_required_complete")" "$(commify "$required_rows")"
  printf -- '- Salesforce comparison: **%s%%** (%s / %s runtime rows)\n' "$parity_percent" "$(commify "$parity_complete")" "$(commify "$runtime_required")"
  printf -- '- %s\n' "$corpus_line"
  printf -- '- %s\n' "$private_project_lines"
  printf -- '- %s\n\n' "$release_line"
  printf '## Required surface checkpoints\n\n'
  printf '| checkpoint | complete | required |\n| --- | ---: | ---: |\n'
  printf '| compile shape | %s | %s |\n' "$(commify "$compile_closed")" "$(commify "$compile_total")"
  printf '| runtime shape | %s | %s |\n' "$(commify "$runtime_shape")" "$(commify "$runtime_required")"
  printf '| local behavior | %s | %s |\n' "$(commify "$local_complete")" "$(commify "$runtime_required")"
  printf '| Salesforce comparison | %s | %s |\n\n' "$(commify "$parity_complete")" "$(commify "$runtime_required")"
  printf 'Hosted-deferred rows: **%s**. Open packet rows: **%s**.\n' "$(commify "$hosted_total")" "$(commify "$open_rows")"
} >"$tmp_output"
mv "$tmp_output" "$output"

if [[ -n "$json_output" ]]; then
  machines='[]'
  unhealthy_names=''
  if [[ -n "$worker_health" ]]; then
    machines="$(jq -c '[.workers | to_entries[] | .key as $index | .value | {
      name: ("worker-" + (($index + 1) | tostring)),
      healthy,
      reachable,
      devHub: {
        connected: .devHub.connected,
        alias: .devHub.alias,
        activeScratchOrgsRemaining: .devHub.activeScratchOrgsRemaining,
        dailyScratchOrgsRemaining: .devHub.dailyScratchOrgsRemaining
      },
      diskFreeBytes,
      run,
      issues
    }]' "$worker_health")"
    unhealthy_names="$(jq -r '[.workers | to_entries[] | select(.value.healthy != true) | "worker-" + ((.key + 1) | tostring)] | join(", ")' "$worker_health")"
  fi
  action_summary='Start current-candidate Salesforce comparison'
  action_reason="No current Salesforce index exists for the ${runtime_required} runtime-required surfaces."
  action_command='Freeze the exact candidate and initialize the all-runtime Salesforce index.'
  action_clears='A candidate-bound Salesforce index exists with one row per runtime-required surface.'
  if [[ -n "$unhealthy_names" ]]; then
    action_summary='Restore worker health'
    action_reason="Unhealthy workers: $unhealthy_names."
    action_command='Repair connectivity or Dev Hub login, then regenerate WORKER_HEALTH.json.'
    action_clears='Every requested worker is reachable, connected to the expected Dev Hub, and above the disk threshold.'
  elif [[ "$local_required_complete" -ne "$required_rows" ]]; then
    action_summary='Close remaining local evidence gaps'
    action_reason="$((required_rows - local_required_complete)) locally required surface rows remain incomplete."
    action_command='Land the next exact local-evidence packet and regenerate the support profile.'
    action_clears='Local evidence equals the reviewed locally required denominator.'
  fi

  mkdir -p "$(dirname "$json_output")"
  tmp_json="$(mktemp "${json_output}.tmp.XXXXXX")"
  jq -n \
    --arg generatedAt "$(date -u +'%Y-%m-%dT%H:%M:%SZ')" \
    --arg programStatus 'NOT DONE' \
    --arg glade "$candidate_commit" \
    --arg tools "$tools_commit" \
    --argjson completionPercent "$surface_percent" \
    --argjson completionComplete "$completed_checkpoints" \
    --argjson completionRequired "$required_checkpoints" \
    --argjson completionRemaining "$remaining_checkpoints" \
    --argjson inventoryComplete "$accounted" \
    --argjson inventoryRequired "$ledger_total" \
    --argjson localComplete "$local_required_complete" \
    --argjson localRequired "$required_rows" \
    --argjson runtimeRequired "$runtime_required" \
    --argjson hostedDeferred "$hosted_total" \
    --argjson packetOpen "$open_rows" \
    --argjson machines "$machines" \
    --arg actionSummary "$action_summary" \
    --arg actionReason "$action_reason" \
    --arg actionCommand "$action_command" \
    --arg actionClears "$action_clears" \
    '{
      schemaVersion: 1,
      generatedAt: $generatedAt,
      programStatus: $programStatus,
      completion: {percent: $completionPercent, complete: $completionComplete, required: $completionRequired, remaining: $completionRemaining},
      candidate: {glade: $glade, tools: $tools},
      tiers: {
        inventory: {complete: $inventoryComplete, required: $inventoryRequired},
        localEvidence: {complete: $localComplete, required: $localRequired},
        salesforceComparison: {complete: 0, required: $runtimeRequired},
        hostedDeferred: $hostedDeferred,
        openPacketRows: $packetOpen
      },
      salesforce: {
        state: "not-started",
        outcomes: {adjudicated: 0, matched: 0, explicitNonParity: 0, productMismatch: 0, inconclusive: 0, open: $runtimeRequired}
      },
      pipeline: {phase: "not-started", status: "idle", startedAt: null, updatedAt: null},
      machines: $machines,
      action: {owner: "agent", summary: $actionSummary, reason: $actionReason, action: $actionCommand, clearsWhen: $actionClears},
      cleanup: {state: "not-reported"},
      delivery: {state: "not-reported"}
    }' >"$tmp_json"
  mv "$tmp_json" "$json_output"
  tmp_json=""
fi

echo "salesforce completeness status: $output" >&2
