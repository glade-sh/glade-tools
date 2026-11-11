"""Import corpus result aggregates without reading test output or source files."""

from __future__ import annotations

import json
import math
import stat
from pathlib import Path
from typing import Any


MAX_HISTORY_BYTES = 40 * 1024 * 1024
MAX_HISTORY_ROWS = 100_000


def _first(*values: Any) -> Any:
    return next((value for value in values if value is not None and value != ""), None)


def _object(value: Any) -> dict:
    return value if isinstance(value, dict) else {}


def _text(value: Any) -> str | None:
    return value if isinstance(value, str) and value.strip() else None


def _count(*values: Any) -> int | None:
    for value in values:
        if isinstance(value, bool):
            continue
        if isinstance(value, int) and value >= 0:
            return value
        if isinstance(value, float) and math.isfinite(value) and value >= 0 and value.is_integer():
            return int(value)
    return None


def _metadata(document: dict) -> dict:
    glade = _object(document.get("glade"))
    candidate = _object(document.get("candidate"))
    return {
        "glade_version": _first(document.get("glade_version"), document.get("gladeVersion"), glade.get("version")),
        "glade_sha": _first(document.get("glade_sha"), document.get("gladeSha"), document.get("gladeCommit"),
                            document.get("candidate_sha"), document.get("candidateSha"), glade.get("sha"),
                            glade.get("commit"), candidate.get("glade"), candidate.get("sha")),
        "api_version": _first(document.get("api_version"), document.get("apiVersion"), document.get("sourceApiVersion")),
        "host": document.get("host"),
        "started_at": _first(document.get("started_at"), document.get("startedAt"), document.get("generatedAt"),
                             document.get("dispatched")),
    }


def _identity(row: dict) -> dict:
    repository = _object(row.get("repository"))
    identity = {
        "repo_path": _first(row.get("repo_path"), row.get("repoPath"), repository.get("path")),
        "repo_url": _first(row.get("repo_url"), row.get("repoUrl"), repository.get("url")),
        "repo_name": _first(row.get("repo_name"), row.get("repoName"), repository.get("name")),
        "repo_sha": _first(row.get("repo_sha"), row.get("repoSha"), row.get("repoRevision"),
                           row.get("repositoryCommit"), repository.get("sha"), repository.get("commit")),
    }
    label = _text(_first(row.get("repo"), row.get("project"), row.get("repository")))
    if label:
        if label.startswith(("https://", "http://", "git@", "ssh://")):
            identity["repo_url"] = identity["repo_url"] or label
        elif label.startswith(("/", "~/", "./", "../")) or (len(label) > 2 and label[1:3] in (":/", ":\\")):
            identity["repo_path"] = identity["repo_path"] or label
        else:
            identity["repo_name"] = identity["repo_name"] or label
    return {key: _text(value) for key, value in identity.items()}


def _row(row: dict, metadata: dict, source: str) -> dict | None:
    identity = _identity(row)
    if not any(identity[key] for key in ("repo_name", "repo_path", "repo_url")):
        return None
    summary = _object(row.get("summary"))
    # A repository registry or a dispatch record alone is not test evidence.
    if not summary and not any(key in row for key in ("passed", "failed", "total", "status", "ready", "exit", "timedOut")):
        return None
    record = dict(identity)
    own = _metadata(row)
    record.update({key: _text(_first(own[key], metadata.get(key))) for key in own})
    if record["api_version"] is None:
        api = _first(own.get("api_version"), metadata.get("api_version"))
        if isinstance(api, (int, float)) and not isinstance(api, bool) and math.isfinite(api):
            record["api_version"] = str(api)
    record.update({
        "passed": _count(row.get("passed"), summary.get("passed"), summary.get("pass")),
        "failed": _count(row.get("failed"), summary.get("failed"), summary.get("fail")),
        "skipped": _count(row.get("skipped"), summary.get("skipped"), summary.get("skip")),
        "total": _count(row.get("total"), summary.get("total")),
        "compile_errors": _count(row.get("compile_errors"), summary.get("compileErrors"), summary.get("compileError")),
        "runtime_errors": _count(row.get("runtime_errors"), summary.get("runtimeErrors"), summary.get("internalError")),
        "unsupported": _count(row.get("unsupported"), summary.get("unsupported")),
        "timeouts": _count(row.get("timeouts"), summary.get("timeouts"), summary.get("timeout")),
        "phase": _text(row.get("phase")),
        "part": _text(row.get("part")),
        "shard": _text(row.get("shard")),
        "source": source,
    })
    status = str(row.get("status") or "unknown").lower()
    status = {"pass": "passed", "success": "passed", "fail": "failed", "failure": "failed"}.get(status, status)
    if row.get("timedOut") is True or row.get("exit") == "timeout":
        status = "timeout"
    elif row.get("parse_error"):
        status = "error"
    elif status == "unknown" and row.get("ready") is True:
        status = "passed"
    elif status == "unknown" and any((record[key] or 0) > 0 for key in ("failed", "compile_errors", "runtime_errors")):
        status = "failed"
    elif status == "unknown" and row.get("exit") not in (None, 0, "0"):
        status = "error"
    if record["part"] or record["shard"]:
        record["result_status"] = status
        status = "partial"
    record["status"] = status
    missing = []
    if not record["repo_sha"]:
        missing.append("repository revision unknown")
    if not record["glade_sha"] and not record["glade_version"]:
        missing.append("Glade revision unknown")
    record["provenance"] = "Imported aggregate" + ("; " + "; ".join(missing) if missing else "")
    for key in ("wall_s", "paused_s", "durationMs", "peak_rss"):
        value = row.get(key)
        if isinstance(value, (int, float)) and not isinstance(value, bool) and math.isfinite(value) and value >= 0:
            record[key] = value
    return record


def parse_history(path, glade_version="", glade_sha="", host="local") -> dict:
    """Read one JSON/JSONL aggregate; missing historical revisions stay unknown.

    Explicit caller metadata can bind a host's ``results.jsonl`` to its dispatch
    record. No HEAD lookup, source traversal, network request, or test run occurs.
    Partial rows are retained individually, never added into a complete run.
    """
    result: dict = {"runs": [], "warnings": []}
    source = Path(path).expanduser()
    if source.suffix.lower() not in (".json", ".jsonl"):
        result["warnings"].append("History imports require an aggregate .json or .jsonl file.")
        return result
    try:
        info = source.stat()
        if not stat.S_ISREG(info.st_mode):
            raise ValueError("History input must be a regular file.")
        if info.st_size > MAX_HISTORY_BYTES:
            raise ValueError("History input exceeds the 40 MiB limit.")
        with source.open("rb") as stream:
            raw = stream.read(MAX_HISTORY_BYTES + 1)
        if len(raw) > MAX_HISTORY_BYTES:
            raise ValueError("History input exceeds the 40 MiB limit.")
        contents = raw.decode("utf-8-sig")
    except (OSError, ValueError, UnicodeError):
        result["warnings"].append("History input is unavailable, not UTF-8, not a regular file, or exceeds the 40 MiB limit.")
        return result
    return parse_history_data(contents, source.suffix.lower(), str(source), glade_version, glade_sha, host)


def parse_history_data(content, suffix=".jsonl", source="import", glade_version="", glade_sha="", host="local", started_at="") -> dict:
    """Normalize aggregate text returned by a host without creating a local file."""
    result: dict = {"runs": [], "warnings": []}
    if suffix not in (".json", ".jsonl"):
        result["warnings"].append("History imports require aggregate JSON or JSONL.")
        return result
    try:
        raw = content if isinstance(content, bytes) else content.encode("utf-8")
        if len(raw) > MAX_HISTORY_BYTES:
            raise ValueError("History input is too large.")
        contents = raw.decode("utf-8-sig")
    except (AttributeError, UnicodeError, ValueError):
        result["warnings"].append("History input must be UTF-8 text of at most 40 MiB.")
        return result
    defaults = {"glade_version": glade_version, "glade_sha": glade_sha, "host": host, "started_at": started_at}
    skipped = 0
    examined = 0

    def consume(document: Any, locator: str) -> None:
        nonlocal skipped, examined
        metadata = dict(defaults)
        if isinstance(document, dict):
            metadata.update({key: value for key, value in _metadata(document).items() if value is not None and value != ""})
            group = next((document[key] for key in ("runs", "projects", "results") if isinstance(document.get(key), list)), None)
            rows = group if group is not None else [document]
        elif isinstance(document, list):
            rows = document
        else:
            rows = [document]
        for index, row in enumerate(rows):
            if examined >= MAX_HISTORY_ROWS:
                return
            examined += 1
            normalized = _row(row, metadata, locator + (f"#{index + 1}" if len(rows) > 1 else "")) if isinstance(row, dict) else None
            if normalized is None:
                skipped += 1
            else:
                result["runs"].append(normalized)

    if suffix == ".jsonl":
        malformed = 0
        for number, line in enumerate(contents.splitlines(), 1):
            if not line.strip():
                continue
            try:
                consume(json.loads(line), f"{source}:{number}")
            except (ValueError, RecursionError):
                malformed += 1
            if examined >= MAX_HISTORY_ROWS:
                break
        if malformed:
            result["warnings"].append(f"Skipped {malformed} malformed JSONL record(s); the file may still be being written.")
    else:
        try:
            consume(json.loads(contents), str(source))
        except (ValueError, RecursionError):
            result["warnings"].append("History input is not valid aggregate JSON.")
    if examined >= MAX_HISTORY_ROWS:
        result["warnings"].append("Import stopped at the 100,000-row limit.")
    if skipped:
        result["warnings"].append(f"Skipped {skipped} record(s) without a repository identity and aggregate outcome.")
    if result["runs"] and any(not row["repo_sha"] for row in result["runs"]):
        result["warnings"].append("Some historical repository revisions were not recorded; they remain unknown.")
    return result
