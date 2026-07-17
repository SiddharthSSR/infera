#!/usr/bin/env bash

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
  local payload response
  payload="$(mktemp)"
  QUERY='query GetPods { myself { pods { id name desiredStatus } } }' python3 - <<'PY' >"${payload}"
import json
import os

print(json.dumps({"query": os.environ["QUERY"]}))
PY
  if ! response="$(curl --fail --silent --show-error --max-time 30 \
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

recovery_runpod_terminate() {
  local pod_id="$1"
  local curl_config="$2"
  local payload
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
  curl --fail --silent --show-error --max-time 120 \
    --config "${curl_config}" -H 'Content-Type: application/json' \
    --data-binary "@${payload}" https://api.runpod.io/graphql >/dev/null
  rm -f "${payload}"
}

recovery_runpod_remove_named_pods() {
  local name="$1"
  local curl_config="$2"
  local pod_id
  local discovered
  local ids=()
  if ! discovered="$(recovery_runpod_ids_by_name "${name}" "${curl_config}")"; then
    echo "ERROR: unable to reconcile release-owned RunPod workers" >&2
    return 1
  fi
  while IFS= read -r pod_id; do
    [[ -n "${pod_id}" ]] && ids[${#ids[@]}]="${pod_id}"
  done <<<"${discovered}"
  for pod_id in "${ids[@]}"; do
    recovery_runpod_terminate "${pod_id}" "${curl_config}"
  done
  for _ in $(seq 1 30); do
    if ! discovered="$(recovery_runpod_ids_by_name "${name}" "${curl_config}")"; then
      echo "ERROR: unable to verify release-owned RunPod workers terminated" >&2
      return 1
    fi
    if [[ -z "${discovered}" ]]; then
      printf '%s\n' "${#ids[@]}"
      return 0
    fi
    sleep 2
  done
  echo "ERROR: release-owned RunPod workers did not terminate" >&2
  return 1
}
