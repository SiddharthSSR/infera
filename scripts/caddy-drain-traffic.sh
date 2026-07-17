#!/usr/bin/env bash
# Replace public application routes with a fail-closed maintenance response.

set -euo pipefail

MANIFEST="${1:?usage: caddy-drain-traffic.sh <release.manifest>}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=recovery-adapter-common.sh
source "${SCRIPT_DIR}/recovery-adapter-common.sh"
recovery_manifest_value "${MANIFEST}" INFERA_RELEASE_ID >/dev/null
: "${INFERA_BASE_URL:?public application origin is required}"
: "${INFERA_DASHBOARD_URL:?public dashboard origin is required}"
APP_HOST="$(recovery_https_host "${INFERA_BASE_URL}")"
DASHBOARD_HOST="$(recovery_https_host "${INFERA_DASHBOARD_URL}")"
WWW_HOST="${INFERA_WWW_HOST:-www.${APP_HOST}}"
[[ "${WWW_HOST}" =~ ^[A-Za-z0-9.-]+$ ]] || { echo "ERROR: invalid public redirect host" >&2; exit 2; }

COMPOSE_FILE="${COMPOSE_FILE:-docker-compose.prod.yml}"
TMP_FILE="$(mktemp)"
trap 'rm -f "${TMP_FILE}"' EXIT
cat >"${TMP_FILE}" <<CADDY
${WWW_HOST} {
  redir https://${APP_HOST}{uri} permanent
}

${APP_HOST} {
  header {
    Strict-Transport-Security "max-age=31536000; includeSubDomains; preload"
    X-Content-Type-Options "nosniff"
    X-Frame-Options "SAMEORIGIN"
    Referrer-Policy "strict-origin-when-cross-origin"
    Retry-After "60"
    -Server
  }
  @workerTokenControl {
    path /api/workers/register /api/workers/heartbeat
    header X-Worker-Token *
  }
  @workerBearerControl {
    path /api/workers/register /api/workers/heartbeat
    header_regexp Authorization ^Bearer[[:space:]]+[^[:space:]]+$
  }

  handle @workerTokenControl {
    reverse_proxy gateway:8080
  }
  handle @workerBearerControl {
    reverse_proxy gateway:8080
  }
  handle {
    respond "Service temporarily unavailable" 503
  }
}

${DASHBOARD_HOST} {
  reverse_proxy grafana:3000
}
CADDY

CADDY_ID="$(docker compose -f "${COMPOSE_FILE}" ps -q caddy)"
[[ -n "${CADDY_ID}" ]]
docker cp "${TMP_FILE}" "${CADDY_ID}:/tmp/infera-maintenance.Caddyfile"
docker compose -f "${COMPOSE_FILE}" exec -T caddy \
  caddy reload --config /tmp/infera-maintenance.Caddyfile --adapter caddyfile

STATUS="$(curl --silent --show-error --output /dev/null --write-out '%{http_code}' \
  --max-time 15 "${INFERA_BASE_URL}/health")"
[[ "${STATUS}" == "503" ]] || {
  echo "ERROR: public ingress did not enter the maintenance state" >&2
  exit 1
}
