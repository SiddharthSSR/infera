#!/usr/bin/env bash
# smoke-test.sh — post-deploy smoke test for Infera production.
#
# Verifies:
#  1) Public and internal health endpoints are reachable
#  2) Authenticated model listing endpoint responds successfully
#  3) Optional authenticated inference checks pass when a model is supplied
#
# Usage:
#   INFERA_SMOKE_API_KEY=inf_xxx ./scripts/smoke-test.sh
#   INFERA_SMOKE_API_KEY=inf_xxx ./scripts/smoke-test.sh https://inferai.co.in
#
# Optional env:
#   INFERA_BASE_URL       Base URL (default: https://inferai.co.in)
#   INFERA_SMOKE_API_KEY  API key for authenticated endpoints
#   SMOKE_TIMEOUT         curl timeout seconds (default: 10)
#   INFERA_SMOKE_MODEL    Optional model ID for inference contract checks
#   INFERA_SMOKE_PROMPT   Optional prompt for inference checks
#   INFERA_SMOKE_STREAM   Set to 1 to also validate streaming SSE output

set -euo pipefail

BASE_URL="${1:-${INFERA_BASE_URL:-https://inferai.co.in}}"
BASE_URL="${BASE_URL%/}"
API_KEY="${INFERA_SMOKE_API_KEY:-${INFERA_ADMIN_KEY:-}}"
SMOKE_TIMEOUT="${SMOKE_TIMEOUT:-10}"
SMOKE_MODEL="${INFERA_SMOKE_MODEL:-}"
SMOKE_PROMPT="${INFERA_SMOKE_PROMPT:-hello from smoke test}"
SMOKE_STREAM="${INFERA_SMOKE_STREAM:-0}"

build_chat_payload() {
  local stream_flag="$1"
  SMOKE_MODEL="${SMOKE_MODEL}" SMOKE_PROMPT="${SMOKE_PROMPT}" SMOKE_STREAM_FLAG="${stream_flag}" python3 - <<'PY'
import json
import os

payload = {
    "model": os.environ["SMOKE_MODEL"],
    "messages": [{"role": "user", "content": os.environ["SMOKE_PROMPT"]}],
}
if os.environ["SMOKE_STREAM_FLAG"] == "1":
    payload["stream"] = True
print(json.dumps(payload))
PY
}

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

echo "2) Checking ${BASE_URL}/api/health"
API_HEALTH_BODY="$(curl -fsS --max-time "${SMOKE_TIMEOUT}" "${BASE_URL}/api/health")"
if [[ "${API_HEALTH_BODY}" != *"healthy"* && "${API_HEALTH_BODY}" != *"ok"* ]]; then
  echo "ERROR: /api/health response did not indicate healthy status."
  echo "Response: ${API_HEALTH_BODY}"
  exit 1
fi
echo "   OK: api health endpoint"

echo "3) Checking authenticated ${BASE_URL}/v1/models"
MODELS_BODY="$(curl -fsS --max-time "${SMOKE_TIMEOUT}" \
  -H "Authorization: Bearer ${API_KEY}" \
  "${BASE_URL}/v1/models")"
MODELS_BODY="${MODELS_BODY}" python3 - <<'PY'
import json
import os

payload = json.loads(os.environ["MODELS_BODY"])
if not isinstance(payload.get("data"), list):
    raise SystemExit("models response missing data array")
PY
echo "   OK: models endpoint"

if [[ -n "${SMOKE_MODEL}" ]]; then
  echo "4) Checking authenticated ${BASE_URL}/v1/chat/completions"
  CHAT_PAYLOAD="$(build_chat_payload 0)"
  CHAT_BODY="$(curl -fsS --max-time "${SMOKE_TIMEOUT}" \
    -H "Authorization: Bearer ${API_KEY}" \
    -H "Content-Type: application/json" \
    -d "${CHAT_PAYLOAD}" \
    "${BASE_URL}/v1/chat/completions")"
  CHAT_BODY="${CHAT_BODY}" python3 - <<'PY'
import json
import os

payload = json.loads(os.environ["CHAT_BODY"])
if payload.get("object") != "chat.completion":
    raise SystemExit(f"unexpected object: {payload.get('object')!r}")
choices = payload.get("choices")
if not isinstance(choices, list) or not choices:
    raise SystemExit("missing completion choices")
message = choices[0].get("message") or {}
if message.get("role") != "assistant":
    raise SystemExit(f"unexpected choice role: {message.get('role')!r}")
usage = payload.get("usage") or {}
required = ("prompt_tokens", "completion_tokens", "total_tokens")
missing = [field for field in required if field not in usage]
if missing:
    raise SystemExit(f"missing usage fields: {missing}")
PY
  echo "   OK: non-streaming chat completions endpoint"

  if [[ "${SMOKE_STREAM}" == "1" ]]; then
    echo "5) Checking streaming ${BASE_URL}/v1/chat/completions"
    STREAM_PAYLOAD="$(build_chat_payload 1)"
    STREAM_BODY="$(curl -fsS --max-time "${SMOKE_TIMEOUT}" \
      -H "Authorization: Bearer ${API_KEY}" \
      -H "Content-Type: application/json" \
      -d "${STREAM_PAYLOAD}" \
      "${BASE_URL}/v1/chat/completions")"
    STREAM_BODY="${STREAM_BODY}" python3 - <<'PY'
import json
import os

lines = [line.strip() for line in os.environ["STREAM_BODY"].splitlines() if line.strip()]
if not lines or lines[-1] != "data: [DONE]":
    raise SystemExit("stream did not terminate with data: [DONE]")
payload_lines = [line for line in lines[:-1] if line.startswith("data: ")]
if len(payload_lines) < 2:
    raise SystemExit("expected at least initial role chunk and one content chunk")
first = json.loads(payload_lines[0][6:])
if first.get("object") != "chat.completion.chunk":
    raise SystemExit(f"unexpected stream object: {first.get('object')!r}")
PY
    echo "   OK: streaming chat completions endpoint"
  fi
else
  echo "4) Skipping chat completion checks (set INFERA_SMOKE_MODEL to enable)"
fi

echo "Smoke test passed."
