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
GLADE_ROOT="$(cd "${GLADE_ROOT}" && pwd)"
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

# --- bind both candidates to clean source commits ---
GLADE_HEAD="$(git -C "${GLADE_ROOT}" rev-parse HEAD)"
TOOLS_HEAD="$(git -C "${REPO_ROOT}" rev-parse HEAD)"
if [[ -n "$(git -C "${GLADE_ROOT}" status --porcelain)" ]]; then
  echo "glade worktree must be clean" >&2
  exit 1
fi
if [[ -n "$(git -C "${REPO_ROOT}" status --porcelain)" ]]; then
  echo "glade-tools worktree must be clean" >&2
  exit 1
fi

binary_vcs_setting() {
  local binary="$1" setting="$2"
  go version -m "${binary}" 2>/dev/null | sed -n "s/^[[:space:]]*build[[:space:]]*vcs\\.${setting}=//p"
}

TOOLS_BIN_REVISION="$(binary_vcs_setting "${TOOLS_BIN}" revision)"
TOOLS_BIN_MODIFIED="$(binary_vcs_setting "${TOOLS_BIN}" modified)"
GLADE_BIN_REVISION="$(binary_vcs_setting "${GLADE_BIN}" revision)"
GLADE_BIN_MODIFIED="$(binary_vcs_setting "${GLADE_BIN}" modified)"
if [[ "${TOOLS_BIN_REVISION}" != "${TOOLS_HEAD}" || "${TOOLS_BIN_MODIFIED}" != "false" ]]; then
  echo "glade-tools binary is not built from clean commit ${TOOLS_HEAD}" >&2
  exit 1
fi
if [[ "${GLADE_BIN_REVISION}" != "${GLADE_HEAD}" || "${GLADE_BIN_MODIFIED}" != "false" ]]; then
  echo "glade candidate is not built from clean commit ${GLADE_HEAD}" >&2
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
TOOLS_BIN_SHA256="$(shasum -a 256 "${TOOLS_BIN}" | awk '{print $1}')"
GLADE_BIN_SHA256="$(shasum -a 256 "${GLADE_BIN}" | awk '{print $1}')"
PROVENANCE_OUT="${OUT_DIR}/candidate-provenance.json"
jq -n \
  --arg gladeCommit "${GLADE_HEAD}" \
  --arg gladeSHA256 "${GLADE_BIN_SHA256}" \
  --arg toolsCommit "${TOOLS_HEAD}" \
  --arg toolsSHA256 "${TOOLS_BIN_SHA256}" \
  '{schemaVersion: 1,
    glade: {commit: $gladeCommit, modified: false, sha256: $gladeSHA256},
    gladeTools: {commit: $toolsCommit, modified: false, sha256: $toolsSHA256}}' > "${PROVENANCE_OUT}"

# --- step 1: generated release data and exact Glade product tests ---
echo "salesforce release check..." >&2
"${TOOLS_BIN}" salesforce release \
  --contract docs/fixtures/salesforce-release-contract.json \
  --glade-root "${GLADE_ROOT}" \
  --check || { echo "release generation check failed" >&2; exit 1; }

echo "installing LWC compiler..." >&2
npm ci --prefix "${GLADE_ROOT}/third_party/lwc"
PRODUCT_TEST_EVENTS="${OUT_DIR}/product-tests.jsonl"
PRODUCT_TEST_EVIDENCE="${OUT_DIR}/product-test-evidence/validation.json"
echo "Glade product tests..." >&2
env LC_ALL=C \
  GLADE_LWC_COMPILE=1 \
  GLADE_ROOT="${GLADE_ROOT}" \
  scripts/salesforce-product-tests.sh "${GLADE_ROOT}" "${OUT_DIR}"

PRODUCT_TEST_EVENTS_SHA256="$(shasum -a 256 "${PRODUCT_TEST_EVENTS}" | awk '{print $1}')"
PRODUCT_TEST_EVIDENCE_SHA256="$(shasum -a 256 "${PRODUCT_TEST_EVIDENCE}" | awk '{print $1}')"
PRODUCT_VERSION_PROOF="${OUT_DIR}/product-version-proof.json"
jq -n \
  --arg gladeCommit "${GLADE_HEAD}" \
  --arg gladeRoot "${GLADE_ROOT}" \
  --arg outDir "${OUT_DIR}" \
  --arg testEventsSHA256 "${PRODUCT_TEST_EVENTS_SHA256}" \
  --arg executionEvidenceSHA256 "${PRODUCT_TEST_EVIDENCE_SHA256}" \
  '{schemaVersion: 2,
    gladeCommit: $gladeCommit,
    status: "pass",
    command: ["env", "LC_ALL=C", "GLADE_LWC_COMPILE=1", ("GLADE_ROOT=" + $gladeRoot), "scripts/salesforce-product-tests.sh", $gladeRoot, $outDir],
    testEvents: "product-tests.jsonl",
    testEventsSHA256: $testEventsSHA256,
    executionEvidence: "product-test-evidence/validation.json",
    executionEvidenceSHA256: $executionEvidenceSHA256}' > "${PRODUCT_VERSION_PROOF}"

# --- step 2: salesforce verify ---
VERIFIER_OUT="${OUT_DIR}/salesforce-verification.json"
echo "salesforce verify..." >&2
"${TOOLS_BIN}" salesforce verify \
  --release-contract docs/fixtures/salesforce-release-contract.json \
  --product-version-proof "${PRODUCT_VERSION_PROOF}" \
  --catalog docs/fixtures/apex-language-rules.json \
  --runtime-cases docs/fixtures/salesforce-runtime-correctness.json \
  --test-project testdata/salesforce-correctness \
  --target-org "${TARGET_ORG}" \
  --glade-bin "${GLADE_BIN}" \
  --glade-root "${GLADE_ROOT}" \
  --out "${VERIFIER_OUT}" || { echo "verifier command failed" >&2; exit 1; }

# --- validate verifier and product-test artifacts ---
jq -e \
  --arg gladeHead "${GLADE_HEAD}" \
  --arg toolsHead "${TOOLS_HEAD}" \
  '.schemaVersion == 2
   and .status == "pass"
   and .glade.commit == $gladeHead
   and .glade.dirty == false
   and .gladeTools.commit == $toolsHead
   and .gladeTools.dirty == false
   and .candidate.sha256Before == .candidate.sha256After
   and (.candidate.sha256Before | length) > 0
   and .summary.required == .summary.pass
   and .summary.fail == 0
   and .summary.inconclusive == 0
   and .compiler.summary.required == .compiler.summary.pass
   and .runtime.summary.required == .runtime.summary.pass
   and .lifecycle.summary.required == .lifecycle.summary.pass
   and .releaseCompleteness.status == "pass"
   and .releaseCompleteness.surfaceDelta.total == .releaseCompleteness.surfaceDelta.classified
   and .releaseCompleteness.surfaceDelta.total == .releaseCompleteness.surfaceDelta.proved
   and .releaseCompleteness.surfaceDelta.total == (.releaseCompleteness.surfaceDelta.implemented + .releaseCompleteness.surfaceDelta.explicitNonParity)
   and .releaseCompleteness.behaviorDelta.total == .releaseCompleteness.behaviorDelta.classified
   and .releaseCompleteness.behaviorDelta.total == .releaseCompleteness.behaviorDelta.proved
   and .releaseCompleteness.behaviorDelta.total == (.releaseCompleteness.behaviorDelta.implemented + .releaseCompleteness.behaviorDelta.explicitNonParity)
   and .releaseCompleteness.changeInventory.total == .releaseCompleteness.changeInventory.routed
   and .releaseCompleteness.sourceVersions.advertised == .releaseCompleteness.sourceVersions.passing
   and .releaseCompleteness.endpointVersions.advertised == .releaseCompleteness.endpointVersions.passing
   and .releaseCompleteness.orgProfiles.advertised == .releaseCompleteness.orgProfiles.passing
   and .releaseCompleteness.silentFallbacks == 0
   and (.releaseCompleteness.unclassified // [] | length) == 0' \
  "${VERIFIER_OUT}" >/dev/null || { echo "verifier artifact validation failed" >&2; exit 1; }
jq -e --arg eventsSHA256 "${PRODUCT_TEST_EVENTS_SHA256}" --arg evidenceSHA256 "${PRODUCT_TEST_EVIDENCE_SHA256}" \
  '.schemaVersion == 2
   and .status == "pass"
   and .testEvents == "product-tests.jsonl"
   and .testEventsSHA256 == $eventsSHA256
   and .executionEvidence == "product-test-evidence/validation.json"
   and .executionEvidenceSHA256 == $evidenceSHA256' \
  "${PRODUCT_VERSION_PROOF}" >/dev/null || { echo "product-version proof validation failed" >&2; exit 1; }

# --- step 3: corpus check ---
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
shasum -a 256 product-tests.jsonl >> "${CHECKSUMS_FILE}"
shasum -a 256 product-test-evidence/validation.json >> "${CHECKSUMS_FILE}"
shasum -a 256 product-version-proof.json >> "${CHECKSUMS_FILE}"
shasum -a 256 candidate-provenance.json >> "${CHECKSUMS_FILE}"
for f in corpus/*.tsv; do
  [[ -f "$f" ]] && shasum -a 256 "$f" >> "${CHECKSUMS_FILE}"
done
sort -k 2 "${CHECKSUMS_FILE}" > "${CHECKSUMS_FILE}.tmp" && mv "${CHECKSUMS_FILE}.tmp" "${CHECKSUMS_FILE}"
cd "${REPO_ROOT}"

echo "Gate passed." >&2
