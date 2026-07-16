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
VERIFY_TIMEOUT="${VERIFY_TIMEOUT:-10}"

echo "Release verification"
echo "  app:       ${BASE_URL}"
echo "  dashboard: ${DASHBOARD_URL}"
if [[ -n "${GATEWAY_INTERNAL_URL}" ]]; then
  echo "  gateway:   ${GATEWAY_INTERNAL_URL}"
else
  echo "  gateway:   docker compose exec gateway"
fi

echo "1) Checking public site root"
curl --fail --silent --show-error --max-time "${VERIFY_TIMEOUT}" -I "${BASE_URL}" >/dev/null
echo "   OK: site root responds"

echo "2) Checking public health endpoint"
GATEWAY_HEALTH_BODY="$(curl --fail --silent --show-error --max-time "${VERIFY_TIMEOUT}" \
  "${BASE_URL}/health")"
GATEWAY_HEALTH_BODY="${GATEWAY_HEALTH_BODY}" python3 - <<'PY'
import json
import os

payload = json.loads(os.environ["GATEWAY_HEALTH_BODY"])
expected_release = os.environ.get("INFERA_RELEASE_ID", "").strip()
expected_protocol = os.environ.get("INFERA_WORKER_PROTOCOL_VERSION", "").strip()
if expected_release and payload.get("release_id") != expected_release:
    raise SystemExit("gateway release identity does not match INFERA_RELEASE_ID")
if expected_protocol and payload.get("worker_protocol_version") != expected_protocol:
    raise SystemExit("gateway worker protocol does not match INFERA_WORKER_PROTOCOL_VERSION")
PY
echo "   OK: /health responds with expected rollout identity"

echo "3) Checking dashboard health"
curl --fail --silent --show-error --max-time "${VERIFY_TIMEOUT}" \
  "${DASHBOARD_URL}/api/health" >/dev/null
echo "   OK: dashboard /api/health responds"

echo "4) Checking gateway-backed worker discovery"
if [[ -n "${GATEWAY_INTERNAL_URL}" ]]; then
  WORKER_TARGETS_BODY="$(curl --fail --silent --show-error --max-time "${VERIFY_TIMEOUT}" \
    "${GATEWAY_INTERNAL_URL}/internal/prometheus/worker-targets")"
else
  WORKER_TARGETS_BODY="$(docker compose -f "${COMPOSE_FILE}" exec -T gateway \
    wget -qO- http://127.0.0.1:8080/internal/prometheus/worker-targets)"
fi
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

echo "5) Running authenticated gateway smoke checks"
if [[ -z "${INFERA_SMOKE_API_KEY:-}" ]]; then
  echo "INFERA_SMOKE_API_KEY must be set to run smoke tests" >&2
  exit 1
fi
"$(dirname "$0")/smoke-test.sh" "${BASE_URL}"

echo "Release verification passed."
