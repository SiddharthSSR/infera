#!/usr/bin/env bash
# Run release verification using the rollout identity in a non-secret release manifest.

set -euo pipefail

MANIFEST="${1:?usage: verify-release-manifest.sh <release.manifest> [base-url]}"
BASE_URL="${2:-${INFERA_BASE_URL:-https://inferai.co.in}}"
: "${INFERA_SMOKE_MODEL:?INFERA_SMOKE_MODEL is required for recovery verification}"
export INFERA_SMOKE_STREAM=1
export SKIP_CHAT_CHECKS=0

if [[ "${INFERA_EXPECT_TRAFFIC_DRAINED:-0}" == "1" && -z "${INFERA_GATEWAY_INTERNAL_URL:-}" ]]; then
  COMPOSE_FILE="${COMPOSE_FILE:-docker-compose.prod.yml}"
  GATEWAY_ID="$(docker compose -f "${COMPOSE_FILE}" ps -q gateway | head -1)"
  [[ -n "${GATEWAY_ID}" ]] || { echo "no gateway container is available for drained verification" >&2; exit 1; }
  GATEWAY_IP="$(docker inspect --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "${GATEWAY_ID}")"
  [[ "${GATEWAY_IP}" =~ ^[0-9a-fA-F:.]+$ ]] || { echo "gateway container has no usable internal address" >&2; exit 1; }
  export INFERA_GATEWAY_INTERNAL_URL="http://${GATEWAY_IP}:8080"
fi

value() {
  awk -F= -v wanted="$2" '$1 == wanted { count++; value=substr($0, index($0, "=") + 1) } END { if (count != 1) exit 1; print value }' "$1"
}

export INFERA_RELEASE_ID="$(value "${MANIFEST}" INFERA_RELEASE_ID)"
export INFERA_WORKER_PROTOCOL_VERSION="$(value "${MANIFEST}" INFERA_WORKER_PROTOCOL_VERSION)"
exec "$(dirname "$0")/release-verify.sh" "${BASE_URL}"
