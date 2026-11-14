#!/bin/bash
# b3-2.sh <bin> <label>   broad3 = three configured repos (Razor ~/advisor-corpus/pubgate or Casper advisor-pubgate)
# The first configured repo runs as LANES (default 2) class shards, one per lane (history = newest other *-B3 run); the
# other two ride on the first/last lane. Repo directory names come from $W/corpus-config.json key "b3Repos" (list of 3; not in this repo). Output out/<label>-B3/<repo>.test.stdout as before, then diff2 vs
# BASE (default: newest other *-B3 dir) -> out/DIFF2-<label>-B3.txt. Env: LANES=2 (1 on Casper), SRC=<clone dir>, PROCS=3, BASE=<dir name>, CFG=1 (apply ./corpus-config.json overlays via run3.py).
W=$(cd "$(dirname "$0")" && pwd); cd $W; RUNNER=run2.py; [ -n "${CFG:-}" ] && RUNNER=run3.py; BIN=$1; L=$2; LANES=${LANES:-2}; PROCS=${PROCS:-3}
SRC=${SRC:-$([ -d $HOME/advisor-corpus/broad/clone-public-new ] && echo $HOME/advisor-corpus/broad/clone-public-new || echo $W/broad3-src)}
B3R=$(python3 -c 'import json,sys; print(" ".join(json.load(open(sys.argv[1]))["b3Repos"]))' "$W/corpus-config.json") || { echo "b3-2.sh: $W/corpus-config.json is missing or has no \"b3Repos\" key" >&2; exit 2; }
set -- $B3R; [ $# -eq 3 ] || { echo "b3-2.sh: corpus-config.json \"b3Repos\" must list exactly 3 repo directory names" >&2; exit 2; }
exec > $W/b3v2-$L.log 2>&1
K=$1; A=$2; F=$3
if [ -z "${BASE:-}" ]; then
  for d in $(ls -td $W/out/*-B3); do
    if [ "$(basename $d)" != "$L-B3" ] && [ -f $d/$K.test.stdout ]; then BASE=$(basename $d); break; fi
  done
fi
echo "start $(date -u +%FT%TZ) $L bin=$BIN lanes=$LANES base=$BASE src=$SRC"
mkdir -p "$W/out/$L-B3"
python3 "$W/check_plan.py" "$W/out/$L-B3" "$K" "$A" "$F" || exit 1
status=0; pids=()
for k in $(seq 0 $((LANES-1))); do
  ( P=$W/b3run-$L-$k; rm -rf $P; mkdir -p $P; cp -R $SRC/$K $P/
    repos="$K"; [ $k -eq 0 ] && { cp -R $SRC/$A $P/; repos="$K $A"; }; [ $k -eq $((LANES-1)) ] && { cp -R $SRC/$F $P/; repos="$repos $F"; }
    [ $LANES -gt 1 ] && sh="--shard $K=$k/$LANES" || sh=""
    nice -n 10 python3 $W/$RUNNER ${CFG:+--corpus-config $W/corpus-config.json} --bin $BIN --label $L-B3 --clone $P --out $W/out --procs $PROCS --memlimit 4GiB --no-cache-check --test-timeout 3600 \
      ${BASE:+--history-from $BASE} $sh --repos $repos 2>$W/$L-B3-$k.err
    rc=$?; echo "lane$k rc=$rc"; rm -rf $P; exit $rc ) &
  pids+=("$!")
done
for pid in "${pids[@]}"; do wait "$pid" || status=1; done
python3 $W/merge_shards.py --scan $W/out/$L-B3
: > $W/out/DIFF2-$L-B3.txt
for f in $W/out/$L-B3/*.test.stdout; do r=$(basename $f .test.stdout)
  [ -n "$BASE" ] && [ -f $W/out/$BASE/$r.test.stdout ] && echo "B3 $r vs$BASE $(python3 $W/diff2.py $W/out/$BASE/$r.test.stdout $f 2>&1 | grep -E '^(new|timeout)' | tr '\n' ' ')" >> $W/out/DIFF2-$L-B3.txt
done
cat $W/out/DIFF2-$L-B3.txt; echo "done $(date -u +%FT%TZ)"
exit $status
