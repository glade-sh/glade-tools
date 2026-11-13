#!/bin/bash
# pubgate2.sh <bin> <label> [repo...]   (Razor ~/advisor-corpus/pubgate, Casper /Volumes/Photos/advisor-pubgate)
# Public 18 (or the listed repos) in LANES (default 2) LPT lanes from the newest *-public history; the one
# class-bound repo (NebulaLogger) is split include/exclude. Output: out/<label>-public/<repo>.test.stdout as before,
# then diff2 vs $BASELINES (default FB59FINAL-public B60C88C5-public) -> out/DIFF2-<label>-public.txt and the log.
# Env: ONLY=a,b (plan lanes over just these repos, testing), MINSPLIT=150, LANES=2 (use 1 on Casper), HIST=<label dir>, PROCS=3, BASELINES="a b", CFG=1 (apply ./corpus-config.json overlays via run3.py).
W=$(cd "$(dirname "$0")" && pwd); cd $W; RUNNER=run2.py; [ -n "${CFG:-}" ] && RUNNER=run3.py; BIN=$1; L=$2; shift 2
LANES=${LANES:-2}; PROCS=${PROCS:-3}; BASELINES=${BASELINES:-FB59FINAL-public B60C88C5-public}
exec > $W/pub2-$L.log 2>&1
if [ -z "${HIST:-}" ]; then
  for d in $(ls -td $W/out/*-public); do
    if [ "$(basename $d)" != "$L-public" ] && [ -f $d/NebulaLogger.test.stdout ]; then HIST=$(basename $d | sed 's/-public$//'); break; fi
  done
fi
echo "start $(date -u +%FT%TZ) $L bin=$BIN lanes=$LANES hist=$HIST"
status=0
if [ $# -gt 0 ]; then   # explicit repos: single lane, no planning
  P=$W/clone-$L; rm -rf $P; mkdir -p $P; for r in "$@"; do cp -R $W/clone-curated/$r $P/; done
  python3 "$W/check_plan.py" "$W/out/$L-public" "$@" || exit 1
  nice -n 10 python3 $W/$RUNNER --bin $BIN --label $L-public --clone $P --out $W/out --procs $PROCS --memlimit 4GiB --no-cache-check --test-timeout 3600 ${CFG:+--corpus-config $W/corpus-config.json} ${HIST:+--history-from $HIST-public} --repos "$@" 2>$W/$L.err
  rc=$?; [ $rc -eq 0 ] || status=1
  rm -rf $P
else
  python3 $W/plan_lanes.py --lanes $LANES --label $L --bin $BIN --hist $HIST --w $W --out $W/out --outdir $W/lanes --runroot $W/runs \
    --set "{L}-public=$W/clone-curated" ${CFG:+--cfg $W/corpus-config.json} --shard3 '' --min-split ${MINSPLIT:-150} --procs $PROCS ${ONLY:+--only $ONLY} || { echo plan failed; exit 1; }
  pids=()
  for k in $(seq 0 $((LANES-1))); do nice -n 10 bash $W/lanes/$L-lane$k.sh > $W/lanes/$L-lane$k.log 2>&1 & pids+=("$!"); done
  for pid in "${pids[@]}"; do wait "$pid" || status=1; done
fi
python3 $W/merge_shards.py --scan $W/out/$L-public
: > $W/out/DIFF2-$L-public.txt
for f in $W/out/$L-public/*.test.stdout; do r=$(basename $f .test.stdout)
  for b in $BASELINES; do [ -f $W/out/$b/$r.test.stdout ] && echo "PUB $r vs$b $(python3 $W/diff2.py $W/out/$b/$r.test.stdout $f 2>&1 | grep -E '^(new|timeout)' | tr '\n' ' ')" >> $W/out/DIFF2-$L-public.txt; done
done
cat $W/out/DIFF2-$L-public.txt
echo "done $(date -u +%FT%TZ)"
exit $status
