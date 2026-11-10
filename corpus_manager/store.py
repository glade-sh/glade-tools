"""Small durable metadata store; no corpus source or credentials are stored here."""
import hashlib
import json
import os
import sqlite3
import threading
import uuid
from datetime import datetime, timezone
from pathlib import Path


def now():
    return datetime.now(timezone.utc).isoformat(timespec="seconds")


def identifier(*parts):
    return hashlib.sha256("\0".join(str(p) for p in parts).encode()).hexdigest()[:24]


class Store:
    tables = ("repositories", "hosts", "queries", "candidates", "runs", "jobs", "events")

    def __init__(self, directory):
        self.directory = Path(directory).expanduser()
        self.directory.mkdir(parents=True, exist_ok=True, mode=0o700)
        self.lock = threading.RLock()
        self.db = sqlite3.connect(self.directory / "catalog.sqlite3", check_same_thread=False)
        os.chmod(self.directory / "catalog.sqlite3", 0o600)
        self.db.execute("PRAGMA journal_mode=WAL")
        self.db.execute("CREATE TABLE IF NOT EXISTS records (kind TEXT, id TEXT, value TEXT, PRIMARY KEY(kind,id))")
        self.db.commit()
        for job in self.all("jobs"):
            if job["status"] in ("queued", "running"):
                job.update(status="interrupted", message="Server stopped before this operation completed.", finished_at=now())
                self.put("jobs", job)

    def all(self, kind):
        with self.lock:
            return [json.loads(r[0]) for r in self.db.execute("SELECT value FROM records WHERE kind=? ORDER BY rowid", (kind,))]

    def get(self, kind, key):
        with self.lock:
            row = self.db.execute("SELECT value FROM records WHERE kind=? AND id=?", (kind, key)).fetchone()
            if row is None:
                raise ValueError("Record not found")
            return json.loads(row[0])

    def put(self, kind, value):
        assert kind in self.tables
        value.setdefault("id", uuid.uuid4().hex)
        with self.lock:
            self.db.execute("INSERT INTO records VALUES (?,?,?) ON CONFLICT(kind,id) DO UPDATE SET value=excluded.value", (kind, value["id"], json.dumps(value)))
            self.db.commit()
        return value

    def delete(self, kind, key):
        with self.lock:
            self.db.execute("DELETE FROM records WHERE kind=? AND id=?", (kind, key))
            self.db.commit()

    def put_many(self, kind, values):
        assert kind in self.tables
        with self.lock:
            self.db.executemany("INSERT INTO records VALUES (?,?,?) ON CONFLICT(kind,id) DO UPDATE SET value=excluded.value", [(kind, value["id"], json.dumps(value)) for value in values])
            self.db.commit()

    def event(self, action, repo_id=None, detail=""):
        self.put("events", {"action": action, "repo_id": repo_id, "detail": detail, "at": now()})

    def state(self, full=False):
        data = {k: self.all(k) for k in self.tables}
        by_repo = {}
        for run in data["runs"]:
            by_repo.setdefault(run.get("repo_id"), []).append(run)
        for repo in data["repositories"]:
            repo["runs"] = sorted(by_repo.get(repo["id"], []), key=lambda x: x.get("started_at") or "", reverse=True)
            repo["run_count"] = len(repo["runs"])
            if not full:
                repo["runs"] = repo["runs"]
        data["runs"].sort(key=lambda x: x.get("started_at") or "", reverse=True)
        jobs = data["jobs"]
        data["jobs"] = [j for j in jobs[:-40] if j.get("status") == "failed" and not j.get("dismissed")] + jobs[-40:]
        data["events"] = data["events"][-100:]
        data["settings"] = {"data_directory": str(self.directory), "automatic_checks": False, "total_runs": sum(r.get("phase") == "test" for r in data["runs"])}
        if not full:
            data["runs"] = data["runs"][:200]
        return data
