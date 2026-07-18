#!/usr/bin/env bash
# release-verify.sh — post-deploy release verification for Infera.

set -euo pipefail

BASE_URL="${1:-${INFERA_BASE_URL:-https://inferai.co.in}}"
BASE_URL="${BASE_URL%/}"
DASHBOARD_URL="${INFERA_DASHBOARD_URL:-https://dashboard.inferai.co.in}"
DASHBOARD_URL="${DASHBOARD_URL%/}"
COMPOSE_FILE="${COMPOSE_FILE:-docker-compose.prod.yml}"
GATEWAY_INTERNAL_URL="${INFERA_GATEWAY_INTERNAL_URL:-}"
GATEWAY_INTERNAL_URL="${GATEWAY_INTERNAL_URL%/}"
APP_VERIFY_URL="${GATEWAY_INTERNAL_URL:-${BASE_URL}}"
VERIFY_TIMEOUT="${VERIFY_TIMEOUT:-10}"
RELEASE_WORKER_MODE="${INFERA_RELEASE_WORKER_MODE:-serving}"

case "${RELEASE_WORKER_MODE}" in
  serving|cost-saving) ;;
  *)
    echo "INFERA_RELEASE_WORKER_MODE must be serving or cost-saving" >&2
    exit 2
    ;;
esac

echo "Release verification"
echo "  app:       ${BASE_URL}"
echo "  dashboard: ${DASHBOARD_URL}"
if [[ -n "${GATEWAY_INTERNAL_URL}" ]]; then
  echo "  gateway:   ${GATEWAY_INTERNAL_URL}"
else
  echo "  gateway:   docker compose exec gateway"
fi

echo "1) Checking public ingress state"
if [[ "${INFERA_EXPECT_TRAFFIC_DRAINED:-0}" == "1" ]]; then
  PUBLIC_STATUS="$(curl --silent --show-error --output /dev/null --write-out '%{http_code}' \
    --max-time "${VERIFY_TIMEOUT}" "${BASE_URL}/health")"
  [[ "${PUBLIC_STATUS}" == "503" ]] || {
    echo "public ingress is not drained" >&2
    exit 1
  }
  echo "   OK: public ingress remains drained"
else
  curl --fail --silent --show-error --max-time "${VERIFY_TIMEOUT}" -I "${BASE_URL}" >/dev/null
  echo "   OK: site root responds"
fi

echo "2) Checking gateway health endpoint"
GATEWAY_HEALTH_BODY="$(curl --fail --silent --show-error --max-time "${VERIFY_TIMEOUT}" \
  "${APP_VERIFY_URL}/health")"
GATEWAY_HEALTH_BODY="${GATEWAY_HEALTH_BODY}" RELEASE_WORKER_MODE="${RELEASE_WORKER_MODE}" python3 - <<'PY'
import json
import os

payload = json.loads(os.environ["GATEWAY_HEALTH_BODY"])
expected_release = os.environ.get("INFERA_RELEASE_ID", "").strip()
expected_protocol = os.environ.get("INFERA_WORKER_PROTOCOL_VERSION", "").strip()
expected_recovery_protocol = os.environ.get("INFERA_RECOVERY_API_PROTOCOL_VERSION", "").strip()
if expected_release and payload.get("release_id") != expected_release:
    raise SystemExit("gateway release identity does not match INFERA_RELEASE_ID")
if expected_protocol and payload.get("worker_protocol_version") != expected_protocol:
    raise SystemExit("gateway worker protocol does not match INFERA_WORKER_PROTOCOL_VERSION")
if expected_recovery_protocol and payload.get("recovery_api_protocol_version") != expected_recovery_protocol:
    raise SystemExit("gateway recovery API protocol does not match INFERA_RECOVERY_API_PROTOCOL_VERSION")
healthy_workers = payload.get("healthy_workers")
if isinstance(healthy_workers, bool) or not isinstance(healthy_workers, int) or healthy_workers < 0:
    raise SystemExit("gateway health missing a valid non-negative healthy_workers count")
mode = os.environ["RELEASE_WORKER_MODE"]
if mode == "serving" and healthy_workers == 0:
    raise SystemExit("serving release has zero healthy workers")
if mode == "cost-saving" and healthy_workers != 0:
    raise SystemExit("cost-saving release unexpectedly has healthy workers")
PY
echo "   OK: /health responds with expected rollout identity and worker mode"

echo "3) Checking dashboard health"
curl --fail --silent --show-error --max-time "${VERIFY_TIMEOUT}" \
  "${DASHBOARD_URL}/api/health" >/dev/null
echo "   OK: dashboard /api/health responds"

echo "4) Checking gateway-backed worker discovery"
if [[ "${RELEASE_WORKER_MODE}" == "cost-saving" ]]; then
  echo "   SKIP: worker discovery is disabled in explicit cost-saving mode"
elif [[ -n "${GATEWAY_INTERNAL_URL}" ]]; then
  WORKER_TARGETS_BODY="$(curl --fail --silent --show-error --max-time "${VERIFY_TIMEOUT}" \
    "${GATEWAY_INTERNAL_URL}/internal/prometheus/worker-targets")"
else
  WORKER_TARGETS_BODY="$(docker compose -f "${COMPOSE_FILE}" exec -T gateway \
    wget -qO- http://127.0.0.1:8080/internal/prometheus/worker-targets)"
fi
if [[ "${RELEASE_WORKER_MODE}" == "serving" ]]; then
  WORKER_TARGETS_BODY="${WORKER_TARGETS_BODY}" python3 - <<'PY'
import json
import os

payload = json.loads(os.environ["WORKER_TARGETS_BODY"])
if not isinstance(payload, list):
    raise SystemExit("worker-target discovery did not return a JSON array")
if not payload:
    raise SystemExit("worker-target discovery returned an empty target list")
if not any(
    isinstance(item, dict)
    and isinstance(item.get("targets"), list)
    and len(item["targets"]) > 0
    for item in payload
):
    raise SystemExit("worker-target discovery did not include any discoverable targets")
PY
  echo "   OK: worker discovery endpoint responds with JSON"
fi

echo "5) Running authenticated gateway smoke checks"
if [[ -z "${INFERA_SMOKE_API_KEY:-}" ]]; then
  echo "INFERA_SMOKE_API_KEY must be set to run smoke tests" >&2
  exit 1
fi
if [[ "${RELEASE_WORKER_MODE}" == "cost-saving" ]]; then
  INFERA_SMOKE_MODEL= SKIP_CHAT_CHECKS=1 "$(dirname "$0")/smoke-test.sh" "${APP_VERIFY_URL}"
else
  "$(dirname "$0")/smoke-test.sh" "${APP_VERIFY_URL}"
fi

echo "Release verification passed."
