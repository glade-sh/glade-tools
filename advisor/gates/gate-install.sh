#!/bin/bash
# gate-install.sh [casper|razor|bmo ...]: copy the v2 gate scripts to the hosts. Each file is copied to a temp name and
# mv'd into place (atomic: a running bash/python keeps its old inode, so a live gate is never corrupted).
# casper canary (private gate) gets run3.py/corpus_cfg.py too; its overlay corpus-config-private.json is copied by hand from private/ (never via this script).
# run3.py, corpus_cfg.py and corpus-config.json are new files (CFG=1 overlays); run.py etc. are never touched.
G=$(cd "$(dirname "$0")" && pwd)
inst() {  # inst <host> <dir> <files...>
  local h=$1 d=$2; shift 2; local f
  for f in "$@"; do scp -q $G/$f $h:$d/.$f.new && ssh $h "mv $d/.$f.new $d/$f" || return 1; done
}
for h in ${@:-casper razor bmo}; do
  case $h in
    casper) inst casper.local /Volumes/Photos/advisor-pubgate run2.py run3.py check_equivalence.py corpus_cfg.py corpus-config.json merge_shards.py mkhist.py diff2.py plan_lanes.py pubgate2.sh b3-2.sh \
              && inst casper.local /Volumes/Photos/advisor-canary-20261012 run2.py run3.py check_equivalence.py corpus_cfg.py merge_shards.py mkhist.py diff2.py priv-gate2.sh order_repos.py;;
    razor)  inst razor.local advisor-corpus/pubgate run2.py run3.py check_equivalence.py corpus_cfg.py corpus-config.json merge_shards.py mkhist.py diff2.py plan_lanes.py pubgate2.sh b3-2.sh;;
    bmo)    inst bmo.local advisor-broad run2.py run3.py check_equivalence.py corpus_cfg.py corpus-config.json merge_shards.py mkhist.py diff2.py plan_lanes.py val2.sh accepted.py accepted-ledger.json;;
  esac && echo "installed $h" || echo "FAILED $h"
done
