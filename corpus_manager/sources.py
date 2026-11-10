"""Discover saved aggregate routes from local dispatch metadata, without writes."""

from __future__ import annotations

import json
import re
import stat
from datetime import datetime
from pathlib import Path


MAX_DISPATCH_BYTES = 1024 * 1024
MAX_DISPATCH_RECORDS = 10_000
ROUTES = {
    "casper_priv": ("private", "casper"),
    "razor_public": ("public", "razor"),
    "razor_b3": ("broad3", "razor"),
    "bmo": ("bmo", "bmo"),
}


def _key(source):
    return (source.get("host"), source.get("path"), source.get("glade_sha"))


def _is_dispatch_source(source):
    return source.get("source_kind") == "dispatch" or str(source.get("provenance", "")).startswith("Local dispatch record ")


def _timestamp(value):
    if not isinstance(value, str) or not value.strip():
        return None
    try:
        datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError:
        return None
    return value


def _extract(document, filename):
    if not isinstance(document, dict):
        return None
    sha = document.get("sha")
    started_at = _timestamp(document.get("dispatched"))
    gates = document.get("gates")
    outputs = document.get("outputs")
    if not isinstance(sha, str) or not re.fullmatch(r"[0-9a-fA-F]{40}", sha) or not started_at:
        return None
    if not isinstance(gates, dict) or not isinstance(outputs, dict):
        return None
    sources = []
    for kind, (flag, host) in ROUTES.items():
        if gates.get(flag) is not True:
            continue
        output = outputs.get(kind)
        if not isinstance(output, str) or not output.startswith(("/", "~/")) or "\0" in output:
            continue
        paths = [output]
        if kind == "bmo" and "{BUCKET2,BROAD}" in output:
            paths = [output.replace("{BUCKET2,BROAD}", group) for group in ("BUCKET2", "BROAD")]
        for path in paths:
            sources.append({
                "host": host,
                "path": path.rstrip("/") + "/results.jsonl",
                "glade_sha": sha.lower(),
                "started_at": started_at,
                "label": document.get("label") if isinstance(document.get("label"), str) else None,
                "kind": kind,
                "source_kind": "dispatch",
                "dispatch_record": filename,
                "provenance": "Local dispatch record " + filename,
            })
    return sources


def refresh_sources(config: dict) -> dict:
    """Return current dispatch routes plus retained history and manual sources.

    Generated routes use the dispatch's recorded SHA, timestamp and output
    directory. No binary or repository HEAD is consulted. Existing routes are
    retained because the original dispatch file can disappear before its saved
    aggregate does. Manual entries take precedence for an identical source key.
    """
    refreshed = dict(config)
    existing = [dict(source) for source in config.get("sources", []) if isinstance(source, dict)]
    refreshed["sources"] = existing
    refreshed["warnings"] = []
    directory = config.get("dispatch_dir")
    if not directory:
        return refreshed
    try:
        directory = Path(directory).expanduser()
        if not directory.is_dir():
            raise OSError("Missing dispatch directory")
        files = sorted(file for file in directory.iterdir() if file.suffix == ".json")
    except (OSError, TypeError, ValueError):
        refreshed["warnings"].append("Dispatch directory is unavailable; existing history sources were retained.")
        return refreshed

    generated = []
    invalid = 0
    for file in files[:MAX_DISPATCH_RECORDS]:
        try:
            info = file.stat()
            if not stat.S_ISREG(info.st_mode) or info.st_size > MAX_DISPATCH_BYTES:
                raise ValueError("Invalid dispatch metadata file")
            with file.open("rb") as stream:
                raw = stream.read(MAX_DISPATCH_BYTES + 1)
            if len(raw) > MAX_DISPATCH_BYTES:
                raise ValueError("Dispatch metadata too large")
            entries = _extract(json.loads(raw.decode("utf-8-sig")), file.name)
            if entries is None:
                raise ValueError("Missing explicit dispatch identity")
            generated.extend(entries)
        except (OSError, UnicodeError, ValueError, RecursionError):
            invalid += 1

    # Upsert refreshed dispatch metadata, then restore explicit manual choices.
    merged = {_key(source): source for source in existing}
    merged.update({_key(source): source for source in generated})
    merged.update({_key(source): source for source in existing if not _is_dispatch_source(source)})
    refreshed["sources"] = sorted(merged.values(), key=lambda source: (str(source.get("started_at") or ""),
                                                                      str(source.get("host") or ""),
                                                                      str(source.get("path") or "")), reverse=True)
    if invalid:
        refreshed["warnings"].append(f"Skipped {invalid} unreadable or invalid dispatch record(s); no revision or date was inferred.")
    if len(files) > MAX_DISPATCH_RECORDS:
        refreshed["warnings"].append("Dispatch discovery stopped at the 10,000-record limit.")
    return refreshed
