#!/usr/bin/env bash
# compose-smoke-prod.sh — build and smoke-test the production compose stack in CI.

set -euo pipefail

COMPOSE_FILE="${COMPOSE_FILE:-docker-compose.prod.yml}"
COMPOSE_ARGS=(-f "${COMPOSE_FILE}")
SMOKE_TIMEOUT="${SMOKE_TIMEOUT:-180}"
INGRESS_URL="${INGRESS_URL:-http://127.0.0.1}"
INGRESS_URL="${INGRESS_URL%/}"
INGRESS_HOST="${INGRESS_HOST:-inferai.co.in}"
TMP_DIR="$(mktemp -d)"

cleanup() {
  if [[ -f "${TMP_DIR}/Caddyfile.backup" ]]; then
    cp "${TMP_DIR}/Caddyfile.backup" deploy/caddy/Caddyfile
  fi

  local down_args=("down" "--remove-orphans")
  if [[ "${REMOVE_COMPOSE_VOLUMES:-false}" == "true" ]]; then
    down_args+=("-v")
  fi
  docker compose "${COMPOSE_ARGS[@]}" "${down_args[@]}" >/dev/null 2>&1 || true
  rm -rf "${TMP_DIR}"
}
trap cleanup EXIT

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

export INFERA_ADMIN_KEY
export INFERA_ALLOWED_ORIGINS
export INFERA_GATEWAY_ADDRESS
export INFERA_WORKER_SHARED_TOKEN
export INFERA_WORKER_IMAGE
export GRAFANA_ADMIN_USER
export GRAFANA_ADMIN_PASSWORD
export ALERT_EMAIL_TO
export ALERT_SMTP_FROM
export ALERT_SMTP_SMARTHOST
export ALERT_SMTP_USERNAME
export ALERT_SMTP_PASSWORD
export RUNPOD_API_KEY
export VASTAI_API_KEY
export HF_TOKEN

bash "$(dirname "$0")/validate-prod-env.sh"

prepare_smoke_compose_file() {
  local smoke_override_file="${TMP_DIR}/docker-compose.smoke.override.yml"
  local smoke_data_dir="${TMP_DIR}/data"
  mkdir -p "${smoke_data_dir}"
  chmod 0777 "${smoke_data_dir}"

  cat > "${smoke_override_file}" <<EOF
services:
  gateway:
    volumes:
      - ${smoke_data_dir}:/app/data
EOF

  COMPOSE_ARGS+=(-f "${smoke_override_file}")
}

prepare_ci_caddyfile() {
  if [[ "${CI:-}" != "true" && "${INGRESS_URL}" != "http://127.0.0.1" && "${INGRESS_URL}" != "http://localhost" ]]; then
    return
  fi

  cp deploy/caddy/Caddyfile "${TMP_DIR}/Caddyfile.backup"
  cat > deploy/caddy/Caddyfile <<'EOF'
:80 {
  encode gzip
  header {
    X-Content-Type-Options "nosniff"
    X-Frame-Options "SAMEORIGIN"
    Referrer-Policy "strict-origin-when-cross-origin"
    -Server
  }

  handle /api/* {
    reverse_proxy gateway:8080
  }

  handle /v1/* {
    reverse_proxy gateway:8080 {
      flush_interval -1
    }
  }

  handle /health {
    reverse_proxy gateway:8080
  }

  handle {
    reverse_proxy frontend:3000
  }
}
EOF
}

compose() {
  docker compose "${COMPOSE_ARGS[@]}" "$@"
}

wait_for_service() {
  local service="$1"
  local timeout_seconds="$2"
  local container_id
  local status
  local elapsed=0

  container_id="$(compose ps -q "${service}")"
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
  compose logs "${service}" --tail=200 || true
  return 1
}

fetch_ingress() {
  local label="$1"
  local url="$2"
  local output_file="$3"
  local elapsed=0

  while (( elapsed < SMOKE_TIMEOUT )); do
    if curl -fsS --max-time 10 \
      -H "Host: ${INGRESS_HOST}" \
      "${@:4}" \
      "${url}" >"${output_file}"; then
      return 0
    fi
    sleep 2
    elapsed=$((elapsed + 2))
  done

  echo "ERROR: ${label} did not respond successfully within ${SMOKE_TIMEOUT}s"
  compose logs caddy --tail=200 || true
  return 1
}

prepare_smoke_compose_file
prepare_ci_caddyfile

echo "Building and starting gateway from ${COMPOSE_FILE}"
compose up -d --build gateway

echo "Waiting for gateway"
wait_for_service gateway "${SMOKE_TIMEOUT}"

echo "Starting frontend"
compose up -d frontend

echo "Waiting for frontend"
wait_for_service frontend "${SMOKE_TIMEOUT}"

echo "Starting caddy ingress"
compose up -d caddy

echo "Waiting for caddy"
wait_for_service caddy "${SMOKE_TIMEOUT}"

echo "Checking ingress /health"
HEALTH_FILE="${TMP_DIR}/health.txt"
fetch_ingress "ingress /health" "${INGRESS_URL}/health" "${HEALTH_FILE}"
HEALTH_BODY="$(cat "${HEALTH_FILE}")"
HEALTH_BODY="${HEALTH_BODY}" python3 - <<'PY'
import os

body = os.environ["HEALTH_BODY"]
if "healthy" not in body and "ok" not in body:
    raise SystemExit(f"/health did not report healthy state: {body}")
PY

echo "Checking authenticated ingress /v1/models"
MODELS_FILE="${TMP_DIR}/models.json"
fetch_ingress "authenticated ingress /v1/models" "${INGRESS_URL}/v1/models" "${MODELS_FILE}" \
  -H "Authorization: Bearer ${INFERA_ADMIN_KEY}"
MODELS_BODY="$(cat "${MODELS_FILE}")"
MODELS_BODY="${MODELS_BODY}" python3 - <<'PY'
import json
import os

payload = json.loads(os.environ["MODELS_BODY"])
if not isinstance(payload.get("data"), list):
    raise SystemExit(f"/v1/models missing data array: {payload}")
PY

echo "Checking ingress root document"
FRONTEND_FILE="${TMP_DIR}/frontend.html"
fetch_ingress "ingress root document" "${INGRESS_URL}/" "${FRONTEND_FILE}"
FRONTEND_BODY="$(cat "${FRONTEND_FILE}")"
if [[ "${FRONTEND_BODY}" != *"<html"* ]]; then
  echo "ERROR: frontend root did not return HTML"
  exit 1
fi

echo "Production compose smoke test passed."
