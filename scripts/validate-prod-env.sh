#!/usr/bin/env bash
# validate-prod-env.sh - verify production env names are present without printing secrets.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=production-env-source.sh
source "${SCRIPT_DIR}/production-env-source.sh"

lookup_env() {
  local name="$1"
  production_env_value "${name}"
}

production_env_validate

worker_image="$(lookup_env INFERA_WORKER_IMAGE)"
bash "$(dirname "$0")/validate-worker-image-pin.sh" "${worker_image}"
gateway_image="$(lookup_env INFERA_GATEWAY_IMAGE)"
bash "$(dirname "$0")/validate-worker-image-pin.sh" "${gateway_image}" "INFERA_GATEWAY_IMAGE"

gateway_replicas="$(lookup_env INFERA_GATEWAY_REPLICAS 2>/dev/null || printf '1')"
audit_backend="$(lookup_env INFERA_AUDIT_LEDGER_BACKEND 2>/dev/null || printf 'sqlite')"
audit_backend="$(printf '%s' "${audit_backend}" | tr '[:upper:]' '[:lower:]')"
if [[ ! "${gateway_replicas}" =~ ^[1-9][0-9]*$ ]]; then
  echo "ERROR: INFERA_GATEWAY_REPLICAS must be a positive integer." >&2
  exit 1
fi
if [[ "${audit_backend}" != "sqlite" && "${audit_backend}" != "postgres" && "${audit_backend}" != "postgresql" ]]; then
  echo "ERROR: INFERA_AUDIT_LEDGER_BACKEND=${audit_backend} is not supported by this release." >&2
  exit 1
fi
if (( gateway_replicas > 1 )) && [[ "${audit_backend}" == "sqlite" ]]; then
  echo "ERROR: multiple gateway replicas require INFERA_AUDIT_LEDGER_BACKEND=postgres; shared-filesystem SQLite is unsafe." >&2
  exit 1
fi

if [[ "${audit_backend}" == "postgres" || "${audit_backend}" == "postgresql" ]]; then
  if ! ledger_dsn="$(lookup_env INFERA_AUDIT_LEDGER_DSN)" || [[ -z "${ledger_dsn}" ]]; then
    echo "ERROR: INFERA_AUDIT_LEDGER_DSN is required for the PostgreSQL audit ledger." >&2
    exit 1
  fi
fi

echo "Production env semantic validation passed (values hidden)."
