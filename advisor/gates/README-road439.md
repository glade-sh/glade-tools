# Check equivalence in the corpus gate

The gate runs each scheduled check twice against the same prepared project and binary, uncached: once with the lane's GOMAXPROCS and once with 1. It compares the complete JSON result and exit status. Diagnostic ordering is normalized while duplicate diagnostics and all fields remain significant. The original check output is preserved; serial output, a JSON verdict, and a unified JSON diff are written alongside it. A timeout, invalid or missing output, or a lane configured with only one worker is incomplete. Any mismatch or incomplete comparison makes the runner fail. Shard lanes that do not schedule a check do not produce a separate comparison.

`gate-status.sh` reads `gstat.py` from the laptop and reports match, mismatch, and incomplete counts for planned checks, including work that never started. Lane scripts and gate wrappers propagate failures after cleanup and test diffs. Inspect the counts and receipts and require every scheduled check to match; a run with no planned checks is not evidence of equivalence. Artifacts may contain private project details and stay on the gate host.

To install after review, copy `run2.py`, `run3.py`, `incremental.py`, `check_equivalence.py`, `check_plan.py`, `plan_lanes.py`, the lane wrappers, `gstat.py`, and `gate-install.sh` from this directory into the operational `advisor/gates` directory, then use its `gate-install.sh` with explicitly selected authorized hosts. It distributes the comparison helper, planner, and updated wrappers with both runners. The public overlay is installed from the operational input directory; the private canary overlay remains in place. Do not run a gate merely to install the change.

Run the synthetic checks with `python3 -m unittest advisor.gates.test_check_equivalence` from the repository root.

## Train mode (ROAD-420)

`plan_lanes.py --train` plans a per-train gate. It reduces coverage and proves nothing about repos it does not run.

```
plan_lanes.py --train --only @scope.txt --cfg $W/corpus-config.json --hist LASTLABEL ...   # via val2.sh PLAN_ARGS
```

- `--only` is required: tier-1 repos plus the repos the train changed (names, comma list or `@file`). A name in no set clone stops the plan (exit 2).
- `--cfg` must be `$W/corpus-config.json` and its SHA-256 must equal `$W/corpus-config.json.sha256`, which `gate-install.sh` writes with the public overlay. Otherwise the plan stops and no lane script is written.
- Train labels must have empty, distinct set output directories and `--out` must be `$W/out`; violations fail before planning. Each set's out directory gets `train-scope.json`, which is the plan only (`planned`, `not_planned`, config digest). An empty effective scope (after `--only` and `--exclude`) stops the plan. Repos in `not_planned` write no results and are never counted as clean. In train mode (`--train` in `PLAN_ARGS`) `val2.sh` runs `python3 incremental.py scope OUT/SET-LABEL` after the lanes finish or die and prints `ran=`, `ran:` and `not_run:` lines just before the final `done` line. It writes `train-ran.json` from `results.jsonl`. `ran` needs a parsed check row and every test part with parsed output and no timeout. `not_run` is every other repo: planned but unfinished, excluded or unselected. A planned repo that did not finish fails the gate. Non-train `val2.sh` output is unchanged.
- Lane scripts pass `--unit-timeout-history <hist>` to `run3.py`: `clamp(3 x wall, 300, default)` for clean history, `min(1200, default)` for a history timeout, the default for no, mixed or incomplete history. A timed-out unit is a timeout, never a pass.
- Without `--train` and without that flag, planner and runner output match main. The release gate and `val2.sh` are unchanged.

Run the tests with `python3 -m unittest test_incremental test_check_equivalence` from this directory.

The installer reads the public config and legacy helpers from `GLADE_GATE_INPUTS` (default `~/.local/share/glade-family-cache/advisor/gates`); it preflights all selected hosts before copying and exits nonzero on failure. Supply the intended public overlay there; the private canary overlay is not installed by this script.

`gate-install.sh` needs six private operational inputs that name corpus repos and are never committed here: `corpus-config.json`, `mkhist.py`, `diff2.py`, `order_repos.py`, `accepted.py`, `accepted-ledger.json`. Point `GLADE_GATE_INPUTS` at them. If any is missing the script names all of them, installs nothing and exits 1.
