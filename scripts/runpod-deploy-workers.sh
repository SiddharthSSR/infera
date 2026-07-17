#!/usr/bin/env bash
# Provision one release-bound RunPod worker and wait until gateway discovery sees it.

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
cleanup_configs() { rm -f "${ADMIN_CONFIG:-}" "${RUNPOD_CONFIG:-}"; }
trap cleanup_configs EXIT
ADMIN_CONFIG="$(recovery_bearer_config "${ADMIN_KEY}")"
RUNPOD_CONFIG="$(recovery_bearer_config "${RUNPOD_KEY}")"
GATEWAY_URL="$(recovery_gateway_url)"
: "${INFERA_RECOVERY_WORKER_MODEL:?reviewed recovery worker model is required}"
MODEL="${INFERA_RECOVERY_WORKER_MODEL}"
GPU_TYPE="${INFERA_RECOVERY_WORKER_GPU_TYPE:-RTX_4090}"
ENGINE="${INFERA_RECOVERY_WORKER_ENGINE:-vllm}"
WAIT_ATTEMPTS="${INFERA_RECOVERY_WORKER_WAIT_ATTEMPTS:-120}"

# Reconcile provider state by exact release name before provisioning. The gateway's
# deployment credential is process-owned, so an orphan from an interrupted attempt
# must be terminated rather than adopted without its credential.
recovery_runpod_remove_named_pods "infera-release-${RELEASE_ID}" "${RUNPOD_CONFIG}" >/dev/null

PAYLOAD="$(RELEASE_ID="${RELEASE_ID}" MODEL="${MODEL}" GPU_TYPE="${GPU_TYPE}" ENGINE="${ENGINE}" python3 - <<'PY'
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

RESPONSE="$(curl --fail --silent --show-error --max-time 720 -X POST \
  --config "${ADMIN_CONFIG}" \
  -H "Content-Type: application/json" \
  --data "${PAYLOAD}" "${GATEWAY_URL}/api/instances/provision")"
INSTANCE_ID="$(RESPONSE="${RESPONSE}" python3 - <<'PY'
import json
import os

print(json.loads(os.environ["RESPONSE"])["instance"]["id"])
PY
)"
[[ "${INSTANCE_ID}" =~ ^[A-Za-z0-9._:-]+$ ]] || {
  echo "ERROR: gateway returned an unsafe instance identifier" >&2
  exit 1
}

for _ in $(seq 1 "${WAIT_ATTEMPTS}"); do
  INSTANCES_JSON="$(curl --fail --silent --show-error --max-time 15 \
    --config "${ADMIN_CONFIG}" "${GATEWAY_URL}/api/instances")"
  if INSTANCE_ID="${INSTANCE_ID}" INSTANCES_JSON="${INSTANCES_JSON}" python3 - <<'PY'
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
    echo "RunPod worker registered for release ${RELEASE_ID}"
    exit 0
  fi
  sleep 5
done

echo "ERROR: RunPod worker did not register before the release timeout" >&2
exit 1
