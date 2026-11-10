"""Loopback dashboard API. Operations are serialized; mutation needs a page token."""
import argparse
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
from .ops import Operations, host_python, validate_host
from .store import Store, identifier, now


DEFAULT_DATA = Path.home() / ".local/share/glade-corpus-manager"
STATIC = Path(__file__).parent / "static"


class App:
    def __init__(self, directory):
        self.store = Store(directory)
        self.ops = Operations(self.store)
        self.token = secrets.token_urlsafe(32)
        self.executor = ThreadPoolExecutor(max_workers=1, thread_name_prefix="corpus")
        self.job_context = threading.local()
        config = self.store.directory / "config.json"
        if config.exists() and not self.store.all("hosts"):
            for host in json.loads(config.read_text()).get("hosts", []):
                self.store.put("hosts", validate_host(host))
        if not self.store.all("queries"):
            for title, query in [("Active Apex projects", "language:Apex archived:false fork:false stars:>=5"), ("Lightning Web Components", "topic:lightning-web-components archived:false fork:false stars:>=5"), ("New Salesforce projects", "topic:salesforce archived:false fork:false created:>=2026-01-01")]:
                self.store.put("queries", {"id": identifier(title), "name": title, "query": query})

    def job(self, kind, function, *args):
        job = self.store.put("jobs", {"kind": kind, "status": "queued", "message": "Waiting for the current operation.", "started_at": now()})
        def run():
            self.job_context.current = job
            job.update(status="running", message="Working…")
            self.store.put("jobs", job)
            try:
                message = function(*args)
                job.update(status="completed", message=str(message or "Done"))
            except Exception as exc:
                # Only controlled errors enter the UI. Never expose subprocess output.
                job.update(status="failed", message=str(exc) if isinstance(exc, ValueError) else "Operation failed. Check host connectivity and input format.")
            job["finished_at"] = now()
            self.store.put("jobs", job)
        self.executor.submit(run)
        return {"job": job["id"]}

    def progress(self, message):
        job = getattr(self.job_context, "current", None)
        if job:
            job["message"] = message
            self.store.put("jobs", job)

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
            name = row["repo_name"]
            candidates = [r for r in repos if r["name"] == name or any(Path(x["path"]).name == name and x["host"] == row.get("host") for x in r["locations"])]
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
                        count += self.save_runs(parsed)
                        good += 1
                except ValueError:
                    failed += len(batch)
        if not good or not count:
            raise ValueError("No history sources could be imported; inspect configured hosts and aggregate paths.")
        return "Imported %s rows from %s history sources; %s unavailable sources left untouched. Historical repository revisions remain unknown where unrecorded. %s" % (count, good, failed, " ".join(config.get("warnings", [])))

    def mutate(self, path, data):
        parts = path.strip("/").split("/")
        if path == "/api/scan":
            return self.job("Inventory", self.ops.scan, data.get("host"))
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
            return self.store.put("runs", row)
        if len(parts) >= 3 and parts[1] == "repos":
            repo = self.store.get("repositories", parts[2])
            if len(parts) == 4 and parts[3] == "check":
                return self.job("Upstream check", self.ops.check, repo["id"])
            if len(parts) == 4 and parts[3] == "snapshot":
                return self.job("Prepare checkout", self.ops.snapshot, repo["id"], data.get("host"))
            if len(parts) == 3:
                for key in ("notes", "tier", "status", "tags", "tracked_location_id"):
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
                self.store.event("catalog updated", repo["id"], repo["status"])
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
            if len(parts) == 4 and parts[3] == "add":
                self.ops.add(candidate["url"])
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
                data = app.store.state()
                data["csrf_token"] = app.token
                data["settings"]["github_authenticated"] = bool(app.ops.github.token)
                return self.respond(200, data)
            if path == "/api/export":
                data = app.store.state(full=True)
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
                rows = [r for r in app.store.all("runs") if (not repo_id or r.get("repo_id") == repo_id) and (not status or r.get("status") == status) and (not needle or needle in " ".join(str(r.get(k) or "") for k in ("repo_name", "glade_sha", "glade_version", "repo_sha", "host", "status")).lower())]
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
