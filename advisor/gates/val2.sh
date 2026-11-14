#!/bin/bash
# val2.sh <bin> <label> [--lanes 3] [--hist B70] [--exclude a,b]   (BMO; run from anywhere; d12 #3)
# 118 public repos (BUCKET2 30 + BROAD 88) in N LPT-balanced lanes, configured repos (corpus-config.json "shard3") as 3 class shards, one-class-bound
# repos split into include/exclude parts. Output layout unchanged:
#   ~/advisor-broad/out/{BUCKET2,BROAD}-<label>/<repo>.test.stdout; then diff2 vs the --hist label -> out/DIFF2-<label>.txt
# Env: W=<workdir> (default ~/advisor-broad), PLAN_ARGS=<extra plan_lanes.py args>, RUNROOT, CFG=1 (apply $W/corpus-config.json overlays via run3.py).
# Binary must already exist (gate-dispatch.sh builds it); never reuse a binary name an active gate uses.
W=${W:-~/advisor-broad}; cd $W; BIN=$1; L=$2; shift 2
LANES=3; HIST=B70; EXCL=""
while [ $# -gt 0 ]; do case "$1" in --lanes) LANES=$2; shift 2;; --hist) HIST=$2; shift 2;; --exclude) EXCL=$2; shift 2;; *) echo "bad arg $1"; exit 2;; esac; done
exec > $W/val2-$L.log 2>&1
echo "start $(date -u +%FT%TZ) bin=$BIN label=$L lanes=$LANES hist=$HIST"
# 10-24: stale project copies from killed or foreign runs make dependency resolution ambiguous; one BMO gate runs at a time, so clear them
for d in ${RUNROOT:-$HOME/advisor-val-run}/lane*/run-*; do [ -d "$d" ] && echo "clearing stale $d" && rm -rf "$d"; done
python3 $W/plan_lanes.py --lanes $LANES --label $L --bin $BIN --hist $HIST ${EXCL:+--exclude $EXCL} --w $W --out $W/out --outdir $W/lanes --runroot ${RUNROOT:-$HOME/advisor-val-run} ${CFG:+--cfg $W/corpus-config.json} $PLAN_ARGS || { echo plan failed; echo done; exit 1; }
status=0; pids=()
# Train mode (the planner's own word-split view of PLAN_ARGS): report actual coverage on every exit path.
TRAIN=; set -f; for _a in $PLAN_ARGS; do [ "$_a" = --train ] && TRAIN=1; done; set +f
if [ -n "$TRAIN" ]; then
  train_report() {
    [ -n "$REPORTED" ] && return; REPORTED=1
    for s in BUCKET2 BROAD; do
      [ -f $W/out/$s-$L/train-scope.json ] || continue
      echo "train scope $s-$L:"
      python3 $W/incremental.py scope $W/out/$s-$L || status=1
    done
  }
  kill_tree() { local c; for c in $(pgrep -P $1 2>/dev/null); do kill_tree $c; done; kill $1 2>/dev/null; }
  on_signal() {  # interrupted: stop the lanes, then report what finished. An uncatchable kill leaves results.jsonl;
    # run `incremental.py scope OUT/SET-LABEL` by hand to recover the report.
    trap '' TERM INT
    echo "interrupted by $1; stopping lanes"
    for pid in "${pids[@]}"; do kill_tree $pid; done
    wait 2>/dev/null
    status=1; train_report
    echo "done $(date -u +%FT%TZ)"
    exit $2
  }
  trap 'on_signal TERM 143' TERM; trap 'on_signal INT 130' INT; trap 'train_report' EXIT
fi
for k in $(seq 0 $((LANES-1))); do nice -n 5 bash $W/lanes/$L-lane$k.sh > $W/lanes/$L-lane$k.log 2>&1 & pids+=("$!"); done
for pid in "${pids[@]}"; do wait "$pid" || status=1; done
for s in BUCKET2 BROAD; do python3 $W/merge_shards.py --scan $W/out/$s-$L; done
: > $W/out/DIFF2-$L.txt
for s in BUCKET2 BROAD; do
  for f in $W/out/$s-$L/*.test.stdout; do r=$(basename $f .test.stdout); b=$W/out/$s-$HIST/$r.test.stdout
    [ -f $b ] && echo "$s $r $(python3 $W/diff2.py $b $f 2>&1 | grep -E '^(new|timeout)' | tr '\n' ' ')" >> $W/out/DIFF2-$L.txt; done
done
echo "new total: $(awk '{for(i=1;i<NF;i++) if($i=="new") n+=$(i+1)} END{print n+0}' $W/out/DIFF2-$L.txt)"
[ -n "$TRAIN" ] && train_report
echo "done $(date -u +%FT%TZ)"
exit $status
