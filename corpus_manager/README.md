# Glade Corpus

A local dashboard for repository inventory, upstream changes, candidate discovery,
and results across Glade revisions. It uses Python 3.10+ and the standard library;
there is no dependency installation or frontend build.

```sh
cd ~/Dev/glade-tools
python3 -m corpus_manager --open
```

Open <http://127.0.0.1:8766>. The server binds to loopback only. A second launch
opens the running server. Use a different `--port` and `--data-dir` for a separate instance.
Ctrl-C stops the server. The launcher `launch.command` keeps it in a Terminal
window and opens the browser. Keep that Terminal window open while using it.

## Daily use

- **Repositories:** refresh inventory, filter by host or upstream state, add a
  GitHub URL, edit tier/tags/notes, and archive or restore a repository.
- **Repository detail:** choose which checkout to track, check upstream, view the
  compared revision and GitHub comparison, or prepare an updated checkout.
- **Discover:** save GitHub repository searches, run them, and add or dismiss
  candidates. Searches return the 50 most recently updated matches. The result
  count and any truncated/incomplete search are visible. Dismissals survive refresh.
- **Run history:** search by repository, Glade commit/version, or outcome; inspect
  the recorded evidence; import an aggregate JSON/JSONL file; or record a result
  manually. Imported history never implies that the dashboard ran a test.
- **Settings:** configure SSH host aliases, inventory roots and managed checkout
  directories; inspect jobs; export catalog metadata as JSON.

All network checks are on demand. The page refreshes progress while jobs run;
it does not run recurring searches or tests in the background. GitHub checks use
`GH_TOKEN`, `GITHUB_TOKEN`, or the existing `gh auth token` session, in that order.
Tokens remain in server memory and are never sent to the browser or stored in the
catalog. Without authentication, public API requests use GitHub's lower rate limit.
See [GitHub search](https://docs.github.com/en/rest/search) and
[rate limits](https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api).

## Storage and checkout rules

Private metadata lives in `~/.local/share/glade-corpus-manager/catalog.sqlite3`,
outside both product and tools Git history. The directory is private to the user.
SQLite supplies transactional writes. Export produces portable metadata; it does
not include repository source, credentials, test stdout, or build artifacts.

Inventory reads metadata over configured SSH aliases with batch authentication.
It recognizes Git checkouts and Salesforce project folders without Git metadata.
Non-Git source revisions and failed dirty-state checks remain unknown. Identical
GitHub upstreams share a catalog record with separate checkout locations. Sources
without an upstream retain host/path identity. An inventory refresh preserves
notes, tiers, archived status and history. Unavailable roots are reported; existing
locations are retained with their last-observed timestamp.

Upstream comparison uses the explicitly tracked checkout, shown in repository
detail. It compares commit ancestry, not push timestamps. Select another checkout
to compare a different host or revision. A prepared snapshot becomes the tracked
checkout; other checkouts are unchanged. An upstream check is not a Glade test.

**Archive removes a repository from the active catalog. It never deletes files.**
Prepare checkout creates a new unique, detached, shallow checkout at the checked
upstream SHA on a remote corpus host. It never pulls into, resets, cleans, or
deletes an existing checkout. It does not change runner source lists or run tests.
The host must retain 50 GiB free; Git is monitored with an additional margin and
stopped if space approaches the reserve. A failed partial checkout is preserved
for inspection. LFS smudging, submodule initialization, and project hooks are not
run. Projects requiring those assets need separate preparation on the host.
No payload copies onto the dashboard Mac are supported, and resolved paths under
`BACKUP` are refused.

## Importing existing history

Adapters accept gate `results.jsonl`, legacy baseline `projects` arrays, and
generic `runs` or `results` arrays. Each file is bounded to 40 MiB and 100,000 rows.
Malformed trailing JSONL records are reported rather than fabricated. Partial
shards remain partial; repeated imports upsert the same observations.

Historical source SHA, Glade SHA/version, and source API are retained only when
explicitly recorded. In particular, today's repository HEAD is never assigned to
an old run. A dispatch record can supply its Glade SHA and timestamp. Ambiguous
repository identities remain unmatched in global history. Manual results are
labelled as manual evidence.

Optional private `config.json` bootstraps `hosts` on the first start:

```json
{"hosts":[{"id":"worker","label":"Corpus host","ssh":"worker.example",
  "roots":["~/corpus"],"managed_root":"~/corpus-managed"}]}
```

Optional private `history-sources.json` enables **Import saved scan history**:

```json
{"sources":[{"host":"worker","path":"~/results/run/results.jsonl",
  "glade_sha":"recorded-full-commit","started_at":"2026-11-01T00:00:00Z"}]}
```

The importer reads only aggregate result files over SSH. It never reads or copies
test source or stdout payloads. Host IDs must match the configured host records.
These private configuration files must stay outside the source repository.
If `history-sources.json` includes a `dispatch_dir`, each import also discovers
new dispatch records there. Only enabled routes with explicitly recorded Glade
commits and timestamps are added. Existing and manually configured routes remain.
Dispatch routes come from a private file outside the repository, found at
`$GLADE_CORPUS_MANAGER_ROUTES` or `~/.config/glade-corpus-manager/routes.json`.
It maps each dispatch output key to a gate flag and a configured host ID:

```json
{"routes":{"output-a":{"gate":"private","host":"host-a"},
  "output-b":{"gate":"public","host":"host-b"}}}
```

An output path containing one `{x,y}` group expands to one source per choice.
The same file may hold `lanes` (`slots_dir` plus per-SSH-alias `flag` and `prefix`)
and optional `busy_processes` (command-line substrings) so the dashboard can check shared test-lane markers before testing.
Without this file no dispatch routes are generated and a warning is shown.

## Groups and update testing

In Settings, choose the tiers JSON file, the default host, the Glade binary,
the full source commit used to build it, and an executable that implements
`glade-tools corpus run` from the corpus v2 runner. Binary paths must be valid
on the selected host. The commit is recorded as supplied; the dashboard cannot
verify that it matches the binary. The runner executes against a temporary
configuration and a scratch copy, applies the configured overlay, and never
changes the tier pin or the pulled checkout. A missing pin baseline on the same
Glade commit is reported explicitly.

Pulling makes a separate checkout under the host's managed root. Testing is
explicit; it checks shared-lane markers and host load before starting. Active
pulls and tests can be cancelled. Only checkouts created with a dashboard
ownership marker can be deleted through the dashboard. Accepting a new pin
requires rebuilding its golden in a reviewed change.

Tier writes re-read the file and reject a changed revision immediately before
atomic replacement. The check is not an interprocess compare-and-swap: editors
that do not cooperate with a lock can still write in the interval before the
replacement. Coordinate concurrent edits to that file.

## Validation

```sh
python3 -m unittest discover -s corpus_manager/tests -v
node --check corpus_manager/static/app.js
```

The suite uses synthetic repositories and mocked GitHub/SSH operations. It covers
identity and history preservation, archive/dismissal persistence, incomplete
evidence, pinned snapshots, disk refusal, and loopback/CSRF checks. Actual corpus
checkouts are never mutated by the tests.
