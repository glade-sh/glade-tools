#!/usr/bin/env bash
set -euo pipefail

if [[ "$#" -ne 4 ]]; then
	echo "usage: scripts/salesforce-stale-scratch-cleanup.sh <dev-hub> <current-marker> <repository> <evidence-json>" >&2
	exit 2
fi

dev_hub="$1"
current_marker="$2"
repository="$3"
evidence="$4"
if [[ ! "$dev_hub" =~ ^[A-Za-z0-9._-]+$ ]] ||
	[[ ! "$current_marker" =~ ^glade-correctness-[1-9][0-9]*-[1-9][0-9]*$ ]] ||
	[[ ! "$repository" =~ ^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$ ]]; then
	echo "invalid stale scratch cleanup input" >&2
	exit 2
fi

mkdir -p "$(dirname "$evidence")"
tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/glade-stale-scratch.XXXXXX")"
trap 'rm -rf -- "$tmp_dir"' EXIT
umask 077
sf_json="$tmp_dir/sf.json"
gh_json="$tmp_dir/gh.json"
stderr_log="$tmp_dir/command.stderr"
candidates="$tmp_dir/candidates.tsv"
terminal_candidates="$tmp_dir/terminal-candidates.tsv"

jq -S -c -n \
	'{schemaVersion: 1, status: "fail", stage: "started", candidateCount: 0, terminalEligibleCount: 0, activeRecordsDeactivated: 0}' \
	>"$evidence"

query="SELECT Id, ScratchOrg, OrgName, Status FROM ScratchOrgInfo WHERE OrgName LIKE 'glade-correctness-%' AND ScratchOrg != null ORDER BY CreatedDate ASC LIMIT 20"
sf data query --target-org "$dev_hub" --query "$query" --json >"$sf_json" 2>"$stderr_log"
python3 - "$sf_json" "$current_marker" >"$candidates" <<'PY'
import json, re, sys

payload = json.load(open(sys.argv[1]))
result = payload.get("result")
records = result.get("records") if isinstance(result, dict) else None
total = result.get("totalSize") if isinstance(result, dict) else None
status = payload.get("status")
if type(status) is not int or status != 0 or not isinstance(records, list) or type(total) is not int:
    raise SystemExit("invalid stale ScratchOrgInfo response")
if total != len(records) or len(records) > 20:
    raise SystemExit("invalid stale ScratchOrgInfo result")

request_ids, scratch_orgs, markers = set(), set(), set()
for row in records:
    if not isinstance(row, dict):
        raise SystemExit("invalid stale ScratchOrgInfo record")
    request_id = row.get("Id", "")
    scratch_org = row.get("ScratchOrg", "")
    marker = row.get("OrgName", "")
    status = row.get("Status")
    match = re.fullmatch(r"glade-correctness-([1-9][0-9]*)-([1-9][0-9]*)", marker)
    if not re.fullmatch(r"2SR[0-9A-Za-z]{12}(?:[0-9A-Za-z]{3})?", request_id):
        raise SystemExit("invalid stale ScratchOrgInfo identity")
    if not re.fullmatch(r"00D[0-9A-Za-z]{12}(?:[0-9A-Za-z]{3})?", scratch_org):
        raise SystemExit("invalid stale scratch org identity")
    if not match or not isinstance(status, str) or not status:
        raise SystemExit("invalid stale scratch marker")
    if request_id in request_ids or scratch_org in scratch_orgs or marker in markers:
        raise SystemExit("duplicate stale scratch identity")
    request_ids.add(request_id)
    scratch_orgs.add(scratch_org)
    markers.add(marker)
    print(scratch_org, match.group(1), match.group(2), marker, sep="\t")
PY
candidate_count="$(awk 'END {print NR + 0}' "$candidates")"
jq -S -c -n --argjson candidates "$candidate_count" \
	'{schemaVersion: 1, status: "fail", stage: "candidates-validated", candidateCount: $candidates, terminalEligibleCount: 0, activeRecordsDeactivated: 0}' \
	>"$evidence"

: >"$terminal_candidates"
while IFS=$'\t' read -r scratch_org run_id attempt marker; do
	[[ -n "$scratch_org" && -n "$run_id" && -n "$attempt" && -n "$marker" ]] || continue
	if [[ "$marker" == "$current_marker" ]]; then
		continue
	fi
	gh api "repos/$repository/actions/runs/$run_id/attempts/$attempt" >"$gh_json" 2>"$stderr_log"
	decision="$(python3 - "$gh_json" "$run_id" "$attempt" "$repository" <<'PY'
import json, sys

run = json.load(open(sys.argv[1]))
run_id, attempt = int(sys.argv[2]), int(sys.argv[3])
if not isinstance(run, dict) or type(run.get("id")) is not int or type(run.get("run_attempt")) is not int:
    raise SystemExit("stale workflow run identity mismatch")
if run["id"] != run_id or run["run_attempt"] != attempt:
    raise SystemExit("stale workflow run identity mismatch")
run_repository = run.get("repository")
if not isinstance(run_repository, dict) or run_repository.get("full_name") != sys.argv[4]:
    raise SystemExit("stale workflow run repository mismatch")
if run.get("path") != ".github/workflows/salesforce-correctness.yml":
    raise SystemExit("stale workflow run path mismatch")
status, conclusion = run.get("status"), run.get("conclusion")
if status == "completed":
    if not isinstance(conclusion, str) or not conclusion:
        raise SystemExit("stale workflow run has invalid conclusion")
    print("terminal")
elif status in {"queued", "in_progress", "waiting", "requested", "pending"}:
    if conclusion is not None:
        raise SystemExit("nonterminal workflow run has a conclusion")
    print("nonterminal")
else:
    raise SystemExit("stale workflow run has invalid status")
PY
	)"
	if [[ "$decision" == "terminal" ]]; then
		printf '%s\n' "$scratch_org" >>"$terminal_candidates"
	elif [[ "$decision" != "nonterminal" ]]; then
		echo "invalid stale workflow decision" >&2
		exit 1
	fi
done <"$candidates"
terminal_count="$(awk 'END {print NR + 0}' "$terminal_candidates")"
jq -S -c -n --argjson candidates "$candidate_count" --argjson terminal "$terminal_count" \
	'{schemaVersion: 1, status: "fail", stage: "runs-validated", candidateCount: $candidates, terminalEligibleCount: $terminal, activeRecordsDeactivated: 0}' \
	>"$evidence"

selected_scratch=""
selected_active=""
while IFS= read -r scratch_org; do
	[[ -n "$scratch_org" ]] || continue
	active_query="SELECT Id, ScratchOrg FROM ActiveScratchOrg WHERE ScratchOrg = '$scratch_org'"
	sf data query --target-org "$dev_hub" --query "$active_query" --json >"$sf_json" 2>"$stderr_log"
	active_id="$(python3 - "$sf_json" "$scratch_org" <<'PY'
import json, re, sys

payload = json.load(open(sys.argv[1]))
result = payload.get("result")
records = result.get("records") if isinstance(result, dict) else None
total = result.get("totalSize") if isinstance(result, dict) else None
status = payload.get("status")
if type(status) is not int or status != 0 or not isinstance(records, list) or type(total) is not int:
    raise SystemExit("invalid stale ActiveScratchOrg response")
if total != len(records) or len(records) > 1:
    raise SystemExit("invalid stale ActiveScratchOrg result")
if not records:
    print("-")
    raise SystemExit
row = records[0]
active_id = row.get("Id", "") if isinstance(row, dict) else ""
if row.get("ScratchOrg") != sys.argv[2] or not re.fullmatch(r"2AS[0-9A-Za-z]{12}(?:[0-9A-Za-z]{3})?", active_id):
    raise SystemExit("invalid stale ActiveScratchOrg identity")
print(active_id)
PY
	)"
	if [[ "$active_id" != "-" ]]; then
		selected_scratch="$scratch_org"
		selected_active="$active_id"
		break
	fi
done <"$terminal_candidates"

if [[ -z "$selected_active" ]]; then
	jq -S -c -n --argjson candidates "$candidate_count" --argjson terminal "$terminal_count" \
		'{schemaVersion: 1, status: "pass", stage: "complete", candidateCount: $candidates, terminalEligibleCount: $terminal, activeRecordsDeactivated: 0}' \
		>"$evidence"
	exit 0
fi
jq -S -c -n --argjson candidates "$candidate_count" --argjson terminal "$terminal_count" \
	'{schemaVersion: 1, status: "fail", stage: "active-selected", candidateCount: $candidates, terminalEligibleCount: $terminal, activeRecordsDeactivated: 0}' \
	>"$evidence"

sf data delete record --target-org "$dev_hub" --sobject ActiveScratchOrg \
	--record-id "$selected_active" --json >"$sf_json" 2>"$stderr_log"
python3 - "$sf_json" "$selected_active" <<'PY'
import json, sys

payload = json.load(open(sys.argv[1]))
result = payload.get("result")
status = payload.get("status")
if type(status) is not int or status != 0 or not isinstance(result, dict) or result.get("success") is not True or result.get("id") != sys.argv[2]:
    raise SystemExit("invalid stale ActiveScratchOrg deletion result")
PY
jq -S -c -n --argjson candidates "$candidate_count" --argjson terminal "$terminal_count" \
	'{schemaVersion: 1, status: "fail", stage: "active-deactivated", candidateCount: $candidates, terminalEligibleCount: $terminal, activeRecordsDeactivated: 1}' \
	>"$evidence"

active_query="SELECT Id, ScratchOrg FROM ActiveScratchOrg WHERE ScratchOrg = '$selected_scratch'"
sf data query --target-org "$dev_hub" --query "$active_query" --json >"$sf_json" 2>"$stderr_log"
python3 - "$sf_json" <<'PY'
import json, sys

payload = json.load(open(sys.argv[1]))
result = payload.get("result")
records = result.get("records") if isinstance(result, dict) else None
total = result.get("totalSize") if isinstance(result, dict) else None
status = payload.get("status")
if type(status) is not int or status != 0 or not isinstance(records, list) or type(total) is not int or total != 0 or records:
    raise SystemExit("remaining stale ActiveScratchOrg residue")
PY
jq -S -c -n --argjson candidates "$candidate_count" --argjson terminal "$terminal_count" \
	'{schemaVersion: 1, status: "pass", stage: "complete", candidateCount: $candidates, terminalEligibleCount: $terminal, activeRecordsDeactivated: 1}' \
	>"$evidence"
