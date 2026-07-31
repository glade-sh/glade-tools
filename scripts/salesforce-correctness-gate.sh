#!/usr/bin/env bash
set -euo pipefail

# --- usage ---
if [[ $# -ne 5 ]]; then
  echo "usage: $0 <glade-tools-bin> <glade-root> <glade-bin> <target-org> <out-dir>" >&2
  exit 1
fi

TOOLS_BIN="$1"
GLADE_ROOT="$2"
GLADE_BIN="$3"
TARGET_ORG="$4"
OUT_DIR="$5"

# --- resolve repository root and change directory ---
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${REPO_ROOT}"

# --- validate inputs ---
if [[ ! -x "${TOOLS_BIN}" ]]; then
  echo "glade-tools binary not found or not executable: ${TOOLS_BIN}" >&2
  exit 1
fi
if ! git -C "${GLADE_ROOT}" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  echo "glade git worktree not found: ${GLADE_ROOT}" >&2
  exit 1
fi
if [[ ! -x "${GLADE_BIN}" ]]; then
  echo "glade candidate binary not found or not executable: ${GLADE_BIN}" >&2
  exit 1
fi
if [[ -z "${TARGET_ORG}" ]]; then
  echo "target org must not be blank" >&2
  exit 1
fi
if [[ -z "${OUT_DIR}" ]]; then
  echo "output directory must not be blank" >&2
  exit 1
fi

# --- require out dir absent or an empty directory ---
if [[ -e "${OUT_DIR}" ]]; then
  if [[ ! -d "${OUT_DIR}" ]]; then
    echo "output path is not a directory: ${OUT_DIR}" >&2
    exit 1
  fi
  if [[ -n "$(ls -A "${OUT_DIR}" 2>/dev/null)" ]]; then
    echo "output directory must be empty: ${OUT_DIR}" >&2
    exit 1
  fi
fi
mkdir -p "${OUT_DIR}"
OUT_DIR="$(cd "${OUT_DIR}" && pwd)"
CORPUS_DIR="${OUT_DIR}/corpus"
mkdir -p "${CORPUS_DIR}"

# --- provenance ---
GLADE_HEAD="$(git -C "${GLADE_ROOT}" rev-parse HEAD)"
TOOLS_HEAD="$(git -C "${REPO_ROOT}" rev-parse HEAD)"

# --- step 1: salesforce verify ---
VERIFIER_OUT="${OUT_DIR}/salesforce-verification.json"
echo "salesforce verify..." >&2
"${TOOLS_BIN}" salesforce verify \
  --release-manifest docs/fixtures/salesforce-release-current.json \
  --catalog docs/fixtures/apex-language-rules.json \
  --runtime-cases docs/fixtures/salesforce-runtime-correctness.json \
  --test-project testdata/salesforce-correctness \
  --target-org "${TARGET_ORG}" \
  --glade-bin "${GLADE_BIN}" \
  --glade-root "${GLADE_ROOT}" \
  --out "${VERIFIER_OUT}" || { echo "verifier command failed" >&2; exit 1; }

# --- validate verifier artifact (one jq -e expression) ---
jq -e \
  --arg gladeHead "${GLADE_HEAD}" \
  --arg toolsHead "${TOOLS_HEAD}" \
  '.status == "pass"
   and .glade.commit == $gladeHead
   and .glade.dirty == false
   and .gladeTools.commit == $toolsHead
   and .gladeTools.dirty == false
   and .candidate.sha256Before == .candidate.sha256After
   and (.candidate.sha256Before | length) > 0
   and .summary.pass == 486
   and .summary.fail == 0
   and .summary.inconclusive == 0
   and .compiler.summary.pass == 422
   and .runtime.summary.pass == 52
   and .lifecycle.summary.pass == 12' \
  "${VERIFIER_OUT}" >/dev/null || { echo "verifier artifact validation failed" >&2; exit 1; }

# --- step 2: corpus check ---
echo "corpus check..." >&2
"${TOOLS_BIN}" corpus check \
  --root testdata/salesforce-correctness \
  --glade "${GLADE_BIN}" \
  --out "${CORPUS_DIR}" \
  --fail-on-unclassified \
  --max-unclassified 0 \
  --fail-on-check-closure || { echo "corpus command failed" >&2; exit 1; }

# --- validate corpus summary (one awk program) ---
awk -F'\t' '
  NR == 1 { next }
  NF != 5 { err=1; exit }
  { count++ }
  $1 != "salesforce-correctness" { err=1; exit }
  $3 != "0" { err=1; exit }
  $4 != "0" { err=1; exit }
  $5 != ""  { err=1; exit }
  END { if (count != 1 || err) exit 1; exit 0 }
' "${CORPUS_DIR}/summary.tsv" || { echo "corpus summary validation failed" >&2; exit 1; }

# --- checksums ---
echo "writing checksums..." >&2
CHECKSUMS_FILE="${OUT_DIR}/SHA256SUMS.txt"
rm -f "${CHECKSUMS_FILE}"
cd "${OUT_DIR}"
shasum -a 256 salesforce-verification.json >> "${CHECKSUMS_FILE}"
for f in corpus/*.tsv; do
  [[ -f "$f" ]] && shasum -a 256 "$f" >> "${CHECKSUMS_FILE}"
done
sort -k 2 "${CHECKSUMS_FILE}" > "${CHECKSUMS_FILE}.tmp" && mv "${CHECKSUMS_FILE}.tmp" "${CHECKSUMS_FILE}"
cd "${REPO_ROOT}"

echo "Gate passed." >&2
