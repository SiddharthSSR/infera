#!/usr/bin/env bash
# smoke-test.sh — post-deploy smoke test for Infera production.
#
# Verifies:
#  1) Gateway health endpoint is reachable
#  2) Authenticated model listing endpoint responds successfully
#
# Usage:
#   INFERA_SMOKE_API_KEY=inf_xxx ./scripts/smoke-test.sh
#   INFERA_SMOKE_API_KEY=inf_xxx ./scripts/smoke-test.sh https://inferai.co.in
#
# Optional env:
#   INFERA_BASE_URL       Base URL (default: https://inferai.co.in)
#   INFERA_SMOKE_API_KEY  API key for authenticated endpoints
#   SMOKE_TIMEOUT         curl timeout seconds (default: 10)

set -euo pipefail

BASE_URL="${1:-${INFERA_BASE_URL:-https://inferai.co.in}}"
BASE_URL="${BASE_URL%/}"
API_KEY="${INFERA_SMOKE_API_KEY:-${INFERA_ADMIN_KEY:-}}"
SMOKE_TIMEOUT="${SMOKE_TIMEOUT:-10}"

if [[ -z "${API_KEY}" ]]; then
  echo "ERROR: INFERA_SMOKE_API_KEY (or INFERA_ADMIN_KEY) is required."
  exit 1
fi

echo "Running smoke checks against ${BASE_URL}"

echo "1) Checking ${BASE_URL}/health"
HEALTH_BODY="$(curl -fsS --max-time "${SMOKE_TIMEOUT}" "${BASE_URL}/health")"
if [[ "${HEALTH_BODY}" != *"healthy"* && "${HEALTH_BODY}" != *"ok"* ]]; then
  echo "ERROR: /health response did not indicate healthy status."
  echo "Response: ${HEALTH_BODY}"
  exit 1
fi
echo "   OK: health endpoint"

echo "2) Checking authenticated ${BASE_URL}/v1/models"
MODELS_BODY="$(curl -fsS --max-time "${SMOKE_TIMEOUT}" \
  -H "Authorization: Bearer ${API_KEY}" \
  "${BASE_URL}/v1/models")"
if [[ "${MODELS_BODY}" != *"\"data\""* ]]; then
  echo "ERROR: /v1/models response missing data field."
  echo "Response: ${MODELS_BODY}"
  exit 1
fi
echo "   OK: models endpoint"

echo "Smoke test passed."
