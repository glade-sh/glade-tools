#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SOURCE="${GLADE_APEX_DOCS_SOURCE:-}"

if [[ -z "${SOURCE}" ]]; then
  echo "apex-docs-support: skipped (set GLADE_APEX_DOCS_SOURCE to the scraped Apex docs directory)"
  exit 0
fi

if [[ ! -d "${SOURCE}" ]]; then
  echo "apex-docs-support: source directory not found: ${SOURCE}" >&2
  exit 1
fi

TMP="$(mktemp -d)"
cleanup() {
  rm -rf "${TMP}"
}
trap cleanup EXIT

cd "${ROOT}"

GLADE_TOOLS="${GLADE_TOOLS_BIN:-}"
if [[ -z "${GLADE_TOOLS}" ]]; then
  go build -o "${TMP}/glade-tools" ./cmd/glade-tools
  GLADE_TOOLS="${TMP}/glade-tools"
fi

INVENTORY="${TMP}/apex-docs-inventory.json"
CATALOG="${TMP}/apex-capability-catalog.json"
PRODUCT_NAMESPACES="${TMP}/apex-product-namespaces.json"
EVIDENCE="${TMP}/apex-evidence.txt"

"${GLADE_TOOLS}" docs-inventory --source "${SOURCE}" --output "${INVENTORY}"
"${GLADE_TOOLS}" docs-inventory --source "${SOURCE}" --check "${INVENTORY}"

"${GLADE_TOOLS}" catalog --inventory "${INVENTORY}" --output "${CATALOG}"
"${GLADE_TOOLS}" catalog --inventory "${INVENTORY}" --check "${CATALOG}"

"${GLADE_TOOLS}" product-namespaces --catalog "${CATALOG}" --output "${PRODUCT_NAMESPACES}"
"${GLADE_TOOLS}" product-namespaces --catalog "${CATALOG}" --check "${PRODUCT_NAMESPACES}"

RECONCILE="${TMP}/apex-reconciliation.json"
"${GLADE_TOOLS}" reconcile --catalog "${CATALOG}" --json >"${RECONCILE}"
"${GLADE_TOOLS}" reconcile --catalog "${CATALOG}"

# Ratchet: documented executable-parity/data-platform surfaces must stay at
# least type-known. Set GLADE_APEX_DOCS_MAX_UNKNOWN to the current floor (see
# the `unknown=` count above) to fail the gate when the gap regresses.
if [[ -n "${GLADE_APEX_DOCS_MAX_UNKNOWN:-}" ]]; then
  "${GLADE_TOOLS}" reconcile --catalog "${CATALOG}" --max-unknown "${GLADE_APEX_DOCS_MAX_UNKNOWN}" >/dev/null || {
    echo "apex-docs-support: runtime-target unknown surfaces regressed past ${GLADE_APEX_DOCS_MAX_UNKNOWN}" >&2
    exit 1
  }
fi

"${GLADE_TOOLS}" evidence --catalog "${CATALOG}" docs/fixtures/*.json >"${EVIDENCE}"
grep -q 'unmatchedEvidence: 0' "${EVIDENCE}" || {
  echo "apex-docs-support: fixture evidence references symbols missing from the catalog" >&2
  cat "${EVIDENCE}" >&2
  exit 1
}

echo "apex-docs-support: ok"
