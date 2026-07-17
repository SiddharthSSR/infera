#!/usr/bin/env bash
# Production Compose adapter for release-recovery.sh. Worker lifecycle remains provider-specific.

set -euo pipefail

ACTION="${1:?usage: compose-release-driver.sh <action> <release.manifest>}"
MANIFEST="${2:?usage: compose-release-driver.sh <action> <release.manifest>}"
COMPOSE_FILE="${COMPOSE_FILE:-docker-compose.prod.yml}"

value() {
  awk -F= -v wanted="$2" '$1 == wanted { count++; value=substr($0, index($0, "=") + 1) } END { if (count != 1) exit 1; print value }' "$1"
}

configured_gateway_replicas() {
  if [[ -n "${INFERA_GATEWAY_REPLICAS:-}" ]]; then
    printf '%s\n' "${INFERA_GATEWAY_REPLICAS}"
    return
  fi
  local env_file="${ENV_FILE:-.env}"
  if [[ -f "${env_file}" ]]; then
    awk -F= '$1 == "INFERA_GATEWAY_REPLICAS" { count++; value=substr($0, index($0, "=") + 1) } END { if (count == 1 && value != "") print value; else print "1" }' "${env_file}"
    return
  fi
  printf '1\n'
}

require_single_gateway_recovery_topology() {
  local replicas
  replicas="$(configured_gateway_replicas)"
  [[ "${replicas}" == "1" ]] || {
    echo "ERROR: recovery adapter requires one gateway until worker credentials and router registration use shared durable state" >&2
    return 1
  }
}

INFERA_RELEASE_ID="$(value "${MANIFEST}" INFERA_RELEASE_ID)"
INFERA_GATEWAY_IMAGE="$(value "${MANIFEST}" INFERA_GATEWAY_IMAGE)"
INFERA_WORKER_IMAGE="$(value "${MANIFEST}" INFERA_WORKER_IMAGE)"
INFERA_WORKER_PROTOCOL_VERSION="$(value "${MANIFEST}" INFERA_WORKER_PROTOCOL_VERSION)"
export INFERA_RELEASE_ID INFERA_GATEWAY_IMAGE INFERA_WORKER_IMAGE INFERA_WORKER_PROTOCOL_VERSION

case "${ACTION}" in
  preflight)
    require_single_gateway_recovery_topology
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
    require_single_gateway_recovery_topology
    docker compose -f "${COMPOSE_FILE}" up -d --no-deps --scale gateway=1 gateway
    gateway_id="$(docker compose -f "${COMPOSE_FILE}" ps -q gateway)"
    [[ -n "${gateway_id}" && "${gateway_id}" != *$'\n'* ]]
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
