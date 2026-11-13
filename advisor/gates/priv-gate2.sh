#!/bin/bash
# priv-gate2.sh <bin> <label>   (Casper only: /Volumes/Photos/advisor-canary-20261012; private repos never leave the host)
# Same flow as priv-gate.sh (pauses tgate-qp workers, APFS-clone copy, out/<label>-priv/<repo>.test.stdout) with:
#   --procs 4 (PROCS env), run2.py 1 s polling + check/test overlap, longest repo first, duration history from the
#   newest other *-priv run (HIST env to override), diff2 vs BASE (default = that history run) -> out/DIFF2-<label>-priv.txt.
# Env: CFG=1 (apply the PRIVATE overlay $W/corpus-config-private.json, or CFGFILE=<path>, via run2.py --cfg -> run3.py; off by default; the file is private and lives only here)  W=<workdir> NO_PAUSE=1 (skip the tgate-qp STOP/CONT; testing) PROCS=4  HIST=<label>-priv dir name  BASE=<dir name>  SHARD="<repo>=I,J/N" (extra run2 --shard, for splitting across lanes/hosts you approve)
W=${W:-/Volumes/Photos/advisor-canary-20261012}; cd $W; BIN=$1; L=$2; PROCS=${PROCS:-4}; exec > $W/full2-$L.log 2>&1
CFGARG=""; if [ -n "${CFG:-}" ]; then CFGFILE=${CFGFILE:-$W/corpus-config-private.json}; [ -f "$CFGFILE" ] && [ -f $W/run3.py ] && [ -f $W/corpus_cfg.py ] || { echo "CFG=1 but overlay or run3.py/corpus_cfg.py missing"; exit 2; }; CFGARG="--cfg $CFGFILE"; echo "cfg overlay $CFGFILE"; fi
echo "start $(date -u +%FT%TZ) $L procs=$PROCS"
if [ -z "${NO_PAUSE:-}" ]; then pkill -STOP -f tgate-qp.sh; while pgrep -f 'tgate.py' >/dev/null; do sleep 15; done; fi
if [ -z "${HIST:-}" ]; then
  for d in $(ls -td $W/out/*-priv); do
    if [ "$(basename $d)" != "$L-priv" ] && ls $d/*.test.stdout >/dev/null 2>&1; then HIST=$(basename $d); break; fi
  done
fi
BASE=${BASE:-$HIST}; echo "hist=$HIST base=$BASE"
P=$W/clone-$L-priv; rm -rf $P; /bin/cp -c -R $W/clone $P
# longest-history repo first (test.stdout size as the proxy when no results.jsonl)
REPOS=$(python3 $W/order_repos.py $W/out/$HIST $P | tr " " "\n" | grep -xE "src-nmb-namz-prog-develop|sf-cred-pkg-develop|nams-workspace|src-nmb-nu-develop" | tr "\n" " ")
python3 "$W/check_plan.py" "$W/out/$L-priv" ${SHARD:+--shard "$SHARD"} -- $REPOS || { [ -z "${NO_PAUSE:-}" ] && pkill -CONT -f tgate-qp.sh; exit 1; }
nice -n 10 python3 $W/run2.py --bin $BIN --label $L-priv --clone $P --out $W/out --procs $PROCS --memlimit 4GiB --no-cache-check --test-timeout 3600 \
  ${HIST:+--history-from $HIST} $CFGARG ${SHARD:+--shard $SHARD} --repos $REPOS 2>$W/$L-priv.err
rc=$?; echo "priv rc=$rc $(date -u +%FT%TZ)"; rm -rf $P
python3 $W/merge_shards.py --scan $W/out/$L-priv
: > $W/out/DIFF2-$L-priv.txt
for f in $W/out/$L-priv/*.test.stdout; do r=$(basename $f .test.stdout)
  [ -f $W/out/$BASE/$r.test.stdout ] && echo "PRIV $r vs$BASE $(python3 $W/diff2.py $W/out/$BASE/$r.test.stdout $f 2>&1 | grep -E '^(new|timeout)' | tr '\n' ' ')" >> $W/out/DIFF2-$L-priv.txt
done
[ -z "${NO_PAUSE:-}" ] && pkill -CONT -f tgate-qp.sh; echo "done $(date -u +%FT%TZ)"
exit $rc
