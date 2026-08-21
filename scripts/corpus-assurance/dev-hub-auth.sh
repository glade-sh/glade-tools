#!/usr/bin/env bash
set -euo pipefail
set +x
umask 077

die() {
  echo "$1" >&2
  exit 1
}

require_absolute() {
  [[ "$2" == /* ]] || die "$1 must be an absolute path"
}

pull_store() {
  git -C "$1" pull --ff-only >/dev/null 2>&1 || die "auth store update failed: $1"
}

login_dev_hub() {
  local store="$1" alias_name="$2" identity="$3" sf_bin="$4"
  local cipher="$store/devhubs/$alias_name.sfdx-auth-url.age"
  pull_store "$store"
  [[ -f "$identity" ]] || die "missing identity: $identity"
  [[ -f "$cipher" ]] || die "missing encrypted alias: $alias_name"
  export SF_USE_GENERIC_UNIX_KEYCHAIN=true
  if ! age -d -i "$identity" "$cipher" 2>/dev/null |
    "$sf_bin" org login sfdx-url --alias "$alias_name" --set-default-dev-hub \
      --json --sfdx-url-stdin >/dev/null 2>&1; then
    die "Dev Hub login failed: $alias_name"
  fi
  echo "authenticated Dev Hub: $alias_name"
}

command_name="${1:-}"
[[ -n "$command_name" ]] || die 'usage: dev-hub-auth.sh <init-host|put|login|verify> [options]'
shift

case "$command_name" in
  init-host)
    identity=''
    while (($#)); do
      [[ "$1" == '--identity' && $# -ge 2 ]] || die "unexpected argument: $1"
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
    age-keygen -y "$identity"
    ;;

  put)
    store=''
    alias_name=''
    source_alias=''
    sf_bin=''
    while (($#)); do
      (($# >= 2)) || die "missing value: $1"
      case "$1" in
        --store) store="$2" ;;
        --alias) alias_name="$2" ;;
        --source-alias) source_alias="$2" ;;
        --sf-bin) sf_bin="$2" ;;
        *) die "unexpected argument: $1" ;;
      esac
      shift 2
    done
    require_absolute store "$store"
    require_absolute sf-bin "$sf_bin"
    [[ "$alias_name" =~ ^[A-Za-z0-9._-]+$ ]] || die 'invalid alias'
    [[ -n "$source_alias" ]] || die 'missing source alias'
    [[ -x "$sf_bin" ]] || die "Salesforce CLI is not executable: $sf_bin"
    command -v age >/dev/null || die 'age is required'
    pull_store "$store"

    recipients_file="$store/recipients.txt"
    [[ -s "$recipients_file" ]] || die "missing recipients: $recipients_file"
    recipients=()
    while IFS= read -r recipient; do
      [[ -z "$recipient" || "$recipient" == \#* ]] || recipients+=("$recipient")
    done <"$recipients_file"
    ((${#recipients[@]} > 0)) || die "missing recipients: $recipients_file"

    mkdir -p "$store/devhubs"
    cipher="$store/devhubs/$alias_name.sfdx-auth-url.age"
    [[ ! -e "$cipher" ]] || die "encrypted alias already exists: $alias_name"
    auth_url="$("$sf_bin" org auth show-sfdx-auth-url --target-org "$source_alias" \
      --json 2>/dev/null | python3 -c 'import json, sys
data = json.load(sys.stdin)
url = data.get("result", {}).get("sfdxAuthUrl")
if not isinstance(url, str):
    raise SystemExit(1)
print(url, end="")' 2>/dev/null)" || die "unable to read source Dev Hub: $source_alias"
    [[ "$auth_url" == force://* ]] || die "invalid source Dev Hub auth URL: $source_alias"

    age_args=()
    for recipient in "${recipients[@]}"; do
      age_args+=(-r "$recipient")
    done
    temporary_cipher="$cipher.tmp.$$"
    if ! printf '%s\n' "$auth_url" | age "${age_args[@]}" -o "$temporary_cipher" \
      >/dev/null 2>&1; then
      find "$temporary_cipher" -maxdepth 0 -type f -delete 2>/dev/null || true
      die "Dev Hub encryption failed: $alias_name"
    fi
    unset auth_url
    mv "$temporary_cipher" "$cipher"
    git -C "$store" add -- recipients.txt "devhubs/$alias_name.sfdx-auth-url.age" \
      >/dev/null 2>&1 || die 'auth store add failed'
    git -C "$store" commit -m "Update encrypted Dev Hub $alias_name" \
      >/dev/null 2>&1 || die 'auth store commit failed'
    git -C "$store" push >/dev/null 2>&1 || die 'auth store push failed'
    echo "stored encrypted Dev Hub: $alias_name"
    ;;

  login|verify)
    store=''
    alias_name=''
    identity=''
    sf_bin=''
    while (($#)); do
      (($# >= 2)) || die "missing value: $1"
      case "$1" in
        --store) store="$2" ;;
        --alias) alias_name="$2" ;;
        --identity) identity="$2" ;;
        --sf-bin) sf_bin="$2" ;;
        *) die "unexpected argument: $1" ;;
      esac
      shift 2
    done
    require_absolute store "$store"
    require_absolute identity "$identity"
    require_absolute sf-bin "$sf_bin"
    [[ "$alias_name" =~ ^[A-Za-z0-9._-]+$ ]] || die 'invalid alias'
    [[ -x "$sf_bin" ]] || die "Salesforce CLI is not executable: $sf_bin"
    command -v age >/dev/null || die 'age is required'
    login_dev_hub "$store" "$alias_name" "$identity" "$sf_bin"
    if [[ "$command_name" == verify ]]; then
      "$sf_bin" org display --target-org "$alias_name" --json >/dev/null 2>&1 ||
        die "Dev Hub verification failed: $alias_name"
      echo "verified Dev Hub: $alias_name"
    fi
    ;;

  *) die "unknown command: $command_name" ;;
esac
