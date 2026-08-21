#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import os
import re
import shlex
import subprocess
import tempfile
from concurrent.futures import ThreadPoolExecutor
from datetime import datetime, timezone
from pathlib import Path


REMOTE_PROBE = r"""
import json
import os
import stat
import subprocess
import sys
from pathlib import Path

alias, disk_path, marker_path, sf_bin = sys.argv[1:]
marker_path = os.path.expanduser(marker_path)
env = os.environ.copy()
env["SF_USE_GENERIC_UNIX_KEYCHAIN"] = "true"

def command_json(argv):
    result = subprocess.run(argv, env=env, text=True, capture_output=True, timeout=25, check=False)
    if result.returncode != 0:
        return {"status": result.returncode}
    try:
        return json.loads(result.stdout)
    except json.JSONDecodeError:
        return {"status": 1}

display = command_json([sf_bin, "org", "display", "--target-org", alias, "--json"])
display_result = display.get("result", {}) if isinstance(display, dict) else {}
limits_document = command_json([sf_bin, "limits", "api", "display", "--target-org", alias, "--json"])
limits = {}
if isinstance(limits_document, dict) and isinstance(limits_document.get("result"), list):
    for row in limits_document["result"]:
        if isinstance(row, dict) and row.get("name") in {"ActiveScratchOrgs", "DailyScratchOrgs"}:
            limits[row["name"]] = {"remaining": row.get("remaining")}

disk_result = subprocess.run(["df", "-k", disk_path], text=True, capture_output=True, timeout=10, check=False)
disk_free = None
if disk_result.returncode == 0:
    lines = [line for line in disk_result.stdout.splitlines() if line.strip()]
    if len(lines) >= 2:
        try:
            disk_free = int(lines[-1].split()[3]) * 1024
        except (IndexError, ValueError):
            pass

run = None
marker_issue = None
try:
    if stat.S_IMODE(os.stat(marker_path).st_mode) != 0o600:
        marker_issue = "unsafe-run-marker"
    else:
        marker = json.loads(Path(marker_path).read_text(encoding="utf-8"))
        if isinstance(marker, dict):
            run = {key: marker.get(key) for key in ("id", "phase", "heartbeatAt")}
except FileNotFoundError:
    pass
except (OSError, json.JSONDecodeError):
    marker_issue = "invalid-run-marker"

print(json.dumps({
    "devHub": {
        "status": display.get("status") if isinstance(display, dict) else 1,
        "orgId": display_result.get("id") if isinstance(display_result, dict) else None,
        "username": display_result.get("username") if isinstance(display_result, dict) else None,
        "connectedStatus": display_result.get("connectedStatus") if isinstance(display_result, dict) else None,
        "limits": limits,
    },
    "diskFreeBytes": disk_free,
    "run": run,
    "runMarkerIssue": marker_issue,
}))
"""


def parse_mapping(value: str) -> tuple[str, str]:
    name, separator, target = value.partition("=")
    if not separator or not re.fullmatch(r"[A-Za-z0-9._-]+", name) or not target or target.startswith("-"):
        raise argparse.ArgumentTypeError("expected NAME=VALUE")
    return name, target


def safe_integer(value: object) -> int | None:
    return value if isinstance(value, int) and not isinstance(value, bool) and value >= 0 else None


def stale_heartbeat(run: dict[str, object] | None, seconds: int) -> bool:
    if not run or not isinstance(run.get("heartbeatAt"), str):
        return False
    try:
        heartbeat = datetime.fromisoformat(run["heartbeatAt"].replace("Z", "+00:00"))
    except ValueError:
        return True
    return (datetime.now(timezone.utc) - heartbeat).total_seconds() > seconds


def unhealthy_row(name: str, host: str, alias: str, issue: str) -> dict[str, object]:
    return {
        "name": name,
        "host": host,
        "healthy": False,
        "reachable": False,
        "devHub": {
            "connected": False,
            "alias": alias,
            "orgId": None,
            "username": None,
            "activeScratchOrgsRemaining": None,
            "dailyScratchOrgsRemaining": None,
        },
        "diskFreeBytes": None,
        "run": None,
        "issues": [issue],
    }


def probe_worker(
    host_spec: tuple[str, str],
    disks: dict[str, str],
    alias: str,
    expected_org_id: str,
    min_disk_free_bytes: int,
    stale_after_seconds: int,
    run_marker: str,
    sf_bin: str,
) -> dict[str, object]:
    name, host = host_spec
    disk_path = disks.get(name, "/")
    remote_command = "python3 -c {} {} {} {} {}".format(
        shlex.quote(REMOTE_PROBE),
        shlex.quote(alias),
        shlex.quote(disk_path),
        shlex.quote(run_marker),
        shlex.quote(sf_bin),
    )
    try:
        result = subprocess.run(
            ["ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", host, remote_command],
            text=True,
            capture_output=True,
            timeout=30,
            check=False,
        )
    except (OSError, subprocess.TimeoutExpired):
        return unhealthy_row(name, host, alias, "unreachable")
    if result.returncode != 0:
        return unhealthy_row(name, host, alias, "unreachable")
    try:
        payload = json.loads(result.stdout)
    except json.JSONDecodeError:
        return unhealthy_row(name, host, alias, "malformed-worker-response")
    if not isinstance(payload, dict):
        return unhealthy_row(name, host, alias, "malformed-worker-response")

    raw_hub = payload.get("devHub") if isinstance(payload.get("devHub"), dict) else {}
    limits = raw_hub.get("limits") if isinstance(raw_hub.get("limits"), dict) else {}
    active = limits.get("ActiveScratchOrgs") if isinstance(limits.get("ActiveScratchOrgs"), dict) else {}
    daily = limits.get("DailyScratchOrgs") if isinstance(limits.get("DailyScratchOrgs"), dict) else {}
    org_id = raw_hub.get("orgId") if isinstance(raw_hub.get("orgId"), str) else None
    username = raw_hub.get("username") if isinstance(raw_hub.get("username"), str) else None
    connected = raw_hub.get("status") == 0 and raw_hub.get("connectedStatus") == "Connected" and org_id is not None
    active_remaining = safe_integer(active.get("remaining"))
    daily_remaining = safe_integer(daily.get("remaining"))
    disk_free = safe_integer(payload.get("diskFreeBytes"))
    raw_run = payload.get("run") if isinstance(payload.get("run"), dict) else None
    run = None
    if raw_run and all(isinstance(raw_run.get(key), str) and raw_run[key] for key in ("id", "phase", "heartbeatAt")):
        run = {key: raw_run[key] for key in ("id", "phase", "heartbeatAt")}

    issues: list[str] = []
    if not connected:
        issues.append("dev-hub-unavailable")
    elif org_id != expected_org_id:
        issues.append("dev-hub-org-id-mismatch")
    if connected and (active_remaining is None or daily_remaining is None):
        issues.append("scratch-org-limits-unavailable")
    if disk_free is None or disk_free < min_disk_free_bytes:
        issues.append("low-disk")
    marker_issue = payload.get("runMarkerIssue")
    if marker_issue in {"unsafe-run-marker", "invalid-run-marker"}:
        issues.append(marker_issue)
    if stale_heartbeat(run, stale_after_seconds):
        issues.append("stale-heartbeat")

    return {
        "name": name,
        "host": host,
        "healthy": not issues,
        "reachable": True,
        "devHub": {
            "connected": connected,
            "alias": alias,
            "orgId": org_id,
            "username": username,
            "activeScratchOrgsRemaining": active_remaining,
            "dailyScratchOrgsRemaining": daily_remaining,
        },
        "diskFreeBytes": disk_free,
        "run": run,
        "issues": issues,
    }


def write_atomic(path: Path, document: dict[str, object]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    descriptor, temporary_name = tempfile.mkstemp(prefix=f"{path.name}.tmp.", dir=path.parent)
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8") as stream:
            json.dump(document, stream, indent=2, sort_keys=True)
            stream.write("\n")
        os.replace(temporary_name, path)
    finally:
        if os.path.exists(temporary_name):
            os.unlink(temporary_name)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--host", action="append", type=parse_mapping, required=True)
    parser.add_argument("--disk", action="append", type=parse_mapping, default=[])
    parser.add_argument("--alias", required=True)
    parser.add_argument("--expected-org-id", required=True)
    parser.add_argument("--min-disk-free-bytes", type=int, default=20 * 1024**3)
    parser.add_argument("--stale-after-seconds", type=int, default=120)
    parser.add_argument("--run-marker", default="~/.config/glade-proof/run.json")
    parser.add_argument("--sf-bin", default="/usr/local/bin/sf")
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    if args.min_disk_free_bytes < 0 or args.stale_after_seconds < 0:
        parser.error("thresholds must be nonnegative")
    host_names = [name for name, _ in args.host]
    if len(host_names) != len(set(host_names)):
        parser.error("duplicate host name")
    disks = dict(args.disk)
    if not set(disks).issubset(host_names):
        parser.error("disk mapping names must match hosts")

    def probe(spec: tuple[str, str]) -> dict[str, object]:
        return probe_worker(
            spec,
            disks,
            args.alias,
            args.expected_org_id,
            args.min_disk_free_bytes,
            args.stale_after_seconds,
            args.run_marker,
            args.sf_bin,
        )

    with ThreadPoolExecutor(max_workers=min(len(args.host), 8)) as executor:
        workers = list(executor.map(probe, args.host))
    document = {
        "schemaVersion": 1,
        "generatedAt": datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
        "alias": args.alias,
        "expectedOrgId": args.expected_org_id,
        "workers": workers,
    }
    write_atomic(args.output, document)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
