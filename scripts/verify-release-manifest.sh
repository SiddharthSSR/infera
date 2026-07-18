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

value() {
  awk -F= -v wanted="$2" '$1 == wanted { count++; value=substr($0, index($0, "=") + 1) } END { if (count != 1) exit 1; print value }' "$1"
}

INFERA_RELEASE_ID="$(value "${MANIFEST}" INFERA_RELEASE_ID)"
INFERA_WORKER_PROTOCOL_VERSION="$(value "${MANIFEST}" INFERA_WORKER_PROTOCOL_VERSION)"
INFERA_RECOVERY_API_PROTOCOL_VERSION="$(value "${MANIFEST}" INFERA_RECOVERY_API_PROTOCOL_VERSION)"
export INFERA_RELEASE_ID INFERA_WORKER_PROTOCOL_VERSION INFERA_RECOVERY_API_PROTOCOL_VERSION

if [[ "${INFERA_EXPECT_TRAFFIC_DRAINED:-0}" == "1" ]]; then
  [[ -z "${INFERA_GATEWAY_INTERNAL_URL:-}" ]] || {
    echo "INFERA_GATEWAY_INTERNAL_URL cannot override drained replica verification" >&2
    exit 1
  }
  gateway_urls=()
  gateway_urls_output="$(recovery_gateway_urls)" || {
    echo "unable to enumerate every configured gateway replica" >&2
    exit 1
  }
  while IFS= read -r gateway_url; do
    [[ -n "${gateway_url}" ]] && gateway_urls+=("${gateway_url}")
  done <<<"${gateway_urls_output}"
  [[ "${#gateway_urls[@]}" -gt 0 ]]
  for gateway_url in "${gateway_urls[@]}"; do
    INFERA_GATEWAY_INTERNAL_URL="${gateway_url}" \
      "$(dirname "$0")/release-verify.sh" "${BASE_URL}"
  done
  exit 0
fi

exec "$(dirname "$0")/release-verify.sh" "${BASE_URL}"
