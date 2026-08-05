#!/usr/bin/env bash
set -euo pipefail

# Materialize one bounded current-base evidence wave. This script never refreshes
# Salesforce or rewrites an existing output directory. Workers should run it
# after producing local and oracle evidence; the reconciler owns promotion.

usage() {
  cat >&2 <<'EOF'
usage: materialize-surface-wave.sh --tools-bin PATH --base-ledger PATH --evidence PATH
  --policy PATH --corpus-usage PATH --snapshot-dir PATH --out-dir PATH
  --dual-rail-manifest PATH
  [--add PATH]... [--remove PATH]... [--tombstone PATH]... [--no-html]
EOF
}

tools_bin=""
base_ledger=""
evidence=""
policy=""
corpus_usage=""
snapshot_dir=""
out_dir=""
dual_rail_manifest=""
additions=()
removals=()
tombstones=()
render_html=true

while (($# > 0)); do
  case "$1" in
    --tools-bin|--base-ledger|--evidence|--policy|--corpus-usage|--snapshot-dir|--out-dir|--dual-rail-manifest)
      flag="$1"
      shift
      (($# > 0)) || { echo "$flag requires a value" >&2; exit 2; }
      case "$flag" in
        --tools-bin) tools_bin="$1" ;;
        --base-ledger) base_ledger="$1" ;;
        --evidence) evidence="$1" ;;
        --policy) policy="$1" ;;
        --corpus-usage) corpus_usage="$1" ;;
        --snapshot-dir) snapshot_dir="$1" ;;
        --out-dir) out_dir="$1" ;;
        --dual-rail-manifest) dual_rail_manifest="$1" ;;
      esac
      shift
      ;;
    --add) shift; (($# > 0)) || { echo "--add requires a value" >&2; exit 2; }; additions+=("$1"); shift ;;
    --remove) shift; (($# > 0)) || { echo "--remove requires a value" >&2; exit 2; }; removals+=("$1"); shift ;;
    --tombstone) shift; (($# > 0)) || { echo "--tombstone requires a value" >&2; exit 2; }; tombstones+=("$1"); shift ;;
    --no-html) render_html=false; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage; exit 2 ;;
  esac
done

for required in tools_bin base_ledger evidence policy corpus_usage snapshot_dir out_dir dual_rail_manifest; do
  if [[ -z "${!required}" ]]; then
    echo "missing required option: --${required//_/-}" >&2
    usage
    exit 2
  fi
done

[[ -x "$tools_bin" ]] || { echo "tools binary is not executable: $tools_bin" >&2; exit 1; }
for input in "$base_ledger" "$evidence" "$policy" "$corpus_usage"; do
  [[ -f "$input" ]] || { echo "input file not found: $input" >&2; exit 1; }
done
[[ -d "$snapshot_dir" ]] || { echo "snapshot directory not found: $snapshot_dir" >&2; exit 1; }
[[ -f "$dual_rail_manifest" ]] || { echo "dual-rail manifest not found: $dual_rail_manifest" >&2; exit 1; }
python3 "$(dirname "$0")/validate-dual-rail-manifest.py" "$dual_rail_manifest" >/dev/null
[[ ! -e "$out_dir" ]] || { echo "refusing to overwrite existing output directory: $out_dir" >&2; exit 1; }
mkdir -p "$out_dir"
cp "$dual_rail_manifest" "$out_dir/DUAL_RAIL_MANIFEST.json"

# The ledger command accepts raw rows for docs and needs empty org/glade inputs
# when a wave is applied to an already merged predecessor ledger.
printf '[]\n' > "$out_dir/EMPTY_ROWS.json"

if ((${#additions[@]} + ${#removals[@]} + ${#tombstones[@]} > 0)); then
  delta_args=(compat surface delta-preflight --base-ledger "$base_ledger" --policy "$policy" --output "$out_dir/DELTA_PREFLIGHT.json")
  for path in "${additions[@]}"; do delta_args+=(--add "$path"); done
  for path in "${removals[@]}"; do delta_args+=(--remove "$path"); done
  for path in "${tombstones[@]}"; do delta_args+=(--tombstone "$path"); done
  "$tools_bin" "${delta_args[@]}" >/dev/null
fi

"$tools_bin" compat surface ledger \
  --docs "$base_ledger" --org "$out_dir/EMPTY_ROWS.json" --glade "$out_dir/EMPTY_ROWS.json" \
  --evidence "$evidence" --output "$out_dir/SURFACE_LEDGER.json" >/dev/null

profile_args=(compat surface support-profile
  --ledger "$out_dir/SURFACE_LEDGER.json" --policy "$policy" --corpus-usage "$corpus_usage"
  --snapshot-dir "$snapshot_dir" --output "$out_dir/apex-support-profile.json")
if [[ "$render_html" == true ]]; then
  profile_args+=(--html-output "$out_dir/apex-support-profile.html")
fi
"$tools_bin" "${profile_args[@]}" >/dev/null

"$tools_bin" compat surface strict-current-base \
  --ledger "$out_dir/SURFACE_LEDGER.json" --output "$out_dir/strict-current-base.json" >/dev/null

checksum_file="$out_dir/SHA256SUMS.txt"
{
  for file in "$out_dir"/*; do
    [[ -f "$file" && "$file" != "$checksum_file" ]] || continue
    if command -v shasum >/dev/null 2>&1; then
      shasum -a 256 "$file"
    else
      sha256sum "$file"
    fi
  done
} | LC_ALL=C sort -k2 > "$checksum_file"

python3 - "$out_dir" <<'PY'
import json
import pathlib
import sys

root = pathlib.Path(sys.argv[1])
profile = json.loads((root / "apex-support-profile.json").read_text())
delta_path = root / "DELTA_PREFLIGHT.json"
delta = json.loads(delta_path.read_text()) if delta_path.exists() else {}
print(json.dumps({
    "outDir": str(root),
    "total": profile.get("total", 0),
    "nonDeferred": len(profile.get("nonDeferredGaps", [])),
    "gapClasses": profile.get("byGapClass", {}),
    "delta": {
        "baseRows": delta.get("baseRows"),
        "resultRows": delta.get("resultRows"),
        "added": len(delta.get("addedIds", [])),
        "changed": len(delta.get("changedIds", [])),
        "removed": len(delta.get("removedIds", [])),
    },
}, separators=(",", ":")))
PY
