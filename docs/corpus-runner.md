# Corpus runner (tiers, goldens, nightly)

`glade-tools corpus run` runs `glade check` and `glade test` on pinned corpus
repos and compares each result with a committed golden file. It prints NEW,
GONE and SLOW per repo. The code names no repo. The tier list, overlays and
goldens live in a private config directory that `--config` points at.

## Commands

```bash
glade-tools corpus run --tier B --glade /abs/path/glade --config /abs/path/config [--only a,b] [--json run.json]
glade-tools corpus golden --config DIR --repo NAME --from-run run.json
glade-tools corpus classify --config DIR --status known-gap --reason TEXT --ticket ID [--repo a,b]
```

The first output line is the verdict. Exit codes:

| First line | Exit | Meaning |
| --- | --- | --- |
| `CORPUS-CLEAN` | 0 | no NEW entries and no infrastructure problem |
| `CORPUS-NEW n in k repos` | 1 | n failing tests, timeouts or diagnostics that are not in a golden |
| `CORPUS-INFRA k repos` | 2 | a run could not finish, a pin or overlay moved, or a golden is missing or unclassified |

Tiers are nested: tier B includes A, and C includes B and A. Tier A runs in CI,
B nightly, C at a release candidate.

## Config directory

```text
config/tiers.json          schemaVersion, depSources, repos[] (name, tier, clone, pin, overlay, blocked)
config/goldens/<repo>.json one golden per repo
```

A repo with a `blocked` reason is never run and is reported as INFRA. A golden is
written by `corpus golden` from a `corpus run --json` report. New entries start
`unclassified`; `corpus classify` gives them a status (`known-gap`, `intended`,
`env`, `flaky`), a reason and a ticket. A golden with an unclassified entry is
INFRA. Example golden (synthetic):

```json
{
  "repo": "sample-app",
  "pin": "0123456789abcdef0123456789abcdef01234567",
  "overlay": {"config": {}},
  "glade": {"sourceApi": "63.0"},
  "counts": {"classes": 12, "tests": 40, "passed": 39},
  "failing": [
    {"test": "SampleTest.needsHostedService", "status": "known-gap", "reason": "hosted service call", "ticket": "ISSUE-1", "message": "..."}
  ],
  "wallSeconds": 21.5
}
```

## Diff rules

- A failing test, timeout or diagnostic the golden lacks is NEW.
- A golden entry the run no longer produces is GONE (reported, exit 0). Flaky
  entries are never GONE.
- Timeouts: from none to some is NEW. Otherwise only more than twice the golden
  count is NEW, and then every timeout the golden lacks counts.
- Wall time above 1.5 times the golden prints SLOW (reported, exit 0).
- A moved pin, overlay or source API, or fewer tests than the golden, is INFRA.

## Nightly

`scripts/corpus-nightly/corpus_nightly.py` clones the Glade repo at `--ref`
(default `main`), builds `glade` and `glade-tools`, runs one tier (default B) and
writes `results/<stamp>.txt` (first line only) plus a `.log` and `.json` in the
state directory. It takes a lock, so two runs never overlap. Cron entry:

```cron
0 2 * * * /usr/bin/python3 $HOME/glade-tools/scripts/corpus-nightly/corpus_nightly.py --state $HOME/corpus-nightly --config $HOME/corpus-config --go /path/to/go
```

Test the script with `python3 -m unittest scripts/corpus-nightly/corpus_nightly_test.py`.
