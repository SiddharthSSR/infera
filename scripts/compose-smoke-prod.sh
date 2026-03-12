#!/usr/bin/env bash
# compose-smoke-prod.sh — build and smoke-test the production compose stack in CI.

set -euo pipefail

COMPOSE_FILE="${COMPOSE_FILE:-docker-compose.prod.yml}"
SMOKE_TIMEOUT="${SMOKE_TIMEOUT:-180}"
INGRESS_URL="${INGRESS_URL:-http://127.0.0.1}"
INGRESS_URL="${INGRESS_URL%/}"
INGRESS_HOST="${INGRESS_HOST:-inferai.co.in}"

: "${INFERA_ADMIN_KEY:=inf_0123456789abcdef0123456789abcdef0123456789abcdef}"
: "${INFERA_ALLOWED_ORIGINS:=https://example.com}"
: "${INFERA_GATEWAY_ADDRESS:=https://example.com}"
: "${INFERA_WORKER_SHARED_TOKEN:=test-worker-token}"
: "${INFERA_WORKER_IMAGE:=ghcr.io/example/infera-worker:test}"
: "${GRAFANA_ADMIN_USER:=admin}"
: "${GRAFANA_ADMIN_PASSWORD:=test-grafana-password}"
: "${ALERT_EMAIL_TO:=alerts@example.com}"
: "${ALERT_SMTP_FROM:=alerts@example.com}"
: "${ALERT_SMTP_SMARTHOST:=smtp.example.com:587}"
: "${ALERT_SMTP_USERNAME:=alerts@example.com}"
: "${ALERT_SMTP_PASSWORD:=test-alert-password}"
: "${RUNPOD_API_KEY:=}"
: "${VASTAI_API_KEY:=}"
: "${HF_TOKEN:=}"

cleanup() {
  local down_args=("down" "--remove-orphans")
  if [[ "${REMOVE_COMPOSE_VOLUMES:-false}" == "true" ]]; then
    down_args+=("-v")
  fi
  docker compose -f "${COMPOSE_FILE}" "${down_args[@]}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

prepare_ci_bind_mounts() {
  if [[ "${CI:-}" != "true" ]]; then
    return
  fi

  # The production compose file bind-mounts ./data into /app/data. In CI the
  # gateway container runs as a non-root user, so make the host path writable
  # before startup.
  mkdir -p data
  chmod 0777 data
}

wait_for_service() {
  local service="$1"
  local timeout_seconds="$2"
  local container_id
  local status
  local elapsed=0

  container_id="$(docker compose -f "${COMPOSE_FILE}" ps -q "${service}")"
  if [[ -z "${container_id}" ]]; then
    echo "ERROR: could not resolve container id for service ${service}"
    return 1
  fi

  while (( elapsed < timeout_seconds )); do
    status="$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "${container_id}")"
    if [[ "${status}" == "healthy" || "${status}" == "running" ]]; then
      echo "   OK: ${service} is ${status}"
      return 0
    fi
    sleep 5
    elapsed=$((elapsed + 5))
  done

  echo "ERROR: ${service} did not become healthy in ${timeout_seconds}s"
  docker compose -f "${COMPOSE_FILE}" logs "${service}" --tail=200 || true
  return 1
}

prepare_ci_bind_mounts

echo "Building and starting gateway from ${COMPOSE_FILE}"
docker compose -f "${COMPOSE_FILE}" up -d --build gateway

echo "Waiting for gateway"
wait_for_service gateway "${SMOKE_TIMEOUT}"

echo "Starting frontend"
docker compose -f "${COMPOSE_FILE}" up -d frontend

echo "Waiting for frontend"
wait_for_service frontend "${SMOKE_TIMEOUT}"

echo "Starting caddy ingress"
docker compose -f "${COMPOSE_FILE}" up -d caddy

echo "Waiting for caddy"
wait_for_service caddy "${SMOKE_TIMEOUT}"

echo "Checking ingress /health"
HEALTH_BODY="$(curl -fsS --max-time 10 \
  -H "Host: ${INGRESS_HOST}" \
  "${INGRESS_URL}/health")"
HEALTH_BODY="${HEALTH_BODY}" python3 - <<'PY'
import os

body = os.environ["HEALTH_BODY"]
if "healthy" not in body and "ok" not in body:
    raise SystemExit(f"/health did not report healthy state: {body}")
PY

echo "Checking authenticated ingress /v1/models"
MODELS_BODY="$(curl -fsS --max-time 10 \
  -H "Host: ${INGRESS_HOST}" \
  -H "Authorization: Bearer ${INFERA_ADMIN_KEY}" \
  "${INGRESS_URL}/v1/models")"
MODELS_BODY="${MODELS_BODY}" python3 - <<'PY'
import json
import os

payload = json.loads(os.environ["MODELS_BODY"])
if not isinstance(payload.get("data"), list):
    raise SystemExit(f"/v1/models missing data array: {payload}")
PY

echo "Checking ingress root document"
FRONTEND_BODY="$(curl -fsS --max-time 10 \
  -H "Host: ${INGRESS_HOST}" \
  "${INGRESS_URL}/")"
if [[ "${FRONTEND_BODY}" != *"<html"* ]]; then
  echo "ERROR: frontend root did not return HTML"
  exit 1
fi

echo "Production compose smoke test passed."
