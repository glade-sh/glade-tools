#!/bin/bash
# gate-install.sh [casper|razor|bmo ...]: atomically install gate files on the selected hosts.
# GLADE_GATE_INPUTS names the operational public inputs directory. Its public config and legacy
# helpers are maintained outside this checkout; the private canary overlay is never copied here.
G=$(cd "$(dirname "$0")" && pwd)
INPUTS=${GLADE_GATE_INPUTS:-"$HOME/.local/share/glade-family-cache/advisor/gates"}
source_file() {
  case $1 in
    corpus-config.json|mkhist.py|diff2.py|order_repos.py|accepted.py|accepted-ledger.json) printf '%s/%s\n' "$INPUTS" "$1";;
    *) printf '%s/%s\n' "$G" "$1";;
  esac
}
inst() {  # inst <host> <dir> <files...>
  local h=$1 d=$2 f src; shift 2
  for f in "$@"; do
    src=$(source_file "$f")
    scp -q "$src" "$h:$d/.$f.new" && ssh "$h" "mv $d/.$f.new $d/$f" || return 1
  done
}
pin_cfg() {  # pin the same local public overlay that was copied
  local h=$1 d=$2 sha
  sha=$(python3 -c 'import hashlib,sys;print(hashlib.sha256(open(sys.argv[1],"rb").read()).hexdigest())' "$(source_file corpus-config.json)") || return 1
  ssh "$h" "printf '%s\n' $sha > $d/.corpus-config.json.sha256.new && mv $d/.corpus-config.json.sha256.new $d/corpus-config.json.sha256"
}
# Preflight every input needed by selected hosts before changing any remote file.
[ "$#" -gt 0 ] || set -- casper razor bmo
files='run2.py run3.py incremental.py check_equivalence.py check_plan.py corpus_cfg.py corpus-config.json merge_shards.py mkhist.py diff2.py plan_lanes.py'
for h in "$@"; do
  case $h in
    casper) files="$files pubgate2.sh b3-2.sh priv-gate2.sh order_repos.py";;
    razor) files="$files pubgate2.sh b3-2.sh";;
    bmo) files="$files val2.sh accepted.py accepted-ledger.json";;
    *) echo "unknown host: $h" >&2; exit 1;;
  esac
done
missing=''
for f in $files; do
  src=$(source_file "$f")
  [ -f "$src" ] && [ -r "$src" ] || missing="$missing $f"
done
if [ -n "$missing" ]; then
  # These are private operational inputs (they name corpus repos), so they are never in a clean checkout.
  echo "gate-install: missing operational inputs in $INPUTS (set GLADE_GATE_INPUTS):$missing" >&2
  echo "gate-install: nothing was installed" >&2
  exit 1
fi
status=0
for h in "$@"; do
  case $h in
    casper) inst casper.local /Volumes/Photos/advisor-pubgate run2.py run3.py incremental.py check_equivalence.py check_plan.py corpus_cfg.py corpus-config.json merge_shards.py mkhist.py diff2.py plan_lanes.py pubgate2.sh b3-2.sh \
              && pin_cfg casper.local /Volumes/Photos/advisor-pubgate \
              && inst casper.local /Volumes/Photos/advisor-canary-20261012 run2.py run3.py incremental.py check_equivalence.py check_plan.py corpus_cfg.py merge_shards.py mkhist.py diff2.py priv-gate2.sh order_repos.py;;
    razor)  inst razor.local advisor-corpus/pubgate run2.py run3.py incremental.py check_equivalence.py check_plan.py corpus_cfg.py corpus-config.json merge_shards.py mkhist.py diff2.py plan_lanes.py pubgate2.sh b3-2.sh \
              && pin_cfg razor.local advisor-corpus/pubgate;;
    bmo)    inst bmo.local advisor-broad run2.py run3.py incremental.py check_equivalence.py check_plan.py corpus_cfg.py corpus-config.json merge_shards.py mkhist.py diff2.py plan_lanes.py val2.sh accepted.py accepted-ledger.json \
              && pin_cfg bmo.local advisor-broad;;
  esac && echo "installed $h" || { echo "FAILED $h" >&2; status=1; }
done
exit "$status"
