#!/usr/bin/env bash
# Run release verification using the rollout identity in a non-secret release manifest.

set -euo pipefail

MANIFEST="${1:?usage: verify-release-manifest.sh <release.manifest> [base-url]}"
BASE_URL="${2:-${INFERA_BASE_URL:-https://inferai.co.in}}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=recovery-adapter-common.sh
source "${SCRIPT_DIR}/recovery-adapter-common.sh"
: "${INFERA_SMOKE_MODEL:?INFERA_SMOKE_MODEL is required for recovery verification}"
export INFERA_SMOKE_STREAM=1
export SKIP_CHAT_CHECKS=0

if [[ "${INFERA_EXPECT_TRAFFIC_DRAINED:-0}" == "1" && -z "${INFERA_GATEWAY_INTERNAL_URL:-}" ]]; then
  INFERA_GATEWAY_INTERNAL_URL="$(recovery_gateway_url)"
  export INFERA_GATEWAY_INTERNAL_URL
fi

value() {
  awk -F= -v wanted="$2" '$1 == wanted { count++; value=substr($0, index($0, "=") + 1) } END { if (count != 1) exit 1; print value }' "$1"
}

INFERA_RELEASE_ID="$(value "${MANIFEST}" INFERA_RELEASE_ID)"
INFERA_WORKER_PROTOCOL_VERSION="$(value "${MANIFEST}" INFERA_WORKER_PROTOCOL_VERSION)"
export INFERA_RELEASE_ID INFERA_WORKER_PROTOCOL_VERSION
exec "$(dirname "$0")/release-verify.sh" "${BASE_URL}"
