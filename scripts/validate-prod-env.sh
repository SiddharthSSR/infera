#!/usr/bin/env bash
# validate-prod-env.sh - verify production env names are present without printing secrets.

set -euo pipefail

ENV_FILE="${ENV_FILE:-.env}"

required_vars=(
  INFERA_ADMIN_KEY
  INFERA_ALLOWED_ORIGINS
  INFERA_GATEWAY_ADDRESS
  INFERA_WORKER_SHARED_TOKEN
  INFERA_RELEASE_ID
  INFERA_WORKER_PROTOCOL_VERSION
  INFERA_GATEWAY_IMAGE
  INFERA_CONTROL_STATE_DSN
  INFERA_PROVIDER_CREDENTIAL_ENCRYPTION_KEY
  INFERA_WORKER_IMAGE
  GRAFANA_ADMIN_USER
  GRAFANA_ADMIN_PASSWORD
  ALERT_EMAIL_TO
  ALERT_SMTP_FROM
  ALERT_SMTP_SMARTHOST
  ALERT_SMTP_USERNAME
  ALERT_SMTP_PASSWORD
)

lookup_env() {
  local name="$1"
  local value="${!name:-}"
  if [[ -n "${value}" ]]; then
    printf '%s' "${value}"
    return 0
  fi

  if [[ ! -f "${ENV_FILE}" ]]; then
    return 1
  fi

  python3 - "${ENV_FILE}" "${name}" <<'PY'
import os
import shlex
import sys
from pathlib import Path

path = Path(sys.argv[1])
name = sys.argv[2]

for raw_line in path.read_text().splitlines():
    line = raw_line.strip()
    if not line or line.startswith("#"):
        continue
    if line.startswith("export "):
        line = line[len("export ") :].strip()
    if "=" not in line:
        continue
    key, raw_value = line.split("=", 1)
    key = key.strip()
    if key != name:
        continue
    raw_value = raw_value.strip()
    try:
        parsed = shlex.split(raw_value, posix=True)
        value = parsed[0] if parsed else ""
    except ValueError:
        value = raw_value.strip("\"'")
    print(os.path.expandvars(value), end="")
    raise SystemExit(0)

raise SystemExit(1)
PY
}

missing=()
for name in "${required_vars[@]}"; do
  if ! value="$(lookup_env "${name}")" || [[ -z "${value}" ]]; then
    missing+=("${name}")
  fi
done

if (( ${#missing[@]} > 0 )); then
  echo "ERROR: missing required production env vars:" >&2
  for name in "${missing[@]}"; do
    echo "  - ${name}" >&2
  done
  exit 1
fi

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

# Production gateway startup is intentionally fail-closed without shared control state.
# A single replica uses the same durable path so scaling out does not change semantics.
control_state_dsn="$(lookup_env INFERA_CONTROL_STATE_DSN)"
if [[ -z "${control_state_dsn}" ]]; then
  echo "ERROR: INFERA_CONTROL_STATE_DSN is required outside development mode." >&2
  exit 1
fi
if [[ "${audit_backend}" == "postgres" || "${audit_backend}" == "postgresql" ]]; then
  if ! ledger_dsn="$(lookup_env INFERA_AUDIT_LEDGER_DSN)" || [[ -z "${ledger_dsn}" ]]; then
    echo "ERROR: INFERA_AUDIT_LEDGER_DSN is required for the PostgreSQL audit ledger." >&2
    exit 1
  fi
fi

echo "Production env validation passed (${#required_vars[@]} required vars present; values hidden)."
