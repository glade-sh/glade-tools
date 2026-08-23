#!/usr/bin/env bash
set -euo pipefail
set +x
umask 077

temporary_paths=()
store_transaction_armed=false
store_transaction_dir=''
store_transaction_store=''
store_transaction_head=''
store_transaction_index=''
store_transaction_paths=()

cleanup_temporary() {
  local path
  if ((${#temporary_paths[@]})); then
    for path in "${temporary_paths[@]}"; do
      find "$path" -depth -delete 2>/dev/null || true
    done
  fi
}

rollback_store_transaction() {
  local path backup restore
  [[ "$store_transaction_armed" == true ]] || return 0
  set +e
  if [[ "$(git -C "$store_transaction_store" rev-parse HEAD 2>/dev/null)" != "$store_transaction_head" ]]; then
    git -C "$store_transaction_store" reset --soft "$store_transaction_head" >/dev/null 2>&1
  fi
  restore="$store_transaction_index.restore.$$"
  cp -p "$store_transaction_dir/index" "$restore" >/dev/null 2>&1 &&
    mv "$restore" "$store_transaction_index" >/dev/null 2>&1
  for path in "${store_transaction_paths[@]}"; do
    backup="$store_transaction_dir/worktree/$path"
    if [[ -e "$backup" || -L "$backup" ]]; then
      mkdir -p "$(dirname "$store_transaction_store/$path")"
      restore="$store_transaction_store/$path.restore.$$"
      cp -p "$backup" "$restore" >/dev/null 2>&1 &&
        mv "$restore" "$store_transaction_store/$path" >/dev/null 2>&1
    else
      find "$store_transaction_store/$path" -maxdepth 0 -type f -delete 2>/dev/null
    fi
  done
  store_transaction_armed=false
}

finish() {
  local status=$?
  trap - EXIT HUP INT TERM
  set +e
  rollback_store_transaction
  cleanup_temporary
  exit "$status"
}
trap finish EXIT
trap 'exit 1' HUP INT TERM

die() {
  echo "$1" >&2
  exit 1
}

require_absolute() {
  [[ "$2" == /* ]] || die "$1 must be an absolute path"
}

require_alias() {
  [[ "$1" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ ]] || die 'invalid alias'
}

require_identity() {
  [[ -f "$1" ]] || die 'missing identity'
  python3 -c 'import os, stat, sys
raise SystemExit(0 if stat.S_IMODE(os.stat(sys.argv[1]).st_mode) == 0o600 else 1)' "$1" ||
    die 'identity must have mode 0600'
}

require_operator() {
  [[ "$(git -C "$1" config --get glade.authRole 2>/dev/null || true)" == operator ]] ||
    die 'operator role required'
}

pull_store() {
  git -C "$1" pull --ff-only >/dev/null 2>&1 || die 'auth store update failed'
}

read_recipients() {
  local store="$1" recipient
  recipients_file="$store/recipients.txt"
  [[ -s "$recipients_file" ]] || return 1
  recipients=()
  while IFS= read -r recipient; do
    [[ -z "$recipient" || "$recipient" == \#* ]] || recipients+=("$recipient")
  done <"$recipients_file"
  ((${#recipients[@]} > 0)) || return 1
  age_args=()
  for recipient in "${recipients[@]}"; do
    age_args+=(-r "$recipient")
  done
}

file_hash() {
  python3 -c 'import hashlib, sys
with open(sys.argv[1], "rb") as source:
    print(hashlib.sha256(source.read()).hexdigest())' "$1"
}

store_hash() {
  python3 - "$1" <<'PY'
import hashlib
import pathlib
import sys

root = pathlib.Path(sys.argv[1])
paths = [root / "recipients.txt", *sorted((root / "devhubs").glob("*.sfdx-auth-url.age"))]
digest = hashlib.sha256()
for path in paths:
    relative = path.relative_to(root).as_posix().encode()
    digest.update(relative)
    digest.update(b"\0")
    digest.update(hashlib.sha256(path.read_bytes()).digest())
print(digest.hexdigest())
PY
}

write_marker() {
  local path="$1" value="$2" temporary="$1.tmp.$$"
  mkdir -p "$(dirname "$path")"
  printf '%s\n' "$value" >"$temporary"
  mv "$temporary" "$path"
}

pause_store() {
  write_marker "$1/pause" store-integrity
}

integrity_die() {
  pause_store "$1"
  die 'auth store integrity failed'
}

quarantine_alias() {
  local state="$1" alias_name="$2" reason="$3"
  write_marker "$state/aliases/$alias_name.health" failed
  write_marker "$state/aliases/$alias_name.quarantine" "$reason"
}

mark_alias_healthy() {
  local state="$1" alias_name="$2" hash="$3"
  write_marker "$state/aliases/$alias_name.hash" "$hash"
  write_marker "$state/aliases/$alias_name.health" healthy
  find "$state/aliases/$alias_name.quarantine" -maxdepth 0 -type f -delete 2>/dev/null || true
}

load_tracked_ciphers() {
  local store="$1" listing metadata mode type object relative recipients_seen=false
  listing="$(git -C "$store" ls-tree -r HEAD 2>/dev/null)" || return 1
  ciphers=()
  while IFS=$'\t' read -r metadata relative; do
    [[ -n "$metadata" ]] || continue
    read -r mode type object <<<"$metadata"
    [[ "$type" == blob && ("$mode" == 100644 || "$mode" == 100755) ]] || return 1
    if [[ "$relative" == recipients.txt ]]; then
      recipients_seen=true
    elif [[ "$relative" =~ ^devhubs/[A-Za-z0-9][A-Za-z0-9._-]*\.sfdx-auth-url\.age$ ]]; then
      [[ -f "$store/$relative" && ! -L "$store/$relative" ]] || return 1
      ciphers+=("$store/$relative")
    else
      return 1
    fi
  done <<<"$listing"
  [[ "$recipients_seen" == true && -f "$store/recipients.txt" && ! -L "$store/recipients.txt" ]]
}

pull_worker_store() {
  local store="$1" state="$2" upstream
  mkdir -p "$state/aliases"
  git -C "$store" diff --quiet HEAD -- recipients.txt devhubs || integrity_die "$state"
  [[ -z "$(git -C "$store" ls-files --others --exclude-standard -- recipients.txt devhubs)" ]] ||
    integrity_die "$state"
  load_tracked_ciphers "$store" || integrity_die "$state"
  upstream="$(git -C "$store" rev-parse '@{u}' 2>/dev/null)" || integrity_die "$state"
  git -C "$store" merge-base --is-ancestor HEAD "$upstream" || integrity_die "$state"
  if ! git -C "$store" pull --ff-only >/dev/null 2>&1; then
    upstream="$(git -C "$store" rev-parse '@{u}' 2>/dev/null)" || integrity_die "$state"
    git -C "$store" merge-base --is-ancestor HEAD "$upstream" || integrity_die "$state"
    die 'auth store unavailable'
  fi
  git -C "$store" diff --quiet HEAD -- recipients.txt devhubs || integrity_die "$state"
  [[ -z "$(git -C "$store" ls-files --others --exclude-standard -- recipients.txt devhubs)" ]] ||
    integrity_die "$state"
  load_tracked_ciphers "$store" || integrity_die "$state"
}

hash_decrypted_credential() {
  local store="$1" identity="$2" cipher="$3" temporary_dir digest_file
  temporary_dir="$(mktemp -d "$store/devhubs/.hash.XXXXXX")"
  temporary_paths+=("$temporary_dir")
  digest_file="$temporary_dir/digest"
  set +e
  age -d -i "$identity" "$cipher" 2>/dev/null |
    python3 -c 'import hashlib, sys
print(hashlib.sha256(sys.stdin.buffer.read()).hexdigest())' >"$digest_file" 2>/dev/null
  pipeline_status=("${PIPESTATUS[@]}")
  set -e
  if ((pipeline_status[0] != 0 || pipeline_status[1] != 0)); then
    die 'unable to decrypt encrypted alias'
  fi
  decrypted_hash="$(cat "$digest_file")"
}

encrypt_source_alias() {
  local store="$1" alias_name="$2" source_alias="$3" sf_bin="$4"
  local prior_hash="${5:-}" cipher="$store/devhubs/$alias_name.sfdx-auth-url.age"
  local auth_url auth_hash temporary_dir temporary_cipher
  read_recipients "$store" || die 'missing recipients'
  auth_url="$("$sf_bin" org auth show-sfdx-auth-url --target-org "$source_alias" \
    --json 2>/dev/null | python3 -c 'import json, sys
data = json.load(sys.stdin)
url = data.get("result", {}).get("sfdxAuthUrl")
if not isinstance(url, str):
    raise SystemExit(1)
print(url, end="")' 2>/dev/null)" || die 'unable to read source Dev Hub'
  [[ "$auth_url" == force://* ]] || die 'invalid source Dev Hub auth URL'
  if [[ -n "$prior_hash" ]]; then
    auth_hash="$(printf '%s\n' "$auth_url" | python3 -c 'import hashlib, sys
print(hashlib.sha256(sys.stdin.buffer.read()).hexdigest())')"
    if [[ "$auth_hash" == "$prior_hash" ]]; then
      unset auth_url
      die 'rotated credential is unchanged'
    fi
  fi

  mkdir -p "$store/devhubs"
  temporary_dir="$(mktemp -d "$store/devhubs/.auth.XXXXXX")"
  temporary_paths+=("$temporary_dir")
  temporary_cipher="$temporary_dir/$(basename "$cipher")"
  if ! printf '%s\n' "$auth_url" | age "${age_args[@]}" -o "$temporary_cipher" \
    >/dev/null 2>&1; then
    unset auth_url
    die "Dev Hub encryption failed: $alias_name"
  fi
  unset auth_url
  mv "$temporary_cipher" "$cipher"
}

begin_store_transaction() {
  local store="$1" path backup index_source
  shift
  [[ "$store_transaction_armed" == false ]] || die 'auth store transaction already active'
  store_transaction_dir="$(mktemp -d "${TMPDIR:-/tmp}/glade-auth-store.XXXXXX")" ||
    die 'auth store snapshot failed'
  temporary_paths+=("$store_transaction_dir")
  store_transaction_store="$store"
  store_transaction_head="$(git -C "$store" rev-parse HEAD 2>/dev/null)" ||
    die 'auth store snapshot failed'
  store_transaction_index="$(git -C "$store" rev-parse --absolute-git-dir 2>/dev/null)/index"
  index_source="$store_transaction_index"
  cp -p "$index_source" "$store_transaction_dir/index" >/dev/null 2>&1 ||
    die 'auth store snapshot failed'
  store_transaction_paths=("$@")
  for path in "$@"; do
    if [[ -e "$store/$path" || -L "$store/$path" ]]; then
      backup="$store_transaction_dir/worktree/$path"
      mkdir -p "$(dirname "$backup")"
      cp -p "$store/$path" "$backup" >/dev/null 2>&1 || die 'auth store snapshot failed'
    fi
  done
  store_transaction_armed=true
}

disarm_store_transaction() {
  store_transaction_armed=false
}

commit_store() {
  local store="$1" message="$2"
  shift 2
  [[ "$store_transaction_armed" == true ]] || die 'auth store transaction missing'
  git -C "$store" add -- "$@" >/dev/null 2>&1 || die 'auth store add failed'
  git -C "$store" commit --only -m "$message" -- "$@" >/dev/null 2>&1 ||
    die 'auth store commit failed'
  git -C "$store" push >/dev/null 2>&1 || die 'auth store push failed'
  disarm_store_transaction
}

parse_store_write() {
  store=''
  alias_name=''
  source_alias=''
  identity=''
  sf_bin=''
  while (($#)); do
    (($# >= 2)) || die 'missing argument value'
    case "$1" in
      --store) store="$2" ;;
      --alias) alias_name="$2" ;;
      --source-alias) source_alias="$2" ;;
      --identity) identity="$2" ;;
      --sf-bin) sf_bin="$2" ;;
      *) die 'unexpected argument' ;;
    esac
    shift 2
  done
  require_absolute store "$store"
  require_absolute sf-bin "$sf_bin"
  require_alias "$alias_name"
  [[ -n "$source_alias" ]] || die 'missing source alias'
  [[ -x "$sf_bin" ]] || die 'Salesforce CLI is not executable'
  command -v age >/dev/null || die 'age is required'
  require_operator "$store"
  pull_store "$store"
}

login_dev_hub() {
  local store="$1" alias_name="$2" identity="$3" state="$4" sf_bin="$5"
  local expected_store_hash="$6" expected_alias_hash="$7"
  local cipher="$store/devhubs/$alias_name.sfdx-auth-url.age" current_store_hash current_alias_hash
  local age_status sf_status

  [[ ! -e "$state/pause" ]] || die 'auth store paused'
  pull_worker_store "$store" "$state"
  require_identity "$identity"
  if [[ ! -f "$cipher" ]]; then
    if [[ -n "$expected_store_hash" || -n "$expected_alias_hash" ]]; then
      integrity_die "$state"
    fi
    die "missing encrypted alias: $alias_name"
  fi
  read_recipients "$store" || integrity_die "$state"
  current_store_hash="$(store_hash "$store")"
  current_alias_hash="$(file_hash "$cipher")"
  [[ -z "$expected_store_hash" || "$expected_store_hash" == "$current_store_hash" ]] ||
    integrity_die "$state"
  [[ -z "$expected_alias_hash" || "$expected_alias_hash" == "$current_alias_hash" ]] ||
    integrity_die "$state"

  if [[ -n "$expected_alias_hash" && -f "$state/aliases/$alias_name.hash" &&
    "$(cat "$state/aliases/$alias_name.hash")" == "$current_alias_hash" &&
    -f "$state/aliases/$alias_name.health" &&
    "$(cat "$state/aliases/$alias_name.health")" == healthy ]]; then
    write_marker "$state/store.hash" "$current_store_hash"
    echo "authenticated Dev Hub: $alias_name"
    return
  fi

  export SF_USE_GENERIC_UNIX_KEYCHAIN=true
  set +e
  age -d -i "$identity" "$cipher" 2>/dev/null |
    "$sf_bin" org login sfdx-url --alias "$alias_name" --set-default-dev-hub \
      --json --sfdx-url-stdin >/dev/null 2>&1
  pipeline_status=("${PIPESTATUS[@]}")
  set -e
  age_status="${pipeline_status[0]}"
  sf_status="${pipeline_status[1]}"
  if ((age_status != 0)); then
    integrity_die "$state"
  fi
  if ((sf_status != 0)); then
    quarantine_alias "$state" "$alias_name" login
    die "Dev Hub login failed: $alias_name"
  fi
  write_marker "$state/store.hash" "$current_store_hash"
  mark_alias_healthy "$state" "$alias_name" "$current_alias_hash"
  echo "authenticated Dev Hub: $alias_name"
}

command_name="${1:-}"
[[ -n "$command_name" ]] ||
  die 'usage: dev-hub-auth.sh <init-host|put|list|replace|rotate|rewrap-all|login|verify> [options]'
shift

case "$command_name" in
  init-host)
    identity=''
    while (($#)); do
      [[ "$1" == '--identity' && $# -ge 2 ]] || die 'unexpected argument'
      identity="$2"
      shift 2
    done
    require_absolute identity "$identity"
    command -v age-keygen >/dev/null || die 'age-keygen is required'
    if [[ ! -f "$identity" ]]; then
      mkdir -p "$(dirname "$identity")"
      age-keygen -o "$identity" >/dev/null 2>&1 || die 'age identity creation failed'
      chmod 600 "$identity"
    fi
    require_identity "$identity"
    age-keygen -y "$identity" 2>/dev/null || die 'age recipient read failed'
    ;;

  put|replace|rotate)
    parse_store_write "$@"
    cipher="$store/devhubs/$alias_name.sfdx-auth-url.age"
    case "$command_name" in
      put) [[ ! -e "$cipher" ]] || die "encrypted alias already exists: $alias_name" ;;
      replace|rotate) [[ -f "$cipher" ]] || die "missing encrypted alias: $alias_name" ;;
    esac
    if [[ "$command_name" == rotate ]]; then
      require_absolute identity "$identity"
      require_identity "$identity"
      hash_decrypted_credential "$store" "$identity" "$cipher"
    elif [[ -n "$identity" ]]; then
      die 'identity is only valid for rotate'
    fi
    read_recipients "$store" || die 'missing recipients'
    if [[ "$command_name" != rotate ]]; then
      git -C "$store" diff --quiet HEAD -- recipients.txt ||
        die 'recipient changes require rotate or rewrap-all'
    fi
    cipher_relative="devhubs/$alias_name.sfdx-auth-url.age"
    begin_store_transaction "$store" "$cipher_relative"
    encrypt_source_alias "$store" "$alias_name" "$source_alias" "$sf_bin" \
      "${decrypted_hash:-}"
    if [[ "$command_name" == rotate ]] && ! git -C "$store" diff --quiet HEAD -- recipients.txt; then
      disarm_store_transaction
      echo "rotated encrypted Dev Hub: $alias_name"
    else
      commit_store "$store" "Update encrypted Dev Hub $alias_name" \
        "$cipher_relative"
      echo "stored encrypted Dev Hub: $alias_name"
    fi
    ;;

  list)
    store=''
    state=''
    expected_store_hash=''
    while (($#)); do
      (($# >= 2)) || die 'missing argument value'
      case "$1" in
        --store) store="$2" ;;
        --state) state="$2" ;;
        --expected-store-hash) expected_store_hash="$2" ;;
        *) die 'unexpected argument' ;;
      esac
      shift 2
    done
    require_absolute store "$store"
    if [[ -n "$state" ]]; then
      require_absolute state "$state"
      pull_worker_store "$store" "$state"
    else
      [[ -z "$expected_store_hash" ]] || die 'expected store hash requires state'
      pull_store "$store"
    fi
    if ! read_recipients "$store"; then
      [[ -z "$state" ]] && die 'missing recipients'
      integrity_die "$state"
    fi
    load_tracked_ciphers "$store" || die 'auth store integrity failed'
    current_store_hash="$(store_hash "$store")"
    if [[ -n "$expected_store_hash" ]]; then
      [[ "$expected_store_hash" == "$current_store_hash" ]] || integrity_die "$state"
      find "$state/pause" -maxdepth 0 -type f -delete 2>/dev/null || true
    fi
    printf 'store\t%s\n' "$current_store_hash"
    if ((${#ciphers[@]})); then
      for cipher in "${ciphers[@]}"; do
        alias_name="$(basename "$cipher" .sfdx-auth-url.age)"
        printf 'alias\t%s\t%s\n' "$alias_name" "$(file_hash "$cipher")"
      done
    fi
    ;;

  rewrap-all)
    store=''
    identity=''
    while (($#)); do
      (($# >= 2)) || die 'missing argument value'
      case "$1" in
        --store) store="$2" ;;
        --identity) identity="$2" ;;
        *) die 'unexpected argument' ;;
      esac
      shift 2
    done
    require_absolute store "$store"
    require_absolute identity "$identity"
    require_identity "$identity"
    command -v age >/dev/null || die 'age is required'
    require_operator "$store"
    pull_store "$store"
    read_recipients "$store" || die 'missing recipients'
    load_tracked_ciphers "$store" || die 'auth store integrity failed'
    ((${#ciphers[@]} > 0)) || die 'missing encrypted aliases'

    committed_recipients="$(git -C "$store" show HEAD:recipients.txt 2>/dev/null)" ||
      die 'unable to read committed recipients'
    removed_recipient=false
    while IFS= read -r recipient; do
      [[ -z "$recipient" || "$recipient" == \#* ]] && continue
      if ! grep -Fqx -- "$recipient" "$recipients_file"; then
        removed_recipient=true
        break
      fi
    done <<<"$committed_recipients"

    commit_paths=(recipients.txt)
    for cipher in "${ciphers[@]}"; do
      commit_paths+=("devhubs/$(basename "$cipher")")
    done
    if [[ "$removed_recipient" == true ]]; then
      for cipher in "${ciphers[@]}"; do
        relative="devhubs/$(basename "$cipher")"
        if git -C "$store" diff --quiet HEAD -- "$relative"; then
          alias_name="$(basename "$cipher" .sfdx-auth-url.age)"
          die "recipient removal requires rotating every credential first: $alias_name"
        fi
      done
    fi
    begin_store_transaction "$store" "${commit_paths[@]}"
    if [[ "$removed_recipient" != true ]]; then
      temporary_dir="$(mktemp -d "$store/devhubs/.rewrap.XXXXXX")"
      temporary_paths+=("$temporary_dir")
      for cipher in "${ciphers[@]}"; do
        temporary_cipher="$temporary_dir/$(basename "$cipher")"
        set +e
        age -d -i "$identity" "$cipher" 2>/dev/null |
          age "${age_args[@]}" -o "$temporary_cipher" >/dev/null 2>&1
        pipeline_status=("${PIPESTATUS[@]}")
        set -e
        if ((pipeline_status[0] != 0 || pipeline_status[1] != 0)); then
          die 'auth store rewrap failed'
        fi
      done
      for cipher in "${ciphers[@]}"; do
        mv "$temporary_dir/$(basename "$cipher")" "$cipher"
      done
    fi
    commit_store "$store" 'Rewrap encrypted Dev Hubs' "${commit_paths[@]}"
    echo 'rewrapped encrypted Dev Hubs'
    ;;

  login|verify)
    store=''
    alias_name=''
    identity=''
    state=''
    sf_bin=''
    expected_store_hash=''
    expected_alias_hash=''
    while (($#)); do
      (($# >= 2)) || die 'missing argument value'
      case "$1" in
        --store) store="$2" ;;
        --alias) alias_name="$2" ;;
        --identity) identity="$2" ;;
        --state) state="$2" ;;
        --sf-bin) sf_bin="$2" ;;
        --expected-store-hash) expected_store_hash="$2" ;;
        --expected-alias-hash) expected_alias_hash="$2" ;;
        *) die 'unexpected argument' ;;
      esac
      shift 2
    done
    require_absolute store "$store"
    require_absolute identity "$identity"
    require_absolute sf-bin "$sf_bin"
    [[ -n "$state" ]] || state="$(dirname "$identity")/state"
    require_absolute state "$state"
    require_alias "$alias_name"
    [[ -x "$sf_bin" ]] || die 'Salesforce CLI is not executable'
    command -v age >/dev/null || die 'age is required'
    login_dev_hub "$store" "$alias_name" "$identity" "$state" "$sf_bin" \
      "$expected_store_hash" "$expected_alias_hash"
    if [[ "$command_name" == verify ]]; then
      if ! "$sf_bin" org display --target-org "$alias_name" --json >/dev/null 2>&1; then
        quarantine_alias "$state" "$alias_name" quota
        die "Dev Hub verification failed: $alias_name"
      fi
      mark_alias_healthy "$state" "$alias_name" "$(file_hash "$store/devhubs/$alias_name.sfdx-auth-url.age")"
      echo "verified Dev Hub: $alias_name"
    fi
    ;;

  *) die "unknown command: $command_name" ;;
esac
