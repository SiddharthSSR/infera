#!/usr/bin/env bash
# Production Compose adapter for release-recovery.sh. Worker lifecycle remains provider-specific.

set -euo pipefail

ACTION="${1:?usage: compose-release-driver.sh <action> <release.manifest>}"
MANIFEST="${2:?usage: compose-release-driver.sh <action> <release.manifest>}"
COMPOSE_FILE="${COMPOSE_FILE:-docker-compose.prod.yml}"

value() {
  awk -F= -v wanted="$2" '$1 == wanted { count++; value=substr($0, index($0, "=") + 1) } END { if (count != 1) exit 1; print value }' "$1"
}

export INFERA_RELEASE_ID="$(value "${MANIFEST}" INFERA_RELEASE_ID)"
export INFERA_GATEWAY_IMAGE="$(value "${MANIFEST}" INFERA_GATEWAY_IMAGE)"
export INFERA_WORKER_IMAGE="$(value "${MANIFEST}" INFERA_WORKER_IMAGE)"
export INFERA_WORKER_PROTOCOL_VERSION="$(value "${MANIFEST}" INFERA_WORKER_PROTOCOL_VERSION)"

case "${ACTION}" in
  preflight)
    for executable_name in \
      INFERA_STOP_WORKERS_EXECUTABLE \
      INFERA_DEPLOY_WORKERS_EXECUTABLE \
      INFERA_DRAIN_TRAFFIC_EXECUTABLE \
      INFERA_RESTORE_TRAFFIC_EXECUTABLE; do
      executable_path="${!executable_name:-}"
      [[ -n "${executable_path}" && -x "${executable_path}" ]] || {
        echo "ERROR: ${executable_name} must name an executable" >&2
        exit 2
      }
    done
    "$(dirname "$0")/validate-prod-env.sh"
    docker compose -f "${COMPOSE_FILE}" config --quiet
    ;;
  stop-workers)
    : "${INFERA_STOP_WORKERS_EXECUTABLE:?provider-specific stop-workers executable is required}"
    [[ -x "${INFERA_STOP_WORKERS_EXECUTABLE}" ]]
    "${INFERA_STOP_WORKERS_EXECUTABLE}" "${MANIFEST}"
    ;;
  deploy-gateway)
    docker compose -f "${COMPOSE_FILE}" up -d --no-deps gateway
    gateway_id="$(docker compose -f "${COMPOSE_FILE}" ps -q gateway)"
    [[ -n "${gateway_id}" ]]
    for _ in $(seq 1 "${INFERA_GATEWAY_HEALTH_ATTEMPTS:-30}"); do
      status="$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "${gateway_id}")"
      [[ "${status}" == "healthy" ]] && exit 0
      [[ "${status}" == "exited" || "${status}" == "unhealthy" ]] && exit 1
      sleep 2
    done
    exit 1
    ;;
  deploy-workers)
    : "${INFERA_DEPLOY_WORKERS_EXECUTABLE:?provider-specific deploy-workers executable is required}"
    [[ -x "${INFERA_DEPLOY_WORKERS_EXECUTABLE}" ]]
    "${INFERA_DEPLOY_WORKERS_EXECUTABLE}" "${MANIFEST}"
    ;;
  drain-traffic)
    : "${INFERA_DRAIN_TRAFFIC_EXECUTABLE:?ingress drain executable is required}"
    [[ -x "${INFERA_DRAIN_TRAFFIC_EXECUTABLE}" ]]
    "${INFERA_DRAIN_TRAFFIC_EXECUTABLE}" "${MANIFEST}"
    ;;
  restore-traffic)
    : "${INFERA_RESTORE_TRAFFIC_EXECUTABLE:?ingress restore executable is required}"
    [[ -x "${INFERA_RESTORE_TRAFFIC_EXECUTABLE}" ]]
    "${INFERA_RESTORE_TRAFFIC_EXECUTABLE}" "${MANIFEST}"
    ;;
  *)
    echo "ERROR: unsupported recovery driver action: ${ACTION}" >&2
    exit 2
    ;;
esac
