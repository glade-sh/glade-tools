# Check equivalence in the corpus gate

The gate runs each scheduled check twice against the same prepared project and binary, uncached: once with the lane's GOMAXPROCS and once with 1. It compares the complete JSON result and exit status. Diagnostic ordering is normalized while duplicate diagnostics and all fields remain significant. The original check output is preserved; serial output, a JSON verdict, and a unified JSON diff are written alongside it. A timeout, invalid or missing output, or a lane configured with only one worker is incomplete. Any mismatch or incomplete comparison makes the runner fail. Shard lanes that do not schedule a check do not produce a separate comparison.

`gate-status.sh` reads `gstat.py` from the laptop and reports match, mismatch, and incomplete counts for planned checks, including work that never started. Lane scripts and gate wrappers propagate failures after cleanup and test diffs. Inspect the counts and receipts and require every scheduled check to match; a run with no planned checks is not evidence of equivalence. Artifacts may contain private project details and stay on the gate host.

To install after review, copy `run2.py`, `run3.py`, `check_equivalence.py`, `check_plan.py`, `plan_lanes.py`, the lane wrappers, `gstat.py`, and `gate-install.sh` from this directory into the operational `advisor/gates` directory, then use its `gate-install.sh` with explicitly selected authorized hosts. It distributes the comparison helper, planner, and updated wrappers with both runners. Existing overlay configuration remains in place. Do not run a gate merely to install the change.

Run the synthetic checks with `python3 -m unittest advisor.gates.test_check_equivalence` from the repository root.
