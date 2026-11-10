"""Loopback dashboard API. Operations are serialized; mutation needs a page token."""
import argparse
import hashlib
import tempfile
import json
import os
import secrets
import threading
import traceback
import urllib.parse
import webbrowser
from concurrent.futures import ThreadPoolExecutor
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from .history import parse_history
from .ops import Operations, UPDATE_RUNNER, host_python, validate_host
from .store import Store, identifier, now


DEFAULT_DATA = Path.home() / ".local/share/glade-corpus-manager"
STATIC = Path(__file__).parent / "static"


class App:
    def __init__(self, directory):
        self.store = Store(directory)
        self.ops = Operations(self.store)
        self.ops.tiers_snapshot = self.tiers_snapshot
        self.ops.tier_entry = self.tier_entry
        self.ops.tier_ambiguous = self.tier_ambiguous
        self.token = secrets.token_urlsafe(32)
        self.executor = ThreadPoolExecutor(max_workers=1, thread_name_prefix="corpus")
        self.job_context = threading.local()
        self.ops.cancel_token = lambda: getattr(self.job_context, "current", {}).get("id", "")
        config = self.store.directory / "config.json"
        if config.exists() and not self.store.all("hosts"):
            for host in json.loads(config.read_text()).get("hosts", []):
                self.store.put("hosts", validate_host(host))
        self.migrate_history()
        if not self.store.all("queries"):
            for title, query in [("Active Apex projects", "language:Apex archived:false fork:false stars:>=5"), ("Lightning Web Components", "topic:lightning-web-components archived:false fork:false stars:>=5"), ("New Salesforce projects", "topic:salesforce archived:false fork:false created:>=2026-01-01")]:
                self.store.put("queries", {"id": identifier(title), "name": title, "query": query})

    def migrate_history(self):
        path = self.store.directory / "history-sources.json"
        try:
            sources = json.loads(path.read_text()).get("sources", [])
        except (OSError, ValueError):
            sources = []
        lookup = {(s.get("host"), s.get("path")): s for s in sources}
        repos = self.store.all("repositories")
        changed = []
        for row in self.store.all("runs"):
            before = dict(row)
            source = str(row.get("source") or "").rsplit(":", 1)[0]
            entry = lookup.get((row.get("host"), source), {})
            for key, source_key in (("gate_label", "label"), ("glade_sha", "glade_sha")):
                if not row.get(key) and entry.get(source_key):
                    row[key] = entry[source_key]
            if not row.get("repo_id"):
                self.match_run(row, repos)
            if row != before:
                changed.append(row)
        if changed:
            self.store.put_many("runs", changed)

    def settings(self):
        try: return json.loads((self.store.directory / "dashboard-settings.json").read_text())
        except (OSError, ValueError): return {}

    def save_settings(self, data):
        settings = self.settings()
        for key in ("tiers_file", "glade_binary", "runner_binary", "glade_sha", "default_host"):
            if key in data: settings[key] = str(data[key]).strip()
        path = settings.get("tiers_file", "")
        if path:
            document = json.loads(Path(path).expanduser().read_text())
            if not isinstance(document, dict) or not isinstance(document.get("repos"), list):
                raise ValueError("Tiers file must contain a repos array.")
        sha = settings.get("glade_sha", "")
        if sha and not __import__("re").fullmatch(r"[0-9a-fA-F]{40}", sha):
            raise ValueError("Glade commit must be a full 40-character SHA.")
        (self.store.directory / "dashboard-settings.json").write_text(json.dumps(settings))

    def set_tiers_file(self, path):
        self.save_settings({"tiers_file": path})

    def tiers_snapshot(self):
        try:
            config = json.loads((self.store.directory / "dashboard-settings.json").read_text())
            path = config.get("tiers_file", "")
            if not path:
                return {"path": "", "repos": [], "revision": ""}
            raw = Path(path).expanduser().read_bytes()
            data = json.loads(raw)
            if not isinstance(data, dict) or not isinstance(data.get("repos"), list):
                raise ValueError("Invalid tiers file")
            return {"path": path, "repos": data["repos"], "revision": hashlib.sha256(raw).hexdigest()}
        except (OSError, ValueError, TypeError) as exc:
            return {"path": locals().get("path", ""), "repos": [], "revision": "", "error": str(exc)}

    @staticmethod
    def repo_tier_names(repo):
        names = {str(repo.get("name") or "").lower().replace("/", "__")}
        names.update(Path(x.get("path", "")).name.lower() for x in repo.get("locations", []))
        names.discard("")
        return names

    def tier_index(self, repos=None):
        index = {}
        for repo in self.store.all("repositories") if repos is None else repos:
            for name in self.repo_tier_names(repo):
                index.setdefault(name, set()).add(repo["id"])
        return index

    def tier_entry(self, repo, entries, index=None):
        names = self.repo_tier_names(repo)
        matches = [x for x in entries if str(x.get("name", "")).lower() in names]
        index = self.tier_index() if index is None else index
        if len(matches) != 1 or len(index.get(str(matches[0].get("name", "")).lower(), ())) != 1:
            return None
        return matches[0]

    def tier_ambiguous(self, repo, entries):
        possible = [x for x in entries if str(x.get("name", "")).lower() in self.repo_tier_names(repo)]
        return bool(possible) and self.tier_entry(repo, entries) is None

    def move_tier(self, repo, tier, revision, pin=None):
        if pin is None and tier not in ("A", "B", "C", "unassigned"):
            raise ValueError("Invalid group.")
        with self.store.lock:
            snapshot = self.tiers_snapshot()
            if not snapshot["path"] or not snapshot["revision"]:
                raise ValueError("Configure an accessible tiers file first.")
            if revision != snapshot["revision"]:
                raise ValueError("The tiers file changed on disk. Reload and try again.")
            path = Path(snapshot["path"]).expanduser()
            raw = path.read_bytes()
            if hashlib.sha256(raw).hexdigest() != revision:
                raise ValueError("The tiers file changed on disk. Reload and try again.")
            doc = json.loads(raw)
            entry = self.tier_entry(repo, doc["repos"])
            possible = [x for x in doc["repos"] if str(x.get("name", "")).lower() in self.repo_tier_names(repo)]
            if possible and not entry:
                raise ValueError("Ambiguous tiers entry; resolve its identity before editing.")
            if pin is not None:
                if not entry:
                    raise ValueError("This repository has no unambiguous tiers entry.")
                entry["pin"] = pin
                entry["blocked"] = "needs golden; rebuild in a reviewed PR"
            elif tier == "unassigned":
                if entry: doc["repos"].remove(entry)
            elif entry:
                entry["tier"] = tier
            else:
                loc = next((x for x in repo.get("locations", []) if x.get("id") == repo.get("tracked_location_id")), None)
                if not loc or not loc.get("path") or not loc.get("head"):
                    raise ValueError("Track a checkout with a known revision before assigning a new group.")
                doc["repos"].append({"name": str(repo.get("name") or Path(loc.get("path", "")).name).replace("/", "__"), "tier": tier, "clone": loc.get("path", ""), "pin": loc.get("head") or "", "overlay": {}, "blocked": "added from dashboard; needs golden"})
            fd, temporary = tempfile.mkstemp(prefix=".tiers-", dir=path.parent)
            try:
                with os.fdopen(fd, "w") as out:
                    json.dump(doc, out, indent=2, ensure_ascii=False)
                    out.write("\n")
                    out.flush()
                    os.fsync(out.fileno())
                os.chmod(temporary, path.stat().st_mode & 0o777)
                if hashlib.sha256(path.read_bytes()).hexdigest() != revision:
                    raise ValueError("The tiers file changed on disk. Reload and try again.")
                os.replace(temporary, path)
            finally:
                if os.path.exists(temporary): os.unlink(temporary)
        self.store.event("pin accepted" if pin is not None else "tier move", repo["id"], pin if pin is not None else tier)
        return {"ok": True}

    def accept_pin(self, repo, pin, revision):
        if not isinstance(pin, str) or not __import__("re").fullmatch(r"[0-9a-fA-F]{40}", pin):
            raise ValueError("A full checkout SHA is required.")
        return self.move_tier(repo, None, revision, pin=pin)

    def state(self, full=False):
        data = self.store.state(full=full)
        tiers = self.tiers_snapshot()
        data["settings"].update(self.settings())
        data["settings"]["tiers"] = tiers
        tier_index = self.tier_index(data["repositories"])
        for repo in data["repositories"]:
            repo["private"] = self.ops.is_private(repo)
            if repo["private"] and not repo.get("url"):
                repo["upstream"] = {"status": "private"}
            elif not repo.get("url") and "__" not in repo.get("name", ""):
                repo["upstream"] = {"status": "no-remote"}
            entry = self.tier_entry(repo, tiers["repos"], tier_index)
            repo["tier"] = entry.get("tier", "unassigned") if entry else "unassigned"
            repo["pin"] = entry.get("pin") if entry else None
        return data

    def delete_pull(self, repo_id, location_id):
        from . import ops
        repo = self.store.get("repositories", repo_id)
        location = next((x for x in repo.get("locations", []) if x.get("id") == location_id), None)
        if not location or not location.get("pulled") or not location.get("ownership_token"):
            raise ValueError("Only dashboard-created checkouts with verified ownership can be deleted.")
        if location_id == repo.get("tracked_location_id"):
            raise ValueError("Cannot delete the tracked checkout.")
        host = self.store.get("hosts", location["host"])
        ops.host_python(host, ops.DELETE_PULL, {"root":host.get("managed_root"), "path":location["path"], "token":location["ownership_token"]}, timeout=120)
        repo["locations"] = [x for x in repo["locations"] if x.get("id") != location_id]
        self.store.put("repositories", repo)
        self.store.event("deleted pulled checkout", repo_id, location.get("head", ""))
        return "Deleted the dashboard-created checkout."

    def test_update(self, repo_id, location_id):
        repo = self.store.get("repositories", repo_id)
        location = next((x for x in repo.get("locations", []) if x.get("id") == location_id and x.get("pulled")), None)
        if not location: raise ValueError("Choose a dashboard-created checkout.")
        host = self.store.get("hosts", location["host"])
        job = getattr(self.job_context, "current", None)
        if job:
            with self.store.lock:
                current = self.store.get("jobs", job["id"])
                if current.get("cancel_requested"): raise ValueError("Cancelled")
                current["host"] = location["host"]
                self.store.put("jobs", current)
                job.update(current)
        if self.ops.host_busy(host): raise ValueError("Host is busy with a gate, test lane, or high load.")
        settings = self.settings()
        if not all(settings.get(x) for x in ("glade_binary", "runner_binary", "glade_sha")):
            raise ValueError("Configure the Glade binary, its commit, and the corpus runner binary.")
        tiers = self.tiers_snapshot()
        entry = self.tier_entry(repo, tiers.get("repos", []))
        if not entry: raise ValueError("A unique tiers entry is required for its overlay.")
        source = json.loads(Path(tiers["path"]).expanduser().read_text())
        dependencies = source.get("depSources", {})
        base = Path(tiers["path"]).expanduser().parent
        for spec in dependencies.values():
            paths = spec.get("paths")
            if paths:
                def absolute(path):
                    return str(base / path) if not path.startswith(("/", "~/")) else path
                spec["paths"] = [absolute(p) for p in paths] if isinstance(paths, list) else absolute(paths)
        from . import ops
        run_entry = dict(entry, pin=location["head"], clone=location["path"], blocked="")
        result = ops.host_python(host, UPDATE_RUNNER, {"root":host.get("managed_root"), "repo":run_entry, "depSources":dependencies, "checkout":location["path"], "sha":location["head"], "glade":settings["glade_binary"], "runner":settings["runner_binary"], "cancel_token":self.ops.cancel_token()}, timeout=1950)
        if not result.get("complete"): raise ValueError("Runner did not complete both phases.")
        observed = result.get("observed", {})
        counts = observed.get("counts", {})
        failures = [x.get("test") for x in observed.get("failing", []) if x.get("test")]
        timeouts = [x.get("test") for x in observed.get("timeouts", []) if x.get("test")]
        diagnostics = [d for d in observed.get("diagnostics", []) if str(d.get("severity", "")).lower() == "error"]
        row = {"repo_id":repo_id,"repo_name":repo["name"],"repo_sha":location["head"],"glade_sha":settings["glade_sha"],"gate_label":"update-"+location["head"][:10],"phase":"test","host":location["host"],"started_at":now(),"status":"failed" if failures or timeouts or diagnostics else "passed","passed":counts.get("passed"),"total":counts.get("tests"),"failed":len(failures),"failed_tests":failures,"timeouts":len(timeouts),"compile_errors":len(diagnostics),"wall_s":observed.get("wallSeconds"),"provenance":"Corpus runner update observation"}
        self.store.put("runs", row)
        repo = self.store.get("repositories", repo_id)
        for loc in repo.get("locations", []):
            if loc.get("id") == location_id: loc["tested"] = True
        self.store.put("repositories", repo)
        self.store.event("update test", repo_id, row["status"])
        return "Update check and test completed."

    def job(self, kind, function, *args, host=None):
        job = self.store.put("jobs", {"kind": kind, "host": host, "status": "queued", "message": "Waiting for the current operation.", "started_at": now()})
        def run():
            self.job_context.current = job
            with self.store.lock:
                current = self.store.get("jobs", job["id"])
                if current.get("cancel_requested"):
                    current.update(status="cancelled", message="Cancelled before starting.", finished_at=now())
                    self.store.put("jobs", current)
                    return
                current.update(status="running", message="Working…")
                self.store.put("jobs", current)
                job.update(current)
            try:
                message = function(*args)
                cancelled = self.store.get("jobs", job["id"]).get("cancel_requested")
                job.update(status="cancelled" if cancelled else "completed", message="Cancelled." if cancelled else str(message or "Done"))
            except Exception as exc:
                cancelled = self.store.get("jobs", job["id"]).get("cancel_requested")
                job.update(status="cancelled" if cancelled else "failed", message="Cancelled; partial checkout may remain." if cancelled else str(exc) if isinstance(exc, ValueError) else "Operation failed. Check host connectivity and input format.")
            job["finished_at"] = now()
            self.store.put("jobs", job)
        self.executor.submit(run)
        return {"job": job["id"]}

    def progress(self, message):
        job = getattr(self.job_context, "current", None)
        if job:
            with self.store.lock:
                current = self.store.get("jobs", job["id"])
                if current.get("cancel_requested"): raise ValueError("Cancelled")
                current["message"] = message
                self.store.put("jobs", current)
                job.update(current)

    def cancel_job(self, job_id):
        with self.store.lock:
            job = self.store.get("jobs", job_id)
            if job.get("status") not in ("queued", "running"):
                raise ValueError("Job is not running or queued.")
            if job.get("status") == "running" and job.get("kind") not in ("Pull checkouts", "Prepare checkout", "Test update", "Test updates"):
                raise ValueError("This running job cannot be cancelled.")
            job["cancel_requested"] = True
            self.store.put("jobs", job)
        if job.get("status") == "running" and job.get("host"):
            host = self.store.get("hosts", job["host"])
            root = host.get("managed_root")
            if root:
                host_python(host, r"""
import json, re
from pathlib import Path
root=Path(DATA['root']).expanduser().resolve()
if 'BACKUP' in root.parts or not re.fullmatch(r'[0-9a-f]{32}',DATA['id']): raise ValueError('Cannot signal this job')
root.mkdir(parents=True, exist_ok=True)
(root / ('.dashboard-cancel-'+DATA['id'])).write_text('cancel')
print(json.dumps({'ok':True}))
""", {"root":root, "id":job_id}, timeout=15)
        return {"ok": True}

    def match_run(self, row, repos=None):
        repos = self.store.all("repositories") if repos is None else repos
        candidates = []
        if row.get("repo_id"):
            candidates = [r for r in repos if r["id"] == row["repo_id"]]
        if not candidates and row.get("repo_url"):
            candidates = [r for r in repos if r["url"].lower().removesuffix(".git") == row["repo_url"].lower().removesuffix(".git")]
        if not candidates and row.get("repo_path"):
            candidates = [r for r in repos if any(x["path"] == row["repo_path"] and x["host"] == row.get("host") for x in r["locations"])]
        if not candidates and row.get("repo_name"):
            name = row["repo_name"].lower().replace("__", "/")
            candidates = [r for r in repos if str(r.get("name", "")).lower() == name or str(r.get("name", "")).lower().split("/")[-1] == name or any(Path(x["path"]).name.lower().replace("__", "/") == name for x in r.get("locations", []))]
            if len(candidates) > 1:
                local = [r for r in candidates if any(x.get("host") == row.get("host") for x in r.get("locations", []))]
                if len(local) == 1: candidates = local
        if len(candidates) == 1:
            row["repo_id"] = candidates[0]["id"]
            row["repo_name"] = candidates[0]["name"]
            row["identity_match"] = "unique catalog match"
        else:
            row["repo_id"] = None
            row["identity_match"] = "ambiguous" if candidates else "unmatched"
        return row

    def save_runs(self, parsed, started_at=None):
        rows = []
        repos = self.store.all("repositories")
        for row in parsed["runs"]:
            key = identifier(row.get("source"), row.get("host"), row.get("repo_path"), row.get("repo_name"), row.get("glade_sha"), row.get("phase"), row.get("part"), row.get("shard"))
            row = self.match_run(row, repos)
            if not row.get("started_at") and started_at:
                row["started_at"] = started_at
                row["provenance"] += "; timestamp from dispatch"
            row["id"] = key
            rows.append(row)
        self.store.put_many("runs", rows)
        return len(rows)

    def import_history(self, data):
        host_id = data.get("host", "local")
        path = str(data.get("path", ""))
        if not path:
            raise ValueError("Choose an aggregate JSON or JSONL file.")
        if host_id in ("local", ""):
            parsed = parse_history(path, glade_version=data.get("glade_version", ""), glade_sha=data.get("glade_sha", ""), host=host_id or "local")
        else:
            from .history import parse_history_data
            host = self.store.get("hosts", host_id)
            raw = host_python(host, '''
import json, stat
from pathlib import Path
p=Path(DATA['path']).expanduser()
if p.suffix not in ('.json','.jsonl') or not stat.S_ISREG(p.stat().st_mode) or p.stat().st_size > 40*1024*1024: raise ValueError('Invalid aggregate')
with p.open('r',encoding='utf-8-sig') as stream: text=stream.read(40*1024*1024+1)
if len(text.encode()) > 40*1024*1024: raise ValueError('Aggregate too large')
print(json.dumps({'content':text}))
''', {"path": path}, timeout=60)
            parsed = parse_history_data(raw["content"], suffix=Path(path).suffix, source=path, glade_version=data.get("glade_version", ""), glade_sha=data.get("glade_sha", ""), host=host_id)
        count = self.save_runs(parsed, data.get("started_at"))
        if not count:
            raise ValueError("No history rows imported. " + " ".join(parsed["warnings"]))
        return "Imported %s aggregate rows. %s" % (count, " ".join(parsed["warnings"]))

    def import_bootstrap(self):
        from .history import parse_history_data
        from .sources import refresh_sources
        path = self.store.directory / "history-sources.json"
        if not path.exists():
            raise ValueError("No history sources configured.")
        config = refresh_sources(json.loads(path.read_text()))
        good, failed, count = 0, 0, 0
        sources = config.get("sources", [])
        for host in self.store.all("hosts"):
            selected = [s for s in sources if s["host"] == host["id"]]
            for start in range(0, len(selected), 12):
                batch = selected[start:start+12]
                self.progress("Importing saved aggregates: %s sources, %s rows imported; %s unavailable." % (good, count, failed))
                try:
                    result = host_python(host, '''
import json, stat
from pathlib import Path
rows=[]
remaining=64*1024*1024
for index,entry in enumerate(DATA['sources']):
    try:
        p=Path(entry['path']).expanduser()
        if p.suffix not in ('.json','.jsonl') or not stat.S_ISREG(p.stat().st_mode) or p.stat().st_size > min(40*1024*1024,remaining): raise ValueError()
        with p.open('r',encoding='utf-8-sig') as stream: content=stream.read(min(40*1024*1024,remaining)+1)
        length=len(content.encode())
        if length>min(40*1024*1024,remaining): raise ValueError()
        remaining-=length
        rows.append({'index':index,'content':content})
    except (OSError,ValueError,UnicodeError): rows.append({'index':index,'error':True})
print(json.dumps(rows))
''', {"sources": batch}, timeout=120)
                    for item in result:
                        if item.get("error"):
                            failed += 1
                            continue
                        source = batch[item["index"]]
                        parsed = parse_history_data(item["content"], suffix=Path(source["path"]).suffix, source=source["path"], host=host["id"], glade_sha=source.get("glade_sha", ""), glade_version=source.get("glade_version", ""), started_at=source.get("started_at", ""))
                        for row in parsed["runs"]: row["gate_label"] = source.get("label")
                        count += self.save_runs(parsed)
                        good += 1
                except ValueError:
                    failed += len(batch)
        if not good or not count:
            raise ValueError("No history sources could be imported; inspect configured hosts and aggregate paths.")
        return "Imported %s rows from %s history sources; %s unavailable sources left untouched. Historical repository revisions remain unknown where unrecorded. %s" % (count, good, failed, " ".join(config.get("warnings", [])))

    def mutate(self, path, data):
        parts = path.strip("/").split("/")
        if path == "/api/settings":
            self.save_settings(data)
            return {"ok": True}
        if path == "/api/scan":
            return self.job("Inventory", self.ops.scan, data.get("host"))
        if path == "/api/test-many":
            ids = data.get("repo_ids")
            if not isinstance(ids, list) or not ids or len(ids) > 500: raise ValueError("Select between one and 500 repositories.")
            def test_many():
                failures = 0
                for i, repo_id in enumerate(ids):
                    self.progress("Testing %s of %s" % (i+1, len(ids)))
                    repo = self.store.get("repositories", repo_id)
                    loc = next((x for x in reversed(repo.get("locations", [])) if x.get("pulled") and not x.get("dismissed")), None)
                    if not loc: failures += 1; continue
                    try: self.test_update(repo_id, loc["id"])
                    except ValueError: failures += 1
                if failures: raise ValueError("Completed %s update tests; %s unavailable or failed." % (len(ids)-failures, failures))
                return "Completed %s update tests." % len(ids)
            return self.job("Test updates", test_many)
        if path == "/api/pull-many":
            ids = data.get("repo_ids")
            host = data.get("host")
            if not isinstance(ids, list) or not ids or len(ids) > 500:
                raise ValueError("Select between one and 500 repositories.")
            def pull_many():
                errors = 0
                for i, repo_id in enumerate(ids):
                    self.progress("Pulling %s of %s" % (i + 1, len(ids)))
                    try: self.ops.snapshot(repo_id, host)
                    except ValueError: errors += 1
                if errors: raise ValueError("Prepared %s checkouts; %s failed." % (len(ids)-errors, errors))
                return "Prepared %s checkouts." % len(ids)
            return self.job("Pull checkouts", pull_many, host=host)
        if path == "/api/check":
            return self.job("Upstream checks", self.ops.check_all)
        if path == "/api/import":
            return self.job("History import", self.import_history, data)
        if path == "/api/import-bootstrap":
            return self.job("Historical scan import", self.import_bootstrap)
        if path == "/api/repos":
            return self.ops.add(data.get("url", ""))
        if path == "/api/hosts":
            host = validate_host(data)
            self.store.put("hosts", host)
            return host
        if path == "/api/queries":
            query = str(data.get("query", "")).strip()
            if not query or len(query) > 256:
                raise ValueError("Enter a GitHub search query of 1–256 characters.")
            return self.store.put("queries", {"id": data.get("id") or identifier(query), "name": str(data.get("name") or query), "query": query})
        if path == "/api/runs":
            repo = self.store.get("repositories", data.get("repo_id"))
            row = {key: data.get(key) for key in ("glade_version", "glade_sha", "repo_sha", "api_version", "status", "passed", "failed", "skipped", "total", "host", "started_at", "source")}
            for key in ("passed", "failed", "skipped", "total"):
                if row[key] not in (None, ""):
                    row[key] = int(row[key])
                    if row[key] < 0:
                        raise ValueError("Counts cannot be negative.")
                else:
                    row[key] = None
            row.update(repo_id=repo["id"], repo_name=repo["name"], provenance="Manually recorded; not an executed validation", started_at=row.get("started_at") or now())
            self.store.event("test recorded", repo["id"], row.get("status") or "unknown")
            return self.store.put("runs", row)
        if len(parts) == 4 and parts[1] == "jobs" and parts[3] == "cancel":
            return self.cancel_job(parts[2])
        if len(parts) == 4 and parts[1] == "jobs" and parts[3] == "dismiss":
            job = self.store.get("jobs", parts[2])
            if job.get("status") != "failed": raise ValueError("Only failed jobs can be dismissed.")
            job["dismissed"] = True
            return self.store.put("jobs", job)
        if len(parts) >= 3 and parts[1] == "repos":
            repo = self.store.get("repositories", parts[2])
            if len(parts) == 4 and parts[3] == "delete-pull":
                return self.job("Delete pulled checkout", self.delete_pull, repo["id"], data.get("location_id"))
            if len(parts) == 4 and parts[3] == "keep-pin":
                loc = next((x for x in repo.get("locations", []) if x.get("id") == data.get("location_id") and x.get("pulled")), None)
                if not loc: raise ValueError("Choose a dashboard-created checkout.")
                loc["dismissed"] = True
                self.store.put("repositories", repo)
                self.store.event("kept pin", repo["id"], loc.get("head", ""))
                return {"ok": True}
            if len(parts) == 4 and parts[3] == "test-update":
                location = next((x for x in repo.get("locations", []) if x.get("id") == data.get("location_id") and x.get("pulled")), None)
                if not location: raise ValueError("Choose a dashboard-created checkout.")
                return self.job("Test update", self.test_update, repo["id"], location["id"], host=location["host"])
            if len(parts) == 4 and parts[3] == "accept-pin":
                loc = next((x for x in repo.get("locations", []) if x.get("id") == data.get("location_id") and x.get("pulled")), None)
                if not loc: raise ValueError("Choose a dashboard-created checkout.")
                return self.accept_pin(repo, loc.get("head"), data.get("revision"))
            if len(parts) == 4 and parts[3] == "tier":
                return self.move_tier(repo, data.get("tier"), data.get("revision"))
            if len(parts) == 4 and parts[3] == "check":
                return self.job("Upstream check", self.ops.check, repo["id"])
            if len(parts) == 4 and parts[3] == "snapshot":
                return self.job("Prepare checkout", self.ops.snapshot, repo["id"], data.get("host"), host=data.get("host"))
            if len(parts) == 3:
                for key in ("notes", "status", "tags", "tracked_location_id"):
                    if key in data:
                        repo[key] = data[key]
                if repo["status"] not in ("active", "archived", "candidate"):
                    raise ValueError("Unknown repository status.")
                if not isinstance(repo["tags"], list) or not all(isinstance(x, str) for x in repo["tags"]):
                    raise ValueError("Tags must be a list of strings.")
                if "tracked_location_id" in data:
                    if not any(x["id"] == data["tracked_location_id"] for x in repo["locations"]):
                        raise ValueError("Choose one of this repository's known checkouts.")
                    repo["upstream"] = {"status": "unknown"}
                self.store.put("repositories", repo)
                self.store.event("note updated" if "notes" in data else "catalog updated", repo["id"], str(data.get("notes") or "") if "notes" in data else repo["status"])
                return repo
        if len(parts) >= 3 and parts[1] == "queries":
            query = self.store.get("queries", parts[2])
            if len(parts) == 4 and parts[3] == "run":
                return self.job("Discovery search", self.ops.discover, query["id"])
            if len(parts) == 4 and parts[3] == "delete":
                self.store.delete("queries", query["id"])
                return {"ok": True}
        if len(parts) >= 3 and parts[1] == "candidates":
            candidate = self.store.get("candidates", parts[2])
            if len(parts) == 4 and parts[3] == "project-check":
                return self.job("Salesforce project check", self.ops.check_candidate, candidate["id"])
            if len(parts) == 4 and parts[3] == "add":
                added = self.ops.add(candidate["url"])
                self.store.event("candidate added", added["id"])
                candidate["status"] = "added"
            elif len(parts) == 3 and data.get("status") in ("new", "dismissed"):
                candidate["status"] = data["status"]
            else:
                raise ValueError("Unknown candidate action.")
            return self.store.put("candidates", candidate)
        raise ValueError("Unknown API operation.")


def handler(app):
    class Handler(BaseHTTPRequestHandler):
        def log_message(self, fmt, *args):
            pass

        def local_request(self):
            host = self.headers.get("Host", "")
            return host in {"127.0.0.1:%s" % self.server.server_port, "localhost:%s" % self.server.server_port}

        def respond(self, status, payload, content_type="application/json"):
            body = json.dumps(payload, ensure_ascii=False).encode() if content_type == "application/json" else payload
            self.send_response(status)
            self.send_header("Content-Type", content_type + "; charset=utf-8")
            self.send_header("Content-Length", str(len(body)))
            self.send_header("Cache-Control", "no-store")
            self.send_header("X-Content-Type-Options", "nosniff")
            self.send_header("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'self'; connect-src 'self'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
            self.end_headers()
            self.wfile.write(body)

        def do_GET(self):
            if not self.local_request():
                return self.respond(403, {"error": "Loopback host required."})
            path = urllib.parse.urlsplit(self.path).path
            if path == "/api/health":
                return self.respond(200, {"app": "glade-corpus-manager"})
            if path == "/api/state":
                data = app.state()
                data["csrf_token"] = app.token
                data["settings"]["github_authenticated"] = bool(app.ops.github.token)
                return self.respond(200, data)
            if path == "/api/export":
                data = app.state(full=True)
                data.pop("csrf_token", None)
                return self.respond(200, data)
            if path == "/api/history":
                query = urllib.parse.parse_qs(urllib.parse.urlsplit(self.path).query)
                try:
                    offset = max(0, int(query.get("offset", ["0"])[0]))
                    limit = min(500, max(1, int(query.get("limit", ["100"])[0])))
                except ValueError:
                    return self.respond(400, {"error": "Invalid pagination."})
                repo_id = query.get("repo_id", [""])[0]
                needle = query.get("q", [""])[0].lower()
                status = query.get("status", [""])[0]
                phase = query.get("phase", [""])[0]
                rows = [r for r in app.store.all("runs") if (not phase or r.get("phase") == phase) and (not repo_id or r.get("repo_id") == repo_id) and (not status or r.get("status") == status) and (not needle or needle in " ".join(str(r.get(k) or "") for k in ("repo_name", "gate_label", "glade_sha", "glade_version", "repo_sha", "host", "status")).lower())]
                rows.sort(key=lambda r: r.get("started_at") or "", reverse=True)
                return self.respond(200, {"runs": rows[offset:offset+limit], "total": len(rows), "offset": offset, "limit": limit})
            assets = {"/": ("index.html", "text/html"), "/app.js": ("app.js", "text/javascript"), "/style.css": ("style.css", "text/css")}
            if path.startswith("/assets/") and Path(path).name == path.removeprefix("/assets/"):
                asset = STATIC / "assets" / Path(path).name
                mime = {".woff2": "font/woff2", ".svg": "image/svg+xml", ".txt": "text/plain"}.get(asset.suffix)
                if mime and asset.is_file():
                    return self.respond(200, asset.read_bytes(), mime)
            if path in assets:
                name, mime = assets[path]
                return self.respond(200, (STATIC / name).read_bytes(), mime)
            self.respond(404, {"error": "Not found"})

        def do_POST(self):
            if not self.local_request() or not secrets.compare_digest(self.headers.get("X-Corpus-Token", ""), app.token):
                return self.respond(403, {"error": "Reload the local dashboard to authorize this operation."})
            origin = self.headers.get("Origin")
            if origin and origin != "http://" + self.headers["Host"]:
                return self.respond(403, {"error": "Same-origin request required."})
            if self.headers.get("Content-Type", "").split(";")[0] != "application/json":
                return self.respond(415, {"error": "JSON required."})
            try:
                size = int(self.headers.get("Content-Length", "0"))
                if not 0 < size <= 1024 * 1024:
                    raise ValueError("Request body must be between 1 byte and 1 MiB.")
                data = json.loads(self.rfile.read(size))
                if not isinstance(data, dict):
                    raise ValueError("Expected a JSON object.")
                result = app.mutate(urllib.parse.urlsplit(self.path).path, data)
                self.respond(200, result)
            except (ValueError, TypeError, KeyError) as exc:
                self.respond(400, {"error": str(exc)})
            except Exception:
                self.respond(500, {"error": "The operation could not be completed."})
    return Handler


def main():
    parser = argparse.ArgumentParser(description="Local Glade corpus dashboard")
    parser.add_argument("--data-dir", default=str(DEFAULT_DATA))
    parser.add_argument("--port", type=int, default=8766)
    parser.add_argument("--open", action="store_true")
    args = parser.parse_args()
    os.umask(0o077)
    # Opening another window must not recover jobs belonging to the live server.
    import urllib.request
    try:
        with urllib.request.urlopen("http://127.0.0.1:%s/api/health" % args.port, timeout=1) as response:
            existing = json.load(response)
        if existing.get("app") == "glade-corpus-manager":
            if args.open:
                webbrowser.open("http://127.0.0.1:%s" % args.port)
            return
    except (OSError, ValueError):
        pass
    app = App(args.data_dir)
    try:
        server = ThreadingHTTPServer(("127.0.0.1", args.port), handler(app))
    except OSError:
        import urllib.request
        try:
            with urllib.request.urlopen("http://127.0.0.1:%s/api/health" % args.port, timeout=2) as response:
                if json.load(response).get("app") != "glade-corpus-manager":
                    raise ValueError("Another service is using the dashboard port.")
            if args.open:
                webbrowser.open("http://127.0.0.1:%s" % args.port)
            return
        finally:
            app.executor.shutdown(wait=False)
    url = "http://127.0.0.1:%s" % server.server_port
    print("Glade Corpus: " + url, flush=True)
    if args.open:
        webbrowser.open(url)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()
        app.executor.shutdown(wait=False, cancel_futures=True)


if __name__ == "__main__":
    main()
