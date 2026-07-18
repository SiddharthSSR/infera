#!/usr/bin/env bash
# Deterministic tests for the production RunPod and Caddy recovery adapters.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT
mkdir -p "${TMP_DIR}/bin"

cat >"${TMP_DIR}/release.manifest" <<'EOF'
INFERA_RELEASE_ID=release-1
INFERA_GATEWAY_IMAGE=example/gateway:release-1
INFERA_WORKER_IMAGE=example/worker:release-1
INFERA_WORKER_PROTOCOL_VERSION=1
INFERA_RECOVERY_API_PROTOCOL_VERSION=1
INFERA_AUDIT_LEDGER_WRITER_PROTOCOL=2
EOF
printf '%s\n' 'INFERA_ADMIN_KEY=test-admin-key' 'RUNPOD_API_KEY=test-runpod-key' >"${TMP_DIR}/env"

cat >"${TMP_DIR}/bin/docker" <<'EOF'
#!/usr/bin/env bash
set -eu
printf 'docker:%s\n' "$*" >>"${TEST_CALLS}"
printf 'docker-worker-vllm:%s\n' "${INFERA_WORKER_IMAGE_VLLM:-}" >>"${TEST_CALLS}"
if [[ "${1:-}" == "cp" && -n "${TEST_CADDY_CONFIG:-}" ]]; then
  cp "$2" "${TEST_CADDY_CONFIG}"
fi
if [[ "$*" == *"exec -T caddy"*"/tmp/infera-maintenance.Caddyfile"* && "${TEST_MAINTENANCE_RELOAD_FAIL:-0}" == "1" ]]; then
  exit 1
fi
case "$*" in
  *"compose"*"ps -q gateway"*)
    printf '%s\n' gateway-container
    [[ "${TEST_GATEWAY_REPLICAS:-1}" == "2" ]] && printf '%s\n' gateway-container-2
    [[ "${TEST_DOCKER_PS_FAIL:-0}" != "1" ]] || exit 1
    ;;
  *"compose"*"ps -q caddy"*) printf '%s\n' caddy-container ;;
  *"inspect"*".State.Health"*) printf '%s\n' healthy ;;
  *"inspect"*)
    if [[ "$*" == *"gateway-container-2"* ]]; then
      printf '%s\n' "${TEST_GATEWAY_IP_2:-172.20.0.10}"
    else
      printf '%s\n' "${TEST_GATEWAY_IP:-172.20.0.9}"
    fi
    [[ "${TEST_GATEWAY_NETWORKS:-1}" == "2" ]] && printf '%s\n' 172.21.0.9
    [[ "${TEST_DOCKER_INSPECT_FAIL:-0}" != "1" ]] || exit 1
    ;;
esac
EOF

cat >"${TMP_DIR}/bin/mktemp" <<'EOF'
#!/usr/bin/env bash
set -eu
if [[ -z "${TEST_MKTEMP_COUNT:-}" ]]; then
  exec /usr/bin/mktemp "$@"
fi
count=0
[[ ! -f "${TEST_MKTEMP_COUNT}" ]] || count="$(cat "${TEST_MKTEMP_COUNT}")"
count=$((count + 1))
printf '%s\n' "${count}" >"${TEST_MKTEMP_COUNT}"
[[ "${count}" != "${TEST_MKTEMP_FAIL_AT:-0}" ]] || exit 1
path="$(/usr/bin/mktemp "$@")"
printf '%s\n' "${path}" >>"${TEST_MKTEMP_PATHS}"
printf '%s\n' "${path}"
EOF

cat >"${TMP_DIR}/bin/curl" <<'EOF'
#!/usr/bin/env bash
set -eu
printf 'curl:%s\n' "$*" >>"${TEST_CALLS}"
output_file=""
previous=""
for arg in "$@"; do
  if [[ "${previous}" == "--output" ]]; then output_file="${arg}"; fi
  previous="${arg}"
done
if [[ "$*" == *"api.runpod.io/graphql"* ]]; then
  body_file=""
  for arg in "$@"; do
    case "${arg}" in @*) body_file="${arg#@}" ;; esac
  done
  [[ -n "${body_file}" ]]
  if grep -q 'GetPods' "${body_file}"; then
    printf 'curl-graphql:GetPods\n' >>"${TEST_CALLS}"
    [[ "${TEST_RUNPOD_QUERY_FAIL:-0}" != "1" ]] || exit 22
    printf '{"data":{"myself":{"pods":'
    cat "${TEST_RUNPOD_STATE}"
    printf '}}}\n'
  else
    printf 'curl-graphql:podTerminate\n' >>"${TEST_CALLS}"
    [[ "${TEST_RUNPOD_TERMINATE_FAIL:-0}" != "1" ]] || exit 22
    python3 - "${TEST_RUNPOD_STATE}" "${body_file}" <<'PY'
import json
import sys

state_path, body_path = sys.argv[1:]
pods = json.load(open(state_path))
pod_id = json.load(open(body_path))["variables"]["input"]["podId"]
json.dump([pod for pod in pods if pod.get("id") != pod_id], open(state_path, "w"))
PY
    printf '%s\n' '{"data":{"podTerminate":true}}'
  fi
  exit 0
fi

case "$*" in
  *"-X POST"*)
    [[ "$*" == *'"name": "infera-release-release-1"'* ]]
    count=0
    [[ ! -f "${TEST_POST_COUNT}" ]] || count="$(cat "${TEST_POST_COUNT}")"
    count=$((count + 1))
    printf '%s\n' "${count}" >"${TEST_POST_COUNT}"
    mode="${TEST_PROVISION_MODE:-success}"
    status=201
    body='{"instance":{"id":"instance-1"}}'
    case "${mode}:${count}" in
      capacity_then_success:1)
        status=503
        body='{"error":{"provider":"runpod","provider_error_code":"capacity_unavailable","retryable":true}}'
        ;;
      capacity_with_orphan:1)
        status=503
        body='{"error":{"provider":"runpod","provider_error_code":"capacity_unavailable","retryable":true}}'
        printf '%s\n' '[{"id":"ambiguous-1","name":"infera-release-release-1","desiredStatus":"RUNNING"}]' >"${TEST_RUNPOD_STATE}"
        ;;
      capacity_nonretryable:*)
        status=503
        body='{"error":{"provider":"runpod","provider_error_code":"capacity_unavailable","retryable":false}}'
        ;;
      capacity_wrong_status:*)
        status=429
        body='{"error":{"provider":"runpod","provider_error_code":"capacity_unavailable","retryable":true}}'
        ;;
      unknown:*)
        status=503
        body='{"error":{"provider":"runpod","provider_error_code":"service_unavailable","retryable":true}}'
        ;;
      malformed_201:*) body='{"instance":{}}' ;;
      transport:*)
        printf '%s\n' '[{"id":"ambiguous-1","name":"infera-release-release-1","desiredStatus":"RUNNING"}]' >"${TEST_RUNPOD_STATE}"
        exit 22
        ;;
      runtime_attach_then_success:1)
        printf '%s\n' '[{"id":"attaching-1","name":"infera-release-release-1","desiredStatus":"RUNNING","runtime":null}]' >"${TEST_RUNPOD_STATE}"
        ;;
      runtime_active_no_registration:1)
        printf '%s\n' '[{"id":"active-1","name":"infera-release-release-1","desiredStatus":"RUNNING","runtime":{"uptimeInSeconds":10}}]' >"${TEST_RUNPOD_STATE}"
        ;;
      runtime_zero_no_registration:1)
        printf '%s\n' '[{"id":"active-1","name":"infera-release-release-1","desiredStatus":"RUNNING","runtime":{"uptimeInSeconds":0}}]' >"${TEST_RUNPOD_STATE}"
        ;;
      runtime_unknown_uptime_no_registration:1)
        printf '%s\n' '[{"id":"active-1","name":"infera-release-release-1","desiredStatus":"RUNNING","runtime":{}}]' >"${TEST_RUNPOD_STATE}"
        ;;
    esac
    if [[ -n "${output_file}" ]]; then
      printf '%s\n' "${body}" >"${output_file}"
      printf '%s' "${status}"
    else
      printf '%s\n' "${body}"
    fi
    ;;
  *"-X DELETE"*)
    [[ "${TEST_GATEWAY_DELETE_FAIL:-0}" != "1" ]] || exit 22
    printf '%s\n' '{"success":true}'
    ;;
  *"--write-out %{http_code}"*) printf '%s' 503 ;;
  *"/internal/prometheus/worker-targets"*) printf '%s\n' '[{"targets":["worker:8081"]}]' ;;
  *"/v1/models"*) printf '%s\n' '{"data":[]}' ;;
  *"/v1/chat/completions"*)
    if [[ "$*" == *'"stream": true'* ]]; then
      printf '%s\n' 'data: {"object":"chat.completion.chunk","choices":[{"delta":{"role":"assistant"}}]}'
      printf '%s\n' 'data: {"object":"chat.completion.chunk","choices":[{"delta":{"content":"ok"}}]}'
      printf '%s\n' 'data: [DONE]'
    else
      printf '%s\n' '{"object":"chat.completion","choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}'
    fi
    ;;
  *"/health"*)
    if [[ "${TEST_BAD_RECOVERY_PROTOCOL:-0}" == "1" ]]; then
      printf '%s\n' '{"status":"healthy","release_id":"release-1","worker_protocol_version":"1","recovery_api_protocol_version":"0"}'
    elif [[ "${TEST_BAD_HEALTH:-0}" == "1" ]]; then
      printf '%s\n' '{"release_id":"wrong-release","worker_protocol_version":"1","recovery_api_protocol_version":"1"}'
    else
      printf '%s\n' '{"status":"healthy","release_id":"release-1","worker_protocol_version":"1","recovery_api_protocol_version":"1"}'
    fi
    ;;
  *)
    if [[ "${TEST_MODE}" == "stop" ]]; then
      printf '%s\n' '{"instances":[{"id":"runpod-1","provider":"runpod","status":"running"},{"id":"vast-1","provider":"vastai","status":"running"}]}'
    elif [[ "${TEST_PROVISION_MODE:-}" == "runtime_attach_then_success" && "$(cat "${TEST_POST_COUNT}" 2>/dev/null || printf 0)" == "1" ]]; then
      printf '%s\n' '{"instances":[{"id":"instance-1","provider":"runpod","status":"provisioning"}]}'
    elif [[ "${TEST_PROVISION_MODE:-}" == runtime_*_no_registration ]]; then
      printf '%s\n' '{"instances":[{"id":"instance-1","provider":"runpod","status":"provisioning"}]}'
    else
      printf '%s\n' '{"instances":[{"id":"instance-1","provider":"runpod","status":"running","worker_id":"worker-1"}]}'
    fi
    ;;
esac
EOF
chmod +x "${TMP_DIR}/bin/docker" "${TMP_DIR}/bin/curl" "${TMP_DIR}/bin/mktemp"

export PATH="${TMP_DIR}/bin:${PATH}"
export TEST_CALLS="${TMP_DIR}/calls"
export TEST_CADDY_CONFIG="${TMP_DIR}/maintenance.Caddyfile"
export TEST_RUNPOD_STATE="${TMP_DIR}/runpod-state.json"
export TEST_POST_COUNT="${TMP_DIR}/post-count"
export INFERA_ENV_FILE="${TMP_DIR}/env"
export COMPOSE_FILE="docker-compose.prod.yml"
export INFERA_BASE_URL="https://inferai.co.in"
export INFERA_DASHBOARD_URL="https://dashboard.inferai.co.in"
export INFERA_RECOVERY_WORKER_MODEL="Qwen/Qwen2.5-7B-Instruct"

# A standalone adapter derives one fixed deadline instead of extending its
# budget on every remaining-time query.
(
  INFERA_RECOVERY_TIMEOUT_SECONDS=10
  source "${REPO_ROOT}/scripts/recovery-adapter-common.sh"
  first_deadline="$(recovery_deadline_epoch)"
  first_remaining="$(recovery_remaining_seconds)"
  sleep 1
  second_deadline="$(recovery_deadline_epoch)"
  second_remaining="$(recovery_remaining_seconds)"
  [[ "${first_deadline}" == "${second_deadline}" ]]
  (( second_remaining < first_remaining ))
)

printf '%s\n' '[]' >"${TEST_RUNPOD_STATE}"
: >"${TEST_CALLS}"
if TEST_RUNPOD_QUERY_FAIL=1 TEST_MODE=stop \
  "${REPO_ROOT}/scripts/runpod-stop-workers.sh" "${TMP_DIR}/release.manifest"; then
  echo "RunPod discovery failure must fail closed" >&2
  exit 1
fi

printf '%s\n' '[]' >"${TEST_RUNPOD_STATE}"
: >"${TMP_DIR}/mktemp-paths"
if TEST_MKTEMP_COUNT="${TMP_DIR}/mktemp-count" \
  TEST_MKTEMP_PATHS="${TMP_DIR}/mktemp-paths" \
  TEST_MKTEMP_FAIL_AT=2 \
  TEST_MODE=deploy \
  "${REPO_ROOT}/scripts/runpod-deploy-workers.sh" "${TMP_DIR}/release.manifest"; then
  echo "second bearer-config failure must fail deployment" >&2
  exit 1
fi
first_bearer_config="$(sed -n '1p' "${TMP_DIR}/mktemp-paths")"
[[ -n "${first_bearer_config}" && ! -e "${first_bearer_config}" ]] || {
  echo "first bearer config leaked after second config creation failed" >&2
  exit 1
}

printf '%s\n' '[{"id":"runpod-1","name":"infera-release-release-1","desiredStatus":"RUNNING"},{"id":"other-1","name":"customer-worker","desiredStatus":"RUNNING"}]' >"${TEST_RUNPOD_STATE}"
TEST_MODE=stop "${REPO_ROOT}/scripts/runpod-stop-workers.sh" "${TMP_DIR}/release.manifest"
grep -q 'customer-worker' "${TEST_RUNPOD_STATE}"
if grep -q 'infera-release-release-1' "${TEST_RUNPOD_STATE}"; then
  echo "stop adapter did not terminate the exact release-owned pod" >&2
  exit 1
fi

printf '%s\n' '[{"id":"runpod-1","name":"infera-release-release-1","desiredStatus":"RUNNING"}]' >"${TEST_RUNPOD_STATE}"
if TEST_RUNPOD_TERMINATE_FAIL=1 TEST_MODE=stop \
  "${REPO_ROOT}/scripts/runpod-stop-workers.sh" "${TMP_DIR}/release.manifest"; then
  echo "RunPod termination failure must stop cleanup immediately" >&2
  exit 1
fi
grep -q 'infera-release-release-1' "${TEST_RUNPOD_STATE}"
if grep -q 'test-runpod-key\|test-admin-key' "${TEST_CALLS}"; then
  echo "recovery adapter exposed a bearer token in process arguments" >&2
  exit 1
fi

run_fallback_case() {
  local mode="$1"
  shift
  printf '%s\n' '[]' >"${TEST_RUNPOD_STATE}"
  rm -f "${TEST_POST_COUNT}"
  : >"${TEST_CALLS}"
  TEST_MODE=deploy TEST_PROVISION_MODE="${mode}" "$@"
}

EVIDENCE_FILE="${TMP_DIR}/worker-evidence.log"
: >"${EVIDENCE_FILE}"
chmod 0600 "${EVIDENCE_FILE}"
run_fallback_case capacity_then_success env \
  INFERA_RECOVERY_EVIDENCE_FILE="${EVIDENCE_FILE}" \
  INFERA_RECOVERY_RELEASE_ID=release-1 \
  INFERA_RECOVERY_STEP=rollback.deploy-workers \
  INFERA_RECOVERY_WORKER_GPU_TYPES=RTX_4090,A100_80GB \
  "${REPO_ROOT}/scripts/runpod-deploy-workers.sh" "${TMP_DIR}/release.manifest"
[[ "$(cat "${TEST_POST_COUNT}")" == "2" ]]
first_gpu_line="$(grep -n 'gpu_type.*RTX_4090' "${TEST_CALLS}" | head -1 | cut -d: -f1)"
second_gpu_line="$(grep -n 'gpu_type.*A100_80GB' "${TEST_CALLS}" | head -1 | cut -d: -f1)"
[[ "${first_gpu_line}" -lt "${second_gpu_line}" ]]
grep -q 'result=fallback gpu=RTX_4090 attempt=1 reason=capacity_unavailable' "${EVIDENCE_FILE}"
grep -q 'result=pass gpu=A100_80GB attempt=2 reason=created' "${EVIDENCE_FILE}"
grep -q -- '--max-time 45 -X POST' "${TEST_CALLS}"

run_fallback_case capacity_with_orphan env \
  INFERA_RECOVERY_WORKER_GPU_TYPES=RTX_4090,A100_80GB \
  "${REPO_ROOT}/scripts/runpod-deploy-workers.sh" "${TMP_DIR}/release.manifest"
[[ "$(cat "${TEST_POST_COUNT}")" == "2" ]]
[[ "$(cat "${TEST_RUNPOD_STATE}")" == "[]" ]]

for terminal_mode in capacity_nonretryable capacity_wrong_status unknown malformed_201 transport; do
  if run_fallback_case "${terminal_mode}" env \
    INFERA_RECOVERY_WORKER_GPU_TYPES=RTX_4090,A100_80GB \
    "${REPO_ROOT}/scripts/runpod-deploy-workers.sh" "${TMP_DIR}/release.manifest"; then
    echo "${terminal_mode} must be terminal without GPU fallback" >&2
    exit 1
  fi
  [[ "$(cat "${TEST_POST_COUNT}")" == "1" ]]
  [[ "$(cat "${TEST_RUNPOD_STATE}")" == "[]" ]]
done

run_fallback_case success env \
  INFERA_RECOVERY_WORKER_GPU_TYPE=L40S \
  "${REPO_ROOT}/scripts/runpod-deploy-workers.sh" "${TMP_DIR}/release.manifest"
grep -q 'gpu_type.*L40S' "${TEST_CALLS}"
[[ "$(cat "${TEST_POST_COUNT}")" == "1" ]]

run_fallback_case runtime_attach_then_success env \
  INFERA_RECOVERY_EVIDENCE_FILE="${EVIDENCE_FILE}" \
  INFERA_RECOVERY_RELEASE_ID=release-1 \
  INFERA_RECOVERY_STEP=rollback.deploy-workers \
  INFERA_RECOVERY_REGISTRATION_ATTEMPT_SECONDS=1 \
  INFERA_RECOVERY_WORKER_GPU_TYPES=L40S,H100 \
  "${REPO_ROOT}/scripts/runpod-deploy-workers.sh" "${TMP_DIR}/release.manifest"
[[ "$(cat "${TEST_POST_COUNT}")" == "2" ]]
first_post_line="$(grep -n -- '-X POST' "${TEST_CALLS}" | sed -n '1s/:.*//p')"
delete_line="$(grep -n -- '-X DELETE' "${TEST_CALLS}" | sed -n '1s/:.*//p')"
terminate_line="$(grep -n 'curl-graphql:podTerminate' "${TEST_CALLS}" | sed -n '1s/:.*//p')"
second_post_line="$(grep -n -- '-X POST' "${TEST_CALLS}" | sed -n '2s/:.*//p')"
[[ "${first_post_line}" -lt "${delete_line}" && "${delete_line}" -lt "${terminate_line}" && "${terminate_line}" -lt "${second_post_line}" ]]
awk -v terminate="${terminate_line}" -v second_post="${second_post_line}" \
  'NR > terminate && NR < second_post && /curl-graphql:GetPods/ { found=1 } END { exit !found }' "${TEST_CALLS}"
grep -q 'event=registration result=fallback gpu=L40S attempt=1 reason=runtime_attachment_timeout' "${EVIDENCE_FILE}"

for attached_runtime_mode in runtime_active_no_registration runtime_zero_no_registration runtime_unknown_uptime_no_registration; do
  if run_fallback_case "${attached_runtime_mode}" env \
    INFERA_RECOVERY_REGISTRATION_ATTEMPT_SECONDS=1 \
    INFERA_RECOVERY_WORKER_GPU_TYPES=L40S,H100 \
    "${REPO_ROOT}/scripts/runpod-deploy-workers.sh" "${TMP_DIR}/release.manifest"; then
    echo "${attached_runtime_mode} must remain terminal without GPU fallback" >&2
    exit 1
  fi
  [[ "$(cat "${TEST_POST_COUNT}")" == "1" ]]
done

if run_fallback_case runtime_attach_then_success env \
  TEST_GATEWAY_DELETE_FAIL=1 \
  INFERA_RECOVERY_REGISTRATION_ATTEMPT_SECONDS=1 \
  INFERA_RECOVERY_WORKER_GPU_TYPES=L40S,H100 \
  "${REPO_ROOT}/scripts/runpod-deploy-workers.sh" "${TMP_DIR}/release.manifest"; then
  echo "gateway instance deletion failure must forbid fallback" >&2
  exit 1
fi
[[ "$(cat "${TEST_POST_COUNT}")" == "1" ]]

printf '%s\n' '[]' >"${TEST_RUNPOD_STATE}"
rm -f "${TEST_POST_COUNT}"
if TEST_BAD_RECOVERY_PROTOCOL=1 TEST_MODE=deploy \
  "${REPO_ROOT}/scripts/runpod-deploy-workers.sh" "${TMP_DIR}/release.manifest"; then
  echo "gateway recovery protocol mismatch must fail before provider mutation" >&2
  exit 1
fi
[[ ! -e "${TEST_POST_COUNT}" ]]

for invalid_gpu_types in 'RTX_4090,RTX_4090' 'RTX_4090,unknown' 'RTX_4090,RTX_4080,A100_40GB,A100_80GB,H100,L40S'; do
  if run_fallback_case success env INFERA_RECOVERY_WORKER_GPU_TYPES="${invalid_gpu_types}" \
    "${REPO_ROOT}/scripts/runpod-deploy-workers.sh" "${TMP_DIR}/release.manifest"; then
    echo "invalid GPU type list must fail before provisioning" >&2
    exit 1
  fi
  [[ ! -e "${TEST_POST_COUNT}" ]]
done

near_deadline="$(( $(date +%s) + 30 ))"
if run_fallback_case success env \
  INFERA_RECOVERY_DEADLINE_EPOCH="${near_deadline}" \
  INFERA_RECOVERY_MIN_ATTEMPT_BUDGET_SECONDS=60 \
  "${REPO_ROOT}/scripts/runpod-deploy-workers.sh" "${TMP_DIR}/release.manifest"; then
  echo "insufficient attempt budget must fail before provisioning" >&2
  exit 1
fi
[[ ! -e "${TEST_POST_COUNT}" ]]
if grep -q 'test-runpod-key\|test-admin-key\|provider-private-payload' "${EVIDENCE_FILE}"; then
  echo "worker recovery evidence exposed non-allowlisted content" >&2
  exit 1
fi
python3 - "${EVIDENCE_FILE}" <<'PY'
import re
import sys

pattern = re.compile(
    r"^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z WORKER_RECOVERY "
    r"event=(candidate_selected|provision_response|reconcile|registration) "
    r"result=(start|pass|fail|fallback|terminal) "
    r"gpu=(RTX_4090|RTX_4080|A100_40GB|A100_80GB|H100|L40S|-) "
    r"attempt=\d+ "
    r"reason=(none|capacity_unavailable|created|registered|deadline_exhausted|invalid_response|unknown_failure|transport_failure|state_not_empty|cleanup_failed|registration_timeout|runtime_attachment_timeout) "
    r"release=[A-Za-z0-9._:+/@-]+ step=[A-Za-z0-9._-]+$"
)
for line in open(sys.argv[1], encoding="ascii"):
    if not pattern.fullmatch(line.rstrip("\n")):
        raise SystemExit(f"unexpected worker evidence schema: {line!r}")
PY

: >"${TEST_CALLS}"
printf '%s\n' '[{"id":"orphan-1","name":"infera-release-release-1","desiredStatus":"RUNNING"}]' >"${TEST_RUNPOD_STATE}"
TEST_MODE=deploy "${REPO_ROOT}/scripts/runpod-deploy-workers.sh" "${TMP_DIR}/release.manifest"
grep -q 'api/instances/provision' "${TEST_CALLS}"
[[ "$(cat "${TEST_RUNPOD_STATE}")" == "[]" ]]
if grep -q 'test-runpod-key\|test-admin-key' "${TEST_CALLS}"; then
  echo "recovery adapter exposed a bearer token in process arguments" >&2
  exit 1
fi

: >"${TEST_CALLS}"
if TEST_GATEWAY_REPLICAS=2 \
INFERA_GATEWAY_REPLICAS=2 \
INFERA_RECOVERY_WORKER_ENGINE=vllm \
INFERA_WORKER_IMAGE_VLLM=example/stale-worker:old \
INFERA_CONTROL_STATE_DSN=postgresql://control.invalid/infera \
INFERA_AUDIT_LEDGER_BACKEND=postgres \
INFERA_AUDIT_LEDGER_DSN=postgresql://ledger.invalid/infera \
INFERA_PROVIDER_CREDENTIAL_ENCRYPTION_KEY=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA= \
INFERA_ADMIN_KEY=test-admin \
INFERA_ALLOWED_ORIGINS=https://example.com \
INFERA_GATEWAY_ADDRESS=https://example.com \
INFERA_WORKER_SHARED_TOKEN=test-worker \
INFERA_WORKER_IMAGE=example/worker:release-1 \
INFERA_GATEWAY_IMAGE=example/gateway:release-1 \
GRAFANA_ADMIN_USER=admin \
GRAFANA_ADMIN_PASSWORD=test-grafana \
ALERT_EMAIL_TO=alerts@example.com \
ALERT_SMTP_FROM=alerts@example.com \
ALERT_SMTP_SMARTHOST=smtp.example.com:587 \
ALERT_SMTP_USERNAME=alerts@example.com \
ALERT_SMTP_PASSWORD=test-smtp \
"${REPO_ROOT}/scripts/compose-release-driver.sh" deploy-gateway "${TMP_DIR}/release.manifest"; then
  :
else
  echo "durable multi-replica recovery deployment failed" >&2
  exit 1
fi
grep -q -- '--scale gateway=2 gateway' "${TEST_CALLS}"
grep -q 'docker-worker-vllm:example/worker:release-1' "${TEST_CALLS}"

if TEST_BAD_RECOVERY_PROTOCOL=1 \
INFERA_CONTROL_STATE_DSN=postgresql://control.invalid/infera \
INFERA_AUDIT_LEDGER_BACKEND=postgres \
INFERA_AUDIT_LEDGER_DSN=postgresql://ledger.invalid/infera \
INFERA_PROVIDER_CREDENTIAL_ENCRYPTION_KEY=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA= \
INFERA_ADMIN_KEY=test-admin \
INFERA_ALLOWED_ORIGINS=https://example.com \
INFERA_GATEWAY_ADDRESS=https://example.com \
INFERA_WORKER_SHARED_TOKEN=test-worker \
INFERA_WORKER_IMAGE=example/worker:release-1 \
INFERA_GATEWAY_IMAGE=example/gateway:release-1 \
GRAFANA_ADMIN_USER=admin \
GRAFANA_ADMIN_PASSWORD=test-grafana \
ALERT_EMAIL_TO=alerts@example.com \
ALERT_SMTP_FROM=alerts@example.com \
ALERT_SMTP_SMARTHOST=smtp.example.com:587 \
ALERT_SMTP_USERNAME=alerts@example.com \
ALERT_SMTP_PASSWORD=test-smtp \
"${REPO_ROOT}/scripts/compose-release-driver.sh" deploy-gateway "${TMP_DIR}/release.manifest"; then
  echo "gateway deployment must reject an incompatible recovery API protocol" >&2
  exit 1
fi

if INFERA_RECOVERY_WORKER_ENGINE=unsupported \
  "${REPO_ROOT}/scripts/compose-release-driver.sh" deploy-gateway "${TMP_DIR}/release.manifest"; then
  echo "unsupported recovery worker engine must fail closed" >&2
  exit 1
fi

if TEST_GATEWAY_NETWORKS=2 bash -c 'source "$1/scripts/recovery-adapter-common.sh"; recovery_gateway_url' _ "${REPO_ROOT}"; then
  echo "multiple gateway network addresses must fail closed" >&2
  exit 1
fi
[[ "$(TEST_GATEWAY_IP=fd00::1 bash -c 'source "$1/scripts/recovery-adapter-common.sh"; recovery_gateway_url' _ "${REPO_ROOT}")" == "http://[fd00::1]:8080" ]]
if TEST_GATEWAY_IP=2001:db8::1 bash -c 'source "$1/scripts/recovery-adapter-common.sh"; recovery_gateway_url' _ "${REPO_ROOT}"; then
  echo "public gateway address must fail closed" >&2
  exit 1
fi
if TEST_DOCKER_PS_FAIL=1 bash -c 'source "$1/scripts/recovery-adapter-common.sh"; recovery_gateway_urls' _ "${REPO_ROOT}"; then
  echo "partial gateway enumeration must fail closed" >&2
  exit 1
fi
if TEST_DOCKER_INSPECT_FAIL=1 bash -c 'source "$1/scripts/recovery-adapter-common.sh"; recovery_gateway_urls' _ "${REPO_ROOT}"; then
  echo "partial gateway inspection must fail closed" >&2
  exit 1
fi
gateway_urls="$(TEST_GATEWAY_REPLICAS=2 INFERA_GATEWAY_REPLICAS=2 bash -c 'source "$1/scripts/recovery-adapter-common.sh"; recovery_gateway_urls' _ "${REPO_ROOT}")"
[[ "$(printf '%s\n' "${gateway_urls}" | wc -l | tr -d ' ')" == "2" ]]
[[ "$(TEST_GATEWAY_REPLICAS=2 INFERA_GATEWAY_REPLICAS=2 bash -c 'source "$1/scripts/recovery-adapter-common.sh"; recovery_gateway_url' _ "${REPO_ROOT}")" == "http://172.20.0.9:8080" ]]
if TEST_GATEWAY_REPLICAS=2 INFERA_GATEWAY_REPLICAS=3 bash -c 'source "$1/scripts/recovery-adapter-common.sh"; recovery_gateway_urls' _ "${REPO_ROOT}"; then
  echo "gateway replica count mismatch must fail closed" >&2
  exit 1
fi

: >"${TEST_CALLS}"
TEST_GATEWAY_REPLICAS=2 \
INFERA_GATEWAY_REPLICAS=2 \
INFERA_EXPECT_TRAFFIC_DRAINED=1 \
INFERA_SMOKE_API_KEY=test-smoke-key \
INFERA_SMOKE_MODEL=test-model \
"${REPO_ROOT}/scripts/verify-release-manifest.sh" "${TMP_DIR}/release.manifest"
grep -q 'http://172.20.0.9:8080/v1/models' "${TEST_CALLS}"
grep -q 'http://172.20.0.10:8080/v1/models' "${TEST_CALLS}"
if TEST_GATEWAY_REPLICAS=2 \
INFERA_GATEWAY_REPLICAS=2 \
INFERA_EXPECT_TRAFFIC_DRAINED=1 \
INFERA_GATEWAY_INTERNAL_URL=http://172.20.0.9:8080 \
INFERA_SMOKE_API_KEY=test-smoke-key \
INFERA_SMOKE_MODEL=test-model \
"${REPO_ROOT}/scripts/verify-release-manifest.sh" "${TMP_DIR}/release.manifest"; then
  echo "drained verification override must not bypass replica enumeration" >&2
  exit 1
fi

: >"${TEST_CALLS}"
"${REPO_ROOT}/scripts/caddy-drain-traffic.sh" "${TMP_DIR}/release.manifest"
grep -q 'infera-maintenance.Caddyfile' "${TEST_CALLS}"
grep -q 'caddy reload --config /tmp/infera-maintenance.Caddyfile' "${TEST_CALLS}"
grep -q 'path /api/workers/register /api/workers/heartbeat' "${TEST_CADDY_CONFIG}"
grep -q 'header X-Worker-Token \*' "${TEST_CADDY_CONFIG}"
grep -q 'header_regexp Authorization \^Bearer' "${TEST_CADDY_CONFIG}"
[[ "$(grep -c 'reverse_proxy gateway:8080' "${TEST_CADDY_CONFIG}")" == "2" ]]
grep -q 'respond "Service temporarily unavailable" 503' "${TEST_CADDY_CONFIG}"
if grep -qE 'path /api/\*|handle /api/\*|reverse_proxy frontend:3000' "${TEST_CADDY_CONFIG}"; then
  echo "maintenance config exposed non-worker application routes" >&2
  exit 1
fi

: >"${TEST_CALLS}"
if TEST_BAD_HEALTH=1 TEST_MAINTENANCE_RELOAD_FAIL=1 \
  "${REPO_ROOT}/scripts/caddy-restore-traffic.sh" "${TMP_DIR}/release.manifest"; then
  echo "expected maintenance reload failure to fail restoration" >&2
  exit 1
fi
grep -q 'compose -f docker-compose.prod.yml stop caddy' "${TEST_CALLS}"

: >"${TEST_CALLS}"
"${REPO_ROOT}/scripts/caddy-restore-traffic.sh" "${TMP_DIR}/release.manifest"
grep -q 'caddy reload --config /etc/caddy/Caddyfile' "${TEST_CALLS}"

: >"${TEST_CALLS}"
if TEST_BAD_HEALTH=1 "${REPO_ROOT}/scripts/caddy-restore-traffic.sh" "${TMP_DIR}/release.manifest"; then
  echo "expected invalid public release identity to fail restoration" >&2
  exit 1
fi
grep -q 'caddy reload --config /tmp/infera-maintenance.Caddyfile' "${TEST_CALLS}"

echo "Production recovery adapter tests passed."
