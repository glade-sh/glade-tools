#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "usage: $0 --ledger <SURFACE_LEDGER.json> --profile <SOURCE_PROFILE.json> --packet <SURFACE_PACKET_MANIFEST.json> --binding <SOURCE_BINDING.json> [--corpus <ASSURANCE.json> --attempt <ATTEMPT.json>] [--private-corpus-status <PRIVATE_CORPUS_STATUS.json>] [--release <RELEASE_VALIDATION.json>] --output <STATUS.md>" >&2
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
output=""
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
    --output) output="${2:-}"; shift 2 ;;
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
  ' "$private_status" >/dev/null || { echo "private corpus status is invalid" >&2; exit 1; }
fi
if [[ -n "$release" ]]; then
  [[ -f "$release" ]] || { echo "release validation input is unavailable" >&2; exit 1; }
  jq -e . "$release" >/dev/null
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

corpus_line='Private corpus: **STALE / MISSING** — run the current candidate before claiming completion.'
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
  corpus_line="Private corpus: **$(percent "$corpus_complete" "$corpus_total")%** ($(commify "$corpus_complete") / $(commify "$corpus_total") repositories complete)"
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
trap '[[ ! -e "$tmp_output" ]] || unlink "$tmp_output"' EXIT
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

echo "salesforce completeness status: $output" >&2
