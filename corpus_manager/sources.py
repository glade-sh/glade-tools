"""Discover saved aggregate routes from local dispatch metadata, without writes."""

from __future__ import annotations

import json
import os
import re
import stat
from datetime import datetime
from pathlib import Path


MAX_DISPATCH_BYTES = 1024 * 1024
MAX_DISPATCH_RECORDS = 10_000
ROUTES_ENV = "GLADE_CORPUS_MANAGER_ROUTES"
DEFAULT_ROUTES_FILE = Path("~/.config/glade-corpus-manager/routes.json")
_BRACE = re.compile(r"\{([^{},]+(?:,[^{},]+)+)\}")


def routes_path():
    return Path(os.environ.get(ROUTES_ENV) or DEFAULT_ROUTES_FILE).expanduser()


def load_routes(path=None):
    """Read route definitions from a private file kept outside the repository.

    Format: {"routes": {"<output key>": {"gate": "<gate flag>", "host": "<host id>"}}}
    Returns (routes, warning). Missing or invalid files yield no routes.
    """
    path = Path(path).expanduser() if path else routes_path()
    try:
        document = json.loads(path.read_text(encoding="utf-8-sig"))
    except FileNotFoundError:
        return {}, "No dispatch routes file at %s; set %s. No dispatch routes were generated." % (path, ROUTES_ENV)
    except (OSError, ValueError):
        return {}, "Dispatch routes file is unreadable; no dispatch routes were generated."
    routes = {}
    raw = document.get("routes") if isinstance(document, dict) else None
    for kind, entry in (raw.items() if isinstance(raw, dict) else []):
        if isinstance(entry, dict) and isinstance(entry.get("gate"), str) and isinstance(entry.get("host"), str):
            routes[kind] = (entry["gate"], entry["host"])
    return routes, None if routes else "Dispatch routes file defines no valid routes."


def load_lanes(path=None):
    """Return shared test-lane settings from the private routes file, or None.

    Format: {"lanes": {"slots_dir": "<dir>", "hosts": {"<ssh alias>":
    {"flag": "<busy marker file>", "prefix": "<slot directory prefix>"}}}}
    """
    path = Path(path).expanduser() if path else routes_path()
    try:
        lanes = json.loads(path.read_text(encoding="utf-8-sig")).get("lanes")
    except (OSError, ValueError, AttributeError):
        return None
    if not isinstance(lanes, dict) or not isinstance(lanes.get("slots_dir"), str) or not isinstance(lanes.get("hosts"), dict):
        return None
    return lanes


def _expand(output):
    match = _BRACE.search(output)
    if not match:
        return [output]
    return [output[:match.start()] + choice + output[match.end():] for choice in match.group(1).split(",")]


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


def _extract(document, filename, routes):
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
    for kind, (flag, host) in routes.items():
        if gates.get(flag) is not True:
            continue
        output = outputs.get(kind)
        if not isinstance(output, str) or not output.startswith(("/", "~/")) or "\0" in output:
            continue
        for path in _expand(output):
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


def refresh_sources(config: dict, routes=None) -> dict:
    """Return current dispatch routes plus retained history and manual sources.

    Generated routes use the dispatch's recorded SHA, timestamp and output
    directory. No binary or repository HEAD is consulted. Existing routes are
    retained because the original dispatch file can disappear before its saved
    aggregate does. Manual entries take precedence for an identical source key.
    Routes map dispatch output keys to gate flags and host IDs; when not given
    they come from the private routes file (see load_routes).
    """
    refreshed = dict(config)
    existing = [dict(source) for source in config.get("sources", []) if isinstance(source, dict)]
    refreshed["sources"] = existing
    refreshed["warnings"] = []
    directory = config.get("dispatch_dir")
    if not directory:
        return refreshed
    route_warning = None
    if routes is None:
        routes, route_warning = load_routes()
    if route_warning:
        refreshed["warnings"].append(route_warning)
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
            entries = _extract(json.loads(raw.decode("utf-8-sig")), file.name, routes)
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
