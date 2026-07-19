#!/usr/bin/env bash
# Fail closed when the recorded last-known-good release differs from the live Compose gateway.

set -euo pipefail

MANIFEST="${1:?usage: check-last-known-good.sh <last-known-good.manifest>}"
COMPOSE_FILE="${COMPOSE_FILE:-docker-compose.prod.yml}"
ACTIVE_LEDGER_PROTOCOL="${INFERA_ACTIVE_AUDIT_LEDGER_WRITER_PROTOCOL:-}"
WORKER_ENGINE="${INFERA_RECOVERY_WORKER_ENGINE:-vllm}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=recovery-adapter-common.sh
source "${SCRIPT_DIR}/recovery-adapter-common.sh"

manifest_value() {
  local key="$1" value
  value="$(recovery_manifest_value "${MANIFEST}" "${key}")" || {
    echo "ERROR: recovery manifest must contain exactly one ${key}" >&2
    exit 2
  }
  [[ "${value}" =~ ^[A-Za-z0-9._:/@+-]+$ ]] || {
    echo "ERROR: recovery manifest contains an unsafe ${key}" >&2
    exit 2
  }
  printf '%s\n' "${value}"
}

container_env_value() {
  local container_id="$1" key="$2"
  docker inspect --format '{{range .Config.Env}}{{println .}}{{end}}' "${container_id}" |
    awk -F= -v wanted="${key}" '$1 == wanted { count++; value=substr($0, index($0, "=") + 1) } END { if (count != 1) exit 1; print value }'
}

[[ -f "${MANIFEST}" ]] || {
  echo "ERROR: last-known-good manifest not found" >&2
  exit 2
}
[[ -n "${ACTIVE_LEDGER_PROTOCOL}" ]] || {
  echo "ERROR: INFERA_ACTIVE_AUDIT_LEDGER_WRITER_PROTOCOL is required" >&2
  exit 2
}

expected_release="$(manifest_value INFERA_RELEASE_ID)"
expected_gateway_image="$(manifest_value INFERA_GATEWAY_IMAGE)"
expected_worker_image="$(manifest_value INFERA_WORKER_IMAGE)"
manifest_value INFERA_WORKER_PROTOCOL_VERSION >/dev/null
manifest_value INFERA_RECOVERY_API_PROTOCOL_VERSION >/dev/null
expected_ledger_protocol="$(manifest_value INFERA_AUDIT_LEDGER_WRITER_PROTOCOL)"

[[ "${expected_ledger_protocol}" == "${ACTIVE_LEDGER_PROTOCOL}" ]] || {
  echo "ERROR: active audit-ledger writer protocol differs from last-known-good manifest" >&2
  exit 1
}

case "${WORKER_ENGINE}" in
  vllm) worker_image_key=INFERA_WORKER_IMAGE_VLLM ;;
  sglang) worker_image_key=INFERA_WORKER_IMAGE_SGLANG ;;
  tensorrt_llm) worker_image_key=INFERA_WORKER_IMAGE_TENSORRT_LLM ;;
  mock) worker_image_key=INFERA_WORKER_IMAGE_MOCK ;;
  *)
    echo "ERROR: unsupported recovery worker engine" >&2
    exit 2
    ;;
esac

recovery_assert_gateway_identity "${MANIFEST}" || {
  echo "ERROR: live gateway release or protocol differs from last-known-good manifest" >&2
  exit 1
}

gateway_ids=()
gateway_ids_output="$(docker compose -f "${COMPOSE_FILE}" ps -q gateway)" || {
  echo "ERROR: unable to enumerate running gateway containers" >&2
  exit 1
}
while IFS= read -r gateway_id; do
  [[ -n "${gateway_id}" ]] && gateway_ids+=("${gateway_id}")
done <<<"${gateway_ids_output}"
[[ "${#gateway_ids[@]}" -gt 0 ]] || {
  echo "ERROR: no running gateway containers found" >&2
  exit 1
}

for gateway_id in "${gateway_ids[@]}"; do
  actual_gateway_image="$(docker inspect --format '{{.Config.Image}}' "${gateway_id}")" || exit 1
  [[ "${actual_gateway_image}" == "${expected_gateway_image}" ]] || {
    echo "ERROR: running gateway image differs from last-known-good manifest" >&2
    exit 1
  }
  actual_worker_image="$(container_env_value "${gateway_id}" "${worker_image_key}")" || {
    echo "ERROR: running gateway is missing the selected worker image" >&2
    exit 1
  }
  [[ "${actual_worker_image}" == "${expected_worker_image}" ]] || {
    echo "ERROR: running gateway worker image differs from last-known-good manifest" >&2
    exit 1
  }
done

printf 'last-known-good manifest matches live release %s across %s gateway replica(s)\n' \
  "${expected_release}" "${#gateway_ids[@]}"
