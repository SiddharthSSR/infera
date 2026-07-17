#!/usr/bin/env bash
# Provision one release-bound RunPod worker with bounded, capacity-only GPU fallback.

set -euo pipefail

MANIFEST="${1:?usage: runpod-deploy-workers.sh <release.manifest>}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=recovery-adapter-common.sh
source "${SCRIPT_DIR}/recovery-adapter-common.sh"

RELEASE_ID="$(recovery_manifest_value "${MANIFEST}" INFERA_RELEASE_ID)"
ADMIN_KEY="$(recovery_env_value INFERA_ADMIN_KEY)"
RUNPOD_KEY="$(recovery_env_value RUNPOD_API_KEY)"
ADMIN_CONFIG=""
RUNPOD_CONFIG=""
RESPONSE_FILE=""
cleanup_configs() { rm -f "${ADMIN_CONFIG:-}" "${RUNPOD_CONFIG:-}" "${RESPONSE_FILE:-}"; }
trap cleanup_configs EXIT
ADMIN_CONFIG="$(recovery_bearer_config "${ADMIN_KEY}")"
RUNPOD_CONFIG="$(recovery_bearer_config "${RUNPOD_KEY}")"
RESPONSE_FILE="$(mktemp)"
chmod 0600 "${RESPONSE_FILE}"
GATEWAY_URL="$(recovery_gateway_url)"
: "${INFERA_RECOVERY_WORKER_MODEL:?reviewed recovery worker model is required}"
MODEL="${INFERA_RECOVERY_WORKER_MODEL}"
ENGINE="${INFERA_RECOVERY_WORKER_ENGINE:-vllm}"
POD_NAME="infera-release-${RELEASE_ID}"

if recovery_is_candidate_step; then
  CLEANUP_RESERVE_SECONDS="${INFERA_RECOVERY_ROLLBACK_RESERVE_SECONDS:-300}"
else
  CLEANUP_RESERVE_SECONDS=0
fi
POST_TIMEOUT_SECONDS="${INFERA_RECOVERY_PROVISION_POST_TIMEOUT_SECONDS:-45}"
MIN_ATTEMPT_BUDGET_SECONDS="${INFERA_RECOVERY_MIN_ATTEMPT_BUDGET_SECONDS:-60}"
AMBIGUOUS_CLEANUP_SECONDS="${INFERA_RECOVERY_AMBIGUOUS_CLEANUP_SECONDS:-60}"
[[ "${POST_TIMEOUT_SECONDS}" =~ ^[1-9][0-9]*$ && "${POST_TIMEOUT_SECONDS}" -le 120 ]] || exit 2
[[ "${MIN_ATTEMPT_BUDGET_SECONDS}" =~ ^[1-9][0-9]*$ ]] || exit 2
[[ "${AMBIGUOUS_CLEANUP_SECONDS}" =~ ^[1-9][0-9]*$ ]] || exit 2
if (( CLEANUP_RESERVE_SECONDS > 0 && AMBIGUOUS_CLEANUP_SECONDS >= CLEANUP_RESERVE_SECONDS )); then
  echo "ERROR: ambiguous cleanup slice must preserve part of the rollback reserve" >&2
  exit 2
fi
export INFERA_RECOVERY_CLEANUP_RESERVE_SECONDS="${CLEANUP_RESERVE_SECONDS}"
export INFERA_RECOVERY_RELEASE_ID="${INFERA_RECOVERY_RELEASE_ID:-${RELEASE_ID}}"

if [[ -n "${INFERA_RECOVERY_WORKER_GPU_TYPES:-}" && -n "${INFERA_RECOVERY_WORKER_GPU_TYPE:-}" ]]; then
  echo "ERROR: set either INFERA_RECOVERY_WORKER_GPU_TYPES or legacy INFERA_RECOVERY_WORKER_GPU_TYPE, not both" >&2
  exit 2
fi
GPU_INPUT="${INFERA_RECOVERY_WORKER_GPU_TYPES:-${INFERA_RECOVERY_WORKER_GPU_TYPE:-RTX_4090}}"
GPU_CANDIDATES=()
if ! GPU_LINES="$(python3 - "${GPU_INPUT}" <<'PY'
import sys

allowed = {"RTX_4090", "RTX_4080", "A100_40GB", "A100_80GB", "H100", "L40S"}
values = [value.strip() for value in sys.argv[1].split(",")]
if not values or len(values) > 5 or any(not value or value not in allowed for value in values):
    raise SystemExit(1)
if len(set(values)) != len(values):
    raise SystemExit(1)
print("\n".join(values))
PY
)"; then
  echo "ERROR: recovery GPU types must be a unique ordered list of at most five supported values" >&2
  exit 2
fi
while IFS= read -r gpu; do
  [[ -n "${gpu}" ]] && GPU_CANDIDATES[${#GPU_CANDIDATES[@]}]="${gpu}"
done <<<"${GPU_LINES}"
[[ "${#GPU_CANDIDATES[@]}" -gt 0 ]] || {
  echo "ERROR: recovery GPU candidates must be a unique ordered list of supported GPU types" >&2
  exit 2
}

record_evidence() {
  recovery_record_worker_evidence "$@" || {
    echo "ERROR: unable to append sanitized worker recovery evidence" >&2
    return 1
  }
}

safe_reconcile_terminal() {
  local gpu="$1" attempt="$2" reason="$3"
  # Once an outcome is ambiguous, cleanup may consume a bounded slice of the
  # rollback reserve while preserving the majority for restoring service.
  if (( CLEANUP_RESERVE_SECONDS > 0 )); then
    export INFERA_RECOVERY_CLEANUP_RESERVE_SECONDS="$((CLEANUP_RESERVE_SECONDS - AMBIGUOUS_CLEANUP_SECONDS))"
  else
    export INFERA_RECOVERY_CLEANUP_RESERVE_SECONDS=0
  fi
  if recovery_runpod_remove_named_pods "${POD_NAME}" "${RUNPOD_CONFIG}" >/dev/null; then
    record_evidence reconcile pass "${gpu}" "${attempt}" none || true
  else
    record_evidence reconcile fail "${gpu}" "${attempt}" cleanup_failed || true
  fi
  record_evidence provision_response terminal "${gpu}" "${attempt}" "${reason}" || true
  return 1
}

classify_error_response() {
  python3 - "${RESPONSE_FILE}" <<'PY'
import json
import sys

try:
    payload = json.load(open(sys.argv[1], encoding="utf-8"))
except (OSError, ValueError):
    raise SystemExit(1)
error = payload.get("error")
if not isinstance(error, dict):
    raise SystemExit(1)
if (
    error.get("provider") == "runpod"
    and error.get("provider_error_code") == "capacity_unavailable"
    and error.get("retryable") is True
):
    print("capacity_unavailable")
    raise SystemExit(0)
raise SystemExit(1)
PY
}

parse_created_instance() {
  python3 - "${RESPONSE_FILE}" <<'PY'
import json
import re
import sys

try:
    payload = json.load(open(sys.argv[1], encoding="utf-8"))
    instance_id = payload["instance"]["id"]
except (OSError, ValueError, KeyError, TypeError):
    raise SystemExit(1)
if not isinstance(instance_id, str) or not re.fullmatch(r"[A-Za-z0-9._:-]+", instance_id):
    raise SystemExit(1)
print(instance_id)
PY
}

wait_for_registration() {
  local instance_id="$1" timeout instances_json
  while recovery_remaining_seconds "${CLEANUP_RESERVE_SECONDS}" >/dev/null; do
    timeout="$(recovery_bounded_timeout 15 "${CLEANUP_RESERVE_SECONDS}")" || return 1
    if instances_json="$(curl --fail --silent --show-error --max-time "${timeout}" \
      --config "${ADMIN_CONFIG}" "${GATEWAY_URL}/api/instances")"; then
      if INSTANCE_ID="${instance_id}" INSTANCES_JSON="${instances_json}" python3 - <<'PY'
import json
import os
import sys

payload = json.loads(os.environ["INSTANCES_JSON"])
for instance in payload.get("instances", []):
    if instance.get("id") == os.environ["INSTANCE_ID"]:
        sys.exit(0 if instance.get("worker_id") and instance.get("status") == "running" else 1)
sys.exit(1)
PY
      then
        return 0
      fi
    fi
    recovery_deadline_sleep 5 "${CLEANUP_RESERVE_SECONDS}" || return 1
  done
  return 1
}

# A prior attempt cannot be adopted because its deployment-bound credential is
# process-owned. Clean it up and prove exact-name absence before any POST.
recovery_runpod_remove_named_pods "${POD_NAME}" "${RUNPOD_CONFIG}" >/dev/null || {
  record_evidence reconcile fail - 0 cleanup_failed || true
  echo "ERROR: unable to reconcile release-owned RunPod workers" >&2
  exit 1
}
record_evidence reconcile pass - 0 none

attempt=0
for gpu in "${GPU_CANDIDATES[@]}"; do
  attempt=$((attempt + 1))
  remaining="$(recovery_remaining_seconds "${CLEANUP_RESERVE_SECONDS}")" || {
    record_evidence provision_response terminal "${gpu}" "${attempt}" deadline_exhausted || true
    echo "ERROR: recovery deadline exhausted before provisioning" >&2
    exit 1
  }
  if (( remaining < MIN_ATTEMPT_BUDGET_SECONDS )); then
    record_evidence provision_response terminal "${gpu}" "${attempt}" deadline_exhausted || true
    echo "ERROR: insufficient attempt and cleanup budget for another provisioning request" >&2
    exit 1
  fi
  record_evidence candidate_selected start "${gpu}" "${attempt}" none

  if ! recovery_runpod_confirm_named_pods_absent "${POD_NAME}" "${RUNPOD_CONFIG}"; then
    safe_reconcile_terminal "${gpu}" "${attempt}" state_not_empty || true
    echo "ERROR: exact release-owned RunPod name was not confirmed empty before provisioning" >&2
    exit 1
  fi
  record_evidence reconcile pass "${gpu}" "${attempt}" none

  PAYLOAD="$(RELEASE_ID="${RELEASE_ID}" MODEL="${MODEL}" GPU_TYPE="${gpu}" ENGINE="${ENGINE}" python3 - <<'PY'
import json
import os

print(json.dumps({
    "name": "infera-release-" + os.environ["RELEASE_ID"],
    "provider": "runpod",
    "engine": os.environ["ENGINE"],
    "gpu_type": os.environ["GPU_TYPE"],
    "gpu_count": 1,
    "models": [os.environ["MODEL"]],
}))
PY
)"

  timeout="$(recovery_bounded_timeout "${POST_TIMEOUT_SECONDS}" "${CLEANUP_RESERVE_SECONDS}")" || {
    record_evidence provision_response terminal "${gpu}" "${attempt}" deadline_exhausted || true
    echo "ERROR: recovery deadline exhausted before provisioning" >&2
    exit 1
  }
  : >"${RESPONSE_FILE}"
  if ! http_status="$(curl --silent --show-error --max-time "${timeout}" -X POST \
    --config "${ADMIN_CONFIG}" -H "Content-Type: application/json" --data "${PAYLOAD}" \
    --output "${RESPONSE_FILE}" --write-out '%{http_code}' \
    "${GATEWAY_URL}/api/instances/provision")"; then
    safe_reconcile_terminal "${gpu}" "${attempt}" transport_failure || true
    echo "ERROR: RunPod provisioning outcome was ambiguous; fallback is forbidden" >&2
    exit 1
  fi
  [[ "${http_status}" =~ ^[0-9]{3}$ ]] || {
    safe_reconcile_terminal "${gpu}" "${attempt}" invalid_response || true
    echo "ERROR: gateway returned an invalid provisioning status" >&2
    exit 1
  }

  if [[ "${http_status}" == "201" ]]; then
    if ! instance_id="$(parse_created_instance)"; then
      safe_reconcile_terminal "${gpu}" "${attempt}" invalid_response || true
      echo "ERROR: gateway returned an invalid successful provisioning response" >&2
      exit 1
    fi
    record_evidence provision_response pass "${gpu}" "${attempt}" created
    if wait_for_registration "${instance_id}"; then
      record_evidence registration pass "${gpu}" "${attempt}" registered
      echo "RunPod worker registered for release ${RELEASE_ID}"
      exit 0
    fi
    safe_reconcile_terminal "${gpu}" "${attempt}" registration_timeout || true
    echo "ERROR: RunPod worker did not register before the recovery deadline" >&2
    exit 1
  fi

  if [[ "${http_status}" == "503" ]] &&
    classification="$(classify_error_response)" && [[ "${classification}" == "capacity_unavailable" ]]; then
    if ! recovery_runpod_remove_named_pods "${POD_NAME}" "${RUNPOD_CONFIG}" >/dev/null ||
      ! recovery_runpod_confirm_named_pods_absent "${POD_NAME}" "${RUNPOD_CONFIG}"; then
      safe_reconcile_terminal "${gpu}" "${attempt}" state_not_empty || true
      echo "ERROR: capacity rejection left ambiguous provider state; fallback is forbidden" >&2
      exit 1
    fi
    if (( attempt < ${#GPU_CANDIDATES[@]} )); then
      record_evidence provision_response fallback "${gpu}" "${attempt}" capacity_unavailable
      continue
    fi
    record_evidence provision_response terminal "${gpu}" "${attempt}" capacity_unavailable
    echo "ERROR: RunPod capacity unavailable for every reviewed GPU candidate" >&2
    exit 1
  fi

  safe_reconcile_terminal "${gpu}" "${attempt}" unknown_failure || true
  echo "ERROR: non-capacity provisioning failure is terminal; fallback is forbidden" >&2
  exit 1
done

echo "ERROR: no recovery GPU candidate was attempted" >&2
exit 1
