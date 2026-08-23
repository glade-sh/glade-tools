#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
helper="$root/scripts/corpus-assurance/dev-hub-auth.sh"
tmp="$(mktemp -d "${TMPDIR:-/tmp}/glade-dev-hub-auth-test.XXXXXX")"
trap 'find "$tmp" -depth -delete' EXIT
mkdir -p "$tmp/bin" "$tmp/logs"

export FAKE_LOG_DIR="$tmp/logs"
export FAKE_AGE_COUNTER="$tmp/age-counter"
export REAL_GIT="$(command -v git)"
export REAL_MV="$(command -v mv)"
export FAKE_MV_COUNTER="$tmp/mv-counter"

printf '%s\n' '#!/usr/bin/env bash' 'set -euo pipefail' \
  'printf "%s\n" "$*" >>"$FAKE_LOG_DIR/age-keygen.log"' \
  'if [[ "${FAKE_AGE_KEYGEN_FAIL:-0}" == 1 ]]; then echo RAW-AGE-ERROR >&2; exit 6; fi' \
  'if [[ "${1:-}" == "-y" ]]; then echo age1hostrecipient; exit 0; fi' \
  '[[ "${1:-}" == "-o" ]]' \
  'printf "%s\n" AGE-SECRET-KEY-TEST >"$2"' \
  >"$tmp/bin/age-keygen"

printf '%s\n' '#!/usr/bin/env bash' 'set -euo pipefail' \
  'printf "%s\n" "$*" >>"$FAKE_LOG_DIR/age.log"' \
  'if [[ "${1:-}" == "-d" ]]; then [[ "${FAKE_AGE_FAIL:-0}" != 1 ]] || exit 7; printf "%s%s\n" "force:" "//TEST-SECRET"; exit 0; fi' \
  'output=""; while (($#)); do if [[ "$1" == "-o" ]]; then output="$2"; shift 2; else shift; fi; done' \
  '[[ -n "$output" ]]; input="$(cat)"; [[ "$input" == "force:""//TEST-SECRET" || "$input" == "force:""//ROTATED-SECRET" ]]' \
  'count=0; [[ ! -f "$FAKE_AGE_COUNTER" ]] || read -r count <"$FAKE_AGE_COUNTER"; count=$((count + 1)); printf "%s\n" "$count" >"$FAKE_AGE_COUNTER"' \
  'printf "%s\n" "encrypted-test-value-$count" >"$output"' \
  '[[ "${FAKE_AGE_FAIL:-0}" != 1 ]] || exit 7' \
  >"$tmp/bin/age"

printf '%s\n' '#!/usr/bin/env bash' 'set -euo pipefail' \
  'printf "%s\n" "$*" >>"$FAKE_LOG_DIR/sf.log"' \
  'if [[ "$*" == *"org auth show-sfdx-auth-url"* ]]; then secret=TEST-SECRET; [[ "$*" != *"--target-org rotated-"* ]] || secret=ROTATED-SECRET; printf '\''{"status":0,"result":{"sfdxAuthUrl":"force://%s"}}\n'\'' "$secret"; exit 0; fi' \
  'if [[ "$*" == *"org login sfdx-url"* ]]; then [[ "${SF_USE_GENERIC_UNIX_KEYCHAIN:-}" == true ]]; [[ "${!#}" == "--sfdx-url-stdin" ]]; input="$(cat)"; [[ "$input" == "force:""//TEST-SECRET" ]]; [[ "${FAKE_SF_FAIL:-0}" != 1 ]] || exit 8; printf "%s\n" '\''{"status":0}'\''; exit 0; fi' \
  'if [[ "$*" == *"org display"* ]]; then [[ "${SF_USE_GENERIC_UNIX_KEYCHAIN:-}" == true ]]; [[ "${FAKE_SF_VERIFY_FAIL:-0}" != 1 ]] || exit 9; printf "%s\n" '\''{"status":0}'\''; exit 0; fi' \
  'exit 2' \
  >"$tmp/bin/sf"

printf '%s\n' '#!/usr/bin/env bash' 'set -euo pipefail' \
  'printf "%s\n" "$*" >>"$FAKE_LOG_DIR/git.log"' \
  'if [[ "${FAKE_GIT_PULL_FAIL:-0}" == 1 && "$*" == *" pull --ff-only"* ]]; then exit 10; fi' \
  'if [[ "${FAKE_GIT_COMMIT_FAIL:-0}" == 1 && " $* " == *" commit "* ]]; then exit 11; fi' \
  'if [[ "${FAKE_GIT_PUSH_FAIL:-0}" == 1 && " $* " == *" push "* ]]; then exit 12; fi' \
  'exec "$REAL_GIT" "$@"' \
  >"$tmp/bin/git"
printf '%s\n' '#!/usr/bin/env bash' 'set -euo pipefail' \
  'if [[ "${FAKE_MV_FAIL_REWRAP:-0}" == 1 && "$*" == *"/.rewrap."* ]]; then' \
  '  count=0; [[ ! -f "$FAKE_MV_COUNTER" ]] || read -r count <"$FAKE_MV_COUNTER"; count=$((count + 1)); printf "%s\n" "$count" >"$FAKE_MV_COUNTER"' \
  '  ((count < 2)) || exit 13' \
  'fi' \
  'exec "$REAL_MV" "$@"' \
  >"$tmp/bin/mv"
chmod +x "$tmp/bin/age-keygen" "$tmp/bin/age" "$tmp/bin/sf" "$tmp/bin/git" "$tmp/bin/mv"
export PATH="$tmp/bin:$PATH"

"$REAL_GIT" init --bare "$tmp/remote.git" >/dev/null
"$REAL_GIT" clone "$tmp/remote.git" "$tmp/store" >/dev/null 2>&1
"$REAL_GIT" -C "$tmp/store" config user.name Test
"$REAL_GIT" -C "$tmp/store" config user.email test@example.invalid
"$REAL_GIT" -C "$tmp/store" config glade.authRole operator
mkdir -p "$tmp/store/devhubs"
printf '%s\n' age1hostrecipient age1workerrecipient >"$tmp/store/recipients.txt"
"$REAL_GIT" -C "$tmp/store" add recipients.txt
"$REAL_GIT" -C "$tmp/store" commit -m recipients >/dev/null
"$REAL_GIT" -C "$tmp/store" push -u origin HEAD >/dev/null 2>&1

identity="$tmp/identity/identity.txt"
state="$tmp/state"
recipient="$($helper init-host --identity "$identity")"
[[ "$recipient" == age1hostrecipient ]]
[[ "$(python3 -c 'import os, stat, sys; print(oct(stat.S_IMODE(os.stat(sys.argv[1]).st_mode)))' "$identity")" == 0o600 ]]
$helper init-host --identity "$identity" >"$tmp/init-again.out"
[[ "$(grep -c -- '^-o ' "$tmp/logs/age-keygen.log")" == 1 ]]
chmod 644 "$identity"
if "$helper" init-host --identity "$identity" 2>"$tmp/identity-mode.err"; then
  echo 'init-host accepted an existing non-0600 identity' >&2
  exit 1
fi
grep -F 'identity must have mode 0600' "$tmp/identity-mode.err"
chmod 600 "$identity"
if FAKE_AGE_KEYGEN_FAIL=1 "$helper" init-host --identity "$identity" 2>"$tmp/age-keygen.err"; then
  echo 'init-host accepted age-keygen failure' >&2
  exit 1
fi
grep -F 'age recipient read failed' "$tmp/age-keygen.err"
! grep -F 'RAW-AGE-ERROR' "$tmp/age-keygen.err"

if "$helper" put --store "$tmp/store" --alias .hidden \
  --source-alias source-dev-hub --sf-bin "$tmp/bin/sf" 2>"$tmp/hidden.err"; then
  echo 'put accepted a leading-dot alias' >&2
  exit 1
fi
grep -F 'invalid alias' "$tmp/hidden.err"

printf '%s\n' unrelated-staged-note >"$tmp/store/operator-note.txt"
"$REAL_GIT" -C "$tmp/store" add operator-note.txt
bash -x "$helper" put --store "$tmp/store" --alias glade-dev-hub \
  --source-alias source-dev-hub --sf-bin "$tmp/bin/sf" \
  >"$tmp/put.out" 2>"$tmp/put.err"
[[ -f "$tmp/store/devhubs/glade-dev-hub.sfdx-auth-url.age" ]]
grep -F -- '-r age1hostrecipient -r age1workerrecipient' "$tmp/logs/age.log"
grep -F -- '-C '"$tmp/store"' pull --ff-only' "$tmp/logs/git.log"
grep -F -- 'org auth show-sfdx-auth-url --target-org source-dev-hub --json' "$tmp/logs/sf.log"
if "$REAL_GIT" --git-dir="$tmp/remote.git" show HEAD:operator-note.txt >/dev/null 2>&1; then
  echo 'put committed an unrelated staged file' >&2
  exit 1
fi
"$REAL_GIT" -C "$tmp/store" diff --cached --name-only | grep -Fx operator-note.txt
"$REAL_GIT" -C "$tmp/store" reset -- operator-note.txt >/dev/null
find "$tmp/store/operator-note.txt" -maxdepth 0 -type f -delete

if "$helper" put --store "$tmp/store" --alias glade-dev-hub \
  --source-alias source-dev-hub --sf-bin "$tmp/bin/sf" 2>"$tmp/duplicate.err"; then
  echo 'put overwrote an encrypted alias' >&2
  exit 1
fi
grep -F 'encrypted alias already exists: glade-dev-hub' "$tmp/duplicate.err"

"$REAL_GIT" clone "$tmp/remote.git" "$tmp/missing" >/dev/null 2>&1
"$REAL_GIT" -C "$tmp/missing" config glade.authRole operator
find "$tmp/missing/recipients.txt" -maxdepth 0 -type f -delete
if "$helper" put --store "$tmp/missing" --alias missing \
  --source-alias source-dev-hub --sf-bin "$tmp/bin/sf" 2>"$tmp/missing.err"; then
  echo 'put accepted missing recipients' >&2
  exit 1
fi
grep -F 'missing recipients' "$tmp/missing.err"

"$helper" list --store "$tmp/store" >"$tmp/list.out"
grep -Eq '^store[[:space:]][0-9a-f]{64}$' "$tmp/list.out"
grep -Eq '^alias[[:space:]]+glade-dev-hub[[:space:]][0-9a-f]{64}$' "$tmp/list.out"
if ! /bin/bash "$helper" list --store "$tmp/store" >"$tmp/list-bash32.out" 2>"$tmp/list-bash32.err"; then
  echo 'list failed under the system Bash with no temporary paths' >&2
  exit 1
fi
[[ ! -s "$tmp/list-bash32.err" ]]

old_hash="$(awk '$1 == "alias" {print $3}' "$tmp/list.out")"
"$helper" replace --store "$tmp/store" --alias glade-dev-hub \
  --source-alias replacement-dev-hub --sf-bin "$tmp/bin/sf" >"$tmp/replace.out"
new_hash="$($helper list --store "$tmp/store" | awk '$1 == "alias" {print $3}')"
[[ "$old_hash" != "$new_hash" ]]

"$helper" put --store "$tmp/store" --alias second-dev-hub \
  --source-alias second-source --sf-bin "$tmp/bin/sf" >"$tmp/second.out"
cp "$tmp/store/devhubs/glade-dev-hub.sfdx-auth-url.age" "$tmp/before-unchanged-rotate.age"
if "$helper" rotate --store "$tmp/store" --alias glade-dev-hub \
  --source-alias source-dev-hub --identity "$identity" --sf-bin "$tmp/bin/sf" \
  2>"$tmp/unchanged-rotate.err"; then
  echo 'rotate accepted an unchanged underlying credential' >&2
  exit 1
fi
grep -F 'rotated credential is unchanged' "$tmp/unchanged-rotate.err"
cmp "$tmp/before-unchanged-rotate.age" "$tmp/store/devhubs/glade-dev-hub.sfdx-auth-url.age"
cp "$identity" "$tmp/bad-mode-identity"
chmod 644 "$tmp/bad-mode-identity"
if "$helper" rotate --store "$tmp/store" --alias glade-dev-hub \
  --source-alias rotated-first --identity "$tmp/bad-mode-identity" --sf-bin "$tmp/bin/sf" \
  2>"$tmp/rotate-mode.err"; then
  echo 'rotate accepted a non-0600 identity' >&2
  exit 1
fi
grep -F 'identity must have mode 0600' "$tmp/rotate-mode.err"
cp "$tmp/store/devhubs/second-dev-hub.sfdx-auth-url.age" "$tmp/second-before-replace.age"
"$helper" replace --store "$tmp/store" --alias glade-dev-hub \
  --source-alias replacement-dev-hub --sf-bin "$tmp/bin/sf" >"$tmp/replace-first-only.out"
cmp "$tmp/second-before-replace.age" "$tmp/store/devhubs/second-dev-hub.sfdx-auth-url.age"
cp "$tmp/store/devhubs/glade-dev-hub.sfdx-auth-url.age" "$tmp/first-before-rewrap.age"
printf '%s\n' unrelated-public-note >"$tmp/store/devhubs/operator-note.txt"
printf '%s\n' age1hostrecipient age1workerrecipient age1newrecipient >"$tmp/store/recipients.txt"
"$helper" rewrap-all --store "$tmp/store" --identity "$identity" >"$tmp/rewrap.out"
cmp -s "$tmp/first-before-rewrap.age" "$tmp/store/devhubs/glade-dev-hub.sfdx-auth-url.age" && {
  echo 'rewrap-all did not replace the first ciphertext' >&2
  exit 1
}
[[ -f "$tmp/store/devhubs/second-dev-hub.sfdx-auth-url.age" ]]
if "$REAL_GIT" -C "$tmp/store" ls-files --error-unmatch devhubs/operator-note.txt >/dev/null 2>&1; then
  echo 'rewrap-all tracked an unrelated file' >&2
  exit 1
fi
find "$tmp/store/devhubs/operator-note.txt" -maxdepth 0 -type f -delete
grep -F -- '-r age1newrecipient' "$tmp/logs/age.log"

printf '%s\n' age1hostrecipient >"$tmp/store/recipients.txt"
if "$helper" rewrap-all --store "$tmp/store" --identity "$identity" 2>"$tmp/removal.err"; then
  echo 'rewrap-all removed recipients without credential rotation' >&2
  exit 1
fi
grep -F 'recipient removal requires rotating every credential first' "$tmp/removal.err"

"$helper" rotate --store "$tmp/store" --alias glade-dev-hub \
  --source-alias rotated-first --identity "$identity" --sf-bin "$tmp/bin/sf" >"$tmp/rotate-first.out"
if "$helper" rewrap-all --store "$tmp/store" --identity "$identity" 2>"$tmp/partial-rotation.err"; then
  echo 'rewrap-all accepted a partial credential rotation' >&2
  exit 1
fi
grep -F 'recipient removal requires rotating every credential first: second-dev-hub' "$tmp/partial-rotation.err"
"$helper" rotate --store "$tmp/store" --alias second-dev-hub \
  --source-alias rotated-second --identity "$identity" --sf-bin "$tmp/bin/sf" >"$tmp/rotate-second.out"
printf '%s\n' unrelated-staged-note >"$tmp/store/transaction-note.txt"
"$REAL_GIT" -C "$tmp/store" add transaction-note.txt
cp "$tmp/store/recipients.txt" "$tmp/before-failed-removal-rewrap.recipients"
cp "$tmp/store/devhubs/glade-dev-hub.sfdx-auth-url.age" "$tmp/before-failed-removal-rewrap.first"
cp "$tmp/store/devhubs/second-dev-hub.sfdx-auth-url.age" "$tmp/before-failed-removal-rewrap.second"
store_index="$($REAL_GIT -C "$tmp/store" rev-parse --absolute-git-dir)/index"
cp "$store_index" "$tmp/before-failed-removal-rewrap.index"
store_head="$($REAL_GIT -C "$tmp/store" rev-parse HEAD)"
if FAKE_GIT_PUSH_FAIL=1 "$helper" rewrap-all --store "$tmp/store" --identity "$identity" \
  2>"$tmp/removal-push.err"; then
  echo 'rewrap-all accepted a rejected push' >&2
  exit 1
fi
grep -F 'auth store push failed' "$tmp/removal-push.err"
[[ "$($REAL_GIT -C "$tmp/store" rev-parse HEAD)" == "$store_head" ]]
cmp "$tmp/before-failed-removal-rewrap.index" "$store_index"
cmp "$tmp/before-failed-removal-rewrap.recipients" "$tmp/store/recipients.txt"
cmp "$tmp/before-failed-removal-rewrap.first" "$tmp/store/devhubs/glade-dev-hub.sfdx-auth-url.age"
cmp "$tmp/before-failed-removal-rewrap.second" "$tmp/store/devhubs/second-dev-hub.sfdx-auth-url.age"
"$REAL_GIT" -C "$tmp/store" diff --cached --name-only | grep -Fx transaction-note.txt
"$helper" rewrap-all --store "$tmp/store" --identity "$identity" >"$tmp/removal-rewrap.out"
[[ "$("$REAL_GIT" -C "$tmp/store" show HEAD:recipients.txt)" == age1hostrecipient ]]

printf '%s\n' age1hostrecipient age1newrecipient >"$tmp/store/recipients.txt"
cp "$tmp/store/recipients.txt" "$tmp/before-partial-rewrap.recipients"
cp "$tmp/store/devhubs/glade-dev-hub.sfdx-auth-url.age" "$tmp/before-partial-rewrap.first"
cp "$tmp/store/devhubs/second-dev-hub.sfdx-auth-url.age" "$tmp/before-partial-rewrap.second"
cp "$store_index" "$tmp/before-partial-rewrap.index"
store_head="$($REAL_GIT -C "$tmp/store" rev-parse HEAD)"
find "$FAKE_MV_COUNTER" -maxdepth 0 -type f -delete 2>/dev/null || true
if FAKE_MV_FAIL_REWRAP=1 "$helper" rewrap-all --store "$tmp/store" --identity "$identity" \
  2>"$tmp/partial-move.err"; then
  echo 'rewrap-all accepted a partial ciphertext move' >&2
  exit 1
fi
[[ "$($REAL_GIT -C "$tmp/store" rev-parse HEAD)" == "$store_head" ]]
cmp "$tmp/before-partial-rewrap.index" "$store_index"
cmp "$tmp/before-partial-rewrap.recipients" "$tmp/store/recipients.txt"
cmp "$tmp/before-partial-rewrap.first" "$tmp/store/devhubs/glade-dev-hub.sfdx-auth-url.age"
cmp "$tmp/before-partial-rewrap.second" "$tmp/store/devhubs/second-dev-hub.sfdx-auth-url.age"
"$REAL_GIT" -C "$tmp/store" diff --cached --name-only | grep -Fx transaction-note.txt
"$helper" rewrap-all --store "$tmp/store" --identity "$identity" >"$tmp/addition-rewrap.out"
"$REAL_GIT" -C "$tmp/store" reset -- transaction-note.txt >/dev/null
find "$tmp/store/transaction-note.txt" -maxdepth 0 -type f -delete

"$REAL_GIT" clone "$tmp/remote.git" "$tmp/worker" >/dev/null 2>&1

"$REAL_GIT" clone --bare "$tmp/remote.git" "$tmp/empty-remote.git" >/dev/null 2>&1
"$REAL_GIT" clone "$tmp/empty-remote.git" "$tmp/empty-worker" >/dev/null 2>&1
"$REAL_GIT" -C "$tmp/empty-worker" config user.name Test
"$REAL_GIT" -C "$tmp/empty-worker" config user.email test@example.invalid
printf '%s\n' '# no active recipients' >"$tmp/empty-worker/recipients.txt"
"$REAL_GIT" -C "$tmp/empty-worker" add recipients.txt
"$REAL_GIT" -C "$tmp/empty-worker" commit -m empty-recipients >/dev/null
"$REAL_GIT" -C "$tmp/empty-worker" push >/dev/null 2>&1
if "$helper" login --store "$tmp/empty-worker" --alias glade-dev-hub \
  --identity "$identity" --state "$tmp/empty-state" --sf-bin "$tmp/bin/sf" \
  2>"$tmp/empty-recipients.err"; then
  echo 'worker accepted a comment-only recipient set' >&2
  exit 1
fi
grep -F 'auth store integrity failed' "$tmp/empty-recipients.err"
[[ "$(cat "$tmp/empty-state/pause")" == store-integrity ]]

if "$helper" replace --store "$tmp/worker" --alias glade-dev-hub \
  --source-alias forbidden --sf-bin "$tmp/bin/sf" 2>"$tmp/worker-write.err"; then
  echo 'worker clone wrote the canonical store' >&2
  exit 1
fi
grep -F 'operator role required' "$tmp/worker-write.err"

remote_before_failure="$($REAL_GIT --git-dir="$tmp/remote.git" rev-parse HEAD)"
"$REAL_GIT" clone "$tmp/remote.git" "$tmp/commit-failure" >/dev/null 2>&1
"$REAL_GIT" -C "$tmp/commit-failure" config user.name Test
"$REAL_GIT" -C "$tmp/commit-failure" config user.email test@example.invalid
"$REAL_GIT" -C "$tmp/commit-failure" config glade.authRole operator
printf '%s\n' unrelated-staged-note >"$tmp/commit-failure/operator-note.txt"
"$REAL_GIT" -C "$tmp/commit-failure" add operator-note.txt
if FAKE_GIT_COMMIT_FAIL=1 "$helper" replace --store "$tmp/commit-failure" \
  --alias glade-dev-hub --source-alias failed-commit --sf-bin "$tmp/bin/sf" \
  2>"$tmp/commit-failure.err"; then
  echo 'replace accepted a Git commit failure' >&2
  exit 1
fi
grep -F 'auth store commit failed' "$tmp/commit-failure.err"
[[ "$($REAL_GIT --git-dir="$tmp/remote.git" rev-parse HEAD)" == "$remote_before_failure" ]]
[[ "$($REAL_GIT -C "$tmp/commit-failure" rev-parse HEAD)" == "$remote_before_failure" ]]
"$REAL_GIT" -C "$tmp/commit-failure" diff --cached --name-only | grep -Fx operator-note.txt
"$REAL_GIT" -C "$tmp/commit-failure" diff --quiet HEAD -- devhubs/glade-dev-hub.sfdx-auth-url.age

"$REAL_GIT" clone "$tmp/remote.git" "$tmp/push-failure" >/dev/null 2>&1
"$REAL_GIT" -C "$tmp/push-failure" config user.name Test
"$REAL_GIT" -C "$tmp/push-failure" config user.email test@example.invalid
"$REAL_GIT" -C "$tmp/push-failure" config glade.authRole operator
printf '%s\n' unrelated-staged-note >"$tmp/push-failure/operator-note.txt"
"$REAL_GIT" -C "$tmp/push-failure" add operator-note.txt
if FAKE_GIT_PUSH_FAIL=1 "$helper" replace --store "$tmp/push-failure" \
  --alias glade-dev-hub --source-alias failed-push --sf-bin "$tmp/bin/sf" \
  2>"$tmp/push-failure.err"; then
  echo 'replace accepted a Git push failure' >&2
  exit 1
fi
grep -F 'auth store push failed' "$tmp/push-failure.err"
[[ "$($REAL_GIT --git-dir="$tmp/remote.git" rev-parse HEAD)" == "$remote_before_failure" ]]
[[ "$($REAL_GIT -C "$tmp/push-failure" rev-parse HEAD)" == "$remote_before_failure" ]]
if "$REAL_GIT" -C "$tmp/push-failure" show HEAD:operator-note.txt >/dev/null 2>&1; then
  echo 'push failure commit included unrelated staged data' >&2
  exit 1
fi
"$REAL_GIT" -C "$tmp/push-failure" diff --cached --name-only | grep -Fx operator-note.txt
"$REAL_GIT" -C "$tmp/push-failure" diff --quiet HEAD -- devhubs/glade-dev-hub.sfdx-auth-url.age

"$REAL_GIT" clone --bare "$tmp/remote.git" "$tmp/acl-remote.git" >/dev/null 2>&1
printf '%s\n' '#!/usr/bin/env bash' 'exit 1' >"$tmp/acl-remote.git/hooks/pre-receive"
chmod +x "$tmp/acl-remote.git/hooks/pre-receive"
"$REAL_GIT" clone "$tmp/acl-remote.git" "$tmp/acl-worker" >/dev/null 2>&1
"$REAL_GIT" -C "$tmp/acl-worker" config user.name Test
"$REAL_GIT" -C "$tmp/acl-worker" config user.email test@example.invalid
"$REAL_GIT" -C "$tmp/acl-worker" config glade.authRole operator
printf '%s\n' unrelated-staged-note >"$tmp/acl-worker/operator-note.txt"
"$REAL_GIT" -C "$tmp/acl-worker" add operator-note.txt
cp "$tmp/acl-worker/devhubs/glade-dev-hub.sfdx-auth-url.age" "$tmp/before-acl-reject.age"
acl_head="$($REAL_GIT --git-dir="$tmp/acl-remote.git" rev-parse HEAD)"
if "$helper" replace --store "$tmp/acl-worker" --alias glade-dev-hub \
  --source-alias acl-rejected --sf-bin "$tmp/bin/sf" 2>"$tmp/acl.err"; then
  echo 'worker-local role bypassed the remote write ACL' >&2
  exit 1
fi
grep -F 'auth store push failed' "$tmp/acl.err"
[[ "$($REAL_GIT --git-dir="$tmp/acl-remote.git" rev-parse HEAD)" == "$acl_head" ]]
[[ "$($REAL_GIT -C "$tmp/acl-worker" rev-parse HEAD)" == "$acl_head" ]]
cmp "$tmp/before-acl-reject.age" "$tmp/acl-worker/devhubs/glade-dev-hub.sfdx-auth-url.age"
"$REAL_GIT" -C "$tmp/acl-worker" diff --cached --name-only | grep -Fx operator-note.txt

"$REAL_GIT" clone "$tmp/remote.git" "$tmp/tampered-worker" >/dev/null 2>&1
"$REAL_GIT" -C "$tmp/tampered-worker" config user.name Test
"$REAL_GIT" -C "$tmp/tampered-worker" config user.email test@example.invalid
printf '%s\n' unexpected >"$tmp/tampered-worker/devhubs/.hidden.sfdx-auth-url.age"
"$REAL_GIT" -C "$tmp/tampered-worker" add devhubs/.hidden.sfdx-auth-url.age
"$REAL_GIT" -C "$tmp/tampered-worker" commit -m unexpected >/dev/null
if "$helper" login --store "$tmp/tampered-worker" --alias glade-dev-hub \
  --identity "$identity" --state "$tmp/tampered-state" --sf-bin "$tmp/bin/sf" \
  2>"$tmp/tampered.err"; then
  echo 'worker accepted tracked non-ciphertext content' >&2
  exit 1
fi
grep -F 'auth store integrity failed' "$tmp/tampered.err"
[[ "$(cat "$tmp/tampered-state/pause")" == store-integrity ]]
"$REAL_GIT" -C "$tmp/tampered-worker" config glade.authRole operator
if "$helper" rewrap-all --store "$tmp/tampered-worker" --identity "$identity" \
  2>"$tmp/tampered-rewrap.err"; then
  echo 'rewrap-all omitted a tracked hidden ciphertext' >&2
  exit 1
fi
grep -F 'auth store integrity failed' "$tmp/tampered-rewrap.err"

before_failure="$(find "$tmp/store/devhubs" -maxdepth 1 -type f -print | sort)"
cp "$tmp/store/devhubs/glade-dev-hub.sfdx-auth-url.age" "$tmp/before-failed-replace.age"
if FAKE_AGE_FAIL=1 "$helper" replace --store "$tmp/store" --alias glade-dev-hub \
  --source-alias replacement-dev-hub --sf-bin "$tmp/bin/sf" 2>"$tmp/encrypt.err"; then
  echo 'replace accepted encryption failure' >&2
  exit 1
fi
grep -F 'Dev Hub encryption failed: glade-dev-hub' "$tmp/encrypt.err"
[[ "$before_failure" == "$(find "$tmp/store/devhubs" -maxdepth 1 -type f -print | sort)" ]]
cmp "$tmp/before-failed-replace.age" "$tmp/store/devhubs/glade-dev-hub.sfdx-auth-url.age"
! find "$tmp/store/devhubs" -maxdepth 1 \( -name '.auth.*' -o -name '.rewrap.*' \) -print | grep .

"$helper" login --store "$tmp/worker" --alias glade-dev-hub \
  --identity "$identity" --state "$state" --sf-bin "$tmp/bin/sf" >"$tmp/login.out" 2>"$tmp/login.err"
grep -F 'org login sfdx-url --alias glade-dev-hub --set-default-dev-hub --json --sfdx-url-stdin' "$tmp/logs/sf.log"
[[ "$(cat "$state/aliases/glade-dev-hub.health")" == healthy ]]
[[ ! -e "$state/aliases/glade-dev-hub.quarantine" ]]
expected_hash="$(cat "$state/aliases/glade-dev-hub.hash")"
login_count="$(grep -c 'org login sfdx-url' "$tmp/logs/sf.log")"
"$helper" login --store "$tmp/worker" --alias glade-dev-hub \
  --identity "$identity" --state "$state" --expected-alias-hash "$expected_hash" \
  --sf-bin "$tmp/bin/sf" >"$tmp/login-cached.out"
[[ "$(grep -c 'org login sfdx-url' "$tmp/logs/sf.log")" == "$login_count" ]]

if FAKE_SF_FAIL=1 "$helper" login --store "$tmp/worker" --alias glade-dev-hub \
  --identity "$identity" --state "$state" --sf-bin "$tmp/bin/sf" 2>"$tmp/sf.err"; then
  echo 'login accepted Salesforce failure' >&2
  exit 1
fi
grep -F 'Dev Hub login failed: glade-dev-hub' "$tmp/sf.err"
[[ "$(cat "$state/aliases/glade-dev-hub.health")" == failed ]]
[[ "$(cat "$state/aliases/glade-dev-hub.quarantine")" == login ]]
[[ ! -e "$state/pause" ]]

"$helper" verify --store "$tmp/worker" --alias glade-dev-hub \
  --identity "$identity" --state "$state" --sf-bin "$tmp/bin/sf" >"$tmp/verify-before-pause.out"
[[ ! -e "$state/aliases/glade-dev-hub.quarantine" ]]

if FAKE_GIT_PULL_FAIL=1 "$helper" login --store "$tmp/worker" --alias glade-dev-hub \
  --identity "$identity" --state "$tmp/pull-state" --sf-bin "$tmp/bin/sf" 2>"$tmp/pull.err"; then
  echo 'login accepted store pull failure' >&2
  exit 1
fi
grep -F 'auth store unavailable' "$tmp/pull.err"
[[ ! -e "$tmp/pull-state/pause" ]]

if "$helper" login --store "$tmp/worker" --alias glade-dev-hub \
  --identity "$identity" --state "$tmp/hash-state" --expected-alias-hash deadbeef \
  --sf-bin "$tmp/bin/sf" 2>"$tmp/hash.err"; then
  echo 'login accepted an alias hash mismatch' >&2
  exit 1
fi
grep -F 'auth store integrity failed' "$tmp/hash.err"
[[ "$(cat "$tmp/hash-state/pause")" == store-integrity ]]

if "$helper" login --store "$tmp/worker" --alias absent-dev-hub \
  --identity "$identity" --state "$tmp/absent-state" --expected-alias-hash deadbeef \
  --sf-bin "$tmp/bin/sf" 2>"$tmp/absent.err"; then
  echo 'login accepted a missing expected alias' >&2
  exit 1
fi
grep -F 'auth store integrity failed' "$tmp/absent.err"
[[ "$(cat "$tmp/absent-state/pause")" == store-integrity ]]

if FAKE_AGE_FAIL=1 "$helper" login --store "$tmp/worker" --alias second-dev-hub \
  --identity "$identity" --state "$state" --sf-bin "$tmp/bin/sf" 2>"$tmp/decrypt.err"; then
  echo 'login accepted decryption failure' >&2
  exit 1
fi
grep -F 'auth store integrity failed' "$tmp/decrypt.err"
[[ "$(cat "$state/pause")" == store-integrity ]]
[[ ! -e "$state/aliases/second-dev-hub.quarantine" ]]

login_count="$(grep -c 'org login sfdx-url' "$tmp/logs/sf.log")"
display_count="$(grep -c 'org display' "$tmp/logs/sf.log")"
if "$helper" verify --store "$tmp/worker" --alias glade-dev-hub \
  --identity "$identity" --state "$state" --sf-bin "$tmp/bin/sf" 2>"$tmp/paused.err"; then
  echo 'verify executed while the auth store was paused' >&2
  exit 1
fi
grep -F 'auth store paused' "$tmp/paused.err"
[[ "$(grep -c 'org login sfdx-url' "$tmp/logs/sf.log")" == "$login_count" ]]
[[ "$(grep -c 'org display' "$tmp/logs/sf.log")" == "$display_count" ]]
[[ "$(cat "$state/pause")" == store-integrity ]]
[[ ! -e "$state/aliases/glade-dev-hub.quarantine" ]]

current_store_hash="$($helper list --store "$tmp/worker" | awk '$1 == "store" {print $2}')"
"$helper" list --store "$tmp/worker" --state "$state" \
  --expected-store-hash "$current_store_hash" >"$tmp/list-ack.out"
[[ ! -e "$state/pause" ]]

"$helper" verify --store "$tmp/worker" --alias glade-dev-hub \
  --identity "$identity" --state "$state" --sf-bin "$tmp/bin/sf" >"$tmp/verify.out" 2>"$tmp/verify.err"
grep -Fx 'verified Dev Hub: glade-dev-hub' "$tmp/verify.out"

if FAKE_SF_VERIFY_FAIL=1 "$helper" verify --store "$tmp/worker" --alias glade-dev-hub \
  --identity "$identity" --state "$state" --sf-bin "$tmp/bin/sf" 2>"$tmp/quota.err"; then
  echo 'verify accepted Salesforce failure' >&2
  exit 1
fi
grep -F 'Dev Hub verification failed: glade-dev-hub' "$tmp/quota.err"
[[ "$(cat "$state/aliases/glade-dev-hub.quarantine")" == quota ]]
[[ ! -e "$state/pause" ]]

if grep -R -F 'force://' "$tmp/logs" "$tmp"/*.out "$tmp"/*.err "$tmp/store/recipients.txt" "$state"; then
  echo 'plaintext auth URL escaped the encrypted pipeline' >&2
  exit 1
fi
if grep -R -E 'TEST-SECRET|ROTATED-SECRET' "$state" "$tmp/logs" "$tmp"/*.out "$tmp"/*.err; then
  echo 'credential value escaped into state or logs' >&2
  exit 1
fi
if grep -F "$tmp" "$tmp"/*.err; then
  echo 'private path escaped into stderr' >&2
  exit 1
fi
if "$REAL_GIT" -C "$tmp/store" ls-files | grep -Ev '^(recipients\.txt|devhubs/[A-Za-z0-9][A-Za-z0-9._-]*\.sfdx-auth-url\.age)$'; then
  echo 'auth store tracked non-ciphertext state' >&2
  exit 1
fi

echo 'Dev Hub auth helper tests passed'
