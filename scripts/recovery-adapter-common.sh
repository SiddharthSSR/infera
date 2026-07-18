#!/usr/bin/env bash

recovery_now_epoch() {
  date +%s
}

RECOVERY_START_EPOCH="$(recovery_now_epoch)"

recovery_deadline_epoch() {
  local now timeout deadline
  now="$(recovery_now_epoch)"
  if [[ -n "${INFERA_RECOVERY_DEADLINE_EPOCH:-}" ]]; then
    deadline="${INFERA_RECOVERY_DEADLINE_EPOCH}"
    [[ "${deadline}" =~ ^[0-9]+$ && "${deadline}" -gt "${now}" && $((deadline - now)) -le 900 ]] || return 1
    printf '%s\n' "${deadline}"
    return
  fi
  timeout="${INFERA_RECOVERY_TIMEOUT_SECONDS:-900}"
  [[ "${timeout}" =~ ^[1-9][0-9]*$ && "${timeout}" -le 900 ]] || return 1
  printf '%s\n' "$((RECOVERY_START_EPOCH + timeout))"
}

recovery_is_candidate_step() {
  [[ "${INFERA_RECOVERY_STEP:-}" == candidate.* ]]
}

recovery_remaining_seconds() {
  local reserve="${1:-0}" deadline now remaining
  [[ "${reserve}" =~ ^[0-9]+$ ]] || return 1
  deadline="$(recovery_deadline_epoch)" || return 1
  now="$(recovery_now_epoch)"
  remaining=$((deadline - now - reserve))
  (( remaining > 0 )) || return 1
  printf '%s\n' "${remaining}"
}

recovery_bounded_timeout() {
  local requested="$1" reserve="${2:-0}" remaining
  [[ "${requested}" =~ ^[1-9][0-9]*$ ]] || return 1
  remaining="$(recovery_remaining_seconds "${reserve}")" || return 1
  (( requested < remaining )) && printf '%s\n' "${requested}" || printf '%s\n' "${remaining}"
}

recovery_deadline_sleep() {
  local requested="$1" reserve="${2:-0}" remaining
  [[ "${requested}" =~ ^[1-9][0-9]*$ ]] || return 1
  remaining="$(recovery_remaining_seconds "${reserve}")" || return 1
  (( remaining > requested )) || return 1
  sleep "${requested}"
}

recovery_record_worker_evidence() {
  local event="$1" result="$2" gpu="$3" attempt="$4" reason="$5"
  local evidence_file="${INFERA_RECOVERY_EVIDENCE_FILE:-}"
  local release="${INFERA_RECOVERY_RELEASE_ID:-unknown}"
  local step="${INFERA_RECOVERY_STEP:-standalone}"
  [[ -n "${evidence_file}" ]] || return 0
  python3 - "${evidence_file}" "${event}" "${result}" "${gpu}" "${attempt}" "${reason}" "${release}" "${step}" <<'PY'
import datetime
import os
import re
import stat
import sys

path, event, result, gpu, attempt, reason, release, step = sys.argv[1:]
allowed_events = {"candidate_selected", "provision_response", "reconcile", "registration"}
allowed_results = {"start", "pass", "fail", "fallback", "terminal"}
allowed_reasons = {
    "none", "capacity_unavailable", "created", "registered", "deadline_exhausted",
    "invalid_response", "unknown_failure", "transport_failure", "state_not_empty",
    "cleanup_failed", "registration_timeout", "runtime_attachment_timeout",
}
if event not in allowed_events or result not in allowed_results or reason not in allowed_reasons:
    raise SystemExit(1)
if not re.fullmatch(r"(?:RTX_4090|RTX_4080|A100_40GB|A100_80GB|H100|L40S|-)", gpu):
    raise SystemExit(1)
if not re.fullmatch(r"[0-9]+", attempt):
    raise SystemExit(1)
if not re.fullmatch(r"[A-Za-z0-9._:+/@-]+", release):
    raise SystemExit(1)
if not re.fullmatch(r"[A-Za-z0-9._-]+", step):
    raise SystemExit(1)
flags = os.O_WRONLY | os.O_APPEND
if not hasattr(os, "O_NOFOLLOW"):
    raise SystemExit(1)
fd = os.open(path, flags | os.O_NOFOLLOW)
try:
    status = os.fstat(fd)
    if not stat.S_ISREG(status.st_mode) or stat.S_IMODE(status.st_mode) != 0o600:
        raise SystemExit(1)
    timestamp = datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    line = (
        f"{timestamp} WORKER_RECOVERY event={event} result={result} gpu={gpu} "
        f"attempt={attempt} reason={reason} release={release} step={step}\n"
    )
    os.write(fd, line.encode("ascii"))
finally:
    os.close(fd)
PY
}

recovery_manifest_value() {
  local manifest="$1"
  local key="$2"
  awk -F= -v wanted="${key}" '$1 == wanted { count++; value=substr($0, index($0, "=") + 1) } END { if (count != 1) exit 1; print value }' "${manifest}"
}

recovery_env_value() {
  local key="$1"
  local env_file="${INFERA_ENV_FILE:-.env}"
  if [[ -n "${!key:-}" ]]; then
    printf '%s\n' "${!key}"
    return
  fi
  awk -F= -v wanted="${key}" '$1 == wanted { count++; value=substr($0, index($0, "=") + 1) } END { if (count != 1 || value == "") exit 1; print value }' "${env_file}"
}

recovery_configured_gateway_replicas() {
  if [[ -n "${INFERA_GATEWAY_REPLICAS:-}" ]]; then
    printf '%s\n' "${INFERA_GATEWAY_REPLICAS}"
    return
  fi
  local env_file="${INFERA_ENV_FILE:-.env}"
  awk -F= '$1 == "INFERA_GATEWAY_REPLICAS" { count++; value=substr($0, index($0, "=") + 1) } END { if (count == 1 && value != "") print value; else print "1" }' "${env_file}"
}

recovery_gateway_urls() {
  local compose_file="${COMPOSE_FILE:-docker-compose.prod.yml}"
  local gateway_id
  local gateway_ip
  local gateway_ids_output
  local gateway_ips_output
  local expected_replicas
  local gateway_ids=()
  local gateway_ips=()
  local gateway_urls=()
  expected_replicas="$(recovery_configured_gateway_replicas)"
  [[ "${expected_replicas}" =~ ^[1-9][0-9]*$ ]] || return 1
  gateway_ids_output="$(docker compose -f "${compose_file}" ps -q gateway)" || return 1
  while IFS= read -r gateway_id; do
    [[ -n "${gateway_id}" ]] && gateway_ids[${#gateway_ids[@]}]="${gateway_id}"
  done <<<"${gateway_ids_output}"
  [[ "${#gateway_ids[@]}" -eq "${expected_replicas}" ]] || return 1
  for gateway_id in "${gateway_ids[@]}"; do
    gateway_ips=()
    gateway_ips_output="$(docker inspect --format '{{range .NetworkSettings.Networks}}{{println .IPAddress}}{{end}}' "${gateway_id}")" || return 1
    while IFS= read -r gateway_ip; do
      [[ -n "${gateway_ip}" ]] && gateway_ips[${#gateway_ips[@]}]="${gateway_ip}"
    done <<<"${gateway_ips_output}"
    [[ "${#gateway_ips[@]}" -eq 1 ]] || return 1
    gateway_ip="${gateway_ips[0]}"
    python3 - "${gateway_ip}" <<'PY' >/dev/null || return 1
import ipaddress
import sys

address = ipaddress.ip_address(sys.argv[1])
allowed = (
    ipaddress.ip_network("10.0.0.0/8"),
    ipaddress.ip_network("172.16.0.0/12"),
    ipaddress.ip_network("192.168.0.0/16"),
    ipaddress.ip_network("fc00::/7"),
)
if not any(address in network for network in allowed):
    raise SystemExit(1)
PY
    [[ "${gateway_ip}" == *:* ]] && gateway_ip="[${gateway_ip}]"
    gateway_urls[${#gateway_urls[@]}]="http://${gateway_ip}:8080"
  done
  printf '%s\n' "${gateway_urls[@]}"
}

recovery_gateway_url() {
  local gateway_urls
  gateway_urls="$(recovery_gateway_urls)" || return 1
  printf '%s\n' "${gateway_urls%%$'\n'*}"
}

recovery_assert_gateway_identity() {
  local manifest="$1" expected_release expected_worker_protocol expected_recovery_protocol
  local gateway_url gateway_urls timeout health_body checked=0
  expected_release="$(recovery_manifest_value "${manifest}" INFERA_RELEASE_ID)" || return 1
  expected_worker_protocol="$(recovery_manifest_value "${manifest}" INFERA_WORKER_PROTOCOL_VERSION)" || return 1
  expected_recovery_protocol="$(recovery_manifest_value "${manifest}" INFERA_RECOVERY_API_PROTOCOL_VERSION)" || return 1
  gateway_urls="$(recovery_gateway_urls)" || return 1
  while IFS= read -r gateway_url; do
    [[ -n "${gateway_url}" ]] || continue
    checked=$((checked + 1))
    timeout="$(recovery_bounded_timeout 15 "${INFERA_RECOVERY_CLEANUP_RESERVE_SECONDS:-0}")" || return 1
    health_body="$(curl --fail --silent --show-error --max-time "${timeout}" "${gateway_url}/health")" || return 1
    HEALTH_BODY="${health_body}" EXPECTED_RELEASE="${expected_release}" \
      EXPECTED_WORKER_PROTOCOL="${expected_worker_protocol}" \
      EXPECTED_RECOVERY_PROTOCOL="${expected_recovery_protocol}" python3 - <<'PY' || return 1
import json
import os

payload = json.loads(os.environ["HEALTH_BODY"])
if payload.get("release_id") != os.environ["EXPECTED_RELEASE"]:
    raise SystemExit("gateway release does not match recovery manifest")
if payload.get("worker_protocol_version") != os.environ["EXPECTED_WORKER_PROTOCOL"]:
    raise SystemExit("gateway worker protocol does not match recovery manifest")
if payload.get("recovery_api_protocol_version") != os.environ["EXPECTED_RECOVERY_PROTOCOL"]:
    raise SystemExit("gateway recovery API protocol does not match recovery manifest")
PY
  done <<<"${gateway_urls}"
  (( checked > 0 ))
}

recovery_https_host() {
  local url="$1"
  URL="${url}" python3 - <<'PY'
import os
import re
from urllib.parse import urlsplit

parsed = urlsplit(os.environ["URL"])
host = parsed.hostname or ""
if (
    parsed.scheme != "https"
    or parsed.username
    or parsed.password
    or parsed.port not in (None, 443)
    or parsed.path not in ("", "/")
    or parsed.query
    or parsed.fragment
    or not re.fullmatch(r"[A-Za-z0-9.-]+", host)
):
    raise SystemExit("expected a public HTTPS origin URL")
print(host.lower())
PY
}

recovery_bearer_config() {
  local token="$1"
  local config
  [[ -n "${token}" && "${token}" != *$'\n'* && "${token}" != *$'\r'* && "${token}" != *'"'* && "${token}" != *'\'* ]] || return 1
  if ! config="$(mktemp)"; then
    return 1
  fi
  if ! chmod 600 "${config}" || ! printf 'header = "Authorization: Bearer %s"\n' "${token}" >"${config}"; then
    rm -f "${config}"
    return 1
  fi
  printf '%s\n' "${config}"
}

recovery_runpod_ids_by_name() {
  local name="$1"
  local curl_config="$2"
  local payload response timeout
  payload="$(mktemp)"
  QUERY='query GetPods { myself { pods { id name desiredStatus runtime { uptimeInSeconds } } } }' python3 - <<'PY' >"${payload}"
import json
import os

print(json.dumps({"query": os.environ["QUERY"]}))
PY
  timeout="$(recovery_bounded_timeout 30 "${INFERA_RECOVERY_CLEANUP_RESERVE_SECONDS:-0}")" || { rm -f "${payload}"; return 1; }
  if ! response="$(curl --fail --silent --show-error --max-time "${timeout}" \
    --config "${curl_config}" -H 'Content-Type: application/json' \
    --data-binary "@${payload}" https://api.runpod.io/graphql)"; then
    rm -f "${payload}"
    return 1
  fi
  rm -f "${payload}"
  RESPONSE="${response}" POD_NAME="${name}" python3 - <<'PY'
import json
import os

payload = json.loads(os.environ["RESPONSE"])
if payload.get("errors"):
    raise SystemExit("RunPod pod query failed")
for pod in payload.get("data", {}).get("myself", {}).get("pods", []):
    if pod.get("name") == os.environ["POD_NAME"]:
        print(pod["id"])
PY
}

# Print "attaching" only when exactly one exact-name pod exists and RunPod has
# not attached a runtime. Every other state is ambiguous to the recovery
# controller and therefore ineligible for GPU fallback.
recovery_runpod_named_pod_attachment_state() {
  local name="$1" curl_config="$2" payload response timeout
  payload="$(mktemp)"
  QUERY='query GetPods { myself { pods { id name desiredStatus runtime { uptimeInSeconds } } } }' python3 - <<'PY' >"${payload}"
import json
import os

print(json.dumps({"query": os.environ["QUERY"]}))
PY
  timeout="$(recovery_bounded_timeout 30 "${INFERA_RECOVERY_CLEANUP_RESERVE_SECONDS:-0}")" || { rm -f "${payload}"; return 1; }
  if ! response="$(curl --fail --silent --show-error --max-time "${timeout}" \
    --config "${curl_config}" -H 'Content-Type: application/json' \
    --data-binary "@${payload}" https://api.runpod.io/graphql)"; then
    rm -f "${payload}"
    return 1
  fi
  rm -f "${payload}"
  RESPONSE="${response}" POD_NAME="${name}" python3 - <<'PY'
import json
import os

payload = json.loads(os.environ["RESPONSE"])
if payload.get("errors"):
    raise SystemExit(1)
pods = [
    pod for pod in payload.get("data", {}).get("myself", {}).get("pods", [])
    if pod.get("name") == os.environ["POD_NAME"]
]
if len(pods) != 1:
    raise SystemExit(1)
runtime = pods[0].get("runtime")
if runtime is None:
    print("attaching")
    raise SystemExit(0)
raise SystemExit(1)
PY
}

recovery_runpod_terminate() {
  local pod_id="$1"
  local curl_config="$2"
  local payload timeout curl_status=0
  [[ "${pod_id}" =~ ^[A-Za-z0-9._:-]+$ ]] || return 1
  payload="$(mktemp)"
  POD_ID="${pod_id}" python3 - <<'PY' >"${payload}"
import json
import os

print(json.dumps({
    "query": "mutation TerminatePod($input: PodTerminateInput!) { podTerminate(input: $input) }",
    "variables": {"input": {"podId": os.environ["POD_ID"]}},
}))
PY
  timeout="$(recovery_bounded_timeout 120 "${INFERA_RECOVERY_CLEANUP_RESERVE_SECONDS:-0}")" || { rm -f "${payload}"; return 1; }
  curl --fail --silent --show-error --max-time "${timeout}" \
    --config "${curl_config}" -H 'Content-Type: application/json' \
    --data-binary "@${payload}" https://api.runpod.io/graphql >/dev/null || curl_status=$?
  rm -f "${payload}"
  return "${curl_status}"
}

recovery_runpod_remove_named_pods() {
  local name="$1"
  local curl_config="$2"
  local pod_id
  local discovered
  local id_count=0
  local index
  local ids=()
  if ! discovered="$(recovery_runpod_ids_by_name "${name}" "${curl_config}")"; then
    echo "ERROR: unable to reconcile release-owned RunPod workers" >&2
    return 1
  fi
  while IFS= read -r pod_id; do
    if [[ -n "${pod_id}" ]]; then
      ids[${id_count}]="${pod_id}"
      id_count=$((id_count + 1))
    fi
  done <<<"${discovered}"
  for ((index = 0; index < id_count; index++)); do
    pod_id="${ids[${index}]}"
    if ! recovery_runpod_terminate "${pod_id}" "${curl_config}"; then
      echo "ERROR: unable to terminate release-owned RunPod worker" >&2
      return 1
    fi
  done
  for _ in $(seq 1 30); do
    if ! discovered="$(recovery_runpod_ids_by_name "${name}" "${curl_config}")"; then
      echo "ERROR: unable to verify release-owned RunPod workers terminated" >&2
      return 1
    fi
    if [[ -z "${discovered}" ]]; then
      printf '%s\n' "${id_count}"
      return 0
    fi
    recovery_deadline_sleep 2 "${INFERA_RECOVERY_CLEANUP_RESERVE_SECONDS:-0}" || return 1
  done
  echo "ERROR: release-owned RunPod workers did not terminate" >&2
  return 1
}

recovery_runpod_confirm_named_pods_absent() {
  local name="$1" curl_config="$2" discovered
  discovered="$(recovery_runpod_ids_by_name "${name}" "${curl_config}")" || return 1
  [[ -z "${discovered}" ]]
}
