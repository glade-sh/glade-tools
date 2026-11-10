"""Metadata-only inventory and GitHub operations; snapshots never replace a checkout."""
import json
import os
import re
import shlex
import socket
import subprocess
import sys
import textwrap
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path
from .store import identifier, now


def github_name(value):
    value = str(value).strip()
    if value.startswith("git@github.com:"):
        value = value.split(":", 1)[1]
    elif "://" in value:
        parsed = urllib.parse.urlsplit(value)
        if parsed.hostname != "github.com" or parsed.username or parsed.password or parsed.query or parsed.fragment:
            raise ValueError("Use a GitHub repository URL without credentials or query parameters.")
        value = parsed.path.strip("/")
    value = value.removesuffix(".git").strip("/")
    if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_.-]*/[A-Za-z0-9_.-]+", value) or any(p in (".", "..") for p in value.split("/")):
        raise ValueError("Enter a GitHub owner/repository or repository URL.")
    return value


class GitHub:
    def __init__(self):
        self.token = os.environ.get("GH_TOKEN") or os.environ.get("GITHUB_TOKEN")
        if not self.token:
            try:
                p = subprocess.run(["gh", "auth", "token", "--hostname", "github.com"], capture_output=True, text=True, timeout=10)
                self.token = p.stdout.strip() if p.returncode == 0 else None
            except (OSError, subprocess.TimeoutExpired):
                pass

    def get(self, route, params=None):
        url = "https://api.github.com/" + route.lstrip("/")
        if params:
            url += "?" + urllib.parse.urlencode(params)
        headers = {"Accept": "application/vnd.github+json", "User-Agent": "glade-corpus-manager", "X-GitHub-Api-Version": "2022-11-28"}
        if self.token:
            headers["Authorization"] = "Bearer " + self.token
        try:
            with urllib.request.urlopen(urllib.request.Request(url, headers=headers), timeout=25) as response:
                return json.load(response)
        except urllib.error.HTTPError as exc:
            if exc.code in (403, 429):
                raise ValueError("GitHub denied this request or its rate limit was reached. Try later or authenticate gh.") from None
            raise ValueError("GitHub returned HTTP %s." % exc.code) from None
        except (urllib.error.URLError, TimeoutError):
            raise ValueError("GitHub could not be reached. Existing results were retained.") from None


def validate_host(data):
    ssh = str(data.get("ssh") or "").strip()
    if ssh and not re.fullmatch(r"[A-Za-z0-9_][A-Za-z0-9_.@-]*", ssh):
        raise ValueError("SSH must be a configured host alias or user@host.")
    roots = data.get("roots", [])
    if not isinstance(roots, list) or not all(isinstance(x, str) and x.startswith(("/", "~/")) for x in roots):
        raise ValueError("Roots must be absolute paths or ~/ paths.")
    managed = str(data.get("managed_root") or "")
    if managed and (not managed.startswith(("/", "~/")) or "BACKUP" in Path(managed).parts):
        raise ValueError("Choose an absolute managed directory outside BACKUP.")
    if len(roots) > 20:
        raise ValueError("At most 20 roots per host.")
    return {"id": str(data.get("id") or identifier(ssh or "local")), "label": str(data.get("label") or ssh or "This Mac"), "ssh": ssh, "roots": roots, "managed_root": managed}


def host_python(host, script, data, timeout=300):
    source = "DATA = " + repr(data) + "\ntry:\n" + textwrap.indent(script, "    ") + "\nexcept ValueError as error:\n    import json\n    print(json.dumps({'_error':str(error)}))\nexcept Exception:\n    import json\n    print(json.dumps({'_error':'The host could not complete this operation; check paths and permissions.'}))\n"
    command = [sys.executable, "-"]
    if host.get("ssh"):
        command = ["ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=8", "--", host["ssh"], "python3 -"]
    try:
        p = subprocess.run(command, input=source, text=True, capture_output=True, timeout=timeout)
    except subprocess.TimeoutExpired:
        raise ValueError("Host operation timed out; check host connectivity and retry.") from None
    if p.returncode:
        # stderr can contain remote URLs or credentials; do not surface it.
        raise ValueError("Host operation failed (exit %s). Check SSH, Python 3, Git, paths and disk space." % p.returncode)
    try:
        result = json.loads(p.stdout)
    except ValueError:
        raise ValueError("Host did not return valid metadata JSON.") from None
    if isinstance(result, dict) and result.get("_error"):
        raise ValueError(result["_error"])
    return result


INVENTORY = r'''
import json, os, subprocess
from pathlib import Path
def git(path, *args):
    p = subprocess.run(['git','-C',str(path),*args],capture_output=True,text=True,timeout=20,env={**os.environ,'GIT_OPTIONAL_LOCKS':'0','GIT_TERMINAL_PROMPT':'0'})
    return p.stdout.strip() if p.returncode == 0 else None
rows, errors, seen = [], [], set()
roots = list(DATA['roots'])
if DATA.get('managed_root') and Path(DATA['managed_root']).expanduser().is_dir(): roots.append(DATA['managed_root'])
for root in roots:
    root = Path(root).expanduser()
    if not root.is_dir():
        errors.append('A configured inventory root is unavailable.')
        continue
    for directory, dirs, files in os.walk(root, followlinks=False):
        path = Path(directory)
        depth = len(path.relative_to(root).parts)
        if '.git' in dirs or '.git' in files:
            if str(path.resolve()) not in seen:
                seen.add(str(path.resolve()))
                try:
                    head = git(path,'rev-parse','HEAD')
                    if head:
                        status = git(path,'status','--porcelain','--untracked-files=normal')
                        rows.append({'path':str(path), 'name':path.name, 'head':head, 'branch':git(path,'branch','--show-current'),'remote':git(path,'remote','get-url','origin'),'dirty':bool(status) if status is not None else None})
                except Exception:
                    errors.append('A repository metadata read failed.')
            dirs[:] = []
        elif 'sfdx-project.json' in files or 'force-app' in dirs or 'package.xml' in files:
            rows.append({'path':str(path), 'name':path.name, 'head':None, 'branch':None, 'remote':'', 'dirty':None})
            dirs[:] = []
        else:
            dirs[:] = [d for d in dirs if not d.startswith('.') and d not in ('node_modules','BACKUP','__pycache__') and not Path(directory,d).is_symlink()] if depth < 4 else []
print(json.dumps({'rows':rows,'errors':errors}))
'''

SNAPSHOT = r'''
import json, os, shutil, signal, socket, subprocess, tempfile, time, uuid
from pathlib import Path
if socket.gethostname() == DATA.get('local_node'): raise ValueError('Cannot clone onto the dashboard host')
root = Path(DATA['root']).expanduser().resolve()
if 'BACKUP' in root.parts:
    raise ValueError('Protected directory')
existing = root
while not existing.exists(): existing = existing.parent
reserve = 50 * 1024**3
if shutil.disk_usage(existing).free < reserve + max(DATA['size_kb']*2048, 2*1024**3):
    raise ValueError('Insufficient disk reserve')
root.mkdir(parents=True,exist_ok=True)
target = root / (DATA['name'].replace('/','__') + '-' + DATA['sha'][:12] + '-' + uuid.uuid4().hex[:6])
env = {**os.environ,'GIT_TERMINAL_PROMPT':'0','GIT_LFS_SKIP_SMUDGE':'1'}
def git(*args):
    # File output avoids a full pipe blocking Git while free space is monitored.
    with tempfile.TemporaryFile() as output:
        p = subprocess.Popen(['git','-c','core.hooksPath=/dev/null','-c','protocol.file.allow=never',*args],stdout=output,stderr=subprocess.DEVNULL,env=env,start_new_session=True)
        deadline = time.monotonic()+240
        try:
            while p.poll() is None:
                if shutil.disk_usage(root).free < reserve+1024**3 or time.monotonic()>deadline:
                    os.killpg(p.pid, signal.SIGTERM)
                    try: p.wait(timeout=3)
                    except subprocess.TimeoutExpired: os.killpg(p.pid,signal.SIGKILL); p.wait()
                    raise ValueError('Snapshot stopped; disk reserve or time limit reached. Partial checkout preserved at '+str(target))
                time.sleep(0.1)
            if p.returncode: raise ValueError('Git snapshot failed; incomplete directory preserved at '+str(target))
            output.seek(0)
            return output.read(1024*1024).decode().strip()
        finally:
            if p.poll() is None: os.killpg(p.pid,signal.SIGKILL); p.wait()
git('clone','--depth','1','--no-checkout','--',DATA['url'],str(target))
git('-C',str(target),'fetch','--depth','1','origin',DATA['sha'])
git('-C',str(target),'checkout','--detach',DATA['sha'])
if shutil.disk_usage(root).free < reserve: raise ValueError('Disk reserve reached; snapshot preserved for inspection at '+str(target))
print(json.dumps({'path':str(target),'head':git('-C',str(target),'rev-parse','HEAD'),'branch':'','dirty':False}))
'''


class Operations:
    def __init__(self, store):
        self.store = store
        self.github = GitHub()
        self.tiers_snapshot = lambda: {"repos": []}
        self.tier_entry = lambda repo, entries: None

    @staticmethod
    def is_private(repo):
        return bool(repo.get("private")) or any(
            any("priv" in component.lower() for component in Path(loc.get("path", "")).parts)
            for loc in repo.get("locations", []))

    def add(self, url):
        name = github_name(url)
        key = identifier(name.lower())
        try:
            repo = self.store.get("repositories", key)
            repo["status"] = "active"
        except ValueError:
            repo = {"id": key, "name": name, "url": "https://github.com/" + name, "status": "active", "tags": [], "tier": "unassigned", "notes": "", "locations": [], "upstream": {"status": "unknown"}, "created_at": now()}
        self.store.put("repositories", repo)
        self.store.event("added", key)
        return repo

    def scan(self, host_id=None):
        hosts = [self.store.get("hosts", host_id)] if host_id else self.store.all("hosts")
        count, errors = 0, []
        for host in hosts:
            try:
                result = host_python(host, INVENTORY, {"roots": host["roots"], "managed_root": host.get("managed_root")})
                for row in result["rows"]:
                    try:
                        name = github_name(row["remote"])
                        key = identifier(name.lower())
                        url = "https://github.com/" + name
                    except ValueError:
                        name, url = row["name"], ""
                        key = identifier(host["id"], row["path"])
                    try:
                        repo = self.store.get("repositories", key)
                    except ValueError:
                        repo = {"id": key, "name": name, "url": url, "status": "active", "tags": [], "tier": "unassigned", "notes": "", "locations": [], "upstream": {"status": "unknown"}, "created_at": now()}
                    location = {k: row[k] for k in ("path", "head", "branch", "dirty")}
                    location.update(id=identifier(host["id"], row["path"]), host=host["id"], scanned_at=now())
                    old = next((x for x in repo["locations"] if x["id"] == location["id"]), None)
                    if old and old["head"] != location["head"]:
                        self.store.event("revision observed", key, str(old["head"]) + " → " + str(location["head"]))
                        if repo.get("tracked_location_id") == location["id"]:
                            repo["upstream"] = {"status": "unknown"}
                    repo["locations"] = [x for x in repo["locations"] if x["id"] != location["id"]] + [location]
                    repo.setdefault("tracked_location_id", location["id"])
                    self.store.put("repositories", repo)
                    count += 1
                host = self.store.get("hosts", host["id"])
                host.update(last_scan=now(), error=" ".join(result["errors"]))
                errors.extend(result["errors"])
            except Exception as exc:
                host = self.store.get("hosts", host["id"])
                host["error"] = str(exc)
                errors.append(str(exc))
            self.store.put("hosts", host)
        if errors:
            raise ValueError("Imported %s checkouts. %s" % (count, " ".join(dict.fromkeys(errors))))
        return "Imported %s checkout observations." % count

    def check(self, repo_id):
        repo = self.store.get("repositories", repo_id)
        metadata_request = False
        try:
            if self.is_private(repo):
                raise ValueError("Private checkout; GitHub was not contacted.")
            url = repo.get("url") or ""
            if not url:
                candidate = str(repo.get("name") or "").replace("__", "/")
                url = "https://github.com/" + github_name(candidate)
            name = github_name(url)
            metadata_request = True
            meta = self.github.get("repos/" + name)
            metadata_request = False
            branch = meta["default_branch"]
            commit = self.github.get("repos/" + name + "/commits/" + urllib.parse.quote(branch, safe=""))
            upstream = {"status": "unknown", "checked_at": now(), "sha": commit["sha"], "default_branch": branch, "stars": meta["stargazers_count"], "pushed_at": meta["pushed_at"], "archived": meta["archived"], "size_kb": meta["size"], "description": meta.get("description") or ""}
            location = next((x for x in repo["locations"] if x["id"] == repo.get("tracked_location_id")), None)
            entry = self.tier_entry(repo, self.tiers_snapshot().get("repos", [])) or {}
            base = entry.get("pin") or (location or {}).get("head")
            if base:
                upstream["base_sha"] = base
                upstream["base_location_id"] = (location or {}).get("id")
                upstream["base_kind"] = "pin" if entry.get("pin") else "tracked checkout"
                if base == commit["sha"]:
                    upstream.update(status="current", behind=0)
                else:
                    comparison = self.github.get("repos/" + name + "/compare/" + base + "..." + commit["sha"], {"per_page": 1})
                    state = comparison["status"]
                    upstream.update(status={"ahead": "updates", "identical": "current", "diverged": "diverged", "behind": "local-ahead"}.get(state, "unknown"), behind=comparison.get("ahead_by", 0), compare_url=comparison["html_url"])
            else:
                upstream["status"] = "uncloned"
            repo = self.store.get("repositories", repo_id)
            repo["upstream"] = upstream
            if not repo.get("url"):
                repo["url"] = "https://github.com/" + name
            self.store.put("repositories", repo)
            return "Upstream metadata refreshed."
        except Exception as exc:
            repo = self.store.get("repositories", repo_id)
            previous = repo.get("upstream", {})
            previous.update(status="gone" if metadata_request and "HTTP 404" in str(exc) else "error", error=str(exc), checked_at=now())
            repo["upstream"] = previous
            self.store.put("repositories", repo)
            raise

    def check_all(self):
        repos = [r for r in self.store.all("repositories") if r["status"] == "active" and (r.get("url") or "__" in r.get("name", "")) and not self.is_private(r)]
        failed = 0
        for repo in repos:
            try:
                self.check(repo["id"])
            except ValueError as exc:
                failed += 1
                if "rate limit" in str(exc):
                    raise ValueError("GitHub rate limit reached; completed checks retained and remaining repositories left unchanged.")
        if failed:
            raise ValueError("Checked %s repositories; %s failed. Inspect repository details." % (len(repos), failed))
        return "Checked %s repositories." % len(repos)

    def discover(self, query_id):
        query = self.store.get("queries", query_id)
        try:
            result = self.github.get("search/repositories", {"q": query["query"], "sort": "updated", "order": "desc", "per_page": 50})
            for row in result["items"]:
                key = str(row["id"])
                try:
                    existing = self.store.get("candidates", key)
                except ValueError:
                    existing = {}
                status = existing.get("status", "new")
                if any(r.get("url", "").lower() == row["html_url"].lower() for r in self.store.all("repositories")):
                    status = "added"
                self.store.put("candidates", {"id": key, "full_name": row["full_name"], "url": row["html_url"], "description": row.get("description") or "", "stars": row["stargazers_count"], "language": row.get("language"), "pushed_at": row["pushed_at"], "query_id": query_id, "status": status, "first_seen": existing.get("first_seen", now()), "checked_at": now(), "archived": row["archived"], "license": (row.get("license") or {}).get("spdx_id", "Unknown"), "salesforce_project": existing.get("salesforce_project", "unknown")})
            try:
                current_query = self.store.get("queries", query_id)
                if current_query["query"] == query["query"]:
                    current_query.update(last_checked=now(), error="", total_count=result["total_count"], incomplete=bool(result.get("incomplete_results")), returned=len(result["items"]))
                    self.store.put("queries", current_query)
            except ValueError:
                pass
            return "Reviewed %s results of %s matches (50-result limit per search)." % (len(result["items"]), result["total_count"])
        except Exception as exc:
            try:
                query = self.store.get("queries", query_id)
                query.update(error=str(exc))
                self.store.put("queries", query)
            except ValueError:
                pass
            raise

    def check_candidate(self, candidate_id):
        candidate = self.store.get("candidates", candidate_id)
        if candidate.get("salesforce_project") in ("confirmed", "absent"):
            return "Cached project check."
        try:
            self.github.get("repos/" + github_name(candidate["url"]) + "/contents/sfdx-project.json")
            candidate["salesforce_project"] = "confirmed"
        except ValueError as exc:
            if "HTTP 404" in str(exc):
                candidate["salesforce_project"] = "absent"
            else:
                candidate["salesforce_project"] = "unknown"
                self.store.put("candidates", candidate)
                raise
        self.store.put("candidates", candidate)
        return "Project check complete."

    def snapshot(self, repo_id, host_id):
        host = self.store.get("hosts", host_id)
        if not host.get("ssh") or not host.get("managed_root"):
            raise ValueError("Choose a remote host with a managed snapshot directory; corpus payloads are not copied to this Mac.")
        self.check(repo_id)
        repo = self.store.get("repositories", repo_id)
        up = repo["upstream"]
        result = host_python(host, SNAPSHOT, {"root": host["managed_root"], "url": repo["url"], "name": github_name(repo["url"]), "sha": up["sha"], "size_kb": up.get("size_kb", 0), "local_node": socket.gethostname()}, timeout=900)
        result.update(id=identifier(host_id, result["path"]), host=host_id, scanned_at=now(), managed=True)
        repo = self.store.get("repositories", repo_id)
        repo["locations"].append(result)
        repo["tracked_location_id"] = result["id"]
        repo["upstream"].update(status="current", behind=0, base_sha=result["head"], base_location_id=result["id"])
        self.store.put("repositories", repo)
        self.store.event("snapshot prepared", repo_id, result["head"])
        return "Prepared a pinned checkout on the remote host. Existing checkouts and runs were preserved."
