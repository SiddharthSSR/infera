#!/usr/bin/env bash
# release-verify.sh — post-deploy release verification for Infera.

set -euo pipefail

BASE_URL="${1:-${INFERA_BASE_URL:-https://inferai.co.in}}"
BASE_URL="${BASE_URL%/}"
DASHBOARD_URL="${INFERA_DASHBOARD_URL:-https://dashboard.inferai.co.in}"
DASHBOARD_URL="${DASHBOARD_URL%/}"
GATEWAY_INTERNAL_URL="${INFERA_GATEWAY_INTERNAL_URL:-http://localhost:8080}"
GATEWAY_INTERNAL_URL="${GATEWAY_INTERNAL_URL%/}"
VERIFY_TIMEOUT="${VERIFY_TIMEOUT:-10}"

echo "Release verification"
echo "  app:       ${BASE_URL}"
echo "  dashboard: ${DASHBOARD_URL}"
echo "  gateway:   ${GATEWAY_INTERNAL_URL}"

echo "1) Checking public site root"
curl --fail --silent --show-error --max-time "${VERIFY_TIMEOUT}" -I "${BASE_URL}" >/dev/null
echo "   OK: site root responds"

echo "2) Checking public health endpoint"
curl --fail --silent --show-error --max-time "${VERIFY_TIMEOUT}" \
  "${BASE_URL}/health" >/dev/null
echo "   OK: /health responds"

echo "3) Checking dashboard health"
curl --fail --silent --show-error --max-time "${VERIFY_TIMEOUT}" \
  "${DASHBOARD_URL}/api/health" >/dev/null
echo "   OK: dashboard /api/health responds"

echo "4) Checking gateway-backed worker discovery"
WORKER_TARGETS_BODY="$(curl --fail --silent --show-error --max-time "${VERIFY_TIMEOUT}" \
  "${GATEWAY_INTERNAL_URL}/internal/prometheus/worker-targets")"
WORKER_TARGETS_BODY="${WORKER_TARGETS_BODY}" python3 - <<'PY'
import json
import os

payload = json.loads(os.environ["WORKER_TARGETS_BODY"])
if not isinstance(payload, list):
    raise SystemExit("worker-target discovery did not return a JSON array")
PY
echo "   OK: worker discovery endpoint responds with JSON"

echo "5) Running authenticated gateway smoke checks"
"$(dirname "$0")/smoke-test.sh" "${BASE_URL}"

echo "Release verification passed."
