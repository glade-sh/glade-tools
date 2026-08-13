#!/usr/bin/env bash
set -euo pipefail

[[ $# -eq 4 ]] || { echo "usage: $0 <receipt> <sha256-sidecar> <repository> <check-name>" >&2; exit 2; }
receipt="$1"
sidecar="$2"
repository="$3"
check_name="$4"
[[ -f "$receipt" && -f "$sidecar" ]] || { echo "receipt and sidecar must be files" >&2; exit 1; }
[[ "$repository" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]] || { echo "invalid target repository" >&2; exit 1; }
[[ "$check_name" == "Salesforce Correctness" ]] || { echo "invalid check name" >&2; exit 1; }
: "${GH_TOKEN:?GH_TOKEN is required}"
: "${GITHUB_SHA:?GITHUB_SHA is required}"
: "${GLADE_SHA:?GLADE_SHA is required}"
: "${GITHUB_RUN_ID:?GITHUB_RUN_ID is required}"
: "${GITHUB_RUN_ATTEMPT:?GITHUB_RUN_ATTEMPT is required}"
: "${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is required}"
: "${GITHUB_SERVER_URL:?GITHUB_SERVER_URL is required}"

request="$(mktemp)"
trap 'rm -f "$request"' EXIT
python3 - "$receipt" "$sidecar" "$repository" "$check_name" "$request" <<'PY'
import hashlib
import json
import os
import re
import sys


def reject_duplicates(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError(f"duplicate JSON key: {key}")
        result[key] = value
    return result


receipt_path, sidecar_path, repository, check_name, request_path = sys.argv[1:]
receipt_bytes = open(receipt_path, "rb").read()
try:
    receipt = json.loads(receipt_bytes, object_pairs_hook=reject_duplicates)
except (json.JSONDecodeError, UnicodeDecodeError, ValueError) as exc:
    raise SystemExit(f"invalid authority receipt: {exc}")

keys = {
    "schemaVersion", "gladeSHA", "toolsSHA", "workflowRunID",
    "workflowRunAttempt", "workflowRunURL", "gateStatus", "cleanupStatus",
    "evidenceArtifactName",
}
if not isinstance(receipt, dict) or set(receipt) != keys:
    raise SystemExit("authority receipt has unexpected schema")
canonical = (json.dumps(receipt, sort_keys=True, separators=(",", ":")) + "\n").encode()
if receipt_bytes != canonical:
    raise SystemExit("authority receipt is not canonical JSON")

sha = re.compile(r"[0-9a-f]{40}")
digest_pattern = re.compile(r"[0-9a-f]{64}")
if receipt["schemaVersion"] != 1:
    raise SystemExit("invalid receipt version")
if not isinstance(receipt["gladeSHA"], str) or not sha.fullmatch(receipt["gladeSHA"]):
    raise SystemExit("invalid Glade SHA")
if receipt["gladeSHA"] != os.environ["GLADE_SHA"]:
    raise SystemExit("receipt Glade SHA does not match workflow input")
if not isinstance(receipt["toolsSHA"], str) or not sha.fullmatch(receipt["toolsSHA"]):
    raise SystemExit("invalid Tools SHA")
if receipt["toolsSHA"] != os.environ["GITHUB_SHA"]:
    raise SystemExit("receipt Tools SHA does not match workflow SHA")
if type(receipt["workflowRunID"]) is not int or receipt["workflowRunID"] <= 0 or str(receipt["workflowRunID"]) != os.environ["GITHUB_RUN_ID"]:
    raise SystemExit("receipt workflow run ID mismatch")
if type(receipt["workflowRunAttempt"]) is not int or receipt["workflowRunAttempt"] <= 0 or str(receipt["workflowRunAttempt"]) != os.environ["GITHUB_RUN_ATTEMPT"]:
    raise SystemExit("receipt workflow run attempt mismatch")
expected_url = f'{os.environ["GITHUB_SERVER_URL"]}/{os.environ["GITHUB_REPOSITORY"]}/actions/runs/{receipt["workflowRunID"]}/attempts/{receipt["workflowRunAttempt"]}'
if receipt["workflowRunURL"] != expected_url:
    raise SystemExit("receipt workflow URL mismatch")
if receipt["gateStatus"] != "PASS" or receipt["cleanupStatus"] != "PASS":
    raise SystemExit("authority requires PASS gate and cleanup")
if receipt["evidenceArtifactName"] != "salesforce-correctness-evidence":
    raise SystemExit("invalid evidence artifact name")

digest = hashlib.sha256(receipt_bytes).hexdigest()
sidecar_bytes = open(sidecar_path, "rb").read()
expected_sidecar = f'{digest}  {os.path.basename(receipt_path)}\n'.encode()
if sidecar_bytes != expected_sidecar or not digest_pattern.fullmatch(digest):
    raise SystemExit("authority receipt digest mismatch")

external_id = (
    f'salesforce-release-authority/v1;tools_sha={receipt["toolsSHA"]};'
    f'run_id={receipt["workflowRunID"]};run_attempt={receipt["workflowRunAttempt"]};'
    f'receipt_sha256={digest}'
)
request = {
    "name": check_name,
    "head_sha": receipt["gladeSHA"],
    "status": "completed",
    "conclusion": "success",
    "external_id": external_id,
    "details_url": receipt["workflowRunURL"],
}
with open(request_path, "w", encoding="utf-8") as stream:
    json.dump(request, stream, sort_keys=True, separators=(",", ":"))
    stream.write("\n")
PY

gh api --method POST "repos/$repository/check-runs" --input - < "$request"
