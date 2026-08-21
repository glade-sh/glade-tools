#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
helper="$root/scripts/corpus-assurance/dev-hub-auth.sh"
tmp="$(mktemp -d "${TMPDIR:-/tmp}/glade-dev-hub-auth-test.XXXXXX")"
trap 'find "$tmp" -depth -delete' EXIT
mkdir -p "$tmp/bin" "$tmp/store/devhubs" "$tmp/logs"

export FAKE_LOG_DIR="$tmp/logs"
export PATH="$tmp/bin:$PATH"

printf '%s\n' '#!/usr/bin/env bash' 'set -euo pipefail' \
  'printf "%s\n" "$*" >>"$FAKE_LOG_DIR/age-keygen.log"' \
  'if [[ "${1:-}" == "-y" ]]; then echo age1hostrecipient; exit 0; fi' \
  '[[ "${1:-}" == "-o" ]]' \
  'printf "%s\n" AGE-SECRET-KEY-TEST >"$2"' \
  >"$tmp/bin/age-keygen"

printf '%s\n' '#!/usr/bin/env bash' 'set -euo pipefail' \
  'printf "%s\n" "$*" >>"$FAKE_LOG_DIR/age.log"' \
  '[[ "${FAKE_AGE_FAIL:-0}" != 1 ]] || exit 7' \
  'if [[ "${1:-}" == "-d" ]]; then printf "%s%s\n" "force:" "//TEST-SECRET"; exit 0; fi' \
  'output=""; while (($#)); do if [[ "$1" == "-o" ]]; then output="$2"; shift 2; else shift; fi; done' \
  '[[ -n "$output" ]]; input="$(cat)"; [[ "$input" == "force:""//TEST-SECRET" ]]' \
  'printf "%s\n" encrypted-test-value >"$output"' \
  >"$tmp/bin/age"

printf '%s\n' '#!/usr/bin/env bash' 'set -euo pipefail' \
  'printf "%s\n" "$*" >>"$FAKE_LOG_DIR/sf.log"' \
  'if [[ "$*" == *"org auth show-sfdx-auth-url"* ]]; then printf "%s\n" '\''{"status":0,"result":{"sfdxAuthUrl":"force://TEST-SECRET"}}'\''; exit 0; fi' \
  'if [[ "$*" == *"org login sfdx-url"* ]]; then input="$(cat)"; [[ "$input" == "force:""//TEST-SECRET" ]]; [[ "${FAKE_SF_FAIL:-0}" != 1 ]] || exit 8; printf "%s\n" '\''{"status":0}'\''; exit 0; fi' \
  'if [[ "$*" == *"org display"* ]]; then [[ "${FAKE_SF_VERIFY_FAIL:-0}" != 1 ]] || exit 9; printf "%s\n" '\''{"status":0}'\''; exit 0; fi' \
  'exit 2' \
  >"$tmp/bin/sf"

printf '%s\n' '#!/usr/bin/env bash' 'set -euo pipefail' \
  'printf "%s\n" "$*" >>"$FAKE_LOG_DIR/git.log"' \
  'exit 0' \
  >"$tmp/bin/git"
chmod +x "$tmp/bin/age-keygen" "$tmp/bin/age" "$tmp/bin/sf" "$tmp/bin/git"

identity="$tmp/identity/identity.txt"
recipient="$($helper init-host --identity "$identity")"
[[ "$recipient" == age1hostrecipient ]]
[[ "$(python3 -c 'import os, stat, sys; print(oct(stat.S_IMODE(os.stat(sys.argv[1]).st_mode)))' "$identity")" == 0o600 ]]
$helper init-host --identity "$identity" >"$tmp/init-again.out"
[[ "$(grep -c -- '^-o ' "$tmp/logs/age-keygen.log")" == 1 ]]

printf '%s\n' age1hostrecipient age1workerrecipient >"$tmp/store/recipients.txt"
bash -x "$helper" put --store "$tmp/store" --alias glade-dev-hub \
  --source-alias source-dev-hub --sf-bin "$tmp/bin/sf" \
  >"$tmp/put.out" 2>"$tmp/put.err"
[[ -f "$tmp/store/devhubs/glade-dev-hub.sfdx-auth-url.age" ]]
grep -F -- '-r age1hostrecipient -r age1workerrecipient' "$tmp/logs/age.log"
grep -F -- '-C '"$tmp/store"' pull --ff-only' "$tmp/logs/git.log"
grep -F -- 'org auth show-sfdx-auth-url --target-org source-dev-hub --json' "$tmp/logs/sf.log"

if "$helper" put --store "$tmp/store" --alias glade-dev-hub \
  --source-alias source-dev-hub --sf-bin "$tmp/bin/sf" 2>"$tmp/duplicate.err"; then
  echo 'put overwrote an encrypted alias' >&2
  exit 1
fi
grep -F 'encrypted alias already exists: glade-dev-hub' "$tmp/duplicate.err"

mkdir -p "$tmp/missing/devhubs"
if "$helper" put --store "$tmp/missing" --alias missing \
  --source-alias source-dev-hub --sf-bin "$tmp/bin/sf" 2>"$tmp/missing.err"; then
  echo 'put accepted missing recipients' >&2
  exit 1
fi
grep -F 'missing recipients:' "$tmp/missing.err"

"$helper" login --store "$tmp/store" --alias glade-dev-hub \
  --identity "$identity" --sf-bin "$tmp/bin/sf" >"$tmp/login.out" 2>"$tmp/login.err"
grep -F 'org login sfdx-url --sfdx-url-stdin --alias glade-dev-hub --set-default-dev-hub --json' "$tmp/logs/sf.log"

if FAKE_AGE_FAIL=1 "$helper" login --store "$tmp/store" --alias glade-dev-hub \
  --identity "$identity" --sf-bin "$tmp/bin/sf" 2>"$tmp/decrypt.err"; then
  echo 'login accepted decryption failure' >&2
  exit 1
fi
grep -F 'Dev Hub login failed: glade-dev-hub' "$tmp/decrypt.err"

if FAKE_SF_FAIL=1 "$helper" login --store "$tmp/store" --alias glade-dev-hub \
  --identity "$identity" --sf-bin "$tmp/bin/sf" 2>"$tmp/sf.err"; then
  echo 'login accepted Salesforce failure' >&2
  exit 1
fi
grep -F 'Dev Hub login failed: glade-dev-hub' "$tmp/sf.err"

"$helper" verify --store "$tmp/store" --alias glade-dev-hub \
  --identity "$identity" --sf-bin "$tmp/bin/sf" >"$tmp/verify.out" 2>"$tmp/verify.err"
grep -Fx 'verified Dev Hub: glade-dev-hub' "$tmp/verify.out"

if grep -R -F 'force://' "$tmp/logs" "$tmp"/*.out "$tmp"/*.err "$tmp/store/recipients.txt"; then
  echo 'plaintext auth URL escaped the encrypted pipeline' >&2
  exit 1
fi

echo 'Dev Hub auth helper tests passed'
