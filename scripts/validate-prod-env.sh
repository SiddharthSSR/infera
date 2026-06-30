#!/usr/bin/env bash
# validate-prod-env.sh - verify production env names are present without printing secrets.

set -euo pipefail

ENV_FILE="${ENV_FILE:-.env}"

required_vars=(
  INFERA_ADMIN_KEY
  INFERA_ALLOWED_ORIGINS
  INFERA_GATEWAY_ADDRESS
  INFERA_WORKER_SHARED_TOKEN
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

echo "Production env validation passed (${#required_vars[@]} required vars present; values hidden)."
