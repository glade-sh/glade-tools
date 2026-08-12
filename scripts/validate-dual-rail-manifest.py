#!/usr/bin/env python3
"""Validate the evidence contract required before a wave is materialized."""
import json
import re
import sys
from pathlib import Path

HEX64 = re.compile(r"^[0-9a-fA-F]{64}$")

def validate(path):
    try:
        payload = json.loads(Path(path).read_text())
    except (OSError, json.JSONDecodeError) as exc:
        raise ValueError(f"manifest is not valid JSON: {exc}")
    if not isinstance(payload, dict):
        raise ValueError("manifest must be a JSON object")
    if payload.get("status") != "pass":
        raise ValueError("status must be pass")
    surface_ids = payload.get("surfaceIds")
    if not isinstance(surface_ids, list) or not surface_ids or not all(isinstance(value, str) and value for value in surface_ids):
        raise ValueError("surfaceIds must be a non-empty array of strings")
    candidate = payload.get("candidate")
    if not isinstance(candidate, dict):
        raise ValueError("candidate must be an object")
    commit = candidate.get("commit")
    if not isinstance(commit, str) or not re.fullmatch(r"[0-9a-fA-F]{40}", commit):
        raise ValueError("candidate.commit must be a 40-character hex SHA")
    binary_sha = candidate.get("binarySha256")
    if not isinstance(binary_sha, str) or not HEX64.fullmatch(binary_sha):
        raise ValueError("candidate.binarySha256 must be a 64-character hex SHA")
    observations = payload.get("observations")
    if not isinstance(observations, dict):
        raise ValueError("observations must be an object")
    for key in ("localResultSha256", "salesforceResultSha256"):
        value = observations.get(key)
        if not isinstance(value, str) or not HEX64.fullmatch(value):
            raise ValueError(f"observations.{key} must be a 64-character hex SHA")
    if observations.get("comparison") != "pass":
        raise ValueError("observations.comparison must be pass")
    return payload

def main(argv):
    if len(argv) != 2:
        print(f"usage: {argv[0]} PATH", file=sys.stderr)
        return 2
    try:
        validate(argv[1])
    except ValueError as exc:
        print(f"dual-rail manifest rejected: {exc}", file=sys.stderr)
        return 1
    print("dual-rail manifest: pass")
    return 0

if __name__ == "__main__":
    raise SystemExit(main(sys.argv))
