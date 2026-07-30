#!/usr/bin/env bash

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT
mkdir -p "${TMP_DIR}/bin"

cat >"${TMP_DIR}/release.manifest" <<'EOF'
INFERA_RELEASE_ID=release-1
INFERA_GATEWAY_IMAGE=example/gateway@sha256:111
INFERA_WORKER_IMAGE=example/worker@sha256:222
INFERA_WORKER_PROTOCOL_VERSION=1
INFERA_RECOVERY_API_PROTOCOL_VERSION=1
INFERA_AUDIT_LEDGER_WRITER_PROTOCOL=2
EOF

cat >"${TMP_DIR}/bin/docker" <<'EOF'
#!/usr/bin/env bash
set -eu
case "$*" in
  *"compose"*"ps -q gateway"*)
    printf '%s\n' gateway-1
    if [[ "${TEST_GATEWAY_REPLICAS:-1}" == "2" ]]; then
      printf '%s\n' gateway-2
    fi
    ;;
  *"{{range .NetworkSettings.Networks}}"*)
    if [[ "$*" == *"gateway-2"* ]]; then
      if [[ "${TEST_GATEWAY_IP_2_MODE:-}" != "missing" ]]; then
        printf '%s\n' "${TEST_GATEWAY_IP_2:-100.64.0.10}"
      fi
    else
      printf '%s\n' "${TEST_GATEWAY_IP_1:-100.64.0.9}"
    fi
    ;;
  *"{{.Config.Image}}"*) printf '%s\n' "${TEST_GATEWAY_IMAGE:-example/gateway@sha256:111}" ;;
  *"{{range .Config.Env}}"*) printf '%s\n' "INFERA_WORKER_IMAGE_VLLM=${TEST_WORKER_IMAGE:-example/worker@sha256:222}" ;;
  *) exit 1 ;;
esac
EOF
chmod +x "${TMP_DIR}/bin/docker"

cat >"${TMP_DIR}/bin/curl" <<'EOF'
#!/usr/bin/env bash
set -eu
if [[ -n "${TEST_UNREACHABLE_GATEWAY_IP:-}" && "$*" == *"${TEST_UNREACHABLE_GATEWAY_IP}"* ]]; then
  exit 7
fi
printf '%s\n' "{\"release_id\":\"${TEST_RELEASE_ID:-release-1}\",\"worker_protocol_version\":\"1\",\"recovery_api_protocol_version\":\"1\"}"
EOF
chmod +x "${TMP_DIR}/bin/curl"

run_check() {
  PATH="${TMP_DIR}/bin:${PATH}" \
    INFERA_GATEWAY_REPLICAS="${TEST_EXPECTED_GATEWAY_REPLICAS:-1}" \
    INFERA_ACTIVE_AUDIT_LEDGER_WRITER_PROTOCOL="${TEST_LEDGER_PROTOCOL:-2}" \
    "${REPO_ROOT}/scripts/check-last-known-good.sh" "${TMP_DIR}/release.manifest"
}

run_check | grep -q 'matches live release release-1 across 1 gateway replica'

TEST_GATEWAY_REPLICAS=2 TEST_EXPECTED_GATEWAY_REPLICAS=2 \
  run_check | grep -q 'matches live release release-1 across 2 gateway replica'

if TEST_GATEWAY_REPLICAS=2 TEST_EXPECTED_GATEWAY_REPLICAS=2 \
  TEST_GATEWAY_IP_2=100.64.0.9 run_check >/dev/null 2>&1; then
  echo "duplicate gateway URLs must fail closed" >&2
  exit 1
fi
if TEST_GATEWAY_REPLICAS=2 TEST_EXPECTED_GATEWAY_REPLICAS=2 \
  TEST_GATEWAY_IP_2_MODE=missing run_check >/dev/null 2>&1; then
  echo "missing gateway URL must fail closed" >&2
  exit 1
fi
if TEST_GATEWAY_REPLICAS=2 TEST_EXPECTED_GATEWAY_REPLICAS=2 \
  TEST_GATEWAY_IP_2=not-an-ip run_check >/dev/null 2>&1; then
  echo "malformed gateway URL must fail closed" >&2
  exit 1
fi
if TEST_GATEWAY_REPLICAS=2 TEST_EXPECTED_GATEWAY_REPLICAS=2 \
  TEST_UNREACHABLE_GATEWAY_IP=100.64.0.10 run_check >/dev/null 2>&1; then
  echo "unreachable gateway URL must fail closed" >&2
  exit 1
fi

if TEST_RELEASE_ID=stale run_check >/dev/null 2>&1; then
  echo "live release drift must fail closed" >&2
  exit 1
fi
if TEST_GATEWAY_IMAGE=example/gateway@sha256:999 run_check >/dev/null 2>&1; then
  echo "gateway image drift must fail closed" >&2
  exit 1
fi
if TEST_WORKER_IMAGE=example/worker@sha256:999 run_check >/dev/null 2>&1; then
  echo "worker image drift must fail closed" >&2
  exit 1
fi
if TEST_LEDGER_PROTOCOL=999 run_check >/dev/null 2>&1; then
  echo "ledger protocol drift must fail closed" >&2
  exit 1
fi

echo "last-known-good drift checks passed"
