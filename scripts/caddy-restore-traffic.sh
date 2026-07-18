#!/usr/bin/env bash
# Restore the repository-owned public ingress configuration.

set -euo pipefail

MANIFEST="${1:?usage: caddy-restore-traffic.sh <release.manifest>}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=recovery-adapter-common.sh
source "${SCRIPT_DIR}/recovery-adapter-common.sh"
RELEASE_ID="$(recovery_manifest_value "${MANIFEST}" INFERA_RELEASE_ID)"
WORKER_PROTOCOL="$(recovery_manifest_value "${MANIFEST}" INFERA_WORKER_PROTOCOL_VERSION)"
RECOVERY_PROTOCOL="$(recovery_manifest_value "${MANIFEST}" INFERA_RECOVERY_API_PROTOCOL_VERSION)"

COMPOSE_FILE="${COMPOSE_FILE:-docker-compose.prod.yml}"
TRAFFIC_OPEN=0
restore_maintenance_on_failure() {
  local status="$?"
  local maintenance_status=""
  local close_ingress=0
  trap - EXIT
  if [[ "${status}" -ne 0 && "${TRAFFIC_OPEN}" == "1" ]]; then
    echo "ERROR: public validation failed; restoring maintenance ingress" >&2
    if docker compose -f "${COMPOSE_FILE}" exec -T caddy \
      caddy reload --config /tmp/infera-maintenance.Caddyfile --adapter caddyfile >/dev/null 2>&1; then
      maintenance_status="$(curl --silent --show-error --output /dev/null --write-out '%{http_code}' \
        --max-time 15 "${INFERA_BASE_URL:-https://inferai.co.in}/health" 2>/dev/null || true)"
      [[ "${maintenance_status}" == "503" ]] || close_ingress=1
    else
      close_ingress=1
    fi
    if [[ "${close_ingress}" == "1" ]]; then
      echo "ERROR: maintenance ingress could not be proven; stopping Caddy" >&2
      if ! docker compose -f "${COMPOSE_FILE}" stop caddy; then
        echo "CRITICAL: failed to stop unverified public ingress" >&2
      fi
    fi
  fi
  exit "${status}"
}
trap restore_maintenance_on_failure EXIT

docker compose -f "${COMPOSE_FILE}" exec -T caddy \
  caddy reload --config /etc/caddy/Caddyfile --adapter caddyfile
TRAFFIC_OPEN=1

HEALTH_BODY="$(curl --fail --silent --show-error --max-time 15 \
  "${INFERA_BASE_URL:-https://inferai.co.in}/health")"
HEALTH_BODY="${HEALTH_BODY}" RELEASE_ID="${RELEASE_ID}" WORKER_PROTOCOL="${WORKER_PROTOCOL}" RECOVERY_PROTOCOL="${RECOVERY_PROTOCOL}" python3 - <<'PY'
import json
import os

payload = json.loads(os.environ["HEALTH_BODY"])
if payload.get("release_id") != os.environ["RELEASE_ID"]:
    raise SystemExit("public ingress reached an unexpected gateway release")
if payload.get("worker_protocol_version") != os.environ["WORKER_PROTOCOL"]:
    raise SystemExit("public ingress reached an unexpected worker protocol")
if payload.get("recovery_api_protocol_version") != os.environ["RECOVERY_PROTOCOL"]:
    raise SystemExit("public ingress reached an unexpected recovery API protocol")
PY
TRAFFIC_OPEN=0
trap - EXIT
